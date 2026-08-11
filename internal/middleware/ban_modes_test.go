package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"aegis/internal/config"
	firewalldomain "aegis/internal/domain/firewall"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func init() { gin.SetMode(gin.TestMode) }

// stubExtChecker 实现 ExtendedBanChecker 接口，方便按测试场景注入决策。
type stubExtChecker struct{ dec firewalldomain.BanDecision }

func (s *stubExtChecker) IsBanned(_ context.Context, _ string) (bool, error) {
	return s.dec.Banned, nil
}
func (s *stubExtChecker) CheckBan(_ context.Context, _ string) (firewalldomain.BanDecision, error) {
	return s.dec, nil
}

func buildFirewallForTest(t *testing.T, checker BanChecker, defaultMode string) *Firewall {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	rc := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rc.Close() })

	cfg := config.NormalizeFirewallConfig(config.FirewallConfig{
		Enabled:        true,
		GlobalRate:     "1200-M",
		AuthRate:       "180-M",
		AdminRate:      "360-M",
		DefaultBanMode: defaultMode,
		TarpitDelayMs:  50,
	})
	f, err := NewFirewall(cfg, zap.NewNop(), rc, "aegis_test", nil, checker)
	if err != nil {
		t.Fatalf("NewFirewall: %v", err)
	}
	return f
}

func doRequest(f *Firewall) *httptest.ResponseRecorder {
	r := gin.New()
	r.Use(RequestID())
	r.Use(f.Handler())
	r.GET("/ping", func(c *gin.Context) { c.String(200, "ok") })
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ping", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	r.ServeHTTP(w, req)
	return w
}

func TestBanMode_Forbidden(t *testing.T) {
	f := buildFirewallForTest(t, &stubExtChecker{dec: firewalldomain.BanDecision{
		Banned: true, Mode: firewalldomain.BanModeForbidden, Reason: "test",
	}}, "forbidden")
	w := doRequest(f)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", w.Code)
	}
}

func TestBanMode_Stealth404(t *testing.T) {
	f := buildFirewallForTest(t, &stubExtChecker{dec: firewalldomain.BanDecision{
		Banned: true, Mode: firewalldomain.BanModeStealth404,
	}}, "forbidden")
	w := doRequest(f)
	if w.Code != http.StatusNotFound {
		t.Fatalf("stealth_404 should return 404, got %d", w.Code)
	}
}

func TestBanMode_Teapot(t *testing.T) {
	f := buildFirewallForTest(t, &stubExtChecker{dec: firewalldomain.BanDecision{
		Banned: true, Mode: firewalldomain.BanModeTeapot,
	}}, "forbidden")
	w := doRequest(f)
	if w.Code != http.StatusTeapot {
		t.Fatalf("teapot should return 418, got %d", w.Code)
	}
}

func TestBanMode_RateChoke(t *testing.T) {
	f := buildFirewallForTest(t, &stubExtChecker{dec: firewalldomain.BanDecision{
		Banned: true, Mode: firewalldomain.BanModeRateChoke,
	}}, "forbidden")
	start := time.Now()
	w := doRequest(f)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("rate_choke should return 429, got %d", w.Code)
	}
	if time.Since(start) < time.Second {
		t.Errorf("rate_choke should incur at least 1s delay")
	}
	if w.Header().Get("Retry-After") == "" {
		t.Errorf("rate_choke should set Retry-After header")
	}
}

func TestBanMode_Tarpit_Delays(t *testing.T) {
	f := buildFirewallForTest(t, &stubExtChecker{dec: firewalldomain.BanDecision{
		Banned: true, Mode: firewalldomain.BanModeTarpit, DelayMs: 100,
	}}, "forbidden")
	start := time.Now()
	w := doRequest(f)
	if w.Code != http.StatusForbidden {
		t.Fatalf("tarpit final status should be 403, got %d", w.Code)
	}
	if time.Since(start) < 80*time.Millisecond {
		t.Errorf("tarpit should delay at least ~100ms; got %v", time.Since(start))
	}
}

func TestBanMode_FallbackToDefault(t *testing.T) {
	// decision.Mode 为空 → 应使用 FirewallConfig.DefaultBanMode
	f := buildFirewallForTest(t, &stubExtChecker{dec: firewalldomain.BanDecision{
		Banned: true, Mode: "",
	}}, "stealth_404")
	w := doRequest(f)
	if w.Code != http.StatusNotFound {
		t.Fatalf("empty mode should fallback to default (stealth_404=404), got %d", w.Code)
	}
}

func TestBanMode_UnknownFallback(t *testing.T) {
	// 非法 mode 也应降级到 forbidden
	f := buildFirewallForTest(t, &stubExtChecker{dec: firewalldomain.BanDecision{
		Banned: true, Mode: "garbage_mode",
	}}, "forbidden")
	w := doRequest(f)
	if w.Code != http.StatusForbidden {
		t.Fatalf("unknown mode should fallback to forbidden, got %d", w.Code)
	}
}

func TestBanMode_PassesWhenNotBanned(t *testing.T) {
	f := buildFirewallForTest(t, &stubExtChecker{dec: firewalldomain.BanDecision{Banned: false}}, "forbidden")
	w := doRequest(f)
	if w.Code != 200 {
		t.Fatalf("non-banned should pass through, got %d; body=%s", w.Code, strings.TrimSpace(w.Body.String()))
	}
}

func TestBanMode_Normalize(t *testing.T) {
	cases := map[string]string{
		"":                 "forbidden",
		"forbidden":        "forbidden",
		"silent_drop":      "silent_drop",
		"connection_reset": "connection_reset",
		"tarpit":           "tarpit",
		"stealth_404":      "stealth_404",
		"teapot":           "teapot",
		"rate_choke":       "rate_choke",
		"unknown":          "forbidden",
	}
	for in, want := range cases {
		if got := firewalldomain.NormalizeBanMode(in, "forbidden"); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}
