package rec

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"hash/fnv"
	"math"
	mrand "math/rand/v2"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"aegis/services/music/contract"
	"aegis/services/music/internal/appcfg"
	"aegis/services/music/internal/model"
	"aegis/services/music/internal/realtime"
	"aegis/services/music/internal/store"
)

// 通道名（RecItem.channels 的键、控制台分数拆解）。
const (
	chALS      = "als"
	chItemCF   = "itemcf"
	chArtist   = "artist"
	chTrending = "trending"
	chExplore  = "explore"
	chPenalty  = "penalty"
)

var channelOrder = []string{chALS, chItemCF, chArtist, chTrending, chExplore}

// ErrNotEnabled 应用关闭了音乐个性化。
var ErrNotEnabled = errors.New("music personalization disabled")

// ErrBadRequest 参数不对。
type ErrBadRequest struct{ Msg string }

func (e ErrBadRequest) Error() string { return e.Msg }

// Service 推荐、画像、遗忘。
type Service struct {
	Reg  *Registry
	Cfgs *Configs
	PG   *store.PG
	CH   *store.CH
	RT   *realtime.Store
	JS   jetstream.JetStream
	Log  *zap.Logger
	Loc  *time.Location

	popMu sync.Mutex
	pop   map[int64]popCache
}

type popCache struct {
	items  []store.Scored
	tracks map[string]contract.Track
	at     time.Time
}

// Request 推荐请求。
type Request struct {
	AppID, UserID          int64
	Scene                  string
	Limit                  int
	Cursor                 string
	SeedSource, SeedSongID string
	Personalized, Explain  bool
}

// Reason 推荐原因。
type Reason struct {
	Kind     string `json:"kind"`
	Text     string `json:"text"`
	RefTitle string `json:"refTitle,omitempty"`
}

// Item 一首推荐。
type Item struct {
	WorkKey  string             `json:"workKey"`
	Track    contract.Track     `json:"track"`
	Score    float64            `json:"score"`
	Reason   Reason             `json:"reason"`
	Channels map[string]float64 `json:"channels,omitempty"`
}

// Result 推荐结果。
type Result struct {
	RecID        string `json:"recId"`
	Scene        string `json:"scene"`
	ModelVersion int64  `json:"modelVersion"`
	Personalized bool   `json:"personalized"`
	ColdStart    bool   `json:"coldStart"`
	NextCursor   string `json:"nextCursor,omitempty"`
	GeneratedAt  int64  `json:"generatedAt"`
	ExpiresAt    int64  `json:"expiresAt"`
	Items        []Item `json:"items"`
	TookMs       int64  `json:"tookMs"`
}

type cand struct {
	ch     [5]float64
	seed   int32 // ItemCF 贡献最大的种子
	seedW  float64
	artist string // 歌手通道贡献最大的歌手展示名
	artW   float64
}

const dailySize = 30

// Recommend 推荐。
func (s *Service) Recommend(ctx context.Context, req Request) (*Result, error) {
	begin := time.Now()
	if !contract.ValidScene(req.Scene) {
		return nil, ErrBadRequest{"未知的推荐场景：" + req.Scene}
	}
	cfg, err := s.Cfgs.Get(ctx, req.AppID)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, ErrNotEnabled
	}
	limit := req.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}
	if req.Scene == contract.SceneDaily {
		limit = dailySize
	}
	if req.Scene == contract.SceneSimilar && (req.SeedSource == "" || req.SeedSongID == "") {
		return nil, ErrBadRequest{"相似歌曲需要指定 source 与 songId"}
	}
	page, _ := strconv.Atoi(req.Cursor)
	if page < 0 || page > 20 {
		page = 0
	}
	now := time.Now()
	day := now.In(s.Loc).Format("20060102")
	personal := req.Personalized && req.UserID > 0

	// 每日推荐当天固定：命中缓存直接返回（预览不读缓存）
	if req.Scene == contract.SceneDaily && personal && !req.Explain {
		if cached, ok := s.cachedDaily(ctx, req, day); ok {
			cached.TookMs = time.Since(begin).Milliseconds()
			return cached, nil
		}
	}

	var state *realtime.State
	if personal {
		if state, err = s.RT.Load(ctx, req.AppID, req.UserID); err != nil {
			s.Log.Warn("load realtime state failed", zap.Error(err))
			state = nil
		}
	}
	if state == nil {
		state = &realtime.State{Negative: map[string]int64{}, DislikedArtists: map[string]int64{}}
	}

	res := &Result{RecID: newRecID(now), Scene: req.Scene, Personalized: personal, GeneratedAt: now.UnixMilli(), Items: []Item{}}
	switch req.Scene {
	case contract.SceneDaily:
		end := time.Date(now.In(s.Loc).Year(), now.In(s.Loc).Month(), now.In(s.Loc).Day()+1, 0, 0, 0, 0, s.Loc)
		res.ExpiresAt = end.UnixMilli()
	case contract.SceneRadio:
		res.ExpiresAt = now.Add(30 * time.Minute).UnixMilli()
	case contract.SceneSimilar:
		res.ExpiresAt = now.Add(6 * time.Hour).UnixMilli()
	default:
		res.ExpiresAt = now.Add(time.Hour).UnixMilli()
	}

	loaded := s.Reg.Get(req.AppID)
	if loaded == nil {
		res.ColdStart = true
		if req.Scene != contract.SceneSimilar {
			items, err := s.popularFallback(ctx, req, state, limit, page)
			if err != nil {
				return nil, err
			}
			res.Items = items
		}
	} else {
		res.ModelVersion = loaded.RunID
		items, cold, err := s.rank(ctx, loaded, cfg, req, state, limit, page, now, day)
		if err != nil {
			return nil, err
		}
		res.Items, res.ColdStart = items, cold
	}
	if req.Scene == contract.SceneRadio || req.Scene == contract.SceneHome {
		res.NextCursor = strconv.Itoa(page + 1)
	}
	if !req.Explain {
		for i := range res.Items {
			res.Items[i].Channels = nil
		}
		if req.Scene == contract.SceneDaily && personal && len(res.Items) > 0 {
			s.storeDaily(ctx, req, day, res)
		}
		if req.Scene == contract.SceneRadio && personal && len(res.Items) > 0 {
			s.markRadio(ctx, req, day, res.Items)
		}
		s.publishServed(req, res)
	}
	res.TookMs = time.Since(begin).Milliseconds()
	return res, nil
}

func (s *Service) rank(ctx context.Context, l *Loaded, cfg appcfg.Config, req Request, state *realtime.State, limit, page int, now time.Time, day string) ([]Item, bool, error) {
	snap := l.Snap
	cands := map[int32]*cand{}
	get := func(i int32) *cand {
		c := cands[i]
		if c == nil {
			c = &cand{seed: -1}
			cands[i] = c
		}
		return c
	}
	u, inModel := int32(-1), false
	if req.Personalized && req.UserID > 0 && !l.Forgotten(req.UserID) {
		u, inModel = snap.User(req.UserID)
	}
	seedItem := int32(-1)
	if req.Scene == contract.SceneSimilar {
		key, err := s.PG.WorkKeyOf(ctx, req.AppID, req.SeedSource, req.SeedSongID)
		if err != nil {
			return nil, false, err
		}
		if i, ok := snap.Item(key); ok {
			seedItem = i
		}
		if seedItem < 0 {
			return []Item{}, true, nil
		}
	}

	// ── 召回 ──
	personalSeeds := 0
	switch {
	case req.Scene == contract.SceneSimilar:
		for _, n := range snap.Neighbors[seedItem] {
			c := get(n.Item)
			c.ch[1] += float64(n.Score)
			c.seed, c.seedW = seedItem, float64(n.Score)
		}
		// ALS 余弦近邻
		sv, sn := snap.ItemVec(seedItem), snap.ItemNorm(seedItem)
		if sn > 0 {
			scores := make([]float32, len(snap.Items))
			for i := range snap.Items {
				if n := snap.ItemNorm(int32(i)); n > 0 && int32(i) != seedItem {
					scores[i] = model.Dot(sv, snap.ItemVec(int32(i))) / (sn * n)
				}
			}
			for _, p := range topScores(scores, 200) {
				get(p.Item).ch[0] = float64(p.Score)
			}
		}
		if a := snap.ItemArtist[seedItem]; a != "" {
			for _, i := range snap.ArtistTop[a] {
				if i != seedItem {
					c := get(i)
					c.ch[2] = float64(snap.ItemPop[i])
					c.artist, c.artW = snap.ItemArtistN[seedItem], 1
				}
			}
		}
	default:
		if inModel {
			uv := snap.UserVec(u)
			scores := make([]float32, len(snap.Items))
			for i := range snap.Items {
				scores[i] = model.Dot(uv, snap.ItemVec(int32(i)))
			}
			for _, p := range topScores(scores, 300) {
				get(p.Item).ch[0] = float64(p.Score)
			}
		}
		// ItemCF：实时正反馈（新在前）+ 最近播放
		type seed struct {
			item int32
			w    float64
		}
		var seeds []seed
		seen := map[int32]bool{}
		for rank, e := range state.Positive {
			if len(seeds) >= 20 {
				break
			}
			if i, ok := snap.Item(e.WorkKey); ok && !seen[i] {
				seen[i] = true
				seeds = append(seeds, seed{i, 1 / (1 + 0.15*float64(rank))})
			}
		}
		for rank, e := range state.Recent {
			if rank >= 10 {
				break
			}
			if i, ok := snap.Item(e.WorkKey); ok && !seen[i] {
				if _, bad := state.Negative[e.WorkKey]; bad {
					continue
				}
				seen[i] = true
				seeds = append(seeds, seed{i, 0.5 / (1 + 0.15*float64(rank))})
			}
		}
		personalSeeds = len(seeds)
		for _, sd := range seeds {
			for _, n := range snap.Neighbors[sd.item] {
				c := get(n.Item)
				v := float64(n.Score) * sd.w
				c.ch[1] += v
				if v > c.seedW {
					c.seed, c.seedW = sd.item, v
				}
			}
		}
		// 歌手偏好：模型里的口味 + 实时种子的歌手
		artists := map[string]float64{}
		names := map[string]string{}
		if inModel {
			for k, a := range snap.UserArtists[u] {
				if k >= 8 {
					break
				}
				artists[a.Artist] += float64(a.Weight)
				names[a.Artist] = a.Name
			}
		}
		for k, sd := range seeds {
			if k >= 8 {
				break
			}
			if a := snap.ItemArtist[sd.item]; a != "" {
				artists[a] += 0.6 * sd.w
				names[a] = snap.ItemArtistN[sd.item]
			}
		}
		for a, w := range artists {
			for k, i := range snap.ArtistTop[a] {
				if k >= 12 {
					break
				}
				c := get(i)
				v := w * float64(snap.ItemPop[i])
				c.ch[2] += v
				if v > c.artW {
					c.artist, c.artW = names[a], v
				}
			}
		}
		// 热度
		var maxT float32
		for k, t := range snap.Trending {
			if k >= 200 {
				break
			}
			maxT = max(maxT, t.Score)
		}
		for k, t := range snap.Trending {
			if k >= 200 || maxT <= 0 {
				break
			}
			get(t.Item).ch[3] = float64(t.Score / maxT)
		}
		// 探索：热度长尾与飙升里随机取几首
		rng := mrand.New(mrand.NewPCG(seedFor(req, now, day), uint64(page)+1))
		pool := make([]int32, 0, 400)
		for k, t := range snap.Trending {
			if k >= 50 {
				pool = append(pool, t.Item)
			}
		}
		for _, t := range snap.Rising {
			pool = append(pool, t.Item)
		}
		nExplore := int(math.Ceil(float64(limit) * cfg.ExploreRatio))
		for k := 0; k < nExplore && len(pool) > 0; k++ {
			j := rng.IntN(len(pool))
			get(pool[j]).ch[4] = 0.6 + 0.4*rng.Float64()
			pool[j] = pool[len(pool)-1]
			pool = pool[:len(pool)-1]
		}
	}
	cold := !inModel && personalSeeds == 0
	if !req.Personalized && req.Scene != contract.SceneSimilar {
		for _, c := range cands {
			c.ch[0], c.ch[1], c.ch[2] = 0, 0, 0
		}
	}

	// ── 归一、融合、过滤 ──
	var maxCh [5]float64
	for _, c := range cands {
		for k, v := range c.ch {
			maxCh[k] = math.Max(maxCh[k], v)
		}
	}
	blend := [5]float64{cfg.Blend.ALS, cfg.Blend.ItemCF, cfg.Blend.Artist, cfg.Blend.Trending, cfg.Blend.Explore}
	if req.Scene == contract.SceneSimilar {
		blend = [5]float64{0.8, 1.2, 0.4, 0, 0}
	}
	disliked := map[string]bool{}
	for a := range state.DislikedArtists {
		disliked[contract.NormalizeArtist(a)] = true
	}
	recentCut := now.Add(-time.Duration(cfg.RecentExcludeDays) * 24 * time.Hour).UnixMilli()
	recent := map[string]int64{}
	for _, e := range state.Recent {
		recent[e.WorkKey] = e.At
	}
	var radioServed map[string]bool
	if req.Scene == contract.SceneRadio && req.Personalized && req.UserID > 0 {
		radioServed = s.radioServed(ctx, req, day)
	}
	negCut := now.Add(-30 * 24 * time.Hour).UnixMilli()

	type scored struct {
		i     int32
		score float64
		norm  [5]float64
		pen   float64
	}
	var list []scored
	for i, c := range cands {
		if i == seedItem || l.Tracks[i].Source == "" {
			continue
		}
		key := snap.Items[i]
		if at, bad := state.Negative[key]; bad && at >= negCut {
			continue
		}
		if disliked[snap.ItemArtist[i]] || radioServed[key] {
			continue
		}
		var norm [5]float64
		total := 0.0
		for k := range c.ch {
			if maxCh[k] > 0 {
				norm[k] = c.ch[k] / maxCh[k]
			}
			total += blend[k] * norm[k]
		}
		if total <= 0 {
			continue
		}
		pen := 1.0
		if at, ok := state.Negative[key]; ok && at < negCut {
			pen *= 0.5
		}
		if at, ok := recent[key]; ok && at >= recentCut {
			switch req.Scene {
			case contract.SceneRadio:
				pen *= 0.6
			case contract.SceneSimilar:
				pen *= 0.8
			default:
				pen *= 0.25
			}
		}
		list = append(list, scored{i: i, score: total * pen, norm: norm, pen: pen})
	}
	sort.Slice(list, func(a, b int) bool {
		if list[a].score != list[b].score {
			return list[a].score > list[b].score
		}
		return list[a].i < list[b].i
	})

	// ── 多样性重排：前 20 首同一歌手最多 artistCap 首，尽量不连着同一歌手 ──
	need := limit * (page + 1)
	if req.Scene == contract.SceneRadio || req.Scene == contract.SceneDaily {
		need = limit
	}
	picked := diversify(len(list), func(k int) string { return snap.ItemArtist[list[k].i] }, need, cfg.ArtistCap)
	start := 0
	if req.Scene == contract.SceneHome || req.Scene == contract.SceneSimilar {
		start = min(limit*page, len(picked))
	}
	out := make([]Item, 0, limit)
	for _, k := range picked[start:] {
		if len(out) >= limit {
			break
		}
		sc := list[k]
		c := cands[sc.i]
		item := Item{WorkKey: snap.Items[sc.i], Track: l.Tracks[sc.i], Score: round4(sc.score),
			Reason: s.reason(snap, req.Scene, c, sc.norm, blend, seedItem)}
		if req.Explain {
			item.Channels = map[string]float64{chPenalty: round4(sc.pen)}
			for k, name := range channelOrder {
				item.Channels[name] = round4(sc.norm[k] * blend[k])
			}
		}
		out = append(out, item)
	}
	return out, cold, nil
}

// diversify 贪心重排：返回 list 下标。前 20 个位置里同一歌手不超过 cap，且尽量不与上一首同歌手；不够时放宽。
func diversify(n int, artist func(int) string, need, capN int) []int {
	used := make([]bool, n)
	count := map[string]int{}
	out := make([]int, 0, need)
	last := ""
	for len(out) < need {
		pick := -1
		fallback := -1
		for k := 0; k < n; k++ {
			if used[k] {
				continue
			}
			a := artist(k)
			if a != "" && len(out) < 20 && count[a] >= capN {
				continue
			}
			if a != "" && a == last {
				if fallback < 0 {
					fallback = k
				}
				continue
			}
			pick = k
			break
		}
		if pick < 0 {
			pick = fallback
		}
		if pick < 0 {
			// 上限挡住了全部剩余：放宽，按原顺序补齐
			for k := 0; k < n; k++ {
				if !used[k] {
					pick = k
					break
				}
			}
		}
		if pick < 0 {
			break
		}
		used[pick] = true
		a := artist(pick)
		count[a]++
		last = a
		out = append(out, pick)
	}
	return out
}

func (s *Service) reason(snap *model.Snapshot, scene string, c *cand, norm, blend [5]float64, seedItem int32) Reason {
	best, bestV := -1, 0.0
	for k := range norm {
		if v := norm[k] * blend[k]; v > bestV {
			best, bestV = k, v
		}
	}
	title := func(i int32) string {
		if i < 0 || int(i) >= len(snap.ItemTitle) {
			return ""
		}
		return snap.ItemTitle[i]
	}
	switch best {
	case 1:
		if scene == contract.SceneSimilar {
			if t := title(seedItem); t != "" {
				return Reason{Kind: "seed", Text: "与《" + t + "》相似", RefTitle: t}
			}
		}
		if t := title(c.seed); t != "" {
			return Reason{Kind: "because_track", Text: "因为你喜欢《" + t + "》", RefTitle: t}
		}
	case 2:
		if c.artist != "" {
			return Reason{Kind: "artist", Text: "你常听 " + c.artist}
		}
	case 0:
		if scene == contract.SceneSimilar {
			if t := title(seedItem); t != "" {
				return Reason{Kind: "seed", Text: "与《" + t + "》相似", RefTitle: t}
			}
		}
		return Reason{Kind: "similar_users", Text: "和你口味相近的人在听"}
	case 4:
		return Reason{Kind: "explore", Text: "换换口味"}
	}
	return Reason{Kind: "trending", Text: "近期热门"}
}

// popularFallback 还没有模型时：近 7 天听众最多的作品。
func (s *Service) popularFallback(ctx context.Context, req Request, state *realtime.State, limit, page int) ([]Item, error) {
	s.popMu.Lock()
	if s.pop == nil {
		s.pop = map[int64]popCache{}
	}
	cache, ok := s.pop[req.AppID]
	s.popMu.Unlock()
	if !ok || time.Since(cache.at) > 10*time.Minute {
		items, err := s.CH.Popular(ctx, req.AppID, 7, 300)
		if err != nil {
			return nil, err
		}
		keys := make([]string, len(items))
		for k, it := range items {
			keys[k] = it.WorkKey
		}
		tracks, err := s.PG.Tracks(ctx, req.AppID, keys)
		if err != nil {
			return nil, err
		}
		cache = popCache{items: items, tracks: tracks, at: time.Now()}
		s.popMu.Lock()
		s.pop[req.AppID] = cache
		s.popMu.Unlock()
	}
	recent := map[string]bool{}
	for _, e := range state.Recent {
		recent[e.WorkKey] = true
	}
	var top float64
	if len(cache.items) > 0 {
		top = cache.items[0].Score
	}
	out := []Item{}
	skip := limit * page
	for _, it := range cache.items {
		t, ok := cache.tracks[it.WorkKey]
		if !ok || t.Source == "" {
			continue
		}
		if _, bad := state.Negative[it.WorkKey]; bad || recent[it.WorkKey] && req.Scene != contract.SceneRadio {
			continue
		}
		if skip > 0 {
			skip--
			continue
		}
		score := 0.0
		if top > 0 {
			score = it.Score / top
		}
		item := Item{WorkKey: it.WorkKey, Track: t, Score: round4(score), Reason: Reason{Kind: "trending", Text: "近期热门"}}
		if req.Explain {
			item.Channels = map[string]float64{chTrending: round4(score), chPenalty: 1}
		}
		out = append(out, item)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// ── 每日推荐缓存与电台去重 ──

func (s *Service) cachedDaily(ctx context.Context, req Request, day string) (*Result, bool) {
	raw, err := s.RT.Client().Get(ctx, s.RT.UserKey(req.AppID, req.UserID, "daily:"+day)).Bytes()
	if err != nil {
		return nil, false
	}
	var res Result
	if json.Unmarshal(raw, &res) != nil {
		return nil, false
	}
	return &res, true
}

func (s *Service) storeDaily(ctx context.Context, req Request, day string, res *Result) {
	raw, _ := json.Marshal(res)
	ttl := time.Until(time.UnixMilli(res.ExpiresAt)) + time.Hour
	if err := s.RT.Client().Set(ctx, s.RT.UserKey(req.AppID, req.UserID, "daily:"+day), raw, ttl).Err(); err != nil {
		s.Log.Warn("cache daily failed", zap.Error(err))
	}
}

func (s *Service) radioServed(ctx context.Context, req Request, day string) map[string]bool {
	keys, err := s.RT.Client().ZRange(ctx, s.RT.UserKey(req.AppID, req.UserID, "radio:"+day), 0, -1).Result()
	out := map[string]bool{}
	if err != nil {
		return out
	}
	for _, k := range keys {
		out[k] = true
	}
	return out
}

func (s *Service) markRadio(ctx context.Context, req Request, day string, items []Item) {
	key := s.RT.UserKey(req.AppID, req.UserID, "radio:"+day)
	zs := make([]redis.Z, len(items))
	now := float64(time.Now().UnixMilli())
	for k, it := range items {
		zs[k] = redis.Z{Score: now, Member: it.WorkKey}
	}
	pipe := s.RT.Client().Pipeline()
	pipe.ZAdd(ctx, key, zs...)
	pipe.Expire(ctx, key, 48*time.Hour)
	if _, err := pipe.Exec(ctx); err != nil {
		s.Log.Warn("mark radio served failed", zap.Error(err))
	}
}

// publishServed 推荐结果发进流（落 ClickHouse impressions，算点击率）。不阻塞请求：
// 后台带重试发布，NATS 长时间不可用时只丢这条统计记录，不影响用户拿到推荐。
func (s *Service) publishServed(req Request, res *Result) {
	if s.JS == nil || req.UserID <= 0 {
		return
	}
	served := contract.Served{RecID: res.RecID, Scene: res.Scene, AppID: req.AppID, UserID: req.UserID,
		ModelVersion: res.ModelVersion, Personalized: res.Personalized, ColdStart: res.ColdStart, TS: res.GeneratedAt}
	for _, it := range res.Items {
		served.Items = append(served.Items, it.WorkKey)
	}
	payload, _ := json.Marshal(served)
	go func() {
		for attempt := 0; attempt < 3; attempt++ {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			_, err := s.JS.Publish(ctx, contract.ServedSubject(req.AppID), payload, jetstream.WithMsgID("served-"+res.RecID))
			cancel()
			if err == nil {
				return
			}
			time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
		}
		s.Log.Warn("publish served failed", zap.String("rec", res.RecID))
	}()
}

// ── 工具 ──

func topScores(scores []float32, k int) []model.ScoredItem {
	out := make([]model.ScoredItem, 0, k)
	minAt := -1
	for i, v := range scores {
		if v <= 0 {
			continue
		}
		if len(out) < k {
			out = append(out, model.ScoredItem{Item: int32(i), Score: v})
			minAt = -1
			continue
		}
		if minAt < 0 {
			minAt = 0
			for j := range out {
				if out[j].Score < out[minAt].Score {
					minAt = j
				}
			}
		}
		if v > out[minAt].Score {
			out[minAt] = model.ScoredItem{Item: int32(i), Score: v}
			minAt = -1
		}
	}
	return out
}

func seedFor(req Request, now time.Time, day string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(strconv.FormatInt(req.AppID, 10) + ":" + strconv.FormatInt(req.UserID, 10) + ":" + req.Scene + ":"))
	if req.Scene == contract.SceneDaily {
		_, _ = h.Write([]byte(day))
	} else {
		// 十分钟内同一页结果稳定（反复打开首页不跳来跳去）
		_, _ = h.Write([]byte(strconv.FormatInt(now.Unix()/600, 10)))
	}
	return h.Sum64()
}

func round4(v float64) float64 { return math.Round(v*1e4) / 1e4 }

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// newRecID ULID：48 位毫秒 + 80 位随机，Crockford Base32，按时间可排序。
func newRecID(now time.Time) string {
	var b [16]byte
	ms := uint64(now.UnixMilli())
	for i := 5; i >= 0; i-- {
		b[i] = byte(ms)
		ms >>= 8
	}
	_, _ = rand.Read(b[6:])
	var sb strings.Builder
	sb.Grow(26)
	// 128 位按 5 位一组编码（首组 3 位）
	var acc uint64
	bits := 0
	hi := uint64(0)
	_ = hi
	all := append([]byte{0}, b[:]...) // 补 0 凑 136 位 → 去掉前 6 位
	for _, by := range all {
		acc = acc<<8 | uint64(by)
		bits += 8
		for bits >= 5 {
			bits -= 5
			sb.WriteByte(crockford[(acc>>uint(bits))&31])
		}
	}
	out := sb.String()
	// 136 位编码出 27 个字符，第一个恒为补位产生的 0：去掉后正好 26 位
	return out[1:]
}
