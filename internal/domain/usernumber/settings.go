package usernumber

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"time"
)

// 位数的硬边界：最短 5 位，最长 12 位（远小于 2^53，客户端按 Long / JS number 解析都不丢精度）。
const (
	HardMinLength = 5
	HardMaxLength = 12
	// 默认号的位数至少 8 位：更短的号本身就是传说 / 至尊级，不能当默认号白送
	MinDefaultLength = 8
)

// Settings 一个应用的号码设置（user_number_settings.config）。
type Settings struct {
	// Enabled 关闭时用户端看不到号码页、不能换号与摇号；号码照常发、照常展示
	Enabled bool `json:"enabled"`
	// MinLength / MaxLength 用户能换到的号码位数范围（5–12）
	MinLength int `json:"minLength"`
	MaxLength int `json:"maxLength"`
	// DefaultLength 新用户与回填拿到的普通号的位数（8–12）
	DefaultLength int `json:"defaultLength"`
	// SelfPick 允许用改号卡 / 靓号券自选号码；关闭时只能摇号
	SelfPick bool `json:"selfPick"`
	// CooldownDays 两次换号之间至少隔几天（0 不限）；管理员指定不受限
	CooldownDays int `json:"cooldownDays"`
	// FreezeDays 换下来的旧号冻结几天才放出（0 立即放出）；冻结期内原主人可以免费换回
	FreezeDays int `json:"freezeDays"`
	// DrawCandidates 一次摇几个候选号（1–12）
	DrawCandidates int `json:"drawCandidates"`
	// DrawHoldMinutes 候选号锁定几分钟（1–120），过期作废、次数不退
	DrawHoldMinutes int `json:"drawHoldMinutes"`
	// DrawWeights 各级在摇号里的基础权重（下标即等级）；非普通级会乘以号运
	DrawWeights [TierCount]float64 `json:"drawWeights"`
	// LuckSpread 号运差异 σ（0 关闭号运；与抽奖的每日运气同一个模型、独立的一份）
	LuckSpread float64 `json:"luckSpread"`
	// LuckSupreme 号运叠加抽奖的至尊加成（应用级设置里的那一份）
	LuckSupreme bool `json:"luckSupreme"`
	// PatternTiers 规律落在哪一级的覆盖（-1 停用）；不写的用默认
	PatternTiers map[string]int `json:"patternTiers,omitempty"`

	UpdatedBy string     `json:"updatedBy,omitempty"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
}

// DefaultSettings 新应用的默认设置。默认开启：号码人人都有，关掉的只是更换入口。
func DefaultSettings() Settings {
	return Settings{
		Enabled:         true,
		MinLength:       HardMinLength,
		MaxLength:       HardMaxLength,
		DefaultLength:   9,
		SelfPick:        true,
		CooldownDays:    7,
		FreezeDays:      90,
		DrawCandidates:  6,
		DrawHoldMinutes: 15,
		DrawWeights:     [TierCount]float64{70, 20, 7, 2.5, 0.5},
		LuckSpread:      0.3,
		LuckSupreme:     true,
	}
}

// Normalize 填默认值、夹范围、清理不认识的规律键。
func (s *Settings) Normalize() {
	d := DefaultSettings()
	if s.MinLength == 0 {
		s.MinLength = d.MinLength
	}
	if s.MaxLength == 0 {
		s.MaxLength = d.MaxLength
	}
	if s.DefaultLength == 0 {
		s.DefaultLength = d.DefaultLength
	}
	if s.DrawCandidates == 0 {
		s.DrawCandidates = d.DrawCandidates
	}
	if s.DrawHoldMinutes == 0 {
		s.DrawHoldMinutes = d.DrawHoldMinutes
	}
	allZero := true
	for _, w := range s.DrawWeights {
		if w != 0 {
			allZero = false
		}
	}
	if allZero {
		s.DrawWeights = d.DrawWeights
	}
	for key, tier := range s.PatternTiers {
		if _, ok := FindPattern(key); !ok || (tier != PatternDisabled && !ValidTier(tier)) {
			delete(s.PatternTiers, key)
		}
	}
	if len(s.PatternTiers) == 0 {
		s.PatternTiers = nil
	}
}

// Validate 校验（先 Normalize）。
func (s *Settings) Validate() error {
	if s.MinLength < HardMinLength || s.MaxLength > HardMaxLength || s.MinLength > s.MaxLength {
		return fmt.Errorf("号码位数范围须在 %d–%d 之间且最短不大于最长", HardMinLength, HardMaxLength)
	}
	if s.DefaultLength < MinDefaultLength || s.DefaultLength > HardMaxLength {
		return fmt.Errorf("默认号码位数须在 %d–%d 之间", MinDefaultLength, HardMaxLength)
	}
	if s.CooldownDays < 0 || s.CooldownDays > 365 {
		return errors.New("换号冷却须在 0–365 天之间")
	}
	if s.FreezeDays < 0 || s.FreezeDays > 3650 {
		return errors.New("旧号冻结须在 0–3650 天之间")
	}
	if s.DrawCandidates < 1 || s.DrawCandidates > 12 {
		return errors.New("每次摇号的候选数须在 1–12 之间")
	}
	if s.DrawHoldMinutes < 1 || s.DrawHoldMinutes > 120 {
		return errors.New("候选号锁定时间须在 1–120 分钟之间")
	}
	total := 0.0
	for i, w := range s.DrawWeights {
		if w < 0 || math.IsNaN(w) || math.IsInf(w, 0) {
			return fmt.Errorf("%s的摇号权重不能为负", TierOf(i).Name)
		}
		total += w
	}
	if total <= 0 {
		return errors.New("摇号权重至少要有一级大于 0")
	}
	if s.LuckSpread < 0 || s.LuckSpread > 1 {
		return errors.New("号运差异须在 0–1 之间")
	}
	return nil
}

func (s *Settings) patternTier(key string, fallback int) int {
	if s == nil {
		return fallback
	}
	if tier, ok := s.PatternTiers[key]; ok {
		return tier
	}
	return fallback
}

func (s *Settings) lengthAllowed(length int) bool {
	return length >= s.MinLength && length <= s.MaxLength
}

// 号码不合法的几种情况。
var (
	ErrNumberFormat = errors.New("号码须为纯数字且不以 0 开头")
	ErrNumberLength = errors.New("号码位数不在允许范围内")
)

// ValidNumber 号码是否合法（位数在范围内、不以 0 开头）。
func (s *Settings) ValidNumber(n int64) error {
	if n <= 0 {
		return ErrNumberFormat
	}
	if !s.lengthAllowed(len(strconv.FormatInt(n, 10))) {
		return ErrNumberLength
	}
	return nil
}

// ParseNumber 解析用户输入的号码（只认纯数字）。
func ParseNumber(raw string) (int64, error) {
	if raw == "" || raw[0] == '0' || len(raw) > HardMaxLength {
		return 0, ErrNumberFormat
	}
	for i := 0; i < len(raw); i++ {
		if raw[i] < '0' || raw[i] > '9' {
			return 0, ErrNumberFormat
		}
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, ErrNumberFormat
	}
	return n, nil
}

// ── 权益的选择 ──

// Accepts 这份权益能不能换 tier 级的号码：改号卡只换普通号，靓号券换不高于其等级的号。摇号次数不用于自选。
func Accepts(kind string, level, tier int) bool {
	switch kind {
	case RightChange:
		return tier == TierCommon
	case RightTicket:
		return level >= tier && level >= TierFine
	}
	return false
}

// PickRight 自动选一份能换 tier 级号码的权益：普通号先用改号卡，其余用等级最低的够用的靓号券。
// 没有可用的返回 false。
func PickRight(rights Rights, tier int) (kind string, level int, ok bool) {
	if tier == TierCommon && rights.Change > 0 {
		return RightChange, 0, true
	}
	for _, t := range rights.Tickets {
		if t.Balance > 0 && Accepts(RightTicket, t.Level, tier) {
			return RightTicket, t.Level, true
		}
	}
	return "", 0, false
}

// Requirement 换 tier 级号码最低要什么权益（展示用）。
func Requirement(tier int) (kind string, level int) {
	if tier == TierCommon {
		return RightChange, 0
	}
	return RightTicket, tier
}

// ── 摇号 ──

// LuckWeights 乘上号运之后的各级权重：普通级不变，其余乘 luck。
func (s *Settings) LuckWeights(luck float64) [TierCount]float64 {
	w := s.DrawWeights
	if luck <= 0 || math.IsNaN(luck) {
		luck = 1
	}
	for i := TierFine; i < TierCount; i++ {
		w[i] *= luck
	}
	return w
}

// TierOdds 各级在一次摇号里出现的概率（百分比，和为 100），用户端公示用。
func (s *Settings) TierOdds(luck float64) [TierCount]float64 {
	w := s.LuckWeights(luck)
	total := 0.0
	for _, v := range w {
		total += v
	}
	var out [TierCount]float64
	if total <= 0 {
		return out
	}
	for i, v := range w {
		out[i] = math.Round(v/total*10000) / 100
	}
	return out
}

// Float 随机源（[0,1) 均匀）。
type Float interface {
	Float64() float64
}

// RollTier 按乘过号运的权重抽一级。
func (s *Settings) RollTier(luck float64, r Float) int {
	w := s.LuckWeights(luck)
	total := 0.0
	for _, v := range w {
		total += v
	}
	x := r.Float64() * total
	for i, v := range w {
		if x < v {
			return i
		}
		x -= v
	}
	return TierCommon
}
