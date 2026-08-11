package egress

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Profile 描述一个调用方的出站需求。
//
// Name 会作为路由条件参与规则匹配（MatchConfig.Profiles），
// 建议用 "模块.渠道" 的写法：payment.stripe / storage.s3 / oauth.google。
type Profile struct {
	Name    string
	Timeout time.Duration

	DialTimeout           time.Duration
	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration
	IdleConnTimeout       time.Duration
	MaxIdleConnsPerHost   int
	DisableKeepAlives     bool

	// BlockPrivateTargets 直连路径上启用 SSRF 校验（拒绝解析到内网的目标）。
	// 走代理时目标由对端解析，本地校验无从谈起，因此只对直连生效。
	BlockPrivateTargets bool

	// Base 以既有 transport 为模板（第三方 SDK 常自带一个），
	// 网关只接管它的 Proxy 与 DialContext，其余设置原样保留。
	Base            *http.Transport
	TLSClientConfig *tls.Config
	CheckRedirect   func(*http.Request, []*http.Request) error
}

type managedTransport struct {
	transport *http.Transport
}

type decisionKeyType struct{}

var decisionKey decisionKeyType

func withDecision(ctx context.Context, d *Decision) context.Context {
	return context.WithValue(ctx, decisionKey, d)
}

func decisionFrom(ctx context.Context) (*Decision, bool) {
	d, ok := ctx.Value(decisionKey).(*Decision)
	return d, ok
}

// Transport 返回一个受网关托管的 RoundTripper。
//
// 路由判定发生在 RoundTrip 入口而不是 DialContext 里：只有在那里才同时看得到
// scheme、Host 与调用方 Profile，判定结果随 context 下发给 Proxy 与 DialContext，
// 保证同一次请求的两个回调看到的是同一个决策。
func (g *Gateway) Transport(profile Profile) http.RoundTripper {
	return g.buildTransport(profile, true)
}

// buildTransport 的 managed 决定是否登记进 g.transports。
// 登记后的 transport 会在每次热重载时被清空空闲连接，代价是网关持有它到进程结束——
// 因此只有长期存活的业务客户端才登记，自测这类一次性客户端不登记，否则重复自测会把
// transport 列表撑爆。
func (g *Gateway) buildTransport(profile Profile, managed bool) http.RoundTripper {
	base := cloneBaseTransport(profile.Base)
	snap := g.snap.Load()

	applyTransportTimeouts(base, profile, snap)
	if profile.TLSClientConfig != nil {
		base.TLSClientConfig = profile.TLSClientConfig
	}
	base.DisableKeepAlives = profile.DisableKeepAlives

	base.Proxy = func(req *http.Request) (*url.URL, error) {
		if !g.Enabled() {
			// 网关未启用时保持标准库原有行为，继续尊重 HTTP_PROXY / HTTPS_PROXY / NO_PROXY。
			// 「引入出海网关」本身不应该变成一次行为变更；一旦启用，路由表就是唯一权威。
			// 例外是显式要求 SSRF 防护的调用方（如对象存储），它们此前就明确禁用了代理。
			if profile.BlockPrivateTargets {
				return nil, nil
			}
			return http.ProxyFromEnvironment(req)
		}
		decision, ok := decisionFrom(req.Context())
		if !ok || !decision.HTTPForward || decision.Endpoint == nil {
			return nil, nil
		}
		return httpProxyURL(decision.Endpoint), nil
	}
	base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		decision, ok := decisionFrom(ctx)
		if !ok {
			// 没有决策说明这次拨号不是经 RoundTrip 进来的（例如 HTTP/2 的
			// 连接预热），退回按地址路由。
			return g.dialWithProfile(ctx, network, address, profile)
		}
		if decision.HTTPForward && decision.Endpoint != nil {
			// 转发模式下 net/http 自己负责与代理对话，我们只把链路铺到代理门口。
			return decision.Endpoint.dialSelf(ctx, network)
		}
		return g.dialDecision(ctx, *decision, network, address, profile)
	}

	if managed {
		g.mu.Lock()
		g.transports = append(g.transports, &managedTransport{transport: base})
		g.mu.Unlock()
	}
	return &gatewayRoundTripper{gw: g, profile: profile, base: base}
}

// Client 返回绑定本网关的 HTTP 客户端。
func (g *Gateway) Client(profile Profile) *http.Client {
	return g.client(profile, true)
}

func (g *Gateway) client(profile Profile, managed bool) *http.Client {
	return &http.Client{
		Transport:     g.buildTransport(profile, managed),
		Timeout:       profile.Timeout,
		CheckRedirect: profile.CheckRedirect,
	}
}

func cloneBaseTransport(base *http.Transport) *http.Transport {
	if base != nil {
		return base.Clone()
	}
	if def, ok := http.DefaultTransport.(*http.Transport); ok {
		return def.Clone()
	}
	return &http.Transport{}
}

func applyTransportTimeouts(t *http.Transport, profile Profile, snap *snapshot) {
	tlsTimeout := profile.TLSHandshakeTimeout
	headerTimeout := profile.ResponseHeaderTimeout
	idleTimeout := profile.IdleConnTimeout
	maxIdle := profile.MaxIdleConnsPerHost
	if snap != nil {
		if tlsTimeout <= 0 {
			tlsTimeout = time.Duration(snap.cfg.TLSHandshakeTimeoutMS) * time.Millisecond
		}
		if headerTimeout <= 0 {
			headerTimeout = time.Duration(snap.cfg.ResponseHeaderTimeoutMS) * time.Millisecond
		}
		if idleTimeout <= 0 {
			idleTimeout = time.Duration(snap.cfg.IdleConnTimeoutMS) * time.Millisecond
		}
		if maxIdle <= 0 {
			maxIdle = snap.cfg.MaxIdleConnsPerHost
		}
	}
	if tlsTimeout > 0 {
		t.TLSHandshakeTimeout = tlsTimeout
	}
	if headerTimeout > 0 {
		t.ResponseHeaderTimeout = headerTimeout
	}
	if idleTimeout > 0 {
		t.IdleConnTimeout = idleTimeout
	}
	if maxIdle > 0 {
		t.MaxIdleConnsPerHost = maxIdle
	}
}

func httpProxyURL(endpoint *Endpoint) *url.URL {
	u := &url.URL{Scheme: "http", Host: endpoint.cfg.Address}
	if endpoint.cfg.Username != "" || endpoint.cfg.Password != "" {
		u.User = url.UserPassword(endpoint.cfg.Username, endpoint.cfg.Password)
	}
	return u
}

type gatewayRoundTripper struct {
	gw      *Gateway
	profile Profile
	base    *http.Transport
}

func (rt *gatewayRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	target := Target{
		Host:    req.URL.Hostname(),
		Port:    portFromURL(req.URL),
		Scheme:  req.URL.Scheme,
		Profile: rt.profile.Name,
	}
	decision := rt.gw.Route(target)
	switch {
	case decision.Action == ActionReject:
		return nil, fmt.Errorf("%w: %s（规则 %s）", ErrRejected, req.URL.Host, decision.Rule)
	case decision.Action == ActionProxy && decision.Err != nil:
		return nil, fmt.Errorf("出海路由失败（规则 %s）: %w", decision.Rule, decision.Err)
	}
	// absolute-URI 转发只对明文 http 有意义；https 必须走 CONNECT 隧道。
	decision.HTTPForward = decision.HTTPForward && decision.Action == ActionProxy && req.URL.Scheme == "http"

	return rt.base.RoundTrip(req.WithContext(withDecision(req.Context(), &decision)))
}

// CloseIdleConnections 让 http.Client.CloseIdleConnections 能穿透到底层 transport。
func (rt *gatewayRoundTripper) CloseIdleConnections() { rt.base.CloseIdleConnections() }

func portFromURL(u *url.URL) int {
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err == nil {
			return n
		}
	}
	switch u.Scheme {
	case "https", "wss":
		return 443
	default:
		return 80
	}
}
