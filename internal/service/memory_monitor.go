package service

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"sync"
	"time"

	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// MemoryMetrics 单次采集的内存指标
type MemoryMetrics struct {
	Timestamp    time.Time `json:"timestamp"`
	HeapAlloc    uint64    `json:"heapAlloc"`    // 堆已分配（bytes）
	HeapSys      uint64    `json:"heapSys"`      // 堆系统保留（bytes）
	HeapInuse    uint64    `json:"heapInuse"`     // 堆使用中（bytes）
	HeapIdle     uint64    `json:"heapIdle"`      // 堆空闲（bytes）
	HeapObjects  uint64    `json:"heapObjects"`   // 堆对象数
	StackInuse   uint64    `json:"stackInuse"`    // 栈使用中（bytes）
	GCSys        uint64    `json:"gcSys"`         // GC 元数据（bytes）
	TotalAlloc   uint64    `json:"totalAlloc"`    // 累计分配（bytes）
	Sys          uint64    `json:"sys"`           // 系统保留总量（bytes）
	NumGC        uint32    `json:"numGC"`         // GC 总次数
	LastGCNano   uint64    `json:"lastGCNano"`    // 上次 GC 时间（UnixNano）
	PauseTotalNs uint64    `json:"pauseTotalNs"`  // GC 暂停总时长（ns）
	Goroutines   int       `json:"goroutines"`    // Goroutine 数
	ProcessRSS   uint64    `json:"processRSS"`    // 进程 RSS（需外部设置）
}

// MemoryAlert 内存告警记录
type MemoryAlert struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`   // warning / critical
	Message string    `json:"message"`
	Value   float64   `json:"value"`   // 触发值
}

// MemoryMonitor 采集内存指标并存入 Redis 历史
type MemoryMonitor struct {
	log            *zap.Logger
	redis          *redislib.Client
	keyPrefix      string
	interval       time.Duration
	historyRetain  time.Duration

	mu             sync.RWMutex
	latest         *MemoryMetrics
	alerts         []MemoryAlert // 最近 50 条告警
	cancel         context.CancelFunc
}

func NewMemoryMonitor(log *zap.Logger, redis *redislib.Client, keyPrefix string, interval, historyRetain time.Duration) *MemoryMonitor {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	if historyRetain <= 0 {
		historyRetain = time.Hour
	}
	return &MemoryMonitor{
		log:           log,
		redis:         redis,
		keyPrefix:     keyPrefix,
		interval:      interval,
		historyRetain: historyRetain,
		alerts:        make([]MemoryAlert, 0, 50),
	}
}

// Start 启动后台采集循环
func (m *MemoryMonitor) Start(ctx context.Context) {
	ctx, m.cancel = context.WithCancel(ctx)
	go m.loop(ctx)
}

// Stop 停止采集
func (m *MemoryMonitor) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
}

// Latest 返回最近一次采集的指标
func (m *MemoryMonitor) Latest() *MemoryMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.latest == nil {
		return nil
	}
	cp := *m.latest
	return &cp
}

// Collect 立即采集一次并返回
func (m *MemoryMonitor) Collect() MemoryMetrics {
	return m.collect()
}

// History 从 Redis 获取时间范围内的历史指标
func (m *MemoryMonitor) History(ctx context.Context, rangeStr string) ([]MemoryMetrics, error) {
	dur := parseRange(rangeStr)
	now := time.Now()
	minScore := fmt.Sprintf("%d", now.Add(-dur).UnixMilli())
	maxScore := fmt.Sprintf("%d", now.UnixMilli())

	key := m.redisKey("memory:history")
	results, err := m.redis.ZRangeByScore(ctx, key, &redislib.ZRangeBy{
		Min: minScore,
		Max: maxScore,
	}).Result()
	if err != nil {
		return nil, err
	}

	metrics := make([]MemoryMetrics, 0, len(results))
	for _, raw := range results {
		var mm MemoryMetrics
		if err := json.Unmarshal([]byte(raw), &mm); err == nil {
			metrics = append(metrics, mm)
		}
	}
	return metrics, nil
}

// Alerts 返回最近的告警
func (m *MemoryMonitor) Alerts() []MemoryAlert {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cp := make([]MemoryAlert, len(m.alerts))
	copy(cp, m.alerts)
	return cp
}

func (m *MemoryMonitor) loop(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	// 启动时先采集一次
	m.collectAndStore(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.collectAndStore(ctx)
		}
	}
}

func (m *MemoryMonitor) collect() MemoryMetrics {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return MemoryMetrics{
		Timestamp:    time.Now().UTC(),
		HeapAlloc:    ms.HeapAlloc,
		HeapSys:      ms.HeapSys,
		HeapInuse:    ms.HeapInuse,
		HeapIdle:     ms.HeapIdle,
		HeapObjects:  ms.HeapObjects,
		StackInuse:   ms.StackInuse,
		GCSys:        ms.GCSys,
		TotalAlloc:   ms.TotalAlloc,
		Sys:          ms.Sys,
		NumGC:        ms.NumGC,
		LastGCNano:   ms.LastGC,
		PauseTotalNs: ms.PauseTotalNs,
		Goroutines:   runtime.NumGoroutine(),
	}
}

func (m *MemoryMonitor) collectAndStore(ctx context.Context) {
	metrics := m.collect()

	m.mu.Lock()
	m.latest = &metrics
	m.mu.Unlock()

	// 检查告警阈值
	m.checkAlerts(&metrics)

	// 存入 Redis 有序集合（score = 毫秒时间戳）
	data, err := json.Marshal(metrics)
	if err != nil {
		return
	}
	key := m.redisKey("memory:history")
	score := float64(metrics.Timestamp.UnixMilli())
	pipe := m.redis.Pipeline()
	pipe.ZAdd(ctx, key, redislib.Z{Score: score, Member: string(data)})
	// 清理过期数据
	cutoff := fmt.Sprintf("%d", metrics.Timestamp.Add(-m.historyRetain).UnixMilli())
	pipe.ZRemRangeByScore(ctx, key, "-inf", cutoff)
	if _, err := pipe.Exec(ctx); err != nil {
		m.log.Debug("内存监控：存储历史指标失败", zap.Error(err))
	}
}

func (m *MemoryMonitor) checkAlerts(metrics *MemoryMetrics) {
	// 堆使用率告警（基于 Sys）
	if metrics.Sys > 0 {
		ratio := float64(metrics.HeapAlloc) / float64(metrics.Sys)
		if ratio > 0.90 {
			m.addAlert("critical", "堆内存使用率超过 90%", ratio*100)
		} else if ratio > 0.75 {
			m.addAlert("warning", "堆内存使用率超过 75%", ratio*100)
		}
	}

	// Goroutine 数告警
	if metrics.Goroutines > 10000 {
		m.addAlert("critical", "Goroutine 数超过 10000", float64(metrics.Goroutines))
	} else if metrics.Goroutines > 5000 {
		m.addAlert("warning", "Goroutine 数超过 5000", float64(metrics.Goroutines))
	}

	// HeapObjects 告警
	if metrics.HeapObjects > 5_000_000 {
		m.addAlert("warning", "堆对象数超过 500 万", float64(metrics.HeapObjects))
	}
}

func (m *MemoryMonitor) addAlert(level, message string, value float64) {
	alert := MemoryAlert{
		Time:    time.Now().UTC(),
		Level:   level,
		Message: message,
		Value:   value,
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alerts = append(m.alerts, alert)
	// 保留最近 50 条
	if len(m.alerts) > 50 {
		m.alerts = m.alerts[len(m.alerts)-50:]
	}
	m.log.Warn("内存监控告警",
		zap.String("level", level),
		zap.String("message", message),
		zap.Float64("value", value),
	)
}

func (m *MemoryMonitor) redisKey(suffix string) string {
	return m.keyPrefix + ":" + suffix
}

func parseRange(rangeStr string) time.Duration {
	switch rangeStr {
	case "5m":
		return 5 * time.Minute
	case "15m":
		return 15 * time.Minute
	case "30m":
		return 30 * time.Minute
	case "hour", "1h":
		return time.Hour
	case "day", "24h":
		return 24 * time.Hour
	default:
		return time.Hour
	}
}
