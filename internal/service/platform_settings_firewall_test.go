package service

import (
	"encoding/json"
	"strings"
	"testing"

	"aegis/internal/config"
)

// 库里 v1 时代存下的防火墙设置没有 CorazaRulesetVersion 字段：解码时不能被「已是 v2」的环境基线顶掉，
// 否则旧默认值（阈值 40、17 条全局移除）永远迁不过来（2026-11-26 上线后实测踩到）。
func TestDecodeFirewallConfigMigratesPersistedV1(t *testing.T) {
	s := &PlatformSettingsService{cfg: config.Config{Firewall: config.FirewallConfig{Enabled: true, CorazaEnabled: true}}}
	v1 := map[string]any{
		"Enabled": true, "CorazaEnabled": true, "CorazaAnomalyThreshold": 40,
		"CorazaRemovedRules": []string{"920350", "920300", "920420", "920230", "920440", "913100", "921110",
			"932200", "932230", "932235", "932236", "932240", "932250", "942100", "942430", "942440", "941100"},
	}
	payload, _ := json.Marshal(v1)
	cfg, err := s.decodeFirewallConfig(payload)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CorazaAnomalyThreshold != 5 || strings.Join(cfg.CorazaRemovedRules, ",") != strings.Join(config.DefaultCorazaRemovedRules, ",") {
		t.Fatalf("v1 默认值应迁到 v2：threshold=%d removed=%v", cfg.CorazaAnomalyThreshold, cfg.CorazaRemovedRules)
	}

	// 迁移后再存回去（带版本号）的记录不能被二次迁移
	v2, _ := json.Marshal(config.FirewallConfig{Enabled: true, CorazaEnabled: true, CorazaAnomalyThreshold: 40, CorazaRulesetVersion: 2})
	cfg, err = s.decodeFirewallConfig(v2)
	if err != nil || cfg.CorazaAnomalyThreshold != 40 {
		t.Fatalf("v2 记录里手工设的 40 应保留：%d %v", cfg.CorazaAnomalyThreshold, err)
	}
}
