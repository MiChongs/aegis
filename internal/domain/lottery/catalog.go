package lottery

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"aegis/internal/domain/invite"
	"aegis/internal/domain/pointstore"
	vipdomain "aegis/internal/domain/vip"
)

// 奖品类型。除「谢谢参与」外与积分商城、邀请同名同义（发放走同一套账本）。
const (
	KindNone         = "none"
	KindPoints       = invite.RewardPoints
	KindVipPlan      = pointstore.RewardVipPlan
	KindVipDays      = pointstore.RewardVipDays
	KindFeature      = pointstore.RewardFeature
	KindExperience   = pointstore.RewardExperience
	KindLotteryDraws = pointstore.RewardLotteryDraws
	KindManual       = pointstore.RewardManual
	// 用户号码的权益（docs/user-number.md）：改号卡、靓号券（refId 为等级 1–4）、摇号次数
	KindNumberChange = pointstore.RewardNumberChange
	KindNumberTicket = pointstore.RewardNumberTicket
	KindNumberDraw   = pointstore.RewardNumberDraw
	// KindCosmetic 个性装扮（docs/cosmetics.md）：refId 装扮编号，amount 天数（0 为永久）
	KindCosmetic = pointstore.RewardCosmetic
	// KindCoupon 优惠券（docs/coupons.md）：refId 券编号，amount 张数
	KindCoupon = pointstore.RewardCoupon
	// 抽奖独有的三档（积分商城、卡密里没有）
	KindVipLifetime = "vip_lifetime"
	KindFeatureTime = "feature_time"
	KindAdFree      = "ad_free"
)

// 抽奖独有奖品的取值形态：功能时长是功能标识 + 分钟数（Reward.Tag + Reward.Amount）。
const ValueFeatureMinutes = "feature_minutes"

// 上界。
const (
	MaxFeatureTimeMinutes = 100_000
	MaxAdFreeDays         = 3650
)

// IntegralCategory / SourceType 积分与经验流水上的分类和来源：抽奖。
const (
	IntegralCategory = "lottery"
	SourceType       = "lottery"
)

// RewardSpec 一档奖品的自述，形状与积分商城相同；控制台的奖品表单由它驱动。
type RewardSpec = pointstore.RewardSpec

// kinds 抽奖能发的奖品，顺序即控制台的展示顺序。
//
// 不含钱包余额：余额是真钱，放进一个按概率出奖的盘子里，等于运营在做一件需要牌照的事。
var kinds = []string{
	KindNone,
	KindPoints,
	KindExperience,
	KindVipDays,
	KindVipPlan,
	KindFeature,
	KindLotteryDraws,
	KindVipLifetime,
	KindFeatureTime,
	KindAdFree,
	KindNumberChange,
	KindNumberTicket,
	KindNumberDraw,
	KindCosmetic,
	KindCoupon,
	KindManual,
}

// localSpecs 抽奖独有的奖品档位。
var localSpecs = map[string]RewardSpec{
	KindVipLifetime: {
		Type:  KindVipLifetime,
		Label: "永久会员",
		Hint:  "直接开通永久会员（不挂套餐、不携带功能标识，与会员天数同口径）；已是永久会员时按已拥有处理",
		Value: "",
	},
	KindFeatureTime: {
		Type:  KindFeatureTime,
		Label: "功能时长",
		Hint:  "直接发放某功能的使用时长（原本看激励广告换的那种），按分钟计；会员与已解锁该功能的人本就免扣",
		Value: ValueFeatureMinutes,
		Unit:  "分钟",
		Min:   1,
		Max:   MaxFeatureTimeMinutes,
	},
	KindAdFree: {
		Type:  KindAdFree,
		Label: "免广告时长",
		Hint:  "有效期内免开屏广告，看激励广告换时长的功能直接免扣；多次抽中顺延",
		Value: ValueAmount,
		Unit:  "天",
		Min:   1,
		Max:   MaxAdFreeDays,
	},
}

// ValueAmount 数量型取值（与积分商城同名）。
const ValueAmount = pointstore.ValueAmount

// clientKinds 老版本客户端只认识积分商城那几档：抽奖独有的奖品对它们按最接近的已知类型下发，
// 图标、配色与「抽中后刷新会员与功能缓存」都能照常工作（真实类型另放在 rewardType）。
var clientKinds = map[string]string{
	KindVipLifetime: KindVipPlan,
	KindFeatureTime: KindFeature,
	KindAdFree:      KindFeature,
}

// ClientKind 用户端 kind 字段的取值。
func ClientKind(kind string) string {
	if alias, ok := clientKinds[kind]; ok {
		return alias
	}
	return kind
}

var noneSpec = RewardSpec{
	Type:  KindNone,
	Label: "谢谢参与",
	Hint:  "不发放任何东西；连续抽到它会累计保底进度",
	Value: "",
}

// Catalog 全部奖品档位。
func Catalog() []RewardSpec {
	out := make([]RewardSpec, 0, len(kinds))
	for _, kind := range kinds {
		if spec, ok := FindSpec(kind); ok {
			out = append(out, spec)
		}
	}
	return out
}

// FindSpec 按类型取奖品自述。
func FindSpec(kind string) (RewardSpec, bool) {
	switch kind {
	case KindNone:
		return noneSpec, true
	case KindPoints:
		spec, ok := invite.FindRewardSpec(KindPoints)
		if ok {
			spec.Hint = "计入积分余额，流水分类为抽奖"
		}
		return spec, ok
	}
	if spec, ok := localSpecs[kind]; ok {
		return spec, true
	}
	if !slices.Contains(kinds, kind) {
		return RewardSpec{}, false
	}
	spec, ok := pointstore.FindRewardSpec(kind)
	if !ok {
		return RewardSpec{}, false
	}
	spec.Repeatable, spec.Exclusive = false, false
	switch kind {
	case KindVipPlan:
		spec.Hint = "按套餐配置发放时长与功能；不能选择试用套餐"
	case KindFeature:
		spec.Hint = "单独解锁一个功能，不成为会员；天数为 0 即永久，同一功能多次抽中时顺延"
	case KindLotteryDraws:
		spec.Hint = "抽中后立即计入抽奖次数，任意活动可用"
	case KindManual:
		spec.Hint = "抽中后进入待发放，由管理员在抽奖记录里标记已发放"
	case KindCosmetic:
		spec.Hint = "头像框、资料卡背景、昵称样式或勋章；天数为 0 即永久，同一件多次抽中时顺延，已永久持有时不重复记"
	case KindCoupon:
		spec.Hint = "抽中后发进券包；券发完或已结束时这一抽失败、不扣次数"
	}
	return spec, true
}

// Kinds 全部奖品类型。
func Kinds() []string { return slices.Clone(kinds) }

// UIModes 界面形态。
func UIModes() []string { return []string{UIModeGrid, UIModeWheel} }

// Statuses 活动状态，顺序即控制台的展示顺序。
func Statuses() []string { return []string{StatusDraft, StatusActive, StatusPaused, StatusEnded} }

// Sources 次数来源。
func Sources() []string { return []string{SourceFree, SourceBonus, SourcePoints} }

// NormalizeReward 只留与类型取值形态相关的字段。
func NormalizeReward(reward Reward) Reward {
	reward.Type = strings.TrimSpace(reward.Type)
	if reward.Type == KindNone {
		return Reward{Type: KindNone}
	}
	switch reward.Type {
	case KindPoints, KindAdFree:
		return Reward{Type: reward.Type, Amount: reward.Amount}
	case KindVipLifetime:
		return Reward{Type: KindVipLifetime}
	case KindFeatureTime:
		tags := vipdomain.NormalizeFeatureTags([]string{reward.Tag})
		tag := ""
		if len(tags) > 0 {
			tag = tags[0]
		}
		return Reward{Type: KindFeatureTime, Tag: tag, Amount: reward.Amount}
	}
	normalized := pointstore.NormalizeRewards([]Reward{reward})
	if len(normalized) == 0 {
		// 未登记的类型原样留着，交给 ValidateReward 报出来
		return reward
	}
	return normalized[0]
}

// ValidateReward 校验一个奖品发的东西（只校验形状，引用的套餐与功能是否存在由服务层查）。
func ValidateReward(reward Reward) error {
	if reward.Type == KindNone {
		return nil
	}
	if !slices.Contains(kinds, reward.Type) {
		return fmt.Errorf("未登记的奖品类型：%s", reward.Type)
	}
	switch reward.Type {
	case KindPoints:
		return invite.ValidateRewards("奖品", []Reward{reward})
	case KindVipLifetime:
		return nil
	case KindFeatureTime, KindAdFree:
		spec := localSpecs[reward.Type]
		if reward.Type == KindFeatureTime && reward.Tag == "" {
			return fmt.Errorf("%s需要选择功能", spec.Label)
		}
		if reward.Amount < spec.Min || reward.Amount > spec.Max {
			return fmt.Errorf("%s需要在 %d–%d %s之间", spec.Label, spec.Min, spec.Max, spec.Unit)
		}
		return nil
	}
	return pointstore.ValidateRewards([]Reward{reward})
}

// DescribeReward 一句话说清这个奖品发什么（「积分 30 点」「会员 7 天」）。
func DescribeReward(reward Reward, plans map[int64]pointstore.PlanRef, features map[string]pointstore.FeatureRef) pointstore.ClientReward {
	switch reward.Type {
	case KindNone:
		return pointstore.ClientReward{Type: KindNone, Label: noneSpec.Label, Detail: noneSpec.Label}
	case KindVipLifetime:
		return pointstore.ClientReward{Type: KindVipLifetime, Label: localSpecs[KindVipLifetime].Label,
			Detail: "永久会员", Permanent: true}
	case KindAdFree:
		return pointstore.ClientReward{Type: KindAdFree, Label: localSpecs[KindAdFree].Label,
			Detail: fmt.Sprintf("免广告 %d 天", reward.Amount), Days: int(reward.Amount)}
	case KindFeatureTime:
		name := reward.Tag
		if feature, ok := features[reward.Tag]; ok && strings.TrimSpace(feature.Name) != "" {
			name = feature.Name
		}
		return pointstore.ClientReward{Type: KindFeatureTime, Label: localSpecs[KindFeatureTime].Label,
			Detail: fmt.Sprintf("%s %d 分钟", name, reward.Amount), Tag: reward.Tag}
	}
	return invite.DescribeReward(reward, plans, features)
}

// DefaultPrizeName 奖品没起名字时用的名字：「30 积分」「会员 7 天」「谢谢参与」。
func DefaultPrizeName(reward Reward, plans map[int64]pointstore.PlanRef, features map[string]pointstore.FeatureRef) string {
	switch reward.Type {
	case KindNone:
		return noneSpec.Label
	case KindPoints:
		return fmt.Sprintf("%d 积分", reward.Amount)
	case KindExperience:
		return fmt.Sprintf("%d 经验", reward.Amount)
	case KindLotteryDraws:
		return fmt.Sprintf("抽奖 %d 次", reward.Amount)
	}
	name := DescribeReward(reward, plans, features).Detail
	if utf8.RuneCountInString(name) > MaxPrizeNameRunes {
		name = string([]rune(name)[:MaxPrizeNameRunes])
	}
	return name
}

// NormalizeInput 清洗活动输入：去空白、补默认值、奖品按位置排序。
func NormalizeInput(input ActivityInput) ActivityInput {
	input.Name = strings.TrimSpace(input.Name)
	input.Description = strings.TrimSpace(input.Description)
	input.Rules = strings.TrimSpace(input.Rules)
	input.VideoScene = strings.ToLower(strings.TrimSpace(input.VideoScene))
	if input.UIMode == "" {
		input.UIMode = UIModeGrid
	}
	if input.Status == "" {
		input.Status = StatusDraft
	}
	if input.PityThreshold <= 0 {
		input.PityThreshold = 0
		input.PityPosition = -1
	}
	prizes := make([]PrizeInput, 0, len(input.Prizes))
	for _, prize := range input.Prizes {
		prize.Name = strings.TrimSpace(prize.Name)
		prize.ImageURL = strings.TrimSpace(prize.ImageURL)
		prize.Reward = NormalizeReward(prize.Reward)
		if prize.Quantity < 0 {
			prize.Quantity = -1
		}
		prizes = append(prizes, prize)
	}
	slices.SortStableFunc(prizes, func(a, b PrizeInput) int { return a.Position - b.Position })
	input.Prizes = prizes
	NormalizeLifetime(&input)
	input.Strategy = NormalizeStrategy(input.Strategy)
	input.Perks = NormalizePerks(input.Perks)
	return input
}

// ValidateInput 校验清洗后的活动输入。每一类问题都给出能直接照着改的文案。
func ValidateInput(input ActivityInput) error {
	switch {
	case input.Name == "":
		return fmt.Errorf("活动名称不能为空")
	case utf8.RuneCountInString(input.Name) > MaxNameRunes:
		return fmt.Errorf("活动名称不能超过 %d 字", MaxNameRunes)
	case utf8.RuneCountInString(input.Description) > MaxDescriptionRunes:
		return fmt.Errorf("活动简介不能超过 %d 字", MaxDescriptionRunes)
	case utf8.RuneCountInString(input.Rules) > MaxRulesRunes:
		return fmt.Errorf("活动规则不能超过 %d 字", MaxRulesRunes)
	case !slices.Contains(UIModes(), input.UIMode):
		return fmt.Errorf("未知的界面形态：%s", input.UIMode)
	case !slices.Contains(Statuses(), input.Status):
		return fmt.Errorf("未知的活动状态：%s", input.Status)
	case input.StartTime != nil && input.EndTime != nil && !input.EndTime.After(*input.StartTime):
		return fmt.Errorf("结束时间需要晚于开始时间")
	case input.FreeDaily < 0 || input.FreeDaily > MaxFreeDaily:
		return fmt.Errorf("每日免费次数需要在 0–%d 之间", MaxFreeDaily)
	case input.PointsCost < 0 || input.PointsCost > MaxPointsCost:
		return fmt.Errorf("每次消耗积分需要在 0–%d 之间", MaxPointsCost)
	case input.PointsDaily < 0 || input.PointsDaily > MaxPointsDaily:
		return fmt.Errorf("积分抽奖每日上限需要在 0–%d 之间", MaxPointsDaily)
	case input.PityThreshold < 0 || input.PityThreshold > MaxPityThreshold:
		return fmt.Errorf("保底次数需要在 0–%d 之间", MaxPityThreshold)
	case input.PityThreshold == 1:
		return fmt.Errorf("保底次数至少为 2（为 1 等于每次必中保底奖）")
	}
	return validatePrizes(input)
}

func validatePrizes(input ActivityInput) error {
	count := len(input.Prizes)
	switch input.UIMode {
	case UIModeGrid:
		if count != GridPrizeCount {
			return fmt.Errorf("九宫格需要恰好 %d 个奖品，当前 %d 个", GridPrizeCount, count)
		}
	case UIModeWheel:
		if count < WheelMinPrizes || count > WheelMaxPrizes {
			return fmt.Errorf("转盘需要 %d–%d 个奖品，当前 %d 个", WheelMinPrizes, WheelMaxPrizes, count)
		}
	}
	positions := make(map[int]bool, count)
	ids := make(map[int64]bool, count)
	totalWeight := 0
	unlimited := false
	fallbacks := 0
	for index, prize := range input.Prizes {
		label := fmt.Sprintf("第 %d 个奖品", index+1)
		if prize.Position < 0 || prize.Position >= count {
			return fmt.Errorf("%s的位置需要在 0–%d 之间", label, count-1)
		}
		if positions[prize.Position] {
			return fmt.Errorf("奖品位置 %d 重复", prize.Position)
		}
		positions[prize.Position] = true
		if prize.ID > 0 {
			if ids[prize.ID] {
				return fmt.Errorf("奖品 #%d 重复出现", prize.ID)
			}
			ids[prize.ID] = true
		}
		if utf8.RuneCountInString(prize.Name) > MaxPrizeNameRunes {
			return fmt.Errorf("%s的名称不能超过 %d 字", label, MaxPrizeNameRunes)
		}
		if len(prize.ImageURL) > 512 {
			return fmt.Errorf("%s的图片地址过长", label)
		}
		if prize.ImageURL != "" && !strings.HasPrefix(prize.ImageURL, "https://") && !strings.HasPrefix(prize.ImageURL, "http://") {
			return fmt.Errorf("%s的图片地址需以 http(s):// 开头", label)
		}
		if err := ValidateReward(prize.Reward); err != nil {
			return fmt.Errorf("%s：%w", label, err)
		}
		if prize.Weight < 0 || prize.Weight > MaxWeight {
			return fmt.Errorf("%s的权重需要在 0–%d 之间", label, MaxWeight)
		}
		if prize.Quantity > MaxQuantity {
			return fmt.Errorf("%s的库存不能超过 %d", label, MaxQuantity)
		}
		for _, limit := range []int{prize.DailyLimit, prize.UserLimit, prize.UserDailyLimit} {
			if limit < 0 || limit > MaxPrizeLimit {
				return fmt.Errorf("%s的限量需要在 0–%d 之间（0 为不限）", label, MaxPrizeLimit)
			}
		}
		if prize.Value < 0 || prize.Value > MaxQuantity {
			return fmt.Errorf("%s的积分当量需要在 0–%d 之间（0 为自动估算）", label, MaxQuantity)
		}
		if prize.UserLimit > 0 && prize.UserDailyLimit > prize.UserLimit {
			return fmt.Errorf("%s的每人每日上限不能大于每人累计上限", label)
		}
		if prize.Fallback && (prize.DailyLimit > 0 || prize.UserLimit > 0 || prize.UserDailyLimit > 0) {
			return fmt.Errorf("%s是兜底奖，不能设置限量", label)
		}
		totalWeight += prize.Weight
		if prize.Quantity < 0 {
			unlimited = true
		}
		if prize.Fallback {
			fallbacks++
			if prize.Quantity >= 0 {
				return fmt.Errorf("%s是兜底奖，库存必须不限", label)
			}
		}
	}
	if fallbacks > 1 {
		return fmt.Errorf("最多设置一个兜底奖")
	}
	if totalWeight <= 0 {
		return fmt.Errorf("奖品的权重合计必须大于 0")
	}
	if err := validateLifetime(input); err != nil {
		return err
	}
	if err := ValidateStrategy(input.Strategy, count); err != nil {
		return err
	}
	if err := ValidatePerks(input.Perks); err != nil {
		return err
	}
	// 至少一个不限库存的奖品：否则奖池总有被抽空的一天，那之后每一次抽奖都只能报错，
	// 而用户的免费次数、积分与看过的广告已经花出去了。
	if !unlimited {
		return fmt.Errorf("至少需要一个不限库存的奖品（通常是「谢谢参与」）")
	}
	if input.PityThreshold > 0 {
		if input.PityPosition < 0 {
			return fmt.Errorf("开启保底时需要选择保底奖品")
		}
		found := false
		for _, prize := range input.Prizes {
			if prize.Position != input.PityPosition {
				continue
			}
			found = true
			if prize.Reward.Type == KindNone {
				return fmt.Errorf("保底奖品不能是「谢谢参与」")
			}
		}
		if !found {
			return fmt.Errorf("保底奖品不在奖品列表里")
		}
	}
	return nil
}

// Probabilities 常规抽取下每个奖品的公示概率（百分比，不取整），按 prizes 的顺序。
// 售罄的奖品不参与抽取，概率为 0。
//
// 不在这里取整：积分账、策略、模拟都拿它算期望，十万分之一的大奖取整到 0.01 会被当成 0
// （公示成 0%，也不进积分账）。给用户看的数用 PublicProbability。
func Probabilities(prizes []Prize) []float64 {
	total := 0
	for _, prize := range prizes {
		if prize.InStock() {
			total += prize.Weight
		}
	}
	out := make([]float64, len(prizes))
	if total == 0 {
		return out
	}
	for index, prize := range prizes {
		if !prize.InStock() || prize.Weight == 0 {
			continue
		}
		out[index] = float64(prize.Weight) * 100 / float64(total)
	}
	return out
}

// PublicProbability 公示概率的取整（百分比）：不小于 1% 的保留两位小数，小于 1% 的保留两位有效数字。
// 抽得到的奖品永远不会显示成 0：28 → 28，33.3333 → 33.33，0.0125 → 0.013，0.001 → 0.001。
func PublicProbability(percent float64) float64 {
	if percent <= 0 || math.IsNaN(percent) || math.IsInf(percent, 0) {
		return 0
	}
	if percent >= 1 {
		return math.Round(percent*100) / 100
	}
	scale := math.Pow(10, 1-math.Floor(math.Log10(percent)))
	return math.Round(percent*scale) / scale
}

// Candidates 常规抽取的候选：有库存、权重大于 0。
func Candidates(prizes []Prize) []Prize {
	out := make([]Prize, 0, len(prizes))
	for _, prize := range prizes {
		if prize.Weight > 0 && prize.InStock() {
			out = append(out, prize)
		}
	}
	return out
}

// Pick 在候选里按权重取一个。roll 是 [0, 权重合计) 里的均匀随机数，由调用方用 crypto/rand 产生。
func Pick(candidates []Prize, roll int64) (Prize, bool) {
	var cumulative int64
	for _, prize := range candidates {
		cumulative += int64(prize.Weight)
		if roll < cumulative {
			return prize, true
		}
	}
	return Prize{}, false
}

// TotalWeight 候选的权重合计。
func TotalWeight(candidates []Prize) int64 {
	var total int64
	for _, prize := range candidates {
		total += int64(prize.Weight)
	}
	return total
}

// FallbackPrize 抽中的奖品被抢光时改发的奖品：标了兜底的那个；没有标的话，
// 依次取不限库存的「谢谢参与」、任意不限库存的奖品。
func FallbackPrize(prizes []Prize) (Prize, bool) {
	for _, prize := range prizes {
		if prize.Fallback && prize.Quantity < 0 {
			return prize, true
		}
	}
	for _, prize := range prizes {
		if prize.Kind == KindNone && prize.Quantity < 0 {
			return prize, true
		}
	}
	for _, prize := range prizes {
		if prize.Quantity < 0 {
			return prize, true
		}
	}
	return Prize{}, false
}

// PityDue 这一次是否触发保底：连续 missStreak 次没抽中，这一次就是第 threshold 次。
func PityDue(threshold, missStreak int) bool {
	return threshold > 0 && missStreak+1 >= threshold
}

// PityProgress 保底进度：最多再抽几次必中。
func PityProgress(activity Activity, prizes []Prize, missStreak int) *Pity {
	if activity.PityThreshold <= 0 || activity.PityPrizeID == nil {
		return nil
	}
	for _, prize := range prizes {
		if prize.ID != *activity.PityPrizeID {
			continue
		}
		// 保底奖抽光了就不再承诺「必中」
		if !prize.InStock() {
			return nil
		}
		remaining := activity.PityThreshold - missStreak
		if remaining < 1 {
			remaining = 1
		}
		return &Pity{Threshold: activity.PityThreshold, PrizeID: prize.ID, PrizeName: prize.Name, Remaining: remaining}
	}
	return nil
}

// NextMissStreak 抽完之后的连续未中次数。
func NextMissStreak(missStreak int, prize Prize) int {
	if prize.Winning() {
		return 0
	}
	return missStreak + 1
}
