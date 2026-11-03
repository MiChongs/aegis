package growth

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// SourceSpec 一种受管的收入来源。Default 为 true 的来源在规则没有列出来源时也受管。
type SourceSpec struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Default     bool   `json:"default"`
	// Kinds 这个来源会发哪些数值
	Kinds []Kind `json:"kinds"`
}

// 受管来源的键与流水的 source_type 一致（签到没有流水，用 signin）。
const (
	SourceSignin      = "signin"
	SourceRewardedAd  = "rewarded_ad"
	SourceLottery     = "lottery"
	SourceInvite      = "invite"
	SourceLeaderboard = "leaderboard"
	SourceItemGrant   = "item_grant"
	SourceCardKey     = "card_key"
	SourcePointsStore = "points_store"
)

var sourceSpecs = []SourceSpec{
	{Key: SourceSignin, Label: "每日签到", Description: "签到奖励（含等级签到加成之后的数值）", Default: true, Kinds: []Kind{KindIntegral, KindExperience}},
	{Key: SourceRewardedAd, Label: "激励广告", Description: "看完激励视频按场景发放的积分与经验", Default: true, Kinds: []Kind{KindIntegral, KindExperience}},
	{Key: SourceLottery, Label: "抽奖", Description: "抽中的积分与经验奖品", Default: true, Kinds: []Kind{KindIntegral, KindExperience}},
	{Key: SourceInvite, Label: "邀请", Description: "邀请与被邀请奖励、推广档位奖励", Default: true, Kinds: []Kind{KindIntegral, KindExperience}},
	{Key: SourceLeaderboard, Label: "排行榜", Description: "排行榜结算奖励", Default: true, Kinds: []Kind{KindIntegral, KindExperience}},
	{Key: SourceItemGrant, Label: "物品发放", Description: "物品发放规则、等级达成奖励与礼包里的积分与经验", Default: true, Kinds: []Kind{KindIntegral, KindExperience}},
	{Key: SourceCardKey, Label: "卡密", Description: "兑换卡赠送的积分与经验（卡密常为售卖品，默认不受管）", Default: false, Kinds: []Kind{KindIntegral, KindExperience}},
	{Key: SourcePointsStore, Label: "积分商城", Description: "用积分兑换的经验包（用户已付积分，默认不受管）", Default: false, Kinds: []Kind{KindExperience}},
}

// Sources 全部受管来源。
func Sources() []SourceSpec { return slices.Clone(sourceSpecs) }

// FindSource 按键找来源。
func FindSource(key string) (SourceSpec, bool) {
	for _, spec := range sourceSpecs {
		if spec.Key == key {
			return spec, true
		}
	}
	return SourceSpec{}, false
}

// SourceFor 一笔入账属于哪个受管来源；返回空串表示不受管（管理员调整、充值、会员赠送、折算、退回……）。
// refund 为 true 表示这是一笔退回（如积分商城取消兑换退回积分），任何来源的退回都不受管。
func SourceFor(kind Kind, sourceType string, refund bool) string {
	if refund {
		return ""
	}
	spec, ok := FindSource(sourceType)
	if !ok || spec.Key == SourceSignin {
		// 签到不走流水入口，由签到事务直接调用
		return ""
	}
	if !slices.Contains(spec.Kinds, kind) {
		return ""
	}
	return spec.Key
}

// RoundingSpec 取整方式说明（控制台下拉）。
type RoundingSpec struct {
	Key         Rounding `json:"key"`
	Label       string   `json:"label"`
	Description string   `json:"description"`
}

// Roundings 全部取整方式。
func Roundings() []RoundingSpec {
	return []RoundingSpec{
		{Key: RoundFloor, Label: "向下取整", Description: "不足 1 的部分舍去，处罚时最常用"},
		{Key: RoundNearest, Label: "四舍五入", Description: "小数部分满 0.5 进 1"},
		{Key: RoundCeil, Label: "向上取整", Description: "只要原本有收入就至少得 1"},
		{Key: RoundRandom, Label: "按概率进位", Description: "小数部分作为进 1 的概率，长期期望与倍率完全一致，适合小额收入"},
	}
}

// Preset 常用倍率预设（控制台快捷按钮）。
type Preset struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Numerator   int    `json:"numerator"`
	Denominator int    `json:"denominator"`
	Effect      string `json:"effect"`
}

// Presets 常用倍率。
func Presets() []Preset {
	return []Preset{
		{Key: "freeze", Label: "冻结", Numerator: 0, Denominator: 1, Effect: EffectFreeze},
		{Key: "div10", Label: "÷10", Numerator: 1, Denominator: 10, Effect: EffectPenalty},
		{Key: "div4", Label: "÷4", Numerator: 1, Denominator: 4, Effect: EffectPenalty},
		{Key: "div2", Label: "÷2", Numerator: 1, Denominator: 2, Effect: EffectPenalty},
		{Key: "x1", Label: "×1", Numerator: 1, Denominator: 1, Effect: EffectNeutral},
		{Key: "x1_5", Label: "×1.5", Numerator: 3, Denominator: 2, Effect: EffectBoost},
		{Key: "x2", Label: "×2", Numerator: 2, Denominator: 1, Effect: EffectBoost},
		{Key: "x3", Label: "×3", Numerator: 3, Denominator: 1, Effect: EffectBoost},
		{Key: "x5", Label: "×5", Numerator: 5, Denominator: 1, Effect: EffectBoost},
	}
}

// ValidationError 输入不合法，Message 直接给到调用方。
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

// Normalize 校验并规整一条规则输入：去重排序来源、约分倍率、裁剪文本。
func (in *RuleInput) Normalize(now time.Time) error {
	if in.AppID <= 0 {
		return invalid("缺少应用")
	}
	if in.UserID < 0 {
		return invalid("用户 ID 无效")
	}
	if !in.Kind.Valid() {
		return invalid("类型只能是积分或经验")
	}
	if in.Denominator == 0 {
		in.Denominator = 1
	}
	if in.Numerator < 0 || in.Numerator > MaxFactorTerm {
		return invalid("倍率分子需要在 0–%d 之间", MaxFactorTerm)
	}
	if in.Denominator < 1 || in.Denominator > MaxFactorTerm {
		return invalid("倍率分母需要在 1–%d 之间", MaxFactorTerm)
	}
	if in.Numerator > 0 {
		g := gcd(in.Numerator, in.Denominator)
		in.Numerator, in.Denominator = in.Numerator/g, in.Denominator/g
	} else {
		in.Denominator = 1
	}
	if in.Rounding == "" {
		in.Rounding = RoundFloor
	}
	if !in.Rounding.Valid() {
		return invalid("不支持的取整方式")
	}
	if in.DailyCap < 0 || in.DailyCap > MaxDailyCap {
		return invalid("每日上限需要在 0–%d 之间（0 为不限）", MaxDailyCap)
	}
	if in.Numerator == in.Denominator && in.DailyCap == 0 {
		return invalid("倍率为 ×1 且不设每日上限的规则没有任何作用")
	}
	sources := make([]string, 0, len(in.Sources))
	for _, raw := range in.Sources {
		key := strings.TrimSpace(raw)
		if key == "" {
			continue
		}
		spec, ok := FindSource(key)
		if !ok {
			return invalid("未知的来源：%s", key)
		}
		if !slices.Contains(spec.Kinds, in.Kind) {
			return invalid("来源「%s」不发%s", spec.Label, in.Kind.Label())
		}
		if !slices.Contains(sources, key) {
			sources = append(sources, key)
		}
	}
	slices.Sort(sources)
	in.Sources = sources
	if in.StartsAt != nil && in.EndsAt != nil && !in.EndsAt.After(*in.StartsAt) {
		return invalid("结束时间需要晚于开始时间")
	}
	if in.EndsAt != nil && !in.EndsAt.After(now) {
		return invalid("结束时间已经过去")
	}
	in.Label = strings.TrimSpace(in.Label)
	if utf8.RuneCountInString(in.Label) > MaxLabelRunes {
		return invalid("名称不能超过 %d 个字", MaxLabelRunes)
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if !utf8.ValidString(in.Reason) || utf8.RuneCountInString(in.Reason) > MaxReasonRunes {
		return invalid("原因不能超过 %d 个字", MaxReasonRunes)
	}
	return nil
}

// Describe 一句话描述规则，写进调控记录与通知。
func (in *RuleInput) Describe() string {
	parts := []string{in.Kind.Label() + " " + Factor{Num: int64(in.Numerator), Den: int64(in.Denominator)}.String()}
	if in.DailyCap > 0 {
		parts = append(parts, fmt.Sprintf("每日上限 %d", in.DailyCap))
	}
	if len(in.Sources) > 0 {
		labels := make([]string, 0, len(in.Sources))
		for _, key := range in.Sources {
			if spec, ok := FindSource(key); ok {
				labels = append(labels, spec.Label)
			}
		}
		parts = append(parts, "仅"+strings.Join(labels, "、"))
	}
	if in.EndsAt != nil {
		parts = append(parts, "至 "+in.EndsAt.Format("2006-01-02 15:04"))
	}
	return strings.Join(parts, "，")
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	if a < 0 {
		return -a
	}
	return a
}
