package bootstrap

import (
	"sync"

	"aegis/pkg/egress"
	"go.uber.org/zap"
)

// 出海网关在**进程内只能有一份**。
//
// Unified 模式（cmd/server）把 API 与 Worker 跑在同一个进程里，如果各建一份，
// 就会出现两套健康探测、两份统计、以及「控制台改了配置但 Worker 还用旧路由」
// 三种都很难排查的错位。这里用一个进程级单例把两边收敛到同一张路由表上。
var (
	egressMu     sync.Mutex
	sharedEgress *egress.Gateway
)

// ensureEgressGateway 返回进程级唯一的出海网关。
// owned 为 true 表示本次调用创建了它，调用方负责 Start 与 Close。
func ensureEgressGateway(cfg egress.Config, log *zap.Logger) (gateway *egress.Gateway, owned bool, err error) {
	egressMu.Lock()
	defer egressMu.Unlock()
	if sharedEgress != nil {
		return sharedEgress, false, nil
	}
	gw, err := egress.New(cfg, log)
	if err != nil {
		return nil, false, err
	}
	// 装配成全局默认之后，所有 egress.NewClient 构造的客户端立即按规则出海。
	egress.SetDefault(gw)
	sharedEgress = gw
	return gw, true, nil
}

// releaseEgressGateway 关闭并释放单例，只应由创建方在进程退出时调用。
func releaseEgressGateway(gateway *egress.Gateway) {
	if gateway == nil {
		return
	}
	egressMu.Lock()
	if sharedEgress == gateway {
		sharedEgress = nil
	}
	egressMu.Unlock()
	gateway.Close()
}
