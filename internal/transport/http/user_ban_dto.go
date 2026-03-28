package httptransport

import "time"

type AdminUserBanCreateRequest struct {
	BanType  string         `json:"banType"`
	BanScope string         `json:"banScope"`
	Reason   string         `json:"reason"`
	Evidence map[string]any `json:"evidence"`
	StartAt  *time.Time     `json:"startAt"`
	EndAt    *time.Time     `json:"endAt"`
}

type AdminUserBanBatchCreateRequest struct {
	UserIDs []int64 `json:"userIds" binding:"required"`
	AdminUserBanCreateRequest
}

type AdminUserBanListQuery struct {
	Status string `form:"status"`
	Page   int    `form:"page"`
	Limit  int    `form:"limit"`
}

type AdminUserBanRevokeRequest struct {
	Reason string `json:"reason"`
}
