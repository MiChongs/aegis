package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"time"

	lotterydomain "aegis/internal/domain/lottery"

	"github.com/jackc/pgx/v5"
)

// ── 行为画像 ──

// lotteryBehaviorSQL 一个人在应用里的全部行为，一条语句取齐（都是按人的索引查询）。
// $1 user_id、$2 appid、$3 此刻、$4 应用时区的今天（DATE）。
//
// 最近一次在线取 user_online_daily.updated_at（最后一次累计的时刻），不能取 day::timestamptz：
// day 是 UTC 日期，转成时刻是那一天的零点、还随会话时区变，「最近活跃」会差出一天多。
const lotteryBehaviorSQL = `
WITH u AS (
  SELECT id, created_at, vip_expire_at, vip_lifetime_at, integral, experience
  FROM users WHERE id = $1 AND appid = $2
), online AS (
  SELECT
    COALESCE(SUM(seconds) FILTER (WHERE day >= $4::date - 6), 0)  AS s7,
    COALESCE(SUM(seconds) FILTER (WHERE day >= $4::date - 29), 0) AS s30,
    COUNT(*) FILTER (WHERE day >= $4::date - 29 AND seconds >= 60) AS d30,
    MAX(updated_at) AS last_at
  FROM user_online_daily WHERE user_id = $1 AND day >= $4::date - 29
), logins AS (
  SELECT COUNT(DISTINCT (created_at AT TIME ZONE 'UTC')::date) AS d30, MAX(created_at) AS last_at
  FROM login_audit_logs
  WHERE user_id = $1 AND appid = $2 AND status = 'success' AND created_at >= $3::timestamptz - INTERVAL '30 days'
)
SELECT
  EXTRACT(EPOCH FROM ($3::timestamptz - u.created_at)) / 86400,
  online.s7 / 60.0, online.s30 / 60.0, online.d30, logins.d30,
  GREATEST(logins.last_at, online.last_at,
           (SELECT MAX(last_seen_at) FROM user_login_devices WHERE appid = $2 AND user_id = $1)),
  COALESCE((SELECT CASE WHEN s.last_sign_date >= $4::date - 1 THEN s.consecutive_days ELSE 0 END
            FROM sign_stats s WHERE s.user_id = $1 AND s.appid = $2), 0),
  (SELECT COUNT(*) FROM daily_signins WHERE user_id = $1 AND appid = $2 AND sign_date >= $4::date - 29),
  COALESCE((SELECT total_sign_days FROM sign_stats WHERE user_id = $1 AND appid = $2), 0),
  EXISTS (SELECT 1 FROM daily_signins WHERE user_id = $1 AND appid = $2 AND sign_date = $4::date),
  COALESCE(u.vip_expire_at > $3::timestamptz, FALSE) OR u.vip_lifetime_at IS NOT NULL,
  u.vip_lifetime_at IS NOT NULL,
  COALESCE((SELECT SUM(duration_days) FROM vip_transactions WHERE user_id = $1 AND appid = $2), 0),
  COALESCE((SELECT SUM(amount) FROM payment_orders WHERE user_id = $1 AND appid = $2 AND status = 'paid'), 0),
  (SELECT COUNT(*) FROM payment_orders WHERE user_id = $1 AND appid = $2 AND status = 'paid'),
  u.experience,
  COALESCE((SELECT current_level FROM user_level_records WHERE user_id = $1 AND appid = $2), 1),
  u.integral,
  COALESCE((SELECT SUM(amount) FROM integral_transactions
            WHERE user_id = $1 AND appid = $2 AND amount > 0 AND created_at >= $3::timestamptz - INTERVAL '30 days'), 0),
  (SELECT COUNT(*) FROM invite_relations WHERE appid = $2 AND inviter_id = $1),
  (SELECT COUNT(*) FROM app_rewarded_ad_views
   WHERE appid = $2 AND user_id = $1 AND status = 'granted' AND created_at >= $3::timestamptz - INTERVAL '30 days'),
  (SELECT COUNT(*) FROM points_store_redemptions WHERE appid = $2 AND user_id = $1)
    + (SELECT COUNT(*) FROM card_key_redemptions WHERE appid = $2 AND user_id = $1),
  (SELECT COUNT(*) FROM user_login_devices WHERE appid = $2 AND user_id = $1),
  0::bigint -- 风险分从风控平台取（见 Repository.SetRiskScorer）
FROM u, online, logins`

// LotteryBehaviorProfile 这个人的行为画像。today 是应用时区的今天。
//
// 在抽奖事务之外取：都是只读的历史事实，不需要与抽奖串行，也不该让这一串查询占着用户行锁。
func (r *Repository) LotteryBehaviorProfile(ctx context.Context, appID, userID int64, now, today time.Time) (lotterydomain.BehaviorProfile, error) {
	var p lotterydomain.BehaviorProfile
	var lastActive *time.Time
	var paid float64
	var onlineDays, loginDays, signDays30, paidOrders, invites, ads, redemptions, devices int64
	var signStreak, signTotal, memberDays, experience, level, integral, earned, risk int64
	err := r.pool.QueryRow(ctx, lotteryBehaviorSQL, userID, appID, now, today.Format(time.DateOnly)).Scan(
		&p.RegisteredDays, &p.OnlineMinutes7d, &p.OnlineMinutes30d, &onlineDays, &loginDays, &lastActive,
		&signStreak, &signDays30, &signTotal, &p.SignedToday, &p.Member, &p.Lifetime, &memberDays, &paid, &paidOrders,
		&experience, &level, &integral, &earned, &invites, &ads, &redemptions, &devices, &risk,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return p, ErrUserNotFound
		}
		return p, err
	}
	p.RegisteredDays = math.Max(0, p.RegisteredDays)
	p.OnlineDays30d, p.LoginDays30d = float64(onlineDays), float64(loginDays)
	p.SignStreak, p.SignDays30d, p.SignTotalDays = float64(signStreak), float64(signDays30), float64(signTotal)
	p.MemberDays = math.Max(0, float64(memberDays))
	switch {
	case p.Lifetime:
		p.MemberRate = 1
	case p.RegisteredDays >= 1:
		p.MemberRate = math.Min(1, p.MemberDays/p.RegisteredDays)
	case p.Member:
		p.MemberRate = 1
	}
	p.PaidAmount, p.PaidOrders = paid, float64(paidOrders)
	p.Experience, p.Level, p.Integral = float64(experience), float64(level), float64(integral)
	p.PointsEarned30d, p.Invites, p.AdViews30d = float64(earned), float64(invites), float64(ads)
	p.Redemptions, p.Devices, p.RiskScore30d = float64(redemptions), float64(devices), float64(risk)
	if r.riskScore != nil {
		p.RiskScore30d = float64(r.riskScore(ctx, appID, userID))
	}
	if lastActive != nil {
		p.RecencyDays = math.Max(0, now.Sub(*lastActive).Hours()/24)
	} else {
		p.RecencyDays = p.RegisteredDays
	}
	return p, nil
}

// ── 抽奖时的状态 ──

// lotteryDrawFacts 选奖所需的、本人状态行以外的事实：全站计数、限量奖品今天已出几份、风控聚集信号。
// 一次事务里取一次（queueLotteryDrawFacts 入批），十连抽的后几次在内存里接着推进。
type lotteryDrawFacts struct {
	totals lotteryTotals
	// dailyUsed 设了每日限量的奖品今天全站已出几份
	dailyUsed      map[int64]int64
	deviceAccounts int
	ipAccounts     int
	subnetAccounts int
	deviceHistory  int
}

// queueLotteryDrawFacts 把这些读排进管道（只读策略用得到的）。全站计数命中读缓存时不读库。
func (r *Repository) queueLotteryDrawFacts(p *pgPipe, activity lotterydomain.Activity, prizes []lotterydomain.Prize,
	input lotterydomain.DrawInput, out *lotteryDrawFacts) {
	strategy := activity.Strategy
	out.dailyUsed = map[int64]int64{}
	out.deviceAccounts, out.ipAccounts = 1, 1
	if strategy.WinRate != nil || strategy.Budget != nil {
		if cached, ok := r.cachedLotteryTotals(activity.ID, input.DayStart); ok {
			out.totals = cached
		} else {
			p.row(func(row pgx.Row) error {
				if err := row.Scan(&out.totals.draws, &out.totals.wins, &out.totals.points); err != nil {
					return err
				}
				r.storeLotteryTotals(activity.ID, input.DayStart, out.totals)
				return nil
			}, lotteryActivityTotalsSQL, activity.ID, input.DayStart)
		}
	}
	limited := make([]int64, 0, len(prizes))
	for _, prize := range prizes {
		if prize.DailyLimit > 0 {
			limited = append(limited, prize.ID)
		}
	}
	if len(limited) > 0 {
		p.rows(func(rows pgx.Rows) error {
			defer rows.Close()
			for rows.Next() {
				var id, used int64
				if err := rows.Scan(&id, &used); err != nil {
					return err
				}
				out.dailyUsed[id] = used
			}
			return rows.Err()
		}, lotteryPrizeDailyUsedSQL, limited, input.DayStart)
	}
	queueLotteryRiskFacts(p, activity, input, out)
}

// queueLotteryRiskFacts 风控聚集信号（同设备 / 同 IP / 同网段的其他账号、设备登录过的账号）。
// 十连抽每一次之前都重读：别人刚提交的抽奖，这一批的后几次就能看到。
func queueLotteryRiskFacts(p *pgPipe, activity lotterydomain.Activity, input lotterydomain.DrawInput, out *lotteryDrawFacts) {
	guard := activity.Strategy.Risk
	if guard == nil {
		return
	}
	count := func(dest *int, kind int16, key string) {
		p.row(func(row pgx.Row) error { return row.Scan(dest) }, lotteryRiskAccountsSQL,
			activity.ID, input.DayStart, kind, key, input.UserID)
	}
	if input.DeviceID != "" && guard.Uses(lotterydomain.RiskSignalDeviceAccounts) {
		count(&out.deviceAccounts, lotteryRiskKeyDevice, truncateColumn(input.DeviceID, 128))
	}
	if input.ClientIP != "" && guard.Uses(lotterydomain.RiskSignalIPAccounts) {
		count(&out.ipAccounts, lotteryRiskKeyIP, truncateColumn(input.ClientIP, 64))
	}
	subnet := lotterydomain.SubnetOf(input.ClientIP)
	if subnet != "" && guard.Uses(lotterydomain.RiskSignalSubnetAccounts) {
		count(&out.subnetAccounts, lotteryRiskKeySubnet, subnet)
	} else if subnet == "" && input.ClientIP != "" {
		out.subnetAccounts = 1
	}
	if input.DeviceID != "" && guard.Uses(lotterydomain.RiskSignalDeviceHistory) {
		p.row(func(row pgx.Row) error { return row.Scan(&out.deviceHistory) },
			`SELECT COUNT(*) FROM user_login_devices WHERE appid = $1 AND device_key = $2`, input.AppID, input.DeviceID)
	}
}

// buildLotteryDrawState 选奖用的状态（纯计算）：本人的部分来自状态行的计数（counters 已对齐到今天），
// 其余来自 facts。
func buildLotteryDrawState(activity lotterydomain.Activity, input lotterydomain.DrawInput, counters lotteryCounters,
	facts lotteryDrawFacts, now time.Time) lotterydomain.DrawState {
	strategy := activity.Strategy
	state := lotterydomain.DrawState{
		Now: now, Source: input.Source, MissStreak: counters.MissStreak, TotalDraws: int(counters.TotalDraws),
		TodayDraws: counters.DrawsToday, WinsTotal: int(counters.WinsTotal), WinsToday: counters.WinsToday,
		PrizeWins: counters.PrizeWins, PrizeWinsToday: counters.PrizeWinsToday, PrizeDailyUsed: map[int64]int64{},
		DeviceAccounts: 1, IPAccounts: 1, Profile: input.Profile,
		LuckExpected: counters.LuckExpected, LuckVariance: counters.LuckVariance, LuckSkew: counters.LuckSkew,
		DeviceMissing: input.DeviceID == "", IPMissing: input.ClientIP == "",
	}
	if state.PrizeWins == nil {
		state.PrizeWins = map[int64]int{}
	}
	if state.PrizeWinsToday == nil {
		state.PrizeWinsToday = map[int64]int{}
	}
	for id, used := range facts.dailyUsed {
		state.PrizeDailyUsed[id] = used
	}
	if input.Location != nil {
		state.Now = now.In(input.Location)
	}
	if !input.DayStart.IsZero() {
		state.DayProgress = math.Min(1, math.Max(0, now.Sub(input.DayStart).Hours()/24))
	}
	if strategy.WinRate != nil || strategy.Budget != nil {
		state.ActivityDrawsToday, state.ActivityWinsToday = int(facts.totals.draws), int(facts.totals.wins)
		state.PointsIssuedToday = facts.totals.points
	}
	if c := strategy.Cooldown; c != nil && c.Draws > 0 {
		state.Recent = counters.Recent[:min(len(counters.Recent), c.Draws)]
	}
	if guard := strategy.Risk; guard != nil {
		state.DeviceAccounts, state.IPAccounts = facts.deviceAccounts, facts.ipAccounts
		state.SubnetAccounts, state.DeviceHistory = facts.subnetAccounts, facts.deviceHistory
		if guard.Uses(lotterydomain.RiskSignalBurst) {
			state.RecentDraws = counters.recentDraws(now)
		}
	}
	return state
}

// lotteryDrawState 选奖用的状态（管理端预览用；抽奖事务里由 lotteryDrawRun 取一次、逐次推进）。
func (r *Repository) lotteryDrawState(ctx context.Context, q batchSender, activity lotterydomain.Activity,
	prizes []lotterydomain.Prize, input lotterydomain.DrawInput, counters lotteryCounters, now time.Time) (lotterydomain.DrawState, error) {
	var facts lotteryDrawFacts
	var p pgPipe
	r.queueLotteryDrawFacts(&p, activity, prizes, input, &facts)
	if err := p.send(ctx, q); err != nil {
		return lotterydomain.DrawState{}, err
	}
	return buildLotteryDrawState(activity, input, counters, facts, now), nil
}

// ── 在线时长 ──

// AccrueOnlineSeconds 把 [from, to) 这段在线时间按 UTC 自然日记进 user_online_daily。
// 同一个人多个设备同时在线会各记一份，单日封顶 86400 秒。
func (r *Repository) AccrueOnlineSeconds(ctx context.Context, appID, userID int64, from, to time.Time) error {
	from, to = from.UTC(), to.UTC()
	for from.Before(to) {
		dayEnd := time.Date(from.Year(), from.Month(), from.Day()+1, 0, 0, 0, 0, time.UTC)
		end := to
		if dayEnd.Before(end) {
			end = dayEnd
		}
		seconds := int64(end.Sub(from) / time.Second)
		if seconds > 0 {
			if _, err := r.pool.Exec(ctx, `INSERT INTO user_online_daily (appid, user_id, day, seconds)
VALUES ($1, $2, $3, LEAST($4, 86400))
ON CONFLICT (user_id, day) DO UPDATE SET seconds = LEAST(user_online_daily.seconds + EXCLUDED.seconds, 86400),
  updated_at = NOW()`, appID, userID, from.Format(time.DateOnly), seconds); err != nil {
				return err
			}
		}
		from = end
	}
	return nil
}

// LotteryPreview 管理端预览：这个人此刻再抽一次时的出奖方案（不加锁、不抽、不写）。
// 预览没有请求上下文：设备与 IP 取这个人在应用里最近一次抽奖用的，风控按它们评估；一次都没抽过时聚集按 1 个账号算。
func (r *Repository) LotteryPreview(ctx context.Context, activity lotterydomain.Activity, prizes []lotterydomain.Prize,
	appID, userID int64, profile lotterydomain.BehaviorProfile, now, dayStart time.Time, location *time.Location,
) (lotterydomain.DrawState, error) {
	counters, err := r.readLotteryCounters(ctx, activity.ID, userID, dayStart, now)
	if err != nil {
		return lotterydomain.DrawState{}, err
	}
	counters = counters.today(dayStart)
	facts, err := lotteryUserFacts(ctx, r.pool, appID, userID, counters, false)
	if err != nil {
		return lotterydomain.DrawState{}, err
	}
	input := lotterydomain.DrawInput{
		AppID: appID, UserID: userID, ActivityID: activity.ID, DayStart: dayStart, Now: now, Location: location,
		Profile: profile,
	}
	if err := r.pool.QueryRow(ctx, `SELECT COALESCE(device_id, ''), COALESCE(client_ip, '') FROM lottery_draws
WHERE appid = $1 AND user_id = $2 ORDER BY created_at DESC LIMIT 1`, appID, userID).Scan(&input.DeviceID, &input.ClientIP); err != nil &&
		!errors.Is(err, pgx.ErrNoRows) {
		return lotterydomain.DrawState{}, err
	}
	state, err := r.lotteryDrawState(ctx, r.pool, activity, prizes, input, counters, now)
	if err != nil {
		return state, err
	}
	if input.DeviceID == "" && input.ClientIP == "" {
		state.DeviceMissing, state.IPMissing = false, false
	}
	chances := lotterydomain.EvaluateChances(activity, facts, lotterydomain.VideoChances{})
	state.Source = chances.Next
	return state, nil
}

// MaxLotteryAuditDraws 一次审计最多读多少条记录（最近的优先）。
const MaxLotteryAuditDraws = 200_000

// LotteryAuditDraws 一个活动在 since 之后带出奖方案的记录（最近的优先，最多 MaxLotteryAuditDraws 条）。
func (r *Repository) LotteryAuditDraws(ctx context.Context, appID, activityID int64, since time.Time) ([]lotterydomain.AuditDraw, error) {
	rows, err := r.pool.Query(ctx, `SELECT d.trace FROM lottery_draws d
JOIN lottery_activities a ON a.id = d.activity_id AND a.appid = $1
WHERE d.activity_id = $2 AND d.created_at >= $3 AND d.trace IS NOT NULL
ORDER BY d.id DESC LIMIT $4`, appID, activityID, since, MaxLotteryAuditDraws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]lotterydomain.AuditDraw, 0, 1024)
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var plan lotterydomain.DrawPlan
		if err := json.Unmarshal(raw, &plan); err != nil || len(plan.Odds) == 0 {
			continue
		}
		out = append(out, lotterydomain.AuditDraw{Odds: plan.Odds, Picked: plan.Picked, Pity: plan.Pity, Fallback: plan.Fallback})
	}
	return out, rows.Err()
}
