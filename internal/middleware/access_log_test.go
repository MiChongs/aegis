package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func init() { gin.SetMode(gin.TestMode) }

func newObservedLogger() (*zap.Logger, *observer.ObservedLogs) {
	core, logs := observer.New(zapcore.DebugLevel)
	return zap.New(core), logs
}

// serve 建一条最小路由并打一次请求，返回被记下的日志。
func serve(t *testing.T, method, target string, status int, register func(*gin.Engine, gin.HandlerFunc)) *observer.ObservedLogs {
	t.Helper()
	log, logs := newObservedLogger()
	router := gin.New()
	mw := AccessLog(log, AccessLogSkipPaths()...)
	register(router, mw)
	_ = status

	req := httptest.NewRequest(method, target, nil)
	req.Header.Set("User-Agent", "aegis-test/1.0")
	router.ServeHTTP(httptest.NewRecorder(), req)
	return logs
}

func TestAccessLogRecordsRequestFacts(t *testing.T) {
	logs := serve(t, http.MethodGet, "/api/admin/apps/demo?page=2", http.StatusOK,
		func(r *gin.Engine, mw gin.HandlerFunc) {
			r.Use(RequestID(), mw)
			r.GET("/api/admin/apps/:appkey", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
		})

	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("记下了 %d 条日志，期望 1 条", len(entries))
	}
	entry := entries[0]
	if entry.Level != zapcore.InfoLevel {
		t.Errorf("2xx 应记 info，得到 %s", entry.Level)
	}

	fields := entry.ContextMap()
	if fields["method"] != http.MethodGet {
		t.Errorf("method = %v", fields["method"])
	}
	if fields["path"] != "/api/admin/apps/demo" {
		t.Errorf("path 应是实际请求路径，得到 %v", fields["path"])
	}
	// route 是聚合用的路由模板。只留 path 的话，带 ID 的路径会把同一个接口
	// 打散成成千上万个互不相干的 key，按接口看延迟与错误率就无从下手。
	if fields["route"] != "/api/admin/apps/:appkey" {
		t.Errorf("route 应是路由模板，得到 %v", fields["route"])
	}
	if fields["query"] != "page=2" {
		t.Errorf("query = %v", fields["query"])
	}
	if fields["status"] != int64(http.StatusOK) {
		t.Errorf("status = %v", fields["status"])
	}
	if fields["user_agent"] != "aegis-test/1.0" {
		t.Errorf("user_agent = %v", fields["user_agent"])
	}
	if _, ok := fields["latency"]; !ok {
		t.Error("缺少 latency 字段")
	}
	if id, ok := fields["request_id"].(string); !ok || id == "" {
		t.Error("缺少 request_id：它是把访问日志与业务日志串起来的唯一线索")
	}
}

func TestAccessLogLevelFollowsStatus(t *testing.T) {
	cases := []struct {
		status int
		want   zapcore.Level
	}{
		{http.StatusOK, zapcore.InfoLevel},
		{http.StatusMovedPermanently, zapcore.InfoLevel},
		// 4xx 记 warn：这一档混着「客户端用错了」和「有人在探接口」，都该被看到
		{http.StatusBadRequest, zapcore.WarnLevel},
		{http.StatusForbidden, zapcore.WarnLevel},
		{http.StatusInternalServerError, zapcore.ErrorLevel},
		{http.StatusBadGateway, zapcore.ErrorLevel},
	}
	for _, tc := range cases {
		logs := serve(t, http.MethodGet, "/x", tc.status, func(r *gin.Engine, mw gin.HandlerFunc) {
			r.Use(mw)
			r.GET("/x", func(c *gin.Context) { c.Status(tc.status) })
		})
		entries := logs.All()
		if len(entries) != 1 {
			t.Fatalf("status=%d 记下了 %d 条日志", tc.status, len(entries))
		}
		if entries[0].Level != tc.want {
			t.Errorf("status=%d 记成 %s，期望 %s", tc.status, entries[0].Level, tc.want)
		}
	}
}

// TestAccessLogSkipsProbes 探针由编排系统每几秒打一次且永远 200，
// 记下来只会把真实流量冲淡。
func TestAccessLogSkipsProbes(t *testing.T) {
	for _, path := range AccessLogSkipPaths() {
		logs := serve(t, http.MethodGet, path, http.StatusOK, func(r *gin.Engine, mw gin.HandlerFunc) {
			r.Use(mw)
			r.GET(path, func(c *gin.Context) { c.String(http.StatusOK, "ok") })
		})
		if n := len(logs.All()); n != 0 {
			t.Errorf("%s 被记了 %d 条日志，应当跳过", path, n)
		}
	}
}

// TestAccessLogCarriesHandlerErrors 没有这一条，5xx 日志只会说「500」，
// 真正的原因要去另一条日志里按时间戳猜。
func TestAccessLogCarriesHandlerErrors(t *testing.T) {
	logs := serve(t, http.MethodPost, "/boom", http.StatusInternalServerError,
		func(r *gin.Engine, mw gin.HandlerFunc) {
			r.Use(mw)
			r.POST("/boom", func(c *gin.Context) {
				_ = c.Error(http.ErrHandlerTimeout)
				c.Status(http.StatusInternalServerError)
			})
		})
	entries := logs.All()
	if len(entries) != 1 {
		t.Fatalf("记下了 %d 条日志", len(entries))
	}
	// observer 用 MapObjectEncoder 落字段，zap.Strings 到这里是 []any 而不是 []string
	errs, ok := entries[0].ContextMap()["errors"].([]any)
	if !ok || len(errs) == 0 {
		t.Fatalf("缺少 errors 字段，得到 %#v", entries[0].ContextMap()["errors"])
	}
	if errs[0] != http.ErrHandlerTimeout.Error() {
		t.Errorf("errors = %v", errs)
	}
}

// TestAccessLogRecordsPreRewritePath 网关的 sealed 档会在处理链里改写 URL
// （密文 query 换成明文），而访问日志要记的是客户端实际发来的那个请求。
func TestAccessLogRecordsPreRewritePath(t *testing.T) {
	log, logs := newObservedLogger()
	router := gin.New()
	router.Use(AccessLog(log))
	// 模拟网关：进入 handler 前把 query 换掉
	router.Use(func(c *gin.Context) {
		c.Request.URL.RawQuery = "page=1&size=20"
		c.Next()
	})
	router.GET("/api/v1/apps/demo/config", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps/demo/config?_payload=ciphertext", nil)
	router.ServeHTTP(httptest.NewRecorder(), req)

	fields := logs.All()[0].ContextMap()
	if fields["query"] != "_payload=ciphertext" {
		t.Errorf("query 应是客户端原样发来的那个，得到 %v", fields["query"])
	}
}

// TestAccessLogNilLoggerIsInert 装配失败的路径上 logger 可能是 nil，
// 那时候少几条日志，而不是让每个请求都 panic。
func TestAccessLogNilLoggerIsInert(t *testing.T) {
	router := gin.New()
	router.Use(AccessLog(nil))
	router.GET("/x", func(c *gin.Context) { c.String(http.StatusOK, "ok") })

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/x", nil))
	if recorder.Code != http.StatusOK {
		t.Errorf("nil logger 下请求应正常处理，得到 %d", recorder.Code)
	}
}
