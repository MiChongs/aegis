// Package cardkey 定义应用级卡密的领域类型。
//
// 一张卡有两种形态：授权卡（卡即登录凭证）与兑换卡（给已登录用户发权益）。
// 两者共用同一套生成、作废、核销与权益目录，只有「怎么被使用」不同。
package cardkey

import (
	"time"

	"github.com/shopspring/decimal"
)

// 卡的形态。
const (
	// KindLogin 授权卡：卡本身就是登录凭证，首次使用自动建号并绑定。
	KindLogin = "login"
	// KindRedeem 兑换卡：给已登录用户发权益。
	KindRedeem = "redeem"
)

// 卡的状态。
//
// 「已过期」刻意不在其中：它是 ExpiresAt 与当前时间比出来的结论，
// 存成状态就需要一个定时任务去翻它，而那个任务停掉的表现是「过期卡还能用」。
const (
	StatusUnused   = "unused"
	StatusActive   = "active"
	StatusUsed     = "used"
	StatusDisabled = "disabled"
)

// 有效期模式。
const (
	// ValidityPermanent 永不过期。
	ValidityPermanent = "permanent"
	// ValidityFixedUntil 批次统一到期，生成时即写死。
	ValidityFixedUntil = "fixed_until"
	// ValidityFromFirstUse 激活即计时。卖出去到被使用之间的时间不算进授权期。
	ValidityFromFirstUse = "days_from_first_use"
)

// 核销来源。
const (
	SourceRedeem = "redeem"
	SourceLogin  = "login"
	SourceAdmin  = "admin"
)

// 批次状态。
const (
	BatchActive   = "active"
	BatchDisabled = "disabled"
)

// Reward 一项权益。
//
// 三个取值字段互斥，由权益目录的 Value 决定用哪一个：整数量走 Amount，
// 金额走 Money，引用既有实体走 RefID。合成一个 any 字段会让「30 天」与
// 「套餐 30 号」在 JSON 里长得一模一样。
type Reward struct {
	Type   string          `json:"type"`
	Amount int64           `json:"amount,omitempty"`
	Money  decimal.Decimal `json:"money,omitempty"`
	RefID  int64           `json:"refId,omitempty"`
}

// RewardResult 一项权益的实际发放结果，写进核销流水。
//
// 排障时要靠它回答「用户说没到账」到底是没发、发少了、还是发到别处了 ——
// 只记「发过什么」而不记「发成了什么」的话，这个问题没有答案。
type RewardResult struct {
	Type   string `json:"type"`
	Label  string `json:"label"`
	Detail string `json:"detail"`
	// TransactionNo 对应的账本流水号（会员 / 积分 / 钱包各有一套），没有则为空。
	TransactionNo string `json:"transactionNo,omitempty"`
}

// Batch 一批卡密。
type Batch struct {
	ID     int64  `json:"id"`
	AppID  int64  `json:"appid"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Remark string `json:"remark,omitempty"`

	CodePrefix    string `json:"codePrefix,omitempty"`
	Segments      int    `json:"segments"`
	SegmentLength int    `json:"segmentLength"`

	Rewards    []Reward `json:"rewards"`
	MaxDevices int      `json:"maxDevices"`

	ValidityMode string     `json:"validityMode"`
	ValidityDays int        `json:"validityDays"`
	ValidUntil   *time.Time `json:"validUntil,omitempty"`

	Total     int    `json:"total"`
	Status    string `json:"status"`
	CreatedBy string `json:"createdBy,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// Stats 各状态的卡数量，列表接口随批次一并下发。
	Stats *BatchStats `json:"stats,omitempty"`
}

// BatchStats 一批卡的核销进度。
type BatchStats struct {
	Total    int64 `json:"total"`
	Unused   int64 `json:"unused"`
	Active   int64 `json:"active"`
	Used     int64 `json:"used"`
	Disabled int64 `json:"disabled"`
	// Expired 已过期（按 ExpiresAt 算出来的，不是一档状态）。
	Expired int64 `json:"expired"`
}

// Card 一张卡。
type Card struct {
	ID      int64  `json:"id"`
	AppID   int64  `json:"appid"`
	BatchID int64  `json:"batchId"`
	Code    string `json:"code"`
	Kind    string `json:"kind"`
	Status  string `json:"status"`

	BoundUserID *int64 `json:"boundUserId,omitempty"`
	MaxDevices  int    `json:"maxDevices"`

	ActivatedAt    *time.Time `json:"activatedAt,omitempty"`
	ExpiresAt      *time.Time `json:"expiresAt,omitempty"`
	UsedAt         *time.Time `json:"usedAt,omitempty"`
	DisabledAt     *time.Time `json:"disabledAt,omitempty"`
	DisabledReason string     `json:"disabledReason,omitempty"`
	Remark         string     `json:"remark,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// 以下为查询时联表补齐，不落库。
	BatchName    string `json:"batchName,omitempty"`
	BoundAccount string `json:"boundAccount,omitempty"`
	DeviceCount  int    `json:"deviceCount"`
}

// Expired 这张卡是否已过授权期。
//
// 写成方法而不是让调用方各自比时间：判据只有一处，
// 且「没有到期时间 = 永久有效」这条容易被写反。
func (c *Card) Expired(now time.Time) bool {
	return c.ExpiresAt != nil && !c.ExpiresAt.After(now)
}

// Device 一张卡绑定的一台设备。
type Device struct {
	ID          int64     `json:"id"`
	CardKeyID   int64     `json:"cardKeyId"`
	DeviceID    string    `json:"deviceId"`
	DeviceName  string    `json:"deviceName,omitempty"`
	FirstSeenAt time.Time `json:"firstSeenAt"`
	LastSeenAt  time.Time `json:"lastSeenAt"`
	SeenCount   int64     `json:"seenCount"`
}

// Redemption 一条核销流水。
type Redemption struct {
	ID        int64          `json:"id"`
	AppID     int64          `json:"appid"`
	CardKeyID int64          `json:"cardKeyId"`
	BatchID   *int64         `json:"batchId,omitempty"`
	Code      string         `json:"code"`
	UserID    int64          `json:"userId"`
	Rewards   []Reward       `json:"rewards"`
	Results   []RewardResult `json:"results"`
	Source    string         `json:"source"`
	DeviceID  string         `json:"deviceId,omitempty"`
	ClientIP  string         `json:"clientIp,omitempty"`
	UserAgent string         `json:"userAgent,omitempty"`
	Operator  string         `json:"operator,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`

	// 查询时联表补齐。
	Account   string `json:"account,omitempty"`
	BatchName string `json:"batchName,omitempty"`
}

// GenerateInput 生成一批卡密。
type GenerateInput struct {
	AppID         int64
	Name          string
	Kind          string
	Remark        string
	Count         int
	CodePrefix    string
	Segments      int
	SegmentLength int
	Rewards       []Reward
	MaxDevices    int
	ValidityMode  string
	ValidityDays  int
	ValidUntil    *time.Time
	Operator      string
}

// RedeemInput 一次兑换核销。
type RedeemInput struct {
	AppID     int64
	Code      string
	UserID    int64
	Source    string
	DeviceID  string
	ClientIP  string
	UserAgent string
	Operator  string
}

// ActivateLoginInput 授权卡的一次登录（首次为激活，之后为续用）。
type ActivateLoginInput struct {
	AppID      int64
	CardID     int64
	UserID     int64
	DeviceID   string
	DeviceName string
	ClientIP   string
	UserAgent  string
}

// CardQuery 单卡列表的筛选条件。
type CardQuery struct {
	AppID   int64
	BatchID int64
	Status  string
	Kind    string
	Keyword string
	UserID  int64
	Page    int
	Limit   int
}

// RedemptionQuery 核销记录的筛选条件。
type RedemptionQuery struct {
	AppID   int64
	BatchID int64
	UserID  int64
	Keyword string
	Page    int
	Limit   int
}

// CardPage 单卡分页结果。
type CardPage struct {
	Items []Card `json:"items"`
	Total int64  `json:"total"`
	Page  int    `json:"page"`
	Limit int    `json:"limit"`
}

// RedemptionPage 核销记录分页结果。
type RedemptionPage struct {
	Items []Redemption `json:"items"`
	Total int64        `json:"total"`
	Page  int          `json:"page"`
	Limit int          `json:"limit"`
}

// RedeemResult 一次核销的结果。
type RedeemResult struct {
	Code    string         `json:"code"`
	Kind    string         `json:"kind"`
	Results []RewardResult `json:"results"`
	// ExpiresAt 授权卡核销后的授权到期时间。
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// LoginAuthorization 授权卡登录成功时的授权快照，随登录结果下发给客户端。
type LoginAuthorization struct {
	Code        string     `json:"code"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	MaxDevices  int        `json:"maxDevices"`
	DeviceCount int        `json:"deviceCount"`
	// FirstActivation 本次是不是这张卡的首次激活（首次才发权益）。
	FirstActivation bool `json:"firstActivation"`
}
