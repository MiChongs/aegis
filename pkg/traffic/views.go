package traffic

import "time"

// 运行态视图：控制台与集群心跳读到的形状。全部是只读快照，生成时不阻塞热路径。

// Counts 一组累计计数。
type Counts struct {
	Arrivals            int64 `json:"arrivals"`
	Admitted            int64 `json:"admitted"`
	Queued              int64 `json:"queued"` // 排队后放行
	Bypassed            int64 `json:"bypassed"`
	Observed            int64 `json:"observed"` // 观察模式下「本应拒绝」
	RejectedRate        int64 `json:"rejectedRate"`
	RejectedConcurrency int64 `json:"rejectedConcurrency"`
	RejectedQueue       int64 `json:"rejectedQueue"` // 队列已满
	RejectedTimeout     int64 `json:"rejectedTimeout"`
	RejectedOverload    int64 `json:"rejectedOverload"`
}

// Rejected 拒绝合计。
func (c Counts) Rejected() int64 {
	return c.RejectedRate + c.RejectedConcurrency + c.RejectedQueue + c.RejectedTimeout + c.RejectedOverload
}

func (c Counts) sub(o Counts) Counts {
	return Counts{
		Arrivals:            c.Arrivals - o.Arrivals,
		Admitted:            c.Admitted - o.Admitted,
		Queued:              c.Queued - o.Queued,
		Bypassed:            c.Bypassed - o.Bypassed,
		Observed:            c.Observed - o.Observed,
		RejectedRate:        c.RejectedRate - o.RejectedRate,
		RejectedConcurrency: c.RejectedConcurrency - o.RejectedConcurrency,
		RejectedQueue:       c.RejectedQueue - o.RejectedQueue,
		RejectedTimeout:     c.RejectedTimeout - o.RejectedTimeout,
		RejectedOverload:    c.RejectedOverload - o.RejectedOverload,
	}
}

// Add 逐项相加（集群聚合用）。
func (c Counts) Add(o Counts) Counts {
	return Counts{
		Arrivals:            c.Arrivals + o.Arrivals,
		Admitted:            c.Admitted + o.Admitted,
		Queued:              c.Queued + o.Queued,
		Bypassed:            c.Bypassed + o.Bypassed,
		Observed:            c.Observed + o.Observed,
		RejectedRate:        c.RejectedRate + o.RejectedRate,
		RejectedConcurrency: c.RejectedConcurrency + o.RejectedConcurrency,
		RejectedQueue:       c.RejectedQueue + o.RejectedQueue,
		RejectedTimeout:     c.RejectedTimeout + o.RejectedTimeout,
		RejectedOverload:    c.RejectedOverload + o.RejectedOverload,
	}
}

// SeriesPoint 每秒一格。
type SeriesPoint struct {
	Ts       int64   `json:"ts"`
	Arrivals int64   `json:"arrivals"`
	Admitted int64   `json:"admitted"`
	Queued   int64   `json:"queued"`
	Rejected int64   `json:"rejected"`
	Observed int64   `json:"observed"`
	P50      float64 `json:"p50"` // 毫秒
	P99      float64 `json:"p99"`
	Inflight int64   `json:"inflight"`
	Limit    float64 `json:"limit"` // 预热上限（无预热为 0）
}

// LatencyView 响应时间分位（毫秒）。
type LatencyView struct {
	Samples int64   `json:"samples"`
	Avg     float64 `json:"avg"`
	P50     float64 `json:"p50"`
	P90     float64 `json:"p90"`
	P99     float64 `json:"p99"`
	Max     float64 `json:"max"`
}

// 预热状态机。
const (
	WarmupStable    = "stable"
	WarmupRamping   = "ramping"
	WarmupSaturated = "saturated"
	WarmupCooldown  = "cooldown"
)

// WarmupView 预热爬坡状态。
type WarmupView struct {
	State       string     `json:"state"`
	Ceiling     float64    `json:"ceiling"`
	Baseline    int        `json:"baseline"`
	Max         int        `json:"max"`
	RampEvents  int64      `json:"rampEvents"`
	LastBurstAt *time.Time `json:"lastBurstAt,omitempty"`
}

// HotKey 被拒绝最多的键（抽样估计）。
type HotKey struct {
	Kind  string `json:"kind"`
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

// LimitView 一项限速的运行态。
type LimitView struct {
	Key         string  `json:"key"`
	Name        string  `json:"name,omitempty"`
	Rate        int     `json:"rate"`
	PeriodMs    int     `json:"periodMs"`
	Burst       int     `json:"burst"`
	Scope       string  `json:"scope"`
	EffectiveRP float64 `json:"effectiveRate"` // 本实例实际承担的速率（集群均分后）
	TrackedKeys int64   `json:"trackedKeys"`
	Overflowed  int64   `json:"overflowed"`
	Rejected    int64   `json:"rejected"`
	LeaseMode   string  `json:"leaseMode,omitempty"` // cluster 全局键：lease | fallback
}

// RuleStats 一条规则的运行态。
type RuleStats struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Mode     string `json:"mode"`
	Class    string `json:"class"`
	Enabled  bool   `json:"enabled"`
	Totals   Counts `json:"totals"`
	LastSec  Counts `json:"lastSecond"`
	Inflight int64  `json:"inflight"`
	// ConcurrencyLimit 当前并发上限（自适应时会变化；0 = 未配置）。
	ConcurrencyLimit int64         `json:"concurrencyLimit"`
	QueueDepth       int64         `json:"queueDepth"`
	QueueCapacity    int           `json:"queueCapacity"`
	Latency          LatencyView   `json:"latency"`
	Warmup           *WarmupView   `json:"warmup,omitempty"`
	Limits           []LimitView   `json:"limits"`
	HotKeys          []HotKey      `json:"hotKeys"`
	Series           []SeriesPoint `json:"series,omitempty"`
}

// OverloadView 过载保护状态。
type OverloadView struct {
	Enabled    bool       `json:"enabled"`
	Active     bool       `json:"active"`
	CPU        float64    `json:"cpu"`
	Threshold  int        `json:"threshold"`
	Inflight   int64      `json:"inflight"`
	Capacity   float64    `json:"capacity"` // 估算的在途承载量（BDP）
	MaxPass    int64      `json:"maxPass"`  // 每秒最大完成数
	MinRTTMs   float64    `json:"minRttMs"`
	Dropped    int64      `json:"dropped"`
	LastDropAt *time.Time `json:"lastDropAt,omitempty"`
}

// Snapshot 本实例的完整运行态。
type Snapshot struct {
	Instance   string        `json:"instance"`
	Enabled    bool          `json:"enabled"`
	Revision   int64         `json:"revision"`
	StatsSince time.Time     `json:"statsSince"`
	Now        time.Time     `json:"now"`
	Totals     Counts        `json:"totals"`
	LastSec    Counts        `json:"lastSecond"`
	Exempt     int64         `json:"exempt"`
	Inflight   int64         `json:"inflight"`
	PeakRPS    int64         `json:"peakRps"`
	Latency    LatencyView   `json:"latency"`
	Overload   OverloadView  `json:"overload"`
	Rules      []RuleStats   `json:"rules"`
	Series     []SeriesPoint `json:"series"`
}

// NodeSummary 集群心跳里每个实例的摘要。
type NodeSummary struct {
	Instance   string    `json:"instance"`
	Host       string    `json:"host"`
	StartedAt  time.Time `json:"startedAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
	Revision   int64     `json:"revision"`
	Enabled    bool      `json:"enabled"`
	LastSec    Counts    `json:"lastSecond"`
	Totals     Counts    `json:"totals"`
	Inflight   int64     `json:"inflight"`
	CPU        float64   `json:"cpu"`
	Overloaded bool      `json:"overloaded"`
	P99Ms      float64   `json:"p99Ms"`
	Goroutines int       `json:"goroutines"`
	HeapMB     float64   `json:"heapMb"`
	Cores      int       `json:"cores"`
}

// MinuteSample 一分钟聚合（落库做历史曲线）。RuleID 为空表示实例整体。
type MinuteSample struct {
	Minute      time.Time `json:"minute"`
	RuleID      string    `json:"ruleId"`
	Counts      Counts    `json:"counts"`
	P50Ms       float64   `json:"p50Ms"`
	P99Ms       float64   `json:"p99Ms"`
	MaxInflight int64     `json:"maxInflight"`
	PeakRPS     int64     `json:"peakRps"`
}

// Event 状态变化事件（预热开始 / 饱和 / 过载保护触发与解除……）。
type Event struct {
	At       time.Time      `json:"at"`
	Kind     string         `json:"kind"`
	RuleID   string         `json:"ruleId,omitempty"`
	RuleName string         `json:"ruleName,omitempty"`
	Detail   map[string]any `json:"detail,omitempty"`
}

// 事件类型。
const (
	EventWarmupStart     = "warmup.start"
	EventWarmupSaturated = "warmup.saturated"
	EventWarmupSettled   = "warmup.settled"
	EventOverloadStart   = "overload.start"
	EventOverloadEnd     = "overload.end"
	EventRejectBurst     = "reject.burst" // 某规则一秒内拒绝数越过阈值
	EventConcurrencyDown = "concurrency.shrink"
)

// SimulateInput 模拟一次请求（不消耗配额）。
type SimulateInput struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Host    string            `json:"host"`
	IP      string            `json:"ip"`
	Query   string            `json:"query"`
	Headers map[string]string `json:"headers"`
}

// SimulateStep 一条规则的判定过程。
type SimulateStep struct {
	RuleID   string   `json:"ruleId"`
	RuleName string   `json:"ruleName"`
	Matched  bool     `json:"matched"`
	Miss     string   `json:"miss,omitempty"` // 未命中原因
	Mode     string   `json:"mode"`
	Keys     []SimKey `json:"keys,omitempty"`
	Outcome  string   `json:"outcome"` // pass | reject | observe | bypass | skip
	Detail   string   `json:"detail,omitempty"`
	Terminal bool     `json:"terminal"`
}

// SimKey 一项限速在这个请求上取到的键与余量。
type SimKey struct {
	Kind      string  `json:"kind"`
	Value     string  `json:"value"`
	Remaining float64 `json:"remaining"` // 估算的剩余令牌
	Capacity  int     `json:"capacity"`
	WouldPass bool    `json:"wouldPass"`
}

// SimulateResult 模拟结果。
type SimulateResult struct {
	Exempt       bool           `json:"exempt"`
	ExemptReason string         `json:"exemptReason,omitempty"`
	Class        string         `json:"class"`
	Verdict      string         `json:"verdict"` // allow | reject | observe
	Steps        []SimulateStep `json:"steps"`
}
