package service

import (
	"strings"
	"testing"
	"time"

	aidomain "aegis/internal/domain/ai"
	"aegis/internal/domain/aigateway"
	authdomain "aegis/internal/domain/auth"
	pgrepo "aegis/internal/repository/postgres"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// TestAIGatewayCommerceIntegration 额度包与计费方案的端到端（docs/ai-billing.md）：
// 积分来源被拒、额度包报价与购买（价格校验、幂等、编号挪用）、先到期的批次先用与跨批次结算、
// 调用退款退回原批次 / 过期批次退成永久额度、到期清理、按剩余比例退额度包、新人礼、
// 时段价格与会员折扣进调用记录、会员专享包、管理员发放与作废、经营统计、全局不变式。
//
// AEGIS_TEST_PG_DSN=postgres://…/aegis_aig_test go test ./internal/service -run TestAIGatewayCommerceIntegration
func TestAIGatewayCommerceIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	log := zap.NewNop()
	ai := NewAIProviderService(log, pg, "ai-gateway-test-master-key")
	gw := NewAIGatewayService(log, pg, nil, ai)
	upstream := newFakeAIUpstream(t)

	const appID = int64(31100)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'ai-commerce-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	if _, err := ai.Save(ctx, aidomain.ConfigMutation{AppID: appID, Name: ptrTo("fake"),
		Provider: ptrTo(aidomain.ProviderCustomOpenAI), Enabled: ptrTo(true),
		Settings: map[string]string{aidomain.KeyBaseURL: upstream.server.URL + "/v1"},
		Secrets:  map[string]string{aidomain.KeyAPIKey: "upstream-secret"}}); err != nil {
		t.Fatalf("建通道失败：%v", err)
	}
	dec := decimal.RequireFromString

	settings := aigateway.DefaultSettings()
	settings.Enabled = true
	settings.Sources = []string{aigateway.SourcePack, aigateway.SourceCredit, aigateway.SourceWallet}
	settings.WalletRate = dec("0.01")
	settings.RPM, settings.MaxConcurrent, settings.DefaultMaxTokens = 0, 100, 1000
	settings.Welcome = aigateway.WelcomeGift{Enabled: true, Amount: dec("5"), ValidityDays: 7}
	saveSettings := func(mutate func(*aigateway.Settings)) {
		t.Helper()
		next := settings
		mutate(&next)
		saved, err := gw.SaveSettings(ctx, appID, next, "test")
		if err != nil {
			t.Fatalf("保存设置失败：%v", err)
		}
		settings = *saved
	}
	saveSettings(func(*aigateway.Settings) {})

	// ── 积分已不能做扣费来源 ──
	bad := settings
	bad.Sources = []string{aigateway.SourceCredit, aigateway.SourceIntegral}
	if _, err := gw.SaveSettings(ctx, appID, bad, "test"); errCode(err) != 40219 {
		t.Fatalf("选积分做来源应返回 40219，得到 %v", err)
	}

	// 1.5 点 / 次（500 输入 × 2000 + 125 输出 × 4000 微点）
	if _, err := gw.CreateModel(ctx, appID, aigateway.ModelInput{Model: "chat", UpstreamModel: "up-ok", Kind: aigateway.KindChat,
		InputPrice: decimal.NewFromInt(2000), OutputPrice: decimal.NewFromInt(4000)}); err != nil {
		t.Fatal(err)
	}
	prompt := strings.Repeat("a", 1500)
	newUser := func(account, wallet string) (int64, *authdomain.Session, *AIGatewayPrincipal) {
		t.Helper()
		userID := insertTestUser(t, ctx, pool, appID, account, wallet)
		session := &authdomain.Session{AppID: appID, UserID: userID}
		created, err := gw.CreateKey(ctx, session, aigateway.KeyInput{Name: ptrTo("test")})
		if err != nil {
			t.Fatal(err)
		}
		principal, err := gw.Authenticate(ctx, created.Secret, "127.0.0.1", "go-test")
		if err != nil {
			t.Fatal(err)
		}
		return userID, session, principal
	}
	// 每次调用前重新鉴权：调用者带的是鉴权那一刻的设置（线上每个请求都重新鉴权）。
	chat := func(stale *AIGatewayPrincipal) *AIGatewayChatResult {
		t.Helper()
		principal := *stale
		current, err := gw.GetSettings(ctx, appID)
		if err != nil {
			t.Fatal(err)
		}
		principal.Settings = *current
		result, err := gw.RelayChat(ctx, &principal, AIGatewayChatInput{Endpoint: aigateway.EndpointChat,
			Request: aidomain.ChatRequest{Model: "chat", MaxTokens: 125,
				Messages: []aidomain.ChatMessage{aidomain.TextMessage(aidomain.RoleUser, prompt)}}}, nil)
		if err != nil {
			t.Fatalf("调用失败：%v", err)
		}
		return result
	}
	lotOf := func(id int64) (remaining, frozen int64, status string) {
		t.Helper()
		if err := pool.QueryRow(ctx, `SELECT remaining, frozen, status FROM ai_gateway_credit_lots WHERE id = $1`, id).
			Scan(&remaining, &frozen, &status); err != nil {
			t.Fatal(err)
		}
		return
	}
	walletOf := func(userID int64) string {
		t.Helper()
		var balance string
		if err := pool.QueryRow(ctx, `SELECT balance::text FROM user_wallets WHERE user_id = $1`, userID).Scan(&balance); err != nil {
			t.Fatal(err)
		}
		return balance
	}
	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, query, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// ── 额度包 ──
	pkgA, err := gw.CreatePackage(ctx, appID, aigateway.PackageInput{Name: "标准包", Points: dec("10"), BonusPoints: dec("2"),
		Price: dec("1.00"), OriginalPrice: dec("1.50"), ValidityDays: 30, PerUserLimit: 3, Tone: "violet", Badge: "热销"})
	if err != nil {
		t.Fatalf("建额度包失败：%v", err)
	}
	pkgB, err := gw.CreatePackage(ctx, appID, aigateway.PackageInput{Name: "日包", Points: dec("5"), Price: dec("0.50"), ValidityDays: 1})
	if err != nil {
		t.Fatal(err)
	}
	pkgVip, err := gw.CreatePackage(ctx, appID, aigateway.PackageInput{Name: "会员包", Points: dec("100"), Price: dec("5.00"),
		Audience: aigateway.AudienceVip})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gw.CreatePackage(ctx, appID, aigateway.PackageInput{Name: "坏包", Points: dec("1.5"), Price: dec("1")}); errCode(err) != 40219 {
		t.Fatalf("非整数额度应 40219，得到 %v", err)
	}

	alice, aliceSession, alicePrincipal := newUser("cm-alice", "5.00")
	store, err := gw.Store(ctx, aliceSession)
	if err != nil || !store.Enabled || len(store.Items) != 3 {
		t.Fatalf("商店应列出 3 个包：%+v %v", store, err)
	}
	for _, item := range store.Items {
		if item.ID == pkgVip.ID && (item.Purchasable || item.Reason != aigateway.ReasonMembersOnly) {
			t.Fatalf("非会员看会员包应不可买（members_only）：%+v", item)
		}
		if item.ID == pkgA.ID && (!item.Purchasable || !item.FinalPrice.Equal(dec("1")) || !item.TotalPoints.Equal(dec("12")) || !item.Affordable) {
			t.Fatalf("标准包报价不对：%+v", item)
		}
	}
	if _, err := gw.Purchase(ctx, aliceSession, pkgVip.ID, "req-vip-00001", nil, ""); errCode(err) != 40243 {
		t.Fatalf("非会员买会员包应 40243，得到 %v", err)
	}
	wrong := dec("0.99")
	if _, err := gw.Purchase(ctx, aliceSession, pkgA.ID, "req-a-000001", &wrong, ""); errCode(err) != 40236 {
		t.Fatalf("价格不符应 40236，得到 %v", err)
	}
	if _, err := gw.Purchase(ctx, aliceSession, pkgA.ID, "short", nil, ""); errCode(err) != 40219 {
		t.Fatalf("过短的 requestId 应 40219，得到 %v", err)
	}
	right := dec("1.00")
	boughtA, err := gw.Purchase(ctx, aliceSession, pkgA.ID, "req-a-000001", &right, "127.0.0.1")
	if err != nil || boughtA.Replayed || !boughtA.Lot.Amount.Equal(dec("12")) || !boughtA.Wallet.Equal(dec("4")) ||
		boughtA.Lot.ExpiresAt == nil || boughtA.Order.Status != aigateway.OrderPaid || !boughtA.Balance.Equal(dec("12")) {
		t.Fatalf("买标准包结果不对：%+v %v", boughtA, err)
	}
	replay, err := gw.Purchase(ctx, aliceSession, pkgA.ID, "req-a-000001", &right, "")
	if err != nil || !replay.Replayed || replay.Order.OrderNo != boughtA.Order.OrderNo || walletOf(alice) != "4.00" {
		t.Fatalf("同一 requestId 应重放原单、不再扣钱：%+v %v 余额 %s", replay, err, walletOf(alice))
	}
	if _, err := gw.Purchase(ctx, aliceSession, pkgB.ID, "req-a-000001", nil, ""); errCode(err) != 40242 {
		t.Fatalf("编号挪用到别的包应 40242，得到 %v", err)
	}
	boughtB, err := gw.Purchase(ctx, aliceSession, pkgB.ID, "req-b-000001", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	lotA, lotB := boughtA.Lot.ID, boughtB.Lot.ID
	if count(`SELECT COUNT(*) FROM wallet_transactions WHERE user_id = $1 AND type = 'ai_package'`, alice) != 2 {
		t.Fatal("两次购买应恰好两条 ai_package 钱包流水")
	}

	// ── 先到期的批次先用；跨批次结算 ──
	first := chat(alicePrincipal)
	if len(first.Record.Charges) != 1 || first.Record.Charges[0].Source != aigateway.SourcePack ||
		len(first.Record.Charges[0].Lots) != 1 || first.Record.Charges[0].Lots[0].LotID != lotB {
		t.Fatalf("应先扣 1 天后到期的日包：%+v", first.Record.Charges)
	}
	chat(alicePrincipal)
	chat(alicePrincipal) // 日包剩 0.5
	spanning := chat(alicePrincipal)
	parts := spanning.Record.Charges[0].Lots
	if len(parts) != 2 || parts[0].LotID != lotB || parts[0].Micro != 500_000 || parts[1].LotID != lotA || parts[1].Micro != 1_000_000 {
		t.Fatalf("跨批次应拆成日包 0.5 + 标准包 1.0：%+v", parts)
	}
	if remaining, frozen, _ := lotOf(lotB); remaining != 0 || frozen != 0 {
		t.Fatalf("日包应用完：%d / %d", remaining, frozen)
	}
	var totalAfter int64
	if err := pool.QueryRow(ctx, `SELECT total_after FROM ai_gateway_credit_ledger WHERE user_id = $1 AND bucket = 'pack' AND kind = 'consume'
ORDER BY id DESC LIMIT 1`, alice).Scan(&totalAfter); err != nil || totalAfter != 11_000_000 {
		t.Fatalf("最后一条消费流水的合计应为 11 点：%d %v", totalAfter, err)
	}
	if count(`SELECT COUNT(*) FROM ai_gateway_credit_ledger WHERE user_id = $1 AND bucket = 'pack' AND kind = 'consume'`, alice) != 4 {
		t.Fatal("四次调用应各一条额度包消费流水")
	}

	// ── 调用退款：退回原批次 ──
	if _, err := gw.RefundRequest(ctx, aigateway.RefundInput{AppID: appID, RequestNo: spanning.RequestNo, Operator: "tester"}); err != nil {
		t.Fatal(err)
	}
	if remaining, _, _ := lotOf(lotB); remaining != 500_000 {
		t.Fatalf("日包应退回 0.5：%d", remaining)
	}
	if remaining, _, _ := lotOf(lotA); remaining != 12_000_000 {
		t.Fatalf("标准包应退回到 12：%d", remaining)
	}

	// ── 到期清理；过期批次的调用退款退成永久额度 ──
	if _, err := pool.Exec(ctx, `UPDATE ai_gateway_credit_lots SET expires_at = NOW() - INTERVAL '1 minute' WHERE id = $1`, lotB); err != nil {
		t.Fatal(err)
	}
	if swept, err := pg.SweepAIGatewayLots(ctx, time.Now(), 100); err != nil || swept != 1 {
		t.Fatalf("应清理 1 个到期批次：%d %v", swept, err)
	}
	if remaining, _, status := lotOf(lotB); remaining != 0 || status != aigateway.LotExpired {
		t.Fatalf("到期批次应清零并标记 expired：%d %s", remaining, status)
	}
	if count(`SELECT COUNT(*) FROM ai_gateway_credit_ledger WHERE lot_id = $1 AND kind = 'expire' AND amount = -500000`, lotB) != 1 {
		t.Fatal("到期清零应写一条 expire 流水")
	}
	if swept, _ := pg.SweepAIGatewayLots(ctx, time.Now(), 100); swept != 0 {
		t.Fatal("清理应可重复执行、不重复写流水")
	}
	if _, err := gw.RefundRequest(ctx, aigateway.RefundInput{AppID: appID, RequestNo: first.RequestNo, Operator: "tester"}); err != nil {
		t.Fatal(err)
	}
	overview, err := gw.UserOverview(ctx, appID, alice)
	if err != nil || !overview.Credit.Equal(dec("1.5")) || !overview.Pack.Equal(dec("12")) || !overview.Balance.Equal(dec("13.5")) {
		t.Fatalf("过期批次的退款应进永久额度 1.5，额度包 12：%+v %v", overview, err)
	}

	// ── 按剩余比例退额度包 ──
	chat(alicePrincipal) // 标准包先于永久额度：12 → 10.5
	chat(alicePrincipal) // → 9
	orders, err := gw.ListOrders(ctx, aigateway.OrderQuery{AppID: appID, UserID: alice})
	if err != nil || orders.Total != 2 {
		t.Fatalf("应有两笔订单：%+v %v", orders, err)
	}
	for _, order := range orders.Items {
		if order.OrderNo == boughtA.Order.OrderNo && (order.RefundEstimate == nil || !order.RefundEstimate.Equal(dec("0.75"))) {
			t.Fatalf("预计退款应为 0.75：%+v", order)
		}
	}
	refundedOrder, err := gw.RefundOrder(ctx, aigateway.OrderRefundInput{AppID: appID, OrderNo: boughtA.Order.OrderNo, Reason: "测试", Operator: "tester"})
	if err != nil || refundedOrder.Status != aigateway.OrderRefunded || !refundedOrder.RefundAmount.Equal(dec("0.75")) ||
		!refundedOrder.RefundPoints.Equal(dec("9")) {
		t.Fatalf("剩 9/12 应退 0.75 元：%+v %v", refundedOrder, err)
	}
	if walletOf(alice) != "4.25" {
		t.Fatalf("退款后余额应为 3.50 + 0.75 = 4.25，得到 %s", walletOf(alice))
	}
	if _, _, status := lotOf(lotA); status != aigateway.LotRevoked {
		t.Fatal("退款后批次应作废")
	}
	if _, err := gw.RefundOrder(ctx, aigateway.OrderRefundInput{AppID: appID, OrderNo: boughtA.Order.OrderNo}); errCode(err) != 40238 {
		t.Fatalf("重复退款应 40238，得到 %v", err)
	}
	if err := gw.DeletePackage(ctx, appID, pkgA.ID); errCode(err) != 40234 {
		t.Fatalf("有订单的包不能删，应 40234，得到 %v", err)
	}

	// ── 新人礼 ──
	welcome, err := gw.ClaimWelcome(ctx, aliceSession)
	if err != nil || welcome.Lot.Origin != aigateway.LotWelcome || !welcome.Lot.Amount.Equal(dec("5")) || welcome.Lot.ExpiresAt == nil {
		t.Fatalf("领新人礼失败：%+v %v", welcome, err)
	}
	if _, err := gw.ClaimWelcome(ctx, aliceSession); errCode(err) != 40240 {
		t.Fatalf("重复领取应 40240，得到 %v", err)
	}
	if overview, _ := gw.UserOverview(ctx, appID, alice); overview.Welcome.Claimable || overview.Welcome.ClaimedAt == nil {
		t.Fatalf("领过之后不可再领：%+v", overview.Welcome)
	}

	// ── 时段价格与会员折扣进调用记录 ──
	saveSettings(func(s *aigateway.Settings) {
		s.TimePricing = aigateway.TimePricing{Enabled: true, Windows: []aigateway.TimeWindow{
			{ID: "all", Name: "全天半价", Tone: aigateway.TonePromo, Start: "00:00", End: "00:00", Multiplier: dec("0.5"), Enabled: true},
		}}
		s.VipMultiplier = dec("0.8")
		s.VipPackDiscount = dec("0.9")
		s.VipPackBonusRate = dec("0.1")
	})
	halfPrice := chat(alicePrincipal)
	if halfPrice.Record.CostMicro != 750_000 || halfPrice.Record.Billing.Pricing == nil ||
		halfPrice.Record.Billing.Pricing.Window == nil || halfPrice.Record.Billing.Pricing.Window.ID != "all" {
		t.Fatalf("全天半价应只收 0.75 点并记下时段：%+v", halfPrice.Record)
	}
	bob, bobSession, bobPrincipal := newUser("cm-bob", "10.00")
	if _, err := pool.Exec(ctx, `UPDATE users SET vip_expire_at = NOW() + INTERVAL '30 days' WHERE id = $1`, bob); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.GrantLot(ctx, aigateway.LotGrantInput{AppID: appID, UserID: bob, Amount: dec("20"), ValidityDays: 3,
		Title: "补偿", RequestID: "grant-bob-0001", Operator: "tester"}); err != nil {
		t.Fatalf("发放限期额度失败：%v", err)
	}
	again, err := gw.GrantLot(ctx, aigateway.LotGrantInput{AppID: appID, UserID: bob, Amount: dec("20"), ValidityDays: 3,
		RequestID: "grant-bob-0001", Operator: "tester"})
	if err != nil || count(`SELECT COUNT(*) FROM ai_gateway_credit_lots WHERE user_id = $1`, bob) != 1 {
		t.Fatalf("同一 requestId 的发放只生效一次：%+v %v", again, err)
	}
	vipCall := chat(bobPrincipal)
	if vipCall.Record.CostMicro != 600_000 || !vipCall.Record.Factor.Equal(dec("0.4")) {
		t.Fatalf("会员 0.8 × 时段 0.5 = 0.4，应收 0.6 点：%+v", vipCall.Record)
	}
	saveSettings(func(s *aigateway.Settings) { s.TimePricing.Stacking = aigateway.StackBest })
	if best := chat(bobPrincipal); best.Record.CostMicro != 750_000 {
		t.Fatalf("取更优惠时只用 0.5 的时段倍率，应收 0.75 点：%d", best.Record.CostMicro)
	}
	vipStore, err := gw.Store(ctx, bobSession)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range vipStore.Items {
		if item.ID == pkgA.ID && (!item.FinalPrice.Equal(dec("0.9")) || !item.MemberBonusPoints.Equal(dec("1")) ||
			!item.TotalPoints.Equal(dec("13")) || item.DiscountLabel == "") {
			t.Fatalf("会员买标准包应 9 折、多送 1 点：%+v", item)
		}
		if item.ID == pkgVip.ID && !item.Purchasable {
			t.Fatalf("会员应能买会员包：%+v", item)
		}
	}
	if _, err := gw.Purchase(ctx, bobSession, pkgVip.ID, "req-vip-bob-01", nil, ""); err != nil {
		t.Fatalf("会员买会员包失败：%v", err)
	}
	plan, err := gw.PricingPlan(ctx, bobSession)
	if err != nil || plan.Current.Window == nil || plan.Member.Source != aigateway.MemberSourceVip || len(plan.Timeline) == 0 {
		t.Fatalf("价格方案不对：%+v %v", plan, err)
	}
	preview, err := gw.PricingPreview(ctx, appID, AIGatewayPricingPreviewInput{Model: "chat", IsVip: true})
	if err != nil || preview.Price == nil || !preview.Breakdown.Total.Equal(dec("0.5")) {
		t.Fatalf("价格模拟不对：%+v %v", preview, err)
	}

	// ── 作废批次 ──
	bobLots, err := gw.ListLots(ctx, aigateway.LotQuery{AppID: appID, UserID: bob, Origin: aigateway.LotAdmin})
	if err != nil || len(bobLots.Items) != 1 {
		t.Fatalf("应有一个管理员发放的批次：%+v %v", bobLots, err)
	}
	revoked, err := gw.RevokeLot(ctx, appID, bobLots.Items[0].ID, "测试作废", "tester")
	if err != nil || revoked.Status != aigateway.LotRevoked || !revoked.Remaining.IsZero() {
		t.Fatalf("作废失败：%+v %v", revoked, err)
	}
	if _, err := gw.RevokeLot(ctx, appID, bobLots.Items[0].ID, "", "tester"); errCode(err) != 40241 {
		t.Fatalf("重复作废应 40241，得到 %v", err)
	}

	// ── 经营统计 ──
	stats, err := gw.CommerceStats(ctx, appID, 7)
	if err != nil || stats.Orders != 3 || !stats.Revenue.Equal(dec("6")) || !stats.Refunded.Equal(dec("0.75")) ||
		len(stats.Daily) != 7 || len(stats.Windows) != 1 || stats.Windows[0].WindowID != "all" || len(stats.Expiry) != 5 {
		t.Fatalf("经营统计不对：%+v %v", stats, err)
	}

	// ── 全局不变式 ──
	if n := count(`SELECT COUNT(*) FROM ai_gateway_requests WHERE appid = $1 AND status = 'reserved'`, appID); n != 0 {
		t.Fatalf("不应残留暂扣：%d", n)
	}
	if n := count(`SELECT COUNT(*) FROM ai_gateway_credit_lots WHERE appid = $1 AND (frozen <> 0 OR remaining + frozen > amount)`, appID); n != 0 {
		t.Fatalf("批次暂扣应归零、余量不超过总量：%d", n)
	}
	if n := count(`SELECT COUNT(*) FROM user_wallets WHERE appid = $1 AND frozen <> 0`, appID); n != 0 {
		t.Fatal("钱包冻结应全部归零")
	}
	if n := count(`SELECT COUNT(*) FROM integral_transactions WHERE appid = $1`, appID); n != 0 {
		t.Fatal("AI 额度与积分不互通：不应出现任何积分流水")
	}
}
