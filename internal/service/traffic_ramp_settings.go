package service

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"aegis/internal/config"
	systemdomain "aegis/internal/domain/system"
	apperrors "aegis/pkg/errors"

	"go.uber.org/zap"
)

// 突发流量爬坡的热配置面。与防火墙同一套生命周期：
// 环境变量给基线 → 数据库覆盖 → 管理端 PUT 即改即生效（Validate → 落库 → Reload）。

const trafficRampSettingKey = "security.traffic_ramp"

// trafficRampRuntime 与 firewallRuntime 同构的热重载契约，外加运行态统计。
type trafficRampRuntime interface {
	CurrentConfig() config.TrafficRampConfig
	ReloadMeta() (uint64, time.Time)
	ValidateConfig(config.TrafficRampConfig) error
	Reload(config.TrafficRampConfig) error
	StatsView(seconds int) systemdomain.TrafficRampStats
	ResetStats()
}

// SetTrafficRamp 注入爬坡运行时（bootstrap 装配期调用一次）。
// 走 setter 而不是改构造函数：与 SetPluginService 同理，避免牵动全部调用点。
func (s *PlatformSettingsService) SetTrafficRamp(rt trafficRampRuntime) {
	s.trafficRamp = rt
}

// initializeTrafficRamp 启动时把数据库里持久化的配置热加载进运行时。
// 由 Initialize 调用；读不到或解不开时保持环境变量基线，只告警不拦启动。
func (s *PlatformSettingsService) initializeTrafficRamp(ctx context.Context) error {
	if s.trafficRamp == nil {
		return nil
	}
	record, err := s.pg.GetPlatformSetting(ctx, trafficRampSettingKey)
	if err != nil {
		return err
	}
	if record == nil || len(record.Value) == 0 {
		return nil
	}
	cfg, err := s.decodeTrafficRampConfig(record.Value)
	if err != nil {
		s.log.Error("decode persisted traffic ramp settings failed", zap.Error(err))
		return nil
	}
	if err := s.trafficRamp.Reload(cfg); err != nil {
		s.log.Error("reload persisted traffic ramp settings failed", zap.Error(err))
	}
	return nil
}

// GetTrafficRampSettings 当前生效配置 + 热重载元信息。
func (s *PlatformSettingsService) GetTrafficRampSettings(ctx context.Context) (*systemdomain.TrafficRampSettingsView, error) {
	if s.trafficRamp == nil {
		return &systemdomain.TrafficRampSettingsView{Source: "unavailable"}, nil
	}
	record, err := s.pg.GetPlatformSetting(ctx, trafficRampSettingKey)
	if err != nil {
		return nil, err
	}
	view := s.buildTrafficRampView(record)
	return &view, nil
}

// UpdateTrafficRampSettings 逐字段 patch → 归一化 → 校验 → 落库 → 热重载。
// 与防火墙同一条戒律：先落库再 Reload，重载失败时库里已是新值，
// 下次启动会再试一次；反过来先 Reload 再落库，进程重启后配置会静默回退。
func (s *PlatformSettingsService) UpdateTrafficRampSettings(ctx context.Context, adminID *int64, patch systemdomain.TrafficRampSettingsPatch) (*systemdomain.TrafficRampSettingsView, error) {
	if s.trafficRamp == nil {
		return nil, apperrors.New(50096, http.StatusInternalServerError, "流量爬坡模块未初始化")
	}
	current := s.trafficRamp.CurrentConfig()
	next := applyTrafficRampPatch(current, patch)
	next = config.NormalizeTrafficRampConfig(next)
	if err := s.trafficRamp.ValidateConfig(next); err != nil {
		return nil, apperrors.New(40092, http.StatusBadRequest, "流量爬坡配置无效")
	}
	payload, err := json.Marshal(next)
	if err != nil {
		return nil, err
	}
	record, err := s.pg.UpsertPlatformSetting(ctx, systemdomain.SettingRecord{Key: trafficRampSettingKey, Value: payload, UpdatedBy: adminID})
	if err != nil {
		return nil, err
	}
	if err := s.trafficRamp.Reload(next); err != nil {
		return nil, apperrors.New(50092, http.StatusInternalServerError, "流量爬坡热重载失败")
	}
	view := s.buildTrafficRampView(record)
	return &view, nil
}

// TrafficRampStats 运行态统计（内存读取，不打库）。
func (s *PlatformSettingsService) TrafficRampStats(seconds int) (*systemdomain.TrafficRampStats, error) {
	if s.trafficRamp == nil {
		return nil, apperrors.New(50096, http.StatusInternalServerError, "流量爬坡模块未初始化")
	}
	stats := s.trafficRamp.StatsView(seconds)
	return &stats, nil
}

// ResetTrafficRampStats 清零累计统计（不影响配置与正在进行的爬坡）。
func (s *PlatformSettingsService) ResetTrafficRampStats() error {
	if s.trafficRamp == nil {
		return apperrors.New(50096, http.StatusInternalServerError, "流量爬坡模块未初始化")
	}
	s.trafficRamp.ResetStats()
	return nil
}

func (s *PlatformSettingsService) decodeTrafficRampConfig(payload []byte) (config.TrafficRampConfig, error) {
	cfg := config.NormalizeTrafficRampConfig(s.cfg.TrafficRamp)
	if len(payload) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(payload, &cfg); err != nil {
		return config.TrafficRampConfig{}, err
	}
	return config.NormalizeTrafficRampConfig(cfg), nil
}

func (s *PlatformSettingsService) buildTrafficRampView(record *systemdomain.SettingRecord) systemdomain.TrafficRampSettingsView {
	current := s.trafficRamp.CurrentConfig()
	reloadVersion, reloadedAt := s.trafficRamp.ReloadMeta()
	view := systemdomain.TrafficRampSettingsView{
		Enabled:            current.Enabled,
		BaselineRPS:        current.BaselineRPS,
		MaxRPS:             current.MaxRPS,
		RampStepPct:        current.RampStepPct,
		RampIntervalMs:     current.RampIntervalMs,
		CooldownSeconds:    current.CooldownSeconds,
		QueueSize:          current.QueueSize,
		QueueTimeoutMs:     current.QueueTimeoutMs,
		MaxConcurrent:      current.MaxConcurrent,
		ExemptPathPrefixes: cloneStrings(current.ExemptPathPrefixes),
		ExemptAdmin:        current.ExemptAdmin,
		RetryAfterSeconds:  current.RetryAfterSeconds,
		Source:             "environment",
		ReloadVersion:      reloadVersion,
		ReloadedAt:         reloadedAt,
	}
	if record != nil {
		view.Source = "database"
		view.UpdatedBy = record.UpdatedBy
		view.UpdatedAt = &record.UpdatedAt
	}
	return view
}

func applyTrafficRampPatch(current config.TrafficRampConfig, patch systemdomain.TrafficRampSettingsPatch) config.TrafficRampConfig {
	if patch.Enabled != nil {
		current.Enabled = *patch.Enabled
	}
	if patch.BaselineRPS != nil {
		current.BaselineRPS = *patch.BaselineRPS
	}
	if patch.MaxRPS != nil {
		current.MaxRPS = *patch.MaxRPS
	}
	if patch.RampStepPct != nil {
		current.RampStepPct = *patch.RampStepPct
	}
	if patch.RampIntervalMs != nil {
		current.RampIntervalMs = *patch.RampIntervalMs
	}
	if patch.CooldownSeconds != nil {
		current.CooldownSeconds = *patch.CooldownSeconds
	}
	if patch.QueueSize != nil {
		current.QueueSize = *patch.QueueSize
	}
	if patch.QueueTimeoutMs != nil {
		current.QueueTimeoutMs = *patch.QueueTimeoutMs
	}
	if patch.MaxConcurrent != nil {
		current.MaxConcurrent = *patch.MaxConcurrent
	}
	if patch.ExemptPathPrefixes != nil {
		current.ExemptPathPrefixes = compactStrings(*patch.ExemptPathPrefixes)
	}
	if patch.ExemptAdmin != nil {
		current.ExemptAdmin = *patch.ExemptAdmin
	}
	if patch.RetryAfterSeconds != nil {
		current.RetryAfterSeconds = *patch.RetryAfterSeconds
	}
	return current
}
