package service

import (
	"encoding/json"
	"net/url"
	"testing"
	"time"

	authdomain "aegis/internal/domain/auth"
	paymentdomain "aegis/internal/domain/payment"
	pgrepo "aegis/internal/repository/postgres"

	"github.com/go-resty/resty/v2"
	"go.uber.org/zap"
)

// 爱发电端到端：真实 Postgres + 模拟的爱发电开放 API。走一遍
// 「下单拿链接 → 推送 → 回查结单 → 推送丢了靠查单补 → 后台补单 → 金额不符 → 后台测试推送」。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestAfdianPaymentIntegration
func TestAfdianPaymentIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	pay := &PaymentService{log: zap.NewNop(), pg: pg, providers: make(map[string]paymentProvider)}
	pay.registerBuiltinProviders(resty.New())
	pay.SetAdminInbox(NewAdminInboxService(zap.NewNop(), pg, nil))
	var superAdmin int64
	if err := pool.QueryRow(ctx, `INSERT INTO admin_accounts (account, password_hash, display_name, is_super_admin)
VALUES ('afdian-super', 'x', '超级管理员', TRUE) RETURNING id`).Scan(&superAdmin); err != nil {
		t.Fatal(err)
	}
	const appID = int64(20100)

	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'afdian-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	fake, cfgData := newFakeAfdian(t)
	if _, err := pg.UpsertPaymentConfig(ctx, paymentdomain.Config{
		AppID: appID, PaymentMethod: paymentdomain.MethodAfdian, ConfigName: "default",
		ConfigData: cfgData, Enabled: true, IsDefault: true,
	}); err != nil {
		t.Fatal(err)
	}
	userID := insertTestUser(t, ctx, pool, appID, "afdian-buyer", "0")
	session := &authdomain.Session{AppID: appID, UserID: userID}

	createOrder := func(amount string) *paymentdomain.Order {
		t.Helper()
		payload, order, err := pay.CreateOrder(ctx, session, "赞助", "", amount, "", paymentdomain.MethodAfdian, "", "", "", nil, "127.0.0.1")
		if err != nil {
			t.Fatal(err)
		}
		u, _ := url.Parse(payload.PaymentURL)
		if u.Query().Get("custom_order_id") != order.OrderNo || u.Query().Get("custom_price") != order.Amount.StringFixed(2) {
			t.Fatalf("下单链接没带上订单号或金额：%s", payload.PaymentURL)
		}
		return order
	}
	// 爱发电那边成交一笔：新单在前
	upstreamPaid := func(outTradeNo, orderNo, amount string) map[string]any {
		o := afdianOrderFixture(outTradeNo, orderNo, amount)
		fake.orders = append([]map[string]any{o}, fake.orders...)
		return o
	}
	statusOf := func(orderNo string) *paymentdomain.Order {
		t.Helper()
		o, err := pg.GetPaymentOrderByOrderNo(ctx, orderNo)
		if err != nil {
			t.Fatal(err)
		}
		return o
	}
	ack := func(result *paymentdomain.CallbackResult, err error) int {
		t.Helper()
		body, ok := pay.CallbackAck(paymentdomain.MethodAfdian, result, err)
		if !ok {
			t.Fatal("爱发电应自行组织应答")
		}
		var got struct {
			EC int `json:"ec"`
		}
		_ = json.Unmarshal(body, &got)
		return got.EC
	}

	// ── 推送 → 回查 → 结单 ──
	first := createOrder("29.90")
	pushed := upstreamPaid("AF001", first.OrderNo, "29.90")
	result, err := pay.HandleCallback(ctx, paymentdomain.MethodAfdian, afdianWebhook(pushed), "POST", "1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Paid || ack(result, nil) != 200 {
		t.Fatalf("推送应结单并回 ec=200：%+v", result)
	}
	if o := statusOf(first.OrderNo); o.Status != "paid" || o.ProviderOrderNo != "AF001" {
		t.Fatalf("订单未结：%+v", o)
	}
	// 重推幂等
	if _, err := pay.HandleCallback(ctx, paymentdomain.MethodAfdian, afdianWebhook(pushed), "POST", "1.2.3.4"); err != nil {
		t.Fatalf("重推应幂等成功：%v", err)
	}

	// ── 推送丢了：用户查单时当场补 ──
	second := createOrder("9.90")
	upstreamPaid("AF002", second.OrderNo, "9.90")
	got, err := pay.GetUserOrder(ctx, session, second.OrderNo)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "paid" {
		t.Fatalf("查单应补上漏掉的推送，订单状态 %s", got.Status)
	}
	// 节流：同一单 15 秒内不再打上游
	third := createOrder("5.00")
	before := fake.calls.Load()
	if _, err := pay.GetUserOrder(ctx, session, third.OrderNo); err != nil {
		t.Fatal(err)
	}
	if _, err := pay.GetUserOrder(ctx, session, third.OrderNo); err != nil {
		t.Fatal(err)
	}
	if calls := fake.calls.Load() - before; calls != 1 {
		t.Fatalf("节流期内应只查一次上游，实际 %d 次", calls)
	}

	// ── 后台补单：过期订单也认 ──
	upstreamPaid("AF003", third.OrderNo, "5.00")
	if _, err := pool.Exec(ctx, `UPDATE payment_orders SET status = 'expired' WHERE order_no = $1`, third.OrderNo); err != nil {
		t.Fatal(err)
	}
	pay.syncUnsettledOrders()
	if o := statusOf(third.OrderNo); o.Status != "paid" || o.ProviderOrderNo != "AF003" {
		t.Fatalf("后台补单没结掉过期订单：%+v", o)
	}

	// ── 用户在爱发电页面上改了金额 ──
	fourth := createOrder("50.00")
	cheap := upstreamPaid("AF004", fourth.OrderNo, "5.00")
	_, err = pay.HandleCallback(ctx, paymentdomain.MethodAfdian, afdianWebhook(cheap), "POST", "1.2.3.4")
	if errCode(err) != 40096 {
		t.Fatalf("金额不符应以 40096 拒绝，得到 %v", err)
	}
	if ack(nil, err) != 200 {
		t.Fatal("金额不符重推也没用，应回 ec=200")
	}
	// 少付：标成待人工处理、记下实付，之后不再查单；超级管理员收到一条站内信
	flagged := statusOf(fourth.OrderNo)
	if flagged.Status == "paid" || flagged.ReviewReason != paymentdomain.ReviewAmountMismatch ||
		flagged.PaidAmount == nil || flagged.PaidAmount.StringFixed(2) != "5.00" {
		t.Fatalf("少付的订单应标成待人工处理并记下实付 5.00，得到 %+v", flagged)
	}
	before = fake.calls.Load()
	pay.syncUnsettledOrders()
	if _, err := pay.GetUserOrder(ctx, session, fourth.OrderNo); err != nil {
		t.Fatal(err)
	}
	if o := statusOf(fourth.OrderNo); o.Status == "paid" {
		t.Fatal("少付的订单被后台补单结掉了")
	}
	if calls := fake.calls.Load() - before; calls != 0 {
		t.Fatalf("待人工处理的订单不应再查单，查了 %d 次", calls)
	}
	// 推送重复到达：照样拒绝，不再重复通知
	if _, err := pay.HandleCallback(ctx, paymentdomain.MethodAfdian, afdianWebhook(cheap), "POST", "1.2.3.4"); errCode(err) != 40096 {
		t.Fatalf("重推仍应拒绝，得到 %v", err)
	}
	waitFor(t, func() bool {
		var n int
		_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_notifications WHERE admin_id = $1 AND type = 'payment_review'`, superAdmin).Scan(&n)
		return n == 1
	})
	time.Sleep(100 * time.Millisecond)
	var notices int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_notifications WHERE admin_id = $1 AND type = 'payment_review'`, superAdmin).
		Scan(&notices); err != nil || notices != 1 {
		t.Fatalf("同一笔订单只通知一次，得到 %d %v", notices, err)
	}

	// 多付：照常结单，记下实付与差额
	generous := createOrder("49.90")
	more := upstreamPaid("AF005", generous.OrderNo, "50.00")
	if _, err := pay.HandleCallback(ctx, paymentdomain.MethodAfdian, afdianWebhook(more), "POST", "1.2.3.4"); err != nil {
		t.Fatalf("多付应照常结单，得到 %v", err)
	}
	if o := statusOf(generous.OrderNo); o.Status != "paid" || o.PaidAmount == nil || o.PaidAmount.StringFixed(2) != "50.00" ||
		o.ReviewReason != "" {
		t.Fatalf("多付的订单应已支付、实付记 50.00，得到 %+v", o)
	}
	var overpaid int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM payment_callback_logs WHERE order_id = $1 AND verification_status = 'overpaid'`,
		generous.ID).Scan(&overpaid); err != nil || overpaid != 1 {
		t.Fatalf("多付应记一条 overpaid 回调日志，得到 %d %v", overpaid, err)
	}
	// 金额对得上的订单，实付记成订单金额
	if o := statusOf(first.OrderNo); o.PaidAmount == nil || o.PaidAmount.StringFixed(2) != "29.90" {
		t.Fatalf("正常结单的实付应为订单金额 29.90，得到 %+v", o.PaidAmount)
	}
	// 管理端订单列表的「待人工处理」筛选：只有少付的那一笔
	reviewItems, reviewTotal, err := pg.ListPaymentOrdersByApp(ctx, appID, paymentdomain.OrderFilterReview, "", "", 0, 1, 50)
	if err != nil || reviewTotal != 1 || len(reviewItems) != 1 || reviewItems[0].OrderNo != fourth.OrderNo {
		t.Fatalf("待人工处理筛选应只有 %s，得到 %d 条 %v", fourth.OrderNo, reviewTotal, err)
	}
	// 付款人改不了金额的渠道仍然严格相等
	if pay.payerAdjustsAmount(paymentdomain.MethodEpay) || !pay.payerAdjustsAmount(paymentdomain.MethodAfdian) {
		t.Fatal("只有爱发电允许多付")
	}

	// ── 爱发电后台的测试推送 / 不经本平台的普通赞助 ──
	sample := afdianOrderFixture("202106232138371083454010626", "", "5.00")
	_, err = pay.HandleCallback(ctx, paymentdomain.MethodAfdian, afdianWebhook(sample), "POST", "1.2.3.4")
	if err == nil || ack(nil, err) != 200 {
		t.Fatalf("测试推送应被忽略并回 ec=200，得到 %v", err)
	}

	// ── 补单窗口之外的不再查 ──
	old := createOrder("1.00")
	if _, err := pool.Exec(ctx, `UPDATE payment_orders SET created_at = $2 WHERE order_no = $1`,
		old.OrderNo, time.Now().Add(-remoteSettleWindow-time.Hour)); err != nil {
		t.Fatal(err)
	}
	unsettled, err := pg.ListUnsettledPaymentOrders(ctx, []string{paymentdomain.MethodAfdian}, time.Now().Add(-remoteSettleWindow), 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range unsettled {
		if o.OrderNo == old.OrderNo {
			t.Fatal("超出补单窗口的订单仍在候选里")
		}
		if o.Status == "paid" {
			t.Fatalf("已支付订单出现在补单候选里：%s", o.OrderNo)
		}
	}
}
