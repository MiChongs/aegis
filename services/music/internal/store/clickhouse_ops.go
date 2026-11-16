package store

import (
	"context"
	"math"
	"sort"
	"strings"
	"time"

	"aegis/services/music/contract"
)

// ── 第二期管理端统计：按模型版本 / 实验变体 / 运营规则的漏斗、搜索分析、歌手 ──

// sqlFirstArtist SQL 里取第一歌手（与 contract.FirstArtist 的分隔符一致），小写后作分组键。
const sqlFirstArtist = `lowerUTF8(trimBoth(arrayElement(splitByRegexp('/|、|,|，|&|;|；| feat\\.| feat | ft\\.| x | X |\\|', artist), 1)))`

const sqlClickTypes = `('click', 'search_click', 'play_start', 'like', 'playlist_add', 'download', 'share')`

// ModelRow 某个模型版本出的推荐的效果。
type ModelRow struct {
	Version     int64   `json:"version"`
	Requests    int64   `json:"requests"`
	Impressions int64   `json:"impressions"`
	Clicks      int64   `json:"clicks"`
	Plays       int64   `json:"plays"`
	CTR         float64 `json:"ctr"`
	PlayRate    float64 `json:"playRate"`
}

// ModelFunnel 按模型版本的请求 → 曝光 → 点击 → 播放（按 recId 去重）。
func (c *CH) ModelFunnel(ctx context.Context, appID int64, from time.Time) ([]ModelRow, error) {
	app := uint64(appID)
	by := map[int64]*ModelRow{}
	get := func(v int64) *ModelRow {
		if r := by[v]; r != nil {
			return r
		}
		r := &ModelRow{Version: v}
		by[v] = r
		return r
	}
	rows, err := c.conn.Query(ctx, `SELECT model_version, count() FROM impressions FINAL WHERE app_id = ? AND ts >= ? GROUP BY model_version`, app, from.UTC())
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var v, n uint64
		if err := rows.Scan(&v, &n); err != nil {
			rows.Close()
			return nil, err
		}
		get(int64(v)).Requests = int64(n)
	}
	rows.Close()
	rows, err = c.conn.Query(ctx, `
SELECT i.model_version, uniqExactIf(e.rec_id, e.type = 'impression'), uniqExactIf(e.rec_id, e.type IN `+sqlClickTypes+`),
       uniqExactIf(e.rec_id, e.type = 'play_start')
FROM (SELECT rec_id, type FROM events WHERE app_id = ? AND ts >= ? AND rec_id != '') AS e
INNER JOIN (SELECT rec_id, model_version FROM impressions FINAL WHERE app_id = ? AND ts >= ?) AS i ON i.rec_id = e.rec_id
GROUP BY i.model_version`, app, from.UTC(), app, from.UTC())
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var v, imp, cl, pl uint64
		if err := rows.Scan(&v, &imp, &cl, &pl); err != nil {
			rows.Close()
			return nil, err
		}
		r := get(int64(v))
		r.Impressions, r.Clicks, r.Plays = int64(imp), int64(cl), int64(pl)
	}
	rows.Close()
	out := make([]ModelRow, 0, len(by))
	for _, r := range by {
		if r.Impressions > 0 {
			r.CTR = float64(r.Clicks) / float64(r.Impressions)
			r.PlayRate = float64(r.Plays) / float64(r.Impressions)
		}
		out = append(out, *r)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Version > out[b].Version })
	return out, nil
}

// ReasonCount 不感兴趣原因的次数。
type ReasonCount struct {
	Reason string `json:"reason"`
	Count  int64  `json:"count"`
}

// FeedbackReasons 窗口内不感兴趣的原因分布。
func (c *CH) FeedbackReasons(ctx context.Context, appID int64, from time.Time) ([]ReasonCount, error) {
	rows, err := c.conn.Query(ctx, `
SELECT multiIf(props['scope'] = 'artist', 'dislike_artist', props['reason'] = '', 'not_interested', props['reason']) AS r, count()
FROM events FINAL WHERE app_id = ? AND ts >= ? AND type = 'dislike' GROUP BY r ORDER BY count() DESC`, uint64(appID), from.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReasonCount{}
	for rows.Next() {
		var r ReasonCount
		var n uint64
		if err := rows.Scan(&r.Reason, &n); err != nil {
			return nil, err
		}
		r.Count = int64(n)
		out = append(out, r)
	}
	return out, rows.Err()
}

// RuleStat 某条规则影响的曝光与点击。
type RuleStat struct {
	Served int64 `json:"served"`
	Clicks int64 `json:"clicks"`
}

// RuleStats 窗口内每条规则影响的推荐条目数与其中被点击的条目数。
func (c *CH) RuleStats(ctx context.Context, appID int64, from time.Time) (map[int64]RuleStat, error) {
	app := uint64(appID)
	out := map[int64]RuleStat{}
	rows, err := c.conn.Query(ctx, `
SELECT r, count() FROM impressions FINAL ARRAY JOIN rules AS r
WHERE app_id = ? AND ts >= ? AND r > 0 GROUP BY r`, app, from.UTC())
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id, n uint64
		if err := rows.Scan(&id, &n); err != nil {
			rows.Close()
			return nil, err
		}
		out[int64(id)] = RuleStat{Served: int64(n)}
	}
	rows.Close()
	rows, err = c.conn.Query(ctx, `
SELECT x.r, uniqExact(e.rec_id, e.work_key)
FROM (SELECT rec_id, r, w FROM impressions FINAL ARRAY JOIN rules AS r, items AS w WHERE app_id = ? AND ts >= ? AND r > 0) AS x
INNER JOIN (SELECT rec_id, work_key FROM events WHERE app_id = ? AND ts >= ? AND rec_id != '' AND type IN `+sqlClickTypes+`) AS e
  ON e.rec_id = x.rec_id AND e.work_key = x.w
GROUP BY x.r`, app, from.UTC(), app, from.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, n uint64
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		st := out[int64(id)]
		st.Clicks = int64(n)
		out[int64(id)] = st
	}
	return out, rows.Err()
}

// VariantStat 实验某变体的效果。
type VariantStat struct {
	Key            string    `json:"key"`
	Name           string    `json:"name"`
	Users          int64     `json:"users"`
	Requests       int64     `json:"requests"`
	Impressions    int64     `json:"impressions"`
	Clicks         int64     `json:"clicks"`
	Plays          int64     `json:"plays"`
	Completes      int64     `json:"completes"`
	Minutes        int64     `json:"minutes"`
	CTR            float64   `json:"ctr"`
	PlayRate       float64   `json:"playRate"`
	CompletionRate float64   `json:"completionRate"`
	MinutesPerUser float64   `json:"minutesPerUser"`
	Lift           *RatePair `json:"lift"`
	PValue         *RatePair `json:"pValue"`
	playEnds       int64
}

// RatePair 点击率与播放率两个数。
type RatePair struct {
	CTR      float64 `json:"ctr"`
	PlayRate float64 `json:"playRate"`
}

// VariantDay 某天某变体。
type VariantDay struct {
	Day      string  `json:"day"`
	Variant  string  `json:"variant"`
	CTR      float64 `json:"ctr"`
	PlayRate float64 `json:"playRate"`
}

// ExperimentResults 一个实验的结果。
type ExperimentResults struct {
	ExperimentID int64         `json:"experimentId"`
	Days         int           `json:"days"`
	Variants     []VariantStat `json:"variants"`
	Daily        []VariantDay  `json:"daily"`
}

// Experiment 实验各变体的漏斗与显著性（两比例 z 检验，对照为第一个变体）。variants 为 (key, name) 顺序表。
func (c *CH) Experiment(ctx context.Context, appID, expID int64, from time.Time, days int, variants [][2]string) (*ExperimentResults, error) {
	app := uint64(appID)
	by := map[string]*VariantStat{}
	out := &ExperimentResults{ExperimentID: expID, Days: days, Variants: []VariantStat{}, Daily: []VariantDay{}}
	for _, v := range variants {
		by[v[0]] = &VariantStat{Key: v[0], Name: v[1]}
	}
	get := func(k string) *VariantStat {
		if r := by[k]; r != nil {
			return r
		}
		r := &VariantStat{Key: k, Name: k}
		by[k] = r
		variants = append(variants, [2]string{k, k})
		return r
	}
	rows, err := c.conn.Query(ctx, `
SELECT variant, uniqExact(user_id), count() FROM impressions FINAL
WHERE app_id = ? AND experiment_id = ? AND ts >= ? GROUP BY variant`, app, uint64(expID), from.UTC())
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k string
		var u, n uint64
		if err := rows.Scan(&k, &u, &n); err != nil {
			rows.Close()
			return nil, err
		}
		r := get(k)
		r.Users, r.Requests = int64(u), int64(n)
	}
	rows.Close()
	join := `
FROM (SELECT rec_id, type, reason, duration_ms, played_ms, ts FROM events WHERE app_id = ? AND ts >= ? AND rec_id != '') AS e
INNER JOIN (SELECT rec_id, variant FROM impressions FINAL WHERE app_id = ? AND experiment_id = ? AND ts >= ?) AS i ON i.rec_id = e.rec_id`
	args := []any{app, from.UTC(), app, uint64(expID), from.UTC()}
	rows, err = c.conn.Query(ctx, `
SELECT i.variant, uniqExactIf(e.rec_id, e.type = 'impression'), uniqExactIf(e.rec_id, e.type IN `+sqlClickTypes+`),
       uniqExactIf(e.rec_id, e.type = 'play_start'),
       countIf(e.type = 'play_end' AND (e.reason = 'completed' OR (e.duration_ms > 0 AND e.played_ms >= e.duration_ms * 0.8))),
       countIf(e.type = 'play_end'), sumIf(e.played_ms, e.type = 'play_end')`+join+`
GROUP BY i.variant`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k string
		var imp, cl, pl, comp, ends, ms uint64
		if err := rows.Scan(&k, &imp, &cl, &pl, &comp, &ends, &ms); err != nil {
			rows.Close()
			return nil, err
		}
		r := get(k)
		r.Impressions, r.Clicks, r.Plays, r.Completes, r.playEnds, r.Minutes = int64(imp), int64(cl), int64(pl), int64(comp), int64(ends), int64(ms/60000)
	}
	rows.Close()
	rows, err = c.conn.Query(ctx, `
SELECT toString(toDate(e.ts, '`+c.TZ+`')) AS day, i.variant,
       uniqExactIf(e.rec_id, e.type = 'impression'), uniqExactIf(e.rec_id, e.type IN `+sqlClickTypes+`), uniqExactIf(e.rec_id, e.type = 'play_start')`+join+`
GROUP BY day, i.variant ORDER BY day, i.variant`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var d VariantDay
		var imp, cl, pl uint64
		if err := rows.Scan(&d.Day, &d.Variant, &imp, &cl, &pl); err != nil {
			rows.Close()
			return nil, err
		}
		if imp > 0 {
			d.CTR, d.PlayRate = float64(cl)/float64(imp), float64(pl)/float64(imp)
		}
		out.Daily = append(out.Daily, d)
	}
	rows.Close()
	var control *VariantStat
	for _, v := range variants {
		r := by[v[0]]
		if r.Impressions > 0 {
			r.CTR = float64(r.Clicks) / float64(r.Impressions)
			r.PlayRate = float64(r.Plays) / float64(r.Impressions)
		}
		if r.playEnds > 0 {
			r.CompletionRate = float64(r.Completes) / float64(r.playEnds)
		}
		if r.Users > 0 {
			r.MinutesPerUser = math.Round(float64(r.Minutes)/float64(r.Users)*100) / 100
		}
		if control == nil {
			control = r
		} else if control.Impressions > 0 && r.Impressions > 0 {
			lift := RatePair{}
			if control.CTR > 0 {
				lift.CTR = r.CTR/control.CTR - 1
			}
			if control.PlayRate > 0 {
				lift.PlayRate = r.PlayRate/control.PlayRate - 1
			}
			r.Lift = &lift
			r.PValue = &RatePair{CTR: ZTest(control.Clicks, control.Impressions, r.Clicks, r.Impressions),
				PlayRate: ZTest(control.Plays, control.Impressions, r.Plays, r.Impressions)}
		}
		out.Variants = append(out.Variants, *r)
	}
	return out, nil
}

// ZTest 两比例 z 检验的双侧 p 值（样本为 0 或方差为 0 时返回 1）。
func ZTest(x1, n1, x2, n2 int64) float64 {
	if n1 <= 0 || n2 <= 0 {
		return 1
	}
	p1, p2 := float64(x1)/float64(n1), float64(x2)/float64(n2)
	p := float64(x1+x2) / float64(n1+n2)
	se := math.Sqrt(p * (1 - p) * (1/float64(n1) + 1/float64(n2)))
	if se == 0 {
		return 1
	}
	z := math.Abs(p1-p2) / se
	return math.Round(math.Erfc(z/math.Sqrt2)*1e6) / 1e6
}

// SearchQueryRow 一个搜索词。
type SearchQueryRow struct {
	Query     string  `json:"query"`
	Searches  int64   `json:"searches"`
	Users     int64   `json:"users"`
	Clicks    int64   `json:"clicks"`
	ClickRate float64 `json:"clickRate"`
}

// SearchDay 一天的搜索。
type SearchDay struct {
	Day       string  `json:"day"`
	Searches  int64   `json:"searches"`
	ClickRate float64 `json:"clickRate"`
}

// SearchStats 搜索分析。
type SearchStats struct {
	Days      int              `json:"days"`
	Searches  int64            `json:"searches"`
	Searchers int64            `json:"searchers"`
	ClickRate float64          `json:"clickRate"`
	Top       []SearchQueryRow `json:"top"`
	NoClick   []SearchQueryRow `json:"noClick"`
	Daily     []SearchDay      `json:"daily"`
}

// Search 搜索分析：「点击率」= 同一人同一天同一个词里有 search_click 的占比。
func (c *CH) Search(ctx context.Context, appID int64, days, limit int) (*SearchStats, error) {
	app := uint64(appID)
	from := time.Now().Add(-time.Duration(days) * 24 * time.Hour).UTC()
	out := &SearchStats{Days: days, Top: []SearchQueryRow{}, NoClick: []SearchQueryRow{}, Daily: []SearchDay{}}
	base := `
SELECT lowerUTF8(trimBoth(query)) AS q, user_id, toDate(ts, '` + c.TZ + `') AS d, countIf(type = 'search') AS s, countIf(type = 'search_click') AS cl
FROM events WHERE app_id = ? AND ts >= ? AND type IN ('search', 'search_click') AND query != ''
GROUP BY q, user_id, d`
	var s, u, sessions, clicked uint64
	if err := c.conn.QueryRow(ctx, `SELECT sum(s), uniqExact(user_id), countIf(s > 0), countIf(s > 0 AND cl > 0) FROM (`+base+`)`, app, from).
		Scan(&s, &u, &sessions, &clicked); err != nil {
		return nil, err
	}
	out.Searches, out.Searchers, out.ClickRate = int64(s), int64(u), rate(clicked, sessions)
	rows, err := c.conn.Query(ctx, `
SELECT q, sum(s), uniqExactIf(user_id, s > 0), sum(cl), countIf(s > 0 AND cl > 0), countIf(s > 0)
FROM (`+base+`) GROUP BY q HAVING sum(s) > 0 ORDER BY sum(s) DESC, q LIMIT ?`, app, from, limit*4)
	if err != nil {
		return nil, err
	}
	var all []SearchQueryRow
	for rows.Next() {
		var r SearchQueryRow
		var srch, users, cl, okSess, sess uint64
		if err := rows.Scan(&r.Query, &srch, &users, &cl, &okSess, &sess); err != nil {
			rows.Close()
			return nil, err
		}
		r.Searches, r.Users, r.Clicks, r.ClickRate = int64(srch), int64(users), int64(cl), rate(okSess, sess)
		all = append(all, r)
	}
	rows.Close()
	for _, r := range all {
		if len(out.Top) < limit {
			out.Top = append(out.Top, r)
		}
		if r.Clicks == 0 && r.Searches >= 2 && len(out.NoClick) < limit {
			out.NoClick = append(out.NoClick, SearchQueryRow{Query: r.Query, Searches: r.Searches, Users: r.Users})
		}
	}
	rows, err = c.conn.Query(ctx, `SELECT toString(d), sum(s), countIf(s > 0 AND cl > 0), countIf(s > 0) FROM (`+base+`) GROUP BY d ORDER BY d`, app, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d SearchDay
		var srch, okSess, sess uint64
		if err := rows.Scan(&d.Day, &srch, &okSess, &sess); err != nil {
			return nil, err
		}
		d.Searches, d.ClickRate = int64(srch), rate(okSess, sess)
		out.Daily = append(out.Daily, d)
	}
	return out, rows.Err()
}

// ArtistRow 歌手排行一行。
type ArtistRow struct {
	Name           string  `json:"name"`
	Plays          int64   `json:"plays"`
	Listeners      int64   `json:"listeners"`
	Minutes        int64   `json:"minutes"`
	Tracks         int64   `json:"tracks"`
	CompletionRate float64 `json:"completionRate"`
	SkipRate       float64 `json:"skipRate"`
	Likes          int64   `json:"likes"`
	Cover          string  `json:"cover"`
	norm           string
	completes      int64
	skips          int64
}

const sqlArtistAgg = `
  countIf(` + sqlIsPlay + `) AS plays, uniqExactIf(user_id, ` + sqlIsPlay + `) AS listeners, sumIf(played_ms, type = 'play_end') AS ms,
  uniqExactIf(work_key, type = 'play_end') AS tracks, countIf(` + sqlIsComplete + `) AS completes, countIf(` + sqlIsSkip + `) AS skips,
  countIf(type = 'like') - countIf(type = 'unlike') AS likes, argMaxIf(cover, ts, cover != '') AS cover, argMax(artist, ts) AS raw`

func scanArtist(r *ArtistRow, plays, listeners, ms, tracks, completes, skips uint64, likes int64, cover, raw string) {
	r.Plays, r.Listeners, r.Minutes, r.Tracks = int64(plays), int64(listeners), int64(ms/60000), int64(tracks)
	r.completes, r.skips, r.Likes, r.Cover = int64(completes), int64(skips), max(likes, 0), cover
	r.Name = contract.FirstArtist(raw)
	r.norm = contract.NormalizeArtist(r.Name)
	if r.Plays > 0 {
		r.CompletionRate = float64(r.completes) / float64(r.Plays)
		r.SkipRate = float64(r.skips) / float64(r.Plays)
	}
}

// Artists 歌手排行（按第一歌手合并，按播放次数），q 按名字包含过滤。
func (c *CH) Artists(ctx context.Context, appID int64, days int, q string, page, pageSize int) ([]ArtistRow, int64, error) {
	from := time.Now().Add(-time.Duration(days) * 24 * time.Hour).UTC()
	rows, err := c.conn.Query(ctx, `
SELECT `+sqlFirstArtist+` AS a,`+sqlArtistAgg+`
FROM events FINAL
WHERE app_id = ? AND ts >= ? AND artist != '' AND type IN ('play_end', 'like', 'unlike')
GROUP BY a HAVING a != '' AND plays > 0 ORDER BY plays DESC LIMIT 5000`, uint64(appID), from)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	merged := map[string]*ArtistRow{}
	var order []string
	q = strings.ToLower(strings.TrimSpace(q))
	for rows.Next() {
		var key, cover, raw string
		var plays, listeners, ms, tracks, completes, skips uint64
		var likes int64
		if err := rows.Scan(&key, &plays, &listeners, &ms, &tracks, &completes, &skips, &likes, &cover, &raw); err != nil {
			return nil, 0, err
		}
		var r ArtistRow
		scanArtist(&r, plays, listeners, ms, tracks, completes, skips, likes, cover, raw)
		if r.norm == "" || q != "" && !strings.Contains(strings.ToLower(r.Name), q) {
			continue
		}
		if m := merged[r.norm]; m != nil {
			m.Plays += r.Plays
			m.Listeners += r.Listeners
			m.Minutes += r.Minutes
			m.Tracks += r.Tracks
			m.completes += r.completes
			m.skips += r.skips
			m.Likes += r.Likes
			if m.Plays > 0 {
				m.CompletionRate, m.SkipRate = float64(m.completes)/float64(m.Plays), float64(m.skips)/float64(m.Plays)
			}
			continue
		}
		merged[r.norm] = &r
		order = append(order, r.norm)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	list := make([]ArtistRow, 0, len(order))
	for _, k := range order {
		list = append(list, *merged[k])
	}
	sort.SliceStable(list, func(a, b int) bool { return list[a].Plays > list[b].Plays })
	total := int64(len(list))
	start := min((page-1)*pageSize, len(list))
	end := min(start+pageSize, len(list))
	return list[start:end], total, nil
}

// ArtistDetailStats 歌手详情的统计部分。
type ArtistDetailStats struct {
	ArtistRow
	Daily     []TrackDay       `json:"daily"`
	TopTracks []ArtistTopTrack `json:"topTracks"`
	Dislikes  int64            `json:"dislikes"`
}

// ArtistTopTrack 歌手的代表作。
type ArtistTopTrack struct {
	WorkKey   string         `json:"workKey"`
	Track     contract.Track `json:"track"`
	Plays     int64          `json:"plays"`
	Listeners int64          `json:"listeners"`
}

// ArtistDetail 一位歌手的统计、每日走势、代表作与不喜欢人数（找不到返回 nil）。
func (c *CH) ArtistDetail(ctx context.Context, appID int64, name string, days int) (*ArtistDetailStats, error) {
	key := strings.ToLower(strings.TrimSpace(contract.FirstArtist(name)))
	if key == "" {
		return nil, nil
	}
	app := uint64(appID)
	from := time.Now().Add(-time.Duration(days) * 24 * time.Hour).UTC()
	where := `app_id = ? AND ts >= ? AND artist != '' AND ` + sqlFirstArtist + ` = ?`
	out := &ArtistDetailStats{Daily: []TrackDay{}, TopTracks: []ArtistTopTrack{}}
	var plays, listeners, ms, tracks, completes, skips uint64
	var likes int64
	var cover, raw string
	if err := c.conn.QueryRow(ctx, `SELECT `+strings.TrimPrefix(sqlArtistAgg, "\n")+` FROM events FINAL WHERE `+where+
		` AND type IN ('play_end', 'like', 'unlike')`, app, from, key).
		Scan(&plays, &listeners, &ms, &tracks, &completes, &skips, &likes, &cover, &raw); err != nil {
		return nil, err
	}
	if plays == 0 && likes == 0 {
		return nil, nil
	}
	scanArtist(&out.ArtistRow, plays, listeners, ms, tracks, completes, skips, likes, cover, raw)
	rows, err := c.conn.Query(ctx, `
SELECT toString(toDate(ts, '`+c.TZ+`')) AS d, countIf(`+sqlIsPlay+`), uniqExactIf(user_id, `+sqlIsPlay+`)
FROM events FINAL WHERE `+where+` AND type = 'play_end' GROUP BY d ORDER BY d`, app, from, key)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var d TrackDay
		var p, l uint64
		if err := rows.Scan(&d.Day, &p, &l); err != nil {
			rows.Close()
			return nil, err
		}
		d.Plays, d.Listeners = int64(p), int64(l)
		out.Daily = append(out.Daily, d)
	}
	rows.Close()
	rows, err = c.conn.Query(ctx, `
SELECT work_key, argMax(source, ts), argMax(song_id, ts), argMax(title, ts), argMax(artist, ts), argMax(album, ts),
       argMaxIf(cover, ts, cover != ''), max(duration_ms), countIf(`+sqlIsPlay+`) AS p, uniqExactIf(user_id, `+sqlIsPlay+`)
FROM events FINAL WHERE `+where+` AND type = 'play_end' AND work_key != ''
GROUP BY work_key ORDER BY p DESC LIMIT 20`, app, from, key)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var t ArtistTopTrack
		var dur uint32
		var p, l uint64
		if err := rows.Scan(&t.WorkKey, &t.Track.Source, &t.Track.SongID, &t.Track.Title, &t.Track.Artist, &t.Track.Album,
			&t.Track.Cover, &dur, &p, &l); err != nil {
			rows.Close()
			return nil, err
		}
		t.Track.DurationMs, t.Plays, t.Listeners = int64(dur), int64(p), int64(l)
		out.TopTracks = append(out.TopTracks, t)
	}
	rows.Close()
	var dis uint64
	if err := c.conn.QueryRow(ctx, `SELECT uniqExact(user_id) FROM events FINAL WHERE `+where+
		` AND type = 'dislike' AND (props['scope'] = 'artist' OR props['reason'] = 'dislike_artist')`, app, from, key).Scan(&dis); err != nil {
		return nil, err
	}
	out.Dislikes = int64(dis)
	return out, nil
}
