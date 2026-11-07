package traffic

import (
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// overload 进程级过载保护（思路同 Kratos / Sentinel 的 BBR 自适应限流）。
//
// 只看 CPU 会误伤：CPU 高可能是后台任务，请求本身处理得很顺；只看在途数也不行：
// 慢下游让在途数涨上去时 CPU 可能很闲。两者同时成立才说明「处理能力已经跟不上」：
//
//	承载量（BDP）= 每秒最大完成数 × 最小平均响应时间
//	丢弃条件   = CPU ≥ 阈值  且  在途 > 承载量
//
// 触发后在冷静期内只看在途是否仍超承载量，防止 CPU 在阈值附近上下抖时反复开关。
// 档位越低越早被丢：low 的 CPU 阈值低 10 个点、承载量按 0.8 倍算；high 高 5 个点、
// 按 1.2 倍算；critical 永不因过载被丢。
type overload struct {
	enabled   atomic.Bool
	threshold atomic.Int64
	maxInfl   atomic.Int64
	cooldown  atomic.Int64 // 纳秒

	inflight atomic.Int64
	cpuMilli atomic.Int64 // CPU 使用率 × 10（千分比）

	// 完成统计：100ms 一格，滚动 10 格（1 秒窗口）。
	passCur  atomic.Int64
	rttCur   atomic.Int64
	mu       sync.Mutex
	pass     [10]int64
	rtt      [10]int64
	idx      int
	maxPass  atomic.Int64 // 每秒
	minRTTNs atomic.Int64

	lastDrop atomic.Int64 // mono 纳秒
	dropped  atomic.Int64
	active   atomic.Bool

	// CPU 采样私有状态
	lastCPU  time.Duration
	lastWall time.Time
	cpuOK    bool
}

func newOverload() *overload {
	o := &overload{}
	o.minRTTNs.Store(int64(time.Millisecond))
	o.lastWall = time.Now()
	o.lastCPU, o.cpuOK = processCPUTime()
	return o
}

func (o *overload) configure(s OverloadSpec) {
	o.enabled.Store(s.Enabled)
	o.threshold.Store(int64(s.CPUThreshold))
	o.maxInfl.Store(int64(s.MaxInflight))
	o.cooldown.Store(int64(time.Duration(s.CooldownMs) * time.Millisecond))
}

// capacity 估算的在途承载量。样本不足时返回 +Inf（不丢）。
func (o *overload) capacity() float64 {
	mp := o.maxPass.Load()
	if mp <= 0 {
		return math.Inf(1)
	}
	return float64(mp) * float64(o.minRTTNs.Load()) / 1e9
}

// shouldDrop 判定是否丢弃一个新请求（调用时该请求尚未计入在途）。
func (o *overload) shouldDrop(class string, now int64) bool {
	if class == ClassCritical || !o.enabled.Load() {
		return false
	}
	infl := o.inflight.Load()
	if m := o.maxInfl.Load(); m > 0 && infl >= m {
		o.markDrop(now)
		return true
	}
	thr := float64(o.threshold.Load()) * 10
	factor := 1.0
	switch class {
	case ClassLow:
		thr -= 100
		factor = 0.8
	case ClassHigh:
		thr += 50
		factor = 1.2
	}
	capacity := o.capacity() * factor
	if capacity < 1 {
		capacity = 1
	}
	if float64(infl) <= capacity {
		return false
	}
	if float64(o.cpuMilli.Load()) >= thr {
		o.markDrop(now)
		return true
	}
	// 冷静期：刚丢过的话，在途仍超承载量就继续丢。
	if last := o.lastDrop.Load(); last > 0 && now-last < o.cooldown.Load() {
		o.dropped.Add(1)
		return true
	}
	return false
}

func (o *overload) markDrop(now int64) {
	o.lastDrop.Store(now)
	o.dropped.Add(1)
}

// done 记录一次完成。
func (o *overload) done(rtt int64) {
	o.passCur.Add(1)
	o.rttCur.Add(rtt)
}

// rotate 每 100ms 调用：滚动完成统计，重算最大吞吐与最小 RTT。
func (o *overload) rotate() {
	pass := o.passCur.Swap(0)
	rtt := o.rttCur.Swap(0)
	o.mu.Lock()
	o.idx = (o.idx + 1) % len(o.pass)
	o.pass[o.idx] = pass
	o.rtt[o.idx] = rtt
	var maxPass int64
	minRTT := int64(math.MaxInt64)
	for i := range o.pass {
		if o.pass[i] > maxPass {
			maxPass = o.pass[i]
		}
		if o.pass[i] > 0 {
			if avg := o.rtt[i] / o.pass[i]; avg < minRTT {
				minRTT = avg
			}
		}
	}
	o.mu.Unlock()
	o.maxPass.Store(maxPass * 10)
	if minRTT != math.MaxInt64 {
		if minRTT < int64(10*time.Microsecond) {
			minRTT = int64(10 * time.Microsecond)
		}
		o.minRTTNs.Store(minRTT)
	}
}

// sampleCPU 每 250ms 调用：EWMA 平滑后的进程 CPU 使用率（按可用核数归一）。
func (o *overload) sampleCPU() {
	if !o.cpuOK {
		return
	}
	cpu, ok := processCPUTime()
	if !ok {
		return
	}
	now := time.Now()
	wall := now.Sub(o.lastWall)
	if wall <= 0 {
		return
	}
	used := cpu - o.lastCPU
	o.lastCPU, o.lastWall = cpu, now
	cores := runtime.GOMAXPROCS(0)
	cur := float64(used) / float64(wall) / float64(cores) * 1000
	if cur < 0 {
		cur = 0
	}
	if cur > 1000 {
		cur = 1000
	}
	prev := float64(o.cpuMilli.Load())
	o.cpuMilli.Store(int64(prev*0.6 + cur*0.4))
}

// updateActive 刷新「保护中」状态，返回状态是否翻转。
func (o *overload) updateActive(now int64) (changed, active bool) {
	last := o.lastDrop.Load()
	act := last > 0 && now-last < o.cooldown.Load()+int64(time.Second)
	prev := o.active.Swap(act)
	return prev != act, act
}

func (o *overload) view() OverloadView {
	v := OverloadView{
		Enabled:   o.enabled.Load(),
		Active:    o.active.Load(),
		CPU:       math.Round(float64(o.cpuMilli.Load())) / 10,
		Threshold: int(o.threshold.Load()),
		Inflight:  o.inflight.Load(),
		MaxPass:   o.maxPass.Load(),
		MinRTTMs:  roundMs(float64(o.minRTTNs.Load()) / 1e6),
		Dropped:   o.dropped.Load(),
	}
	if c := o.capacity(); !math.IsInf(c, 1) {
		v.Capacity = math.Round(c*10) / 10
	}
	if last := o.lastDrop.Load(); last > 0 {
		t := monoToTime(last)
		v.LastDropAt = &t
	}
	return v
}

// monoToTime 把 mono() 时间换回墙钟时间（展示用）。
func monoToTime(m int64) time.Time {
	return monoBase.Add(time.Duration(m - int64(time.Hour))).UTC()
}

// ── 预热爬坡 ──

// warmup 一条规则的爬坡控制器。上限以 gcraLimiter 的速率体现；
// 状态机由采样循环按配置的周期推进，热路径只读限速器。
type warmup struct {
	limiter *gcraLimiter

	mu          sync.Mutex
	spec        WarmupSpec
	state       string
	ceiling     float64
	belowSince  time.Time
	lastBurstAt time.Time
	events      int64
	lastEval    time.Time
	ceilingBits atomic.Uint64
}

func newWarmup(spec WarmupSpec) *warmup {
	w := &warmup{state: WarmupStable, ceiling: float64(spec.BaselineRPS), spec: spec}
	w.limiter = newGCRALimiter(w.ceiling, time.Second, maxInt(1, spec.BaselineRPS))
	w.ceilingBits.Store(math.Float64bits(w.ceiling))
	return w
}

func (w *warmup) configure(spec WarmupSpec) {
	w.mu.Lock()
	w.spec = spec
	base, top := float64(spec.BaselineRPS), float64(spec.MaxRPS)
	if w.ceiling < base {
		w.ceiling = base
	}
	if w.ceiling > top {
		w.ceiling = top
	}
	w.applyLocked()
	w.mu.Unlock()
}

func (w *warmup) applyLocked() {
	w.limiter.set(w.ceiling, time.Second, maxInt(1, int(w.ceiling)))
	w.ceilingBits.Store(math.Float64bits(w.ceiling))
}

func (w *warmup) currentCeiling() float64 { return math.Float64frombits(w.ceilingBits.Load()) }

// evaluate 推进状态机。demand 是上一个完整秒的到达数。返回发生的事件（可能为空）。
func (w *warmup) evaluate(now time.Time, demand float64) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if now.Sub(w.lastEval) < time.Duration(w.spec.IntervalMs)*time.Millisecond {
		return ""
	}
	w.lastEval = now
	base, top := float64(w.spec.BaselineRPS), float64(w.spec.MaxRPS)
	step := base * float64(w.spec.StepPct) / 100
	if step < 1 {
		step = 1
	}
	prev := w.state
	event := ""
	switch {
	// 需求逼近上限（90% 起算，等真打满才动手就已经在拒人了）：向上爬。
	case demand > 0 && demand >= w.ceiling*0.9:
		w.belowSince = time.Time{}
		if w.ceiling < top {
			if prev != WarmupRamping && prev != WarmupSaturated {
				w.events++
				w.lastBurstAt = now.UTC()
				event = EventWarmupStart
			}
			w.ceiling = math.Min(top, w.ceiling+step)
		}
		if w.ceiling >= top {
			w.state = WarmupSaturated
			if prev != WarmupSaturated {
				event = EventWarmupSaturated
			}
		} else {
			w.state = WarmupRamping
		}
		w.applyLocked()
	// 需求低于基线：熬过冷静期才回落，锯齿流量下立刻回落会反复抖。
	case demand < base:
		if w.ceiling > base {
			if w.belowSince.IsZero() {
				w.belowSince = now
			}
			if now.Sub(w.belowSince) >= time.Duration(w.spec.CooldownSeconds)*time.Second {
				w.ceiling = math.Max(base, w.ceiling-step)
				if w.ceiling <= base {
					w.state = WarmupStable
					w.belowSince = time.Time{}
					event = EventWarmupSettled
				} else {
					w.state = WarmupCooldown
				}
				w.applyLocked()
			}
		} else {
			w.state = WarmupStable
			w.belowSince = time.Time{}
		}
	default:
		w.belowSince = time.Time{}
		if w.ceiling > base {
			w.state = WarmupRamping
		} else {
			w.state = WarmupStable
		}
	}
	return event
}

func (w *warmup) view() *WarmupView {
	w.mu.Lock()
	defer w.mu.Unlock()
	v := &WarmupView{
		State:      w.state,
		Ceiling:    math.Round(w.ceiling*10) / 10,
		Baseline:   w.spec.BaselineRPS,
		Max:        w.spec.MaxRPS,
		RampEvents: w.events,
	}
	if !w.lastBurstAt.IsZero() {
		t := w.lastBurstAt
		v.LastBurstAt = &t
	}
	return v
}
