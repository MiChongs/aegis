package montecarlo

import (
	"math"
	"slices"

	"github.com/DataDog/sketches-go/ddsketch"
	"gonum.org/v1/gonum/stat/distuv"
)

// ── 均值与置信区间 ──

// Moments 在线均值与方差（Welford 递推），可合并。零值可用。
type Moments struct {
	N    int64
	Mean float64
	// m2 离差平方和 Σ(x − mean)²
	m2       float64
	Min, Max float64
}

// Add 加入一个观测值。
func (m *Moments) Add(x float64) {
	if m.N == 0 {
		m.Min, m.Max = x, x
	} else {
		m.Min, m.Max = math.Min(m.Min, x), math.Max(m.Max, x)
	}
	m.N++
	delta := x - m.Mean
	m.Mean += delta / float64(m.N)
	m.m2 += delta * (x - m.Mean)
}

// Merge 并入另一组观测（Chan、Golub、LeVeque 的成对合并公式）。
func (m *Moments) Merge(o Moments) {
	if o.N == 0 {
		return
	}
	if m.N == 0 {
		*m = o
		return
	}
	n := float64(m.N + o.N)
	delta := o.Mean - m.Mean
	m.m2 += o.m2 + delta*delta*float64(m.N)*float64(o.N)/n
	m.Mean += delta * float64(o.N) / n
	m.N += o.N
	m.Min, m.Max = math.Min(m.Min, o.Min), math.Max(m.Max, o.Max)
}

// Variance 样本方差（除以 n − 1）；不足两个观测时为 0。
func (m Moments) Variance() float64 {
	if m.N < 2 {
		return 0
	}
	return m.m2 / float64(m.N-1)
}

// Std 样本标准差。
func (m Moments) Std() float64 { return math.Sqrt(m.Variance()) }

// StdErr 均值的标准误。
func (m Moments) StdErr() float64 {
	if m.N < 2 {
		return 0
	}
	return math.Sqrt(m.Variance() / float64(m.N))
}

// Estimate 一个指标的估计：均值、标准误与置信区间。
type Estimate struct {
	Mean   float64 `json:"mean"`
	StdErr float64 `json:"stdErr"`
	// Low / High 置信区间；只有一次重复实验时与 Mean 相同（给不出区间）
	Low  float64 `json:"low"`
	High float64 `json:"high"`
	// N 用了几次重复实验
	N int64 `json:"n"`
}

// RelativeError 置信区间半宽相对均值的比例（0 表示均值为 0 或没有区间）。
func (e Estimate) RelativeError() float64 {
	if e.Mean == 0 {
		return 0
	}
	return math.Abs((e.High - e.Low) / 2 / e.Mean)
}

// Estimate 均值的置信区间：重复实验互相独立、各自是同一个量的无偏估计，n 次的均值按 Student t 分布取区间
// （自由度 n − 1）。level 为置信水平，如 0.95。
func (m Moments) Estimate(level float64) Estimate {
	out := Estimate{Mean: m.Mean, Low: m.Mean, High: m.Mean, N: m.N}
	if m.N < 2 {
		return out
	}
	out.StdErr = m.StdErr()
	if out.StdErr == 0 {
		return out
	}
	t := distuv.StudentsT{Mu: 0, Sigma: 1, Nu: float64(m.N - 1)}.Quantile(1 - (1-level)/2)
	out.Low, out.High = m.Mean-t*out.StdErr, m.Mean+t*out.StdErr
	return out
}

// ── 整数直方图 ──

// Histogram 非负整数的直方图：计数精确，分位数精确，可合并。零值可用。
// 取值密集的小整数（次数、连败长度）放在切片里，超过 histogramDense 的放在稀疏表里。
type Histogram struct {
	dense  []int64
	sparse map[int]int64
	total  int64
	sum    float64
	max    int
}

const histogramDense = 4096

// Add 记一个取值（负数按 0 记）。
func (h *Histogram) Add(value int) { h.AddN(value, 1) }

// AddN 记 count 个相同的取值。
func (h *Histogram) AddN(value int, count int64) {
	if count <= 0 {
		return
	}
	value = max(value, 0)
	if value < histogramDense {
		if value >= len(h.dense) {
			h.dense = append(h.dense, make([]int64, value+1-len(h.dense))...)
		}
		h.dense[value] += count
	} else {
		if h.sparse == nil {
			h.sparse = map[int]int64{}
		}
		h.sparse[value] += count
	}
	h.total += count
	h.sum += float64(value) * float64(count)
	h.max = max(h.max, value)
}

// Merge 并入另一个直方图。
func (h *Histogram) Merge(o *Histogram) {
	if o == nil {
		return
	}
	for value, count := range o.dense {
		h.AddN(value, count)
	}
	for value, count := range o.sparse {
		h.AddN(value, count)
	}
}

// Count 观测个数。
func (h *Histogram) Count() int64 { return h.total }

// Mean 均值。
func (h *Histogram) Mean() float64 {
	if h.total == 0 {
		return 0
	}
	return h.sum / float64(h.total)
}

// Max 最大取值。
func (h *Histogram) Max() int { return h.max }

// Quantile 最小的 v 使 P(X ≤ v) ≥ q。
func (h *Histogram) Quantile(q float64) int {
	if h.total == 0 {
		return 0
	}
	need := int64(math.Ceil(q * float64(h.total)))
	need = min(max(need, 1), h.total)
	var seen int64
	for value, count := range h.dense {
		if seen += count; seen >= need {
			return value
		}
	}
	keys := make([]int, 0, len(h.sparse))
	for value := range h.sparse {
		keys = append(keys, value)
	}
	slices.Sort(keys)
	for _, value := range keys {
		if seen += h.sparse[value]; seen >= need {
			return value
		}
	}
	return h.max
}

// Bucket 直方图的一段：取值在 [From, To] 里的有 Count 个。
type Bucket struct {
	From  int   `json:"from"`
	To    int   `json:"to"`
	Count int64 `json:"count"`
}

// Buckets 压成最多 limit 段（等宽）供画图；取值种数不超过 limit 时每个取值一段。
func (h *Histogram) Buckets(limit int) []Bucket {
	if h.total == 0 || limit <= 0 {
		return []Bucket{}
	}
	width := max(1, (h.max+limit)/limit)
	out := make([]Bucket, 0, min(limit, h.max+1))
	add := func(value int, count int64) {
		if count == 0 {
			return
		}
		index := value / width
		for len(out) <= index {
			from := len(out) * width
			out = append(out, Bucket{From: from, To: min(from+width-1, h.max)})
		}
		out[index].Count += count
	}
	for value, count := range h.dense {
		add(value, count)
	}
	for value, count := range h.sparse {
		add(value, count)
	}
	return out
}

// ── 连续量的分位数 ──

// Sketch 连续非负量的分位数草图（DDSketch，Masson、Rim、Lee，VLDB 2019）：任何分位数的相对误差不超过
// 建图时给的精度，桶数随取值跨度的对数增长，两张图可以无损合并。用在「每人拿到的价值」这类
// 取值连续、人数上百万、存不下全部观测的地方。
type Sketch struct {
	inner   *ddsketch.DDSketch
	moments Moments
}

// SketchAccuracy 草图的相对精度：分位数的相对误差不超过 0.5%。
const SketchAccuracy = 0.005

// NewSketch 建一张空草图。
func NewSketch() *Sketch {
	inner, err := ddsketch.NewDefaultDDSketch(SketchAccuracy)
	if err != nil {
		// 精度是常数，在 (0, 1) 之内，不会走到这里
		panic(err)
	}
	return &Sketch{inner: inner}
}

// Add 加入一个观测值（负数按 0 记）。
func (s *Sketch) Add(x float64) {
	x = math.Max(x, 0)
	_ = s.inner.Add(x)
	s.moments.Add(x)
}

// Merge 并入另一张草图。
func (s *Sketch) Merge(o *Sketch) {
	if o == nil || o.moments.N == 0 {
		return
	}
	_ = s.inner.MergeWith(o.inner)
	s.moments.Merge(o.moments)
}

// Count 观测个数。
func (s *Sketch) Count() int64 { return s.moments.N }

// Mean 均值（精确）。
func (s *Sketch) Mean() float64 { return s.moments.Mean }

// Max 最大值（精确）。
func (s *Sketch) Max() float64 { return s.moments.Max }

// Quantile 分位数；没有观测时为 0。
func (s *Sketch) Quantile(q float64) float64 {
	if s.moments.N == 0 {
		return 0
	}
	value, err := s.inner.GetValueAtQuantile(q)
	if err != nil {
		return 0
	}
	// 草图在桶内取代表值，可能略微越过真实的极值
	return math.Min(math.Max(value, s.moments.Min), s.moments.Max)
}

// ── 比例的区间 ──

// Wilson 二项比例的 Wilson 得分区间（successes / trials）。比例接近 0 或 1、样本不多时
// 比正态近似可靠：十万分之一的大奖抽了一百万次只出 8 个，照样给得出不含负数的区间。
func Wilson(successes, trials int64, level float64) (low, high float64) {
	if trials <= 0 {
		return 0, 0
	}
	z := distuv.UnitNormal.Quantile(1 - (1-level)/2)
	n := float64(trials)
	p := float64(successes) / n
	denominator := 1 + z*z/n
	center := (p + z*z/(2*n)) / denominator
	half := z * math.Sqrt(p*(1-p)/n+z*z/(4*n*n)) / denominator
	return math.Max(0, center-half), math.Min(1, center+half)
}
