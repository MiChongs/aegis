package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aegis/internal/config"

	"github.com/gin-gonic/gin"
)

// 空的 CORS 配置会拒掉**同源**的写请求。
//
// 这条不是理论问题：浏览器按 Fetch 规范对一切非 GET/HEAD 请求都带 `Origin`，
// 同源 POST 也带。前后端分域部署时表现为「页面打得开、一提交就 403」，
// 而那个 403 没有 body、没有日志，排查会从业务代码一路找回来。
func TestEmptyAllowOriginsRejectsSameOriginWrites(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(CORS(config.CORSConfig{Enabled: true}))
	router.POST("/api/admin/auth/login", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.GET("/api/public/branding", func(c *gin.Context) { c.Status(http.StatusOK) })

	// 不带 Origin（curl / 服务端调用）：放行
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/admin/auth/login", strings.NewReader("{}")))
	if rec.Code != http.StatusOK {
		t.Fatalf("无 Origin 的 POST 应放行，实得 %d", rec.Code)
	}

	// 带 Origin（任何浏览器发出的 POST，含同源）：被拒
	req := httptest.NewRequest(http.MethodPost, "/api/admin/auth/login", strings.NewReader("{}"))
	req.Header.Set("Origin", "https://console.example.com")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("空白名单下带 Origin 的 POST 应被拒，实得 %d", rec.Code)
	}

	// 同源 GET 不带 Origin，因此不受影响 —— 这正是「读得出来、写不进去」的由来
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/public/branding", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("同源 GET 应放行，实得 %d", rec.Code)
	}
}

// 配上来源之后写请求恢复。
func TestConfiguredOriginAllowsWrites(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(CORS(config.CORSConfig{
		Enabled:      true,
		AllowOrigins: []string{"https://console.example.com"},
		AllowMethods: []string{"GET", "POST", "OPTIONS"},
		AllowHeaders: []string{"Content-Type", "Authorization"},
	}))
	router.POST("/api/admin/auth/login", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodPost, "/api/admin/auth/login", strings.NewReader("{}"))
	req.Header.Set("Origin", "https://console.example.com")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("已配置的来源应放行，实得 %d", rec.Code)
	}

	// 未登记的来源仍然拒绝
	req = httptest.NewRequest(http.MethodPost, "/api/admin/auth/login", strings.NewReader("{}"))
	req.Header.Set("Origin", "https://evil.example.com")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("未登记来源应被拒，实得 %d", rec.Code)
	}
}

// 告警只在「会静默拒绝写请求」的那一种配置下出现，其余情况必须闭嘴 ——
// 一条对所有部署都亮的告警会被训练成背景噪音。
func TestCORSGuardWarningOnlyFiresWhenItWouldBlock(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.CORSConfig
		want bool
	}{
		{"启用但没配来源：会拦所有写请求", config.CORSConfig{Enabled: true}, true},
		{"只有空白项，等同没配", config.CORSConfig{Enabled: true, AllowOrigins: []string{"", "  "}}, true},
		{"配了来源", config.CORSConfig{Enabled: true, AllowOrigins: []string{"https://console.example.com"}}, false},
		{"允许全部来源", config.CORSConfig{Enabled: true, AllowAllOrigins: true}, false},
		{"整个关掉 CORS：中间件直通，不拦", config.CORSConfig{Enabled: false}, false},
		{
			// 带凭据时 AllowAllOrigins 不生效（浏览器也不允许 * + credentials），
			// 于是仍然会拦，必须告警
			name: "允许全部来源但要带凭据：实际仍会拦",
			cfg:  config.CORSConfig{Enabled: true, AllowAllOrigins: true, AllowCredentials: true},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CORSGuardWarning(tc.cfg) != ""
			if got != tc.want {
				t.Fatalf("告警出现 = %v，期望 %v", got, tc.want)
			}
		})
	}
}
