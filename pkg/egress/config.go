package egress

import (
	"fmt"
	"maps"
	"net"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Protocol 端点使用的出海协议。
//
// 新增协议不必改动本包：实现 Dialer 后用 RegisterProtocol 注册即可，
// 校验层会把已注册的自定义协议视为合法。
type Protocol string

const (
	// ProtocolDirect 直连端点。放在 failover 列表末尾即可实现「代理全挂就直连」。
	ProtocolDirect Protocol = "direct"
	// ProtocolHTTP 明文 HTTP 代理（CONNECT 隧道；可选 absolute-URI 转发模式）。
	ProtocolHTTP Protocol = "http"
	// ProtocolHTTPS TLS 之上的 HTTP 代理（CONNECT 隧道）。
	ProtocolHTTPS Protocol = "https"
	// ProtocolSOCKS5 SOCKS5，域名在本地解析后送 IP。
	ProtocolSOCKS5 Protocol = "socks5"
	// ProtocolSOCKS5H SOCKS5，域名交由代理解析（出海场景默认用这个，避免境内 DNS 污染）。
	ProtocolSOCKS5H Protocol = "socks5h"
	// ProtocolSSH 通过 SSH direct-tcpip 通道出海。
	ProtocolSSH Protocol = "ssh"
	// ProtocolTrojan Trojan（TLS + SHA224 口令 + SOCKS5 风格地址）。
	ProtocolTrojan Protocol = "trojan"
	// ProtocolShadowsocks Shadowsocks AEAD（aes-128-gcm / aes-256-gcm / chacha20-ietf-poly1305）。
	ProtocolShadowsocks Protocol = "shadowsocks"
)

// Action 规则命中后的动作。
type Action string

const (
	// ActionProxy 走代理端点出海。
	ActionProxy Action = "proxy"
	// ActionDirect 直连，不经任何端点。
	ActionDirect Action = "direct"
	// ActionReject 拒绝连接（黑洞），用于禁止访问某些目标。
	ActionReject Action = "reject"
)

// Strategy 一条规则挂多个端点时的选路策略。
type Strategy string

const (
	// StrategyFailover 按声明顺序取第一个健康端点（默认）。
	StrategyFailover Strategy = "failover"
	// StrategyRoundRobin 在健康端点间轮询。
	StrategyRoundRobin Strategy = "round_robin"
	// StrategyRandom 在健康端点间随机。
	StrategyRandom Strategy = "random"
	// StrategyWeighted 按 Weight 加权随机。
	StrategyWeighted Strategy = "weighted"
	// StrategyLatency 选探测延迟最低的健康端点。
	StrategyLatency Strategy = "latency"
)

// 配置默认值。数值型配置留 0 表示「用默认」，Normalize 负责补齐。
const (
	DefaultDialTimeout           = 10 * time.Second
	DefaultTLSHandshakeTimeout   = 10 * time.Second
	DefaultResponseHeaderTimeout = 30 * time.Second
	DefaultIdleConnTimeout       = 90 * time.Second
	DefaultMaxIdleConnsPerHost   = 8
	DefaultHealthInterval        = 60 * time.Second
	DefaultHealthTimeout         = 8 * time.Second
	DefaultHealthCooldown        = 30 * time.Second
	DefaultProbeURL              = "https://www.gstatic.com/generate_204"
	// maxViaDepth 端点链的最大跳数，防止配置写出环或过深的链路。
	maxViaDepth = 8
)

// Config 出海网关的完整配置，也是管理端 API 与 .env 的共同落点。
//
// 时间一律用毫秒/秒的整数字段而不是 time.Duration：JSON 里 "10s" 与 10000000000
// 两种写法都会让接入方困惑，整数 + 单位后缀的字段名没有歧义。
type Config struct {
	Enabled bool `json:"enabled"`
	// DefaultAction 所有规则都没命中时的动作，默认 direct。
	// 想做「白名单出海、其余一律直连」保持 direct；
	// 想做「全局出海、例外直连」把它设成 proxy 并填 DefaultEndpoints。
	DefaultAction    Action   `json:"defaultAction"`
	DefaultEndpoints []string `json:"defaultEndpoints,omitempty"`
	DefaultStrategy  Strategy `json:"defaultStrategy,omitempty"`

	DialTimeoutMS           int `json:"dialTimeoutMs,omitempty"`
	TLSHandshakeTimeoutMS   int `json:"tlsHandshakeTimeoutMs,omitempty"`
	ResponseHeaderTimeoutMS int `json:"responseHeaderTimeoutMs,omitempty"`
	IdleConnTimeoutMS       int `json:"idleConnTimeoutMs,omitempty"`
	MaxIdleConnsPerHost     int `json:"maxIdleConnsPerHost,omitempty"`

	Health    HealthConfig     `json:"health"`
	Endpoints []EndpointConfig `json:"endpoints"`
	Rules     []RuleConfig     `json:"rules"`

	// Source 配置来源标记（env / database / api），仅用于展示与排障。
	Source string `json:"source,omitempty"`
}

// HealthConfig 端点健康检查。主动探测负责发现「线路已经挂了」，
// 被动熔断负责「刚刚拨号失败的端点先别再用」，两者互补。
type HealthConfig struct {
	Enabled          bool   `json:"enabled"`
	IntervalSeconds  int    `json:"intervalSeconds,omitempty"`
	TimeoutSeconds   int    `json:"timeoutSeconds,omitempty"`
	FailureThreshold int    `json:"failureThreshold,omitempty"`
	SuccessThreshold int    `json:"successThreshold,omitempty"`
	ProbeURL         string `json:"probeUrl,omitempty"`
	// PassiveEnabled 拨号失败即计入失败计数并进入冷却，默认开。
	PassiveEnabled  bool `json:"passiveEnabled"`
	CooldownSeconds int  `json:"cooldownSeconds,omitempty"`
	// AllowUnhealthy 所有端点都不健康时是否仍然尝试（默认 true）。
	// 关掉它意味着探测误判会直接切断出海链路，一般不建议。
	AllowUnhealthy *bool `json:"allowUnhealthy,omitempty"`
}

// EndpointConfig 一条出海线路。
type EndpointConfig struct {
	Name     string   `json:"name"`
	Enabled  *bool    `json:"enabled,omitempty"` // 省略视为启用
	Protocol Protocol `json:"protocol"`
	Address  string   `json:"address,omitempty"` // host:port，direct 协议可留空
	Username string   `json:"username,omitempty"`
	Password string   `json:"password,omitempty"`
	// Via 先经另一个端点连出去，形成代理链（如 内网跳板 → 海外落地）。
	Via           string `json:"via,omitempty"`
	Weight        int    `json:"weight,omitempty"`
	DialTimeoutMS int    `json:"dialTimeoutMs,omitempty"`
	Region        string `json:"region,omitempty"`
	Remark        string `json:"remark,omitempty"`
	ProbeURL      string `json:"probeUrl,omitempty"`

	// HTTPForwardMode 仅对 http 协议有效：明文 http:// 请求用 absolute-URI 转发
	// 而不是 CONNECT 隧道。部分企业代理禁止 CONNECT 到 80 端口时需要打开。
	HTTPForwardMode bool              `json:"httpForwardMode,omitempty"`
	Headers         map[string]string `json:"headers,omitempty"` // CONNECT 附加请求头

	TLS         TLSConfig         `json:"tls"`
	SSH         SSHConfig         `json:"ssh"`
	Shadowsocks ShadowsocksConfig `json:"shadowsocks"`
	// Options 自定义协议的扩展参数，内置协议不使用。
	Options map[string]string `json:"options,omitempty"`
}

// TLSConfig 端点侧 TLS（https / trojan 必开；socks5 over TLS 也可用）。
type TLSConfig struct {
	Enabled            bool     `json:"enabled"`
	ServerName         string   `json:"serverName,omitempty"`
	InsecureSkipVerify bool     `json:"insecureSkipVerify,omitempty"`
	CAPEM              string   `json:"caPem,omitempty"`
	ClientCertPEM      string   `json:"clientCertPem,omitempty"`
	ClientKeyPEM       string   `json:"clientKeyPem,omitempty"`
	ALPN               []string `json:"alpn,omitempty"`
	MinVersion         string   `json:"minVersion,omitempty"` // 1.2 / 1.3
}

// SSHConfig SSH 隧道端点。HostKeyFingerprint 留空表示不校验主机密钥，
// 只应出现在完全可控的内网跳板上。
type SSHConfig struct {
	User               string `json:"user,omitempty"`
	Password           string `json:"password,omitempty"`
	PrivateKeyPEM      string `json:"privateKeyPem,omitempty"`
	Passphrase         string `json:"passphrase,omitempty"`
	HostKeyFingerprint string `json:"hostKeyFingerprint,omitempty"` // SHA256:xxx
	KeepAliveSeconds   int    `json:"keepAliveSeconds,omitempty"`
}

// ShadowsocksConfig Shadowsocks AEAD 参数。Password 留空时回退到 EndpointConfig.Password。
type ShadowsocksConfig struct {
	Method   string `json:"method,omitempty"`
	Password string `json:"password,omitempty"`
}

// RuleConfig 一条路由规则。
type RuleConfig struct {
	Name      string      `json:"name"`
	Enabled   *bool       `json:"enabled,omitempty"`
	Priority  int         `json:"priority,omitempty"` // 越小越先匹配；相同按声明顺序
	Action    Action      `json:"action,omitempty"`   // 默认 proxy
	Endpoints []string    `json:"endpoints,omitempty"`
	Strategy  Strategy    `json:"strategy,omitempty"`
	Match     MatchConfig `json:"match"`
	Remark    string      `json:"remark,omitempty"`
}

// MatchConfig 规则匹配条件。同一条规则内**不同维度之间是与**（都要满足），
// 同一维度内的多个值是或。DomainSuffixes 是主力维度，其余按需叠加。
type MatchConfig struct {
	// DomainSuffixes 域名后缀。"*.google.com" / ".google.com" / "google.com" 等价，
	// 且都按标签边界匹配：命中 www.google.com 与 google.com，不命中 notgoogle.com。
	DomainSuffixes []string `json:"domainSuffixes,omitempty"`
	// ExcludeDomainSuffixes 后缀例外，优先于 DomainSuffixes（如整域出海但某子域直连）。
	ExcludeDomainSuffixes []string `json:"excludeDomainSuffixes,omitempty"`
	Domains               []string `json:"domains,omitempty"`        // 精确域名
	DomainKeywords        []string `json:"domainKeywords,omitempty"` // 子串包含
	DomainRegexps         []string `json:"domainRegexps,omitempty"`
	CIDRs                 []string `json:"cidrs,omitempty"` // 目标是字面 IP 时按网段匹配
	Ports                 []int    `json:"ports,omitempty"`
	Schemes               []string `json:"schemes,omitempty"` // http / https / tcp
	// Profiles 调用方标识（如 payment.stripe / storage.s3 / oauth.google），
	// 让「同一个域名，不同业务走不同线路」成为可能。支持 "payment.*" 前缀通配。
	Profiles []string `json:"profiles,omitempty"`
	// MatchAll 无条件命中，用于兜底规则。
	MatchAll bool `json:"matchAll,omitempty"`
}

func boolValue(p *bool, fallback bool) bool {
	if p == nil {
		return fallback
	}
	return *p
}

func boolPtr(v bool) *bool { return &v }

// Normalize 补齐默认值并做无副作用的清洗（大小写、去空白、去重、排序）。
// 它是幂等的：Normalize(Normalize(c)) == Normalize(c)。
func (c Config) Normalize() Config {
	out := c.Clone()

	out.DefaultAction = normalizeAction(out.DefaultAction, ActionDirect)
	out.DefaultStrategy = normalizeStrategy(out.DefaultStrategy)
	out.DefaultEndpoints = dedupeStrings(out.DefaultEndpoints)
	out.Source = strings.TrimSpace(out.Source)

	if out.DialTimeoutMS <= 0 {
		out.DialTimeoutMS = int(DefaultDialTimeout / time.Millisecond)
	}
	if out.TLSHandshakeTimeoutMS <= 0 {
		out.TLSHandshakeTimeoutMS = int(DefaultTLSHandshakeTimeout / time.Millisecond)
	}
	if out.ResponseHeaderTimeoutMS <= 0 {
		out.ResponseHeaderTimeoutMS = int(DefaultResponseHeaderTimeout / time.Millisecond)
	}
	if out.IdleConnTimeoutMS <= 0 {
		out.IdleConnTimeoutMS = int(DefaultIdleConnTimeout / time.Millisecond)
	}
	if out.MaxIdleConnsPerHost <= 0 {
		out.MaxIdleConnsPerHost = DefaultMaxIdleConnsPerHost
	}

	out.Health = normalizeHealth(out.Health)

	for i := range out.Endpoints {
		out.Endpoints[i] = normalizeEndpoint(out.Endpoints[i])
	}
	for i := range out.Rules {
		out.Rules[i] = normalizeRule(out.Rules[i], i)
	}
	// 稳定排序：Priority 升序，相同 Priority 保持声明顺序。
	sort.SliceStable(out.Rules, func(i, j int) bool {
		return out.Rules[i].Priority < out.Rules[j].Priority
	})
	return out
}

func normalizeHealth(h HealthConfig) HealthConfig {
	if h.IntervalSeconds <= 0 {
		h.IntervalSeconds = int(DefaultHealthInterval / time.Second)
	}
	if h.TimeoutSeconds <= 0 {
		h.TimeoutSeconds = int(DefaultHealthTimeout / time.Second)
	}
	if h.FailureThreshold <= 0 {
		h.FailureThreshold = 2
	}
	if h.SuccessThreshold <= 0 {
		h.SuccessThreshold = 1
	}
	if h.CooldownSeconds <= 0 {
		h.CooldownSeconds = int(DefaultHealthCooldown / time.Second)
	}
	if strings.TrimSpace(h.ProbeURL) == "" {
		h.ProbeURL = DefaultProbeURL
	}
	h.ProbeURL = strings.TrimSpace(h.ProbeURL)
	h.AllowUnhealthy = boolPtr(boolValue(h.AllowUnhealthy, true))
	return h
}

func normalizeEndpoint(e EndpointConfig) EndpointConfig {
	e.Name = strings.TrimSpace(e.Name)
	e.Protocol = Protocol(strings.ToLower(strings.TrimSpace(string(e.Protocol))))
	if e.Protocol == "" {
		e.Protocol = ProtocolHTTP
	}
	// socks 是历史上常见的简写，统一到 socks5h（出海场景要远端解析 DNS）。
	if e.Protocol == "socks" {
		e.Protocol = ProtocolSOCKS5H
	}
	if e.Protocol == "ss" {
		e.Protocol = ProtocolShadowsocks
	}
	e.Address = strings.TrimSpace(e.Address)
	e.Via = strings.TrimSpace(e.Via)
	e.Region = strings.TrimSpace(e.Region)
	e.ProbeURL = strings.TrimSpace(e.ProbeURL)
	e.Enabled = boolPtr(boolValue(e.Enabled, true))
	if e.Weight <= 0 {
		e.Weight = 1
	}
	if e.Protocol != ProtocolHTTP {
		e.HTTPForwardMode = false
	}
	// https / trojan 天然基于 TLS，配置里漏写 tls.enabled 不该导致明文外发。
	if e.Protocol == ProtocolHTTPS || e.Protocol == ProtocolTrojan {
		e.TLS.Enabled = true
	}
	if e.TLS.Enabled && strings.TrimSpace(e.TLS.ServerName) == "" {
		if host, _, err := net.SplitHostPort(e.Address); err == nil {
			e.TLS.ServerName = host
		}
	}
	e.TLS.ServerName = strings.TrimSpace(e.TLS.ServerName)
	e.TLS.MinVersion = strings.TrimSpace(e.TLS.MinVersion)
	if e.Protocol == ProtocolShadowsocks {
		e.Shadowsocks.Method = strings.ToLower(strings.TrimSpace(e.Shadowsocks.Method))
		if e.Shadowsocks.Method == "" {
			e.Shadowsocks.Method = "aes-256-gcm"
		}
		if e.Shadowsocks.Password == "" {
			e.Shadowsocks.Password = e.Password
		}
	}
	if e.Protocol == ProtocolSSH {
		e.SSH.User = strings.TrimSpace(e.SSH.User)
		if e.SSH.User == "" {
			e.SSH.User = strings.TrimSpace(e.Username)
		}
		if e.SSH.Password == "" {
			e.SSH.Password = e.Password
		}
		e.SSH.HostKeyFingerprint = strings.TrimSpace(e.SSH.HostKeyFingerprint)
	}
	if len(e.Headers) == 0 {
		e.Headers = nil
	}
	if len(e.Options) == 0 {
		e.Options = nil
	}
	return e
}

func normalizeRule(r RuleConfig, index int) RuleConfig {
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" {
		r.Name = fmt.Sprintf("rule-%d", index+1)
	}
	r.Enabled = boolPtr(boolValue(r.Enabled, true))
	r.Action = normalizeAction(r.Action, ActionProxy)
	r.Strategy = normalizeStrategy(r.Strategy)
	r.Endpoints = dedupeStrings(r.Endpoints)
	if r.Action != ActionProxy {
		r.Endpoints = nil
	}

	m := r.Match
	m.DomainSuffixes = dedupeStrings(mapStrings(m.DomainSuffixes, NormalizeDomainSuffix))
	m.ExcludeDomainSuffixes = dedupeStrings(mapStrings(m.ExcludeDomainSuffixes, NormalizeDomainSuffix))
	m.Domains = dedupeStrings(mapStrings(m.Domains, normalizeHost))
	m.DomainKeywords = dedupeStrings(mapStrings(m.DomainKeywords, strings.ToLower))
	m.DomainRegexps = dedupeStrings(mapStrings(m.DomainRegexps, strings.TrimSpace))
	m.CIDRs = dedupeStrings(mapStrings(m.CIDRs, strings.TrimSpace))
	m.Schemes = dedupeStrings(mapStrings(m.Schemes, strings.ToLower))
	m.Profiles = dedupeStrings(mapStrings(m.Profiles, strings.ToLower))
	ports := make([]int, 0, len(m.Ports))
	seen := map[int]struct{}{}
	for _, p := range m.Ports {
		if p <= 0 || p > 65535 {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		ports = append(ports, p)
	}
	sort.Ints(ports)
	m.Ports = ports
	r.Match = m
	return r
}

func normalizeAction(a Action, fallback Action) Action {
	switch Action(strings.ToLower(strings.TrimSpace(string(a)))) {
	case ActionProxy:
		return ActionProxy
	case ActionDirect:
		return ActionDirect
	case ActionReject:
		return ActionReject
	default:
		return fallback
	}
}

func normalizeStrategy(s Strategy) Strategy {
	switch Strategy(strings.ToLower(strings.TrimSpace(string(s)))) {
	case StrategyRoundRobin:
		return StrategyRoundRobin
	case StrategyRandom:
		return StrategyRandom
	case StrategyWeighted:
		return StrategyWeighted
	case StrategyLatency:
		return StrategyLatency
	default:
		return StrategyFailover
	}
}

// NormalizeDomainSuffix 把 "*.Google.com." / ".google.com" / "google.com"
// 统一成 "google.com"。返回空串表示这条后缀无意义（调用方应丢弃）。
func NormalizeDomainSuffix(raw string) string {
	s := normalizeHost(raw)
	s = strings.TrimPrefix(s, "*.")
	s = strings.TrimPrefix(s, ".")
	s = strings.TrimSuffix(s, ".")
	if s == "*" {
		return ""
	}
	return s
}

func normalizeHost(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.TrimSuffix(s, ".")
	return s
}

func mapStrings(in []string, fn func(string) string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, item := range in {
		if v := strings.TrimSpace(fn(item)); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func dedupeStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Clone 深拷贝，保证快照之间不共享切片/map。
func (c Config) Clone() Config {
	out := c
	out.DefaultEndpoints = append([]string(nil), c.DefaultEndpoints...)
	if c.Health.AllowUnhealthy != nil {
		out.Health.AllowUnhealthy = boolPtr(*c.Health.AllowUnhealthy)
	}
	out.Endpoints = make([]EndpointConfig, len(c.Endpoints))
	for i, e := range c.Endpoints {
		out.Endpoints[i] = e.Clone()
	}
	out.Rules = make([]RuleConfig, len(c.Rules))
	for i, r := range c.Rules {
		out.Rules[i] = r.Clone()
	}
	return out
}

// Clone 深拷贝端点配置。
func (e EndpointConfig) Clone() EndpointConfig {
	out := e
	if e.Enabled != nil {
		out.Enabled = boolPtr(*e.Enabled)
	}
	out.TLS.ALPN = append([]string(nil), e.TLS.ALPN...)
	if len(e.Headers) > 0 {
		out.Headers = maps.Clone(e.Headers)
	}
	if len(e.Options) > 0 {
		out.Options = maps.Clone(e.Options)
	}
	return out
}

// Clone 深拷贝规则配置。
func (r RuleConfig) Clone() RuleConfig {
	out := r
	if r.Enabled != nil {
		out.Enabled = boolPtr(*r.Enabled)
	}
	out.Endpoints = append([]string(nil), r.Endpoints...)
	out.Match.DomainSuffixes = append([]string(nil), r.Match.DomainSuffixes...)
	out.Match.ExcludeDomainSuffixes = append([]string(nil), r.Match.ExcludeDomainSuffixes...)
	out.Match.Domains = append([]string(nil), r.Match.Domains...)
	out.Match.DomainKeywords = append([]string(nil), r.Match.DomainKeywords...)
	out.Match.DomainRegexps = append([]string(nil), r.Match.DomainRegexps...)
	out.Match.CIDRs = append([]string(nil), r.Match.CIDRs...)
	out.Match.Schemes = append([]string(nil), r.Match.Schemes...)
	out.Match.Profiles = append([]string(nil), r.Match.Profiles...)
	out.Match.Ports = append([]int(nil), r.Match.Ports...)
	return out
}

// Validate 校验规范化之后的配置。返回的错误面向配置者，直接展示给管理端。
//
// 调用前应先 Normalize；Validate 不修改入参。
func (c Config) Validate() error {
	names := make(map[string]struct{}, len(c.Endpoints))
	for _, e := range c.Endpoints {
		if e.Name == "" {
			return fmt.Errorf("端点名称不能为空")
		}
		if !endpointNamePattern.MatchString(e.Name) {
			return fmt.Errorf("端点 %q 名称非法：只允许字母、数字、下划线、连字符和点", e.Name)
		}
		if _, dup := names[e.Name]; dup {
			return fmt.Errorf("端点名称重复: %s", e.Name)
		}
		names[e.Name] = struct{}{}
		if err := validateEndpoint(e); err != nil {
			return fmt.Errorf("端点 %s: %w", e.Name, err)
		}
	}
	// Via 链：存在性 + 无环 + 深度上限。
	for _, e := range c.Endpoints {
		if err := validateViaChain(c.Endpoints, e); err != nil {
			return err
		}
	}
	for _, name := range c.DefaultEndpoints {
		if _, ok := names[name]; !ok {
			return fmt.Errorf("默认端点 %s 不存在", name)
		}
	}
	if c.DefaultAction == ActionProxy && len(c.DefaultEndpoints) == 0 {
		return fmt.Errorf("默认动作为 proxy 时必须配置 defaultEndpoints")
	}

	ruleNames := make(map[string]struct{}, len(c.Rules))
	for _, r := range c.Rules {
		if _, dup := ruleNames[r.Name]; dup {
			return fmt.Errorf("规则名称重复: %s", r.Name)
		}
		ruleNames[r.Name] = struct{}{}
		if err := validateRule(r, names); err != nil {
			return fmt.Errorf("规则 %s: %w", r.Name, err)
		}
	}
	if c.Health.FailureThreshold <= 0 || c.Health.SuccessThreshold <= 0 {
		return fmt.Errorf("健康检查阈值必须为正整数")
	}
	return nil
}

var endpointNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

func validateEndpoint(e EndpointConfig) error {
	if e.Protocol != ProtocolDirect {
		if e.Address == "" {
			return fmt.Errorf("缺少 address（host:port）")
		}
		host, port, err := net.SplitHostPort(e.Address)
		if err != nil {
			return fmt.Errorf("address 必须是 host:port 形式: %v", err)
		}
		if strings.TrimSpace(host) == "" {
			return fmt.Errorf("address 缺少主机名")
		}
		if n, err := strconv.Atoi(port); err != nil || n <= 0 || n > 65535 {
			return fmt.Errorf("address 端口非法: %s", port)
		}
	}
	switch e.Protocol {
	case ProtocolDirect, ProtocolHTTP, ProtocolHTTPS, ProtocolSOCKS5, ProtocolSOCKS5H:
		// 无额外必填项：代理鉴权可选。
	case ProtocolSSH:
		if e.SSH.User == "" {
			return fmt.Errorf("ssh 端点缺少 ssh.user")
		}
		if e.SSH.Password == "" && strings.TrimSpace(e.SSH.PrivateKeyPEM) == "" {
			return fmt.Errorf("ssh 端点需要 ssh.password 或 ssh.privateKeyPem")
		}
	case ProtocolTrojan:
		if e.Password == "" {
			return fmt.Errorf("trojan 端点缺少 password")
		}
	case ProtocolShadowsocks:
		if e.Shadowsocks.Password == "" {
			return fmt.Errorf("shadowsocks 端点缺少 password")
		}
		if !supportedShadowsocksMethod(e.Shadowsocks.Method) {
			return fmt.Errorf("不支持的 shadowsocks 加密方式: %s", e.Shadowsocks.Method)
		}
	default:
		if !protocolRegistered(e.Protocol) {
			return fmt.Errorf("未知协议: %s", e.Protocol)
		}
	}
	if e.TLS.Enabled {
		switch e.TLS.MinVersion {
		case "", "1.2", "1.3":
		default:
			return fmt.Errorf("tls.minVersion 仅支持 1.2 / 1.3")
		}
		if (e.TLS.ClientCertPEM == "") != (e.TLS.ClientKeyPEM == "") {
			return fmt.Errorf("双向 TLS 需要同时提供 clientCertPem 与 clientKeyPem")
		}
	}
	return nil
}

func validateViaChain(all []EndpointConfig, start EndpointConfig) error {
	index := make(map[string]EndpointConfig, len(all))
	for _, e := range all {
		index[e.Name] = e
	}
	seen := map[string]struct{}{start.Name: {}}
	cur := start
	for depth := 0; cur.Via != ""; depth++ {
		if depth >= maxViaDepth {
			return fmt.Errorf("端点 %s 的 via 链超过 %d 跳", start.Name, maxViaDepth)
		}
		next, ok := index[cur.Via]
		if !ok {
			return fmt.Errorf("端点 %s 的 via 指向不存在的端点 %s", cur.Name, cur.Via)
		}
		if _, dup := seen[next.Name]; dup {
			return fmt.Errorf("端点 %s 的 via 链存在环: %s", start.Name, next.Name)
		}
		seen[next.Name] = struct{}{}
		cur = next
	}
	return nil
}

func validateRule(r RuleConfig, endpoints map[string]struct{}) error {
	if r.Action == ActionProxy && len(r.Endpoints) == 0 {
		return fmt.Errorf("动作为 proxy 时必须指定至少一个端点")
	}
	for _, name := range r.Endpoints {
		if _, ok := endpoints[name]; !ok {
			return fmt.Errorf("引用了不存在的端点 %s", name)
		}
	}
	m := r.Match
	if !m.MatchAll &&
		len(m.DomainSuffixes) == 0 && len(m.Domains) == 0 && len(m.DomainKeywords) == 0 &&
		len(m.DomainRegexps) == 0 && len(m.CIDRs) == 0 && len(m.Ports) == 0 &&
		len(m.Schemes) == 0 && len(m.Profiles) == 0 {
		return fmt.Errorf("至少要有一个匹配条件（或显式设置 matchAll）")
	}
	for _, expr := range m.DomainRegexps {
		if _, err := regexp.Compile(expr); err != nil {
			return fmt.Errorf("正则 %q 无效: %v", expr, err)
		}
	}
	for _, cidr := range m.CIDRs {
		if _, err := netip.ParsePrefix(cidr); err != nil {
			return fmt.Errorf("CIDR %q 无效: %v", cidr, err)
		}
	}
	for _, scheme := range m.Schemes {
		switch scheme {
		case "http", "https", "tcp":
		default:
			return fmt.Errorf("scheme %q 无效（仅 http / https / tcp）", scheme)
		}
	}
	return nil
}

func (c Config) dialTimeout() time.Duration {
	return time.Duration(c.DialTimeoutMS) * time.Millisecond
}
