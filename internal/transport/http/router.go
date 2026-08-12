package httptransport

import (
	"aegis/internal/config"
	admindomain "aegis/internal/domain/admin"
	appdomain "aegis/internal/domain/app"
	authdomain "aegis/internal/domain/auth"
	captchadomain "aegis/internal/domain/captcha"
	notificationdomain "aegis/internal/domain/notification"
	pointdomain "aegis/internal/domain/points"
	userdomain "aegis/internal/domain/user"
	"aegis/internal/middleware"
	redisrepo "aegis/internal/repository/redis"
	"aegis/internal/service"
	"aegis/pkg/crashlog"
	"aegis/pkg/response"
	"aegis/pkg/tracing"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type Handler struct {
	auth            *service.AuthService
	admin           *service.AdminService
	user            *service.UserService
	signin          *service.SignInService
	points          *service.PointsService
	notifications   *service.NotificationService
	app             *service.AppService
	site            *service.SiteService
	version         *service.VersionService
	roleApp         *service.RoleApplicationService
	email           *service.EmailService
	payment         *service.PaymentService
	wallet          *service.WalletService
	vip             *service.VipService
	workflow        *service.WorkflowService
	storage         *service.StorageService
	avatar          *service.AvatarService
	monitor         *service.MonitorService
	realtime        *service.RealtimeService
	system          *service.PlatformSettingsService
	security        *service.SecurityService
	captcha         *service.CaptchaService
	firewallLog     *service.FirewallLogService
	ipBan           *service.IPBanService
	geoBan          *service.GeoBanService
	geoFence        *service.GeoFenceService
	geoAnalytics    *service.GeoAnalyticsService
	location        *service.LocationService
	lottery         *service.LotteryService
	announcement    *service.AnnouncementService
	ldapSvc         *service.LDAPService
	oidcSvc         *service.OIDCService
	samlSvc         *service.SAMLService
	sessions        *redisrepo.SessionRepository
	org             *service.OrganizationService
	tmpl            *service.TemplateService
	audit           *service.AuditService
	plugin          *service.PluginService
	appFunction     *service.AppFunctionService
	authProtocol    *service.AuthProtocolService
	dashboard       *service.DashboardService
	orgApproval     *service.OrgApprovalService
	sessionMgmt     *service.SessionMgmtService
	storageResource *service.StorageResourceService
	userMaster      *service.UserMasterService
	report          *service.ReportService
	risk            *service.RiskService
	deviceMarketing *service.DeviceMarketingService
	platformBanner  *service.PlatformBannerService
	// ticket 工单系统（管理端 + 用户端），权限判定在 service 层收敛
	ticket *service.TicketService
	// notify 统一通知出口（飞书/钉钉/企微/Slack/Webhook/邮件/站内信/实时）
	notify *service.NotifyHub
	// adminInbox 管理员站内收件箱（与用户站内信分表，主键空间不同）
	adminInbox *service.AdminInboxService
	// appOAuth 应用级第三方登录渠道配置中心（管理端 CRUD + 运行期解析）
	appOAuth *service.AppOAuthService
	// authProviderHealth 统一探测 LDAP/OIDC/SAML 可用性（单例 + 30s TTL 缓存）
	// 仅在管理员列表等需要标注"账号可能无法登录"的超管场景被调用
	authProviderHealth *service.AuthProviderHealthService
	// egress 出海代理网关管理面（端点 / 规则 / 自测 / 路由解释）
	egress *service.EgressService
	// governance 平台治理：全站应用的冻结 / 限制 / 封禁 / 归档与申诉
	governance *service.PlatformGovernanceService
}

func NewRouter(authService *service.AuthService, adminService *service.AdminService, userService *service.UserService, signInService *service.SignInService, pointsService *service.PointsService, notificationService *service.NotificationService, appService *service.AppService, siteService *service.SiteService, versionService *service.VersionService, roleApplicationService *service.RoleApplicationService, emailService *service.EmailService, paymentService *service.PaymentService, walletService *service.WalletService, vipService *service.VipService, workflowService *service.WorkflowService, storageService *service.StorageService, avatarService *service.AvatarService, monitorService *service.MonitorService, firewall *middleware.Firewall, replayGuard *middleware.ReplayGuard, locationService *service.LocationService, realtimeService *service.RealtimeService, systemService *service.PlatformSettingsService, securityService *service.SecurityService, captchaService *service.CaptchaService, firewallLogService *service.FirewallLogService, ipBanService *service.IPBanService, geoBanService *service.GeoBanService, geoFenceService *service.GeoFenceService, geoAnalyticsService *service.GeoAnalyticsService, lotteryService *service.LotteryService, announcementService *service.AnnouncementService, ldapService *service.LDAPService, oidcService *service.OIDCService, samlService *service.SAMLService, sessionRepo *redisrepo.SessionRepository, orgService *service.OrganizationService, templateService *service.TemplateService, auditService *service.AuditService, pluginService *service.PluginService, appFunctionService *service.AppFunctionService, authProtocolService *service.AuthProtocolService, dashboardService *service.DashboardService, approvalService *service.OrgApprovalService, sessionMgmtService *service.SessionMgmtService, storageResourceService *service.StorageResourceService, userMasterService *service.UserMasterService, reportService *service.ReportService, riskService *service.RiskService, deviceMarketingService *service.DeviceMarketingService, platformBannerService *service.PlatformBannerService, ticketService *service.TicketService, notifyHub *service.NotifyHub, adminInboxService *service.AdminInboxService, appOAuthService *service.AppOAuthService, authProviderHealthService *service.AuthProviderHealthService, memoryManager *service.MemoryManager, databaseManager *service.DatabaseManager, egressService *service.EgressService, governanceService *service.PlatformGovernanceService, cl *crashlog.Logger, log *zap.Logger, corsConfig config.CORSConfig, trustedProxies []string, docsPortalURL string) (*gin.Engine, error) {
	router := gin.New()
	if err := router.SetTrustedProxies(trustedProxies); err != nil {
		return nil, fmt.Errorf("配置可信代理失败: %w", err)
	}
	router.HandleMethodNotAllowed = true
	// 显式声明 multipart 内存缓冲上限：32MB
	// Gin 默认 32MB，这里显式写出便于一眼读懂上传体量策略；
	// 超过该值的部分会 spill 到临时文件，不影响成功率，仅影响解析成本。
	router.MaxMultipartMemory = 32 << 20
	// 空 CORS 配置会让浏览器的每一个写请求都吃 403（同源 POST 也带 Origin），
	// 而那个 403 没有 body 也没有日志。在启动时说一次，比让人从空白页倒查回来强。
	if warning := middleware.CORSGuardWarning(corsConfig); warning != "" && log != nil {
		log.Warn(warning)
	}
	router.Use(
		middleware.RequestID(),
		middleware.RequestOrigin(),
		middleware.CrashRecovery(log, cl),
		tracing.GinMiddleware("aegis", "/healthz", "/readyz"),
		middleware.CORS(corsConfig),
		gin.Logger(),
		firewall.Handler(),
		replayGuard.Handler(),
		middleware.AppGateway(authProtocolService),
		middleware.AppEncryption(appService),
		middleware.Location(locationService),
	)
	router.NoRoute(func(c *gin.Context) {
		response.Error(c, http.StatusNotFound, 40400, "请求的页面不存在")
	})
	router.NoMethod(func(c *gin.Context) {
		response.Error(c, http.StatusNotImplemented, 50100, "服务能力暂未开放")
	})

	h := &Handler{auth: authService, admin: adminService, user: userService, signin: signInService, points: pointsService, notifications: notificationService, app: appService, site: siteService, version: versionService, roleApp: roleApplicationService, email: emailService, payment: paymentService, wallet: walletService, vip: vipService, workflow: workflowService, storage: storageService, avatar: avatarService, monitor: monitorService, realtime: realtimeService, system: systemService, security: securityService, captcha: captchaService, firewallLog: firewallLogService, ipBan: ipBanService, geoBan: geoBanService, geoFence: geoFenceService, geoAnalytics: geoAnalyticsService, location: locationService, lottery: lotteryService, announcement: announcementService, ldapSvc: ldapService, oidcSvc: oidcService, samlSvc: samlService, sessions: sessionRepo, org: orgService, tmpl: templateService, audit: auditService, plugin: pluginService, appFunction: appFunctionService, authProtocol: authProtocolService, dashboard: dashboardService, orgApproval: approvalService, sessionMgmt: sessionMgmtService, storageResource: storageResourceService, userMaster: userMasterService, report: reportService, risk: riskService, deviceMarketing: deviceMarketingService, platformBanner: platformBannerService, ticket: ticketService, notify: notifyHub, adminInbox: adminInboxService, appOAuth: appOAuthService, authProviderHealth: authProviderHealthService, egress: egressService, governance: governanceService}
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
	router.GET("/api/ws", h.WebSocket)

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

		// 当前用户：资料 / 设置 / 安全概览
		appGatewayAuthed.GET("/me", h.AppMe)
		appGatewayAuthed.GET("/me/profile", h.Profile)
		appGatewayAuthed.PUT("/me/profile", h.UpdateProfile)
		appGatewayAuthed.POST("/me/profile/changes/confirm", h.ConfirmProfileChange)
		appGatewayAuthed.POST("/me/avatar", h.UploadUserAvatar)
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

	adminAuth := router.Group("/api/admin/auth")
	// 认证路由组挂 AuditMiddleware：
	//   正常路径下 handler 通过 recordAuditAuth() + MarkAuditRecorded() 精确记录；
	//   handler 未到达（bind / captcha 早失败）时由中间件的 recordAnonymousAuthFallback 兜底，
	//   保证"普通管理员登录 / 注册 / OIDC / SAML / LDAP / MFA"全部产出审计痕迹。
	adminAuth.Use(middleware.AuditMiddleware(auditService))
	{
		adminAuth.POST("/login", h.AdminLogin)
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

	appCompat := router.Group("/api/app/password-policy")
	appCompat.Use(middleware.AdminAccess(adminService, appService, governanceService))
	appCompat.Use(middleware.AuditMiddleware(auditService))
	{
		appCompat.POST("/get", h.GetAppPasswordPolicy)
		appCompat.POST("/set", h.SetAppPasswordPolicy)
		appCompat.POST("/test", h.TestAppPasswordPolicy)
		appCompat.GET("/templates", h.PasswordPolicyTemplates)
		appCompat.POST("/reset", h.ResetAppPasswordPolicy)
	}

	appCompatPoints := router.Group("/api/app/points")
	appCompatPoints.Use(middleware.AdminAccess(adminService, appService, governanceService))
	appCompatPoints.Use(middleware.AuditMiddleware(auditService))
	{
		appCompatPoints.POST("/stats", h.AppPointsStats)
		appCompatPoints.POST("/adjust-integral", h.AppAdjustIntegral)
		appCompatPoints.POST("/adjust-experience", h.AppAdjustExperience)
		appCompatPoints.POST("/batch-adjust", h.AppBatchAdjustIntegral)
	}

	appCompatVersion := router.Group("/api/admin/app/version")
	appCompatVersion.Use(middleware.AdminAccess(adminService, appService, governanceService))
	appCompatVersion.Use(middleware.AuditMiddleware(auditService))
	{
		appCompatVersion.POST("/list", h.AdminVersionListCompat)
		appCompatVersion.POST("/detail", h.AdminVersionDetailCompat)
		appCompatVersion.POST("/create", h.AdminVersionCreateCompat)
		appCompatVersion.POST("/update", h.AdminVersionUpdateCompat)
		appCompatVersion.POST("/delete", h.AdminVersionDeleteCompat)
		appCompatVersion.POST("/channel/list", h.AdminVersionChannelListCompat)
		appCompatVersion.POST("/channel/detail", h.AdminVersionChannelDetailCompat)
		appCompatVersion.POST("/channel/create", h.AdminVersionChannelCreateCompat)
		appCompatVersion.POST("/channel/update", h.AdminVersionChannelUpdateCompat)
		appCompatVersion.POST("/channel/delete", h.AdminVersionChannelDeleteCompat)
		appCompatVersion.POST("/channel/users", h.AdminVersionChannelUsersCompat)
		appCompatVersion.POST("/channel/add-users", h.AdminVersionChannelAddUsersCompat)
		appCompatVersion.POST("/channel/remove-users", h.AdminVersionChannelRemoveUsersCompat)
		appCompatVersion.POST("/stats", h.AdminVersionStatsCompat)
		appCompatVersion.POST("/channel/preview-match", h.AdminVersionPreviewMatchCompat)
	}

	appCompatSite := router.Group("/api/admin/app/site")
	appCompatSite.Use(middleware.AdminAccess(adminService, appService, governanceService))
	appCompatSite.Use(middleware.AuditMiddleware(auditService))
	{
		appCompatSite.POST("/audit-list", h.AdminSiteAuditListCompat)
		appCompatSite.POST("/audit", h.AdminSiteAuditCompat)
		appCompatSite.POST("/batch-audit", h.AdminSiteBatchAuditCompat)
		appCompatSite.POST("/list", h.AdminSiteListCompat)
		appCompatSite.POST("/detail", h.AdminSiteDetailCompat)
		appCompatSite.POST("/update", h.AdminSiteUpdateCompat)
		appCompatSite.POST("/delete", h.AdminSiteDeleteCompat)
		appCompatSite.POST("/toggle-pin", h.AdminSiteTogglePinCompat)
		appCompatSite.POST("/user-sites", h.AdminSiteUserSitesCompat)
		appCompatSite.POST("/audit-stats", h.AdminSiteAuditStatsCompat)
	}

	appCompatRole := router.Group("/api/admin/app/role-application")
	appCompatRole.Use(middleware.AdminAccess(adminService, appService, governanceService))
	appCompatRole.Use(middleware.AuditMiddleware(auditService))
	{
		appCompatRole.POST("/list", h.AdminRoleApplicationsCompat)
		appCompatRole.POST("/detail", h.AdminRoleApplicationDetailCompat)
		appCompatRole.POST("/review", h.AdminRoleApplicationReviewCompat)
		appCompatRole.POST("/batch-review", h.AdminRoleApplicationBatchReviewCompat)
		appCompatRole.POST("/statistics", h.AdminRoleApplicationStatisticsCompat)
	}

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

	// ─── 平台治理（全站作用域）─────────────────────────────
	//
	// 与 /api/admin/apps/:appkey/* 的分工：那一组是**应用作用域**，
	// 授权按管理员绑定的应用判定；这一组是**全局作用域**，
	// resolveAdminPermission 对 /api/admin/platform/ 前缀恒返回 appScoped=false，
	// 所以应用级管理员即使被授了 platform:app:govern 也进不来 —— 不存在自己解自己封禁。
	adminPlatform := router.Group("/api/admin/platform")
	adminPlatform.Use(middleware.AdminAccess(adminService, appService, governanceService))
	adminPlatform.Use(middleware.AuditMiddleware(auditService))
	{
		adminPlatform.GET("/catalog", h.AdminPlatformGovernanceCatalog)
		adminPlatform.GET("/overview", h.AdminPlatformOverview)
		adminPlatform.GET("/apps", h.AdminPlatformOverview)
		adminPlatform.POST("/apps/batch-governance", h.AdminPlatformBatchGovernance)
		adminPlatform.GET("/apps/:appkey/governance", h.AdminPlatformAppGovernance)
		adminPlatform.POST("/apps/:appkey/governance", h.AdminPlatformApplyGovernance)
		adminPlatform.POST("/apps/:appkey/revoke-sessions", h.AdminPlatformRevokeAppSessions)
		adminPlatform.GET("/governance/actions", h.AdminPlatformGovernanceActions)
		adminPlatform.GET("/governance/appeals", h.AdminPlatformListAppeals)
		adminPlatform.POST("/governance/appeals/:appealId/review", h.AdminPlatformReviewAppeal)
	}

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

	emailAdmin := router.Group("/api/admin/app/email-config")
	emailAdmin.Use(middleware.AdminAccess(adminService, appService, governanceService))
	emailAdmin.Use(middleware.AuditMiddleware(auditService))
	{
		emailAdmin.POST("/list", h.AdminEmailConfigList)
		emailAdmin.POST("/detail", h.AdminEmailConfigDetail)
		emailAdmin.POST("/create", h.AdminEmailConfigCreate)
		emailAdmin.POST("/update", h.AdminEmailConfigUpdate)
		emailAdmin.POST("/delete", h.AdminEmailConfigDelete)
		emailAdmin.POST("/test", h.AdminEmailConfigTest)
		emailAdmin.POST("/deliveries", h.AdminEmailDeliveryList)
	}

	payCompat := router.Group("/api/admin/app/payment-config")
	payCompat.Use(middleware.AdminAccess(adminService, appService, governanceService))
	payCompat.Use(middleware.AuditMiddleware(auditService))
	{
		payCompat.POST("/list", h.AdminPaymentConfigList)
		payCompat.POST("/detail", h.AdminPaymentConfigDetail)
		payCompat.POST("/create", h.AdminPaymentConfigCreate)
		payCompat.POST("/update", h.AdminPaymentConfigUpdate)
		payCompat.POST("/delete", h.AdminPaymentConfigDelete)
		payCompat.POST("/test", h.AdminPaymentConfigTest)
		// 管理端订单查询
		payCompat.POST("/orders/list", h.AdminPaymentOrderList)
		payCompat.POST("/orders/detail", h.AdminPaymentOrderDetail)
		// 凭证：管理端代开（客服 / 对账留档）+ 语言与字体能力自述
		payCompat.POST("/orders/receipt", h.AdminPaymentOrderReceipt)
		payCompat.POST("/orders/receipt/email", h.AdminPaymentOrderReceiptEmail)
		payCompat.POST("/receipt/options", h.PaymentReceiptOptions)

		// 退款
		payCompat.POST("/refunds/refundable", h.AdminPaymentRefundable)
		payCompat.POST("/refunds/create", h.AdminPaymentRefundCreate)
		payCompat.POST("/refunds/list", h.AdminPaymentRefundList)
		payCompat.POST("/refunds/order", h.AdminPaymentRefundOrderList)
		payCompat.POST("/refunds/sync", h.AdminPaymentRefundSync)
		payCompat.POST("/epay/init", h.AdminPaymentEpayInit)
		payCompat.POST("/methods", h.PaymentMethods)
	}

	appStorageAdmin := router.Group("/api/admin/app/storage-config")
	appStorageAdmin.Use(middleware.AdminAccess(adminService, appService, governanceService))
	appStorageAdmin.Use(middleware.AuditMiddleware(auditService))
	{
		appStorageAdmin.POST("/list", h.AdminAppStorageConfigList)
		appStorageAdmin.POST("/detail", h.AdminAppStorageConfigDetail)
		appStorageAdmin.POST("/create", h.AdminAppStorageConfigCreate)
		appStorageAdmin.POST("/update", h.AdminAppStorageConfigUpdate)
		appStorageAdmin.POST("/delete", h.AdminAppStorageConfigDelete)
		appStorageAdmin.POST("/test", h.AdminAppStorageConfigTest)
	}

	globalStorageAdmin := router.Group("/api/admin/platform/storage-config")
	globalStorageAdmin.Use(middleware.AdminAccess(adminService, appService, governanceService))
	globalStorageAdmin.Use(middleware.AuditMiddleware(auditService))
	{
		globalStorageAdmin.POST("/list", h.AdminGlobalStorageConfigList)
		globalStorageAdmin.POST("/detail", h.AdminGlobalStorageConfigDetail)
		globalStorageAdmin.POST("/create", h.AdminGlobalStorageConfigCreate)
		globalStorageAdmin.POST("/update", h.AdminGlobalStorageConfigUpdate)
		globalStorageAdmin.POST("/delete", h.AdminGlobalStorageConfigDelete)
		globalStorageAdmin.POST("/test", h.AdminGlobalStorageConfigTest)
	}

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

	workflowCompat := router.Group("/api/app/workflow")
	workflowCompat.Use(middleware.AdminAccess(adminService, appService, governanceService))
	workflowCompat.Use(middleware.AuditMiddleware(auditService))
	{
		workflowCompat.POST("/list", h.WorkflowList)
		workflowCompat.POST("/create", h.WorkflowCreate)
		workflowCompat.POST("/detail", h.WorkflowDetail)
		workflowCompat.POST("/info", h.WorkflowDetail)
		workflowCompat.POST("/update", h.WorkflowUpdate)
		workflowCompat.POST("/delete", h.WorkflowDelete)
		workflowCompat.POST("/start", h.WorkflowStart)
		workflowCompat.POST("/instances", h.WorkflowInstances)
		workflowCompat.POST("/instances/list", h.WorkflowInstances)
		workflowCompat.POST("/instance/detail", h.WorkflowInstanceDetail)
		workflowCompat.POST("/instances/info", h.WorkflowInstanceDetail)
		workflowCompat.POST("/instance/pause", h.WorkflowInstancePause)
		workflowCompat.POST("/instances/pause", h.WorkflowInstancePause)
		workflowCompat.POST("/instance/resume", h.WorkflowInstanceResume)
		workflowCompat.POST("/instances/resume", h.WorkflowInstanceResume)
		workflowCompat.POST("/instance/cancel", h.WorkflowInstanceCancel)
		workflowCompat.POST("/instances/cancel", h.WorkflowInstanceCancel)
		workflowCompat.POST("/tasks/todo", h.WorkflowTasksTodo)
		workflowCompat.POST("/task/detail", h.WorkflowTaskDetail)
		workflowCompat.POST("/tasks/complete", h.WorkflowTaskComplete)
		workflowCompat.POST("/task/complete", h.WorkflowTaskComplete)
		workflowCompat.POST("/task/assign", h.WorkflowTaskAssign)
		workflowCompat.POST("/task/history", h.WorkflowTaskHistory)
		workflowCompat.POST("/templates", h.WorkflowTemplates)
		workflowCompat.POST("/templates/list", h.WorkflowTemplates)
		workflowCompat.POST("/create-from-template", h.WorkflowCreateFromTemplate)
		workflowCompat.POST("/templates/create", h.WorkflowCreateFromTemplate)
		workflowCompat.POST("/save-as-template", h.WorkflowSaveAsTemplate)
		workflowCompat.POST("/validate", h.WorkflowValidate)
		workflowCompat.POST("/node-types", h.WorkflowNodeTypes)
		workflowCompat.POST("/statistics", h.WorkflowStatistics)
		workflowCompat.POST("/logs", h.WorkflowLogs)
		workflowCompat.POST("/engine/status", h.WorkflowEngineStatus)
	}

	adminSystem := router.Group("/api/admin/system")
	adminSystem.Use(middleware.AdminAccess(adminService, appService, governanceService))
	adminSystem.Use(middleware.AuditMiddleware(auditService))
	{
		adminSystem.GET("/roles", h.AdminRoleCatalog)
		adminSystem.GET("/roles/permissions", h.AdminRolePermissionTree)
		adminSystem.GET("/admins", h.AdminListAccounts)
		adminSystem.POST("/admins", h.AdminCreateAccount)
		// 会话管理（注意：/admins/online 必须在 /admins/:adminId 之前注册）
		adminSystem.GET("/admins/online", h.ListOnlineAdmins)
		adminSystem.PUT("/admins/:adminId/status", h.AdminUpdateAccountStatus)
		adminSystem.PUT("/admins/:adminId/access", h.AdminUpdateAccountAccess)
		adminSystem.GET("/admins/:adminId/sessions", h.ListAdminSessions)
		adminSystem.POST("/admins/:adminId/force-logout", h.ForceLogoutAdmin)
		adminSystem.GET("/sessions", h.ListAllSessions)
		adminSystem.POST("/sessions/:sessionId/revoke", h.RevokeSession)
		adminSystem.GET("/temp-permissions", h.ListTempPermissions)
		adminSystem.POST("/temp-permissions", h.GrantTempPermission)
		adminSystem.POST("/temp-permissions/:permId/revoke", h.RevokeTempPermission)
		adminSystem.GET("/delegations", h.ListDelegations)
		adminSystem.POST("/delegations", h.CreateDelegation)
		adminSystem.POST("/delegations/:delegationId/revoke", h.RevokeDelegation)
		adminSystem.GET("/runtime", h.AdminSystemRuntime)
		adminSystem.GET("/settings", h.AdminGetSystemSettings)
		adminSystem.PUT("/settings", h.AdminUpdateSystemSettings)
		// 出海代理网关：按域名后缀把出站流量路由到境外线路
		adminSystem.GET("/egress", h.AdminGetEgressSettings)
		adminSystem.PUT("/egress", h.AdminUpdateEgressSettings)
		adminSystem.POST("/egress/reset", h.AdminResetEgressSettings)
		adminSystem.POST("/egress/test", h.AdminTestEgress)
		adminSystem.POST("/egress/explain", h.AdminExplainEgress)
		adminSystem.POST("/egress/probe", h.AdminProbeEgress)
		adminSystem.POST("/ldap/test", h.AdminLDAPTest)
		adminSystem.POST("/oidc/test", h.AdminOIDCTest)
		adminSystem.POST("/saml/test", h.AdminSAMLTest)
		// 插件系统
		adminSystem.GET("/plugins", h.AdminListPlugins)
		adminSystem.POST("/plugins", h.AdminCreatePlugin)
		adminSystem.GET("/plugins/registry", h.AdminGetHookRegistry)
		adminSystem.GET("/plugins/executions", h.AdminListHookExecutions)
		adminSystem.GET("/plugins/:id", h.AdminGetPlugin)
		adminSystem.PUT("/plugins/:id", h.AdminUpdatePlugin)
		adminSystem.DELETE("/plugins/:id", h.AdminDeletePlugin)
		adminSystem.POST("/plugins/:id/enable", h.AdminEnablePlugin)
		adminSystem.POST("/plugins/:id/disable", h.AdminDisablePlugin)
		adminSystem.POST("/roles", h.AdminCreateCustomRole)
		adminSystem.PUT("/roles/:roleKey", h.AdminUpdateCustomRole)
		adminSystem.DELETE("/roles/:roleKey", h.AdminDeleteCustomRole)
		adminSystem.GET("/roles/matrix", h.AdminGetRoleMatrix)
		adminSystem.GET("/roles/graph", h.AdminGetRoleGraph)
		adminSystem.GET("/roles/:roleKey/impact", h.AdminGetRoleImpactPreview)
		// ── 组织架构 ──
		//
		// 全部实体一律用 UUID 定位，且每条子路径都挂在 /organizations/:orgId 之下 ——
		// 组织归属由路径本身携带，服务层据此做隔离校验。
		// 旧路由把部门/岗位/审批链挂在顶层（/departments/:deptId），
		// 请求里根本没有组织信息，也就无从校验「这个部门是不是你家的」。
		adminSystem.GET("/org-metadata", h.OrgMetadata)
		adminSystem.GET("/organizations", h.ListOrganizations)
		adminSystem.POST("/organizations", h.CreateOrganization)
		adminSystem.GET("/organizations/tree", h.OrganizationTree)
		adminSystem.GET("/organizations/:orgId", h.GetOrganization)
		adminSystem.PUT("/organizations/:orgId", h.UpdateOrganization)
		adminSystem.DELETE("/organizations/:orgId", h.DeleteOrganization)
		adminSystem.GET("/organizations/:orgId/overview", h.GetOrgOverview)
		adminSystem.POST("/organizations/:orgId/transfer", h.TransferOrganization)
		adminSystem.GET("/organizations/:orgId/activity", h.ListOrgActivity)

		// 部门
		adminSystem.GET("/organizations/:orgId/departments", h.GetDepartmentTree)
		adminSystem.POST("/organizations/:orgId/departments", h.CreateDepartment)
		adminSystem.PUT("/organizations/:orgId/dept-order", h.ReorderDepartments)
		adminSystem.GET("/organizations/:orgId/departments/:deptId", h.GetDepartment)
		adminSystem.PUT("/organizations/:orgId/departments/:deptId", h.UpdateDepartment)
		adminSystem.DELETE("/organizations/:orgId/departments/:deptId", h.DeleteDepartment)
		adminSystem.PUT("/organizations/:orgId/departments/:deptId/move", h.MoveDepartment)

		// 部门成员
		adminSystem.GET("/organizations/:orgId/departments/:deptId/members", h.ListDepartmentMembers)
		adminSystem.PUT("/organizations/:orgId/departments/:deptId/members/:adminId", h.SetDepartmentMember)
		adminSystem.DELETE("/organizations/:orgId/departments/:deptId/members/:adminId", h.RemoveDepartmentMember)
		adminSystem.GET("/organizations/:orgId/departments/:deptId/members/:adminId/reporting-chain", h.GetReportingChain)
		adminSystem.GET("/organizations/:orgId/departments/:deptId/members/:adminId/reports", h.GetDirectReports)

		// 组织成员
		adminSystem.GET("/organizations/:orgId/members", h.ListOrgMembers)
		adminSystem.POST("/organizations/:orgId/members", h.AddOrgMember)
		adminSystem.GET("/organizations/:orgId/assignable-admins", h.SearchAssignableAdmins)
		adminSystem.GET("/organizations/:orgId/members/:adminId", h.GetOrgMember)
		adminSystem.PUT("/organizations/:orgId/members/:adminId", h.UpdateOrgMember)
		adminSystem.DELETE("/organizations/:orgId/members/:adminId", h.RemoveOrgMember)
		adminSystem.PUT("/organizations/:orgId/members/:adminId/departments", h.AssignMemberDepartments)

		// 邀请：受邀人此时还不在组织里，所以入口挂在个人维度
		adminSystem.POST("/organizations/:orgId/invitations", h.InviteOrgMembers)
		adminSystem.GET("/org-invitations", h.ListMyInvitations)
		adminSystem.GET("/org-invitations/count", h.CountPendingInvitations)
		adminSystem.POST("/org-invitations/:inviteId/:action", h.RespondInvitation)

		// 岗位
		adminSystem.GET("/organizations/:orgId/positions", h.ListPositions)
		adminSystem.POST("/organizations/:orgId/positions", h.CreatePosition)
		adminSystem.PUT("/organizations/:orgId/positions/:posId", h.UpdatePosition)
		adminSystem.DELETE("/organizations/:orgId/positions/:posId", h.DeletePosition)

		// 组织角色与授权
		adminSystem.GET("/organizations/:orgId/roles", h.ListOrgRoles)
		adminSystem.POST("/organizations/:orgId/roles", h.CreateOrgRole)
		adminSystem.PUT("/organizations/:orgId/roles/:roleId", h.UpdateOrgRole)
		adminSystem.DELETE("/organizations/:orgId/roles/:roleId", h.DeleteOrgRole)
		adminSystem.GET("/organizations/:orgId/roles/:roleId/grants", h.ListOrgRoleGrants)
		adminSystem.POST("/organizations/:orgId/roles/:roleId/grants", h.GrantOrgRole)
		adminSystem.DELETE("/organizations/:orgId/roles/:roleId/grants/:adminId", h.RevokeOrgRole)

		// 审批链
		adminSystem.GET("/organizations/:orgId/approval-chains", h.ListApprovalChains)
		adminSystem.POST("/organizations/:orgId/approval-chains", h.CreateApprovalChain)
		adminSystem.PUT("/organizations/:orgId/approval-chains/:chainId", h.UpdateApprovalChain)
		adminSystem.DELETE("/organizations/:orgId/approval-chains/:chainId", h.DeleteApprovalChain)

		// 审批实例：待办跨组织聚合，因此单列一个个人维度入口
		adminSystem.GET("/organizations/:orgId/approvals", h.ListApprovalInstances)
		adminSystem.GET("/organizations/:orgId/approvals/:instanceId", h.GetApprovalInstance)
		adminSystem.GET("/org-approvals/pending", h.ListMyPendingApprovals)
		adminSystem.POST("/org-approvals/:instanceId/decision", h.DecideApproval)

		// 权限模板
		adminSystem.GET("/organizations/:orgId/perm-templates", h.ListOrgPermTemplates)
		adminSystem.POST("/organizations/:orgId/perm-templates", h.CreateOrgPermTemplate)
		adminSystem.DELETE("/organizations/:orgId/perm-templates/:templateId", h.DeleteOrgPermTemplate)
		adminSystem.POST("/organizations/:orgId/perm-templates/:templateId/apply", h.ApplyPermTemplate)

		// 应用资源绑定
		adminSystem.GET("/organizations/:orgId/apps", h.ListOrgApps)
		adminSystem.POST("/organizations/:orgId/apps", h.BindOrgApp)
		adminSystem.DELETE("/organizations/:orgId/apps/:appId", h.UnbindOrgApp)

		// 协作组
		adminSystem.GET("/organizations/:orgId/collab-groups", h.ListCollabGroups)
		adminSystem.POST("/organizations/:orgId/collab-groups", h.CreateCollabGroup)
		adminSystem.PUT("/organizations/:orgId/collab-groups/:groupId", h.UpdateCollabGroup)
		adminSystem.DELETE("/organizations/:orgId/collab-groups/:groupId", h.DeleteCollabGroup)

		// 导入导出（Excel）
		adminSystem.GET("/organizations/:orgId/export", h.ExportOrganization)
		adminSystem.GET("/organizations/:orgId/import-template", h.DownloadImportTemplate)
		adminSystem.POST("/organizations/:orgId/import", h.ImportOrganization)
		adminSystem.GET("/templates", h.ListTemplates)
		adminSystem.GET("/templates/:code", h.GetTemplate)
		adminSystem.POST("/templates", h.CreateTemplate)
		adminSystem.PUT("/templates/:code", h.UpdateTemplate)
		adminSystem.DELETE("/templates/:code", h.DeleteTemplate)
		adminSystem.POST("/templates/:code/preview", h.PreviewTemplate)
		adminSystem.GET("/audit-logs", h.ListAuditLogs)
		adminSystem.GET("/audit-logs/stats", h.GetAuditStats)
		adminSystem.GET("/audit-logs/export", h.ExportAuditLogs)
		adminSystem.GET("/audit-logs/:id", h.GetAuditLog)

		// 设备营销名称字典（仅超级管理员可改，handler 内部校验）
		adminSystem.GET("/device-marketing-names", h.ListDeviceMarketingNames)
		adminSystem.GET("/device-marketing-names/manufacturers", h.ListDeviceMarketingManufacturers)
		adminSystem.GET("/device-marketing-names/lookup", h.LookupDeviceMarketingName)
		adminSystem.POST("/device-marketing-names", h.CreateDeviceMarketingName)
		adminSystem.POST("/device-marketing-names/seed", h.ReseedDeviceMarketingNames)
		adminSystem.GET("/device-marketing-names/:id", h.GetDeviceMarketingName)
		adminSystem.PUT("/device-marketing-names/:id", h.UpdateDeviceMarketingName)
		adminSystem.DELETE("/device-marketing-names/:id", h.DeleteDeviceMarketingName)
		adminSystem.GET("/online/stats", h.AdminOnlineStats)
		adminSystem.GET("/online/apps/:appkey", h.AdminAppOnlineStats)
		adminSystem.GET("/online/apps/:appkey/users", h.AdminAppOnlineUsers)
		adminSystem.GET("/firewall/logs", h.AdminFirewallLogs)
		adminSystem.GET("/firewall/logs/:logId", h.AdminFirewallLogDetail)
		adminSystem.GET("/firewall/stats", h.AdminFirewallStats)
		adminSystem.DELETE("/firewall/logs", h.AdminFirewallLogsCleanup)
		adminSystem.GET("/firewall/bans", h.AdminListIPBans)
		adminSystem.GET("/firewall/bans/modes", h.AdminListIPBanModes)
		adminSystem.POST("/firewall/bans", h.AdminBanIP)
		adminSystem.DELETE("/firewall/bans/:banId", h.AdminUnbanIP)
		adminSystem.GET("/firewall/geo-bans", h.AdminListGeoBans)
		adminSystem.POST("/firewall/geo-bans", h.AdminUpsertGeoBan)
		adminSystem.PATCH("/firewall/geo-bans/:id", h.AdminToggleGeoBan)
		adminSystem.DELETE("/firewall/geo-bans/:id", h.AdminDeleteGeoBan)

		// 地理围栏管理
		adminSystem.GET("/firewall/geo-fences", h.AdminListGeoFences)
		adminSystem.POST("/firewall/geo-fences", h.AdminCreateGeoFence)
		adminSystem.POST("/firewall/geo-fences/preview", h.AdminPreviewGeoFence)
		adminSystem.PUT("/firewall/geo-fences/:id", h.AdminUpdateGeoFence)
		adminSystem.PATCH("/firewall/geo-fences/:id", h.AdminToggleGeoFence)
		adminSystem.DELETE("/firewall/geo-fences/:id", h.AdminDeleteGeoFence)

		// 地理分析（热力图 / 攻击聚类 / 登录轨迹）
		adminSystem.GET("/geo/heatmap", h.AdminGeoHeatmap)
		adminSystem.GET("/geo/clusters", h.AdminGeoClusters)
		adminSystem.GET("/geo/trail", h.AdminUserGeoTrail)

		// 系统公告管理
		adminSystem.GET("/announcements", h.AdminListAnnouncements)
		adminSystem.POST("/announcements", h.AdminCreateAnnouncement)
		adminSystem.GET("/announcements/:id", h.AdminGetAnnouncement)
		adminSystem.PUT("/announcements/:id", h.AdminUpdateAnnouncement)
		adminSystem.DELETE("/announcements/:id", h.AdminDeleteAnnouncement)
		adminSystem.POST("/announcements/:id/publish", h.AdminPublishAnnouncement)
		adminSystem.POST("/announcements/:id/archive", h.AdminArchiveAnnouncement)

		// 崩溃日志管理（仅超级管理员）
		clh := &crashLogHandlers{cl: cl}
		adminSystem.GET("/crashlogs", clh.ListCrashLogs)
		adminSystem.GET("/crashlogs/:filename", clh.GetCrashLog)
		adminSystem.DELETE("/crashlogs/:filename", clh.DeleteCrashLog)

		// 内存管理（仅超级管理员）
		mh := &memoryHandlers{mm: memoryManager}
		adminSystem.GET("/memory/snapshot", mh.AdminMemorySnapshot)
		adminSystem.POST("/memory/gc", mh.AdminMemoryForceGC)
		adminSystem.PUT("/memory/gogc", mh.AdminMemorySetGOGC)
		adminSystem.GET("/memory/history", mh.AdminMemoryHistory)
		adminSystem.GET("/memory/pools", mh.AdminMemoryPoolStats)
		adminSystem.GET("/memory/cache", mh.AdminMemoryCacheStats)
		adminSystem.DELETE("/memory/cache", mh.AdminMemoryFlushCaches)
		adminSystem.GET("/memory/leak", mh.AdminMemoryLeakReport)

		// 数据库生命周期与泄漏监控（仅超级管理员）
		dbh := &databaseHandlers{dm: databaseManager}
		adminSystem.GET("/database/snapshot", dbh.AdminDatabaseSnapshot)
		adminSystem.POST("/database/refresh", dbh.AdminDatabaseRefresh)
		adminSystem.GET("/database/history", dbh.AdminDatabaseHistory)
		adminSystem.GET("/database/leak", dbh.AdminDatabaseLeak)
		adminSystem.GET("/database/slow-queries", dbh.AdminDatabaseSlowQueries)
		adminSystem.GET("/database/sessions", dbh.AdminDatabaseSessions)
		adminSystem.POST("/database/sessions/:pid/terminate", dbh.AdminDatabaseTerminateSession)
		adminSystem.POST("/database/sessions/:pid/cancel", dbh.AdminDatabaseCancelSession)
		adminSystem.GET("/database/maintenance", dbh.AdminDatabaseMaintenance)
		adminSystem.POST("/database/warmup", dbh.AdminDatabaseWarmup)

		// 存储资源中心
		adminSystem.GET("/storage/objects", h.ListStorageObjects)
		adminSystem.POST("/storage/objects/batch", h.BatchMutateStorageObjects)
		adminSystem.GET("/storage/objects/:objectId", h.GetStorageObjectDetail)
		adminSystem.DELETE("/storage/objects/:objectId", h.SoftDeleteStorageObject)
		adminSystem.POST("/storage/objects/:objectId/restore", h.RestoreStorageObject)
		adminSystem.POST("/storage/objects/:objectId/link", h.CreateStorageObjectLink)
		adminSystem.GET("/storage/objects/:objectId/thumbnail", h.GetStorageObjectThumbnail)
		adminSystem.DELETE("/storage/objects/:objectId/permanent", h.PermanentDeleteStorageObject)
		adminSystem.GET("/storage/trash", h.ListTrashObjects)
		adminSystem.POST("/storage/trash/cleanup", h.CleanupTrash)
		adminSystem.GET("/storage/rules", h.ListStorageRules)
		adminSystem.POST("/storage/rules", h.CreateStorageRule)
		adminSystem.PUT("/storage/rules/:ruleId", h.UpdateStorageRule)
		adminSystem.DELETE("/storage/rules/:ruleId", h.DeleteStorageRule)
		adminSystem.GET("/storage/cdn/:configId", h.GetCDNConfig)
		adminSystem.PUT("/storage/cdn/:configId", h.UpsertCDNConfig)
		adminSystem.DELETE("/storage/cdn/:configId", h.DeleteCDNConfig)
		adminSystem.GET("/storage/image-rules", h.ListImageRules)
		adminSystem.POST("/storage/image-rules", h.CreateImageRule)
		adminSystem.DELETE("/storage/image-rules/:ruleId", h.DeleteImageRule)
		adminSystem.GET("/storage/usage", h.GetStorageUsage)
		adminSystem.GET("/storage/usage/history", h.GetStorageUsageHistory)

		// 用户主数据中心
		adminSystem.GET("/user-master/identities", h.AdminListIdentities)
		adminSystem.POST("/user-master/identities", h.AdminCreateIdentity)
		adminSystem.GET("/user-master/identities/:id", h.AdminGetIdentity)
		adminSystem.PUT("/user-master/identities/:id/status", h.AdminUpdateIdentityStatus)
		adminSystem.PUT("/user-master/identities/:id/lifecycle", h.AdminUpdateIdentityLifecycle)
		adminSystem.PUT("/user-master/identities/:id/risk", h.AdminUpdateIdentityRisk)
		adminSystem.GET("/user-master/identities/:id/mappings", h.AdminListMappingsByIdentity)
		adminSystem.GET("/user-master/identities/:id/tags", h.AdminListIdentityTags)
		adminSystem.POST("/user-master/mappings", h.AdminCreateMapping)
		adminSystem.DELETE("/user-master/mappings/:id", h.AdminDeleteMapping)
		adminSystem.GET("/user-master/tags", h.AdminListUserTags)
		adminSystem.POST("/user-master/tags", h.AdminCreateUserTag)
		adminSystem.DELETE("/user-master/tags/:id", h.AdminDeleteUserTag)
		adminSystem.POST("/user-master/tags/assign", h.AdminAssignTag)
		adminSystem.POST("/user-master/tags/remove", h.AdminRemoveTag)
		adminSystem.GET("/user-master/segments", h.AdminListSegments)
		adminSystem.POST("/user-master/segments", h.AdminCreateSegment)
		adminSystem.PUT("/user-master/segments/:id", h.AdminUpdateSegment)
		adminSystem.DELETE("/user-master/segments/:id", h.AdminDeleteSegment)
		adminSystem.GET("/user-master/segments/:id/members", h.AdminListSegmentMembers)
		adminSystem.POST("/user-master/segments/:id/members", h.AdminAddSegmentMember)
		adminSystem.DELETE("/user-master/segments/:id/members/:identityId", h.AdminRemoveSegmentMember)
		adminSystem.GET("/user-master/lists", h.AdminListUserListEntries)
		adminSystem.POST("/user-master/lists", h.AdminCreateUserListEntry)
		adminSystem.DELETE("/user-master/lists/:id", h.AdminDeleteUserListEntry)
		adminSystem.POST("/user-master/lists/check", h.AdminCheckBlacklist)
		adminSystem.POST("/user-master/merges", h.AdminMergeIdentity)
		adminSystem.GET("/user-master/merges", h.AdminListMerges)
		adminSystem.GET("/user-master/appeals", h.AdminListAppeals)
		adminSystem.POST("/user-master/appeals", h.AdminCreateAppeal)
		adminSystem.PUT("/user-master/appeals/:id", h.AdminReviewAppeal)
		adminSystem.GET("/user-master/deactivations", h.AdminListDeactivations)
		adminSystem.POST("/user-master/deactivations", h.AdminCreateDeactivation)
		adminSystem.POST("/user-master/deactivations/:id/cancel", h.AdminCancelDeactivation)
		adminSystem.POST("/user-master/sync", h.AdminSyncIdentity)
		adminSystem.POST("/user-master/sync/batch", h.AdminBatchSyncIdentities)

		// 风控中心
		// 目录（/metadata）驱动控制台的场景下拉、条件参数表单与表达式提示：
		// 后端新增一种条件类型时前端零改动即自动出现，不要在前端另抄一份枚举。
		adminSystem.GET("/risk/metadata", h.AdminRiskMetadata)
		adminSystem.POST("/risk/expression/validate", h.AdminValidateRiskExpression)
		adminSystem.GET("/risk/dashboard", h.AdminRiskDashboard)
		adminSystem.POST("/risk/evaluate", h.AdminEvaluateRisk)
		adminSystem.POST("/risk/simulate", h.AdminSimulateRisk)
		adminSystem.GET("/risk/rules", h.AdminListRiskRules)
		adminSystem.POST("/risk/rules", h.AdminCreateRiskRule)
		adminSystem.GET("/risk/rules/:id", h.AdminGetRiskRule)
		adminSystem.PUT("/risk/rules/:id", h.AdminUpdateRiskRule)
		adminSystem.DELETE("/risk/rules/:id", h.AdminDeleteRiskRule)
		adminSystem.POST("/risk/rules/:id/simulate", h.AdminSimulateRiskRule)
		adminSystem.GET("/risk/assessments", h.AdminListRiskAssessments)
		adminSystem.DELETE("/risk/assessments", h.AdminPurgeRiskAssessments)
		adminSystem.GET("/risk/assessments/:id", h.AdminGetRiskAssessment)
		adminSystem.POST("/risk/assessments/:id/review", h.AdminReviewRiskAssessment)
		adminSystem.POST("/risk/assessments/:id/replay", h.AdminReplayRiskAssessment)
		adminSystem.GET("/risk/reviews/pending", h.AdminListPendingReviews)
		adminSystem.GET("/risk/devices", h.AdminListRiskDevices)
		adminSystem.GET("/risk/devices/suspicious", h.AdminListSuspiciousDevices)
		adminSystem.GET("/risk/devices/:deviceId", h.AdminGetDeviceFingerprint)
		adminSystem.PUT("/risk/devices/:id/tag", h.AdminUpdateDeviceRiskTag)
		adminSystem.GET("/risk/ips", h.AdminListHighRiskIPs)
		adminSystem.GET("/risk/ips/:ip", h.AdminGetIPRisk)
		adminSystem.POST("/risk/ips/:ip/refresh", h.AdminRefreshIPReputation)
		adminSystem.PUT("/risk/ips/:id/tag", h.AdminUpdateIPRiskTag)
		adminSystem.GET("/risk/actions", h.AdminListRiskActions)
		adminSystem.POST("/risk/actions", h.AdminCreateRiskAction)
		adminSystem.PUT("/risk/actions/:id", h.AdminUpdateRiskAction)
		adminSystem.DELETE("/risk/actions/:id", h.AdminDeleteRiskAction)

		// 平台级 Banner 的"写"操作：统一套 RequireSuperAdmin，仅超管可进行 CRUD
		platformBannerGroup := adminSystem.Group("/banners", middleware.RequireSuperAdmin())
		{
			platformBannerGroup.GET("", h.ListPlatformBanners)
			platformBannerGroup.POST("", h.CreatePlatformBanner)
			platformBannerGroup.GET("/:id", h.GetPlatformBanner)
			platformBannerGroup.PUT("/:id", h.UpdatePlatformBanner)
			platformBannerGroup.DELETE("/:id", h.DeletePlatformBanner)
			platformBannerGroup.POST("/bulk-delete", h.BulkDeletePlatformBanners)
			platformBannerGroup.POST("/upload", h.UploadPlatformBannerImage)
		}
	}

	// 平台级 Banner · 激活列表（/active）：
	//   概览页所有已登录管理员都要读取，不应走 adminSystem 组的 AdminAccess RBAC
	//   （该 RBAC 会因当前管理员无 banners:read 权限返回 40311）。
	//   因此在 adminSystem 组外单独注册：仅 AdminAuth（验证会话） + AuditMiddleware（审计），
	//   不再参与 Casbin 授权判定。所有管理员无差别可读。
	router.GET(
		"/api/admin/system/banners/active",
		middleware.AdminAuth(adminService),
		middleware.AuditMiddleware(auditService),
		h.GetActivePlatformBanners,
	)

	docsOptions := DefaultDocsOptions()
	if trimmed := strings.TrimSpace(docsPortalURL); trimmed != "" {
		docsOptions.PortalURL = trimmed
	}
	if err := RegisterDocsRoutes(router, docsOptions); err != nil {
		return nil, fmt.Errorf("register docs routes: %w", err)
	}

	return router, nil
}

func (h *Handler) Healthz(c *gin.Context) {
	if h.monitor == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "系统监测服务暂不可用")
		return
	}
	response.Success(c, 200, "ok", gin.H{
		"status":    "healthy",
		"checkedAt": time.Now().UTC(),
	})
}

func (h *Handler) Readyz(c *gin.Context) {
	if h.monitor == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "系统监测服务暂不可用")
		return
	}
	_, ready := h.monitor.ReadinessReport(c.Request.Context())
	c.Header("Cache-Control", "no-store")
	if !ready {
		response.Error(c, http.StatusServiceUnavailable, 50312, "服务未就绪")
		return
	}
	response.Success(c, 200, "ok", gin.H{
		"status":    "ready",
		"checkedAt": time.Now().UTC(),
	})
}

func (h *Handler) PasswordLogin(c *gin.Context) {
	var req PasswordLoginRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if !h.enforceLegacyAuthProtocol(c, req.AppID, "password", false) {
		return
	}
	// 前置校验：如果该 App 启用了图形验证码，必须校验通过才能登录
	if !h.verifyAppCaptcha(c, req.AppID, req.CaptchaID, req.CaptchaAnswer, captchadomain.PurposeLogin) {
		return
	}
	// 前置校验：登录设备检查 — 若 App 开启，必须由客户端显式提供 deviceId + device
	// 合法来源：body(deviceId/device) 或 Header(X-Device-Id/X-Device-Name)，UA 推断不算数
	if !h.enforceDevicePolicy(c, req.AppID, req.DeviceID, req.Device, req.MarkCode, captchadomain.PurposeLogin) {
		return
	}
	// 1) 取客户端显式值（body / header），未做任何加工
	deviceID, clientDevice := resolveClientDevice(c, req.DeviceID, req.Device, req.MarkCode)
	// 2) 查字典：命中 → 营销名；未命中 → 保留 clientDevice 原样
	device := h.enrichDeviceFromDict(c, deviceID, clientDevice)
	// 3) 客户端未传且字典未命中时，再用 UA 兜底
	if device == "" {
		device = guessDeviceFromUA(c.Request.UserAgent())
	}
	result, err := h.auth.PasswordLogin(c.Request.Context(), req.AppID, req.Account, req.Password, deviceID, device, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, authResultMessage(result, "登录成功"), result)
}

func (h *Handler) PasswordRegister(c *gin.Context) {
	var req PasswordRegisterRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if !h.enforceLegacyAuthProtocol(c, req.AppID, "password", true) {
		return
	}
	// 前置校验：注册同样受 App 验证码策略约束
	if !h.verifyAppCaptcha(c, req.AppID, req.CaptchaID, req.CaptchaAnswer, captchadomain.PurposeRegister) {
		return
	}
	// 前置校验：登录设备检查 — 注册后进入登录态，同样适用
	if !h.enforceDevicePolicy(c, req.AppID, req.DeviceID, req.Device, req.MarkCode, captchadomain.PurposeRegister) {
		return
	}
	deviceID, clientDevice := resolveClientDevice(c, req.DeviceID, req.Device, req.MarkCode)
	device := h.enrichDeviceFromDict(c, deviceID, clientDevice)
	if device == "" {
		device = guessDeviceFromUA(c.Request.UserAgent())
	}
	result, err := h.auth.RegisterWithPassword(c.Request.Context(), service.PasswordRegisterInput{
		AppID:     req.AppID,
		Account:   req.Account,
		Password:  req.Password,
		Nickname:  req.Nickname,
		DeviceID:  deviceID,
		Device:    device,
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, authResultMessage(result, "注册成功"), result)
}

func (h *Handler) OAuthAuthURL(c *gin.Context) {
	var req OAuthAuthURLRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	// Web OAuth 启动阶段仅能携带 deviceId（device 名称由 302 链路无法传递，最终签发时再严格校验）
	// 因此这里只需保证策略开启时 deviceId 来自客户端显式字段
	if !h.enforceDevicePolicyIDOnly(c, req.AppID, req.DeviceID, req.MarkCode) {
		return
	}
	deviceID, _ := resolveDeviceInfo(c, req.DeviceID, req.Device, req.MarkCode)
	url, err := h.auth.BuildOAuthAuthURL(c.Request.Context(), req.Provider, req.AppID, deviceID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取授权地址成功", gin.H{"url": url})
}

// OAuthCallback 同一回调地址承载「登录/注册」与「已登录用户绑定」两条链路，
// 由授权发起时写入 state 的 purpose 区分。登录场景的响应结构与历史版本保持一致。
func (h *Handler) OAuthCallback(c *gin.Context) {
	provider := c.Query("provider")
	code := c.Query("code")
	state := c.Query("state")
	outcome, err := h.auth.HandleOAuthCallback(c.Request.Context(), provider, code, state, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		h.writeError(c, err)
		return
	}
	if outcome.Binding != nil {
		response.Success(c, 200, "第三方账号绑定成功", outcome.Binding)
		return
	}
	response.Success(c, 200, authResultMessage(outcome.Login, "OAuth2 登录成功"), outcome.Login)
}

func (h *Handler) OAuthMobileLogin(c *gin.Context) {
	var req OAuthMobileLoginRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.oauthMobileLoginWith(c, req)
}

// oauthMobileLoginWith 承载移动端 OAuth 换取会话的实际流程。
// 旧 /api/auth/* 从 body 取 appid，v1 网关从路径取 appKey，两条入口共用这一段。
func (h *Handler) oauthMobileLoginWith(c *gin.Context, req OAuthMobileLoginRequest) {
	profile := authdomain.ProviderProfile{
		Provider:       req.Provider,
		ProviderUserID: req.ProviderUserID,
		UnionID:        req.UnionID,
		Nickname:       req.Nickname,
		Avatar:         req.Avatar,
		Email:          req.Email,
		RawProfile:     req.RawProfile,
		Tokens: map[string]string{
			"access_token":  req.AccessToken,
			"refresh_token": req.RefreshToken,
		},
	}
	if !h.enforceDevicePolicy(c, req.AppID, req.DeviceID, req.Device, req.MarkCode, captchadomain.PurposeLogin) {
		return
	}
	deviceID, clientDevice := resolveClientDevice(c, req.DeviceID, req.Device, req.MarkCode)
	device := h.enrichDeviceFromDict(c, deviceID, clientDevice)
	if device == "" {
		device = guessDeviceFromUA(c.Request.UserAgent())
	}
	result, err := h.auth.MobileOAuthLogin(c.Request.Context(), req.AppID, req.Provider, profile, deviceID, device, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, authResultMessage(result, "OAuth2 登录成功"), result)
}

func (h *Handler) Refresh(c *gin.Context) {
	var req RefreshRequest
	_ = bind(c, &req)
	token := req.RefreshToken
	if token == "" {
		token = req.Token
	}
	if token == "" {
		token = middlewareBearer(c.GetHeader("Authorization"))
	}
	deviceID, _ := resolveDeviceInfo(c, req.DeviceID, req.Device, req.MarkCode)
	result, err := h.auth.Refresh(c.Request.Context(), token, deviceID, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "刷新成功", result)
}

func (h *Handler) Logout(c *gin.Context) {
	tokenValue, _ := c.Get("auth.token")
	token, _ := tokenValue.(string)
	if err := h.auth.Logout(c.Request.Context(), token); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "退出成功", nil)
}

func (h *Handler) VerifyPassword(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req VerifyPasswordRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.auth.VerifyCurrentPassword(c.Request.Context(), session, req.Password); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "验证成功", gin.H{"valid": true})
}

func (h *Handler) ChangePassword(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req ChangePasswordRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.auth.ChangePassword(c.Request.Context(), session, req.CurrentPassword, req.NewPassword); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "密码修改成功", gin.H{"changed": true})
}

func (h *Handler) My(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	view, err := h.user.GetMy(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.attachMyAvatar(c, session, view)
	response.Success(c, 200, "获取成功", view)
}

func (h *Handler) AppPublic(c *gin.Context) {
	var query AppIDQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.app.GetApp(c.Request.Context(), query.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}

	// 获取传输加密配置（公开视图，不含私钥）
	var encryptionView any
	if enc, err := h.app.GetTransportEncryption(c.Request.Context(), query.AppID); err == nil && enc != nil {
		enc.SecretHint = ""
		encryptionView = enc
	}

	response.Success(c, 200, "获取成功", gin.H{
		"id":             item.ID,
		"name":           item.Name,
		"status":         item.Status,
		"registerStatus": item.RegisterStatus,
		"loginStatus":    item.LoginStatus,
		"policy":         h.app.ResolvePolicy(item),
		"settings":       h.app.PublicSettings(item),
		"encryption":     encryptionView,
	})
}

func (h *Handler) UserBanner(c *gin.Context) {
	var query AppIDQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, err := h.app.GetBanners(c.Request.Context(), query.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) UserNotice(c *gin.Context) {
	var query AppIDQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, err := h.app.GetNotices(c.Request.Context(), query.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) CheckVersion(c *gin.Context) {
	var query VersionCheckQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	var session *authdomain.Session
	if value, ok := c.Get("auth.session"); ok {
		session, _ = value.(*authdomain.Session)
	}
	result, err := h.version.CheckForUpdate(c.Request.Context(), query.AppID, query.VersionCode, query.Platform, session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if result == nil || result.Version == nil {
		response.Error(c, http.StatusNotFound, 40430, "暂无新版本信息")
		return
	}
	response.Success(c, 200, "有新版本", result)
}

func (h *Handler) CreateSite(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req SiteCreateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.site.Create(c.Request.Context(), session, appdomain.SiteMutation{
		AppID:       req.AppID,
		Name:        &req.Name,
		URL:         &req.URL,
		Description: &req.Description,
		Type:        &req.Type,
		Header:      &req.Header,
		Category:    &req.Category,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "创建成功，请等待审核。", item)
}

func (h *Handler) UpdateSite(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req SiteUpdateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.site.Update(c.Request.Context(), session, appdomain.SiteMutation{
		ID:          req.ID,
		AppID:       req.AppID,
		Name:        maybeString(req.Name),
		URL:         maybeString(req.URL),
		Description: maybeString(req.Description),
		Type:        maybeString(req.Type),
		Header:      maybeString(req.Header),
		Category:    maybeString(req.Category),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "更新成功，站点需重新审核", item)
}

func (h *Handler) DeleteSite(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req SiteDeleteRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.site.Delete(c.Request.Context(), session, req.ID, req.AppID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", nil)
}

func (h *Handler) SiteDetail(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query SiteDetailQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.site.Detail(c.Request.Context(), session, query.ID, query.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) SiteList(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query SiteListQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.site.PublicList(c.Request.Context(), session, query.AppID, appdomain.SiteListQuery{
		Page:      normalizePage(pickPositive(query.Page, query.PageSize)),
		Limit:     normalizeLimit(pickPositive(query.PageSize, query.Limit)),
		Keyword:   query.Keyword,
		SortBy:    query.SortBy,
		SortOrder: query.SortOrder,
		Category:  query.Category,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{
		"data": result.List,
		"pagination": gin.H{
			"currentPage": result.Page,
			"pageSize":    result.Limit,
			"totalCount":  result.Total,
			"totalPages":  result.TotalPages,
			"hasNextPage": result.HasNextPage,
			"hasPrevPage": result.HasPrevPage,
		},
		"cached": result.Cached,
	})
}

func (h *Handler) SearchSites(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req SiteListQuery
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.site.Search(c.Request.Context(), session, req.AppID, req.Keyword, normalizePage(req.Page), normalizeLimit(req.PageSize))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"data": result.List, "pagination": gin.H{"currentPage": result.Page, "pageSize": result.Limit, "totalCount": result.Total, "totalPages": result.TotalPages}})
}

func (h *Handler) MySites(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req SiteListQuery
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.site.MySites(c.Request.Context(), session, appdomain.SiteListQuery{
		Page:   normalizePage(req.Page),
		Limit:  normalizeLimit(pickPositive(req.Limit, req.PageSize)),
		Status: req.Status,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) ResubmitSite(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req SiteDeleteRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.site.Resubmit(c.Request.Context(), session, req.ID, req.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "重新提交成功，请等待审核", item)
}

func (h *Handler) SubmitRoleApplication(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req RoleApplyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.roleApp.Submit(c.Request.Context(), session, req.AppID, req.RequestedRole, req.Reason, req.Priority, req.ValidDays, map[string]any{
		"ip":        c.ClientIP(),
		"userAgent": c.Request.UserAgent(),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "角色申请提交成功", item)
}

func (h *Handler) RoleApplications(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query RoleApplicationsQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, err := h.roleApp.UserList(c.Request.Context(), session, query.AppID, userdomain.RoleApplicationListQuery{
		Page:   normalizePage(query.Page),
		Limit:  normalizeLimit(query.Limit),
		Status: query.Status,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取角色申请列表成功", items)
}

func (h *Handler) RoleApplicationDetail(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query RoleAppIDQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	applicationID, err := pathInt64(c, "applicationId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "申请ID必须是整数")
		return
	}
	item, err := h.roleApp.UserDetail(c.Request.Context(), session, query.AppID, applicationID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取申请详情成功", item)
}

func (h *Handler) CancelRoleApplication(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req RoleAppIDQuery
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	applicationID, err := pathInt64(c, "applicationId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "申请ID必须是整数")
		return
	}
	item, err := h.roleApp.Cancel(c.Request.Context(), session, req.AppID, applicationID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "申请已取消", item)
}

func (h *Handler) AvailableRoles(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query RoleAppIDQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, err := h.roleApp.AvailableRoles(c.Request.Context(), session, query.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取可申请角色列表成功", items)
}

func (h *Handler) ResubmitRoleApplication(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req RoleResubmitRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	applicationID, err := pathInt64(c, "applicationId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "申请ID必须是整数")
		return
	}
	item, err := h.roleApp.Resubmit(c.Request.Context(), session, req.AppID, applicationID, req.Reason)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "重新提交成功，请等待审核", item)
}

func (h *Handler) AdminApps(c *gin.Context) {
	items, err := h.app.ListApps(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	// 非超管：按角色分配过滤可见应用
	session, ok := adminAccessSession(c)
	if ok && session != nil && !session.IsSuperAdmin {
		items = filterAppsByAssignments(items, session.Assignments)
	}
	response.Success(c, 200, "获取成功", items)
}

// filterAppsByAssignments 按管理员角色分配过滤应用列表
// 全局角色（appID == nil）可见所有应用，应用级角色只可见绑定的应用
func filterAppsByAssignments(apps []appdomain.App, assignments []admindomain.Assignment) []appdomain.App {
	// 如果有任何全局角色，返回全部应用
	for _, a := range assignments {
		if a.AppID == nil {
			return apps
		}
	}
	// 收集有权限的 appID
	allowed := make(map[int64]struct{}, len(assignments))
	for _, a := range assignments {
		if a.AppID != nil {
			allowed[*a.AppID] = struct{}{}
		}
	}
	filtered := make([]appdomain.App, 0, len(allowed))
	for _, app := range apps {
		if _, ok := allowed[app.ID]; ok {
			filtered = append(filtered, app)
		}
	}
	return filtered
}

func (h *Handler) AdminApp(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	item, err := h.app.GetApp(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminAppPolicy(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	item, err := h.app.GetPolicy(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminAppStats(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	item, err := h.app.GetStats(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminAppUserTrend(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var query AdminAppTrendQuery
	_ = bind(c, &query)
	item, err := h.app.GetUserTrend(c.Request.Context(), appID, query.Days)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminAppRegionStats(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var query AdminRegionStatsQuery
	_ = bind(c, &query)
	item, err := h.app.GetRegionStats(c.Request.Context(), appID, appdomain.RegionStatsQuery{
		Type:  query.Type,
		Limit: query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminAppAuthSources(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	item, err := h.app.GetAuthSourceStats(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminAppLoginAudits(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var query AdminLoginAuditQuery
	_ = bind(c, &query)
	item, err := h.app.ListLoginAudits(c.Request.Context(), appID, appdomain.LoginAuditQuery{
		Keyword: query.Keyword,
		Status:  query.Status,
		Page:    query.Page,
		Limit:   query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) ExportAdminAppLoginAudits(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var query AdminLoginAuditQuery
	_ = bind(c, &query)
	items, err := h.app.ExportLoginAudits(c.Request.Context(), appID, appdomain.LoginAuditExportQuery{
		Keyword: query.Keyword,
		Status:  query.Status,
		Limit:   query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}

	filename := "app_login_audits_" + strconv.FormatInt(appID, 10) + ".csv"
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	_ = writer.Write([]string{"id", "user_id", "appid", "account", "nickname", "login_type", "provider", "token_jti", "login_ip", "device_id", "user_agent", "status", "created_at", "metadata"})
	for _, item := range items {
		userID := ""
		if item.UserID != nil {
			userID = strconv.FormatInt(*item.UserID, 10)
		}
		metadata := ""
		if len(item.Metadata) > 0 {
			if encoded, err := json.Marshal(item.Metadata); err == nil {
				metadata = string(encoded)
			}
		}
		_ = writer.Write([]string{
			strconv.FormatInt(item.ID, 10),
			userID,
			strconv.FormatInt(item.AppID, 10),
			item.Account,
			item.Nickname,
			item.LoginType,
			item.Provider,
			item.TokenJTI,
			item.LoginIP,
			item.DeviceID,
			item.UserAgent,
			item.Status,
			item.CreatedAt.UTC().Format(time.RFC3339),
			metadata,
		})
	}
}

func (h *Handler) AdminAppSessionAudits(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var query AdminSessionAuditQuery
	_ = bind(c, &query)
	item, err := h.app.ListSessionAudits(c.Request.Context(), appID, appdomain.SessionAuditQuery{
		Keyword:   query.Keyword,
		EventType: query.EventType,
		Page:      query.Page,
		Limit:     query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) ExportAdminAppSessionAudits(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var query AdminSessionAuditQuery
	_ = bind(c, &query)
	items, err := h.app.ExportSessionAudits(c.Request.Context(), appID, appdomain.SessionAuditExportQuery{
		Keyword:   query.Keyword,
		EventType: query.EventType,
		Limit:     query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}

	filename := "app_session_audits_" + strconv.FormatInt(appID, 10) + ".csv"
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	_ = writer.Write([]string{"id", "user_id", "appid", "account", "nickname", "token_jti", "event_type", "created_at", "metadata"})
	for _, item := range items {
		userID := ""
		if item.UserID != nil {
			userID = strconv.FormatInt(*item.UserID, 10)
		}
		metadata := ""
		if len(item.Metadata) > 0 {
			if encoded, err := json.Marshal(item.Metadata); err == nil {
				metadata = string(encoded)
			}
		}
		_ = writer.Write([]string{
			strconv.FormatInt(item.ID, 10),
			userID,
			strconv.FormatInt(item.AppID, 10),
			item.Account,
			item.Nickname,
			item.TokenJTI,
			item.EventType,
			item.CreatedAt.UTC().Format(time.RFC3339),
			metadata,
		})
	}
}

func (h *Handler) AdminBulkNotifyUsers(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	if _, err := h.app.GetApp(c.Request.Context(), appID); err != nil {
		h.writeError(c, err)
		return
	}
	var req AdminBulkNotificationRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.notifications.AdminBulkSend(c.Request.Context(), appID, notificationdomain.AdminBulkSendCommand{
		UserIDs:  req.UserIDs,
		Keyword:  req.Keyword,
		Enabled:  req.Enabled,
		Limit:    req.Limit,
		Type:     req.Type,
		Title:    req.Title,
		Content:  req.Content,
		Level:    req.Level,
		Metadata: req.Metadata,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "通知发送成功", result)
}

func (h *Handler) AdminAppNotifications(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	if _, err := h.app.GetApp(c.Request.Context(), appID); err != nil {
		h.writeError(c, err)
		return
	}
	var query AdminNotificationListQuery
	_ = bind(c, &query)
	result, err := h.notifications.AdminList(c.Request.Context(), appID, notificationdomain.AdminListQuery{
		Keyword: query.Keyword,
		Type:    query.Type,
		Level:   query.Level,
		Page:    query.Page,
		Limit:   query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) DeleteAdminAppNotifications(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	if _, err := h.app.GetApp(c.Request.Context(), appID); err != nil {
		h.writeError(c, err)
		return
	}
	var req AdminNotificationDeleteRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.notifications.AdminDelete(c.Request.Context(), appID, req.IDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", result)
}

func (h *Handler) DeleteAdminAppNotificationsByFilter(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	if _, err := h.app.GetApp(c.Request.Context(), appID); err != nil {
		h.writeError(c, err)
		return
	}
	var req AdminNotificationDeleteFilterRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.notifications.AdminDeleteByFilter(c.Request.Context(), appID, notificationdomain.AdminExportQuery{
		Keyword: req.Keyword,
		Type:    req.Type,
		Level:   req.Level,
		Limit:   req.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", result)
}

func (h *Handler) ExportAdminAppNotifications(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	if _, err := h.app.GetApp(c.Request.Context(), appID); err != nil {
		h.writeError(c, err)
		return
	}
	var query AdminNotificationListQuery
	_ = bind(c, &query)
	items, err := h.notifications.AdminExport(c.Request.Context(), appID, notificationdomain.AdminExportQuery{
		Keyword: query.Keyword,
		Type:    query.Type,
		Level:   query.Level,
		Limit:   query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}

	filename := "app_notifications_" + strconv.FormatInt(appID, 10) + ".csv"
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	_ = writer.Write([]string{"id", "appid", "user_id", "account", "nickname", "type", "title", "content", "level", "status", "read_at", "created_at", "updated_at", "metadata"})
	for _, item := range items {
		userID := ""
		if item.UserID != nil {
			userID = strconv.FormatInt(*item.UserID, 10)
		}
		readAt := ""
		if item.ReadAt != nil {
			readAt = item.ReadAt.UTC().Format(time.RFC3339)
		}
		metadata := ""
		if len(item.Metadata) > 0 {
			if encoded, err := json.Marshal(item.Metadata); err == nil {
				metadata = string(encoded)
			}
		}
		_ = writer.Write([]string{
			strconv.FormatInt(item.ID, 10),
			strconv.FormatInt(item.AppID, 10),
			userID,
			item.Account,
			item.Nickname,
			item.Type,
			item.Title,
			item.Content,
			item.Level,
			item.Status,
			readAt,
			item.CreatedAt.UTC().Format(time.RFC3339),
			item.UpdatedAt.UTC().Format(time.RFC3339),
			metadata,
		})
	}
}

func (h *Handler) AdminAppUsers(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	if _, err := h.app.GetApp(c.Request.Context(), appID); err != nil {
		h.writeError(c, err)
		return
	}
	var query AdminUserListQuery
	_ = bind(c, &query)
	createdFrom, err := parseOptionalDateTime(query.CreatedFrom)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "createdFrom 格式错误")
		return
	}
	createdTo, err := parseOptionalDateTime(query.CreatedTo)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "createdTo 格式错误")
		return
	}
	if createdTo != nil && len(strings.TrimSpace(query.CreatedTo)) == len("2006-01-02") {
		adjusted := createdTo.Add(24*time.Hour - time.Nanosecond)
		createdTo = &adjusted
	}
	items, err := h.user.ListAdminUsers(c.Request.Context(), appID, userdomain.AdminUserQuery{
		Keyword:     query.Keyword,
		Account:     query.Account,
		Nickname:    query.Nickname,
		Email:       query.Email,
		Phone:       query.Phone,
		InviteCode:  query.InviteCode,
		RegisterIP:  query.RegisterIP,
		UserID:      query.UserID,
		Enabled:     query.Enabled,
		CreatedFrom: createdFrom,
		CreatedTo:   createdTo,
		Sort:        query.Sort,
		Order:       query.Order,
		Page:        query.Page,
		Limit:       query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) ExportAdminAppUsers(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	if _, err := h.app.GetApp(c.Request.Context(), appID); err != nil {
		h.writeError(c, err)
		return
	}
	var query AdminUserListQuery
	_ = bind(c, &query)
	createdFrom, err := parseOptionalDateTime(query.CreatedFrom)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "createdFrom 格式错误")
		return
	}
	createdTo, err := parseOptionalDateTime(query.CreatedTo)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "createdTo 格式错误")
		return
	}
	if createdTo != nil && len(strings.TrimSpace(query.CreatedTo)) == len("2006-01-02") {
		adjusted := createdTo.Add(24*time.Hour - time.Nanosecond)
		createdTo = &adjusted
	}
	items, err := h.user.ExportAdminUsers(c.Request.Context(), appID, userdomain.AdminUserQuery{
		Keyword:     query.Keyword,
		Account:     query.Account,
		Nickname:    query.Nickname,
		Email:       query.Email,
		Phone:       query.Phone,
		InviteCode:  query.InviteCode,
		RegisterIP:  query.RegisterIP,
		UserID:      query.UserID,
		Enabled:     query.Enabled,
		CreatedFrom: createdFrom,
		CreatedTo:   createdTo,
		Sort:        query.Sort,
		Order:       query.Order,
		Limit:       query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}

	filename := "app_users_" + strconv.FormatInt(appID, 10) + ".csv"
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	_ = writer.Write([]string{"id", "appid", "account", "nickname", "email", "phone", "enabled", "integral", "experience", "register_ip", "register_time", "register_province", "register_city", "vip_expire_at"})
	for _, item := range items {
		registerTime := ""
		if item.RegisterTime != nil {
			registerTime = item.RegisterTime.UTC().Format(time.RFC3339)
		}
		vipExpireAt := ""
		if item.VIPExpireAt != nil {
			vipExpireAt = item.VIPExpireAt.UTC().Format(time.RFC3339)
		}
		_ = writer.Write([]string{
			strconv.FormatInt(item.ID, 10),
			strconv.FormatInt(item.AppID, 10),
			item.Account,
			item.Nickname,
			item.Email,
			item.Phone,
			strconv.FormatBool(item.Enabled),
			strconv.FormatInt(item.Integral, 10),
			strconv.FormatInt(item.Experience, 10),
			item.RegisterIP,
			registerTime,
			item.RegisterProvince,
			item.RegisterCity,
			vipExpireAt,
		})
	}
}

func (h *Handler) AdminAppUser(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	if _, err := h.app.GetApp(c.Request.Context(), appID); err != nil {
		h.writeError(c, err)
		return
	}
	item, err := h.user.GetAdminUserDetail(c.Request.Context(), appID, userID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) BatchUpdateAdminAppUserStatus(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	if _, err := h.app.GetApp(c.Request.Context(), appID); err != nil {
		h.writeError(c, err)
		return
	}
	var req AdminUserBatchStatusRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	adminID, adminName := adminActor(c)
	item, err := h.user.BatchUpdateAdminUserStatus(c.Request.Context(), appID, userdomain.AdminUserBatchStatusMutation{
		UserIDs: req.UserIDs,
		AdminUserStatusMutation: userdomain.AdminUserStatusMutation{
			Enabled:              req.Enabled,
			DisabledEndTime:      req.DisabledEndTime,
			ClearDisabledEndTime: req.ClearDisabledEndTime,
			DisabledReason:       req.DisabledReason,
		},
	}, userdomain.BanOperator{AdminID: adminID, AdminName: adminName})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "批量更新成功", item)
}

func (h *Handler) UpdateAdminAppUserStatus(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	if _, err := h.app.GetApp(c.Request.Context(), appID); err != nil {
		h.writeError(c, err)
		return
	}
	var req AdminUserStatusRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	adminID, adminName := adminActor(c)
	item, err := h.user.UpdateAdminUserStatus(c.Request.Context(), appID, userID, userdomain.AdminUserStatusMutation{
		Enabled:              req.Enabled,
		DisabledEndTime:      req.DisabledEndTime,
		ClearDisabledEndTime: req.ClearDisabledEndTime,
		DisabledReason:       req.DisabledReason,
	}, userdomain.BanOperator{AdminID: adminID, AdminName: adminName})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "更新成功", item)
}

func (h *Handler) AdminUpdateUserProfile(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	var req AdminUpdateUserProfileRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.user.AdminUpdateUserProfile(c.Request.Context(), appID, userID, req.Nickname, req.Email); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "用户资料已更新", nil)
}

func (h *Handler) AdminResetUserPassword(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	var req AdminResetUserPasswordRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.user.AdminResetUserPassword(c.Request.Context(), appID, userID, req.NewPassword); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "用户密码已重置", nil)
}

func (h *Handler) AdminRevokeUserSessions(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	if err := h.user.AdminRevokeUserSessions(c.Request.Context(), appID, userID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "用户会话已全部踢出", nil)
}

func (h *Handler) AdminListUserSessions(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	sessions, err := h.user.AdminListUserSessions(c.Request.Context(), appID, userID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	// GeoIP 位置解析（含经纬度与内网标记，供活动地图定位）
	if h.location != nil {
		ips := make([]string, 0, len(sessions))
		for i := range sessions {
			ips = append(ips, sessions[i].IP)
		}
		located := h.resolveIPLocations(c.Request.Context(), ips)
		for i := range sessions {
			loc, ok := located[sessions[i].IP]
			if !ok {
				continue
			}
			sessions[i].Country = loc.Country
			sessions[i].CountryCode = loc.CountryCode
			sessions[i].Region = loc.Region
			sessions[i].City = loc.City
			sessions[i].ISP = loc.ISP
			sessions[i].Location = loc.Location
			sessions[i].Latitude, sessions[i].Longitude = geoCoords(loc)
			sessions[i].IsPrivate = loc.IsPrivate
		}
	}
	response.Success(c, 200, "获取成功", gin.H{"items": sessions, "total": len(sessions)})
}

func (h *Handler) AdminRevokeUserSession(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	tokenHash := c.Param("tokenHash")
	if err := h.user.AdminRevokeUserSession(c.Request.Context(), appID, userID, tokenHash); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "会话已撤销", gin.H{"revoked": 1})
}

func (h *Handler) AdminRevokeUserSessionsBatch(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	var req AdminSessionRevokeBatchRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	revoked, err := h.user.AdminRevokeUserSessionsBatch(c.Request.Context(), appID, userID, req.TokenHashes)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "批量撤销完成", gin.H{"revoked": revoked})
}

func (h *Handler) AdminDeleteUser(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	if err := h.user.AdminDeleteUser(c.Request.Context(), appID, userID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "用户已删除", nil)
}

func (h *Handler) CreateAdminApp(c *gin.Context) {
	var req AdminAppCreateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.app.SaveApp(c.Request.Context(), appdomain.AppMutation{
		ID:                     0,
		Name:                   req.Name,
		Status:                 req.Status,
		DisabledReason:         req.DisabledReason,
		RegisterStatus:         req.RegisterStatus,
		DisabledRegisterReason: req.DisabledRegisterReason,
		LoginStatus:            req.LoginStatus,
		DisabledLoginReason:    req.DisabledLoginReason,
		Settings:               req.Settings,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	// 创建应用后自动为创建者分配 app_admin 角色
	session, ok := adminAccessSession(c)
	if ok && session != nil && session.AdminID > 0 && !session.IsSuperAdmin {
		_ = h.admin.AutoAssignAppRole(c.Request.Context(), session.AdminID, item.ID, "app_admin")
	}
	response.Success(c, 200, "保存成功", item)
}

func (h *Handler) UpdateAdminApp(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminAppUpsertRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.saveAdminApp(c, appID, req)
}

func (h *Handler) AdminDeleteApp(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可删除应用")
		return
	}
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	if err := h.app.DeleteApp(c.Request.Context(), appID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "应用已删除", nil)
}

func (h *Handler) AdminAppEncryption(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	item, err := h.app.GetTransportEncryption(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) UpdateAdminAppEncryption(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req appdomain.TransportEncryptionUpdate
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.app.UpdateTransportEncryption(c.Request.Context(), appID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "加密配置已更新", item)
}

// AdminAppCommerceSettings 读取应用级交易设置（积分兑换率）。
func (h *Handler) AdminAppCommerceSettings(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	item, err := h.app.GetCommerceSettings(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

// UpdateAdminAppCommerceSettings 更新应用级交易设置。
func (h *Handler) UpdateAdminAppCommerceSettings(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req appdomain.CommerceSettings
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.app.UpdateCommerceSettings(c.Request.Context(), appID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "交易设置已更新", item)
}

// AdminAppLoginBaseline 查看某用户的登录绑定基线（绑定设备 / 网段 / 属地 / 上次换绑时间）。
func (h *Handler) AdminAppLoginBaseline(c *gin.Context) {
	appID, userID, ok := resolveAppScopedUserID(c, h.app)
	if !ok {
		return
	}
	baseline, err := h.auth.InspectLoginBaseline(c.Request.Context(), appID, userID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{
		"appid":    appID,
		"userId":   userID,
		"bound":    baseline != nil,
		"baseline": baseline,
	})
}

// ResetAdminAppLoginBaseline 重置某用户的登录绑定，下次登录重新建立基线。
func (h *Handler) ResetAdminAppLoginBaseline(c *gin.Context) {
	appID, userID, ok := resolveAppScopedUserID(c, h.app)
	if !ok {
		return
	}
	if err := h.auth.ResetLoginBaseline(c.Request.Context(), appID, userID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "登录绑定已重置", gin.H{"appid": appID, "userId": userID, "bound": false})
}

// resolveAppScopedUserID 解析「应用 + 路径上的 userid」二元组。
func resolveAppScopedUserID(c *gin.Context, appSvc *service.AppService) (int64, int64, bool) {
	appID, ok := resolveAppID(c, appSvc)
	if !ok {
		return 0, 0, false
	}
	userID, err := strconv.ParseInt(strings.TrimSpace(c.Param("userid")), 10, 64)
	if err != nil || userID <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "用户 ID 无效")
		return 0, 0, false
	}
	return appID, userID, true
}

func (h *Handler) UpdateAdminAppPolicy(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminAppPolicyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.app.UpdatePolicy(c.Request.Context(), appID, appdomain.Policy{
		LoginCheckDevice:     req.LoginCheckDevice,
		LoginCheckUser:       req.LoginCheckUser,
		LoginCheckIP:         req.LoginCheckIP,
		DeviceRebindInterval: req.DeviceRebindInterval,
		MultiDeviceLogin:     req.MultiDeviceLogin,
		MultiDeviceLimit:     req.MultiDeviceLimit,
		RegisterCheckIP:      req.RegisterCheckIP,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "更新成功", item)
}

func (h *Handler) AdminAppPasswordPolicy(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	item, err := h.app.GetPasswordPolicy(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取密码策略成功", item)
}

func (h *Handler) UpdateAdminAppPasswordPolicy(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminPasswordPolicyUpdateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.app.SetPasswordPolicy(c.Request.Context(), appID, req.Policy)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "密码策略设置成功", item)
}

func (h *Handler) TestAdminAppPasswordPolicy(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminPasswordPolicyTestRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.app.TestPasswordPolicy(c.Request.Context(), appID, req.Password)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "密码策略测试完成", item)
}

func (h *Handler) ResetAdminAppPasswordPolicy(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	item, err := h.app.ResetPasswordPolicy(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "密码策略已重置", item)
}

func (h *Handler) AdminAppSignInReward(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	item, err := h.app.GetSignInRewardPolicy(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取签到奖励策略成功", item)
}

func (h *Handler) UpdateAdminAppSignInReward(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminSignInRewardPolicyUpdateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.app.SetSignInRewardPolicy(c.Request.Context(), appID, req.Policy)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "签到奖励策略设置成功", item)
}

func (h *Handler) TestAdminAppSignInReward(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminSignInRewardTestRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.app.PreviewSignInReward(c.Request.Context(), appID, appdomain.SignInRewardPreviewInput{
		OccurredAt:      req.OccurredAt,
		ConsecutiveDays: req.ConsecutiveDays,
		TotalSignIns:    req.TotalSignIns,
		UserExperience:  req.UserExperience,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "签到奖励策略测试完成", item)
}

func (h *Handler) ResetAdminAppSignInReward(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	item, err := h.app.ResetSignInRewardPolicy(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "签到奖励策略已重置", item)
}

func (h *Handler) SignInRewardTemplates(c *gin.Context) {
	response.Success(c, 200, "获取签到奖励模板成功", gin.H{
		"templates": h.app.GetSignInRewardTemplates(),
		"usage": gin.H{
			"balanced":  "兼容当前默认行为，适合作为迁移基线",
			"growth":    "强调新用户启动和工作日活跃拉升",
			"retention": "强调长周期连签和里程碑奖励",
		},
	})
}

func (h *Handler) AdminAppSignInStats(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminAppSignInStatsQuery
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.app.GetAppSignInStats(c.Request.Context(), appID, req.Days)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取签到统计成功", item)
}

func (h *Handler) AdminAppSignInRecords(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminAppSignInRecordQuery
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}

	dateFrom, err := parseOptionalDateInLocation(req.DateFrom, time.Local)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "dateFrom 格式无效，要求 YYYY-MM-DD")
		return
	}
	dateTo, err := parseOptionalDateInLocation(req.DateTo, time.Local)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "dateTo 格式无效，要求 YYYY-MM-DD")
		return
	}
	if dateFrom != nil && dateTo != nil && dateFrom.After(*dateTo) {
		response.Error(c, http.StatusBadRequest, 40000, "dateFrom 不能晚于 dateTo")
		return
	}

	item, err := h.app.ListAppSignInRecords(c.Request.Context(), appID, appdomain.AppSignInRecordQuery{
		Keyword:  req.Keyword,
		Source:   req.Source,
		DateFrom: dateFrom,
		DateTo:   dateTo,
		Page:     req.Page,
		Limit:    req.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取签到明细成功", item)
}

func (h *Handler) GetAppPasswordPolicy(c *gin.Context) {
	var req PasswordPolicyAppIDRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.app.GetPasswordPolicy(c.Request.Context(), req.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取密码策略成功", item)
}

func (h *Handler) SetAppPasswordPolicy(c *gin.Context) {
	var req PasswordPolicySetRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.app.SetPasswordPolicy(c.Request.Context(), req.AppID, req.Policy)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "密码策略设置成功", item)
}

func (h *Handler) TestAppPasswordPolicy(c *gin.Context) {
	var req PasswordPolicyTestRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.app.TestPasswordPolicy(c.Request.Context(), req.AppID, req.Password)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "密码策略测试完成", item)
}

func (h *Handler) PasswordPolicyTemplates(c *gin.Context) {
	response.Success(c, 200, "获取密码策略模板成功", gin.H{
		"templates": h.app.GetPasswordPolicyTemplates(),
		"usage": gin.H{
			"basic":      "适合个人应用或对安全要求不高的场景",
			"standard":   "适合大多数商业应用",
			"strict":     "适合金融、医疗等高安全要求行业",
			"enterprise": "适合大型企业内部系统",
		},
	})
}

func (h *Handler) ResetAppPasswordPolicy(c *gin.Context) {
	var req PasswordPolicyAppIDRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.app.ResetPasswordPolicy(c.Request.Context(), req.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "密码策略已重置", item)
}

func (h *Handler) saveAdminApp(c *gin.Context, appID int64, req AdminAppUpsertRequest) {
	item, err := h.app.SaveApp(c.Request.Context(), appdomain.AppMutation{
		ID:                     appID,
		Name:                   req.Name,
		Status:                 req.Status,
		DisabledReason:         req.DisabledReason,
		RegisterStatus:         req.RegisterStatus,
		DisabledRegisterReason: req.DisabledRegisterReason,
		LoginStatus:            req.LoginStatus,
		DisabledLoginReason:    req.DisabledLoginReason,
		Settings:               req.Settings,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "保存成功", item)
}

func (h *Handler) AdminUserSettingsStats(c *gin.Context) {
	var query AdminSettingsStatsQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.user.GetAdminSettingsStats(c.Request.Context(), query.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) AdminUserSettings(c *gin.Context) {
	var query AdminUserSettingsQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.user.GetAdminUserSettings(c.Request.Context(), query.AppID, query.UserID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) AdminBatchInitializeUserSettings(c *gin.Context) {
	var req AdminBatchInitializeSettingsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.user.BatchInitializeSettingsAdmin(c.Request.Context(), req.AppID, req.BatchSize, req.Categories)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "批量初始化完成", result)
}

func (h *Handler) AdminInitializeUserSettings(c *gin.Context) {
	var req AdminInitializeUserSettingsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.user.InitializeUserSettingsAdmin(c.Request.Context(), req.AppID, req.UserID, req.Categories)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "用户设置初始化完成", result)
}

func (h *Handler) AdminCheckUserSettingsIntegrity(c *gin.Context) {
	var query AdminSettingsIntegrityQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.user.CheckAndRepairSettings(c.Request.Context(), query.AppID, query.AutoRepair)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "设置完整性检查完成", result)
}

func (h *Handler) AdminCleanupUserSettings(c *gin.Context) {
	var query AdminSettingsCleanupQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	dryRun := true
	if query.DryRun != nil {
		dryRun = *query.DryRun
	}
	result, err := h.user.CleanupInvalidSettingsAdmin(c.Request.Context(), query.AppID, dryRun)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "无效设置清理完成", result)
}

func (h *Handler) Profile(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	profile, err := h.user.GetProfile(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.attachUserProfileAvatar(c, session, profile)
	response.Success(c, 200, "获取成功", profile)
}

func (h *Handler) UpdateProfile(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req UpdateProfileRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.user.UpdateProfile(c.Request.Context(), session, userdomain.ProfileUpdate(req))
	if err != nil {
		h.writeError(c, err)
		return
	}
	if result != nil && result.Profile != nil {
		h.attachUserProfileAvatar(c, session, result.Profile)
	}
	response.Success(c, 200, "更新成功", result)
}

func (h *Handler) ConfirmProfileChange(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req ConfirmProfileChangeRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.user.ConfirmSensitiveProfileChange(c.Request.Context(), session, req.Field, req.Code)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if result != nil && result.Profile != nil {
		h.attachUserProfileAvatar(c, session, result.Profile)
	}
	response.Success(c, 200, "资料变更已生效", result)
}

func (h *Handler) Settings(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	settings, err := h.user.GetSettings(c.Request.Context(), session, c.Query("category"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", settings)
}

func (h *Handler) UpdateSettings(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req UpdateSettingsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	settings, err := h.user.UpdateSettings(c.Request.Context(), session, req.Category, req.Settings)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "更新成功", settings)
}

func (h *Handler) LegacyUserSettings(c *gin.Context) {
	h.Settings(c)
}

func (h *Handler) LegacyUpdateUserSettings(c *gin.Context) {
	h.UpdateSettings(c)
}

func (h *Handler) LegacyResetUserSettings(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req ResetSettingsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	settings, err := h.user.ResetSettings(c.Request.Context(), session, req.Category)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "重置成功", settings)
}

func (h *Handler) UserSettingCategories(c *gin.Context) {
	response.Success(c, 200, "获取成功", gin.H{
		"categories": h.user.ListSettingCategories(),
	})
}

func (h *Handler) LegacyAutoSignStatus(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	settings, err := h.user.GetSettings(c.Request.Context(), session, "autoSign")
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{
		"enabled":   settings.Settings["enabled"],
		"category":  settings.Category,
		"settings":  settings.Settings,
		"version":   settings.Version,
		"isActive":  settings.IsActive,
		"updatedAt": settings.UpdatedAt,
	})
}

func (h *Handler) LegacyAutoSignTestNotification(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if err := h.notifications.SendUserNotification(c.Request.Context(), session, "system", "自动签到测试通知", "自动签到通知链路正常，当前配置已可用。", "info", map[string]any{
		"scene": "auto_sign_test",
	}); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "测试通知发送成功", gin.H{"sent": true})
}

func (h *Handler) Security(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	status, err := h.user.GetSecurityStatus(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", status)
}

func (h *Handler) UserLoginAudits(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query UserLoginAuditQuery
	_ = bind(c, &query)
	result, err := h.user.ListLoginAudits(c.Request.Context(), session, userdomain.LoginAuditQuery{
		Status: query.Status,
		Page:   query.Page,
		Limit:  query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) ExportUserLoginAudits(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query UserLoginAuditQuery
	_ = bind(c, &query)
	items, err := h.user.ExportLoginAudits(c.Request.Context(), session, userdomain.LoginAuditExportQuery{
		Status: query.Status,
		Limit:  query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}

	filename := "user_login_audits_" + strconv.FormatInt(session.UserID, 10) + ".csv"
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	_ = writer.Write([]string{"id", "appid", "login_type", "provider", "token_jti", "login_ip", "device_id", "user_agent", "status", "created_at", "metadata"})
	for _, item := range items {
		metadata := ""
		if len(item.Metadata) > 0 {
			if encoded, err := json.Marshal(item.Metadata); err == nil {
				metadata = string(encoded)
			}
		}
		_ = writer.Write([]string{
			strconv.FormatInt(item.ID, 10),
			strconv.FormatInt(item.AppID, 10),
			item.LoginType,
			item.Provider,
			item.TokenJTI,
			item.LoginIP,
			item.DeviceID,
			item.UserAgent,
			item.Status,
			item.CreatedAt.UTC().Format(time.RFC3339),
			metadata,
		})
	}
}

func (h *Handler) UserSessionAudits(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query UserSessionAuditQuery
	_ = bind(c, &query)
	result, err := h.user.ListSessionAudits(c.Request.Context(), session, userdomain.SessionAuditQuery{
		EventType: query.EventType,
		Page:      query.Page,
		Limit:     query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) ExportUserSessionAudits(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query UserSessionAuditQuery
	_ = bind(c, &query)
	items, err := h.user.ExportSessionAudits(c.Request.Context(), session, userdomain.SessionAuditExportQuery{
		EventType: query.EventType,
		Limit:     query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}

	filename := "user_session_audits_" + strconv.FormatInt(session.UserID, 10) + ".csv"
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	_ = writer.Write([]string{"id", "appid", "token_jti", "event_type", "created_at", "metadata"})
	for _, item := range items {
		metadata := ""
		if len(item.Metadata) > 0 {
			if encoded, err := json.Marshal(item.Metadata); err == nil {
				metadata = string(encoded)
			}
		}
		_ = writer.Write([]string{
			strconv.FormatInt(item.ID, 10),
			strconv.FormatInt(item.AppID, 10),
			item.TokenJTI,
			item.EventType,
			item.CreatedAt.UTC().Format(time.RFC3339),
			metadata,
		})
	}
}

func (h *Handler) UserSessions(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	items, err := h.user.ListSessions(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) RevokeUserSession(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	result, err := h.user.RevokeSession(c.Request.Context(), session, c.Param("tokenHash"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "会话已撤销", result)
}

func (h *Handler) RevokeAllUserSessions(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req UserSessionRevokeAllRequest
	_ = bind(c, &req)
	result, err := h.user.RevokeAllSessions(c.Request.Context(), session, req.IncludeCurrent)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "会话已撤销", result)
}

func (h *Handler) SignInStatus(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	status, err := h.signin.GetStatus(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", status)
}

func (h *Handler) SignInHistory(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query PaginationQuery
	_ = bind(c, &query)
	result, err := h.signin.ListHistory(c.Request.Context(), session, normalizePage(query.Page), normalizeLimit(query.Limit))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) ExportUserSignInHistory(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query PaginationQuery
	_ = bind(c, &query)
	items, err := h.signin.ExportHistory(c.Request.Context(), session, userdomain.SignHistoryExportQuery{
		Limit: query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}

	filename := "user_signin_history_" + strconv.FormatInt(session.UserID, 10) + ".csv"
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	_ = writer.Write([]string{"id", "appid", "signed_at", "sign_date", "integral_reward", "experience_reward", "integral_before", "integral_after", "experience_before", "experience_after", "consecutive_days", "reward_multiplier", "bonus_type", "bonus_description", "sign_in_source", "device_info", "ip_address", "location", "created_at"})
	for _, item := range items {
		_ = writer.Write([]string{
			strconv.FormatInt(item.ID, 10),
			strconv.FormatInt(item.AppID, 10),
			item.SignedAt.UTC().Format(time.RFC3339),
			item.SignDate,
			strconv.FormatInt(item.IntegralReward, 10),
			strconv.FormatInt(item.ExperienceReward, 10),
			strconv.FormatInt(item.IntegralBefore, 10),
			strconv.FormatInt(item.IntegralAfter, 10),
			strconv.FormatInt(item.ExperienceBefore, 10),
			strconv.FormatInt(item.ExperienceAfter, 10),
			strconv.Itoa(item.ConsecutiveDays),
			strconv.FormatFloat(item.RewardMultiplier, 'f', -1, 64),
			item.BonusType,
			item.BonusDescription,
			item.SignInSource,
			item.DeviceInfo,
			item.IPAddress,
			item.Location,
			item.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
}

func (h *Handler) SignIn(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req SignInRequest
	_ = bind(c, &req)
	source := req.Source
	if source == "" {
		source = "manual"
	}
	location := strings.TrimSpace(req.Location)
	if location == "" {
		location = middleware.RequestLocationString(c)
	}
	result, err := h.signin.SignIn(c.Request.Context(), session, source, c.Request.UserAgent(), c.ClientIP(), location)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "签到成功", result)
}

func (h *Handler) PointsOverview(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	overview, err := h.points.GetOverview(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", overview)
}

func (h *Handler) PointsLevels(c *gin.Context) {
	levels, err := h.points.ListLevels(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", levels)
}

func (h *Handler) LegacyLevelConfig(c *gin.Context) {
	levels, err := h.points.ListLevels(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取等级配置成功", gin.H{
		"levels":     levels,
		"expRewards": []any{},
	})
}

func (h *Handler) MyLevel(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	level, err := h.points.GetMyLevel(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", level)
}

func (h *Handler) IntegralTransactions(c *gin.Context) {
	h.writeTransactions(c, func(session *authdomain.Session, page int, limit int) ([]pointdomain.Transaction, int64, error) {
		return h.points.ListIntegralTransactions(c.Request.Context(), session, page, limit)
	})
}

func (h *Handler) ExperienceTransactions(c *gin.Context) {
	h.writeTransactions(c, func(session *authdomain.Session, page int, limit int) ([]pointdomain.Transaction, int64, error) {
		return h.points.ListExperienceTransactions(c.Request.Context(), session, page, limit)
	})
}

func (h *Handler) PointsRankings(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query RankingQuery
	_ = bind(c, &query)
	rankings, err := h.points.GetRankings(c.Request.Context(), session, query.Type, normalizePage(query.Page), normalizeLimit(query.Limit))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", rankings)
}

func (h *Handler) LegacyMyLevel(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req LegacyRankingRequest
	_ = bind(c, &req)
	if req.AppID > 0 && req.AppID != session.AppID {
		response.Error(c, http.StatusForbidden, 40313, "应用不匹配")
		return
	}
	level, err := h.points.GetMyLevel(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取等级信息成功", gin.H{
		"levelInfo":  level.LevelInfo,
		"experience": level.UserInfo.Experience,
		"userInfo":   level.UserInfo,
	})
}

func (h *Handler) LegacyLevelRanking(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req LegacyRankingRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	rankings, err := h.points.GetLegacyRanking(c.Request.Context(), session, req.AppID, "level", normalizePage(req.Page), normalizeLimit(req.PageSize))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取等级排行榜成功", rankings)
}

func (h *Handler) LegacyDailyRank(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req LegacyRankingRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	rankingType, err := h.points.ResolveLegacyDailyRankingType(req.Type)
	if err != nil {
		h.writeError(c, err)
		return
	}
	rankings, err := h.points.GetLegacyRanking(c.Request.Context(), session, req.AppID, rankingType, normalizePage(req.Page), normalizeLimit(req.PageSize))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取签到排行榜成功", rankings)
}

func (h *Handler) LegacyIntegralRank(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req LegacyRankingRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	rankings, err := h.points.GetLegacyRanking(c.Request.Context(), session, req.AppID, "integral", normalizePage(req.Page), normalizeLimit(req.PageSize))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取积分排行榜成功", rankings)
}

func (h *Handler) LegacyDailySign(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req SignInRequest
	_ = bind(c, &req)
	source := req.Source
	if source == "" {
		source = "manual"
	}
	location := strings.TrimSpace(req.Location)
	if location == "" {
		location = middleware.RequestLocationString(c)
	}
	result, err := h.signin.SignIn(c.Request.Context(), session, source, c.Request.UserAgent(), c.ClientIP(), location)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "签到成功", result)
}

func (h *Handler) AppPointsStats(c *gin.Context) {
	var req AppPointsStatsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	stats, err := h.points.GetAppStatistics(c.Request.Context(), req.AppID, req.TimeRange)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取积分经验统计成功", stats)
}

func (h *Handler) AppAdjustIntegral(c *gin.Context) {
	var req AppAdjustIntegralRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	adminID, adminAccount := adminAccount(c)
	result, err := h.points.AdjustUserIntegral(c.Request.Context(), req.UserID, req.AppID, req.Amount, req.Reason, pointdomain.AdminAdjustOptions{
		AdminID:      adminID,
		AdminAccount: adminAccount,
		ClientIP:     c.ClientIP(),
		UserAgent:    c.Request.UserAgent(),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "用户积分调整成功", result)
}

func (h *Handler) AppAdjustExperience(c *gin.Context) {
	var req AppAdjustExperienceRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	adminID, adminAccount := adminAccount(c)
	result, err := h.points.AdjustUserExperience(c.Request.Context(), req.UserID, req.AppID, req.Amount, req.Reason, pointdomain.AdminAdjustOptions{
		AdminID:      adminID,
		AdminAccount: adminAccount,
		ClientIP:     c.ClientIP(),
		UserAgent:    c.Request.UserAgent(),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "用户经验值调整成功", result)
}

func (h *Handler) AppBatchAdjustIntegral(c *gin.Context) {
	var req AppBatchAdjustIntegralRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	adminID, adminAccount := adminAccount(c)
	result, err := h.points.BatchAdjustUserIntegral(c.Request.Context(), req.UserIDs, req.AppID, req.Amount, req.OperationType, req.Reason, pointdomain.AdminAdjustOptions{
		AdminID:      adminID,
		AdminAccount: adminAccount,
		ClientIP:     c.ClientIP(),
		UserAgent:    c.Request.UserAgent(),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "批量调整用户积分成功", result)
}

func (h *Handler) AdminVersionListCompat(c *gin.Context) {
	var req AdminAppVersionListRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.version.List(c.Request.Context(), req.AppID, appdomain.AppVersionListQuery{Page: normalizePage(req.Page), Limit: normalizeLimit(req.Limit), Status: req.Status, Platform: req.Platform, ChannelID: req.ChannelID})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) AdminVersionDetailCompat(c *gin.Context) {
	var req AdminAppVersionDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.version.Detail(c.Request.Context(), req.VersionID, req.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminVersionCreateCompat(c *gin.Context) {
	h.adminVersionSaveCompat(c, 0)
}

func (h *Handler) AdminVersionUpdateCompat(c *gin.Context) {
	var req AdminAppVersionSaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.version.Save(c.Request.Context(), appdomain.AppVersionMutation{
		ID:           req.VersionID,
		AppID:        req.AppID,
		ChannelID:    req.ChannelID,
		Version:      maybeString(req.Version),
		VersionCode:  maybeInt64(req.VersionCode),
		Description:  maybeString(req.Description),
		ReleaseNotes: maybeString(req.ReleaseNotes),
		DownloadURL:  maybeString(req.DownloadURL),
		FileSize:     maybeInt64(req.FileSize),
		FileHash:     maybeString(req.FileHash),
		ForceUpdate:  req.ForceUpdate,
		UpdateType:   maybeString(req.UpdateType),
		Platform:     maybeString(req.Platform),
		MinOSVersion: maybeString(req.MinOSVersion),
		Status:       maybeString(req.Status),
		Metadata:     req.Metadata,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "更新成功", item)
}

func (h *Handler) adminVersionSaveCompat(c *gin.Context, id int64) {
	var req AdminAppVersionSaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.version.Save(c.Request.Context(), appdomain.AppVersionMutation{
		ID:           id,
		AppID:        req.AppID,
		ChannelID:    req.ChannelID,
		Version:      maybeString(req.Version),
		VersionCode:  maybeInt64(req.VersionCode),
		Description:  maybeString(req.Description),
		ReleaseNotes: maybeString(req.ReleaseNotes),
		DownloadURL:  maybeString(req.DownloadURL),
		FileSize:     maybeInt64(req.FileSize),
		FileHash:     maybeString(req.FileHash),
		ForceUpdate:  req.ForceUpdate,
		UpdateType:   maybeString(req.UpdateType),
		Platform:     maybeString(req.Platform),
		MinOSVersion: maybeString(req.MinOSVersion),
		Status:       maybeString(req.Status),
		Metadata:     req.Metadata,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "创建成功", item)
}

func (h *Handler) AdminVersionDeleteCompat(c *gin.Context) {
	var req AdminAppVersionDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.version.Delete(c.Request.Context(), req.AppID, req.VersionID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", nil)
}

func (h *Handler) AdminVersionChannelListCompat(c *gin.Context) {
	var req RoleAppIDQuery
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, err := h.version.ListChannels(c.Request.Context(), req.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) AdminVersionChannelDetailCompat(c *gin.Context) {
	var req AdminVersionChannelDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.version.ChannelDetail(c.Request.Context(), req.ChannelID, req.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminVersionChannelCreateCompat(c *gin.Context) {
	h.adminVersionChannelSaveCompat(c, 0)
}

func (h *Handler) AdminVersionChannelUpdateCompat(c *gin.Context) {
	h.adminVersionChannelSaveCompat(c, -1)
}

func (h *Handler) adminVersionChannelSaveCompat(c *gin.Context, createFlag int64) {
	var req AdminVersionChannelSaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	channelID := req.ChannelID
	if createFlag == 0 {
		channelID = 0
	}
	item, err := h.version.SaveChannel(c.Request.Context(), appdomain.AppVersionChannelMutation{
		ID:             channelID,
		AppID:          req.AppID,
		Name:           maybeString(req.Name),
		Code:           maybeString(req.Code),
		Description:    maybeString(req.Description),
		IsDefault:      req.IsDefault,
		Status:         req.Status,
		Priority:       req.Priority,
		Color:          maybeString(req.Color),
		Level:          maybeString(req.Level),
		RolloutPct:     req.RolloutPct,
		Platforms:      req.Platforms,
		MinVersionCode: req.MinVersionCode,
		MaxVersionCode: req.MaxVersionCode,
		Rules:          req.Rules,
		TargetAudience: req.TargetAudience,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "创建成功"
	if channelID > 0 {
		message = "更新成功"
	}
	response.Success(c, 200, message, item)
}

func (h *Handler) AdminVersionChannelDeleteCompat(c *gin.Context) {
	var req AdminVersionChannelDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.version.DeleteChannel(c.Request.Context(), req.AppID, req.ChannelID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", nil)
}

func (h *Handler) AdminVersionChannelUsersCompat(c *gin.Context) {
	var req AdminVersionChannelUsersRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, total, err := h.version.ListChannelUsers(c.Request.Context(), req.AppID, req.ChannelID, normalizePage(req.Page), normalizeLimit(req.Limit))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"items": items, "page": normalizePage(req.Page), "limit": normalizeLimit(req.Limit), "total": total, "totalPages": calcPages(total, normalizeLimit(req.Limit))})
}

func (h *Handler) AdminVersionChannelAddUsersCompat(c *gin.Context) {
	var req AdminVersionChannelUsersRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	added, err := h.version.AddChannelUsers(c.Request.Context(), req.AppID, req.ChannelID, req.UserIDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "添加成功", gin.H{"added": added, "skipped": len(req.UserIDs) - int(added)})
}

func (h *Handler) AdminVersionChannelRemoveUsersCompat(c *gin.Context) {
	var req AdminVersionChannelUsersRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	removed, err := h.version.RemoveChannelUsers(c.Request.Context(), req.AppID, req.ChannelID, req.UserIDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "移除成功", gin.H{"removed": removed})
}

func (h *Handler) AdminVersionStatsCompat(c *gin.Context) {
	var req RoleAppIDQuery
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	stats, err := h.version.Stats(c.Request.Context(), req.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", stats)
}

func (h *Handler) AdminVersionPreviewMatchCompat(c *gin.Context) {
	var req AdminVersionPreviewMatchRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if req.ChannelID == 0 {
		response.Success(c, 200, "获取成功", gin.H{"matchedUsers": 0})
		return
	}
	_, total, err := h.version.ListChannelUsers(c.Request.Context(), req.AppID, req.ChannelID, 1, 1)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"matchedUsers": total, "targetAudience": req.TargetAudience})
}

func (h *Handler) AdminSiteAuditListCompat(c *gin.Context) {
	var req SiteListQuery
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.site.AdminList(c.Request.Context(), req.AppID, appdomain.SiteListQuery{Page: normalizePage(req.Page), Limit: normalizeLimit(pickPositive(req.Limit, req.PageSize)), Status: req.Status, Keyword: req.Keyword, SortBy: req.SortBy, SortOrder: req.SortOrder})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) AdminSiteAuditCompat(c *gin.Context) {
	var req AdminSiteAuditRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	adminID, _ := adminActor(c)
	item, err := h.site.AdminAudit(c.Request.Context(), req.SiteID, req.AppID, adminID, req.Status, req.Reason)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "审核成功", item)
}

func (h *Handler) AdminSiteBatchAuditCompat(c *gin.Context) {
	var req AdminSiteBatchAuditRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	success := 0
	failed := 0
	adminID, _ := adminActor(c)
	for _, id := range req.SiteIDs {
		if _, err := h.site.AdminAudit(c.Request.Context(), id, req.AppID, adminID, req.Status, req.Reason); err != nil {
			failed++
		} else {
			success++
		}
	}
	response.Success(c, 200, "批量审核完成", gin.H{"success": success, "failed": failed})
}

func (h *Handler) AdminSiteListCompat(c *gin.Context) { h.AdminSiteAuditListCompat(c) }

func (h *Handler) AdminSiteDetailCompat(c *gin.Context) {
	var req AdminSiteDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.site.AdminDetail(c.Request.Context(), req.AppID, req.ID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if item == nil {
		response.Error(c, http.StatusNotFound, 40420, "站点不存在")
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminSiteUpdateCompat(c *gin.Context) {
	var req SiteUpdateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.site.AdminUpdate(c.Request.Context(), req.AppID, appdomain.SiteMutation{ID: req.ID, AppID: req.AppID, Name: maybeString(req.Name), URL: maybeString(req.URL), Description: maybeString(req.Description), Type: maybeString(req.Type), Header: maybeString(req.Header), Category: maybeString(req.Category)})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "更新成功", item)
}

func (h *Handler) AdminSiteDeleteCompat(c *gin.Context) {
	var req AdminSiteDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.site.AdminDelete(c.Request.Context(), req.AppID, req.ID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", nil)
}

func (h *Handler) AdminSiteTogglePinCompat(c *gin.Context) {
	var req AdminSiteTogglePinRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.site.AdminTogglePinned(c.Request.Context(), req.AppID, req.ID, req.IsPinned)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "操作成功", item)
}

func (h *Handler) AdminSiteUserSitesCompat(c *gin.Context) {
	var req AdminSiteUserRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.site.AdminUserSites(c.Request.Context(), req.AppID, req.UserID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) AdminSiteAuditStatsCompat(c *gin.Context) {
	var req RoleAppIDQuery
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	stats, err := h.site.AdminAuditStats(c.Request.Context(), req.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", stats)
}

func (h *Handler) AdminRoleApplicationsCompat(c *gin.Context) {
	var req RoleApplicationsQuery
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, err := h.roleApp.AdminList(c.Request.Context(), req.AppID, userdomain.RoleApplicationListQuery{Page: normalizePage(req.Page), Limit: normalizeLimit(req.Limit), Status: req.Status, RequestedRole: req.RequestedRole, Priority: req.Priority, Keyword: req.Keyword, SortBy: req.SortBy, SortOrder: req.SortOrder})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) AdminRoleApplicationDetailCompat(c *gin.Context) {
	var req AdminRoleApplicationDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.roleApp.AdminDetail(c.Request.Context(), req.AppID, req.ID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminRoleApplicationReviewCompat(c *gin.Context) {
	var req AdminRoleApplicationReviewRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	adminID, adminName := adminActor(c)
	item, err := h.roleApp.Review(c.Request.Context(), req.AppID, req.ID, adminID, adminName, req.Action, req.ReviewReason)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "审核成功", item)
}

func (h *Handler) AdminRoleApplicationBatchReviewCompat(c *gin.Context) {
	var req AdminRoleApplicationBatchReviewRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	success := 0
	failed := 0
	adminID, adminName := adminActor(c)
	for _, id := range req.IDs {
		if _, err := h.roleApp.Review(c.Request.Context(), req.AppID, id, adminID, adminName, req.Action, req.ReviewReason); err != nil {
			failed++
		} else {
			success++
		}
	}
	response.Success(c, 200, "批量审核完成", gin.H{"success": success, "failed": failed})
}

func (h *Handler) AdminRoleApplicationStatisticsCompat(c *gin.Context) {
	var req RoleAppIDQuery
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	stats, err := h.roleApp.Statistics(c.Request.Context(), req.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", stats)
}

func (h *Handler) Notifications(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query NotificationQuery
	_ = bind(c, &query)
	items, err := h.notifications.List(c.Request.Context(), session, notificationdomain.UserListQuery{
		Status: query.Status,
		Type:   query.Type,
		Level:  query.Level,
		Page:   normalizePage(query.Page),
		Limit:  normalizeLimit(query.Limit),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) NotificationUnreadCount(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	count, err := h.notifications.UnreadCount(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"unread": count})
}

func (h *Handler) ReadNotification(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req NotificationReadRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.notifications.MarkRead(c.Request.Context(), session, req.NotificationID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已标记已读", gin.H{"notificationId": req.NotificationID})
}

func (h *Handler) ReadNotificationsBatch(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req NotificationReadBatchRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.notifications.MarkReadBatch(c.Request.Context(), session, req.IDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已批量标记已读", result)
}

func (h *Handler) ReadAllNotifications(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if err := h.notifications.MarkAllRead(c.Request.Context(), session); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已全部标记已读", gin.H{"readAll": true})
}

func (h *Handler) DeleteNotification(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	notificationID, err := pathInt64(c, "notificationId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的通知标识")
		return
	}
	result, err := h.notifications.Delete(c.Request.Context(), session, notificationID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", result)
}

func (h *Handler) ClearNotifications(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req NotificationClearRequest
	_ = bind(c, &req)
	result, err := h.notifications.ClearFiltered(c.Request.Context(), session, req.Status, req.Type, req.Level)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "清空成功", result)
}

func (h *Handler) writeTransactions(c *gin.Context, loader func(session *authdomain.Session, page int, limit int) ([]pointdomain.Transaction, int64, error)) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query PaginationQuery
	_ = bind(c, &query)
	page := normalizePage(query.Page)
	limit := normalizeLimit(query.Limit)
	items, total, err := loader(session, page, limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{
		"items":      items,
		"page":       page,
		"limit":      limit,
		"total":      total,
		"totalPages": calcTotalPages(total, limit),
	})
}

func authSession(c *gin.Context) (*authdomain.Session, bool) {
	sessionValue, ok := c.Get("auth.session")
	if !ok {
		return nil, false
	}
	session, _ := sessionValue.(*authdomain.Session)
	if session == nil {
		return nil, false
	}
	return session, true
}

func normalizePage(page int) int {
	if page < 1 {
		return 1
	}
	return page
}

func normalizeLimit(limit int) int {
	if limit <= 0 {
		return 20
	}
	if limit > 100 {
		return 100
	}
	return limit
}

func maybeString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}

func maybeInt64(value int64) *int64 {
	if value == 0 {
		return nil
	}
	return &value
}

func pickPositive(primary int, fallback int) int {
	if primary > 0 {
		return primary
	}
	return fallback
}

func calcPages(total int64, limit int) int {
	if limit <= 0 || total <= 0 {
		return 0
	}
	return int((total + int64(limit) - 1) / int64(limit))
}

func calcTotalPages(total int64, limit int) int {
	if limit <= 0 {
		return 1
	}
	pages := int((total + int64(limit) - 1) / int64(limit))
	if pages == 0 {
		return 1
	}
	return pages
}
