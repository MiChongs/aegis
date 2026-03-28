package httptransport

import "time"

// ────────────────────── 统一身份 DTO ──────────────────────

// CreateIdentityRequest 创建全局身份请求
type CreateIdentityRequest struct {
	Email       string `json:"email"`
	Phone       string `json:"phone"`
	DisplayName string `json:"displayName"`
}

// UpdateIdentityStatusRequest 更新身份状态请求
type UpdateIdentityStatusRequest struct {
	Status string `json:"status" binding:"required"`
}

// UpdateIdentityLifecycleRequest 更新生命周期请求
type UpdateIdentityLifecycleRequest struct {
	State string `json:"state" binding:"required"`
}

// UpdateIdentityRiskRequest 更新风险评分请求
type UpdateIdentityRiskRequest struct {
	Score int    `json:"score"`
	Level string `json:"level" binding:"required"`
}

// IdentityListQuery 身份列表查询参数
type IdentityListQueryParams struct {
	Keyword        string `form:"keyword"`
	Status         string `form:"status"`
	LifecycleState string `form:"lifecycleState"`
	RiskLevel      string `form:"riskLevel"`
	TagID          *int64 `form:"tagId"`
	Page           int    `form:"page"`
	Limit          int    `form:"limit"`
}

// ────────────────────── 映射 DTO ──────────────────────

// CreateMappingRequest 创建映射请求
type CreateMappingRequest struct {
	IdentityID int64 `json:"identityId" binding:"required"`
	AppID      int64 `json:"appId" binding:"required"`
	UserID     int64 `json:"userId" binding:"required"`
}

// ────────────────────── 标签 DTO ──────────────────────

// CreateTagRequest 创建标签请求
type CreateTagRequest struct {
	Name        string `json:"name" binding:"required"`
	Color       string `json:"color"`
	Description string `json:"description"`
}

// TagAssignRequest 标签分配请求
type TagAssignRequest struct {
	IdentityID int64 `json:"identityId" binding:"required"`
	TagID      int64 `json:"tagId" binding:"required"`
}

// ────────────────────── 分群 DTO ──────────────────────

// CreateSegmentRequest 创建分群请求
type CreateSegmentRequest struct {
	Name        string         `json:"name" binding:"required"`
	Description string         `json:"description"`
	SegmentType string         `json:"segmentType"`
	Rules       map[string]any `json:"rules"`
}

// UpdateSegmentRequest 更新分群请求
type UpdateSegmentRequest struct {
	Name        string         `json:"name" binding:"required"`
	Description string         `json:"description"`
	Rules       map[string]any `json:"rules"`
}

// SegmentMemberRequest 分群成员操作请求
type SegmentMemberRequest struct {
	IdentityID int64 `json:"identityId" binding:"required"`
}

// SegmentMemberListQuery 分群成员列表查询参数
type SegmentMemberListQuery struct {
	Page  int `form:"page"`
	Limit int `form:"limit"`
}

// ────────────────────── 黑白名单 DTO ──────────────────────

// CreateListEntryRequest 创建名单条目请求
type CreateListEntryRequest struct {
	ListType   string     `json:"listType" binding:"required"`
	IdentityID *int64     `json:"identityId,omitempty"`
	Email      string     `json:"email"`
	Phone      string     `json:"phone"`
	IP         string     `json:"ip"`
	Reason     string     `json:"reason"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
}

// ListEntryListQuery 名单列表查询参数
type ListEntryListQuery struct {
	ListType string `form:"listType"`
	Page     int    `form:"page"`
	Limit    int    `form:"limit"`
}

// CheckBlacklistRequest 黑名单检查请求
type CheckBlacklistRequest struct {
	Email string `json:"email"`
	Phone string `json:"phone"`
	IP    string `json:"ip"`
}

// ────────────────────── 合并 DTO ──────────────────────

// MergeIdentityRequest 身份合并请求
type MergeIdentityRequest struct {
	PrimaryID int64 `json:"primaryId" binding:"required"`
	MergedID  int64 `json:"mergedId" binding:"required"`
}

// ────────────────────── 申诉 DTO ──────────────────────

// CreateAppealRequest 创建申诉请求
type CreateAppealRequest struct {
	IdentityID int64  `json:"identityId" binding:"required"`
	AppealType string `json:"appealType" binding:"required"`
	Reason     string `json:"reason" binding:"required"`
	Evidence   string `json:"evidence"`
}

// ReviewAppealRequest 审核申诉请求
type ReviewAppealRequest struct {
	Action  string `json:"action" binding:"required"`
	Comment string `json:"comment"`
}

// AppealListQuery 申诉列表查询参数
type AppealListQuery struct {
	Status string `form:"status"`
	Page   int    `form:"page"`
	Limit  int    `form:"limit"`
}

// ────────────────────── 注销 DTO ──────────────────────

// CreateDeactivationRequestDTO 创建注销请求
type CreateDeactivationRequestDTO struct {
	IdentityID  int64  `json:"identityId" binding:"required"`
	Reason      string `json:"reason"`
	CoolingDays int    `json:"coolingDays"`
}

// ────────────────────── 同步 DTO ──────────────────────

// SyncIdentityRequest 同步单个用户身份请求
type SyncIdentityRequest struct {
	AppID  int64 `json:"appId" binding:"required"`
	UserID int64 `json:"userId" binding:"required"`
}

// BatchSyncRequest 批量同步请求
type BatchSyncRequest struct {
	AppID int64 `json:"appId" binding:"required"`
}
