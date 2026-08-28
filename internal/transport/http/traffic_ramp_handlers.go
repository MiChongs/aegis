package httptransport

import (
	"net/http"

	systemdomain "aegis/internal/domain/system"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 突发流量爬坡管理端。全部限超级管理员：准入上限决定平台在洪峰下放多少
// 流量进后端，改错一个参数的影响面等同于改防火墙限流。

// AdminTrafficRampUpdateRequest 逐字段更新；缺省字段不修改。
type AdminTrafficRampUpdateRequest struct {
	Enabled            *bool     `json:"enabled,omitempty"`
	BaselineRPS        *int      `json:"baselineRps,omitempty"`
	MaxRPS             *int      `json:"maxRps,omitempty"`
	RampStepPct        *int      `json:"rampStepPct,omitempty"`
	RampIntervalMs     *int      `json:"rampIntervalMs,omitempty"`
	CooldownSeconds    *int      `json:"cooldownSeconds,omitempty"`
	QueueSize          *int      `json:"queueSize,omitempty"`
	QueueTimeoutMs     *int      `json:"queueTimeoutMs,omitempty"`
	MaxConcurrent      *int      `json:"maxConcurrent,omitempty"`
	ExemptPathPrefixes *[]string `json:"exemptPathPrefixes,omitempty"`
	ExemptAdmin        *bool     `json:"exemptAdmin,omitempty"`
	RetryAfterSeconds  *int      `json:"retryAfterSeconds,omitempty"`
}

// AdminTrafficRampStatsQuery 统计查询参数。
type AdminTrafficRampStatsQuery struct {
	// Seconds 时间序列跨度（秒），60–900，缺省 300。
	Seconds int `form:"seconds"`
}

func (h *Handler) AdminGetTrafficRamp(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可查看流量爬坡配置")
		return
	}
	item, err := h.system.GetTrafficRampSettings(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminUpdateTrafficRamp(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可调整流量爬坡配置")
		return
	}
	var req AdminTrafficRampUpdateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.system.UpdateTrafficRampSettings(c.Request.Context(), actorIDPtr(c), systemdomain.TrafficRampSettingsPatch{
		Enabled:            req.Enabled,
		BaselineRPS:        req.BaselineRPS,
		MaxRPS:             req.MaxRPS,
		RampStepPct:        req.RampStepPct,
		RampIntervalMs:     req.RampIntervalMs,
		CooldownSeconds:    req.CooldownSeconds,
		QueueSize:          req.QueueSize,
		QueueTimeoutMs:     req.QueueTimeoutMs,
		MaxConcurrent:      req.MaxConcurrent,
		ExemptPathPrefixes: req.ExemptPathPrefixes,
		ExemptAdmin:        req.ExemptAdmin,
		RetryAfterSeconds:  req.RetryAfterSeconds,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "更新成功", item)
	h.recordAudit(c, "settings.traffic_ramp.update", "settings", "", "修改流量爬坡配置")
}

func (h *Handler) AdminTrafficRampStats(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可查看流量爬坡统计")
		return
	}
	var query AdminTrafficRampStatsQuery
	_ = c.ShouldBindQuery(&query)
	if query.Seconds <= 0 {
		query.Seconds = 300
	}
	stats, err := h.system.TrafficRampStats(query.Seconds)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", stats)
}

func (h *Handler) AdminResetTrafficRampStats(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可清零流量爬坡统计")
		return
	}
	if err := h.system.ResetTrafficRampStats(); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "统计已清零", gin.H{"ok": true})
	h.recordAudit(c, "settings.traffic_ramp.reset_stats", "settings", "", "清零流量爬坡统计")
}
