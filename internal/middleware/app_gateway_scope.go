package middleware

import (
	"net/http"

	authdomain "aegis/internal/domain/auth"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// AppGatewayTokenScope 确保 Bearer 令牌属于路径上的那个应用。
//
// 令牌里带 appid，业务 handler 也一律按令牌里的 appid 取数据，所以跨应用拿不到别人的数据 ——
// 但如果不校验，用 A 应用的令牌请求 `/api/v1/apps/B/me` 会**成功**并返回 A 的资料。
// 没有越权，却是一种最难查的错：接入方以为自己在调 B，拿到的却是 A 的数据，
// 而两边都不报错。与其让它静默地对，不如在这里明确拒掉。
//
// 挂在 middleware.Auth 之后：Auth 负责证明令牌有效，这里只负责证明它是这个应用的。
func AppGatewayTokenScope() gin.HandlerFunc {
	return func(c *gin.Context) {
		pathAppID, hasPathApp := gatewayAppID(c)
		session, hasSession := gatewaySession(c)
		if hasPathApp && hasSession && session.AppID != pathAppID {
			response.Error(c, http.StatusForbidden, 40372,
				"访问令牌不属于该应用，请用本应用的登录结果换取令牌")
			c.Abort()
			return
		}
		c.Next()
	}
}

// gatewayAppID 取 AppGateway 拆包时写进上下文的应用 ID。
func gatewayAppID(c *gin.Context) (int64, bool) {
	value, exists := c.Get("aegis.app_id")
	if !exists {
		return 0, false
	}
	appID, ok := value.(int64)
	return appID, ok && appID > 0
}

func gatewaySession(c *gin.Context) (*authdomain.Session, bool) {
	value, exists := c.Get("auth.session")
	if !exists {
		return nil, false
	}
	session, ok := value.(*authdomain.Session)
	return session, ok && session != nil
}
