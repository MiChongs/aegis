package httptransport

import (
	"fmt"
	"net/http"
	"strconv"

	systemdomain "aegis/internal/domain/system"
	fw "aegis/internal/firewall"
	auditmiddleware "aegis/internal/middleware"
	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 防火墙工作台（docs/firewall.md）：运行态、设置、自定义规则、WAF 例外、规则目录、测试台。全部仅超级管理员。

func (h *Handler) requireFirewallAdmin(c *gin.Context) (int64, bool) {
	session, ok := requireSuperAdminSession(c)
	if !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理防火墙")
		return 0, false
	}
	if h.firewallPolicy == nil {
		response.Error(c, http.StatusServiceUnavailable, 50390, "防火墙未启用")
		return 0, false
	}
	return session.AdminID, true
}

func firewallIDParam(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的 ID")
		return 0, false
	}
	return id, true
}

// AdminFirewallRuntime 本副本运行态
// GET /api/admin/system/firewall/runtime
func (h *Handler) AdminFirewallRuntime(c *gin.Context) {
	if _, ok := h.requireFirewallAdmin(c); !ok {
		return
	}
	response.Success(c, 200, "获取成功", h.firewallPolicy.Runtime())
}

// AdminFirewallSettings 防火墙设置
// GET /api/admin/system/firewall/settings
func (h *Handler) AdminFirewallSettings(c *gin.Context) {
	if _, ok := h.requireFirewallAdmin(c); !ok {
		return
	}
	view, err := h.system.GetFirewallSettings(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", view)
}

// AdminUpdateFirewallSettings 修改防火墙设置（部分更新）
// PUT /api/admin/system/firewall/settings
func (h *Handler) AdminUpdateFirewallSettings(c *gin.Context) {
	adminID, ok := h.requireFirewallAdmin(c)
	if !ok {
		return
	}
	var req systemdomain.FirewallSettingsPatch
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	auditmiddleware.SetAuditResource(c, "firewall.settings", "")
	view, err := h.system.UpdateFirewallSettings(c.Request.Context(), &adminID, req)
	if err != nil {
		auditmiddleware.SetAuditFailure(c, err)
		h.writeError(c, err)
		return
	}
	auditmiddleware.SetAuditSummary(c, "修改防火墙设置")
	auditmiddleware.SetAuditSeverity(c, systemdomain.AuditSeverityHigh)
	response.Success(c, 200, "已保存并生效", view)
}

// ── 自定义规则 ───────────────────────────────────────────────

func firewallRuleInput(req FirewallRuleRequest) service.FirewallRuleInput {
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	return service.FirewallRuleInput{
		Name: req.Name, Description: req.Description, Enabled: enabled, Priority: req.Priority,
		Expression: req.Expression, Action: req.Action, BlockMode: req.BlockMode, BanSeconds: req.BanSeconds,
	}
}

// AdminListFirewallRules 自定义规则列表
// GET /api/admin/system/firewall/rules
func (h *Handler) AdminListFirewallRules(c *gin.Context) {
	if _, ok := h.requireFirewallAdmin(c); !ok {
		return
	}
	items, err := h.firewallPolicy.ListRules(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

// AdminCreateFirewallRule 新建自定义规则
// POST /api/admin/system/firewall/rules
func (h *Handler) AdminCreateFirewallRule(c *gin.Context) {
	adminID, ok := h.requireFirewallAdmin(c)
	if !ok {
		return
	}
	var req FirewallRuleRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	auditmiddleware.SetAuditResource(c, "firewall.rule", "")
	rule, err := h.firewallPolicy.CreateRule(c.Request.Context(), firewallRuleInput(req), adminID)
	if err != nil {
		auditmiddleware.SetAuditFailure(c, err)
		h.writeError(c, err)
		return
	}
	auditmiddleware.SetAuditResource(c, "firewall.rule", strconv.FormatInt(rule.ID, 10))
	auditmiddleware.SetAuditSummary(c, fmt.Sprintf("新建防火墙规则「%s」（%s）", rule.Name, rule.Action))
	auditmiddleware.SetAuditSeverity(c, systemdomain.AuditSeverityHigh)
	response.Success(c, 200, "已创建并生效", rule)
}

// AdminUpdateFirewallRule 修改自定义规则
// PUT /api/admin/system/firewall/rules/:ruleId
func (h *Handler) AdminUpdateFirewallRule(c *gin.Context) {
	adminID, ok := h.requireFirewallAdmin(c)
	if !ok {
		return
	}
	id, ok := firewallIDParam(c, "ruleId")
	if !ok {
		return
	}
	var req FirewallRuleRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	auditmiddleware.SetAuditResource(c, "firewall.rule", strconv.FormatInt(id, 10))
	rule, err := h.firewallPolicy.UpdateRule(c.Request.Context(), id, firewallRuleInput(req), adminID)
	if err != nil {
		auditmiddleware.SetAuditFailure(c, err)
		h.writeError(c, err)
		return
	}
	auditmiddleware.SetAuditSummary(c, fmt.Sprintf("修改防火墙规则「%s」", rule.Name))
	auditmiddleware.SetAuditSeverity(c, systemdomain.AuditSeverityHigh)
	response.Success(c, 200, "已保存并生效", rule)
}

// AdminToggleFirewallRule 启停自定义规则
// PATCH /api/admin/system/firewall/rules/:ruleId
func (h *Handler) AdminToggleFirewallRule(c *gin.Context) {
	adminID, ok := h.requireFirewallAdmin(c)
	if !ok {
		return
	}
	id, ok := firewallIDParam(c, "ruleId")
	if !ok {
		return
	}
	var req FirewallToggleRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	auditmiddleware.SetAuditResource(c, "firewall.rule", strconv.FormatInt(id, 10))
	if err := h.firewallPolicy.ToggleRule(c.Request.Context(), id, req.Enabled, adminID); err != nil {
		auditmiddleware.SetAuditFailure(c, err)
		h.writeError(c, err)
		return
	}
	auditmiddleware.SetAuditSummary(c, fmt.Sprintf("%s防火墙规则 #%d", map[bool]string{true: "启用", false: "停用"}[req.Enabled], id))
	auditmiddleware.SetAuditSeverity(c, systemdomain.AuditSeverityHigh)
	response.Success(c, 200, "已生效", gin.H{"id": id, "enabled": req.Enabled})
}

// AdminDeleteFirewallRule 删除自定义规则
// DELETE /api/admin/system/firewall/rules/:ruleId
func (h *Handler) AdminDeleteFirewallRule(c *gin.Context) {
	if _, ok := h.requireFirewallAdmin(c); !ok {
		return
	}
	id, ok := firewallIDParam(c, "ruleId")
	if !ok {
		return
	}
	auditmiddleware.SetAuditResource(c, "firewall.rule", strconv.FormatInt(id, 10))
	if err := h.firewallPolicy.DeleteRule(c.Request.Context(), id); err != nil {
		auditmiddleware.SetAuditFailure(c, err)
		h.writeError(c, err)
		return
	}
	auditmiddleware.SetAuditSummary(c, fmt.Sprintf("删除防火墙规则 #%d", id))
	auditmiddleware.SetAuditSeverity(c, systemdomain.AuditSeverityHigh)
	response.Success(c, 200, "已删除", gin.H{"id": id})
}

// AdminReorderFirewallRules 调整自定义规则顺序
// POST /api/admin/system/firewall/rules/reorder
func (h *Handler) AdminReorderFirewallRules(c *gin.Context) {
	adminID, ok := h.requireFirewallAdmin(c)
	if !ok {
		return
	}
	var req FirewallReorderRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	auditmiddleware.SetAuditResource(c, "firewall.rule", "")
	if err := h.firewallPolicy.ReorderRules(c.Request.Context(), req.IDs, adminID); err != nil {
		auditmiddleware.SetAuditFailure(c, err)
		h.writeError(c, err)
		return
	}
	auditmiddleware.SetAuditSummary(c, fmt.Sprintf("调整防火墙规则顺序（%d 条）", len(req.IDs)))
	response.Success(c, 200, "已生效", gin.H{"ids": req.IDs})
}

// AdminValidateFirewallRule 校验表达式
// POST /api/admin/system/firewall/rules/validate
func (h *Handler) AdminValidateFirewallRule(c *gin.Context) {
	if _, ok := h.requireFirewallAdmin(c); !ok {
		return
	}
	var req FirewallRuleValidateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	action := req.Action
	if action == "" {
		action = fw.ActionBlock
	}
	if _, err := fw.CompileRule(fw.CustomRule{Name: "validate", Expression: req.Expression, Action: action}); err != nil {
		response.Success(c, 200, "表达式有误", gin.H{"valid": false, "error": err.Error()})
		return
	}
	response.Success(c, 200, "表达式有效", gin.H{"valid": true})
}

// ── WAF 例外 ─────────────────────────────────────────────────

func firewallExclusionInput(req FirewallExclusionRequest) service.FirewallExclusionInput {
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	return service.FirewallExclusionInput{
		Name: req.Name, Note: req.Note, Enabled: enabled, PathMatch: req.PathMatch, Path: req.Path,
		Methods: req.Methods, RuleIDs: req.RuleIDs, Tags: req.Tags, Targets: req.Targets,
	}
}

// AdminListFirewallExclusions WAF 例外列表
// GET /api/admin/system/firewall/exclusions
func (h *Handler) AdminListFirewallExclusions(c *gin.Context) {
	if _, ok := h.requireFirewallAdmin(c); !ok {
		return
	}
	items, err := h.firewallPolicy.ListExclusions(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

// AdminCreateFirewallExclusion 新建 WAF 例外
// POST /api/admin/system/firewall/exclusions
func (h *Handler) AdminCreateFirewallExclusion(c *gin.Context) {
	adminID, ok := h.requireFirewallAdmin(c)
	if !ok {
		return
	}
	var req FirewallExclusionRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	auditmiddleware.SetAuditResource(c, "firewall.exclusion", "")
	item, err := h.firewallPolicy.CreateExclusion(c.Request.Context(), firewallExclusionInput(req), adminID)
	if err != nil {
		auditmiddleware.SetAuditFailure(c, err)
		h.writeError(c, err)
		return
	}
	auditmiddleware.SetAuditResource(c, "firewall.exclusion", strconv.FormatInt(item.ID, 10))
	auditmiddleware.SetAuditSummary(c, fmt.Sprintf("新建 WAF 例外「%s」（%s）", item.Name, item.Path))
	auditmiddleware.SetAuditSeverity(c, systemdomain.AuditSeverityHigh)
	response.Success(c, 200, "已创建并生效", item)
}

// AdminUpdateFirewallExclusion 修改 WAF 例外
// PUT /api/admin/system/firewall/exclusions/:exclusionId
func (h *Handler) AdminUpdateFirewallExclusion(c *gin.Context) {
	adminID, ok := h.requireFirewallAdmin(c)
	if !ok {
		return
	}
	id, ok := firewallIDParam(c, "exclusionId")
	if !ok {
		return
	}
	var req FirewallExclusionRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	auditmiddleware.SetAuditResource(c, "firewall.exclusion", strconv.FormatInt(id, 10))
	item, err := h.firewallPolicy.UpdateExclusion(c.Request.Context(), id, firewallExclusionInput(req), adminID)
	if err != nil {
		auditmiddleware.SetAuditFailure(c, err)
		h.writeError(c, err)
		return
	}
	auditmiddleware.SetAuditSummary(c, fmt.Sprintf("修改 WAF 例外「%s」", item.Name))
	auditmiddleware.SetAuditSeverity(c, systemdomain.AuditSeverityHigh)
	response.Success(c, 200, "已保存并生效", item)
}

// AdminToggleFirewallExclusion 启停 WAF 例外
// PATCH /api/admin/system/firewall/exclusions/:exclusionId
func (h *Handler) AdminToggleFirewallExclusion(c *gin.Context) {
	adminID, ok := h.requireFirewallAdmin(c)
	if !ok {
		return
	}
	id, ok := firewallIDParam(c, "exclusionId")
	if !ok {
		return
	}
	var req FirewallToggleRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	auditmiddleware.SetAuditResource(c, "firewall.exclusion", strconv.FormatInt(id, 10))
	if err := h.firewallPolicy.ToggleExclusion(c.Request.Context(), id, req.Enabled, adminID); err != nil {
		auditmiddleware.SetAuditFailure(c, err)
		h.writeError(c, err)
		return
	}
	auditmiddleware.SetAuditSummary(c, fmt.Sprintf("%s WAF 例外 #%d", map[bool]string{true: "启用", false: "停用"}[req.Enabled], id))
	auditmiddleware.SetAuditSeverity(c, systemdomain.AuditSeverityHigh)
	response.Success(c, 200, "已生效", gin.H{"id": id, "enabled": req.Enabled})
}

// AdminDeleteFirewallExclusion 删除 WAF 例外
// DELETE /api/admin/system/firewall/exclusions/:exclusionId
func (h *Handler) AdminDeleteFirewallExclusion(c *gin.Context) {
	if _, ok := h.requireFirewallAdmin(c); !ok {
		return
	}
	id, ok := firewallIDParam(c, "exclusionId")
	if !ok {
		return
	}
	auditmiddleware.SetAuditResource(c, "firewall.exclusion", strconv.FormatInt(id, 10))
	if err := h.firewallPolicy.DeleteExclusion(c.Request.Context(), id); err != nil {
		auditmiddleware.SetAuditFailure(c, err)
		h.writeError(c, err)
		return
	}
	auditmiddleware.SetAuditSummary(c, fmt.Sprintf("删除 WAF 例外 #%d", id))
	auditmiddleware.SetAuditSeverity(c, systemdomain.AuditSeverityHigh)
	response.Success(c, 200, "已删除", gin.H{"id": id})
}

// ── 规则目录与测试台 ─────────────────────────────────────────

// AdminFirewallCatalog CRS 与 Aegis 规则目录（按当前配置标注启用 / 移除 / 生效）
// GET /api/admin/system/firewall/catalog
func (h *Handler) AdminFirewallCatalog(c *gin.Context) {
	if _, ok := h.requireFirewallAdmin(c); !ok {
		return
	}
	catalog, err := h.firewallPolicy.Catalog()
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", catalog)
}

// AdminFirewallCatalogRule 规则详情与最近命中
// GET /api/admin/system/firewall/catalog/rules/:ruleId
func (h *Handler) AdminFirewallCatalogRule(c *gin.Context) {
	if _, ok := h.requireFirewallAdmin(c); !ok {
		return
	}
	id, err := strconv.Atoi(c.Param("ruleId"))
	if err != nil || id < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的规则 ID")
		return
	}
	detail, err := h.firewallPolicy.RuleDetail(c.Request.Context(), id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", detail)
}

// AdminFirewallDirectives 线上 WAF 的完整指令
// GET /api/admin/system/firewall/directives
func (h *Handler) AdminFirewallDirectives(c *gin.Context) {
	if _, ok := h.requireFirewallAdmin(c); !ok {
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"directives": h.firewallPolicy.Directives()})
}

// AdminFirewallSandbox 测试台：在进程内把一个请求过一遍自定义规则与 WAF
// POST /api/admin/system/firewall/sandbox
func (h *Handler) AdminFirewallSandbox(c *gin.Context) {
	if _, ok := h.requireFirewallAdmin(c); !ok {
		return
	}
	var req FirewallSandboxRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	in := service.SandboxInput{Method: req.Method, URL: req.URL, Headers: req.Headers, Body: req.Body, IP: req.IP, Transport: req.Transport}
	if req.Draft != nil {
		draft := firewallExclusionInput(*req.Draft)
		in.DraftExclusion = &draft
	}
	result, err := h.firewallPolicy.Sandbox(c.Request.Context(), in)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "检测完成", result)
}
