package httptransport

import (
	"aegis/internal/middleware"

	"github.com/gin-gonic/gin"
)

// 应用用户端与旧明文认证命名空间。
//
// 本文件只做路由注册，由 NewRouter 按原顺序调用；分组规则见 route_groups.go。

// registerLegacyAuthRoutes 注册旧明文认证命名空间，由各应用的 allowLegacy 开关控制。
func registerLegacyAuthRoutes(router *gin.Engine, h *Handler, deps RouterDeps) {
	var (
		authService = deps.Auth
	)
	auth := router.Group("/api/auth")
	{
		auth.POST("/register/password", h.PasswordRegister)
		auth.POST("/login/password", h.PasswordLogin)
		auth.GET("/oauth2/providers", h.AppOAuthPublicProviders)
		auth.POST("/oauth2/auth-url", h.OAuthAuthURL)
		auth.GET("/oauth2/callback", h.OAuthCallback)
		auth.POST("/oauth2/mobile-login", h.OAuthMobileLogin)
		auth.POST("/oauth2/bind-url", middleware.Auth(authService), h.OAuthBindURL)
		auth.GET("/oauth2/bindings", middleware.Auth(authService), h.ListMyOAuthBindings)
		auth.DELETE("/oauth2/bindings/:provider", middleware.Auth(authService), h.UnbindMyOAuthProvider)
		auth.POST("/2fa/verify", h.VerifySecondFactor)
		auth.POST("/passkey/options", h.PasskeyAuthOptions)
		auth.POST("/passkey/auth-options", h.PasskeyAuthOptions)
		auth.POST("/passkey/verify", h.PasskeyLogin)
		auth.POST("/passkey/login", h.PasskeyLogin)
		auth.POST("/refresh", h.Refresh)
		auth.POST("/logout", middleware.Auth(authService), h.Logout)
		auth.POST("/password/verify", middleware.Auth(authService), h.VerifyPassword)
		auth.POST("/password/change", middleware.Auth(authService), h.ChangePassword)
	}
}

// registerUserRoutes 注册应用用户端：工单、抽奖、资料与设置、积分、排行榜、站内信、邮件。
func registerUserRoutes(router *gin.Engine, h *Handler, deps RouterDeps) {
	var (
		authService = deps.Auth
	)
	// ─── 工单系统（应用用户端）────────────────────────────
	userTickets := router.Group("/api/user/tickets")
	userTickets.Use(middleware.Auth(authService))
	{
		userTickets.GET("/categories", h.UserListTicketCategories)
		userTickets.POST("/attachments", h.UserUploadTicketAttachment)
		userTickets.GET("", h.UserListTickets)
		userTickets.POST("", h.UserCreateTicket)
		userTickets.GET("/:ticketId", h.UserGetTicket)
		userTickets.POST("/:ticketId/replies", h.UserReplyTicket)
		userTickets.POST("/:ticketId/rating", h.UserRateTicket)
		userTickets.POST("/:ticketId/cancel", h.UserCancelTicket)
	}

	appFunctions := router.Group("/api/apps/:appkey/functions")
	{
		appFunctions.POST("/:functionName/invoke", h.InvokeAppFunction)
	}

	// 用户抽奖路由
	lotteryUser := router.Group("/api/lottery")
	lotteryUser.Use(middleware.Auth(authService))
	{
		lotteryUser.GET("/activities", h.UserLotteryActivities)
		lotteryUser.GET("/activities/:id", h.UserLotteryActivityDetail)
		lotteryUser.GET("/activities/:id/prizes", h.UserLotteryActivityPrizes)
		lotteryUser.GET("/activities/:id/verify", h.UserLotteryVerify)
		lotteryUser.POST("/join", h.UserLotteryJoin)
		lotteryUser.POST("/draw", h.UserLotteryDraw)
		lotteryUser.GET("/draws", h.UserLotteryDrawHistory)
	}

	userPublic := router.Group("/api/user")
	{
		userPublic.GET("/banner", h.UserBanner)
		userPublic.GET("/notice", h.UserNotice)
		userPublic.GET("/level/config", h.LegacyLevelConfig)
		userPublic.GET("/check-version", h.CheckVersion)
	}

	userSettings := router.Group("/api/user-settings")
	userSettings.Use(middleware.Auth(authService))
	{
		userSettings.GET("", h.LegacyUserSettings)
		userSettings.GET("/categories", h.UserSettingCategories)
		userSettings.GET("/auto-sign/status", h.LegacyAutoSignStatus)
		userSettings.POST("/update", h.LegacyUpdateUserSettings)
		userSettings.POST("/reset", h.LegacyResetUserSettings)
		userSettings.POST("/auto-sign/test-notification", h.LegacyAutoSignTestNotification)
	}

	user := router.Group("/api/user")
	user.Use(middleware.Auth(authService))
	{
		user.POST("/my", h.My)
		user.POST("/daily", h.LegacyDailySign)
		user.POST("/create-site", h.CreateSite)
		user.POST("/search-site", h.SearchSites)
		user.GET("/site-list", h.SiteList)
		user.GET("/site-detail", h.SiteDetail)
		user.POST("/my-site", h.MySites)
		user.POST("/resubmit-site", h.ResubmitSite)
		user.PUT("/update-site", h.UpdateSite)
		user.DELETE("/delete-site", h.DeleteSite)
		user.POST("/role/apply", h.SubmitRoleApplication)
		user.GET("/role/applications", h.RoleApplications)
		user.GET("/role/applications/:applicationId", h.RoleApplicationDetail)
		user.PUT("/role/applications/:applicationId/cancel", h.CancelRoleApplication)
		user.GET("/role/available", h.AvailableRoles)
		user.POST("/role/applications/:applicationId/resubmit", h.ResubmitRoleApplication)
		user.GET("/profile", h.Profile)
		user.PUT("/profile", h.UpdateProfile)
		user.POST("/profile/changes/confirm", h.ConfirmProfileChange)
		user.POST("/profile/avatar", h.UploadUserAvatar)
		user.POST("/profile/upload-avatar", h.UploadUserAvatar)
		user.GET("/settings", h.Settings)
		user.PUT("/settings", h.UpdateSettings)
		user.POST("/level/info", h.LegacyMyLevel)
		user.POST("/level/ranking", h.LegacyLevelRanking)
		user.POST("/dailyRank", h.LegacyDailyRank)
		user.POST("/integralRank", h.LegacyIntegralRank)
		user.POST("/settings/reset", h.LegacyResetUserSettings)
		user.GET("/security", h.Security)
		user.POST("/two-factor/enroll", h.BeginTOTPEnrollment)
		user.POST("/two-factor/enable", h.EnableTOTP)
		user.POST("/two-factor/disable", h.DisableTOTP)
		user.GET("/two-factor/recovery-codes", h.ListRecoveryCodes)
		user.POST("/two-factor/recovery-codes", h.GenerateRecoveryCodes)
		user.POST("/two-factor/recovery-codes/regenerate", h.RegenerateRecoveryCodes)
		user.GET("/passkey", h.ListPasskeys)
		user.POST("/passkey/register/options", h.BeginPasskeyRegistration)
		user.POST("/passkey/register", h.FinishPasskeyRegistration)
		user.DELETE("/passkey/:credentialId", h.DeletePasskey)
		user.GET("/auto-sign/status", h.LegacyAutoSignStatus)
		user.POST("/auto-sign/test-notification", h.LegacyAutoSignTestNotification)
		user.GET("/audits/login", h.UserLoginAudits)
		user.GET("/audits/login/export", h.ExportUserLoginAudits)
		user.GET("/audits/sessions", h.UserSessionAudits)
		user.GET("/audits/sessions/export", h.ExportUserSessionAudits)
		user.GET("/sessions", h.UserSessions)
		user.DELETE("/sessions/:tokenHash", h.RevokeUserSession)
		user.POST("/sessions/revoke-all", h.RevokeAllUserSessions)
		user.GET("/signin/status", h.SignInStatus)
		user.GET("/signin/history", h.SignInHistory)
		user.GET("/signin/history/export", h.ExportUserSignInHistory)
		user.POST("/signin", h.SignIn)
	}

	points := router.Group("/api/points")
	points.Use(middleware.Auth(authService))
	{
		points.GET("/overview", h.PointsOverview)
		points.GET("/levels", h.PointsLevels)
		points.GET("/level", h.MyLevel)
		points.GET("/integral-transactions", h.IntegralTransactions)
		points.GET("/experience-transactions", h.ExperienceTransactions)
		points.GET("/rankings", h.PointsRankings)
	}

	// 用户侧排行榜（积分 / 经验 / 等级 / 签到），独立路由组便于挂载热点限流
	leaderboard := router.Group("/api/leaderboard")
	leaderboard.Use(middleware.Auth(authService))
	// 叠加排行榜专用限流：每账号每分钟 60 次，匿名回退到 IP
	leaderboard.Use(middleware.LeaderboardRateLimit(60))
	{
		leaderboard.GET("/summary", h.LeaderboardSummary)     // 综合概览（6 类榜单 Top N 并发聚合）
		leaderboard.GET("/me", h.LeaderboardMe)               // 我在所有榜单的排名
		leaderboard.GET("/points/:type", h.LeaderboardPoints) // type: integral / experience / level
		leaderboard.GET("/signin/:type", h.LeaderboardSignIn) // type: today / consecutive / monthly
	}

	notifications := router.Group("/api/notifications")
	notifications.Use(middleware.Auth(authService))
	{
		notifications.GET("", h.Notifications)
		notifications.GET("/unread-count", h.NotificationUnreadCount)
		notifications.POST("/read", h.ReadNotification)
		notifications.POST("/read-batch", h.ReadNotificationsBatch)
		notifications.POST("/read-all", h.ReadAllNotifications)
		notifications.DELETE("/:notificationId", h.DeleteNotification)
		notifications.POST("/clear", h.ClearNotifications)
	}

	emailPublic := router.Group("/api/email")
	{
		emailPublic.POST("/send-code", h.SendEmailCode)
		emailPublic.POST("/verify-code", h.VerifyEmailCode)
		emailPublic.POST("/send-password-reset", h.SendPasswordResetEmail)
		emailPublic.POST("/verify-reset-token", h.VerifyResetToken)
		// Zeabur Email 投递回执。公开路由，准入靠 HMAC 签名而非管理员令牌；
		// 不带 :config 时落到该应用的默认邮件配置。
		emailPublic.POST("/webhook/zeabur/:appid", h.ZeaburEmailWebhook)
		emailPublic.POST("/webhook/zeabur/:appid/:config", h.ZeaburEmailWebhook)
	}
}

// registerCommerceRoutes 注册支付、钱包、会员、存储与渠道回调。
func registerCommerceRoutes(router *gin.Engine, h *Handler, deps RouterDeps) {
	var (
		authService = deps.Auth
	)
	// 邮件里的凭证下载链接：签名授权、无需会话。
	// 必须挂在鉴权组之外 —— 邮件客户端里没有登录态，需要登录的链接等于打不开。
	router.GET("/api/pay/receipts/:appid/:billId/download", h.DownloadSignedPaymentReceipt)

	pay := router.Group("/api/pay")
	pay.Use(middleware.Auth(authService))
	{
		pay.GET("/orders", h.PaymentOrders)
		pay.POST("/orders/create", h.CreatePaymentOrder)
		pay.GET("/orders/:orderNo", h.PaymentOrderDetail)
		pay.GET("/orders/:orderNo/bill", h.ExportPaymentBill)
		pay.POST("/orders/:orderNo/bill", h.ExportPaymentBill)
		// 直接取 PDF：省掉「先创建再下载」两步，适合只想存一份文件的客户端
		pay.GET("/orders/:orderNo/receipt", h.DownloadPaymentReceipt)
		pay.GET("/bills/:billId/download", h.DownloadPaymentBill)
		// 寄到账号绑定的邮箱（收件地址不接受请求指定，见 handler 注释）
		pay.POST("/orders/:orderNo/receipt/email", h.EmailPaymentReceipt)
		// 可选语言与当前环境的字体能力（缺中日韩字体时客户端可提前提示）
		pay.GET("/receipt/options", h.PaymentReceiptOptions)
		pay.GET("/epay/query/:orderNo", h.QueryEpayOrder)
	}

	// 余额系统（用户端）
	wallet := router.Group("/api/wallet")
	wallet.Use(middleware.Auth(authService))
	{
		wallet.GET("", h.MyWallet)
		wallet.GET("/transactions", h.MyWalletTransactions)
		wallet.POST("/consume", h.WalletConsume)
		// 流水凭证：与订单凭证同构的三条入口（直接取 PDF / 导出可分享凭据 / 寄邮箱）。
		// 余额直购会员、业务消费、管理员调账都不产生支付订单，
		// 没有这几条，用「钱包付的钱」就永远拿不到凭证。
		wallet.GET("/transactions/:transactionNo", h.MyWalletTransactionDetail)
		wallet.GET("/transactions/:transactionNo/receipt", h.DownloadWalletReceipt)
		wallet.GET("/transactions/:transactionNo/bill", h.ExportWalletBill)
		wallet.POST("/transactions/:transactionNo/bill", h.ExportWalletBill)
		wallet.POST("/transactions/:transactionNo/receipt/email", h.EmailWalletReceipt)
	}

	// 会员系统（用户端）
	vip := router.Group("/api/vip")
	vip.Use(middleware.Auth(authService))
	{
		vip.GET("/plans", h.VipPlans)
		vip.GET("/status", h.MyVipStatus)
		vip.GET("/transactions", h.MyVipTransactions)
		vip.POST("/purchase", h.PurchaseVip)
	}

	storage := router.Group("/api/storage")
	storage.Use(middleware.Auth(authService))
	{
		storage.POST("/upload", h.StorageUpload)
		storage.POST("/object-link", h.StorageObjectLink)
	}

	publicPay := router.Group("/api/public/pay")
	{
		publicPay.POST("/epay", h.EpayCallback)
		publicPay.GET("/epay", h.EpayCallback)
		publicPay.POST("/callback/:method", h.PaymentCallback)
		publicPay.GET("/callback/:method", h.PaymentCallback)
		// 路径段携带应用标识（微信支付 v3 通知地址禁止查询参数）
		publicPay.POST("/callback/:method/:appid", h.PaymentCallback)
		publicPay.GET("/callback/:method/:appid", h.PaymentCallback)
		// 同步跳转结果查询（只读，凭渠道的签名 query 换订单状态；渠道由订单推导）
		publicPay.GET("/return", h.PaymentReturn)
	}

	publicStorage := router.Group("/api/storage")
	{
		publicStorage.GET("/proxy/:ticket", h.StorageProxyDownload)
	}
}
