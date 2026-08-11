package httptransport

import (
	"errors"
	"net/http"
	"strconv"

	"aegis/internal/service"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// writeServiceError 把服务层错误翻译成统一响应。
// databaseHandlers 不是 *Handler（无需注入全部服务），因此单独提供一份等价实现。
func writeServiceError(c *gin.Context, err error) {
	if appErr, ok := errors.AsType[*apperrors.AppError](err); ok {
		response.Error(c, appErr.HTTPStatus, appErr.Code, appErr.Message)
		return
	}
	_ = c.Error(err)
	summary := err.Error()
	if len(summary) > 200 {
		summary = summary[:200]
	}
	response.Error(c, http.StatusInternalServerError, 50000, summary)
}

// databaseHandlers 数据库生命周期与泄漏监控 API（仅超级管理员）。
//
// 读接口分两档：
//   - snapshot / leak / slow-queries 走内存缓存，零数据库往返，可高频轮询；
//   - refresh / sessions / maintenance 会真正查库，前端应按需触发而非定时刷。
type databaseHandlers struct {
	dm *service.DatabaseManager
}

func (h *databaseHandlers) guard(c *gin.Context, action string) bool {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40314, "仅超级管理员可"+action)
		return false
	}
	if h.dm == nil {
		response.Error(c, http.StatusServiceUnavailable, 50330, "数据库监控服务暂不可用")
		return false
	}
	return true
}

// AdminDatabaseSnapshot 完整快照（纯内存读）
func (h *databaseHandlers) AdminDatabaseSnapshot(c *gin.Context) {
	if !h.guard(c, "查看数据库监控信息") {
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, http.StatusOK, "获取成功", h.dm.Snapshot())
}

// AdminDatabaseRefresh 立即采集一次并返回最新快照
func (h *databaseHandlers) AdminDatabaseRefresh(c *gin.Context) {
	if !h.guard(c, "刷新数据库监控数据") {
		return
	}
	if _, err := h.dm.Refresh(c.Request.Context()); err != nil {
		writeServiceError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, http.StatusOK, "已刷新", h.dm.Snapshot())
}

// AdminDatabaseHistory 历史指标（Redis 时序）
func (h *databaseHandlers) AdminDatabaseHistory(c *gin.Context) {
	if !h.guard(c, "查看数据库监控信息") {
		return
	}
	items, err := h.dm.History(c.Request.Context(), c.Query("range"))
	if err != nil {
		writeServiceError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", gin.H{"items": items})
}

// AdminDatabaseLeak 泄漏检测报告
func (h *databaseHandlers) AdminDatabaseLeak(c *gin.Context) {
	if !h.guard(c, "查看数据库泄漏检测结果") {
		return
	}
	snapshot := h.dm.Snapshot()
	c.Header("Cache-Control", "no-store")
	response.Success(c, http.StatusOK, "获取成功", gin.H{
		"leak":         snapshot.Leak,
		"leakSuspects": snapshot.LeakSuspects,
		"inFlight":     snapshot.InFlight,
	})
}

// AdminDatabaseSlowQueries 慢查询样本
func (h *databaseHandlers) AdminDatabaseSlowQueries(c *gin.Context) {
	if !h.guard(c, "查看数据库慢查询") {
		return
	}
	snapshot := h.dm.Snapshot()
	c.Header("Cache-Control", "no-store")
	response.Success(c, http.StatusOK, "获取成功", gin.H{"items": snapshot.SlowQueries})
}

// AdminDatabaseSessions 服务端会话列表
func (h *databaseHandlers) AdminDatabaseSessions(c *gin.Context) {
	if !h.guard(c, "查看数据库会话") {
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	onlyProblematic := c.Query("onlyProblematic") == "true"
	items, err := h.dm.Sessions(c.Request.Context(), onlyProblematic, limit)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, http.StatusOK, "获取成功", gin.H{"items": items})
}

// AdminDatabaseTerminateSession 终止会话（回滚其事务并断开）
func (h *databaseHandlers) AdminDatabaseTerminateSession(c *gin.Context) {
	if !h.guard(c, "终止数据库会话") {
		return
	}
	pid, ok := parseBackendPID(c)
	if !ok {
		return
	}
	if err := h.dm.TerminateSession(c.Request.Context(), pid); err != nil {
		writeServiceError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "会话已终止", gin.H{"pid": pid, "terminated": true})
}

// AdminDatabaseCancelSession 取消会话正在执行的语句（保留连接与事务）
func (h *databaseHandlers) AdminDatabaseCancelSession(c *gin.Context) {
	if !h.guard(c, "取消数据库语句") {
		return
	}
	pid, ok := parseBackendPID(c)
	if !ok {
		return
	}
	if err := h.dm.CancelSession(c.Request.Context(), pid); err != nil {
		writeServiceError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "语句已取消", gin.H{"pid": pid, "canceled": true})
}

// AdminDatabaseMaintenance 存储侧健康视图：死元组表 + 未使用索引
func (h *databaseHandlers) AdminDatabaseMaintenance(c *gin.Context) {
	if !h.guard(c, "查看数据库维护信息") {
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	result, err := h.dm.Maintenance(c.Request.Context(), limit)
	if err != nil {
		writeServiceError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// AdminDatabaseWarmup 手动预热连接池
func (h *databaseHandlers) AdminDatabaseWarmup(c *gin.Context) {
	if !h.guard(c, "预热数据库连接池") {
		return
	}
	if err := h.dm.Warmup(c.Request.Context()); err != nil {
		writeServiceError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "连接池已预热", h.dm.Snapshot())
}

func parseBackendPID(c *gin.Context) (int32, bool) {
	value, err := strconv.ParseInt(c.Param("pid"), 10, 32)
	if err != nil || value <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "无效的会话 PID")
		return 0, false
	}
	return int32(value), true
}
