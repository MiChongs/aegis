package user

import "time"

// GlobalIdentity 全局统一身份
type GlobalIdentity struct {
	ID                int64          `json:"id"`
	Email             string         `json:"email,omitempty"`
	Phone             string         `json:"phone,omitempty"`
	DisplayName       string         `json:"displayName"`
	Status            string         `json:"status"`          // active/frozen/deactivating/deleted
	RiskScore         int            `json:"riskScore"`
	RiskLevel         string         `json:"riskLevel"`       // normal/low/medium/high/critical
	LifecycleState    string         `json:"lifecycleState"`  // registered/active/silent/churned/returning/deactivating/deleted
	LifecycleChangedAt time.Time     `json:"lifecycleChangedAt"`
	Metadata          map[string]any `json:"metadata,omitempty"`
	CreatedAt         time.Time      `json:"createdAt"`
	UpdatedAt         time.Time      `json:"updatedAt"`
	DeletedAt         *time.Time     `json:"deletedAt,omitempty"`
	// 关联数据（查询时填充）
	Tags     []UserTag             `json:"tags,omitempty"`
	Mappings []IdentityUserMapping `json:"mappings,omitempty"`
}

// IdentityUserMapping 跨应用用户映射
type IdentityUserMapping struct {
	ID         int64     `json:"id"`
	IdentityID int64     `json:"identityId"`
	AppID      int64     `json:"appId"`
	UserID     int64     `json:"userId"`
	CreatedAt  time.Time `json:"createdAt"`
	// 关联
	AppName  string `json:"appName,omitempty"`
	Account  string `json:"account,omitempty"`
	Nickname string `json:"nickname,omitempty"`
}

// UserTag 用户标签定义
type UserTag struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Color       string    `json:"color"`
	Description string    `json:"description"`
	CreatedBy   *int64    `json:"createdBy,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

// UserSegment 用户分群
type UserSegment struct {
	ID          int64          `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	SegmentType string         `json:"segmentType"` // static/dynamic
	Rules       map[string]any `json:"rules"`
	MemberCount int            `json:"memberCount"`
	CreatedBy   *int64         `json:"createdBy,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

// UserListEntry 黑白名单条目
type UserListEntry struct {
	ID         int64      `json:"id"`
	ListType   string     `json:"listType"` // blacklist/whitelist
	IdentityID *int64     `json:"identityId,omitempty"`
	Email      string     `json:"email,omitempty"`
	Phone      string     `json:"phone,omitempty"`
	IP         string     `json:"ip,omitempty"`
	Reason     string     `json:"reason"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	CreatedBy  *int64     `json:"createdBy,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
}

// IdentityMerge 账号合并记录
type IdentityMerge struct {
	ID        int64          `json:"id"`
	PrimaryID int64          `json:"primaryId"`
	MergedID  int64          `json:"mergedId"`
	MergedBy  int64          `json:"mergedBy"`
	Status    string         `json:"status"` // pending/completed/failed
	Details   map[string]any `json:"details"`
	CreatedAt time.Time      `json:"createdAt"`
}

// UserAppeal 用户申诉
type UserAppeal struct {
	ID            int64      `json:"id"`
	IdentityID    int64      `json:"identityId"`
	AppealType    string     `json:"appealType"` // unfreeze/unban/data_request/deletion_cancel
	Reason        string     `json:"reason"`
	Evidence      string     `json:"evidence"`
	Status        string     `json:"status"` // pending/approved/rejected
	ReviewerID    *int64     `json:"reviewerId,omitempty"`
	ReviewComment string     `json:"reviewComment"`
	ReviewedAt    *time.Time `json:"reviewedAt,omitempty"`
	CreatedAt     time.Time  `json:"createdAt"`
	// 关联
	IdentityName string `json:"identityName,omitempty"`
}

// DeactivationRequest 注销请求
type DeactivationRequest struct {
	ID          int64     `json:"id"`
	IdentityID  int64     `json:"identityId"`
	Reason      string    `json:"reason"`
	CoolingDays int       `json:"coolingDays"`
	ScheduledAt time.Time `json:"scheduledAt"`
	Status      string    `json:"status"` // pending/cancelled/completed
	CreatedAt   time.Time `json:"createdAt"`
	// 关联
	IdentityName string `json:"identityName,omitempty"`
}

// ── 查询/输入类型 ──

type IdentityListQuery struct {
	Keyword        string `json:"keyword,omitempty"`
	Status         string `json:"status,omitempty"`
	LifecycleState string `json:"lifecycleState,omitempty"`
	RiskLevel      string `json:"riskLevel,omitempty"`
	TagID          *int64 `json:"tagId,omitempty"`
	Page           int    `json:"page"`
	Limit          int    `json:"limit"`
}

type CreateTagInput struct {
	Name        string `json:"name"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

type CreateSegmentInput struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	SegmentType string         `json:"segmentType"`
	Rules       map[string]any `json:"rules"`
}

type CreateListEntryInput struct {
	ListType   string     `json:"listType"`
	IdentityID *int64     `json:"identityId,omitempty"`
	Email      string     `json:"email,omitempty"`
	Phone      string     `json:"phone,omitempty"`
	IP         string     `json:"ip,omitempty"`
	Reason     string     `json:"reason"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
}

type CreateAppealInput struct {
	AppealType string `json:"appealType"`
	Reason     string `json:"reason"`
	Evidence   string `json:"evidence"`
}

type ReviewAppealInput struct {
	Action  string `json:"action"` // approved/rejected
	Comment string `json:"comment"`
}
