package httptransport

// FirewallLogListRequest 防火墙日志列表查询参数
type FirewallLogListRequest struct {
	Page        int    `form:"page"`
	PageSize    int    `form:"pageSize"`
	StartTime   string `form:"startTime"`   // RFC3339
	EndTime     string `form:"endTime"`     // RFC3339
	IP          string `form:"ip"`
	Country     string `form:"country"`     // country_code
	Reason      string `form:"reason"`
	WAFRuleID   *int   `form:"wafRuleId"`
	PathPattern string `form:"pathPattern"`
	Severity    string `form:"severity"`    // low / medium / high / critical
	SortBy      string `form:"sortBy"`      // blocked_at | ip
	SortOrder   string `form:"sortOrder"`   // asc | desc
}

// FirewallLogStatsRequest 防火墙日志统计查询参数
type FirewallLogStatsRequest struct {
	StartTime   string `form:"startTime"`   // RFC3339
	EndTime     string `form:"endTime"`     // RFC3339
	Granularity string `form:"granularity"` // hour | day
}

// FirewallLogCleanupRequest 防火墙日志清理请求
type FirewallLogCleanupRequest struct {
	Before string `json:"before" binding:"required"` // RFC3339
}
