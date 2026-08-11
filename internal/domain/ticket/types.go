package ticket

import "time"

// ─────────────── 枚举常量 ───────────────

// 工单状态机
//
//	open ──► processing ──► resolved ──► closed
//	  │           │  ▲          │
//	  │           ▼  │          └──► reopen 回到 processing
//	  │      pending_user / pending_third_party
//	  └──► cancelled（提单人撤单）
const (
	StatusOpen              = "open"                // 待受理
	StatusProcessing        = "processing"          // 处理中
	StatusPendingUser       = "pending_user"        // 等待提单人补充
	StatusPendingThirdParty = "pending_third_party" // 等待第三方
	StatusResolved          = "resolved"            // 已解决（待确认）
	StatusClosed            = "closed"              // 已关闭
	StatusCancelled         = "cancelled"           // 已撤销
)

// 优先级
const (
	PriorityLow    = "low"
	PriorityNormal = "normal"
	PriorityHigh   = "high"
	PriorityUrgent = "urgent"
)

// 提单来源
const (
	SourceConsole = "console"
	SourceApp     = "app"
	SourceAPI     = "api"
	SourceEmail   = "email"
	SourceBot     = "bot"
	SourceImport  = "import"
)

// 提单人类型
const (
	RequesterUser  = "user"
	RequesterAdmin = "admin"
)

// 消息作者类型
const (
	AuthorRequester = "requester"
	AuthorAgent     = "agent"
	AuthorSystem    = "system"
)

// SLA 状态
const (
	SLAOnTime   = "ontime"
	SLAWarning  = "warning"
	SLABreached = "breached"
	SLAPaused   = "paused"
	SLAMet      = "met"
)

// 时间线事件 key
const (
	EventCreated         = "created"
	EventAssigned        = "assigned"
	EventGroupChanged    = "group_changed"
	EventStatusChanged   = "status_changed"
	EventPriorityChanged = "priority_changed"
	EventCategoryChanged = "category_changed"
	EventReplied         = "replied"
	EventInternalNote    = "internal_note"
	EventReopened        = "reopened"
	EventRated           = "rated"
	EventTagsChanged     = "tags_changed"
	EventWatcherAdded    = "watcher_added"
	EventWatcherRemoved  = "watcher_removed"
	EventSLAWarning      = "sla_warning"
	EventSLABreached     = "sla_breached"
	EventMerged          = "merged"
)

// ValidStatuses 是否为受支持的工单状态。
var ValidStatuses = map[string]struct{}{
	StatusOpen: {}, StatusProcessing: {}, StatusPendingUser: {},
	StatusPendingThirdParty: {}, StatusResolved: {}, StatusClosed: {}, StatusCancelled: {},
}

// ValidPriorities 是否为受支持的优先级。
var ValidPriorities = map[string]struct{}{
	PriorityLow: {}, PriorityNormal: {}, PriorityHigh: {}, PriorityUrgent: {},
}

// PriorityWeight 优先级权重，用于排序与「不低于某优先级」的订阅过滤。
var PriorityWeight = map[string]int{
	PriorityLow: 1, PriorityNormal: 2, PriorityHigh: 3, PriorityUrgent: 4,
}

// ActiveStatuses 未终结状态（仍需处理人跟进）。
var ActiveStatuses = []string{StatusOpen, StatusProcessing, StatusPendingUser, StatusPendingThirdParty}

// IsTerminal 是否为终态（终态不再计 SLA、默认禁止回复）。
func IsTerminal(status string) bool {
	return status == StatusClosed || status == StatusCancelled
}

// ─────────────── 核心实体 ───────────────

// Ticket 工单主体。列表与详情共用；列表场景不填充 Messages/Events。
type Ticket struct {
	ID       int64  `json:"id"`
	TicketNo string `json:"ticketNo"`
	AppID    int64  `json:"appid"`
	AppName  string `json:"appName,omitempty"`

	RequesterType    string `json:"requesterType"`
	RequesterUserID  *int64 `json:"requesterUserId,omitempty"`
	RequesterAdminID *int64 `json:"requesterAdminId,omitempty"`
	RequesterName    string `json:"requesterName"`
	RequesterContact string `json:"requesterContact,omitempty"`

	CategoryID   *int64 `json:"categoryId,omitempty"`
	CategoryName string `json:"categoryName,omitempty"`
	Title        string `json:"title"`
	Status       string `json:"status"`
	Priority     string `json:"priority"`
	Source       string `json:"source"`

	AssigneeAdminID *int64 `json:"assigneeAdminId,omitempty"`
	AssigneeName    string `json:"assigneeName,omitempty"`
	GroupID         *int64 `json:"groupId,omitempty"`
	GroupName       string `json:"groupName,omitempty"`

	SLAPolicyID        *int64     `json:"slaPolicyId,omitempty"`
	FirstResponseDueAt *time.Time `json:"firstResponseDueAt,omitempty"`
	ResolveDueAt       *time.Time `json:"resolveDueAt,omitempty"`
	FirstRespondedAt   *time.Time `json:"firstRespondedAt,omitempty"`
	ResolvedAt         *time.Time `json:"resolvedAt,omitempty"`
	ClosedAt           *time.Time `json:"closedAt,omitempty"`
	SLAState           string     `json:"slaState"`

	MessageCount    int        `json:"messageCount"`
	LastMessageAt   *time.Time `json:"lastMessageAt,omitempty"`
	LastMessageRole string     `json:"lastMessageRole,omitempty"`
	ReopenedCount   int        `json:"reopenedCount"`

	Rating        *int16     `json:"rating,omitempty"`
	RatingComment string     `json:"ratingComment,omitempty"`
	RatedAt       *time.Time `json:"ratedAt,omitempty"`

	Tags     []string       `json:"tags"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Locked   bool           `json:"locked"`

	CreatedByAdminID *int64    `json:"createdByAdminId,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`

	// 详情场景填充
	Messages    []Message    `json:"messages,omitempty"`
	Events      []Event      `json:"events,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
	Watchers    []Watcher    `json:"watchers,omitempty"`

	// 当前访问者对本工单可执行的操作（前端据此决定按钮显隐，避免出现「点了才 403」）
	Permissions *ActionSet `json:"permissions,omitempty"`
}

// Message 工单会话消息。
type Message struct {
	ID            int64          `json:"id"`
	TicketID      int64          `json:"ticketId"`
	AuthorType    string         `json:"authorType"`
	AuthorUserID  *int64         `json:"authorUserId,omitempty"`
	AuthorAdminID *int64         `json:"authorAdminId,omitempty"`
	AuthorName    string         `json:"authorName"`
	Internal      bool           `json:"internal"`
	Content       string         `json:"content"`
	ContentType   string         `json:"contentType"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	EditedAt      *time.Time     `json:"editedAt,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
	Attachments   []Attachment   `json:"attachments,omitempty"`
}

// Attachment 附件。DownloadURL 为即时换取的带票据代理地址，不落库。
// TicketID 为 nil 表示已上传但尚未归属工单（提单表单先传附件、建单时回填）。
type Attachment struct {
	ID             int64     `json:"id"`
	TicketID       *int64    `json:"ticketId,omitempty"`
	MessageID      *int64    `json:"messageId,omitempty"`
	FileName       string    `json:"fileName"`
	ContentType    string    `json:"contentType"`
	SizeBytes      int64     `json:"sizeBytes"`
	StorageRef     string    `json:"-"`
	DownloadURL    string    `json:"downloadUrl,omitempty"`
	UploadedByType string    `json:"uploadedByType"`
	UploadedByID   *int64    `json:"uploadedById,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
}

// Event 时间线条目。
type Event struct {
	ID        int64          `json:"id"`
	TicketID  int64          `json:"ticketId"`
	Event     string         `json:"event"`
	ActorType string         `json:"actorType"`
	ActorID   *int64         `json:"actorId,omitempty"`
	ActorName string         `json:"actorName"`
	FromValue string         `json:"fromValue,omitempty"`
	ToValue   string         `json:"toValue,omitempty"`
	Summary   string         `json:"summary"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}

// Watcher 关注人。
type Watcher struct {
	TicketID    int64     `json:"ticketId"`
	AdminID     int64     `json:"adminId"`
	Account     string    `json:"account,omitempty"`
	DisplayName string    `json:"displayName,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

// ─────────────── 配置实体 ───────────────

// Category 工单分类。
type Category struct {
	ID              int64       `json:"id"`
	AppID           int64       `json:"appid"`
	ParentID        *int64      `json:"parentId,omitempty"`
	Key             string      `json:"key"`
	Name            string      `json:"name"`
	Description     string      `json:"description"`
	DefaultPriority string      `json:"defaultPriority"`
	DefaultGroupID  *int64      `json:"defaultGroupId,omitempty"`
	SLAPolicyID     *int64      `json:"slaPolicyId,omitempty"`
	FormSchema      []FormField `json:"formSchema"`
	UserSubmittable bool        `json:"userSubmittable"`
	Sort            int         `json:"sort"`
	Enabled         bool        `json:"enabled"`
	CreatedAt       time.Time   `json:"createdAt"`
	UpdatedAt       time.Time   `json:"updatedAt"`
}

// FormField 分类自定义表单字段定义，前端据此动态渲染提单表单。
type FormField struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Type        string   `json:"type"` // text / textarea / select / number / date / switch
	Required    bool     `json:"required"`
	Placeholder string   `json:"placeholder,omitempty"`
	Options     []string `json:"options,omitempty"`
}

// Group 处理组。
type Group struct {
	ID             int64         `json:"id"`
	AppID          int64         `json:"appid"`
	Key            string        `json:"key"`
	Name           string        `json:"name"`
	Description    string        `json:"description"`
	AssignStrategy string        `json:"assignStrategy"` // manual / round_robin / least_open
	Enabled        bool          `json:"enabled"`
	Members        []GroupMember `json:"members,omitempty"`
	MemberCount    int           `json:"memberCount"`
	OpenCount      int64         `json:"openCount"`
	CreatedAt      time.Time     `json:"createdAt"`
	UpdatedAt      time.Time     `json:"updatedAt"`
}

// GroupMember 处理组成员。
type GroupMember struct {
	GroupID     int64     `json:"groupId"`
	AdminID     int64     `json:"adminId"`
	Account     string    `json:"account,omitempty"`
	DisplayName string    `json:"displayName,omitempty"`
	Avatar      string    `json:"avatar,omitempty"`
	Role        string    `json:"role"` // agent / leader
	OpenCount   int64     `json:"openCount"`
	CreatedAt   time.Time `json:"createdAt"`
}

// SLAPolicy SLA 策略。
type SLAPolicy struct {
	ID                   int64          `json:"id"`
	AppID                int64          `json:"appid"`
	Name                 string         `json:"name"`
	Description          string         `json:"description"`
	FirstResponseMinutes map[string]int `json:"firstResponseMinutes"`
	ResolveMinutes       map[string]int `json:"resolveMinutes"`
	BusinessHours        *BusinessHours `json:"businessHours,omitempty"`
	WarnRatio            float64        `json:"warnRatio"`
	Enabled              bool           `json:"enabled"`
	CreatedAt            time.Time      `json:"createdAt"`
	UpdatedAt            time.Time      `json:"updatedAt"`
}

// BusinessHours 工作时间窗口。为空表示 7×24 计时。
type BusinessHours struct {
	Timezone string `json:"timezone"`
	Days     []int  `json:"days"` // 1=周一 … 7=周日
	Start    string `json:"start"`
	End      string `json:"end"`
}

// QuickReply 快捷回复模板。
type QuickReply struct {
	ID           int64     `json:"id"`
	AppID        int64     `json:"appid"`
	Title        string    `json:"title"`
	Content      string    `json:"content"`
	CategoryID   *int64    `json:"categoryId,omitempty"`
	OwnerAdminID *int64    `json:"ownerAdminId,omitempty"`
	UsageCount   int64     `json:"usageCount"`
	Sort         int       `json:"sort"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// ─────────────── 查询 / 命令 ───────────────

// ListQuery 管理端列表查询条件。Scope 由服务层根据会话注入，不由前端传入。
type ListQuery struct {
	AppID      *int64   `json:"appid,omitempty"`
	Statuses   []string `json:"statuses,omitempty"`
	Priorities []string `json:"priorities,omitempty"`
	CategoryID *int64   `json:"categoryId,omitempty"`
	GroupID    *int64   `json:"groupId,omitempty"`
	AssigneeID *int64   `json:"assigneeId,omitempty"`
	// Unassigned=true 时只看未指派工单（与 AssigneeID 互斥）
	Unassigned    bool       `json:"unassigned,omitempty"`
	RequesterID   *int64     `json:"requesterId,omitempty"`
	Keyword       string     `json:"keyword,omitempty"`
	Tags          []string   `json:"tags,omitempty"`
	SLAState      string     `json:"slaState,omitempty"`
	OverdueOnly   bool       `json:"overdueOnly,omitempty"`
	CreatedFrom   *time.Time `json:"createdFrom,omitempty"`
	CreatedTo     *time.Time `json:"createdTo,omitempty"`
	Rated         *bool      `json:"rated,omitempty"`
	SortBy        string     `json:"sortBy,omitempty"`  // updated / created / priority / due
	SortDir       string     `json:"sortDir,omitempty"` // asc / desc
	Page          int        `json:"page"`
	Limit         int        `json:"limit"`
	IncludeClosed bool       `json:"includeClosed,omitempty"`
}

// ListResponse 分页结果。
type ListResponse struct {
	Items      []Ticket `json:"items"`
	Page       int      `json:"page"`
	Limit      int      `json:"limit"`
	Total      int64    `json:"total"`
	TotalPages int      `json:"totalPages"`
	// Scope 回传当前会话的数据可见范围，前端可据此提示「你只看得到指派给自己的工单」
	Scope *ScopeInfo `json:"scope,omitempty"`
}

// CreateCommand 创建工单。
type CreateCommand struct {
	AppID            int64          `json:"appid"`
	RequesterType    string         `json:"requesterType"`
	RequesterUserID  *int64         `json:"requesterUserId,omitempty"`
	RequesterAdminID *int64         `json:"requesterAdminId,omitempty"`
	RequesterName    string         `json:"requesterName"`
	RequesterContact string         `json:"requesterContact"`
	CategoryID       *int64         `json:"categoryId,omitempty"`
	Title            string         `json:"title"`
	Content          string         `json:"content"`
	ContentType      string         `json:"contentType,omitempty"`
	Priority         string         `json:"priority,omitempty"`
	Source           string         `json:"source,omitempty"`
	AssigneeAdminID  *int64         `json:"assigneeAdminId,omitempty"`
	GroupID          *int64         `json:"groupId,omitempty"`
	Tags             []string       `json:"tags,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
	AttachmentIDs    []int64        `json:"attachmentIds,omitempty"`
	CreatedByAdminID *int64         `json:"-"`
}

// ReplyCommand 追加一条会话消息。
type ReplyCommand struct {
	TicketID      int64          `json:"ticketId"`
	Content       string         `json:"content"`
	ContentType   string         `json:"contentType,omitempty"`
	Internal      bool           `json:"internal,omitempty"`
	AttachmentIDs []int64        `json:"attachmentIds,omitempty"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	// 回复同时切换状态（如客服回复后置为 pending_user）
	NextStatus string `json:"nextStatus,omitempty"`
}

// UpdateCommand 工单字段变更。指针语义：nil = 不改。
type UpdateCommand struct {
	Title      *string   `json:"title,omitempty"`
	CategoryID *int64    `json:"categoryId,omitempty"`
	Priority   *string   `json:"priority,omitempty"`
	Tags       *[]string `json:"tags,omitempty"`
	Locked     *bool     `json:"locked,omitempty"`
}

// AssignCommand 指派。两者都为 nil 表示取消指派。
type AssignCommand struct {
	AssigneeAdminID *int64 `json:"assigneeAdminId,omitempty"`
	GroupID         *int64 `json:"groupId,omitempty"`
	// AutoPick=true 时忽略 AssigneeAdminID，按组策略自动挑人
	AutoPick bool   `json:"autoPick,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// StatusCommand 状态流转。
type StatusCommand struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
	// Solution 在置为 resolved 时作为一条对外消息追加
	Solution string `json:"solution,omitempty"`
}

// RatingCommand 满意度评价（仅提单人可提交）。
type RatingCommand struct {
	Rating  int16  `json:"rating"`
	Comment string `json:"comment,omitempty"`
}

// BulkCommand 批量操作。
type BulkCommand struct {
	IDs             []int64 `json:"ids"`
	Action          string  `json:"action"` // assign / status / priority / tag / close / delete
	AssigneeAdminID *int64  `json:"assigneeAdminId,omitempty"`
	GroupID         *int64  `json:"groupId,omitempty"`
	Status          string  `json:"status,omitempty"`
	Priority        string  `json:"priority,omitempty"`
	Tags            []string `json:"tags,omitempty"`
	Reason          string  `json:"reason,omitempty"`
}

// BulkResult 批量操作结果。Failed 记录逐条失败原因，前端可精确提示。
type BulkResult struct {
	Requested int             `json:"requested"`
	Succeeded int             `json:"succeeded"`
	Failed    []BulkFailure   `json:"failed,omitempty"`
	Action    string          `json:"action"`
}

// BulkFailure 单条失败详情。
type BulkFailure struct {
	ID     int64  `json:"id"`
	Reason string `json:"reason"`
}

// ─────────────── 权限与范围 ───────────────

// Scope 数据可见范围。由 TicketService 根据管理员会话推导，Repository 据此拼 WHERE。
// 内部类型：不出网（出网的是脱敏后的 ScopeInfo），故不加 json tag。
type Scope struct {
	// All=true 时无视其它字段，可见全部工单（超管 / 全局 ticket:read）
	All bool
	// AppIDs 应用级角色可见的应用集合；空且 All=false 时退化为「仅本人相关」
	AppIDs []int64
	// AdminID 当前管理员，用于匹配受理人 / 提单人 / 关注人
	AdminID int64
	// GroupIDs 当前管理员所属处理组
	GroupIDs []int64
	// PersonalOnly=true 时即使有 AppIDs 也只看本人相关（无 ticket:read 权限点的组成员）
	PersonalOnly bool
}

// ScopeInfo 回传给前端的可见范围说明。
type ScopeInfo struct {
	Level    string  `json:"level"` // all / app / personal
	AppIDs   []int64 `json:"appIds,omitempty"`
	GroupIDs []int64 `json:"groupIds,omitempty"`
	Label    string  `json:"label"`
}

// ActionSet 当前访问者对某工单的可执行操作。
type ActionSet struct {
	View            bool `json:"view"`
	Reply           bool `json:"reply"`
	InternalNote    bool `json:"internalNote"`
	ViewInternal    bool `json:"viewInternal"`
	Edit            bool `json:"edit"`
	Assign          bool `json:"assign"`
	ChangeStatus    bool `json:"changeStatus"`
	Close           bool `json:"close"`
	Reopen          bool `json:"reopen"`
	Delete          bool `json:"delete"`
	Watch           bool `json:"watch"`
	ManageWatchers  bool `json:"manageWatchers"`
	UploadAttachment bool `json:"uploadAttachment"`
}

// ─────────────── 统计 ───────────────

// Stats 工单概览统计（受 Scope 约束）。
type Stats struct {
	Total          int64            `json:"total"`
	Open           int64            `json:"open"`
	Processing     int64            `json:"processing"`
	PendingUser    int64            `json:"pendingUser"`
	Resolved       int64            `json:"resolved"`
	Closed         int64            `json:"closed"`
	Unassigned     int64            `json:"unassigned"`
	MineAssigned   int64            `json:"mineAssigned"`
	OverdueFirst   int64            `json:"overdueFirstResponse"`
	OverdueResolve int64            `json:"overdueResolve"`
	CreatedToday   int64            `json:"createdToday"`
	ResolvedToday  int64            `json:"resolvedToday"`
	ByPriority     map[string]int64 `json:"byPriority"`
	ByCategory     []CategoryStat   `json:"byCategory"`
	AvgFirstRespMs int64            `json:"avgFirstResponseMs"`
	AvgResolveMs   int64            `json:"avgResolveMs"`
	AvgRating      float64          `json:"avgRating"`
	RatingCount    int64            `json:"ratingCount"`
}

// CategoryStat 分类维度统计。
type CategoryStat struct {
	CategoryID   *int64 `json:"categoryId,omitempty"`
	CategoryName string `json:"categoryName"`
	Count        int64  `json:"count"`
}

// TrendPoint 工单趋势单点。
type TrendPoint struct {
	Date     string `json:"date"`
	Created  int64  `json:"created"`
	Resolved int64  `json:"resolved"`
	Closed   int64  `json:"closed"`
}

// AgentStat 处理人绩效。
type AgentStat struct {
	AdminID        int64   `json:"adminId"`
	Account        string  `json:"account"`
	DisplayName    string  `json:"displayName"`
	Assigned       int64   `json:"assigned"`
	Resolved       int64   `json:"resolved"`
	Open           int64   `json:"open"`
	AvgFirstRespMs int64   `json:"avgFirstResponseMs"`
	AvgResolveMs   int64   `json:"avgResolveMs"`
	AvgRating      float64 `json:"avgRating"`
	Breached       int64   `json:"breached"`
}
