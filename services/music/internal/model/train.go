package model

import (
	"context"
	"math"
	"math/rand/v2"
	"runtime"
	"slices"
	"sort"
	"sync"
	"time"

	"aegis/services/music/contract"
	"aegis/services/music/internal/appcfg"
	"aegis/services/music/internal/signal"
	"aegis/services/music/internal/store"
)

// Entry 一个人对一个作品的累计偏好。
type Entry struct {
	Item  int32
	Score float32 // 衰减后的偏好分（正 = 喜欢，负 = 不喜欢）
	Last  int64   // 最后一次相关事件（毫秒）
}

// itemStat 作品统计（热度、筛选用）。
type itemStat struct {
	key, title, artist, artistN   string
	posUsers                      int32 // 窗口内正反馈人数
	listeners7, completes7        int32
	plays7                        int32
	listeners1, listenersPrev     int32
	plays30, completes30, skips30 int32
	lang                          uint8
}

// Builder 从按 (user, ts) 排好序的事件流累计交互矩阵。
type Builder struct {
	cfg   appcfg.Config
	now   int64
	day   int64
	items map[string]int32
	stats []itemStat

	users []int64
	rows  [][]Entry
	trans map[uint64]float32

	curUser int64
	cur     map[int32]*Entry
	seen7   map[int32]bool
	seen1   map[int32]bool
	seenP   map[int32]bool
	prevPos int32
	prevAt  int64
	prevSes string
	events  int64
}

// NewBuilder 新建。
func NewBuilder(cfg appcfg.Config, now time.Time) *Builder {
	return &Builder{cfg: cfg, now: now.UnixMilli(), day: 24 * 3600 * 1000, items: map[string]int32{},
		trans: map[uint64]float32{}, curUser: -1, prevPos: -1}
}

// Events 已读入的事件数。
func (b *Builder) Events() int64 { return b.events }

func (b *Builder) item(r *store.InteractionRow) int32 {
	if i, ok := b.items[r.WorkKey]; ok {
		if b.stats[i].title == "" && r.Title != "" {
			b.stats[i].title = r.Title
		}
		return i
	}
	i := int32(len(b.stats))
	b.items[r.WorkKey] = i
	first := contract.FirstArtist(r.Artist)
	b.stats = append(b.stats, itemStat{key: r.WorkKey, title: r.Title, artist: contract.NormalizeArtist(first), artistN: first,
		lang: uint8(contract.LangIndex(contract.DetectLang(r.Title, r.Artist)))})
	return i
}

// Add 读入一条事件。
func (b *Builder) Add(r *store.InteractionRow) {
	b.events++
	if r.UserID != b.curUser {
		b.flush()
		b.curUser = r.UserID
		b.cur = map[int32]*Entry{}
		b.seen7, b.seen1, b.seenP = map[int32]bool{}, map[int32]bool{}, map[int32]bool{}
		b.prevPos, b.prevAt, b.prevSes = -1, 0, ""
	}
	i := b.item(r)
	ev := contract.Event{Type: r.Type, PlayedMs: r.PlayedMs, PositionMs: r.PositionMs, Reason: r.Reason,
		Track: &contract.Track{DurationMs: r.DurationMs},
		Props: map[string]string{"from": r.SeekFrom, "reason": r.DislikeReason, "scope": r.DislikeScope}}
	k := signal.Classify(&ev)
	if k == signal.KindNone {
		return
	}
	age := float64(b.now-r.TS) / float64(b.day)
	score := signal.Weight(k, b.cfg.Weights) * signal.Decay(age, b.cfg.HalfLifeDays)
	e := b.cur[i]
	if e == nil {
		e = &Entry{Item: i}
		b.cur[i] = e
	}
	e.Score += float32(score)
	if r.TS > e.Last {
		e.Last = r.TS
	}
	if r.Type == contract.TypePlayEnd && age <= 30 && (k == signal.KindComplete || k == signal.KindPartial || k == signal.KindSkip) {
		st := &b.stats[i]
		st.plays30++
		switch k {
		case signal.KindComplete:
			st.completes30++
		case signal.KindSkip:
			st.skips30++
		}
	}
	if r.Type == contract.TypePlayEnd && (k == signal.KindComplete || k == signal.KindPartial) {
		st := &b.stats[i]
		if age <= 7 {
			if !b.seen7[i] {
				b.seen7[i] = true
				st.listeners7++
			}
			st.plays7++
			if k == signal.KindComplete {
				st.completes7++
			}
			if age <= 1 {
				if !b.seen1[i] {
					b.seen1[i] = true
					st.listeners1++
				}
			} else if !b.seenP[i] {
				b.seenP[i] = true
				st.listenersPrev++
			}
		}
		// 会话内相邻的两次正向播放 → 「下一首」转移
		if b.prevPos >= 0 && b.prevPos != i && r.TS-b.prevAt <= 30*60*1000 && (r.SessionID == "" || r.SessionID == b.prevSes) {
			b.trans[uint64(uint32(b.prevPos))<<32|uint64(uint32(i))]++
		}
		b.prevPos, b.prevAt, b.prevSes = i, r.TS, r.SessionID
	}
}

func (b *Builder) flush() {
	if b.curUser < 0 || len(b.cur) == 0 {
		return
	}
	row := make([]Entry, 0, len(b.cur))
	for _, e := range b.cur {
		if e.Score == 0 {
			continue
		}
		row = append(row, *e)
		if e.Score > 0 {
			b.stats[e.Item].posUsers++
		}
	}
	if len(row) == 0 {
		return
	}
	b.users = append(b.users, b.curUser)
	b.rows = append(b.rows, row)
}

// Matrix 筛选后的交互矩阵（作品重新编号）。
type Matrix struct {
	Users    []int64
	Rows     [][]Entry
	Items    []itemStat
	Trans    map[uint64]float32
	Interact int64
}

// Finish 结束读入：去掉听众太少的作品与没有交互的人，重新编号。
func (b *Builder) Finish() *Matrix {
	b.flush()
	minUsers := int32(max(b.cfg.MinItemUsers, 1))
	remap := make([]int32, len(b.stats))
	var items []itemStat
	for i, st := range b.stats {
		if st.posUsers >= minUsers {
			remap[i] = int32(len(items))
			items = append(items, st)
		} else {
			remap[i] = -1
		}
	}
	m := &Matrix{Items: items, Trans: map[uint64]float32{}}
	for u, row := range b.rows {
		var kept []Entry
		for _, e := range row {
			if j := remap[e.Item]; j >= 0 {
				e.Item = j
				kept = append(kept, e)
			}
		}
		if len(kept) == 0 {
			continue
		}
		m.Users = append(m.Users, b.users[u])
		m.Rows = append(m.Rows, kept)
		m.Interact += int64(len(kept))
	}
	for k, v := range b.trans {
		i, j := remap[uint32(k>>32)], remap[uint32(k)]
		if i >= 0 && j >= 0 {
			m.Trans[uint64(uint32(i))<<32|uint64(uint32(j))] += v
		}
	}
	return m
}

// ── 隐式 ALS（共轭梯度） ──

type cell struct {
	idx int32
	c   float32 // 置信度
	p   float32 // 偏好 0 / 1
}

// ALSResult ALS 产物。
type ALSResult struct {
	Factors int
	X, Y    []float32 // 用户、作品向量
}

// TrainALS 隐式 ALS（Hu, Koren, Volinsky 2008），每次交替用 3 步共轭梯度近似求解（Takács 2011）。
func TrainALS(ctx context.Context, rows [][]Entry, nItems int, p appcfg.ALS, workers int, seed uint64) (*ALSResult, error) {
	f := p.Factors
	nUsers := len(rows)
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	userCells := make([][]cell, nUsers)
	itemCells := make([][]cell, nItems)
	for u, row := range rows {
		cs := make([]cell, 0, len(row))
		for _, e := range row {
			pref := float32(0)
			if e.Score > 0 {
				pref = 1
			}
			c := float32(signal.Confidence(float64(e.Score), p.Alpha))
			cs = append(cs, cell{idx: e.Item, c: c, p: pref})
			itemCells[e.Item] = append(itemCells[e.Item], cell{idx: int32(u), c: c, p: pref})
		}
		userCells[u] = cs
	}
	rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	init := func(n int) []float32 {
		v := make([]float32, n*f)
		for i := range v {
			v[i] = float32(rng.NormFloat64() * 0.01)
		}
		return v
	}
	X, Y := init(nUsers), init(nItems)
	reg := float32(p.Regularization)
	for it := 0; it < p.Iterations; it++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		cgSolve(X, Y, userCells, f, reg, workers)
		cgSolve(Y, X, itemCells, f, reg, workers)
	}
	return &ALSResult{Factors: f, X: X, Y: Y}, nil
}

// cgSolve 固定 other，按共轭梯度更新 target 的每一行。
func cgSolve(target, other []float32, cells [][]cell, f int, reg float32, workers int) {
	gram := gramian(other, f)
	var wg sync.WaitGroup
	next := make(chan int, 256)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := make([]float32, f)
			d := make([]float32, f)
			ad := make([]float32, f)
			for u := range next {
				x := target[u*f : (u+1)*f]
				cs := cells[u]
				// r = b − A x，A = YᵀY + λI + Σ (c−1) y yᵀ，b = Σ c·p·y
				matVec(gram, x, r, f)
				for k := range r {
					r[k] = -r[k] - reg*x[k]
				}
				for _, c := range cs {
					y := other[int(c.idx)*f : (int(c.idx)+1)*f]
					coef := c.c*c.p - (c.c-1)*Dot(y, x)
					for k := range r {
						r[k] += coef * y[k]
					}
				}
				copy(d, r)
				rs := Dot(r, r)
				if rs < 1e-20 {
					continue
				}
				for step := 0; step < 3; step++ {
					matVec(gram, d, ad, f)
					for k := range ad {
						ad[k] += reg * d[k]
					}
					for _, c := range cs {
						y := other[int(c.idx)*f : (int(c.idx)+1)*f]
						coef := (c.c - 1) * Dot(y, d)
						for k := range ad {
							ad[k] += coef * y[k]
						}
					}
					den := Dot(d, ad)
					if den <= 0 || math.IsNaN(float64(den)) {
						break
					}
					alpha := rs / den
					for k := range x {
						x[k] += alpha * d[k]
						r[k] -= alpha * ad[k]
					}
					rsNew := Dot(r, r)
					if rsNew < 1e-20 {
						break
					}
					beta := rsNew / rs
					for k := range d {
						d[k] = r[k] + beta*d[k]
					}
					rs = rsNew
				}
			}
		}()
	}
	for u := range cells {
		next <- u
	}
	close(next)
	wg.Wait()
}

func gramian(m []float32, f int) []float32 {
	g := make([]float64, f*f)
	n := len(m) / f
	for i := 0; i < n; i++ {
		v := m[i*f : (i+1)*f]
		for a := 0; a < f; a++ {
			va := float64(v[a])
			if va == 0 {
				continue
			}
			row := g[a*f : (a+1)*f]
			for b := 0; b < f; b++ {
				row[b] += va * float64(v[b])
			}
		}
	}
	out := make([]float32, f*f)
	for i, v := range g {
		out[i] = float32(v)
	}
	return out
}

func matVec(m, x, out []float32, f int) {
	for a := 0; a < f; a++ {
		out[a] = Dot(m[a*f:(a+1)*f], x)
	}
}

// ── ItemCF ──

// TrainItemCF 正反馈集合上的余弦相似度 + IUF 惩罚 + 会话转移加成，每个作品留前 K。
func TrainItemCF(ctx context.Context, rows [][]Entry, nItems int, trans map[uint64]float32, p appcfg.ItemCF, workers int) ([][]Neighbor, error) {
	const perUserCap = 300
	userItems := make([][]int32, len(rows))
	itemUsers := make([][]int32, nItems)
	for u, row := range rows {
		pos := make([]Entry, 0, len(row))
		for _, e := range row {
			if e.Score > 0 {
				pos = append(pos, e)
			}
		}
		if len(pos) > perUserCap {
			sort.Slice(pos, func(a, b int) bool { return pos[a].Last > pos[b].Last })
			pos = pos[:perUserCap]
		}
		items := make([]int32, len(pos))
		for k, e := range pos {
			items[k] = e.Item
			itemUsers[e.Item] = append(itemUsers[e.Item], int32(u))
		}
		userItems[u] = items
	}
	userW := make([]float32, len(rows))
	for u, items := range userItems {
		if len(items) > 0 {
			userW[u] = float32(1 / math.Log(2+float64(len(items))))
		}
	}
	out := make([]Neighbor, 0)
	_ = out
	transOut := make([][]Neighbor, nItems)
	for k, v := range trans {
		i, j := int32(k>>32), int32(uint32(k))
		transOut[i] = append(transOut[i], Neighbor{Item: j, Score: v})
		transOut[j] = append(transOut[j], Neighbor{Item: i, Score: v})
	}
	neighbors := make([][]Neighbor, nItems)
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	var wg sync.WaitGroup
	next := make(chan int, 256)
	var firstErr error
	var errOnce sync.Once
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			acc := make([]float32, nItems)
			touched := make([]int32, 0, 1024)
			for i := range next {
				if ctx.Err() != nil {
					errOnce.Do(func() { firstErr = ctx.Err() })
					continue
				}
				touched = touched[:0]
				for _, u := range itemUsers[i] {
					w := userW[u]
					for _, j := range userItems[u] {
						if int(j) == i {
							continue
						}
						if acc[j] == 0 {
							touched = append(touched, j)
						}
						acc[j] += w
					}
				}
				ni := float64(len(itemUsers[i]))
				if p.SessionBoost > 0 {
					for _, t := range transOut[i] {
						if acc[t.Item] == 0 {
							touched = append(touched, t.Item)
						}
						acc[t.Item] += float32(p.SessionBoost) * float32(math.Log1p(float64(t.Score)))
					}
				}
				cand := make([]Neighbor, 0, len(touched))
				for _, j := range touched {
					nj := float64(len(itemUsers[j]))
					den := math.Sqrt(math.Max(ni, 1) * math.Max(nj, 1))
					cand = append(cand, Neighbor{Item: j, Score: float32(float64(acc[j]) / den)})
					acc[j] = 0
				}
				neighbors[i] = topNeighbors(cand, p.Neighbors)
			}
		}()
	}
	for i := 0; i < nItems; i++ {
		next <- i
	}
	close(next)
	wg.Wait()
	return neighbors, firstErr
}

func topNeighbors(c []Neighbor, k int) []Neighbor {
	sort.Slice(c, func(a, b int) bool {
		if c[a].Score != c[b].Score {
			return c[a].Score > c[b].Score
		}
		return c[a].Item < c[b].Item
	})
	if len(c) > k {
		c = c[:k]
	}
	return slices.Clip(c)
}

// ── 热度与画像 ──

func trending(items []itemStat) (trend, rising []ScoredItem, pop []float32) {
	pop = make([]float32, len(items))
	var maxPop float64
	for i, st := range items {
		v := math.Log1p(float64(st.posUsers))
		pop[i] = float32(v)
		maxPop = math.Max(maxPop, v)
	}
	if maxPop > 0 {
		for i := range pop {
			pop[i] /= float32(maxPop)
		}
	}
	for i, st := range items {
		if st.listeners7 > 0 {
			rate := (float64(st.completes7) + 1.2) / (float64(st.plays7) + 2)
			trend = append(trend, ScoredItem{Item: int32(i), Score: float32(math.Log1p(float64(st.listeners7)) * rate)})
		}
		if st.listeners1 >= 2 {
			base := float64(st.listenersPrev)/6 + 1
			rising = append(rising, ScoredItem{Item: int32(i), Score: float32(float64(st.listeners1) / base)})
		}
	}
	sortScored(trend)
	sortScored(rising)
	if len(trend) > 500 {
		trend = trend[:500]
	}
	if len(rising) > 200 {
		rising = rising[:200]
	}
	return trend, rising, pop
}

func sortScored(s []ScoredItem) {
	sort.Slice(s, func(a, b int) bool {
		if s[a].Score != s[b].Score {
			return s[a].Score > s[b].Score
		}
		return s[a].Item < s[b].Item
	})
}

func userArtists(rows [][]Entry, items []itemStat) [][]ArtistWeight {
	out := make([][]ArtistWeight, len(rows))
	for u, row := range rows {
		acc := map[string]*ArtistWeight{}
		for _, e := range row {
			if e.Score <= 0 {
				continue
			}
			st := items[e.Item]
			if st.artist == "" {
				continue
			}
			a := acc[st.artist]
			if a == nil {
				a = &ArtistWeight{Artist: st.artist, Name: st.artistN}
				acc[st.artist] = a
			}
			a.Weight += e.Score
		}
		list := make([]ArtistWeight, 0, len(acc))
		for _, a := range acc {
			list = append(list, *a)
		}
		sort.Slice(list, func(a, b int) bool {
			if list[a].Weight != list[b].Weight {
				return list[a].Weight > list[b].Weight
			}
			return list[a].Artist < list[b].Artist
		})
		if len(list) > 20 {
			list = list[:20]
		}
		if len(list) > 0 && list[0].Weight > 0 {
			top := list[0].Weight
			for k := range list {
				list[k].Weight /= top
			}
		}
		out[u] = list
	}
	return out
}

func artistTop(items []itemStat) map[string][]int32 {
	by := map[string][]int32{}
	for i, st := range items {
		if st.artist != "" {
			by[st.artist] = append(by[st.artist], int32(i))
		}
	}
	for a, list := range by {
		sort.Slice(list, func(x, y int) bool {
			if items[list[x]].posUsers != items[list[y]].posUsers {
				return items[list[x]].posUsers > items[list[y]].posUsers
			}
			return list[x] < list[y]
		})
		if len(list) > 30 {
			list = list[:30]
		}
		by[a] = list
	}
	return by
}

// Build 用矩阵训练出完整快照（不含评估）。
func Build(ctx context.Context, m *Matrix, cfg appcfg.Config, workers int, seed uint64, stage func(name string, start time.Time)) (*Snapshot, error) {
	t := time.Now()
	als, err := TrainALS(ctx, m.Rows, len(m.Items), cfg.ALS, workers, seed)
	if err != nil {
		return nil, err
	}
	stage("als", t)
	t = time.Now()
	nb, err := TrainItemCF(ctx, m.Rows, len(m.Items), m.Trans, cfg.ItemCF, workers)
	if err != nil {
		return nil, err
	}
	stage("itemcf", t)
	t = time.Now()
	trend, rising, pop := trending(m.Items)
	s := &Snapshot{
		Format: FormatVersion, Factors: cfg.ALS.Factors,
		ItemFactors: als.Y, UserFactors: als.X, Users: m.Users, Neighbors: nb,
		Trending: trend, Rising: rising, ItemPop: pop,
		UserArtists: userArtists(m.Rows, m.Items), ArtistTop: artistTop(m.Items),
	}
	s.Gram = gramian(als.Y, cfg.ALS.Factors)
	s.Lambda, s.Alpha = float32(cfg.ALS.Regularization), float32(cfg.ALS.Alpha)
	s.ArtistName = map[string]string{}
	for _, st := range m.Items {
		s.Items = append(s.Items, st.key)
		s.ItemArtist = append(s.ItemArtist, st.artist)
		s.ItemArtistN = append(s.ItemArtistN, st.artistN)
		s.ItemTitle = append(s.ItemTitle, st.title)
		s.ItemLang = append(s.ItemLang, st.lang)
		// 贝叶斯平滑：先验完播 0.6、早跳 0.2，各相当于 5 次播放
		s.ItemComplete = append(s.ItemComplete, float32((float64(st.completes30)+3)/(float64(st.plays30)+5)))
		s.ItemSkip = append(s.ItemSkip, float32((float64(st.skips30)+1)/(float64(st.plays30)+5)))
		if st.artist != "" && s.ArtistName[st.artist] == "" {
			s.ArtistName[st.artist] = st.artistN
		}
	}
	s.ArtistSim = artistSimilarity(s.UserArtists, 20)
	for _, row := range m.Rows {
		s.UserCounts = append(s.UserCounts, int32(len(row)))
	}
	s.Index()
	stage("trending", t)
	return s, nil
}
