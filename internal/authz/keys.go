// Package authz 是平台**唯一**的授权判定引擎。
//
// 它取代了此前散在三处的判定逻辑：
//
//	internal/service/admin_service.go   平台 RBAC —— 一个退化成 map 查表的 Casbin 模型
//	internal/service/org_access_control.go  组织 RBAC —— 另一个模型、另一个 enforcer
//	internal/middleware/admin.go       路由 → 权限点 —— 250 行 switch + 前缀/后缀字符串匹配
//
// 三者各自持有一份策略、各自决定什么时候重载、且**都只在内存里**：
// 进程一重启策略回到编译期的样子，多实例部署时改一次角色只有一台机器知道。
//
// 现在只有一个模型、一份策略表（authz_policies）、一个 enforcer，
// 落库 + 跨实例广播 + 可解释（EnforceEx 能说出是哪一条策略放行/拒绝的）。
//
// # 命名空间
//
// 判定的三个维度全部走本文件的构造函数，**不允许在别处拼这些字符串**。
// 主体与域是安全边界的一部分，一次拼错（比如把 "app:5" 写成 "app5"）
// 的表现是静默放行或静默拒绝，而不是编译错误。
//
//	主体 sub    role:<key>            平台/应用角色（内置与自定义）
//	            admin:<id>            某一个具体管理员（直接授予 / 直接禁止）
//	            orgrole:<key>         组织内置角色
//	            orgrole:<orgID>:<key> 组织自定义角色
//	域   dom    platform              平台（全局）作用域
//	            app:<id>              某个应用
//	            org:<id>              某个组织
//	            *                     任意域（策略侧才用，请求侧永远是具体值）
//	客体 obj    权限点，支持前缀通配：ticket:* / app:* / *
package authz

import "strconv"

// 域常量。请求侧永远给具体值，`*` 只出现在策略里。
const (
	// PlatformDomain 平台（全局）作用域。
	//
	// 取一个**不可能与 app:N / org:N 前缀相撞**的字面量是有意的：
	// 匹配函数是 keyMatch，若平台域取 "" 或 "*"，一条 `app:*` 的策略
	// 会连平台级请求一起放行 —— 那正是"应用管理员给自己解封"那一类越权。
	PlatformDomain = "platform"
	// AnyDomain 仅用于策略：该策略在任何域下都成立。
	AnyDomain = "*"
	// AnyPermission 仅用于策略：不受权限点约束（超管式授权）。
	AnyPermission = "*"

	appDomainPrefix = "app:"
	orgDomainPrefix = "org:"
)

// 主体前缀。
const (
	rolePrefix    = "role:"
	adminPrefix   = "admin:"
	orgRolePrefix = "orgrole:"
)

// 策略效果。
const (
	// EffectAllow 放行。
	EffectAllow = "allow"
	// EffectDeny 拒绝，且**压倒任何放行** —— 这是旧实现完全表达不了的一档：
	// 以前要收回某人的一个能力，只能把他从角色里摘掉（连带失去其余全部权限），
	// 或者专门为他造一个角色。
	EffectDeny = "deny"
)

// AppDomain 返回某个应用的域键。
func AppDomain(appID int64) string {
	return appDomainPrefix + strconv.FormatInt(appID, 10)
}

// OrgDomain 返回某个组织的域键。
func OrgDomain(orgID int64) string {
	return orgDomainPrefix + strconv.FormatInt(orgID, 10)
}

// DomainForApp 按可选的应用 ID 决定请求域：nil = 平台作用域。
func DomainForApp(appID *int64) string {
	if appID == nil || *appID <= 0 {
		return PlatformDomain
	}
	return AppDomain(*appID)
}

// RoleSubject 平台/应用角色主体。
func RoleSubject(roleKey string) string {
	return rolePrefix + roleKey
}

// AdminSubject 具体管理员主体，用于「只给这一个人」的直接授予与禁止。
func AdminSubject(adminID int64) string {
	return adminPrefix + strconv.FormatInt(adminID, 10)
}

// OrgRoleSubject 组织内置角色主体（对所有组织通用）。
func OrgRoleSubject(roleKey string) string {
	return orgRolePrefix + roleKey
}

// OrgCustomRoleSubject 某个组织自定义角色的主体。
//
// 带上 orgID 是必须的：两个组织可以各自建一个同名角色，
// 不区分的话 A 组织给自己的角色加权限会连带改掉 B 组织的同名角色。
func OrgCustomRoleSubject(orgID int64, roleKey string) string {
	return orgRolePrefix + strconv.FormatInt(orgID, 10) + ":" + roleKey
}
