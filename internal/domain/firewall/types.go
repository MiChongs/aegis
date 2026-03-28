package firewall

import "time"

// ──────────────────────────────────────
// 严重性常量
// ──────────────────────────────────────

const (
	SeverityCritical = "critical"
	SeverityHigh     = "high"
	SeverityMedium   = "medium"
	SeverityLow      = "low"
)

// ReasonSeverity 将拦截原因映射为严重性等级
func ReasonSeverity(reason string) string {
	switch reason {
	case "waf_blocked":
		return SeverityCritical
	case "blocked_signature", "blocked_path":
		return SeverityHigh
	case "rate_limited", "blocked_cidr", "not_in_allowlist", "blocked_user_agent":
		return SeverityMedium
	case "banned_ip":
		return SeverityHigh
	case "blocked_method", "path_too_long", "query_too_long", "invalid_ip", "waf_processing_error":
		return SeverityLow
	default:
		return SeverityMedium
	}
}

// ──────────────────────────────────────
// 日志记录
// ──────────────────────────────────────

// FirewallLog 防火墙拦截日志记录
type FirewallLog struct {
	ID           int64             `json:"id"`
	RequestID    string            `json:"requestId"`
	IP           string            `json:"ip"`
	Method       string            `json:"method"`
	Path         string            `json:"path"`
	QueryString  string            `json:"queryString"`
	UserAgent    string            `json:"userAgent"`
	Headers      map[string]string `json:"headers,omitempty"`
	Reason       string            `json:"reason"`
	HTTPStatus   int               `json:"httpStatus"`
	ResponseCode int               `json:"responseCode"`
	WAFRuleID    *int              `json:"wafRuleId,omitempty"`
	WAFAction    string            `json:"wafAction,omitempty"`
	WAFData      string            `json:"wafData,omitempty"`
	Country      string            `json:"country"`
	CountryCode  string            `json:"countryCode"`
	Region       string            `json:"region"`
	City         string            `json:"city"`
	ISP          string            `json:"isp"`
	ASN          string            `json:"asn"`
	Timezone     string            `json:"timezone"`
	Latitude     *float64          `json:"latitude,omitempty"`
	Longitude    *float64          `json:"longitude,omitempty"`
	Severity     string            `json:"severity"`
	BlockedAt    time.Time         `json:"blockedAt"`
}

// ──────────────────────────────────────
// 查询过滤 & 分页
// ──────────────────────────────────────

// FirewallLogFilter 查询过滤条件
type FirewallLogFilter struct {
	StartTime   *time.Time
	EndTime     *time.Time
	IP          string
	Country     string
	Reason      string
	WAFRuleID   *int
	PathPattern string
	Severity    string
	Page        int
	PageSize    int
	SortBy      string // "blocked_at" | "ip"
	SortOrder   string // "asc" | "desc"
}

// FirewallLogPage 分页结果
type FirewallLogPage struct {
	Items      []FirewallLog `json:"items"`
	Total      int64         `json:"total"`
	Page       int           `json:"page"`
	PageSize   int           `json:"pageSize"`
	TotalPages int           `json:"totalPages"`
}

// ──────────────────────────────────────
// 聚合统计
// ──────────────────────────────────────

// RankedItem 排行榜项
type RankedItem struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

// TimeSeriesPoint 时间序列数据点（按严重性分维度）
type TimeSeriesPoint struct {
	Time     time.Time `json:"time"`
	Count    int64     `json:"count"`
	Critical int64     `json:"critical"`
	High     int64     `json:"high"`
	Medium   int64     `json:"medium"`
	Low      int64     `json:"low"`
}

// FirewallStats 聚合统计
type FirewallStats struct {
	TotalBlocked   int64             `json:"totalBlocked"`
	TopIPs         []RankedItem      `json:"topIPs"`
	TopCountries   []RankedItem      `json:"topCountries"`
	TopRules       []RankedItem      `json:"topRules"`
	TopPaths       []RankedItem      `json:"topPaths"`
	TopReasons     []RankedItem      `json:"topReasons"`
	SeverityCounts []RankedItem      `json:"severityCounts"`
	TimeSeries     []TimeSeriesPoint `json:"timeSeries"`
}

// ──────────────────────────────────────
// NATS 事件载荷
// ──────────────────────────────────────

// BlockEvent 防火墙拦截事件（用于 NATS 传输，不含 GeoIP）
type BlockEvent struct {
	RequestID    string            `json:"requestId"`
	IP           string            `json:"ip"`
	Method       string            `json:"method"`
	Path         string            `json:"path"`
	QueryString  string            `json:"queryString"`
	UserAgent    string            `json:"userAgent"`
	Headers      map[string]string `json:"headers,omitempty"`
	Reason       string            `json:"reason"`
	HTTPStatus   int               `json:"httpStatus"`
	ResponseCode int               `json:"responseCode"`
	WAFRuleID    *int              `json:"wafRuleId,omitempty"`
	WAFAction    string            `json:"wafAction,omitempty"`
	WAFData      string            `json:"wafData,omitempty"`
	Severity     string            `json:"severity"`
	BlockedAt    time.Time         `json:"blockedAt"`
}

// ──────────────────────────────────────
// IP 封禁
// ──────────────────────────────────────

// IPBan IP 封禁记录
type IPBan struct {
	ID           int64      `json:"id"`
	IP           string     `json:"ip"`
	Reason       string     `json:"reason"`
	Source       string     `json:"source"`       // manual | auto
	TriggerRule  string     `json:"triggerRule"`
	Severity     string     `json:"severity"`
	Duration     int64      `json:"duration"`     // 秒，0=永久
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
	Status       string     `json:"status"`       // active | expired | revoked
	RevokedBy    *int64     `json:"revokedBy,omitempty"`
	RevokedAt    *time.Time `json:"revokedAt,omitempty"`
	Country      string     `json:"country"`
	CountryCode  string     `json:"countryCode"`
	Region       string     `json:"region"`
	City         string     `json:"city"`
	ISP          string     `json:"isp"`
	TriggerCount int        `json:"triggerCount"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

// IPBanFilter 查询过滤条件
type IPBanFilter struct {
	IP       string
	Status   string // all | active | expired | revoked
	Source   string // all | manual | auto
	Page     int
	PageSize int
}

// IPBanPage 分页结果
type IPBanPage struct {
	Items      []IPBan `json:"items"`
	Total      int64   `json:"total"`
	Page       int     `json:"page"`
	PageSize   int     `json:"pageSize"`
	TotalPages int     `json:"totalPages"`
}

// AutoBanRule 自动封禁规则
type AutoBanRule struct {
	Name            string        // 规则名称
	Window          time.Duration // 时间窗口
	Threshold       int           // 阈值（窗口内拦截次数）
	BanDuration     time.Duration // 封禁时长（0=永久）
	Severity        string        // 封禁严重性
	SeverityFilter  []string      // 仅统计哪些 severity（空=全部）
}
