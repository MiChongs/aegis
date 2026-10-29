package mihomo

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"

	"aegis/pkg/egress"
)

// Protocol 出海网关里的 mihomo 端点协议。
//
// 端点配置不填 address：入站地址与口令在核心启动后才确定，拨号时向进程级核心要。
// Options["target"] 是要交给的策略组或节点名，留空表示「交给核心按分流规则决定」。
// TCP 与 UDP 都支持：每个目标有专用入站，UDP 经 SOCKS5 UDP ASSOCIATE 交给核心。
const Protocol egress.Protocol = "mihomo"

// OptionTarget 端点 Options 里存目标名的键。
const OptionTarget = "target"

func init() {
	egress.RegisterAddresslessProtocol(Protocol, newDialer)
	egress.RegisterProtocolUDP(Protocol, egress.UDPOverUDP)
}

type coreDialer struct {
	target string

	mu    sync.Mutex
	key   string
	inner egress.Dialer
}

func newDialer(cfg egress.EndpointConfig) (egress.Dialer, error) {
	if strings.TrimSpace(cfg.Via) != "" {
		return nil, fmt.Errorf("mihomo 端点不能再经由其它端点（核心在本机）")
	}
	return &coreDialer{target: strings.TrimSpace(cfg.Options[OptionTarget])}, nil
}

func (d *coreDialer) DialContext(ctx context.Context, base egress.BaseDialFunc, network, address string) (net.Conn, error) {
	core := Default()
	if core == nil {
		return nil, fmt.Errorf("mihomo 核心未装配")
	}
	if !core.Running() {
		return nil, fmt.Errorf("mihomo 核心未运行（%s）", statusLabel(core.Status().Status))
	}
	addr, password, ok := core.Inbound(d.target)
	if !ok {
		return nil, fmt.Errorf("mihomo 核心里没有「%s」，配置可能还没生效", d.target)
	}
	inner, err := d.socks(addr, password)
	if err != nil {
		return nil, err
	}
	return inner.DialContext(ctx, base, network, address)
}

// socks 复用内置的 socks5h 拨号器；入站地址或口令变了（核心换了端口）才重建。
func (d *coreDialer) socks(addr, password string) (egress.Dialer, error) {
	key := addr + "\x00" + password
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.inner != nil && d.key == key {
		return d.inner, nil
	}
	inner, err := egress.NewDialer(egress.EndpointConfig{
		Name:     "mihomo-inbound",
		Protocol: egress.ProtocolSOCKS5H,
		Address:  addr,
		Username: InboundUser,
		Password: password,
	})
	if err != nil {
		return nil, err
	}
	d.inner, d.key = inner, key
	return inner, nil
}
