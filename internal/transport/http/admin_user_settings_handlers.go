package httptransport

// 用户设置的管理端视图与批量维护。
//
// 由 router.go 拆出，函数体逐字节原样搬迁。

import (
	"net/http"

	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

func (h *Handler) AdminUserSettingsStats(c *gin.Context) {
	var query AdminSettingsStatsQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.user.GetAdminSettingsStats(c.Request.Context(), query.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) AdminUserSettings(c *gin.Context) {
	var query AdminUserSettingsQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.user.GetAdminUserSettings(c.Request.Context(), query.AppID, query.UserID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) AdminBatchInitializeUserSettings(c *gin.Context) {
	var req AdminBatchInitializeSettingsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.user.BatchInitializeSettingsAdmin(c.Request.Context(), req.AppID, req.BatchSize, req.Categories)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "批量初始化完成", result)
}

func (h *Handler) AdminInitializeUserSettings(c *gin.Context) {
	var req AdminInitializeUserSettingsRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.user.InitializeUserSettingsAdmin(c.Request.Context(), req.AppID, req.UserID, req.Categories)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "用户设置初始化完成", result)
}

func (h *Handler) AdminCheckUserSettingsIntegrity(c *gin.Context) {
	var query AdminSettingsIntegrityQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.user.CheckAndRepairSettings(c.Request.Context(), query.AppID, query.AutoRepair)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "设置完整性检查完成", result)
}

func (h *Handler) AdminCleanupUserSettings(c *gin.Context) {
	var query AdminSettingsCleanupQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	dryRun := true
	if query.DryRun != nil {
		dryRun = *query.DryRun
	}
	result, err := h.user.CleanupInvalidSettingsAdmin(c.Request.Context(), query.AppID, dryRun)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "无效设置清理完成", result)
}
