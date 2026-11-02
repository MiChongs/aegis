// Package lottery 应用级抽奖：活动、奖品、抽奖记录与每个人的抽奖次数。
//
// 结果只在服务端产生：客户端只说「我要用哪一种次数抽一次」，抽中什么、发不发、发多少
// 全部在同一个数据库事务里决定并到账，客户端拿到的只是这次的结果。不存在「客户端上报抽中了 X，
// 服务端照着发」这条路 —— 那等于把奖池交给了任何能抓包的人。
//
// 次数有三种来源，各自计数、互不挤占：
//
//   - free   每人每天的免费次数（活动配置）
//   - bonus  应用级的抽奖次数余额：看激励广告、卡密、积分商城、邀请等发的「抽奖次数」，分批记账（见 tickets.go）
//   - points 每次消耗若干积分（活动配置，可设每日上限）
//
// 扣除顺序只有一条：今日免费 → 赠送的次数 → 兑换的次数 → 积分（ConsumptionOrder）。客户端指定 source 时
// 它只是「最多用到这一档」，排在它前面、更便宜的还有剩就先用那个，免费次数永远先扣。
//
// 「视频抽奖」不是第四种来源：活动指定一个激励广告场景，那个场景发的是抽奖次数，
// 广告看完、两方确认之后次数进 bonus，客户端再用 bonus 抽。看没看完由广告平台的服务端回调说了算。
package lottery

import (
	"time"

	"aegis/internal/domain/pointstore"
)

// 界面形态：九宫格固定 8 个奖品；转盘 2–12 个。
const (
	UIModeGrid  = "grid"
	UIModeWheel = "wheel"
)

// 活动状态。只有 active 且在时间窗内的活动能抽。
const (
	StatusDraft  = "draft"
	StatusActive = "active"
	StatusPaused = "paused"
	StatusEnded  = "ended"
)

// 次数来源。
const (
	SourceFree   = "free"
	SourceBonus  = "bonus"
	SourcePoints = "points"
)

// 抽奖记录状态。
const (
	// DrawGranted 奖品已在抽奖事务里到账（含「谢谢参与」）
	DrawGranted = "granted"
	// DrawPending 人工发放的奖品，等管理员处理
	DrawPending = "pending"
	// DrawFulfilled 人工发放的奖品已由管理员标记发放
	DrawFulfilled = "fulfilled"
)

// 规模上界。
const (
	GridPrizeCount      = 8
	WheelMinPrizes      = 2
	WheelMaxPrizes      = 12
	MaxNameRunes        = 32
	MaxDescriptionRunes = 200
	MaxRulesRunes       = 2000
	MaxPrizeNameRunes   = 16
	MaxFreeDaily        = 100
	MaxPointsDaily      = 1000
	MaxPointsCost       = 1_000_000
	MaxWeight           = 1_000_000
	MaxQuantity         = 100_000_000
	MaxPityThreshold    = 1000
	MaxNoteRunes        = 500
	MaxRequestIDLength  = 64
	// MaxBatchDraws 一次连抽最多几次（十连抽）
	MaxBatchDraws = 10
	// MaxBatchRequestIDLength 连抽的 requestId 上限：每一抽用「requestId#序号」，要留出后缀
	MaxBatchRequestIDLength = MaxRequestIDLength - 3
)

// Reward 奖品发的东西，与积分商城同构（同一套账本、同一套描述）。
type Reward = pointstore.Reward

// RewardResult 一次发放的结果。
type RewardResult = pointstore.RewardResult

// Activity 一个抽奖活动。
type Activity struct {
	ID          int64      `json:"id"`
	AppID       int64      `json:"appid"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Rules       string     `json:"rules"`
	UIMode      string     `json:"uiMode"`
	Status      string     `json:"status"`
	StartTime   *time.Time `json:"startTime,omitempty"`
	EndTime     *time.Time `json:"endTime,omitempty"`
	// FreeDaily 每人每天免费抽几次，0 为不送
	FreeDaily int `json:"freeDaily"`
	// PointsCost 每次消耗多少积分，0 为不开放积分抽奖；PointsDaily 积分抽奖每天上限，0 为不限
	PointsCost  int64 `json:"pointsCost"`
	PointsDaily int   `json:"pointsDaily"`
	// VideoScene 激励广告场景，看完发抽奖次数；空为不开放视频抽奖
	VideoScene string `json:"videoScene"`
	// PityThreshold 连续这么多次没抽中（谢谢参与）时，这一次必中 PityPrizeID；0 为关闭
	PityThreshold int    `json:"pityThreshold"`
	PityPrizeID   *int64 `json:"pityPrizeId,omitempty"`
	// LifetimePolicy 抽中已拥有的奖品（永久会员抽中会员等）怎么办，见 lifetime.go
	LifetimePolicy string `json:"lifetimePolicy"`
	// LifetimePointsPerDay 折算积分时每天会员 / 功能折多少积分；LifetimePointsLifetime 永久套餐 / 永久功能折多少
	LifetimePointsPerDay   int64 `json:"lifetimePointsPerDay"`
	LifetimePointsLifetime int64 `json:"lifetimePointsLifetime"`
	// LifetimeDraws 改发抽奖次数时发几次
	LifetimeDraws int64 `json:"lifetimeDraws"`
	// Strategy 多维度出奖策略，见 strategy.go；零值为纯加权随机 + 硬保底
	Strategy Strategy `json:"strategy"`
	// Perks 人群特权，见 perks.go
	Perks     []Perk    `json:"perks"`
	SortOrder int       `json:"sortOrder"`
	CreatedBy string    `json:"createdBy,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Prize 一个奖品，占奖盘上的一格。
type Prize struct {
	ID         int64  `json:"id"`
	ActivityID int64  `json:"activityId"`
	Name       string `json:"name"`
	// Kind 与 Reward.Type 相同；「谢谢参与」为 none，Reward 只有 Type
	Kind     string `json:"kind"`
	Reward   Reward `json:"reward"`
	ImageURL string `json:"imageUrl"`
	// Quantity 总库存，-1 为不限；Used 已抽中次数
	Quantity int64 `json:"quantity"`
	Used     int64 `json:"used"`
	// Weight 抽中权重，0 表示不参与常规抽取（只作保底奖或兜底奖）
	Weight int `json:"weight"`
	// Position 在奖盘上的位置（九宫格为外圈顺时针 0–7）
	Position int `json:"position"`
	// Fallback 抽中的奖品恰好被抢光时改发它（必须不限库存）
	Fallback bool `json:"fallback"`
	// DailyLimit 全站每天最多出几个；UserLimit 每人最多中几次；UserDailyLimit 每人每天最多中几次。0 为不限
	DailyLimit     int `json:"dailyLimit"`
	UserLimit      int `json:"userLimit"`
	UserDailyLimit int `json:"userDailyLimit"`
	// Value 奖品的积分当量（护栏的期望价值用）；0 为按类型估算，见 PrizeValue
	Value int64 `json:"value"`
	// LifetimeReward 抽中的人已拥有这个奖品时改发什么（积分 / 经验 / 抽奖次数）；空为按活动的处理方式折算
	LifetimeReward *Reward   `json:"lifetimeReward,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// InStock 还有没有库存。
func (p Prize) InStock() bool {
	return p.Quantity < 0 || p.Used < p.Quantity
}

// Winning 是否算「抽中」：除了「谢谢参与」都算。
func (p Prize) Winning() bool {
	return p.Kind != KindNone
}

// ActivityInput 新建 / 整体更新活动（含全部奖品）。
//
// 奖品与活动一起保存：九宫格要求恰好 8 格、保底奖必须是本活动的奖品、
// 至少一个不限库存的奖品 —— 这些都是「整张奖盘」的约束，分开保存就会有
// 「改到一半违反约束」的中间态。
type ActivityInput struct {
	AppID         int64
	Name          string
	Description   string
	Rules         string
	UIMode        string
	Status        string
	StartTime     *time.Time
	EndTime       *time.Time
	FreeDaily     int
	PointsCost    int64
	PointsDaily   int
	VideoScene    string
	PityThreshold int
	// PityPosition 保底奖在 Prizes 里的 Position（奖品是新建时还没有 ID）；-1 为不设
	PityPosition           int
	LifetimePolicy         string
	LifetimePointsPerDay   int64
	LifetimePointsLifetime int64
	LifetimeDraws          int64
	Strategy               Strategy
	Perks                  []Perk
	SortOrder              int
	Prizes                 []PrizeInput
	Operator               string
}

// PrizeInput 一个奖品的输入；ID 为 0 表示新建，非 0 表示更新已有奖品（保留已抽中计数）。
type PrizeInput struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Reward   Reward `json:"reward"`
	ImageURL string `json:"imageUrl"`
	Quantity int64  `json:"quantity"`
	Weight   int    `json:"weight"`
	Position int    `json:"position"`
	Fallback bool   `json:"fallback"`
	// 限量：全站每日 / 每人累计 / 每人每日，0 为不限
	DailyLimit     int `json:"dailyLimit"`
	UserLimit      int `json:"userLimit"`
	UserDailyLimit int `json:"userDailyLimit"`
	// Value 积分当量，0 为按类型估算
	Value int64 `json:"value"`
	// LifetimeReward 已拥有时改发什么；nil 为按活动的处理方式
	LifetimeReward *Reward `json:"lifetimeReward,omitempty"`
}

// AdminActivity 管理端列表上的一行：活动 + 奖品 + 汇总。
type AdminActivity struct {
	Activity
	Prizes     []Prize `json:"prizes"`
	TotalDraws int64   `json:"totalDraws"`
	TotalUsers int64   `json:"totalUsers"`
	TodayDraws int64   `json:"todayDraws"`
	Pending    int64   `json:"pending"`
	// Economy 积分账与套利检查（见 economy.go）；取不到时为空
	Economy *ActivityEconomy `json:"economy,omitempty"`
}

// Draw 一次抽奖。奖品信息存快照：奖品被改、被删之后，记录说的仍是当时抽中的那一份。
type Draw struct {
	ID           int64  `json:"id"`
	DrawNo       string `json:"drawNo"`
	AppID        int64  `json:"appid"`
	ActivityID   int64  `json:"activityId"`
	ActivityName string `json:"activityName,omitempty"`
	UserID       int64  `json:"userId"`
	Account      string `json:"account,omitempty"`
	Nickname     string `json:"nickname,omitempty"`
	PrizeID      *int64 `json:"prizeId,omitempty"`
	PrizeName    string `json:"prizeName"`
	PrizeKind    string `json:"prizeKind"`
	// RewardType 用户端：奖品的真实类型（PrizeKind 对老客户端按 ClientKind 映射过）
	RewardType string `json:"rewardType,omitempty"`
	Reward     Reward `json:"reward"`
	Position   int    `json:"position"`
	Source     string `json:"source"`
	// Ticket source = bonus 时用的是哪一批次数（来源、是否兑换所得）
	Ticket     *DrawTicket    `json:"ticket,omitempty"`
	PointsCost int64          `json:"pointsCost,omitempty"`
	Pity       bool           `json:"pity,omitempty"`
	Status     string         `json:"status"`
	Results    []RewardResult `json:"results"`
	// Conversion 抽中的奖品对这个人是已拥有的、改发了别的东西时的说明（Reward 仍是抽中的那一格）
	Conversion *Conversion `json:"conversion,omitempty"`
	Note       string      `json:"note,omitempty"`
	HandledBy  string      `json:"handledBy,omitempty"`
	HandledAt  *time.Time  `json:"handledAt,omitempty"`
	RequestID  string      `json:"requestId,omitempty"`
	DeviceID   string      `json:"deviceId,omitempty"`
	ClientIP   string      `json:"clientIp,omitempty"`
	// Trace 出奖方案（每个奖品的系数、最终概率、行为分）。只给管理端，客户端接口返回前清空
	Trace *DrawPlan `json:"trace,omitempty"`
	// RiskRules / RiskAction 命中的风控信号与最重的处置（只给管理端）
	RiskRules  []string `json:"riskRules,omitempty"`
	RiskAction string   `json:"riskAction,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// DrawQuery 抽奖记录筛选。
type DrawQuery struct {
	AppID      int64
	ActivityID int64
	UserID     int64
	Status     string
	Source     string
	// Winning 只看抽中的（排除谢谢参与）
	Winning bool
	// Keyword 检索式，见 ParseDrawSearch
	Keyword string
	// Risk any = 命中过风控，clean = 没命中，其余为信号键；RiskAction 按处置筛
	Risk       string
	RiskAction string
	// Outcome pity 保底 / fallback 改发兜底 / conversion 已拥有改发
	Outcome string
	PrizeID int64
	Start   *time.Time
	End     *time.Time
	Page    int
	Limit   int
}

// Page 分页结果。
type Page[T any] struct {
	Items      []T   `json:"items"`
	Total      int64 `json:"total"`
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	TotalPages int   `json:"totalPages"`
}

// DrawInput 用户抽一次。
type DrawInput struct {
	AppID      int64
	UserID     int64
	ActivityID int64
	// Source 最多用到哪一种次数；为空时不设上限。两种情况都按 free → bonus → points 的顺序取第一个可用的
	Source    string
	RequestID string
	DeviceID  string
	ClientIP  string
	UserAgent string
	// DayStart 「今天」从什么时候算起（应用时区的零点），由服务层算好传进来
	DayStart time.Time
	Now      time.Time
	// Location 应用时区（规则里的 draw.hour / draw.weekday 按它算）；空为 Now 自带的时区
	Location *time.Location
	// Profile 行为画像，服务层在事务外取好
	Profile BehaviorProfile
	// Video 视频抽奖的配置与今天的进度（enabled、dailyLimit、watched：免看广告按顺序解锁，见 WalkAdSkip），
	// 服务层按激励广告配置取好
	Video VideoChances
}

// DrawResult 一次抽奖的结果。Replayed 为 true 表示同一个 requestId 的重复提交，原样返回第一次的结果。
type DrawResult struct {
	Draw     Draw    `json:"draw"`
	Prize    *Prize  `json:"-"`
	Chances  Chances `json:"chances"`
	Pity     *Pity   `json:"pity,omitempty"`
	Replayed bool    `json:"replayed"`
}

// UserFacts 判定次数所需的事实，仓储层一次取齐。
type UserFacts struct {
	FreeUsedToday   int
	PointsUsedToday int
	Bonus           int64
	// BonusPurchased Bonus 里用积分或钱换来的那部分（排在赠送的之后用）
	BonusPurchased int64
	Integral       int64
	MissStreak     int
	TotalDraws     int64
}

// Chances 一个人在一个活动里现在能抽几次。
type Chances struct {
	Free  FreeChances `json:"free"`
	Bonus int64       `json:"bonus"`
	// Tickets Bonus 按档拆开：赠送的先用、兑换的后用
	Tickets TicketBalance `json:"tickets"`
	Points  PointsChances `json:"points"`
	Video   VideoChances  `json:"video"`
	// Next 「点一下抽奖」会用哪一种次数，空表示没有可用次数（客户端引导去看视频或明天再来）
	Next string `json:"next"`
}

// FreeChances 免费次数。
type FreeChances struct {
	Daily     int `json:"daily"`
	Used      int `json:"used"`
	Remaining int `json:"remaining"`
}

// PointsChances 积分抽奖。Remaining 为 -1 表示不限次数（仍受余额约束）。
type PointsChances struct {
	Enabled   bool  `json:"enabled"`
	Cost      int64 `json:"cost"`
	Daily     int   `json:"daily"`
	Used      int   `json:"used"`
	Remaining int   `json:"remaining"`
	Balance   int64 `json:"balance"`
	// Affordable 余额够不够抽一次
	Affordable bool `json:"affordable"`
}

// VideoChances 视频抽奖：激励广告场景今天还能看几次。
//
// 客户端用 scene / placementId / userId 去加载广告，看完按激励广告的流程上报，
// 到账的是抽奖次数（进 bonus），再用 bonus 抽。
type VideoChances struct {
	Enabled     bool   `json:"enabled"`
	Scene       string `json:"scene,omitempty"`
	PlacementID string `json:"placementId,omitempty"`
	// UserID 传给广告 SDK 的签名用户标识（与 /ads/rewarded 的 userId 相同）
	UserID string `json:"userId,omitempty"`
	// DrawsPerView 看一次发几次抽奖
	DrawsPerView int64 `json:"drawsPerView"`
	DailyLimit   int   `json:"dailyLimit"`
	// Remaining 今天还能看几次，-1 为不限
	Remaining int `json:"remaining"`
	// Watched 今天已看完并发放的次数（只在服务端用：免看广告按顺序解锁要知道走到第几次）
	Watched           int  `json:"-"`
	CooldownRemaining int  `json:"cooldownRemaining"`
	Available         bool `json:"available"`
}

// Pity 保底进度。
type Pity struct {
	Threshold int    `json:"threshold"`
	PrizeID   int64  `json:"prizeId"`
	PrizeName string `json:"prizeName"`
	// Remaining 最多再抽几次必中（1 表示下一次必中）
	Remaining int `json:"remaining"`
}

// ClientActivity 用户端看到的活动。
type ClientActivity struct {
	ID          int64         `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Rules       string        `json:"rules"`
	UIMode      string        `json:"uiMode"`
	StartTime   *time.Time    `json:"startTime,omitempty"`
	EndTime     *time.Time    `json:"endTime,omitempty"`
	Prizes      []ClientPrize `json:"prizes"`
}

// ClientPrize 用户端看到的奖品：不含库存与权重，只带公示概率。
type ClientPrize struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	// Kind 对老客户端按 ClientKind 映射过的类型；RewardType 是真实类型
	Kind       string `json:"kind"`
	RewardType string `json:"rewardType"`
	Detail     string `json:"detail"`
	ImageURL   string `json:"imageUrl,omitempty"`
	Position   int    `json:"position"`
	// Probability 常规抽取下抽中它的概率（百分比，PublicProbability 取整：不小于 1% 保留两位小数、
	// 小于 1% 保留两位有效数字，抽得到的不会是 0），不含保底与库存的影响
	Probability float64 `json:"probability"`
	// BaseProbability 活动公示的概率：只去掉对他不参与抽取的已拥有奖品，不乘人群特权的中奖加成与今日运气。
	// 与 Probability 相比就是这个人今天的概率比公示高了还是低了
	BaseProbability float64 `json:"baseProbability"`
	SoldOut         bool    `json:"soldOut,omitempty"`
	// Swap 对当前用户来说这一格会怎样（已拥有时改发什么 / 不会抽到）；照常发放时没有
	Swap *PrizeSwap `json:"swap,omitempty"`
}

// ClientState 用户端的一个活动页：活动、我的次数、保底进度。
type ClientState struct {
	Activity ClientActivity `json:"activity"`
	Chances  Chances        `json:"chances"`
	Pity     *Pity          `json:"pity,omitempty"`
	// Lifetime 当前用户有已拥有的奖品时的提示（永久会员抽中会员奖品会怎样）
	//
	// 有生效的人群特权时，特权说明也并进这里（policy = perk，message 为整句）：老客户端只认这一张提示卡
	Lifetime *LifetimeNotice `json:"lifetime,omitempty"`
	// Perks 生效的人群特权
	Perks *PerkSummary `json:"perks,omitempty"`
	// Fortune 今日运气；活动没开每日运气时没有
	Fortune *FortuneReading `json:"fortune,omitempty"`
	// Viewer 当前用户的身份（客户端按它换永久会员的专属外观）
	Viewer     ClientViewer `json:"viewer"`
	ServerTime time.Time    `json:"serverTime"`
}

// ClientViewer 活动页上当前用户的身份。
type ClientViewer struct {
	Lifetime bool `json:"lifetime"`
}

// BatchResult 连抽的结果：逐次抽，每一次各自是一个事务、各自到账。
//
// 中途次数用完（或别的原因抽不了）时停下，已经抽到的照常返回，停下的原因在 Stopped；
// 第一次就抽不了时整个请求报那个错误，与单抽同一个码。
type BatchResult struct {
	Draws   []Draw  `json:"draws"`
	Chances Chances `json:"chances"`
	Pity    *Pity   `json:"pity,omitempty"`
	// Requested 要求抽几次；len(Draws) 是实际抽了几次
	Requested int        `json:"requested"`
	Stopped   *BatchStop `json:"stopped,omitempty"`
	// Replayed 同一个 requestId 的重复提交：每一次都是原样返回第一次的结果
	Replayed bool `json:"replayed"`
}

// BatchStop 连抽中途停下的原因：错误码与单抽时报的相同。
type BatchStop struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ClientList 用户端的活动列表。
type ClientList struct {
	Activities []ClientActivitySummary `json:"activities"`
	ServerTime time.Time               `json:"serverTime"`
}

// ClientActivitySummary 列表上的一个活动。
type ClientActivitySummary struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	UIMode      string     `json:"uiMode"`
	StartTime   *time.Time `json:"startTime,omitempty"`
	EndTime     *time.Time `json:"endTime,omitempty"`
}

// HandleInput 管理员处理一条人工发放的记录。
type HandleInput struct {
	AppID    int64
	DrawID   int64
	Note     string
	Operator string
}

// StatsDay 一天的统计。
type StatsDay struct {
	Date  string `json:"date"`
	Draws int64  `json:"draws"`
	Wins  int64  `json:"wins"`
	Users int64  `json:"users"`
}

// StatsSource 按次数来源的统计。
type StatsSource struct {
	Source string `json:"source"`
	Draws  int64  `json:"draws"`
}

// StatsPrize 按奖品的统计。
type StatsPrize struct {
	PrizeID  *int64 `json:"prizeId,omitempty"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Draws    int64  `json:"draws"`
	Quantity int64  `json:"quantity"`
	Used     int64  `json:"used"`
	// Converted 其中抽中的人已拥有、改发了别的东西的次数
	Converted int64 `json:"converted"`
}

// StatsSummary 概览数字。
type StatsSummary struct {
	TodayDraws  int64 `json:"todayDraws"`
	TodayUsers  int64 `json:"todayUsers"`
	TodayWins   int64 `json:"todayWins"`
	TotalDraws  int64 `json:"totalDraws"`
	TotalUsers  int64 `json:"totalUsers"`
	PointsSpent int64 `json:"pointsSpent"`
	Pending     int64 `json:"pending"`
	PityHits    int64 `json:"pityHits"`
	// Converted 抽中已拥有的奖品、改发了别的东西的次数；ConvertedPoints 其中折算出去的积分
	Converted       int64 `json:"converted"`
	ConvertedPoints int64 `json:"convertedPoints"`
}

// Stats 一个活动的概览。
type Stats struct {
	ActivityID int64         `json:"activityId"`
	Days       int           `json:"days"`
	Summary    StatsSummary  `json:"summary"`
	Trend      []StatsDay    `json:"trend"`
	Sources    []StatsSource `json:"sources"`
	Prizes     []StatsPrize  `json:"prizes"`
}
