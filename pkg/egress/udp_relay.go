package egress

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/shadowsocks/go-shadowsocks2/core"
	"github.com/shadowsocks/go-shadowsocks2/socks"
)

// Shadowsocks 与 Trojan 的 UDP。

// ── Shadowsocks UDP relay ──
//
// 每个报文是 [目标地址][载荷] 整体 AEAD 加密后发往服务端；回程报文是 [来源地址][载荷]。
// 加解密交给 go-shadowsocks2 的 PacketConn，这里只拼地址、剥地址。

func (d *shadowsocksDialer) dialUDP(ctx context.Context, base BaseDialFunc, network, address string) (net.Conn, error) {
	cipher, ok := d.cipher.(core.PacketConnCipher)
	if !ok {
		return nil, fmt.Errorf("shadowsocks 加密方式不支持 UDP")
	}
	target := socks.ParseAddr(address)
	if target == nil {
		return nil, fmt.Errorf("shadowsocks 无法编码目标地址: %s", address)
	}
	remote, err := ParseAddr("udp", address)
	if err != nil {
		return nil, err
	}
	udpNetwork := "udp"
	if network == "udp4" || network == "udp6" {
		udpNetwork = network
	}
	raw, err := base(ctx, udpNetwork, d.address)
	if err != nil {
		return nil, fmt.Errorf("连接 Shadowsocks 服务器 %s 失败: %w", d.address, err)
	}
	// 上一跳给的是已连接的数据报连接：WriteTo 一律发往服务端。
	pc := &packetAdapter{Conn: raw}
	return &relayUDPConn{
		raw:    raw,
		pc:     cipher.PacketConn(pc),
		server: raw.RemoteAddr(),
		header: target,
		remote: remote,
	}, nil
}

type relayUDPConn struct {
	raw    net.Conn
	pc     net.PacketConn
	server net.Addr
	header []byte
	remote Addr

	writeMu sync.Mutex
	buf     []byte
}

func (c *relayUDPConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.buf = append(append(c.buf[:0], c.header...), p...)
	if _, err := c.pc.WriteTo(c.buf, c.server); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *relayUDPConn) Read(p []byte) (int, error) {
	buf := make([]byte, 65535)
	for {
		n, _, err := c.pc.ReadFrom(buf)
		if err != nil {
			return 0, err
		}
		addr := socks.SplitAddr(buf[:n])
		if addr == nil {
			continue
		}
		return copy(p, buf[len(addr):n]), nil
	}
}

func (c *relayUDPConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, err := c.Read(p)
	return n, c.remote, err
}

func (c *relayUDPConn) WriteTo(p []byte, _ net.Addr) (int, error) { return c.Write(p) }
func (c *relayUDPConn) Close() error                              { return c.raw.Close() }
func (c *relayUDPConn) LocalAddr() net.Addr                       { return c.raw.LocalAddr() }
func (c *relayUDPConn) RemoteAddr() net.Addr                      { return c.remote }
func (c *relayUDPConn) SetDeadline(t time.Time) error             { return c.raw.SetDeadline(t) }
func (c *relayUDPConn) SetReadDeadline(t time.Time) error         { return c.raw.SetReadDeadline(t) }
func (c *relayUDPConn) SetWriteDeadline(t time.Time) error        { return c.raw.SetWriteDeadline(t) }

// ── Trojan UDP ASSOCIATE ──
//
// 请求头与 TCP 相同，命令字换成 0x03；之后 TLS 流里每个报文是
// ATYP DST.ADDR DST.PORT | 长度(2) | CRLF | 载荷。整条关联只走 TCP，所以可以接在任意代理链后面。

const trojanCmdUDP = 0x03

func (d *trojanDialer) dialUDP(ctx context.Context, base BaseDialFunc, address string) (net.Conn, error) {
	target := socks.ParseAddr(address)
	if target == nil {
		return nil, fmt.Errorf("trojan 无法编码目标地址: %s", address)
	}
	remote, err := ParseAddr("udp", address)
	if err != nil {
		return nil, err
	}
	conn, err := base(ctx, "tcp", d.address)
	if err != nil {
		return nil, fmt.Errorf("连接 Trojan 服务器 %s 失败: %w", d.address, err)
	}
	tlsConn, err := wrapTLS(ctx, conn, d.tlsConfig, d.tlsHost)
	if err != nil {
		return nil, err
	}
	restore := applyHandshakeDeadline(ctx, tlsConn)
	header := make([]byte, 0, len(d.authHex)+2+1+len(target)+2)
	header = append(header, d.authHex...)
	header = append(header, '\r', '\n', trojanCmdUDP)
	header = append(header, target...)
	header = append(header, '\r', '\n')
	if _, err := tlsConn.Write(header); err != nil {
		_ = tlsConn.Close()
		return nil, fmt.Errorf("发送 Trojan 请求头失败: %w", err)
	}
	restore()
	return &trojanUDPConn{stream: tlsConn, reader: bufio.NewReaderSize(tlsConn, 64<<10), target: target, remote: remote}, nil
}

type trojanUDPConn struct {
	stream net.Conn
	reader *bufio.Reader
	target []byte
	remote Addr

	writeMu sync.Mutex
	buf     []byte
}

func (c *trojanUDPConn) Write(p []byte) (int, error) {
	if len(p) > 0xffff {
		return 0, fmt.Errorf("UDP 报文超过 65535 字节")
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.buf = append(c.buf[:0], c.target...)
	c.buf = append(c.buf, 0, 0, '\r', '\n')
	putUint16(c.buf[len(c.target):], len(p))
	c.buf = append(c.buf, p...)
	if _, err := c.stream.Write(c.buf); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *trojanUDPConn) Read(p []byte) (int, error) {
	if _, err := socks.ReadAddr(c.reader); err != nil {
		return 0, err
	}
	var meta [4]byte
	if _, err := io.ReadFull(c.reader, meta[:]); err != nil {
		return 0, err
	}
	length := int(meta[0])<<8 | int(meta[1])
	payload := make([]byte, length)
	if _, err := io.ReadFull(c.reader, payload); err != nil {
		return 0, err
	}
	return copy(p, payload), nil
}

func (c *trojanUDPConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, err := c.Read(p)
	return n, c.remote, err
}

func (c *trojanUDPConn) WriteTo(p []byte, _ net.Addr) (int, error) { return c.Write(p) }
func (c *trojanUDPConn) Close() error                              { return c.stream.Close() }
func (c *trojanUDPConn) LocalAddr() net.Addr                       { return c.stream.LocalAddr() }
func (c *trojanUDPConn) RemoteAddr() net.Addr                      { return c.remote }
func (c *trojanUDPConn) SetDeadline(t time.Time) error             { return c.stream.SetDeadline(t) }
func (c *trojanUDPConn) SetReadDeadline(t time.Time) error         { return c.stream.SetReadDeadline(t) }
func (c *trojanUDPConn) SetWriteDeadline(t time.Time) error        { return c.stream.SetWriteDeadline(t) }
