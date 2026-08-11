package egress

import (
	"testing"
)

func testGateway(t *testing.T, cfg Config) *Gateway {
	t.Helper()
	gw, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("构造网关失败: %v", err)
	}
	t.Cleanup(gw.Close)
	return gw
}

func baseConfig() Config {
	return Config{
		Enabled:       true,
		DefaultAction: ActionDirect,
		Endpoints: []EndpointConfig{
			{Name: "hk", Protocol: ProtocolSOCKS5H, Address: "10.0.0.1:1080"},
			{Name: "us", Protocol: ProtocolHTTP, Address: "10.0.0.2:3128"},
			{Name: "fallback", Protocol: ProtocolDirect},
		},
	}
}

func TestRouteBySuffixAndPriority(t *testing.T) {
	cfg := baseConfig()
	cfg.Rules = []RuleConfig{
		{
			Name: "内网直连", Priority: 1, Action: ActionDirect,
			Match: MatchConfig{DomainSuffixes: []string{"internal.corp"}},
		},
		{
			Name: "谷歌系走香港", Priority: 10, Action: ActionProxy, Endpoints: []string{"hk"},
			Match: MatchConfig{
				DomainSuffixes:        []string{"*.google.com", "googleapis.com"},
				ExcludeDomainSuffixes: []string{"cn.google.com"},
			},
		},
		{
			Name: "广告拒绝", Priority: 20, Action: ActionReject,
			Match: MatchConfig{DomainSuffixes: []string{"doubleclick.net"}},
		},
	}
	gw := testGateway(t, cfg)

	cases := []struct {
		host     string
		action   Action
		endpoint string
	}{
		{"www.google.com", ActionProxy, "hk"},
		{"googleapis.com", ActionProxy, "hk"},
		// 例外后缀优先于命中后缀。
		{"cn.google.com", ActionDirect, ""},
		{"ads.doubleclick.net", ActionReject, ""},
		{"api.internal.corp", ActionDirect, ""},
		{"example.com", ActionDirect, ""},
	}
	for _, tc := range cases {
		decision := gw.Route(Target{Host: tc.host, Port: 443, Scheme: "https"})
		if decision.Action != tc.action {
			t.Errorf("%s: action = %s，期望 %s", tc.host, decision.Action, tc.action)
			continue
		}
		got := ""
		if decision.Endpoint != nil {
			got = decision.Endpoint.Name()
		}
		if got != tc.endpoint {
			t.Errorf("%s: endpoint = %q，期望 %q", tc.host, got, tc.endpoint)
		}
	}
}

func TestRouteDimensionsAreConjunctive(t *testing.T) {
	cfg := baseConfig()
	cfg.Rules = []RuleConfig{{
		Name: "只放 443", Action: ActionProxy, Endpoints: []string{"hk"},
		Match: MatchConfig{DomainSuffixes: []string{"example.com"}, Ports: []int{443}},
	}}
	gw := testGateway(t, cfg)

	if got := gw.Route(Target{Host: "a.example.com", Port: 443, Scheme: "https"}); got.Action != ActionProxy {
		t.Errorf("443 应命中代理，实际 %s", got.Action)
	}
	// 域名对但端口不对 —— 不同维度是「与」，不能放行。
	if got := gw.Route(Target{Host: "a.example.com", Port: 8080, Scheme: "tcp"}); got.Action != ActionDirect {
		t.Errorf("8080 不应命中代理，实际 %s", got.Action)
	}
}

func TestRouteByProfile(t *testing.T) {
	cfg := baseConfig()
	cfg.Rules = []RuleConfig{
		{
			Name: "支付走美国", Priority: 1, Action: ActionProxy, Endpoints: []string{"us"},
			Match: MatchConfig{DomainSuffixes: []string{"example.com"}, Profiles: []string{"payment.*"}},
		},
		{
			Name: "其余走香港", Priority: 2, Action: ActionProxy, Endpoints: []string{"hk"},
			Match: MatchConfig{DomainSuffixes: []string{"example.com"}},
		},
	}
	gw := testGateway(t, cfg)

	if got := gw.Route(Target{Host: "api.example.com", Port: 443, Profile: "payment.stripe"}); got.Endpoint.Name() != "us" {
		t.Errorf("payment.stripe 应走 us，实际 %s", got.Endpoint.Name())
	}
	if got := gw.Route(Target{Host: "api.example.com", Port: 443, Profile: "storage.s3"}); got.Endpoint.Name() != "hk" {
		t.Errorf("storage.s3 应走 hk，实际 %s", got.Endpoint.Name())
	}
}

func TestRouteByCIDR(t *testing.T) {
	cfg := baseConfig()
	cfg.Rules = []RuleConfig{{
		Name: "指定网段出海", Action: ActionProxy, Endpoints: []string{"hk"},
		Match: MatchConfig{CIDRs: []string{"203.0.113.0/24"}},
	}}
	gw := testGateway(t, cfg)

	if got := gw.Route(Target{Host: "203.0.113.9", Port: 443}); got.Action != ActionProxy {
		t.Errorf("网段内 IP 应走代理，实际 %s", got.Action)
	}
	if got := gw.Route(Target{Host: "198.51.100.9", Port: 443}); got.Action != ActionDirect {
		t.Errorf("网段外 IP 应直连，实际 %s", got.Action)
	}
	// 域名目标没有字面 IP，不该命中 CIDR 规则。
	if got := gw.Route(Target{Host: "example.com", Port: 443}); got.Action != ActionDirect {
		t.Errorf("域名不应命中 CIDR 规则，实际 %s", got.Action)
	}
}

func TestDefaultActionProxyRequiresEndpoints(t *testing.T) {
	cfg := baseConfig()
	cfg.DefaultAction = ActionProxy
	if _, err := New(cfg, nil); err == nil {
		t.Fatal("默认动作为 proxy 却没有默认端点，应校验失败")
	}

	cfg.DefaultEndpoints = []string{"hk"}
	gw := testGateway(t, cfg)
	decision := gw.Route(Target{Host: "anything.example", Port: 443})
	if decision.Action != ActionProxy || decision.Endpoint.Name() != "hk" {
		t.Fatalf("全局出海模式应默认走 hk，实际 %s/%v", decision.Action, decision.Endpoint)
	}
}

func TestFailoverSkipsUnhealthyEndpoint(t *testing.T) {
	cfg := baseConfig()
	cfg.Health.AllowUnhealthy = boolPtr(false)
	cfg.Rules = []RuleConfig{{
		Name: "出海", Action: ActionProxy, Endpoints: []string{"hk", "us", "fallback"},
		Match: MatchConfig{DomainSuffixes: []string{"example.com"}},
	}}
	gw := testGateway(t, cfg)

	hk, _ := gw.Endpoint("hk")
	hk.recordProbe(0, errProbe, 1, 1)

	decision := gw.Route(Target{Host: "a.example.com", Port: 443})
	if decision.Endpoint.Name() != "us" {
		t.Fatalf("hk 不健康时应切到 us，实际 %s", decision.Endpoint.Name())
	}

	us, _ := gw.Endpoint("us")
	us.recordProbe(0, errProbe, 1, 1)
	decision = gw.Route(Target{Host: "a.example.com", Port: 443})
	if decision.Endpoint.Name() != "fallback" {
		t.Fatalf("代理全挂时应落到 direct 端点，实际 %s", decision.Endpoint.Name())
	}
}

func TestNoAvailableEndpointFailsInsteadOfSilentDirect(t *testing.T) {
	cfg := baseConfig()
	cfg.Health.AllowUnhealthy = boolPtr(false)
	cfg.Rules = []RuleConfig{{
		Name: "出海", Action: ActionProxy, Endpoints: []string{"hk"},
		Match: MatchConfig{DomainSuffixes: []string{"example.com"}},
	}}
	gw := testGateway(t, cfg)
	hk, _ := gw.Endpoint("hk")
	hk.recordProbe(0, errProbe, 1, 1)

	decision := gw.Route(Target{Host: "a.example.com", Port: 443})
	// 本该出海的流量悄悄走直连，是一种会被误当成功的失败。
	if decision.Err == nil {
		t.Fatal("没有可用端点时应返回错误而不是降级直连")
	}
	if decision.Action != ActionProxy {
		t.Fatalf("动作应保持 proxy，实际 %s", decision.Action)
	}
}

func TestDisabledGatewayRoutesEverythingDirect(t *testing.T) {
	cfg := baseConfig()
	cfg.Enabled = false
	cfg.Rules = []RuleConfig{{
		Name: "出海", Action: ActionProxy, Endpoints: []string{"hk"},
		Match: MatchConfig{DomainSuffixes: []string{"example.com"}},
	}}
	gw := testGateway(t, cfg)
	if got := gw.Route(Target{Host: "a.example.com", Port: 443}); got.Action != ActionDirect {
		t.Fatalf("网关关闭时应全部直连，实际 %s", got.Action)
	}
}

func TestExplainDoesNotDisturbRouting(t *testing.T) {
	cfg := baseConfig()
	cfg.Rules = []RuleConfig{{
		Name: "轮询", Action: ActionProxy, Endpoints: []string{"hk", "us"}, Strategy: StrategyRoundRobin,
		Match: MatchConfig{DomainSuffixes: []string{"example.com"}},
	}}
	gw := testGateway(t, cfg)

	first := gw.Route(Target{Host: "a.example.com", Port: 443}).Endpoint.Name()
	for range 5 {
		gw.Explain(Target{Host: "a.example.com", Port: 443})
	}
	second := gw.Route(Target{Host: "a.example.com", Port: 443}).Endpoint.Name()
	if first == second {
		t.Fatalf("轮询游标被 Explain 推进了：两次真实路由都得到 %s", first)
	}

	explanation := gw.Explain(Target{Host: "a.example.com", Port: 443})
	if explanation.Rule != "轮询" || len(explanation.Evaluated) != 1 {
		t.Fatalf("解释结果不符合预期: %+v", explanation)
	}
}

func TestReloadKeepsOldConfigOnInvalidInput(t *testing.T) {
	gw := testGateway(t, baseConfig())
	before, _ := gw.ReloadMeta()

	bad := baseConfig()
	bad.Rules = []RuleConfig{{Name: "坏规则", Action: ActionProxy, Endpoints: []string{"不存在"},
		Match: MatchConfig{DomainSuffixes: []string{"example.com"}}}}
	if err := gw.Reload(bad); err == nil {
		t.Fatal("引用不存在端点的配置应被拒绝")
	}
	after, _ := gw.ReloadMeta()
	if before != after {
		t.Fatalf("重载失败不应改变生效版本：%d → %d", before, after)
	}
}

func TestViaChainCycleRejected(t *testing.T) {
	cfg := baseConfig()
	cfg.Endpoints = []EndpointConfig{
		{Name: "a", Protocol: ProtocolSOCKS5H, Address: "10.0.0.1:1080", Via: "b"},
		{Name: "b", Protocol: ProtocolSOCKS5H, Address: "10.0.0.2:1080", Via: "a"},
	}
	if _, err := New(cfg, nil); err == nil {
		t.Fatal("via 成环应被拒绝")
	}
}

var errProbe = errProbeType{}

type errProbeType struct{}

func (errProbeType) Error() string { return "探测失败（测试）" }
