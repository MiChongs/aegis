package service

import (
	"strings"
	"testing"
	"time"

	paymentdomain "aegis/internal/domain/payment"
	"aegis/pkg/receipt"

	"github.com/shopspring/decimal"
)

func paidOrder() *paymentdomain.Order {
	paidAt := time.Date(2026, 3, 21, 10, 0, 0, 0, time.UTC)
	return &paymentdomain.Order{
		ID:            7,
		AppID:         12,
		OrderNo:       "P1220260321100000123456",
		Amount:        decimal.RequireFromString("100.00"),
		Status:        "paid",
		RefundStatus:  paymentdomain.OrderRefundNone,
		PaymentMethod: paymentdomain.MethodAlipayNative,
		PaidAt:        &paidAt,
	}
}

// 未支付的订单不能出「收据」—— 那是在给一笔没收到的钱开凭证。
func TestReceiptDocTypeFollowsOrderState(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(*paymentdomain.Order)
		override string
		want     receipt.DocType
	}{
		{name: "已支付出收据", mutate: func(*paymentdomain.Order) {}, want: receipt.TypeReceipt},
		{name: "未支付出账单", mutate: func(o *paymentdomain.Order) { o.Status = "pending" }, want: receipt.TypeInvoice},
		{name: "已过期出账单", mutate: func(o *paymentdomain.Order) { o.Status = "expired" }, want: receipt.TypeInvoice},
		{
			name:   "全额退款出退款凭证",
			mutate: func(o *paymentdomain.Order) { o.RefundStatus = paymentdomain.OrderRefundFull },
			want:   receipt.TypeCreditNote,
		},
		{
			name:   "部分退款仍是收据",
			mutate: func(o *paymentdomain.Order) { o.RefundStatus = paymentdomain.OrderRefundPartial },
			want:   receipt.TypeReceipt,
		},
		{
			name:     "显式指定优先",
			mutate:   func(o *paymentdomain.Order) { o.Status = "pending" },
			override: paymentdomain.ReceiptTypeReceipt,
			want:     receipt.TypeReceipt,
		},
		{
			name:     "无法识别的类型退回推导",
			mutate:   func(o *paymentdomain.Order) { o.Status = "pending" },
			override: "nonsense",
			want:     receipt.TypeInvoice,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			order := paidOrder()
			tc.mutate(order)
			if got := resolveReceiptDocType(order, tc.override); got != tc.want {
				t.Fatalf("得到 %s，期望 %s", got, tc.want)
			}
		})
	}
}

func TestReceiptStatusMapping(t *testing.T) {
	cases := map[string]struct {
		status, refundStatus string
		want                 receipt.Status
	}{
		"已支付":  {"paid", paymentdomain.OrderRefundNone, receipt.StatusPaid},
		"全额退款": {"paid", paymentdomain.OrderRefundFull, receipt.StatusRefunded},
		"部分退款": {"paid", paymentdomain.OrderRefundPartial, receipt.StatusPartiallyRefunded},
		"待支付":  {"pending", paymentdomain.OrderRefundNone, receipt.StatusPending},
		"已过期":  {"expired", paymentdomain.OrderRefundNone, receipt.StatusExpired},
		"支付失败": {"failed", paymentdomain.OrderRefundNone, receipt.StatusFailed},
	}
	for name, tc := range cases {
		order := paidOrder()
		order.Status, order.RefundStatus = tc.status, tc.refundStatus
		if got := resolveReceiptStatus(order); got != tc.want {
			t.Errorf("%s：得到 %s，期望 %s", name, got, tc.want)
		}
	}
}

// 凭证上的「已退款」只能是已经退成功的钱。
// 订单上的 refunded_amount 记的是已占用额度，含在途退款，
// 拿它去印凭证会让用户看到一笔还没到账的退款。
func TestRefundedTotalCountsOnlySuccessfulRefunds(t *testing.T) {
	order := paidOrder()
	order.RefundedAmount = decimal.RequireFromString("80.00") // 预占了 80，但只成功了 30
	refunds := []paymentdomain.Refund{
		{Amount: decimal.RequireFromString("30.00"), Status: paymentdomain.RefundSuccess},
		{Amount: decimal.RequireFromString("50.00"), Status: paymentdomain.RefundProcessing},
		{Amount: decimal.RequireFromString("10.00"), Status: paymentdomain.RefundFailed},
	}
	if got := refundedTotal(order, refunds); !got.Equal(decimal.RequireFromString("30.00")) {
		t.Fatalf("得到 %s，期望 30.00", got)
	}
}

// 退款单查不到时（退款表读失败），全额退款的订单仍要显示已全额退款。
func TestRefundedTotalFallsBackToOrderStatus(t *testing.T) {
	order := paidOrder()
	order.RefundStatus = paymentdomain.OrderRefundFull
	if got := refundedTotal(order, nil); !got.Equal(order.Amount) {
		t.Fatalf("得到 %s，期望 %s", got, order.Amount)
	}
	order.RefundStatus = paymentdomain.OrderRefundNone
	if got := refundedTotal(order, nil); !got.IsZero() {
		t.Fatalf("未退款订单得到 %s，期望 0", got)
	}
}

// 尚未提交上游的退款单不该出现在凭证上：它还不是一笔已发生的资金往来。
func TestPendingRefundsAreNotPrinted(t *testing.T) {
	items := buildReceiptRefunds([]paymentdomain.Refund{
		{RefundNo: "RF1", Status: paymentdomain.RefundPending},
		{RefundNo: "RF2", Status: paymentdomain.RefundSuccess},
	})
	if len(items) != 1 || items[0].Number != "RF2" {
		t.Fatalf("只应保留已提交上游的退款单，实得 %+v", items)
	}
}

// 币种取自渠道自述，而不是这里再维护一张表 —— 两处各一份早晚会漂移。
func TestCurrencyComesFromProviderSelfDescription(t *testing.T) {
	s := newTestPaymentGateway()
	cases := map[string]string{
		paymentdomain.MethodAlipayNative: "CNY",
		paymentdomain.MethodWechatNative: "CNY",
		paymentdomain.MethodEpay:         "CNY",
		paymentdomain.MethodBalance:      "CNY",
		paymentdomain.MethodRazorpay:     "INR",
		paymentdomain.MethodStripe:       "USD",
		paymentdomain.MethodPaypal:       "USD",
	}
	for method, want := range cases {
		if got := s.defaultCurrencyForMethod(method); got != want {
			t.Errorf("%s 的默认币种 = %s，期望 %s", method, got, want)
		}
	}
	// 渠道配置里显式写了就以它为准
	if got := s.resolveConfigCurrency(paymentdomain.MethodStripe, map[string]any{"currency": "eur"}); got != "EUR" {
		t.Errorf("配置的币种未生效：%s", got)
	}
	// 空串按未配置处理，而不是拿一个空币种去印凭证
	if got := s.resolveConfigCurrency(paymentdomain.MethodStripe, map[string]any{"currency": "  "}); got != "USD" {
		t.Errorf("空币种应退回渠道默认：%s", got)
	}
}

// 每个渠道都必须自述至少一种货币，否则凭证只能靠猜。
func TestEveryProviderDeclaresCurrency(t *testing.T) {
	s := newTestPaymentGateway()
	for _, meta := range s.AvailableMethods() {
		if len(meta.Currencies) == 0 {
			t.Errorf("渠道 %s 未自述计价货币，凭证将无从确定币种", meta.Method)
		}
	}
}

// 下载文件名恒为 ASCII：Content-Disposition 的非 ASCII 文件名各家客户端处理得参差不齐。
func TestReceiptFileNameIsASCII(t *testing.T) {
	name := receiptFileName(receipt.TypeCreditNote, "P12/2026✓0321", "zh-Hans")
	if !strings.HasPrefix(name, "credit_note_") || !strings.HasSuffix(name, ".pdf") {
		t.Fatalf("文件名形态异常：%s", name)
	}
	for _, r := range name {
		if r > 127 {
			t.Fatalf("文件名含非 ASCII 字符：%s", name)
		}
	}
	if strings.ContainsAny(name, `/\`) {
		t.Fatalf("文件名含路径分隔符：%s", name)
	}
}

// 凭证标识来自 URL，必须挡住路径穿越 —— 否则拼出的路径能读到别的应用目录。
func TestReceiptIDRejectsPathTraversal(t *testing.T) {
	for _, bad := range []string{"../../etc/passwd", "a/b", "..", "abc.pdf", "", "zz"} {
		if isHexToken(bad) {
			t.Errorf("%q 不该被当成合法的凭证标识", bad)
		}
	}
	if !isHexToken(randomReceiptID(16)) {
		t.Error("自己生成的凭证标识应当合法")
	}
}

func TestReceiptTimezoneResolution(t *testing.T) {
	if got := resolveReceiptTimezone("", nil); got != time.UTC {
		t.Errorf("无任何来源时应为 UTC，实得 %v", got)
	}
	if got := resolveReceiptTimezone("", map[string]any{"timezone": "Asia/Shanghai"}); got.String() != "Asia/Shanghai" {
		t.Errorf("用户设置未生效：%v", got)
	}
	// 显式指定优先于用户设置
	if got := resolveReceiptTimezone("Europe/Berlin", map[string]any{"timezone": "Asia/Shanghai"}); got.String() != "Europe/Berlin" {
		t.Errorf("显式时区未优先：%v", got)
	}
	// 时区名写错时不该整份凭证失败，退回下一个来源
	if got := resolveReceiptTimezone("Not/AZone", map[string]any{"timezone": "Asia/Shanghai"}); got.String() != "Asia/Shanghai" {
		t.Errorf("非法时区应退回下一来源：%v", got)
	}
}

// 语言优先级：显式指定 → 用户设置 → 请求头 → 平台默认。
// 用户在应用里选过语言是一次明确表达，不该被浏览器的 Accept-Language 覆盖。
func TestLocalePreferenceOrder(t *testing.T) {
	s := &PaymentService{}
	s.receiptCfg.DefaultLocale = "en"
	prefs := s.resolveLocalePrefs(
		paymentdomain.ReceiptOptions{Locale: "ja", AcceptLanguage: "de-DE"},
		map[string]any{"language": "zh-CN"},
	)
	want := []string{"ja", "zh-CN", "de-DE", "en"}
	if len(prefs) != len(want) {
		t.Fatalf("偏好链长度不符：%v", prefs)
	}
	for i := range want {
		if prefs[i] != want[i] {
			t.Fatalf("偏好链 = %v，期望 %v", prefs, want)
		}
	}
}

// 元数据的标签恒为译文键，可翻译的值走 ValueKey；用户数据只能进 Value。
// 否则一个叫「purpose.x」的套餐名会被当成译文键翻掉。
func TestReceiptMetadataSeparatesKeysFromUserData(t *testing.T) {
	order := paidOrder()
	userID := int64(90210)
	order.UserID = &userID
	order.Metadata = map[string]any{
		paymentdomain.MetaKeyPurpose:     paymentdomain.PurposeVipPurchase,
		paymentdomain.MetaKeyVipPlanName: "purpose.wallet_recharge", // 恶意/巧合的套餐名
		paymentdomain.MetaKeyVipDays:     float64(365),
	}
	pairs := buildReceiptMetadata(order)
	index := map[string]receipt.KeyValue{}
	for _, kv := range pairs {
		index[kv.Key] = kv
	}
	if got := index["meta.purpose"].ValueKey; got != "purpose.vip_purchase" {
		t.Errorf("用途应走译文键，实得 ValueKey=%q Value=%q", got, index["meta.purpose"].Value)
	}
	plan := index["meta.plan"]
	if plan.ValueKey != "" || plan.Value != "purpose.wallet_recharge" {
		t.Errorf("套餐名是用户数据，必须原样保留：ValueKey=%q Value=%q", plan.ValueKey, plan.Value)
	}
	if index["meta.duration"].Value != "365" {
		t.Errorf("时长解析异常：%q", index["meta.duration"].Value)
	}
	if index["meta.userId"].Value != "90210" {
		t.Errorf("用户编号缺失：%+v", index["meta.userId"])
	}
}

func TestReceiptNotesFollowOrderState(t *testing.T) {
	order := paidOrder()
	if notes := receiptNotes(order); len(notes) != 0 {
		t.Errorf("正常已支付订单不该有提示：%+v", notes)
	}
	order.Status = "pending"
	if notes := receiptNotes(order); len(notes) != 1 || notes[0].Key != "notes.unpaid" {
		t.Errorf("未支付订单应提示不作为付款凭证：%+v", notes)
	}
	order.Status = "paid"
	order.RefundStatus = paymentdomain.OrderRefundFull
	if notes := receiptNotes(order); len(notes) != 1 || notes[0].Key != "notes.refunded" {
		t.Errorf("全额退款应提示：%+v", notes)
	}
}
