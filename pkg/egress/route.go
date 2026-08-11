package egress

import (
	"fmt"
	"math/rand/v2"
	"net/netip"
	"regexp"
	"strings"
	"sync/atomic"
	"time"
)

// Target 一次出站连接的目标描述。
//
// Profile 是调用方标识（payment.stripe / storage.s3 / oauth.google …），
// 它让「同一个域名，不同业务走不同线路」成为可能——例如支付回调必须走固定出口 IP，
// 而对象存储更在意带宽。
type Target struct {
	Host    string
	Port    int
	Scheme  string // http / https / tcp
	Profile string
}

func (t Target) normalized() Target {
	t.Host = normalizeHost(t.Host)
	t.Scheme = strings.ToLower(strings.TrimSpace(t.Scheme))
	if t.Scheme == "" {
		t.Scheme = "tcp"
	}
	t.Profile = strings.ToLower(strings.TrimSpace(t.Profile))
	return t
}

// String 便于日志展示。
func (t Target) String() string {
	if t.Port > 0 {
		return fmt.Sprintf("%s:%d", t.Host, t.Port)
	}
	return t.Host
}

// Decision 一次路由判定的结果。
type Decision struct {
	Action Action
	// Rule 命中的规则名；未命中任何规则时为 "default"。
	Rule string
	// Reason 命中原因（如 "domainSuffix=google.com"），只用于展示与排障。
	Reason   string
	Endpoint *Endpoint
	// Chain 端点链（含 via），从落地端点到最外层跳板。
	Chain []string
	// HTTPForward 该请求应使用 absolute-URI 转发而非 CONNECT 隧道。
	HTTPForward bool
	// Err 判定本身失败（如规则要求走代理但无可用端点）。
	Err error
}

// compiledRule 是 RuleConfig 的可执行形态：所有字符串匹配都预编译成
// trie / map / 正则，避免每条连接重复解析。
type compiledRule struct {
	cfg           RuleConfig
	suffix        *suffixTrie
	excludeSuffix *suffixTrie
	domains       map[string]struct{}
	keywords      []string
	regexps       []*regexp.Regexp
	prefixes      []netip.Prefix
	ports         map[int]struct{}
	schemes       map[string]struct{}
	profiles      []string
	endpoints     []*Endpoint

	matched atomic.Uint64
	rr      atomic.Uint64
}

func compileRules(cfg Config, endpoints map[string]*Endpoint) ([]*compiledRule, error) {
	out := make([]*compiledRule, 0, len(cfg.Rules))
	for _, item := range cfg.Rules {
		if !boolValue(item.Enabled, true) {
			continue
		}
		rule, err := compileRule(item, endpoints)
		if err != nil {
			return nil, fmt.Errorf("规则 %s: %w", item.Name, err)
		}
		out = append(out, rule)
	}
	return out, nil
}

func compileRule(cfg RuleConfig, endpoints map[string]*Endpoint) (*compiledRule, error) {
	r := &compiledRule{
		cfg:           cfg,
		suffix:        buildSuffixTrie(cfg.Match.DomainSuffixes),
		excludeSuffix: buildSuffixTrie(cfg.Match.ExcludeDomainSuffixes),
		keywords:      cfg.Match.DomainKeywords,
		profiles:      cfg.Match.Profiles,
	}
	if len(cfg.Match.Domains) > 0 {
		r.domains = make(map[string]struct{}, len(cfg.Match.Domains))
		for _, d := range cfg.Match.Domains {
			r.domains[d] = struct{}{}
		}
	}
	for _, expr := range cfg.Match.DomainRegexps {
		compiled, err := regexp.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf("正则 %q 无效: %w", expr, err)
		}
		r.regexps = append(r.regexps, compiled)
	}
	for _, cidr := range cfg.Match.CIDRs {
		prefix, err := netip.ParsePrefix(cidr)
		if err != nil {
			return nil, fmt.Errorf("CIDR %q 无效: %w", cidr, err)
		}
		r.prefixes = append(r.prefixes, prefix)
	}
	if len(cfg.Match.Ports) > 0 {
		r.ports = make(map[int]struct{}, len(cfg.Match.Ports))
		for _, p := range cfg.Match.Ports {
			r.ports[p] = struct{}{}
		}
	}
	if len(cfg.Match.Schemes) > 0 {
		r.schemes = make(map[string]struct{}, len(cfg.Match.Schemes))
		for _, s := range cfg.Match.Schemes {
			r.schemes[s] = struct{}{}
		}
	}
	for _, name := range cfg.Endpoints {
		endpoint, ok := endpoints[name]
		if !ok {
			return nil, fmt.Errorf("引用了不存在的端点 %s", name)
		}
		r.endpoints = append(r.endpoints, endpoint)
	}
	return r, nil
}

// match 判定目标是否命中本规则，并返回命中原因。
//
// 不同维度之间是「与」：写了域名后缀又写了端口，两者都要满足。
// 这是刻意的——出海规则最怕的是「本想只放 443，结果整域全放了」。
func (r *compiledRule) match(t Target, ip netip.Addr, hasIP bool) (string, bool) {
	if r.excludeSuffix != nil && r.excludeSuffix.contains(t.Host) {
		return "", false
	}
	if len(r.schemes) > 0 {
		if _, ok := r.schemes[t.Scheme]; !ok {
			return "", false
		}
	}
	if len(r.ports) > 0 {
		if _, ok := r.ports[t.Port]; !ok {
			return "", false
		}
	}
	if len(r.profiles) > 0 && !matchProfile(r.profiles, t.Profile) {
		return "", false
	}
	if len(r.prefixes) > 0 {
		if !hasIP {
			return "", false
		}
		hit := false
		for _, prefix := range r.prefixes {
			if prefix.Contains(ip) {
				hit = true
				break
			}
		}
		if !hit {
			return "", false
		}
	}

	// 域名维度：四种写法内部是「或」，只要有一条命中即可。
	hasDomainCondition := r.suffix != nil || len(r.domains) > 0 || len(r.keywords) > 0 || len(r.regexps) > 0
	if hasDomainCondition {
		if suffix, ok := r.suffix.match(t.Host); ok {
			return "domainSuffix=" + suffix, true
		}
		if _, ok := r.domains[t.Host]; ok {
			return "domain=" + t.Host, true
		}
		for _, keyword := range r.keywords {
			if strings.Contains(t.Host, keyword) {
				return "domainKeyword=" + keyword, true
			}
		}
		for _, expr := range r.regexps {
			if expr.MatchString(t.Host) {
				return "domainRegexp=" + expr.String(), true
			}
		}
		return "", false
	}

	switch {
	case len(r.prefixes) > 0:
		return "cidr", true
	case len(r.profiles) > 0:
		return "profile=" + t.Profile, true
	case len(r.ports) > 0:
		return fmt.Sprintf("port=%d", t.Port), true
	case len(r.schemes) > 0:
		return "scheme=" + t.Scheme, true
	case r.cfg.Match.MatchAll:
		return "matchAll", true
	default:
		return "", false
	}
}

// matchProfile 支持 "payment.*" 形式的前缀通配。
func matchProfile(patterns []string, profile string) bool {
	for _, pattern := range patterns {
		if pattern == "*" {
			return true
		}
		if strings.HasSuffix(pattern, ".*") {
			if strings.HasPrefix(profile, strings.TrimSuffix(pattern, "*")) {
				return true
			}
			continue
		}
		if pattern == profile {
			return true
		}
	}
	return false
}

// pick 按策略挑一个可用端点。
//
// 没有可用端点时返回错误而**不是**悄悄降级直连：
// 「本该出海的流量走了内网出口」是一种会被误当成功的失败，
// 需要直连兜底就在端点列表末尾显式挂一个 direct 端点。
func pickEndpoint(candidates []*Endpoint, strategy Strategy, rr *atomic.Uint64, allowUnhealthy bool) (*Endpoint, error) {
	if len(candidates) == 0 {
		return nil, fmt.Errorf("没有配置可用端点")
	}
	now := time.Now()
	available := make([]*Endpoint, 0, len(candidates))
	for _, endpoint := range candidates {
		if endpoint.available(now, false) {
			available = append(available, endpoint)
		}
	}
	if len(available) == 0 && allowUnhealthy {
		for _, endpoint := range candidates {
			if endpoint.enabled() {
				available = append(available, endpoint)
			}
		}
	}
	if len(available) == 0 {
		return nil, fmt.Errorf("所有端点均不可用（未启用或健康检查未通过）")
	}

	switch strategy {
	case StrategyRoundRobin:
		idx := rr.Add(1) - 1
		return available[int(idx%uint64(len(available)))], nil
	case StrategyRandom:
		return available[rand.IntN(len(available))], nil
	case StrategyWeighted:
		return pickWeighted(available), nil
	case StrategyLatency:
		return pickLowestLatency(available), nil
	default: // StrategyFailover
		return available[0], nil
	}
}

func pickWeighted(candidates []*Endpoint) *Endpoint {
	total := 0
	for _, endpoint := range candidates {
		weight := endpoint.cfg.Weight
		if weight <= 0 {
			weight = 1
		}
		total += weight
	}
	if total <= 0 {
		return candidates[0]
	}
	pick := rand.IntN(total)
	for _, endpoint := range candidates {
		weight := endpoint.cfg.Weight
		if weight <= 0 {
			weight = 1
		}
		pick -= weight
		if pick < 0 {
			return endpoint
		}
	}
	return candidates[len(candidates)-1]
}

func pickLowestLatency(candidates []*Endpoint) *Endpoint {
	best := candidates[0]
	_, bestLatency, _, _, _, _ := best.snapshotHealth()
	for _, endpoint := range candidates[1:] {
		_, latency, _, _, _, _ := endpoint.snapshotHealth()
		// 尚未探测过（latency==0）的端点不参与「最低」比较，否则它永远是最优解。
		if latency == 0 {
			continue
		}
		if bestLatency == 0 || latency < bestLatency {
			best, bestLatency = endpoint, latency
		}
	}
	return best
}
