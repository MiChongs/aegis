package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"aegis/internal/config"
	appdomain "aegis/internal/domain/app"
	userdomain "aegis/internal/domain/user"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/hydra"

	"go.uber.org/zap"
)

// OAuth2ServerService 对外的 OAuth2 / OIDC 授权服务器。
//
// 协议由 Ory Hydra 承载，本服务是它的 login / consent / logout provider：
//
//	浏览器 → Hydra /oauth2/auth → Aegis /oauth2/login   认人（复用 App 网关的全部登录关卡）
//	                           → Aegis /oauth2/consent 问同意、出 claims
//	       → Hydra 签发 code / 令牌
//	Hydra 签发与刷新令牌时 → Aegis 令牌钩子           再判一次账号与应用状态
//
// 多租户的落点是「客户端属于某个应用」：Hydra 客户端的 owner 写成 aegis-app:<id>、
// metadata 写 aegisAppId，登录页据此只在该应用的用户库里认人。主体（sub）是用户 ID，
// 用户本身只属于一个应用，因此跨应用的主体天然不会撞。详见 docs/oauth2-provider.md。
type OAuth2ServerService struct {
	cfg     config.OAuth2ServerConfig
	apiBase string
	log     *zap.Logger
	hydra   *hydra.Client
	pg      *pgrepo.Repository
	auth    *AuthService
	app     *AppService
	avatar  *AvatarService
}

func NewOAuth2ServerService(cfg config.Config, log *zap.Logger, pg *pgrepo.Repository, auth *AuthService, app *AppService, avatar *AvatarService) *OAuth2ServerService {
	if log == nil {
		log = zap.NewNop()
	}
	if cfg.OAuth2Server.Enabled {
		// 这几项缺了不会让启动失败，但会让授权在第一次使用时才以难以排查的方式失败，在这里说清楚
		if cfg.OAuth2Server.TokenHookSecret == "" {
			log.Warn("OAUTH2_SERVER_ENABLED=true 但 HYDRA_TOKEN_HOOK_SECRET 为空：令牌钩子会拒绝 Hydra 的调用，所有令牌签发都将失败")
		}
		if cfg.OAuth2Server.HydraPublicURL == "" {
			log.Warn("OAUTH2_SERVER_ENABLED=true 但 HYDRA_PUBLIC_URL 为空：控制台无法给出签发方与端点地址")
		}
		if strings.TrimSpace(cfg.APIBaseURL) == "" {
			log.Warn("OAUTH2_SERVER_ENABLED=true 但 API_BASE_URL 为空：ID Token 里不会带头像地址")
		}
	}
	return &OAuth2ServerService{
		cfg:     cfg.OAuth2Server,
		apiBase: strings.TrimRight(strings.TrimSpace(cfg.APIBaseURL), "/"),
		log:     log,
		hydra:   hydra.New(cfg.OAuth2Server.HydraAdminURL, nil),
		pg:      pg,
		auth:    auth,
		app:     app,
		avatar:  avatar,
	}
}

const (
	oauth2OwnerPrefix   = "aegis-app:"
	oauth2MetaAppID     = "aegisAppId"
	errCodeOAuth2Off    = 50380
	errCodeOAuth2Hydra  = 50281
	errCodeOAuth2Client = 40480
)

// Enabled 授权服务器是否启用。nil 安全。
func (s *OAuth2ServerService) Enabled() bool {
	return s != nil && s.cfg.Enabled
}

// Config 页面层需要的配置（Cookie、CSRF 密钥）。
func (s *OAuth2ServerService) Config() config.OAuth2ServerConfig {
	if s == nil {
		return config.OAuth2ServerConfig{}
	}
	return s.cfg
}

func (s *OAuth2ServerService) ensureEnabled() error {
	if !s.Enabled() {
		return apperrors.New(errCodeOAuth2Off, http.StatusServiceUnavailable, "OAuth2 授权服务未启用")
	}
	return nil
}

// hydraError 把 Hydra 的错误翻成对外错误：challenge 失效是用户可理解的 410，其余是上游故障。
func (s *OAuth2ServerService) hydraError(op string, err error) error {
	if err == nil {
		return nil
	}
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) {
		return err
	}
	if hydra.IsNotFound(err) || hydra.IsGone(err) {
		return apperrors.New(41080, http.StatusGone, "授权请求已失效，请回到应用重新发起登录")
	}
	var he *hydra.Error
	if errors.As(err, &he) && he.StatusCode >= 400 && he.StatusCode < 500 {
		return apperrors.New(40080, http.StatusBadRequest, firstNonEmpty(he.Description, he.Code, "授权请求无效"))
	}
	s.log.Warn("hydra call failed", zap.String("op", op), zap.Error(err))
	return apperrors.New(errCodeOAuth2Hydra, http.StatusBadGateway, "授权服务暂时不可用，请稍后重试")
}

// ── 客户端与应用的绑定 ───────────────────────────────────────────────

func oauth2Owner(appID int64) string {
	return oauth2OwnerPrefix + strconv.FormatInt(appID, 10)
}

// clientAppID 从客户端上读出它所属的应用。owner 与 metadata 两处都写，读时以 owner 为准、
// 两者矛盾按未绑定处理 —— 直接在 Hydra 里改过 metadata 的客户端不该因此换了用户库。
func clientAppID(client hydra.OAuth2Client) (int64, bool) {
	raw, ok := strings.CutPrefix(client.Owner, oauth2OwnerPrefix)
	if !ok {
		return 0, false
	}
	appID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || appID <= 0 {
		return 0, false
	}
	if meta, ok := client.Metadata[oauth2MetaAppID]; ok {
		if fmt.Sprint(meta) != raw {
			return 0, false
		}
	}
	return appID, true
}

func (s *OAuth2ServerService) appForClient(ctx context.Context, client hydra.OAuth2Client) (*appdomain.App, error) {
	appID, ok := clientAppID(client)
	if !ok {
		return nil, apperrors.New(40381, http.StatusForbidden, "该客户端未绑定 Aegis 应用，无法登录")
	}
	if s.app == nil {
		return &appdomain.App{ID: appID}, nil
	}
	return s.app.GetApp(ctx, appID)
}

// ── 登录 ────────────────────────────────────────────────────────────

// OAuth2LoginContext 渲染登录页所需的全部上下文。
type OAuth2LoginContext struct {
	Challenge string
	App       *appdomain.App
	Client    hydra.OAuth2Client
	LoginHint string
	UILocales []string
}

// OAuth2LoginStep 登录页一步的结果：要么跳走，要么继续收二次验证码。
type OAuth2LoginStep struct {
	RedirectTo     string
	MFAChallengeID string
	MFAMethods     []string
}

// BeginLogin 打开登录页。Hydra 已记住登录态（skip）时不渲染页面，直接重新判定状态后放行。
func (s *OAuth2ServerService) BeginLogin(ctx context.Context, challenge string) (*OAuth2LoginContext, string, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, "", err
	}
	req, err := s.hydra.GetLoginRequest(ctx, challenge)
	if err != nil {
		return nil, "", s.hydraError("login.get", err)
	}
	app, err := s.appForClient(ctx, req.Client)
	if err != nil {
		redirect, rerr := s.rejectLogin(ctx, challenge, "unauthorized_client", err)
		return nil, redirect, rerr
	}
	if req.Skip {
		user, err := s.auth.ResolveOAuth2Subject(ctx, app.ID, req.Subject, false)
		if err == nil {
			redirect, aerr := s.hydra.AcceptLoginRequest(ctx, challenge, hydra.AcceptLogin{
				Subject:               OAuth2Subject(user.ID),
				ExtendSessionLifespan: true,
			})
			return nil, redirect, s.hydraError("login.accept_skip", aerr)
		}
		// 浏览器里记住的是**另一个应用**的账号（Hydra 的登录态按浏览器、不按应用），
		// 或者这个账号已经不能登录了。skip=true 时 Hydra 不允许换主体，
		// 于是带上 prompt=login 从原始授权请求重新开始：那一次 skip=false，
		// 用户登录另一个账号后由 Hydra 自己替换掉这个浏览器上的旧会话。
		// 不能由这里先删旧会话 —— Hydra 接受新登录时还会去删一次，删不到就报错。
		if restart, ok := withPromptLogin(req.RequestURL); ok {
			return nil, restart, nil
		}
		redirect, rerr := s.rejectLogin(ctx, challenge, "login_required", err)
		return nil, redirect, rerr
	}
	return &OAuth2LoginContext{
		Challenge: challenge,
		App:       app,
		Client:    req.Client,
		LoginHint: req.OIDCContext.LoginHint,
		UILocales: req.OIDCContext.UILocales,
	}, "", nil
}

// LoadLogin 表单提交时重新读出登录请求。与 BeginLogin 不同，它不处理 skip、没有副作用：
// 页面上的 challenge 来自用户提交的表单，只能当作「要继续哪一个请求」的指针。
func (s *OAuth2ServerService) LoadLogin(ctx context.Context, challenge string) (*OAuth2LoginContext, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	req, err := s.hydra.GetLoginRequest(ctx, challenge)
	if err != nil {
		return nil, s.hydraError("login.get", err)
	}
	if req.Skip {
		return nil, apperrors.New(40086, http.StatusConflict, "登录状态已变化，请回到应用重新发起登录")
	}
	app, err := s.appForClient(ctx, req.Client)
	if err != nil {
		return nil, err
	}
	return &OAuth2LoginContext{
		Challenge: challenge,
		App:       app,
		Client:    req.Client,
		LoginHint: req.OIDCContext.LoginHint,
		UILocales: req.OIDCContext.UILocales,
	}, nil
}

// withPromptLogin 给原始授权请求加上 prompt=login。客户端要求静默授权（prompt=none）时
// 不能弹出登录页，按 OIDC 的约定返回 login_required，因此返回 false。
func withPromptLogin(requestURL string) (string, bool) {
	u, err := url.Parse(requestURL)
	if err != nil || requestURL == "" {
		return "", false
	}
	q := u.Query()
	if slices.Contains(strings.Fields(q.Get("prompt")), "none") {
		return "", false
	}
	q.Set("prompt", "login")
	u.RawQuery = q.Encode()
	return u.String(), true
}

// OAuth2DeviceInfo 登录页拿得到的设备信息。
type OAuth2DeviceInfo struct {
	DeviceID  string
	Device    string
	IP        string
	UserAgent string
}

// LoginWithPassword 登录页提交账号密码。
func (s *OAuth2ServerService) LoginWithPassword(ctx context.Context, lc *OAuth2LoginContext, account, password string, remember bool, dev OAuth2DeviceInfo) (*OAuth2LoginStep, error) {
	result, err := s.auth.AuthenticatePasswordForOAuth2(ctx, lc.App.ID, account, password, dev.DeviceID, dev.Device, dev.IP, dev.UserAgent)
	if err != nil {
		return nil, err
	}
	if result.Challenge != nil {
		return &OAuth2LoginStep{MFAChallengeID: result.Challenge.ChallengeID, MFAMethods: result.Challenge.Methods}, nil
	}
	return s.acceptLogin(ctx, lc.Challenge, result, remember)
}

// LoginWithSecondFactor 登录页提交两步验证码或恢复码。
func (s *OAuth2ServerService) LoginWithSecondFactor(ctx context.Context, lc *OAuth2LoginContext, mfaChallengeID, code, recoveryCode string, remember bool) (*OAuth2LoginStep, error) {
	result, err := s.auth.VerifySecondFactorForOAuth2(ctx, lc.App.ID, mfaChallengeID, code, recoveryCode)
	if err != nil {
		return nil, err
	}
	return s.acceptLogin(ctx, lc.Challenge, result, remember)
}

func (s *OAuth2ServerService) acceptLogin(ctx context.Context, challenge string, result *OAuth2Authentication, remember bool) (*OAuth2LoginStep, error) {
	body := hydra.AcceptLogin{
		Subject:  OAuth2Subject(result.User.ID),
		Remember: remember,
		AMR:      result.AMR,
	}
	if remember {
		body.RememberFor = int64(s.cfg.LoginRememberFor / time.Second)
	}
	if len(result.AMR) > 1 {
		// 多因素登录的 ACR。值取 OIDC 生态里通用的写法，接入方可以据此要求二次认证（acr_values）
		body.ACR = "urn:aegis:acr:mfa"
	}
	redirect, err := s.hydra.AcceptLoginRequest(ctx, challenge, body)
	if err != nil {
		return nil, s.hydraError("login.accept", err)
	}
	return &OAuth2LoginStep{RedirectTo: redirect}, nil
}

// CancelLogin 用户在登录页点了「取消」。
func (s *OAuth2ServerService) CancelLogin(ctx context.Context, challenge string) (string, error) {
	if err := s.ensureEnabled(); err != nil {
		return "", err
	}
	redirect, err := s.hydra.RejectLoginRequest(ctx, challenge, hydra.RejectRequest{
		Error: "access_denied", ErrorDescription: "用户取消了登录",
	})
	return redirect, s.hydraError("login.reject", err)
}

// oauth2PublicMessage 给拒绝原因取一句可以交给接入方的话。拒绝原因会随回调地址
// 交到第三方手里，因此只透出业务错误本身的文案，内部错误一律换成泛化说法。
func oauth2PublicMessage(err error) string {
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) && appErr.Message != "" {
		return appErr.Message
	}
	return "无法完成登录"
}

func (s *OAuth2ServerService) rejectLogin(ctx context.Context, challenge, code string, cause error) (string, error) {
	redirect, err := s.hydra.RejectLoginRequest(ctx, challenge, hydra.RejectRequest{
		Error: code, ErrorDescription: oauth2PublicMessage(cause),
	})
	return redirect, s.hydraError("login.reject", err)
}

// ── 同意 ────────────────────────────────────────────────────────────

// OAuth2ScopeView 同意页上的一项权限。
type OAuth2ScopeView struct {
	Scope       string `json:"scope"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Required 用户不能取消勾选（openid / offline_access：取消等于拒绝整个请求）
	Required bool `json:"required"`
}

// oauth2ScopeCatalog 标准权限的说明。未列出的自定义权限在同意页上原样展示名字。
var oauth2ScopeCatalog = map[string]OAuth2ScopeView{
	"openid":         {Title: "确认你的身份", Description: "获取你在本应用中的用户标识", Required: true},
	"offline_access": {Title: "保持访问", Description: "在你不在线时继续访问已授权的信息", Required: true},
	"offline":        {Title: "保持访问", Description: "在你不在线时继续访问已授权的信息", Required: true},
	"profile":        {Title: "基本资料", Description: "昵称、账号名与头像"},
	"email":          {Title: "邮箱地址", Description: "你绑定的邮箱"},
	"phone":          {Title: "手机号码", Description: "你绑定的手机号"},
}

func describeScopes(scopes []string) []OAuth2ScopeView {
	out := make([]OAuth2ScopeView, 0, len(scopes))
	for _, scope := range scopes {
		view, ok := oauth2ScopeCatalog[scope]
		if !ok {
			view = OAuth2ScopeView{Title: scope, Description: "由该应用定义的访问权限"}
		}
		view.Scope = scope
		out = append(out, view)
	}
	return out
}

// OAuth2ConsentContext 渲染同意页所需的上下文。
type OAuth2ConsentContext struct {
	Challenge string
	App       *appdomain.App
	Client    hydra.OAuth2Client
	Scopes    []OAuth2ScopeView
	Nickname  string
	Account   string
	Avatar    string
}

// BeginConsent 打开同意页。已记住同意或客户端被标记为免同意（第一方应用）时直接放行。
func (s *OAuth2ServerService) BeginConsent(ctx context.Context, challenge string) (*OAuth2ConsentContext, string, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, "", err
	}
	req, err := s.hydra.GetConsentRequest(ctx, challenge)
	if err != nil {
		return nil, "", s.hydraError("consent.get", err)
	}
	app, err := s.appForClient(ctx, req.Client)
	if err != nil {
		redirect, rerr := s.rejectConsent(ctx, challenge, "unauthorized_client", err)
		return nil, redirect, rerr
	}
	user, err := s.auth.ResolveOAuth2Subject(ctx, app.ID, req.Subject, false)
	if err != nil {
		redirect, rerr := s.rejectConsent(ctx, challenge, "access_denied", err)
		return nil, redirect, rerr
	}
	if req.Skip || req.Client.SkipConsent {
		redirect, err := s.acceptConsent(ctx, req, app, user, req.RequestedScope, true)
		return nil, redirect, err
	}
	view := &OAuth2ConsentContext{
		Challenge: challenge,
		App:       app,
		Client:    req.Client,
		Scopes:    describeScopes(req.RequestedScope),
		Account:   user.Account,
	}
	if profile, _ := s.pg.GetUserProfileByUserID(ctx, user.ID); profile != nil {
		view.Nickname = profile.Nickname
		view.Avatar = s.avatarURL(ctx, user, profile)
	}
	return view, "", nil
}

// AcceptConsent 用户同意。granted 是用户保留勾选的权限；必选项无论是否勾选都会授予。
func (s *OAuth2ServerService) AcceptConsent(ctx context.Context, challenge string, granted []string, remember bool) (string, error) {
	if err := s.ensureEnabled(); err != nil {
		return "", err
	}
	req, err := s.hydra.GetConsentRequest(ctx, challenge)
	if err != nil {
		return "", s.hydraError("consent.get", err)
	}
	app, err := s.appForClient(ctx, req.Client)
	if err != nil {
		return s.rejectConsent(ctx, challenge, "unauthorized_client", err)
	}
	user, err := s.auth.ResolveOAuth2Subject(ctx, app.ID, req.Subject, false)
	if err != nil {
		return s.rejectConsent(ctx, challenge, "access_denied", err)
	}
	scopes := make([]string, 0, len(req.RequestedScope))
	for _, scope := range req.RequestedScope {
		if oauth2ScopeCatalog[scope].Required || slices.Contains(granted, scope) {
			scopes = append(scopes, scope)
		}
	}
	return s.acceptConsent(ctx, req, app, user, scopes, remember)
}

// RejectConsent 用户拒绝授权。
func (s *OAuth2ServerService) RejectConsent(ctx context.Context, challenge string) (string, error) {
	if err := s.ensureEnabled(); err != nil {
		return "", err
	}
	redirect, err := s.hydra.RejectConsentRequest(ctx, challenge, hydra.RejectRequest{
		Error: "access_denied", ErrorDescription: "用户拒绝了授权",
	})
	return redirect, s.hydraError("consent.reject", err)
}

func (s *OAuth2ServerService) rejectConsent(ctx context.Context, challenge, code string, cause error) (string, error) {
	redirect, err := s.hydra.RejectConsentRequest(ctx, challenge, hydra.RejectRequest{
		Error: code, ErrorDescription: oauth2PublicMessage(cause),
	})
	return redirect, s.hydraError("consent.reject", err)
}

func (s *OAuth2ServerService) acceptConsent(ctx context.Context, req *hydra.ConsentRequest, app *appdomain.App, user *userdomain.User, scopes []string, remember bool) (string, error) {
	body := hydra.AcceptConsent{
		GrantScope:               scopes,
		GrantAccessTokenAudience: req.RequestedAccessTokenAudience,
		Remember:                 remember,
		Session:                  s.buildSession(ctx, app, user, scopes),
	}
	if remember {
		body.RememberFor = int64(s.cfg.ConsentRememberFor / time.Second)
	}
	redirect, err := s.hydra.AcceptConsentRequest(ctx, req.Challenge, body)
	return redirect, s.hydraError("consent.accept", err)
}

// buildSession 按授予的权限组装 claims。ID Token 只放标准 OIDC claims 与应用标识；
// 访问令牌（及内省结果的 ext）放应用标识，资源服务器据此判断令牌属于哪个应用。
func (s *OAuth2ServerService) buildSession(ctx context.Context, app *appdomain.App, user *userdomain.User, scopes []string) hydra.ConsentSession {
	idToken := map[string]any{"appid": app.ID}
	if app.AppKey != "" {
		idToken["app_key"] = app.AppKey
	}
	profile, _ := s.pg.GetUserProfileByUserID(ctx, user.ID)
	if slices.Contains(scopes, "profile") {
		idToken["preferred_username"] = user.Account
		name := user.Account
		if profile != nil && strings.TrimSpace(profile.Nickname) != "" {
			name = profile.Nickname
			idToken["nickname"] = profile.Nickname
		}
		idToken["name"] = name
		if picture := s.avatarURL(ctx, user, profile); picture != "" {
			idToken["picture"] = picture
		}
		idToken["updated_at"] = user.UpdatedAt.Unix()
	}
	if profile != nil && slices.Contains(scopes, "email") && strings.TrimSpace(profile.Email) != "" {
		// 不下发 email_verified：管理员导入、后台改写的邮箱没有经过验证码，Aegis 无从担保
		idToken["email"] = profile.Email
	}
	if profile != nil && slices.Contains(scopes, "phone") && strings.TrimSpace(profile.Phone) != "" {
		idToken["phone_number"] = profile.Phone
	}
	return hydra.ConsentSession{
		AccessToken: map[string]any{"appid": app.ID},
		IDToken:     idToken,
	}
}

func (s *OAuth2ServerService) avatarURL(ctx context.Context, user *userdomain.User, profile *userdomain.Profile) string {
	if s.avatar == nil {
		return ""
	}
	raw := ""
	if profile != nil {
		raw = profile.Avatar
	}
	picture := s.avatar.ResolveUserAvatar(ctx, s.apiBase, user.AppID, user.ID, raw, user.Account)
	// 相对地址对接入方没有意义（它不知道 Aegis 在哪），配了 API_BASE_URL 才会是绝对地址
	if !strings.HasPrefix(picture, "http://") && !strings.HasPrefix(picture, "https://") {
		return ""
	}
	return picture
}

// ── 登出 ────────────────────────────────────────────────────────────

// OAuth2LogoutContext 渲染登出确认页的上下文。
type OAuth2LogoutContext struct {
	Challenge  string
	ClientName string
}

// BeginLogout 打开登出页。由客户端发起且客户端声明免确认时直接登出。
func (s *OAuth2ServerService) BeginLogout(ctx context.Context, challenge string) (*OAuth2LogoutContext, string, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, "", err
	}
	req, err := s.hydra.GetLogoutRequest(ctx, challenge)
	if err != nil {
		return nil, "", s.hydraError("logout.get", err)
	}
	view := &OAuth2LogoutContext{Challenge: challenge}
	if req.Client != nil {
		view.ClientName = req.Client.ClientName
		if req.RPInitiated && req.Client.SkipLogoutConsent != nil && *req.Client.SkipLogoutConsent {
			redirect, err := s.hydra.AcceptLogoutRequest(ctx, challenge)
			return nil, redirect, s.hydraError("logout.accept", err)
		}
	}
	return view, "", nil
}

func (s *OAuth2ServerService) AcceptLogout(ctx context.Context, challenge string) (string, error) {
	if err := s.ensureEnabled(); err != nil {
		return "", err
	}
	redirect, err := s.hydra.AcceptLogoutRequest(ctx, challenge)
	return redirect, s.hydraError("logout.accept", err)
}

func (s *OAuth2ServerService) RejectLogout(ctx context.Context, challenge string) error {
	if err := s.ensureEnabled(); err != nil {
		return err
	}
	return s.hydraError("logout.reject", s.hydra.RejectLogoutRequest(ctx, challenge))
}

// ── 设备授权（RFC 8628）────────────────────────────────────────────

var deviceUserCodePattern = regexp.MustCompile(`[^A-Za-z0-9]`)

// VerifyDeviceCode 用户在验证页输入电视、命令行等设备上显示的用户码。
func (s *OAuth2ServerService) VerifyDeviceCode(ctx context.Context, challenge, userCode string) (string, error) {
	if err := s.ensureEnabled(); err != nil {
		return "", err
	}
	// 用户码常被分段展示（ABCD-EFGH），用户也可能输入小写或空格；Hydra 只认原始字符
	code := deviceUserCodePattern.ReplaceAllString(strings.TrimSpace(userCode), "")
	if code == "" {
		return "", apperrors.New(40081, http.StatusBadRequest, "请输入设备上显示的代码")
	}
	redirect, err := s.hydra.AcceptDeviceRequest(ctx, challenge, code)
	if err != nil {
		var he *hydra.Error
		if errors.As(err, &he) && he.StatusCode >= 400 && he.StatusCode < 500 {
			return "", apperrors.New(40082, http.StatusBadRequest, "代码无效或已过期，请核对设备上的代码")
		}
		return "", s.hydraError("device.accept", err)
	}
	return redirect, nil
}

// ── 令牌钩子 ────────────────────────────────────────────────────────

// OAuth2TokenHookRequest Hydra 在签发令牌前（含刷新）POST 过来的内容，只取用得到的字段。
type OAuth2TokenHookRequest struct {
	Session struct {
		IDToken struct {
			Subject       string         `json:"subject"`
			IDTokenClaims map[string]any `json:"id_token_claims"`
		} `json:"id_token"`
		Extra    map[string]any `json:"extra"`
		ClientID string         `json:"client_id"`
	} `json:"session"`
	Request struct {
		ClientID        string   `json:"client_id"`
		GrantedScopes   []string `json:"granted_scopes"`
		GrantedAudience []string `json:"granted_audience"`
		GrantTypes      []string `json:"grant_types"`
	} `json:"request"`
}

// OAuth2TokenHookResponse 返回给 Hydra 的新 claims；nil 表示不改动（回 204）。
type OAuth2TokenHookResponse struct {
	Session hydra.ConsentSession `json:"session"`
}

// HandleTokenHook 每次签发令牌前的状态关卡。
//
// Hydra 刷新令牌时完全不经过登录与同意页，没有这一步的话，被封禁的用户、
// 被冻结的应用可以靠刷新令牌一直续命。这里拒绝就是让本次令牌请求失败。
// 刷新时顺带按当前资料重算 ID Token 的 claims，改了昵称头像的用户不必重新授权。
func (s *OAuth2ServerService) HandleTokenHook(ctx context.Context, in OAuth2TokenHookRequest) (*OAuth2TokenHookResponse, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	subject := firstNonEmpty(in.Session.IDToken.Subject, fmt.Sprint(in.Session.IDToken.IDTokenClaims["sub"]))
	if subject == "" || subject == "<nil>" {
		// client_credentials：没有用户，令牌代表客户端自己
		return nil, nil
	}
	clientID := firstNonEmpty(in.Request.ClientID, in.Session.ClientID)
	client, err := s.hydra.GetClient(ctx, clientID)
	if err != nil {
		return nil, s.hydraError("hook.client", err)
	}
	app, err := s.appForClient(ctx, *client)
	if err != nil {
		return nil, err
	}
	refresh := slices.Contains(in.Request.GrantTypes, "refresh_token")
	user, err := s.auth.ResolveOAuth2Subject(ctx, app.ID, subject, refresh)
	if err != nil {
		return nil, err
	}
	if !refresh {
		return nil, nil
	}
	session := s.buildSession(ctx, app, user, in.Request.GrantedScopes)
	return &OAuth2TokenHookResponse{Session: session}, nil
}

// TokenHookSecret 令牌钩子的共享密钥（只在 Hydra 与 Aegis 之间）。
func (s *OAuth2ServerService) TokenHookSecret() string {
	if s == nil {
		return ""
	}
	return s.cfg.TokenHookSecret
}

// ── 概览 ────────────────────────────────────────────────────────────

// OAuth2ServerOverview 控制台展示给接入方的授权服务器信息。
type OAuth2ServerOverview struct {
	Enabled      bool              `json:"enabled"`
	Ready        bool              `json:"ready"`
	ReadyError   string            `json:"readyError,omitempty"`
	Issuer       string            `json:"issuer,omitempty"`
	Endpoints    map[string]string `json:"endpoints,omitempty"`
	Scopes       []OAuth2ScopeView `json:"scopes"`
	GrantTypes   []string          `json:"grantTypes"`
	ClientCount  int               `json:"clientCount"`
	SubjectClaim string            `json:"subjectClaim"`
}

func (s *OAuth2ServerService) Overview(ctx context.Context, appID int64) (*OAuth2ServerOverview, error) {
	out := &OAuth2ServerOverview{
		Enabled:      s.Enabled(),
		Scopes:       describeScopes([]string{"openid", "offline_access", "profile", "email", "phone"}),
		GrantTypes:   slices.Clone(oauth2AllowedGrantTypes),
		SubjectClaim: "sub 为用户 ID（与 Aegis 接口中的 userId 相同）",
	}
	if !out.Enabled {
		return out, nil
	}
	issuer := s.cfg.HydraPublicURL
	out.Issuer = issuer
	if issuer != "" {
		out.Endpoints = map[string]string{
			"discovery":     issuer + "/.well-known/openid-configuration",
			"authorization": issuer + "/oauth2/auth",
			"token":         issuer + "/oauth2/token",
			"userinfo":      issuer + "/userinfo",
			"jwks":          issuer + "/.well-known/jwks.json",
			"revocation":    issuer + "/oauth2/revoke",
			"endSession":    issuer + "/oauth2/sessions/logout",
			"device":        issuer + "/oauth2/device/auth",
		}
	}
	if err := s.hydra.Ready(ctx); err != nil {
		out.ReadyError = "无法连接 Hydra 管理端"
		return out, nil
	}
	out.Ready = true
	page, err := s.hydra.ListClients(ctx, hydra.ListClientsQuery{Owner: oauth2Owner(appID), PageSize: 500})
	if err == nil {
		out.ClientCount = len(page.Items)
	}
	return out, nil
}

// ── 客户端管理（控制台）────────────────────────────────────────────

var oauth2AllowedGrantTypes = []string{
	"authorization_code",
	"refresh_token",
	"client_credentials",
	"urn:ietf:params:oauth:grant-type:device_code",
}

var oauth2AllowedAuthMethods = []string{"client_secret_basic", "client_secret_post", "none"}

var oauth2ScopePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9:._/-]{0,63}$`)

// OAuth2ClientInput 控制台创建 / 编辑客户端的入参。
type OAuth2ClientInput struct {
	Name                    string   `json:"name"`
	RedirectURIs            []string `json:"redirectUris"`
	PostLogoutRedirectURIs  []string `json:"postLogoutRedirectUris"`
	GrantTypes              []string `json:"grantTypes"`
	Scopes                  []string `json:"scopes"`
	Audience                []string `json:"audience"`
	TokenEndpointAuthMethod string   `json:"tokenEndpointAuthMethod"`
	LogoURI                 string   `json:"logoUri"`
	ClientURI               string   `json:"clientUri"`
	PolicyURI               string   `json:"policyUri"`
	TosURI                  string   `json:"tosUri"`
	AllowedCORSOrigins      []string `json:"allowedCorsOrigins"`
	SkipConsent             bool     `json:"skipConsent"`
	SkipLogoutConsent       bool     `json:"skipLogoutConsent"`
	SubjectType             string   `json:"subjectType"`
	AccessTokenStrategy     string   `json:"accessTokenStrategy"`
	FrontchannelLogoutURI   string   `json:"frontchannelLogoutUri"`
	BackchannelLogoutURI    string   `json:"backchannelLogoutUri"`
}

// OAuth2ClientView 控制台看到的客户端。ClientSecret 只在创建与轮换时出现一次。
type OAuth2ClientView struct {
	ClientID                string    `json:"clientId"`
	Name                    string    `json:"name"`
	ClientSecret            string    `json:"clientSecret,omitempty"`
	Public                  bool      `json:"public"`
	RedirectURIs            []string  `json:"redirectUris"`
	PostLogoutRedirectURIs  []string  `json:"postLogoutRedirectUris"`
	GrantTypes              []string  `json:"grantTypes"`
	Scopes                  []string  `json:"scopes"`
	Audience                []string  `json:"audience"`
	TokenEndpointAuthMethod string    `json:"tokenEndpointAuthMethod"`
	LogoURI                 string    `json:"logoUri,omitempty"`
	ClientURI               string    `json:"clientUri,omitempty"`
	PolicyURI               string    `json:"policyUri,omitempty"`
	TosURI                  string    `json:"tosUri,omitempty"`
	AllowedCORSOrigins      []string  `json:"allowedCorsOrigins"`
	SkipConsent             bool      `json:"skipConsent"`
	SkipLogoutConsent       bool      `json:"skipLogoutConsent"`
	SubjectType             string    `json:"subjectType"`
	AccessTokenStrategy     string    `json:"accessTokenStrategy,omitempty"`
	FrontchannelLogoutURI   string    `json:"frontchannelLogoutUri,omitempty"`
	BackchannelLogoutURI    string    `json:"backchannelLogoutUri,omitempty"`
	CreatedAt               time.Time `json:"createdAt"`
	UpdatedAt               time.Time `json:"updatedAt"`
}

func clientView(c hydra.OAuth2Client) OAuth2ClientView {
	return OAuth2ClientView{
		ClientID:                c.ClientID,
		Name:                    c.ClientName,
		ClientSecret:            c.ClientSecret,
		Public:                  c.TokenEndpointAuthMethod == "none",
		RedirectURIs:            nonNil(c.RedirectURIs),
		PostLogoutRedirectURIs:  nonNil(c.PostLogoutRedirectURIs),
		GrantTypes:              nonNil(c.GrantTypes),
		Scopes:                  strings.Fields(c.Scope),
		Audience:                nonNil(c.Audience),
		TokenEndpointAuthMethod: c.TokenEndpointAuthMethod,
		LogoURI:                 c.LogoURI,
		ClientURI:               c.ClientURI,
		PolicyURI:               c.PolicyURI,
		TosURI:                  c.TosURI,
		AllowedCORSOrigins:      nonNil(c.AllowedCORSOrigins),
		SkipConsent:             c.SkipConsent,
		SkipLogoutConsent:       c.SkipLogoutConsent != nil && *c.SkipLogoutConsent,
		SubjectType:             firstNonEmpty(c.SubjectType, "public"),
		AccessTokenStrategy:     c.AccessTokenStrategy,
		FrontchannelLogoutURI:   c.FrontchannelLogoutURI,
		BackchannelLogoutURI:    c.BackchannelLogoutURI,
		CreatedAt:               c.CreatedAt,
		UpdatedAt:               c.UpdatedAt,
	}
}

func nonNil(items []string) []string {
	if items == nil {
		return []string{}
	}
	return items
}

func (s *OAuth2ServerService) ListClients(ctx context.Context, appID int64) ([]OAuth2ClientView, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	out := []OAuth2ClientView{}
	token := ""
	for range 20 {
		page, err := s.hydra.ListClients(ctx, hydra.ListClientsQuery{Owner: oauth2Owner(appID), PageSize: 250, PageToken: token})
		if err != nil {
			return nil, s.hydraError("clients.list", err)
		}
		for _, item := range page.Items {
			out = append(out, clientView(item))
		}
		token = page.NextPageToken
		if token == "" || len(page.Items) == 0 {
			break
		}
	}
	slices.SortFunc(out, func(a, b OAuth2ClientView) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return out, nil
}

// ownedClient 读出客户端并确认它属于该应用；不属于时按不存在处理，不透露别的应用有这个 ID。
func (s *OAuth2ServerService) ownedClient(ctx context.Context, appID int64, clientID string) (*hydra.OAuth2Client, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	client, err := s.hydra.GetClient(ctx, strings.TrimSpace(clientID))
	if err != nil {
		if hydra.IsNotFound(err) {
			return nil, apperrors.New(errCodeOAuth2Client, http.StatusNotFound, "客户端不存在")
		}
		var he *hydra.Error
		if errors.As(err, &he) && he.StatusCode == http.StatusUnauthorized {
			return nil, apperrors.New(errCodeOAuth2Client, http.StatusNotFound, "客户端不存在")
		}
		return nil, s.hydraError("clients.get", err)
	}
	if owner, ok := clientAppID(*client); !ok || owner != appID {
		return nil, apperrors.New(errCodeOAuth2Client, http.StatusNotFound, "客户端不存在")
	}
	return client, nil
}

func (s *OAuth2ServerService) GetClient(ctx context.Context, appID int64, clientID string) (*OAuth2ClientView, error) {
	client, err := s.ownedClient(ctx, appID, clientID)
	if err != nil {
		return nil, err
	}
	view := clientView(*client)
	view.ClientSecret = ""
	return &view, nil
}

func (s *OAuth2ServerService) CreateClient(ctx context.Context, appID int64, in OAuth2ClientInput) (*OAuth2ClientView, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	client, err := buildHydraClient(appID, in)
	if err != nil {
		return nil, err
	}
	created, err := s.hydra.CreateClient(ctx, client)
	if err != nil {
		return nil, s.hydraError("clients.create", err)
	}
	view := clientView(*created)
	return &view, nil
}

func (s *OAuth2ServerService) UpdateClient(ctx context.Context, appID int64, clientID string, in OAuth2ClientInput) (*OAuth2ClientView, error) {
	current, err := s.ownedClient(ctx, appID, clientID)
	if err != nil {
		return nil, err
	}
	next, err := buildHydraClient(appID, in)
	if err != nil {
		return nil, err
	}
	// 读-改-写：把 Aegis 不管的字段（各类令牌寿命、JWKS 等）原样带回，PUT 才不会把它们清成默认值
	merged := *current
	merged.ClientSecret = ""
	merged.ClientName = next.ClientName
	merged.RedirectURIs = next.RedirectURIs
	merged.PostLogoutRedirectURIs = next.PostLogoutRedirectURIs
	merged.GrantTypes = next.GrantTypes
	merged.ResponseTypes = next.ResponseTypes
	merged.Scope = next.Scope
	merged.Audience = next.Audience
	merged.TokenEndpointAuthMethod = next.TokenEndpointAuthMethod
	merged.LogoURI = next.LogoURI
	merged.ClientURI = next.ClientURI
	merged.PolicyURI = next.PolicyURI
	merged.TosURI = next.TosURI
	merged.AllowedCORSOrigins = next.AllowedCORSOrigins
	merged.SkipConsent = next.SkipConsent
	merged.SkipLogoutConsent = next.SkipLogoutConsent
	merged.SubjectType = next.SubjectType
	merged.AccessTokenStrategy = next.AccessTokenStrategy
	merged.FrontchannelLogoutURI = next.FrontchannelLogoutURI
	merged.BackchannelLogoutURI = next.BackchannelLogoutURI
	merged.Owner = next.Owner
	merged.Metadata = mergeMetadata(current.Metadata, next.Metadata)
	if current.TokenEndpointAuthMethod == "none" && merged.TokenEndpointAuthMethod != "none" {
		return nil, apperrors.New(40083, http.StatusBadRequest, "公开客户端不能改为机密客户端，请新建一个客户端")
	}
	updated, err := s.hydra.SetClient(ctx, merged)
	if err != nil {
		return nil, s.hydraError("clients.update", err)
	}
	view := clientView(*updated)
	view.ClientSecret = ""
	return &view, nil
}

// RotateClientSecret 生成新密钥（旧密钥立即失效），并吊销这个客户端已签发的访问令牌。
func (s *OAuth2ServerService) RotateClientSecret(ctx context.Context, appID int64, clientID string) (*OAuth2ClientView, error) {
	current, err := s.ownedClient(ctx, appID, clientID)
	if err != nil {
		return nil, err
	}
	if current.TokenEndpointAuthMethod == "none" {
		return nil, apperrors.New(40084, http.StatusBadRequest, "公开客户端没有密钥")
	}
	secret, err := randomURLToken(32)
	if err != nil {
		return nil, err
	}
	next := *current
	next.ClientSecret = secret
	updated, err := s.hydra.SetClient(ctx, next)
	if err != nil {
		return nil, s.hydraError("clients.rotate", err)
	}
	if err := s.hydra.DeleteClientTokens(ctx, current.ClientID); err != nil {
		s.log.Warn("revoke client tokens after rotation failed", zap.String("clientId", current.ClientID), zap.Error(err))
	}
	view := clientView(*updated)
	view.ClientSecret = secret
	return &view, nil
}

func (s *OAuth2ServerService) DeleteClient(ctx context.Context, appID int64, clientID string) error {
	current, err := s.ownedClient(ctx, appID, clientID)
	if err != nil {
		return err
	}
	if err := s.hydra.DeleteClient(ctx, current.ClientID); err != nil {
		return s.hydraError("clients.delete", err)
	}
	return nil
}

func mergeMetadata(current, next map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range current {
		out[k] = v
	}
	for k, v := range next {
		out[k] = v
	}
	return out
}

// buildHydraClient 校验控制台入参并组装 Hydra 客户端。
func buildHydraClient(appID int64, in OAuth2ClientInput) (hydra.OAuth2Client, error) {
	bad := func(msg string) (hydra.OAuth2Client, error) {
		return hydra.OAuth2Client{}, apperrors.New(40085, http.StatusBadRequest, msg)
	}
	name := strings.TrimSpace(in.Name)
	if name == "" || len([]rune(name)) > 64 {
		return bad("客户端名称不能为空，且不超过 64 个字符")
	}
	method := firstNonEmpty(strings.TrimSpace(in.TokenEndpointAuthMethod), "client_secret_basic")
	if !slices.Contains(oauth2AllowedAuthMethods, method) {
		return bad("不支持的客户端认证方式：" + method)
	}
	grants := dedupe(in.GrantTypes)
	if len(grants) == 0 {
		grants = []string{"authorization_code", "refresh_token"}
	}
	for _, g := range grants {
		if !slices.Contains(oauth2AllowedGrantTypes, g) {
			return bad("不支持的授权类型：" + g)
		}
	}
	if method == "none" && slices.Contains(grants, "client_credentials") {
		return bad("公开客户端不能使用客户端凭据模式")
	}
	redirects, err := validateRedirectURIs(in.RedirectURIs, "回调地址")
	if err != nil {
		return hydra.OAuth2Client{}, err
	}
	if slices.Contains(grants, "authorization_code") && len(redirects) == 0 {
		return bad("授权码模式至少需要一个回调地址")
	}
	postLogout, err := validateRedirectURIs(in.PostLogoutRedirectURIs, "登出后跳转地址")
	if err != nil {
		return hydra.OAuth2Client{}, err
	}
	scopes := dedupe(in.Scopes)
	if len(scopes) == 0 {
		scopes = []string{"openid", "offline_access", "profile"}
	}
	for _, scope := range scopes {
		if !oauth2ScopePattern.MatchString(scope) {
			return bad("权限名称格式不正确：" + scope)
		}
	}
	for _, aud := range in.Audience {
		if _, err := url.ParseRequestURI(strings.TrimSpace(aud)); err != nil && strings.TrimSpace(aud) != "" {
			return bad("受众格式不正确：" + aud)
		}
	}
	origins := dedupe(in.AllowedCORSOrigins)
	for _, origin := range origins {
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || (u.Path != "" && u.Path != "/") {
			return bad("跨域来源格式不正确：" + origin)
		}
	}
	for label, raw := range map[string]string{
		"Logo 地址": in.LogoURI, "主页地址": in.ClientURI, "隐私政策地址": in.PolicyURI, "服务条款地址": in.TosURI,
		"前端通道登出地址": in.FrontchannelLogoutURI, "后端通道登出地址": in.BackchannelLogoutURI,
	} {
		if raw = strings.TrimSpace(raw); raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return bad(label + "必须是 http(s) 绝对地址")
		}
	}
	subjectType := firstNonEmpty(strings.TrimSpace(in.SubjectType), "public")
	if subjectType != "public" && subjectType != "pairwise" {
		return bad("主体类型只能是 public 或 pairwise")
	}
	strategy := strings.TrimSpace(in.AccessTokenStrategy)
	if strategy != "" && strategy != "opaque" && strategy != "jwt" {
		return bad("访问令牌格式只能是 opaque 或 jwt")
	}
	responseTypes := []string{}
	if slices.Contains(grants, "authorization_code") {
		responseTypes = append(responseTypes, "code")
	}
	skipLogout := in.SkipLogoutConsent
	return hydra.OAuth2Client{
		ClientName:              name,
		RedirectURIs:            redirects,
		PostLogoutRedirectURIs:  postLogout,
		GrantTypes:              grants,
		ResponseTypes:           responseTypes,
		Scope:                   strings.Join(scopes, " "),
		Audience:                dedupe(in.Audience),
		Owner:                   oauth2Owner(appID),
		LogoURI:                 strings.TrimSpace(in.LogoURI),
		ClientURI:               strings.TrimSpace(in.ClientURI),
		PolicyURI:               strings.TrimSpace(in.PolicyURI),
		TosURI:                  strings.TrimSpace(in.TosURI),
		Contacts:                []string{},
		AllowedCORSOrigins:      origins,
		SubjectType:             subjectType,
		TokenEndpointAuthMethod: method,
		FrontchannelLogoutURI:   strings.TrimSpace(in.FrontchannelLogoutURI),
		BackchannelLogoutURI:    strings.TrimSpace(in.BackchannelLogoutURI),
		SkipConsent:             in.SkipConsent,
		SkipLogoutConsent:       &skipLogout,
		AccessTokenStrategy:     strategy,
		Metadata:                map[string]any{oauth2MetaAppID: appID},
	}, nil
}

// validateRedirectURIs 回调地址的规则与 OAuth 2.0 for Native Apps（RFC 8252）一致：
// 网页用 https；http 只允许回环地址（本机调试、桌面应用）；移动应用用私有协议（com.example.app:/cb）。
func validateRedirectURIs(raw []string, label string) ([]string, error) {
	items := dedupe(raw)
	if len(items) > 20 {
		return nil, apperrors.New(40085, http.StatusBadRequest, label+"最多 20 个")
	}
	for _, item := range items {
		u, err := url.Parse(item)
		if err != nil || u.Scheme == "" {
			return nil, apperrors.New(40085, http.StatusBadRequest, label+"格式不正确："+item)
		}
		if u.Fragment != "" {
			return nil, apperrors.New(40085, http.StatusBadRequest, label+"不能包含 # 片段："+item)
		}
		switch strings.ToLower(u.Scheme) {
		case "https":
			if u.Host == "" {
				return nil, apperrors.New(40085, http.StatusBadRequest, label+"缺少主机名："+item)
			}
		case "http":
			host := u.Hostname()
			if host != "localhost" && host != "127.0.0.1" && host != "::1" {
				return nil, apperrors.New(40085, http.StatusBadRequest, label+"使用 http 时只允许本机回环地址："+item)
			}
		case "javascript", "data", "file", "vbscript":
			return nil, apperrors.New(40085, http.StatusBadRequest, label+"协议不安全："+item)
		default:
			// 私有协议必须像反向域名（含点），避免与其他应用抢同一个 myapp://
			if !strings.Contains(u.Scheme, ".") {
				return nil, apperrors.New(40085, http.StatusBadRequest, label+"的私有协议需使用反向域名形式，如 com.example.app:/callback")
			}
		}
	}
	return items, nil
}

func dedupe(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" && !slices.Contains(out, item) {
			out = append(out, item)
		}
	}
	return out
}

// ── 用户的授权记录 ──────────────────────────────────────────────────

// OAuth2GrantView 用户对一个客户端的已记住授权。
type OAuth2GrantView struct {
	ClientID   string    `json:"clientId"`
	ClientName string    `json:"clientName"`
	LogoURI    string    `json:"logoUri,omitempty"`
	ClientURI  string    `json:"clientUri,omitempty"`
	Scopes     []string  `json:"scopes"`
	GrantedAt  time.Time `json:"grantedAt"`
}

// ListUserGrants 列出用户授权过的客户端（同一客户端多次授权合并成一条，取最近一次）。
func (s *OAuth2ServerService) ListUserGrants(ctx context.Context, appID, userID int64) ([]OAuth2GrantView, error) {
	if err := s.ensureEnabled(); err != nil {
		return nil, err
	}
	sessions, err := s.hydra.ListConsentSessions(ctx, OAuth2Subject(userID))
	if err != nil {
		return nil, s.hydraError("grants.list", err)
	}
	byClient := map[string]*OAuth2GrantView{}
	for _, item := range sessions {
		client := item.ConsentRequest.Client
		if owner, ok := clientAppID(client); !ok || owner != appID {
			continue
		}
		existing := byClient[client.ClientID]
		if existing != nil && existing.GrantedAt.After(item.HandledAt) {
			continue
		}
		byClient[client.ClientID] = &OAuth2GrantView{
			ClientID:   client.ClientID,
			ClientName: client.ClientName,
			LogoURI:    client.LogoURI,
			ClientURI:  client.ClientURI,
			Scopes:     nonNil(item.GrantScope),
			GrantedAt:  item.HandledAt,
		}
	}
	out := make([]OAuth2GrantView, 0, len(byClient))
	for _, v := range byClient {
		out = append(out, *v)
	}
	slices.SortFunc(out, func(a, b OAuth2GrantView) int { return b.GrantedAt.Compare(a.GrantedAt) })
	return out, nil
}

// RevokeUserGrants 撤销授权并吊销对应令牌。clientID 为空时撤销该用户的全部授权并让其在
// 授权服务器上的登录态失效（「强制下线全部第三方应用」）。
func (s *OAuth2ServerService) RevokeUserGrants(ctx context.Context, appID, userID int64, clientID string) error {
	if err := s.ensureEnabled(); err != nil {
		return err
	}
	subject := OAuth2Subject(userID)
	clientID = strings.TrimSpace(clientID)
	if clientID != "" {
		if _, err := s.ownedClient(ctx, appID, clientID); err != nil {
			return err
		}
		return s.hydraError("grants.revoke", s.hydra.RevokeConsentSessions(ctx, subject, clientID))
	}
	if err := s.hydra.RevokeConsentSessions(ctx, subject, ""); err != nil {
		return s.hydraError("grants.revoke_all", err)
	}
	return s.hydraError("sessions.revoke", s.hydra.RevokeLoginSessions(ctx, subject))
}

// RevokeUserEverywhere 账号被封禁、删除、重置密码或被踢下线时调用：撤销全部授权与登录态。
// 异步执行，不拖慢调用方；未启用或 Hydra 不可用时只记日志 ——
// 令牌钩子与登录关卡仍会拦住这个账号，这里只是让已签发的令牌立刻失效。
func (s *OAuth2ServerService) RevokeUserEverywhere(appID, userID int64) {
	if !s.Enabled() {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := s.RevokeUserGrants(ctx, appID, userID, ""); err != nil {
			s.log.Warn("revoke oauth2 grants failed", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
		}
	}()
}
