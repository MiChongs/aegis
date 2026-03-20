package notification

import "time"

type Item struct {
	ID        int64          `json:"id"`
	AppID     int64          `json:"appid"`
	UserID    *int64         `json:"userId,omitempty"`
	Type      string         `json:"type"`
	Title     string         `json:"title"`
	Content   string         `json:"content"`
	Level     string         `json:"level"`
	Status    string         `json:"status"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	ReadAt    *time.Time     `json:"readAt,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

type ListResponse struct {
	Items      []Item `json:"items"`
	Page       int    `json:"page"`
	Limit      int    `json:"limit"`
	Total      int64  `json:"total"`
	TotalPages int    `json:"totalPages"`
	Unread     int64  `json:"unread"`
}

type UserListQuery struct {
	Status string `json:"status"`
	Type   string `json:"type"`
	Level  string `json:"level"`
	Page   int    `json:"page"`
	Limit  int    `json:"limit"`
}

type DeleteResult struct {
	AppID      int64  `json:"appid"`
	UserID     int64  `json:"userId"`
	Deleted    int64  `json:"deleted"`
	Status     string `json:"status,omitempty"`
	Type       string `json:"type,omitempty"`
	Level      string `json:"level,omitempty"`
	ClearedAll bool   `json:"clearedAll"`
}

type ReadBatchResult struct {
	AppID     int64   `json:"appid"`
	UserID    int64   `json:"userId"`
	Requested int     `json:"requested"`
	Updated   int64   `json:"updated"`
	IDs       []int64 `json:"ids"`
}

type AdminBulkSendCommand struct {
	UserIDs  []int64        `json:"userIds,omitempty"`
	Keyword  string         `json:"keyword,omitempty"`
	Enabled  *bool          `json:"enabled,omitempty"`
	Limit    int            `json:"limit"`
	Type     string         `json:"type"`
	Title    string         `json:"title"`
	Content  string         `json:"content"`
	Level    string         `json:"level"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type AdminBulkSendResult struct {
	AppID        int64   `json:"appid"`
	Requested    int     `json:"requested"`
	Delivered    int     `json:"delivered"`
	RecipientIDs []int64 `json:"recipientIds"`
}

type AdminListQuery struct {
	Keyword string `json:"keyword"`
	Type    string `json:"type"`
	Level   string `json:"level"`
	Page    int    `json:"page"`
	Limit   int    `json:"limit"`
}

type AdminExportQuery struct {
	Keyword string `json:"keyword"`
	Type    string `json:"type"`
	Level   string `json:"level"`
	Limit   int    `json:"limit"`
}

type AdminItem struct {
	ID        int64          `json:"id"`
	AppID     int64          `json:"appid"`
	UserID    *int64         `json:"userId,omitempty"`
	Account   string         `json:"account,omitempty"`
	Nickname  string         `json:"nickname,omitempty"`
	Type      string         `json:"type"`
	Title     string         `json:"title"`
	Content   string         `json:"content"`
	Level     string         `json:"level"`
	Status    string         `json:"status"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	ReadAt    *time.Time     `json:"readAt,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

type AdminListResponse struct {
	Items      []AdminItem `json:"items"`
	Page       int         `json:"page"`
	Limit      int         `json:"limit"`
	Total      int64       `json:"total"`
	TotalPages int         `json:"totalPages"`
}

type AdminDeleteResult struct {
	AppID         int64   `json:"appid"`
	Requested     int     `json:"requested"`
	Deleted       int64   `json:"deleted"`
	AffectedUsers []int64 `json:"affectedUsers"`
}

type AdminDeleteFilterResult struct {
	AppID         int64   `json:"appid"`
	Requested     int     `json:"requested"`
	Deleted       int64   `json:"deleted"`
	AffectedUsers []int64 `json:"affectedUsers"`
	Keyword       string  `json:"keyword,omitempty"`
	Type          string  `json:"type,omitempty"`
	Level         string  `json:"level,omitempty"`
}
