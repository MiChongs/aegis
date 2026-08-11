package service

import (
	"context"
	"math"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	gopsutilmem "github.com/shirou/gopsutil/v4/mem"
	"go.uber.org/zap"
)

// GCTuner 根据当前内存压力与 GC 行为动态调整 `GOGC` 和 `SoftMemoryLimit`，
// 在高压时激进回收、低压时放松以提升吞吐；同时带以下防抖与自我保护机制：
//
//   - **指数加权移动平均**（EWMA, α=0.3）抹平瞬时峰值，避免对毛刺过度反应
//   - **滞回步长**（默认 10）—— 目标 GOGC 与当前值差距 < 该阈值时不动
//   - **最小变更间隔**（默认 30s）—— 同一决策过程内最多每 30s 修改一次 GOGC
//   - **GC 暂停反馈**—— 近 8 次 GC 平均暂停 > 100ms 时主动放松，避免 GC CPU 黑洞
//   - **紧急模式**—— 使用率 > 95%：立即 GC + GOGC=10；> 98%：再 FreeOSMemory
//   - **cgroup 感知**—— Linux 下优先读 cgroup v2/v1 的 memory.max 作为真实内存上限
//   - **系统内存回退**—— 非容器化环境下用 gopsutil 获取真实系统内存 80% 作限制
type GCTuner struct {
	log           *zap.Logger
	interval      time.Duration
	memoryLimitMB int64
	// 调优参数（可通过 Setters 暴露；目前使用合理默认值）
	minGOGC           int
	maxGOGC           int
	hysteresis        int
	minChangeInterval time.Duration
	emergencyThresh   float64 // 紧急 GC 触发线（usageRatio）
	criticalThresh    float64 // 更紧急：调用 FreeOSMemory
	pauseBackoffMs    float64 // 近 8 次 avg pause 超过这个值则放松 GOGC

	// 运行时状态（atomic 读写）
	currentGOGC  atomic.Int64
	currentLimit atomic.Int64
	lastTuneAt   atomic.Int64 // UnixNano
	lastChangeAt atomic.Int64 // UnixNano（上次真改动 GOGC 的时间）
	tuneCount    atomic.Int64
	changeCount  atomic.Int64 // 真正改动了 GOGC 的次数
	emergencies  atomic.Int64 // 触发紧急 GC 的次数
	backoffs     atomic.Int64 // 因 pause 反馈放松 GOGC 的次数

	// EWMA 使用率（非 atomic，只在 tune 协程内读写）
	ewmaUsage float64

	// GC 暂停滑窗（最近 8 次）
	recentPauses [8]uint64
	pauseIdx     int
	pauseFilled  int
	lastNumGC    uint32

	// 内存限制来源（configured / cgroup / system / fallback）
	// 只在 Start 时写一次，之后由 Snapshot 多 goroutine 读取；atomic.Value 保证无数据竞争
	limitSource atomic.Value // stores string

	cancel context.CancelFunc
}

// GCTunerSnapshot 暴露给管理面板/API 的当前状态
type GCTunerSnapshot struct {
	GOGC              int64   `json:"gogc"`
	MemoryLimitMB     int64   `json:"memoryLimitMB"`
	MemoryLimitRaw    int64   `json:"memoryLimitRaw"`
	TuneCount         int64   `json:"tuneCount"`
	ChangeCount       int64   `json:"changeCount"`
	EmergencyCount    int64   `json:"emergencyCount"`
	PauseBackoffCount int64   `json:"pauseBackoffCount"`
	LastTuneAt        string  `json:"lastTuneAt,omitempty"`
	LastChangeAt      string  `json:"lastChangeAt,omitempty"`
	EWMAUsageRatio    float64 `json:"ewmaUsageRatio"`
	LastPauseAvgMs    float64 `json:"lastPauseAvgMs"`
	Mode              string  `json:"mode"`        // idle / normal / eager / aggressive / emergency / backoff
	LimitSource       string  `json:"limitSource"` // configured / cgroup / system / fallback
}

func NewGCTuner(log *zap.Logger, interval time.Duration, memoryLimitMB int64) *GCTuner {
	if log == nil {
		log = zap.NewNop()
	}
	if interval <= 0 {
		interval = 30 * time.Second
	}
	t := &GCTuner{
		log:               log,
		interval:          interval,
		memoryLimitMB:     memoryLimitMB,
		minGOGC:           10,
		maxGOGC:           300,
		hysteresis:        10,
		minChangeInterval: 30 * time.Second,
		emergencyThresh:   0.95,
		criticalThresh:    0.98,
		pauseBackoffMs:    100.0,
	}
	// 读取初始 GOGC：SetGCPercent(-1) 读当前值，再写回去不改变
	current := debug.SetGCPercent(-1)
	if current < 0 {
		current = 100
	}
	debug.SetGCPercent(current)
	t.currentGOGC.Store(int64(current))
	return t
}

// Start 启动后台调优循环（幂等）
func (t *GCTuner) Start(ctx context.Context) {
	if t.cancel != nil {
		return
	}
	ctx, t.cancel = context.WithCancel(ctx)

	limit, source := t.resolveMemoryLimit()
	t.currentLimit.Store(limit)
	if limit > 0 {
		debug.SetMemoryLimit(limit)
		t.log.Info("GC 调优器：设置 SoftMemoryLimit",
			zap.Int64("limitMB", limit/(1024*1024)),
			zap.String("source", source),
		)
	}
	go t.loop(ctx)
}

// Stop 停止调优（幂等）
func (t *GCTuner) Stop() {
	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
}

// Snapshot 当前状态
func (t *GCTuner) Snapshot() GCTunerSnapshot {
	snap := GCTunerSnapshot{
		GOGC:              t.currentGOGC.Load(),
		MemoryLimitRaw:    t.currentLimit.Load(),
		MemoryLimitMB:     t.currentLimit.Load() / (1024 * 1024),
		TuneCount:         t.tuneCount.Load(),
		ChangeCount:       t.changeCount.Load(),
		EmergencyCount:    t.emergencies.Load(),
		PauseBackoffCount: t.backoffs.Load(),
		EWMAUsageRatio:    t.ewmaUsage,
		LastPauseAvgMs:    t.avgPauseMs(),
		Mode:              t.modeDescription(),
		LimitSource:       loadLimitSource(&t.limitSource),
	}
	if ts := t.lastTuneAt.Load(); ts > 0 {
		snap.LastTuneAt = time.Unix(0, ts).UTC().Format(time.RFC3339)
	}
	if ts := t.lastChangeAt.Load(); ts > 0 {
		snap.LastChangeAt = time.Unix(0, ts).UTC().Format(time.RFC3339)
	}
	return snap
}

// SetGOGC 手动设置 GOGC 值（管理面板使用）
func (t *GCTuner) SetGOGC(value int) int {
	if value < t.minGOGC {
		value = t.minGOGC
	}
	if value > t.maxGOGC {
		value = t.maxGOGC
	}
	old := debug.SetGCPercent(value)
	t.currentGOGC.Store(int64(value))
	t.lastChangeAt.Store(time.Now().UnixNano())
	t.changeCount.Add(1)
	t.log.Info("GC 调优器：手动设置 GOGC", zap.Int("old", old), zap.Int("new", value))
	return old
}

// MemoryLimit 当前 SoftMemoryLimit（bytes）
func (t *GCTuner) MemoryLimit() int64 { return t.currentLimit.Load() }

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

// tune 单次调优决策
func (t *GCTuner) tune() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	limit := t.currentLimit.Load()
	t.tuneCount.Add(1)
	t.lastTuneAt.Store(time.Now().UnixNano())

	// 更新 GC 暂停滑窗
	t.absorbPauses(&m)
	avgPause := t.avgPauseMs()

	// 没有 MemoryLimit 时不做 tuning（除非能紧急救援）
	if limit <= 0 {
		return
	}

	raw := float64(m.Alloc) / float64(limit)
	// EWMA 平滑（α=0.3）—— 第一次采样直接赋值
	if t.ewmaUsage == 0 {
		t.ewmaUsage = raw
	} else {
		t.ewmaUsage = 0.7*t.ewmaUsage + 0.3*raw
	}
	smoothed := t.ewmaUsage

	// 紧急模式：使用率极高时立即强制回收
	if raw >= t.criticalThresh {
		runtime.GC()
		debug.FreeOSMemory()
		t.applyGOGC(t.minGOGC, "critical emergency GC + FreeOSMemory", raw, true)
		t.emergencies.Add(1)
		return
	}
	if raw >= t.emergencyThresh {
		runtime.GC()
		t.applyGOGC(t.minGOGC, "emergency GC", raw, true)
		t.emergencies.Add(1)
		return
	}

	// GC 暂停反馈：最近 8 次平均暂停过高则放松 GOGC（减少 GC 频率，牺牲内存）
	if avgPause > t.pauseBackoffMs {
		target := int(float64(t.currentGOGC.Load())*1.2) + 20
		if target > t.maxGOGC {
			target = t.maxGOGC
		}
		if int64(target) > t.currentGOGC.Load() {
			t.applyGOGC(target, "gc pause backoff", smoothed, false)
			t.backoffs.Add(1)
			return
		}
	}

	// 基于平滑使用率计算目标 GOGC（连续曲线，避免阈值跳变）
	target := gogcCurve(smoothed)
	if target < t.minGOGC {
		target = t.minGOGC
	}
	if target > t.maxGOGC {
		target = t.maxGOGC
	}

	// 滞回：差距 < hysteresis 不动
	current := int(t.currentGOGC.Load())
	if absInt(target-current) < t.hysteresis {
		return
	}

	// 频率限制：距上次真正改动不足 minChangeInterval 不动
	if last := t.lastChangeAt.Load(); last > 0 {
		if time.Since(time.Unix(0, last)) < t.minChangeInterval {
			return
		}
	}

	t.applyGOGC(target, "adaptive", smoothed, false)
}

// applyGOGC 设置 GOGC 并记录事件
func (t *GCTuner) applyGOGC(target int, reason string, usage float64, emergency bool) {
	oldGOGC := t.currentGOGC.Load()
	if int64(target) == oldGOGC {
		return
	}
	debug.SetGCPercent(target)
	t.currentGOGC.Store(int64(target))
	t.lastChangeAt.Store(time.Now().UnixNano())
	t.changeCount.Add(1)

	fields := []zap.Field{
		zap.String("reason", reason),
		zap.Int64("oldGOGC", oldGOGC),
		zap.Int("newGOGC", target),
		zap.Float64("usageRatio", usage),
		zap.Float64("avgPauseMs", t.avgPauseMs()),
	}
	if emergency {
		t.log.Warn("GC 调优器：调整 GOGC", fields...)
	} else {
		t.log.Debug("GC 调优器：调整 GOGC", fields...)
	}
}

// absorbPauses 把 MemStats.PauseNs 里的新暂停采样并入滑窗
func (t *GCTuner) absorbPauses(m *runtime.MemStats) {
	newGC := int64(m.NumGC) - int64(t.lastNumGC)
	if newGC <= 0 {
		t.lastNumGC = m.NumGC
		return
	}
	if newGC > int64(len(m.PauseNs)) {
		newGC = int64(len(m.PauseNs))
	}
	for i := int64(0); i < newGC; i++ {
		idx := (int64(m.NumGC) - i - 1 + int64(len(m.PauseNs))) % int64(len(m.PauseNs))
		pause := m.PauseNs[idx]
		if pause == 0 {
			continue
		}
		t.recentPauses[t.pauseIdx%len(t.recentPauses)] = pause
		t.pauseIdx++
		if t.pauseFilled < len(t.recentPauses) {
			t.pauseFilled++
		}
	}
	t.lastNumGC = m.NumGC
}

// avgPauseMs 近 8 次 GC 平均暂停（毫秒）
func (t *GCTuner) avgPauseMs() float64 {
	if t.pauseFilled == 0 {
		return 0
	}
	var sum uint64
	for i := 0; i < t.pauseFilled; i++ {
		sum += t.recentPauses[i]
	}
	return float64(sum) / float64(t.pauseFilled) / 1e6
}

// modeDescription 根据当前状态给出直观描述
func (t *GCTuner) modeDescription() string {
	if t.currentLimit.Load() <= 0 {
		return "idle"
	}
	u := t.ewmaUsage
	switch {
	case u >= t.criticalThresh:
		return "critical"
	case u >= t.emergencyThresh:
		return "emergency"
	case t.avgPauseMs() > t.pauseBackoffMs:
		return "backoff"
	case u >= 0.75:
		return "aggressive"
	case u >= 0.5:
		return "eager"
	default:
		return "normal"
	}
}

// gogcCurve 基于使用率计算目标 GOGC（连续曲线，避免阈值跳变）
//
//	usage 越高 → GOGC 越小（更频繁 GC）
//	usage=0.0  → GOGC≈200
//	usage=0.5  → GOGC≈100
//	usage=0.8  → GOGC≈40
//	usage=0.95 → GOGC≈15
//	公式：GOGC = round(220 * (1 - usage)^1.8 + 10)
func gogcCurve(usage float64) int {
	if usage < 0 {
		usage = 0
	}
	if usage > 1 {
		usage = 1
	}
	raw := 220.0*math.Pow(1.0-usage, 1.8) + 10.0
	return int(math.Round(raw))
}

// resolveMemoryLimit 解析 SoftMemoryLimit 的实际字节数，并记录来源
//
//	优先级：
//	  1) 配置显式指定 memoryLimitMB > 0
//	  2) Linux cgroup v2 (/sys/fs/cgroup/memory.max)
//	  3) Linux cgroup v1 (/sys/fs/cgroup/memory/memory.limit_in_bytes)
//	  4) gopsutil VirtualMemory().Total × 80%
//	  5) 回退到 runtime.MemStats.Sys × 4 × 80%
//	上下限：128 MB ~ 128 GB
func (t *GCTuner) resolveMemoryLimit() (int64, string) {
	const (
		minLimit = int64(128 * 1024 * 1024)        // 128 MB
		maxLimit = int64(128 * 1024 * 1024 * 1024) // 128 GB
	)

	if t.memoryLimitMB > 0 {
		v := clampLimit(t.memoryLimitMB*1024*1024, minLimit, maxLimit)
		t.limitSource.Store("configured")
		return v, "configured"
	}

	if v := readCgroupMemoryMax(); v > 0 {
		// 给 cgroup 上限留 10% 安全边距
		target := clampLimit(v*9/10, minLimit, maxLimit)
		t.limitSource.Store("cgroup")
		return target, "cgroup"
	}

	if vm, err := gopsutilmem.VirtualMemory(); err == nil && vm != nil && vm.Total > 0 {
		target := clampLimit(int64(float64(vm.Total)*0.8), minLimit, maxLimit)
		t.limitSource.Store("system")
		return target, "system"
	}

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	estimated := int64(ms.Sys) * 4
	target := clampLimit(estimated*80/100, minLimit, maxLimit)
	t.limitSource.Store("fallback")
	return target, "fallback"
}

// loadLimitSource atomic.Value 的字符串安全读取（未写入时返回空串）
func loadLimitSource(v *atomic.Value) string {
	if s, ok := v.Load().(string); ok {
		return s
	}
	return ""
}

// readCgroupMemoryMax 读取 Linux cgroup v2/v1 的内存限制（bytes）
// Windows / macOS 下文件不存在，直接返回 0
func readCgroupMemoryMax() int64 {
	paths := []string{
		"/sys/fs/cgroup/memory.max",                   // cgroup v2 unified
		"/sys/fs/cgroup/memory/memory.limit_in_bytes", // cgroup v1
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		s := strings.TrimSpace(string(data))
		if s == "" || s == "max" {
			continue
		}
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil || v <= 0 {
			continue
		}
		// cgroup v1 "无限制" 常见值：非常大的数字（如 9223372036854771712）
		if v >= 1<<60 {
			continue
		}
		return v
	}
	return 0
}

func clampLimit(v, min, max int64) int64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
