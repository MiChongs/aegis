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

type LevelConfig struct {
	ID                 int64          `json:"id"`
	Level              int            `json:"level"`
	LevelName          string         `json:"levelName"`
	ExperienceRequired int64          `json:"experienceRequired"`
	ExperienceNext     *int64         `json:"experienceNext,omitempty"`
	ExpMultiplier      float64        `json:"expMultiplier"`
	Icon               string         `json:"icon,omitempty"`
	Color              string         `json:"color,omitempty"`
	Privileges         []any          `json:"privileges,omitempty"`
	Rewards            map[string]any `json:"rewards,omitempty"`
	Description        string         `json:"description,omitempty"`
	IsActive           bool           `json:"isActive"`
	SortOrder          int            `json:"sortOrder"`
	CreatedAt          time.Time      `json:"createdAt"`
	UpdatedAt          time.Time      `json:"updatedAt"`
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
}

type ExperienceAdjustResult struct {
	UserID        int64     `json:"userId"`
	AppID         int64     `json:"appid"`
	Account       string    `json:"account"`
	Amount        int64     `json:"amount"`
	BeforeAmount  int64     `json:"beforeAmount"`
	AfterAmount   int64     `json:"afterAmount"`
	Reason        string    `json:"reason"`
	OperationType string    `json:"operationType"`
	TransactionNo string    `json:"transactionNo"`
	LevelChanged  bool      `json:"levelChanged"`
	OldLevel      int       `json:"oldLevel"`
	NewLevel      int       `json:"newLevel"`
	CreatedAt     time.Time `json:"createdAt"`
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
}
