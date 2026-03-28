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
		AllowBrowserExtensions:    true,
		AllowFiles:                true,
		AllowPrivateNetwork:       true,
		OptionsResponseStatusCode: http.StatusNoContent,
	}

	allowOrigins := normalizeCORSValues(cfg.AllowOrigins)
	switch {
	case cfg.AllowAllOrigins && cfg.AllowCredentials:
		corsConfig.AllowOriginFunc = func(origin string) bool {
			return strings.TrimSpace(origin) != ""
		}
	case cfg.AllowAllOrigins:
		corsConfig.AllowAllOrigins = true
	case len(allowOrigins) > 0:
		corsConfig.AllowOrigins = allowOrigins
	default:
		corsConfig.AllowAllOrigins = true
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
