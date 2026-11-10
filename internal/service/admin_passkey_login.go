package service

import (
	"context"
	"net/http"

	admindomain "aegis/internal/domain/admin"
	plugindomain "aegis/internal/domain/plugin"
	securitydomain "aegis/internal/domain/security"
	apperrors "aegis/pkg/errors"
	"github.com/go-webauthn/webauthn/protocol"
)

// BeginPasskeyLogin 控制台登录页发起通行密钥登录。
//
// 风控在这一步先评估一次（只有 IP 与 UA）：被拦下的来源连挑战都拿不到，浏览器也就不会弹出选择框。
func (s *AdminService) BeginPasskeyLogin(ctx context.Context, ip, userAgent string) (*securitydomain.PasskeyLoginSession, *protocol.CredentialAssertion, error) {
	if s.security == nil {
		return nil, nil, apperrors.New(50002, http.StatusInternalServerError, "安全服务未初始化")
	}
	if err := s.guardAdminLogin(ctx, ip, userAgent, ""); err != nil {
		return nil, nil, err
	}
	return s.security.BeginAdminPasskeyLogin(ctx)
}

// LoginWithPasskey 用通行密钥登录：校验断言 → 认出是哪位管理员 → 风控（带上账号）→ 插件登录前检查 → 签发会话。
//
// 与密码登录的区别只在「怎么证明是本人」：通行密钥要求设备上的用户验证，本身就是多因素，
// 所以通过后直接签发会话，不再要 TOTP；账号停用、风控与插件的拦截照旧生效。
// 返回的 LoginResult.Admin 在失败时也尽量带上已认出的管理员，供审计记账号。
func (s *AdminService) LoginWithPasskey(ctx context.Context, challengeID string, payload []byte, ip, userAgent string) (*admindomain.LoginResult, error) {
	if s.security == nil {
		return nil, apperrors.New(50002, http.StatusInternalServerError, "安全服务未初始化")
	}
	adminID, err := s.security.VerifyAdminPasskeyLogin(ctx, challengeID, payload)
	if err != nil {
		return nil, err
	}
	profile, err := s.pg.GetAdminAccessByID(ctx, adminID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, apperrors.New(40450, http.StatusNotFound, "管理员不存在")
	}
	identified := &admindomain.LoginResult{Admin: profile.Account}
	if profile.Account.Status != "active" {
		return identified, apperrors.New(40310, http.StatusForbidden, "管理员账户不可用")
	}
	account := profile.Account.Account
	if err := s.guardAdminLogin(ctx, ip, userAgent, account); err != nil {
		return identified, err
	}
	if s.plugin != nil {
		hookResult := s.plugin.ExecuteHook(ctx, HookAuthPreLogin, map[string]any{
			"account": account, "method": "passkey",
		}, plugindomain.HookMetadata{AdminID: &adminID})
		if !hookResult.Allow {
			msg := hookResult.Message
			if msg == "" {
				msg = "插件拒绝了此登录请求"
			}
			return identified, apperrors.New(40399, http.StatusForbidden, msg)
		}
	}

	result, err := s.issueSession(ctx, profile, ip, userAgent)
	if err != nil {
		return identified, err
	}
	if s.plugin != nil {
		go s.plugin.ExecuteHook(context.Background(), HookAuthSessionIssued, map[string]any{
			"adminId": adminID, "account": account, "method": "passkey",
		}, plugindomain.HookMetadata{AdminID: &adminID})
	}
	return result, nil
}

// guardAdminLogin 管理员登录场景的风控评估（与密码登录同一场景、同一套规则）。
func (s *AdminService) guardAdminLogin(ctx context.Context, ip, userAgent, account string) error {
	if s.risk == nil {
		return nil
	}
	_, err := s.risk.Guard(ctx, securitydomain.RiskEvalRequest{
		Scene: securitydomain.SceneAdminLogin, IP: ip, UserAgent: userAgent, Account: account,
		Extra: map[string]any{"admin": true, "method": "passkey"},
	}, RiskGuardOption{Message: "当前请求被风控拦截"})
	return err
}
