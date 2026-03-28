package service

import (
	"context"
	"runtime"
	"runtime/debug"
	"time"

	"aegis/internal/config"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// MemorySnapshot 完整的内存管理快照
type MemorySnapshot struct {
	Timestamp  time.Time         `json:"timestamp"`
	GCTuner    *GCTunerSnapshot  `json:"gcTuner,omitempty"`
	Metrics    *MemoryMetrics    `json:"metrics,omitempty"`
	Pools      []PoolStats       `json:"pools"`
	Cache      CacheStats        `json:"cache"`
	LeakReport *LeakReport       `json:"leakReport,omitempty"`
	Alerts     []MemoryAlert     `json:"alerts"`
	Config     MemoryConfigView  `json:"config"`
}

// MemoryConfigView 配置视图（不含敏感信息）
type MemoryConfigView struct {
	GCAutoTune      bool   `json:"gcAutoTune"`
	MemoryLimitMB   int64  `json:"memoryLimitMB"`
	MonitorInterval string `json:"monitorInterval"`
	GCTuneInterval  string `json:"gcTuneInterval"`
	LeakDetection   bool   `json:"leakDetection"`
	LeakWindow      int    `json:"leakWindow"`
	CacheMaxEntries int    `json:"cacheMaxEntries"`
	CacheTTL        string `json:"cacheTTL"`
}

// MemoryManager 统一内存管理器，组合所有子模块
type MemoryManager struct {
	log      *zap.Logger
	cfg      config.MemoryConfig
	tuner    *GCTuner
	monitor  *MemoryMonitor
	pools    *MemoryPools
	cache    *MemoryCache
	detector *LeakDetector
	cancel   context.CancelFunc
}

func NewMemoryManager(cfg config.MemoryConfig, log *zap.Logger, redis *redislib.Client, keyPrefix string) *MemoryManager {
	mm := &MemoryManager{
		log: log,
		cfg: cfg,
	}

	// 始终创建对象池（零开销）
	mm.pools = NewMemoryPools()

	// 始终创建缓存
	mm.cache = NewMemoryCache(cfg.CacheMaxEntries, cfg.CacheTTL)

	// GC 调优器（可选）
	if cfg.GCAutoTune {
		mm.tuner = NewGCTuner(log, cfg.GCTuneInterval, cfg.MemoryLimitMB)
	}

	// 内存监控
	mm.monitor = NewMemoryMonitor(log, redis, keyPrefix, cfg.MonitorInterval, cfg.HistoryRetain)

	// 泄漏检测（可选）
	if cfg.LeakDetection {
		mm.detector = NewLeakDetector(cfg.LeakWindow)
	}

	return mm
}

// Start 启动所有后台服务
func (m *MemoryManager) Start(ctx context.Context) {
	ctx, m.cancel = context.WithCancel(ctx)

	if m.tuner != nil {
		m.tuner.Start(ctx)
		m.log.Info("内存管理：GC 自适应调优器已启动",
			zap.Duration("interval", m.cfg.GCTuneInterval),
		)
	}

	m.monitor.Start(ctx)
	m.log.Info("内存管理：监控采集器已启动",
		zap.Duration("interval", m.cfg.MonitorInterval),
	)

	// 如果启用泄漏检测，注册一个与监控同步的采样回调
	if m.detector != nil {
		go m.leakSamplingLoop(ctx)
		m.log.Info("内存管理：泄漏检测已启动",
			zap.Int("windowSize", m.cfg.LeakWindow),
		)
	}

	m.log.Info("内存管理系统已完全启动")
}

// Stop 停止所有后台服务
func (m *MemoryManager) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	if m.tuner != nil {
		m.tuner.Stop()
	}
	m.monitor.Stop()
	m.log.Info("内存管理系统已停止")
}

// Snapshot 获取完整快照
func (m *MemoryManager) Snapshot() MemorySnapshot {
	snap := MemorySnapshot{
		Timestamp: time.Now().UTC(),
		Pools:     m.pools.AllStats(),
		Cache:     m.cache.Stats(),
		Alerts:    m.monitor.Alerts(),
		Config:    m.configView(),
	}

	if m.tuner != nil {
		ts := m.tuner.Snapshot()
		snap.GCTuner = &ts
	}

	if latest := m.monitor.Latest(); latest != nil {
		snap.Metrics = latest
	}

	if m.detector != nil {
		report := m.detector.Report()
		snap.LeakReport = &report
	}

	return snap
}

// ForceGC 触发一次强制 GC
func (m *MemoryManager) ForceGC() {
	runtime.GC()
	debug.FreeOSMemory()
	m.log.Info("内存管理：已执行强制 GC + FreeOSMemory")
}

// SetGOGC 手动设置 GOGC 值
func (m *MemoryManager) SetGOGC(value int) int {
	if m.tuner != nil {
		return m.tuner.SetGOGC(value)
	}
	old := debug.SetGCPercent(value)
	m.log.Info("内存管理：手动设置 GOGC（无调优器模式）",
		zap.Int("old", old),
		zap.Int("new", value),
	)
	return old
}

// FlushCaches 清空本地缓存
func (m *MemoryManager) FlushCaches() {
	m.cache.Flush()
	m.log.Info("内存管理：已清空本地缓存")
}

// GetLeakReport 获取泄漏检测报告
func (m *MemoryManager) GetLeakReport() *LeakReport {
	if m.detector == nil {
		return nil
	}
	report := m.detector.Report()
	return &report
}

// History 获取内存历史指标
func (m *MemoryManager) History(ctx context.Context, rangeStr string) ([]MemoryMetrics, error) {
	return m.monitor.History(ctx, rangeStr)
}

// Pools 返回对象池引用（供其他服务使用）
func (m *MemoryManager) Pools() *MemoryPools {
	return m.pools
}

// Cache 返回缓存引用（供其他服务使用）
func (m *MemoryManager) Cache() *MemoryCache {
	return m.cache
}

func (m *MemoryManager) configView() MemoryConfigView {
	return MemoryConfigView{
		GCAutoTune:      m.cfg.GCAutoTune,
		MemoryLimitMB:   m.cfg.MemoryLimitMB,
		MonitorInterval: m.cfg.MonitorInterval.String(),
		GCTuneInterval:  m.cfg.GCTuneInterval.String(),
		LeakDetection:   m.cfg.LeakDetection,
		LeakWindow:      m.cfg.LeakWindow,
		CacheMaxEntries: m.cfg.CacheMaxEntries,
		CacheTTL:        m.cfg.CacheTTL.String(),
	}
}

// leakSamplingLoop 定期从监控指标中提取样本给泄漏检测器
func (m *MemoryManager) leakSamplingLoop(ctx context.Context) {
	// 泄漏采样间隔 = 监控间隔的 2 倍（避免过于频繁）
	interval := m.cfg.MonitorInterval * 2
	if interval < 30*time.Second {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if latest := m.monitor.Latest(); latest != nil {
				m.detector.RecordSample(
					float64(latest.HeapAlloc),
					float64(latest.HeapObjects),
					float64(latest.Sys),
					latest.Goroutines,
				)
			}
		}
	}
}
