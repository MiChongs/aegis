package middleware

import (
	"aegis/internal/service"

	"github.com/gin-gonic/gin"
)

// RequestOrigin 把「浏览器地址栏的源」写进请求 context。
//
// 目前唯一的消费者是 Passkey：WebAuthn 的 RP ID 必须等于当前页面的有效域名
// 或它的可注册后缀，所以服务端只有知道用户是从哪个域打开的，才能签出一份
// 浏览器肯收的挑战（详见 internal/service/security_passkey_rp.go）。
//
// 做成全局中间件而不是在各个 Passkey handler 里各写一遍：注册、登录、
// 管理端与应用网关一共六个入口散在三个文件里，漏掉任何一个都会退回
// 「换个地址就报 relying party ID 错误」，而那个错误只在浏览器里出现、
// 服务端日志上什么都看不到。
func RequestOrigin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if origin := service.ResolveRequestOrigin(c.Request); origin != "" {
			c.Request = c.Request.WithContext(service.ContextWithRequestOrigin(c.Request.Context(), origin))
		}
		c.Next()
	}
}
