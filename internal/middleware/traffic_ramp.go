package middleware

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"aegis/internal/config"
	systemdomain "aegis/internal/domain/system"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"golang.org/x/time/rate"
)

// TrafficRamp 突发流量爬坡：单实例自适应准入控制。
//
// 与 Firewall 的分工：Firewall 按 IP 拒绝**恶意**流量（Redis 计数，跨实例一致），
// TrafficRamp 整形**合法**流量的进入速度 —— 突发洪峰不是一次性放进后端，
// 而是从基线速率起按步长逐步抬高准入上限（爬坡），让冷连接池 / 冷缓存 /
// 下游依赖有时间热身；多出来的请求先短暂排队削峰，排不下再回 429 + Retry-After。
// 峰值退去后上限按同样的节奏回落到基线。
//
// 状态机四态（见 systemdomain.TrafficRampState*）：
//
//	stable ──需求逼近上限──▶ ramping ──爬到顶仍供不应求──▶ saturated
//	   ▲                        │
//	   └──回落到基线── cooldown ◀──需求持续低于基线──┘
//
// 所有计数与上限都是**单实例**口径：每个实例保护的是自己这条进程，
// 多副本部署时集群总吞吐 = 副本数 × MaxRPS。这也是刻意不走 Redis 的原因：
// 准入判定在每个请求的热路径上，一次跨网络往返比被保护的操作本身还贵。
type TrafficRamp struct {
	log *zap.Logger

	// mu 保护配置、热重载元信息与爬坡状态机。
	mu         sync.RWMutex
	cfg        config.TrafficRampConfig
	version    uint64
	reloadedAt time.Time

	state       string
	ceiling     float64   // 当前准入上限（RPS），恒在 [baseline, max] 区间
	belowSince  time.Time // 需求低于基线的起始时刻；零值 = 当前不低于
	lastBurstAt time.Time
	rampEvents  int64
	lastPeakSec int64 // 峰值统计已经消化到的秒，防止重复计

	// limiter 是准入令牌桶；SetLimit/SetBurst 随爬坡动态调节。
	limiter *rate.Limiter

	// ceilingBits 上限的原子副本（math.Float64bits）。
	// 时间序列环在 statsMu 里要读上限，用原子副本避免 statsMu 与 mu 嵌套。
	ceilingBits atomic.Uint64

	// 即时水位与累计计数（原子，热路径不加锁）。
	inflight             atomic.Int64
	queueDepth           atomic.Int64
	totalArrivals        atomic.Int64
	totalAdmitted        atomic.Int64
	totalQueuedAdmitted  atomic.Int64
	totalRejectedRate    atomic.Int64
	totalRejectedTimeout atomic.Int64
	totalRejectedLoad    atomic.Int64
	totalExempt          atomic.Int64
	peakArrivalRPS       atomic.Int64
	peakLimitBits        atomic.Uint64
	statsSinceUnixMs     atomic.Int64

	// 每秒时间序列环（15 分钟）。statsMu 只保护环本身，临界区极小。
	statsMu sync.Mutex
	ring    [rampRingSize]rampRingSlot

	stopOnce sync.Once
	stopCh   chan struct{}
}

const rampRingSize = 900 // 15 分钟 × 60 秒

// rampLoopTick 后台评估循环的基础节拍。真实的爬坡周期由配置的
// RampIntervalMs 决定（最小 100ms），循环按节拍累计时间到点才评估，
// 这样热重载改周期无需重建循环。
const rampLoopTick = 100 * time.Millisecond

type rampRingSlot struct {
	epochSec int64
	arrivals int64
	admitted int64
	queued   int64
	rejected int64
	limit    float64
}

// NewTrafficRamp 构造并启动后台评估循环。cfg 会先过 Normalize，
// 与 Firewall 一样保证运行期拿到的配置永远自洽。
func NewTrafficRamp(cfg config.TrafficRampConfig, log *zap.Logger) *TrafficRamp {
	if log == nil {
		log = zap.NewNop()
	}
	cfg = config.NormalizeTrafficRampConfig(cfg)
	t := &TrafficRamp{
		log:        log,
		cfg:        cfg,
		version:    1,
		reloadedAt: time.Now().UTC(),
		state:      systemdomain.TrafficRampStateStable,
		ceiling:    float64(cfg.BaselineRPS),
		limiter:    rate.NewLimiter(rate.Limit(cfg.BaselineRPS), maxIntOne(cfg.BaselineRPS)),
		stopCh:     make(chan struct{}),
	}
	t.ceilingBits.Store(math.Float64bits(t.ceiling))
	t.peakLimitBits.Store(math.Float64bits(t.ceiling))
	t.statsSinceUnixMs.Store(time.Now().UnixMilli())
	go t.loop()
	return t
}

// Close 停掉后台评估循环（进程常驻场景不必调用，测试用）。
func (t *TrafficRamp) Close() {
	t.stopOnce.Do(func() { close(t.stopCh) })
}

// ── 热重载契约（与 Firewall 同一套四件） ──

func (t *TrafficRamp) CurrentConfig() config.TrafficRampConfig {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.cfg
}

func (t *TrafficRamp) ReloadMeta() (uint64, time.Time) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.version, t.reloadedAt
}

// ValidateConfig 所有字段都是数值与开关，Normalize 能修复一切非法区间，
// 因此这里恒为 nil。保留方法是为了与其他热重载运行时共用同一调用序列
// （Validate → 落库 → Reload），未来引入不可修复的字段时不必再改服务层。
func (t *TrafficRamp) ValidateConfig(cfg config.TrafficRampConfig) error {
	_ = cfg
	return nil
}

// Reload 原子替换配置并立即生效。上限被夹回新的 [baseline, max] 区间；
// 关闭时状态机复位（再次开启从基线干净起步，而不是带着上次的残余上限）。
func (t *TrafficRamp) Reload(cfg config.TrafficRampConfig) error {
	cfg = config.NormalizeTrafficRampConfig(cfg)
	t.mu.Lock()
	t.cfg = cfg
	baseline := float64(cfg.BaselineRPS)
	maxLimit := float64(cfg.MaxRPS)
	if !cfg.Enabled {
		t.state = systemdomain.TrafficRampStateStable
		t.ceiling = baseline
		t.belowSince = time.Time{}
	} else {
		if t.ceiling < baseline {
			t.ceiling = baseline
		}
		if t.ceiling > maxLimit {
			t.ceiling = maxLimit
		}
	}
	t.applyCeilingLocked()
	t.version++
	t.reloadedAt = time.Now().UTC()
	version := t.version
	enabled := cfg.Enabled
	t.mu.Unlock()

	t.log.Info("traffic ramp settings reloaded",
		zap.Uint64("version", version),
		zap.Bool("enabled", enabled),
		zap.Int("baseline_rps", cfg.BaselineRPS),
		zap.Int("max_rps", cfg.MaxRPS),
	)
	return nil
}

// applyCeilingLocked 把 t.ceiling 同步到令牌桶与原子副本。调用方必须持有 t.mu。
func (t *TrafficRamp) applyCeilingLocked() {
	t.ceilingBits.Store(math.Float64bits(t.ceiling))
	t.limiter.SetLimit(rate.Limit(t.ceiling))
	// 桶容量 = 1 秒的量：瞬时微突发的容忍度随上限一起爬。
	t.limiter.SetBurst(maxIntOne(int(t.ceiling)))
	if bits := t.peakLimitBits.Load(); t.ceiling > math.Float64frombits(bits) {
		t.peakLimitBits.Store(math.Float64bits(t.ceiling))
	}
}

// ── 请求处理 ──

func (t *TrafficRamp) Handler() gin.HandlerFunc {
	return func(c *gin.Context) {
		if t == nil {
			c.Next()
			return
		}
		cfg := t.CurrentConfig()
		if !cfg.Enabled {
			c.Next()
			return
		}
		// 预检与 HEAD 不占准入额度：浏览器的 CORS preflight 被拒会把
		// 真正的错误藏在一层跨域报错后面，排查方向完全指错。
		switch c.Request.Method {
		case http.MethodOptions, http.MethodHead:
			c.Next()
			return
		}
		path := strings.ToLower(strings.TrimSpace(c.Request.URL.Path))
		// 探活与实时通道恒免且不计数：编排系统每几秒一次的探针要是计进
		// 豁免统计，会把真正值得看的业务豁免量整个冲淡。
		if path == "/healthz" || path == "/readyz" || path == "/api/ws" {
			c.Next()
			return
		}
		if t.isExempt(cfg, path) {
			t.totalExempt.Add(1)
			c.Next()
			return
		}

		now := time.Now()
		t.totalArrivals.Add(1)
		t.note(now, rampNoteArrival)

		// 并发闸门：速率管「进入的快慢」，并发管「同时在处理的多少」。
		// 慢接口堆积时进入速率完全正常，进程照样会被拖垮。
		if cfg.MaxConcurrent > 0 && t.inflight.Load() >= int64(cfg.MaxConcurrent) {
			t.totalRejectedLoad.Add(1)
			t.note(now, rampNoteRejected)
			t.shed(c, cfg)
			return
		}

		// 快路径：桶里有令牌直接放行。
		if t.limiter.Allow() {
			t.totalAdmitted.Add(1)
			t.note(now, rampNoteAdmitted)
			t.serve(c)
			return
		}

		// 排队削峰：短暂等待即将补充的令牌，把毛刺抹平而不是直接打回。
		if cfg.QueueSize <= 0 || t.queueDepth.Load() >= int64(cfg.QueueSize) {
			t.totalRejectedRate.Add(1)
			t.note(now, rampNoteRejected)
			t.shed(c, cfg)
			return
		}
		t.queueDepth.Add(1)
		waitCtx, cancel := context.WithTimeout(c.Request.Context(), time.Duration(cfg.QueueTimeoutMs)*time.Millisecond)
		err := t.limiter.Wait(waitCtx)
		cancel()
		t.queueDepth.Add(-1)
		if err != nil {
			// 超时、等不到、或客户端已断开，一律按超时拒绝计。
			t.totalRejectedTimeout.Add(1)
			t.note(time.Now(), rampNoteRejected)
			t.shed(c, cfg)
			return
		}
		t.totalQueuedAdmitted.Add(1)
		t.note(time.Now(), rampNoteQueued)
		t.serve(c)
	}
}

func (t *TrafficRamp) serve(c *gin.Context) {
	t.inflight.Add(1)
	defer t.inflight.Add(-1)
	c.Next()
}

// shed 拒绝并明确告知何时重试。429 而不是 403：调用方该做的是稍后再来，
// 不是去排查凭据。
func (t *TrafficRamp) shed(c *gin.Context, cfg config.TrafficRampConfig) {
	c.Header("Retry-After", strconv.Itoa(cfg.RetryAfterSeconds))
	response.Error(c, http.StatusTooManyRequests, 42910, "服务繁忙，请稍后再试")
	c.Abort()
}

// isExempt 判定业务豁免（入参 path 已小写化）。
func (t *TrafficRamp) isExempt(cfg config.TrafficRampConfig, path string) bool {
	// 管理端默认免：洪峰恰恰是管理员最需要打开控制台看统计、调参数的时刻，
	// 把他们拒之门外等于把灭火通道也锁了。
	if cfg.ExemptAdmin && strings.HasPrefix(path, "/api/admin/") {
		return true
	}
	for _, prefix := range cfg.ExemptPathPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// ── 每秒时间序列环 ──

type rampNoteKind int

const (
	rampNoteArrival rampNoteKind = iota
	rampNoteAdmitted
	rampNoteQueued
	rampNoteRejected
)

func (t *TrafficRamp) note(now time.Time, kind rampNoteKind) {
	sec := now.Unix()
	t.statsMu.Lock()
	slot := &t.ring[sec%rampRingSize]
	if slot.epochSec != sec {
		*slot = rampRingSlot{
			epochSec: sec,
			limit:    math.Float64frombits(t.ceilingBits.Load()),
		}
	}
	switch kind {
	case rampNoteArrival:
		slot.arrivals++
	case rampNoteAdmitted:
		slot.admitted++
	case rampNoteQueued:
		slot.queued++
	case rampNoteRejected:
		slot.rejected++
	}
	t.statsMu.Unlock()
}

// arrivalsAt 读某一秒的到达数（demand 信号）。
func (t *TrafficRamp) arrivalsAt(sec int64) int64 {
	t.statsMu.Lock()
	defer t.statsMu.Unlock()
	slot := &t.ring[sec%rampRingSize]
	if slot.epochSec != sec {
		return 0
	}
	return slot.arrivals
}

// ── 爬坡状态机 ──

func (t *TrafficRamp) loop() {
	ticker := time.NewTicker(rampLoopTick)
	defer ticker.Stop()
	var sinceEval time.Duration
	for {
		select {
		case <-t.stopCh:
			return
		case <-ticker.C:
			sinceEval += rampLoopTick
			t.mu.RLock()
			interval := time.Duration(t.cfg.RampIntervalMs) * time.Millisecond
			t.mu.RUnlock()
			if sinceEval < interval {
				continue
			}
			sinceEval = 0
			t.evaluate(time.Now())
		}
	}
}

// evaluate 跑一轮爬坡评估。demand 取上一个**完整**秒的到达数 ——
// 当前秒还没走完，读它会把半秒的量当成一整秒，需求被系统性低估一半。
// 代价是突发起点最多滞后一秒，这一秒由排队队列先顶住。
func (t *TrafficRamp) evaluate(now time.Time) {
	prevSec := now.Unix() - 1
	demand := float64(t.arrivalsAt(prevSec))

	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.cfg.Enabled {
		return
	}

	// 峰值到达速率：每个完整秒只消化一次。
	if prevSec > t.lastPeakSec {
		t.lastPeakSec = prevSec
		if d := int64(demand); d > t.peakArrivalRPS.Load() {
			t.peakArrivalRPS.Store(d)
		}
	}

	baseline := float64(t.cfg.BaselineRPS)
	maxLimit := float64(t.cfg.MaxRPS)
	step := baseline * float64(t.cfg.RampStepPct) / 100
	if step <= 0 {
		step = 1
	}
	prevState := t.state

	switch {
	// 需求逼近当前上限（90% 起算，等真打满才动手就已经在拒人了）：向上爬。
	case demand >= t.ceiling*0.9 && demand > 0:
		t.belowSince = time.Time{}
		if t.ceiling < maxLimit {
			if prevState != systemdomain.TrafficRampStateRamping && prevState != systemdomain.TrafficRampStateSaturated {
				t.rampEvents++
				t.lastBurstAt = now.UTC()
			}
			t.ceiling = math.Min(maxLimit, t.ceiling+step)
			if t.ceiling >= maxLimit {
				t.state = systemdomain.TrafficRampStateSaturated
			} else {
				t.state = systemdomain.TrafficRampStateRamping
			}
		} else {
			t.state = systemdomain.TrafficRampStateSaturated
		}
		t.applyCeilingLocked()

	// 需求低于基线：熬过冷静期才开始回落，锯齿流量下立刻回落会反复抖。
	case demand < baseline:
		if t.ceiling > baseline {
			if t.belowSince.IsZero() {
				t.belowSince = now
			}
			if now.Sub(t.belowSince) >= time.Duration(t.cfg.CooldownSeconds)*time.Second {
				t.ceiling = math.Max(baseline, t.ceiling-step)
				if t.ceiling <= baseline {
					t.state = systemdomain.TrafficRampStateStable
					t.belowSince = time.Time{}
				} else {
					t.state = systemdomain.TrafficRampStateCooldown
				}
				t.applyCeilingLocked()
			}
		} else {
			t.state = systemdomain.TrafficRampStateStable
			t.belowSince = time.Time{}
		}

	// 介于两者之间：稳住当前上限，既不加码也不回收。
	default:
		t.belowSince = time.Time{}
		if t.ceiling > baseline {
			t.state = systemdomain.TrafficRampStateRamping
		} else {
			t.state = systemdomain.TrafficRampStateStable
		}
	}

	if t.state != prevState {
		t.log.Info("traffic ramp state transition",
			zap.String("from", prevState),
			zap.String("to", t.state),
			zap.Float64("ceiling_rps", t.ceiling),
			zap.Float64("demand_rps", demand),
		)
	}
}

// ── 统计视图 ──

// StatsView 运行态快照 + 最近 seconds 秒的时间序列（60–900 秒夹取）。
func (t *TrafficRamp) StatsView(seconds int) systemdomain.TrafficRampStats {
	if seconds < 60 {
		seconds = 60
	}
	if seconds > rampRingSize {
		seconds = rampRingSize
	}

	t.mu.RLock()
	cfg := t.cfg
	state := t.state
	ceiling := t.ceiling
	rampEvents := t.rampEvents
	lastBurst := t.lastBurstAt
	t.mu.RUnlock()

	stats := systemdomain.TrafficRampStats{
		Enabled:              cfg.Enabled,
		State:                state,
		CurrentLimit:         ceiling,
		BaselineRPS:          cfg.BaselineRPS,
		MaxRPS:               cfg.MaxRPS,
		Inflight:             t.inflight.Load(),
		QueueDepth:           t.queueDepth.Load(),
		QueueCapacity:        cfg.QueueSize,
		TotalArrivals:        t.totalArrivals.Load(),
		TotalAdmitted:        t.totalAdmitted.Load(),
		TotalQueuedAdmitted:  t.totalQueuedAdmitted.Load(),
		TotalRejectedRate:    t.totalRejectedRate.Load(),
		TotalRejectedTimeout: t.totalRejectedTimeout.Load(),
		TotalRejectedLoad:    t.totalRejectedLoad.Load(),
		TotalExempt:          t.totalExempt.Load(),
		RampEvents:           rampEvents,
		PeakArrivalRPS:       t.peakArrivalRPS.Load(),
		PeakLimit:            math.Float64frombits(t.peakLimitBits.Load()),
		StatsSince:           time.UnixMilli(t.statsSinceUnixMs.Load()).UTC(),
	}
	if !lastBurst.IsZero() {
		burst := lastBurst
		stats.LastBurstAt = &burst
	}

	// 序列：空秒补零行；上限列对空秒做前向填充，图上才不会掉到 0。
	nowSec := time.Now().Unix()
	points := make([]systemdomain.TrafficRampSeriesPoint, 0, seconds)
	t.statsMu.Lock()
	for sec := nowSec - int64(seconds) + 1; sec <= nowSec; sec++ {
		slot := &t.ring[sec%rampRingSize]
		point := systemdomain.TrafficRampSeriesPoint{Ts: sec}
		if slot.epochSec == sec {
			point.Arrivals = slot.arrivals
			point.Admitted = slot.admitted
			point.Queued = slot.queued
			point.Rejected = slot.rejected
			point.Limit = slot.limit
		}
		points = append(points, point)
	}
	t.statsMu.Unlock()
	var lastLimit float64
	for i := range points {
		if points[i].Limit > 0 {
			lastLimit = points[i].Limit
		} else {
			points[i].Limit = lastLimit
		}
	}
	// 序列末尾还没有样本时，至少让最新一格反映当前上限。
	if len(points) > 0 && points[len(points)-1].Limit == 0 {
		points[len(points)-1].Limit = ceiling
	}
	stats.Series = points
	return stats
}

// ResetStats 清零累计计数与时间序列。状态机与当前上限不动：
// 清统计是观测动作，不该顺手把正在进行的爬坡打断。
func (t *TrafficRamp) ResetStats() {
	t.totalArrivals.Store(0)
	t.totalAdmitted.Store(0)
	t.totalQueuedAdmitted.Store(0)
	t.totalRejectedRate.Store(0)
	t.totalRejectedTimeout.Store(0)
	t.totalRejectedLoad.Store(0)
	t.totalExempt.Store(0)
	t.peakArrivalRPS.Store(0)
	t.peakLimitBits.Store(t.ceilingBits.Load())
	t.statsSinceUnixMs.Store(time.Now().UnixMilli())
	t.mu.Lock()
	t.rampEvents = 0
	t.lastBurstAt = time.Time{}
	t.mu.Unlock()
	t.statsMu.Lock()
	t.ring = [rampRingSize]rampRingSlot{}
	t.statsMu.Unlock()
}

func maxIntOne(v int) int {
	if v < 1 {
		return 1
	}
	return v
}
