package httptransport

// ReportQueryParams 报表查询参数
type ReportQueryParams struct {
	Start       string `form:"start" binding:"required"`
	End         string `form:"end" binding:"required"`
	Granularity string `form:"granularity"`
	ActivityID  *int64 `form:"activityId"`
}

// ExportQueryParams 报表导出参数
type ExportQueryParams struct {
	Type   string `form:"type" binding:"required"`
	Start  string `form:"start" binding:"required"`
	End    string `form:"end" binding:"required"`
	Format string `form:"format"`
}
