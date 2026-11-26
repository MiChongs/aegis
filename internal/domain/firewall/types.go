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
	case "custom_rule":
		return SeverityHigh
	case "body_too_large", "waf_observed", "custom_log", "waf_outbound":
		return SeverityLow
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

	EventDetail
}

// 事件动作：拦截之外，观察模式「本该拦截」、自定义规则的记录动作、出站检测也落同一张表，
// 统计「拦了多少」的查询一律带 action = 'block'（见 docs/firewall.md）。
const (
	EventActionBlock   = "block"
	EventActionObserve = "observe"
	EventActionLog     = "log"
)

// 检测阶段：edge = 入口（IP、封禁、签名、自定义规则、限流，还没解密）；
// payload = 解密后的 WAF；response = 出站检测。
const (
	StageEdge     = "edge"
	StagePayload  = "payload"
	StageResponse = "response"
)

// 载荷来源：plain 明文；sealed = 接入网关 Transport v2；legacy = 旧 X-Aegis-Encrypted。
const (
	TransportPlain  = "plain"
	TransportSealed = "sealed"
	TransportLegacy = "legacy"
)

// MatchedRule WAF 事件里的一条命中规则（截断、脱敏后入库）。
type MatchedRule struct {
	ID       int      `json:"id"`
	Message  string   `json:"message"`
	Data     string   `json:"data,omitempty"`
	Severity string   `json:"severity"`
	Phase    int      `json:"phase"`
	Group    string   `json:"group"`
	Tags     []string `json:"tags,omitempty"`
	Targets  []string `json:"targets,omitempty"`
}

// EventDetail 规则集 v2 起事件多出来的字段（BlockEvent 与 FirewallLog 共用）。
type EventDetail struct {
	Action       string        `json:"action"`
	Stage        string        `json:"stage"`
	Transport    string        `json:"transport"`
	AppKey       string        `json:"appKey,omitempty"`
	AnomalyScore int           `json:"anomalyScore,omitempty"`
	Matches      []MatchedRule `json:"matches,omitempty"`
	RuleRef      int64         `json:"ruleRef,omitempty"`
	RuleName     string        `json:"ruleName,omitempty"`
	// BanSeconds 自定义规则要求的封禁时长（由 Worker 执行；0 = 不封，-1 = 永久）。
	BanSeconds int64 `json:"banSeconds,omitempty"`
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
	Action      string
	Stage       string
	Transport   string
	AppKey      string
	RuleRef     *int64
	Keyword     string
	Page        int
	PageSize    int
	SortBy      string // "blocked_at" | "ip" | "score"
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

	// 规则集 v2 新增：按动作、阶段、载荷来源、规则组、自定义规则、应用聚合；
	// TotalBlocked 只数 action = block，观察与记录单独计数。
	TotalObserved   int64        `json:"totalObserved"`
	TotalLogged     int64        `json:"totalLogged"`
	UniqueIPs       int64        `json:"uniqueIPs"`
	ActionCounts    []RankedItem `json:"actionCounts"`
	StageCounts     []RankedItem `json:"stageCounts"`
	TransportCounts []RankedItem `json:"transportCounts"`
	TopGroups       []RankedItem `json:"topGroups"`
	TopCustomRules  []RankedItem `json:"topCustomRules"`
	TopApps         []RankedItem `json:"topApps"`
	// PreviousBlocked 上一个等长时间窗的拦截数（总览卡片的环比）。
	PreviousBlocked int64 `json:"previousBlocked"`
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

	EventDetail
}

// ──────────────────────────────────────
// 自定义规则与 WAF 例外（规则集 v2）
// ──────────────────────────────────────

// Rule 自定义规则（表 firewall_rules）。表达式语法见 docs/firewall.md「自定义规则」。
type Rule struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Enabled     bool       `json:"enabled"`
	Priority    int        `json:"priority"`
	Expression  string     `json:"expression"`
	Action      string     `json:"action"`
	BlockMode   string     `json:"blockMode"`
	BanSeconds  int64      `json:"banSeconds"`
	Hits        int64      `json:"hits"`
	LastHitAt   *time.Time `json:"lastHitAt,omitempty"`
	CreatedBy   *int64     `json:"createdBy,omitempty"`
	UpdatedBy   *int64     `json:"updatedBy,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// WAFExclusion WAF 例外（表 firewall_waf_exclusions）。
type WAFExclusion struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Note      string    `json:"note"`
	Enabled   bool      `json:"enabled"`
	Position  int       `json:"position"`
	PathMatch string    `json:"pathMatch"`
	Path      string    `json:"path"`
	Methods   []string  `json:"methods"`
	RuleIDs   []int     `json:"ruleIds"`
	Tags      []string  `json:"tags"`
	Targets   []string  `json:"targets"`
	CreatedBy *int64    `json:"createdBy,omitempty"`
	UpdatedBy *int64    `json:"updatedBy,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ──────────────────────────────────────
// IP 封禁
// ──────────────────────────────────────

// IPBan IP 封禁记录
type IPBan struct {
	ID           int64      `json:"id"`
	IP           string     `json:"ip"`
	Reason       string     `json:"reason"`
	Source       string     `json:"source"` // manual | auto
	Mode         string     `json:"mode"`   // forbidden | silent_drop | tarpit | stealth_404 | teapot | connection_reset
	TriggerRule  string     `json:"triggerRule"`
	Severity     string     `json:"severity"`
	Duration     int64      `json:"duration"` // 秒，0=永久
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
	Status       string     `json:"status"` // active | expired | revoked
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
	// RevokeNote 解封备注（批量解封时填写的原因）
	RevokeNote string `json:"revokeNote,omitempty"`
	// DelayMs tarpit 模式的延迟毫秒（落库后服务重启同步回 Redis 不再丢）
	DelayMs   int    `json:"delayMs,omitempty"`
	UpdatedBy *int64 `json:"updatedBy,omitempty"`
}

// ──────────────────────────────────────
// 封禁模式（Ban Mode）
// ──────────────────────────────────────

// 封禁响应模式常量。
//
// 不同模式决定被封禁 IP 访问时的响应行为：
//   - BanModeForbidden     : 返回 HTTP 403 + JSON 错误信封（对合法运维最友好）
//   - BanModeSilentDrop    : 完全不响应（劫持连接直接 close，攻击者感知为"网络不通"）
//   - BanModeConnReset     : 硬 TCP Reset（同 silent_drop，但语义更强）
//   - BanModeTarpit        : 拖延 N 秒后再 403（消耗攻击者资源）
//   - BanModeStealth404    : 伪装 404（让扫描器认为资源不存在）
//   - BanModeStealth503    : 伪装 503 服务不可用
//   - BanModeTeapot        : 返回 418 (I'm a teapot)
//   - BanModeRateChoke     : 极端限速（延迟 2s 后返 429，Retry-After 5 分钟）
//   - BanModeRedirect      : 302 重定向到蜜罐 URL
//   - BanModeHoneypot      : 200 OK + 诱饵响应体（诱导攻击者以为成功）
//   - BanModeFakeEmpty     : 200 OK + 空 body（让爬虫以为是空资源）
//   - BanModeRandomError   : 随机 5xx（500/502/503/504），扰乱指纹
//   - BanModeSlowResponse  : 逐字节慢输出（拖住攻击者连接）
//   - BanModeRandomDelay   : 随机 1-15s 延迟后再 403（破坏时序分析）
//   - BanModeGone          : HTTP 410 Gone（永久移除语义）
//   - BanModeBandwidthChoke: 响应 403 时以极低带宽输出，慢慢"滴"
const (
	BanModeForbidden      = "forbidden"
	BanModeSilentDrop     = "silent_drop"
	BanModeConnReset      = "connection_reset"
	BanModeTarpit         = "tarpit"
	BanModeStealth404     = "stealth_404"
	BanModeStealth503     = "stealth_503"
	BanModeTeapot         = "teapot"
	BanModeRateChoke      = "rate_choke"
	BanModeRedirect       = "redirect"
	BanModeHoneypot       = "honeypot"
	BanModeFakeEmpty      = "fake_empty"
	BanModeRandomError    = "random_error"
	BanModeSlowResponse   = "slow_response"
	BanModeRandomDelay    = "random_delay"
	BanModeGone           = "gone"
	BanModeBandwidthChoke = "bandwidth_choke"

	// ── 增强型"恶心"模式 ──
	BanModeZipBomb          = "zip_bomb"          // 返回 gzip 压缩炸弹（~10KB 解压为 10GB），撑爆扫描器内存
	BanModeChunkedInfinite  = "chunked_infinite"  // Transfer-Encoding: chunked 无限流，每秒 1KB 持续至连接断开
	BanModeInfiniteRedirect = "infinite_redirect" // 302 → 随机子路径 → 又 302 → ... 无限重定向循环
	BanModeMirrorRequest    = "mirror_request"    // 把攻击者完整请求（method/URL/头/body）回显到响应体
	BanModeFakeLogin        = "fake_login"        // 伪装一个看似真实的登录页，诱导攻击者浪费时间
	BanModeRandomGarbage    = "random_garbage"    // 返回 512KB 完全随机的二进制 + 伪造 Content-Type
	BanModeCursedHeaders    = "cursed_headers"    // 返回 50+ 个伪造/矛盾响应头，破坏扫描器指纹识别
	BanModeJSONBomb         = "json_bomb"         // 深度嵌套 10000 层 JSON，爆掉大多数 parser 栈
	BanModeCookieBomb       = "cookie_bomb"       // 下发 80+ 大 Cookie，让攻击者后续每请求携带 100KB+ 头
	BanModeReverseSlowloris = "reverse_slowloris" // 服务端反向 slowloris：头部按字节 1s 滴一个，拖住连接
)

// AllBanModes 返回所有有效的封禁模式（用于 DTO / 前端枚举）。
func AllBanModes() []string {
	return []string{
		BanModeForbidden,
		BanModeSilentDrop,
		BanModeConnReset,
		BanModeTarpit,
		BanModeStealth404,
		BanModeStealth503,
		BanModeTeapot,
		BanModeRateChoke,
		BanModeRedirect,
		BanModeHoneypot,
		BanModeFakeEmpty,
		BanModeRandomError,
		BanModeSlowResponse,
		BanModeRandomDelay,
		BanModeGone,
		BanModeBandwidthChoke,
		// 增强恶心模式
		BanModeZipBomb,
		BanModeChunkedInfinite,
		BanModeInfiniteRedirect,
		BanModeMirrorRequest,
		BanModeFakeLogin,
		BanModeRandomGarbage,
		BanModeCursedHeaders,
		BanModeJSONBomb,
		BanModeCookieBomb,
		BanModeReverseSlowloris,
	}
}

// NormalizeBanMode 规范化模式字符串；未知/空时返回 fallback（默认 forbidden）。
func NormalizeBanMode(mode, fallback string) string {
	for _, m := range AllBanModes() {
		if mode == m {
			return mode
		}
	}
	if fallback == "" {
		return BanModeForbidden
	}
	// 防止递归：若 fallback 也非法，直接返回 forbidden
	for _, m := range AllBanModes() {
		if fallback == m {
			return fallback
		}
	}
	return BanModeForbidden
}

// ──────────────────────────────────────
// 地域封禁
// ──────────────────────────────────────

// GeoBanScope 地域封禁的作用域类型。
const (
	GeoScopeCountry = "country" // ISO 3166-1 alpha-2 (CN/US/RU/...)
	GeoScopeRegion  = "region"  // 省/州；格式建议 "国-省" 如 "CN-BJ"
	GeoScopeCity    = "city"
	GeoScopeASN     = "asn" // AS编号 "AS15169"
	GeoScopeISP     = "isp"
)

// AllGeoScopes 返回所有作用域类型。
func AllGeoScopes() []string {
	return []string{GeoScopeCountry, GeoScopeRegion, GeoScopeCity, GeoScopeASN, GeoScopeISP}
}

// GeoBan 地域封禁规则（平台级）。
// 注：检查顺序为 IP 精确封禁 → 地域规则；即 IP 优先。
type GeoBan struct {
	ID          int64      `json:"id"`
	ScopeType   string     `json:"scopeType"` // country / region / city / asn / isp
	ScopeValue  string     `json:"scopeValue"`
	Mode        string     `json:"mode"`
	Reason      string     `json:"reason"`
	Enabled     bool       `json:"enabled"`
	CreatedBy   *int64     `json:"createdBy,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	MatchCount  int64      `json:"matchCount"`
	LastMatchAt *time.Time `json:"lastMatchAt,omitempty"`
}

// GeoBanMutation 创建/更新 payload。
type GeoBanMutation struct {
	ScopeType  string
	ScopeValue string
	Mode       string
	Reason     string
	Enabled    bool
	ExpiresAt  *time.Time
}

// BanDecision 防火墙向中间件返回的封禁决策。
type BanDecision struct {
	Banned  bool   `json:"banned"`
	Mode    string `json:"mode"` // 见 BanMode* 常量
	Reason  string `json:"reason"`
	BanID   int64  `json:"banId,omitempty"`
	DelayMs int    `json:"delayMs,omitempty"` // tarpit 模式生效时的延迟毫秒数
}

// IPBanFilter 查询过滤条件（列表、批量解封、统计共用一套）
type IPBanFilter struct {
	IP      string
	Status  string // all | active | expired | revoked
	Source  string // all | manual | auto | risk | rule
	Country string // 国家代码
	Mode    string // 响应模式；"default" = 未指定（沿用全局默认）
	Keyword string // IP 前缀 / 原因 / 触发规则 / 运营商
	// ExpiringWithin 只看这么多秒内到期的（0 = 不限）
	ExpiringWithin int64
	// Permanent true = 只看永久、false = 只看有期限、nil = 不限
	Permanent *bool
	Sort      string // created_desc（默认）| created_asc | expires_asc | ip
	Page      int
	PageSize  int
}

// IPBanStats 封禁概况（封禁页签顶部）
type IPBanStats struct {
	Active       int64        `json:"active"`
	Permanent    int64        `json:"permanent"`
	ExpiringSoon int64        `json:"expiringSoon"` // 24 小时内到期
	Created24h   int64        `json:"created24h"`
	Revoked24h   int64        `json:"revoked24h"`
	Expired24h   int64        `json:"expired24h"`
	BySource     []RankedItem `json:"bySource"`
	ByMode       []RankedItem `json:"byMode"`
	ByCountry    []RankedItem `json:"byCountry"`
	Daily        []BanDaily   `json:"daily"` // 近 14 天
}

// BanDaily 每天新增与解除
type BanDaily struct {
	Day     string `json:"day"`
	Created int64  `json:"created"`
	Revoked int64  `json:"revoked"`
	Expired int64  `json:"expired"`
}

// BanBatchItem 批量封禁 / 解封的逐条结果
type BanBatchItem struct {
	IP     string `json:"ip"`
	BanID  int64  `json:"banId,omitempty"`
	Status string `json:"status"` // created | exists | invalid | failed | revoked | skipped
	Error  string `json:"error,omitempty"`
}

// BanBatchResult 批量操作结论
type BanBatchResult struct {
	Total     int            `json:"total"`
	Succeeded int            `json:"succeeded"`
	Skipped   int            `json:"skipped"`
	Failed    int            `json:"failed"`
	Items     []BanBatchItem `json:"items"`
	// Truncated 按筛选批量解封时超出单次上限，只处理了前一部分
	Truncated bool `json:"truncated,omitempty"`
}

// IPBanUpdate 修改一条生效中的封禁（nil = 不改）
type IPBanUpdate struct {
	Mode     *string
	Reason   *string
	Duration *int64 // 从现在起算的秒数，0 = 改为永久
	DelayMs  *int
}

// IPInspection 某个 IP 现在会不会被拦、被哪一层拦（IP 精确封禁 → 地域规则 → 地理围栏）
type IPInspection struct {
	IP          string      `json:"ip"`
	Blocked     bool        `json:"blocked"`
	Layer       string      `json:"layer,omitempty"` // ip_ban | geo_ban | geo_fence
	Decision    BanDecision `json:"decision"`
	ActiveBan   *IPBan      `json:"activeBan,omitempty"`
	History     []IPBan     `json:"history"`
	Blocks24h   int         `json:"blocks24h"`
	Blocks7d    int         `json:"blocks7d"`
	Country     string      `json:"country"`
	CountryCode string      `json:"countryCode"`
	Region      string      `json:"region"`
	City        string      `json:"city"`
	ISP         string      `json:"isp"`
	ASN         string      `json:"asn"`
	GeoRule     *GeoBan     `json:"geoRule,omitempty"`
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
	Name           string        // 规则名称
	Window         time.Duration // 时间窗口
	Threshold      int           // 阈值（窗口内拦截次数）
	BanDuration    time.Duration // 封禁时长（0=永久）
	Severity       string        // 封禁严重性
	SeverityFilter []string      // 仅统计哪些 severity（空=全部）
	ReasonFilter   []string      // 仅统计哪些拦截原因（空=全部）；优先于 SeverityFilter
}
