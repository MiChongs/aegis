package user

import "time"

const (
	AccountBanTypeTemporary = "temporary"
	AccountBanTypePermanent = "permanent"

	AccountBanScopeLogin = "login"
	AccountBanScopeAll   = "all"

	AccountBanStatusActive  = "active"
	AccountBanStatusExpired = "expired"
	AccountBanStatusRevoked = "revoked"
)

type BanOperator struct {
	AdminID   int64  `json:"adminId"`
	AdminName string `json:"adminName"`
}

type AccountBan struct {
	ID                 int64          `json:"id"`
	AppID              int64          `json:"appid"`
	UserID             int64          `json:"userId"`
	BanType            string         `json:"banType"`
	BanScope           string         `json:"banScope"`
	Status             string         `json:"status"`
	Reason             string         `json:"reason"`
	Evidence           map[string]any `json:"evidence,omitempty"`
	BannedByAdminID    *int64         `json:"bannedByAdminId,omitempty"`
	BannedByAdminName  string         `json:"bannedByAdminName,omitempty"`
	RevokedByAdminID   *int64         `json:"revokedByAdminId,omitempty"`
	RevokedByAdminName string         `json:"revokedByAdminName,omitempty"`
	RevokeReason       string         `json:"revokeReason,omitempty"`
	StartAt            time.Time      `json:"startAt"`
	EndAt              *time.Time     `json:"endAt,omitempty"`
	RevokedAt          *time.Time     `json:"revokedAt,omitempty"`
	CreatedAt          time.Time      `json:"createdAt"`
	UpdatedAt          time.Time      `json:"updatedAt"`
}

type AccountBanCreateInput struct {
	BanType  string         `json:"banType"`
	BanScope string         `json:"banScope"`
	Reason   string         `json:"reason"`
	Evidence map[string]any `json:"evidence,omitempty"`
	StartAt  *time.Time     `json:"startAt,omitempty"`
	EndAt    *time.Time     `json:"endAt,omitempty"`
	Operator BanOperator    `json:"operator"`
}

type AccountBanRevokeInput struct {
	Reason   string      `json:"reason"`
	Operator BanOperator `json:"operator"`
}

type AccountBanQuery struct {
	Status string `json:"status"`
	Page   int    `json:"page"`
	Limit  int    `json:"limit"`
}

type AccountBanListResult struct {
	Items      []AccountBan `json:"items"`
	Page       int          `json:"page"`
	Limit      int          `json:"limit"`
	Total      int64        `json:"total"`
	TotalPages int          `json:"totalPages"`
}

type AccountBanBatchCreateInput struct {
	UserIDs []int64 `json:"userIds"`
	AccountBanCreateInput
}

type AccountBanBatchCreateResult struct {
	AppID            int64   `json:"appid"`
	Requested        int     `json:"requested"`
	Created          int64   `json:"created"`
	Failed           int     `json:"failed"`
	ProcessedUserIDs []int64 `json:"processedUserIds"`
	FailedUserIDs    []int64 `json:"failedUserIds"`
}
