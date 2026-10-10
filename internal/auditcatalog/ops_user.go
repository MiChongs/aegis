package auditcatalog

// 用户管理：应用用户、封禁、会话、云存储内容、登录与会话记录，以及用户主数据
// （统一身份、标签、分群、黑白名单、合并与注销）、用户站点与角色申请。
func init() {
	Register("user",
		// 应用用户
		View("GET /api/admin/apps/:appkey/users", "查看用户列表", ""),
		View("GET /api/admin/apps/:appkey/users/:userId", "查看用户详情", "用户"),
		Export("GET /api/admin/apps/:appkey/users/export", "导出用户", ""),
		Write("PUT /api/admin/apps/:appkey/users/:userId/profile", "修改用户资料", "用户", SeverityMedium),
		Write("PUT /api/admin/apps/:appkey/users/:userId/status", "修改用户状态", "用户", SeverityHigh),
		View("GET /api/admin/apps/:appkey/users/:userId/oauth2-grants", "查看用户的第三方授权", "用户"),
		Write("DELETE /api/admin/apps/:appkey/users/:userId/oauth2-grants", "撤销用户的第三方授权", "用户", SeverityMedium),
		Write("PUT /api/admin/apps/:appkey/users/status/batch", "批量修改用户状态", "用户", SeverityCritical),
		Write("DELETE /api/admin/apps/:appkey/users/:userId", "删除用户", "用户", SeverityCritical),
		Write("POST /api/admin/apps/:appkey/users/:userId/reset-password", "重置用户密码", "用户", SeverityHigh),
		Write("POST /api/admin/apps/:appkey/users/:userId/avatar", "上传用户头像", "用户", SeverityLow),
		Write("DELETE /api/admin/apps/:appkey/users/:userId/avatar", "移除用户头像", "用户", SeverityMedium),

		// 封禁
		View("GET /api/admin/apps/:appkey/users/:userId/bans", "查看用户封禁记录", "用户"),
		View("GET /api/admin/apps/:appkey/users/:userId/bans/active", "查看用户当前封禁", "用户"),
		Write("POST /api/admin/apps/:appkey/users/:userId/bans", "封禁用户", "用户", SeverityHigh),
		Write("POST /api/admin/apps/:appkey/users/:userId/bans/:banId/revoke", "解除用户封禁", "用户", SeverityHigh),
		Write("POST /api/admin/apps/:appkey/users/bans/batch", "批量封禁用户", "用户", SeverityCritical),

		// 会话
		View("GET /api/admin/apps/:appkey/users/:userId/sessions", "查看用户会话", "用户"),
		Write("DELETE /api/admin/apps/:appkey/users/:userId/sessions/:tokenHash", "下线用户会话", "用户", SeverityHigh),
		Write("POST /api/admin/apps/:appkey/users/:userId/sessions/revoke-batch", "批量下线用户会话", "用户", SeverityHigh),
		Write("POST /api/admin/apps/:appkey/users/:userId/revoke-sessions", "下线用户全部会话", "用户", SeverityHigh),

		// 用户钱包（只读）
		View("GET /api/admin/apps/:appkey/users/:userId/wallet", "查看用户钱包", "用户"),
		View("GET /api/admin/apps/:appkey/users/:userId/wallet/transactions", "查看用户钱包流水", "用户"),

		// 用户云存储
		View("GET /api/admin/apps/:appkey/users/:userId/cloud-storage", "查看用户云存储", "用户"),
		View("GET /api/admin/apps/:appkey/users/:userId/ad-policy", "查看用户广告服务情况", "用户"),
		View("GET /api/admin/apps/:appkey/users/:userId/cloud-storage/items", "查看用户云存储条目", "用户"),
		View("GET /api/admin/apps/:appkey/users/:userId/cloud-storage/items/:itemId", "查看云存储条目详情", "云存储条目"),
		View("GET /api/admin/apps/:appkey/users/:userId/cloud-storage/items/:itemId/revisions", "查看云存储条目历史版本", "云存储条目"),
		Write("PUT /api/admin/apps/:appkey/users/:userId/cloud-storage", "修改用户云存储配额", "用户", SeverityMedium),
		Write("DELETE /api/admin/apps/:appkey/users/:userId/cloud-storage", "清空用户云存储", "用户", SeverityCritical),
		Write("DELETE /api/admin/apps/:appkey/users/:userId/cloud-storage/items/:itemId", "删除云存储条目", "云存储条目", SeverityHigh),
		Write("POST /api/admin/apps/:appkey/users/:userId/cloud-storage/items/:itemId/link", "生成云存储条目下载链接", "云存储条目", SeverityMedium),
		Write("POST /api/admin/apps/:appkey/users/:userId/cloud-storage/items/:itemId/restore", "恢复云存储条目", "云存储条目", SeverityMedium),
		Write("POST /api/admin/apps/:appkey/users/:userId/cloud-storage/items/:itemId/rollback", "回滚云存储条目", "云存储条目", SeverityHigh),

		// 登录与会话记录
		View("GET /api/admin/apps/:appkey/audits/login", "查看登录记录", ""),
		Export("GET /api/admin/apps/:appkey/audits/login/export", "导出登录记录", ""),
		View("GET /api/admin/apps/:appkey/audits/sessions", "查看会话记录", ""),
		Export("GET /api/admin/apps/:appkey/audits/sessions/export", "导出会话记录", ""),
		View("GET /api/admin/apps/:appkey/users/:userId/audits/login", "查看用户登录记录", "用户"),
		View("GET /api/admin/apps/:appkey/users/:userId/audits/sessions", "查看用户会话记录", "用户"),

		// 第三方账号绑定
		View("GET /api/admin/apps/:appkey/oauth-bindings", "查看第三方账号绑定", ""),
		Write("DELETE /api/admin/apps/:appkey/oauth-bindings", "解除第三方账号绑定", "第三方绑定", SeverityHigh),

		// 用户主数据：统一身份
		View("GET /api/admin/system/user-master/identities", "查看统一身份列表", ""),
		View("GET /api/admin/system/user-master/identities/:id", "查看统一身份详情", "统一身份"),
		View("GET /api/admin/system/user-master/identities/:id/mappings", "查看身份映射", "统一身份"),
		View("GET /api/admin/system/user-master/identities/:id/tags", "查看身份标签", "统一身份"),
		Write("POST /api/admin/system/user-master/identities", "新建统一身份", "统一身份", SeverityMedium),
		Write("PUT /api/admin/system/user-master/identities/:id/lifecycle", "修改身份生命周期", "统一身份", SeverityHigh),
		Write("PUT /api/admin/system/user-master/identities/:id/risk", "修改身份风险等级", "统一身份", SeverityHigh),
		Write("PUT /api/admin/system/user-master/identities/:id/status", "修改身份状态", "统一身份", SeverityHigh),
		Write("POST /api/admin/system/user-master/mappings", "新建身份映射", "身份映射", SeverityMedium),
		Write("DELETE /api/admin/system/user-master/mappings/:id", "删除身份映射", "身份映射", SeverityHigh),
		View("GET /api/admin/system/user-master/merges", "查看身份合并记录", ""),
		Write("POST /api/admin/system/user-master/merges", "合并统一身份", "统一身份", SeverityCritical),
		Write("POST /api/admin/system/user-master/sync", "同步统一身份", "统一身份", SeverityMedium),
		Write("POST /api/admin/system/user-master/sync/batch", "批量同步统一身份", "统一身份", SeverityHigh),

		// 用户主数据：标签
		View("GET /api/admin/system/user-master/tags", "查看用户标签", ""),
		Write("POST /api/admin/system/user-master/tags", "新建用户标签", "用户标签", SeverityLow),
		Write("DELETE /api/admin/system/user-master/tags/:id", "删除用户标签", "用户标签", SeverityHigh),
		Write("POST /api/admin/system/user-master/tags/assign", "为用户添加标签", "用户标签", SeverityMedium),
		Write("POST /api/admin/system/user-master/tags/remove", "移除用户标签", "用户标签", SeverityMedium),

		// 用户主数据：分群
		View("GET /api/admin/system/user-master/segments", "查看用户分群", ""),
		View("GET /api/admin/system/user-master/segments/:id/members", "查看分群成员", "用户分群"),
		Write("POST /api/admin/system/user-master/segments", "新建用户分群", "用户分群", SeverityLow),
		Write("PUT /api/admin/system/user-master/segments/:id", "编辑用户分群", "用户分群", SeverityMedium),
		Write("DELETE /api/admin/system/user-master/segments/:id", "删除用户分群", "用户分群", SeverityHigh),
		Write("POST /api/admin/system/user-master/segments/:id/members", "添加分群成员", "用户分群", SeverityMedium),
		Write("DELETE /api/admin/system/user-master/segments/:id/members/:identityId", "移除分群成员", "用户分群", SeverityMedium),

		// 用户主数据：黑白名单（lists 与 user-lists 是同一组接口的两个路径）
		View("GET /api/admin/system/user-master/lists", "查看黑白名单", ""),
		Write("POST /api/admin/system/user-master/lists", "添加黑白名单条目", "名单条目", SeverityHigh),
		Write("DELETE /api/admin/system/user-master/lists/:id", "删除黑白名单条目", "名单条目", SeverityHigh),
		View("POST /api/admin/system/user-master/lists/check", "检查黑名单命中", ""),
		View("GET /api/admin/system/user-master/user-lists", "查看黑白名单", ""),
		Write("POST /api/admin/system/user-master/user-lists", "添加黑白名单条目", "名单条目", SeverityHigh),
		Write("DELETE /api/admin/system/user-master/user-lists/:id", "删除黑白名单条目", "名单条目", SeverityHigh),

		// 用户主数据：申诉与注销
		View("GET /api/admin/system/user-master/appeals", "查看用户申诉", ""),
		Write("POST /api/admin/system/user-master/appeals", "登记用户申诉", "申诉", SeverityLow),
		Write("PUT /api/admin/system/user-master/appeals/:id", "处理用户申诉", "申诉", SeverityHigh),
		View("GET /api/admin/system/user-master/deactivations", "查看注销申请", ""),
		Write("POST /api/admin/system/user-master/deactivations", "发起账号注销", "统一身份", SeverityCritical),
		Write("POST /api/admin/system/user-master/deactivations/:id/cancel", "取消账号注销", "注销申请", SeverityHigh),

		// 用户站点（兼容接口）
		View("POST /api/admin/app/site/list", "查看用户站点列表（兼容接口）", ""),
		View("POST /api/admin/app/site/detail", "查看用户站点详情（兼容接口）", "用户站点"),
		View("POST /api/admin/app/site/user-sites", "查看用户的站点（兼容接口）", "用户"),
		View("POST /api/admin/app/site/audit-list", "查看站点待审列表（兼容接口）", ""),
		View("POST /api/admin/app/site/audit-stats", "查看站点审核统计（兼容接口）", ""),
		Write("POST /api/admin/app/site/update", "编辑用户站点（兼容接口）", "用户站点", SeverityMedium),
		Write("POST /api/admin/app/site/delete", "删除用户站点（兼容接口）", "用户站点", SeverityHigh),
		Write("POST /api/admin/app/site/toggle-pin", "置顶或取消置顶站点（兼容接口）", "用户站点", SeverityLow),
		Write("POST /api/admin/app/site/audit", "审核用户站点（兼容接口）", "用户站点", SeverityMedium),
		Write("POST /api/admin/app/site/batch-audit", "批量审核用户站点（兼容接口）", "用户站点", SeverityHigh),

		// 角色申请（兼容接口）
		View("POST /api/admin/app/role-application/list", "查看角色申请列表（兼容接口）", ""),
		View("POST /api/admin/app/role-application/detail", "查看角色申请详情（兼容接口）", "角色申请"),
		View("POST /api/admin/app/role-application/statistics", "查看角色申请统计（兼容接口）", ""),
		Write("POST /api/admin/app/role-application/review", "审批角色申请（兼容接口）", "角色申请", SeverityHigh),
		Write("POST /api/admin/app/role-application/batch-review", "批量审批角色申请（兼容接口）", "角色申请", SeverityHigh),
	)
}
