package auditcatalog

// 平台与系统：控制台总览、平台治理、系统设置、数据库运维、插件、崩溃日志与审计日志本身。
func init() {
	Register("platform",
		View("GET /api/admin/dashboard", "查看控制台总览", ""),
		View("GET /api/admin/system/runtime", "查看运行状态", ""),

		// 平台治理
		View("GET /api/admin/platform/overview", "查看平台总览", ""),
		View("GET /api/admin/platform/apps", "查看平台应用概况", ""),
		View("GET /api/admin/platform/catalog", "查看治理措施目录", ""),
		View("GET /api/admin/platform/apps/:appkey/governance", "查看应用治理状态", "应用"),
		Write("POST /api/admin/platform/apps/:appkey/governance", "执行应用治理措施", "应用", SeverityCritical),
		Write("POST /api/admin/platform/apps/batch-governance", "批量执行应用治理措施", "应用", SeverityCritical),
		Write("POST /api/admin/platform/apps/:appkey/revoke-sessions", "强制下线应用全部会话", "应用", SeverityCritical),
		View("GET /api/admin/platform/governance/actions", "查看治理记录", ""),
		View("GET /api/admin/platform/governance/appeals", "查看治理申诉", ""),
		Write("POST /api/admin/platform/governance/appeals/:appealId/review", "审核治理申诉", "治理申诉", SeverityHigh),

		// 系统设置
		View("GET /api/admin/system/settings", "查看平台设置", ""),
		Write("PUT /api/admin/system/settings", "修改平台设置", "", SeverityCritical),

		// 数据库运维
		View("GET /api/admin/system/database/snapshot", "查看数据库状态", ""),
		View("GET /api/admin/system/database/history", "查看数据库指标历史", ""),
		View("GET /api/admin/system/database/sessions", "查看数据库会话", ""),
		View("GET /api/admin/system/database/slow-queries", "查看慢查询", ""),
		View("GET /api/admin/system/database/leak", "查看连接泄漏检测", ""),
		View("GET /api/admin/system/database/maintenance", "查看数据库维护建议", ""),
		Write("POST /api/admin/system/database/refresh", "刷新数据库指标", "", SeverityLow),
		Write("POST /api/admin/system/database/warmup", "预热数据库缓存", "", SeverityHigh),
		Write("POST /api/admin/system/database/sessions/:pid/cancel", "取消数据库查询", "数据库会话", SeverityHigh),
		Write("POST /api/admin/system/database/sessions/:pid/terminate", "终止数据库会话", "数据库会话", SeverityCritical),

		// 插件
		View("GET /api/admin/system/plugins", "查看插件", ""),
		View("GET /api/admin/system/plugins/registry", "查看插件钩子注册表", ""),
		View("GET /api/admin/system/plugins/executions", "查看插件执行记录", ""),
		View("GET /api/admin/system/plugins/:id", "查看插件详情", "插件"),
		Write("POST /api/admin/system/plugins", "安装插件", "插件", SeverityHigh),
		Write("PUT /api/admin/system/plugins/:id", "编辑插件", "插件", SeverityHigh),
		Write("POST /api/admin/system/plugins/:id/enable", "启用插件", "插件", SeverityHigh),
		Write("POST /api/admin/system/plugins/:id/disable", "停用插件", "插件", SeverityHigh),
		Write("DELETE /api/admin/system/plugins/:id", "删除插件", "插件", SeverityHigh),

		// 崩溃日志
		View("GET /api/admin/system/crashlogs", "查看崩溃日志", ""),
		View("GET /api/admin/system/crashlogs/:filename", "查看崩溃日志详情", "崩溃日志"),
		Write("DELETE /api/admin/system/crashlogs/:filename", "删除崩溃日志", "崩溃日志", SeverityMedium),

		// 审计日志
		View("GET /api/admin/system/audit-logs", "查看审计日志", ""),
		View("GET /api/admin/system/audit-logs/stats", "查看审计统计", ""),
		View("GET /api/admin/system/audit-logs/overview", "查看审计总览", ""),
		View("GET /api/admin/system/audit-logs/facets", "查看审计筛选项", ""),
		View("GET /api/admin/system/audit-logs/:id", "查看审计日志详情", "审计日志"),
		Export("GET /api/admin/system/audit-logs/export", "导出审计日志", "审计日志"),
	)

	// 平台级存储配置（应用未配置时共用）
	Register("storage",
		View("POST /api/admin/platform/storage-config/list", "查看平台存储配置", ""),
		View("POST /api/admin/platform/storage-config/detail", "查看平台存储配置详情", "存储配置"),
		Write("POST /api/admin/platform/storage-config/create", "新建平台存储配置", "存储配置", SeverityHigh),
		Write("POST /api/admin/platform/storage-config/update", "编辑平台存储配置", "存储配置", SeverityHigh),
		Write("POST /api/admin/platform/storage-config/delete", "删除平台存储配置", "存储配置", SeverityHigh),
		Write("POST /api/admin/platform/storage-config/test", "测试平台存储配置", "存储配置", SeverityLow),
	)
}
