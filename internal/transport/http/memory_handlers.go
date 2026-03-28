package httptransport

import (
	"net/http"

	"aegis/internal/service"
	"aegis/pkg/response"
	"github.com/gin-gonic/gin"
)

// memoryHandlers 内存管理 API（仅超级管理员）
type memoryHandlers struct {
	mm *service.MemoryManager
}

// AdminMemorySnapshot 获取内存管理完整快照
func (h *memoryHandlers) AdminMemorySnapshot(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可查看内存管理信息")
		return
	}
	if h.mm == nil {
		response.Error(c, http.StatusServiceUnavailable, 50320, "内存管理服务暂不可用")
		return
	}
	snap := h.mm.Snapshot()
	response.Success(c, 200, "获取成功", snap)
}

// AdminMemoryForceGC 触发强制 GC
func (h *memoryHandlers) AdminMemoryForceGC(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可执行内存管理操作")
		return
	}
	if h.mm == nil {
		response.Error(c, http.StatusServiceUnavailable, 50320, "内存管理服务暂不可用")
		return
	}
	h.mm.ForceGC()
	// 返回 GC 后的最新快照
	snap := h.mm.Snapshot()
	response.Success(c, 200, "强制 GC 已执行", snap)
}

// AdminMemorySetGOGC 手动设置 GOGC 值
func (h *memoryHandlers) AdminMemorySetGOGC(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可执行内存管理操作")
		return
	}
	if h.mm == nil {
		response.Error(c, http.StatusServiceUnavailable, 50320, "内存管理服务暂不可用")
		return
	}
	var req struct {
		Value int `json:"value" binding:"required,min=10,max=500"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "GOGC 值必须在 10~500 之间")
		return
	}
	old := h.mm.SetGOGC(req.Value)
	response.Success(c, 200, "GOGC 已更新", gin.H{
		"old": old,
		"new": req.Value,
	})
}

// AdminMemoryHistory 获取内存历史指标
func (h *memoryHandlers) AdminMemoryHistory(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可查看内存管理信息")
		return
	}
	if h.mm == nil {
		response.Error(c, http.StatusServiceUnavailable, 50320, "内存管理服务暂不可用")
		return
	}
	rangeStr := c.DefaultQuery("range", "hour")
	metrics, err := h.mm.History(c.Request.Context(), rangeStr)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 50321, "获取内存历史数据失败")
		return
	}
	response.Success(c, 200, "获取成功", metrics)
}

// AdminMemoryPoolStats 获取对象池统计
func (h *memoryHandlers) AdminMemoryPoolStats(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可查看内存管理信息")
		return
	}
	if h.mm == nil {
		response.Error(c, http.StatusServiceUnavailable, 50320, "内存管理服务暂不可用")
		return
	}
	pools := h.mm.Pools()
	response.Success(c, 200, "获取成功", pools.AllStats())
}

// AdminMemoryCacheStats 获取缓存统计
func (h *memoryHandlers) AdminMemoryCacheStats(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可查看内存管理信息")
		return
	}
	if h.mm == nil {
		response.Error(c, http.StatusServiceUnavailable, 50320, "内存管理服务暂不可用")
		return
	}
	cache := h.mm.Cache()
	response.Success(c, 200, "获取成功", cache.Stats())
}

// AdminMemoryFlushCaches 清空本地缓存
func (h *memoryHandlers) AdminMemoryFlushCaches(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可执行内存管理操作")
		return
	}
	if h.mm == nil {
		response.Error(c, http.StatusServiceUnavailable, 50320, "内存管理服务暂不可用")
		return
	}
	h.mm.FlushCaches()
	cache := h.mm.Cache()
	response.Success(c, 200, "缓存已清空", cache.Stats())
}

// AdminMemoryLeakReport 获取泄漏检测报告
func (h *memoryHandlers) AdminMemoryLeakReport(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可查看内存管理信息")
		return
	}
	if h.mm == nil {
		response.Error(c, http.StatusServiceUnavailable, 50320, "内存管理服务暂不可用")
		return
	}
	report := h.mm.GetLeakReport()
	if report == nil {
		response.Error(c, http.StatusNotFound, 40450, "泄漏检测未启用")
		return
	}
	response.Success(c, 200, "获取成功", report)
}
