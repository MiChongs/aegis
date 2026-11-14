package httptransport

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	admindomain "aegis/internal/domain/admin"
	captchadomain "aegis/internal/domain/captcha"
	systemdomain "aegis/internal/domain/system"
	"aegis/internal/service"
	"aegis/pkg/response"
	"aegis/pkg/timeutil"
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
	// 控制台按浏览器时区计「今日」，tz 为 IANA 时区名；缺省或无法识别时退回平台默认时区
	loc := timeutil.DefaultLocation()
	if tz := strings.TrimSpace(c.Query("tz")); tz != "" {
		if parsed, err := timeutil.LoadLocation(tz); err == nil {
			loc = parsed
		}
	}
	data, err := h.dashboard.GetDashboard(c.Request.Context(), session.AdminID, session.IsSuperAdmin, loc)
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
		// 失败侧：尚未确认 provider，先以 "password" 记；UI 能看到账号 + 失败原因
		h.recordAuditAuth(c, AuthAuditParams{
			AdminName: req.Account,
			Provider:  "password",
			Event:     "login",
			Status:    "failed",
			Reason:    err.Error(),
		})
		h.writeError(c, err)
		return
	}
	if result.RequiresSecondFactor {
		response.Success(c, 200, "需要双因子验证", result)
		// 登录到达凭证正确但待 MFA 的状态，也算一次审计事件（便于排查 MFA 超时）
		h.recordAuditAuth(c, AuthAuditParams{
			AdminID:     result.Admin.ID,
			AdminName:   result.Admin.Account,
			DisplayName: result.Admin.DisplayName,
			Provider:    authProviderFromAccount(result.Admin.AuthSource),
			Event:       "login",
			Status:      systemdomain.AuditStatusSuccess,
			MFARequired: true,
		})
		return
	}
	h.attachAdminAccountAvatar(c, &result.Admin)
	response.Success(c, 200, "登录成功", result)
	h.recordAuditAuth(c, AuthAuditParams{
		AdminID:     result.Admin.ID,
		AdminName:   result.Admin.Account,
		DisplayName: result.Admin.DisplayName,
		Provider:    authProviderFromAccount(result.Admin.AuthSource),
		Event:       "login",
		Status:      systemdomain.AuditStatusSuccess,
	})
}

func (h *Handler) AdminVerifyMFA(c *gin.Context) {
	var req AdminVerifyMFARequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	result, err := h.admin.VerifyMFA(c.Request.Context(), req.ChallengeID, req.Code, req.RecoveryCode, c.ClientIP(), c.GetHeader("User-Agent"))
	if err != nil {
		h.recordAuditAuth(c, AuthAuditParams{
			Provider: "mfa",
			Event:    "verify",
			Status:   "failed",
			Reason:   err.Error(),
		})
		h.writeError(c, err)
		return
	}
	h.attachAdminAccountAvatar(c, &result.Admin)
	response.Success(c, 200, "验证成功", result)
	h.recordAuditAuth(c, AuthAuditParams{
		AdminID:     result.Admin.ID,
		AdminName:   result.Admin.Account,
		DisplayName: result.Admin.DisplayName,
		Provider:    "mfa",
		Event:       "verify",
		Status:      systemdomain.AuditStatusSuccess,
	})
}

// AdminPasskeyLoginOptions POST /api/admin/auth/passkey/options
// 发起通行密钥登录，返回挑战与 navigator.credentials.get 的选项（可发现凭据，不需要账号）。
func (h *Handler) AdminPasskeyLoginOptions(c *gin.Context) {
	loginSession, options, err := h.admin.BeginPasskeyLogin(c.Request.Context(), c.ClientIP(), c.GetHeader("User-Agent"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取成功", gin.H{"session": loginSession, "options": options})
}

// AdminPasskeyLogin POST /api/admin/auth/passkey/login
// 校验断言并签发会话；返回体与密码登录成功时相同。
func (h *Handler) AdminPasskeyLogin(c *gin.Context) {
	rawBody, _ := snapshotRequestBody(c)
	var req AdminPasskeyLoginRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	payload := credentialPayload(rawBody, req.Credential, req.Payload)
	if len(payload) == 0 {
		response.Error(c, http.StatusBadRequest, 40000, "通行密钥凭证数据不能为空")
		return
	}
	result, err := h.admin.LoginWithPasskey(c.Request.Context(), req.ChallengeID, payload, c.ClientIP(), c.GetHeader("User-Agent"))
	if err != nil {
		params := AuthAuditParams{Provider: "passkey", Event: "login", Status: "failed", Reason: err.Error()}
		if result != nil {
			params.AdminID = result.Admin.ID
			params.AdminName = result.Admin.Account
			params.DisplayName = result.Admin.DisplayName
		}
		h.recordAuditAuth(c, params)
		h.writeError(c, err)
		return
	}
	h.attachAdminAccountAvatar(c, &result.Admin)
	response.Success(c, 200, "登录成功", result)
	h.recordAuditAuth(c, AuthAuditParams{
		AdminID:     result.Admin.ID,
		AdminName:   result.Admin.Account,
		DisplayName: result.Admin.DisplayName,
		Provider:    "passkey",
		Event:       "login",
		Status:      systemdomain.AuditStatusSuccess,
	})
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
	// 读取会话以在审计中留下身份（登出后 context 仍保留 session）
	sess, _ := adminAccessSession(c)
	if err := h.admin.Logout(c.Request.Context(), token); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "退出成功", gin.H{"logout": true})
	p := AuthAuditParams{Event: "logout", Status: systemdomain.AuditStatusSuccess, Provider: "password"}
	if sess != nil {
		p.AdminID = sess.AdminID
		p.AdminName = sess.Account
		p.DisplayName = sess.DisplayName
	}
	h.recordAuditAuth(c, p)
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
		h.recordAuditAuth(c, AuthAuditParams{
			AdminName:   req.Account,
			DisplayName: req.DisplayName,
			Provider:    "password",
			Event:       "register",
			Status:      "failed",
			Reason:      err.Error(),
		})
		h.writeError(c, err)
		return
	}
	// 注册成功后自动登录
	result, loginErr := h.admin.Login(c.Request.Context(), req.Account, req.Password, c.ClientIP(), c.GetHeader("User-Agent"))
	if loginErr != nil {
		// 注册成功但登录失败，仍返回 profile
		response.Success(c, 201, "注册成功", profile)
		h.recordAuditAuth(c, AuthAuditParams{
			AdminID:     profile.Account.ID,
			AdminName:   profile.Account.Account,
			DisplayName: profile.Account.DisplayName,
			Provider:    "password",
			Event:       "register",
			Status:      systemdomain.AuditStatusSuccess,
		})
		return
	}
	h.attachAdminAccountAvatar(c, &result.Admin)
	response.Success(c, 201, "注册成功", result)
	h.recordAuditAuth(c, AuthAuditParams{
		AdminID:     result.Admin.ID,
		AdminName:   result.Admin.Account,
		DisplayName: result.Admin.DisplayName,
		Provider:    "password",
		Event:       "register",
		Status:      systemdomain.AuditStatusSuccess,
	})
	// 注册后自动登录，再记一条 login 事件，清晰表达 "注册 → 登录" 双事件
	h.recordAuditAuth(c, AuthAuditParams{
		AdminID:     result.Admin.ID,
		AdminName:   result.Admin.Account,
		DisplayName: result.Admin.DisplayName,
		Provider:    authProviderFromAccount(result.Admin.AuthSource),
		Event:       "login",
		Status:      systemdomain.AuditStatusSuccess,
	})
}

// authProviderFromAccount 把 Account.AuthSource 规范化为审计 provider 字段
//
//	"" / "local" → password；其余小写透传
func authProviderFromAccount(source string) string {
	s := strings.ToLower(strings.TrimSpace(source))
	if s == "" || s == "local" {
		return "password"
	}
	return s
}

// AdminMe 当前会话 + 展开后的生效权限 + 自助能力。
//
// 回包内嵌原来的 AccessContext，字段位置全部不变，只是多了 permissions
// 与 selfService 两块。多这两块是为了让控制台**不必自己推导权限**：
// 光有角色 key 的话前端得维护一份"角色 → 权限"的副本，那份副本一旦与
// builtInAdminRoles 漂移，用户看到的就是"按钮在、点了 403"；
// 而 selfService 直接回答「新建应用这个入口现在能不能点、不能点是为什么」。
func (h *Handler) AdminMe(c *gin.Context) {
	session, ok := adminAccessSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40110, "管理员未认证")
		return
	}
	response.Success(c, 200, "获取成功", h.admin.AccessSnapshot(c.Request.Context(), session))
}

// AdminSelfServiceConfig 自助能力的公开视图（免登录）。
//
// 登录页要靠它决定「注册」链接显不显示。放在公开端点是刻意的：
// 关掉注册之后，注册页还挂在那里、提交才报错，是最容易被当成故障上报的一种体验。
func (h *Handler) AdminSelfServiceConfig(c *gin.Context) {
	response.Success(c, 200, "ok", gin.H{
		"registrationEnabled": h.admin.RegistrationEnabled(c.Request.Context()),
	})
}

func (h *Handler) AdminListAccounts(c *gin.Context) {
	items, err := h.admin.ListAdmins(c.Request.Context())
	if err != nil {
		h.writeError(c, err)
		return
	}
	session, _ := adminAccessSession(c)

	// 第三方认证源的 loginAvailability 只给超管看（避免暴露 LDAP/OIDC/SAML 的健康信息）；
	// Snapshot() 纯内存读，实际探测由后台 30s 定时循环完成。
	var health *service.AuthProviderHealthSnapshot
	if session != nil && session.IsSuperAdmin && h.authProviderHealth != nil {
		snapshot := h.authProviderHealth.Snapshot()
		health = &snapshot
	}
	enriched := make([]AdminListItem, 0, len(items))
	for i := range items {
		h.attachAdminProfileAvatar(c, &items[i])
		item := AdminListItem{Profile: items[i], Capabilities: h.admin.AdminCapabilities(c.Request.Context(), session, items[i].Account)}
		if src := strings.ToLower(strings.TrimSpace(items[i].Account.AuthSource)); health != nil && src != "" && src != "password" {
			status := health.ForSource(src)
			item.LoginAvailability = &AdminLoginAvailability{
				Source:    src,
				Available: status.Available,
				Reason:    status.Reason,
				CheckedAt: status.CheckedAt,
			}
		}
		enriched = append(enriched, item)
	}
	response.Success(c, 200, "获取成功", enriched)
}

// AdminGetAccount 管理员详情：资料、角色分配、按数据权限展开后的分配、生效权限、个人授权与能力。
func (h *Handler) AdminGetAccount(c *gin.Context) {
	adminID, err := pathInt64(c, "adminId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的管理员标识")
		return
	}
	session, _ := adminAccessSession(c)
	detail, err := h.admin.GetAdminDetail(c.Request.Context(), session, adminID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.attachAdminProfileAvatar(c, &detail.Profile)
	response.Success(c, 200, "ok", detail)
}

func (h *Handler) AdminCreateAccount(c *gin.Context) {
	var req AdminCreateRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	session, _ := adminAccessSession(c)
	item, err := h.admin.CreateAdmin(c.Request.Context(), session, admindomain.CreateInput{
		Account:      req.Account,
		Password:     req.Password,
		DisplayName:  req.DisplayName,
		Email:        req.Email,
		IsSuperAdmin: req.IsSuperAdmin,
		Tier:         req.Tier,
		Assignments:  req.Assignments,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.attachAdminProfileAvatar(c, item)
	response.Success(c, 200, "创建成功", item)
	h.recordAudit(c, "admin.create", "admin", strconv.FormatInt(item.Account.ID, 10),
		fmt.Sprintf("创建%s %s", admindomain.TierName(item.Account.Tier), req.Account))
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
	if err := h.admin.UpdateAdminStatus(c.Request.Context(), session, adminID, req.Status); err != nil {
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
	session, _ := adminAccessSession(c)
	if err := h.admin.UpdateAdminAccess(c.Request.Context(), session, adminID, admindomain.UpdateAccessInput{
		IsSuperAdmin: req.IsSuperAdmin,
		Tier:         req.Tier,
		Assignments:  req.Assignments,
	}); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "更新成功", gin.H{"id": adminID})
	tier := req.Tier
	if tier == "" && req.IsSuperAdmin {
		tier = admindomain.TierSuper
	}
	h.recordAudit(c, "admin.update_access", "admin", strconv.FormatInt(adminID, 10),
		fmt.Sprintf("修改管理员 #%d 授权：身份 %s，角色 %d 项", adminID, admindomain.TierName(tier), len(req.Assignments)))
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
	ok, err := h.captcha.Verify(c.Request.Context(), captchadomain.VerifyRequest{
		CaptchaID: captchaID, Answer: answer, Clear: true,
		ExpectedPurpose: captchadomain.PurposeAdminLogin, ExpectedScope: captchadomain.ScopeAdmin,
	})
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
