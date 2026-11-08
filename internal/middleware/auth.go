package middleware

import (
	"errors"
	"net/http"

	authdomain "aegis/internal/domain/auth"
	"aegis/internal/service"
	apperrors "aegis/pkg/errors"
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
			response.Error(c, http.StatusUnauthorized, authFailureCode(err), err.Error())
			c.Abort()
			return
		}
		c.Set("auth.session", session)
		c.Set("auth.token", token)
		c.Request = c.Request.WithContext(authdomain.WithSession(c.Request.Context(), session))
		registerUserIDAlias(c, authService, session)
		c.Next()
	}
}

// registerUserIDAlias 让这次响应里调用者自己的主键一律以对外 ID 出现（docs/user-public-id.md）。
func registerUserIDAlias(c *gin.Context, authService *service.AuthService, session *authdomain.Session) {
	if session == nil {
		return
	}
	publicID := authService.PublicIDs().Of(c.Request.Context(), session.UserID)
	response.SetUserIDAlias(c, session.UserID, publicID)
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
			c.Request = c.Request.WithContext(authdomain.WithSession(c.Request.Context(), session))
			registerUserIDAlias(c, authService, session)
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

// authFailureCode 令牌失效一律报 40100（客户端据此刷新令牌），只有「被设备数控制挤下线」与
// 「被设备管理移除」原样透出：这两种情况刷新令牌也一并失效了，客户端要据此提示用户并回到登录页，
// 而不是先刷新、失败、再报一句笼统的「登录已过期」（docs/device-limit.md）。
func authFailureCode(err error) int {
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) && (appErr.Code == 40130 || appErr.Code == 40131) {
		return appErr.Code
	}
	return 40100
}
