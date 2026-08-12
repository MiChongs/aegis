package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aegis/pkg/clientip"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/gin-gonic/gin"
)

func newClientIPEngine(t *testing.T, cfg clientip.Config) (*gin.Engine, *string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	// 平台探测显式喂空，判定结果才不会随跑测试的机器变化。
	resolver, err := clientip.NewWithEnv(cfg, func(string) string { return "" })
	if err != nil {
		t.Fatalf("clientip.NewWithEnv() error = %v", err)
	}

	engine := gin.New()
	// 与 transport/http/router.go 保持一致：gin 自己那套转发头解析必须关掉。
	if err := engine.SetTrustedProxies(nil); err != nil {
		t.Fatalf("SetTrustedProxies(nil) error = %v", err)
	}
	engine.Use(ClientIP(resolver, zap.NewNop()))

	seen := new(string)
	engine.GET("/probe", func(c *gin.Context) {
		*seen = c.ClientIP()
		c.String(http.StatusOK, "ok")
	})
	return engine, seen
}

// TestClientIPRewritesRemoteAddr 这条测试守的是整个改造的落点：
// 判定结果必须通过 RemoteAddr 传导到 gin 的 ClientIP()，
// 否则仓库里那上百处 c.ClientIP() 调用一处都不会变对。
func TestClientIPRewritesRemoteAddr(t *testing.T) {
	engine, seen := newClientIPEngine(t, clientip.Config{})

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.RemoteAddr = "10.42.0.7:38210"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	engine.ServeHTTP(httptest.NewRecorder(), req)

	if got, want := *seen, "203.0.113.9"; got != want {
		t.Fatalf("c.ClientIP() = %q, want %q", got, want)
	}
}

// TestClientIPKeepsSpoofedHeadersOut 直连公网的客户端自己写 XFF 时，
// 判定结果必须仍是它自己的地址 —— 破了这一条，限流与封禁就形同虚设。
func TestClientIPKeepsSpoofedHeadersOut(t *testing.T) {
	engine, seen := newClientIPEngine(t, clientip.Config{})

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.RemoteAddr = "203.0.113.9:44321"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	engine.ServeHTTP(httptest.NewRecorder(), req)

	if got, want := *seen, "203.0.113.9"; got != want {
		t.Fatalf("c.ClientIP() = %q, want %q", got, want)
	}
}

// TestClientIPExposesPeerAndDetail 判定错时，第一个要问的是「这请求从哪台机器连过来的」。
func TestClientIPExposesPeerAndDetail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resolver, err := clientip.NewWithEnv(clientip.Config{}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("clientip.NewWithEnv() error = %v", err)
	}

	engine := gin.New()
	if err := engine.SetTrustedProxies(nil); err != nil {
		t.Fatalf("SetTrustedProxies(nil) error = %v", err)
	}
	engine.Use(ClientIP(resolver, zap.NewNop()))

	var (
		clientIP string
		peerIP   string
		detail   clientip.Result
		hasDeta  bool
	)
	engine.GET("/probe", func(c *gin.Context) {
		clientIP = RequestClientIP(c)
		peerIP = RequestPeerIP(c)
		detail, hasDeta = RequestClientIPDetail(c)
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.RemoteAddr = "10.42.0.7:38210"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	engine.ServeHTTP(httptest.NewRecorder(), req)

	if got, want := clientIP, "203.0.113.9"; got != want {
		t.Fatalf("RequestClientIP = %q, want %q", got, want)
	}
	if got, want := peerIP, "10.42.0.7"; got != want {
		t.Fatalf("RequestPeerIP = %q, want %q", got, want)
	}
	if !hasDeta {
		t.Fatal("判定过程应当留在请求上下文里")
	}
	if detail.Source != clientip.SourceChain || !detail.PeerTrusted {
		t.Fatalf("判定过程不符合预期：%s", detail)
	}
}

// TestClientIPDebugHeader 开关打开时，一次 curl 就能看到结论和它的全部依据。
func TestClientIPDebugHeader(t *testing.T) {
	engine, _ := newClientIPEngine(t, clientip.Config{DebugHeader: true})

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.RemoteAddr = "10.42.0.7:38210"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, req)

	if got, want := recorder.Header().Get("X-Aegis-Client-IP"), "203.0.113.9"; got != want {
		t.Fatalf("X-Aegis-Client-IP = %q, want %q", got, want)
	}
	source := recorder.Header().Get("X-Aegis-Client-IP-Source")
	for _, want := range []string{"source=forwarded-chain", "peer=10.42.0.7", "peer_trusted=true"} {
		if !strings.Contains(source, want) {
			t.Errorf("X-Aegis-Client-IP-Source 缺少 %q，实际为 %q", want, source)
		}
	}

	// 默认关闭：判定依据里含内网拓扑，不该吐给所有人。
	quiet, _ := newClientIPEngine(t, clientip.Config{})
	quietRecorder := httptest.NewRecorder()
	quietReq := httptest.NewRequest(http.MethodGet, "/probe", nil)
	quietReq.RemoteAddr = "10.42.0.7:38210"
	quiet.ServeHTTP(quietRecorder, quietReq)
	if got := quietRecorder.Header().Get("X-Aegis-Client-IP"); got != "" {
		t.Fatalf("默认不应当回显判定结果，得到 %q", got)
	}
}

// TestClientIPWarnsOnIgnoredForwarding 「全站客户端 IP 收敛成同一个地址」原本完全无声：
// 判定退回直连对端是一条正常路径，访问日志照记，只是记的全是入口反代的地址。
// 云上与 PaaS 的入口反代常常持有公网地址，默认受信集合覆盖不到，
// 那正是最需要被告知的时候。
func TestClientIPWarnsOnIgnoredForwarding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resolver, err := clientip.NewWithEnv(clientip.Config{}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("clientip.NewWithEnv() error = %v", err)
	}
	log, logs := newObservedLogger()

	engine := gin.New()
	if err := engine.SetTrustedProxies(nil); err != nil {
		t.Fatalf("SetTrustedProxies(nil) error = %v", err)
	}
	engine.Use(ClientIP(resolver, log))
	engine.GET("/probe", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	call := func(peer string) {
		req := httptest.NewRequest(http.MethodGet, "/probe", nil)
		req.RemoteAddr = peer
		req.Header.Set("X-Forwarded-For", "2409:8a4c:7810:eb7c::ed6, 104.22.72.17")
		engine.ServeHTTP(httptest.NewRecorder(), req)
	}

	call("8.221.123.21:52104")
	entries := logs.FilterLevelExact(zapcore.WarnLevel).All()
	if len(entries) != 1 {
		t.Fatalf("应当告警一次，实际 %d 条", len(entries))
	}
	fields := entries[0].ContextMap()
	if fields["peer"] != "8.221.123.21" {
		t.Errorf("peer = %v，告警里必须带上要加进 TRUSTED_PROXIES 的那个地址", fields["peer"])
	}
	if ignored, ok := fields["forwarded_ignored"].([]any); !ok || len(ignored) != 2 {
		t.Errorf("forwarded_ignored = %#v，应当列出被丢掉的那条链", fields["forwarded_ignored"])
	}
	for _, key := range []string{"consequence", "fix_pin", "fix_floating"} {
		if text, _ := fields[key].(string); text == "" {
			t.Errorf("告警缺少 %s：只说「被忽略了」而不说怎么改，等于还得再查一轮", key)
		}
	}

	// 同一个对端不再重复喊：每个请求喊一遍只会把日志淹掉，反而没人看。
	call("8.221.123.21:52999")
	if n := len(logs.FilterLevelExact(zapcore.WarnLevel).All()); n != 1 {
		t.Errorf("同一对端应当只告警一次，实际 %d 条", n)
	}

	// 换一个对端要重新喊：入口地址漂移过去，说明配置又没覆盖到新的那个。
	call("8.221.200.7:41000")
	if n := len(logs.FilterLevelExact(zapcore.WarnLevel).All()); n != 2 {
		t.Errorf("新对端应当重新告警，实际 %d 条", n)
	}

	// 对端受信、判定正常时不该有任何告警 —— 否则这条告警会被当成噪音而被忽略。
	quiet, quietLogs := newObservedLogger()
	quietEngine := gin.New()
	if err := quietEngine.SetTrustedProxies(nil); err != nil {
		t.Fatalf("SetTrustedProxies(nil) error = %v", err)
	}
	quietEngine.Use(ClientIP(resolver, quiet))
	quietEngine.GET("/probe", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.RemoteAddr = "10.42.0.7:38210"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	quietEngine.ServeHTTP(httptest.NewRecorder(), req)
	if n := len(quietLogs.All()); n != 0 {
		t.Errorf("判定正常时不该有日志，实际 %d 条", n)
	}
}

// TestClientIPNilResolverIsPassThrough 零依赖装配（openapi / routes 子命令）走这一支。
func TestClientIPNilResolverIsPassThrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	if err := engine.SetTrustedProxies(nil); err != nil {
		t.Fatalf("SetTrustedProxies(nil) error = %v", err)
	}
	engine.Use(ClientIP(nil, zap.NewNop()))

	var seen string
	engine.GET("/probe", func(c *gin.Context) {
		seen = c.ClientIP()
		c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequest(http.MethodGet, "/probe", nil)
	req.RemoteAddr = "10.42.0.7:38210"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	engine.ServeHTTP(httptest.NewRecorder(), req)

	if got, want := seen, "10.42.0.7"; got != want {
		t.Fatalf("c.ClientIP() = %q, want %q", got, want)
	}
}
