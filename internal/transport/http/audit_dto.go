package httptransport

type AuditLogQuery struct {
	Action    string `form:"action"`
	Resource  string `form:"resource"`
	AdminID   *int64 `form:"adminId"`
	Keyword   string `form:"keyword"`
	StartTime string `form:"startTime"`
	EndTime   string `form:"endTime"`
	Page      int    `form:"page"`
	Limit     int    `form:"limit"`
}
