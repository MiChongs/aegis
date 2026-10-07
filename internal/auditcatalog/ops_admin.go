package auditcatalog

// 管理员与权限：管理员账号、角色、授权策略、临时权限、委托与会话。
func init() {
	Register("admin",
		View("GET /api/admin/system/admins", "查看管理员列表", ""),
		Write("POST /api/admin/system/admins", "新增管理员", "管理员", SeverityHigh),
		Write("PUT /api/admin/system/admins/:adminId/access", "调整管理员权限", "管理员", SeverityHigh),
		Write("PUT /api/admin/system/admins/:adminId/status", "变更管理员状态", "管理员", SeverityHigh),
		View("GET /api/admin/system/admins/online", "查看在线管理员", ""),
		View("GET /api/admin/system/admins/:adminId/sessions", "查看管理员会话", "管理员"),
		Write("POST /api/admin/system/admins/:adminId/force-logout", "强制管理员下线", "管理员", SeverityHigh),

		View("GET /api/admin/system/roles", "查看角色目录", ""),
		View("GET /api/admin/system/roles/permissions", "查看权限树", ""),
		View("GET /api/admin/system/roles/matrix", "查看角色权限矩阵", ""),
		View("GET /api/admin/system/roles/graph", "查看角色关系图", ""),
		View("GET /api/admin/system/roles/:roleKey/impact", "预览角色变更影响", "角色"),
		Write("POST /api/admin/system/roles", "新建自定义角色", "角色", SeverityHigh),
		Write("PUT /api/admin/system/roles/:roleKey", "编辑自定义角色", "角色", SeverityHigh),
		Write("DELETE /api/admin/system/roles/:roleKey", "删除自定义角色", "角色", SeverityHigh),

		View("GET /api/admin/system/authz/model", "查看授权模型", ""),
		View("GET /api/admin/system/authz/policies", "查看授权策略", ""),
		View("GET /api/admin/system/authz/policies/subject", "查看主体授权策略", ""),
		View("POST /api/admin/system/authz/explain", "解释授权判定", ""),
		Write("POST /api/admin/system/authz/reload", "重新加载授权策略", "", SeverityMedium),
		Write("POST /api/admin/system/authz/roles/override", "覆盖角色授权", "角色", SeverityCritical),
		Write("PUT /api/admin/system/authz/admins/:adminId/grants", "设置管理员授权", "管理员", SeverityCritical),

		View("GET /api/admin/system/temp-permissions", "查看临时权限", ""),
		Write("POST /api/admin/system/temp-permissions", "授予临时权限", "临时权限", SeverityHigh),
		Write("POST /api/admin/system/temp-permissions/:permId/revoke", "撤销临时权限", "临时权限", SeverityHigh),
		View("GET /api/admin/system/delegations", "查看权限委托", ""),
		Write("POST /api/admin/system/delegations", "创建权限委托", "权限委托", SeverityHigh),
		Write("POST /api/admin/system/delegations/:delegationId/revoke", "撤销权限委托", "权限委托", SeverityHigh),

		View("GET /api/admin/system/sessions", "查看全部会话", ""),
		Write("POST /api/admin/system/sessions/:sessionId/revoke", "撤销会话", "会话", SeverityHigh),
		View("GET /api/admin/system/online/stats", "查看在线统计", ""),
		View("GET /api/admin/system/online/apps/:appkey", "查看应用在线统计", ""),
		View("GET /api/admin/system/online/apps/:appkey/users", "查看应用在线用户", ""),
	)
}
