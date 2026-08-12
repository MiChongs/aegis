package config

import (
	"fmt"
	"strings"
	"time"
)

type SecurityConfig struct {
	MasterKey    string
	ChallengeTTL time.Duration
	Modules      SecurityModulesConfig
	TOTP         TOTPConfig
	RecoveryCode RecoveryCodeConfig
	Passkey      PasskeyConfig
}

type SecurityModulesConfig struct {
	TOTPEnabled          bool
	RecoveryCodesEnabled bool
	PasskeyEnabled       bool
}

type TOTPConfig struct {
	Enabled       bool
	Issuer        string
	EnrollmentTTL time.Duration
	Skew          uint
	Digits        int
}

type RecoveryCodeConfig struct {
	Enabled bool
	Count   int
	Length  int
}

type PasskeyConfig struct {
	Enabled          bool
	RPDisplayName    string
	RPID             string
	RPOrigins        []string
	RPTopOrigins     []string
	ChallengeTTL     time.Duration
	UserVerification string
}

func NormalizeSecurityConfig(cfg SecurityConfig, appName, jwtSecret string) SecurityConfig {
	appName = strings.TrimSpace(appName)
	if appName == "" {
		appName = "AEGIS"
	}
	if strings.TrimSpace(cfg.MasterKey) == "" {
		cfg.MasterKey = jwtSecret
	}
	if cfg.ChallengeTTL <= 0 {
		cfg.ChallengeTTL = 10 * time.Minute
	}
	if !cfg.Modules.TOTPEnabled && !cfg.Modules.RecoveryCodesEnabled && !cfg.Modules.PasskeyEnabled {
		cfg.Modules.TOTPEnabled = true
		cfg.Modules.RecoveryCodesEnabled = true
		cfg.Modules.PasskeyEnabled = true
	}
	if !cfg.Modules.TOTPEnabled {
		cfg.TOTP.Enabled = false
	} else if !cfg.TOTP.Enabled {
		cfg.TOTP.Enabled = true
	}
	if strings.TrimSpace(cfg.TOTP.Issuer) == "" {
		cfg.TOTP.Issuer = strings.ToUpper(appName)
	}
	if cfg.TOTP.EnrollmentTTL <= 0 {
		cfg.TOTP.EnrollmentTTL = cfg.ChallengeTTL
	}
	if cfg.TOTP.Skew == 0 {
		cfg.TOTP.Skew = 1
	}
	if cfg.TOTP.Digits == 0 {
		cfg.TOTP.Digits = 6
	}
	if !cfg.Modules.RecoveryCodesEnabled {
		cfg.RecoveryCode.Enabled = false
	} else if !cfg.RecoveryCode.Enabled {
		cfg.RecoveryCode.Enabled = true
	}
	if cfg.RecoveryCode.Count <= 0 {
		cfg.RecoveryCode.Count = 10
	}
	if cfg.RecoveryCode.Length <= 0 {
		cfg.RecoveryCode.Length = 12
	}
	if !cfg.Modules.PasskeyEnabled {
		cfg.Passkey.Enabled = false
	} else if !cfg.Passkey.Enabled {
		cfg.Passkey.Enabled = true
	}
	if strings.TrimSpace(cfg.Passkey.RPDisplayName) == "" {
		cfg.Passkey.RPDisplayName = strings.ToUpper(appName)
	}
	// RP ID 留空是**合法且推荐**的取值：留空表示「跟随访问域名」，
	// 由 service.webAuthnForRequest 按本次请求的来源解析。
	//
	// 这里以前会兜底成 "localhost"，而缺省允许来源里同时写着 http://127.0.0.1:3000 ——
	// 从 127.0.0.1 打开控制台时，浏览器会以「RP ID 不是当前域名的可注册后缀」
	// 直接拒绝，缺省配置自己和自己打架。上线换域名后是同一个症状。
	cfg.Passkey.RPID = strings.ToLower(strings.TrimSpace(cfg.Passkey.RPID))
	cfg.Passkey.RPID = strings.TrimSuffix(cfg.Passkey.RPID, ".")
	cfg.Passkey.RPOrigins = compactSecurityStrings(cfg.Passkey.RPOrigins)
	if len(cfg.Passkey.RPOrigins) == 0 {
		cfg.Passkey.RPOrigins = []string{"http://localhost:3000", "http://127.0.0.1:3000"}
	}
	cfg.Passkey.RPTopOrigins = compactSecurityStrings(cfg.Passkey.RPTopOrigins)
	if cfg.Passkey.ChallengeTTL <= 0 {
		cfg.Passkey.ChallengeTTL = cfg.ChallengeTTL
	}
	if strings.TrimSpace(cfg.Passkey.UserVerification) == "" {
		cfg.Passkey.UserVerification = "preferred"
	}
	return cfg
}

func applySecurityDefaults(cfg *Config) {
	cfg.Security = NormalizeSecurityConfig(cfg.Security, cfg.AppName, cfg.JWT.Secret)
}

func (cfg SecurityConfig) WebAuthnOriginSummary() string {
	if len(cfg.Passkey.RPOrigins) == 0 {
		return ""
	}
	return fmt.Sprintf("%s (%d)", cfg.Passkey.RPOrigins[0], len(cfg.Passkey.RPOrigins))
}

func compactSecurityStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	items := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			items = append(items, value)
		}
	}
	return items
}
