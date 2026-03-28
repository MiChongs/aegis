package httptransport

import (
	"fmt"
	"net/http"
	"strconv"

	admindomain "aegis/internal/domain/admin"
	captchadomain "aegis/internal/domain/captcha"
	"aegis/pkg/response"
	"github.com/gin-gonic/gin"
)

// AdminDashboard 管理员工作台数据
func (h *Handler) AdminDashboard(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok || session == nil {
		response.Error(c, http.StatusUnauthorized, 40100, "未登录")
		return
	}
	if h.dashboard == nil {
		response.Error(c, http.StatusServiceUnavailable, 50001, "工作台服务未初始化")
		return
	}
	data, err := h.dashboard.GetDashboard(c.Request.Context(), session.AdminID, session.IsSuperAdmin)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "ok", data)
}

// AdminLDAPPublicConfig 返回 LDAP 是否启用（公开端点，登录页用）
func (h *Handler) AdminLDAPPublicConfig(c *gin.Context) {
	enabled := h.ldapSvc != nil && h.ldapSvc.IsEnabled()
	response.Success(c, 200, "ok", gin.H{"enabled": enabled})
}

func (h *Handler) AdminLogin(c *gin.Context) {
	var req AdminLoginRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	// 验证码校验（如果全局配置要求）
	if h.system != nil {
		cfg := h.system.GetAdminCaptchaConfig(c.Request.Context())
		if cfg.Enabled && cfg.RequireForLogin {
			if err := h.verifyAdminCaptcha(c, req.CaptchaID, req.CaptchaAnswer); err != nil {
				return
			}
		}
	}
	result, err := h.admin.Login(c.Request.Context(), req.Account, req.Password, c.ClientIP(), c.GetHeader("User-Agent"))
	if err != nil {
		h.recordAuditWithAdmin(c, 0, req.Account, "admin.login_failed", "admin", "", "管理员 "+req.Account+" 登录失败", "failed")
		h.writeError(c, err)
		return
	}
	if result.RequiresSecondFactor {
		response.Success(c, 200, "需要双因子验证", result)
		return
	}
	h.attachAdminAccountAvatar(c, &result.Admin)
	response.Success(c, 200, "登录成功", result)
	h.recordAuditWithAdmin(c, result.Admin.ID, result.Admin.Account, "admin.login", "admin", strconv.FormatInt(result.Admin.ID, 10), "管理员 "+result.Admin.Account+" 登录", "success")
}

func (h *Handler) AdminVerifyMFA(c *gin.Context) {
	var req AdminVerifyMFARequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.admin.VerifyMFA(c.Request.Context(), req.ChallengeID, req.Code, req.RecoveryCode, c.ClientIP(), c.GetHeader("User-Agent"))
	if err != nil {
		h.recordAuditFailed(c, "admin.mfa_failed", "admin", "", "MFA 验证失败")
		h.writeError(c, err)
		return
	}
	h.attachAdminAccountAvatar(c, &result.Admin)
	response.Success(c, 200, "验证成功", result)
	h.recordAuditWithAdmin(c, result.Admin.ID, result.Admin.Account, "admin.login", "admin", strconv.FormatInt(result.Admin.ID, 10), "管理员 "+result.Admin.Account+" MFA 验证登录", "success")
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
	h.recordAudit(c, "admin.logout", "admin", "", "管理员登出")
}

func (h *Handler) AdminRegister(c *gin.Context) {
	var req AdminRegisterRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	// 验证码校验（如果全局配置要求）
	if h.system != nil {
		cfg := h.system.GetAdminCaptchaConfig(c.Request.Context())
		if cfg.Enabled && cfg.RequireForRegister {
			if err := h.verifyAdminCaptcha(c, req.CaptchaID, req.CaptchaAnswer); err != nil {
				return
			}
		}
	}
	profile, err := h.admin.RegisterAdmin(c.Request.Context(), req.Account, req.Password, req.DisplayName, req.Email)
	if err != nil {
		h.writeError(c, err)
		return
	}
	// 注册成功后自动登录
	result, loginErr := h.admin.Login(c.Request.Context(), req.Account, req.Password, c.ClientIP(), c.GetHeader("User-Agent"))
	if loginErr != nil {
		// 注册成功但登录失败，仍返回 profile
		response.Success(c, 201, "注册成功", profile)
		return
	}
	h.attachAdminAccountAvatar(c, &result.Admin)
	response.Success(c, 201, "注册成功", result)
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
	for i := range items {
		h.attachAdminProfileAvatar(c, &items[i])
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
	h.attachAdminProfileAvatar(c, item)
	response.Success(c, 200, "创建成功", item)
	h.recordAudit(c, "admin.create", "admin", strconv.FormatInt(item.Account.ID, 10), "创建管理员 "+req.Account)
}

func (h *Handler) AdminUpdateAccountStatus(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok || session == nil {
		response.Error(c, http.StatusUnauthorized, 40100, "管理员未登录")
		return
	}
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
	if err := h.admin.UpdateAdminStatus(c.Request.Context(), session.AdminID, adminID, req.Status); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "更新成功", gin.H{"id": adminID, "status": req.Status})
	h.recordAudit(c, "admin.status_change", "admin", strconv.FormatInt(adminID, 10), fmt.Sprintf("管理员 #%d 状态变更为 %s", adminID, req.Status))
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
	h.recordAudit(c, "admin.update_access", "admin", strconv.FormatInt(adminID, 10), fmt.Sprintf("修改管理员 #%d 权限", adminID))
}

func (h *Handler) AdminRoleCatalog(c *gin.Context) {
	response.Success(c, 200, "获取成功", h.admin.ListRoles())
}

func (h *Handler) AdminRolePermissionTree(c *gin.Context) {
	response.Success(c, 200, "获取成功", h.admin.ListRolesWithPermissionTree())
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

// verifyAdminCaptcha 校验管理员验证码，失败时写入响应并返回 error
func (h *Handler) verifyAdminCaptcha(c *gin.Context, captchaID, answer string) error {
	if captchaID == "" || answer == "" {
		response.Error(c, http.StatusBadRequest, 40093, "请输入验证码")
		c.Abort()
		return http.ErrAbortHandler
	}
	if h.captcha == nil {
		response.Error(c, http.StatusServiceUnavailable, 50321, "验证码服务暂不可用")
		c.Abort()
		return http.ErrAbortHandler
	}
	ok, err := h.captcha.Verify(c.Request.Context(), captchadomain.VerifyRequest{CaptchaID: captchaID, Answer: answer, Clear: true})
	if err != nil {
		h.writeError(c, err)
		c.Abort()
		return err
	}
	if !ok {
		response.Error(c, http.StatusBadRequest, 40094, "验证码错误")
		c.Abort()
		return http.ErrAbortHandler
	}
	return nil
}

// AdminCaptchaPublicConfig 返回管理员验证码配置（公开，登录/注册前调用）
func (h *Handler) AdminCaptchaPublicConfig(c *gin.Context) {
	if h.system == nil {
		response.Success(c, 200, "获取成功", gin.H{"enabled": false, "type": "image", "requireForLogin": false, "requireForRegister": false})
		return
	}
	cfg := h.system.GetAdminCaptchaConfig(c.Request.Context())
	response.Success(c, 200, "获取成功", cfg)
}
