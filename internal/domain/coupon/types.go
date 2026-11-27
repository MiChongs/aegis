// Package coupon 优惠系统：优惠券（模板 → 用户券）与优惠活动（限时折扣 / 满减 / 支付返券返积分），
// 以及把两者落到一笔订单上的定价引擎。
//
// 规则见 docs/coupons.md。这个包只放类型、配置的清洗与校验、判定与定价（纯函数）：
// 下单试算、领券资格、用券资格、活动命中全部由这里的函数决定，列表上「为什么不能用」与下单失败的
// 错误是同一套判据。预占、核销、释放、退款返还在仓储层的事务里完成。
package coupon

import (
	"time"

	"github.com/shopspring/decimal"
)

// ── 适用范围 ──
//
// 范围对应订单用途（domain/payment 的 purpose）。余额充值不能打折：充进去的本金必须等于实付，
// 打折充值等于凭空印钱；它只能参加「支付返券 / 返积分」活动（充值档位本来就有附赠）。
const (
	// ScopeVip 买会员套餐（vip_purchase）
	ScopeVip = "vip_purchase"
	// ScopeIntegral 积分充值（integral_purchase）：按原价发积分，用户少付钱
	ScopeIntegral = "integral_purchase"
	// ScopeGoods 普通商品（不带用途的订单）：价格由接入方给，接入方自己发货
	ScopeGoods = "goods"
	// ScopeRecharge 余额充值（wallet_recharge）：只能用于返券 / 返积分活动
	ScopeRecharge = "wallet_recharge"
)

// ── 优惠券 ──

// 券的优惠方式。
const (
	// KindAmountOff 立减：Amount 元（有门槛即「满 X 减 Y」）
	KindAmountOff = "amount_off"
	// KindPercentOff 折扣：按 Rate（付款比例 %，85 即 8.5 折）付款，可设 MaxDiscount 封顶
	KindPercentOff = "percent_off"
)

// 模板状态。
const (
	StatusDraft  = "draft"  // 草稿：不能发放、不出现在领券中心
	StatusActive = "active" // 进行中
	StatusPaused = "paused" // 暂停：不能再发放，已发出的券照常能用
	StatusEnded  = "ended"  // 已结束：不能再发放、不再出现；已发出的券照常能用到各自的有效期
)

// 有效期方式。
const (
	// ValidityFixed 固定时段：ValidFrom（可空，为发放时）到 ValidUntil
	ValidityFixed = "fixed"
	// ValidityRelative 领取后生效：发放后 ValidDelayDays 天生效、再过 ValidDays 天失效
	ValidityRelative = "relative"
)

// 用户券状态（落库的）。展示时还会按时间推导出 not_started（未生效）与 expired（已过期）。
const (
	CouponAvailable = "available" // 可用
	CouponLocked    = "locked"    // 已用于一笔待支付订单（订单关闭后返还）
	CouponUsed      = "used"      // 已使用
	CouponExpired   = "expired"   // 已过期（后台扫描写入；读取时也按时间推导）
	CouponRevoked   = "revoked"   // 已撤销（管理员收回、或退款时收回返券）
	// CouponNotStarted 只在展示时出现：还没到生效时间
	CouponNotStarted = "not_started"
)

// 用户券的来源（user_coupons.source）。
const (
	SourceClaim       = "claim"        // 领券中心领取
	SourceAdmin       = "admin"        // 管理员发放
	SourceRebate      = "rebate"       // 支付返券
	SourceCardKey     = "card_key"     // 卡密兑换
	SourcePointsStore = "points_store" // 积分商城
	SourceLottery     = "lottery"      // 抽奖
	SourceItemGrant   = "item_grant"   // 物品发放（含等级奖励）
	SourceInvite      = "invite"       // 邀请（含推广活动）
	SourceLeaderboard = "leaderboard"  // 排行榜
	SourceRefund      = "refund"       // 退款返还（原券不能返还时补发的同款券）
)

// 领取 / 参与资格的人群。
const (
	AudienceAll        = "all"         // 所有人
	AudienceNewUser    = "new_user"    // 注册 N 天内
	AudienceFirstOrder = "first_order" // 在本应用还没有付过款
	AudienceNonVip     = "non_vip"     // 当前不是会员
	AudienceVip        = "vip"         // 当前是会员
)

// 视觉色调：券面与活动角标的配色，客户端按它取主题色（不下发具体色值，深浅色由客户端决定）。
const (
	ThemeRed    = "red"
	ThemeOrange = "orange"
	ThemeGold   = "gold"
	ThemeGreen  = "green"
	ThemeBlue   = "blue"
	ThemePurple = "purple"
)

// Audience 人群条件。
type Audience struct {
	Kind string `json:"kind"`
	// NewUserDays 新用户的天数（Kind=new_user 时，1–365）
	NewUserDays int `json:"newUserDays,omitempty"`
}

// Template 优惠券模板：一种券的规则。用户手里的每一张券（UserCoupon）都来自一个模板。
//
// 规则字段（方式、面额、门槛、范围、叠加）在发出第一张券后锁定：已经在用户手里的券
// 改了面额，用户看到的和用的就对不上了。之后只能改名称、说明、库存、领取设置与状态。
type Template struct {
	ID          int64  `json:"id"`
	AppID       int64  `json:"appid"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Theme       string `json:"theme"`

	Kind        string          `json:"kind"`
	Amount      decimal.Decimal `json:"amount"`
	Rate        int             `json:"rate,omitempty"`
	MaxDiscount decimal.Decimal `json:"maxDiscount"`
	MinAmount   decimal.Decimal `json:"minAmount"`

	Scopes []string `json:"scopes"`
	// PlanIDs 只能用于这些会员套餐（空为全部套餐）
	PlanIDs []int64 `json:"planIds"`
	// StackWithPromotion 能与优惠活动同享（活动价之后再用券）
	StackWithPromotion bool `json:"stackWithPromotion"`

	ValidityMode   string     `json:"validityMode"`
	ValidFrom      *time.Time `json:"validFrom,omitempty"`
	ValidUntil     *time.Time `json:"validUntil,omitempty"`
	ValidDays      int        `json:"validDays,omitempty"`
	ValidDelayDays int        `json:"validDelayDays,omitempty"`

	// TotalQuantity 发放总量（0 不限），全部渠道共用
	TotalQuantity int64 `json:"totalQuantity"`
	// PerUserLimit 每人最多领几张（只限领券中心，0 不限）
	PerUserLimit int `json:"perUserLimit"`
	// Claimable 出现在领券中心，用户自己领
	Claimable     bool       `json:"claimable"`
	ClaimStartsAt *time.Time `json:"claimStartsAt,omitempty"`
	ClaimEndsAt   *time.Time `json:"claimEndsAt,omitempty"`
	Audience      Audience   `json:"audience"`
	// RefundReturn 订单全额退款后把券还给用户（还在有效期内时）
	RefundReturn bool `json:"refundReturn"`

	Status string `json:"status"`

	IssuedCount   int64           `json:"issuedCount"`
	UsedCount     int64           `json:"usedCount"`
	DiscountTotal decimal.Decimal `json:"discountTotal"`

	CreatedBy string    `json:"createdBy,omitempty"`
	UpdatedBy string    `json:"updatedBy,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// TemplateInput 新建 / 修改模板。
type TemplateInput struct {
	AppID       int64
	Name        string
	Description string
	Theme       string

	Kind        string
	Amount      decimal.Decimal
	Rate        int
	MaxDiscount decimal.Decimal
	MinAmount   decimal.Decimal

	Scopes             []string
	PlanIDs            []int64
	StackWithPromotion bool

	ValidityMode   string
	ValidFrom      *time.Time
	ValidUntil     *time.Time
	ValidDays      int
	ValidDelayDays int

	TotalQuantity int64
	PerUserLimit  int
	Claimable     bool
	ClaimStartsAt *time.Time
	ClaimEndsAt   *time.Time
	Audience      Audience
	RefundReturn  bool

	Operator string
}

// UserCoupon 用户手里的一张券。
type UserCoupon struct {
	ID         int64  `json:"id"`
	AppID      int64  `json:"appid"`
	UserID     int64  `json:"userId"`
	TemplateID int64  `json:"templateId"`
	Code       string `json:"code"`
	Status     string `json:"status"`
	Source     string `json:"source"`
	SourceRef  string `json:"sourceRef,omitempty"`

	ValidFrom  time.Time `json:"validFrom"`
	ValidUntil time.Time `json:"validUntil"`

	// OrderNo 占用 / 使用它的订单
	OrderNo  string          `json:"orderNo,omitempty"`
	UsedAt   *time.Time      `json:"usedAt,omitempty"`
	Discount decimal.Decimal `json:"discount"`
	// RevokedReason 撤销原因
	RevokedReason string `json:"revokedReason,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// Template 券面（列表 / 试算时带上）；规则字段就是模板的
	Template *Template `json:"template,omitempty"`

	// 管理端列表带上的用户信息
	UserAccount  string `json:"userAccount,omitempty"`
	UserNickname string `json:"userNickname,omitempty"`
}

// DisplayStatus 展示用的状态：可用的券按时间推导出「未生效」「已过期」。
func (c UserCoupon) DisplayStatus(now time.Time) string {
	if c.Status == CouponAvailable {
		if !now.Before(c.ValidUntil) {
			return CouponExpired
		}
		if now.Before(c.ValidFrom) {
			return CouponNotStarted
		}
	}
	return c.Status
}

// UserCouponQuery 用户券列表查询。
type UserCouponQuery struct {
	AppID      int64
	UserID     int64
	TemplateID int64
	// Status available / locked / used / expired / revoked / not_started；usable 为可用与未生效
	Status  string
	Source  string
	Keyword string
	Page    int
	Limit   int
}

// TemplateQuery 模板列表查询。
type TemplateQuery struct {
	AppID   int64
	Status  string
	Keyword string
	Page    int
	Limit   int
}

// ── 优惠活动 ──

// 活动类型。
const (
	// PromotionDiscount 限时折扣：打折 / 立减 / 特价
	PromotionDiscount = "discount"
	// PromotionThreshold 满减：按订单金额阶梯减
	PromotionThreshold = "threshold"
	// PromotionRebate 支付返券 / 返积分：付款满额后发券、发积分（不改订单价格）
	PromotionRebate = "rebate"
)

// 限时折扣的方式。
const (
	DiscountPercent    = "percent"     // 打折：Rate 付款比例 %
	DiscountAmountOff  = "amount_off"  // 立减 Amount 元
	DiscountFixedPrice = "fixed_price" // 特价：直接按 Amount 元卖（只在原价更高时生效）
)

// ThresholdTier 满减的一档：满 MinAmount 减 Off。
type ThresholdTier struct {
	MinAmount decimal.Decimal `json:"minAmount"`
	Off       decimal.Decimal `json:"off"`
}

// RebateCoupon 返券的一项：模板与张数。
type RebateCoupon struct {
	TemplateID int64 `json:"templateId"`
	Count      int   `json:"count"`
	// TemplateName 展示用（读取时补上）
	TemplateName string `json:"templateName,omitempty"`
}

// PromotionRule 活动规则（按类型用其中一部分）。
type PromotionRule struct {
	// 限时折扣
	DiscountMode string          `json:"discountMode,omitempty"`
	Rate         int             `json:"rate,omitempty"`
	Amount       decimal.Decimal `json:"amount"`
	MaxDiscount  decimal.Decimal `json:"maxDiscount"`
	// 满减
	Tiers []ThresholdTier `json:"tiers,omitempty"`
	// 返券 / 返积分
	MinAmount      decimal.Decimal `json:"minAmount"`
	RebateCoupons  []RebateCoupon  `json:"rebateCoupons,omitempty"`
	RebateIntegral int64           `json:"rebateIntegral,omitempty"`
}

// Promotion 一期优惠活动。
type Promotion struct {
	ID          int64  `json:"id"`
	AppID       int64  `json:"appid"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Tag 角标（如「限时 8 折」）；空时按规则生成
	Tag   string `json:"tag"`
	Theme string `json:"theme"`

	Kind     string        `json:"kind"`
	Rule     PromotionRule `json:"rule"`
	Priority int           `json:"priority"`

	Scopes     []string `json:"scopes"`
	PlanIDs    []int64  `json:"planIds"`
	PackageIDs []int64  `json:"packageIds"`
	Audience   Audience `json:"audience"`
	// StackWithCoupon 能与优惠券同享
	StackWithCoupon bool `json:"stackWithCoupon"`

	StartsAt *time.Time `json:"startsAt,omitempty"`
	EndsAt   *time.Time `json:"endsAt,omitempty"`

	// PerUserLimit 每人参与几次（按下单计，0 不限）
	PerUserLimit int `json:"perUserLimit"`
	// TotalLimit 一共参与几次（0 不限）
	TotalLimit int64 `json:"totalLimit"`
	// Budget 优惠总额上限（0 不限）；返积分活动不计
	Budget decimal.Decimal `json:"budget"`

	Status        string          `json:"status"`
	UsedCount     int64           `json:"usedCount"`
	DiscountTotal decimal.Decimal `json:"discountTotal"`

	CreatedBy string    `json:"createdBy,omitempty"`
	UpdatedBy string    `json:"updatedBy,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// PromotionInput 新建 / 修改活动。
type PromotionInput struct {
	AppID           int64
	Name            string
	Description     string
	Tag             string
	Theme           string
	Kind            string
	Rule            PromotionRule
	Priority        int
	Scopes          []string
	PlanIDs         []int64
	PackageIDs      []int64
	Audience        Audience
	StackWithCoupon bool
	StartsAt        *time.Time
	EndsAt          *time.Time
	PerUserLimit    int
	TotalLimit      int64
	Budget          decimal.Decimal
	Operator        string
}

// PromotionQuery 活动列表查询。
type PromotionQuery struct {
	AppID   int64
	Kind    string
	Status  string
	Keyword string
	Page    int
	Limit   int
}

// ── 订单上的优惠 ──

// 订单优惠记录的状态（payment_order_promotions.status）。
const (
	// OrderReserved 已下单未付款：券已占用、活动名额与预算已预占
	OrderReserved = "reserved"
	// OrderSettled 已付款：券已核销、活动计入，返券返积分已处理
	OrderSettled = "settled"
	// OrderReleased 订单没付款就关闭了：券已返还、名额与预算已退回
	OrderReleased = "released"
	// OrderRefunded 全额退款：名额与预算已退回，券按模板设置返还
	OrderRefunded = "refunded"
)

// 返券返积分的状态。
const (
	RebateNone    = "none"    // 没有命中返券活动
	RebatePending = "pending" // 命中了，等付款
	RebateGranted = "granted" // 已发放
	RebateSkipped = "skipped" // 付款时已不满足（活动结束、名额满……）
	RebateRevoked = "revoked" // 退款后已收回
	RebateFailed  = "failed"  // 发放失败（如券已发完），详见说明
)

// OrderPromotion 一笔订单用了哪些优惠。
type OrderPromotion struct {
	ID      int64  `json:"id"`
	AppID   int64  `json:"appid"`
	UserID  int64  `json:"userId"`
	OrderID int64  `json:"orderId"`
	OrderNo string `json:"orderNo"`
	Purpose string `json:"purpose"`
	Subject string `json:"subject,omitempty"`

	OriginalAmount    decimal.Decimal `json:"originalAmount"`
	PromotionDiscount decimal.Decimal `json:"promotionDiscount"`
	CouponDiscount    decimal.Decimal `json:"couponDiscount"`
	PayableAmount     decimal.Decimal `json:"payableAmount"`

	PromotionID   *int64 `json:"promotionId,omitempty"`
	PromotionName string `json:"promotionName,omitempty"`
	UserCouponID  *int64 `json:"userCouponId,omitempty"`
	CouponName    string `json:"couponName,omitempty"`
	TemplateID    *int64 `json:"templateId,omitempty"`

	RebatePromotionID   *int64 `json:"rebatePromotionId,omitempty"`
	RebatePromotionName string `json:"rebatePromotionName,omitempty"`
	RebateStatus        string `json:"rebateStatus"`
	RebateNote          string `json:"rebateNote,omitempty"`

	Status     string     `json:"status"`
	Note       string     `json:"note,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	SettledAt  *time.Time `json:"settledAt,omitempty"`
	ReleasedAt *time.Time `json:"releasedAt,omitempty"`

	// 管理端列表带上的
	UserAccount  string `json:"userAccount,omitempty"`
	UserNickname string `json:"userNickname,omitempty"`
	OrderStatus  string `json:"orderStatus,omitempty"`
}

// OrderPromotionQuery 订单优惠记录查询。
type OrderPromotionQuery struct {
	AppID       int64
	UserID      int64
	PromotionID int64
	TemplateID  int64
	Status      string
	Keyword     string
	Page        int
	Limit       int
}

// Reservation 下单时要预占的优惠（由 Quote 得出，仓储层在下单事务里逐项条件预占、复核）。
type Reservation struct {
	OriginalAmount    decimal.Decimal
	PromotionDiscount decimal.Decimal
	CouponDiscount    decimal.Decimal
	PayableAmount     decimal.Decimal

	Purpose   string
	PlanID    int64
	PackageID int64

	PromotionID           int64
	PromotionName         string
	PromotionPerUserLimit int

	UserCouponID int64
	TemplateID   int64
	CouponName   string

	RebatePromotionID   int64
	RebatePromotionName string
	RebatePerUserLimit  int
}

// Empty 没有任何优惠（不用落记录）。
func (r Reservation) Empty() bool {
	return r.PromotionID == 0 && r.UserCouponID == 0 && r.RebatePromotionID == 0
}

// ── 统计 ──

// Overview 优惠概览。
type Overview struct {
	Templates        int64           `json:"templates"`
	ActiveTemplates  int64           `json:"activeTemplates"`
	Promotions       int64           `json:"promotions"`
	ActivePromotions int64           `json:"activePromotions"`
	Issued           int64           `json:"issued"`
	Used             int64           `json:"used"`
	Available        int64           `json:"available"`
	Expired          int64           `json:"expired"`
	Orders           int64           `json:"orders"`
	PaidAmount       decimal.Decimal `json:"paidAmount"`
	OriginalAmount   decimal.Decimal `json:"originalAmount"`
	CouponDiscount   decimal.Decimal `json:"couponDiscount"`
	PromoDiscount    decimal.Decimal `json:"promotionDiscount"`
	RebateIntegral   int64           `json:"rebateIntegral"`
	RebateCoupons    int64           `json:"rebateCoupons"`
	Trend            []TrendPoint    `json:"trend"`
	TopTemplates     []RankItem      `json:"topTemplates"`
	TopPromotions    []RankItem      `json:"topPromotions"`
	Sources          []SourceCount   `json:"sources"`
}

// TrendPoint 一天的数据。
type TrendPoint struct {
	Date     string          `json:"date"`
	Issued   int64           `json:"issued"`
	Used     int64           `json:"used"`
	Orders   int64           `json:"orders"`
	Paid     decimal.Decimal `json:"paid"`
	Discount decimal.Decimal `json:"discount"`
}

// RankItem 排行的一项。
type RankItem struct {
	ID       int64           `json:"id"`
	Name     string          `json:"name"`
	Count    int64           `json:"count"`
	Discount decimal.Decimal `json:"discount"`
	Paid     decimal.Decimal `json:"paid"`
}

// SourceCount 按来源的发券数。
type SourceCount struct {
	Source string `json:"source"`
	Count  int64  `json:"count"`
}

// Page 分页结果。
type Page[T any] struct {
	Items      []T   `json:"items"`
	Total      int64 `json:"total"`
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	TotalPages int   `json:"totalPages"`
}

// NewPage 组装分页结果。
func NewPage[T any](items []T, total int64, page, limit int) Page[T] {
	if items == nil {
		items = []T{}
	}
	pages := 0
	if limit > 0 {
		pages = int((total + int64(limit) - 1) / int64(limit))
	}
	return Page[T]{Items: items, Total: total, Page: page, Limit: limit, TotalPages: pages}
}

// GrantInput 各渠道发券的统一入参（仓储层 grantCouponTx）。
type GrantInput struct {
	AppID      int64
	UserID     int64
	TemplateID int64
	Count      int
	Source     string
	SourceRef  string
	// IgnoreStatus 管理员发放、返券可以发暂停中的模板（草稿与已结束的仍不行）
	IgnoreStatus bool
}

// GrantResult 发券结果。
type GrantResult struct {
	TemplateName string       `json:"templateName"`
	Coupons      []UserCoupon `json:"coupons"`
}
