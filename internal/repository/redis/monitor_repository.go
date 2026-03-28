package redis

import (
	"context"
	"fmt"
	"time"

	gojson "github.com/goccy/go-json"
	redislib "github.com/redis/go-redis/v9"
)

// MonitorHistoryPoint 历史检测点（紧凑 JSON）
type MonitorHistoryPoint struct {
	T int64   `json:"t"`           // unix 毫秒时间戳
	S string  `json:"s"`           // 状态: available / degraded / unavailable
	L float64 `json:"l,omitempty"` // 检测耗时 ms
}

const (
	// 原始数据保留 24 小时（15s × 5760 = 24h）
	MonitorRawHistoryMax = 5760
	// 小时聚合数据保留 30 天（24h × 30 = 720 条）
	MonitorHourlyHistoryMax = 720
)

// MonitorRepository Redis 监控快照缓存与历史存储
type MonitorRepository struct {
	client    *redislib.Client
	keyPrefix string
}

// NewMonitorRepository 创建监控 Redis 仓库
func NewMonitorRepository(client *redislib.Client, keyPrefix string) *MonitorRepository {
	return &MonitorRepository{client: client, keyPrefix: keyPrefix}
}

// ── 快照缓存 ──

func (r *MonitorRepository) snapshotKey(suffix string) string {
	return fmt.Sprintf("%s:monitor:snapshot:%s", r.keyPrefix, suffix)
}

// SetSnapshot 写入快照缓存
func (r *MonitorRepository) SetSnapshot(ctx context.Context, suffix string, data any, ttl time.Duration) error {
	raw, err := gojson.Marshal(data)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, r.snapshotKey(suffix), raw, ttl).Err()
}

// GetSnapshot 读取快照缓存（返回原始 JSON bytes）
func (r *MonitorRepository) GetSnapshot(ctx context.Context, suffix string) ([]byte, error) {
	raw, err := r.client.Get(ctx, r.snapshotKey(suffix)).Bytes()
	if err == redislib.Nil {
		return nil, nil
	}
	return raw, err
}

// ── 历史存储（双层 Sorted Set） ──
//
// 原始层 (raw)：  {prefix}:monitor:history:{namespace}        — 15s 粒度，保留 24h
// 小时层 (hourly)：{prefix}:monitor:hourly:{namespace}        — 1h 粒度，保留 30 天

func (r *MonitorRepository) rawHistoryKey(namespace string) string {
	return fmt.Sprintf("%s:monitor:history:%s", r.keyPrefix, namespace)
}

func (r *MonitorRepository) hourlyHistoryKey(namespace string) string {
	return fmt.Sprintf("%s:monitor:hourly:%s", r.keyPrefix, namespace)
}

// AppendHistory 追加原始历史点
func (r *MonitorRepository) AppendHistory(ctx context.Context, namespace string, point MonitorHistoryPoint) error {
	raw, err := gojson.Marshal(point)
	if err != nil {
		return err
	}
	return r.client.ZAdd(ctx, r.rawHistoryKey(namespace), redislib.Z{
		Score:  float64(point.T),
		Member: string(raw),
	}).Err()
}

// AppendHourlyHistory 追加小时聚合历史点
func (r *MonitorRepository) AppendHourlyHistory(ctx context.Context, namespace string, point MonitorHistoryPoint) error {
	raw, err := gojson.Marshal(point)
	if err != nil {
		return err
	}
	return r.client.ZAdd(ctx, r.hourlyHistoryKey(namespace), redislib.Z{
		Score:  float64(point.T),
		Member: string(raw),
	}).Err()
}

// TrimRawHistory 修剪原始历史
func (r *MonitorRepository) TrimRawHistory(ctx context.Context, namespace string, maxCount int64) error {
	return r.client.ZRemRangeByRank(ctx, r.rawHistoryKey(namespace), 0, -maxCount-1).Err()
}

// TrimHourlyHistory 修剪小时聚合历史
func (r *MonitorRepository) TrimHourlyHistory(ctx context.Context, namespace string, maxCount int64) error {
	return r.client.ZRemRangeByRank(ctx, r.hourlyHistoryKey(namespace), 0, -maxCount-1).Err()
}

// GetRawHistoryRange 按时间范围获取原始历史（用于小时/天查询）
func (r *MonitorRepository) GetRawHistoryRange(ctx context.Context, namespace string, startMs, endMs int64, limit int64) ([]MonitorHistoryPoint, error) {
	return r.zrangeByScore(ctx, r.rawHistoryKey(namespace), startMs, endMs, limit)
}

// GetHourlyHistoryRange 按时间范围获取小时聚合历史（用于周/月查询）
func (r *MonitorRepository) GetHourlyHistoryRange(ctx context.Context, namespace string, startMs, endMs int64, limit int64) ([]MonitorHistoryPoint, error) {
	return r.zrangeByScore(ctx, r.hourlyHistoryKey(namespace), startMs, endMs, limit)
}

// GetBatchHistoryRange 批量按时间范围获取历史（自动选层）
func (r *MonitorRepository) GetBatchHistoryRange(ctx context.Context, namespaces []string, startMs, endMs int64, limit int64, useHourly bool) (map[string][]MonitorHistoryPoint, error) {
	if len(namespaces) == 0 {
		return map[string][]MonitorHistoryPoint{}, nil
	}

	pipe := r.client.Pipeline()
	cmds := make(map[string]*redislib.StringSliceCmd, len(namespaces))
	for _, ns := range namespaces {
		key := r.rawHistoryKey(ns)
		if useHourly {
			key = r.hourlyHistoryKey(ns)
		}
		cmds[ns] = pipe.ZRangeArgs(ctx, redislib.ZRangeArgs{
			Key:     key,
			ByScore: true,
			Start:   fmt.Sprintf("%d", startMs),
			Stop:    fmt.Sprintf("%d", endMs),
			Count:   limit,
			Rev:     true,
		})
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redislib.Nil {
		return nil, err
	}

	result := make(map[string][]MonitorHistoryPoint, len(namespaces))
	for ns, cmd := range cmds {
		values, err := cmd.Result()
		if err != nil {
			result[ns] = nil
			continue
		}
		result[ns] = parseHistoryPoints(values)
	}
	return result, nil
}

// GetRawHistory 获取最近 limit 条原始历史（按时间倒序，兼容旧调用）
func (r *MonitorRepository) GetRawHistory(ctx context.Context, namespace string, limit int64) ([]MonitorHistoryPoint, error) {
	results, err := r.client.ZRangeArgs(ctx, redislib.ZRangeArgs{
		Key:   r.rawHistoryKey(namespace),
		Start: 0,
		Stop:  limit - 1,
		Rev:   true,
	}).Result()
	if err != nil {
		return nil, err
	}
	return parseHistoryPoints(results), nil
}

// GetBatchHistory 批量获取最近 limit 条原始历史（兼容旧调用）
func (r *MonitorRepository) GetBatchHistory(ctx context.Context, namespaces []string, limit int64) (map[string][]MonitorHistoryPoint, error) {
	if len(namespaces) == 0 {
		return map[string][]MonitorHistoryPoint{}, nil
	}

	pipe := r.client.Pipeline()
	cmds := make(map[string]*redislib.StringSliceCmd, len(namespaces))
	for _, ns := range namespaces {
		cmds[ns] = pipe.ZRangeArgs(ctx, redislib.ZRangeArgs{
			Key:   r.rawHistoryKey(ns),
			Start: 0,
			Stop:  limit - 1,
			Rev:   true,
		})
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redislib.Nil {
		return nil, err
	}

	result := make(map[string][]MonitorHistoryPoint, len(namespaces))
	for ns, cmd := range cmds {
		values, err := cmd.Result()
		if err != nil {
			result[ns] = nil
			continue
		}
		result[ns] = parseHistoryPoints(values)
	}
	return result, nil
}

// ── 小时聚合辅助 ──

// GetRawPointsForHour 获取指定小时内的所有原始点（用于聚合）
func (r *MonitorRepository) GetRawPointsForHour(ctx context.Context, namespace string, hourStartMs, hourEndMs int64) ([]MonitorHistoryPoint, error) {
	return r.zrangeByScore(ctx, r.rawHistoryKey(namespace), hourStartMs, hourEndMs, 0)
}

// ── 内部辅助 ──

func (r *MonitorRepository) zrangeByScore(ctx context.Context, key string, startMs, endMs, limit int64) ([]MonitorHistoryPoint, error) {
	args := redislib.ZRangeArgs{
		Key:     key,
		ByScore: true,
		Start:   fmt.Sprintf("%d", startMs),
		Stop:    fmt.Sprintf("%d", endMs),
		Rev:     true,
	}
	if limit > 0 {
		args.Count = limit
	}
	results, err := r.client.ZRangeArgs(ctx, args).Result()
	if err != nil {
		return nil, err
	}
	return parseHistoryPoints(results), nil
}

func parseHistoryPoints(values []string) []MonitorHistoryPoint {
	points := make([]MonitorHistoryPoint, 0, len(values))
	for _, raw := range values {
		var p MonitorHistoryPoint
		if err := gojson.Unmarshal([]byte(raw), &p); err != nil {
			continue
		}
		points = append(points, p)
	}
	return points
}
