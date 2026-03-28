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
	Branding     BrandingSettingsView     `json:"branding"`
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
	Branding     BrandingSettingsPatch     `json:"branding"`
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
