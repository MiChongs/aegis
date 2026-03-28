package httptransport

import (
	"net/http"
	"strconv"
	"time"

	firewalldomain "aegis/internal/domain/firewall"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// AdminFirewallLogs 分页查询防火墙拦截日志
// GET /api/admin/firewall/logs
func (h *Handler) AdminFirewallLogs(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可查看防火墙日志")
		return
	}
	var req FirewallLogListRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}

	filter := firewalldomain.FirewallLogFilter{
		IP:          req.IP,
		Country:     req.Country,
		Reason:      req.Reason,
		WAFRuleID:   req.WAFRuleID,
		PathPattern: req.PathPattern,
		Severity:    req.Severity,
		Page:        req.Page,
		PageSize:    req.PageSize,
		SortBy:      req.SortBy,
		SortOrder:   req.SortOrder,
	}
	if req.StartTime != "" {
		if t, err := time.Parse(time.RFC3339, req.StartTime); err == nil {
			filter.StartTime = &t
		}
	}
	if req.EndTime != "" {
		if t, err := time.Parse(time.RFC3339, req.EndTime); err == nil {
			filter.EndTime = &t
		}
	}

	page, err := h.firewallLog.ListLogs(c.Request.Context(), filter)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", page)
}

// AdminFirewallLogDetail 获取单条防火墙日志详情
// GET /api/admin/firewall/logs/:logId
func (h *Handler) AdminFirewallLogDetail(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可查看防火墙日志")
		return
	}
	logID, err := strconv.ParseInt(c.Param("logId"), 10, 64)
	if err != nil || logID < 1 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的日志 ID")
		return
	}
	item, err := h.firewallLog.GetLog(c.Request.Context(), logID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if item == nil {
		response.Error(c, http.StatusNotFound, 40400, "日志不存在")
		return
	}
	response.Success(c, 200, "获取成功", item)
}

// AdminFirewallStats 防火墙拦截统计
// GET /api/admin/firewall/stats
func (h *Handler) AdminFirewallStats(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可查看防火墙统计")
		return
	}
	var req FirewallLogStatsRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}

	// 默认最近 24 小时
	end := time.Now().UTC()
	start := end.Add(-24 * time.Hour)
	if req.StartTime != "" {
		if t, err := time.Parse(time.RFC3339, req.StartTime); err == nil {
			start = t
		}
	}
	if req.EndTime != "" {
		if t, err := time.Parse(time.RFC3339, req.EndTime); err == nil {
			end = t
		}
	}
	granularity := "hour"
	if req.Granularity == "day" {
		granularity = "day"
	}

	stats, err := h.firewallLog.GetStats(c.Request.Context(), start, end, granularity)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", stats)
}

// AdminFirewallLogsCleanup 批量清理防火墙日志
// DELETE /api/admin/firewall/logs
func (h *Handler) AdminFirewallLogsCleanup(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可执行日志清理")
		return
	}
	var req FirewallLogCleanupRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	before, err := time.Parse(time.RFC3339, req.Before)
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的时间格式，请使用 RFC3339")
		return
	}
	deleted, err := h.firewallLog.CleanupLogs(c.Request.Context(), before)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "清理完成", map[string]any{
		"deleted": deleted,
		"before":  before,
	})
}
