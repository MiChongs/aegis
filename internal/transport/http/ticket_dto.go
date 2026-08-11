package httptransport

import (
	ticketdomain "aegis/internal/domain/ticket"
)

// ─────────────── 工单列表 / 详情 ───────────────

// TicketListQuery 管理端列表查询参数。
// 逗号分隔的多值字段（status/priority/tags）在 handler 里拆分。
type TicketListQuery struct {
	AppID         *int64 `form:"appid"`
	Status        string `form:"status"`
	Priority      string `form:"priority"`
	CategoryID    *int64 `form:"categoryId"`
	GroupID       *int64 `form:"groupId"`
	AssigneeID    *int64 `form:"assigneeId"`
	Unassigned    bool   `form:"unassigned"`
	RequesterID   *int64 `form:"requesterId"`
	Keyword       string `form:"keyword"`
	Tags          string `form:"tags"`
	SLAState      string `form:"slaState"`
	OverdueOnly   bool   `form:"overdueOnly"`
	CreatedFrom   string `form:"createdFrom"`
	CreatedTo     string `form:"createdTo"`
	Rated         *bool  `form:"rated"`
	SortBy        string `form:"sortBy"`
	SortDir       string `form:"sortDir"`
	IncludeClosed bool   `form:"includeClosed"`
	// Mine=true 时强制只看指派给自己的工单（「我的待办」快捷视图）
	Mine  bool `form:"mine"`
	Page  int  `form:"page"`
	Limit int  `form:"limit"`
}

// TicketCreateRequest 建单请求。
type TicketCreateRequest struct {
	AppID            int64          `json:"appid"`
	RequesterType    string         `json:"requesterType"`
	RequesterUserID  *int64         `json:"requesterUserId"`
	RequesterAdminID *int64         `json:"requesterAdminId"`
	RequesterName    string         `json:"requesterName"`
	RequesterContact string         `json:"requesterContact"`
	CategoryID       *int64         `json:"categoryId"`
	Title            string         `json:"title" binding:"required"`
	Content          string         `json:"content" binding:"required"`
	ContentType      string         `json:"contentType"`
	Priority         string         `json:"priority"`
	Source           string         `json:"source"`
	AssigneeAdminID  *int64         `json:"assigneeAdminId"`
	GroupID          *int64         `json:"groupId"`
	Tags             []string       `json:"tags"`
	Metadata         map[string]any `json:"metadata"`
	AttachmentIDs    []int64        `json:"attachmentIds"`
}

// ToCommand 转成领域命令。
func (r TicketCreateRequest) ToCommand() ticketdomain.CreateCommand {
	return ticketdomain.CreateCommand{
		AppID:            r.AppID,
		RequesterType:    r.RequesterType,
		RequesterUserID:  r.RequesterUserID,
		RequesterAdminID: r.RequesterAdminID,
		RequesterName:    r.RequesterName,
		RequesterContact: r.RequesterContact,
		CategoryID:       r.CategoryID,
		Title:            r.Title,
		Content:          r.Content,
		ContentType:      r.ContentType,
		Priority:         r.Priority,
		Source:           r.Source,
		AssigneeAdminID:  r.AssigneeAdminID,
		GroupID:          r.GroupID,
		Tags:             r.Tags,
		Metadata:         r.Metadata,
		AttachmentIDs:    r.AttachmentIDs,
	}
}

// TicketReplyRequest 回复请求。
type TicketReplyRequest struct {
	Content       string         `json:"content" binding:"required"`
	ContentType   string         `json:"contentType"`
	Internal      bool           `json:"internal"`
	AttachmentIDs []int64        `json:"attachmentIds"`
	Metadata      map[string]any `json:"metadata"`
	NextStatus    string         `json:"nextStatus"`
	// QuickReplyID 非零时记一次快捷回复使用量
	QuickReplyID int64 `json:"quickReplyId"`
}

// TicketUpdateRequest 字段更新请求。指针 = 不传则不改。
type TicketUpdateRequest struct {
	Title      *string   `json:"title"`
	CategoryID *int64    `json:"categoryId"`
	Priority   *string   `json:"priority"`
	Tags       *[]string `json:"tags"`
	Locked     *bool     `json:"locked"`
}

// TicketAssignRequest 指派请求。
type TicketAssignRequest struct {
	AssigneeAdminID *int64 `json:"assigneeAdminId"`
	GroupID         *int64 `json:"groupId"`
	AutoPick        bool   `json:"autoPick"`
	Reason          string `json:"reason"`
}

// TicketStatusRequest 状态流转请求。
type TicketStatusRequest struct {
	Status   string `json:"status" binding:"required"`
	Reason   string `json:"reason"`
	Solution string `json:"solution"`
}

// TicketRatingRequest 满意度评价请求。
type TicketRatingRequest struct {
	Rating  int16  `json:"rating" binding:"required"`
	Comment string `json:"comment"`
}

// TicketBulkRequest 批量操作请求。
type TicketBulkRequest struct {
	IDs             []int64  `json:"ids" binding:"required"`
	Action          string   `json:"action" binding:"required"`
	AssigneeAdminID *int64   `json:"assigneeAdminId"`
	GroupID         *int64   `json:"groupId"`
	Status          string   `json:"status"`
	Priority        string   `json:"priority"`
	Tags            []string `json:"tags"`
	Reason          string   `json:"reason"`
}

// TicketWatchersRequest 关注人设置。
type TicketWatchersRequest struct {
	AdminIDs []int64 `json:"adminIds"`
}

// TicketCancelRequest 用户撤单。
type TicketCancelRequest struct {
	Reason string `json:"reason"`
}

// ─────────────── 配置 ───────────────

// TicketCategoryRequest 分类新建/更新。
type TicketCategoryRequest struct {
	AppID           int64                   `json:"appid"`
	ParentID        *int64                  `json:"parentId"`
	Key             string                  `json:"key" binding:"required"`
	Name            string                  `json:"name" binding:"required"`
	Description     string                  `json:"description"`
	DefaultPriority string                  `json:"defaultPriority"`
	DefaultGroupID  *int64                  `json:"defaultGroupId"`
	SLAPolicyID     *int64                  `json:"slaPolicyId"`
	FormSchema      []ticketdomain.FormField `json:"formSchema"`
	UserSubmittable *bool                   `json:"userSubmittable"`
	Sort            int                     `json:"sort"`
	Enabled         *bool                   `json:"enabled"`
}

// ToDomain 转领域对象。未传的布尔字段取安全默认值。
func (r TicketCategoryRequest) ToDomain(id int64) ticketdomain.Category {
	item := ticketdomain.Category{
		ID:              id,
		AppID:           r.AppID,
		ParentID:        r.ParentID,
		Key:             r.Key,
		Name:            r.Name,
		Description:     r.Description,
		DefaultPriority: r.DefaultPriority,
		DefaultGroupID:  r.DefaultGroupID,
		SLAPolicyID:     r.SLAPolicyID,
		FormSchema:      r.FormSchema,
		UserSubmittable: true,
		Sort:            r.Sort,
		Enabled:         true,
	}
	if r.UserSubmittable != nil {
		item.UserSubmittable = *r.UserSubmittable
	}
	if r.Enabled != nil {
		item.Enabled = *r.Enabled
	}
	return item
}

// TicketGroupRequest 处理组新建/更新。
type TicketGroupRequest struct {
	AppID          int64  `json:"appid"`
	Key            string `json:"key" binding:"required"`
	Name           string `json:"name" binding:"required"`
	Description    string `json:"description"`
	AssignStrategy string `json:"assignStrategy"`
	Enabled        *bool  `json:"enabled"`
}

// TicketGroupMembersRequest 组成员全量设置。
type TicketGroupMembersRequest struct {
	Members []TicketGroupMemberInput `json:"members"`
}

// TicketGroupMemberInput 单个成员。
type TicketGroupMemberInput struct {
	AdminID int64  `json:"adminId"`
	Role    string `json:"role"`
}

// TicketSLARequest SLA 策略新建/更新。
type TicketSLARequest struct {
	AppID                int64                       `json:"appid"`
	Name                 string                      `json:"name" binding:"required"`
	Description          string                      `json:"description"`
	FirstResponseMinutes map[string]int              `json:"firstResponseMinutes"`
	ResolveMinutes       map[string]int              `json:"resolveMinutes"`
	BusinessHours        *ticketdomain.BusinessHours `json:"businessHours"`
	WarnRatio            float64                     `json:"warnRatio"`
	Enabled              *bool                       `json:"enabled"`
}

// TicketQuickReplyRequest 快捷回复新建/更新。
type TicketQuickReplyRequest struct {
	AppID      int64  `json:"appid"`
	Title      string `json:"title" binding:"required"`
	Content    string `json:"content" binding:"required"`
	CategoryID *int64 `json:"categoryId"`
	// Private=true 时归属当前管理员，仅自己可见
	Private bool  `json:"private"`
	Sort    int   `json:"sort"`
	Enabled *bool `json:"enabled"`
}
