package httptransport

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"

	appdomain "aegis/internal/domain/app"
	authdomain "aegis/internal/domain/auth"
	authprotocol "aegis/internal/domain/authprotocol"
	captchadomain "aegis/internal/domain/captcha"
	oauthdomain "aegis/internal/domain/oauth"
	"aegis/internal/service"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// 应用接入网关 —— /api/v1/apps/{appKey}/*
//
// 这一组路由是接入方**唯一**需要认识的入口：路径、请求体与响应体在
// standard / signed / sealed 三档安全等级下完全一致，等级只由
// middleware.AppGateway 决定请求"怎么包装"。因此客户端换档时只替换一层
// transport 适配器，业务代码一行不动。

// AppConfig 返回应用的认证能力与当前安全等级规格。
// 该接口在任何等级下都免包装可读——否则客户端无从知道该怎么包装后续请求。
func (h *Handler) AppConfig(c *gin.Context) {
	app, policy, ok := h.resolveGatewayApp(c)
	if !ok {
		return
	}
	// 第三方登录渠道并入 config，登录页不必再单独发一次请求。
	var providers []oauthdomain.PublicProvider
	if h.appOAuth != nil {
		if items, err := h.appOAuth.PublicProviders(c.Request.Context(), app.ID); err == nil {
			providers = items
		}
	}
	config, err := h.authProtocol.BuildConfig(
		c.Request.Context(), app, policy, providers,
		h.resolveCaptchaRequirement(c, app.ID, policy),
	)
	if err != nil {
		h.writeError(c, err)
		return
	}
	// sealed 档的公钥有轮换窗口，缓存时间必须短于轮换后的 24h 兼容期。
	c.Header("Cache-Control", "public, max-age=60, stale-while-revalidate=300")
	response.Success(c, http.StatusOK, "获取成功", config)
}

// AppCaptcha 按应用策略签发登录/注册验证码。
func (h *Handler) AppCaptcha(c *gin.Context) {
	app, _, ok := h.resolveGatewayApp(c)
	if !ok {
		return
	}
	if h.captcha == nil {
		response.Error(c, http.StatusServiceUnavailable, 50371, "验证码服务不可用")
		return
	}
	var req struct {
		Purpose string `json:"purpose"`
	}
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	purpose := captchadomain.Purpose(strings.ToLower(strings.TrimSpace(req.Purpose)))
	if purpose == "" {
		purpose = captchadomain.PurposeLogin
	}
	if purpose != captchadomain.PurposeLogin && purpose != captchadomain.PurposeRegister {
		response.Error(c, http.StatusBadRequest, 40017, "验证码用途只能是 login 或 register")
		return
	}
	captchaType, err := h.resolveUserCaptchaType(c, app.ID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	if captchaType == "" {
		response.Error(c, http.StatusForbidden, 40310, "当前应用未启用图形验证码")
		return
	}
	result, err := h.captcha.Generate(c.Request.Context(), captchaType, captchadomain.GenerateRequest{
		Type: captchaType, Purpose: purpose, Scope: captchadomain.ScopeUser, AppID: app.ID,
	})
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "验证码生成成功", result)
}

// AppLogin 单一登录入口：method 决定用哪组字段，新增登录方式不再新增路由。
func (h *Handler) AppLogin(c *gin.Context) {
	app, policy, ok := h.resolveGatewayApp(c)
	if !ok {
		return
	}
	var req authprotocol.LoginInput
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	method := defaultAuthMethod(req.Method)
	if !h.requireAuthMethod(c, policy.LoginMethods, method) {
		return
	}
	if !h.verifyGatewayCaptcha(c, policy, app.ID, req.CaptchaID, req.CaptchaAnswer, captchadomain.PurposeLogin) {
		return
	}
	if !h.enforceDevicePolicy(c, app.ID, req.DeviceID, req.Device, "", captchadomain.PurposeLogin) {
		return
	}
	deviceID, device := h.resolveGatewayDevice(c, req.DeviceID, req.Device)

	var result *authdomain.LoginResult
	var err error
	switch method {
	case authprotocol.MethodPassword:
		if strings.TrimSpace(req.Account) == "" || req.Password == "" {
			response.Error(c, http.StatusBadRequest, 40000, "account 与 password 不能为空")
			return
		}
		result, err = h.auth.PasswordLogin(
			c.Request.Context(), app.ID, req.Account, req.Password,
			deviceID, device, c.ClientIP(), c.Request.UserAgent(),
		)
	case authprotocol.MethodSMS:
		// 短信码在这里校验后即作废，服务层只负责"这个手机号是谁"
		if !h.verifySMSCode(c, app.ID, req.Phone, req.Code, captchadomain.PurposeLogin) {
			return
		}
		result, err = h.auth.SMSLogin(c.Request.Context(), service.SMSLoginInput{
			AppID: app.ID, Phone: req.Phone,
			// 手机号未注册时能否自动建号，取决于应用是否启用了短信注册
			AllowRegister: containsProtocolValue(policy.RegisterMethods, authprotocol.MethodSMS),
			DeviceID:      deviceID, Device: device,
			IP: c.ClientIP(), UserAgent: c.Request.UserAgent(),
		})
	case authprotocol.MethodOAuth:
		response.Error(c, http.StatusBadRequest, 40087,
			"第三方登录请走 /auth/oauth/url 与 /auth/oauth/exchange")
		return
	default:
		response.Error(c, http.StatusBadRequest, 40000, "不支持的登录方式："+method)
		return
	}
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, authResultMessage(result, "登录成功"), result)
}

// AppRegister 单一注册入口，注册字段受 registrationSchema 约束。
func (h *Handler) AppRegister(c *gin.Context) {
	app, policy, ok := h.resolveGatewayApp(c)
	if !ok {
		return
	}
	var req authprotocol.RegisterInput
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	method := defaultAuthMethod(req.Method)
	if !h.requireAuthMethod(c, policy.RegisterMethods, method) {
		return
	}
	if !h.verifyGatewayCaptcha(c, policy, app.ID, req.CaptchaID, req.CaptchaAnswer, captchadomain.PurposeRegister) {
		return
	}
	if !h.enforceDevicePolicy(c, app.ID, req.DeviceID, req.Device, "", captchadomain.PurposeRegister) {
		return
	}
	deviceID, device := h.resolveGatewayDevice(c, req.DeviceID, req.Device)

	var result *authdomain.LoginResult
	var err error
	switch method {
	case authprotocol.MethodPassword:
		profile, validateErr := validateRegistrationInput(policy, req)
		if validateErr != nil {
			response.Error(c, http.StatusBadRequest, 40085, validateErr.Error())
			return
		}
		result, err = h.auth.RegisterWithPassword(c.Request.Context(), service.PasswordRegisterInput{
			AppID: app.ID, Account: req.Account, Password: req.Password, Nickname: req.Nickname,
			Profile: profile, SuppressSession: !policy.AutoLogin,
			DeviceID: deviceID, Device: device, IP: c.ClientIP(), UserAgent: c.Request.UserAgent(),
		})
	case authprotocol.MethodSMS:
		// 短信注册以手机号为账号，registrationSchema 里的 account/password 不参与校验
		profile, validateErr := validateSMSRegistrationInput(policy, req)
		if validateErr != nil {
			response.Error(c, http.StatusBadRequest, 40085, validateErr.Error())
			return
		}
		if !h.verifySMSCode(c, app.ID, req.Phone, req.Code, captchadomain.PurposeRegister) {
			return
		}
		result, err = h.auth.SMSLogin(c.Request.Context(), service.SMSLoginInput{
			AppID: app.ID, Phone: req.Phone, Nickname: req.Nickname, Profile: profile,
			AllowRegister: true, SuppressSession: !policy.AutoLogin,
			DeviceID: deviceID, Device: device,
			IP: c.ClientIP(), UserAgent: c.Request.UserAgent(),
		})
	default:
		response.Error(c, http.StatusBadRequest, 40000, "不支持的注册方式："+method)
		return
	}
	if err != nil {
		h.writeError(c, err)
		return
	}
	message := authResultMessage(result, "注册成功")
	if !policy.AutoLogin {
		message = "注册成功，请登录"
	}
	response.Success(c, http.StatusOK, message, result)
}

// AppSMSCode 申请短信验证码。
//
// purpose 决定这串码只能用于登录还是注册，与后续 /auth/login、/auth/register
// 的校验用途一一对应 —— 拿注册码去登录会被拒，防止用途混用绕过策略。
func (h *Handler) AppSMSCode(c *gin.Context) {
	app, policy, ok := h.resolveGatewayApp(c)
	if !ok {
		return
	}
	if h.captcha == nil || h.app == nil {
		response.Error(c, http.StatusServiceUnavailable, 50371, "验证码服务不可用")
		return
	}
	var req authprotocol.SMSCodeInput
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if strings.TrimSpace(req.Phone) == "" {
		response.Error(c, http.StatusBadRequest, 40000, "phone 不能为空")
		return
	}
	purpose := captchadomain.Purpose(strings.ToLower(strings.TrimSpace(req.Purpose)))
	if purpose == "" {
		purpose = captchadomain.PurposeLogin
	}
	switch purpose {
	case captchadomain.PurposeLogin:
		if !h.requireAuthMethod(c, policy.LoginMethods, authprotocol.MethodSMS) {
			return
		}
	case captchadomain.PurposeRegister:
		if !h.requireAuthMethod(c, policy.RegisterMethods, authprotocol.MethodSMS) {
			return
		}
	default:
		response.Error(c, http.StatusBadRequest, 40017, "验证码用途只能是 login 或 register")
		return
	}

	appCfg, err := h.app.GetCaptchaConfig(c.Request.Context(), app.ID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	providerCfg, err := service.BuildSMSProviderConfig(app.ID, purpose, appCfg.SMS)
	if err != nil {
		h.writeError(c, err)
		return
	}
	result, err := h.captcha.SendSMSCode(c.Request.Context(), captchadomain.SMSSendRequest{
		AppID: app.ID, Phone: req.Phone, Purpose: purpose, ClientIP: c.ClientIP(),
		CaptchaID: req.CaptchaID, CaptchaAnswer: req.CaptchaAnswer,
	}, providerCfg)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "短信验证码已发送", result)
}

// AppRefresh 刷新令牌。令牌可放 body.refreshToken，也可放 Authorization 头。
func (h *Handler) AppRefresh(c *gin.Context) {
	if _, _, ok := h.resolveGatewayApp(c); !ok {
		return
	}
	var req struct {
		RefreshToken string `json:"refreshToken"`
		Token        string `json:"token"`
		DeviceID     string `json:"deviceId"`
		Device       string `json:"device"`
	}
	_ = bind(c, &req)
	token := gatewayFirstNonEmpty(req.RefreshToken, req.Token, middlewareBearer(c.GetHeader("Authorization")))
	if token == "" {
		response.Error(c, http.StatusBadRequest, 40000, "缺少 refreshToken")
		return
	}
	deviceID, _ := resolveClientDevice(c, req.DeviceID, req.Device, "")
	result, err := h.auth.Refresh(c.Request.Context(), token, deviceID, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "刷新成功", result)
}

// AppSecondFactor 完成登录返回的二次认证挑战。
func (h *Handler) AppSecondFactor(c *gin.Context) {
	if _, _, ok := h.resolveGatewayApp(c); !ok {
		return
	}
	h.VerifySecondFactor(c)
}

// AppOAuthURL 取第三方登录授权地址（Web / 系统浏览器跳转流程的第一步）。
func (h *Handler) AppOAuthURL(c *gin.Context) {
	app, policy, ok := h.resolveGatewayApp(c)
	if !ok {
		return
	}
	if !h.requireAuthMethod(c, policy.LoginMethods, authprotocol.MethodOAuth) {
		return
	}
	var req struct {
		Provider string `json:"provider"`
		DeviceID string `json:"deviceId"`
		Device   string `json:"device"`
	}
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if strings.TrimSpace(req.Provider) == "" {
		response.Error(c, http.StatusBadRequest, 40000, "缺少 provider")
		return
	}
	if !h.enforceDevicePolicyIDOnly(c, app.ID, req.DeviceID, "") {
		return
	}
	deviceID, _ := resolveDeviceInfo(c, req.DeviceID, req.Device, "")
	url, err := h.auth.BuildOAuthAuthURL(c.Request.Context(), req.Provider, app.ID, deviceID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取授权地址成功", gin.H{"url": url})
}

// AppOAuthCallback 第三方授权后的回跳落点。
//
// 这是整条链路里唯一**不经过安全等级包装**的接口（middleware.AppGateway 已放行）：
// 请求由第三方平台重定向浏览器发起，客户端没有任何机会给它签名或加密。
// 因此 sealed 档下它也是明文的 —— 需要全链路加密的应用应改用原生 SDK 取到
// profile 后走 /auth/oauth/exchange，那条路径受完整包装保护。
//
// state 里已经绑定了 appID 与 purpose，越权与 CSRF 由 AuthService 校验。
func (h *Handler) AppOAuthCallback(c *gin.Context) {
	if _, policy, ok := h.resolveGatewayApp(c); !ok {
		return
	} else if !containsProtocolValue(policy.LoginMethods, authprotocol.MethodOAuth) {
		response.Error(c, http.StatusForbidden, 40370, "当前应用未启用 oauth 认证方式")
		return
	}
	h.OAuthCallback(c)
}

// AppOAuthExchange 原生 SDK 拿到第三方 profile 后换取 Aegis 会话。
func (h *Handler) AppOAuthExchange(c *gin.Context) {
	app, policy, ok := h.resolveGatewayApp(c)
	if !ok {
		return
	}
	if !h.requireAuthMethod(c, policy.LoginMethods, authprotocol.MethodOAuth) {
		return
	}
	var req OAuthMobileLoginRequest
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	if !h.enforceDevicePolicy(c, app.ID, req.DeviceID, req.Device, req.MarkCode, captchadomain.PurposeLogin) {
		return
	}
	deviceID, device := h.resolveGatewayDevice(c, req.DeviceID, req.Device)
	// 是否允许自动建号 = 渠道级 allowRegister ∧ 应用已启用第三方登录。
	// 注意注册方式里没有 oauth：那会变成两处配同一件事，见 authprotocol.RegisterMethods。
	result, err := h.auth.MobileOAuthLoginScoped(
		c.Request.Context(), app.ID, req.Provider,
		authdomain.ProviderProfile{
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
		},
		deviceID, device, c.ClientIP(), c.Request.UserAgent(), true,
	)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, authResultMessage(result, "第三方登录成功"), result)
}

// AppLogout 注销当前会话。
func (h *Handler) AppLogout(c *gin.Context) {
	if _, _, ok := h.resolveGatewayApp(c); !ok {
		return
	}
	h.Logout(c)
}

// AppMe 返回当前登录用户资料。
func (h *Handler) AppMe(c *gin.Context) {
	if _, _, ok := h.resolveGatewayApp(c); !ok {
		return
	}
	h.My(c)
}

// ─────────────────────────────────────────────────────────────────────
// 共用辅助
// ─────────────────────────────────────────────────────────────────────

// resolveGatewayApp 解析路径上的 appKey，并顺带拿到策略。
// 中间件已经按等级放行过一次，这里只负责取值与停用判定。
func (h *Handler) resolveGatewayApp(c *gin.Context) (*appdomain.App, *authprotocol.Policy, bool) {
	if h.authProtocol == nil || h.auth == nil {
		response.Error(c, http.StatusServiceUnavailable, 50370, "认证协议服务不可用")
		return nil, nil, false
	}
	app, policy, err := h.authProtocol.ResolveAppAndPolicy(c.Request.Context(), c.Param("appkey"))
	if err != nil {
		h.writeError(c, err)
		return nil, nil, false
	}
	return app, policy, true
}

// resolveCaptchaRequirement 汇总三处开关，算出 /config 要下发的验证码结论。
//
// 客户端拿到的是「调这个接口要不要先取验证码」的答案，不需要知道背后有
// 策略开关、应用验证码配置、平台短信配置三个来源，更不该把这套判断复制一份。
//
// 读配置失败按"未开启"处理，与 verifyAppCaptcha 的降级取向一致 ——
// 一次数据库抖动不该把所有人挡在登录之外。这种情况下真实判定同样会放行，
// 因此下发的结论与执行点仍然一致。
func (h *Handler) resolveCaptchaRequirement(
	c *gin.Context,
	appID int64,
	policy *authprotocol.Policy,
) authprotocol.CaptchaRequirement {
	var appCfg *captchadomain.CaptchaAppConfig
	if h.app != nil {
		appCfg, _ = h.app.GetCaptchaConfig(c.Request.Context(), appID)
	}
	return service.ResolveCaptchaRequirement(policy, appCfg, h.captcha.RequiresPreCaptchaForSMS())
}

func (h *Handler) requireAuthMethod(c *gin.Context, allowed []string, method string) bool {
	if containsProtocolValue(allowed, method) {
		return true
	}
	response.Error(c, http.StatusForbidden, 40370,
		fmt.Sprintf("当前应用未启用 %s 认证方式", method))
	return false
}

func (h *Handler) resolveGatewayDevice(c *gin.Context, bodyDeviceID, bodyDevice string) (string, string) {
	deviceID, clientDevice := resolveClientDevice(c, bodyDeviceID, bodyDevice, "")
	device := h.enrichDeviceFromDict(c, deviceID, clientDevice)
	if device == "" {
		device = guessDeviceFromUA(c.Request.UserAgent())
	}
	return deviceID, device
}

// verifyGatewayCaptcha 网关侧的验证码闸门。
//
// 判定用的是 /config 下发给客户端的**同一个函数**：客户端按 config.auth.captcha
// 决定要不要先取验证码，这里按同一份结论决定要不要校验。
//
// 两处各写一遍判断是这个 bug 的来源 —— 原先客户端只看得到 policy.RequireCaptcha，
// 而服务端实际上还叠了应用验证码配置的分场景开关，于是「策略没开、应用配置要求
// 登录验证码」这种普通组合会让登录直接被拒，登录页上却没有任何提示。
func (h *Handler) verifyGatewayCaptcha(c *gin.Context, policy *authprotocol.Policy, appID int64, captchaID, answer string, purpose captchadomain.Purpose) bool {
	required := h.resolveCaptchaRequirement(c, appID, policy)
	if !required.Required(captchaEntryForPurpose(purpose)) {
		return true
	}
	return h.enforceCaptcha(c, appID, captchaID, answer, purpose)
}

// captchaEntryForPurpose 把验证码用途映射到 /config 下发的入口名。
// 未来新增用途时这里与 CaptchaRequirement 一起改，客户端才看得到那个入口。
func captchaEntryForPurpose(purpose captchadomain.Purpose) string {
	switch purpose {
	case captchadomain.PurposeRegister:
		return authprotocol.CaptchaEntryRegister
	default:
		return authprotocol.CaptchaEntryLogin
	}
}

// enforceLegacyAuthProtocol 旧 /api/auth/* 命名空间的闸门。
// 新协议等级与它正交：应用可以一边跑 sealed 的 v1，一边保留明文旧接口过渡。
func (h *Handler) enforceLegacyAuthProtocol(c *gin.Context, appID int64, method string, register bool) bool {
	// 未启用协议服务的离线工具/兼容测试保持原行为。
	if h == nil || h.authProtocol == nil {
		return true
	}
	policy, err := h.authProtocol.GetPolicy(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return false
	}
	if !policy.AllowLegacy {
		response.Error(c, http.StatusUpgradeRequired, 42670,
			"该应用已关闭旧认证接口，请改用 /api/v1/apps/{appKey}/auth/*")
		return false
	}
	methods := policy.LoginMethods
	if register {
		methods = policy.RegisterMethods
	}
	if !containsProtocolValue(methods, method) {
		response.Error(c, http.StatusForbidden, 40370, "当前应用未启用该认证方式")
		return false
	}
	return true
}

func defaultAuthMethod(method string) string {
	method = strings.ToLower(strings.TrimSpace(method))
	if method == "" {
		return "password"
	}
	return method
}

func gatewayFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// verifySMSCode 校验短信验证码；校验通过即作废，同一串码不能复用。
func (h *Handler) verifySMSCode(c *gin.Context, appID int64, phone, code string, purpose captchadomain.Purpose) bool {
	if h.captcha == nil {
		response.Error(c, http.StatusServiceUnavailable, 50371, "验证码服务不可用")
		return false
	}
	phone, code = strings.TrimSpace(phone), strings.TrimSpace(code)
	if phone == "" || code == "" {
		response.Error(c, http.StatusBadRequest, 40000, "phone 与 code 不能为空")
		return false
	}
	valid, err := h.captcha.VerifySMSCode(c.Request.Context(), captchadomain.SMSVerifyRequest{
		AppID: appID, Phone: phone, Code: code, Purpose: purpose,
	})
	if err != nil {
		h.writeError(c, err)
		return false
	}
	if !valid {
		response.Error(c, http.StatusBadRequest, errCodeCaptchaInvalid, "短信验证码错误或已过期")
		return false
	}
	return true
}

// validateSMSRegistrationInput 短信注册的字段校验。
//
// 与密码注册的差别只有一处：账号与密码由手机号取代，因此 schema 里的
// account / password 两项跳过必填判定，其余自定义字段规则完全一致。
func validateSMSRegistrationInput(policy *authprotocol.Policy, req authprotocol.RegisterInput) (map[string]any, error) {
	relaxed := *policy
	relaxed.RegistrationSchema = make([]authprotocol.RegistrationField, 0, len(policy.RegistrationSchema))
	for _, field := range policy.RegistrationSchema {
		if field.Name == "account" || field.Name == "password" {
			continue
		}
		relaxed.RegistrationSchema = append(relaxed.RegistrationSchema, field)
	}
	return validateRegistrationInput(&relaxed, req)
}

func validateRegistrationInput(policy *authprotocol.Policy, req authprotocol.RegisterInput) (map[string]any, error) {
	profile := map[string]any{}
	if len(req.Profile) > 0 && string(req.Profile) != "null" {
		if err := json.Unmarshal(req.Profile, &profile); err != nil {
			return nil, fmt.Errorf("profile 必须是 JSON 对象")
		}
	}
	allowed := map[string]authprotocol.RegistrationField{}
	for _, field := range policy.RegistrationSchema {
		allowed[field.Name] = field
		var value any
		switch field.Name {
		case "account":
			value = req.Account
		case "password":
			value = req.Password
		case "nickname":
			value = req.Nickname
		default:
			value = profile[field.Name]
		}
		if field.Required && isEmptyRegistrationValue(value) {
			return nil, fmt.Errorf("缺少必填注册字段 %s", field.Name)
		}
		if !isEmptyRegistrationValue(value) && !registrationValueMatches(field.Type, value) {
			return nil, fmt.Errorf("注册字段 %s 类型无效", field.Name)
		}
	}
	for key := range profile {
		field, exists := allowed[key]
		if !exists || key == "account" || key == "password" || key == "nickname" ||
			strings.HasPrefix(strings.ToLower(key), "register_") {
			return nil, fmt.Errorf("不允许的注册字段 %s", key)
		}
		if !field.Mutable {
			return nil, fmt.Errorf("注册字段 %s 不可由客户端写入", key)
		}
	}
	return profile, nil
}

func registrationValueMatches(kind string, value any) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", "text", "password", "email", "phone":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "number":
		number, ok := value.(float64)
		return ok && !math.IsNaN(number) && !math.IsInf(number, 0)
	default:
		return false
	}
}

func isEmptyRegistrationValue(value any) bool {
	if value == nil {
		return true
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text) == ""
	}
	return false
}

func containsProtocolValue(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
