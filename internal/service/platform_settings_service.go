package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"aegis/internal/cluster"
	"aegis/internal/config"
	captchadomain "aegis/internal/domain/captcha"
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
const samlSettingKey = "admin.saml"
const brandingSettingKey = "platform.branding"
const selfServiceSettingKey = "admin.self_service"

type PlatformSettingsService struct {
	cfg      config.Config
	log      *zap.Logger
	pg       *pgrepo.Repository
	firewall firewallRuntime
	security securityRuntime
	ldap     *LDAPService
	oidc     *OIDCService
	saml     *SAMLService
	plugin   *PluginService
	// cluster 改完设置后通知其他实例重载（防火墙 / 认证安全 / LDAP / OIDC / SAML 都是进程内的运行时状态）
	cluster clusterLink
}

// 平台设置里需要热重载到进程内运行时的分区，也是集群广播里的分区名。
// 验证码、品牌、自助能力每次判定都现读数据库，不需要广播。
const (
	platformSectionFirewall = "firewall"
	platformSectionSecurity = "security"
	platformSectionLDAP     = "ldap"
	platformSectionOIDC     = "oidc"
	platformSectionSAML     = "saml"
)

// platformSettingsChange 集群广播的消息体：只说「哪几个分区变了」，内容由接收方自己从数据库读。
// 不在消息里带配置本身：里面有 LDAP 绑定密码、OIDC 客户端密钥，没必要让它们多过一道消息总线。
type platformSettingsChange struct {
	Sections []string `json:"sections"`
}

// SetCluster 接上集群：订阅其他实例的设置变更广播，收到后从数据库重载对应分区。
//
// 重载走 reloadPersisted，它只读库、改本地运行时，不再广播 —— 否则 A 广播、B 重载后又广播、A 再重载……
// 总线本身也会跳过本实例自己发出的消息，两道保险。
func (s *PlatformSettingsService) SetCluster(c *cluster.Cluster) {
	if c == nil {
		return
	}
	s.cluster.set(c)
	c.Subscribe(cluster.TopicPlatformSettings, s.handleClusterChange)
}

// handleClusterChange 处理其他实例发来的设置变更广播。
func (s *PlatformSettingsService) handleClusterChange(payload []byte) {
	var change platformSettingsChange
	if err := json.Unmarshal(payload, &change); err != nil {
		s.log.Warn("平台设置变更广播无法解析", zap.Error(err))
		return
	}
	if len(change.Sections) == 0 {
		// 空分区在 reloadPersisted 里表示「全部」；广播方不会发空列表，收到了当成格式错误丢掉
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := s.reloadPersisted(ctx, change.Sections...); err != nil {
		s.log.Warn("按集群广播重载平台设置失败", zap.Strings("sections", change.Sections), zap.Error(err))
		return
	}
	s.log.Info("已按集群广播重载平台设置", zap.Strings("sections", change.Sections))
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

func NewPlatformSettingsService(cfg config.Config, log *zap.Logger, pg *pgrepo.Repository, firewall firewallRuntime, security securityRuntime, ldap *LDAPService, oidc *OIDCService, saml *SAMLService) *PlatformSettingsService {
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
		saml:     saml,
	}
}

func (s *PlatformSettingsService) Initialize(ctx context.Context) error {
	return s.reloadPersisted(ctx)
}

// reloadPersisted 从数据库读出持久化的设置并热重载到进程内运行时。sections 为空表示全部分区。
// 启动时（Initialize）与收到其他实例的变更广播时共用这一条路径，两边的解析与归一化保证一致。
func (s *PlatformSettingsService) reloadPersisted(ctx context.Context, sections ...string) error {
	want := func(section string) bool {
		if len(sections) == 0 {
			return true
		}
		for _, item := range sections {
			if item == section {
				return true
			}
		}
		return false
	}
	if s.firewall != nil && want(platformSectionFirewall) {
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

	if s.security != nil && want(platformSectionSecurity) {
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

	if s.ldap != nil && want(platformSectionLDAP) {
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

	if s.oidc != nil && want(platformSectionOIDC) {
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
	if s.saml != nil && want(platformSectionSAML) {
		record, err := s.pg.GetPlatformSetting(ctx, samlSettingKey)
		if err != nil {
			return err
		}
		if record != nil && len(record.Value) > 0 {
			var cfg systemdomain.SAMLConfig
			if err := json.Unmarshal(record.Value, &cfg); err != nil {
				s.log.Error("decode persisted SAML settings failed", zap.Error(err))
			} else {
				cfg = systemdomain.NormalizeSAMLConfig(cfg)
				if err := s.saml.Reload(cfg); err != nil {
					s.log.Error("reload persisted SAML settings failed", zap.Error(err))
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
	samlRecord, _ := s.pg.GetPlatformSetting(ctx, samlSettingKey)
	brandingRecord, _ := s.pg.GetPlatformSetting(ctx, brandingSettingKey)
	selfServiceRecord, _ := s.pg.GetPlatformSetting(ctx, selfServiceSettingKey)
	return &systemdomain.SettingsView{
		Firewall:     s.buildFirewallView(firewallRecord),
		Security:     s.buildSecurityView(securityRecord),
		AdminCaptcha: s.buildAdminCaptchaView(captchaRecord),
		LDAP:         s.buildLDAPView(ldapRecord),
		OIDC:         s.buildOIDCView(oidcRecord),
		SAML:         s.buildSAMLView(samlRecord),
		Branding:     buildBrandingView(brandingRecord),
		SelfService:  buildSelfServiceView(selfServiceRecord),
	}, nil
}

func (s *PlatformSettingsService) UpdateSettings(ctx context.Context, adminID *int64, patch systemdomain.SettingsUpdate) (*systemdomain.SettingsView, error) {
	// 每个分区写库成功就记一笔，函数无论从哪里返回都把已落库的分区广播出去：
	// 中途某个分区校验失败时，前面已经写进库的分区在其他实例上也得生效，否则各实例从此各说各话。
	var persisted []string
	defer func() {
		if len(persisted) > 0 {
			s.cluster.publish(s.log, cluster.TopicPlatformSettings, platformSettingsChange{Sections: persisted})
		}
	}()
	var firewallRecord *systemdomain.SettingRecord
	if s.firewall != nil {
		current := s.firewall.CurrentConfig()
		next := applyFirewallPatch(current, patch.Firewall)
		next = config.NormalizeFirewallConfig(next)
		if err := s.firewall.ValidateConfig(next); err != nil {
			return nil, apperrors.New(40090, http.StatusBadRequest, "防火墙配置无效："+err.Error())
		}
		payload, err := json.Marshal(next)
		if err != nil {
			return nil, err
		}
		firewallRecord, err = s.pg.UpsertPlatformSetting(ctx, systemdomain.SettingRecord{Key: firewallSettingKey, Value: payload, UpdatedBy: adminID})
		if err != nil {
			return nil, err
		}
		persisted = append(persisted, platformSectionFirewall)
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
		persisted = append(persisted, platformSectionSecurity)
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
		persisted = append(persisted, platformSectionLDAP)
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
		persisted = append(persisted, platformSectionOIDC)
		if err := s.oidc.Reload(next); err != nil {
			return nil, apperrors.New(50094, http.StatusInternalServerError, "OIDC 配置热重载失败")
		}
	}

	// SAML 配置
	var samlRecord *systemdomain.SettingRecord
	if s.saml != nil {
		current := s.saml.CurrentConfig()
		next := applySAMLPatch(current, patch.SAML, s.saml)
		next = systemdomain.NormalizeSAMLConfig(next)
		if next.Enabled {
			var err error
			next, err = s.saml.EnsureCredentials(next)
			if err != nil {
				return nil, apperrors.New(40099, http.StatusBadRequest, "SAML SP 凭证生成失败")
			}
		}
		payload, err := json.Marshal(next)
		if err != nil {
			return nil, err
		}
		samlRecord, err = s.pg.UpsertPlatformSetting(ctx, systemdomain.SettingRecord{Key: samlSettingKey, Value: payload, UpdatedBy: adminID})
		if err != nil {
			return nil, err
		}
		persisted = append(persisted, platformSectionSAML)
		if err := s.saml.Reload(next); err != nil {
			return nil, apperrors.New(50095, http.StatusInternalServerError, "SAML 配置热重载失败")
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
	if samlRecord == nil {
		samlRecord, _ = s.pg.GetPlatformSetting(ctx, samlSettingKey)
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

	// 自助能力配置（纯 JSON 持久化，无热重载 —— 每次判定都现读）
	var selfServiceRecord *systemdomain.SettingRecord
	{
		existingRecord, _ := s.pg.GetPlatformSetting(ctx, selfServiceSettingKey)
		next := applySelfServicePatch(decodeSelfServiceConfig(existingRecord), patch.SelfService)
		payload, err := json.Marshal(next)
		if err == nil {
			selfServiceRecord, _ = s.pg.UpsertPlatformSetting(ctx, systemdomain.SettingRecord{Key: selfServiceSettingKey, Value: payload, UpdatedBy: adminID})
		}
	}
	if selfServiceRecord == nil {
		selfServiceRecord, _ = s.pg.GetPlatformSetting(ctx, selfServiceSettingKey)
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
		SAML:         s.buildSAMLView(samlRecord),
		Branding:     buildBrandingView(brandingRecord),
		SelfService:  buildSelfServiceView(selfServiceRecord),
	}, nil
}

// GetFirewallSettings 只取防火墙分区（防火墙工作台的「设置」页用）。
func (s *PlatformSettingsService) GetFirewallSettings(ctx context.Context) (systemdomain.FirewallSettingsView, error) {
	record, err := s.pg.GetPlatformSetting(ctx, firewallSettingKey)
	if err != nil {
		return systemdomain.FirewallSettingsView{}, err
	}
	return s.buildFirewallView(record), nil
}

// UpdateFirewallSettings 只改防火墙分区：校验（含 WAF 试编译）→ 落库 → 本机热更新 → 广播其他副本。
func (s *PlatformSettingsService) UpdateFirewallSettings(ctx context.Context, adminID *int64, patch systemdomain.FirewallSettingsPatch) (systemdomain.FirewallSettingsView, error) {
	if s.firewall == nil {
		return systemdomain.FirewallSettingsView{}, apperrors.New(50390, http.StatusServiceUnavailable, "防火墙未启用")
	}
	next := config.NormalizeFirewallConfig(applyFirewallPatch(s.firewall.CurrentConfig(), patch))
	if err := s.firewall.ValidateConfig(next); err != nil {
		return systemdomain.FirewallSettingsView{}, apperrors.New(40090, http.StatusBadRequest, "防火墙配置无效："+err.Error())
	}
	payload, err := json.Marshal(next)
	if err != nil {
		return systemdomain.FirewallSettingsView{}, err
	}
	record, err := s.pg.UpsertPlatformSetting(ctx, systemdomain.SettingRecord{Key: firewallSettingKey, Value: payload, UpdatedBy: adminID})
	if err != nil {
		return systemdomain.FirewallSettingsView{}, err
	}
	s.cluster.publish(s.log, cluster.TopicPlatformSettings, platformSettingsChange{Sections: []string{platformSectionFirewall}})
	if err := s.firewall.Reload(next); err != nil {
		return systemdomain.FirewallSettingsView{}, apperrors.New(50090, http.StatusInternalServerError, "防火墙热重载失败："+err.Error())
	}
	return s.buildFirewallView(record), nil
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
		AllowedCIDRs:      stringsOrEmpty(current.AllowedCIDRs),
		BlockedCIDRs:      stringsOrEmpty(current.BlockedCIDRs),
		BlockedUserAgents: stringsOrEmpty(current.BlockedUserAgents),
		BlockedPathPrefix: stringsOrEmpty(current.BlockedPathPrefix),
		MaxPathLength:     current.MaxPathLength,
		MaxQueryLength:    current.MaxQueryLength,
		DefaultBanMode:    current.DefaultBanMode,
		TarpitDelayMs:     current.TarpitDelayMs,
		BanRedirectURL:    current.BanRedirectURL,

		RulesetVersion:           current.CorazaRulesetVersion,
		CorazaDetectionOnly:      current.CorazaDetectionOnly,
		CorazaDetectionParanoia:  current.CorazaDetectionParanoia,
		CorazaAnomalyThreshold:   current.CorazaAnomalyThreshold,
		CorazaOutboundThreshold:  current.CorazaOutboundThreshold,
		CorazaResponseInspection: current.CorazaResponseInspection,
		CorazaResponseBodyLimit:  current.CorazaResponseBodyLimit,
		CorazaRemovedRules:       stringsOrEmpty(current.CorazaRemovedRules),
		CorazaDisabledGroups:     stringsOrEmpty(current.CorazaDisabledGroups),
		CorazaInspectAdminBody:   current.CorazaInspectAdminBody,
		CorazaBodyExemptPrefixes: stringsOrEmpty(current.CorazaBodyExemptPrefixes),
		CorazaSensitiveArgs:      stringsOrEmpty(current.CorazaSensitiveArgs),
		CorazaRichTextArgs:       stringsOrEmpty(current.CorazaRichTextArgs),
		CorazaArgumentsLimit:     current.CorazaArgumentsLimit,
		CorazaExtraContentTypes:  stringsOrEmpty(current.CorazaExtraContentTypes),
		TrustedCIDRs:             stringsOrEmpty(current.TrustedCIDRs),
		BlockedSignatures:        stringsOrEmpty(current.BlockedSignatures),
		Source:                   "environment",
		ReloadVersion:            reloadVersion,
		ReloadedAt:               reloadedAt,
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
	// 打底的环境配置已经归一化成当前规则集版本；库里 v1 时代存下的记录根本没有这个字段，
	// 不清零的话 Unmarshal 之后仍是「已是 v2」，旧默认值（阈值 40、17 条全局移除）就永远迁不过来。
	cfg.CorazaRulesetVersion = 0
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
	if patch.DefaultBanMode != nil {
		current.DefaultBanMode = strings.TrimSpace(*patch.DefaultBanMode)
	}
	if patch.TarpitDelayMs != nil {
		current.TarpitDelayMs = *patch.TarpitDelayMs
	}
	if patch.BanRedirectURL != nil {
		current.BanRedirectURL = strings.TrimSpace(*patch.BanRedirectURL)
	}
	if patch.CorazaDetectionOnly != nil {
		current.CorazaDetectionOnly = *patch.CorazaDetectionOnly
	}
	if patch.CorazaDetectionParanoia != nil {
		current.CorazaDetectionParanoia = *patch.CorazaDetectionParanoia
	}
	if patch.CorazaAnomalyThreshold != nil {
		current.CorazaAnomalyThreshold = *patch.CorazaAnomalyThreshold
	}
	if patch.CorazaOutboundThreshold != nil {
		current.CorazaOutboundThreshold = *patch.CorazaOutboundThreshold
	}
	if patch.CorazaResponseInspection != nil {
		current.CorazaResponseInspection = *patch.CorazaResponseInspection
	}
	if patch.CorazaResponseBodyLimit != nil {
		current.CorazaResponseBodyLimit = *patch.CorazaResponseBodyLimit
	}
	if patch.CorazaInspectAdminBody != nil {
		current.CorazaInspectAdminBody = *patch.CorazaInspectAdminBody
	}
	if patch.CorazaArgumentsLimit != nil {
		current.CorazaArgumentsLimit = *patch.CorazaArgumentsLimit
	}
	// 列表：空数组 = 显式清空（非 nil 的空切片，落库为 []）；nil 才表示「用默认值」，见 config.NormalizeFirewallConfig。
	explicit := func(values []string) []string {
		values = compactStrings(values)
		if values == nil {
			return []string{}
		}
		return values
	}
	if patch.CorazaRemovedRules != nil {
		current.CorazaRemovedRules = explicit(*patch.CorazaRemovedRules)
	}
	if patch.CorazaDisabledGroups != nil {
		current.CorazaDisabledGroups = compactStrings(*patch.CorazaDisabledGroups)
	}
	if patch.CorazaBodyExemptPrefixes != nil {
		current.CorazaBodyExemptPrefixes = explicit(*patch.CorazaBodyExemptPrefixes)
	}
	if patch.CorazaSensitiveArgs != nil {
		current.CorazaSensitiveArgs = explicit(*patch.CorazaSensitiveArgs)
	}
	if patch.CorazaRichTextArgs != nil {
		current.CorazaRichTextArgs = explicit(*patch.CorazaRichTextArgs)
	}
	if patch.CorazaExtraContentTypes != nil {
		current.CorazaExtraContentTypes = explicit(*patch.CorazaExtraContentTypes)
	}
	if patch.TrustedCIDRs != nil {
		current.TrustedCIDRs = compactStrings(*patch.TrustedCIDRs)
	}
	if patch.BlockedSignatures != nil {
		current.BlockedSignatures = explicit(*patch.BlockedSignatures)
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

// stringsOrEmpty 复制一份；空时返回 []（JSON 里是 [] 而不是 null，控制台不用到处判空）。
func stringsOrEmpty(values []string) []string {
	items := make([]string, len(values))
	copy(items, values)
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
	Enabled            bool                        `json:"enabled"`
	Type               string                      `json:"type"`
	RequireForLogin    bool                        `json:"requireForLogin"`
	RequireForRegister bool                        `json:"requireForRegister"`
	AudioLang          string                      `json:"audioLang"`
	Dynamic            captchadomain.DynamicConfig `json:"dynamic"`
}

func (s *PlatformSettingsService) decodeAdminCaptchaConfig(record *systemdomain.SettingRecord) adminCaptchaConfig {
	// 先铺默认值再反序列化：库里可能只存了部分字段
	cfg := adminCaptchaConfig{Type: "image", Dynamic: captchadomain.DefaultDynamicConfig()}
	if record != nil && len(record.Value) > 0 {
		_ = json.Unmarshal(record.Value, &cfg)
	}
	if cfg.Type == "" {
		cfg.Type = "image"
	}
	cfg.Dynamic = cfg.Dynamic.Normalized()
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
		Dynamic:            cfg.Dynamic,
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

// BrandingPlatformName 当前生效的平台名称，读不到时返回空串。
//
// 给的是名称而不是整份品牌配置：调用方（法律文本的占位符替换）只需要这一个字段，
// 返回整份配置会让「谁在读品牌配置」这件事在代码里查不清楚。
func (s *PlatformSettingsService) BrandingPlatformName(ctx context.Context) string {
	record, err := s.pg.GetPlatformSetting(ctx, brandingSettingKey)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(loadBrandingFromRecord(record).PlatformName)
}

// ── 自助能力配置 ──

// selfServiceDefaults 是"这个键还没落过库"时生效的取值。
//
// 注册默认开着：这条路由在引入本配置之前就是开的，把默认值改成关等于
// 一次静默的行为变更 —— 升级后所有部署方的注册页会突然开始报 403。
// 自助建应用同样默认开着，它是零角色账号唯一的自举出口；真正的闸门是配额。
var selfServiceDefaults = systemdomain.SelfServiceSettingsView{
	AllowRegistration: true,
	AllowAppCreation:  true,
	MaxAppsPerAdmin:   3,
	CreatorRoleKey:    "app_admin",
}

// selfServiceMaxAppsCeiling 配额上限。
// 不设上限时「不限」与「填了个很大的数」在库里长得一样，
// 而前者应该显式写 0 —— 那是唯一能在 UI 上表达清楚的写法。
const selfServiceMaxAppsCeiling = 1000

func decodeSelfServiceConfig(record *systemdomain.SettingRecord) systemdomain.SelfServiceSettingsView {
	cfg := selfServiceDefaults
	if record != nil && len(record.Value) > 0 {
		_ = json.Unmarshal(record.Value, &cfg)
	}
	return normalizeSelfServiceConfig(cfg)
}

func normalizeSelfServiceConfig(cfg systemdomain.SelfServiceSettingsView) systemdomain.SelfServiceSettingsView {
	if cfg.MaxAppsPerAdmin < 0 {
		cfg.MaxAppsPerAdmin = 0
	}
	if cfg.MaxAppsPerAdmin > selfServiceMaxAppsCeiling {
		cfg.MaxAppsPerAdmin = selfServiceMaxAppsCeiling
	}
	cfg.CreatorRoleKey = strings.TrimSpace(cfg.CreatorRoleKey)
	if cfg.CreatorRoleKey == "" {
		cfg.CreatorRoleKey = selfServiceDefaults.CreatorRoleKey
	}
	return cfg
}

func buildSelfServiceView(record *systemdomain.SettingRecord) systemdomain.SelfServiceSettingsView {
	return decodeSelfServiceConfig(record)
}

func applySelfServicePatch(current systemdomain.SelfServiceSettingsView, patch systemdomain.SelfServiceSettingsPatch) systemdomain.SelfServiceSettingsView {
	if patch.AllowRegistration != nil {
		current.AllowRegistration = *patch.AllowRegistration
	}
	if patch.AllowAppCreation != nil {
		current.AllowAppCreation = *patch.AllowAppCreation
	}
	if patch.MaxAppsPerAdmin != nil {
		current.MaxAppsPerAdmin = *patch.MaxAppsPerAdmin
	}
	if patch.CreatorRoleKey != nil {
		current.CreatorRoleKey = strings.TrimSpace(*patch.CreatorRoleKey)
	}
	return normalizeSelfServiceConfig(current)
}

// GetSelfServiceConfig 自助能力配置的读取入口。
//
// 读库失败时返回默认值而不是报错：这份配置的两个消费点（注册、建应用）
// 都在用户面前，一次库抖动把它们变成 500 比暂时按默认放行更糟 ——
// 而默认值恰好就是引入本配置之前的行为。
func (s *PlatformSettingsService) GetSelfServiceConfig(ctx context.Context) systemdomain.SelfServiceSettingsView {
	record, err := s.pg.GetPlatformSetting(ctx, selfServiceSettingKey)
	if err != nil {
		s.log.Warn("自助能力配置读取失败，按默认值放行", zap.Error(err))
		return selfServiceDefaults
	}
	return decodeSelfServiceConfig(record)
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
	// 留空即不修改
	if patch.Dynamic != nil {
		current.Dynamic = patch.Dynamic.Normalized()
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
		Enabled:             cfg.Enabled,
		IssuerURL:           cfg.IssuerURL,
		ClientID:            cfg.ClientID,
		HasClientSecret:     cfg.ClientSecret != "",
		RedirectURL:         cfg.RedirectURL,
		Scopes:              cloneStrings(cfg.Scopes),
		AllowedDomains:      cloneStrings(cfg.AllowedDomains),
		AdminGroupClaim:     cfg.AdminGroupClaim,
		AdminGroupValue:     cfg.AdminGroupValue,
		AttrMapping:         cfg.AttrMapping,
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

func (s *PlatformSettingsService) buildSAMLView(record *systemdomain.SettingRecord) systemdomain.SAMLSettingsView {
	if s.saml == nil {
		return systemdomain.SAMLSettingsView{Source: "unavailable"}
	}
	cfg := s.saml.CurrentConfig()
	view := systemdomain.SAMLSettingsView{
		Enabled:             cfg.Enabled,
		IDPMetadataURL:      cfg.IDPMetadataURL,
		HasIDPMetadataXML:   cfg.IDPMetadataXML != "",
		EntityID:            cfg.EntityID,
		MetadataURL:         cfg.MetadataURL,
		ACSURL:              cfg.ACSURL,
		SPCertificate:       cfg.SPCertificate,
		HasSPPrivateKey:     cfg.SPPrivateKey != "",
		NameIDFormat:        cfg.NameIDFormat,
		SignAuthnRequests:   cfg.SignAuthnRequests,
		ForceAuthn:          cfg.ForceAuthn,
		AllowIDPInitiated:   cfg.AllowIDPInitiated,
		AllowedDomains:      cloneStrings(cfg.AllowedDomains),
		AdminGroupAttribute: cfg.AdminGroupAttribute,
		AdminGroupValue:     cfg.AdminGroupValue,
		AttrMapping:         cfg.AttrMapping,
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

func applySAMLPatch(current systemdomain.SAMLConfig, patch systemdomain.SAMLSettingsPatch, samlSvc *SAMLService) systemdomain.SAMLConfig {
	if patch.Enabled != nil {
		current.Enabled = *patch.Enabled
	}
	if patch.IDPMetadataURL != nil {
		current.IDPMetadataURL = strings.TrimSpace(*patch.IDPMetadataURL)
	}
	if patch.IDPMetadataXML != nil {
		current.IDPMetadataXML = strings.TrimSpace(*patch.IDPMetadataXML)
	}
	if patch.EntityID != nil {
		current.EntityID = strings.TrimSpace(*patch.EntityID)
	}
	if patch.MetadataURL != nil {
		current.MetadataURL = strings.TrimSpace(*patch.MetadataURL)
	}
	if patch.ACSURL != nil {
		current.ACSURL = strings.TrimSpace(*patch.ACSURL)
	}
	if patch.SPCertificate != nil {
		current.SPCertificate = strings.TrimSpace(*patch.SPCertificate)
	}
	if patch.SPPrivateKey != nil && strings.TrimSpace(*patch.SPPrivateKey) != "" {
		if encrypted, err := samlSvc.EncryptPrivateKey(strings.TrimSpace(*patch.SPPrivateKey)); err == nil {
			current.SPPrivateKey = encrypted
		}
	}
	if patch.NameIDFormat != nil {
		current.NameIDFormat = strings.TrimSpace(*patch.NameIDFormat)
	}
	if patch.SignAuthnRequests != nil {
		current.SignAuthnRequests = *patch.SignAuthnRequests
	}
	if patch.ForceAuthn != nil {
		current.ForceAuthn = *patch.ForceAuthn
	}
	if patch.AllowIDPInitiated != nil {
		current.AllowIDPInitiated = *patch.AllowIDPInitiated
	}
	if patch.AllowedDomains != nil {
		current.AllowedDomains = compactStrings(*patch.AllowedDomains)
	}
	if patch.AdminGroupAttribute != nil {
		current.AdminGroupAttribute = strings.TrimSpace(*patch.AdminGroupAttribute)
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
		if patch.AttrMapping.Groups != nil {
			current.AttrMapping.Groups = strings.TrimSpace(*patch.AttrMapping.Groups)
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
