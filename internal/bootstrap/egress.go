package bootstrap

import (
	"path/filepath"
	"sync"

	"aegis/internal/config"
	"aegis/pkg/egress"
	"aegis/pkg/egress/mihomo"
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
	sharedCore   *mihomo.Core
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

// ensureMihomoCore 返回进程级唯一的 mihomo 核心托管器（与网关同一个生命周期）。
// role 决定家目录下的子目录：API 与 Worker 分进程部署时各用各的缓存文件，互不抢锁。
// 托管器构造时不会启动进程，第一次应用含 mihomo 节点的画布时才启动。
func ensureMihomoCore(cfg config.MihomoConfig, role string, log *zap.Logger) *mihomo.Core {
	egressMu.Lock()
	defer egressMu.Unlock()
	if sharedCore != nil {
		return sharedCore
	}
	sharedCore = mihomo.NewCore(mihomo.Options{
		Home:        filepath.Join(cfg.Home, role),
		Binary:      cfg.Binary,
		AutoInstall: cfg.AutoInstall,
		Mirror:      cfg.Mirror,
		Version:     cfg.Version,
		Log:         log,
	})
	mihomo.SetDefault(sharedCore)
	return sharedCore
}

// releaseEgressGateway 关闭并释放单例（连同 mihomo 核心），只应由创建方在进程退出时调用。
func releaseEgressGateway(gateway *egress.Gateway) {
	if gateway == nil {
		return
	}
	egressMu.Lock()
	core := sharedCore
	if sharedEgress == gateway {
		sharedEgress = nil
		sharedCore = nil
	}
	egressMu.Unlock()
	gateway.Close()
	if core != nil {
		core.Close()
	}
}
