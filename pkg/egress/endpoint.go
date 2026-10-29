package egress

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Endpoint 是端点的运行期形态：配置 + 协议拨号器 + 健康状态 + 计数器。
//
// 一次重载会重建全部 Endpoint，旧对象随快照一起被丢弃；因此这里的状态
// （健康、统计）天然按配置版本隔离，不需要额外的失效逻辑。
type Endpoint struct {
	cfg         EndpointConfig
	dialer      Dialer
	via         *Endpoint
	dialTimeout time.Duration
	netDialer   *net.Dialer

	health healthState
	// udpHealth UDP 单独的健康状态：TCP 通而 UDP 不通（服务端没开 UDP）很常见，
	// 不能让 UDP 的失败把 TCP 流量也赶走。
	udpHealth healthState
	stats     endpointCounters
}

type healthState struct {
	mu            sync.Mutex
	healthy       bool
	failures      int
	successes     int
	lastError     string
	lastCheckedAt time.Time
	cooldownUntil time.Time
	latency       time.Duration
}

type endpointCounters struct {
	dials     atomic.Uint64
	successes atomic.Uint64
	failures  atomic.Uint64
	bytesIn   atomic.Uint64
	bytesOut  atomic.Uint64
	// UDP：建立过的会话数与收发的报文数。
	udpSessions atomic.Uint64
	packetsIn   atomic.Uint64
	packetsOut  atomic.Uint64
}

func newEndpoint(cfg EndpointConfig, defaultDialTimeout time.Duration) (*Endpoint, error) {
	dialer, err := buildDialer(cfg)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(cfg.DialTimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = defaultDialTimeout
	}
	if timeout <= 0 {
		timeout = DefaultDialTimeout
	}
	e := &Endpoint{
		cfg:         cfg,
		dialer:      dialer,
		dialTimeout: timeout,
		netDialer:   &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second},
	}
	// 新端点默认视为健康：主动探测尚未跑过就先判死，会让每次重载后
	// 出现一个「所有线路不可用」的窗口。
	e.health.healthy = true
	e.udpHealth.healthy = true
	return e, nil
}

// Name 端点名。
func (e *Endpoint) Name() string { return e.cfg.Name }

// Protocol 端点协议。
func (e *Endpoint) Protocol() Protocol { return e.cfg.Protocol }

// Config 返回端点配置副本（含密钥，调用方负责脱敏）。
func (e *Endpoint) Config() EndpointConfig { return e.cfg.Clone() }

func (e *Endpoint) enabled() bool { return boolValue(e.cfg.Enabled, true) }

// SupportsUDP 这条线路能不能承载 UDP：协议支持、没有显式关掉，
// 且 UDP 要以 UDP 发给服务端的协议（SOCKS5、Shadowsocks）上一跳也得能承载 UDP。
func (e *Endpoint) SupportsUDP() bool {
	if e.cfg.UDP != nil && !*e.cfg.UDP {
		return false
	}
	switch ProtocolUDP(e.cfg.Protocol) {
	case UDPOverStream:
		return true
	case UDPOverUDP:
		return e.via == nil || e.via.SupportsUDP()
	default:
		return false
	}
}

func (e *Endpoint) healthFor(udp bool) *healthState {
	if udp {
		return &e.udpHealth
	}
	return &e.health
}

// chain 返回从本端点回溯到最外层跳板的名字链，用于展示与排障。
func (e *Endpoint) chain() []string {
	out := []string{e.cfg.Name}
	for cur := e.via; cur != nil; cur = cur.via {
		out = append(out, cur.cfg.Name)
	}
	return out
}

// dial 经本端点连接目标地址。
//
// base 的构造是递归的：有 via 就交给上一跳，没有就落到系统直连。
// 统计与被动健康只记在**本端点**上——上一跳失败会同时反映在它自己的计数里。
func (e *Endpoint) dial(ctx context.Context, network, address string, passive bool, cooldown time.Duration, threshold int) (net.Conn, error) {
	e.stats.dials.Add(1)
	start := time.Now()

	dialCtx := ctx
	if _, ok := ctx.Deadline(); !ok && e.dialTimeout > 0 {
		var cancel context.CancelFunc
		dialCtx, cancel = context.WithTimeout(ctx, e.dialTimeout)
		defer cancel()
	}

	udp := isUDPNetwork(network)
	conn, err := e.dialer.DialContext(dialCtx, e.baseDial, network, address)
	if err != nil {
		e.stats.failures.Add(1)
		if passive {
			e.markFailure(e.healthFor(udp), err, cooldown, threshold)
		}
		return nil, err
	}
	e.stats.successes.Add(1)
	if passive {
		e.markSuccess(e.healthFor(udp), time.Since(start))
	}
	if udp {
		e.stats.udpSessions.Add(1)
		datagram := asDatagram(conn)
		return &countingPacketConn{
			Conn: datagram, pc: datagram.(net.PacketConn),
			in: &e.stats.bytesIn, out: &e.stats.bytesOut, pin: &e.stats.packetsIn, pout: &e.stats.packetsOut,
		}, nil
	}
	return &countingConn{Conn: conn, in: &e.stats.bytesIn, out: &e.stats.bytesOut}, nil
}

// dialSelf 连到端点自身的地址（不做协议握手）。
// HTTP 转发模式下由 net/http 自己完成 CONNECT，我们只负责把链路铺到代理门口。
func (e *Endpoint) dialSelf(ctx context.Context, network string) (net.Conn, error) {
	return e.baseDial(ctx, network, e.cfg.Address)
}

func (e *Endpoint) baseDial(ctx context.Context, network, address string) (net.Conn, error) {
	if e.via != nil {
		return e.via.dialer.DialContext(ctx, e.via.baseDial, network, address)
	}
	return e.netDialer.DialContext(ctx, network, address)
}

// available 判断端点当前能否参与 TCP 选路。
func (e *Endpoint) available(now time.Time, allowUnhealthy bool) bool {
	return e.enabled() && e.health.usable(now, allowUnhealthy)
}

// availableFor 按传输层判断能否参与选路；UDP 看 UDP 的健康状态。
func (e *Endpoint) availableFor(now time.Time, udp bool) bool {
	if udp && !e.SupportsUDP() {
		return false
	}
	return e.enabled() && e.healthFor(udp).usable(now, false)
}

// latencyFor 选「最低延迟」时用的延迟；UDP 用 UDP 探测的结果。
func (e *Endpoint) latencyFor(udp bool) time.Duration {
	h := e.healthFor(udp)
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.latency
}

func (h *healthState) usable(now time.Time, allowUnhealthy bool) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if now.Before(h.cooldownUntil) {
		return false
	}
	return h.healthy || allowUnhealthy
}

func (e *Endpoint) markSuccess(h *healthState, latency time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.failures = 0
	h.successes++
	h.healthy = true
	h.lastError = ""
	h.cooldownUntil = time.Time{}
	// 指数加权平均：单次抖动不至于让「最低延迟」策略来回横跳。
	if h.latency == 0 {
		h.latency = latency
	} else {
		h.latency = (h.latency*3 + latency) / 4
	}
}

func (e *Endpoint) markFailure(h *healthState, err error, cooldown time.Duration, threshold int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.successes = 0
	h.failures++
	if err != nil {
		h.lastError = err.Error()
	}
	if threshold <= 0 {
		threshold = 1
	}
	if h.failures >= threshold {
		h.healthy = false
		if cooldown > 0 {
			h.cooldownUntil = time.Now().Add(cooldown)
		}
	}
}

// recordProbe 写入主动探测结果，阈值语义与被动熔断一致。
func (e *Endpoint) recordProbe(latency time.Duration, err error, failureThreshold, successThreshold int) {
	e.health.record(latency, err, failureThreshold, successThreshold)
}

// recordUDPProbe 写入 UDP 探测结果。
func (e *Endpoint) recordUDPProbe(latency time.Duration, err error, failureThreshold, successThreshold int) {
	e.udpHealth.record(latency, err, failureThreshold, successThreshold)
}

func (h *healthState) record(latency time.Duration, err error, failureThreshold, successThreshold int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastCheckedAt = time.Now()
	if err != nil {
		h.successes = 0
		h.failures++
		h.lastError = err.Error()
		if failureThreshold <= 0 {
			failureThreshold = 1
		}
		if h.failures >= failureThreshold {
			h.healthy = false
		}
		return
	}
	h.failures = 0
	h.successes++
	h.lastError = ""
	if successThreshold <= 0 {
		successThreshold = 1
	}
	if h.successes >= successThreshold {
		h.healthy = true
		h.cooldownUntil = time.Time{}
	}
	if h.latency == 0 {
		h.latency = latency
	} else {
		h.latency = (h.latency*3 + latency) / 4
	}
}

func (e *Endpoint) snapshotHealth() (healthy bool, latency time.Duration, failures int, lastErr string, checkedAt, cooldownUntil time.Time) {
	return e.health.snapshot()
}

func (h *healthState) snapshot() (healthy bool, latency time.Duration, failures int, lastErr string, checkedAt, cooldownUntil time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.healthy, h.latency, h.failures, h.lastError, h.lastCheckedAt, h.cooldownUntil
}

// close 释放协议拨号器持有的长连接（目前只有 SSH 端点有）。
func (e *Endpoint) close() {
	if closer, ok := e.dialer.(io.Closer); ok {
		_ = closer.Close()
	}
}

// countingConn 统计单端点的出入字节数，用于容量与费用观测。
type countingConn struct {
	net.Conn
	in  *atomic.Uint64
	out *atomic.Uint64
}

func (c *countingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.in.Add(uint64(n))
	}
	return n, err
}

func (c *countingConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n > 0 {
		c.out.Add(uint64(n))
	}
	return n, err
}

// buildEndpoints 构造端点集合并接好 via 链。
func buildEndpoints(cfg Config) (map[string]*Endpoint, []*Endpoint, error) {
	byName := make(map[string]*Endpoint, len(cfg.Endpoints))
	order := make([]*Endpoint, 0, len(cfg.Endpoints))
	for _, item := range cfg.Endpoints {
		endpoint, err := newEndpoint(item, cfg.dialTimeout())
		if err != nil {
			return nil, nil, fmt.Errorf("端点 %s 初始化失败: %w", item.Name, err)
		}
		byName[item.Name] = endpoint
		order = append(order, endpoint)
	}
	for _, endpoint := range order {
		if endpoint.cfg.Via == "" {
			continue
		}
		via, ok := byName[endpoint.cfg.Via]
		if !ok {
			return nil, nil, fmt.Errorf("端点 %s 的 via 指向不存在的端点 %s", endpoint.cfg.Name, endpoint.cfg.Via)
		}
		endpoint.via = via
	}
	return byName, order, nil
}
