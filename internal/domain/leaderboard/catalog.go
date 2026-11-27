package leaderboard

import "slices"

// 榜单 key。
const (
	BoardPointsTotal   = "points_total"
	BoardPointsDaily   = "points_daily"
	BoardPointsWeekly  = "points_weekly"
	BoardPointsMonthly = "points_monthly"
	BoardExperience    = "experience"
	BoardLevel         = "level"
)

// 取值的来源。
const (
	MetricPoints     = "points"     // 积分（总榜是余额，周期榜是周期内获得的积分）
	MetricExperience = "experience" // users.experience
	MetricLevel      = "level"      // 当前等级，同级按经验
)

// BoardSpec 一个榜单的自述。控制台的榜单开关、用户端的标签都读它。
type BoardSpec struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Metric    string `json:"metric"`
	Period    string `json:"period"`
	ValueUnit string `json:"valueUnit"`
	// Description 一句话说清取值口径
	Description    string `json:"description"`
	DefaultEnabled bool   `json:"defaultEnabled"`
}

// Periodic 是否是周期榜（日 / 周 / 月）：只有周期榜会结算、能配奖励。
func (b BoardSpec) Periodic() bool { return b.Period != PeriodTotal }

// catalog 单一事实源，顺序即默认的标签顺序。
var catalog = []BoardSpec{
	{Key: BoardPointsTotal, Label: "积分总榜", Metric: MetricPoints, Period: PeriodTotal, ValueUnit: "积分",
		Description: "当前积分余额", DefaultEnabled: true},
	{Key: BoardPointsDaily, Label: "积分日榜", Metric: MetricPoints, Period: PeriodDay, ValueUnit: "积分",
		Description: "当天获得的积分（不含退回、排行榜奖励）", DefaultEnabled: false},
	{Key: BoardPointsWeekly, Label: "积分周榜", Metric: MetricPoints, Period: PeriodWeek, ValueUnit: "积分",
		Description: "本周（周一起）获得的积分（不含退回、排行榜奖励）", DefaultEnabled: true},
	{Key: BoardPointsMonthly, Label: "积分月榜", Metric: MetricPoints, Period: PeriodMonth, ValueUnit: "积分",
		Description: "本月获得的积分（不含退回、排行榜奖励）", DefaultEnabled: true},
	{Key: BoardExperience, Label: "经验榜", Metric: MetricExperience, Period: PeriodTotal, ValueUnit: "经验",
		Description: "累计经验", DefaultEnabled: false},
	{Key: BoardLevel, Label: "等级榜", Metric: MetricLevel, Period: PeriodTotal, ValueUnit: "级",
		Description: "当前等级，同级按经验", DefaultEnabled: false},
}

// Catalog 全部榜单。
func Catalog() []BoardSpec { return slices.Clone(catalog) }

// FindBoard 按 key 取榜单。
func FindBoard(key string) (BoardSpec, bool) {
	for _, spec := range catalog {
		if spec.Key == key {
			return spec, true
		}
	}
	return BoardSpec{}, false
}

// ── 积分口径：哪些流水算「获得」 ──

// 积分流水分类（integral_transactions.category）在周期榜里的处理。
const (
	CategoryCounted  = "counted"  // 计入
	CategoryExcluded = "excluded" // 不计入
	// CategoryAdmin 管理员在控制台的调整：由设置 countAdminAdjust 决定。
	// 同一分类下没有操作管理员的流水（抽奖奖励、远程函数发的积分）照常计入，见 CategoryAdminAdjust。
	CategoryAdmin = "admin"
)

// CategoryAdminAdjust 管理员调整的分类。抽奖与远程函数也经同一个入口改积分、落同一个分类，
// 区别在于没有操作管理员（source_type = 'admin_manual' 且 source_id 为空）—— 它们是用户自己挣来的。
const CategoryAdminAdjust = "admin_adjust"

// AdminAdjustSourceType 管理员调整的 source_type；source_id 是操作管理员的 ID。
const AdminAdjustSourceType = "admin_manual"

// CategoryRule 一个积分流水分类在周期榜里怎么算。
type CategoryRule struct {
	Category string `json:"category"`
	Label    string `json:"label"`
	Rule     string `json:"rule"`
}

// integralCategories 全部写积分流水的分类（逐一查过所有写 integral_transactions 的地方）。
// 新增分类时必须在这里登记：TestIntegralCategoriesCoverEveryWriter 会把漏登记的找出来。
//
// 只统计 amount > 0 的流水，扣减类（兑换消耗、退款回收）天然不进周期榜；
// 这里把它们也列上，是为了让「漏登记」与「刻意不计」分得开。
var integralCategories = []CategoryRule{
	{Category: "daily_signin", Label: "签到奖励", Rule: CategoryCounted},
	{Category: "rewarded_ad", Label: "看广告赠送", Rule: CategoryCounted},
	{Category: "card_key", Label: "卡密赠送", Rule: CategoryCounted},
	{Category: "invite", Label: "邀请奖励", Rule: CategoryCounted},
	{Category: "vip_bonus", Label: "会员套餐赠送", Rule: CategoryCounted},
	{Category: "purchase", Label: "积分充值", Rule: CategoryCounted},
	// 钱包：充值余额兑换积分与在线直购积分同价同性质；充值档位附赠的积分与会员套餐赠送同性质
	{Category: "wallet_exchange", Label: "余额兑换积分", Rule: CategoryCounted},
	{Category: "wallet_recharge_bonus", Label: "充值赠送积分", Rule: CategoryCounted},
	// 抽奖：抽中的积分计入；积分抽奖的消耗是扣减，天然不进周期榜
	{Category: "lottery", Label: "抽奖", Rule: CategoryCounted},
	// 优惠活动：付款满额返的积分与充值赠送同性质；退款时的收回是扣减（docs/coupons.md）
	{Category: "coupon_rebate", Label: "下单返积分", Rule: CategoryCounted},
	{Category: "coupon_rebate_refund", Label: "退款收回返积分（扣减）", Rule: CategoryExcluded},
	{Category: CategoryAdminAdjust, Label: "管理员调整", Rule: CategoryAdmin},
	{Category: "points_store", Label: "积分兑换（扣减）", Rule: CategoryExcluded},
	{Category: "points_store_refund", Label: "积分兑换取消退回", Rule: CategoryExcluded},
	{Category: "refund", Label: "订单退款回收（扣减）", Rule: CategoryExcluded},
	{Category: "wallet_recharge_bonus_refund", Label: "充值退款回收赠送积分（扣减）", Rule: CategoryExcluded},
	{Category: IntegralCategory, Label: "排行榜奖励", Rule: CategoryExcluded},
	// AI 中转网关：调用扣积分是扣减；管理员退款是把扣掉的还回去，不是「获得」
	{Category: "ai_gateway", Label: "AI 调用（扣减）", Rule: CategoryExcluded},
	{Category: "ai_gateway_refund", Label: "AI 调用退款", Rule: CategoryExcluded},
	// 会员权益折算：已拥有的权益与开通永久时被覆盖的存量折成的积分，是补偿而不是挣来的；取消永久时的收回是扣减
	{Category: "vip_convert", Label: "会员权益折算", Rule: CategoryExcluded},
	// 物品发放：运营按规则赠送，不是挣来的
	{Category: "item_grant", Label: "物品发放", Rule: CategoryExcluded},
	// 有效反馈奖励：管理员按反馈发放，可撤回；发放与撤回都不计入（docs/feedback-rewards.md）
	{Category: "feedback_reward", Label: "反馈奖励", Rule: CategoryExcluded},
	{Category: "feedback_reward_revoke", Label: "反馈奖励撤回（扣减）", Rule: CategoryExcluded},
}

// IntegralCategories 全部已登记的分类与处理方式。
func IntegralCategories() []CategoryRule { return slices.Clone(integralCategories) }

// FindCategory 按分类取处理方式；未登记的分类按计入处理（不能因为漏登记就让人的积分凭空不算）。
func FindCategory(category string) (CategoryRule, bool) {
	for _, rule := range integralCategories {
		if rule.Category == category {
			return rule, true
		}
	}
	return CategoryRule{Category: category, Rule: CategoryCounted}, false
}

// ExcludedCategories 周期榜一律不计入的分类（SQL 的 NOT (category = ANY(...)) 用它）。
func ExcludedCategories() []string {
	out := []string{}
	for _, rule := range integralCategories {
		if rule.Rule == CategoryExcluded {
			out = append(out, rule.Category)
		}
	}
	return out
}

// Counts 一笔流水是否计入周期榜（与仓储层 SQL 同一口径，供单测钉住）。
// adminOperated：source_type = admin_manual 且有操作管理员。
func Counts(category string, amount int64, completed bool, adminOperated bool, countAdminAdjust bool) bool {
	if amount <= 0 || !completed {
		return false
	}
	rule, _ := FindCategory(category)
	switch rule.Rule {
	case CategoryExcluded:
		return false
	case CategoryAdmin:
		return countAdminAdjust || !adminOperated
	default:
		return true
	}
}
