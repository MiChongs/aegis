package egress

import (
	"context"
	"net"
)

// directDialer 不做任何协议封装，直接把目标地址交给上一跳。
//
// 它存在的意义是让「代理都挂了就直连」可以写成一条普通的 failover 端点，
// 而不用在选路逻辑里再开一个特例分支。
type directDialer struct{}

func newDirectDialer(EndpointConfig) (Dialer, error) { return directDialer{}, nil }

func (directDialer) DialContext(ctx context.Context, base BaseDialFunc, network, address string) (net.Conn, error) {
	return base(ctx, network, address)
}
