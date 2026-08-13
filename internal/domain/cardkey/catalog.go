package cardkey

import (
	"fmt"
	"slices"

	"github.com/shopspring/decimal"
)

// 权益类型。
const (
	RewardVipPlan      = "vip_plan"
	RewardVipDays      = "vip_days"
	RewardIntegral     = "integral"
	RewardExperience   = "experience"
	RewardBalance      = "balance"
	RewardLotteryDraws = "lottery_draws"
	RewardDeviceSlots  = "device_slots"
)

// 权益的取值形态：决定 Reward 上的哪个字段承载数量。
const (
	// ValueAmount 整数量，走 Reward.Amount。
	ValueAmount = "amount"
	// ValueMoney 金额，走 Reward.Money。
	ValueMoney = "money"
	// ValueRef 引用既有实体的 ID，走 Reward.RefID。
	ValueRef = "ref"
)

// MaxRewardsPerCard 一张卡最多挂几档权益。
//
// 上限不是性能考虑，是可解释性：一张卡发七样东西时，用户在客户端上
// 看到的「兑换成功」后面要跟七行结果，而其中任何一样没到账都很难被发现。
const MaxRewardsPerCard = 6

// RewardSpec 一档权益的自述。
//
// 这张表是**单一事实源**：服务端校验、控制台的权益编辑表单、以及
// 「这张卡会发什么」的展示文案全部读它。新增一档权益只需在这里加一行 +
// 在发放处加一个 case，控制台零改动即自动出现 —— 与远程函数的能力目录、
// 支付渠道的 Describe()、风控条件目录是同一套做法。
//
// TestRewardCatalogHasGrantBranch 双向钉死「目录 ↔ 发放分支」：
// 目录多一条 → 配得上却发不出来；发放多一条 → 控制台配不出来。
type RewardSpec struct {
	Type  string `json:"type"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
	// Value 取值形态，见 ValueAmount / ValueMoney / ValueRef。
	Value string `json:"value"`
	Unit  string `json:"unit,omitempty"`
	// Min / Max 仅对 ValueAmount 有意义。
	Min int64 `json:"min,omitempty"`
	Max int64 `json:"max,omitempty"`
	// NeedsLoginCard 领取人名下必须有一张仍在授权期内的授权卡，否则拒发。
	//
	// 只有「加设备位」是这样：设备位是挂在授权卡上的，没有卡就没有地方加。
	// 静默成功比报错更糟 —— 用户会以为买到了，而客户端上什么都没变。
	NeedsLoginCard bool `json:"needsLoginCard,omitempty"`
}

// rewardSpecs 顺序即控制台的展示顺序。
var rewardSpecs = []RewardSpec{
	{
		Type:  RewardVipPlan,
		Label: "会员套餐",
		Hint:  "按套餐配置发放时长与功能标识，与购买同一套账本；套餐改名不影响已发出的权益",
		Value: ValueRef,
	},
	{
		Type:  RewardVipDays,
		Label: "会员天数",
		Hint:  "不挂套餐，直接把会员到期时间往后顺延；不携带任何功能标识",
		Value: ValueAmount,
		Unit:  "天",
		Min:   1,
		Max:   3650,
	},
	{
		Type:  RewardIntegral,
		Label: "积分",
		Hint:  "进积分账本，与签到、抽奖消耗共用同一套余额",
		Value: ValueAmount,
		Unit:  "分",
		Min:   1,
		Max:   100_000_000,
	},
	{
		Type:  RewardExperience,
		Label: "经验值",
		Hint:  "进经验账本并同步等级；等级只升不降，发放后不会因为退卡回落",
		Value: ValueAmount,
		Unit:  "点",
		Min:   1,
		Max:   100_000_000,
	},
	{
		Type:  RewardBalance,
		Label: "钱包余额",
		Hint:  "计入余额但不计入累计充值：卡密不是充值，把它算进充值会让充值报表虚高",
		Value: ValueMoney,
	},
	{
		Type:  RewardLotteryDraws,
		Label: "抽奖次数",
		Hint:  "应用级次数，任意活动可用；消耗赠送次数时不扣积分、也不计入活动的日限与总限",
		Value: ValueAmount,
		Unit:  "次",
		Min:   1,
		Max:   10_000,
	},
	{
		Type:           RewardDeviceSlots,
		Label:          "设备位",
		Hint:           "给领取人名下授权期最长的那张授权卡加可绑定设备数；没有授权卡时拒发",
		Value:          ValueAmount,
		Unit:           "台",
		Min:            1,
		Max:            63,
		NeedsLoginCard: true,
	},
}

// maxBalanceReward 单张卡能发的余额上限。
//
// 不是业务规则，是防手滑：多打两个零的卡密一旦生成并发出去就收不回来了。
var maxBalanceReward = decimal.NewFromInt(1_000_000)

// RewardCatalog 返回全部权益档位。控制台的权益编辑表单由它驱动。
func RewardCatalog() []RewardSpec {
	out := make([]RewardSpec, len(rewardSpecs))
	copy(out, rewardSpecs)
	return out
}

// FindRewardSpec 按类型取权益自述；未登记时返回 false。
func FindRewardSpec(rewardType string) (RewardSpec, bool) {
	for _, spec := range rewardSpecs {
		if spec.Type == rewardType {
			return spec, true
		}
	}
	return RewardSpec{}, false
}

// RewardTypes 全部已登记的权益类型。
func RewardTypes() []string {
	out := make([]string, 0, len(rewardSpecs))
	for _, spec := range rewardSpecs {
		out = append(out, spec.Type)
	}
	return out
}

// ValidateRewards 校验一张卡的权益清单。
//
// 三类问题在这里被挡住，每一类都有明确的错误文案：未登记的类型（拼错）、
// 超出范围的数量（手滑）、以及同一档权益出现两次（「到底是加起来还是取后者」
// 没有答案，而这个答案不该由发放代码的实现细节决定）。
func ValidateRewards(rewards []Reward) error {
	if len(rewards) == 0 {
		return fmt.Errorf("至少要配置一项权益")
	}
	if len(rewards) > MaxRewardsPerCard {
		return fmt.Errorf("一张卡最多配置 %d 项权益", MaxRewardsPerCard)
	}

	seen := make([]string, 0, len(rewards))
	for _, reward := range rewards {
		spec, ok := FindRewardSpec(reward.Type)
		if !ok {
			return fmt.Errorf("未登记的权益类型：%s", reward.Type)
		}
		if slices.Contains(seen, reward.Type) {
			return fmt.Errorf("「%s」重复配置了两次", spec.Label)
		}
		seen = append(seen, reward.Type)

		switch spec.Value {
		case ValueAmount:
			if reward.Amount < spec.Min || reward.Amount > spec.Max {
				return fmt.Errorf("「%s」需要在 %d–%d %s之间", spec.Label, spec.Min, spec.Max, spec.Unit)
			}
		case ValueMoney:
			if reward.Money.LessThanOrEqual(decimal.Zero) {
				return fmt.Errorf("「%s」必须大于 0", spec.Label)
			}
			if reward.Money.GreaterThan(maxBalanceReward) {
				return fmt.Errorf("「%s」单张不能超过 %s", spec.Label, maxBalanceReward.String())
			}
		case ValueRef:
			if reward.RefID <= 0 {
				return fmt.Errorf("「%s」没有选择具体对象", spec.Label)
			}
		}
	}
	return nil
}

// NormalizeRewards 按目录顺序重排权益清单，并抹掉与取值形态无关的字段。
//
// 重排是为了让「同一批卡的权益在任何地方都按同一顺序展示」；抹字段是为了让
// 落库的 JSON 只留下真正生效的那一个 —— 否则一条 {"type":"integral","refId":7}
// 会让读日志的人以为 refId 起了作用。
func NormalizeRewards(rewards []Reward) []Reward {
	out := make([]Reward, 0, len(rewards))
	for _, spec := range rewardSpecs {
		for _, reward := range rewards {
			if reward.Type != spec.Type {
				continue
			}
			clean := Reward{Type: spec.Type}
			switch spec.Value {
			case ValueAmount:
				clean.Amount = reward.Amount
			case ValueMoney:
				clean.Money = reward.Money
			case ValueRef:
				clean.RefID = reward.RefID
			}
			out = append(out, clean)
			break
		}
	}
	return out
}

// DescribeReward 一句话说清这项权益是什么，用于核销结果与控制台摘要。
func DescribeReward(reward Reward) string {
	spec, ok := FindRewardSpec(reward.Type)
	if !ok {
		return reward.Type
	}
	switch spec.Value {
	case ValueAmount:
		return fmt.Sprintf("%s %d %s", spec.Label, reward.Amount, spec.Unit)
	case ValueMoney:
		return fmt.Sprintf("%s %s", spec.Label, reward.Money.StringFixed(2))
	default:
		return spec.Label
	}
}

// ValidKind 卡的形态是否合法。
func ValidKind(kind string) bool {
	return kind == KindLogin || kind == KindRedeem
}

// ValidValidityMode 有效期模式是否合法。
func ValidValidityMode(mode string) bool {
	return mode == ValidityPermanent || mode == ValidityFixedUntil || mode == ValidityFromFirstUse
}
