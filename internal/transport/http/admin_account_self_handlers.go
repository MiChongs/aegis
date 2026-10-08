package httptransport

import (
	"net/http"
	"strconv"

	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 管理员账号自助：一次性改名与修改密码。规则见 service/admin_account_self_service.go。

// AdminAccountAvailability 检查新用户名是否可用。不可用时同样返回 200，原因放在 reason / message。
func (h *Handler) AdminAccountAvailability(c *gin.Context) {
	access, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	var query AdminAccountAvailabilityQuery
	if err := bind(c, &query); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.admin.CheckAccountAvailability(c.Request.Context(), access.AdminID, query.Account)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", result)
}

// ChangeAdminAccount 使用唯一一次改名机会。
func (h *Handler) ChangeAdminAccount(c *gin.Context) {
	access, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	var req AdminAccountChangeRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	previous := access.Account
	profile, err := h.admin.ChangeAccount(c.Request.Context(), access.AdminID, req.Account, req.CurrentPassword)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.attachAdminProfileAvatar(c, profile)
	h.recordAudit(c, "admin.account.rename", "admin", strconv.FormatInt(access.AdminID, 10),
		"修改用户名 "+previous+" → "+profile.Account.Account)
	response.Success(c, 200, "用户名已修改", profile)
}

// ChangeAdminPassword 修改自己的密码，默认同时下线其他设备。
func (h *Handler) ChangeAdminPassword(c *gin.Context) {
	access, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	var req AdminPasswordChangeRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	signOutOthers := req.SignOutOthers == nil || *req.SignOutOthers
	result, err := h.admin.ChangePassword(c.Request.Context(), access, req.CurrentPassword, req.NewPassword, req.Code, req.RecoveryCode, signOutOthers)
	if err != nil {
		h.writeError(c, err)
		return
	}
	detail := "修改密码"
	if result.RevokedSessions > 0 {
		detail += "，下线其他会话 " + strconv.FormatInt(result.RevokedSessions, 10) + " 个"
	}
	h.recordAudit(c, "admin.password.change", "admin", strconv.FormatInt(access.AdminID, 10), detail)
	response.Success(c, 200, "密码已修改", result)
}
