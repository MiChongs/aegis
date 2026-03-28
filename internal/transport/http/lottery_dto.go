package httptransport

import "time"

// ────────────────────── 抽奖活动 DTO ──────────────────────

// LotteryActivityCreateRequest 创建抽奖活动请求
type LotteryActivityCreateRequest struct {
	Name          string         `json:"name" binding:"required"`
	Description   string         `json:"description"`
	UIMode        string         `json:"uiMode"`
	Status        string         `json:"status"`
	JoinMode      string         `json:"joinMode"`
	AutoJoinRules map[string]any `json:"autoJoinRules"`
	CostType      string         `json:"costType"`
	CostAmount    int            `json:"costAmount"`
	DailyLimit    int            `json:"dailyLimit"`
	TotalLimit    int            `json:"totalLimit"`
	StartTime     time.Time      `json:"startTime" binding:"required"`
	EndTime       time.Time      `json:"endTime" binding:"required"`
}

// LotteryActivityUpdateRequest 更新抽奖活动请求
type LotteryActivityUpdateRequest struct {
	Name          *string        `json:"name"`
	Description   *string        `json:"description"`
	UIMode        *string        `json:"uiMode"`
	Status        *string        `json:"status"`
	JoinMode      *string        `json:"joinMode"`
	AutoJoinRules map[string]any `json:"autoJoinRules"`
	CostType      *string        `json:"costType"`
	CostAmount    *int           `json:"costAmount"`
	DailyLimit    *int           `json:"dailyLimit"`
	TotalLimit    *int           `json:"totalLimit"`
	StartTime     *time.Time     `json:"startTime"`
	EndTime       *time.Time     `json:"endTime"`
}

// LotteryActivityListQuery 活动列表查询参数
type LotteryActivityListQuery struct {
	Status  string `form:"status"`
	Keyword string `form:"keyword"`
	Page    int    `form:"page"`
	Limit   int    `form:"limit"`
}

// ────────────────────── 奖品 DTO ──────────────────────

// LotteryPrizeCreateRequest 创建奖品请求
type LotteryPrizeCreateRequest struct {
	Name      string         `json:"name" binding:"required"`
	Type      string         `json:"type" binding:"required"`
	Value     string         `json:"value"`
	ImageURL  string         `json:"imageUrl"`
	Quantity  int            `json:"quantity"`
	Weight    int            `json:"weight"`
	Position  int            `json:"position"`
	IsDefault bool           `json:"isDefault"`
	Extra     map[string]any `json:"extra"`
}

// LotteryPrizeUpdateRequest 更新奖品请求
type LotteryPrizeUpdateRequest struct {
	Name      *string        `json:"name"`
	Type      *string        `json:"type"`
	Value     *string        `json:"value"`
	ImageURL  *string        `json:"imageUrl"`
	Quantity  *int           `json:"quantity"`
	Weight    *int           `json:"weight"`
	Position  *int           `json:"position"`
	IsDefault *bool          `json:"isDefault"`
	Extra     map[string]any `json:"extra"`
}

// ────────────────────── 抽奖 / 参与 DTO ──────────────────────

// LotteryDrawRequest 用户抽奖请求
type LotteryDrawRequest struct {
	ActivityID int64 `json:"activityId" binding:"required"`
}

// LotteryJoinRequest 用户加入活动请求
type LotteryJoinRequest struct {
	ActivityID int64 `json:"activityId" binding:"required"`
}

// LotteryDrawListQuery 抽奖记录查询参数
type LotteryDrawListQuery struct {
	ActivityID int64  `form:"activityId"`
	UserID     int64  `form:"userId"`
	Status     string `form:"status"`
	Page       int    `form:"page"`
	Limit      int    `form:"limit"`
}

// LotteryMyDrawListQuery 用户自己的抽奖记录查询参数
type LotteryMyDrawListQuery struct {
	ActivityID int64  `form:"activityId"`
	Status     string `form:"status"`
	Page       int    `form:"page"`
	Limit      int    `form:"limit"`
}
