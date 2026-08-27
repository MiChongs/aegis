package authz

import (
	admindomain "aegis/internal/domain/admin"
)

// 内置角色。
//
// 这些定义是**代码给的**，每次启动整组重刷进 authz_policies（source=builtin），
// 于是"升级后给 app_admin 加了一个权限点"能到达所有既有部署 ——
// 把它们做成纯 DB 数据就没有这个性质了，新权限点永远到不了老部署。
//
// 部署方要调整内置角色不是改这里，而是加一条 source=override 的策略：
// allow 是扩权、deny 是砍权，两者都不会在下次启动时被冲掉。
// 这条分工线让"跟随版本升级"和"按部署定制"同时成立。
//
// 注意 Permissions 里现在**可以写前缀通配**（ticket:* / *），
// 一条顶一片；旧实现是字符串全等，每加一个权限点都要在每个角色里补一行。

const (
	RoleSuperAdmin    = "super_admin"
	RolePlatformAdmin = "platform_admin"
	RoleAppAdmin      = "app_admin"
	RoleAppOperator   = "app_operator"
	RoleAppAuditor    = "app_auditor"
	RoleTicketAgent   = "ticket_agent"
	RoleAppViewer     = "app_viewer"
)

// BuiltinRoles 内置角色定义表。
func BuiltinRoles() map[string]admindomain.RoleDefinition {
	return map[string]admindomain.RoleDefinition{
		RoleSuperAdmin: {
			Key:         RoleSuperAdmin,
			Name:        "超级管理员",
			Description: "平台最高管理权限",
			Level:       100,
			Scope:       "global",
			// 通配到底。以前这里是空列表 + 判定处一个 `if IsSuperAdmin` 短路，
			// 于是"超管有哪些权限"在权限矩阵里是一片空白，得靠前端特判补上。
			// 现在它是一条真实策略，矩阵直接读得出来。
			// （运行时仍保留 IsSuperAdmin 短路：那是 DB 上的一列，每次请求现查，
			//   不受策略缓存影响，撤销超管身份立即生效。）
			Permissions: []string{AnyPermission},
		},
		RolePlatformAdmin: {
			Key:         RolePlatformAdmin,
			Name:        "平台管理员",
			Description: "全局平台与运维配置管理",
			Level:       90,
			Scope:       "global",
			Permissions: []string{
				// 平台治理：可冻结 / 限制 / 解除，但**不含**封禁与归档 ——
				// 那两档是不可逆感极强的动作，留给超管或显式授权的自定义角色。
				PermPlatformAppRead, PermPlatformAppGovern, PermPlatformAppealReview,
				PermPlatformStorageRead, PermPlatformStorageWrite,
				"system:settings:*", "system:user_setting:*",
				"app:*",
				"content:*",
				"audit:*",
				"storage:*",
				"workflow:*",
				"version:*",
				"site:*",
				"role_application:*",
				"points:*",
				"email:*",
				"ai:*",
				"payment:*",
				"ticket:*",
				PermNotifyChannelRead, PermNotifyDeliveryRead,
				"org:*",
			},
		},
		RoleAppAdmin: {
			Key:         RoleAppAdmin,
			Name:        "应用管理员",
			Description: "单应用全量管理权限（与平台管理员同级，仅限绑定应用）",
			Level:       70,
			Scope:       "app",
			Permissions: []string{
				PermSystemSettingsRead, PermSystemUserSettingRead, PermSystemUserSettingWrite,
				"app:*",
				"content:*",
				"audit:*",
				"storage:*",
				"workflow:*",
				"version:*",
				"site:*",
				"role_application:*",
				"points:*",
				"email:*",
				"payment:*",
				PermTicketRead, PermTicketWrite, PermTicketReply, PermTicketInternal,
				PermTicketAssign, PermTicketClose, PermTicketManage, PermTicketExport,
				PermNotifyChannelRead, PermNotifyDeliveryRead,
				PermOrgDeptRead, PermOrgMemberRead, PermOrgMemberInvite,
			},
		},
		RoleAppOperator: {
			Key:         RoleAppOperator,
			Name:        "应用运营管理员",
			Description: "运营、内容、用户与版本维护",
			Level:       60,
			Scope:       "app",
			Permissions: []string{
				PermAppRead, PermAppUserRead, PermAppUserWrite, PermAppNotificationRead, PermAppNotifWrite,
				"content:*",
				"audit:*",
				"points:*",
				"version:*",
				PermSiteRead, PermSiteWrite,
				PermWorkflowRead,
				PermEmailRead,
				PermPaymentRead,
				PermRoleApplicationRead,
				PermStorageRead,
				PermTicketRead, PermTicketWrite, PermTicketReply, PermTicketInternal,
				PermTicketAssign, PermTicketClose, PermTicketExport,
				PermOrgDeptRead, PermOrgMemberRead,
			},
		},
		RoleAppAuditor: {
			Key:         RoleAppAuditor,
			Name:        "应用审核管理员",
			Description: "审计、审核与只读分析权限",
			Level:       40,
			Scope:       "app",
			Permissions: []string{
				PermAppRead, PermAppUserRead, PermAppNotificationRead,
				PermContentBannerRead, PermContentNoticeRead,
				"audit:*",
				PermPointsRead,
				PermVersionRead,
				PermSiteRead, PermSiteAudit,
				PermWorkflowRead,
				PermEmailRead,
				PermPaymentRead,
				PermRoleApplicationRead, PermRoleApplicationReview,
				PermStorageRead,
				PermTicketRead, PermTicketReply, PermTicketInternal, PermTicketExport,
				PermOrgDeptRead, PermOrgMemberRead,
			},
		},
		// 工单处理专员：只给工单能力，不碰用户/内容/配置。
		// 用于把"特定人员"精确授权成客服角色；再把他们加进对应处理组即可限定受理范围。
		RoleTicketAgent: {
			Key:         RoleTicketAgent,
			Name:        "工单处理专员",
			Description: "仅工单处理权限，可结合处理组限定受理范围",
			Level:       30,
			Scope:       "app",
			Permissions: []string{
				PermTicketRead, PermTicketWrite, PermTicketReply, PermTicketInternal,
				PermTicketAssign, PermTicketClose, PermTicketExport,
				PermAppRead, PermAppUserRead,
			},
		},
		RoleAppViewer: {
			Key:         RoleAppViewer,
			Name:        "应用观察员",
			Description: "只读查看权限",
			Level:       20,
			Scope:       "app",
			Permissions: []string{
				PermAppRead, PermAppUserRead, PermAppNotificationRead,
				PermContentBannerRead, PermContentNoticeRead,
				"audit:*",
				PermPointsRead,
				PermVersionRead,
				PermSiteRead,
				PermWorkflowRead,
				PermEmailRead,
				PermPaymentRead,
				PermRoleApplicationRead,
				PermStorageRead,
				PermTicketRead,
				PermOrgDeptRead, PermOrgMemberRead,
			},
		},
	}
}

// BuiltinPolicies 把内置角色定义翻成落库的策略行。
func BuiltinPolicies() []PolicyRule {
	roles := BuiltinRoles()
	rules := make([]PolicyRule, 0, 128)
	for key, role := range roles {
		subject := RoleSubject(key)
		for _, permission := range role.Permissions {
			rules = append(rules, PolicyRule{
				PType:  "p",
				Values: []string{subject, AnyDomain, permission, EffectAllow},
				Source: SourceBuiltin,
				Owner:  subject,
			})
		}
	}
	return rules
}
