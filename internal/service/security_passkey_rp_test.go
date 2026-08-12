package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"aegis/internal/config"
	apperrors "aegis/pkg/errors"

	webauthnlib "github.com/go-webauthn/webauthn/webauthn"
)

// RP ID 的判据就是浏览器那一句「等于当前域名，或是它的可注册后缀」。
// 这里把该判据钉住：任何一条走偏，症状都是 navigator.credentials.create()
// 直接抛 SecurityError —— 服务端日志上一片安静，最难排查的那种。
func TestResolvePasskeyRPID(t *testing.T) {
	cases := []struct {
		name       string
		configured string
		host       string
		want       string
	}{
		{"未配置时跟随访问域名", "", "console.example.com", "console.example.com"},
		{"配置值等于访问域名", "example.com", "example.com", "example.com"},
		{"配置值是访问域名的父域，凭据跨子域通用", "example.com", "console.example.com", "example.com"},
		// 这一条是本次修复的起点：缺省 RP ID 是 localhost，缺省允许来源里却有
		// 127.0.0.1:3000，两者凑不到一起，从 127.0.0.1 打开必然报错。
		{"localhost 不是 127.0.0.1 的后缀，必须改用访问域名", "localhost", "127.0.0.1", "127.0.0.1"},
		{"配置值与访问域名无关时以访问域名为准", "localhost", "console.example.com", "console.example.com"},
		// example.com 是 notexample.com 的字符串后缀但不是域名后缀，
		// 少判一个点号就会签出一个浏览器不收的 RP ID。
		{"仅字符串后缀不算域名后缀", "example.com", "notexample.com", "notexample.com"},
		{"大小写与空白归一化", "  EXAMPLE.COM ", "console.example.com", "example.com"},
		{"拿不到访问域名时沿用配置", "example.com", "", "example.com"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolvePasskeyRPID(tc.configured, tc.host); got != tc.want {
				t.Fatalf("resolvePasskeyRPID(%q, %q) = %q, want %q", tc.configured, tc.host, got, tc.want)
			}
		})
	}
}

func TestPasskeyOriginAllowed(t *testing.T) {
	allowed := []string{"http://localhost:3000", "https://console.example.com", " "}

	cases := []struct {
		origin string
		want   bool
	}{
		{"http://localhost:3000", true},
		{"https://console.example.com", true},
		// 端口是源的一部分：3001 与 3000 是两个源
		{"http://localhost:3001", false},
		// scheme 同理，http 与 https 不能互认
		{"http://console.example.com", false},
		// 白名单里写的是 localhost，127.0.0.1 是另一个源，必须显式加
		{"http://127.0.0.1:3000", false},
	}

	for _, tc := range cases {
		if got := passkeyOriginAllowed(allowed, tc.origin); got != tc.want {
			t.Fatalf("passkeyOriginAllowed(%q) = %v, want %v", tc.origin, got, tc.want)
		}
	}
}

func TestResolveRequestOrigin(t *testing.T) {
	t.Run("优先 Origin 头", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "http://gateway.internal/api/admin/profile/passkey/register/options", nil)
		req.Header.Set("Origin", "https://Console.Example.COM")
		req.Header.Set("Referer", "https://other.example.com/page")
		if got := ResolveRequestOrigin(req); got != "https://console.example.com" {
			t.Fatalf("origin = %q", got)
		}
	})

	t.Run("没有 Origin 时退回 Referer", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "http://gateway.internal/x", nil)
		req.Header.Set("Referer", "https://console.example.com/profile?tab=security")
		if got := ResolveRequestOrigin(req); got != "https://console.example.com" {
			t.Fatalf("origin = %q", got)
		}
	})

	t.Run("两个头都没有时按转发头还原", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "http://gateway.internal/x", nil)
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Header.Set("X-Forwarded-Host", "console.example.com")
		if got := ResolveRequestOrigin(req); got != "https://console.example.com" {
			t.Fatalf("origin = %q", got)
		}
	})

	// 沙箱 iframe 送的是字面量 "null"，它不是一个可用的源；
	// 当成域名去算 RP ID 会得到 "null"，浏览器随后拒绝且不说原因。
	t.Run("null 来源视为拿不到", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "http://console.example.com/x", nil)
		req.Header.Set("Origin", "null")
		if got := ResolveRequestOrigin(req); got != "http://console.example.com" {
			t.Fatalf("origin = %q", got)
		}
	})
}

// 端到端钉住这次修复：同一份配置、同一个服务实例，从不同来源发起时
// 下发的 RP ID 必须各自等于那个来源的域名。
func TestWebAuthnForRequestFollowsOrigin(t *testing.T) {
	newService := func(rpID string) *SecurityService {
		cfg := config.NormalizeSecurityConfig(config.SecurityConfig{
			Passkey: config.PasskeyConfig{
				Enabled:       true,
				RPDisplayName: "Aegis",
				RPID:          rpID,
				RPOrigins:     []string{"http://localhost:3000", "http://127.0.0.1:3000"},
			},
			Modules: config.SecurityModulesConfig{PasskeyEnabled: true},
		}, "AEGIS", "test-secret")

		instance, err := buildPasskeyWebAuthn(cfg.Passkey, cfg.Passkey.RPID)
		if err != nil {
			t.Fatalf("build template instance: %v", err)
		}
		return &SecurityService{securityCfg: cfg, webauthn: instance}
	}

	assertRPID := func(t *testing.T, instance *webauthnlib.WebAuthn, want string) {
		t.Helper()
		if instance.Config.RPID != want {
			t.Fatalf("RPID = %q, want %q", instance.Config.RPID, want)
		}
	}

	// 这是 bug 报告里的场景：控制台从 127.0.0.1 打开。
	// 修复前 RP ID 恒为 localhost，浏览器报
	// "relying party ID is not a registrable domain suffix of, nor equal to the current domain"。
	t.Run("从 127.0.0.1 访问时 RP ID 跟着变", func(t *testing.T) {
		svc := newService("localhost")
		ctx := ContextWithRequestOrigin(context.Background(), "http://127.0.0.1:3000")
		instance, err := svc.webAuthnForRequest(ctx)
		if err != nil {
			t.Fatalf("webAuthnForRequest: %v", err)
		}
		assertRPID(t, instance, "127.0.0.1")
	})

	t.Run("从 localhost 访问时仍是 localhost", func(t *testing.T) {
		svc := newService("localhost")
		ctx := ContextWithRequestOrigin(context.Background(), "http://localhost:3000")
		instance, err := svc.webAuthnForRequest(ctx)
		if err != nil {
			t.Fatalf("webAuthnForRequest: %v", err)
		}
		assertRPID(t, instance, "localhost")
	})

	// 白名单是唯一的安全边界，不能因为"跟随访问域名"就把它一并放开。
	t.Run("来源不在白名单内直接拒绝并说清怎么改", func(t *testing.T) {
		svc := newService("")
		ctx := ContextWithRequestOrigin(context.Background(), "https://evil.example.com")
		if _, err := svc.webAuthnForRequest(ctx); err == nil {
			t.Fatal("expected rejection for origin outside the allow list")
		} else if appErr, ok := err.(*apperrors.AppError); !ok || appErr.Code != 40039 {
			t.Fatalf("err = %v, want app error 40039", err)
		}
	})

	// Finish 阶段必须复用 Begin 下发的那个 RP ID，否则中途换地址栏能把校验绕过去。
	t.Run("Finish 沿用挑战里记下的 RP ID", func(t *testing.T) {
		svc := newService("localhost")
		ctx := ContextWithRequestOrigin(context.Background(), "http://localhost:3000")
		instance, err := svc.webAuthnForCeremony(ctx, "127.0.0.1")
		if err != nil {
			t.Fatalf("webAuthnForCeremony: %v", err)
		}
		assertRPID(t, instance, "127.0.0.1")
	})
}
