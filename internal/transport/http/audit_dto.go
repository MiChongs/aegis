package httptransport

type AuditLogQuery struct {
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
	Keyword    string `form:"keyword"`
	StartTime  string `form:"startTime"`
	EndTime    string `form:"endTime"`
	Page       int    `form:"page"`
	Limit      int    `form:"limit"`
}
