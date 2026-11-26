package firewall

import (
	"time"

	"aegis/internal/config"
)

// Snapshot 防火墙运行态（设置页与总览页显示）。放在引擎包里，service 与 middleware 都能引用而不成环。
type Snapshot struct {
	Config         config.FirewallConfig
	ReloadVersion  uint64
	ReloadedAt     time.Time
	RuntimeEnabled bool
	WAFActive      bool
	CustomRules    int
	RuleErrors     []string
	Exclusions     int
	Catalog        *Catalog
	Directives     string
	Counters       Counters
}

// Counters 本副本自启动以来的计数（多副本时各自独立，控制台按副本显示）。
type Counters struct {
	Inspected    int64     `json:"inspected"`
	EdgeBlocked  int64     `json:"edgeBlocked"`
	WAFBlocked   int64     `json:"wafBlocked"`
	Observed     int64     `json:"observed"`
	Decrypted    int64     `json:"decrypted"`
	WAFErrors    int64     `json:"wafErrors"`
	AvgWAFMicros int64     `json:"avgWafMicros"`
	StartedAt    time.Time `json:"startedAt"`
}
