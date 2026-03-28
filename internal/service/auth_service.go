package service

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
	"time"

	"aegis/internal/config"
	appdomain "aegis/internal/domain/app"
	authdomain "aegis/internal/domain/auth"
	plugindomain "aegis/internal/domain/plugin"
	securitydomain "aegis/internal/domain/security"
	userdomain "aegis/internal/domain/user"
	"aegis/internal/event"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/sync/singleflight"
)

type AuthService struct {
	cfg            config.Config
	log            *zap.Logger
	pg             *pgrepo.Repository
	sessions       *redisrepo.SessionRepository
	publisher      *event.Publisher
	app            *AppService
	security       *SecurityService
	providers      map[string]*OAuthProvider
	http           *http.Client
	plugin         *PluginService
	risk           *RiskService
	search         *AdminUserSearchService
	registerFlight singleflight.Group
}

type PasswordRegisterInput struct {
	AppID     int64
	Account   string
	Password  string
	Nickname  string
	DeviceID  string
	IP        string
	UserAgent string
}

// SetPluginService 注入插件服务
func (s *AuthService) SetPluginService(plugin *PluginService) {
	s.plugin = plugin
}

// SetRiskService 注入风控服务
func (s *AuthService) SetRiskService(risk *RiskService) {
	s.risk = risk
}

func (s *AuthService) SetAdminUserSearchService(search *AdminUserSearchService) {
	s.search = search
}

func NewAuthService(cfg config.Config, log *zap.Logger, pg *pgrepo.Repository, sessions *redisrepo.SessionRepository, publisher *event.Publisher, app *AppService, security *SecurityService) *AuthService {
	providers := map[string]*OAuthProvider{}
	for name, providerCfg := range cfg.OAuth {
		providers[name] = NewOAuthProvider(providerCfg)
	}
	return &AuthService{
		cfg:       cfg,
		log:       log,
		pg:        pg,
		sessions:  sessions,
		publisher: publisher,
		app:       app,
		security:  security,
		providers: providers,
		http:      &http.Client{Timeout: 8 * time.Second},
	}
}

func (s *AuthService) PasswordLogin(ctx context.Context, appID int64, account, password, deviceID, ip, userAgent string) (*authdomain.LoginResult, error) {
	account = normalizeAccount(account)

	// 风控评估：登录场景
	if s.risk != nil {
		riskResult, _ := s.risk.EvaluateRisk(ctx, securitydomain.RiskEvalRequest{
			Scene: "login", AppID: &appID, IP: ip, DeviceID: deviceID, UserAgent: userAgent,
			Extra: map[string]any{"account": account},
		})
		if riskResult != nil && (riskResult.Action == "block" || riskResult.Action == "ban") {
			return nil, apperrors.New(40398, http.StatusForbidden, "当前请求被风控拦截，请稍后重试")
		}
	}

	// 插件钩子：登录前检查
	if s.plugin != nil {
		hookResult := s.plugin.ExecuteHook(ctx, HookAuthPreLogin, map[string]any{
			"account": account, "appId": appID, "ip": ip, "deviceId": deviceID,
		}, plugindomain.HookMetadata{IP: ip, UserAgent: userAgent, AppID: &appID})
		if !hookResult.Allow {
			msg := hookResult.Message
			if msg == "" {
				msg = "插件拒绝了此登录请求"
			}
			return nil, apperrors.New(40399, http.StatusForbidden, msg)
		}
	}

	if s.app != nil {
		app, err := s.app.EnsureLoginAllowed(ctx, appID)
		if err != nil {
			return nil, err
		}
		if err := s.validateLoginPolicy(app, deviceID); err != nil {
			return nil, err
		}
	}
	user, err := s.pg.GetUserByAppAndAccount(ctx, appID, account)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, apperrors.New(40101, http.StatusUnauthorized, "账号或密码错误")
	}
	if err := s.ensureUserLoginState(ctx, user); err != nil {
		return nil, err
	}
	if !verifyPassword(user.PasswordHash, password) {
		_ = s.pg.InsertLoginAudit(ctx, appID, user.ID, "password", "", "", ip, deviceID, userAgent, "failed", map[string]any{"reason": "invalid_password", "account": account})
		if s.plugin != nil {
			go s.plugin.ExecuteHook(context.Background(), HookAuthLoginFailed, map[string]any{"account": account, "appId": appID, "ip": ip}, plugindomain.HookMetadata{IP: ip, AppID: &appID})
		}
		return nil, apperrors.New(40101, http.StatusUnauthorized, "账号或密码错误")
	}
	result, err := s.completeLogin(ctx, user, "password", "password", deviceID, ip, userAgent)
	if err == nil && s.plugin != nil {
		uid := user.ID
		go s.plugin.ExecuteHook(context.Background(), HookAuthSessionIssued, map[string]any{"userId": user.ID, "account": account, "appId": appID}, plugindomain.HookMetadata{IP: ip, AppID: &appID, UserID: &uid})
	}
	return result, err
}

func (s *AuthService) RegisterWithPassword(ctx context.Context, input PasswordRegisterInput) (*authdomain.LoginResult, error) {
	input.Account = normalizeAccount(input.Account)
	if err := validateAccount(input.Account); err != nil {
		return nil, err
	}
	if err := s.validatePasswordPolicy(ctx, input.AppID, input.Password); err != nil {
		return nil, err
	}
	// 风控评估：注册场景
	if s.risk != nil {
		riskResult, _ := s.risk.EvaluateRisk(ctx, securitydomain.RiskEvalRequest{
			Scene: "register", AppID: &input.AppID, IP: input.IP, DeviceID: input.DeviceID, UserAgent: input.UserAgent,
			Extra: map[string]any{"account": input.Account},
		})
		if riskResult != nil && (riskResult.Action == "block" || riskResult.Action == "ban") {
			return nil, apperrors.New(40398, http.StatusForbidden, "注册请求被风控拦截")
		}
	}
	if s.app != nil {
		app, err := s.app.EnsureRegisterAllowed(ctx, input.AppID)
		if err != nil {
			return nil, err
		}
		if err := s.validateRegisterPolicy(ctx, app, input.IP); err != nil {
			return nil, err
		}
	}

	result, err, _ := s.registerFlight.Do(registerFlightKey(input.AppID, input.Account), func() (any, error) {
		return s.registerWithPasswordOnce(ctx, input)
	})
	if err != nil {
		return nil, err
	}
	loginResult, _ := result.(*authdomain.LoginResult)
	if loginResult == nil {
		return nil, fmt.Errorf("register result is nil")
	}
	return loginResult, nil
}

func (s *AuthService) registerWithPasswordOnce(ctx context.Context, input PasswordRegisterInput) (*authdomain.LoginResult, error) {
	passwordAnalysis := AnalyzePasswordStrength(input.Password)
	passwordHash, err := hashPassword(input.Password)
	if err != nil {
		return nil, err
	}
	user, err := s.pg.CreateUser(ctx, input.AppID, input.Account, passwordHash)
	if err != nil {
		if err == pgrepo.ErrAccountAlreadyExists {
			return nil, apperrors.New(40901, http.StatusConflict, "账号已存在")
		}
		return nil, err
	}

	_ = s.pg.UpsertUserProfile(ctx, userdomain.Profile{
		UserID:   user.ID,
		Nickname: strings.TrimSpace(input.Nickname),
		Extra: map[string]any{
			"register_ip":              input.IP,
			"register_user_agent":      input.UserAgent,
			"password_changed_at":      time.Now().UTC().Format(time.RFC3339),
			"password_strength_score":  passwordAnalysis.Score,
			"password_change_required": false,
		},
	})
	_ = s.pg.InsertLoginAudit(ctx, input.AppID, user.ID, "register", "password", "", input.IP, input.DeviceID, input.UserAgent, "success", map[string]any{"account": input.Account})
	if s.plugin != nil {
		uid := user.ID
		go s.plugin.ExecuteHook(context.Background(), HookUserRegistered, map[string]any{"userId": user.ID, "account": input.Account, "appId": input.AppID}, plugindomain.HookMetadata{IP: input.IP, AppID: &input.AppID, UserID: &uid})
	}
	s.syncAdminUserSearch(input.AppID, user.ID)
	return s.completeLogin(ctx, user, "password", "password", input.DeviceID, input.IP, input.UserAgent)
}

func (s *AuthService) BuildOAuthAuthURL(ctx context.Context, provider string, appID int64, deviceID string) (string, error) {
	provider = strings.TrimSpace(strings.ToLower(provider))
	p, ok := s.providers[provider]
	if !ok {
		return "", apperrors.New(40001, http.StatusBadRequest, "不支持的 OAuth2 提供商")
	}
	if s.app != nil {
		app, err := s.app.EnsureLoginAllowed(ctx, appID)
		if err != nil {
			return "", err
		}
		if err := s.validateLoginPolicy(app, deviceID); err != nil {
			return "", err
		}
	}
	if p.cfg.ClientID == "" || p.cfg.RedirectURL == "" {
		return "", apperrors.New(40010, http.StatusBadRequest, "OAuth2 提供商未完成配置")
	}
	state := uuid.NewString()
	if err := s.sessions.SetOAuthState(ctx, state, map[string]string{
		"provider":  provider,
		"appid":     fmt.Sprintf("%d", appID),
		"device_id": deviceID,
	}, 5*time.Minute); err != nil {
		return "", err
	}
	return p.AuthURL(state), nil
}

func (s *AuthService) HandleOAuthCallback(ctx context.Context, provider, code, state, ip, userAgent string) (*authdomain.LoginResult, error) {
	provider = strings.TrimSpace(strings.ToLower(provider))
	if provider == "" || strings.TrimSpace(code) == "" || strings.TrimSpace(state) == "" {
		return nil, apperrors.New(40013, http.StatusBadRequest, "OAuth2 回调参数不完整")
	}
	payload, err := s.sessions.ConsumeOAuthState(ctx, state)
	if err != nil {
		return nil, err
	}
	if payload == nil || payload["provider"] != provider {
		return nil, apperrors.New(40002, http.StatusBadRequest, "OAuth2 状态无效或已过期")
	}
	appID, err := parseInt64(payload["appid"])
	if err != nil {
		return nil, apperrors.New(40003, http.StatusBadRequest, "无效的应用标识")
	}
	if s.app != nil {
		app, err := s.app.EnsureLoginAllowed(ctx, appID)
		if err != nil {
			return nil, err
		}
		if err := s.validateLoginPolicy(app, payload["device_id"]); err != nil {
			return nil, err
		}
	}
	deviceID := payload["device_id"]
	p, ok := s.providers[provider]
	if !ok {
		return nil, apperrors.New(40001, http.StatusBadRequest, "不支持的 OAuth2 提供商")
	}
	profile, err := p.ExchangeCode(ctx, s.http, code)
	if err != nil {
		return nil, err
	}
	return s.loginWithOAuthProfile(ctx, appID, profile, deviceID, ip, userAgent, "oauth_callback")
}

func (s *AuthService) MobileOAuthLogin(ctx context.Context, appID int64, provider string, profile authdomain.ProviderProfile, deviceID, ip, userAgent string) (*authdomain.LoginResult, error) {
	provider = strings.TrimSpace(strings.ToLower(provider))
	if _, ok := s.providers[provider]; !ok {
		return nil, apperrors.New(40001, http.StatusBadRequest, "不支持的 OAuth2 提供商")
	}
	if s.app != nil {
		app, err := s.app.EnsureLoginAllowed(ctx, appID)
		if err != nil {
			return nil, err
		}
		if err := s.validateLoginPolicy(app, deviceID); err != nil {
			return nil, err
		}
	}
	if profile.ProviderUserID == "" {
		return nil, apperrors.New(40004, http.StatusBadRequest, "providerUserId 不能为空")
	}
	profile.Provider = provider
	return s.loginWithOAuthProfile(ctx, appID, profile, deviceID, ip, userAgent, "oauth_mobile")
}

func (s *AuthService) Refresh(ctx context.Context, token, deviceID, ip, userAgent string) (*authdomain.LoginResult, error) {
	refreshSession, err := s.validateRefreshToken(ctx, token)
	if err != nil {
		return nil, err
	}
	if deviceID == "" {
		deviceID = refreshSession.DeviceID
	}
	if refreshSession.DeviceID != "" && deviceID != "" && refreshSession.DeviceID != deviceID {
		s.handleRefreshReuse(ctx, refreshSession)
		return nil, apperrors.New(40104, http.StatusUnauthorized, "刷新令牌设备绑定校验失败")
	}
	if refreshSession.UsedAt != nil || strings.TrimSpace(refreshSession.ReplacedByToken) != "" {
		s.handleRefreshReuse(ctx, refreshSession)
		return nil, apperrors.New(40104, http.StatusUnauthorized, "刷新令牌已失效")
	}
	user, err := s.pg.GetUserByID(ctx, refreshSession.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, apperrors.New(40103, http.StatusUnauthorized, "会话用户不存在")
	}
	if err := s.ensureUserLoginState(ctx, user); err != nil {
		return nil, err
	}
	bundle, err := s.issueSessionBundle(ctx, user, refreshSession.Provider, "refresh", deviceID, ip, userAgent, refreshSession.FamilyID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	refreshSession.UsedAt = &now
	refreshSession.RotatedAt = &now
	refreshSession.ReplacedByToken = bundle.RefreshSession.TokenID
	if err := s.sessions.UpdateRefreshSession(ctx, token, *refreshSession, time.Until(refreshSession.ExpiresAt)); err != nil {
		s.log.Warn("mark refresh token rotated failed", zap.Error(err))
	}
	return bundle.Result, nil
}

func (s *AuthService) Logout(ctx context.Context, token string) error {
	session, err := s.ValidateAccessToken(ctx, token)
	if err != nil {
		return err
	}
	_ = s.sessions.DeleteSession(ctx, token)
	if session.RefreshFamilyID != "" {
		_ = s.sessions.RevokeRefreshFamily(ctx, session.AppID, session.UserID, session.RefreshFamilyID, s.cfg.JWT.RefreshTTL)
		_ = s.revokeRefreshFamilySessions(ctx, session.AppID, session.UserID, session.RefreshFamilyID)
	}
	return s.sessions.BlacklistToken(ctx, session.TokenID, time.Until(session.ExpiresAt))
}

func (s *AuthService) VerifyCurrentPassword(ctx context.Context, session *authdomain.Session, password string) error {
	user, err := s.pg.GetUserByID(ctx, session.UserID)
	if err != nil {
		return err
	}
	if user == nil || user.AppID != session.AppID {
		return apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	if !verifyPassword(user.PasswordHash, password) {
		return apperrors.New(40106, http.StatusUnauthorized, "当前密码错误")
	}
	return nil
}

func (s *AuthService) ChangePassword(ctx context.Context, session *authdomain.Session, currentPassword, newPassword string) error {
	if err := s.validatePasswordPolicy(ctx, session.AppID, newPassword); err != nil {
		return err
	}
	user, err := s.pg.GetUserByID(ctx, session.UserID)
	if err != nil {
		return err
	}
	if user == nil || user.AppID != session.AppID {
		return apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	if user.PasswordHash != "" && !verifyPassword(user.PasswordHash, currentPassword) {
		return apperrors.New(40106, http.StatusUnauthorized, "当前密码错误")
	}
	if user.PasswordHash != "" && verifyPassword(user.PasswordHash, newPassword) {
		return apperrors.New(40006, http.StatusBadRequest, "新密码不能与当前密码相同")
	}

	passwordHash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	changedAt := time.Now().UTC()
	if err := s.pg.UpdateUserPassword(ctx, user.ID, passwordHash, changedAt); err != nil {
		return err
	}
	passwordAnalysis := AnalyzePasswordStrength(newPassword)
	if err := s.pg.PatchUserProfileExtra(ctx, user.ID, map[string]any{
		"password_changed_at":      changedAt.Format(time.RFC3339),
		"password_strength_score":  passwordAnalysis.Score,
		"password_change_required": false,
	}); err != nil {
		return err
	}
	_ = s.sessions.DeleteSecurityStatus(ctx, session.AppID, session.UserID)
	_ = s.sessions.DeleteUserProfile(ctx, session.AppID, session.UserID)
	_ = s.publisher.PublishJSON(ctx, event.SubjectSessionAuditRequested, map[string]any{
		"user_id":    session.UserID,
		"appid":      session.AppID,
		"token_jti":  session.TokenID,
		"event_type": "password_changed",
		"changed_at": changedAt.Format(time.RFC3339),
	})
	return nil
}

func (s *AuthService) ValidateAccessToken(ctx context.Context, token string) (*authdomain.Session, error) {
	claims, err := s.parseTokenClaims(token, "access")
	if err != nil {
		return nil, err
	}
	tokenID, _ := claims["jti"].(string)
	blacklisted, err := s.sessions.IsBlacklisted(ctx, tokenID)
	if err != nil {
		return nil, err
	}
	if blacklisted {
		return nil, apperrors.New(40104, http.StatusUnauthorized, "Token 已失效")
	}
	session, err := s.sessions.GetSession(ctx, token)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, apperrors.New(40105, http.StatusUnauthorized, "会话不存在或已过期")
	}
	return session, nil
}

func (s *AuthService) validateRefreshToken(ctx context.Context, token string) (*authdomain.RefreshSession, error) {
	claims, err := s.parseTokenClaims(token, "refresh")
	if err != nil {
		return nil, err
	}
	tokenID, _ := claims["jti"].(string)
	blacklisted, err := s.sessions.IsBlacklisted(ctx, tokenID)
	if err != nil {
		return nil, err
	}
	if blacklisted {
		return nil, apperrors.New(40104, http.StatusUnauthorized, "刷新令牌已失效")
	}
	session, err := s.sessions.GetRefreshSession(ctx, token)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, apperrors.New(40105, http.StatusUnauthorized, "刷新会话不存在或已过期")
	}
	if session.TokenID != tokenID {
		return nil, apperrors.New(40102, http.StatusUnauthorized, "刷新令牌无效")
	}
	revoked, err := s.sessions.IsRefreshFamilyRevoked(ctx, session.AppID, session.UserID, session.FamilyID)
	if err != nil {
		return nil, err
	}
	if revoked {
		return nil, apperrors.New(40104, http.StatusUnauthorized, "刷新令牌族已失效")
	}
	return session, nil
}

func (s *AuthService) parseTokenClaims(token string, expectedType string) (jwt.MapClaims, error) {
	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (interface{}, error) {
		return []byte(s.cfg.JWT.Secret), nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(s.cfg.JWT.Issuer))
	if err != nil || !parsed.Valid {
		return nil, apperrors.New(40102, http.StatusUnauthorized, "Token 无效")
	}
	tokenType, _ := claims["typ"].(string)
	if expectedType == "access" && tokenType == "" {
		return claims, nil
	}
	if tokenType != expectedType {
		return nil, apperrors.New(40102, http.StatusUnauthorized, "Token 类型不匹配")
	}
	return claims, nil
}

func (s *AuthService) loginWithOAuthProfile(ctx context.Context, appID int64, profile authdomain.ProviderProfile, deviceID, ip, userAgent, loginType string) (*authdomain.LoginResult, error) {
	userID, err := s.pg.FindOAuthBinding(ctx, appID, profile.Provider, profile.ProviderUserID)
	if err != nil {
		return nil, err
	}
	var user *userdomain.User
	if userID > 0 {
		user, err = s.pg.GetUserByID(ctx, userID)
		if err != nil {
			return nil, err
		}
	}
	if user == nil {
		account := pgrepo.LegacyOAuthAccount(profile.Provider, profile.ProviderUserID)
		user, err = s.pg.CreateUser(ctx, appID, account, "")
		if err != nil {
			return nil, err
		}
	}
	_ = s.pg.UpsertUserProfile(ctx, userdomain.Profile{
		UserID:   user.ID,
		Nickname: profile.Nickname,
		Avatar:   profile.Avatar,
		Email:    profile.Email,
		Extra: map[string]any{
			"provider": profile.Provider,
		},
	})
	if err := s.pg.UpsertOAuthBinding(ctx, appID, user.ID, profile); err != nil {
		return nil, err
	}
	if err := s.ensureUserLoginState(ctx, user); err != nil {
		return nil, err
	}
	s.syncAdminUserSearch(appID, user.ID)
	return s.completeLogin(ctx, user, profile.Provider, loginType, deviceID, ip, userAgent)
}

func (s *AuthService) syncAdminUserSearch(appID int64, userID int64) {
	if s.search == nil || appID <= 0 || userID <= 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.search.IndexUser(ctx, appID, userID); err != nil {
			s.log.Warn("sync admin user search failed", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
		}
	}()
}

func (s *AuthService) completeLogin(ctx context.Context, user *userdomain.User, provider, loginType, deviceID, ip, userAgent string) (*authdomain.LoginResult, error) {
	if err := s.ensureUserLoginState(ctx, user); err != nil {
		return nil, err
	}
	if s.security != nil {
		challenge, err := s.security.MaybeCreateSecondFactorChallenge(ctx, user, provider, loginType, deviceID, ip, userAgent)
		if err != nil {
			return nil, err
		}
		if challenge != nil {
			if s.plugin != nil {
				uid := user.ID
				go s.plugin.ExecuteHook(context.Background(), HookAuthMFACreated, map[string]any{"userId": user.ID, "account": user.Account}, plugindomain.HookMetadata{IP: ip, UserID: &uid})
			}
			return challenge, nil
		}
	}
	return s.issueSession(ctx, user, provider, loginType, deviceID, ip, userAgent)
}

func (s *AuthService) VerifySecondFactor(ctx context.Context, challengeID string, code string, recoveryCode string) (*authdomain.LoginResult, error) {
	if s.security == nil {
		return nil, apperrors.New(50321, http.StatusServiceUnavailable, "双因子认证模块未启用")
	}
	user, challenge, err := s.security.VerifySecondFactorChallenge(ctx, challengeID, code, recoveryCode)
	if err != nil {
		return nil, err
	}
	if err := s.ensureUserLoginState(ctx, user); err != nil {
		return nil, err
	}
	result, err := s.issueSession(ctx, user, challenge.Provider, challenge.LoginType, challenge.DeviceID, challenge.IP, challenge.UserAgent)
	if err == nil && s.plugin != nil {
		uid := user.ID
		go s.plugin.ExecuteHook(context.Background(), HookAuthMFAVerified, map[string]any{"userId": user.ID, "account": user.Account}, plugindomain.HookMetadata{IP: challenge.IP, UserID: &uid})
	}
	return result, err
}

func (s *AuthService) BeginPasskeyLogin(ctx context.Context, appID int64, deviceID string) (*securitydomain.PasskeyLoginSession, *protocol.CredentialAssertion, error) {
	if s.security == nil {
		return nil, nil, apperrors.New(50321, http.StatusServiceUnavailable, "Passkey 模块未启用")
	}
	if s.app != nil {
		app, err := s.app.EnsureLoginAllowed(ctx, appID)
		if err != nil {
			return nil, nil, err
		}
		if err := s.validateLoginPolicy(app, deviceID); err != nil {
			return nil, nil, err
		}
	}
	return s.security.BeginPasskeyLogin(ctx, appID)
}

func (s *AuthService) VerifyPasskeyLogin(ctx context.Context, appID int64, challengeID string, payload []byte, deviceID, ip, userAgent string) (*authdomain.LoginResult, error) {
	if s.security == nil {
		return nil, apperrors.New(50321, http.StatusServiceUnavailable, "Passkey 模块未启用")
	}
	user, err := s.security.VerifyPasskeyLogin(ctx, appID, challengeID, payload)
	if err != nil {
		return nil, err
	}
	if err := s.ensureUserLoginState(ctx, user); err != nil {
		return nil, err
	}
	return s.issueSession(ctx, user, "passkey", "passkey", deviceID, ip, userAgent)
}

func (s *AuthService) ensureUserLoginState(ctx context.Context, user *userdomain.User) error {
	if user == nil {
		return apperrors.New(40103, http.StatusUnauthorized, "会话用户不存在")
	}
	if ban, err := s.pg.RefreshUserAccountBanState(ctx, user.AppID, user.ID); err != nil {
		s.log.Warn("refresh user account ban state failed", zap.Int64("appid", user.AppID), zap.Int64("userId", user.ID), zap.Error(err))
	} else if ban != nil {
		if ban.BanType == userdomain.AccountBanTypePermanent {
			return apperrors.New(40301, http.StatusForbidden, BanMessageFromRecord(ban))
		}
		return apperrors.New(40302, http.StatusForbidden, BanMessageFromRecord(ban))
	}
	if !user.Enabled {
		return apperrors.New(40301, http.StatusForbidden, "用户账户已被禁用")
	}
	if user.DisabledEndTime != nil && user.DisabledEndTime.After(time.Now()) {
		return apperrors.New(40302, http.StatusForbidden, "用户账户暂时被冻结")
	}
	return nil
}

func (s *AuthService) issueSession(ctx context.Context, user *userdomain.User, provider, loginType, deviceID, ip, userAgent string) (*authdomain.LoginResult, error) {
	bundle, err := s.issueSessionBundle(ctx, user, provider, loginType, deviceID, ip, userAgent, "")
	if err != nil {
		return nil, err
	}
	return bundle.Result, nil
}

type issuedSessionBundle struct {
	Result         *authdomain.LoginResult
	AccessSession  authdomain.Session
	RefreshSession authdomain.RefreshSession
}

func (s *AuthService) issueSessionBundle(ctx context.Context, user *userdomain.User, provider, loginType, deviceID, ip, userAgent, refreshFamilyID string) (*issuedSessionBundle, error) {
	if s.app != nil {
		app, err := s.app.GetApp(ctx, user.AppID)
		if err != nil {
			return nil, err
		}
		if err := s.validateLoginPolicy(app, deviceID); err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC()
	accessExpiresAt := now.Add(s.cfg.JWT.TTL)
	refreshExpiresAt := now.Add(s.cfg.JWT.RefreshTTL)
	accessTokenID := uuid.NewString()
	refreshTokenID := uuid.NewString()
	refreshFamilyID = strings.TrimSpace(refreshFamilyID)
	if refreshFamilyID == "" {
		refreshFamilyID = uuid.NewString()
	}
	accessClaims := jwt.MapClaims{
		"uid":     user.ID,
		"appid":   user.AppID,
		"account": user.Account,
		"sv":      1,
		"jti":     accessTokenID,
		"typ":     "access",
		"family":  refreshFamilyID,
		"iss":     s.cfg.JWT.Issuer,
		"iat":     now.Unix(),
		"exp":     accessExpiresAt.Unix(),
	}
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	signedAccess, err := accessToken.SignedString([]byte(s.cfg.JWT.Secret))
	if err != nil {
		return nil, err
	}
	refreshClaims := jwt.MapClaims{
		"uid":     user.ID,
		"appid":   user.AppID,
		"account": user.Account,
		"sv":      1,
		"jti":     refreshTokenID,
		"typ":     "refresh",
		"family":  refreshFamilyID,
		"iss":     s.cfg.JWT.Issuer,
		"iat":     now.Unix(),
		"exp":     refreshExpiresAt.Unix(),
	}
	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	signedRefresh, err := refreshToken.SignedString([]byte(s.cfg.JWT.Secret))
	if err != nil {
		return nil, err
	}
	session := authdomain.Session{
		UserID:          user.ID,
		AppID:           user.AppID,
		Account:         user.Account,
		TokenID:         accessTokenID,
		RefreshFamilyID: refreshFamilyID,
		SessionVersion:  1,
		DeviceID:        deviceID,
		IP:              ip,
		UserAgent:       userAgent,
		ExpiresAt:       accessExpiresAt,
		IssuedAt:        now,
		Provider:        provider,
	}
	refreshSession := authdomain.RefreshSession{
		UserID:         user.ID,
		AppID:          user.AppID,
		Account:        user.Account,
		TokenID:        refreshTokenID,
		FamilyID:       refreshFamilyID,
		SessionVersion: 1,
		DeviceID:       deviceID,
		IP:             ip,
		UserAgent:      userAgent,
		Provider:       provider,
		ExpiresAt:      refreshExpiresAt,
		IssuedAt:       now,
	}
	if err := s.sessions.SetSession(ctx, signedAccess, session, time.Until(accessExpiresAt)); err != nil {
		return nil, err
	}
	if err := s.sessions.SetRefreshSession(ctx, signedRefresh, refreshSession, time.Until(refreshExpiresAt)); err != nil {
		_ = s.sessions.DeleteSession(ctx, signedAccess)
		return nil, err
	}
	if err := s.enforceSessionPolicy(ctx, user.AppID, user.ID, accessTokenID, refreshFamilyID); err != nil {
		return nil, err
	}
	_ = s.publisher.PublishJSON(ctx, event.SubjectAuthLoginAuditRequested, map[string]any{
		"user_id":    user.ID,
		"appid":      user.AppID,
		"login_type": loginType,
		"provider":   provider,
		"token_jti":  accessTokenID,
		"ip":         ip,
		"device_id":  deviceID,
		"user_agent": userAgent,
	})
	_ = s.publisher.PublishJSON(ctx, event.SubjectSessionAuditRequested, map[string]any{
		"user_id":    user.ID,
		"appid":      user.AppID,
		"token_jti":  accessTokenID,
		"event_type": "issued",
		"provider":   provider,
		"login_type": loginType,
		"ip":         ip,
		"device_id":  deviceID,
		"user_agent": userAgent,
	})
	return &issuedSessionBundle{
		Result: &authdomain.LoginResult{
			AccessToken:      signedAccess,
			RefreshToken:     signedRefresh,
			ExpiresAt:        accessExpiresAt,
			RefreshExpiresAt: refreshExpiresAt,
			TokenType:        "Bearer",
			UserID:           user.ID,
			Account:          user.Account,
			Provider:         provider,
		},
		AccessSession:  session,
		RefreshSession: refreshSession,
	}, nil
}

func (s *AuthService) validateLoginPolicy(app *appdomain.App, deviceID string) error {
	if s.app == nil {
		return nil
	}
	policy := s.app.ResolvePolicy(app)
	if policy.LoginCheckDevice && strings.TrimSpace(deviceID) == "" {
		return apperrors.New(40024, http.StatusBadRequest, "当前应用要求提供设备标识")
	}
	return nil
}

func (s *AuthService) validateRegisterPolicy(ctx context.Context, app *appdomain.App, ip string) error {
	if s.app == nil {
		return nil
	}
	policy := s.app.ResolvePolicy(app)
	if policy.RegisterCheckIP {
		if strings.TrimSpace(ip) == "" {
			return apperrors.New(40025, http.StatusBadRequest, "当前应用要求提供注册 IP")
		}
		exists, err := s.pg.HasAnyUserRegisteredFromIP(ctx, ip)
		if err != nil {
			return err
		}
		if exists {
			return apperrors.New(40902, http.StatusConflict, "IP已注册过账号")
		}
	}
	return nil
}

func (s *AuthService) enforceSessionPolicy(ctx context.Context, appID int64, userID int64, currentTokenID string, currentFamilyID string) error {
	if s.app == nil || s.sessions == nil {
		return nil
	}
	app, err := s.app.GetApp(ctx, appID)
	if err != nil {
		return err
	}
	policy := s.app.ResolvePolicy(app)
	if policy.MultiDeviceLogin && policy.MultiDeviceLimit <= 0 {
		return nil
	}
	limit := policy.MultiDeviceLimit
	if limit <= 0 {
		limit = 1
	}
	sessions, err := s.sessions.ListUserSessions(ctx, appID, userID)
	if err != nil {
		return err
	}
	if len(sessions) <= limit {
		return nil
	}
	excess := len(sessions) - limit
	for _, item := range sessions {
		if excess <= 0 {
			break
		}
		if item.Session.TokenID == currentTokenID {
			continue
		}
		if err := s.sessions.DeleteSessionByHash(ctx, appID, userID, item.TokenHash); err != nil {
			return err
		}
		ttl := time.Until(item.Session.ExpiresAt)
		if ttl > 0 {
			if err := s.sessions.BlacklistToken(ctx, item.Session.TokenID, ttl); err != nil {
				return err
			}
		}
		if item.Session.RefreshFamilyID != "" && item.Session.RefreshFamilyID != currentFamilyID {
			if err := s.sessions.RevokeRefreshFamily(ctx, appID, userID, item.Session.RefreshFamilyID, s.cfg.JWT.RefreshTTL); err != nil {
				return err
			}
			if err := s.revokeRefreshFamilySessions(ctx, appID, userID, item.Session.RefreshFamilyID); err != nil {
				return err
			}
		}
		excess--
	}
	return nil
}

func (s *AuthService) handleRefreshReuse(ctx context.Context, session *authdomain.RefreshSession) {
	if session == nil {
		return
	}
	if err := s.sessions.RevokeRefreshFamily(ctx, session.AppID, session.UserID, session.FamilyID, s.cfg.JWT.RefreshTTL); err != nil {
		s.log.Warn("revoke refresh family failed", zap.Int64("appid", session.AppID), zap.Int64("userId", session.UserID), zap.Error(err))
	}
	if err := s.revokeRefreshFamilySessions(ctx, session.AppID, session.UserID, session.FamilyID); err != nil {
		s.log.Warn("cleanup refresh sessions failed", zap.Int64("appid", session.AppID), zap.Int64("userId", session.UserID), zap.Error(err))
	}
	if err := s.revokeAccessSessionsByFamily(ctx, session.AppID, session.UserID, session.FamilyID); err != nil {
		s.log.Warn("cleanup access sessions failed", zap.Int64("appid", session.AppID), zap.Int64("userId", session.UserID), zap.Error(err))
	}
}

func (s *AuthService) revokeRefreshFamilySessions(ctx context.Context, appID int64, userID int64, familyID string) error {
	if familyID == "" {
		return nil
	}
	items, err := s.sessions.ListIndexedRefreshSessions(ctx, appID, userID)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.Session.FamilyID != familyID {
			continue
		}
		if err := s.sessions.DeleteRefreshSessionByHash(ctx, appID, userID, item.TokenHash); err != nil {
			return err
		}
		ttl := time.Until(item.Session.ExpiresAt)
		if ttl > 0 {
			if err := s.sessions.BlacklistToken(ctx, item.Session.TokenID, ttl); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *AuthService) revokeAccessSessionsByFamily(ctx context.Context, appID int64, userID int64, familyID string) error {
	if familyID == "" {
		return nil
	}
	items, err := s.sessions.ListUserSessions(ctx, appID, userID)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.Session.RefreshFamilyID != familyID {
			continue
		}
		if err := s.sessions.DeleteSessionByHash(ctx, appID, userID, item.TokenHash); err != nil {
			return err
		}
		ttl := time.Until(item.Session.ExpiresAt)
		if ttl > 0 {
			if err := s.sessions.BlacklistToken(ctx, item.Session.TokenID, ttl); err != nil {
				return err
			}
		}
	}
	return nil
}

func verifyPassword(hash, password string) bool {
	if hash == "" {
		return false
	}
	if strings.HasPrefix(hash, "$2a$") || strings.HasPrefix(hash, "$2b$") || strings.HasPrefix(hash, "$2y$") {
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	}
	return subtle.ConstantTimeCompare([]byte(hash), []byte(password)) == 1
}

func hashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func normalizeAccount(account string) string {
	return strings.TrimSpace(account)
}

func registerFlightKey(appID int64, account string) string {
	return fmt.Sprintf("%d:%s", appID, account)
}

func validateAccount(account string) error {
	if account == "" {
		return apperrors.New(40005, http.StatusBadRequest, "账号不能为空")
	}
	if len(account) < 3 || len(account) > 64 {
		return apperrors.New(40005, http.StatusBadRequest, "账号长度必须在 3 到 64 个字符之间")
	}
	return nil
}

func validatePasswordStrength(password string) error {
	password = strings.TrimSpace(password)
	if len(password) < 8 {
		return apperrors.New(40007, http.StatusBadRequest, "密码长度不能少于 8 位")
	}
	if len(password) > 72 {
		return apperrors.New(40008, http.StatusBadRequest, "密码长度不能超过 72 位")
	}
	return nil
}

func (s *AuthService) validatePasswordPolicy(ctx context.Context, appID int64, password string) error {
	if s.app != nil {
		return s.app.ValidatePasswordWithAppPolicy(ctx, appID, password)
	}
	return validatePasswordStrength(password)
}

func parseInt64(value string) (int64, error) {
	var result int64
	_, err := fmt.Sscanf(value, "%d", &result)
	return result, err
}
