package service

import (
	"strconv"
	"strings"
	"testing"

	authdomain "aegis/internal/domain/auth"
	paymentdomain "aegis/internal/domain/payment"
	vipdomain "aegis/internal/domain/vip"
	walletdomain "aegis/internal/domain/wallet"
	pgrepo "aegis/internal/repository/postgres"

	"github.com/go-resty/resty/v2"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"
)

// 多应用接入的履约端到端：真实 Postgres + 模拟爱发电。复现 2026-11 元匣工具箱的线上传参
// （type=vip、metadata.planId 字符串；type=wallet_recharge、不带 metadata），确认付款后会员与余额自动到账；
// 再走一遍服务端定价、两个应用共用一个爱发电账号的合并查单、后台重试、老订单补履约与「标记已处理」。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestPaymentFulfillmentIntegration
func TestPaymentFulfillmentIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	vips := NewVipService(zap.NewNop(), pg)
	wallets := NewWalletService(zap.NewNop(), pg)
	pay := &PaymentService{log: zap.NewNop(), pg: pg, providers: make(map[string]paymentProvider)}
	pay.registerBuiltinProviders(resty.New())
	pay.SetWalletService(wallets)
	money := decimal.RequireFromString

	const appA, appB = int64(20300), int64(20301)
	fake, cfgData := newFakeAfdian(t)
	for _, appID := range []int64{appA, appB} {
		if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'fulfil-test') ON CONFLICT DO NOTHING`, appID); err != nil {
			t.Fatal(err)
		}
		// 两个应用填的是同一个爱发电账号
		if _, err := pg.UpsertPaymentConfig(ctx, paymentdomain.Config{
			AppID: appID, PaymentMethod: paymentdomain.MethodAfdian, ConfigName: "爱发电",
			ConfigData: cfgData, Enabled: true, IsDefault: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	lifetime := mustSavePlan(t, vips, vipdomain.PlanMutation{
		AppID: appA, Name: ptrTo("永久会员"), Lifetime: ptrTo(true), Price: ptrTo(money("9.99")), BonusIntegral: ptrTo(int64(500)), IsActive: ptrTo(true),
	})
	monthly := mustSavePlan(t, vips, vipdomain.PlanMutation{
		AppID: appB, Name: ptrTo("月度会员"), DurationDays: ptrTo(30), Price: ptrTo(money("5.88")), IsActive: ptrTo(true),
	})
	pkg, err := wallets.AdminSavePackage(ctx, appB, walletdomain.RechargePackage{
		Title: "充 20 送 5", Amount: money("20"), BonusAmount: money("5"), IsActive: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	alice := insertTestUser(t, ctx, pool, appA, "fulfil-alice", "0")
	bob := insertTestUser(t, ctx, pool, appB, "fulfil-bob", "0")
	sessionA := &authdomain.Session{AppID: appA, UserID: alice}
	sessionB := &authdomain.Session{AppID: appB, UserID: bob}

	upstreamPaid := func(outTradeNo, orderNo, amount string) map[string]any {
		o := afdianOrderFixture(outTradeNo, orderNo, amount)
		fake.orders = append([]map[string]any{o}, fake.orders...)
		return o
	}
	reload := func(orderNo string) *paymentdomain.Order {
		t.Helper()
		o, err := pg.GetPaymentOrderByOrderNo(ctx, orderNo)
		if err != nil || o == nil {
			t.Fatalf("读订单失败：%v", err)
		}
		return o
	}
	cash := func(userID int64) decimal.Decimal {
		t.Helper()
		var balance string
		if err := pool.QueryRow(ctx, `SELECT (balance - bonus_balance)::text FROM user_wallets WHERE user_id = $1`, userID).Scan(&balance); err != nil {
			t.Fatal(err)
		}
		return money(balance)
	}

	expectCode := func(step string, err error, code int) {
		t.Helper()
		if errCode(err) != code {
			t.Fatalf("%s：期望错误码 %d，得到 %v", step, code, err)
		}
	}

	// ── 元匣工具箱的写法：type=vip、planId 是字符串 → 付款后自动开永久会员 ──
	_, _, err = pay.CreateOrder(ctx, sessionA, "永久会员", "永久", "9.99", "vip", paymentdomain.MethodAfdian, "", "", "",
		map[string]any{"planId": "abc", "renew": true}, "127.0.0.1")
	expectCode("认得是会员单但套餐 ID 坏了，下单就该拒", err, 40084)

	_, vipOrder, err := pay.CreateOrder(ctx, sessionA, "永久会员", "永久", "9.99", "vip", paymentdomain.MethodAfdian, "", "", "",
		map[string]any{"planId": strconv.FormatInt(lifetime.ID, 10), "renew": true}, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if vipOrder.Purpose != paymentdomain.PurposeVipPurchase || vipOrder.ProviderType == "vip" ||
		metaInt64(vipOrder.Metadata, paymentdomain.MetaKeyVipPlanID) != lifetime.ID || !metaBool(vipOrder.Metadata, paymentdomain.MetaKeyVipLifetime) {
		t.Fatalf("会员单应归一化并固化永久套餐快照：%+v", vipOrder)
	}
	_, _, err = pay.CreateOrder(ctx, sessionA, "永久会员", "", "1.00", "vip", paymentdomain.MethodAfdian, "", "", "",
		map[string]any{"planId": float64(lifetime.ID)}, "127.0.0.1")
	expectCode("改价下单仍要拒", err, 40089)

	// 付款人把 9.99 改成了 10.00：多付照常结单并开会员
	pushed := upstreamPaid("AFV1", vipOrder.OrderNo, "10.00")
	if _, err := pay.HandleCallback(ctx, paymentdomain.MethodAfdian, afdianWebhook(pushed), "POST", "1.2.3.4"); err != nil {
		t.Fatal(err)
	}
	if o := reload(vipOrder.OrderNo); o.Status != "paid" || o.FulfillmentStatus != paymentdomain.FulfillmentDone ||
		o.FulfillmentState != paymentdomain.FulfillmentStateDone {
		t.Fatalf("会员单应已支付并已履约：%+v", o)
	}
	if ent := mustEntitlement(t, vips, appA, alice); !ent.IsVIP || !ent.IsLifetime {
		t.Fatalf("付款后应是永久会员：%+v", ent)
	}

	// ── 充值：只有 type=wallet_recharge → 本金入账 ──
	_, recharge, err := pay.CreateOrder(ctx, sessionB, "", "", "10", "wallet_recharge", paymentdomain.MethodAfdian, "", "", "", nil, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if recharge.Subject != "余额充值" || recharge.Purpose != paymentdomain.PurposeWalletRecharge {
		t.Fatalf("充值单应自动取名并带用途：%+v", recharge)
	}

	// ── 服务端定价：买会员、按档位充值都不用传金额 ──
	vipPayload, vipB, err := pay.CreateOrderFromInput(ctx, sessionB, PaymentOrderInput{
		PaymentMethod: paymentdomain.MethodAfdian, Intent: OrderIntent{Purpose: "vip", PlanID: monthly.ID}, ClientIP: "127.0.0.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !vipB.Amount.Equal(money("5.88")) || vipB.Subject != "月度会员" || !strings.Contains(vipPayload.PaymentURL, "custom_price=5.88") {
		t.Fatalf("会员单应按套餐定价取名：%+v %s", vipB, vipPayload.PaymentURL)
	}
	_, pkgOrder, err := pay.CreateOrderFromInput(ctx, sessionB, PaymentOrderInput{
		PaymentMethod: paymentdomain.MethodAfdian, Intent: OrderIntent{PackageID: pkg.ID}, ClientIP: "127.0.0.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !pkgOrder.Amount.Equal(money("20")) || pkgOrder.Subject != "余额充值：充 20 送 5" ||
		metaString(pkgOrder.Metadata, paymentdomain.MetaKeyWalletBonus) != "5.00" {
		t.Fatalf("档位单应按档位定价并固化附赠：%+v", pkgOrder)
	}
	_, _, err = pay.CreateOrderFromInput(ctx, sessionB, PaymentOrderInput{PaymentMethod: paymentdomain.MethodAfdian, Subject: "x"})
	expectCode("普通商品单仍必须给金额", err, 40072)

	// ── 两个应用共用一个爱发电账号：推送全丢，后台补单一轮查单结掉两边的单 ──
	upstreamPaid("AFR1", recharge.OrderNo, "10.00")
	upstreamPaid("AFR2", vipB.OrderNo, "5.88")
	upstreamPaid("AFR3", pkgOrder.OrderNo, "20.00")
	_, otherApp, err := pay.CreateOrder(ctx, sessionA, "赞助", "", "3.00", "", paymentdomain.MethodAfdian, "", "", "", nil, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	upstreamPaid("AFR4", otherApp.OrderNo, "3.00")
	before := fake.calls.Load()
	pay.syncUnsettledOrders()
	if calls := fake.calls.Load() - before; calls != 1 {
		t.Fatalf("同一爱发电账号应合并成一轮查单（一页），实际查了 %d 次", calls)
	}
	for _, no := range []string{recharge.OrderNo, vipB.OrderNo, pkgOrder.OrderNo, otherApp.OrderNo} {
		if o := reload(no); o.Status != "paid" {
			t.Fatalf("%s 应被后台补单结掉：%+v", no, o)
		}
	}
	if got := cash(bob); !got.Equal(money("30")) {
		t.Fatalf("两笔充值本金应共 30（10 + 档位 20），得到 %s", got)
	}
	if ent := mustEntitlement(t, vips, appB, bob); !ent.IsVIP || ent.IsLifetime {
		t.Fatalf("月度会员应已开通：%+v", ent)
	}
	if o := reload(otherApp.OrderNo); o.FulfillmentState != paymentdomain.FulfillmentStateNotRequired {
		t.Fatalf("普通商品单不需要履约：%+v", o)
	}

	// ── 留言兜底：付款人改掉了 custom_order_id，留言里还有订单号 ──
	_, remarked, err := pay.CreateOrder(ctx, sessionB, "", "", "7", "recharge", paymentdomain.MethodAfdian, "", "", "", nil, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	lost := afdianOrderFixture("AFR5", "", "7.00")
	lost["remark"] = "订单 " + remarked.OrderNo
	fake.orders = append([]map[string]any{lost}, fake.orders...)
	if _, err := pay.HandleCallback(ctx, paymentdomain.MethodAfdian, afdianWebhook(lost), "POST", "1.2.3.4"); err != nil {
		t.Fatalf("留言里的订单号应能对上：%v", err)
	}
	if o := reload(remarked.OrderNo); o.Status != "paid" || o.FulfillmentStatus != paymentdomain.FulfillmentDone {
		t.Fatalf("留言兜底的订单应结单并入账：%+v", o)
	}

	cfgB, err := pg.GetPaymentConfig(ctx, appB, paymentdomain.MethodAfdian, "")
	if err != nil || cfgB == nil {
		t.Fatalf("读配置失败：%v", err)
	}
	// ── 后台重试：写清了用途的单履约失败过（这里直接造一笔已付款未履约的） ──
	stuck := insertPaidOrder(t, ctx, pool, pg, appB, cfgB.ID, bob, "PSTUCK0000000000001", money("4"),
		map[string]any{"purpose": paymentdomain.PurposeWalletRecharge})
	if stuck.FulfillmentState != paymentdomain.FulfillmentStatePending {
		t.Fatalf("写清用途、未履约的已付款单应是 pending：%+v", stuck)
	}
	pay.retryPendingFulfillments()
	if o := reload(stuck.OrderNo); o.FulfillmentStatus != paymentdomain.FulfillmentDone {
		t.Fatalf("后台重试应补上履约：%+v", o)
	}
	if got := cash(bob); !got.Equal(money("41")) {
		t.Fatalf("重试后本金应为 41，得到 %s", got)
	}

	// ── 老订单（修复前落库、没写 purpose）：不自动补，控制台逐笔处理 ──
	legacyVip := insertPaidOrder(t, ctx, pool, pg, appB, cfgB.ID, bob, "PLEGACY000000000001", money("5.88"),
		map[string]any{"planId": strconv.FormatInt(monthly.ID, 10), "renew": true})
	if _, err := pool.Exec(ctx, `UPDATE payment_orders SET provider_type = 'vip' WHERE id = $1`, legacyVip.ID); err != nil {
		t.Fatal(err)
	}
	legacyRecharge := insertPaidOrder(t, ctx, pool, pg, appB, cfgB.ID, bob, "PLEGACY000000000002", money("10"), map[string]any{})
	if _, err := pool.Exec(ctx, `UPDATE payment_orders SET provider_type = 'wallet_recharge' WHERE id = $1`, legacyRecharge.ID); err != nil {
		t.Fatal(err)
	}
	plainGoods := insertPaidOrder(t, ctx, pool, pg, appB, cfgB.ID, bob, "PLEGACY000000000003", money("1"), map[string]any{"sku": "A"})
	pay.retryPendingFulfillments()
	if o := reload(legacyVip.OrderNo); o.FulfillmentStatus != paymentdomain.FulfillmentNone || o.FulfillmentState != paymentdomain.FulfillmentStateUnrecognized {
		t.Fatalf("老订单不该被自动补，且应标成 unrecognized：%+v", o)
	}
	items, total, err := pg.ListPaymentOrdersByApp(ctx, appB, paymentdomain.OrderFilterUnfulfilled, "", "", 0, 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, item := range items {
		listed[item.OrderNo] = true
	}
	if total != 2 || !listed[legacyVip.OrderNo] || !listed[legacyRecharge.OrderNo] || listed[plainGoods.OrderNo] {
		t.Fatalf("「待履约」应只有两笔老订单，得到 %d 条 %v", total, listed)
	}

	// 预览：不改任何东西
	preview, err := pay.AdminFulfillOrder(ctx, appB, legacyVip.OrderNo, AdminFulfillRequest{DryRun: true, Operator: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	if preview.Applied || preview.Purpose != paymentdomain.PurposeVipPurchase ||
		metaInt64(preview.Snapshot, paymentdomain.MetaKeyVipPlanID) != monthly.ID {
		t.Fatalf("预览结果不对：%+v", preview)
	}
	if o := reload(legacyVip.OrderNo); o.FulfillmentStatus != paymentdomain.FulfillmentNone {
		t.Fatal("预览不该改订单")
	}
	// 补发会员：在已有月卡上再叠 30 天
	entBefore := mustEntitlement(t, vips, appB, bob)
	done, err := pay.AdminFulfillOrder(ctx, appB, legacyVip.OrderNo, AdminFulfillRequest{Operator: "tester", Note: "老客户端漏发"})
	if err != nil {
		t.Fatal(err)
	}
	if !done.Applied || done.Order.FulfillmentStatus != paymentdomain.FulfillmentDone {
		t.Fatalf("补发后应已履约：%+v", done)
	}
	if entAfter := mustEntitlement(t, vips, appB, bob); entAfter.RemainingDays <= entBefore.RemainingDays {
		t.Fatalf("补发会员应延长到期时间：%d → %d 天", entBefore.RemainingDays, entAfter.RemainingDays)
	}
	_, err = pay.AdminFulfillOrder(ctx, appB, legacyVip.OrderNo, AdminFulfillRequest{Operator: "tester"})
	expectCode("已履约的单不能再补", err, errCodeOrderNotFulfillable)

	// 充值老订单已经用物品发放规则补过：标记已处理，平台不再入账
	cashBefore := cash(bob)
	marked, err := pay.AdminFulfillOrder(ctx, appB, legacyRecharge.OrderNo, AdminFulfillRequest{Action: FulfillActionManual, Operator: "tester", Note: "规则 7 已补"})
	if err != nil {
		t.Fatal(err)
	}
	if marked.Order.FulfillmentStatus != paymentdomain.FulfillmentManual || marked.Order.FulfillmentState != paymentdomain.FulfillmentStateManual {
		t.Fatalf("应标成已人工处理：%+v", marked.Order)
	}
	if got := cash(bob); !got.Equal(cashBefore) {
		t.Fatalf("标记已处理不该动余额：%s → %s", cashBefore, got)
	}
	if _, total, _ := pg.ListPaymentOrdersByApp(ctx, appB, paymentdomain.OrderFilterUnfulfilled, "", "", 0, 1, 50); total != 0 {
		t.Fatalf("处理完后「待履约」应为空，还剩 %d", total)
	}
	_, err = pay.AdminFulfillOrder(ctx, appB, plainGoods.OrderNo, AdminFulfillRequest{Operator: "tester"})
	expectCode("普通商品单推断不出用途", err, errCodeOrderIntentUnknown)
	_, err = pay.AdminFulfillOrder(ctx, appA, plainGoods.OrderNo, AdminFulfillRequest{Operator: "tester"})
	expectCode("别的应用的订单查不到", err, 40472)
}
