package service

import (
	"context"
	"strings"
	"testing"

	authdomain "aegis/internal/domain/auth"
	securitydomain "aegis/internal/domain/security"
	"aegis/pkg/clientip"
	riskevents "aegis/services/risk/events"
)

func TestFillRiskEnvFromContext(t *testing.T) {
	ctx := clientip.WithIP(context.Background(), "2409:8962:5a6:57::1")
	ctx = authdomain.WithSession(ctx, &authdomain.Session{AppID: 10000, UserID: 7, Account: "alice", DeviceID: "dev-1", UserAgent: "UA"})

	req := securitydomain.RiskEvalRequest{Scene: securitydomain.ScenePasswordChange}
	fillRiskEnv(ctx, &req)
	if req.IP != "2409:8962:5a6:57::1" || req.DeviceID != "dev-1" || req.UserAgent != "UA" || req.Account != "alice" {
		t.Fatalf("env not filled: %+v", req)
	}
	if req.AppID == nil || *req.AppID != 10000 || req.UserID == nil || *req.UserID != 7 {
		t.Fatalf("app / user not filled: %+v", req)
	}

	// 显式传入的不覆盖；评估的是别的用户时不把会话里的账号塞进去
	other := int64(8)
	req = securitydomain.RiskEvalRequest{IP: "1.2.3.4", UserID: &other}
	fillRiskEnv(ctx, &req)
	if req.IP != "1.2.3.4" || *req.UserID != 8 || req.Account != "" {
		t.Fatalf("explicit values overwritten: %+v", req)
	}

	e := riskevents.Event{Type: riskevents.TypePasswordChange}
	fillEventEnv(ctx, &e)
	if e.IP == "" || e.AppID != 10000 || e.UserID != 7 || e.DeviceID != "dev-1" || e.UA != "UA" || e.Account != "alice" {
		t.Fatalf("event env not filled: %+v", e)
	}
}

func TestIdentityPropsDigestOnly(t *testing.T) {
	p := identityProps(authdomain.ProviderProfile{Provider: "QQ", ProviderUserID: "OPENID123", UnionID: "UNION456", Nickname: "川意"})
	if p[riskevents.PropIdentityKey] != riskevents.Digest("UNION456") || p[riskevents.PropIdentityProvider] != "qq" {
		t.Fatalf("unexpected props: %v", p)
	}
	for _, v := range p {
		if s, ok := v.(string); ok && (strings.Contains(s, "OPENID123") || strings.Contains(s, "UNION456")) {
			t.Fatalf("raw identifier leaked: %v", p)
		}
	}
	p = identityProps(authdomain.ProviderProfile{Provider: "google", ProviderUserID: "sub-1"})
	if p[riskevents.PropIdentityKey] != riskevents.Digest("sub-1") {
		t.Fatalf("openid fallback: %v", p)
	}
}
