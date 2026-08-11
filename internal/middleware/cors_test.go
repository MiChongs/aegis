package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"aegis/internal/config"
	"github.com/gin-gonic/gin"
)

func TestCORSRejectsUnconfiguredCrossOrigin(t *testing.T) {
	t.Parallel()

	router := gin.New()
	router.Use(CORS(config.CORSConfig{Enabled: true}))
	router.GET("/resource", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	req := httptest.NewRequest(http.MethodGet, "/resource", nil)
	req.Header.Set("Origin", "https://evil.example")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if origin := recorder.Header().Get("Access-Control-Allow-Origin"); origin != "" {
		t.Fatalf("未配置来源时不应返回跨域许可，得到 %q", origin)
	}
}

func TestCORSAllowsExplicitOrigin(t *testing.T) {
	t.Parallel()

	router := gin.New()
	router.Use(CORS(config.CORSConfig{
		Enabled:      true,
		AllowOrigins: []string{"https://console.example.com"},
		AllowMethods: []string{http.MethodGet},
	}))
	router.GET("/resource", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	req := httptest.NewRequest(http.MethodGet, "/resource", nil)
	req.Header.Set("Origin", "https://console.example.com")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if origin := recorder.Header().Get("Access-Control-Allow-Origin"); origin != "https://console.example.com" {
		t.Fatalf("显式来源未被允许，得到 %q", origin)
	}
}

func TestCORSDoesNotCombineWildcardWithCredentials(t *testing.T) {
	t.Parallel()

	router := gin.New()
	router.Use(CORS(config.CORSConfig{
		Enabled:          true,
		AllowAllOrigins:  true,
		AllowCredentials: true,
	}))
	router.GET("/resource", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	req := httptest.NewRequest(http.MethodGet, "/resource", nil)
	req.Header.Set("Origin", "https://evil.example")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if origin := recorder.Header().Get("Access-Control-Allow-Origin"); origin != "" {
		t.Fatalf("凭据模式不得回显任意来源，得到 %q", origin)
	}
}
