package service

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"aegis/internal/authz"
	"aegis/internal/config"
	admindomain "aegis/internal/domain/admin"
	plugindomain "aegis/internal/domain/plugin"
	securitydomain "aegis/internal/domain/security"
	systemdomain "aegis/internal/domain/system"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

type AdminService struct {
	cfg      config.Config
	log      *zap.Logger
	pg       *pgrepo.Repository
	sessions *redisrepo.SessionRepository
	// authz 是平台唯一的授权判定引擎（策略落库 + 跨实例同步 + 可解释）。
	// 它取代了原来挂在本结构体上的那个内存 Casbin enforcer。
	authz *authz.Engine
	// roles / customRoles 只是**展示用**的角色元数据（名称、层级、作用域），
	// 判定一律走 authz。两份数据分开是有意的：判定要的是策略，
	// 列表要的是"这个角色叫什么、排第几"，混在一起就会出现
	// 「改了展示名顺带把权限也改了」这类事故。
	roles       map[string]admindomain.RoleDefinition
	rolesMu     sync.RWMutex
	customRoles map[string]admindomain.RoleDefinition
	security    *SecurityService // 用于 MFA 验证（通过 SetSecurityService 注入，避免循环初始化）
	ldap        *LDAPService     // LDAP 认证（通过 SetLDAPService 注入）
	oidc        *OIDCService     // OIDC 认证（通过 SetOIDCService 注入）
	saml        *SAMLService     // SAML 认证（通过 SetSAMLService 注入）
	plugin      *PluginService   // 插件系统（通过 SetPluginService 注入）
	risk        *RiskService     // 风控服务（通过 SetRiskService 注入）
	// settings 平台设置（通过 SetPlatformSettings 注入）：自助注册开关与建应用配额存在这里
	settings *PlatformSettingsService
}

// SetRiskService 注入风控服务
func (s *AdminService) SetRiskService(risk *RiskService) {
	s.risk = risk
}

func NewAdminService(cfg config.Config, log *zap.Logger, pg *pgrepo.Repository, sessions *redisrepo.SessionRepository, engine *authz.Engine) (*AdminService, error) {
	if log == nil {
		log = zap.NewNop()
	}
	if engine == nil {
		return nil, fmt.Errorf("授权引擎未初始化")
	}
	return &AdminService{
		cfg:      cfg,
		log:      log,
		pg:       pg,
		sessions: sessions,
		authz:    engine,
		roles:    authz.BuiltinRoles(),
	}, nil
}

// Authz 暴露授权引擎，供管理端的策略维护与自检接口使用。
func (s *AdminService) Authz() *authz.Engine { return s.authz }

// SetSecurityService 注入 SecurityService（在 bootstrap 中调用，避免循环初始化）
func (s *AdminService) SetSecurityService(sec *SecurityService) {
	s.security = sec
}

// SetLDAPService 注入 LDAPService（在 bootstrap 中调用）
func (s *AdminService) SetLDAPService(ldap *LDAPService) {
	s.ldap = ldap
}

// SetOIDCService 注入 OIDCService（在 bootstrap 中调用）
func (s *AdminService) SetOIDCService(oidc *OIDCService) {
	s.oidc = oidc
}

// SetSAMLService 注入 SAMLService（在 bootstrap 中调用）
func (s *AdminService) SetSAMLService(saml *SAMLService) {
	s.saml = saml
}

// SetPluginService 注入 PluginService（在 bootstrap 中调用）
func (s *AdminService) SetPluginService(plugin *PluginService) {
	s.plugin = plugin
}

func (s *AdminService) EnsureBootstrapSuperAdmin(ctx context.Context) error {
	password := strings.TrimSpace(s.cfg.AdminBootstrap.Password)
	if password == "" {
		return nil
	}
	input := admindomain.CreateInput{
		Account:      strings.TrimSpace(s.cfg.AdminBootstrap.Account),
		Password:     password,
		DisplayName:  strings.TrimSpace(s.cfg.AdminBootstrap.DisplayName),
		Email:        strings.TrimSpace(s.cfg.AdminBootstrap.Email),
		IsSuperAdmin: true,
	}
	hash, err := adminHashPassword(input.Password)
	if err != nil {
		return err
	}
	profile, err := s.pg.UpsertBootstrapAdmin(ctx, input, hash)
	if err != nil {
		return err
	}
	if profile != nil {
		s.log.Info("bootstrap super admin ensured",
			zap.Int64("admin_id", profile.Account.ID),
			zap.String("account", profile.Account.Account),
		)
	}
	return nil
}

func (s *AdminService) Login(ctx context.Context, account, password, ip, userAgent string) (*admindomain.LoginResult, error) {
	account = strings.TrimSpace(account)

	// ── 风控评估：管理员登录 ──
	if s.risk != nil {
		riskResult, _ := s.risk.EvaluateRisk(ctx, securitydomain.RiskEvalRequest{
			Scene: "login", IP: ip, UserAgent: userAgent,
			Extra: map[string]any{"account": account},
		})
		if riskResult != nil && (riskResult.Action == "block" || riskResult.Action == "ban") {
			return nil, apperrors.New(40398, http.StatusForbidden, "当前请求被风控拦截")
		}
	}

	// ── 插件钩子：登录前检查 ──
	if s.plugin != nil {
		hookResult := s.plugin.ExecuteHook(ctx, HookAuthPreLogin, map[string]any{
			"account": account,
		}, plugindomain.HookMetadata{})
		if !hookResult.Allow {
			msg := hookResult.Message
			if msg == "" {
				msg = "插件拒绝了此登录请求"
			}
			return nil, apperrors.New(40399, http.StatusForbidden, msg)
		}
	}

	// ── LDAP 认证分支 ──
	if s.ldap != nil && s.ldap.IsReady() {
		ldapResult, ldapErr := s.tryLDAPAuth(ctx, account, password, ip, userAgent)
		if ldapResult != nil {
			return ldapResult, nil
		}
		if ldapErr != nil {
			if _, ok := ldapErr.(*apperrors.AppError); ok {
				return nil, ldapErr
			}
			s.log.Warn("LDAP 认证异常，尝试本地回退", zap.String("account", account), zap.Error(ldapErr))
			if !s.ldap.CurrentConfig().FallbackToLocal {
				return nil, apperrors.New(50192, http.StatusServiceUnavailable, "LDAP 服务不可用")
			}
		}
	}

	// ── 本地认证 ──
	record, err := s.pg.GetAdminAuthByAccount(ctx, account)
	if err != nil {
		return nil, err
	}
	if record == nil || !adminVerifyPassword(record.PasswordHash, password) {
		if s.plugin != nil {
			go s.plugin.ExecuteHook(context.Background(), HookAuthLoginFailed, map[string]any{"account": account}, plugindomain.HookMetadata{})
		}
		return nil, apperrors.New(40110, http.StatusUnauthorized, "管理员账号或密码错误")
	}
	if record.Account.Status != "active" {
		return nil, apperrors.New(40310, http.StatusForbidden, "管理员账户不可用")
	}

	return s.continueLoginWithMFA(ctx, record.Account.ID, record.Account.Account, ip, userAgent)
}

// continueLoginWithMFA 检查 MFA 后颁发会话（LDAP 和本地登录共用）
func (s *AdminService) continueLoginWithMFA(ctx context.Context, adminID int64, account, ip, userAgent string) (*admindomain.LoginResult, error) {
	totpRecord, _ := s.pg.GetAdminTOTPSecret(ctx, adminID)
	if totpRecord != nil && totpRecord.Enabled {
		challengeID := fmt.Sprintf("admin-mfa-%d-%d", adminID, timeutil.Now().UnixNano())
		methods := []string{"totp", "recovery_code"}
		challenge := securitydomain.LoginChallenge{
			ChallengeID: challengeID,
			UserID:      adminID,
			Account:     account,
			Methods:     methods,
			ExpiresAt:   timeutil.NowUTC().Add(5 * time.Minute),
			CreatedAt:   timeutil.NowUTC(),
		}
		if err := s.sessions.SetTwoFactorChallenge(ctx, challenge, 5*time.Minute); err != nil {
			return nil, err
		}
		return &admindomain.LoginResult{
			RequiresSecondFactor: true,
			Challenge: &admindomain.MFAChallenge{
				ChallengeID: challengeID,
				Methods:     methods,
				ExpiresAt:   challenge.ExpiresAt,
			},
		}, nil
	}

	profile, err := s.pg.GetAdminAccessByID(ctx, adminID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, apperrors.New(40450, http.StatusNotFound, "管理员不存在")
	}
	result, err := s.issueSession(ctx, profile, ip, userAgent)
	if err == nil && s.plugin != nil {
		go s.plugin.ExecuteHook(context.Background(), HookAuthSessionIssued, map[string]any{
			"adminId": adminID, "account": account,
		}, plugindomain.HookMetadata{AdminID: &adminID})
	}
	return result, err
}

// tryLDAPAuth LDAP 认证尝试
func (s *AdminService) tryLDAPAuth(ctx context.Context, account, password, ip, userAgent string) (*admindomain.LoginResult, error) {
	ldapUser, err := s.ldap.Authenticate(ctx, account, password)
	if err != nil {
		return nil, err
	}
	if ldapUser == nil {
		return nil, nil
	}

	// LDAP 认证成功 → 同步本地管理员
	localAccount, err := s.syncLDAPAdmin(ctx, ldapUser)
	if err != nil {
		return nil, err
	}
	if localAccount.Status != "active" {
		return nil, apperrors.New(40310, http.StatusForbidden, "管理员账户已被停用")
	}

	return s.continueLoginWithMFA(ctx, localAccount.ID, localAccount.Account, ip, userAgent)
}

// GetOIDCAuthURL 生成 OIDC 授权 URL 并缓存 state
func (s *AdminService) GetOIDCAuthURL(ctx context.Context) (string, string, error) {
	if s.oidc == nil || !s.oidc.IsEnabled() {
		return "", "", apperrors.New(40190, http.StatusBadRequest, "OIDC 认证未启用")
	}
	state := uuid.NewString()
	if err := s.sessions.SetOIDCState(ctx, state, 5*time.Minute); err != nil {
		return "", "", err
	}
	url, err := s.oidc.AuthURL(ctx, state)
	if err != nil {
		return "", "", err
	}
	return url, state, nil
}

// HandleOIDCCallback 处理 OIDC IdP 回调
func (s *AdminService) HandleOIDCCallback(ctx context.Context, code, state, ip, userAgent string) (*admindomain.LoginResult, error) {
	ok, err := s.sessions.GetAndDeleteOIDCState(ctx, state)
	if err != nil || !ok {
		return nil, apperrors.New(40195, http.StatusUnauthorized, "OIDC state 无效或已过期")
	}
	oidcUser, err := s.oidc.ExchangeAndVerify(ctx, code)
	if err != nil {
		if _, ok := err.(*apperrors.AppError); ok {
			return nil, err
		}
		s.log.Error("OIDC exchange/verify 失败", zap.Error(err))
		return nil, apperrors.New(40196, http.StatusUnauthorized, "OIDC 认证失败")
	}
	localAccount, err := s.syncExternalAdmin(ctx, oidcUser.Account, oidcUser.DisplayName, oidcUser.Email, oidcUser.Phone, "oidc")
	if err != nil {
		return nil, err
	}
	if localAccount.Status != "active" {
		return nil, apperrors.New(40310, http.StatusForbidden, "管理员账户已被停用")
	}
	return s.continueLoginWithMFA(ctx, localAccount.ID, localAccount.Account, ip, userAgent)
}

// GetSAMLAuthURL 生成 SAML AuthnRequest 重定向 URL。
func (s *AdminService) GetSAMLAuthURL(ctx context.Context) (string, string, error) {
	if s.saml == nil || !s.saml.IsEnabled() {
		return "", "", apperrors.New(40197, http.StatusBadRequest, "SAML 认证未启用")
	}
	relayState := uuid.NewString()
	url, requestID, err := s.saml.BuildAuthRedirect(ctx, relayState)
	if err != nil {
		return "", "", err
	}
	if err := s.sessions.SetSAMLState(ctx, relayState, requestID, 5*time.Minute); err != nil {
		return "", "", err
	}
	return url, relayState, nil
}

// HandleSAMLCallback 处理 SAML ACS 回调。
func (s *AdminService) HandleSAMLCallback(ctx context.Context, req *http.Request, ip, userAgent string) (*admindomain.LoginResult, error) {
	if s.saml == nil || !s.saml.IsEnabled() {
		return nil, apperrors.New(40197, http.StatusBadRequest, "SAML 认证未启用")
	}
	if err := req.ParseForm(); err != nil {
		return nil, apperrors.New(40097, http.StatusBadRequest, "SAML 回调参数解析失败")
	}
	relayState := strings.TrimSpace(req.FormValue("RelayState"))
	possibleRequestIDs := []string(nil)
	if relayState != "" {
		requestID, err := s.sessions.GetAndDeleteSAMLState(ctx, relayState)
		if err != nil {
			return nil, err
		}
		if requestID != "" {
			possibleRequestIDs = append(possibleRequestIDs, requestID)
		}
	}
	user, err := s.saml.ParseAndVerifyResponse(ctx, req, possibleRequestIDs)
	if err != nil {
		return nil, apperrors.New(40198, http.StatusUnauthorized, "SAML 认证失败")
	}
	localAccount, err := s.syncExternalAdmin(ctx, user.Account, user.DisplayName, user.Email, user.Phone, "saml")
	if err != nil {
		return nil, err
	}
	if localAccount.Status != "active" {
		return nil, apperrors.New(40310, http.StatusForbidden, "管理员账户已被停用")
	}
	return s.continueLoginWithMFA(ctx, localAccount.ID, localAccount.Account, ip, userAgent)
}

// syncExternalAdmin 同步外部认证用户到本地（LDAP/OIDC 共用）
func (s *AdminService) syncExternalAdmin(ctx context.Context, account, displayName, email, phone, authSource string) (*admindomain.Account, error) {
	record, err := s.pg.GetAdminAuthByAccount(ctx, account)
	if err != nil {
		return nil, err
	}
	if record != nil {
		_ = s.pg.UpdateAdminExternalSync(ctx, record.Account.ID, displayName, email, phone, authSource)
		record.Account.AuthSource = authSource
		return &record.Account, nil
	}
	profile, err := s.pg.CreateExternalAdminAccount(ctx, account, displayName, email, phone, authSource)
	if err != nil {
		return nil, err
	}
	s.log.Info("外部认证管理员自动创建", zap.String("account", account), zap.String("authSource", authSource), zap.Int64("id", profile.Account.ID))
	return &profile.Account, nil
}

// syncLDAPAdmin 同步 LDAP 用户到本地 admin_accounts
func (s *AdminService) syncLDAPAdmin(ctx context.Context, ldapUser *systemdomain.LDAPUser) (*admindomain.Account, error) {
	return s.syncExternalAdmin(ctx, ldapUser.Account, ldapUser.DisplayName, ldapUser.Email, ldapUser.Phone, "ldap")
}

// VerifyMFA 验证管理员 MFA 挑战（TOTP 或恢复码），成功后颁发会话 Token
func (s *AdminService) VerifyMFA(ctx context.Context, challengeID, code, recoveryCode, ip, userAgent string) (*admindomain.LoginResult, error) {
	challenge, err := s.sessions.GetTwoFactorChallenge(ctx, challengeID)
	if err != nil || challenge == nil {
		return nil, apperrors.New(40111, http.StatusUnauthorized, "MFA 挑战已过期或不存在")
	}
	if timeutil.NowUTC().After(challenge.ExpiresAt) {
		_ = s.sessions.DeleteTwoFactorChallenge(ctx, challengeID)
		return nil, apperrors.New(40112, http.StatusUnauthorized, "MFA 挑战已过期")
	}

	adminID := challenge.UserID

	// 委托给 SecurityService 验证（复用已有的 TOTP/恢复码验证逻辑）
	if s.security == nil {
		return nil, apperrors.New(50002, http.StatusInternalServerError, "安全服务未初始化")
	}
	if err := s.security.verifyAdminSecondFactor(ctx, adminID, strings.TrimSpace(code), strings.TrimSpace(recoveryCode)); err != nil {
		// 确保返回给前端的是可读的业务错误，而非内部堆栈
		if _, ok := err.(*apperrors.AppError); ok {
			return nil, err
		}
		return nil, apperrors.New(40114, http.StatusUnauthorized, "验证码或恢复码无效")
	}

	// 验证通过，删除挑战，颁发会话
	_ = s.sessions.DeleteTwoFactorChallenge(ctx, challengeID)

	profile, err := s.pg.GetAdminAccessByID(ctx, adminID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, apperrors.New(40450, http.StatusNotFound, "管理员不存在")
	}
	result, err := s.issueSession(ctx, profile, ip, userAgent)
	if err == nil && s.plugin != nil {
		go s.plugin.ExecuteHook(context.Background(), HookAuthMFAVerified, map[string]any{"adminId": adminID, "account": profile.Account.Account}, plugindomain.HookMetadata{AdminID: &adminID})
	}
	return result, err
}

func (s *AdminService) ValidateAccessToken(ctx context.Context, token string) (*admindomain.AccessContext, error) {
	// 静态令牌不再作为 API 访问令牌（仅用于 /api/admin/auth/emergency-login 端点）
	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (interface{}, error) {
		return []byte(s.cfg.JWT.Secret), nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(s.cfg.JWT.Issuer))
	if err != nil || !parsed.Valid {
		return nil, apperrors.New(40110, http.StatusUnauthorized, "管理员令牌无效")
	}
	if typ, _ := claims["typ"].(string); typ != "admin" {
		return nil, apperrors.New(40110, http.StatusUnauthorized, "管理员令牌无效")
	}
	tokenID, _ := claims["jti"].(string)
	blacklisted, err := s.sessions.IsBlacklisted(ctx, tokenID)
	if err != nil {
		return nil, err
	}
	if blacklisted {
		s.cleanupAdminSessionStateAsync(token, adminIDFromClaims(claims), tokenID)
		return nil, apperrors.New(40111, http.StatusUnauthorized, "管理员令牌已失效")
	}
	// 检查会话是否被管理端撤销
	revoked, _ := s.pg.IsSessionRevoked(ctx, tokenID)
	if revoked {
		s.cleanupAdminSessionStateAsync(token, adminIDFromClaims(claims), tokenID)
		return nil, apperrors.New(40115, http.StatusUnauthorized, "会话已被撤销")
	}
	session, err := s.sessions.GetAdminSession(ctx, token)
	if err != nil {
		return nil, err
	}
	if session == nil {
		s.cleanupAdminSessionStateAsync(token, adminIDFromClaims(claims), tokenID)
		return nil, apperrors.New(40112, http.StatusUnauthorized, "管理员会话不存在或已过期")
	}
	profile, err := s.pg.GetAdminAccessByID(ctx, session.AdminID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, apperrors.New(40450, http.StatusNotFound, "管理员不存在")
	}
	if profile.Account.Status != "active" {
		s.cleanupAdminSessionStateAsync(token, session.AdminID, session.TokenID)
		return nil, apperrors.New(40310, http.StatusForbidden, "管理员账户不可用")
	}
	s.touchAdminSessionAsync(session.AdminID, session.TokenID)
	return &admindomain.AccessContext{
		Session: admindomain.Session{
			AdminID:      profile.Account.ID,
			Account:      profile.Account.Account,
			DisplayName:  profile.Account.DisplayName,
			TokenID:      session.TokenID,
			IssuedAt:     session.IssuedAt,
			ExpiresAt:    session.ExpiresAt,
			IsSuperAdmin: profile.Account.IsSuperAdmin,
		},
		Assignments: profile.Assignments,
	}, nil
}

func (s *AdminService) Logout(ctx context.Context, token string) error {
	access, err := s.ValidateAccessToken(ctx, token)
	if err != nil {
		return err
	}
	_ = s.sessions.DeleteAdminSession(ctx, token)
	// 标记会话记录为已撤销 + 清除在线状态
	_ = s.pg.RevokeAdminSession(ctx, access.TokenID, access.AdminID)
	_ = s.sessions.RemoveAdminOnline(ctx, access.AdminID, access.TokenID)
	return s.sessions.BlacklistToken(ctx, access.TokenID, timeutil.Until(access.ExpiresAt))
}
func (s *AdminService) touchAdminSessionAsync(adminID int64, tokenID string) {
	if adminID <= 0 || strings.TrimSpace(tokenID) == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.pg.UpdateSessionLastActive(ctx, tokenID)
		if s.sessions != nil {
			_ = s.sessions.SetAdminOnline(ctx, adminID, tokenID)
		}
	}()
}

func (s *AdminService) cleanupAdminSessionStateAsync(token string, adminID int64, tokenID string) {
	if strings.TrimSpace(token) == "" && strings.TrimSpace(tokenID) == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if s.sessions != nil && strings.TrimSpace(token) != "" {
			_ = s.sessions.DeleteAdminSession(ctx, token)
		}
		if s.sessions != nil && adminID > 0 && strings.TrimSpace(tokenID) != "" {
			_ = s.sessions.RemoveAdminOnline(ctx, adminID, tokenID)
		}
	}()
}

func adminIDFromClaims(claims jwt.MapClaims) int64 {
	switch value := claims["aid"].(type) {
	case float64:
		return int64(value)
	case int64:
		return value
	case int:
		return int64(value)
	case string:
		id, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		return id
	default:
		return 0
	}
}

func (s *AdminService) ListAdmins(ctx context.Context) ([]admindomain.Profile, error) {
	return s.pg.ListAdminAccounts(ctx)
}

func (s *AdminService) GetProfile(ctx context.Context, adminID int64) (*admindomain.Profile, error) {
	profile, err := s.pg.GetAdminAccessByID(ctx, adminID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, apperrors.New(40450, http.StatusNotFound, "管理员不存在")
	}
	return profile, nil
}

func (s *AdminService) UpdateProfile(ctx context.Context, adminID int64, input admindomain.ProfileUpdate) (*admindomain.Profile, error) {
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Email = strings.TrimSpace(input.Email)
	input.Avatar = strings.TrimSpace(input.Avatar)
	return s.pg.UpdateAdminProfile(ctx, adminID, input)
}

func (s *AdminService) CreateAdmin(ctx context.Context, input admindomain.CreateInput) (*admindomain.Profile, error) {
	input.Account = strings.TrimSpace(input.Account)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Email = strings.TrimSpace(input.Email)
	if err := s.validateCreateInput(input); err != nil {
		return nil, err
	}
	hash, err := adminHashPassword(input.Password)
	if err != nil {
		return nil, err
	}
	input.Assignments, err = s.normalizeAssignments(input.Assignments, input.IsSuperAdmin)
	if err != nil {
		return nil, err
	}
	profile, err := s.pg.CreateAdminAccount(ctx, input, hash)
	if err == nil && profile != nil && s.plugin != nil {
		go s.plugin.ExecuteHook(context.Background(), HookAdminCreated, map[string]any{"adminId": profile.Account.ID, "account": input.Account}, plugindomain.HookMetadata{AdminID: &profile.Account.ID})
	}
	return profile, err
}

// RegisterAdmin 管理员自助注册（公开接口，无需认证）。
//
// 建出来的账号**一条角色分配都没有**，这是刻意的：注册是个匿名入口，
// 在这里发角色等于任何人都能给自己弄一份平台级授权。他能做的只有
// 「自助能力」那一档 —— 主要就是创建属于自己的第一个应用并成为它的管理员，
// 判定见 EnsureCanCreateApp。
func (s *AdminService) RegisterAdmin(ctx context.Context, account, password, displayName, email string) (*admindomain.Profile, error) {
	// 开关关闭时给一条说得清的 403。以前关掉这条路的做法是把路由摘掉，
	// 于是前端拿到 40400「请求的页面不存在」，看起来像接口地址写错了。
	if !s.RegistrationEnabled(ctx) {
		return nil, apperrors.New(40317, http.StatusForbidden,
			"平台已关闭管理员自助注册。请联系超级管理员为你开通账号。")
	}
	input := admindomain.CreateInput{
		Account:      strings.TrimSpace(account),
		Password:     password,
		DisplayName:  strings.TrimSpace(displayName),
		Email:        strings.TrimSpace(email),
		IsSuperAdmin: false,
		Assignments:  nil,
	}
	if err := s.validateCreateInput(input); err != nil {
		return nil, err
	}
	hash, err := adminHashPassword(input.Password)
	if err != nil {
		return nil, err
	}
	return s.pg.CreateAdminAccount(ctx, input, hash)
}

// AutoAssignAppRole 为管理员补一条应用级角色分配。
//
// 创建应用时的登记已经并进 AppService.CreateApp 的事务里了（见
// Repository.CreateAppOwnedBy），这里只剩「事后补授」这一种用途，
// 因此它会校验角色确实是应用级的 —— 传进一个全局角色会把 appID 静默丢掉，
// 表现为一次无声的提权。
func (s *AdminService) AutoAssignAppRole(ctx context.Context, adminID, appID int64, roleKey string) error {
	if adminID <= 0 || appID <= 0 {
		return apperrors.New(40058, http.StatusBadRequest, "缺少有效的管理员或应用标识")
	}
	roleKey = strings.TrimSpace(roleKey)
	role, ok := s.roles[roleKey]
	if !ok {
		s.rolesMu.RLock()
		role, ok = s.customRoles[roleKey]
		s.rolesMu.RUnlock()
	}
	if !ok || role.Scope != "app" {
		return apperrors.New(40053, http.StatusBadRequest, "只能分配应用级角色")
	}
	return s.pg.AddAdminAssignment(ctx, adminID, roleKey, &appID)
}

func (s *AdminService) UpdateAdminStatus(ctx context.Context, actorID int64, adminID int64, status string) error {
	status = strings.TrimSpace(strings.ToLower(status))
	if status != "active" && status != "disabled" {
		return apperrors.New(40050, http.StatusBadRequest, "无效的管理员状态")
	}

	// 停用操作需要安全检查
	if status == "disabled" {
		// 禁止停用自身
		if actorID == adminID {
			return apperrors.New(40392, http.StatusForbidden, "无法停用自身账户")
		}

		target, err := s.pg.GetAdminAccessByID(ctx, adminID)
		if err != nil {
			return err
		}
		if target == nil {
			return apperrors.New(40450, http.StatusNotFound, "管理员不存在")
		}

		// 超级管理员停用保护：确保系统中至少保留一个活跃超级管理员
		if target.Account.IsSuperAdmin {
			activeSuperCount, err := s.pg.CountActiveSuperAdmins(ctx)
			if err != nil {
				return err
			}
			if activeSuperCount <= 1 {
				return apperrors.New(40391, http.StatusForbidden, "无法停用最后一个超级管理员，系统需要至少一个活跃的超级管理员")
			}
		}
	}

	if err := s.pg.UpdateAdminStatus(ctx, adminID, status); err != nil {
		return err
	}
	if s.plugin != nil {
		go s.plugin.ExecuteHook(context.Background(), HookAdminStatusChanged, map[string]any{"adminId": adminID, "status": status}, plugindomain.HookMetadata{AdminID: &adminID})
	}
	return nil
}

func (s *AdminService) UpdateAdminAccess(ctx context.Context, adminID int64, input admindomain.UpdateAccessInput) error {
	assignments, err := s.normalizeAssignments(input.Assignments, input.IsSuperAdmin)
	if err != nil {
		return err
	}
	input.Assignments = assignments
	if err := s.pg.UpdateAdminAccess(ctx, adminID, input); err != nil {
		return err
	}
	if s.plugin != nil {
		go s.plugin.ExecuteHook(context.Background(), HookAdminAccessUpdated, map[string]any{"adminId": adminID}, plugindomain.HookMetadata{AdminID: &adminID})
	}
	return nil
}

// ListRoles 返回全部角色，`Permissions` 是**展开后的生效集合**。
//
// 展开是必须的：角色定义里现在可以写前缀通配（`content:*`）、可以继承父角色、
// 还可能被一条 override 的 deny 砍掉一项。直接把定义里的字符串发出去，
// 权限矩阵会把 `content:*` 当成一个查无此项的权限点，于是
// app_admin 在矩阵里看起来几乎什么都没有 —— 而它其实什么都有。
//
// 展开走的是与真实判定完全相同的那段代码，因此矩阵上打勾的每一项，
// 点下去一定不会 403。
func (s *AdminService) ListRoles() []admindomain.RoleDefinition {
	items := make([]admindomain.RoleDefinition, 0, len(s.roles)+len(s.customRoles))
	for _, item := range s.roles {
		items = append(items, item)
	}
	s.rolesMu.RLock()
	for _, item := range s.customRoles {
		items = append(items, item)
	}
	s.rolesMu.RUnlock()
	catalog := authz.AllPermissionCodes()
	for i := range items {
		// 用应用域展开：应用级角色的权限只在应用域下成立，
		// 用平台域展开会让它们看起来一个权限都没有。
		items[i].Permissions = s.authz.PermissionsFor(
			[]string{authz.RoleSubject(items[i].Key)}, authz.AppDomain(1), catalog)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Level == items[j].Level {
			return items[i].Key < items[j].Key
		}
		return items[i].Level > items[j].Level
	})
	return items
}

// ListRolesWithPermissionTree 返回所有角色及其权限树
func (s *AdminService) ListRolesWithPermissionTree() []admindomain.RoleWithPermissions {
	roles := s.ListRoles()
	allGroups := allPermissionGroups()
	result := make([]admindomain.RoleWithPermissions, 0, len(roles))
	for _, role := range roles {
		// role.Permissions 已由 ListRoles 展开成生效集合（含通配展开、继承与拒绝），
		// 因此这里不再需要"超管特判"那一行 —— 它的 `*` 策略会如实展开成全部权限点。
		granted := make(map[string]bool, len(role.Permissions))
		for _, p := range role.Permissions {
			granted[p] = true
		}

		groups := make([]admindomain.PermissionGroup, 0, len(allGroups))
		for _, g := range allGroups {
			perms := make([]admindomain.Permission, len(g.Permissions))
			copy(perms, g.Permissions)
			groups = append(groups, admindomain.PermissionGroup{
				Key: g.Key, Name: g.Name, Permissions: perms,
			})
			// 标记哪些权限是授权的（通过 Description 字段传递，前端读取）
			for i := range groups[len(groups)-1].Permissions {
				if granted[groups[len(groups)-1].Permissions[i].Code] {
					groups[len(groups)-1].Permissions[i].Description = "granted"
				}
			}
		}
		result = append(result, admindomain.RoleWithPermissions{
			RoleDefinition:   role,
			PermissionGroups: groups,
		})
	}
	return result
}

// 权限点常量的兼容别名。唯一事实源是 internal/authz 的权限目录 ——
// 常量散落在业务包里正是"加一个权限点要改三个包"的成因。
const (
	PermissionPlatformAppRead      = authz.PermPlatformAppRead
	PermissionPlatformAppGovern    = authz.PermPlatformAppGovern
	PermissionPlatformAppDanger    = authz.PermPlatformAppDanger
	PermissionPlatformAppealReview = authz.PermPlatformAppealReview
	PermissionPlatformStorageRead  = authz.PermPlatformStorageRead
	PermissionPlatformStorageWrite = authz.PermPlatformStorageWrite
)

// allPermissionGroups 返回所有权限分组定义（唯一事实源在 internal/authz）。
func allPermissionGroups() []admindomain.PermissionGroup { return authz.PermissionCatalog() }

func (s *AdminService) validateCreateInput(input admindomain.CreateInput) error {
	input.Account = strings.TrimSpace(input.Account)
	if input.Account == "" {
		return apperrors.New(40051, http.StatusBadRequest, "管理员账号不能为空")
	}
	if len(input.Account) < 3 || len(input.Account) > 64 {
		return apperrors.New(40052, http.StatusBadRequest, "管理员账号长度必须在 3 到 64 个字符之间")
	}
	if err := validateAdminPassword(input.Password); err != nil {
		return err
	}
	return nil
}

func (s *AdminService) normalizeAssignments(assignments []admindomain.AssignmentMutation, isSuperAdmin bool) ([]admindomain.AssignmentMutation, error) {
	if isSuperAdmin {
		return nil, nil
	}
	items := make([]admindomain.AssignmentMutation, 0, len(assignments))
	seen := map[string]struct{}{}
	for _, item := range assignments {
		roleKey := strings.TrimSpace(item.RoleKey)
		role, ok := s.roles[roleKey]
		if !ok || roleKey == "super_admin" {
			return nil, apperrors.New(40053, http.StatusBadRequest, "包含无效的管理员角色")
		}
		if role.Scope == "app" && item.AppID == nil {
			return nil, apperrors.New(40054, http.StatusBadRequest, "应用级角色必须绑定应用")
		}
		if role.Scope == "global" {
			item.AppID = nil
		}
		key := roleKey + ":*"
		if item.AppID != nil {
			key = roleKey + ":" + strconvInt64(*item.AppID)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		items = append(items, admindomain.AssignmentMutation{RoleKey: roleKey, AppID: item.AppID})
	}
	if len(items) == 0 {
		return nil, apperrors.New(40055, http.StatusBadRequest, "至少需要一个管理员角色")
	}
	return items, nil
}

func (s *AdminService) issueSession(ctx context.Context, profile *admindomain.Profile, ip, userAgent string) (*admindomain.LoginResult, error) {
	now := timeutil.NowUTC()
	expiresAt := now.Add(s.cfg.AdminSessionTTL)
	tokenID := uuid.NewString()
	claims := jwt.MapClaims{
		"aid":     profile.Account.ID,
		"account": profile.Account.Account,
		"super":   profile.Account.IsSuperAdmin,
		"typ":     "admin",
		"jti":     tokenID,
		"iss":     s.cfg.JWT.Issuer,
		"iat":     now.Unix(),
		"exp":     expiresAt.Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(s.cfg.JWT.Secret))
	if err != nil {
		return nil, err
	}
	session := admindomain.Session{
		AdminID:      profile.Account.ID,
		Account:      profile.Account.Account,
		DisplayName:  profile.Account.DisplayName,
		TokenID:      tokenID,
		IssuedAt:     now,
		ExpiresAt:    expiresAt,
		IsSuperAdmin: profile.Account.IsSuperAdmin,
	}
	if err := s.sessions.SetAdminSession(ctx, signed, session, timeutil.Until(expiresAt)); err != nil {
		return nil, err
	}
	_ = s.pg.UpdateAdminLastLogin(ctx, profile.Account.ID, now)
	// 写入会话持久化记录
	_ = s.pg.CreateAdminSessionRecord(ctx, admindomain.AdminSessionRecord{
		ID: tokenID, AdminID: profile.Account.ID,
		IP: ip, UserAgent: userAgent,
		IssuedAt: now, ExpiresAt: expiresAt,
	})
	// 标记在线状态
	_ = s.sessions.SetAdminOnline(ctx, profile.Account.ID, tokenID)
	return &admindomain.LoginResult{
		AccessToken: signed,
		ExpiresAt:   expiresAt,
		TokenType:   "Bearer",
		Admin:       profile.Account,
		Assignments: profile.Assignments,
	}, nil
}

// 内置角色与权限目录已迁往 internal/authz —— 权限词汇、角色定义、路由映射、
// 判定引擎同属一个包，彼此的一致性由那个包的测试守住。此处只保留薄封装，
// 让既有调用方不必一次性全改。

func builtInAdminRoles() map[string]admindomain.RoleDefinition { return authz.BuiltinRoles() }

func scopeMatches(assignmentAppID *int64, requestAppID *int64) bool {
	if requestAppID == nil {
		return assignmentAppID == nil
	}
	if assignmentAppID == nil {
		return true
	}
	return *assignmentAppID == *requestAppID
}

func adminVerifyPassword(hash, password string) bool {
	if hash == "" {
		return false
	}
	if strings.HasPrefix(hash, "$2a$") || strings.HasPrefix(hash, "$2b$") || strings.HasPrefix(hash, "$2y$") {
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	}
	return subtle.ConstantTimeCompare([]byte(hash), []byte(password)) == 1
}

func adminHashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func validateAdminPassword(password string) error {
	password = strings.TrimSpace(password)
	if len(password) < 8 {
		return apperrors.New(40056, http.StatusBadRequest, "管理员密码长度不能少于 8 位")
	}
	if len(password) > 72 {
		return apperrors.New(40057, http.StatusBadRequest, "管理员密码长度不能超过 72 位")
	}
	return nil
}

func strconvInt64(value int64) string {
	return strconv.FormatInt(value, 10)
}

// ── 自定义角色 CRUD ──

// LoadCustomRoles 装载自定义角色的展示元数据，并把它们的权限同步进授权引擎。
//
// 名字沿用旧的（调用点在 bootstrap），但做的事变了：策略本身现在存在
// authz_policies 表里，引擎自己会装载。这里只负责两件事 ——
// 把角色的展示元数据缓进内存供列表用，以及把 admin_roles/admin_role_permissions
// 这份"角色编辑器写的东西"投影成策略。
func (s *AdminService) LoadCustomRoles(ctx context.Context) error {
	return s.syncCustomRoles(ctx, true)
}

// syncCustomRoles 重建自定义角色的展示缓存；syncPolicies 为真时一并回写策略。
func (s *AdminService) syncCustomRoles(ctx context.Context, syncPolicies bool) error {
	customRoles, err := s.pg.ListCustomRoles(ctx)
	if err != nil {
		return err
	}
	customMap := make(map[string]admindomain.RoleDefinition, len(customRoles))
	for _, cr := range customRoles {
		customMap[cr.RoleKey] = admindomain.RoleDefinition{
			Key: cr.RoleKey, Name: cr.Name, Description: cr.Description,
			Level: cr.Level, Scope: cr.Scope, Permissions: cr.Permissions,
			IsCustom: true, BaseRole: cr.BaseRole, CreatedBy: cr.CreatedBy,
		}
		if !syncPolicies {
			continue
		}
		if err := s.writeCustomRolePolicy(ctx, cr, nil); err != nil {
			return err
		}
	}
	s.rolesMu.Lock()
	s.customRoles = customMap
	s.rolesMu.Unlock()
	s.log.Info("自定义角色已装载", zap.Int("customRoles", len(customMap)))
	return nil
}

// writeCustomRolePolicy 把一个自定义角色写成策略。
//
// base_role 终于是**执行点**而不只是一根装饰线：它落成一条角色继承边，
// 于是父角色加一个权限点，所有继承它的自定义角色跟着有。
// 此前这一列只被拿去画关系图，判定完全不看 —— 控制台上标着
// 「继承自应用运营管理员」的角色，实际一个继承来的权限都没有。
func (s *AdminService) writeCustomRolePolicy(ctx context.Context, role admindomain.CustomRole, updatedBy *int64) error {
	policy := authz.RolePolicy{Allow: role.Permissions, Note: role.Name}
	if base := strings.TrimSpace(role.BaseRole); base != "" && base != role.RoleKey {
		policy.Inherits = []string{authz.RoleSubject(base)}
	}
	return s.authz.SetRolePolicy(ctx, authz.SourceCustom, authz.RoleSubject(role.RoleKey), policy, updatedBy)
}

func (s *AdminService) CreateCustomRole(ctx context.Context, input admindomain.CreateCustomRoleInput, createdBy int64) (*admindomain.CustomRole, error) {
	if !strings.HasPrefix(input.RoleKey, "custom_") {
		return nil, apperrors.New(40058, http.StatusBadRequest, "自定义角色标识必须以 custom_ 开头")
	}
	if input.Level < 1 || input.Level >= 20 {
		return nil, apperrors.New(40059, http.StatusBadRequest, "自定义角色级别必须在 1-19 之间")
	}
	if _, ok := s.roles[input.RoleKey]; ok {
		return nil, apperrors.New(40960, http.StatusConflict, "角色标识与内置角色冲突")
	}
	allPerms := s.allPermissionCodes()
	for _, p := range input.Permissions {
		if _, ok := allPerms[p]; !ok {
			return nil, apperrors.New(40060, http.StatusBadRequest, fmt.Sprintf("权限代码不存在: %s", p))
		}
	}

	cr, err := s.pg.CreateCustomRole(ctx, input, createdBy)
	if err != nil {
		return nil, err
	}
	// 策略写入失败必须**上抛**：角色已经建出来了却没有任何策略，
	// 表现是"角色在列表里、勾了一堆权限、授给谁都不生效"。
	// 旧实现这里只记一行 error 就返回成功，于是这种半成品状态没有任何人会发现。
	if err := s.writeCustomRolePolicy(ctx, *cr, &createdBy); err != nil {
		return nil, err
	}
	if err := s.syncCustomRoles(ctx, false); err != nil {
		s.log.Warn("自定义角色元数据刷新失败", zap.Error(err))
	}
	return cr, nil
}

func (s *AdminService) UpdateCustomRole(ctx context.Context, roleKey string, input admindomain.UpdateCustomRoleInput) (*admindomain.CustomRole, error) {
	if !strings.HasPrefix(roleKey, "custom_") {
		return nil, apperrors.New(40061, http.StatusBadRequest, "仅可编辑自定义角色")
	}
	if input.Level < 1 || input.Level >= 20 {
		return nil, apperrors.New(40059, http.StatusBadRequest, "自定义角色级别必须在 1-19 之间")
	}
	allPerms := s.allPermissionCodes()
	for _, p := range input.Permissions {
		if _, ok := allPerms[p]; !ok {
			return nil, apperrors.New(40060, http.StatusBadRequest, fmt.Sprintf("权限代码不存在: %s", p))
		}
	}

	cr, err := s.pg.UpdateCustomRole(ctx, roleKey, input)
	if err != nil {
		return nil, err
	}
	if err := s.writeCustomRolePolicy(ctx, *cr, nil); err != nil {
		return nil, err
	}
	if err := s.syncCustomRoles(ctx, false); err != nil {
		s.log.Warn("自定义角色元数据刷新失败", zap.Error(err))
	}
	return cr, nil
}

func (s *AdminService) DeleteCustomRole(ctx context.Context, roleKey string, force bool) error {
	if !strings.HasPrefix(roleKey, "custom_") {
		return apperrors.New(40061, http.StatusBadRequest, "仅可删除自定义角色")
	}
	count, err := s.pg.CountAdminsByRoleKey(ctx, roleKey)
	if err != nil {
		return err
	}
	if count > 0 && !force {
		return apperrors.New(40962, http.StatusConflict, fmt.Sprintf("该角色正在被 %d 位管理员使用，请先移除分配或使用强制删除", count))
	}
	if err := s.pg.DeleteCustomRole(ctx, roleKey); err != nil {
		return err
	}
	if err := s.authz.RemoveRolePolicy(ctx, authz.SourceCustom, authz.RoleSubject(roleKey)); err != nil {
		return err
	}
	if err := s.syncCustomRoles(ctx, false); err != nil {
		s.log.Warn("自定义角色元数据刷新失败", zap.Error(err))
	}
	return nil
}

func (s *AdminService) GetRoleImpactPreview(ctx context.Context, roleKey string) (*admindomain.ImpactPreview, error) {
	admins, err := s.pg.ListAdminsByRoleKey(ctx, roleKey)
	if err != nil {
		return nil, err
	}
	return &admindomain.ImpactPreview{AffectedAdmins: admins, TotalAffected: len(admins)}, nil
}

// ── 权限矩阵 + 角色关系图 ──

func (s *AdminService) GetRoleMatrix() admindomain.RoleMatrix {
	roles := s.ListRoles()
	groups := allPermissionGroups()
	permSets := make(map[string]map[string]bool, len(roles))
	for _, r := range roles {
		m := make(map[string]bool, len(r.Permissions))
		for _, p := range r.Permissions {
			m[p] = true
		}
		permSets[r.Key] = m
	}

	var rows []admindomain.RoleMatrixRow
	for _, g := range groups {
		for _, p := range g.Permissions {
			grants := make(map[string]bool, len(roles))
			for _, r := range roles {
				grants[r.Key] = permSets[r.Key][p.Code]
			}
			rows = append(rows, admindomain.RoleMatrixRow{
				PermissionCode: p.Code, PermissionName: p.Name,
				GroupKey: g.Key, GroupName: g.Name, Grants: grants,
			})
		}
	}
	return admindomain.RoleMatrix{Roles: roles, Groups: groups, Rows: rows}
}

func (s *AdminService) GetRoleGraph() admindomain.RoleGraph {
	roles := s.ListRoles()
	permSets := make(map[string]map[string]bool, len(roles))
	for _, r := range roles {
		m := make(map[string]bool, len(r.Permissions))
		for _, p := range r.Permissions {
			m[p] = true
		}
		permSets[r.Key] = m
	}

	var nodes []admindomain.RoleGraphNode
	for _, r := range roles {
		nodes = append(nodes, admindomain.RoleGraphNode{
			Key: r.Key, Name: r.Name, Level: r.Level,
			Scope: r.Scope, IsCustom: r.IsCustom, PermCount: len(r.Permissions),
		})
	}

	var edges []admindomain.RoleGraphEdge
	// BaseRole 继承边
	for _, r := range roles {
		if r.BaseRole != "" {
			edges = append(edges, admindomain.RoleGraphEdge{Source: r.BaseRole, Target: r.Key, Relation: "inherits"})
		}
	}
	// 权限包含关系边（A 是 B 的超集且 A.level > B.level）
	for i, a := range roles {
		for j, b := range roles {
			if i == j || a.Level <= b.Level {
				continue
			}
			if isSuperset(permSets[a.Key], permSets[b.Key]) && len(permSets[a.Key]) > len(permSets[b.Key]) {
				// 只保留直接包含（排除传递关系）
				direct := true
				for k, c := range roles {
					if k == i || k == j || c.Level <= b.Level || c.Level >= a.Level {
						continue
					}
					if isSuperset(permSets[a.Key], permSets[c.Key]) && isSuperset(permSets[c.Key], permSets[b.Key]) {
						direct = false
						break
					}
				}
				if direct {
					edges = append(edges, admindomain.RoleGraphEdge{Source: a.Key, Target: b.Key, Relation: "includes"})
				}
			}
		}
	}
	return admindomain.RoleGraph{Nodes: nodes, Edges: edges}
}

func isSuperset(a, b map[string]bool) bool {
	for k := range b {
		if !a[k] {
			return false
		}
	}
	return true
}

func (s *AdminService) allPermissionCodes() map[string]bool {
	groups := allPermissionGroups()
	result := make(map[string]bool)
	for _, g := range groups {
		for _, p := range g.Permissions {
			result[p.Code] = true
		}
	}
	return result
}
