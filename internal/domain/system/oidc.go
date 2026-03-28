package system

import "time"

// OIDCConfig OIDC 认证配置（持久化到 platform_settings 表）
type OIDCConfig struct {
	Enabled         bool     `json:"enabled"`
	IssuerURL       string   `json:"issuerURL"`
	ClientID        string   `json:"clientID"`
	ClientSecret    string   `json:"clientSecret"` // AES-GCM 加密存储
	RedirectURL     string   `json:"redirectURL"`
	Scopes          []string `json:"scopes"`
	AllowedDomains  []string `json:"allowedDomains,omitempty"`
	AdminGroupClaim string   `json:"adminGroupClaim,omitempty"`
	AdminGroupValue string   `json:"adminGroupValue,omitempty"`
	AttrMapping         OIDCAttributeMapping `json:"attrMapping"`
	FallbackToLocal     bool     `json:"fallbackToLocal"`
	FrontendCallbackURL string   `json:"frontendCallbackURL"` // 如 http://localhost:3000/login/oidc-callback
}

// OIDCAttributeMapping OIDC claims 到管理员字段的映射
type OIDCAttributeMapping struct {
	Account     string `json:"account"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
	Phone       string `json:"phone"`
}

// OIDCSettingsView 返回给前端的视图（脱敏 ClientSecret）
type OIDCSettingsView struct {
	Enabled         bool                 `json:"enabled"`
	IssuerURL       string               `json:"issuerURL"`
	ClientID        string               `json:"clientID"`
	HasClientSecret bool                 `json:"hasClientSecret"`
	RedirectURL     string               `json:"redirectURL"`
	Scopes          []string             `json:"scopes"`
	AllowedDomains  []string             `json:"allowedDomains,omitempty"`
	AdminGroupClaim string               `json:"adminGroupClaim,omitempty"`
	AdminGroupValue string               `json:"adminGroupValue,omitempty"`
	AttrMapping     OIDCAttributeMapping `json:"attrMapping"`
	FallbackToLocal     bool                 `json:"fallbackToLocal"`
	FrontendCallbackURL string               `json:"frontendCallbackURL"`
	Source              string               `json:"source"`
	UpdatedBy       *int64               `json:"updatedBy,omitempty"`
	UpdatedAt       *time.Time           `json:"updatedAt,omitempty"`
}

// OIDCSettingsPatch 前端更新请求
type OIDCSettingsPatch struct {
	Enabled         *bool                        `json:"enabled,omitempty"`
	IssuerURL       *string                      `json:"issuerURL,omitempty"`
	ClientID        *string                      `json:"clientID,omitempty"`
	ClientSecret    *string                      `json:"clientSecret,omitempty"`
	RedirectURL     *string                      `json:"redirectURL,omitempty"`
	Scopes          *[]string                    `json:"scopes,omitempty"`
	AllowedDomains  *[]string                    `json:"allowedDomains,omitempty"`
	AdminGroupClaim *string                      `json:"adminGroupClaim,omitempty"`
	AdminGroupValue *string                      `json:"adminGroupValue,omitempty"`
	AttrMapping         *OIDCAttributeMappingPatch   `json:"attrMapping,omitempty"`
	FallbackToLocal     *bool                        `json:"fallbackToLocal,omitempty"`
	FrontendCallbackURL *string                      `json:"frontendCallbackURL,omitempty"`
}

// OIDCAttributeMappingPatch 属性映射更新
type OIDCAttributeMappingPatch struct {
	Account     *string `json:"account,omitempty"`
	DisplayName *string `json:"displayName,omitempty"`
	Email       *string `json:"email,omitempty"`
	Phone       *string `json:"phone,omitempty"`
}

// OIDCTestRequest Discovery 测试请求
type OIDCTestRequest struct {
	IssuerURL string `json:"issuerURL"`
}

// OIDCTestResult Discovery 测试结果
type OIDCTestResult struct {
	DiscoveryOK      bool     `json:"discoveryOK"`
	Issuer           string   `json:"issuer,omitempty"`
	AuthEndpoint     string   `json:"authEndpoint,omitempty"`
	TokenEndpoint    string   `json:"tokenEndpoint,omitempty"`
	UserInfoEndpoint string   `json:"userInfoEndpoint,omitempty"`
	JWKSEndpoint     string   `json:"jwksEndpoint,omitempty"`
	SupportedScopes  []string `json:"supportedScopes,omitempty"`
	Error            string   `json:"error,omitempty"`
	LatencyMs        int64    `json:"latencyMs"`
}

// OIDCUser OIDC 认证成功后返回的用户信息
type OIDCUser struct {
	Subject     string   `json:"subject"`
	Account     string   `json:"account"`
	DisplayName string   `json:"displayName"`
	Email       string   `json:"email"`
	Phone       string   `json:"phone"`
	Groups      []string `json:"groups,omitempty"`
}

// NormalizeOIDCConfig 填充默认值
func NormalizeOIDCConfig(cfg OIDCConfig) OIDCConfig {
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{"openid", "profile", "email"}
	}
	if cfg.AttrMapping.Account == "" {
		cfg.AttrMapping.Account = "preferred_username"
	}
	if cfg.AttrMapping.DisplayName == "" {
		cfg.AttrMapping.DisplayName = "name"
	}
	if cfg.AttrMapping.Email == "" {
		cfg.AttrMapping.Email = "email"
	}
	if cfg.AttrMapping.Phone == "" {
		cfg.AttrMapping.Phone = "phone_number"
	}
	return cfg
}
