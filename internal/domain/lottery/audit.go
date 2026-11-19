package lottery

import (
	"math"
	"slices"
	"sort"

	"gonum.org/v1/gonum/mat"
	"gonum.org/v1/gonum/stat/distuv"
)

// 出奖审计：线上的出奖结果是否真的服从算法给出的概率。
//
// 每一次抽奖的概率都不同（策略因人因时而变），普通的「按公示概率做卡方检验」不适用。但每条记录都存了
// 这一次的完整方案（trace），于是第 t 次抽奖是一个参数为 p_t 的单次多项试验，结果的指示向量 e_t 满足
//
//	E[e_t] = p_t,   Cov[e_t] = diag(p_t) − p_t p_tᵀ
//
// 各次独立，n 次合计 O = Σ e_t 的期望 E = Σ p_t、协方差 Σ = Σ_t (diag(p_t) − p_t p_tᵀ)。由多元中心极限定理，
//
//	T = (O − E)ᵀ Σ⁺ (O − E)  →  χ²(rank Σ)
//
// Σ⁺ 是 Moore–Penrose 伪逆（Σ 在「概率和为 1」的方向上奇异，秩为类别数 − 1），用 SVD 求。
// T 的 p 值很小说明观测与算法给的概率不符：随机源、实现或数据出了问题。
//
// 每个奖品另做一次边际检验：O_i 服从泊松二项分布，z = (O_i − E_i) / √Σ p(1−p)，双侧 p 值；
// 多个奖品同时检验时用 Holm–Bonferroni 校正控制族错误率。硬保底与「改发兜底奖」的那几次不是从方案里抽的，不计入。

// ── 逐笔复算 ──
//
// 上面的检验是统计意义上的：几千次里个别几次被动了手脚，卡方看不出来。逐笔复算是确定性的：
// 每条记录的 trace 里有这一次每个奖品的最终权重与抽样用的随机种子（见 NewSeededDrawSource），
// 把同样的权重、同样的种子交给同一个抽样函数，抽到的必须是记录上的那个奖品。对不上只有三种可能：
// 记录被改过、方案被改过、或者抽样的实现变了 —— 每一种都该有人知道。
// 硬保底与「权重全为 0 改发兜底」没有抽样，不复算；限量被抢光改发兜底的，核对的是抽样抽到的那个（Drawn）。
// 种子功能上线之前的记录没有种子，计入 Unseeded。

// AuditDraw 一次抽奖的方案与结果（取自 trace）。
type AuditDraw struct {
	// ID / DrawNo 记录的主键与单号（复算对不上时指给人看）
	ID     int64
	DrawNo string
	Odds   []PrizeOdds
	Picked int64
	// Drawn 抽样抽到的奖品（只在与 Picked 不同时有）；Seed 抽样用的种子
	Drawn    int64
	Seed     string
	Pity     bool
	Fallback bool
}

// AuditMismatch 复算与记录对不上的一次抽奖。
type AuditMismatch struct {
	DrawID int64  `json:"drawId"`
	DrawNo string `json:"drawNo"`
	// Recorded 记录上抽样抽到的奖品；Replayed 按种子复算抽到的奖品（0 为种子无效）
	Recorded int64 `json:"recorded"`
	Replayed int64 `json:"replayed"`
}

// AuditReplay 逐笔复算的结果。
type AuditReplay struct {
	// Checked 带种子、复算过的次数；Matched 其中与记录一致的次数
	Checked int `json:"checked"`
	Matched int `json:"matched"`
	// Unseeded 没有种子、无法复算的次数（上线之前的记录）；Skipped 没有抽样的次数（硬保底、权重全为 0）
	Unseeded int `json:"unseeded"`
	Skipped  int `json:"skipped"`
	// Mismatches 对不上的记录（最多 MaxAuditMismatches 条）
	Mismatches []AuditMismatch `json:"mismatches"`
}

// MaxAuditMismatches 复算对不上的记录最多列几条。
const MaxAuditMismatches = 50

// ReplayDraws 逐笔复算一批抽奖。
func ReplayDraws(draws []AuditDraw) AuditReplay {
	replay := AuditReplay{Mismatches: []AuditMismatch{}}
	for _, draw := range draws {
		recorded := draw.Picked
		if draw.Drawn != 0 {
			recorded = draw.Drawn
		}
		switch {
		case draw.Pity, draw.Fallback && draw.Drawn == 0:
			replay.Skipped++
			continue
		case draw.Seed == "":
			replay.Unseeded++
			continue
		}
		replay.Checked++
		replayed, ok := ReplaySample(DrawPlan{Odds: draw.Odds, Seed: draw.Seed})
		if ok && replayed == recorded {
			replay.Matched++
			continue
		}
		if len(replay.Mismatches) < MaxAuditMismatches {
			replay.Mismatches = append(replay.Mismatches, AuditMismatch{
				DrawID: draw.ID, DrawNo: draw.DrawNo, Recorded: recorded, Replayed: replayed,
			})
		}
	}
	return replay
}

// AuditPrize 一个奖品的边际检验。
type AuditPrize struct {
	PrizeID  int64   `json:"prizeId"`
	Position int     `json:"position"`
	Observed int     `json:"observed"`
	Expected float64 `json:"expected"`
	Std      float64 `json:"std"`
	Z        float64 `json:"z"`
	PValue   float64 `json:"pValue"`
	// Flagged Holm–Bonferroni 校正后在 Alpha 水平上显著
	Flagged bool `json:"flagged,omitempty"`
}

// AuditResult 整体检验与各奖品的边际检验。
type AuditResult struct {
	Draws int `json:"draws"`
	// Skipped 保底、改发兜底奖或方案不完整而不计入的次数
	Skipped   int     `json:"skipped"`
	Statistic float64 `json:"statistic"`
	DF        int     `json:"df"`
	PValue    float64 `json:"pValue"`
	Alpha     float64 `json:"alpha"`
	// Passed 统计检验通过，且逐笔复算没有对不上的
	Passed bool         `json:"passed"`
	Prizes []AuditPrize `json:"prizes"`
	// Replay 逐笔复算（确定性核对，与上面的统计检验互补）
	Replay AuditReplay `json:"replay"`
}

// AuditAlpha 审计的显著性水平。
const AuditAlpha = 0.01

// Audit 对一批抽奖做整体与边际检验。
func Audit(draws []AuditDraw) AuditResult {
	result := auditStatistics(draws)
	result.Replay = ReplayDraws(draws)
	if result.Replay.Matched != result.Replay.Checked {
		result.Passed = false
	}
	return result
}

// auditStatistics 整体与边际的统计检验。
func auditStatistics(draws []AuditDraw) AuditResult {
	result := AuditResult{Alpha: AuditAlpha, Passed: true, Prizes: []AuditPrize{}, Replay: AuditReplay{Mismatches: []AuditMismatch{}}}
	// 类别：出现过的全部奖品 ID
	index := map[int64]int{}
	positions := map[int64]int{}
	for _, draw := range draws {
		for _, odds := range draw.Odds {
			if _, ok := index[odds.PrizeID]; !ok {
				index[odds.PrizeID] = len(index)
				positions[odds.PrizeID] = odds.Position
			}
		}
	}
	k := len(index)
	if k < 2 {
		result.Skipped = len(draws)
		return result
	}
	observed := make([]float64, k)
	expected := make([]float64, k)
	variance := make([]float64, k)
	cov := mat.NewSymDense(k, nil)
	p := make([]float64, k)
	for _, draw := range draws {
		pickedAt, ok := index[draw.Picked]
		if draw.Pity || draw.Fallback || draw.Picked == 0 || !ok {
			result.Skipped++
			continue
		}
		clear(p)
		var sum float64
		for _, odds := range draw.Odds {
			p[index[odds.PrizeID]] = odds.Probability
			sum += odds.Probability
		}
		if sum <= 0 {
			result.Skipped++
			continue
		}
		// 抽中了方案里概率为 0 的奖品时照样计入：方案与结果对不上，正是检验要暴露的
		for i := range p {
			p[i] /= sum // trace 里的概率保留了 6 位小数，归一一下
		}
		result.Draws++
		observed[pickedAt]++
		for i := range p {
			expected[i] += p[i]
			variance[i] += p[i] * (1 - p[i])
			for j := i; j < k; j++ {
				value := -p[i] * p[j]
				if i == j {
					value += p[i]
				}
				cov.SetSym(i, j, cov.At(i, j)+value)
			}
		}
	}
	if result.Draws == 0 {
		return result
	}

	// 整体：Mahalanobis 统计量 + 伪逆
	diff := mat.NewVecDense(k, nil)
	for i := range observed {
		diff.SetVec(i, observed[i]-expected[i])
	}
	var svd mat.SVD
	if svd.Factorize(cov, mat.SVDThin) {
		values := svd.Values(nil)
		var u mat.Dense
		svd.UTo(&u)
		tolerance := values[0] * 1e-10 * float64(k)
		var statistic float64
		rank := 0
		for i, sigma := range values {
			if sigma <= tolerance {
				continue
			}
			rank++
			proj := mat.Dot(u.ColView(i), diff)
			statistic += proj * proj / sigma
		}
		result.Statistic = round6(statistic)
		result.DF = rank
		if rank > 0 {
			result.PValue = round6(1 - distuv.ChiSquared{K: float64(rank)}.CDF(statistic))
			result.Passed = result.PValue >= AuditAlpha
		}
	}

	// 边际：每个奖品的 z 检验 + Holm–Bonferroni
	normal := distuv.UnitNormal
	ids := make([]int64, 0, k)
	for id := range index {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b int64) int { return positions[a] - positions[b] })
	for _, id := range ids {
		i := index[id]
		prize := AuditPrize{
			PrizeID: id, Position: positions[id], Observed: int(observed[i]),
			Expected: round6(expected[i]), Std: round6(math.Sqrt(variance[i])), PValue: 1,
		}
		if variance[i] > 0 {
			z := (observed[i] - expected[i]) / math.Sqrt(variance[i])
			prize.Z = round6(z)
			prize.PValue = round6(2 * (1 - normal.CDF(math.Abs(z))))
		}
		result.Prizes = append(result.Prizes, prize)
	}
	order := make([]int, len(result.Prizes))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return result.Prizes[order[a]].PValue < result.Prizes[order[b]].PValue })
	m := len(order)
	for rank, i := range order {
		if result.Prizes[i].PValue*float64(m-rank) >= AuditAlpha {
			break // Holm：第一个不显著之后全部不显著
		}
		result.Prizes[i].Flagged = true
		result.Passed = false
	}
	return result
}
