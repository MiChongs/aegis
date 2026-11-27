package service

import (
	"testing"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	authdomain "aegis/internal/domain/auth"
	"aegis/internal/domain/coupon"
	paymentdomain "aegis/internal/domain/payment"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
)

// 优惠系统的端到端集成测试（真实 Postgres）：
// 建券与活动 → 领券（每人限领）→ 试算（限时 8 折 + 满 20 减 5 叠加、返券预告）→ 余额支付下单（预占、核销、返券返积分）→
// 活动每人一次 → 全额退款（券返还、活动名额退回、返券与返积分收回）→ 外部渠道订单过期释放 → 释放后才到账的付款照样结算。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55433/postgres go test ./internal/service -run TestCouponIntegration
func TestCouponIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	money := decimal.RequireFromString
	coupons := NewCouponService(zap.NewNop(), pg)
	vips := NewVipService(zap.NewNop(), pg)
	pay := &PaymentService{log: zap.NewNop(), pg: pg, providers: make(map[string]paymentProvider)}
	pay.registerBuiltinProviders(resty.New())
	pay.SetCouponService(coupons)

	const appID = int64(31700)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'coupon-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	balanceCfg, err := pg.UpsertPaymentConfig(ctx, paymentdomain.Config{
		AppID: appID, PaymentMethod: paymentdomain.MethodBalance, ConfigName: "余额", ConfigData: map[string]any{}, Enabled: true, IsDefault: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	plan := mustSavePlan(t, vips, vipdomain.PlanMutation{
		AppID: appID, Name: ptrTo("月卡"), DurationDays: ptrTo(30), Price: ptrTo(money("30")), IsActive: ptrTo(true),
	})
	alice := insertTestUser(t, ctx, pool, appID, "coupon-alice", "100")
	session := &authdomain.Session{AppID: appID, UserID: alice}
	expectCode := func(step string, err error, code int) {
		t.Helper()
		if errCode(err) != code {
			t.Fatalf("%s：期望 %d，得到 %v", step, code, err)
		}
	}
	scalar := func(query string, args ...any) string {
		t.Helper()
		var out string
		if err := pool.QueryRow(ctx, query, args...).Scan(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	// ── 建券 ──
	until := time.Now().Add(30 * 24 * time.Hour)
	tplA, err := coupons.CreateTemplate(ctx, coupon.TemplateInput{
		AppID: appID, Name: "会员满 20 减 5", Kind: coupon.KindAmountOff, Amount: money("5"), MinAmount: money("20"),
		Scopes: []string{coupon.ScopeVip}, StackWithPromotion: true, ValidityMode: coupon.ValidityFixed, ValidUntil: &until,
		TotalQuantity: 2, PerUserLimit: 1, Claimable: true, RefundReturn: true, Operator: "tester",
	})
	if err != nil {
		t.Fatal(err)
	}
	tplB, err := coupons.CreateTemplate(ctx, coupon.TemplateInput{
		AppID: appID, Name: "返利券", Kind: coupon.KindAmountOff, Amount: money("3"),
		Scopes: []string{coupon.ScopeVip, coupon.ScopeGoods}, ValidityMode: coupon.ValidityRelative, ValidDays: 7, TotalQuantity: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coupons.CreateTemplate(ctx, coupon.TemplateInput{AppID: appID, Name: "坏券", Kind: coupon.KindAmountOff, Amount: money("10"),
		MinAmount: money("10"), Scopes: []string{coupon.ScopeVip}, ValidityMode: coupon.ValidityFixed, ValidUntil: &until}); errCode(err) != errCodeCouponTemplateInvalid {
		t.Fatalf("满 10 减 10 应拒绝，得到 %v", err)
	}
	// 草稿不能领
	_, err = coupons.Claim(ctx, appID, alice, tplA.ID)
	expectCode("草稿领取", err, errCodeCouponUnavailable)
	for _, id := range []int64{tplA.ID, tplB.ID} {
		if _, err := coupons.SetTemplateStatus(ctx, appID, id, coupon.StatusActive, "tester"); err != nil {
			t.Fatal(err)
		}
	}
	center, err := coupons.Center(ctx, appID, alice)
	if err != nil || len(center) != 1 || !center[0].CanClaim || center[0].Remaining != 2 {
		t.Fatalf("领券中心：%+v %v", center, err)
	}
	claimed, err := coupons.Claim(ctx, appID, alice, tplA.ID)
	if err != nil || claimed.Status != coupon.CouponAvailable {
		t.Fatalf("领券：%+v %v", claimed, err)
	}
	_, err = coupons.Claim(ctx, appID, alice, tplA.ID)
	expectCode("每人限领", err, errCodeCouponClaimLimit)

	// ── 活动 ──
	promo, err := coupons.CreatePromotion(ctx, coupon.PromotionInput{
		AppID: appID, Name: "会员限时 8 折", Kind: coupon.PromotionDiscount,
		Rule:   coupon.PromotionRule{DiscountMode: coupon.DiscountPercent, Rate: 80},
		Scopes: []string{coupon.ScopeVip}, PlanIDs: []int64{plan.ID}, StackWithCoupon: true, PerUserLimit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	rebate, err := coupons.CreatePromotion(ctx, coupon.PromotionInput{
		AppID: appID, Name: "满 10 返券", Kind: coupon.PromotionRebate,
		Rule:   coupon.PromotionRule{MinAmount: money("10"), RebateCoupons: []coupon.RebateCoupon{{TemplateID: tplB.ID, Count: 1}}, RebateIntegral: 50},
		Scopes: []string{coupon.ScopeVip},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{promo.ID, rebate.ID} {
		if _, err := coupons.SetPromotionStatus(ctx, appID, id, coupon.StatusActive, "tester"); err != nil {
			t.Fatal(err)
		}
	}
	view, err := coupons.Promotions(ctx, appID, alice)
	if err != nil || len(view.PlanPrices) != 1 || !view.PlanPrices[0].Price.Equal(money("24")) || view.UsableCoupons != 1 {
		t.Fatalf("活动价：%+v %v", view, err)
	}

	// ── 试算 ──
	quote, _, err := coupons.Quote(ctx, QuoteRequest{AppID: appID, UserID: alice, Purpose: paymentdomain.PurposeVipPurchase,
		PlanID: plan.ID, Amount: money("30"), CouponMode: coupon.CouponAuto})
	if err != nil {
		t.Fatal(err)
	}
	if !quote.PayableAmount.Equal(money("19")) || quote.Promotion == nil || quote.Coupon == nil || quote.Rebate == nil || !quote.Rebate.Eligible {
		t.Fatalf("试算应为 30 → 8 折 24 → 减 5 = 19 并命中返券，得到 %+v", quote)
	}

	// ── 下单：价格变了被拦下 ──
	_, _, err = pay.CreateOrderFromInput(ctx, session, PaymentOrderInput{PaymentMethod: paymentdomain.MethodBalance,
		Intent: OrderIntent{Purpose: paymentdomain.PurposeVipPurchase, PlanID: plan.ID}, CouponMode: coupon.CouponAuto, ExpectedAmount: "18.00"})
	expectCode("应付金额不一致", err, errCodeCouponPriceChanged)

	// ── 下单：余额支付 ──
	_, order, err := pay.CreateOrderFromInput(ctx, session, PaymentOrderInput{PaymentMethod: paymentdomain.MethodBalance,
		Intent: OrderIntent{Purpose: paymentdomain.PurposeVipPurchase, PlanID: plan.ID}, CouponMode: coupon.CouponAuto, ExpectedAmount: "19.00"})
	if err != nil {
		t.Fatal(err)
	}
	if order.Status != "paid" || !order.Amount.Equal(money("19")) || order.Discount == nil || !order.Discount.OriginalAmount.Equal(money("30")) {
		t.Fatalf("订单：%+v discount=%+v", order, order.Discount)
	}
	if got := scalar(`SELECT balance::text FROM user_wallets WHERE user_id = $1`, alice); !money(got).Equal(money("81")) {
		t.Fatalf("余额应扣 19，剩 %s", got)
	}
	if got := scalar(`SELECT status FROM user_coupons WHERE id = $1`, claimed.ID); got != coupon.CouponUsed {
		t.Fatalf("券应已使用：%s", got)
	}
	rec, err := coupons.OrderPromotion(ctx, appID, order.OrderNo)
	if err != nil || rec == nil || rec.Status != coupon.OrderSettled || rec.RebateStatus != coupon.RebateGranted {
		t.Fatalf("订单优惠记录：%+v %v", rec, err)
	}
	if got := scalar(`SELECT integral::text FROM users WHERE id = $1`, alice); got != "50" {
		t.Fatalf("返积分 50，实际 %s", got)
	}
	if got := scalar(`SELECT COUNT(*)::text FROM user_coupons WHERE user_id = $1 AND template_id = $2 AND source = 'rebate'`, alice, tplB.ID); got != "1" {
		t.Fatalf("应返 1 张券，实际 %s", got)
	}
	if got := scalar(`SELECT used_count::text || '/' || discount_total::text FROM coupon_promotions WHERE id = $1`, promo.ID); got != "1/6.00" {
		t.Fatalf("活动计数：%s", got)
	}
	// 结算幂等
	if _, err := coupons.SettleOrder(ctx, order.ID); err != nil {
		t.Fatal(err)
	}
	if got := scalar(`SELECT used_count::text FROM coupon_templates WHERE id = $1`, tplA.ID); got != "1" {
		t.Fatalf("重复结算不应重复计数：%s", got)
	}

	// ── 活动每人一次：第二单只能用返利券 ──
	quote, _, err = coupons.Quote(ctx, QuoteRequest{AppID: appID, UserID: alice, Purpose: paymentdomain.PurposeVipPurchase,
		PlanID: plan.ID, Amount: money("30"), CouponMode: coupon.CouponAuto})
	if err != nil || quote.Promotion != nil || quote.Coupon == nil || !quote.PayableAmount.Equal(money("27")) {
		t.Fatalf("第二单试算：%+v %v", quote, err)
	}
	// 指定一张不能用的券
	_, _, err = coupons.Quote(ctx, QuoteRequest{AppID: appID, UserID: alice, Purpose: paymentdomain.PurposeVipPurchase,
		PlanID: plan.ID, Amount: money("30"), CouponID: claimed.ID})
	expectCode("已用过的券", err, errCodeCouponNotApplicable)

	// ── 全额退款：券返还、名额退回、返券与返积分收回 ──
	instr, _, err := pay.buildFulfillmentInstruction(order)
	if err != nil {
		t.Fatal(err)
	}
	order, _ = pg.GetPaymentOrderByOrderNo(ctx, order.OrderNo)
	refund, err := pg.CreatePaymentRefund(ctx, paymentdomain.RefundCreation{AppID: appID, Order: order, RefundNo: "RF-COUPON-1", Amount: order.Amount, Reason: "测试"})
	if err != nil {
		t.Fatal(err)
	}
	settled, _, err := pg.RefundPaymentOrderToWallet(ctx, refund.ID, order, order.Amount, refund.RefundNo, &instr, true)
	if err != nil {
		t.Fatal(err)
	}
	if got := scalar(`SELECT status FROM user_coupons WHERE id = $1`, claimed.ID); got != coupon.CouponAvailable {
		t.Fatalf("退款后券应返还：%s（%s）", got, settled.ReversalMessage)
	}
	if got := scalar(`SELECT used_count::text FROM coupon_promotions WHERE id = $1`, promo.ID); got != "0" {
		t.Fatalf("退款后活动名额应退回：%s", got)
	}
	if got := scalar(`SELECT status FROM user_coupons WHERE user_id = $1 AND source = 'rebate'`, alice); got != coupon.CouponRevoked {
		t.Fatalf("返券应收回：%s", got)
	}
	if got := scalar(`SELECT integral::text FROM users WHERE id = $1`, alice); got != "0" {
		t.Fatalf("返积分应收回，实际 %s", got)
	}

	// ── 外部渠道订单：过期释放，释放后才到账照样结算 ──
	quote, res, err := coupons.Quote(ctx, QuoteRequest{AppID: appID, UserID: alice, Purpose: paymentdomain.PurposeVipPurchase,
		PlanID: plan.ID, Amount: money("30"), CouponID: claimed.ID})
	if err != nil || res.UserCouponID != claimed.ID || res.PromotionID != promo.ID {
		t.Fatalf("退款后应能再用活动与这张券：%+v %v", quote, err)
	}
	expire := time.Now().Add(-time.Hour)
	pending, err := pg.CreatePaymentOrderWithPromotion(ctx, paymentdomain.OrderMutation{
		AppID: appID, UserID: &alice, ConfigID: balanceCfg.ID, OrderNo: "P-COUPON-EXT-1", Subject: "月卡", Amount: quote.PayableAmount,
		PaymentMethod: "epay", ProviderType: "alipay", Metadata: ApplyDiscountMetadata(map[string]any{}, res), ExpireAt: &expire,
	}, res)
	if err != nil {
		t.Fatal(err)
	}
	// 被占用时再下单：券已不可用
	if _, err := pg.CreatePaymentOrderWithPromotion(ctx, paymentdomain.OrderMutation{
		AppID: appID, UserID: &alice, ConfigID: balanceCfg.ID, OrderNo: "P-COUPON-EXT-2", Subject: "月卡", Amount: quote.PayableAmount,
		PaymentMethod: "epay", ProviderType: "alipay", ExpireAt: &expire,
	}, coupon.Reservation{UserCouponID: claimed.ID, TemplateID: tplA.ID, OriginalAmount: money("30"), PayableAmount: money("25"), CouponDiscount: money("5")}); errCode(coupons.translate(err)) != errCodeCouponQuoteChanged {
		t.Fatalf("占用中的券不能再用，得到 %v", err)
	}
	if n, err := pg.ReleaseOrderPromotions(ctx, 0, 100); err != nil || n != 1 {
		t.Fatalf("应释放 1 笔：%d %v", n, err)
	}
	if got := scalar(`SELECT status FROM user_coupons WHERE id = $1`, claimed.ID); got != coupon.CouponAvailable {
		t.Fatalf("释放后券应可用：%s", got)
	}
	if got := scalar(`SELECT used_count::text FROM coupon_promotions WHERE id = $1`, promo.ID); got != "0" {
		t.Fatalf("释放后名额应退回：%s", got)
	}
	if _, err := pg.MarkPaymentOrderPaid(ctx, pending.ID, "UP-1", "TRADE_SUCCESS", map[string]any{}, decimal.Zero); err != nil {
		t.Fatal(err)
	}
	late, err := coupons.SettleOrder(ctx, pending.ID)
	if err != nil || late == nil || late.Status != coupon.OrderSettled {
		t.Fatalf("晚到的付款应结算：%+v %v", late, err)
	}
	if got := scalar(`SELECT status FROM user_coupons WHERE id = $1`, claimed.ID); got != coupon.CouponUsed {
		t.Fatalf("晚到的付款应记为用掉了这张券：%s", got)
	}
	if got := scalar(`SELECT used_count::text FROM coupon_promotions WHERE id = $1`, promo.ID); got != "1" {
		t.Fatalf("晚到的付款应重新计入名额：%s", got)
	}

	// ── 管理端：概览、列表、删除限制 ──
	overview, err := coupons.Overview(ctx, appID, "Asia/Shanghai", 14)
	if err != nil || overview.Issued != 2 || overview.Orders != 1 || len(overview.Trend) != 14 {
		t.Fatalf("概览：%+v %v", overview, err)
	}
	expectCode("已发券的模板不能删", coupons.DeleteTemplate(ctx, appID, tplA.ID), errCodeCouponInUse)
	expectCode("有订单的活动不能删", coupons.DeletePromotion(ctx, appID, promo.ID), errCodeCouponInUse)
	in := coupon.TemplateInput{AppID: appID, Name: tplA.Name, Kind: coupon.KindAmountOff, Amount: money("6"), MinAmount: money("20"),
		Scopes: []string{coupon.ScopeVip}, StackWithPromotion: true, ValidityMode: coupon.ValidityFixed, ValidUntil: tplA.ValidUntil, TotalQuantity: 2}
	_, err = coupons.UpdateTemplate(ctx, tplA.ID, in)
	expectCode("已发券的模板不能改面额", err, errCodeCouponRuleLocked)
	page, err := coupons.ListUserCoupons(ctx, coupon.UserCouponQuery{AppID: appID, Keyword: "coupon-alice"})
	if err != nil || page.Total != 2 {
		t.Fatalf("管理端用户券：%+v %v", page, err)
	}
	orders, err := coupons.ListOrders(ctx, coupon.OrderPromotionQuery{AppID: appID, PromotionID: promo.ID})
	if err != nil || orders.Total != 2 {
		t.Fatalf("订单优惠记录：%+v %v", orders, err)
	}
	// 管理员发券：库存 2 已发完
	_, err = coupons.Issue(ctx, appID, tplA.ID, []int64{alice}, 2, "tester", false)
	expectCode("库存不足", err, errCodeCouponUnavailable)
	if outcomes, err := coupons.Issue(ctx, appID, tplB.ID, []int64{alice, 999999999}, 2, "tester", false); err != nil || len(outcomes) != 2 || !outcomes[0].OK || outcomes[1].OK {
		t.Fatalf("发券：%+v %v", outcomes, err)
	}
}
