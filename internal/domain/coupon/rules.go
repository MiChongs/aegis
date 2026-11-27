package coupon

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shopspring/decimal"
)

// 上限。
const (
	MaxNameRunes        = 32
	MaxDescriptionRunes = 200
	MaxTagRunes         = 12
	MaxValidDays        = 3650
	MaxDelayDays        = 365
	MaxPlanIDs          = 50
	MaxTiers            = 5
	MaxRebateCoupons    = 5
	MaxRebateCount      = 20
	MaxRebateIntegral   = 10_000_000
	MaxIssueUsers       = 500
	MaxIssueCount       = 20
	MaxPerUserLimit     = 1000
	MaxNewUserDays      = 365
	// MaxCouponGrantCount 其它渠道（卡密、积分商城、抽奖……）一次最多发几张
	MaxCouponGrantCount = 20
)

// MaxMoney 金额字段的上限（防手滑）。
var MaxMoney = decimal.NewFromInt(1_000_000)

// MinPayable 优惠后订单最少要付的钱：渠道不接受 0 元单。
var MinPayable = decimal.NewFromFloat(0.01)

var (
	templateStatuses  = []string{StatusDraft, StatusActive, StatusPaused, StatusEnded}
	couponKinds       = []string{KindAmountOff, KindPercentOff}
	priceScopes       = []string{ScopeVip, ScopeIntegral, ScopeGoods}
	rebateScopes      = []string{ScopeVip, ScopeIntegral, ScopeGoods, ScopeRecharge}
	promotionKinds    = []string{PromotionDiscount, PromotionThreshold, PromotionRebate}
	discountModes     = []string{DiscountPercent, DiscountAmountOff, DiscountFixedPrice}
	audienceKinds     = []string{AudienceAll, AudienceNewUser, AudienceFirstOrder, AudienceNonVip, AudienceVip}
	themes            = []string{ThemeRed, ThemeOrange, ThemeGold, ThemeGreen, ThemeBlue, ThemePurple}
	userCouponStatus  = []string{CouponAvailable, CouponLocked, CouponUsed, CouponExpired, CouponRevoked, CouponNotStarted}
	couponSources     = []string{SourceClaim, SourceAdmin, SourceRebate, SourceCardKey, SourcePointsStore, SourceLottery, SourceItemGrant, SourceInvite, SourceLeaderboard, SourceRefund}
	orderStatuses     = []string{OrderReserved, OrderSettled, OrderReleased, OrderRefunded}
	rebateStatusesAll = []string{RebateNone, RebatePending, RebateGranted, RebateSkipped, RebateRevoked, RebateFailed}
)

// ValidTemplateStatus / ValidPromotionStatus 状态是否认识（两者共用一套状态）。
func ValidTemplateStatus(status string) bool { return slices.Contains(templateStatuses, status) }

// ValidUserCouponStatus 用户券筛选状态是否认识（含 usable）。
func ValidUserCouponStatus(status string) bool {
	return status == "" || status == "usable" || slices.Contains(userCouponStatus, status)
}

// ValidOrderStatus 订单优惠记录的状态是否认识。
func ValidOrderStatus(status string) bool {
	return status == "" || slices.Contains(orderStatuses, status)
}

// ValidSource 来源是否认识。
func ValidSource(source string) bool { return slices.Contains(couponSources, source) }

// CanTransition 状态能不能这样改。草稿可以上线；进行中可以暂停、结束；暂停可以恢复、结束；结束是终点。
func CanTransition(from, to string) bool {
	if from == to {
		return true
	}
	switch from {
	case StatusDraft:
		return to == StatusActive || to == StatusEnded
	case StatusActive:
		return to == StatusPaused || to == StatusEnded
	case StatusPaused:
		return to == StatusActive || to == StatusEnded
	}
	return false
}

// ── 清洗 ──

func cleanText(text string, max int) string {
	text = strings.TrimSpace(text)
	if utf8.RuneCountInString(text) > max {
		text = string([]rune(text)[:max])
	}
	return text
}

func cleanScopes(scopes []string, allowed []string) []string {
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		scope = strings.TrimSpace(scope)
		if slices.Contains(allowed, scope) && !slices.Contains(out, scope) {
			out = append(out, scope)
		}
	}
	slices.SortFunc(out, func(a, b string) int { return slices.Index(allowed, a) - slices.Index(allowed, b) })
	return out
}

func cleanIDs(ids []int64) []int64 {
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id > 0 && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

func money(d decimal.Decimal) decimal.Decimal { return d.Round(2) }

func cleanTheme(theme string) string {
	theme = strings.TrimSpace(theme)
	if slices.Contains(themes, theme) {
		return theme
	}
	return ThemeRed
}

func cleanAudience(a Audience) Audience {
	a.Kind = strings.TrimSpace(a.Kind)
	if a.Kind == "" {
		a.Kind = AudienceAll
	}
	if a.Kind != AudienceNewUser {
		a.NewUserDays = 0
	}
	return a
}

// NormalizeTemplate 清洗模板输入：去空白、截断、范围去重排序、按方式清掉用不上的字段。
func NormalizeTemplate(in TemplateInput) TemplateInput {
	in.Name = cleanText(in.Name, MaxNameRunes+1)
	in.Description = cleanText(in.Description, MaxDescriptionRunes+1)
	in.Theme = cleanTheme(in.Theme)
	in.Kind = strings.TrimSpace(in.Kind)
	in.Amount = money(in.Amount)
	in.MaxDiscount = money(in.MaxDiscount)
	in.MinAmount = money(in.MinAmount)
	switch in.Kind {
	case KindAmountOff:
		in.Rate = 0
		in.MaxDiscount = decimal.Zero
	case KindPercentOff:
		in.Amount = decimal.Zero
	}
	in.Scopes = cleanScopes(in.Scopes, priceScopes)
	in.PlanIDs = cleanIDs(in.PlanIDs)
	if !slices.Contains(in.Scopes, ScopeVip) {
		in.PlanIDs = []int64{}
	}
	in.ValidityMode = strings.TrimSpace(in.ValidityMode)
	switch in.ValidityMode {
	case ValidityFixed:
		in.ValidDays, in.ValidDelayDays = 0, 0
	case ValidityRelative:
		in.ValidFrom, in.ValidUntil = nil, nil
	}
	in.Audience = cleanAudience(in.Audience)
	if !in.Claimable {
		in.ClaimStartsAt, in.ClaimEndsAt = nil, nil
	}
	return in
}

// ValidateTemplate 校验清洗后的模板输入。
func ValidateTemplate(in TemplateInput) error {
	if in.Name == "" {
		return fmt.Errorf("请填写券的名称")
	}
	if utf8.RuneCountInString(in.Name) > MaxNameRunes {
		return fmt.Errorf("名称最多 %d 个字", MaxNameRunes)
	}
	if utf8.RuneCountInString(in.Description) > MaxDescriptionRunes {
		return fmt.Errorf("说明最多 %d 个字", MaxDescriptionRunes)
	}
	switch in.Kind {
	case KindAmountOff:
		if !in.Amount.IsPositive() || in.Amount.GreaterThan(MaxMoney) {
			return fmt.Errorf("立减金额需要在 0.01–%s 之间", MaxMoney.String())
		}
		if in.MinAmount.IsPositive() && in.Amount.GreaterThanOrEqual(in.MinAmount) {
			return fmt.Errorf("立减金额要小于使用门槛（满 %s 减 %s 等于白送）", in.MinAmount.StringFixed(2), in.Amount.StringFixed(2))
		}
	case KindPercentOff:
		if in.Rate < 1 || in.Rate > 99 {
			return fmt.Errorf("折扣需要在 0.1–9.9 折之间（付款比例 1–99%%）")
		}
		if in.MaxDiscount.IsNegative() || in.MaxDiscount.GreaterThan(MaxMoney) {
			return fmt.Errorf("最多优惠金额需要在 0–%s 之间（0 为不封顶）", MaxMoney.String())
		}
	default:
		return fmt.Errorf("请选择优惠方式（立减或折扣）")
	}
	if in.MinAmount.IsNegative() || in.MinAmount.GreaterThan(MaxMoney) {
		return fmt.Errorf("使用门槛需要在 0–%s 之间", MaxMoney.String())
	}
	if len(in.Scopes) == 0 {
		return fmt.Errorf("请至少选择一种适用订单（会员、积分充值或普通商品）")
	}
	if len(in.PlanIDs) > MaxPlanIDs {
		return fmt.Errorf("指定套餐最多 %d 个", MaxPlanIDs)
	}
	switch in.ValidityMode {
	case ValidityFixed:
		if in.ValidUntil == nil {
			return fmt.Errorf("请填写券的失效时间")
		}
		if in.ValidFrom != nil && !in.ValidFrom.Before(*in.ValidUntil) {
			return fmt.Errorf("券的生效时间要早于失效时间")
		}
	case ValidityRelative:
		if in.ValidDays < 1 || in.ValidDays > MaxValidDays {
			return fmt.Errorf("有效天数需要在 1–%d 之间", MaxValidDays)
		}
		if in.ValidDelayDays < 0 || in.ValidDelayDays > MaxDelayDays {
			return fmt.Errorf("延迟生效天数需要在 0–%d 之间", MaxDelayDays)
		}
	default:
		return fmt.Errorf("请选择有效期方式（固定时段或领取后生效）")
	}
	if in.TotalQuantity < 0 || in.TotalQuantity > 100_000_000 {
		return fmt.Errorf("发放总量需要在 0–100000000 之间（0 为不限）")
	}
	if in.PerUserLimit < 0 || in.PerUserLimit > MaxPerUserLimit {
		return fmt.Errorf("每人限领需要在 0–%d 之间（0 为不限）", MaxPerUserLimit)
	}
	if in.ClaimStartsAt != nil && in.ClaimEndsAt != nil && !in.ClaimStartsAt.Before(*in.ClaimEndsAt) {
		return fmt.Errorf("领取开始时间要早于结束时间")
	}
	return validateAudience(in.Audience)
}

func validateAudience(a Audience) error {
	if !slices.Contains(audienceKinds, a.Kind) {
		return fmt.Errorf("人群条件不认识：%s", a.Kind)
	}
	if a.Kind == AudienceNewUser && (a.NewUserDays < 1 || a.NewUserDays > MaxNewUserDays) {
		return fmt.Errorf("新用户的天数需要在 1–%d 之间", MaxNewUserDays)
	}
	return nil
}

// RuleLocked 已发出券的模板，规则字段与原来是否一致（不一致就不能保存）。
func RuleLocked(old Template, in TemplateInput) bool {
	return old.Kind == in.Kind && old.Amount.Equal(in.Amount) && old.Rate == in.Rate &&
		old.MaxDiscount.Equal(in.MaxDiscount) && old.MinAmount.Equal(in.MinAmount) &&
		slices.Equal(old.Scopes, in.Scopes) && slices.Equal(old.PlanIDs, in.PlanIDs) &&
		old.StackWithPromotion == in.StackWithPromotion && old.ValidityMode == in.ValidityMode &&
		sameTime(old.ValidFrom, in.ValidFrom) && sameTime(old.ValidUntil, in.ValidUntil) &&
		old.ValidDays == in.ValidDays && old.ValidDelayDays == in.ValidDelayDays
}

func sameTime(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

// CouponWindow 发券这一刻的有效期。固定时段的券生效时间空着就是发放时。
func CouponWindow(t Template, now time.Time) (time.Time, time.Time) {
	if t.ValidityMode == ValidityRelative {
		from := now.AddDate(0, 0, t.ValidDelayDays)
		return from, from.AddDate(0, 0, t.ValidDays)
	}
	from := now
	if t.ValidFrom != nil && t.ValidFrom.After(now) {
		from = *t.ValidFrom
	}
	until := now
	if t.ValidUntil != nil {
		until = *t.ValidUntil
	}
	return from, until
}

// Grantable 模板此刻能不能发（各渠道共用）：状态、固定时段是否已过、库存。
// count 为这次要发的张数。返回不能发的原因。
func Grantable(t Template, count int, ignoreStatus bool, now time.Time) error {
	switch t.Status {
	case StatusActive:
	case StatusPaused:
		if !ignoreStatus {
			return fmt.Errorf("「%s」已暂停发放", t.Name)
		}
	default:
		return fmt.Errorf("「%s」不在发放中", t.Name)
	}
	if t.ValidityMode == ValidityFixed && t.ValidUntil != nil && !now.Before(*t.ValidUntil) {
		return fmt.Errorf("「%s」已过了有效期", t.Name)
	}
	if t.TotalQuantity > 0 && t.IssuedCount+int64(count) > t.TotalQuantity {
		return fmt.Errorf("「%s」已发完", t.Name)
	}
	return nil
}

// NormalizePromotion 清洗活动输入。
func NormalizePromotion(in PromotionInput) PromotionInput {
	in.Name = cleanText(in.Name, MaxNameRunes+1)
	in.Description = cleanText(in.Description, MaxDescriptionRunes+1)
	in.Tag = cleanText(in.Tag, MaxTagRunes+1)
	in.Theme = cleanTheme(in.Theme)
	in.Kind = strings.TrimSpace(in.Kind)
	rule := in.Rule
	clean := PromotionRule{Amount: decimal.Zero, MaxDiscount: decimal.Zero, MinAmount: decimal.Zero}
	switch in.Kind {
	case PromotionDiscount:
		clean.DiscountMode = strings.TrimSpace(rule.DiscountMode)
		switch clean.DiscountMode {
		case DiscountPercent:
			clean.Rate = rule.Rate
			clean.MaxDiscount = money(rule.MaxDiscount)
		case DiscountAmountOff, DiscountFixedPrice:
			clean.Amount = money(rule.Amount)
		}
		in.Scopes = cleanScopes(in.Scopes, priceScopes)
	case PromotionThreshold:
		tiers := make([]ThresholdTier, 0, len(rule.Tiers))
		for _, tier := range rule.Tiers {
			tiers = append(tiers, ThresholdTier{MinAmount: money(tier.MinAmount), Off: money(tier.Off)})
		}
		slices.SortFunc(tiers, func(a, b ThresholdTier) int { return a.MinAmount.Cmp(b.MinAmount) })
		clean.Tiers = tiers
		in.Scopes = cleanScopes(in.Scopes, priceScopes)
	case PromotionRebate:
		clean.MinAmount = money(rule.MinAmount)
		items := make([]RebateCoupon, 0, len(rule.RebateCoupons))
		for _, item := range rule.RebateCoupons {
			if item.TemplateID <= 0 {
				continue
			}
			if index := slices.IndexFunc(items, func(x RebateCoupon) bool { return x.TemplateID == item.TemplateID }); index >= 0 {
				items[index].Count += item.Count
				continue
			}
			items = append(items, RebateCoupon{TemplateID: item.TemplateID, Count: item.Count})
		}
		clean.RebateCoupons = items
		clean.RebateIntegral = rule.RebateIntegral
		in.Scopes = cleanScopes(in.Scopes, rebateScopes)
		// 返券不改价格，谈不上与券叠不叠加
		in.StackWithCoupon = true
		in.Budget = decimal.Zero
	}
	in.Rule = clean
	in.PlanIDs = cleanIDs(in.PlanIDs)
	if !slices.Contains(in.Scopes, ScopeVip) {
		in.PlanIDs = []int64{}
	}
	in.PackageIDs = cleanIDs(in.PackageIDs)
	if !slices.Contains(in.Scopes, ScopeRecharge) {
		in.PackageIDs = []int64{}
	}
	in.Audience = cleanAudience(in.Audience)
	in.Budget = money(in.Budget)
	return in
}

// ValidatePromotion 校验清洗后的活动输入。
func ValidatePromotion(in PromotionInput) error {
	if in.Name == "" {
		return fmt.Errorf("请填写活动名称")
	}
	if utf8.RuneCountInString(in.Name) > MaxNameRunes {
		return fmt.Errorf("活动名称最多 %d 个字", MaxNameRunes)
	}
	if utf8.RuneCountInString(in.Description) > MaxDescriptionRunes {
		return fmt.Errorf("活动说明最多 %d 个字", MaxDescriptionRunes)
	}
	if utf8.RuneCountInString(in.Tag) > MaxTagRunes {
		return fmt.Errorf("角标最多 %d 个字", MaxTagRunes)
	}
	rule := in.Rule
	switch in.Kind {
	case PromotionDiscount:
		switch rule.DiscountMode {
		case DiscountPercent:
			if rule.Rate < 1 || rule.Rate > 99 {
				return fmt.Errorf("折扣需要在 0.1–9.9 折之间（付款比例 1–99%%）")
			}
			if rule.MaxDiscount.IsNegative() || rule.MaxDiscount.GreaterThan(MaxMoney) {
				return fmt.Errorf("最多优惠金额需要在 0–%s 之间（0 为不封顶）", MaxMoney.String())
			}
		case DiscountAmountOff:
			if !rule.Amount.IsPositive() || rule.Amount.GreaterThan(MaxMoney) {
				return fmt.Errorf("立减金额需要在 0.01–%s 之间", MaxMoney.String())
			}
		case DiscountFixedPrice:
			if !rule.Amount.GreaterThanOrEqual(MinPayable) || rule.Amount.GreaterThan(MaxMoney) {
				return fmt.Errorf("特价需要在 0.01–%s 之间", MaxMoney.String())
			}
		default:
			return fmt.Errorf("请选择折扣方式（打折、立减或特价）")
		}
	case PromotionThreshold:
		if len(rule.Tiers) == 0 || len(rule.Tiers) > MaxTiers {
			return fmt.Errorf("满减需要 1–%d 档", MaxTiers)
		}
		for index, tier := range rule.Tiers {
			if !tier.MinAmount.IsPositive() || tier.MinAmount.GreaterThan(MaxMoney) {
				return fmt.Errorf("第 %d 档的门槛需要在 0.01–%s 之间", index+1, MaxMoney.String())
			}
			if !tier.Off.IsPositive() || !tier.Off.LessThan(tier.MinAmount) {
				return fmt.Errorf("第 %d 档的减免要大于 0 且小于门槛", index+1)
			}
			if index > 0 {
				prev := rule.Tiers[index-1]
				if tier.MinAmount.Equal(prev.MinAmount) {
					return fmt.Errorf("满减门槛不能重复（%s）", tier.MinAmount.StringFixed(2))
				}
				if !tier.Off.GreaterThan(prev.Off) {
					return fmt.Errorf("门槛越高减得要越多（第 %d 档）", index+1)
				}
			}
		}
	case PromotionRebate:
		if rule.MinAmount.IsNegative() || rule.MinAmount.GreaterThan(MaxMoney) {
			return fmt.Errorf("返券门槛需要在 0–%s 之间", MaxMoney.String())
		}
		if len(rule.RebateCoupons) == 0 && rule.RebateIntegral <= 0 {
			return fmt.Errorf("请至少设置返券或返积分中的一项")
		}
		if len(rule.RebateCoupons) > MaxRebateCoupons {
			return fmt.Errorf("最多返 %d 种券", MaxRebateCoupons)
		}
		for _, item := range rule.RebateCoupons {
			if item.Count < 1 || item.Count > MaxRebateCount {
				return fmt.Errorf("每种券返 1–%d 张", MaxRebateCount)
			}
		}
		if rule.RebateIntegral < 0 || rule.RebateIntegral > MaxRebateIntegral {
			return fmt.Errorf("返积分需要在 0–%d 之间", MaxRebateIntegral)
		}
	default:
		return fmt.Errorf("请选择活动类型（限时折扣、满减或支付返券）")
	}
	if len(in.Scopes) == 0 {
		return fmt.Errorf("请至少选择一种适用订单")
	}
	if len(in.PlanIDs) > MaxPlanIDs || len(in.PackageIDs) > MaxPlanIDs {
		return fmt.Errorf("指定套餐 / 档位最多 %d 个", MaxPlanIDs)
	}
	if in.Kind == PromotionDiscount && in.Rule.DiscountMode == DiscountFixedPrice &&
		(len(in.Scopes) != 1 || in.Scopes[0] != ScopeVip || len(in.PlanIDs) == 0) {
		return fmt.Errorf("特价只能用于指定的会员套餐")
	}
	if in.StartsAt != nil && in.EndsAt != nil && !in.StartsAt.Before(*in.EndsAt) {
		return fmt.Errorf("活动开始时间要早于结束时间")
	}
	if in.PerUserLimit < 0 || in.PerUserLimit > MaxPerUserLimit {
		return fmt.Errorf("每人参与次数需要在 0–%d 之间（0 为不限）", MaxPerUserLimit)
	}
	if in.TotalLimit < 0 || in.TotalLimit > 100_000_000 {
		return fmt.Errorf("总参与次数需要在 0–100000000 之间（0 为不限）")
	}
	if in.Budget.IsNegative() || in.Budget.GreaterThan(decimal.NewFromInt(100_000_000)) {
		return fmt.Errorf("优惠预算需要在 0–100000000 之间（0 为不限）")
	}
	return validateAudience(in.Audience)
}

// ── 展示 ──

// RateLabel 付款比例 → 「8.5 折」。
func RateLabel(rate int) string {
	if rate%10 == 0 {
		return fmt.Sprintf("%d 折", rate/10)
	}
	return fmt.Sprintf("%d.%d 折", rate/10, rate%10)
}

// TemplateFace 券面一句话：「满 100 减 20」「8.5 折，最多减 30」「无门槛减 5」。
func TemplateFace(t Template) string {
	switch t.Kind {
	case KindAmountOff:
		if t.MinAmount.IsPositive() {
			return "满 " + trimMoney(t.MinAmount) + " 减 " + trimMoney(t.Amount)
		}
		return "无门槛减 " + trimMoney(t.Amount)
	case KindPercentOff:
		label := RateLabel(t.Rate)
		if t.MinAmount.IsPositive() {
			label = "满 " + trimMoney(t.MinAmount) + " 享 " + label
		}
		if t.MaxDiscount.IsPositive() {
			label += "，最多减 " + trimMoney(t.MaxDiscount)
		}
		return label
	}
	return t.Name
}

// PromotionTag 活动角标：设置了用设置的，否则按规则生成。
func PromotionTag(p Promotion) string {
	if strings.TrimSpace(p.Tag) != "" {
		return p.Tag
	}
	switch p.Kind {
	case PromotionDiscount:
		switch p.Rule.DiscountMode {
		case DiscountPercent:
			return "限时 " + RateLabel(p.Rule.Rate)
		case DiscountAmountOff:
			return "立减 " + trimMoney(p.Rule.Amount)
		case DiscountFixedPrice:
			return "特价 " + trimMoney(p.Rule.Amount)
		}
	case PromotionThreshold:
		if len(p.Rule.Tiers) > 0 {
			tier := p.Rule.Tiers[0]
			return "满 " + trimMoney(tier.MinAmount) + " 减 " + trimMoney(tier.Off)
		}
	case PromotionRebate:
		if len(p.Rule.RebateCoupons) > 0 {
			return "下单返券"
		}
		return "下单返积分"
	}
	return p.Name
}

// trimMoney 金额去掉多余的零：20.00 → 20，8.50 → 8.5。
func trimMoney(d decimal.Decimal) string {
	return d.Round(2).String()
}

// Catalog 控制台的表单由它驱动。
func Catalog() map[string]any {
	return map[string]any{
		"couponKinds":       couponKinds,
		"priceScopes":       priceScopes,
		"rebateScopes":      rebateScopes,
		"promotionKinds":    promotionKinds,
		"discountModes":     discountModes,
		"audiences":         audienceKinds,
		"themes":            themes,
		"statuses":          templateStatuses,
		"userCouponStatus":  userCouponStatus,
		"sources":           couponSources,
		"orderStatuses":     orderStatuses,
		"rebateStatuses":    rebateStatusesAll,
		"minPayable":        MinPayable.StringFixed(2),
		"maxIssueUsers":     MaxIssueUsers,
		"maxIssueCount":     MaxIssueCount,
		"maxTiers":          MaxTiers,
		"maxRebateCoupons":  MaxRebateCoupons,
		"maxNameLength":     MaxNameRunes,
		"maxDescLength":     MaxDescriptionRunes,
		"maxTagLength":      MaxTagRunes,
		"maxValidDays":      MaxValidDays,
		"maxGrantCount":     MaxCouponGrantCount,
		"maxPlanIds":        MaxPlanIDs,
		"maxRebateIntegral": MaxRebateIntegral,
	}
}
