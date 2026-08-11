package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"aegis/internal/config"
	geodomain "aegis/internal/domain/geo"
	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// 分析层默认参数（配置为 0 时生效）。
const (
	defaultGeoRollupInterval       = 10 * time.Minute
	defaultGeoRetentionMonths      = 6
	defaultGeoProfileRecomputeDays = 90
)

// GeoAnalyticsService 地理分析服务（L3 离线层）。
//
// 职责：
//   - 小时聚合：把 firewall_logs / login_geo_events 滚动汇总进 geo_stats_hourly
//     （仪表盘只读聚合表，永不扫明细）
//   - 每日维护：画像基线重算、分区滚动（建下月分区 / DROP 过期分区）
//   - 管理端查询：热力图、攻击聚类、用户轨迹
//
// 所有方法都由 Worker 定时循环或管理 API 调用，不出现在请求热路径。
type GeoAnalyticsService struct {
	log *zap.Logger
	cfg config.GeoRiskConfig
	pg  *pgrepo.Repository
}

// NewGeoAnalyticsService 构造服务并规范化配置默认值。
func NewGeoAnalyticsService(log *zap.Logger, cfg config.GeoRiskConfig, pg *pgrepo.Repository) *GeoAnalyticsService {
	if cfg.RollupInterval <= 0 {
		cfg.RollupInterval = defaultGeoRollupInterval
	}
	if cfg.RetentionMonths <= 0 {
		cfg.RetentionMonths = defaultGeoRetentionMonths
	}
	if cfg.ProfileRecomputeDays <= 0 {
		cfg.ProfileRecomputeDays = defaultGeoProfileRecomputeDays
	}
	return &GeoAnalyticsService{log: log, cfg: cfg, pg: pg}
}

// RollupInterval 返回聚合任务执行间隔（Worker 循环用）。
func (s *GeoAnalyticsService) RollupInterval() time.Duration { return s.cfg.RollupInterval }

// ──────────────────────────────────────
// 定时任务（Worker 循环调用）
// ──────────────────────────────────────

// RunHourlyRollup 重算最近 3 个小时桶（含当前未满小时；幂等可重跑，
// 覆盖 Worker 短暂停机造成的缺口）。
func (s *GeoAnalyticsService) RunHourlyRollup(ctx context.Context) error {
	now := time.Now().UTC()
	start := now.Truncate(time.Hour).Add(-2 * time.Hour)
	end := now.Truncate(time.Hour).Add(time.Hour)
	for _, kind := range []string{geodomain.StatsKindBlock, geodomain.StatsKindLogin} {
		if err := s.pg.RollupGeoStatsRange(ctx, kind, start, end); err != nil {
			return fmt.Errorf("rollup %s [%s, %s): %w", kind, start.Format(time.RFC3339), end.Format(time.RFC3339), err)
		}
	}
	return nil
}

// BackfillRollup 手动回填指定时间范围的聚合（管理端补数用）。
func (s *GeoAnalyticsService) BackfillRollup(ctx context.Context, start, end time.Time) error {
	start = start.UTC().Truncate(time.Hour)
	end = end.UTC().Truncate(time.Hour).Add(time.Hour)
	if !end.After(start) {
		return fmt.Errorf("时间范围无效")
	}
	if end.Sub(start) > 31*24*time.Hour {
		return fmt.Errorf("单次回填不能超过 31 天")
	}
	for _, kind := range []string{geodomain.StatsKindBlock, geodomain.StatsKindLogin} {
		if err := s.pg.RollupGeoStatsRange(ctx, kind, start, end); err != nil {
			return err
		}
	}
	return nil
}

// RunDailyMaintenance 每日维护：分区滚动 + 画像基线重算。
func (s *GeoAnalyticsService) RunDailyMaintenance(ctx context.Context) {
	if err := s.pg.EnsureLoginGeoPartitions(ctx); err != nil {
		s.log.Warn("ensure login geo partitions failed", zap.Error(err))
	}
	dropped, err := s.pg.DropLoginGeoPartitionsBefore(ctx, s.cfg.RetentionMonths)
	if err != nil {
		s.log.Warn("drop expired login geo partitions failed", zap.Error(err))
	} else if len(dropped) > 0 {
		s.log.Info("login geo partitions dropped", zap.Strings("partitions", dropped))
	}
	updated, err := s.pg.RecomputeUserGeoProfiles(ctx, s.cfg.ProfileRecomputeDays)
	if err != nil {
		s.log.Warn("recompute user geo profiles failed", zap.Error(err))
	} else {
		s.log.Info("user geo profiles recomputed", zap.Int64("updated", updated))
	}
}

// ──────────────────────────────────────
// 管理端查询
// ──────────────────────────────────────

// Heatmap 查询热力图（仅读 geo_stats_hourly 预聚合）。
func (s *GeoAnalyticsService) Heatmap(ctx context.Context, q geodomain.HeatmapQuery) (*geodomain.HeatmapResult, error) {
	q.Kind = strings.ToLower(strings.TrimSpace(q.Kind))
	if q.Kind != geodomain.StatsKindBlock && q.Kind != geodomain.StatsKindLogin {
		return nil, fmt.Errorf("无效的 kind（允许: block/login）")
	}
	now := time.Now().UTC()
	if q.End.IsZero() {
		q.End = now
	}
	if q.Start.IsZero() {
		q.Start = q.End.Add(-24 * time.Hour)
	}
	if !q.End.After(q.Start) {
		return nil, fmt.Errorf("时间范围无效")
	}
	if q.End.Sub(q.Start) > 90*24*time.Hour {
		return nil, fmt.Errorf("查询窗口不能超过 90 天")
	}
	return s.pg.QueryGeoHeatmap(ctx, q)
}

// Clusters 攻击源 DBSCAN 聚类（带时间窗口的按需 PostGIS 查询）。
func (s *GeoAnalyticsService) Clusters(ctx context.Context, hours int, epsDegrees float64, minPoints, limit int) ([]geodomain.Cluster, error) {
	if hours <= 0 || hours > 7*24 {
		hours = 24
	}
	since := time.Now().UTC().Add(-time.Duration(hours) * time.Hour)
	return s.pg.QueryGeoClusters(ctx, since, epsDegrees, minPoints, limit)
}

// UserTrail 用户登录轨迹回放。
func (s *GeoAnalyticsService) UserTrail(ctx context.Context, userID, appID int64, limit int) ([]geodomain.TrailPoint, error) {
	if userID <= 0 || appID <= 0 {
		return nil, fmt.Errorf("userId 与 appId 必填")
	}
	return s.pg.ListUserGeoTrail(ctx, userID, appID, limit)
}
