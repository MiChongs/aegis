package egress

import (
	"context"
	"fmt"
	"net"
	"slices"
	"sync"
)

// BaseDialFunc 把「连到某个地址」这件事抽象出来。
// 协议实现用它连到自己的代理服务器，而不关心那一跳是直连还是又套了一层代理——
// 端点链（EndpointConfig.Via）就是靠这个函数递归拼起来的。
type BaseDialFunc func(ctx context.Context, network, address string) (net.Conn, error)

// Dialer 一种出海协议的握手实现。
//
// 契约：实现方先用 base 连到自己的服务端（地址来自构造期的 EndpointConfig），
// 完成协议握手后返回一条「写进去就等于写给 address」的连接。
// 返回的 conn 由调用方负责关闭；握手失败时实现方必须自己关掉半开的底层连接。
type Dialer interface {
	DialContext(ctx context.Context, base BaseDialFunc, network, address string) (net.Conn, error)
}

// DialerFunc 让普通函数满足 Dialer。
type DialerFunc func(ctx context.Context, base BaseDialFunc, network, address string) (net.Conn, error)

// DialContext 实现 Dialer。
func (f DialerFunc) DialContext(ctx context.Context, base BaseDialFunc, network, address string) (net.Conn, error) {
	return f(ctx, base, network, address)
}

// DialerFactory 由端点配置构造协议拨号器。构造期做完所有能提前做的解析
// （密钥、证书、口令派生），让每次拨号只剩网络 I/O。
type DialerFactory func(cfg EndpointConfig) (Dialer, error)

var (
	registryMu sync.RWMutex
	registry   = map[Protocol]DialerFactory{}
)

// RegisterProtocol 注册一种协议实现。
//
// 这是本包对外的扩展点：VMess / Hysteria / 自研隧道都可以在
// 不改动网关核心的前提下接进来，注册后即可在配置里当作普通端点使用。
// 重复注册同一协议会覆盖旧实现（便于测试替身）。
func RegisterProtocol(p Protocol, factory DialerFactory) {
	if p == "" || factory == nil {
		return
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[p] = factory
}

// RegisteredProtocols 返回当前可用的协议清单（含内置与自定义），已排序。
func RegisteredProtocols() []Protocol {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]Protocol, 0, len(registry))
	for p := range registry {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

func protocolRegistered(p Protocol) bool {
	registryMu.RLock()
	defer registryMu.RUnlock()
	_, ok := registry[p]
	return ok
}

func buildDialer(cfg EndpointConfig) (Dialer, error) {
	registryMu.RLock()
	factory, ok := registry[cfg.Protocol]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("未知协议: %s", cfg.Protocol)
	}
	return factory(cfg)
}

func init() {
	RegisterProtocol(ProtocolDirect, newDirectDialer)
	RegisterProtocol(ProtocolHTTP, newHTTPDialer)
	RegisterProtocol(ProtocolHTTPS, newHTTPDialer)
	RegisterProtocol(ProtocolSOCKS5, newSocksDialer)
	RegisterProtocol(ProtocolSOCKS5H, newSocksDialer)
	RegisterProtocol(ProtocolSSH, newSSHDialer)
	RegisterProtocol(ProtocolTrojan, newTrojanDialer)
	RegisterProtocol(ProtocolShadowsocks, newShadowsocksDialer)
}
