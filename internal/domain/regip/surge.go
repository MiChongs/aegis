package regip

import (
	"math"
)

// MaxSurgeBaselineMinutes 突增基线最长多少分钟。分钟桶在 Redis 里保留这么久再多一点。
const MaxSurgeBaselineMinutes = 360

// SurgeState 突增检测的结论，概览与拦截记录里原样展示。
type SurgeState struct {
	Active bool `json:"active"`
	// Current 最近 WindowMinutes 分钟（含当前不完整的一分钟）的注册数
	Current int `json:"current"`
	// Expected 按基线推算的同样长度窗口里的期望注册数
	Expected float64 `json:"expected"`
	// Threshold 触发线：max(MinRegistrations, Expected + Sensitivity × σ)
	Threshold float64 `json:"threshold"`
	// Score 偏离期望多少个标准差
	Score float64 `json:"score"`
	// Series 参与计算的分钟桶，最老的在前，最后一个是当前分钟
	Series []int `json:"series,omitempty"`
}

// DetectSurge 用指数加权滑动平均（EWMA）估计平常每分钟的注册数与方差，判断最近一个窗口是否异常偏高。
//
// buckets 是最近 BaselineMinutes + WindowMinutes 个分钟桶，最老的在前、最后一个是当前分钟。
// 窗口之前的部分是基线，窗口内的部分求和得到 Current。
//
//   - EWMA 而不是简单平均：一小时前的平静不该和五分钟前的热闹同等分量，基线能跟上正常的早晚潮汐
//   - 窗口期望 μW = m·W，标准差 σW = sqrt(v·W)（相邻分钟近似独立，方差可加）
//   - σ 取 max(sqrt(v·W), sqrt(μW), 1)：平常几乎没人注册的应用方差接近 0，
//     不加泊松下限的话来两个人就会被判成「几十个标准差」
//   - Current < MinRegistrations 永不触发：小应用的绝对量再怎么翻倍也不是攻击
func DetectSurge(buckets []int, cfg Surge) SurgeState {
	window := cfg.WindowMinutes
	if window <= 0 || len(buckets) <= window {
		return SurgeState{Series: buckets}
	}
	baseline := buckets[:len(buckets)-window]
	recent := buckets[len(buckets)-window:]
	current := 0
	for _, value := range recent {
		current += value
	}
	mean, variance := ewma(baseline)
	expected := mean * float64(window)
	sigma := math.Max(math.Sqrt(variance*float64(window)), math.Max(math.Sqrt(expected), 1))
	threshold := math.Max(float64(cfg.MinRegistrations), expected+cfg.Sensitivity*sigma)
	score := (float64(current) - expected) / sigma
	return SurgeState{
		Active:    current >= cfg.MinRegistrations && float64(current) >= threshold,
		Current:   current,
		Expected:  round2(expected),
		Threshold: round2(threshold),
		Score:     round2(score),
		Series:    buckets,
	}
}

// ewma 指数加权的均值与方差（West 增量公式），α = 2/(N+1)，与 N 期简单平均的「重心」一致。
func ewma(values []int) (mean, variance float64) {
	if len(values) == 0 {
		return 0, 0
	}
	alpha := 2 / (float64(len(values)) + 1)
	mean = float64(values[0])
	for _, value := range values[1:] {
		diff := float64(value) - mean
		incr := alpha * diff
		mean += incr
		variance = (1 - alpha) * (variance + diff*incr)
	}
	return mean, variance
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// PenaltyDuration 第 level 次进惩罚期的时长（秒）：Base × Multiplier^(level-1)，封顶 Max。
func PenaltyDuration(cfg Penalty, level int) int {
	if level < 1 {
		level = 1
	}
	seconds := float64(cfg.BaseSeconds) * math.Pow(cfg.Multiplier, float64(level-1))
	if math.IsInf(seconds, 0) || seconds > float64(cfg.MaxSeconds) {
		return cfg.MaxSeconds
	}
	return int(seconds)
}

// EffectiveLimit 规则在这一次判定里的生效上限：名单放宽乘 factor，突增期间乘收紧系数，至少为 1。
func EffectiveLimit(limit int, factor float64, tighten float64) int {
	value := float64(limit)
	if factor > 0 {
		value *= factor
	}
	if tighten > 0 && tighten < 1 {
		value *= tighten
	}
	if value > MaxLimit*MaxListFactor {
		value = MaxLimit * MaxListFactor
	}
	return max(1, int(math.Floor(value)))
}
