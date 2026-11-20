package invite

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/shopspring/decimal"

	"aegis/internal/domain/pointstore"
	vipdomain "aegis/internal/domain/vip"
)

// RewardPoints 积分：邀请独有的一档（积分商城花积分，不能再发积分）。
const RewardPoints = "points"

// RewardSpec 一档权益的自述，形状与积分商城相同；控制台的奖励表单由它驱动。
type RewardSpec = pointstore.RewardSpec

// rewardTypes 邀请能发的权益，顺序即控制台的展示顺序，也是发放顺序。
// 不含「人工发放」：邀请奖励在填写的同一个事务里就要到账，等人处理的东西不该混进来。
var rewardTypes = []string{
	RewardPoints,
	pointstore.RewardVipPlan,
	pointstore.RewardVipDays,
	pointstore.RewardFeature,
	pointstore.RewardExperience,
	pointstore.RewardBalance,
	pointstore.RewardLotteryDraws,
	pointstore.RewardNumberChange,
	pointstore.RewardNumberTicket,
	pointstore.RewardNumberDraw,
	pointstore.RewardCosmetic,
}

// maxBalanceReward 一项奖励能发的余额上限（防手滑）。
var maxBalanceReward = decimal.NewFromInt(100_000)

var pointsSpec = RewardSpec{
	Type:  RewardPoints,
	Label: "积分",
	Hint:  "计入积分余额，流水分类为邀请",
	Value: pointstore.ValueAmount,
	Unit:  "点",
	Min:   1,
	Max:   100_000_000,
}

// RewardCatalog 邀请可配置的全部权益。
func RewardCatalog() []RewardSpec {
	out := make([]RewardSpec, 0, len(rewardTypes))
	for _, rewardType := range rewardTypes {
		if spec, ok := FindRewardSpec(rewardType); ok {
			out = append(out, spec)
		}
	}
	return out
}

// FindRewardSpec 按类型取权益自述。
func FindRewardSpec(rewardType string) (RewardSpec, bool) {
	if rewardType == RewardPoints {
		return pointsSpec, true
	}
	if !slices.Contains(rewardTypes, rewardType) {
		return RewardSpec{}, false
	}
	spec, ok := pointstore.FindRewardSpec(rewardType)
	if !ok {
		return RewardSpec{}, false
	}
	switch rewardType {
	case pointstore.RewardVipPlan:
		spec.Hint = "按套餐配置发放时长与功能；不能选择试用套餐"
	case pointstore.RewardFeature:
		spec.Hint = "单独解锁一个功能，不成为会员；天数为 0 即永久，同一功能多次获得时顺延"
	}
	return spec, true
}

// RewardTypes 全部可配置的权益类型。
func RewardTypes() []string { return slices.Clone(rewardTypes) }

// ValidateRewards 校验一组奖励（只校验形状，引用的套餐与功能是否存在由服务层查）。
// 空列表合法：一方不发奖励是常见配置（只奖励邀请人、或只奖励新人）。
func ValidateRewards(label string, rewards []Reward) error {
	if len(rewards) > MaxRewardsPerList {
		return fmt.Errorf("%s最多配置 %d 项", label, MaxRewardsPerList)
	}
	seenTypes := make([]string, 0, len(rewards))
	seenTags := make([]string, 0, len(rewards))
	for _, reward := range rewards {
		spec, ok := FindRewardSpec(reward.Type)
		if !ok {
			return fmt.Errorf("%s里有未登记的权益类型：%s", label, reward.Type)
		}
		if !spec.Repeatable && slices.Contains(seenTypes, reward.Type) {
			return fmt.Errorf("%s里「%s」重复配置了两次", label, spec.Label)
		}
		seenTypes = append(seenTypes, reward.Type)
		switch spec.Value {
		case pointstore.ValueAmount:
			if reward.Amount < spec.Min || reward.Amount > spec.Max {
				return fmt.Errorf("%s里「%s」需要在 %d–%d %s之间", label, spec.Label, spec.Min, spec.Max, spec.Unit)
			}
		case pointstore.ValueMoney:
			if reward.Money.LessThanOrEqual(decimal.Zero) {
				return fmt.Errorf("%s里「%s」必须大于 0", label, spec.Label)
			}
			if reward.Money.GreaterThan(maxBalanceReward) {
				return fmt.Errorf("%s里「%s」不能超过 %s", label, spec.Label, maxBalanceReward.String())
			}
			if !reward.Money.Equal(reward.Money.Round(2)) {
				return fmt.Errorf("%s里「%s」最多两位小数", label, spec.Label)
			}
		case pointstore.ValueRef:
			if reward.RefID <= 0 {
				return fmt.Errorf("%s里「%s」没有选择具体套餐", label, spec.Label)
			}
		case pointstore.ValueFeature:
			tag := normalizeTag(reward.Tag)
			if tag == "" {
				return fmt.Errorf("%s里「%s」没有选择功能", label, spec.Label)
			}
			if slices.Contains(seenTags, tag) {
				return fmt.Errorf("%s里功能 %s 重复配置了两次", label, tag)
			}
			seenTags = append(seenTags, tag)
			if reward.Amount < 0 || reward.Amount > pointstore.MaxFeatureDays {
				return fmt.Errorf("%s里「%s」的天数需要在 0–%d 之间（0 为永久）", label, spec.Label, pointstore.MaxFeatureDays)
			}
		case pointstore.ValueNumberTicket:
			if reward.RefID < 1 || reward.RefID > 4 {
				return fmt.Errorf("%s里「%s」没有选择等级（1–4）", label, spec.Label)
			}
			if reward.Amount < spec.Min || reward.Amount > spec.Max {
				return fmt.Errorf("%s里「%s」需要在 %d–%d %s之间", label, spec.Label, spec.Min, spec.Max, spec.Unit)
			}
		case pointstore.ValueCosmetic:
			if reward.RefID <= 0 {
				return fmt.Errorf("%s里「%s」没有选择具体装扮", label, spec.Label)
			}
			if reward.Amount < 0 || reward.Amount > pointstore.MaxFeatureDays {
				return fmt.Errorf("%s里「%s」的天数需要在 0–%d 之间（0 为永久）", label, spec.Label, pointstore.MaxFeatureDays)
			}
		}
	}
	return nil
}

// NormalizeRewards 按目录顺序重排，并抹掉与取值形态无关的字段。nil 归一为空列表。
func NormalizeRewards(rewards []Reward) []Reward {
	out := make([]Reward, 0, len(rewards))
	for _, rewardType := range rewardTypes {
		spec, _ := FindRewardSpec(rewardType)
		for _, reward := range rewards {
			if reward.Type != rewardType {
				continue
			}
			clean := Reward{Type: rewardType}
			switch spec.Value {
			case pointstore.ValueAmount:
				clean.Amount = reward.Amount
			case pointstore.ValueMoney:
				clean.Money = reward.Money.Round(2)
			case pointstore.ValueRef:
				clean.RefID = reward.RefID
			case pointstore.ValueFeature:
				clean.Tag = normalizeTag(reward.Tag)
				clean.Amount = reward.Amount
			case pointstore.ValueNumberTicket:
				clean.Amount = reward.Amount
				clean.RefID = reward.RefID
			case pointstore.ValueCosmetic:
				clean.Amount = reward.Amount
				clean.RefID = reward.RefID
			}
			out = append(out, clean)
			if !spec.Repeatable {
				break
			}
		}
	}
	// 未登记的类型原样留在末尾，交给 ValidateRewards 报出来：悄悄丢掉会让运营以为配上了
	for _, reward := range rewards {
		if !slices.Contains(rewardTypes, reward.Type) {
			out = append(out, reward)
		}
	}
	return out
}

// DescribeReward 一句话说清这项权益，供用户端与发放结果共用。
func DescribeReward(reward Reward, plans map[int64]pointstore.PlanRef, features map[string]pointstore.FeatureRef) ClientReward {
	if reward.Type == RewardPoints {
		return ClientReward{Type: RewardPoints, Label: pointsSpec.Label,
			Detail: "积分 " + strconv.FormatInt(reward.Amount, 10) + " 点"}
	}
	return pointstore.DescribeReward(reward, plans, features)
}

// DescribeRewards 一组权益的用户端描述。
func DescribeRewards(rewards []Reward, plans map[int64]pointstore.PlanRef, features map[string]pointstore.FeatureRef) []ClientReward {
	out := make([]ClientReward, 0, len(rewards))
	for _, reward := range NormalizeRewards(rewards) {
		out = append(out, DescribeReward(reward, plans, features))
	}
	return out
}

// RewardsResolvable 引用的套餐与功能都还在、可用（套餐被删、功能停用之后发不出去）。
func RewardsResolvable(rewards []Reward, plans map[int64]pointstore.PlanRef, features map[string]pointstore.FeatureRef) bool {
	for _, reward := range rewards {
		switch reward.Type {
		case pointstore.RewardVipPlan:
			plan, ok := plans[reward.RefID]
			if !ok || plan.Trial {
				return false
			}
		case pointstore.RewardFeature:
			feature, ok := features[reward.Tag]
			if !ok || !feature.IsActive {
				return false
			}
		}
	}
	return true
}

func normalizeTag(tag string) string {
	tags := vipdomain.NormalizeFeatureTags([]string{tag})
	if len(tags) == 0 {
		return ""
	}
	return tags[0]
}
