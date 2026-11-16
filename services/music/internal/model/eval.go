package model

import (
	"context"
	"math"
	"math/rand/v2"
	"sort"

	"aegis/services/music/internal/appcfg"
	"aegis/services/music/internal/store"
)

// 离线评估：每人按时间留出最后 20% 的正反馈作测试集（至少 5 个正反馈才参与），
// 其余数据训练 ALS + ItemCF，按融合权重打分取前 20，与「热度」基线比较。
const (
	evalK        = 20
	evalMinPos   = 5
	evalHoldout  = 0.2
	evalMaxUsers = 2000
)

// Evaluate 评估。矩阵太小（测试用户 < 10）时返回 nil（不评估，自动发布不设门槛）。
func Evaluate(ctx context.Context, m *Matrix, cfg appcfg.Config, workers int, seed uint64) (*store.Metrics, error) {
	train := make([][]Entry, len(m.Rows))
	tests := map[int][]int32{}
	for u, row := range m.Rows {
		var pos []Entry
		for _, e := range row {
			if e.Score > 0 {
				pos = append(pos, e)
			}
		}
		if len(pos) < evalMinPos {
			train[u] = row
			continue
		}
		sort.Slice(pos, func(a, b int) bool { return pos[a].Last < pos[b].Last })
		n := int(math.Ceil(float64(len(pos)) * evalHoldout))
		held := map[int32]bool{}
		for _, e := range pos[len(pos)-n:] {
			held[e.Item] = true
			tests[u] = append(tests[u], e.Item)
		}
		kept := make([]Entry, 0, len(row)-n)
		for _, e := range row {
			if !held[e.Item] {
				kept = append(kept, e)
			}
		}
		train[u] = kept
	}
	if len(tests) < 10 {
		return nil, nil
	}
	nItems := len(m.Items)
	als, err := TrainALS(ctx, train, nItems, cfg.ALS, workers, seed)
	if err != nil {
		return nil, err
	}
	nb, err := TrainItemCF(ctx, train, nItems, nil, cfg.ItemCF, workers)
	if err != nil {
		return nil, err
	}
	// 基线：训练集里的正反馈人数
	popCount := make([]float32, nItems)
	for _, row := range train {
		for _, e := range row {
			if e.Score > 0 {
				popCount[e.Item]++
			}
		}
	}
	popOrder := make([]int32, nItems)
	for i := range popOrder {
		popOrder[i] = int32(i)
	}
	sort.Slice(popOrder, func(a, b int) bool {
		if popCount[popOrder[a]] != popCount[popOrder[b]] {
			return popCount[popOrder[a]] > popCount[popOrder[b]]
		}
		return popOrder[a] < popOrder[b]
	})

	users := make([]int, 0, len(tests))
	for u := range tests {
		users = append(users, u)
	}
	sort.Ints(users)
	if len(users) > evalMaxUsers {
		rng := rand.New(rand.NewPCG(seed, 7))
		rng.Shuffle(len(users), func(a, b int) { users[a], users[b] = users[b], users[a] })
		users = users[:evalMaxUsers]
	}
	f := cfg.ALS.Factors
	var sum, base metricSum
	covered, baseCovered := map[int32]bool{}, map[int32]bool{}
	scores := make([]float32, nItems)
	cf := make([]float32, nItems)
	for _, u := range users {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		seen := map[int32]bool{}
		var recentPos []Entry
		for _, e := range train[u] {
			seen[e.Item] = true
			if e.Score > 0 {
				recentPos = append(recentPos, e)
			}
		}
		sort.Slice(recentPos, func(a, b int) bool { return recentPos[a].Last > recentPos[b].Last })
		if len(recentPos) > 50 {
			recentPos = recentPos[:50]
		}
		x := als.X[u*f : (u+1)*f]
		var maxALS, maxCF float32
		for i := 0; i < nItems; i++ {
			s := Dot(x, als.Y[i*f:(i+1)*f])
			scores[i] = s
			if s > maxALS {
				maxALS = s
			}
			cf[i] = 0
		}
		for rank, e := range recentPos {
			w := float32(1 / (1 + 0.1*float64(rank)))
			for _, n := range nb[e.Item] {
				cf[n.Item] += n.Score * w
			}
		}
		for i := range cf {
			if cf[i] > maxCF {
				maxCF = cf[i]
			}
		}
		for i := range scores {
			v := float32(0)
			if maxALS > 0 && scores[i] > 0 {
				v += float32(cfg.Blend.ALS) * scores[i] / maxALS
			}
			if maxCF > 0 {
				v += float32(cfg.Blend.ItemCF) * cf[i] / maxCF
			}
			scores[i] = v
		}
		top := topK(scores, seen, evalK)
		truth := map[int32]bool{}
		for _, i := range tests[u] {
			truth[i] = true
		}
		sum.add(top, truth)
		for _, i := range top {
			covered[i] = true
		}
		var baseTop []int32
		for _, i := range popOrder {
			if !seen[i] {
				baseTop = append(baseTop, i)
				if len(baseTop) == evalK {
					break
				}
			}
		}
		base.add(baseTop, truth)
		for _, i := range baseTop {
			baseCovered[i] = true
		}
	}
	n := float64(len(users))
	cover := func(c map[int32]bool) float64 { return float64(len(c)) / math.Max(float64(nItems), 1) }
	return &store.Metrics{
		Recall20: sum.recall / n, NDCG20: sum.ndcg / n, HitRate20: sum.hit / n, Coverage: cover(covered),
		Baseline:  store.BaseMetrics{Recall20: base.recall / n, NDCG20: base.ndcg / n, HitRate20: base.hit / n, Coverage: cover(baseCovered)},
		TestUsers: len(users),
	}, nil
}

type metricSum struct{ recall, ndcg, hit float64 }

func (m *metricSum) add(top []int32, truth map[int32]bool) {
	if len(truth) == 0 {
		return
	}
	hits := 0
	dcg := 0.0
	for rank, i := range top {
		if truth[i] {
			hits++
			dcg += 1 / math.Log2(float64(rank)+2)
		}
	}
	idcg := 0.0
	for r := 0; r < min(len(truth), evalK); r++ {
		idcg += 1 / math.Log2(float64(r)+2)
	}
	m.recall += float64(hits) / float64(min(len(truth), evalK))
	if idcg > 0 {
		m.ndcg += dcg / idcg
	}
	if hits > 0 {
		m.hit++
	}
}

// topK 分数最高的 k 个（跳过 seen）。
func topK(scores []float32, seen map[int32]bool, k int) []int32 {
	type pair struct {
		i int32
		s float32
	}
	heap := make([]pair, 0, k)
	minIdx := func() int {
		m := 0
		for j := 1; j < len(heap); j++ {
			if heap[j].s < heap[m].s {
				m = j
			}
		}
		return m
	}
	for i, s := range scores {
		if s <= 0 || seen[int32(i)] {
			continue
		}
		if len(heap) < k {
			heap = append(heap, pair{int32(i), s})
			continue
		}
		if m := minIdx(); s > heap[m].s {
			heap[m] = pair{int32(i), s}
		}
	}
	sort.Slice(heap, func(a, b int) bool {
		if heap[a].s != heap[b].s {
			return heap[a].s > heap[b].s
		}
		return heap[a].i < heap[b].i
	})
	out := make([]int32, len(heap))
	for j, p := range heap {
		out[j] = p.i
	}
	return out
}
