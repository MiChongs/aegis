// Package traffic 流量管理的持久化视图类型。引擎的配置与运行态类型在 pkg/traffic。
package traffic

import (
	"time"

	enginepkg "aegis/pkg/traffic"
)

// ConfigView 当前生效配置 + 最后修改信息。
type ConfigView struct {
	enginepkg.Config
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
	UpdatedBy *int64     `json:"updatedBy,omitempty"`
}

// 修订动作。
const (
	ActionSeed        = "seed"
	ActionImport      = "import"
	ActionSettings    = "settings"
	ActionRuleCreate  = "rule.create"
	ActionRuleUpdate  = "rule.update"
	ActionRuleDelete  = "rule.delete"
	ActionRuleReorder = "rule.reorder"
	ActionRuleToggle  = "rule.toggle"
	ActionRollback    = "rollback"
)

// RevisionSummary 修订列表项。
type RevisionSummary struct {
	Revision  int64     `json:"revision"`
	Action    string    `json:"action"`
	Note      string    `json:"note"`
	RuleCount int       `json:"ruleCount"`
	Enabled   bool      `json:"enabled"`
	CreatedBy *int64    `json:"createdBy,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// RevisionDetail 修订详情（含完整快照）。
type RevisionDetail struct {
	RevisionSummary
	Config enginepkg.Config `json:"config"`
}

// HistoryPoint 历史曲线的一格（集群合计）。
type HistoryPoint struct {
	Ts          int64   `json:"ts"`
	Arrivals    int64   `json:"arrivals"`
	Admitted    int64   `json:"admitted"`
	Queued      int64   `json:"queued"`
	Rejected    int64   `json:"rejected"`
	Observed    int64   `json:"observed"`
	P50         float64 `json:"p50"`
	P99         float64 `json:"p99"`
	PeakRPS     int64   `json:"peakRps"`
	MaxInflight int64   `json:"maxInflight"`
}

// History 一段时间的历史曲线。
type History struct {
	Range       string           `json:"range"`
	StepSeconds int              `json:"stepSeconds"`
	RuleID      string           `json:"ruleId,omitempty"`
	Points      []HistoryPoint   `json:"points"`
	Totals      enginepkg.Counts `json:"totals"`
}

// EventRecord 落库后的事件。
type EventRecord struct {
	ID       int64          `json:"id"`
	At       time.Time      `json:"at"`
	Instance string         `json:"instance"`
	Kind     string         `json:"kind"`
	RuleID   string         `json:"ruleId,omitempty"`
	RuleName string         `json:"ruleName,omitempty"`
	Detail   map[string]any `json:"detail,omitempty"`
}

// MinuteBatch 一个实例一次落库的分钟样本。
type MinuteBatch struct {
	Instance string
	Samples  []enginepkg.MinuteSample
}
