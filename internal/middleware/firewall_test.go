package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aegis/internal/config"
)

func TestNormalizeFirewallConfigUsesConservativeDefaults(t *testing.T) {
	cfg := config.NormalizeFirewallConfig(config.FirewallConfig{})

	if cfg.GlobalRate != "1200-M" {
		t.Fatalf("unexpected global rate: %s", cfg.GlobalRate)
	}
	if cfg.AuthRate != "180-M" {
		t.Fatalf("unexpected auth rate: %s", cfg.AuthRate)
	}
	if cfg.AdminRate != "360-M" {
		t.Fatalf("unexpected admin rate: %s", cfg.AdminRate)
	}
	if cfg.MaxPathLength != 2048 {
		t.Fatalf("unexpected max path length: %d", cfg.MaxPathLength)
	}
	if cfg.MaxQueryLength != 4096 {
		t.Fatalf("unexpected max query length: %d", cfg.MaxQueryLength)
	}
}

func TestFirewallSnapshotRequestBodyRestoresBody(t *testing.T) {
	firewall := &Firewall{state: firewallState{cfg: config.FirewallConfig{RequestBodyLimit: 1024}}}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login/password", strings.NewReader(`{"appid":10000,"account":"123456"}`))

	body, err := firewall.snapshotRequestBody(request)
	if err != nil {
		t.Fatalf("snapshotRequestBody returned error: %v", err)
	}
	if string(body) != `{"appid":10000,"account":"123456"}` {
		t.Fatalf("unexpected snapshot body: %s", string(body))
	}

	restored, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("failed to read restored body: %v", err)
	}
	if string(restored) != string(body) {
		t.Fatalf("unexpected restored body: %s", string(restored))
	}
}

func TestFirewallSnapshotRequestBodyRejectsOversizeBody(t *testing.T) {
	firewall := &Firewall{state: firewallState{cfg: config.FirewallConfig{RequestBodyLimit: 4}}}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login/password", strings.NewReader(`12345`))

	if _, err := firewall.snapshotRequestBody(request); err == nil {
		t.Fatal("expected oversize body error")
	}
}

func TestBlockByPathOrQueryAvoidsAggressiveKeywordBlocking(t *testing.T) {
	state := firewallState{
		blockedPathPrefix: []string{"/.env", "/.git"},
		blockedFragments: []string{
			"../", "..\\", "%2e%2e", "/.git/", "/.env", "/vendor/phpunit", "/etc/passwd", "/proc/self/environ",
			"<script", "%3cscript", "javascript:", "onerror=", "onload=",
			"union+select", "union%20select", "sleep(", "benchmark(", "load_file(", "information_schema",
			";cat+", "|cat+", "$(curl", "$(wget", "`curl", "`wget",
		},
	}

	// 正常请求不应被拦截
	if blocked, reason := blockByPathOrQuery(state, "/api/search", "keyword=hello+world"); blocked {
		t.Fatalf("unexpected keyword block: %s", reason)
	}
	// 路径遍历应被拦截
	if blocked, reason := blockByPathOrQuery(state, "/api/search", "redirect=../etc/passwd"); !blocked || reason == "" {
		t.Fatalf("expected traversal block, got blocked=%v reason=%s", blocked, reason)
	}
	// XSS 应被拦截
	if blocked, reason := blockByPathOrQuery(state, "/api/search", "q=<script>alert(1)</script>"); !blocked || reason == "" {
		t.Fatalf("expected XSS block, got blocked=%v reason=%s", blocked, reason)
	}
	// SQLi 应被拦截
	if blocked, reason := blockByPathOrQuery(state, "/api/search", "id=1+union+select+1,2,3"); !blocked || reason == "" {
		t.Fatalf("expected SQLi block, got blocked=%v reason=%s", blocked, reason)
	}
}

func TestBuildCorazaDirectivesUsesRelaxedAPIBaseline(t *testing.T) {
	directives := buildCorazaDirectives(config.FirewallConfig{CorazaParanoia: 1})

	if !strings.Contains(directives, "tx.inbound_anomaly_score_threshold=25") {
		t.Fatalf("expected relaxed inbound anomaly threshold, got: %s", directives)
	}
	if !strings.Contains(directives, "tx.outbound_anomaly_score_threshold=10") {
		t.Fatalf("expected relaxed outbound anomaly threshold, got: %s", directives)
	}
	// JSON Content-Type 请求仍应排除 SQLi/XSS（JSON body 中 < > 等字符合法）
	if !strings.Contains(directives, "ctl:ruleRemoveByTag=attack-sqli") {
		t.Fatalf("expected JSON body SQLi exclusion, got: %s", directives)
	}
	if !strings.Contains(directives, "ctl:ruleRemoveByTag=attack-xss") {
		t.Fatalf("expected JSON body XSS exclusion, got: %s", directives)
	}
}
