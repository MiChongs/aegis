package app

// TimeSeriesPoint 时间序列数据点
type TimeSeriesPoint struct {
	Date  string         `json:"date"`
	Count int64          `json:"count"`
	Extra map[string]any `json:"extra,omitempty"`
}

// DimensionPoint 维度分布数据点
type DimensionPoint struct {
	Label      string  `json:"label"`
	Count      int64   `json:"count"`
	Percentage float64 `json:"percentage"`
}

// FunnelStep 漏斗步骤
type FunnelStep struct {
	Step string  `json:"step"`
	Count int64  `json:"count"`
	Rate  float64 `json:"rate"` // 相对上一步的转化率
}

// RetentionRow 留存行（某一天注册的用户在后续天数的留存）
type RetentionRow struct {
	CohortDate string    `json:"cohortDate"` // 注册日期
	CohortSize int64     `json:"cohortSize"` // 当日注册数
	Retained   []int64   `json:"retained"`   // 第N天的留存人数
	Rates      []float64 `json:"rates"`      // 第N天的留存率
}

// RegistrationReport 注册报表
type RegistrationReport struct {
	Total  int64              `json:"total"`
	Series []TimeSeriesPoint  `json:"series"`
}

// LoginReport 登录报表
type LoginReport struct {
	TotalSuccess int64              `json:"totalSuccess"`
	TotalFailure int64              `json:"totalFailure"`
	Series       []TimeSeriesPoint  `json:"series"` // Extra: {success, failure}
}

// ActiveReport 活跃报表
type ActiveReport struct {
	DAU    []TimeSeriesPoint `json:"dau"`
	WAU    []TimeSeriesPoint `json:"wau,omitempty"`
	MAU    []TimeSeriesPoint `json:"mau,omitempty"`
}

// DeviceReport 设备报表
type DeviceReport struct {
	OS       []DimensionPoint `json:"os"`
	Browser  []DimensionPoint `json:"browser"`
	Platform []DimensionPoint `json:"platform"` // mobile/desktop/tablet
}

// PaymentReport 支付报表
type PaymentReport struct {
	TotalAmount int64              `json:"totalAmount"` // 分
	TotalOrders int64              `json:"totalOrders"`
	Series      []TimeSeriesPoint  `json:"series"`  // Extra: {amount, orders}
	Methods     []DimensionPoint   `json:"methods"` // 支付方式分布
}

// NotificationReport 通知报表
type NotificationReport struct {
	TotalSent int64              `json:"totalSent"`
	TotalRead int64              `json:"totalRead"`
	Series    []TimeSeriesPoint  `json:"series"`
	Types     []DimensionPoint   `json:"types"` // 通知类型分布
}

// RiskReport 风控报表
type RiskReport struct {
	TotalBlocked int64              `json:"totalBlocked"`
	Series       []TimeSeriesPoint  `json:"series"`
	Severity     []DimensionPoint   `json:"severity"` // 严重等级分布
	TopRules     []DimensionPoint   `json:"topRules"` // TOP 拦截规则
}

// ActivityReport 活动报表
type ActivityReport struct {
	TotalParticipants int64            `json:"totalParticipants"`
	TotalDraws        int64            `json:"totalDraws"`
	WinRate           float64          `json:"winRate"`
	Prizes            []DimensionPoint `json:"prizes"` // 奖品消耗分布
}

// FunnelReport 转化漏斗
type FunnelReport struct {
	Steps []FunnelStep `json:"steps"`
}

// RetentionReport 留存报表
type RetentionReport struct {
	Days []int          `json:"days"` // 如 [1,3,7,14,30]
	Rows []RetentionRow `json:"rows"`
}

// RegionReport 地域分布报表
type RegionReport struct {
	TopIPs []DimensionPoint `json:"topIPs"` // 登录 IP 分布 TOP N
}

// ChannelReport 渠道来源报表
type ChannelReport struct {
	Channels []DimensionPoint `json:"channels"` // 登录渠道分布（provider）
}

// ReportQuery 通用报表查询参数
type ReportQuery struct {
	AppID       int64  `json:"appId"`
	Start       string `json:"start"` // RFC3339
	End         string `json:"end"`
	Granularity string `json:"granularity,omitempty"` // day/week/month
	ActivityID  *int64 `json:"activityId,omitempty"`
}
