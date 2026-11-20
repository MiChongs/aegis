package cardkey

import (
	"fmt"
	"slices"
	"strings"

	"github.com/shopspring/decimal"

	"aegis/internal/domain/usernumber"
	vipdomain "aegis/internal/domain/vip"
)

// 权益类型。
const (
	RewardVipPlan      = "vip_plan"
	RewardVipDays      = "vip_days"
	RewardFeature      = "feature"
	RewardIntegral     = "integral"
	RewardExperience   = "experience"
	RewardBalance      = "balance"
	RewardLotteryDraws = "lottery_draws"
	RewardLicenseDays  = "license_days"
	RewardDeviceSlots  = "device_slots"
	// 用户号码的权益（docs/user-number.md）
	RewardNumberChange = "number_change"
	RewardNumberTicket = "number_ticket"
	RewardNumberDraw   = "number_draw"
	// RewardCosmetic 个性装扮（docs/cosmetics.md）
	RewardCosmetic = "cosmetic"
)

// 权益的取值形态：决定 Reward 上的哪个字段承载数量。
const (
	// ValueAmount 整数量，走 Reward.Amount。
	ValueAmount = "amount"
	// ValueMoney 金额，走 Reward.Money。
	ValueMoney = "money"
	// ValueRef 引用既有实体的 ID，走 Reward.RefID。
	ValueRef = "ref"
	// ValueFeature 功能标识 + 整数量，走 Reward.Feature 与 Reward.Amount。
	//
	// 卡密的「功能解锁」（Amount 为天数，0 为永久）与激励广告的「功能时长」
	// （rewardedad.RewardFeatureMinutes，Amount 为分钟）共用这一形态，控制台按同一套 value 渲染表单。
	ValueFeature = "feature"
	// ValueNumberTicket 靓号券：Reward.Amount 张数 + Reward.RefID 等级（1 精品、2 稀有、3 传说、4 至尊）。
	ValueNumberTicket = "number_ticket"
	// ValueCosmetic 个性装扮：Reward.RefID 装扮编号 + Reward.Amount 天数（0 为永久）。
	ValueCosmetic = "cosmetic"
)

// MaxRewardsPerCard 一张卡最多挂几档权益。
//
// 上限不是性能考虑，是可解释性：一张卡发十样东西时，用户在客户端上
// 看到的「兑换成功」后面要跟十行结果，而其中任何一样没到账都很难被发现。
const MaxRewardsPerCard = 8

// MaxFeatureDays 功能解锁的天数上限（0 为永久）。
const MaxFeatureDays = 3650

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
	// Value 取值形态，见 ValueAmount / ValueMoney / ValueRef / ValueFeature。
	Value string `json:"value"`
	Unit  string `json:"unit,omitempty"`
	// Min / Max 对 ValueAmount 与 ValueFeature 的数量有意义。
	Min int64 `json:"min,omitempty"`
	Max int64 `json:"max,omitempty"`
	// Repeatable 同一张卡里可以出现多次（只有功能解锁：一张卡解锁几个不同的功能）。
	Repeatable bool `json:"repeatable,omitempty"`
	// NeedsLoginCard 领取人名下必须有一张仍在授权期内的授权卡，否则拒发。
	//
	// 「加设备位」与「授权时长」是这样：它们都挂在授权卡上，没有卡就没有地方加。
	// 静默成功比报错更糟 —— 用户会以为买到了，而客户端上什么都没变。
	NeedsLoginCard bool `json:"needsLoginCard,omitempty"`
}

// rewardSpecs 顺序即控制台的展示顺序，也是发放与展示的顺序。
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
		Type:           RewardLicenseDays,
		Label:          "授权时长",
		Hint:           "续费卡：给领取人名下的授权卡延长授权期（已过期的从今天起算）；名下没有限时授权卡时拒发",
		Value:          ValueAmount,
		Unit:           "天",
		Min:            1,
		Max:            3650,
		NeedsLoginCard: true,
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
	{
		Type:  RewardNumberChange,
		Label: "改号卡",
		Hint:  "自选一个普通号码；与积分商城、抽奖等渠道发的是同一个账户",
		Value: ValueAmount,
		Unit:  "张",
		Min:   1,
		Max:   1000,
	},
	{
		Type:       RewardNumberTicket,
		Label:      "靓号券",
		Hint:       "按等级自选靓号（1 精品、2 稀有、3 传说、4 至尊），能换不高于其等级的号码；不同等级可各配一项",
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
		Hint:       "头像框、资料卡背景、昵称样式或勋章；天数为 0 即永久，同一件多次兑换时顺延，可以配几件不同的装扮",
		Value:      ValueCosmetic,
		Unit:       "天",
		Min:        0,
		Max:        MaxFeatureDays,
		Repeatable: true,
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

// ValidateRewards 校验一张卡的权益清单（只校验形状；引用的套餐与功能是否存在由服务层查）。
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
	seenFeatures := make([]string, 0, 2)
	seenCosmetics := make([]int64, 0, 2)
	for _, reward := range rewards {
		spec, ok := FindRewardSpec(reward.Type)
		if !ok {
			return fmt.Errorf("未登记的权益类型：%s", reward.Type)
		}
		if !spec.Repeatable && slices.Contains(seen, reward.Type) {
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
			if !reward.Money.Equal(reward.Money.Round(2)) {
				return fmt.Errorf("「%s」最多两位小数", spec.Label)
			}
		case ValueRef:
			if reward.RefID <= 0 {
				return fmt.Errorf("「%s」没有选择具体对象", spec.Label)
			}
		case ValueFeature:
			tag := strings.TrimSpace(reward.Feature)
			if tag == "" {
				return fmt.Errorf("「%s」没有选择功能", spec.Label)
			}
			if !vipdomain.FeatureTagPattern.MatchString(tag) {
				return fmt.Errorf("「%s」的功能标识「%s」无效", spec.Label, tag)
			}
			if slices.Contains(seenFeatures, tag) {
				return fmt.Errorf("「%s」里功能「%s」配置了两次", spec.Label, tag)
			}
			seenFeatures = append(seenFeatures, tag)
			if reward.Amount < spec.Min || reward.Amount > spec.Max {
				return fmt.Errorf("「%s」的天数需要在 %d–%d 之间（0 为永久）", spec.Label, spec.Min, spec.Max)
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
				return fmt.Errorf("「%s」里同一件装扮配置了两次", spec.Label)
			}
			seenCosmetics = append(seenCosmetics, reward.RefID)
			if reward.Amount < spec.Min || reward.Amount > spec.Max {
				return fmt.Errorf("「%s」的天数需要在 %d–%d 之间（0 为永久）", spec.Label, spec.Min, spec.Max)
			}
		}
	}
	return nil
}

// NormalizeRewards 按目录顺序重排权益清单，并抹掉与取值形态无关的字段。
//
// 重排是为了让「同一批卡的权益在任何地方都按同一顺序展示」；抹字段是为了让
// 落库的 JSON 只留下真正生效的那一个 —— 否则一条 {"type":"integral","refId":7}
// 会让读日志的人以为 refId 起了作用。未登记的类型被丢掉（激励广告在此之上补它独有的档位）。
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
			case ValueFeature:
				clean.Feature = strings.ToLower(strings.TrimSpace(reward.Feature))
				clean.Amount = reward.Amount
			case ValueNumberTicket:
				clean.Amount = reward.Amount
				clean.RefID = reward.RefID
			case ValueCosmetic:
				clean.Amount = reward.Amount
				clean.RefID = reward.RefID
			}
			out = append(out, clean)
			if !spec.Repeatable {
				break
			}
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
	case ValueFeature:
		if reward.Amount == 0 {
			return fmt.Sprintf("%s %s 永久", spec.Label, reward.Feature)
		}
		return fmt.Sprintf("%s %s %d %s", spec.Label, reward.Feature, reward.Amount, spec.Unit)
	case ValueNumberTicket:
		return fmt.Sprintf("%s %d %s", usernumber.RightName(usernumber.RightTicket, int(reward.RefID)), reward.Amount, spec.Unit)
	case ValueCosmetic:
		if reward.Amount == 0 {
			return fmt.Sprintf("%s #%d 永久", spec.Label, reward.RefID)
		}
		return fmt.Sprintf("%s #%d %d %s", spec.Label, reward.RefID, reward.Amount, spec.Unit)
	default:
		return spec.Label
	}
}

// RewardsNeedLoginCard 这张卡的权益里是否有挂在授权卡上的那几档。
func RewardsNeedLoginCard(rewards []Reward) bool {
	for _, reward := range rewards {
		if spec, ok := FindRewardSpec(reward.Type); ok && spec.NeedsLoginCard {
			return true
		}
	}
	return false
}

// ValidKind 卡的形态是否合法。
func ValidKind(kind string) bool {
	return kind == KindLogin || kind == KindRedeem
}

// ValidValidityMode 有效期模式是否合法。
func ValidValidityMode(mode string) bool {
	return mode == ValidityPermanent || mode == ValidityFixedUntil || mode == ValidityFromFirstUse
}

// ValidAudience 领取对象是否合法。
func ValidAudience(audience string) bool {
	return audience == AudienceAll || audience == AudienceVip || audience == AudienceNonVip
}

// Kinds / ValidityModes / Charsets / Audiences 顺序即控制台的展示顺序。
func Kinds() []string { return []string{KindRedeem, KindLogin} }
func ValidityModes() []string {
	return []string{ValidityPermanent, ValidityFixedUntil, ValidityFromFirstUse}
}
func Charsets() []string  { return []string{CharsetSafe, CharsetDigits, CharsetLetters} }
func Audiences() []string { return []string{AudienceAll, AudienceVip, AudienceNonVip} }

// 卡面字符集。
//
// safe 剔掉 I / O / 0 / 1：卡密是**要被人抄写的**（贴在卡片上、发在群里、手打进客户端），
// 而这四个字符在绝大多数字体下两两难辨。剩 32 个字符正好整除 256，可以直接对随机字节取模。
// letters 同样剔掉 I / O（24 个），digits 是全部 10 个数字；这两档不整除 256，生成时用拒绝采样。
const (
	alphabetSafe    = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	alphabetDigits  = "0123456789"
	alphabetLetters = "ABCDEFGHJKLMNPQRSTUVWXYZ"
)

// Alphabet 字符集对应的字符表；未知字符集回落到 safe。
func Alphabet(charset string) string {
	switch charset {
	case CharsetDigits:
		return alphabetDigits
	case CharsetLetters:
		return alphabetLetters
	default:
		return alphabetSafe
	}
}

// ValidCharset 字符集是否合法。
func ValidCharset(charset string) bool {
	return charset == CharsetSafe || charset == CharsetDigits || charset == CharsetLetters
}

// 导入卡面的长度约束（不含分隔符的字符数）。
const (
	MinImportedCodeLength = 4
	MaxCodeLength         = 64
)

// NormalizeCode 把用户输入的卡面收敛成库里存的形态。
//
// 抄卡密的人会带上空格、用小写、把分隔符打成下划线或空格、甚至连着打两个分隔符。
// 不归一化的表现是「明明抄对了却提示卡密不存在」，而这是最没法自查的一类错误。
//
// 保留的是 A–Z 与 0–9 全集，**不是** safe 字符集：前缀与导入的卡面里可以有 I / O / 0 / 1
// （防混淆只约束随机段），按 safe 过滤会把 `VIP-…` 悄悄削成 `VP-…`。
func NormalizeCode(code string) string {
	upper := strings.ToUpper(strings.TrimSpace(code))
	var sb strings.Builder
	pendingDash := false
	for _, char := range upper {
		switch {
		case IsCodeRune(char):
			if pendingDash && sb.Len() > 0 {
				sb.WriteByte('-')
			}
			pendingDash = false
			sb.WriteRune(char)
		case char == '-' || char == '_' || char == ' ' || char == '—' || char == '－':
			pendingDash = true
		}
	}
	return sb.String()
}

// IsCodeRune 卡面允许的字符（A–Z 与 0–9）。
func IsCodeRune(char rune) bool {
	return (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')
}

// ValidateImportedCode 校验一张导入的卡面（已经过 NormalizeCode）。
func ValidateImportedCode(code string) error {
	if code == "" {
		return fmt.Errorf("卡面为空")
	}
	if len(code) > MaxCodeLength {
		return fmt.Errorf("「%s」超过 %d 个字符", code, MaxCodeLength)
	}
	if len(strings.ReplaceAll(code, "-", "")) < MinImportedCodeLength {
		return fmt.Errorf("「%s」太短，至少 %d 个字母或数字", code, MinImportedCodeLength)
	}
	return nil
}
