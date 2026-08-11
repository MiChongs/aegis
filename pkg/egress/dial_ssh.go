package egress

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// sshDialer 通过 SSH 的 direct-tcpip 通道出海。
//
// SSH 握手（KEX + 认证）比一次 TCP 连接贵得多，因此这里复用一条长连接：
// 客户端懒建立、并发共享、失败即丢弃重建。上层看到的仍是「一次拨号一条 conn」。
type sshDialer struct {
	address   string
	clientCfg *ssh.ClientConfig
	keepAlive time.Duration

	mu     sync.Mutex
	client *ssh.Client
	closed bool
}

func newSSHDialer(cfg EndpointConfig) (Dialer, error) {
	auths := make([]ssh.AuthMethod, 0, 2)
	if key := strings.TrimSpace(cfg.SSH.PrivateKeyPEM); key != "" {
		var signer ssh.Signer
		var err error
		if cfg.SSH.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(key), []byte(cfg.SSH.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(key))
		}
		if err != nil {
			return nil, fmt.Errorf("解析 SSH 私钥失败: %w", err)
		}
		auths = append(auths, ssh.PublicKeys(signer))
	}
	if cfg.SSH.Password != "" {
		auths = append(auths, ssh.Password(cfg.SSH.Password))
	}
	if len(auths) == 0 {
		return nil, fmt.Errorf("ssh 端点缺少认证凭据")
	}

	hostKeyCallback := ssh.InsecureIgnoreHostKey() //nolint:gosec // 未配置指纹时由管理员自行承担
	if fp := strings.TrimSpace(cfg.SSH.HostKeyFingerprint); fp != "" {
		expected := fp
		hostKeyCallback = func(_ string, _ net.Addr, key ssh.PublicKey) error {
			actual := ssh.FingerprintSHA256(key)
			if actual != expected && strings.TrimPrefix(actual, "SHA256:") != strings.TrimPrefix(expected, "SHA256:") {
				return fmt.Errorf("SSH 主机密钥指纹不匹配: 期望 %s，实际 %s", expected, actual)
			}
			return nil
		}
	}

	keepAlive := time.Duration(cfg.SSH.KeepAliveSeconds) * time.Second
	if keepAlive <= 0 {
		keepAlive = 30 * time.Second
	}
	return &sshDialer{
		address: cfg.Address,
		clientCfg: &ssh.ClientConfig{
			User:            cfg.SSH.User,
			Auth:            auths,
			HostKeyCallback: hostKeyCallback,
			Timeout:         DefaultDialTimeout,
		},
		keepAlive: keepAlive,
	}, nil
}

func (d *sshDialer) DialContext(ctx context.Context, base BaseDialFunc, network, address string) (net.Conn, error) {
	if err := requireTCP(network, ProtocolSSH); err != nil {
		return nil, err
	}
	if _, _, err := splitTarget(address); err != nil {
		return nil, err
	}
	// 第一次失败可能只是缓存的长连接已经断了，重建后再试一次。
	for attempt := 0; attempt < 2; attempt++ {
		client, fresh, err := d.ensureClient(ctx, base)
		if err != nil {
			return nil, err
		}
		conn, err := client.Dial("tcp", address)
		if err == nil {
			return &sshChannelConn{Conn: conn}, nil
		}
		d.discard(client)
		if fresh {
			return nil, fmt.Errorf("SSH 隧道连接 %s 失败: %w", address, err)
		}
	}
	return nil, fmt.Errorf("SSH 隧道连接 %s 失败", address)
}

// ensureClient 返回可用的 SSH 客户端；fresh 表示这次是新建的
// （新建的还失败说明不是「连接过期」而是真的不通，不该再重试）。
func (d *sshDialer) ensureClient(ctx context.Context, base BaseDialFunc) (client *ssh.Client, fresh bool, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return nil, false, fmt.Errorf("ssh 端点已关闭")
	}
	if d.client != nil {
		return d.client, false, nil
	}
	conn, err := base(ctx, "tcp", d.address)
	if err != nil {
		return nil, false, fmt.Errorf("连接 SSH 服务器 %s 失败: %w", d.address, err)
	}
	restore := applyHandshakeDeadline(ctx, conn)
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, d.address, d.clientCfg)
	if err != nil {
		_ = conn.Close()
		return nil, false, fmt.Errorf("SSH 握手失败: %w", err)
	}
	restore()
	d.client = ssh.NewClient(sshConn, chans, reqs)
	go d.keepAliveLoop(d.client)
	return d.client, true, nil
}

func (d *sshDialer) discard(client *ssh.Client) {
	d.mu.Lock()
	if d.client == client {
		d.client = nil
	}
	d.mu.Unlock()
	_ = client.Close()
}

func (d *sshDialer) keepAliveLoop(client *ssh.Client) {
	ticker := time.NewTicker(d.keepAlive)
	defer ticker.Stop()
	for range ticker.C {
		if _, _, err := client.SendRequest("keepalive@openssh.com", true, nil); err != nil {
			d.discard(client)
			return
		}
		d.mu.Lock()
		stale := d.client != client
		d.mu.Unlock()
		if stale {
			return
		}
	}
}

// Close 释放长连接，由网关在重载/关闭时调用。
func (d *sshDialer) Close() error {
	d.mu.Lock()
	client := d.client
	d.client = nil
	d.closed = true
	d.mu.Unlock()
	if client != nil {
		return client.Close()
	}
	return nil
}

// sshChannelConn 抹平 SSH 通道不支持 deadline 的差异。
//
// x/crypto/ssh 的通道对 SetDeadline 一律返回错误，而 net/http、TLS 栈都会
// 例行设置 deadline；把错误原样抛上去会让完全正常的隧道看起来像坏连接。
// 超时控制交给 context / http.Client.Timeout，它们通过关闭连接来生效。
type sshChannelConn struct {
	net.Conn
}

func (c *sshChannelConn) SetDeadline(time.Time) error      { return nil }
func (c *sshChannelConn) SetReadDeadline(time.Time) error  { return nil }
func (c *sshChannelConn) SetWriteDeadline(time.Time) error { return nil }
