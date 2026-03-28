package service

import (
	"context"
	"fmt"
	"time"

	redisrepo "aegis/internal/repository/redis"

	gojson "github.com/goccy/go-json"
	"go.uber.org/zap"
)

const (
	monitorSnapshotTTL    = 60 * time.Second // 快照缓存 60 秒
	monitorCollectTimeout = 30 * time.Second // 单次采集超时
)

// ── 查询范围常量（前端传入） ──

const (
	HistoryRangeHour  = "hour"  // 最近 1 小时，原始粒度
	HistoryRangeDay   = "day"   // 最近 24 小时，原始粒度
	HistoryRangeWeek  = "week"  // 最近 7 天，小时聚合
	HistoryRangeMonth = "month" // 最近 30 天，小时聚合
)

// SetMonitorRepo 设置监控 Redis 仓库（在 bootstrap 中调用）
func (s *MonitorService) SetMonitorRepo(repo *redisrepo.MonitorRepository) {
	s.monitorRepo = repo
}

// SetCrashLog 设置崩溃日志管理器（在 bootstrap 中调用）
func (s *MonitorService) SetCrashLog(cl interface{ Write(component string, r interface{}, recovered bool) }) {
	s.crashLog = cl
}

// StartCollector 启动后台定时采集（在 bootstrap 中调用）
func (s *MonitorService) StartCollector(ctx context.Context, interval time.Duration) {
	if s.monitorRepo == nil {
		s.log.Warn("监控采集器未启动：monitorRepo 为空")
		return
	}
	s.stopCollect = make(chan struct{})
	go s.collectorLoop(ctx, interval)
	s.log.Info("监控后台采集器已启动", zap.Duration("interval", interval))
}

// StopCollector 停止采集器
func (s *MonitorService) StopCollector() {
	if s.stopCollect != nil {
		close(s.stopCollect)
		s.stopCollect = nil
	}
}

func (s *MonitorService) collectorLoop(parentCtx context.Context, interval time.Duration) {
	// 永久循环：即使整个内层 loop panic，也自动重启，绝不退出
	for {
		exited := s.collectorInnerLoop(parentCtx, interval)
		if exited {
			// 正常退出（stop 信号或 ctx 取消）
			return
		}
		// panic 恢复后等待一个周期再重启，避免高频 panic 风暴
		s.log.Warn("监控采集器将在下一周期自动重启")
		select {
		case <-time.After(interval):
		case <-s.stopCollect:
			return
		case <-parentCtx.Done():
			return
		}
	}
}

// collectorInnerLoop 内层采集循环，返回 true 表示正常退出，false 表示 panic 恢复
func (s *MonitorService) collectorInnerLoop(parentCtx context.Context, interval time.Duration) (normalExit bool) {
	defer func() {
		if r := recover(); r != nil {
			if s.crashLog != nil {
				s.crashLog.Write("monitor.collector", r, true)
			}
			s.log.Error("监控采集器 panic（将自动重启）", zap.Any("panic", r), zap.Stack("stack"))
			normalExit = false
		}
	}()

	s.safeCollectOnce(parentCtx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	hourlyTicker := time.NewTicker(1 * time.Hour)
	defer hourlyTicker.Stop()

	for {
		select {
		case <-s.stopCollect:
			s.log.Info("监控后台采集器已停止")
			return true
		case <-parentCtx.Done():
			return true
		case <-ticker.C:
			s.safeCollectOnce(parentCtx)
		case <-hourlyTicker.C:
			s.safeAggregateHourly(parentCtx)
		}
	}
}

// safeCollectOnce 用 recover 包裹单次采集
func (s *MonitorService) safeCollectOnce(parentCtx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("监控采集 panic（已恢复，将在下一周期重试）", zap.Any("panic", r))
		}
	}()
	s.collectOnce(parentCtx)
}

// safeAggregateHourly 用 recover 包裹小时聚合
func (s *MonitorService) safeAggregateHourly(parentCtx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("监控小时聚合 panic（已恢复）", zap.Any("panic", r))
		}
	}()
	s.aggregateHourly(parentCtx)
}

func (s *MonitorService) collectOnce(parentCtx context.Context) {
	ctx, cancel := context.WithTimeout(parentCtx, monitorCollectTimeout)
	defer cancel()

	// 1. 采集系统总览
	systemResult, err := s.SystemOverview(ctx)
	if err != nil {
		s.log.Debug("监控采集：系统总览失败", zap.Error(err))
		return
	}

	// 2. 写入系统快照
	if err := s.monitorRepo.SetSnapshot(ctx, "system", systemResult, monitorSnapshotTTL); err != nil {
		s.log.Debug("监控采集：写入系统快照失败", zap.Error(err))
	}

	// 3. 写入应用列表快照
	if err := s.monitorRepo.SetSnapshot(ctx, "apps", systemResult.Applications, monitorSnapshotTTL); err != nil {
		s.log.Debug("监控采集：写入应用列表快照失败", zap.Error(err))
	}

	// 4. 追加系统组件原始历史
	now := time.Now().UnixMilli()
	for _, comp := range systemResult.Components {
		latency := extractLatency(comp.Meta)
		point := redisrepo.MonitorHistoryPoint{T: now, S: comp.Status, L: latency}
		ns := "system:" + comp.Key
		if err := s.monitorRepo.AppendHistory(ctx, ns, point); err != nil {
			s.log.Debug("监控采集：追加系统历史失败", zap.String("key", comp.Key), zap.Error(err))
		}
		_ = s.monitorRepo.TrimRawHistory(ctx, ns, redisrepo.MonitorRawHistoryMax)
	}

	// 5. 采集每个应用
	for _, appBrief := range systemResult.Applications {
		appCtx, appCancel := context.WithTimeout(parentCtx, monitorCollectTimeout)
		appResult, appErr := s.AppOverview(appCtx, appBrief.ID)
		appCancel()
		if appErr != nil {
			s.log.Debug("监控采集：应用详情失败", zap.Int64("appID", appBrief.ID), zap.Error(appErr))
			continue
		}

		suffix := fmt.Sprintf("app:%d", appBrief.ID)
		if err := s.monitorRepo.SetSnapshot(ctx, suffix, appResult, monitorSnapshotTTL); err != nil {
			s.log.Debug("监控采集：写入应用快照失败", zap.Int64("appID", appBrief.ID), zap.Error(err))
		}

		for _, comp := range appResult.Components {
			latency := extractLatency(comp.Meta)
			point := redisrepo.MonitorHistoryPoint{T: now, S: comp.Status, L: latency}
			ns := fmt.Sprintf("app:%d:%s", appBrief.ID, comp.Key)
			if err := s.monitorRepo.AppendHistory(ctx, ns, point); err != nil {
				s.log.Debug("监控采集：追加应用历史失败", zap.String("key", comp.Key), zap.Error(err))
			}
			_ = s.monitorRepo.TrimRawHistory(ctx, ns, redisrepo.MonitorRawHistoryMax)
		}
	}

	s.log.Debug("监控采集完成", zap.Int("apps", len(systemResult.Applications)), zap.Int("components", len(systemResult.Components)))
}

// ── 小时聚合 ──

// aggregateHourly 将上一个完整小时的原始数据聚合为一条小时点
func (s *MonitorService) aggregateHourly(parentCtx context.Context) {
	ctx, cancel := context.WithTimeout(parentCtx, monitorCollectTimeout)
	defer cancel()

	// 聚合上一个完整小时
	now := time.Now().UTC()
	hourEnd := now.Truncate(time.Hour)              // 本小时开始 = 上小时结束
	hourStart := hourEnd.Add(-1 * time.Hour)         // 上小时开始
	startMs := hourStart.UnixMilli()
	endMs := hourEnd.UnixMilli() - 1

	// 获取当前所有组件 key（从最新快照中提取）
	cached, err := s.CachedSystemOverview(ctx)
	if err != nil || cached == nil {
		s.log.Debug("监控小时聚合：无可用快照，跳过")
		return
	}

	// 聚合系统组件
	for _, comp := range cached.Components {
		ns := "system:" + comp.Key
		s.aggregateOneHourly(ctx, ns, startMs, endMs, hourStart.UnixMilli())
	}

	// 聚合应用组件
	for _, appBrief := range cached.Applications {
		appCached, appErr := s.CachedAppOverview(ctx, appBrief.ID)
		if appErr != nil || appCached == nil {
			continue
		}
		for _, comp := range appCached.Components {
			ns := fmt.Sprintf("app:%d:%s", appBrief.ID, comp.Key)
			s.aggregateOneHourly(ctx, ns, startMs, endMs, hourStart.UnixMilli())
		}
	}

	s.log.Debug("监控小时聚合完成", zap.Time("hour", hourStart))
}

// aggregateOneHourly 对单个命名空间的上一小时原始数据进行聚合
func (s *MonitorService) aggregateOneHourly(ctx context.Context, namespace string, startMs, endMs, hourTs int64) {
	points, err := s.monitorRepo.GetRawPointsForHour(ctx, namespace, startMs, endMs)
	if err != nil || len(points) == 0 {
		return
	}

	// 聚合策略：状态取最差（unavailable > degraded > available），延迟取平均
	worstStatus := "available"
	var totalLatency float64
	for _, p := range points {
		if statusPriority(p.S) > statusPriority(worstStatus) {
			worstStatus = p.S
		}
		totalLatency += p.L
	}
	avgLatency := totalLatency / float64(len(points))

	hourlyPoint := redisrepo.MonitorHistoryPoint{
		T: hourTs,
		S: worstStatus,
		L: avgLatency,
	}
	if err := s.monitorRepo.AppendHourlyHistory(ctx, namespace, hourlyPoint); err != nil {
		s.log.Debug("监控小时聚合：写入失败", zap.String("ns", namespace), zap.Error(err))
		return
	}
	_ = s.monitorRepo.TrimHourlyHistory(ctx, namespace, redisrepo.MonitorHourlyHistoryMax)
}

func statusPriority(s string) int {
	switch s {
	case "unavailable":
		return 2
	case "degraded":
		return 1
	default:
		return 0
	}
}

// ── 缓存读取 ──

// CachedSystemOverview 从 Redis 读取系统快照（<1ms）
func (s *MonitorService) CachedSystemOverview(ctx context.Context) (*MonitorOverview, error) {
	if s.monitorRepo == nil {
		return nil, nil
	}
	raw, err := s.monitorRepo.GetSnapshot(ctx, "system")
	if err != nil || raw == nil {
		return nil, err
	}
	var result MonitorOverview
	if err := gojson.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// CachedApps 从 Redis 读取应用列表快照
func (s *MonitorService) CachedApps(ctx context.Context) ([]MonitorAppBrief, error) {
	if s.monitorRepo == nil {
		return nil, nil
	}
	raw, err := s.monitorRepo.GetSnapshot(ctx, "apps")
	if err != nil || raw == nil {
		return nil, err
	}
	var result []MonitorAppBrief
	if err := gojson.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// CachedAppOverview 从 Redis 读取应用快照
func (s *MonitorService) CachedAppOverview(ctx context.Context, appID int64) (*AppMonitorOverview, error) {
	if s.monitorRepo == nil {
		return nil, nil
	}
	raw, err := s.monitorRepo.GetSnapshot(ctx, fmt.Sprintf("app:%d", appID))
	if err != nil || raw == nil {
		return nil, err
	}
	var result AppMonitorOverview
	if err := gojson.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ── 历史查询（支持 range: hour/day/week/month） ──

// resolveHistoryRange 将范围字符串转为时间窗口和是否使用小时聚合层
func resolveHistoryRange(rangeStr string) (startMs, endMs int64, useHourly bool, limit int64) {
	now := time.Now().UTC()
	endMs = now.UnixMilli()

	switch rangeStr {
	case HistoryRangeHour:
		startMs = now.Add(-1 * time.Hour).UnixMilli()
		useHourly = false
		limit = 240 // 15s × 240 = 1h
	case HistoryRangeDay:
		startMs = now.Add(-24 * time.Hour).UnixMilli()
		useHourly = false
		limit = 5760 // 15s × 5760 = 24h
	case HistoryRangeWeek:
		startMs = now.Add(-7 * 24 * time.Hour).UnixMilli()
		useHourly = true
		limit = 168 // 24h × 7 = 168 小时
	case HistoryRangeMonth:
		startMs = now.Add(-30 * 24 * time.Hour).UnixMilli()
		useHourly = true
		limit = 720 // 24h × 30 = 720 小时
	default:
		// 默认最近 1 小时
		startMs = now.Add(-1 * time.Hour).UnixMilli()
		useHourly = false
		limit = 240
	}
	return
}

// SystemHistory 批量获取系统组件历史（支持 range 参数）
func (s *MonitorService) SystemHistory(ctx context.Context, keys []string, rangeStr string) (map[string][]redisrepo.MonitorHistoryPoint, error) {
	if s.monitorRepo == nil {
		return map[string][]redisrepo.MonitorHistoryPoint{}, nil
	}
	startMs, endMs, useHourly, limit := resolveHistoryRange(rangeStr)

	namespaces := make([]string, len(keys))
	for i, k := range keys {
		namespaces[i] = "system:" + k
	}
	result, err := s.monitorRepo.GetBatchHistoryRange(ctx, namespaces, startMs, endMs, limit, useHourly)
	if err != nil {
		return nil, err
	}
	mapped := make(map[string][]redisrepo.MonitorHistoryPoint, len(result))
	for i, k := range keys {
		mapped[k] = result[namespaces[i]]
	}
	return mapped, nil
}

// AppHistory 批量获取应用组件历史（支持 range 参数）
func (s *MonitorService) AppHistory(ctx context.Context, appID int64, keys []string, rangeStr string) (map[string][]redisrepo.MonitorHistoryPoint, error) {
	if s.monitorRepo == nil {
		return map[string][]redisrepo.MonitorHistoryPoint{}, nil
	}
	startMs, endMs, useHourly, limit := resolveHistoryRange(rangeStr)

	namespaces := make([]string, len(keys))
	for i, k := range keys {
		namespaces[i] = fmt.Sprintf("app:%d:%s", appID, k)
	}
	result, err := s.monitorRepo.GetBatchHistoryRange(ctx, namespaces, startMs, endMs, limit, useHourly)
	if err != nil {
		return nil, err
	}
	mapped := make(map[string][]redisrepo.MonitorHistoryPoint, len(result))
	for i, k := range keys {
		mapped[k] = result[namespaces[i]]
	}
	return mapped, nil
}

// ── 辅助 ──

func extractLatency(meta map[string]any) float64 {
	if meta == nil {
		return 0
	}
	if v, ok := meta["latencyMs"]; ok {
		switch t := v.(type) {
		case float64:
			return t
		case int64:
			return float64(t)
		}
	}
	return 0
}
