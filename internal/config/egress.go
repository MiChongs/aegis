package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"aegis/pkg/egress"
	"github.com/spf13/viper"
)

// loadEgressConfig 从环境变量装配出海网关配置。
//
// 三种写法优先级：EGRESS_CONFIG_FILE（全量 JSON）> EGRESS_CONFIG（内联 JSON）>
// EGRESS_ENDPOINTS + EGRESS_RULES（紧凑 DSL）。
// 前两者是「字段全集」，DSL 覆盖日常 90% 的需求：一行写清哪些域名走哪条线。
//
// 解析失败直接返回错误让进程起不来，而不是降级成「没有出海」——
// 一个写错的规则如果被静默忽略，境外调用会变成难以归因的超时。
func loadEgressConfig(v *viper.Viper) (egress.Config, error) {
	if path := strings.TrimSpace(v.GetString("EGRESS_CONFIG_FILE")); path != "" {
		cfg, err := loadEgressConfigFile(path)
		if err != nil {
			return egress.Config{}, err
		}
		cfg.Source = "file:" + filepath.Base(path)
		return applyEgressEnvOverrides(v, cfg), nil
	}
	if inline := strings.TrimSpace(v.GetString("EGRESS_CONFIG")); inline != "" {
		cfg, err := egress.ParseConfigJSON([]byte(inline))
		if err != nil {
			return egress.Config{}, fmt.Errorf("EGRESS_CONFIG: %w", err)
		}
		cfg.Source = "env"
		return applyEgressEnvOverrides(v, cfg), nil
	}

	endpoints, err := egress.ParseEndpoints(v.GetString("EGRESS_ENDPOINTS"))
	if err != nil {
		return egress.Config{}, fmt.Errorf("EGRESS_ENDPOINTS: %w", err)
	}
	rules, err := egress.ParseRules(v.GetString("EGRESS_RULES"))
	if err != nil {
		return egress.Config{}, fmt.Errorf("EGRESS_RULES: %w", err)
	}
	cfg := egress.Config{Endpoints: endpoints, Rules: rules, Source: "env"}
	return applyEgressEnvOverrides(v, cfg), nil
}

func loadEgressConfigFile(path string) (egress.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return egress.Config{}, fmt.Errorf("读取 EGRESS_CONFIG_FILE %s 失败: %w", path, err)
	}
	cfg, err := egress.ParseConfigJSON(data)
	if err != nil {
		return egress.Config{}, fmt.Errorf("EGRESS_CONFIG_FILE %s: %w", path, err)
	}
	return cfg, nil
}

// applyEgressEnvOverrides 让单项环境变量可以覆盖 JSON 里的对应字段，
// 便于「一份基线配置文件 + 各环境少量差异」的部署方式。
func applyEgressEnvOverrides(v *viper.Viper, cfg egress.Config) egress.Config {
	cfg.Enabled = getBool(v, "EGRESS_ENABLED", cfg.Enabled)
	if raw := strings.TrimSpace(v.GetString("EGRESS_DEFAULT_ACTION")); raw != "" {
		cfg.DefaultAction = egress.Action(raw)
	}
	if raw := csvList(v.GetString("EGRESS_DEFAULT_ENDPOINTS")); len(raw) > 0 {
		cfg.DefaultEndpoints = raw
	}
	if raw := strings.TrimSpace(v.GetString("EGRESS_DEFAULT_STRATEGY")); raw != "" {
		cfg.DefaultStrategy = egress.Strategy(raw)
	}

	cfg.DialTimeoutMS = egressMillis(v, "EGRESS_DIAL_TIMEOUT", cfg.DialTimeoutMS)
	cfg.TLSHandshakeTimeoutMS = egressMillis(v, "EGRESS_TLS_HANDSHAKE_TIMEOUT", cfg.TLSHandshakeTimeoutMS)
	cfg.ResponseHeaderTimeoutMS = egressMillis(v, "EGRESS_RESPONSE_HEADER_TIMEOUT", cfg.ResponseHeaderTimeoutMS)
	cfg.IdleConnTimeoutMS = egressMillis(v, "EGRESS_IDLE_CONN_TIMEOUT", cfg.IdleConnTimeoutMS)
	if v.IsSet("EGRESS_MAX_IDLE_CONNS_PER_HOST") {
		cfg.MaxIdleConnsPerHost = v.GetInt("EGRESS_MAX_IDLE_CONNS_PER_HOST")
	}

	cfg.Health.Enabled = getBool(v, "EGRESS_HEALTH_ENABLED", true)
	cfg.Health.PassiveEnabled = getBool(v, "EGRESS_HEALTH_PASSIVE", true)
	cfg.Health.IntervalSeconds = egressSeconds(v, "EGRESS_HEALTH_INTERVAL", cfg.Health.IntervalSeconds)
	cfg.Health.TimeoutSeconds = egressSeconds(v, "EGRESS_HEALTH_TIMEOUT", cfg.Health.TimeoutSeconds)
	cfg.Health.CooldownSeconds = egressSeconds(v, "EGRESS_HEALTH_COOLDOWN", cfg.Health.CooldownSeconds)
	if v.IsSet("EGRESS_HEALTH_FAILURE_THRESHOLD") {
		cfg.Health.FailureThreshold = v.GetInt("EGRESS_HEALTH_FAILURE_THRESHOLD")
	}
	if v.IsSet("EGRESS_HEALTH_SUCCESS_THRESHOLD") {
		cfg.Health.SuccessThreshold = v.GetInt("EGRESS_HEALTH_SUCCESS_THRESHOLD")
	}
	if raw := strings.TrimSpace(v.GetString("EGRESS_HEALTH_PROBE_URL")); raw != "" {
		cfg.Health.ProbeURL = raw
	}
	if v.IsSet("EGRESS_HEALTH_ALLOW_UNHEALTHY") {
		allow := v.GetBool("EGRESS_HEALTH_ALLOW_UNHEALTHY")
		cfg.Health.AllowUnhealthy = &allow
	}
	return cfg
}

func egressMillis(v *viper.Viper, key string, fallback int) int {
	if !v.IsSet(key) {
		return fallback
	}
	d := v.GetDuration(key)
	if d <= 0 {
		return fallback
	}
	return int(d / time.Millisecond)
}

func egressSeconds(v *viper.Viper, key string, fallback int) int {
	if !v.IsSet(key) {
		return fallback
	}
	d := v.GetDuration(key)
	if d <= 0 {
		return fallback
	}
	return int(d / time.Second)
}
