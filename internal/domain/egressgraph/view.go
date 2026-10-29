package egressgraph

import (
	"time"

	"aegis/pkg/egress"
	"aegis/pkg/egress/mihomo"
)

// 管理端读写画布的视图与载荷。

// CompileView 编译结果里给控制台看的部分。
type CompileView struct {
	Issues      []Issue           `json:"issues"`
	Summary     Summary           `json:"summary"`
	Bindings    map[string]string `json:"bindings"`
	RuleNodes   map[string]string `json:"ruleNodes"`
	MihomoNames map[string]string `json:"mihomoNames"`
	Chain       []string          `json:"chain"`
	Targets     []string          `json:"targets"`
}

// ViewOf 从编译结果取出控制台视图。
func ViewOf(result Result) CompileView {
	return CompileView{
		Issues:      result.Issues,
		Summary:     result.Summary,
		Bindings:    result.Bindings,
		RuleNodes:   result.RuleNodes,
		MihomoNames: result.MihomoNames,
		Chain:       result.Chain,
		Targets:     result.Targets,
	}
}

// Catalog 画布目录：节点、端口、网关协议与 mihomo 表单。
type Catalog struct {
	Nodes              []NodeSpec     `json:"nodes"`
	Categories         []Category     `json:"categories"`
	Protocols          []string       `json:"protocols"`
	Actions            []string       `json:"actions"`
	Strategies         []string       `json:"strategies"`
	ShadowsocksMethods []string       `json:"shadowsocksMethods"`
	DefaultProbeURL    string         `json:"defaultProbeUrl"`
	SecretKept         string         `json:"secretKept"`
	DefaultGateway     GatewayData    `json:"defaultGateway"`
	DefaultCore        CoreData       `json:"defaultCore"`
	Mihomo             mihomo.Catalog `json:"mihomo"`
	// Profiles 平台里已知的调用方标识，规则的「调用方」条件从这里选。
	Profiles []ProfileOption `json:"profiles"`
}

// ProfileOption 一个调用方标识。
type ProfileOption struct {
	Name  string `json:"name"`
	Label string `json:"label"`
}

// KnownProfiles 平台代码里 egress.NewClient 用到的调用方（"x.*" 是前缀通配）。
var KnownProfiles = []ProfileOption{
	{Name: "auth.*", Label: "全部登录相关"},
	{Name: "auth.oauth", Label: "第三方登录 OAuth"},
	{Name: "auth.oidc", Label: "OIDC 登录"},
	{Name: "auth.saml_metadata", Label: "SAML 元数据"},
	{Name: "auth.provider_health", Label: "登录渠道健康检查"},
	{Name: "oauth.probe", Label: "OAuth 配置探测"},
	{Name: "payment.*", Label: "全部支付"},
	{Name: "payment.stripe", Label: "Stripe"},
	{Name: "payment.paypal", Label: "PayPal"},
	{Name: "payment.gateway", Label: "其它支付网关"},
	{Name: "email.*", Label: "全部邮件"},
	{Name: "email.resend", Label: "Resend"},
	{Name: "email.ses", Label: "Amazon SES"},
	{Name: "email.zeabur", Label: "Zeabur 邮件"},
	{Name: "storage.outbound", Label: "对象存储与云盘"},
	{Name: "notify.webhook", Label: "Webhook 通知"},
	{Name: "location.lookup", Label: "IP 归属地查询"},
	{Name: "risk.ipqs", Label: "IPQS 风险评分"},
	{Name: "captcha.*", Label: "验证码外部服务"},
	{Name: "ai.*", Label: "全部 AI"},
	{Name: "ai.mcp", Label: "AI MCP 工具"},
	{Name: "ai.catalog.*", Label: "AI 模型目录同步"},
	{Name: "egress.subscription", Label: "订阅下载"},
	{Name: "egress.mihomo.install", Label: "核心下载"},
}

// BuildCatalog 目录；mirror 是 GitHub 镜像前缀，用于新核心节点的 Geo 数据库地址。
func BuildCatalog(mirror string) Catalog {
	protocols := []string{}
	for _, item := range egress.RegisteredProtocols() {
		if item != mihomo.Protocol {
			protocols = append(protocols, string(item))
		}
	}
	return Catalog{
		Nodes:      NodeSpecs(),
		Categories: Categories(),
		Protocols:  protocols,
		Actions:    []string{string(egress.ActionProxy), string(egress.ActionDirect), string(egress.ActionReject)},
		Strategies: []string{
			string(egress.StrategyFailover), string(egress.StrategyRoundRobin), string(egress.StrategyRandom),
			string(egress.StrategyWeighted), string(egress.StrategyLatency),
		},
		ShadowsocksMethods: egress.SupportedShadowsocksMethods(),
		DefaultProbeURL:    egress.DefaultProbeURL,
		SecretKept:         SecretKept,
		DefaultGateway:     DefaultGateway(),
		DefaultCore:        CoreData{Settings: DefaultCoreSettings(mirror)},
		Mihomo:             mihomo.BuildCatalog(),
		Profiles:           KnownProfiles,
	}
}

// View 管理端读到的画布。
type View struct {
	Graph  Graph  `json:"graph"`
	Origin string `json:"origin"` // draft / applied / config（由当前配置生成）
	// Dirty 草稿与已应用的不同。
	Dirty          bool       `json:"dirty"`
	DraftRevision  int64      `json:"draftRevision"`
	DraftUpdatedAt *time.Time `json:"draftUpdatedAt,omitempty"`
	DraftUpdatedBy *int64     `json:"draftUpdatedBy,omitempty"`
	AppliedVersion int64      `json:"appliedVersion"`
	AppliedAt      *time.Time `json:"appliedAt,omitempty"`
	AppliedBy      *int64     `json:"appliedBy,omitempty"`
	// Active 当前生效的配置是否来自画布（否则来自 .env 或旧版控制台）。
	Active  bool          `json:"active"`
	Compile CompileView   `json:"compile"`
	Catalog Catalog       `json:"catalog"`
	Core    mihomo.Status `json:"core"`
	Runtime egress.Stats  `json:"runtime"`
}

// HistoryEntry 一次应用的记录（回滚用）。
type HistoryEntry struct {
	Version   int64     `json:"version"`
	AppliedAt time.Time `json:"appliedAt"`
	AppliedBy *int64    `json:"appliedBy,omitempty"`
	Note      string    `json:"note,omitempty"`
	Summary   Summary   `json:"summary"`
	Graph     *Graph    `json:"graph,omitempty"`
}

// SaveRequest 保存草稿。
type SaveRequest struct {
	Graph    Graph `json:"graph"`
	Revision int64 `json:"revision"`
}

// ApplyRequest 应用画布（同时保存为草稿）。
type ApplyRequest struct {
	Graph    Graph  `json:"graph"`
	Revision int64  `json:"revision"`
	Note     string `json:"note,omitempty"`
}

// ValidateResult 校验结果，附带渲染后的核心配置（密钥已打码）。
type ValidateResult struct {
	Compile CompileView  `json:"compile"`
	YAML    string       `json:"yaml,omitempty"`
	Routes  RoutePreview `json:"routes"`
}

// RoutePreview 编译出的网关路由表（密钥已打码），用来核对画布到底生成了什么。
type RoutePreview struct {
	Enabled          bool                `json:"enabled"`
	DefaultAction    string              `json:"defaultAction"`
	DefaultEndpoints []string            `json:"defaultEndpoints"`
	Rules            []egress.RuleConfig `json:"rules"`
	Endpoints        []RouteEndpoint     `json:"endpoints"`
}

// RouteEndpoint 路由表里的一条线路。
type RouteEndpoint struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Address  string `json:"address,omitempty"`
	Via      string `json:"via,omitempty"`
	Target   string `json:"target,omitempty"`
}

// PreviewOf 路由表预览。
func PreviewOf(cfg egress.Config) RoutePreview {
	out := RoutePreview{
		Enabled:          cfg.Enabled,
		DefaultAction:    string(cfg.DefaultAction),
		DefaultEndpoints: cfg.DefaultEndpoints,
		Rules:            cfg.Rules,
	}
	for _, endpoint := range cfg.Endpoints {
		out.Endpoints = append(out.Endpoints, RouteEndpoint{
			Name: endpoint.Name, Protocol: string(endpoint.Protocol), Address: endpoint.Address,
			Via: endpoint.Via, Target: endpoint.Options[mihomo.OptionTarget],
		})
	}
	return out
}

// ImportRequest 导入 mihomo / Clash 配置或分享链接。
type ImportRequest struct {
	Graph   Graph         `json:"graph"`
	Content string        `json:"content,omitempty"`
	URL     string        `json:"url,omitempty"`
	Options ImportOptions `json:"options"`
}

// ImportResult 导入后的新画布。
type ImportResult struct {
	Graph   Graph       `json:"graph"`
	Format  string      `json:"format"`
	Notes   []string    `json:"notes"`
	Added   Summary     `json:"added"`
	Compile CompileView `json:"compile"`
}

// ExportRequest 导出核心配置。
type ExportRequest struct {
	Graph          Graph `json:"graph"`
	IncludeSecrets bool  `json:"includeSecrets"`
}
