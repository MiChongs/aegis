package egress

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"golang.org/x/net/proxy"
)

// socksDialer 基于官方 golang.org/x/net/proxy 实现 SOCKS5。
//
// 协议帧、认证协商、错误码全部由 x/net 负责；本类型只做两件它不管的事：
//  1. 把上一跳（Via / TLS）包装成 proxy.ContextDialer 交给它当 forward，实现代理链；
//  2. 区分 socks5 与 socks5h —— x/net 一律把域名交给代理解析（即 socks5h 语义），
//     所以 socks5 由这里先本地解析成 IP 再交给库。
type socksDialer struct {
	protocol  Protocol
	address   string
	auth      *proxy.Auth
	tlsConfig *tls.Config
	tlsHost   string
	resolver  *net.Resolver
}

func newSocksDialer(cfg EndpointConfig) (Dialer, error) {
	tlsCfg, err := buildTLSConfig(cfg.TLS)
	if err != nil {
		return nil, err
	}
	d := &socksDialer{
		protocol:  cfg.Protocol,
		address:   cfg.Address,
		tlsConfig: tlsCfg,
		resolver:  net.DefaultResolver,
	}
	if cfg.Username != "" || cfg.Password != "" {
		if len(cfg.Username) > 255 || len(cfg.Password) > 255 {
			return nil, fmt.Errorf("socks 用户名/密码长度不能超过 255 字节")
		}
		d.auth = &proxy.Auth{User: cfg.Username, Password: cfg.Password}
	}
	if tlsCfg != nil {
		if host, _, splitErr := net.SplitHostPort(cfg.Address); splitErr == nil {
			d.tlsHost = host
		}
	}
	return d, nil
}

func (d *socksDialer) DialContext(ctx context.Context, base BaseDialFunc, network, address string) (net.Conn, error) {
	if err := requireTCP(network, d.protocol); err != nil {
		return nil, err
	}
	host, port, err := splitTarget(address)
	if err != nil {
		return nil, err
	}
	if d.protocol == ProtocolSOCKS5 {
		// socks5（非 h）语义要求本地解析后送 IP 给代理。
		if _, parseErr := netip.ParseAddr(host); parseErr != nil {
			resolved, resolveErr := d.resolveOne(ctx, host)
			if resolveErr != nil {
				return nil, resolveErr
			}
			address = net.JoinHostPort(resolved.String(), strconv.Itoa(port))
		}
	}

	forward := &hopDialer{base: base, tlsConfig: d.tlsConfig, tlsHost: d.tlsHost}
	socksProxy, err := proxy.SOCKS5("tcp", d.address, d.auth, forward)
	if err != nil {
		return nil, fmt.Errorf("构造 SOCKS5 拨号器失败: %w", err)
	}
	ctxDialer, ok := socksProxy.(proxy.ContextDialer)
	if !ok {
		return nil, fmt.Errorf("SOCKS5 拨号器不支持 context")
	}
	conn, err := ctxDialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, fmt.Errorf("经 SOCKS5 代理 %s 连接 %s 失败: %w", d.address, address, err)
	}
	return conn, nil
}

func (d *socksDialer) resolveOne(ctx context.Context, host string) (netip.Addr, error) {
	addrs, err := d.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("解析目标域名 %s 失败: %w", host, err)
	}
	if len(addrs) == 0 {
		return netip.Addr{}, fmt.Errorf("域名 %s 没有可用地址", host)
	}
	return addrs[0].Unmap(), nil
}

// hopDialer 把网关的「上一跳」适配成 proxy.Dialer / proxy.ContextDialer，
// 顺带在需要时完成到代理服务器的 TLS（SOCKS over TLS）。
//
// x/net/proxy 检测到 forward 实现了 ContextDialer 就会走带 ctx 的路径，
// 因此取消与超时能一路穿透到最底层的 TCP 连接。
type hopDialer struct {
	base      BaseDialFunc
	tlsConfig *tls.Config
	tlsHost   string
}

var (
	_ proxy.Dialer        = (*hopDialer)(nil)
	_ proxy.ContextDialer = (*hopDialer)(nil)
)

func (h *hopDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := h.base(ctx, network, address)
	if err != nil {
		return nil, err
	}
	if h.tlsConfig == nil {
		return conn, nil
	}
	return wrapTLS(ctx, conn, h.tlsConfig, h.tlsHost)
}

func (h *hopDialer) Dial(network, address string) (net.Conn, error) {
	return h.DialContext(context.Background(), network, address)
}

// socksSchemeProtocol 把 URL scheme 映射到内部协议，供 DSL 解析复用。
func socksSchemeProtocol(scheme string) (Protocol, bool) {
	switch strings.ToLower(scheme) {
	case "socks5":
		return ProtocolSOCKS5, true
	case "socks5h", "socks":
		return ProtocolSOCKS5H, true
	default:
		return "", false
	}
}
