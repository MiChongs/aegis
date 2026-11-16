package rec

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"hash/fnv"
	"math"
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
	chFoldIn   = "foldin"
	chRanker   = "ranker"
	chRule     = "rule"
	chFatigue  = "fatigue"
	chPenalty  = "penalty"
)

var channelOrder = []string{chALS, chItemCF, chArtist, chTrending, chExplore}

// ErrNotEnabled 应用关闭了音乐个性化。
var ErrNotEnabled = errors.New("music personalization disabled")

// ErrBadRequest 参数不对。
type ErrBadRequest struct{ Msg string }

func (e ErrBadRequest) Error() string { return e.Msg }

// Service 推荐、画像、遗忘、用户侧操作。
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

	opsOnce sync.Once
	opsC    *opsCache
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
	Seeds                  []contract.Track // autoplay：最近放的在后
	Exclude                []string         // autoplay：source:songId
	MixID                  string           // mix
	Artist                 string           // artist
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

	rule     int64     // 影响这首的运营规则
	features []float32 // 精排特征（记录训练样本）
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
	ExperimentID int64  `json:"experimentId"`
	Variant      string `json:"variant"`
	Ranker       bool   `json:"ranker"`
	Items        []Item `json:"items"`
	TookMs       int64  `json:"tookMs"`
}

type cand struct {
	ch     [5]float64
	fold   float64
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
		switch req.Scene {
		case contract.SceneAutoplay:
			limit = cfg.Autoplay.Size
		case contract.SceneArtist:
			limit = 30
		}
	}
	limit = min(limit, 50)
	switch req.Scene {
	case contract.SceneDaily:
		limit = dailySize
	case contract.SceneDiscover:
		limit = cfg.Discover.Size
	case contract.SceneSimilar:
		if req.SeedSource == "" || req.SeedSongID == "" {
			return nil, ErrBadRequest{"相似歌曲需要指定 source 与 songId"}
		}
	case contract.SceneMix:
		if req.MixID == "" {
			return nil, ErrBadRequest{"风格歌单需要指定 mixId"}
		}
	case contract.SceneArtist:
		req.Artist = strings.TrimSpace(req.Artist)
		if req.Artist == "" {
			return nil, ErrBadRequest{"歌手推荐需要指定 artist"}
		}
	}
	if len(req.Seeds) > 20 {
		req.Seeds = req.Seeds[len(req.Seeds)-20:]
	}
	if len(req.Exclude) > 300 {
		req.Exclude = req.Exclude[:300]
	}
	page, _ := strconv.Atoi(req.Cursor)
	if page < 0 || page > 20 {
		page = 0
	}
	now := time.Now()
	local := now.In(s.Loc)
	day := local.Format("20060102")

	prefs := store.DefaultPrefs()
	if req.Personalized && req.UserID > 0 {
		prefs = s.userPrefs(ctx, req.AppID, req.UserID)
	}
	personal := req.Personalized && req.UserID > 0 && prefs.Personalized
	asg := s.assign(ctx, req.AppID, req.UserID, req.Scene)
	if asg != nil {
		cfg = applyOverrides(cfg, asg.Overrides)
	}

	// 当天 / 当周固定的场景：命中缓存直接返回（预览不读缓存）
	period := ""
	switch req.Scene {
	case contract.SceneDaily:
		period = "daily:" + day
	case contract.SceneDiscover:
		y, w := local.ISOWeek()
		period = "discover:" + strconv.Itoa(y) + "w" + strconv.Itoa(w)
	}
	if period != "" && asg != nil {
		period += ":x" + strconv.FormatInt(asg.ExperimentID, 10) + asg.Variant
	}
	if period != "" && personal && !req.Explain {
		if cached, ok := s.cachedResult(ctx, req, period); ok {
			cached.TookMs = time.Since(begin).Milliseconds()
			return cached, nil
		}
	}
	if req.Scene == contract.SceneMix {
		return s.mixResult(ctx, req, cfg, personal, asg, begin)
	}

	state := realtime.Empty()
	if personal {
		if st, err := s.RT.Load(ctx, req.AppID, req.UserID); err != nil {
			s.Log.Warn("load realtime state failed", zap.Error(err))
		} else {
			state = st
		}
	}

	res := &Result{RecID: newRecID(now), Scene: req.Scene, Personalized: personal, GeneratedAt: now.UnixMilli(), Items: []Item{}}
	if asg != nil {
		res.ExperimentID, res.Variant = asg.ExperimentID, asg.Variant
	}
	switch req.Scene {
	case contract.SceneDaily:
		res.ExpiresAt = time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, s.Loc).UnixMilli()
	case contract.SceneDiscover:
		wd := (int(local.Weekday()) + 6) % 7 // 周一 = 0
		res.ExpiresAt = time.Date(local.Year(), local.Month(), local.Day()-wd+7, 0, 0, 0, 0, s.Loc).UnixMilli()
	case contract.SceneRadio, contract.SceneAutoplay:
		res.ExpiresAt = now.Add(30 * time.Minute).UnixMilli()
	case contract.SceneSimilar, contract.SceneArtist:
		res.ExpiresAt = now.Add(6 * time.Hour).UnixMilli()
	default:
		res.ExpiresAt = now.Add(time.Hour).UnixMilli()
	}

	loaded := s.Reg.Get(req.AppID)
	if asg != nil && asg.Overrides.ModelRunID != nil {
		if pinned := s.Reg.GetRun(ctx, req.AppID, *asg.Overrides.ModelRunID); pinned != nil {
			loaded = pinned
		}
	}
	rules := s.activeRules(ctx, req.AppID, now)
	if loaded == nil {
		res.ColdStart = true
		items, err := s.popularFallback(ctx, req, cfg, state, rules, limit, page)
		if err != nil {
			return nil, err
		}
		res.Items = items
	} else {
		res.ModelVersion = loaded.RunID
		uc := s.buildUserCtx(ctx, loaded, cfg, req, state, prefs, personal, now)
		items, cold, ranked, err := s.rank(ctx, loaded, cfg, req, uc, rules, limit, page, now, day)
		if err != nil {
			return nil, err
		}
		res.Items, res.ColdStart, res.Ranker = items, cold, ranked
	}
	if req.Scene == contract.SceneRadio || req.Scene == contract.SceneHome || req.Scene == contract.SceneAutoplay {
		res.NextCursor = strconv.Itoa(page + 1)
	}
	if !req.Explain {
		if period != "" && personal && len(res.Items) > 0 {
			s.storeResult(ctx, req, period, res)
		}
		if req.Scene == contract.SceneRadio && personal && len(res.Items) > 0 {
			s.markRadio(ctx, req, day, res.Items)
		}
		if personal && len(res.Items) > 0 && req.Scene != contract.SceneSimilar && req.Scene != contract.SceneArtist {
			keys := make([]string, len(res.Items))
			for k, it := range res.Items {
				keys[k] = it.WorkKey
			}
			if err := s.RT.AddExposure(ctx, req.AppID, req.UserID, keys); err != nil {
				s.Log.Debug("add exposure failed", zap.Error(err))
			}
		}
		s.markPins(ctx, req, day, res.Items)
		s.publishServed(req, res)
		for i := range res.Items {
			res.Items[i].Channels = nil
		}
	}
	res.TookMs = time.Since(begin).Milliseconds()
	return res, nil
}

// ── 用户上下文 ──

type seedRef struct {
	item int32
	w    float64
}

type userCtx struct {
	personal    bool
	state       *realtime.State
	prefs       store.Prefs
	u           int32
	inModel     bool
	vec         []float32 // 用来打 ALS 通道的向量（模型向量与折入混合，或只有折入）
	fold        []float32 // 只有折入（特征用）
	seeds       []seedRef // ItemCF 种子
	artists     map[string]float64
	artistNames map[string]string
	prefArtists map[string]bool
	prefLangs   map[int]bool
	langShare   [5]float64
	known       map[string]bool // 实时状态里出现过的作品
}

func (s *Service) buildUserCtx(ctx context.Context, l *Loaded, cfg appcfg.Config, req Request, state *realtime.State,
	prefs store.Prefs, personal bool, now time.Time) *userCtx {
	snap := l.Snap
	uc := &userCtx{personal: personal, state: state, prefs: prefs, u: -1, artists: map[string]float64{}, artistNames: map[string]string{},
		prefArtists: map[string]bool{}, prefLangs: map[int]bool{}, known: map[string]bool{}}
	if !personal {
		return uc
	}
	if !l.Forgotten(req.UserID) {
		uc.u, uc.inModel = snap.User(req.UserID)
	}
	for _, e := range state.Recent {
		uc.known[e.WorkKey] = true
	}
	for _, e := range state.Positive {
		uc.known[e.WorkKey] = true
	}
	for k := range state.Negative {
		uc.known[k] = true
	}
	for _, a := range prefs.Artists {
		if n := contract.NormalizeArtist(contract.FirstArtist(a)); n != "" {
			uc.prefArtists[n] = true
		}
	}
	for _, lang := range prefs.Languages {
		uc.prefLangs[contract.LangIndex(lang)] = true
	}
	// ItemCF 种子：实时正反馈（新在前）+ 最近播放
	seen := map[int32]bool{}
	for rank, e := range state.Positive {
		if len(uc.seeds) >= 20 {
			break
		}
		if i, ok := snap.Item(e.WorkKey); ok && !seen[i] {
			seen[i] = true
			uc.seeds = append(uc.seeds, seedRef{i, 1 / (1 + 0.15*float64(rank))})
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
			uc.seeds = append(uc.seeds, seedRef{i, 0.5 / (1 + 0.15*float64(rank))})
		}
	}
	// 实时折入
	if cfg.FoldIn && len(snap.Gram) > 0 {
		var items []int32
		var conf []float32
		alpha := float64(snap.Alpha)
		if alpha <= 0 {
			alpha = cfg.ALS.Alpha
		}
		fresh := 0
		for _, e := range state.Positive {
			if i, ok := snap.Item(e.WorkKey); ok {
				w := math.Max(e.Weight, 0.4)
				items = append(items, i)
				conf = append(conf, float32(1+alpha*math.Log1p(w/0.5)))
				if e.At > snap.CreatedAt {
					fresh++
				}
			}
		}
		for k, e := range state.Recent {
			if k >= 30 {
				break
			}
			if _, bad := state.Negative[e.WorkKey]; bad {
				continue
			}
			if i, ok := snap.Item(e.WorkKey); ok && !seen[i] {
				items = append(items, i)
				conf = append(conf, float32(1+alpha*math.Log1p(0.4/0.5)))
			}
		}
		uc.fold = snap.FoldIn(items, conf)
		if uc.fold != nil {
			if uc.inModel {
				r := math.Min(0.5, float64(fresh)/float64(fresh+10))
				mv := snap.UserVec(uc.u)
				uc.vec = make([]float32, len(mv))
				for k := range mv {
					uc.vec[k] = float32((1-r)*float64(mv[k]) + r*float64(uc.fold[k]))
				}
			} else {
				uc.vec = uc.fold
			}
		}
	}
	if uc.vec == nil && uc.inModel {
		uc.vec = snap.UserVec(uc.u)
	}
	// 歌手偏好：模型里的口味 + 实时种子的歌手 + 偏好设置
	if uc.inModel {
		for k, a := range snap.UserArtists[uc.u] {
			if k >= 8 {
				break
			}
			uc.artists[a.Artist] += float64(a.Weight)
			uc.artistNames[a.Artist] = a.Name
		}
	}
	for k, sd := range uc.seeds {
		if k >= 8 {
			break
		}
		if a := snap.ItemArtist[sd.item]; a != "" {
			uc.artists[a] += 0.6 * sd.w
			uc.artistNames[a] = snap.ItemArtistN[sd.item]
		}
	}
	prefW := 0.4
	if !uc.inModel {
		prefW = 0.8
	}
	for a := range uc.prefArtists {
		uc.artists[a] += prefW
		if uc.artistNames[a] == "" {
			uc.artistNames[a] = snap.ArtistName[a]
		}
	}
	top := 0.0
	for _, w := range uc.artists {
		top = math.Max(top, w)
	}
	if top > 0 {
		for a := range uc.artists {
			uc.artists[a] /= top
		}
	}
	// 语种占比（实时状态里的作品）
	var total float64
	for key := range uc.known {
		if i, ok := snap.Item(key); ok && int(i) < len(snap.ItemLang) {
			if _, bad := state.Negative[key]; bad {
				continue
			}
			uc.langShare[snap.ItemLang[i]]++
			total++
		}
	}
	if total > 0 {
		for k := range uc.langShare {
			uc.langShare[k] /= total
		}
	}
	_ = now
	return uc
}

func (uc *userCtx) langMatch(lang uint8) float64 {
	if len(uc.prefLangs) > 0 {
		if uc.prefLangs[int(lang)] {
			return 1
		}
		return 0.5 * uc.langShare[lang]
	}
	return uc.langShare[lang]
}

// ── 召回 + 打分 ──

func (s *Service) rank(ctx context.Context, l *Loaded, cfg appcfg.Config, req Request, uc *userCtx, rules []store.Rule,
	limit, page int, now time.Time, day string) ([]Item, bool, bool, error) {
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
	state := uc.state
	seedItem := int32(-1)
	exclude := map[string]bool{}  // 作品键
	excludeT := map[string]bool{} // 音源版本
	var history map[string]bool   // discover：这个人交互过的全部作品

	addALS := func(k int) {
		if uc.vec == nil {
			return
		}
		scores := make([]float32, len(snap.Items))
		for i := range snap.Items {
			scores[i] = model.Dot(uc.vec, snap.ItemVec(int32(i)))
		}
		for _, p := range topScores(scores, k) {
			get(p.Item).ch[0] = float64(p.Score)
		}
	}
	addItemCF := func(seeds []seedRef) {
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
	}
	addArtists := func(artists map[string]float64, names map[string]string, perArtist int) {
		for a, w := range artists {
			for k, i := range snap.ArtistTop[a] {
				if k >= perArtist {
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
	}
	addTrending := func() {
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
	}
	addExplore := func() {
		rng := newRand(seedFor(req, now, day), uint64(page)+1)
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

	switch req.Scene {
	case contract.SceneSimilar:
		key, err := s.PG.WorkKeyOf(ctx, req.AppID, req.SeedSource, req.SeedSongID)
		if err != nil {
			return nil, false, false, err
		}
		if i, ok := snap.Item(key); ok {
			seedItem = i
		}
		if seedItem < 0 {
			return []Item{}, true, false, nil
		}
		for _, n := range snap.Neighbors[seedItem] {
			c := get(n.Item)
			c.ch[1] += float64(n.Score)
			c.seed, c.seedW = seedItem, float64(n.Score)
		}
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
	case contract.SceneAutoplay:
		// 种子 = 正在放的队列（最近的在后），权重按新旧衰减
		var seeds []seedRef
		artists, names := map[string]float64{}, map[string]string{}
		for k := len(req.Seeds) - 1; k >= 0; k-- {
			t := req.Seeds[k]
			key := contract.WorkKey(t.Title, t.Artist)
			if key == "" && t.Source != "" && t.SongID != "" {
				key, _ = s.PG.WorkKeyOf(ctx, req.AppID, t.Source, t.SongID)
			}
			if key == "" {
				continue
			}
			exclude[key] = true
			if i, ok := snap.Item(key); ok {
				w := math.Pow(0.85, float64(len(seeds)))
				seeds = append(seeds, seedRef{i, w})
				if a := snap.ItemArtist[i]; a != "" {
					artists[a] += 0.5 * w
					names[a] = snap.ItemArtistN[i]
				}
			}
		}
		if len(req.Exclude) > 0 {
			for _, tk := range req.Exclude {
				excludeT[tk] = true
			}
			if keys, err := s.PG.WorkKeysOf(ctx, req.AppID, req.Exclude); err == nil {
				for _, k := range keys {
					exclude[k] = true
				}
			}
		}
		cut := now.Add(-2 * time.Hour).UnixMilli()
		for _, e := range state.Recent {
			if e.At >= cut {
				exclude[e.WorkKey] = true
			}
		}
		if len(seeds) == 0 {
			seeds = uc.seeds
		}
		addItemCF(seeds)
		addALS(300)
		for a, w := range uc.artists {
			artists[a] += 0.5 * w
			if names[a] == "" {
				names[a] = uc.artistNames[a]
			}
		}
		addArtists(artists, names, 10)
		if len(cands) < limit*3 {
			addTrending()
		}
	case contract.SceneDiscover:
		addALS(800)
		addItemCF(uc.seeds)
		// 相似歌手的代表作（偏离当前口味一步）
		sim := map[string]float64{}
		names := map[string]string{}
		for a, w := range uc.artists {
			for _, n := range snap.ArtistSim[a] {
				if _, mine := uc.artists[n.Artist]; mine {
					continue
				}
				sim[n.Artist] += w * float64(n.Score)
				names[n.Artist] = snap.ArtistName[n.Artist]
			}
		}
		addArtists(sim, names, 6)
		if uc.personal {
			keys, err := s.CH.UserWorkSet(ctx, req.AppID, req.UserID, 365)
			if err != nil {
				s.Log.Warn("load user history failed", zap.Error(err))
			}
			history = make(map[string]bool, len(keys)+len(uc.known))
			for _, k := range keys {
				history[k] = true
			}
			for k := range uc.known {
				history[k] = true
			}
		}
		if len(cands) < limit*3 {
			addTrending()
			addExplore()
		}
	case contract.SceneArtist:
		target := contract.NormalizeArtist(contract.FirstArtist(req.Artist))
		for _, i := range snap.ArtistTop[target] {
			c := get(i)
			c.ch[3] = float64(snap.ItemPop[i])
			c.artist, c.artW = snap.ArtistName[target], 1
		}
		for i := range cands {
			if uc.vec != nil {
				cands[i].ch[0] = math.Max(0, float64(model.Dot(uc.vec, snap.ItemVec(i))))
			}
		}
		for _, sd := range uc.seeds {
			for _, n := range snap.Neighbors[sd.item] {
				if c := cands[n.Item]; c != nil {
					c.ch[1] += float64(n.Score) * sd.w
				}
			}
		}
	default: // home / daily / radio
		addALS(300)
		addItemCF(uc.seeds)
		addArtists(uc.artists, uc.artistNames, 12)
		addTrending()
		addExplore()
	}
	cold := !uc.inModel && uc.fold == nil && len(uc.seeds) == 0 && len(uc.prefArtists) == 0
	if !uc.personal && req.Scene != contract.SceneSimilar && req.Scene != contract.SceneAutoplay {
		for _, c := range cands {
			c.ch[0], c.ch[1], c.ch[2] = 0, 0, 0
		}
	}
	if uc.fold != nil {
		for i, c := range cands {
			c.fold = math.Max(0, float64(model.Dot(uc.fold, snap.ItemVec(i))))
		}
	}

	// ── 归一、特征、打分、过滤 ──
	var maxCh [5]float64
	var maxFold float64
	for _, c := range cands {
		for k, v := range c.ch {
			maxCh[k] = math.Max(maxCh[k], v)
		}
		maxFold = math.Max(maxFold, c.fold)
	}
	blend := [5]float64{cfg.Blend.ALS, cfg.Blend.ItemCF, cfg.Blend.Artist, cfg.Blend.Trending, cfg.Blend.Explore}
	switch req.Scene {
	case contract.SceneSimilar:
		blend = [5]float64{0.8, 1.2, 0.4, 0, 0}
	case contract.SceneAutoplay:
		blend = [5]float64{0.8, 1.2, 0.6, 0.2, 0}
	case contract.SceneArtist:
		blend = [5]float64{1, 0.6, 0, 0.5, 0}
	}
	ranker := snap.Ranker
	useRanker := cfg.Ranker.Enabled && ranker != nil && ranker.Enabled && uc.personal && len(ranker.Weights) == model.NumRankFeatures
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
	if req.Scene == contract.SceneRadio && uc.personal {
		radioServed = s.radioServed(ctx, req, day)
	}
	negCut := now.Add(-30 * 24 * time.Hour).UnixMilli()
	nowMs := now.UnixMilli()
	blockW, blockA, boosts := ruleIndex(rules, req.Scene)

	type scored struct {
		i       int32
		score   float64
		norm    [5]float64
		pen     float64
		ranker  float64
		fatigue float64
		boost   float64
		rule    int64
		x       []float32
	}
	var list []scored
	for i, c := range cands {
		if i == seedItem || l.Tracks[i].Source == "" {
			continue
		}
		key := snap.Items[i]
		art := snap.ItemArtist[i]
		if at, bad := state.Negative[key]; bad && at >= negCut {
			continue
		}
		if disliked[art] || radioServed[key] || exclude[key] || excludeT[l.Tracks[i].Key()] || blockW[key] || blockA[art] {
			continue
		}
		if until, ok := state.Suppressed[key]; ok && until > nowMs {
			continue
		}
		if _, bad := state.BadVersions[l.Tracks[i].Key()]; bad {
			continue
		}
		if history != nil && history[key] {
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
		x := make([]float32, model.NumRankFeatures)
		for k := 0; k < 5; k++ {
			x[k] = float32(norm[k])
		}
		if maxFold > 0 {
			x[5] = float32(c.fold / maxFold)
		}
		x[6] = snap.ItemPop[i]
		if int(i) < len(snap.ItemComplete) {
			x[7], x[8] = snap.ItemComplete[i], snap.ItemSkip[i]
		}
		x[9] = float32(uc.artists[art])
		if at, ok := recent[key]; ok && at >= recentCut {
			x[10] = 1
		}
		fat := state.Fatigue[key]
		x[11] = float32(math.Log1p(float64(fat)))
		if int(i) < len(snap.ItemLang) {
			x[12] = float32(uc.langMatch(snap.ItemLang[i]))
		}
		if !uc.known[key] && (history == nil || !history[key]) {
			x[13] = 1
		}
		if uc.prefArtists[art] {
			x[14] = 1
		}
		score := total
		rs := 0.0
		if useRanker {
			// 精排调整融合分而不是替换它：融合分保证召回通道的排序基本盘，精排在 0.5–1.5 倍之间调整。
			// 只用精排概率时，从没被展示过的候选（训练分布之外）会被打出不可靠的分，把个性化结果挤下去。
			rs = ranker.Score(x)
			score = total * (0.5 + rs)
		} else if len(uc.prefLangs) > 0 && int(i) < len(snap.ItemLang) && uc.prefLangs[int(snap.ItemLang[i])] {
			score *= 1.15 // 没有精排时，偏好语种直接加成
		}
		pen := 1.0
		if at, ok := state.Negative[key]; ok && at < negCut {
			pen *= 0.5
		}
		if x[10] == 1 {
			switch req.Scene {
			case contract.SceneRadio:
				pen *= 0.6
			case contract.SceneSimilar, contract.SceneArtist:
				pen *= 0.8
			default:
				pen *= 0.25
			}
		}
		fatPen := 1.0
		if th := cfg.Fatigue.Threshold; th > 0 && fat >= th {
			fatPen = math.Pow(cfg.Fatigue.Decay, float64(fat-th+1))
		}
		if req.Scene == contract.SceneDiscover {
			pen *= math.Pow(math.Max(1-float64(snap.ItemPop[i]), 0.05), cfg.Discover.PopularityPenalty)
		}
		boost, rule := 1.0, int64(0)
		if b, ok := boosts.work[key]; ok {
			boost, rule = b.w, b.id
		} else if b, ok := boosts.artist[art]; ok {
			boost, rule = b.w, b.id
		}
		list = append(list, scored{i: i, score: score * pen * fatPen * boost, norm: norm, pen: pen, ranker: rs,
			fatigue: fatPen, boost: boost, rule: rule, x: x})
	}
	sort.Slice(list, func(a, b int) bool {
		if list[a].score != list[b].score {
			return list[a].score > list[b].score
		}
		return list[a].i < list[b].i
	})

	// ── 多样性重排：前 20 首同一歌手最多 artistCap 首，尽量不连着同一歌手 ──
	need := limit * (page + 1)
	switch req.Scene {
	case contract.SceneRadio, contract.SceneDaily, contract.SceneDiscover:
		need = limit
	}
	capN := cfg.ArtistCap
	if req.Scene == contract.SceneArtist {
		capN = need
	}
	picked := diversify(len(list), func(k int) string {
		if req.Scene == contract.SceneArtist {
			return ""
		}
		return snap.ItemArtist[list[k].i]
	}, need, capN)
	start := 0
	switch req.Scene {
	case contract.SceneHome, contract.SceneSimilar, contract.SceneAutoplay, contract.SceneArtist:
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
			Reason: s.reason(snap, req.Scene, c, sc.norm, blend, seedItem, uc), rule: sc.rule, features: sc.x}
		if req.Explain {
			item.Channels = map[string]float64{chPenalty: round4(sc.pen), chFatigue: round4(sc.fatigue), chRule: round4(sc.boost)}
			for k, name := range channelOrder {
				item.Channels[name] = round4(sc.norm[k] * blend[k])
			}
			item.Channels[chFoldIn] = round4(float64(sc.x[5]))
			if useRanker {
				item.Channels[chRanker] = round4(sc.ranker)
			}
		}
		out = append(out, item)
	}
	if page == 0 {
		out = s.applyPins(ctx, l, req, rules, out, state, disliked, limit, day)
	}
	return out, cold, useRanker, nil
}

// ── 运营规则 ──

type boostRef struct {
	w  float64
	id int64
}

type boostIndex struct {
	work   map[string]boostRef
	artist map[string]boostRef
}

// ruleIndex 把生效规则按作品 / 歌手建索引：屏蔽全场景生效，加权只对规则里的场景（空 = 全部）。
func ruleIndex(rules []store.Rule, scene string) (blockW, blockA map[string]bool, boosts boostIndex) {
	blockW, blockA = map[string]bool{}, map[string]bool{}
	boosts = boostIndex{work: map[string]boostRef{}, artist: map[string]boostRef{}}
	for _, r := range rules {
		artist := contract.NormalizeArtist(contract.FirstArtist(r.Target.Artist))
		switch r.Kind {
		case "block":
			if r.Target.Type == "work" && r.Target.WorkKey != "" {
				blockW[r.Target.WorkKey] = true
			} else if r.Target.Type == "artist" && artist != "" {
				blockA[artist] = true
			}
		case "boost":
			if len(r.Scenes) > 0 && !contains(r.Scenes, scene) {
				continue
			}
			ref := boostRef{w: r.Weight, id: r.ID}
			if r.Target.Type == "work" && r.Target.WorkKey != "" {
				boosts.work[r.Target.WorkKey] = ref
			} else if artist != "" {
				boosts.artist[artist] = ref
			}
		}
	}
	return blockW, blockA, boosts
}

// pinScenes 置顶规则不写场景时默认作用的场景（列表类）。
var pinScenes = []string{contract.SceneHome, contract.SceneDaily, contract.SceneDiscover, contract.SceneRadio}

// applyPins 把置顶规则的作品插到指定位置（每人每天每条规则最多一次；这个人不感兴趣的不插）。
func (s *Service) applyPins(ctx context.Context, l *Loaded, req Request, rules []store.Rule, out []Item, state *realtime.State,
	disliked map[string]bool, limit int, day string) []Item {
	var pins []store.Rule
	for _, r := range rules {
		if r.Kind != "pin" {
			continue
		}
		scenes := r.Scenes
		if len(scenes) == 0 {
			scenes = pinScenes
		}
		if contains(scenes, req.Scene) {
			pins = append(pins, r)
		}
	}
	if len(pins) == 0 {
		return out
	}
	shown := map[int64]bool{}
	if req.UserID > 0 && !req.Explain {
		ids, _ := s.RT.Client().SMembers(ctx, s.RT.UserKey(req.AppID, req.UserID, "pin:"+day)).Result()
		for _, id := range ids {
			if v, err := strconv.ParseInt(id, 10, 64); err == nil {
				shown[v] = true
			}
		}
	}
	in := map[string]bool{}
	for _, it := range out {
		in[it.WorkKey] = true
	}
	snap := l.Snap
	sort.Slice(pins, func(a, b int) bool { return pins[a].Position < pins[b].Position })
	for _, r := range pins {
		if shown[r.ID] {
			continue
		}
		key := r.Target.WorkKey
		if r.Target.Type == "artist" {
			a := contract.NormalizeArtist(contract.FirstArtist(r.Target.Artist))
			key = ""
			for _, i := range snap.ArtistTop[a] {
				if k := snap.Items[i]; !in[k] && l.Tracks[i].Source != "" {
					if _, bad := state.Negative[k]; !bad {
						key = k
						break
					}
				}
			}
		}
		if key == "" {
			continue
		}
		if _, bad := state.Negative[key]; bad {
			continue
		}
		pos := min(max(r.Position, 1)-1, len(out))
		if in[key] {
			// 已经自然出现在结果里：挪到置顶位置
			for k := range out {
				if out[k].WorkKey == key {
					item := out[k]
					item.Reason, item.rule = Reason{Kind: "featured", Text: "精选推荐"}, r.ID
					out = append(out[:k], out[k+1:]...)
					pos = min(pos, len(out))
					out = append(out[:pos], append([]Item{item}, out[pos:]...)...)
					break
				}
			}
			continue
		}
		var track contract.Track
		if i, ok := snap.Item(key); ok {
			track = l.Tracks[i]
			if disliked[snap.ItemArtist[i]] {
				continue
			}
		} else if tracks, err := s.PG.Tracks(ctx, req.AppID, []string{key}); err == nil {
			track = tracks[key]
		}
		if track.Source == "" {
			continue
		}
		item := Item{WorkKey: key, Track: track, Score: 1, Reason: Reason{Kind: "featured", Text: "精选推荐"}, rule: r.ID}
		if req.Explain {
			item.Channels = map[string]float64{chRule: 1}
		}
		out = append(out[:pos], append([]Item{item}, out[pos:]...)...)
		in[key] = true
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// markPins 记下今天已经给这个人插过的置顶规则。
func (s *Service) markPins(ctx context.Context, req Request, day string, items []Item) {
	if req.UserID <= 0 {
		return
	}
	var ids []any
	for _, it := range items {
		if it.Reason.Kind == "featured" && it.rule > 0 {
			ids = append(ids, strconv.FormatInt(it.rule, 10))
		}
	}
	if len(ids) == 0 {
		return
	}
	key := s.RT.UserKey(req.AppID, req.UserID, "pin:"+day)
	pipe := s.RT.Client().Pipeline()
	pipe.SAdd(ctx, key, ids...)
	pipe.Expire(ctx, key, 48*time.Hour)
	_, _ = pipe.Exec(ctx)
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

func (s *Service) reason(snap *model.Snapshot, scene string, c *cand, norm, blend [5]float64, seedItem int32, uc *userCtx) Reason {
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
	if scene == contract.SceneDiscover && (best == 0 || best == 4) {
		return Reason{Kind: "discover", Text: "你可能还没听过"}
	}
	switch best {
	case 1:
		if scene == contract.SceneSimilar {
			if t := title(seedItem); t != "" {
				return Reason{Kind: "seed", Text: "与《" + t + "》相似", RefTitle: t}
			}
		}
		if t := title(c.seed); t != "" {
			if scene == contract.SceneAutoplay {
				return Reason{Kind: "seed", Text: "接着《" + t + "》", RefTitle: t}
			}
			return Reason{Kind: "because_track", Text: "因为你喜欢《" + t + "》", RefTitle: t}
		}
	case 2:
		if c.artist != "" {
			if scene == contract.SceneDiscover {
				return Reason{Kind: "artist", Text: "与你常听的歌手风格相近"}
			}
			if uc != nil && !uc.inModel && len(uc.prefArtists) > 0 {
				return Reason{Kind: "artist", Text: "你选择了 " + c.artist}
			}
			return Reason{Kind: "artist", Text: "你常听 " + c.artist}
		}
	case 0:
		if scene == contract.SceneSimilar {
			if t := title(seedItem); t != "" {
				return Reason{Kind: "seed", Text: "与《" + t + "》相似", RefTitle: t}
			}
		}
		if uc != nil && !uc.inModel && uc.fold != nil {
			return Reason{Kind: "recent_taste", Text: "根据你最近在听的歌"}
		}
		return Reason{Kind: "similar_users", Text: "和你口味相近的人在听"}
	case 4:
		return Reason{Kind: "explore", Text: "换换口味"}
	}
	if scene == contract.SceneArtist {
		return Reason{Kind: "trending", Text: "代表作"}
	}
	return Reason{Kind: "trending", Text: "近期热门"}
}

// popularFallback 还没有模型时：近 7 天听众最多的作品（照样过滤不感兴趣、暂不推荐、屏蔽规则）。
func (s *Service) popularFallback(ctx context.Context, req Request, cfg appcfg.Config, state *realtime.State, rules []store.Rule,
	limit, page int) ([]Item, error) {
	if req.Scene == contract.SceneSimilar {
		return []Item{}, nil
	}
	if req.Scene == contract.SceneArtist {
		return s.artistFallback(ctx, req, state, rules, limit)
	}
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
	blockW, blockA, _ := ruleIndex(rules, req.Scene)
	recent := map[string]bool{}
	for _, e := range state.Recent {
		recent[e.WorkKey] = true
	}
	exclude := map[string]bool{}
	for _, t := range req.Seeds {
		exclude[contract.WorkKey(t.Title, t.Artist)] = true
	}
	excludeT := map[string]bool{}
	for _, tk := range req.Exclude {
		excludeT[tk] = true
	}
	disliked := map[string]bool{}
	for a := range state.DislikedArtists {
		disliked[contract.NormalizeArtist(a)] = true
	}
	nowMs := time.Now().UnixMilli()
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
		art := contract.NormalizeArtist(contract.FirstArtist(t.Artist))
		if _, bad := state.Negative[it.WorkKey]; bad || recent[it.WorkKey] && req.Scene != contract.SceneRadio {
			continue
		}
		if blockW[it.WorkKey] || blockA[art] || disliked[art] || exclude[it.WorkKey] || excludeT[t.Key()] {
			continue
		}
		if until, ok := state.Suppressed[it.WorkKey]; ok && until > nowMs {
			continue
		}
		if _, bad := state.BadVersions[t.Key()]; bad {
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
	_ = cfg
	return out, nil
}

// artistFallback 没有模型时的歌手推荐：近 30 天这位歌手听众最多的作品。
func (s *Service) artistFallback(ctx context.Context, req Request, state *realtime.State, rules []store.Rule, limit int) ([]Item, error) {
	rows, err := s.CH.ArtistTracks(ctx, req.AppID, req.Artist, 30, limit*2)
	if err != nil {
		return nil, err
	}
	blockW, _, _ := ruleIndex(rules, req.Scene)
	out := []Item{}
	var top float64
	for _, r := range rows {
		if _, bad := state.Negative[r.WorkKey]; bad || blockW[r.WorkKey] {
			continue
		}
		if top == 0 {
			top = float64(r.Listeners)
		}
		out = append(out, Item{WorkKey: r.WorkKey, Track: r.Track, Score: round4(float64(r.Listeners) / math.Max(top, 1)),
			Reason: Reason{Kind: "trending", Text: "代表作"}})
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

// ── 当天 / 当周缓存与电台去重 ──

func (s *Service) cachedResult(ctx context.Context, req Request, period string) (*Result, bool) {
	raw, err := s.RT.Client().Get(ctx, s.RT.UserKey(req.AppID, req.UserID, period)).Bytes()
	if err != nil {
		return nil, false
	}
	var res Result
	if json.Unmarshal(raw, &res) != nil {
		return nil, false
	}
	return &res, true
}

func (s *Service) storeResult(ctx context.Context, req Request, period string, res *Result) {
	copyRes := *res
	copyRes.Items = make([]Item, len(res.Items))
	for k, it := range res.Items {
		it.Channels = nil
		copyRes.Items[k] = it
	}
	raw, _ := json.Marshal(copyRes)
	ttl := time.Until(time.UnixMilli(res.ExpiresAt)) + time.Hour
	if err := s.RT.Client().Set(ctx, s.RT.UserKey(req.AppID, req.UserID, period), raw, ttl).Err(); err != nil {
		s.Log.Warn("cache result failed", zap.Error(err))
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

// publishServed 推荐结果发进流（落 ClickHouse impressions 与 rank_samples，算点击率、训练精排）。不阻塞请求：
// 后台带重试发布，NATS 长时间不可用时只丢这条统计记录，不影响用户拿到推荐。
func (s *Service) publishServed(req Request, res *Result) {
	if s.JS == nil || req.UserID <= 0 {
		return
	}
	served := contract.Served{RecID: res.RecID, Scene: res.Scene, AppID: req.AppID, UserID: req.UserID,
		ModelVersion: res.ModelVersion, Personalized: res.Personalized, ColdStart: res.ColdStart, TS: res.GeneratedAt,
		ExperimentID: res.ExperimentID, Variant: res.Variant, Ranker: res.Ranker}
	withFeatures := res.Personalized && res.ModelVersion > 0
	anyRule := false
	for k, it := range res.Items {
		served.Items = append(served.Items, it.WorkKey)
		served.Rules = append(served.Rules, it.rule)
		anyRule = anyRule || it.rule > 0
		if withFeatures {
			f := it.features
			if len(f) != model.NumRankFeatures {
				withFeatures = false
				continue
			}
			x := append([]float32(nil), f...)
			x[model.FeatPosition] = float32(math.Log1p(float64(k)))
			served.Features = append(served.Features, x)
		}
	}
	if !withFeatures {
		served.Features = nil
	}
	if !anyRule {
		served.Rules = nil
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
	switch req.Scene {
	case contract.SceneDaily:
		_, _ = h.Write([]byte(day))
	case contract.SceneDiscover:
		y, w := now.ISOWeek()
		_, _ = h.Write([]byte(strconv.Itoa(y*100 + w)))
	default:
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
	sb.Grow(27)
	var acc uint64
	bits := 0
	all := append([]byte{0}, b[:]...) // 补 0 凑 136 位
	for _, by := range all {
		acc = acc<<8 | uint64(by)
		bits += 8
		for bits >= 5 {
			bits -= 5
			sb.WriteByte(crockford[(acc>>uint(bits))&31])
		}
	}
	// 136 位编码出 27 个字符，第一个恒为补位产生的 0：去掉后正好 26 位
	return sb.String()[1:]
}
