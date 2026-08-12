package httptransport

import (
	"github.com/gin-gonic/gin"
)

// 组织架构 /api/admin/system/organizations/*。
//
// 本文件只做路由注册，由 NewRouter 按原顺序调用；分组规则见 route_groups.go。

// registerOrgRoutes 注册组织架构。
//
// 收的是**已经配好中间件的** adminSystem 组而不是 *gin.Engine：
// 重新建一个同路径的组会另起一条中间件链，鉴权与审计就都对不上了。
func registerOrgRoutes(adminSystem *gin.RouterGroup, h *Handler) {
	// ── 组织架构 ──
	//
	// 全部实体一律用 UUID 定位，且每条子路径都挂在 /organizations/:orgId 之下 ——
	// 组织归属由路径本身携带，服务层据此做隔离校验。
	// 旧路由把部门/岗位/审批链挂在顶层（/departments/:deptId），
	// 请求里根本没有组织信息，也就无从校验「这个部门是不是你家的」。
	adminSystem.GET("/org-metadata", h.OrgMetadata)
	adminSystem.GET("/organizations", h.ListOrganizations)
	adminSystem.POST("/organizations", h.CreateOrganization)
	adminSystem.GET("/organizations/tree", h.OrganizationTree)
	adminSystem.GET("/organizations/:orgId", h.GetOrganization)
	adminSystem.PUT("/organizations/:orgId", h.UpdateOrganization)
	adminSystem.DELETE("/organizations/:orgId", h.DeleteOrganization)
	adminSystem.GET("/organizations/:orgId/overview", h.GetOrgOverview)
	adminSystem.POST("/organizations/:orgId/transfer", h.TransferOrganization)
	adminSystem.GET("/organizations/:orgId/activity", h.ListOrgActivity)

	// 部门
	adminSystem.GET("/organizations/:orgId/departments", h.GetDepartmentTree)
	adminSystem.POST("/organizations/:orgId/departments", h.CreateDepartment)
	adminSystem.PUT("/organizations/:orgId/dept-order", h.ReorderDepartments)
	adminSystem.GET("/organizations/:orgId/departments/:deptId", h.GetDepartment)
	adminSystem.PUT("/organizations/:orgId/departments/:deptId", h.UpdateDepartment)
	adminSystem.DELETE("/organizations/:orgId/departments/:deptId", h.DeleteDepartment)
	adminSystem.PUT("/organizations/:orgId/departments/:deptId/move", h.MoveDepartment)

	// 部门成员
	adminSystem.GET("/organizations/:orgId/departments/:deptId/members", h.ListDepartmentMembers)
	adminSystem.PUT("/organizations/:orgId/departments/:deptId/members/:adminId", h.SetDepartmentMember)
	adminSystem.DELETE("/organizations/:orgId/departments/:deptId/members/:adminId", h.RemoveDepartmentMember)
	adminSystem.GET("/organizations/:orgId/departments/:deptId/members/:adminId/reporting-chain", h.GetReportingChain)
	adminSystem.GET("/organizations/:orgId/departments/:deptId/members/:adminId/reports", h.GetDirectReports)

	// 组织成员
	adminSystem.GET("/organizations/:orgId/members", h.ListOrgMembers)
	adminSystem.POST("/organizations/:orgId/members", h.AddOrgMember)
	adminSystem.GET("/organizations/:orgId/assignable-admins", h.SearchAssignableAdmins)
	adminSystem.GET("/organizations/:orgId/members/:adminId", h.GetOrgMember)
	adminSystem.PUT("/organizations/:orgId/members/:adminId", h.UpdateOrgMember)
	adminSystem.DELETE("/organizations/:orgId/members/:adminId", h.RemoveOrgMember)
	adminSystem.PUT("/organizations/:orgId/members/:adminId/departments", h.AssignMemberDepartments)

	// 邀请：受邀人此时还不在组织里，所以入口挂在个人维度
	adminSystem.POST("/organizations/:orgId/invitations", h.InviteOrgMembers)
	adminSystem.GET("/org-invitations", h.ListMyInvitations)
	adminSystem.GET("/org-invitations/count", h.CountPendingInvitations)
	adminSystem.POST("/org-invitations/:inviteId/:action", h.RespondInvitation)

	// 岗位
	adminSystem.GET("/organizations/:orgId/positions", h.ListPositions)
	adminSystem.POST("/organizations/:orgId/positions", h.CreatePosition)
	adminSystem.PUT("/organizations/:orgId/positions/:posId", h.UpdatePosition)
	adminSystem.DELETE("/organizations/:orgId/positions/:posId", h.DeletePosition)

	// 组织角色与授权
	adminSystem.GET("/organizations/:orgId/roles", h.ListOrgRoles)
	adminSystem.POST("/organizations/:orgId/roles", h.CreateOrgRole)
	adminSystem.PUT("/organizations/:orgId/roles/:roleId", h.UpdateOrgRole)
	adminSystem.DELETE("/organizations/:orgId/roles/:roleId", h.DeleteOrgRole)
	adminSystem.GET("/organizations/:orgId/roles/:roleId/grants", h.ListOrgRoleGrants)
	adminSystem.POST("/organizations/:orgId/roles/:roleId/grants", h.GrantOrgRole)
	adminSystem.DELETE("/organizations/:orgId/roles/:roleId/grants/:adminId", h.RevokeOrgRole)

	// 审批链
	adminSystem.GET("/organizations/:orgId/approval-chains", h.ListApprovalChains)
	adminSystem.POST("/organizations/:orgId/approval-chains", h.CreateApprovalChain)
	adminSystem.PUT("/organizations/:orgId/approval-chains/:chainId", h.UpdateApprovalChain)
	adminSystem.DELETE("/organizations/:orgId/approval-chains/:chainId", h.DeleteApprovalChain)

	// 审批实例：待办跨组织聚合，因此单列一个个人维度入口
	adminSystem.GET("/organizations/:orgId/approvals", h.ListApprovalInstances)
	adminSystem.GET("/organizations/:orgId/approvals/:instanceId", h.GetApprovalInstance)
	adminSystem.GET("/org-approvals/pending", h.ListMyPendingApprovals)
	adminSystem.POST("/org-approvals/:instanceId/decision", h.DecideApproval)

	// 权限模板
	adminSystem.GET("/organizations/:orgId/perm-templates", h.ListOrgPermTemplates)
	adminSystem.POST("/organizations/:orgId/perm-templates", h.CreateOrgPermTemplate)
	adminSystem.DELETE("/organizations/:orgId/perm-templates/:templateId", h.DeleteOrgPermTemplate)
	adminSystem.POST("/organizations/:orgId/perm-templates/:templateId/apply", h.ApplyPermTemplate)

	// 应用资源绑定
	adminSystem.GET("/organizations/:orgId/apps", h.ListOrgApps)
	adminSystem.POST("/organizations/:orgId/apps", h.BindOrgApp)
	adminSystem.DELETE("/organizations/:orgId/apps/:appId", h.UnbindOrgApp)

	// 协作组
	adminSystem.GET("/organizations/:orgId/collab-groups", h.ListCollabGroups)
	adminSystem.POST("/organizations/:orgId/collab-groups", h.CreateCollabGroup)
	adminSystem.PUT("/organizations/:orgId/collab-groups/:groupId", h.UpdateCollabGroup)
	adminSystem.DELETE("/organizations/:orgId/collab-groups/:groupId", h.DeleteCollabGroup)

	// 导入导出（Excel）
	adminSystem.GET("/organizations/:orgId/export", h.ExportOrganization)
	adminSystem.GET("/organizations/:orgId/import-template", h.DownloadImportTemplate)
	adminSystem.POST("/organizations/:orgId/import", h.ImportOrganization)
}
