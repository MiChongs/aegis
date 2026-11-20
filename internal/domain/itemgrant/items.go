package itemgrant

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/shopspring/decimal"

	vipdomain "aegis/internal/domain/vip"
)

// 物品发放能发的全部物品。
//
// 这是全站「能给用户发的东西」的并集：积分商城、卡密、激励广告、邀请、排行榜、抽奖各自只开放其中一部分，
// 物品发放是运营手里的总闸，全部开放。发放逐档复用各自账本的同一个事务内入口（会员段、功能解锁、
// 功能时长、免广告通行证、抽奖次数批次、钱包、积分 / 经验流水、AI 赠送额度、授权卡），
// 所以这里发出去的东西与从别处得到的完全同口径：会员折算、扣除顺序、排行榜口径都照常生效。
const (
	ItemPoints       = "points"
	ItemExperience   = "experience"
	ItemVipDays      = "vip_days"
	ItemVipPlan      = "vip_plan"
	ItemVipLifetime  = "vip_lifetime"
	ItemFeature      = "feature"
	ItemFeatureTime  = "feature_time"
	ItemAdFree       = "ad_free"
	ItemLotteryDraws = "lottery_draws"
	ItemAICredit     = "ai_credit"
	ItemBalance      = "balance"
	ItemLicenseDays  = "license_days"
	ItemDeviceSlots  = "device_slots"
	ItemManual       = "manual"
	// 用户号码的权益（docs/user-number.md）
	ItemNumberChange = "number_change"
	ItemNumberTicket = "number_ticket"
	ItemNumberDraw   = "number_draw"
	ItemNumberAssign = "number_assign"
	// ItemCosmetic 个性装扮（docs/cosmetics.md）
	ItemCosmetic = "cosmetic"
)

// 取值形态：决定 Item 上的哪个字段承载内容（控制台按它渲染表单）。
const (
	ValueNone           = ""                // 不需要取值（永久会员）
	ValueAmount         = "amount"          // Item.Amount（可用 AmountExpr 按人计算）
	ValueMoney          = "money"           // Item.Money
	ValueRef            = "ref"             // Item.RefID（会员套餐）
	ValueFeature        = "feature"         // Item.Tag + Item.Amount（天数，0 为永久）
	ValueFeatureMinutes = "feature_minutes" // Item.Tag + Item.Amount（分钟）
	ValueText           = "text"            // Item.Text（人工发放）
	ValueNumberTicket   = "number_ticket"   // Item.Amount（张数）+ Item.RefID（靓号券等级 1–4）
	ValueCosmetic       = "cosmetic"        // Item.RefID（装扮编号）+ Item.Amount（天数，0 为永久）
)

// 物品分组，控制台物品面板按组排列。
const (
	GroupGrowth     = "growth"
	GroupMembership = "membership"
	GroupBenefit    = "benefit"
	GroupAsset      = "asset"
	GroupLicense    = "license"
	GroupOther      = "other"
)

// 上界。都不是业务规则，是防手滑：多打两个零的规则一旦面向全站执行就收不回来了。
const (
	MaxFeatureDays        = 3650
	MaxFeatureTimeMinutes = 100_000
	MaxAdFreeDays         = 3650
	MaxAICreditPoints     = 1_000_000
	MaxManualTextRunes    = 64
	// MaxItemsPerNode 一个发放节点最多几项物品
	MaxItemsPerNode = 12
)

var maxBalance = decimal.NewFromInt(100_000)

// IntegralCategory / SourceType 积分与经验流水的分类与来源：物品发放（运营赠送，不计入周期排行榜）。
const (
	IntegralCategory = "item_grant"
	SourceType       = "item_grant"
)

// ItemSpec 一档物品的自述。服务端校验、控制台的物品面板与发放结果的文案都读它。
//
// TestItemCatalogHasGrantBranch 双向钉死「目录 ↔ 发放分支」：目录多一条 → 配得上却发不出来；
// 发放多一条 → 控制台配不出来。
type ItemSpec struct {
	Type  string `json:"type"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
	Group string `json:"group"`
	Value string `json:"value"`
	Unit  string `json:"unit,omitempty"`
	Min   int64  `json:"min,omitempty"`
	Max   int64  `json:"max,omitempty"`
	// Scalable 数量可以写成按人计算的表达式（AmountExpr）
	Scalable bool `json:"scalable,omitempty"`
	// NeedsLoginCard 领取人名下必须有授权卡，否则这一项发放失败（整笔回滚）
	NeedsLoginCard bool `json:"needsLoginCard,omitempty"`
	// Pending 不自动到账：记录进入待发放，由管理员处理
	Pending bool `json:"pending,omitempty"`
}

var itemSpecs = []ItemSpec{
	{Type: ItemPoints, Label: "积分", Group: GroupGrowth, Value: ValueAmount, Unit: "积分", Min: 1, Max: 100_000_000, Scalable: true,
		Hint: "进积分账本，流水分类为物品发放（不计入周期排行榜）"},
	{Type: ItemExperience, Label: "经验值", Group: GroupGrowth, Value: ValueAmount, Unit: "点", Min: 1, Max: 100_000_000, Scalable: true,
		Hint: "进经验账本并同步等级；等级只升不降"},
	{Type: ItemVipDays, Label: "会员天数", Group: GroupMembership, Value: ValueAmount, Unit: "天", Min: 1, Max: 3650, Scalable: true,
		Hint: "不挂套餐，直接顺延会员到期时间；永久会员收到时按应用的折算策略折成积分"},
	{Type: ItemVipPlan, Label: "会员套餐", Group: GroupMembership, Value: ValueRef,
		Hint: "按套餐配置发放时长与功能（试用套餐除外）；已包含时按折算策略处理"},
	{Type: ItemVipLifetime, Label: "永久会员", Group: GroupMembership, Value: ValueNone,
		Hint: "直接开通永久会员（不挂套餐）；开通时被覆盖的限时权益按应用策略折算"},
	{Type: ItemFeature, Label: "功能解锁", Group: GroupBenefit, Value: ValueFeature, Unit: "天", Min: 0, Max: MaxFeatureDays,
		Hint: "单独解锁一个功能，不成为会员；天数为 0 即永久，同一功能多次发放时顺延"},
	{Type: ItemFeatureTime, Label: "功能时长", Group: GroupBenefit, Value: ValueFeatureMinutes, Unit: "分钟", Min: 1, Max: MaxFeatureTimeMinutes, Scalable: true,
		Hint: "按实际使用秒数计量的功能时长，与激励广告领到的是同一个账户"},
	{Type: ItemAdFree, Label: "免广告时长", Group: GroupBenefit, Value: ValueAmount, Unit: "天", Min: 1, Max: MaxAdFreeDays, Scalable: true,
		Hint: "有效期内免开屏广告，功能时长直接免扣；多次发放顺延"},
	{Type: ItemLotteryDraws, Label: "抽奖次数", Group: GroupAsset, Value: ValueAmount, Unit: "次", Min: 1, Max: 10_000, Scalable: true,
		Hint: "应用级次数，任意活动可用；记为赠送的次数（排在兑换所得之前使用）"},
	{Type: ItemAICredit, Label: "AI 额度", Group: GroupAsset, Value: ValueAmount, Unit: "点", Min: 1, Max: MaxAICreditPoints, Scalable: true,
		Hint: "AI 中转的赠送额度，1 点按 1 积分定价、不过期"},
	{Type: ItemBalance, Label: "钱包余额", Group: GroupAsset, Value: ValueMoney, Unit: "元",
		Hint: "计入余额但不计入累计充值"},
	{Type: ItemLicenseDays, Label: "授权时长", Group: GroupLicense, Value: ValueAmount, Unit: "天", Min: 1, Max: 3650, Scalable: true, NeedsLoginCard: true,
		Hint: "给领取人名下的授权卡延长授权期；名下没有限时授权卡时这一笔失败"},
	{Type: ItemDeviceSlots, Label: "设备位", Group: GroupLicense, Value: ValueAmount, Unit: "台", Min: 1, Max: 63, NeedsLoginCard: true,
		Hint: "给领取人名下授权期最长的授权卡加可绑定设备数；没有授权卡时这一笔失败"},
	{Type: ItemNumberChange, Label: "改号卡", Group: GroupBenefit, Value: ValueAmount, Unit: "张", Min: 1, Max: MaxNumberRights, Scalable: true,
		Hint: "自选一个普通号码；与积分商城、卡密等渠道发的是同一个账户"},
	{Type: ItemNumberTicket, Label: "靓号券", Group: GroupBenefit, Value: ValueNumberTicket, Unit: "张", Min: 1, Max: MaxNumberRights,
		Hint: "按等级自选靓号：1 精品、2 稀有、3 传说、4 至尊；能换不高于其等级的号码"},
	{Type: ItemNumberDraw, Label: "摇号次数", Group: GroupBenefit, Value: ValueAmount, Unit: "次", Min: 1, Max: MaxNumberRights, Scalable: true,
		Hint: "摇一次给若干候选号，任选其一换上；概率受今日号运影响"},
	{Type: ItemNumberAssign, Label: "指定号码", Group: GroupBenefit, Value: ValueText,
		Hint: "直接把这个号码换给领取人（不扣权益、不看冷却）；号码已被使用、保留或冻结时这一笔失败"},
	{Type: ItemCosmetic, Label: "个性装扮", Group: GroupBenefit, Value: ValueCosmetic, Unit: "天", Min: 0, Max: MaxFeatureDays,
		Hint: "头像框、资料卡背景、昵称样式或勋章；天数为 0 即永久，同一件多次发放时顺延，已永久持有时不重复发放"},
	{Type: ItemManual, Label: "人工发放", Group: GroupOther, Value: ValueText, Pending: true,
		Hint: "记录进入待发放，由管理员在发放记录里标记已发放（实物、兑换码等）"},
}

// GroupSpec 物品分组的自述。
type GroupSpec struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

var groupSpecs = []GroupSpec{
	{Key: GroupGrowth, Label: "成长"},
	{Key: GroupMembership, Label: "会员"},
	{Key: GroupBenefit, Label: "功能权益"},
	{Key: GroupAsset, Label: "资产"},
	{Key: GroupLicense, Label: "授权卡"},
	{Key: GroupOther, Label: "其他"},
}

// ItemCatalog 全部物品（副本），顺序即控制台的展示顺序，也是同一节点内的发放顺序。
func ItemCatalog() []ItemSpec { return slices.Clone(itemSpecs) }

// ItemGroups 物品分组（副本）。
func ItemGroups() []GroupSpec { return slices.Clone(groupSpecs) }

// FindItemSpec 按类型取物品自述。
func FindItemSpec(itemType string) (ItemSpec, bool) {
	for _, spec := range itemSpecs {
		if spec.Type == itemType {
			return spec, true
		}
	}
	return ItemSpec{}, false
}

// Item 一项物品。
type Item struct {
	Type   string          `json:"type"`
	Amount int64           `json:"amount,omitempty"`
	Money  decimal.Decimal `json:"money,omitempty"`
	RefID  int64           `json:"refId,omitempty"`
	Tag    string          `json:"tag,omitempty"`
	Text   string          `json:"text,omitempty"`
	// AmountExpr 按人计算数量的表达式（只对 Scalable 的物品有效），例如 `10 + user.signStreak * 2`；
	// 结果向下取整并截在物品的上下界内，算不出来时退回 Amount。
	AmountExpr string `json:"amountExpr,omitempty"`
}

// NormalizeItem 抹掉与取值形态无关的字段。
func NormalizeItem(item Item) Item {
	spec, ok := FindItemSpec(item.Type)
	if !ok {
		return item
	}
	clean := Item{Type: spec.Type}
	switch spec.Value {
	case ValueAmount:
		clean.Amount = item.Amount
		if spec.Scalable {
			clean.AmountExpr = strings.TrimSpace(item.AmountExpr)
		}
	case ValueMoney:
		clean.Money = item.Money.Round(2)
	case ValueRef:
		clean.RefID = item.RefID
	case ValueFeature:
		clean.Tag = normalizeTag(item.Tag)
		clean.Amount = item.Amount
	case ValueFeatureMinutes:
		clean.Tag = normalizeTag(item.Tag)
		clean.Amount = item.Amount
		clean.AmountExpr = strings.TrimSpace(item.AmountExpr)
	case ValueNumberTicket:
		clean.Amount = item.Amount
		clean.RefID = item.RefID
	case ValueCosmetic:
		clean.RefID = item.RefID
		clean.Amount = item.Amount
	case ValueText:
		clean.Text = strings.TrimSpace(item.Text)
	}
	return clean
}

// ValidateItem 校验一项物品的形状（引用的套餐与功能是否存在由服务层查）。
func ValidateItem(item Item) error {
	spec, ok := FindItemSpec(item.Type)
	if !ok {
		return fmt.Errorf("未登记的物品类型：%s", item.Type)
	}
	switch spec.Value {
	case ValueAmount, ValueFeatureMinutes:
		if spec.Value == ValueFeatureMinutes && item.Tag == "" {
			return fmt.Errorf("「%s」没有选择功能", spec.Label)
		}
		if item.Amount < spec.Min || item.Amount > spec.Max {
			return fmt.Errorf("「%s」需要在 %d–%d %s之间", spec.Label, spec.Min, spec.Max, spec.Unit)
		}
		if item.AmountExpr != "" && !spec.Scalable {
			return fmt.Errorf("「%s」不支持按人计算数量", spec.Label)
		}
	case ValueMoney:
		if item.Money.LessThanOrEqual(decimal.Zero) {
			return fmt.Errorf("「%s」必须大于 0", spec.Label)
		}
		if item.Money.GreaterThan(maxBalance) {
			return fmt.Errorf("「%s」单笔不能超过 %s 元", spec.Label, maxBalance.String())
		}
		if !item.Money.Equal(item.Money.Round(2)) {
			return fmt.Errorf("「%s」最多两位小数", spec.Label)
		}
	case ValueRef:
		if item.RefID <= 0 {
			return fmt.Errorf("「%s」没有选择具体套餐", spec.Label)
		}
	case ValueFeature:
		if item.Tag == "" {
			return fmt.Errorf("「%s」没有选择功能", spec.Label)
		}
		if item.Amount < 0 || item.Amount > MaxFeatureDays {
			return fmt.Errorf("「%s」的天数需要在 0–%d 之间（0 为永久）", spec.Label, MaxFeatureDays)
		}
	case ValueNumberTicket:
		if item.RefID < 1 || item.RefID > 4 {
			return fmt.Errorf("「%s」没有选择等级（1–4）", spec.Label)
		}
		if item.Amount < spec.Min || item.Amount > spec.Max {
			return fmt.Errorf("「%s」需要在 %d–%d %s之间", spec.Label, spec.Min, spec.Max, spec.Unit)
		}
	case ValueCosmetic:
		if item.RefID <= 0 {
			return fmt.Errorf("「%s」没有选择具体装扮", spec.Label)
		}
		if item.Amount < 0 || item.Amount > MaxFeatureDays {
			return fmt.Errorf("「%s」的天数需要在 0–%d 之间（0 为永久）", spec.Label, MaxFeatureDays)
		}
	case ValueText:
		if item.Type == ItemNumberAssign {
			if !validNumberText(item.Text) {
				return fmt.Errorf("「%s」需要填写 5–12 位、不以 0 开头的纯数字", spec.Label)
			}
			return nil
		}
		if item.Text == "" {
			return fmt.Errorf("「%s」需要写明发放什么", spec.Label)
		}
		if utf8.RuneCountInString(item.Text) > MaxManualTextRunes {
			return fmt.Errorf("「%s」的说明不能超过 %d 字", spec.Label, MaxManualTextRunes)
		}
	}
	return nil
}

// ValidateItems 校验一组物品（一个发放节点或随机节点的一个选项）。
func ValidateItems(items []Item) error {
	if len(items) == 0 {
		return fmt.Errorf("至少要配置一项物品")
	}
	if len(items) > MaxItemsPerNode {
		return fmt.Errorf("最多配置 %d 项物品", MaxItemsPerNode)
	}
	for _, item := range items {
		if err := ValidateItem(item); err != nil {
			return err
		}
	}
	return nil
}

// PlanRef / FeatureRef 描述物品时需要的套餐与功能信息。
type PlanRef struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	DurationDays int    `json:"durationDays"`
	Lifetime     bool   `json:"lifetime"`
	Trial        bool   `json:"trial"`
	Active       bool   `json:"active"`
}

type FeatureRef struct {
	Tag      string `json:"tag"`
	Name     string `json:"name"`
	IsActive bool   `json:"isActive"`
}

// CosmeticRef 描述物品时需要的装扮信息。Live 为上架且发布过（只有这样的装扮发得出去）。
type CosmeticRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Slot string `json:"slot"`
	Live bool   `json:"live"`
}

// Refs 套餐、功能与装扮目录（描述物品、校验引用用）。
type Refs struct {
	Plans     map[int64]PlanRef
	Features  map[string]FeatureRef
	Cosmetics map[int64]CosmeticRef
}

// DescribeItem 一句话说清这项物品：「积分 500」「在线听歌 永久」「会员 30 天」。
func DescribeItem(item Item, refs Refs) string {
	spec, ok := FindItemSpec(item.Type)
	if !ok {
		return item.Type
	}
	featureName := func(tag string) string {
		if feature, ok := refs.Features[tag]; ok && strings.TrimSpace(feature.Name) != "" {
			return feature.Name
		}
		return tag
	}
	switch spec.Value {
	case ValueNone:
		return spec.Label
	case ValueMoney:
		return spec.Label + " " + item.Money.StringFixed(2) + " 元"
	case ValueRef:
		if plan, ok := refs.Plans[item.RefID]; ok {
			return plan.Name + "（" + TermLabel(plan.DurationDays, plan.Lifetime) + "）"
		}
		return "套餐 #" + strconv.FormatInt(item.RefID, 10)
	case ValueFeature:
		return featureName(item.Tag) + " " + TermLabel(int(item.Amount), item.Amount == 0)
	case ValueFeatureMinutes:
		return featureName(item.Tag) + " " + strconv.FormatInt(item.Amount, 10) + " 分钟"
	case ValueNumberTicket:
		return numberTicketName(item.RefID) + " " + strconv.FormatInt(item.Amount, 10) + " 张"
	case ValueCosmetic:
		name := "装扮 #" + strconv.FormatInt(item.RefID, 10)
		if ref, ok := refs.Cosmetics[item.RefID]; ok && strings.TrimSpace(ref.Name) != "" {
			name = ref.Name
		}
		return name + " " + TermLabel(int(item.Amount), item.Amount == 0)
	case ValueText:
		if item.Type == ItemNumberAssign {
			return "号码 " + item.Text
		}
		return item.Text
	}
	switch item.Type {
	case ItemVipDays:
		return "会员 " + TermLabel(int(item.Amount), false)
	case ItemAdFree:
		return "免广告 " + TermLabel(int(item.Amount), false)
	case ItemLicenseDays:
		return "授权 " + TermLabel(int(item.Amount), false)
	}
	if spec.Unit == spec.Label {
		// 「积分 10 积分」读着重复：单位就是名称时只写数量与单位
		return strconv.FormatInt(item.Amount, 10) + " " + spec.Unit
	}
	return spec.Label + " " + strconv.FormatInt(item.Amount, 10) + " " + spec.Unit
}

// TermLabel 时长的展示口径：永久 / 1 年 / 3 个月 / 7 天。
func TermLabel(days int, permanent bool) string {
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

func normalizeTag(tag string) string {
	tags := vipdomain.NormalizeFeatureTags([]string{tag})
	if len(tags) == 0 {
		return ""
	}
	return tags[0]
}
