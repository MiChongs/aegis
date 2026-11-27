package service

import (
	"net/http"
	"strings"
	"testing"

	authdomain "aegis/internal/domain/auth"
	paymentdomain "aegis/internal/domain/payment"
	pgrepo "aegis/internal/repository/postgres"

	"github.com/go-resty/resty/v2"
	"go.uber.org/zap"
)

// 抖音支付端到端：真实 Postgres + 模拟的抖音支付服务器，走完整网关。
// 「H5 下单 → 支付通知结单（含重推幂等）→ 通知丢了靠用户查单补 → 退款受理中 → 退款通知结算 →
// 不认识的退款通知照常应答 → 同应用第二个商户号的通知也能验」。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestDouyinPayIntegration
func TestDouyinPayIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	pay := &PaymentService{log: zap.NewNop(), pg: pg, providers: make(map[string]paymentProvider)}
	pay.registerBuiltinProviders(resty.New())

	fake := newDyFakeServer(t)
	pay.registerProvider(fake.provider())

	const appID = int64(20200)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'douyinpay-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.UpsertPaymentConfig(ctx, paymentdomain.Config{
		AppID: appID, PaymentMethod: paymentdomain.MethodDouyinPay, ConfigName: "default",
		ConfigData: fake.config(nil), Enabled: true, IsDefault: true,
	}); err != nil {
		t.Fatal(err)
	}
	userID := insertTestUser(t, ctx, pool, appID, "douyin-buyer", "0")
	session := &authdomain.Session{AppID: appID, UserID: userID}

	fake.handle("POST /v1/trade/transactions/h5", func(r *http.Request, body map[string]any) (int, any) {
		return 200, map[string]any{"h5_url": "https://cashier.douyinpay.com/x?prepay_id=dy" + body["out_trade_no"].(string)}
	})
	paidUpstream := map[string]bool{}
	fake.handle("GET /v1/trade/transactions/out-trade-no/", func(r *http.Request, body map[string]any) (int, any) {
		no := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		state := "NOTPAY"
		if paidUpstream[no] {
			state = "SUCCESS"
		}
		return 200, map[string]any{"mchid": dyTestMchID, "out_trade_no": no, "transaction_id": "TX" + no, "trade_state": state,
			"amount": map[string]any{"total": 990}}
	})
	refundState := "PROCESSING"
	fake.handle("POST /v1/trade/refund/domestic/refunds", func(r *http.Request, body map[string]any) (int, any) {
		return 200, map[string]any{"refund_id": "RF1", "out_refund_no": body["out_refund_no"], "status": refundState,
			"amount": map[string]any{"refund": 990, "total": 990}}
	})

	createOrder := func() *paymentdomain.Order {
		t.Helper()
		payload, order, err := pay.CreateOrder(ctx, session, "抖音测试商品", "", "9.90", "", paymentdomain.MethodDouyinPay, "", "", "", nil, "14.23.150.211")
		if err != nil {
			t.Fatal(err)
		}
		if order.ProviderType != "h5" || !strings.Contains(payload.PaymentURL, order.OrderNo) {
			t.Fatalf("默认应走 H5：provider_type=%s url=%s", order.ProviderType, payload.PaymentURL)
		}
		return order
	}
	statusOf := func(orderNo string) *paymentdomain.Order {
		t.Helper()
		o, err := pg.GetPaymentOrderByOrderNo(ctx, orderNo)
		if err != nil {
			t.Fatal(err)
		}
		return o
	}
	withApp := func(data map[string]string) map[string]string {
		data["__app_id"] = "20200"
		return data
	}
	paidNotify := func(orderNo string, totalFen int) map[string]string {
		return withApp(fake.notify("TRANSACTION.SUCCESS", "transaction", map[string]any{
			"mchid": dyTestMchID, "appid": dyTestWebAppID, "out_trade_no": orderNo, "transaction_id": "TX" + orderNo,
			"trade_type": "H5", "trade_state": "SUCCESS", "amount": map[string]any{"total": totalFen, "payer_total": totalFen},
		}))
	}

	// ── 下单：通知地址带上了应用 ID ──
	first := createOrder()
	if sent := fake.lastRequest("/v1/trade/transactions/h5").Body; sent["notify_url"] != "https://api.example.com/api/public/pay/callback/douyinpay/20200" {
		t.Fatalf("notify_url = %v", sent["notify_url"])
	}

	// ── 支付通知 → 结单；重推幂等 ──
	res, err := pay.HandleCallback(ctx, paymentdomain.MethodDouyinPay, paidNotify(first.OrderNo, 990), "POST", "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Paid {
		t.Fatalf("支付通知应结单：%+v", res)
	}
	if o := statusOf(first.OrderNo); o.Status != "paid" || o.ProviderOrderNo != "TX"+first.OrderNo {
		t.Fatalf("订单未结：%+v", o)
	}
	if _, err := pay.HandleCallback(ctx, paymentdomain.MethodDouyinPay, paidNotify(first.OrderNo, 990), "POST", "1.2.3.4"); err != nil {
		t.Fatalf("重推应幂等成功：%v", err)
	}

	// ── 金额对不上：拒绝且不结单 ──
	short := createOrder()
	if _, err := pay.HandleCallback(ctx, paymentdomain.MethodDouyinPay, paidNotify(short.OrderNo, 1), "POST", ""); err == nil {
		t.Fatal("少付的通知必须拒绝")
	}
	if o := statusOf(short.OrderNo); o.Status == "paid" {
		t.Fatal("金额不符的订单不能结单")
	}

	// ── 通知丢了：用户查单时当场向上游核实 ──
	second := createOrder()
	paidUpstream[second.OrderNo] = true
	got, err := pay.GetUserOrder(ctx, session, second.OrderNo)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "paid" {
		t.Fatalf("查单应补上漏掉的通知，订单状态 %s", got.Status)
	}

	// ── 退款：受理中 → 退款通知结算 ──
	refund, err := pay.RefundOrder(ctx, appID, first.OrderNo, RefundOptions{Reason: "测试退款", Operator: "tester"})
	if err != nil {
		t.Fatal(err)
	}
	if refund.Status != paymentdomain.RefundProcessing {
		t.Fatalf("退款应处于受理中：%+v", refund)
	}
	if sent := fake.lastRequest("/v1/trade/refund/domestic/refunds").Body; sent["notify_url"] != "https://api.example.com/api/public/pay/callback/douyinpay/20200" {
		t.Fatalf("退款通知地址 = %v", sent["notify_url"])
	}
	refundNotify := func(refundNo string, fen int) map[string]string {
		return withApp(fake.notify("REFUND.SUCCESS", "refund", map[string]any{
			"mchid": dyTestMchID, "out_trade_no": first.OrderNo, "transaction_id": "TX" + first.OrderNo,
			"out_refund_no": refundNo, "refund_id": "RF1", "refund_status": "SUCCESS",
			"amount": map[string]any{"total": 990, "refund": fen, "payer_total": 990, "payer_refund": fen},
		}))
	}
	// 金额对不上的退款通知不结算
	if _, err := pay.HandleCallback(ctx, paymentdomain.MethodDouyinPay, refundNotify(refund.RefundNo, 1), "POST", ""); err == nil {
		t.Fatal("金额不符的退款通知必须拒绝")
	}
	res, err = pay.HandleCallback(ctx, paymentdomain.MethodDouyinPay, refundNotify(refund.RefundNo, 990), "POST", "")
	if err != nil {
		t.Fatalf("退款通知：%v", err)
	}
	if res.Paid || res.Refund == nil {
		t.Fatalf("退款通知被当成了支付：%+v", res)
	}
	settled, err := pg.GetPaymentRefundByNo(ctx, appID, refund.RefundNo)
	if err != nil {
		t.Fatal(err)
	}
	if settled.Status != paymentdomain.RefundSuccess || settled.ProviderRefundNo != "RF1" {
		t.Fatalf("退款单未结算：%+v", settled)
	}
	if o := statusOf(first.OrderNo); o.RefundStatus != paymentdomain.OrderRefundFull {
		t.Fatalf("订单退款状态 = %s", o.RefundStatus)
	}
	// 重推幂等
	if _, err := pay.HandleCallback(ctx, paymentdomain.MethodDouyinPay, refundNotify(refund.RefundNo, 990), "POST", ""); err != nil {
		t.Fatalf("退款通知重推应幂等：%v", err)
	}
	// 商户后台手工退的款：本地没有退款单，照常应答
	if _, err := pay.HandleCallback(ctx, paymentdomain.MethodDouyinPay, refundNotify("MANUAL-REFUND-1", 990), "POST", ""); err != nil {
		t.Fatalf("不认识的退款通知应照常应答：%v", err)
	}

	// ── 同一应用的第二个商户号：默认配置验不过，换另一份配置验 ──
	// 第二个商户号的平台证书固定进配置（provider 的传输层只连得上第一个模拟服务器）；
	// 两边证书序列号相同、公钥不同，正好验证「默认配置验签失败 → 换配置」这条路
	other := newDyFakeServer(t)
	if _, err := pg.UpsertPaymentConfig(ctx, paymentdomain.Config{
		AppID: appID, PaymentMethod: paymentdomain.MethodDouyinPay, ConfigName: "second",
		ConfigData: other.config(map[string]any{"mchId": "6020000000000777", "platformCertificate": other.platformCert}), Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	third := createOrder()
	notice := withApp(other.notify("TRANSACTION.SUCCESS", "transaction", map[string]any{
		"mchid": "6020000000000777", "out_trade_no": third.OrderNo, "transaction_id": "TX2" + third.OrderNo,
		"trade_state": "SUCCESS", "amount": map[string]any{"total": 990},
	}))
	if _, err := pay.HandleCallback(ctx, paymentdomain.MethodDouyinPay, notice, "POST", ""); err != nil {
		t.Fatalf("第二个商户号的通知应能用另一份配置验过：%v", err)
	}
	if o := statusOf(third.OrderNo); o.Status != "paid" {
		t.Fatalf("第二个商户号的通知没结单：%s", o.Status)
	}
}
