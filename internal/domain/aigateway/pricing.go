package aigateway

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/shopspring/decimal"

	aidomain "aegis/internal/domain/ai"
)

// Price 一个模型的价格（单位：点）。
//
// token 类价格按每百万 token 报：这样「每百万 token 多少点」在数值上正好等于
// 「每个 token 多少微点」，计价不需要任何换算常数，也不会在乘除之间丢精度。
type Price struct {
	// InputPrice / OutputPrice 每百万输入 / 输出 token。
	InputPrice  decimal.Decimal `json:"inputPrice"`
	OutputPrice decimal.Decimal `json:"outputPrice"`
	// RequestPrice 每次调用的固定价。
	RequestPrice decimal.Decimal `json:"requestPrice"`
	// ImagePrice 每张图的默认价；ImagePrices 按尺寸 / 质量覆盖。
	ImagePrice  decimal.Decimal            `json:"imagePrice"`
	ImagePrices map[string]decimal.Decimal `json:"imagePrices,omitempty"`
	// CharPrice 每百万字符（语音合成的输入文本）。
	CharPrice decimal.Decimal `json:"charPrice"`
	// MinutePrice 每分钟音频（语音转写）。
	MinutePrice decimal.Decimal `json:"minutePrice"`
	// Free 显式免费。全部价格为 0 而没有这个标记的模型不允许保存。
	Free bool `json:"free"`
}

// Meters 一次调用的计量。
type Meters struct {
	InputTokens  int64  `json:"inputTokens"`
	OutputTokens int64  `json:"outputTokens"`
	Images       int64  `json:"images"`
	ImageSize    string `json:"imageSize,omitempty"`
	ImageQuality string `json:"imageQuality,omitempty"`
	Characters   int64  `json:"characters"`
	AudioMillis  int64  `json:"audioMs"`
}

var (
	million     = decimal.NewFromInt(MicroPerPoint)
	minuteMs    = decimal.NewFromInt(60_000)
	hundred     = decimal.NewFromInt(100)
	maxPrice    = decimal.NewFromInt(1_000_000)
	maxMultiple = decimal.NewFromInt(100)
)

// ImageUnitPrice 单张图的价格：质量:尺寸 > 尺寸 > 质量 > 默认价。
func (p Price) ImageUnitPrice(size, quality string) decimal.Decimal {
	size = strings.ToLower(strings.TrimSpace(size))
	quality = strings.ToLower(strings.TrimSpace(quality))
	if len(p.ImagePrices) > 0 {
		candidates := make([]string, 0, 3)
		if quality != "" && size != "" {
			candidates = append(candidates, quality+":"+size)
		}
		if size != "" {
			candidates = append(candidates, size)
		}
		if quality != "" {
			candidates = append(candidates, quality)
		}
		for _, key := range candidates {
			if price, ok := p.ImagePrices[key]; ok {
				return price
			}
		}
	}
	return p.ImagePrice
}

// costMicroRaw 不含倍率的价格，单位微点，未取整。
func (p Price) costMicroRaw(m Meters) decimal.Decimal {
	if p.Free {
		return decimal.Zero
	}
	total := p.RequestPrice.Mul(million)
	// 每百万 token 的点数 = 每 token 的微点数。
	total = total.Add(decimal.NewFromInt(m.InputTokens).Mul(p.InputPrice))
	total = total.Add(decimal.NewFromInt(m.OutputTokens).Mul(p.OutputPrice))
	total = total.Add(decimal.NewFromInt(m.Characters).Mul(p.CharPrice))
	if m.Images > 0 {
		total = total.Add(decimal.NewFromInt(m.Images).Mul(p.ImageUnitPrice(m.ImageSize, m.ImageQuality)).Mul(million))
	}
	if m.AudioMillis > 0 {
		total = total.Add(decimal.NewFromInt(m.AudioMillis).Mul(p.MinutePrice).Mul(million).Div(minuteMs))
	}
	return total
}

// CostMicro 含倍率的价格（微点），向上取整到 1 微点。
//
// 取整只在微点这一层做一次：1 微点是百万分之一个积分，向上取整对用户的影响可以忽略，
// 而且保证「有用量就有价格」—— 不会出现一个请求被舍入成 0。
func CostMicro(p Price, m Meters, factor decimal.Decimal) int64 {
	raw := p.costMicroRaw(m).Mul(factor)
	if raw.Sign() <= 0 {
		return 0
	}
	return raw.Ceil().IntPart()
}

// MaxAffordableOutput 预算（微点）够付多少个输出 token；fixed 是输出以外的部分（输入、固定价等）。
// 输出不计价时返回 -1，表示不必限制。
func MaxAffordableOutput(p Price, fixed Meters, factor decimal.Decimal, budget int64) int64 {
	if p.Free || p.OutputPrice.Sign() <= 0 || factor.Sign() <= 0 {
		return -1
	}
	fixed.OutputTokens = 0
	remaining := decimal.NewFromInt(budget).Sub(p.costMicroRaw(fixed).Mul(factor).Ceil())
	if remaining.Sign() <= 0 {
		return 0
	}
	perToken := p.OutputPrice.Mul(factor)
	return remaining.Div(perToken).Floor().IntPart()
}

// PointsToMicroFloor 点 → 微点（向下取整），用于额度上限这类「给用户的」数值。
func PointsToMicroFloor(points decimal.Decimal) int64 {
	if points.Sign() <= 0 {
		return 0
	}
	return points.Mul(million).Floor().IntPart()
}

// PointsToMicroCeil 点 → 微点（向上取整），用于「从用户那里收的」数值。
func PointsToMicroCeil(points decimal.Decimal) int64 {
	if points.Sign() <= 0 {
		return 0
	}
	return points.Mul(million).Ceil().IntPart()
}

// MicroToPoints 微点 → 点，出网用。
func MicroToPoints(micro int64) decimal.Decimal {
	return decimal.NewFromInt(micro).Div(million)
}

// ── 积分与余额的整数单位换算 ──
//
// 积分与余额的账本只认整数单位（1 积分、1 分），而一次调用的价格是微点。
// 做法是：暂扣时**向上**取整（保证结算时一定够扣），结算时把这一次的微点加到
// 账户的零头上再**向下**取整扣掉整数部分，余下不足一单位的部分留在零头里。
// 这样任意多次调用累计扣掉的整数单位，与累计价格的差永远小于一个单位。

// CeilUnits 微点按 per 换成整数单位，向上取整。
func CeilUnits(micro, per int64) int64 {
	if micro <= 0 {
		return 0
	}
	return (micro + per - 1) / per
}

// ChargeWithCarry 把 micro 加到零头上，返回这次要扣的整数单位与新零头。
func ChargeWithCarry(carry, micro, per int64) (units int64, newCarry int64) {
	total := carry + micro
	return total / per, total % per
}

// RefundWithCarry 退回一笔 micro：先抵掉零头里欠着的部分，不够再退整数单位。
//
// 零头的含义是「已经计价、还没扣成整数单位」的欠款，所以账户上恒有
// 已扣单位 × per + 零头 = 全部计价之和。退款要让这个等式对「去掉这一笔之后」仍成立：
// 退的整数单位可能比当初这一笔扣的少一个（当初那个单位里有之前的零头），也可能是 0
// （这一笔还整个躺在零头里没扣），但账永远是平的。
func RefundWithCarry(carry, micro, per int64) (units int64, newCarry int64) {
	if micro <= carry {
		return 0, carry - micro
	}
	units = CeilUnits(micro-carry, per)
	return units, carry - micro + units*per
}

// WalletMicroCents 点（微点）→ 金额（微分），向下取整。rate 是每点多少元。
func WalletMicroCents(micro int64, rate decimal.Decimal) int64 {
	if micro <= 0 || rate.Sign() <= 0 {
		return 0
	}
	return decimal.NewFromInt(micro).Mul(rate).Mul(hundred).Floor().IntPart()
}

// WalletHoldCents 暂扣 micro 点要从余额里移走多少分（向上取整）。
func WalletHoldCents(micro int64, rate decimal.Decimal) int64 {
	if micro <= 0 || rate.Sign() <= 0 {
		return 0
	}
	microCents := decimal.NewFromInt(micro).Mul(rate).Mul(hundred).Ceil().IntPart()
	return CeilUnits(microCents, MicroPerPoint)
}

// WalletAvailableMicro 余额 cents 分最多能付多少微点（向下取整）。
func WalletAvailableMicro(cents int64, rate decimal.Decimal) int64 {
	if cents <= 0 || rate.Sign() <= 0 {
		return 0
	}
	return decimal.NewFromInt(cents).Mul(million).Div(rate.Mul(hundred)).Floor().IntPart()
}

// ── 按来源分摊 ──

// Availability 一个来源此刻可用多少微点。
type Availability struct {
	Source string
	Micro  int64
	Period string
}

// PlanHolds 按顺序从各来源取，直到凑够 need（或取完）。返回各来源的取用与合计。
// Units 由调用方按来源填（积分 / 余额要换成整数单位）。
func PlanHolds(available []Availability, need int64) ([]Hold, int64) {
	holds := make([]Hold, 0, len(available))
	var total int64
	for _, item := range available {
		if total >= need {
			break
		}
		if item.Micro <= 0 {
			continue
		}
		take := min(item.Micro, need-total)
		holds = append(holds, Hold{Source: item.Source, Micro: take, Period: item.Period})
		total += take
	}
	return holds, total
}

// AllocateCost 把实际费用按暂扣顺序摊到各来源上：前面的来源先承担，每个来源最多承担它暂扣的部分。
// 返回各来源承担的微点（与 holds 一一对应）与暂扣不够覆盖的部分。
func AllocateCost(holds []Hold, cost int64) ([]int64, int64) {
	consumed := make([]int64, len(holds))
	remaining := max(cost, 0)
	for i, hold := range holds {
		if remaining == 0 {
			break
		}
		take := min(hold.Micro, remaining)
		consumed[i] = take
		remaining -= take
	}
	return consumed, remaining
}

// HoldsTotal 暂扣合计。
func HoldsTotal(holds []Hold) int64 {
	var total int64
	for _, hold := range holds {
		total += hold.Micro
	}
	return total
}

// ── 校验 ──

// ValidationError 输入不合法，Message 直接给到调用方。
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(format string, args ...any) error {
	return &ValidationError{Message: fmt.Sprintf(format, args...)}
}

var (
	modelNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+\-]{0,127}$`)
	imageKeyPattern  = regexp.MustCompile(`^([a-z0-9]{1,16}:)?[a-z0-9x]{1,16}$`)
)

// ValidModelName 模型名的形态：OpenAI / OpenRouter / 各家国内厂商的命名都能装下。
func ValidModelName(name string) bool {
	return modelNamePattern.MatchString(name)
}

func checkPrice(label string, value decimal.Decimal) error {
	if value.Sign() < 0 {
		return invalid("%s不能为负数", label)
	}
	if value.GreaterThan(maxPrice) {
		return invalid("%s不能超过 %s 点", label, maxPrice.String())
	}
	if value.Exponent() < -6 && !value.Equal(value.Round(6)) {
		return invalid("%s最多保留 6 位小数", label)
	}
	return nil
}

// NormalizeModel 校验并规整模型入参，返回可落库的模型（ID / AppID / 时间由调用方填）。
func NormalizeModel(in ModelInput) (Model, error) {
	out := Model{
		Model:           strings.TrimSpace(in.Model),
		UpstreamModel:   strings.TrimSpace(in.UpstreamModel),
		Kind:            strings.TrimSpace(in.Kind),
		DisplayName:     strings.TrimSpace(in.DisplayName),
		Description:     strings.TrimSpace(in.Description),
		ConfigID:        in.ConfigID,
		Enabled:         in.Enabled == nil || *in.Enabled,
		VipOnly:         in.VipOnly,
		MaxOutputTokens: in.MaxOutputTokens,
		SortOrder:       in.SortOrder,
		Price: Price{
			InputPrice:   in.InputPrice,
			OutputPrice:  in.OutputPrice,
			RequestPrice: in.RequestPrice,
			ImagePrice:   in.ImagePrice,
			CharPrice:    in.CharPrice,
			MinutePrice:  in.MinutePrice,
			Free:         in.Free,
		},
	}
	if !ValidModelName(out.Model) {
		return out, invalid("模型名只能包含字母、数字与 . _ : / @ + -，最长 128 位")
	}
	if out.UpstreamModel == out.Model {
		out.UpstreamModel = ""
	}
	if out.UpstreamModel != "" && !ValidModelName(out.UpstreamModel) {
		return out, invalid("上游模型名只能包含字母、数字与 . _ : / @ + -，最长 128 位")
	}
	if !ValidKind(out.Kind) {
		return out, invalid("不支持的模型种类：%s", out.Kind)
	}
	if len([]rune(out.DisplayName)) > 128 {
		return out, invalid("显示名不能超过 128 个字")
	}
	if len([]rune(out.Description)) > 500 {
		return out, invalid("说明不能超过 500 个字")
	}
	if out.ConfigID < 0 {
		return out, invalid("通道 ID 不合法")
	}
	if out.MaxOutputTokens < 0 || out.MaxOutputTokens > 10_000_000 {
		return out, invalid("最大输出 token 应在 0-10000000 之间")
	}
	capabilities, err := NormalizeCapabilities(in.Capabilities)
	if err != nil {
		return out, err
	}
	out.Capabilities = capabilities
	checks := []struct {
		label string
		value decimal.Decimal
	}{
		{"输入单价", out.InputPrice}, {"输出单价", out.OutputPrice}, {"每次调用价格", out.RequestPrice},
		{"每张图价格", out.ImagePrice}, {"字符单价", out.CharPrice}, {"每分钟价格", out.MinutePrice},
	}
	for _, item := range checks {
		if err := checkPrice(item.label, item.value); err != nil {
			return out, err
		}
	}
	if len(in.ImagePrices) > 32 {
		return out, invalid("图片分档价格最多 32 档")
	}
	if len(in.ImagePrices) > 0 {
		out.ImagePrices = make(map[string]decimal.Decimal, len(in.ImagePrices))
		keys := make([]string, 0, len(in.ImagePrices))
		for key := range in.ImagePrices {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, raw := range keys {
			key := strings.ToLower(strings.TrimSpace(raw))
			if !imageKeyPattern.MatchString(key) {
				return out, invalid("图片分档「%s」应写成尺寸（1024x1024）、质量（hd）或「质量:尺寸」", raw)
			}
			if err := checkPrice("图片分档「"+key+"」的价格", in.ImagePrices[raw]); err != nil {
				return out, err
			}
			out.ImagePrices[key] = in.ImagePrices[raw]
		}
	}
	if !out.Free && !out.HasAnyPrice() {
		return out, invalid("模型没有设置任何价格：免费提供请勾选「免费」")
	}
	return out, nil
}

// HasAnyPrice 至少有一项价格大于 0。
func (p Price) HasAnyPrice() bool {
	for _, value := range []decimal.Decimal{p.InputPrice, p.OutputPrice, p.RequestPrice, p.ImagePrice, p.CharPrice, p.MinutePrice} {
		if value.Sign() > 0 {
			return true
		}
	}
	for _, value := range p.ImagePrices {
		if value.Sign() > 0 {
			return true
		}
	}
	return false
}

// Scaled 乘上倍率后的价格（给用户看的有效单价）。
func (p Price) Scaled(factor decimal.Decimal) Price {
	scale := func(value decimal.Decimal) decimal.Decimal { return value.Mul(factor).Round(6) }
	out := Price{
		InputPrice: scale(p.InputPrice), OutputPrice: scale(p.OutputPrice),
		RequestPrice: scale(p.RequestPrice), ImagePrice: scale(p.ImagePrice),
		CharPrice: scale(p.CharPrice), MinutePrice: scale(p.MinutePrice), Free: p.Free,
	}
	if len(p.ImagePrices) > 0 {
		out.ImagePrices = make(map[string]decimal.Decimal, len(p.ImagePrices))
		for key, value := range p.ImagePrices {
			out.ImagePrices[key] = scale(value)
		}
	}
	return out
}

// NormalizeSettings 校验并规整设置。
func NormalizeSettings(in Settings) (Settings, error) {
	out := in
	out.Audience = strings.TrimSpace(out.Audience)
	if out.Audience == "" {
		out.Audience = AudienceAll
	}
	if out.Audience != AudienceAll && out.Audience != AudienceVip {
		return out, invalid("使用对象只能是 all 或 vip")
	}
	seen := map[string]bool{}
	sources := make([]string, 0, len(out.Sources))
	for _, raw := range out.Sources {
		source := strings.TrimSpace(raw)
		if source == SourceIntegral {
			return out, invalid("AI 额度与积分已不互通，扣费来源不能再选积分")
		}
		if _, ok := FindSource(source); !ok {
			return out, invalid("未知的扣费来源：%s", raw)
		}
		if seen[source] {
			return out, invalid("扣费来源「%s」重复", source)
		}
		seen[source] = true
		sources = append(sources, source)
	}
	if len(sources) == 0 {
		return out, invalid("至少要启用一个扣费来源")
	}
	out.Sources = sources

	for _, item := range []struct {
		label string
		value decimal.Decimal
	}{{"全局倍率", out.PriceMultiplier}, {"会员倍率", out.VipMultiplier}} {
		if item.value.Sign() <= 0 || item.value.GreaterThan(maxMultiple) {
			return out, invalid("%s应大于 0 且不超过 100", item.label)
		}
	}
	levels := map[int]bool{}
	for _, item := range out.LevelMultipliers {
		if item.Level < 1 || item.Level > 9999 {
			return out, invalid("等级倍率的等级应在 1-9999 之间")
		}
		if levels[item.Level] {
			return out, invalid("等级 %d 的倍率重复", item.Level)
		}
		levels[item.Level] = true
		if item.Multiplier.Sign() <= 0 || item.Multiplier.GreaterThan(maxMultiple) {
			return out, invalid("等级 %d 的倍率应大于 0 且不超过 100", item.Level)
		}
	}
	if len(out.LevelMultipliers) > 64 {
		return out, invalid("等级倍率最多 64 档")
	}
	sort.Slice(out.LevelMultipliers, func(i, j int) bool { return out.LevelMultipliers[i].Level < out.LevelMultipliers[j].Level })
	if out.LevelMultipliers == nil {
		out.LevelMultipliers = []LevelMultiplier{}
	}

	limit := decimal.NewFromInt(1_000_000_000)
	for _, item := range []struct {
		label string
		value decimal.Decimal
	}{{"每日免费额度", out.FreeDaily}, {"会员每日额度", out.VipDaily}, {"会员每月额度", out.VipMonthly},
		{"每日消费上限", out.DailySpendLimit}} {
		if item.value.Sign() < 0 || item.value.GreaterThan(limit) {
			return out, invalid("%s应在 0-%s 点之间", item.label, limit.String())
		}
	}
	if out.HasSource(SourceWallet) && (out.WalletRate.Sign() <= 0 || out.WalletRate.GreaterThan(decimal.NewFromInt(1000))) {
		return out, invalid("启用余额直付时，单价应大于 0 且不超过 1000 元/额度")
	}
	if out.WalletRate.Sign() < 0 {
		return out, invalid("余额直付单价不能为负数")
	}
	if out.RPM < 0 || out.RPM > 100_000 {
		return out, invalid("每分钟请求数应在 0-100000 之间（0 不限）")
	}
	if out.MaxConcurrent < 1 || out.MaxConcurrent > 1000 {
		return out, invalid("并发请求数应在 1-1000 之间")
	}
	if out.MaxKeysPerUser < 0 || out.MaxKeysPerUser > 100 {
		return out, invalid("每人密钥数应在 0-100 之间")
	}
	if out.DefaultMaxTokens < 1 || out.DefaultMaxTokens > 10_000_000 {
		return out, invalid("默认输出 token 应在 1-10000000 之间")
	}
	out.Timezone = strings.TrimSpace(out.Timezone)
	if out.Timezone == "" {
		out.Timezone = "Asia/Shanghai"
	}
	if _, err := loadLocation(out.Timezone); err != nil {
		return out, invalid("无效的时区：%s", out.Timezone)
	}
	if err := normalizeBillingPlan(&out); err != nil {
		return out, err
	}
	return out, nil
}

// NormalizeCapabilities 校验能力声明：强度去重并按由低到高排好；没给时取默认。
func NormalizeCapabilities(in *Capabilities) (Capabilities, error) {
	if in == nil {
		return DefaultCapabilities(), nil
	}
	out := *in
	if out.ContextLength < 0 || out.ContextLength > 100_000_000 {
		return out, invalid("上下文长度应在 0-100000000 之间")
	}
	if out.MaxOutput < 0 || out.MaxOutput > 100_000_000 {
		return out, invalid("最大输出应在 0-100000000 之间")
	}
	out.CatalogKey = strings.TrimSpace(out.CatalogKey)
	if len(out.CatalogKey) > 256 {
		return out, invalid("目录条目名过长")
	}
	seen := map[string]bool{}
	for _, effort := range in.ReasoningEfforts {
		normalized, ok := aidomain.NormalizeReasoningEffort(effort)
		if !ok {
			return out, invalid("不支持的思考强度：%s", effort)
		}
		seen[normalized] = true
	}
	out.ReasoningEfforts = make([]string, 0, len(seen))
	for _, effort := range aidomain.ReasoningEfforts {
		if seen[effort] {
			out.ReasoningEfforts = append(out.ReasoningEfforts, effort)
		}
	}
	if !out.Reasoning {
		out.ReasoningEfforts = []string{}
	}
	return out, nil
}
