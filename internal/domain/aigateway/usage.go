package aigateway

import (
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// ── 用户端用量统计（GET /ai/usage）──

// 统计时间段。日以下按小时分桶，其余按日；日界按应用设置的时区。
const (
	UsageRangeToday = "today"
	UsageRange24h   = "24h"
	UsageRange7d    = "7d"
	UsageRange30d   = "30d"
	UsageRange90d   = "90d"

	UsageBucketHour = "hour"
	UsageBucketDay  = "day"
)

// UsageQuery 一个用户在一个时间段里的用量统计条件。
type UsageQuery struct {
	AppID  int64
	UserID int64
	Range  string
	// From / To 半开区间 [From, To)。
	From     time.Time
	To       time.Time
	Bucket   string
	Location *time.Location
	// KeyID 只统计这把密钥；为空统计全部，0 表示登录令牌发起的调用。
	KeyID *int64
	// Model 只统计这个模型；空为全部。
	Model string
}

// ResolveUsageRange 把时间段名换成 [from, to) 与分桶粒度；认不出的按近 7 天。
func ResolveUsageRange(name string, now time.Time, loc *time.Location) (string, time.Time, time.Time, string) {
	if loc == nil {
		loc = time.UTC
	}
	local := now.In(loc)
	midnight := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	switch strings.ToLower(strings.TrimSpace(name)) {
	case UsageRangeToday:
		return UsageRangeToday, midnight, midnight.AddDate(0, 0, 1), UsageBucketHour
	case UsageRange24h:
		// 滚动 24 小时：从 23 小时前的整点到下一个整点，正好 24 个桶。
		end := time.Date(local.Year(), local.Month(), local.Day(), local.Hour(), 0, 0, 0, loc).Add(time.Hour)
		return UsageRange24h, end.Add(-24 * time.Hour), end, UsageBucketHour
	case UsageRange30d:
		return UsageRange30d, midnight.AddDate(0, 0, -29), midnight.AddDate(0, 0, 1), UsageBucketDay
	case UsageRange90d:
		return UsageRange90d, midnight.AddDate(0, 0, -89), midnight.AddDate(0, 0, 1), UsageBucketDay
	default:
		return UsageRange7d, midnight.AddDate(0, 0, -6), midnight.AddDate(0, 0, 1), UsageBucketDay
	}
}

// UsageTotals 时间段内的合计。金额单位为点。
type UsageTotals struct {
	Requests int64 `json:"requests"`
	// Succeeded 成功（含之后被退款的）；Aborted 流式中途断开、按已生成内容计费；
	// Failed 失败与过期，不扣费；Pending 进行中。
	Succeeded int64 `json:"succeeded"`
	Aborted   int64 `json:"aborted"`
	Failed    int64 `json:"failed"`
	Refunded  int64 `json:"refunded"`
	Pending   int64 `json:"pending"`
	Streamed  int64 `json:"streamed"`

	InputTokens  int64 `json:"inputTokens"`
	OutputTokens int64 `json:"outputTokens"`
	Images       int64 `json:"images"`
	Characters   int64 `json:"characters"`
	AudioMs      int64 `json:"audioMs"`

	Charged   decimal.Decimal `json:"charged"`
	Refund    decimal.Decimal `json:"refund"`
	Shortfall decimal.Decimal `json:"shortfall"`

	// 只算结束了的调用；首字延迟只算流式且有首字的。
	AvgLatencyMs    int `json:"avgLatencyMs"`
	P95LatencyMs    int `json:"p95LatencyMs"`
	AvgFirstTokenMs int `json:"avgFirstTokenMs"`
}

// UsagePoint 一个时间桶。
type UsagePoint struct {
	Time         time.Time       `json:"time"`
	Requests     int64           `json:"requests"`
	Failed       int64           `json:"failed"`
	InputTokens  int64           `json:"inputTokens"`
	OutputTokens int64           `json:"outputTokens"`
	Charged      decimal.Decimal `json:"charged"`
}

// UsageGroup 按某个维度（模型 / 密钥 / 接口）的分组合计。
type UsageGroup struct {
	Key string `json:"key"`
	// Label 展示名：密钥为名称（删掉的密钥为空，客户端用尾号），其余为空。
	Label        string          `json:"label,omitempty"`
	Hint         string          `json:"hint,omitempty"`
	Requests     int64           `json:"requests"`
	Failed       int64           `json:"failed"`
	InputTokens  int64           `json:"inputTokens"`
	OutputTokens int64           `json:"outputTokens"`
	Images       int64           `json:"images"`
	Charged      decimal.Decimal `json:"charged"`
	AvgLatencyMs int             `json:"avgLatencyMs"`
}

// Usage 一个用户在一个时间段里的用量。
type Usage struct {
	Range    string    `json:"range"`
	From     time.Time `json:"from"`
	To       time.Time `json:"to"`
	Bucket   string    `json:"bucket"`
	Timezone string    `json:"timezone"`

	Totals     UsageTotals   `json:"totals"`
	Series     []UsagePoint  `json:"series"`
	ByModel    []UsageGroup  `json:"byModel"`
	ByKey      []UsageGroup  `json:"byKey"`
	ByEndpoint []UsageGroup  `json:"byEndpoint"`
	BySource   []SourceTotal `json:"bySource"`
}

// FillUsageSeries 按粒度把 [from, to) 切成连续的桶，没有调用的桶补零。
// points 的 Time 须已对齐到桶起点（与 from 同一时区的整点 / 零点）。
func FillUsageSeries(points []UsagePoint, from, to time.Time, bucket string) []UsagePoint {
	byTime := make(map[int64]UsagePoint, len(points))
	for _, point := range points {
		byTime[point.Time.Unix()] = point
	}
	out := []UsagePoint{}
	for t := from; t.Before(to); {
		point, ok := byTime[t.Unix()]
		if !ok {
			point = UsagePoint{Charged: decimal.Zero}
		}
		point.Time = t
		out = append(out, point)
		if bucket == UsageBucketHour {
			t = t.Add(time.Hour)
		} else {
			t = t.AddDate(0, 0, 1)
		}
		if len(out) > 24*93 {
			break
		}
	}
	return out
}
