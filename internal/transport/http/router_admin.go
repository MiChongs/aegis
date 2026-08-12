package httptransport

import (
	"aegis/internal/middleware"

	"github.com/gin-gonic/gin"
)

// 管理端：认证、应用管理、工单与通知。
//
// 本文件只做路由注册，由 NewRouter 按原顺序调用；分组规则见 route_groups.go。

// registerAdminAuthRoutes 注册管理端认证与管理员自身资料。
func registerAdminAuthRoutes(router *gin.Engine, h *Handler, deps RouterDeps) {
	var (
		adminService = deps.Admin
		auditService = deps.Audit
	)
	adminAuth := router.Group("/api/admin/auth")
	// 认证路由组挂 AuditMiddleware：
	//   正常路径下 handler 通过 recordAuditAuth() + MarkAuditRecorded() 精确记录；
	//   handler 未到达（bind / captcha 早失败）时由中间件的 recordAnonymousAuthFallback 兜底，
	//   保证"普通管理员登录 / 注册 / OIDC / SAML / LDAP / MFA"全部产出审计痕迹。
	adminAuth.Use(middleware.AuditMiddleware(auditService))
	{
		adminAuth.POST("/login", h.AdminLogin)
		// 自助注册。handler 与审计早就写好了（上面那段注释里就列着「注册」），
		// 只是这一行一直没写，于是控制台的注册页每次提交都打到 NoRoute 上
		// 拿回一个 40400「请求的页面不存在」—— 前端看起来像是接口地址写错了。
		//
		// 建出来的账号 `isSuperAdmin=false` 且不带任何 assignment：能登录、
		// 但在被授权之前什么都看不到（见 AdminService.RegisterAdmin）。
		// 是否要对外开放这个入口是部署方的选择 —— 要关就摘掉这一行，
		// 控制台的 /register 会随之只剩一个必然失败的表单，记得一并处理。
		adminAuth.POST("/register", h.AdminRegister)
		// 注册入口开不开是**配置**（platform_settings 的 admin.self_service），
		// 不再是"路由挂不挂"。登录页据此决定注册链接显不显示，
		// 关掉之后提交会拿到 40317 而不是一个看起来像地址写错的 40400。
		adminAuth.GET("/self-service", h.AdminSelfServiceConfig)
		adminAuth.POST("/verify-mfa", h.AdminVerifyMFA)
		adminAuth.GET("/ldap/config", h.AdminLDAPPublicConfig)
		adminAuth.GET("/oidc/config", h.AdminOIDCPublicConfig)
		adminAuth.GET("/oidc/authorize", h.AdminOIDCAuthorize)
		adminAuth.GET("/oidc/callback", h.AdminOIDCCallback)
		adminAuth.POST("/oidc/exchange", h.AdminOIDCExchange)
		adminAuth.GET("/saml/config", h.AdminSAMLPublicConfig)
		adminAuth.GET("/saml/authorize", h.AdminSAMLAuthorize)
		adminAuth.GET("/saml/metadata", h.AdminSAMLMetadata)
		adminAuth.GET("/saml/callback", h.AdminSAMLCallback)
		adminAuth.POST("/saml/callback", h.AdminSAMLCallback)
		adminAuth.POST("/saml/exchange", h.AdminSAMLExchange)
		adminAuth.GET("/me", middleware.AdminAuth(adminService), h.AdminMe)
		// /logout 保留 AdminAuth（需要登录态）；AuditMiddleware 由上面的 adminAuth.Use 统一提供
		adminAuth.POST("/logout", middleware.AdminAuth(adminService), h.AdminLogout)
	}

	adminProfile := router.Group("/api/admin/profile")
	adminProfile.Use(middleware.AdminAuth(adminService))
	adminProfile.Use(middleware.AuditMiddleware(auditService))
	{
		adminProfile.GET("", h.AdminProfile)
		adminProfile.PUT("", h.UpdateAdminProfile)
		adminProfile.POST("/avatar", h.UploadAdminAvatar)
		adminProfile.POST("/upload-avatar", h.UploadAdminAvatar)
		adminProfile.GET("/security", h.AdminSecurity)
		adminProfile.POST("/two-factor/enroll", h.BeginAdminTOTPEnrollment)
		adminProfile.POST("/two-factor/enable", h.EnableAdminTOTP)
		adminProfile.POST("/two-factor/disable", h.DisableAdminTOTP)
		adminProfile.GET("/two-factor/recovery-codes", h.ListAdminRecoveryCodes)
		adminProfile.POST("/two-factor/recovery-codes", h.GenerateAdminRecoveryCodes)
		adminProfile.POST("/two-factor/recovery-codes/regenerate", h.RegenerateAdminRecoveryCodes)
		adminProfile.GET("/passkey", h.ListAdminPasskeys)
		adminProfile.POST("/passkey/register/options", h.BeginAdminPasskeyRegistration)
		adminProfile.POST("/passkey/register", h.FinishAdminPasskeyRegistration)
		adminProfile.DELETE("/passkey/:credentialId", h.DeleteAdminPasskey)
		adminProfile.GET("/roles", h.AdminRoleCatalog)
		adminProfile.GET("/roles/permissions", h.AdminRolePermissionTree)
	}

	adminAvatarCompat := router.Group("/api/admin/avatar")
	adminAvatarCompat.Use(middleware.AdminAuth(adminService))
	adminAvatarCompat.Use(middleware.AuditMiddleware(auditService))
	{
		adminAvatarCompat.POST("", h.UploadAdminAvatar)
		adminAvatarCompat.POST("/upload", h.UploadAdminAvatar)
	}
}

// registerAdminAppRoutes 注册应用管理 —— 管理端最大的一组，作用域是**单个应用**，
// 授权按管理员绑定的应用判定。
func registerAdminAppRoutes(router *gin.Engine, h *Handler, deps RouterDeps) {
	var (
		adminService      = deps.Admin
		appService        = deps.App
		auditService      = deps.Audit
		governanceService = deps.Governance
	)
	// 应用列表和创建：仅需登录，不检查权限（注册用户可创建应用）
	adminAppEntry := router.Group("/api/admin")
	adminAppEntry.Use(middleware.AdminAuth(adminService))
	adminAppEntry.Use(middleware.AuditMiddleware(auditService))
	{
		adminAppEntry.GET("/apps", h.AdminApps)
	}

	admin := router.Group("/api/admin")
	admin.Use(middleware.AdminAccess(adminService, appService, governanceService))
	admin.Use(middleware.AuditMiddleware(auditService))
	admin.GET("/dashboard", h.AdminDashboard)
	{
		admin.POST("/apps", h.CreateAdminApp)
		admin.GET("/apps/:appkey", h.AdminApp)
		// 应用自己的治理视图：看自己被平台怎么了 + 申诉。
		// 改动治理结论要走 /api/admin/platform/*（全局作用域），这里只读 + 申诉。
		admin.GET("/apps/:appkey/governance", h.AdminAppGovernance)
		admin.GET("/apps/:appkey/governance/history", h.AdminAppGovernanceHistory)
		admin.POST("/apps/:appkey/governance/appeals", h.AdminAppSubmitGovernanceAppeal)
		admin.POST("/apps/:appkey/governance/appeals/:appealId/withdraw", h.AdminAppWithdrawGovernanceAppeal)
		admin.GET("/apps/:appkey/policy", h.AdminAppPolicy)
		admin.PUT("/apps/:appkey/policy", h.UpdateAdminAppPolicy)
		admin.GET("/apps/:appkey/password-policy", h.AdminAppPasswordPolicy)
		admin.PUT("/apps/:appkey/password-policy", h.UpdateAdminAppPasswordPolicy)
		admin.POST("/apps/:appkey/password-policy/test", h.TestAdminAppPasswordPolicy)
		admin.POST("/apps/:appkey/password-policy/reset", h.ResetAdminAppPasswordPolicy)
		admin.GET("/apps/password-policy/templates", h.PasswordPolicyTemplates)
		// 登录绑定基线：应用开启设备/IP/属地强绑定后的查看与解绑出口
		admin.GET("/apps/:appkey/login-baseline/:userid", h.AdminAppLoginBaseline)
		admin.DELETE("/apps/:appkey/login-baseline/:userid", h.ResetAdminAppLoginBaseline)
		admin.GET("/apps/:appkey/commerce", h.AdminAppCommerceSettings)
		admin.PUT("/apps/:appkey/commerce", h.UpdateAdminAppCommerceSettings)
		admin.GET("/apps/:appkey/signin/stats", h.AdminAppSignInStats)
		admin.GET("/apps/:appkey/signin/records", h.AdminAppSignInRecords)
		admin.GET("/apps/:appkey/signin-reward", h.AdminAppSignInReward)
		admin.PUT("/apps/:appkey/signin-reward", h.UpdateAdminAppSignInReward)
		admin.POST("/apps/:appkey/signin-reward/test", h.TestAdminAppSignInReward)
		admin.POST("/apps/:appkey/signin-reward/reset", h.ResetAdminAppSignInReward)
		// 会员系统（管理端）
		admin.GET("/apps/:appkey/vip/plans", h.AdminAppVipPlans)
		admin.POST("/apps/:appkey/vip/plans", h.AdminSaveAppVipPlan)
		admin.DELETE("/apps/:appkey/vip/plans/:planId", h.AdminDeleteAppVipPlan)
		admin.POST("/apps/:appkey/vip/grant", h.AdminGrantAppUserVip)
		admin.GET("/apps/:appkey/vip/transactions", h.AdminAppVipTransactions)
		// 余额系统（管理端）
		admin.GET("/apps/:appkey/users/:userId/wallet", h.AdminAppUserWallet)
		admin.GET("/apps/:appkey/users/:userId/wallet/transactions", h.AdminAppUserWalletTransactions)
		admin.POST("/apps/:appkey/wallet/adjust", h.AdminAdjustAppUserWallet)
		// 全应用资金视图：按用户一个个点过去无法对账
		admin.GET("/apps/:appkey/wallet/transactions", h.AdminAppWalletTransactions)
		admin.GET("/apps/:appkey/wallet/stats", h.AdminAppWalletStats)
		admin.POST("/apps/:appkey/wallet/receipt", h.AdminAppWalletReceipt)
		admin.POST("/apps/:appkey/wallet/receipt/email", h.AdminAppWalletReceiptEmail)
		// 交易概览：订单 + 钱包 + 凭证能力一次取齐（分开拉会出现时间窗对不上的画面）
		admin.GET("/apps/:appkey/commerce/overview", h.AdminAppCommerceOverview)
		admin.GET("/apps/:appkey/functions", h.AdminListAppFunctions)
		admin.POST("/apps/:appkey/functions", h.AdminCreateAppFunction)
		admin.GET("/apps/:appkey/function-keys", h.AdminListAppFunctionKeys)
		admin.POST("/apps/:appkey/function-keys", h.AdminCreateAppFunctionKey)
		admin.DELETE("/apps/:appkey/function-keys/:keyId", h.AdminRevokeAppFunctionKey)
		admin.GET("/apps/:appkey/functions/:functionName", h.AdminGetAppFunction)
		admin.PUT("/apps/:appkey/functions/:functionName", h.AdminUpdateAppFunction)
		admin.DELETE("/apps/:appkey/functions/:functionName", h.AdminDeleteAppFunction)
		admin.GET("/apps/:appkey/functions/:functionName/versions", h.AdminListAppFunctionVersions)
		admin.POST("/apps/:appkey/functions/:functionName/versions", h.AdminCreateAppFunctionVersion)
		admin.POST("/apps/:appkey/functions/:functionName/versions/:version/activate", h.AdminActivateAppFunctionVersion)
		admin.POST("/apps/:appkey/functions/:functionName/invoke", h.AdminInvokeAppFunction)
		admin.GET("/apps/:appkey/functions/:functionName/invocations", h.AdminListAppFunctionInvocations)
		// 应用级第三方登录渠道（配置 + 自检 + 绑定治理）
		admin.GET("/oauth-providers/templates", h.AdminOAuthProviderTemplates)
		admin.GET("/apps/:appkey/oauth-providers", h.AdminListAppOAuthProviders)
		admin.POST("/apps/:appkey/oauth-providers", h.AdminCreateAppOAuthProvider)
		admin.POST("/apps/:appkey/oauth-providers/reorder", h.AdminReorderAppOAuthProviders)
		admin.GET("/apps/:appkey/oauth-providers/:provider", h.AdminGetAppOAuthProvider)
		admin.PUT("/apps/:appkey/oauth-providers/:provider", h.AdminUpdateAppOAuthProvider)
		admin.DELETE("/apps/:appkey/oauth-providers/:provider", h.AdminDeleteAppOAuthProvider)
		admin.PUT("/apps/:appkey/oauth-providers/:provider/enabled", h.AdminSetAppOAuthProviderEnabled)
		admin.POST("/apps/:appkey/oauth-providers/:provider/test", h.AdminTestAppOAuthProvider)
		admin.GET("/apps/:appkey/oauth-bindings", h.AdminListAppOAuthBindings)
		admin.DELETE("/apps/:appkey/oauth-bindings", h.AdminDeleteAppOAuthBinding)
		admin.GET("/apps/:appkey/auth-protocol", h.AdminGetAppAuthProtocol)
		admin.PUT("/apps/:appkey/auth-protocol", h.AdminUpdateAppAuthProtocol)
		admin.POST("/apps/:appkey/auth-protocol/secret/rotate", h.AdminRotateAppSigningSecret)
		admin.POST("/apps/:appkey/auth-protocol/selftest", h.AdminAppIntegrationSelfTest)
		admin.GET("/apps/:appkey/auth-protocol/transport/keys", h.AdminListAppTransportKeys)
		admin.POST("/apps/:appkey/auth-protocol/transport/rotate", h.AdminRotateAppTransportKey)
		admin.DELETE("/apps/:appkey/auth-protocol/transport/keys/:keyId", h.AdminRevokeAppTransportKey)
		admin.GET("/apps/signin-reward/templates", h.SignInRewardTemplates)
		admin.GET("/apps/:appkey/stats", h.AdminAppStats)
		admin.GET("/apps/:appkey/stats/user-trend", h.AdminAppUserTrend)
		admin.GET("/apps/:appkey/stats/regions", h.AdminAppRegionStats)
		admin.GET("/apps/:appkey/stats/auth-sources", h.AdminAppAuthSources)
		admin.GET("/apps/:appkey/audits/login", h.AdminAppLoginAudits)
		admin.GET("/apps/:appkey/audits/login/export", h.ExportAdminAppLoginAudits)
		admin.GET("/apps/:appkey/audits/sessions", h.AdminAppSessionAudits)
		admin.GET("/apps/:appkey/audits/sessions/export", h.ExportAdminAppSessionAudits)
		admin.GET("/apps/:appkey/notifications", h.AdminAppNotifications)
		admin.GET("/apps/:appkey/notifications/export", h.ExportAdminAppNotifications)
		admin.DELETE("/apps/:appkey/notifications", h.DeleteAdminAppNotifications)
		admin.POST("/apps/:appkey/notifications/delete-by-filter", h.DeleteAdminAppNotificationsByFilter)
		admin.POST("/apps/:appkey/notifications/bulk", h.AdminBulkNotifyUsers)
		admin.GET("/apps/:appkey/users", h.AdminAppUsers)
		admin.GET("/apps/:appkey/users/export", h.ExportAdminAppUsers)
		admin.POST("/apps/:appkey/users/bans/batch", h.BatchCreateAdminAppUserBan)
		admin.PUT("/apps/:appkey/users/status/batch", h.BatchUpdateAdminAppUserStatus)
		admin.GET("/apps/:appkey/users/:userId", h.AdminAppUser)
		admin.GET("/apps/:appkey/users/:userId/bans", h.AdminAppUserBans)
		admin.GET("/apps/:appkey/users/:userId/bans/active", h.AdminAppUserActiveBan)
		admin.POST("/apps/:appkey/users/:userId/bans", h.CreateAdminAppUserBan)
		admin.POST("/apps/:appkey/users/:userId/bans/:banId/revoke", h.RevokeAdminAppUserBan)
		admin.PUT("/apps/:appkey/users/:userId/status", h.UpdateAdminAppUserStatus)
		admin.PUT("/apps/:appkey/users/:userId/profile", h.AdminUpdateUserProfile)
		admin.POST("/apps/:appkey/users/:userId/reset-password", h.AdminResetUserPassword)
		admin.GET("/apps/:appkey/users/:userId/audits/login", h.AdminAppUserLoginAudits)
		admin.GET("/apps/:appkey/users/:userId/audits/sessions", h.AdminAppUserSessionAudits)
		admin.GET("/apps/:appkey/users/:userId/sessions", h.AdminListUserSessions)
		admin.DELETE("/apps/:appkey/users/:userId/sessions/:tokenHash", h.AdminRevokeUserSession)
		admin.POST("/apps/:appkey/users/:userId/sessions/revoke-batch", h.AdminRevokeUserSessionsBatch)
		admin.POST("/apps/:appkey/users/:userId/revoke-sessions", h.AdminRevokeUserSessions)
		admin.DELETE("/apps/:appkey/users/:userId", h.AdminDeleteUser)
		admin.PUT("/apps/:appkey", h.UpdateAdminApp)
		admin.DELETE("/apps/:appkey", h.AdminDeleteApp)
		admin.GET("/apps/:appkey/captcha-config", h.AdminGetCaptchaConfig)
		admin.PUT("/apps/:appkey/captcha-config", h.AdminUpdateCaptchaConfig)
		admin.POST("/apps/:appkey/captcha-config/test-sms", h.AdminTestSMS)
		admin.GET("/apps/:appkey/encryption", h.AdminAppEncryption)
		admin.PUT("/apps/:appkey/encryption", h.UpdateAdminAppEncryption)
		admin.GET("/apps/:appkey/content/overview", h.AdminContentOverview)
		admin.GET("/apps/:appkey/banners", h.AdminBanners)
		admin.GET("/apps/:appkey/banners/export", h.ExportAdminBanners)
		admin.POST("/apps/:appkey/banners", h.CreateAdminBanner)
		admin.POST("/apps/:appkey/banners/image", h.UploadAdminBannerImage)
		admin.DELETE("/apps/:appkey/banners", h.DeleteAdminBanners)
		// order 在 :bannerId 之前注册：拖拽提交的是完整顺序，不是某一条的新位置
		admin.PUT("/apps/:appkey/banners/order", h.ReorderAdminBanners)
		admin.PUT("/apps/:appkey/banners/:bannerId", h.UpdateAdminBanner)
		admin.DELETE("/apps/:appkey/banners/:bannerId", h.DeleteAdminBanner)
		admin.GET("/apps/:appkey/notices", h.AdminNotices)
		admin.GET("/apps/:appkey/notices/export", h.ExportAdminNotices)
		admin.POST("/apps/:appkey/notices", h.CreateAdminNotice)
		admin.DELETE("/apps/:appkey/notices", h.DeleteAdminNotices)
		admin.PUT("/apps/:appkey/notices/:noticeId", h.UpdateAdminNotice)
		admin.DELETE("/apps/:appkey/notices/:noticeId", h.DeleteAdminNotice)
		admin.GET("/user-settings/stats", h.AdminUserSettingsStats)
		admin.GET("/user-settings/user", h.AdminUserSettings)
		admin.POST("/user-settings/batch-initialize", h.AdminBatchInitializeUserSettings)
		admin.POST("/user-settings/initialize-user", h.AdminInitializeUserSettings)
		admin.GET("/user-settings/check-integrity", h.AdminCheckUserSettingsIntegrity)
		admin.DELETE("/user-settings/cleanup", h.AdminCleanupUserSettings)

		// 抽奖系统管理路由
		admin.GET("/apps/:appkey/lottery/activities", h.AdminListLotteryActivities)
		admin.POST("/apps/:appkey/lottery/activities", h.AdminCreateLotteryActivity)
		admin.GET("/apps/:appkey/lottery/activities/:id", h.AdminGetLotteryActivity)
		admin.PUT("/apps/:appkey/lottery/activities/:id", h.AdminUpdateLotteryActivity)
		admin.DELETE("/apps/:appkey/lottery/activities/:id", h.AdminDeleteLotteryActivity)
		admin.GET("/apps/:appkey/lottery/activities/:id/prizes", h.AdminListLotteryPrizes)
		admin.POST("/apps/:appkey/lottery/activities/:id/prizes", h.AdminCreateLotteryPrize)
		admin.PUT("/apps/:appkey/lottery/prizes/:id", h.AdminUpdateLotteryPrize)
		admin.DELETE("/apps/:appkey/lottery/prizes/:id", h.AdminDeleteLotteryPrize)
		admin.GET("/apps/:appkey/lottery/activities/:id/stats", h.AdminLotteryActivityStats)
		admin.POST("/apps/:appkey/lottery/activities/:id/seed/commit", h.AdminLotteryCommitSeed)
		admin.POST("/apps/:appkey/lottery/activities/:id/seed/reveal", h.AdminLotteryRevealSeed)
		admin.GET("/apps/:appkey/lottery/draws", h.AdminListLotteryDraws)

		// 版本发布管理 RESTful 路由
		admin.GET("/apps/:appkey/versions", h.AdminListVersions)
		admin.POST("/apps/:appkey/versions", h.AdminCreateVersion)
		admin.GET("/apps/:appkey/versions/stats", h.AdminVersionStats)
		admin.GET("/apps/:appkey/versions/:vid", h.AdminGetVersion)
		admin.PUT("/apps/:appkey/versions/:vid", h.AdminUpdateVersion)
		admin.DELETE("/apps/:appkey/versions/:vid", h.AdminDeleteVersion)
		admin.POST("/apps/:appkey/versions/:vid/publish", h.AdminPublishVersion)
		admin.POST("/apps/:appkey/versions/:vid/revoke", h.AdminRevokeVersion)
		admin.GET("/apps/:appkey/channels", h.AdminListVersionChannels)
		admin.POST("/apps/:appkey/channels", h.AdminCreateVersionChannel)
		admin.GET("/apps/:appkey/channels/:cid", h.AdminGetVersionChannel)
		admin.PUT("/apps/:appkey/channels/:cid", h.AdminUpdateVersionChannel)
		admin.DELETE("/apps/:appkey/channels/:cid", h.AdminDeleteVersionChannel)
		admin.GET("/apps/:appkey/channels/:cid/users", h.AdminListVersionChannelUsers)
		admin.POST("/apps/:appkey/channels/:cid/users", h.AdminAddVersionChannelUsers)
		admin.DELETE("/apps/:appkey/channels/:cid/users", h.AdminRemoveVersionChannelUsers)

		// 报表分析中心
		admin.GET("/apps/:appkey/reports/registration", h.ReportRegistration)
		admin.GET("/apps/:appkey/reports/login", h.ReportLogin)
		admin.GET("/apps/:appkey/reports/retention", h.ReportRetention)
		admin.GET("/apps/:appkey/reports/active", h.ReportActive)
		admin.GET("/apps/:appkey/reports/device", h.ReportDevice)
		admin.GET("/apps/:appkey/reports/region", h.ReportRegion)
		admin.GET("/apps/:appkey/reports/channel", h.ReportChannel)
		admin.GET("/apps/:appkey/reports/payment", h.ReportPayment)
		admin.GET("/apps/:appkey/reports/notification", h.ReportNotification)
		admin.GET("/apps/:appkey/reports/risk", h.ReportRisk)
		admin.GET("/apps/:appkey/reports/activity", h.ReportActivity)
		admin.GET("/apps/:appkey/reports/funnel", h.ReportFunnel)
		admin.GET("/apps/:appkey/reports/export", h.ReportExport)
	}
}

// registerAdminModuleRoutes 注册管理端的工单、收件箱与统一通知出口。
func registerAdminModuleRoutes(router *gin.Engine, h *Handler, deps RouterDeps) {
	var (
		adminService      = deps.Admin
		appService        = deps.App
		auditService      = deps.Audit
		governanceService = deps.Governance
	)
	// ─── 工单系统（管理端）─────────────────────────────────
	// 走 AdminAccess：中间件解析出 ticket:* 权限点做粗粒度闸门，
	// 细粒度的"能不能看/改这一条"由 TicketService 的 Scope + ActionSet 判定，
	// 因此仅仅是处理组成员的管理员也能进来处理自己名下的工单。
	adminTickets := router.Group("/api/admin/tickets")
	adminTickets.Use(middleware.AdminAccess(adminService, appService, governanceService))
	adminTickets.Use(middleware.AuditMiddleware(auditService))
	{
		// 元数据与配置（放在 :ticketId 之前，避免被通配路由吃掉）
		adminTickets.GET("/metadata", h.AdminTicketMetadata)
		adminTickets.GET("/stats", h.AdminTicketStats)
		adminTickets.GET("/trend", h.AdminTicketTrend)
		adminTickets.GET("/agents", h.AdminTicketAgentStats)
		adminTickets.GET("/workbench", h.AdminTicketWorkbench)
		adminTickets.GET("/export", h.ExportAdminTickets)

		adminTickets.GET("/categories", h.AdminListTicketCategories)
		adminTickets.POST("/categories", h.AdminSaveTicketCategory)
		adminTickets.PUT("/categories/:id", h.AdminSaveTicketCategory)
		adminTickets.DELETE("/categories/:id", h.AdminDeleteTicketCategory)

		adminTickets.GET("/groups", h.AdminListTicketGroups)
		adminTickets.POST("/groups", h.AdminSaveTicketGroup)
		adminTickets.PUT("/groups/:id", h.AdminSaveTicketGroup)
		adminTickets.DELETE("/groups/:id", h.AdminDeleteTicketGroup)
		adminTickets.PUT("/groups/:id/members", h.AdminSetTicketGroupMembers)

		adminTickets.GET("/sla-policies", h.AdminListTicketSLAPolicies)
		adminTickets.POST("/sla-policies", h.AdminSaveTicketSLAPolicy)
		adminTickets.PUT("/sla-policies/:id", h.AdminSaveTicketSLAPolicy)
		adminTickets.DELETE("/sla-policies/:id", h.AdminDeleteTicketSLAPolicy)

		adminTickets.GET("/quick-replies", h.AdminListTicketQuickReplies)
		adminTickets.POST("/quick-replies", h.AdminSaveTicketQuickReply)
		adminTickets.PUT("/quick-replies/:id", h.AdminSaveTicketQuickReply)
		adminTickets.DELETE("/quick-replies/:id", h.AdminDeleteTicketQuickReply)

		adminTickets.POST("/attachments", h.AdminUploadTicketAttachment)
		adminTickets.POST("/bulk", h.AdminBulkTickets)

		// 工单本体
		adminTickets.GET("", h.AdminListTickets)
		adminTickets.POST("", h.AdminCreateTicket)
		adminTickets.GET("/:ticketId", h.AdminGetTicket)
		adminTickets.PATCH("/:ticketId", h.AdminUpdateTicket)
		adminTickets.DELETE("/:ticketId", h.AdminDeleteTicket)
		adminTickets.POST("/:ticketId/replies", h.AdminReplyTicket)
		adminTickets.POST("/:ticketId/assign", h.AdminAssignTicket)
		adminTickets.POST("/:ticketId/status", h.AdminChangeTicketStatus)
		adminTickets.POST("/:ticketId/watch", h.AdminWatchTicket)
		adminTickets.PUT("/:ticketId/watchers", h.AdminSetTicketWatchers)
	}

	// ─── 管理员收件箱 ─────────────────────────────────────
	// 私有资源：一律以会话 adminID 为准，不做权限点校验（每个管理员都有自己的收件箱）。
	adminInboxGroup := router.Group("/api/admin/notifications")
	adminInboxGroup.Use(middleware.AdminAuth(adminService))
	{
		adminInboxGroup.GET("", h.ListAdminInbox)
		adminInboxGroup.GET("/unread-count", h.AdminInboxUnreadCount)
		adminInboxGroup.POST("/read", h.MarkAdminInboxRead)
		adminInboxGroup.POST("/delete", h.DeleteAdminInbox)
	}

	// ─── 统一通知出口（管理端）──────────────────────────────
	// 渠道里存着飞书/钉钉等 IM 的凭据，属平台级敏感资源：读要 notify:channel:read，
	// 写与测试发送一律要求超级管理员，防止授权表漂移导致凭据被越权改写。
	adminNotify := router.Group("/api/admin/notify")
	adminNotify.Use(middleware.AdminAccess(adminService, appService, governanceService))
	adminNotify.Use(middleware.AuditMiddleware(auditService))
	{
		adminNotify.GET("/catalog", h.AdminNotifyCatalog)
		adminNotify.GET("/channels", h.AdminListNotifyChannels)
		adminNotify.GET("/channels/:id", h.AdminGetNotifyChannel)
		adminNotify.GET("/subscriptions", h.AdminListNotifySubscriptions)
		adminNotify.GET("/templates", h.AdminListNotifyTemplates)
		adminNotify.GET("/deliveries", h.AdminListNotifyDeliveries)
		adminNotify.GET("/deliveries/stats", h.AdminNotifyDeliveryStats)

		notifyWrite := adminNotify.Group("", middleware.RequireSuperAdmin())
		{
			notifyWrite.POST("/channels", h.AdminSaveNotifyChannel)
			notifyWrite.PUT("/channels/:id", h.AdminSaveNotifyChannel)
			notifyWrite.DELETE("/channels/:id", h.AdminDeleteNotifyChannel)
			notifyWrite.POST("/channels/:id/test", h.AdminTestNotifyChannel)
			notifyWrite.POST("/subscriptions", h.AdminSaveNotifySubscription)
			notifyWrite.PUT("/subscriptions/:id", h.AdminSaveNotifySubscription)
			notifyWrite.DELETE("/subscriptions/:id", h.AdminDeleteNotifySubscription)
			notifyWrite.POST("/templates", h.AdminSaveNotifyTemplate)
			notifyWrite.PUT("/templates/:id", h.AdminSaveNotifyTemplate)
			notifyWrite.DELETE("/templates/:id", h.AdminDeleteNotifyTemplate)
			notifyWrite.POST("/templates/preview", h.AdminPreviewNotifyTemplate)
			notifyWrite.DELETE("/deliveries", h.AdminPurgeNotifyDeliveries)
		}
	}
}
