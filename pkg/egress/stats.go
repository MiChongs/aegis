package egress

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// Stats 网关运行态快照，供管理端与监控聚合使用。
type Stats struct {
	Enabled       bool           `json:"enabled"`
	Source        string         `json:"source,omitempty"`
	Version       uint64         `json:"version"`
	LoadedAt      time.Time      `json:"loadedAt"`
	DefaultAction Action         `json:"defaultAction"`
	Routed        uint64         `json:"routed"`
	RoutedUDP     uint64         `json:"routedUdp"`
	Proxied       uint64         `json:"proxied"`
	Direct        uint64         `json:"direct"`
	Rejected      uint64         `json:"rejected"`
	Failed        uint64         `json:"failed"`
	Endpoints     []EndpointStat `json:"endpoints"`
	Rules         []RuleStat     `json:"rules"`
}

// EndpointStat 单端点运行态。
type EndpointStat struct {
	Name                string     `json:"name"`
	Protocol            Protocol   `json:"protocol"`
	Address             string     `json:"address,omitempty"`
	Region              string     `json:"region,omitempty"`
	Remark              string     `json:"remark,omitempty"`
	Via                 string     `json:"via,omitempty"`
	Chain               []string   `json:"chain,omitempty"`
	Weight              int        `json:"weight"`
	Enabled             bool       `json:"enabled"`
	Healthy             bool       `json:"healthy"`
	Dials               uint64     `json:"dials"`
	Successes           uint64     `json:"successes"`
	Failures            uint64     `json:"failures"`
	BytesIn             uint64     `json:"bytesIn"`
	BytesOut            uint64     `json:"bytesOut"`
	LatencyMS           int64      `json:"latencyMs"`
	ConsecutiveFailures int        `json:"consecutiveFailures"`
	LastError           string     `json:"lastError,omitempty"`
	LastCheckedAt       *time.Time `json:"lastCheckedAt,omitempty"`
	CooldownUntil       *time.Time `json:"cooldownUntil,omitempty"`

	// UDP：是否承载、怎么承载、单独的健康与延迟、会话与报文数。
	UDP              bool         `json:"udp"`
	UDPTransport     UDPTransport `json:"udpTransport,omitempty"`
	UDPHealthy       bool         `json:"udpHealthy"`
	UDPLatencyMS     int64        `json:"udpLatencyMs"`
	UDPLastError     string       `json:"udpLastError,omitempty"`
	UDPLastCheckedAt *time.Time   `json:"udpLastCheckedAt,omitempty"`
	UDPSessions      uint64       `json:"udpSessions"`
	PacketsIn        uint64       `json:"packetsIn"`
	PacketsOut       uint64       `json:"packetsOut"`
}

// RuleStat 单规则运行态。
type RuleStat struct {
	Name        string   `json:"name"`
	Enabled     bool     `json:"enabled"`
	Priority    int      `json:"priority"`
	Action      Action   `json:"action"`
	Strategy    Strategy `json:"strategy,omitempty"`
	Endpoints   []string `json:"endpoints,omitempty"`
	SuffixCount int      `json:"suffixCount"`
	Networks    []string `json:"networks,omitempty"`
	Matched     uint64   `json:"matched"`
	Remark      string   `json:"remark,omitempty"`
}

// Stats 返回当前运行态。
func (g *Gateway) Stats() Stats {
	snap := g.snap.Load()
	out := Stats{
		Routed:    g.counters.routed.Load(),
		RoutedUDP: g.counters.routedUDP.Load(),
		Proxied:   g.counters.proxied.Load(),
		Direct:    g.counters.direct.Load(),
		Rejected:  g.counters.rejected.Load(),
		Failed:    g.counters.failed.Load(),
	}
	if snap == nil {
		return out
	}
	out.Enabled = snap.cfg.Enabled
	out.Source = snap.cfg.Source
	out.Version = snap.version
	out.LoadedAt = snap.loadedAt
	out.DefaultAction = snap.cfg.DefaultAction

	out.Endpoints = make([]EndpointStat, 0, len(snap.endpoints))
	for _, endpoint := range snap.endpoints {
		healthy, latency, failures, lastErr, checkedAt, cooldownUntil := endpoint.snapshotHealth()
		stat := EndpointStat{
			Name:                endpoint.cfg.Name,
			Protocol:            endpoint.cfg.Protocol,
			Address:             endpoint.cfg.Address,
			Region:              endpoint.cfg.Region,
			Remark:              endpoint.cfg.Remark,
			Via:                 endpoint.cfg.Via,
			Chain:               endpoint.chain(),
			Weight:              endpoint.cfg.Weight,
			Enabled:             endpoint.enabled(),
			Healthy:             healthy,
			Dials:               endpoint.stats.dials.Load(),
			Successes:           endpoint.stats.successes.Load(),
			Failures:            endpoint.stats.failures.Load(),
			BytesIn:             endpoint.stats.bytesIn.Load(),
			BytesOut:            endpoint.stats.bytesOut.Load(),
			LatencyMS:           latency.Milliseconds(),
			ConsecutiveFailures: failures,
			LastError:           lastErr,
		}
		if !checkedAt.IsZero() {
			ts := checkedAt
			stat.LastCheckedAt = &ts
		}
		if cooldownUntil.After(time.Now()) {
			ts := cooldownUntil
			stat.CooldownUntil = &ts
		}
		if endpoint.SupportsUDP() {
			udpHealthy, udpLatency, _, udpErr, udpChecked, _ := endpoint.udpHealth.snapshot()
			stat.UDP = true
			stat.UDPTransport = ProtocolUDP(endpoint.cfg.Protocol)
			stat.UDPHealthy = udpHealthy
			stat.UDPLatencyMS = udpLatency.Milliseconds()
			stat.UDPLastError = udpErr
			if !udpChecked.IsZero() {
				ts := udpChecked
				stat.UDPLastCheckedAt = &ts
			}
		}
		stat.UDPSessions = endpoint.stats.udpSessions.Load()
		stat.PacketsIn = endpoint.stats.packetsIn.Load()
		stat.PacketsOut = endpoint.stats.packetsOut.Load()
		out.Endpoints = append(out.Endpoints, stat)
	}

	out.Rules = make([]RuleStat, 0, len(snap.rules))
	for _, rule := range snap.rules {
		stat := RuleStat{
			Name:      rule.cfg.Name,
			Enabled:   boolValue(rule.cfg.Enabled, true),
			Priority:  rule.cfg.Priority,
			Action:    rule.cfg.Action,
			Strategy:  rule.cfg.Strategy,
			Endpoints: rule.cfg.Endpoints,
			Matched:   rule.matched.Load(),
			Remark:    rule.cfg.Remark,
			Networks:  rule.cfg.Match.Networks,
		}
		if rule.suffix != nil {
			stat.SuffixCount = rule.suffix.size
		}
		out.Rules = append(out.Rules, stat)
	}
	return out
}

// RuleEvaluation 单条规则的匹配过程，用于向管理员解释判定结果。
type RuleEvaluation struct {
	Rule    string `json:"rule"`
	Matched bool   `json:"matched"`
	Reason  string `json:"reason,omitempty"`
}

// Explanation 「这个域名会怎么出去」的完整解释。
type Explanation struct {
	Host      string           `json:"host"`
	Port      int              `json:"port"`
	Scheme    string           `json:"scheme"`
	Network   string           `json:"network"`
	ProfileID string           `json:"profile,omitempty"`
	Action    Action           `json:"action"`
	Rule      string           `json:"rule"`
	Reason    string           `json:"reason,omitempty"`
	Endpoint  string           `json:"endpoint,omitempty"`
	Chain     []string         `json:"chain,omitempty"`
	Error     string           `json:"error,omitempty"`
	Evaluated []RuleEvaluation `json:"evaluated"`
}

// Explain 复用与真实流量完全相同的匹配逻辑，回答「这个目标会走哪条线」。
//
// 它不计入统计、也不推进轮询游标——解释一次不应该改变下一条真实连接的去向。
func (g *Gateway) Explain(target Target) Explanation {
	target = target.normalized()
	out := Explanation{
		Host: target.Host, Port: target.Port, Scheme: target.Scheme, Network: target.Network, ProfileID: target.Profile,
		Evaluated: []RuleEvaluation{},
	}
	snap := g.snap.Load()
	if snap == nil || !snap.cfg.Enabled {
		out.Action, out.Rule, out.Reason = ActionDirect, "disabled", "网关未启用"
		return out
	}

	ip, hasIP := netip.Addr{}, false
	if parsed, err := netip.ParseAddr(target.Host); err == nil {
		ip, hasIP = parsed.Unmap(), true
	}

	var scratch atomic.Uint64
	for _, rule := range snap.rules {
		reason, ok := rule.match(target, ip, hasIP)
		out.Evaluated = append(out.Evaluated, RuleEvaluation{Rule: rule.cfg.Name, Matched: ok, Reason: reason})
		if !ok {
			continue
		}
		fillExplanation(&out, decide(snap, rule.cfg.Name, reason, rule.cfg.Action, rule.endpoints, rule.cfg.Strategy, &scratch, target.Network))
		return out
	}
	fillExplanation(&out, decide(snap, "default", "未命中任何规则", snap.cfg.DefaultAction, snap.defaultEndpoints, snap.cfg.DefaultStrategy, &scratch, target.Network))
	return out
}

func fillExplanation(out *Explanation, decision Decision) {
	out.Action = decision.Action
	out.Rule = decision.Rule
	out.Reason = decision.Reason
	out.Chain = decision.Chain
	if decision.Endpoint != nil {
		out.Endpoint = decision.Endpoint.Name()
	}
	if decision.Err != nil {
		out.Error = decision.Err.Error()
	}
}

// TestRequest 连通性自测入参。
type TestRequest struct {
	// URL 目标地址；留空时用健康检查的探测地址。
	URL string `json:"url,omitempty"`
	// Endpoint 指定端点则绕过规则直连该端点，用于验证单条线路是否可用。
	Endpoint string `json:"endpoint,omitempty"`
	// Profile 模拟某个调用方，验证 Profiles 维度的规则是否如预期生效。
	Profile   string `json:"profile,omitempty"`
	TimeoutMS int    `json:"timeoutMs,omitempty"`
	// Network 为 udp 时做 UDP 自测：URL 是 DNS 服务器 host:port（留空用 UDP 探测服务器），
	// 经路由表（或指定端点）向它查询 Name。
	Network string `json:"network,omitempty"`
	Name    string `json:"name,omitempty"`
}

// TestResult 自测结果。Body 截断保留，方便把 URL 指向「查询出口 IP」类服务
// 直接确认落地在境外。
type TestResult struct {
	OK         bool     `json:"ok"`
	URL        string   `json:"url"`
	Action     Action   `json:"action"`
	Rule       string   `json:"rule,omitempty"`
	Reason     string   `json:"reason,omitempty"`
	Endpoint   string   `json:"endpoint,omitempty"`
	Chain      []string `json:"chain,omitempty"`
	StatusCode int      `json:"statusCode,omitempty"`
	LatencyMS  int64    `json:"latencyMs"`
	Body       string   `json:"body,omitempty"`
	Error      string   `json:"error,omitempty"`
	Network    string   `json:"network"`
	// Answers UDP 自测（DNS 查询）的解析结果。
	Answers []string `json:"answers,omitempty"`
}

const testBodyLimit = 512

// Test 实跑一次出站请求。它走的是与业务完全相同的 Transport 路径，
// 因此「自测通过但业务失败」不会因为两套代码而发生。
func (g *Gateway) Test(ctx context.Context, req TestRequest) TestResult {
	if networkKind(strings.ToLower(strings.TrimSpace(req.Network))) == "udp" {
		return g.testUDP(ctx, req)
	}
	snap := g.snap.Load()
	targetURL := strings.TrimSpace(req.URL)
	if targetURL == "" && snap != nil {
		targetURL = snap.cfg.Health.ProbeURL
	}
	if targetURL == "" {
		targetURL = DefaultProbeURL
	}
	result := TestResult{URL: targetURL, Network: "tcp"}

	parsed, err := url.Parse(targetURL)
	if err != nil || parsed.Host == "" {
		result.Error = "URL 无效"
		return result
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		result.Error = "仅支持 http / https"
		return result
	}

	timeout := time.Duration(req.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	testCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var client *http.Client
	if name := strings.TrimSpace(req.Endpoint); name != "" {
		endpoint, ok := g.Endpoint(name)
		if !ok {
			result.Error = fmt.Sprintf("端点 %s 不存在", name)
			return result
		}
		result.Action, result.Rule, result.Reason = ActionProxy, "manual", "指定端点"
		result.Endpoint, result.Chain = endpoint.Name(), endpoint.chain()
		client = &http.Client{Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				return endpoint.dial(ctx, network, address, false, 0, 0)
			},
			DisableKeepAlives:   true,
			TLSHandshakeTimeout: DefaultTLSHandshakeTimeout,
		}}
	} else {
		explanation := g.Explain(Target{
			Host: parsed.Hostname(), Port: portFromURL(parsed), Scheme: parsed.Scheme, Profile: req.Profile,
		})
		result.Action, result.Rule, result.Reason = explanation.Action, explanation.Rule, explanation.Reason
		result.Endpoint, result.Chain = explanation.Endpoint, explanation.Chain
		if explanation.Error != "" {
			result.Error = explanation.Error
			return result
		}
		client = g.client(Profile{Name: req.Profile, Timeout: timeout, DisableKeepAlives: true}, false)
	}
	defer client.CloseIdleConnections()

	httpReq, err := http.NewRequestWithContext(testCtx, http.MethodGet, targetURL, nil)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	start := time.Now()
	resp, err := client.Do(httpReq)
	result.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, testBodyLimit))
	result.StatusCode = resp.StatusCode
	result.Body = strings.TrimSpace(string(body))
	result.OK = resp.StatusCode < 400
	if !result.OK {
		result.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return result
}

// testUDP 经路由表（或指定端点）向 DNS 服务器发一次查询。
func (g *Gateway) testUDP(ctx context.Context, req TestRequest) TestResult {
	snap := g.snap.Load()
	server := strings.TrimSpace(req.URL)
	server = strings.TrimPrefix(strings.TrimPrefix(server, "udp://"), "dns://")
	name := strings.TrimSpace(req.Name)
	if snap != nil {
		if server == "" {
			server = snap.cfg.Health.UDPProbeServer
		}
		if name == "" {
			name = snap.cfg.Health.UDPProbeName
		}
	}
	if server == "" {
		server = DefaultUDPProbeServer
	}
	if name == "" {
		name = DefaultUDPProbeName
	}
	result := TestResult{URL: server, Network: "udp"}
	host, port, err := splitTarget(server)
	if err != nil {
		result.Error = "DNS 服务器必须是 host:port"
		return result
	}
	timeout := time.Duration(req.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	testCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var conn net.Conn
	start := time.Now()
	if endpointName := strings.TrimSpace(req.Endpoint); endpointName != "" {
		endpoint, ok := g.Endpoint(endpointName)
		if !ok {
			result.Error = fmt.Sprintf("端点 %s 不存在", endpointName)
			return result
		}
		result.Action, result.Rule, result.Reason = ActionProxy, "manual", "指定端点"
		result.Endpoint, result.Chain = endpoint.Name(), endpoint.chain()
		if !endpoint.SupportsUDP() {
			result.Error = fmt.Sprintf("端点 %s 不能承载 UDP", endpointName)
			return result
		}
		conn, err = endpoint.dial(testCtx, "udp", server, false, 0, 0)
	} else {
		explanation := g.Explain(Target{Host: host, Port: port, Network: "udp", Profile: req.Profile})
		result.Action, result.Rule, result.Reason = explanation.Action, explanation.Rule, explanation.Reason
		result.Endpoint, result.Chain = explanation.Endpoint, explanation.Chain
		if explanation.Error != "" {
			result.Error = explanation.Error
			return result
		}
		conn, err = g.dialWithProfile(testCtx, "udp", server, Profile{Name: req.Profile})
	}
	if err != nil {
		result.LatencyMS = time.Since(start).Milliseconds()
		result.Error = err.Error()
		return result
	}
	defer conn.Close()
	answers, err := dnsQuery(testCtx, conn, name)
	result.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		result.Error = err.Error()
		return result
	}
	result.OK = true
	result.Answers = answers
	result.Body = fmt.Sprintf("%s → %s", name, strings.Join(answers, "、"))
	return result
}
