package points

import "time"

type Transaction struct {
	ID            int64          `json:"id"`
	TransactionNo string         `json:"transactionNo"`
	UserID        int64          `json:"userId"`
	AppID         int64          `json:"appid"`
	Type          string         `json:"type"`
	Category      string         `json:"category"`
	Amount        int64          `json:"amount"`
	BalanceBefore int64          `json:"balanceBefore"`
	BalanceAfter  int64          `json:"balanceAfter"`
	LevelBefore   int            `json:"levelBefore,omitempty"`
	LevelAfter    int            `json:"levelAfter,omitempty"`
	Status        string         `json:"status"`
	Title         string         `json:"title"`
	Description   string         `json:"description,omitempty"`
	SourceID      *int64         `json:"sourceId,omitempty"`
	SourceType    string         `json:"sourceType,omitempty"`
	Multiplier    float64        `json:"multiplier"`
	IsLevelUp     bool           `json:"isLevelUp,omitempty"`
	ClientIP      string         `json:"clientIp,omitempty"`
	UserAgent     string         `json:"userAgent,omitempty"`
	ExtraData     map[string]any `json:"extraData,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
}

type Overview struct {
	Integral                int64         `json:"integral"`
	Experience              int64         `json:"experience"`
	TotalSignIns            int64         `json:"totalSignIns"`
	TotalIntegralEarned     int64         `json:"totalIntegralEarned"`
	TotalExperienceEarned   int64         `json:"totalExperienceEarned"`
	LevelInfo               *LevelInfo    `json:"levelInfo,omitempty"`
	RecentIntegralRecords   []Transaction `json:"recentIntegralRecords"`
	RecentExperienceRecords []Transaction `json:"recentExperienceRecords"`
}

// LevelConfig 一级的展示配置（GET /points/levels）。来自应用的等级体系（docs/levels.md）。
type LevelConfig struct {
	ID                 int64   `json:"id"`
	Level              int     `json:"level"`
	LevelName          string  `json:"levelName"`
	ExperienceRequired int64   `json:"experienceRequired"`
	ExperienceNext     *int64  `json:"experienceNext,omitempty"`
	ExpMultiplier      float64 `json:"expMultiplier"`
	Icon               string  `json:"icon,omitempty"`
	Color              string  `json:"color,omitempty"`
	// Privileges 这一级上配置的权益（不含继承）；Effective 到达这一级时实际享有的全部权益
	Privileges  []LevelPrivilege  `json:"privileges"`
	Effective   []LevelPrivilege  `json:"effective"`
	Rewards     []LevelRewardInfo `json:"rewards"`
	Gate        *LevelGateInfo    `json:"gate,omitempty"`
	Description string            `json:"description,omitempty"`
	IsActive    bool              `json:"isActive"`
	SortOrder   int               `json:"sortOrder"`
	// Reached 当前用户是否已达到（列表按用户返回时才有意义）
	Reached   bool      `json:"reached"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// LevelPrivilege 一条等级权益。Kind：exp_boost / integral_boost / display / custom（App 按 Key 判断）。
type LevelPrivilege struct {
	Kind  string  `json:"kind"`
	Label string  `json:"label"`
	Key   string  `json:"key,omitempty"`
	Value float64 `json:"value,omitempty"`
	Text  string  `json:"text,omitempty"`
	Icon  string  `json:"icon,omitempty"`
	// Level 权益来自哪一级
	Level int `json:"level"`
}

// LevelRewardInfo 一份达成奖励。Claim：auto 升级自动到账 / manual 在 App 里领取。
type LevelRewardInfo struct {
	NodeID string   `json:"nodeId"`
	Label  string   `json:"label"`
	Items  []string `json:"items"`
	Claim  string   `json:"claim"`
}

// LevelGateInfo 晋级条件（给用户看的说明）。
type LevelGateInfo struct {
	Label string `json:"label,omitempty"`
	Hint  string `json:"hint,omitempty"`
}

type LevelInfo struct {
	CurrentLevel       int          `json:"currentLevel"`
	CurrentLevelName   string       `json:"currentLevelName"`
	ExpMultiplier      float64      `json:"expMultiplier"`
	TotalExperience    int64        `json:"totalExperience"`
	ExpInCurrentLevel  int64        `json:"expInCurrentLevel"`
	ExpRangeForLevel   int64        `json:"expRangeForLevel"`
	ExpToNextLevel     int64        `json:"expToNextLevel"`
	LevelProgress      float64      `json:"levelProgress"`
	CurrentLevelMinExp int64        `json:"currentLevelMinExp"`
	NextLevel          int          `json:"nextLevel,omitempty"`
	NextLevelName      string       `json:"nextLevelName,omitempty"`
	NextLevelMinExp    *int64       `json:"nextLevelMinExp,omitempty"`
	HighestLevel       int          `json:"highestLevel"`
	LevelUpCount       int          `json:"levelUpCount"`
	LastLevelUpAt      *time.Time   `json:"lastLevelUpAt,omitempty"`
	CurrentLevelConfig *LevelConfig `json:"currentLevelConfig,omitempty"`
	NextLevelConfig    *LevelConfig `json:"nextLevelConfig,omitempty"`
	IsMaxLevel         bool         `json:"isMaxLevel"`
	Icon               string       `json:"icon,omitempty"`
	Color              string       `json:"color,omitempty"`
	Description        string       `json:"description,omitempty"`
	MaxLevel           int          `json:"maxLevel"`
	// Privileges 当前实际享有的全部等级权益（含继承）
	Privileges []LevelPrivilege `json:"privileges"`
	// ClaimableRewards 待领取的达成奖励数
	ClaimableRewards int `json:"claimableRewards"`
	// BlockedBy 经验已够、但卡在下一级的晋级条件上
	BlockedBy *LevelGateInfo `json:"blockedBy,omitempty"`
	// CappedLevel 管理员设的等级封顶（成长调控），0 = 不封顶；CapReached 已经停在封顶处
	CappedLevel   int    `json:"cappedLevel,omitempty"`
	CapReached    bool   `json:"capReached,omitempty"`
	SchemeName    string `json:"schemeName,omitempty"`
	SchemeVersion int    `json:"schemeVersion"`
	// HideLocked 体系要求 App 不展示还没达到的等级
	HideLocked bool `json:"hideLocked,omitempty"`
}

type LevelUserInfo struct {
	ID         int64  `json:"id"`
	Account    string `json:"account"`
	Nickname   string `json:"nickname,omitempty"`
	Avatar     string `json:"avatar,omitempty"`
	Integral   int64  `json:"integral"`
	Experience int64  `json:"experience"`
}

type LevelProfile struct {
	UserInfo  LevelUserInfo `json:"userInfo"`
	LevelInfo LevelInfo     `json:"levelInfo"`
}

type RankingItem struct {
	Rank            int        `json:"rank"`
	UserID          int64      `json:"userId"`
	Account         string     `json:"account"`
	Nickname        string     `json:"nickname,omitempty"`
	Avatar          string     `json:"avatar,omitempty"`
	Value           int64      `json:"value"`
	Type            string     `json:"type"`
	LevelName       string     `json:"levelName,omitempty"`
	Experience      int64      `json:"experience,omitempty"`
	Progress        float64    `json:"progress,omitempty"`
	ConsecutiveDays int        `json:"consecutiveDays,omitempty"`
	TotalSignDays   int64      `json:"totalSignDays,omitempty"`
	LastSignDate    string     `json:"lastSignDate,omitempty"`
	SignedAt        *time.Time `json:"signedAt,omitempty"`
	LastSignAt      *time.Time `json:"lastSignAt,omitempty"`
	Period          string     `json:"period,omitempty"`
}

type RankingResponse struct {
	Type       string        `json:"type"`
	Page       int           `json:"page"`
	Limit      int           `json:"limit"`
	Total      int64         `json:"total"`
	Items      []RankingItem `json:"items"`
	MyRank     *RankingItem  `json:"myRank,omitempty"`
	TotalPages int           `json:"totalPages"`
}

type AppStatsOverview struct {
	TotalUsers  int64  `json:"total_users"`
	ActiveUsers int64  `json:"active_users"`
	ActiveRate  string `json:"active_rate"`
	TimeRange   int    `json:"time_range"`
}

type AppTransactionCategoryStat struct {
	Type        string `json:"type"`
	Category    string `json:"category"`
	Count       int64  `json:"count"`
	TotalAmount int64  `json:"total_amount"`
	LevelUps    int64  `json:"level_ups,omitempty"`
}

type AppTopUser struct {
	ID         int64  `json:"id"`
	Account    string `json:"account"`
	Nickname   string `json:"nickname,omitempty"`
	Avatar     string `json:"avatar,omitempty"`
	Integral   int64  `json:"integral,omitempty"`
	Experience int64  `json:"experience,omitempty"`
}

type AppDailyTransactionStat struct {
	Date                       string `json:"date"`
	IntegralTransactionCount   int64  `json:"integral_transaction_count"`
	IntegralEarned             int64  `json:"integral_earned"`
	IntegralConsumed           int64  `json:"integral_consumed"`
	IntegralActiveUsers        int64  `json:"integral_active_users"`
	ExperienceTransactionCount int64  `json:"experience_transaction_count"`
	ExperienceGained           int64  `json:"experience_gained"`
	ExperienceLevelUps         int64  `json:"experience_level_ups"`
	ExperienceActiveUsers      int64  `json:"experience_active_users"`
}

type AppStatistics struct {
	AppID      int64                     `json:"appid"`
	Overview   AppStatsOverview          `json:"overview"`
	Integral   AppIntegralStatistics     `json:"integral"`
	Experience AppExperienceStatistics   `json:"experience"`
	DailyStats []AppDailyTransactionStat `json:"daily_stats"`
}

type AppIntegralStatistics struct {
	Stats    []AppTransactionCategoryStat `json:"stats"`
	TopUsers []AppTopUser                 `json:"top_users"`
}

type AppExperienceStatistics struct {
	Stats    []AppTransactionCategoryStat `json:"stats"`
	TopUsers []AppTopUser                 `json:"top_users"`
}

type AdminAdjustOptions struct {
	AdminID      int64
	AdminAccount string
	ClientIP     string
	UserAgent    string
	// Extra 并入流水 extra_data 的附加字段（如冲正时的 reversalOf）
	Extra map[string]any
}

type IntegralAdjustResult struct {
	UserID        int64     `json:"userId"`
	AppID         int64     `json:"appid"`
	Account       string    `json:"account"`
	Amount        int64     `json:"amount"`
	BeforeAmount  int64     `json:"beforeAmount"`
	AfterAmount   int64     `json:"afterAmount"`
	Reason        string    `json:"reason"`
	OperationType string    `json:"operationType"`
	TransactionNo string    `json:"transactionNo"`
	CreatedAt     time.Time `json:"createdAt"`
	// Replayed 为真表示同一请求编号已经生效过，这里返回的是首次的结果，没有再次变更余额
	Replayed bool `json:"replayed,omitempty"`
}

type ExperienceAdjustResult struct {
	UserID        int64  `json:"userId"`
	AppID         int64  `json:"appid"`
	Account       string `json:"account"`
	Amount        int64  `json:"amount"`
	BeforeAmount  int64  `json:"beforeAmount"`
	AfterAmount   int64  `json:"afterAmount"`
	Reason        string `json:"reason"`
	OperationType string `json:"operationType"`
	TransactionNo string `json:"transactionNo"`
	LevelChanged  bool   `json:"levelChanged"`
	OldLevel      int    `json:"oldLevel"`
	NewLevel      int    `json:"newLevel"`
	// LevelMode 扣减时的等级处理（keep / recalc），增加时为空
	LevelMode LevelMode `json:"levelMode,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	// Replayed 为真表示同一请求编号已经生效过，这里返回的是首次的结果，没有再次变更余额
	Replayed bool `json:"replayed,omitempty"`
}

// BatchExperienceAdjustResult 批量调整经验：逐人在自己的事务里执行（等级要逐人判定）。
type BatchExperienceAdjustResult struct {
	AppID          int64                    `json:"appid"`
	OperationType  string                   `json:"operationType"`
	Amount         int64                    `json:"amount"`
	LevelMode      LevelMode                `json:"levelMode,omitempty"`
	RequestedCount int                      `json:"requestedCount"`
	SuccessCount   int                      `json:"successCount"`
	FailedCount    int                      `json:"failedCount"`
	Results        []ExperienceAdjustResult `json:"results"`
	Failures       []BatchAdjustFailure     `json:"failures"`
}

type BatchAdjustFailure struct {
	UserID int64  `json:"userId"`
	Error  string `json:"error"`
}

type BatchIntegralAdjustResult struct {
	AppID          int64                  `json:"appid"`
	OperationType  string                 `json:"operationType"`
	Amount         int64                  `json:"amount"`
	RequestedCount int                    `json:"requestedCount"`
	SuccessCount   int                    `json:"successCount"`
	FailedCount    int                    `json:"failedCount"`
	Results        []IntegralAdjustResult `json:"results"`
	Failures       []BatchAdjustFailure   `json:"failures"`
	Replayed       bool                   `json:"replayed,omitempty"`
}
