package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"aegis/internal/config"
	lotterydomain "aegis/internal/domain/lottery"
	pgrepo "aegis/internal/repository/postgres"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// TestLotteryStrategyIntegration 出奖策略在真实 Postgres 上：每日限量并发不超发、个人上限、积分预算、
// 同设备聚集、行为画像的整条查询与规则、出奖方案落库且不给用户端、在线时长累计、模拟与单人预览。
//
//	AEGIS_TEST_PG_DSN=postgres://... go test ./internal/service -run TestLotteryStrategyIntegration
func TestLotteryStrategyIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	rewarded := NewRewardedAdService(zap.NewNop(), pg, config.Config{})
	lottery := NewLotteryService(zap.NewNop(), pg, rewarded)
	const appID = int64(41000)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'lottery-strategy') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}

	// 位置 1 是权重极大的 5 积分，其余是谢谢参与：不加限制时几乎每次都中它
	grid := func(name string, special lotterydomain.PrizeInput, strategy lotterydomain.Strategy) lotterydomain.ActivityInput {
		prizes := make([]lotterydomain.PrizeInput, 0, 8)
		for position := 0; position < 8; position++ {
			prizes = append(prizes, lotterydomain.PrizeInput{Position: position, Reward: lotterydomain.Reward{Type: lotterydomain.KindNone},
				Weight: 1, Quantity: -1, Fallback: position == 0})
		}
		special.Position = 1
		if special.Reward.Type == "" {
			special.Reward = lotterydomain.Reward{Type: lotterydomain.KindPoints, Amount: 5}
		}
		special.Weight, special.Quantity = lotterydomain.MaxWeight, -1
		prizes[1] = special
		return lotterydomain.ActivityInput{AppID: appID, Name: name, UIMode: lotterydomain.UIModeGrid,
			Status: lotterydomain.StatusActive, PityPosition: -1, Prizes: prizes, Strategy: strategy, Operator: "admin"}
	}
	create := func(input lotterydomain.ActivityInput) *lotterydomain.AdminActivity {
		t.Helper()
		activity, err := lottery.CreateActivity(ctx, input)
		if err != nil {
			t.Fatalf("新建活动失败：%v", err)
		}
		return activity
	}
	users := make([]int64, 12)
	for index := range users {
		users[index] = insertTestUser(t, ctx, pool, appID, fmt.Sprintf("strategy-%d", index), "0")
		if _, err := pool.Exec(ctx, `INSERT INTO lottery_draw_grants (appid, user_id, balance, total_granted) VALUES ($1, $2, 50, 50)`,
			appID, users[index]); err != nil {
			t.Fatal(err)
		}
	}
	drawAs := func(userID, activityID int64, device string) *lotterydomain.DrawResult {
		t.Helper()
		result, err := lottery.Draw(ctx, LotteryDrawRequest{AppID: appID, UserID: userID, ActivityID: activityID,
			Source: lotterydomain.SourceBonus, DeviceID: device})
		if err != nil {
			t.Fatalf("抽奖失败：%v", err)
		}
		return result
	}
	wins := func(activityID int64) int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM lottery_draws WHERE activity_id = $1 AND prize_kind <> 'none'`,
			activityID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// ── 全站每日限量：12 个人并发抽，只出 3 个 ──
	daily := create(grid("每日限量", lotterydomain.PrizeInput{DailyLimit: 3}, lotterydomain.Strategy{}))
	var wg sync.WaitGroup
	for _, userID := range users {
		wg.Add(1)
		go func(userID int64) {
			defer wg.Done()
			for range 2 {
				if _, err := lottery.Draw(ctx, LotteryDrawRequest{AppID: appID, UserID: userID, ActivityID: daily.ID,
					Source: lotterydomain.SourceBonus}); err != nil {
					t.Errorf("并发抽奖失败：%v", err)
				}
			}
		}(userID)
	}
	wg.Wait()
	var used int
	if err := pool.QueryRow(ctx, `SELECT COALESCE(SUM(used), 0) FROM lottery_prize_stock WHERE prize_id = $1 AND scope = 1`,
		daily.Prizes[1].ID).Scan(&used); err != nil {
		t.Fatal(err)
	}
	if got := wins(daily.ID); got != 3 || used != 3 {
		t.Fatalf("每日限量 3：实际出了 %d 个，计数 %d", got, used)
	}

	// ── 个人上限：同一个人最多中 2 次 ──
	personal := create(grid("个人上限", lotterydomain.PrizeInput{UserLimit: 2}, lotterydomain.Strategy{}))
	for range 6 {
		drawAs(users[0], personal.ID, "")
	}
	if got := wins(personal.ID); got != 2 {
		t.Fatalf("每人最多中 2 次，实际 %d", got)
	}

	// ── 积分预算：每天 12 积分，5 积分的奖品最多出 2 个 ──
	budget := create(grid("积分预算", lotterydomain.PrizeInput{}, lotterydomain.Strategy{Budget: &lotterydomain.Budget{DailyPoints: 12}}))
	for range 6 {
		drawAs(users[1], budget.ID, "")
	}
	if got := wins(budget.ID); got != 2 {
		t.Fatalf("预算 12 积分时 5 积分奖最多出 2 个，实际 %d", got)
	}

	// ── 同设备聚集：第 3 个账号起只出未中奖 ──
	risky := create(grid("同设备", lotterydomain.PrizeInput{}, lotterydomain.Strategy{Risk: &lotterydomain.RiskGuard{DeviceAccounts: 2}}))
	for index := range 4 {
		result := drawAs(users[2+index], risky.ID, "farm-device")
		won := result.Draw.PrizeKind != lotterydomain.KindNone
		if index < 2 && !won || index >= 2 && won {
			t.Fatalf("同设备第 %d 个账号：中奖=%v", index+1, won)
		}
	}

	// ── 行为画像：整条查询能跑，规则读得到签到、在线、邀请、会员 ──
	loyal, fresh := users[6], users[7]
	today := lotteryDayStart(time.Now())
	mustExec(t, ctx, pool, `UPDATE users SET created_at = NOW() - INTERVAL '400 days', experience = 500,
  vip_expire_at = NOW() + INTERVAL '30 days' WHERE id = $1`, loyal)
	mustExec(t, ctx, pool, `INSERT INTO sign_stats (user_id, appid, last_sign_date, consecutive_days, total_sign_days)
VALUES ($1, $2, $3::date, 9, 120)`, loyal, appID, today.Format(time.DateOnly))
	mustExec(t, ctx, pool, `INSERT INTO vip_transactions (transaction_no, user_id, appid, plan_name, duration_days, pay_channel, expire_after)
VALUES ('VT-STRATEGY-1', $1, $2, '月卡', 200, 'admin', NOW() + INTERVAL '30 days')`, loyal, appID)
	for index := range 3 {
		invitee := insertTestUser(t, ctx, pool, appID, fmt.Sprintf("invitee-%d", index), "0")
		mustExec(t, ctx, pool, `INSERT INTO invite_relations (appid, invitee_id, inviter_id, code, code_kind, source)
VALUES ($1, $2, $3, 'CODE', 'user', 'register')`, appID, invitee, loyal)
	}
	now := time.Now().UTC()
	if err := pg.AccrueOnlineSeconds(ctx, appID, loyal, now.Add(-3*time.Hour), now); err != nil {
		t.Fatal(err)
	}
	if err := pg.AccrueOnlineSeconds(ctx, appID, loyal, now.Add(-48*time.Hour), now.Add(-47*time.Hour)); err != nil {
		t.Fatal(err)
	}
	profile, err := pg.LotteryBehaviorProfile(ctx, appID, loyal, time.Now(), today)
	if err != nil {
		t.Fatalf("行为画像查询失败：%v", err)
	}
	if profile.RegisteredDays < 399 || profile.SignStreak != 9 || profile.SignTotalDays != 120 || !profile.Member ||
		profile.Invites != 3 || profile.MemberDays != 200 || profile.OnlineMinutes30d < 239 || profile.OnlineDays30d < 2 ||
		profile.MemberRate < 0.49 || profile.MemberRate > 0.51 || profile.RecencyDays > 1 {
		t.Fatalf("行为画像不对：%+v", profile)
	}
	blank, err := pg.LotteryBehaviorProfile(ctx, appID, fresh, time.Now(), today)
	if err != nil || blank.SignStreak != 0 || blank.Member || blank.OnlineMinutes30d != 0 {
		t.Fatalf("新号画像应为空：%+v %v", blank, err)
	}

	behavior := create(grid("行为画像", lotterydomain.PrizeInput{}, lotterydomain.Strategy{
		Behavior: &lotterydomain.BehaviorModel{MinFactor: 0.5, MaxBoost: 2},
		Rules: []lotterydomain.StrategyRule{{
			Name: "只给连签与常在线的人", When: `!(user.signStreak >= 7 && user.onlineMinutes30d >= 120 && user.invites >= 3)`,
			Kinds: []string{lotterydomain.KindPoints}, Factor: 0,
		}},
	}))
	if got := drawAs(loyal, behavior.ID, ""); got.Draw.PrizeKind != lotterydomain.KindPoints {
		t.Fatalf("满足规则的老用户应能中积分，得到 %s", got.Draw.PrizeKind)
	}
	for range 3 {
		if got := drawAs(fresh, behavior.ID, ""); got.Draw.PrizeKind != lotterydomain.KindNone {
			t.Fatalf("不满足规则的新号不该中积分，得到 %s", got.Draw.PrizeKind)
		}
	}

	// ── 出奖方案落库，只给管理端 ──
	page, err := lottery.ListDraws(ctx, lotterydomain.DrawQuery{AppID: appID, ActivityID: behavior.ID, UserID: loyal, Page: 1, Limit: 5})
	if err != nil || len(page.Items) != 1 || page.Items[0].Trace == nil || page.Items[0].Trace.Behavior == nil ||
		page.Items[0].Trace.Picked != behavior.Prizes[1].ID {
		t.Fatalf("管理端记录应带出奖方案：%+v %v", page, err)
	}
	if page.Items[0].Trace.Behavior.Score <= 0.3 {
		t.Fatalf("老用户的行为分偏低：%+v", page.Items[0].Trace.Behavior)
	}
	mine, err := lottery.MyDraws(ctx, appID, loyal, behavior.ID, 1, 5)
	if err != nil || len(mine.Items) != 1 || mine.Items[0].Trace != nil {
		t.Fatalf("用户端记录不该带出奖方案：%+v %v", mine, err)
	}

	// ── 模拟与单人预览 ──
	sim, err := lottery.Simulate(ctx, LotterySimulation{
		AppID: appID, ActivityID: behavior.ID, UserID: fresh,
		Options: lotterydomain.SimulationInput{Users: 50, DrawsPerUser: 20, Seed: 3},
	})
	if err != nil {
		t.Fatalf("模拟失败：%v", err)
	}
	if sim.Current == nil || sim.Current.WinRate != 0 || sim.Result.WinRate != 0 || sim.Result.Draws != 1000 {
		t.Fatalf("新号的预览与模拟都不该中积分：current=%+v result=%+v", sim.Current, sim.Result)
	}
	draft := grid("未保存", lotterydomain.PrizeInput{}, lotterydomain.Strategy{SoftPity: &lotterydomain.SoftPity{Start: 1, Step: 50}})
	draft.Prizes[1].Weight = 1
	sim, err = lottery.Simulate(ctx, LotterySimulation{AppID: appID, Input: &draft,
		Options: lotterydomain.SimulationInput{Users: 100, DrawsPerUser: 10, Seed: 9}})
	if err != nil {
		t.Fatalf("模拟未保存的活动失败：%v", err)
	}
	if sim.Current != nil || sim.Result.WinRate <= sim.Result.BaseWinRate {
		t.Fatalf("软保底应抬高未保存活动的模拟中奖率：%+v", sim.Result)
	}
	if _, err := lottery.Simulate(ctx, LotterySimulation{AppID: appID, ActivityID: behavior.ID,
		Options: lotterydomain.SimulationInput{Users: 5000, DrawsPerUser: 1000}}); errCode(err) != 40000 {
		t.Fatalf("超出规模的模拟应被拒，得到 %v", err)
	}

	// ── 控制层：PRD + 运气平衡 + 全站反馈 + 护栏，运气累计落库，护栏写进方案 ──
	controlled := grid("控制层", lotterydomain.PrizeInput{Value: 50}, lotterydomain.Strategy{
		Variance: lotterydomain.VariancePRD,
		Luck:     &lotterydomain.LuckBalance{Strength: 0.4},
		WinRate:  &lotterydomain.WinRateControl{Target: 0.3, Gain: 3},
		Guard:    &lotterydomain.Guard{MaxWinRate: 0.5, MaxValue: 20},
	})
	controlled.Prizes[1].Weight = 1
	control := create(controlled)
	if control.Prizes[1].Value != 50 {
		t.Fatalf("积分当量应落库：%+v", control.Prizes[1])
	}
	for range 8 {
		result := drawAs(users[8], control.ID, "")
		_ = result
	}
	var expected, variance float64
	if err := pool.QueryRow(ctx, `SELECT luck_expected, luck_variance FROM lottery_user_states WHERE activity_id = $1 AND user_id = $2`,
		control.ID, users[8]).Scan(&expected, &variance); err != nil {
		t.Fatal(err)
	}
	if expected <= 0 || variance <= 0 || expected > 8 {
		t.Fatalf("运气累计应随抽奖增长：expected=%v variance=%v", expected, variance)
	}
	cp, err := lottery.ListDraws(ctx, lotterydomain.DrawQuery{AppID: appID, ActivityID: control.ID, Page: 1, Limit: 20})
	if err != nil || len(cp.Items) != 8 {
		t.Fatalf("控制层记录：%+v %v", cp, err)
	}
	for _, item := range cp.Items {
		plan := item.Trace
		if plan == nil || plan.Fair <= 0 || plan.WinRate > 0.5+1e-4 || plan.Value > 20+1e-3 {
			t.Fatalf("方案应满足护栏并带公平中奖率：%+v", plan)
		}
	}

	// ── 审计：线上出奖服从每一次的方案 ──
	for range 40 {
		drawAs(users[9], control.ID, "")
	}
	audit, err := lottery.Audit(ctx, appID, control.ID, 7)
	if err != nil || audit.Draws+audit.Skipped != 48 || audit.DF < 1 || len(audit.Prizes) != 8 {
		t.Fatalf("审计：%+v %v", audit, err)
	}
	sim2, err := lottery.Simulate(ctx, LotterySimulation{AppID: appID, ActivityID: control.ID, UserID: users[9],
		Options: lotterydomain.SimulationInput{Users: 10, DrawsPerUser: 10, Seed: 1}})
	if err != nil || sim2.Analytic.Mean <= 0 || sim2.Analytic.LongRunWinRate <= 0 || len(sim2.Analytic.Hazard) == 0 {
		t.Fatalf("精确分析：%+v %v", sim2.Analytic, err)
	}

	// ── 保存与回读：策略与奖品限量原样落库 ──
	got, err := lottery.GetActivity(ctx, appID, behavior.ID)
	if err != nil || got.Strategy.Behavior == nil || len(got.Strategy.Rules) != 1 || got.Strategy.Behavior.MaxBoost != 2 {
		t.Fatalf("策略回读不对：%+v %v", got.Strategy, err)
	}
	got, err = lottery.GetActivity(ctx, appID, daily.ID)
	if err != nil || got.Prizes[1].DailyLimit != 3 {
		t.Fatalf("每日限量回读不对：%+v %v", got.Prizes[1], err)
	}
	bad := grid("坏规则", lotterydomain.PrizeInput{}, lotterydomain.Strategy{Rules: []lotterydomain.StrategyRule{{When: "user.nope", Factor: 1}}})
	if _, err := lottery.CreateActivity(ctx, bad); errCode(err) != 40000 {
		t.Fatalf("写错变量的规则应在保存时被拒，得到 %v", err)
	}
}

func mustExec(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("%v\n%s", err, sql)
	}
}
