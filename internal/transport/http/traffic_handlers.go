package httptransport

import (
	"net/http"
	"strconv"

	plugindomain "aegis/internal/domain/plugin"
	trafficplugin "aegis/internal/plugins/traffic"
	"aegis/pkg/response"
	"aegis/pkg/traffic"

	"github.com/gin-gonic/gin"
)

// 流量管理（内置插件 traffic）与内置插件宿主的管理端。全部限超级管理员：
// 流量规则决定平台在洪峰下放多少请求进后端，改错一个参数的影响面等同于改防火墙。

// ── 请求体 ──

// AdminTrafficOverviewQuery 总览查询。
type AdminTrafficOverviewQuery struct {
	// Seconds 秒级序列跨度，60–900，缺省 300。
	Seconds int `form:"seconds"`
}

// AdminTrafficSettingsRequest 替换全局设置。
type AdminTrafficSettingsRequest struct {
	Settings         traffic.Settings `json:"settings"`
	ExpectedRevision int64            `json:"expectedRevision"`
	Note             string           `json:"note"`
}

// AdminTrafficRuleRequest 新建 / 修改规则。
type AdminTrafficRuleRequest struct {
	Rule             traffic.Rule `json:"rule"`
	ExpectedRevision int64        `json:"expectedRevision"`
	Note             string       `json:"note"`
	// Position 新建时的位置：top | bottom（缺省）。
	Position string `json:"position"`
}

// AdminTrafficToggleRequest 启用 / 停用规则。
type AdminTrafficToggleRequest struct {
	Enabled          bool  `json:"enabled"`
	ExpectedRevision int64 `json:"expectedRevision"`
}

// AdminTrafficReorderRequest 调整规则顺序。
type AdminTrafficReorderRequest struct {
	IDs              []string `json:"ids"`
	ExpectedRevision int64    `json:"expectedRevision"`
}

// AdminTrafficValidateRequest 校验规则草稿。
type AdminTrafficValidateRequest struct {
	Rule traffic.Rule `json:"rule"`
}

// AdminTrafficSimulateRequest 模拟请求；Draft 非空时用草稿替换同 ID 规则后再模拟。
type AdminTrafficSimulateRequest struct {
	traffic.SimulateInput
	Draft *traffic.Rule `json:"draft"`
}

// AdminTrafficRollbackRequest 回滚到某个修订。
type AdminTrafficRollbackRequest struct {
	Revision int64  `json:"revision"`
	Note     string `json:"note"`
}

// AdminTrafficHistoryQuery 历史曲线查询。
type AdminTrafficHistoryQuery struct {
	// Range 1h | 6h | 24h | 7d。
	Range  string `form:"range"`
	RuleID string `form:"ruleId"`
}

// AdminTrafficListQuery 列表查询。
type AdminTrafficListQuery struct {
	Limit int    `form:"limit"`
	Kind  string `form:"kind"`
}

// AdminTrafficDeleteQuery 删除规则的乐观锁参数。
type AdminTrafficDeleteQuery struct {
	ExpectedRevision int64 `form:"expectedRevision"`
}

// ── 公共守卫 ──

func (h *Handler) trafficGuard(c *gin.Context) (*trafficplugin.Plugin, bool) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理流量")
		return nil, false
	}
	if h.traffic == nil {
		response.Error(c, http.StatusServiceUnavailable, 50390, "流量管理插件未加载")
		return nil, false
	}
	return h.traffic, true
}

// ── 运行态 ──

func (h *Handler) AdminTrafficOverview(c *gin.Context) {
	p, ok := h.trafficGuard(c)
	if !ok {
		return
	}
	var q AdminTrafficOverviewQuery
	_ = c.ShouldBindQuery(&q)
	if q.Seconds <= 0 {
		q.Seconds = 300
	}
	var view = h.nativePluginView(trafficplugin.ID)
	response.Success(c, 200, "获取成功", p.Overview(c.Request.Context(), q.Seconds, view))
}

func (h *Handler) AdminTrafficRuleStats(c *gin.Context) {
	p, ok := h.trafficGuard(c)
	if !ok {
		return
	}
	var q AdminTrafficOverviewQuery
	_ = c.ShouldBindQuery(&q)
	if q.Seconds <= 0 {
		q.Seconds = 300
	}
	stats, err := p.RuleStats(c.Param("ruleId"), q.Seconds)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", stats)
}

func (h *Handler) AdminTrafficResetStats(c *gin.Context) {
	p, ok := h.trafficGuard(c)
	if !ok {
		return
	}
	p.ResetStats()
	response.Success(c, 200, "统计已清零", gin.H{"ok": true})
	h.recordAudit(c, "traffic.reset_stats", "traffic", "", "清零流量统计（本实例）")
}

// ── 配置 ──

func (h *Handler) AdminTrafficConfig(c *gin.Context) {
	p, ok := h.trafficGuard(c)
	if !ok {
		return
	}
	view, err := p.Config(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", view)
}

func (h *Handler) AdminTrafficTemplates(c *gin.Context) {
	if _, ok := h.trafficGuard(c); !ok {
		return
	}
	response.Success(c, 200, "获取成功", trafficplugin.Templates())
}

func (h *Handler) AdminTrafficSaveSettings(c *gin.Context) {
	p, ok := h.trafficGuard(c)
	if !ok {
		return
	}
	var req AdminTrafficSettingsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	view, err := p.SaveSettings(c.Request.Context(), actorIDPtr(c), req.Settings, req.ExpectedRevision, req.Note)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已保存", view)
	h.recordAudit(c, "traffic.settings.update", "traffic", "", "修改流量管理全局设置")
}

func (h *Handler) AdminTrafficCreateRule(c *gin.Context) {
	p, ok := h.trafficGuard(c)
	if !ok {
		return
	}
	var req AdminTrafficRuleRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	view, err := p.CreateRule(c.Request.Context(), actorIDPtr(c), req.Rule, req.Position, req.ExpectedRevision, req.Note)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已创建", view)
	h.recordAudit(c, "traffic.rule.create", "traffic_rule", "", "新建流量规则「"+req.Rule.Name+"」")
}

func (h *Handler) AdminTrafficUpdateRule(c *gin.Context) {
	p, ok := h.trafficGuard(c)
	if !ok {
		return
	}
	var req AdminTrafficRuleRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	id := c.Param("ruleId")
	view, err := p.UpdateRule(c.Request.Context(), actorIDPtr(c), id, req.Rule, req.ExpectedRevision, req.Note)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已保存", view)
	h.recordAudit(c, "traffic.rule.update", "traffic_rule", id, "修改流量规则「"+req.Rule.Name+"」")
}

func (h *Handler) AdminTrafficDeleteRule(c *gin.Context) {
	p, ok := h.trafficGuard(c)
	if !ok {
		return
	}
	var q AdminTrafficDeleteQuery
	_ = c.ShouldBindQuery(&q)
	id := c.Param("ruleId")
	view, err := p.DeleteRule(c.Request.Context(), actorIDPtr(c), id, q.ExpectedRevision)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已删除", view)
	h.recordAudit(c, "traffic.rule.delete", "traffic_rule", id, "删除流量规则")
}

func (h *Handler) AdminTrafficToggleRule(c *gin.Context) {
	p, ok := h.trafficGuard(c)
	if !ok {
		return
	}
	var req AdminTrafficToggleRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	id := c.Param("ruleId")
	view, err := p.ToggleRule(c.Request.Context(), actorIDPtr(c), id, req.Enabled, req.ExpectedRevision)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已更新", view)
	detail := "停用流量规则"
	if req.Enabled {
		detail = "启用流量规则"
	}
	h.recordAudit(c, "traffic.rule.toggle", "traffic_rule", id, detail)
}

func (h *Handler) AdminTrafficReorderRules(c *gin.Context) {
	p, ok := h.trafficGuard(c)
	if !ok {
		return
	}
	var req AdminTrafficReorderRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	view, err := p.ReorderRules(c.Request.Context(), actorIDPtr(c), req.IDs, req.ExpectedRevision)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已调整顺序", view)
	h.recordAudit(c, "traffic.rule.reorder", "traffic", "", "调整流量规则顺序")
}

func (h *Handler) AdminTrafficValidateRule(c *gin.Context) {
	p, ok := h.trafficGuard(c)
	if !ok {
		return
	}
	var req AdminTrafficValidateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	rule, errs := p.Validate(req.Rule)
	response.Success(c, 200, "校验完成", gin.H{"rule": rule, "errors": errs})
}

func (h *Handler) AdminTrafficSimulate(c *gin.Context) {
	p, ok := h.trafficGuard(c)
	if !ok {
		return
	}
	var req AdminTrafficSimulateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	response.Success(c, 200, "模拟完成", p.Simulate(req.SimulateInput, req.Draft))
}

// ── 修订 ──

func (h *Handler) AdminTrafficRevisions(c *gin.Context) {
	p, ok := h.trafficGuard(c)
	if !ok {
		return
	}
	var q AdminTrafficListQuery
	_ = c.ShouldBindQuery(&q)
	items, err := p.Revisions(c.Request.Context(), q.Limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) AdminTrafficRevision(c *gin.Context) {
	p, ok := h.trafficGuard(c)
	if !ok {
		return
	}
	rev, err := strconv.ParseInt(c.Param("revision"), 10, 64)
	if err != nil || rev <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "修订号无效")
		return
	}
	item, err := p.Revision(c.Request.Context(), rev)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminTrafficRollback(c *gin.Context) {
	p, ok := h.trafficGuard(c)
	if !ok {
		return
	}
	var req AdminTrafficRollbackRequest
	if err := bind(c, &req); err != nil || req.Revision <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "修订号无效")
		return
	}
	view, err := p.Rollback(c.Request.Context(), actorIDPtr(c), req.Revision, req.Note)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已回滚", view)
	h.recordAudit(c, "traffic.rollback", "traffic", strconv.FormatInt(req.Revision, 10), "回滚流量配置到修订 "+strconv.FormatInt(req.Revision, 10))
}

// ── 历史 ──

func (h *Handler) AdminTrafficHistory(c *gin.Context) {
	p, ok := h.trafficGuard(c)
	if !ok {
		return
	}
	var q AdminTrafficHistoryQuery
	_ = c.ShouldBindQuery(&q)
	item, err := p.History(c.Request.Context(), q.Range, q.RuleID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminTrafficEvents(c *gin.Context) {
	p, ok := h.trafficGuard(c)
	if !ok {
		return
	}
	var q AdminTrafficListQuery
	_ = c.ShouldBindQuery(&q)
	items, err := p.Events(c.Request.Context(), q.Limit, q.Kind)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

// ── 内置插件宿主 ──

func (h *Handler) nativePluginView(id string) *plugindomain.NativeView {
	if h.nativePlugins == nil {
		return nil
	}
	v, err := h.nativePlugins.Get(id)
	if err != nil {
		return nil
	}
	return &v
}

func (h *Handler) nativePluginGuard(c *gin.Context) bool {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可管理内置插件")
		return false
	}
	if h.nativePlugins == nil {
		response.Error(c, http.StatusServiceUnavailable, 50390, "插件宿主未加载")
		return false
	}
	return true
}

func (h *Handler) AdminListNativePlugins(c *gin.Context) {
	if !h.nativePluginGuard(c) {
		return
	}
	response.Success(c, 200, "获取成功", h.nativePlugins.List())
}

func (h *Handler) AdminEnableNativePlugin(c *gin.Context) {
	if !h.nativePluginGuard(c) {
		return
	}
	id := c.Param("pluginId")
	v, err := h.nativePlugins.Enable(c.Request.Context(), id, actorIDPtr(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已启用", v)
	h.recordAudit(c, "native_plugin.enable", "native_plugin", id, "启用内置插件")
}

func (h *Handler) AdminDisableNativePlugin(c *gin.Context) {
	if !h.nativePluginGuard(c) {
		return
	}
	id := c.Param("pluginId")
	v, err := h.nativePlugins.Disable(c.Request.Context(), id, actorIDPtr(c))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已停用", v)
	h.recordAudit(c, "native_plugin.disable", "native_plugin", id, "停用内置插件")
}

func (h *Handler) AdminRestartNativePlugin(c *gin.Context) {
	if !h.nativePluginGuard(c) {
		return
	}
	id := c.Param("pluginId")
	v, err := h.nativePlugins.Restart(c.Request.Context(), id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已重启", v)
	h.recordAudit(c, "native_plugin.restart", "native_plugin", id, "重启内置插件")
}
