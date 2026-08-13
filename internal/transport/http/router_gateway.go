package httptransport

import (
	"aegis/internal/middleware"

	"github.com/gin-gonic/gin"
)

// 应用接入网关 /api/v1/apps/:appkey/*。
//
// 本文件只做路由注册，由 NewRouter 按原顺序调用；分组规则见 route_groups.go。

// registerGatewayRoutes 注册应用接入网关 —— 接入方唯一需要认识的命名空间。
// 完整规格见 docs/app-integration.md。
func registerGatewayRoutes(router *gin.Engine, h *Handler, deps RouterDeps) {
	var (
		authService = deps.Auth
	)
	// 应用接入网关：接入方唯一需要认识的命名空间。
	// 同一批路径在 standard / signed / sealed 三档下结构完全一致，
	// 由 middleware.AppGateway 按应用策略决定请求的包装方式。
	appGateway := router.Group("/api/v1/apps/:appkey")
	{
		appGateway.GET("/config", h.AppConfig)
		appGateway.POST("/captcha", h.AppCaptcha)
		appGateway.POST("/auth/sms/code", h.AppSMSCode)
		appGateway.POST("/auth/register", h.AppRegister)
		appGateway.POST("/auth/login", h.AppLogin)
		appGateway.POST("/auth/refresh", h.AppRefresh)
		appGateway.POST("/auth/2fa/verify", h.AppSecondFactor)
		appGateway.POST("/auth/oauth/url", h.AppOAuthURL)
		// 回跳由第三方平台重定向浏览器发起，客户端无法给它签名或加密，
		// 因此 AppGateway 对该路径放行（详见 AppOAuthCallback 的注释）
		appGateway.GET("/auth/oauth/callback", h.AppOAuthCallback)
		appGateway.POST("/auth/oauth/exchange", h.AppOAuthExchange)
		// 登录之前就要用的入口：应用由路径注入（见 injectGatewayAppID）
		appGateway.POST("/auth/email/code", h.AppEmailCode)
		appGateway.POST("/auth/email/verify", h.AppEmailVerify)
		appGateway.POST("/auth/password/forgot", h.AppPasswordForgot)
		appGateway.POST("/auth/password/reset/verify", h.AppPasswordResetVerify)
		appGateway.POST("/auth/passkey/options", h.AppPasskeyOptions)
		appGateway.POST("/auth/passkey/login", h.AppPasskeyLogin)
		// 免登录的内容接口
		appGateway.GET("/banners", h.AppBanners)
		// 点击上报：banners.click_count 从建表起就在，却从来没有代码写过它。
		// 没有这条接口，控制台上的「点击 0」既可能是真没人点、也可能是根本没统计。
		appGateway.POST("/banners/:bannerId/click", h.AppBannerClick)
		appGateway.GET("/notices", h.AppNotices)
		appGateway.GET("/version/check", h.AppVersionCheck)
	}

	// 网关内需要 Bearer 令牌的部分。
	//
	// AppGatewayTokenScope 挂在 Auth 之后：Auth 证明令牌有效，它证明令牌是**这个应用**的 ——
	// 否则拿 A 应用的令牌请求 /apps/B/me 会成功并返回 A 的资料，两边都不报错。
	//
	// 这一组存在的意义是让接入方只认识一个命名空间：同样的三档包装、同样的信封、
	// 同样的错误码，覆盖登录之后客户端真正要用的全部能力。旧的 /api/user/* 等
	// 命名空间原样保留，由各应用的 allowLegacy 控制，两者正交。
	appGatewayAuthed := router.Group("/api/v1/apps/:appkey")
	appGatewayAuthed.Use(middleware.Auth(authService), middleware.AppGatewayTokenScope())
	{
		appGatewayAuthed.POST("/auth/logout", h.AppLogout)
		appGatewayAuthed.POST("/auth/password/verify", h.VerifyPassword)
		appGatewayAuthed.POST("/auth/password/change", h.ChangePassword)
		appGatewayAuthed.POST("/auth/oauth/bind/url", h.OAuthBindURL)
		appGatewayAuthed.GET("/auth/oauth/bindings", h.ListMyOAuthBindings)
		appGatewayAuthed.DELETE("/auth/oauth/bindings/:provider", h.UnbindMyOAuthProvider)

		// 卡密：兑换与「我的授权」。授权卡登录本身不在这里 ——
		// 它是 /auth/login 的一档 method，加登录方式不加路由。
		appGatewayAuthed.POST("/card-keys/redeem", h.AppRedeemCardKey)
		appGatewayAuthed.GET("/card-keys/mine", h.AppMyCardKeys)

		// 当前用户：资料 / 设置 / 安全概览
		appGatewayAuthed.GET("/me", h.AppMe)
		appGatewayAuthed.GET("/me/profile", h.Profile)
		appGatewayAuthed.PUT("/me/profile", h.UpdateProfile)
		appGatewayAuthed.POST("/me/profile/changes/confirm", h.ConfirmProfileChange)
		appGatewayAuthed.POST("/me/avatar", h.UploadUserAvatar)
		appGatewayAuthed.DELETE("/me/avatar", h.RemoveUserAvatar)
		appGatewayAuthed.GET("/me/avatar/history", h.ListUserAvatarHistory)
		appGatewayAuthed.POST("/me/avatar/restore", h.RestoreUserAvatar)
		appGatewayAuthed.GET("/me/settings", h.Settings)
		appGatewayAuthed.PUT("/me/settings", h.UpdateSettings)
		appGatewayAuthed.GET("/me/security", h.Security)

		// 二次认证
		appGatewayAuthed.POST("/me/2fa/totp/enroll", h.BeginTOTPEnrollment)
		appGatewayAuthed.POST("/me/2fa/totp/enable", h.EnableTOTP)
		appGatewayAuthed.POST("/me/2fa/totp/disable", h.DisableTOTP)
		appGatewayAuthed.GET("/me/2fa/recovery-codes", h.ListRecoveryCodes)
		appGatewayAuthed.POST("/me/2fa/recovery-codes", h.GenerateRecoveryCodes)
		appGatewayAuthed.POST("/me/2fa/recovery-codes/regenerate", h.RegenerateRecoveryCodes)

		// Passkey 管理
		appGatewayAuthed.GET("/me/passkeys", h.ListPasskeys)
		appGatewayAuthed.POST("/me/passkeys/options", h.BeginPasskeyRegistration)
		appGatewayAuthed.POST("/me/passkeys", h.FinishPasskeyRegistration)
		appGatewayAuthed.DELETE("/me/passkeys/:credentialId", h.DeletePasskey)

		// 会话与登录审计
		appGatewayAuthed.GET("/me/sessions", h.UserSessions)
		appGatewayAuthed.DELETE("/me/sessions/:tokenHash", h.RevokeUserSession)
		appGatewayAuthed.POST("/me/sessions/revoke-all", h.RevokeAllUserSessions)
		appGatewayAuthed.GET("/me/audits/login", h.UserLoginAudits)
		appGatewayAuthed.GET("/me/audits/sessions", h.UserSessionAudits)

		// 签到 / 积分 / 排行榜
		appGatewayAuthed.GET("/signin/status", h.SignInStatus)
		appGatewayAuthed.POST("/signin", h.SignIn)
		appGatewayAuthed.GET("/signin/history", h.SignInHistory)
		appGatewayAuthed.GET("/points/overview", h.PointsOverview)
		appGatewayAuthed.GET("/points/level", h.MyLevel)
		appGatewayAuthed.GET("/points/levels", h.PointsLevels)
		appGatewayAuthed.GET("/points/integral-transactions", h.IntegralTransactions)
		appGatewayAuthed.GET("/points/experience-transactions", h.ExperienceTransactions)
		appGatewayAuthed.GET("/leaderboard/summary", h.LeaderboardSummary)
		appGatewayAuthed.GET("/leaderboard/me", h.LeaderboardMe)
		appGatewayAuthed.GET("/leaderboard/points/:type", h.LeaderboardPoints)
		appGatewayAuthed.GET("/leaderboard/signin/:type", h.LeaderboardSignIn)

		// 站内信
		appGatewayAuthed.GET("/notifications", h.Notifications)
		appGatewayAuthed.GET("/notifications/unread-count", h.NotificationUnreadCount)
		appGatewayAuthed.POST("/notifications/read", h.ReadNotification)
		appGatewayAuthed.POST("/notifications/read-batch", h.ReadNotificationsBatch)
		appGatewayAuthed.POST("/notifications/read-all", h.ReadAllNotifications)
		appGatewayAuthed.POST("/notifications/clear", h.ClearNotifications)
		appGatewayAuthed.DELETE("/notifications/:notificationId", h.DeleteNotification)

		// 钱包 / 会员 / 支付
		appGatewayAuthed.GET("/wallet", h.MyWallet)
		appGatewayAuthed.GET("/wallet/transactions", h.MyWalletTransactions)
		appGatewayAuthed.POST("/wallet/consume", h.WalletConsume)
		appGatewayAuthed.GET("/vip/plans", h.VipPlans)
		appGatewayAuthed.GET("/vip/status", h.MyVipStatus)
		appGatewayAuthed.GET("/vip/transactions", h.MyVipTransactions)
		appGatewayAuthed.POST("/vip/purchase", h.PurchaseVip)
		appGatewayAuthed.POST("/vip/trial", h.ClaimVipTrial)
		appGatewayAuthed.GET("/pay/orders", h.PaymentOrders)
		appGatewayAuthed.POST("/pay/orders", h.CreatePaymentOrder)
		appGatewayAuthed.GET("/pay/orders/:orderNo", h.PaymentOrderDetail)

		// 存储
		appGatewayAuthed.POST("/storage/upload", h.StorageUpload)
		appGatewayAuthed.POST("/storage/object-link", h.StorageObjectLink)

		// 工单（用户自助）
		appGatewayAuthed.GET("/tickets", h.UserListTickets)
		appGatewayAuthed.POST("/tickets", h.UserCreateTicket)
		appGatewayAuthed.GET("/tickets/categories", h.UserListTicketCategories)
		appGatewayAuthed.POST("/tickets/attachments", h.UserUploadTicketAttachment)
		appGatewayAuthed.GET("/tickets/:ticketId", h.UserGetTicket)
		appGatewayAuthed.POST("/tickets/:ticketId/replies", h.UserReplyTicket)
		appGatewayAuthed.POST("/tickets/:ticketId/rating", h.UserRateTicket)
		appGatewayAuthed.POST("/tickets/:ticketId/cancel", h.UserCancelTicket)
	}
}
