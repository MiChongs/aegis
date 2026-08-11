package httptransport

import "time"

// PlatformBannerUpsertRequest 新建/更新请求（指针字段支持 Patch 语义）。
type PlatformBannerUpsertRequest struct {
	Title       *string    `json:"title"`
	Description *string    `json:"description"`
	ImageURL    *string    `json:"imageUrl"`
	ClickURL    *string    `json:"clickUrl"`
	Type        *string    `json:"type"`
	Position    *int       `json:"position"`
	Status      *bool      `json:"status"`
	StartTime   *time.Time `json:"startTime"`
	EndTime     *time.Time `json:"endTime"`
}

// PlatformBannerListQuery 列表查询参数。
type PlatformBannerListQuery struct {
	Status  *bool  `form:"status"`
	Type    string `form:"type"`
	Keyword string `form:"keyword"`
	Page    int    `form:"page"`
	Limit   int    `form:"limit"`
}

// PlatformBannerBatchIDsRequest 批量删除请求。
type PlatformBannerBatchIDsRequest struct {
	IDs []int64 `json:"ids" binding:"required"`
}
