package egressgraph

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"aegis/pkg/egress"
	"aegis/pkg/egress/mihomo"
)

func node(id, nodeType string, data any) Node {
	raw, _ := json.Marshal(data)
	return Node{ID: id, Type: nodeType, Data: raw}
}

func sampleGraph() Graph {
	gateway := DefaultGateway()
	gateway.DefaultAction = egress.ActionProxy
	return Graph{
		Nodes: []Node{
			node("gw", NodeGateway, gateway),
			node("r1", NodeRule, RuleData{Name: "stripe", Action: egress.ActionProxy, Match: egress.MatchConfig{DomainSuffixes: []string{"stripe.com"}}}),
			node("r2", NodeRule, RuleData{Name: "openai", Match: egress.MatchConfig{DomainSuffixes: []string{"openai.com"}}}),
			node("r3", NodeRule, RuleData{Name: "block", Action: egress.ActionReject, Match: egress.MatchConfig{Domains: []string{"bad.example"}}}),
			node("loose", NodeRule, RuleData{Name: "loose", Match: egress.MatchConfig{MatchAll: true}}),
			node("ep1", NodeEndpoint, egress.EndpointConfig{Name: "hk-socks", Protocol: egress.ProtocolSOCKS5H, Address: "10.0.0.1:1080", Password: "s3cret"}),
			node("ep2", NodeEndpoint, egress.EndpointConfig{Name: "jump", Protocol: egress.ProtocolSSH, Address: "10.0.0.2:22", SSH: egress.SSHConfig{User: "root", Password: "pw"}}),
			node("g1", NodeGroup, GroupData{Group: map[string]any{"name": "🚀 节点选择", "type": "select"}, Builtins: []string{"DIRECT"}}),
			node("g2", NodeGroup, GroupData{Group: map[string]any{"name": "自动", "type": "url-test", "interval": 300}}),
			node("p1", NodeProxy, ProxyData{Proxy: map[string]any{"name": "香港 01", "type": "vless", "server": "hk.example.com", "port": 443, "uuid": "u-1"}}),
			node("p2", NodeProxy, ProxyData{Proxy: map[string]any{"name": "落地", "type": "ss", "server": "us.example.com", "port": 8388, "cipher": "aes-256-gcm", "password": "x"}}),
			node("sub", NodeProvider, ProviderData{Name: "机场", Provider: map[string]any{"type": "http", "url": "https://sub.example.com/?token=t", "interval": 3600}}),
			node("rules", NodeRules, RulesData{
				Rules: []MihomoRule{
					{ID: "a", Type: "GEOIP", Payload: "CN", Target: "DIRECT", Params: []string{"no-resolve"}},
					{ID: "b", Type: "RULE-SET", Payload: "ai", Target: "🚀 节点选择"},
					{ID: "c", Type: "MATCH", Target: "自动"},
				},
				RuleProviders: map[string]map[string]any{"ai": {"type": "http", "behavior": "domain", "url": "https://example.com/ai.mrs", "format": "mrs"}},
			}),
		},
		Edges: []Edge{
			link("gw", PortRules, "r1"),
			link("r1", PortNext, "r2"),
			link("r2", PortNext, "r3"),
			link("r1", PortHit, "ep1"),
			link("r1", PortHit, "g1"),
			link("r2", PortHit, "rules"),
			link("ep1", PortVia, "ep2"),
			link("g1", PortMembers, "g2"),
			link("g1", PortMembers, "p1"),
			link("g2", PortMembers, "p1"),
			link("g2", PortMembers, "sub"),
			link("p2", PortDialer, "p1"),
			link("gw", PortFallback, "g2"),
		},
	}
}

func TestCompileSample(t *testing.T) {
	result := Compile(sampleGraph())
	for _, issue := range result.Issues {
		t.Logf("%s %s %s", issue.Level, issue.NodeID, issue.Message)
	}
	if result.HasErrors() {
		t.Fatalf("不应有错误")
	}
	cfg := result.Config
	if len(cfg.Rules) != 3 || cfg.Rules[0].Name != "stripe" || cfg.Rules[2].Action != egress.ActionReject {
		t.Fatalf("规则链不对: %+v", cfg.Rules)
	}
	if !slices.Equal(cfg.Rules[0].Endpoints, []string{"hk-socks", "mihomo.g1"}) {
		t.Fatalf("命中线路不对: %v", cfg.Rules[0].Endpoints)
	}
	if !slices.Equal(cfg.Rules[1].Endpoints, []string{rulesEndpoint}) {
		t.Fatalf("交给分流的线路不对: %v", cfg.Rules[1].Endpoints)
	}
	if cfg.DefaultAction != egress.ActionProxy || !slices.Equal(cfg.DefaultEndpoints, []string{"mihomo.g2"}) {
		t.Fatalf("未命中不对: %s %v", cfg.DefaultAction, cfg.DefaultEndpoints)
	}
	var via string
	for _, endpoint := range cfg.Endpoints {
		if endpoint.Name == "hk-socks" {
			via = endpoint.Via
		}
	}
	if via != "jump" {
		t.Fatalf("经由没有编译进去: %q", via)
	}
	if !slices.Equal(result.Targets, []string{"自动", "🚀 节点选择"}) {
		t.Fatalf("目标不对: %v", result.Targets)
	}
	if !slices.ContainsFunc(result.Issues, func(issue Issue) bool { return issue.NodeID == "loose" }) {
		t.Fatalf("没接入规则链的规则应有提醒")
	}

	p := result.Profile
	if len(p.Proxies) != 2 || len(p.ProxyGroups) != 2 || len(p.ProxyProviders) != 1 || len(p.Rules) != 3 {
		t.Fatalf("核心配置不全: %+v", p)
	}
	for _, proxy := range p.Proxies {
		if proxy["name"] == "落地" && proxy["dialer-proxy"] != "香港 01" {
			t.Fatalf("前置代理不对: %+v", proxy)
		}
	}
	for _, group := range p.ProxyGroups {
		if group["name"] == "🚀 节点选择" {
			members := group["proxies"].([]any)
			if len(members) != 3 || members[2] != "DIRECT" {
				t.Fatalf("成员不对: %v", members)
			}
		}
		if group["name"] == "自动" && group["use"].([]any)[0] != "机场" {
			t.Fatalf("订阅成员不对: %+v", group)
		}
	}
	if p.Rules[0] != "GEOIP,CN,DIRECT,no-resolve" {
		t.Fatalf("分流规则不对: %v", p.Rules)
	}
	if _, err := mihomo.Render(p, mihomo.System{ControllerAddr: "127.0.0.1:1", Secret: "s", InboundPort: 2, Password: "p", TargetPorts: mihomo.PlaceholderPorts(result.Targets)}); err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	gw, err := egress.New(cfg, nil)
	if err != nil {
		t.Fatalf("网关不接受编译结果: %v", err)
	}
	gw.Close()
}

func TestCompileErrors(t *testing.T) {
	graph := sampleGraph()
	graph.Edges = append(graph.Edges, link("g2", PortMembers, "g1"))                                                          // 成环
	graph.Edges = append(graph.Edges, link("r3", PortHit, "p1"))                                                              // 拒绝却连了线
	graph.Nodes = append(graph.Nodes, node("p3", NodeProxy, ProxyData{Proxy: map[string]any{"name": "香港 01", "type": "ss"}})) // 重名
	result := Compile(graph)
	messages := []string{}
	for _, issue := range result.Issues {
		messages = append(messages, issue.Message)
	}
	joined := strings.Join(messages, "\n")
	for _, want := range []string{"成环", "不会用到", "重名"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("缺少「%s」:\n%s", want, joined)
		}
	}
	if !result.HasErrors() {
		t.Fatalf("应有错误")
	}
	if issues := Compile(Graph{}).Issues; len(issues) == 0 {
		t.Fatalf("空画布应提示缺少网关设置")
	}
}

func TestSecretsMaskAndRestore(t *testing.T) {
	original := sampleGraph()
	masked := Mask(original)
	text, _ := json.Marshal(masked)
	for _, secret := range []string{"s3cret", "u-1", "token=t", `"pw"`} {
		if strings.Contains(string(text), secret) {
			t.Fatalf("密钥 %s 没有打码", secret)
		}
	}
	// 复制出来的节点：ID 变了，按同名找回。
	masked.Nodes = append(masked.Nodes, Node{ID: "copy", Type: NodeProxy, Data: masked.Nodes[9].Data})
	restored, issues := Restore(masked, original)
	if len(issues) != 0 {
		t.Fatalf("不应缺密钥: %+v", issues)
	}
	text, _ = json.Marshal(restored)
	if strings.Count(string(text), "u-1") != 2 || !strings.Contains(string(text), "token=t") {
		t.Fatalf("没有还原: %s", text)
	}
	// 新节点用了占位却找不到原值
	masked.Nodes = append(masked.Nodes, node("new", NodeProxy, ProxyData{Proxy: map[string]any{"name": "新", "type": "ss", "password": SecretKept}}))
	if _, issues := Restore(masked, original); len(issues) != 1 {
		t.Fatalf("应提示需要重新填写: %+v", issues)
	}
}

func TestFromConfigRoundTrip(t *testing.T) {
	cfg := egress.Config{
		Enabled:          true,
		DefaultAction:    egress.ActionProxy,
		DefaultEndpoints: []string{"b"},
		Endpoints: []egress.EndpointConfig{
			{Name: "a", Protocol: egress.ProtocolHTTP, Address: "1.1.1.1:3128"},
			{Name: "b", Protocol: egress.ProtocolSOCKS5H, Address: "2.2.2.2:1080", Via: "a"},
		},
		Rules: []egress.RuleConfig{
			{Name: "x", Priority: 5, Endpoints: []string{"a", "b"}, Match: egress.MatchConfig{DomainSuffixes: []string{"x.com"}}},
			{Name: "y", Priority: 1, Action: egress.ActionDirect, Match: egress.MatchConfig{Ports: []int{25}}},
		},
	}
	result := Compile(FromConfig(cfg))
	if result.HasErrors() {
		t.Fatalf("旧配置转画布后应能编译: %+v", result.Issues)
	}
	if result.Config.Rules[0].Name != "y" || !slices.Equal(result.Config.Rules[1].Endpoints, []string{"a", "b"}) {
		t.Fatalf("顺序或线路不对: %+v", result.Config.Rules)
	}
	if result.Config.DefaultAction != egress.ActionProxy || result.Config.Endpoints[1].Via != "a" {
		t.Fatalf("默认动作或经由丢了")
	}
}

func TestImportIntoGraph(t *testing.T) {
	imported, err := mihomo.Import([]byte(`
proxies:
  - {name: A, type: ss, server: a.example.com, port: 1, cipher: aes-256-gcm, password: p}
  - {name: B, type: trojan, server: b.example.com, port: 443, password: p, dialer-proxy: A}
proxy-groups:
  - {name: 节点选择, type: select, proxies: [DIRECT, A, B, 自动]}
  - {name: 自动, type: url-test, proxies: [A, B]}
rules:
  - AND,((DOMAIN-SUFFIX,a.com),(DST-PORT,443)),节点选择
  - MATCH,节点选择
`))
	if err != nil {
		t.Fatal(err)
	}
	base := sampleGraph()
	merged, notes := Import(base, imported.Profile, ImportOptions{Mode: ImportMerge})
	t.Log(notes)
	result := Compile(merged)
	if result.HasErrors() {
		for _, issue := range result.Issues {
			t.Log(issue.Level, issue.Message)
		}
		t.Fatalf("合并后应能编译")
	}
	if len(result.Profile.Proxies) != 4 || len(result.Profile.ProxyGroups) != 4 {
		t.Fatalf("合并数量不对: %d %d", len(result.Profile.Proxies), len(result.Profile.ProxyGroups))
	}

	replaced, _ := Import(base, imported.Profile, ImportOptions{Mode: ImportReplace, RouteFallback: true})
	result = Compile(replaced)
	if result.HasErrors() {
		for _, issue := range result.Issues {
			t.Log(issue.Level, issue.Message)
		}
		t.Fatalf("替换后应能编译")
	}
	if !slices.Equal(result.Config.DefaultEndpoints, []string{rulesEndpoint}) {
		t.Fatalf("未命中应接到分流: %v", result.Config.DefaultEndpoints)
	}
	for _, group := range result.Profile.ProxyGroups {
		if group["name"] == "节点选择" && group["proxies"].([]any)[0] != "DIRECT" {
			t.Fatalf("内置出站顺序丢了: %v", group["proxies"])
		}
	}
	found := false
	for _, rule := range result.Profile.Rules {
		found = found || strings.HasPrefix(rule, "AND,((DOMAIN-SUFFIX,a.com),(DST-PORT,443)),节点选择")
	}
	if !found {
		t.Fatalf("逻辑规则没有保住: %v", result.Profile.Rules)
	}
}

func TestCompileWarnsUDPWithoutCapableTargets(t *testing.T) {
	gateway := DefaultGateway()
	build := func(protocol egress.Protocol, via bool) Result {
		nodes := []Node{
			node("gw", NodeGateway, gateway),
			node("r", NodeRule, RuleData{Name: "quic", Match: egress.MatchConfig{Networks: []string{"udp"}}}),
			node("ep", NodeEndpoint, egress.EndpointConfig{Name: "line", Protocol: protocol, Address: "10.0.0.1:1080", Password: "p", Shadowsocks: egress.ShadowsocksConfig{Method: "aes-128-gcm", Password: "p"}}),
			node("hop", NodeEndpoint, egress.EndpointConfig{Name: "hop", Protocol: egress.ProtocolHTTP, Address: "10.0.0.2:3128"}),
		}
		edges := []Edge{link("gw", PortRules, "r"), link("r", PortHit, "ep")}
		if via {
			edges = append(edges, link("ep", PortVia, "hop"))
		}
		return Compile(Graph{Nodes: nodes, Edges: edges})
	}
	warned := func(result Result) bool {
		return slices.ContainsFunc(result.Issues, func(issue Issue) bool { return issue.NodeID == "r" && strings.Contains(issue.Message, "UDP") })
	}
	if !warned(build(egress.ProtocolHTTP, false)) {
		t.Fatalf("HTTP 线路承载不了 UDP，应提醒")
	}
	if warned(build(egress.ProtocolSOCKS5H, false)) {
		t.Fatalf("SOCKS5 能承载 UDP，不该提醒")
	}
	if !warned(build(egress.ProtocolShadowsocks, true)) {
		t.Fatalf("经 HTTP 跳板的 Shadowsocks 承载不了 UDP，应提醒")
	}
	if warned(build(egress.ProtocolTrojan, true)) {
		t.Fatalf("Trojan 的 UDP 在 TLS 流里，经 HTTP 跳板也行")
	}
}
