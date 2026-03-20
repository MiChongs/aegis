package httptransport

import (
	"net/http"

	admindomain "aegis/internal/domain/admin"
	"aegis/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *Handler) AdminLogin(c *gin.Context) {
	var req AdminLoginRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.admin.Login(c.Request.Context(), req.Account, req.Password)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "登录成功", result)
}

func (h *Handler) AdminLogout(c *gin.Context) {
	token := middlewareBearer(c.GetHeader("Authorization"))
	if token == "" {
		token = c.GetHeader("X-Admin-Token")
	}
	if token == "" {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员令牌无效")
		return
	}
	if err := h.admin.Logout(c.Request.Context(), token); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "退出成功", gin.H{"logout": true})
}

func (h *Handler) AdminMe(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	response.Success(c, 200, "获取成功", session)
}

func (h *Handler) AdminListAccounts(c *gin.Context) {
	items, err := h.admin.ListAdmins(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", items)
}

func (h *Handler) AdminCreateAccount(c *gin.Context) {
	var req AdminCreateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.admin.CreateAdmin(c.Request.Context(), admindomain.CreateInput{
		Account:      req.Account,
		Password:     req.Password,
		DisplayName:  req.DisplayName,
		Email:        req.Email,
		IsSuperAdmin: req.IsSuperAdmin,
		Assignments:  req.Assignments,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "创建成功", item)
}

func (h *Handler) AdminUpdateAccountStatus(c *gin.Context) {
	adminID, err := pathInt64(c, "adminId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的管理员标识")
		return
	}
	var req AdminStatusUpdateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.admin.UpdateAdminStatus(c.Request.Context(), adminID, req.Status); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "更新成功", gin.H{"id": adminID, "status": req.Status})
}

func (h *Handler) AdminUpdateAccountAccess(c *gin.Context) {
	adminID, err := pathInt64(c, "adminId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的管理员标识")
		return
	}
	var req AdminAccessUpdateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.admin.UpdateAdminAccess(c.Request.Context(), adminID, admindomain.UpdateAccessInput{
		IsSuperAdmin: req.IsSuperAdmin,
		Assignments:  req.Assignments,
	}); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "更新成功", gin.H{"id": adminID})
}

func (h *Handler) AdminRoleCatalog(c *gin.Context) {
	response.Success(c, 200, "获取成功", h.admin.ListRoles())
}

func adminAccessSession(c *gin.Context) (*admindomain.AccessContext, bool) {
	value, ok := c.Get("admin.session")
	if !ok {
		return nil, false
	}
	session, _ := value.(*admindomain.AccessContext)
	return session, session != nil
}

func adminActor(c *gin.Context) (int64, string) {
	session, ok := adminAccessSession(c)
	if !ok || session == nil {
		return 0, ""
	}
	return session.AdminID, session.DisplayName
}

func adminAccount(c *gin.Context) (int64, string) {
	session, ok := adminAccessSession(c)
	if !ok || session == nil {
		return 0, ""
	}
	return session.AdminID, session.Account
}
