// Package pointstore 积分商城：用积分兑换会员、功能解锁、经验、余额、抽奖次数与人工发放的权益。
//
// 规则见 docs/points-store.md。这个包只放类型、权益目录与可兑换判定（纯函数），
// 发放在仓储层的同一个事务里完成。
package pointstore

import (
	"time"

	"github.com/shopspring/decimal"

	vipdomain "aegis/internal/domain/vip"
)

// 商品状态。
const (
	StatusActive   = "active"   // 上架
	StatusInactive = "inactive" // 下架
)

// 限兑周期：limitCount 次 / 周期。周期按应用默认时区的自然日、ISO 周（周一起）、自然月算。
const (
	PeriodTotal = "total"
	PeriodDay   = "day"
	PeriodWeek  = "week"
	PeriodMonth = "month"
)

// 库存补货周期：每到一个新周期，剩余库存回到 restockQuantity。周期边界与限兑周期同一口径。
const (
	RestockNone  = "none"
	RestockDay   = PeriodDay
	RestockWeek  = PeriodWeek
	RestockMonth = PeriodMonth
)

// 兑换对象。
const (
	AudienceAll    = "all"
	AudienceVip    = "vip"     // 仅会员
	AudienceNonVip = "non_vip" // 仅非会员
	// AudienceLifetime 仅永久会员：永久会员已拥有全部会员与功能权益，给他们专属的兑换项
	AudienceLifetime = "lifetime"
)

// 兑换记录状态。
const (
	// RedemptionCompleted 已到账（自动发放的权益在兑换的同一个事务里就发完了）
	RedemptionCompleted = "completed"
	// RedemptionPending 等待人工发放（商品含「人工发放」权益）
	RedemptionPending = "pending"
	// RedemptionCancelled 已取消并退回积分（只有待发放的记录能取消）
	RedemptionCancelled = "cancelled"
)

// 积分流水的分类（integral_transactions.category）。
const (
	IntegralCategoryRedeem = "points_store"
	IntegralCategoryRefund = "points_store_refund"
)

// 字段上限。
const (
	MaxTitleRunes       = 32
	MaxSubtitleRunes    = 64
	MaxDescriptionRunes = 1000
	MaxCategoryRunes    = 16
	MaxBadgeRunes       = 6
	MaxNoteRunes        = 500
	MaxCost             = 1_000_000_000
	MaxStock            = 100_000_000
	MaxLimitCount       = 10_000
	MaxMinLevel         = 1000
	// MinutesPerDay 每日开放时段的分钟刻度上限（关闭时刻可以写 24:00）
	MinutesPerDay = 24 * 60
)

// Settings 应用的积分商城开关。
type Settings struct {
	AppID     int64     `json:"appid"`
	Enabled   bool      `json:"enabled"`
	UpdatedBy string    `json:"updatedBy,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
	// Configured 是否保存过（没保存过时按关闭处理）
	Configured bool `json:"configured"`
}

// Reward 一项权益。取值字段由目录的 Value 决定用哪个（见 RewardSpec）：
// 整数量走 Amount，金额走 Money，引用套餐走 RefID，功能解锁走 Tag + Amount（天数，0 为永久），
// 人工发放走 Text（发什么，用户与管理员都看得到）。
type Reward struct {
	Type   string          `json:"type"`
	Amount int64           `json:"amount,omitempty"`
	Money  decimal.Decimal `json:"money,omitempty"`
	RefID  int64           `json:"refId,omitempty"`
	Tag    string          `json:"tag,omitempty"`
	Text   string          `json:"text,omitempty"`
}

// RewardResult 一项权益的实际发放结果，写进兑换记录。
type RewardResult struct {
	Type   string `json:"type"`
	Label  string `json:"label"`
	Detail string `json:"detail"`
	// TransactionNo 对应账本的流水号（会员 / 经验 / 钱包），没有则为空
	TransactionNo string `json:"transactionNo,omitempty"`
	// ExpireAt 功能解锁到期时间（永久为空）
	ExpireAt *time.Time `json:"expireAt,omitempty"`
	// Pending 人工发放、尚未处理
	Pending bool `json:"pending,omitempty"`
	// Conversion 已拥有、按应用折算策略折成了积分（见 vip.Conversion）
	Conversion *vipdomain.ConversionSummary `json:"conversion,omitempty"`
}

// Item 一件商品。
type Item struct {
	ID          int64  `json:"id"`
	AppID       int64  `json:"appid"`
	Title       string `json:"title"`
	Subtitle    string `json:"subtitle,omitempty"`
	Description string `json:"description,omitempty"`
	Category    string `json:"category,omitempty"`
	Badge       string `json:"badge,omitempty"`
	// Cost 兑换所需积分
	Cost int64 `json:"cost"`
	// OriginalCost 划线价（高于 cost 才有意义）
	OriginalCost *int64   `json:"originalCost,omitempty"`
	Rewards      []Reward `json:"rewards"`
	// Stock 剩余库存，nil 为不限。开了补货周期时是「本周期还剩多少」，跨周期后读到的值要经
	// CurrentStock 换算（库里的数要等到新周期第一笔兑换才回写）
	Stock     *int64 `json:"stock,omitempty"`
	SoldCount int64  `json:"soldCount"`
	// RestockPeriod 补货周期（none / day / week / month）；RestockQuantity 每个周期补到多少
	RestockPeriod   string `json:"restockPeriod"`
	RestockQuantity *int64 `json:"restockQuantity,omitempty"`
	// StockWindowStart 库里的 Stock 属于哪个补货周期（该周期的起点）
	StockWindowStart *time.Time `json:"stockWindowStart,omitempty"`
	// GlobalLimitPeriod / GlobalLimitCount 全站名额：所有人合计每个周期最多兑多少份，0 为不限。
	// 与库存的区别：名额按兑换记录计数，取消的那笔自动让出名额；库存是运营放进去的货
	GlobalLimitPeriod string `json:"globalLimitPeriod"`
	GlobalLimitCount  int    `json:"globalLimitCount"`
	// LimitPeriod / LimitCount 每人限兑：limitCount 为 0 时不限
	LimitPeriod string `json:"limitPeriod"`
	LimitCount  int    `json:"limitCount"`
	// UserTotalLimit 每人累计上限（与按周期限兑叠加，如「每日 1 次、累计 10 次」），0 为不限
	UserTotalLimit int `json:"userTotalLimit"`
	// DailyOpenMinute / DailyCloseMinute 每日开放时段（应用默认时区，自零点起的分钟数），
	// 都为空时全天开放；开放晚于关闭表示跨零点（22:00–02:00）
	DailyOpenMinute  *int   `json:"dailyOpenMinute,omitempty"`
	DailyCloseMinute *int   `json:"dailyCloseMinute,omitempty"`
	Audience         string `json:"audience"`
	// MinLevel 等级门槛，0 为不限
	MinLevel  int        `json:"minLevel"`
	StartsAt  *time.Time `json:"startsAt,omitempty"`
	EndsAt    *time.Time `json:"endsAt,omitempty"`
	Status    string     `json:"status"`
	SortOrder int        `json:"sortOrder"`
	CreatedBy string     `json:"createdBy,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

// ItemInput 商品的创建 / 整体更新。
type ItemInput struct {
	AppID        int64
	Title        string
	Subtitle     string
	Description  string
	Category     string
	Badge        string
	Cost         int64
	OriginalCost *int64
	Rewards      []Reward
	Stock        *int64
	// RestockPeriod / RestockQuantity 见 Item；StockWindowStart 由服务层按保存时刻算好
	RestockPeriod     string
	RestockQuantity   *int64
	StockWindowStart  *time.Time
	GlobalLimitPeriod string
	GlobalLimitCount  int
	LimitPeriod       string
	LimitCount        int
	UserTotalLimit    int
	DailyOpenMinute   *int
	DailyCloseMinute  *int
	Audience          string
	MinLevel          int
	StartsAt          *time.Time
	EndsAt            *time.Time
	Status            string
	SortOrder         int
	Operator          string
}

// AdminItem 管理端列表的一行：商品 + 兑换汇总。
type AdminItem struct {
	Item
	// Redemptions 兑换笔数（不含已取消）
	Redemptions int64 `json:"redemptions"`
	// Redeemers 兑换人数（不含已取消）
	Redeemers int64 `json:"redeemers"`
	// PointsSpent 累计消耗积分（不含已退回）
	PointsSpent int64 `json:"pointsSpent"`
	// Pending 待人工发放的笔数
	Pending int64 `json:"pending"`
	// CurrentStock 此刻的剩余库存（已按补货周期换算），不限时为空
	CurrentStock *int64 `json:"currentStock,omitempty"`
	// NextRestockAt 下一次补货的时刻（开了补货周期才有）
	NextRestockAt *time.Time `json:"nextRestockAt,omitempty"`
	// GlobalUsed 本名额周期已用的份数；GlobalResetsAt 名额下次清零的时刻（累计名额为空）
	GlobalUsed     int64      `json:"globalUsed"`
	GlobalResetsAt *time.Time `json:"globalResetsAt,omitempty"`
	// OpenNow 此刻是否在每日开放时段内
	OpenNow bool `json:"openNow"`
}

// Redemption 一笔兑换。
type Redemption struct {
	ID           int64  `json:"id"`
	RedemptionNo string `json:"redemptionNo"`
	AppID        int64  `json:"appid"`
	UserID       int64  `json:"userId"`
	Account      string `json:"account,omitempty"`
	Nickname     string `json:"nickname,omitempty"`
	// ItemID 商品被删除后为空，标题与权益看快照
	ItemID    *int64         `json:"itemId,omitempty"`
	ItemTitle string         `json:"itemTitle"`
	Cost      int64          `json:"cost"`
	Rewards   []Reward       `json:"rewards"`
	Results   []RewardResult `json:"results"`
	Status    string         `json:"status"`
	// IntegralTransactionNo 扣积分的流水号；RefundTransactionNo 取消时退积分的流水号
	IntegralTransactionNo string     `json:"integralTransactionNo,omitempty"`
	RefundTransactionNo   string     `json:"refundTransactionNo,omitempty"`
	RequestID             string     `json:"requestId,omitempty"`
	Note                  string     `json:"note,omitempty"`
	HandledBy             string     `json:"handledBy,omitempty"`
	HandledAt             *time.Time `json:"handledAt,omitempty"`
	DeviceID              string     `json:"deviceId,omitempty"`
	ClientIP              string     `json:"clientIp,omitempty"`
	CreatedAt             time.Time  `json:"createdAt"`
	UpdatedAt             time.Time  `json:"updatedAt"`
}

// RedeemInput 一次兑换指令。
type RedeemInput struct {
	AppID  int64
	UserID int64
	ItemID int64
	// RequestID 客户端生成的幂等键：同一个键重复提交返回第一次的结果，不重复扣积分
	RequestID string
	DeviceID  string
	ClientIP  string
	UserAgent string
	// Windows 限兑周期的起点（服务层按应用默认时区算好）；Location 是同一个时区，判每日开放时段用
	Windows  Windows
	Location *time.Location
}

// RedeemResult 兑换结果。
type RedeemResult struct {
	Redemption Redemption `json:"redemption"`
	// Balance 兑换后的积分余额
	Balance int64 `json:"balance"`
	// Replayed 命中幂等键，本次没有再扣积分（返回的是第一次的记录）
	Replayed bool `json:"replayed,omitempty"`
}

// HandleInput 管理员处理一笔待发放的兑换。
type HandleInput struct {
	AppID        int64
	RedemptionID int64
	Note         string
	Operator     string
}

// RedemptionQuery 兑换记录查询。
type RedemptionQuery struct {
	AppID   int64
	UserID  int64
	ItemID  int64
	Status  string
	Keyword string
	// From / To 下单时间区间，左闭右开；为空不限
	From  *time.Time
	To    *time.Time
	Page  int
	Limit int
}

// Page 分页结果。
type Page[T any] struct {
	Items      []T   `json:"items"`
	Total      int64 `json:"total"`
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	TotalPages int   `json:"totalPages"`
}

// Counts 按周期分别计数的兑换次数（不含已取消）。
type Counts struct {
	Total int64 `json:"total"`
	Day   int64 `json:"day"`
	Week  int64 `json:"week"`
	Month int64 `json:"month"`
}

// In 指定周期内的次数。
func (c Counts) In(period string) int64 {
	switch period {
	case PeriodDay:
		return c.Day
	case PeriodWeek:
		return c.Week
	case PeriodMonth:
		return c.Month
	default:
		return c.Total
	}
}

// Usage 判定一件商品所需的计数：这个人兑了几次（Total…Month），以及所有人合计兑了几次（Global）。
type Usage struct {
	Total int64 `json:"total"`
	Day   int64 `json:"day"`
	Week  int64 `json:"week"`
	Month int64 `json:"month"`
	// Global 全站合计，判全站名额用
	Global Counts `json:"global"`
}

// In 这个人在指定周期内的已兑次数。
func (u Usage) In(period string) int64 {
	return Counts{Total: u.Total, Day: u.Day, Week: u.Week, Month: u.Month}.In(period)
}

// Windows 各限兑周期的起点。
type Windows struct {
	Day   time.Time
	Week  time.Time
	Month time.Time
}

// Start 周期的起点；累计（total）与未知周期返回零值。
func (w Windows) Start(period string) time.Time {
	switch period {
	case PeriodDay:
		return w.Day
	case PeriodWeek:
		return w.Week
	case PeriodMonth:
		return w.Month
	}
	return time.Time{}
}

// Next 周期的下一个起点（即本周期何时结束、计数何时清零）；累计周期返回 nil。
func (w Windows) Next(period string) *time.Time {
	var next time.Time
	switch period {
	case PeriodDay:
		next = w.Day.AddDate(0, 0, 1)
	case PeriodWeek:
		next = w.Week.AddDate(0, 0, 7)
	case PeriodMonth:
		next = w.Month.AddDate(0, 1, 0)
	default:
		return nil
	}
	return &next
}

// WindowsAt 按 loc 算出 now 所在的自然日、ISO 周（周一起）、自然月的起点。
func WindowsAt(now time.Time, loc *time.Location) Windows {
	if loc == nil {
		loc = time.Local
	}
	local := now.In(loc)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	weekday := int(day.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	return Windows{
		Day:   day,
		Week:  day.AddDate(0, 0, 1-weekday),
		Month: time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc),
	}
}

// UserFacts 判定一个人能不能兑换所需的事实（一次取齐）。
type UserFacts struct {
	Integral int64
	Level    int
	IsVip    bool
	// IsLifetime 永久会员
	IsLifetime bool
	// Holdings 已经永久拥有什么（判「已拥有」的统一判据 vip.Holdings）
	Holdings vipdomain.Holdings
	// Plans 商品引用到的会员套餐（判永久套餐是否已包含），按 ID 索引
	Plans map[int64]PlanRef
	// Features 功能目录（含停用的），按标识索引：判功能是否还在
	Features map[string]FeatureRef
	// Cosmetics 装扮目录：编号 → 是否可发（上架且发布过）；为 nil 时不判
	Cosmetics map[int64]bool
	// PermanentCosmetics 已永久持有的装扮
	PermanentCosmetics []int64
}

// PlanRef 判定所需的套餐信息。
type PlanRef struct {
	ID           int64    `json:"id"`
	Name         string   `json:"name"`
	DurationDays int      `json:"durationDays"`
	Lifetime     bool     `json:"lifetime"`
	Trial        bool     `json:"trial"`
	Features     []string `json:"features"`
}

// FeatureRef 判定与展示所需的功能信息。
type FeatureRef struct {
	Tag      string `json:"tag"`
	Name     string `json:"name"`
	IsActive bool   `json:"isActive"`
}

// 不能兑换的原因。客户端按 reason 分支，不要匹配 message。
const (
	ReasonOK         = "ok"
	ReasonInactive   = "inactive"
	ReasonNotStarted = "not_started"
	ReasonEnded      = "ended"
	// ReasonClosedNow 不在每日开放时段内
	ReasonClosedNow = "closed_now"
	// ReasonQuotaReached 全站名额已满（所有人合计）
	ReasonQuotaReached       = "quota_reached"
	ReasonSoldOut            = "sold_out"
	ReasonLimitReached       = "limit_reached"
	ReasonLevelRequired      = "level_required"
	ReasonVipOnly            = "vip_only"
	ReasonNonVipOnly         = "non_vip_only"
	ReasonLifetimeOnly       = "lifetime_only"
	ReasonOwned              = "owned"
	ReasonInsufficientPoints = "insufficient_points"
	ReasonRewardUnavailable  = "reward_unavailable"
)

// Availability 一件商品对一个人的可兑换判定。
type Availability struct {
	Redeemable bool   `json:"redeemable"`
	Reason     string `json:"reason"`
	Message    string `json:"message"`
}

// ClientItem 用户端看到的一件商品。
type ClientItem struct {
	ID           int64          `json:"id"`
	Title        string         `json:"title"`
	Subtitle     string         `json:"subtitle,omitempty"`
	Description  string         `json:"description,omitempty"`
	Category     string         `json:"category,omitempty"`
	Badge        string         `json:"badge,omitempty"`
	Cost         int64          `json:"cost"`
	OriginalCost *int64         `json:"originalCost,omitempty"`
	Rewards      []ClientReward `json:"rewards"`
	// Kind 主权益的类型（第一项），客户端据此选图标
	Kind string `json:"kind"`
	// Stock 此刻的剩余库存（已按补货周期换算），不限时为空
	Stock     *int64 `json:"stock,omitempty"`
	SoldCount int64  `json:"soldCount"`
	// RestockPeriod 补货周期（none / day / week / month）；RestockQuantity 每次补到多少；
	// NextRestockAt 下一次补货的时刻
	RestockPeriod   string     `json:"restockPeriod"`
	RestockQuantity *int64     `json:"restockQuantity,omitempty"`
	NextRestockAt   *time.Time `json:"nextRestockAt,omitempty"`
	// GlobalLimitPeriod / GlobalLimitCount 全站名额；GlobalUsed 本周期已被兑走的份数；
	// GlobalResetsAt 名额下次清零的时刻（累计名额为空）
	GlobalLimitPeriod string     `json:"globalLimitPeriod"`
	GlobalLimitCount  int        `json:"globalLimitCount"`
	GlobalUsed        int64      `json:"globalUsed"`
	GlobalResetsAt    *time.Time `json:"globalResetsAt,omitempty"`
	LimitPeriod       string     `json:"limitPeriod"`
	LimitCount        int        `json:"limitCount"`
	// Used 本周期已兑次数（limitCount 为 0 时是累计次数）；LimitResetsAt 次数下次清零的时刻
	Used          int64      `json:"used"`
	LimitResetsAt *time.Time `json:"limitResetsAt,omitempty"`
	// UserTotalLimit 每人累计上限，0 为不限；UsedTotal 这个人累计已兑次数
	UserTotalLimit int   `json:"userTotalLimit"`
	UsedTotal      int64 `json:"usedTotal"`
	// OpenHours 每日开放时段（如「10:00–22:00」），全天开放为空；OpenNow 此刻是否开放；
	// NextOpenAt 不在时段内时下一次开放的时刻
	OpenHours    string       `json:"openHours,omitempty"`
	OpenNow      bool         `json:"openNow"`
	NextOpenAt   *time.Time   `json:"nextOpenAt,omitempty"`
	Audience     string       `json:"audience"`
	MinLevel     int          `json:"minLevel"`
	StartsAt     *time.Time   `json:"startsAt,omitempty"`
	EndsAt       *time.Time   `json:"endsAt,omitempty"`
	Availability Availability `json:"availability"`
}

// ClientReward 用户端看到的一项权益：一句话说清是什么。
type ClientReward struct {
	Type  string `json:"type"`
	Label string `json:"label"`
	// Detail 「30 天」「在线音乐 永久」「500 点」这类具体内容
	Detail string `json:"detail"`
	// Tag 功能解锁的功能标识；Days 会员天数 / 功能解锁天数（0 为永久）
	Tag       string `json:"tag,omitempty"`
	Days      int    `json:"days,omitempty"`
	Permanent bool   `json:"permanent,omitempty"`
	// RefID 个性装扮的装扮编号（客户端据此显示装扮外观）
	RefID int64 `json:"refId,omitempty"`
}

// ClientStore 用户端的积分商城首页。
type ClientStore struct {
	Enabled bool `json:"enabled"`
	// Balance 当前积分
	Balance int64 `json:"balance"`
	Level   int   `json:"level"`
	IsVip   bool  `json:"isVip"`
	// Categories 有商品的分类，按商品排序先后出现的顺序
	Categories []string     `json:"categories"`
	Items      []ClientItem `json:"items"`
	ServerTime time.Time    `json:"serverTime"`
	// Timezone 周期边界与开放时段所用的时区（IANA 名）
	Timezone string `json:"timezone"`
}

// Stats 管理端概览。
type Stats struct {
	Summary  StatsSummary `json:"summary"`
	Trend    []StatsDay   `json:"trend"`
	TopItems []StatsItem  `json:"topItems"`
}

// StatsSummary 窗口内汇总 + 当前待处理。
type StatsSummary struct {
	Redemptions int64 `json:"redemptions"`
	Redeemers   int64 `json:"redeemers"`
	PointsSpent int64 `json:"pointsSpent"`
	Pending     int64 `json:"pending"`
	ActiveItems int64 `json:"activeItems"`
	Today       int64 `json:"today"`
	// Cancelled 窗口内被取消（已退积分）的笔数，不计入上面的兑换汇总
	Cancelled int64 `json:"cancelled"`
}

// StatsDay 按天的兑换。
type StatsDay struct {
	Date        string `json:"date"`
	Redemptions int64  `json:"redemptions"`
	PointsSpent int64  `json:"pointsSpent"`
	Redeemers   int64  `json:"redeemers"`
}

// StatsItem 窗口内兑换最多的商品。
type StatsItem struct {
	ItemID      *int64 `json:"itemId,omitempty"`
	Title       string `json:"title"`
	Redemptions int64  `json:"redemptions"`
	PointsSpent int64  `json:"pointsSpent"`
}

// UnlockGrantInput 管理员授予功能解锁。
type UnlockGrantInput struct {
	AppID    int64
	UserID   int64
	Tag      string
	Days     int
	Operator string
}
