package httptransport

import (
	"aegis/internal/middleware"

	"github.com/gin-gonic/gin"
)

// 旧管理端命名空间 /api/app/* 与 /api/admin/app/*。
//
// 本文件只做路由注册，由 NewRouter 按原顺序调用；分组规则见 route_groups.go。

// registerAppCompatRoutes 注册 /api/app/* 与 /api/admin/app/* 旧管理端命名空间。
func registerAppCompatRoutes(router *gin.Engine, h *Handler, deps RouterDeps) {
	var (
		adminService      = deps.Admin
		appService        = deps.App
		auditService      = deps.Audit
		governanceService = deps.Governance
	)
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
}

// registerAdminAppConfigRoutes 注册应用级的邮件 / 支付 / 存储配置管理面。
func registerAdminAppConfigRoutes(router *gin.Engine, h *Handler, deps RouterDeps) {
	var (
		adminService      = deps.Admin
		appService        = deps.App
		auditService      = deps.Audit
		governanceService = deps.Governance
	)
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
		emailAdmin.POST("/stats", h.AdminEmailDeliveryStats)
		// 服务商目录：静态自述，两个作用域共用同一个 handler
		emailAdmin.POST("/providers", h.EmailProviderCatalog)
		// 当前生效的通道（含「继承自平台共享通道」的判定）
		emailAdmin.POST("/channel", h.AdminEmailChannel)
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
}

// registerWorkflowCompatRoutes 注册工作流管理面。
func registerWorkflowCompatRoutes(router *gin.Engine, h *Handler, deps RouterDeps) {
	var (
		adminService      = deps.Admin
		appService        = deps.App
		auditService      = deps.Audit
		governanceService = deps.Governance
	)
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
}
