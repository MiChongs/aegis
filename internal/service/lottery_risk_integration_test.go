package service

import (
	"errors"
	"fmt"
	"testing"

	"aegis/internal/config"
	lotterydomain "aegis/internal/domain/lottery"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"

	"go.uber.org/zap"
)

// TestLotteryRiskIntegration 风控规则在真实 Postgres 上：同设备 / 同网段聚集只发兜底且兜底奖照常发、
// 风控列落库、拒绝抽奖回滚并记事件、风控总览的并行聚合、记录检索式与风控筛选。
//
//	AEGIS_TEST_PG_DSN=postgres://... go test ./internal/service -run TestLotteryRiskIntegration
func TestLotteryRiskIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	rewarded := NewRewardedAdService(zap.NewNop(), pg, config.Config{})
	lottery := NewLotteryService(zap.NewNop(), pg, rewarded)
	const appID = int64(42000)
	mustExec(t, ctx, pool, `INSERT INTO apps (id, name) VALUES ($1, 'lottery-risk') ON CONFLICT DO NOTHING`, appID)

	// 没有谢谢参与：10 积分兜底，位置 1 是权重极大的 50 积分
	prizes := make([]lotterydomain.PrizeInput, 0, 8)
	for position := 0; position < 8; position++ {
		prizes = append(prizes, lotterydomain.PrizeInput{Position: position, Name: fmt.Sprintf("10 积分 %d", position),
			Reward: lotterydomain.Reward{Type: lotterydomain.KindPoints, Amount: 10}, Weight: 1, Quantity: -1, Fallback: position == 0})
	}
	prizes[1] = lotterydomain.PrizeInput{Position: 1, Name: "50 积分大奖", Reward: lotterydomain.Reward{Type: lotterydomain.KindPoints, Amount: 50},
		Weight: lotterydomain.MaxWeight, Quantity: -1}
	activity, err := lottery.CreateActivity(ctx, lotterydomain.ActivityInput{AppID: appID, Name: "风控", UIMode: lotterydomain.UIModeGrid,
		Status: lotterydomain.StatusActive, PityPosition: -1, Prizes: prizes, Operator: "admin",
		Strategy: lotterydomain.Strategy{Risk: &lotterydomain.RiskGuard{Rules: []lotterydomain.RiskRule{
			{Name: "同设备多账号", Signal: lotterydomain.RiskSignalDeviceAccounts, Op: lotterydomain.RiskGT, Threshold: 2, Action: lotterydomain.RiskFloor},
			{Signal: lotterydomain.RiskSignalSubnetAccounts, Op: lotterydomain.RiskGT, Threshold: 3, Action: lotterydomain.RiskDamp, Factor: 0.5},
			{Signal: lotterydomain.RiskSignalNoDevice, Op: lotterydomain.RiskGTE, Threshold: 1, Action: lotterydomain.RiskDeny},
		}}}})
	if err != nil {
		t.Fatalf("新建活动失败：%v", err)
	}
	users := make([]int64, 5)
	for index := range users {
		users[index] = insertTestUser(t, ctx, pool, appID, fmt.Sprintf("risk-%d", index), "0")
		mustExec(t, ctx, pool, `INSERT INTO lottery_draw_grants (appid, user_id, balance, total_granted) VALUES ($1, $2, 20, 20)`, appID, users[index])
	}
	draw := func(userID int64, device, ip string) (*lotterydomain.DrawResult, error) {
		return lottery.Draw(ctx, LotteryDrawRequest{AppID: appID, UserID: userID, ActivityID: activity.ID,
			Source: lotterydomain.SourceBonus, DeviceID: device, ClientIP: ip})
	}

	// 同一台设备、同一个 /24：前两个账号照常（50 积分），第 3 个起只发兜底；第 4 个起同网段也命中降权
	for index, userID := range users {
		result, err := draw(userID, "shared-phone", fmt.Sprintf("203.0.113.%d", 10+index))
		if err != nil {
			t.Fatalf("第 %d 个账号抽奖失败：%v", index+1, err)
		}
		wantFloor := index >= 2
		if got := result.Draw.Reward.Amount == 10; got != wantFloor {
			t.Fatalf("第 %d 个账号：只发兜底=%v，实际奖品 %+v", index+1, wantFloor, result.Draw.Reward)
		}
	}
	page, err := lottery.ListDraws(ctx, lotterydomain.DrawQuery{AppID: appID, ActivityID: activity.ID, Risk: "any", Limit: 50})
	if err != nil || page.Total != 3 {
		t.Fatalf("命中风控的记录应有 3 条：%+v %v", page, err)
	}
	last := page.Items[0]
	if last.RiskAction != lotterydomain.RiskFloor || len(last.RiskRules) != 2 || last.Trace == nil || last.Trace.RiskReport == nil {
		t.Fatalf("风控列与 trace 应落库：action=%s rules=%v", last.RiskAction, last.RiskRules)
	}
	if checks := last.Trace.RiskReport.Checks; checks[0].Value != 5 || checks[1].Value != 5 || !checks[1].Hit {
		t.Fatalf("同设备、同网段都应数到 5 个账号：%+v", checks)
	}
	if page, _ := lottery.ListDraws(ctx, lotterydomain.DrawQuery{AppID: appID, Risk: lotterydomain.RiskSignalSubnetAccounts}); page.Total != 2 {
		t.Fatalf("按信号筛同网段应有 2 条，得到 %d", page.Total)
	}

	// 没有设备号：拒绝抽奖，次数不扣，事件留痕
	var before int64
	if err := pool.QueryRow(ctx, `SELECT balance FROM lottery_draw_grants WHERE user_id = $1`, users[0]).Scan(&before); err != nil {
		t.Fatal(err)
	}
	_, err = draw(users[0], "", "198.51.100.1")
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != errCodeLotteryRiskBlocked {
		t.Fatalf("缺设备标识应拒绝（40348），得到 %v", err)
	}
	var after int64
	if err := pool.QueryRow(ctx, `SELECT balance FROM lottery_draw_grants WHERE user_id = $1`, users[0]).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("拒绝时次数不该扣：%d → %d", before, after)
	}

	overview, err := lottery.RiskOverview(ctx, appID, activity.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if overview.Draws != 5 || overview.Flagged != 3 || overview.Denied != 1 || len(overview.Events) != 1 || len(overview.Rules) != 3 {
		t.Fatalf("总览：draws=%d flagged=%d denied=%d events=%d rules=%d", overview.Draws, overview.Flagged, overview.Denied,
			len(overview.Events), len(overview.Rules))
	}
	if len(overview.Devices) != 1 || overview.Devices[0].Accounts != 5 || len(overview.Subnets) != 1 || overview.Subnets[0].Key != "203.0.113.0/24" {
		t.Fatalf("聚集：devices=%+v subnets=%+v", overview.Devices, overview.Subnets)
	}
	counts := map[string]lotterydomain.RiskCount{}
	for _, item := range overview.Signals {
		counts[item.Key] = item
	}
	if counts[lotterydomain.RiskSignalDeviceAccounts].Draws != 3 || counts[lotterydomain.RiskSignalNoDevice].Denied != 1 {
		t.Fatalf("按信号：%+v", overview.Signals)
	}

	// 检索式：单号、账号、奖品名、IP、组合
	cases := map[string]int64{
		last.DrawNo:                   1,
		"risk-3":                      1,
		"user:risk-":                  5,
		"prize:大奖":                    2,
		"203.0.113.12":                1,
		"user:risk- 10":               3,
		fmt.Sprint("user:", users[4]): 1,
		"ip:203.0.113.10 prize:积分":    1,
		"不存在的东西":                      0,
	}
	for keyword, want := range cases {
		page, err := lottery.ListDraws(ctx, lotterydomain.DrawQuery{AppID: appID, Keyword: keyword})
		if err != nil || page.Total != want || int64(len(page.Items)) != want {
			t.Fatalf("检索「%s」应有 %d 条：total=%v items=%d err=%v", keyword, want, page.Total, len(page.Items), err)
		}
	}
}
