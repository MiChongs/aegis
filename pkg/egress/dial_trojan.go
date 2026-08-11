package egress

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"

	"github.com/shadowsocks/go-shadowsocks2/socks"
)

// trojanCmdConnect Trojan 请求头里的 CONNECT 命令字（与 SOCKS5 同值）。
const trojanCmdConnect = 0x01

// trojanDialer 实现 Trojan 协议。
//
// Trojan 没有独立的 Go 客户端库（现成实现都在 sing-box / xray 里，二者分别是
// GPL-3.0 与 MPL-2.0，不适合直接编进本平台），但它本身也不需要：
// TLS 由 crypto/tls 承担，口令摘要由 crypto/sha256 承担，目标地址复用
// go-shadowsocks2 的 socks.ParseAddr —— 真正属于 Trojan 的只有
// 「56 字节口令十六进制 + CRLF + 命令字 + 地址 + CRLF」这一行拼接。
type trojanDialer struct {
	address   string
	tlsConfig *tls.Config
	tlsHost   string
	authHex   []byte // hex(SHA224(password))，56 字节
}

func newTrojanDialer(cfg EndpointConfig) (Dialer, error) {
	tlsCfg, err := buildTLSConfig(cfg.TLS)
	if err != nil {
		return nil, err
	}
	if tlsCfg == nil {
		tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	if cfg.Password == "" {
		return nil, fmt.Errorf("trojan 端点缺少 password")
	}
	sum := sha256.Sum224([]byte(cfg.Password))
	encoded := make([]byte, hex.EncodedLen(len(sum)))
	hex.Encode(encoded, sum[:])

	d := &trojanDialer{address: cfg.Address, tlsConfig: tlsCfg, authHex: encoded}
	if host, _, splitErr := net.SplitHostPort(cfg.Address); splitErr == nil {
		d.tlsHost = host
	}
	return d, nil
}

func (d *trojanDialer) DialContext(ctx context.Context, base BaseDialFunc, network, address string) (net.Conn, error) {
	if err := requireTCP(network, ProtocolTrojan); err != nil {
		return nil, err
	}
	if _, _, err := splitTarget(address); err != nil {
		return nil, err
	}
	target := socks.ParseAddr(address)
	if target == nil {
		return nil, fmt.Errorf("trojan 无法编码目标地址: %s", address)
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
	header = append(header, '\r', '\n')
	header = append(header, trojanCmdConnect)
	header = append(header, target...)
	header = append(header, '\r', '\n')
	if _, err := tlsConn.Write(header); err != nil {
		_ = tlsConn.Close()
		return nil, fmt.Errorf("发送 Trojan 请求头失败: %w", err)
	}
	restore()
	// Trojan 没有服务端应答：口令错误时连接会被静默转发到伪装站点，
	// 表现为后续 TLS/HTTP 层解析失败，而不是这里报错。
	return tlsConn, nil
}
