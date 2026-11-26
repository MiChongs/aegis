package aigateway

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// 计费方案：时段价格、会员档位优惠与倍率分解。规格见 docs/ai-billing.md。
//
// 这里全是纯函数：暂扣事务、用户总览、模型价目、控制台的价格模拟读同一份规则，
// 不会出现「页面上写着夜间半价、扣费却按原价」的分歧。

// ── 时段价格 ──

// 时段的展示色调。只影响展示，不影响计价。
const (
	ToneNormal  = "normal"
	ToneOffPeak = "off_peak"
	TonePeak    = "peak"
	TonePromo   = "promo"
)

// 会员折扣与时段折扣的叠加方式。
const (
	// StackMultiply 两者相乘（默认）。
	StackMultiply = "multiply"
	// StackBest 时段打折时只取会员与时段里更低的那个倍率；时段加价（> 1）时仍相乘。
	StackBest = "best"
)

// MaxTimeWindows 一个应用最多配置多少个时段。
const MaxTimeWindows = 32

// TimeWindow 一个价格时段。时间按设置的时区，半开区间 [Start, End)。
type TimeWindow struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Tone string `json:"tone"`
	// Weekdays 1 = 周一 … 7 = 周日，空表示每天。跨零点的时段按开始那一天判星期。
	Weekdays []int `json:"weekdays"`
	// Start / End 为 HH:MM。End ≤ Start 表示跨零点，Start = End 表示全天。
	Start      string          `json:"start"`
	End        string          `json:"end"`
	Multiplier decimal.Decimal `json:"multiplier"`
	// Models 对外模型名、Kinds 模型种类，空表示全部；都填时同时满足。
	Models []string `json:"models"`
	Kinds  []string `json:"kinds"`
	// From / To 活动日期（YYYY-MM-DD，含当天），按时段开始那一天判断，可空。
	From    *string `json:"from"`
	To      *string `json:"to"`
	Enabled bool    `json:"enabled"`

	startMin, endMin int
}

// TimePricing 时段价格设置。
type TimePricing struct {
	Enabled  bool         `json:"enabled"`
	Stacking string       `json:"stacking"`
	Windows  []TimeWindow `json:"windows"`
}

// TimeMatch 命中的时段。
type TimeMatch struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Tone       string          `json:"tone"`
	Multiplier decimal.Decimal `json:"multiplier"`
	// EndsAt 这个时段（对同一范围）结束、价格变化的时刻；一周内不变时为空。
	EndsAt *time.Time `json:"endsAt,omitempty"`
}

// TimelineSegment 时间轴上价格不变的一段。
type TimelineSegment struct {
	Start      time.Time       `json:"start"`
	End        time.Time       `json:"end"`
	WindowID   string          `json:"windowId"`
	Name       string          `json:"name"`
	Tone       string          `json:"tone"`
	Multiplier decimal.Decimal `json:"multiplier"`
}

var windowIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

func parseClock(value string) (int, bool) {
	value = strings.TrimSpace(value)
	parts := strings.Split(value, ":")
	if len(parts) != 2 || len(parts[0]) != 2 || len(parts[1]) != 2 {
		return 0, false
	}
	hour, err1 := strconv.Atoi(parts[0])
	minute, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, false
	}
	return hour*60 + minute, true
}

func validDate(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil, nil
	}
	if _, err := time.Parse("2006-01-02", trimmed); err != nil {
		return nil, invalid("活动日期「%s」应为 YYYY-MM-DD", trimmed)
	}
	return &trimmed, nil
}

// normalizeTimePricing 校验并规整时段设置。
func normalizeTimePricing(in TimePricing) (TimePricing, error) {
	out := in
	out.Stacking = strings.TrimSpace(out.Stacking)
	if out.Stacking == "" {
		out.Stacking = StackMultiply
	}
	if out.Stacking != StackMultiply && out.Stacking != StackBest {
		return out, invalid("叠加方式只能是 multiply 或 best")
	}
	if len(out.Windows) > MaxTimeWindows {
		return out, invalid("时段最多 %d 个", MaxTimeWindows)
	}
	ids := map[string]bool{}
	windows := make([]TimeWindow, 0, len(out.Windows))
	for i, raw := range out.Windows {
		window := raw
		window.Name = strings.TrimSpace(window.Name)
		if window.Name == "" || len([]rune(window.Name)) > 24 {
			return out, invalid("第 %d 个时段的名称应为 1-24 个字", i+1)
		}
		window.ID = strings.TrimSpace(window.ID)
		if window.ID == "" {
			for n := i + 1; ; n++ {
				candidate := fmt.Sprintf("w%d", n)
				if !ids[candidate] && !slices.ContainsFunc(out.Windows, func(w TimeWindow) bool { return strings.TrimSpace(w.ID) == candidate }) {
					window.ID = candidate
					break
				}
			}
		}
		if !windowIDPattern.MatchString(window.ID) {
			return out, invalid("时段「%s」的编号只能是 1-32 位字母、数字、- 或 _", window.Name)
		}
		if ids[window.ID] {
			return out, invalid("时段编号「%s」重复", window.ID)
		}
		ids[window.ID] = true
		window.Tone = strings.TrimSpace(window.Tone)
		if window.Tone == "" {
			window.Tone = ToneNormal
		}
		if !slices.Contains([]string{ToneNormal, ToneOffPeak, TonePeak, TonePromo}, window.Tone) {
			return out, invalid("时段「%s」的色调只能是 normal、off_peak、peak 或 promo", window.Name)
		}
		start, ok := parseClock(window.Start)
		if !ok {
			return out, invalid("时段「%s」的开始时间应为 HH:MM", window.Name)
		}
		end, ok := parseClock(window.End)
		if !ok {
			return out, invalid("时段「%s」的结束时间应为 HH:MM", window.Name)
		}
		window.Start, window.End = fmt.Sprintf("%02d:%02d", start/60, start%60), fmt.Sprintf("%02d:%02d", end/60, end%60)
		days := make([]int, 0, len(window.Weekdays))
		for _, day := range window.Weekdays {
			if day < 1 || day > 7 {
				return out, invalid("时段「%s」的星期应在 1-7 之间", window.Name)
			}
			if !slices.Contains(days, day) {
				days = append(days, day)
			}
		}
		slices.Sort(days)
		if len(days) == 7 {
			days = []int{}
		}
		window.Weekdays = days
		if window.Multiplier.LessThan(decimal.RequireFromString("0.01")) || window.Multiplier.GreaterThan(maxMultiple) {
			return out, invalid("时段「%s」的倍率应在 0.01-100 之间", window.Name)
		}
		window.Models = normalizeNames(window.Models)
		kinds := normalizeNames(window.Kinds)
		for _, kind := range kinds {
			if !ValidKind(kind) {
				return out, invalid("时段「%s」的模型种类「%s」无效", window.Name, kind)
			}
		}
		window.Kinds = kinds
		from, err := validDate(window.From)
		if err != nil {
			return out, err
		}
		to, err := validDate(window.To)
		if err != nil {
			return out, err
		}
		if from != nil && to != nil && *to < *from {
			return out, invalid("时段「%s」的活动结束日期早于开始日期", window.Name)
		}
		window.From, window.To = from, to
		windows = append(windows, window)
	}
	out.Windows = windows
	return out, nil
}

func normalizeNames(in []string) []string {
	out := make([]string, 0, len(in))
	for _, raw := range in {
		name := strings.TrimSpace(raw)
		if name != "" && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}

// prepared 解析好时刻的副本（设置从 JSON 读出时没有经过 normalize）。
func (w TimeWindow) prepared() (TimeWindow, bool) {
	start, ok1 := parseClock(w.Start)
	end, ok2 := parseClock(w.End)
	if !ok1 || !ok2 || w.Multiplier.Sign() <= 0 {
		return w, false
	}
	w.startMin, w.endMin = start, end
	return w, true
}

// appliesTo 时段是否作用于这个模型。model 与 kind 都为空表示「全部模型」的口径：只有不限模型、不限种类的时段算数。
func (w TimeWindow) appliesTo(model, kind string) bool {
	if model == "" && kind == "" {
		return len(w.Models) == 0 && len(w.Kinds) == 0
	}
	if len(w.Models) > 0 && !slices.Contains(w.Models, model) {
		return false
	}
	if len(w.Kinds) > 0 && !slices.Contains(w.Kinds, kind) {
		return false
	}
	return true
}

func isoWeekday(t time.Time) int {
	day := int(t.Weekday())
	if day == 0 {
		return 7
	}
	return day
}

// dayAllowed 以 day（某个本地日期）为开始日的这一段是否生效：星期与活动日期。
func (w TimeWindow) dayAllowed(day time.Time) bool {
	if len(w.Weekdays) > 0 && !slices.Contains(w.Weekdays, isoWeekday(day)) {
		return false
	}
	date := day.Format("2006-01-02")
	if w.From != nil && date < *w.From {
		return false
	}
	if w.To != nil && date > *w.To {
		return false
	}
	return true
}

// covers 时段是否覆盖 local（已换到设置时区）这一刻。
func (w TimeWindow) covers(local time.Time) bool {
	minute := local.Hour()*60 + local.Minute()
	switch {
	case w.startMin == w.endMin:
		return w.dayAllowed(local)
	case w.startMin < w.endMin:
		return minute >= w.startMin && minute < w.endMin && w.dayAllowed(local)
	default: // 跨零点
		if minute >= w.startMin {
			return w.dayAllowed(local)
		}
		if minute < w.endMin {
			return w.dayAllowed(local.AddDate(0, 0, -1))
		}
		return false
	}
}

// matchAt 此刻对这个模型生效的时段（列表靠前者优先）。
func (p TimePricing) matchAt(model, kind string, local time.Time) (TimeWindow, bool) {
	if !p.Enabled {
		return TimeWindow{}, false
	}
	for _, raw := range p.Windows {
		if !raw.Enabled || !raw.appliesTo(model, kind) {
			continue
		}
		window, ok := raw.prepared()
		if !ok {
			continue
		}
		if window.covers(local) {
			return window, true
		}
	}
	return TimeWindow{}, false
}

// boundaries [from, to] 之间所有可能变价的时刻：各时段在每天的开始与结束、每天零点。
func (p TimePricing) boundaries(model, kind string, from, to time.Time, loc *time.Location) []time.Time {
	localFrom := from.In(loc)
	dayStart := time.Date(localFrom.Year(), localFrom.Month(), localFrom.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -1)
	var out []time.Time
	for day := dayStart; !day.After(to); day = day.AddDate(0, 0, 1) {
		out = append(out, day)
		for _, raw := range p.Windows {
			if !raw.Enabled || !raw.appliesTo(model, kind) {
				continue
			}
			window, ok := raw.prepared()
			if !ok || window.startMin == window.endMin {
				continue
			}
			out = append(out, day.Add(time.Duration(window.startMin)*time.Minute))
			end := day.Add(time.Duration(window.endMin) * time.Minute)
			if window.endMin < window.startMin {
				end = end.AddDate(0, 0, 1)
			}
			out = append(out, end)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return slices.CompactFunc(out, func(a, b time.Time) bool { return a.Equal(b) })
}

func windowKey(window TimeWindow, ok bool) string {
	if !ok {
		return ""
	}
	return window.ID + "|" + window.Multiplier.String()
}

// nextChange now 之后第一次换时段（或换倍率）的时刻；horizon 内不变时为空。
func (p TimePricing) nextChange(model, kind string, now time.Time, loc *time.Location, horizon time.Duration) *time.Time {
	if !p.Enabled {
		return nil
	}
	current := windowKey(p.matchAt(model, kind, now.In(loc)))
	limit := now.Add(horizon)
	for _, candidate := range p.boundaries(model, kind, now, limit, loc) {
		if !candidate.After(now) || candidate.After(limit) {
			continue
		}
		if windowKey(p.matchAt(model, kind, candidate.In(loc))) != current {
			at := candidate
			return &at
		}
	}
	return nil
}

// Match 此刻对这个模型生效的时段；model 与 kind 都为空时按「全部模型」的口径。
func (s Settings) Match(model, kind string, now time.Time) *TimeMatch {
	loc := s.Location()
	window, ok := s.TimePricing.matchAt(model, kind, now.In(loc))
	if !ok {
		return nil
	}
	return &TimeMatch{ID: window.ID, Name: window.Name, Tone: window.Tone, Multiplier: window.Multiplier,
		EndsAt: s.TimePricing.nextChange(model, kind, now, loc, 8*24*time.Hour)}
}

// NextChange now 之后价格（对这个范围）第一次变化的时刻，8 天内不变时为空。
func (s Settings) NextChange(model, kind string, now time.Time) *time.Time {
	return s.TimePricing.nextChange(model, kind, now, s.Location(), 8*24*time.Hour)
}

// Timeline 从 now 所在本地日的零点起 days 天、对这个范围生效的价格时间轴（相邻同价段合并）。
func (s Settings) Timeline(model, kind string, now time.Time, days int) []TimelineSegment {
	loc := s.Location()
	local := now.In(loc)
	from := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	to := from.AddDate(0, 0, days)
	points := []time.Time{from}
	if s.TimePricing.Enabled {
		for _, at := range s.TimePricing.boundaries(model, kind, from, to, loc) {
			if at.After(from) && at.Before(to) {
				points = append(points, at)
			}
		}
	}
	points = append(points, to)
	out := make([]TimelineSegment, 0, len(points))
	for i := 0; i+1 < len(points); i++ {
		start, end := points[i], points[i+1]
		if !end.After(start) {
			continue
		}
		segment := TimelineSegment{Start: start, End: end, Name: "标准价", Tone: ToneNormal, Multiplier: decimal.NewFromInt(1)}
		if window, ok := s.TimePricing.matchAt(model, kind, start.In(loc)); ok {
			segment.WindowID, segment.Name, segment.Tone, segment.Multiplier = window.ID, window.Name, window.Tone, window.Multiplier
		}
		if n := len(out); n > 0 && out[n-1].WindowID == segment.WindowID && out[n-1].Multiplier.Equal(segment.Multiplier) {
			out[n-1].End = end
			continue
		}
		out = append(out, segment)
	}
	return out
}

// ── 会员优惠 ──

// 会员优惠的出处。
const (
	MemberSourceNone = "none"
	MemberSourceVip  = "vip"
	MemberSourceTier = "tier"
)

// MemberTierConfig 一个会员档位的 AI 优惠。
type MemberTierConfig struct {
	TierID int64  `json:"tierId"`
	Label  string `json:"label"`
	// Multiplier 调用倍率（0.8 = 8 折）。
	Multiplier decimal.Decimal `json:"multiplier"`
	// Daily / Monthly 会员每日 / 每月额度（点），替代通用会员值。
	Daily   decimal.Decimal `json:"daily"`
	Monthly decimal.Decimal `json:"monthly"`
	// PackDiscount 买额度包的折扣（0.85 = 85 折），PackBonusRate 买额度包多给的比例（0.1 = 多送 10%）。
	PackDiscount  decimal.Decimal `json:"packDiscount"`
	PackBonusRate decimal.Decimal `json:"packBonusRate"`
	// RPM / MaxConcurrent 为 0 时沿用应用设置。
	RPM           int `json:"rpm"`
	MaxConcurrent int `json:"maxConcurrent"`
}

// MemberTier 设置里的会员档位优惠（别名，JSON 字段名 memberTiers）。
type MemberTier = MemberTierConfig

// MemberBenefit 一位用户此刻生效的会员优惠（非会员时各项为中性值）。
type MemberBenefit struct {
	Source        string          `json:"source"`
	TierID        int64           `json:"tierId,omitempty"`
	Name          string          `json:"name"`
	Multiplier    decimal.Decimal `json:"multiplier"`
	Daily         decimal.Decimal `json:"daily"`
	Monthly       decimal.Decimal `json:"monthly"`
	PackDiscount  decimal.Decimal `json:"packDiscount"`
	PackBonusRate decimal.Decimal `json:"packBonusRate"`
	RPM           int             `json:"rpm"`
	MaxConcurrent int             `json:"maxConcurrent"`
}

// NoMember 非会员的中性优惠。
func NoMember() MemberBenefit {
	return MemberBenefit{Source: MemberSourceNone, Name: "", Multiplier: decimal.NewFromInt(1), Daily: decimal.Zero,
		Monthly: decimal.Zero, PackDiscount: decimal.NewFromInt(1), PackBonusRate: decimal.Zero}
}

// VipMember 通用会员的优惠。
func (s Settings) VipMember() MemberBenefit {
	discount := s.VipPackDiscount
	if discount.Sign() <= 0 {
		discount = decimal.NewFromInt(1)
	}
	return MemberBenefit{Source: MemberSourceVip, Name: "会员", Multiplier: positiveOrOne(s.VipMultiplier), Daily: s.VipDaily,
		Monthly: s.VipMonthly, PackDiscount: discount, PackBonusRate: s.VipPackBonusRate}
}

func positiveOrOne(value decimal.Decimal) decimal.Decimal {
	if value.Sign() <= 0 {
		return decimal.NewFromInt(1)
	}
	return value
}

// TierMember 某个已配置档位的优惠；name 是档位名（label 为空时用它）。
func (s Settings) TierMember(tier MemberTierConfig, name string) MemberBenefit {
	label := strings.TrimSpace(tier.Label)
	if label == "" {
		label = name
	}
	if label == "" {
		label = fmt.Sprintf("档位 %d", tier.TierID)
	}
	discount := tier.PackDiscount
	if discount.Sign() <= 0 {
		discount = decimal.NewFromInt(1)
	}
	return MemberBenefit{Source: MemberSourceTier, TierID: tier.TierID, Name: label, Multiplier: positiveOrOne(tier.Multiplier),
		Daily: tier.Daily, Monthly: tier.Monthly, PackDiscount: discount, PackBonusRate: tier.PackBonusRate,
		RPM: tier.RPM, MaxConcurrent: tier.MaxConcurrent}
}

// Member 用户此刻生效的会员优惠：持有的档位里，MemberTiers 列表中最靠前的那个；
// 是会员但档位都没配置时用通用会员配置；非会员为中性值。
func (s Settings) Member(profile Profile) MemberBenefit {
	if !profile.IsVip {
		return NoMember()
	}
	for _, tier := range s.MemberTiers {
		if slices.Contains(profile.TierIDs, tier.TierID) {
			return s.TierMember(tier, profile.tierNames[tier.TierID])
		}
	}
	return s.VipMember()
}

// ApplyMember 把生效的档位记回画像（出网用）。
func (p Profile) ApplyMember(member MemberBenefit) Profile {
	if member.Source == MemberSourceTier {
		p.TierID, p.TierName = member.TierID, member.Name
	} else {
		p.TierID, p.TierName = 0, ""
	}
	return p
}

// EffectiveRPM / EffectiveConcurrency 会员档位覆盖后的频率与并发上限。
func (s Settings) EffectiveRPM(member MemberBenefit) int {
	if member.RPM > 0 {
		return member.RPM
	}
	return s.RPM
}

func (s Settings) EffectiveConcurrency(member MemberBenefit) int {
	if member.MaxConcurrent > 0 {
		return member.MaxConcurrent
	}
	return s.MaxConcurrent
}

// normalizeMemberTiers 校验档位优惠。档位是否存在由服务层核对（领域层不认识 vip_tiers）。
func normalizeMemberTiers(in []MemberTierConfig) ([]MemberTierConfig, error) {
	if len(in) > 32 {
		return nil, invalid("会员档位优惠最多 32 档")
	}
	seen := map[int64]bool{}
	out := make([]MemberTierConfig, 0, len(in))
	limit := decimal.NewFromInt(1_000_000_000)
	for _, raw := range in {
		tier := raw
		if tier.TierID <= 0 {
			return nil, invalid("会员档位优惠缺少档位")
		}
		if seen[tier.TierID] {
			return nil, invalid("档位 %d 的优惠重复", tier.TierID)
		}
		seen[tier.TierID] = true
		tier.Label = strings.TrimSpace(tier.Label)
		if len([]rune(tier.Label)) > 16 {
			return nil, invalid("档位显示名不能超过 16 个字")
		}
		if tier.Multiplier.Sign() <= 0 || tier.Multiplier.GreaterThan(maxMultiple) {
			return nil, invalid("档位 %d 的调用倍率应大于 0 且不超过 100", tier.TierID)
		}
		for _, item := range []struct {
			label string
			value decimal.Decimal
		}{{"每日额度", tier.Daily}, {"每月额度", tier.Monthly}} {
			if item.value.Sign() < 0 || item.value.GreaterThan(limit) {
				return nil, invalid("档位 %d 的%s应在 0-%s 点之间", tier.TierID, item.label, limit.String())
			}
		}
		if err := checkPackDiscount(fmt.Sprintf("档位 %d 的", tier.TierID), tier.PackDiscount, tier.PackBonusRate); err != nil {
			return nil, err
		}
		if tier.RPM < 0 || tier.RPM > 100_000 {
			return nil, invalid("档位 %d 的每分钟请求数应在 0-100000 之间（0 沿用应用设置）", tier.TierID)
		}
		if tier.MaxConcurrent < 0 || tier.MaxConcurrent > 1000 {
			return nil, invalid("档位 %d 的并发数应在 0-1000 之间（0 沿用应用设置）", tier.TierID)
		}
		out = append(out, tier)
	}
	return out, nil
}

func checkPackDiscount(prefix string, discount, bonus decimal.Decimal) error {
	if discount.Sign() <= 0 || discount.GreaterThan(decimal.NewFromInt(1)) {
		return invalid("%s额度包折扣应大于 0 且不超过 1", prefix)
	}
	if bonus.Sign() < 0 || bonus.GreaterThan(decimal.NewFromInt(10)) {
		return invalid("%s额度包多送比例应在 0-10 之间", prefix)
	}
	return nil
}

// ── 倍率分解 ──

// FactorBreakdown 一次计价的倍率分解。Total 是实际乘到单价上的数。
type FactorBreakdown struct {
	Base     decimal.Decimal `json:"base"`
	Member   decimal.Decimal `json:"member"`
	Level    decimal.Decimal `json:"level"`
	Time     decimal.Decimal `json:"time"`
	Total    decimal.Decimal `json:"total"`
	Stacking string          `json:"stacking"`
	Window   *TimeMatch      `json:"window"`
}

// Pricing 某位用户此刻调用某个模型的倍率分解；model 与 kind 都为空时按「全部模型」的口径（总览用）。
func (s Settings) Pricing(member MemberBenefit, level int, model, kind string, now time.Time) FactorBreakdown {
	stacking := s.TimePricing.Stacking
	if stacking == "" {
		stacking = StackMultiply
	}
	out := FactorBreakdown{
		Base: positiveOrOne(s.PriceMultiplier), Member: positiveOrOne(member.Multiplier), Level: s.levelFactor(level),
		Time: decimal.NewFromInt(1), Stacking: stacking,
	}
	if match := s.Match(model, kind, now); match != nil {
		out.Window = match
		out.Time = match.Multiplier
	}
	combined := out.Member.Mul(out.Time)
	if stacking == StackBest && out.Time.LessThan(decimal.NewFromInt(1)) {
		combined = decimal.Min(out.Member, out.Time)
	}
	out.Total = out.Base.Mul(combined).Mul(out.Level)
	return out
}

// ── 新人礼与额度商店 ──

// WelcomeGift 新人礼：每人可领一次的限期额度。
type WelcomeGift struct {
	Enabled      bool            `json:"enabled"`
	Amount       decimal.Decimal `json:"amount"`
	ValidityDays int             `json:"validityDays"`
}

// StoreSettings 额度商店。
type StoreSettings struct {
	Enabled bool   `json:"enabled"`
	Notice  string `json:"notice"`
}

// normalizeBillingPlan NormalizeSettings 里属于计费方案的那部分。
func normalizeBillingPlan(out *Settings) error {
	tiers, err := normalizeMemberTiers(out.MemberTiers)
	if err != nil {
		return err
	}
	out.MemberTiers = tiers
	if out.VipPackDiscount.IsZero() {
		out.VipPackDiscount = decimal.NewFromInt(1)
	}
	if err := checkPackDiscount("通用会员的", out.VipPackDiscount, out.VipPackBonusRate); err != nil {
		return err
	}
	pricing, err := normalizeTimePricing(out.TimePricing)
	if err != nil {
		return err
	}
	out.TimePricing = pricing
	if out.Welcome.Amount.Sign() < 0 || out.Welcome.Amount.GreaterThan(decimal.NewFromInt(100_000_000)) {
		return invalid("新人礼额度应在 0-100000000 点之间")
	}
	if out.Welcome.Enabled && out.Welcome.Amount.Sign() <= 0 {
		return invalid("开启新人礼时额度应大于 0")
	}
	if out.Welcome.ValidityDays < 0 || out.Welcome.ValidityDays > 3650 {
		return invalid("新人礼有效天数应在 0-3650 之间（0 为永久）")
	}
	out.Store.Notice = strings.TrimSpace(out.Store.Notice)
	if len([]rune(out.Store.Notice)) > 2000 {
		return invalid("购买须知不能超过 2000 个字")
	}
	return nil
}

// ZheLabel 把折扣写成「8 折」「85 折」；1 及以上返回空串。
func ZheLabel(discount decimal.Decimal) string {
	if discount.Sign() <= 0 || discount.GreaterThanOrEqual(decimal.NewFromInt(1)) {
		return ""
	}
	tenth := discount.Mul(decimal.NewFromInt(10))
	if tenth.Equal(tenth.Truncate(0)) {
		return tenth.Truncate(0).String() + " 折"
	}
	hundredth := discount.Mul(decimal.NewFromInt(100))
	if hundredth.Equal(hundredth.Truncate(0)) {
		return hundredth.Truncate(0).String() + " 折"
	}
	return tenth.Round(2).String() + " 折"
}
