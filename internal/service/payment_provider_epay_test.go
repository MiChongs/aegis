package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/go-resty/resty/v2"
	"github.com/shopspring/decimal"
)

// signLikeEpayGateway 按易支付站点的 PHP 参考实现复算签名：
// 剔除 sign / sign_type / 空值，键名升序拼成 a=1&b=2，末尾直接接商户密钥后取 MD5。
//
// 刻意**不复用** generatePaymentSign —— 用被测代码去验被测代码，两边同时错时
// 测试照样是绿的，而下单链路漏排除 sign_type 正是这样躲过了此前所有单测。
func signLikeEpayGateway(params map[string]string, key string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if k == "sign" || k == "sign_type" || v == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, params[k]))
	}
	sum := md5.Sum([]byte(strings.Join(parts, "&") + key))
	return hex.EncodeToString(sum[:])
}

func epayTestConfig() map[string]any {
	return map[string]any{
		"pid":       "1001",
		"key":       "epay_merchant_key",
		"apiUrl":    "https://pay.example.com/",
		"notifyUrl": "https://aegis.example.com/api/public/pay/callback/epay",
		"returnUrl": "https://app.example.com/pay/done",
		"signType":  "MD5",
	}
}

// TestEpayCreateOrderSignVerifiesAtGateway 下单参数必须能被上游按易支付协议验过。
//
// 失败时上游只回一句「MD5签名校验失败」，本地怎么算都自洽 —— 因此这条断言必须
// 站在网关那一侧，而不是断言「我们算出来的等于我们算出来的」。
func TestEpayCreateOrderSignVerifiesAtGateway(t *testing.T) {
	provider := newEpayProvider(resty.New())
	cfg := epayTestConfig()

	payload, err := provider.CreateOrder(context.Background(), cfg, PaymentOrderRequest{
		OrderNo:      "P10000202608120354391717",
		Subject:      "会员套餐 · 一年",
		Amount:       decimal.RequireFromString("29.90"),
		ProviderType: "alipay",
		Metadata:     map[string]any{"purpose": "vip_purchase", "vipPlanId": 3},
	})
	if err != nil {
		t.Fatalf("下单失败: %v", err)
	}

	form := make(map[string]string, len(payload.FormData))
	for k, v := range payload.FormData {
		form[k] = fmt.Sprint(v)
	}
	if form["sign_type"] != "MD5" {
		t.Fatalf("sign_type 应随请求提交，实际 %q", form["sign_type"])
	}
	if want := signLikeEpayGateway(form, "epay_merchant_key"); form["sign"] != want {
		t.Fatalf("上游验签不通过\n提交的 sign: %s\n网关算出的: %s", form["sign"], want)
	}
}

// TestPaymentSignExcludesSignFields 排除规则属于函数本身，不属于调用点。
//
// 下单与回调两条链路共用这一个函数，规则写在调用点就必然出现「一边排除、
// 一边不排除」的不对称，而两边各自的单测都发现不了。
func TestPaymentSignExcludesSignFields(t *testing.T) {
	const key = "k"
	base := map[string]string{"pid": "1001", "money": "1.00"}
	want := generatePaymentSign(base, key, "MD5")

	noisy := map[string]string{
		"pid":       "1001",
		"money":     "1.00",
		"sign_type": "MD5",
		"sign":      "deadbeef",
		"empty":     "",
		"blank":     "   ",
	}
	if got := generatePaymentSign(noisy, key, "MD5"); got != want {
		t.Fatalf("sign / sign_type / 空值不应参与签名：期望 %s，实际 %s", want, got)
	}
}

// TestEpayCallbackVerifiesGatewaySignature 回调链路与网关同规则。
func TestEpayCallbackVerifiesGatewaySignature(t *testing.T) {
	provider := newEpayProvider(resty.New())
	cfg := epayTestConfig()

	callback := map[string]string{
		"pid":          "1001",
		"trade_no":     "20260812114500123456",
		"out_trade_no": "P10000202608120354391717",
		"type":         "alipay",
		"name":         "会员套餐 · 一年",
		"money":        "29.90",
		"trade_status": "TRADE_SUCCESS",
		"sign_type":    "MD5",
	}
	callback["sign"] = signLikeEpayGateway(callback, "epay_merchant_key")
	// 传输层注入的保留键不属于上游签名参数集，混进来不能影响验签结果。
	callback["__raw_body"] = "pid=1001&trade_status=TRADE_SUCCESS"

	result, err := provider.HandleCallback(context.Background(), cfg, callback, "1.2.3.4")
	if err != nil {
		t.Fatalf("合法回调被拒: %v", err)
	}
	if !result.Paid || result.OrderNo != "P10000202608120354391717" {
		t.Fatalf("回调解析有误: %+v", result)
	}

	callback["money"] = "0.01"
	if _, err := provider.HandleCallback(context.Background(), cfg, callback, "1.2.3.4"); err == nil {
		t.Fatal("金额被篡改后仍通过验签")
	}
}

// TestEpayCallbackReportsAmount 回调必须把 money 带回来。
//
// 不带的后果不是少一个字段，而是网关层那道「回调金额必须与订单一致」的交叉校验
// 整条空转（它以 Amount.IsPositive() 为前置条件）—— 看起来在防，其实没防。
func TestEpayCallbackReportsAmount(t *testing.T) {
	provider := newEpayProvider(resty.New())
	callback := map[string]string{
		"pid": "1001", "out_trade_no": "P1", "trade_no": "T1",
		"type": "alipay", "name": "测试", "money": "29.90",
		"trade_status": "TRADE_SUCCESS", "sign_type": "MD5",
	}
	callback["sign"] = signLikeEpayGateway(callback, "epay_merchant_key")

	result, err := provider.HandleCallback(context.Background(), epayTestConfig(), callback, "1.2.3.4")
	if err != nil {
		t.Fatalf("合法回调被拒: %v", err)
	}
	if !result.Amount.Equal(decimal.RequireFromString("29.90")) {
		t.Fatalf("回调金额未回填：期望 29.90，实际 %s", result.Amount.String())
	}
}

// TestEpayVerifyReturn 同步跳转的校验：验签通过才给订单号，且不看 trade_status。
func TestEpayVerifyReturn(t *testing.T) {
	provider := newEpayProvider(resty.New())
	params := map[string]string{
		"pid": "1001", "out_trade_no": "P1", "trade_no": "T1",
		"type": "alipay", "name": "测试", "money": "29.90",
		"trade_status": "TRADE_SUCCESS", "sign_type": "MD5",
	}
	params["sign"] = signLikeEpayGateway(params, "epay_merchant_key")

	t.Run("签名正确则返回订单号", func(t *testing.T) {
		orderNo, err := provider.VerifyReturn(epayTestConfig(), params)
		if err != nil {
			t.Fatalf("合法回跳被拒: %v", err)
		}
		if orderNo != "P1" {
			t.Fatalf("订单号不对: %q", orderNo)
		}
	})

	t.Run("IP 白名单不作用于回跳", func(t *testing.T) {
		// 回跳由用户浏览器发起，来源 IP 是任意用户的 IP。
		// 若把异步通知那份白名单套上来，每个真实用户都会被挡在结果页外。
		cfg := epayTestConfig()
		cfg["verifyIP"] = true
		cfg["allowedIPs"] = []string{"203.0.113.1"}
		if _, err := provider.VerifyReturn(cfg, params); err != nil {
			t.Fatalf("回跳被 IP 白名单误伤: %v", err)
		}
	})

	t.Run("参数被篡改则拒绝", func(t *testing.T) {
		tampered := map[string]string{}
		for k, v := range params {
			tampered[k] = v
		}
		tampered["money"] = "0.01"
		if _, err := provider.VerifyReturn(epayTestConfig(), tampered); err == nil {
			t.Fatal("篡改金额后仍通过回跳验签")
		}
	})

	t.Run("缺签名则拒绝", func(t *testing.T) {
		bare := map[string]string{"out_trade_no": "P1", "trade_status": "TRADE_SUCCESS"}
		if _, err := provider.VerifyReturn(epayTestConfig(), bare); err == nil {
			t.Fatal("无签名的回跳被接受了 —— 那等于凭订单号即可查交易")
		}
	})
}

// TestEpaySiteNameEntersSignedParams sitename 配了就必须真的发出去并参与签名。
func TestEpaySiteNameEntersSignedParams(t *testing.T) {
	provider := newEpayProvider(resty.New())
	cfg := epayTestConfig()
	cfg["sitename"] = "Voyage"

	payload, err := provider.CreateOrder(context.Background(), cfg, PaymentOrderRequest{
		OrderNo: "P1",
		Subject: "测试",
		Amount:  decimal.RequireFromString("1.00"),
	})
	if err != nil {
		t.Fatalf("下单失败: %v", err)
	}
	form := make(map[string]string, len(payload.FormData))
	for k, v := range payload.FormData {
		form[k] = fmt.Sprint(v)
	}
	if form["sitename"] != "Voyage" {
		t.Fatalf("sitename 未提交，实际 %q", form["sitename"])
	}
	if want := signLikeEpayGateway(form, "epay_merchant_key"); form["sign"] != want {
		t.Fatalf("带 sitename 后上游验签不通过：%s != %s", form["sign"], want)
	}
}
