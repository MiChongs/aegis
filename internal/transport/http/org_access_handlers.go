package httptransport

import (
	"net/http"
	"strconv"
	"strings"

	orgdomain "aegis/internal/domain/organization"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 审批链 / 权限模板 / 协作组。与组织 Handler 同一约定：
// 权限判定在 OrgApprovalService 内完成，这里只搬参数。

// ── 审批链 ──

// ListApprovalChains 审批链列表
func (h *Handler) ListApprovalChains(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	chains, err := h.orgApproval.ListChains(c.Request.Context(), session, orgUUIDParam(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OK(c, chains)
}

// CreateApprovalChain 创建审批链
func (h *Handler) CreateApprovalChain(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req ApprovalChainRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	chain, err := h.orgApproval.CreateChain(c.Request.Context(), session, orgUUIDParam(c),
		orgdomain.CreateApprovalChainInput{
			Name: req.Name, TriggerType: req.TriggerType,
			Steps: toApprovalSteps(req.Steps), IsActive: req.IsActive,
		})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusCreated, "审批链已创建", chain)
	h.recordAudit(c, "approval.chain_create", "approval_chain", chain.UUID, "创建审批链 "+chain.Name)
}

// UpdateApprovalChain 更新审批链
func (h *Handler) UpdateApprovalChain(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req UpdateApprovalChainRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	input := orgdomain.UpdateApprovalChainInput{Name: req.Name, IsActive: req.IsActive}
	if req.Steps != nil {
		steps := toApprovalSteps(*req.Steps)
		input.Steps = &steps
	}
	chain, err := h.orgApproval.UpdateChain(c.Request.Context(), session, orgUUIDParam(c),
		strings.TrimSpace(c.Param("chainId")), input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OKWithMessage(c, "审批链已更新", chain)
	h.recordAudit(c, "approval.chain_update", "approval_chain", chain.UUID, "更新审批链 "+chain.Name)
}

// DeleteApprovalChain 删除审批链
func (h *Handler) DeleteApprovalChain(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	chainUUID := strings.TrimSpace(c.Param("chainId"))
	if err := h.orgApproval.DeleteChain(c.Request.Context(), session, orgUUIDParam(c), chainUUID); err != nil {
		h.writeError(c, err)
		return
	}
	response.OKWithMessage(c, "审批链已删除", nil)
	h.recordAudit(c, "approval.chain_delete", "approval_chain", chainUUID, "删除审批链")
}

// ── 审批实例 ──

// ListApprovalInstances 审批记录
func (h *Handler) ListApprovalInstances(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	page, _ := strconv.Atoi(c.Query("page"))
	limit, _ := strconv.Atoi(c.Query("limit"))
	result, err := h.orgApproval.ListInstances(c.Request.Context(), session, orgUUIDParam(c), c.Query("status"), page, limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OK(c, result)
}

// GetApprovalInstance 审批详情
func (h *Handler) GetApprovalInstance(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	inst, err := h.orgApproval.GetInstance(c.Request.Context(), session, orgUUIDParam(c),
		strings.TrimSpace(c.Param("instanceId")))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OK(c, inst)
}

// ListMyPendingApprovals 待我审批（跨组织聚合）
func (h *Handler) ListMyPendingApprovals(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	items, err := h.orgApproval.ListMyPending(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OK(c, items)
}

// DecideApproval 审批通过 / 驳回
func (h *Handler) DecideApproval(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req ApprovalDecisionRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	instUUID := strings.TrimSpace(c.Param("instanceId"))
	inst, err := h.orgApproval.Advance(c.Request.Context(), session, instUUID, req.Action, req.Comment)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OKWithMessage(c, "审批已提交", inst)
	h.recordAudit(c, "approval.decide", "approval", instUUID, "审批："+req.Action)
}

// ── 权限模板 ──

// ListOrgPermTemplates 权限模板列表
func (h *Handler) ListOrgPermTemplates(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	items, err := h.orgApproval.ListTemplates(c.Request.Context(), session, orgUUIDParam(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OK(c, items)
}

// CreateOrgPermTemplate 创建权限模板
func (h *Handler) CreateOrgPermTemplate(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req PermTemplateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	tpl, err := h.orgApproval.CreateTemplate(c.Request.Context(), session, orgUUIDParam(c),
		orgdomain.CreatePermTemplateInput{
			Name: req.Name, Description: req.Description,
			Permissions: req.Permissions, IsDefault: req.IsDefault,
		})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusCreated, "权限模板已创建", tpl)
	h.recordAudit(c, "template.create", "perm_template", tpl.UUID, "创建权限模板 "+tpl.Name)
}

// DeleteOrgPermTemplate 删除权限模板
func (h *Handler) DeleteOrgPermTemplate(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	templateUUID := strings.TrimSpace(c.Param("templateId"))
	if err := h.orgApproval.DeleteTemplate(c.Request.Context(), session, orgUUIDParam(c), templateUUID); err != nil {
		h.writeError(c, err)
		return
	}
	response.OKWithMessage(c, "权限模板已删除", nil)
	h.recordAudit(c, "template.delete", "perm_template", templateUUID, "删除权限模板")
}

// ApplyPermTemplate 套用权限模板：落成组织角色并授予指定成员
func (h *Handler) ApplyPermTemplate(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req ApplyTemplateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	templateUUID := strings.TrimSpace(c.Param("templateId"))
	role, granted, err := h.orgApproval.ApplyTemplate(c.Request.Context(), session, orgUUIDParam(c), templateUUID,
		orgdomain.ApplyTemplateInput{
			AdminIDs: req.AdminIDs, ScopeDeptUUID: req.ScopeDeptID, RoleName: req.RoleName,
		})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OKWithMessage(c, "模板已套用", gin.H{"role": role, "granted": granted})
	h.recordAudit(c, "template.apply", "perm_template", templateUUID,
		"套用模板给 "+strconv.Itoa(granted)+" 位成员")
}

// ── 协作组 ──

// ListCollabGroups 协作组列表
func (h *Handler) ListCollabGroups(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	items, err := h.orgApproval.ListCollabGroups(c.Request.Context(), session, orgUUIDParam(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.OK(c, items)
}

// CreateCollabGroup 创建协作组
func (h *Handler) CreateCollabGroup(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req CollabGroupRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	group, err := h.orgApproval.CreateCollabGroup(c.Request.Context(), session, orgUUIDParam(c),
		orgdomain.CollabGroupInput{
			Name: req.Name, Description: req.Description,
			DeptUUIDs: req.DeptIDs, Permissions: req.Permissions,
		})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusCreated, "协作组已创建", group)
	h.recordAudit(c, "collab.create", "collab_group", group.UUID, "创建协作组 "+group.Name)
}

// UpdateCollabGroup 更新协作组
func (h *Handler) UpdateCollabGroup(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	var req CollabGroupRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	groupUUID := strings.TrimSpace(c.Param("groupId"))
	if err := h.orgApproval.UpdateCollabGroup(c.Request.Context(), session, orgUUIDParam(c), groupUUID,
		orgdomain.CollabGroupInput{
			Name: req.Name, Description: req.Description,
			DeptUUIDs: req.DeptIDs, Permissions: req.Permissions,
		}); err != nil {
		h.writeError(c, err)
		return
	}
	response.OKWithMessage(c, "协作组已更新", nil)
	h.recordAudit(c, "collab.update", "collab_group", groupUUID, "更新协作组")
}

// DeleteCollabGroup 删除协作组
func (h *Handler) DeleteCollabGroup(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	groupUUID := strings.TrimSpace(c.Param("groupId"))
	if err := h.orgApproval.DeleteCollabGroup(c.Request.Context(), session, orgUUIDParam(c), groupUUID); err != nil {
		h.writeError(c, err)
		return
	}
	response.OKWithMessage(c, "协作组已删除", nil)
	h.recordAudit(c, "collab.delete", "collab_group", groupUUID, "删除协作组")
}

func toApprovalSteps(items []ApprovalStepPayload) []orgdomain.ApprovalStep {
	steps := make([]orgdomain.ApprovalStep, 0, len(items))
	for i, item := range items {
		order := item.Order
		if order == 0 {
			order = i + 1
		}
		steps = append(steps, orgdomain.ApprovalStep{
			ApproverType: item.ApproverType, ApproverID: item.ApproverID,
			ApproverRole: item.ApproverRole, Name: item.Name, Order: order,
		})
	}
	return steps
}
