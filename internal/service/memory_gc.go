package service

import (
	"context"
	"math"
	"runtime"
	"runtime/debug"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// GCTuner 根据当前内存使用率动态调整 GOGC 和 MemoryLimit，
// 在高负载时激进回收，低负载时放松以提高吞吐。
type GCTuner struct {
	log            *zap.Logger
	interval       time.Duration
	memoryLimitMB  int64   // 软内存上限（MB），0 = 自动检测
	currentGOGC    atomic.Int64
	currentLimit   atomic.Int64 // 当前 MemoryLimit（bytes）
	lastTuneAt     atomic.Int64 // UnixNano
	tuneCount      atomic.Int64
	cancel         context.CancelFunc
}

// GCTunerSnapshot 表示 GC 调优器的快照状态
type GCTunerSnapshot struct {
	GOGC           int64  `json:"gogc"`
	MemoryLimitMB  int64  `json:"memoryLimitMB"`
	MemoryLimitRaw int64  `json:"memoryLimitRaw"`
	TuneCount      int64  `json:"tuneCount"`
	LastTuneAt     string `json:"lastTuneAt,omitempty"`
}

func NewGCTuner(log *zap.Logger, interval time.Duration, memoryLimitMB int64) *GCTuner {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	t := &GCTuner{
		log:           log,
		interval:      interval,
		memoryLimitMB: memoryLimitMB,
	}
	// 记录初始 GOGC
	current := debug.SetGCPercent(-1)
	debug.SetGCPercent(current)
	if current < 0 {
		current = 100 // Go 默认值
	}
	t.currentGOGC.Store(int64(current))
	return t
}

// Start 启动后台调优循环
func (t *GCTuner) Start(ctx context.Context) {
	ctx, t.cancel = context.WithCancel(ctx)
	// 计算实际内存上限
	limit := t.resolveMemoryLimit()
	t.currentLimit.Store(limit)
	if limit > 0 {
		debug.SetMemoryLimit(limit)
		t.log.Info("GC 调优器：设置 MemoryLimit",
			zap.Int64("limitMB", limit/(1024*1024)),
		)
	}
	go t.loop(ctx)
}

// Stop 停止调优循环
func (t *GCTuner) Stop() {
	if t.cancel != nil {
		t.cancel()
	}
}

// Snapshot 返回当前调优状态
func (t *GCTuner) Snapshot() GCTunerSnapshot {
	snap := GCTunerSnapshot{
		GOGC:           t.currentGOGC.Load(),
		MemoryLimitMB:  t.currentLimit.Load() / (1024 * 1024),
		MemoryLimitRaw: t.currentLimit.Load(),
		TuneCount:      t.tuneCount.Load(),
	}
	if ts := t.lastTuneAt.Load(); ts > 0 {
		snap.LastTuneAt = time.Unix(0, ts).UTC().Format(time.RFC3339)
	}
	return snap
}

// SetGOGC 手动设置 GOGC 值（供 API 调用）
func (t *GCTuner) SetGOGC(value int) int {
	old := debug.SetGCPercent(value)
	t.currentGOGC.Store(int64(value))
	t.log.Info("GC 调优器：手动设置 GOGC",
		zap.Int("old", old),
		zap.Int("new", value),
	)
	return old
}

func (t *GCTuner) loop(ctx context.Context) {
	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.tune()
		}
	}
}

func (t *GCTuner) tune() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	limit := t.currentLimit.Load()
	if limit <= 0 {
		return
	}

	// 计算堆使用率（相对于软限制）
	usageRatio := float64(m.Alloc) / float64(limit)

	// 根据使用率动态调整 GOGC：
	// - 使用率 > 80%：激进回收（GOGC = 20~50）
	// - 使用率 60-80%：适中（GOGC = 50~80）
	// - 使用率 40-60%：标准（GOGC = 80~120）
	// - 使用率 < 40%：宽松（GOGC = 120~200）
	var targetGOGC int
	switch {
	case usageRatio > 0.85:
		targetGOGC = 20
	case usageRatio > 0.75:
		targetGOGC = int(math.Round(lerp(50, 20, (usageRatio-0.75)/0.10)))
	case usageRatio > 0.60:
		targetGOGC = int(math.Round(lerp(80, 50, (usageRatio-0.60)/0.15)))
	case usageRatio > 0.40:
		targetGOGC = int(math.Round(lerp(120, 80, (usageRatio-0.40)/0.20)))
	default:
		targetGOGC = int(math.Round(lerp(200, 120, math.Max(0, usageRatio)/0.40)))
	}

	// 边界限制
	if targetGOGC < 10 {
		targetGOGC = 10
	}
	if targetGOGC > 300 {
		targetGOGC = 300
	}

	oldGOGC := t.currentGOGC.Load()
	if int64(targetGOGC) != oldGOGC {
		debug.SetGCPercent(targetGOGC)
		t.currentGOGC.Store(int64(targetGOGC))
		t.log.Debug("GC 调优器：调整 GOGC",
			zap.Int64("old", oldGOGC),
			zap.Int("new", targetGOGC),
			zap.Float64("usageRatio", usageRatio),
			zap.Uint64("heapAllocMB", m.Alloc/(1024*1024)),
		)
	}

	t.tuneCount.Add(1)
	t.lastTuneAt.Store(time.Now().UnixNano())
}

// resolveMemoryLimit 计算内存软上限（bytes）
func (t *GCTuner) resolveMemoryLimit() int64 {
	if t.memoryLimitMB > 0 {
		return t.memoryLimitMB * 1024 * 1024
	}
	// 自动检测：取系统总内存的 80%（通过 MemStats.Sys 估算进程可用范围）
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	// 使用 Sys 的 4 倍作为估算上限（Sys 通常远小于实际可用内存）
	estimated := int64(m.Sys) * 4
	limit := estimated * 80 / 100
	// 下限 128MB，上限 32GB
	if limit < 128*1024*1024 {
		limit = 128 * 1024 * 1024
	}
	if limit > 32*1024*1024*1024 {
		limit = 32 * 1024 * 1024 * 1024
	}
	return limit
}

// lerp 线性插值 from a -> b，t 在 [0, 1]
func lerp(a, b, t float64) float64 {
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return a + (b-a)*t
}
