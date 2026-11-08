package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	lotterydomain "aegis/internal/domain/lottery"
	"aegis/internal/domain/pointstore"
	vipdomain "aegis/internal/domain/vip"
)

// 抽奖事务（docs/lottery.md「容量与并发」）。
//
// 单抽与十连抽是同一个事务：单抽是 1 次，十连抽是 N 次在**一个事务**里逐次推进。每一次在自己的保存点里抽
// （保存点随前一批读一起开、随这一次的写一起释放，不单独占往返）：
// 这一次抽不了（次数用完、风控拒绝、库里出错）只回滚这一次，前面已经抽到的照常提交，批次在这里停下。
// 保底、每日运气、库存、奖品限额在这几次之间照常推进（状态行的计数在内存里逐次累加，最后写一次）。
//
// 一次事务的往返：
//  1. 锁与基础读一批发出：咨询锁、活动、锁用户行、奖品、幂等回放、锁状态行；
//  2. 判定要用的读一批发出：次数余额、积分、特权事实、会员事实、折算策略、套餐目录、运气干预、至尊加成、
//     商城单价、全站计数、限量已出、风控聚集、预取记录 ID；
//  3. 每一次：扣次数（一条语句挑批、扣批、记流水、扣汇总）、占库存、扣积分、发奖（这几步有先后依赖，逐条），
//     记录、增量、风控聚集键一批写；
//     第 2 次起先一批重读会被上一次改变的事实（次数余额、积分、特权、会员）与风控聚集信号；
//  4. 状态行一次写回，记录一次读回，提交。
//
// 以前每一次抽奖二十多次往返、十连抽十个事务；现在单抽约 7 次往返（抽中积分奖多 3 次），
// 十连抽一个事务、一次提交，后几次每次约 3 次往返（TestLotteryRoundTrips）。

// LotteryDrawOutcome 抽奖事务里一次的产出：记录、当时的活动与奖品，以及是否是重复提交的回放。
type LotteryDrawOutcome struct {
	Draw     lotterydomain.Draw
	Activity lotterydomain.Activity
	Prizes   []lotterydomain.Prize
	Replayed bool
}

// DrawLottery 抽一次（单事务，要么全成要么全不成）。随机数来自 crypto/rand，在服务端产生、不出事务。
func (r *Repository) DrawLottery(ctx context.Context, input lotterydomain.DrawInput) (*LotteryDrawOutcome, error) {
	outcomes, _, err := r.DrawLotteryBatch(ctx, input, []string{input.RequestID})
	if err != nil {
		return nil, err
	}
	return &outcomes[0], nil
}

// DrawLotteryBatch 在一个事务里连抽 len(requestIDs) 次（第 i 次的幂等键是 requestIDs[i]，可以为空）。
//
// 返回：抽到的（含原样回放的）、让批次中途停下的那个错误（第 2 次起抽不了时，前面的照常提交）、
// 第 1 次就抽不了时的错误（此时什么都没写）。风控拒绝的那一次单独记一条风控事件。
func (r *Repository) DrawLotteryBatch(ctx context.Context, input lotterydomain.DrawInput, requestIDs []string) (
	[]LotteryDrawOutcome, error, error) {
	if len(requestIDs) == 0 {
		requestIDs = []string{""}
	}
	outcomes, stop, err := r.drawLotteryBatch(ctx, input, requestIDs)
	for _, failure := range []error{err, stop} {
		var denied *LotteryRiskDeniedError
		if errors.As(failure, &denied) {
			// 那一次已回滚，没有抽奖记录：单独记一条风控事件，管理端「风控」里看得到被挡下的请求
			r.recordLotteryRiskEvent(ctx, input, denied.Plan)
		}
	}
	return outcomes, stop, err
}

// lotteryDrawRun 一个抽奖事务的上下文。
type lotteryDrawRun struct {
	r     *Repository
	tx    pgx.Tx
	input lotterydomain.DrawInput

	activity *lotterydomain.Activity
	prizes   []lotterydomain.Prize
	replays  map[string]lotterydomain.Draw
	counters lotteryCounters

	// 事务里不变的
	policy     vipdomain.ConversionPolicy
	plans      map[int64]pointstore.PlanRef
	features   map[string]pointstore.FeatureRef
	controls   []lotterydomain.FortuneControl
	appSupreme *lotterydomain.Supreme
	storePrice int64
	drawFacts  lotteryDrawFacts
	ids        []int64

	// 每一次之前（第 2 次起）重读的
	facts     lotterydomain.UserFacts
	open      int64 // 有剩余的批次之和（与 facts.Bonus 不等时扣次数前先对齐）
	perkFacts lotterydomain.PerkFacts
	holdings  vipdomain.Holdings
}

func (r *Repository) drawLotteryBatch(ctx context.Context, input lotterydomain.DrawInput, requestIDs []string) (
	[]LotteryDrawOutcome, error, error) {
	if input.Now.IsZero() {
		input.Now = time.Now()
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	run := &lotteryDrawRun{r: r, tx: tx, input: input}

	// ── 1. 锁与基础读 ──
	if err := run.lockAndLoad(ctx, requestIDs); err != nil {
		return nil, nil, err
	}
	// 全是回放（网络重试带同一组 requestId，而且都抽过了）：原样返回，不再读别的
	allReplayed := true
	for _, id := range requestIDs {
		if _, ok := run.replays[id]; id == "" || !ok {
			allReplayed = false
			break
		}
	}
	if allReplayed {
		outcomes := make([]LotteryDrawOutcome, 0, len(requestIDs))
		for _, id := range requestIDs {
			outcomes = append(outcomes, LotteryDrawOutcome{Draw: run.replays[id], Activity: *run.activity,
				Prizes: run.prizes, Replayed: true})
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, nil, err
		}
		committed = true
		return outcomes, nil, nil
	}

	// ── 2. 判定要用的读 ──
	if err := run.loadFacts(ctx, len(requestIDs)); err != nil {
		return nil, nil, err
	}

	// ── 3. 逐次抽 ──
	type pending struct {
		index    int
		drawID   int64
		replayed *lotterydomain.Draw
	}
	done := make([]pending, 0, len(requestIDs))
	var stop error
	fresh := 0
	for index, requestID := range requestIDs {
		if requestID != "" {
			if draw, ok := run.replays[requestID]; ok {
				draw := draw
				done = append(done, pending{index: index, replayed: &draw})
				continue
			}
		}
		if fresh > 0 {
			if err := run.refresh(ctx); err != nil {
				stop = err
				break
			}
		}
		drawID, err := run.drawOne(ctx, fresh, requestID, time.Now())
		if err != nil {
			stop = err
			break
		}
		done = append(done, pending{index: index, drawID: drawID})
		fresh++
	}
	if len(done) == 0 {
		return nil, nil, stop
	}

	// ── 4. 写回状态行、读回记录、提交 ──
	var p pgPipe
	ids := make([]int64, 0, fresh)
	for _, item := range done {
		if item.replayed == nil {
			ids = append(ids, item.drawID)
		}
	}
	draws := map[int64]lotterydomain.Draw{}
	if len(ids) > 0 {
		queueWriteLotteryCounters(&p, run.activity.ID, input.UserID, run.counters)
		p.rows(func(rows pgx.Rows) error {
			defer rows.Close()
			for rows.Next() {
				draw, err := scanLotteryDraw(rows)
				if err != nil {
					return err
				}
				draws[draw.ID] = *draw
			}
			return rows.Err()
		}, `SELECT `+lotteryDrawColumns+` `+lotteryDrawJoins+` WHERE d.id = ANY($1)`, ids)
		if err := p.send(ctx, tx); err != nil {
			return nil, nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	committed = true

	outcomes := make([]LotteryDrawOutcome, 0, len(done))
	for _, item := range done {
		outcome := LotteryDrawOutcome{Activity: *run.activity, Prizes: run.prizes}
		if item.replayed != nil {
			outcome.Draw, outcome.Replayed = *item.replayed, true
		} else {
			outcome.Draw = draws[item.drawID]
			r.noteLotteryDraw(run.activity.ID, input.DayStart, outcome.Draw.PrizeKind != lotterydomain.KindNone,
				lotteryIssuedPointsOf(outcome.Draw))
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes, stop, nil
}

// lockAndLoad 第一批：咨询锁（与改奖盘互斥）、活动、锁用户行、奖品、幂等回放、锁状态行。
//
// 锁等待上限压到 lotteryLockTimeout：同一个人在别的实例上的抽奖、兑换占着用户行时快速失败（报「繁忙」让客户端
// 稍后重试），不在连接池里排队。活动用共享咨询锁：行级 FOR SHARE 在高并发下会变成 MultiXact。
// 幂等键在锁用户行之后查：同一个键的并发请求，后一个等前一个提交后在这里看到它的记录。
func (run *lotteryDrawRun) lockAndLoad(ctx context.Context, requestIDs []string) error {
	input := run.input
	var p pgPipe
	var activityErr, userErr, countersErr error
	var userFound, countersFound bool
	p.exec(`SELECT set_config('lock_timeout', $1, true), pg_advisory_xact_lock_shared($2, $3::int)`,
		lotteryLockTimeout, lotteryActivityLockClass, int32(input.ActivityID))
	p.row(func(row pgx.Row) error {
		run.activity, activityErr = scanLotteryActivity(row)
		if activityErr != nil && !errors.Is(activityErr, ErrLotteryActivityNotFound) {
			return activityErr
		}
		return nil
	}, `SELECT `+lotteryActivityColumns+` FROM lottery_activities a WHERE a.id = $1 AND a.appid = $2`,
		input.ActivityID, input.AppID)
	p.row(func(row pgx.Row) error {
		var id int64
		userErr = row.Scan(&id)
		if errors.Is(userErr, pgx.ErrNoRows) {
			return nil
		}
		userFound = userErr == nil
		return userErr
	}, `SELECT id FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`, input.UserID, input.AppID)
	p.rows(func(rows pgx.Rows) error {
		var err error
		run.prizes, err = scanLotteryPrizeRows(rows)
		return err
	}, listLotteryPrizesSQL, input.ActivityID)
	run.replays = map[string]lotterydomain.Draw{}
	keys := make([]string, 0, len(requestIDs))
	for _, id := range requestIDs {
		if id != "" {
			keys = append(keys, id)
		}
	}
	if len(keys) > 0 {
		p.rows(func(rows pgx.Rows) error {
			defer rows.Close()
			for rows.Next() {
				draw, err := scanLotteryDraw(rows)
				if err != nil {
					return err
				}
				run.replays[draw.RequestID] = *draw
			}
			return rows.Err()
		}, `SELECT `+lotteryDrawColumns+` `+lotteryDrawJoins+`
WHERE d.activity_id = $1 AND d.user_id = $2 AND d.request_id = ANY($3)`, input.ActivityID, input.UserID, keys)
	}
	p.row(func(row pgx.Row) error {
		run.counters, countersErr = scanLotteryCounters(row)
		if errors.Is(countersErr, pgx.ErrNoRows) {
			return nil
		}
		countersFound = countersErr == nil
		return countersErr
	}, `SELECT `+lotteryCounterColumns+` FROM lottery_user_states WHERE activity_id = $1 AND user_id = $2 FOR UPDATE`,
		input.ActivityID, input.UserID)
	if err := p.send(ctx, run.tx); err != nil {
		return err
	}
	if activityErr != nil {
		return activityErr
	}
	if !userFound {
		return ErrUserNotFound
	}
	if !countersFound || !run.counters.trusted() {
		// 还没有状态行（建一行）或计数对不上（按记录重算）：都是每人一次的慢路径
		counters, err := lockLotteryCountersTx(ctx, run.tx, run.activity.ID, input.UserID, input.DayStart, input.Now)
		if err != nil {
			return err
		}
		run.counters = counters
	}
	run.counters = run.counters.today(input.DayStart)
	return nil
}

// loadFacts 第二批：判定要用的、事务里不变的事实，加上第一次的次数余额、积分、特权、会员事实，与预取的记录 ID。
func (run *lotteryDrawRun) loadFacts(ctx context.Context, count int) error {
	input, activity := run.input, *run.activity
	var p pgPipe
	run.queueUserFacts(&p)
	p.row(func(row pgx.Row) error {
		var err error
		run.policy, err = scanVipConversionPolicy(row, input.AppID)
		return err
	}, vipConversionPolicySQL, input.AppID)
	if lotteryNeedsCatalog(run.prizes) {
		run.plans, run.features = map[int64]pointstore.PlanRef{}, map[string]pointstore.FeatureRef{}
		p.rows(func(rows pgx.Rows) error { return scanCatalogPlans(rows, run.plans) }, pointsStoreCatalogPlansSQL, input.AppID)
		p.rows(func(rows pgx.Rows) error { return scanCatalogFeatures(rows, run.features) },
			pointsStoreCatalogFeaturesSQL, input.AppID)
	}
	if f := activity.Strategy.Fortune; f != nil && f.Spread > 0 {
		p.rows(func(rows pgx.Rows) error {
			var err error
			run.controls, err = scanFortuneControlRows(rows, input.Now)
			return err
		}, lotteryFortuneControlsSQL, activity.ID, input.UserID, input.Now)
		p.row(func(row pgx.Row) error {
			settings, err := scanLotteryAppSettings(row, input.AppID)
			if err == nil && settings.Supreme.Enabled {
				run.appSupreme = &settings.Supreme
			}
			return err
		}, lotteryAppSettingsSQL, input.AppID)
	}
	p.row(func(row pgx.Row) error {
		var err error
		run.storePrice, err = scanLotteryStoreDrawPrice(row)
		return err
	}, lotteryStoreDrawPriceSQL, input.AppID, pointstore.RewardLotteryDraws, pointstore.StatusActive)
	run.r.queueLotteryDrawFacts(&p, activity, run.prizes, input, &run.drawFacts)
	// 记录 ID 先取号：积分流水、发放结果都引用它，等发完再一次写入整条记录（不再「先插后改」）
	p.rows(func(rows pgx.Rows) error {
		defer rows.Close()
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return err
			}
			run.ids = append(run.ids, id)
		}
		return rows.Err()
	}, `SELECT nextval(pg_get_serial_sequence('lottery_draws', 'id')) FROM generate_series(1, $1::int)`, count)
	// 第一次的保存点随这一批开（见 drawOne）
	p.exec(`SAVEPOINT ` + lotteryDrawSavepoint)
	if err := p.send(ctx, run.tx); err != nil {
		return err
	}
	return run.afterUserFacts()
}

// refresh 第 2 次起：重读会被上一次改变的事实（次数余额、积分、特权事实、会员事实），以及别人刚提交的风控聚集信号。
func (run *lotteryDrawRun) refresh(ctx context.Context) error {
	var p pgPipe
	run.queueUserFacts(&p)
	queueLotteryRiskFacts(&p, *run.activity, run.input, &run.drawFacts)
	p.exec(`SAVEPOINT ` + lotteryDrawSavepoint)
	if err := p.send(ctx, run.tx); err != nil {
		return err
	}
	return run.afterUserFacts()
}

// queueUserFacts 次数余额（锁汇总行）、有剩余的批次（全部 / 兑换所得）、积分、特权事实、会员事实。
func (run *lotteryDrawRun) queueUserFacts(p *pgPipe) {
	input := run.input
	run.facts = lotterydomain.UserFacts{}
	run.open = 0
	p.row(func(row pgx.Row) error {
		err := row.Scan(&run.facts.Bonus)
		if errors.Is(err, pgx.ErrNoRows) {
			run.facts.Bonus = 0
			return nil
		}
		return err
	}, `SELECT balance FROM lottery_draw_grants WHERE appid = $1 AND user_id = $2 FOR UPDATE`, input.AppID, input.UserID)
	p.row(func(row pgx.Row) error { return row.Scan(&run.open, &run.facts.BonusPurchased) },
		`SELECT COALESCE(SUM(remaining), 0), COALESCE(SUM(remaining) FILTER (WHERE paid), 0) FROM lottery_draw_lots
WHERE appid = $1 AND user_id = $2 AND remaining > 0`, input.AppID, input.UserID)
	p.row(func(row pgx.Row) error {
		err := row.Scan(&run.facts.Integral)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserNotFound
		}
		return err
	}, `SELECT integral FROM users WHERE id = $1 AND appid = $2`, input.UserID, input.AppID)
	if lotterydomain.PerksActive(run.activity.Perks) {
		local := input.Now
		if input.Location != nil {
			local = input.Now.In(input.Location)
		}
		p.row(func(row pgx.Row) error {
			var err error
			run.perkFacts, err = scanLotteryPerkFacts(row, local)
			return err
		}, lotteryPerkFactsSQL, input.UserID, input.AppID, local, input.DayStart.Format(time.DateOnly))
	}
	p.row(func(row pgx.Row) error {
		facts, err := scanVipEntitlementFacts(row, input.AppID, input.UserID)
		if err != nil {
			return err
		}
		run.holdings = vipdomain.HoldingsOf(*facts, time.Now())
		return nil
	}, vipEntitlementFactsSQL, input.UserID, input.AppID)
}

// afterUserFacts 把余额整理成次数判定的口径（与 lotteryTicketFactsQ 相同），并接上状态行的计数。
func (run *lotteryDrawRun) afterUserFacts() error {
	if run.facts.Bonus <= 0 {
		run.facts.Bonus, run.facts.BonusPurchased = 0, 0
	} else {
		run.facts.BonusPurchased = min(run.facts.BonusPurchased, run.facts.Bonus)
	}
	c := run.counters
	run.facts.FreeUsedToday, run.facts.PointsUsedToday = c.FreeToday, c.PointsToday
	run.facts.MissStreak, run.facts.TotalDraws = c.MissStreak, c.TotalDraws
	return nil
}

// lotteryDrawSavepoint 每一次抽奖的保存点。随前一批读一起开（loadFacts / refresh），随这一次的写一起释放，
// 出错时回到它（这一次全部回滚，前面的不动）；提交时还开着的保存点随事务一起结束。
const lotteryDrawSavepoint = "lottery_draw"

// drawOne 抽一次（在保存点 lotteryDrawSavepoint 里，调用前已开），返回记录 ID。
// 出错时回到保存点：这一次写过的全部撤销，内存里的计数不动。
func (run *lotteryDrawRun) drawOne(ctx context.Context, index int, requestID string, now time.Time) (int64, error) {
	id, wrote, err := run.drawOneIn(ctx, index, requestID, now)
	if err != nil && wrote {
		if _, rbErr := run.tx.Exec(ctx, `ROLLBACK TO SAVEPOINT `+lotteryDrawSavepoint); rbErr != nil {
			return 0, rbErr
		}
	}
	return id, err
}

// drawOneIn 见 drawOne。wrote 表示出错前是否已经往库里写过（要回到保存点）。
func (run *lotteryDrawRun) drawOneIn(ctx context.Context, index int, requestID string, now time.Time) (int64, bool, error) {
	input, activity := run.input, *run.activity
	if reason := lotterydomain.Window(activity, now); reason != lotterydomain.ReasonOK {
		return 0, false, lotteryUnavailable(reason)
	}
	facts := run.facts

	// 人群特权：这个人眼里的活动（免费次数、积分价格、保底）与奖盘权重，与活动页同一个函数
	var effects lotterydomain.PerkEffects
	if lotterydomain.PerksActive(activity.Perks) {
		perkFacts := run.perkFacts
		perkFacts.Profile, perkFacts.TotalDraws = input.Profile, int(facts.TotalDraws)
		effects = lotterydomain.ResolvePerks(activity.Perks, perkFacts)
		effects = effects.WithAdSkip(activity, input.UserID, input.DayStart, input.Video, facts.FreeUsedToday)
	}
	personalActivity := effects.Personalize(activity)
	chances := lotterydomain.EvaluateChances(personalActivity, facts, lotterydomain.VideoChances{})
	source, reason := lotterydomain.ResolveSource(chances, input.Source)
	if reason != lotterydomain.ReasonOK {
		return 0, false, lotteryUnavailable(reason)
	}

	drawNo := lotteryDrawNo(now)
	var err error
	// 次数按使用顺序取一批：赠送的先于兑换的，同一档先到先用（lotterydomain.TicketConsumeOrder）
	var ticket lotteryTicketUse
	if source == lotterydomain.SourceBonus {
		ticket, err = run.consumeTicket(ctx, drawNo, "抽奖 "+activity.Name)
		if errors.Is(err, ErrLotteryTicketsInsufficient) {
			return 0, true, lotteryUnavailable(lotterydomain.ReasonBonusExhausted)
		}
		if err != nil {
			return 0, true, err
		}
	}

	// 已拥有判定在锁用户行之后取：同一把锁下，抽奖与购买 / 兑换会员是串行的
	coverage := lotterydomain.MemberCoverage{Holdings: run.holdings, Policy: run.policy}
	rule := lotterydomain.RuleOf(activity, coverage.Policy)
	// 今日运气计入管理员干预（全站与本人的），再按已拥有的判定加上至尊加成，与活动页同一个解析
	var fortune float64 = 1
	var supreme *lotterydomain.SupremeOutcome
	if f := activity.Strategy.Fortune; f != nil && f.Spread > 0 {
		fortune = lotterydomain.FortuneFactor(f, activity.ID, input.UserID, input.DayStart, now, run.controls)
		config, _ := lotterydomain.EffectiveSupreme(f, run.appSupreme)
		if supreme = lotterydomain.ResolveSupreme(config, coverage.Holdings, fortune); supreme != nil {
			fortune = supreme.Factor
		}
	}
	personal := lotterydomain.FortunePrizes(effects.BoostPrizes(lotterydomain.PersonalPrizes(rule, coverage, run.prizes,
		run.plans)), fortune)
	// 无套利：原价折算会让积分买次数稳赚时，已拥有格子的改发按刚好回本的比例缩减（正常奖盘照原价）
	rule = rule.WithEconomy(lotterydomain.EconomyOf(rule, coverage, personal, run.plans,
		lotterydomain.DrawPrice(personalActivity, run.storePrice)))

	state := buildLotteryDrawState(personalActivity, input, run.counters, run.drawFacts, now)
	state.Source = source
	prize, plan, err := pickLotteryPrize(ctx, run.tx, personalActivity, personal, state, input.DayStart, input.UserID)
	if err != nil {
		return 0, true, err
	}
	if !effects.Empty() {
		plan.Perks = &effects
	}
	if fortune != 1 {
		plan.Fortune = fortune
	}
	if supreme != nil {
		plan.FortuneBase, plan.Supreme = supreme.Base, &supreme.Grade
	}
	riskHits, riskAction := []string{}, (*string)(nil)
	if report := plan.RiskReport; report != nil && report.Action != "" {
		riskHits, riskAction = report.Hits, &report.Action
	}
	trace, err := json.Marshal(plan)
	if err != nil {
		return 0, true, err
	}
	status := lotterydomain.DrawGranted
	if prize.Kind == lotterydomain.KindManual {
		status = lotterydomain.DrawPending
	}
	var pointsCost int64
	if source == lotterydomain.SourcePoints {
		// 特权折扣之后的价格（与活动页上的 chances.points.cost 同一个数）
		pointsCost = chances.Points.Cost
	}
	reward, err := json.Marshal(prize.Reward)
	if err != nil {
		return 0, true, err
	}
	drawID := run.ids[index]
	var ticketLot *int64
	if ticket.LotID > 0 {
		ticketLot = &ticket.LotID
	}

	meta := map[string]any{"drawNo": drawNo, "activityId": activity.ID, "prizeId": prize.ID}
	var integralTxnNo string
	if pointsCost > 0 {
		_, _, integralTxnNo, err = applyIntegralChangeTx(ctx, run.tx, input.UserID, input.AppID, -pointsCost,
			"consume", lotterydomain.IntegralCategory, "积分抽奖", activity.Name, lotterydomain.SourceType, &drawID, meta)
		if err != nil {
			return 0, true, err
		}
	}

	// 抽中的奖品对他是已拥有的：按活动的处理方式改发（照常发放时 swapped=false）
	granted := prize
	var conversion *lotterydomain.Conversion
	if reason, owned := coverage.Owned(prize.Reward, run.plans); owned {
		if alt, swapped := lotterydomain.Swap(rule, prize, run.plans); swapped {
			label := lotterydomain.SwapLabel(alt)
			conversion = &lotterydomain.Conversion{
				Reason: reason, Reward: alt, Label: label,
				Message: lotterydomain.ConversionMessage(reason, prize.Name, label),
			}
			granted.Kind, granted.Reward = alt.Type, alt
			meta["converted"] = true
			meta["originalKind"] = prize.Kind
		}
	}
	title := "抽奖奖品"
	if conversion != nil {
		title = "抽奖奖品折算"
	}
	results, err := run.r.grantLotteryPrizeTx(ctx, run.tx, lotteryPrizeGrant{
		AppID: input.AppID, UserID: input.UserID, DrawID: drawID, DrawNo: drawNo,
		Title: title, Source: activity.Name, Meta: meta, Plans: run.plans, Features: run.features,
	}, granted)
	if err != nil {
		return 0, true, err
	}
	if conversion != nil {
		// 与卡密、邀请、管理员发放的折算记在同一处：用户的「折算记录」与控制台的折算视图都看得到
		if err := recordLotteryConversionTx(ctx, run.tx, input.AppID, input.UserID, rule, prize, run.plans, conversion,
			results); err != nil {
			return 0, true, err
		}
	}
	resultsJSON, err := json.Marshal(results)
	if err != nil {
		return 0, true, err
	}
	var conversionJSON []byte
	if conversion != nil {
		if conversionJSON, err = json.Marshal(conversion); err != nil {
			return 0, true, err
		}
	}

	// 记录、全站增量、风控聚集键一批写（都随事务提交），连同释放保存点；状态行在事务最后写一次
	var p pgPipe
	p.exec(`INSERT INTO lottery_draws
(id, draw_no, appid, activity_id, user_id, prize_id, prize_name, prize_kind, reward, position, source, points_cost, pity,
 status, request_id, device_id, client_ip, user_agent, created_at, trace, ticket_lot_id, ticket_origin, ticket_paid,
 risk_rules, risk_action, results, integral_transaction_no, conversion)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28)`,
		drawID, drawNo, input.AppID, activity.ID, input.UserID, prize.ID, truncateColumn(prize.Name, 64), prize.Kind, reward,
		prize.Position, source, pointsCost, plan.Pity, status, nullableString(requestID),
		nullableString(truncateColumn(input.DeviceID, 128)), nullableString(truncateColumn(input.ClientIP, 64)),
		nullableString(truncateColumn(input.UserAgent, 256)), now, trace, ticketLot, ticket.Origin, ticket.Paid,
		riskHits, riskAction, resultsJSON, nullableString(integralTxnNo), conversionJSON)
	win := prize.Kind != lotterydomain.KindNone
	issued := lotteryIssuedPoints(prize, conversion)
	queueLotteryActivityDelta(&p, activity.ID, input.DayStart, win, issued)
	if lotteryRiskKeysNeeded(activity.Strategy.Risk) {
		queueLotteryRiskKeys(&p, activity.ID, input.UserID, input.DayStart, input.DeviceID, input.ClientIP)
	}
	p.exec(`RELEASE SAVEPOINT ` + lotteryDrawSavepoint)
	if err := p.send(ctx, run.tx); err != nil {
		return 0, true, err
	}

	// 这一次成了：内存里的计数接着推进（下一次、以及事务最后写回状态行用）
	// 运气按控制层之前的公平中奖率累计（见 lottery.LuckZ）
	run.counters = run.counters.advance(input.DayStart, now, source, prize, plan,
		lotterydomain.NextMissStreak(facts.MissStreak, prize))
	run.drawFacts.totals = run.drawFacts.totals.plus(lotteryTotals{draws: 1, wins: boolInt64(win), points: issued})
	if prize.DailyLimit > 0 && !plan.Fallback {
		run.drawFacts.dailyUsed[prize.ID]++
	}
	if prize.Quantity >= 0 && !plan.Fallback {
		for i := range run.prizes {
			if run.prizes[i].ID == prize.ID {
				run.prizes[i].Used++
			}
		}
	}
	return drawID, false, nil
}

// consumeTicket 用掉一次。余额与批次对得上（平时都对得上）时：一条语句挑下一批、扣掉、记流水、扣汇总；
// 对不上（滚动发布期间旧版本动过）时走 consumeLotteryTicketTx，先对齐再扣。
func (run *lotteryDrawRun) consumeTicket(ctx context.Context, ref, title string) (lotteryTicketUse, error) {
	input := run.input
	balance := run.facts.Bonus
	if balance <= 0 {
		return lotteryTicketUse{}, ErrLotteryTicketsInsufficient
	}
	if balance != run.open {
		return consumeLotteryTicketTx(ctx, run.tx, input.AppID, input.UserID, ref, title)
	}
	// 一条语句里挑下一批、扣批次、记流水、扣汇总（修改型 CTE 在同一条语句里原子执行）：之后发奖时
	// （奖品本身是抽奖次数时会核对汇总与批次）两边已经一致
	var use lotteryTicketUse
	if err := run.tx.QueryRow(ctx, `WITH pick AS (
  SELECT id, origin, paid FROM lottery_draw_lots
  WHERE appid = $1 AND user_id = $2 AND remaining > 0
  ORDER BY `+lotterydomain.TicketConsumeOrder+` LIMIT 1 FOR UPDATE
), taken AS (
  UPDATE lottery_draw_lots l SET remaining = l.remaining - 1, updated_at = NOW() FROM pick WHERE l.id = pick.id
  RETURNING pick.id, pick.origin, pick.paid
), ledger AS (
  INSERT INTO lottery_draw_ledger (appid, user_id, lot_id, kind, origin, paid, delta, balance_after, ref, title, operator)
  SELECT $1, $2, taken.id, $3, taken.origin, taken.paid, -1, $4, $5, $6, NULL FROM taken
  RETURNING 1
), summary AS (
  UPDATE lottery_draw_grants SET balance = balance - 1, total_consumed = total_consumed + 1, updated_at = NOW()
  WHERE appid = $1 AND user_id = $2 AND balance > 0 AND EXISTS (SELECT 1 FROM taken)
  RETURNING 1
)
SELECT id, origin, paid FROM taken`, input.AppID, input.UserID, lotterydomain.LedgerConsume, balance-1,
		truncateColumn(ref, 64), truncateColumn(title, 128)).Scan(&use.LotID, &use.Origin, &use.Paid); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return use, ErrLotteryTicketsInsufficient
		}
		return use, err
	}
	use.Count = 1
	return use, nil
}

// queueWriteLotteryCounters 写回状态行（入批）。
func queueWriteLotteryCounters(p *pgPipe, activityID, userID int64, c lotteryCounters) {
	recent := c.Recent
	if recent == nil {
		recent = []int64{}
	}
	p.exec(lotteryCountersUpdateSQL, activityID, userID, c.MissStreak, c.TotalDraws, c.Counted, c.LastDrawAt,
		c.LuckExpected, c.LuckVariance, c.LuckSkew, c.DayStart, c.FreeToday, c.PointsToday, c.DrawsToday, c.WinsToday,
		c.WinsTotal, encodePrizeCounts(c.PrizeWins), encodePrizeCounts(c.PrizeWinsToday), recent, c.BurstStart,
		c.BurstCount, c.BurstPrev)
}

// lotteryIssuedPointsOf 一条记录发出去的积分（与 lotteryIssuedPoints 同一口径，读缓存用）。
func lotteryIssuedPointsOf(draw lotterydomain.Draw) int64 {
	if draw.Conversion != nil {
		if draw.Conversion.Reward.Type == lotterydomain.KindPoints {
			return draw.Conversion.Reward.Amount
		}
		return 0
	}
	if draw.Reward.Type == lotterydomain.KindPoints {
		return draw.Reward.Amount
	}
	return 0
}

func boolInt64(value bool) int64 {
	if value {
		return 1
	}
	return 0
}
