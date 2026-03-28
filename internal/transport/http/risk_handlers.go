package httptransport

import (
	"fmt"
	"net/http"
	"time"

	securitydomain "aegis/internal/domain/security"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// ════════════════════════════════════════════════════════════
//  风险规则
// ════════════════════════════════════════════════════════════

// AdminListRiskRules 列出风险规则
// GET /api/admin/system/risk/rules
func (h *Handler) AdminListRiskRules(c *gin.Context) {
	scene := c.Query("scene")
	rules, err := h.risk.ListRiskRules(c.Request.Context(), scene)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", rules)
}

// AdminCreateRiskRule 创建风险规则
// POST /api/admin/system/risk/rules
func (h *Handler) AdminCreateRiskRule(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理风控规则")
		return
	}
	var req RiskRuleCreateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	input := securitydomain.CreateRiskRuleInput{
		Name:          req.Name,
		Description:   req.Description,
		Scene:         req.Scene,
		ConditionType: req.ConditionType,
		ConditionData: req.ConditionData,
		Score:         req.Score,
		Priority:      req.Priority,
	}
	rule, err := h.risk.CreateRiskRule(c.Request.Context(), input, session.AdminID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "create", "risk_rule", fmt.Sprintf("%d", rule.ID), fmt.Sprintf("创建风险规则: %s", rule.Name))
	response.Success(c, 200, "创建成功", rule)
}

// AdminUpdateRiskRule 更新风险规则
// PUT /api/admin/system/risk/rules/:id
func (h *Handler) AdminUpdateRiskRule(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理风控规则")
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的规则 ID")
		return
	}
	var req RiskRuleUpdateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	input := securitydomain.UpdateRiskRuleInput{
		Name:          req.Name,
		Description:   req.Description,
		ConditionData: req.ConditionData,
		Score:         req.Score,
		IsActive:      req.IsActive,
		Priority:      req.Priority,
	}
	if err := h.risk.UpdateRiskRule(c.Request.Context(), id, input); err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "update", "risk_rule", fmt.Sprintf("%d", id), "更新风险规则")
	response.Success(c, 200, "更新成功", nil)
}

// AdminDeleteRiskRule 删除风险规则
// DELETE /api/admin/system/risk/rules/:id
func (h *Handler) AdminDeleteRiskRule(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理风控规则")
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的规则 ID")
		return
	}
	if err := h.risk.DeleteRiskRule(c.Request.Context(), id); err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "delete", "risk_rule", fmt.Sprintf("%d", id), "删除风险规则")
	response.Success(c, 200, "删除成功", nil)
}

// ════════════════════════════════════════════════════════════
//  评估记录
// ════════════════════════════════════════════════════════════

// AdminListRiskAssessments 列出评估记录
// GET /api/admin/system/risk/assessments
func (h *Handler) AdminListRiskAssessments(c *gin.Context) {
	var req RiskAssessmentListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if req.PageSize < 1 {
		req.PageSize = 20
	}
	items, total, err := h.risk.ListRiskAssessments(c.Request.Context(), req.Scene, req.RiskLevel, req.Action, req.Page, req.PageSize)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"list": items, "total": total})
}

// AdminGetRiskAssessment 获取评估详情
// GET /api/admin/system/risk/assessments/:id
func (h *Handler) AdminGetRiskAssessment(c *gin.Context) {
	id, err := pathInt64(c, "id")
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的评估记录 ID")
		return
	}
	item, err := h.risk.GetRiskAssessment(c.Request.Context(), id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if item == nil {
		response.Error(c, http.StatusNotFound, 40400, "评估记录不存在")
		return
	}
	response.Success(c, 200, "获取成功", item)
}

// AdminListPendingReviews 列出待复核记录
// GET /api/admin/system/risk/reviews/pending
func (h *Handler) AdminListPendingReviews(c *gin.Context) {
	var req RiskPageRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if req.PageSize < 1 {
		req.PageSize = 20
	}
	items, total, err := h.risk.ListPendingReviews(c.Request.Context(), req.Page, req.PageSize)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"list": items, "total": total})
}

// AdminReviewRiskAssessment 复核评估记录
// POST /api/admin/system/risk/assessments/:id/review
func (h *Handler) AdminReviewRiskAssessment(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可复核风控记录")
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的评估记录 ID")
		return
	}
	var req RiskReviewRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.risk.ReviewRiskAssessment(c.Request.Context(), id, session.AdminID, req.Result, req.Comment); err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "review", "risk_assessment", fmt.Sprintf("%d", id), fmt.Sprintf("复核结果: %s", req.Result))
	response.Success(c, 200, "复核成功", nil)
}

// ════════════════════════════════════════════════════════════
//  设备指纹
// ════════════════════════════════════════════════════════════

// AdminListSuspiciousDevices 列出可疑设备
// GET /api/admin/system/risk/devices/suspicious
func (h *Handler) AdminListSuspiciousDevices(c *gin.Context) {
	var req RiskPageRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if req.PageSize < 1 {
		req.PageSize = 20
	}
	items, total, err := h.risk.ListSuspiciousDevices(c.Request.Context(), req.Page, req.PageSize)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"list": items, "total": total})
}

// AdminGetDeviceFingerprint 查询设备指纹
// GET /api/admin/system/risk/devices/:deviceId
func (h *Handler) AdminGetDeviceFingerprint(c *gin.Context) {
	deviceID := c.Param("deviceId")
	if deviceID == "" {
		response.Error(c, http.StatusBadRequest, 40000, "设备 ID 不能为空")
		return
	}
	fp, err := h.risk.GetDeviceFingerprint(c.Request.Context(), deviceID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if fp == nil {
		response.Error(c, http.StatusNotFound, 40400, "设备指纹不存在")
		return
	}
	response.Success(c, 200, "获取成功", fp)
}

// AdminUpdateDeviceRiskTag 更新设备风险标签
// PUT /api/admin/system/risk/devices/:id/tag
func (h *Handler) AdminUpdateDeviceRiskTag(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理设备风险标签")
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的设备 ID")
		return
	}
	var req DeviceRiskTagRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.risk.UpdateDeviceRiskTag(c.Request.Context(), id, req.Tag); err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "update", "device_fingerprint", fmt.Sprintf("%d", id), fmt.Sprintf("更新设备风险标签: %s", req.Tag))
	response.Success(c, 200, "更新成功", nil)
}

// ════════════════════════════════════════════════════════════
//  IP 风险库
// ════════════════════════════════════════════════════════════

// AdminListHighRiskIPs 列出高风险 IP
// GET /api/admin/system/risk/ips
func (h *Handler) AdminListHighRiskIPs(c *gin.Context) {
	var req RiskPageRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if req.PageSize < 1 {
		req.PageSize = 20
	}
	items, total, err := h.risk.ListHighRiskIPs(c.Request.Context(), req.Page, req.PageSize)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"list": items, "total": total})
}

// AdminGetIPRisk 查询 IP 风险信息
// GET /api/admin/system/risk/ips/:ip
func (h *Handler) AdminGetIPRisk(c *gin.Context) {
	ip := c.Param("ip")
	if ip == "" {
		response.Error(c, http.StatusBadRequest, 40000, "IP 地址不能为空")
		return
	}
	rec, err := h.risk.GetIPRisk(c.Request.Context(), ip)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if rec == nil {
		response.Error(c, http.StatusNotFound, 40400, "IP 风险记录不存在")
		return
	}
	response.Success(c, 200, "获取成功", rec)
}

// AdminUpdateIPRiskTag 更新 IP 风险标签
// PUT /api/admin/system/risk/ips/:id/tag
func (h *Handler) AdminUpdateIPRiskTag(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理 IP 风险标签")
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的 IP 记录 ID")
		return
	}
	var req IPRiskTagRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.risk.UpdateIPRiskTag(c.Request.Context(), id, req.Tag); err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "update", "ip_risk_record", fmt.Sprintf("%d", id), fmt.Sprintf("更新 IP 风险标签: %s", req.Tag))
	response.Success(c, 200, "更新成功", nil)
}

// ════════════════════════════════════════════════════════════
//  处置策略
// ════════════════════════════════════════════════════════════

// AdminListRiskActions 列出处置策略
// GET /api/admin/system/risk/actions
func (h *Handler) AdminListRiskActions(c *gin.Context) {
	scene := c.Query("scene")
	actions, err := h.risk.ListRiskActions(c.Request.Context(), scene)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", actions)
}

// AdminCreateRiskAction 创建处置策略
// POST /api/admin/system/risk/actions
func (h *Handler) AdminCreateRiskAction(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理处置策略")
		return
	}
	var req RiskActionCreateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	input := securitydomain.CreateRiskActionInput{
		Scene:       req.Scene,
		MinScore:    req.MinScore,
		MaxScore:    req.MaxScore,
		Action:      req.Action,
		BanDuration: req.BanDuration,
		Description: req.Description,
	}
	action, err := h.risk.CreateRiskAction(c.Request.Context(), input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "create", "risk_action", fmt.Sprintf("%d", action.ID), fmt.Sprintf("创建处置策略: %s/%s", req.Scene, req.Action))
	response.Success(c, 200, "创建成功", action)
}

// AdminUpdateRiskAction 更新处置策略（启用/禁用）
// PUT /api/admin/system/risk/actions/:id
func (h *Handler) AdminUpdateRiskAction(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理处置策略")
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的策略 ID")
		return
	}
	var req RiskActionUpdateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.risk.UpdateRiskAction(c.Request.Context(), id, req.IsActive); err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "update", "risk_action", fmt.Sprintf("%d", id), fmt.Sprintf("更新处置策略启用状态: %v", req.IsActive))
	response.Success(c, 200, "更新成功", nil)
}

// AdminDeleteRiskAction 删除处置策略
// DELETE /api/admin/system/risk/actions/:id
func (h *Handler) AdminDeleteRiskAction(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理处置策略")
		return
	}
	id, err := pathInt64(c, "id")
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的策略 ID")
		return
	}
	if err := h.risk.DeleteRiskAction(c.Request.Context(), id); err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "delete", "risk_action", fmt.Sprintf("%d", id), "删除处置策略")
	response.Success(c, 200, "删除成功", nil)
}

// ════════════════════════════════════════════════════════════
//  评估 / 模拟
// ════════════════════════════════════════════════════════════

// AdminEvaluateRisk 手动触发风险评估
// POST /api/admin/system/risk/evaluate
func (h *Handler) AdminEvaluateRisk(c *gin.Context) {
	var req RiskEvalRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	evalReq := securitydomain.RiskEvalRequest{
		Scene:     req.Scene,
		AppID:     req.AppID,
		UserID:    req.UserID,
		IP:        req.IP,
		DeviceID:  req.DeviceID,
		UserAgent: req.UserAgent,
		Extra:     req.Extra,
	}
	result, err := h.risk.EvaluateRisk(c.Request.Context(), evalReq)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "评估完成", result)
}

// AdminSimulateRisk 模拟规则评估
// POST /api/admin/system/risk/rules/:id/simulate
func (h *Handler) AdminSimulateRisk(c *gin.Context) {
	ruleID, err := pathInt64(c, "id")
	if err != nil || ruleID < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的规则 ID")
		return
	}
	var req RiskSimulateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	input := securitydomain.SimulateInput{
		Scene:     req.Scene,
		IP:        req.IP,
		DeviceID:  req.DeviceID,
		UserAgent: req.UserAgent,
	}
	result, err := h.risk.SimulateRule(c.Request.Context(), ruleID, input)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "模拟完成", result)
}

// ════════════════════════════════════════════════════════════
//  统计大盘
// ════════════════════════════════════════════════════════════

// AdminRiskDashboard 风控大盘统计
// GET /api/admin/system/risk/dashboard
func (h *Handler) AdminRiskDashboard(c *gin.Context) {
	var req RiskDashboardRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	// 默认最近 7 天
	end := time.Now().UTC()
	start := end.Add(-7 * 24 * time.Hour)
	if req.Start != "" {
		if t, err := time.Parse(time.RFC3339, req.Start); err == nil {
			start = t.UTC()
		}
	}
	if req.End != "" {
		if t, err := time.Parse(time.RFC3339, req.End); err == nil {
			end = t.UTC()
		}
	}
	dash, err := h.risk.GetRiskDashboard(c.Request.Context(), start, end)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", dash)
}
