package system

import "time"

// 突发流量爬坡的管理端视图 / 更新载荷 / 运行态统计。
//
// 配置走逐字段 patch（与防火墙一致，全是标量开关）；
// 统计是运行期内存态，读取不打库。

// TrafficRampSettingsView 管理端读到的完整配置 + 热重载元信息。
type TrafficRampSettingsView struct {
	Enabled            bool     `json:"enabled"`
	BaselineRPS        int      `json:"baselineRps"`
	MaxRPS             int      `json:"maxRps"`
	RampStepPct        int      `json:"rampStepPct"`
	RampIntervalMs     int      `json:"rampIntervalMs"`
	CooldownSeconds    int      `json:"cooldownSeconds"`
	QueueSize          int      `json:"queueSize"`
	QueueTimeoutMs     int      `json:"queueTimeoutMs"`
	MaxConcurrent      int      `json:"maxConcurrent"`
	ExemptPathPrefixes []string `json:"exemptPathPrefixes"`
	ExemptAdmin        bool     `json:"exemptAdmin"`
	RetryAfterSeconds  int      `json:"retryAfterSeconds"`

	Source        string     `json:"source"`
	ReloadVersion uint64     `json:"reloadVersion"`
	ReloadedAt    time.Time  `json:"reloadedAt"`
	UpdatedBy     *int64     `json:"updatedBy,omitempty"`
	UpdatedAt     *time.Time `json:"updatedAt,omitempty"`
}

// TrafficRampSettingsPatch 逐字段更新载荷；nil 表示不修改。
type TrafficRampSettingsPatch struct {
	Enabled            *bool     `json:"enabled,omitempty"`
	BaselineRPS        *int      `json:"baselineRps,omitempty"`
	MaxRPS             *int      `json:"maxRps,omitempty"`
	RampStepPct        *int      `json:"rampStepPct,omitempty"`
	RampIntervalMs     *int      `json:"rampIntervalMs,omitempty"`
	CooldownSeconds    *int      `json:"cooldownSeconds,omitempty"`
	QueueSize          *int      `json:"queueSize,omitempty"`
	QueueTimeoutMs     *int      `json:"queueTimeoutMs,omitempty"`
	MaxConcurrent      *int      `json:"maxConcurrent,omitempty"`
	ExemptPathPrefixes *[]string `json:"exemptPathPrefixes,omitempty"`
	ExemptAdmin        *bool     `json:"exemptAdmin,omitempty"`
	RetryAfterSeconds  *int      `json:"retryAfterSeconds,omitempty"`
}

// 爬坡状态机的四个状态。
const (
	TrafficRampStateStable    = "stable"    // 上限 = 基线，需求未越线
	TrafficRampStateRamping   = "ramping"   // 检测到突发，上限逐步抬高中
	TrafficRampStateSaturated = "saturated" // 上限已到顶且仍供不应求
	TrafficRampStateCooldown  = "cooldown"  // 峰值退去，上限逐步回落中
)

// TrafficRampSeriesPoint 每秒一格的时间序列点，驱动控制台图表。
type TrafficRampSeriesPoint struct {
	Ts       int64   `json:"ts"` // unix 秒
	Arrivals int64   `json:"arrivals"`
	Admitted int64   `json:"admitted"`
	Queued   int64   `json:"queued"`
	Rejected int64   `json:"rejected"`
	Limit    float64 `json:"limit"` // 该秒的准入上限（RPS）
}

// TrafficRampStats 运行态快照：状态机 + 累计计数 + 即时水位 + 时间序列。
type TrafficRampStats struct {
	Enabled       bool    `json:"enabled"`
	State         string  `json:"state"`
	CurrentLimit  float64 `json:"currentLimit"` // 当前准入上限（RPS）
	BaselineRPS   int     `json:"baselineRps"`
	MaxRPS        int     `json:"maxRps"`
	Inflight      int64   `json:"inflight"`
	QueueDepth    int64   `json:"queueDepth"`
	QueueCapacity int     `json:"queueCapacity"`

	TotalArrivals        int64 `json:"totalArrivals"`
	TotalAdmitted        int64 `json:"totalAdmitted"`        // 直接放行
	TotalQueuedAdmitted  int64 `json:"totalQueuedAdmitted"`  // 排队后放行
	TotalRejectedRate    int64 `json:"totalRejectedRate"`    // 队列已满 / 不排队被拒
	TotalRejectedTimeout int64 `json:"totalRejectedTimeout"` // 排队超时被拒
	TotalRejectedLoad    int64 `json:"totalRejectedLoad"`    // 并发上限被拒
	TotalExempt          int64 `json:"totalExempt"`          // 免整形放行
	RampEvents           int64 `json:"rampEvents"`           // 进入爬坡的次数

	PeakArrivalRPS int64      `json:"peakArrivalRps"`
	PeakLimit      float64    `json:"peakLimit"`
	LastBurstAt    *time.Time `json:"lastBurstAt,omitempty"`
	StatsSince     time.Time  `json:"statsSince"`

	Series []TrafficRampSeriesPoint `json:"series"`
}
