package service

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"aegis/internal/cluster"
)

// 存储中心在多副本下的协调。
//
//   - 每日维护（清过期事件、写用量快照）与容量预警事件：写的是共享表，N 个副本各跑一遍就是 N 份快照、N 条预警，
//     由 Gate 选出一个实例负责。
//   - 迁移任务：同一个任务只能有一个执行者，靠 storage_migrations 上的认领 + 心跳（storageMigrationRunner），
//     不用 Gate —— Gate 换手时旧持有者手上那一批对象还在复制，新持有者从同一个游标开始，会把这一批复制两遍、
//     计数记两遍；认领 + 心跳是逐任务的，旧持有者确认自己不再持有之后才会有人接手。
//   - 取消 / 暂停：发起请求的实例本地没在跑这个任务时，广播 TopicStorageMigration 让持有者立刻停；
//     广播丢了也不要紧，持有者的心跳会发现状态已经不是 running，最多一个心跳周期后停下。

const (
	// storageMigrationHeartbeat 迁移持有者续心跳的周期。
	storageMigrationHeartbeat = 20 * time.Second
	// storageMigrationStaleAfter 心跳超过这么久没更新，视为持有者已经没了，别的实例可以接手。
	// 取心跳周期的几倍：数据库偶尔抖一下、一次心跳没写上，不该让任务换手。
	storageMigrationStaleAfter = 2 * time.Minute
	// storageMigrationRescan 接上集群后，每隔这么久扫一遍待跑 / 无人持有的迁移任务，接手宕机实例留下的任务。
	storageMigrationRescan = time.Minute
)

// storageMigrationControl 迁移控制的集群广播消息体。
type storageMigrationControl struct {
	Op string `json:"op"`
	ID int64  `json:"id"`
}

const storageMigrationOpCancel = "cancel"

// storageClusterState 存储服务的集群句柄与后台任务判定。
type storageClusterState struct {
	link         clusterLink
	dailyTask    clusterTask
	capacityTask clusterTask
}

// localStorageClustered 进程接上集群后置 true：本地存储提供者据此在探活结果与「文件不存在」日志里提示多节点风险。
// 提供者是无状态、按配置现建的，没有地方挂这个标志与日志器，只好放在包级。
//
// 接上集群不等于真的跑了多个副本（单机部署也会有 Postgres 租约与 NATS），所以提示的措辞是「多节点部署时……」，
// 只提示、不拦截：单机继续用本地存储完全没问题。
var (
	localStorageClustered atomic.Bool
	localStorageLog       atomic.Pointer[zap.Logger]
	localStorageWarnedAt  atomic.Int64
)

// localStorageMultiNodeHint 本地存储池在多节点下的风险说明，探活结果与日志共用一份措辞。
const localStorageMultiNodeHint = "本地存储池的文件只保存在写入它的那个节点的磁盘上，多节点部署时其他节点读不到；多节点部署需改用 S3 兼容存储池（S3 / R2 / MinIO / OSS 等）"

// localStorageWarnInterval 「本地存储读不到文件」提示的最小间隔：读不到往往是成片的，每次都打会刷屏。
const localStorageWarnInterval = 5 * time.Minute

// warnLocalStorageMiss 本地存储读取时文件不存在：接上集群时提示多节点风险（限频）。
func warnLocalStorageMiss(objectKey string) {
	if !localStorageClustered.Load() {
		return
	}
	log := localStorageLog.Load()
	if log == nil {
		return
	}
	now := time.Now().UnixNano()
	last := localStorageWarnedAt.Load()
	if now-last < int64(localStorageWarnInterval) || !localStorageWarnedAt.CompareAndSwap(last, now) {
		return
	}
	log.Warn("本地存储读取的文件不存在："+localStorageMultiNodeHint, zap.String("key", objectKey))
}

// SetCluster 接上集群协调（bootstrap 在构造完、Start 之前调用）。
func (s *StorageService) SetCluster(c *cluster.Cluster) {
	if c == nil {
		return
	}
	s.clusterState.link.set(c)
	s.clusterState.dailyTask.bind(c, "storage.daily-maintenance")
	s.clusterState.capacityTask.bind(c, "storage.capacity-alerts")
	localStorageClustered.Store(true)
	if s.log != nil {
		localStorageLog.Store(s.log)
	}
	c.Subscribe(cluster.TopicStorageMigration, s.handleMigrationControl)
}

// handleMigrationControl 处理其他实例发来的迁移控制广播。
func (s *StorageService) handleMigrationControl(payload []byte) {
	var msg storageMigrationControl
	if err := json.Unmarshal(payload, &msg); err != nil {
		s.log.Warn("存储迁移控制广播无法解析", zap.Error(err))
		return
	}
	if msg.Op == storageMigrationOpCancel && msg.ID > 0 {
		if s.migrations.cancel(msg.ID) {
			s.log.Info("已按集群广播停止存储迁移任务", zap.Int64("migration", msg.ID))
		}
	}
}

// stopMigration 停掉一个迁移任务：本地在跑就本地取消，否则广播给持有它的实例。
func (s *StorageService) stopMigration(id int64) {
	if s.migrations.cancel(id) {
		return
	}
	s.clusterState.link.publish(s.log, cluster.TopicStorageMigration, storageMigrationControl{Op: storageMigrationOpCancel, ID: id})
}

// migrationOwner 迁移任务的持有者标识。
func (s *StorageService) migrationOwner() string {
	return s.clusterState.link.get().ID()
}

// migrationClaimForced 没接集群（单实例）时认领不看旧持有者：没有别人会来抢，
// 进程崩溃重启后也不必干等旧心跳过期才能续跑 —— 与改造前「启动即续跑」一致。
func (s *StorageService) migrationClaimForced() bool {
	return !s.clusterState.link.attached()
}

// migrationRescanEnabled 只有接上集群才需要周期性地接手别的实例留下的任务。
func (s *StorageService) migrationRescanEnabled() bool {
	return s.clusterState.link.attached()
}

// heartbeatMigration 迁移持有期间续心跳；发现任务已经不归本实例（被接手、被暂停 / 取消）就调用 lost。
func (s *StorageService) heartbeatMigration(ctx context.Context, id int64, owner string, lost func()) {
	ticker := time.NewTicker(storageMigrationHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			beatCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			ok, err := s.pg.HeartbeatStorageMigration(beatCtx, id, owner)
			cancel()
			if err != nil {
				// 写不上心跳不立刻停：数据库抖一下就让任务停摆不划算；真的长时间写不上，
				// 别的实例会在心跳过期后接手，那时本实例下一次心跳就会返回 false。
				if ctx.Err() == nil {
					s.log.Warn("存储迁移心跳写入失败", zap.Int64("migration", id), zap.Error(err))
				}
				continue
			}
			if !ok {
				lost()
				return
			}
		}
	}
}
