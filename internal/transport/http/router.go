package httptransport

import (
	appdomain "aegis/internal/domain/app"
	authdomain "aegis/internal/domain/auth"
	notificationdomain "aegis/internal/domain/notification"
	pointdomain "aegis/internal/domain/points"
	userdomain "aegis/internal/domain/user"
	"aegis/internal/middleware"
	"aegis/internal/service"
	"aegis/pkg/response"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	auth          *service.AuthService
	admin         *service.AdminService
	user          *service.UserService
	signin        *service.SignInService
	points        *service.PointsService
	notifications *service.NotificationService
	app           *service.AppService
	site          *service.SiteService
	version       *service.VersionService
	roleApp       *service.RoleApplicationService
	email         *service.EmailService
	payment       *service.PaymentService
	workflow      *service.WorkflowService
	storage       *service.StorageService
	realtime      *service.RealtimeService
}

func NewHandler(auth *service.AuthService, admin *service.AdminService, user *service.UserService, signin *service.SignInService, points *service.PointsService, notifications *service.NotificationService, app *service.AppService, site *service.SiteService, version *service.VersionService, roleApp *service.RoleApplicationService, email *service.EmailService, payment *service.PaymentService, workflow *service.WorkflowService, storage *service.StorageService, realtime *service.RealtimeService) *Handler {
	return &Handler{auth: auth, admin: admin, user: user, signin: signin, points: points, notifications: notifications, app: app, site: site, version: version, roleApp: roleApp, email: email, payment: payment, workflow: workflow, storage: storage, realtime: realtime}
}

func NewRouter(authService *service.AuthService, adminService *service.AdminService, userService *service.UserService, signInService *service.SignInService, pointsService *service.PointsService, notificationService *service.NotificationService, appService *service.AppService, siteService *service.SiteService, versionService *service.VersionService, roleApplicationService *service.RoleApplicationService, emailService *service.EmailService, paymentService *service.PaymentService, workflowService *service.WorkflowService, storageService *service.StorageService, firewall *middleware.Firewall, locationService *service.LocationService, realtimeService *service.RealtimeService) *gin.Engine {
	router := gin.New()
	router.HandleMethodNotAllowed = true
	router.Use(middleware.RequestID(), middleware.Recovery(), gin.Logger(), firewall.Handler(), middleware.AppEncryption(appService), middleware.Location(locationService))
	router.NoRoute(func(c *gin.Context) {
		response.Error(c, http.StatusNotFound, 40400, "请求的页面不存在")
	})
	router.NoMethod(func(c *gin.Context) {
		response.Error(c, http.StatusNotImplemented, 50100, "服务能力暂未开放")
	})

	h := NewHandler(authService, adminService, userService, signInService, pointsService, notificationService, appService, siteService, versionService, roleApplicationService, emailService, paymentService, workflowService, storageService, realtimeService)
	router.GET("/healthz", h.Healthz)
	router.GET("/readyz", h.Readyz)
	router.GET("/api/app/public", h.AppPublic)
	router.GET("/api/ws", h.WebSocket)

	adminAuth := router.Group("/api/admin/auth")
	{
		adminAuth.POST("/login", h.AdminLogin)
		adminAuth.GET("/me", middleware.AdminAuth(adminService), h.AdminMe)
		adminAuth.POST("/logout", middleware.AdminAuth(adminService), h.AdminLogout)
	}

	appCompat := router.Group("/api/app/password-policy")
	appCompat.Use(middleware.AdminAccess(adminService))
	{
		appCompat.POST("/get", h.GetAppPasswordPolicy)
		appCompat.POST("/set", h.SetAppPasswordPolicy)
		appCompat.POST("/test", h.TestAppPasswordPolicy)
		appCompat.GET("/templates", h.PasswordPolicyTemplates)
		appCompat.POST("/reset", h.ResetAppPasswordPolicy)
	}

	appCompatPoints := router.Group("/api/app/points")
	appCompatPoints.Use(middleware.AdminAccess(adminService))
	{
		appCompatPoints.POST("/stats", h.AppPointsStats)
		appCompatPoints.POST("/adjust-integral", h.AppAdjustIntegral)
		appCompatPoints.POST("/adjust-experience", h.AppAdjustExperience)
		appCompatPoints.POST("/batch-adjust", h.AppBatchAdjustIntegral)
	}

	appCompatVersion := router.Group("/api/admin/app/version")
	appCompatVersion.Use(middleware.AdminAccess(adminService))
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
	appCompatSite.Use(middleware.AdminAccess(adminService))
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
	appCompatRole.Use(middleware.AdminAccess(adminService))
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
		auth.POST("/oauth2/auth-url", h.OAuthAuthURL)
		auth.GET("/oauth2/callback", h.OAuthCallback)
		auth.POST("/oauth2/mobile-login", h.OAuthMobileLogin)
		auth.POST("/refresh", h.Refresh)
		auth.POST("/logout", middleware.Auth(authService), h.Logout)
		auth.POST("/password/verify", middleware.Auth(authService), h.VerifyPassword)
		auth.POST("/password/change", middleware.Auth(authService), h.ChangePassword)
	}

	admin := router.Group("/api/admin")
	admin.Use(middleware.AdminAccess(adminService))
	{
		admin.GET("/apps", h.AdminApps)
		admin.GET("/apps/:appid", h.AdminApp)
		admin.GET("/apps/:appid/policy", h.AdminAppPolicy)
		admin.PUT("/apps/:appid/policy", h.UpdateAdminAppPolicy)
		admin.GET("/apps/:appid/password-policy", h.AdminAppPasswordPolicy)
		admin.PUT("/apps/:appid/password-policy", h.UpdateAdminAppPasswordPolicy)
		admin.POST("/apps/:appid/password-policy/test", h.TestAdminAppPasswordPolicy)
		admin.POST("/apps/:appid/password-policy/reset", h.ResetAdminAppPasswordPolicy)
		admin.GET("/apps/password-policy/templates", h.PasswordPolicyTemplates)
		admin.GET("/apps/:appid/stats", h.AdminAppStats)
		admin.GET("/apps/:appid/stats/user-trend", h.AdminAppUserTrend)
		admin.GET("/apps/:appid/stats/regions", h.AdminAppRegionStats)
		admin.GET("/apps/:appid/stats/auth-sources", h.AdminAppAuthSources)
		admin.GET("/apps/:appid/audits/login", h.AdminAppLoginAudits)
		admin.GET("/apps/:appid/audits/login/export", h.ExportAdminAppLoginAudits)
		admin.GET("/apps/:appid/audits/sessions", h.AdminAppSessionAudits)
		admin.GET("/apps/:appid/audits/sessions/export", h.ExportAdminAppSessionAudits)
		admin.GET("/apps/:appid/notifications", h.AdminAppNotifications)
		admin.GET("/apps/:appid/notifications/export", h.ExportAdminAppNotifications)
		admin.DELETE("/apps/:appid/notifications", h.DeleteAdminAppNotifications)
		admin.POST("/apps/:appid/notifications/delete-by-filter", h.DeleteAdminAppNotificationsByFilter)
		admin.POST("/apps/:appid/notifications/bulk", h.AdminBulkNotifyUsers)
		admin.GET("/apps/:appid/users", h.AdminAppUsers)
		admin.GET("/apps/:appid/users/export", h.ExportAdminAppUsers)
		admin.PUT("/apps/:appid/users/status/batch", h.BatchUpdateAdminAppUserStatus)
		admin.GET("/apps/:appid/users/:userId", h.AdminAppUser)
		admin.PUT("/apps/:appid/users/:userId/status", h.UpdateAdminAppUserStatus)
		admin.POST("/apps", h.CreateAdminApp)
		admin.PUT("/apps/:appid", h.UpdateAdminApp)
		admin.GET("/apps/:appid/banners", h.AdminBanners)
		admin.GET("/apps/:appid/banners/export", h.ExportAdminBanners)
		admin.POST("/apps/:appid/banners", h.CreateAdminBanner)
		admin.DELETE("/apps/:appid/banners", h.DeleteAdminBanners)
		admin.PUT("/apps/:appid/banners/:bannerId", h.UpdateAdminBanner)
		admin.DELETE("/apps/:appid/banners/:bannerId", h.DeleteAdminBanner)
		admin.GET("/apps/:appid/notices", h.AdminNotices)
		admin.GET("/apps/:appid/notices/export", h.ExportAdminNotices)
		admin.POST("/apps/:appid/notices", h.CreateAdminNotice)
		admin.DELETE("/apps/:appid/notices", h.DeleteAdminNotices)
		admin.PUT("/apps/:appid/notices/:noticeId", h.UpdateAdminNotice)
		admin.DELETE("/apps/:appid/notices/:noticeId", h.DeleteAdminNotice)
		admin.GET("/user-settings/stats", h.AdminUserSettingsStats)
		admin.GET("/user-settings/user", h.AdminUserSettings)
		admin.POST("/user-settings/batch-initialize", h.AdminBatchInitializeUserSettings)
		admin.POST("/user-settings/initialize-user", h.AdminInitializeUserSettings)
		admin.GET("/user-settings/check-integrity", h.AdminCheckUserSettingsIntegrity)
		admin.DELETE("/user-settings/cleanup", h.AdminCleanupUserSettings)
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
		user.GET("/settings", h.Settings)
		user.PUT("/settings", h.UpdateSettings)
		user.POST("/level/info", h.LegacyMyLevel)
		user.POST("/level/ranking", h.LegacyLevelRanking)
		user.POST("/dailyRank", h.LegacyDailyRank)
		user.POST("/integralRank", h.LegacyIntegralRank)
		user.POST("/settings/reset", h.LegacyResetUserSettings)
		user.GET("/security", h.Security)
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
	}

	emailAdmin := router.Group("/api/admin/app/email-config")
	emailAdmin.Use(middleware.AdminAccess(adminService))
	{
		emailAdmin.POST("/list", h.AdminEmailConfigList)
		emailAdmin.POST("/detail", h.AdminEmailConfigDetail)
		emailAdmin.POST("/create", h.AdminEmailConfigCreate)
		emailAdmin.POST("/update", h.AdminEmailConfigUpdate)
		emailAdmin.POST("/delete", h.AdminEmailConfigDelete)
		emailAdmin.POST("/test", h.AdminEmailConfigTest)
	}

	payCompat := router.Group("/api/admin/app/payment-config")
	payCompat.Use(middleware.AdminAccess(adminService))
	{
		payCompat.POST("/list", h.AdminPaymentConfigList)
		payCompat.POST("/detail", h.AdminPaymentConfigDetail)
		payCompat.POST("/create", h.AdminPaymentConfigCreate)
		payCompat.POST("/update", h.AdminPaymentConfigUpdate)
		payCompat.POST("/delete", h.AdminPaymentConfigDelete)
		payCompat.POST("/test", h.AdminPaymentConfigTest)
		payCompat.POST("/epay/init", h.AdminPaymentEpayInit)
	}

	appStorageAdmin := router.Group("/api/admin/app/storage-config")
	appStorageAdmin.Use(middleware.AdminAccess(adminService))
	{
		appStorageAdmin.POST("/list", h.AdminAppStorageConfigList)
		appStorageAdmin.POST("/detail", h.AdminAppStorageConfigDetail)
		appStorageAdmin.POST("/create", h.AdminAppStorageConfigCreate)
		appStorageAdmin.POST("/update", h.AdminAppStorageConfigUpdate)
		appStorageAdmin.POST("/delete", h.AdminAppStorageConfigDelete)
		appStorageAdmin.POST("/test", h.AdminAppStorageConfigTest)
	}

	globalStorageAdmin := router.Group("/api/admin/platform/storage-config")
	globalStorageAdmin.Use(middleware.AdminAccess(adminService))
	{
		globalStorageAdmin.POST("/list", h.AdminGlobalStorageConfigList)
		globalStorageAdmin.POST("/detail", h.AdminGlobalStorageConfigDetail)
		globalStorageAdmin.POST("/create", h.AdminGlobalStorageConfigCreate)
		globalStorageAdmin.POST("/update", h.AdminGlobalStorageConfigUpdate)
		globalStorageAdmin.POST("/delete", h.AdminGlobalStorageConfigDelete)
		globalStorageAdmin.POST("/test", h.AdminGlobalStorageConfigTest)
	}

	pay := router.Group("/api/pay")
	pay.Use(middleware.Auth(authService))
	{
		pay.POST("/orders/create", h.CreatePaymentOrder)
		pay.GET("/orders/:orderNo", h.PaymentOrderDetail)
		pay.GET("/epay/query/:orderNo", h.QueryEpayOrder)
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
	}

	publicStorage := router.Group("/api/storage")
	{
		publicStorage.GET("/proxy/:ticket", h.StorageProxyDownload)
	}

	workflowCompat := router.Group("/api/app/workflow")
	workflowCompat.Use(middleware.AdminAccess(adminService))
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
	adminSystem.Use(middleware.AdminAccess(adminService))
	{
		adminSystem.GET("/roles", h.AdminRoleCatalog)
		adminSystem.GET("/admins", h.AdminListAccounts)
		adminSystem.POST("/admins", h.AdminCreateAccount)
		adminSystem.PUT("/admins/:adminId/status", h.AdminUpdateAccountStatus)
		adminSystem.PUT("/admins/:adminId/access", h.AdminUpdateAccountAccess)
		adminSystem.GET("/online/stats", h.AdminOnlineStats)
		adminSystem.GET("/online/apps/:appid", h.AdminAppOnlineStats)
		adminSystem.GET("/online/apps/:appid/users", h.AdminAppOnlineUsers)
	}

	return router
}

func (h *Handler) Healthz(c *gin.Context) {
	response.Success(c, 200, "ok", gin.H{"status": "healthy"})
}

func (h *Handler) Readyz(c *gin.Context) {
	response.Success(c, 200, "ok", gin.H{"status": "ready"})
}

func (h *Handler) PasswordLogin(c *gin.Context) {
	var req PasswordLoginRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.auth.PasswordLogin(c.Request.Context(), req.AppID, req.Account, req.Password, req.MarkCode, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "登录成功", result)
}

func (h *Handler) PasswordRegister(c *gin.Context) {
	var req PasswordRegisterRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.auth.RegisterWithPassword(c.Request.Context(), req.AppID, req.Account, req.Password, req.Nickname, req.MarkCode, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "注册成功", result)
}

func (h *Handler) OAuthAuthURL(c *gin.Context) {
	var req OAuthAuthURLRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	url, err := h.auth.BuildOAuthAuthURL(c.Request.Context(), req.Provider, req.AppID, req.MarkCode)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取授权地址成功", gin.H{"url": url})
}

func (h *Handler) OAuthCallback(c *gin.Context) {
	provider := c.Query("provider")
	code := c.Query("code")
	state := c.Query("state")
	result, err := h.auth.HandleOAuthCallback(c.Request.Context(), provider, code, state, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "OAuth2 登录成功", result)
}

func (h *Handler) OAuthMobileLogin(c *gin.Context) {
	var req OAuthMobileLoginRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
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
	result, err := h.auth.MobileOAuthLogin(c.Request.Context(), req.AppID, req.Provider, profile, req.MarkCode, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "OAuth2 登录成功", result)
}

func (h *Handler) Refresh(c *gin.Context) {
	var req RefreshRequest
	_ = bind(c, &req)
	token := req.Token
	if token == "" {
		token = middlewareBearer(c.GetHeader("Authorization"))
	}
	result, err := h.auth.Refresh(c.Request.Context(), token, req.MarkCode, c.ClientIP(), c.Request.UserAgent())
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
	response.Success(c, 200, "获取成功", gin.H{
		"id":             item.ID,
		"name":           item.Name,
		"status":         item.Status,
		"registerStatus": item.RegisterStatus,
		"loginStatus":    item.LoginStatus,
		"policy":         h.app.ResolvePolicy(item),
		"settings":       h.app.PublicSettings(item),
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
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) AdminApp(c *gin.Context) {
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
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
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
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
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
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
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
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
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
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
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
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
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
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
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
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
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
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
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
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
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
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
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
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
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
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
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
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
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
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
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
		return
	}
	if _, err := h.app.GetApp(c.Request.Context(), appID); err != nil {
		h.writeError(c, err)
		return
	}
	var query AdminUserListQuery
	_ = bind(c, &query)
	items, err := h.user.ListAdminUsers(c.Request.Context(), appID, userdomain.AdminUserQuery{
		Keyword: query.Keyword,
		Enabled: query.Enabled,
		Page:    query.Page,
		Limit:   query.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) ExportAdminAppUsers(c *gin.Context) {
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
		return
	}
	if _, err := h.app.GetApp(c.Request.Context(), appID); err != nil {
		h.writeError(c, err)
		return
	}
	var query AdminUserListQuery
	_ = bind(c, &query)
	items, err := h.user.ExportAdminUsers(c.Request.Context(), appID, userdomain.AdminUserQuery{
		Keyword: query.Keyword,
		Enabled: query.Enabled,
		Limit:   query.Limit,
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

	_ = writer.Write([]string{"id", "appid", "account", "nickname", "email", "enabled", "integral", "experience", "register_ip", "register_time", "register_province", "register_city", "vip_expire_at"})
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
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
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
	item, err := h.user.GetAdminUser(c.Request.Context(), appID, userID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) BatchUpdateAdminAppUserStatus(c *gin.Context) {
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
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
	item, err := h.user.BatchUpdateAdminUserStatus(c.Request.Context(), appID, userdomain.AdminUserBatchStatusMutation{
		UserIDs: req.UserIDs,
		AdminUserStatusMutation: userdomain.AdminUserStatusMutation{
			Enabled:              req.Enabled,
			DisabledEndTime:      req.DisabledEndTime,
			ClearDisabledEndTime: req.ClearDisabledEndTime,
			DisabledReason:       req.DisabledReason,
		},
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "批量更新成功", item)
}

func (h *Handler) UpdateAdminAppUserStatus(c *gin.Context) {
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
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
	item, err := h.user.UpdateAdminUserStatus(c.Request.Context(), appID, userID, userdomain.AdminUserStatusMutation{
		Enabled:              req.Enabled,
		DisabledEndTime:      req.DisabledEndTime,
		ClearDisabledEndTime: req.ClearDisabledEndTime,
		DisabledReason:       req.DisabledReason,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "更新成功", item)
}

func (h *Handler) CreateAdminApp(c *gin.Context) {
	var req struct {
		AppID int64 `json:"appid" binding:"required"`
		AdminAppUpsertRequest
	}
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.saveAdminApp(c, req.AppID, req.AdminAppUpsertRequest)
}

func (h *Handler) UpdateAdminApp(c *gin.Context) {
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
		return
	}
	var req AdminAppUpsertRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.saveAdminApp(c, appID, req)
}

func (h *Handler) UpdateAdminAppPolicy(c *gin.Context) {
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
		return
	}
	var req AdminAppPolicyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.app.UpdatePolicy(c.Request.Context(), appID, appdomain.Policy{
		LoginCheckDevice:        req.LoginCheckDevice,
		LoginCheckUser:          req.LoginCheckUser,
		LoginCheckIP:            req.LoginCheckIP,
		LoginCheckDeviceTimeout: req.LoginCheckDeviceTimeout,
		MultiDeviceLogin:        req.MultiDeviceLogin,
		MultiDeviceLimit:        req.MultiDeviceLimit,
		RegisterCaptcha:         req.RegisterCaptcha,
		RegisterCaptchaTimeout:  req.RegisterCaptchaTimeout,
		RegisterCheckIP:         req.RegisterCheckIP,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "更新成功", item)
}

func (h *Handler) AdminAppPasswordPolicy(c *gin.Context) {
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
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
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
		return
	}
	var req struct {
		Policy appdomain.PasswordPolicy `json:"policy" binding:"required"`
	}
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
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
		return
	}
	var req struct {
		Password string `json:"password" binding:"required"`
	}
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
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
		return
	}
	item, err := h.app.ResetPasswordPolicy(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "密码策略已重置", item)
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
		AppKey:                 req.AppKey,
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

func (h *Handler) AdminBanners(c *gin.Context) {
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
		return
	}
	items, err := h.app.ListBannersForAdmin(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) ExportAdminBanners(c *gin.Context) {
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
		return
	}
	items, err := h.app.ListBannersForAdmin(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}

	filename := "app_banners_" + strconv.FormatInt(appID, 10) + ".csv"
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	_ = writer.Write([]string{"id", "header", "title", "content", "url", "type", "position", "status", "start_time", "end_time", "view_count", "click_count", "created_at", "updated_at"})
	for _, item := range items {
		startTime := ""
		if item.StartTime != nil {
			startTime = item.StartTime.UTC().Format(time.RFC3339)
		}
		endTime := ""
		if item.EndTime != nil {
			endTime = item.EndTime.UTC().Format(time.RFC3339)
		}
		_ = writer.Write([]string{
			strconv.FormatInt(item.ID, 10),
			item.Header,
			item.Title,
			item.Content,
			item.URL,
			item.Type,
			strconv.Itoa(item.Position),
			strconv.FormatBool(item.Status),
			startTime,
			endTime,
			strconv.FormatInt(item.ViewCount, 10),
			strconv.FormatInt(item.ClickCount, 10),
			item.CreatedAt.UTC().Format(time.RFC3339),
			item.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
}

func (h *Handler) CreateAdminBanner(c *gin.Context) {
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
		return
	}
	var req AdminBannerUpsertRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.saveAdminBanner(c, appID, 0, req)
}

func (h *Handler) UpdateAdminBanner(c *gin.Context) {
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
		return
	}
	bannerID, err := pathInt64(c, "bannerId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的 Banner 标识")
		return
	}
	var req AdminBannerUpsertRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.saveAdminBanner(c, appID, bannerID, req)
}

func (h *Handler) saveAdminBanner(c *gin.Context, appID int64, bannerID int64, req AdminBannerUpsertRequest) {
	item, err := h.app.SaveBanner(c.Request.Context(), appID, appdomain.BannerMutation{
		ID:        bannerID,
		Header:    req.Header,
		Title:     req.Title,
		Content:   req.Content,
		URL:       req.URL,
		Type:      req.Type,
		Position:  req.Position,
		Status:    req.Status,
		StartTime: req.StartTime,
		EndTime:   req.EndTime,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "保存成功", item)
}

func (h *Handler) DeleteAdminBanner(c *gin.Context) {
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
		return
	}
	bannerID, err := pathInt64(c, "bannerId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的 Banner 标识")
		return
	}
	if err := h.app.DeleteBanner(c.Request.Context(), appID, bannerID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", gin.H{"id": bannerID})
}

func (h *Handler) DeleteAdminBanners(c *gin.Context) {
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
		return
	}
	var req AdminBatchIDsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	deleted, ids, err := h.app.DeleteBanners(c.Request.Context(), appID, req.IDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "批量删除成功", gin.H{"deleted": deleted, "ids": ids})
}

func (h *Handler) AdminNotices(c *gin.Context) {
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
		return
	}
	items, err := h.app.ListNoticesForAdmin(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) ExportAdminNotices(c *gin.Context) {
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
		return
	}
	items, err := h.app.ListNoticesForAdmin(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}

	filename := "app_notices_" + strconv.FormatInt(appID, 10) + ".csv"
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	_ = writer.Write([]string{"id", "title", "content", "created_at", "updated_at"})
	for _, item := range items {
		_ = writer.Write([]string{
			strconv.FormatInt(item.ID, 10),
			item.Title,
			item.Content,
			item.CreatedAt.UTC().Format(time.RFC3339),
			item.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
}

func (h *Handler) CreateAdminNotice(c *gin.Context) {
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
		return
	}
	var req AdminNoticeUpsertRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.saveAdminNotice(c, appID, 0, req)
}

func (h *Handler) UpdateAdminNotice(c *gin.Context) {
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
		return
	}
	noticeID, err := pathInt64(c, "noticeId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的公告标识")
		return
	}
	var req AdminNoticeUpsertRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.saveAdminNotice(c, appID, noticeID, req)
}

func (h *Handler) saveAdminNotice(c *gin.Context, appID int64, noticeID int64, req AdminNoticeUpsertRequest) {
	item, err := h.app.SaveNotice(c.Request.Context(), appID, appdomain.NoticeMutation{
		ID:      noticeID,
		Title:   req.Title,
		Content: req.Content,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "保存成功", item)
}

func (h *Handler) DeleteAdminNotice(c *gin.Context) {
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
		return
	}
	noticeID, err := pathInt64(c, "noticeId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的公告标识")
		return
	}
	if err := h.app.DeleteNotice(c.Request.Context(), appID, noticeID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", gin.H{"id": noticeID})
}

func (h *Handler) DeleteAdminNotices(c *gin.Context) {
	appID, err := pathInt64(c, "appid")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用标识")
		return
	}
	var req AdminBatchIDsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	deleted, ids, err := h.app.DeleteNotices(c.Request.Context(), appID, req.IDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "批量删除成功", gin.H{"deleted": deleted, "ids": ids})
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
	profile, err := h.user.UpdateProfile(c.Request.Context(), session, userdomain.ProfileUpdate(req))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "更新成功", profile)
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
