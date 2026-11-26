package httptransport

// FirewallLogListRequest 防火墙事件列表查询参数
type FirewallLogListRequest struct {
	Page        int    `form:"page"`
	PageSize    int    `form:"pageSize"`
	StartTime   string `form:"startTime"` // RFC3339
	EndTime     string `form:"endTime"`   // RFC3339
	IP          string `form:"ip"`
	Country     string `form:"country"` // country_code
	Reason      string `form:"reason"`
	WAFRuleID   *int   `form:"wafRuleId"`
	PathPattern string `form:"pathPattern"`
	Severity    string `form:"severity"`  // low / medium / high / critical
	Action      string `form:"action"`    // block / observe / log
	Stage       string `form:"stage"`     // edge / payload / response
	Transport   string `form:"transport"` // plain / sealed / legacy
	AppKey      string `form:"appKey"`
	RuleRef     *int64 `form:"ruleRef"` // 自定义规则 ID
	Keyword     string `form:"keyword"` // 请求 ID / 路径 / UA / 规则名
	SortBy      string `form:"sortBy"`  // blocked_at | ip | score
	SortOrder   string `form:"sortOrder"`
}

// FirewallLogStatsRequest 防火墙日志统计查询参数
type FirewallLogStatsRequest struct {
	StartTime   string `form:"startTime"`   // RFC3339
	EndTime     string `form:"endTime"`     // RFC3339
	Granularity string `form:"granularity"` // minute | hour | day
}

// FirewallLogCleanupRequest 防火墙日志清理请求
type FirewallLogCleanupRequest struct {
	Before string `json:"before" binding:"required"` // RFC3339
}

// FirewallRuleRequest 新建 / 修改自定义规则
type FirewallRuleRequest struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	Enabled     *bool  `json:"enabled"`
	Priority    int    `json:"priority"`
	Expression  string `json:"expression" binding:"required"`
	Action      string `json:"action" binding:"required"` // allow / block / log / skip_waf
	BlockMode   string `json:"blockMode"`
	BanSeconds  int64  `json:"banSeconds"`
}

// FirewallToggleRequest 启停
type FirewallToggleRequest struct {
	Enabled bool `json:"enabled"`
}

// FirewallReorderRequest 规则排序（按数组顺序重排优先级）
type FirewallReorderRequest struct {
	IDs []int64 `json:"ids" binding:"required"`
}

// FirewallRuleValidateRequest 校验表达式（可带一个示例请求试算）
type FirewallRuleValidateRequest struct {
	Expression string `json:"expression" binding:"required"`
	Action     string `json:"action"`
}

// FirewallExclusionRequest 新建 / 修改 WAF 例外
type FirewallExclusionRequest struct {
	Name      string   `json:"name" binding:"required"`
	Note      string   `json:"note"`
	Enabled   *bool    `json:"enabled"`
	PathMatch string   `json:"pathMatch" binding:"required"` // prefix / exact / regex
	Path      string   `json:"path" binding:"required"`
	Methods   []string `json:"methods"`
	RuleIDs   []int    `json:"ruleIds"`
	Tags      []string `json:"tags"`
	Targets   []string `json:"targets"`
}

// FirewallSandboxRequest 测试台请求
type FirewallSandboxRequest struct {
	Method    string                    `json:"method"`
	URL       string                    `json:"url" binding:"required"`
	Headers   map[string]string         `json:"headers"`
	Body      string                    `json:"body"`
	IP        string                    `json:"ip"`
	Transport string                    `json:"transport"` // plain / sealed / legacy
	Draft     *FirewallExclusionRequest `json:"draftExclusion"`
}
