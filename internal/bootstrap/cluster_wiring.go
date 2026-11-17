package bootstrap

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
