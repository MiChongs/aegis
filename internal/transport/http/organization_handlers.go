package httptransport

import (
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	orgdomain "aegis/internal/domain/organization"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 组织 Handler 只做三件事：取参数、调服务、写响应。
//
// 权限判定全部在 OrganizationService 内完成（每个方法第一步都是 orgContext）。
// 旧实现把判定散在 handler 里，结果 UpdateDepartment / MoveDepartment /
// DeleteDepartment 三个写接口一个检查都没有 —— 任何登录管理员都能改别家组织的部门。

func orgUUIDParam(c *gin.Context) string  { return strings.TrimSpace(c.Param("orgId")) }
func deptUUIDParam(c *gin.Context) string { return strings.TrimSpace(c.Param("deptId")) }

// ── 元数据 ──

// OrgMetadata 组织元数据：角色、权限目录、枚举。
// 前端据此渲染权限勾选树与下拉框，新增权限点后端改一处即可。
func (h *Handler) OrgMetadata(c *gin.Context) {
	if _, ok := adminAccessSession(c); !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	response.OK(c, gin.H{
		"builtinRoles":       orgdomain.BuiltinRoles(),
		"permissionCatalog":  orgdomain.PermissionCatalog(),
		"rolePermissions":    orgdomain.BuiltinRolePermissions(),
		"orgKinds":           []gin.H{{"value": "enterprise", "label": "企业"}, {"value": "subsidiary", "label": "子公司"}, {"value": "branch", "label": "分支机构"}, {"value": "team", "label": "团队"}, {"value": "partner", "label": "合作方"}},
		"deptKinds":          []gin.H{{"value": "department", "label": "部门"}, {"value": "team", "label": "小组"}, {"value": "group", "label": "群组"}, {"value": "virtual", "label": "虚拟组织"}},
		"orgStatuses":        []gin.H{{"value": "active", "label": "正常"}, {"value": "suspended", "label": "停用"}, {"value": "archived", "label": "已归档"}},
		"memberStatuses":     []gin.H{{"value": "active", "label": "在职"}, {"value": "suspended", "label": "停用"}, {"value": "left", "label": "已离开"}},
		"approvalTriggers":   []gin.H{{"value": orgdomain.TriggerMemberJoin, "label": "成员加入"}, {"value": orgdomain.TriggerMemberLeave, "label": "成员离开"}, {"value": orgdomain.TriggerDeptCreate, "label": "部门创建"}, {"value": orgdomain.TriggerDeptDelete, "label": "部门删除"}, {"value": orgdomain.TriggerRoleChange, "label": "角色变更"}, {"value": orgdomain.TriggerAppBind, "label": "应用绑定"}},
		"approverTypes":      []gin.H{{"value": orgdomain.ApproverLeader, "label": "部门负责人"}, {"value": orgdomain.ApproverOrgRole, "label": "组织角色"}, {"value": orgdomain.ApproverAdmin, "label": "指定管理员"}, {"value": orgdomain.ApproverPosition, "label": "岗位持有者"}},
		"deleteStrategies":   []gin.H{{"value": "restrict", "label": "仅删除空部门"}, {"value": "reparent", "label": "子部门上移一层"}, {"value": "cascade", "label": "连同子部门一并删除"}},
	})
}

// ── 组织 ──

// ListOrganizations 组织列表
func (h *Handler) ListOrganizations(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var q OrgListQueryParams
	_ = c.ShouldBindQuery(&q)
	page, err := h.org.ListOrganizations(c.Request.Context(), session, orgdomain.OrgListQuery{
		Keyword: q.Keyword, Status: q.Status, Kind: q.Kind, ParentUUID: q.ParentID,
		OnlyMine: q.OnlyMine, Page: q.Page, Limit: q.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OK(c, page)
}

// OrganizationTree 组织层级树
func (h *Handler) OrganizationTree(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	tree, err := h.org.OrganizationTree(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OK(c, tree)
}

// GetOrganization 组织详情。响应里带 permissions —— 前端按钮显隐读它，
// 与服务端判定同源，不会出现「点了才 403」。
func (h *Handler) GetOrganization(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	org, access, err := h.org.GetOrganization(c.Request.Context(), session, orgUUIDParam(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OK(c, gin.H{"organization": org, "access": access})
}

// GetOrgOverview 组织概览
func (h *Handler) GetOrgOverview(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	overview, err := h.org.GetOverview(c.Request.Context(), session, orgUUIDParam(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OK(c, overview)
}

// CreateOrganization 创建组织
func (h *Handler) CreateOrganization(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req CreateOrgRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	expiresAt, err := parseOptionalDateTime(req.ExpiresAt)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "expiresAt 需为 RFC3339 格式")
		return
	}
	org, err := h.org.CreateOrganization(c.Request.Context(), session, orgdomain.CreateOrgInput{
		Name: req.Name, Code: req.Code, Kind: req.Kind, ParentUUID: req.ParentID,
		Description: req.Description, LogoURL: req.LogoURL,
		Contact:  orgdomain.Contact{Name: req.ContactName, Email: req.ContactEmail, Phone: req.ContactPhone},
		Industry: req.Industry, Region: req.Region,
		Quota:     orgdomain.Quota{MemberLimit: req.MemberLimit, DeptLimit: req.DeptLimit, AppLimit: req.AppLimit},
		ExpiresAt: expiresAt, Settings: req.Settings, OwnerAdminID: req.OwnerAdminID,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusCreated, "组织已创建", org)
	h.recordAudit(c, "org.create", "organization", org.UUID, "创建组织 "+org.Name)
}

// UpdateOrganization 更新组织
func (h *Handler) UpdateOrganization(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req UpdateOrgRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	input := orgdomain.UpdateOrgInput{
		Name: req.Name, Code: req.Code, Kind: req.Kind, Description: req.Description,
		LogoURL: req.LogoURL, Status: req.Status, Industry: req.Industry, Region: req.Region,
		Settings: req.Settings, ClearExpiry: req.ClearExpiry,
	}
	if req.ContactName != nil || req.ContactEmail != nil || req.ContactPhone != nil {
		input.Contact = &orgdomain.Contact{
			Name: derefString(req.ContactName), Email: derefString(req.ContactEmail), Phone: derefString(req.ContactPhone),
		}
	}
	if req.MemberLimit != nil || req.DeptLimit != nil || req.AppLimit != nil {
		input.Quota = &orgdomain.Quota{
			MemberLimit: derefInt(req.MemberLimit), DeptLimit: derefInt(req.DeptLimit), AppLimit: derefInt(req.AppLimit),
		}
	}
	if req.ExpiresAt != nil {
		expiresAt, err := parseOptionalDateTime(*req.ExpiresAt)
		if err != nil {
			response.Error(c, http.StatusBadRequest, 40000, "expiresAt 需为 RFC3339 格式")
			return
		}
		input.ExpiresAt = expiresAt
	}

	org, err := h.org.UpdateOrganization(c.Request.Context(), session, orgUUIDParam(c), input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OKWithMessage(c, "组织已更新", org)
	h.recordAudit(c, "org.update", "organization", org.UUID, "更新组织 "+org.Name)
}

// TransferOrganization 转让所有权
func (h *Handler) TransferOrganization(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req TransferOrgRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	orgUUID := orgUUIDParam(c)
	if err := h.org.TransferOwnership(c.Request.Context(), session, orgUUID, req.NewOwnerAdminID); err != nil {
		h.writeError(c, err)
		return
	}
	response.OKWithMessage(c, "所有权已转让", nil)
	h.recordAudit(c, "org.transfer", "organization", orgUUID,
		"转让所有权给管理员 #"+strconv.FormatInt(req.NewOwnerAdminID, 10))
}

// DeleteOrganization 删除组织
func (h *Handler) DeleteOrganization(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	orgUUID := orgUUIDParam(c)
	if err := h.org.DeleteOrganization(c.Request.Context(), session, orgUUID); err != nil {
		h.writeError(c, err)
		return
	}
	response.OKWithMessage(c, "组织已删除", nil)
	h.recordAudit(c, "org.delete", "organization", orgUUID, "删除组织")
}

// ListOrgActivity 组织操作日志
func (h *Handler) ListOrgActivity(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	limit, _ := strconv.Atoi(c.Query("limit"))
	result, err := h.org.ListActivity(c.Request.Context(), session, orgUUIDParam(c), c.Query("action"), page, limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OK(c, result)
}

// ── 部门 ──

// GetDepartmentTree 部门树
func (h *Handler) GetDepartmentTree(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	tree, err := h.org.DepartmentTree(c.Request.Context(), session, orgUUIDParam(c), c.Query("status"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OK(c, tree)
}

// GetDepartment 部门详情
func (h *Handler) GetDepartment(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	dept, err := h.org.GetDepartment(c.Request.Context(), session, orgUUIDParam(c), deptUUIDParam(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	if dept == nil {
		response.Error(c, http.StatusNotFound, 40472, "部门不存在")
		return
	}
	response.OK(c, dept)
}

// CreateDepartment 创建部门
func (h *Handler) CreateDepartment(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req CreateDeptRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	dept, err := h.org.CreateDepartment(c.Request.Context(), session, orgUUIDParam(c), orgdomain.CreateDeptInput{
		ParentUUID: req.ParentID, Name: req.Name, Code: req.Code, Kind: req.Kind,
		Description: req.Description, SortOrder: req.SortOrder,
		LeaderAdmin: req.LeaderAdminID, MemberLimit: req.MemberLimit, Settings: req.Settings,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusCreated, "部门已创建", dept)
	h.recordAudit(c, "dept.create", "department", dept.UUID, "创建部门 "+dept.Name)
}

// UpdateDepartment 更新部门
func (h *Handler) UpdateDepartment(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req UpdateDeptRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	dept, err := h.org.UpdateDepartment(c.Request.Context(), session, orgUUIDParam(c), deptUUIDParam(c),
		orgdomain.UpdateDeptInput{
			Name: req.Name, Code: req.Code, Kind: req.Kind, Description: req.Description,
			SortOrder: req.SortOrder, LeaderAdmin: req.LeaderAdminID, ClearLeader: req.ClearLeader,
			Status: req.Status, MemberLimit: req.MemberLimit, Settings: req.Settings,
		})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OKWithMessage(c, "部门已更新", dept)
	h.recordAudit(c, "dept.update", "department", dept.UUID, "更新部门 "+dept.Name)
}

// MoveDepartment 移动部门
func (h *Handler) MoveDepartment(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req MoveDeptRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	deptUUID := deptUUIDParam(c)
	if err := h.org.MoveDepartment(c.Request.Context(), session, orgUUIDParam(c), deptUUID,
		orgdomain.MoveDeptInput{ParentUUID: req.ParentID, SortOrder: req.SortOrder}); err != nil {
		h.writeError(c, err)
		return
	}
	response.OKWithMessage(c, "部门已移动", nil)
	h.recordAudit(c, "dept.move", "department", deptUUID, "移动部门")
}

// ReorderDepartments 批量调整同级顺序
func (h *Handler) ReorderDepartments(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req ReorderDeptRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.org.ReorderDepartments(c.Request.Context(), session, orgUUIDParam(c), req.Order); err != nil {
		h.writeError(c, err)
		return
	}
	response.OKWithMessage(c, "排序已更新", nil)
}

// DeleteDepartment 删除部门，strategy 决定子部门与成员的处置方式
func (h *Handler) DeleteDepartment(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	strategy := orgdomain.DeleteDeptStrategy(strings.TrimSpace(c.Query("strategy")))
	switch strategy {
	case orgdomain.DeleteCascade, orgdomain.DeleteReparent, orgdomain.DeleteRestrict:
	case "":
		strategy = orgdomain.DeleteRestrict
	default:
		response.Error(c, http.StatusBadRequest, 40000, "strategy 只能是 restrict / reparent / cascade")
		return
	}
	deptUUID := deptUUIDParam(c)
	if err := h.org.DeleteDepartment(c.Request.Context(), session, orgUUIDParam(c), deptUUID, strategy); err != nil {
		h.writeError(c, err)
		return
	}
	response.OKWithMessage(c, "部门已删除", nil)
	h.recordAudit(c, "dept.delete", "department", deptUUID, "删除部门（策略："+string(strategy)+"）")
}

// ── 组织成员 ──

// ListOrgMembers 组织成员列表
func (h *Handler) ListOrgMembers(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var q MemberListQueryParams
	_ = c.ShouldBindQuery(&q)
	page, err := h.org.ListMembers(c.Request.Context(), session, orgUUIDParam(c), orgdomain.MemberListQuery{
		Keyword: q.Keyword, OrgRole: q.OrgRole, Status: q.Status,
		DeptUUID: q.DeptID, IncludeSubDept: q.IncludeSubDepts, Unassigned: q.Unassigned,
		Page: q.Page, Limit: q.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OK(c, page)
}

// GetOrgMember 组织成员详情
func (h *Handler) GetOrgMember(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	adminID, err := pathInt64(c, "adminId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的管理员 ID")
		return
	}
	member, err := h.org.GetMember(c.Request.Context(), session, orgUUIDParam(c), adminID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OK(c, member)
}

// AddOrgMember 直接把管理员加入组织
func (h *Handler) AddOrgMember(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req AddOrgMemberRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	orgUUID := orgUUIDParam(c)
	member, err := h.org.AddMember(c.Request.Context(), session, orgUUID, orgdomain.AddMemberInput{
		AdminID: req.AdminID, OrgRole: req.OrgRole, EmployeeNo: req.EmployeeNo, Title: req.Title,
		DeptUUIDs: req.DeptIDs, PrimaryDept: req.PrimaryDeptID,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusCreated, "成员已加入组织", member)
	h.recordAudit(c, "org.member_add", "organization", orgUUID,
		"加入成员 #"+strconv.FormatInt(req.AdminID, 10))
}

// UpdateOrgMember 更新成员档案
func (h *Handler) UpdateOrgMember(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	adminID, err := pathInt64(c, "adminId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的管理员 ID")
		return
	}
	var req UpdateOrgMemberRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	orgUUID := orgUUIDParam(c)
	member, err := h.org.UpdateMember(c.Request.Context(), session, orgUUID, adminID, orgdomain.UpdateMemberInput{
		OrgRole: req.OrgRole, EmployeeNo: req.EmployeeNo, Title: req.Title,
		Status: req.Status, PrimaryDept: req.PrimaryDeptID,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OKWithMessage(c, "成员已更新", member)
	h.recordAudit(c, "org.member_update", "organization", orgUUID,
		"更新成员 #"+strconv.FormatInt(adminID, 10))
}

// RemoveOrgMember 移出组织
func (h *Handler) RemoveOrgMember(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	adminID, err := pathInt64(c, "adminId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的管理员 ID")
		return
	}
	orgUUID := orgUUIDParam(c)
	if err := h.org.RemoveMember(c.Request.Context(), session, orgUUID, adminID); err != nil {
		h.writeError(c, err)
		return
	}
	response.OKWithMessage(c, "成员已移出组织", nil)
	h.recordAudit(c, "org.member_remove", "organization", orgUUID,
		"移出成员 #"+strconv.FormatInt(adminID, 10))
}

// AssignMemberDepartments 调整成员的部门归属
func (h *Handler) AssignMemberDepartments(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	adminID, err := pathInt64(c, "adminId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的管理员 ID")
		return
	}
	var req AssignDeptRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.org.AssignMemberDepartments(c.Request.Context(), session, orgUUIDParam(c), adminID,
		orgdomain.AssignDeptInput{DeptUUIDs: req.DeptIDs, PrimaryDept: req.PrimaryDeptID, Replace: req.Replace}); err != nil {
		h.writeError(c, err)
		return
	}
	response.OKWithMessage(c, "部门归属已更新", nil)
}

// SearchAssignableAdmins 成员选择器：搜索可加入组织的管理员
func (h *Handler) SearchAssignableAdmins(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	items, err := h.org.SearchAssignableAdmins(c.Request.Context(), session, orgUUIDParam(c),
		c.Query("keyword"), c.Query("exclude") != "false", limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OK(c, items)
}

// ── 部门成员 ──

// ListDepartmentMembers 部门内成员
func (h *Handler) ListDepartmentMembers(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	items, err := h.org.ListDepartmentMembers(c.Request.Context(), session, orgUUIDParam(c), deptUUIDParam(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OK(c, items)
}

// SetDepartmentMember 设置部门内成员属性
func (h *Handler) SetDepartmentMember(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	adminID, err := pathInt64(c, "adminId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的管理员 ID")
		return
	}
	var req SetDeptMemberRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	input := orgdomain.SetDeptMemberInput{
		IsLeader: req.IsLeader, PositionUUID: req.PositionID, JobTitle: req.JobTitle,
		ReportingTo: req.ReportingTo, ClearReport: req.ClearReporting,
		DelegateTo: req.DelegateTo, ClearDeleg: req.ClearDelegate,
	}
	if req.DelegateExpiresAt != nil {
		expiresAt, err := parseOptionalDateTime(*req.DelegateExpiresAt)
		if err != nil {
			response.Error(c, http.StatusBadRequest, 40000, "delegateExpiresAt 需为 RFC3339 格式")
			return
		}
		input.DelegateTill = expiresAt
	}
	if err := h.org.SetDepartmentMember(c.Request.Context(), session, orgUUIDParam(c), deptUUIDParam(c), adminID, input); err != nil {
		h.writeError(c, err)
		return
	}
	response.OKWithMessage(c, "成员信息已更新", nil)
}

// RemoveDepartmentMember 把成员移出部门
func (h *Handler) RemoveDepartmentMember(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	adminID, err := pathInt64(c, "adminId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的管理员 ID")
		return
	}
	deptUUID := deptUUIDParam(c)
	if err := h.org.RemoveDepartmentMember(c.Request.Context(), session, orgUUIDParam(c), deptUUID, adminID); err != nil {
		h.writeError(c, err)
		return
	}
	response.OKWithMessage(c, "成员已移出部门", nil)
	h.recordAudit(c, "dept.member_remove", "department", deptUUID,
		"移出成员 #"+strconv.FormatInt(adminID, 10))
}

// GetReportingChain 汇报链
func (h *Handler) GetReportingChain(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	adminID, err := pathInt64(c, "adminId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的管理员 ID")
		return
	}
	chain, err := h.org.ReportingChain(c.Request.Context(), session, orgUUIDParam(c), deptUUIDParam(c), adminID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OK(c, chain)
}

// GetDirectReports 直接下属
func (h *Handler) GetDirectReports(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	adminID, err := pathInt64(c, "adminId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的管理员 ID")
		return
	}
	reports, err := h.org.DirectReports(c.Request.Context(), session, orgUUIDParam(c), deptUUIDParam(c), adminID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OK(c, reports)
}

// ── 邀请 ──

// InviteOrgMembers 发起邀请
func (h *Handler) InviteOrgMembers(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req InviteMembersRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	orgUUID := orgUUIDParam(c)
	invitations, err := h.org.InviteMembers(c.Request.Context(), session, orgUUID, orgdomain.InviteInput{
		AdminIDs: req.AdminIDs, DeptUUID: req.DeptID, OrgRole: req.OrgRole,
		IsLeader: req.IsLeader, Message: req.Message,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusCreated, "邀请已发送", gin.H{
		"created": len(invitations), "requested": len(req.AdminIDs), "invitations": invitations,
	})
	h.recordAudit(c, "org.invite", "organization", orgUUID,
		"发出 "+strconv.Itoa(len(invitations))+" 份邀请")
}

// ListMyInvitations 我收到 / 发出的邀请
func (h *Handler) ListMyInvitations(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var q InvitationListQueryParams
	_ = c.ShouldBindQuery(&q)
	if q.Role == "" {
		q.Role = "received"
	}
	result, err := h.org.ListMyInvitations(c.Request.Context(), session, orgdomain.InvitationQuery{
		Role: q.Role, Status: q.Status, OrgUUID: q.OrgID, Page: q.Page, Limit: q.Limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OK(c, result)
}

// CountPendingInvitations 待处理邀请数
func (h *Handler) CountPendingInvitations(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	count, err := h.org.CountPendingInvitations(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OK(c, gin.H{"count": count})
}

// RespondInvitation 接受 / 拒绝 / 取消邀请
func (h *Handler) RespondInvitation(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	inviteUUID := strings.TrimSpace(c.Param("inviteId"))
	action := ""
	switch strings.TrimSpace(c.Param("action")) {
	case "accept":
		action = "accepted"
	case "reject":
		action = "rejected"
	case "cancel":
		action = "cancelled"
	default:
		response.Error(c, http.StatusBadRequest, 40000, "action 只能是 accept / reject / cancel")
		return
	}
	inv, err := h.org.RespondInvitation(c.Request.Context(), session, inviteUUID, action)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OKWithMessage(c, "操作成功", inv)
	h.recordAudit(c, "org.invite_"+action, "invitation", inviteUUID, "处理组织邀请")
}

// ── 岗位 ──

// ListPositions 岗位列表
func (h *Handler) ListPositions(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	items, err := h.org.ListPositions(c.Request.Context(), session, orgUUIDParam(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OK(c, items)
}

// CreatePosition 创建岗位
func (h *Handler) CreatePosition(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req PositionRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	pos, err := h.org.CreatePosition(c.Request.Context(), session, orgUUIDParam(c), orgdomain.PositionInput{
		Name: req.Name, Code: req.Code, Level: req.Level, Description: req.Description,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusCreated, "岗位已创建", pos)
	h.recordAudit(c, "position.create", "position", pos.UUID, "创建岗位 "+pos.Name)
}

// UpdatePosition 更新岗位
func (h *Handler) UpdatePosition(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req PositionRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	pos, err := h.org.UpdatePosition(c.Request.Context(), session, orgUUIDParam(c),
		strings.TrimSpace(c.Param("posId")), orgdomain.PositionInput{
			Name: req.Name, Code: req.Code, Level: req.Level, Description: req.Description,
		})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OKWithMessage(c, "岗位已更新", pos)
	h.recordAudit(c, "position.update", "position", pos.UUID, "更新岗位 "+pos.Name)
}

// DeletePosition 删除岗位
func (h *Handler) DeletePosition(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	posUUID := strings.TrimSpace(c.Param("posId"))
	if err := h.org.DeletePosition(c.Request.Context(), session, orgUUIDParam(c), posUUID); err != nil {
		h.writeError(c, err)
		return
	}
	response.OKWithMessage(c, "岗位已删除", nil)
	h.recordAudit(c, "position.delete", "position", posUUID, "删除岗位")
}

// ── 组织角色 ──

// ListOrgRoles 组织角色（自定义 + 内置）
func (h *Handler) ListOrgRoles(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	roles, builtins, err := h.org.ListRoles(c.Request.Context(), session, orgUUIDParam(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OK(c, gin.H{"roles": roles, "builtinRoles": builtins, "catalog": orgdomain.PermissionCatalog()})
}

// CreateOrgRole 创建组织角色
func (h *Handler) CreateOrgRole(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req OrgRoleRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	role, err := h.org.CreateRole(c.Request.Context(), session, orgUUIDParam(c), orgdomain.RoleInput{
		RoleKey: req.RoleKey, Name: req.Name, Description: req.Description, Permissions: req.Permissions,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusCreated, "角色已创建", role)
	h.recordAudit(c, "org.role_create", "org_role", role.UUID, "创建组织角色 "+role.Name)
}

// UpdateOrgRole 更新组织角色
func (h *Handler) UpdateOrgRole(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req OrgRoleRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	role, err := h.org.UpdateRole(c.Request.Context(), session, orgUUIDParam(c),
		strings.TrimSpace(c.Param("roleId")), orgdomain.RoleInput{
			RoleKey: req.RoleKey, Name: req.Name, Description: req.Description, Permissions: req.Permissions,
		})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OKWithMessage(c, "角色已更新", role)
	h.recordAudit(c, "org.role_update", "org_role", role.UUID, "更新组织角色 "+role.Name)
}

// DeleteOrgRole 删除组织角色
func (h *Handler) DeleteOrgRole(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	roleUUID := strings.TrimSpace(c.Param("roleId"))
	if err := h.org.DeleteRole(c.Request.Context(), session, orgUUIDParam(c), roleUUID); err != nil {
		h.writeError(c, err)
		return
	}
	response.OKWithMessage(c, "角色已删除", nil)
	h.recordAudit(c, "org.role_delete", "org_role", roleUUID, "删除组织角色")
}

// ListOrgRoleGrants 角色授予记录
func (h *Handler) ListOrgRoleGrants(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	grants, err := h.org.ListRoleGrants(c.Request.Context(), session, orgUUIDParam(c), strings.TrimSpace(c.Param("roleId")))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OK(c, grants)
}

// GrantOrgRole 授予组织角色
func (h *Handler) GrantOrgRole(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req GrantOrgRoleRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	roleUUID := strings.TrimSpace(c.Param("roleId"))
	granted, err := h.org.GrantRole(c.Request.Context(), session, orgUUIDParam(c), roleUUID,
		orgdomain.GrantRoleInput{AdminIDs: req.AdminIDs, ScopeDeptUUID: req.ScopeDeptID})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OKWithMessage(c, "角色已授予", gin.H{"granted": granted})
	h.recordAudit(c, "org.role_grant", "org_role", roleUUID,
		"授予角色给 "+strconv.Itoa(granted)+" 位成员")
}

// RevokeOrgRole 撤销组织角色
func (h *Handler) RevokeOrgRole(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	adminID, err := pathInt64(c, "adminId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的管理员 ID")
		return
	}
	roleUUID := strings.TrimSpace(c.Param("roleId"))
	if err := h.org.RevokeRole(c.Request.Context(), session, orgUUIDParam(c), roleUUID, adminID); err != nil {
		h.writeError(c, err)
		return
	}
	response.OKWithMessage(c, "角色已撤销", nil)
	h.recordAudit(c, "org.role_revoke", "org_role", roleUUID,
		"撤销管理员 #"+strconv.FormatInt(adminID, 10)+" 的角色")
}

// ── 应用绑定 ──

// ListOrgApps 组织绑定的应用
func (h *Handler) ListOrgApps(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	items, err := h.org.ListOrgApps(c.Request.Context(), session, orgUUIDParam(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OK(c, items)
}

// BindOrgApp 绑定应用
func (h *Handler) BindOrgApp(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req BindOrgAppRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	orgUUID := orgUUIDParam(c)
	if err := h.org.BindOrgApp(c.Request.Context(), session, orgUUID, req.AppID, req.Owned); err != nil {
		h.writeError(c, err)
		return
	}
	response.OKWithMessage(c, "应用已绑定", nil)
	h.recordAudit(c, "org.app_bind", "organization", orgUUID,
		"绑定应用 #"+strconv.FormatInt(req.AppID, 10))
}

// UnbindOrgApp 解绑应用
func (h *Handler) UnbindOrgApp(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	appID, err := pathInt64(c, "appId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用 ID")
		return
	}
	orgUUID := orgUUIDParam(c)
	if err := h.org.UnbindOrgApp(c.Request.Context(), session, orgUUID, appID); err != nil {
		h.writeError(c, err)
		return
	}
	response.OKWithMessage(c, "应用已解绑", nil)
	h.recordAudit(c, "org.app_unbind", "organization", orgUUID,
		"解绑应用 #"+strconv.FormatInt(appID, 10))
}

// ── 导入导出 ──

// ExportOrganization 导出组织架构 Excel
func (h *Handler) ExportOrganization(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	data, filename, err := h.org.ExportOrganization(c.Request.Context(), session, orgUUIDParam(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	writeXLSX(c, filename, data)
	h.recordAudit(c, "org.export", "organization", orgUUIDParam(c), "导出组织架构")
}

// DownloadImportTemplate 下载导入模板
func (h *Handler) DownloadImportTemplate(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	data, filename, err := h.org.ExportTemplate(c.Request.Context(), session, orgUUIDParam(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	writeXLSX(c, filename, data)
}

// ImportOrganization 导入组织架构。dryRun=true 时只校验不落库。
func (h *Handler) ImportOrganization(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "请上传 Excel 文件（字段名 file）")
		return
	}
	if fileHeader.Size > 10<<20 {
		response.Error(c, http.StatusBadRequest, 40000, "文件不能超过 10MB")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无法读取上传的文件")
		return
	}
	defer file.Close()

	data := make([]byte, fileHeader.Size)
	if _, err := io.ReadFull(file, data); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "读取文件失败")
		return
	}

	dryRun := c.PostForm("dryRun") == "true" || c.Query("dryRun") == "true"
	orgUUID := orgUUIDParam(c)
	result, err := h.org.ImportOrganization(c.Request.Context(), session, orgUUID, data, dryRun)
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "导入完成"
	if dryRun {
		message = "校验完成"
	}
	response.OKWithMessage(c, message, result)
	if !dryRun {
		h.recordAudit(c, "org.import", "organization", orgUUID, "导入组织架构")
	}
}

// ── 内部辅助 ──

func writeXLSX(c *gin.Context, filename string, data []byte) {
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	// 文件名含中文，必须走 RFC 5987 的 filename*，否则下载下来会是乱码
	c.Header("Content-Disposition", "attachment; filename=export.xlsx; filename*=UTF-8''"+url.PathEscape(filename))
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", data)
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func derefInt(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}
