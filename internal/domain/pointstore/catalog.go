package pointstore

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/shopspring/decimal"

	"aegis/internal/domain/usernumber"
	vipdomain "aegis/internal/domain/vip"
)

// 权益类型。前六档与卡密同名同义（发放走同一套账本），feature 与 manual 是积分商城独有的。
const (
	RewardVipPlan      = "vip_plan"
	RewardVipDays      = "vip_days"
	RewardFeature      = "feature"
	RewardExperience   = "experience"
	RewardBalance      = "balance"
	RewardLotteryDraws = "lottery_draws"
	RewardManual       = "manual"
	// 用户号码的权益（docs/user-number.md），与卡密、物品发放同名
	RewardNumberChange = "number_change"
	RewardNumberTicket = "number_ticket"
	RewardNumberDraw   = "number_draw"
	// RewardCosmetic 个性装扮（docs/cosmetics.md），与卡密、物品发放同名同形
	RewardCosmetic = "cosmetic"
)

// 权益的取值形态：决定 Reward 上的哪个字段承载内容。
const (
	ValueAmount  = "amount"  // Reward.Amount
	ValueMoney   = "money"   // Reward.Money
	ValueRef     = "ref"     // Reward.RefID（会员套餐）
	ValueFeature = "feature" // Reward.Tag + Reward.Amount（天数，0 为永久）
	ValueText    = "text"    // Reward.Text
	// ValueNumberTicket 靓号券：Reward.Amount 张数 + Reward.RefID 等级（1 精品、2 稀有、3 传说、4 至尊）
	ValueNumberTicket = "number_ticket"
	// ValueCosmetic 个性装扮：Reward.RefID 装扮编号 + Reward.Amount 天数（0 为永久）
	ValueCosmetic = "cosmetic"
)

// MaxRewardsPerItem 一件商品最多挂几项权益。
const MaxRewardsPerItem = 6

// MaxManualTextRunes 人工发放的说明最长多少字。
const MaxManualTextRunes = 64

// MaxFeatureDays 功能解锁的天数上限（0 为永久）。
const MaxFeatureDays = 3650

// RewardSpec 一档权益的自述。服务端校验、控制台的权益表单与展示文案都读它。
//
// TestRewardCatalogHasGrantBranch 双向钉死「目录 ↔ 发放分支」：
// 目录多一条 → 配得上却发不出来；发放多一条 → 控制台配不出来。
type RewardSpec struct {
	Type  string `json:"type"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
	Value string `json:"value"`
	Unit  string `json:"unit,omitempty"`
	Min   int64  `json:"min,omitempty"`
	Max   int64  `json:"max,omitempty"`
	// Repeatable 同一件商品里可以出现多次（只有功能解锁：一次解锁几个不同的功能）
	Repeatable bool `json:"repeatable,omitempty"`
	// Exclusive 只能单独成为一件商品（人工发放：整笔兑换要等人处理，不能与自动到账的混在一起）
	Exclusive bool `json:"exclusive,omitempty"`
}

// rewardSpecs 顺序即控制台的展示顺序，也是发放与展示的顺序。
var rewardSpecs = []RewardSpec{
	{
		Type:  RewardVipPlan,
		Label: "会员套餐",
		Hint:  "按套餐配置发放时长与功能，与购买同一套账本；选永久套餐即发永久会员",
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
		Type:       RewardFeature,
		Label:      "功能解锁",
		Hint:       "单独解锁一个功能，不成为会员；天数为 0 即永久，同一功能多次兑换时顺延",
		Value:      ValueFeature,
		Unit:       "天",
		Min:        0,
		Max:        MaxFeatureDays,
		Repeatable: true,
	},
	{
		Type:  RewardExperience,
		Label: "经验值",
		Hint:  "进经验账本并同步等级",
		Value: ValueAmount,
		Unit:  "点",
		Min:   1,
		Max:   100_000_000,
	},
	{
		Type:  RewardBalance,
		Label: "钱包余额",
		Hint:  "计入余额但不计入累计充值",
		Value: ValueMoney,
	},
	{
		Type:  RewardLotteryDraws,
		Label: "抽奖次数",
		Hint:  "应用级次数，任意活动可用；不扣积分、不计入活动的日限与总限",
		Value: ValueAmount,
		Unit:  "次",
		Min:   1,
		Max:   10_000,
	},
	{
		Type:  RewardNumberChange,
		Label: "改号卡",
		Hint:  "自选一个普通号码；与卡密、抽奖等渠道发的是同一个账户",
		Value: ValueAmount,
		Unit:  "张",
		Min:   1,
		Max:   1000,
	},
	{
		Type:       RewardNumberTicket,
		Label:      "靓号券",
		Hint:       "按等级自选靓号（1 精品、2 稀有、3 传说、4 至尊），能换不高于其等级的号码",
		Value:      ValueNumberTicket,
		Unit:       "张",
		Min:        1,
		Max:        1000,
		Repeatable: true,
	},
	{
		Type:  RewardNumberDraw,
		Label: "摇号次数",
		Hint:  "摇一次给若干候选号，任选其一换上；概率受今日号运影响",
		Value: ValueAmount,
		Unit:  "次",
		Min:   1,
		Max:   1000,
	},
	{
		Type:       RewardCosmetic,
		Label:      "个性装扮",
		Hint:       "头像框、资料卡背景、昵称样式或勋章；天数为 0 即永久，同一件多次获得时顺延，已永久持有的人不能再兑",
		Value:      ValueCosmetic,
		Unit:       "天",
		Min:        0,
		Max:        MaxFeatureDays,
		Repeatable: true,
	},
	{
		Type:      RewardManual,
		Label:     "人工发放",
		Hint:      "兑换后进入待发放，由管理员在兑换记录里标记已发放或取消（取消即退回积分）",
		Value:     ValueText,
		Exclusive: true,
	},
}

// maxBalanceReward 单件商品能发的余额上限（防手滑）。
var maxBalanceReward = decimal.NewFromInt(1_000_000)

// RewardCatalog 全部权益档位。
func RewardCatalog() []RewardSpec {
	out := make([]RewardSpec, len(rewardSpecs))
	copy(out, rewardSpecs)
	return out
}

// FindRewardSpec 按类型取权益自述。
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

// LimitPeriods 限兑周期，顺序即控制台的展示顺序。
func LimitPeriods() []string {
	return []string{PeriodTotal, PeriodDay, PeriodWeek, PeriodMonth}
}

// Audiences 兑换对象，顺序即控制台的展示顺序。
func Audiences() []string {
	return []string{AudienceAll, AudienceVip, AudienceNonVip, AudienceLifetime}
}

// ValidateRewards 校验一件商品的权益清单（只校验形状，引用的套餐与功能是否存在由服务层查）。
func ValidateRewards(rewards []Reward) error {
	if len(rewards) == 0 {
		return fmt.Errorf("至少要配置一项权益")
	}
	if len(rewards) > MaxRewardsPerItem {
		return fmt.Errorf("一件商品最多配置 %d 项权益", MaxRewardsPerItem)
	}
	seenTypes := make([]string, 0, len(rewards))
	seenTags := make([]string, 0, len(rewards))
	seenCosmetics := make([]int64, 0, 2)
	for _, reward := range rewards {
		spec, ok := FindRewardSpec(reward.Type)
		if !ok {
			return fmt.Errorf("未登记的权益类型：%s", reward.Type)
		}
		if spec.Exclusive && len(rewards) > 1 {
			return fmt.Errorf("「%s」只能单独配置", spec.Label)
		}
		if !spec.Repeatable && slices.Contains(seenTypes, reward.Type) {
			return fmt.Errorf("「%s」重复配置了两次", spec.Label)
		}
		seenTypes = append(seenTypes, reward.Type)

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
				return fmt.Errorf("「%s」不能超过 %s", spec.Label, maxBalanceReward.String())
			}
			if reward.Money.Exponent() < -2 && !reward.Money.Equal(reward.Money.Round(2)) {
				return fmt.Errorf("「%s」最多两位小数", spec.Label)
			}
		case ValueRef:
			if reward.RefID <= 0 {
				return fmt.Errorf("「%s」没有选择具体套餐", spec.Label)
			}
		case ValueFeature:
			tag := normalizeTag(reward.Tag)
			if tag == "" {
				return fmt.Errorf("「%s」没有选择功能", spec.Label)
			}
			if slices.Contains(seenTags, tag) {
				return fmt.Errorf("功能 %s 重复配置了两次", tag)
			}
			seenTags = append(seenTags, tag)
			if reward.Amount < 0 || reward.Amount > MaxFeatureDays {
				return fmt.Errorf("「%s」的天数需要在 0–%d 之间（0 为永久）", spec.Label, MaxFeatureDays)
			}
		case ValueNumberTicket:
			if reward.RefID < 1 || reward.RefID > 4 {
				return fmt.Errorf("「%s」没有选择等级（1–4）", spec.Label)
			}
			if reward.Amount < spec.Min || reward.Amount > spec.Max {
				return fmt.Errorf("「%s」需要在 %d–%d %s之间", spec.Label, spec.Min, spec.Max, spec.Unit)
			}
		case ValueCosmetic:
			if reward.RefID <= 0 {
				return fmt.Errorf("「%s」没有选择具体装扮", spec.Label)
			}
			if slices.Contains(seenCosmetics, reward.RefID) {
				return fmt.Errorf("同一件装扮重复配置了两次")
			}
			seenCosmetics = append(seenCosmetics, reward.RefID)
			if reward.Amount < 0 || reward.Amount > MaxFeatureDays {
				return fmt.Errorf("「%s」的天数需要在 0–%d 之间（0 为永久）", spec.Label, MaxFeatureDays)
			}
		case ValueText:
			text := strings.TrimSpace(reward.Text)
			if text == "" {
				return fmt.Errorf("「%s」需要写明发放什么", spec.Label)
			}
			if utf8.RuneCountInString(text) > MaxManualTextRunes {
				return fmt.Errorf("「%s」的说明不能超过 %d 字", spec.Label, MaxManualTextRunes)
			}
		}
	}
	return nil
}

// NormalizeRewards 按目录顺序重排权益，并抹掉与取值形态无关的字段。
// 同一类型出现多次（功能解锁）时保持原有相对顺序。
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
				clean.Money = reward.Money.Round(2)
			case ValueRef:
				clean.RefID = reward.RefID
			case ValueFeature:
				clean.Tag = normalizeTag(reward.Tag)
				clean.Amount = reward.Amount
			case ValueNumberTicket:
				clean.Amount = reward.Amount
				clean.RefID = reward.RefID
			case ValueCosmetic:
				clean.Amount = reward.Amount
				clean.RefID = reward.RefID
			case ValueText:
				clean.Text = strings.TrimSpace(reward.Text)
			}
			out = append(out, clean)
			if !spec.Repeatable {
				break
			}
		}
	}
	return out
}

// HasManual 商品是否含人工发放的权益（兑换后进入待发放）。
func HasManual(rewards []Reward) bool {
	for _, reward := range rewards {
		if reward.Type == RewardManual {
			return true
		}
	}
	return false
}

// DescribeReward 一句话说清这项权益（Detail 不依赖 Label 也能单独成立），供兑换结果与用户端共用。
// plans / features 用于把引用翻成名字，缺失时退回标识本身。
func DescribeReward(reward Reward, plans map[int64]PlanRef, features map[string]FeatureRef) ClientReward {
	spec, ok := FindRewardSpec(reward.Type)
	out := ClientReward{Type: reward.Type, Label: reward.Type}
	if !ok {
		return out
	}
	out.Label = spec.Label
	switch reward.Type {
	case RewardVipPlan:
		if plan, ok := plans[reward.RefID]; ok {
			out.Days = plan.DurationDays
			out.Permanent = plan.Lifetime
			out.Detail = plan.Name + " " + termLabel(plan.DurationDays, plan.Lifetime)
		} else {
			out.Detail = "套餐 #" + strconv.FormatInt(reward.RefID, 10)
		}
	case RewardVipDays:
		out.Days = int(reward.Amount)
		out.Detail = "会员 " + termLabel(int(reward.Amount), false)
	case RewardFeature:
		out.Tag = reward.Tag
		out.Days = int(reward.Amount)
		out.Permanent = reward.Amount == 0
		name := reward.Tag
		if feature, ok := features[reward.Tag]; ok && strings.TrimSpace(feature.Name) != "" {
			name = feature.Name
		}
		out.Detail = name + " " + termLabel(int(reward.Amount), reward.Amount == 0)
	case RewardBalance:
		out.Detail = "余额 " + reward.Money.StringFixed(2)
	case RewardNumberTicket:
		out.Detail = usernumber.RightName(usernumber.RightTicket, int(reward.RefID)) + " " + strconv.FormatInt(reward.Amount, 10) + " 张"
	case RewardCosmetic:
		// 名称不在这里翻：客户端按 refId 在装扮中心里取外观与名称，发放结果里由发放分支写上名称
		out.RefID = reward.RefID
		out.Days = int(reward.Amount)
		out.Permanent = reward.Amount == 0
		out.Detail = spec.Label + " " + termLabel(int(reward.Amount), reward.Amount == 0)
	case RewardManual:
		out.Detail = reward.Text
	default:
		out.Detail = detailNoun[reward.Type] + " " + strconv.FormatInt(reward.Amount, 10) + " " + spec.Unit
	}
	return out
}

// detailNoun 数量型权益在一句话摘要里的名词：「经验 500 点」「抽奖 3 次」。
// 摘要要能脱离标签单独成立 —— 客户端常把几项摘要连成一行，只写「500 点」读不出是什么。
var detailNoun = map[string]string{
	RewardExperience:   "经验",
	RewardLotteryDraws: "抽奖",
	RewardNumberChange: "改号卡",
	RewardNumberDraw:   "摇号",
}

// termLabel 时长的展示口径：永久 / 1 年 / 3 个月 / 7 天。
func termLabel(days int, permanent bool) string {
	switch {
	case permanent:
		return "永久"
	case days > 0 && days%365 == 0:
		return strconv.Itoa(days/365) + " 年"
	case days > 0 && days%30 == 0:
		return strconv.Itoa(days/30) + " 个月"
	default:
		return strconv.Itoa(days) + " 天"
	}
}

// ValidPeriod / ValidAudience / ValidStatus 枚举校验。
func ValidPeriod(period string) bool     { return slices.Contains(LimitPeriods(), period) }
func ValidAudience(audience string) bool { return slices.Contains(Audiences(), audience) }
func ValidStatus(status string) bool     { return status == StatusActive || status == StatusInactive }

// NormalizeItemInput 清洗商品输入：去空白、补默认值、按目录重排权益。
func NormalizeItemInput(input ItemInput) ItemInput {
	input.Title = strings.TrimSpace(input.Title)
	input.Subtitle = strings.TrimSpace(input.Subtitle)
	input.Description = strings.TrimSpace(input.Description)
	input.Category = strings.TrimSpace(input.Category)
	input.Badge = strings.TrimSpace(input.Badge)
	if input.LimitPeriod == "" {
		input.LimitPeriod = PeriodTotal
	}
	// 累计周期本身就是累计上限，另一个累计上限没有意义
	if input.LimitPeriod == PeriodTotal {
		input.UserTotalLimit = 0
	}
	if input.RestockPeriod == "" {
		input.RestockPeriod = RestockNone
	}
	if input.RestockPeriod == RestockNone {
		input.RestockQuantity = nil
		input.StockWindowStart = nil
	} else if input.Stock == nil && input.RestockQuantity != nil {
		// 开了补货却没填当前剩余：从满货开始
		quantity := *input.RestockQuantity
		input.Stock = &quantity
	}
	if input.GlobalLimitPeriod == "" {
		input.GlobalLimitPeriod = PeriodDay
	}
	// 开放时段要么都填、要么都不填
	if input.DailyOpenMinute == nil || input.DailyCloseMinute == nil {
		input.DailyOpenMinute, input.DailyCloseMinute = nil, nil
	}
	if input.Audience == "" {
		input.Audience = AudienceAll
	}
	if input.Delivery == "" {
		input.Delivery = DeliverySelf
	}
	if input.Status == "" {
		input.Status = StatusActive
	}
	if input.OriginalCost != nil && *input.OriginalCost <= 0 {
		input.OriginalCost = nil
	}
	input.Rewards = NormalizeRewards(input.Rewards)
	return input
}

// ValidateItemInput 校验清洗后的商品输入。
func ValidateItemInput(input ItemInput) error {
	switch {
	case input.Title == "":
		return fmt.Errorf("商品名称不能为空")
	case utf8.RuneCountInString(input.Title) > MaxTitleRunes:
		return fmt.Errorf("商品名称不能超过 %d 字", MaxTitleRunes)
	case utf8.RuneCountInString(input.Subtitle) > MaxSubtitleRunes:
		return fmt.Errorf("副标题不能超过 %d 字", MaxSubtitleRunes)
	case utf8.RuneCountInString(input.Description) > MaxDescriptionRunes:
		return fmt.Errorf("详情不能超过 %d 字", MaxDescriptionRunes)
	case utf8.RuneCountInString(input.Category) > MaxCategoryRunes:
		return fmt.Errorf("分类不能超过 %d 字", MaxCategoryRunes)
	case utf8.RuneCountInString(input.Badge) > MaxBadgeRunes:
		return fmt.Errorf("角标不能超过 %d 字", MaxBadgeRunes)
	case input.Cost <= 0 || input.Cost > MaxCost:
		return fmt.Errorf("兑换积分需要在 1–%d 之间", MaxCost)
	case input.OriginalCost != nil && (*input.OriginalCost <= input.Cost || *input.OriginalCost > MaxCost):
		return fmt.Errorf("划线价需要高于兑换积分")
	case input.Stock != nil && (*input.Stock < 0 || *input.Stock > MaxStock):
		return fmt.Errorf("库存需要在 0–%d 之间", MaxStock)
	case !ValidPeriod(input.LimitPeriod):
		return fmt.Errorf("未知的限兑周期：%s", input.LimitPeriod)
	case input.LimitCount < 0 || input.LimitCount > MaxLimitCount:
		return fmt.Errorf("限兑次数需要在 0–%d 之间", MaxLimitCount)
	case input.UserTotalLimit < 0 || input.UserTotalLimit > MaxLimitCount:
		return fmt.Errorf("每人累计上限需要在 0–%d 之间", MaxLimitCount)
	case input.UserTotalLimit > 0 && input.LimitCount > input.UserTotalLimit:
		return fmt.Errorf("每人累计上限不能少于每个周期的限兑次数")
	case !ValidRestockPeriod(input.RestockPeriod):
		return fmt.Errorf("未知的补货周期：%s", input.RestockPeriod)
	case input.RestockPeriod != RestockNone && input.RestockQuantity == nil:
		return fmt.Errorf("开启补货周期时需要填写每次补到的数量")
	case input.RestockQuantity != nil && (*input.RestockQuantity <= 0 || *input.RestockQuantity > MaxStock):
		return fmt.Errorf("补货数量需要在 1–%d 之间", MaxStock)
	case input.RestockQuantity != nil && input.Stock != nil && *input.Stock > *input.RestockQuantity:
		return fmt.Errorf("当前剩余不能多于每次补到的数量")
	case !ValidPeriod(input.GlobalLimitPeriod):
		return fmt.Errorf("未知的名额周期：%s", input.GlobalLimitPeriod)
	case input.GlobalLimitCount < 0 || input.GlobalLimitCount > MaxStock:
		return fmt.Errorf("全站名额需要在 0–%d 之间", MaxStock)
	case input.DailyOpenMinute != nil && (*input.DailyOpenMinute < 0 || *input.DailyOpenMinute >= MinutesPerDay):
		return fmt.Errorf("开放时刻需要在 00:00–23:59 之间")
	case input.DailyCloseMinute != nil && (*input.DailyCloseMinute < 0 || *input.DailyCloseMinute > MinutesPerDay):
		return fmt.Errorf("关闭时刻需要在 00:00–24:00 之间")
	case input.DailyOpenMinute != nil && input.DailyCloseMinute != nil &&
		(*input.DailyOpenMinute == *input.DailyCloseMinute || *input.DailyOpenMinute%MinutesPerDay == *input.DailyCloseMinute%MinutesPerDay):
		return fmt.Errorf("开放与关闭时刻不能相同；全天开放请清空时段")
	case !ValidAudience(input.Audience):
		return fmt.Errorf("未知的兑换对象：%s", input.Audience)
	case input.MinLevel < 0 || input.MinLevel > MaxMinLevel:
		return fmt.Errorf("等级门槛需要在 0–%d 之间", MaxMinLevel)
	case input.StartsAt != nil && input.EndsAt != nil && !input.EndsAt.After(*input.StartsAt):
		return fmt.Errorf("下架时间需要晚于上架时间")
	case !ValidStatus(input.Status):
		return fmt.Errorf("未知的商品状态：%s", input.Status)
	case !ValidDelivery(input.Delivery):
		return fmt.Errorf("未知的发放对象：%s", input.Delivery)
	}
	if err := ValidateRewards(input.Rewards); err != nil {
		return err
	}
	if input.Delivery == DeliveryGift {
		return ValidateGiftRewards(input.Rewards)
	}
	return nil
}

func normalizeTag(tag string) string {
	tags := vipdomain.NormalizeFeatureTags([]string{tag})
	if len(tags) == 0 {
		return ""
	}
	return tags[0]
}
