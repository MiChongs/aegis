package egress

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// splitTarget 拆出目标主机与端口。地址一律要求 host:port——
// 出海链路上任何一层缺省端口的猜测都会变成难查的线上问题。
func splitTarget(address string) (host string, port int, err error) {
	h, p, err := net.SplitHostPort(address)
	if err != nil {
		return "", 0, fmt.Errorf("目标地址 %q 无效: %w", address, err)
	}
	h = strings.TrimSpace(h)
	if h == "" {
		return "", 0, fmt.Errorf("目标地址 %q 缺少主机", address)
	}
	n, err := strconv.Atoi(p)
	if err != nil || n <= 0 || n > 65535 {
		return "", 0, fmt.Errorf("目标地址 %q 端口非法", address)
	}
	return h, n, nil
}

// buildTLSConfig 由端点 TLS 配置构造 *tls.Config；未启用返回 nil。
func buildTLSConfig(cfg TLSConfig) (*tls.Config, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	out := &tls.Config{
		ServerName:         cfg.ServerName,
		InsecureSkipVerify: cfg.InsecureSkipVerify, //nolint:gosec // 由管理端显式开启，用于自签名跳板
		MinVersion:         tls.VersionTLS12,
	}
	if cfg.MinVersion == "1.3" {
		out.MinVersion = tls.VersionTLS13
	}
	if len(cfg.ALPN) > 0 {
		out.NextProtos = append([]string(nil), cfg.ALPN...)
	}
	if pem := strings.TrimSpace(cfg.CAPEM); pem != "" {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM([]byte(pem)) {
			return nil, fmt.Errorf("tls.caPem 解析失败")
		}
		out.RootCAs = pool
	}
	if strings.TrimSpace(cfg.ClientCertPEM) != "" {
		pair, err := tls.X509KeyPair([]byte(cfg.ClientCertPEM), []byte(cfg.ClientKeyPEM))
		if err != nil {
			return nil, fmt.Errorf("客户端证书解析失败: %w", err)
		}
		out.Certificates = []tls.Certificate{pair}
	}
	return out, nil
}

// wrapTLS 在已建立的连接上完成 TLS 握手，握手失败时负责关掉底层连接。
func wrapTLS(ctx context.Context, conn net.Conn, cfg *tls.Config, fallbackServerName string) (net.Conn, error) {
	clone := cfg.Clone()
	if clone.ServerName == "" {
		clone.ServerName = fallbackServerName
	}
	tlsConn := tls.Client(conn, clone)
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("TLS 握手失败: %w", err)
	}
	return tlsConn, nil
}

// isTCPNetwork 目前所有协议隧道都只承载 TCP。UDP 出海需要另一套语义
// （SOCKS5 UDP ASSOCIATE / SS UDP relay），当前明确拒绝而不是静默降级。
func isTCPNetwork(network string) bool {
	switch network {
	case "tcp", "tcp4", "tcp6":
		return true
	default:
		return false
	}
}

func requireTCP(network string, protocol Protocol) error {
	if !isTCPNetwork(network) {
		return fmt.Errorf("%s 端点不支持 %s 网络（仅支持 tcp）", protocol, network)
	}
	return nil
}
