package model

import (
	"fmt"
	"math"
	"sort"
)

// RankFeatures 精排特征（顺序固定，第二期 16 个 + 第三期 4 个，与 music-deep 的 LightGBM 一致）。改了顺序或个数要升快照格式。
var RankFeatures = []string{
	"als", "itemcf", "artist", "trending", "explore", // 0–4 各通道归一化分
	"foldin",      // 5 折入向量点积（归一化）
	"pop",         // 6 作品热度
	"complete",    // 7 完播率
	"skip",        // 8 早跳率
	"artist_aff",  // 9 用户对该歌手的偏好
	"recent",      // 10 近期播放过
	"fatigue",     // 11 近 7 天展示未点 log1p
	"lang_match",  // 12 语种占比
	"fresh",       // 13 从没听过
	"pref_artist", // 14 偏好里选过的歌手
	"position",    // 15 展示位置 log1p（线上置 0）
	// 第三期（docs/music-personalization-3.md「第三期精排特征」）
	"seq",       // 16 序列模型分（本次候选里按最大值归一）
	"tag_match", // 17 物品标签与用户近期标签分布的余弦
	"semantic",  // 18 物品语义向量与用户近期正反馈语义均值的余弦（0–1）
	"llm",       // 19 来自大模型扩展
}

// FeatPosition 位置特征的下标。
const FeatPosition = 15

// NumRankFeatures 特征数。
var NumRankFeatures = len(RankFeatures)

// Ranker 逻辑回归精排（特征标准化后线性 + sigmoid）。
type Ranker struct {
	Features    []string
	Mean, Std   []float32
	Weights     []float32
	Bias        float32
	Samples     int
	Positives   int
	AUC         float64 // 留出集
	BaselineAUC float64 // 同一留出集上「融合分」的 AUC
	Enabled     bool
	Reason      string // 没启用的原因
}

// Score 打分（0–1）。x 的长度必须等于特征数；调用方把位置特征置 0。
func (r *Ranker) Score(x []float32) float64 {
	z := float64(r.Bias)
	for i, w := range r.Weights {
		v := float64(x[i])
		if r.Std[i] > 0 {
			v = (v - float64(r.Mean[i])) / float64(r.Std[i])
		} else {
			v = 0
		}
		z += float64(w) * v
	}
	return 1 / (1 + math.Exp(-z))
}

// Sample 一条训练样本。
type Sample struct {
	X    []float32
	Y    bool
	TS   int64
	Base float32 // 基线分（融合分），算基线 AUC 用
}

// RankerParams 训练参数。
type RankerParams struct {
	MinSamples int
	L2         float64
	Epochs     int
}

// TrainRanker 训练精排：按时间留出最后 20% 评估，与融合分基线比 AUC；更好才启用（用全部样本重训一遍作为最终权重）。
func TrainRanker(samples []Sample, p RankerParams) *Ranker {
	n := NumRankFeatures
	r := &Ranker{Features: append([]string(nil), RankFeatures...), Samples: len(samples)}
	for _, s := range samples {
		if s.Y {
			r.Positives++
		}
	}
	if len(samples) < p.MinSamples {
		r.Reason = fmt.Sprintf("样本不足 %d（现有 %d）", p.MinSamples, len(samples))
		return r
	}
	if r.Positives < 20 || len(samples)-r.Positives < 20 {
		r.Reason = "正负样本太少"
		return r
	}
	sort.SliceStable(samples, func(a, b int) bool { return samples[a].TS < samples[b].TS })
	cut := len(samples) * 8 / 10
	train, test := samples[:cut], samples[cut:]
	mean, std := standardize(train, n)
	w, b := fitLR(train, mean, std, p)
	model := &Ranker{Mean: mean, Std: std, Weights: w, Bias: b}
	scores := make([]float64, len(test))
	base := make([]float64, len(test))
	labels := make([]bool, len(test))
	for i, s := range test {
		x := append([]float32(nil), s.X...)
		x[FeatPosition] = 0
		scores[i] = model.Score(x)
		base[i] = float64(s.Base)
		labels[i] = s.Y
	}
	r.AUC, r.BaselineAUC = AUC(scores, labels), AUC(base, labels)
	if math.IsNaN(r.AUC) {
		r.Reason = "留出集只有一类标签"
		return r
	}
	if r.AUC <= r.BaselineAUC {
		r.Reason = fmt.Sprintf("AUC %.4f 不高于基线 %.4f", r.AUC, r.BaselineAUC)
		r.Mean, r.Std, r.Weights, r.Bias = mean, std, w, b
		return r
	}
	r.Mean, r.Std = standardize(samples, n)
	r.Weights, r.Bias = fitLR(samples, r.Mean, r.Std, p)
	r.Enabled = true
	return r
}

func standardize(samples []Sample, n int) (mean, std []float32) {
	m := make([]float64, n)
	v := make([]float64, n)
	for _, s := range samples {
		for i := 0; i < n; i++ {
			m[i] += float64(s.X[i])
		}
	}
	for i := range m {
		m[i] /= float64(len(samples))
	}
	for _, s := range samples {
		for i := 0; i < n; i++ {
			d := float64(s.X[i]) - m[i]
			v[i] += d * d
		}
	}
	mean, std = make([]float32, n), make([]float32, n)
	for i := range m {
		mean[i] = float32(m[i])
		std[i] = float32(math.Sqrt(v[i] / float64(len(samples))))
		if std[i] < 1e-6 {
			std[i] = 0
		}
	}
	return mean, std
}

// fitLR 小批量梯度下降（AdaGrad 步长），正负样本按比例重加权，样本顺序按确定性洗牌。
func fitLR(samples []Sample, mean, std []float32, p RankerParams) ([]float32, float32) {
	n := len(mean)
	w := make([]float64, n)
	var b float64
	pos := 0
	for _, s := range samples {
		if s.Y {
			pos++
		}
	}
	wPos := float64(len(samples)) / (2 * float64(max(pos, 1)))
	wNeg := float64(len(samples)) / (2 * float64(max(len(samples)-pos, 1)))
	gw := make([]float64, n)
	var gb float64
	order := make([]int, len(samples))
	for i := range order {
		order[i] = i
	}
	x := make([]float64, n)
	const lr, batch = 0.1, 256
	gradW := make([]float64, n)
	epochs := max(p.Epochs, 1)
	for e := 0; e < epochs; e++ {
		// 确定性洗牌（线性同余）
		seed := uint64(e*7919 + 17)
		for i := len(order) - 1; i > 0; i-- {
			seed = seed*6364136223846793005 + 1442695040888963407
			j := int(seed>>33) % (i + 1)
			order[i], order[j] = order[j], order[i]
		}
		for start := 0; start < len(order); start += batch {
			end := min(start+batch, len(order))
			for i := range gradW {
				gradW[i] = 0
			}
			var gradB float64
			for _, idx := range order[start:end] {
				s := samples[idx]
				z := b
				for i := 0; i < n; i++ {
					v := 0.0
					if std[i] > 0 {
						v = (float64(s.X[i]) - float64(mean[i])) / float64(std[i])
					}
					x[i] = v
					z += w[i] * v
				}
				pr := 1 / (1 + math.Exp(-z))
				y, sw := 0.0, wNeg
				if s.Y {
					y, sw = 1, wPos
				}
				g := (pr - y) * sw
				for i := 0; i < n; i++ {
					gradW[i] += g * x[i]
				}
				gradB += g
			}
			m := float64(end - start)
			for i := 0; i < n; i++ {
				gi := gradW[i]/m + p.L2*w[i]
				gw[i] += gi * gi
				w[i] -= lr * gi / (math.Sqrt(gw[i]) + 1e-8)
			}
			gbi := gradB / m
			gb += gbi * gbi
			b -= lr * gbi / (math.Sqrt(gb) + 1e-8)
		}
	}
	out := make([]float32, n)
	for i, v := range w {
		out[i] = float32(v)
	}
	return out, float32(b)
}

// AUC 排序 AUC（并列取平均秩）。只有一类标签时返回 NaN。
func AUC(scores []float64, labels []bool) float64 {
	idx := make([]int, len(scores))
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return scores[idx[a]] < scores[idx[b]] })
	var pos, neg float64
	var rankSum float64
	for i := 0; i < len(idx); {
		j := i
		for j+1 < len(idx) && scores[idx[j+1]] == scores[idx[i]] {
			j++
		}
		avg := float64(i+j)/2 + 1
		for k := i; k <= j; k++ {
			if labels[idx[k]] {
				rankSum += avg
				pos++
			} else {
				neg++
			}
		}
		i = j + 1
	}
	if pos == 0 || neg == 0 {
		return math.NaN()
	}
	return (rankSum - pos*(pos+1)/2) / (pos * neg)
}
