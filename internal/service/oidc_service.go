package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	systemdomain "aegis/internal/domain/system"
	apperrors "aegis/pkg/errors"

	"github.com/coreos/go-oidc/v3/oidc"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
)

// OIDCService 管理员 OIDC 认证服务（热重载配置）
type OIDCService struct {
	log           *zap.Logger
	mu            sync.RWMutex
	config        systemdomain.OIDCConfig
	encryptionKey []byte
	provider      *oidc.Provider
	providerURL   string // 缓存的 IssuerURL，变更时清空 provider
}

// NewOIDCService 创建 OIDC 服务
func NewOIDCService(log *zap.Logger, masterKey string) *OIDCService {
	return &OIDCService{
		log:           log,
		encryptionKey: securityKeyMaterial(masterKey),
	}
}

func (s *OIDCService) IsEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config.Enabled
}

func (s *OIDCService) CurrentConfig() systemdomain.OIDCConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config
}

func (s *OIDCService) Reload(cfg systemdomain.OIDCConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.providerURL != cfg.IssuerURL {
		s.provider = nil
		s.providerURL = ""
	}
	s.config = cfg
	s.log.Info("OIDC 配置已重载", zap.Bool("enabled", cfg.Enabled), zap.String("issuer", cfg.IssuerURL))
	return nil
}

func (s *OIDCService) EncryptClientSecret(plaintext string) (string, error) {
	return encryptSecret(s.encryptionKey, plaintext)
}

func (s *OIDCService) DecryptClientSecret(ciphertext string) (string, error) {
	return decryptSecret(s.encryptionKey, ciphertext)
}

// ensureProvider 惰性初始化 OIDC Provider（Discovery 缓存）
func (s *OIDCService) ensureProvider(ctx context.Context) (*oidc.Provider, *oauth2.Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg := s.config
	if !cfg.Enabled || cfg.IssuerURL == "" {
		return nil, nil, apperrors.New(40190, http.StatusBadRequest, "OIDC 认证未启用")
	}

	if s.provider == nil || s.providerURL != cfg.IssuerURL {
		provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
		if err != nil {
			return nil, nil, fmt.Errorf("oidc discovery: %w", err)
		}
		s.provider = provider
		s.providerURL = cfg.IssuerURL
	}

	secret, err := s.DecryptClientSecret(cfg.ClientSecret)
	if err != nil {
		return nil, nil, fmt.Errorf("decrypt client secret: %w", err)
	}

	oauth2Cfg := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: secret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     s.provider.Endpoint(),
		Scopes:       cfg.Scopes,
	}
	return s.provider, oauth2Cfg, nil
}

// AuthURL 生成 OIDC 授权 URL
func (s *OIDCService) AuthURL(ctx context.Context, state string) (string, error) {
	_, oauth2Cfg, err := s.ensureProvider(ctx)
	if err != nil {
		return "", err
	}
	return oauth2Cfg.AuthCodeURL(state), nil
}

// ExchangeAndVerify 用 authorization code 换取并验证 ID Token，返回用户信息
func (s *OIDCService) ExchangeAndVerify(ctx context.Context, code string) (*systemdomain.OIDCUser, error) {
	provider, oauth2Cfg, err := s.ensureProvider(ctx)
	if err != nil {
		return nil, err
	}

	token, err := oauth2Cfg.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("oidc code exchange: %w", err)
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return nil, apperrors.New(40193, http.StatusUnauthorized, "OIDC 响应中缺少 id_token")
	}

	verifier := provider.Verifier(&oidc.Config{ClientID: oauth2Cfg.ClientID})
	idToken, err := verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("oidc id_token verify: %w", err)
	}

	// 提取 claims
	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("oidc claims: %w", err)
	}

	s.mu.RLock()
	cfg := s.config
	s.mu.RUnlock()

	user := s.mapClaimsToUser(cfg, claims, idToken.Subject)

	// 邮箱域名白名单检查
	if len(cfg.AllowedDomains) > 0 && user.Email != "" {
		parts := strings.SplitN(user.Email, "@", 2)
		if len(parts) == 2 {
			domain := strings.ToLower(parts[1])
			allowed := false
			for _, d := range cfg.AllowedDomains {
				if strings.EqualFold(d, domain) {
					allowed = true
					break
				}
			}
			if !allowed {
				return nil, apperrors.New(40394, http.StatusForbidden, "邮箱域名不在允许列表中")
			}
		}
	}

	// 管理员组检查
	if cfg.AdminGroupClaim != "" && cfg.AdminGroupValue != "" {
		if !s.checkGroupClaim(claims, cfg.AdminGroupClaim, cfg.AdminGroupValue) {
			return nil, apperrors.New(40392, http.StatusForbidden, "OIDC 用户不在管理员组中")
		}
	}

	return user, nil
}

// TestDiscovery 测试 OIDC Discovery URL
func (s *OIDCService) TestDiscovery(ctx context.Context, issuerURL string) *systemdomain.OIDCTestResult {
	start := time.Now()
	result := &systemdomain.OIDCTestResult{}

	provider, err := oidc.NewProvider(ctx, issuerURL)
	if err != nil {
		result.Error = err.Error()
		result.LatencyMs = time.Since(start).Milliseconds()
		return result
	}
	result.DiscoveryOK = true

	// 读取 Discovery 文档端点
	var doc struct {
		Issuer                string   `json:"issuer"`
		AuthorizationEndpoint string   `json:"authorization_endpoint"`
		TokenEndpoint         string   `json:"token_endpoint"`
		UserInfoEndpoint      string   `json:"userinfo_endpoint"`
		JWKSURI               string   `json:"jwks_uri"`
		ScopesSupported       []string `json:"scopes_supported"`
	}
	raw, err := json.Marshal(provider)
	if err == nil {
		_ = json.Unmarshal(raw, &doc)
	}
	// go-oidc 的 Provider 不直接导出所有字段，通过 Claims 获取
	_ = provider.Claims(&doc)

	result.Issuer = doc.Issuer
	result.AuthEndpoint = doc.AuthorizationEndpoint
	result.TokenEndpoint = doc.TokenEndpoint
	result.UserInfoEndpoint = doc.UserInfoEndpoint
	result.JWKSEndpoint = doc.JWKSURI
	result.SupportedScopes = doc.ScopesSupported
	result.LatencyMs = time.Since(start).Milliseconds()
	return result
}

func (s *OIDCService) mapClaimsToUser(cfg systemdomain.OIDCConfig, claims map[string]any, subject string) *systemdomain.OIDCUser {
	user := &systemdomain.OIDCUser{Subject: subject}
	user.Account = claimString(claims, cfg.AttrMapping.Account)
	user.DisplayName = claimString(claims, cfg.AttrMapping.DisplayName)
	user.Email = claimString(claims, cfg.AttrMapping.Email)
	user.Phone = claimString(claims, cfg.AttrMapping.Phone)
	if user.Account == "" {
		user.Account = user.Email
	}
	if user.Account == "" {
		user.Account = subject
	}
	// 提取 groups
	if v, ok := claims["groups"]; ok {
		if arr, ok := v.([]any); ok {
			for _, g := range arr {
				if gs, ok := g.(string); ok {
					user.Groups = append(user.Groups, gs)
				}
			}
		}
	}
	return user
}

func (s *OIDCService) checkGroupClaim(claims map[string]any, claimKey, requiredValue string) bool {
	v, ok := claims[claimKey]
	if !ok {
		return false
	}
	switch val := v.(type) {
	case string:
		return strings.EqualFold(val, requiredValue)
	case []any:
		for _, item := range val {
			if s, ok := item.(string); ok && strings.EqualFold(s, requiredValue) {
				return true
			}
		}
	}
	return false
}

func claimString(claims map[string]any, key string) string {
	if key == "" {
		return ""
	}
	if v, ok := claims[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
