package middleware

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aegis/internal/config"
	fw "aegis/internal/firewall"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func TestNormalizeFirewallConfigUsesConservativeDefaults(t *testing.T) {
	cfg := config.NormalizeFirewallConfig(config.FirewallConfig{})

	if cfg.GlobalRate != "1200-M" || cfg.AuthRate != "180-M" || cfg.AdminRate != "360-M" {
		t.Fatalf("unexpected rates: %s %s %s", cfg.GlobalRate, cfg.AuthRate, cfg.AdminRate)
	}
	if cfg.MaxPathLength != 2048 || cfg.MaxQueryLength != 4096 {
		t.Fatalf("unexpected length limits: %d %d", cfg.MaxPathLength, cfg.MaxQueryLength)
	}
	if cfg.CorazaAnomalyThreshold != 5 || cfg.CorazaRulesetVersion != config.CurrentCorazaRulesetVersion {
		t.Fatalf("unexpected ruleset defaults: threshold=%d version=%d", cfg.CorazaAnomalyThreshold, cfg.CorazaRulesetVersion)
	}
}

// v1 持久化下来的「没人改过的默认值」升级到 v2 默认值；手工调过的不动。
func TestNormalizeFirewallConfigMigratesRulesetV1(t *testing.T) {
	legacy := []string{"920350", "920300", "920420", "920230", "920440", "913100", "921110", "932200", "932230", "932235", "932236", "932240", "932250", "942100", "942430", "942440", "941100"}
	cfg := config.NormalizeFirewallConfig(config.FirewallConfig{CorazaAnomalyThreshold: 40, CorazaRemovedRules: legacy})
	if cfg.CorazaAnomalyThreshold != 5 {
		t.Fatalf("threshold 40 should migrate to 5, got %d", cfg.CorazaAnomalyThreshold)
	}
	if strings.Join(cfg.CorazaRemovedRules, ",") != strings.Join(config.DefaultCorazaRemovedRules, ",") {
		t.Fatalf("legacy removed rules should migrate, got %v", cfg.CorazaRemovedRules)
	}
	custom := config.NormalizeFirewallConfig(config.FirewallConfig{CorazaAnomalyThreshold: 25, CorazaRemovedRules: []string{"941100"}})
	if custom.CorazaAnomalyThreshold != 25 || strings.Join(custom.CorazaRemovedRules, ",") != "941100" {
		t.Fatalf("hand-tuned values must survive: %+v", custom)
	}
	again := config.NormalizeFirewallConfig(config.FirewallConfig{CorazaRulesetVersion: 2, CorazaAnomalyThreshold: 40})
	if again.CorazaAnomalyThreshold != 40 {
		t.Fatal("v2 config must not be migrated again")
	}
}

func testState(t *testing.T, cfg config.FirewallConfig) *firewallState {
	t.Helper()
	cfg = config.NormalizeFirewallConfig(cfg)
	waf, err := fw.CompileWAF(firewallOptions(cfg, nil))
	if err != nil {
		t.Fatal(err)
	}
	return &firewallState{cfg: cfg, waf: waf, bodyExempt: cfg.CorazaBodyExemptPrefixes, blockedFragments: normalizeEntries(cfg.BlockedSignatures)}
}

func TestReadBodyForInspectionRestoresBody(t *testing.T) {
	f := &Firewall{}
	state := testState(t, config.FirewallConfig{RequestBodyLimit: 1024, CorazaEnabled: true})
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login/password", strings.NewReader(`{"appid":10000,"account":"123456"}`))

	body, inspected, err := f.readBodyForInspection(request, state)
	if err != nil || !inspected {
		t.Fatalf("readBodyForInspection: inspected=%v err=%v", inspected, err)
	}
	restored, _ := io.ReadAll(request.Body)
	if string(restored) != string(body) || string(body) != `{"appid":10000,"account":"123456"}` {
		t.Fatalf("body not restored: %q / %q", body, restored)
	}
}

func TestReadBodyForInspectionRejectsOversizeBody(t *testing.T) {
	f := &Firewall{}
	state := testState(t, config.FirewallConfig{RequestBodyLimit: 4, CorazaEnabled: true})
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login/password", strings.NewReader(`12345`))
	if _, _, err := f.readBodyForInspection(request, state); !errors.Is(err, fw.ErrBodyTooLarge) {
		t.Fatalf("expected ErrBodyTooLarge, got %v", err)
	}
}

// 超出检查上限的文件上传放行且请求体完整：安装包上传曾因请求体被读走一截而在下游报「缺少上传文件」。
func TestReadBodyForInspectionSkipsOversizeUpload(t *testing.T) {
	f := &Firewall{}
	state := testState(t, config.FirewallConfig{RequestBodyLimit: 4, CorazaEnabled: true})
	payload := "--b\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a.apk\"\r\n\r\n0123456789\r\n--b--\r\n"
	request := httptest.NewRequest(http.MethodPost, "/api/v1/apps/x/releases/assets", strings.NewReader(payload))
	request.Header.Set("Content-Type", "multipart/form-data; boundary=b")
	_, inspected, err := f.readBodyForInspection(request, state)
	if err != nil || inspected {
		t.Fatalf("expected skip, got inspected=%v err=%v", inspected, err)
	}
	restored, err := io.ReadAll(request.Body)
	if err != nil || string(restored) != payload {
		t.Fatalf("upload body must reach the handler intact, got %q (%v)", restored, err)
	}
}

// 管理端与豁免前缀不读请求体（AI 中转的请求体动辄几十 MB）。
func TestReadBodyForInspectionHonoursExemptions(t *testing.T) {
	f := &Firewall{}
	state := testState(t, config.FirewallConfig{CorazaEnabled: true})
	for _, path := range []string{"/api/admin/system/risk/rules", "/api/ai/v1/chat/completions", "/api/public/pay/callback/alipay"} {
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"x":1}`))
		if _, inspected, _ := f.readBodyForInspection(request, state); inspected {
			t.Errorf("%s body should not be inspected", path)
		}
		if b, _ := io.ReadAll(request.Body); string(b) != `{"x":1}` {
			t.Errorf("%s body must stay untouched", path)
		}
	}
}

func TestBlockByPathOrQueryAvoidsAggressiveKeywordBlocking(t *testing.T) {
	state := &firewallState{
		blockedPathPrefix: []string{"/.env", "/.git"},
		blockedFragments:  normalizeEntries(config.DefaultFirewallBlockedSignatures),
	}
	if blocked, reason := blockByPathOrQuery(state, "/api/search", "keyword=hello+world"); blocked {
		t.Fatalf("unexpected keyword block: %s", reason)
	}
	for _, q := range []string{"redirect=../etc/passwd", "q=<script>alert(1)</script>", "id=1+union+select+1,2,3", "x=${jndi:ldap://a}"} {
		if blocked, _ := blockByPathOrQuery(state, "/api/search", q); !blocked {
			t.Errorf("expected block for %s", q)
		}
	}
}

func newTestFirewall(t *testing.T, cfg config.FirewallConfig) *Firewall {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	rc := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rc.Close() })
	cfg.Enabled = true
	cfg.CorazaEnabled = true
	f, err := NewFirewall(cfg, zap.NewNop(), rc, "aegis_test", nil, nil)
	if err != nil {
		t.Fatalf("NewFirewall: %v", err)
	}
	return f
}

// fakeSealed 模拟 AppGateway 的 sealed 档：入口处请求体是密文，解开后把明文放回请求、
// 包一层加密 Writer（这里用前缀 "SEALED:" 代替真正的 AEAD），再交给后续中间件。
func fakeSealed(plain string) gin.HandlerFunc {
	return func(c *gin.Context) {
		_, _ = io.ReadAll(c.Request.Body)
		c.Request.Body = io.NopCloser(strings.NewReader(plain))
		c.Request.ContentLength = int64(len(plain))
		c.Request.Header.Set("Content-Type", "application/json")
		SetRequestTransport(c, "sealed")
		writer := newAppEncryptionResponseWriter(c.Writer)
		c.Writer = writer
		c.Next()
		writer.ResponseWriter.WriteHeader(writer.status)
		_, _ = writer.ResponseWriter.Write(append([]byte("SEALED:"), writer.body.Bytes()...))
	}
}

func sealedRouter(f *Firewall, plain string) *gin.Engine {
	r := gin.New()
	r.Use(RequestID(), f.Handler(), fakeSealed(plain), f.WAFHandler())
	r.POST("/api/v1/apps/:appkey/user/profile", func(c *gin.Context) {
		var body map[string]any
		_ = c.ShouldBindJSON(&body)
		c.JSON(http.StatusOK, gin.H{"code": 200, "data": body})
	})
	return r
}

// 加密载荷解开后才检测：入口处是一段「密文」，攻击串藏在明文里，WAF 必须拦下，
// 而且拦截响应要经过加密 Writer（客户端能解开看到 40396）。
func TestWAFInspectsDecryptedPayload(t *testing.T) {
	f := newTestFirewall(t, config.FirewallConfig{})
	attack := `{"nickname":"x' UNION SELECT username,password FROM users--"}`
	r := sealedRouter(f, attack)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/apps/voyage_main/user/profile", bytes.NewReader([]byte("q8Zk1n0ciphertext==")))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-Aegis-Protocol", "aegis-transport-v2")
	req.Header.Set("User-Agent", "AegisKotlin/1.0")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("decrypted attack should be blocked, got %d %s", rec.Code, rec.Body.String())
	}
	if !strings.HasPrefix(rec.Body.String(), "SEALED:") {
		t.Fatalf("block response must go through the encryption writer: %s", rec.Body.String())
	}
	var payload struct {
		Code int `json:"code"`
	}
	_ = json.Unmarshal([]byte(strings.TrimPrefix(rec.Body.String(), "SEALED:")), &payload)
	if payload.Code != 40396 {
		t.Fatalf("expected 40396, got %s", rec.Body.String())
	}
	if c := f.Counters(); c.WAFBlocked != 1 || c.Decrypted != 1 {
		t.Fatalf("counters: %+v", c)
	}
}

func TestWAFPassesBenignDecryptedPayload(t *testing.T) {
	f := newTestFirewall(t, config.FirewallConfig{})
	r := sealedRouter(f, `{"nickname":"李四 · 远航","signature":"Life is short; use Go :)"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/apps/voyage_main/user/profile", bytes.NewReader([]byte("ciphertext")))
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("User-Agent", "AegisKotlin/1.0")
	req.Header.Set("Accept", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "李四") {
		t.Fatalf("benign request should pass with body intact, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestCustomRulesAtEdge(t *testing.T) {
	f := newTestFirewall(t, config.FirewallConfig{})
	if err := f.SetPolicy([]fw.CustomRule{
		{ID: 1, Name: "block curl writes", Priority: 10, Action: fw.ActionBlock, Expression: `ua startsWith "curl/" && method == "POST"`},
		{ID: 2, Name: "trust probe", Priority: 5, Action: fw.ActionAllow, Expression: `headers["x-probe"] == "ok"`},
	}, nil); err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.Use(RequestID(), f.Handler(), f.WAFHandler())
	r.POST("/api/v1/apps/a/search", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	do := func(ua, probe, body string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/apps/a/search", strings.NewReader(body))
		req.Header.Set("User-Agent", ua)
		req.Header.Set("Content-Type", "application/json")
		if probe != "" {
			req.Header.Set("X-Probe", probe)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := do("curl/8.5", "", `{}`); code != http.StatusForbidden {
		t.Fatalf("custom block rule: %d", code)
	}
	// allow 跳过 WAF：同样的攻击载荷在探针请求上放行
	if code := do("curl/8.5", "ok", `{"q":"' union select password from users --"}`); code != http.StatusOK {
		t.Fatalf("allow rule should bypass waf: %d", code)
	}
	if code := do("Mozilla/5.0", "", `{"q":"' union select password from users --"}`); code != http.StatusForbidden {
		t.Fatalf("waf should block: %d", code)
	}
	if hits := f.TakeRuleHits(); hits[1] != 1 || hits[2] != 1 {
		t.Fatalf("hits: %v", hits)
	}
}

func TestDetectionOnlyDoesNotBlock(t *testing.T) {
	f := newTestFirewall(t, config.FirewallConfig{CorazaDetectionOnly: true})
	r := gin.New()
	r.Use(RequestID(), f.Handler(), f.WAFHandler())
	r.GET("/api/v1/apps/a/search", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps/a/search?q=1%27%20or%20%271%27%3D%271%27%20--%20", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("observe mode must not block: %d", rec.Code)
	}
	if f.Counters().Observed != 1 {
		t.Fatalf("observe counter: %+v", f.Counters())
	}
}

func TestTrustedCIDRSkipsChecks(t *testing.T) {
	f := newTestFirewall(t, config.FirewallConfig{TrustedCIDRs: []string{"192.0.2.0/24"}})
	r := gin.New()
	r.Use(RequestID(), f.Handler(), f.WAFHandler())
	r.GET("/api/v1/apps/a/search", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps/a/search?q=%3Cscript%3Ealert(1)%3C/script%3E", nil)
	req.RemoteAddr = "192.0.2.10:5555"
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("trusted network should skip signatures and waf: %d", rec.Code)
	}
}

func TestValidatePolicyRejectsBrokenExclusion(t *testing.T) {
	f := newTestFirewall(t, config.FirewallConfig{})
	if err := f.ValidatePolicy(nil, []fw.Exclusion{{PathMatch: "regex", Path: "^/a(", RuleIDs: []int{941100}}}); err == nil {
		t.Fatal("broken regex must fail validation")
	}
	if err := f.ValidatePolicy([]fw.CustomRule{{ID: 1, Action: fw.ActionBlock, Expression: "ip =="}}, nil); err == nil {
		t.Fatal("broken expression must fail validation")
	}
}
