package service

import plugindomain "aegis/internal/domain/plugin"

// 钩子点常量（26 个，6 领域）
const (
	// 认证 (6)
	HookAuthPreLogin          = "auth.onPreLogin"
	HookAuthPasswordVerified  = "auth.onPasswordVerified"
	HookAuthMFACreated        = "auth.onMFACreated"
	HookAuthMFAVerified       = "auth.onMFAVerified"
	HookAuthSessionIssued     = "auth.onSessionIssued"
	HookAuthLoginFailed       = "auth.onLoginFailed"
	// 用户 (5)
	HookUserRegistered        = "user.onRegistered"
	HookUserProfileUpdated    = "user.onProfileUpdated"
	HookUserDeleted           = "user.onDeleted"
	HookUserRoleChanged       = "user.onRoleChanged"
	HookUserBanned            = "user.onBanned"
	// 应用 (3)
	HookAppCreated            = "app.onCreated"
	HookAppUpdated            = "app.onUpdated"
	HookAppDeleted            = "app.onDeleted"
	// 管理员 (3)
	HookAdminCreated          = "admin.onCreated"
	HookAdminStatusChanged    = "admin.onStatusChanged"
	HookAdminAccessUpdated    = "admin.onAccessUpdated"
	// 支付 (3)
	HookPaymentCreated        = "payment.onCreated"
	HookPaymentCompleted      = "payment.onCompleted"
	HookPaymentRefunded       = "payment.onRefunded"
	// 通知 (2)
	HookNotificationCreated   = "notification.onCreated"
	HookNotificationSent      = "notification.onSent"
	// 存储 (2)
	HookFileUploaded          = "storage.onFileUploaded"
	HookFileDeleted           = "storage.onFileDeleted"
	// 系统 (2)
	HookSettingsUpdated       = "system.onSettingsUpdated"
	HookSystemStartup         = "system.onStartup"
)

// allHookDefinitions 所有钩子点元数据
var allHookDefinitions = []plugindomain.HookDefinition{
	{Name: HookAuthPreLogin, Domain: "auth", Phase: "both", Description: "登录前检查（可拒绝）"},
	{Name: HookAuthPasswordVerified, Domain: "auth", Phase: "after", Description: "密码验证后"},
	{Name: HookAuthMFACreated, Domain: "auth", Phase: "after", Description: "MFA 挑战创建后"},
	{Name: HookAuthMFAVerified, Domain: "auth", Phase: "after", Description: "MFA 验证成功后"},
	{Name: HookAuthSessionIssued, Domain: "auth", Phase: "after", Description: "会话颁发后"},
	{Name: HookAuthLoginFailed, Domain: "auth", Phase: "after", Description: "登录失败后"},

	{Name: HookUserRegistered, Domain: "user", Phase: "after", Description: "用户注册完成后"},
	{Name: HookUserProfileUpdated, Domain: "user", Phase: "after", Description: "用户资料更新后"},
	{Name: HookUserDeleted, Domain: "user", Phase: "after", Description: "用户删除后"},
	{Name: HookUserRoleChanged, Domain: "user", Phase: "after", Description: "用户角色变更后"},
	{Name: HookUserBanned, Domain: "user", Phase: "after", Description: "用户封禁后"},

	{Name: HookAppCreated, Domain: "app", Phase: "after", Description: "应用创建后"},
	{Name: HookAppUpdated, Domain: "app", Phase: "after", Description: "应用更新后"},
	{Name: HookAppDeleted, Domain: "app", Phase: "after", Description: "应用删除后"},

	{Name: HookAdminCreated, Domain: "admin", Phase: "after", Description: "管理员创建后"},
	{Name: HookAdminStatusChanged, Domain: "admin", Phase: "after", Description: "管理员状态变更后"},
	{Name: HookAdminAccessUpdated, Domain: "admin", Phase: "after", Description: "管理员权限更新后"},

	{Name: HookPaymentCreated, Domain: "payment", Phase: "after", Description: "支付订单创建后"},
	{Name: HookPaymentCompleted, Domain: "payment", Phase: "after", Description: "支付完成后"},
	{Name: HookPaymentRefunded, Domain: "payment", Phase: "after", Description: "退款完成后"},

	{Name: HookNotificationCreated, Domain: "notification", Phase: "after", Description: "通知创建后"},
	{Name: HookNotificationSent, Domain: "notification", Phase: "after", Description: "通知发送后"},

	{Name: HookFileUploaded, Domain: "storage", Phase: "after", Description: "文件上传后"},
	{Name: HookFileDeleted, Domain: "storage", Phase: "after", Description: "文件删除后"},

	{Name: HookSettingsUpdated, Domain: "system", Phase: "after", Description: "系统设置更新后"},
	{Name: HookSystemStartup, Domain: "system", Phase: "after", Description: "系统启动后"},
}

// GetAllHookDefinitions 返回所有钩子点定义
func GetAllHookDefinitions() []plugindomain.HookDefinition {
	out := make([]plugindomain.HookDefinition, len(allHookDefinitions))
	copy(out, allHookDefinitions)
	return out
}
