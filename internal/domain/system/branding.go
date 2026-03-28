package system

import "time"

// BrandingConfig 平台品牌配置
type BrandingConfig struct {
	PlatformName     string `json:"platformName"`
	ConsoleName      string `json:"consoleName"`
	LogoURL          string `json:"logoURL"`
	LogoDarkURL      string `json:"logoDarkURL"`
	FaviconURL       string `json:"faviconURL"`
	PrimaryColor     string `json:"primaryColor"`
	PrimaryColorDark string `json:"primaryColorDark"`
	AccentColor      string `json:"accentColor"`
	LoginBgURL       string `json:"loginBgURL"`
	LoginBgColor     string `json:"loginBgColor"`
	FooterText       string `json:"footerText"`
	CustomCSS        string `json:"customCSS"`
}

// BrandingSettingsView 返回给前端的品牌视图
type BrandingSettingsView struct {
	BrandingConfig
	Source    string     `json:"source"`
	UpdatedBy *int64    `json:"updatedBy,omitempty"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
}

// BrandingSettingsPatch 品牌更新请求
type BrandingSettingsPatch struct {
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

// NormalizeBrandingConfig 填充默认值
func NormalizeBrandingConfig(cfg BrandingConfig) BrandingConfig {
	if cfg.PlatformName == "" {
		cfg.PlatformName = "Aegis"
	}
	if cfg.ConsoleName == "" {
		cfg.ConsoleName = "控制台"
	}
	return cfg
}
