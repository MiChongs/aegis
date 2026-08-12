package httptransport

import (
	"aegis/internal/middleware"

	"github.com/gin-gonic/gin"
)

// 平台设置 /api/admin/system/*。
//
// 本文件只做路由注册，由 NewRouter 按原顺序调用；分组规则见 route_groups.go。

// registerPlatformBannerActiveRoute 单独注册平台 Banner 的激活列表。
// 它刻意不在 adminSystem 组里，理由见函数体内的注释。
func registerPlatformBannerActiveRoute(router *gin.Engine, h *Handler, deps RouterDeps) {
	var (
		adminService = deps.Admin
		auditService = deps.Audit
	)
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
}

// registerAdminSystemRoutes 注册平台设置（/api/admin/system/*）。
//
// 组织架构那一段抽成了 registerOrgRoutes：它有六十多条路由，且全部挂在
// /organizations/:orgId 之下，是这一组里唯一自成体系的子域。调用点留在
// 原来的位置 —— gin 的路由树对注册顺序敏感（静态段与参数段共存时会 panic），
// 挪动顺序等于在拆分里夹带一次行为变更。
func registerAdminSystemRoutes(router *gin.Engine, h *Handler, deps RouterDeps) {
	var (
		adminService      = deps.Admin
		appService        = deps.App
		auditService      = deps.Audit
		governanceService = deps.Governance
		memoryManager     = deps.MemoryManager
		databaseManager   = deps.DatabaseManager
		cl                = deps.CrashLog
	)
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
		// ── 授权策略管理面 ──
		//
		// 「不能灵活配置」的落点。写操作一律叠加 RequireSuperAdmin：
		// 一条 allow 策略就能给自己提权，把这个入口交给普通管理员
		// 等于让整套 RBAC 可以被它管辖的对象自行改写。
		adminSystem.GET("/authz/model", h.AdminAuthzModel)
		adminSystem.GET("/authz/policies", h.AdminAuthzPolicies)
		adminSystem.GET("/authz/policies/subject", h.AdminAuthzSubjectPolicies)
		adminSystem.POST("/authz/explain", h.AdminAuthzExplain)
		adminSystem.POST("/authz/roles/override", middleware.RequireSuperAdmin(), h.AdminAuthzSetRoleOverride)
		adminSystem.PUT("/authz/admins/:adminId/grants", middleware.RequireSuperAdmin(), h.AdminAuthzSetAdminGrants)
		adminSystem.POST("/authz/reload", middleware.RequireSuperAdmin(), h.AdminAuthzReload)
		registerOrgRoutes(adminSystem, h)

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
		// 黑白名单：与 /auth/register 同一种漏注册。handler、service、DTO、审计
		// 全都在（user_master_handlers.go 的「黑白名单」一节），只是路由没写，
		// 于是控制台「用户主数据」页的名单区永远是空的，新增和删除也静默失败。
		// 路径取 user-lists 而不是 handler 注释里写的 lists —— 前者是控制台
		// 已经在调的那个，改后端一处比改前端三处加一个 query key 更小。
		adminSystem.GET("/user-master/user-lists", h.AdminListUserListEntries)
		adminSystem.POST("/user-master/user-lists", h.AdminCreateUserListEntry)
		adminSystem.DELETE("/user-master/user-lists/:id", h.AdminDeleteUserListEntry)
		// 法律文本管理（限超管，权限在 handler 内二次校验）
		adminSystem.GET("/legal/documents", h.AdminListLegalDocuments)
		adminSystem.GET("/legal/documents/:docType/:locale", h.AdminGetLegalDocument)
		// 预览：把草稿按当前部署的值渲染一遍，供编辑器的「预览」页签使用。
		// 用 POST 是因为草稿正文放在请求体里 —— 一份两万字的条款塞不进 query。
		adminSystem.POST("/legal/documents/:docType/:locale/preview", h.AdminPreviewLegalDocument)
		adminSystem.PUT("/legal/documents/:docType/:locale", h.AdminSaveLegalDocument)
		adminSystem.DELETE("/legal/documents/:docType/:locale", h.AdminDeleteLegalDocument)

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
}
