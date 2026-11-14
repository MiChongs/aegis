package authz

import "net/http"

// 权限树：目录 → 菜单 → 操作。
//
// 这一份定义同时驱动四件事，彼此由本包的测试守住一致性：
//
//  1. 控制台角色编辑器的勾选树（目录 / 菜单只是分组，真正落进策略的是操作节点的 Code）；
//  2. 路由权限规则表（每个操作节点声明它覆盖的接口，规则表由此生成）；
//  3. 资源层级（Casbin g2）：每个细分权限点挂在它取代的旧粗粒度权限点下，
//     于是持有 `app:write` 这类旧权限点的角色 / 策略 / MCP 密钥 / 临时权限原样生效；
//  4. 生效权限快照（/api/admin/auth/me），控制台据此决定按钮显隐。
//
// # 命名约定
//
//   - 读类操作一律以 `:read` 结尾。只读 MCP 密钥靠这个后缀放行用 POST 承载的查询接口
//     （见 middleware.IsReadPermission），起别的名字会让只读密钥调不动查询。
//   - 新权限点必须落在它所替换的旧权限点的**前缀家族**里：替换 `app:user:read` 的写成
//     `app:user:…`，替换 `app:read` 的写成 `app:…` 但不能落进 `app:user:` / `app:notification:`。
//     否则一条旧的前缀通配（比如自定义角色里的 `app:user:*`）会多吃到或少吃到权限 ——
//     TestLegacyHoldersKeepRouteAccess 逐条路由比对，写错会直接失败。
//   - 一个代码要么是树上的操作节点，要么是旧权限点（只作兼容组），不能同时是两者：
//     否则勾选「旧代码」那一项会顺带放行它名下全部细分项，而界面上看不出来。

// NodeKind 节点类型（对应若依的 目录 M / 菜单 C / 按钮 F）。
type NodeKind string

const (
	NodeDir    NodeKind = "dir"
	NodeMenu   NodeKind = "menu"
	NodeAction NodeKind = "action"
)

// PermScope 权限点的判定作用域。
type PermScope string

const (
	// ScopePlatform 只在平台域判定：绑定到应用的角色永远满足不了它。
	ScopePlatform PermScope = "platform"
	// ScopeAppOnly 只在应用域判定。
	ScopeAppOnly PermScope = "app"
	// ScopeBoth 两个域都有接口用到。
	ScopeBoth PermScope = "both"
)

// PermissionNode 权限树节点。
type PermissionNode struct {
	Key         string           `json:"key"`
	Code        string           `json:"code,omitempty"`
	Name        string           `json:"name"`
	Kind        NodeKind         `json:"kind"`
	Scope       PermScope        `json:"scope"`
	Icon        string           `json:"icon,omitempty"`
	Description string           `json:"description,omitempty"`
	Danger      bool             `json:"danger,omitempty"`
	Legacy      string           `json:"legacy,omitempty"`
	Routes      int              `json:"routes,omitempty"`
	Children    []PermissionNode `json:"children,omitempty"`

	routes []routeSpec
}

// routeSpec 操作节点覆盖的一组接口。
type routeSpec struct {
	methods []string
	pattern string
	note    string
}

// ── 路由前缀简写 ──

const (
	pApp      = "/api/admin/apps/:appkey"
	pSystem   = "/api/admin/system"
	pPlatform = "/api/admin/platform"
	pAdmin    = "/api/admin"
)

var (
	mRead   = readMethods
	mWrite  = []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}
	mDelete = []string{http.MethodDelete}
	mPost   = []string{http.MethodPost}
	mPut    = []string{http.MethodPut}
	mPatch  = []string{http.MethodPatch}
	mAny    []string
)

func rt(methods []string, pattern string) routeSpec {
	return routeSpec{methods: methods, pattern: pattern}
}

// under 一个集合根与它下面的全部子资源（`*` 吃零段）。
func under(methods []string, prefix string) routeSpec { return rt(methods, prefix+"/*") }

// ── 节点构造 ──

type actionOpt func(*PermissionNode)

func danger() actionOpt { return func(n *PermissionNode) { n.Danger = true } }
func desc(text string) actionOpt {
	return func(n *PermissionNode) { n.Description = text }
}

// act 操作节点。legacy 是它所替换的旧权限点；与 code 相同表示这是一个未拆分的原有权限点。
func act(code, name, legacy string, routes []routeSpec, opts ...actionOpt) PermissionNode {
	node := PermissionNode{Key: code, Code: code, Name: name, Kind: NodeAction, Legacy: legacy, routes: routes}
	if legacy == code {
		node.Legacy = ""
	}
	for _, opt := range opts {
		opt(&node)
	}
	return node
}

func routes(items ...routeSpec) []routeSpec { return items }

func menu(key, name, icon string, scope PermScope, children ...PermissionNode) PermissionNode {
	return PermissionNode{Key: key, Name: name, Icon: icon, Kind: NodeMenu, Scope: scope, Children: children}
}

func dir(key, name, icon string, scope PermScope, children ...PermissionNode) PermissionNode {
	return PermissionNode{Key: key, Name: name, Icon: icon, Kind: NodeDir, Scope: scope, Children: children}
}

// 旧权限点（兼容组）。它们不在树上，只作为 g2 的父节点存在。
const (
	LegacyAppRead           = "app:read"
	LegacyAppWrite          = "app:write"
	LegacyAppUserRead       = "app:user:read"
	LegacyAppUserWrite      = "app:user:write"
	LegacySystemAdminManage = "system:admin:manage"
	LegacyVersionRead       = "version:read"
	LegacyVersionWrite      = "version:write"
	LegacySettingsRead      = "system:settings:read"
	LegacySettingsWrite     = "system:settings:write"
)

// LegacyPermissions 旧权限点的中文名，用于在角色编辑器里展示历史策略。
var legacyPermissionNames = map[string]string{
	LegacyAppRead:           "应用信息查看（旧版合并权限）",
	LegacyAppWrite:          "应用信息修改（旧版合并权限）",
	LegacyAppUserRead:       "应用用户查看（旧版合并权限）",
	LegacyAppUserWrite:      "应用用户管理（旧版合并权限）",
	LegacySystemAdminManage: "系统管理（旧版合并权限）",
	LegacyVersionRead:       "版本查看（旧版合并权限）",
	LegacyVersionWrite:      "版本管理（旧版合并权限）",
	LegacySettingsRead:      "系统设置查看（旧版，无对应接口）",
	LegacySettingsWrite:     "系统设置修改（旧版，无对应接口）",
}

// 应用模块的标准三件套：查看 / 编辑 / 删除。
//
// 查看吃 GET，删除吃 DELETE，编辑吃剩下的写方法。三条规则同前缀，
// 生成规则表时按「方法受限优先」排序，因此不会互相遮挡。
func appStd(base, prefix string, withDelete bool, extra ...PermissionNode) []PermissionNode {
	nodes := append([]PermissionNode{}, extra...)
	if withDelete {
		nodes = append(nodes, act(base+":delete", "删除", LegacyAppWrite, routes(under(mDelete, pApp+prefix))))
	}
	return append(nodes,
		act(base+":read", "查看", LegacyAppRead, routes(under(mRead, pApp+prefix))),
		act(base+":write", "编辑", LegacyAppWrite, routes(under(mWrite, pApp+prefix))),
	)
}

// sysStd 平台模块（原 system:admin:manage 大桶）的查看 / 编辑 / 删除。
func sysStd(base, prefix string, withDelete bool, extra ...PermissionNode) []PermissionNode {
	nodes := append([]PermissionNode{}, extra...)
	if withDelete {
		nodes = append(nodes, act(base+":delete", "删除", LegacySystemAdminManage, routes(under(mDelete, pSystem+prefix))))
	}
	return append(nodes,
		act(base+":read", "查看", LegacySystemAdminManage, routes(under(mRead, pSystem+prefix))),
		act(base+":write", "编辑", LegacySystemAdminManage, routes(under(mWrite, pSystem+prefix))),
	)
}

// permissionTree 完整的权限树。顺序即控制台里的展示顺序。
func permissionTree() []PermissionNode {
	const sys = LegacySystemAdminManage
	return []PermissionNode{
		// ════════════════ 应用 ════════════════
		dir("app", "应用", "app-window", ScopeAppOnly,
			menu("app.info", "基本信息", "info", ScopeBoth,
				act("app:info:read", "查看", LegacyAppRead, routes(
					rt(mRead, pAdmin+"/apps"), rt(mRead, pApp),
					rt(mRead, pApp+"/content/overview"),
				)),
				act("app:info:edit", "编辑", LegacyAppWrite, routes(rt(mPut, pApp))),
				act("app:info:delete", "删除应用", LegacyAppWrite, routes(rt(mDelete, pApp)), danger()),
				act(PermAppCreate, "创建应用", LegacyAppWrite, nil,
					desc("平台级授予时不受自助创建配额约束")),
			),
			menu("app.stats", "统计分析", "bar-chart", ScopeAppOnly,
				act("app:stats:read", "查看", LegacyAppRead, routes(under(mRead, pApp+"/stats"))),
			),
			menu("app.report", "报表", "line-chart", ScopeAppOnly,
				act("app:report:export", "导出", LegacyAppRead, routes(rt(mRead, pApp+"/reports/export"))),
				act("app:report:read", "查看", LegacyAppRead, routes(under(mRead, pApp+"/reports"))),
			),
			menu("app.audit", "审计日志", "scroll-text", ScopeAppOnly,
				act(PermAuditLoginRead, "登录审计", PermAuditLoginRead, routes(
					rt(mAny, pApp+"/users/:userId/audits/login"), under(mAny, pApp+"/audits/login"),
				)),
				act(PermAuditSessionRead, "会话审计", PermAuditSessionRead, routes(
					under(mAny, pApp+"/users/:userId/audits"), under(mAny, pApp+"/audits"),
				)),
			),
			menu("app.crash", "崩溃报告", "bug", ScopeAppOnly,
				act("app:crash:read", "查看", LegacyAppRead, routes(under(mRead, pApp+"/crash-reports"))),
				act("app:crash:resolve", "标记处理", LegacyAppWrite, routes(rt(mPost, pApp+"/crash-reports/resolve"))),
				act("app:crash:delete", "删除", LegacyAppWrite, routes(
					rt(mPost, pApp+"/crash-reports/delete"), under(mDelete, pApp+"/crash-reports"),
				), danger()),
			),
			menu("app.governance", "治理状态", "gavel", ScopeAppOnly,
				act("app:governance:read", "查看", LegacyAppRead, routes(under(mRead, pApp+"/governance"))),
				act("app:governance:appeal", "提交申诉", LegacyAppWrite, routes(under(mWrite, pApp+"/governance"))),
			),
		),

		dir("auth", "认证与安全", "fingerprint", ScopeAppOnly,
			menu("app.policy", "认证与会话", "fingerprint", ScopeAppOnly,
				act("app:policy:read", "查看", LegacyAppRead, routes(
					rt(mRead, pApp+"/policy"), under(mRead, pApp+"/login-baseline"),
				)),
				act("app:policy:write", "编辑", LegacyAppWrite, routes(rt(mPut, pApp+"/policy"))),
				act("app:policy:baseline", "重置登录基线", LegacyAppWrite, routes(under(mDelete, pApp+"/login-baseline"))),
			),
			menu("app.device_limit", "登录设备", "monitor-smartphone", ScopeAppOnly,
				act("app:device_limit:policy", "修改策略", LegacyAppWrite, routes(rt(mPut, pApp+"/device-limit/policy"))),
				act("app:user:device:read", "查看", LegacyAppUserRead, routes(
					under(mRead, pApp+"/device-limit"),
					rt(mPost, pApp+"/device-limit/simulate"),
				)),
				act("app:user:device:manage", "例外与下线", LegacyAppUserWrite, routes(under(mWrite, pApp+"/device-limit"))),
			),
			menu("app.register_ip", "注册准入", "shield-ban", ScopeAppOnly,
				act("app:register_ip:policy", "修改策略", LegacyAppWrite, routes(rt(mPut, pApp+"/register-ip/policy"))),
				act("app:register_ip:list", "名单管理", LegacyAppWrite, routes(
					under(mWrite, pApp+"/register-ip/lists"),
					under(mWrite, pApp+"/register-ip/device-lists"),
				)),
				act("app:user:register:read", "查看", LegacyAppUserRead, routes(
					under(mRead, pApp+"/register-ip"),
					rt(mPost, pApp+"/register-ip/simulate"),
				)),
				act("app:user:register:release", "解除限制", LegacyAppUserWrite, routes(under(mWrite, pApp+"/register-ip"))),
			),
			menu("app.password", "密码与凭证", "key-round", ScopeAppOnly,
				act("app:password:read", "查看", LegacyAppRead, routes(
					rt(mRead, pApp+"/password-policy"), rt(mAny, "/api/app/password-policy/get"),
				)),
				act("app:password:write", "编辑", LegacyAppWrite, routes(
					under(mWrite, pApp+"/password-policy"), under(mAny, "/api/app/password-policy"),
				)),
			),
			menu("app.passkey", "通行密钥", "scan-face", ScopeAppOnly,
				appStd("app:passkey", "/passkey", false)...,
			),
			menu("app.captcha", "验证码", "shield-check", ScopeAppOnly,
				appStd("app:captcha", "/captcha-config", false)...,
			),
			menu("app.oauth", "第三方登录", "key-round", ScopeAppOnly,
				appStd("app:oauth", "/oauth-providers", true)...,
			),
			menu("app.account_setup", "账号初始化", "user-cog", ScopeAppOnly,
				appStd("app:account_setup", "/account-setup", false)...,
			),
			menu("app.oauth2", "OAuth2 授权服务", "network", ScopeAppOnly,
				appStd("app:oauth2", "/oauth2", true,
					act("app:oauth2:secret", "轮换客户端密钥", LegacyAppWrite,
						routes(rt(mPost, pApp+"/oauth2/clients/:clientId/rotate-secret")), danger()),
				)...,
			),
			menu("app.auth_protocol", "接入", "plug-zap", ScopeAppOnly,
				act("app:auth_protocol:secret", "轮换密钥", LegacyAppWrite, routes(
					rt(mPost, pApp+"/auth-protocol/secret/rotate"), rt(mPost, pApp+"/auth-protocol/transport/rotate"),
					under(mDelete, pApp+"/auth-protocol/transport/keys"),
				), danger()),
				act("app:auth_protocol:read", "查看", LegacyAppRead, routes(
					under(mRead, pApp+"/auth-protocol"), rt(mRead, pApp+"/encryption"),
				)),
				act("app:auth_protocol:write", "编辑", LegacyAppWrite, routes(
					under(mWrite, pApp+"/auth-protocol"), rt(mWrite, pApp+"/encryption"),
				)),
			),
		),

		dir("service", "服务能力", "sparkles", ScopeBoth,
			menu("service.email", "邮件", "mail", ScopeBoth,
				act(PermEmailRead, "查看", PermEmailRead, routes(
					rt(mPost, pSystem+"/email/preview"), under(mRead, pSystem+"/email"),
					rt(mAny, "/api/admin/app/email-config/list"), rt(mAny, "/api/admin/app/email-config/detail"),
					rt(mAny, "/api/admin/app/email-config/deliveries"), rt(mAny, "/api/admin/app/email-config/stats"),
					rt(mAny, "/api/admin/app/email-config/channel"), rt(mAny, "/api/admin/app/email-config/policy"),
					rt(mAny, "/api/admin/app/email-config/preview"), rt(mAny, "/api/admin/app/email-config/overview"),
					rt(mAny, "/api/admin/app/email-config/deliveries/detail"), rt(mAny, "/api/admin/app/email-config/suppressions"),
				)),
				act(PermEmailWrite, "编辑", PermEmailWrite, routes(
					under(mAny, pSystem+"/email"), under(mAny, "/api/admin/app/email-config"),
				)),
			),
			menu("service.ai_platform", "平台 AI 通道", "sparkles", ScopePlatform,
				act(PermAIRead, "查看", PermAIRead, routes(under(mRead, pSystem+"/ai")),
					desc("密钥恒不回显")),
				act(PermAIWrite, "编辑", PermAIWrite, routes(under(mAny, pSystem+"/ai")),
					desc("含连通性测试，会产生真实调用")),
			),
			menu("app.ai", "应用 AI 服务", "bot", ScopeAppOnly,
				appStd("app:ai", "/ai", true,
					act("app:ai:agent", "AI 助手对话", LegacyAppWrite, routes(under(mAny, pApp+"/ai/agent")),
						desc("对话中可执行写操作")),
				)...,
			),
			menu("app.ai_gateway", "AI 中转", "route", ScopeAppOnly,
				appStd("app:ai_gateway", "/ai-gateway", true,
					act("app:ai_gateway:credit", "调整用户额度", LegacyAppWrite, routes(rt(mPost, pApp+"/ai-gateway/users/:userId/credits")), danger()),
					act("app:ai_gateway:refund", "请求退款", LegacyAppWrite, routes(rt(mPost, pApp+"/ai-gateway/requests/:requestNo/refund")), danger()),
				)...,
			),
			menu("service.payment", "支付", "credit-card", ScopeAppOnly,
				act(PermPaymentRead, "查看", PermPaymentRead, routes(
					rt(mAny, "/api/admin/app/payment-config/list"), rt(mAny, "/api/admin/app/payment-config/detail"),
					rt(mAny, "/api/admin/app/payment-config/orders/list"), rt(mAny, "/api/admin/app/payment-config/orders/detail"),
					rt(mAny, "/api/admin/app/payment-config/refunds/list"), rt(mAny, "/api/admin/app/payment-config/refunds/order"),
					rt(mAny, "/api/admin/app/payment-config/refunds/refundable"),
				)),
				act(PermPaymentWrite, "编辑", PermPaymentWrite, routes(under(mAny, "/api/admin/app/payment-config"))),
			),
			menu("service.storage", "存储配置与云存储", "cloud", ScopeAppOnly,
				act(PermStorageRead, "查看", PermStorageRead, routes(
					rt(mAny, "/api/admin/app/storage-config/list"), rt(mAny, "/api/admin/app/storage-config/detail"),
					under(mRead, pApp+"/cloud-storage"),
				)),
				act(PermStorageWrite, "编辑", PermStorageWrite, routes(
					under(mAny, "/api/admin/app/storage-config"), under(mAny, pApp+"/cloud-storage"),
				)),
			),
			menu("app.listen", "一起听", "headphones", ScopeAppOnly,
				act("app:listen:read", "查看", LegacyAppRead, routes(under(mRead, pApp+"/listen-together"))),
				act("app:listen:manage", "关闭房间与踢人", LegacyAppWrite, routes(under(mWrite, pApp+"/listen-together"))),
			),
			menu("service.user_setting", "用户设置", "sliders-horizontal", ScopePlatform,
				act(PermSystemUserSettingRead, "查看", PermSystemUserSettingRead, routes(
					rt(mAny, pAdmin+"/user-settings/stats"), rt(mAny, pAdmin+"/user-settings/user"),
					rt(mAny, pAdmin+"/user-settings/check-integrity"),
				)),
				act(PermSystemUserSettingWrite, "编辑", PermSystemUserSettingWrite, routes(under(mAny, pAdmin+"/user-settings"))),
			),
			menu("service.workflow", "工作流", "workflow", ScopeAppOnly,
				act(PermWorkflowRead, "查看", PermWorkflowRead, routes(
					rt(mAny, "/api/app/workflow/list"), rt(mAny, "/api/app/workflow/detail"),
					rt(mAny, "/api/app/workflow/info"), rt(mAny, "/api/app/workflow/instances"),
					rt(mAny, "/api/app/workflow/instances/list"), rt(mAny, "/api/app/workflow/instances/info"),
					rt(mAny, "/api/app/workflow/instance/detail"), rt(mAny, "/api/app/workflow/tasks/todo"),
					rt(mAny, "/api/app/workflow/task/detail"), rt(mAny, "/api/app/workflow/task/history"),
					rt(mAny, "/api/app/workflow/templates"), rt(mAny, "/api/app/workflow/templates/list"),
					rt(mAny, "/api/app/workflow/validate"), rt(mAny, "/api/app/workflow/statistics"),
					rt(mAny, "/api/app/workflow/logs"),
				)),
				act(PermWorkflowWrite, "编辑", PermWorkflowWrite, routes(under(mAny, "/api/app/workflow"))),
			),
			menu("service.site", "站点", "globe", ScopeAppOnly,
				act(PermSiteRead, "查看", PermSiteRead, routes(
					rt(mAny, "/api/admin/app/site/list"), rt(mAny, "/api/admin/app/site/detail"),
					rt(mAny, "/api/admin/app/site/audit-list"), rt(mAny, "/api/admin/app/site/audit-stats"),
					rt(mAny, "/api/admin/app/site/user-sites"),
				)),
				act(PermSiteAudit, "审核", PermSiteAudit, routes(rt(mAny, "/api/admin/app/site/audit"))),
				act(PermSiteWrite, "编辑", PermSiteWrite, routes(under(mAny, "/api/admin/app/site"))),
			),
			menu("service.role_application", "角色申请", "user-check", ScopeAppOnly,
				act(PermRoleApplicationRead, "查看", PermRoleApplicationRead, routes(
					rt(mAny, "/api/admin/app/role-application/list"), rt(mAny, "/api/admin/app/role-application/detail"),
					rt(mAny, "/api/admin/app/role-application/statistics"),
				)),
				act(PermRoleApplicationReview, "审批", PermRoleApplicationReview, routes(under(mAny, "/api/admin/app/role-application"))),
			),
		),

		dir("growth", "用户运营", "trending-up", ScopeAppOnly,
			menu("app.vip", "会员", "crown", ScopeAppOnly,
				appStd("app:vip", "/vip", true,
					act("app:vip:grant", "发放与收回", LegacyAppWrite, routes(
						rt(mPost, pApp+"/vip/grant"), rt(mPost, pApp+"/vip/grant/preview"), rt(mPost, pApp+"/vip/revoke"),
						rt(mPost, pApp+"/vip/trial/claims"), rt(mDelete, pApp+"/vip/trial/claims/:userId"),
					), danger()),
				)...,
			),
			menu("app.card_key", "卡密", "ticket", ScopeAppOnly,
				appStd("app:card_key", "/card-keys", true,
					act("app:card_key:export", "导出", LegacyAppRead, routes(rt(mRead, pApp+"/card-keys/batches/:batchId/export"))),
					act("app:card_key:extract", "提取", LegacyAppWrite, routes(rt(mPost, pApp+"/card-keys/batches/:batchId/extract")), danger()),
					act("app:card_key:distribute", "分发", LegacyAppWrite, routes(rt(mPost, pApp+"/card-keys/codes/distribute"))),
					act("app:card_key:redeem", "代用户兑换", LegacyAppWrite, routes(rt(mPost, pApp+"/card-keys/redeem"))),
					act("app:card_key:remove", "批量删除", LegacyAppWrite, routes(rt(mPost, pApp+"/card-keys/codes/delete")), danger()),
				)...,
			),
			menu("app.signin", "签到奖励", "gift", ScopeAppOnly,
				act("app:signin:read", "查看", LegacyAppRead, routes(
					under(mRead, pApp+"/signin"), rt(mRead, pApp+"/signin-reward"), rt(mRead, pApp+"/auto-sign"),
				)),
				act("app:signin:write", "编辑", LegacyAppWrite, routes(
					under(mWrite, pApp+"/signin-reward"), rt(mWrite, pApp+"/auto-sign"),
				)),
			),
			menu("app.points_store", "积分商城", "shopping-bag", ScopeAppOnly,
				appStd("app:points_store", "/points-store", true,
					act("app:points_store:fulfill", "兑换处理", LegacyAppWrite, routes(under(mPost, pApp+"/points-store/redemptions"))),
				)...,
			),
			menu("service.points", "积分", "coins", ScopeAppOnly,
				act(PermPointsRead, "查看", PermPointsRead, routes(rt(mAny, "/api/app/points/stats"))),
				act(PermPointsWrite, "调整", PermPointsWrite, routes(under(mAny, "/api/app/points")), danger()),
			),
			menu("app.level", "等级体系", "milestone", ScopeAppOnly,
				appStd("app:level", "/levels", false,
					act("app:level:adjust", "调整用户等级", LegacyAppWrite, routes(
						rt(mPost, pApp+"/levels/users/:userId/adjust"), rt(mPost, pApp+"/levels/users/adjust"),
					), danger()),
					act("app:level:reward", "奖励补发", LegacyAppWrite, routes(under(mPost, pApp+"/levels/rewards"))),
				)...,
			),
			menu("app.growth", "成长规则", "sprout", ScopeAppOnly,
				appStd("app:growth", "/growth", true)...,
			),
			menu("app.invite", "邀请", "user-plus", ScopeAppOnly,
				appStd("app:invite", "/invite", true,
					act("app:invite:settle", "结算与撤销", LegacyAppWrite, routes(under(mPost, pApp+"/invite/relations"))),
				)...,
			),
			menu("app.promo", "推广活动", "megaphone", ScopeAppOnly,
				appStd("app:promo", "/promo", true,
					act("app:promo:review", "审核提交", LegacyAppWrite, routes(rt(mPost, pApp+"/promo/submissions/review"))),
				)...,
			),
			menu("app.leaderboard", "排行榜", "trophy", ScopeAppOnly,
				appStd("app:leaderboard", "/leaderboard", true,
					act("app:leaderboard:settle", "结算", LegacyAppWrite, routes(
						rt(mPost, pApp+"/leaderboard/settle"), rt(mPost, pApp+"/leaderboard/snapshots/:snapshotId/retry"),
					)),
				)...,
			),
			menu("app.lottery", "抽奖", "dices", ScopeAppOnly,
				appStd("app:lottery", "/lottery", true,
					act("app:lottery:fortune", "运势调控", LegacyAppWrite, routes(
						under(mWrite, pApp+"/lottery/activities/:activityId/fortune"),
					), danger()),
					act("app:lottery:fulfill", "奖品发放", LegacyAppWrite, routes(rt(mPost, pApp+"/lottery/draws/:drawId/fulfill"))),
					act("app:lottery:ticket", "调整抽奖次数", LegacyAppWrite, routes(rt(mPost, pApp+"/lottery/tickets/users/:userId/adjust")), danger()),
				)...,
			),
			menu("app.item_grant", "物品发放", "package-plus", ScopeAppOnly,
				appStd("app:item_grant", "/item-grants", true,
					act("app:item_grant:run", "执行与取消", LegacyAppWrite, routes(
						rt(mPost, pApp+"/item-grants/rules/:ruleId/run"), rt(mPost, pApp+"/item-grants/runs/:runId/cancel"),
					), danger()),
					act("app:item_grant:fulfill", "补发与重试", LegacyAppWrite, routes(under(mPost, pApp+"/item-grants/records"))),
				)...,
			),
			menu("app.rewarded_ad", "激励广告", "monitor-play", ScopeAppOnly,
				appStd("app:rewarded_ad", "/rewarded-ads", false,
					act("app:rewarded_ad:grant", "补发奖励", LegacyAppWrite, routes(rt(mPost, pApp+"/rewarded-ads/views/:viewId/grant"))),
				)...,
			),
			menu("app.ad_policy", "广告服务", "shield-half", ScopeAppOnly,
				appStd("app:ad_policy", "/ad-policy", false)...,
			),
			menu("app.account_deletion", "账号注销", "user-x", ScopeAppOnly,
				act("app:user:deletion:execute", "立即执行", LegacyAppUserWrite,
					routes(rt(mPost, pApp+"/account-deletion/requests/:requestId/execute")), danger()),
				act("app:user:deletion:settings", "修改设置", LegacyAppUserWrite, routes(rt(mPut, pApp+"/account-deletion/settings"))),
				act("app:user:deletion:release", "解除重新注册限制", LegacyAppUserWrite, routes(under(mDelete, pApp+"/account-deletion/tombstones"))),
				act("app:user:deletion:read", "查看", LegacyAppUserRead, routes(under(mRead, pApp+"/account-deletion"))),
				act("app:user:deletion:review", "审核", LegacyAppUserWrite, routes(under(mWrite, pApp+"/account-deletion"))),
			),
		),

		dir("users", "应用用户", "users-round", ScopeAppOnly,
			menu("app.user", "用户", "users-round", ScopeAppOnly,
				act("app:user:export", "导出", LegacyAppUserRead, routes(rt(mRead, pApp+"/users/export"))),
				act("app:user:status", "启用与停用", LegacyAppUserWrite, routes(
					rt(mPut, pApp+"/users/:userId/status"), rt(mPut, pApp+"/users/status/batch"),
				)),
				act("app:user:ban", "封禁与解封", LegacyAppUserWrite, routes(
					under(mPost, pApp+"/users/:userId/bans"), rt(mPost, pApp+"/users/bans/batch"),
				), danger()),
				act("app:user:password", "重置密码", LegacyAppUserWrite, routes(rt(mPost, pApp+"/users/:userId/reset-password")), danger()),
				act("app:user:session", "下线会话", LegacyAppUserWrite, routes(
					rt(mPost, pApp+"/users/:userId/revoke-sessions"), under(mWrite, pApp+"/users/:userId/sessions"),
				)),
				act("app:user:delete", "删除与注销", LegacyAppUserWrite, routes(
					rt(mDelete, pApp+"/users/:userId"), rt(mPost, pApp+"/users/:userId/account-deletion"),
				), danger()),
				act("app:user:asset", "资产冲正", LegacyAppUserWrite, routes(under(mPost, pApp+"/users/:userId/points")), danger()),
				act("app:user:cloud", "云存储数据", LegacyAppUserWrite, routes(under(mWrite, pApp+"/users/:userId/cloud-storage"))),
				act("app:user:info:read", "查看", LegacyAppUserRead, routes(
					under(mRead, pApp+"/users"),
					under(mRead, pApp+"/oauth-bindings"), under(mRead, pApp+"/channels/:cid/users"),
				)),
				act("app:user:edit", "编辑资料", LegacyAppUserWrite, routes(
					under(mWrite, pApp+"/users"), under(mWrite, pApp+"/oauth-bindings"), under(mWrite, pApp+"/channels/:cid/users"),
				)),
			),
			menu("app.ban_appeal", "封禁申诉", "scale", ScopeAppOnly,
				act("app:user:appeal:read", "查看", LegacyAppUserRead, routes(under(mRead, pApp+"/ban-appeals"))),
				act("app:user:appeal:review", "处理", LegacyAppUserWrite, routes(under(mWrite, pApp+"/ban-appeals"))),
			),
			menu("app.channel", "渠道", "split", ScopeAppOnly,
				appStd("app:channel", "/channels", true)...,
			),
			menu("app.notification", "通知", "bell", ScopeAppOnly,
				act(PermAppNotificationRead, "查看", PermAppNotificationRead, routes(under(mRead, pApp+"/notifications"))),
				act(PermAppNotifWrite, "发送与删除", PermAppNotifWrite, routes(under(mAny, pApp+"/notifications"))),
			),
		),

		dir("content", "内容", "gallery-horizontal-end", ScopeAppOnly,
			menu("content.banner", "Banner", "image", ScopeAppOnly,
				act(PermContentBannerRead, "查看", PermContentBannerRead, routes(under(mRead, pApp+"/banners"))),
				act(PermContentBannerWrite, "编辑", PermContentBannerWrite, routes(under(mAny, pApp+"/banners"))),
			),
			menu("content.notice", "公告", "megaphone", ScopeAppOnly,
				act(PermContentNoticeRead, "查看", PermContentNoticeRead, routes(under(mRead, pApp+"/notices"))),
				act(PermContentNoticeWrite, "编辑", PermContentNoticeWrite, routes(under(mAny, pApp+"/notices"))),
			),
		),

		dir("release", "发布与版本", "package-check", ScopeAppOnly,
			menu("version.release", "发布中心", "rocket", ScopeAppOnly,
				act("version:release:read", "查看", LegacyVersionRead, routes(
					under(mRead, pApp+"/releases"),
					rt(mPost, pApp+"/releases/simulate"), rt(mPost, pApp+"/releases/notes/preview"),
				)),
				act("version:release:publish", "发布与回滚", LegacyVersionWrite, routes(
					rt(mPost, pApp+"/releases/:rid/publish"), rt(mPost, pApp+"/releases/:rid/pause"),
					rt(mPost, pApp+"/releases/:rid/resume"), rt(mPost, pApp+"/releases/:rid/revoke"),
					rt(mPut, pApp+"/releases/:rid/rollout"),
				), danger()),
				act("version:release:delete", "删除", LegacyVersionWrite, routes(under(mDelete, pApp+"/releases"))),
				act("version:release:write", "编辑", LegacyVersionWrite, routes(under(mWrite, pApp+"/releases"))),
			),
			menu("version.control", "版本管控", "shield-check", ScopeAppOnly,
				act("version:control:read", "查看", LegacyVersionRead, routes(
					under(mRead, pApp+"/client-control"),
					rt(mPost, pApp+"/client-control/simulate"),
				)),
				act("version:control:secret", "轮换密钥", LegacyVersionWrite, routes(rt(mPost, pApp+"/client-control/keys/:kind/rotate")), danger()),
				act("version:control:device", "删除设备记录", LegacyVersionWrite, routes(under(mDelete, pApp+"/client-control/devices"))),
				act("version:control:write", "编辑规则与设置", LegacyVersionWrite, routes(under(mWrite, pApp+"/client-control"))),
			),
			menu("version.legacy", "版本管理（旧版接口）", "history", ScopeAppOnly,
				act("version:legacy:read", "查看", LegacyVersionRead, routes(
					rt(mAny, "/api/admin/app/version/list"), rt(mAny, "/api/admin/app/version/detail"),
					rt(mAny, "/api/admin/app/version/stats"), rt(mAny, "/api/admin/app/version/channel/list"),
					rt(mAny, "/api/admin/app/version/channel/detail"), rt(mAny, "/api/admin/app/version/channel/users"),
					rt(mAny, "/api/admin/app/version/channel/preview-match"),
				)),
				act("version:legacy:write", "编辑", LegacyVersionWrite, routes(under(mAny, "/api/admin/app/version"))),
			),
			menu("app.version", "应用版本", "git-branch", ScopeAppOnly,
				appStd("app:version", "/versions", true,
					act("app:version:publish", "发布与撤回", LegacyAppWrite, routes(
						rt(mPost, pApp+"/versions/:vid/publish"), rt(mPost, pApp+"/versions/:vid/revoke"),
					)),
				)...,
			),
		),

		dir("functions", "远程函数", "code-2", ScopeAppOnly,
			menu("app.function", "远程函数", "code-2", ScopeAppOnly,
				act("app:function:invoke", "调用", LegacyAppWrite, routes(rt(mPost, pApp+"/functions/:functionName/invoke"))),
				act("app:function:deploy", "切换生效版本", LegacyAppWrite, routes(rt(mPost, pApp+"/functions/:functionName/versions/:version/activate")), danger()),
				act("app:function:key", "调用密钥", LegacyAppWrite, routes(under(mWrite, pApp+"/function-keys")), danger()),
				act("app:function:kv", "清理 KV", LegacyAppWrite, routes(under(mWrite, pApp+"/function-kv")), danger()),
				act("app:function:read", "查看", LegacyAppRead, routes(
					under(mRead, pApp+"/functions"), under(mRead, pApp+"/function-catalog"),
					under(mRead, pApp+"/function-keys"), under(mRead, pApp+"/function-kv"),
				)),
				act("app:function:delete", "删除", LegacyAppWrite, routes(under(mDelete, pApp+"/functions"))),
				act("app:function:write", "编辑与测试", LegacyAppWrite, routes(under(mWrite, pApp+"/functions"))),
			),
		),

		dir("commerce", "交易", "wallet", ScopeAppOnly,
			menu("app.commerce", "交易设置", "store", ScopeAppOnly,
				act("app:commerce:read", "查看", LegacyAppRead, routes(under(mRead, pApp+"/commerce"))),
				act("app:commerce:write", "编辑", LegacyAppWrite, routes(rt(mWrite, pApp+"/commerce"))),
			),
			menu("app.wallet", "钱包", "wallet", ScopeAppOnly,
				act("app:wallet:read", "查看", LegacyAppRead, routes(under(mRead, pApp+"/wallet"))),
				act("app:wallet:adjust", "调账", LegacyAppWrite, routes(rt(mPost, pApp+"/wallet/adjust")), danger()),
				act("app:wallet:receipt", "回单", LegacyAppWrite, routes(under(mPost, pApp+"/wallet/receipt"))),
			),
		),

		dir("tickets", "工单与反馈", "life-buoy", ScopeBoth,
			menu("ticket.work", "工单处理", "life-buoy", ScopeBoth,
				act(PermTicketRead, "查看", PermTicketRead, nil, desc("全局授予可见全部工单，应用授予可见该应用工单")),
				act(PermTicketWrite, "编辑", PermTicketWrite, nil),
				act(PermTicketReply, "回复", PermTicketReply, nil),
				act(PermTicketInternal, "内部备注", PermTicketInternal, nil),
				act(PermTicketAssign, "指派", PermTicketAssign, nil),
				act(PermTicketClose, "结单与重开", PermTicketClose, nil),
				act(PermTicketDelete, "删除", PermTicketDelete, nil, danger()),
				act(PermTicketExport, "导出", PermTicketExport, routes(rt(mRead, pAdmin+"/tickets/export"))),
			),
			menu("ticket.config", "工单配置", "settings-2", ScopeBoth,
				act(PermTicketManage, "分类、SLA、处理组与快捷回复", PermTicketManage, routes(
					under(mAny, pAdmin+"/tickets/categories"), under(mAny, pAdmin+"/tickets/groups"),
					under(mAny, pAdmin+"/tickets/sla-policies"), under(mAny, pAdmin+"/tickets/quick-replies"),
				)),
			),
		),

		// ════════════════ 平台 ════════════════
		dir("platform", "平台治理", "gavel", ScopePlatform,
			menu("platform.governance", "应用治理", "gavel", ScopePlatform,
				act(PermPlatformAppRead, "全站总览", PermPlatformAppRead, routes(
					under(mRead, pPlatform), rt(mAny, pAdmin+"/apps/password-policy/templates"),
					rt(mAny, pAdmin+"/apps/signin-reward/templates"), rt(mAny, "/api/app/password-policy/templates"),
				)),
				act(PermPlatformAppGovern, "治理处置", PermPlatformAppGovern, routes(under(mAny, pPlatform)),
					desc("限制、冻结、停运与解除")),
				act(PermPlatformAppDanger, "危险处置", PermPlatformAppDanger, routes(
					rt(mAny, pPlatform+"/apps/:appkey/revoke-sessions"),
				), danger(), desc("封禁、归档、强制下线全站会话")),
				act(PermPlatformAppealReview, "申诉审批", PermPlatformAppealReview, routes(
					rt(mAny, pPlatform+"/governance/appeals/:appealId/review"),
				)),
			),
			menu("platform.storage", "平台存储", "hard-drive", ScopePlatform,
				act(PermPlatformStorageRead, "查看", PermPlatformStorageRead, routes(
					rt(mAny, pPlatform+"/storage-config/list"), rt(mAny, pPlatform+"/storage-config/detail"),
					under(mRead, pPlatform+"/storage"),
				)),
				act(PermPlatformStorageWrite, "编辑", PermPlatformStorageWrite, routes(
					under(mAny, pPlatform+"/storage-config"), under(mAny, pPlatform+"/storage"),
				)),
			),
		),

		dir("system", "系统管理", "settings-2", ScopePlatform,
			menu("system.admin", "管理员", "shield-check", ScopePlatform,
				act(PermSystemAdminRead, "查看", sys, routes(
					rt(mRead, pSystem+"/admins"), rt(mRead, pSystem+"/admins/online"),
					rt(mRead, pSystem+"/admins/:adminId"), rt(mRead, pSystem+"/authz/admins/:adminId/grants"),
				)),
				act(PermSystemAdminCreate, "新增", sys, routes(rt(mPost, pSystem+"/admins"))),
				act(PermSystemAdminEdit, "角色授权", sys, routes(rt(mPut, pSystem+"/admins/:adminId/access")),
					desc("只能授予自己持有的权限")),
				act(PermSystemAdminStatus, "启用与停用", sys, routes(rt(mPut, pSystem+"/admins/:adminId/status"))),
				act(PermSystemAdminGrant, "个人授权", sys, routes(rt(mPut, pSystem+"/authz/admins/:adminId/grants")),
					desc("直接授予或禁止到某个管理员")),
			),
			menu("system.session", "管理员会话", "monitor", ScopePlatform,
				act(PermSystemSessionRead, "查看", sys, routes(rt(mRead, pSystem+"/sessions"), rt(mRead, pSystem+"/admins/:adminId/sessions"))),
				act(PermSystemSessionRevoke, "强制下线", sys, routes(
					rt(mPost, pSystem+"/sessions/:sessionId/revoke"), rt(mPost, pSystem+"/admins/:adminId/force-logout"),
				)),
			),
			menu("system.role", "角色权限", "key-round", ScopePlatform,
				act(PermSystemRoleRead, "查看", sys, routes(
					under(mRead, pSystem+"/roles"),
					under(mRead, pSystem+"/authz"), rt(mPost, pSystem+"/authz/explain"),
				)),
				act(PermSystemRoleCreate, "新增", sys, routes(rt(mPost, pSystem+"/roles"), rt(mPost, pSystem+"/roles/:roleKey/copy"))),
				act(PermSystemRoleEdit, "编辑", sys, routes(
					under(mWrite, pSystem+"/roles/:roleKey"),
					rt(mPost, pSystem+"/authz/roles/override"),
				), desc("只能授予自己持有的权限")),
				act(PermSystemRoleDelete, "删除", sys, routes(rt(mDelete, pSystem+"/roles/:roleKey")), danger()),
				act(PermSystemRoleReload, "重载策略", sys, routes(rt(mPost, pSystem+"/authz/reload"))),
			),
			menu("system.temp_perm", "临时权限", "timer", ScopePlatform,
				act(PermSystemTempPermRead, "查看", sys, routes(rt(mRead, pSystem+"/temp-permissions"))),
				act(PermSystemTempPermGrant, "授予", sys, routes(rt(mPost, pSystem+"/temp-permissions"))),
				act(PermSystemTempPermRevoke, "撤销", sys, routes(rt(mPost, pSystem+"/temp-permissions/:permId/revoke"))),
			),
			menu("system.delegation", "代理授权", "handshake", ScopePlatform,
				act(PermSystemDelegationRead, "查看", sys, routes(rt(mRead, pSystem+"/delegations"))),
				act(PermSystemDelegationCreate, "创建", sys, routes(rt(mPost, pSystem+"/delegations"))),
				act(PermSystemDelegationRevoke, "撤销", sys, routes(rt(mPost, pSystem+"/delegations/:delegationId/revoke"))),
			),
			menu("system.config", "平台设置", "sliders-horizontal", ScopePlatform,
				act("system:config:read", "查看", sys, routes(rt(mRead, pSystem+"/settings"), rt(mRead, pSystem+"/runtime"))),
				act("system:config:write", "编辑", sys, routes(rt(mPut, pSystem+"/settings"), rt(mPost, pSystem+"/captcha/preview")), danger()),
				act("system:identity_source:test", "身份源连通测试", sys, routes(
					rt(mPost, pSystem+"/ldap/test"), rt(mPost, pSystem+"/oidc/test"), rt(mPost, pSystem+"/saml/test"),
				)),
			),
			menu("system.legal", "法律文本", "file-check-2", ScopePlatform,
				act(PermSystemLegalManage, "管理", PermSystemLegalManage, routes(under(mAny, pSystem+"/legal"))),
			),
			menu("system.template", "消息模板", "message-square-text", ScopePlatform,
				act("system:template:delete", "删除", sys, routes(under(mDelete, pSystem+"/templates"))),
				act("system:template:read", "查看", sys, routes(under(mRead, pSystem+"/templates"), rt(mPost, pSystem+"/templates/:code/preview"))),
				act("system:template:write", "编辑", sys, routes(under(mWrite, pSystem+"/templates"))),
			),
			menu("system.announcement", "平台公告", "megaphone", ScopePlatform,
				sysStd("system:announcement", "/announcements", true,
					act("system:announcement:publish", "发布与归档", sys, routes(
						rt(mPost, pSystem+"/announcements/:id/publish"), rt(mPost, pSystem+"/announcements/:id/archive"),
					)),
				)...,
			),
			menu("system.banner", "平台 Banner", "gallery-horizontal-end", ScopePlatform,
				sysStd("system:banner", "/banners", true,
					act("system:banner:bulk_delete", "批量删除", sys, routes(rt(mPost, pSystem+"/banners/bulk-delete")), danger()),
				)...,
			),
			menu("system.audit_log", "审计日志", "scroll-text", ScopePlatform,
				act("system:audit_log:export", "导出", sys, routes(rt(mRead, pSystem+"/audit-logs/export"))),
				act("system:audit_log:read", "查看", sys, routes(under(mRead, pSystem+"/audit-logs"))),
			),
			menu("system.plugin", "插件", "puzzle", ScopePlatform,
				sysStd("system:plugin", "/plugins", true,
					act("system:plugin:toggle", "启停", sys, routes(
						rt(mPost, pSystem+"/plugins/:id/enable"), rt(mPost, pSystem+"/plugins/:id/disable"),
					)),
				)...,
			),
			menu("system.native_plugin", "内置插件", "package", ScopePlatform,
				act("system:native_plugin:read", "查看", sys, routes(rt(mRead, pSystem+"/native-plugins"))),
				act("system:native_plugin:manage", "启停与重启", sys, routes(under(mPost, pSystem+"/native-plugins")), danger()),
			),
			menu("system.device_name", "设备型号库", "smartphone", ScopePlatform,
				sysStd("system:device_name", "/device-marketing-names", true)...,
			),
		),

		dir("org", "组织架构", "building-2", ScopePlatform,
			menu("org.structure", "组织与部门", "building-2", ScopePlatform,
				act(PermOrgCreate, "创建组织", PermOrgCreate, routes(under(mPost, pSystem+"/organizations"))),
				act(PermOrgWrite, "修改与删除组织", PermOrgWrite, routes(under(mAny, pSystem+"/organizations")), danger()),
				act(PermOrgDeptRead, "查看部门", PermOrgDeptRead, nil),
				act(PermOrgDeptWrite, "管理部门", PermOrgDeptWrite, nil),
			),
			menu("org.member", "成员", "users-2", ScopePlatform,
				act(PermOrgMemberRead, "查看成员", PermOrgMemberRead, nil),
				act(PermOrgMemberWrite, "管理成员", PermOrgMemberWrite, nil),
				act(PermOrgMemberInvite, "邀请成员", PermOrgMemberInvite, nil),
			),
			menu("org.flow", "审批与邀请", "check-circle", ScopePlatform,
				act("system:org_flow:read", "查看", sys, routes(
					under(mRead, pSystem+"/org-approvals"), under(mRead, pSystem+"/org-invitations"),
					rt(mRead, pSystem+"/org-metadata"),
				)),
				act("system:org_flow:handle", "处理", sys, routes(
					under(mPost, pSystem+"/org-approvals"), under(mPost, pSystem+"/org-invitations"),
				)),
			),
		),

		dir("identity", "用户主档", "contact", ScopePlatform,
			menu("identity.master", "用户主档", "contact", ScopePlatform,
				act("system:identity:read", "查看", sys, routes(
					under(mRead, pSystem+"/user-master"), rt(mPost, pSystem+"/user-master/lists/check"),
				)),
				act("system:identity:merge", "合并与同步", sys, routes(
					under(mPost, pSystem+"/user-master/merges"), under(mPost, pSystem+"/user-master/sync"),
				), danger()),
				act("system:identity:segment", "分群", sys, routes(under(mWrite, pSystem+"/user-master/segments"))),
				act("system:identity:tag", "标签", sys, routes(under(mWrite, pSystem+"/user-master/tags"))),
				act("system:identity:list", "名单", sys, routes(
					under(mWrite, pSystem+"/user-master/lists"), under(mWrite, pSystem+"/user-master/user-lists"),
				)),
				act("system:identity:lifecycle", "申诉与注销", sys, routes(
					under(mWrite, pSystem+"/user-master/appeals"), under(mWrite, pSystem+"/user-master/deactivations"),
				)),
				act("system:identity:write", "编辑档案", sys, routes(under(mWrite, pSystem+"/user-master"))),
			),
		),

		dir("security", "安全与风控", "shield-alert", ScopePlatform,
			menu("security.firewall", "防火墙", "brick-wall", ScopePlatform,
				act("system:firewall:read", "查看", sys, routes(under(mRead, pSystem+"/firewall"))),
				act("system:firewall:log_clean", "清理日志", sys, routes(rt(mDelete, pSystem+"/firewall/logs")), danger()),
				act("system:firewall:fence", "地理围栏", sys, routes(
					under(mWrite, pSystem+"/firewall/geo-fences"),
				)),
				act("system:firewall:ban", "封禁 IP 与地域", sys, routes(
					under(mWrite, pSystem+"/firewall/bans"), under(mWrite, pSystem+"/firewall/geo-bans"),
				), danger()),
			),
			menu("security.geo", "地理分析", "map", ScopePlatform,
				act("system:geo:read", "查看", sys, routes(under(mRead, pSystem+"/geo"))),
			),
			menu("security.risk", "风控平台", "shield-alert", ScopePlatform,
				act("system:risk:read", "查看", sys, routes(under(mRead, pSystem+"/risk"))),
				act("system:risk:write", "规则与处置", sys, routes(under(mWrite, pSystem+"/risk")), danger()),
			),
			menu("security.risk_ops", "风控运维", "wrench", ScopePlatform,
				act("system:risk_ops:read", "查看", sys, routes(
					under(mRead, pSystem+"/risk-ops"), rt(mPost, pSystem+"/risk-ops/inspect"),
				)),
				act("system:risk_ops:manage", "清理与解除", sys, routes(under(mWrite, pSystem+"/risk-ops")), danger()),
			),
			menu("security.platform_ban", "平台封禁", "ban", ScopePlatform,
				act("system:platform_ban:read", "查看", sys, routes(
					under(mRead, pSystem+"/platform-bans"), under(mRead, pSystem+"/ban-appeals"),
				)),
				act("system:platform_ban:revoke", "解除", sys, routes(under(mPost, pSystem+"/platform-bans"))),
			),
		),

		dir("ops", "平台运维", "server-cog", ScopePlatform,
			menu("ops.storage_center", "存储中心", "database", ScopePlatform,
				act("system:storage:purge", "彻底删除与清空回收站", sys, routes(
					rt(mDelete, pSystem+"/storage/objects/:objectId/permanent"), rt(mPost, pSystem+"/storage/trash/cleanup"),
				), danger()),
				act("system:storage:read", "查看", sys, routes(under(mRead, pSystem+"/storage"))),
				act("system:storage:delete", "删除", sys, routes(under(mDelete, pSystem+"/storage"))),
				act("system:storage:write", "编辑", sys, routes(under(mWrite, pSystem+"/storage"))),
			),
			menu("ops.egress", "出海网关", "waves", ScopePlatform,
				act("system:egress:read", "查看", sys, routes(
					under(mRead, pSystem+"/egress"), rt(mPost, pSystem+"/egress/explain"),
					rt(mPost, pSystem+"/egress/graph/validate"), rt(mPost, pSystem+"/egress/graph/export"),
				)),
				act("system:egress:apply", "应用、回滚与重置", sys, routes(
					rt(mPost, pSystem+"/egress/graph/apply"), rt(mPost, pSystem+"/egress/graph/rollback"),
					rt(mPost, pSystem+"/egress/reset"), rt(mPut, pSystem+"/egress"),
				), danger(), desc("影响全部出站流量")),
				act("system:egress:core", "核心运维", sys, routes(under(mWrite, pSystem+"/egress/mihomo")), danger()),
				act("system:egress:write", "编辑草稿与测试", sys, routes(under(mWrite, pSystem+"/egress"))),
			),
			menu("ops.traffic", "流量管理", "activity", ScopePlatform,
				act("system:traffic:read", "查看", sys, routes(
					under(mRead, pSystem+"/traffic"), rt(mPost, pSystem+"/traffic/validate"),
					rt(mPost, pSystem+"/traffic/simulate"),
				)),
				act("system:traffic:write", "编辑规则与回滚", sys, routes(under(mWrite, pSystem+"/traffic")), danger()),
			),
			menu("ops.database", "数据库", "database", ScopePlatform,
				act("system:database:read", "查看", sys, routes(under(mRead, pSystem+"/database"))),
				act("system:database:session", "取消与终止连接", sys, routes(under(mPost, pSystem+"/database/sessions")), danger()),
				act("system:database:manage", "刷新与预热", sys, routes(under(mWrite, pSystem+"/database"))),
			),
			menu("ops.memory", "内存", "cpu", ScopePlatform,
				act("system:memory:read", "查看", sys, routes(under(mRead, pSystem+"/memory"))),
				act("system:memory:manage", "回收与调参", sys, routes(under(mWrite, pSystem+"/memory")), danger()),
			),
			menu("ops.crashlog", "服务端崩溃日志", "bug", ScopePlatform,
				act("system:crashlog:read", "查看", sys, routes(under(mRead, pSystem+"/crashlogs"))),
				act("system:crashlog:delete", "删除", sys, routes(under(mDelete, pSystem+"/crashlogs"))),
			),
			menu("ops.online", "在线监控", "radio", ScopePlatform,
				act(PermSystemOnlineRead, "查看", sys, routes(under(mRead, pSystem+"/online"))),
			),
		),

		dir("notify", "通知出口", "bell-dot", ScopePlatform,
			menu("notify.channel", "渠道与订阅", "bell-dot", ScopePlatform,
				act(PermNotifyChannelRead, "查看", PermNotifyChannelRead, routes(under(mRead, pAdmin+"/notify"))),
				act(PermNotifyChannelWrite, "管理", PermNotifyChannelWrite, routes(under(mAny, pAdmin+"/notify")),
					desc("含 IM 凭据")),
				act(PermNotifyTest, "测试发送", PermNotifyTest, nil),
			),
			menu("notify.delivery", "投递记录", "send", ScopePlatform,
				act(PermNotifyDeliveryRead, "查看", PermNotifyDeliveryRead, routes(under(mRead, pAdmin+"/notify/deliveries"))),
			),
		),
	}
}

// openRoutes 不要求权限点、只要求已登录的管理端接口（细粒度在 service 层，或是不含租户数据的静态目录）。
var openRoutes = []RouteRule{
	{Methods: readMethods, Pattern: pAdmin + "/mcp/*", Note: "自述接口，内容按调用者的权限与密钥范围裁剪"},
	{Pattern: pAdmin + "/dashboard", Note: "任意已登录管理员可见，内容按身份裁剪"},
	{Pattern: pPlatform + "/catalog", Note: "状态/能力目录是编译进二进制的静态表"},
	{Methods: readMethods, Pattern: pAdmin + "/tickets/categories/*"},
	{Methods: readMethods, Pattern: pAdmin + "/tickets/groups/*"},
	{Methods: readMethods, Pattern: pAdmin + "/tickets/sla-policies/*"},
	{Methods: readMethods, Pattern: pAdmin + "/tickets/quick-replies/*"},
	{Pattern: pAdmin + "/tickets/*", Note: "列表/详情/回复/指派等：进模块即可，细粒度在 TicketService"},
	{Pattern: pAdmin + "/notify/catalog", Note: "静态目录"},
	{Methods: readMethods, Pattern: pSystem + "/organizations/*", Note: "组织可见范围由 OrgAccessControl 推导"},
	{Pattern: pSystem + "/email/providers", Note: "邮件服务商静态目录"},
	{Pattern: pSystem + "/email/scenes", Note: "邮件场景静态目录"},
	{Pattern: pSystem + "/ai/providers", Note: "AI 供应商静态目录"},
	{Methods: readMethods, Pattern: pSystem + "/authz/catalog", Note: "权限树是编译进二进制的静态目录"},
	{Pattern: pAdmin + "/oauth-providers/templates", Note: "第三方登录渠道模板"},
	{Pattern: pAdmin + "/app/payment-config/methods", Note: "支付渠道静态目录"},
	{Pattern: "/api/app/workflow/node-types", Note: "工作流节点类型目录"},
	{Pattern: "/api/app/workflow/engine/status", Note: "Temporal 连通性"},
	{Pattern: pAdmin + "/app/email-config/providers", Note: "邮件服务商静态目录"},
	{Pattern: pAdmin + "/app/email-config/scenes", Note: "邮件场景静态目录"},
	{Pattern: pAdmin + "/app/storage-config/providers", Note: "存储提供商静态目录"},
	{Methods: mPost, Pattern: pAdmin + "/apps", Note: "自助创建：闸门在 AdminService.EnsureCanCreateApp"},
}

// appScopedPrefixes 这些前缀下的接口按应用作用域判定（需要解析 appid）。
var appScopedPrefixes = []string{
	pApp,
	"/api/admin/app/",
	"/api/app/password-policy/",
	"/api/app/points/",
	"/api/app/workflow/",
}

// globalScopeExceptions 落在应用前缀下却按平台作用域判定的接口。
var globalScopeExceptions = []string{
	"/api/app/password-policy/templates",
}
