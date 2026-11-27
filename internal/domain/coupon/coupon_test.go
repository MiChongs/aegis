package coupon

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

var now = time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func activePromo(id int64, kind string, rule PromotionRule, stack bool) PromotionState {
	return PromotionState{Promotion: Promotion{
		ID: id, Name: "活动" + itoa(int(id)), Kind: kind, Rule: rule, Status: StatusActive,
		Scopes: []string{ScopeVip, ScopeGoods}, Audience: Audience{Kind: AudienceAll}, StackWithCoupon: stack,
		Budget: decimal.Zero, DiscountTotal: decimal.Zero,
	}}
}

func coupon(id int64, t Template) UserCoupon {
	t.ID = id * 100
	if t.Scopes == nil {
		t.Scopes = []string{ScopeVip, ScopeGoods}
	}
	return UserCoupon{ID: id, TemplateID: t.ID, Status: CouponAvailable, ValidFrom: now.Add(-time.Hour),
		ValidUntil: now.Add(time.Duration(id) * time.Hour), Template: &t}
}

func vipTarget(amount string) Target { return Target{Scope: ScopeVip, PlanID: 7, Amount: d(amount)} }

func TestPercentAndCap(t *testing.T) {
	p := activePromo(1, PromotionDiscount, PromotionRule{DiscountMode: DiscountPercent, Rate: 80, MaxDiscount: d("5")}, false)
	if got := PromotionDiscountOn(p.Promotion, d("100")); !got.Equal(d("5")) {
		t.Fatalf("cap: got %s", got)
	}
	p.Promotion.Rule.MaxDiscount = decimal.Zero
	if got := PromotionDiscountOn(p.Promotion, d("19.99")); !got.Equal(d("4")) { // 19.99*0.8=15.992→15.99
		t.Fatalf("percent: got %s", got)
	}
}

func TestMinPayable(t *testing.T) {
	tpl := Template{Name: "大额", Kind: KindAmountOff, Amount: d("50"), MinAmount: decimal.Zero}
	res := PriceQuote(QuoteInput{Now: now, Target: vipTarget("10"), Coupons: []UserCoupon{coupon(1, tpl)}, CouponMode: CouponAuto})
	if !res.Quote.PayableAmount.Equal(d("0.01")) {
		t.Fatalf("payable floor: %s", res.Quote.PayableAmount)
	}
}

func TestBestPromotionAndStacking(t *testing.T) {
	big := activePromo(1, PromotionDiscount, PromotionRule{DiscountMode: DiscountAmountOff, Amount: d("30")}, false)
	small := activePromo(2, PromotionThreshold, PromotionRule{Tiers: []ThresholdTier{{MinAmount: d("50"), Off: d("5")}}}, true)
	stackTpl := Template{Name: "叠加券", Kind: KindAmountOff, Amount: d("10"), StackWithPromotion: true}
	soloTpl := Template{Name: "独享券", Kind: KindAmountOff, Amount: d("40")}
	weakTpl := Template{Name: "弱券", Kind: KindAmountOff, Amount: d("20")}
	in := QuoteInput{Now: now, Target: vipTarget("100"), Promotions: []PromotionState{big, small},
		Coupons: []UserCoupon{coupon(1, stackTpl), coupon(2, soloTpl), coupon(3, weakTpl)}, CouponMode: CouponNone}

	res := PriceQuote(in)
	if res.Quote.Promotion == nil || res.Quote.Promotion.ID != 1 || !res.Quote.PayableAmount.Equal(d("70")) {
		t.Fatalf("no coupon should pick the 30 off promotion: %+v", res.Quote)
	}
	byID := map[int64]CouponOption{}
	for _, o := range res.Quote.Coupons {
		byID[o.Coupon.ID] = o
	}
	// 叠加券：配满减 5 + 10 = 15 < 30 → 不可选
	if byID[1].Usable || byID[1].ReasonCode != ReasonStack {
		t.Fatalf("stack coupon should lose to big promotion: %+v", byID[1])
	}
	// 独享券 40 > 30 → 可选，且会放弃活动
	if !byID[2].Usable || !byID[2].DropsPromotion || !byID[2].TotalDiscount.Equal(d("40")) {
		t.Fatalf("solo coupon: %+v", byID[2])
	}
	if byID[3].Usable {
		t.Fatalf("weak coupon should be unusable")
	}
	if res.Quote.BestCouponID != 2 {
		t.Fatalf("best coupon: %d", res.Quote.BestCouponID)
	}

	in.CouponMode = CouponAuto
	res = PriceQuote(in)
	if res.Quote.Coupon == nil || res.Quote.Coupon.ID != 2 || res.Quote.Promotion != nil || !res.Quote.PayableAmount.Equal(d("60")) {
		t.Fatalf("auto: %+v", res.Quote)
	}

	// 去掉大活动：叠加券配满减 = 15，独享券 40 仍最优；指定叠加券时走满减 + 券
	in.Promotions = []PromotionState{small}
	in.CouponMode, in.CouponID = "", 1
	res = PriceQuote(in)
	if res.SelectedErr != nil || res.Quote.Promotion == nil || res.Quote.Promotion.ID != 2 || !res.Quote.PayableAmount.Equal(d("85")) {
		t.Fatalf("selected stacking coupon: %+v err=%+v", res.Quote, res.SelectedErr)
	}
}

func TestCouponThresholdAfterPromotion(t *testing.T) {
	promo := activePromo(1, PromotionDiscount, PromotionRule{DiscountMode: DiscountPercent, Rate: 80}, true)
	tpl := Template{Name: "满90减10", Kind: KindAmountOff, Amount: d("10"), MinAmount: d("90"), StackWithPromotion: true}
	res := PriceQuote(QuoteInput{Now: now, Target: vipTarget("100"), Promotions: []PromotionState{promo},
		Coupons: []UserCoupon{coupon(1, tpl)}, CouponMode: CouponAuto})
	// 打折后 80 < 90，不能叠加；单用券 10 < 20 → 不可用
	if res.Quote.Coupon != nil || !res.Quote.PayableAmount.Equal(d("80")) {
		t.Fatalf("threshold after promotion: %+v", res.Quote)
	}
}

func TestEligibilityAndPlans(t *testing.T) {
	p := activePromo(1, PromotionDiscount, PromotionRule{DiscountMode: DiscountFixedPrice, Amount: d("9.9")}, false)
	p.Promotion.PlanIDs = []int64{8}
	if code, _ := PromotionEligible(p.Promotion, 0, vipTarget("30"), UserFacts{}, now); code != ReasonPlan {
		t.Fatalf("plan filter: %s", code)
	}
	p.Promotion.PlanIDs = []int64{7}
	p.Promotion.Audience = Audience{Kind: AudienceFirstOrder}
	if code, _ := PromotionEligible(p.Promotion, 0, vipTarget("30"), UserFacts{PaidOrders: 1}, now); code != ReasonAudience {
		t.Fatalf("first order: %s", code)
	}
	p.Promotion.Audience = Audience{Kind: AudienceAll}
	p.Promotion.PerUserLimit = 1
	if code, _ := PromotionEligible(p.Promotion, 1, vipTarget("30"), UserFacts{}, now); code != ReasonLimit {
		t.Fatalf("per user: %s", code)
	}
	p.Promotion.PerUserLimit = 0
	price, applied := BestPromotionPrice([]PromotionState{p}, vipTarget("30"), UserFacts{}, now)
	if applied == nil || !price.Equal(d("9.9")) {
		t.Fatalf("fixed price: %s", price)
	}
	// 预算不够打这一单的折扣：不命中
	p.Promotion.Budget = d("25")
	p.Promotion.DiscountTotal = d("10")
	if _, applied := BestPromotionPrice([]PromotionState{p}, vipTarget("30"), UserFacts{}, now); applied != nil {
		t.Fatalf("budget should block")
	}
}

func TestCouponStatusReasons(t *testing.T) {
	tpl := Template{Name: "券", Kind: KindAmountOff, Amount: d("5")}
	locked := coupon(1, tpl)
	locked.Status, locked.OrderNo = CouponLocked, "P1"
	future := coupon(2, tpl)
	future.ValidFrom = now.Add(time.Hour)
	goodsOnly := coupon(3, Template{Name: "商品券", Kind: KindAmountOff, Amount: d("5"), Scopes: []string{ScopeGoods}})
	res := PriceQuote(QuoteInput{Now: now, Target: vipTarget("50"), Coupons: []UserCoupon{locked, future, goodsOnly}, CouponMode: CouponAuto})
	want := map[int64]string{1: ReasonLocked, 2: ReasonNotStarted, 3: ReasonScope}
	for _, o := range res.Quote.Coupons {
		if o.ReasonCode != want[o.Coupon.ID] {
			t.Fatalf("coupon %d: %s", o.Coupon.ID, o.ReasonCode)
		}
	}
	// 复核时被本单占用的券算可用
	res = PriceQuote(QuoteInput{Now: now, Target: vipTarget("50"), Coupons: []UserCoupon{locked}, CouponID: 1, LockedOrderNo: "P1"})
	if res.SelectedErr != nil || res.Quote.Coupon == nil {
		t.Fatalf("own lock: %+v", res.SelectedErr)
	}
}

func TestRebate(t *testing.T) {
	rebate := activePromo(5, PromotionRebate, PromotionRule{MinAmount: d("50"), RebateIntegral: 100}, true)
	rebate.Promotion.Scopes = []string{ScopeRecharge}
	res := PriceQuote(QuoteInput{Now: now, Target: Target{Scope: ScopeRecharge, Amount: d("30")}, Promotions: []PromotionState{rebate}})
	if res.Quote.Rebate == nil || res.Quote.Rebate.Eligible || !res.Quote.Rebate.Gap.Equal(d("20")) {
		t.Fatalf("rebate preview: %+v", res.Quote.Rebate)
	}
	res = PriceQuote(QuoteInput{Now: now, Target: Target{Scope: ScopeRecharge, Amount: d("60")}, Promotions: []PromotionState{rebate}})
	r := res.Quote.Reservation(ScopeRecharge, 0, 0)
	if !res.Quote.Rebate.Eligible || r.RebatePromotionID != 5 || !r.PayableAmount.Equal(d("60")) {
		t.Fatalf("rebate hit: %+v", r)
	}
}

func TestValidation(t *testing.T) {
	until := now.Add(24 * time.Hour)
	in := NormalizeTemplate(TemplateInput{Name: "满10减10", Kind: KindAmountOff, Amount: d("10"), MinAmount: d("10"),
		Scopes: []string{"goods", "bogus"}, ValidityMode: ValidityFixed, ValidUntil: &until})
	if err := ValidateTemplate(in); err == nil {
		t.Fatalf("amount >= threshold should fail")
	}
	if len(in.Scopes) != 1 {
		t.Fatalf("scopes not cleaned: %v", in.Scopes)
	}
	p := NormalizePromotion(PromotionInput{Name: "满减", Kind: PromotionThreshold, Scopes: []string{ScopeVip},
		Rule: PromotionRule{Tiers: []ThresholdTier{{MinAmount: d("100"), Off: d("10")}, {MinAmount: d("50"), Off: d("20")}}}})
	if err := ValidatePromotion(p); err == nil {
		t.Fatalf("higher threshold with smaller off should fail")
	}
	if !CanTransition(StatusDraft, StatusActive) || CanTransition(StatusEnded, StatusActive) {
		t.Fatalf("transitions")
	}
	if TemplateFace(Template{Kind: KindPercentOff, Rate: 85, MaxDiscount: d("30"), MinAmount: decimal.Zero}) != "8.5 折，最多减 30" {
		t.Fatalf("face")
	}
}
