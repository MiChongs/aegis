package system

import "time"

// Announcement 全站系统公告（仅管理员后台可见）
type Announcement struct {
	ID          int64          `json:"id"`
	AdminID     int64          `json:"adminId"`
	AdminName   string         `json:"adminName,omitempty"` // JOIN admin_accounts
	Type        string         `json:"type"`                // info / warning / maintenance / update / security
	Title       string         `json:"title"`
	Content     string         `json:"content"`
	Level       string         `json:"level"`  // normal / important / critical
	Pinned      bool           `json:"pinned"`
	Status      string         `json:"status"` // draft / published / archived
	PublishedAt *time.Time     `json:"publishedAt,omitempty"`
	ExpiresAt   *time.Time     `json:"expiresAt,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

// AnnouncementMutation 创建/更新请求（指针字段支持部分更新）
type AnnouncementMutation struct {
	ID      int64
	AdminID int64
	Type    *string
	Title   *string
	Content *string
	Level   *string
	Pinned  *bool
	Status  *string
	// ExpiresAt 以字符串传入，空串表示不限
	ExpiresAt *string
	Metadata  map[string]any
}

// AnnouncementListQuery 列表查询参数
type AnnouncementListQuery struct {
	Status string
	Type   string
	Level  string
	Page   int
	Limit  int
}

// AnnouncementListResult 分页结果
type AnnouncementListResult struct {
	Items      []Announcement `json:"items"`
	Page       int            `json:"page"`
	Limit      int            `json:"limit"`
	Total      int64          `json:"total"`
	TotalPages int            `json:"totalPages"`
}
