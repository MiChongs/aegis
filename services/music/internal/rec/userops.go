package rec

import (
	"context"
	"encoding/json"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go/jetstream"
	"go.uber.org/zap"

	"aegis/services/music/contract"
	"aegis/services/music/internal/model"
	"aegis/services/music/internal/realtime"
	"aegis/services/music/internal/store"
)

// 用户侧操作（docs/music-personalization-2.md「客户端接口增量」）：偏好、不喜欢列表与撤销、导出、热搜、搜索重排。

// PrefsPatch PUT /music/preferences 的请求体（缺省字段保持原值）。
type PrefsPatch struct {
	Personalized *bool    `json:"personalized"`
	Languages    []string `json:"languages"`
	Artists      []string `json:"artists"`
	Autoplay     *bool    `json:"autoplay"`
	Onboarded    *bool    `json:"onboarded"`
	Skipped      *bool    `json:"skipped"`
}

// GetPrefs 读偏好。
func (s *Service) GetPrefs(ctx context.Context, appID, userID int64) (store.Prefs, error) {
	p, _, err := s.PG.UserPrefs(ctx, appID, userID)
	return p, err
}

// SavePrefs 合并写入偏好。
func (s *Service) SavePrefs(ctx context.Context, appID, userID int64, patch PrefsPatch) (store.Prefs, error) {
	cur, _, err := s.PG.UserPrefs(ctx, appID, userID)
	if err != nil {
		return cur, err
	}
	if patch.Personalized != nil {
		cur.Personalized = *patch.Personalized
	}
	if patch.Autoplay != nil {
		cur.Autoplay = *patch.Autoplay
	}
	if patch.Onboarded != nil {
		cur.Onboarded = *patch.Onboarded
	}
	if patch.Skipped != nil {
		cur.Skipped = *patch.Skipped
	}
	if patch.Languages != nil {
		langs := []string{}
		for _, l := range patch.Languages {
			if !contract.ValidLang(l) {
				return cur, ErrBadRequest{"未知的语种：" + l}
			}
			if !contains(langs, l) {
				langs = append(langs, l)
			}
		}
		cur.Languages = langs
	}
	if patch.Artists != nil {
		if len(patch.Artists) > 50 {
			return cur, ErrBadRequest{"喜欢的歌手最多 50 位"}
		}
		arts := []string{}
		for _, a := range patch.Artists {
			a = strings.TrimSpace(a)
			if a == "" {
				continue
			}
			if len([]rune(a)) > 64 {
				return cur, ErrBadRequest{"歌手名不能超过 64 字"}
			}
			if !contains(arts, a) {
				arts = append(arts, a)
			}
		}
		cur.Artists = arts
	}
	saved, err := s.PG.SaveUserPrefs(ctx, appID, userID, cur)
	if err != nil {
		return saved, err
	}
	s.invalidatePrefs(appID, userID)
	// 偏好变了，当天 / 当周缓存的推荐与风格歌单作废
	s.dropPeriodCaches(ctx, appID, userID)
	return saved, nil
}

func (s *Service) dropPeriodCaches(ctx context.Context, appID, userID int64) {
	local := time.Now().In(s.Loc)
	y, w := local.ISOWeek()
	day := local.Format("20060102")
	keys := []string{
		s.RT.UserKey(appID, userID, "daily:"+day),
		s.RT.UserKey(appID, userID, "discover:"+itoa(y)+"w"+itoa(w)),
		s.RT.UserKey(appID, userID, "mixes:"+day),
	}
	_ = s.RT.Client().Del(ctx, keys...).Err()
}

// FeedbackItem 我的不喜欢里的一项。
type FeedbackItem struct {
	Kind    string          `json:"kind"` // track | artist | suppressed | version
	WorkKey string          `json:"workKey"`
	Track   *contract.Track `json:"track"`
	Artist  string          `json:"artist"`
	Reason  string          `json:"reason"`
	At      int64           `json:"at"`
	Until   int64           `json:"until"`
}

// FeedbackPage 分页结果。
type FeedbackPage struct {
	Items []FeedbackItem `json:"items"`
	Total int            `json:"total"`
}

// Feedback 我的不喜欢；kind 为空时全部（控制台），客户端用 track / artist / suppressed。
func (s *Service) Feedback(ctx context.Context, appID, userID int64, kind string, page, pageSize int) (*FeedbackPage, error) {
	state, err := s.RT.Load(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	cfg, _ := s.Cfgs.Get(ctx, appID)
	items := FeedbackList(state, kind, cfg.SuppressDays)
	out := &FeedbackPage{Items: []FeedbackItem{}, Total: len(items)}
	page, pageSize = max(page, 1), min(max(pageSize, 1), 200)
	start := min((page-1)*pageSize, len(items))
	end := min(start+pageSize, len(items))
	out.Items = append(out.Items, items[start:end]...)
	var keys []string
	for _, it := range out.Items {
		if it.WorkKey != "" {
			keys = append(keys, it.WorkKey)
		}
	}
	tracks, err := s.PG.Tracks(ctx, appID, keys)
	if err == nil {
		for i := range out.Items {
			if t, ok := tracks[out.Items[i].WorkKey]; ok {
				t := t
				out.Items[i].Track = &t
			}
		}
	}
	return out, nil
}

// FeedbackList 实时状态 → 列表（新在前）。
func FeedbackList(state *realtime.State, kind string, suppressDays int) []FeedbackItem {
	var items []FeedbackItem
	if kind == "" || kind == "track" {
		for k, at := range state.Disliked {
			items = append(items, FeedbackItem{Kind: "track", WorkKey: k, Reason: contract.DislikeNotInterested, At: at})
		}
	}
	if kind == "" || kind == "artist" {
		for a, at := range state.DislikedArtists {
			items = append(items, FeedbackItem{Kind: "artist", Artist: a, Reason: contract.DislikeArtist, At: at})
		}
	}
	if kind == "" || kind == "suppressed" {
		days := int64(max(suppressDays, 1))
		for k, until := range state.Suppressed {
			items = append(items, FeedbackItem{Kind: "suppressed", WorkKey: k, Reason: contract.DislikeHeardTooMuch,
				At: until - days*24*3600*1000, Until: until})
		}
	}
	if kind == "" || kind == "version" {
		for tk, at := range state.BadVersions {
			items = append(items, FeedbackItem{Kind: "version", Artist: "", WorkKey: "", Reason: contract.DislikePoorQuality, At: at,
				Track: versionTrack(tk)})
		}
	}
	sort.Slice(items, func(a, b int) bool {
		if items[a].At != items[b].At {
			return items[a].At > items[b].At
		}
		return items[a].WorkKey+items[a].Artist < items[b].WorkKey+items[b].Artist
	})
	if items == nil {
		items = []FeedbackItem{}
	}
	return items
}

func versionTrack(trackKey string) *contract.Track {
	src, id, _ := strings.Cut(trackKey, ":")
	return &contract.Track{Source: src, SongID: id}
}

// UndoFeedback 撤销一项不喜欢：直接改实时状态，并发一条 undislike 事件（训练时抵消原来的负分）。
func (s *Service) UndoFeedback(ctx context.Context, appID, userID int64, kind, workKey, artist string) error {
	now := time.Now()
	ev := contract.Event{ID: "undo" + strings.ToLower(newRecID(now)), Type: contract.TypeUndislike, TS: now.UnixMilli(),
		ReceivedAt: now.UnixMilli(), AppID: appID, UserID: userID, Props: map[string]string{}}
	member := workKey
	switch kind {
	case "track", "suppressed":
		if workKey == "" {
			return ErrBadRequest{"缺少 workKey"}
		}
		ev.WorkKey = workKey
		if tracks, err := s.PG.Tracks(ctx, appID, []string{workKey}); err == nil {
			if t, ok := tracks[workKey]; ok {
				ev.Track, ev.TrackKey = &t, t.Key()
			}
		}
		ev.Props["reason"] = contract.DislikeNotInterested
		if kind == "suppressed" {
			ev.Props["reason"] = contract.DislikeHeardTooMuch
		}
	case "artist":
		artist = strings.TrimSpace(artist)
		if artist == "" {
			return ErrBadRequest{"缺少 artist"}
		}
		member = artist
		ev.Props["scope"], ev.Props["artist"], ev.Props["reason"] = "artist", artist, contract.DislikeArtist
	case "version":
		if workKey == "" {
			return ErrBadRequest{"缺少音源版本"}
		}
		ev.TrackKey = workKey
		ev.Props["reason"] = contract.DislikePoorQuality
	default:
		return ErrBadRequest{"kind 只能是 track、artist、suppressed、version"}
	}
	if err := s.RT.Undo(ctx, appID, userID, kind, member, now.UnixMilli()); err != nil {
		return err
	}
	s.dropPeriodCaches(ctx, appID, userID)
	if s.JS != nil && kind != "version" {
		payload, _ := json.Marshal(ev)
		pctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if _, err := s.JS.Publish(pctx, contract.EventSubject(appID), payload,
			jetstream.WithMsgID(contract.MsgID(appID, userID, ev.ID))); err != nil {
			// 实时状态已经撤销；事件没发出去只影响下次训练里这条负分，记日志
			s.Log.Warn("publish undislike failed", zap.Error(err))
		}
	}
	return nil
}

// ExportPage 导出的一页。
type ExportPage struct {
	Items      []store.ExportRow `json:"items"`
	NextBefore *int64            `json:"nextBefore"`
}

// Export 导出我的听歌记录。
func (s *Service) Export(ctx context.Context, appID, userID, before int64, limit int) (*ExportPage, error) {
	limit = min(max(limit, 1), 1000)
	rows, err := s.CH.Export(ctx, appID, userID, before, limit)
	if err != nil {
		return nil, err
	}
	out := &ExportPage{Items: rows}
	if len(rows) == limit {
		next := rows[len(rows)-1].TS
		out.NextBefore = &next
	}
	return out, nil
}

// TrendingResult 热搜。
type TrendingResult struct {
	Items     []store.TrendingQuery `json:"items"`
	UpdatedAt int64                 `json:"updatedAt"`
}

var trendingCache sync.Map // appID → cached[TrendingResult]

// Trending 热搜（应用级缓存 5 分钟；至少 3 个不同用户搜过才出现）。
func (s *Service) Trending(ctx context.Context, appID int64, limit int) (*TrendingResult, error) {
	limit = min(max(limit, 1), 50)
	if v, ok := trendingCache.Load(appID); ok {
		c := v.(cached[TrendingResult])
		if time.Since(c.at) < 5*time.Minute {
			r := c.v
			if len(r.Items) > limit {
				r.Items = r.Items[:limit]
			}
			return &r, nil
		}
	}
	items, err := s.CH.TrendingSearches(ctx, appID, 3, 50)
	if err != nil {
		return nil, err
	}
	_, blockA, _ := ruleIndex(s.activeRules(ctx, appID, time.Now()), contract.SceneHome)
	kept := []store.TrendingQuery{}
	for _, q := range items {
		if blockA[contract.NormalizeArtist(q.Query)] {
			continue
		}
		kept = append(kept, q)
	}
	res := TrendingResult{Items: kept, UpdatedAt: time.Now().UnixMilli()}
	trendingCache.Store(appID, cached[TrendingResult]{v: res, at: time.Now()})
	if len(res.Items) > limit {
		res.Items = res.Items[:limit]
	}
	return &res, nil
}

// RerankResult 搜索重排结果。
type RerankResult struct {
	Order        []int `json:"order"`
	Personalized bool  `json:"personalized"`
}

// Rerank 搜索结果按口味轻度重排：排序键 = 原位置 − 3 × 口味分（0–1），任何一首最多越过 3 首；不感兴趣的沉底。
func (s *Service) Rerank(ctx context.Context, appID, userID int64, personalized bool, items []contract.Track) (*RerankResult, error) {
	if len(items) > 100 {
		return nil, ErrBadRequest{"最多 100 首"}
	}
	out := &RerankResult{Order: make([]int, len(items))}
	for i := range out.Order {
		out.Order[i] = i
	}
	if !personalized || userID <= 0 || len(items) < 2 {
		return out, nil
	}
	prefs := s.userPrefs(ctx, appID, userID)
	if !prefs.Personalized {
		return out, nil
	}
	cfg, err := s.Cfgs.Get(ctx, appID)
	if err != nil || !cfg.Enabled {
		return out, nil
	}
	state, err := s.RT.Load(ctx, appID, userID)
	if err != nil {
		return out, nil
	}
	out.Personalized = true
	pos := map[string]bool{}
	for _, e := range state.Positive {
		pos[e.WorkKey] = true
	}
	disliked := map[string]bool{}
	for a := range state.DislikedArtists {
		disliked[contract.NormalizeArtist(a)] = true
	}
	taste := make([]float64, len(items))
	bad := make([]bool, len(items))
	var uc *userCtx
	var snap *model.Snapshot
	if l := s.Reg.Get(appID); l != nil {
		snap = l.Snap
		uc = s.buildUserCtx(ctx, l, cfg, Request{AppID: appID, UserID: userID, Personalized: true}, state, prefs, true, time.Now())
	}
	var maxDot float64
	dots := make([]float64, len(items))
	for i, t := range items {
		key := contract.WorkKey(t.Title, t.Artist)
		art := contract.NormalizeArtist(contract.FirstArtist(t.Artist))
		if _, neg := state.Disliked[key]; neg || disliked[art] {
			bad[i] = true
			continue
		}
		if _, bad2 := state.BadVersions[t.Key()]; bad2 {
			bad[i] = true
			continue
		}
		if pos[key] {
			taste[i] += 0.5
		}
		if uc != nil {
			taste[i] += 0.3 * uc.artists[art]
			if uc.vec != nil {
				if it, ok := snap.Item(key); ok {
					dots[i] = math.Max(0, float64(model.Dot(uc.vec, snap.ItemVec(it))))
					maxDot = math.Max(maxDot, dots[i])
				}
			}
		}
	}
	for i := range items {
		if maxDot > 0 {
			taste[i] += 0.4 * dots[i] / maxDot
		}
		taste[i] = math.Min(taste[i], 1)
	}
	keyOf := func(i int) float64 {
		k := float64(i) - 3*taste[i]
		if bad[i] {
			k += 1000
		}
		return k
	}
	sort.SliceStable(out.Order, func(a, b int) bool { return keyOf(out.Order[a]) < keyOf(out.Order[b]) })
	return out, nil
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// UserOp 按 op 分发（参数 JSON 的字段与客户端接口同名）。
func (s *Service) UserOp(ctx context.Context, appID, userID int64, op string, params []byte, personalized bool) (any, error) {
	var p struct {
		Limit    int              `json:"limit"`
		Period   string           `json:"period"`
		Offset   int              `json:"offset"`
		Kind     string           `json:"kind"`
		WorkKey  string           `json:"workKey"`
		Artist   string           `json:"artist"`
		Page     int              `json:"page"`
		PageSize int              `json:"pageSize"`
		Before   int64            `json:"before"`
		Query    string           `json:"query"`
		Items    []contract.Track `json:"items"`
		PrefsPatch
	}
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, ErrBadRequest{"参数格式不正确"}
		}
	}
	switch op {
	case "prefs_get", "prefs_put", "feedback_list", "feedback_undo", "export", "report":
		// 这些不受应用开关以外的限制，但应用关了音乐个性化时一律不可用
		cfg, err := s.Cfgs.Get(ctx, appID)
		if err != nil {
			return nil, err
		}
		if !cfg.Enabled {
			return nil, ErrNotEnabled
		}
	}
	switch op {
	case "mixes":
		return s.Mixes(ctx, appID, userID, personalized)
	case "artists":
		items, err := s.RecommendedArtists(ctx, appID, userID, personalized, orDefault(p.Limit, 12))
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": items}, nil
	case "onboarding":
		return s.GetOnboarding(ctx, appID, userID)
	case "prefs_get":
		return s.GetPrefs(ctx, appID, userID)
	case "prefs_put":
		return s.SavePrefs(ctx, appID, userID, p.PrefsPatch)
	case "feedback_list":
		if p.Kind != "" && p.Kind != "track" && p.Kind != "artist" && p.Kind != "suppressed" {
			return nil, ErrBadRequest{"kind 只能是 track、artist、suppressed"}
		}
		return s.Feedback(ctx, appID, userID, p.Kind, p.Page, orDefault(p.PageSize, 50))
	case "feedback_undo":
		if p.Kind == "version" {
			return nil, ErrBadRequest{"kind 只能是 track、artist、suppressed"}
		}
		if err := s.UndoFeedback(ctx, appID, userID, p.Kind, p.WorkKey, p.Artist); err != nil {
			return nil, err
		}
		return map[string]bool{"removed": true}, nil
	case "report":
		if p.Period == "" {
			p.Period = "week"
		}
		return s.GetReport(ctx, appID, userID, p.Period, p.Offset, true)
	case "trending":
		cfg, err := s.Cfgs.Get(ctx, appID)
		if err != nil {
			return nil, err
		}
		if !cfg.Enabled {
			return nil, ErrNotEnabled
		}
		return s.Trending(ctx, appID, orDefault(p.Limit, 10))
	case "rerank":
		return s.Rerank(ctx, appID, userID, personalized, p.Items)
	case "export":
		return s.Export(ctx, appID, userID, p.Before, orDefault(p.Limit, 500))
	}
	return nil, ErrBadRequest{"未知的操作：" + op}
}

func orDefault(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}
