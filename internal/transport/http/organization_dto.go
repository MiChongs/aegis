package httptransport

type CreateOrgRequest struct {
	Name        string `json:"name" binding:"required"`
	Code        string `json:"code" binding:"required"`
	Description string `json:"description"`
	LogoURL     string `json:"logoURL"`
}

type UpdateOrgRequest struct {
	Name        *string `json:"name,omitempty"`
	Code        *string `json:"code,omitempty"`
	Description *string `json:"description,omitempty"`
	LogoURL     *string `json:"logoURL,omitempty"`
	Status      *string `json:"status,omitempty"`
}

type CreateDeptRequest struct {
	ParentID    *int64 `json:"parentId,omitempty"`
	Name        string `json:"name" binding:"required"`
	Code        string `json:"code" binding:"required"`
	Description string `json:"description"`
	SortOrder   int    `json:"sortOrder"`
	LeaderID    *int64 `json:"leaderId,omitempty"`
}

type UpdateDeptRequest struct {
	Name        *string `json:"name,omitempty"`
	Code        *string `json:"code,omitempty"`
	Description *string `json:"description,omitempty"`
	SortOrder   *int    `json:"sortOrder,omitempty"`
	LeaderID    *int64  `json:"leaderId,omitempty"`
	Status      *string `json:"status,omitempty"`
}

type MoveDeptRequest struct {
	ParentID *int64 `json:"parentId"`
}

type AddMemberRequest struct {
	AdminID  int64 `json:"adminId" binding:"required"`
	IsLeader bool  `json:"isLeader"`
}

type InviteMemberRequest struct {
	AdminID  int64  `json:"adminId" binding:"required"`
	IsLeader bool   `json:"isLeader"`
	Message  string `json:"message"`
}

type InvitationListQueryParams struct {
	Role   string `form:"role"`   // sent / received
	Status string `form:"status"` // pending / accepted / rejected / expired / cancelled
	Page   int    `form:"page"`
	Limit  int    `form:"limit"`
}

type CreatePositionRequest struct {
	Name        string `json:"name" binding:"required"`
	Code        string `json:"code" binding:"required"`
	Description string `json:"description"`
	Level       int    `json:"level"`
}

type UpdatePositionRequest struct {
	Name        *string `json:"name,omitempty"`
	Code        *string `json:"code,omitempty"`
	Description *string `json:"description,omitempty"`
	Level       *int    `json:"level,omitempty"`
}

type UpdateMemberPositionRequest struct {
	PositionID *int64 `json:"positionId"`
	JobTitle   string `json:"jobTitle"`
}

type SetReportingRequest struct {
	ReportingTo int64 `json:"reportingTo" binding:"required"`
}

type SetDelegateRequest struct {
	DelegateTo int64   `json:"delegateTo" binding:"required"`
	ExpiresAt  *string `json:"expiresAt,omitempty"` // RFC3339 时间字符串
}

type BatchInviteRequest struct {
	AdminIDs []int64 `json:"adminIds" binding:"required,min=1"`
	Message  string  `json:"message"`
}
