package system

import (
	"encoding/json"
	"time"

	securitydomain "aegis/internal/domain/security"
)

type SettingRecord struct {
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	UpdatedBy *int64          `json:"updatedBy,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

type SettingsView struct {
	Firewall     FirewallSettingsView     `json:"firewall"`
	Security     SecuritySettingsView     `json:"security"`
	AdminCaptcha AdminCaptchaSettingsView `json:"adminCaptcha"`
	LDAP         LDAPSettingsView         `json:"ldap"`
	OIDC         OIDCSettingsView         `json:"oidc"`
	SAML         SAMLSettingsView         `json:"saml"`
	Branding     BrandingSettingsView     `json:"branding"`
	SelfService  SelfServiceSettingsView  `json:"selfService"`
}

// SelfServiceSettingsView 自助能力配置（平台级，对所有应用一视同仁）。
//
// 管着两件事：陌生人能不能自己注册成管理员，以及一个**零角色**的管理员
// 能不能自己拉起第一个应用。后者是整套 RBAC 唯一的自举出口 ——
// 关掉它，自助注册出来的账号就只能干等超管授权。
//
// 三个字段刻意分开：只关注册（对内部部署）与只关建应用（开放注册但人工审批）
// 是两种真实存在的运营取向，合成一个开关会逼人二选一。
type SelfServiceSettingsView struct {
	// AllowRegistration 是否开放 POST /api/admin/auth/register。
	// 关闭后该路由返回明确的 403 而不是 404 —— 404 会让接入方以为地址写错了。
	AllowRegistration bool `json:"allowRegistration"`
	// AllowAppCreation 未被授予 app:write 的管理员能否自助创建应用。
	AllowAppCreation bool `json:"allowAppCreation"`
	// MaxAppsPerAdmin 每人自助创建的应用数上限，0 = 不限。
	// 只统计 apps.created_by 命中的行：被超管授权去管理的既有应用不占配额。
	MaxAppsPerAdmin int `json:"maxAppsPerAdmin"`
	// CreatorRoleKey 创建者自动获得的应用级角色，必须是 scope=app 的角色。
	CreatorRoleKey string `json:"creatorRoleKey"`
}

// SelfServiceSettingsPatch 自助能力配置补丁，nil 表示不修改。
type SelfServiceSettingsPatch struct {
	AllowRegistration *bool   `json:"allowRegistration,omitempty"`
	AllowAppCreation  *bool   `json:"allowAppCreation,omitempty"`
	MaxAppsPerAdmin   *int    `json:"maxAppsPerAdmin,omitempty"`
	CreatorRoleKey    *string `json:"creatorRoleKey,omitempty"`
}

type FirewallSettingsView struct {
	Enabled           bool       `json:"enabled"`
	GlobalRate        string     `json:"globalRate"`
	AuthRate          string     `json:"authRate"`
	AdminRate         string     `json:"adminRate"`
	CorazaEnabled     bool       `json:"corazaEnabled"`
	CorazaParanoia    int        `json:"corazaParanoia"`
	RequestBodyLimit  int        `json:"requestBodyLimit"`
	RequestBodyMemory int        `json:"requestBodyMemory"`
	AllowedCIDRs      []string   `json:"allowedCIDRs"`
	BlockedCIDRs      []string   `json:"blockedCIDRs"`
	BlockedUserAgents []string   `json:"blockedUserAgents"`
	BlockedPathPrefix []string   `json:"blockedPathPrefix"`
	MaxPathLength     int        `json:"maxPathLength"`
	MaxQueryLength    int        `json:"maxQueryLength"`
	// IP 封禁响应模式（数据库驱动，平台级全局）
	DefaultBanMode    string     `json:"defaultBanMode"`
	TarpitDelayMs     int        `json:"tarpitDelayMs"`
	Source            string     `json:"source"`
	ReloadVersion     uint64     `json:"reloadVersion"`
	ReloadedAt        time.Time  `json:"reloadedAt"`
	UpdatedBy         *int64     `json:"updatedBy,omitempty"`
	UpdatedAt         *time.Time `json:"updatedAt,omitempty"`
}

type SecuritySettingsView struct {
	MasterKeyConfigured bool                             `json:"masterKeyConfigured"`
	ChallengeTTLSeconds int64                            `json:"challengeTTLSeconds"`
	Modules             SecurityModuleSettingsView       `json:"modules"`
	TOTP                SecurityTOTPSettingsView         `json:"totp"`
	RecoveryCodes       SecurityRecoveryCodeSettingsView `json:"recoveryCodes"`
	Passkey             SecurityPasskeySettingsView      `json:"passkey"`
	RuntimeModules      []securitydomain.ModuleStatus    `json:"runtimeModules,omitempty"`
	Source              string                           `json:"source"`
	ReloadVersion       uint64                           `json:"reloadVersion"`
	ReloadedAt          time.Time                        `json:"reloadedAt"`
	UpdatedBy           *int64                           `json:"updatedBy,omitempty"`
	UpdatedAt           *time.Time                       `json:"updatedAt,omitempty"`
}

type SecurityModuleSettingsView struct {
	TOTPEnabled          bool `json:"totpEnabled"`
	RecoveryCodesEnabled bool `json:"recoveryCodesEnabled"`
	PasskeyEnabled       bool `json:"passkeyEnabled"`
}

type SecurityTOTPSettingsView struct {
	Enabled              bool   `json:"enabled"`
	Issuer               string `json:"issuer"`
	EnrollmentTTLSeconds int64  `json:"enrollmentTTLSeconds"`
	Skew                 uint   `json:"skew"`
	Digits               int    `json:"digits"`
}

type SecurityRecoveryCodeSettingsView struct {
	Enabled bool `json:"enabled"`
	Count   int  `json:"count"`
	Length  int  `json:"length"`
}

type SecurityPasskeySettingsView struct {
	Enabled             bool     `json:"enabled"`
	RPDisplayName       string   `json:"rpDisplayName"`
	RPID                string   `json:"rpId"`
	RPOrigins           []string `json:"rpOrigins"`
	RPTopOrigins        []string `json:"rpTopOrigins"`
	ChallengeTTLSeconds int64    `json:"challengeTTLSeconds"`
	UserVerification    string   `json:"userVerification"`
}

// AdminCaptchaSettingsView 全局管理员验证码配置（独立于应用级验证码）
type AdminCaptchaSettingsView struct {
	Enabled            bool   `json:"enabled"`            // 总开关
	Type               string `json:"type"`               // image / math / digit / dynamic / audio / chiral
	RequireForLogin    bool   `json:"requireForLogin"`    // 登录需验证码
	RequireForRegister bool   `json:"requireForRegister"` // 注册需验证码
	AudioLang          string `json:"audioLang"`          // 音频语言：zh / en
}

type AdminCaptchaSettingsPatch struct {
	Enabled            *bool   `json:"enabled,omitempty"`
	Type               *string `json:"type,omitempty"`
	RequireForLogin    *bool   `json:"requireForLogin,omitempty"`
	RequireForRegister *bool   `json:"requireForRegister,omitempty"`
	AudioLang          *string `json:"audioLang,omitempty"`
}

type SettingsUpdate struct {
	Firewall     FirewallSettingsPatch     `json:"firewall"`
	Security     SecuritySettingsPatch     `json:"security"`
	AdminCaptcha AdminCaptchaSettingsPatch `json:"adminCaptcha"`
	LDAP         LDAPSettingsPatch         `json:"ldap"`
	OIDC         OIDCSettingsPatch         `json:"oidc"`
	SAML         SAMLSettingsPatch         `json:"saml"`
	Branding     BrandingSettingsPatch     `json:"branding"`
	SelfService  SelfServiceSettingsPatch  `json:"selfService"`
}

type FirewallSettingsPatch struct {
	Enabled           *bool     `json:"enabled,omitempty"`
	GlobalRate        *string   `json:"globalRate,omitempty"`
	AuthRate          *string   `json:"authRate,omitempty"`
	AdminRate         *string   `json:"adminRate,omitempty"`
	CorazaEnabled     *bool     `json:"corazaEnabled,omitempty"`
	CorazaParanoia    *int      `json:"corazaParanoia,omitempty"`
	RequestBodyLimit  *int      `json:"requestBodyLimit,omitempty"`
	RequestBodyMemory *int      `json:"requestBodyMemory,omitempty"`
	AllowedCIDRs      *[]string `json:"allowedCIDRs,omitempty"`
	BlockedCIDRs      *[]string `json:"blockedCIDRs,omitempty"`
	BlockedUserAgents *[]string `json:"blockedUserAgents,omitempty"`
	BlockedPathPrefix *[]string `json:"blockedPathPrefix,omitempty"`
	MaxPathLength     *int      `json:"maxPathLength,omitempty"`
	MaxQueryLength    *int      `json:"maxQueryLength,omitempty"`
	DefaultBanMode    *string   `json:"defaultBanMode,omitempty"`
	TarpitDelayMs     *int      `json:"tarpitDelayMs,omitempty"`
}

type SecuritySettingsPatch struct {
	ChallengeTTLSeconds *int64                            `json:"challengeTTLSeconds,omitempty"`
	Modules             SecurityModuleSettingsPatch       `json:"modules"`
	TOTP                SecurityTOTPSettingsPatch         `json:"totp"`
	RecoveryCodes       SecurityRecoveryCodeSettingsPatch `json:"recoveryCodes"`
	Passkey             SecurityPasskeySettingsPatch      `json:"passkey"`
}

type SecurityModuleSettingsPatch struct {
	TOTPEnabled          *bool `json:"totpEnabled,omitempty"`
	RecoveryCodesEnabled *bool `json:"recoveryCodesEnabled,omitempty"`
	PasskeyEnabled       *bool `json:"passkeyEnabled,omitempty"`
}

type SecurityTOTPSettingsPatch struct {
	Enabled              *bool   `json:"enabled,omitempty"`
	Issuer               *string `json:"issuer,omitempty"`
	EnrollmentTTLSeconds *int64  `json:"enrollmentTTLSeconds,omitempty"`
	Skew                 *uint   `json:"skew,omitempty"`
	Digits               *int    `json:"digits,omitempty"`
}

type SecurityRecoveryCodeSettingsPatch struct {
	Enabled *bool `json:"enabled,omitempty"`
	Count   *int  `json:"count,omitempty"`
	Length  *int  `json:"length,omitempty"`
}

type SecurityPasskeySettingsPatch struct {
	Enabled             *bool     `json:"enabled,omitempty"`
	RPDisplayName       *string   `json:"rpDisplayName,omitempty"`
	RPID                *string   `json:"rpId,omitempty"`
	RPOrigins           *[]string `json:"rpOrigins,omitempty"`
	RPTopOrigins        *[]string `json:"rpTopOrigins,omitempty"`
	ChallengeTTLSeconds *int64    `json:"challengeTTLSeconds,omitempty"`
	UserVerification    *string   `json:"userVerification,omitempty"`
}
