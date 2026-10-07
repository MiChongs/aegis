package auditcatalog

// 个人中心：当前管理员的资料、头像、通行密钥、二次验证与站内通知。
// 用户设置维护工具（/user-settings）操作的是终端用户数据，登记在用户管理下。
func init() {
	Register("account",
		View("GET /api/admin/profile", "查看个人资料", ""),
		Write("PUT /api/admin/profile", "修改个人资料", "", SeverityLow),
		View("GET /api/admin/profile/security", "查看账号安全状态", ""),

		// 账号自助
		View("GET /api/admin/profile/account/availability", "检查用户名可用性", ""),
		Auth("POST /api/admin/profile/account", "修改用户名", SeverityHigh),
		Auth("POST /api/admin/profile/password", "修改密码", SeverityHigh),
		View("GET /api/admin/profile/roles", "查看我的角色", ""),
		View("GET /api/admin/profile/roles/permissions", "查看我的权限", ""),

		// 头像（多个历史路径指向同一个处理）
		Write("POST /api/admin/avatar", "上传头像", "", SeverityLow),
		Write("POST /api/admin/avatar/upload", "上传头像", "", SeverityLow),
		Write("POST /api/admin/profile/avatar", "上传头像", "", SeverityLow),
		Write("POST /api/admin/profile/upload-avatar", "上传头像", "", SeverityLow),
		Write("DELETE /api/admin/profile/avatar", "移除头像", "", SeverityMedium),

		// 通行密钥
		View("GET /api/admin/profile/passkey", "查看通行密钥", ""),
		Auth("POST /api/admin/profile/passkey/register/options", "开始注册通行密钥", SeverityLow),
		Auth("POST /api/admin/profile/passkey/register", "注册通行密钥", SeverityMedium),
		Auth("DELETE /api/admin/profile/passkey/:credentialId", "删除通行密钥", SeverityHigh),

		// 二次验证
		Auth("POST /api/admin/profile/two-factor/enroll", "开始绑定两步验证", SeverityLow),
		Auth("POST /api/admin/profile/two-factor/enable", "启用两步验证", SeverityMedium),
		Auth("POST /api/admin/profile/two-factor/disable", "停用两步验证", SeverityHigh),
		View("GET /api/admin/profile/two-factor/recovery-codes", "查看恢复码状态", ""),
		Auth("POST /api/admin/profile/two-factor/recovery-codes", "生成恢复码", SeverityMedium),
		Auth("POST /api/admin/profile/two-factor/recovery-codes/regenerate", "重新生成恢复码", SeverityHigh),

		// 站内通知
		View("GET /api/admin/notifications", "查看站内通知", ""),
		View("GET /api/admin/notifications/unread-count", "查看未读通知数", ""),
		Write("POST /api/admin/notifications/read", "标记通知已读", "", SeverityLow),
		Write("POST /api/admin/notifications/delete", "删除站内通知", "", SeverityLow),
	)

	Register("user",
		View("GET /api/admin/user-settings/user", "查看用户设置", "用户"),
		View("GET /api/admin/user-settings/stats", "查看用户设置统计", ""),
		View("GET /api/admin/user-settings/check-integrity", "检查用户设置完整性", ""),
		Write("POST /api/admin/user-settings/initialize-user", "初始化用户设置", "用户", SeverityMedium),
		Write("POST /api/admin/user-settings/batch-initialize", "批量初始化用户设置", "", SeverityHigh),
		Write("DELETE /api/admin/user-settings/cleanup", "清理用户设置", "", SeverityCritical),
	)
}
