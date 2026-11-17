package config

import "time"

// ClusterConfig 多副本部署相关配置。
//
//	AEGIS_ROLE             cmd/server 的角色：all（默认）/ api / worker。
//	AEGIS_NODE_ID          实例标识（日志、集群租约持有者）。Swarm 里设为 {{.Node.Hostname}}-api-{{.Task.Slot}}；
//	                       留空用「主机名-随机串」。进程内还会再拼一段随机串，重启后不会冒用旧进程的租约。
//	MIGRATE_ON_BOOT        API 启动时是否自动执行数据库迁移，默认 true（单机兼容）。
//	                       集群部署设 false，改由一次性的 `aegis-server migrate` 任务在发布前执行。
//	                       即使为 true，多个副本同时启动也是安全的：迁移在 advisory lock 下排队，已执行的版本会跳过。
//	SHUTDOWN_DRAIN_DELAY   收到停止信号后先把 /readyz 置为 503、等这么久再关监听，
//	                       让负载均衡先把本实例摘掉，滚动发布时不丢请求。默认 0（单机无负载均衡）；集群建议 5s。
//	WORKER_HEALTH_ADDR     Worker 角色的健康检查监听地址（/healthz、/readyz），默认 :8089。设为 off 关闭。
//	CLUSTER_LEASE_TTL      后台任务选主租约的有效期，默认 30s：负责实例宕机后最长这么久由别的实例接手。
type ClusterConfig struct {
	Role             string
	NodeID           string
	MigrateOnBoot    bool
	DrainDelay       time.Duration
	WorkerHealthAddr string
	LeaseTTL         time.Duration
}
