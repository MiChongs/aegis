package service

import (
	"testing"

	"aegis/internal/config"
	paymentdomain "aegis/internal/domain/payment"

	"github.com/go-resty/resty/v2"
)

func notifyTestService(baseURL string) *PaymentService {
	return &PaymentService{
		receiptCfg: config.PaymentReceiptConfig{PublicBaseURL: baseURL},
	}
}

func notifyTestConfig(configured string) *paymentdomain.Config {
	data := map[string]any{"pid": "1001", "key": "k", "apiUrl": "https://pay.example.com"}
	if configured != "" {
		data["notifyUrl"] = configured
	}
	return &paymentdomain.Config{PaymentMethod: paymentdomain.MethodEpay, ConfigData: data}
}

// TestResolveNotifyURLPrecedence 异步通知地址的三级取值必须是 请求 → 渠道配置 → 平台默认。
//
// 顺序反了会让管理员在渠道里填的地址被平台默认值盖掉；少了最后一级则是
// 配置表单上「留空则使用平台默认回调地址」这句话没人兑现 —— 留空时 notify_url
// 空着发给上游，用户付了钱而订单永远停在待支付。
func TestResolveNotifyURLPrecedence(t *testing.T) {
	provider := newEpayProvider(resty.New())
	svc := notifyTestService("https://aegis.example.com")

	t.Run("下单请求优先级最高", func(t *testing.T) {
		got := svc.resolveNotifyURL(provider, notifyTestConfig("https://cfg.example.com/cb"), "https://req.example.com/cb")
		if got != "https://req.example.com/cb" {
			t.Fatalf("请求指定的地址应当胜出，实际 %q", got)
		}
	})

	t.Run("渠道配置盖过平台默认", func(t *testing.T) {
		got := svc.resolveNotifyURL(provider, notifyTestConfig("https://cfg.example.com/cb"), "")
		if got != "https://cfg.example.com/cb" {
			t.Fatalf("管理员配置的地址应当胜出，实际 %q", got)
		}
	})

	t.Run("都留空时回落到平台默认", func(t *testing.T) {
		got := svc.resolveNotifyURL(provider, notifyTestConfig(""), "")
		want := "https://aegis.example.com" + provider.Describe().CallbackPath
		if got != want {
			t.Fatalf("平台默认回调地址不对：期望 %q，实际 %q", want, got)
		}
	})
}

// TestResolveReturnURLPrecedence 同步跳转地址同样是三级取值，默认指向控制台结果页。
func TestResolveReturnURLPrecedence(t *testing.T) {
	svc := notifyTestService("https://aegis-api.example.com")
	svc.SetConsoleBaseURL("https://console.example.com/")

	withReturn := func(configured string) *paymentdomain.Config {
		cfg := notifyTestConfig("")
		if configured != "" {
			cfg.ConfigData["returnUrl"] = configured
		}
		return cfg
	}

	if got := svc.resolveReturnURL(withReturn("https://cfg.example.com/done"), "https://req.example.com/done"); got != "https://req.example.com/done" {
		t.Fatalf("请求指定的地址应当胜出，实际 %q", got)
	}
	if got := svc.resolveReturnURL(withReturn("https://cfg.example.com/done"), ""); got != "https://cfg.example.com/done" {
		t.Fatalf("管理员配置的地址应当胜出，实际 %q", got)
	}
	want := "https://console.example.com" + PaymentResultPath
	if got := svc.resolveReturnURL(withReturn(""), ""); got != want {
		t.Fatalf("默认应指向控制台结果页：期望 %q，实际 %q", want, got)
	}
}

// TestResolveReturnURLNeverUsesAPIBase 没配 CONSOLE_BASE_URL 时不下发同步跳转。
//
// 尤其不能拿 API_BASE_URL 顶替：/pay/result 是控制台的页面，
// 在后端域名上是 404 —— 把刚付过钱的用户送到 404 比不跳转糟得多。
func TestResolveReturnURLNeverUsesAPIBase(t *testing.T) {
	svc := notifyTestService("https://aegis-api.example.com") // 只配了 API_BASE_URL
	if got := svc.resolveReturnURL(notifyTestConfig(""), ""); got != "" {
		t.Fatalf("未配置 CONSOLE_BASE_URL 时不应下发同步跳转，实际 %q", got)
	}
}

// TestResolveNotifyURLNeverFabricates 没配 API_BASE_URL 时宁可留空，也不猜一个域名。
//
// 错的回调地址比没有更难查：上游会把通知打到一个不存在的地方，日志里什么都看不到。
func TestResolveNotifyURLNeverFabricates(t *testing.T) {
	provider := newEpayProvider(resty.New())
	if got := notifyTestService("").resolveNotifyURL(provider, notifyTestConfig(""), ""); got != "" {
		t.Fatalf("未配置 API_BASE_URL 时不应造出地址，实际 %q", got)
	}
}

// TestEveryProviderDeclaresCallbackPath 平台默认地址的路径来自渠道自述，
// 少一条就意味着那个渠道的「留空使用默认」是空话。
func TestEveryProviderDeclaresCallbackPath(t *testing.T) {
	svc := &PaymentService{providers: map[string]paymentProvider{}}
	svc.registerBuiltinProviders(resty.New())

	for method, provider := range svc.providers {
		// 余额是内部通道，没有上游也就没有回调。
		if method == paymentdomain.MethodBalance {
			continue
		}
		if provider.Describe().CallbackPath == "" {
			t.Errorf("渠道 %s 未声明 CallbackPath，平台默认回调地址对它无效", method)
		}
	}
}
