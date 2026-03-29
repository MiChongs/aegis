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

	"aegis/internal/config"
	admindomain "aegis/internal/domain/admin"
	plugindomain "aegis/internal/domain/plugin"
	securitydomain "aegis/internal/domain/security"
	systemdomain "aegis/internal/domain/system"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

type AdminService struct {
	cfg         config.Config
	log         *zap.Logger
	pg          *pgrepo.Repository
	sessions    *redisrepo.SessionRepository
	enforcer    *casbin.Enforcer
	enforcerMu  sync.RWMutex
	roles       map[string]admindomain.RoleDefinition
	customRoles map[string]admindomain.RoleDefinition
	security    *SecurityService // 用于 MFA 验证（通过 SetSecurityService 注入，避免循环初始化）
	ldap        *LDAPService     // LDAP 认证（通过 SetLDAPService 注入）
	oidc        *OIDCService     // OIDC 认证（通过 SetOIDCService 注入）
	saml        *SAMLService     // SAML 认证（通过 SetSAMLService 注入）
	plugin      *PluginService   // 插件系统（通过 SetPluginService 注入）
	risk        *RiskService     // 风控服务（通过 SetRiskService 注入）
}

// SetRiskService 注入风控服务
func (s *AdminService) SetRiskService(risk *RiskService) {
	s.risk = risk
}

func NewAdminService(cfg config.Config, log *zap.Logger, pg *pgrepo.Repository, sessions *redisrepo.SessionRepository) (*AdminService, error) {
	if log == nil {
		log = zap.NewNop()
	}
	enforcer, err := newAdminEnforcer()
	if err != nil {
		return nil, err
	}
	return &AdminService{
		cfg:      cfg,
		log:      log,
		pg:       pg,
		sessions: sessions,
		enforcer: enforcer,
		roles:    builtInAdminRoles(),
	}, nil
}

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

func (s *AdminService) Authorize(ctx context.Context, access *admindomain.AccessContext, permission string, appID *int64) error {
	if access == nil {
		return apperrors.New(40110, http.StatusUnauthorized, "管理员未认证")
	}
	if access.IsSuperAdmin {
		return nil
	}
	if permission == "" {
		return nil
	}
	s.enforcerMu.RLock()
	defer s.enforcerMu.RUnlock()
	for _, assignment := range access.Assignments {
		if !scopeMatches(assignment.AppID, appID) {
			continue
		}
		allowed, err := s.enforcer.Enforce(assignment.RoleKey, permission)
		if err != nil {
			return err
		}
		if allowed {
			return nil
		}
	}
	// Casbin 拒绝后，检查临时权限
	tempPerms, _ := s.pg.GetActiveTempPermissions(ctx, access.AdminID)
	for _, tp := range tempPerms {
		if tp == permission {
			return nil
		}
	}
	return apperrors.New(40311, http.StatusForbidden, "当前管理员无权执行此操作")
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

// RegisterAdmin 管理员自助注册（公开接口，无需认证）
// 注册后无角色分配，创建应用时自动获得 app_admin 权限
func (s *AdminService) RegisterAdmin(ctx context.Context, account, password, displayName, email string) (*admindomain.Profile, error) {
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

// AutoAssignAppRole 自动为管理员分配应用角色（创建应用时调用）
func (s *AdminService) AutoAssignAppRole(ctx context.Context, adminID, appID int64, roleKey string) error {
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

func (s *AdminService) ListRoles() []admindomain.RoleDefinition {
	items := make([]admindomain.RoleDefinition, 0, len(s.roles)+len(s.customRoles))
	for _, item := range s.roles {
		items = append(items, item)
	}
	s.enforcerMu.RLock()
	for _, item := range s.customRoles {
		items = append(items, item)
	}
	s.enforcerMu.RUnlock()
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
		granted := make(map[string]bool, len(role.Permissions))
		for _, p := range role.Permissions {
			granted[p] = true
		}
		// 超级管理员拥有全部权限
		isSuperAdmin := role.Key == "super_admin"

		groups := make([]admindomain.PermissionGroup, 0, len(allGroups))
		for _, g := range allGroups {
			perms := make([]admindomain.Permission, len(g.Permissions))
			copy(perms, g.Permissions)
			groups = append(groups, admindomain.PermissionGroup{
				Key: g.Key, Name: g.Name, Permissions: perms,
			})
			// 标记哪些权限是授权的（通过 Description 字段传递，前端读取）
			for i := range groups[len(groups)-1].Permissions {
				if isSuperAdmin || granted[groups[len(groups)-1].Permissions[i].Code] {
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

// allPermissionGroups 返回所有权限分组定义
func allPermissionGroups() []admindomain.PermissionGroup {
	return []admindomain.PermissionGroup{
		{Key: "system", Name: "系统管理", Permissions: []admindomain.Permission{
			{Code: "system:admin:manage", Name: "管理员管理"},
			{Code: "system:settings:read", Name: "系统设置查看"},
			{Code: "system:settings:write", Name: "系统设置修改"},
			{Code: "system:user_setting:read", Name: "用户设置查看"},
			{Code: "system:user_setting:write", Name: "用户设置修改"},
		}},
		{Key: "app", Name: "应用管理", Permissions: []admindomain.Permission{
			{Code: "app:read", Name: "应用信息查看"},
			{Code: "app:write", Name: "应用信息修改"},
			{Code: "app:user:read", Name: "应用用户查看"},
			{Code: "app:user:write", Name: "应用用户管理"},
			{Code: "app:notification:read", Name: "通知查看"},
			{Code: "app:notification:write", Name: "通知管理"},
		}},
		{Key: "content", Name: "内容管理", Permissions: []admindomain.Permission{
			{Code: "content:banner:read", Name: "Banner 查看"},
			{Code: "content:banner:write", Name: "Banner 管理"},
			{Code: "content:notice:read", Name: "公告查看"},
			{Code: "content:notice:write", Name: "公告管理"},
		}},
		{Key: "audit", Name: "审计日志", Permissions: []admindomain.Permission{
			{Code: "audit:login:read", Name: "登录审计查看"},
			{Code: "audit:session:read", Name: "会话审计查看"},
		}},
		{Key: "storage", Name: "存储管理", Permissions: []admindomain.Permission{
			{Code: "storage:read", Name: "存储配置查看"},
			{Code: "storage:write", Name: "存储配置修改"},
		}},
		{Key: "workflow", Name: "工作流", Permissions: []admindomain.Permission{
			{Code: "workflow:read", Name: "工作流查看"},
			{Code: "workflow:write", Name: "工作流管理"},
		}},
		{Key: "version", Name: "版本管理", Permissions: []admindomain.Permission{
			{Code: "version:read", Name: "版本查看"},
			{Code: "version:write", Name: "版本管理"},
		}},
		{Key: "site", Name: "站点管理", Permissions: []admindomain.Permission{
			{Code: "site:read", Name: "站点查看"},
			{Code: "site:write", Name: "站点管理"},
			{Code: "site:audit", Name: "站点审核"},
		}},
		{Key: "role_application", Name: "角色申请", Permissions: []admindomain.Permission{
			{Code: "role_application:read", Name: "申请查看"},
			{Code: "role_application:review", Name: "申请审批"},
		}},
		{Key: "points", Name: "积分管理", Permissions: []admindomain.Permission{
			{Code: "points:read", Name: "积分查看"},
			{Code: "points:write", Name: "积分调整"},
		}},
		{Key: "email", Name: "邮件服务", Permissions: []admindomain.Permission{
			{Code: "email:read", Name: "邮件配置查看"},
			{Code: "email:write", Name: "邮件配置修改"},
		}},
		{Key: "payment", Name: "支付管理", Permissions: []admindomain.Permission{
			{Code: "payment:read", Name: "支付配置查看"},
			{Code: "payment:write", Name: "支付配置修改"},
		}},
		{Key: "org", Name: "组织架构", Permissions: []admindomain.Permission{
			{Code: "org:create", Name: "创建组织"},
			{Code: "org:write", Name: "修改/删除组织"},
			{Code: "org:dept:read", Name: "查看部门"},
			{Code: "org:dept:write", Name: "管理部门"},
			{Code: "org:member:read", Name: "查看成员"},
			{Code: "org:member:write", Name: "管理成员"},
			{Code: "org:member:invite", Name: "邀请成员"},
		}},
	}
}

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

func newAdminEnforcer() (*casbin.Enforcer, error) {
	m, err := model.NewModelFromString(`
[request_definition]
r = sub, obj

[policy_definition]
p = sub, obj

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub) && r.obj == p.obj
`)
	if err != nil {
		return nil, err
	}
	e, err := casbin.NewEnforcer(m)
	if err != nil {
		return nil, err
	}
	for _, role := range builtInAdminRoles() {
		for _, permission := range role.Permissions {
			if _, err := e.AddPermissionForUser(role.Key, permission); err != nil {
				return nil, err
			}
		}
	}
	return e, nil
}

func builtInAdminRoles() map[string]admindomain.RoleDefinition {
	return map[string]admindomain.RoleDefinition{
		"super_admin": {
			Key:         "super_admin",
			Name:        "超级管理员",
			Description: "平台最高管理权限",
			Level:       100,
			Scope:       "global",
			Permissions: []string{},
		},
		"platform_admin": {
			Key:         "platform_admin",
			Name:        "平台管理员",
			Description: "全局平台与运维配置管理",
			Level:       90,
			Scope:       "global",
			Permissions: []string{
				"system:settings:read", "system:settings:write", "system:user_setting:read", "system:user_setting:write",
				"app:read", "app:write", "app:user:read", "app:user:write", "app:notification:read", "app:notification:write",
				"content:banner:read", "content:banner:write", "content:notice:read", "content:notice:write",
				"audit:login:read", "audit:session:read",
				"storage:read", "storage:write",
				"workflow:read", "workflow:write",
				"version:read", "version:write",
				"site:read", "site:write", "site:audit",
				"role_application:read", "role_application:review",
				"points:read", "points:write",
				"email:read", "email:write",
				"payment:read", "payment:write",
				"org:create", "org:write", "org:dept:read", "org:dept:write", "org:member:read", "org:member:write", "org:member:invite",
			},
		},
		"app_admin": {
			Key:         "app_admin",
			Name:        "应用管理员",
			Description: "单应用全量管理权限（与平台管理员同级，仅限绑定应用）",
			Level:       70,
			Scope:       "app",
			Permissions: []string{
				"system:settings:read", "system:user_setting:read", "system:user_setting:write",
				"app:read", "app:write", "app:user:read", "app:user:write", "app:notification:read", "app:notification:write",
				"content:banner:read", "content:banner:write", "content:notice:read", "content:notice:write",
				"audit:login:read", "audit:session:read",
				"storage:read", "storage:write",
				"workflow:read", "workflow:write",
				"version:read", "version:write",
				"site:read", "site:write", "site:audit",
				"role_application:read", "role_application:review",
				"points:read", "points:write",
				"email:read", "email:write",
				"payment:read", "payment:write",
				"org:dept:read", "org:member:read", "org:member:invite",
			},
		},
		"app_operator": {
			Key:         "app_operator",
			Name:        "应用运营管理员",
			Description: "运营、内容、用户与版本维护",
			Level:       60,
			Scope:       "app",
			Permissions: []string{
				"app:read", "app:user:read", "app:user:write", "app:notification:read", "app:notification:write",
				"content:banner:read", "content:banner:write", "content:notice:read", "content:notice:write",
				"audit:login:read", "audit:session:read",
				"points:read", "points:write",
				"version:read", "version:write",
				"site:read", "site:write",
				"workflow:read",
				"email:read",
				"payment:read",
				"role_application:read",
				"storage:read",
				"org:dept:read", "org:member:read",
			},
		},
		"app_auditor": {
			Key:         "app_auditor",
			Name:        "应用审核管理员",
			Description: "审计、审核与只读分析权限",
			Level:       40,
			Scope:       "app",
			Permissions: []string{
				"app:read", "app:user:read", "app:notification:read",
				"content:banner:read", "content:notice:read",
				"audit:login:read", "audit:session:read",
				"points:read",
				"version:read",
				"site:read", "site:audit",
				"workflow:read",
				"email:read",
				"payment:read",
				"role_application:read", "role_application:review",
				"storage:read",
				"org:dept:read", "org:member:read",
			},
		},
		"app_viewer": {
			Key:         "app_viewer",
			Name:        "应用观察员",
			Description: "只读查看权限",
			Level:       20,
			Scope:       "app",
			Permissions: []string{
				"app:read", "app:user:read", "app:notification:read",
				"content:banner:read", "content:notice:read",
				"audit:login:read", "audit:session:read",
				"points:read",
				"version:read",
				"site:read",
				"workflow:read",
				"email:read",
				"payment:read",
				"role_application:read",
				"storage:read",
				"org:dept:read", "org:member:read",
			},
		},
	}
}

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

// LoadCustomRoles 启动时从数据库加载自定义角色到 Enforcer
func (s *AdminService) LoadCustomRoles(ctx context.Context) error {
	return s.reloadEnforcer(ctx)
}

func (s *AdminService) reloadEnforcer(ctx context.Context) error {
	customRoles, err := s.pg.ListCustomRoles(ctx)
	if err != nil {
		return err
	}

	s.enforcerMu.Lock()
	defer s.enforcerMu.Unlock()

	e, err := newAdminEnforcer()
	if err != nil {
		return err
	}

	customMap := make(map[string]admindomain.RoleDefinition, len(customRoles))
	for _, cr := range customRoles {
		for _, perm := range cr.Permissions {
			_, _ = e.AddPermissionForUser(cr.RoleKey, perm)
		}
		customMap[cr.RoleKey] = admindomain.RoleDefinition{
			Key: cr.RoleKey, Name: cr.Name, Description: cr.Description,
			Level: cr.Level, Scope: cr.Scope, Permissions: cr.Permissions,
			IsCustom: true, BaseRole: cr.BaseRole, CreatedBy: cr.CreatedBy,
		}
	}

	s.enforcer = e
	s.customRoles = customMap
	s.log.Info("Casbin Enforcer 已重载", zap.Int("customRoles", len(customMap)))
	return nil
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
	if err := s.reloadEnforcer(ctx); err != nil {
		s.log.Error("创建自定义角色后 Enforcer 重载失败", zap.Error(err))
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
	if err := s.reloadEnforcer(ctx); err != nil {
		s.log.Error("更新自定义角色后 Enforcer 重载失败", zap.Error(err))
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
	if err := s.reloadEnforcer(ctx); err != nil {
		s.log.Error("删除自定义角色后 Enforcer 重载失败", zap.Error(err))
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
