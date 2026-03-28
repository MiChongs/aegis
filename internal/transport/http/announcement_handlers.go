package httptransport

import (
	"net/http"
	"strconv"

	systemdomain "aegis/internal/domain/system"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// ── 管理员 CRUD ──

// AdminListAnnouncements GET /api/admin/system/announcements
func (h *Handler) AdminListAnnouncements(c *gin.Context) {
	result, err := h.announcement.List(c.Request.Context(), systemdomain.AnnouncementListQuery{
		Status: c.Query("status"),
		Type:   c.Query("type"),
		Level:  c.Query("level"),
		Page:   queryInt(c, "page", 1),
		Limit:  queryInt(c, "limit", 20),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

// AdminCreateAnnouncement POST /api/admin/system/announcements
func (h *Handler) AdminCreateAnnouncement(c *gin.Context) {
	access, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "未认证")
		return
	}
	var req AnnouncementSaveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.announcement.Save(c.Request.Context(), systemdomain.AnnouncementMutation{
		AdminID:   access.AdminID,
		Type:      maybeString(req.Type),
		Title:     maybeString(req.Title),
		Content:   maybeString(req.Content),
		Level:     maybeString(req.Level),
		Pinned:    req.Pinned,
		ExpiresAt: req.ExpiresAt,
		Metadata:  req.Metadata,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 201, "创建成功", item)
}

// AdminGetAnnouncement GET /api/admin/system/announcements/:id
func (h *Handler) AdminGetAnnouncement(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "ID 无效")
		return
	}
	item, err := h.announcement.Detail(c.Request.Context(), id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", item)
}

// AdminUpdateAnnouncement PUT /api/admin/system/announcements/:id
func (h *Handler) AdminUpdateAnnouncement(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "ID 无效")
		return
	}
	var req AnnouncementSaveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.announcement.Save(c.Request.Context(), systemdomain.AnnouncementMutation{
		ID:        id,
		Type:      maybeString(req.Type),
		Title:     maybeString(req.Title),
		Content:   maybeString(req.Content),
		Level:     maybeString(req.Level),
		Pinned:    req.Pinned,
		ExpiresAt: req.ExpiresAt,
		Metadata:  req.Metadata,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "更新成功", item)
}

// AdminDeleteAnnouncement DELETE /api/admin/system/announcements/:id
func (h *Handler) AdminDeleteAnnouncement(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "ID 无效")
		return
	}
	if err := h.announcement.Delete(c.Request.Context(), id); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", nil)
}

// AdminPublishAnnouncement POST /api/admin/system/announcements/:id/publish
func (h *Handler) AdminPublishAnnouncement(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "ID 无效")
		return
	}
	item, err := h.announcement.Publish(c.Request.Context(), id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "发布成功", item)
}

// AdminArchiveAnnouncement POST /api/admin/system/announcements/:id/archive
func (h *Handler) AdminArchiveAnnouncement(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.Error(c, http.StatusBadRequest, 40000, "ID 无效")
		return
	}
	item, err := h.announcement.Archive(c.Request.Context(), id)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "归档成功", item)
}

// ── 公开端点 ──

// ActiveAnnouncements GET /api/system/announcements/active
func (h *Handler) ActiveAnnouncements(c *gin.Context) {
	items, err := h.announcement.ListActive(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

// ── DTO ──

type AnnouncementSaveRequest struct {
	Type      string         `json:"type"`
	Title     string         `json:"title" binding:"required"`
	Content   string         `json:"content"`
	Level     string         `json:"level"`
	Pinned    *bool          `json:"pinned"`
	ExpiresAt *string        `json:"expiresAt"`
	Metadata  map[string]any `json:"metadata"`
}
