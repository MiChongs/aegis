package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	admindomain "aegis/internal/domain/admin"
	securitydomain "aegis/internal/domain/security"
	apperrors "aegis/pkg/errors"
	"github.com/go-webauthn/webauthn/protocol"
	webauthnlib "github.com/go-webauthn/webauthn/webauthn"
	gojson "github.com/goccy/go-json"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

type webAuthnAdmin struct {
	account     *admindomain.Account
	credentials []webauthnlib.Credential
}

func (s *SecurityService) GetAdminSecurityStatus(ctx context.Context, access *admindomain.AccessContext) (*admindomain.SecurityStatus, error) {
	account, err := s.requireAdminSecurityAccount(ctx, access)
	if err != nil {
		return nil, err
	}

	totpRecord, err := s.pg.GetAdminTOTPSecret(ctx, account.Account.ID)
	if err != nil {
		return nil, err
	}
	recoveryItems, err := s.pg.ListAdminRecoveryCodes(ctx, account.Account.ID)
	if err != nil {
		return nil, err
	}
	passkeyItems, err := s.pg.ListAdminPasskeys(ctx, account.Account.ID)
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

	recoverySummary := s.buildAdminRecoverySummary(recoveryItems)
	passkeySummary := s.buildAdminPasskeySummary(passkeyItems)
	return &admindomain.SecurityStatus{
		HasPassword:      account.PasswordHash != "",
		TwoFactorEnabled: twoFactor.Enabled,
		TwoFactorMethod:  twoFactor.Method,
		PasskeyEnabled:   passkeySummary.Count > 0,
		LastLoginAt:      account.Account.LastLoginAt,
		TwoFactor:        twoFactor,
		RecoveryCodes:    recoverySummary,
		Passkeys:         passkeySummary,
		Modules:          s.Modules(),
	}, nil
}

func (s *SecurityService) BeginAdminTOTPEnrollment(ctx context.Context, access *admindomain.AccessContext) (*securitydomain.TOTPEnrollment, error) {
	if err := s.ensureModuleReady(securitydomain.ModuleTOTP); err != nil {
		return nil, err
	}
	if s.sessions == nil {
		return nil, apperrors.New(50320, http.StatusServiceUnavailable, "安全会话服务不可用")
	}

	account, err := s.requireAdminSecurityAccount(ctx, access)
	if err != nil {
		return nil, err
	}
	existing, err := s.pg.GetAdminTOTPSecret(ctx, account.Account.ID)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.Enabled {
		return nil, apperrors.New(40920, http.StatusConflict, "双因子认证已启用")
	}

	cfg := s.currentConfig()
	accountName := s.resolveAdminTOTPAccountName(account)
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

	enrollmentID := newChallengeID("admin_totp")
	expiresAt := time.Now().UTC().Add(cfg.TOTP.EnrollmentTTL)
	state := securitydomain.TOTPEnrollmentState{
		EnrollmentID: enrollmentID,
		AppID:        0,
		UserID:       account.Account.ID,
		Secret:       key.Secret(),
		Issuer:       cfg.TOTP.Issuer,
		AccountName:  accountName,
		ExpiresAt:    expiresAt,
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

func (s *SecurityService) EnableAdminTOTP(ctx context.Context, access *admindomain.AccessContext, enrollmentID string, code string) (*securitydomain.TOTPStatus, *admindomain.RecoveryCodeIssueResult, error) {
	if err := s.ensureModuleReady(securitydomain.ModuleTOTP); err != nil {
		return nil, nil, err
	}
	if s.sessions == nil {
		return nil, nil, apperrors.New(50320, http.StatusServiceUnavailable, "安全会话服务不可用")
	}

	account, err := s.requireAdminSecurityAccount(ctx, access)
	if err != nil {
		return nil, nil, err
	}
	state, err := s.sessions.GetTOTPEnrollmentState(ctx, strings.TrimSpace(enrollmentID))
	if err != nil {
		return nil, nil, err
	}
	if state == nil || state.AppID != 0 || state.UserID != account.Account.ID || state.ExpiresAt.Before(time.Now().UTC()) {
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
	record := admindomain.TOTPSecretRecord{
		AdminID:        account.Account.ID,
		SecretCipher:   ciphertext,
		Issuer:         state.Issuer,
		AccountName:    state.AccountName,
		Enabled:        true,
		EnabledAt:      &now,
		LastVerifiedAt: &now,
	}
	if err := s.pg.UpsertAdminTOTPSecret(ctx, record); err != nil {
		return nil, nil, err
	}
	_ = s.sessions.DeleteTOTPEnrollmentState(ctx, state.EnrollmentID)

	recoveryResult, err := s.issueAdminRecoveryCodes(ctx, account.Account.ID)
	if err != nil {
		return nil, nil, err
	}
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

func (s *SecurityService) DisableAdminTOTP(ctx context.Context, access *admindomain.AccessContext, code string, recoveryCode string) (*securitydomain.TOTPStatus, error) {
	account, err := s.requireAdminSecurityAccount(ctx, access)
	if err != nil {
		return nil, err
	}
	record, err := s.pg.GetAdminTOTPSecret(ctx, account.Account.ID)
	if err != nil {
		return nil, err
	}
	if record == nil || !record.Enabled {
		return nil, apperrors.New(40051, http.StatusBadRequest, "双因子认证未启用")
	}
	if err := s.verifyAdminSecondFactor(ctx, account.Account.ID, code, recoveryCode); err != nil {
		return nil, err
	}
	if err := s.pg.DeleteAdminTOTPSecret(ctx, account.Account.ID); err != nil {
		return nil, err
	}
	if err := s.pg.DeleteAdminRecoveryCodes(ctx, account.Account.ID); err != nil {
		return nil, err
	}
	return &securitydomain.TOTPStatus{Enabled: false, Method: "totp", Issuer: record.Issuer, AccountName: record.AccountName}, nil
}

func (s *SecurityService) ListAdminRecoveryCodes(ctx context.Context, access *admindomain.AccessContext) (*admindomain.RecoveryCodeSummary, error) {
	account, err := s.requireAdminSecurityAccount(ctx, access)
	if err != nil {
		return nil, err
	}
	items, err := s.pg.ListAdminRecoveryCodes(ctx, account.Account.ID)
	if err != nil {
		return nil, err
	}
	summary := s.buildAdminRecoverySummary(items)
	return &summary, nil
}

func (s *SecurityService) GenerateAdminRecoveryCodes(ctx context.Context, access *admindomain.AccessContext, code string, recoveryCode string) (*admindomain.RecoveryCodeIssueResult, error) {
	return s.rotateAdminRecoveryCodes(ctx, access, code, recoveryCode)
}

func (s *SecurityService) RegenerateAdminRecoveryCodes(ctx context.Context, access *admindomain.AccessContext, code string, recoveryCode string) (*admindomain.RecoveryCodeIssueResult, error) {
	return s.rotateAdminRecoveryCodes(ctx, access, code, recoveryCode)
}

func (s *SecurityService) BeginAdminPasskeyRegistration(ctx context.Context, access *admindomain.AccessContext) (*securitydomain.PasskeyRegistrationSession, *protocol.CredentialCreation, error) {
	if err := s.ensureModuleReady(securitydomain.ModulePasskey); err != nil {
		return nil, nil, err
	}
	if s.sessions == nil {
		return nil, nil, apperrors.New(50320, http.StatusServiceUnavailable, "安全会话服务不可用")
	}

	account, err := s.requireAdminSecurityAccount(ctx, access)
	if err != nil {
		return nil, nil, err
	}
	webauthn := s.currentWebAuthn()
	if webauthn == nil {
		return nil, nil, apperrors.New(50322, http.StatusServiceUnavailable, "当前安全模块暂不可用")
	}
	adapter, err := s.makeAdminWebAuthnUser(ctx, account.Account.ID)
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

	cfg := s.currentConfig()
	challengeID := newChallengeID("admin_passkey_reg")
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
		AppID:       0,
		UserID:      account.Account.ID,
		SessionData: rawSession,
		ExpiresAt:   expiresAt,
	}
	if err := s.sessions.SetPasskeyRegistrationState(ctx, state, time.Until(expiresAt)); err != nil {
		return nil, nil, err
	}
	return &securitydomain.PasskeyRegistrationSession{ChallengeID: challengeID, AppID: 0, UserID: account.Account.ID, ExpiresAt: expiresAt}, creation, nil
}

func (s *SecurityService) FinishAdminPasskeyRegistration(ctx context.Context, access *admindomain.AccessContext, challengeID string, payload []byte, credentialName string) (*securitydomain.PasskeyView, error) {
	if err := s.ensureModuleReady(securitydomain.ModulePasskey); err != nil {
		return nil, err
	}
	if s.sessions == nil {
		return nil, apperrors.New(50320, http.StatusServiceUnavailable, "安全会话服务不可用")
	}

	account, err := s.requireAdminSecurityAccount(ctx, access)
	if err != nil {
		return nil, err
	}
	state, err := s.sessions.GetPasskeyRegistrationState(ctx, strings.TrimSpace(challengeID))
	if err != nil {
		return nil, err
	}
	if state == nil || state.AppID != 0 || state.UserID != account.Account.ID || state.ExpiresAt.Before(time.Now().UTC()) {
		return nil, apperrors.New(40052, http.StatusBadRequest, "Passkey 注册会话已失效")
	}

	var sessionData webauthnlib.SessionData
	if err := gojson.Unmarshal(state.SessionData, &sessionData); err != nil {
		return nil, err
	}
	adapter, err := s.makeAdminWebAuthnUser(ctx, account.Account.ID)
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
	record, err := s.pg.CreateAdminPasskey(ctx, admindomain.PasskeyRecord{
		AdminID:        account.Account.ID,
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
	view := s.adminPasskeyRecordToView(*record)
	return &view, nil
}

func (s *SecurityService) ListAdminPasskeys(ctx context.Context, access *admindomain.AccessContext) (*securitydomain.PasskeySummary, error) {
	account, err := s.requireAdminSecurityAccount(ctx, access)
	if err != nil {
		return nil, err
	}
	items, err := s.pg.ListAdminPasskeys(ctx, account.Account.ID)
	if err != nil {
		return nil, err
	}
	summary := s.buildAdminPasskeySummary(items)
	return &summary, nil
}

func (s *SecurityService) RemoveAdminPasskey(ctx context.Context, access *admindomain.AccessContext, credentialID string) error {
	if err := s.ensureModuleReady(securitydomain.ModulePasskey); err != nil {
		return err
	}
	account, err := s.requireAdminSecurityAccount(ctx, access)
	if err != nil {
		return err
	}
	rawID, err := credentialIDFromString(credentialID)
	if err != nil {
		return apperrors.New(40055, http.StatusBadRequest, "Passkey 标识无效")
	}
	deleted, err := s.pg.DeleteAdminPasskey(ctx, account.Account.ID, rawID)
	if err != nil {
		return err
	}
	if deleted == 0 {
		return apperrors.New(40421, http.StatusNotFound, "Passkey 不存在")
	}
	return nil
}

func (s *SecurityService) rotateAdminRecoveryCodes(ctx context.Context, access *admindomain.AccessContext, code string, recoveryCode string) (*admindomain.RecoveryCodeIssueResult, error) {
	if err := s.ensureModuleReady(securitydomain.ModuleRecoveryCodes); err != nil {
		return nil, err
	}
	account, err := s.requireAdminSecurityAccount(ctx, access)
	if err != nil {
		return nil, err
	}
	record, err := s.pg.GetAdminTOTPSecret(ctx, account.Account.ID)
	if err != nil {
		return nil, err
	}
	if record == nil || !record.Enabled {
		return nil, apperrors.New(40058, http.StatusBadRequest, "双因子认证未启用")
	}
	if err := s.verifyAdminSecondFactor(ctx, account.Account.ID, code, recoveryCode); err != nil {
		return nil, err
	}
	return s.issueAdminRecoveryCodes(ctx, account.Account.ID)
}

func (s *SecurityService) issueAdminRecoveryCodes(ctx context.Context, adminID int64) (*admindomain.RecoveryCodeIssueResult, error) {
	if !s.recoveryModuleEnabled() {
		return nil, nil
	}
	cfg := s.currentConfig()
	codes, err := generateRecoveryCodes(cfg.RecoveryCode.Count, cfg.RecoveryCode.Length)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	records := make([]admindomain.RecoveryCodeRecord, 0, len(codes))
	for _, code := range codes {
		records = append(records, admindomain.RecoveryCodeRecord{
			AdminID:  adminID,
			CodeHash: hashRecoveryCode(code),
			CodeHint: recoveryCodeHint(code),
		})
	}
	if err := s.pg.ReplaceAdminRecoveryCodes(ctx, adminID, records); err != nil {
		return nil, err
	}
	items, err := s.pg.ListAdminRecoveryCodes(ctx, adminID)
	if err != nil {
		return nil, err
	}
	return &admindomain.RecoveryCodeIssueResult{
		Total:       len(items),
		Remaining:   len(items),
		GeneratedAt: now,
		Codes:       codes,
		Items:       items,
	}, nil
}

func (s *SecurityService) verifyAdminSecondFactor(ctx context.Context, adminID int64, code string, recoveryCode string) error {
	record, err := s.pg.GetAdminTOTPSecret(ctx, adminID)
	if err != nil {
		return err
	}
	if record != nil && record.Enabled && strings.TrimSpace(code) != "" {
		return s.verifyAdminTOTPRecord(ctx, record, code)
	}
	if strings.TrimSpace(recoveryCode) != "" {
		ok, err := s.consumeAdminRecoveryCode(ctx, adminID, recoveryCode)
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

func (s *SecurityService) verifyAdminTOTPRecord(ctx context.Context, record *admindomain.TOTPSecretRecord, code string) error {
	secret, err := decryptSecret(s.currentEncryptionKey(), record.SecretCipher)
	if err != nil {
		return err
	}
	if err := s.verifyTOTPSecret(secret, code); err != nil {
		return err
	}
	now := time.Now().UTC()
	record.LastVerifiedAt = &now
	return s.pg.UpsertAdminTOTPSecret(ctx, *record)
}

func (s *SecurityService) consumeAdminRecoveryCode(ctx context.Context, adminID int64, code string) (bool, error) {
	if !s.recoveryModuleEnabled() {
		return false, nil
	}
	return s.pg.MarkAdminRecoveryCodeUsed(ctx, adminID, hashRecoveryCode(code), time.Now().UTC())
}

func (s *SecurityService) requireAdminSecurityAccount(ctx context.Context, access *admindomain.AccessContext) (*admindomain.AuthRecord, error) {
	if access == nil || access.AdminID <= 0 {
		return nil, apperrors.New(40312, http.StatusForbidden, "当前会话不支持账户安全管理")
	}
	account, err := s.pg.GetAdminAuthByID(ctx, access.AdminID)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, apperrors.New(40450, http.StatusNotFound, "管理员不存在")
	}
	if account.Account.Status != "active" {
		return nil, apperrors.New(40310, http.StatusForbidden, "管理员账户不可用")
	}
	return account, nil
}

func (s *SecurityService) resolveAdminTOTPAccountName(account *admindomain.AuthRecord) string {
	if account != nil && strings.TrimSpace(account.Account.Email) != "" {
		return strings.TrimSpace(account.Account.Email)
	}
	if account != nil {
		return strings.TrimSpace(account.Account.Account)
	}
	return ""
}

func (s *SecurityService) buildAdminRecoverySummary(items []admindomain.RecoveryCodeRecord) admindomain.RecoveryCodeSummary {
	summary := admindomain.RecoveryCodeSummary{Enabled: len(items) > 0, Total: len(items), Items: items, Remaining: 0}
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

func (s *SecurityService) buildAdminPasskeySummary(items []admindomain.PasskeyRecord) securitydomain.PasskeySummary {
	views := make([]securitydomain.PasskeyView, 0, len(items))
	for _, item := range items {
		views = append(views, s.adminPasskeyRecordToView(item))
	}
	return securitydomain.PasskeySummary{Enabled: len(views) > 0, Count: len(views), Items: views}
}

func (s *SecurityService) adminPasskeyRecordToView(item admindomain.PasskeyRecord) securitydomain.PasskeyView {
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

func (s *SecurityService) makeAdminWebAuthnUser(ctx context.Context, adminID int64) (*webAuthnAdmin, error) {
	account, err := s.pg.GetAdminAccessByID(ctx, adminID)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, apperrors.New(40450, http.StatusNotFound, "管理员不存在")
	}
	records, err := s.pg.ListAdminPasskeys(ctx, adminID)
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
	return &webAuthnAdmin{account: &account.Account, credentials: credentials}, nil
}

func (u *webAuthnAdmin) WebAuthnID() []byte {
	if u == nil || u.account == nil {
		return nil
	}
	return adminPasskeyUserHandle(u.account.ID)
}

func (u *webAuthnAdmin) WebAuthnName() string {
	if u == nil || u.account == nil {
		return ""
	}
	return u.account.Account
}

func (u *webAuthnAdmin) WebAuthnDisplayName() string {
	if u == nil || u.account == nil {
		return ""
	}
	if strings.TrimSpace(u.account.DisplayName) != "" {
		return strings.TrimSpace(u.account.DisplayName)
	}
	return u.account.Account
}

func (u *webAuthnAdmin) WebAuthnCredentials() []webauthnlib.Credential {
	if u == nil {
		return nil
	}
	return u.credentials
}
