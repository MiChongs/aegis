package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"runtime"
	"runtime/debug"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	redislib "github.com/redis/go-redis/v9"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/process"
	"go.uber.org/zap"
)

// MemoryMetrics 单次采集的内存指标。
//
// 字段分组：
//  1. Go 运行时基础指标（HeapAlloc/Sys/...）
//  2. GC 派生指标（暂停时间 avg/p95/p99/max、GC CPU 占比、GC 速率）
//  3. 采样间速率（分配速率 MB/s、GC/分钟）—— 需要至少两次相邻采样才有值
//  4. 进程级指标（进程 RSS / CPU / 线程数）—— 使用 gopsutil 采集
//  5. 系统级指标（物理总/已用/可用/使用率）—— 使用 gopsutil 采集
//  6. 内存限制上下文（MemoryLimit、UsageRatio、GOGC）—— 与 GCTuner 协同
type MemoryMetrics struct {
	Timestamp time.Time `json:"timestamp"`

	// ── Go runtime 基础 ──
	HeapAlloc    uint64 `json:"heapAlloc"`
	HeapSys      uint64 `json:"heapSys"`
	HeapInuse    uint64 `json:"heapInuse"`
	HeapIdle     uint64 `json:"heapIdle"`
	HeapReleased uint64 `json:"heapReleased"`
	HeapObjects  uint64 `json:"heapObjects"`
	StackInuse   uint64 `json:"stackInuse"`
	StackSys     uint64 `json:"stackSys"`
	GCSys        uint64 `json:"gcSys"`
	OtherSys     uint64 `json:"otherSys"`
	TotalAlloc   uint64 `json:"totalAlloc"`
	Mallocs      uint64 `json:"mallocs"`
	Frees        uint64 `json:"frees"`
	Sys          uint64 `json:"sys"`
	NumGC        uint32 `json:"numGC"`
	LastGCNano   uint64 `json:"lastGCNano"`
	PauseTotalNs uint64 `json:"pauseTotalNs"`
	Goroutines   int    `json:"goroutines"`

	// ── GC 派生指标 ──
	GCPauseAvgMs  float64 `json:"gcPauseAvgMs"`
	GCPauseMaxMs  float64 `json:"gcPauseMaxMs"`
	GCPauseP95Ms  float64 `json:"gcPauseP95Ms"`
	GCPauseP99Ms  float64 `json:"gcPauseP99Ms"`
	GCCPUFraction float64 `json:"gcCPUFraction"`

	// ── 速率（相邻采样差值）──
	AllocRateMBps float64 `json:"allocRateMBps,omitempty"`
	GCPerMinute   float64 `json:"gcPerMinute,omitempty"`

	// ── 进程 / 系统（gopsutil）──
	ProcessRSS     uint64  `json:"processRSS,omitempty"`
	ProcessCPU     float64 `json:"processCPU,omitempty"`
	ProcessThreads int32   `json:"processThreads,omitempty"`
	SysTotal       uint64  `json:"sysTotal,omitempty"`
	SysUsed        uint64  `json:"sysUsed,omitempty"`
	SysAvailable   uint64  `json:"sysAvailable,omitempty"`
	SysUsedPercent float64 `json:"sysUsedPercent,omitempty"`

	// ── 限制上下文 ──
	MemoryLimit int64   `json:"memoryLimit,omitempty"` // 软内存上限 bytes（来自 GCTuner）
	UsageRatio  float64 `json:"usageRatio,omitempty"`  // HeapAlloc / MemoryLimit（若有）
	GOGC        int     `json:"gogc,omitempty"`
}

// MemoryAlert 内存告警记录。
// 向后兼容：保留原有 time/level/message/value 四字段；新增 key/category/state/threshold/durationMs。
type MemoryAlert struct {
	Time       time.Time `json:"time"`
	Level      string    `json:"level"`
	Message    string    `json:"message"`
	Value      float64   `json:"value"`
	Key        string    `json:"key,omitempty"`        // 稳定标识，用于去重 / 关联恢复事件
	Category   string    `json:"category,omitempty"`   // memory / runtime / gc / system
	State      string    `json:"state,omitempty"`      // firing / resolved
	Threshold  float64   `json:"threshold,omitempty"`  // 触发阈值
	DurationMs int64     `json:"durationMs,omitempty"` // 该告警本次 firing 累计时长
}

// AlertRule 带滞回的告警规则。
//
//	Trigger  —— 越过则进入 firing 候选
//	Clear    —— 低于则恢复（必须 < Trigger，避免在阈值附近抖动）
//	FireDelay —— 需连续越过阈值该时长才真正 firing（防止采样毛刺）
//	LogCooldown —— 同一条 firing 状态的日志最小间隔（默认 5 分钟）
type AlertRule struct {
	Key         string
	Category    string
	Level       string
	Message     string
	Trigger     float64
	Clear       float64
	FireDelay   time.Duration
	LogCooldown time.Duration
}

type alertState struct {
	firing    bool
	firstOver time.Time // 首次越过阈值
	firstFire time.Time // 真正 firing 开始
	lastLogAt time.Time
	lastValue float64
}

const (
	memoryPausesBufferSize = 256
	memoryAlertsCapacity   = 200
)

// MemoryMonitor 采集 Go 运行时 + 进程 + 系统级内存指标，带滞回告警。
//
// 工作方式：
//  1. `Start(ctx)` 启动后台定时采集 goroutine；
//  2. 每个 tick：runtime.ReadMemStats → gopsutil → 计算速率/GC 分布 → 写缓存 + Redis 历史；
//  3. 规则驱动的告警状态机：trigger/clear 双阈值形成滞回，firstOver→firstFire 防毛刺，
//     日志按 LogCooldown 节流；firing 结束时自动生成 "resolved" 恢复记录。
type MemoryMonitor struct {
	log           *zap.Logger
	redis         *redislib.Client
	keyPrefix     string
	interval      time.Duration
	historyRetain time.Duration

	mu     sync.RWMutex
	latest *MemoryMetrics
	alerts []MemoryAlert

	// GC 暂停滑窗（近 256 次）
	pausesBuf []uint64

	// 上一轮累计值，用于速率计算
	lastTotalAlloc uint64
	lastNumGC      uint32
	lastSampleAt   time.Time

	// 规则 & 状态机
	rules  []AlertRule
	states map[string]*alertState

	// 从 GCTuner 取 MemoryLimit 和 GOGC（可选注入）
	tuner *GCTuner

	// gopsutil 进程句柄：懒初始化；失败只打一次日志
	procOnce sync.Once
	proc     *process.Process

	cancel  context.CancelFunc
	started atomic.Bool
}

// NewMemoryMonitor 构造监控器（未启动）
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
		alerts:        make([]MemoryAlert, 0, memoryAlertsCapacity),
		rules:         defaultMemoryAlertRules(),
		states:        make(map[string]*alertState, 8),
	}
}

// SetTuner 注入 GCTuner，用于在快照中附带 MemoryLimit / GOGC 上下文
// 供 MemoryManager 在 Start 前调用一次，线程安全（监控循环只读 tuner 的 atomic 字段）
func (m *MemoryMonitor) SetTuner(t *GCTuner) { m.tuner = t }

// Start 启动后台采集循环（幂等）
func (m *MemoryMonitor) Start(ctx context.Context) {
	if !m.started.CompareAndSwap(false, true) {
		return
	}
	ctx, m.cancel = context.WithCancel(ctx)
	go m.loop(ctx)
}

// Stop 停止采集（幂等）
func (m *MemoryMonitor) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
}

// Latest 返回最近一次采集快照
func (m *MemoryMonitor) Latest() *MemoryMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.latest == nil {
		return nil
	}
	cp := *m.latest
	return &cp
}

// Collect 立即采集一次（不入库、不触发告警）
func (m *MemoryMonitor) Collect() MemoryMetrics { return m.collect(context.Background()) }

// Alerts 返回最近告警副本（含 firing + resolved）
func (m *MemoryMonitor) Alerts() []MemoryAlert {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cp := make([]MemoryAlert, len(m.alerts))
	copy(cp, m.alerts)
	return cp
}

// History 读取 Redis 历史指标（与旧版本签名一致）
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

func (m *MemoryMonitor) loop(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
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

// collect 采集一次快照（不写 Redis、不触发告警）
//
// 关键：`runtime.MemStats.PauseNs` 是 256 长的环形缓冲，本函数把它合并进
// 监控侧的滑窗（去重：只追加上一次没看过的暂停点），以便算 avg/p95/p99/max。
func (m *MemoryMonitor) collect(ctx context.Context) MemoryMetrics {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	now := time.Now()
	metric := MemoryMetrics{
		Timestamp:     now.UTC(),
		HeapAlloc:     ms.HeapAlloc,
		HeapSys:       ms.HeapSys,
		HeapInuse:     ms.HeapInuse,
		HeapIdle:      ms.HeapIdle,
		HeapReleased:  ms.HeapReleased,
		HeapObjects:   ms.HeapObjects,
		StackInuse:    ms.StackInuse,
		StackSys:      ms.StackSys,
		GCSys:         ms.GCSys,
		OtherSys:      ms.OtherSys,
		TotalAlloc:    ms.TotalAlloc,
		Mallocs:       ms.Mallocs,
		Frees:         ms.Frees,
		Sys:           ms.Sys,
		NumGC:         ms.NumGC,
		LastGCNano:    ms.LastGC,
		PauseTotalNs:  ms.PauseTotalNs,
		Goroutines:    runtime.NumGoroutine(),
		GCCPUFraction: ms.GCCPUFraction,
	}

	// ── GC 暂停分布 ──
	m.mu.Lock()
	m.absorbPauses(&ms)
	pauses := append([]uint64(nil), m.pausesBuf...)
	// 相邻采样间速率
	dtSec := 0.0
	if !m.lastSampleAt.IsZero() {
		dtSec = now.Sub(m.lastSampleAt).Seconds()
	}
	if dtSec > 0 && m.lastTotalAlloc > 0 && ms.TotalAlloc >= m.lastTotalAlloc {
		bytesDiff := ms.TotalAlloc - m.lastTotalAlloc
		metric.AllocRateMBps = float64(bytesDiff) / (1024 * 1024) / dtSec
	}
	if dtSec > 0 && ms.NumGC >= m.lastNumGC {
		gcDiff := int64(ms.NumGC) - int64(m.lastNumGC)
		if gcDiff >= 0 {
			metric.GCPerMinute = float64(gcDiff) / dtSec * 60.0
		}
	}
	m.lastTotalAlloc = ms.TotalAlloc
	m.lastNumGC = ms.NumGC
	m.lastSampleAt = now
	m.mu.Unlock()

	metric.GCPauseAvgMs, metric.GCPauseP95Ms, metric.GCPauseP99Ms, metric.GCPauseMaxMs = pauseDistribution(pauses)

	// ── 进程 / 系统（gopsutil） ──
	m.attachProcessStats(ctx, &metric)
	m.attachSystemStats(ctx, &metric)

	// ── 限制上下文（GCTuner） ──
	if m.tuner != nil {
		snap := m.tuner.Snapshot()
		metric.MemoryLimit = snap.MemoryLimitRaw
		metric.GOGC = int(snap.GOGC)
		if snap.MemoryLimitRaw > 0 {
			metric.UsageRatio = float64(ms.HeapAlloc) / float64(snap.MemoryLimitRaw)
		}
	} else {
		// 回退：从 debug.SetMemoryLimit(-1) 读当前值（不会改）
		limit := debug.SetMemoryLimit(-1)
		if limit > 0 && limit < math.MaxInt64 {
			metric.MemoryLimit = limit
			metric.UsageRatio = float64(ms.HeapAlloc) / float64(limit)
		}
		// GOGC 反复设回去
		gogc := debug.SetGCPercent(-1)
		if gogc >= 0 {
			debug.SetGCPercent(gogc)
			metric.GOGC = gogc
		}
	}

	return metric
}

func (m *MemoryMonitor) collectAndStore(ctx context.Context) {
	metric := m.collect(ctx)

	m.mu.Lock()
	m.latest = &metric
	m.mu.Unlock()

	m.checkAlerts(&metric)

	data, err := json.Marshal(metric)
	if err != nil {
		return
	}
	key := m.redisKey("memory:history")
	score := float64(metric.Timestamp.UnixMilli())
	pipe := m.redis.Pipeline()
	pipe.ZAdd(ctx, key, redislib.Z{Score: score, Member: string(data)})
	cutoff := fmt.Sprintf("%d", metric.Timestamp.Add(-m.historyRetain).UnixMilli())
	pipe.ZRemRangeByScore(ctx, key, "-inf", cutoff)
	if _, err := pipe.Exec(ctx); err != nil {
		m.log.Debug("内存监控：存储历史指标失败", zap.Error(err))
	}
}

// absorbPauses 把 ms.PauseNs 环形缓冲里"上次没消化过"的新暂停点并入监控侧滑窗
// 必须在持锁下调用（读写 pausesBuf / lastNumGC）
func (m *MemoryMonitor) absorbPauses(ms *runtime.MemStats) {
	if m.pausesBuf == nil {
		m.pausesBuf = make([]uint64, 0, memoryPausesBufferSize)
	}
	newGC := int64(ms.NumGC) - int64(m.lastNumGC)
	if newGC <= 0 {
		return
	}
	if newGC > int64(len(ms.PauseNs)) {
		newGC = int64(len(ms.PauseNs))
	}
	// Go 源码约定：ms.PauseNs[(NumGC+255)%256] 是最近一次暂停
	for i := int64(0); i < newGC; i++ {
		idx := (int64(ms.NumGC) - i - 1 + int64(len(ms.PauseNs))) % int64(len(ms.PauseNs))
		pause := ms.PauseNs[idx]
		if pause == 0 {
			continue
		}
		if len(m.pausesBuf) < memoryPausesBufferSize {
			m.pausesBuf = append(m.pausesBuf, pause)
		} else {
			// 环形写入：用最老位置
			m.pausesBuf[int(ms.NumGC)%memoryPausesBufferSize] = pause
		}
	}
}

// pauseDistribution 计算 avg/p95/p99/max（单位：ms），输入是纳秒
func pauseDistribution(pauses []uint64) (avgMs, p95Ms, p99Ms, maxMs float64) {
	if len(pauses) == 0 {
		return 0, 0, 0, 0
	}
	sorted := append([]uint64(nil), pauses...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	var sum uint64
	for _, v := range sorted {
		sum += v
	}
	avgMs = float64(sum) / float64(len(sorted)) / 1e6
	maxMs = float64(sorted[len(sorted)-1]) / 1e6
	pctIdx := func(p float64) int {
		idx := int(math.Ceil(p*float64(len(sorted)))) - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= len(sorted) {
			idx = len(sorted) - 1
		}
		return idx
	}
	p95Ms = float64(sorted[pctIdx(0.95)]) / 1e6
	p99Ms = float64(sorted[pctIdx(0.99)]) / 1e6
	return
}

// attachProcessStats 用 gopsutil 取进程 RSS / CPU / 线程数
// 失败不致命：指标字段保持 0，后续采集会再次尝试。
func (m *MemoryMonitor) attachProcessStats(ctx context.Context, metric *MemoryMetrics) {
	m.procOnce.Do(func() {
		p, err := process.NewProcess(int32(os.Getpid()))
		if err != nil {
			m.log.Debug("内存监控：gopsutil 进程初始化失败", zap.Error(err))
			return
		}
		m.proc = p
	})
	if m.proc == nil {
		return
	}
	if mi, err := m.proc.MemoryInfoWithContext(ctx); err == nil && mi != nil {
		metric.ProcessRSS = mi.RSS
	}
	if cpuPct, err := m.proc.CPUPercentWithContext(ctx); err == nil {
		metric.ProcessCPU = cpuPct
	}
	if n, err := m.proc.NumThreadsWithContext(ctx); err == nil {
		metric.ProcessThreads = n
	}
}

// attachSystemStats 系统级物理内存使用情况
func (m *MemoryMonitor) attachSystemStats(ctx context.Context, metric *MemoryMetrics) {
	vm, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil || vm == nil {
		return
	}
	metric.SysTotal = vm.Total
	metric.SysUsed = vm.Used
	metric.SysAvailable = vm.Available
	metric.SysUsedPercent = vm.UsedPercent
}

// ── 告警：滞回 + 防毛刺 + 日志节流 + 恢复事件 ──

func (m *MemoryMonitor) checkAlerts(metric *MemoryMetrics) {
	now := time.Now()
	for _, rule := range m.rules {
		value, ok := extractRuleValue(rule.Key, metric)
		if !ok {
			continue
		}
		st, exists := m.stateFor(rule.Key)
		if !exists {
			continue
		}
		st.lastValue = value
		over := value >= rule.Trigger
		under := value <= rule.Clear

		m.mu.Lock()
		switch {
		case !st.firing && over:
			if st.firstOver.IsZero() {
				st.firstOver = now
			}
			if now.Sub(st.firstOver) >= rule.FireDelay {
				st.firing = true
				st.firstFire = now
				st.lastLogAt = now
				m.appendAlertLocked(MemoryAlert{
					Time: now.UTC(), Key: rule.Key, Category: rule.Category,
					Level: rule.Level, State: "firing", Message: rule.Message,
					Value: value, Threshold: rule.Trigger,
				})
				m.log.Warn("内存监控告警",
					zap.String("key", rule.Key),
					zap.String("level", rule.Level),
					zap.String("state", "firing"),
					zap.String("message", rule.Message),
					zap.Float64("value", value),
					zap.Float64("threshold", rule.Trigger),
				)
			}
		case st.firing && !under:
			// 保持 firing。日志按 LogCooldown 节流
			if now.Sub(st.lastLogAt) >= rule.LogCooldown {
				st.lastLogAt = now
				m.log.Warn("内存监控告警持续",
					zap.String("key", rule.Key),
					zap.String("level", rule.Level),
					zap.String("state", "firing"),
					zap.String("message", rule.Message),
					zap.Float64("value", value),
					zap.Duration("duration", now.Sub(st.firstFire)),
				)
			}
		case st.firing && under:
			dur := now.Sub(st.firstFire)
			m.appendAlertLocked(MemoryAlert{
				Time: now.UTC(), Key: rule.Key, Category: rule.Category,
				Level: "info", State: "resolved",
				Message: rule.Message + "（已恢复）",
				Value:   value, Threshold: rule.Clear,
				DurationMs: dur.Milliseconds(),
			})
			m.log.Info("内存监控告警恢复",
				zap.String("key", rule.Key),
				zap.Float64("value", value),
				zap.Float64("clearThreshold", rule.Clear),
				zap.Duration("firingDuration", dur),
			)
			st.firing = false
			st.firstOver = time.Time{}
			st.firstFire = time.Time{}
			st.lastLogAt = time.Time{}
		case !st.firing && !over:
			// 从候选状态回落，清零 firstOver
			st.firstOver = time.Time{}
		}
		m.mu.Unlock()
	}
}

func (m *MemoryMonitor) stateFor(key string) (*alertState, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.states[key]
	if !ok {
		st = &alertState{}
		m.states[key] = st
	}
	return st, true
}

func (m *MemoryMonitor) appendAlertLocked(a MemoryAlert) {
	m.alerts = append(m.alerts, a)
	if len(m.alerts) > memoryAlertsCapacity {
		m.alerts = m.alerts[len(m.alerts)-memoryAlertsCapacity:]
	}
}

func (m *MemoryMonitor) redisKey(suffix string) string { return m.keyPrefix + ":" + suffix }

// extractRuleValue 按规则 key 从指标里取当前值
func extractRuleValue(key string, metric *MemoryMetrics) (float64, bool) {
	switch key {
	case "heap_usage":
		if metric.UsageRatio > 0 {
			return metric.UsageRatio * 100.0, true
		}
		// 回退：没有 MemoryLimit 时用系统内存使用率
		if metric.SysUsedPercent > 0 {
			return metric.SysUsedPercent, true
		}
		return 0, false
	case "process_rss_ratio":
		if metric.SysTotal > 0 && metric.ProcessRSS > 0 {
			return float64(metric.ProcessRSS) / float64(metric.SysTotal) * 100.0, true
		}
		return 0, false
	case "system_memory":
		if metric.SysUsedPercent > 0 {
			return metric.SysUsedPercent, true
		}
		return 0, false
	case "goroutines":
		return float64(metric.Goroutines), true
	case "heap_objects":
		return float64(metric.HeapObjects), true
	case "gc_pause_p99":
		return metric.GCPauseP99Ms, true
	case "gc_cpu_fraction":
		return metric.GCCPUFraction * 100.0, true
	case "alloc_rate":
		return metric.AllocRateMBps, true
	}
	return 0, false
}

// defaultMemoryAlertRules 默认告警规则（带滞回）
//
//	规则 ID        触发(≥)  清除(≤)  防抖     冷却    等级        分类
//	heap_usage       85      75       30s     5min    warning    memory
//	heap_usage       95      90       30s     2min    critical   memory    （高优先级，同 key 按 level 顺序匹配）
//	process_rss      75      65       60s     5min    warning    memory
//	system_memory    90      85       60s     5min    warning    system
//	goroutines       5000    3000     60s    10min    warning    runtime
//	goroutines      20000   15000     60s     5min    critical   runtime
//	heap_objects     5M      3M      120s    10min    warning    runtime
//	gc_pause_p99    200     100      60s     5min    warning    gc
//	gc_cpu_fraction  15      10      60s     5min    warning    gc
//	alloc_rate      500     300     120s    10min    warning    runtime     MB/s
func defaultMemoryAlertRules() []AlertRule {
	return []AlertRule{
		// heap usage vs MemoryLimit（UsageRatio * 100）
		{Key: "heap_usage_critical", Category: "memory", Level: "critical", Message: "堆内存使用率超过 95%（相对软限制）",
			Trigger: 95, Clear: 90, FireDelay: 30 * time.Second, LogCooldown: 2 * time.Minute},
		{Key: "heap_usage", Category: "memory", Level: "warning", Message: "堆内存使用率超过 85%（相对软限制）",
			Trigger: 85, Clear: 75, FireDelay: 30 * time.Second, LogCooldown: 5 * time.Minute},

		// 进程 RSS 占系统内存比
		{Key: "process_rss_ratio", Category: "memory", Level: "warning", Message: "进程 RSS 占系统内存超过 75%",
			Trigger: 75, Clear: 65, FireDelay: time.Minute, LogCooldown: 5 * time.Minute},

		// 系统物理内存使用率
		{Key: "system_memory", Category: "system", Level: "warning", Message: "系统内存使用率超过 90%",
			Trigger: 90, Clear: 85, FireDelay: time.Minute, LogCooldown: 5 * time.Minute},

		// Goroutine 数
		{Key: "goroutines_critical", Category: "runtime", Level: "critical", Message: "Goroutine 数超过 20000（疑似泄漏）",
			Trigger: 20000, Clear: 15000, FireDelay: time.Minute, LogCooldown: 5 * time.Minute},
		{Key: "goroutines", Category: "runtime", Level: "warning", Message: "Goroutine 数超过 5000",
			Trigger: 5000, Clear: 3000, FireDelay: time.Minute, LogCooldown: 10 * time.Minute},

		// 堆对象数
		{Key: "heap_objects", Category: "runtime", Level: "warning", Message: "堆对象数超过 500 万",
			Trigger: 5_000_000, Clear: 3_000_000, FireDelay: 2 * time.Minute, LogCooldown: 10 * time.Minute},

		// GC 暂停 P99
		{Key: "gc_pause_p99", Category: "gc", Level: "warning", Message: "GC 暂停 P99 超过 200ms",
			Trigger: 200, Clear: 100, FireDelay: time.Minute, LogCooldown: 5 * time.Minute},

		// GC CPU 占比
		{Key: "gc_cpu_fraction", Category: "gc", Level: "warning", Message: "GC 占 CPU 超过 15%",
			Trigger: 15, Clear: 10, FireDelay: time.Minute, LogCooldown: 5 * time.Minute},

		// 分配速率（MB/s）
		{Key: "alloc_rate", Category: "runtime", Level: "warning", Message: "内存分配速率超过 500 MB/s",
			Trigger: 500, Clear: 300, FireDelay: 2 * time.Minute, LogCooldown: 10 * time.Minute},
	}
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
