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

	// 可读性（2026-10 重构）
	Kind       string `json:"kind"`       // read / write / export / auth，见 auditcatalog
	AppID      int64  `json:"appId"`      // 操作所在应用（路径带 :appkey 时）
	AppName    string `json:"appName"`    // 写入时的应用名称，应用改名或删除后仍可读
	TargetName string `json:"targetName"` // 操作对象的名称（上传的文件名、handler 指定的对象名）
	CatalogRev int    `json:"-"`          // 写入时的目录版本
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

	Kind       string `json:"kind"`
	AppID      int64  `json:"appId,omitempty"`
	AppName    string `json:"appName,omitempty"`
	TargetName string `json:"targetName,omitempty"`

	// 以下为读取时由操作目录与请求信息推导的展示字段，不落库
	OperationName string `json:"operationName"`         // 「上传安装包」
	ModuleLabel   string `json:"moduleLabel"`           // 「发布中心」
	TargetType    string `json:"targetType,omitempty"`  // 「安装包」
	TargetLabel   string `json:"targetLabel,omitempty"` // 「voyage-1.0.apk」或「#12」
	Browser       string `json:"browser,omitempty"`     // 「Chrome 141」
	OS            string `json:"os,omitempty"`          // 「macOS」
	Location      string `json:"location,omitempty"`    // 「中国 上海」
}

// AuditFilter 审计日志查询过滤条件
type AuditFilter struct {
	// Kind：operation（除查看外的全部，默认视图）/ read / write / export / auth；空表示不限
	Kind       string `json:"kind"`
	AppID      *int64 `json:"appId"`
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
	SessionID  string `json:"sessionId"` // 同一次登录会话里的全部操作
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

// AuditOverview 审计总览（控制台顶部）。「操作」不含查看类请求。
type AuditOverview struct {
	TodayOperations int64           `json:"todayOperations"`
	TodayFailed     int64           `json:"todayFailed"`
	TodayHighRisk   int64           `json:"todayHighRisk"`
	TodayReads      int64           `json:"todayReads"`
	WeekOperations  int64           `json:"weekOperations"`
	ActiveAdmins    int64           `json:"activeAdmins"` // 近 7 天有过操作的管理员数
	Trend           []AuditTrendDay `json:"trend"`        // 近 14 天
	TopAdmins       []AuditStatItem `json:"topAdmins"`    // 近 7 天操作最多
	TopModules      []AuditStatItem `json:"topModules"`   // 近 7 天操作最多的模块
}

type AuditTrendDay struct {
	Day        string `json:"day"`
	Operations int64  `json:"operations"`
	Failed     int64  `json:"failed"`
	HighRisk   int64  `json:"highRisk"`
}

// AuditFacets 筛选项：模块与操作者，带近 30 天的计数。
type AuditFacets struct {
	Modules []AuditStatItem `json:"modules"`
	Admins  []AuditStatItem `json:"admins"`
	Apps    []AuditStatItem `json:"apps"`
}

// AuditStatItem 统计项
type AuditStatItem struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int64  `json:"count"`
}
