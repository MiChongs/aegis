package egress

import (
	"strings"
	"testing"
)

func TestParseEndpointsDSL(t *testing.T) {
	raw := `
# 香港落地
hk=socks5h://alice:s3cret@10.0.0.1:1080?region=hk&weight=3
us=http://proxy.us.internal:3128?forward=1
jump=ssh://ops@bastion.internal:22
edge=trojan://tr0janpass@edge.example.com:443?sni=www.example.com
relay=ss://aes-128-gcm:sspass@relay.example.com:8388?via=jump
bare=1.2.3.4:8080
`
	endpoints, err := ParseEndpoints(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(endpoints) != 6 {
		t.Fatalf("端点数 = %d，期望 6", len(endpoints))
	}

	byName := map[string]EndpointConfig{}
	for _, item := range endpoints {
		byName[item.Name] = item
	}

	hk := byName["hk"]
	if hk.Protocol != ProtocolSOCKS5H || hk.Address != "10.0.0.1:1080" ||
		hk.Username != "alice" || hk.Password != "s3cret" || hk.Region != "hk" || hk.Weight != 3 {
		t.Errorf("hk 解析错误: %+v", hk)
	}
	if !byName["us"].HTTPForwardMode {
		t.Error("us 应开启 forward 模式")
	}
	if byName["jump"].SSH.User != "ops" || byName["jump"].Address != "bastion.internal:22" {
		t.Errorf("jump 解析错误: %+v", byName["jump"])
	}
	edge := byName["edge"]
	if edge.Password != "tr0janpass" || edge.TLS.ServerName != "www.example.com" || !edge.TLS.Enabled {
		t.Errorf("edge 解析错误: %+v", edge)
	}
	relay := byName["relay"]
	if relay.Shadowsocks.Method != "aes-128-gcm" || relay.Shadowsocks.Password != "sspass" || relay.Via != "jump" {
		t.Errorf("relay 解析错误: %+v", relay)
	}
	// 裸 host:port 省略 scheme 时按 http 代理处理。
	if byName["bare"].Protocol != ProtocolHTTP || byName["bare"].Address != "1.2.3.4:8080" {
		t.Errorf("bare 解析错误: %+v", byName["bare"])
	}
}

func TestParseEndpointNameDerivedFromHost(t *testing.T) {
	// 不写名字时从主机名推导，让 EGRESS_ENDPOINTS 里只写一条 URL 也能用。
	endpoints, err := ParseEndpoints("socks5h://gw.example.com:1080")
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].Name != "gw.example.com" {
		t.Fatalf("名字推导失败: %+v", endpoints)
	}
}

func TestParseEndpointsJSON(t *testing.T) {
	raw := `[{"name":"hk","protocol":"socks5h","address":"10.0.0.1:1080","weight":2}]`
	endpoints, err := ParseEndpoints(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].Name != "hk" || endpoints[0].Weight != 2 {
		t.Fatalf("JSON 解析结果不符: %+v", endpoints)
	}
}

func TestParseRulesDSL(t *testing.T) {
	raw := `hk|us@round_robin=*.google.com, *.googleapis.com
direct=*.aliyuncs.com
reject=*.doubleclick.net`

	rules, err := ParseRules(raw)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(rules) != 3 {
		t.Fatalf("规则数 = %d，期望 3", len(rules))
	}
	if rules[0].Action != ActionProxy || strings.Join(rules[0].Endpoints, "|") != "hk|us" ||
		rules[0].Strategy != StrategyRoundRobin || len(rules[0].Match.DomainSuffixes) != 2 {
		t.Errorf("第一条规则解析错误: %+v", rules[0])
	}
	if rules[1].Action != ActionDirect || len(rules[1].Endpoints) != 0 {
		t.Errorf("direct 规则解析错误: %+v", rules[1])
	}
	if rules[2].Action != ActionReject {
		t.Errorf("reject 规则解析错误: %+v", rules[2])
	}
	// 声明顺序即优先级。
	if rules[0].Priority >= rules[1].Priority {
		t.Error("优先级应按声明顺序递增")
	}
}

func TestParseRulesRejectsMalformed(t *testing.T) {
	if _, err := ParseRules("hk"); err == nil {
		t.Error("缺少 '=' 应报错")
	}
	if _, err := ParseRules("hk="); err == nil {
		t.Error("没有域名后缀应报错")
	}
	if _, err := ParseEndpoints("bad=ftp://host:21"); err == nil {
		t.Error("不支持的协议应报错")
	}
}

func TestConfigNormalizeIsIdempotent(t *testing.T) {
	cfg := Config{
		Enabled: true,
		Endpoints: []EndpointConfig{
			{Name: "hk", Protocol: "SOCKS", Address: "10.0.0.1:1080"},
		},
		Rules: []RuleConfig{
			{Priority: 5, Endpoints: []string{"hk"}, Match: MatchConfig{DomainSuffixes: []string{"*.Google.COM", ".google.com"}}},
			{Priority: 1, Action: ActionDirect, Match: MatchConfig{DomainSuffixes: []string{"internal.corp"}}},
		},
	}
	once := cfg.Normalize()
	twice := once.Normalize()

	if once.Endpoints[0].Protocol != ProtocolSOCKS5H {
		t.Errorf("socks 简写应归一到 socks5h，实际 %s", once.Endpoints[0].Protocol)
	}
	// 大小写与通配前缀不同的两条后缀归一后是同一条，应去重。
	proxyRule := once.Rules[1]
	if len(proxyRule.Match.DomainSuffixes) != 1 || proxyRule.Match.DomainSuffixes[0] != "google.com" {
		t.Errorf("后缀归一/去重失败: %+v", proxyRule.Match.DomainSuffixes)
	}
	if once.Rules[0].Priority != 1 {
		t.Errorf("规则应按优先级升序排列，实际首条 priority=%d", once.Rules[0].Priority)
	}
	if len(twice.Rules) != len(once.Rules) || twice.Rules[0].Name != once.Rules[0].Name {
		t.Error("Normalize 应当幂等")
	}
	if err := once.Validate(); err != nil {
		t.Fatalf("规范化后的配置应通过校验: %v", err)
	}
}

func TestValidateRejectsBadConfig(t *testing.T) {
	cases := map[string]Config{
		"端点重名": {Endpoints: []EndpointConfig{
			{Name: "a", Protocol: ProtocolHTTP, Address: "1.1.1.1:8080"},
			{Name: "a", Protocol: ProtocolHTTP, Address: "1.1.1.2:8080"},
		}},
		"缺少端口": {Endpoints: []EndpointConfig{{Name: "a", Protocol: ProtocolHTTP, Address: "1.1.1.1"}}},
		"未知协议": {Endpoints: []EndpointConfig{{Name: "a", Protocol: "vmess", Address: "1.1.1.1:8080"}}},
		"ss 缺口令": {Endpoints: []EndpointConfig{
			{Name: "a", Protocol: ProtocolShadowsocks, Address: "1.1.1.1:8388"},
		}},
		"规则无条件": {Rules: []RuleConfig{{Name: "r", Action: ActionDirect}}},
		"正则非法": {Rules: []RuleConfig{{Name: "r", Action: ActionDirect,
			Match: MatchConfig{DomainRegexps: []string{"([a-z"}}}}},
	}
	for name, cfg := range cases {
		if err := cfg.Normalize().Validate(); err == nil {
			t.Errorf("%s：应校验失败", name)
		}
	}
}
