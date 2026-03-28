package httptransport

type AdminSystemSettingsUpdateRequest struct {
	Firewall     AdminFirewallSettingsUpdateRequest     `json:"firewall"`
	Security     AdminSecuritySettingsUpdateRequest     `json:"security"`
	AdminCaptcha AdminCaptchaSettingsUpdateRequest      `json:"adminCaptcha"`
	LDAP         AdminLDAPSettingsUpdateRequest         `json:"ldap"`
	OIDC         AdminOIDCSettingsUpdateRequest         `json:"oidc"`
	Branding     AdminBrandingSettingsUpdateRequest     `json:"branding"`
}

type AdminBrandingSettingsUpdateRequest struct {
	PlatformName     *string `json:"platformName,omitempty"`
	ConsoleName      *string `json:"consoleName,omitempty"`
	LogoURL          *string `json:"logoURL,omitempty"`
	LogoDarkURL      *string `json:"logoDarkURL,omitempty"`
	FaviconURL       *string `json:"faviconURL,omitempty"`
	PrimaryColor     *string `json:"primaryColor,omitempty"`
	PrimaryColorDark *string `json:"primaryColorDark,omitempty"`
	AccentColor      *string `json:"accentColor,omitempty"`
	LoginBgURL       *string `json:"loginBgURL,omitempty"`
	LoginBgColor     *string `json:"loginBgColor,omitempty"`
	FooterText       *string `json:"footerText,omitempty"`
	CustomCSS        *string `json:"customCSS,omitempty"`
}

type AdminLDAPSettingsUpdateRequest struct {
	Enabled                  *bool                              `json:"enabled,omitempty"`
	Server                   *string                            `json:"server,omitempty"`
	Port                     *int                               `json:"port,omitempty"`
	UseTLS                   *bool                              `json:"useTLS,omitempty"`
	UseStartTLS              *bool                              `json:"useStartTLS,omitempty"`
	SkipTLSVerify            *bool                              `json:"skipTLSVerify,omitempty"`
	BindDN                   *string                            `json:"bindDN,omitempty"`
	BindPassword             *string                            `json:"bindPassword,omitempty"`
	BaseDN                   *string                            `json:"baseDN,omitempty"`
	UserFilter               *string                            `json:"userFilter,omitempty"`
	UserAttribute            *string                            `json:"userAttribute,omitempty"`
	GroupBaseDN              *string                            `json:"groupBaseDN,omitempty"`
	GroupFilter              *string                            `json:"groupFilter,omitempty"`
	GroupAttribute           *string                            `json:"groupAttribute,omitempty"`
	AdminGroupDN             *string                            `json:"adminGroupDN,omitempty"`
	AttrMapping              *AdminLDAPAttrMappingUpdateRequest `json:"attrMapping,omitempty"`
	ConnectionTimeoutSeconds *int                               `json:"connectionTimeoutSeconds,omitempty"`
	SearchTimeoutSeconds     *int                               `json:"searchTimeoutSeconds,omitempty"`
	FallbackToLocal          *bool                              `json:"fallbackToLocal,omitempty"`
}

type AdminLDAPAttrMappingUpdateRequest struct {
	Account     *string `json:"account,omitempty"`
	DisplayName *string `json:"displayName,omitempty"`
	Email       *string `json:"email,omitempty"`
	Phone       *string `json:"phone,omitempty"`
}

type AdminLDAPTestRequest struct {
	Server                   string `json:"server" binding:"required"`
	Port                     int    `json:"port"`
	UseTLS                   bool   `json:"useTLS"`
	UseStartTLS              bool   `json:"useStartTLS"`
	SkipTLSVerify            bool   `json:"skipTLSVerify"`
	BindDN                   string `json:"bindDN" binding:"required"`
	BindPassword             string `json:"bindPassword" binding:"required"`
	BaseDN                   string `json:"baseDN" binding:"required"`
	UserFilter               string `json:"userFilter"`
	TestAccount              string `json:"testAccount"`
	ConnectionTimeoutSeconds int    `json:"connectionTimeoutSeconds"`
}

type AdminCaptchaSettingsUpdateRequest struct {
	Enabled            *bool   `json:"enabled,omitempty"`
	Type               *string `json:"type,omitempty"`
	RequireForLogin    *bool   `json:"requireForLogin,omitempty"`
	RequireForRegister *bool   `json:"requireForRegister,omitempty"`
	AudioLang          *string `json:"audioLang,omitempty"`
}

type AdminFirewallSettingsUpdateRequest struct {
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

type AdminSecuritySettingsUpdateRequest struct {
	ChallengeTTLSeconds *int64                                    `json:"challengeTTLSeconds,omitempty"`
	Modules             AdminSecurityModuleSettingsUpdateRequest  `json:"modules"`
	TOTP                AdminSecurityTOTPSettingsUpdateRequest    `json:"totp"`
	RecoveryCodes       AdminSecurityRecoveryCodesUpdateRequest   `json:"recoveryCodes"`
	Passkey             AdminSecurityPasskeySettingsUpdateRequest `json:"passkey"`
}

type AdminSecurityModuleSettingsUpdateRequest struct {
	TOTPEnabled          *bool `json:"totpEnabled,omitempty"`
	RecoveryCodesEnabled *bool `json:"recoveryCodesEnabled,omitempty"`
	PasskeyEnabled       *bool `json:"passkeyEnabled,omitempty"`
}

type AdminSecurityTOTPSettingsUpdateRequest struct {
	Enabled              *bool   `json:"enabled,omitempty"`
	Issuer               *string `json:"issuer,omitempty"`
	EnrollmentTTLSeconds *int64  `json:"enrollmentTTLSeconds,omitempty"`
	Skew                 *uint   `json:"skew,omitempty"`
	Digits               *int    `json:"digits,omitempty"`
}

type AdminSecurityRecoveryCodesUpdateRequest struct {
	Enabled *bool `json:"enabled,omitempty"`
	Count   *int  `json:"count,omitempty"`
	Length  *int  `json:"length,omitempty"`
}

type AdminSecurityPasskeySettingsUpdateRequest struct {
	Enabled             *bool     `json:"enabled,omitempty"`
	RPDisplayName       *string   `json:"rpDisplayName,omitempty"`
	RPID                *string   `json:"rpId,omitempty"`
	RPOrigins           *[]string `json:"rpOrigins,omitempty"`
	RPTopOrigins        *[]string `json:"rpTopOrigins,omitempty"`
	ChallengeTTLSeconds *int64    `json:"challengeTTLSeconds,omitempty"`
	UserVerification    *string   `json:"userVerification,omitempty"`
}
