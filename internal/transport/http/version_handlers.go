package httptransport

import (
	appdomain "aegis/internal/domain/app"
	"aegis/pkg/response"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// ── 版本 CRUD ──

// AdminListVersions GET /api/admin/apps/:appkey/versions
func (h *Handler) AdminListVersions(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	query := appdomain.AppVersionListQuery{
		Page:      normalizePage(queryInt(c, "page", 1)),
		Limit:     normalizeLimit(queryInt(c, "limit", 20)),
		Status:    c.Query("status"),
		Platform:  c.Query("platform"),
		ChannelID: int64(queryInt(c, "channel_id", 0)),
	}
	result, err := h.version.List(c.Request.Context(), appID, query)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

// AdminCreateVersion POST /api/admin/apps/:appkey/versions
func (h *Handler) AdminCreateVersion(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminAppVersionSaveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.version.Save(c.Request.Context(), appdomain.AppVersionMutation{
		AppID:        appID,
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
	response.Success(c, 201, "创建成功", item)
}

// AdminGetVersion GET /api/admin/apps/:appkey/versions/:vid
func (h *Handler) AdminGetVersion(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	vid, err := strconv.ParseInt(c.Param("vid"), 10, 64)
	if err != nil || vid <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "版本 ID 无效")
		return
	}
	item, err := h.version.Detail(c.Request.Context(), vid, appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

// AdminUpdateVersion PUT /api/admin/apps/:appkey/versions/:vid
func (h *Handler) AdminUpdateVersion(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	vid, err := strconv.ParseInt(c.Param("vid"), 10, 64)
	if err != nil || vid <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "版本 ID 无效")
		return
	}
	var req AdminAppVersionSaveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.version.Save(c.Request.Context(), appdomain.AppVersionMutation{
		ID:           vid,
		AppID:        appID,
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

// AdminDeleteVersion DELETE /api/admin/apps/:appkey/versions/:vid
func (h *Handler) AdminDeleteVersion(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	vid, err := strconv.ParseInt(c.Param("vid"), 10, 64)
	if err != nil || vid <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "版本 ID 无效")
		return
	}
	if err := h.version.Delete(c.Request.Context(), appID, vid); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", nil)
}

// AdminPublishVersion POST /api/admin/apps/:appkey/versions/:vid/publish
func (h *Handler) AdminPublishVersion(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	vid, err := strconv.ParseInt(c.Param("vid"), 10, 64)
	if err != nil || vid <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "版本 ID 无效")
		return
	}
	item, err := h.version.Publish(c.Request.Context(), appID, vid)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "发布成功", item)
}

// AdminRevokeVersion POST /api/admin/apps/:appkey/versions/:vid/revoke
func (h *Handler) AdminRevokeVersion(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	vid, err := strconv.ParseInt(c.Param("vid"), 10, 64)
	if err != nil || vid <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "版本 ID 无效")
		return
	}
	item, err := h.version.Revoke(c.Request.Context(), appID, vid)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "撤回成功", item)
}

// AdminVersionStats GET /api/admin/apps/:appkey/versions/stats
func (h *Handler) AdminVersionStats(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	stats, err := h.version.Stats(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", stats)
}

// ── 渠道 CRUD ──

// AdminListVersionChannels GET /api/admin/apps/:appkey/channels
func (h *Handler) AdminListVersionChannels(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	items, err := h.version.ListChannels(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

// AdminCreateVersionChannel POST /api/admin/apps/:appkey/channels
func (h *Handler) AdminCreateVersionChannel(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	var req AdminVersionChannelSaveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.version.SaveChannel(c.Request.Context(), appdomain.AppVersionChannelMutation{
		AppID:          appID,
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
	response.Success(c, 201, "创建成功", item)
}

// AdminGetVersionChannel GET /api/admin/apps/:appkey/channels/:cid
func (h *Handler) AdminGetVersionChannel(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	cid, err := strconv.ParseInt(c.Param("cid"), 10, 64)
	if err != nil || cid <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "渠道 ID 无效")
		return
	}
	item, err := h.version.ChannelDetail(c.Request.Context(), cid, appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

// AdminUpdateVersionChannel PUT /api/admin/apps/:appkey/channels/:cid
func (h *Handler) AdminUpdateVersionChannel(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	cid, err := strconv.ParseInt(c.Param("cid"), 10, 64)
	if err != nil || cid <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "渠道 ID 无效")
		return
	}
	var req AdminVersionChannelSaveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.version.SaveChannel(c.Request.Context(), appdomain.AppVersionChannelMutation{
		ID:             cid,
		AppID:          appID,
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
	response.Success(c, 200, "更新成功", item)
}

// AdminDeleteVersionChannel DELETE /api/admin/apps/:appkey/channels/:cid
func (h *Handler) AdminDeleteVersionChannel(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	cid, err := strconv.ParseInt(c.Param("cid"), 10, 64)
	if err != nil || cid <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "渠道 ID 无效")
		return
	}
	if err := h.version.DeleteChannel(c.Request.Context(), appID, cid); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", nil)
}

// AdminListVersionChannelUsers GET /api/admin/apps/:appkey/channels/:cid/users
func (h *Handler) AdminListVersionChannelUsers(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	cid, err := strconv.ParseInt(c.Param("cid"), 10, 64)
	if err != nil || cid <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "渠道 ID 无效")
		return
	}
	page := normalizePage(queryInt(c, "page", 1))
	limit := normalizeLimit(queryInt(c, "limit", 20))
	items, total, err := h.version.ListChannelUsers(c.Request.Context(), appID, cid, page, limit)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{
		"items": items, "page": page, "limit": limit,
		"total": total, "totalPages": calcPages(total, limit),
	})
}

// AdminAddVersionChannelUsers POST /api/admin/apps/:appkey/channels/:cid/users
func (h *Handler) AdminAddVersionChannelUsers(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	cid, err := strconv.ParseInt(c.Param("cid"), 10, 64)
	if err != nil || cid <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "渠道 ID 无效")
		return
	}
	var body struct {
		UserIDs []int64 `json:"user_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	added, err := h.version.AddChannelUsers(c.Request.Context(), appID, cid, body.UserIDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "添加成功", gin.H{
		"added": added, "skipped": len(body.UserIDs) - int(added),
	})
}

// AdminRemoveVersionChannelUsers DELETE /api/admin/apps/:appkey/channels/:cid/users
func (h *Handler) AdminRemoveVersionChannelUsers(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	cid, err := strconv.ParseInt(c.Param("cid"), 10, 64)
	if err != nil || cid <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "渠道 ID 无效")
		return
	}
	var body struct {
		UserIDs []int64 `json:"user_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	removed, err := h.version.RemoveChannelUsers(c.Request.Context(), appID, cid, body.UserIDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "移除成功", gin.H{"removed": removed})
}

// queryInt 从查询参数中读取整数，解析失败时返回 fallback
func queryInt(c *gin.Context, key string, fallback int) int {
	v := c.Query(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}
