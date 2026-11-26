package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"aegis/internal/domain/pointstore"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
)

// 积分商城「赠好友」的端到端集成测试（docs/points-store-gift.md）：真实 Postgres，跑全部迁移，走一遍
// 「赠送商品的保存校验 → 选人与核对 → 送达（同一事务扣积分、发给对方）→ 幂等 →
// 硬性条件（自己、查无此人、对方停用、对方已拥有、兑换方式不符）→ 风控拦截（新号、同设备、频繁送同一人、
// 每日次数与额度、对方每日接收）→ 进入审核（同网络、互送、新号接收）后通过与驳回 → 撤销追回（不退 / 退积分）→
// 并发下每日次数不被突破 → 双方看到的记录」。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestPointsStoreGiftIntegration
func TestPointsStoreGiftIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	vip := NewVipService(zap.NewNop(), pg)
	store := NewPointsStoreService(zap.NewNop(), pg)
	const appID = int64(30100)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'points-gift-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"ai_image", "voyage_ai"} {
		if _, err := vip.AdminSaveFeature(ctx, vipdomain.FeatureMutation{AppID: appID, Tag: tag, Name: ptrTo(tag)}); err != nil {
			t.Fatal(err)
		}
	}
	monthPlan := mustSavePlan(t, vip, vipdomain.PlanMutation{AppID: appID, Name: ptrTo("月度会员"),
		Kind: ptrTo(vipdomain.KindPaid), DurationDays: ptrTo(30), Features: &[]string{"ai_image"},
		Price: ptrTo(decimal.NewFromInt(10))})
	lifetimePlan := mustSavePlan(t, vip, vipdomain.PlanMutation{AppID: appID, Name: ptrTo("永久"),
		Kind: ptrTo(vipdomain.KindPaid), Lifetime: ptrTo(true), Price: ptrTo(decimal.NewFromInt(99))})
	if _, err := store.SaveSettings(ctx, appID, true, "admin"); err != nil {
		t.Fatal(err)
	}

	number := int64(10_000_000)
	newUser := func(account string, ageDays int, integral int64) int64 {
		t.Helper()
		id := insertTestUser(t, ctx, pool, appID, account, "0")
		number++
		if _, err := pool.Exec(ctx, `UPDATE users SET integral = $2, number = $3, created_at = NOW() - make_interval(days => $4) WHERE id = $1`,
			id, integral, number, ageDays); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO user_profiles (user_id, nickname) VALUES ($1, $2)
ON CONFLICT (user_id) DO UPDATE SET nickname = EXCLUDED.nickname`, id, account); err != nil {
			t.Fatal(err)
		}
		return id
	}
	numberOf := func(userID int64) string {
		var value int64
		if err := pool.QueryRow(ctx, `SELECT number FROM users WHERE id = $1`, userID).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return fmt.Sprint(value)
	}
	integralOf := func(userID int64) int64 {
		var value int64
		if err := pool.QueryRow(ctx, `SELECT integral FROM users WHERE id = $1`, userID).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	vipExpireOf := func(userID int64) *time.Time {
		var value *time.Time
		if err := pool.QueryRow(ctx, `SELECT vip_expire_at FROM users WHERE id = $1`, userID).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	activeUnlocks := func(userID int64) int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_feature_unlocks WHERE user_id = $1 AND revoked_at IS NULL
AND (ends_at IS NULL OR ends_at > NOW())`, userID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	blockedCount := func(senderID int64) int {
		var n int
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM points_store_gifts WHERE sender_id = $1 AND status = 'blocked'`, senderID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	savePolicy := func(mutate func(rules map[string]*pointstore.GiftRule)) {
		t.Helper()
		policy := pointstore.DefaultGiftPolicy(appID)
		rules := map[string]*pointstore.GiftRule{}
		for i := range policy.Rules {
			rules[policy.Rules[i].Key] = &policy.Rules[i]
		}
		mutate(rules)
		if _, err := store.SaveGiftPolicy(ctx, policy, "admin"); err != nil {
			t.Fatalf("保存赠送风控失败：%v", err)
		}
	}
	gift := func(senderID, itemID int64, recipient, requestID string) (*pointstore.GiftResult, error) {
		return store.Gift(ctx, GiftRequest{AppID: appID, SenderID: senderID, ItemID: itemID, Recipient: recipient,
			Message: "生日快乐", RequestID: requestID, ClientIP: "203.0.113.9"}, nil)
	}
	mustGift := func(senderID, itemID int64, recipient, requestID, wantStatus string) *pointstore.GiftResult {
		t.Helper()
		result, err := gift(senderID, itemID, recipient, requestID)
		if err != nil {
			t.Fatalf("赠送商品 %d 给 %s 失败：%v", itemID, recipient, err)
		}
		if result.Gift.Status != wantStatus {
			t.Fatalf("赠送状态应为 %s，得到 %+v", wantStatus, result.Gift)
		}
		return result
	}
	expectCode := func(err error, code int, what string) {
		t.Helper()
		if errCode(err) != code {
			t.Fatalf("%s 应报 %d，得到 %v", what, code, err)
		}
	}
	giftID := func(giftNo string) int64 {
		var id int64
		if err := pool.QueryRow(ctx, `SELECT id FROM points_store_gifts WHERE gift_no = $1`, giftNo).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}

	alice := newUser("gift-alice", 60, 100_000)
	bob := newUser("gift-bob", 60, 20_000)
	carol := newUser("gift-carol", 0, 20_000) // 新号
	dave := newUser("gift-dave", 60, 0)       // 与 alice 共用设备
	erin := newUser("gift-erin", 60, 0)       // 与 alice 同网络
	frank := newUser("gift-frank", 60, 0)     // 被停用
	gina := newUser("gift-gina", 60, 0)       // 已永久解锁 ai_image

	// ── 赠送商品只能配可追回的权益 ──
	createGift := func(input pointstore.ItemInput) (*pointstore.Item, error) {
		input.AppID, input.Delivery = appID, pointstore.DeliveryGift
		return store.CreateItem(ctx, input)
	}
	for name, rewards := range map[string][]pointstore.Reward{
		"余额":   {{Type: pointstore.RewardBalance, Money: decimal.NewFromInt(5)}},
		"人工发放": {{Type: pointstore.RewardManual, Text: "人工送"}},
		"经验":   {{Type: pointstore.RewardExperience, Amount: 100}},
		"永久套餐": {{Type: pointstore.RewardVipPlan, RefID: lifetimePlan.ID}},
	} {
		if _, err := createGift(pointstore.ItemInput{Title: "坏赠送", Cost: 100, Rewards: rewards}); errCode(err) != 40000 {
			t.Fatalf("赠送商品配「%s」应被拒，得到 %v", name, err)
		}
	}
	mustCreateGift := func(input pointstore.ItemInput) *pointstore.Item {
		t.Helper()
		item, err := createGift(input)
		if err != nil {
			t.Fatalf("新建赠送商品失败：%v", err)
		}
		if item.Delivery != pointstore.DeliveryGift {
			t.Fatalf("商品应为赠送商品，得到 %+v", item)
		}
		return item
	}
	vip7 := mustCreateGift(pointstore.ItemInput{Title: "赠好友会员 7 天", Cost: 800, Stock: ptrTo(int64(50)),
		Rewards: []pointstore.Reward{{Type: pointstore.RewardVipDays, Amount: 7}}})
	aiDual := mustCreateGift(pointstore.ItemInput{Title: "赠好友 AI 双享", Cost: 1500,
		Rewards: []pointstore.Reward{{Type: pointstore.RewardFeature, Tag: "ai_image", Amount: 30},
			{Type: pointstore.RewardFeature, Tag: "voyage_ai", Amount: 30}}})
	planGift := mustCreateGift(pointstore.ItemInput{Title: "赠好友月度会员", Cost: 3000, Stock: ptrTo(int64(10)),
		Rewards: []pointstore.Reward{{Type: pointstore.RewardVipPlan, RefID: monthPlan.ID}}})
	selfItem, err := store.CreateItem(ctx, pointstore.ItemInput{AppID: appID, Title: "会员 3 天", Cost: 100,
		Rewards: []pointstore.Reward{{Type: pointstore.RewardVipDays, Amount: 3}}})
	if err != nil {
		t.Fatal(err)
	}

	// ── 兑换方式不符 ──
	if _, err := store.Redeem(ctx, pointstore.RedeemInput{AppID: appID, UserID: alice, ItemID: vip7.ID}); errCode(err) != errCodeGiftItemMismatch {
		t.Fatalf("赠送商品走兑换应报 42254，得到 %v", err)
	}
	_, err = gift(alice, selfItem.ID, numberOf(bob), "")
	expectCode(err, errCodeGiftItemMismatch, "普通商品走赠送")

	page, err := store.Store(ctx, appID, alice)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range page.Items {
		want := pointstore.DeliverySelf
		if item.ID != selfItem.ID {
			want = pointstore.DeliveryGift
		}
		if item.Delivery != want || !item.Availability.Redeemable {
			t.Fatalf("商城里的商品 %s 应为 %s 且可兑，得到 %+v", item.Title, want, item)
		}
	}

	// ── 选人与核对 ──
	candidates, err := store.GiftCandidates(ctx, appID, alice, "", nil)
	if err != nil || len(candidates) != 0 {
		t.Fatalf("没有互动过时候选应为空，得到 %+v %v", candidates, err)
	}
	candidates, err = store.GiftCandidates(ctx, appID, alice, numberOf(bob), nil)
	if err != nil || len(candidates) != 1 || candidates[0].Nickname != "gift-bob" || candidates[0].PublicID == 0 ||
		candidates[0].UserID != 0 || candidates[0].Relation != "search" {
		t.Fatalf("按号码应找到 bob（只给对外 ID 与昵称），得到 %+v %v", candidates, err)
	}
	if mine, _ := store.GiftCandidates(ctx, appID, alice, numberOf(alice), nil); len(mine) != 0 {
		t.Fatalf("搜自己的号码不应出现在候选里，得到 %+v", mine)
	}
	preview, err := store.PreviewGift(ctx, GiftRequest{AppID: appID, SenderID: alice, ItemID: vip7.ID, Recipient: numberOf(bob)}, nil)
	if err != nil || !preview.Allowed || preview.Recipient.Nickname != "gift-bob" || len(preview.Rewards) != 1 {
		t.Fatalf("核对应允许并给出对方与权益，得到 %+v %v", preview, err)
	}
	if integralOf(alice) != 100_000 {
		t.Fatal("核对不应扣积分")
	}

	// ── 硬性条件 ──
	_, err = gift(alice, vip7.ID, numberOf(alice), "")
	expectCode(err, errCodeGiftSelf, "送给自己")
	_, err = gift(alice, vip7.ID, "99999999999", "")
	expectCode(err, errCodeGiftRecipientNotFound, "查无此人")
	if _, err := pool.Exec(ctx, `UPDATE users SET enabled = FALSE WHERE id = $1`, frank); err != nil {
		t.Fatal(err)
	}
	_, err = gift(alice, vip7.ID, numberOf(frank), "")
	expectCode(err, errCodeGiftRecipientInactive, "对方被停用")
	preview, err = store.PreviewGift(ctx, GiftRequest{AppID: appID, SenderID: alice, ItemID: vip7.ID, Recipient: numberOf(frank)}, nil)
	if err != nil || preview.Allowed || preview.Reason != pointstore.GiftReasonRecipientInactive {
		t.Fatalf("核对应说明对方状态异常，得到 %+v %v", preview, err)
	}
	if _, err := store.GrantUnlock(ctx, pointstore.UnlockGrantInput{AppID: appID, UserID: gina, Tag: "ai_image", Days: 0, Operator: "admin"}); err != nil {
		t.Fatal(err)
	}
	_, err = gift(alice, aiDual.ID, numberOf(gina), "")
	expectCode(err, errCodeGiftRecipientOwned, "对方已永久拥有")

	// ── 送达：同一事务扣积分、发给对方 ──
	delivered := mustGift(alice, vip7.ID, numberOf(bob), "req-1", pointstore.GiftDelivered)
	if delivered.Balance != 100_000-800 || integralOf(alice) != 99_200 || integralOf(bob) != 20_000 {
		t.Fatalf("积分应只从赠送人扣，得到 balance=%d alice=%d bob=%d", delivered.Balance, integralOf(alice), integralOf(bob))
	}
	if expire := vipExpireOf(bob); expire == nil || expire.Sub(time.Now()) < 6*24*time.Hour {
		t.Fatalf("bob 应收到 7 天会员，到期 %v", expire)
	}
	if vipExpireOf(alice) != nil {
		t.Fatal("会员不应发到赠送人身上")
	}
	if delivered.Gift.Recipient.Nickname != "gift-bob" || delivered.Gift.Recipient.UserID != 0 || delivered.Gift.Direction != "sent" {
		t.Fatalf("用户端的赠送记录应只给对方的公开信息，得到 %+v", delivered.Gift.Recipient)
	}
	replay := mustGift(alice, vip7.ID, numberOf(bob), "req-1", pointstore.GiftDelivered)
	if !replay.Replayed || replay.Gift.GiftNo != delivered.Gift.GiftNo || integralOf(alice) != 99_200 {
		t.Fatalf("同一 requestId 应原样回放，得到 %+v", replay)
	}
	redemptions, err := store.MyRedemptions(ctx, appID, alice, 1, 20)
	if err != nil || len(redemptions.Items) != 1 || redemptions.Items[0].GiftNo != delivered.Gift.GiftNo {
		t.Fatalf("兑换记录应带赠送单号，得到 %+v %v", redemptions, err)
	}
	if candidates, _ := store.GiftCandidates(ctx, appID, alice, "", nil); len(candidates) != 1 || candidates[0].Relation != "recent_sent" {
		t.Fatalf("送过之后 bob 应出现在最近互动里，得到 %+v", candidates)
	}

	// ── 风控拦截：频繁送同一人（默认 24 小时 1 次）──
	_, err = gift(alice, aiDual.ID, numberOf(bob), "")
	expectCode(err, errCodeGiftFrequency, "24 小时内再送同一人")
	if blockedCount(alice) != 1 || integralOf(alice) != 99_200 {
		t.Fatalf("拦截应留痕且不扣积分，blocked=%d integral=%d", blockedCount(alice), integralOf(alice))
	}

	// ── 风控拦截：同设备（默认拒绝）──
	for _, uid := range []int64{alice, dave} {
		if _, err := pool.Exec(ctx, `INSERT INTO user_login_devices (appid, user_id, device_key) VALUES ($1, $2, 'shared-device-0001')`, appID, uid); err != nil {
			t.Fatal(err)
		}
	}
	// UA 摘要不是设备：同款机型的摘要相同，不能算同一台
	for _, uid := range []int64{alice, erin} {
		if _, err := pool.Exec(ctx, `INSERT INTO user_login_devices (appid, user_id, device_key) VALUES ($1, $2, 'ua:0011223344556677')`, appID, uid); err != nil {
			t.Fatal(err)
		}
	}
	_, err = gift(alice, vip7.ID, numberOf(dave), "")
	expectCode(err, errCodeGiftRisk, "同设备")

	// ── 进入审核：同网络（默认审核）→ 驳回退积分 ──
	for _, uid := range []int64{alice, erin} {
		if _, err := pool.Exec(ctx, `INSERT INTO login_audit_logs (user_id, appid, login_type, login_ip, status)
VALUES ($1, $2, 'password', '198.51.100.7', 'success')`, uid, appID); err != nil {
			t.Fatal(err)
		}
	}
	// 内网地址不算同一网络
	for _, uid := range []int64{alice, bob} {
		if _, err := pool.Exec(ctx, `INSERT INTO login_audit_logs (user_id, appid, login_type, login_ip, status)
VALUES ($1, $2, 'password', '10.0.0.5', 'success')`, uid, appID); err != nil {
			t.Fatal(err)
		}
	}
	reviewing := mustGift(alice, vip7.ID, numberOf(erin), "req-erin", pointstore.GiftReviewing)
	if integralOf(alice) != 99_200-800 || vipExpireOf(erin) != nil {
		t.Fatal("审核中应已扣积分、尚未发放")
	}
	detail, err := store.GetGift(ctx, appID, giftID(reviewing.Gift.GiftNo))
	if err != nil || detail.RiskAction != pointstore.GiftActionReview || len(detail.RiskHits) != 1 ||
		detail.RiskHits[0].Key != pointstore.GiftRuleSameNetwork || detail.RiskFacts == nil || len(detail.RiskFacts.SharedNetworks) != 1 {
		t.Fatalf("管理端应看到同网络命中与事实，得到 %+v %v", detail, err)
	}
	// 待处理的兑换不能在兑换记录里「标记发放」：那样东西发不到接收人手上
	_, err = store.Fulfill(ctx, pointstore.HandleInput{AppID: appID, RedemptionID: *detail.RedemptionID, Operator: "admin"})
	expectCode(err, errCodeRedemptionIsGift, "在兑换记录里处理赠送")
	stockBefore := itemStock(t, ctx, pg, appID, vip7.ID)
	rejected, err := store.RejectGift(ctx, pointstore.GiftReviewInput{AppID: appID, GiftID: detail.ID, Note: "同网络多账号", Operator: "admin"})
	if err != nil || rejected.Status != pointstore.GiftRejected || rejected.RefundTransactionNo == "" {
		t.Fatalf("驳回失败：%+v %v", rejected, err)
	}
	if integralOf(alice) != 99_200 || itemStock(t, ctx, pg, appID, vip7.ID) != stockBefore+1 || vipExpireOf(erin) != nil {
		t.Fatal("驳回应退回积分、加回库存、不发放")
	}
	_, err = store.ApproveGift(ctx, pointstore.GiftReviewInput{AppID: appID, GiftID: detail.ID, Operator: "admin"})
	expectCode(err, errCodeGiftState, "驳回后再通过")

	// ── 进入审核：新号接收（默认审核）→ 通过后发放 ──
	reviewCarol := mustGift(alice, planGift.ID, numberOf(carol), "req-carol", pointstore.GiftReviewing)
	approved, err := store.ApproveGift(ctx, pointstore.GiftReviewInput{AppID: appID, GiftID: giftID(reviewCarol.Gift.GiftNo), Operator: "admin"})
	if err != nil || approved.Status != pointstore.GiftDelivered || len(approved.Results) != 1 || approved.Results[0].TransactionNo == "" {
		t.Fatalf("通过后应发放并记下流水号，得到 %+v %v", approved, err)
	}
	if expire := vipExpireOf(carol); expire == nil || expire.Sub(time.Now()) < 29*24*time.Hour {
		t.Fatalf("carol 应收到 30 天会员，到期 %v", expire)
	}

	// ── 风控拦截：新号赠送（默认拒绝）──
	_, err = gift(carol, vip7.ID, numberOf(bob), "")
	expectCode(err, errCodeGiftRisk, "新号赠送")

	// ── 进入审核：互送（bob 回赠 alice）→ 通过 ──
	mutual := mustGift(bob, aiDual.ID, numberOf(alice), "req-mutual", pointstore.GiftReviewing)
	mutualDetail, _ := store.GetGift(ctx, appID, giftID(mutual.Gift.GiftNo))
	if mutualDetail.RiskHits[0].Key != pointstore.GiftRuleMutual {
		t.Fatalf("应命中互相赠送，得到 %+v", mutualDetail.RiskHits)
	}
	if _, err := store.ApproveGift(ctx, pointstore.GiftReviewInput{AppID: appID, GiftID: mutualDetail.ID, Operator: "admin"}); err != nil {
		t.Fatal(err)
	}
	if activeUnlocks(alice) != 2 {
		t.Fatalf("alice 应收到两项功能解锁，得到 %d", activeUnlocks(alice))
	}

	// ── 每日次数（alice 今日已计：送达 bob、送达 carol，共 2；驳回的不算）──
	savePolicy(func(rules map[string]*pointstore.GiftRule) { rules[pointstore.GiftRuleSenderDailyCount].Threshold = 2 })
	other := newUser("gift-other", 60, 0)
	_, err = gift(alice, vip7.ID, numberOf(other), "")
	expectCode(err, errCodeGiftLimit, "每日赠送次数")
	// 每日额度
	savePolicy(func(rules map[string]*pointstore.GiftRule) {
		rules[pointstore.GiftRuleSenderDailyPoints].Threshold = 4000
	})
	_, err = gift(alice, vip7.ID, numberOf(other), "")
	expectCode(err, errCodeGiftLimit, "每日赠送额度（已送 3800 + 800）")
	// 对方每日接收：bob、alice 都给 other 送过之后 other 再收第二份
	savePolicy(func(rules map[string]*pointstore.GiftRule) {
		rules[pointstore.GiftRuleRecipientDailyCount].Threshold = 1
	})
	mustGift(bob, vip7.ID, numberOf(other), "", pointstore.GiftDelivered)
	_, err = gift(alice, vip7.ID, numberOf(other), "")
	expectCode(err, errCodeGiftLimit, "对方每日接收次数")

	// ── 策略校验 ──
	bad := pointstore.DefaultGiftPolicy(appID)
	bad.Rules[0].Action = "ban"
	if _, err := store.SaveGiftPolicy(ctx, bad, "admin"); errCode(err) != 40000 {
		t.Fatalf("未知处置应被拒，得到 %v", err)
	}
	savePolicy(func(rules map[string]*pointstore.GiftRule) {})

	// ── 撤销追回：不退积分 ──
	bobExpire := vipExpireOf(bob)
	revoked, err := store.RevokeGift(ctx, pointstore.GiftReviewInput{AppID: appID, GiftID: giftID(delivered.Gift.GiftNo),
		Note: "套利", Operator: "admin"})
	if err != nil || revoked.Status != pointstore.GiftRevoked || len(revoked.Revocation) != 1 ||
		revoked.Revocation[0].Outcome != pointstore.RevokeRecovered || revoked.RefundTransactionNo != "" {
		t.Fatalf("撤销失败：%+v %v", revoked, err)
	}
	if expire := vipExpireOf(bob); expire == nil || bobExpire.Sub(*expire) < 6*24*time.Hour {
		t.Fatalf("bob 的会员应被追回 7 天，撤销前 %v 撤销后 %v", bobExpire, expire)
	}
	if integralOf(alice) != 99_200-3000 {
		t.Fatalf("不退积分时赠送人的积分不变，得到 %d", integralOf(alice))
	}
	var redemptionStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM points_store_redemptions WHERE id = $1`, *revoked.RedemptionID).Scan(&redemptionStatus); err != nil ||
		redemptionStatus != pointstore.RedemptionRevoked {
		t.Fatalf("不退积分时兑换记为已撤销，得到 %s %v", redemptionStatus, err)
	}
	_, err = store.RevokeGift(ctx, pointstore.GiftReviewInput{AppID: appID, GiftID: revoked.ID, Operator: "admin"})
	expectCode(err, errCodeGiftState, "重复撤销")

	// ── 撤销追回：退积分（bob 送 alice 的功能解锁）──
	bobBefore := integralOf(bob)
	refunded, err := store.RevokeGift(ctx, pointstore.GiftReviewInput{AppID: appID, GiftID: mutualDetail.ID, RefundSender: true, Operator: "admin"})
	if err != nil || refunded.RefundTransactionNo == "" || len(refunded.Revocation) != 2 {
		t.Fatalf("退积分撤销失败：%+v %v", refunded, err)
	}
	if activeUnlocks(alice) != 0 || integralOf(bob) != bobBefore+1500 {
		t.Fatalf("功能解锁应收回、积分应退给 bob，unlocks=%d bob=%d", activeUnlocks(alice), integralOf(bob))
	}

	// ── 双方看到的记录 ──
	received, err := store.MyGifts(ctx, appID, bob, "received", 1, 20, nil)
	if err != nil || len(received.Items) != 1 || received.Items[0].Status != pointstore.GiftRevoked ||
		received.Items[0].Sender.Nickname != "gift-alice" || received.Items[0].Cost != 0 || received.Items[0].Direction != "received" {
		t.Fatalf("bob 收到的应只有那笔被撤销的（看得到谁送的、看不到花了多少），得到 %+v %v", received, err)
	}
	erinBox, _ := store.MyGifts(ctx, appID, erin, "received", 1, 20, nil)
	if len(erinBox.Items) != 0 {
		t.Fatalf("被驳回的赠送接收人不该看到，得到 %+v", erinBox.Items)
	}
	sent, _ := store.MyGifts(ctx, appID, alice, "sent", 1, 20, nil)
	for _, item := range sent.Items {
		if item.Status == pointstore.GiftBlocked {
			t.Fatal("被拦下的尝试不出现在用户的记录里")
		}
	}
	list, err := store.ListGifts(ctx, pointstore.GiftQuery{AppID: appID, Status: pointstore.GiftBlocked})
	if err != nil || list["summary"].(*pointstore.GiftSummary).Blocked < 6 {
		t.Fatalf("管理端应看到全部拦截，得到 %+v %v", list["summary"], err)
	}

	// ── 并发：同一个人同时送 6 个人，每日上限 2，恰好 2 笔成功 ──
	sender := newUser("gift-rush", 60, 100_000)
	savePolicy(func(rules map[string]*pointstore.GiftRule) { rules[pointstore.GiftRuleSenderDailyCount].Threshold = 2 })
	recipients := make([]string, 6)
	for i := range recipients {
		recipients[i] = numberOf(newUser(fmt.Sprintf("gift-rush-%d", i), 60, 0))
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	okCount, limitCount := 0, 0
	for i := range recipients {
		wg.Add(1)
		go func(recipient string) {
			defer wg.Done()
			_, err := gift(sender, vip7.ID, recipient, "")
			mu.Lock()
			defer mu.Unlock()
			switch errCode(err) {
			case 0:
				okCount++
			case errCodeGiftLimit:
				limitCount++
			default:
				t.Errorf("并发赠送意外失败：%v", err)
			}
		}(recipients[i])
	}
	wg.Wait()
	if okCount != 2 || limitCount != 4 || integralOf(sender) != 100_000-1600 {
		t.Fatalf("并发下应恰好 2 笔成功，得到 ok=%d limit=%d integral=%d", okCount, limitCount, integralOf(sender))
	}
}

func itemStock(t *testing.T, ctx context.Context, pg *pgrepo.Repository, appID, itemID int64) int64 {
	t.Helper()
	item, err := pg.GetPointsStoreItem(ctx, appID, itemID)
	if err != nil || item.Stock == nil {
		t.Fatalf("读库存失败：%+v %v", item, err)
	}
	return *item.Stock
}
