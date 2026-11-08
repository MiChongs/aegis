package service

import (
	"context"
	"strings"

	authdomain "aegis/internal/domain/auth"
	securitydomain "aegis/internal/domain/security"
	riskevents "aegis/services/risk/events"
)

// 风控接入点的辅助：AuthService 直接持有 *RiskService（不是 riskHook），这里补一层「没接风控就放行」。
// 请求环境（客户端 IP、登录会话的设备与 UA）由 RiskService 从请求上下文自动补齐（fillRiskEnv / fillEventEnv）。

func (s *AuthService) riskCheck(ctx context.Context, req securitydomain.RiskEvalRequest, message string) error {
	if s == nil || s.risk == nil {
		return nil
	}
	_, err := s.risk.Guard(ctx, req, RiskGuardOption{Message: message})
	return err
}

func (s *AuthService) riskEmit(ctx context.Context, e riskevents.Event) {
	if s == nil || s.risk == nil {
		return
	}
	s.risk.Report(ctx, e)
}

// identityProps 第三方身份放进事件的属性：只带摘要，不带原始 openid / unionid。
// 主体取 unionid（同一开放平台下跨应用不变），没有时取 openid——与关系图回灌的取法一致。
func identityProps(p authdomain.ProviderProfile) map[string]any {
	subject := strings.TrimSpace(p.UnionID)
	if subject == "" {
		subject = strings.TrimSpace(p.ProviderUserID)
	}
	if subject == "" || p.Provider == "" {
		return map[string]any{"provider": p.Provider}
	}
	out := map[string]any{
		"provider":                      p.Provider,
		riskevents.PropIdentityProvider: strings.ToLower(p.Provider),
		riskevents.PropIdentityKey:      riskevents.Digest(subject),
	}
	if n := strings.TrimSpace(p.Nickname); n != "" {
		out[riskevents.PropIdentityNickname] = n
	}
	return out
}
