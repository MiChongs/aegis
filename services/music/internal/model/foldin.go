package model

import (
	"math"
	"sort"
)

// FoldIn 实时折入：固定作品向量 Y，按这个人当下的正反馈解一次隐式 ALS 的最小二乘，得到口味向量。
//
//	(YᵀY + Σ (cᵢ − 1) yᵢ yᵢᵀ + λI) x = Σ cᵢ yᵢ        （pᵢ = 1）
//
// conf 是每个作品的置信度 c（≥ 1）。没有可用作品或解不出时返回 nil。f ≤ 256，Cholesky 一次 < 1 毫秒。
func (s *Snapshot) FoldIn(items []int32, conf []float32) []float32 {
	f := s.Factors
	if len(items) == 0 || len(s.Gram) != f*f {
		return nil
	}
	a := make([]float64, f*f)
	for i, v := range s.Gram {
		a[i] = float64(v)
	}
	lambda := float64(s.Lambda)
	if lambda <= 0 {
		lambda = 0.05
	}
	// 与训练时 cgSolve 相同的正则形式（A = YᵀY + λI + Σ (c−1) y yᵀ）
	for d := 0; d < f; d++ {
		a[d*f+d] += lambda
	}
	b := make([]float64, f)
	used := 0
	for k, it := range items {
		if it < 0 || int(it) >= len(s.Items) {
			continue
		}
		c := float64(conf[k])
		if c < 1 {
			c = 1
		}
		y := s.ItemVec(it)
		for p := 0; p < f; p++ {
			yp := float64(y[p])
			if yp == 0 {
				continue
			}
			b[p] += c * yp
			row := a[p*f : (p+1)*f]
			for q := 0; q < f; q++ {
				row[q] += (c - 1) * yp * float64(y[q])
			}
		}
		used++
	}
	if used == 0 {
		return nil
	}
	x, ok := choleskySolve(a, b, f)
	if !ok {
		return nil
	}
	out := make([]float32, f)
	for i, v := range x {
		out[i] = float32(v)
	}
	return out
}

// choleskySolve 解对称正定方程组 A x = b（A 被原地分解）。
func choleskySolve(a, b []float64, n int) ([]float64, bool) {
	for j := 0; j < n; j++ {
		sum := a[j*n+j]
		for k := 0; k < j; k++ {
			sum -= a[j*n+k] * a[j*n+k]
		}
		if sum <= 1e-12 {
			return nil, false
		}
		d := math.Sqrt(sum)
		a[j*n+j] = d
		for i := j + 1; i < n; i++ {
			s := a[i*n+j]
			for k := 0; k < j; k++ {
				s -= a[i*n+k] * a[j*n+k]
			}
			a[i*n+j] = s / d
		}
	}
	y := make([]float64, n)
	for i := 0; i < n; i++ {
		s := b[i]
		for k := 0; k < i; k++ {
			s -= a[i*n+k] * y[k]
		}
		y[i] = s / a[i*n+i]
	}
	x := make([]float64, n)
	for i := n - 1; i >= 0; i-- {
		s := y[i]
		for k := i + 1; k < n; k++ {
			s -= a[k*n+i] * x[k]
		}
		x[i] = s / a[i*n+i]
	}
	return x, true
}

// artistSimilarity 歌手相似度：每人加权 Top 歌手（userArtists 的结果）按共同听众算余弦，听得杂的人权重小（IUF）。
func artistSimilarity(users [][]ArtistWeight, k int) map[string][]ArtistNeighbor {
	type pair struct{ a, b string }
	acc := map[pair]float64{}
	norm := map[string]float64{}
	for _, list := range users {
		if len(list) < 2 {
			continue
		}
		iuf := 1 / math.Log(2+float64(len(list)))
		for x := range list {
			wa := float64(list[x].Weight)
			norm[list[x].Artist] += wa * wa * iuf
			for y := x + 1; y < len(list); y++ {
				v := wa * float64(list[y].Weight) * iuf
				a, b := list[x].Artist, list[y].Artist
				if a > b {
					a, b = b, a
				}
				acc[pair{a, b}] += v
			}
		}
	}
	nb := map[string][]ArtistNeighbor{}
	for p, v := range acc {
		d := math.Sqrt(norm[p.a] * norm[p.b])
		if d <= 0 {
			continue
		}
		sc := float32(v / d)
		nb[p.a] = append(nb[p.a], ArtistNeighbor{Artist: p.b, Score: sc})
		nb[p.b] = append(nb[p.b], ArtistNeighbor{Artist: p.a, Score: sc})
	}
	for a, list := range nb {
		sort.Slice(list, func(x, y int) bool {
			if list[x].Score != list[y].Score {
				return list[x].Score > list[y].Score
			}
			return list[x].Artist < list[y].Artist
		})
		if len(list) > k {
			list = list[:k]
		}
		nb[a] = list
	}
	return nb
}

// KMeans 余弦 k-means（向量先归一化），返回每个点的簇号；确定性初始化（k-means++ 用固定种子的顺序）。
func KMeans(vecs [][]float32, k, iters int) []int {
	n := len(vecs)
	if n == 0 || k <= 0 {
		return nil
	}
	if k > n {
		k = n
	}
	f := len(vecs[0])
	unit := make([][]float32, n)
	for i, v := range vecs {
		u := make([]float32, f)
		var s float64
		for _, x := range v {
			s += float64(x) * float64(x)
		}
		d := float32(math.Sqrt(s))
		if d > 0 {
			for j, x := range v {
				u[j] = x / d
			}
		}
		unit[i] = u
	}
	// 初始化：第一个中心取第 0 个，之后每次取离已选中心最远（余弦最小）的点
	centers := [][]float32{append([]float32(nil), unit[0]...)}
	for len(centers) < k {
		best, bestD := -1, float32(2)
		for i, u := range unit {
			var nearest float32 = -2
			for _, c := range centers {
				nearest = max(nearest, Dot(u, c))
			}
			if nearest < bestD {
				best, bestD = i, nearest
			}
		}
		centers = append(centers, append([]float32(nil), unit[best]...))
	}
	assign := make([]int, n)
	for it := 0; it < iters; it++ {
		changed := false
		for i, u := range unit {
			best, bestS := 0, float32(-2)
			for c, ctr := range centers {
				if s := Dot(u, ctr); s > bestS {
					best, bestS = c, s
				}
			}
			if assign[i] != best || it == 0 {
				changed = changed || assign[i] != best
				assign[i] = best
			}
		}
		for c := range centers {
			sum := make([]float64, f)
			cnt := 0
			for i, u := range unit {
				if assign[i] == c {
					cnt++
					for j, x := range u {
						sum[j] += float64(x)
					}
				}
			}
			if cnt == 0 {
				continue
			}
			var s float64
			for _, x := range sum {
				s += x * x
			}
			d := math.Sqrt(s)
			for j := range sum {
				if d > 0 {
					centers[c][j] = float32(sum[j] / d)
				}
			}
		}
		if !changed && it > 0 {
			break
		}
	}
	return assign
}
