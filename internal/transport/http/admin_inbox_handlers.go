package httptransport

import (
	"net/http"
	"strconv"

	notificationdomain "aegis/internal/domain/notification"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 管理员收件箱接口。
//
// 一律以当前会话的 adminID 为准，不接受前端传入的 adminId ——
// 收件箱是私有资源，越权读写没有任何合理场景。

// AdminInboxIDsRequest 批量已读/删除入参。
type AdminInboxIDsRequest struct {
	IDs []int64 `json:"ids"`
	// OnlyRead=true 且 IDs 为空时表示"清空已读"，否则为"清空全部"
	OnlyRead bool `json:"onlyRead"`
}

// ListAdminInbox 收件箱分页
// GET /api/admin/notifications
func (h *Handler) ListAdminInbox(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	result, err := h.adminInbox.List(c.Request.Context(), session.AdminID, notificationdomain.AdminInboxQuery{
		Status:   c.Query("status"),
		Type:     c.Query("type"),
		Level:    c.Query("level"),
		Resource: c.Query("resource"),
		Keyword:  c.Query("keyword"),
		Page:     page,
		Limit:    limit,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", result)
}

// AdminInboxUnreadCount 未读数（角标兜底轮询；正常由实时事件驱动）
// GET /api/admin/notifications/unread-count
func (h *Handler) AdminInboxUnreadCount(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	count, err := h.adminInbox.UnreadCount(c.Request.Context(), session.AdminID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", gin.H{"unread": count})
}

// MarkAdminInboxRead 标记已读（ids 为空 = 全部已读）
// POST /api/admin/notifications/read
func (h *Handler) MarkAdminInboxRead(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	var req AdminInboxIDsRequest
	_ = bind(c, &req)
	result, err := h.adminInbox.MarkRead(c.Request.Context(), session.AdminID, req.IDs)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "操作成功", result)
}

// DeleteAdminInbox 删除通知（ids 为空 = 按 onlyRead 清空）
// POST /api/admin/notifications/delete
func (h *Handler) DeleteAdminInbox(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	var req AdminInboxIDsRequest
	_ = bind(c, &req)
	result, err := h.adminInbox.Delete(c.Request.Context(), session.AdminID, req.IDs, req.OnlyRead)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "删除成功", result)
}
