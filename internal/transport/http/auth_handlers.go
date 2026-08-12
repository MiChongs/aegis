package httptransport

// 旧明文认证命名空间的处理器（/api/auth/*）。
//
// 由 router.go 拆出，函数体逐字节原样搬迁。

import (
	"net/http"

	authdomain "aegis/internal/domain/auth"
	captchadomain "aegis/internal/domain/captcha"
	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

func (h *Handler) PasswordLogin(c *gin.Context) {
	var req PasswordLoginRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if !h.enforceLegacyAuthProtocol(c, req.AppID, "password", false) {
		return
	}
	// 前置校验：如果该 App 启用了图形验证码，必须校验通过才能登录
	if !h.verifyAppCaptcha(c, req.AppID, req.CaptchaID, req.CaptchaAnswer, captchadomain.PurposeLogin) {
		return
	}
	// 前置校验：登录设备检查 — 若 App 开启，必须由客户端显式提供 deviceId + device
	// 合法来源：body(deviceId/device) 或 Header(X-Device-Id/X-Device-Name)，UA 推断不算数
	if !h.enforceDevicePolicy(c, req.AppID, req.DeviceID, req.Device, req.MarkCode, captchadomain.PurposeLogin) {
		return
	}
	// 1) 取客户端显式值（body / header），未做任何加工
	deviceID, clientDevice := resolveClientDevice(c, req.DeviceID, req.Device, req.MarkCode)
	// 2) 查字典：命中 → 营销名；未命中 → 保留 clientDevice 原样
	device := h.enrichDeviceFromDict(c, deviceID, clientDevice)
	// 3) 客户端未传且字典未命中时，再用 UA 兜底
	if device == "" {
		device = guessDeviceFromUA(c.Request.UserAgent())
	}
	result, err := h.auth.PasswordLogin(c.Request.Context(), req.AppID, req.Account, req.Password, deviceID, device, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, authResultMessage(result, "登录成功"), result)
}

func (h *Handler) PasswordRegister(c *gin.Context) {
	var req PasswordRegisterRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if !h.enforceLegacyAuthProtocol(c, req.AppID, "password", true) {
		return
	}
	// 前置校验：注册同样受 App 验证码策略约束
	if !h.verifyAppCaptcha(c, req.AppID, req.CaptchaID, req.CaptchaAnswer, captchadomain.PurposeRegister) {
		return
	}
	// 前置校验：登录设备检查 — 注册后进入登录态，同样适用
	if !h.enforceDevicePolicy(c, req.AppID, req.DeviceID, req.Device, req.MarkCode, captchadomain.PurposeRegister) {
		return
	}
	deviceID, clientDevice := resolveClientDevice(c, req.DeviceID, req.Device, req.MarkCode)
	device := h.enrichDeviceFromDict(c, deviceID, clientDevice)
	if device == "" {
		device = guessDeviceFromUA(c.Request.UserAgent())
	}
	result, err := h.auth.RegisterWithPassword(c.Request.Context(), service.PasswordRegisterInput{
		AppID:     req.AppID,
		Account:   req.Account,
		Password:  req.Password,
		Nickname:  req.Nickname,
		DeviceID:  deviceID,
		Device:    device,
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, authResultMessage(result, "注册成功"), result)
}

func (h *Handler) OAuthAuthURL(c *gin.Context) {
	var req OAuthAuthURLRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	// Web OAuth 启动阶段仅能携带 deviceId（device 名称由 302 链路无法传递，最终签发时再严格校验）
	// 因此这里只需保证策略开启时 deviceId 来自客户端显式字段
	if !h.enforceDevicePolicyIDOnly(c, req.AppID, req.DeviceID, req.MarkCode) {
		return
	}
	deviceID, _ := resolveDeviceInfo(c, req.DeviceID, req.Device, req.MarkCode)
	url, err := h.auth.BuildOAuthAuthURL(c.Request.Context(), req.Provider, req.AppID, deviceID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "获取授权地址成功", gin.H{"url": url})
}

// OAuthCallback 同一回调地址承载「登录/注册」与「已登录用户绑定」两条链路，
// 由授权发起时写入 state 的 purpose 区分。登录场景的响应结构与历史版本保持一致。
func (h *Handler) OAuthCallback(c *gin.Context) {
	provider := c.Query("provider")
	code := c.Query("code")
	state := c.Query("state")
	outcome, err := h.auth.HandleOAuthCallback(c.Request.Context(), provider, code, state, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		h.writeError(c, err)
		return
	}
	if outcome.Binding != nil {
		response.Success(c, 200, "第三方账号绑定成功", outcome.Binding)
		return
	}
	response.Success(c, 200, authResultMessage(outcome.Login, "OAuth2 登录成功"), outcome.Login)
}

func (h *Handler) OAuthMobileLogin(c *gin.Context) {
	var req OAuthMobileLoginRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	h.oauthMobileLoginWith(c, req)
}

// oauthMobileLoginWith 承载移动端 OAuth 换取会话的实际流程。
// 旧 /api/auth/* 从 body 取 appid，v1 网关从路径取 appKey，两条入口共用这一段。
func (h *Handler) oauthMobileLoginWith(c *gin.Context, req OAuthMobileLoginRequest) {
	profile := authdomain.ProviderProfile{
		Provider:       req.Provider,
		ProviderUserID: req.ProviderUserID,
		UnionID:        req.UnionID,
		Nickname:       req.Nickname,
		Avatar:         req.Avatar,
		Email:          req.Email,
		RawProfile:     req.RawProfile,
		Tokens: map[string]string{
			"access_token":  req.AccessToken,
			"refresh_token": req.RefreshToken,
		},
	}
	if !h.enforceDevicePolicy(c, req.AppID, req.DeviceID, req.Device, req.MarkCode, captchadomain.PurposeLogin) {
		return
	}
	deviceID, clientDevice := resolveClientDevice(c, req.DeviceID, req.Device, req.MarkCode)
	device := h.enrichDeviceFromDict(c, deviceID, clientDevice)
	if device == "" {
		device = guessDeviceFromUA(c.Request.UserAgent())
	}
	result, err := h.auth.MobileOAuthLogin(c.Request.Context(), req.AppID, req.Provider, profile, deviceID, device, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, authResultMessage(result, "OAuth2 登录成功"), result)
}

func (h *Handler) Refresh(c *gin.Context) {
	var req RefreshRequest
	_ = bind(c, &req)
	token := req.RefreshToken
	if token == "" {
		token = req.Token
	}
	if token == "" {
		token = middlewareBearer(c.GetHeader("Authorization"))
	}
	deviceID, _ := resolveDeviceInfo(c, req.DeviceID, req.Device, req.MarkCode)
	result, err := h.auth.Refresh(c.Request.Context(), token, deviceID, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "刷新成功", result)
}

func (h *Handler) Logout(c *gin.Context) {
	tokenValue, _ := c.Get("auth.token")
	token, _ := tokenValue.(string)
	if err := h.auth.Logout(c.Request.Context(), token); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "退出成功", nil)
}

func (h *Handler) VerifyPassword(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req VerifyPasswordRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.auth.VerifyCurrentPassword(c.Request.Context(), session, req.Password); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "验证成功", gin.H{"valid": true})
}

func (h *Handler) ChangePassword(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	var req ChangePasswordRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if err := h.auth.ChangePassword(c.Request.Context(), session, req.CurrentPassword, req.NewPassword); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, 200, "密码修改成功", gin.H{"changed": true})
}

func (h *Handler) My(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	view, err := h.user.GetMy(c.Request.Context(), session)
	if err != nil {
		h.writeError(c, err)
		return
	}
	h.attachMyAvatar(c, session, view)
	response.Success(c, 200, "获取成功", view)
}
