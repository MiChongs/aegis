package httptransport

// 站内信（用户端）。
//
// 由 router.go 拆出，函数体逐字节原样搬迁。

import (
	"net/http"

	notificationdomain "aegis/internal/domain/notification"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

func (h *Handler) Notifications(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query NotificationQuery
	_ = bind(c, &query)
	items, err := h.notifications.List(c.Request.Context(), session, notificationdomain.UserListQuery{
		Status: query.Status,
		Type:   query.Type,
		Level:  query.Level,
		Page:   normalizePage(query.Page),
		Limit:  normalizeLimit(query.Limit),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) NotificationUnreadCount(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	count, err := h.notifications.UnreadCount(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"unread": count})
}

func (h *Handler) ReadNotification(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req NotificationReadRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.notifications.MarkRead(c.Request.Context(), session, req.NotificationID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已标记已读", gin.H{"notificationId": req.NotificationID})
}

func (h *Handler) ReadNotificationsBatch(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req NotificationReadBatchRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.notifications.MarkReadBatch(c.Request.Context(), session, req.IDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已批量标记已读", result)
}

func (h *Handler) ReadAllNotifications(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if err := h.notifications.MarkAllRead(c.Request.Context(), session); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "已全部标记已读", gin.H{"readAll": true})
}

func (h *Handler) DeleteNotification(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	notificationID, err := pathInt64(c, "notificationId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的通知标识")
		return
	}
	result, err := h.notifications.Delete(c.Request.Context(), session, notificationID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "删除成功", result)
}

func (h *Handler) ClearNotifications(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req NotificationClearRequest
	_ = bind(c, &req)
	result, err := h.notifications.ClearFiltered(c.Request.Context(), session, req.Status, req.Type, req.Level)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "清空成功", result)
}
