package rec

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"time"

	"go.uber.org/zap"

	"aegis/services/music/contract"
	"aegis/services/music/internal/appcfg"
	"aegis/services/music/internal/model"
	"aegis/services/music/internal/realtime"
	"aegis/services/music/internal/store"
)

// deepSequence 序列模型的输入：实时状态里的正反馈与播放按时间合并（新 → 旧取 maxLen 个），去掉不感兴趣的，再反转为旧 → 新。
func deepSequence(m *model.DeepModel, st *realtime.State) []int32 {
	type ev struct {
		key string
		at  int64
	}
	at := map[string]int64{}
	for _, e := range st.Recent {
		at[e.WorkKey] = max(at[e.WorkKey], e.At)
	}
	for _, e := range st.Positive {
		at[e.WorkKey] = max(at[e.WorkKey], e.At)
	}
	list := make([]ev, 0, len(at))
	for k, t := range at {
		if _, bad := st.Disliked[k]; bad {
			continue
		}
		if neg, bad := st.Negative[k]; bad && neg >= t {
			continue
		}
		list = append(list, ev{k, t})
	}
	sort.Slice(list, func(a, b int) bool {
		if list[a].at != list[b].at {
			return list[a].at > list[b].at
		}
		return list[a].key < list[b].key
	})
	var seq []int32
	for _, e := range list {
		if len(seq) >= m.Meta.MaxLen {
			break
		}
		if i, ok := m.Item(e.key); ok {
			seq = append(seq, i)
		}
	}
	for a, b := 0, len(seq)-1; a < b; a, b = a+1, b-1 {
		seq[a], seq[b] = seq[b], seq[a]
	}
	return seq
}

func snapCount(snap *model.Snapshot, u int32) int {
	if u < 0 || int(u) >= len(snap.UserCounts) {
		return 0
	}
	return int(snap.UserCounts[u])
}

func maxContribution(norm, blend [5]float64) float64 {
	best := 0.0
	for k := range norm {
		best = math.Max(best, norm[k]*blend[k])
	}
	return best
}

func seqReason(scene string, fresh bool) Reason {
	switch {
	case scene == contract.SceneAutoplay:
		return Reason{Kind: "seq", Text: "接着你在听的歌"}
	case fresh:
		return Reason{Kind: "seq", Text: "新歌，和你最近在听的很像"}
	}
	return Reason{Kind: "seq", Text: "根据你的听歌习惯"}
}

// ── 内容画像：用户近期正反馈的标签分布与语义向量均值 ──

type contentProfile struct {
	tags    map[string]float64
	tagNorm float64
	emb     []float64
	embNorm float64
}

func buildContentProfile(ct *appContent, st *realtime.State) contentProfile {
	p := contentProfile{tags: map[string]float64{}}
	if ct == nil || len(ct.items) == 0 {
		return p
	}
	add := func(key string, w float64) {
		ic := ct.items[key]
		if ic == nil {
			return
		}
		for _, t := range ic.Keys {
			p.tags[t] += w
		}
		if len(ic.Embedding) > 0 {
			if p.emb == nil {
				p.emb = make([]float64, len(ic.Embedding))
			}
			if len(ic.Embedding) == len(p.emb) {
				for i, v := range ic.Embedding {
					p.emb[i] += w * float64(v)
				}
			}
		}
	}
	for k, e := range st.Positive {
		if k >= 50 {
			break
		}
		add(e.WorkKey, 1)
	}
	for k, e := range st.Recent {
		if k >= 50 {
			break
		}
		if _, bad := st.Negative[e.WorkKey]; !bad {
			add(e.WorkKey, 0.5)
		}
	}
	for _, v := range p.tags {
		p.tagNorm += v * v
	}
	p.tagNorm = math.Sqrt(p.tagNorm)
	for _, v := range p.emb {
		p.embNorm += v * v
	}
	p.embNorm = math.Sqrt(p.embNorm)
	return p
}

// match 物品与画像的标签余弦、语义余弦（映射到 0–1）；没有内容时都是 0。
func (p contentProfile) match(ic *store.ItemContent) (float32, float32) {
	if ic == nil {
		return 0, 0
	}
	var tag, sem float64
	if p.tagNorm > 0 && len(ic.Keys) > 0 {
		var dot float64
		for _, t := range ic.Keys {
			dot += p.tags[t]
		}
		tag = dot / (p.tagNorm * math.Sqrt(float64(len(ic.Keys))))
	}
	if p.embNorm > 0 && len(ic.Embedding) == len(p.emb) {
		var dot, n float64
		for i, v := range ic.Embedding {
			dot += float64(v) * p.emb[i]
			n += float64(v) * float64(v)
		}
		if n > 0 {
			sem = (dot/(p.embNorm*math.Sqrt(n)) + 1) / 2
		}
	}
	return float32(tag), float32(sem)
}

// insertResolve 待解析项（大模型推荐、曲库里没有）插进结果：每 3 首插 1 首，最多占 ratio；不喜欢的歌手、被屏蔽的歌手不插。
func insertResolve(out, extra []Item, limit int, ratio float64, disliked, blockA map[string]bool, explain bool) []Item {
	maxN := int(math.Floor(float64(limit) * ratio))
	if maxN <= 0 || len(extra) == 0 {
		return out
	}
	in := map[string]bool{}
	for _, it := range out {
		in[it.WorkKey] = true
	}
	var picks []Item
	for _, it := range extra {
		art := contract.NormalizeArtist(contract.FirstArtist(it.Track.Artist))
		if in[it.WorkKey] || disliked[art] || blockA[art] || it.WorkKey == "" {
			continue
		}
		in[it.WorkKey] = true
		if explain {
			it.Channels = map[string]float64{chLLM: 1}
		}
		picks = append(picks, it)
		if len(picks) >= maxN {
			break
		}
	}
	for k, it := range picks {
		pos := min(2+3*k, len(out))
		out = append(out[:pos], append([]Item{it}, out[pos:]...)...)
	}
	if len(out) > limit {
		// 截断时优先保留插进来的扩展项之外的普通结果：从尾部删
		out = out[:limit]
	}
	return out
}

// ── 大模型曲库扩展 ──

type expandItem struct {
	Title   string `json:"title"`
	Artist  string `json:"artist"`
	WorkKey string `json:"workKey"`
}

var expandScenes = map[string]bool{contract.SceneHome: true, contract.SceneDaily: true, contract.SceneDiscover: true, contract.SceneRadio: true}

// llmExpand 行为少的人（或每周雷达）：按最近喜欢的歌、偏好歌手与语种，请大模型推荐 30 首真实存在、不在列表里的歌。
// 按人按天缓存；没有缓存时在后台请求，这次先不带（不让请求等大模型）。
func (s *Service) llmExpand(ctx context.Context, req Request, cfg appcfg.Config, st *realtime.State, prefs store.Prefs,
	personal, weak bool, day string) []expandItem {
	if !s.LLM.Enabled() || !cfg.Content.Enabled || !cfg.Content.LLMExpand || !personal || req.UserID <= 0 {
		return nil
	}
	if !expandScenes[req.Scene] || !(weak || req.Scene == contract.SceneDiscover) {
		return nil
	}
	key := s.RT.UserKey(req.AppID, req.UserID, "llmexp:"+day)
	if raw, err := s.RT.Client().Get(ctx, key).Bytes(); err == nil {
		var cached []expandItem
		if json.Unmarshal(raw, &cached) == nil {
			return cached
		}
	}
	if req.Explain {
		return nil
	}
	// 同一人同一天只请求一次：先占位，失败也不反复打
	if ok, err := s.RT.Client().SetNX(ctx, key+":lock", "1", 10*time.Minute).Result(); err != nil || !ok {
		return nil
	}
	go s.fetchExpand(req.AppID, st, prefs, key)
	return nil
}

// ExpandSystemPrompt 曲库扩展的系统提示词。
const ExpandSystemPrompt = "你是资深的音乐推荐编辑。根据用户最近喜欢的歌、喜欢的歌手、偏好的语种，推荐 30 首他可能喜欢但不在列表里的歌。" +
	"必须是真实存在、能在主流音乐平台搜到的歌，用最常见的歌名与歌手写法；不要推荐列表里已有的歌，不要推荐用户不喜欢的歌手；" +
	"约三分之一可以是风格相近但用户还没听过的歌手。" + `只输出 JSON：{"items":[{"title":"","artist":""}]}`

func (s *Service) fetchExpand(appID int64, st *realtime.State, prefs store.Prefs, key string) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var keys []string
	for k, e := range st.Positive {
		if k >= 20 {
			break
		}
		keys = append(keys, e.WorkKey)
	}
	for k, e := range st.Recent {
		if len(keys) >= 30 || k >= 30 {
			break
		}
		if _, bad := st.Negative[e.WorkKey]; !bad {
			keys = append(keys, e.WorkKey)
		}
	}
	tracks, _ := s.PG.Tracks(ctx, appID, keys)
	var liked []string
	seen := map[string]bool{}
	for _, k := range keys {
		if t, ok := tracks[k]; ok && !seen[k] {
			seen[k] = true
			liked = append(liked, t.Title+" - "+t.Artist)
		}
	}
	if len(liked) == 0 && len(prefs.Artists) == 0 {
		return
	}
	disliked := []string{}
	for a := range st.DislikedArtists {
		disliked = append(disliked, a)
	}
	langs := []string{}
	for _, l := range prefs.Languages {
		langs = append(langs, contract.LangTitles[l])
	}
	raw, _ := json.Marshal(map[string]any{"liked": liked, "favoriteArtists": prefs.Artists, "languages": langs,
		"dislikedArtists": disliked, "count": 30})
	var out struct {
		Items []expandItem `json:"items"`
	}
	usage, err := s.LLM.ChatJSON(ctx, ExpandSystemPrompt, string(raw), &out)
	_ = s.PG.AddUsage(ctx, appID, "expand", 1, usage.TotalTokens, len(out.Items))
	if err != nil {
		s.Log.Debug("llm expand failed", zap.Error(err))
		return
	}
	items := []expandItem{}
	have := map[string]bool{}
	for _, k := range keys {
		have[k] = true
	}
	for _, it := range out.Items {
		it.Title, it.Artist = strings.TrimSpace(it.Title), strings.TrimSpace(it.Artist)
		it.WorkKey = contract.WorkKey(it.Title, it.Artist)
		if it.WorkKey == "" || it.Artist == "" || have[it.WorkKey] || len([]rune(it.Title)) > 100 || len([]rune(it.Artist)) > 100 {
			continue
		}
		have[it.WorkKey] = true
		items = append(items, it)
		if len(items) >= 30 {
			break
		}
	}
	if b, err := json.Marshal(items); err == nil {
		_ = s.RT.Client().Set(ctx, key, b, 26*time.Hour).Err()
	}
}
