package leaderboard

import (
	"time"

	"aegis/internal/domain/cosmetic"
)

// 用户端的数据形状（字段与 docs/leaderboard.md 的「用户端接口」逐字对应）。

// ClientTier 一档奖励的用户端描述。
type ClientTier struct {
	RankFrom int            `json:"rankFrom"`
	RankTo   int            `json:"rankTo"`
	Label    string         `json:"label"`
	Rewards  []ClientReward `json:"rewards"`
}

// ClientBoard 一个榜单与它的当前周期。
type ClientBoard struct {
	Key         string       `json:"key"`
	Label       string       `json:"label"`
	Metric      string       `json:"metric"`
	Period      string       `json:"period"`
	PeriodKey   string       `json:"periodKey"`
	PeriodLabel string       `json:"periodLabel"`
	PeriodStart *time.Time   `json:"periodStart"`
	PeriodEnd   *time.Time   `json:"periodEnd"`
	ValueUnit   string       `json:"valueUnit"`
	Rewards     []ClientTier `json:"rewards"`
}

// ClientMeState 我的偏好与状态（首页用）。
type ClientMeState struct {
	Anonymous      bool `json:"anonymous"`
	AllowAnonymous bool `json:"allowAnonymous"`
	Excluded       bool `json:"excluded"`
}

// ClientHome GET /leaderboard/boards。
type ClientHome struct {
	Enabled bool          `json:"enabled"`
	Boards  []ClientBoard `json:"boards"`
	Me      ClientMeState `json:"me"`
}

// ClientRow 榜上的一行。匿名的人 userId 省略、avatar 为空、name 为「匿名用户」（自己那一行例外）。
type ClientRow struct {
	Rank      int    `json:"rank"`
	UserID    *int64 `json:"userId,omitempty"`
	Name      string `json:"name"`
	Avatar    string `json:"avatar"`
	Value     int64  `json:"value"`
	Anonymous bool   `json:"anonymous"`
	IsMe      bool   `json:"isMe"`
	LevelName string `json:"levelName,omitempty"`
	// Cosmetics 这个人身上的装扮（头像框、昵称样式、勋章；docs/cosmetics.md），由传输层并进来；
	// 匿名上榜、没戴装扮或应用未开启装扮时省略
	Cosmetics *cosmetic.Loadout `json:"cosmetics,omitempty"`
	// AvatarOwner / RawAvatar 由传输层翻成永久头像地址（匿名的人为 0，不出头像）
	AvatarOwner int64  `json:"-"`
	RawAvatar   string `json:"-"`
}

// ClientMe 我在这个榜的名次（实时）。
type ClientMe struct {
	Rank      *int   `json:"rank"`
	Value     int64  `json:"value"`
	GapToPrev int64  `json:"gapToPrev"`
	InTop     bool   `json:"inTop"`
	Anonymous bool   `json:"anonymous"`
	Excluded  bool   `json:"excluded"`
	Name      string `json:"name"`
	Avatar    string `json:"avatar"`
	LevelName string `json:"levelName,omitempty"`

	AvatarOwner int64  `json:"-"`
	RawAvatar   string `json:"-"`
}

// ClientBoardPage GET /leaderboard/boards/:key。
type ClientBoardPage struct {
	Board      ClientBoard `json:"board"`
	Total      int64       `json:"total"`
	TopLimit   int         `json:"topLimit"`
	Page       int         `json:"page"`
	Limit      int         `json:"limit"`
	TotalPages int         `json:"totalPages"`
	Items      []ClientRow `json:"items"`
	Me         ClientMe    `json:"me"`
	UpdatedAt  time.Time   `json:"updatedAt"`
}

// ClientMyPeriod 我在某一期的名次与奖励。
type ClientMyPeriod struct {
	Rank         int            `json:"rank"`
	Value        int64          `json:"value"`
	RewardStatus string         `json:"rewardStatus"`
	Rewards      []RewardResult `json:"rewards,omitempty"`
}

// ClientPeriod 往期的一期。
type ClientPeriod struct {
	PeriodKey    string          `json:"periodKey"`
	PeriodLabel  string          `json:"periodLabel"`
	PeriodStart  time.Time       `json:"periodStart"`
	PeriodEnd    time.Time       `json:"periodEnd"`
	Participants int64           `json:"participants"`
	SettledAt    *time.Time      `json:"settledAt"`
	My           *ClientMyPeriod `json:"my,omitempty"`
}

// ClientPeriods GET /leaderboard/boards/:key/periods。
type ClientPeriods struct {
	Items []ClientPeriod `json:"items"`
}

// ClientPeriodPage GET /leaderboard/boards/:key/periods/:periodKey。
type ClientPeriodPage struct {
	Board      ClientBoard     `json:"board"`
	Period     ClientPeriod    `json:"period"`
	Total      int64           `json:"total"`
	Page       int             `json:"page"`
	Limit      int             `json:"limit"`
	TotalPages int             `json:"totalPages"`
	Items      []ClientRow     `json:"items"`
	Me         *ClientMyPeriod `json:"me"`
	Rewards    []ClientTier    `json:"rewards"`
}

// ClientRewardRecord 我的一条榜单奖励。
type ClientRewardRecord struct {
	Board       string         `json:"board"`
	BoardLabel  string         `json:"boardLabel"`
	PeriodKey   string         `json:"periodKey"`
	PeriodLabel string         `json:"periodLabel"`
	Rank        int            `json:"rank"`
	Value       int64          `json:"value"`
	Status      string         `json:"status"`
	Rewards     []RewardResult `json:"rewards"`
	GrantedAt   *time.Time     `json:"grantedAt"`
}

// ClientPreference GET/PUT /leaderboard/preference。
type ClientPreference struct {
	Anonymous      bool `json:"anonymous"`
	AllowAnonymous bool `json:"allowAnonymous"`
}

// RewardRecord 仓储读出的一条奖励记录（我的奖励记录用）。
type RewardRecord struct {
	SnapshotID    int64
	Board         string
	PeriodKey     string
	PeriodStart   time.Time
	PeriodEnd     time.Time
	Rank          int
	Value         int64
	Status        string
	RewardResults []RewardResult
	GrantedAt     *time.Time
}

// PresentRow 把一行榜单事实翻成用户端的一行。
// anonymousActive：这个人开了匿名且应用允许匿名；isMe 那一行例外（自己看自己显示真名）。
func PresentRow(rank int, userID int64, name, rawAvatar string, value int64, levelName string,
	anonymousActive bool, meID int64) ClientRow {
	row := ClientRow{Rank: rank, Value: value, LevelName: levelName, Anonymous: anonymousActive, IsMe: userID == meID}
	if anonymousActive && !row.IsMe {
		row.Name = AnonymousName
		return row
	}
	id := userID
	row.UserID = &id
	row.Name = name
	row.AvatarOwner = userID
	row.RawAvatar = rawAvatar
	return row
}
