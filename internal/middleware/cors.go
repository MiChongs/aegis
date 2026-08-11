package middleware

import (
	"net/http"
	"strings"

	"aegis/internal/config"
	corslib "github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

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
		// 未显式配置来源时拒绝所有跨域请求；同源请求没有 Origin 头，不受影响。
		corsConfig.AllowOriginFunc = func(string) bool { return false }
	}

	return corslib.New(corsConfig)
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
