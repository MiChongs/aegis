package traffic

import (
	"math"
	"math/bits"
	"math/rand/v2"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// 计数的热路径：每个请求要给全局与命中的每条规则各加几次计数。几十万 QPS 下
// 所有核心抢同一个原子变量会让缓存行在核间来回弹，代价比限速判定本身还高。
// 所以计数分成 16 条带（stripe），每次随机挑一条加；读的时候再把各条带求和。
// 秒级曲线不在热路径上维护，而是由采样循环每秒对累计值做差分得到。

const stripes = 16

// 计数项下标（与 Counts 字段一一对应）。
const (
	cArrivals = iota
	cAdmitted
	cQueued
	cBypassed
	cObserved
	cRejRate
	cRejConc
	cRejQueue
	cRejTimeout
	cRejOverload
	nCounters
)

// 延迟直方图：第 i 格上界 = 50µs × 2^i，共 24 格（最后一格约 7 分钟，兜底）。
const (
	histBuckets = 24
	histBaseNs  = 50_000
)

type stripe struct {
	c     [nCounters]atomic.Int64
	hist  [histBuckets]atomic.Int64
	sumNs atomic.Int64
	maxNs atomic.Int64
	_     [64]byte
}

type stripedStats struct {
	s [stripes]stripe
}

func (st *stripedStats) add(kind int) {
	st.s[rand.Uint32()&(stripes-1)].c[kind].Add(1)
}

func (st *stripedStats) observe(ns int64) {
	s := &st.s[rand.Uint32()&(stripes-1)]
	s.hist[histIndex(ns)].Add(1)
	s.sumNs.Add(ns)
	for {
		m := s.maxNs.Load()
		if ns <= m || s.maxNs.CompareAndSwap(m, ns) {
			break
		}
	}
}

func histIndex(ns int64) int {
	if ns <= histBaseNs {
		return 0
	}
	i := bits.Len64(uint64(ns / histBaseNs))
	if i >= histBuckets {
		return histBuckets - 1
	}
	return i
}

// histogram 一份直方图快照。
type histogram struct {
	b     [histBuckets]int64
	sumNs int64
	maxNs int64
}

func (h histogram) count() int64 {
	var n int64
	for _, v := range h.b {
		n += v
	}
	return n
}

func (h histogram) sub(o histogram) histogram {
	var r histogram
	for i := range h.b {
		r.b[i] = h.b[i] - o.b[i]
	}
	r.sumNs = h.sumNs - o.sumNs
	r.maxNs = h.maxNs
	return r
}

func (h *histogram) addInto(o histogram) {
	for i := range h.b {
		h.b[i] += o.b[i]
	}
	h.sumNs += o.sumNs
	if o.maxNs > h.maxNs {
		h.maxNs = o.maxNs
	}
}

// quantileMs 在格内线性插值，误差上界是格宽（相对误差 < 2 倍，足够看趋势）。
func (h histogram) quantileMs(q float64) float64 {
	total := h.count()
	if total == 0 {
		return 0
	}
	target := q * float64(total)
	var acc float64
	for i, v := range h.b {
		if v == 0 {
			continue
		}
		if acc+float64(v) >= target {
			lo := 0.0
			if i > 0 {
				lo = float64(int64(histBaseNs) << (i - 1))
			}
			hi := float64(int64(histBaseNs) << i)
			frac := (target - acc) / float64(v)
			ns := lo + (hi-lo)*frac
			if h.maxNs > 0 && ns > float64(h.maxNs) {
				ns = float64(h.maxNs)
			}
			return roundMs(ns / 1e6)
		}
		acc += float64(v)
	}
	return roundMs(float64(h.maxNs) / 1e6)
}

func roundMs(v float64) float64 { return math.Round(v*100) / 100 }

func (h histogram) view() LatencyView {
	n := h.count()
	if n == 0 {
		return LatencyView{}
	}
	return LatencyView{
		Samples: n,
		Avg:     roundMs(float64(h.sumNs) / float64(n) / 1e6),
		P50:     h.quantileMs(0.50),
		P90:     h.quantileMs(0.90),
		P99:     h.quantileMs(0.99),
		Max:     roundMs(float64(h.maxNs) / 1e6),
	}
}

// read 汇总各条带。
func (st *stripedStats) read() (Counts, histogram) {
	var c [nCounters]int64
	var h histogram
	for i := range st.s {
		s := &st.s[i]
		for k := range c {
			c[k] += s.c[k].Load()
		}
		for b := range h.b {
			h.b[b] += s.hist[b].Load()
		}
		h.sumNs += s.sumNs.Load()
		if m := s.maxNs.Load(); m > h.maxNs {
			h.maxNs = m
		}
	}
	return Counts{
		Arrivals:            c[cArrivals],
		Admitted:            c[cAdmitted],
		Queued:              c[cQueued],
		Bypassed:            c[cBypassed],
		Observed:            c[cObserved],
		RejectedRate:        c[cRejRate],
		RejectedConcurrency: c[cRejConc],
		RejectedQueue:       c[cRejQueue],
		RejectedTimeout:     c[cRejTimeout],
		RejectedOverload:    c[cRejOverload],
	}, h
}

func (st *stripedStats) reset() {
	for i := range st.s {
		s := &st.s[i]
		for k := range s.c {
			s.c[k].Store(0)
		}
		for b := range s.hist {
			s.hist[b].Store(0)
		}
		s.sumNs.Store(0)
		s.maxNs.Store(0)
	}
}

// ── 秒级环与分钟聚合（只由采样循环写，读者持 mu） ──

const ringSeconds = 900

type ringSlot struct {
	ts       int64
	c        Counts
	p50, p99 float32
	inflight int64
	limit    float32
}

type series struct {
	mu   sync.Mutex
	ring [ringSeconds]ringSlot

	// 采样循环私有：上一秒的累计值。
	prevC Counts
	prevH histogram
	// 最近 60 秒的直方图增量（滚动），用于「当前」分位。
	recent    [60]histogram
	recentIdx int

	// 当前分钟的聚合。
	minute      int64
	minC        Counts
	minH        histogram
	minMaxInfl  int64
	minPeakRPS  int64
	lastSec     Counts
	peakRPS     int64
	hasBaseline bool
	burstFlag   bool // 拒绝突增事件已报过（采样循环私有）
}

// tick 写入一秒：totals 是当前累计值。返回这一秒的增量。
func (s *series) tick(sec int64, totals Counts, h histogram, inflight int64, limit float64) (Counts, histogram) {
	if !s.hasBaseline {
		s.prevC, s.prevH, s.hasBaseline = totals, h, true
		return Counts{}, histogram{}
	}
	dc := totals.sub(s.prevC)
	dh := h.sub(s.prevH)
	// 统计被清零后累计值会变小，这一秒按 0 记。
	if dc.Arrivals < 0 || dc.Admitted < 0 {
		dc, dh = Counts{}, histogram{}
	}
	s.prevC, s.prevH = totals, h

	s.mu.Lock()
	s.ring[sec%ringSeconds] = ringSlot{
		ts:       sec,
		c:        dc,
		p50:      float32(dh.quantileMs(0.5)),
		p99:      float32(dh.quantileMs(0.99)),
		inflight: inflight,
		limit:    float32(limit),
	}
	s.recentIdx = (s.recentIdx + 1) % len(s.recent)
	s.recent[s.recentIdx] = dh
	s.lastSec = dc
	if dc.Arrivals > s.peakRPS {
		s.peakRPS = dc.Arrivals
	}
	s.mu.Unlock()
	return dc, dh
}

// accumulateMinute 把一秒的增量并入当前分钟；跨分钟时返回上一分钟的样本。
func (s *series) accumulateMinute(sec int64, dc Counts, dh histogram, inflight int64) (MinuteSample, bool) {
	minute := sec / 60 * 60
	var out MinuteSample
	flushed := false
	if s.minute != 0 && minute != s.minute && s.minC.Arrivals+s.minC.Bypassed > 0 {
		out = MinuteSample{
			Minute:      time.Unix(s.minute, 0).UTC(),
			Counts:      s.minC,
			P50Ms:       s.minH.quantileMs(0.5),
			P99Ms:       s.minH.quantileMs(0.99),
			MaxInflight: s.minMaxInfl,
			PeakRPS:     s.minPeakRPS,
		}
		flushed = true
	}
	if minute != s.minute {
		s.minute = minute
		s.minC, s.minH, s.minMaxInfl, s.minPeakRPS = Counts{}, histogram{}, 0, 0
	}
	s.minC = s.minC.Add(dc)
	s.minH.addInto(dh)
	if inflight > s.minMaxInfl {
		s.minMaxInfl = inflight
	}
	if dc.Arrivals > s.minPeakRPS {
		s.minPeakRPS = dc.Arrivals
	}
	return out, flushed
}

func (s *series) points(seconds int, nowSec int64) []SeriesPoint {
	if seconds < 60 {
		seconds = 60
	}
	if seconds > ringSeconds {
		seconds = ringSeconds
	}
	out := make([]SeriesPoint, 0, seconds)
	s.mu.Lock()
	for sec := nowSec - int64(seconds) + 1; sec <= nowSec; sec++ {
		slot := &s.ring[sec%ringSeconds]
		p := SeriesPoint{Ts: sec}
		if slot.ts == sec {
			p.Arrivals = slot.c.Arrivals
			p.Admitted = slot.c.Admitted
			p.Queued = slot.c.Queued
			p.Rejected = slot.c.Rejected()
			p.Observed = slot.c.Observed
			p.P50 = float64(slot.p50)
			p.P99 = float64(slot.p99)
			p.Inflight = slot.inflight
			p.Limit = float64(slot.limit)
		}
		out = append(out, p)
	}
	s.mu.Unlock()
	// 上限列前向填充：空秒不该让曲线掉到 0。
	var last float64
	for i := range out {
		if out[i].Limit > 0 {
			last = out[i].Limit
		} else {
			out[i].Limit = last
		}
	}
	return out
}

func (s *series) recentLatency() LatencyView {
	s.mu.Lock()
	var h histogram
	for _, r := range s.recent {
		h.addInto(r)
	}
	s.mu.Unlock()
	return h.view()
}

func (s *series) last() (Counts, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSec, s.peakRPS
}

func (s *series) reset() {
	s.mu.Lock()
	s.ring = [ringSeconds]ringSlot{}
	s.recent = [60]histogram{}
	s.lastSec = Counts{}
	s.peakRPS = 0
	s.mu.Unlock()
	s.hasBaseline = false
	s.minute, s.minC, s.minH, s.minMaxInfl, s.minPeakRPS = 0, Counts{}, histogram{}, 0, 0
}

// ── 热点键：Space-Saving top-K（只在拒绝路径上抽样） ──

const hotKeyCapacity = 32

type hotKeys struct {
	mu    sync.Mutex
	items map[string]*hotItem
}

type hotItem struct {
	kind  string
	count int64
}

func newHotKeys() *hotKeys { return &hotKeys{items: make(map[string]*hotItem, hotKeyCapacity)} }

// sampleHot 1/8 抽样：洪水时拒绝路径本身就是热路径，不能每次都进锁。
func sampleHot() bool { return rand.Uint32()&7 == 0 }

func (h *hotKeys) hit(kind, key string, weight int64) {
	h.mu.Lock()
	if it, ok := h.items[key]; ok {
		it.count += weight
		h.mu.Unlock()
		return
	}
	if len(h.items) < hotKeyCapacity {
		h.items[key] = &hotItem{kind: kind, count: weight}
		h.mu.Unlock()
		return
	}
	// 满了：替换计数最小的一项，新项继承它的计数（Space-Saving 的误差上界）。
	var minKey string
	var minItem *hotItem
	for k, it := range h.items {
		if minItem == nil || it.count < minItem.count {
			minKey, minItem = k, it
		}
	}
	delete(h.items, minKey)
	h.items[key] = &hotItem{kind: kind, count: minItem.count + weight}
	h.mu.Unlock()
}

// decay 计数减半，让榜单反映最近的情况；归零的项移除。
func (h *hotKeys) decay() {
	h.mu.Lock()
	for k, it := range h.items {
		it.count /= 2
		if it.count == 0 {
			delete(h.items, k)
		}
	}
	h.mu.Unlock()
}

func (h *hotKeys) top(n int) []HotKey {
	h.mu.Lock()
	out := make([]HotKey, 0, len(h.items))
	for k, it := range h.items {
		out = append(out, HotKey{Kind: it.kind, Key: k, Count: it.count})
	}
	h.mu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func (h *hotKeys) reset() {
	h.mu.Lock()
	h.items = make(map[string]*hotItem, hotKeyCapacity)
	h.mu.Unlock()
}
