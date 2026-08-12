package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	appdomain "aegis/internal/domain/app"
	captchadomain "aegis/internal/domain/captcha"
	platformdomain "aegis/internal/domain/platform"
	plugindomain "aegis/internal/domain/plugin"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/receipt"
	"aegis/pkg/timeutil"
	gojson "github.com/goccy/go-json"
	"go.uber.org/zap"
)

type AppService struct {
	log      *zap.Logger
	pg       *pgrepo.Repository
	sessions *redisrepo.SessionRepository
	location *time.Location
	plugin   *PluginService
	// governance 平台治理判定。与 apps.status 是「与」的关系：
	// 应用自己把开关打开也盖不过平台的冻结结论。
	governance *PlatformGovernanceService
	// storage Banner 图片的落地通道。构造期还没有 StorageService，
	// 与 plugin / governance 一样走 setter 注入。为空时上传接口如实报「存储未启用」。
	storage *StorageService
}

func (s *AppService) SetPluginService(p *PluginService) { s.plugin = p }

// SetStorageService 注入存储服务（bootstrap 中调用，避免构造期循环依赖）。
func (s *AppService) SetStorageService(st *StorageService) { s.storage = st }

// SetGovernanceService 注入平台治理服务（bootstrap 中调用，避免构造期循环依赖）。
func (s *AppService) SetGovernanceService(g *PlatformGovernanceService) { s.governance = g }

func NewAppService(log *zap.Logger, pg *pgrepo.Repository, sessions *redisrepo.SessionRepository) *AppService {
	return &AppService{log: log, pg: pg, sessions: sessions, location: timeutil.DefaultLocation()}
}

func (s *AppService) GetApp(ctx context.Context, appID int64) (*appdomain.App, error) {
	if s.sessions != nil {
		cached, err := s.sessions.GetAppByID(ctx, appID)
		if err != nil {
			s.log.Warn("load app cache failed", zap.Int64("appid", appID), zap.Error(err))
		} else if cached != nil {
			return cached, nil
		}
	}
	app, err := s.pg.GetAppByID(ctx, appID)
	if err != nil {
		return nil, err
	}
	if app == nil {
		return nil, apperrors.New(40410, http.StatusNotFound, "无法找到该应用")
	}
	if s.sessions != nil {
		if err := s.sessions.SetAppByID(ctx, appID, *app, 5*time.Minute); err != nil {
			s.log.Warn("cache app failed", zap.Int64("appid", appID), zap.Error(err))
		}
	}
	return app, nil
}

func (s *AppService) ResolvePolicy(app *appdomain.App) appdomain.Policy {
	policy := appdomain.Policy{
		MultiDeviceLogin: true,
	}
	if app == nil || app.Settings == nil {
		return policy
	}
	policy.LoginCheckDevice = boolSetting(app.Settings, "loginCheckDevice")
	policy.LoginCheckUser = boolSetting(app.Settings, "loginCheckUser")
	policy.LoginCheckIP = boolSetting(app.Settings, "loginCheckIp")
	policy.DeviceRebindInterval = max(0, intSetting(app.Settings, "loginCheckDeviceTimeOut"))
	policy.RegisterCheckIP = boolSetting(app.Settings, "registerCheckIp")
	if value, ok := lookupBool(app.Settings, "multiDeviceLogin"); ok {
		policy.MultiDeviceLogin = value
	}
	policy.MultiDeviceLimit = intSetting(app.Settings, "multiDeviceLoginNum")
	if !policy.MultiDeviceLogin {
		policy.MultiDeviceLimit = 1
	}
	return policy
}

// resolveCommerceSettings 解析应用级交易设置，未配置时回落到平台默认兑换率。
//
// 包级函数而非方法：PaymentService 下单时也要读同一份兑换率，但它不持有 AppService。
// 兑换率与兜底值只在这里定义一次，避免「控制台显示 100、下单按另一个值算」。
func resolveCommerceSettings(app *appdomain.App) appdomain.CommerceSettings {
	settings := appdomain.CommerceSettings{
		IntegralPerCurrency: appdomain.DefaultIntegralPerCurrency,
		WalletCurrency:      appdomain.DefaultWalletCurrency,
	}
	if app == nil {
		return settings
	}
	if value, ok := lookupInt(app.Settings, "integralPerCurrency"); ok && value > 0 {
		settings.IntegralPerCurrency = value
	}
	if value, ok := app.Settings["receiptEmailOnPaid"].(bool); ok {
		settings.ReceiptEmailOnPaid = value
	}
	if value, ok := app.Settings["receiptLocale"].(string); ok {
		settings.ReceiptLocale = strings.TrimSpace(value)
	}
	if value, ok := app.Settings["walletCurrency"].(string); ok {
		if code := strings.ToUpper(strings.TrimSpace(value)); code != "" {
			settings.WalletCurrency = code
		}
	}
	return settings
}

// ResolveCommerceSettings 见 resolveCommerceSettings。
func (s *AppService) ResolveCommerceSettings(app *appdomain.App) appdomain.CommerceSettings {
	return resolveCommerceSettings(app)
}

// GetCommerceSettings 读取应用级交易设置。
func (s *AppService) GetCommerceSettings(ctx context.Context, appID int64) (*appdomain.CommerceSettings, error) {
	app, err := s.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	settings := s.ResolveCommerceSettings(app)
	return &settings, nil
}

// UpdateCommerceSettings 写入应用级交易设置。
func (s *AppService) UpdateCommerceSettings(ctx context.Context, appID int64, input appdomain.CommerceSettings) (*appdomain.CommerceSettings, error) {
	if input.IntegralPerCurrency < 1 || input.IntegralPerCurrency > 1_000_000 {
		return nil, apperrors.New(40029, http.StatusBadRequest, "积分兑换率必须在 1-1000000 之间")
	}
	// 语言当场校验：存一个渲染器不认识的语言，表现是几个月后某封凭证邮件
	// 悄悄变成了英文，那时候没人会想到是这里配错了。
	if locale := strings.TrimSpace(input.ReceiptLocale); locale != "" && !receiptLocaleSupported(locale) {
		return nil, apperrors.New(40030, http.StatusBadRequest, "不支持的凭证语言："+locale)
	}
	// 币种同样当场校验：它会原样印在凭证上，存一个 "人民币" 进去
	// 只会在几个月后的某份凭证上出现「人民币 128.00」这种谁也认不出的写法。
	input.WalletCurrency = strings.ToUpper(strings.TrimSpace(input.WalletCurrency))
	if input.WalletCurrency == "" {
		input.WalletCurrency = appdomain.DefaultWalletCurrency
	}
	if !isCurrencyCode(input.WalletCurrency) {
		return nil, apperrors.New(40031, http.StatusBadRequest, "钱包币种必须是 3 位 ISO 4217 代码，如 CNY / USD")
	}
	app, err := s.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	settings := cloneSettingsMap(app.Settings)
	settings["integralPerCurrency"] = input.IntegralPerCurrency
	settings["receiptEmailOnPaid"] = input.ReceiptEmailOnPaid
	input.ReceiptLocale = strings.TrimSpace(input.ReceiptLocale)
	settings["receiptLocale"] = input.ReceiptLocale
	settings["walletCurrency"] = input.WalletCurrency
	if _, err := s.SaveApp(ctx, appdomain.AppMutation{ID: appID, Settings: settings}); err != nil {
		return nil, err
	}
	return &input, nil
}

func (s *AppService) ResolveTransportEncryption(app *appdomain.App) appdomain.TransportEncryptionPolicy {
	policy := appdomain.TransportEncryptionPolicy{
		Strict:             true,
		ResponseEncryption: true,
	}
	if app == nil || app.Settings == nil {
		return policy
	}

	rawConfig := lookupMap(app.Settings, "transportEncryption")
	if value, ok := lookupNestedBool(rawConfig, "enabled"); ok {
		policy.Enabled = value
	} else if value, ok := lookupBool(app.Settings, "transportEncryptionEnabled"); ok {
		policy.Enabled = value
	}
	if value, ok := lookupNestedBool(rawConfig, "strict"); ok {
		policy.Strict = value
	}
	if value, ok := lookupNestedBool(rawConfig, "responseEncryption"); ok {
		policy.ResponseEncryption = value
	} else if value, ok := lookupNestedBool(rawConfig, "responseEncrypt"); ok {
		policy.ResponseEncryption = value
	}
	if secret, ok := lookupNestedString(rawConfig, "secret"); ok {
		policy.Secret = secret
	} else if secret, ok := lookupNestedString(rawConfig, "key"); ok {
		policy.Secret = secret
	} else if secret, ok := lookupNestedString(rawConfig, "passphrase"); ok {
		policy.Secret = secret
	}
	return policy
}

// GetTransportEncryption 获取应用传输加密配置（不含私钥）
func (s *AppService) GetTransportEncryption(ctx context.Context, appID int64) (*appdomain.TransportEncryptionView, error) {
	app, err := s.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	if app == nil {
		return nil, apperrors.New(40410, http.StatusNotFound, "应用不存在")
	}
	policy := s.ResolveTransportEncryption(app)
	secretHint := ""
	if len(policy.Secret) > 0 {
		if len(policy.Secret) > 8 {
			secretHint = policy.Secret[:4] + "****" + policy.Secret[len(policy.Secret)-4:]
		} else {
			secretHint = "****"
		}
	}

	// 读取允许的算法列表
	rawConfig := lookupMap(app.Settings, "transportEncryption")
	var allowedAlgorithms []string
	if rawAllowed, ok := rawConfig["allowedAlgorithms"]; ok {
		if list, ok := rawAllowed.([]any); ok {
			for _, item := range list {
				if s, ok := item.(string); ok {
					allowedAlgorithms = append(allowedAlgorithms, s)
				}
			}
		}
	}
	if len(allowedAlgorithms) == 0 {
		allowedAlgorithms = []string{"XChaCha20Poly1305", "AES-256-GCM"}
	}

	// 读取密钥对状态
	rsaPub, _ := rawConfig["rsaPublicKey"].(string)
	rsaPriv, _ := rawConfig["rsaPrivateKey"].(string)
	ecdhPub, _ := rawConfig["ecdhPublicKey"].(string)
	ecdhPriv, _ := rawConfig["ecdhPrivateKey"].(string)

	return &appdomain.TransportEncryptionView{
		Enabled:             policy.Enabled,
		Strict:              policy.Strict,
		ResponseEncryption:  policy.ResponseEncryption,
		HasSecret:           len(policy.Secret) > 0,
		SecretHint:          secretHint,
		AllowedAlgorithms:   allowedAlgorithms,
		SupportedAlgorithms: []string{"XChaCha20Poly1305", "AES-256-GCM", "hybrid-rsa-xchacha20", "hybrid-rsa-aes256gcm", "hybrid-ecdh-xchacha20", "hybrid-ecdh-aes256gcm"},
		HasRSAKey:           len(rsaPub) > 0 && len(rsaPriv) > 0,
		RSAPublicKey:        rsaPub,
		HasECDHKey:          len(ecdhPub) > 0 && len(ecdhPriv) > 0,
		ECDHPublicKey:       ecdhPub,
	}, nil
}

// UpdateTransportEncryption 更新应用传输加密配置（支持密钥对生成）
func (s *AppService) UpdateTransportEncryption(ctx context.Context, appID int64, update appdomain.TransportEncryptionUpdate) (*appdomain.TransportEncryptionView, error) {
	app, err := s.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	if app == nil {
		return nil, apperrors.New(40410, http.StatusNotFound, "应用不存在")
	}
	settings := cloneSettingsMap(app.Settings)
	transport := lookupMap(settings, "transportEncryption")
	if transport == nil {
		transport = map[string]any{}
	}
	if update.Enabled != nil {
		transport["enabled"] = *update.Enabled
	}
	if update.Strict != nil {
		transport["strict"] = *update.Strict
	}
	if update.ResponseEncryption != nil {
		transport["responseEncryption"] = *update.ResponseEncryption
	}
	if update.Secret != nil {
		transport["secret"] = strings.TrimSpace(*update.Secret)
	}
	if len(update.AllowedAlgorithms) > 0 {
		transport["allowedAlgorithms"] = update.AllowedAlgorithms
	}

	// 生成 RSA 密钥对
	if update.GenerateRSAKey {
		pubPEM, privPEM, err := generateRSAKeyPair()
		if err != nil {
			return nil, fmt.Errorf("生成 RSA 密钥对失败: %w", err)
		}
		transport["rsaPublicKey"] = pubPEM
		transport["rsaPrivateKey"] = privPEM
	}

	// 生成 ECDH 密钥对
	if update.GenerateECDHKey {
		pubPEM, privPEM, err := generateECDHKeyPair()
		if err != nil {
			return nil, fmt.Errorf("生成 ECDH 密钥对失败: %w", err)
		}
		transport["ecdhPublicKey"] = pubPEM
		transport["ecdhPrivateKey"] = privPEM
	}

	settings["transportEncryption"] = transport
	mutation := appdomain.AppMutation{ID: appID, Settings: settings}
	if _, err := s.SaveApp(ctx, mutation); err != nil {
		return nil, err
	}
	return s.GetTransportEncryption(ctx, appID)
}

func (s *AppService) PublicSettings(app *appdomain.App) map[string]any {
	if app == nil || app.Settings == nil {
		return map[string]any{}
	}
	settings := appSettingsDeepCloneMap(app.Settings)
	transport := lookupMap(settings, "transportEncryption")
	if len(transport) > 0 {
		delete(transport, "secret")
		delete(transport, "key")
		delete(transport, "passphrase")
		settings["transportEncryption"] = transport
	}
	delete(settings, "transportEncryptionSecret")
	delete(settings, "transportEncryptionKey")
	return settings
}

func (s *AppService) EnsureLoginAllowed(ctx context.Context, appID int64) (*appdomain.App, error) {
	app, err := s.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	// 平台治理先判：它的结论应用管理员改不动，放在应用自治开关之前拒绝，
	// 也避免"把 status 打开就能绕过冻结"。
	if s.governance != nil {
		if err := s.governance.EnsureCapability(appID, platformdomain.CapabilityLogin); err != nil {
			return nil, err
		}
	}
	if !app.Status {
		message := app.DisabledReason
		if message == "" {
			message = "应用已被禁用"
		}
		return nil, apperrors.New(40310, http.StatusForbidden, message)
	}
	if !app.LoginStatus {
		message := app.DisabledLoginReason
		if message == "" {
			message = "当前应用暂时关闭登录"
		}
		return nil, apperrors.New(40311, http.StatusForbidden, message)
	}
	return app, nil
}

func (s *AppService) EnsureRegisterAllowed(ctx context.Context, appID int64) (*appdomain.App, error) {
	app, err := s.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	if s.governance != nil {
		if err := s.governance.EnsureCapability(appID, platformdomain.CapabilityRegister); err != nil {
			return nil, err
		}
	}
	if !app.Status {
		message := app.DisabledReason
		if message == "" {
			message = "应用已被禁用"
		}
		return nil, apperrors.New(40310, http.StatusForbidden, message)
	}
	if !app.RegisterStatus {
		message := app.DisabledRegisterReason
		if message == "" {
			message = "当前应用暂时关闭注册"
		}
		return nil, apperrors.New(40312, http.StatusForbidden, message)
	}
	return app, nil
}

func (s *AppService) ListApps(ctx context.Context) ([]appdomain.App, error) {
	return s.pg.ListApps(ctx)
}

func (s *AppService) GetStats(ctx context.Context, appID int64) (*appdomain.Stats, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	return s.pg.GetAppStats(ctx, appID)
}

func (s *AppService) GetPolicy(ctx context.Context, appID int64) (*appdomain.Policy, error) {
	app, err := s.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	policy := s.ResolvePolicy(app)
	return &policy, nil
}

func (s *AppService) GetUserTrend(ctx context.Context, appID int64, days int) (*appdomain.UserTrend, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	if days <= 0 {
		days = 7
	}
	if days > 365 {
		days = 365
	}
	return s.pg.GetAppUserTrend(ctx, appID, days)
}

func (s *AppService) GetRegionStats(ctx context.Context, appID int64, query appdomain.RegionStatsQuery) (*appdomain.RegionStatsResult, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	if query.Limit <= 0 {
		query.Limit = 20
	}
	if query.Limit > 256 {
		query.Limit = 256
	}
	if strings.TrimSpace(query.Type) == "" {
		query.Type = "province"
	}
	return s.pg.GetAppRegionStats(ctx, appID, query)
}

func (s *AppService) GetAuthSourceStats(ctx context.Context, appID int64) (*appdomain.AuthSourceStats, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	return s.pg.GetAppAuthSourceStats(ctx, appID)
}

func (s *AppService) ListLoginAudits(ctx context.Context, appID int64, query appdomain.LoginAuditQuery) (*appdomain.LoginAuditListResult, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	items, total, err := s.pg.ListLoginAuditsByApp(ctx, appID, appdomain.LoginAuditQuery{
		Keyword: query.Keyword,
		Status:  query.Status,
		Page:    page,
		Limit:   limit,
	})
	if err != nil {
		return nil, err
	}
	return &appdomain.LoginAuditListResult{
		Items:      items,
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: calcPagesForService(total, limit),
	}, nil
}

func (s *AppService) ExportLoginAudits(ctx context.Context, appID int64, query appdomain.LoginAuditExportQuery) ([]appdomain.LoginAuditItem, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 5000
	}
	if limit > 20000 {
		limit = 20000
	}
	return s.pg.ListLoginAuditsByAppForExport(ctx, appID, appdomain.LoginAuditExportQuery{
		Keyword: query.Keyword,
		Status:  query.Status,
		Limit:   limit,
	})
}

func (s *AppService) ListSessionAudits(ctx context.Context, appID int64, query appdomain.SessionAuditQuery) (*appdomain.SessionAuditListResult, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	items, total, err := s.pg.ListSessionAuditsByApp(ctx, appID, appdomain.SessionAuditQuery{
		Keyword:   query.Keyword,
		EventType: query.EventType,
		Page:      page,
		Limit:     limit,
	})
	if err != nil {
		return nil, err
	}
	return &appdomain.SessionAuditListResult{
		Items:      items,
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: calcPagesForService(total, limit),
	}, nil
}

func (s *AppService) ExportSessionAudits(ctx context.Context, appID int64, query appdomain.SessionAuditExportQuery) ([]appdomain.SessionAuditItem, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 5000
	}
	if limit > 20000 {
		limit = 20000
	}
	return s.pg.ListSessionAuditsByAppForExport(ctx, appID, appdomain.SessionAuditExportQuery{
		Keyword:   query.Keyword,
		EventType: query.EventType,
		Limit:     limit,
	})
}

func (s *AppService) SaveApp(ctx context.Context, mutation appdomain.AppMutation) (*appdomain.App, error) {
	var item appdomain.App

	if mutation.ID > 0 {
		// 更新已有应用
		current, err := s.pg.GetAppByID(ctx, mutation.ID)
		if err != nil {
			return nil, err
		}
		if current == nil {
			return nil, apperrors.New(40404, http.StatusNotFound, "应用不存在")
		}
		item = *current
		if item.Settings == nil {
			item.Settings = map[string]any{}
		}
	} else {
		// 新建：id 和 appKey 由数据库自动生成
		item = appdomain.App{
			Status:         true,
			RegisterStatus: true,
			LoginStatus:    true,
			Settings:       map[string]any{},
		}
	}

	if mutation.Name != nil {
		item.Name = strings.TrimSpace(*mutation.Name)
	}
	if strings.TrimSpace(item.Name) == "" {
		return nil, apperrors.New(40021, http.StatusBadRequest, "应用名称不能为空")
	}
	// AppKey 不可更改，创建时由数据库自动生成
	if mutation.Status != nil {
		item.Status = *mutation.Status
	}
	if mutation.DisabledReason != nil {
		item.DisabledReason = strings.TrimSpace(*mutation.DisabledReason)
	}
	if mutation.RegisterStatus != nil {
		item.RegisterStatus = *mutation.RegisterStatus
	}
	if mutation.DisabledRegisterReason != nil {
		item.DisabledRegisterReason = strings.TrimSpace(*mutation.DisabledRegisterReason)
	}
	if mutation.LoginStatus != nil {
		item.LoginStatus = *mutation.LoginStatus
	}
	if mutation.DisabledLoginReason != nil {
		item.DisabledLoginReason = strings.TrimSpace(*mutation.DisabledLoginReason)
	}
	if mutation.Settings != nil {
		item.Settings = mutation.Settings
	}

	saved, err := s.pg.UpsertApp(ctx, item)
	if err != nil {
		return nil, err
	}
	s.invalidateAppCache(ctx, saved.ID)
	if s.plugin != nil {
		if mutation.ID == 0 {
			go s.plugin.ExecuteHook(context.Background(), HookAppCreated, map[string]any{
				"appId": saved.ID,
				"name":  saved.Name,
			}, plugindomain.HookMetadata{AppID: &saved.ID})
		} else {
			go s.plugin.ExecuteHook(context.Background(), HookAppUpdated, map[string]any{
				"appId": saved.ID,
			}, plugindomain.HookMetadata{AppID: &saved.ID})
		}
	}
	return saved, nil
}

// GetAppByKey 通过 appKey 查询应用
func (s *AppService) GetAppByKey(ctx context.Context, appKey string) (*appdomain.App, error) {
	return s.pg.GetAppByKey(ctx, appKey)
}

// DeleteApp 删除应用及其所有关联数据
func (s *AppService) DeleteApp(ctx context.Context, appID int64) error {
	app, err := s.GetApp(ctx, appID)
	if err != nil {
		return err
	}
	if app == nil {
		return apperrors.New(40410, http.StatusNotFound, "应用不存在")
	}
	// 先删除该应用下的所有用户（users 表无 CASCADE）
	if _, err := s.pg.DeleteUsersByApp(ctx, appID); err != nil {
		return fmt.Errorf("删除应用用户失败: %w", err)
	}
	// 删除应用（banners/notices/sites 等通过 CASCADE 自动清理）
	if err := s.pg.DeleteApp(ctx, appID); err != nil {
		return fmt.Errorf("删除应用失败: %w", err)
	}
	s.invalidateAppCache(ctx, appID)
	s.log.Warn("应用已删除", zap.Int64("appid", appID), zap.String("name", app.Name))
	if s.plugin != nil {
		go s.plugin.ExecuteHook(context.Background(), HookAppDeleted, map[string]any{
			"appId": appID,
		}, plugindomain.HookMetadata{AppID: &appID})
	}
	return nil
}

func (s *AppService) UpdatePolicy(ctx context.Context, appID int64, policy appdomain.Policy) (*appdomain.Policy, error) {
	app, err := s.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	policy.DeviceRebindInterval = max(0, policy.DeviceRebindInterval)
	if policy.MultiDeviceLogin {
		policy.MultiDeviceLimit = max(1, policy.MultiDeviceLimit)
	}
	settings := cloneSettingsMap(app.Settings)
	settings["loginCheckDevice"] = policy.LoginCheckDevice
	settings["loginCheckUser"] = policy.LoginCheckUser
	settings["loginCheckIp"] = policy.LoginCheckIP
	settings["loginCheckDeviceTimeOut"] = policy.DeviceRebindInterval
	settings["multiDeviceLogin"] = policy.MultiDeviceLogin
	settings["multiDeviceLoginNum"] = policy.MultiDeviceLimit
	settings["registerCheckIp"] = policy.RegisterCheckIP
	// 注册验证码已归口到应用验证码配置（captcha.requireForRegister）。
	// 残留键会被 ResolvePolicy 忽略，但留在库里会让人以为还能从这里配 —— 一并清掉。
	delete(settings, "registerCaptcha")
	delete(settings, "registerCaptchaTimeOut")

	if _, err := s.SaveApp(ctx, appdomain.AppMutation{
		ID:       appID,
		Settings: settings,
	}); err != nil {
		return nil, err
	}
	updated := s.ResolvePolicy(&appdomain.App{Settings: settings})
	return &updated, nil
}

func (s *AppService) invalidateAppCache(ctx context.Context, appID int64) {
	if s.sessions == nil {
		return
	}
	if err := s.sessions.DeleteAppByID(ctx, appID); err != nil {
		s.log.Warn("delete app cache failed", zap.Int64("appid", appID), zap.Error(err))
	}
}

func lookupBool(settings map[string]any, key string) (bool, bool) {
	if settings == nil {
		return false, false
	}
	value, ok := settings[key]
	if !ok || value == nil {
		return false, false
	}
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		switch strings.TrimSpace(strings.ToLower(typed)) {
		case "1", "true", "yes", "on":
			return true, true
		case "0", "false", "no", "off":
			return false, true
		}
	case float64:
		return typed != 0, true
	case int:
		return typed != 0, true
	case int64:
		return typed != 0, true
	}
	return false, false
}

func boolSetting(settings map[string]any, key string) bool {
	value, _ := lookupBool(settings, key)
	return value
}

func intSetting(settings map[string]any, key string) int {
	value, _ := lookupInt(settings, key)
	return value
}

// lookupInt 与 intSetting 的区别是第二个返回值区分「键不存在」与「显式为 0」。
// 对 maxAge（0 = 永不过期）、preventReuse（0 = 不限制）这类 0 有语义的字段，
// 必须用它才能正确区分「没配过，用默认值」和「配成了 0」。
func lookupInt(settings map[string]any, key string) (int, bool) {
	if settings == nil {
		return 0, false
	}
	value, ok := settings[key]
	if !ok || value == nil {
		return 0, false
	}
	switch typed := value.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return 0, false
		}
		var parsed int
		if _, err := fmt.Sscanf(trimmed, "%d", &parsed); err != nil {
			return 0, false
		}
		return parsed, true
	}
	return 0, false
}

func calcPagesForService(total int64, limit int) int {
	if limit <= 0 {
		return 1
	}
	pages := int((total + int64(limit) - 1) / int64(limit))
	if pages == 0 {
		return 1
	}
	return pages
}

func cloneSettingsMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func appSettingsDeepCloneMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = appSettingsDeepCloneValue(value)
	}
	return result
}

func appSettingsDeepCloneSlice(input []any) []any {
	if input == nil {
		return nil
	}
	result := make([]any, len(input))
	for index, value := range input {
		result[index] = appSettingsDeepCloneValue(value)
	}
	return result
}

func appSettingsDeepCloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return appSettingsDeepCloneMap(typed)
	case []any:
		return appSettingsDeepCloneSlice(typed)
	default:
		return typed
	}
}

// ── 应用级验证码配置 ──

// GetCaptchaConfig 获取应用验证码配置
func (s *AppService) GetCaptchaConfig(ctx context.Context, appID int64) (*captchadomain.CaptchaAppConfig, error) {
	app, err := s.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	if app == nil {
		return nil, apperrors.New(40410, http.StatusNotFound, "应用不存在")
	}
	cfg := captchadomain.DefaultCaptchaAppConfig()
	raw := lookupMap(app.Settings, "captcha")
	if raw != nil {
		// 用 JSON 序列化/反序列化来合并配置
		jsonBytes, _ := gojson.Marshal(raw)
		_ = gojson.Unmarshal(jsonBytes, &cfg)
	}
	return &cfg, nil
}

// UpdateCaptchaConfig 更新应用验证码配置
func (s *AppService) UpdateCaptchaConfig(ctx context.Context, appID int64, cfg captchadomain.CaptchaAppConfig) (*captchadomain.CaptchaAppConfig, error) {
	app, err := s.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	if app == nil {
		return nil, apperrors.New(40410, http.StatusNotFound, "应用不存在")
	}
	if app.Settings == nil {
		app.Settings = map[string]any{}
	}
	// 序列化配置到 map[string]any
	jsonBytes, err := gojson.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var cfgMap map[string]any
	if err := gojson.Unmarshal(jsonBytes, &cfgMap); err != nil {
		return nil, err
	}
	app.Settings["captcha"] = cfgMap
	// 通过 SaveApp 持久化
	_, err = s.pg.UpdateAppSettings(ctx, appID, app.Settings)
	if err != nil {
		return nil, err
	}
	// 关键：清除 Redis 中的 app 缓存。
	// GetApp 会把 app 缓存 5 分钟，如果这里不主动失效，下次 GetCaptchaConfig 仍会
	// 读到旧的 captcha 配置，新开关的变更会被覆盖，前端表现为"保存无效"。
	s.invalidateAppCache(ctx, appID)
	return &cfg, nil
}

func lookupMap(settings map[string]any, key string) map[string]any {
	if settings == nil {
		return nil
	}
	value, ok := settings[key]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case map[string]string:
		result := make(map[string]any, len(typed))
		for nestedKey, nestedValue := range typed {
			result[nestedKey] = nestedValue
		}
		return result
	}
	return nil
}

func lookupNestedBool(settings map[string]any, key string) (bool, bool) {
	if settings == nil {
		return false, false
	}
	value, ok := settings[key]
	if !ok || value == nil {
		return false, false
	}
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		switch strings.TrimSpace(strings.ToLower(typed)) {
		case "1", "true", "yes", "on":
			return true, true
		case "0", "false", "no", "off":
			return false, true
		}
	case float64:
		return typed != 0, true
	case int:
		return typed != 0, true
	case int64:
		return typed != 0, true
	}
	return false, false
}

func lookupNestedString(settings map[string]any, key string) (string, bool) {
	if settings == nil {
		return "", false
	}
	value, ok := settings[key]
	if !ok || value == nil {
		return "", false
	}
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return "", false
		}
		return trimmed, true
	case fmt.Stringer:
		trimmed := strings.TrimSpace(typed.String())
		if trimmed == "" {
			return "", false
		}
		return trimmed, true
	}
	return "", false
}

func normalizeUniqueIDs(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	result := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

// isCurrencyCode ISO 4217 的形状：恰好 3 个大写拉丁字母。
//
// 刻意只校验形状而不比对一张币种全表：ISO 4217 每年都在增删（新币启用、
// 旧币退役），维护一张会过期的白名单只会让某天上线的合法币种被拒。
// 形状校验已经挡住了「人民币」「￥」「CNY 元」这类真正会印坏凭证的输入。
func isCurrencyCode(code string) bool {
	if len(code) != 3 {
		return false
	}
	for _, r := range code {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

// receiptLocaleSupported 凭证渲染器是否内置了该语言。
// 直接问 pkg/receipt，而不是在这里再维护一份语言清单 ——
// 新增语言时只加目录文件即可，两处各一份早晚会漂移。
func receiptLocaleSupported(tag string) bool {
	bundle, err := receipt.Bundle()
	if err != nil {
		// 目录装不起来时不拦配置：真正的问题会在渲染时以明确的错误暴露
		return true
	}
	tag = strings.TrimSpace(tag)
	for _, info := range bundle.Locales() {
		if strings.EqualFold(info.Tag, tag) {
			return true
		}
	}
	return false
}
