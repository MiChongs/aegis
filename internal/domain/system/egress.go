package system

import (
	"time"

	"aegis/pkg/egress"
)

// 出海代理网关的管理端视图与更新载荷。
//
// 端点与规则天生是「整份路由表」，不是一堆互不相干的开关，
// 因此更新采用整份替换而不是逐字段 patch —— 部分更新在列表型配置上
// 既难表达也难审计。

// EgressSettingsView 管理端读到的完整配置 + 运行态。
type EgressSettingsView struct {
	Enabled                 bool                 `json:"enabled"`
	DefaultAction           string               `json:"defaultAction"`
	DefaultEndpoints        []string             `json:"defaultEndpoints"`
	DefaultStrategy         string               `json:"defaultStrategy"`
	DialTimeoutMs           int                  `json:"dialTimeoutMs"`
	TLSHandshakeTimeoutMs   int                  `json:"tlsHandshakeTimeoutMs"`
	ResponseHeaderTimeoutMs int                  `json:"responseHeaderTimeoutMs"`
	IdleConnTimeoutMs       int                  `json:"idleConnTimeoutMs"`
	MaxIdleConnsPerHost     int                  `json:"maxIdleConnsPerHost"`
	Health                  egress.HealthConfig  `json:"health"`
	Endpoints               []EgressEndpointView `json:"endpoints"`
	Rules                   []egress.RuleConfig  `json:"rules"`

	Source        string     `json:"source"`
	ReloadVersion uint64     `json:"reloadVersion"`
	ReloadedAt    time.Time  `json:"reloadedAt"`
	UpdatedBy     *int64     `json:"updatedBy,omitempty"`
	UpdatedAt     *time.Time `json:"updatedAt,omitempty"`

	// Runtime 端点健康与流量统计，让「配置」和「现在到底通不通」在同一屏里。
	Runtime egress.Stats `json:"runtime"`
	// Catalog 可选项目录，驱动前端下拉框，避免前端硬编码一份枚举。
	Catalog EgressCatalog `json:"catalog"`
}

// EgressEndpointView 端点视图：密钥一律不出网，只回传「是否已配置」。
type EgressEndpointView struct {
	egress.EndpointConfig
	Password      string `json:"password,omitempty"` // 恒为空，覆盖内嵌字段防止密钥外泄
	PasswordSet   bool   `json:"passwordSet"`
	PrivateKeySet bool   `json:"privateKeySet"`
}

// EgressCatalog 协议 / 策略 / 动作等可选值清单。
type EgressCatalog struct {
	Protocols           []string `json:"protocols"`
	Actions             []string `json:"actions"`
	Strategies          []string `json:"strategies"`
	ShadowsocksMethods  []string `json:"shadowsocksMethods"`
	DefaultProbeURL     string   `json:"defaultProbeUrl"`
	SecretPlaceholderCN string   `json:"secretPlaceholder"`
}

// EgressSettingsUpdate 整份替换的更新载荷。
//
// 密钥字段留空表示「保持原值」（编辑表单不回填密钥）；
// 要真正清空请把该端点的 clearSecrets 置为 true。
type EgressSettingsUpdate struct {
	Enabled                 bool                   `json:"enabled"`
	DefaultAction           string                 `json:"defaultAction"`
	DefaultEndpoints        []string               `json:"defaultEndpoints"`
	DefaultStrategy         string                 `json:"defaultStrategy"`
	DialTimeoutMs           int                    `json:"dialTimeoutMs"`
	TLSHandshakeTimeoutMs   int                    `json:"tlsHandshakeTimeoutMs"`
	ResponseHeaderTimeoutMs int                    `json:"responseHeaderTimeoutMs"`
	IdleConnTimeoutMs       int                    `json:"idleConnTimeoutMs"`
	MaxIdleConnsPerHost     int                    `json:"maxIdleConnsPerHost"`
	Health                  egress.HealthConfig    `json:"health"`
	Endpoints               []EgressEndpointUpdate `json:"endpoints"`
	Rules                   []egress.RuleConfig    `json:"rules"`
}

// EgressEndpointUpdate 端点更新项。
type EgressEndpointUpdate struct {
	egress.EndpointConfig
	// ClearSecrets 显式清空该端点的所有密钥（口令 / SSH 私钥 / 客户端证书私钥）。
	ClearSecrets bool `json:"clearSecrets,omitempty"`
}

// EgressExplainRequest 「这个域名会怎么出去」的查询入参。
type EgressExplainRequest struct {
	Host    string `json:"host"`
	Port    int    `json:"port,omitempty"`
	Scheme  string `json:"scheme,omitempty"`
	Profile string `json:"profile,omitempty"`
}
