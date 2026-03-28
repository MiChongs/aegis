package httptransport

import (
	"net/http"

	"aegis/pkg/crashlog"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// crashLogHandlers 持有崩溃日志管理器引用（不属于 Handler 结构体，因为 crashlog 不是业务 service）
type crashLogHandlers struct {
	cl *crashlog.Logger
}

func (h *crashLogHandlers) ListCrashLogs(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可查看崩溃日志")
		return
	}
	if h.cl == nil {
		response.Success(c, 200, "获取成功", []any{})
		return
	}
	files, err := h.cl.ListFiles()
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 50000, err.Error())
		return
	}
	if files == nil {
		files = []crashlog.FileInfo{}
	}
	response.Success(c, 200, "获取成功", files)
}

func (h *crashLogHandlers) GetCrashLog(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可查看崩溃日志")
		return
	}
	filename := c.Param("filename")
	if filename == "" {
		response.Error(c, http.StatusBadRequest, 40000, "filename is required")
		return
	}
	if h.cl == nil {
		response.Error(c, http.StatusNotFound, 40400, "崩溃日志管理器未初始化")
		return
	}
	data, err := h.cl.ReadFile(filename)
	if err != nil {
		response.Error(c, http.StatusNotFound, 40400, "崩溃日志不存在")
		return
	}
	// 返回原始 JSON Lines 文本
	c.Data(http.StatusOK, "application/x-ndjson; charset=utf-8", data)
}

func (h *crashLogHandlers) DeleteCrashLog(c *gin.Context) {
	if _, ok := requireSuperAdminSession(c); !ok {
		response.Error(c, http.StatusForbidden, 40313, "仅超级管理员可删除崩溃日志")
		return
	}
	filename := c.Param("filename")
	if filename == "" {
		response.Error(c, http.StatusBadRequest, 40000, "filename is required")
		return
	}
	if h.cl == nil {
		response.Error(c, http.StatusNotFound, 40400, "崩溃日志管理器未初始化")
		return
	}
	if err := h.cl.DeleteFile(filename); err != nil {
		response.Error(c, http.StatusNotFound, 40400, "崩溃日志不存在或删除失败")
		return
	}
	response.Success(c, 200, "删除成功", nil)
}
