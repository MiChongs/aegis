package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"aegis/internal/config"
	authdomain "aegis/internal/domain/auth"
	securitydomain "aegis/internal/domain/security"
	userdomain "aegis/internal/domain/user"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"github.com/go-webauthn/webauthn/protocol"
	webauthnlib "github.com/go-webauthn/webauthn/webauthn"
	gojson "github.com/goccy/go-json"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"go.uber.org/zap"
)

type SecurityService struct {
	cfg           config.Config
	log           *zap.Logger
	pg            *pgrepo.Repository
	sessions      *redisrepo.SessionRepository
	app           *AppService
	registry      *SecurityRegistry
	mu            sync.RWMutex
	securityCfg   config.SecurityConfig
	webauthn      *webauthnlib.WebAuthn
	encryptionKey []byte
	reloadVersion uint64
	reloadedAt    time.Time
}

type webAuthnUser struct {
	user        *userdomain.User
	profile     *userdomain.Profile
	credentials []webauthnlib.Credential
}

func NewSecurityService(cfg config.Config, log *zap.Logger, pg *pgrepo.Repository, sessions *redisrepo.SessionRepository, app *AppService) *SecurityService {
	service := &SecurityService{
		cfg:      cfg,
		log:      log,
		pg:       pg,
		sessions: sessions,
		app:      app,
		registry: NewSecurityRegistry(),
	}
	if err := service.applyInitialConfig(cfg.Security); err != nil {
		service.log.Warn("apply initial security config failed", zap.Error(err))
	}
	return service
}

func (s *SecurityService) CurrentConfig() config.SecurityConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSecurityConfig(s.securityCfg)
}

func (s *SecurityService) ReloadMeta() (uint64, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.reloadVersion, s.reloadedAt
}

func (s *SecurityService) ValidateConfig(cfg config.SecurityConfig) error {
	_, _, _, _, err := s.prepareRuntimeConfig(cfg, true)
	return err
}

func (s *SecurityService) Reload(cfg config.SecurityConfig) error {
	normalized, statuses, webAuthn, encryptionKey, err := s.prepareRuntimeConfig(cfg, true)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.securityCfg = cloneSecurityConfig(normalized)
	s.webauthn = webAuthn
	s.encryptionKey = encryptionKey
	s.reloadVersion++
	s.reloadedAt = time.Now().UTC()
	s.mu.Unlock()
	s.registerStatuses(statuses)
	return nil
}

func (s *SecurityService) applyInitialConfig(cfg config.SecurityConfig) error {
	normalized, statuses, webAuthn, encryptionKey, err := s.prepareRuntimeConfig(cfg, false)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.securityCfg = cloneSecurityConfig(normalized)
	s.webauthn = webAuthn
	s.encryptionKey = encryptionKey
	s.reloadVersion = 1
	s.reloadedAt = time.Now().UTC()
	s.mu.Unlock()
	s.registerStatuses(statuses)
	return nil
}

func (s *SecurityService) prepareRuntimeConfig(raw config.SecurityConfig, strict bool) (config.SecurityConfig, []securitydomain.ModuleStatus, *webauthnlib.WebAuthn, []byte, error) {
	cfg := s.normalizeRuntimeConfig(raw)
	encryptionKey := securityKeyMaterial(cfg.MasterKey)
	statuses := make([]securitydomain.ModuleStatus, 0, 3)

	totpEnabled := cfg.Modules.TOTPEnabled && cfg.TOTP.Enabled
	totpReady := len(encryptionKey) > 0
	if totpEnabled && !totpReady && strict {
		return config.SecurityConfig{}, nil, nil, nil, fmt.Errorf("init totp module: missing encryption key")
	}
	statuses = append(statuses, securitydomain.ModuleStatus{
		Key:       securitydomain.ModuleTOTP,
		Name:      "双因子认证",
		Enabled:   totpEnabled,
		Ready:     totpReady,
		HotReload: true,
		Message:   "TOTP 校验与密钥托管",
	})
	statuses = append(statuses, securitydomain.ModuleStatus{
		Key:       securitydomain.ModuleRecoveryCodes,
		Name:      "恢复码",
		Enabled:   cfg.Modules.RecoveryCodesEnabled && cfg.RecoveryCode.Enabled,
		Ready:     true,
		HotReload: true,
		Message:   "一次性恢复码",
	})

	passkeyEnabled := cfg.Modules.PasskeyEnabled && cfg.Passkey.Enabled
	passkeyStatus := securitydomain.ModuleStatus{
		Key:       securitydomain.ModulePasskey,
		Name:      "Passkey",
		Enabled:   passkeyEnabled,
		Ready:     true,
		HotReload: true,
		Message:   "WebAuthn 无密码登录",
	}
	var webAuthn *webauthnlib.WebAuthn
	if passkeyEnabled {
		instance, err := webauthnlib.New(&webauthnlib.Config{
			RPDisplayName: cfg.Passkey.RPDisplayName,
			RPID:          cfg.Passkey.RPID,
			RPOrigins:     cloneStrings(cfg.Passkey.RPOrigins),
			RPTopOrigins:  cloneStrings(cfg.Passkey.RPTopOrigins),
			AuthenticatorSelection: protocol.AuthenticatorSelection{
				UserVerification: userVerificationRequirement(cfg.Passkey.UserVerification),
			},
		})
		if err != nil {
			passkeyStatus.Ready = false
			passkeyStatus.Message = "Passkey 模块初始化失败"
			if strict {
				return config.SecurityConfig{}, nil, nil, nil, fmt.Errorf("init passkey module: %w", err)
			}
			s.log.Warn("init passkey module failed", zap.Error(err))
		} else {
			webAuthn = instance
		}
	}
	statuses = append(statuses, passkeyStatus)
	return cloneSecurityConfig(cfg), statuses, webAuthn, encryptionKey, nil
}

func (s *SecurityService) normalizeRuntimeConfig(cfg config.SecurityConfig) config.SecurityConfig {
	cfg = config.NormalizeSecurityConfig(cfg, s.cfg.AppName, s.cfg.JWT.Secret)
	cfg.Passkey.RPOrigins = compactStrings(cfg.Passkey.RPOrigins)
	cfg.Passkey.RPTopOrigins = compactStrings(cfg.Passkey.RPTopOrigins)
	if cfg.TOTP.Digits != 6 && cfg.TOTP.Digits != 8 {
		cfg.TOTP.Digits = 6
	}
	return cfg
}

func (s *SecurityService) registerStatuses(statuses []securitydomain.ModuleStatus) {
	if s.registry == nil {
		return
	}
	modules := make([]SecurityModule, 0, len(statuses))
	for _, status := range statuses {
		modules = append(modules, NewStaticSecurityModule(status))
	}
	s.registry.Replace(modules)
}

func (s *SecurityService) currentConfig() config.SecurityConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSecurityConfig(s.securityCfg)
}

func (s *SecurityService) currentWebAuthn() *webauthnlib.WebAuthn {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.webauthn
}

func (s *SecurityService) currentEncryptionKey() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.encryptionKey) == 0 {
		return nil
	}
	result := make([]byte, len(s.encryptionKey))
	copy(result, s.encryptionKey)
	return result
}

func (s *SecurityService) Modules() []securitydomain.ModuleStatus {
	if s.registry == nil {
		return nil
	}
	return s.registry.Statuses()
}

func (s *SecurityService) GetSecurityStatus(ctx context.Context, session *authdomain.Session) (*userdomain.SecurityStatus, error) {
	if s.sessions != nil {
		cached, err := s.sessions.GetSecurityStatus(ctx, session.AppID, session.UserID)
		if err != nil {
			s.log.Warn("load security cache failed", zap.Error(err))
		} else if cached != nil {
			return cached, nil
		}
	}

	user, profile, err := s.loadUserProfile(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	providers, err := s.pg.ListOAuthProvidersByUserID(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	totpRecord, err := s.pg.GetUserTOTPSecret(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	recoveryItems, err := s.pg.ListUserRecoveryCodes(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	passkeyItems, err := s.pg.ListUserPasskeys(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}

	cfg := s.currentConfig()
	twoFactor := securitydomain.TOTPStatus{Issuer: cfg.TOTP.Issuer}
	if totpRecord != nil {
		twoFactor.Enabled = totpRecord.Enabled
		twoFactor.Method = "totp"
		twoFactor.Issuer = totpRecord.Issuer
		twoFactor.AccountName = totpRecord.AccountName
		twoFactor.EnabledAt = totpRecord.EnabledAt
		twoFactor.LastVerifiedAt = totpRecord.LastVerifiedAt
	}

	recoverySummary := s.buildRecoverySummary(recoveryItems)
	passkeySummary := s.buildPasskeySummary(passkeyItems)
	status := &userdomain.SecurityStatus{
		HasPassword:            user.PasswordHash != "",
		TwoFactorEnabled:       twoFactor.Enabled,
		TwoFactorMethod:        twoFactor.Method,
		PasskeyEnabled:         passkeySummary.Count > 0,
		PasswordStrengthScore:  extraInt(profile, "password_strength_score"),
		PasswordChangeRequired: extraBool(profile, "password_change_required"),
		PasswordChangedAt:      extraTime(profile, "password_changed_at"),
		PasswordExpiresAt:      extraTime(profile, "password_expires_at"),
		OAuth2Bindings:         len(providers),
		OAuth2Providers:        providers,
		TwoFactor:              twoFactor,
		RecoveryCodes:          recoverySummary,
		Passkeys:               passkeySummary,
		Modules:                s.Modules(),
	}

	if s.sessions != nil {
		if err := s.sessions.SetSecurityStatus(ctx, session.AppID, session.UserID, *status, 60*time.Second); err != nil {
			s.log.Warn("cache security status failed", zap.Error(err))
		}
	}
	return status, nil
}

func (s *SecurityService) BeginTOTPEnrollment(ctx context.Context, session *authdomain.Session) (*securitydomain.TOTPEnrollment, error) {
	if err := s.ensureModuleReady(securitydomain.ModuleTOTP); err != nil {
		return nil, err
	}
	cfg := s.currentConfig()
	user, profile, err := s.loadUserProfile(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	existing, err := s.pg.GetUserTOTPSecret(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.Enabled {
		return nil, apperrors.New(40920, http.StatusConflict, "双因子认证已启用")
	}

	accountName := s.resolveTOTPAccountName(user, profile)
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      cfg.TOTP.Issuer,
		AccountName: accountName,
		Period:      30,
		Digits:      digitsFromInt(cfg.TOTP.Digits),
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return nil, err
	}

	enrollmentID := newChallengeID("totp")
	expiresAt := time.Now().UTC().Add(cfg.TOTP.EnrollmentTTL)
	state := securitydomain.TOTPEnrollmentState{
		EnrollmentID: enrollmentID,
		AppID:        session.AppID,
		UserID:       session.UserID,
		Secret:       key.Secret(),
		Issuer:       cfg.TOTP.Issuer,
		AccountName:  accountName,
		ExpiresAt:    expiresAt,
	}
	if s.sessions == nil {
		return nil, apperrors.New(50320, http.StatusServiceUnavailable, "安全会话服务不可用")
	}
	if err := s.sessions.SetTOTPEnrollmentState(ctx, state, time.Until(expiresAt)); err != nil {
		return nil, err
	}

	return &securitydomain.TOTPEnrollment{
		EnrollmentID:    enrollmentID,
		Secret:          key.Secret(),
		SecretMasked:    maskSecret(key.Secret()),
		ProvisioningURI: key.URL(),
		Issuer:          cfg.TOTP.Issuer,
		AccountName:     accountName,
		ExpiresAt:       expiresAt,
	}, nil
}

func (s *SecurityService) EnableTOTP(ctx context.Context, session *authdomain.Session, enrollmentID string, code string) (*securitydomain.TOTPStatus, *securitydomain.RecoveryCodeIssueResult, error) {
	if err := s.ensureModuleReady(securitydomain.ModuleTOTP); err != nil {
		return nil, nil, err
	}
	if s.sessions == nil {
		return nil, nil, apperrors.New(50320, http.StatusServiceUnavailable, "安全会话服务不可用")
	}
	state, err := s.sessions.GetTOTPEnrollmentState(ctx, strings.TrimSpace(enrollmentID))
	if err != nil {
		return nil, nil, err
	}
	if state == nil || state.AppID != session.AppID || state.UserID != session.UserID || state.ExpiresAt.Before(time.Now().UTC()) {
		return nil, nil, apperrors.New(40050, http.StatusBadRequest, "双因子认证配置已失效")
	}
	if err := s.verifyTOTPSecret(state.Secret, code); err != nil {
		return nil, nil, err
	}

	ciphertext, err := encryptSecret(s.currentEncryptionKey(), state.Secret)
	if err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC()
	record := securitydomain.TOTPSecretRecord{
		UserID:         session.UserID,
		AppID:          session.AppID,
		SecretCipher:   ciphertext,
		Issuer:         state.Issuer,
		AccountName:    state.AccountName,
		Enabled:        true,
		EnabledAt:      &now,
		LastVerifiedAt: &now,
	}
	if err := s.pg.UpsertUserTOTPSecret(ctx, record); err != nil {
		return nil, nil, err
	}
	_ = s.sessions.DeleteTOTPEnrollmentState(ctx, state.EnrollmentID)

	if err := s.patchSecurityFlags(ctx, session.UserID, map[string]any{
		"two_factor_enabled": true,
		"two_factor_method":  "totp",
	}); err != nil {
		return nil, nil, err
	}

	recoveryResult, err := s.issueRecoveryCodes(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, nil, err
	}
	s.invalidateSecurityCaches(ctx, session.AppID, session.UserID)
	s.notifySecurityEvent(ctx, session.AppID, session.UserID, "security", "双因子认证已启用", "账户的双因子认证已启用。", "info")

	status := &securitydomain.TOTPStatus{
		Enabled:        true,
		Method:         "totp",
		Issuer:         state.Issuer,
		AccountName:    state.AccountName,
		EnabledAt:      &now,
		LastVerifiedAt: &now,
	}
	return status, recoveryResult, nil
}

func (s *SecurityService) DisableTOTP(ctx context.Context, session *authdomain.Session, code string, recoveryCode string) (*securitydomain.TOTPStatus, error) {
	record, err := s.pg.GetUserTOTPSecret(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	if record == nil || !record.Enabled {
		return nil, apperrors.New(40051, http.StatusBadRequest, "双因子认证未启用")
	}
	if err := s.verifyUserSecondFactor(ctx, session.AppID, session.UserID, code, recoveryCode); err != nil {
		return nil, err
	}
	if err := s.pg.DeleteUserTOTPSecret(ctx, session.AppID, session.UserID); err != nil {
		return nil, err
	}
	if err := s.pg.DeleteUserRecoveryCodes(ctx, session.AppID, session.UserID); err != nil {
		return nil, err
	}
	if err := s.patchSecurityFlags(ctx, session.UserID, map[string]any{
		"two_factor_enabled": false,
		"two_factor_method":  "",
	}); err != nil {
		return nil, err
	}
	s.invalidateSecurityCaches(ctx, session.AppID, session.UserID)
	s.notifySecurityEvent(ctx, session.AppID, session.UserID, "security", "双因子认证已关闭", "账户的双因子认证已关闭。", "warning")
	return &securitydomain.TOTPStatus{Enabled: false, Method: "totp", Issuer: record.Issuer, AccountName: record.AccountName}, nil
}

func (s *SecurityService) ListRecoveryCodes(ctx context.Context, session *authdomain.Session) (*securitydomain.RecoveryCodeSummary, error) {
	items, err := s.pg.ListUserRecoveryCodes(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	summary := s.buildRecoverySummary(items)
	return &summary, nil
}

func (s *SecurityService) GenerateRecoveryCodes(ctx context.Context, session *authdomain.Session, code string, recoveryCode string) (*securitydomain.RecoveryCodeIssueResult, error) {
	return s.rotateRecoveryCodes(ctx, session, code, recoveryCode)
}

func (s *SecurityService) RegenerateRecoveryCodes(ctx context.Context, session *authdomain.Session, code string, recoveryCode string) (*securitydomain.RecoveryCodeIssueResult, error) {
	return s.rotateRecoveryCodes(ctx, session, code, recoveryCode)
}

func (s *SecurityService) BeginPasskeyRegistration(ctx context.Context, session *authdomain.Session) (*securitydomain.PasskeyRegistrationSession, *protocol.CredentialCreation, error) {
	if err := s.ensureModuleReady(securitydomain.ModulePasskey); err != nil {
		return nil, nil, err
	}
	if s.sessions == nil {
		return nil, nil, apperrors.New(50320, http.StatusServiceUnavailable, "安全会话服务不可用")
	}
	cfg := s.currentConfig()
	webauthn := s.currentWebAuthn()
	if webauthn == nil {
		return nil, nil, apperrors.New(50322, http.StatusServiceUnavailable, "当前安全模块暂不可用")
	}
	adapter, err := s.makeWebAuthnUser(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, nil, err
	}
	creation, sessionData, err := webauthn.BeginRegistration(adapter,
		webauthnlib.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthnlib.WithExclusions(webauthnlib.Credentials(adapter.WebAuthnCredentials()).CredentialDescriptors()),
	)
	if err != nil {
		return nil, nil, err
	}
	challengeID := newChallengeID("passkey_reg")
	rawSession, err := gojson.Marshal(sessionData)
	if err != nil {
		return nil, nil, err
	}
	expiresAt := sessionData.Expires
	if expiresAt.IsZero() {
		expiresAt = time.Now().UTC().Add(cfg.Passkey.ChallengeTTL)
	}
	state := securitydomain.PasskeyRegistrationState{
		ChallengeID: challengeID,
		AppID:       session.AppID,
		UserID:      session.UserID,
		SessionData: rawSession,
		ExpiresAt:   expiresAt,
	}
	if err := s.sessions.SetPasskeyRegistrationState(ctx, state, time.Until(expiresAt)); err != nil {
		return nil, nil, err
	}
	return &securitydomain.PasskeyRegistrationSession{ChallengeID: challengeID, AppID: session.AppID, UserID: session.UserID, ExpiresAt: expiresAt}, creation, nil
}

func (s *SecurityService) FinishPasskeyRegistration(ctx context.Context, session *authdomain.Session, challengeID string, payload []byte, credentialName string) (*securitydomain.PasskeyView, error) {
	if err := s.ensureModuleReady(securitydomain.ModulePasskey); err != nil {
		return nil, err
	}
	if s.sessions == nil {
		return nil, apperrors.New(50320, http.StatusServiceUnavailable, "安全会话服务不可用")
	}
	state, err := s.sessions.GetPasskeyRegistrationState(ctx, strings.TrimSpace(challengeID))
	if err != nil {
		return nil, err
	}
	if state == nil || state.AppID != session.AppID || state.UserID != session.UserID || state.ExpiresAt.Before(time.Now().UTC()) {
		return nil, apperrors.New(40052, http.StatusBadRequest, "Passkey 注册会话已失效")
	}
	var sessionData webauthnlib.SessionData
	if err := gojson.Unmarshal(state.SessionData, &sessionData); err != nil {
		return nil, err
	}
	adapter, err := s.makeWebAuthnUser(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	req, err := buildJSONRequest(ctx, payload)
	if err != nil {
		return nil, apperrors.New(40053, http.StatusBadRequest, "Passkey 凭证数据不能为空")
	}
	webauthn := s.currentWebAuthn()
	if webauthn == nil {
		return nil, apperrors.New(50322, http.StatusServiceUnavailable, "当前安全模块暂不可用")
	}
	credential, err := webauthn.FinishRegistration(adapter, sessionData, req)
	if err != nil {
		return nil, apperrors.New(40054, http.StatusBadRequest, "Passkey 注册校验失败")
	}
	rawCredential, err := gojson.Marshal(credential)
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(credentialName)
	if name == "" {
		name = fmt.Sprintf("Passkey %s", time.Now().In(time.Local).Format("2006-01-02 15:04"))
	}
	record, err := s.pg.CreateUserPasskey(ctx, securitydomain.PasskeyRecord{
		UserID:         session.UserID,
		AppID:          session.AppID,
		CredentialID:   credential.ID,
		CredentialName: name,
		CredentialJSON: rawCredential,
		AAGUID:         credential.Authenticator.AAGUID,
		SignCount:      credential.Authenticator.SignCount,
	})
	if err != nil {
		return nil, err
	}
	_ = s.sessions.DeletePasskeyRegistrationState(ctx, state.ChallengeID)
	if err := s.syncPasskeyFlag(ctx, session.AppID, session.UserID); err != nil {
		return nil, err
	}
	s.invalidateSecurityCaches(ctx, session.AppID, session.UserID)
	s.notifySecurityEvent(ctx, session.AppID, session.UserID, "security", "Passkey 已添加", "账户已完成新的 Passkey 绑定。", "info")
	view := s.passkeyRecordToView(*record)
	return &view, nil
}

func (s *SecurityService) ListPasskeys(ctx context.Context, session *authdomain.Session) (*securitydomain.PasskeySummary, error) {
	items, err := s.pg.ListUserPasskeys(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	summary := s.buildPasskeySummary(items)
	return &summary, nil
}

func (s *SecurityService) RemovePasskey(ctx context.Context, session *authdomain.Session, credentialID string) error {
	if err := s.ensureModuleReady(securitydomain.ModulePasskey); err != nil {
		return err
	}
	rawID, err := credentialIDFromString(credentialID)
	if err != nil {
		return apperrors.New(40055, http.StatusBadRequest, "Passkey 标识无效")
	}
	deleted, err := s.pg.DeleteUserPasskey(ctx, session.AppID, session.UserID, rawID)
	if err != nil {
		return err
	}
	if deleted == 0 {
		return apperrors.New(40421, http.StatusNotFound, "Passkey 不存在")
	}
	if err := s.syncPasskeyFlag(ctx, session.AppID, session.UserID); err != nil {
		return err
	}
	s.invalidateSecurityCaches(ctx, session.AppID, session.UserID)
	s.notifySecurityEvent(ctx, session.AppID, session.UserID, "security", "Passkey 已移除", "账户的 Passkey 绑定已移除。", "warning")
	return nil
}

func (s *SecurityService) MaybeCreateSecondFactorChallenge(ctx context.Context, user *userdomain.User, provider, loginType, deviceID, ip, userAgent string) (*authdomain.LoginResult, error) {
	if user == nil {
		return nil, nil
	}
	record, err := s.pg.GetUserTOTPSecret(ctx, user.AppID, user.ID)
	if err != nil {
		return nil, err
	}
	if record == nil || !record.Enabled {
		return nil, nil
	}
	if !s.moduleEnabled(securitydomain.ModuleTOTP) {
		return nil, nil
	}
	if s.sessions == nil {
		return nil, apperrors.New(50320, http.StatusServiceUnavailable, "安全会话服务不可用")
	}
	cfg := s.currentConfig()
	methods := []string{"totp"}
	if s.recoveryModuleEnabled() {
		items, err := s.pg.ListUserRecoveryCodes(ctx, user.AppID, user.ID)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if item.UsedAt == nil {
				methods = append(methods, "recovery_code")
				break
			}
		}
	}
	now := time.Now().UTC()
	expiresAt := now.Add(cfg.ChallengeTTL)
	challenge := securitydomain.LoginChallenge{
		ChallengeID: newChallengeID("mfa"),
		AppID:       user.AppID,
		UserID:      user.ID,
		Account:     user.Account,
		Provider:    provider,
		LoginType:   loginType,
		DeviceID:    deviceID,
		IP:          ip,
		UserAgent:   userAgent,
		Methods:     methods,
		ExpiresAt:   expiresAt,
		CreatedAt:   now,
	}
	if err := s.sessions.SetTwoFactorChallenge(ctx, challenge, time.Until(expiresAt)); err != nil {
		return nil, err
	}
	return &authdomain.LoginResult{
		UserID:               user.ID,
		Account:              user.Account,
		Provider:             provider,
		RequiresSecondFactor: true,
		AuthenticationState:  "second_factor_required",
		Challenge: &authdomain.SecondFactorChallenge{
			ChallengeID: challenge.ChallengeID,
			State:       "pending",
			Methods:     methods,
			ExpiresAt:   expiresAt,
		},
	}, nil
}

func (s *SecurityService) VerifySecondFactorChallenge(ctx context.Context, challengeID string, code string, recoveryCode string) (*userdomain.User, *securitydomain.LoginChallenge, error) {
	if s.sessions == nil {
		return nil, nil, apperrors.New(50320, http.StatusServiceUnavailable, "安全会话服务不可用")
	}
	challenge, err := s.sessions.GetTwoFactorChallenge(ctx, strings.TrimSpace(challengeID))
	if err != nil {
		return nil, nil, err
	}
	if challenge == nil || challenge.ExpiresAt.Before(time.Now().UTC()) {
		return nil, nil, apperrors.New(40056, http.StatusBadRequest, "二次认证挑战不存在或已过期")
	}
	if err := s.verifyUserSecondFactor(ctx, challenge.AppID, challenge.UserID, code, recoveryCode); err != nil {
		return nil, nil, err
	}
	user, err := s.pg.GetUserByID(ctx, challenge.UserID)
	if err != nil {
		return nil, nil, err
	}
	if user == nil || user.AppID != challenge.AppID {
		return nil, nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	_ = s.sessions.DeleteTwoFactorChallenge(ctx, challenge.ChallengeID)
	return user, challenge, nil
}

func (s *SecurityService) BeginPasskeyLogin(ctx context.Context, appID int64) (*securitydomain.PasskeyLoginSession, *protocol.CredentialAssertion, error) {
	if err := s.ensureModuleReady(securitydomain.ModulePasskey); err != nil {
		return nil, nil, err
	}
	if s.sessions == nil {
		return nil, nil, apperrors.New(50320, http.StatusServiceUnavailable, "安全会话服务不可用")
	}
	if s.app != nil {
		if _, err := s.app.EnsureLoginAllowed(ctx, appID); err != nil {
			return nil, nil, err
		}
	}
	cfg := s.currentConfig()
	webauthn := s.currentWebAuthn()
	if webauthn == nil {
		return nil, nil, apperrors.New(50322, http.StatusServiceUnavailable, "当前安全模块暂不可用")
	}
	assertion, sessionData, err := webauthn.BeginDiscoverableLogin()
	if err != nil {
		return nil, nil, err
	}
	challengeID := newChallengeID("passkey_login")
	rawSession, err := gojson.Marshal(sessionData)
	if err != nil {
		return nil, nil, err
	}
	expiresAt := sessionData.Expires
	if expiresAt.IsZero() {
		expiresAt = time.Now().UTC().Add(cfg.Passkey.ChallengeTTL)
	}
	state := securitydomain.PasskeyLoginState{ChallengeID: challengeID, AppID: appID, SessionData: rawSession, ExpiresAt: expiresAt}
	if err := s.sessions.SetPasskeyLoginState(ctx, state, time.Until(expiresAt)); err != nil {
		return nil, nil, err
	}
	return &securitydomain.PasskeyLoginSession{ChallengeID: challengeID, AppID: appID, ExpiresAt: expiresAt}, assertion, nil
}

func (s *SecurityService) VerifyPasskeyLogin(ctx context.Context, appID int64, challengeID string, payload []byte) (*userdomain.User, error) {
	if err := s.ensureModuleReady(securitydomain.ModulePasskey); err != nil {
		return nil, err
	}
	if s.sessions == nil {
		return nil, apperrors.New(50320, http.StatusServiceUnavailable, "安全会话服务不可用")
	}
	webauthn := s.currentWebAuthn()
	if webauthn == nil {
		return nil, apperrors.New(50322, http.StatusServiceUnavailable, "当前安全模块暂不可用")
	}
	state, err := s.sessions.GetPasskeyLoginState(ctx, strings.TrimSpace(challengeID))
	if err != nil {
		return nil, err
	}
	if state == nil || state.AppID != appID || state.ExpiresAt.Before(time.Now().UTC()) {
		return nil, apperrors.New(40057, http.StatusBadRequest, "Passkey 登录会话已失效")
	}
	var sessionData webauthnlib.SessionData
	if err := gojson.Unmarshal(state.SessionData, &sessionData); err != nil {
		return nil, err
	}
	req, err := buildJSONRequest(ctx, payload)
	if err != nil {
		return nil, apperrors.New(40053, http.StatusBadRequest, "Passkey 凭证数据不能为空")
	}
	validatedUser, credential, err := webauthn.FinishPasskeyLogin(func(rawID []byte, userHandle []byte) (webauthnlib.User, error) {
		return s.lookupPasskeyLoginUser(ctx, appID, rawID, userHandle)
	}, sessionData, req)
	if err != nil {
		return nil, apperrors.New(40107, http.StatusUnauthorized, "Passkey 登录校验失败")
	}
	adapter, ok := validatedUser.(*webAuthnUser)
	if !ok || adapter.user == nil {
		return nil, apperrors.New(40107, http.StatusUnauthorized, "Passkey 登录校验失败")
	}
	rawCredential, err := gojson.Marshal(credential)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if err := s.pg.UpdateUserPasskeyCredential(ctx, appID, adapter.user.ID, credential.ID, rawCredential, credential.Authenticator.SignCount, &now); err != nil {
		return nil, err
	}
	_ = s.sessions.DeletePasskeyLoginState(ctx, state.ChallengeID)
	return adapter.user, nil
}

func (s *SecurityService) rotateRecoveryCodes(ctx context.Context, session *authdomain.Session, code string, recoveryCode string) (*securitydomain.RecoveryCodeIssueResult, error) {
	if err := s.ensureModuleReady(securitydomain.ModuleRecoveryCodes); err != nil {
		return nil, err
	}
	record, err := s.pg.GetUserTOTPSecret(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	if record == nil || !record.Enabled {
		return nil, apperrors.New(40058, http.StatusBadRequest, "双因子认证未启用")
	}
	if err := s.verifyUserSecondFactor(ctx, session.AppID, session.UserID, code, recoveryCode); err != nil {
		return nil, err
	}
	result, err := s.issueRecoveryCodes(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	s.invalidateSecurityCaches(ctx, session.AppID, session.UserID)
	return result, nil
}

func (s *SecurityService) issueRecoveryCodes(ctx context.Context, appID int64, userID int64) (*securitydomain.RecoveryCodeIssueResult, error) {
	if !s.recoveryModuleEnabled() {
		return nil, nil
	}
	cfg := s.currentConfig()
	codes, err := generateRecoveryCodes(cfg.RecoveryCode.Count, cfg.RecoveryCode.Length)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	records := make([]securitydomain.RecoveryCodeRecord, 0, len(codes))
	for _, code := range codes {
		records = append(records, securitydomain.RecoveryCodeRecord{
			UserID:   userID,
			AppID:    appID,
			CodeHash: hashRecoveryCode(code),
			CodeHint: recoveryCodeHint(code),
		})
	}
	if err := s.pg.ReplaceUserRecoveryCodes(ctx, appID, userID, records); err != nil {
		return nil, err
	}
	items, err := s.pg.ListUserRecoveryCodes(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	return &securitydomain.RecoveryCodeIssueResult{
		Total:       len(items),
		Remaining:   len(items),
		GeneratedAt: now,
		Codes:       codes,
		Items:       items,
	}, nil
}

func (s *SecurityService) verifyUserSecondFactor(ctx context.Context, appID int64, userID int64, code string, recoveryCode string) error {
	record, err := s.pg.GetUserTOTPSecret(ctx, appID, userID)
	if err != nil {
		return err
	}
	if record != nil && record.Enabled && strings.TrimSpace(code) != "" {
		return s.verifyTOTPRecord(ctx, record, code)
	}
	if strings.TrimSpace(recoveryCode) != "" {
		ok, err := s.consumeRecoveryCode(ctx, appID, userID, recoveryCode)
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		return apperrors.New(40108, http.StatusUnauthorized, "恢复码无效")
	}
	if record != nil && record.Enabled {
		return apperrors.New(40059, http.StatusBadRequest, "请输入双因子认证验证码")
	}
	return apperrors.New(40060, http.StatusBadRequest, "缺少可用的验证方式")
}

func (s *SecurityService) verifyTOTPRecord(ctx context.Context, record *securitydomain.TOTPSecretRecord, code string) error {
	secret, err := decryptSecret(s.currentEncryptionKey(), record.SecretCipher)
	if err != nil {
		return err
	}
	if err := s.verifyTOTPSecret(secret, code); err != nil {
		return err
	}
	now := time.Now().UTC()
	record.LastVerifiedAt = &now
	if err := s.pg.UpsertUserTOTPSecret(ctx, *record); err != nil {
		return err
	}
	return nil
}

func (s *SecurityService) verifyTOTPSecret(secret string, code string) error {
	cfg := s.currentConfig()
	valid, err := totp.ValidateCustom(strings.TrimSpace(code), secret, time.Now().UTC(), totp.ValidateOpts{
		Period:    30,
		Skew:      cfg.TOTP.Skew,
		Digits:    digitsFromInt(cfg.TOTP.Digits),
		Algorithm: otp.AlgorithmSHA1,
	})
	if err != nil {
		return err
	}
	if !valid {
		return apperrors.New(40109, http.StatusUnauthorized, "双因子认证验证码无效")
	}
	return nil
}

func (s *SecurityService) consumeRecoveryCode(ctx context.Context, appID int64, userID int64, code string) (bool, error) {
	if !s.recoveryModuleEnabled() {
		return false, nil
	}
	return s.pg.MarkRecoveryCodeUsed(ctx, appID, userID, hashRecoveryCode(code), time.Now().UTC())
}

func (s *SecurityService) loadUserProfile(ctx context.Context, appID int64, userID int64) (*userdomain.User, *userdomain.Profile, error) {
	user, err := s.pg.GetUserByID(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	if user == nil || user.AppID != appID {
		return nil, nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	profile, err := s.pg.GetUserProfileByUserID(ctx, userID)
	if err != nil {
		return nil, nil, err
	}
	return user, profile, nil
}

func (s *SecurityService) resolveTOTPAccountName(user *userdomain.User, profile *userdomain.Profile) string {
	if profile != nil && strings.TrimSpace(profile.Email) != "" {
		return strings.TrimSpace(profile.Email)
	}
	return strings.TrimSpace(user.Account)
}

func (s *SecurityService) patchSecurityFlags(ctx context.Context, userID int64, extra map[string]any) error {
	return s.pg.PatchUserProfileExtra(ctx, userID, extra)
}

func (s *SecurityService) syncPasskeyFlag(ctx context.Context, appID int64, userID int64) error {
	items, err := s.pg.ListUserPasskeys(ctx, appID, userID)
	if err != nil {
		return err
	}
	return s.patchSecurityFlags(ctx, userID, map[string]any{"passkey_enabled": len(items) > 0})
}

func (s *SecurityService) invalidateSecurityCaches(ctx context.Context, appID int64, userID int64) {
	if s.sessions == nil {
		return
	}
	_ = s.sessions.DeleteSecurityStatus(ctx, appID, userID)
	_ = s.sessions.DeleteMyView(ctx, appID, userID)
	_ = s.sessions.DeleteUserProfile(ctx, appID, userID)
}

func (s *SecurityService) buildRecoverySummary(items []securitydomain.RecoveryCodeRecord) securitydomain.RecoveryCodeSummary {
	summary := securitydomain.RecoveryCodeSummary{Enabled: len(items) > 0, Total: len(items), Items: items, Remaining: 0}
	for _, item := range items {
		if summary.GeneratedAt == nil {
			generatedAt := item.CreatedAt
			summary.GeneratedAt = &generatedAt
		}
		if item.UsedAt == nil {
			summary.Remaining++
		}
	}
	return summary
}

func (s *SecurityService) buildPasskeySummary(items []securitydomain.PasskeyRecord) securitydomain.PasskeySummary {
	views := make([]securitydomain.PasskeyView, 0, len(items))
	for _, item := range items {
		views = append(views, s.passkeyRecordToView(item))
	}
	return securitydomain.PasskeySummary{Enabled: len(views) > 0, Count: len(views), Items: views}
}

func (s *SecurityService) passkeyRecordToView(item securitydomain.PasskeyRecord) securitydomain.PasskeyView {
	return securitydomain.PasskeyView{
		ID:             item.ID,
		CredentialID:   credentialIDToString(item.CredentialID),
		CredentialName: item.CredentialName,
		SignCount:      item.SignCount,
		LastUsedAt:     item.LastUsedAt,
		CreatedAt:      item.CreatedAt,
		UpdatedAt:      item.UpdatedAt,
	}
}

func (s *SecurityService) makeWebAuthnUser(ctx context.Context, appID int64, userID int64) (*webAuthnUser, error) {
	user, profile, err := s.loadUserProfile(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	records, err := s.pg.ListUserPasskeys(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	credentials := make([]webauthnlib.Credential, 0, len(records))
	for _, record := range records {
		var credential webauthnlib.Credential
		if err := gojson.Unmarshal(record.CredentialJSON, &credential); err != nil {
			return nil, err
		}
		credentials = append(credentials, credential)
	}
	return &webAuthnUser{user: user, profile: profile, credentials: credentials}, nil
}

func (s *SecurityService) lookupPasskeyLoginUser(ctx context.Context, appID int64, rawID []byte, userHandle []byte) (webauthnlib.User, error) {
	record, err := s.pg.GetUserPasskeyByCredentialID(ctx, appID, rawID)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, apperrors.New(40110, http.StatusUnauthorized, "Passkey 凭证不存在")
	}
	adapter, err := s.makeWebAuthnUser(ctx, appID, record.UserID)
	if err != nil {
		return nil, err
	}
	if len(userHandle) > 0 && string(userHandle) != string(adapter.WebAuthnID()) {
		return nil, apperrors.New(40110, http.StatusUnauthorized, "Passkey 凭证不存在")
	}
	return adapter, nil
}

func (s *SecurityService) ensureModuleReady(key string) error {
	status, ok := s.registry.Status(key)
	if !ok || !status.Enabled {
		return apperrors.New(50321, http.StatusServiceUnavailable, "当前安全模块未启用")
	}
	if !status.Ready {
		return apperrors.New(50322, http.StatusServiceUnavailable, "当前安全模块暂不可用")
	}
	return nil
}

func (s *SecurityService) recoveryModuleEnabled() bool {
	return s.moduleEnabled(securitydomain.ModuleRecoveryCodes)
}

func (s *SecurityService) moduleEnabled(key string) bool {
	if s.registry == nil {
		return false
	}
	status, ok := s.registry.Status(key)
	return ok && status.Enabled && status.Ready
}

func (s *SecurityService) userVerificationRequirement() protocol.UserVerificationRequirement {
	cfg := s.currentConfig()
	return userVerificationRequirement(cfg.Passkey.UserVerification)
}

func userVerificationRequirement(value string) protocol.UserVerificationRequirement {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "required":
		return protocol.VerificationRequired
	case "discouraged":
		return protocol.VerificationDiscouraged
	default:
		return protocol.VerificationPreferred
	}
}

func cloneSecurityConfig(cfg config.SecurityConfig) config.SecurityConfig {
	cfg.Passkey.RPOrigins = cloneStrings(cfg.Passkey.RPOrigins)
	cfg.Passkey.RPTopOrigins = cloneStrings(cfg.Passkey.RPTopOrigins)
	return cfg
}

func (s *SecurityService) notifySecurityEvent(ctx context.Context, appID int64, userID int64, notificationType string, title string, content string, level string) {
	if s.pg == nil {
		return
	}
	if err := s.pg.CreateUserNotification(ctx, appID, userID, notificationType, title, content, level, map[string]any{"module": "security"}); err != nil {
		s.log.Warn("create security notification failed", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
	}
}

func extraBool(profile *userdomain.Profile, key string) bool {
	if profile == nil || profile.Extra == nil {
		return false
	}
	value, ok := profile.Extra[key]
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true") || strings.TrimSpace(typed) == "1"
	case float64:
		return typed != 0
	default:
		return false
	}
}

func extraInt(profile *userdomain.Profile, key string) int {
	if profile == nil || profile.Extra == nil {
		return 0
	}
	value, ok := profile.Extra[key]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		var result int
		_, _ = fmt.Sscanf(strings.TrimSpace(typed), "%d", &result)
		return result
	default:
		return 0
	}
}

func extraTime(profile *userdomain.Profile, key string) *time.Time {
	if profile == nil || profile.Extra == nil {
		return nil
	}
	value, ok := profile.Extra[key]
	if !ok {
		return nil
	}
	switch typed := value.(type) {
	case time.Time:
		parsed := typed.UTC()
		return &parsed
	case string:
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(typed))
		if err != nil {
			return nil
		}
		parsed = parsed.UTC()
		return &parsed
	default:
		return nil
	}
}

func (u *webAuthnUser) WebAuthnID() []byte {
	if u == nil || u.user == nil {
		return nil
	}
	return passkeyUserHandle(u.user.AppID, u.user.ID)
}

func (u *webAuthnUser) WebAuthnName() string {
	if u == nil || u.user == nil {
		return ""
	}
	return u.user.Account
}

func (u *webAuthnUser) WebAuthnDisplayName() string {
	if u == nil || u.user == nil {
		return ""
	}
	if u.profile != nil && strings.TrimSpace(u.profile.Nickname) != "" {
		return strings.TrimSpace(u.profile.Nickname)
	}
	return u.user.Account
}

func (u *webAuthnUser) WebAuthnCredentials() []webauthnlib.Credential {
	if u == nil {
		return nil
	}
	return u.credentials
}
