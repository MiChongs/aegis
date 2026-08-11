package system

import "time"

// 审计严重度等级（severity）
const (
	AuditSeverityInfo     = "info"     // 普通只读/轻量操作
	AuditSeverityLow      = "low"      // 低风险变更
	AuditSeverityMedium   = "medium"   // 中等风险变更
	AuditSeverityHigh     = "high"     // 高风险变更（删除、权限、全局设置）
	AuditSeverityCritical = "critical" // 关键风险（超管行为、鉴权失败、数据库破坏性）
)

// 审计状态（status）
const (
	AuditStatusSuccess = "success" // 业务处理成功
	AuditStatusFailed  = "failed"  // 业务处理失败
	AuditStatusDenied  = "denied"  // 权限/策略拒绝
	AuditStatusBlocked = "blocked" // WAF / 限流拦截
)

// AuditEntry 审计日志写入条目
type AuditEntry struct {
	// 身份
	AdminID   int64  `json:"adminId"`
	AdminName string `json:"adminName"`
	AdminRole string `json:"adminRole"` // super_admin / custom role key
	SessionID string `json:"sessionId"` // 管理员会话 / token ID

	// 动作分类
	Action   string `json:"action"`   // 机器可读的操作 key（如 admin.user.update）
	Category string `json:"category"` // 业务域：auth/admin/user/app/storage/workflow/security/settings...
	Severity string `json:"severity"` // info / low / medium / high / critical

	// 目标资源
	Resource   string `json:"resource"`   // 资源类型（如 user、app、role）
	ResourceID string `json:"resourceId"` // 目标资源 ID

	// 人类可读摘要
	Summary string `json:"summary"` // 一句话摘要："更新用户 42 为 frozen"
	Detail  string `json:"detail"`  // 附加详情

	// 请求层
	RequestID  string `json:"requestId"`  // X-Request-Id
	TraceID    string `json:"traceId"`    // OpenTelemetry TraceID
	Method     string `json:"method"`     // HTTP Method
	Path       string `json:"path"`       // 实际请求路径
	Route      string `json:"route"`      // Gin 路由模板（含 :param）
	StatusCode int    `json:"statusCode"` // HTTP 状态码
	LatencyMs  int    `json:"latencyMs"`  // 处理耗时（毫秒）

	// 流量层
	RequestSize     int    `json:"requestSize"`     // 请求体字节数
	ResponseSize    int    `json:"responseSize"`    // 响应体字节数
	ResponseSnippet string `json:"responseSnippet"` // 响应摘要（失败时的错误 / 成功时的关键字段）

	// 终端
	IP        string `json:"ip"`
	Country   string `json:"country"`
	Region    string `json:"region"`
	City      string `json:"city"`
	ISP       string `json:"isp"`
	UserAgent string `json:"userAgent"`

	// 结果
	Status       string `json:"status"`       // success / failed / denied / blocked
	ErrorCode    string `json:"errorCode"`    // 业务错误码
	ErrorMessage string `json:"errorMessage"` // 错误详情

	// 结构化上下文
	Changes map[string]any `json:"changes,omitempty"` // 请求体 / query / route_params / before-after diff / 自定义字段
}

// AuditLog 审计日志查询结果（数据库读出）
type AuditLog struct {
	ID int64 `json:"id"`

	AdminID   int64  `json:"adminId"`
	AdminName string `json:"adminName"`
	AdminRole string `json:"adminRole"`
	SessionID string `json:"sessionId"`

	Action   string `json:"action"`
	Category string `json:"category"`
	Severity string `json:"severity"`

	Resource   string `json:"resource"`
	ResourceID string `json:"resourceId"`

	Summary string `json:"summary"`
	Detail  string `json:"detail"`

	RequestID  string `json:"requestId"`
	TraceID    string `json:"traceId"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	Route      string `json:"route"`
	StatusCode int    `json:"statusCode"`
	LatencyMs  int    `json:"latencyMs"`

	RequestSize     int    `json:"requestSize"`
	ResponseSize    int    `json:"responseSize"`
	ResponseSnippet string `json:"responseSnippet"`

	IP        string `json:"ip"`
	Country   string `json:"country"`
	Region    string `json:"region"`
	City      string `json:"city"`
	ISP       string `json:"isp"`
	UserAgent string `json:"userAgent"`

	Status       string `json:"status"`
	ErrorCode    string `json:"errorCode"`
	ErrorMessage string `json:"errorMessage"`

	Changes   map[string]any `json:"changes,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}

// AuditFilter 审计日志查询过滤条件
type AuditFilter struct {
	Action     string `json:"action"`
	Resource   string `json:"resource"`
	Category   string `json:"category"`
	Severity   string `json:"severity"`
	Status     string `json:"status"`
	AdminID    *int64 `json:"adminId"`
	StatusCode *int   `json:"statusCode"`
	IP         string `json:"ip"`
	Country    string `json:"country"`
	RequestID  string `json:"requestId"`
	TraceID    string `json:"traceId"`
	Keyword    string `json:"keyword"`
	StartTime  string `json:"startTime"`
	EndTime    string `json:"endTime"`
	Page       int    `json:"page"`
	Limit      int    `json:"limit"`
}

// AuditPage 审计日志分页结果
type AuditPage struct {
	Items []AuditLog `json:"items"`
	Total int64      `json:"total"`
	Page  int        `json:"page"`
	Limit int        `json:"limit"`
}

// AuditStats 审计统计
type AuditStats struct {
	TodayCount      int64           `json:"todayCount"`
	WeekCount       int64           `json:"weekCount"`
	FailedToday     int64           `json:"failedToday"`
	CriticalToday   int64           `json:"criticalToday"`
	AvgLatencyMs    int64           `json:"avgLatencyMs"`
	TopAdmins       []AuditStatItem `json:"topAdmins"`
	TopActions      []AuditStatItem `json:"topActions"`
	TopCategories   []AuditStatItem `json:"topCategories"`
	SeverityBuckets []AuditStatItem `json:"severityBuckets"`
}

// AuditStatItem 统计项
type AuditStatItem struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int64  `json:"count"`
}
