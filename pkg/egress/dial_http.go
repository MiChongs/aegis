package egress

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// httpDialer 通过 HTTP CONNECT 建隧道。protocol=https 时先与代理完成 TLS。
type httpDialer struct {
	protocol  Protocol
	address   string
	authValue string
	headers   map[string]string
	tlsConfig *tls.Config
	tlsHost   string
}

func newHTTPDialer(cfg EndpointConfig) (Dialer, error) {
	tlsCfg, err := buildTLSConfig(cfg.TLS)
	if err != nil {
		return nil, err
	}
	d := &httpDialer{
		protocol: cfg.Protocol,
		address:  cfg.Address,
		headers:  cfg.Headers,
	}
	if cfg.Username != "" || cfg.Password != "" {
		raw := cfg.Username + ":" + cfg.Password
		d.authValue = "Basic " + base64.StdEncoding.EncodeToString([]byte(raw))
	}
	if cfg.Protocol == ProtocolHTTPS || cfg.TLS.Enabled {
		if tlsCfg == nil {
			tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12}
		}
		d.tlsConfig = tlsCfg
		host, _, splitErr := net.SplitHostPort(cfg.Address)
		if splitErr == nil {
			d.tlsHost = host
		}
	}
	return d, nil
}

func (d *httpDialer) DialContext(ctx context.Context, base BaseDialFunc, network, address string) (net.Conn, error) {
	if err := requireTCP(network, d.protocol); err != nil {
		return nil, err
	}
	if _, _, err := splitTarget(address); err != nil {
		return nil, err
	}
	conn, err := base(ctx, "tcp", d.address)
	if err != nil {
		return nil, fmt.Errorf("连接 HTTP 代理 %s 失败: %w", d.address, err)
	}
	if d.tlsConfig != nil {
		conn, err = wrapTLS(ctx, conn, d.tlsConfig, d.tlsHost)
		if err != nil {
			return nil, err
		}
	}
	restore := applyHandshakeDeadline(ctx, conn)

	req := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: address},
		Host:   address,
		Header: http.Header{},
	}
	// 隧道建立阶段不谈论持久连接语义，显式声明避免某些代理返回 Connection: close 后关连接。
	req.Header.Set("Proxy-Connection", "Keep-Alive")
	req.Header.Set("User-Agent", "")
	if d.authValue != "" {
		req.Header.Set("Proxy-Authorization", d.authValue)
	}
	for k, v := range d.headers {
		req.Header.Set(k, v)
	}
	if err := req.Write(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("发送 CONNECT 失败: %w", err)
	}

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, req)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("读取 CONNECT 响应失败: %w", err)
	}
	// CONNECT 的响应体对隧道没有意义，但必须消费掉 http 包持有的引用。
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_ = conn.Close()
		return nil, fmt.Errorf("HTTP 代理拒绝 CONNECT %s: %s", address, strings.TrimSpace(resp.Status))
	}
	restore()
	// 少数代理会把响应和后续数据一起塞进同一个 TCP 段，bufio 里可能已经预读了
	// 属于隧道的数据；丢掉它会表现为「TLS 握手随机失败」，因此必须接回去。
	if reader.Buffered() > 0 {
		peeked, _ := reader.Peek(reader.Buffered())
		return &bufferedConn{Conn: conn, pending: append([]byte(nil), peeked...)}, nil
	}
	return conn, nil
}

// bufferedConn 把握手阶段被 bufio 预读走的字节还给后续 Read。
type bufferedConn struct {
	net.Conn
	pending []byte
}

func (c *bufferedConn) Read(p []byte) (int, error) {
	if len(c.pending) > 0 {
		n := copy(p, c.pending)
		c.pending = c.pending[n:]
		return n, nil
	}
	return c.Conn.Read(p)
}

// applyHandshakeDeadline 把 ctx 的截止时间压到连接上，返回恢复函数。
// 协议握手阶段没有 ctx 感知的读写 API，只能靠 deadline 兜底，
// 否则一个不回包的代理会把调用方 goroutine 永久挂住。
func applyHandshakeDeadline(ctx context.Context, conn net.Conn) func() {
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(DefaultDialTimeout)
	}
	_ = conn.SetDeadline(deadline)
	return func() { _ = conn.SetDeadline(time.Time{}) }
}
