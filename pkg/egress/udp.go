package egress

import (
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
)

// UDP 出海。
//
// 网关对 UDP 的约定是「已连接的数据报连接」：DialContext(ctx, "udp", "host:port") 返回的 net.Conn
// 每次 Write 发出一个数据报、每次 Read 读回一个数据报，对端固定为 address。
// 返回的连接同时实现 net.PacketConn（WriteTo 忽略地址、ReadFrom 回报对端），
// 这样 net.Resolver 会按 UDP 报文而不是 TCP 长度前缀与它对话。
//
// 需要一个套接字发往多个目标（QUIC、STUN）时用 ListenPacket：按每个目标各自路由。
//
// 各协议怎么承载 UDP：
//
//	direct       系统 UDP 套接字
//	socks5(h)    UDP ASSOCIATE（RFC 1928 §7）：TCP 控制连接 + 到中继地址的 UDP
//	shadowsocks  UDP relay：每个报文单独 AEAD 加密
//	trojan       UDP ASSOCIATE：报文按「地址 + 长度 + CRLF + 载荷」封装在 TLS 流里
//	http/https/ssh  不支持（CONNECT 与 direct-tcpip 都只有 TCP）

// UDPTransport 一种协议的 UDP 怎么到服务端，决定它能不能接在代理链后面。
type UDPTransport string

const (
	// UDPNone 不支持 UDP。
	UDPNone UDPTransport = ""
	// UDPOverUDP 报文以 UDP 发给服务端：上一跳（via）也必须能承载 UDP。
	UDPOverUDP UDPTransport = "udp"
	// UDPOverStream 报文封装在 TCP / TLS 流里：上一跳只要能走 TCP。
	UDPOverStream UDPTransport = "stream"
)

var (
	udpRegistryMu sync.RWMutex
	udpRegistry   = map[Protocol]UDPTransport{}
)

// RegisterProtocolUDP 声明某个协议能承载 UDP（协议实现的 DialContext 需处理 "udp" 网络）。
func RegisterProtocolUDP(p Protocol, transport UDPTransport) {
	udpRegistryMu.Lock()
	defer udpRegistryMu.Unlock()
	if transport == UDPNone {
		delete(udpRegistry, p)
		return
	}
	udpRegistry[p] = transport
}

// ProtocolUDP 协议的 UDP 承载方式；UDPNone 表示不支持。
func ProtocolUDP(p Protocol) UDPTransport {
	udpRegistryMu.RLock()
	defer udpRegistryMu.RUnlock()
	return udpRegistry[p]
}

// UDPProtocols 支持 UDP 的协议及承载方式（管理端展示用）。
func UDPProtocols() map[Protocol]UDPTransport {
	udpRegistryMu.RLock()
	defer udpRegistryMu.RUnlock()
	out := make(map[Protocol]UDPTransport, len(udpRegistry))
	for p, t := range udpRegistry {
		out[p] = t
	}
	return out
}

func init() {
	RegisterProtocolUDP(ProtocolDirect, UDPOverUDP)
	RegisterProtocolUDP(ProtocolSOCKS5, UDPOverUDP)
	RegisterProtocolUDP(ProtocolSOCKS5H, UDPOverUDP)
	RegisterProtocolUDP(ProtocolShadowsocks, UDPOverUDP)
	RegisterProtocolUDP(ProtocolTrojan, UDPOverStream)
}

func isUDPNetwork(network string) bool {
	switch network {
	case "udp", "udp4", "udp6":
		return true
	default:
		return false
	}
}

// networkKind 把 tcp4 / udp6 这类写法归成 tcp / udp，用于路由与统计。
func networkKind(network string) string {
	if isUDPNetwork(network) {
		return "udp"
	}
	return "tcp"
}

// Addr 带域名的目标地址。net.UDPAddr 只能装 IP，而出海场景要把域名原样交给代理解析
// （避开本地 DNS 污染），向 ListenPacket 返回的连接 WriteTo 时用它。
type Addr struct {
	Net  string // udp / tcp，留空为 udp
	Host string
	Port int
}

// Network 实现 net.Addr。
func (a Addr) Network() string {
	if a.Net == "" {
		return "udp"
	}
	return a.Net
}

// String 实现 net.Addr，形如 example.com:443。
func (a Addr) String() string { return net.JoinHostPort(a.Host, strconv.Itoa(a.Port)) }

// ParseAddr 把 host:port 解析成 Addr。
func ParseAddr(network, address string) (Addr, error) {
	host, port, err := splitTarget(address)
	if err != nil {
		return Addr{}, err
	}
	return Addr{Net: network, Host: host, Port: port}, nil
}

// datagramConn 协议实现返回的「已连接数据报连接」需要满足的最小形态。
// packetAdapter 在它之上补出 net.PacketConn 的两个方法。
type packetAdapter struct {
	net.Conn
}

// asDatagram 让一条已连接的数据报连接同时满足 net.PacketConn，且 WriteTo 忽略地址。
// 系统的 *net.UDPConn 虽然实现了 PacketConn，但已连接时调用 WriteTo 会报错，所以同样包一层。
func asDatagram(conn net.Conn) net.Conn {
	switch conn.(type) {
	case *packetAdapter, *countingPacketConn, *socksUDPConn, *relayUDPConn, *trojanUDPConn:
		return conn
	}
	return &packetAdapter{Conn: conn}
}

func (p *packetAdapter) ReadFrom(b []byte) (int, net.Addr, error) {
	n, err := p.Conn.Read(b)
	return n, p.Conn.RemoteAddr(), err
}

func (p *packetAdapter) WriteTo(b []byte, _ net.Addr) (int, error) { return p.Conn.Write(b) }

// countingPacketConn 与 countingConn 相同，额外保留 net.PacketConn，并按报文计数。
type countingPacketConn struct {
	net.Conn
	pc        net.PacketConn
	in, out   *atomic.Uint64
	pin, pout *atomic.Uint64
}

func (c *countingPacketConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.in.Add(uint64(n))
		c.pin.Add(1)
	}
	return n, err
}

func (c *countingPacketConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n > 0 {
		c.out.Add(uint64(n))
		c.pout.Add(1)
	}
	return n, err
}

func (c *countingPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, addr, err := c.pc.ReadFrom(p)
	if n > 0 {
		c.in.Add(uint64(n))
		c.pin.Add(1)
	}
	return n, addr, err
}

func (c *countingPacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	n, err := c.pc.WriteTo(p, addr)
	if n > 0 {
		c.out.Add(uint64(n))
		c.pout.Add(1)
	}
	return n, err
}

// errUDPUnsupported 统一的「这个协议不能走 UDP」错误。
func errUDPUnsupported(protocol Protocol) error {
	return fmt.Errorf("%s 端点不支持 UDP", protocol)
}

// requireStream 只承载 TCP 的协议在入口处拒绝其它网络。
func requireStream(network string, protocol Protocol) error {
	if isUDPNetwork(network) {
		return errUDPUnsupported(protocol)
	}
	return requireTCP(network, protocol)
}
