package httptransport

// 角色申请（用户端）。
//
// 由 router.go 拆出，函数体逐字节原样搬迁。

import (
	"net/http"

	userdomain "aegis/internal/domain/user"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

func (h *Handler) SubmitRoleApplication(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req RoleApplyRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.roleApp.Submit(c.Request.Context(), session, req.AppID, req.RequestedRole, req.Reason, req.Priority, req.ValidDays, map[string]any{
		"ip":        c.ClientIP(),
		"userAgent": c.Request.UserAgent(),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "角色申请提交成功", item)
}

func (h *Handler) RoleApplications(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query RoleApplicationsQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, err := h.roleApp.UserList(c.Request.Context(), session, query.AppID, userdomain.RoleApplicationListQuery{
		Page:   normalizePage(query.Page),
		Limit:  normalizeLimit(query.Limit),
		Status: query.Status,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取角色申请列表成功", items)
}

func (h *Handler) RoleApplicationDetail(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query RoleAppIDQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	applicationID, err := pathInt64(c, "applicationId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "申请ID必须是整数")
		return
	}
	item, err := h.roleApp.UserDetail(c.Request.Context(), session, query.AppID, applicationID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取申请详情成功", item)
}

func (h *Handler) CancelRoleApplication(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req RoleAppIDQuery
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	applicationID, err := pathInt64(c, "applicationId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "申请ID必须是整数")
		return
	}
	item, err := h.roleApp.Cancel(c.Request.Context(), session, req.AppID, applicationID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "申请已取消", item)
}

func (h *Handler) AvailableRoles(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var query RoleAppIDQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	items, err := h.roleApp.AvailableRoles(c.Request.Context(), session, query.AppID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取可申请角色列表成功", items)
}

func (h *Handler) ResubmitRoleApplication(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req RoleResubmitRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	applicationID, err := pathInt64(c, "applicationId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "申请ID必须是整数")
		return
	}
	item, err := h.roleApp.Resubmit(c.Request.Context(), session, req.AppID, applicationID, req.Reason)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "重新提交成功，请等待审核", item)
}
