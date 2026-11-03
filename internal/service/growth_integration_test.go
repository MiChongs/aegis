package service

import (
	"encoding/json"
	"testing"
	"time"

	"go.uber.org/zap"

	"aegis/internal/domain/growth"
	"aegis/internal/domain/itemgrant"
	"aegis/internal/domain/levels"
	userdomain "aegis/internal/domain/user"
	pgrepo "aegis/internal/repository/postgres"
)

// 成长调控（docs/growth-control.md）：个人 ÷2 与全应用 ×2 叠乘、每日上限、物品发放（等级奖励）入账换算、
// 撤销即恢复、等级封顶与降级、解除封顶补升、强制升级不能越过封顶、总览与推演。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55447/postgres go test ./internal/service -run TestGrowthControlIntegration
func TestGrowthControlIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	svc := NewGrowthService(zap.NewNop(), pg)
	levelSvc := NewLevelService(zap.NewNop(), pg)
	const appID = int64(40801)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'growth') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	ann := insertTestUser(t, ctx, pool, appID, "growth-ann", "0")
	bob := insertTestUser(t, ctx, pool, appID, "growth-bob", "0")

	raw := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	n := func(id, typ string, data any) levels.Node { return levels.Node{ID: id, Type: typ, Data: raw(data)} }
	e := func(src, handle, dst string) levels.Edge {
		return levels.Edge{ID: src + ":" + handle + "->" + dst, Source: src, SourceHandle: handle, Target: dst}
	}
	graph := levels.Graph{
		Nodes: []levels.Node{
			n("s", levels.NodeScheme, levels.SchemeConfig{NodeMeta: levels.NodeMeta{Label: "成长"}}),
			n("l1", levels.NodeLevel, levels.LevelConfig{NodeMeta: levels.NodeMeta{Label: "一"}, Threshold: 0}),
			n("r1", levels.NodeReward, levels.RewardConfig{NodeMeta: levels.NodeMeta{Label: "见面礼"}, Items: []itemgrant.Item{{Type: itemgrant.ItemPoints, Amount: 11}}}),
			n("l2", levels.NodeLevel, levels.LevelConfig{NodeMeta: levels.NodeMeta{Label: "二"}, Threshold: 100}),
			n("l3", levels.NodeLevel, levels.LevelConfig{NodeMeta: levels.NodeMeta{Label: "三"}, Threshold: 300}),
		},
		Edges: []levels.Edge{e("s", levels.HandleNext, "l1"), e("l1", levels.HandleAttach, "r1"), e("l1", levels.HandleNext, "l2"), e("l2", levels.HandleNext, "l3")},
	}
	if _, err := levelSvc.Publish(ctx, LevelPublishInput{AppID: appID, Graph: graph, Operator: "admin"}); err != nil {
		t.Fatal(err)
	}

	balances := func(userID int64) (int64, int64) {
		t.Helper()
		var integral, experience int64
		if err := pool.QueryRow(ctx, `SELECT integral, experience FROM users WHERE id = $1`, userID).Scan(&integral, &experience); err != nil {
			t.Fatal(err)
		}
		return integral, experience
	}

	// 个人：积分 ÷2 向下取整；全应用：经验 ×2；个人经验每日上限 150
	half, err := svc.CreateRule(ctx, growth.RuleInput{Enabled: true, AppID: appID, UserID: ann, Kind: growth.KindIntegral, Numerator: 1, Denominator: 2, Reason: "刷分", Operator: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if half.Effect != growth.EffectPenalty || half.Status != growth.StatusActive {
		t.Fatalf("rule = %+v", half)
	}
	if _, err := svc.CreateRule(ctx, growth.RuleInput{Enabled: true, AppID: appID, Kind: growth.KindExperience, Numerator: 2, Denominator: 1, Label: "双倍经验周", Operator: "admin"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateRule(ctx, growth.RuleInput{Enabled: true, AppID: appID, UserID: ann, Kind: growth.KindExperience, Numerator: 1, Denominator: 1, DailyCap: 150, Operator: "admin"}); err != nil {
		t.Fatal(err)
	}
	// ×1 且不限量的规则没有作用，拒绝
	if _, err := svc.CreateRule(ctx, growth.RuleInput{Enabled: true, AppID: appID, UserID: ann, Kind: growth.KindExperience, Numerator: 3, Denominator: 3}); err == nil {
		t.Fatal("no-op rule should be rejected")
	}

	today := time.Now()
	if _, err := pg.CreateDailySign(ctx, ann, appID, userdomain.SignInReward{IntegralReward: 9, ExperienceReward: 100, RewardMultiplier: 1}, today, "app", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if integral, experience := balances(ann); integral != 4 || experience != 150 {
		t.Fatalf("ann after sign = %d integral / %d exp, want 4 / 150", integral, experience)
	}
	// bob 只受全应用 ×2
	if _, err := pg.CreateDailySign(ctx, bob, appID, userdomain.SignInReward{IntegralReward: 9, ExperienceReward: 200, RewardMultiplier: 1}, today, "app", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if integral, experience := balances(bob); integral != 9 || experience != 400 {
		t.Fatalf("bob after sign = %d / %d, want 9 / 400", integral, experience)
	}

	// 等级见面礼走物品发放（受管来源）：11 积分 ÷2 = 5，流水记下倍率与原值
	if _, _, _, err := pg.SyncUserLevelAdmin(ctx, appID, ann, pgrepo.LevelAdminSync{}); err != nil {
		t.Fatal(err)
	}
	queued, err := pg.QueuedLevelRewards(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range queued {
		if _, err := pg.GrantLevelReward(ctx, id, pgrepo.LevelRewardAuto, 0); err != nil {
			t.Fatal(err)
		}
	}
	var granted, base int64
	var multiplier float64
	if err := pool.QueryRow(ctx, `SELECT amount, (extra_data->'growth'->>'base')::bigint, multiplier FROM integral_transactions
WHERE user_id = $1 AND source_type = 'item_grant' ORDER BY id DESC LIMIT 1`, ann).Scan(&granted, &base, &multiplier); err != nil {
		t.Fatal(err)
	}
	if granted != 5 || base != 11 || multiplier != 0.5 {
		t.Fatalf("item grant ledger = %d (base %d, ×%v), want 5 (base 11, ×0.5)", granted, base, multiplier)
	}

	// 总览：积分综合 ÷2、经验综合 ×2 且每日上限 150、今日已入账 150
	profile, err := svc.Profile(ctx, appID, ann)
	if err != nil {
		t.Fatal(err)
	}
	if profile.Integral.Factor != (growth.Factor{Num: 1, Den: 2}) || !profile.Integral.Restricted {
		t.Fatalf("integral view = %+v", profile.Integral)
	}
	if profile.Experience.Factor != (growth.Factor{Num: 2, Den: 1}) || profile.Experience.DailyCap != 150 || profile.Experience.UsedToday != 150 {
		t.Fatalf("experience view = %+v", profile.Experience)
	}
	// 签到流水（source_type daily_signin）归到 signin，只出现一次，并带着规则前后的数额
	signins := 0
	for _, stat := range profile.Integral.Stats {
		if stat.Source == growth.SourceSignin {
			signins++
			if stat.Base != 9 || stat.Granted != 4 || stat.Touched != 1 {
				t.Fatalf("signin stat = %+v, want base 9 / granted 4 / touched 1", stat)
			}
		}
	}
	if signins != 1 {
		t.Fatalf("signin appears %d times in stats", signins)
	}
	if len(profile.Rules) != 2 || len(profile.AppRules) != 1 || len(profile.Events) < 3 {
		t.Fatalf("profile rules=%d app=%d events=%d", len(profile.Rules), len(profile.AppRules), len(profile.Events))
	}

	// 推演：抽奖 10 积分，当前 ÷2 → 5；加一条 ÷5 草稿 → 1（10/10）
	preview, err := svc.Preview(ctx, GrowthPreviewInput{AppID: appID, UserID: ann, Kind: growth.KindIntegral, Source: growth.SourceLottery, Base: 10,
		Draft: &growth.RuleInput{Numerator: 1, Denominator: 5}})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Before.Granted != 5 || preview.After.Granted != 1 {
		t.Fatalf("preview = %+v", preview)
	}

	// 撤销 ÷2 之后，下一笔恢复原值（次日签到）
	if _, err := svc.RevokeRule(ctx, appID, half.ID, "admin", "申诉通过"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.RevokeRule(ctx, appID, half.ID, "admin", ""); err == nil {
		t.Fatal("revoking twice should fail")
	}
	if _, err := pg.CreateDailySign(ctx, ann, appID, userdomain.SignInReward{IntegralReward: 9, ExperienceReward: 0, RewardMultiplier: 1}, today.AddDate(0, 0, 1), "app", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if integral, _ := balances(ann); integral != 4+5+9 {
		t.Fatalf("ann integral after revoke = %d, want 18", integral)
	}

	// 等级封顶：bob 400 经验是 Lv.3；封顶 2 不降级被拒，降级后停在 2，再加经验也不升
	if _, _, _, err := pg.SyncUserLevelAdmin(ctx, appID, bob, pgrepo.LevelAdminSync{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetLevelCap(ctx, appID, bob, 2, false, "admin", ""); err == nil {
		t.Fatal("cap below current level without demote should fail")
	}
	state, err := svc.SetLevelCap(ctx, appID, bob, 2, true, "admin", "违规")
	if err != nil {
		t.Fatal(err)
	}
	if state.Level != 2 || state.Capped != 2 || state.Eligible < 2 {
		t.Fatalf("capped state = %+v", state)
	}
	if _, err := pg.CreateDailySign(ctx, bob, appID, userdomain.SignInReward{IntegralReward: 1, ExperienceReward: 50, RewardMultiplier: 1}, today.AddDate(0, 0, 1), "app", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if state, _ = pg.GrowthLevel(ctx, appID, bob); state.Level != 2 {
		t.Fatalf("capped user leveled up to %d", state.Level)
	}
	if _, err := levelSvc.AdjustUser(ctx, LevelAdjustInput{AppID: appID, UserID: bob, Level: 3, Operator: "admin"}); err == nil {
		t.Fatal("force upgrade above cap should fail")
	}
	var penaltyEvents int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_level_events WHERE user_id = $1 AND reason = 'penalty' AND to_level = 2`, bob).Scan(&penaltyEvents); err != nil || penaltyEvents != 1 {
		t.Fatalf("penalty level events = %d (%v)", penaltyEvents, err)
	}
	state, err = svc.ClearLevelCap(ctx, appID, bob, "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	if state.Level != 3 || state.Capped != 0 {
		t.Fatalf("after uncap = %+v", state)
	}

	// 列表筛选：生效中的处罚 / 全应用奖励 / 关键词
	page, err := svc.ListRules(ctx, growth.RuleQuery{AppID: appID, Scope: "app", Effect: growth.EffectBoost})
	if err != nil || page.Total != 1 {
		t.Fatalf("app boost rules = %v (%v)", page, err)
	}
	page, err = svc.ListRules(ctx, growth.RuleQuery{AppID: appID, Keyword: "growth-ann"})
	if err != nil || page.Total != 2 {
		t.Fatalf("keyword rules total = %v (%v)", page, err)
	}
	page, err = svc.ListRules(ctx, growth.RuleQuery{AppID: appID, Status: growth.StatusRevoked})
	if err != nil || page.Total != 1 {
		t.Fatalf("revoked rules = %v (%v)", page, err)
	}
}
