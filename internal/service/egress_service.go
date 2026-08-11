package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	systemdomain "aegis/internal/domain/system"
	pgrepo "aegis/internal/repository/postgres"
	"aegis/pkg/egress"
	apperrors "aegis/pkg/errors"
	"go.uber.org/zap"
)

// egressSettingKey 出海网关配置在 platform_settings 里的键。
const egressSettingKey = "platform.egress"

// EgressService 出海代理网关的管理面。
//
// 职责边界：路由与拨号全在 pkg/egress 里，这里只负责三件平台侧的事——
// 配置持久化、密钥加解密、以及「数据库覆盖 .env」的优先级。
type EgressService struct {
	log *zap.Logger
	pg  *pgrepo.Repository
	gw  *egress.Gateway
	key []byte

	mu sync.RWMutex
	// envConfig 是 .env / 配置文件给出的基线，重置时回到它。
	envConfig egress.Config
	// dbOverride 表示当前生效的是数据库里的配置。
	// 只有它为 false 时，.env 热重载才会真正改变运行时行为——
	// 否则管理员在控制台改的配置会被一次 .env 保存悄悄冲掉。
	dbOverride bool
	updatedBy  *int64
	updatedAt  *time.Time
}

// NewEgressService 构造管理面。gw 为 nil 时所有管理端接口返回 503。
func NewEgressService(log *zap.Logger, pg *pgrepo.Repository, gw *egress.Gateway, masterKey string, envConfig egress.Config) *EgressService {
	if log == nil {
		log = zap.NewNop()
	}
	return &EgressService{
		log:       log,
		pg:        pg,
		gw:        gw,
		key:       securityKeyMaterial(masterKey),
		envConfig: envConfig.Normalize(),
	}
}

// Initialize 启动时从数据库载入覆盖配置。
//
// 载入失败只告警不中断启动：出海配置坏掉应该表现为「境外调用走不通」，
// 而不是整个平台起不来。
func (s *EgressService) Initialize(ctx context.Context) error {
	if s == nil || s.gw == nil || s.pg == nil {
		return nil
	}
	record, err := s.pg.GetPlatformSetting(ctx, egressSettingKey)
	if err != nil {
		return err
	}
	if record == nil || len(record.Value) == 0 {
		s.log.Info("出海网关沿用 .env 配置（数据库无覆盖）")
		return nil
	}
	cfg, err := s.decodeStored(record.Value)
	if err != nil {
		s.log.Error("解析持久化的出海网关配置失败，沿用 .env 配置", zap.Error(err))
		return nil
	}
	cfg.Source = "database"
	if err := s.gw.Reload(cfg); err != nil {
		s.log.Error("加载持久化的出海网关配置失败，沿用 .env 配置", zap.Error(err))
		return nil
	}
	s.mu.Lock()
	s.dbOverride = true
	s.updatedBy = record.UpdatedBy
	updatedAt := record.UpdatedAt
	s.updatedAt = &updatedAt
	s.mu.Unlock()
	return nil
}

// ApplyEnvConfig 在 .env 热重载时调用。
// 数据库已有覆盖时只更新基线，不改变当前生效配置。
func (s *EgressService) ApplyEnvConfig(cfg egress.Config) {
	if s == nil {
		return
	}
	normalized := cfg.Normalize()
	s.mu.Lock()
	s.envConfig = normalized
	override := s.dbOverride
	s.mu.Unlock()
	if override || s.gw == nil {
		return
	}
	if err := s.gw.Reload(normalized); err != nil {
		s.log.Warn("出海网关热重载失败，沿用旧配置", zap.Error(err))
	}
}

// GetSettings 返回脱敏后的配置 + 运行态。
func (s *EgressService) GetSettings(_ context.Context) (systemdomain.EgressSettingsView, error) {
	if s == nil || s.gw == nil {
		return systemdomain.EgressSettingsView{}, apperrors.New(50301, http.StatusServiceUnavailable, "出海网关未启用")
	}
	return s.view(), nil
}

// UpdateSettings 整份替换配置：校验 → 持久化 → 热生效。
//
// 顺序刻意是「先校验、再落库、最后生效」：
// 落库成功但生效失败会导致重启后行为突变，因此生效失败要回滚这次写入。
func (s *EgressService) UpdateSettings(ctx context.Context, actorID *int64, update systemdomain.EgressSettingsUpdate) (systemdomain.EgressSettingsView, error) {
	if s == nil || s.gw == nil {
		return systemdomain.EgressSettingsView{}, apperrors.New(50301, http.StatusServiceUnavailable, "出海网关未启用")
	}
	if s.pg == nil {
		return systemdomain.EgressSettingsView{}, apperrors.New(50302, http.StatusServiceUnavailable, "出海网关配置存储不可用")
	}

	next, err := s.materialize(update)
	if err != nil {
		return systemdomain.EgressSettingsView{}, err
	}
	next = next.Normalize()
	if err := s.gw.ValidateConfig(next); err != nil {
		return systemdomain.EgressSettingsView{}, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}

	previous := s.gw.Config()
	previousOverride, previousRecord := s.overrideState()

	payload, err := s.encodeStored(next)
	if err != nil {
		return systemdomain.EgressSettingsView{}, apperrors.New(50001, http.StatusInternalServerError, "加密出海网关密钥失败")
	}
	saved, err := s.pg.UpsertPlatformSetting(ctx, systemdomain.SettingRecord{
		Key: egressSettingKey, Value: payload, UpdatedBy: actorID,
	})
	if err != nil {
		return systemdomain.EgressSettingsView{}, err
	}

	next.Source = "database"
	if err := s.gw.Reload(next); err != nil {
		// 已落库但没生效：立刻把旧配置写回去，避免下次重启加载到一份跑不起来的配置。
		if rollback, encodeErr := s.encodeStored(previous); encodeErr == nil {
			if _, restoreErr := s.pg.UpsertPlatformSetting(ctx, systemdomain.SettingRecord{
				Key: egressSettingKey, Value: rollback, UpdatedBy: actorID,
			}); restoreErr != nil {
				s.log.Error("出海网关配置回滚失败", zap.Error(restoreErr))
			}
		}
		s.restoreOverrideState(previousOverride, previousRecord)
		return systemdomain.EgressSettingsView{}, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}

	s.mu.Lock()
	s.dbOverride = true
	s.updatedBy = saved.UpdatedBy
	updatedAt := saved.UpdatedAt
	s.updatedAt = &updatedAt
	s.mu.Unlock()

	s.log.Info("出海网关配置已更新",
		zap.Bool("enabled", next.Enabled),
		zap.Int("endpoints", len(next.Endpoints)),
		zap.Int("rules", len(next.Rules)),
	)
	return s.view(), nil
}

// ResetToEnv 丢弃数据库覆盖，回到 .env 基线。
func (s *EgressService) ResetToEnv(ctx context.Context, actorID *int64) (systemdomain.EgressSettingsView, error) {
	if s == nil || s.gw == nil {
		return systemdomain.EgressSettingsView{}, apperrors.New(50301, http.StatusServiceUnavailable, "出海网关未启用")
	}
	s.mu.RLock()
	baseline := s.envConfig.Clone()
	s.mu.RUnlock()

	if err := s.gw.Reload(baseline); err != nil {
		return systemdomain.EgressSettingsView{}, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	if s.pg != nil {
		// 写入 null 值而不是删行：platform_settings 没有删除接口，
		// 空值在 Initialize 里被当作「无覆盖」处理，语义一致。
		if _, err := s.pg.UpsertPlatformSetting(ctx, systemdomain.SettingRecord{
			Key: egressSettingKey, Value: json.RawMessage("null"), UpdatedBy: actorID,
		}); err != nil {
			return systemdomain.EgressSettingsView{}, err
		}
	}
	s.mu.Lock()
	s.dbOverride = false
	s.updatedBy = actorID
	now := time.Now()
	s.updatedAt = &now
	s.mu.Unlock()
	return s.view(), nil
}

// Test 实跑一次出站请求。
func (s *EgressService) Test(ctx context.Context, req egress.TestRequest) (egress.TestResult, error) {
	if s == nil || s.gw == nil {
		return egress.TestResult{}, apperrors.New(50301, http.StatusServiceUnavailable, "出海网关未启用")
	}
	return s.gw.Test(ctx, req), nil
}

// Explain 解释某个目标会走哪条线路。
func (s *EgressService) Explain(req systemdomain.EgressExplainRequest) (egress.Explanation, error) {
	if s == nil || s.gw == nil {
		return egress.Explanation{}, apperrors.New(50301, http.StatusServiceUnavailable, "出海网关未启用")
	}
	host := strings.TrimSpace(req.Host)
	if host == "" {
		return egress.Explanation{}, apperrors.New(40000, http.StatusBadRequest, "host 不能为空")
	}
	port := req.Port
	if port <= 0 {
		port = 443
	}
	scheme := strings.TrimSpace(req.Scheme)
	if scheme == "" {
		scheme = "https"
	}
	return s.gw.Explain(egress.Target{Host: host, Port: port, Scheme: scheme, Profile: req.Profile}), nil
}

// Probe 立即探测全部端点。
func (s *EgressService) Probe(ctx context.Context) ([]egress.ProbeResult, error) {
	if s == nil || s.gw == nil {
		return nil, apperrors.New(50301, http.StatusServiceUnavailable, "出海网关未启用")
	}
	return s.gw.ProbeAll(ctx), nil
}

// Stats 运行态快照，供 MonitorService 聚合。
func (s *EgressService) Stats() egress.Stats {
	if s == nil || s.gw == nil {
		return egress.Stats{}
	}
	return s.gw.Stats()
}

// --- 平台运行时组件约定（与 firewall / security 一致） ---

// CurrentConfig 当前生效配置。
func (s *EgressService) CurrentConfig() egress.Config {
	if s == nil || s.gw == nil {
		return egress.Config{}
	}
	return s.gw.Config()
}

// ValidateConfig 只校验不生效。
func (s *EgressService) ValidateConfig(cfg egress.Config) error {
	if s == nil || s.gw == nil {
		return apperrors.New(50301, http.StatusServiceUnavailable, "出海网关未启用")
	}
	return s.gw.ValidateConfig(cfg)
}

// Reload 直接热替换配置（不落库）。
func (s *EgressService) Reload(cfg egress.Config) error {
	if s == nil || s.gw == nil {
		return apperrors.New(50301, http.StatusServiceUnavailable, "出海网关未启用")
	}
	return s.gw.Reload(cfg)
}

// ReloadMeta 配置版本与加载时间。
func (s *EgressService) ReloadMeta() (uint64, time.Time) {
	if s == nil || s.gw == nil {
		return 0, time.Time{}
	}
	return s.gw.ReloadMeta()
}

// --- 内部实现 ---

func (s *EgressService) overrideState() (bool, *int64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dbOverride, s.updatedBy
}

func (s *EgressService) restoreOverrideState(override bool, updatedBy *int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dbOverride = override
	s.updatedBy = updatedBy
}

func (s *EgressService) view() systemdomain.EgressSettingsView {
	cfg := s.gw.Config()
	version, reloadedAt := s.gw.ReloadMeta()

	s.mu.RLock()
	updatedBy, updatedAt := s.updatedBy, s.updatedAt
	s.mu.RUnlock()

	endpoints := make([]systemdomain.EgressEndpointView, 0, len(cfg.Endpoints))
	for _, item := range cfg.Endpoints {
		endpoints = append(endpoints, maskEgressEndpoint(item))
	}

	protocols := egress.RegisteredProtocols()
	protocolNames := make([]string, 0, len(protocols))
	for _, item := range protocols {
		protocolNames = append(protocolNames, string(item))
	}

	return systemdomain.EgressSettingsView{
		Enabled:                 cfg.Enabled,
		DefaultAction:           string(cfg.DefaultAction),
		DefaultEndpoints:        cfg.DefaultEndpoints,
		DefaultStrategy:         string(cfg.DefaultStrategy),
		DialTimeoutMs:           cfg.DialTimeoutMS,
		TLSHandshakeTimeoutMs:   cfg.TLSHandshakeTimeoutMS,
		ResponseHeaderTimeoutMs: cfg.ResponseHeaderTimeoutMS,
		IdleConnTimeoutMs:       cfg.IdleConnTimeoutMS,
		MaxIdleConnsPerHost:     cfg.MaxIdleConnsPerHost,
		Health:                  cfg.Health,
		Endpoints:               endpoints,
		Rules:                   cfg.Rules,
		Source:                  cfg.Source,
		ReloadVersion:           version,
		ReloadedAt:              reloadedAt,
		UpdatedBy:               updatedBy,
		UpdatedAt:               updatedAt,
		Runtime:                 s.gw.Stats(),
		Catalog: systemdomain.EgressCatalog{
			Protocols: protocolNames,
			Actions:   []string{string(egress.ActionProxy), string(egress.ActionDirect), string(egress.ActionReject)},
			Strategies: []string{
				string(egress.StrategyFailover), string(egress.StrategyRoundRobin),
				string(egress.StrategyRandom), string(egress.StrategyWeighted), string(egress.StrategyLatency),
			},
			ShadowsocksMethods:  egress.SupportedShadowsocksMethods(),
			DefaultProbeURL:     egress.DefaultProbeURL,
			SecretPlaceholderCN: "留空表示保持原密钥不变",
		},
	}
}

// maskEgressEndpoint 抹掉所有密钥字段，只保留「是否已配置」。
func maskEgressEndpoint(item egress.EndpointConfig) systemdomain.EgressEndpointView {
	view := systemdomain.EgressEndpointView{
		PasswordSet:   item.Password != "" || item.SSH.Password != "" || item.Shadowsocks.Password != "",
		PrivateKeySet: strings.TrimSpace(item.SSH.PrivateKeyPEM) != "" || strings.TrimSpace(item.TLS.ClientKeyPEM) != "",
	}
	item.Password = ""
	item.SSH.Password = ""
	item.SSH.PrivateKeyPEM = ""
	item.SSH.Passphrase = ""
	item.Shadowsocks.Password = ""
	item.TLS.ClientKeyPEM = ""
	view.EndpointConfig = item
	return view
}

// materialize 把更新载荷合并成完整配置，补回被前端省略的密钥。
func (s *EgressService) materialize(update systemdomain.EgressSettingsUpdate) (egress.Config, error) {
	current := s.gw.Config()
	existing := make(map[string]egress.EndpointConfig, len(current.Endpoints))
	for _, item := range current.Endpoints {
		existing[item.Name] = item
	}

	endpoints := make([]egress.EndpointConfig, 0, len(update.Endpoints))
	for _, item := range update.Endpoints {
		endpoint := item.EndpointConfig
		if !item.ClearSecrets {
			if previous, ok := existing[strings.TrimSpace(endpoint.Name)]; ok {
				endpoint = inheritEgressSecrets(endpoint, previous)
			}
		}
		endpoints = append(endpoints, endpoint)
	}

	return egress.Config{
		Enabled:                 update.Enabled,
		DefaultAction:           egress.Action(update.DefaultAction),
		DefaultEndpoints:        update.DefaultEndpoints,
		DefaultStrategy:         egress.Strategy(update.DefaultStrategy),
		DialTimeoutMS:           update.DialTimeoutMs,
		TLSHandshakeTimeoutMS:   update.TLSHandshakeTimeoutMs,
		ResponseHeaderTimeoutMS: update.ResponseHeaderTimeoutMs,
		IdleConnTimeoutMS:       update.IdleConnTimeoutMs,
		MaxIdleConnsPerHost:     update.MaxIdleConnsPerHost,
		Health:                  update.Health,
		Endpoints:               endpoints,
		Rules:                   update.Rules,
		Source:                  "database",
	}, nil
}

// inheritEgressSecrets 空密钥字段继承旧值 —— 编辑表单不回填密钥，
// 否则每次改个备注都要把 SSH 私钥重新贴一遍。
func inheritEgressSecrets(next, previous egress.EndpointConfig) egress.EndpointConfig {
	if next.Password == "" {
		next.Password = previous.Password
	}
	if next.SSH.Password == "" {
		next.SSH.Password = previous.SSH.Password
	}
	if strings.TrimSpace(next.SSH.PrivateKeyPEM) == "" {
		next.SSH.PrivateKeyPEM = previous.SSH.PrivateKeyPEM
	}
	if next.SSH.Passphrase == "" {
		next.SSH.Passphrase = previous.SSH.Passphrase
	}
	if next.Shadowsocks.Password == "" {
		next.Shadowsocks.Password = previous.Shadowsocks.Password
	}
	if strings.TrimSpace(next.TLS.ClientKeyPEM) == "" {
		next.TLS.ClientKeyPEM = previous.TLS.ClientKeyPEM
	}
	return next
}

// egressSecretFields 需要加密落库的字段。
// 用取址函数集中登记，新增密钥字段时只改这一处，加解密两侧自动对齐。
func egressSecretFields(item *egress.EndpointConfig) []*string {
	return []*string{
		&item.Password,
		&item.SSH.Password,
		&item.SSH.PrivateKeyPEM,
		&item.SSH.Passphrase,
		&item.Shadowsocks.Password,
		&item.TLS.ClientKeyPEM,
	}
}

func (s *EgressService) encodeStored(cfg egress.Config) (json.RawMessage, error) {
	stored := cfg.Clone()
	for i := range stored.Endpoints {
		for _, field := range egressSecretFields(&stored.Endpoints[i]) {
			if *field == "" {
				continue
			}
			cipherText, err := encryptSecret(s.key, *field)
			if err != nil {
				return nil, err
			}
			*field = cipherText
		}
	}
	payload, err := json.Marshal(stored)
	if err != nil {
		return nil, err
	}
	return payload, nil
}

func (s *EgressService) decodeStored(raw json.RawMessage) (egress.Config, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return egress.Config{}, fmt.Errorf("配置为空")
	}
	cfg, err := egress.ParseConfigJSON(raw)
	if err != nil {
		return egress.Config{}, err
	}
	for i := range cfg.Endpoints {
		for _, field := range egressSecretFields(&cfg.Endpoints[i]) {
			if *field == "" {
				continue
			}
			plaintext, err := decryptSecret(s.key, *field)
			if err != nil {
				// 兼容升级期可能存在的明文（与 AppOAuthService.revealSecret 同策略）。
				continue
			}
			*field = plaintext
		}
	}
	return cfg, nil
}
