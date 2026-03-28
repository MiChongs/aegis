package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"aegis/internal/config"
	plugindomain "aegis/internal/domain/plugin"
	securitydomain "aegis/internal/domain/security"
	systemdomain "aegis/internal/domain/system"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"go.uber.org/zap"
)

const firewallSettingKey = "security.firewall"
const securitySettingKey = "security.authentication"
const adminCaptchaSettingKey = "admin.captcha"
const ldapSettingKey = "admin.ldap"
const oidcSettingKey = "admin.oidc"
const brandingSettingKey = "platform.branding"

type PlatformSettingsService struct {
	cfg      config.Config
	log      *zap.Logger
	pg       *pgrepo.Repository
	firewall firewallRuntime
	security securityRuntime
	ldap     *LDAPService
	oidc     *OIDCService
	plugin   *PluginService
}

// SetPluginService 注入插件服务
func (s *PlatformSettingsService) SetPluginService(p *PluginService) {
	s.plugin = p
}

type firewallRuntime interface {
	CurrentConfig() config.FirewallConfig
	ReloadMeta() (uint64, time.Time)
	ValidateConfig(config.FirewallConfig) error
	Reload(config.FirewallConfig) error
}

type securityRuntime interface {
	CurrentConfig() config.SecurityConfig
	ReloadMeta() (uint64, time.Time)
	ValidateConfig(config.SecurityConfig) error
	Reload(config.SecurityConfig) error
	Modules() []securitydomain.ModuleStatus
}

func NewPlatformSettingsService(cfg config.Config, log *zap.Logger, pg *pgrepo.Repository, firewall firewallRuntime, security securityRuntime, ldap *LDAPService, oidc *OIDCService) *PlatformSettingsService {
	if log == nil {
		log = zap.NewNop()
	}
	return &PlatformSettingsService{
		cfg:      cfg,
		log:      log,
		pg:       pg,
		firewall: firewall,
		security: security,
		ldap:     ldap,
		oidc:     oidc,
	}
}

func (s *PlatformSettingsService) Initialize(ctx context.Context) error {
	if s.firewall != nil {
		record, err := s.pg.GetPlatformSetting(ctx, firewallSettingKey)
		if err != nil {
			return err
		}
		if record != nil && len(record.Value) > 0 {
			cfg, err := s.decodeFirewallConfig(record.Value)
			if err != nil {
				s.log.Error("decode persisted firewall settings failed", zap.Error(err))
			} else if err := s.firewall.Reload(cfg); err != nil {
				s.log.Error("reload persisted firewall settings failed", zap.Error(err))
			}
		}
	}

	if s.security != nil {
		record, err := s.pg.GetPlatformSetting(ctx, securitySettingKey)
		if err != nil {
			return err
		}
		if record != nil && len(record.Value) > 0 {
			cfg, err := s.decodeSecurityConfig(record.Value)
			if err != nil {
				s.log.Error("decode persisted security settings failed", zap.Error(err))
			} else if err := s.security.Reload(cfg); err != nil {
				s.log.Error("reload persisted security settings failed", zap.Error(err))
			}
		}
	}

	if s.ldap != nil {
		record, err := s.pg.GetPlatformSetting(ctx, ldapSettingKey)
		if err != nil {
			return err
		}
		if record != nil && len(record.Value) > 0 {
			var cfg systemdomain.LDAPConfig
			if err := json.Unmarshal(record.Value, &cfg); err != nil {
				s.log.Error("decode persisted LDAP settings failed", zap.Error(err))
			} else {
				cfg = systemdomain.NormalizeLDAPConfig(cfg)
				if err := s.ldap.Reload(cfg); err != nil {
					s.log.Error("reload persisted LDAP settings failed", zap.Error(err))
				}
			}
		}
	}

	if s.oidc != nil {
		record, err := s.pg.GetPlatformSetting(ctx, oidcSettingKey)
		if err != nil {
			return err
		}
		if record != nil && len(record.Value) > 0 {
			var cfg systemdomain.OIDCConfig
			if err := json.Unmarshal(record.Value, &cfg); err != nil {
				s.log.Error("decode persisted OIDC settings failed", zap.Error(err))
			} else {
				cfg = systemdomain.NormalizeOIDCConfig(cfg)
				if err := s.oidc.Reload(cfg); err != nil {
					s.log.Error("reload persisted OIDC settings failed", zap.Error(err))
				}
			}
		}
	}
	return nil
}

func (s *PlatformSettingsService) GetSettings(ctx context.Context) (*systemdomain.SettingsView, error) {
	firewallRecord, err := s.pg.GetPlatformSetting(ctx, firewallSettingKey)
	if err != nil {
		return nil, err
	}
	securityRecord, err := s.pg.GetPlatformSetting(ctx, securitySettingKey)
	if err != nil {
		return nil, err
	}
	captchaRecord, err := s.pg.GetPlatformSetting(ctx, adminCaptchaSettingKey)
	if err != nil {
		return nil, err
	}
	ldapRecord, _ := s.pg.GetPlatformSetting(ctx, ldapSettingKey)
	oidcRecord, _ := s.pg.GetPlatformSetting(ctx, oidcSettingKey)
	brandingRecord, _ := s.pg.GetPlatformSetting(ctx, brandingSettingKey)
	return &systemdomain.SettingsView{
		Firewall:     s.buildFirewallView(firewallRecord),
		Security:     s.buildSecurityView(securityRecord),
		AdminCaptcha: s.buildAdminCaptchaView(captchaRecord),
		LDAP:         s.buildLDAPView(ldapRecord),
		OIDC:         s.buildOIDCView(oidcRecord),
		Branding:     buildBrandingView(brandingRecord),
	}, nil
}

func (s *PlatformSettingsService) UpdateSettings(ctx context.Context, adminID *int64, patch systemdomain.SettingsUpdate) (*systemdomain.SettingsView, error) {
	var firewallRecord *systemdomain.SettingRecord
	if s.firewall != nil {
		current := s.firewall.CurrentConfig()
		next := applyFirewallPatch(current, patch.Firewall)
		next = config.NormalizeFirewallConfig(next)
		if err := s.firewall.ValidateConfig(next); err != nil {
			return nil, apperrors.New(40090, http.StatusBadRequest, "防火墙配置无效")
		}
		payload, err := json.Marshal(next)
		if err != nil {
			return nil, err
		}
		firewallRecord, err = s.pg.UpsertPlatformSetting(ctx, systemdomain.SettingRecord{Key: firewallSettingKey, Value: payload, UpdatedBy: adminID})
		if err != nil {
			return nil, err
		}
		if err := s.firewall.Reload(next); err != nil {
			return nil, apperrors.New(50090, http.StatusInternalServerError, "系统设置热重载失败")
		}
	}

	var securityRecord *systemdomain.SettingRecord
	if s.security != nil {
		current := s.security.CurrentConfig()
		next := applySecurityPatch(current, patch.Security)
		next = config.NormalizeSecurityConfig(next, s.cfg.AppName, s.cfg.JWT.Secret)
		if err := s.security.ValidateConfig(next); err != nil {
			return nil, apperrors.New(40091, http.StatusBadRequest, "认证安全配置无效")
		}
		payload, err := json.Marshal(next)
		if err != nil {
			return nil, err
		}
		securityRecord, err = s.pg.UpsertPlatformSetting(ctx, systemdomain.SettingRecord{Key: securitySettingKey, Value: payload, UpdatedBy: adminID})
		if err != nil {
			return nil, err
		}
		if err := s.security.Reload(next); err != nil {
			return nil, apperrors.New(50091, http.StatusInternalServerError, "认证安全模块热重载失败")
		}
	}

	// 管理员验证码配置（简单 JSON 持久化，无热重载）
	var captchaRecord *systemdomain.SettingRecord
	captchaRecord, _ = s.pg.GetPlatformSetting(ctx, adminCaptchaSettingKey)
	currentCaptcha := s.decodeAdminCaptchaConfig(captchaRecord)
	nextCaptcha := applyAdminCaptchaPatch(currentCaptcha, patch.AdminCaptcha)
	captchaPayload, err := json.Marshal(nextCaptcha)
	if err == nil {
		captchaRecord, _ = s.pg.UpsertPlatformSetting(ctx, systemdomain.SettingRecord{Key: adminCaptchaSettingKey, Value: captchaPayload, UpdatedBy: adminID})
	}

	// LDAP 配置
	var ldapRecord *systemdomain.SettingRecord
	if s.ldap != nil {
		current := s.ldap.CurrentConfig()
		next := applyLDAPPatch(current, patch.LDAP, s.ldap)
		next = systemdomain.NormalizeLDAPConfig(next)
		payload, err := json.Marshal(next)
		if err != nil {
			return nil, err
		}
		ldapRecord, err = s.pg.UpsertPlatformSetting(ctx, systemdomain.SettingRecord{Key: ldapSettingKey, Value: payload, UpdatedBy: adminID})
		if err != nil {
			return nil, err
		}
		if err := s.ldap.Reload(next); err != nil {
			return nil, apperrors.New(50093, http.StatusInternalServerError, "LDAP 配置热重载失败")
		}
	}

	// OIDC 配置
	var oidcRecord *systemdomain.SettingRecord
	if s.oidc != nil {
		current := s.oidc.CurrentConfig()
		next := applyOIDCPatch(current, patch.OIDC, s.oidc)
		next = systemdomain.NormalizeOIDCConfig(next)
		payload, err := json.Marshal(next)
		if err != nil {
			return nil, err
		}
		oidcRecord, err = s.pg.UpsertPlatformSetting(ctx, systemdomain.SettingRecord{Key: oidcSettingKey, Value: payload, UpdatedBy: adminID})
		if err != nil {
			return nil, err
		}
		if err := s.oidc.Reload(next); err != nil {
			return nil, apperrors.New(50094, http.StatusInternalServerError, "OIDC 配置热重载失败")
		}
	}

	if firewallRecord == nil {
		firewallRecord, _ = s.pg.GetPlatformSetting(ctx, firewallSettingKey)
	}
	if securityRecord == nil {
		securityRecord, _ = s.pg.GetPlatformSetting(ctx, securitySettingKey)
	}
	if ldapRecord == nil {
		ldapRecord, _ = s.pg.GetPlatformSetting(ctx, ldapSettingKey)
	}
	if oidcRecord == nil {
		oidcRecord, _ = s.pg.GetPlatformSetting(ctx, oidcSettingKey)
	}

	// 品牌配置
	var brandingRecord *systemdomain.SettingRecord
	{
		existingRecord, _ := s.pg.GetPlatformSetting(ctx, brandingSettingKey)
		current := loadBrandingFromRecord(existingRecord)
		next := applyBrandingPatch(current, patch.Branding)
		next = systemdomain.NormalizeBrandingConfig(next)
		payload, err := json.Marshal(next)
		if err == nil {
			brandingRecord, _ = s.pg.UpsertPlatformSetting(ctx, systemdomain.SettingRecord{Key: brandingSettingKey, Value: payload, UpdatedBy: adminID})
		}
	}
	if brandingRecord == nil {
		brandingRecord, _ = s.pg.GetPlatformSetting(ctx, brandingSettingKey)
	}

	if s.plugin != nil {
		go s.plugin.ExecuteHook(context.Background(), HookSettingsUpdated, map[string]any{}, plugindomain.HookMetadata{})
	}
	return &systemdomain.SettingsView{
		Firewall:     s.buildFirewallView(firewallRecord),
		Security:     s.buildSecurityView(securityRecord),
		AdminCaptcha: s.buildAdminCaptchaView(captchaRecord),
		LDAP:         s.buildLDAPView(ldapRecord),
		OIDC:         s.buildOIDCView(oidcRecord),
		Branding:     buildBrandingView(brandingRecord),
	}, nil
}

func (s *PlatformSettingsService) buildFirewallView(record *systemdomain.SettingRecord) systemdomain.FirewallSettingsView {
	if s.firewall == nil {
		return systemdomain.FirewallSettingsView{Source: "unavailable"}
	}
	current := s.firewall.CurrentConfig()
	reloadVersion, reloadedAt := s.firewall.ReloadMeta()
	view := systemdomain.FirewallSettingsView{
		Enabled:           current.Enabled,
		GlobalRate:        current.GlobalRate,
		AuthRate:          current.AuthRate,
		AdminRate:         current.AdminRate,
		CorazaEnabled:     current.CorazaEnabled,
		CorazaParanoia:    current.CorazaParanoia,
		RequestBodyLimit:  current.RequestBodyLimit,
		RequestBodyMemory: current.RequestBodyMemory,
		AllowedCIDRs:      cloneStrings(current.AllowedCIDRs),
		BlockedCIDRs:      cloneStrings(current.BlockedCIDRs),
		BlockedUserAgents: cloneStrings(current.BlockedUserAgents),
		BlockedPathPrefix: cloneStrings(current.BlockedPathPrefix),
		MaxPathLength:     current.MaxPathLength,
		MaxQueryLength:    current.MaxQueryLength,
		Source:            "environment",
		ReloadVersion:     reloadVersion,
		ReloadedAt:        reloadedAt,
	}
	if record != nil {
		view.Source = "database"
		view.UpdatedBy = record.UpdatedBy
		view.UpdatedAt = &record.UpdatedAt
	}
	return view
}

func (s *PlatformSettingsService) buildSecurityView(record *systemdomain.SettingRecord) systemdomain.SecuritySettingsView {
	if s.security == nil {
		return systemdomain.SecuritySettingsView{Source: "unavailable"}
	}
	current := s.security.CurrentConfig()
	reloadVersion, reloadedAt := s.security.ReloadMeta()
	view := systemdomain.SecuritySettingsView{
		MasterKeyConfigured: strings.TrimSpace(current.MasterKey) != "",
		ChallengeTTLSeconds: int64(current.ChallengeTTL / time.Second),
		Modules: systemdomain.SecurityModuleSettingsView{
			TOTPEnabled:          current.Modules.TOTPEnabled && current.TOTP.Enabled,
			RecoveryCodesEnabled: current.Modules.RecoveryCodesEnabled && current.RecoveryCode.Enabled,
			PasskeyEnabled:       current.Modules.PasskeyEnabled && current.Passkey.Enabled,
		},
		TOTP: systemdomain.SecurityTOTPSettingsView{
			Enabled:              current.TOTP.Enabled,
			Issuer:               current.TOTP.Issuer,
			EnrollmentTTLSeconds: int64(current.TOTP.EnrollmentTTL / time.Second),
			Skew:                 current.TOTP.Skew,
			Digits:               current.TOTP.Digits,
		},
		RecoveryCodes: systemdomain.SecurityRecoveryCodeSettingsView{
			Enabled: current.RecoveryCode.Enabled,
			Count:   current.RecoveryCode.Count,
			Length:  current.RecoveryCode.Length,
		},
		Passkey: systemdomain.SecurityPasskeySettingsView{
			Enabled:             current.Passkey.Enabled,
			RPDisplayName:       current.Passkey.RPDisplayName,
			RPID:                current.Passkey.RPID,
			RPOrigins:           cloneStrings(current.Passkey.RPOrigins),
			RPTopOrigins:        cloneStrings(current.Passkey.RPTopOrigins),
			ChallengeTTLSeconds: int64(current.Passkey.ChallengeTTL / time.Second),
			UserVerification:    current.Passkey.UserVerification,
		},
		RuntimeModules: cloneModuleStatuses(s.security.Modules()),
		Source:         "environment",
		ReloadVersion:  reloadVersion,
		ReloadedAt:     reloadedAt,
	}
	if record != nil {
		view.Source = "database"
		view.UpdatedBy = record.UpdatedBy
		view.UpdatedAt = &record.UpdatedAt
	}
	return view
}

func (s *PlatformSettingsService) decodeFirewallConfig(payload []byte) (config.FirewallConfig, error) {
	cfg := config.NormalizeFirewallConfig(s.cfg.Firewall)
	if len(payload) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(payload, &cfg); err != nil {
		return config.FirewallConfig{}, err
	}
	return config.NormalizeFirewallConfig(cfg), nil
}

func (s *PlatformSettingsService) decodeSecurityConfig(payload []byte) (config.SecurityConfig, error) {
	cfg := config.NormalizeSecurityConfig(s.cfg.Security, s.cfg.AppName, s.cfg.JWT.Secret)
	if len(payload) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(payload, &cfg); err != nil {
		return config.SecurityConfig{}, err
	}
	return config.NormalizeSecurityConfig(cfg, s.cfg.AppName, s.cfg.JWT.Secret), nil
}

func applyFirewallPatch(current config.FirewallConfig, patch systemdomain.FirewallSettingsPatch) config.FirewallConfig {
	if patch.Enabled != nil {
		current.Enabled = *patch.Enabled
	}
	if patch.GlobalRate != nil {
		current.GlobalRate = strings.TrimSpace(*patch.GlobalRate)
	}
	if patch.AuthRate != nil {
		current.AuthRate = strings.TrimSpace(*patch.AuthRate)
	}
	if patch.AdminRate != nil {
		current.AdminRate = strings.TrimSpace(*patch.AdminRate)
	}
	if patch.CorazaEnabled != nil {
		current.CorazaEnabled = *patch.CorazaEnabled
	}
	if patch.CorazaParanoia != nil {
		current.CorazaParanoia = *patch.CorazaParanoia
	}
	if patch.RequestBodyLimit != nil {
		current.RequestBodyLimit = *patch.RequestBodyLimit
	}
	if patch.RequestBodyMemory != nil {
		current.RequestBodyMemory = *patch.RequestBodyMemory
	}
	if patch.AllowedCIDRs != nil {
		current.AllowedCIDRs = compactStrings(*patch.AllowedCIDRs)
	}
	if patch.BlockedCIDRs != nil {
		current.BlockedCIDRs = compactStrings(*patch.BlockedCIDRs)
	}
	if patch.BlockedUserAgents != nil {
		current.BlockedUserAgents = compactStrings(*patch.BlockedUserAgents)
	}
	if patch.BlockedPathPrefix != nil {
		current.BlockedPathPrefix = compactStrings(*patch.BlockedPathPrefix)
	}
	if patch.MaxPathLength != nil {
		current.MaxPathLength = *patch.MaxPathLength
	}
	if patch.MaxQueryLength != nil {
		current.MaxQueryLength = *patch.MaxQueryLength
	}
	return current
}

func applySecurityPatch(current config.SecurityConfig, patch systemdomain.SecuritySettingsPatch) config.SecurityConfig {
	if patch.ChallengeTTLSeconds != nil && *patch.ChallengeTTLSeconds > 0 {
		current.ChallengeTTL = time.Duration(*patch.ChallengeTTLSeconds) * time.Second
	}
	if patch.Modules.TOTPEnabled != nil {
		current.Modules.TOTPEnabled = *patch.Modules.TOTPEnabled
		current.TOTP.Enabled = *patch.Modules.TOTPEnabled
	}
	if patch.Modules.RecoveryCodesEnabled != nil {
		current.Modules.RecoveryCodesEnabled = *patch.Modules.RecoveryCodesEnabled
		current.RecoveryCode.Enabled = *patch.Modules.RecoveryCodesEnabled
	}
	if patch.Modules.PasskeyEnabled != nil {
		current.Modules.PasskeyEnabled = *patch.Modules.PasskeyEnabled
		current.Passkey.Enabled = *patch.Modules.PasskeyEnabled
	}
	if patch.TOTP.Enabled != nil {
		current.TOTP.Enabled = *patch.TOTP.Enabled
		current.Modules.TOTPEnabled = *patch.TOTP.Enabled
	}
	if patch.TOTP.Issuer != nil {
		current.TOTP.Issuer = strings.TrimSpace(*patch.TOTP.Issuer)
	}
	if patch.TOTP.EnrollmentTTLSeconds != nil && *patch.TOTP.EnrollmentTTLSeconds > 0 {
		current.TOTP.EnrollmentTTL = time.Duration(*patch.TOTP.EnrollmentTTLSeconds) * time.Second
	}
	if patch.TOTP.Skew != nil {
		current.TOTP.Skew = *patch.TOTP.Skew
	}
	if patch.TOTP.Digits != nil {
		current.TOTP.Digits = *patch.TOTP.Digits
	}
	if patch.RecoveryCodes.Enabled != nil {
		current.RecoveryCode.Enabled = *patch.RecoveryCodes.Enabled
		current.Modules.RecoveryCodesEnabled = *patch.RecoveryCodes.Enabled
	}
	if patch.RecoveryCodes.Count != nil {
		current.RecoveryCode.Count = *patch.RecoveryCodes.Count
	}
	if patch.RecoveryCodes.Length != nil {
		current.RecoveryCode.Length = *patch.RecoveryCodes.Length
	}
	if patch.Passkey.Enabled != nil {
		current.Passkey.Enabled = *patch.Passkey.Enabled
		current.Modules.PasskeyEnabled = *patch.Passkey.Enabled
	}
	if patch.Passkey.RPDisplayName != nil {
		current.Passkey.RPDisplayName = strings.TrimSpace(*patch.Passkey.RPDisplayName)
	}
	if patch.Passkey.RPID != nil {
		current.Passkey.RPID = strings.TrimSpace(*patch.Passkey.RPID)
	}
	if patch.Passkey.RPOrigins != nil {
		current.Passkey.RPOrigins = compactStrings(*patch.Passkey.RPOrigins)
	}
	if patch.Passkey.RPTopOrigins != nil {
		current.Passkey.RPTopOrigins = compactStrings(*patch.Passkey.RPTopOrigins)
	}
	if patch.Passkey.ChallengeTTLSeconds != nil && *patch.Passkey.ChallengeTTLSeconds > 0 {
		current.Passkey.ChallengeTTL = time.Duration(*patch.Passkey.ChallengeTTLSeconds) * time.Second
	}
	if patch.Passkey.UserVerification != nil {
		current.Passkey.UserVerification = strings.TrimSpace(*patch.Passkey.UserVerification)
	}
	return current
}

func compactStrings(values []string) []string {
	items := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			items = append(items, value)
		}
	}
	return items
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	items := make([]string, len(values))
	copy(items, values)
	return items
}

func cloneModuleStatuses(values []securitydomain.ModuleStatus) []securitydomain.ModuleStatus {
	if len(values) == 0 {
		return nil
	}
	items := make([]securitydomain.ModuleStatus, len(values))
	copy(items, values)
	return items
}

// ── 管理员验证码配置 ──

type adminCaptchaConfig struct {
	Enabled            bool   `json:"enabled"`
	Type               string `json:"type"`
	RequireForLogin    bool   `json:"requireForLogin"`
	RequireForRegister bool   `json:"requireForRegister"`
	AudioLang          string `json:"audioLang"`
}

func (s *PlatformSettingsService) decodeAdminCaptchaConfig(record *systemdomain.SettingRecord) adminCaptchaConfig {
	cfg := adminCaptchaConfig{Type: "image"}
	if record != nil && len(record.Value) > 0 {
		_ = json.Unmarshal(record.Value, &cfg)
	}
	if cfg.Type == "" {
		cfg.Type = "image"
	}
	return cfg
}

func (s *PlatformSettingsService) buildAdminCaptchaView(record *systemdomain.SettingRecord) systemdomain.AdminCaptchaSettingsView {
	cfg := s.decodeAdminCaptchaConfig(record)
	audioLang := cfg.AudioLang
	if audioLang == "" {
		audioLang = "zh"
	}
	return systemdomain.AdminCaptchaSettingsView{
		Enabled:            cfg.Enabled,
		Type:               cfg.Type,
		RequireForLogin:    cfg.RequireForLogin,
		RequireForRegister: cfg.RequireForRegister,
		AudioLang:          audioLang,
	}
}

// GetAdminCaptchaConfig 获取管理员验证码配置（公开，登录/注册前调用）
func (s *PlatformSettingsService) GetAdminCaptchaConfig(ctx context.Context) systemdomain.AdminCaptchaSettingsView {
	record, err := s.pg.GetPlatformSetting(ctx, adminCaptchaSettingKey)
	if err != nil {
		return systemdomain.AdminCaptchaSettingsView{Type: "image"}
	}
	return s.buildAdminCaptchaView(record)
}

func applyAdminCaptchaPatch(current adminCaptchaConfig, patch systemdomain.AdminCaptchaSettingsPatch) adminCaptchaConfig {
	if patch.Enabled != nil {
		current.Enabled = *patch.Enabled
	}
	if patch.Type != nil {
		current.Type = strings.TrimSpace(*patch.Type)
	}
	if patch.RequireForLogin != nil {
		current.RequireForLogin = *patch.RequireForLogin
	}
	if patch.RequireForRegister != nil {
		current.RequireForRegister = *patch.RequireForRegister
	}
	if patch.AudioLang != nil {
		current.AudioLang = strings.TrimSpace(*patch.AudioLang)
	}
	return current
}

// ── LDAP 配置 ──

func (s *PlatformSettingsService) buildLDAPView(record *systemdomain.SettingRecord) systemdomain.LDAPSettingsView {
	if s.ldap == nil {
		return systemdomain.LDAPSettingsView{Source: "unavailable"}
	}
	cfg := s.ldap.CurrentConfig()
	view := systemdomain.LDAPSettingsView{
		Enabled:                  cfg.Enabled,
		Server:                   cfg.Server,
		Port:                     cfg.Port,
		UseTLS:                   cfg.UseTLS,
		UseStartTLS:              cfg.UseStartTLS,
		SkipTLSVerify:            cfg.SkipTLSVerify,
		BindDN:                   cfg.BindDN,
		HasBindPassword:          cfg.BindPassword != "",
		BaseDN:                   cfg.BaseDN,
		UserFilter:               cfg.UserFilter,
		UserAttribute:            cfg.UserAttribute,
		GroupBaseDN:              cfg.GroupBaseDN,
		GroupFilter:              cfg.GroupFilter,
		GroupAttribute:           cfg.GroupAttribute,
		AdminGroupDN:             cfg.AdminGroupDN,
		AttrMapping:              cfg.AttrMapping,
		ConnectionTimeoutSeconds: cfg.ConnectionTimeoutSeconds,
		SearchTimeoutSeconds:     cfg.SearchTimeoutSeconds,
		FallbackToLocal:          cfg.FallbackToLocal,
		Source:                   "unconfigured",
	}
	if record != nil {
		view.Source = "database"
		view.UpdatedBy = record.UpdatedBy
		view.UpdatedAt = &record.UpdatedAt
	}
	return view
}

func applyLDAPPatch(current systemdomain.LDAPConfig, patch systemdomain.LDAPSettingsPatch, ldapSvc *LDAPService) systemdomain.LDAPConfig {
	if patch.Enabled != nil {
		current.Enabled = *patch.Enabled
	}
	if patch.Server != nil {
		current.Server = strings.TrimSpace(*patch.Server)
	}
	if patch.Port != nil {
		current.Port = *patch.Port
	}
	if patch.UseTLS != nil {
		current.UseTLS = *patch.UseTLS
	}
	if patch.UseStartTLS != nil {
		current.UseStartTLS = *patch.UseStartTLS
	}
	if patch.SkipTLSVerify != nil {
		current.SkipTLSVerify = *patch.SkipTLSVerify
	}
	if patch.BindDN != nil {
		current.BindDN = strings.TrimSpace(*patch.BindDN)
	}
	if patch.BindPassword != nil && *patch.BindPassword != "" {
		if encrypted, err := ldapSvc.EncryptBindPassword(*patch.BindPassword); err == nil {
			current.BindPassword = encrypted
		}
	}
	if patch.BaseDN != nil {
		current.BaseDN = strings.TrimSpace(*patch.BaseDN)
	}
	if patch.UserFilter != nil {
		current.UserFilter = strings.TrimSpace(*patch.UserFilter)
	}
	if patch.UserAttribute != nil {
		current.UserAttribute = strings.TrimSpace(*patch.UserAttribute)
	}
	if patch.GroupBaseDN != nil {
		current.GroupBaseDN = strings.TrimSpace(*patch.GroupBaseDN)
	}
	if patch.GroupFilter != nil {
		current.GroupFilter = strings.TrimSpace(*patch.GroupFilter)
	}
	if patch.GroupAttribute != nil {
		current.GroupAttribute = strings.TrimSpace(*patch.GroupAttribute)
	}
	if patch.AdminGroupDN != nil {
		current.AdminGroupDN = strings.TrimSpace(*patch.AdminGroupDN)
	}
	if patch.AttrMapping != nil {
		if patch.AttrMapping.Account != nil {
			current.AttrMapping.Account = strings.TrimSpace(*patch.AttrMapping.Account)
		}
		if patch.AttrMapping.DisplayName != nil {
			current.AttrMapping.DisplayName = strings.TrimSpace(*patch.AttrMapping.DisplayName)
		}
		if patch.AttrMapping.Email != nil {
			current.AttrMapping.Email = strings.TrimSpace(*patch.AttrMapping.Email)
		}
		if patch.AttrMapping.Phone != nil {
			current.AttrMapping.Phone = strings.TrimSpace(*patch.AttrMapping.Phone)
		}
	}
	if patch.ConnectionTimeoutSeconds != nil {
		current.ConnectionTimeoutSeconds = *patch.ConnectionTimeoutSeconds
	}
	if patch.SearchTimeoutSeconds != nil {
		current.SearchTimeoutSeconds = *patch.SearchTimeoutSeconds
	}
	if patch.FallbackToLocal != nil {
		current.FallbackToLocal = *patch.FallbackToLocal
	}
	return current
}

// ── OIDC 配置 ──

func (s *PlatformSettingsService) buildOIDCView(record *systemdomain.SettingRecord) systemdomain.OIDCSettingsView {
	if s.oidc == nil {
		return systemdomain.OIDCSettingsView{Source: "unavailable"}
	}
	cfg := s.oidc.CurrentConfig()
	view := systemdomain.OIDCSettingsView{
		Enabled:         cfg.Enabled,
		IssuerURL:       cfg.IssuerURL,
		ClientID:        cfg.ClientID,
		HasClientSecret: cfg.ClientSecret != "",
		RedirectURL:     cfg.RedirectURL,
		Scopes:          cloneStrings(cfg.Scopes),
		AllowedDomains:  cloneStrings(cfg.AllowedDomains),
		AdminGroupClaim: cfg.AdminGroupClaim,
		AdminGroupValue: cfg.AdminGroupValue,
		AttrMapping:     cfg.AttrMapping,
		FallbackToLocal:     cfg.FallbackToLocal,
		FrontendCallbackURL: cfg.FrontendCallbackURL,
		Source:              "unconfigured",
	}
	if record != nil {
		view.Source = "database"
		view.UpdatedBy = record.UpdatedBy
		view.UpdatedAt = &record.UpdatedAt
	}
	return view
}

func applyOIDCPatch(current systemdomain.OIDCConfig, patch systemdomain.OIDCSettingsPatch, oidcSvc *OIDCService) systemdomain.OIDCConfig {
	if patch.Enabled != nil {
		current.Enabled = *patch.Enabled
	}
	if patch.IssuerURL != nil {
		current.IssuerURL = strings.TrimSpace(*patch.IssuerURL)
	}
	if patch.ClientID != nil {
		current.ClientID = strings.TrimSpace(*patch.ClientID)
	}
	if patch.ClientSecret != nil && *patch.ClientSecret != "" {
		if encrypted, err := oidcSvc.EncryptClientSecret(*patch.ClientSecret); err == nil {
			current.ClientSecret = encrypted
		}
	}
	if patch.RedirectURL != nil {
		current.RedirectURL = strings.TrimSpace(*patch.RedirectURL)
	}
	if patch.Scopes != nil {
		current.Scopes = compactStrings(*patch.Scopes)
	}
	if patch.AllowedDomains != nil {
		current.AllowedDomains = compactStrings(*patch.AllowedDomains)
	}
	if patch.AdminGroupClaim != nil {
		current.AdminGroupClaim = strings.TrimSpace(*patch.AdminGroupClaim)
	}
	if patch.AdminGroupValue != nil {
		current.AdminGroupValue = strings.TrimSpace(*patch.AdminGroupValue)
	}
	if patch.AttrMapping != nil {
		if patch.AttrMapping.Account != nil {
			current.AttrMapping.Account = strings.TrimSpace(*patch.AttrMapping.Account)
		}
		if patch.AttrMapping.DisplayName != nil {
			current.AttrMapping.DisplayName = strings.TrimSpace(*patch.AttrMapping.DisplayName)
		}
		if patch.AttrMapping.Email != nil {
			current.AttrMapping.Email = strings.TrimSpace(*patch.AttrMapping.Email)
		}
		if patch.AttrMapping.Phone != nil {
			current.AttrMapping.Phone = strings.TrimSpace(*patch.AttrMapping.Phone)
		}
	}
	if patch.FallbackToLocal != nil {
		current.FallbackToLocal = *patch.FallbackToLocal
	}
	if patch.FrontendCallbackURL != nil {
		current.FrontendCallbackURL = strings.TrimSpace(*patch.FrontendCallbackURL)
	}
	return current
}

// ── 品牌配置 ──

func loadBrandingFromRecord(record *systemdomain.SettingRecord) systemdomain.BrandingConfig {
	var cfg systemdomain.BrandingConfig
	if record != nil && len(record.Value) > 0 {
		_ = json.Unmarshal(record.Value, &cfg)
	}
	return systemdomain.NormalizeBrandingConfig(cfg)
}

func buildBrandingView(record *systemdomain.SettingRecord) systemdomain.BrandingSettingsView {
	cfg := loadBrandingFromRecord(record)
	view := systemdomain.BrandingSettingsView{
		BrandingConfig: cfg,
		Source:         "unconfigured",
	}
	if record != nil {
		view.Source = "database"
		view.UpdatedBy = record.UpdatedBy
		view.UpdatedAt = &record.UpdatedAt
	}
	return view
}

func applyBrandingPatch(current systemdomain.BrandingConfig, patch systemdomain.BrandingSettingsPatch) systemdomain.BrandingConfig {
	if patch.PlatformName != nil {
		current.PlatformName = strings.TrimSpace(*patch.PlatformName)
	}
	if patch.ConsoleName != nil {
		current.ConsoleName = strings.TrimSpace(*patch.ConsoleName)
	}
	if patch.LogoURL != nil {
		current.LogoURL = strings.TrimSpace(*patch.LogoURL)
	}
	if patch.LogoDarkURL != nil {
		current.LogoDarkURL = strings.TrimSpace(*patch.LogoDarkURL)
	}
	if patch.FaviconURL != nil {
		current.FaviconURL = strings.TrimSpace(*patch.FaviconURL)
	}
	if patch.PrimaryColor != nil {
		current.PrimaryColor = strings.TrimSpace(*patch.PrimaryColor)
	}
	if patch.PrimaryColorDark != nil {
		current.PrimaryColorDark = strings.TrimSpace(*patch.PrimaryColorDark)
	}
	if patch.AccentColor != nil {
		current.AccentColor = strings.TrimSpace(*patch.AccentColor)
	}
	if patch.LoginBgURL != nil {
		current.LoginBgURL = strings.TrimSpace(*patch.LoginBgURL)
	}
	if patch.LoginBgColor != nil {
		current.LoginBgColor = strings.TrimSpace(*patch.LoginBgColor)
	}
	if patch.FooterText != nil {
		current.FooterText = *patch.FooterText
	}
	if patch.CustomCSS != nil {
		current.CustomCSS = *patch.CustomCSS
	}
	return current
}
