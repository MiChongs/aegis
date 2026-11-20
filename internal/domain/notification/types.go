package notification

import "time"

// Item 用户站内信（docs/notification-center.md「消息形状」）。
//
// 可空字段（readAt / action / image / expiresAt）为空时输出 null 而不是省略，
// 客户端按「字段恒在」声明模型即可。
type Item struct {
	ID            int64  `json:"id"`
	AppID         int64  `json:"appid"`
	UserID        *int64 `json:"userId,omitempty"`
	Type          string `json:"type"`
	Category      string `json:"category"`
	CategoryLabel string `json:"categoryLabel"`
	Title         string `json:"title"`
	Content       string `json:"content"`
	Level         string `json:"level"`
	// Status unread | read（回执表里的 delivered 对外一律叫 unread）
	Status    string         `json:"status"`
	Read      bool           `json:"read"`
	ReadAt    *time.Time     `json:"readAt"`
	Action    *Action        `json:"action"`
	Image     *string        `json:"image"`
	ExpiresAt *time.Time     `json:"expiresAt"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

// Counts 写操作与实时事件统一带回的未读计数。CategoryUnread 只含 n > 0 的分类，恒为对象（不为 null）。
type Counts struct {
	Unread         int64            `json:"unread"`
	CategoryUnread map[string]int64 `json:"categoryUnread"`
	// NextExpiry 未读里最早的过期时间，只用于算缓存寿命，不出网
	NextExpiry *time.Time `json:"-"`
}

type ListResponse struct {
	Items      []Item `json:"items"`
	Page       int    `json:"page"`
	Limit      int    `json:"limit"`
	Total      int64  `json:"total"`
	TotalPages int    `json:"totalPages"`
	Counts
	// NextExpiry 筛选范围内最早的过期时间，只用于算缓存寿命，不出网
	NextExpiry *time.Time `json:"-"`
}

type UserListQuery struct {
	Status   string `json:"status"`
	Category string `json:"category"`
	Type     string `json:"type"`
	Level    string `json:"level"`
	Page     int    `json:"page"`
	Limit    int    `json:"limit"`
}

// UnreadCountResult GET /notifications/unread-count。
type UnreadCountResult struct {
	Unread     int64            `json:"unread"`
	Categories map[string]int64 `json:"categories"`
}

// CategorySummary 消息中心首页的一行。
type CategorySummary struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Unread int64  `json:"unread"`
	Total  int64  `json:"total"`
	Latest *Item  `json:"latest"`
}

// Summary GET /notifications/summary：全部分类都列出，没有消息的 total=0、latest=null。
type Summary struct {
	Unread     int64             `json:"unread"`
	Categories []CategorySummary `json:"categories"`
}

// ReadResult 标为已读 / 未读。
type ReadResult struct {
	NotificationID int64 `json:"notificationId"`
	Read           bool  `json:"read"`
	Counts
}

// ReadAllResult 全部（或某一类）标为已读。
type ReadAllResult struct {
	ReadAll  bool   `json:"readAll"`
	Updated  int64  `json:"updated"`
	Category string `json:"category,omitempty"`
	Counts
}

type DeleteResult struct {
	AppID      int64  `json:"appid"`
	UserID     int64  `json:"userId"`
	Deleted    int64  `json:"deleted"`
	Status     string `json:"status,omitempty"`
	Category   string `json:"category,omitempty"`
	Type       string `json:"type,omitempty"`
	Level      string `json:"level,omitempty"`
	ClearedAll bool   `json:"clearedAll"`
	Counts
}

// DeleteBatchResult 批量删除。
type DeleteBatchResult struct {
	Requested int     `json:"requested"`
	Deleted   int64   `json:"deleted"`
	IDs       []int64 `json:"ids"`
	Counts
}

type ReadBatchResult struct {
	AppID     int64   `json:"appid"`
	UserID    int64   `json:"userId"`
	Requested int     `json:"requested"`
	Updated   int64   `json:"updated"`
	IDs       []int64 `json:"ids"`
	Counts
}

// PreferenceItem 一个分类的提醒开关。
type PreferenceItem struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Push    bool   `json:"push"`
	Mutable bool   `json:"mutable"`
}

// Preferences GET / PUT /notifications/preferences。
type Preferences struct {
	Categories []PreferenceItem `json:"categories"`
}

// DeliverInput NotificationService.Deliver 的入参：一切站内信写入的唯一形状。
type DeliverInput struct {
	AppID  int64
	UserID int64
	Type   string
	// Category 可空，空时由 CategoryOf(Type) 推出；给了就必须是目录里的 key
	Category  string
	Title     string
	Content   string
	Level     string
	Metadata  map[string]any
	Action    *Action
	Image     string
	ExpiresAt *time.Time
}

// 实时事件类型（/api/ws，信封同 realtime.Event）。
const (
	EventCreated      = "notification.created"
	EventRead         = "notification.read"
	EventDeleted      = "notification.deleted"
	EventStateChanged = "notification.state.changed"
)

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
	// Category 可空，管理员群发默认 announcement
	Category  string     `json:"category,omitempty"`
	Action    *Action    `json:"action,omitempty"`
	Image     string     `json:"image,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// AdminBulkSendResult 旧群发接口的结果。人数不超过 BulkSendInlineLimit 时在请求里发完（async=false，
// recipientIds 是真正收到的人）；更多的人转成后台任务（async=true，看 campaign 的进度）。
type AdminBulkSendResult struct {
	AppID        int64     `json:"appid"`
	Requested    int       `json:"requested"`
	Delivered    int       `json:"delivered"`
	RecipientIDs []int64   `json:"recipientIds"`
	Async        bool      `json:"async"`
	Campaign     *Campaign `json:"campaign"`
}

type AdminListQuery struct {
	Keyword  string `json:"keyword"`
	Category string `json:"category"`
	Type     string `json:"type"`
	Level    string `json:"level"`
	// Status read | unread，空不限
	Status string `json:"status"`
	// UserID 只看这个人的
	UserID int64 `json:"userId"`
	// CampaignID 只看这个群发任务的
	CampaignID int64 `json:"campaignId"`
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
}

type AdminExportQuery struct {
	Keyword    string `json:"keyword"`
	Category   string `json:"category"`
	Type       string `json:"type"`
	Level      string `json:"level"`
	Status     string `json:"status"`
	UserID     int64  `json:"userId"`
	CampaignID int64  `json:"campaignId"`
	Limit      int    `json:"limit"`
}

// Filter 管理端列表、导出、按条件删除共用的筛选条件。
func (q AdminListQuery) Filter() AdminExportQuery {
	return AdminExportQuery{Keyword: q.Keyword, Category: q.Category, Type: q.Type, Level: q.Level, Status: q.Status,
		UserID: q.UserID, CampaignID: q.CampaignID}
}

type AdminItem struct {
	ID     int64  `json:"id"`
	AppID  int64  `json:"appid"`
	UserID *int64 `json:"userId,omitempty"`
	// CampaignID 来自哪个群发任务，单条投递为 null
	CampaignID *int64 `json:"campaignId"`
	Account    string `json:"account,omitempty"`
	Nickname   string `json:"nickname,omitempty"`
	Type       string `json:"type"`
	Category   string `json:"category"`
	// CategoryLabel 分类中文名
	CategoryLabel string         `json:"categoryLabel"`
	Title         string         `json:"title"`
	Content       string         `json:"content"`
	Level         string         `json:"level"`
	Status        string         `json:"status"`
	Action        *Action        `json:"action"`
	Image         *string        `json:"image"`
	ExpiresAt     *time.Time     `json:"expiresAt"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	ReadAt        *time.Time     `json:"readAt,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
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
	Category      string  `json:"category,omitempty"`
	Type          string  `json:"type,omitempty"`
	Level         string  `json:"level,omitempty"`
}
