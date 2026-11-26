package aigateway

import (
	"slices"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// 额度商店：额度包、报价、订单与额度批次。规格见 docs/ai-billing.md「额度包」「额度批次」。

// 额度包的配色（只影响展示）。
var PackageTones = []string{"blue", "violet", "amber", "emerald", "rose", "slate"}

// Package 一个额度包。
type Package struct {
	ID            int64           `json:"id"`
	AppID         int64           `json:"appid"`
	Name          string          `json:"name"`
	Subtitle      string          `json:"subtitle"`
	Description   string          `json:"description"`
	Badge         string          `json:"badge"`
	Tone          string          `json:"tone"`
	Points        decimal.Decimal `json:"points"`
	BonusPoints   decimal.Decimal `json:"bonusPoints"`
	Price         decimal.Decimal `json:"price"`
	OriginalPrice decimal.Decimal `json:"originalPrice"`
	// ValidityDays 有效天数，0 为永久。
	ValidityDays int `json:"validityDays"`
	// Audience all / vip；TierIDs 非空时只有持有这些档位的会员能买。
	Audience       string  `json:"audience"`
	TierIDs        []int64 `json:"tierIds"`
	MemberDiscount bool    `json:"memberDiscount"`
	// PerUserLimit 每人累计限购、MonthlyLimit 每人每自然月限购、Stock 总库存，都是 0 不限。
	PerUserLimit int        `json:"perUserLimit"`
	MonthlyLimit int        `json:"monthlyLimit"`
	Stock        int        `json:"stock"`
	Sold         int        `json:"sold"`
	StartsAt     *time.Time `json:"startsAt"`
	EndsAt       *time.Time `json:"endsAt"`
	Featured     bool       `json:"featured"`
	Enabled      bool       `json:"enabled"`
	SortOrder    int        `json:"sortOrder"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`

	// 管理端列表的经营数据。
	Orders  int64            `json:"orders,omitempty"`
	Buyers  int64            `json:"buyers,omitempty"`
	Revenue *decimal.Decimal `json:"revenue,omitempty"`
}

// PackageInput 新建 / 修改额度包（整体替换）。
type PackageInput struct {
	Name           string          `json:"name"`
	Subtitle       string          `json:"subtitle"`
	Description    string          `json:"description"`
	Badge          string          `json:"badge"`
	Tone           string          `json:"tone"`
	Points         decimal.Decimal `json:"points"`
	BonusPoints    decimal.Decimal `json:"bonusPoints"`
	Price          decimal.Decimal `json:"price"`
	OriginalPrice  decimal.Decimal `json:"originalPrice"`
	ValidityDays   int             `json:"validityDays"`
	Audience       string          `json:"audience"`
	TierIDs        []int64         `json:"tierIds"`
	MemberDiscount *bool           `json:"memberDiscount"`
	PerUserLimit   int             `json:"perUserLimit"`
	MonthlyLimit   int             `json:"monthlyLimit"`
	Stock          int             `json:"stock"`
	StartsAt       *time.Time      `json:"startsAt"`
	EndsAt         *time.Time      `json:"endsAt"`
	Featured       bool            `json:"featured"`
	Enabled        *bool           `json:"enabled"`
	SortOrder      int             `json:"sortOrder"`
}

// NormalizePackage 校验额度包入参，返回可落库的包（ID / AppID / Sold / 时间由调用方填）。
func NormalizePackage(in PackageInput) (Package, error) {
	out := Package{
		Name: strings.TrimSpace(in.Name), Subtitle: strings.TrimSpace(in.Subtitle),
		Description: strings.TrimSpace(in.Description), Badge: strings.TrimSpace(in.Badge),
		Tone: strings.TrimSpace(in.Tone), Points: in.Points, BonusPoints: in.BonusPoints, Price: in.Price,
		OriginalPrice: in.OriginalPrice, ValidityDays: in.ValidityDays, Audience: strings.TrimSpace(in.Audience),
		TierIDs: []int64{}, MemberDiscount: true, PerUserLimit: in.PerUserLimit, MonthlyLimit: in.MonthlyLimit,
		Stock: in.Stock, StartsAt: in.StartsAt, EndsAt: in.EndsAt, Featured: in.Featured, Enabled: true,
		SortOrder: in.SortOrder,
	}
	if in.MemberDiscount != nil {
		out.MemberDiscount = *in.MemberDiscount
	}
	if in.Enabled != nil {
		out.Enabled = *in.Enabled
	}
	if out.Name == "" || len([]rune(out.Name)) > 32 {
		return out, invalid("额度包名称应为 1-32 个字")
	}
	if len([]rune(out.Subtitle)) > 64 {
		return out, invalid("副标题不能超过 64 个字")
	}
	if len([]rune(out.Description)) > 1000 {
		return out, invalid("说明不能超过 1000 个字")
	}
	if len([]rune(out.Badge)) > 8 {
		return out, invalid("角标不能超过 8 个字")
	}
	if out.Tone == "" {
		out.Tone = "blue"
	}
	if !slices.Contains(PackageTones, out.Tone) {
		return out, invalid("配色只能是 %s", strings.Join(PackageTones, "、"))
	}
	limit := decimal.NewFromInt(1_000_000_000)
	if out.Points.Sign() <= 0 || out.Points.GreaterThan(limit) || !out.Points.Equal(out.Points.Truncate(0)) {
		return out, invalid("额度应为 1-1000000000 之间的整数")
	}
	if out.BonusPoints.Sign() < 0 || out.BonusPoints.GreaterThan(limit) || !out.BonusPoints.Equal(out.BonusPoints.Truncate(0)) {
		return out, invalid("赠送额度应为 0-1000000000 之间的整数")
	}
	maxPrice := decimal.NewFromInt(100_000)
	if out.Price.Sign() < 0 || out.Price.GreaterThan(maxPrice) || !out.Price.Equal(out.Price.Round(2)) {
		return out, invalid("售价应在 0-100000 元之间，最多两位小数")
	}
	if out.OriginalPrice.Sign() < 0 || out.OriginalPrice.GreaterThan(maxPrice) || !out.OriginalPrice.Equal(out.OriginalPrice.Round(2)) {
		return out, invalid("划线价应在 0-100000 元之间，最多两位小数")
	}
	if out.OriginalPrice.Sign() > 0 && out.OriginalPrice.LessThanOrEqual(out.Price) {
		return out, invalid("划线价应高于售价（不需要时填 0）")
	}
	if out.ValidityDays < 0 || out.ValidityDays > 3650 {
		return out, invalid("有效天数应在 0-3650 之间（0 为永久）")
	}
	if out.Audience == "" {
		out.Audience = AudienceAll
	}
	if out.Audience != AudienceAll && out.Audience != AudienceVip {
		return out, invalid("购买对象只能是 all 或 vip")
	}
	for _, id := range in.TierIDs {
		if id <= 0 {
			return out, invalid("档位编号无效")
		}
		if !slices.Contains(out.TierIDs, id) {
			out.TierIDs = append(out.TierIDs, id)
		}
	}
	if len(out.TierIDs) > 0 {
		out.Audience = AudienceVip
	}
	if out.PerUserLimit < 0 || out.PerUserLimit > 100_000 || out.MonthlyLimit < 0 || out.MonthlyLimit > 100_000 {
		return out, invalid("限购次数应在 0-100000 之间（0 不限）")
	}
	if out.PerUserLimit > 0 && out.MonthlyLimit > out.PerUserLimit {
		return out, invalid("每月限购不能多于累计限购")
	}
	if out.Stock < 0 || out.Stock > 100_000_000 {
		return out, invalid("库存应在 0-100000000 之间（0 不限）")
	}
	if out.StartsAt != nil && out.EndsAt != nil && !out.EndsAt.After(*out.StartsAt) {
		return out, invalid("售卖结束时间应晚于开始时间")
	}
	if out.SortOrder < -100_000 || out.SortOrder > 100_000 {
		return out, invalid("排序值应在 -100000-100000 之间")
	}
	return out, nil
}

// PackageQuote 一位用户买这个包的报价。
type PackageQuote struct {
	FinalPrice        decimal.Decimal `json:"finalPrice"`
	MemberSaving      decimal.Decimal `json:"memberSaving"`
	MemberBonusPoints decimal.Decimal `json:"memberBonusPoints"`
	TotalPoints       decimal.Decimal `json:"totalPoints"`
	// UnitPrice 每万额度多少元（展示用）。
	UnitPrice     decimal.Decimal `json:"unitPrice"`
	DiscountLabel string          `json:"discountLabel"`
}

// QuotePackage 报价：会员折扣按分位向下取整（至少 0.01，免费包保持 0），会员多送按整额度向下取整。
func QuotePackage(pkg Package, member MemberBenefit) PackageQuote {
	quote := PackageQuote{FinalPrice: pkg.Price, MemberSaving: decimal.Zero, MemberBonusPoints: decimal.Zero}
	if pkg.MemberDiscount && member.Source != MemberSourceNone {
		labels := []string{}
		if member.PackDiscount.Sign() > 0 && member.PackDiscount.LessThan(decimal.NewFromInt(1)) && pkg.Price.Sign() > 0 {
			final := pkg.Price.Mul(member.PackDiscount).RoundFloor(2)
			if final.LessThan(decimal.New(1, -2)) {
				final = decimal.New(1, -2)
			}
			if final.LessThan(pkg.Price) {
				quote.FinalPrice = final
				quote.MemberSaving = pkg.Price.Sub(final)
				labels = append(labels, ZheLabel(member.PackDiscount))
			}
		}
		if member.PackBonusRate.Sign() > 0 {
			bonus := pkg.Points.Mul(member.PackBonusRate).Floor()
			if bonus.Sign() > 0 {
				quote.MemberBonusPoints = bonus
				labels = append(labels, "多送 "+member.PackBonusRate.Mul(decimal.NewFromInt(100)).Round(1).String()+"%")
			}
		}
		if len(labels) > 0 {
			quote.DiscountLabel = strings.TrimSpace(member.Name + " " + strings.Join(labels, "·"))
		}
	}
	quote.TotalPoints = pkg.Points.Add(pkg.BonusPoints).Add(quote.MemberBonusPoints)
	if quote.TotalPoints.Sign() > 0 {
		quote.UnitPrice = quote.FinalPrice.Div(quote.TotalPoints).Mul(decimal.NewFromInt(10_000)).Round(2)
	}
	return quote
}

// 不能购买的原因。
const (
	ReasonStoreClosed         = "store_closed"
	ReasonMembersOnly         = "members_only"
	ReasonTierRequired        = "tier_required"
	ReasonNotStarted          = "not_started"
	ReasonEnded               = "ended"
	ReasonSoldOut             = "sold_out"
	ReasonLimitReached        = "limit_reached"
	ReasonMonthlyLimitReached = "monthly_limit_reached"
)

// PackageAvailability 这位用户此刻能不能买这个包。purchased / monthly 是他已买的次数（累计 / 本月，不含已退款）。
func PackageAvailability(pkg Package, profile Profile, storeOpen bool, purchased, monthly int, now time.Time) (bool, string) {
	switch {
	case !storeOpen || !pkg.Enabled:
		return false, ReasonStoreClosed
	case pkg.Audience == AudienceVip && !profile.IsVip:
		return false, ReasonMembersOnly
	case len(pkg.TierIDs) > 0 && !slices.ContainsFunc(pkg.TierIDs, func(id int64) bool { return slices.Contains(profile.TierIDs, id) }):
		return false, ReasonTierRequired
	case pkg.StartsAt != nil && now.Before(*pkg.StartsAt):
		return false, ReasonNotStarted
	case pkg.EndsAt != nil && !now.Before(*pkg.EndsAt):
		return false, ReasonEnded
	case pkg.Stock > 0 && pkg.Sold >= pkg.Stock:
		return false, ReasonSoldOut
	case pkg.PerUserLimit > 0 && purchased >= pkg.PerUserLimit:
		return false, ReasonLimitReached
	case pkg.MonthlyLimit > 0 && monthly >= pkg.MonthlyLimit:
		return false, ReasonMonthlyLimitReached
	}
	return true, ""
}

// ReasonMessage 不能购买的说法（40235 / 40243 的提示）。
func ReasonMessage(reason string) string {
	switch reason {
	case ReasonStoreClosed:
		return "额度包已下架"
	case ReasonMembersOnly:
		return "该额度包仅限会员购买"
	case ReasonTierRequired:
		return "该额度包仅限指定会员档位购买"
	case ReasonNotStarted:
		return "该额度包尚未开售"
	case ReasonEnded:
		return "该额度包已停售"
	case ReasonSoldOut:
		return "该额度包已售罄"
	case ReasonLimitReached:
		return "已达到该额度包的限购次数"
	case ReasonMonthlyLimitReached:
		return "本月已达到该额度包的限购次数"
	}
	return "暂时不能购买"
}

// UserPackage 用户看到的额度包：包本身 + 报价 + 能否购买。
type UserPackage struct {
	ID                 int64           `json:"id"`
	Name               string          `json:"name"`
	Subtitle           string          `json:"subtitle"`
	Description        string          `json:"description"`
	Badge              string          `json:"badge"`
	Tone               string          `json:"tone"`
	Featured           bool            `json:"featured"`
	Points             decimal.Decimal `json:"points"`
	BonusPoints        decimal.Decimal `json:"bonusPoints"`
	Price              decimal.Decimal `json:"price"`
	OriginalPrice      decimal.Decimal `json:"originalPrice"`
	ValidityDays       int             `json:"validityDays"`
	Audience           string          `json:"audience"`
	TierIDs            []int64         `json:"tierIds"`
	MemberDiscount     bool            `json:"memberDiscount"`
	PerUserLimit       int             `json:"perUserLimit"`
	MonthlyLimit       int             `json:"monthlyLimit"`
	Purchased          int             `json:"purchased"`
	PurchasedThisMonth int             `json:"purchasedThisMonth"`
	Stock              int             `json:"stock"`
	Sold               int             `json:"sold"`
	RemainingStock     *int            `json:"remainingStock"`
	StartsAt           *time.Time      `json:"startsAt"`
	EndsAt             *time.Time      `json:"endsAt"`
	PackageQuote
	Purchasable bool   `json:"purchasable"`
	Reason      string `json:"reason"`
	Affordable  bool   `json:"affordable"`
}

// BuildUserPackage 组装用户视图。
func BuildUserPackage(pkg Package, quote PackageQuote, purchasable bool, reason string, purchased, monthly int, walletCents int64) UserPackage {
	out := UserPackage{
		ID: pkg.ID, Name: pkg.Name, Subtitle: pkg.Subtitle, Description: pkg.Description, Badge: pkg.Badge, Tone: pkg.Tone,
		Featured: pkg.Featured, Points: pkg.Points, BonusPoints: pkg.BonusPoints, Price: pkg.Price,
		OriginalPrice: pkg.OriginalPrice, ValidityDays: pkg.ValidityDays, Audience: pkg.Audience, TierIDs: pkg.TierIDs,
		MemberDiscount: pkg.MemberDiscount, PerUserLimit: pkg.PerUserLimit, MonthlyLimit: pkg.MonthlyLimit,
		Purchased: purchased, PurchasedThisMonth: monthly, Stock: pkg.Stock, Sold: pkg.Sold, StartsAt: pkg.StartsAt,
		EndsAt: pkg.EndsAt, PackageQuote: quote, Purchasable: purchasable, Reason: reason,
		Affordable: decimal.New(walletCents, -2).GreaterThanOrEqual(quote.FinalPrice),
	}
	if out.TierIDs == nil {
		out.TierIDs = []int64{}
	}
	if pkg.Stock > 0 {
		remaining := max(pkg.Stock-pkg.Sold, 0)
		out.RemainingStock = &remaining
	}
	return out
}

// PackageStore 用户端额度商店。
type PackageStore struct {
	Enabled bool            `json:"enabled"`
	Notice  string          `json:"notice"`
	Member  MemberBenefit   `json:"member"`
	Wallet  decimal.Decimal `json:"wallet"`
	Items   []UserPackage   `json:"items"`
}

// ── 额度批次 ──

// 批次来源。
const (
	LotPurchase = "purchase"
	LotWelcome  = "welcome"
	LotAdmin    = "admin"
	LotGrant    = "grant"
)

// 批次状态（存储值），另有对外的计算值 exhausted。
const (
	LotActive    = "active"
	LotExpired   = "expired"
	LotRevoked   = "revoked"
	LotExhausted = "exhausted"
)

// CreditLot 一个额度批次。
type CreditLot struct {
	ID        int64           `json:"id"`
	Origin    string          `json:"origin"`
	Title     string          `json:"title"`
	Amount    decimal.Decimal `json:"amount"`
	Remaining decimal.Decimal `json:"remaining"`
	Frozen    decimal.Decimal `json:"frozen"`
	Used      decimal.Decimal `json:"used"`
	OrderNo   string          `json:"orderNo,omitempty"`
	PackageID int64           `json:"packageId,omitempty"`
	ExpiresAt *time.Time      `json:"expiresAt"`
	Status    string          `json:"status"`
	CreatedAt time.Time       `json:"createdAt"`
	UserID    int64           `json:"userId,omitempty"`
	Account   string          `json:"account,omitempty"`

	AmountMicro    int64 `json:"-"`
	RemainingMicro int64 `json:"-"`
	FrozenMicro    int64 `json:"-"`
}

// Finish 由微点填出网字段与对外状态。
func (l *CreditLot) Finish() {
	l.Amount = MicroToPoints(l.AmountMicro)
	l.Remaining = MicroToPoints(l.RemainingMicro)
	l.Frozen = MicroToPoints(l.FrozenMicro)
	l.Used = MicroToPoints(max(l.AmountMicro-l.RemainingMicro-l.FrozenMicro, 0))
	if l.Status == LotActive && l.RemainingMicro == 0 && l.FrozenMicro == 0 {
		l.Status = LotExhausted
	}
}

// LotUsable 批次此刻能不能用于暂扣。
func LotUsable(status string, expiresAt *time.Time, now time.Time) bool {
	return status == LotActive && (expiresAt == nil || now.Before(*expiresAt))
}

// LotExpiry 有效天数推出的到期时间（0 为永久）。
func LotExpiry(now time.Time, validityDays int) *time.Time {
	if validityDays <= 0 {
		return nil
	}
	at := now.AddDate(0, 0, validityDays)
	return &at
}

// LotGrantInput 发放一个额度批次（管理员、新人礼、购买都走它）。
type LotGrantInput struct {
	AppID        int64
	UserID       int64
	Origin       string
	Title        string
	Amount       decimal.Decimal
	ValidityDays int
	RequestID    string
	Operator     string
	Remark       string
}

// LotQuery 批次查询。
type LotQuery struct {
	AppID  int64
	UserID int64
	// Status active / expired / revoked / exhausted；Scope=active 时只要可用或暂扣中的。
	Status string
	Origin string
	Scope  string
	Page   int
	Limit  int
}

// ── 订单 ──

const (
	OrderPaid     = "paid"
	OrderRefunded = "refunded"
)

// PackageOrder 一笔额度包订单。
type PackageOrder struct {
	OrderNo       string          `json:"orderNo"`
	PackageID     int64           `json:"packageId"`
	PackageName   string          `json:"packageName"`
	ListPrice     decimal.Decimal `json:"listPrice"`
	Price         decimal.Decimal `json:"price"`
	Discount      decimal.Decimal `json:"discount"`
	DiscountLabel string          `json:"discountLabel"`
	Points        decimal.Decimal `json:"points"`
	BonusPoints   decimal.Decimal `json:"bonusPoints"`
	TotalPoints   decimal.Decimal `json:"totalPoints"`
	ValidityDays  int             `json:"validityDays"`
	ExpiresAt     *time.Time      `json:"expiresAt"`
	LotID         int64           `json:"lotId"`
	WalletTxnNo   string          `json:"walletTxnNo"`
	Status        string          `json:"status"`
	RefundAmount  decimal.Decimal `json:"refundAmount"`
	RefundPoints  decimal.Decimal `json:"refundPoints"`
	RefundReason  string          `json:"refundReason"`
	RefundedAt    *time.Time      `json:"refundedAt"`
	CreatedAt     time.Time       `json:"createdAt"`
	UserID        int64           `json:"userId,omitempty"`
	Account       string          `json:"account,omitempty"`
	RefundedBy    string          `json:"refundedBy,omitempty"`
	// 管理端：到账批次此刻的余量与按剩余比例的预计退款（已退款的订单为空）。
	LotAmount      *decimal.Decimal `json:"lotAmount,omitempty"`
	LotRemaining   *decimal.Decimal `json:"lotRemaining,omitempty"`
	LotFrozen      *decimal.Decimal `json:"lotFrozen,omitempty"`
	RefundEstimate *decimal.Decimal `json:"refundEstimate,omitempty"`
}

// PurchaseInput 用户买额度包。
type PurchaseInput struct {
	AppID         int64
	UserID        int64
	PackageID     int64
	RequestID     string
	ExpectedPrice *decimal.Decimal
	Settings      Settings
	ClientIP      string
	Now           time.Time
}

// PurchaseResult 购买结论。
type PurchaseResult struct {
	Order    PackageOrder    `json:"order"`
	Lot      CreditLot       `json:"lot"`
	Wallet   decimal.Decimal `json:"wallet"`
	Balance  decimal.Decimal `json:"balance"`
	Replayed bool            `json:"replayed"`
}

// OrderQuery 订单查询。
type OrderQuery struct {
	AppID     int64
	UserID    int64
	PackageID int64
	Status    string
	Keyword   string
	From      *time.Time
	To        *time.Time
	Page      int
	Limit     int
}

// OrderRefundInput 管理员退额度包。
type OrderRefundInput struct {
	AppID    int64
	OrderNo  string
	Reason   string
	Operator string
}

// WelcomeResult 领新人礼的结论。
type WelcomeResult struct {
	Lot     CreditLot       `json:"lot"`
	Balance decimal.Decimal `json:"balance"`
}

// RefundByRemaining 额度包按剩余比例退的钱：floor₂(price × remaining / amount)。
func RefundByRemaining(price decimal.Decimal, remaining, amount int64) decimal.Decimal {
	if amount <= 0 || remaining <= 0 || price.Sign() <= 0 {
		return decimal.Zero
	}
	if remaining >= amount {
		return price
	}
	return price.Mul(decimal.NewFromInt(remaining)).Div(decimal.NewFromInt(amount)).RoundFloor(2)
}

// ── 价格方案与统计 ──

// PricingPlan 用户端的价格方案。
type PricingPlan struct {
	Timezone     string             `json:"timezone"`
	Now          time.Time          `json:"now"`
	Enabled      bool               `json:"enabled"`
	Stacking     string             `json:"stacking"`
	Base         decimal.Decimal    `json:"base"`
	Levels       []LevelMultiplier  `json:"levels"`
	FreeDaily    decimal.Decimal    `json:"freeDaily"`
	Windows      []TimeWindow       `json:"windows"`
	Current      FactorBreakdown    `json:"current"`
	NextChangeAt *time.Time         `json:"nextChangeAt"`
	Timeline     []TimelineSegment  `json:"timeline"`
	Member       MemberBenefit      `json:"member"`
	Tiers        []MemberBenefit    `json:"tiers"`
}

// PricingPreview 控制台的价格模拟。
type PricingPreview struct {
	Breakdown    FactorBreakdown   `json:"breakdown"`
	Member       MemberBenefit     `json:"member"`
	Timeline     []TimelineSegment `json:"timeline"`
	NextChangeAt *time.Time        `json:"nextChangeAt"`
	Price        *Price            `json:"price,omitempty"`
}

// CommerceStats 额度生意统计。金额单位元，额度单位点。
type CommerceStats struct {
	Days                 int              `json:"days"`
	Revenue              decimal.Decimal  `json:"revenue"`
	Refunded             decimal.Decimal  `json:"refunded"`
	Orders               int64            `json:"orders"`
	Buyers               int64            `json:"buyers"`
	PointsSold           decimal.Decimal  `json:"pointsSold"`
	Outstanding          decimal.Decimal  `json:"outstanding"`
	OutstandingPermanent decimal.Decimal  `json:"outstandingPermanent"`
	ExpiringSoon         decimal.Decimal  `json:"expiringSoon"`
	ByPackage            []PackageTotal   `json:"byPackage"`
	Daily                []CommerceDaily  `json:"daily"`
	Expiry               []ExpiryBucket   `json:"expiry"`
	Windows              []WindowUsage    `json:"windows"`
}

// PackageTotal 按包汇总。
type PackageTotal struct {
	PackageID int64           `json:"packageId"`
	Name      string          `json:"name"`
	Orders    int64           `json:"orders"`
	Revenue   decimal.Decimal `json:"revenue"`
	Points    decimal.Decimal `json:"points"`
}

// CommerceDaily 按日汇总。
type CommerceDaily struct {
	Date     string          `json:"date"`
	Orders   int64           `json:"orders"`
	Revenue  decimal.Decimal `json:"revenue"`
	Refunded decimal.Decimal `json:"refunded"`
}

// ExpiryBucket 未消耗额度包按到期远近分布。
type ExpiryBucket struct {
	Bucket string          `json:"bucket"`
	Amount decimal.Decimal `json:"amount"`
}

// WindowUsage 按时段汇总的调用与让利。
type WindowUsage struct {
	WindowID string          `json:"windowId"`
	Name     string          `json:"name"`
	Requests int64           `json:"requests"`
	Charged  decimal.Decimal `json:"charged"`
	Saved    decimal.Decimal `json:"saved"`
}
