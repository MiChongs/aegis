package service

import (
	"errors"
	"testing"

	paymentdomain "aegis/internal/domain/payment"
	apperrors "aegis/pkg/errors"
)

func testPayProviders() map[string]paymentProvider {
	providers := map[string]paymentProvider{}
	for _, p := range []paymentProvider{
		newEpayProvider(nil),
		newRainbowEpayProvider(nil),
		newQRPayProvider(nil),
		newStripeProvider(nil),
		newBalanceProvider(),
	} {
		providers[p.Name()] = p
	}
	return providers
}

func epayConfigData(types ...any) map[string]any {
	data := map[string]any{"pid": "1001", "key": "secret", "apiUrl": "https://pay.example.com"}
	if len(types) > 0 {
		data["supportedTypes"] = types
	}
	return data
}

func fixedCurrency(string, map[string]any) string { return "CNY" }

func methodKeys(items []paymentdomain.UserPayMethod) []string {
	keys := make([]string, 0, len(items))
	for _, item := range items {
		keys = append(keys, item.Key)
	}
	return keys
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// 后台「启用的支付类型」决定用户端列出哪几项、按什么顺序；wechat 是 wxpay 的别名，重复的只算一次。
func TestRoutedPayTypesFollowConfiguredOrder(t *testing.T) {
	types, fallback := newEpayProvider(nil).RoutedPayTypes(epayConfigData("qqpay", "Alipay", "wechat", "alipay"))

	got := make([]string, 0, len(types))
	for _, option := range types {
		got = append(got, option.Value)
	}
	if !equalStrings(got, []string{"qqpay", "alipay", "wxpay"}) {
		t.Fatalf("类型顺序 = %v，期望按配置顺序且去重", got)
	}
	if fallback != "wxpay" {
		t.Fatalf("放行 wxpay 时默认值应沿用 wxpay，得到 %q", fallback)
	}
	if types[1].Label != "支付宝" {
		t.Fatalf("声明过的类型应带上中文名，得到 %q", types[1].Label)
	}
}

func TestRoutedPayTypesDefaults(t *testing.T) {
	all, fallback := newEpayProvider(nil).RoutedPayTypes(epayConfigData())
	if len(all) != len(epayPayTypes) || fallback != "wxpay" {
		t.Fatalf("留空应放行全部类型并默认 wxpay，得到 %d 项、默认 %q", len(all), fallback)
	}

	// 不放行 wxpay 时，默认值换成第一项，而不是交给上游一个被关掉的类型。
	_, fallback = newEpayProvider(nil).RoutedPayTypes(epayConfigData("alipay", "qqpay"))
	if fallback != "alipay" {
		t.Fatalf("不放行 wxpay 时默认值应为第一项，得到 %q", fallback)
	}

	// 逗号分隔的字符串也认（早期手写的配置）。
	types, _ := newEpayProvider(nil).RoutedPayTypes(map[string]any{"supportedTypes": "alipay, wxpay"})
	if len(types) != 2 {
		t.Fatalf("逗号分隔的 supportedTypes 应解析出 2 项，得到 %d", len(types))
	}
}

// 易支付把类型原样交给站点，站点自加的类型照样放行；码支付会把表外类型悄悄当成微信，所以不放行。
func TestCustomPayTypesOnlyPassThroughForEpay(t *testing.T) {
	epayTypes, _ := newEpayProvider(nil).RoutedPayTypes(epayConfigData("alipay", "usdt"))
	if len(epayTypes) != 2 || epayTypes[1].Value != "usdt" || epayTypes[1].Label != "USDT" {
		t.Fatalf("易支付应放行自定义类型，得到 %+v", epayTypes)
	}

	qrTypes, _ := newQRPayProvider(nil).RoutedPayTypes(map[string]any{"supportedTypes": []any{"alipay", "usdt"}})
	if len(qrTypes) != 1 || qrTypes[0].Value != "alipay" {
		t.Fatalf("码支付只应放行声明过的类型，得到 %+v", qrTypes)
	}

	// 填的全是不认识的类型：退回声明的全部，而不是让这条配置一种都收不了。
	fallbackTypes, _ := newQRPayProvider(nil).RoutedPayTypes(map[string]any{"supportedTypes": []any{"usdt"}})
	if len(fallbackTypes) != len(qrpayPayTypes) {
		t.Fatalf("全是未知类型时应退回声明的全部，得到 %d 项", len(fallbackTypes))
	}
}

func TestResolveOrderPayType(t *testing.T) {
	epay := newEpayProvider(nil)
	config := &paymentdomain.Config{PaymentMethod: paymentdomain.MethodEpay, ConfigData: epayConfigData("alipay", "qqpay")}

	cases := []struct {
		requested string
		want      string
	}{
		{"", "alipay"},       // 未指明：取默认（不放行 wxpay，所以是第一项）
		{" QQPAY ", "qqpay"}, // 规范化后在放行范围内
		{"alipay", "alipay"},
	}
	for _, c := range cases {
		got, err := resolveOrderPayType(epay, config, c.requested)
		if err != nil || got != c.want {
			t.Fatalf("resolveOrderPayType(%q) = %q, %v；期望 %q", c.requested, got, err, c.want)
		}
	}

	// 后台关掉的类型，客户端改参数也用不了。
	_, err := resolveOrderPayType(epay, config, "wxpay")
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.Code != errCodePayTypeUnavailable {
		t.Fatalf("未放行的类型应返回 %d，得到 %v", errCodePayTypeUnavailable, err)
	}

	// 不分子类型的渠道：原样透传，与改动前一致。
	stripe := newStripeProvider(nil)
	got, err := resolveOrderPayType(stripe, &paymentdomain.Config{PaymentMethod: paymentdomain.MethodStripe}, " card ")
	if err != nil || got != "card" {
		t.Fatalf("Stripe 的类型应原样透传，得到 %q, %v", got, err)
	}
}

func TestBuildUserPayMethodsExpandsRoutedChannels(t *testing.T) {
	configs := []paymentdomain.Config{
		{ID: 1, PaymentMethod: paymentdomain.MethodEpay, ConfigName: "default", Enabled: true, IsDefault: true,
			ConfigData: func() map[string]any {
				data := epayConfigData("alipay", "wxpay")
				data["minAmount"] = 1.0
				return data
			}()},
		{ID: 2, PaymentMethod: paymentdomain.MethodRainbowEpay, ConfigName: "default", Enabled: true,
			ConfigData: epayConfigData("alipay")},
		{ID: 3, PaymentMethod: paymentdomain.MethodStripe, ConfigName: "intl", Enabled: true,
			ConfigData: map[string]any{"secretKey": "sk_test_x", "publishableKey": "pk_test_x", "webhookSecret": "whsec_x"}},
		{ID: 4, PaymentMethod: paymentdomain.MethodBalance, ConfigName: "wallet", Enabled: true, ConfigData: map[string]any{}},
	}

	result := buildUserPayMethods(testPayProviders(), configs, paymentdomain.PurposeVipPurchase, fixedCurrency)

	want := []string{
		"epay:default:alipay",
		"epay:default:wxpay",
		"rainbow_epay:default:alipay",
		"stripe:intl:",
		"balance:wallet:",
	}
	if got := methodKeys(result.Items); !equalStrings(got, want) {
		t.Fatalf("付款方式 = %v\n期望 %v", got, want)
	}
	if result.DefaultKey != "epay:default:wxpay" {
		t.Fatalf("默认项应是默认配置的默认类型，得到 %q", result.DefaultKey)
	}
	first := result.Items[0]
	if first.Label != "支付宝" || first.Channel != "易支付" || first.MinAmount != "1.00" || first.MaxAmount != "" {
		t.Fatalf("展示字段不对：%+v", first)
	}
	if result.Items[2].Channel != "彩虹易支付" {
		t.Fatalf("同名付款应用要靠渠道名区分，得到 %q", result.Items[2].Channel)
	}
	if stripe := result.Items[3]; stripe.Type != "" || stripe.Label != "Stripe" {
		t.Fatalf("不分子类型的渠道只列一项、不带类型：%+v", stripe)
	}
	defaults := 0
	for _, item := range result.Items {
		if item.IsDefault {
			defaults++
		}
	}
	if defaults != 1 {
		t.Fatalf("默认项应恰好一个，得到 %d", defaults)
	}
}

func TestBuildUserPayMethodsSkipsUnusableConfigs(t *testing.T) {
	configs := []paymentdomain.Config{
		// 凭据不完整：下单必然失败，不列；它是默认配置，所以整张表没有默认项。
		{ID: 1, PaymentMethod: paymentdomain.MethodEpay, ConfigName: "default", Enabled: true, IsDefault: true,
			ConfigData: map[string]any{"pid": "1001"}},
		{ID: 2, PaymentMethod: paymentdomain.MethodBalance, ConfigName: "wallet", Enabled: true, ConfigData: map[string]any{}},
		{ID: 3, PaymentMethod: "unknown_gateway", ConfigName: "x", Enabled: true, ConfigData: map[string]any{}},
		{ID: 4, PaymentMethod: paymentdomain.MethodRainbowEpay, ConfigName: "off", Enabled: false,
			ConfigData: epayConfigData()},
	}

	result := buildUserPayMethods(testPayProviders(), configs, paymentdomain.PurposeWalletRecharge, fixedCurrency)

	if len(result.Items) != 0 || result.DefaultKey != "" {
		t.Fatalf("充值时不列余额，凭据不全、未知渠道、停用的都不列，得到 %v（默认 %q）",
			methodKeys(result.Items), result.DefaultKey)
	}
}

// 下发给用户的字段里不能有任何配置数据：那里面是商户密钥。
func TestUserPayMethodsNeverCarryCredentials(t *testing.T) {
	configs := []paymentdomain.Config{
		{ID: 1, PaymentMethod: paymentdomain.MethodEpay, ConfigName: "default", Enabled: true, IsDefault: true,
			ConfigData: epayConfigData()},
	}
	result := buildUserPayMethods(testPayProviders(), configs, "", fixedCurrency)
	for _, item := range result.Items {
		for _, value := range []string{item.Key, item.Method, item.ConfigName, item.Type, item.Label,
			item.Description, item.Channel, item.Currency, item.MinAmount, item.MaxAmount} {
			if value == "secret" || value == "1001" || value == "https://pay.example.com" {
				t.Fatalf("用户端字段里出现了配置数据：%+v", item)
			}
		}
	}
}

func TestUserChannelName(t *testing.T) {
	cases := map[string][2]string{
		"易支付":    {paymentdomain.MethodEpay, "易支付 (Epay)"},
		"码支付":    {paymentdomain.MethodQRPay, "码支付 (QRPay)"},
		"支付宝":    {paymentdomain.MethodAlipayNative, "支付宝原生"},
		"PayPal": {paymentdomain.MethodPaypal, "PayPal"},
	}
	for want, input := range cases {
		if got := userChannelName(input[0], input[1]); got != want {
			t.Fatalf("userChannelName(%q) = %q，期望 %q", input[1], got, want)
		}
	}
}
