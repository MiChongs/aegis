package middleware

import (
	"testing"

	"aegis/internal/config"
)

// TestBodyInspectableExemptsOnlyConfiguredPrefixes AI 中转、支付回调、邮件回调与管理端免请求体检查，其余路径照常。
func TestBodyInspectableExemptsOnlyConfiguredPrefixes(t *testing.T) {
	cfg := config.NormalizeFirewallConfig(config.FirewallConfig{})
	state := &firewallState{cfg: cfg, bodyExempt: cfg.CorazaBodyExemptPrefixes}
	for path, want := range map[string]bool{
		"/api/ai/v1/chat/completions":        false,
		"/api/ai/v1/messages":                false,
		"/api/ai/v1x/chat":                   true,
		"/api/apps/k/ai/v1/chat/completions": true,
		"/api/v1/apps/k/auth/login":          true,
		"/api/public/pay/callback/alipay":    false,
		"/api/email/webhook/mailgun/x":       false,
		"/api/admin/system/risk/rules":       false,
	} {
		if got := bodyInspectable(path, state); got != want {
			t.Errorf("bodyInspectable(%q) = %v，想要 %v", path, got, want)
		}
	}
	state.cfg.CorazaInspectAdminBody = true
	if !bodyInspectable("/api/admin/system/risk/rules", state) {
		t.Error("开启管理端请求体检查后应检查 /api/admin/")
	}
}
