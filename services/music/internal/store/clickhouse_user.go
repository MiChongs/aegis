package store

import (
	"context"
	"strings"
	"time"

	"aegis/services/music/contract"
)

// ── 第二期：个人侧查询（每周雷达、风格歌单、新用户、导出、报告、热搜） ──

// UserWorkSet 一个人 days 天内交互过的全部作品（每周雷达只出没听过的）。
func (c *CH) UserWorkSet(ctx context.Context, appID, userID int64, days int) ([]string, error) {
	rows, err := c.conn.Query(ctx, `
SELECT DISTINCT work_key FROM events
WHERE app_id = ? AND user_id = ? AND ts >= now64(3) - toIntervalDay(?) AND work_key != ''
LIMIT 50000`, uint64(appID), uint64(userID), days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// UserTopWorks 一个人 days 天内按偏好加权的作品（听完 1、部分 0.4、收藏 3、加歌单 2、早跳 −0.6、不感兴趣 −6），只要正分的。
func (c *CH) UserTopWorks(ctx context.Context, appID, userID int64, days, limit int) ([]Scored, error) {
	rows, err := c.conn.Query(ctx, `
SELECT work_key,
  countIf(`+sqlIsComplete+`) + 0.4 * countIf(type = 'play_end' AND played_ms >= 30000 AND NOT `+sqlIsComplete+`)
  + 3 * (countIf(type = 'like') - countIf(type = 'unlike')) + 2 * countIf(type = 'playlist_add')
  - 0.6 * countIf(`+sqlIsSkip+`) - 6 * countIf(type = 'dislike' AND props['reason'] NOT IN ('heard_too_much','poor_quality','wrong_song')) AS score
FROM events FINAL
WHERE app_id = ? AND user_id = ? AND ts >= now64(3) - toIntervalDay(?) AND work_key != ''
GROUP BY work_key HAVING score > 0 ORDER BY score DESC LIMIT ?`, uint64(appID), uint64(userID), days, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Scored
	for rows.Next() {
		var s Scored
		if err := rows.Scan(&s.WorkKey, &s.Score); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// UserPlayCount 一个人有效播放次数（新用户判定）。
func (c *CH) UserPlayCount(ctx context.Context, appID, userID int64) (int64, error) {
	var n uint64
	err := c.conn.QueryRow(ctx, `SELECT count() FROM events WHERE app_id = ? AND user_id = ? AND `+sqlIsPlay,
		uint64(appID), uint64(userID)).Scan(&n)
	return int64(n), err
}

// ArtistTrackRow 某位歌手的一个作品。
type ArtistTrackRow struct {
	WorkKey   string
	Track     contract.Track
	Listeners int64
	Plays     int64
}

// ArtistTracks 近 days 天某位歌手（按第一歌手规范化后比较）听众最多的作品。
func (c *CH) ArtistTracks(ctx context.Context, appID int64, artist string, days, limit int) ([]ArtistTrackRow, error) {
	target := contract.NormalizeArtist(contract.FirstArtist(artist))
	if target == "" {
		return nil, nil
	}
	rows, err := c.conn.Query(ctx, `
SELECT work_key, argMax(source, ts), argMax(song_id, ts), argMax(title, ts), argMax(artist, ts), argMax(album, ts),
       argMax(cover, ts), max(duration_ms), uniqExact(user_id), countIf(`+sqlIsPlay+`)
FROM events
WHERE app_id = ? AND ts >= now64(3) - toIntervalDay(?) AND type = 'play_end' AND work_key != ''
  AND positionCaseInsensitiveUTF8(artist, ?) > 0
GROUP BY work_key ORDER BY uniqExact(user_id) DESC LIMIT ?`, uint64(appID), days, contract.FirstArtist(artist), limit*3)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArtistTrackRow
	for rows.Next() {
		var r ArtistTrackRow
		var dur uint32
		var l, p uint64
		if err := rows.Scan(&r.WorkKey, &r.Track.Source, &r.Track.SongID, &r.Track.Title, &r.Track.Artist, &r.Track.Album,
			&r.Track.Cover, &dur, &l, &p); err != nil {
			return nil, err
		}
		if contract.NormalizeArtist(contract.FirstArtist(r.Track.Artist)) != target {
			continue
		}
		r.Track.DurationMs, r.Listeners, r.Plays = int64(dur), int64(l), int64(p)
		out = append(out, r)
		if len(out) >= limit {
			break
		}
	}
	return out, rows.Err()
}

// ArtistStat 一位歌手的听众统计（新用户偏好、冷启动推荐歌手）。
type ArtistStat struct {
	Name      string
	Norm      string
	Listeners int64
	Cover     string
	Title     string
}

// TopArtists 近 days 天听众最多的歌手（按第一歌手合并）。
func (c *CH) TopArtists(ctx context.Context, appID int64, days, limit int) ([]ArtistStat, error) {
	rows, err := c.conn.Query(ctx, `
SELECT artist, uniqExact(user_id) AS listeners, argMax(cover, ts), argMax(title, ts)
FROM events
WHERE app_id = ? AND ts >= now64(3) - toIntervalDay(?) AND type = 'play_end' AND artist != ''
GROUP BY artist ORDER BY listeners DESC LIMIT ?`, uint64(appID), days, limit*4)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	by := map[string]*ArtistStat{}
	var order []string
	for rows.Next() {
		var raw, cover, title string
		var l uint64
		if err := rows.Scan(&raw, &l, &cover, &title); err != nil {
			return nil, err
		}
		name := contract.FirstArtist(raw)
		norm := contract.NormalizeArtist(name)
		if norm == "" {
			continue
		}
		a := by[norm]
		if a == nil {
			a = &ArtistStat{Name: name, Norm: norm, Cover: cover, Title: title}
			by[norm] = a
			order = append(order, norm)
		}
		a.Listeners += int64(l)
		if a.Cover == "" {
			a.Cover = cover
		}
	}
	out := make([]ArtistStat, 0, len(order))
	for _, n := range order {
		out = append(out, *by[n])
	}
	sortArtistStats(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, rows.Err()
}

func sortArtistStats(a []ArtistStat) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && (a[j].Listeners > a[j-1].Listeners || a[j].Listeners == a[j-1].Listeners && a[j].Norm < a[j-1].Norm); j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

// ExportRow 导出的一条事件（不含 IP、设备）。
type ExportRow struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	TS         int64             `json:"ts"`
	Track      *contract.Track   `json:"track"`
	PlayedMs   int64             `json:"playedMs"`
	PositionMs int64             `json:"positionMs"`
	Reason     string            `json:"reason"`
	Context    contract.Context  `json:"context"`
	Props      map[string]string `json:"props"`
	Platform   string            `json:"platform"`
}

// Export 一个人的事件，按时间倒序，before 为毫秒游标（0 = 从最新开始）。
func (c *CH) Export(ctx context.Context, appID, userID, before int64, limit int) ([]ExportRow, error) {
	if before <= 0 {
		before = time.Now().Add(time.Hour).UnixMilli()
	}
	rows, err := c.conn.Query(ctx, `
SELECT event_id, type, toUnixTimestamp64Milli(ts), source, song_id, title, artist, album, cover, duration_ms, played_ms, position_ms, reason,
       page, section, position, query, rec_id, scene, playlist_id, queue_source, props, platform
FROM events FINAL
WHERE app_id = ? AND user_id = ? AND ts < ?
ORDER BY ts DESC LIMIT ?`, uint64(appID), uint64(userID), time.UnixMilli(before).UTC(), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExportRow{}
	for rows.Next() {
		var r ExportRow
		var t contract.Track
		var dur, played, pos uint32
		if err := rows.Scan(&r.ID, &r.Type, &r.TS, &t.Source, &t.SongID, &t.Title, &t.Artist, &t.Album, &t.Cover, &dur, &played, &pos,
			&r.Reason, &r.Context.Page, &r.Context.Section, &r.Context.Position, &r.Context.Query, &r.Context.RecID, &r.Context.Scene,
			&r.Context.PlaylistID, &r.Context.QueueSource, &r.Props, &r.Platform); err != nil {
			return nil, err
		}
		if t.Source != "" {
			t.DurationMs = int64(dur)
			r.Track = &t
		}
		r.PlayedMs, r.PositionMs = int64(played), int64(pos)
		if r.Props == nil {
			r.Props = map[string]string{}
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// TrendingQuery 一个热搜词。
type TrendingQuery struct {
	Query  string `json:"query"`
	Users  int64  `json:"users"`
	Rising bool   `json:"rising"`
}

// TrendingSearches 近 24 小时搜索人数最多的词（至少 minUsers 个不同用户；rising = 比前 24 小时翻倍）。
func (c *CH) TrendingSearches(ctx context.Context, appID int64, minUsers, limit int) ([]TrendingQuery, error) {
	rows, err := c.conn.Query(ctx, `
SELECT q, uniqExactIf(user_id, ts >= now64(3) - toIntervalHour(24)) AS cur,
          uniqExactIf(user_id, ts < now64(3) - toIntervalHour(24)) AS prev
FROM (SELECT lowerUTF8(trimBoth(query)) AS q, user_id, ts FROM events
      WHERE app_id = ? AND type = 'search' AND ts >= now64(3) - toIntervalHour(48) AND query != '')
GROUP BY q HAVING cur >= ? ORDER BY cur DESC, q LIMIT ?`, uint64(appID), minUsers, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TrendingQuery{}
	for rows.Next() {
		var q string
		var cur, prev uint64
		if err := rows.Scan(&q, &cur, &prev); err != nil {
			return nil, err
		}
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		out = append(out, TrendingQuery{Query: q, Users: int64(cur), Rising: cur >= 2*max(prev, 1) && prev < cur})
	}
	return out, rows.Err()
}

// UserFirstSeen 一个人每个作品、每位歌手（第一歌手规范化名）第一次有效播放的时刻（听歌报告的「新歌」「新认识的歌手」）。
func (c *CH) UserFirstSeen(ctx context.Context, appID, userID int64) (works, artists map[string]int64, err error) {
	rows, err := c.conn.Query(ctx, `
SELECT work_key, artist, toUnixTimestamp64Milli(min(ts))
FROM events WHERE app_id = ? AND user_id = ? AND `+sqlIsPlay+` AND work_key != ''
GROUP BY work_key, artist`, uint64(appID), uint64(userID))
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	works, artists = map[string]int64{}, map[string]int64{}
	for rows.Next() {
		var key, artist string
		var at int64
		if err := rows.Scan(&key, &artist, &at); err != nil {
			return nil, nil, err
		}
		if v, ok := works[key]; !ok || at < v {
			works[key] = at
		}
		if a := contract.NormalizeArtist(contract.FirstArtist(artist)); a != "" {
			if v, ok := artists[a]; !ok || at < v {
				artists[a] = at
			}
		}
	}
	return works, artists, rows.Err()
}
