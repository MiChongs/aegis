package pointstore

import (
	"slices"
	"strconv"

	vipdomain "aegis/internal/domain/vip"
)

// reasonMessages 不能兑换的原因说给用户听的话。
//
// 列表上的「为什么灰着」与兑换失败的报错必须是同一句话：
// 一处说「本周已兑完」、另一处说「超出兑换次数」，用户会以为是两回事。
var reasonMessages = map[string]string{
	ReasonOK:                 "可以兑换",
	ReasonInactive:           "商品已下架",
	ReasonNotStarted:         "尚未开始兑换",
	ReasonEnded:              "兑换已结束",
	ReasonClosedNow:          "当前不在兑换时段",
	ReasonSoldOut:            "已兑完",
	ReasonQuotaReached:       "名额已满",
	ReasonLimitReached:       "已达到兑换次数上限",
	ReasonLevelRequired:      "等级不足",
	ReasonVipOnly:            "仅限会员兑换",
	ReasonNonVipOnly:         "仅限非会员兑换",
	ReasonLifetimeOnly:       "仅限永久会员兑换",
	ReasonOwned:              "已拥有该权益，无需兑换",
	ReasonInsufficientPoints: "积分不足",
	ReasonRewardUnavailable:  "商品配置的权益已失效",
}

// ReasonMessage 原因对应的说明，未知原因返回空串。
func ReasonMessage(reason string) string { return reasonMessages[reason] }

// Evaluate 这件商品此刻能不能被这个人兑换。纯函数。
//
// 判据顺序即优先级，从「商品本身」到「这个人」：下架的商品先说下架，
// 而不是先说「你积分不够」—— 后者会让用户去攒积分，攒够了才发现兑不了。
// 「积分不足」排在最后：它是唯一一个用户自己能马上解决的原因。
//
// 库存按补货周期换算后再判（item.CurrentStock），调用方不必先换算。
// 会过去的原因（不在时段、已兑完待补货、名额已满、本周期次数用完）在说明里带上何时恢复。
func Evaluate(item Item, facts UserFacts, usage Usage, clock Clock) Availability {
	item.Stock, _ = item.CurrentStock(clock.Windows)
	reason := evaluateReason(item, facts, usage, clock)
	availability := Availability{Redeemable: reason == ReasonOK, Reason: reason, Message: ReasonMessage(reason)}
	switch reason {
	case ReasonLevelRequired:
		availability.Message = "需要达到 Lv." + strconv.Itoa(item.MinLevel)
	case ReasonLimitReached:
		if item.LimitCount > 0 && usage.In(item.LimitPeriod) >= int64(item.LimitCount) {
			availability.Message = limitMessage(item.LimitPeriod)
		} else {
			availability.Message = "已达到累计兑换次数上限"
		}
	case ReasonClosedNow:
		availability.Message = "每日 " + item.OpenHoursLabel() + " 开放兑换"
	case ReasonSoldOut:
		if item.Restocks() {
			availability.Message = "已兑完，" + restockHint(item.RestockPeriod)
		}
	case ReasonQuotaReached:
		availability.Message = quotaMessage(item.GlobalLimitPeriod)
	}
	return availability
}

func evaluateReason(item Item, facts UserFacts, usage Usage, clock Clock) string {
	now := clock.Now
	switch {
	case item.Status != StatusActive:
		return ReasonInactive
	case item.StartsAt != nil && now.Before(*item.StartsAt):
		return ReasonNotStarted
	case item.EndsAt != nil && !now.Before(*item.EndsAt):
		return ReasonEnded
	case !rewardsResolvable(item.Rewards, facts):
		return ReasonRewardUnavailable
	case !item.OpenAt(clock):
		return ReasonClosedNow
	case item.Stock != nil && *item.Stock <= 0:
		return ReasonSoldOut
	case item.GlobalLimited() && usage.Global.In(item.GlobalLimitPeriod) >= int64(item.GlobalLimitCount):
		return ReasonQuotaReached
	case item.LimitCount > 0 && usage.In(item.LimitPeriod) >= int64(item.LimitCount):
		return ReasonLimitReached
	case item.UserTotalLimit > 0 && usage.Total >= int64(item.UserTotalLimit):
		return ReasonLimitReached
	case item.Audience == AudienceVip && !facts.IsVip:
		return ReasonVipOnly
	case item.Audience == AudienceNonVip && facts.IsVip:
		return ReasonNonVipOnly
	case item.Audience == AudienceLifetime && !facts.IsLifetime:
		return ReasonLifetimeOnly
	case item.MinLevel > 0 && facts.Level < item.MinLevel:
		return ReasonLevelRequired
	case Owned(item.Rewards, facts):
		return ReasonOwned
	case facts.Integral < item.Cost:
		return ReasonInsufficientPoints
	}
	return ReasonOK
}

// rewardsResolvable 商品引用的套餐与功能都还在：套餐被删、功能被删或停用之后，
// 兑换出来的东西要么发不出去、要么发了也用不上，不能让用户先花积分再发现。
// facts 里没有带目录（Plans / Features 为 nil）时不判，交给发放时的查询兜底。
func rewardsResolvable(rewards []Reward, facts UserFacts) bool {
	for _, reward := range rewards {
		switch reward.Type {
		case RewardVipPlan:
			if facts.Plans == nil {
				continue
			}
			plan, ok := facts.Plans[reward.RefID]
			if !ok || plan.Trial {
				return false
			}
		case RewardFeature:
			if facts.Features == nil {
				continue
			}
			feature, ok := facts.Features[reward.Tag]
			if !ok || !feature.IsActive {
				return false
			}
		case RewardCosmetic:
			if facts.Cosmetics == nil {
				continue
			}
			if live, ok := facts.Cosmetics[reward.RefID]; !ok || !live {
				return false
			}
		}
	}
	return true
}

// Owned 这件商品里是否有一项权益此人已经永久拥有：再兑一次拿不到任何新东西。
//
// 拦的是纯花积分的情形 —— 永久会员再兑会员天数或已包含的套餐、已永久解锁的功能再兑一次限时的。
// 一件商品里只要有一项是这样就拦：积分花出去收不回来，宁可让运营把商品拆开。
func Owned(rewards []Reward, facts UserFacts) bool {
	holdings := facts.Holdings
	for _, reward := range rewards {
		switch reward.Type {
		case RewardVipPlan:
			// 与购买的 40378 同一口径：永久会员已包含套餐的全部功能即算拥有
			plan, ok := facts.Plans[reward.RefID]
			if ok && holdings.CoversPlan(vipdomain.Plan{Kind: vipdomain.KindPaid, Lifetime: plan.Lifetime, Features: plan.Features}) {
				return true
			}
		case RewardVipDays:
			// 不带功能的会员天数对永久会员没有任何作用
			if holdings.CoversMembership(nil) {
				return true
			}
		case RewardFeature:
			if _, ok := holdings.CoversFeature(reward.Tag); ok {
				return true
			}
		case RewardCosmetic:
			// 已永久持有这件装扮：再兑一段限时的或永久的都拿不到新东西
			if slices.Contains(facts.PermanentCosmetics, reward.RefID) {
				return true
			}
		}
	}
	return false
}

// restockHint 「明日补货」这类恢复提示。
func restockHint(period string) string {
	switch period {
	case RestockDay:
		return "明日补货"
	case RestockWeek:
		return "下周补货"
	case RestockMonth:
		return "下月补货"
	}
	return "等待补货"
}

func quotaMessage(period string) string {
	switch period {
	case PeriodDay:
		return "今日名额已满"
	case PeriodWeek:
		return "本周名额已满"
	case PeriodMonth:
		return "本月名额已满"
	default:
		return reasonMessages[ReasonQuotaReached]
	}
}

func limitMessage(period string) string {
	switch period {
	case PeriodDay:
		return "今日兑换次数已用完"
	case PeriodWeek:
		return "本周兑换次数已用完"
	case PeriodMonth:
		return "本月兑换次数已用完"
	default:
		return reasonMessages[ReasonLimitReached]
	}
}
