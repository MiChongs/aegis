package httptransport

type AuditLogQuery struct {
	// Kind：operation（默认视图，不含查看）/ read / write / export / auth；空表示全部
	Kind       string `form:"kind"`
	AppID      *int64 `form:"appId"`
	Action     string `form:"action"`
	Resource   string `form:"resource"`
	Category   string `form:"category"`
	Severity   string `form:"severity"`
	Status     string `form:"status"`
	StatusCode *int   `form:"statusCode"`
	AdminID    *int64 `form:"adminId"`
	IP         string `form:"ip"`
	Country    string `form:"country"`
	RequestID  string `form:"requestId"`
	TraceID    string `form:"traceId"`
	SessionID  string `form:"sessionId"`
	Keyword    string `form:"keyword"`
	StartTime  string `form:"startTime"`
	EndTime    string `form:"endTime"`
	Page       int    `form:"page"`
	Limit      int    `form:"limit"`
}
