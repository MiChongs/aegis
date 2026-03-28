package httptransport

import (
	"net/http"
	"strings"

	"aegis/internal/service"
	"aegis/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *Handler) SystemMonitor(c *gin.Context) {
	if h.monitor == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "系统监测服务暂不可用")
		return
	}
	// 优先返回缓存快照
	cached, err := h.monitor.CachedSystemOverview(c.Request.Context())
	if err == nil && cached != nil {
		response.Success(c, 200, "获取成功", cached)
		return
	}
	// 降级：实时计算（首次启动时快照尚未就绪）
	result, err := h.monitor.SystemOverview(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, 50311, "系统监测数据暂时不可用")
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) SystemMonitorApps(c *gin.Context) {
	if h.monitor == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "系统监测服务暂不可用")
		return
	}
	cached, err := h.monitor.CachedApps(c.Request.Context())
	if err == nil && cached != nil {
		response.Success(c, 200, "获取成功", cached)
		return
	}
	items, err := h.monitor.ListAppBriefs(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusServiceUnavailable, 50311, "应用监测数据暂时不可用")
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) SystemMonitorComponents(c *gin.Context) {
	if h.monitor == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "系统监测服务暂不可用")
		return
	}
	overview, _ := h.monitor.CachedSystemOverview(c.Request.Context())
	if overview == nil {
		var err error
		overview, err = h.monitor.SystemOverview(c.Request.Context())
		if err != nil {
			response.Error(c, http.StatusServiceUnavailable, 50311, "系统监测数据暂时不可用")
			return
		}
	}
	response.Success(c, 200, "获取成功", gin.H{
		"status":           overview.Status,
		"score":            overview.Score,
		"availabilityRate": overview.AvailabilityRate,
		"checkedAt":        overview.CheckedAt,
		"runtime":          overview.Runtime,
		"counts":           overview.Counts,
		"components":       overview.Components,
		"infrastructure":   overview.Infrastructure,
		"modules":          overview.Modules,
	})
}

func (h *Handler) AppMonitor(c *gin.Context) {
	result, ok := h.loadAppMonitor(c)
	if !ok {
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) AppMonitorComponents(c *gin.Context) {
	result, ok := h.loadAppMonitor(c)
	if !ok {
		return
	}
	response.Success(c, 200, "获取成功", gin.H{
		"status":           result.Status,
		"score":            result.Score,
		"availabilityRate": result.AvailabilityRate,
		"checkedAt":        result.CheckedAt,
		"runtime":          result.Runtime,
		"counts":           result.Counts,
		"app":              result.App,
		"entrypoints":      result.Entrypoints,
		"modules":          result.Modules,
		"components":       result.Components,
		"infrastructure":   result.Infrastructure,
	})
}

func (h *Handler) loadAppMonitor(c *gin.Context) (*service.AppMonitorOverview, bool) {
	if h.monitor == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "系统监测服务暂不可用")
		return nil, false
	}
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return nil, false
	}
	// 优先返回缓存
	cached, _ := h.monitor.CachedAppOverview(c.Request.Context(), appID)
	if cached != nil {
		return cached, true
	}
	result, loadErr := h.monitor.AppOverview(c.Request.Context(), appID)
	if loadErr != nil {
		h.writeError(c, loadErr)
		return nil, false
	}
	return result, true
}

func (h *Handler) SystemMonitorHistory(c *gin.Context) {
	if h.monitor == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "系统监测服务暂不可用")
		return
	}
	keysRaw := c.Query("keys")
	if keysRaw == "" {
		response.Error(c, http.StatusBadRequest, 40000, "缺少 keys 参数")
		return
	}
	keys := splitKeys(keysRaw)
	rangeStr := c.DefaultQuery("range", "hour")
	result, err := h.monitor.SystemHistory(c.Request.Context(), keys, rangeStr)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 50312, "获取历史数据失败")
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) AppMonitorHistory(c *gin.Context) {
	if h.monitor == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "系统监测服务暂不可用")
		return
	}
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	keysRaw := c.Query("keys")
	if keysRaw == "" {
		response.Error(c, http.StatusBadRequest, 40000, "缺少 keys 参数")
		return
	}
	keys := splitKeys(keysRaw)
	rangeStr := c.DefaultQuery("range", "hour")
	result, err := h.monitor.AppHistory(c.Request.Context(), appID, keys, rangeStr)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 50312, "获取历史数据失败")
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) AdminSystemRuntime(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可查看系统运行信息")
		return
	}
	if h.monitor == nil {
		response.Error(c, http.StatusServiceUnavailable, 50310, "系统监测服务暂不可用")
		return
	}
	info := h.monitor.GetRuntimeInfo()
	response.Success(c, 200, "获取成功", info)
}

func splitKeys(raw string) []string {
	var keys []string
	for _, k := range strings.Split(raw, ",") {
		k = strings.TrimSpace(k)
		if k != "" {
			keys = append(keys, k)
		}
	}
	return keys
}

