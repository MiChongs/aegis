package coupon

import (
	"slices"
	"time"

	"github.com/shopspring/decimal"
)

// ── 定价引擎 ──
//
// 一笔订单最多用一个价格活动（限时折扣或满减，取优惠最多的那个）和一张券；返券活动另算，不改价格。
// 券的门槛按活动价之后的金额判（先打折再用券，与常见电商一致）。
//
// 券与活动能不能同享由双方的开关共同决定：券允许、活动也允许才叠加。不能叠加时取「只用券」与
// 「只用活动」里更便宜的那个；用券反而更贵（或一样）的券不可选，原因写明。
// 「最优」的口径是用户少付的钱：自动选券就是选让总优惠最大的那张，相同时选先过期的。
//
// 优惠后至少付 MinPayable（渠道不接受 0 元单）。

// UserFacts 资格判定需要的用户事实。
type UserFacts struct {
	RegisteredAt time.Time
	// PaidOrders 在这个应用付过款的订单数（不含这一笔）
	PaidOrders int64
	IsVip      bool
}

// AudienceMatch 用户是否属于这个人群。
func AudienceMatch(a Audience, f UserFacts, now time.Time) bool {
	switch a.Kind {
	case "", AudienceAll:
		return true
	case AudienceNewUser:
		return !f.RegisteredAt.IsZero() && now.Sub(f.RegisteredAt) < time.Duration(a.NewUserDays)*24*time.Hour
	case AudienceFirstOrder:
		return f.PaidOrders == 0
	case AudienceNonVip:
		return !f.IsVip
	case AudienceVip:
		return f.IsVip
	}
	return false
}

// AudienceLabel 人群的一句话说明。
func AudienceLabel(a Audience) string {
	switch a.Kind {
	case AudienceNewUser:
		return "注册 " + itoa(a.NewUserDays) + " 天内的新用户"
	case AudienceFirstOrder:
		return "首次付款的用户"
	case AudienceNonVip:
		return "非会员用户"
	case AudienceVip:
		return "会员用户"
	}
	return "所有用户"
}

// ScopeOfPurpose 订单用途 → 优惠范围。
func ScopeOfPurpose(purpose string) string {
	switch purpose {
	case ScopeVip, ScopeIntegral, ScopeRecharge:
		return purpose
	case "":
		return ScopeGoods
	}
	return ""
}

// Target 一笔待定价的订单。
type Target struct {
	Scope     string
	PlanID    int64
	PackageID int64
	// Amount 原价
	Amount decimal.Decimal
}

// PromotionState 一期活动与这个用户参与过几次（不含已释放与已退款的）。
type PromotionState struct {
	Promotion Promotion
	UserUsed  int64
}

// 不能用 / 没命中的原因代码。客户端可以据此本地化，Reason 是现成的中文。
const (
	ReasonLocked     = "locked"
	ReasonNotStarted = "not_started"
	ReasonExpired    = "expired"
	ReasonScope      = "scope"
	ReasonPlan       = "plan"
	ReasonThreshold  = "threshold"
	ReasonStack      = "stack"
	ReasonAudience   = "audience"
	ReasonLimit      = "limit"
	ReasonSoldOut    = "sold_out"
	ReasonBudget     = "budget"
	ReasonWindow     = "window"
	ReasonNoDiscount = "no_discount"
	ReasonStatus     = "status"
)

// AppliedPromotion 订单用上的活动。
type AppliedPromotion struct {
	ID       int64           `json:"id"`
	Name     string          `json:"name"`
	Tag      string          `json:"tag"`
	Kind     string          `json:"kind"`
	Theme    string          `json:"theme"`
	Discount decimal.Decimal `json:"discount"`
	EndsAt   *time.Time      `json:"endsAt,omitempty"`
}

// AppliedCoupon 订单用上的券。
type AppliedCoupon struct {
	ID       int64           `json:"id"`
	Name     string          `json:"name"`
	Face     string          `json:"face"`
	Discount decimal.Decimal `json:"discount"`
}

// CouponOption 试算时每张券能不能用、能省多少。
type CouponOption struct {
	Coupon     UserCoupon      `json:"coupon"`
	Face       string          `json:"face"`
	Usable     bool            `json:"usable"`
	ReasonCode string          `json:"reasonCode,omitempty"`
	Reason     string          `json:"reason,omitempty"`
	Discount   decimal.Decimal `json:"discount"`
	// TotalDiscount 用这张券时整笔订单的总优惠（含能同享的活动）
	TotalDiscount decimal.Decimal `json:"totalDiscount"`
	// Payable 用这张券时要付多少
	Payable decimal.Decimal `json:"payable"`
	Best    bool            `json:"best"`
	// DropsPromotion 用这张券就不再享受当前的活动
	DropsPromotion bool `json:"dropsPromotion,omitempty"`

	promotion *PromotionState
}

// RebatePreview 返券活动的预告。
type RebatePreview struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Tag       string          `json:"tag"`
	Eligible  bool            `json:"eligible"`
	MinAmount decimal.Decimal `json:"minAmount"`
	// Gap 还差多少能拿到（Eligible=false 时）
	Gap      decimal.Decimal `json:"gap"`
	Coupons  []RebateCoupon  `json:"coupons,omitempty"`
	Integral int64           `json:"integral,omitempty"`

	perUserLimit int
}

// Quote 一笔订单的价格。
type Quote struct {
	Scope             string            `json:"scope"`
	OriginalAmount    decimal.Decimal   `json:"originalAmount"`
	PromotionDiscount decimal.Decimal   `json:"promotionDiscount"`
	CouponDiscount    decimal.Decimal   `json:"couponDiscount"`
	TotalDiscount     decimal.Decimal   `json:"totalDiscount"`
	PayableAmount     decimal.Decimal   `json:"payableAmount"`
	Promotion         *AppliedPromotion `json:"promotion,omitempty"`
	Coupon            *AppliedCoupon    `json:"coupon,omitempty"`
	Rebate            *RebatePreview    `json:"rebate,omitempty"`
	Coupons           []CouponOption    `json:"coupons"`
	// BestCouponID 让总优惠最大的那张券（没有可用的为 0）
	BestCouponID int64    `json:"bestCouponId"`
	Notes        []string `json:"notes"`

	promotionState *PromotionState
	rebateState    *PromotionState
}

// 选券方式。
const (
	CouponAuto = "auto" // 自动选最优的
	CouponNone = "none" // 不用券
	// 其余情况按 CouponID 选
)

// QuoteInput 试算入参。
type QuoteInput struct {
	Now        time.Time
	Target     Target
	User       UserFacts
	Promotions []PromotionState
	// Coupons 用户手里可用与被占用的券（Template 必须已填）
	Coupons []UserCoupon
	// CouponMode auto / none；CouponID>0 时按它选
	CouponMode string
	CouponID   int64
	// LockedOrderNo 当前订单号（复核时：被这一笔订单自己占着的券算可用）
	LockedOrderNo string
}

// QuoteResult 试算结果；SelectedErr 非空时表示指定的那张券用不了（Reason 已写好）。
type QuoteResult struct {
	Quote       Quote
	SelectedErr *CouponOption
}

// PriceQuote 计算一笔订单的价格（纯函数）。
func PriceQuote(in QuoteInput) QuoteResult {
	amount := money(in.Target.Amount)
	q := Quote{
		Scope:             in.Target.Scope,
		OriginalAmount:    amount,
		PromotionDiscount: decimal.Zero,
		CouponDiscount:    decimal.Zero,
		TotalDiscount:     decimal.Zero,
		PayableAmount:     amount,
		Coupons:           []CouponOption{},
		Notes:             []string{},
	}
	result := QuoteResult{}
	if !amount.IsPositive() {
		result.Quote = q
		return result
	}

	// 1. 价格活动：每期算出对原价的优惠，取最大
	type priced struct {
		state    *PromotionState
		discount decimal.Decimal
	}
	pricedList := make([]priced, 0, len(in.Promotions))
	for index := range in.Promotions {
		state := &in.Promotions[index]
		p := state.Promotion
		if p.Kind != PromotionDiscount && p.Kind != PromotionThreshold {
			continue
		}
		if code, _ := PromotionEligible(p, state.UserUsed, in.Target, in.User, in.Now); code != "" {
			continue
		}
		d := PromotionDiscountOn(p, amount)
		if !d.IsPositive() {
			continue
		}
		if p.Budget.IsPositive() && p.DiscountTotal.Add(d).GreaterThan(p.Budget) {
			continue
		}
		pricedList = append(pricedList, priced{state: state, discount: d})
	}
	betterPromotion := func(a, b priced) bool {
		if c := a.discount.Cmp(b.discount); c != 0 {
			return c > 0
		}
		if a.state.Promotion.Priority != b.state.Promotion.Priority {
			return a.state.Promotion.Priority > b.state.Promotion.Priority
		}
		return a.state.Promotion.ID < b.state.Promotion.ID
	}
	var best *priced
	for index := range pricedList {
		if best == nil || betterPromotion(pricedList[index], *best) {
			best = &pricedList[index]
		}
	}
	bestStackable := func(allowCoupon bool) *priced {
		if !allowCoupon {
			return nil
		}
		var found *priced
		for index := range pricedList {
			item := pricedList[index]
			if !item.state.Promotion.StackWithCoupon {
				continue
			}
			if found == nil || betterPromotion(item, *found) {
				found = &pricedList[index]
			}
		}
		return found
	}
	noCouponDiscount := decimal.Zero
	if best != nil {
		noCouponDiscount = best.discount
	}

	// 2. 每张券：能不能用、配哪个活动最省
	options := make([]CouponOption, 0, len(in.Coupons))
	for _, c := range in.Coupons {
		option := CouponOption{Coupon: c, Discount: decimal.Zero, TotalDiscount: decimal.Zero, Payable: amount}
		if c.Template != nil {
			option.Face = TemplateFace(*c.Template)
		}
		if code, reason := couponBasicCheck(c, in.Target, in.Now, in.LockedOrderNo); code != "" {
			option.ReasonCode, option.Reason = code, reason
			options = append(options, option)
			continue
		}
		t := *c.Template
		// 只用券
		alone, aloneCode, aloneReason := CouponDiscountOn(t, amount)
		total := alone
		var pair *priced
		if stack := bestStackable(t.StackWithPromotion); stack != nil {
			base := amount.Sub(stack.discount)
			withPromo, code, _ := CouponDiscountOn(t, base)
			if code == "" && stack.discount.Add(withPromo).GreaterThan(total) {
				total = stack.discount.Add(withPromo)
				pair = stack
				option.Discount = withPromo
			}
		}
		if pair == nil {
			if aloneCode != "" {
				option.ReasonCode, option.Reason = aloneCode, aloneReason
				options = append(options, option)
				continue
			}
			option.Discount = alone
		}
		if !total.GreaterThan(noCouponDiscount) {
			option.ReasonCode = ReasonStack
			if best != nil {
				option.Reason = "不能与「" + best.state.Promotion.Name + "」同享，当前活动更优惠"
			} else {
				option.Reason = "这笔订单用不上这张券"
			}
			option.Discount = decimal.Zero
			options = append(options, option)
			continue
		}
		option.Usable = true
		option.TotalDiscount = total
		option.Payable = amount.Sub(total)
		if pair != nil {
			option.promotion = pair.state
		}
		option.DropsPromotion = best != nil && (pair == nil || pair.state.Promotion.ID != best.state.Promotion.ID)
		options = append(options, option)
	}

	// 3. 最优券：总优惠最大，相同选先过期的，再相同选编号小的
	bestCoupon := -1
	for index, option := range options {
		if !option.Usable {
			continue
		}
		if bestCoupon < 0 {
			bestCoupon = index
			continue
		}
		cur := options[bestCoupon]
		switch c := option.TotalDiscount.Cmp(cur.TotalDiscount); {
		case c > 0:
			bestCoupon = index
		case c == 0 && option.Coupon.ValidUntil.Before(cur.Coupon.ValidUntil):
			bestCoupon = index
		case c == 0 && option.Coupon.ValidUntil.Equal(cur.Coupon.ValidUntil) && option.Coupon.ID < cur.Coupon.ID:
			bestCoupon = index
		}
	}
	if bestCoupon >= 0 {
		options[bestCoupon].Best = true
		q.BestCouponID = options[bestCoupon].Coupon.ID
	}

	// 4. 选券
	chosen := -1
	switch {
	case in.CouponID > 0:
		for index, option := range options {
			if option.Coupon.ID == in.CouponID {
				if option.Usable {
					chosen = index
				} else {
					failed := option
					result.SelectedErr = &failed
				}
				break
			}
		}
		if chosen < 0 && result.SelectedErr == nil {
			result.SelectedErr = &CouponOption{Coupon: UserCoupon{ID: in.CouponID}, ReasonCode: ReasonStatus, Reason: "这张券不存在或已不可用"}
		}
	case in.CouponMode == CouponAuto:
		chosen = bestCoupon
	}

	// 5. 落到价格上
	if chosen >= 0 {
		option := options[chosen]
		if option.promotion != nil {
			q.setPromotion(option.promotion, option.TotalDiscount.Sub(option.Discount))
		}
		if option.DropsPromotion {
			q.Notes = append(q.Notes, "使用这张券后不再享受「"+best.state.Promotion.Name+"」")
		}
		q.CouponDiscount = option.Discount
		q.Coupon = &AppliedCoupon{ID: option.Coupon.ID, Face: option.Face, Discount: option.Discount}
		if option.Coupon.Template != nil {
			q.Coupon.Name = option.Coupon.Template.Name
		}
	} else if best != nil {
		q.setPromotion(best.state, best.discount)
	}
	q.TotalDiscount = q.PromotionDiscount.Add(q.CouponDiscount)
	q.PayableAmount = amount.Sub(q.TotalDiscount)
	q.Coupons = options

	// 6. 返券活动
	q.Rebate, q.rebateState = pickRebate(in.Promotions, in.Target, in.User, q.PayableAmount, in.Now)
	result.Quote = q
	return result
}

func (q *Quote) setPromotion(state *PromotionState, discount decimal.Decimal) {
	p := state.Promotion
	q.promotionState = state
	q.PromotionDiscount = discount
	q.Promotion = &AppliedPromotion{ID: p.ID, Name: p.Name, Tag: PromotionTag(p), Kind: p.Kind, Theme: p.Theme, Discount: discount, EndsAt: p.EndsAt}
}

// Reservation 这份价格要预占的东西。
func (q Quote) Reservation(purpose string, planID, packageID int64) Reservation {
	r := Reservation{
		OriginalAmount: q.OriginalAmount, PromotionDiscount: q.PromotionDiscount,
		CouponDiscount: q.CouponDiscount, PayableAmount: q.PayableAmount,
		Purpose: purpose, PlanID: planID, PackageID: packageID,
	}
	if q.promotionState != nil {
		p := q.promotionState.Promotion
		r.PromotionID, r.PromotionName, r.PromotionPerUserLimit = p.ID, p.Name, p.PerUserLimit
	}
	if q.Coupon != nil {
		r.UserCouponID, r.CouponName = q.Coupon.ID, q.Coupon.Name
		for _, option := range q.Coupons {
			if option.Coupon.ID == q.Coupon.ID {
				r.TemplateID = option.Coupon.TemplateID
			}
		}
	}
	if q.Rebate != nil && q.Rebate.Eligible && q.rebateState != nil {
		r.RebatePromotionID, r.RebatePromotionName = q.Rebate.ID, q.Rebate.Name
		r.RebatePerUserLimit = q.Rebate.perUserLimit
	}
	return r
}

// pickRebate 命中的返券活动：优先级最高的那个；一个都没达到门槛时给出差得最少的那个做预告。
func pickRebate(states []PromotionState, target Target, facts UserFacts, payable decimal.Decimal, now time.Time) (*RebatePreview, *PromotionState) {
	var hit, near *PromotionState
	for index := range states {
		state := &states[index]
		p := state.Promotion
		if p.Kind != PromotionRebate {
			continue
		}
		if code, _ := PromotionEligible(p, state.UserUsed, target, facts, now); code != "" {
			continue
		}
		if payable.GreaterThanOrEqual(p.Rule.MinAmount) {
			if hit == nil || p.Priority > hit.Promotion.Priority || (p.Priority == hit.Promotion.Priority && p.ID < hit.Promotion.ID) {
				hit = state
			}
			continue
		}
		if near == nil || p.Rule.MinAmount.LessThan(near.Promotion.Rule.MinAmount) {
			near = state
		}
	}
	pick := hit
	if pick == nil {
		pick = near
	}
	if pick == nil {
		return nil, nil
	}
	p := pick.Promotion
	preview := &RebatePreview{
		ID: p.ID, Name: p.Name, Tag: PromotionTag(p), Eligible: hit != nil,
		MinAmount: p.Rule.MinAmount, Gap: decimal.Zero, Coupons: p.Rule.RebateCoupons, Integral: p.Rule.RebateIntegral,
		perUserLimit: p.PerUserLimit,
	}
	if hit == nil {
		preview.Gap = p.Rule.MinAmount.Sub(payable)
		return preview, nil
	}
	return preview, pick
}

// PromotionEligible 活动对这笔订单、这个用户是否生效（不看金额能不能打出折扣）。返回原因代码与说明。
func PromotionEligible(p Promotion, userUsed int64, target Target, facts UserFacts, now time.Time) (string, string) {
	if p.Status != StatusActive {
		return ReasonStatus, "活动不在进行中"
	}
	if p.StartsAt != nil && now.Before(*p.StartsAt) {
		return ReasonWindow, "活动还没开始"
	}
	if p.EndsAt != nil && !now.Before(*p.EndsAt) {
		return ReasonWindow, "活动已结束"
	}
	if !slices.Contains(p.Scopes, target.Scope) {
		return ReasonScope, "不适用于这类订单"
	}
	if target.Scope == ScopeVip && len(p.PlanIDs) > 0 && !slices.Contains(p.PlanIDs, target.PlanID) {
		return ReasonPlan, "不适用于这个套餐"
	}
	if target.Scope == ScopeRecharge && len(p.PackageIDs) > 0 && !slices.Contains(p.PackageIDs, target.PackageID) {
		return ReasonPlan, "不适用于这个充值档位"
	}
	if !AudienceMatch(p.Audience, facts, now) {
		return ReasonAudience, "仅限" + AudienceLabel(p.Audience)
	}
	if p.PerUserLimit > 0 && userUsed >= int64(p.PerUserLimit) {
		return ReasonLimit, "已参与过这个活动"
	}
	if p.TotalLimit > 0 && p.UsedCount >= p.TotalLimit {
		return ReasonSoldOut, "活动名额已满"
	}
	return "", ""
}

// PromotionDiscountOn 价格活动对这个金额能优惠多少（不满足门槛为 0；保证优惠后至少付 MinPayable）。
func PromotionDiscountOn(p Promotion, amount decimal.Decimal) decimal.Decimal {
	d := decimal.Zero
	switch p.Kind {
	case PromotionDiscount:
		switch p.Rule.DiscountMode {
		case DiscountPercent:
			d = percentOff(amount, p.Rule.Rate, p.Rule.MaxDiscount)
		case DiscountAmountOff:
			d = p.Rule.Amount
		case DiscountFixedPrice:
			if p.Rule.Amount.LessThan(amount) {
				d = amount.Sub(p.Rule.Amount)
			}
		}
	case PromotionThreshold:
		for _, tier := range p.Rule.Tiers {
			if amount.GreaterThanOrEqual(tier.MinAmount) {
				d = tier.Off
			}
		}
	}
	return clampDiscount(d, amount)
}

// CouponDiscountOn 券对这个金额能优惠多少。不满足门槛时返回原因。
func CouponDiscountOn(t Template, base decimal.Decimal) (decimal.Decimal, string, string) {
	if t.MinAmount.IsPositive() && base.LessThan(t.MinAmount) {
		return decimal.Zero, ReasonThreshold, "还差 " + trimMoney(t.MinAmount.Sub(base)) + " 元可用"
	}
	d := decimal.Zero
	switch t.Kind {
	case KindAmountOff:
		d = t.Amount
	case KindPercentOff:
		d = percentOff(base, t.Rate, t.MaxDiscount)
	}
	d = clampDiscount(d, base)
	if !d.IsPositive() {
		return decimal.Zero, ReasonNoDiscount, "这笔订单已是最低价"
	}
	return d, "", ""
}

func percentOff(amount decimal.Decimal, rate int, maxDiscount decimal.Decimal) decimal.Decimal {
	pay := amount.Mul(decimal.NewFromInt(int64(rate))).Div(decimal.NewFromInt(100)).Round(2)
	d := amount.Sub(pay)
	if maxDiscount.IsPositive() && d.GreaterThan(maxDiscount) {
		d = maxDiscount
	}
	return d
}

// clampDiscount 优惠不为负、优惠后至少付 MinPayable。
func clampDiscount(d, amount decimal.Decimal) decimal.Decimal {
	d = money(d)
	if d.IsNegative() {
		return decimal.Zero
	}
	ceiling := amount.Sub(MinPayable)
	if ceiling.IsNegative() {
		return decimal.Zero
	}
	if d.GreaterThan(ceiling) {
		return ceiling
	}
	return d
}

// couponBasicCheck 与金额无关的用券条件：状态、时间、范围、套餐。
func couponBasicCheck(c UserCoupon, target Target, now time.Time, lockedOrderNo string) (string, string) {
	if c.Template == nil {
		return ReasonStatus, "券的规则读取失败"
	}
	switch c.Status {
	case CouponAvailable:
	case CouponLocked:
		if lockedOrderNo == "" || c.OrderNo != lockedOrderNo {
			return ReasonLocked, "已用于一笔待支付订单，订单关闭后返还"
		}
	default:
		return ReasonStatus, "这张券已不可用"
	}
	if !now.Before(c.ValidUntil) {
		return ReasonExpired, "已过期"
	}
	if now.Before(c.ValidFrom) {
		return ReasonNotStarted, c.ValidFrom.Format("2006-01-02 15:04") + " 起可用"
	}
	t := *c.Template
	if !slices.Contains(t.Scopes, target.Scope) {
		return ReasonScope, "不适用于这类订单"
	}
	if target.Scope == ScopeVip && len(t.PlanIDs) > 0 && !slices.Contains(t.PlanIDs, target.PlanID) {
		return ReasonPlan, "不适用于这个套餐"
	}
	return "", ""
}

// ClaimCheck 领券中心里这张券此刻能不能领（不含库存与已领张数以外的事务内复核）。
// claimed 为这个人已经领过几张。
func ClaimCheck(t Template, claimed int64, facts UserFacts, now time.Time) (string, string) {
	if t.Status != StatusActive || !t.Claimable {
		return ReasonStatus, "现在不能领取"
	}
	if t.ClaimStartsAt != nil && now.Before(*t.ClaimStartsAt) {
		return ReasonWindow, t.ClaimStartsAt.Format("01-02 15:04") + " 开抢"
	}
	if t.ClaimEndsAt != nil && !now.Before(*t.ClaimEndsAt) {
		return ReasonWindow, "领取已结束"
	}
	if t.ValidityMode == ValidityFixed && t.ValidUntil != nil && !now.Before(*t.ValidUntil) {
		return ReasonExpired, "券已过期"
	}
	if t.TotalQuantity > 0 && t.IssuedCount >= t.TotalQuantity {
		return ReasonSoldOut, "已抢光"
	}
	if t.PerUserLimit > 0 && claimed >= int64(t.PerUserLimit) {
		return ReasonLimit, "已领取"
	}
	if !AudienceMatch(t.Audience, facts, now) {
		return ReasonAudience, "仅限" + AudienceLabel(t.Audience)
	}
	return "", ""
}

// BestPromotionPrice 不用券时这件商品的活动价（列表上的划线价用）。没有命中活动时 promotion 为 nil。
func BestPromotionPrice(states []PromotionState, target Target, facts UserFacts, now time.Time) (decimal.Decimal, *AppliedPromotion) {
	result := PriceQuote(QuoteInput{Now: now, Target: target, User: facts, Promotions: states, CouponMode: CouponNone})
	return result.Quote.PayableAmount, result.Quote.Promotion
}

func itoa(n int) string { return decimal.NewFromInt(int64(n)).String() }
