package pointstore

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// 赠好友：兑换人付积分，权益发给他选的另一个人。规则见 docs/points-store-gift.md。
//
// 商品上的 delivery 决定发给谁。赠送商品照常走可兑换判定（库存、限兑、兑换对象、积分都按付款的人算），
// 「已拥有」改按接收人算；在此之上再过一层赠送风控（GiftPolicy + EvaluateGift，纯函数）。
// 扣积分、扣库存、给对方发放在同一个事务里完成；需要审核的先扣积分、记为审核中，
// 管理员通过时再在一个事务里发放，驳回时原路退回。

// 发给谁。
const (
	DeliverySelf = "self" // 兑换人自己
	DeliveryGift = "gift" // 兑换人选的好友
)

// ValidDelivery 枚举校验。
func ValidDelivery(delivery string) bool { return delivery == DeliverySelf || delivery == DeliveryGift }

// RedemptionRevoked 赠送被撤销、权益已追回且没有退回积分的那笔兑换。
const RedemptionRevoked = "revoked"

// 赠送状态。
const (
	GiftDelivered = "delivered" // 已送达
	GiftReviewing = "reviewing" // 审核中：积分已扣，等管理员通过后发放
	GiftRejected  = "rejected"  // 审核驳回：积分已退回
	GiftRevoked   = "revoked"   // 已撤销：权益已从接收人处追回
	GiftBlocked   = "blocked"   // 风控拦截：没扣积分，只留痕
)

// GiftStatuses 全部赠送状态，顺序即控制台的筛选顺序。
func GiftStatuses() []string {
	return []string{GiftReviewing, GiftDelivered, GiftRejected, GiftRevoked, GiftBlocked}
}

// ValidGiftStatus 枚举校验。
func ValidGiftStatus(status string) bool { return slices.Contains(GiftStatuses(), status) }

// 风控处置，从轻到重。
const (
	GiftActionAllow  = "allow"
	GiftActionReview = "review"
	GiftActionReject = "reject"
)

func giftActionRank(action string) int {
	switch action {
	case GiftActionReview:
		return 1
	case GiftActionReject:
		return 2
	}
	return 0
}

// MaxGiftMessageRunes 赠言最长多少字。
const MaxGiftMessageRunes = 60

// GiftableRewardTypes 能做成赠送商品的权益：都是发给接收人之后能逐项追回的。
//
// 余额（等于转账，洗钱的口子）、人工发放（这次重构要去掉的正是它）、经验与抽奖次数、号码权益
// （发出去马上就能用掉，撤销时追不回来）不能送。永久会员套餐也不能送：一次撤销追不回「已经折算掉的存量」。
func GiftableRewardTypes() []string {
	return []string{RewardVipPlan, RewardVipDays, RewardFeature, RewardCosmetic}
}

// ValidateGiftRewards 赠送商品的权益只能是可追回的几类（永久套餐由服务层查套餐后另判）。
func ValidateGiftRewards(rewards []Reward) error {
	for _, reward := range rewards {
		if !slices.Contains(GiftableRewardTypes(), reward.Type) {
			spec, _ := FindRewardSpec(reward.Type)
			label := spec.Label
			if label == "" {
				label = reward.Type
			}
			return fmt.Errorf("「%s」不能用于赠送商品：赠送只支持会员套餐、会员天数、功能解锁与个性装扮", label)
		}
	}
	return nil
}

// ── 风控判据 ──

// 判据键。
const (
	GiftRuleSenderAge           = "sender_age"
	GiftRuleRecipientAge        = "recipient_age"
	GiftRuleSameDevice          = "same_device"
	GiftRuleSameNetwork         = "same_network"
	GiftRuleSenderDailyCount    = "sender_daily_count"
	GiftRuleSenderDailyPoints   = "sender_daily_points"
	GiftRuleRecipientDailyCount = "recipient_daily_count"
	GiftRulePairFrequency       = "pair_frequency"
	GiftRuleMutual              = "mutual"
	GiftRuleLargeGift           = "large_gift"
	GiftRuleRiskPlatform        = "risk_platform"
)

// GiftRuleSpec 一条判据的自述：控制台的风控配置表单与服务端校验都读它。
type GiftRuleSpec struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
	// ThresholdLabel / ThresholdUnit / Min / Max 阈值的含义与范围；ThresholdLabel 为空表示这条判据没有阈值
	ThresholdLabel string `json:"thresholdLabel,omitempty"`
	ThresholdUnit  string `json:"thresholdUnit,omitempty"`
	Min            int64  `json:"min,omitempty"`
	Max            int64  `json:"max,omitempty"`
	// Window 这条判据有时间窗（小时）
	Window    bool  `json:"window,omitempty"`
	MaxWindow int64 `json:"maxWindow,omitempty"`
	// Actions 可选的处置
	Actions []string `json:"actions"`
	Default GiftRule `json:"default"`
}

// GiftRule 一条判据的配置。
type GiftRule struct {
	Key     string `json:"key"`
	Enabled bool   `json:"enabled"`
	Action  string `json:"action"`
	// Threshold 阈值：天数 / 次数 / 积分，含义见 GiftRuleSpec
	Threshold int64 `json:"threshold,omitempty"`
	// WindowHours 时间窗（小时）
	WindowHours int64 `json:"windowHours,omitempty"`
}

const maxGiftWindowHours = 24 * 90

var reviewOrReject = []string{GiftActionReview, GiftActionReject}

// giftRuleSpecs 顺序即控制台的展示顺序，也是命中多条时说给用户听的那一条的优先顺序。
var giftRuleSpecs = []GiftRuleSpec{
	{
		Key: GiftRuleSenderAge, Label: "赠送人注册天数",
		Hint:           "注册不满这么多天的账号不能赠送：新号批量刷积分再送给主号是最常见的套利",
		ThresholdLabel: "至少注册", ThresholdUnit: "天", Min: 1, Max: 365,
		Actions: reviewOrReject,
		Default: GiftRule{Enabled: true, Action: GiftActionReject, Threshold: 7},
	},
	{
		Key: GiftRuleRecipientAge, Label: "接收人注册天数",
		Hint:           "接收人是新注册账号时：小号接收是转移权益的典型手法",
		ThresholdLabel: "至少注册", ThresholdUnit: "天", Min: 1, Max: 365,
		Actions: reviewOrReject,
		Default: GiftRule{Enabled: true, Action: GiftActionReview, Threshold: 3},
	},
	{
		Key: GiftRuleSameDevice, Label: "同一设备",
		Hint:    "两个账号登录或注册时用过同一台设备（登录设备台账与注册台账）：多半是同一个人的大小号",
		Actions: reviewOrReject,
		Default: GiftRule{Enabled: true, Action: GiftActionReject},
	},
	{
		Key: GiftRuleSameNetwork, Label: "同一网络",
		Hint:   "两个账号在时间窗内用过同一个地址登录，或注册地址相同。家人、室友也会命中，默认送审核而不是拒绝",
		Window: true, MaxWindow: maxGiftWindowHours,
		Actions: reviewOrReject,
		Default: GiftRule{Enabled: true, Action: GiftActionReview, WindowHours: 24 * 30},
	},
	{
		Key: GiftRuleSenderDailyCount, Label: "每人每日赠送次数",
		Hint:           "按应用默认时区的自然日计，审核中与已撤销的也算",
		ThresholdLabel: "每日最多", ThresholdUnit: "次", Min: 1, Max: 1000,
		Actions: []string{GiftActionReject},
		Default: GiftRule{Enabled: true, Action: GiftActionReject, Threshold: 3},
	},
	{
		Key: GiftRuleSenderDailyPoints, Label: "每人每日赠送额度",
		Hint:           "当日已送出的积分加上这一笔超过额度时拒绝",
		ThresholdLabel: "每日最多", ThresholdUnit: "积分", Min: 1, Max: MaxCost,
		Actions: []string{GiftActionReject},
		Default: GiftRule{Enabled: true, Action: GiftActionReject, Threshold: 30000},
	},
	{
		Key: GiftRuleRecipientDailyCount, Label: "每人每日接收次数",
		Hint:           "一个账号一天最多收到几份：多个小号集中送给一个人",
		ThresholdLabel: "每日最多", ThresholdUnit: "次", Min: 1, Max: 1000,
		Actions: reviewOrReject,
		Default: GiftRule{Enabled: true, Action: GiftActionReject, Threshold: 3},
	},
	{
		Key: GiftRulePairFrequency, Label: "频繁赠送同一人",
		Hint:           "时间窗内送给同一个人的次数达到上限后再送",
		ThresholdLabel: "窗口内最多", ThresholdUnit: "次", Min: 1, Max: 100,
		Window: true, MaxWindow: maxGiftWindowHours,
		Actions: reviewOrReject,
		Default: GiftRule{Enabled: true, Action: GiftActionReject, Threshold: 1, WindowHours: 24},
	},
	{
		Key: GiftRuleMutual, Label: "互相赠送",
		Hint:   "对方在时间窗内刚送过你：互刷限兑名额或活动折扣",
		Window: true, MaxWindow: maxGiftWindowHours,
		Actions: reviewOrReject,
		Default: GiftRule{Enabled: true, Action: GiftActionReview, WindowHours: 24 * 7},
	},
	{
		Key: GiftRuleLargeGift, Label: "大额赠送",
		Hint:           "单笔赠送的积分达到这个数时送审核",
		ThresholdLabel: "单笔达到", ThresholdUnit: "积分", Min: 1, Max: MaxCost,
		Actions: reviewOrReject,
		Default: GiftRule{Enabled: false, Action: GiftActionReview, Threshold: 10000},
	},
	{
		Key: GiftRuleRiskPlatform, Label: "风险决策平台",
		Hint:    "按风险决策平台的 points_gift 场景评估：平台判拦截或封禁时一律拒绝，判复核时按这里的处置",
		Actions: reviewOrReject,
		Default: GiftRule{Enabled: true, Action: GiftActionReview},
	},
}

// GiftRuleCatalog 全部判据的自述。
func GiftRuleCatalog() []GiftRuleSpec {
	out := make([]GiftRuleSpec, len(giftRuleSpecs))
	copy(out, giftRuleSpecs)
	for i := range out {
		out[i].Default.Key = out[i].Key
	}
	return out
}

// FindGiftRuleSpec 按键取判据。
func FindGiftRuleSpec(key string) (GiftRuleSpec, bool) {
	for _, spec := range giftRuleSpecs {
		if spec.Key == key {
			spec.Default.Key = spec.Key
			return spec, true
		}
	}
	return GiftRuleSpec{}, false
}

// GiftPolicy 应用的赠送风控策略。
type GiftPolicy struct {
	AppID int64 `json:"appid"`
	// Enabled 允许赠送：关掉后赠送商品在商城里照列，但不能提交
	Enabled bool       `json:"enabled"`
	Rules   []GiftRule `json:"rules"`
	// Configured 是否保存过（没保存过时是推荐默认值）
	Configured bool      `json:"configured"`
	UpdatedBy  string    `json:"updatedBy,omitempty"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// DefaultGiftPolicy 推荐默认值。
func DefaultGiftPolicy(appID int64) GiftPolicy {
	policy := GiftPolicy{AppID: appID, Enabled: true, Rules: make([]GiftRule, 0, len(giftRuleSpecs))}
	for _, spec := range giftRuleSpecs {
		rule := spec.Default
		rule.Key = spec.Key
		policy.Rules = append(policy.Rules, rule)
	}
	return policy
}

// Rule 按键取配置（策略里缺的那条按默认值）。
func (p GiftPolicy) Rule(key string) GiftRule {
	for _, rule := range p.Rules {
		if rule.Key == key {
			return rule
		}
	}
	spec, _ := FindGiftRuleSpec(key)
	return spec.Default
}

// NormalizeGiftPolicy 按目录补齐、排序，丢掉未知的键；缺的判据用默认值。
func NormalizeGiftPolicy(policy GiftPolicy) GiftPolicy {
	byKey := make(map[string]GiftRule, len(policy.Rules))
	for _, rule := range policy.Rules {
		rule.Key = strings.TrimSpace(rule.Key)
		rule.Action = strings.TrimSpace(rule.Action)
		byKey[rule.Key] = rule
	}
	rules := make([]GiftRule, 0, len(giftRuleSpecs))
	for _, spec := range giftRuleSpecs {
		rule, ok := byKey[spec.Key]
		if !ok {
			rule = spec.Default
		}
		rule.Key = spec.Key
		if rule.Action == "" {
			rule.Action = spec.Default.Action
		}
		if spec.ThresholdLabel == "" {
			rule.Threshold = 0
		}
		if !spec.Window {
			rule.WindowHours = 0
		}
		rules = append(rules, rule)
	}
	policy.Rules = rules
	return policy
}

// ValidateGiftPolicy 校验清洗后的策略。
func ValidateGiftPolicy(policy GiftPolicy) error {
	for _, rule := range policy.Rules {
		spec, ok := FindGiftRuleSpec(rule.Key)
		if !ok {
			return fmt.Errorf("未知的判据：%s", rule.Key)
		}
		if !slices.Contains(spec.Actions, rule.Action) {
			return fmt.Errorf("「%s」不支持处置 %s", spec.Label, rule.Action)
		}
		if spec.ThresholdLabel != "" && (rule.Threshold < spec.Min || rule.Threshold > spec.Max) {
			return fmt.Errorf("「%s」的阈值需要在 %d–%d %s之间", spec.Label, spec.Min, spec.Max, spec.ThresholdUnit)
		}
		if spec.Window && (rule.WindowHours < 1 || rule.WindowHours > spec.MaxWindow) {
			return fmt.Errorf("「%s」的时间窗需要在 1–%d 小时之间", spec.Label, spec.MaxWindow)
		}
	}
	return nil
}

// ── 判定 ──

// GiftFacts 赠送风控判定所需的事实（仓储层在锁住双方的事务里一次取齐）。
type GiftFacts struct {
	Now time.Time `json:"now"`
	// SenderAgeDays / RecipientAgeDays 注册了多少天（不足一天为 0）
	SenderAgeDays    int64 `json:"senderAgeDays"`
	RecipientAgeDays int64 `json:"recipientAgeDays"`
	// SharedDevices 两个账号共同用过的设备（登录设备台账 + 注册台账），只存打码后的样本
	SharedDevices []string `json:"sharedDevices"`
	// SharedNetworks 两个账号在时间窗内都用过的地址（登录记录 + 注册地址）
	SharedNetworks []string `json:"sharedNetworks"`
	// SenderToday / SenderTodayPoints 赠送人今天已送出的次数与积分（不含驳回、拦截；含审核中与已撤销）
	SenderToday       int64 `json:"senderToday"`
	SenderTodayPoints int64 `json:"senderTodayPoints"`
	// RecipientToday 接收人今天已收到的次数（同上口径）
	RecipientToday int64 `json:"recipientToday"`
	// PairRecent 时间窗内赠送人送给这个接收人的次数
	PairRecent int64 `json:"pairRecent"`
	// MutualRecent 时间窗内接收人送给赠送人的次数
	MutualRecent int64 `json:"mutualRecent"`
	// Cost 这一笔的积分
	Cost int64 `json:"cost"`
	// Platform 风险决策平台的结论（没接入、没评估为空）
	Platform *GiftPlatformVerdict `json:"platform,omitempty"`
}

// GiftPlatformVerdict 风险决策平台对这次赠送的结论。
type GiftPlatformVerdict struct {
	DecisionID string   `json:"decisionId,omitempty"`
	Action     string   `json:"action"`
	Score      int      `json:"score"`
	Reasons    []string `json:"reasons,omitempty"`
}

// GiftHit 命中的一条判据。Message 说给用户听（拒绝时原样返回），Detail 给管理员看。
type GiftHit struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Action  string `json:"action"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

// GiftVerdict 判定结论：取命中判据里最重的处置。
type GiftVerdict struct {
	Action string    `json:"action"`
	Hits   []GiftHit `json:"hits"`
}

// RejectHit 拒绝时说给用户听的那一条（按目录顺序第一条拒绝的）。
func (v GiftVerdict) RejectHit() *GiftHit {
	for i := range v.Hits {
		if v.Hits[i].Action == GiftActionReject {
			return &v.Hits[i]
		}
	}
	return nil
}

// EvaluateGift 赠送风控判定。纯函数。
//
// 每条启用的判据独立判；命中多条时取最重的处置（拒绝 > 审核 > 放行），全部命中都记下来给管理员看。
// 风险决策平台判拦截或封禁时一律拒绝，不看这里配的处置：平台的拦截是全站口径。
func EvaluateGift(policy GiftPolicy, facts GiftFacts) GiftVerdict {
	verdict := GiftVerdict{Action: GiftActionAllow, Hits: []GiftHit{}}
	hit := func(rule GiftRule, action, message, detail string) {
		spec, _ := FindGiftRuleSpec(rule.Key)
		verdict.Hits = append(verdict.Hits, GiftHit{Key: rule.Key, Label: spec.Label, Action: action, Message: message, Detail: detail})
		if giftActionRank(action) > giftActionRank(verdict.Action) {
			verdict.Action = action
		}
	}
	for _, spec := range giftRuleSpecs {
		rule := policy.Rule(spec.Key)
		if !rule.Enabled {
			continue
		}
		switch spec.Key {
		case GiftRuleSenderAge:
			if facts.SenderAgeDays < rule.Threshold {
				hit(rule, rule.Action, "账号注册满 "+itoa(rule.Threshold)+" 天后才能赠送",
					"赠送人注册 "+itoa(facts.SenderAgeDays)+" 天")
			}
		case GiftRuleRecipientAge:
			if facts.RecipientAgeDays < rule.Threshold {
				hit(rule, rule.Action, "对方账号注册不满 "+itoa(rule.Threshold)+" 天，暂时不能接收赠送",
					"接收人注册 "+itoa(facts.RecipientAgeDays)+" 天")
			}
		case GiftRuleSameDevice:
			if len(facts.SharedDevices) > 0 {
				hit(rule, rule.Action, "不能赠送给与你使用同一台设备的账号",
					"共用设备 "+strings.Join(facts.SharedDevices, "、"))
			}
		case GiftRuleSameNetwork:
			if len(facts.SharedNetworks) > 0 {
				hit(rule, rule.Action, "不能赠送给与你使用同一网络的账号",
					"共用地址 "+strings.Join(facts.SharedNetworks, "、"))
			}
		case GiftRuleSenderDailyCount:
			if facts.SenderToday >= rule.Threshold {
				hit(rule, rule.Action, "今日赠送次数已达上限（"+itoa(rule.Threshold)+" 次）",
					"今日已送 "+itoa(facts.SenderToday)+" 次")
			}
		case GiftRuleSenderDailyPoints:
			if facts.SenderTodayPoints+facts.Cost > rule.Threshold {
				hit(rule, rule.Action, "今日赠送额度已达上限（"+itoa(rule.Threshold)+" 积分）",
					"今日已送 "+itoa(facts.SenderTodayPoints)+" 积分，本笔 "+itoa(facts.Cost))
			}
		case GiftRuleRecipientDailyCount:
			if facts.RecipientToday >= rule.Threshold {
				hit(rule, rule.Action, "对方今日接收的赠送已达上限",
					"接收人今日已收 "+itoa(facts.RecipientToday)+" 份")
			}
		case GiftRulePairFrequency:
			if facts.PairRecent >= rule.Threshold {
				hit(rule, rule.Action, itoa(rule.WindowHours)+" 小时内送给同一人的次数已达上限，请稍后再送",
					itoa(rule.WindowHours)+" 小时内已送过 "+itoa(facts.PairRecent)+" 次")
			}
		case GiftRuleMutual:
			if facts.MutualRecent > 0 {
				hit(rule, rule.Action, "对方近期刚送过你，暂时不能回赠",
					itoa(rule.WindowHours)+" 小时内对方送过 "+itoa(facts.MutualRecent)+" 次")
			}
		case GiftRuleLargeGift:
			if facts.Cost >= rule.Threshold {
				hit(rule, rule.Action, "单笔赠送积分较多，需要审核", "本笔 "+itoa(facts.Cost)+" 积分")
			}
		case GiftRuleRiskPlatform:
			if facts.Platform == nil {
				continue
			}
			detail := "平台判定 " + facts.Platform.Action + "，评分 " + strconv.Itoa(facts.Platform.Score)
			if len(facts.Platform.Reasons) > 0 {
				detail += "：" + strings.Join(facts.Platform.Reasons, "；")
			}
			switch facts.Platform.Action {
			case "block", "ban":
				hit(rule, GiftActionReject, "当前环境存在风险，暂时无法赠送", detail)
			case "review", "captcha":
				hit(rule, rule.Action, "当前环境存在风险，暂时无法赠送", detail)
			}
		}
	}
	return verdict
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// ── 赠送的硬性条件（不可配置） ──

// 赠送被拒的原因（客户端按 reason 分支，不要匹配 message）。可兑换判定的原因（库存、限兑、积分……）照旧。
const (
	GiftReasonDisabled          = "gift_disabled"
	GiftReasonNotGiftItem       = "not_gift_item"
	GiftReasonGiftItem          = "gift_item"
	GiftReasonRecipientNotFound = "recipient_not_found"
	GiftReasonSelf              = "self"
	GiftReasonRecipientInactive = "recipient_inactive"
	GiftReasonRecipientOwned    = "recipient_owned"
	GiftReasonRisk              = "risk"
	GiftReasonLimit             = "gift_limit"
	GiftReasonFrequency         = "gift_frequency"
)

var giftReasonMessages = map[string]string{
	GiftReasonDisabled:          "赠送功能暂未开放",
	GiftReasonNotGiftItem:       "这件商品不能赠送",
	GiftReasonGiftItem:          "这件商品需要选择赠送对象",
	GiftReasonRecipientNotFound: "没有找到这个用户，请核对号码",
	GiftReasonSelf:              "不能赠送给自己",
	GiftReasonRecipientInactive: "对方账号状态异常，暂时不能接收赠送",
	GiftReasonRecipientOwned:    "对方已拥有该权益，无需赠送",
	GiftReasonRisk:              "当前环境存在风险，暂时无法赠送",
	GiftReasonLimit:             "赠送次数已达上限",
	GiftReasonFrequency:         "赠送过于频繁，请稍后再试",
}

// GiftReasonMessage 原因对应的说明。
func GiftReasonMessage(reason string) string { return giftReasonMessages[reason] }

// GiftHitReason 风控命中的判据归到哪类原因（决定错误码）。
func GiftHitReason(key string) string {
	switch key {
	case GiftRuleSenderDailyCount, GiftRuleSenderDailyPoints, GiftRuleRecipientDailyCount:
		return GiftReasonLimit
	case GiftRulePairFrequency, GiftRuleMutual:
		return GiftReasonFrequency
	}
	return GiftReasonRisk
}

// NormalizeGiftMessage 赠言：去空白、压掉换行，超长报错。
func NormalizeGiftMessage(message string) (string, error) {
	message = strings.Join(strings.Fields(strings.TrimSpace(message)), " ")
	if utf8.RuneCountInString(message) > MaxGiftMessageRunes {
		return "", fmt.Errorf("赠言不能超过 %d 字", MaxGiftMessageRunes)
	}
	return message, nil
}

// MaskDevice 设备标识打码：只留首尾各 4 位，给管理员看「是同一台」就够了。
func MaskDevice(device string) string {
	device = strings.TrimSpace(device)
	if utf8.RuneCountInString(device) <= 8 {
		return device
	}
	runes := []rune(device)
	return string(runes[:4]) + "…" + string(runes[len(runes)-4:])
}

// ── 记录 ──

// GiftUser 赠送记录里的一方。用户端只给对外 ID、号码、昵称与头像。
type GiftUser struct {
	// UserID 主键，只在管理端给
	UserID   int64  `json:"userId,omitempty"`
	PublicID int64  `json:"publicId,omitempty"`
	Number   int64  `json:"number,omitempty"`
	Account  string `json:"account,omitempty"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
	// RawAvatar 库里的原值，传输层据此换成可访问的地址
	RawAvatar string `json:"-"`
}

// GiftRevocationItem 撤销时一项权益的追回结果。
type GiftRevocationItem struct {
	Type   string `json:"type"`
	Label  string `json:"label"`
	Detail string `json:"detail"`
	// Outcome recovered 已追回 / expired 已过期，无可追回 / missing 找不到发放记录（已被收回或折算）
	Outcome string `json:"outcome"`
}

// 追回结果。
const (
	RevokeRecovered = "recovered"
	RevokeExpired   = "expired"
	RevokeMissing   = "missing"
)

// Gift 一次赠送。
type Gift struct {
	ID           int64          `json:"id"`
	GiftNo       string         `json:"giftNo"`
	AppID        int64          `json:"appid"`
	RedemptionID *int64         `json:"redemptionId,omitempty"`
	RedemptionNo string         `json:"redemptionNo,omitempty"`
	Sender       GiftUser       `json:"sender"`
	Recipient    GiftUser       `json:"recipient"`
	ItemID       *int64         `json:"itemId,omitempty"`
	ItemTitle    string         `json:"itemTitle"`
	Cost         int64          `json:"cost"`
	Rewards      []Reward       `json:"rewards,omitempty"`
	Results      []RewardResult `json:"results"`
	Message      string         `json:"message,omitempty"`
	Status       string         `json:"status"`
	RiskAction   string         `json:"riskAction,omitempty"`
	RiskHits     []GiftHit      `json:"riskHits,omitempty"`
	RiskFacts    *GiftFacts     `json:"riskFacts,omitempty"`
	DecisionID   string         `json:"decisionId,omitempty"`
	// StatusMessage 给用户看的一句话（拦截原因、审核说明、驳回或撤销的备注）
	StatusMessage       string               `json:"statusMessage,omitempty"`
	RequestID           string               `json:"requestId,omitempty"`
	DeviceID            string               `json:"deviceId,omitempty"`
	ClientIP            string               `json:"clientIp,omitempty"`
	ReviewNote          string               `json:"reviewNote,omitempty"`
	ReviewedBy          string               `json:"reviewedBy,omitempty"`
	ReviewedAt          *time.Time           `json:"reviewedAt,omitempty"`
	RevokeNote          string               `json:"revokeNote,omitempty"`
	RevokedBy           string               `json:"revokedBy,omitempty"`
	RevokedAt           *time.Time           `json:"revokedAt,omitempty"`
	Revocation          []GiftRevocationItem `json:"revocation,omitempty"`
	RefundTransactionNo string               `json:"refundTransactionNo,omitempty"`
	CreatedAt           time.Time            `json:"createdAt"`
	UpdatedAt           time.Time            `json:"updatedAt"`
	// Direction 用户端：sent 我送出的 / received 我收到的
	Direction string `json:"direction,omitempty"`
}

// GiftInput 一次赠送指令。
type GiftInput struct {
	AppID       int64
	SenderID    int64
	RecipientID int64
	ItemID      int64
	Message     string
	RequestID   string
	DeviceID    string
	ClientIP    string
	UserAgent   string
	Windows     Windows
	Location    *time.Location
	Policy      GiftPolicy
	// Platform 风险决策平台的结论（事务外评估好带进来）
	Platform *GiftPlatformVerdict
}

// GiftResult 赠送结果。
type GiftResult struct {
	Gift Gift `json:"gift"`
	// Balance 赠送后的积分余额
	Balance int64 `json:"balance"`
	// Replayed 命中幂等键，没有再扣积分
	Replayed bool `json:"replayed,omitempty"`
}

// GiftQuery 赠送记录查询。
type GiftQuery struct {
	AppID int64
	// UserID 作为赠送人或接收人；SenderID / RecipientID 只看一侧
	UserID      int64
	SenderID    int64
	RecipientID int64
	ItemID      int64
	Status      string
	Keyword     string
	From        *time.Time
	To          *time.Time
	Page        int
	Limit       int
}

// GiftSummary 管理端按状态的笔数。
type GiftSummary struct {
	Reviewing int64 `json:"reviewing"`
	Delivered int64 `json:"delivered"`
	Rejected  int64 `json:"rejected"`
	Revoked   int64 `json:"revoked"`
	Blocked   int64 `json:"blocked"`
	// TodayDelivered / TodayPoints 今天送达的笔数与积分
	TodayDelivered int64 `json:"todayDelivered"`
	TodayPoints    int64 `json:"todayPoints"`
}

// GiftReviewInput 管理员审核或撤销。
type GiftReviewInput struct {
	AppID  int64
	GiftID int64
	Note   string
	// RefundSender 撤销时是否把积分退给赠送人（误送、客诉时退；套利、欺诈时不退）
	RefundSender bool
	Operator     string
}

// GiftCandidate 选人时的一个候选：确认昵称与头像后再提交。
type GiftCandidate struct {
	GiftUser
	// Relation 为什么出现在这里：recent_sent 最近送过 / recent_received 最近送过你 / inviter 邀请你的人 /
	// invitee 你邀请的人 / search 按号码找到
	Relation string `json:"relation"`
	// LastAt 最近一次互动
	LastAt *time.Time `json:"lastAt,omitempty"`
}

// GiftPreview 提交前的核对结果：对方是谁、能不能送、不能送的原因。
type GiftPreview struct {
	Recipient GiftCandidate `json:"recipient"`
	Allowed   bool          `json:"allowed"`
	Reason    string        `json:"reason,omitempty"`
	Message   string        `json:"message,omitempty"`
	// Rewards 对方会收到什么
	Rewards []ClientReward `json:"rewards"`
}
