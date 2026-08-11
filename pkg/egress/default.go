package egress

import (
	"context"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
)

// 全局默认网关。
//
// 出海是横切关注点：几十处业务代码都要出网，逐个透传 *Gateway 会污染每一层
// 构造函数签名。这里采用与 http.DefaultTransport 相同的取舍——提供一个进程级
// 默认实例，同时保留 Gateway 上的显式方法供测试和多实例场景使用。
var (
	defaultGateway atomic.Pointer[Gateway]
	fallbackOnce   sync.Once
	fallbackGW     *Gateway
)

// SetDefault 装配全局默认网关，通常在 bootstrap 里调用一次。
// 已经由 NewClient 创建出去的客户端会在下一次请求时自动切到新网关。
func SetDefault(gw *Gateway) { defaultGateway.Store(gw) }

// Default 返回全局默认网关；未装配时返回一个「全部直连」的空网关，
// 这样单元测试与 CLI 命令不需要任何初始化也能正常出网。
func Default() *Gateway {
	if gw := defaultGateway.Load(); gw != nil {
		return gw
	}
	fallbackOnce.Do(func() { fallbackGW = NewDisabled(nil) })
	return fallbackGW
}

// NewClient 返回绑定全局默认网关的 HTTP 客户端。业务侧只需要这一行。
//
//	client := egress.NewClient(egress.Profile{Name: "payment.stripe", Timeout: 15 * time.Second})
func NewClient(profile Profile) *http.Client {
	return &http.Client{
		Transport:     NewTransport(profile),
		Timeout:       profile.Timeout,
		CheckRedirect: profile.CheckRedirect,
	}
}

// NewTransport 返回绑定全局默认网关的 RoundTripper，
// 供需要自己拼 http.Client 或把 transport 交给第三方 SDK 的场景使用。
func NewTransport(profile Profile) http.RoundTripper {
	return &lazyRoundTripper{profile: profile}
}

// DialContext 用全局默认网关建立裸 TCP 连接（SMTP / LDAP 等非 HTTP 出站）。
func DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return Default().DialContext(ctx, network, address)
}

// Enabled 全局默认网关是否已启用出海路由。
func Enabled() bool { return Default().Enabled() }

type boundTransport struct {
	gw *Gateway
	rt http.RoundTripper
}

// lazyRoundTripper 把「解析默认网关」推迟到第一次请求。
//
// 业务服务在 bootstrap 早期就构造好了 http.Client，那时网关还没装配；
// 延迟绑定让这些客户端不必关心装配顺序，也能在 SetDefault 之后自动切换。
type lazyRoundTripper struct {
	profile Profile
	mu      sync.Mutex
	bound   atomic.Pointer[boundTransport]
}

func (l *lazyRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	gw := Default()
	if current := l.bound.Load(); current != nil && current.gw == gw {
		return current.rt.RoundTrip(req)
	}
	l.mu.Lock()
	current := l.bound.Load()
	if current == nil || current.gw != gw {
		previous := current
		current = &boundTransport{gw: gw, rt: gw.Transport(l.profile)}
		l.bound.Store(current)
		if previous != nil {
			closeIdle(previous.rt)
		}
	}
	l.mu.Unlock()
	return current.rt.RoundTrip(req)
}

// CloseIdleConnections 让 http.Client.CloseIdleConnections 能穿透下去。
func (l *lazyRoundTripper) CloseIdleConnections() {
	if current := l.bound.Load(); current != nil {
		closeIdle(current.rt)
	}
}

func closeIdle(rt http.RoundTripper) {
	if closer, ok := rt.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}
