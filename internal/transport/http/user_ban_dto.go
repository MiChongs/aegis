package httptransport

import "time"

type AdminUserBanCreateRequest struct {
	BanType  string         `json:"banType"`
	BanScope string         `json:"banScope"`
	Reason   string         `json:"reason"`
	Evidence map[string]any `json:"evidence"`
	StartAt  *time.Time     `json:"startAt"`
	EndAt    *time.Time     `json:"endAt"`
	// Level app（默认）/ platform 全站封禁；ReasonCode 原因类别；PublicReason 给用户看的说明
	Level        string `json:"level"`
	ReasonCode   string `json:"reasonCode"`
	PublicReason string `json:"publicReason"`
	Appealable   *bool  `json:"appealable"`
	Notify       *bool  `json:"notify"`
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
