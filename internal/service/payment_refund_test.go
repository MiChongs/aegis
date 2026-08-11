package service

import (
	"strings"
	"testing"

	paymentdomain "aegis/internal/domain/payment"
)

// TestRefundCapabilityMatchesImplementation 能力矩阵与接口实现必须严格一致。
//
// 这是本子系统最重要的不变式：控制台按 Capabilities.Refund 决定是否显示退款按钮，
// 若声明为 true 却没实现 paymentRefunder，用户会点了才发现不支持；
// 反过来实现了却不声明，则该渠道的退款能力永远无法被使用。
func TestRefundCapabilityMatchesImplementation(t *testing.T) {
	gateway := newTestPaymentGateway()

	for method, provider := range gateway.providers {
		caps := provider.Describe().Capabilities
		_, implemented := provider.(paymentRefunder)

		if caps.Refund && !implemented {
			t.Errorf("%s: 声明支持退款（Capabilities.Refund=true）但未实现 paymentRefunder", method)
		}
		if !caps.Refund && implemented {
			t.Errorf("%s: 实现了 paymentRefunder 但未在能力矩阵中声明，控制台不会展示退款入口", method)
		}

		// 退款查询同理：声明可查就必须实现 refundQuerier
		_, queryable := provider.(refundQuerier)
		if caps.RefundQuery && !queryable {
			t.Errorf("%s: 声明支持退款查询但未实现 refundQuerier", method)
		}
		if caps.RefundQuery && !caps.Refund {
			t.Errorf("%s: 声明可查询退款却不支持退款，能力矩阵自相矛盾", method)
		}
		// 不支持退款的渠道不应声明部分退款
		if !caps.Refund && caps.PartialRefund {
			t.Errorf("%s: 不支持退款却声明支持部分退款", method)
		}
	}
}

// TestResolveRefunderRejectsUnsupported 不支持退款的渠道必须在发起前被明确拒绝
func TestResolveRefunderRejectsUnsupported(t *testing.T) {
	gateway := newTestPaymentGateway()

	// Coinbase Commerce 是链上支付，没有退款接口
	if _, _, err := gateway.resolveRefunder(paymentdomain.MethodCoinbase); err == nil {
		t.Error("Coinbase 不支持退款，resolveRefunder 应返回错误")
	}
	// 支持退款的渠道必须能取到 refunder
	for _, method := range []string{
		paymentdomain.MethodAlipayNative,
		paymentdomain.MethodWechatNative,
		paymentdomain.MethodStripe,
		paymentdomain.MethodPaypal,
		paymentdomain.MethodBalance,
	} {
		if _, refunder, err := gateway.resolveRefunder(method); err != nil || refunder == nil {
			t.Errorf("%s 应支持退款，resolveRefunder 返回 err=%v", method, err)
		}
	}
}

// TestRefundStatusIsFinal 终态判定直接决定「额度是否释放」与「是否继续轮询」
func TestRefundStatusIsFinal(t *testing.T) {
	final := map[string]bool{
		paymentdomain.RefundSuccess:    true,
		paymentdomain.RefundFailed:     true,
		paymentdomain.RefundClosed:     true,
		paymentdomain.RefundPending:    false,
		paymentdomain.RefundProcessing: false,
	}
	for status, want := range final {
		if got := paymentdomain.RefundStatusIsFinal(status); got != want {
			t.Errorf("RefundStatusIsFinal(%q) = %v, want %v", status, got, want)
		}
	}
}

// TestGenerateRefundNoCharset 退款单号会作为上游 out_refund_no 提交，
// 微信的字符集约束最严（数字/字母/_-|*@），这里守住最小公共集合。
func TestGenerateRefundNoCharset(t *testing.T) {
	const allowed = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_-|*@"
	seen := make(map[string]bool)
	for i := 0; i < 200; i++ {
		no := generateRefundNo(1001)
		if len(no) > 64 {
			t.Fatalf("退款单号超过 64 字符：%s", no)
		}
		for _, ch := range no {
			if !strings.ContainsRune(allowed, ch) {
				t.Fatalf("退款单号含非法字符 %q：%s", ch, no)
			}
		}
		if seen[no] {
			t.Fatalf("退款单号重复：%s", no)
		}
		seen[no] = true
	}
}

// TestWechatRefundStatusMapping 微信 ABNORMAL 表示资金可能在途，
// 若误判为 failed 会释放额度并允许再退一次，造成重复退款。
func TestWechatRefundStatusMapping(t *testing.T) {
	cases := map[string]string{
		"SUCCESS":    paymentdomain.RefundSuccess,
		"CLOSED":     paymentdomain.RefundClosed,
		"PROCESSING": paymentdomain.RefundProcessing,
		"ABNORMAL":   paymentdomain.RefundProcessing,
		"":           paymentdomain.RefundProcessing,
	}
	for state, want := range cases {
		if got := wechatRefundStatus(state); got != want {
			t.Errorf("wechatRefundStatus(%q) = %q, want %q", state, got, want)
		}
	}
}
