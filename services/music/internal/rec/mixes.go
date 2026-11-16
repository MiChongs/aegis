package rec

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"math"
	mrand "math/rand/v2"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"aegis/services/music/contract"
	"aegis/services/music/internal/appcfg"
	"aegis/services/music/internal/model"
	"aegis/services/music/internal/realtime"
	"aegis/services/music/internal/store"
)

func newRand(a, b uint64) *mrand.Rand { return mrand.New(mrand.NewPCG(a, b)) }

// ── 风格歌单 ──

// Mix 一个风格歌单（列表里的摘要）。
type Mix struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Subtitle string   `json:"subtitle"`
	Covers   []string `json:"covers"`
	Artists  []string `json:"artists"`
	Size     int      `json:"size"`
	Lang     string   `json:"lang"`
}

// mixFull 缓存里的完整歌单（带曲目）。
type mixFull struct {
	Mix
	Items []Item `json:"items"`
}

// MixList 风格歌单列表。
type MixList struct {
	Items       []Mix `json:"items"`
	GeneratedAt int64 `json:"generatedAt"`
	ExpiresAt   int64 `json:"expiresAt"`
}

type mixCache struct {
	GeneratedAt int64     `json:"generatedAt"`
	ExpiresAt   int64     `json:"expiresAt"`
	Mixes       []mixFull `json:"mixes"`
}

// Mixes 这个人的风格歌单（当天固定；关了个性化、没有模型或数据不足时为空）。
func (s *Service) Mixes(ctx context.Context, appID, userID int64, personalized bool) (*MixList, error) {
	c, err := s.mixes(ctx, appID, userID, personalized, false)
	if err != nil {
		return nil, err
	}
	out := &MixList{Items: []Mix{}, GeneratedAt: c.GeneratedAt, ExpiresAt: c.ExpiresAt}
	for _, m := range c.Mixes {
		out.Items = append(out.Items, m.Mix)
	}
	return out, nil
}

func (s *Service) mixes(ctx context.Context, appID, userID int64, personalized, preview bool) (*mixCache, error) {
	cfg, err := s.Cfgs.Get(ctx, appID)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, ErrNotEnabled
	}
	now := time.Now()
	local := now.In(s.Loc)
	end := time.Date(local.Year(), local.Month(), local.Day()+1, 0, 0, 0, 0, s.Loc)
	empty := &mixCache{GeneratedAt: now.UnixMilli(), ExpiresAt: end.UnixMilli(), Mixes: []mixFull{}}
	prefs := s.userPrefs(ctx, appID, userID)
	if !personalized || userID <= 0 || !prefs.Personalized || cfg.Mixes.Max == 0 {
		return empty, nil
	}
	key := s.RT.UserKey(appID, userID, "mixes:"+local.Format("20060102"))
	if !preview {
		if raw, err := s.RT.Client().Get(ctx, key).Bytes(); err == nil {
			var c mixCache
			if json.Unmarshal(raw, &c) == nil {
				return &c, nil
			}
		}
	}
	l := s.Reg.Get(appID)
	if l == nil {
		return empty, nil
	}
	state, err := s.RT.Load(ctx, appID, userID)
	if err != nil {
		state = realtime.Empty()
	}
	top, err := s.CH.UserTopWorks(ctx, appID, userID, 90, 120)
	if err != nil {
		return nil, err
	}
	built := s.buildMixes(l, cfg, state, top, local.Format("20060102"), userID, s.activeRules(ctx, appID, now), s.content(ctx, appID))
	out := &mixCache{GeneratedAt: now.UnixMilli(), ExpiresAt: end.UnixMilli(), Mixes: built}
	if !preview {
		raw, _ := json.Marshal(out)
		if err := s.RT.Client().Set(ctx, key, raw, time.Until(end)+time.Hour).Err(); err != nil {
			s.Log.Warn("cache mixes failed", zap.Error(err))
		}
	}
	return out, nil
}

// buildMixes 这个人近 90 天加权 Top 作品的向量做余弦 k-means，每簇一个歌单：40% 自己常听的 + 60% 簇中心附近没听过的。
func (s *Service) buildMixes(l *Loaded, cfg appcfg.Config, state *realtime.State, top []store.Scored, day string, userID int64,
	rules []store.Rule, ct *appContent) []mixFull {
	snap := l.Snap
	type own struct {
		i int32
		w float64
	}
	var mine []own
	known := map[int32]bool{}
	for _, t := range top {
		if i, ok := snap.Item(t.WorkKey); ok && snap.ItemNorm(i) > 0 && l.Tracks[i].Source != "" {
			mine = append(mine, own{i, t.Score})
			known[i] = true
		}
	}
	if len(mine) < 12 {
		return []mixFull{}
	}
	k := int(math.Round(float64(len(mine)) / 20))
	k = min(max(k, 2), cfg.Mixes.Max)
	vecs := make([][]float32, len(mine))
	for x, m := range mine {
		vecs[x] = snap.ItemVec(m.i)
	}
	assign := model.KMeans(vecs, k, 12)
	blockW, blockA, _ := ruleIndex(rules, contract.SceneMix)
	disliked := map[string]bool{}
	for a := range state.DislikedArtists {
		disliked[contract.NormalizeArtist(a)] = true
	}
	nowMs := time.Now().UnixMilli()
	ok := func(i int32) bool {
		key := snap.Items[i]
		if l.Tracks[i].Source == "" || blockW[key] || blockA[snap.ItemArtist[i]] || disliked[snap.ItemArtist[i]] {
			return false
		}
		if _, bad := state.Negative[key]; bad {
			return false
		}
		if until, s := state.Suppressed[key]; s && until > nowMs {
			return false
		}
		_, bad := state.BadVersions[l.Tracks[i].Key()]
		return !bad
	}
	used := map[int32]bool{}
	type cluster struct {
		members []own
		weight  float64
	}
	clusters := make([]cluster, k)
	for x, c := range assign {
		clusters[c].members = append(clusters[c].members, mine[x])
		clusters[c].weight += mine[x].w
	}
	sort.SliceStable(clusters, func(a, b int) bool { return clusters[a].weight > clusters[b].weight })
	size := cfg.Mixes.Size
	var out []mixFull
	for _, cl := range clusters {
		if len(cl.members) < 4 {
			continue
		}
		f := snap.Factors
		centroid := make([]float32, f)
		for _, m := range cl.members {
			v, n := snap.ItemVec(m.i), snap.ItemNorm(m.i)
			for d := range centroid {
				centroid[d] += v[d] / n * float32(m.w)
			}
		}
		// 自己常听的（按权重）
		sort.SliceStable(cl.members, func(a, b int) bool { return cl.members[a].w > cl.members[b].w })
		nOwn := size * 4 / 10
		var ownPick []int32
		for _, m := range cl.members {
			if len(ownPick) >= nOwn {
				break
			}
			if !used[m.i] && ok(m.i) {
				ownPick = append(ownPick, m.i)
				used[m.i] = true
			}
		}
		// 簇中心附近没听过的
		scores := make([]float32, len(snap.Items))
		for i := range snap.Items {
			ii := int32(i)
			if known[ii] || used[ii] {
				continue
			}
			if n := snap.ItemNorm(ii); n > 0 {
				scores[i] = model.Dot(centroid, snap.ItemVec(ii)) / n
			}
		}
		near := topScores(scores, size*4)
		sort.Slice(near, func(a, b int) bool { return near[a].Score > near[b].Score })
		var newPick []int32
		artistCount := map[string]int{}
		for _, p := range ownPick {
			artistCount[snap.ItemArtist[p]]++
		}
		for _, p := range near {
			if len(newPick)+len(ownPick) >= size {
				break
			}
			if !ok(p.Item) || used[p.Item] || artistCount[snap.ItemArtist[p.Item]] >= 4 {
				continue
			}
			newPick = append(newPick, p.Item)
			used[p.Item] = true
			artistCount[snap.ItemArtist[p.Item]]++
		}
		if len(ownPick)+len(newPick) < 10 {
			continue
		}
		// 交错排列：新的在前，每两首新歌插一首常听的
		var order []int32
		for a, b := 0, 0; a < len(newPick) || b < len(ownPick); {
			for n := 0; n < 2 && a < len(newPick); n++ {
				order = append(order, newPick[a])
				a++
			}
			if b < len(ownPick) {
				order = append(order, ownPick[b])
				b++
			}
		}
		// 标题：簇里出现最多的歌手
		cnt := map[string]int{}
		name := map[string]string{}
		var langCnt [5]int
		for _, m := range cl.members {
			a := snap.ItemArtist[m.i]
			if a != "" {
				cnt[a]++
				name[a] = snap.ItemArtistN[m.i]
			}
			if int(m.i) < len(snap.ItemLang) {
				langCnt[snap.ItemLang[m.i]]++
			}
		}
		arts := make([]string, 0, len(cnt))
		for a := range cnt {
			arts = append(arts, a)
		}
		sort.Slice(arts, func(x, y int) bool {
			if cnt[arts[x]] != cnt[arts[y]] {
				return cnt[arts[x]] > cnt[arts[y]]
			}
			return arts[x] < arts[y]
		})
		var shown []string
		for _, a := range arts {
			if len(shown) >= 3 {
				break
			}
			shown = append(shown, name[a])
		}
		if len(shown) == 0 {
			continue
		}
		title := strings.Join(shown[:min(2, len(shown))], "、") + " 等"
		mem := make([]mixMember, len(cl.members))
		for x, m := range cl.members {
			mem[x] = mixMember{m.i, m.w}
		}
		if t := mixTitleFromTags(ct, snap, mem, langCnt); t != "" {
			title = t
		}
		lang := 0
		for x := range langCnt {
			if langCnt[x] > langCnt[lang] {
				lang = x
			}
		}
		h := sha1.New()
		_, _ = h.Write([]byte(day + ":" + strconv.FormatInt(userID, 10)))
		for _, m := range cl.members[:min(5, len(cl.members))] {
			_, _ = h.Write([]byte(snap.Items[m.i]))
		}
		m := mixFull{Mix: Mix{ID: "mx_" + hex.EncodeToString(h.Sum(nil))[:12], Title: title, Subtitle: "包含 " + strings.Join(shown, "、"),
			Covers: []string{}, Artists: shown, Size: len(order), Lang: contract.Langs[lang]}}
		ownSet := map[int32]bool{}
		for _, p := range ownPick {
			ownSet[p] = true
		}
		for rank, i := range order {
			reason := Reason{Kind: "mix", Text: "与这组歌风格相近"}
			if ownSet[i] {
				reason = Reason{Kind: "favorite", Text: "你常听"}
			}
			m.Items = append(m.Items, Item{WorkKey: snap.Items[i], Track: l.Tracks[i], Score: round4(1 - float64(rank)/float64(len(order))), Reason: reason})
			if c := l.Tracks[i].Cover; c != "" && len(m.Covers) < 4 && !contains(m.Covers, c) {
				m.Covers = append(m.Covers, c)
			}
		}
		out = append(out, m)
		if len(out) >= cfg.Mixes.Max {
			break
		}
	}
	if out == nil {
		out = []mixFull{}
	}
	return out
}

// mixResult scene=mix：返回某个风格歌单的曲目（与列表同一份缓存，当天固定）。
func (s *Service) mixResult(ctx context.Context, req Request, cfg appcfg.Config, personal bool, asg *Assignment, begin time.Time) (*Result, error) {
	c, err := s.mixes(ctx, req.AppID, req.UserID, personal, req.Explain)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	res := &Result{RecID: newRecID(now), Scene: req.Scene, Personalized: personal, GeneratedAt: now.UnixMilli(),
		ExpiresAt: c.ExpiresAt, Items: []Item{}}
	if l := s.Reg.Get(req.AppID); l != nil {
		res.ModelVersion = l.RunID
	}
	if asg != nil {
		res.ExperimentID, res.Variant = asg.ExperimentID, asg.Variant
	}
	found := false
	for _, m := range c.Mixes {
		if m.ID == req.MixID {
			res.Items = append(res.Items, m.Items...)
			found = true
			break
		}
	}
	if !found {
		return nil, ErrBadRequest{"风格歌单不存在或已过期，请刷新列表"}
	}
	if !req.Explain {
		s.publishServed(req, res)
	}
	_ = cfg
	res.TookMs = time.Since(begin).Milliseconds()
	return res, nil
}

// ── 推荐歌手 ──

// ArtistRec 一位推荐的歌手。
type ArtistRec struct {
	Name   string           `json:"name"`
	Cover  string           `json:"cover"`
	Reason string           `json:"reason"`
	Tracks []contract.Track `json:"tracks"`
}

// RecommendedArtists 相似歌手里这个人还不常听的；没有模型或没有口味时给近期热门歌手。
func (s *Service) RecommendedArtists(ctx context.Context, appID, userID int64, personalized bool, limit int) ([]ArtistRec, error) {
	cfg, err := s.Cfgs.Get(ctx, appID)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, ErrNotEnabled
	}
	limit = min(max(limit, 1), 30)
	l := s.Reg.Get(appID)
	prefs := s.userPrefs(ctx, appID, userID)
	personal := personalized && userID > 0 && prefs.Personalized
	state := realtime.Empty()
	if personal {
		if st, err := s.RT.Load(ctx, appID, userID); err == nil {
			state = st
		}
	}
	disliked := map[string]bool{}
	for a := range state.DislikedArtists {
		disliked[contract.NormalizeArtist(a)] = true
	}
	_, blockA, _ := ruleIndex(s.activeRules(ctx, appID, time.Now()), contract.SceneArtist)
	out := []ArtistRec{}
	if l != nil && personal {
		uc := s.buildUserCtx(ctx, l, cfg, Request{AppID: appID, UserID: userID, Personalized: true}, state, prefs, true, time.Now())
		snap := l.Snap
		score := map[string]float64{}
		via := map[string]string{}
		viaW := map[string]float64{}
		for a, w := range uc.artists {
			for _, n := range snap.ArtistSim[a] {
				if uc.artists[n.Artist] >= 0.3 || disliked[n.Artist] || blockA[n.Artist] {
					continue
				}
				v := w * float64(n.Score)
				score[n.Artist] += v
				if v > viaW[n.Artist] {
					via[n.Artist], viaW[n.Artist] = uc.artistNames[a], v
				}
			}
		}
		// 大模型的歌手知识补充（数据少时 ArtistSim 很稀）：只要曲库里有这位歌手的歌
		ct := s.content(ctx, appID)
		for a, w := range uc.artists {
			k, ok := ct.artists[a]
			if !ok {
				continue
			}
			for rank, name := range k.Similar {
				na := contract.NormalizeArtist(contract.FirstArtist(name))
				if _, has := snap.ArtistTop[na]; !has || uc.artists[na] >= 0.3 || disliked[na] || blockA[na] {
					continue
				}
				v := 0.6 * w / (1 + 0.1*float64(rank))
				score[na] += v
				if v > viaW[na] {
					via[na], viaW[na] = uc.artistNames[a], v
				}
			}
		}
		names := make([]string, 0, len(score))
		for a := range score {
			names = append(names, a)
		}
		sort.Slice(names, func(x, y int) bool {
			if score[names[x]] != score[names[y]] {
				return score[names[x]] > score[names[y]]
			}
			return names[x] < names[y]
		})
		for _, a := range names {
			if len(out) >= limit {
				break
			}
			rec := ArtistRec{Name: snap.ArtistName[a], Tracks: []contract.Track{}}
			if via[a] != "" {
				rec.Reason = "与 " + via[a] + " 风格相近"
			} else {
				rec.Reason = "你可能会喜欢"
			}
			for _, i := range snap.ArtistTop[a] {
				t := l.Tracks[i]
				if t.Source == "" {
					continue
				}
				if _, bad := state.Negative[snap.Items[i]]; bad {
					continue
				}
				if rec.Cover == "" {
					rec.Cover = t.Cover
				}
				rec.Tracks = append(rec.Tracks, t)
				if len(rec.Tracks) >= 3 {
					break
				}
			}
			if rec.Name != "" && len(rec.Tracks) > 0 {
				out = append(out, rec)
			}
		}
	}
	if len(out) >= limit {
		return out, nil
	}
	// 补：近期热门歌手（去掉已经有的、这个人常听的）
	have := map[string]bool{}
	for _, r := range out {
		have[contract.NormalizeArtist(r.Name)] = true
	}
	top, err := s.CH.TopArtists(ctx, appID, 30, limit*3)
	if err != nil {
		return out, nil
	}
	for _, a := range top {
		if len(out) >= limit {
			break
		}
		if have[a.Norm] || disliked[a.Norm] || blockA[a.Norm] {
			continue
		}
		rows, err := s.CH.ArtistTracks(ctx, appID, a.Name, 30, 3)
		if err != nil || len(rows) == 0 {
			continue
		}
		rec := ArtistRec{Name: a.Name, Cover: a.Cover, Reason: "近期热门歌手", Tracks: []contract.Track{}}
		for _, r := range rows {
			rec.Tracks = append(rec.Tracks, r.Track)
			if rec.Cover == "" {
				rec.Cover = r.Track.Cover
			}
		}
		out = append(out, rec)
		have[a.Norm] = true
	}
	return out, nil
}

// ── 新用户偏好 ──

// OnboardingArtist 新用户偏好里的一位歌手。
type OnboardingArtist struct {
	Name  string `json:"name"`
	Cover string `json:"cover"`
	Lang  string `json:"lang"`
}

// LangOption 语种选项。
type LangOption struct {
	Key   string `json:"key"`
	Title string `json:"title"`
}

// Onboarding 新用户偏好选择页的数据。
type Onboarding struct {
	Needed    bool               `json:"needed"`
	Artists   []OnboardingArtist `json:"artists"`
	Languages []LangOption       `json:"languages"`
}

// GetOnboarding 没选过也没跳过、且有效播放少于 20 次时需要弹；歌手按语种穿插，最多 40 位。
func (s *Service) GetOnboarding(ctx context.Context, appID, userID int64) (*Onboarding, error) {
	cfg, err := s.Cfgs.Get(ctx, appID)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, ErrNotEnabled
	}
	out := &Onboarding{Artists: []OnboardingArtist{}, Languages: []LangOption{}}
	for _, l := range contract.Langs {
		out.Languages = append(out.Languages, LangOption{Key: l, Title: contract.LangTitles[l]})
	}
	prefs, _, err := s.PG.UserPrefs(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	if !prefs.Onboarded && !prefs.Skipped {
		n, err := s.CH.UserPlayCount(ctx, appID, userID)
		if err != nil {
			s.Log.Warn("count plays failed", zap.Error(err))
		}
		out.Needed = err == nil && n < 20
	}
	_, blockA, _ := ruleIndex(s.activeRules(ctx, appID, time.Now()), contract.SceneHome)
	byLang := make([][]OnboardingArtist, len(contract.Langs))
	if l := s.Reg.Get(appID); l != nil {
		snap := l.Snap
		type art struct {
			a   string
			pop float64
		}
		var list []art
		for a, items := range snap.ArtistTop {
			if blockA[a] {
				continue
			}
			var p float64
			for _, i := range items {
				p += float64(snap.ItemPop[i])
			}
			list = append(list, art{a, p})
		}
		sort.Slice(list, func(x, y int) bool {
			if list[x].pop != list[y].pop {
				return list[x].pop > list[y].pop
			}
			return list[x].a < list[y].a
		})
		for _, a := range list {
			items := snap.ArtistTop[a.a]
			var cover string
			var langCnt [5]int
			for _, i := range items {
				if cover == "" {
					cover = l.Tracks[i].Cover
				}
				if int(i) < len(snap.ItemLang) {
					langCnt[snap.ItemLang[i]]++
				}
			}
			lang := 0
			for x := range langCnt {
				if langCnt[x] > langCnt[lang] {
					lang = x
				}
			}
			if name := snap.ArtistName[a.a]; name != "" && len(byLang[lang]) < 20 {
				byLang[lang] = append(byLang[lang], OnboardingArtist{Name: name, Cover: cover, Lang: contract.Langs[lang]})
			}
		}
	} else if top, err := s.CH.TopArtists(ctx, appID, 90, 80); err == nil {
		for _, a := range top {
			if blockA[a.Norm] {
				continue
			}
			lang := contract.LangIndex(contract.DetectLang(a.Title, a.Name))
			if len(byLang[lang]) < 20 {
				byLang[lang] = append(byLang[lang], OnboardingArtist{Name: a.Name, Cover: a.Cover, Lang: contract.Langs[lang]})
			}
		}
	}
	// 按语种穿插：每轮每个语种取一位
	for round := 0; len(out.Artists) < 40; round++ {
		added := false
		for _, list := range byLang {
			if round < len(list) && len(out.Artists) < 40 {
				out.Artists = append(out.Artists, list[round])
				added = true
			}
		}
		if !added {
			break
		}
	}
	return out, nil
}

// mixTitleFromTags 簇里一半以上的歌有标签时，用「语种 + 主曲风，主情绪」做标题，如「华语流行，治愈」。
type mixMember struct {
	i int32
	w float64
}

func mixTitleFromTags(ct *appContent, snap *model.Snapshot, members []mixMember, langCnt [5]int) string {
	if ct == nil || len(ct.items) == 0 {
		return ""
	}
	genres, moods := map[string]float64{}, map[string]float64{}
	tagged := 0
	for _, m := range members {
		ic := ct.items[snap.Items[m.i]]
		if ic == nil {
			continue
		}
		tagged++
		for k, g := range ic.Tags.Genres {
			genres[g] += m.w / float64(k+1)
		}
		for k, g := range ic.Tags.Moods {
			moods[g] += m.w / float64(k+1)
		}
	}
	if tagged*2 < len(members) || len(genres) == 0 {
		return ""
	}
	top := func(m map[string]float64) string {
		best, bw := "", -1.0
		for k, w := range m {
			if w > bw || w == bw && k < best {
				best, bw = k, w
			}
		}
		return best
	}
	lang := 0
	for x := range langCnt {
		if langCnt[x] > langCnt[lang] {
			lang = x
		}
	}
	prefix := ""
	if l := contract.Langs[lang]; l != contract.LangOther {
		prefix = contract.LangTitles[l]
	}
	title := prefix + contract.TagTitle(top(genres))
	if m := top(moods); m != "" {
		title += "，" + contract.TagTitle(m)
	}
	return title
}
