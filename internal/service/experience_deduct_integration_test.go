package service

import (
	"encoding/json"
	"testing"

	"go.uber.org/zap"

	"aegis/internal/domain/itemgrant"
	"aegis/internal/domain/levels"
	pointdomain "aegis/internal/domain/points"
	pgrepo "aegis/internal/repository/postgres"
)

// 管理员扣减经验：keep 保留等级、recalc 按经验降级、不足整笔拒绝、锁定保底挡住降级、
// 冲正扣减（还回经验）、请求编号重放、再次升级不重复发奖励、批量逐人执行。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55447/postgres go test ./internal/service -run TestExperienceDeductionIntegration
func TestExperienceDeductionIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	points := NewPointsService(zap.NewNop(), pg, nil)
	levelSvc := NewLevelService(zap.NewNop(), pg)
	const appID = int64(40901)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'exp-deduct') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	gina := insertTestUser(t, ctx, pool, appID, "deduct-gina", "0")

	raw := func(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
	n := func(id, typ string, data any) levels.Node { return levels.Node{ID: id, Type: typ, Data: raw(data)} }
	e := func(src, handle, dst string) levels.Edge {
		return levels.Edge{ID: src + ":" + handle + "->" + dst, Source: src, SourceHandle: handle, Target: dst}
	}
	gift := []itemgrant.Item{{Type: itemgrant.ItemPoints, Amount: 1}}
	graph := levels.Graph{
		Nodes: []levels.Node{
			n("s", levels.NodeScheme, levels.SchemeConfig{NodeMeta: levels.NodeMeta{Label: "扣减"}}),
			n("l1", levels.NodeLevel, levels.LevelConfig{NodeMeta: levels.NodeMeta{Label: "一"}, Threshold: 0}),
			n("l2", levels.NodeLevel, levels.LevelConfig{NodeMeta: levels.NodeMeta{Label: "二"}, Threshold: 100}),
			n("l3", levels.NodeLevel, levels.LevelConfig{NodeMeta: levels.NodeMeta{Label: "三"}, Threshold: 300}),
			n("r3", levels.NodeReward, levels.RewardConfig{NodeMeta: levels.NodeMeta{Label: "三级礼"}, Items: gift}),
		},
		Edges: []levels.Edge{e("s", levels.HandleNext, "l1"), e("l1", levels.HandleNext, "l2"), e("l2", levels.HandleNext, "l3"), e("l3", levels.HandleAttach, "r3")},
	}
	if _, err := levelSvc.Publish(ctx, LevelPublishInput{AppID: appID, Graph: graph, Operator: "admin"}); err != nil {
		t.Fatal(err)
	}
	opts := pointdomain.AdminAdjustOptions{AdminID: 1, AdminAccount: "admin"}
	adjust := func(amount int64, mode pointdomain.LevelMode, requestID string) (*pointdomain.ExperienceAdjustResult, error) {
		return points.AdminAdjustExperience(ctx, pointdomain.AdjustInput{AppID: appID, UserID: gina, Amount: amount, LevelMode: mode, RequestID: requestID, Reason: "测试"}, opts)
	}
	level := func() int {
		t.Helper()
		state, err := pg.GrowthLevel(ctx, appID, gina)
		if err != nil {
			t.Fatal(err)
		}
		return state.Level
	}
	rewardsAt3 := func() int {
		t.Helper()
		var count int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_level_rewards WHERE user_id = $1 AND level = 3`, gina).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}

	if _, err := adjust(350, "", ""); err != nil {
		t.Fatal(err)
	}
	if level() != 3 || rewardsAt3() != 1 {
		t.Fatalf("after +350: level %d rewards %d", level(), rewardsAt3())
	}

	// keep：经验 250，等级仍是 3
	kept, err := adjust(-100, pointdomain.LevelKeep, "deduct-keep-0001")
	if err != nil {
		t.Fatal(err)
	}
	if kept.AfterAmount != 250 || kept.NewLevel != 3 || kept.LevelChanged || kept.OperationType != "consume" || kept.LevelMode != pointdomain.LevelKeep {
		t.Fatalf("keep result = %+v", kept)
	}
	// 同一编号重放：不再扣
	replay, err := adjust(-100, pointdomain.LevelKeep, "deduct-keep-0001")
	if err != nil || !replay.Replayed || replay.AfterAmount != 250 || replay.OperationType != "consume" {
		t.Fatalf("replay = %+v (%v)", replay, err)
	}

	// recalc：经验 150 → 降到 2，写一条管理员升级记录
	recalc, err := adjust(-100, pointdomain.LevelRecalc, "deduct-recalc-001")
	if err != nil {
		t.Fatal(err)
	}
	if recalc.AfterAmount != 150 || recalc.OldLevel != 3 || recalc.NewLevel != 2 || !recalc.LevelChanged || level() != 2 {
		t.Fatalf("recalc result = %+v, level %d", recalc, level())
	}
	var events int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_level_events WHERE user_id = $1 AND from_level = 3 AND to_level = 2 AND reason = 'admin'`, gina).Scan(&events); err != nil || events != 1 {
		t.Fatalf("demotion events = %d (%v)", events, err)
	}
	var txnType string
	var amount int64
	if err := pool.QueryRow(ctx, `SELECT type, amount FROM experience_transactions WHERE transaction_no = $1`, recalc.TransactionNo).Scan(&txnType, &amount); err != nil || txnType != "consume" || amount != -100 {
		t.Fatalf("ledger = %s %d (%v)", txnType, amount, err)
	}

	// 不足：整笔拒绝，余额不变
	if _, err := adjust(-1000, pointdomain.LevelRecalc, ""); err == nil {
		t.Fatal("deducting more than the balance should fail")
	}
	if _, err := adjust(-1, "drop", ""); err == nil {
		t.Fatal("unknown level mode should be rejected")
	}

	// 锁定保底 Lv.2：扣到 50 也不降
	if _, err := levelSvc.AdjustUser(ctx, LevelAdjustInput{AppID: appID, UserID: gina, Lock: true, Operator: "admin"}); err != nil {
		t.Fatal(err)
	}
	locked, err := adjust(-100, pointdomain.LevelRecalc, "")
	if err != nil {
		t.Fatal(err)
	}
	if locked.AfterAmount != 50 || locked.NewLevel != 2 || level() != 2 {
		t.Fatalf("locked deduction = %+v", locked)
	}

	// 冲正这笔扣减：还回 100 经验
	reversed, err := points.AdminReverseExperience(ctx, appID, gina, locked.TransactionNo, "", "", opts)
	if err != nil {
		t.Fatal(err)
	}
	if reversed.Amount != 100 || reversed.AfterAmount != 150 {
		t.Fatalf("reverse = %+v", reversed)
	}
	if _, err := points.AdminReverseExperience(ctx, appID, gina, reversed.TransactionNo, "", "", opts); err == nil {
		t.Fatal("a reversal must not be reversed again")
	}

	// 再升回 Lv.3：三级礼不重复写
	if _, err := adjust(200, "", ""); err != nil {
		t.Fatal(err)
	}
	if level() != 3 || rewardsAt3() != 1 {
		t.Fatalf("after re-promotion: level %d rewards %d", level(), rewardsAt3())
	}

	// 批量：一人成功、一人不存在
	batch, err := points.BatchAdjustUserExperience(ctx, []int64{gina, 99999999}, appID, 10, "consume", "批量回收", "batch-exp-00001", "recalc", opts)
	if err != nil {
		t.Fatal(err)
	}
	if batch.SuccessCount != 1 || batch.FailedCount != 1 || batch.Results[0].AfterAmount != 340 || batch.LevelMode != pointdomain.LevelRecalc {
		t.Fatalf("batch = %+v", batch)
	}
	again, err := points.BatchAdjustUserExperience(ctx, []int64{gina}, appID, 10, "consume", "批量回收", "batch-exp-00001", "recalc", opts)
	if err != nil || !again.Results[0].Replayed || again.Results[0].AfterAmount != 340 {
		t.Fatalf("batch replay = %+v (%v)", again, err)
	}
}
