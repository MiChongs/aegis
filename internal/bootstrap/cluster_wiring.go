package bootstrap

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// 后台任务的集群租约名。改名等于换一把锁：发布期间新旧版本会各持一把、同时干活，
// 所以名字一经上线就不要再动。
const (
	// leaseAutoSign 自动签到的到点执行与全量重建。API 与 Worker 都跑这个循环，
	// 认领本身靠 ZREM 不会重复签到，但每 15 分钟一次的全量重建扫描只需要全集群做一次。
	leaseAutoSign = "autosign.loop"
	// leaseAdminSessionCleanup 管理员过期会话清理：幂等，但每个副本都扫一遍纯属浪费。
	leaseAdminSessionCleanup = "admin.session-cleanup"
)

func (a *APIApp) autoSignGate() bool {
	if a == nil || a.Cluster == nil {
		return true
	}
	if a.autoSignGateFn == nil {
		a.autoSignGateFn = a.Cluster.Gate(leaseAutoSign)
	}
	return a.autoSignGateFn()
}

// attachCluster 把集群句柄交给需要选主或广播的服务。必须在 Elector.Start 之前调用：
// 各服务在 SetCluster 里登记租约名，Start 的第一轮就会去抢。
func (a *APIApp) attachCluster() {
	if a == nil || a.Cluster == nil {
		return
	}
	// 在 Start 之前登记，第一轮就能抢到
	a.autoSignGate()
}

// watchSchema 关闭了启动时迁移的实例，在库追上镜像里的迁移之前一直报未就绪，每 15 秒复查一次。
// 查询本身出错不判落后（数据库抖动由 /readyz 自己的 Postgres 检查负责）。
func (a *APIApp) watchSchema(ctx context.Context) {
	if a == nil || a.drain == nil || a.Postgres == nil || a.Config.Cluster.MigrateOnBoot {
		return
	}
	check := func() bool {
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		pending, err := pendingNewMigrations(checkCtx, a.Postgres)
		if err != nil {
			a.drain.schemaBehind.Store(false)
			return false
		}
		behind := len(pending) > 0
		if behind && !a.drain.schemaBehind.Load() {
			a.Logger.Error("数据库迁移落后于当前版本，本实例报未就绪，请先执行 migrate", zap.Strings("pending", pending))
		}
		a.drain.schemaBehind.Store(behind)
		return behind
	}
	if !check() {
		return
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !check() {
				a.Logger.Info("数据库迁移已追上，本实例恢复就绪")
				return
			}
		}
	}
}
