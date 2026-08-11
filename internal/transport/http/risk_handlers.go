package httptransport

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	securitydomain "aegis/internal/domain/security"
	"aegis/pkg/response"
	"aegis/pkg/timeutil"

	"github.com/gin-gonic/gin"
)

// 风控中心管理端。
//
// 写操作一律限超管（与旧实现一致），读操作对所有能进 /api/admin/system 的管理员开放 ——
// 风控留痕是排查故障时的第一手材料，把它锁死在超管手里会让一线运维无从下手。

const riskForbiddenMsg = "仅超级管理员可管理风控配置"

func requireRiskAdmin(c *gin.Context) bool {
	if _, ok := requireSuperAdminSession(c); ok {
		return true
	}
	response.Error(c, http.StatusForbidden, 40313, riskForbiddenMsg)
	return false
}

// ════════════════════════════════════════════════════════════
//  自描述目录
// ════════════════════════════════════════════════════════════

// AdminRiskMetadata 风控中心目录（场景 / 条件类型参数 schema / 表达式变量 / 等级图例）
// GET /api/admin/system/risk/metadata
func (h *Handler) AdminRiskMetadata(c *gin.Context) {
	response.Success(c, 200, "获取成功", h.risk.Metadata())
}

// AdminValidateRiskExpression 校验一段 expr 表达式
// POST /api/admin/system/risk/expression/validate
func (h *Handler) AdminValidateRiskExpression(c *gin.Context) {
	var req RiskExprValidateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	response.Success(c, 200, "校验完成", h.risk.ValidateExpression(req.Expression))
}

// ════════════════════════════════════════════════════════════
//  风险规则
// ════════════════════════════════════════════════════════════

// AdminListRiskRules 列出风险规则
// GET /api/admin/system/risk/rules
func (h *Handler) AdminListRiskRules(c *gin.Context) {
	rules, err := h.risk.ListRiskRules(c.Request.Context(), c.Query("scene"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", rules)
}

// AdminGetRiskRule 规则详情：定义 + 效果统计 + 最近命中 + 命中趋势
// GET /api/admin/system/risk/rules/:id
func (h *Handler) AdminGetRiskRule(c *gin.Context) {
	id, err := pathInt64(c, "id")
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的规则 ID")
		return
	}
	start, end := resolveRiskRange(c.Query("start"), c.Query("end"))
	detail, err := h.risk.GetRiskRuleDetail(c.Request.Context(), id, start, end)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if detail == nil {
		response.Error(c, http.StatusNotFound, 40400, "规则不存在")
		return
	}
	response.Success(c, 200, "获取成功", detail)
}

// AdminCreateRiskRule 创建风险规则
// POST /api/admin/system/risk/rules
func (h *Handler) AdminCreateRiskRule(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, riskForbiddenMsg)
		return
	}
	var req RiskRuleCreateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	rule, err := h.risk.CreateRiskRule(c.Request.Context(), securitydomain.CreateRiskRuleInput{
		Name:          req.Name,
		Description:   req.Description,
		Scene:         req.Scene,
		ConditionType: req.ConditionType,
		ConditionData: req.ConditionData,
		Score:         req.Score,
		Priority:      req.Priority,
		IsActive:      req.IsActive,
	}, session.AdminID)
	if err != nil {
		// 规则配置错误是使用者的输入问题，不是服务端故障。
		// 走 writeError 会被当成 500，管理员看不到"哪里写错了"。
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.recordAudit(c, "create", "risk_rule", strconv.FormatInt(rule.ID, 10), fmt.Sprintf("创建风险规则: %s", rule.Name))
	response.Success(c, 200, "创建成功", rule)
}

// AdminUpdateRiskRule 更新风险规则
// PUT /api/admin/system/risk/rules/:id
func (h *Handler) AdminUpdateRiskRule(c *gin.Context) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, riskForbiddenMsg)
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
	adminID := session.AdminID
	if err := h.risk.UpdateRiskRule(c.Request.Context(), id, securitydomain.UpdateRiskRuleInput{
		Name:          req.Name,
		Description:   req.Description,
		Scene:         req.Scene,
		ConditionType: req.ConditionType,
		ConditionData: req.ConditionData,
		Score:         req.Score,
		IsActive:      req.IsActive,
		Priority:      req.Priority,
		UpdatedBy:     &adminID,
	}); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.recordAudit(c, "update", "risk_rule", strconv.FormatInt(id, 10), "更新风险规则")
	response.Success(c, 200, "更新成功", nil)
}

// AdminDeleteRiskRule 删除风险规则
// DELETE /api/admin/system/risk/rules/:id
func (h *Handler) AdminDeleteRiskRule(c *gin.Context) {
	if !requireRiskAdmin(c) {
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
	h.recordAudit(c, "delete", "risk_rule", strconv.FormatInt(id, 10), "删除风险规则")
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
	query := securitydomain.AssessmentQuery{
		Scene:     req.Scene,
		RiskLevel: req.RiskLevel,
		Action:    req.Action,
		IP:        strings.TrimSpace(req.IP),
		DeviceID:  strings.TrimSpace(req.DeviceID),
		Account:   strings.TrimSpace(req.Account),
		Keyword:   strings.TrimSpace(req.Keyword),
		RuleID:    req.RuleID,
		MinScore:  req.MinScore,
		MaxScore:  req.MaxScore,
		Page:      req.Page,
		PageSize:  req.PageSize,
	}
	if parsed, ok := parseOptionalBool(req.Reviewed); ok {
		query.Reviewed = &parsed
	}
	if at, err := time.Parse(time.RFC3339, req.Start); err == nil {
		query.Start = &at
	}
	if at, err := time.Parse(time.RFC3339, req.End); err == nil {
		query.End = &at
	}

	items, total, err := h.risk.ListRiskAssessments(c.Request.Context(), query)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"list": items, "total": total})
}

// AdminGetRiskAssessment 评估详情：记录 + 判据 + 同 IP / 同设备 / 同账号的近期行为
// GET /api/admin/system/risk/assessments/:id
func (h *Handler) AdminGetRiskAssessment(c *gin.Context) {
	id, err := pathInt64(c, "id")
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的评估记录 ID")
		return
	}
	detail, err := h.risk.GetRiskAssessmentDetail(c.Request.Context(), id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if detail == nil {
		response.Error(c, http.StatusNotFound, 40400, "评估记录不存在")
		return
	}
	response.Success(c, 200, "获取成功", detail)
}

// AdminReplayRiskAssessment 用历史上下文重跑当前规则集
// POST /api/admin/system/risk/assessments/:id/replay
func (h *Handler) AdminReplayRiskAssessment(c *gin.Context) {
	id, err := pathInt64(c, "id")
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的评估记录 ID")
		return
	}
	result, err := h.risk.ReplayAssessment(c.Request.Context(), id)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	response.Success(c, 200, "重放完成", result)
}

// AdminListPendingReviews 列出待复核记录
// GET /api/admin/system/risk/reviews/pending
func (h *Handler) AdminListPendingReviews(c *gin.Context) {
	var req RiskPageRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
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
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.recordAudit(c, "review", "risk_assessment", strconv.FormatInt(id, 10), fmt.Sprintf("复核结果: %s", req.Result))
	response.Success(c, 200, "复核成功", nil)
}

// AdminPurgeRiskAssessments 清理历史评估记录
// DELETE /api/admin/system/risk/assessments
func (h *Handler) AdminPurgeRiskAssessments(c *gin.Context) {
	if !requireRiskAdmin(c) {
		return
	}
	var req RiskPurgeRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	before := timeutil.NowUTC().AddDate(0, 0, -req.Days)
	removed, err := h.risk.PurgeAssessments(c.Request.Context(), before)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.recordAudit(c, "delete", "risk_assessment", "", fmt.Sprintf("清理 %d 天前的评估记录，共 %d 条", req.Days, removed))
	response.Success(c, 200, "清理完成", gin.H{"removed": removed, "before": before})
}

// ════════════════════════════════════════════════════════════
//  设备指纹
// ════════════════════════════════════════════════════════════

// AdminListRiskDevices 设备列表
// GET /api/admin/system/risk/devices
func (h *Handler) AdminListRiskDevices(c *gin.Context) {
	query, ok := bindRiskEntityQuery(c)
	if !ok {
		return
	}
	items, total, err := h.risk.ListDevices(c.Request.Context(), query)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"list": items, "total": total})
}

// AdminListSuspiciousDevices 可疑设备列表（保留旧路由，等价于 onlyRisk=true）
// GET /api/admin/system/risk/devices/suspicious
func (h *Handler) AdminListSuspiciousDevices(c *gin.Context) {
	var req RiskPageRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, total, err := h.risk.ListDevices(c.Request.Context(), securitydomain.EntityQuery{
		OnlyRisk: true, Page: req.Page, PageSize: req.PageSize,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"list": items, "total": total})
}

// AdminGetDeviceFingerprint 设备详情：档案 + 画像 + 最近评估 + 关联 IP / 账号
// GET /api/admin/system/risk/devices/:deviceId
func (h *Handler) AdminGetDeviceFingerprint(c *gin.Context) {
	deviceID := strings.TrimSpace(c.Param("deviceId"))
	if deviceID == "" {
		response.Error(c, http.StatusBadRequest, 40000, "设备 ID 不能为空")
		return
	}
	detail, err := h.risk.GetDeviceDetail(c.Request.Context(), deviceID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if detail == nil {
		response.Error(c, http.StatusNotFound, 40400, "设备指纹不存在")
		return
	}
	response.Success(c, 200, "获取成功", detail)
}

// AdminUpdateDeviceRiskTag 更新设备风险标签
// PUT /api/admin/system/risk/devices/:id/tag
func (h *Handler) AdminUpdateDeviceRiskTag(c *gin.Context) {
	if !requireRiskAdmin(c) {
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
	if err := h.risk.UpdateDeviceRiskTag(c.Request.Context(), id, req.Tag, req.Note); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.recordAudit(c, "update", "device_fingerprint", strconv.FormatInt(id, 10), fmt.Sprintf("更新设备风险标签: %s", req.Tag))
	response.Success(c, 200, "更新成功", nil)
}

// ════════════════════════════════════════════════════════════
//  IP 风险库
// ════════════════════════════════════════════════════════════

// AdminListHighRiskIPs IP 风险库列表
// GET /api/admin/system/risk/ips
func (h *Handler) AdminListHighRiskIPs(c *gin.Context) {
	query, ok := bindRiskEntityQuery(c)
	if !ok {
		return
	}
	items, total, err := h.risk.ListIPRecords(c.Request.Context(), query)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"list": items, "total": total})
}

// AdminGetIPRisk IP 详情：档案 + 画像 + 最近评估 + 关联设备 / 账号
// GET /api/admin/system/risk/ips/:ip
func (h *Handler) AdminGetIPRisk(c *gin.Context) {
	ip := strings.TrimSpace(c.Param("ip"))
	if ip == "" {
		response.Error(c, http.StatusBadRequest, 40000, "IP 地址不能为空")
		return
	}
	detail, err := h.risk.GetIPDetail(c.Request.Context(), ip)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", detail)
}

// AdminUpdateIPRiskTag 更新 IP 风险标签
// PUT /api/admin/system/risk/ips/:id/tag
func (h *Handler) AdminUpdateIPRiskTag(c *gin.Context) {
	if !requireRiskAdmin(c) {
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
	if err := h.risk.UpdateIPRiskTag(c.Request.Context(), id, req.Tag, req.Note); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.recordAudit(c, "update", "ip_risk_record", strconv.FormatInt(id, 10), fmt.Sprintf("更新 IP 风险标签: %s", req.Tag))
	response.Success(c, 200, "更新成功", nil)
}

// AdminRefreshIPReputation 强制向情报源重拉一次
// POST /api/admin/system/risk/ips/:ip/refresh
func (h *Handler) AdminRefreshIPReputation(c *gin.Context) {
	if !requireRiskAdmin(c) {
		return
	}
	ip := strings.TrimSpace(c.Param("ip"))
	if ip == "" {
		response.Error(c, http.StatusBadRequest, 40000, "IP 地址不能为空")
		return
	}
	record, err := h.risk.RefreshIPReputation(c.Request.Context(), ip)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.recordAudit(c, "update", "ip_risk_record", ip, "刷新 IP 情报")
	response.Success(c, 200, "刷新成功", record)
}

// ════════════════════════════════════════════════════════════
//  处置策略
// ════════════════════════════════════════════════════════════

// AdminListRiskActions 列出处置策略
// GET /api/admin/system/risk/actions
func (h *Handler) AdminListRiskActions(c *gin.Context) {
	actions, err := h.risk.ListRiskActions(c.Request.Context(), c.Query("scene"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", actions)
}

// AdminCreateRiskAction 创建处置策略
// POST /api/admin/system/risk/actions
func (h *Handler) AdminCreateRiskAction(c *gin.Context) {
	if !requireRiskAdmin(c) {
		return
	}
	var req RiskActionCreateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	action, err := h.risk.CreateRiskAction(c.Request.Context(), securitydomain.CreateRiskActionInput{
		Scene:       req.Scene,
		MinScore:    req.MinScore,
		MaxScore:    req.MaxScore,
		Action:      req.Action,
		BanDuration: req.BanDuration,
		Description: req.Description,
	})
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.recordAudit(c, "create", "risk_action", strconv.FormatInt(action.ID, 10), fmt.Sprintf("创建处置策略: %s/%s", req.Scene, req.Action))
	response.Success(c, 200, "创建成功", action)
}

// AdminUpdateRiskAction 更新处置策略
// PUT /api/admin/system/risk/actions/:id
func (h *Handler) AdminUpdateRiskAction(c *gin.Context) {
	if !requireRiskAdmin(c) {
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
	input := securitydomain.UpdateRiskActionInput{
		MinScore:    req.MinScore,
		Action:      req.Action,
		BanDuration: req.BanDuration,
		Description: req.Description,
		IsActive:    req.IsActive,
	}
	if req.MaxScore != nil {
		input.MaxScore = &req.MaxScore
	}
	if err := h.risk.UpdateRiskAction(c.Request.Context(), id, input); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.recordAudit(c, "update", "risk_action", strconv.FormatInt(id, 10), "更新处置策略")
	response.Success(c, 200, "更新成功", nil)
}

// AdminDeleteRiskAction 删除处置策略
// DELETE /api/admin/system/risk/actions/:id
func (h *Handler) AdminDeleteRiskAction(c *gin.Context) {
	if !requireRiskAdmin(c) {
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
	h.recordAudit(c, "delete", "risk_action", strconv.FormatInt(id, 10), "删除处置策略")
	response.Success(c, 200, "删除成功", nil)
}

// ════════════════════════════════════════════════════════════
//  评估 / 模拟
// ════════════════════════════════════════════════════════════

// AdminEvaluateRisk 手动触发一次真实评估（会落库）
// POST /api/admin/system/risk/evaluate
func (h *Handler) AdminEvaluateRisk(c *gin.Context) {
	var req RiskEvalRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.risk.EvaluateRisk(c.Request.Context(), securitydomain.RiskEvalRequest{
		Scene:     req.Scene,
		AppID:     req.AppID,
		UserID:    req.UserID,
		IP:        req.IP,
		DeviceID:  req.DeviceID,
		UserAgent: req.UserAgent,
		Extra:     req.Extra,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "评估完成", result)
}

// AdminSimulateRisk 模拟评估（不落库，可试草稿规则、可覆写环境变量）
// POST /api/admin/system/risk/simulate
func (h *Handler) AdminSimulateRisk(c *gin.Context) {
	var req RiskSimulateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.risk.SimulateRisk(c.Request.Context(), buildSimulateInput(req))
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	response.Success(c, 200, "模拟完成", result)
}

// AdminSimulateRiskRule 针对单条规则的模拟（兼容旧路由）
// POST /api/admin/system/risk/rules/:id/simulate
func (h *Handler) AdminSimulateRiskRule(c *gin.Context) {
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
	input := buildSimulateInput(req)
	input.RuleIDs = []int64{ruleID}
	result, err := h.risk.SimulateRisk(c.Request.Context(), input)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	response.Success(c, 200, "模拟完成", result)
}

func buildSimulateInput(req RiskSimulateRequest) securitydomain.SimulateInput {
	return securitydomain.SimulateInput{
		Scene:     req.Scene,
		IP:        req.IP,
		DeviceID:  req.DeviceID,
		UserAgent: req.UserAgent,
		Account:   req.Account,
		AppID:     req.AppID,
		RuleIDs:   req.RuleIDs,
		Draft:     req.Draft.ToDomain(req.Scene),
		Overrides: req.Overrides,
	}
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
	start, end := resolveRiskRange(req.Start, req.End)
	dash, err := h.risk.GetRiskDashboard(c.Request.Context(), start, end)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", dash)
}

// ════════════════════════════════════════════════════════════
//  共用小工具
// ════════════════════════════════════════════════════════════

// resolveRiskRange 解析时间区间，默认最近 7 天。
// 起止顺序颠倒时自动交换 —— 让一个手滑的参数返回空数据，
// 比返回一个"看起来没有风险"的假象要好，但自动纠正比两者都好。
func resolveRiskRange(rawStart, rawEnd string) (time.Time, time.Time) {
	end := timeutil.NowUTC()
	start := end.Add(-7 * 24 * time.Hour)
	if at, err := time.Parse(time.RFC3339, rawStart); err == nil {
		start = at.UTC()
	}
	if at, err := time.Parse(time.RFC3339, rawEnd); err == nil {
		end = at.UTC()
	}
	if end.Before(start) {
		start, end = end, start
	}
	return start, end
}

func bindRiskEntityQuery(c *gin.Context) (securitydomain.EntityQuery, bool) {
	var req RiskEntityListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return securitydomain.EntityQuery{}, false
	}
	onlyRisk, _ := parseOptionalBool(req.OnlyRisk)
	return securitydomain.EntityQuery{
		Keyword:  strings.TrimSpace(req.Keyword),
		Tag:      strings.TrimSpace(req.Tag),
		OnlyRisk: onlyRisk,
		Page:     req.Page,
		PageSize: req.PageSize,
	}, true
}

// parseOptionalBool 区分「没传」与「传了 false」。
// 用裸 bool 接查询参数会让"只看未复核"和"全部"在后端长得一模一样。
func parseOptionalBool(raw string) (bool, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return false, false
	}
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return false, false
	}
	return parsed, true
}
