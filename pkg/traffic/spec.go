// Package traffic 是 Aegis 的流量管理引擎：按规则对进入的请求做分类、
// 限速、并发控制、排队削峰、预热爬坡与过载保护。
//
// 引擎本身不依赖 Aegis 的任何内部包，只认 net/http：配置用纯数据结构描述
// （本文件），运行时由 Engine 编译成不可变快照，热路径上只做原子操作与
// 只读查表，不加全局锁、不分配内存。集群协同（跨实例配额、实例计数）经
// Coordinator 接口注入，单机部署时可以为 nil。
//
// 设计目标与容量估算见 docs/traffic-management.md。
package traffic

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"
)

// ── 配置 ──

// Config 是一份完整、可发布的流量管理配置：全局设置 + 有序规则表。
// 每次发布在数据库里留一个修订号，回滚即重新发布旧修订的内容。
type Config struct {
	Revision int64    `json:"revision"`
	Settings Settings `json:"settings"`
	Rules    []Rule   `json:"rules"`
}

// Settings 全局设置。
type Settings struct {
	// Enabled 总开关。关闭后所有请求直接放行，统计也停止计数。
	Enabled bool `json:"enabled"`
	// ExemptAdmin /api/admin/ 前缀免于规则（过载保护仍然只放行 critical 级）。
	// 默认开：洪峰时恰恰需要打开控制台看统计、调参数。
	ExemptAdmin bool `json:"exemptAdmin"`
	// ExemptPaths 免检路径（与规则的路径写法相同）。/healthz、/readyz 与 WebSocket 升级请求恒免。
	ExemptPaths []string `json:"exemptPaths"`
	// Overload 进程级过载保护。
	Overload OverloadSpec `json:"overload"`
	// Response 默认拒绝响应，规则可以覆盖。
	Response ResponseSpec `json:"response"`
	// Cluster 集群协同。
	Cluster ClusterSpec `json:"cluster"`
	// MaxTrackedKeys 每个按键限速器最多跟踪多少个不同的键（IP、用户、令牌……）。
	// 超出后新键共享溢出桶，保证内存有上界。默认 2,000,000。
	MaxTrackedKeys int `json:"maxTrackedKeys"`
}

// OverloadSpec 过载保护：CPU 使用率越过阈值且在途请求超过当前处理能力
// （最大吞吐 × 最小响应时间，即 BDP）时，按优先级从低到高丢弃新请求。
type OverloadSpec struct {
	Enabled bool `json:"enabled"`
	// CPUThreshold 进程 CPU 使用率阈值（百分比，按可用核数归一）。默认 85。
	CPUThreshold int `json:"cpuThreshold"`
	// MaxInflight 全局在途硬上限（0 = 不限）。保护内存不被慢请求堆满。
	MaxInflight int `json:"maxInflight"`
	// CooldownMs 触发丢弃后至少保持多久的保护状态，防止在阈值附近抖动。
	CooldownMs int `json:"cooldownMs"`
}

// ResponseSpec 拒绝请求时的响应。
type ResponseSpec struct {
	Status            int    `json:"status"`
	Code              int    `json:"code"`
	Message           string `json:"message"`
	RetryAfterSeconds int    `json:"retryAfterSeconds"`
}

// ClusterSpec 集群协同。开启后：
//   - scope=cluster 的全局限速经 Coordinator 以租约方式共享一个集群总额；
//   - scope=cluster 的按键限速按存活实例数均分（每个实例只承担自己那份）。
type ClusterSpec struct {
	Enabled bool `json:"enabled"`
}

// 规则模式。
const (
	ModeEnforce = "enforce" // 生效：超限即拒绝
	ModeObserve = "observe" // 观察：只统计「本应拒绝」，不拦截
	ModeBypass  = "bypass"  // 放行：命中后直接放行并停止匹配
)

// 优先级档位。过载保护按档位从低到高丢弃，critical 永不因过载被丢弃。
const (
	ClassCritical = "critical"
	ClassHigh     = "high"
	ClassNormal   = "normal"
	ClassLow      = "low"
)

// Rule 一条流量规则：匹配条件 + 一组控制手段。命中的规则按顺序依次生效，
// 任一环节拒绝即拒绝；Terminal 的规则命中后不再继续匹配后面的规则。
type Rule struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
	Mode        string `json:"mode"`
	Class       string `json:"class"`
	Terminal    bool   `json:"terminal"`

	Match       MatchSpec        `json:"match"`
	Limits      []LimitSpec      `json:"limits"`
	Concurrency *ConcurrencySpec `json:"concurrency,omitempty"`
	Queue       *QueueSpec       `json:"queue,omitempty"`
	Warmup      *WarmupSpec      `json:"warmup,omitempty"`
	Response    *ResponseSpec    `json:"response,omitempty"`

	UpdatedAt time.Time `json:"updatedAt"`
	UpdatedBy *int64    `json:"updatedBy,omitempty"`
}

// MatchSpec 匹配条件。各字段之间是「且」，字段内的多个取值是「或」；空字段不限制。
type MatchSpec struct {
	// Methods HTTP 方法，大写。
	Methods []string `json:"methods"`
	// Paths 路径：精确 `/api/auth/login`，前缀 `/api/auth/*`，单段通配 `/api/apps/*/users`。
	Paths []string `json:"paths"`
	// ExcludePaths 排除的路径，写法同上。
	ExcludePaths []string `json:"excludePaths"`
	// Hosts 主机名：精确或 `*.example.com`。
	Hosts []string `json:"hosts"`
	// AppKeys 应用（按 X-Aegis-App-Key 请求头或 appKey 查询参数）。
	AppKeys []string `json:"appKeys"`
	// CIDRs 来源网段；ExcludeCIDRs 排除的网段。
	CIDRs        []string `json:"cidrs"`
	ExcludeCIDRs []string `json:"excludeCidrs"`
	// Headers 请求头条件。
	Headers []HeaderMatch `json:"headers"`
}

// HeaderMatch 请求头条件。Op：exists | absent | equals | prefix | contains。
type HeaderMatch struct {
	Name  string `json:"name"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

// 限速键。
const (
	KeyGlobal = "global" // 所有命中请求共享一个桶
	KeyIP     = "ip"     // 每个来源 IP
	KeySubnet = "subnet" // 每个网段：IPv4 /24、IPv6 /64
	KeyApp    = "app"    // 每个应用
	KeyToken  = "token"  // 每个 Authorization 凭据
	KeyHeader = "header" // 每个指定请求头的取值
	KeyQuery  = "query"  // 每个指定查询参数的取值
)

// 限速作用域。
const (
	ScopeLocal   = "local"   // 每个实例各自计数
	ScopeCluster = "cluster" // 集群共享（需开启 Settings.Cluster）
)

// LimitSpec 一条速率限制：每 Period 最多 Rate 次，允许 Burst 的瞬时突发。
// 算法是 GCRA（等价于令牌桶，但每个键只存一个时间戳）。
type LimitSpec struct {
	Key string `json:"key"`
	// Name Key 为 header / query 时的名字。
	Name string `json:"name"`
	// Rate 每个周期允许的请求数。
	Rate int `json:"rate"`
	// PeriodMs 周期，毫秒。默认 1000。
	PeriodMs int `json:"periodMs"`
	// Burst 瞬时突发容量。默认等于 Rate。
	Burst int `json:"burst"`
	// Scope local | cluster。
	Scope string `json:"scope"`
	// Fallback 取不到键值时（没有该请求头等）：ip = 改按 IP 计，skip = 不限制。
	Fallback string `json:"fallback"`
}

// ConcurrencySpec 并发上限。Adaptive 时上限在 [MinLimit, MaxLimit] 内
// 按响应时间自动调节（AIMD）：延迟显著高于基线就乘性收缩，否则加性试探。
type ConcurrencySpec struct {
	Max      int  `json:"max"`
	Adaptive bool `json:"adaptive"`
	MinLimit int  `json:"minLimit"`
	MaxLimit int  `json:"maxLimit"`
	// LatencyToleranceX10 延迟容忍倍数 ×10（默认 20 = 基线的 2 倍）。
	LatencyToleranceX10 int `json:"latencyToleranceX10"`
}

// QueueSpec 排队削峰：被限速或并发已满的请求先等一会儿，而不是立刻拒绝。
type QueueSpec struct {
	Size      int `json:"size"`
	TimeoutMs int `json:"timeoutMs"`
}

// WarmupSpec 预热爬坡：从 BaselineRPS 起，需求逼近上限时每个周期按
// StepPct 抬高准入上限，直到 MaxRPS；需求持续低于基线 CooldownSeconds 后回落。
// 冷连接池、冷缓存、下游依赖由此获得热身时间，而不是被洪峰一次打穿。
type WarmupSpec struct {
	BaselineRPS     int `json:"baselineRps"`
	MaxRPS          int `json:"maxRps"`
	StepPct         int `json:"stepPct"`
	IntervalMs      int `json:"intervalMs"`
	CooldownSeconds int `json:"cooldownSeconds"`
}

// ── 默认值与校验 ──

// DefaultSettings 首次启用时的全局设置。
func DefaultSettings() Settings {
	return NormalizeSettings(Settings{
		Enabled:     true,
		ExemptAdmin: true,
		Overload: OverloadSpec{
			Enabled:      true,
			CPUThreshold: 85,
			CooldownMs:   1000,
		},
		Response: ResponseSpec{
			Status:            429,
			Code:              42910,
			Message:           "服务繁忙，请稍后再试",
			RetryAfterSeconds: 3,
		},
	})
}

// NormalizeSettings 铺默认值并夹取非法区间。
func NormalizeSettings(s Settings) Settings {
	s.ExemptPaths = normalizePaths(s.ExemptPaths)
	if s.Overload.CPUThreshold <= 0 {
		s.Overload.CPUThreshold = 85
	}
	s.Overload.CPUThreshold = clampInt(s.Overload.CPUThreshold, 20, 100)
	s.Overload.MaxInflight = clampInt(s.Overload.MaxInflight, 0, 10_000_000)
	if s.Overload.CooldownMs <= 0 {
		s.Overload.CooldownMs = 1000
	}
	s.Overload.CooldownMs = clampInt(s.Overload.CooldownMs, 100, 60_000)
	s.Response = normalizeResponse(s.Response)
	if s.MaxTrackedKeys <= 0 {
		s.MaxTrackedKeys = 2_000_000
	}
	s.MaxTrackedKeys = clampInt(s.MaxTrackedKeys, 10_000, 50_000_000)
	return s
}

func normalizeResponse(r ResponseSpec) ResponseSpec {
	if r.Status != 429 && r.Status != 503 && r.Status != 403 {
		r.Status = 429
	}
	if r.Code <= 0 {
		r.Code = 42910
	}
	r.Message = strings.TrimSpace(r.Message)
	if r.Message == "" {
		r.Message = "服务繁忙，请稍后再试"
	}
	if len([]rune(r.Message)) > 200 {
		r.Message = string([]rune(r.Message)[:200])
	}
	if r.RetryAfterSeconds <= 0 {
		r.RetryAfterSeconds = 3
	}
	r.RetryAfterSeconds = clampInt(r.RetryAfterSeconds, 1, 3600)
	return r
}

// NormalizeRule 铺默认值。ID 为空时由调用方分配。
func NormalizeRule(r Rule) Rule {
	r.ID = strings.TrimSpace(r.ID)
	r.Name = strings.TrimSpace(r.Name)
	r.Description = strings.TrimSpace(r.Description)
	switch r.Mode {
	case ModeEnforce, ModeObserve, ModeBypass:
	default:
		r.Mode = ModeEnforce
	}
	switch r.Class {
	case ClassCritical, ClassHigh, ClassNormal, ClassLow:
	default:
		r.Class = ClassNormal
	}
	m := &r.Match
	m.Methods = normalizeUpper(m.Methods)
	m.Paths = normalizePaths(m.Paths)
	m.ExcludePaths = normalizePaths(m.ExcludePaths)
	m.Hosts = normalizeLower(m.Hosts)
	m.AppKeys = normalizeTrim(m.AppKeys)
	m.CIDRs = normalizeTrim(m.CIDRs)
	m.ExcludeCIDRs = normalizeTrim(m.ExcludeCIDRs)
	headers := m.Headers[:0:0]
	for _, h := range m.Headers {
		h.Name = strings.TrimSpace(h.Name)
		if h.Name == "" {
			continue
		}
		switch h.Op {
		case "exists", "absent", "equals", "prefix", "contains":
		default:
			h.Op = "equals"
		}
		headers = append(headers, h)
	}
	m.Headers = headers

	limits := r.Limits[:0:0]
	for _, l := range r.Limits {
		switch l.Key {
		case KeyGlobal, KeyIP, KeySubnet, KeyApp, KeyToken, KeyHeader, KeyQuery:
		default:
			l.Key = KeyIP
		}
		l.Name = strings.TrimSpace(l.Name)
		if l.PeriodMs <= 0 {
			l.PeriodMs = 1000
		}
		l.PeriodMs = clampInt(l.PeriodMs, 10, 86_400_000)
		l.Rate = clampInt(l.Rate, 1, 100_000_000)
		if l.Burst <= 0 {
			l.Burst = l.Rate
		}
		l.Burst = clampInt(l.Burst, 1, 100_000_000)
		if l.Scope != ScopeCluster {
			l.Scope = ScopeLocal
		}
		if l.Fallback != "skip" {
			l.Fallback = "ip"
		}
		limits = append(limits, l)
	}
	r.Limits = limits

	if c := r.Concurrency; c != nil {
		cc := *c
		cc.Max = clampInt(cc.Max, 1, 10_000_000)
		if cc.Adaptive {
			if cc.MinLimit <= 0 {
				cc.MinLimit = maxInt(1, cc.Max/10)
			}
			if cc.MaxLimit <= 0 {
				cc.MaxLimit = cc.Max * 4
			}
			cc.MinLimit = clampInt(cc.MinLimit, 1, 10_000_000)
			cc.MaxLimit = clampInt(cc.MaxLimit, cc.MinLimit, 10_000_000)
			cc.Max = clampInt(cc.Max, cc.MinLimit, cc.MaxLimit)
		}
		if cc.LatencyToleranceX10 <= 0 {
			cc.LatencyToleranceX10 = 20
		}
		cc.LatencyToleranceX10 = clampInt(cc.LatencyToleranceX10, 11, 100)
		r.Concurrency = &cc
	}
	if q := r.Queue; q != nil {
		qq := *q
		qq.Size = clampInt(qq.Size, 0, 1_000_000)
		if qq.TimeoutMs <= 0 {
			qq.TimeoutMs = 2000
		}
		qq.TimeoutMs = clampInt(qq.TimeoutMs, 10, 60_000)
		if qq.Size == 0 {
			r.Queue = nil
		} else {
			r.Queue = &qq
		}
	}
	if w := r.Warmup; w != nil {
		ww := *w
		if ww.BaselineRPS <= 0 {
			ww.BaselineRPS = 300
		}
		ww.BaselineRPS = clampInt(ww.BaselineRPS, 1, 10_000_000)
		if ww.MaxRPS <= 0 {
			ww.MaxRPS = ww.BaselineRPS * 10
		}
		ww.MaxRPS = clampInt(ww.MaxRPS, ww.BaselineRPS, 50_000_000)
		if ww.StepPct <= 0 {
			ww.StepPct = 20
		}
		ww.StepPct = clampInt(ww.StepPct, 1, 1000)
		if ww.IntervalMs <= 0 {
			ww.IntervalMs = 1000
		}
		ww.IntervalMs = clampInt(ww.IntervalMs, 100, 60_000)
		if ww.CooldownSeconds <= 0 {
			ww.CooldownSeconds = 30
		}
		ww.CooldownSeconds = clampInt(ww.CooldownSeconds, 1, 3600)
		r.Warmup = &ww
	}
	if r.Response != nil {
		resp := normalizeResponse(*r.Response)
		r.Response = &resp
	}
	return r
}

// ValidationError 字段级校验错误，Field 用 JSON 路径。
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func (e ValidationError) Error() string { return e.Field + ": " + e.Message }

// ValidateRule 校验 Normalize 修不了的问题（名字、网段、路径写法、键名）。
func ValidateRule(r Rule) []ValidationError {
	var errs []ValidationError
	add := func(field, msg string) { errs = append(errs, ValidationError{Field: field, Message: msg}) }
	if r.Name == "" {
		add("name", "请填写规则名称")
	} else if len([]rune(r.Name)) > 64 {
		add("name", "规则名称不超过 64 个字符")
	}
	if len([]rune(r.Description)) > 500 {
		add("description", "说明不超过 500 个字符")
	}
	for i, p := range r.Match.Paths {
		if err := validatePathPattern(p); err != "" {
			add(fmt.Sprintf("match.paths[%d]", i), err)
		}
	}
	for i, p := range r.Match.ExcludePaths {
		if err := validatePathPattern(p); err != "" {
			add(fmt.Sprintf("match.excludePaths[%d]", i), err)
		}
	}
	for i, c := range r.Match.CIDRs {
		if _, err := parsePrefix(c); err != nil {
			add(fmt.Sprintf("match.cidrs[%d]", i), "网段格式无效")
		}
	}
	for i, c := range r.Match.ExcludeCIDRs {
		if _, err := parsePrefix(c); err != nil {
			add(fmt.Sprintf("match.excludeCidrs[%d]", i), "网段格式无效")
		}
	}
	for i, m := range r.Match.Methods {
		switch m {
		case "GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD":
		default:
			add(fmt.Sprintf("match.methods[%d]", i), "不支持的方法")
		}
	}
	for i, l := range r.Limits {
		if (l.Key == KeyHeader || l.Key == KeyQuery) && l.Name == "" {
			add(fmt.Sprintf("limits[%d].name", i), "请填写请求头或参数名")
		}
	}
	if r.Mode != ModeBypass && len(r.Limits) == 0 && r.Concurrency == nil && r.Warmup == nil {
		add("limits", "至少配置一项限速、并发或预热")
	}
	if len(r.Limits) > 8 {
		add("limits", "每条规则最多 8 项限速")
	}
	return errs
}

func validatePathPattern(p string) string {
	if !strings.HasPrefix(p, "/") {
		return "路径必须以 / 开头"
	}
	if strings.Contains(p, "**") {
		return "不支持 **"
	}
	if i := strings.Index(p, "*"); i >= 0 {
		// * 只能作为整段出现
		segs := strings.Split(p, "/")
		for _, s := range segs {
			if strings.Contains(s, "*") && s != "*" {
				return "* 必须单独占一段"
			}
		}
	}
	if len(p) > 256 {
		return "路径过长"
	}
	return ""
}

// SortRules 按 Priority 字段之外的显式顺序排列时使用：调用方给的顺序即优先级。
// 这里只做稳定去重（同 ID 只保留第一条）。
func dedupeRules(rules []Rule) []Rule {
	seen := make(map[string]struct{}, len(rules))
	out := make([]Rule, 0, len(rules))
	for _, r := range rules {
		if _, ok := seen[r.ID]; ok {
			continue
		}
		seen[r.ID] = struct{}{}
		out = append(out, r)
	}
	return out
}

// ── 小工具 ──

func parsePrefix(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "/") {
		addr, err := netip.ParseAddr(s)
		if err != nil {
			return netip.Prefix{}, err
		}
		return netip.PrefixFrom(addr, addr.BitLen()), nil
	}
	p, err := netip.ParsePrefix(s)
	if err != nil {
		return netip.Prefix{}, err
	}
	return p.Masked(), nil
}

func normalizePaths(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, p := range in {
		p = strings.ToLower(strings.TrimSpace(p))
		if p == "" {
			continue
		}
		if !strings.HasPrefix(p, "/") {
			p = "/" + p
		}
		if len(p) > 1 && strings.HasSuffix(p, "/") {
			p = strings.TrimRight(p, "/")
			if p == "" {
				p = "/"
			}
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func normalizeUpper(in []string) []string {
	out := normalizeTrim(in)
	for i := range out {
		out[i] = strings.ToUpper(out[i])
	}
	sort.Strings(out)
	return out
}

func normalizeLower(in []string) []string {
	out := normalizeTrim(in)
	for i := range out {
		out[i] = strings.ToLower(out[i])
	}
	return out
}

func normalizeTrim(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
