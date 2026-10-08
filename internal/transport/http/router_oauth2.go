package httptransport

import "github.com/gin-gonic/gin"

// registerOAuth2Routes 注册 OAuth2 授权服务器（Ory Hydra）的托管页面与令牌钩子。
//
// /oauth2/* 是 Hydra 配置里 urls.login / consent / logout / device / error 指向的地址，
// 浏览器由 Hydra 重定向而来，只带一个 challenge；页面自己做 CSRF，不走任何鉴权中间件。
// 令牌钩子只给 Hydra 调，凭 HYDRA_TOKEN_HOOK_SECRET 认证。见 docs/oauth2-provider.md。
func registerOAuth2Routes(router *gin.Engine, h *Handler, _ RouterDeps) {
	pages := router.Group("/oauth2")
	{
		pages.GET("/login", h.OAuth2LoginPage)
		pages.POST("/login", h.OAuth2LoginSubmit)
		pages.POST("/login/mfa", h.OAuth2SecondFactorSubmit)
		pages.GET("/consent", h.OAuth2ConsentPage)
		pages.POST("/consent", h.OAuth2ConsentSubmit)
		pages.GET("/logout", h.OAuth2LogoutPage)
		pages.POST("/logout", h.OAuth2LogoutSubmit)
		pages.GET("/logged-out", h.OAuth2LoggedOut)
		pages.GET("/device", h.OAuth2DevicePage)
		pages.POST("/device", h.OAuth2DeviceSubmit)
		pages.GET("/device/done", h.OAuth2DeviceDone)
		pages.GET("/error", h.OAuth2ErrorPage)
	}
	router.POST("/api/oauth2/hooks/token", h.OAuth2TokenHook)
}
