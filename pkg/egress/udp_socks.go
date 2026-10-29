package egress

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/shadowsocks/go-shadowsocks2/socks"
)

// SOCKS5 UDP ASSOCIATE（RFC 1928 §7）。
//
// x/net/proxy 只实现了 CONNECT，这里自己做握手：TCP 控制连接走上一跳（可以套 TLS），
// 服务端回给中继地址后，再经上一跳建一条到中继的 UDP。每个报文前面是
// RSV(2) FRAG(1) ATYP DST.ADDR DST.PORT。控制连接断开即关联结束，UDP 一并关闭。

const (
	socksVersion       = 5
	socksCmdUDP        = 3
	socksAuthNone      = 0
	socksAuthPassword  = 2
	socksAuthNoAccept  = 0xff
	socksUDPHeaderSize = 3
)

func (d *socksDialer) dialUDP(ctx context.Context, base BaseDialFunc, network, address string) (net.Conn, error) {
	host, port, err := splitTarget(address)
	if err != nil {
		return nil, err
	}
	if d.protocol == ProtocolSOCKS5 {
		if _, parseErr := netip.ParseAddr(host); parseErr != nil {
			resolved, resolveErr := d.resolveOne(ctx, host)
			if resolveErr != nil {
				return nil, resolveErr
			}
			host = resolved.String()
		}
	}
	target := socks.ParseAddr(net.JoinHostPort(host, strconv.Itoa(port)))
	if target == nil {
		return nil, fmt.Errorf("SOCKS5 无法编码目标地址: %s", address)
	}

	hop := &hopDialer{base: base, tlsConfig: d.tlsConfig, tlsHost: d.tlsHost}
	ctrl, err := hop.DialContext(ctx, "tcp", d.address)
	if err != nil {
		return nil, fmt.Errorf("连接 SOCKS5 代理 %s 失败: %w", d.address, err)
	}
	restore := applyHandshakeDeadline(ctx, ctrl)
	relay, err := d.associate(ctrl)
	restore()
	if err != nil {
		_ = ctrl.Close()
		return nil, fmt.Errorf("SOCKS5 UDP ASSOCIATE 失败: %w", err)
	}

	udpNetwork := "udp"
	if network == "udp4" || network == "udp6" {
		udpNetwork = network
	}
	packets, err := base(ctx, udpNetwork, relay)
	if err != nil {
		_ = ctrl.Close()
		return nil, fmt.Errorf("连接 SOCKS5 UDP 中继 %s 失败: %w", relay, err)
	}
	conn := &socksUDPConn{
		ctrl:    ctrl,
		packets: packets,
		header:  append([]byte{0, 0, 0}, target...),
		remote:  Addr{Net: "udp", Host: host, Port: port},
	}
	// 服务端在控制连接关闭时结束关联；反过来控制连接断了，UDP 也没有意义了。
	go func() {
		_, _ = io.Copy(io.Discard, ctrl)
		_ = conn.Close()
	}()
	return conn, nil
}

// associate 完成认证并发起 UDP ASSOCIATE，返回服务端给的中继地址。
func (d *socksDialer) associate(ctrl net.Conn) (string, error) {
	methods := []byte{socksAuthNone}
	if d.auth != nil {
		methods = []byte{socksAuthPassword}
	}
	if _, err := ctrl.Write(append([]byte{socksVersion, byte(len(methods))}, methods...)); err != nil {
		return "", err
	}
	var reply [2]byte
	if _, err := io.ReadFull(ctrl, reply[:]); err != nil {
		return "", err
	}
	if reply[0] != socksVersion {
		return "", fmt.Errorf("服务端不是 SOCKS5（版本 %d）", reply[0])
	}
	switch reply[1] {
	case socksAuthNone:
	case socksAuthPassword:
		if d.auth == nil {
			return "", errors.New("服务端要求用户名密码")
		}
		request := []byte{1, byte(len(d.auth.User))}
		request = append(request, d.auth.User...)
		request = append(request, byte(len(d.auth.Password)))
		request = append(request, d.auth.Password...)
		if _, err := ctrl.Write(request); err != nil {
			return "", err
		}
		var status [2]byte
		if _, err := io.ReadFull(ctrl, status[:]); err != nil {
			return "", err
		}
		if status[1] != 0 {
			return "", errors.New("用户名或密码错误")
		}
	case socksAuthNoAccept:
		return "", errors.New("服务端不接受任何认证方式")
	default:
		return "", fmt.Errorf("服务端选择了不支持的认证方式 %d", reply[1])
	}

	// 客户端地址填 0.0.0.0:0：经代理链时我们并不知道自己在中继看来的出口地址。
	if _, err := ctrl.Write([]byte{socksVersion, socksCmdUDP, 0, socks.AtypIPv4, 0, 0, 0, 0, 0, 0}); err != nil {
		return "", err
	}
	var head [3]byte
	if _, err := io.ReadFull(ctrl, head[:]); err != nil {
		return "", err
	}
	if head[1] != 0 {
		return "", fmt.Errorf("服务端拒绝（代码 %d%s）", head[1], socksReplyHint(head[1]))
	}
	bound, err := socks.ReadAddr(ctrl)
	if err != nil {
		return "", fmt.Errorf("读取中继地址失败: %w", err)
	}
	relayHost, relayPort, err := net.SplitHostPort(bound.String())
	if err != nil {
		return "", err
	}
	// 中继地址是 0.0.0.0 / :: 时表示「与控制连接同一台主机」。
	if ip, parseErr := netip.ParseAddr(relayHost); parseErr == nil && ip.IsUnspecified() {
		if proxyHost, _, splitErr := net.SplitHostPort(d.address); splitErr == nil {
			relayHost = proxyHost
		}
	}
	return net.JoinHostPort(relayHost, relayPort), nil
}

func socksReplyHint(code byte) string {
	switch code {
	case 2:
		return "：规则不允许"
	case 7:
		return "：服务端没有开启 UDP"
	default:
		return ""
	}
}

type socksUDPConn struct {
	ctrl    net.Conn
	packets net.Conn
	header  []byte
	remote  Addr

	writeMu sync.Mutex
	buf     []byte
	once    sync.Once
}

func (c *socksUDPConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.buf = append(append(c.buf[:0], c.header...), p...)
	if _, err := c.packets.Write(c.buf); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *socksUDPConn) Read(p []byte) (int, error) {
	buf := make([]byte, 65535)
	for {
		n, err := c.packets.Read(buf)
		if err != nil {
			return 0, err
		}
		if n <= socksUDPHeaderSize || buf[2] != 0 {
			continue // 太短或是分片报文（几乎没有服务端会发），丢弃
		}
		addr := socks.SplitAddr(buf[socksUDPHeaderSize:n])
		if addr == nil {
			continue
		}
		return copy(p, buf[socksUDPHeaderSize+len(addr):n]), nil
	}
}

func (c *socksUDPConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, err := c.Read(p)
	return n, c.remote, err
}

func (c *socksUDPConn) WriteTo(p []byte, _ net.Addr) (int, error) { return c.Write(p) }

func (c *socksUDPConn) Close() error {
	var err error
	c.once.Do(func() {
		err = c.packets.Close()
		_ = c.ctrl.Close()
	})
	return err
}

func (c *socksUDPConn) LocalAddr() net.Addr                { return c.packets.LocalAddr() }
func (c *socksUDPConn) RemoteAddr() net.Addr               { return c.remote }
func (c *socksUDPConn) SetDeadline(t time.Time) error      { return c.packets.SetDeadline(t) }
func (c *socksUDPConn) SetReadDeadline(t time.Time) error  { return c.packets.SetReadDeadline(t) }
func (c *socksUDPConn) SetWriteDeadline(t time.Time) error { return c.packets.SetWriteDeadline(t) }

// putUint16 写大端 16 位（Trojan 报文长度也用它）。
func putUint16(b []byte, v int) { binary.BigEndian.PutUint16(b, uint16(v)) }
