package egressgraph

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"

	"aegis/pkg/egress"
	"aegis/pkg/egress/mihomo"
)

// 自动排版用的网格（与控制台卡片宽度 280 对齐）。
const (
	columnWidth = 380
	rowHeight   = 190
	perColumn   = 12
)

func newNodeID(prefix string) string {
	buf := make([]byte, 5)
	_, _ = rand.Read(buf)
	return prefix + "-" + hex.EncodeToString(buf)
}

func edgeID(source, port, target string) string { return source + ":" + port + "->" + target }

func link(source, port, target string) Edge {
	return Edge{ID: edgeID(source, port, target), Source: source, SourceHandle: port, Target: target}
}

// DefaultGateway 新画布里网关设置节点的默认值。
func DefaultGateway() GatewayData {
	return GatewayData{
		Enabled:       true,
		DefaultAction: egress.ActionDirect,
		Health: egress.HealthConfig{
			Enabled: true, PassiveEnabled: true, IntervalSeconds: 60, TimeoutSeconds: 8,
			FailureThreshold: 2, SuccessThreshold: 1, CooldownSeconds: 30, ProbeURL: egress.DefaultProbeURL,
		},
	}
}

// NewGraph 空白画布：只有一个网关设置。
func NewGraph() Graph {
	return Graph{Nodes: []Node{{ID: "gateway", Type: NodeGateway, Data: encode(DefaultGateway())}}, Edges: []Edge{}}
}

// FromConfig 把现有的网关配置（.env 或旧版控制台保存的）画成画布：
// 网关设置在左上，规则按优先级竖排成链，线路排在右侧一列。
func FromConfig(cfg egress.Config) Graph {
	cfg = cfg.Normalize()
	graph := Graph{Edges: []Edge{}}
	gateway := GatewayData{
		Enabled:                 cfg.Enabled,
		DefaultAction:           cfg.DefaultAction,
		DefaultStrategy:         cfg.DefaultStrategy,
		DialTimeoutMS:           cfg.DialTimeoutMS,
		TLSHandshakeTimeoutMS:   cfg.TLSHandshakeTimeoutMS,
		ResponseHeaderTimeoutMS: cfg.ResponseHeaderTimeoutMS,
		IdleConnTimeoutMS:       cfg.IdleConnTimeoutMS,
		MaxIdleConnsPerHost:     cfg.MaxIdleConnsPerHost,
		Health:                  cfg.Health,
	}
	graph.Nodes = append(graph.Nodes, Node{ID: "gateway", Type: NodeGateway, Position: Position{X: 0, Y: 0}, Data: encode(gateway)})

	endpointIDs := map[string]string{}
	row := 0
	for _, endpoint := range cfg.Endpoints {
		if endpoint.Protocol == mihomo.Protocol {
			continue
		}
		id := "ep-" + unsafeName.ReplaceAllString(endpoint.Name, "_")
		endpointIDs[endpoint.Name] = id
		endpoint.Via = ""
		graph.Nodes = append(graph.Nodes, Node{ID: id, Type: NodeEndpoint, Position: Position{X: columnWidth + 80, Y: float64(row * rowHeight)}, Data: encode(endpoint)})
		row++
	}
	for _, endpoint := range cfg.Endpoints {
		if endpoint.Via != "" {
			if source, ok := endpointIDs[endpoint.Name]; ok {
				if target, ok := endpointIDs[endpoint.Via]; ok {
					graph.Edges = append(graph.Edges, link(source, PortVia, target))
				}
			}
		}
	}

	resolve := func(names []string) []string {
		out := []string{}
		for _, name := range names {
			if id, ok := endpointIDs[name]; ok {
				out = append(out, id)
			}
		}
		return out
	}
	previous, port := "gateway", PortRules
	for index, rule := range cfg.Rules {
		id := fmt.Sprintf("rule-%d", index+1)
		data := RuleData{Name: rule.Name, Enabled: rule.Enabled, Action: rule.Action, Strategy: rule.Strategy, Match: rule.Match, Note: rule.Remark}
		targets := resolve(rule.Endpoints)
		data.Order = targets
		graph.Nodes = append(graph.Nodes, Node{ID: id, Type: NodeRule, Position: Position{X: 0, Y: float64((index + 1) * (rowHeight + 40))}, Data: encode(data)})
		graph.Edges = append(graph.Edges, link(previous, port, id))
		for _, target := range targets {
			graph.Edges = append(graph.Edges, link(id, PortHit, target))
		}
		previous, port = id, PortNext
	}
	for _, target := range resolve(cfg.DefaultEndpoints) {
		graph.Edges = append(graph.Edges, link("gateway", PortFallback, target))
	}
	return graph
}

// ImportMode 导入方式。
type ImportMode string

const (
	// ImportMerge 追加到画布，重名的加后缀。
	ImportMerge ImportMode = "merge"
	// ImportReplace 替换画布上全部 mihomo 节点；网关规则连向旧节点的线按名字接到新节点上。
	ImportReplace ImportMode = "replace"
)

// ImportOptions 导入选项。
type ImportOptions struct {
	Mode ImportMode `json:"mode"`
	// RouteFallback 把「未命中」接到 mihomo 分流（没有分流表时接到第一个策略组）。
	RouteFallback bool `json:"routeFallback"`
}

// Import 把一份 mihomo 配置并进画布，返回新画布与说明。
func Import(graph Graph, profile mihomo.Profile, opts ImportOptions) (Graph, []string) {
	notes := []string{}
	out := Graph{Nodes: append([]Node(nil), graph.Nodes...), Edges: append([]Edge(nil), graph.Edges...), Viewport: graph.Viewport}
	if len(out.Nodes) == 0 {
		out = NewGraph()
	}

	// 替换模式：记下网关规则原来连到哪些名字，删掉旧的 mihomo 节点与它们的线。
	relink := map[string][]Edge{}
	if opts.Mode == ImportReplace {
		removed := map[string]bool{}
		names := map[string]string{}
		for _, node := range out.Nodes {
			if strings.HasPrefix(node.Type, "m_") {
				removed[node.ID] = true
				names[node.ID] = NodeName(node)
			}
		}
		kept := out.Nodes[:0]
		for _, node := range out.Nodes {
			if !removed[node.ID] {
				kept = append(kept, node)
			}
		}
		out.Nodes = kept
		edges := out.Edges[:0]
		for _, edge := range out.Edges {
			if removed[edge.Target] && !removed[edge.Source] {
				key := names[edge.Target]
				if node := findType(graph, edge.Target); node == NodeRules {
					key = "\x00rules"
				}
				relink[key] = append(relink[key], edge)
				continue
			}
			if removed[edge.Source] || removed[edge.Target] {
				continue
			}
			edges = append(edges, edge)
		}
		out.Edges = edges
	}

	taken := map[string]bool{}
	hasRules, hasCore := false, false
	for _, node := range out.Nodes {
		if strings.HasPrefix(node.Type, "m_") {
			taken[NodeName(node)] = true
		}
		hasRules = hasRules || node.Type == NodeRules
		hasCore = hasCore || node.Type == NodeCore
	}
	rename := map[string]string{}
	unique := func(name string) string {
		if !taken[name] {
			taken[name] = true
			return name
		}
		for i := 2; ; i++ {
			candidate := fmt.Sprintf("%s %d", name, i)
			if !taken[candidate] {
				taken[candidate] = true
				rename[name] = candidate
				return candidate
			}
		}
	}

	// 放在现有节点的右侧，避免压住。
	baseX := 0.0
	for _, node := range out.Nodes {
		baseX = max(baseX, node.Position.X+columnWidth)
	}
	baseX += 120
	ids := map[string]string{} // 核心名字 → 节点
	place := func(column, index int) Position {
		return Position{X: baseX + float64(column)*columnWidth, Y: float64(index * rowHeight)}
	}

	column := 0
	if !hasCore {
		data := CoreData{Settings: profile.Settings, DNS: profile.DNS, DNSEnabled: len(profile.DNS) > 0, Extra: profile.Extra}
		if data.Settings.Mode == "" {
			data.Settings = DefaultCoreSettings("")
		}
		out.Nodes = append(out.Nodes, Node{ID: newNodeID(NodeCore), Type: NodeCore, Position: place(column, 0), Data: encode(data)})
	} else if len(profile.DNS) > 0 || len(profile.Extra) > 0 {
		notes = append(notes, "画布上已有核心设置，导入内容里的 DNS 与全局设置没有覆盖它")
	}
	var rulesNodeID string
	if len(profile.Rules) > 0 && !hasRules {
		rulesNodeID = newNodeID(NodeRules)
		out.Nodes = append(out.Nodes, Node{ID: rulesNodeID, Type: NodeRules, Position: place(column, 2)})
	} else if len(profile.Rules) > 0 {
		notes = append(notes, "画布上已有 mihomo 分流，导入的分流规则没有并入")
	}
	column++

	groupIDs := []string{}
	for index, group := range profile.ProxyGroups {
		unique(stringOf(group, "name"))
		id := newNodeID(NodeGroup)
		ids[stringOf(group, "name")] = id
		groupIDs = append(groupIDs, id)
		out.Nodes = append(out.Nodes, Node{ID: id, Type: NodeGroup, Position: place(column+index/perColumn, index%perColumn)})
	}
	if len(profile.ProxyGroups) > 0 {
		column += (len(profile.ProxyGroups)-1)/perColumn + 1
	}
	for index, proxy := range profile.Proxies {
		original := stringOf(proxy, "name")
		data := ProxyData{Proxy: cloneMap(proxy)}
		data.Proxy["name"] = unique(original)
		delete(data.Proxy, "dialer-proxy")
		id := newNodeID(NodeProxy)
		ids[original] = id
		out.Nodes = append(out.Nodes, Node{ID: id, Type: NodeProxy, Position: place(column+index/perColumn, index%perColumn), Data: encode(data)})
	}
	if len(profile.Proxies) > 0 {
		column += (len(profile.Proxies)-1)/perColumn + 1
	}
	providerNames := sortedKeys(profile.ProxyProviders)
	providerIDs := map[string]string{}
	for index, name := range providerNames {
		provider := cloneMap(profile.ProxyProviders[name])
		override := mapOf(provider["override"])
		delete(override, "dialer-proxy")
		if len(override) > 0 {
			provider["override"] = override
		} else {
			delete(provider, "override")
		}
		id := newNodeID(NodeProvider)
		providerIDs[name] = id
		out.Nodes = append(out.Nodes, Node{ID: id, Type: NodeProvider, Position: place(column, index), Data: encode(ProviderData{Name: unique(name), Provider: provider})})
	}

	// 第二遍：策略组数据（成员要等全部节点建好才能连）与前置代理。
	nodeIndex := map[string]int{}
	for i, node := range out.Nodes {
		nodeIndex[node.ID] = i
	}
	for i, group := range profile.ProxyGroups {
		id := groupIDs[i]
		data := GroupData{Group: cloneMap(group)}
		data.Group["name"] = NodeNameFromGroup(group, rename)
		delete(data.Group, "proxies")
		delete(data.Group, "use")
		for _, member := range toStrings(group["proxies"]) {
			if mihomo.IsBuiltin(member) {
				data.Builtins = append(data.Builtins, member)
				if len(data.Order) == 0 && len(data.Builtins) == 1 {
					data.BuiltinsFirst = true
				}
				continue
			}
			if target, ok := ids[member]; ok {
				out.Edges = append(out.Edges, link(id, PortMembers, target))
				data.Order = append(data.Order, target)
			} else {
				notes = append(notes, fmt.Sprintf("策略组「%s」的成员「%s」不存在，已跳过", data.Group["name"], member))
			}
		}
		for _, use := range toStrings(group["use"]) {
			if target, ok := providerIDs[use]; ok {
				out.Edges = append(out.Edges, link(id, PortMembers, target))
				data.Order = append(data.Order, target)
			}
		}
		out.Nodes[nodeIndex[id]].Data = encode(data)
	}
	for _, proxy := range profile.Proxies {
		if dialer := stringOf(proxy, "dialer-proxy"); dialer != "" {
			if target, ok := ids[dialer]; ok {
				out.Edges = append(out.Edges, link(ids[stringOf(proxy, "name")], PortDialer, target))
			}
		}
	}
	for name, provider := range profile.ProxyProviders {
		if dialer := stringOf(mapOf(provider["override"]), "dialer-proxy"); dialer != "" {
			if target, ok := ids[dialer]; ok {
				out.Edges = append(out.Edges, link(providerIDs[name], PortDialer, target))
			}
		}
	}

	if rulesNodeID != "" {
		data := RulesData{RuleProviders: profile.RuleProviders}
		for _, raw := range profile.Rules {
			rule, ok := ParseMihomoRule(raw)
			if !ok {
				notes = append(notes, "无法识别的分流规则: "+raw)
				continue
			}
			if renamed, ok := rename[rule.Target]; ok {
				rule.Target = renamed
			}
			data.Rules = append(data.Rules, rule)
		}
		out.Nodes[nodeIndex[rulesNodeID]].Data = encode(data)
	}

	// 替换模式下把网关规则按名字接回新节点。
	for key, edges := range relink {
		target := ids[key]
		if key == "\x00rules" {
			target = rulesNodeID
			for _, node := range out.Nodes {
				if node.Type == NodeRules {
					target = node.ID
				}
			}
		}
		for _, edge := range edges {
			if target == "" {
				notes = append(notes, fmt.Sprintf("原来连到「%s」的线路在新配置里找不到同名节点，已断开", key))
				continue
			}
			out.Edges = append(out.Edges, link(edge.Source, edge.SourceHandle, target))
		}
	}

	if opts.RouteFallback {
		target := rulesNodeID
		if target == "" && len(groupIDs) > 0 {
			target = groupIDs[0]
		}
		if target != "" {
			out.Edges = slices.DeleteFunc(out.Edges, func(edge Edge) bool { return edge.SourceHandle == PortFallback })
			for i, node := range out.Nodes {
				if node.Type != NodeGateway {
					continue
				}
				gateway, _ := decode[GatewayData](node)
				gateway.DefaultAction = egress.ActionProxy
				gateway.Order = nil
				out.Nodes[i].Data = encode(gateway)
				out.Edges = append(out.Edges, link(node.ID, PortFallback, target))
				break
			}
		}
	}
	if len(rename) > 0 {
		notes = append(notes, fmt.Sprintf("%d 个名字与画布上已有的重名，已加序号", len(rename)))
	}
	return out, notes
}

// NodeNameFromGroup 策略组导入后的名字（重名时取改过的）。
func NodeNameFromGroup(group map[string]any, rename map[string]string) string {
	name := stringOf(group, "name")
	if renamed, ok := rename[name]; ok {
		return renamed
	}
	return name
}

func findType(graph Graph, id string) string {
	for _, node := range graph.Nodes {
		if node.ID == id {
			return node.Type
		}
	}
	return ""
}

func toStrings(v any) []string {
	switch items := v.(type) {
	case []string:
		return items
	case []any:
		out := make([]string, 0, len(items))
		for _, item := range items {
			out = append(out, strings.TrimSpace(fmt.Sprint(item)))
		}
		return out
	default:
		return nil
	}
}

var ruleSeq int

// ParseMihomoRule 拆一条规则字符串。逻辑规则（AND / OR / NOT / SUB-RULE）的 payload 是括号表达式，
// 里面的逗号不算分隔。
func ParseMihomoRule(raw string) (MihomoRule, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.HasPrefix(raw, "#") {
		return MihomoRule{}, false
	}
	parts := []string{}
	depth, start := 0, 0
	for i, r := range raw {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(raw[start:i]))
				start = i + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(raw[start:]))
	ruleSeq++
	rule := MihomoRule{ID: fmt.Sprintf("r%x", ruleSeq), Type: strings.ToUpper(parts[0])}
	switch {
	case rule.Type == "MATCH" && len(parts) >= 2:
		rule.Target = parts[1]
	case len(parts) >= 3:
		rule.Payload, rule.Target = parts[1], parts[2]
		rule.Params = parts[3:]
	default:
		return MihomoRule{}, false
	}
	return rule, true
}
