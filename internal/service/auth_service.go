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
	userdomain "aegis/internal/domain/user"
	"aegis/internal/event"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

type AuthService struct {
	cfg       config.Config
	log       *zap.Logger
	pg        *pgrepo.Repository
	sessions  *redisrepo.SessionRepository
	publisher *event.Publisher
	app       *AppService
	providers map[string]*OAuthProvider
	http      *http.Client
}

func NewAuthService(cfg config.Config, log *zap.Logger, pg *pgrepo.Repository, sessions *redisrepo.SessionRepository, publisher *event.Publisher, app *AppService) *AuthService {
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
		providers: providers,
		http:      &http.Client{Timeout: 8 * time.Second},
	}
}

func (s *AuthService) PasswordLogin(ctx context.Context, appID int64, account, password, deviceID, ip, userAgent string) (*authdomain.LoginResult, error) {
	account = normalizeAccount(account)
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
	if !user.Enabled {
		return nil, apperrors.New(40301, http.StatusForbidden, "用户账户已被禁用")
	}
	if user.DisabledEndTime != nil && user.DisabledEndTime.After(time.Now()) {
		return nil, apperrors.New(40302, http.StatusForbidden, "用户账户暂时被冻结")
	}
	if !verifyPassword(user.PasswordHash, password) {
		_ = s.pg.InsertLoginAudit(ctx, appID, user.ID, "password", "", "", ip, deviceID, userAgent, "failed", map[string]any{"reason": "invalid_password", "account": account})
		return nil, apperrors.New(40101, http.StatusUnauthorized, "账号或密码错误")
	}
	return s.issueSession(ctx, user, "password", "password", deviceID, ip, userAgent)
}

func (s *AuthService) RegisterWithPassword(ctx context.Context, appID int64, account, password, nickname, deviceID, ip, userAgent string) (*authdomain.LoginResult, error) {
	account = normalizeAccount(account)
	if err := validateAccount(account); err != nil {
		return nil, err
	}
	if err := s.validatePasswordPolicy(ctx, appID, password); err != nil {
		return nil, err
	}
	if s.app != nil {
		app, err := s.app.EnsureRegisterAllowed(ctx, appID)
		if err != nil {
			return nil, err
		}
		if err := s.validateRegisterPolicy(ctx, app, ip); err != nil {
			return nil, err
		}
	}

	existing, err := s.pg.GetUserByAppAndAccount(ctx, appID, account)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, apperrors.New(40901, http.StatusConflict, "账号已存在")
	}

	passwordAnalysis := AnalyzePasswordStrength(password)
	passwordHash, err := hashPassword(password)
	if err != nil {
		return nil, err
	}
	user, err := s.pg.CreateUser(ctx, appID, account, passwordHash)
	if err != nil {
		if err == pgrepo.ErrAccountAlreadyExists {
			return nil, apperrors.New(40901, http.StatusConflict, "账号已存在")
		}
		return nil, err
	}

	_ = s.pg.UpsertUserProfile(ctx, userdomain.Profile{
		UserID:   user.ID,
		Nickname: strings.TrimSpace(nickname),
		Extra: map[string]any{
			"register_ip":              ip,
			"register_user_agent":      userAgent,
			"password_changed_at":      time.Now().UTC().Format(time.RFC3339),
			"password_strength_score":  passwordAnalysis.Score,
			"password_change_required": false,
		},
	})
	_ = s.pg.InsertLoginAudit(ctx, appID, user.ID, "register", "password", "", ip, deviceID, userAgent, "success", map[string]any{"account": account})
	return s.issueSession(ctx, user, "password", "password", deviceID, ip, userAgent)
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
	session, err := s.ValidateAccessToken(ctx, token)
	if err != nil {
		return nil, err
	}
	user, err := s.pg.GetUserByID(ctx, session.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, apperrors.New(40103, http.StatusUnauthorized, "会话用户不存在")
	}
	if deviceID == "" {
		deviceID = session.DeviceID
	}
	if err := s.sessions.DeleteSession(ctx, token); err != nil {
		s.log.Warn("delete previous session failed", zap.Error(err))
	}
	if err := s.sessions.BlacklistToken(ctx, session.TokenID, time.Until(session.ExpiresAt)); err != nil {
		s.log.Warn("blacklist previous token failed", zap.Error(err))
	}
	return s.issueSession(ctx, user, session.Provider, "refresh", deviceID, ip, userAgent)
}

func (s *AuthService) Logout(ctx context.Context, token string) error {
	session, err := s.ValidateAccessToken(ctx, token)
	if err != nil {
		return err
	}
	_ = s.sessions.DeleteSession(ctx, token)
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
	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (interface{}, error) {
		return []byte(s.cfg.JWT.Secret), nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(s.cfg.JWT.Issuer))
	if err != nil || !parsed.Valid {
		return nil, apperrors.New(40102, http.StatusUnauthorized, "Token 无效")
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
	return s.issueSession(ctx, user, profile.Provider, loginType, deviceID, ip, userAgent)
}

func (s *AuthService) issueSession(ctx context.Context, user *userdomain.User, provider, loginType, deviceID, ip, userAgent string) (*authdomain.LoginResult, error) {
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
	expiresAt := now.Add(s.cfg.JWT.TTL)
	tokenID := uuid.NewString()
	claims := jwt.MapClaims{
		"uid":     user.ID,
		"appid":   user.AppID,
		"account": user.Account,
		"sv":      1,
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
	session := authdomain.Session{
		UserID:         user.ID,
		AppID:          user.AppID,
		Account:        user.Account,
		TokenID:        tokenID,
		SessionVersion: 1,
		DeviceID:       deviceID,
		IP:             ip,
		UserAgent:      userAgent,
		ExpiresAt:      expiresAt,
		IssuedAt:       now,
		Provider:       provider,
	}
	if err := s.sessions.SetSession(ctx, signed, session, time.Until(expiresAt)); err != nil {
		return nil, err
	}
	if err := s.enforceSessionPolicy(ctx, user.AppID, user.ID, tokenID); err != nil {
		return nil, err
	}
	_ = s.publisher.PublishJSON(ctx, event.SubjectAuthLoginAuditRequested, map[string]any{
		"user_id":    user.ID,
		"appid":      user.AppID,
		"login_type": loginType,
		"provider":   provider,
		"token_jti":  tokenID,
		"ip":         ip,
		"device_id":  deviceID,
		"user_agent": userAgent,
	})
	_ = s.publisher.PublishJSON(ctx, event.SubjectSessionAuditRequested, map[string]any{
		"user_id":    user.ID,
		"appid":      user.AppID,
		"token_jti":  tokenID,
		"event_type": "issued",
		"provider":   provider,
		"login_type": loginType,
		"ip":         ip,
		"device_id":  deviceID,
		"user_agent": userAgent,
	})
	return &authdomain.LoginResult{
		AccessToken: signed,
		ExpiresAt:   expiresAt,
		TokenType:   "Bearer",
		UserID:      user.ID,
		Account:     user.Account,
		Provider:    provider,
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

func (s *AuthService) enforceSessionPolicy(ctx context.Context, appID int64, userID int64, currentTokenID string) error {
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
		excess--
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
