package service

import (
	"context"
	"strings"
	"testing"

	paymentdomain "aegis/internal/domain/payment"
)

// callbackWith 构造一份带原始报文与签名头的回调数据，模拟传输层注入结果
func callbackWith(rawBody string, headers map[string]string) map[string]string {
	data := map[string]string{CallbackRawBodyKey: rawBody}
	for name, value := range headers {
		data[CallbackHeaderPrefix+strings.ToLower(name)] = value
	}
	return data
}

func TestVerifyWebhookHMAC(t *testing.T) {
	const secret = "whsec_test_key"
	const payload = `{"event":"payment.paid","amount":1000}`

	t.Run("hex 签名匹配则通过", func(t *testing.T) {
		if err := verifyWebhookHMAC(secret, payload, hmacSHA256Hex(secret, payload), "hex", "测试"); err != nil {
			t.Fatalf("合法签名被拒: %v", err)
		}
	})

	t.Run("base64 签名匹配则通过", func(t *testing.T) {
		if err := verifyWebhookHMAC(secret, payload, hmacSHA256Base64(secret, payload), "base64", "测试"); err != nil {
			t.Fatalf("合法签名被拒: %v", err)
		}
	})

	t.Run("报文被篡改则拒绝", func(t *testing.T) {
		sig := hmacSHA256Hex(secret, payload)
		tampered := `{"event":"payment.paid","amount":100000}`
		if err := verifyWebhookHMAC(secret, tampered, sig, "hex", "测试"); err == nil {
			t.Fatal("篡改金额后仍通过验签")
		}
	})

	t.Run("密钥不符则拒绝", func(t *testing.T) {
		sig := hmacSHA256Hex("attacker_key", payload)
		if err := verifyWebhookHMAC(secret, payload, sig, "hex", "测试"); err == nil {
			t.Fatal("错误密钥签名仍通过验签")
		}
	})

	t.Run("缺少签名头则拒绝", func(t *testing.T) {
		if err := verifyWebhookHMAC(secret, payload, "", "hex", "测试"); err == nil {
			t.Fatal("空签名仍通过验签")
		}
	})

	t.Run("未配置密钥则拒绝", func(t *testing.T) {
		if err := verifyWebhookHMAC("", payload, hmacSHA256Hex("", payload), "hex", "测试"); err == nil {
			t.Fatal("未配置密钥时仍通过验签——这会让任何人都能伪造回调")
		}
	})
}

func TestParsePaddleSignature(t *testing.T) {
	cases := []struct {
		header string
		ts     string
		h1     string
	}{
		{"ts=1671552777;h1=eb4d0dc8", "1671552777", "eb4d0dc8"},
		{"h1=abc;ts=123", "123", "abc"},
		{" ts=1 ; h1=2 ", "1", "2"},
		{"garbage", "", ""},
		{"", "", ""},
	}
	for _, tc := range cases {
		ts, h1 := parsePaddleSignature(tc.header)
		if ts != tc.ts || h1 != tc.h1 {
			t.Errorf("parsePaddleSignature(%q) = (%q, %q)，期望 (%q, %q)", tc.header, ts, h1, tc.ts, tc.h1)
		}
	}
}

// TestNewProviderCallbackVerification 逐个验证新增国际渠道的回调链路：
// 正确签名能解析出订单号/金额/支付状态，篡改报文必须被拒绝。
func TestNewProviderCallbackVerification(t *testing.T) {
	ctx := context.Background()
	gateway := newTestPaymentGateway()

	const secret = "test_webhook_secret"
	const orderNo = "P120260810120000123456"

	cases := []struct {
		method     string
		config     map[string]any
		rawBody    string
		headers    func(body string) map[string]string
		wantAmount string
	}{
		{
			method: paymentdomain.MethodPaddle,
			config: map[string]any{"apiKey": "pdl_test", "webhookSecret": secret, "currency": "USD"},
			rawBody: `{"event_type":"transaction.completed","data":{"id":"txn_01","status":"completed",` +
				`"currency_code":"USD","custom_data":{"order_no":"` + orderNo + `"},` +
				`"details":{"totals":{"grand_total":"1999","currency_code":"USD"}}}}`,
			headers: func(body string) map[string]string {
				const ts = "1700000000"
				return map[string]string{"Paddle-Signature": "ts=" + ts + ";h1=" + hmacSHA256Hex(secret, ts+":"+body)}
			},
			wantAmount: "19.99",
		},
		{
			method: paymentdomain.MethodLemonSqueezy,
			config: map[string]any{"apiKey": "k", "storeId": "1", "variantId": "2", "webhookSecret": secret, "currency": "USD"},
			rawBody: `{"meta":{"event_name":"order_created","custom_data":{"order_no":"` + orderNo + `"}},` +
				`"data":{"id":"9","attributes":{"identifier":"ls_9","status":"paid","currency":"USD","total":2500}}}`,
			headers: func(body string) map[string]string {
				return map[string]string{"X-Signature": hmacSHA256Hex(secret, body)}
			},
			wantAmount: "25",
		},
		{
			method: paymentdomain.MethodRazorpay,
			config: map[string]any{"keyId": "rzp_test_x", "keySecret": "s", "webhookSecret": secret, "currency": "INR"},
			rawBody: `{"event":"payment_link.paid","payload":{"payment_link":{"entity":{"id":"plink_1",` +
				`"status":"paid","reference_id":"` + orderNo + `","amount":50000,"amount_paid":50000,"currency":"INR"}},` +
				`"payment":{"entity":{"id":"pay_1","status":"captured","amount":50000,"currency":"INR","notes":{}}}}}`,
			headers: func(body string) map[string]string {
				return map[string]string{"X-Razorpay-Signature": hmacSHA256Hex(secret, body)}
			},
			wantAmount: "500",
		},
		{
			method: paymentdomain.MethodCoinbase,
			config: map[string]any{"apiKey": "k", "webhookSecret": secret, "currency": "USD"},
			rawBody: `{"event":{"id":"evt_1","type":"charge:confirmed","data":{"id":"ch_1","code":"ABCD1234",` +
				`"metadata":{"order_no":"` + orderNo + `"},"pricing":{"local":{"amount":"42.50","currency":"USD"}},` +
				`"timeline":[{"status":"COMPLETED"}]}}}`,
			headers: func(body string) map[string]string {
				return map[string]string{"X-CC-Webhook-Signature": hmacSHA256Hex(secret, body)}
			},
			wantAmount: "42.5",
		},
		{
			method: paymentdomain.MethodSquare,
			config: map[string]any{
				"accessToken": "EAAA", "locationId": "L1", "webhookSecret": secret,
				"webhookUrl": "https://example.com/api/public/pay/callback/square", "currency": "USD",
			},
			rawBody: `{"type":"payment.updated","data":{"object":{"payment":{"id":"pay_1","order_id":"ord_1",` +
				`"status":"COMPLETED","note":"` + orderNo + `","amount_money":{"amount":1500,"currency":"USD"}}}}}`,
			headers: func(body string) map[string]string {
				const url = "https://example.com/api/public/pay/callback/square"
				return map[string]string{"x-square-hmacsha256-signature": hmacSHA256Base64(secret, url+body)}
			},
			wantAmount: "15",
		},
	}

	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			provider, ok := gateway.providers[tc.method]
			if !ok {
				t.Fatalf("渠道 %s 未注册", tc.method)
			}

			// 1) 合法签名：应解析出订单号、金额并判定为已支付
			result, err := provider.HandleCallback(ctx, tc.config, callbackWith(tc.rawBody, tc.headers(tc.rawBody)), "1.2.3.4")
			if err != nil {
				t.Fatalf("合法回调被拒: %v", err)
			}
			if !result.Paid {
				t.Errorf("成功事件未判定为已支付，trade_status=%q", result.TradeStatus)
			}
			if result.OrderNo != orderNo {
				t.Errorf("订单号解析错误: got %q, want %q", result.OrderNo, orderNo)
			}
			if got := result.Amount.String(); got != tc.wantAmount {
				t.Errorf("金额解析错误: got %s, want %s", got, tc.wantAmount)
			}
			if strings.TrimSpace(result.ProviderOrderNo) == "" {
				t.Error("未解析出上游单号")
			}

			// 2) 报文被篡改（签名不变）：必须拒绝
			tampered := strings.Replace(tc.rawBody, orderNo, "P999999999999999999999", 1)
			if _, err := provider.HandleCallback(ctx, tc.config, callbackWith(tampered, tc.headers(tc.rawBody)), "1.2.3.4"); err == nil {
				t.Error("篡改订单号后仍通过验签")
			}

			// 3) 缺少签名头：必须拒绝
			if _, err := provider.HandleCallback(ctx, tc.config, callbackWith(tc.rawBody, nil), "1.2.3.4"); err == nil {
				t.Error("无签名头的回调仍被接受")
			}

			// 4) 订单号预提取（服务层据此定位订单）应与验签结果一致
			extractor, ok := provider.(callbackOrderExtractor)
			if !ok {
				t.Fatalf("渠道 %s 未实现 callbackOrderExtractor，Webhook 无法定位订单", tc.method)
			}
			if got := extractor.ExtractOrderNo(callbackWith(tc.rawBody, nil)); got != orderNo {
				t.Errorf("ExtractOrderNo = %q, want %q", got, orderNo)
			}
		})
	}
}

// TestCoinbasePendingIsNotPaid 链上待确认不得当作到账，否则会出现「未确认即发货」
func TestCoinbasePendingIsNotPaid(t *testing.T) {
	ctx := context.Background()
	gateway := newTestPaymentGateway()
	provider := gateway.providers[paymentdomain.MethodCoinbase]

	const secret = "test_webhook_secret"
	body := `{"event":{"id":"evt_2","type":"charge:pending","data":{"code":"X1","metadata":{"order_no":"P1"},` +
		`"pricing":{"local":{"amount":"10.00","currency":"USD"}}}}}`

	result, err := provider.HandleCallback(ctx,
		map[string]any{"apiKey": "k", "webhookSecret": secret},
		callbackWith(body, map[string]string{"X-CC-Webhook-Signature": hmacSHA256Hex(secret, body)}),
		"1.2.3.4")
	if err != nil {
		t.Fatalf("合法回调被拒: %v", err)
	}
	if result.Paid {
		t.Error("charge:pending 被判定为已支付——链上尚未确认就会提前发货")
	}
}

// TestSquareRequiresWebhookURL Square 的签名原文包含通知地址，缺失时必须明确报错而非静默放行
func TestSquareRequiresWebhookURL(t *testing.T) {
	ctx := context.Background()
	gateway := newTestPaymentGateway()
	provider := gateway.providers[paymentdomain.MethodSquare]

	body := `{"type":"payment.updated","data":{"object":{"payment":{"id":"p","status":"COMPLETED","note":"P1"}}}}`
	_, err := provider.HandleCallback(ctx,
		map[string]any{"accessToken": "EAAA", "locationId": "L1", "webhookSecret": "s"},
		callbackWith(body, map[string]string{"x-square-hmacsha256-signature": "whatever"}),
		"1.2.3.4")
	if err == nil {
		t.Fatal("未配置通知地址时仍尝试验签并放行")
	}
}
