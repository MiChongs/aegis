package system

import "time"

// LDAPConfig LDAP 认证配置（持久化到 platform_settings 表）
type LDAPConfig struct {
	Enabled        bool   `json:"enabled"`
	Server         string `json:"server"`
	Port           int    `json:"port"`
	UseTLS         bool   `json:"useTLS"`
	UseStartTLS    bool   `json:"useStartTLS"`
	SkipTLSVerify  bool   `json:"skipTLSVerify"`
	BindDN         string `json:"bindDN"`
	BindPassword   string `json:"bindPassword"` // AES-GCM 加密存储
	BaseDN         string `json:"baseDN"`
	UserFilter     string `json:"userFilter"`
	UserAttribute  string `json:"userAttribute"`
	GroupBaseDN    string `json:"groupBaseDN,omitempty"`
	GroupFilter    string `json:"groupFilter,omitempty"`
	GroupAttribute string `json:"groupAttribute,omitempty"`
	AdminGroupDN   string `json:"adminGroupDN,omitempty"`
	AttrMapping    LDAPAttributeMapping `json:"attrMapping"`
	ConnectionTimeoutSeconds int  `json:"connectionTimeoutSeconds"`
	SearchTimeoutSeconds     int  `json:"searchTimeoutSeconds"`
	FallbackToLocal          bool `json:"fallbackToLocal"`
}

// LDAPAttributeMapping LDAP 属性到管理员字段的映射
type LDAPAttributeMapping struct {
	Account     string `json:"account"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
	Phone       string `json:"phone"`
}

// LDAPSettingsView 返回给前端的视图（脱敏 BindPassword）
type LDAPSettingsView struct {
	Enabled                  bool                 `json:"enabled"`
	Server                   string               `json:"server"`
	Port                     int                  `json:"port"`
	UseTLS                   bool                 `json:"useTLS"`
	UseStartTLS              bool                 `json:"useStartTLS"`
	SkipTLSVerify            bool                 `json:"skipTLSVerify"`
	BindDN                   string               `json:"bindDN"`
	HasBindPassword          bool                 `json:"hasBindPassword"`
	BaseDN                   string               `json:"baseDN"`
	UserFilter               string               `json:"userFilter"`
	UserAttribute            string               `json:"userAttribute"`
	GroupBaseDN              string               `json:"groupBaseDN,omitempty"`
	GroupFilter              string               `json:"groupFilter,omitempty"`
	GroupAttribute           string               `json:"groupAttribute,omitempty"`
	AdminGroupDN             string               `json:"adminGroupDN,omitempty"`
	AttrMapping              LDAPAttributeMapping `json:"attrMapping"`
	ConnectionTimeoutSeconds int                  `json:"connectionTimeoutSeconds"`
	SearchTimeoutSeconds     int                  `json:"searchTimeoutSeconds"`
	FallbackToLocal          bool                 `json:"fallbackToLocal"`
	Source                   string               `json:"source"`
	UpdatedBy                *int64               `json:"updatedBy,omitempty"`
	UpdatedAt                *time.Time           `json:"updatedAt,omitempty"`
}

// LDAPSettingsPatch 前端更新请求
type LDAPSettingsPatch struct {
	Enabled                  *bool                       `json:"enabled,omitempty"`
	Server                   *string                     `json:"server,omitempty"`
	Port                     *int                        `json:"port,omitempty"`
	UseTLS                   *bool                       `json:"useTLS,omitempty"`
	UseStartTLS              *bool                       `json:"useStartTLS,omitempty"`
	SkipTLSVerify            *bool                       `json:"skipTLSVerify,omitempty"`
	BindDN                   *string                     `json:"bindDN,omitempty"`
	BindPassword             *string                     `json:"bindPassword,omitempty"`
	BaseDN                   *string                     `json:"baseDN,omitempty"`
	UserFilter               *string                     `json:"userFilter,omitempty"`
	UserAttribute            *string                     `json:"userAttribute,omitempty"`
	GroupBaseDN              *string                     `json:"groupBaseDN,omitempty"`
	GroupFilter              *string                     `json:"groupFilter,omitempty"`
	GroupAttribute           *string                     `json:"groupAttribute,omitempty"`
	AdminGroupDN             *string                     `json:"adminGroupDN,omitempty"`
	AttrMapping              *LDAPAttributeMappingPatch  `json:"attrMapping,omitempty"`
	ConnectionTimeoutSeconds *int                        `json:"connectionTimeoutSeconds,omitempty"`
	SearchTimeoutSeconds     *int                        `json:"searchTimeoutSeconds,omitempty"`
	FallbackToLocal          *bool                       `json:"fallbackToLocal,omitempty"`
}

// LDAPAttributeMappingPatch 属性映射更新
type LDAPAttributeMappingPatch struct {
	Account     *string `json:"account,omitempty"`
	DisplayName *string `json:"displayName,omitempty"`
	Email       *string `json:"email,omitempty"`
	Phone       *string `json:"phone,omitempty"`
}

// LDAPTestRequest 连接测试请求
type LDAPTestRequest struct {
	Server                   string `json:"server"`
	Port                     int    `json:"port"`
	UseTLS                   bool   `json:"useTLS"`
	UseStartTLS              bool   `json:"useStartTLS"`
	SkipTLSVerify            bool   `json:"skipTLSVerify"`
	BindDN                   string `json:"bindDN"`
	BindPassword             string `json:"bindPassword"`
	BaseDN                   string `json:"baseDN"`
	UserFilter               string `json:"userFilter"`
	TestAccount              string `json:"testAccount"`
	ConnectionTimeoutSeconds int    `json:"connectionTimeoutSeconds"`
}

// LDAPTestResult 连接测试结果
type LDAPTestResult struct {
	Connected   bool   `json:"connected"`
	BindSuccess bool   `json:"bindSuccess"`
	SearchOK    bool   `json:"searchOK"`
	UserFound   bool   `json:"userFound,omitempty"`
	UserDN      string `json:"userDN,omitempty"`
	Error       string `json:"error,omitempty"`
	LatencyMs   int64  `json:"latencyMs"`
}

// LDAPUser LDAP 认证成功后返回的用户信息
type LDAPUser struct {
	DN          string   `json:"dn"`
	Account     string   `json:"account"`
	DisplayName string   `json:"displayName"`
	Email       string   `json:"email"`
	Phone       string   `json:"phone"`
	Groups      []string `json:"groups,omitempty"`
}

// NormalizeLDAPConfig 填充默认值
func NormalizeLDAPConfig(cfg LDAPConfig) LDAPConfig {
	if cfg.Port == 0 {
		if cfg.UseTLS {
			cfg.Port = 636
		} else {
			cfg.Port = 389
		}
	}
	if cfg.UserFilter == "" {
		cfg.UserFilter = "(sAMAccountName=%s)"
	}
	if cfg.UserAttribute == "" {
		cfg.UserAttribute = "sAMAccountName"
	}
	if cfg.AttrMapping.Account == "" {
		cfg.AttrMapping.Account = "sAMAccountName"
	}
	if cfg.AttrMapping.DisplayName == "" {
		cfg.AttrMapping.DisplayName = "displayName"
	}
	if cfg.AttrMapping.Email == "" {
		cfg.AttrMapping.Email = "mail"
	}
	if cfg.AttrMapping.Phone == "" {
		cfg.AttrMapping.Phone = "telephoneNumber"
	}
	if cfg.ConnectionTimeoutSeconds <= 0 {
		cfg.ConnectionTimeoutSeconds = 10
	}
	if cfg.SearchTimeoutSeconds <= 0 {
		cfg.SearchTimeoutSeconds = 15
	}
	return cfg
}
