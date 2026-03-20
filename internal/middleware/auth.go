package middleware

import (
	"net/http"

	"aegis/internal/service"
	"aegis/pkg/response"
	"github.com/gin-gonic/gin"
)

func Recovery() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		response.Error(c, http.StatusInternalServerError, 50000, "服务器内部错误")
	})
}

func Auth(authService *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := bearerToken(c.GetHeader("Authorization"))
		if token == "" {
			response.Error(c, http.StatusUnauthorized, 40100, "缺少 Authorization Bearer Token")
			c.Abort()
			return
		}
		session, err := authService.ValidateAccessToken(c.Request.Context(), token)
		if err != nil {
			response.Error(c, http.StatusUnauthorized, 40100, err.Error())
			c.Abort()
			return
		}
		c.Set("auth.session", session)
		c.Set("auth.token", token)
		c.Next()
	}
}

func OptionalAuth(authService *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := bearerToken(c.GetHeader("Authorization"))
		if token == "" {
			c.Next()
			return
		}
		session, err := authService.ValidateAccessToken(c.Request.Context(), token)
		if err == nil && session != nil {
			c.Set("auth.session", session)
			c.Set("auth.token", token)
		}
		c.Next()
	}
}

func bearerToken(header string) string {
	const prefix = "Bearer "
	if len(header) <= len(prefix) || header[:len(prefix)] != prefix {
		return ""
	}
	return header[len(prefix):]
}
