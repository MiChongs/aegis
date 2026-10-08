package config

import (
	"strings"
	"time"

	"github.com/spf13/viper"
)

// OAuth2ServerConfig 对外的 OAuth2 / OIDC 授权服务器（由 Ory Hydra 承载）。
//
// Hydra 负责协议，Aegis 是它的 login / consent / logout provider，详见 docs/oauth2-provider.md。
// 未启用时 /oauth2/* 页面回 503，控制台的客户端管理接口回 503，其余功能不受影响。
//
// 相关环境变量：
//
//	OAUTH2_SERVER_ENABLED          bool，默认 false
//	HYDRA_ADMIN_URL                Hydra 管理端，如 http://hydra:4445（只在内网可达）
//	HYDRA_PUBLIC_URL               Hydra 公开端（即签发方 issuer），如 https://auth.example.com
//	HYDRA_TOKEN_HOOK_SECRET        Hydra 调用令牌钩子时携带的 Bearer 密钥（与 hydra.yml 一致）
//	OAUTH2_LOGIN_REMEMBER_FOR      勾选「保持登录」后记住多久，默认 720h
//	OAUTH2_CONSENT_REMEMBER_FOR    记住授权同意多久，默认 4320h（180 天）；0 = 永久
//	OAUTH2_COOKIE_SECURE           页面 Cookie 是否加 Secure；留空时按 API_BASE_URL 是否 https 推断
type OAuth2ServerConfig struct {
	Enabled            bool
	HydraAdminURL      string
	HydraPublicURL     string
	TokenHookSecret    string
	LoginRememberFor   time.Duration
	ConsentRememberFor time.Duration
	CookieSecure       bool
	// CSRFKey 页面表单 CSRF 令牌的 HMAC 密钥，取自 SECURITY_MASTER_KEY / JWT_SECRET，不单独配置
	CSRFKey string
}

func loadOAuth2ServerConfig(v *viper.Viper, apiBaseURL, masterKey, jwtSecret string) OAuth2ServerConfig {
	cfg := OAuth2ServerConfig{
		Enabled:            getBool(v, "OAUTH2_SERVER_ENABLED", false),
		HydraAdminURL:      strings.TrimRight(strings.TrimSpace(v.GetString("HYDRA_ADMIN_URL")), "/"),
		HydraPublicURL:     strings.TrimRight(strings.TrimSpace(v.GetString("HYDRA_PUBLIC_URL")), "/"),
		TokenHookSecret:    strings.TrimSpace(v.GetString("HYDRA_TOKEN_HOOK_SECRET")),
		LoginRememberFor:   getDuration(v, "OAUTH2_LOGIN_REMEMBER_FOR", 30*24*time.Hour),
		ConsentRememberFor: getDuration(v, "OAUTH2_CONSENT_REMEMBER_FOR", 180*24*time.Hour),
		CookieSecure:       getBool(v, "OAUTH2_COOKIE_SECURE", strings.HasPrefix(strings.ToLower(strings.TrimSpace(apiBaseURL)), "https://")),
	}
	if cfg.HydraAdminURL == "" {
		cfg.HydraAdminURL = "http://127.0.0.1:4445"
	}
	cfg.CSRFKey = firstNonEmptyStr(masterKey, jwtSecret)
	return cfg
}
