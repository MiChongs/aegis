// Package egressgraph 出海网关的画布：一张图同时描述网关的路由表与 mihomo 核心的配置。
//
// 画布与物品发放、等级体系同形（itemgrant.Graph），节点位置与连线只服务于编辑，
// 真正生效的是 Compile 的产物：
//
//	网关设置 ──规则链──▶ 规则 ──▶ 规则 ──▶ …（自上而下依次匹配，都没命中走「未命中」）
//	   │                 │
//	   └──未命中──▶       └──命中──▶ 网关线路 / mihomo 策略组 / mihomo 节点 / 交给 mihomo 分流
//
//	网关线路 ──经由──▶ 网关线路（代理链）
//	策略组 ──成员──▶ 节点 / 策略组 / 订阅
//	节点、订阅 ──前置──▶ 节点 / 策略组（dialer-proxy）
//
// 规则引用的 mihomo 目标被编译成 "mihomo" 协议的网关端点，经本机 SOCKS5 入站交给核心。
package egressgraph

import (
	"encoding/json"

	"aegis/internal/domain/itemgrant"
	"aegis/pkg/egress"
)

// Graph 与 Issue 沿用物品发放的画布格式，控制台的画布往返代码因此可以共用。
type (
	Graph    = itemgrant.Graph
	Node     = itemgrant.Node
	Edge     = itemgrant.Edge
	Position = itemgrant.Position
	Issue    = itemgrant.Issue
)

// 节点类型。
const (
	NodeGateway  = "gateway"
	NodeRule     = "rule"
	NodeEndpoint = "endpoint"
	NodeProxy    = "m_proxy"
	NodeGroup    = "m_group"
	NodeProvider = "m_provider"
	NodeRules    = "m_rules"
	NodeCore     = "m_core"
)

// 输出端口。
const (
	PortRules    = "rules"    // 网关设置 → 第一条规则
	PortFallback = "fallback" // 网关设置 → 未命中时的线路
	PortNext     = "next"     // 规则 → 下一条规则
	PortHit      = "hit"      // 规则 → 命中后的线路
	PortVia      = "via"      // 网关线路 → 上一跳网关线路
	PortMembers  = "members"  // 策略组 → 成员
	PortDialer   = "dialer"   // 节点 / 订阅 → 前置代理
)

// GatewayData 网关设置节点：全局开关、未命中时的动作、超时与健康检查。
type GatewayData struct {
	Enabled                 bool                `json:"enabled"`
	DefaultAction           egress.Action       `json:"defaultAction,omitempty"`
	DefaultStrategy         egress.Strategy     `json:"defaultStrategy,omitempty"`
	DialTimeoutMS           int                 `json:"dialTimeoutMs,omitempty"`
	TLSHandshakeTimeoutMS   int                 `json:"tlsHandshakeTimeoutMs,omitempty"`
	ResponseHeaderTimeoutMS int                 `json:"responseHeaderTimeoutMs,omitempty"`
	IdleConnTimeoutMS       int                 `json:"idleConnTimeoutMs,omitempty"`
	MaxIdleConnsPerHost     int                 `json:"maxIdleConnsPerHost,omitempty"`
	Health                  egress.HealthConfig `json:"health"`
	// Order 「未命中」连出去的线路顺序（节点 ID）；没列到的排在后面。
	Order []string `json:"order,omitempty"`
	Note  string   `json:"note,omitempty"`
}

// RuleData 一条网关规则。规则顺序由规则链决定，不再单独填优先级。
type RuleData struct {
	Name     string             `json:"name"`
	Enabled  *bool              `json:"enabled,omitempty"`
	Action   egress.Action      `json:"action,omitempty"`
	Strategy egress.Strategy    `json:"strategy,omitempty"`
	Match    egress.MatchConfig `json:"match"`
	Order    []string           `json:"order,omitempty"`
	Note     string             `json:"note,omitempty"`
}

// EndpointData 网关自带协议的线路（http / socks5 / ssh / trojan / shadowsocks / direct）。
// Via 由「经由」连线决定，节点数据里的 via 会被覆盖。
type EndpointData = egress.EndpointConfig

// ProxyData mihomo 节点，Proxy 是 mihomo 原生键值（name / type / server / port …）。
type ProxyData struct {
	Proxy map[string]any `json:"proxy"`
	Note  string         `json:"note,omitempty"`
}

// GroupData mihomo 策略组。成员由连线决定；Builtins 是附加的内置出站（DIRECT、REJECT…）。
type GroupData struct {
	Group    map[string]any `json:"group"`
	Builtins []string       `json:"builtins,omitempty"`
	// BuiltinsFirst 内置出站排在连线成员之前（例如「直连优先」的手动选择组）。
	BuiltinsFirst bool     `json:"builtinsFirst,omitempty"`
	Order         []string `json:"order,omitempty"`
	Note          string   `json:"note,omitempty"`
}

// ProviderData mihomo 订阅（proxy-provider）。
type ProviderData struct {
	Name     string         `json:"name"`
	Provider map[string]any `json:"provider"`
	Note     string         `json:"note,omitempty"`
}

// MihomoRule 分流表里的一行。
type MihomoRule struct {
	ID      string   `json:"id"`
	Type    string   `json:"type"`
	Payload string   `json:"payload,omitempty"`
	Target  string   `json:"target"`
	Params  []string `json:"params,omitempty"`
	Enabled *bool    `json:"enabled,omitempty"`
	Note    string   `json:"note,omitempty"`
}

// RulesData mihomo 分流：「交给 mihomo 分流」的流量按这张表走。
type RulesData struct {
	Rules         []MihomoRule              `json:"rules"`
	RuleProviders map[string]map[string]any `json:"ruleProviders,omitempty"`
	Note          string                    `json:"note,omitempty"`
}

// CoreData mihomo 核心的全局设置、DNS 与其它顶层键。
type CoreData struct {
	Settings     mihomoSettings `json:"settings"`
	DNS          map[string]any `json:"dns,omitempty"`
	DNSEnabled   bool           `json:"dnsEnabled"`
	Extra        map[string]any `json:"extra,omitempty"`
	Note         string         `json:"note,omitempty"`
	AutoTestURL  string         `json:"autoTestUrl,omitempty"`
	SkipCoreTest bool           `json:"skipCoreTest,omitempty"`
}

// PortSpec 一个输出端口：能连到哪些节点、能不能连多条。
type PortSpec struct {
	ID      string   `json:"id"`
	Label   string   `json:"label"`
	Accepts []string `json:"accepts"`
	Multi   bool     `json:"multi"`
	Side    string   `json:"side"` // bottom / right
}

// NodeSpec 节点目录项，驱动控制台的节点库与连线合法性。
type NodeSpec struct {
	Type     string     `json:"type"`
	Label    string     `json:"label"`
	Category string     `json:"category"`
	Hint     string     `json:"hint"`
	Single   bool       `json:"single,omitempty"`
	Input    string     `json:"input,omitempty"` // 输入端口位置：top / left；空表示没有输入
	Outputs  []PortSpec `json:"outputs"`
}

// Category 节点库分组。
type Category struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

var routeTargets = []string{NodeEndpoint, NodeGroup, NodeProxy, NodeRules}

// NodeSpecs 节点目录。
func NodeSpecs() []NodeSpec {
	return []NodeSpec{
		{Type: NodeGateway, Label: "网关设置", Category: "gateway", Single: true,
			Hint: "总开关、超时与健康检查；往下接第一条规则，「未命中」接兜底线路",
			Outputs: []PortSpec{
				{ID: PortRules, Label: "规则链", Accepts: []string{NodeRule}, Side: "bottom"},
				{ID: PortFallback, Label: "未命中", Accepts: routeTargets, Multi: true, Side: "right"},
			}},
		{Type: NodeRule, Label: "路由规则", Category: "gateway", Input: "top",
			Hint: "按域名、网段、端口、调用方匹配；命中后走代理、直连或拒绝",
			Outputs: []PortSpec{
				{ID: PortNext, Label: "下一条", Accepts: []string{NodeRule}, Side: "bottom"},
				{ID: PortHit, Label: "命中", Accepts: routeTargets, Multi: true, Side: "right"},
			}},
		{Type: NodeEndpoint, Label: "网关线路", Category: "gateway", Input: "left",
			Hint: "网关内置协议：HTTP / HTTPS / SOCKS5 / SSH / Trojan / Shadowsocks / 直连",
			Outputs: []PortSpec{
				{ID: PortVia, Label: "经由", Accepts: []string{NodeEndpoint}, Side: "right"},
			}},
		{Type: NodeGroup, Label: "策略组", Category: "mihomo", Input: "left",
			Hint: "手动选择、自动测速、故障转移、负载均衡；成员可以是节点、策略组、订阅",
			Outputs: []PortSpec{
				{ID: PortMembers, Label: "成员", Accepts: []string{NodeProxy, NodeGroup, NodeProvider}, Multi: true, Side: "right"},
			}},
		{Type: NodeProxy, Label: "节点", Category: "mihomo", Input: "left",
			Hint: "mihomo 支持的全部协议：VLESS / REALITY / Hysteria2 / TUIC / WireGuard / AnyTLS …",
			Outputs: []PortSpec{
				{ID: PortDialer, Label: "前置代理", Accepts: []string{NodeProxy, NodeGroup}, Side: "right"},
			}},
		{Type: NodeProvider, Label: "订阅", Category: "mihomo", Input: "left",
			Hint: "机场订阅或本地节点文件，核心定时拉取，支持筛选、改名与健康检查",
			Outputs: []PortSpec{
				{ID: PortDialer, Label: "前置代理", Accepts: []string{NodeProxy, NodeGroup}, Side: "right"},
			}},
		{Type: NodeRules, Label: "mihomo 分流", Category: "mihomo", Single: true, Input: "left",
			Hint: "交给核心按 GeoIP、GeoSite、规则集等再分流；网关规则连到这里即可"},
		{Type: NodeCore, Label: "mihomo 核心", Category: "mihomo", Single: true,
			Hint: "核心的全局设置、DNS、Geo 数据库与其它高级键"},
	}
}

// Categories 节点库分组。
func Categories() []Category {
	return []Category{{Key: "gateway", Label: "网关"}, {Key: "mihomo", Label: "mihomo"}}
}

func specOf(nodeType string) (NodeSpec, bool) {
	for _, spec := range NodeSpecs() {
		if spec.Type == nodeType {
			return spec, true
		}
	}
	return NodeSpec{}, false
}

func portOf(nodeType, port string) (PortSpec, bool) {
	spec, ok := specOf(nodeType)
	if !ok {
		return PortSpec{}, false
	}
	for _, item := range spec.Outputs {
		if item.ID == port {
			return item, true
		}
	}
	return PortSpec{}, false
}

func decode[T any](node Node) (T, error) {
	var out T
	if len(node.Data) == 0 {
		return out, nil
	}
	err := json.Unmarshal(node.Data, &out)
	return out, err
}

func encode(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
