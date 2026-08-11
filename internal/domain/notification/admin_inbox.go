package notification

import "time"

// 管理员收件箱。
//
// 与本包里的 Item（应用用户站内信）是**两套独立的收件箱**：
// 用户站内信落 notifications（外键 users），管理员通知落 admin_notifications（外键 admin_accounts）。
// 两者主键空间不同，混用会导致跨租户串消息，因此类型也刻意分开，避免被误当成同一张表。

// 管理员通知级别
const (
	AdminLevelInfo     = "info"
	AdminLevelSuccess  = "success"
	AdminLevelWarning  = "warning"
	AdminLevelCritical = "critical"
)

// 管理员通知状态
const (
	AdminStatusUnread = "unread"
	AdminStatusRead   = "read"
)

// ValidAdminLevels 受支持的通知级别。
var ValidAdminLevels = map[string]struct{}{
	AdminLevelInfo: {}, AdminLevelSuccess: {}, AdminLevelWarning: {}, AdminLevelCritical: {},
}

// AdminInboxItem 管理员收件箱条目。
type AdminInboxItem struct {
	ID         int64          `json:"id"`
	AdminID    int64          `json:"adminId"`
	Type       string         `json:"type"`
	Title      string         `json:"title"`
	Content    string         `json:"content"`
	Level      string         `json:"level"`
	Status     string         `json:"status"`
	Resource   string         `json:"resource,omitempty"`
	ResourceID string         `json:"resourceId,omitempty"`
	// Link 为控制台内部相对路径（如 /tickets?id=42），前端直接用 next/link 跳转
	Link      string         `json:"link,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	ReadAt    *time.Time     `json:"readAt,omitempty"`
}

// AdminInboxQuery 收件箱查询条件。
type AdminInboxQuery struct {
	// Status 为空或 "all" 表示不过滤
	Status   string `json:"status"`
	Type     string `json:"type"`
	Level    string `json:"level"`
	Resource string `json:"resource"`
	Keyword  string `json:"keyword"`
	Page     int    `json:"page"`
	Limit    int    `json:"limit"`
}

// AdminInboxPage 分页结果，附带未读数供角标直接使用。
type AdminInboxPage struct {
	Items      []AdminInboxItem `json:"items"`
	Page       int              `json:"page"`
	Limit      int              `json:"limit"`
	Total      int64            `json:"total"`
	TotalPages int              `json:"totalPages"`
	Unread     int64            `json:"unread"`
}

// AdminInboxPush 一条待投递的管理员通知。
// 内部类型：Provider → Service → Repository 的写入 DTO，不出网，故不加 json tag。
type AdminInboxPush struct {
	AdminID    int64
	Type       string
	Title      string
	Content    string
	Level      string
	Resource   string
	ResourceID string
	Link       string
	DedupeKey  string
	Metadata   map[string]any
}

// AdminInboxMutationResult 批量已读/删除的结果。
type AdminInboxMutationResult struct {
	AdminID  int64 `json:"adminId"`
	Affected int64 `json:"affected"`
	Unread   int64 `json:"unread"`
}
