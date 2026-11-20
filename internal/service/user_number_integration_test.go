package service

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"go.uber.org/zap"

	"aegis/internal/domain/usernumber"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
)

// 用户号码的端到端集成测试：真实 Postgres（全部迁移跑两遍）。
// 「建号自动发默认位数的普通号、两个应用可以同号 → 发权益 → 查号（等级、要什么权益）→ 改号卡换普通号 →
// 冷却 → 靓号券换稀有号、旧号冻结（别人拿不到、自己免费换回）→ 摇号（候选锁定、别人拿不到、选中、次数扣减）→
// 保留号（用户拿不到、管理员指定）→ 回滚 → 删号冻结 → 总览与流水对平」。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55465/postgres go test ./internal/service -run TestUserNumberIntegration
func TestUserNumberIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	numbers := NewUserNumberService(zap.NewNop(), pg, nil)
	clock := time.Now()
	numbers.now = func() time.Time { return clock }

	const appID, otherApp = int64(33000), int64(33001)
	for _, id := range []int64{appID, otherApp} {
		if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'user-number-test') ON CONFLICT DO NOTHING`, id); err != nil {
			t.Fatal(err)
		}
	}
	alice := insertTestUser(t, ctx, pool, appID, "un-alice", "0")
	bob := insertTestUser(t, ctx, pool, appID, "un-bob", "0")

	code := func(err error) int {
		t.Helper()
		var appErr *apperrors.AppError
		if errors.As(err, &appErr) {
			return appErr.Code
		}
		t.Fatalf("not an app error: %v", err)
		return 0
	}

	// 建号发号：9 位、普通、按应用唯一
	me, err := numbers.Me(ctx, appID, alice)
	if err != nil {
		t.Fatal(err)
	}
	if len(strconv.FormatInt(me.Number, 10)) != 9 || me.Tier != usernumber.TierCommon {
		t.Fatalf("default number: %d tier %d", me.Number, me.Tier)
	}
	aliceStart := me.Number
	// 另一个应用可以有同一个号
	carol := insertTestUser(t, ctx, pool, otherApp, "un-carol", "0")
	if _, err := pool.Exec(ctx, `UPDATE users SET number = $1 WHERE id = $2`, aliceStart, carol); err != nil {
		t.Fatalf("same number in another app must be allowed: %v", err)
	}

	// 没有权益时查一个稀有号：空闲、要稀有靓号券、现在换不了
	check, err := numbers.Check(ctx, appID, alice, "527777103")
	if err != nil {
		t.Fatal(err)
	}
	if !check.Available || check.Tier != usernumber.TierRare || check.NeedKind != usernumber.RightTicket || check.NeedLevel != 2 || check.CanChange {
		t.Fatalf("check rare: %+v", check)
	}
	if _, err := numbers.Change(ctx, appID, alice, "527777103", 0, "", 0); code(err) != errCodeUserNumberNoRight {
		t.Fatalf("change without right: %v", err)
	}

	// 发权益：改号卡 1、稀有券 1、摇号 2
	for _, g := range []usernumber.Grant{
		{AppID: appID, UserID: alice, Kind: usernumber.RightChange, Amount: 1, Origin: usernumber.OriginItemGrant},
		{AppID: appID, UserID: alice, Kind: usernumber.RightTicket, Level: usernumber.TierRare, Amount: 1, Origin: usernumber.OriginCardKey},
		{AppID: appID, UserID: bob, Kind: usernumber.RightDraw, Amount: 2, Origin: usernumber.OriginLottery},
	} {
		if _, err := pg.GrantUserNumberRight(ctx, g); err != nil {
			t.Fatal(err)
		}
	}

	// 改号卡换普通号
	plain := int64(530729164)
	result, err := numbers.Change(ctx, appID, alice, strconv.FormatInt(plain, 10), 0, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Number != plain || result.Source != usernumber.SourceChange || result.Rights.Change != 0 || result.NextChangeAt == nil {
		t.Fatalf("change card: %+v", result)
	}
	// 冷却 7 天
	if _, err := numbers.Change(ctx, appID, alice, "527777103", 0, "", 0); code(err) != errCodeUserNumberCooldown {
		t.Fatalf("cooldown: %v", err)
	}
	clock = clock.Add(8 * 24 * time.Hour)

	// 改号卡用完了，普通号也能用稀有券换；这里换稀有号
	result, err = numbers.Change(ctx, appID, alice, "527777103", 0, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != usernumber.SourceTicket || result.Tier != usernumber.TierRare || result.Used == nil || result.Used.Level != 2 {
		t.Fatalf("ticket change: %+v", result)
	}
	// 旧号冻结：bob 拿不到，alice 免费换回
	check, err = numbers.Check(ctx, appID, bob, strconv.FormatInt(plain, 10))
	if err != nil {
		t.Fatal(err)
	}
	if check.Available || check.Reason != numberReasonFrozen {
		t.Fatalf("frozen for others: %+v", check)
	}
	clock = clock.Add(8 * 24 * time.Hour)
	check, err = numbers.Check(ctx, appID, alice, strconv.FormatInt(plain, 10))
	if err != nil {
		t.Fatal(err)
	}
	if !check.OwnFrozen || !check.CanChange {
		t.Fatalf("own frozen: %+v", check)
	}
	result, err = numbers.Change(ctx, appID, alice, strconv.FormatInt(plain, 10), 0, "", 0)
	if err != nil || result.Source != usernumber.SourceFrozen {
		t.Fatalf("switch back: %+v %v", result, err)
	}

	clock = clock.Add(8 * 24 * time.Hour)
	// 摇号：bob 摇出 6 个候选，次数扣 1
	drawn, err := numbers.Draw(ctx, appID, bob)
	if err != nil {
		t.Fatal(err)
	}
	if len(drawn.Draw.Candidates) == 0 || drawn.Rights.Draw != 1 {
		t.Fatalf("draw: %+v", drawn)
	}
	pick := drawn.Draw.Candidates[0]
	// 候选号别人拿不到
	if _, err := pg.GrantUserNumberRight(ctx, usernumber.Grant{AppID: appID, UserID: alice, Kind: usernumber.RightTicket,
		Level: usernumber.TierSupreme, Amount: 1, Origin: usernumber.OriginAdmin}); err != nil {
		t.Fatal(err)
	}
	if _, err := numbers.Change(ctx, appID, alice, strconv.FormatInt(pick.Number, 10), 0, "", 0); code(err) != errCodeUserNumberHeld {
		t.Fatalf("candidate locked for others: %v", err)
	}
	// 不在单里的号不能按摇号选
	if _, err := numbers.Change(ctx, appID, bob, "530729999", drawn.Draw.ID, "", 0); err == nil {
		t.Fatal("non-candidate must be rejected")
	}
	result, err = numbers.Change(ctx, appID, bob, strconv.FormatInt(pick.Number, 10), drawn.Draw.ID, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Source != usernumber.SourceDraw || result.Tier != pick.Tier {
		t.Fatalf("draw pick: %+v", result)
	}
	// 选完以后其余候选号释放
	free, err := pg.FreeUserNumbers(ctx, appID, []int64{drawn.Draw.Candidates[len(drawn.Draw.Candidates)-1].Number})
	if err != nil {
		t.Fatal(err)
	}
	if len(drawn.Draw.Candidates) > 1 && len(free) != 1 {
		t.Fatalf("other candidates should be released: %v", free)
	}
	// 同一张单不能再选
	if _, err := numbers.Change(ctx, appID, bob, strconv.FormatInt(drawn.Draw.Candidates[len(drawn.Draw.Candidates)-1].Number, 10), drawn.Draw.ID, "", 0); err == nil {
		t.Fatal("closed draw must be rejected")
	}

	// 保留号：用户拿不到、管理员可以指定
	reserved, err := numbers.Reserve(ctx, appID, []string{"66666", "abc", strconv.FormatInt(plain, 10)}, "活动奖品", nil, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if len(reserved.Reserved) != 1 || reserved.Reserved[0] != 66666 || len(reserved.Skipped) != 2 {
		t.Fatalf("reserve: %+v", reserved)
	}
	if _, err := numbers.Change(ctx, appID, alice, "66666", 0, "", 0); code(err) != errCodeUserNumberHeld && code(err) != errCodeUserNumberInvalid {
		t.Fatalf("reserved for users: %v", err)
	}
	if _, err := numbers.Assign(ctx, appID, bob, "66666", false, "", "admin"); code(err) != errCodeUserNumberHeld {
		t.Fatalf("assign reserved without force: %v", err)
	}
	h, err := numbers.Assign(ctx, appID, bob, "66666", true, "活动", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if h.Tier != usernumber.TierSupreme || h.Source != usernumber.SourceAdmin {
		t.Fatalf("assign: %+v", h)
	}
	// 不能抢别人正在用的号
	if _, err := numbers.Assign(ctx, appID, bob, strconv.FormatInt(plain, 10), true, "", "admin"); code(err) != errCodeUserNumberTaken {
		t.Fatalf("assign taken: %v", err)
	}

	// 回滚：回到指定之前的号
	back, err := numbers.Rollback(ctx, appID, bob, h.ID, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if back.NewNumber != pick.Number || back.Source != usernumber.SourceRollback {
		t.Fatalf("rollback: %+v", back)
	}

	// 删号：号码冻结，谁都拿不到
	bobNumber := back.NewNumber
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, bob); err != nil {
		t.Fatal(err)
	}
	avail, err := pg.CheckUserNumber(ctx, appID, bobNumber)
	if err != nil {
		t.Fatal(err)
	}
	if avail.Hold == nil || avail.Hold.Kind != usernumber.HoldFrozen || avail.Hold.UserID != nil {
		t.Fatalf("deleted user's number must be frozen: %+v", avail.Hold)
	}

	// 权益调整：扣到负数被拒
	if _, err := numbers.AdjustRight(ctx, appID, alice, usernumber.RightDraw, 0, -1, "", "admin"); code(err) != errCodeUserNumberBalance {
		t.Fatalf("adjust below zero: %v", err)
	}
	rights, err := numbers.AdjustRight(ctx, appID, alice, usernumber.RightDraw, 0, 3, "补偿", "admin")
	if err != nil || rights.Draw != 3 {
		t.Fatalf("adjust: %+v %v", rights, err)
	}

	// 流水对平：alice 的余额 = Σ流水
	ledger, _, err := numbers.AdminLedger(ctx, appID, alice, "", 1, 100)
	if err != nil {
		t.Fatal(err)
	}
	sum := map[string]int64{}
	for _, e := range ledger {
		sum[e.Kind+strconv.Itoa(e.Level)] += e.Delta
	}
	finalRights, err := pg.ListUserNumberRights(ctx, appID, alice)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range finalRights.Items {
		if sum[r.Kind+strconv.Itoa(r.Level)] != r.Balance {
			t.Fatalf("ledger mismatch %s/%d: %d vs %d", r.Kind, r.Level, sum[r.Kind+strconv.Itoa(r.Level)], r.Balance)
		}
	}

	overview, err := numbers.Overview(ctx, appID)
	if err != nil {
		t.Fatal(err)
	}
	if overview.Users != 1 || overview.Holds[usernumber.HoldReserved] != 0 || overview.Holds[usernumber.HoldFrozen] < 1 {
		t.Fatalf("overview: %+v", overview)
	}

	// 设置：默认位数 7 被拒；改成 10 位后新用户拿 10 位号
	settings := usernumber.DefaultSettings()
	settings.DefaultLength = 7
	if _, err := numbers.SaveSettings(ctx, appID, settings, "admin"); err == nil {
		t.Fatal("default length 7 must be rejected")
	}
	settings.DefaultLength = 10
	if _, err := numbers.SaveSettings(ctx, appID, settings, "admin"); err != nil {
		t.Fatal(err)
	}
	dave := insertTestUser(t, ctx, pool, appID, "un-dave", "0")
	rec, err := pg.GetUserNumber(ctx, appID, dave)
	if err != nil || len(strconv.FormatInt(rec.Number, 10)) != 10 {
		t.Fatalf("10-digit default: %+v %v", rec, err)
	}

	// 摇号次数用完
	if _, err := numbers.Draw(ctx, appID, dave); code(err) != errCodeUserNumberNoDraw {
		t.Fatalf("draw without right: %v", err)
	}
}
