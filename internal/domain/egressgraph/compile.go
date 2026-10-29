package egressgraph

import (
	"fmt"

	"aegis/internal/domain/itemgrant"
	"regexp"
	"slices"
	"strings"

	"aegis/pkg/egress"
	"aegis/pkg/egress/mihomo"
)

type mihomoSettings = mihomo.Settings

// Summary 画布概况，显示在顶栏与概览里。
type Summary struct {
	Enabled       bool   `json:"enabled"`
	DefaultAction string `json:"defaultAction"`
	Rules         int    `json:"rules"`
	ActiveRules   int    `json:"activeRules"`
	Endpoints     int    `json:"endpoints"`
	Proxies       int    `json:"proxies"`
	Groups        int    `json:"groups"`
	Providers     int    `json:"providers"`
	MihomoRules   int    `json:"mihomoRules"`
	RuleProviders int    `json:"ruleProviders"`
	MihomoEnabled bool   `json:"mihomoEnabled"`
	Targets       int    `json:"targets"`
}

// Result 编译产物。
type Result struct {
	Config  egress.Config  `json:"-"`
	Profile mihomo.Profile `json:"-"`
	// Targets 网关引用的 mihomo 目标名（每个对应一个入站用户）。
	Targets []string `json:"targets"`
	Issues  []Issue  `json:"issues"`
	Summary Summary  `json:"summary"`
	// Bindings 节点 → 网关端点名；运行态（健康、延迟）按它贴回画布。
	Bindings map[string]string `json:"bindings"`
	// RuleNodes 规则名 → 节点，命中计数按它贴回画布。
	RuleNodes map[string]string `json:"ruleNodes"`
	// MihomoNames 节点 → 核心里的名字（节点、策略组、订阅）。
	MihomoNames map[string]string `json:"mihomoNames"`
	// Chain 规则链顺序（节点 ID）。
	Chain []string `json:"chain"`
}

// HasErrors 是否有阻止应用的问题。
func (r Result) HasErrors() bool {
	for _, issue := range r.Issues {
		if issue.Level == issueError {
			return true
		}
	}
	return false
}

const (
	issueError    = itemgrant.IssueError
	issueWarning  = itemgrant.IssueWarning
	rulesEndpoint = "mihomo.rules"
)

var endpointNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

type compiler struct {
	graph    Graph
	nodes    map[string]Node
	out      map[string][]Edge // source + "\x00" + port → 连线（按声明顺序）
	incoming map[string][]Edge
	issues   []Issue

	endpointNames map[string]string // 网关端点名 → 节点
	mihomoNames   map[string]string // 节点 → 核心里的名字
	synthesized   map[string]egress.EndpointConfig
	targets       []string
}

func (c *compiler) errorf(nodeID, edgeID, format string, args ...any) {
	c.issues = append(c.issues, Issue{Level: issueError, NodeID: nodeID, EdgeID: edgeID, Message: fmt.Sprintf(format, args...)})
}

func (c *compiler) warnf(nodeID, edgeID, format string, args ...any) {
	c.issues = append(c.issues, Issue{Level: issueWarning, NodeID: nodeID, EdgeID: edgeID, Message: fmt.Sprintf(format, args...)})
}

func edgeKey(source, port string) string { return source + "\x00" + port }

func (c *compiler) edges(source, port string) []Edge { return c.out[edgeKey(source, port)] }

// Compile 校验画布并编译成网关配置与核心配置。即使有错误也尽量把能编译的部分编译出来，
// 让控制台一次看到所有问题；调用方用 HasErrors 决定能不能应用。
func Compile(graph Graph) Result {
	c := &compiler{
		graph:         graph,
		nodes:         map[string]Node{},
		out:           map[string][]Edge{},
		incoming:      map[string][]Edge{},
		endpointNames: map[string]string{},
		mihomoNames:   map[string]string{},
		synthesized:   map[string]egress.EndpointConfig{},
	}
	result := Result{Bindings: map[string]string{}, RuleNodes: map[string]string{}}

	for _, node := range graph.Nodes {
		if _, ok := specOf(node.Type); !ok {
			c.errorf(node.ID, "", "未知节点类型 %s", node.Type)
			continue
		}
		if _, dup := c.nodes[node.ID]; dup {
			c.errorf(node.ID, "", "节点标识重复")
			continue
		}
		c.nodes[node.ID] = node
	}
	c.indexEdges()

	gateway, gatewayData := c.gateway()
	c.collectNames()

	cfg := egress.Config{Enabled: true, DefaultAction: egress.ActionDirect, Source: "graph"}
	if gateway != nil {
		cfg.Enabled = gatewayData.Enabled
		cfg.DefaultStrategy = gatewayData.DefaultStrategy
		cfg.DialTimeoutMS = gatewayData.DialTimeoutMS
		cfg.TLSHandshakeTimeoutMS = gatewayData.TLSHandshakeTimeoutMS
		cfg.ResponseHeaderTimeoutMS = gatewayData.ResponseHeaderTimeoutMS
		cfg.IdleConnTimeoutMS = gatewayData.IdleConnTimeoutMS
		cfg.MaxIdleConnsPerHost = gatewayData.MaxIdleConnsPerHost
		cfg.Health = gatewayData.Health
	}

	// 网关线路（内置协议）
	for _, node := range c.sorted(NodeEndpoint) {
		data, err := decode[EndpointData](node)
		if err != nil {
			c.errorf(node.ID, "", "线路配置无法解析: %v", err)
			continue
		}
		if data.Protocol == mihomo.Protocol {
			c.errorf(node.ID, "", "mihomo 线路请用「节点」或「策略组」，网关会自动接入核心")
			continue
		}
		data.Via = ""
		if via := c.edges(node.ID, PortVia); len(via) > 0 {
			if target, ok := c.nodes[via[0].Target]; ok {
				if name := c.endpointNameOf(target); name != "" {
					data.Via = name
				}
			}
		}
		cfg.Endpoints = append(cfg.Endpoints, data)
		result.Bindings[node.ID] = data.Name
	}

	// 规则链
	chain := c.ruleChain(gateway)
	result.Chain = chain
	ruleNames := map[string]string{}
	activeRules := 0
	for index, id := range chain {
		node := c.nodes[id]
		data, err := decode[RuleData](node)
		if err != nil {
			c.errorf(id, "", "规则配置无法解析: %v", err)
			continue
		}
		data.Name = strings.TrimSpace(data.Name)
		if data.Name == "" {
			c.errorf(id, "", "规则需要名称")
			continue
		}
		if previous, dup := ruleNames[data.Name]; dup && previous != id {
			c.errorf(id, "", "规则名称「%s」重复", data.Name)
			continue
		}
		ruleNames[data.Name] = id
		action := data.Action
		if action == "" {
			action = egress.ActionProxy
		}
		targets := c.routeTargets(node, PortHit, data.Order)
		rule := egress.RuleConfig{
			Name:     data.Name,
			Enabled:  data.Enabled,
			Priority: (index + 1) * 10,
			Action:   action,
			Strategy: data.Strategy,
			Match:    data.Match,
			Remark:   data.Note,
		}
		switch action {
		case egress.ActionProxy:
			if len(targets) == 0 {
				c.errorf(id, "", "动作是「走代理」，需要从「命中」连到至少一条线路")
			}
			rule.Endpoints = targets
		default:
			if len(c.edges(id, PortHit)) > 0 {
				c.warnf(id, "", "动作是「%s」，连出去的线路不会用到", actionLabel(action))
			}
		}
		if !matchHasCondition(data.Match) {
			c.errorf(id, "", "至少要有一个匹配条件，或打开「匹配全部」")
		}
		if data.Enabled == nil || *data.Enabled {
			activeRules++
		}
		cfg.Rules = append(cfg.Rules, rule)
		result.RuleNodes[data.Name] = id
	}
	c.checkUDPTargets(cfg, result.RuleNodes)
	for _, node := range c.sorted(NodeRule) {
		if !slices.Contains(chain, node.ID) {
			c.warnf(node.ID, "", "没有接进规则链，不会生效")
		}
	}

	// 未命中
	if gateway != nil {
		fallback := c.routeTargets(*gateway, PortFallback, gatewayData.Order)
		action := gatewayData.DefaultAction
		if action == "" {
			if len(fallback) > 0 {
				action = egress.ActionProxy
			} else {
				action = egress.ActionDirect
			}
		}
		cfg.DefaultAction = action
		if action == egress.ActionProxy {
			if len(fallback) == 0 {
				c.errorf(gateway.ID, "", "未命中时「走代理」，需要从「未命中」连到至少一条线路")
			}
			cfg.DefaultEndpoints = fallback
		} else if len(fallback) > 0 {
			c.warnf(gateway.ID, "", "未命中时「%s」，「未命中」连出去的线路不会用到", actionLabel(action))
		}
	}

	for _, name := range sortedKeys(c.synthesized) {
		cfg.Endpoints = append(cfg.Endpoints, c.synthesized[name])
	}
	for nodeID := range c.mihomoNames {
		if node := c.nodes[nodeID]; node.Type == NodeProxy || node.Type == NodeGroup {
			if endpoint := synthesizedName(node); c.synthesized[endpoint].Name != "" {
				result.Bindings[nodeID] = endpoint
			}
		}
	}
	for _, node := range c.sorted(NodeRules) {
		if _, ok := c.synthesized[rulesEndpoint]; ok {
			result.Bindings[node.ID] = rulesEndpoint
		}
	}

	normalized := cfg.Normalize()
	if !c.hasErrors() {
		if err := normalized.Validate(); err != nil {
			c.errorf("", "", "%v", err)
		}
	}
	result.Config = normalized

	profile := c.mihomoProfile()
	result.Profile = profile
	result.Targets = uniqueSorted(c.targets)
	result.MihomoNames = c.mihomoNames

	result.Summary = Summary{
		Enabled:       cfg.Enabled,
		DefaultAction: string(cfg.DefaultAction),
		Rules:         len(c.sorted(NodeRule)),
		ActiveRules:   activeRules,
		Endpoints:     len(c.sorted(NodeEndpoint)),
		Proxies:       len(profile.Proxies),
		Groups:        len(profile.ProxyGroups),
		Providers:     len(profile.ProxyProviders),
		MihomoRules:   len(profile.Rules),
		RuleProviders: len(profile.RuleProviders),
		MihomoEnabled: !profile.Empty(),
		Targets:       len(result.Targets),
	}
	result.Issues = c.issues
	if result.Issues == nil {
		result.Issues = []Issue{}
	}
	return result
}

func (c *compiler) hasErrors() bool {
	for _, issue := range c.issues {
		if issue.Level == issueError {
			return true
		}
	}
	return false
}

// indexEdges 校验连线：端口存在、目标类型合法、单线端口只连一条、不重复。
func (c *compiler) indexEdges() {
	seen := map[string]bool{}
	for _, edge := range c.graph.Edges {
		source, okSource := c.nodes[edge.Source]
		target, okTarget := c.nodes[edge.Target]
		if !okSource || !okTarget {
			continue
		}
		port, ok := portOf(source.Type, edge.SourceHandle)
		if !ok {
			c.errorf(source.ID, edge.ID, "连线从不存在的端口 %q 连出", edge.SourceHandle)
			continue
		}
		if !slices.Contains(port.Accepts, target.Type) {
			c.errorf(source.ID, edge.ID, "「%s」不能连到这种节点", port.Label)
			continue
		}
		if edge.Source == edge.Target {
			c.errorf(source.ID, edge.ID, "节点不能连到自己")
			continue
		}
		key := edgeKey(edge.Source, edge.SourceHandle)
		if seen[key+"\x00"+edge.Target] {
			continue
		}
		seen[key+"\x00"+edge.Target] = true
		if !port.Multi && len(c.out[key]) > 0 {
			c.errorf(source.ID, edge.ID, "「%s」只能连一条线", port.Label)
			continue
		}
		c.out[key] = append(c.out[key], edge)
		c.incoming[edge.Target] = append(c.incoming[edge.Target], edge)
	}
}

func (c *compiler) gateway() (*Node, GatewayData) {
	nodes := c.sorted(NodeGateway)
	if len(nodes) == 0 {
		c.errorf("", "", "画布上需要一个「网关设置」节点")
		return nil, GatewayData{}
	}
	if len(nodes) > 1 {
		for _, node := range nodes[1:] {
			c.errorf(node.ID, "", "只能有一个「网关设置」")
		}
	}
	data, err := decode[GatewayData](nodes[0])
	if err != nil {
		c.errorf(nodes[0].ID, "", "网关设置无法解析: %v", err)
	}
	return &nodes[0], data
}

// sorted 按画布声明顺序返回某类节点。
func (c *compiler) sorted(nodeType string) []Node {
	out := []Node{}
	for _, node := range c.graph.Nodes {
		if node.Type == nodeType {
			if _, ok := c.nodes[node.ID]; ok {
				out = append(out, node)
			}
		}
	}
	return out
}

// ruleChain 从网关设置的「规则链」沿「下一条」走到底。
func (c *compiler) ruleChain(gateway *Node) []string {
	if gateway == nil {
		return nil
	}
	chain := []string{}
	visited := map[string]bool{}
	edges := c.edges(gateway.ID, PortRules)
	for len(edges) > 0 {
		edge := edges[0]
		id := edge.Target
		if visited[id] {
			c.errorf(id, edge.ID, "规则链成环")
			break
		}
		if incoming := c.chainIncoming(id); incoming > 1 {
			c.errorf(id, "", "一条规则只能有一个上游（规则链是一条直线）")
		}
		visited[id] = true
		chain = append(chain, id)
		edges = c.edges(id, PortNext)
	}
	return chain
}

func (c *compiler) chainIncoming(id string) int {
	count := 0
	for _, edge := range c.incoming[id] {
		if edge.SourceHandle == PortRules || edge.SourceHandle == PortNext {
			count++
		}
	}
	return count
}

// collectNames 登记网关线路名与核心里的名字，并检查重名。
func (c *compiler) collectNames() {
	for _, node := range c.sorted(NodeEndpoint) {
		data, err := decode[EndpointData](node)
		if err != nil {
			continue
		}
		name := strings.TrimSpace(data.Name)
		switch {
		case name == "":
			c.errorf(node.ID, "", "线路需要名称")
		case !endpointNamePattern.MatchString(name):
			c.errorf(node.ID, "", "线路名称「%s」只能用字母、数字、下划线、连字符和点", name)
		case strings.HasPrefix(name, "mihomo."):
			c.errorf(node.ID, "", "线路名称不能以 mihomo. 开头（留给核心线路）")
		default:
			if previous, dup := c.endpointNames[name]; dup && previous != node.ID {
				c.errorf(node.ID, "", "线路名称「%s」重复", name)
			}
			c.endpointNames[name] = node.ID
		}
	}
	names := map[string]string{}
	claim := func(node Node, name, kind string) {
		switch {
		case name == "":
			c.errorf(node.ID, "", "%s需要名称", kind)
		case strings.Contains(name, ","):
			c.errorf(node.ID, "", "%s名称不能含逗号", kind)
		case mihomo.IsBuiltin(name):
			c.errorf(node.ID, "", "%s名称「%s」与内置出站重名", kind, name)
		default:
			if previous, dup := names[name]; dup && previous != node.ID {
				c.errorf(node.ID, "", "%s名称「%s」与其它节点或策略组重名", kind, name)
			}
			names[name] = node.ID
			c.mihomoNames[node.ID] = name
		}
	}
	for _, node := range c.sorted(NodeProxy) {
		data, err := decode[ProxyData](node)
		if err != nil {
			c.errorf(node.ID, "", "节点配置无法解析: %v", err)
			continue
		}
		claim(node, stringOf(data.Proxy, "name"), "节点")
	}
	for _, node := range c.sorted(NodeGroup) {
		data, err := decode[GroupData](node)
		if err != nil {
			c.errorf(node.ID, "", "策略组配置无法解析: %v", err)
			continue
		}
		claim(node, stringOf(data.Group, "name"), "策略组")
	}
	providers := map[string]string{}
	for _, node := range c.sorted(NodeProvider) {
		data, err := decode[ProviderData](node)
		if err != nil {
			c.errorf(node.ID, "", "订阅配置无法解析: %v", err)
			continue
		}
		name := strings.TrimSpace(data.Name)
		switch {
		case name == "":
			c.errorf(node.ID, "", "订阅需要名称")
		case strings.Contains(name, ","):
			c.errorf(node.ID, "", "订阅名称不能含逗号")
		default:
			if previous, dup := providers[name]; dup && previous != node.ID {
				c.errorf(node.ID, "", "订阅名称「%s」重复", name)
			}
			providers[name] = node.ID
			c.mihomoNames[node.ID] = name
		}
	}
}

// endpointNameOf 一个可作为线路的节点对应的网关端点名；mihomo 目标按需合成端点。
func (c *compiler) endpointNameOf(node Node) string {
	switch node.Type {
	case NodeEndpoint:
		data, err := decode[EndpointData](node)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(data.Name)
	case NodeProxy, NodeGroup:
		target := c.mihomoNames[node.ID]
		if target == "" {
			return ""
		}
		name := synthesizedName(node)
		if _, ok := c.synthesized[name]; !ok {
			c.synthesized[name] = egress.EndpointConfig{
				Name:     name,
				Protocol: mihomo.Protocol,
				Options:  map[string]string{mihomo.OptionTarget: target},
				Region:   "mihomo",
				Remark:   target,
			}
			c.targets = append(c.targets, target)
		}
		return name
	case NodeRules:
		if _, ok := c.synthesized[rulesEndpoint]; !ok {
			c.synthesized[rulesEndpoint] = egress.EndpointConfig{
				Name:     rulesEndpoint,
				Protocol: mihomo.Protocol,
				Region:   "mihomo",
				Remark:   "mihomo 分流",
			}
		}
		return rulesEndpoint
	default:
		return ""
	}
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// synthesizedName 核心目标对应的网关端点名：按节点 ID 而不是核心里的名字，
// 改名不会让运行态统计断开。
func synthesizedName(node Node) string {
	name := "mihomo." + unsafeName.ReplaceAllString(node.ID, "_")
	if len(name) > 64 {
		name = name[:64]
	}
	return name
}

// routeTargets 某个端口连出的线路，按 order 排序后转成网关端点名。
func (c *compiler) routeTargets(node Node, port string, order []string) []string {
	edges := orderEdges(c.edges(node.ID, port), order)
	out := []string{}
	for _, edge := range edges {
		target, ok := c.nodes[edge.Target]
		if !ok {
			continue
		}
		name := c.endpointNameOf(target)
		if name == "" {
			c.errorf(node.ID, edge.ID, "连到的线路缺少名称")
			continue
		}
		if !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}

// orderEdges 按 order 里的节点 ID 排；没列到的保持声明顺序排在后面。
func orderEdges(edges []Edge, order []string) []Edge {
	if len(order) == 0 {
		return edges
	}
	rank := map[string]int{}
	for i, id := range order {
		rank[id] = i
	}
	out := append([]Edge(nil), edges...)
	slices.SortStableFunc(out, func(a, b Edge) int {
		ra, okA := rank[a.Target]
		rb, okB := rank[b.Target]
		switch {
		case okA && okB:
			return ra - rb
		case okA:
			return -1
		case okB:
			return 1
		default:
			return 0
		}
	})
	return out
}

func (c *compiler) mihomoProfile() mihomo.Profile {
	profile := mihomo.Profile{Settings: DefaultCoreSettings("")}
	if cores := c.sorted(NodeCore); len(cores) > 0 {
		if len(cores) > 1 {
			for _, node := range cores[1:] {
				c.errorf(node.ID, "", "只能有一个「mihomo 核心」")
			}
		}
		data, err := decode[CoreData](cores[0])
		if err != nil {
			c.errorf(cores[0].ID, "", "核心设置无法解析: %v", err)
		} else {
			profile.Settings = data.Settings
			if data.DNSEnabled && len(data.DNS) > 0 {
				profile.DNS = data.DNS
			}
			profile.Extra = map[string]any{}
			for key, value := range data.Extra {
				if mihomo.ReservedKey(key) {
					c.warnf(cores[0].ID, "", "高级设置里的 %s 由网关托管，已忽略", key)
					continue
				}
				profile.Extra[key] = value
			}
		}
	}

	nameOf := func(id string) string { return c.mihomoNames[id] }
	for _, node := range c.sorted(NodeProxy) {
		data, err := decode[ProxyData](node)
		if err != nil || nameOf(node.ID) == "" {
			continue
		}
		proxy := cloneMap(data.Proxy)
		proxy["name"] = nameOf(node.ID)
		delete(proxy, "dialer-proxy")
		if stringOf(proxy, "type") == "" {
			c.errorf(node.ID, "", "节点缺少协议类型")
		}
		if needsServer(stringOf(proxy, "type")) && stringOf(proxy, "server") == "" {
			c.errorf(node.ID, "", "节点缺少服务器地址")
		}
		if dialer := c.edges(node.ID, PortDialer); len(dialer) > 0 {
			if name := nameOf(dialer[0].Target); name != "" {
				proxy["dialer-proxy"] = name
			}
		}
		profile.Proxies = append(profile.Proxies, proxy)
	}

	for _, node := range c.sorted(NodeProvider) {
		data, err := decode[ProviderData](node)
		if err != nil || nameOf(node.ID) == "" {
			continue
		}
		provider := cloneMap(data.Provider)
		if stringOf(provider, "type") == "" {
			provider["type"] = "http"
		}
		if stringOf(provider, "type") == "http" && stringOf(provider, "url") == "" {
			c.errorf(node.ID, "", "订阅缺少地址")
		}
		override := mapOf(provider["override"])
		delete(override, "dialer-proxy")
		if dialer := c.edges(node.ID, PortDialer); len(dialer) > 0 {
			if name := nameOf(dialer[0].Target); name != "" {
				override["dialer-proxy"] = name
			}
		}
		if len(override) > 0 {
			provider["override"] = override
		} else {
			delete(provider, "override")
		}
		if profile.ProxyProviders == nil {
			profile.ProxyProviders = map[string]map[string]any{}
		}
		profile.ProxyProviders[nameOf(node.ID)] = provider
	}

	for _, node := range c.sorted(NodeGroup) {
		data, err := decode[GroupData](node)
		if err != nil || nameOf(node.ID) == "" {
			continue
		}
		group := cloneMap(data.Group)
		group["name"] = nameOf(node.ID)
		delete(group, "proxies")
		delete(group, "use")
		if stringOf(group, "type") == "" {
			group["type"] = "select"
		}
		proxies := []any{}
		uses := []any{}
		for _, edge := range orderEdges(c.edges(node.ID, PortMembers), data.Order) {
			member, ok := c.nodes[edge.Target]
			if !ok || nameOf(member.ID) == "" {
				continue
			}
			if member.Type == NodeProvider {
				uses = append(uses, nameOf(member.ID))
			} else {
				proxies = append(proxies, nameOf(member.ID))
			}
		}
		builtins := []any{}
		for _, name := range data.Builtins {
			if mihomo.IsBuiltin(name) {
				builtins = append(builtins, name)
			}
		}
		if data.BuiltinsFirst {
			proxies = append(builtins, proxies...)
		} else {
			proxies = append(proxies, builtins...)
		}
		if len(proxies) > 0 {
			group["proxies"] = proxies
		}
		if len(uses) > 0 {
			group["use"] = uses
		}
		includeAll := truthy(group["include-all"]) || truthy(group["include-all-proxies"]) || truthy(group["include-all-providers"])
		if len(proxies) == 0 && len(uses) == 0 && !includeAll {
			c.errorf(node.ID, "", "策略组没有成员：从「成员」连到节点、策略组或订阅，或附加内置出站")
		}
		if stringOf(group, "type") == "relay" && len(uses) > 0 {
			c.warnf(node.ID, "", "链式中继不支持订阅成员")
		}
		profile.ProxyGroups = append(profile.ProxyGroups, group)
	}
	c.checkGroupCycles()

	if rulesNodes := c.sorted(NodeRules); len(rulesNodes) > 0 {
		if len(rulesNodes) > 1 {
			for _, node := range rulesNodes[1:] {
				c.errorf(node.ID, "", "只能有一个「mihomo 分流」")
			}
		}
		node := rulesNodes[0]
		data, err := decode[RulesData](node)
		if err != nil {
			c.errorf(node.ID, "", "分流表无法解析: %v", err)
		} else {
			profile.RuleProviders = data.RuleProviders
			profile.Rules = c.compileMihomoRules(node.ID, data, profile)
		}
	}

	if !profile.Empty() {
		if err := profile.Validate(); err != nil && !c.hasErrors() {
			c.errorf("", "", "%v", err)
		}
	}
	return profile
}

func (c *compiler) compileMihomoRules(nodeID string, data RulesData, profile mihomo.Profile) []string {
	known := map[string]bool{}
	for _, name := range profile.Names() {
		known[name] = true
	}
	ruleTypes := map[string]mihomo.RuleTypeSpec{}
	for _, spec := range mihomo.BuildCatalog().RuleTypes {
		ruleTypes[spec.Type] = spec
	}
	out := []string{}
	matched := false
	for index, rule := range data.Rules {
		if rule.Enabled != nil && !*rule.Enabled {
			continue
		}
		ruleType := strings.ToUpper(strings.TrimSpace(rule.Type))
		payload := strings.TrimSpace(rule.Payload)
		target := strings.TrimSpace(rule.Target)
		row := index + 1
		if ruleType == "" {
			c.errorf(nodeID, "", "分流第 %d 行缺少类型", row)
			continue
		}
		spec, knownType := ruleTypes[ruleType]
		if !knownType {
			c.warnf(nodeID, "", "分流第 %d 行的类型 %s 不在常用列表里，交给核心校验", row, ruleType)
		}
		if target == "" {
			c.errorf(nodeID, "", "分流第 %d 行缺少目标", row)
			continue
		}
		if !known[target] && !mihomo.IsBuiltin(target) {
			c.errorf(nodeID, "", "分流第 %d 行的目标「%s」不存在", row, target)
			continue
		}
		if matched {
			c.warnf(nodeID, "", "分流第 %d 行在「兜底」之后，永远不会命中", row)
		}
		if ruleType == "MATCH" {
			matched = true
			out = append(out, "MATCH,"+target)
			continue
		}
		if payload == "" && !spec.NoPayload {
			c.errorf(nodeID, "", "分流第 %d 行缺少匹配内容", row)
			continue
		}
		if ruleType == "RULE-SET" {
			if _, ok := data.RuleProviders[payload]; !ok {
				c.errorf(nodeID, "", "分流第 %d 行引用了不存在的规则集「%s」", row, payload)
				continue
			}
		}
		if strings.Contains(payload, ",") && !spec.Logic {
			c.errorf(nodeID, "", "分流第 %d 行的匹配内容不能含逗号", row)
			continue
		}
		parts := []string{ruleType, payload, target}
		for _, param := range rule.Params {
			if param = strings.TrimSpace(param); param != "" {
				parts = append(parts, param)
			}
		}
		out = append(out, strings.Join(parts, ","))
	}
	return out
}

// checkGroupCycles 策略组之间（含前置代理）不能互相引用成环，核心会直接拒绝启动。
func (c *compiler) checkGroupCycles() {
	next := map[string][]Edge{}
	for _, node := range c.graph.Nodes {
		if node.Type != NodeGroup && node.Type != NodeProxy {
			continue
		}
		next[node.ID] = append(next[node.ID], c.edges(node.ID, PortMembers)...)
		next[node.ID] = append(next[node.ID], c.edges(node.ID, PortDialer)...)
	}
	state := map[string]int{} // 0 未访问，1 在栈上，2 已完成
	var visit func(id string)
	visit = func(id string) {
		state[id] = 1
		for _, edge := range next[id] {
			switch state[edge.Target] {
			case 0:
				visit(edge.Target)
			case 1:
				c.errorf(id, edge.ID, "策略组与前置代理互相引用成环")
			}
		}
		state[id] = 2
	}
	for _, node := range c.graph.Nodes {
		if (node.Type == NodeGroup || node.Type == NodeProxy) && state[node.ID] == 0 {
			visit(node.ID)
		}
	}
}

func matchHasCondition(m egress.MatchConfig) bool {
	return m.MatchAll || len(m.DomainSuffixes) > 0 || len(m.Domains) > 0 || len(m.DomainKeywords) > 0 ||
		len(m.DomainRegexps) > 0 || len(m.CIDRs) > 0 || len(m.Ports) > 0 || len(m.Schemes) > 0 || len(m.Profiles) > 0
}

func actionLabel(action egress.Action) string {
	switch action {
	case egress.ActionDirect:
		return "直连"
	case egress.ActionReject:
		return "拒绝"
	default:
		return "走代理"
	}
}

func needsServer(proxyType string) bool {
	switch proxyType {
	case "direct", "dns", "reject", "":
		return false
	default:
		return true
	}
}

func stringOf(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return strings.TrimSpace(v)
	}
	if v, ok := m[key]; ok && v != nil {
		return strings.TrimSpace(fmt.Sprint(v))
	}
	return ""
}

func mapOf(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return cloneMap(m)
	}
	return map[string]any{}
}

func cloneMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func truthy(v any) bool {
	b, ok := v.(bool)
	return ok && b
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	slices.Sort(out)
	return out
}

func uniqueSorted(in []string) []string {
	out := append([]string(nil), in...)
	slices.Sort(out)
	return slices.Compact(out)
}

// DefaultCoreSettings 新建核心节点时的默认设置。mirror 非空时 Geo 数据库也走它下载。
func DefaultCoreSettings(mirror string) mihomo.Settings {
	settings := mihomo.Settings{
		Mode:          "rule",
		LogLevel:      "info",
		UnifiedDelay:  true,
		TCPConcurrent: true,
		GeodataMode:   false,
	}
	if mirror = strings.TrimRight(strings.TrimSpace(mirror), "/"); mirror != "" {
		base := mirror + "/https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/"
		settings.GeoxURL = map[string]string{
			"geoip":   base + "geoip.dat",
			"geosite": base + "geosite.dat",
			"mmdb":    base + "country.mmdb",
			"asn":     base + "GeoLite2-ASN.mmdb",
		}
	}
	return settings
}

// checkUDPTargets 规则明确匹配 UDP 而连到的线路都不能承载 UDP 时提醒：这类流量一定会失败。
func (c *compiler) checkUDPTargets(cfg egress.Config, ruleNodes map[string]string) {
	endpoints := map[string]egress.EndpointConfig{}
	for _, endpoint := range cfg.Endpoints {
		endpoints[endpoint.Name] = endpoint
	}
	for name, endpoint := range c.synthesized {
		endpoints[name] = endpoint
	}
	for _, rule := range cfg.Rules {
		if rule.Action != egress.ActionProxy || !slices.Contains(rule.Match.Networks, "udp") {
			continue
		}
		capable := false
		for _, name := range rule.Endpoints {
			if udpCapable(name, endpoints, 0) {
				capable = true
				break
			}
		}
		if !capable {
			c.warnf(ruleNodes[rule.Name], "", "这条规则匹配 UDP，但连到的线路都不能承载 UDP（HTTP、SSH 只有 TCP；SOCKS5、Shadowsocks 经不能承载 UDP 的跳板时也不行）")
		}
	}
}

func udpCapable(name string, endpoints map[string]egress.EndpointConfig, depth int) bool {
	endpoint, ok := endpoints[name]
	if !ok || depth > 8 || (endpoint.UDP != nil && !*endpoint.UDP) {
		return false
	}
	switch egress.ProtocolUDP(endpoint.Protocol) {
	case egress.UDPOverStream:
		return true
	case egress.UDPOverUDP:
		return endpoint.Via == "" || udpCapable(endpoint.Via, endpoints, depth+1)
	default:
		return false
	}
}
