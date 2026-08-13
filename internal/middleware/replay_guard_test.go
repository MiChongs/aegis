package middleware

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"aegis/internal/config"
	redisrepo "aegis/internal/repository/redis"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func newGuardForTest(t *testing.T, mutate func(*config.ReplayProtectionConfig)) (*ReplayGuard, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	client := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	cfg := config.ReplayProtectionConfig{
		Enabled:            true,
		NonceWindow:        5 * time.Minute,
		NonceSkew:          30 * time.Second,
		FingerprintTTL:     5 * time.Second,
		SignatureEnabled:   true,
		IdempotencyEnabled: true,
		IdempotencyTTL:     time.Hour,
		IdempotencyLockTTL: 10 * time.Second,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	repo := redisrepo.NewReplayRepository(client, "test")
	return NewReplayGuard(cfg, "test-secret", repo, zap.NewNop()), mr
}

// routerWith 装一个只回 200 的处理器，方便观察中间件的判定
func routerWith(guard *ReplayGuard, route string, handler gin.HandlerFunc) *gin.Engine {
	r := gin.New()
	r.Use(guard.Handler())
	if handler == nil {
		handler = func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) }
	}
	r.POST(route, handler)
	return r
}

func post(r *gin.Engine, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.9:1234"
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// ─────────────────────────────────────────────────────────────────────────
// 回归：验证码必须能被连续原样调用
//
// 这是本次改动要消灭的那个 bug。旧实现里指纹去重对所有非 GET 请求生效，
// 而「换一张」发出的请求体、路径、IP 完全一致，于是第二次点击稳定收到 403。
// ─────────────────────────────────────────────────────────────────────────

func TestCaptchaEndpointsAllowIdenticalRepeats(t *testing.T) {
	paths := []string{
		"/api/captcha/generate",
		"/api/captcha/verify",
		"/api/captcha/sms/send",
		"/api/admin/captcha/generate",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			guard, _ := newGuardForTest(t, nil)
			r := routerWith(guard, path, nil)

			// 连点五次，每一次都必须是 200
			for i := range 5 {
				w := post(r, path, `{"scene":"login"}`, nil)
				if w.Code != http.StatusOK {
					t.Fatalf("第 %d 次请求返回 %d，期望 200；body=%s", i+1, w.Code, w.Body.String())
				}
			}
		})
	}
}

func TestPolicyTableClassification(t *testing.T) {
	cases := []struct {
		method string
		path   string
		want   ReplayPolicy
	}{
		{"POST", "/api/captcha/generate", PolicyOff},
		{"POST", "/api/admin/captcha/verify-click", PolicyOff},
		{"POST", "/api/auth/login", PolicyOff},
		{"POST", "/api/v1/apps/demo/auth/login", PolicyOff},
		{"POST", "/api/admin/apps/demo/wallet/adjust", PolicyGuarded},
		{"POST", "/api/admin/payments/8812/refund", PolicyGuarded},
		// 未登记的写接口回落到 idempotent，也就是不做指纹去重
		{"POST", "/api/admin/apps/demo/banners", PolicyIdempotent},
		{"PUT", "/api/admin/system/settings", PolicyIdempotent},
		// 段锚定：captcha-config 是写配置的接口，不能被 /api/captcha 的规则吃掉
		{"PUT", "/api/admin/apps/demo/captcha-config", PolicyIdempotent},
	}

	for _, tc := range cases {
		got, _ := ResolveReplayPolicy(tc.method, tc.path)
		if got != tc.want {
			t.Errorf("%s %s → %s，期望 %s", tc.method, tc.path, got, tc.want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────
// 指纹兜底：只对 guarded 路由生效，且失败后要归还占位
// ─────────────────────────────────────────────────────────────────────────

func TestGuardedRouteBlocksDuplicateWithConflict(t *testing.T) {
	guard, _ := newGuardForTest(t, nil)
	r := routerWith(guard, "/api/admin/apps/:appkey/wallet/adjust", nil)

	first := post(r, "/api/admin/apps/demo/wallet/adjust", `{"amount":"10.00"}`, nil)
	if first.Code != http.StatusOK {
		t.Fatalf("首次请求应放行，得到 %d", first.Code)
	}

	second := post(r, "/api/admin/apps/demo/wallet/adjust", `{"amount":"10.00"}`, nil)
	// 409 而不是 403：这不是权限问题，调用方该做的是稍后重试
	if second.Code != http.StatusConflict {
		t.Fatalf("重复请求应回 409，得到 %d；body=%s", second.Code, second.Body.String())
	}
	if second.Header().Get("Retry-After") == "" {
		t.Error("409 必须带 Retry-After，否则调用方不知道该等多久")
	}
}

func TestGuardedFingerprintReleasedOnFailure(t *testing.T) {
	guard, _ := newGuardForTest(t, nil)
	fail := true
	r := routerWith(guard, "/api/admin/apps/:appkey/wallet/adjust", func(c *gin.Context) {
		if fail {
			c.JSON(http.StatusBadRequest, gin.H{"code": 40001})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	if got := post(r, "/api/admin/apps/demo/wallet/adjust", `{"amount":"x"}`, nil).Code; got != http.StatusBadRequest {
		t.Fatalf("首次应由 handler 回 400，得到 %d", got)
	}

	// 用户改完参数立刻重试。失败的那次没有产生副作用，占位必须已经还回去，
	// 否则他会撞上自己刚才那次失败，且错误信息是"重复请求"。
	fail = false
	if got := post(r, "/api/admin/apps/demo/wallet/adjust", `{"amount":"x"}`, nil).Code; got != http.StatusOK {
		t.Fatalf("失败后重试应被放行，得到 %d", got)
	}
}

// ─────────────────────────────────────────────────────────────────────────
// 幂等键
// ─────────────────────────────────────────────────────────────────────────

func TestIdempotencyReplaysFirstResponse(t *testing.T) {
	guard, _ := newGuardForTest(t, nil)
	calls := 0
	r := routerWith(guard, "/api/admin/apps/:appkey/wallet/adjust", func(c *gin.Context) {
		calls++
		c.JSON(http.StatusOK, gin.H{"orderId": fmt.Sprintf("order-%d", calls)})
	})

	headers := map[string]string{HeaderIdempotencyKey: "req-0001-abcdef"}
	body := `{"amount":"10.00"}`

	first := post(r, "/api/admin/apps/demo/wallet/adjust", body, headers)
	second := post(r, "/api/admin/apps/demo/wallet/adjust", body, headers)

	if calls != 1 {
		t.Fatalf("handler 应只执行一次，实际 %d 次", calls)
	}
	if second.Code != http.StatusOK {
		t.Fatalf("重放应回 200 而不是错误，得到 %d", second.Code)
	}
	if first.Body.String() != second.Body.String() {
		t.Fatalf("重放的响应体必须与首次逐字一致：\n首次 %s\n重放 %s",
			first.Body.String(), second.Body.String())
	}
	if second.Header().Get(HeaderIdempotentReplayed) != "true" {
		t.Error("重放必须带 Idempotent-Replayed 头，否则调用方分不出这是新结果还是旧结果")
	}
}

func TestIdempotencyRejectsSameKeyDifferentBody(t *testing.T) {
	guard, _ := newGuardForTest(t, nil)
	r := routerWith(guard, "/api/admin/apps/:appkey/wallet/adjust", nil)
	headers := map[string]string{HeaderIdempotencyKey: "req-0002-abcdef"}

	post(r, "/api/admin/apps/demo/wallet/adjust", `{"amount":"10.00"}`, headers)
	w := post(r, "/api/admin/apps/demo/wallet/adjust", `{"amount":"999.00"}`, headers)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("同键不同内容应回 422，得到 %d；body=%s", w.Code, w.Body.String())
	}
}

func TestIdempotencyIsScopedPerCaller(t *testing.T) {
	guard, _ := newGuardForTest(t, nil)
	calls := 0
	r := routerWith(guard, "/api/admin/apps/:appkey/wallet/adjust", func(c *gin.Context) {
		calls++
		c.JSON(http.StatusOK, gin.H{"n": calls})
	})
	body := `{"amount":"10.00"}`

	post(r, "/api/admin/apps/demo/wallet/adjust", body, map[string]string{
		HeaderIdempotencyKey: "shared-key-0003",
		"Authorization":      "Bearer alice-token-value",
	})
	post(r, "/api/admin/apps/demo/wallet/adjust", body, map[string]string{
		HeaderIdempotencyKey: "shared-key-0003",
		"Authorization":      "Bearer bob-token-value",
	})

	// 两个不同调用方碰巧用了同一个键，绝不能互相拿到对方的响应
	if calls != 2 {
		t.Fatalf("不同调用方的同名幂等键应各自执行，实际只执行了 %d 次", calls)
	}
}

func TestIdempotencyDoesNotCacheServerErrors(t *testing.T) {
	guard, _ := newGuardForTest(t, nil)
	boom := true
	r := routerWith(guard, "/api/admin/apps/:appkey/wallet/adjust", func(c *gin.Context) {
		if boom {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 50000})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	headers := map[string]string{HeaderIdempotencyKey: "req-0004-abcdef"}
	body := `{"amount":"10.00"}`

	post(r, "/api/admin/apps/demo/wallet/adjust", body, headers)

	// 5xx 是服务端自己的问题，重试就该真的重跑一遍；
	// 把一次 500 缓存下来等于把偶发故障钉死成永久故障。
	boom = false
	w := post(r, "/api/admin/apps/demo/wallet/adjust", body, headers)
	if w.Code != http.StatusOK {
		t.Fatalf("5xx 不该被缓存，重试应重新执行并成功，得到 %d", w.Code)
	}
}

func TestIdempotencyKeyValidation(t *testing.T) {
	guard, _ := newGuardForTest(t, nil)
	r := routerWith(guard, "/api/admin/apps/:appkey/wallet/adjust", nil)

	for _, bad := range []string{"short", strings.Repeat("x", 256), "has:colon:inside", "has space"} {
		w := post(r, "/api/admin/apps/demo/wallet/adjust", `{}`,
			map[string]string{HeaderIdempotencyKey: bad})
		if w.Code != http.StatusBadRequest {
			t.Errorf("非法幂等键 %q 应回 400，得到 %d", bad, w.Code)
		}
	}
}

// 双击：同一进程内的并发同键请求，只能有一个真正执行
func TestIdempotencyCollapsesConcurrentDoubleSubmit(t *testing.T) {
	guard, _ := newGuardForTest(t, nil)
	var mu sync.Mutex
	calls := 0
	r := routerWith(guard, "/api/admin/apps/:appkey/wallet/adjust", func(c *gin.Context) {
		mu.Lock()
		calls++
		mu.Unlock()
		time.Sleep(40 * time.Millisecond) // 模拟一次真实的写操作
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	headers := map[string]string{HeaderIdempotencyKey: "req-0005-abcdef"}
	var wg sync.WaitGroup
	codes := make([]int, 4)
	for i := range codes {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			codes[idx] = post(r, "/api/admin/apps/demo/wallet/adjust", `{"amount":"10.00"}`, headers).Code
		}(i)
	}
	wg.Wait()

	if calls != 1 {
		t.Fatalf("并发同键请求只能执行一次，实际 %d 次", calls)
	}
	for i, code := range codes {
		if code != http.StatusOK && code != http.StatusConflict {
			t.Errorf("第 %d 个并发请求返回 %d，期望 200（执行或回放）或 409（执行中）", i, code)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────
// Nonce 层：签名必须先于 Nonce 消耗
// ─────────────────────────────────────────────────────────────────────────

func TestBadSignatureDoesNotBurnNonce(t *testing.T) {
	guard, _ := newGuardForTest(t, nil)
	r := routerWith(guard, "/api/admin/apps/:appkey/wallet/adjust", nil)

	const path = "/api/admin/apps/demo/wallet/adjust"
	body := `{"amount":"10.00"}`
	ts := fmt.Sprintf("%d", time.Now().Unix())
	nonce := "nonce-0006-abcdef"

	bad := post(r, path, body, map[string]string{
		headerTimestamp: ts, headerNonce: nonce, headerSignature: "AAAAwrongsignature==",
	})
	if bad.Code != http.StatusForbidden {
		t.Fatalf("错误签名应回 403，得到 %d", bad.Code)
	}

	// 同一个 Nonce 配正确签名重试：它一次都没成功过，不该被判成重放。
	// 旧实现先消耗 Nonce 再验签，这里会拿到"重复请求"。
	payload := ts + "\n" + nonce + "\n" + http.MethodPost + "\n" + path + "\n" + sha256Hex([]byte(body))
	good := post(r, path, body, map[string]string{
		headerTimestamp: ts,
		headerNonce:     nonce,
		headerSignature: computeHMAC(guard.deriveSecret(&gin.Context{Request: httptest.NewRequest(http.MethodPost, path, nil)}), payload),
	})
	if good.Code != http.StatusOK {
		t.Fatalf("签名纠正后重试应放行，得到 %d；body=%s", good.Code, good.Body.String())
	}
}

func TestNonceReuseReturnsConflict(t *testing.T) {
	guard, _ := newGuardForTest(t, func(c *config.ReplayProtectionConfig) {
		c.SignatureEnabled = false
	})
	r := routerWith(guard, "/api/admin/apps/:appkey/wallet/adjust", nil)

	const path = "/api/admin/apps/demo/wallet/adjust"
	headers := map[string]string{
		headerTimestamp: fmt.Sprintf("%d", time.Now().Unix()),
		headerNonce:     "nonce-0007-abcdef",
	}

	if got := post(r, path, `{}`, headers).Code; got != http.StatusOK {
		t.Fatalf("首次应放行，得到 %d", got)
	}
	second := post(r, path, `{}`, headers)
	if second.Code != http.StatusConflict {
		t.Fatalf("Nonce 复用应回 409，得到 %d", second.Code)
	}
}

// Redis 挂掉时必须放行：防重放是纵深防御的一层，不是唯一一层
func TestRedisOutageFailsOpen(t *testing.T) {
	guard, mr := newGuardForTest(t, nil)
	r := routerWith(guard, "/api/admin/apps/:appkey/wallet/adjust", nil)
	mr.Close()

	w := post(r, "/api/admin/apps/demo/wallet/adjust", `{"amount":"10.00"}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("Redis 不可用时应放行，得到 %d；body=%s", w.Code, w.Body.String())
	}
}

// 幂等记录的编解码要能跨进程还原（回放依赖它）
func TestIdempotencyRecordRoundTrip(t *testing.T) {
	record := redisrepo.IdempotencyRecord{
		Status:      201,
		Body:        []byte(`{"orderId":"o-1"}`),
		ContentType: "application/json; charset=utf-8",
		RequestHash: "abc123",
		CompletedAt: time.Now().Unix(),
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back redisrepo.IdempotencyRecord
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(back.Body) != string(record.Body) || back.Status != record.Status {
		t.Fatalf("往返后不一致：%+v", back)
	}
}
