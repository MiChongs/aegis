package system

import "time"

// AuditEntry 审计日志写入条目
type AuditEntry struct {
	AdminID    int64           `json:"adminId"`
	AdminName  string          `json:"adminName"`
	Action     string          `json:"action"`
	Resource   string          `json:"resource"`
	ResourceID string          `json:"resourceId"`
	Detail     string          `json:"detail"`
	Changes    map[string]any  `json:"changes,omitempty"`
	IP         string          `json:"ip"`
	UserAgent  string          `json:"userAgent"`
	Status     string          `json:"status"`
}

// AuditLog 审计日志查询结果
type AuditLog struct {
	ID         int64          `json:"id"`
	AdminID    int64          `json:"adminId"`
	AdminName  string         `json:"adminName"`
	Action     string         `json:"action"`
	Resource   string         `json:"resource"`
	ResourceID string         `json:"resourceId"`
	Detail     string         `json:"detail"`
	Changes    map[string]any `json:"changes,omitempty"`
	IP         string         `json:"ip"`
	UserAgent  string         `json:"userAgent"`
	Status     string         `json:"status"`
	CreatedAt  time.Time      `json:"createdAt"`
}

// AuditFilter 审计日志查询过滤条件
type AuditFilter struct {
	Action    string `json:"action"`
	Resource  string `json:"resource"`
	AdminID   *int64 `json:"adminId"`
	Keyword   string `json:"keyword"`
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
	Page      int    `json:"page"`
	Limit     int    `json:"limit"`
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
	TodayCount    int64              `json:"todayCount"`
	WeekCount     int64              `json:"weekCount"`
	TopAdmins     []AuditStatItem    `json:"topAdmins"`
	TopActions    []AuditStatItem    `json:"topActions"`
}

// AuditStatItem 统计项
type AuditStatItem struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Count int64  `json:"count"`
}
