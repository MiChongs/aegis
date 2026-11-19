package lottery

import (
	"math"

	"gonum.org/v1/gonum/mat"
)

// 信息投影（I-projection）。
//
// 问题：给定当前分布 q（各维度系数之后的权重），找满足一组线性不等式约束 A·p ≤ b 的分布 p，
// 使 KL(p ‖ q) 最小 —— 在所有满足约束的分布里，它是「对 q 改动最少、不额外引入任何信息」的那个
// （最小鉴别信息原理，Kullback；Csiszár 1975 证明了线性族上 I-投影的存在唯一性）。
//
// 由拉格朗日对偶，解一定是指数族形式：
//
//	p_i(λ) = q_i · exp(−Σ_j λ_j · a_ji) / Z(λ),   λ ≥ 0
//
// 对偶函数 D(λ) = −log Z(λ) − λ·b 是凹函数，梯度 ∇D = E_p[a] − b，Hessian = −Cov_p(a)。
// 在 λ ≥ 0 上做带有效集的投影牛顿法（Bertsekas 1982）：约束不紧（λ_j = 0 且梯度 < 0）的维度固定为 0，
// 其余维度用 Cov_p(a) 求牛顿方向，回溯线搜索保证对偶函数单调上升。KKT 条件满足时停止。
//
// 只有 q_i > 0 的奖品能分到概率（I-投影不会凭空给被挡掉的奖品概率），约束在这个支撑集上不可行时
// 返回 feasible = false，并给出尽力而为的结果（对偶变量截断在上界）。

// linearConstraint 一条 a·p ≤ b；eq 为真时是等式 a·p = b（对偶变量不限符号，恒在自由集里）。
// 护栏只用不等式；奖盘求解（solver.go）把「综合中奖率等于目标」这类要求写成等式。
type linearConstraint struct {
	a  []float64
	b  float64
	eq bool
}

const (
	projectionIterations = 100
	projectionTolerance  = 1e-9
	projectionMaxLambda  = 1e4
)

// iProject 返回投影后的权重（总和与 q 相同）以及约束是否可行。
func iProject(q []float64, constraints []linearConstraint) ([]float64, bool) {
	n, m := len(q), len(constraints)
	if m == 0 {
		return append([]float64(nil), q...), true
	}
	var total float64
	for _, w := range q {
		total += w
	}
	if total <= 0 {
		return append([]float64(nil), q...), true
	}
	base := make([]float64, n)
	for i, w := range q {
		base[i] = w / total
	}

	// 对数域计算，避免大 λ 时 exp 溢出
	distribution := func(lambda []float64) []float64 {
		logs := make([]float64, n)
		maxLog := math.Inf(-1)
		for i := range base {
			if base[i] <= 0 {
				logs[i] = math.Inf(-1)
				continue
			}
			l := math.Log(base[i])
			for j, c := range constraints {
				l -= lambda[j] * c.a[i]
			}
			logs[i] = l
			maxLog = math.Max(maxLog, l)
		}
		p := make([]float64, n)
		var sum float64
		for i, l := range logs {
			if math.IsInf(l, -1) {
				continue
			}
			p[i] = math.Exp(l - maxLog)
			sum += p[i]
		}
		for i := range p {
			p[i] /= sum
		}
		return p
	}
	// 对偶函数 D(λ) = −log Σ q_i exp(−λ·a_i) − λ·b
	dual := func(lambda []float64) float64 {
		maxLog := math.Inf(-1)
		logs := make([]float64, n)
		for i := range base {
			if base[i] <= 0 {
				logs[i] = math.Inf(-1)
				continue
			}
			l := math.Log(base[i])
			for j, c := range constraints {
				l -= lambda[j] * c.a[i]
			}
			logs[i] = l
			maxLog = math.Max(maxLog, l)
		}
		var sum float64
		for _, l := range logs {
			if !math.IsInf(l, -1) {
				sum += math.Exp(l - maxLog)
			}
		}
		value := -(maxLog + math.Log(sum))
		for j, c := range constraints {
			value -= lambda[j] * c.b
		}
		return value
	}
	moments := func(p []float64) ([]float64, *mat.SymDense) {
		mean := make([]float64, m)
		for j, c := range constraints {
			for i := range p {
				mean[j] += p[i] * c.a[i]
			}
		}
		cov := mat.NewSymDense(m, nil)
		for j := 0; j < m; j++ {
			for k := j; k < m; k++ {
				var s float64
				for i := range p {
					s += p[i] * (constraints[j].a[i] - mean[j]) * (constraints[k].a[i] - mean[k])
				}
				cov.SetSym(j, k, s)
			}
		}
		return mean, cov
	}

	lambda := make([]float64, m)
	for range projectionIterations {
		p := distribution(lambda)
		mean, cov := moments(p)
		grad := make([]float64, m)
		for j, c := range constraints {
			grad[j] = mean[j] - c.b // ∂D/∂λ_j
		}
		// KKT：λ_j > 0 时梯度为 0；λ_j = 0 时梯度 ≤ 0（约束满足）
		free := make([]int, 0, m)
		optimal := true
		for j := range lambda {
			if constraints[j].eq || lambda[j] > 0 || grad[j] > 0 {
				free = append(free, j)
				if math.Abs(grad[j]) > projectionTolerance {
					optimal = false
				}
			}
		}
		if optimal {
			return scaleTo(p, total), true
		}
		// 自由维度上的牛顿方向：Cov · d = grad（加一点岭项防奇异）
		k := len(free)
		h := mat.NewSymDense(k, nil)
		g := mat.NewVecDense(k, nil)
		for x, j := range free {
			g.SetVec(x, grad[j])
			for y, l := range free {
				if y >= x {
					h.SetSym(x, y, cov.At(j, l))
				}
			}
			h.SetSym(x, x, h.At(x, x)+1e-12)
		}
		var direction mat.VecDense
		var chol mat.Cholesky
		if chol.Factorize(h) {
			if err := chol.SolveVecTo(&direction, g); err != nil {
				direction.CloneFromVec(g)
			}
		} else {
			direction.CloneFromVec(g) // 退化时用梯度方向
		}
		// 回溯线搜索：投影到 λ ≥ 0 后对偶函数必须上升
		current := dual(lambda)
		step := 1.0
		improved := false
		for range 50 {
			next := append([]float64(nil), lambda...)
			for x, j := range free {
				floor := 0.0
				if constraints[j].eq {
					floor = -projectionMaxLambda
				}
				next[j] = math.Min(projectionMaxLambda, math.Max(floor, lambda[j]+step*direction.AtVec(x)))
			}
			if dual(next) > current+1e-15 {
				lambda = next
				improved = true
				break
			}
			step /= 2
		}
		if !improved {
			break
		}
	}
	p := distribution(lambda)
	feasible := true
	for _, c := range constraints {
		var s float64
		for i := range p {
			s += p[i] * c.a[i]
		}
		if s > c.b+1e-6 || c.eq && s < c.b-1e-6 {
			feasible = false
		}
	}
	return scaleTo(p, total), feasible
}

func scaleTo(p []float64, total float64) []float64 {
	out := make([]float64, len(p))
	for i := range p {
		out[i] = p[i] * total
	}
	return out
}
