package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	lotterydomain "aegis/internal/domain/lottery"
	pgrepo "aegis/internal/repository/postgres"
)

// 抽奖高并发改造的端到端集成测试（真实 Postgres，迁移 000149，docs/lottery.md「容量与并发」）：
//
//   - 同一个人在两个实例上同时猛抽（2026-11-08 的事故形状）：每个请求要么抽成、要么立刻回 40925，
//     没有别的错误；记录数、次数余额、状态行计数三者对得上；
//   - 50 个人同时十连抽：全部抽成（全站计数分片、风控聚集键没有互相阻塞），全站计数之和等于记录数；
//   - 计数作废（滚动发布期间旧版本抽过）之后按记录重算并写回；
//   - 首次启动从历史记录补出全站计数与风控聚集键，只补一次。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestLotteryScaleIntegration
func TestLotteryScaleIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	lottery := NewLotteryService(zap.NewNop(), pg, nil)
	// 另一个实例：进程内的闸门不共享，两边之间靠库里的行锁串行
	peer := NewLotteryService(zap.NewNop(), pg, nil)
	const appID = int64(40300)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'lottery-scale') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	giveTickets := func(userID int64, count int) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO lottery_draw_grants (appid, user_id, balance, total_granted) VALUES ($1, $2, $3::bigint, $3::bigint)
ON CONFLICT (appid, user_id) DO UPDATE SET balance = lottery_draw_grants.balance + EXCLUDED.balance`, appID, userID, count); err != nil {
			t.Fatal(err)
		}
	}

	// 第 0 格兜底积分，第 1 格积分（常中），第 2 格经验，其余谢谢参与；开全站反馈、预算、冷却与两条风控
	prizes := make([]lotterydomain.PrizeInput, 0, 8)
	for position := 0; position < 8; position++ {
		prizes = append(prizes, lotterydomain.PrizeInput{Position: position, Reward: lotterydomain.Reward{Type: lotterydomain.KindNone},
			Weight: 10, Quantity: -1})
	}
	prizes[0] = lotterydomain.PrizeInput{Position: 0, Reward: lotterydomain.Reward{Type: lotterydomain.KindPoints, Amount: 1},
		Weight: 20, Quantity: -1, Fallback: true}
	prizes[1] = lotterydomain.PrizeInput{Position: 1, Reward: lotterydomain.Reward{Type: lotterydomain.KindPoints, Amount: 5},
		Weight: 30, Quantity: -1}
	prizes[2] = lotterydomain.PrizeInput{Position: 2, Reward: lotterydomain.Reward{Type: lotterydomain.KindExperience, Amount: 3},
		Weight: 20, Quantity: -1}
	activity, err := lottery.CreateActivity(ctx, lotterydomain.ActivityInput{
		AppID: appID, Name: "并发抽奖", UIMode: lotterydomain.UIModeGrid, Status: lotterydomain.StatusActive,
		PityPosition: -1, Prizes: prizes, Operator: "admin",
		Strategy: lotterydomain.Strategy{
			WinRate:  &lotterydomain.WinRateControl{Target: 0.5, Gain: 0.5},
			Budget:   &lotterydomain.Budget{DailyPoints: 1_000_000},
			Cooldown: &lotterydomain.Cooldown{Draws: 5, Factor: 0.8},
			Risk: &lotterydomain.RiskGuard{Rules: []lotterydomain.RiskRule{
				{Signal: lotterydomain.RiskSignalDeviceAccounts, Op: lotterydomain.RiskGTE, Threshold: 3, Action: lotterydomain.RiskFloor},
				{Signal: lotterydomain.RiskSignalBurst, Op: lotterydomain.RiskGTE, Threshold: 100_000, Action: lotterydomain.RiskDamp, Factor: 0.5},
			}},
		},
	})
	if err != nil {
		t.Fatalf("新建活动失败：%v", err)
	}
	dayStart := lotteryDayStart(time.Now())

	// reconcile 状态行的计数与记录逐项对账
	reconcile := func(userID int64, checkOrder bool) {
		t.Helper()
		var total, counted, today, winsTotal, winsToday int64
		var winsMatch, todayMatch, recentMatch bool
		if err := pool.QueryRow(ctx, `SELECT s.total_draws, s.counted_draws, s.draws_today, s.wins_total, s.wins_today,
  s.prize_wins = COALESCE((SELECT jsonb_object_agg(x.prize_id::text, x.n) FROM (
      SELECT prize_id, COUNT(*) AS n FROM lottery_draws
      WHERE activity_id = $1 AND user_id = $2 AND prize_kind <> 'none' GROUP BY prize_id) x), '{}'::jsonb),
  s.prize_wins_today = COALESCE((SELECT jsonb_object_agg(x.prize_id::text, x.n) FROM (
      SELECT prize_id, COUNT(*) AS n FROM lottery_draws
      WHERE activity_id = $1 AND user_id = $2 AND prize_kind <> 'none' AND created_at >= $3 GROUP BY prize_id) x), '{}'::jsonb),
  s.recent[1:5] = ARRAY(SELECT CASE WHEN prize_kind = 'none' THEN 0 ELSE prize_id END FROM lottery_draws
      WHERE activity_id = $1 AND user_id = $2 ORDER BY created_at DESC, id DESC LIMIT 5)
FROM lottery_user_states s WHERE s.activity_id = $1 AND s.user_id = $2`, activity.ID, userID, dayStart).Scan(
			&total, &counted, &today, &winsTotal, &winsToday, &winsMatch, &todayMatch, &recentMatch); err != nil {
			t.Fatal(err)
		}
		var actual, actualWins, actualToday, actualWinsToday int64
		if err := pool.QueryRow(ctx, `SELECT COUNT(*), COUNT(*) FILTER (WHERE prize_kind <> 'none'),
  COUNT(*) FILTER (WHERE created_at >= $3), COUNT(*) FILTER (WHERE created_at >= $3 AND prize_kind <> 'none')
FROM lottery_draws WHERE activity_id = $1 AND user_id = $2`, activity.ID, userID, dayStart).Scan(
			&actual, &actualWins, &actualToday, &actualWinsToday); err != nil {
			t.Fatal(err)
		}
		if total != actual || counted != actual || today != actualToday || winsTotal != actualWins || winsToday != actualWinsToday ||
			!winsMatch || !todayMatch || (checkOrder && !recentMatch) {
			t.Fatalf("用户 %d 计数与记录不符：计数 total=%d counted=%d today=%d wins=%d/%d 奖品=%v/%v 最近=%v；记录 %d/%d/%d/%d",
				userID, total, counted, today, winsTotal, winsToday, winsMatch, todayMatch, recentMatch,
				actual, actualToday, actualWins, actualWinsToday)
		}
	}

	// ── 同一个人在两个实例上各并发 30 个单抽 ──
	heavy := insertTestUser(t, ctx, pool, appID, "scale-heavy", "0")
	giveTickets(heavy, 500)
	var granted, busy atomic.Int64
	var mu sync.Mutex
	var unexpected []error
	var wg sync.WaitGroup
	started := time.Now()
	for i := 0; i < 60; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			svc := lottery
			if i%2 == 1 {
				svc = peer
			}
			_, err := svc.Draw(ctx, LotteryDrawRequest{AppID: appID, UserID: heavy, ActivityID: activity.ID,
				Source: lotterydomain.SourceBonus, RequestID: fmt.Sprintf("heavy-%d", i), DeviceID: "dev-heavy",
				ClientIP: "203.0.113.9"})
			switch errCode(err) {
			case 0:
				granted.Add(1)
			case errCodeLotteryDrawing:
				busy.Add(1)
			default:
				mu.Lock()
				unexpected = append(unexpected, err)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(unexpected) > 0 {
		t.Fatalf("同一个人并发抽奖只应抽成或回 40925，得到 %v", unexpected)
	}
	if granted.Load() < 2 || granted.Load()+busy.Load() != 60 {
		t.Fatalf("两个实例各至少抽成 1 次，得到抽成 %d、繁忙 %d", granted.Load(), busy.Load())
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("60 个并发请求应在几秒内全部答复，用了 %v", elapsed)
	}
	var balance int64
	if err := pool.QueryRow(ctx, `SELECT balance FROM lottery_draw_grants WHERE appid = $1 AND user_id = $2`, appID, heavy).
		Scan(&balance); err != nil || balance != 500-granted.Load() {
		t.Fatalf("次数余额应为 %d，得到 %d %v", 500-granted.Load(), balance, err)
	}
	reconcile(heavy, false)
	t.Logf("同一个人 60 个并发请求：抽成 %d、繁忙 %d，用时 %v", granted.Load(), busy.Load(), time.Since(started))

	// 排队的接着抽：依次抽，全部抽成
	sequential := time.Now()
	for i := 0; i < 20; i++ {
		if _, err := lottery.Draw(ctx, LotteryDrawRequest{AppID: appID, UserID: heavy, ActivityID: activity.ID,
			Source: lotterydomain.SourceBonus, DeviceID: "dev-heavy", ClientIP: "203.0.113.9"}); err != nil {
			t.Fatalf("依次抽奖失败：%v", err)
		}
	}
	t.Logf("依次抽 20 次，平均每次 %v", time.Since(sequential)/20)
	reconcile(heavy, true)

	// ── 50 个人同时十连抽，其中 3 个人共用一台设备 ──
	parallel := time.Now()
	users := make([]int64, 50)
	for index := range users {
		users[index] = insertTestUser(t, ctx, pool, appID, fmt.Sprintf("scale-u%02d", index), "0")
		giveTickets(users[index], 20)
	}
	// 共用设备的前两个账号先各抽一次（已提交），第 3 个账号来抽时应看到同设备已有 2 个别的账号
	for index := range 2 {
		if _, err := lottery.Draw(ctx, LotteryDrawRequest{AppID: appID, UserID: users[index], ActivityID: activity.ID,
			Source: lotterydomain.SourceBonus, DeviceID: "dev-shared", ClientIP: fmt.Sprintf("198.51.100.%d", index+1)}); err != nil {
			t.Fatalf("共用设备的账号先抽一次失败：%v", err)
		}
		giveTickets(users[index], 1)
	}
	unexpected = nil
	for index, userID := range users {
		wg.Add(1)
		go func() {
			defer wg.Done()
			device := fmt.Sprintf("dev-%02d", index)
			if index < 3 {
				device = "dev-shared"
			}
			result, err := lottery.DrawBatch(ctx, LotteryBatchRequest{LotteryDrawRequest: LotteryDrawRequest{
				AppID: appID, UserID: userID, ActivityID: activity.ID, Source: lotterydomain.SourceBonus,
				RequestID: fmt.Sprintf("batch-%d", index), DeviceID: device, ClientIP: fmt.Sprintf("198.51.100.%d", index+1),
			}, Count: 10})
			if err == nil && len(result.Draws) != 10 {
				err = fmt.Errorf("用户 %d 只抽到 %d 次（%+v）", userID, len(result.Draws), result.Stopped)
			}
			if err != nil {
				mu.Lock()
				unexpected = append(unexpected, err)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(unexpected) > 0 {
		t.Fatalf("不同的人同时十连抽应全部抽成，得到 %v", unexpected)
	}
	t.Logf("50 个人同时十连抽（500 次）用时 %v", time.Since(parallel))
	for _, userID := range users {
		reconcile(userID, true)
	}
	var shared int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM lottery_risk_keys
WHERE activity_id = $1 AND day_start = $2 AND kind = 1 AND key = 'dev-shared'`, activity.ID, dayStart).Scan(&shared); err != nil || shared != 3 {
		t.Fatalf("共用设备上应记下 3 个账号，得到 %d %v", shared, err)
	}
	var floored int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM lottery_draws d
WHERE d.activity_id = $1 AND d.risk_action = 'floor' AND d.device_id = 'dev-shared'`, activity.ID).Scan(&floored); err != nil || floored == 0 {
		t.Fatalf("同设备第 3 个账号起应命中「只发兜底」，得到 %d %v", floored, err)
	}

	// ── 全站计数之和等于记录数，管理端汇总取的是计数 ──
	if _, err := pg.CompactLotteryActivityDeltas(ctx); err != nil {
		t.Fatal(err)
	}
	var leftover int64
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM lottery_activity_deltas WHERE activity_id = $1`, activity.ID).
		Scan(&leftover); err != nil || leftover != 0 {
		t.Fatalf("合并之后增量应清空，剩 %d %v", leftover, err)
	}
	var sumDraws, sumWins, sumPoints, rows, rowWins, rowPoints int64
	if err := pool.QueryRow(ctx, `SELECT COALESCE(SUM(draws), 0), COALESCE(SUM(wins), 0), COALESCE(SUM(points_issued), 0)
FROM lottery_activity_daily WHERE activity_id = $1`, activity.ID).Scan(&sumDraws, &sumWins, &sumPoints); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*), COUNT(*) FILTER (WHERE prize_kind <> 'none'),
  COALESCE(SUM(CASE WHEN reward->>'type' = 'points' THEN (reward->>'amount')::bigint ELSE 0 END), 0)
FROM lottery_draws WHERE activity_id = $1`, activity.ID).Scan(&rows, &rowWins, &rowPoints); err != nil {
		t.Fatal(err)
	}
	if sumDraws != rows || sumWins != rowWins || sumPoints != rowPoints {
		t.Fatalf("全站计数 %d/%d/%d 应等于记录 %d/%d/%d", sumDraws, sumWins, sumPoints, rows, rowWins, rowPoints)
	}
	admin, err := lottery.GetActivity(ctx, appID, activity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if admin.TotalDraws != rows || admin.TodayDraws != rows || admin.TotalUsers != 51 {
		t.Fatalf("管理端汇总应为 %d 次、今天 %d 次、51 人，得到 %d/%d/%d", rows, rows, admin.TotalDraws, admin.TodayDraws, admin.TotalUsers)
	}

	// ── 计数作废（旧版本抽过）：活动页按记录重算并写回，下一抽照常 ──
	if _, err := pool.Exec(ctx, `UPDATE lottery_user_states SET counted_draws = -1, draws_today = 0, wins_total = 0,
  prize_wins = '{}', recent = '{}' WHERE activity_id = $1 AND user_id = $2`, activity.ID, heavy); err != nil {
		t.Fatal(err)
	}
	if _, err := lottery.State(ctx, appID, heavy, activity.ID); err != nil {
		t.Fatal(err)
	}
	reconcile(heavy, true)
	if _, err := pool.Exec(ctx, `UPDATE lottery_user_states SET total_draws = total_draws + 1 WHERE activity_id = $1 AND user_id = $2`,
		activity.ID, heavy); err != nil {
		t.Fatal(err)
	}
	if _, err := lottery.Draw(ctx, LotteryDrawRequest{AppID: appID, UserID: heavy, ActivityID: activity.ID,
		Source: lotterydomain.SourceBonus}); err != nil {
		t.Fatal(err)
	}
	reconcile(heavy, true)

	// ── 历史很长的人（事故里那个人抽了 4.4 万次）：每次抽奖的耗时与历史长度无关 ──
	veteran := insertTestUser(t, ctx, pool, appID, "scale-veteran", "0")
	giveTickets(veteran, 100)
	var noneID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM lottery_prizes WHERE activity_id = $1 AND kind = 'none' ORDER BY position LIMIT 1`,
		activity.ID).Scan(&noneID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO lottery_draws (draw_no, appid, activity_id, user_id, prize_id, prize_kind, source, device_id, created_at)
SELECT 'LTDVET' || g, $1, $2, $3, $4, 'none', 'bonus', 'dev-veteran', NOW() - (g || ' seconds')::interval
FROM generate_series(1, 50000) g`, appID, activity.ID, veteran, noneID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO lottery_user_states (activity_id, user_id, total_draws) VALUES ($1, $2, 50000)`,
		activity.ID, veteran); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ANALYZE lottery_draws`); err != nil {
		t.Fatal(err)
	}
	drawVeteran := func() time.Duration {
		t.Helper()
		begin := time.Now()
		if _, err := lottery.Draw(ctx, LotteryDrawRequest{AppID: appID, UserID: veteran, ActivityID: activity.ID,
			Source: lotterydomain.SourceBonus, DeviceID: "dev-veteran", ClientIP: "192.0.2.77"}); err != nil {
			t.Fatal(err)
		}
		return time.Since(begin)
	}
	recount := drawVeteran() // 第一次：计数对不上（-1），按 5 万条记录重算一次
	var steady time.Duration
	for range 20 {
		steady += drawVeteran()
	}
	steady /= 20
	t.Logf("5 万条历史：首次重算 %v，之后平均每次 %v", recount, steady)
	if steady > 50*time.Millisecond {
		t.Fatalf("计数生效后每次抽奖不应随历史变慢，平均 %v", steady)
	}
	reconcile(veteran, true)

	// ── 连接池只有 4 条、40 个人同时抽限量奖品：限量名额在事务外占，不能从同一个池借第二条连接（会把池锁死）──
	smallConfig := pool.Config()
	smallConfig.MaxConns = 4
	small, err := pgxpool.NewWithConfig(ctx, smallConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer small.Close()
	smallLottery := NewLotteryService(zap.NewNop(), pgrepo.New(small), nil)
	limited := make([]lotterydomain.PrizeInput, 0, 8)
	for position := 0; position < 8; position++ {
		limited = append(limited, lotterydomain.PrizeInput{Position: position, Reward: lotterydomain.Reward{Type: lotterydomain.KindPoints, Amount: 1},
			Weight: 1, Quantity: 1000, DailyLimit: 500, Fallback: position == 0})
	}
	limited[0].Quantity, limited[0].DailyLimit = -1, 0
	limitedActivity, err := smallLottery.CreateActivity(ctx, lotterydomain.ActivityInput{
		AppID: appID, Name: "限量", UIMode: lotterydomain.UIModeGrid, Status: lotterydomain.StatusActive,
		PityPosition: -1, Prizes: limited, Operator: "admin",
	})
	if err != nil {
		t.Fatal(err)
	}
	poolStarted := time.Now()
	unexpected = nil
	for index := range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			userID := users[index]
			for range 3 {
				if _, err := smallLottery.Draw(ctx, LotteryDrawRequest{AppID: appID, UserID: userID, ActivityID: limitedActivity.ID,
					Source: lotterydomain.SourceBonus}); err != nil {
					mu.Lock()
					unexpected = append(unexpected, err)
					mu.Unlock()
					return
				}
			}
		}()
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("小连接池上并发抽限量奖品卡住了（连接池死锁）")
	}
	if len(unexpected) > 0 {
		t.Fatalf("小连接池上并发抽限量奖品不应失败：%v", unexpected)
	}
	var used, issued int64
	if err := pool.QueryRow(ctx, `SELECT COALESCE(SUM(s.used), 0) FROM lottery_prize_stock s JOIN lottery_prizes p ON p.id = s.prize_id
WHERE p.activity_id = $1 AND s.scope = 0`,
		limitedActivity.ID).Scan(&used); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM lottery_draws d JOIN lottery_prizes p ON p.id = d.prize_id
WHERE d.activity_id = $1 AND p.quantity >= 0`, limitedActivity.ID).Scan(&issued); err != nil || used != issued {
		t.Fatalf("库存扣减 %d 应等于抽中限量奖品的记录数 %d %v", used, issued, err)
	}
	var dailyUsed, overCap int64
	if err := pool.QueryRow(ctx, `SELECT COALESCE(SUM(s.used), 0), COUNT(*) FILTER (WHERE s.used > s.cap)
FROM lottery_prize_stock s JOIN lottery_prizes p ON p.id = s.prize_id
WHERE p.activity_id = $1 AND s.scope = 1 AND s.day_start = $2`, limitedActivity.ID, dayStart).Scan(&dailyUsed, &overCap); err != nil ||
		dailyUsed != issued || overCap != 0 {
		t.Fatalf("每日限量分片之和 %d 应等于记录数 %d，且没有分片超过上限（%d 个超了）%v", dailyUsed, issued, overCap, err)
	}
	t.Logf("4 条连接、40 人各抽 3 次限量奖品：用时 %v", time.Since(poolStarted))

	// ── 库存分片：200 人同时抢 37 份，恰好出 37 份；改库存后已出的保留、余量重新分配 ──
	stockPrizes := make([]lotterydomain.PrizeInput, 0, 8)
	for position := 0; position < 8; position++ {
		stockPrizes = append(stockPrizes, lotterydomain.PrizeInput{Position: position,
			Reward: lotterydomain.Reward{Type: lotterydomain.KindNone}, Weight: 1, Quantity: -1, Fallback: position == 0})
	}
	stockPrizes[1] = lotterydomain.PrizeInput{Position: 1, Reward: lotterydomain.Reward{Type: lotterydomain.KindPoints, Amount: 2},
		Weight: 1_000_000, Quantity: 37}
	stockInput := lotterydomain.ActivityInput{AppID: appID, Name: "抢库存", UIMode: lotterydomain.UIModeGrid,
		Status: lotterydomain.StatusActive, PityPosition: -1, Prizes: stockPrizes, Operator: "admin"}
	stockActivity, err := lottery.CreateActivity(ctx, stockInput)
	if err != nil {
		t.Fatal(err)
	}
	crowd := make([]int64, 200)
	for index := range crowd {
		crowd[index] = insertTestUser(t, ctx, pool, appID, fmt.Sprintf("scale-crowd-%03d", index), "0")
		giveTickets(crowd[index], 5)
	}
	unexpected = nil
	for _, userID := range crowd {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := lottery.Draw(ctx, LotteryDrawRequest{AppID: appID, UserID: userID, ActivityID: stockActivity.ID,
				Source: lotterydomain.SourceBonus}); err != nil {
				mu.Lock()
				unexpected = append(unexpected, err)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if len(unexpected) > 0 {
		t.Fatalf("抢库存时抽奖不应失败：%v", unexpected)
	}
	stockOf := func() (winners, used, caps int64) {
		t.Helper()
		if err := pool.QueryRow(ctx, `SELECT
  (SELECT COUNT(*) FROM lottery_draws d JOIN lottery_prizes p ON p.id = d.prize_id WHERE p.activity_id = $1 AND p.quantity >= 0),
  (SELECT COALESCE(SUM(s.used), 0) FROM lottery_prize_stock s JOIN lottery_prizes p ON p.id = s.prize_id WHERE p.activity_id = $1 AND s.scope = 0),
  (SELECT COALESCE(SUM(s.cap), 0) FROM lottery_prize_stock s JOIN lottery_prizes p ON p.id = s.prize_id WHERE p.activity_id = $1 AND s.scope = 0)`,
			stockActivity.ID).Scan(&winners, &used, &caps); err != nil {
			t.Fatal(err)
		}
		return
	}
	if winners, used, caps := stockOf(); winners != 37 || used != 37 || caps != 37 {
		t.Fatalf("库存 37：应恰好抽中 37 份，得到中奖 %d、已出 %d、上限 %d", winners, used, caps)
	}
	// 加库存到 60：已出的 37 保留，再抽只能多出 23
	saved, err := lottery.GetActivity(ctx, appID, stockActivity.ID)
	if err != nil {
		t.Fatal(err)
	}
	for index := range stockInput.Prizes {
		stockInput.Prizes[index].ID = saved.Prizes[index].ID
	}
	stockInput.Prizes[1].Quantity = 60
	if _, err := lottery.UpdateActivity(ctx, stockActivity.ID, stockInput); err != nil {
		t.Fatalf("加库存失败：%v", err)
	}
	if _, used, caps := stockOf(); used != 37 || caps != 60 {
		t.Fatalf("加库存后已出 37、上限 60，得到 %d / %d", used, caps)
	}
	for _, userID := range crowd[:100] {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = lottery.Draw(ctx, LotteryDrawRequest{AppID: appID, UserID: userID, ActivityID: stockActivity.ID,
				Source: lotterydomain.SourceBonus})
		}()
	}
	wg.Wait()
	if winners, used, _ := stockOf(); winners != 60 || used != 60 {
		t.Fatalf("加库存后应再出 23 份、共 60，得到中奖 %d、已出 %d", winners, used)
	}
	stockInput.Prizes[1].Quantity = 59
	if _, err := lottery.UpdateActivity(ctx, stockActivity.ID, stockInput); errCode(err) != 40000 {
		t.Fatalf("库存不能改到已出的 60 以下，得到 %v", err)
	}

	// ── 首次启动补齐：清掉计数后从历史补，只补一次 ──
	if _, err := pool.Exec(ctx, `DELETE FROM lottery_activity_daily; DELETE FROM lottery_activity_deltas; DELETE FROM lottery_risk_keys; DELETE FROM schema_backfills`); err != nil {
		t.Fatal(err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	lottery.Start(runCtx)
	cancel()
	if err := pool.QueryRow(ctx, `SELECT COALESCE(SUM(draws), 0) FROM lottery_activity_daily WHERE activity_id = $1 AND day_start = $2`,
		activity.ID, dayStart).Scan(&sumDraws); err != nil {
		t.Fatal(err)
	}
	var todayRows int64
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM lottery_draws WHERE activity_id = $1 AND created_at >= $2`,
		activity.ID, dayStart).Scan(&todayRows); err != nil || sumDraws != todayRows {
		t.Fatalf("补出的今天计数应为 %d，得到 %d %v", todayRows, sumDraws, err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM lottery_risk_keys
WHERE activity_id = $1 AND day_start = $2 AND kind = 1 AND key = 'dev-shared'`, activity.ID, dayStart).Scan(&shared); err != nil || shared != 3 {
		t.Fatalf("补出的风控聚集键应有共用设备上的 3 个账号，得到 %d %v", shared, err)
	}
	if done, err := pg.BackfillLotteryCounters(ctx, "UTC", dayStart); err != nil || done {
		t.Fatalf("补过一次之后不再补，得到 %v %v", done, err)
	}
}
