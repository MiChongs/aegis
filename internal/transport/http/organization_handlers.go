package httptransport

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	systemdomain "aegis/internal/domain/system"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

func (h *Handler) orgID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("orgId"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的组织 ID")
		return 0, false
	}
	return id, true
}

func (h *Handler) deptID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("deptId"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的部门 ID")
		return 0, false
	}
	return id, true
}

// ── 权限辅助 ──

// requireDeptLeaderOrSuper 部门负责人或超管才能执行写操作
func (h *Handler) requireDeptLeaderOrSuper(c *gin.Context, deptID int64) bool {
	session, ok := adminAccessSession(c)
	if !ok || session == nil {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return false
	}
	if session.IsSuperAdmin {
		return true
	}
	isLeader, _ := h.org.IsDepartmentLeader(c.Request.Context(), deptID, session.AdminID)
	if isLeader {
		return true
	}
	response.Error(c, http.StatusForbidden, 40393, "需要部门负责人权限")
	return false
}

// requireDeptMemberOrSuper 部门成员或超管才能访问
func (h *Handler) requireDeptMemberOrSuper(c *gin.Context, deptID int64) bool {
	session, ok := adminAccessSession(c)
	if !ok || session == nil {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return false
	}
	if session.IsSuperAdmin {
		return true
	}
	isMember, _ := h.org.IsDepartmentMember(c.Request.Context(), deptID, session.AdminID)
	if isMember {
		return true
	}
	response.Error(c, http.StatusForbidden, 40394, "非该部门成员")
	return false
}

// ── 组织 ──

func (h *Handler) ListOrganizations(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok || session == nil {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	items, err := h.org.ListOrganizations(c.Request.Context(), session.AdminID, session.IsSuperAdmin)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", items)
}

func (h *Handler) CreateOrganization(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		return
	}
	var req CreateOrgRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	org, err := h.org.CreateOrganization(c.Request.Context(), systemdomain.CreateOrgInput{
		Name: req.Name, Code: req.Code, Description: req.Description, LogoURL: req.LogoURL,
	}, session.AdminID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 201, "组织已创建", org)
	h.recordAudit(c, "org.create", "organization", strconv.FormatInt(org.ID, 10), "创建组织 "+req.Name)
}

func (h *Handler) UpdateOrganization(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	orgID, ok := h.orgID(c)
	if !ok {
		return
	}
	var req UpdateOrgRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	org, err := h.org.UpdateOrganization(c.Request.Context(), orgID, systemdomain.UpdateOrgInput{
		Name: req.Name, Code: req.Code, Description: req.Description, LogoURL: req.LogoURL, Status: req.Status,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "组织已更新", org)
	h.recordAudit(c, "org.update", "organization", strconv.FormatInt(orgID, 10), fmt.Sprintf("修改组织 #%d", orgID))
}

func (h *Handler) DeleteOrganization(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	orgID, ok := h.orgID(c)
	if !ok {
		return
	}
	if err := h.org.DeleteOrganization(c.Request.Context(), orgID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "组织已删除", nil)
	h.recordAudit(c, "org.delete", "organization", strconv.FormatInt(orgID, 10), fmt.Sprintf("删除组织 #%d", orgID))
}

// ── 部门 ──

func (h *Handler) GetDepartmentTree(c *gin.Context) {
	orgID, ok := h.orgID(c)
	if !ok {
		return
	}
	if !h.requireOrgMemberOrSuper(c, orgID) {
		return
	}
	tree, err := h.org.GetDepartmentTree(c.Request.Context(), orgID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", tree)
}

func (h *Handler) CreateDepartment(c *gin.Context) {
	orgID, ok := h.orgID(c)
	if !ok {
		return
	}
	if !h.requireOrgMemberOrSuper(c, orgID) {
		return
	}
	var req CreateDeptRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	dept, err := h.org.CreateDepartment(c.Request.Context(), orgID, systemdomain.CreateDeptInput{
		ParentID: req.ParentID, Name: req.Name, Code: req.Code,
		Description: req.Description, SortOrder: req.SortOrder, LeaderID: req.LeaderID,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 201, "部门已创建", dept)
	h.recordAudit(c, "dept.create", "department", strconv.FormatInt(dept.ID, 10), "创建部门 "+req.Name)
}

func (h *Handler) UpdateDepartment(c *gin.Context) {
	deptID, ok := h.deptID(c)
	if !ok {
		return
	}
	var req UpdateDeptRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	dept, err := h.org.UpdateDepartment(c.Request.Context(), deptID, systemdomain.UpdateDeptInput{
		Name: req.Name, Code: req.Code, Description: req.Description,
		SortOrder: req.SortOrder, LeaderID: req.LeaderID, Status: req.Status,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "部门已更新", dept)
}

func (h *Handler) MoveDepartment(c *gin.Context) {
	deptID, ok := h.deptID(c)
	if !ok {
		return
	}
	var req MoveDeptRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.org.MoveDepartment(c.Request.Context(), deptID, req.ParentID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "部门已移动", nil)
}

func (h *Handler) DeleteDepartment(c *gin.Context) {
	deptID, ok := h.deptID(c)
	if !ok {
		return
	}
	if err := h.org.DeleteDepartment(c.Request.Context(), deptID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "部门已删除", nil)
	h.recordAudit(c, "dept.delete", "department", strconv.FormatInt(deptID, 10), fmt.Sprintf("删除部门 #%d", deptID))
}

// ── 成员 ──

func (h *Handler) ListDepartmentMembers(c *gin.Context) {
	deptID, ok := h.deptID(c)
	if !ok {
		return
	}
	if orgID, ok2 := h.deptOrgID(c, deptID); ok2 {
		if !h.requireOrgMemberOrSuper(c, orgID) {
			return
		}
	}
	items, err := h.org.ListDepartmentMembers(c.Request.Context(), deptID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", items)
}

func (h *Handler) AddDepartmentMember(c *gin.Context) {
	deptID, ok := h.deptID(c)
	if !ok {
		return
	}
	if orgID, ok2 := h.deptOrgID(c, deptID); ok2 {
		if !h.requireOrgMemberOrSuper(c, orgID) {
			return
		}
	}
	if !h.requireDeptLeaderOrSuper(c, deptID) {
		return
	}
	var req AddMemberRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.org.AddDepartmentMember(c.Request.Context(), deptID, req.AdminID, req.IsLeader); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "成员已添加", nil)
	h.recordAudit(c, "dept.member_add", "department", strconv.FormatInt(deptID, 10), fmt.Sprintf("添加成员 #%d 到部门 #%d", req.AdminID, deptID))
}

func (h *Handler) RemoveDepartmentMember(c *gin.Context) {
	deptID, ok := h.deptID(c)
	if !ok {
		return
	}
	if orgID, ok2 := h.deptOrgID(c, deptID); ok2 {
		if !h.requireOrgMemberOrSuper(c, orgID) {
			return
		}
	}
	if !h.requireDeptLeaderOrSuper(c, deptID) {
		return
	}
	adminID, err := strconv.ParseInt(c.Param("adminId"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的管理员 ID")
		return
	}
	if err := h.org.RemoveDepartmentMember(c.Request.Context(), deptID, adminID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "成员已移除", nil)
	h.recordAudit(c, "dept.member_remove", "department", strconv.FormatInt(deptID, 10), fmt.Sprintf("移除成员 #%d 从部门 #%d", adminID, deptID))
}

func (h *Handler) ListAdminDepartments(c *gin.Context) {
	adminID, err := strconv.ParseInt(c.Param("adminId"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的管理员 ID")
		return
	}
	items, err := h.org.ListAdminDepartments(c.Request.Context(), adminID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", items)
}

// ── 邀请 ──

func (h *Handler) InviteDeptMember(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok || session == nil {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	deptID, ok := h.deptID(c)
	if !ok {
		return
	}
	if orgID, ok2 := h.deptOrgID(c, deptID); ok2 {
		if !h.requireOrgMemberOrSuper(c, orgID) {
			return
		}
	}
	if !session.IsSuperAdmin {
		if !h.requireDeptMemberOrSuper(c, deptID) {
			return
		}
	}
	var req InviteMemberRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	inv, err := h.org.InviteMember(c.Request.Context(), deptID, req.AdminID, req.IsLeader, req.Message, session.AdminID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 201, "邀请已发送", inv)
	h.recordAudit(c, "dept.invite", "department", strconv.FormatInt(deptID, 10), fmt.Sprintf("邀请管理员 #%d 加入部门", req.AdminID))
}

func (h *Handler) ListMyInvitations(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok || session == nil {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var q InvitationListQueryParams
	_ = c.ShouldBindQuery(&q)
	if q.Role == "" {
		q.Role = "received"
	}
	result, err := h.org.ListMyInvitations(c.Request.Context(), session.AdminID, q.Role, q.Status, q.Page, q.Limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", result)
}

func (h *Handler) CountPendingInvitations(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok || session == nil {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	count, err := h.org.CountPendingInvitations(c.Request.Context(), session.AdminID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", gin.H{"count": count})
}

func (h *Handler) AcceptInvitation(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok || session == nil {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的邀请 ID")
		return
	}
	if err := h.org.AcceptInvitation(c.Request.Context(), id, session.AdminID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "邀请已接受", nil)
	h.recordAudit(c, "dept.invite_accept", "invitation", strconv.FormatInt(id, 10), "接受部门邀请")
}

func (h *Handler) RejectInvitation(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok || session == nil {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的邀请 ID")
		return
	}
	if err := h.org.RejectInvitation(c.Request.Context(), id, session.AdminID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "邀请已拒绝", nil)
	h.recordAudit(c, "dept.invite_reject", "invitation", strconv.FormatInt(id, 10), "拒绝部门邀请")
}

func (h *Handler) CancelInvitation(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok || session == nil {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的邀请 ID")
		return
	}
	if err := h.org.CancelInvitation(c.Request.Context(), id, session.AdminID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "邀请已取消", nil)
	h.recordAudit(c, "dept.invite_cancel", "invitation", strconv.FormatInt(id, 10), "取消部门邀请")
}

// ── 岗位 ──

func (h *Handler) ListPositions(c *gin.Context) {
	orgID, ok := h.orgID(c)
	if !ok {
		return
	}
	items, err := h.org.ListPositions(c.Request.Context(), orgID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", items)
}

func (h *Handler) CreatePosition(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	orgID, ok := h.orgID(c)
	if !ok {
		return
	}
	var req CreatePositionRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	pos, err := h.org.CreatePosition(c.Request.Context(), systemdomain.CreatePositionInput{
		OrgID: orgID, Name: req.Name, Code: req.Code, Description: req.Description, Level: req.Level,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 201, "岗位已创建", pos)
	h.recordAudit(c, "position.create", "position", strconv.FormatInt(pos.ID, 10), "创建岗位 "+req.Name)
}

func (h *Handler) UpdatePosition(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	posID, err := pathInt64(c, "posId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的岗位 ID")
		return
	}
	var req UpdatePositionRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	// 对于 PATCH 语义，为空时取默认值；这里要求前端传完整值
	name, code, desc := "", "", ""
	level := 0
	if req.Name != nil {
		name = *req.Name
	}
	if req.Code != nil {
		code = *req.Code
	}
	if req.Description != nil {
		desc = *req.Description
	}
	if req.Level != nil {
		level = *req.Level
	}
	pos, err2 := h.org.UpdatePosition(c.Request.Context(), posID, name, code, desc, level)
	if err2 != nil {
		h.writeError(c, err2)
		return
	}
	response.Success(c, 200, "岗位已更新", pos)
	h.recordAudit(c, "position.update", "position", strconv.FormatInt(posID, 10), fmt.Sprintf("修改岗位 #%d", posID))
}

func (h *Handler) DeletePosition(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	posID, err := pathInt64(c, "posId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的岗位 ID")
		return
	}
	if err := h.org.DeletePosition(c.Request.Context(), posID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "岗位已删除", nil)
	h.recordAudit(c, "position.delete", "position", strconv.FormatInt(posID, 10), fmt.Sprintf("删除岗位 #%d", posID))
}

// ── 成员增强 ──

func (h *Handler) UpdateMemberPosition(c *gin.Context) {
	deptID, ok := h.deptID(c)
	if !ok {
		return
	}
	adminID, err := pathInt64(c, "adminId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的管理员 ID")
		return
	}
	var req UpdateMemberPositionRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.org.UpdateMemberPosition(c.Request.Context(), deptID, adminID, req.PositionID, req.JobTitle); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "成员岗位已更新", nil)
	h.recordAudit(c, "dept.member_position", "department", strconv.FormatInt(deptID, 10), fmt.Sprintf("更新成员 #%d 岗位", adminID))
}

func (h *Handler) SetMemberReporting(c *gin.Context) {
	deptID, ok := h.deptID(c)
	if !ok {
		return
	}
	adminID, err := pathInt64(c, "adminId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的管理员 ID")
		return
	}
	var req SetReportingRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.org.SetMemberReporting(c.Request.Context(), deptID, adminID, req.ReportingTo); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "汇报线已设置", nil)
	h.recordAudit(c, "dept.member_reporting", "department", strconv.FormatInt(deptID, 10), fmt.Sprintf("设置成员 #%d 汇报给 #%d", adminID, req.ReportingTo))
}

func (h *Handler) SetMemberDelegate(c *gin.Context) {
	deptID, ok := h.deptID(c)
	if !ok {
		return
	}
	adminID, err := pathInt64(c, "adminId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的管理员 ID")
		return
	}
	var req SetDelegateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	var expiresAt *time.Time
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			response.Error(c, http.StatusBadRequest, 40000, "expiresAt 格式无效，请使用 RFC3339 格式")
			return
		}
		expiresAt = &t
	}
	if err := h.org.SetMemberDelegate(c.Request.Context(), deptID, adminID, req.DelegateTo, expiresAt); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "代理人已设置", nil)
	h.recordAudit(c, "dept.member_delegate", "department", strconv.FormatInt(deptID, 10), fmt.Sprintf("设置成员 #%d 代理人为 #%d", adminID, req.DelegateTo))
}

func (h *Handler) GetReportingChain(c *gin.Context) {
	deptID, ok := h.deptID(c)
	if !ok {
		return
	}
	adminID, err := pathInt64(c, "adminId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的管理员 ID")
		return
	}
	chain, err := h.org.GetReportingChain(c.Request.Context(), deptID, adminID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", chain)
}

func (h *Handler) BatchInviteMembers(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok || session == nil {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	deptID, ok := h.deptID(c)
	if !ok {
		return
	}
	if orgID, ok2 := h.deptOrgID(c, deptID); ok2 {
		if !h.requireOrgMemberOrSuper(c, orgID) {
			return
		}
	}
	var req BatchInviteRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	invitations, err := h.org.BatchInviteMembers(c.Request.Context(), deptID, session.AdminID, req.AdminIDs, req.Message)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 201, "批量邀请已发送", invitations)
	h.recordAudit(c, "dept.batch_invite", "department", strconv.FormatInt(deptID, 10), fmt.Sprintf("批量邀请 %d 人加入部门", len(req.AdminIDs)))
}
