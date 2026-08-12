package httptransport

// 版本管理的旧命名空间（/api/admin/app/version/*）。
//
// 由 router.go 拆出，函数体逐字节原样搬迁。

import (
	"net/http"

	appdomain "aegis/internal/domain/app"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

func (h *Handler) AdminVersionListCompat(c *gin.Context) {
	var req AdminAppVersionListRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.version.List(c.Request.Context(), req.AppID, appdomain.AppVersionListQuery{Page: normalizePage(req.Page), Limit: normalizeLimit(req.Limit), Status: req.Status, Platform: req.Platform, ChannelID: req.ChannelID})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

func (h *Handler) AdminVersionDetailCompat(c *gin.Context) {
	var req AdminAppVersionDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.version.Detail(c.Request.Context(), req.VersionID, req.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminVersionCreateCompat(c *gin.Context) {
	h.adminVersionSaveCompat(c, 0)
}

func (h *Handler) AdminVersionUpdateCompat(c *gin.Context) {
	var req AdminAppVersionSaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.version.Save(c.Request.Context(), appdomain.AppVersionMutation{
		ID:           req.VersionID,
		AppID:        req.AppID,
		ChannelID:    req.ChannelID,
		Version:      maybeString(req.Version),
		VersionCode:  maybeInt64(req.VersionCode),
		Description:  maybeString(req.Description),
		ReleaseNotes: maybeString(req.ReleaseNotes),
		DownloadURL:  maybeString(req.DownloadURL),
		FileSize:     maybeInt64(req.FileSize),
		FileHash:     maybeString(req.FileHash),
		ForceUpdate:  req.ForceUpdate,
		UpdateType:   maybeString(req.UpdateType),
		Platform:     maybeString(req.Platform),
		MinOSVersion: maybeString(req.MinOSVersion),
		Status:       maybeString(req.Status),
		Metadata:     req.Metadata,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "更新成功", item)
}

func (h *Handler) adminVersionSaveCompat(c *gin.Context, id int64) {
	var req AdminAppVersionSaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.version.Save(c.Request.Context(), appdomain.AppVersionMutation{
		ID:           id,
		AppID:        req.AppID,
		ChannelID:    req.ChannelID,
		Version:      maybeString(req.Version),
		VersionCode:  maybeInt64(req.VersionCode),
		Description:  maybeString(req.Description),
		ReleaseNotes: maybeString(req.ReleaseNotes),
		DownloadURL:  maybeString(req.DownloadURL),
		FileSize:     maybeInt64(req.FileSize),
		FileHash:     maybeString(req.FileHash),
		ForceUpdate:  req.ForceUpdate,
		UpdateType:   maybeString(req.UpdateType),
		Platform:     maybeString(req.Platform),
		MinOSVersion: maybeString(req.MinOSVersion),
		Status:       maybeString(req.Status),
		Metadata:     req.Metadata,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "创建成功", item)
}

func (h *Handler) AdminVersionDeleteCompat(c *gin.Context) {
	var req AdminAppVersionDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.version.Delete(c.Request.Context(), req.AppID, req.VersionID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", nil)
}

func (h *Handler) AdminVersionChannelListCompat(c *gin.Context) {
	var req RoleAppIDQuery
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, err := h.version.ListChannels(c.Request.Context(), req.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) AdminVersionChannelDetailCompat(c *gin.Context) {
	var req AdminVersionChannelDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.version.ChannelDetail(c.Request.Context(), req.ChannelID, req.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

func (h *Handler) AdminVersionChannelCreateCompat(c *gin.Context) {
	h.adminVersionChannelSaveCompat(c, 0)
}

func (h *Handler) AdminVersionChannelUpdateCompat(c *gin.Context) {
	h.adminVersionChannelSaveCompat(c, -1)
}

func (h *Handler) adminVersionChannelSaveCompat(c *gin.Context, createFlag int64) {
	var req AdminVersionChannelSaveRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	channelID := req.ChannelID
	if createFlag == 0 {
		channelID = 0
	}
	item, err := h.version.SaveChannel(c.Request.Context(), appdomain.AppVersionChannelMutation{
		ID:             channelID,
		AppID:          req.AppID,
		Name:           maybeString(req.Name),
		Code:           maybeString(req.Code),
		Description:    maybeString(req.Description),
		IsDefault:      req.IsDefault,
		Status:         req.Status,
		Priority:       req.Priority,
		Color:          maybeString(req.Color),
		Level:          maybeString(req.Level),
		RolloutPct:     req.RolloutPct,
		Platforms:      req.Platforms,
		MinVersionCode: req.MinVersionCode,
		MaxVersionCode: req.MaxVersionCode,
		Rules:          req.Rules,
		TargetAudience: req.TargetAudience,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := "创建成功"
	if channelID > 0 {
		message = "更新成功"
	}
	response.Success(c, 200, message, item)
}

func (h *Handler) AdminVersionChannelDeleteCompat(c *gin.Context) {
	var req AdminVersionChannelDetailRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.version.DeleteChannel(c.Request.Context(), req.AppID, req.ChannelID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", nil)
}

func (h *Handler) AdminVersionChannelUsersCompat(c *gin.Context) {
	var req AdminVersionChannelUsersRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, total, err := h.version.ListChannelUsers(c.Request.Context(), req.AppID, req.ChannelID, normalizePage(req.Page), normalizeLimit(req.Limit))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"items": items, "page": normalizePage(req.Page), "limit": normalizeLimit(req.Limit), "total": total, "totalPages": calcPages(total, normalizeLimit(req.Limit))})
}

func (h *Handler) AdminVersionChannelAddUsersCompat(c *gin.Context) {
	var req AdminVersionChannelUsersRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	added, err := h.version.AddChannelUsers(c.Request.Context(), req.AppID, req.ChannelID, req.UserIDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "添加成功", gin.H{"added": added, "skipped": len(req.UserIDs) - int(added)})
}

func (h *Handler) AdminVersionChannelRemoveUsersCompat(c *gin.Context) {
	var req AdminVersionChannelUsersRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	removed, err := h.version.RemoveChannelUsers(c.Request.Context(), req.AppID, req.ChannelID, req.UserIDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "移除成功", gin.H{"removed": removed})
}

func (h *Handler) AdminVersionStatsCompat(c *gin.Context) {
	var req RoleAppIDQuery
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	stats, err := h.version.Stats(c.Request.Context(), req.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", stats)
}

func (h *Handler) AdminVersionPreviewMatchCompat(c *gin.Context) {
	var req AdminVersionPreviewMatchRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if req.ChannelID == 0 {
		response.Success(c, 200, "获取成功", gin.H{"matchedUsers": 0})
		return
	}
	_, total, err := h.version.ListChannelUsers(c.Request.Context(), req.AppID, req.ChannelID, 1, 1)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"matchedUsers": total, "targetAudience": req.TargetAudience})
}
