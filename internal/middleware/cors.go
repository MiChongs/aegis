package middleware

import (
	"net/http"
	"strings"

	"aegis/internal/config"
	corslib "github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// CORS 跨域处理。
//
// **`CORS_ALLOW_ORIGINS` 留空不等于「只影响跨域调用方」。**
// 按 Fetch 规范，浏览器对一切非 GET/HEAD 请求都会带上 `Origin`，
// **同源的 POST/PUT/DELETE 也带**。因此空配置下：
//
//	同源 GET   → 无 Origin → 放行
//	同源 POST  → 有 Origin → 一律 403（空 body，无日志）
//
// 表现就是「页面打得开、列表读得出来，一提交就 403」，而 403 里什么线索都没有。
// 前后端分域（控制台反代到 API）时尤其容易踩：两边都是自己的域名，
// 直觉上根本不觉得这是「跨域」。
//
// 所以装配时必须显式列出控制台的来源，见 CORSGuardWarning。
func CORS(cfg config.CORSConfig) gin.HandlerFunc {
	if !cfg.Enabled {
		return func(c *gin.Context) {
			c.Next()
		}
	}

	corsConfig := corslib.Config{
		AllowMethods:              normalizeCORSValues(cfg.AllowMethods),
		AllowHeaders:              normalizeCORSValues(cfg.AllowHeaders),
		ExposeHeaders:             normalizeCORSValues(cfg.ExposeHeaders),
		AllowCredentials:          cfg.AllowCredentials,
		MaxAge:                    cfg.MaxAge,
		AllowWildcard:             true,
		AllowWebSockets:           true,
		AllowBrowserExtensions:    false,
		AllowFiles:                false,
		AllowPrivateNetwork:       false,
		OptionsResponseStatusCode: http.StatusNoContent,
	}

	allowOrigins := normalizeCORSValues(cfg.AllowOrigins)
	switch {
	case cfg.AllowAllOrigins && !cfg.AllowCredentials:
		corsConfig.AllowAllOrigins = true
	case len(allowOrigins) > 0:
		corsConfig.AllowOrigins = allowOrigins
	default:
		// 未显式配置来源时拒绝一切带 Origin 的请求 —— 含同源的写请求，见上方说明
		corsConfig.AllowOriginFunc = func(string) bool { return false }
	}

	return corslib.New(corsConfig)
}

// CORSGuardWarning 在配置会静默拒绝所有写请求时返回一句可直接展示的告警，
// 否则返回空串。
//
// 单独成函数而不是在 CORS() 里打日志：中间件构造发生在日志器装配的同一批代码里，
// 由调用方决定怎么呈现（启动横幅 / 日志）比在这里硬编码一个 log 依赖更合适。
func CORSGuardWarning(cfg config.CORSConfig) string {
	if !cfg.Enabled {
		return ""
	}
	if cfg.AllowAllOrigins && !cfg.AllowCredentials {
		return ""
	}
	if len(normalizeCORSValues(cfg.AllowOrigins)) > 0 {
		return ""
	}
	return "CORS_ALLOW_ORIGINS 为空：浏览器发出的所有写请求（含同源 POST/PUT/DELETE）都会被拒绝为 403。" +
		"前后端分域部署时请把控制台地址填进 CORS_ALLOW_ORIGINS。"
}

func normalizeCORSValues(values []string) []string {
	items := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			items = append(items, trimmed)
		}
	}
	return items
}
