package httptransport

import (
	"aegis/internal/middleware"

	"github.com/gin-gonic/gin"
)

// 平台治理与平台级配置（恒全局作用域）。
//
// 本文件只做路由注册，由 NewRouter 按原顺序调用；分组规则见 route_groups.go。

// registerPlatformGovernanceRoutes 注册平台治理。
func registerPlatformGovernanceRoutes(router *gin.Engine, h *Handler, deps RouterDeps) {
	var (
		adminService      = deps.Admin
		appService        = deps.App
		auditService      = deps.Audit
		governanceService = deps.Governance
	)
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
}

// registerPlatformStorageRoutes 注册平台级存储配置。
func registerPlatformStorageRoutes(router *gin.Engine, h *Handler, deps RouterDeps) {
	var (
		adminService      = deps.Admin
		appService        = deps.App
		auditService      = deps.Audit
		governanceService = deps.Governance
	)
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
}
