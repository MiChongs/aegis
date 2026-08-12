package authz

import (
	admindomain "aegis/internal/domain/admin"
)

// 权限点词汇表。
//
// 这份目录此前住在 internal/service/admin_service.go 里，与「角色定义」「判定逻辑」
// 挤在同一个 1400 行的文件中，而「路由 → 权限点」的映射又在 internal/middleware。
// 于是加一个权限点要改三个包，且没有任何机制保证三处对得上 ——
// 少改一处的表现不是编译错误，而是一条谁也点不动、或者谁都点得动的接口。
//
// 现在权限词汇、角色、路由映射、判定引擎同属一个包，彼此的一致性由本包的测试守住。

// 平台治理权限点。
//
// 与应用级权限点的区别是**判定作用域**：这些只在平台域（DomainForApp(nil)）下有意义 ——
// 一个只管着自己那个应用的管理员即便被授了 platform:app:govern，
// 也只能"治理自己"，那等于自己给自己解封。因此读它们的地方一律传 appID = nil。
const (
	PermPlatformAppRead      = "platform:app:read"
	PermPlatformAppGovern    = "platform:app:govern"
	PermPlatformAppDanger    = "platform:app:danger"
	PermPlatformAppealReview = "platform:appeal:review"
	PermPlatformStorageRead  = "platform:storage:read"
	PermPlatformStorageWrite = "platform:storage:write"
)

// 系统与应用权限点。
const (
	PermSystemAdminManage      = "system:admin:manage"
	PermSystemSettingsRead     = "system:settings:read"
	PermSystemSettingsWrite    = "system:settings:write"
	PermSystemUserSettingRead  = "system:user_setting:read"
	PermSystemUserSettingWrite = "system:user_setting:write"

	PermAppRead             = "app:read"
	PermAppWrite            = "app:write"
	PermAppUserRead         = "app:user:read"
	PermAppUserWrite        = "app:user:write"
	PermAppNotificationRead = "app:notification:read"
	PermAppNotifWrite       = "app:notification:write"

	PermContentBannerRead  = "content:banner:read"
	PermContentBannerWrite = "content:banner:write"
	PermContentNoticeRead  = "content:notice:read"
	PermContentNoticeWrite = "content:notice:write"

	PermAuditLoginRead   = "audit:login:read"
	PermAuditSessionRead = "audit:session:read"

	PermStorageRead  = "storage:read"
	PermStorageWrite = "storage:write"

	PermWorkflowRead  = "workflow:read"
	PermWorkflowWrite = "workflow:write"

	PermVersionRead  = "version:read"
	PermVersionWrite = "version:write"

	PermSiteRead  = "site:read"
	PermSiteWrite = "site:write"
	PermSiteAudit = "site:audit"

	PermRoleApplicationRead   = "role_application:read"
	PermRoleApplicationReview = "role_application:review"

	PermPointsRead  = "points:read"
	PermPointsWrite = "points:write"

	PermEmailRead  = "email:read"
	PermEmailWrite = "email:write"

	PermPaymentRead  = "payment:read"
	PermPaymentWrite = "payment:write"

	PermTicketRead     = "ticket:read"
	PermTicketWrite    = "ticket:write"
	PermTicketReply    = "ticket:reply"
	PermTicketInternal = "ticket:internal"
	PermTicketAssign   = "ticket:assign"
	PermTicketClose    = "ticket:close"
	PermTicketDelete   = "ticket:delete"
	PermTicketManage   = "ticket:manage"
	PermTicketExport   = "ticket:export"

	PermNotifyChannelRead  = "notify:channel:read"
	PermNotifyChannelWrite = "notify:channel:write"
	PermNotifyDeliveryRead = "notify:delivery:read"
	PermNotifyTest         = "notify:test"

	PermOrgCreate       = "org:create"
	PermOrgWrite        = "org:write"
	PermOrgDeptRead     = "org:dept:read"
	PermOrgDeptWrite    = "org:dept:write"
	PermOrgMemberRead   = "org:member:read"
	PermOrgMemberWrite  = "org:member:write"
	PermOrgMemberInvite = "org:member:invite"
)

// PermissionCatalog 返回全部权限点分组。
//
// 它同时驱动三件事：角色编辑器的勾选树、权限矩阵、以及拒绝文案里的中文名。
// 新增权限点只改这里 —— 分组信息缺失时前端会把它丢在"未分组"里，
// 而权限矩阵会少一行，两者都不会报错。
func PermissionCatalog() []admindomain.PermissionGroup {
	return []admindomain.PermissionGroup{
		{Key: "platform", Name: "平台治理", Permissions: []admindomain.Permission{
			{Code: PermPlatformAppRead, Name: "全站应用总览", Description: "跨应用查看治理状态与用量指标"},
			{Code: PermPlatformAppGovern, Name: "应用治理", Description: "限制 / 冻结 / 停运 / 解除，应用管理员无法自行撤销"},
			{Code: PermPlatformAppDanger, Name: "危险治理操作", Description: "封禁 / 归档 / 强制下线全站会话 / 删除被治理应用"},
			{Code: PermPlatformAppealReview, Name: "治理申诉审批"},
			{Code: PermPlatformStorageRead, Name: "平台存储配置查看"},
			{Code: PermPlatformStorageWrite, Name: "平台存储配置修改"},
		}},
		{Key: "system", Name: "系统管理", Permissions: []admindomain.Permission{
			{Code: PermSystemAdminManage, Name: "管理员管理"},
			{Code: PermSystemSettingsRead, Name: "系统设置查看"},
			{Code: PermSystemSettingsWrite, Name: "系统设置修改"},
			{Code: PermSystemUserSettingRead, Name: "用户设置查看"},
			{Code: PermSystemUserSettingWrite, Name: "用户设置修改"},
		}},
		{Key: "app", Name: "应用管理", Permissions: []admindomain.Permission{
			{Code: PermAppRead, Name: "应用信息查看"},
			{Code: PermAppWrite, Name: "应用信息修改", Description: "全局作用域下同时意味着「可无限制创建应用」，不受自助配额约束"},
			{Code: PermAppUserRead, Name: "应用用户查看"},
			{Code: PermAppUserWrite, Name: "应用用户管理"},
			{Code: PermAppNotificationRead, Name: "通知查看"},
			{Code: PermAppNotifWrite, Name: "通知管理"},
		}},
		{Key: "content", Name: "内容管理", Permissions: []admindomain.Permission{
			{Code: PermContentBannerRead, Name: "Banner 查看"},
			{Code: PermContentBannerWrite, Name: "Banner 管理"},
			{Code: PermContentNoticeRead, Name: "公告查看"},
			{Code: PermContentNoticeWrite, Name: "公告管理"},
		}},
		{Key: "audit", Name: "审计日志", Permissions: []admindomain.Permission{
			{Code: PermAuditLoginRead, Name: "登录审计查看"},
			{Code: PermAuditSessionRead, Name: "会话审计查看"},
		}},
		{Key: "storage", Name: "存储管理", Permissions: []admindomain.Permission{
			{Code: PermStorageRead, Name: "存储配置查看"},
			{Code: PermStorageWrite, Name: "存储配置修改"},
		}},
		{Key: "workflow", Name: "工作流", Permissions: []admindomain.Permission{
			{Code: PermWorkflowRead, Name: "工作流查看"},
			{Code: PermWorkflowWrite, Name: "工作流管理"},
		}},
		{Key: "version", Name: "版本管理", Permissions: []admindomain.Permission{
			{Code: PermVersionRead, Name: "版本查看"},
			{Code: PermVersionWrite, Name: "版本管理"},
		}},
		{Key: "site", Name: "站点管理", Permissions: []admindomain.Permission{
			{Code: PermSiteRead, Name: "站点查看"},
			{Code: PermSiteWrite, Name: "站点管理"},
			{Code: PermSiteAudit, Name: "站点审核"},
		}},
		{Key: "role_application", Name: "角色申请", Permissions: []admindomain.Permission{
			{Code: PermRoleApplicationRead, Name: "申请查看"},
			{Code: PermRoleApplicationReview, Name: "申请审批"},
		}},
		{Key: "points", Name: "积分管理", Permissions: []admindomain.Permission{
			{Code: PermPointsRead, Name: "积分查看"},
			{Code: PermPointsWrite, Name: "积分调整"},
		}},
		{Key: "email", Name: "邮件服务", Permissions: []admindomain.Permission{
			{Code: PermEmailRead, Name: "邮件配置查看"},
			{Code: PermEmailWrite, Name: "邮件配置修改"},
		}},
		{Key: "payment", Name: "支付管理", Permissions: []admindomain.Permission{
			{Code: PermPaymentRead, Name: "支付配置查看"},
			{Code: PermPaymentWrite, Name: "支付配置修改"},
		}},
		{Key: "ticket", Name: "工单系统", Permissions: []admindomain.Permission{
			{Code: PermTicketRead, Name: "工单查看", Description: "决定可见范围：全局作用域=全部工单，应用作用域=该应用工单"},
			{Code: PermTicketWrite, Name: "工单编辑", Description: "建单、改标题/分类/优先级/标签"},
			{Code: PermTicketReply, Name: "工单回复", Description: "对提单人可见的回复"},
			{Code: PermTicketInternal, Name: "内部备注", Description: "查看并发表仅内部可见的备注"},
			{Code: PermTicketAssign, Name: "工单指派", Description: "指派受理人 / 转派处理组 / 管理关注人"},
			{Code: PermTicketClose, Name: "工单结单", Description: "解决、关闭、重开"},
			{Code: PermTicketDelete, Name: "工单删除"},
			{Code: PermTicketManage, Name: "工单配置", Description: "分类、SLA、处理组、快捷回复"},
			{Code: PermTicketExport, Name: "工单导出"},
		}},
		{Key: "notify", Name: "通知出口", Permissions: []admindomain.Permission{
			{Code: PermNotifyChannelRead, Name: "通知渠道查看"},
			{Code: PermNotifyChannelWrite, Name: "通知渠道管理", Description: "飞书/钉钉/企微/Webhook 等渠道与订阅配置"},
			{Code: PermNotifyDeliveryRead, Name: "投递记录查看"},
			{Code: PermNotifyTest, Name: "测试发送"},
		}},
		{Key: "org", Name: "组织架构", Permissions: []admindomain.Permission{
			{Code: PermOrgCreate, Name: "创建组织"},
			{Code: PermOrgWrite, Name: "修改/删除组织"},
			{Code: PermOrgDeptRead, Name: "查看部门"},
			{Code: PermOrgDeptWrite, Name: "管理部门"},
			{Code: PermOrgMemberRead, Name: "查看成员"},
			{Code: PermOrgMemberWrite, Name: "管理成员"},
			{Code: PermOrgMemberInvite, Name: "邀请成员"},
		}},
	}
}

// AllPermissionCodes 返回目录里的全部权限点代码（有序，供权限展开与校验使用）。
func AllPermissionCodes() []string {
	groups := PermissionCatalog()
	codes := make([]string, 0, 96)
	for _, group := range groups {
		for _, item := range group.Permissions {
			codes = append(codes, item.Code)
		}
	}
	return codes
}

// PermissionExists 判定权限点是否登记在目录里。
func PermissionExists(code string) bool {
	for _, group := range PermissionCatalog() {
		for _, item := range group.Permissions {
			if item.Code == code {
				return true
			}
		}
	}
	return false
}

// PermissionName 返回权限点的中文名；未登记时原样返回代码。
func PermissionName(code string) string {
	for _, group := range PermissionCatalog() {
		for _, item := range group.Permissions {
			if item.Code == code {
				return item.Name
			}
		}
	}
	return code
}
