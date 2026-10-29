package egress

import (
	"context"
	"errors"
	"net"
	"os"
	"sync"
	"time"
)

// ListenPacket 返回一个按目标路由的 UDP 套接字：WriteTo 到哪个地址，就按那个地址查路由表、
// 经对应线路建一条 UDP 会话；各会话的回包汇总到 ReadFrom，来源地址就是当初 WriteTo 的那个地址。
//
// 用于一个套接字要发往多个目标的场景（QUIC 客户端、STUN、批量 DNS）。目标是域名时用 Addr
// 而不是 *net.UDPAddr，域名才会原样交给代理解析。会话空闲 3 分钟回收。
func (g *Gateway) ListenPacket(ctx context.Context, profile Profile) (net.PacketConn, error) {
	pc := &routedPacketConn{
		gw:       g,
		profile:  profile,
		ctx:      context.WithoutCancel(ctx),
		sessions: map[string]*packetSession{},
		inbox:    make(chan inboundPacket, 256),
		closed:   make(chan struct{}),
		idle:     3 * time.Minute,
	}
	go pc.janitor()
	return pc, nil
}

// DialProfile 以指定调用方身份按路由表建立连接（tcp 或 udp），供需要 Profile 维度路由的裸连接使用。
func (g *Gateway) DialProfile(ctx context.Context, profile Profile, network, address string) (net.Conn, error) {
	return g.dialWithProfile(ctx, network, address, profile)
}

// Resolver 返回一个经网关出站的 DNS 解析器：查询报文按路由表走（UDP 优先，截断时按 TCP 重试）。
//
// servers 指定 DNS 服务器（host:port，多个时轮流用）；留空则用系统 resolv.conf 里的服务器。
// 境内部署要解析境外域名、避开本地 DNS 污染时，填一个境外服务器并让路由表把它送进代理。
func (g *Gateway) Resolver(profile Profile, servers ...string) *net.Resolver {
	return newResolver(func(ctx context.Context, network, address string) (net.Conn, error) {
		return g.dialWithProfile(ctx, network, address, profile)
	}, servers)
}

func newResolver(dial func(ctx context.Context, network, address string) (net.Conn, error), servers []string) *net.Resolver {
	var next atomicCounter
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			if len(servers) > 0 {
				address = servers[int(next.add()%uint64(len(servers)))]
			}
			return dial(ctx, network, address)
		},
	}
}

type atomicCounter struct {
	mu sync.Mutex
	n  uint64
}

func (c *atomicCounter) add() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	return c.n - 1
}

type inboundPacket struct {
	data []byte
	from net.Addr
}

type packetSession struct {
	conn     net.Conn
	addr     net.Addr
	lastUsed time.Time
}

type routedPacketConn struct {
	gw      *Gateway
	profile Profile
	ctx     context.Context

	mu       sync.Mutex
	sessions map[string]*packetSession
	inbox    chan inboundPacket
	closed   chan struct{}
	once     sync.Once
	idle     time.Duration

	deadlineMu    sync.Mutex
	readDeadline  time.Time
	writeDeadline time.Time
}

var errPacketConnClosed = net.ErrClosed

func (c *routedPacketConn) session(addr net.Addr) (*packetSession, error) {
	key := addr.String()
	c.mu.Lock()
	if s, ok := c.sessions[key]; ok {
		s.lastUsed = time.Now()
		c.mu.Unlock()
		return s, nil
	}
	c.mu.Unlock()

	select {
	case <-c.closed:
		return nil, errPacketConnClosed
	default:
	}
	dialCtx, cancel := context.WithTimeout(c.ctx, DefaultDialTimeout)
	defer cancel()
	conn, err := c.gw.dialWithProfile(dialCtx, "udp", key, c.profile)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	if existing, ok := c.sessions[key]; ok {
		// 并发写同一个新目标时只留一条会话。
		c.mu.Unlock()
		_ = conn.Close()
		return existing, nil
	}
	s := &packetSession{conn: conn, addr: addr, lastUsed: time.Now()}
	c.sessions[key] = s
	c.mu.Unlock()
	go c.pump(key, s)
	return s, nil
}

// pump 把一条会话的回包送进 inbox。
func (c *routedPacketConn) pump(key string, s *packetSession) {
	buf := make([]byte, 65535)
	for {
		n, err := s.conn.Read(buf)
		if err != nil {
			c.mu.Lock()
			if c.sessions[key] == s {
				delete(c.sessions, key)
			}
			c.mu.Unlock()
			_ = s.conn.Close()
			return
		}
		c.mu.Lock()
		s.lastUsed = time.Now()
		c.mu.Unlock()
		packet := inboundPacket{data: append([]byte(nil), buf[:n]...), from: s.addr}
		select {
		case c.inbox <- packet:
		case <-c.closed:
			return
		default:
			// 读得比来得慢：丢掉最新的报文，与内核 UDP 缓冲区满时的行为一致。
		}
	}
}

func (c *routedPacketConn) janitor() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.closed:
			return
		case now := <-ticker.C:
			c.mu.Lock()
			for key, s := range c.sessions {
				if now.Sub(s.lastUsed) > c.idle {
					delete(c.sessions, key)
					_ = s.conn.Close()
				}
			}
			c.mu.Unlock()
		}
	}
}

func (c *routedPacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	if addr == nil {
		return 0, errors.New("egress: WriteTo 需要目标地址")
	}
	s, err := c.session(addr)
	if err != nil {
		return 0, err
	}
	c.deadlineMu.Lock()
	deadline := c.writeDeadline
	c.deadlineMu.Unlock()
	_ = s.conn.SetWriteDeadline(deadline)
	return s.conn.Write(p)
}

func (c *routedPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	c.deadlineMu.Lock()
	deadline := c.readDeadline
	c.deadlineMu.Unlock()
	var timeout <-chan time.Time
	if !deadline.IsZero() {
		wait := time.Until(deadline)
		if wait <= 0 {
			return 0, nil, os.ErrDeadlineExceeded
		}
		timer := time.NewTimer(wait)
		defer timer.Stop()
		timeout = timer.C
	}
	select {
	case packet := <-c.inbox:
		return copy(p, packet.data), packet.from, nil
	case <-c.closed:
		return 0, nil, errPacketConnClosed
	case <-timeout:
		return 0, nil, os.ErrDeadlineExceeded
	}
}

func (c *routedPacketConn) Close() error {
	c.once.Do(func() {
		close(c.closed)
		c.mu.Lock()
		for key, s := range c.sessions {
			delete(c.sessions, key)
			_ = s.conn.Close()
		}
		c.mu.Unlock()
	})
	return nil
}

func (c *routedPacketConn) LocalAddr() net.Addr { return &net.UDPAddr{IP: net.IPv4zero} }

func (c *routedPacketConn) SetDeadline(t time.Time) error {
	_ = c.SetReadDeadline(t)
	return c.SetWriteDeadline(t)
}

func (c *routedPacketConn) SetReadDeadline(t time.Time) error {
	c.deadlineMu.Lock()
	c.readDeadline = t
	c.deadlineMu.Unlock()
	return nil
}

func (c *routedPacketConn) SetWriteDeadline(t time.Time) error {
	c.deadlineMu.Lock()
	c.writeDeadline = t
	c.deadlineMu.Unlock()
	return nil
}

// SetReadBuffer / SetWriteBuffer 让 quic-go 这类会调大套接字缓冲区的调用方不报警告：
// 真正的套接字在各条线路的会话里，回包在 inbox 里排队，这里没有可调的缓冲区。
func (c *routedPacketConn) SetReadBuffer(int) error  { return nil }
func (c *routedPacketConn) SetWriteBuffer(int) error { return nil }
