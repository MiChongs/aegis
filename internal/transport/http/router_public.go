package httptransport

import (
	"aegis/internal/middleware"

	"github.com/gin-gonic/gin"
)

// 公开入口：站点页面、健康探针、公开元数据、验证码与法律文本。
//
// 本文件只做路由注册，由 NewRouter 按原顺序调用；分组规则见 route_groups.go。

// registerPublicRoutes 注册不需要任何凭据就能访问的入口：站点页面、健康探针、
// 公开元数据，以及只对超管开放的系统监控。
func registerPublicRoutes(router *gin.Engine, h *Handler, deps RouterDeps) {
	var (
		adminService = deps.Admin
	)
	router.GET("/", h.Homepage)
	router.GET("/status", h.StatusPage)
	router.GET("/announcements", h.AnnouncementsPage)
	router.GET("/cons-env", h.EnvPage)
	router.GET("/healthz", h.Healthz)
	router.GET("/readyz", h.Readyz)
	router.GET("/api/system/announcements/active", h.ActiveAnnouncements)
	router.GET("/api/public/branding", h.AdminGetPublicBranding)
	systemMonitor := router.Group("/api/system/monitor")
	systemMonitor.Use(middleware.AdminAuth(adminService), middleware.RequireSuperAdmin())
	{
		systemMonitor.GET("", h.SystemMonitor)
		systemMonitor.GET("/apps", h.SystemMonitorApps)
		systemMonitor.GET("/components", h.SystemMonitorComponents)
		systemMonitor.GET("/history", h.SystemMonitorHistory)
		systemMonitor.GET("/apps/:appkey/components", h.AppMonitorComponents)
		systemMonitor.GET("/apps/:appkey/history", h.AppMonitorHistory)
		systemMonitor.GET("/apps/:appkey", h.AppMonitor)
	}
	router.GET("/api/app/public", h.AppPublic)
	router.GET("/api/functions/signing-key", h.AppFunctionSigningKey)
	router.GET("/api/avatar/:hash", h.AvatarRedirect)
	// 永久头像地址。**免登录**是必须的：这个地址会出现在 <img src> 里，
	// 而浏览器加载图片不会带上 Authorization 头；它也会出现在邮件正文里，
	// 那里根本没有登录态。防遍历靠地址本身带的签名，不靠鉴权中间件。
	router.GET("/api/avatars/:token", h.AvatarImage)
	router.GET("/api/ws", h.WebSocket)
}

// registerPublicCaptchaAndLegalRoutes 注册登录之前就要用的公开入口：
// 验证码与法律文本。要求登录才能读条款是荒谬的，所以它们不在鉴权组里。
func registerPublicCaptchaAndLegalRoutes(router *gin.Engine, h *Handler, deps RouterDeps) {
	// 验证码路由（公开，无需认证）
	captchaGroup := router.Group("/api/captcha")
	{
		captchaGroup.GET("/public-config", h.UserCaptchaPublicConfig) // 登录/注册前端查询验证码策略
		captchaGroup.POST("/generate", h.GenerateCaptcha)
		captchaGroup.POST("/verify", h.VerifyCaptcha)
		captchaGroup.POST("/sms/send", h.SendSMSCode)
		captchaGroup.POST("/sms/verify", h.VerifySMSCode)
		captchaGroup.POST("/verify-click", h.VerifyCaptchaClick)
	}

	// 管理员验证码路由（公开，用于管理员登录前获取验证码）
	adminCaptcha := router.Group("/api/admin/captcha")
	{
		adminCaptcha.POST("/generate", h.AdminGenerateCaptcha)
		adminCaptcha.POST("/verify", h.AdminVerifyCaptcha)
		adminCaptcha.POST("/verify-click", h.VerifyCaptchaClick) // 手性碳点选：captchaId 已是 admin scope，复用核心校验
		adminCaptcha.GET("/config", h.AdminCaptchaPublicConfig)
	}

	// 法律文本（免登录）。登录页与注册页在用户还没有账号时就要链到它，
	// 要求登录才能读条款是荒谬的；放在 /api/legal 而不是 /api/admin/*，
	// 正是为了让它不落进任何一条管理端鉴权规则里。
	legalPublic := router.Group("/api/legal")
	{
		legalPublic.GET("/documents", h.PublicLegalCatalog)
		legalPublic.GET("/documents/:docType", h.PublicLegalDocument)
	}
}
