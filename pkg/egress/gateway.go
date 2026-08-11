package egress

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// ErrRejected 规则判定为拒绝时返回。调用方可用 errors.Is 区分「被策略拦下」
// 与「网络不通」，前者不该触发重试与告警。
var ErrRejected = errors.New("egress: 目标被出海策略拒绝")

// Gateway 出海网关：持有一份可原子替换的路由快照，对外提供
// DialContext / Transport / Client 三种出口。
//
// 所有读路径（路由判定、拨号）都只读快照指针，不加锁；
// 写路径（Reload）在锁内构造新快照后一次性替换，因此热重载对在途请求无感。
type Gateway struct {
	log  *zap.Logger
	snap atomic.Pointer[snapshot]

	mu         sync.Mutex
	transports []*managedTransport
	version    atomic.Uint64

	counters gatewayCounters

	startOnce    sync.Once
	healthCancel context.CancelFunc
	healthDone   chan struct{}
	closed       atomic.Bool

	// gracePeriod 旧端点在重载后延迟关闭的时间，避免掐断在途连接。
	gracePeriod time.Duration
}

type gatewayCounters struct {
	routed   atomic.Uint64
	proxied  atomic.Uint64
	direct   atomic.Uint64
	rejected atomic.Uint64
	failed   atomic.Uint64
}

type snapshot struct {
	cfg              Config
	endpointsByName  map[string]*Endpoint
	endpoints        []*Endpoint
	rules            []*compiledRule
	defaultEndpoints []*Endpoint
	defaultRR        atomic.Uint64

	version          uint64
	loadedAt         time.Time
	allowUnhealthy   bool
	passive          bool
	cooldown         time.Duration
	failureThreshold int
	successThreshold int
	probeTimeout     time.Duration
	probeInterval    time.Duration
	dialTimeout      time.Duration
}

// New 用给定配置构造网关。配置非法时返回错误，调用方应保留旧网关。
func New(cfg Config, log *zap.Logger) (*Gateway, error) {
	if log == nil {
		log = zap.NewNop()
	}
	g := &Gateway{log: log, gracePeriod: 60 * time.Second}
	if err := g.Reload(cfg); err != nil {
		return nil, err
	}
	return g, nil
}

// NewDisabled 返回一个「全部直连」的网关，用于未配置出海时的默认形态。
func NewDisabled(log *zap.Logger) *Gateway {
	g, err := New(Config{Enabled: false, Source: "disabled"}, log)
	if err != nil {
		// 空配置不可能校验失败；真失败了说明包内不变式被破坏。
		panic(fmt.Sprintf("egress: 构造禁用网关失败: %v", err))
	}
	return g
}

// Reload 用新配置原子替换路由快照。
//
// 失败时旧快照保持不变——出海链路宁可停在旧配置上，也不能因为一次误配
// 就把所有境外调用打回直连。
func (g *Gateway) Reload(cfg Config) error {
	normalized := cfg.Normalize()
	if err := normalized.Validate(); err != nil {
		return err
	}
	endpointsByName, endpoints, err := buildEndpoints(normalized)
	if err != nil {
		return err
	}
	rules, err := compileRules(normalized, endpointsByName)
	if err != nil {
		return err
	}
	defaults := make([]*Endpoint, 0, len(normalized.DefaultEndpoints))
	for _, name := range normalized.DefaultEndpoints {
		endpoint, ok := endpointsByName[name]
		if !ok {
			return fmt.Errorf("默认端点 %s 不存在", name)
		}
		defaults = append(defaults, endpoint)
	}

	next := &snapshot{
		cfg:              normalized,
		endpointsByName:  endpointsByName,
		endpoints:        endpoints,
		rules:            rules,
		defaultEndpoints: defaults,
		version:          g.version.Add(1),
		loadedAt:         time.Now(),
		allowUnhealthy:   boolValue(normalized.Health.AllowUnhealthy, true),
		passive:          normalized.Health.PassiveEnabled,
		cooldown:         time.Duration(normalized.Health.CooldownSeconds) * time.Second,
		failureThreshold: normalized.Health.FailureThreshold,
		successThreshold: normalized.Health.SuccessThreshold,
		probeTimeout:     time.Duration(normalized.Health.TimeoutSeconds) * time.Second,
		probeInterval:    time.Duration(normalized.Health.IntervalSeconds) * time.Second,
		dialTimeout:      normalized.dialTimeout(),
	}

	g.mu.Lock()
	previous := g.snap.Swap(next)
	transports := append([]*managedTransport(nil), g.transports...)
	g.mu.Unlock()

	// 已建立的空闲连接可能指向刚被删掉的端点，必须让它们重新走一遍路由。
	for _, t := range transports {
		t.transport.CloseIdleConnections()
	}
	if previous != nil {
		g.retireEndpoints(previous.endpoints, next.endpointsByName)
	}
	g.log.Info("出海网关配置已加载",
		zap.Bool("enabled", normalized.Enabled),
		zap.String("source", normalized.Source),
		zap.Int("endpoints", len(endpoints)),
		zap.Int("rules", len(rules)),
		zap.Uint64("version", next.version),
	)
	return nil
}

// retireEndpoints 延迟关闭被替换掉的端点。
// SSH 这类端点持有长连接，立刻 Close 会掐断正在传输的请求。
func (g *Gateway) retireEndpoints(old []*Endpoint, keep map[string]*Endpoint) {
	stale := make([]*Endpoint, 0, len(old))
	for _, endpoint := range old {
		if current, ok := keep[endpoint.cfg.Name]; ok && current == endpoint {
			continue
		}
		stale = append(stale, endpoint)
	}
	if len(stale) == 0 {
		return
	}
	if g.gracePeriod <= 0 {
		for _, endpoint := range stale {
			endpoint.close()
		}
		return
	}
	time.AfterFunc(g.gracePeriod, func() {
		for _, endpoint := range stale {
			endpoint.close()
		}
	})
}

// Config 返回当前生效的配置副本。
func (g *Gateway) Config() Config {
	snap := g.snap.Load()
	if snap == nil {
		return Config{}
	}
	return snap.cfg.Clone()
}

// ReloadMeta 返回配置版本与加载时间，与平台内其它可热重载组件保持同一约定。
func (g *Gateway) ReloadMeta() (uint64, time.Time) {
	snap := g.snap.Load()
	if snap == nil {
		return 0, time.Time{}
	}
	return snap.version, snap.loadedAt
}

// Enabled 网关是否启用。关闭时所有出站一律直连。
func (g *Gateway) Enabled() bool {
	snap := g.snap.Load()
	return snap != nil && snap.cfg.Enabled
}

// ValidateConfig 只校验不生效，供管理端「保存前检查」使用。
func (g *Gateway) ValidateConfig(cfg Config) error {
	normalized := cfg.Normalize()
	if err := normalized.Validate(); err != nil {
		return err
	}
	// 校验必须走到真正的构造：协议参数（私钥、加密方式、证书）只有在
	// 构造拨号器时才会被解析，光看字段非空是拦不住坏配置的。
	endpointsByName, endpoints, err := buildEndpoints(normalized)
	if err != nil {
		return err
	}
	if _, err := compileRules(normalized, endpointsByName); err != nil {
		return err
	}
	for _, endpoint := range endpoints {
		endpoint.close()
	}
	return nil
}

// Route 判定一次出站连接该怎么走。它是纯函数式的（除计数器外无副作用），
// 因此管理端的「规则解释」可以直接复用同一条代码路径。
func (g *Gateway) Route(target Target) Decision {
	snap := g.snap.Load()
	target = target.normalized()
	g.counters.routed.Add(1)

	if snap == nil || !snap.cfg.Enabled {
		g.counters.direct.Add(1)
		return Decision{Action: ActionDirect, Rule: "disabled", Reason: "网关未启用"}
	}

	ip, hasIP := netip.Addr{}, false
	if parsed, err := netip.ParseAddr(target.Host); err == nil {
		ip, hasIP = parsed.Unmap(), true
	}

	for _, rule := range snap.rules {
		reason, ok := rule.match(target, ip, hasIP)
		if !ok {
			continue
		}
		rule.matched.Add(1)
		return g.decide(snap, rule.cfg.Name, reason, rule.cfg.Action, rule.endpoints, rule.cfg.Strategy, &rule.rr)
	}
	return g.decide(snap, "default", "未命中任何规则", snap.cfg.DefaultAction, snap.defaultEndpoints, snap.cfg.DefaultStrategy, &snap.defaultRR)
}

// decide 由动作与候选端点得出最终决策。它没有副作用（除推进传入的轮询游标外），
// 因此管理端的 Explain 可以传一个临时游标复用同一段判定逻辑。
func decide(snap *snapshot, ruleName, reason string, action Action, candidates []*Endpoint, strategy Strategy, rr *atomic.Uint64) Decision {
	switch action {
	case ActionReject:
		return Decision{Action: ActionReject, Rule: ruleName, Reason: reason}
	case ActionDirect:
		return Decision{Action: ActionDirect, Rule: ruleName, Reason: reason}
	}
	endpoint, err := pickEndpoint(candidates, strategy, rr, snap.allowUnhealthy)
	if err != nil {
		return Decision{Action: ActionProxy, Rule: ruleName, Reason: reason, Err: err}
	}
	return Decision{
		Action:      ActionProxy,
		Rule:        ruleName,
		Reason:      reason,
		Endpoint:    endpoint,
		Chain:       endpoint.chain(),
		HTTPForward: endpoint.cfg.HTTPForwardMode,
	}
}

func (g *Gateway) decide(snap *snapshot, ruleName, reason string, action Action, candidates []*Endpoint, strategy Strategy, rr *atomic.Uint64) Decision {
	decision := decide(snap, ruleName, reason, action, candidates, strategy, rr)
	switch {
	case decision.Action == ActionReject:
		g.counters.rejected.Add(1)
	case decision.Action == ActionDirect:
		g.counters.direct.Add(1)
	case decision.Err != nil:
		g.counters.failed.Add(1)
	default:
		g.counters.proxied.Add(1)
	}
	return decision
}

// DialContext 按路由表建立一条到 address 的 TCP 连接。
// 供 SMTP / LDAP 等非 HTTP 出站复用。
func (g *Gateway) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return g.dialWithProfile(ctx, network, address, Profile{})
}

func (g *Gateway) dialWithProfile(ctx context.Context, network, address string, profile Profile) (net.Conn, error) {
	host, port, err := splitTarget(address)
	if err != nil {
		return nil, err
	}
	decision := g.Route(Target{Host: host, Port: port, Scheme: schemeForPort(port), Profile: profile.Name})
	return g.dialDecision(ctx, decision, network, address, profile)
}

func (g *Gateway) dialDecision(ctx context.Context, decision Decision, network, address string, profile Profile) (net.Conn, error) {
	switch decision.Action {
	case ActionReject:
		return nil, fmt.Errorf("%w: %s（规则 %s）", ErrRejected, address, decision.Rule)
	case ActionDirect:
		return g.dialDirect(ctx, network, address, profile)
	}
	if decision.Err != nil {
		return nil, fmt.Errorf("出海路由失败（规则 %s）: %w", decision.Rule, decision.Err)
	}
	if decision.Endpoint == nil {
		return nil, fmt.Errorf("出海路由失败（规则 %s）: 未选出端点", decision.Rule)
	}
	snap := g.snap.Load()
	passive, cooldown, threshold := false, time.Duration(0), 1
	if snap != nil {
		passive, cooldown, threshold = snap.passive, snap.cooldown, snap.failureThreshold
	}
	return decision.Endpoint.dial(ctx, network, address, passive, cooldown, threshold)
}

// dialDirect 直连出站。开启 BlockPrivateTargets 时先做 SSRF 校验：
// 走代理时目标 IP 由对端解析，这层校验只在直连路径上才有意义。
func (g *Gateway) dialDirect(ctx context.Context, network, address string, profile Profile) (net.Conn, error) {
	timeout := profile.DialTimeout
	if timeout <= 0 {
		if snap := g.snap.Load(); snap != nil {
			timeout = snap.dialTimeout
		}
	}
	if timeout <= 0 {
		timeout = DefaultDialTimeout
	}
	dialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	if !profile.BlockPrivateTargets {
		return dialer.DialContext(ctx, network, address)
	}
	return dialGuarded(ctx, dialer, network, address)
}

// Endpoint 按名字取端点，供管理端「指定端点自测」使用。
func (g *Gateway) Endpoint(name string) (*Endpoint, bool) {
	snap := g.snap.Load()
	if snap == nil {
		return nil, false
	}
	endpoint, ok := snap.endpointsByName[name]
	return endpoint, ok
}

// Close 停止健康检查并释放所有端点持有的长连接。
func (g *Gateway) Close() {
	if !g.closed.CompareAndSwap(false, true) {
		return
	}
	if g.healthCancel != nil {
		g.healthCancel()
		<-g.healthDone
	}
	if snap := g.snap.Load(); snap != nil {
		for _, endpoint := range snap.endpoints {
			endpoint.close()
		}
	}
}

func schemeForPort(port int) string {
	switch port {
	case 80:
		return "http"
	case 443:
		return "https"
	default:
		return "tcp"
	}
}

// blockedOutboundPrefixes 直连 SSRF 校验用的保留网段（回环/私有/链路本地由
// net.IP 自带方法判定，这里补齐它漏掉的几段）。
var blockedOutboundPrefixes = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"), // 运营商级 NAT
	netip.MustParsePrefix("192.0.0.0/24"),  // IETF 协议保留
	netip.MustParsePrefix("198.18.0.0/15"), // 基准测试网络
	netip.MustParsePrefix("::ffff:0:0/96"), // IPv4 映射地址，避免绕过
}

// IsBlockedOutboundIP 判断 IP 是否属于禁止直连的内网/保留地址。
func IsBlockedOutboundIP(ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return true
	}
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	if addr.Is4In6() {
		addr = addr.Unmap()
		if addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() {
			return true
		}
	}
	for _, prefix := range blockedOutboundPrefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// dialGuarded 解析目标并逐个校验 IP 后再连接，防止 DNS 指向内网。
func dialGuarded(ctx context.Context, dialer *net.Dialer, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("解析目标地址失败: %w", err)
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("解析目标主机失败: %w", err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("目标主机 %s 没有可用地址", host)
	}
	for _, item := range ips {
		if IsBlockedOutboundIP(item.IP) {
			return nil, fmt.Errorf("目标 %s 解析到本机或私有网络，已拒绝", host)
		}
	}
	// 用已校验的 IP 直接连接，避免「校验后重新解析」被 DNS rebinding 钻空子。
	return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].IP.String(), port))
}
