package growth

import (
	"fmt"
	"math/big"
	"math/rand/v2"
	"time"
)

// Factor 倍率，用分数表示：×2 = 2/1，÷3 = 1/3，×1.5 = 3/2，冻结 = 0/1。
//
// 用分数而不是小数，是因为「÷3」在任何定长小数里都不精确：0.33 用一年会让人少拿 1%，
// 0.3333 又会在规则叠乘之后漂出更多位。分数叠乘、约分、最后一步才取整，结果只取决于取整方式。
type Factor struct {
	Num int64 `json:"num"`
	Den int64 `json:"den"`
}

// One 不改变数值的倍率。
var One = Factor{Num: 1, Den: 1}

// IsOne 是否为 1 倍。
func (f Factor) IsOne() bool { return f.Den > 0 && f.Num == f.Den }

// String ×2 / ÷3 / ×3/2 / 冻结。
func (f Factor) String() string {
	switch {
	case f.Den <= 0:
		return "×1"
	case f.Num == 0:
		return "冻结"
	case f.Num%f.Den == 0:
		return fmt.Sprintf("×%d", f.Num/f.Den)
	case f.Num == 1:
		return fmt.Sprintf("÷%d", f.Den)
	}
	return fmt.Sprintf("×%d/%d", f.Num, f.Den)
}

// Combined 生效规则叠乘后的结果。
type Combined struct {
	Factor   Factor
	Rounding Rounding
	// DailyCap 生效规则里最严的每日上限，0 = 不限
	DailyCap int64
	RuleIDs  []int64
}

// Combine 把作用于某个来源的生效规则叠乘：倍率相乘（大数运算后约分，叠多少条都不会溢出），
// 每日上限取最小的非零值，取整方式取个人规则里最新的一条（个人规则比全应用规则更具体），没有个人规则时取全应用最新的一条。
func Combine(rules []Rule, source string, now time.Time) Combined {
	out := Combined{Factor: One, Rounding: RoundFloor, RuleIDs: []int64{}}
	product := big.NewRat(1, 1)
	var roundingFrom *Rule
	for i := range rules {
		rule := &rules[i]
		if !rule.ActiveAt(now) || !rule.Covers(source) {
			continue
		}
		if rule.Denominator <= 0 {
			continue
		}
		product.Mul(product, big.NewRat(int64(rule.Numerator), int64(rule.Denominator)))
		if rule.DailyCap > 0 && (out.DailyCap == 0 || rule.DailyCap < out.DailyCap) {
			out.DailyCap = rule.DailyCap
		}
		if roundingFrom == nil || moreSpecific(rule, roundingFrom) {
			roundingFrom = rule
		}
		out.RuleIDs = append(out.RuleIDs, rule.ID)
	}
	if roundingFrom != nil && roundingFrom.Rounding.Valid() {
		out.Rounding = roundingFrom.Rounding
	}
	out.Factor = clampRat(product)
	return out
}

// moreSpecific a 是否比 b 更该决定取整方式：个人规则优先，同一层取最新的。
func moreSpecific(a, b *Rule) bool {
	if (a.UserID > 0) != (b.UserID > 0) {
		return a.UserID > 0
	}
	if !a.CreatedAt.Equal(b.CreatedAt) {
		return a.CreatedAt.After(b.CreatedAt)
	}
	return a.ID > b.ID
}

// clampRat 把叠乘结果截到可表示的范围：分子分母都放进 int64，过大的倍率截到 ×1e6、过小的按 0 处理。
func clampRat(r *big.Rat) Factor {
	if r.Sign() <= 0 {
		return Factor{Num: 0, Den: 1}
	}
	const limit = 1_000_000
	if r.Num().IsInt64() && r.Denom().IsInt64() && r.Num().Int64() <= limit*limit && r.Denom().Int64() <= limit*limit {
		return Factor{Num: r.Num().Int64(), Den: r.Denom().Int64()}
	}
	if r.Cmp(big.NewRat(limit, 1)) >= 0 {
		return Factor{Num: limit, Den: 1}
	}
	if r.Cmp(big.NewRat(1, limit)) <= 0 {
		return Factor{Num: 0, Den: 1}
	}
	// 中间值：按 1e6 精度近似（只在叠了很多条怪异分数时才走到）
	approx := new(big.Rat).Mul(r, big.NewRat(limit, 1))
	f, _ := approx.Float64()
	return Factor{Num: int64(f + 0.5), Den: limit}
}

// Scale 按倍率与取整方式换算一笔收入。rng 只在随机取整时使用，可传 nil（此时用全局随机源）。
// 结果不会是负数；base <= 0 原样返回。
func Scale(base int64, factor Factor, rounding Rounding, rng *rand.Rand) int64 {
	if base <= 0 || factor.IsOne() || factor.Den <= 0 {
		return max(base, 0)
	}
	if factor.Num == 0 {
		return 0
	}
	product := new(big.Int).Mul(big.NewInt(base), big.NewInt(factor.Num))
	den := big.NewInt(factor.Den)
	quo, rem := new(big.Int).QuoRem(product, den, new(big.Int))
	if rem.Sign() != 0 {
		switch rounding {
		case RoundCeil:
			quo.Add(quo, big.NewInt(1))
		case RoundNearest:
			// rem*2 >= den 进位
			if new(big.Int).Lsh(rem, 1).Cmp(den) >= 0 {
				quo.Add(quo, big.NewInt(1))
			}
		case RoundRandom:
			// 以 rem/den 的概率进位
			var draw int64
			if rng != nil {
				draw = rng.Int64N(factor.Den)
			} else {
				draw = rand.Int64N(factor.Den)
			}
			if big.NewInt(draw).Cmp(rem) < 0 {
				quo.Add(quo, big.NewInt(1))
			}
		}
	}
	if !quo.IsInt64() || quo.Int64() > MaxDailyCap*100 {
		return MaxDailyCap * 100
	}
	return quo.Int64()
}

// Apply 一笔收入经过规则：先换算，再按每日上限截断。usedToday 是今天（同一类型、受管来源）已经实际入账的合计。
func Apply(base int64, combined Combined, usedToday int64, source string, rng *rand.Rand) Outcome {
	scaled := Scale(base, combined.Factor, combined.Rounding, rng)
	out := Outcome{
		Base: base, Scaled: scaled, Granted: scaled,
		Num: combined.Factor.Num, Den: combined.Factor.Den, Rounding: string(combined.Rounding),
		DailyCap: combined.DailyCap, UsedToday: usedToday, RuleIDs: combined.RuleIDs, Source: source,
	}
	if combined.DailyCap > 0 {
		room := max(combined.DailyCap-max(usedToday, 0), 0)
		if out.Granted > room {
			out.Granted = room
			out.Capped = true
		}
	}
	return out
}
