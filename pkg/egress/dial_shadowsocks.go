package egress

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/shadowsocks/go-shadowsocks2/core"
	"github.com/shadowsocks/go-shadowsocks2/socks"
)

// shadowsocksDialer 基于 Shadowsocks 官方参考实现 go-shadowsocks2。
//
// 密钥派生（EVP_BytesToKey）、HKDF 子密钥、AEAD 分块与 nonce 递增全部由
// core.Cipher 负责；这里只做「连过去 → 套加密流 → 写目标地址」三步，
// 与上游 tcp.go 里客户端的写法保持一致。
type shadowsocksDialer struct {
	address string
	cipher  core.Cipher
}

func newShadowsocksDialer(cfg EndpointConfig) (Dialer, error) {
	password := cfg.Shadowsocks.Password
	if password == "" {
		password = cfg.Password
	}
	if password == "" {
		return nil, fmt.Errorf("shadowsocks 端点缺少 password")
	}
	if !supportedShadowsocksMethod(cfg.Shadowsocks.Method) {
		return nil, fmt.Errorf("不支持的 shadowsocks 加密方式: %s", cfg.Shadowsocks.Method)
	}
	cipher, err := core.PickCipher(cfg.Shadowsocks.Method, nil, password)
	if err != nil {
		return nil, fmt.Errorf("初始化 shadowsocks 加密失败: %w", err)
	}
	return &shadowsocksDialer{address: cfg.Address, cipher: cipher}, nil
}

func (d *shadowsocksDialer) DialContext(ctx context.Context, base BaseDialFunc, network, address string) (net.Conn, error) {
	if err := requireTCP(network, ProtocolShadowsocks); err != nil {
		return nil, err
	}
	if _, _, err := splitTarget(address); err != nil {
		return nil, err
	}
	target := socks.ParseAddr(address)
	if target == nil {
		return nil, fmt.Errorf("shadowsocks 无法编码目标地址: %s", address)
	}
	conn, err := base(ctx, "tcp", d.address)
	if err != nil {
		return nil, fmt.Errorf("连接 Shadowsocks 服务器 %s 失败: %w", d.address, err)
	}
	stream := d.cipher.StreamConn(conn)
	restore := applyHandshakeDeadline(ctx, stream)
	if _, err := stream.Write(target); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("发送 Shadowsocks 目标地址失败: %w", err)
	}
	restore()
	return stream, nil
}

// SupportedShadowsocksMethods 返回可用的加密方式（管理端下拉框用）。
//
// core.ListCipher() 返回的是 AEAD_AES_128_GCM 这类内部名，而配置里通行的写法是
// aes-128-gcm，PickCipher 两种都认。这里只暴露通行写法，避免管理端出现两套名字。
func SupportedShadowsocksMethods() []string {
	return []string{"aes-128-gcm", "aes-256-gcm", "chacha20-ietf-poly1305"}
}

func supportedShadowsocksMethod(method string) bool {
	method = strings.TrimSpace(method)
	if method == "" {
		return false
	}
	// dummy 是明文直通，出现在出海配置里一定是误配，显式拒绝。
	if strings.EqualFold(method, "dummy") {
		return false
	}
	// 用参考实现自己判定，避免在这里维护第二份加密方式清单。
	_, err := core.PickCipher(method, nil, "probe")
	return err == nil
}
