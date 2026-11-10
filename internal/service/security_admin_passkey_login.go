package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	securitydomain "aegis/internal/domain/security"
	apperrors "aegis/pkg/errors"
	"github.com/go-webauthn/webauthn/protocol"
	webauthnlib "github.com/go-webauthn/webauthn/webauthn"
	gojson "github.com/goccy/go-json"
	"go.uber.org/zap"
)

// 管理员通行密钥登录（控制台登录页）。
//
// 用可发现凭据（discoverable credential）：注册时已要求 resident key（见 BeginAdminPasskeyRegistration），
// 所以登录不用先填账号 —— 浏览器列出本域的通行密钥，选中即登录，凭据 ID 与 user handle 反查是哪位管理员。
//
// 通行密钥本身就是多因素（持有设备 + 设备上的指纹 / 面容 / PIN），这里把用户验证定为**必须**，
// 通过后直接签发会话、不再走 TOTP；没有用户验证的断言（例如未设 PIN 的安全钥匙）会被拒。
//
// 挑战与应用用户的通行密钥登录共用同一个存储（AppID=0 表示管理员），靠 ID 前缀与 AppID 双重区分：
// 应用侧校验要求 AppID 等于自己的应用（应用 ID 从 10000 起，不会是 0），管理员侧只认本前缀且 AppID=0。

const adminPasskeyLoginPrefix = "admin_passkey_login"

// BeginAdminPasskeyLogin 发起一次管理员通行密钥登录：返回挑战 ID 与给浏览器 navigator.credentials.get 的选项。
func (s *SecurityService) BeginAdminPasskeyLogin(ctx context.Context) (*securitydomain.PasskeyLoginSession, *protocol.CredentialAssertion, error) {
	if err := s.ensureModuleReady(securitydomain.ModulePasskey); err != nil {
		return nil, nil, err
	}
	if s.sessions == nil {
		return nil, nil, apperrors.New(50320, http.StatusServiceUnavailable, "安全会话服务不可用")
	}
	webauthn, err := s.webAuthnForRequest(ctx)
	if err != nil {
		return nil, nil, err
	}
	assertion, sessionData, err := webauthn.BeginDiscoverableLogin(
		webauthnlib.WithUserVerification(protocol.VerificationRequired),
	)
	if err != nil {
		return nil, nil, err
	}
	rawSession, err := gojson.Marshal(sessionData)
	if err != nil {
		return nil, nil, err
	}
	expiresAt := sessionData.Expires
	if expiresAt.IsZero() {
		expiresAt = time.Now().UTC().Add(s.currentConfig().Passkey.ChallengeTTL)
	}
	challengeID := newChallengeID(adminPasskeyLoginPrefix)
	state := securitydomain.PasskeyLoginState{ChallengeID: challengeID, AppID: 0, SessionData: rawSession, ExpiresAt: expiresAt}
	if err := s.sessions.SetPasskeyLoginState(ctx, state, time.Until(expiresAt)); err != nil {
		return nil, nil, err
	}
	return &securitydomain.PasskeyLoginSession{ChallengeID: challengeID, AppID: 0, ExpiresAt: expiresAt}, assertion, nil
}

// VerifyAdminPasskeyLogin 校验浏览器返回的断言，通过时返回管理员 ID 并回写凭据的签名计数与最近使用时间。
//
// 挑战在校验前就被取走（GETDEL）：同一份断言并发提交两次只有一次能走到校验，失败也要重新发起。
func (s *SecurityService) VerifyAdminPasskeyLogin(ctx context.Context, challengeID string, payload []byte) (int64, error) {
	if err := s.ensureModuleReady(securitydomain.ModulePasskey); err != nil {
		return 0, err
	}
	if s.sessions == nil {
		return 0, apperrors.New(50320, http.StatusServiceUnavailable, "安全会话服务不可用")
	}
	challengeID = strings.TrimSpace(challengeID)
	expired := apperrors.New(40057, http.StatusBadRequest, "通行密钥登录已过期，请重试")
	if !strings.HasPrefix(challengeID, adminPasskeyLoginPrefix) {
		return 0, expired
	}
	state, err := s.sessions.TakePasskeyLoginState(ctx, challengeID)
	if err != nil {
		return 0, err
	}
	if state == nil || state.AppID != 0 || state.ExpiresAt.Before(time.Now().UTC()) {
		return 0, expired
	}

	var sessionData webauthnlib.SessionData
	if err := gojson.Unmarshal(state.SessionData, &sessionData); err != nil {
		return 0, err
	}
	webauthn, err := s.webAuthnForCeremony(ctx, sessionData.RelyingPartyID)
	if err != nil {
		return 0, err
	}
	req, err := buildJSONRequest(ctx, payload)
	if err != nil {
		return 0, apperrors.New(40053, http.StatusBadRequest, "Passkey 凭证数据不能为空")
	}
	rejected := apperrors.New(40107, http.StatusUnauthorized, "通行密钥校验失败")
	validated, credential, err := webauthn.FinishPasskeyLogin(func(rawID []byte, userHandle []byte) (webauthnlib.User, error) {
		return s.lookupAdminPasskeyUser(ctx, rawID, userHandle)
	}, sessionData, req)
	if err != nil {
		var appErr *apperrors.AppError
		if errors.As(err, &appErr) {
			return 0, err
		}
		s.log.Info("admin passkey login rejected",
			zap.String("rpId", sessionData.RelyingPartyID), zap.String("detail", passkeyFailureDetail(err)))
		return 0, rejected
	}
	adapter, ok := validated.(*webAuthnAdmin)
	if !ok || adapter.account == nil {
		return 0, rejected
	}
	rawCredential, err := gojson.Marshal(credential)
	if err != nil {
		return 0, err
	}
	if err := s.pg.UpdateAdminPasskeyCredential(ctx, adapter.account.ID, credential.ID, rawCredential,
		credential.Authenticator.SignCount, time.Now().UTC()); err != nil {
		return 0, err
	}
	return adapter.account.ID, nil
}

// lookupAdminPasskeyUser 从凭据 ID 反查管理员；浏览器带了 user handle 时必须与这位管理员一致。
// 认不出的凭据（已删除、或是应用用户的通行密钥）一律报同一句，不暴露区别。
func (s *SecurityService) lookupAdminPasskeyUser(ctx context.Context, rawID []byte, userHandle []byte) (webauthnlib.User, error) {
	unknown := apperrors.New(40107, http.StatusUnauthorized, "这把通行密钥未绑定管理员账号")
	record, err := s.pg.GetAdminPasskeyByCredentialID(ctx, rawID)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, unknown
	}
	adapter, err := s.makeAdminWebAuthnUser(ctx, record.AdminID)
	if err != nil {
		return nil, err
	}
	if len(userHandle) > 0 && string(userHandle) != string(adapter.WebAuthnID()) {
		return nil, unknown
	}
	return adapter, nil
}
