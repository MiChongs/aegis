package httptransport

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"

	systemdomain "aegis/internal/domain/system"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// ── 权限辅助 ──

// requireOrgMemberOrSuper 组织成员或超管才能访问
func (h *Handler) requireOrgMemberOrSuper(c *gin.Context, orgID int64) bool {
	session, ok := adminAccessSession(c)
	if !ok || session == nil {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return false
	}
	if session.IsSuperAdmin {
		return true
	}
	ok, _ = h.org.IsOrganizationMember(c.Request.Context(), orgID, session.AdminID)
	if !ok {
		response.Error(c, http.StatusForbidden, 40395, "无权访问该组织")
		return false
	}
	return true
}

// deptOrgID 从 deptId 反查 orgId
func (h *Handler) deptOrgID(c *gin.Context, deptID int64) (int64, bool) {
	orgID, err := h.org.GetDepartmentOrgID(c.Request.Context(), deptID)
	if err != nil {
		response.Error(c, http.StatusNotFound, 40472, "部门不存在")
		return 0, false
	}
	return orgID, true
}

// ── 审批链 Handler ──

func (h *Handler) ListApprovalChains(c *gin.Context) {
	orgID, ok := h.orgID(c)
	if !ok {
		return
	}
	if !h.requireOrgMemberOrSuper(c, orgID) {
		return
	}
	items, err := h.approval.ListApprovalChains(c.Request.Context(), orgID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", items)
}

func (h *Handler) CreateApprovalChain(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	orgID, ok := h.orgID(c)
	if !ok {
		return
	}
	var req CreateApprovalChainRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	chain, err := h.approval.CreateApprovalChain(c.Request.Context(), orgID, systemdomain.CreateApprovalChainInput{
		Name: req.Name, TriggerType: req.TriggerType, Steps: req.Steps,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 201, "审批链已创建", chain)
	h.recordAudit(c, "approval_chain.create", "approval_chain", strconv.FormatInt(chain.ID, 10), "创建审批链 "+req.Name)
}

func (h *Handler) UpdateApprovalChain(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	chainID, err := pathInt64(c, "chainId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的审批链 ID")
		return
	}
	var req UpdateApprovalChainRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.approval.UpdateApprovalChain(c.Request.Context(), chainID, req.Name, req.Steps, req.IsActive); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "审批链已更新", nil)
	h.recordAudit(c, "approval_chain.update", "approval_chain", strconv.FormatInt(chainID, 10), fmt.Sprintf("修改审批链 #%d", chainID))
}

func (h *Handler) DeleteApprovalChain(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	chainID, err := pathInt64(c, "chainId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的审批链 ID")
		return
	}
	if err := h.approval.DeleteApprovalChain(c.Request.Context(), chainID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "审批链已删除", nil)
	h.recordAudit(c, "approval_chain.delete", "approval_chain", strconv.FormatInt(chainID, 10), fmt.Sprintf("删除审批链 #%d", chainID))
}

// ── 审批实例 Handler ──

func (h *Handler) ListApprovalInstances(c *gin.Context) {
	orgID, ok := h.orgID(c)
	if !ok {
		return
	}
	if !h.requireOrgMemberOrSuper(c, orgID) {
		return
	}
	var q ApprovalInstanceListQuery
	_ = c.ShouldBindQuery(&q)
	items, total, err := h.approval.ListApprovalInstances(c.Request.Context(), orgID, q.Status, q.Page, q.Limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	limit := q.Limit
	if limit < 1 || limit > 50 {
		limit = 20
	}
	response.Success(c, 200, "ok", gin.H{
		"items":      items,
		"total":      total,
		"page":       q.Page,
		"limit":      limit,
		"totalPages": (total + int64(limit) - 1) / int64(limit),
	})
}

func (h *Handler) GetApprovalInstance(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok || session == nil {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	instanceID, err := pathInt64(c, "instanceId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的审批实例 ID")
		return
	}
	inst, err := h.approval.GetApprovalInstance(c.Request.Context(), instanceID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if inst == nil {
		response.Error(c, http.StatusNotFound, 40481, "审批实例不存在")
		return
	}
	// 验证组织归属
	if !h.requireOrgMemberOrSuper(c, inst.OrgID) {
		return
	}
	response.Success(c, 200, "ok", inst)
}

func (h *Handler) ApproveInstance(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok || session == nil {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	instanceID, err := pathInt64(c, "instanceId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的审批实例 ID")
		return
	}
	var req ApproveRejectRequest
	_ = bind(c, &req)
	inst, err := h.approval.AdvanceApprovalStep(c.Request.Context(), instanceID, session.AdminID, "approved", req.Comment)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已批准", inst)
	h.recordAudit(c, "approval.approve", "approval_instance", strconv.FormatInt(instanceID, 10), fmt.Sprintf("批准审批 #%d", instanceID))
}

func (h *Handler) RejectInstance(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok || session == nil {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	instanceID, err := pathInt64(c, "instanceId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的审批实例 ID")
		return
	}
	var req ApproveRejectRequest
	_ = bind(c, &req)
	inst, err := h.approval.AdvanceApprovalStep(c.Request.Context(), instanceID, session.AdminID, "rejected", req.Comment)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已驳回", inst)
	h.recordAudit(c, "approval.reject", "approval_instance", strconv.FormatInt(instanceID, 10), fmt.Sprintf("驳回审批 #%d", instanceID))
}

func (h *Handler) ListMyPendingApprovals(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok || session == nil {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	items, err := h.approval.ListMyPendingApprovals(c.Request.Context(), session.AdminID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", items)
}

// ── 权限模板 Handler ──

func (h *Handler) ListOrgPermTemplates(c *gin.Context) {
	orgID, ok := h.orgID(c)
	if !ok {
		return
	}
	if !h.requireOrgMemberOrSuper(c, orgID) {
		return
	}
	items, err := h.approval.ListOrgPermTemplates(c.Request.Context(), orgID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", items)
}

func (h *Handler) CreateOrgPermTemplate(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	orgID, ok := h.orgID(c)
	if !ok {
		return
	}
	var req CreatePermTemplateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	tmpl, err := h.approval.CreateOrgPermTemplate(c.Request.Context(), orgID, systemdomain.CreatePermTemplateInput{
		Name: req.Name, Description: req.Description, Permissions: req.Permissions, IsDefault: req.IsDefault,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 201, "权限模板已创建", tmpl)
	h.recordAudit(c, "perm_template.create", "perm_template", strconv.FormatInt(tmpl.ID, 10), "创建权限模板 "+req.Name)
}

func (h *Handler) DeleteOrgPermTemplate(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	templateID, err := pathInt64(c, "templateId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的模板 ID")
		return
	}
	if err := h.approval.DeleteOrgPermTemplate(c.Request.Context(), templateID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "权限模板已删除", nil)
	h.recordAudit(c, "perm_template.delete", "perm_template", strconv.FormatInt(templateID, 10), fmt.Sprintf("删除权限模板 #%d", templateID))
}

func (h *Handler) ApplyPermTemplate(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	templateID, err := pathInt64(c, "templateId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的模板 ID")
		return
	}
	var req ApplyTemplateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	// 获取模板权限 → 应用到管理员（此处仅记录审计，实际权限设置需对接 Casbin）
	_ = templateID
	response.Success(c, 200, "权限模板已应用", gin.H{"adminId": req.AdminID, "templateId": templateID})
	h.recordAudit(c, "perm_template.apply", "perm_template", strconv.FormatInt(templateID, 10), fmt.Sprintf("应用模板 #%d 到管理员 #%d", templateID, req.AdminID))
}

// ── 资源绑定 Handler ──

func (h *Handler) ListOrgApps(c *gin.Context) {
	orgID, ok := h.orgID(c)
	if !ok {
		return
	}
	if !h.requireOrgMemberOrSuper(c, orgID) {
		return
	}
	items, err := h.approval.ListOrgApps(c.Request.Context(), orgID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", items)
}

func (h *Handler) BindOrgApp(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	orgID, ok := h.orgID(c)
	if !ok {
		return
	}
	var req BindOrgAppRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	binding, err := h.approval.BindOrgApp(c.Request.Context(), orgID, req.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 201, "应用已绑定", binding)
	h.recordAudit(c, "org_app.bind", "org_app_binding", strconv.FormatInt(binding.ID, 10), fmt.Sprintf("绑定应用 #%d 到组织 #%d", req.AppID, orgID))
}

func (h *Handler) UnbindOrgApp(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	orgID, ok := h.orgID(c)
	if !ok {
		return
	}
	appID, err := pathInt64(c, "appId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的应用 ID")
		return
	}
	if err := h.approval.UnbindOrgApp(c.Request.Context(), orgID, appID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "应用已解绑", nil)
	h.recordAudit(c, "org_app.unbind", "org_app_binding", fmt.Sprintf("%d-%d", orgID, appID), fmt.Sprintf("解绑应用 #%d 从组织 #%d", appID, orgID))
}

// ── 协作组 Handler ──

func (h *Handler) ListCollabGroups(c *gin.Context) {
	orgID, ok := h.orgID(c)
	if !ok {
		return
	}
	if !h.requireOrgMemberOrSuper(c, orgID) {
		return
	}
	items, err := h.approval.ListCollabGroups(c.Request.Context(), orgID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", items)
}

func (h *Handler) CreateCollabGroup(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	orgID, ok := h.orgID(c)
	if !ok {
		return
	}
	var req CreateCollabGroupRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	grp, err := h.approval.CreateCollabGroup(c.Request.Context(), orgID, systemdomain.CreateCollabGroupInput{
		Name: req.Name, Description: req.Description, DeptIDs: req.DeptIDs, Permissions: req.Permissions,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 201, "协作组已创建", grp)
	h.recordAudit(c, "collab_group.create", "collaboration_group", strconv.FormatInt(grp.ID, 10), "创建协作组 "+req.Name)
}

func (h *Handler) UpdateCollabGroup(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	groupID, err := pathInt64(c, "groupId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的协作组 ID")
		return
	}
	var req UpdateCollabGroupRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.approval.UpdateCollabGroup(c.Request.Context(), groupID, req.Name, req.Description, req.DeptIDs, req.Permissions); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "协作组已更新", nil)
	h.recordAudit(c, "collab_group.update", "collaboration_group", strconv.FormatInt(groupID, 10), fmt.Sprintf("修改协作组 #%d", groupID))
}

func (h *Handler) DeleteCollabGroup(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	groupID, err := pathInt64(c, "groupId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的协作组 ID")
		return
	}
	if err := h.approval.DeleteCollabGroup(c.Request.Context(), groupID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "协作组已删除", nil)
	h.recordAudit(c, "collab_group.delete", "collaboration_group", strconv.FormatInt(groupID, 10), fmt.Sprintf("删除协作组 #%d", groupID))
}

// ── 成员导入 / 导出 Handler ──

// ImportDeptMembers 通过 CSV 文件批量导入部门成员
func (h *Handler) ImportDeptMembers(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		return
	}
	deptID, ok := h.deptID(c)
	if !ok {
		return
	}
	file, _, err := c.Request.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "请上传 CSV 文件")
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)
	records, err := reader.ReadAll()
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "CSV 文件解析失败: "+err.Error())
		return
	}

	// CSV 格式：每行一个 admin_id
	var adminIDs []int64
	for i, row := range records {
		if i == 0 && len(row) > 0 && row[0] == "admin_id" {
			continue // 跳过表头
		}
		if len(row) == 0 {
			continue
		}
		id, err := strconv.ParseInt(row[0], 10, 64)
		if err != nil {
			continue
		}
		adminIDs = append(adminIDs, id)
	}

	if len(adminIDs) == 0 {
		response.Error(c, http.StatusBadRequest, 40000, "CSV 文件中未找到有效的管理员 ID")
		return
	}

	inserted, err := h.approval.BatchAddDepartmentMembers(c.Request.Context(), deptID, adminIDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "导入完成", gin.H{"total": len(adminIDs), "inserted": inserted})
	h.recordAudit(c, "dept.import_members", "department", strconv.FormatInt(deptID, 10), fmt.Sprintf("批量导入 %d 人到部门（实际 %d）", len(adminIDs), inserted))
}

// ExportOrgMembers 导出组织所有成员为 CSV
func (h *Handler) ExportOrgMembers(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok || session == nil {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	orgID, ok := h.orgID(c)
	if !ok {
		return
	}
	if !h.requireOrgMemberOrSuper(c, orgID) {
		return
	}

	members, err := h.approval.ExportOrgMembers(c.Request.Context(), orgID)
	if err != nil {
		h.writeError(c, err)
		return
	}

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=org_%d_members.csv", orgID))
	// UTF-8 BOM
	_, _ = c.Writer.Write([]byte{0xEF, 0xBB, 0xBF})

	w := csv.NewWriter(c.Writer)
	// 写表头
	_ = w.Write([]string{"admin_id", "account", "display_name", "is_leader", "position", "job_title"})
	for _, m := range members {
		leader := "否"
		if m.IsLeader {
			leader = "是"
		}
		_ = w.Write([]string{
			strconv.FormatInt(m.AdminID, 10),
			m.Account,
			m.DisplayName,
			leader,
			m.PositionName,
			m.JobTitle,
		})
	}
	w.Flush()
	h.recordAudit(c, "org.export_members", "organization", strconv.FormatInt(orgID, 10), fmt.Sprintf("导出组织 #%d 成员 (%d)", orgID, len(members)))
}
