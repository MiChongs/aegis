package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"aegis/internal/service"

	"github.com/gin-gonic/gin"
)

// 中间件与 service 之间只靠一个 context key 对接，任一边改了名字都不会编译失败，
// 表现只是 Passkey 又退回「按静态配置算 RP ID」—— 也就是这次修复之前的老毛病。
func TestRequestOriginReachesRequestContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var seen string
	router := gin.New()
	router.Use(RequestOrigin())
	router.POST("/x", func(c *gin.Context) {
		seen = service.RequestOriginFromContext(c.Request.Context())
		c.Status(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.Header.Set("Origin", "http://127.0.0.1:3000")
	router.ServeHTTP(httptest.NewRecorder(), req)

	if seen != "http://127.0.0.1:3000" {
		t.Fatalf("origin in context = %q, want http://127.0.0.1:3000", seen)
	}
}
