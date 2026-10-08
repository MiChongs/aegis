package httptransport

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"html/template"
	"net/http"
	"strings"
	"time"

	authprotocol "aegis/internal/domain/authprotocol"
	captchadomain "aegis/internal/domain/captcha"
	"aegis/internal/service"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
)

// OAuth2 授权服务器（Ory Hydra）的 login / consent / logout provider 页面与令牌钩子。
// 业务判定在 service.OAuth2ServerService，这里只管表单、Cookie、CSRF 与渲染。
// 协议流程见 docs/oauth2-provider.md。

const (
	oauth2CSRFCookie   = "aegis_oauth2_csrf"
	oauth2DeviceCookie = "aegis_oauth2_did"
	oauth2CookiePath   = "/oauth2"
)

// ── CSRF：双提交 Cookie + 与 challenge 绑定的 HMAC ─────────────────────
//
// 表单里的令牌 = HMAC(密钥, Cookie 值 | challenge)。攻击者拿不到受害者的 Cookie 值，
// 也就算不出令牌；令牌又绑定 challenge，抓到一张表单也挪不到别的授权请求上。
// 防的是「登录 CSRF」：诱导受害者在攻击者发起的授权请求里登录或点允许。

func (h *Handler) oauth2CSRFToken(c *gin.Context, challenge string) string {
	seed, err := c.Cookie(oauth2CSRFCookie)
	if err != nil || len(seed) < 32 {
		seed = randomCookieValue()
		h.setOAuth2Cookie(c, oauth2CSRFCookie, seed, 0)
	}
	return oauth2CSRFSign(h.oauth2.Config().CSRFKey, seed, challenge)
}

func (h *Handler) oauth2CSRFValid(c *gin.Context, challenge string) bool {
	seed, err := c.Cookie(oauth2CSRFCookie)
	if err != nil || seed == "" {
		return false
	}
	expected := oauth2CSRFSign(h.oauth2.Config().CSRFKey, seed, challenge)
	return subtle.ConstantTimeCompare([]byte(expected), []byte(c.PostForm("csrf"))) == 1
}

func oauth2CSRFSign(key, seed, challenge string) string {
	mac := hmac.New(sha256.New, []byte("aegis/oauth2/csrf\x00"+key))
	mac.Write([]byte(seed))
	mac.Write([]byte{'|'})
	mac.Write([]byte(challenge))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func randomCookieValue() string {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}

// setOAuth2Cookie 页面 Cookie：HttpOnly + SameSite=Lax。Lax 而不是 Strict：
// 用户是从接入方站点跳过来的，Strict 会让第一次 GET 就丢 Cookie。maxAge=0 为会话 Cookie。
func (h *Handler) setOAuth2Cookie(c *gin.Context, name, value string, maxAge int) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     oauth2CookiePath,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   h.oauth2.Config().CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

// oauth2Device 浏览器的设备标识：应用开了「登录设备检查」时登录关卡要求 deviceId，
// 网页没有原生设备 ID，用一枚长期 Cookie 充当，同一浏览器多次登录保持不变。
func (h *Handler) oauth2Device(c *gin.Context) service.OAuth2DeviceInfo {
	deviceID, err := c.Cookie(oauth2DeviceCookie)
	if err != nil || len(deviceID) < 16 || len(deviceID) > 64 {
		deviceID = "web-" + randomCookieValue()[:32]
		h.setOAuth2Cookie(c, oauth2DeviceCookie, deviceID, 400*24*3600)
	}
	device := guessDeviceFromUA(c.Request.UserAgent())
	if device == "" {
		device = "Web"
	}
	return service.OAuth2DeviceInfo{DeviceID: deviceID, Device: device, IP: c.ClientIP(), UserAgent: c.Request.UserAgent()}
}

// ── 公共 ────────────────────────────────────────────────────────────

func (h *Handler) oauth2Available(c *gin.Context) bool {
	if h.oauth2 == nil || !h.oauth2.Enabled() {
		renderOAuth2Page(c, http.StatusServiceUnavailable, "message", oauth2PageView{
			Title: "授权服务未启用", Error: "此服务尚未开启 OAuth2 授权登录，请联系应用开发者。",
		})
		return false
	}
	return true
}

// oauth2Fail 把错误渲染成页面。只展示业务错误的文案，内部错误不外泄。
func oauth2Fail(c *gin.Context, base oauth2PageView, err error) {
	status := http.StatusBadRequest
	message := "请求处理失败，请稍后重试"
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) {
		status = appErr.HTTPStatus
		message = appErr.Message
	} else {
		status = http.StatusInternalServerError
	}
	if base.Title == "" {
		base.Title = "无法继续"
	}
	base.Error = message
	renderOAuth2Page(c, status, "message", base)
}

func oauth2Redirect(c *gin.Context, target string) {
	// 303：表单 POST 之后一律以 GET 跟随，避免浏览器把表单重新提交给 Hydra
	c.Header("Cache-Control", "no-store")
	c.Redirect(http.StatusSeeOther, target)
}

func loginBaseView(lc *service.OAuth2LoginContext) oauth2PageView {
	view := oauth2PageView{Title: "登录", Challenge: lc.Challenge}
	if lc.App != nil {
		view.AppName = lc.App.Name
	}
	view.Client = firstNonEmptyString(lc.Client.ClientName, lc.Client.ClientID)
	view.ClientURI = safeHTTPURL(lc.Client.ClientURI)
	view.LogoURI = safeHTTPURL(lc.Client.LogoURI)
	return view
}

// safeHTTPURL 只放行 http(s) 地址进 href / src；其余（javascript: 之类）丢弃。
// html/template 已会把危险协议改写成 #ZgotmplZ，这里再挡一层是为了不渲染出无意义的链接。
func safeHTTPURL(raw string) string {
	raw = strings.TrimSpace(raw)
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://") {
		return raw
	}
	return ""
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// ── 登录 ────────────────────────────────────────────────────────────

// OAuth2LoginPage GET /oauth2/login?login_challenge=
func (h *Handler) OAuth2LoginPage(c *gin.Context) {
	if !h.oauth2Available(c) {
		return
	}
	lc, redirect, err := h.oauth2.BeginLogin(c.Request.Context(), c.Query("login_challenge"))
	if err != nil {
		oauth2Fail(c, oauth2PageView{Title: "无法登录"}, err)
		return
	}
	if redirect != "" {
		oauth2Redirect(c, redirect)
		return
	}
	view := loginBaseView(lc)
	view.Account = lc.LoginHint
	view.Remember = true
	h.renderLoginForm(c, http.StatusOK, lc, view)
}

// renderLoginForm 渲染登录表单；应用要求登录验证码时同时签发一张。
func (h *Handler) renderLoginForm(c *gin.Context, status int, lc *service.OAuth2LoginContext, view oauth2PageView) {
	policy, err := h.oauth2LoginPolicy(c, lc.App.ID)
	if err != nil {
		oauth2Fail(c, view, err)
		return
	}
	if policy != nil && !containsProtocolValue(policy.LoginMethods, authprotocol.MethodPassword) {
		view.Error = "该应用未开放账号密码登录，无法通过网页授权登录。"
		renderOAuth2Page(c, http.StatusForbidden, "message", view)
		return
	}
	if h.oauth2CaptchaRequired(c, lc.App.ID, policy) {
		if err := h.issueOAuth2Captcha(c, lc.App.ID, &view); err != nil {
			oauth2Fail(c, view, err)
			return
		}
	}
	view.CSRF = h.oauth2CSRFToken(c, lc.Challenge)
	renderOAuth2Page(c, status, "login", view)
}

func (h *Handler) oauth2LoginPolicy(c *gin.Context, appID int64) (*authprotocol.Policy, error) {
	if h.authProtocol == nil {
		return nil, nil
	}
	return h.authProtocol.GetPolicy(c.Request.Context(), appID)
}

func (h *Handler) oauth2CaptchaRequired(c *gin.Context, appID int64, policy *authprotocol.Policy) bool {
	if h.captcha == nil || policy == nil {
		return false
	}
	return h.resolveCaptchaRequirement(c, appID, policy).Required(authprotocol.CaptchaEntryLogin)
}

// issueOAuth2Captcha 签发登录验证码。页面不跑脚本，点选类（手性碳）与音频类在这里画不出来，
// 一律改发图形验证码：校验只认 appId / 用途 / 作用域，不认类型，安全强度不因此降低到无。
func (h *Handler) issueOAuth2Captcha(c *gin.Context, appID int64, view *oauth2PageView) error {
	captchaType, appCfg, err := h.resolveUserCaptchaType(c, appID)
	if err != nil {
		return err
	}
	switch captchaType {
	case captchadomain.TypeImage, captchadomain.TypeMath, captchadomain.TypeDigit, captchadomain.TypeDynamic:
	default:
		captchaType = captchadomain.TypeImage
	}
	result, err := h.captcha.Generate(c.Request.Context(), captchaType, captchadomain.GenerateRequest{
		Type: captchaType, Purpose: captchadomain.PurposeLogin, Scope: captchadomain.ScopeUser, AppID: appID,
		Dynamic: dynamicConfigOf(appCfg),
	})
	if err != nil {
		return err
	}
	view.CaptchaID = result.CaptchaID
	view.CaptchaHint = result.Hint
	if result.ImageData != "" {
		data := result.ImageData
		if !strings.HasPrefix(data, "data:") {
			mime := firstNonEmptyString(result.MimeType, "image/png")
			data = "data:" + mime + ";base64," + data
		}
		view.CaptchaImage = template.URL(data)
	}
	return nil
}

func (h *Handler) verifyOAuth2Captcha(c *gin.Context, appID int64) error {
	captchaID := strings.TrimSpace(c.PostForm("captchaId"))
	if captchaID == "" {
		return apperrors.New(errCodeCaptchaRequired, http.StatusBadRequest, "请输入验证码")
	}
	valid, err := h.captcha.Verify(c.Request.Context(), captchadomain.VerifyRequest{
		CaptchaID:       captchaID,
		Answer:          strings.TrimSpace(c.PostForm("captchaAnswer")),
		Clear:           true,
		ExpectedAppID:   appID,
		ExpectedPurpose: captchadomain.PurposeLogin,
		ExpectedScope:   captchadomain.ScopeUser,
	})
	if err != nil {
		return err
	}
	if !valid {
		return apperrors.New(errCodeCaptchaInvalid, http.StatusBadRequest, "验证码错误")
	}
	return nil
}

// OAuth2LoginSubmit POST /oauth2/login
func (h *Handler) OAuth2LoginSubmit(c *gin.Context) {
	if !h.oauth2Available(c) {
		return
	}
	ctx := c.Request.Context()
	challenge := c.PostForm("challenge")
	if !h.oauth2CSRFValid(c, challenge) {
		oauth2Fail(c, oauth2PageView{Title: "页面已过期"}, apperrors.New(40387, http.StatusForbidden, "页面已过期，请回到应用重新发起登录"))
		return
	}
	if c.PostForm("action") == "cancel" {
		redirect, err := h.oauth2.CancelLogin(ctx, challenge)
		if err != nil {
			oauth2Fail(c, oauth2PageView{}, err)
			return
		}
		oauth2Redirect(c, redirect)
		return
	}
	lc, err := h.oauth2.LoadLogin(ctx, challenge)
	if err != nil {
		oauth2Fail(c, oauth2PageView{Title: "无法登录"}, err)
		return
	}
	view := loginBaseView(lc)
	view.Account = strings.TrimSpace(c.PostForm("account"))
	view.Remember = c.PostForm("remember") == "1"

	policy, err := h.oauth2LoginPolicy(c, lc.App.ID)
	if err != nil {
		oauth2Fail(c, view, err)
		return
	}
	if policy != nil && !containsProtocolValue(policy.LoginMethods, authprotocol.MethodPassword) {
		view.Error = "该应用未开放账号密码登录，无法通过网页授权登录。"
		renderOAuth2Page(c, http.StatusForbidden, "message", view)
		return
	}
	if h.oauth2CaptchaRequired(c, lc.App.ID, policy) {
		if err := h.verifyOAuth2Captcha(c, lc.App.ID); err != nil {
			view.Error = apperrMessage(err)
			h.renderLoginForm(c, http.StatusBadRequest, lc, view)
			return
		}
	}
	if view.Account == "" || c.PostForm("password") == "" {
		view.Error = "请输入账号和密码"
		h.renderLoginForm(c, http.StatusBadRequest, lc, view)
		return
	}
	step, err := h.oauth2.LoginWithPassword(ctx, lc, view.Account, c.PostForm("password"), view.Remember, h.oauth2Device(c))
	if err != nil {
		view.Error = apperrMessage(err)
		h.renderLoginForm(c, apperrStatus(err), lc, view)
		return
	}
	if step.MFAChallengeID != "" {
		view.MFAChallengeID = step.MFAChallengeID
		view.AllowRecovery = containsProtocolValue(step.MFAMethods, "recovery_code")
		view.Title = "两步验证"
		view.CSRF = h.oauth2CSRFToken(c, lc.Challenge)
		renderOAuth2Page(c, http.StatusOK, "mfa", view)
		return
	}
	oauth2Redirect(c, step.RedirectTo)
}

// OAuth2SecondFactorSubmit POST /oauth2/login/mfa
func (h *Handler) OAuth2SecondFactorSubmit(c *gin.Context) {
	if !h.oauth2Available(c) {
		return
	}
	ctx := c.Request.Context()
	challenge := c.PostForm("challenge")
	if !h.oauth2CSRFValid(c, challenge) {
		oauth2Fail(c, oauth2PageView{Title: "页面已过期"}, apperrors.New(40387, http.StatusForbidden, "页面已过期，请回到应用重新发起登录"))
		return
	}
	if c.PostForm("action") == "cancel" {
		redirect, err := h.oauth2.CancelLogin(ctx, challenge)
		if err != nil {
			oauth2Fail(c, oauth2PageView{}, err)
			return
		}
		oauth2Redirect(c, redirect)
		return
	}
	lc, err := h.oauth2.LoadLogin(ctx, challenge)
	if err != nil {
		oauth2Fail(c, oauth2PageView{Title: "无法登录"}, err)
		return
	}
	view := loginBaseView(lc)
	view.Title = "两步验证"
	view.Remember = c.PostForm("remember") == "1"
	view.MFAChallengeID = c.PostForm("mfa")
	code := strings.TrimSpace(c.PostForm("code"))
	recovery := strings.TrimSpace(c.PostForm("recoveryCode"))
	view.AllowRecovery = true
	if code == "" && recovery == "" {
		view.Error = "请输入验证码"
		view.CSRF = h.oauth2CSRFToken(c, lc.Challenge)
		renderOAuth2Page(c, http.StatusBadRequest, "mfa", view)
		return
	}
	step, err := h.oauth2.LoginWithSecondFactor(ctx, lc, view.MFAChallengeID, code, recovery, view.Remember)
	if err != nil {
		var appErr *apperrors.AppError
		// 挑战本身失效（过期、已用）时没法在本页重试，回到登录表单
		if errors.As(err, &appErr) && appErr.Code == 40056 {
			view.Title = "登录"
			view.Error = "验证已超时，请重新登录"
			h.renderLoginForm(c, http.StatusBadRequest, lc, view)
			return
		}
		view.Error = apperrMessage(err)
		view.CSRF = h.oauth2CSRFToken(c, lc.Challenge)
		renderOAuth2Page(c, apperrStatus(err), "mfa", view)
		return
	}
	oauth2Redirect(c, step.RedirectTo)
}

func apperrMessage(err error) string {
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) && appErr.Message != "" {
		return appErr.Message
	}
	return "登录失败，请稍后重试"
}

func apperrStatus(err error) int {
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) && appErr.HTTPStatus > 0 {
		return appErr.HTTPStatus
	}
	return http.StatusInternalServerError
}

// ── 同意 ────────────────────────────────────────────────────────────

// OAuth2ConsentPage GET /oauth2/consent?consent_challenge=
func (h *Handler) OAuth2ConsentPage(c *gin.Context) {
	if !h.oauth2Available(c) {
		return
	}
	cc, redirect, err := h.oauth2.BeginConsent(c.Request.Context(), c.Query("consent_challenge"))
	if err != nil {
		oauth2Fail(c, oauth2PageView{Title: "无法授权"}, err)
		return
	}
	if redirect != "" {
		oauth2Redirect(c, redirect)
		return
	}
	view := oauth2PageView{
		Title:       "授权请求",
		Challenge:   cc.Challenge,
		Client:      firstNonEmptyString(cc.Client.ClientName, cc.Client.ClientID),
		ClientURI:   safeHTTPURL(cc.Client.ClientURI),
		LogoURI:     safeHTTPURL(cc.Client.LogoURI),
		PolicyURI:   safeHTTPURL(cc.Client.PolicyURI),
		TosURI:      safeHTTPURL(cc.Client.TosURI),
		UserName:    cc.Nickname,
		UserAccount: cc.Account,
		UserAvatar:  safeHTTPURL(cc.Avatar),
	}
	if cc.App != nil {
		view.AppName = cc.App.Name
	}
	for _, scope := range cc.Scopes {
		view.Scopes = append(view.Scopes, oauth2ScopeItem{Scope: scope.Scope, Title: scope.Title, Description: scope.Description, Required: scope.Required})
	}
	view.CSRF = h.oauth2CSRFToken(c, cc.Challenge)
	renderOAuth2Page(c, http.StatusOK, "consent", view)
}

// OAuth2ConsentSubmit POST /oauth2/consent
func (h *Handler) OAuth2ConsentSubmit(c *gin.Context) {
	if !h.oauth2Available(c) {
		return
	}
	ctx := c.Request.Context()
	challenge := c.PostForm("challenge")
	if !h.oauth2CSRFValid(c, challenge) {
		oauth2Fail(c, oauth2PageView{Title: "页面已过期"}, apperrors.New(40387, http.StatusForbidden, "页面已过期，请回到应用重新发起授权"))
		return
	}
	var (
		redirect string
		err      error
	)
	if c.PostForm("action") == "allow" {
		redirect, err = h.oauth2.AcceptConsent(ctx, challenge, c.PostFormArray("scope"), c.PostForm("remember") == "1")
	} else {
		redirect, err = h.oauth2.RejectConsent(ctx, challenge)
	}
	if err != nil {
		oauth2Fail(c, oauth2PageView{Title: "无法授权"}, err)
		return
	}
	oauth2Redirect(c, redirect)
}

// ── 登出 ────────────────────────────────────────────────────────────

// OAuth2LogoutPage GET /oauth2/logout?logout_challenge=
func (h *Handler) OAuth2LogoutPage(c *gin.Context) {
	if !h.oauth2Available(c) {
		return
	}
	lc, redirect, err := h.oauth2.BeginLogout(c.Request.Context(), c.Query("logout_challenge"))
	if err != nil {
		oauth2Fail(c, oauth2PageView{Title: "无法退出"}, err)
		return
	}
	if redirect != "" {
		oauth2Redirect(c, redirect)
		return
	}
	view := oauth2PageView{Title: "退出登录", Challenge: lc.Challenge, Client: lc.ClientName}
	view.CSRF = h.oauth2CSRFToken(c, lc.Challenge)
	renderOAuth2Page(c, http.StatusOK, "logout", view)
}

// OAuth2LogoutSubmit POST /oauth2/logout
func (h *Handler) OAuth2LogoutSubmit(c *gin.Context) {
	if !h.oauth2Available(c) {
		return
	}
	ctx := c.Request.Context()
	challenge := c.PostForm("challenge")
	if !h.oauth2CSRFValid(c, challenge) {
		oauth2Fail(c, oauth2PageView{Title: "页面已过期"}, apperrors.New(40387, http.StatusForbidden, "页面已过期，请重新操作"))
		return
	}
	if c.PostForm("action") != "yes" {
		if err := h.oauth2.RejectLogout(ctx, challenge); err != nil {
			oauth2Fail(c, oauth2PageView{Title: "无法退出"}, err)
			return
		}
		renderOAuth2Page(c, http.StatusOK, "message", oauth2PageView{Title: "已保持登录", Notice: "你可以关闭此页面。"})
		return
	}
	redirect, err := h.oauth2.AcceptLogout(ctx, challenge)
	if err != nil {
		oauth2Fail(c, oauth2PageView{Title: "无法退出"}, err)
		return
	}
	oauth2Redirect(c, redirect)
}

// OAuth2LoggedOut GET /oauth2/logged-out —— 客户端没有给登出后跳转地址时的落地页。
func (h *Handler) OAuth2LoggedOut(c *gin.Context) {
	renderOAuth2Page(c, http.StatusOK, "message", oauth2PageView{Title: "已退出登录", Notice: "你已安全退出，可以关闭此页面。"})
}

// ── 设备授权 ────────────────────────────────────────────────────────

// OAuth2DevicePage GET /oauth2/device?device_challenge=&user_code=
func (h *Handler) OAuth2DevicePage(c *gin.Context) {
	if !h.oauth2Available(c) {
		return
	}
	challenge := c.Query("device_challenge")
	if strings.TrimSpace(challenge) == "" {
		renderOAuth2Page(c, http.StatusBadRequest, "message", oauth2PageView{Title: "无法继续", Error: "请在设备上重新获取登录代码，并按设备提示的地址打开此页面。"})
		return
	}
	view := oauth2PageView{Title: "设备登录", Challenge: challenge, UserCode: c.Query("user_code")}
	view.CSRF = h.oauth2CSRFToken(c, challenge)
	renderOAuth2Page(c, http.StatusOK, "device", view)
}

// OAuth2DeviceSubmit POST /oauth2/device
func (h *Handler) OAuth2DeviceSubmit(c *gin.Context) {
	if !h.oauth2Available(c) {
		return
	}
	challenge := c.PostForm("challenge")
	if !h.oauth2CSRFValid(c, challenge) {
		oauth2Fail(c, oauth2PageView{Title: "页面已过期"}, apperrors.New(40387, http.StatusForbidden, "页面已过期，请在设备上重新获取代码"))
		return
	}
	redirect, err := h.oauth2.VerifyDeviceCode(c.Request.Context(), challenge, c.PostForm("userCode"))
	if err != nil {
		view := oauth2PageView{Title: "设备登录", Challenge: challenge, UserCode: c.PostForm("userCode"), Error: apperrMessage(err)}
		var appErr *apperrors.AppError
		if !errors.As(err, &appErr) || appErr.HTTPStatus >= 500 || appErr.Code == 41080 {
			oauth2Fail(c, oauth2PageView{Title: "无法继续"}, err)
			return
		}
		view.CSRF = h.oauth2CSRFToken(c, challenge)
		renderOAuth2Page(c, appErr.HTTPStatus, "device", view)
		return
	}
	oauth2Redirect(c, redirect)
}

// OAuth2DeviceDone GET /oauth2/device/done —— 设备授权完成后的落地页。
func (h *Handler) OAuth2DeviceDone(c *gin.Context) {
	renderOAuth2Page(c, http.StatusOK, "message", oauth2PageView{Title: "设备已登录", Notice: "请回到设备上继续操作，可以关闭此页面。"})
}

// OAuth2ErrorPage GET /oauth2/error —— Hydra 无法把错误交回客户端时（如回调地址不合法）的落地页。
func (h *Handler) OAuth2ErrorPage(c *gin.Context) {
	message := firstNonEmptyString(c.Query("error_description"), c.Query("error_hint"), c.Query("error"), "授权请求无效")
	if len([]rune(message)) > 300 {
		message = string([]rune(message)[:300])
	}
	renderOAuth2Page(c, http.StatusBadRequest, "message", oauth2PageView{Title: "授权失败", Error: message})
}

// ── 令牌钩子 ────────────────────────────────────────────────────────

// OAuth2TokenHook POST /api/oauth2/hooks/token —— 只给 Hydra 调用，凭共享密钥认证。
func (h *Handler) OAuth2TokenHook(c *gin.Context) {
	if h.oauth2 == nil || !h.oauth2.Enabled() {
		response.Error(c, http.StatusServiceUnavailable, 50380, "OAuth2 授权服务未启用")
		return
	}
	secret := h.oauth2.TokenHookSecret()
	if secret == "" {
		// 没配密钥就等于任何人都能调这个接口来探测用户状态，宁可让令牌签发失败
		response.Error(c, http.StatusServiceUnavailable, 50382, "令牌钩子未配置密钥")
		return
	}
	got, _ := strings.CutPrefix(c.GetHeader("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(strings.TrimSpace(got)), []byte(secret)) != 1 {
		response.Error(c, http.StatusUnauthorized, 40180, "令牌钩子认证失败")
		return
	}
	var in service.OAuth2TokenHookRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "请求格式不正确")
		return
	}
	out, err := h.oauth2.HandleTokenHook(c.Request.Context(), in)
	if err != nil {
		var appErr *apperrors.AppError
		if errors.As(err, &appErr) && appErr.HTTPStatus < 500 {
			// Hydra 对 4xx 的处理是让本次令牌请求以 access_denied 失败
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "access_denied", "error_description": appErr.Message})
			return
		}
		h.writeError(c, err)
		return
	}
	if out == nil {
		c.Status(http.StatusNoContent)
		return
	}
	c.JSON(http.StatusOK, out)
}

// ── 管理端：客户端与授权记录 ─────────────────────────────────────────

func (h *Handler) oauth2AdminApp(c *gin.Context) (int64, bool) {
	if h.oauth2 == nil {
		response.Error(c, http.StatusServiceUnavailable, 50380, "OAuth2 授权服务未启用")
		return 0, false
	}
	return resolveAppID(c, h.app)
}

// AdminOAuth2Overview GET /api/admin/apps/:appkey/oauth2/overview
func (h *Handler) AdminOAuth2Overview(c *gin.Context) {
	appID, ok := h.oauth2AdminApp(c)
	if !ok {
		return
	}
	out, err := h.oauth2.Overview(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", out)
}

// AdminListOAuth2Clients GET /api/admin/apps/:appkey/oauth2/clients
func (h *Handler) AdminListOAuth2Clients(c *gin.Context) {
	appID, ok := h.oauth2AdminApp(c)
	if !ok {
		return
	}
	items, err := h.oauth2.ListClients(c.Request.Context(), appID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, http.StatusOK, "获取成功", gin.H{"items": items})
}

// AdminGetOAuth2Client GET /api/admin/apps/:appkey/oauth2/clients/:clientId
func (h *Handler) AdminGetOAuth2Client(c *gin.Context) {
	appID, ok := h.oauth2AdminApp(c)
	if !ok {
		return
	}
	item, err := h.oauth2.GetClient(c.Request.Context(), appID, c.Param("clientId"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, http.StatusOK, "获取成功", item)
}

// AdminCreateOAuth2Client POST /api/admin/apps/:appkey/oauth2/clients
func (h *Handler) AdminCreateOAuth2Client(c *gin.Context) {
	appID, ok := h.oauth2AdminApp(c)
	if !ok {
		return
	}
	var req service.OAuth2ClientInput
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.oauth2.CreateClient(c.Request.Context(), appID, req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, http.StatusCreated, "客户端已创建", item)
}

// AdminUpdateOAuth2Client PUT /api/admin/apps/:appkey/oauth2/clients/:clientId
func (h *Handler) AdminUpdateOAuth2Client(c *gin.Context) {
	appID, ok := h.oauth2AdminApp(c)
	if !ok {
		return
	}
	var req service.OAuth2ClientInput
	if err := bind(c, &req); err != nil {
		response.Error(c, http.StatusBadRequest, 40000, err.Error())
		return
	}
	item, err := h.oauth2.UpdateClient(c.Request.Context(), appID, c.Param("clientId"), req)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "客户端已保存", item)
}

// AdminRotateOAuth2ClientSecret POST /api/admin/apps/:appkey/oauth2/clients/:clientId/rotate-secret
func (h *Handler) AdminRotateOAuth2ClientSecret(c *gin.Context) {
	appID, ok := h.oauth2AdminApp(c)
	if !ok {
		return
	}
	item, err := h.oauth2.RotateClientSecret(c.Request.Context(), appID, c.Param("clientId"))
	if err != nil {
		h.writeError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, http.StatusOK, "密钥已重新生成", item)
}

// AdminDeleteOAuth2Client DELETE /api/admin/apps/:appkey/oauth2/clients/:clientId
func (h *Handler) AdminDeleteOAuth2Client(c *gin.Context) {
	appID, ok := h.oauth2AdminApp(c)
	if !ok {
		return
	}
	if err := h.oauth2.DeleteClient(c.Request.Context(), appID, c.Param("clientId")); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "客户端已删除", nil)
}

// AdminListUserOAuth2Grants GET /api/admin/apps/:appkey/users/:userId/oauth2-grants
func (h *Handler) AdminListUserOAuth2Grants(c *gin.Context) {
	appID, ok := h.oauth2AdminApp(c)
	if !ok {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	items, err := h.oauth2.ListUserGrants(c.Request.Context(), appID, userID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", gin.H{"items": items})
}

// AdminRevokeUserOAuth2Grants DELETE /api/admin/apps/:appkey/users/:userId/oauth2-grants?clientId=
func (h *Handler) AdminRevokeUserOAuth2Grants(c *gin.Context) {
	appID, ok := h.oauth2AdminApp(c)
	if !ok {
		return
	}
	userID, err := pathInt64(c, "userId")
	if err != nil {
		response.Error(c, http.StatusBadRequest, 40000, "无效的用户标识")
		return
	}
	if err := h.oauth2.RevokeUserGrants(c.Request.Context(), appID, userID, c.Query("clientId")); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "授权已撤销", nil)
}

// ── 网关：用户自助管理已授权的第三方应用 ──────────────────────────────

// AppMyOAuth2Grants GET /api/v1/apps/:appkey/me/oauth2/grants
func (h *Handler) AppMyOAuth2Grants(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if h.oauth2 == nil || !h.oauth2.Enabled() {
		// 未启用授权服务时没有任何授权记录，空列表比 503 对客户端更友好
		response.Success(c, http.StatusOK, "获取成功", gin.H{"items": []service.OAuth2GrantView{}})
		return
	}
	items, err := h.oauth2.ListUserGrants(c.Request.Context(), session.AppID, session.UserID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "获取成功", gin.H{"items": items})
}

// AppRevokeMyOAuth2Grant DELETE /api/v1/apps/:appkey/me/oauth2/grants/:clientId
func (h *Handler) AppRevokeMyOAuth2Grant(c *gin.Context) {
	session, ok := authSession(c)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 40100, "未认证")
		return
	}
	if h.oauth2 == nil || !h.oauth2.Enabled() {
		response.Error(c, http.StatusServiceUnavailable, 50380, "OAuth2 授权服务未启用")
		return
	}
	clientID := strings.TrimSpace(c.Param("clientId"))
	if clientID == "" {
		response.Error(c, http.StatusBadRequest, 40000, "缺少客户端标识")
		return
	}
	if err := h.oauth2.RevokeUserGrants(c.Request.Context(), session.AppID, session.UserID, clientID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, "已取消授权", gin.H{"clientId": clientID, "revokedAt": time.Now().UTC()})
}
