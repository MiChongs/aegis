package lottery

import "time"

// ── 活动 ──

type Activity struct {
	ID            int64          `json:"id"`
	AppID         int64          `json:"appid"`
	Name          string         `json:"name"`
	Description   string         `json:"description,omitempty"`
	UIMode        string         `json:"uiMode"`        // wheel | grid
	Status        string         `json:"status"`        // draft | active | paused | ended
	JoinMode      string         `json:"joinMode"`      // manual | auto | both
	AutoJoinRules map[string]any `json:"autoJoinRules,omitempty"`
	CostType      string         `json:"costType"`      // free | points
	CostAmount    int            `json:"costAmount"`
	DailyLimit    int            `json:"dailyLimit"`    // 0 = 不限
	TotalLimit    int            `json:"totalLimit"`    // 0 = 不限
	StartTime     time.Time      `json:"startTime"`
	EndTime       time.Time      `json:"endTime"`
	SeedHash      string         `json:"seedHash,omitempty"`
	SeedValue     string         `json:"seedValue,omitempty"`
	ChainTxHash   string         `json:"chainTxHash,omitempty"`
	ChainNetwork  string         `json:"chainNetwork,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

type ActivityMutation struct {
	ID            int64
	Name          *string
	Description   *string
	UIMode        *string
	Status        *string
	JoinMode      *string
	AutoJoinRules map[string]any
	CostType      *string
	CostAmount    *int
	DailyLimit    *int
	TotalLimit    *int
	StartTime     *time.Time
	EndTime       *time.Time
}

type ActivityListQuery struct {
	AppID   int64
	Status  string
	Keyword string
	Page    int
	Limit   int
}

type ActivityListResult struct {
	Items      []Activity `json:"items"`
	Page       int        `json:"page"`
	Limit      int        `json:"limit"`
	Total      int64      `json:"total"`
	TotalPages int        `json:"totalPages"`
}

// ── 奖品 ──

type Prize struct {
	ID         int64          `json:"id"`
	ActivityID int64          `json:"activityId"`
	Name       string         `json:"name"`
	Type       string         `json:"type"`    // points | experience | item | coupon | custom
	Value      string         `json:"value"`   // 数量或描述
	ImageURL   string         `json:"imageUrl,omitempty"`
	Quantity   int            `json:"quantity"` // -1 = 无限
	Used       int            `json:"used"`
	Weight     int            `json:"weight"`
	Position   int            `json:"position"`
	IsDefault  bool           `json:"isDefault"` // 保底奖
	Extra      map[string]any `json:"extra,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
	UpdatedAt  time.Time      `json:"updatedAt"`
}

type PrizeMutation struct {
	ID        int64
	Name      *string
	Type      *string
	Value     *string
	ImageURL  *string
	Quantity  *int
	Weight    *int
	Position  *int
	IsDefault *bool
	Extra     map[string]any
}

// ── 参与记录 ──

type Participant struct {
	ID         int64     `json:"id"`
	ActivityID int64     `json:"activityId"`
	UserID     int64     `json:"userId"`
	JoinType   string    `json:"joinType"` // manual | auto
	JoinedAt   time.Time `json:"joinedAt"`
}

// ── 抽奖记录 ──

type Draw struct {
	ID            int64          `json:"id"`
	ActivityID    int64          `json:"activityId"`
	UserID        int64          `json:"userId"`
	PrizeID       int64          `json:"prizeId"`
	PrizeSnapshot map[string]any `json:"prizeSnapshot"`
	DrawSeed      string         `json:"drawSeed,omitempty"`
	DrawProof     string         `json:"drawProof,omitempty"`
	Status        string         `json:"status"` // awarded | claimed | expired
	ClaimedAt     *time.Time     `json:"claimedAt,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
}

type DrawListQuery struct {
	ActivityID int64
	UserID     int64
	Status     string
	Page       int
	Limit      int
}

type DrawListResult struct {
	Items      []Draw `json:"items"`
	Page       int    `json:"page"`
	Limit      int    `json:"limit"`
	Total      int64  `json:"total"`
	TotalPages int    `json:"totalPages"`
}

// ── 种子承诺 ──

type SeedCommitment struct {
	ID          int64      `json:"id"`
	ActivityID  int64      `json:"activityId"`
	Round       int        `json:"round"`
	SeedHash    string     `json:"seedHash"`
	SeedValue   string     `json:"seedValue,omitempty"`
	CommittedAt time.Time  `json:"committedAt"`
	RevealedAt  *time.Time `json:"revealedAt,omitempty"`
	ChainTxHash string     `json:"chainTxHash,omitempty"`
}

// ── 抽奖结果 ──

type DrawResult struct {
	Draw       Draw   `json:"draw"`
	Prize      Prize  `json:"prize"`
	SeedHash   string `json:"seedHash,omitempty"`
	DrawSeed   string `json:"drawSeed,omitempty"`
	DrawProof  string `json:"drawProof,omitempty"`
	PrizeIndex int    `json:"prizeIndex"`
}

// ── 验证结果 ──

type VerifyResult struct {
	Valid        bool   `json:"valid"`
	SeedHash     string `json:"seedHash"`
	SeedValue    string `json:"seedValue,omitempty"`
	ChainTxHash  string `json:"chainTxHash,omitempty"`
	ChainNetwork string `json:"chainNetwork,omitempty"`
	Message      string `json:"message"`
}

// ── 统计 ──

type ActivityStats struct {
	ActivityID    int64            `json:"activityId"`
	TotalDraws    int64            `json:"totalDraws"`
	TotalUsers    int64            `json:"totalUsers"`
	Participants  int64            `json:"participants"`
	PrizeStats    []PrizeStatItem  `json:"prizeStats"`
}

type PrizeStatItem struct {
	PrizeID  int64  `json:"prizeId"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Quantity int    `json:"quantity"`
	Used     int    `json:"used"`
	Weight   int    `json:"weight"`
}
