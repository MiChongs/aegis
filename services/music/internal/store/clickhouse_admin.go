package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"aegis/services/music/contract"
)

// 管理端（控制台）查询。返回结构的 JSON 与 docs「管理端返回结构」一致。

// Totals 总量。
type Totals struct {
	Events           int64   `json:"events"`
	Listeners        int64   `json:"listeners"`
	Plays            int64   `json:"plays"`
	ListeningMinutes int64   `json:"listeningMinutes"`
	CompletionRate   float64 `json:"completionRate"`
	SkipRate         float64 `json:"skipRate"`
	Likes            int64   `json:"likes"`
	Searches         int64   `json:"searches"`
	Tracks           int64   `json:"tracks"`
}

// PrevTotals 上一周期。
type PrevTotals struct {
	Events           int64   `json:"events"`
	Listeners        int64   `json:"listeners"`
	Plays            int64   `json:"plays"`
	ListeningMinutes int64   `json:"listeningMinutes"`
	CompletionRate   float64 `json:"completionRate"`
	SkipRate         float64 `json:"skipRate"`
}

// DayRow 每日。
type DayRow struct {
	Day              string  `json:"day"`
	Events           int64   `json:"events"`
	Listeners        int64   `json:"listeners"`
	Plays            int64   `json:"plays"`
	ListeningMinutes int64   `json:"listeningMinutes"`
	CompletionRate   float64 `json:"completionRate"`
	SkipRate         float64 `json:"skipRate"`
}

// TypeCount 事件类型分布。
type TypeCount struct {
	Type  string `json:"type"`
	Count int64  `json:"count"`
}

// SceneRow 推荐场景漏斗。
type SceneRow struct {
	Scene       string  `json:"scene"`
	Requests    int64   `json:"requests"`
	Impressions int64   `json:"impressions"`
	Clicks      int64   `json:"clicks"`
	Plays       int64   `json:"plays"`
	CTR         float64 `json:"ctr"`
	PlayRate    float64 `json:"playRate"`
}

// TopTrack 热门作品。
type TopTrack struct {
	WorkKey        string         `json:"workKey"`
	Track          contract.Track `json:"track"`
	Plays          int64          `json:"plays"`
	Listeners      int64          `json:"listeners"`
	CompletionRate float64        `json:"completionRate"`
}

// TopArtist 热门歌手。
type TopArtist struct {
	Name      string `json:"name"`
	Plays     int64  `json:"plays"`
	Listeners int64  `json:"listeners"`
}

// SourceCount 音源分布。
type SourceCount struct {
	Source string `json:"source"`
	Plays  int64  `json:"plays"`
}

// PlatformCount 平台分布。
type PlatformCount struct {
	Platform  string `json:"platform"`
	Listeners int64  `json:"listeners"`
}

// Overview 总览的 ClickHouse 部分（模型与积压由调用方补）。
type Overview struct {
	Days       int             `json:"days"`
	From       int64           `json:"from"`
	To         int64           `json:"to"`
	Totals     Totals          `json:"totals"`
	Previous   PrevTotals      `json:"previous"`
	Daily      []DayRow        `json:"daily"`
	EventTypes []TypeCount     `json:"eventTypes"`
	Hours      []int64         `json:"hours"`
	Scenes     []SceneRow      `json:"scenes"`
	TopTracks  []TopTrack      `json:"topTracks"`
	TopArtists []TopArtist     `json:"topArtists"`
	Sources    []SourceCount   `json:"sources"`
	Platforms  []PlatformCount `json:"platforms"`
	LastEvent  int64           `json:"-"`
}

func rate(a, b uint64) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

// Overview 总览。
func (c *CH) Overview(ctx context.Context, appID int64, days int) (*Overview, error) {
	now := time.Now()
	from := now.Add(-time.Duration(days) * 24 * time.Hour)
	prevFrom := from.Add(-time.Duration(days) * 24 * time.Hour)
	app := uint64(appID)
	out := &Overview{Days: days, From: from.UnixMilli(), To: now.UnixMilli(), Hours: make([]int64, 24),
		Daily: []DayRow{}, EventTypes: []TypeCount{}, Scenes: []SceneRow{}, TopTracks: []TopTrack{}, TopArtists: []TopArtist{},
		Sources: []SourceCount{}, Platforms: []PlatformCount{}}

	var ev, listeners, plays, playedMs, completes, ends, skips, likes, unlikes, searches, tracks uint64
	var last time.Time
	if err := c.conn.QueryRow(ctx, `
SELECT count(), uniqExact(user_id), countIf(`+sqlIsPlay+`), sumIf(played_ms, type = 'play_end'),
       countIf(`+sqlIsComplete+`), countIf(type = 'play_end'), countIf(`+sqlIsSkip+`),
       countIf(type = 'like'), countIf(type = 'unlike'), countIf(type = 'search'), uniqExactIf(work_key, type = 'play_end'), max(ts)
FROM events FINAL WHERE app_id = ? AND ts >= ?`, app, from.UTC()).Scan(
		&ev, &listeners, &plays, &playedMs, &completes, &ends, &skips, &likes, &unlikes, &searches, &tracks, &last); err != nil {
		return nil, fmt.Errorf("总量：%w", err)
	}
	netLikes := int64(likes) - int64(unlikes)
	if netLikes < 0 {
		netLikes = 0
	}
	out.Totals = Totals{Events: int64(ev), Listeners: int64(listeners), Plays: int64(plays), ListeningMinutes: int64(playedMs / 60000),
		CompletionRate: rate(completes, ends), SkipRate: rate(skips, ends), Likes: netLikes, Searches: int64(searches), Tracks: int64(tracks)}
	if !last.IsZero() && last.Year() > 2000 {
		out.LastEvent = last.UnixMilli()
	}

	var pev, plis, pplays, pms, pcomp, pends, pskips uint64
	if err := c.conn.QueryRow(ctx, `
SELECT count(), uniqExact(user_id), countIf(`+sqlIsPlay+`), sumIf(played_ms, type = 'play_end'),
       countIf(`+sqlIsComplete+`), countIf(type = 'play_end'), countIf(`+sqlIsSkip+`)
FROM events FINAL WHERE app_id = ? AND ts >= ? AND ts < ?`, app, prevFrom.UTC(), from.UTC()).Scan(&pev, &plis, &pplays, &pms, &pcomp, &pends, &pskips); err != nil {
		return nil, fmt.Errorf("上一周期：%w", err)
	}
	out.Previous = PrevTotals{Events: int64(pev), Listeners: int64(plis), Plays: int64(pplays), ListeningMinutes: int64(pms / 60000),
		CompletionRate: rate(pcomp, pends), SkipRate: rate(pskips, pends)}

	rows, err := c.conn.Query(ctx, `
SELECT toString(toDate(ts, ?)) AS d, count(), uniqExact(user_id), countIf(`+sqlIsPlay+`), sumIf(played_ms, type = 'play_end'),
       countIf(`+sqlIsComplete+`), countIf(type = 'play_end'), countIf(`+sqlIsSkip+`)
FROM events FINAL WHERE app_id = ? AND ts >= ? GROUP BY d ORDER BY d`, c.TZ, app, from.UTC())
	if err != nil {
		return nil, fmt.Errorf("每日：%w", err)
	}
	byDay := map[string]DayRow{}
	for rows.Next() {
		var d string
		var e, l, p, pm, comp, en, sk uint64
		if err := rows.Scan(&d, &e, &l, &p, &pm, &comp, &en, &sk); err != nil {
			rows.Close()
			return nil, err
		}
		byDay[d] = DayRow{Day: d, Events: int64(e), Listeners: int64(l), Plays: int64(p), ListeningMinutes: int64(pm / 60000),
			CompletionRate: rate(comp, en), SkipRate: rate(sk, en)}
	}
	rows.Close()
	loc := loadLoc(c.TZ)
	for d := from.In(loc); !d.After(now.In(loc)); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		row, ok := byDay[key]
		if !ok {
			row = DayRow{Day: key}
		}
		out.Daily = append(out.Daily, row)
	}

	rows, err = c.conn.Query(ctx, `SELECT type, count() AS n FROM events FINAL WHERE app_id = ? AND ts >= ? GROUP BY type ORDER BY n DESC`, app, from.UTC())
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var t TypeCount
		var n uint64
		if err := rows.Scan(&t.Type, &n); err != nil {
			rows.Close()
			return nil, err
		}
		t.Count = int64(n)
		out.EventTypes = append(out.EventTypes, t)
	}
	rows.Close()

	rows, err = c.conn.Query(ctx, `SELECT toHour(ts, ?) AS h, count() FROM events FINAL WHERE app_id = ? AND ts >= ? AND `+sqlIsPlay+` GROUP BY h`, c.TZ, app, from.UTC())
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var h uint8
		var n uint64
		if err := rows.Scan(&h, &n); err != nil {
			rows.Close()
			return nil, err
		}
		if h < 24 {
			out.Hours[h] = int64(n)
		}
	}
	rows.Close()

	if out.Scenes, err = c.sceneFunnel(ctx, app, from); err != nil {
		return nil, fmt.Errorf("推荐场景：%w", err)
	}

	rows, err = c.conn.Query(ctx, `
SELECT work_key, argMax(source, ts), argMax(song_id, ts), argMax(title, ts), argMax(artist, ts), argMax(album, ts), argMax(cover, ts),
       max(duration_ms), countIf(`+sqlIsPlay+`) AS p, uniqExact(user_id), countIf(`+sqlIsComplete+`), count()
FROM events FINAL WHERE app_id = ? AND ts >= ? AND type = 'play_end' AND work_key != ''
GROUP BY work_key ORDER BY p DESC LIMIT 20`, app, from.UTC())
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var t TopTrack
		var dur uint32
		var p, l, comp, n uint64
		if err := rows.Scan(&t.WorkKey, &t.Track.Source, &t.Track.SongID, &t.Track.Title, &t.Track.Artist, &t.Track.Album, &t.Track.Cover,
			&dur, &p, &l, &comp, &n); err != nil {
			rows.Close()
			return nil, err
		}
		t.Track.DurationMs, t.Plays, t.Listeners, t.CompletionRate = int64(dur), int64(p), int64(l), rate(comp, n)
		out.TopTracks = append(out.TopTracks, t)
	}
	rows.Close()

	rows, err = c.conn.Query(ctx, `
SELECT trim(BOTH ' ' FROM splitByRegexp('[/、,，&;；|]', artist)[1]) AS a, countIf(`+sqlIsPlay+`) AS p, uniqExact(user_id)
FROM events FINAL WHERE app_id = ? AND ts >= ? AND type = 'play_end' AND artist != ''
GROUP BY a HAVING a != '' ORDER BY p DESC LIMIT 20`, app, from.UTC())
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var a TopArtist
		var p, l uint64
		if err := rows.Scan(&a.Name, &p, &l); err != nil {
			rows.Close()
			return nil, err
		}
		a.Plays, a.Listeners = int64(p), int64(l)
		out.TopArtists = append(out.TopArtists, a)
	}
	rows.Close()

	rows, err = c.conn.Query(ctx, `SELECT source, countIf(`+sqlIsPlay+`) AS p FROM events FINAL WHERE app_id = ? AND ts >= ? AND type = 'play_end' AND source != ''
GROUP BY source ORDER BY p DESC`, app, from.UTC())
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s SourceCount
		var p uint64
		if err := rows.Scan(&s.Source, &p); err != nil {
			rows.Close()
			return nil, err
		}
		s.Plays = int64(p)
		out.Sources = append(out.Sources, s)
	}
	rows.Close()

	rows, err = c.conn.Query(ctx, `SELECT if(platform = '', 'unknown', platform) AS pf, uniqExact(user_id) AS l FROM events FINAL WHERE app_id = ? AND ts >= ?
GROUP BY pf ORDER BY l DESC`, app, from.UTC())
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p PlatformCount
		var l uint64
		if err := rows.Scan(&p.Platform, &l); err != nil {
			rows.Close()
			return nil, err
		}
		p.Listeners = int64(l)
		out.Platforms = append(out.Platforms, p)
	}
	rows.Close()
	return out, nil
}

// sceneFunnel 各场景：请求（推荐服务发出）→ 曝光（客户端展示）→ 点击 → 播放，按 recId 去重。
func (c *CH) sceneFunnel(ctx context.Context, app uint64, from time.Time) ([]SceneRow, error) {
	var order []string
	idx := map[string]*SceneRow{}
	get := func(scene string) *SceneRow {
		if r, ok := idx[scene]; ok {
			return r
		}
		r := &SceneRow{Scene: scene}
		idx[scene] = r
		order = append(order, scene)
		return r
	}
	for _, s := range contract.Scenes {
		get(s)
	}
	rows, err := c.conn.Query(ctx, `SELECT scene, count() FROM impressions FINAL WHERE app_id = ? AND ts >= ? GROUP BY scene`, app, from.UTC())
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var scene string
		var n uint64
		if err := rows.Scan(&scene, &n); err != nil {
			rows.Close()
			return nil, err
		}
		get(scene).Requests = int64(n)
	}
	rows.Close()
	rows, err = c.conn.Query(ctx, `
SELECT scene,
       uniqExactIf(rec_id, type = 'impression'),
       uniqExactIf(rec_id, type IN ('click', 'search_click', 'play_start', 'like', 'playlist_add', 'download', 'share')),
       uniqExactIf(rec_id, type = 'play_start')
FROM events FINAL WHERE app_id = ? AND ts >= ? AND rec_id != '' AND scene != '' GROUP BY scene`, app, from.UTC())
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var scene string
		var imp, clicks, plays uint64
		if err := rows.Scan(&scene, &imp, &clicks, &plays); err != nil {
			rows.Close()
			return nil, err
		}
		r := get(scene)
		r.Impressions, r.Clicks, r.Plays = int64(imp), int64(clicks), int64(plays)
	}
	rows.Close()
	out := make([]SceneRow, 0, len(order))
	for _, scene := range order {
		row := *idx[scene]
		if row.Impressions > 0 {
			row.CTR = float64(row.Clicks) / float64(row.Impressions)
			row.PlayRate = float64(row.Plays) / float64(row.Impressions)
		}
		out = append(out, row)
	}
	return out, nil
}

func loadLoc(tz string) *time.Location {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.Local
	}
	return loc
}

// UserRow 听众排行一行。
type UserRow struct {
	UserID       int64   `json:"userId"`
	Minutes      int64   `json:"minutes"`
	Plays        int64   `json:"plays"`
	Tracks       int64   `json:"tracks"`
	Likes        int64   `json:"likes"`
	SkipRate     float64 `json:"skipRate"`
	LastActiveAt int64   `json:"lastActiveAt"`
}

// Users 听众排行（按听歌时长）；userID>0 时只查这个人。
func (c *CH) Users(ctx context.Context, appID int64, days int, userID int64, page, pageSize int) ([]UserRow, int64, error) {
	from := time.Now().Add(-time.Duration(days) * 24 * time.Hour).UTC()
	where := "app_id = ? AND ts >= ?"
	args := []any{uint64(appID), from}
	if userID > 0 {
		where += " AND user_id = ?"
		args = append(args, uint64(userID))
	}
	var total uint64
	if err := c.conn.QueryRow(ctx, "SELECT uniqExact(user_id) FROM events FINAL WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := c.conn.Query(ctx, `
SELECT user_id, sumIf(played_ms, type = 'play_end') AS ms, countIf(`+sqlIsPlay+`), uniqExactIf(work_key, type = 'play_end'),
       countIf(type = 'like'), countIf(`+sqlIsSkip+`), countIf(type = 'play_end'), max(ts)
FROM events FINAL WHERE `+where+` GROUP BY user_id ORDER BY ms DESC, user_id LIMIT ? OFFSET ?`,
		append(args, pageSize, (page-1)*pageSize)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []UserRow{}
	for rows.Next() {
		var uid, msum, plays, tracks, likes, skips, ends uint64
		var last time.Time
		if err := rows.Scan(&uid, &msum, &plays, &tracks, &likes, &skips, &ends, &last); err != nil {
			return nil, 0, err
		}
		out = append(out, UserRow{UserID: int64(uid), Minutes: int64(msum / 60000), Plays: int64(plays), Tracks: int64(tracks),
			Likes: int64(likes), SkipRate: rate(skips, ends), LastActiveAt: last.UnixMilli()})
	}
	return out, int64(total), rows.Err()
}

// EventView 管理端看到的事件。
type EventView struct {
	ID         string            `json:"id"`
	Type       string            `json:"type"`
	TS         int64             `json:"ts"`
	ReceivedAt int64             `json:"receivedAt"`
	UserID     int64             `json:"userId"`
	DeviceID   string            `json:"deviceId"`
	SessionID  string            `json:"sessionId"`
	WorkKey    string            `json:"workKey"`
	Track      *contract.Track   `json:"track"`
	PlayedMs   int64             `json:"playedMs"`
	PositionMs int64             `json:"positionMs"`
	Reason     string            `json:"reason"`
	Context    contract.Context  `json:"context"`
	Platform   string            `json:"platform"`
	AppVersion string            `json:"appVersion"`
	Network    string            `json:"network"`
	Props      map[string]string `json:"props"`
}

const eventColumns = `event_id, type, toUnixTimestamp64Milli(ts), toUnixTimestamp64Milli(received_at), user_id, device_id, session_id,
work_key, source, song_id, title, artist, album, cover, duration_ms, played_ms, position_ms, reason,
page, section, position, query, rec_id, scene, playlist_id, queue_source, platform, app_version, network, props`

func scanEvents(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
}) ([]EventView, error) {
	out := []EventView{}
	for rows.Next() {
		var e EventView
		var uid uint64
		var t contract.Track
		var dur, played, pos uint32
		if err := rows.Scan(&e.ID, &e.Type, &e.TS, &e.ReceivedAt, &uid, &e.DeviceID, &e.SessionID, &e.WorkKey,
			&t.Source, &t.SongID, &t.Title, &t.Artist, &t.Album, &t.Cover, &dur, &played, &pos, &e.Reason,
			&e.Context.Page, &e.Context.Section, &e.Context.Position, &e.Context.Query, &e.Context.RecID, &e.Context.Scene,
			&e.Context.PlaylistID, &e.Context.QueueSource, &e.Platform, &e.AppVersion, &e.Network, &e.Props); err != nil {
			return nil, err
		}
		e.UserID, e.PlayedMs, e.PositionMs = int64(uid), int64(played), int64(pos)
		if t.Source != "" {
			t.DurationMs = int64(dur)
			e.Track = &t
		}
		if e.Props == nil {
			e.Props = map[string]string{}
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// EventFilter 事件检索条件。
type EventFilter struct {
	Type   string
	UserID int64
	Q      string
	From   time.Time
	To     time.Time
}

// Events 事件检索（按时间倒序分页）。
func (c *CH) Events(ctx context.Context, appID int64, f EventFilter, page, pageSize int) ([]EventView, int64, error) {
	where := []string{"app_id = ?", "ts >= ?", "ts < ?"}
	args := []any{uint64(appID), f.From.UTC(), f.To.UTC()}
	if f.Type != "" {
		where = append(where, "type = ?")
		args = append(args, f.Type)
	}
	if f.UserID > 0 {
		where = append(where, "user_id = ?")
		args = append(args, uint64(f.UserID))
	}
	if q := strings.TrimSpace(f.Q); q != "" {
		where = append(where, "(positionCaseInsensitiveUTF8(title, ?) > 0 OR positionCaseInsensitiveUTF8(artist, ?) > 0 OR positionCaseInsensitiveUTF8(query, ?) > 0 OR work_key = ? OR rec_id = ? OR event_id = ?)")
		args = append(args, q, q, q, q, q, q)
	}
	cond := strings.Join(where, " AND ")
	var total uint64
	if err := c.conn.QueryRow(ctx, "SELECT count() FROM events FINAL WHERE "+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := c.conn.Query(ctx, "SELECT "+eventColumns+" FROM events FINAL WHERE "+cond+" ORDER BY ts DESC LIMIT ? OFFSET ?",
		append(args, pageSize, (page-1)*pageSize)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items, err := scanEvents(rows)
	return items, int64(total), err
}

// Timeline 一个人的事件（before 毫秒游标，倒序）。
func (c *CH) Timeline(ctx context.Context, appID, userID int64, typ string, before int64, limit int) ([]EventView, error) {
	where := "app_id = ? AND user_id = ? AND ts < ?"
	args := []any{uint64(appID), uint64(userID), ms(before)}
	if typ != "" {
		where += " AND type = ?"
		args = append(args, typ)
	}
	rows, err := c.conn.Query(ctx, "SELECT "+eventColumns+" FROM events FINAL WHERE "+where+" ORDER BY ts DESC LIMIT ?", append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

// TrackRowStat 作品排行一行。
type TrackRowStat struct {
	WorkKey        string         `json:"workKey"`
	Track          contract.Track `json:"track"`
	Plays          int64          `json:"plays"`
	Listeners      int64          `json:"listeners"`
	Minutes        int64          `json:"minutes"`
	CompletionRate float64        `json:"completionRate"`
	SkipRate       float64        `json:"skipRate"`
	Likes          int64          `json:"likes"`
}

// Tracks 作品排行。
func (c *CH) Tracks(ctx context.Context, appID int64, days int, q, sort string, page, pageSize int) ([]TrackRowStat, int64, error) {
	from := time.Now().Add(-time.Duration(days) * 24 * time.Hour).UTC()
	where := "app_id = ? AND ts >= ? AND work_key != ''"
	args := []any{uint64(appID), from}
	if q = strings.TrimSpace(q); q != "" {
		where += " AND (positionCaseInsensitiveUTF8(title, ?) > 0 OR positionCaseInsensitiveUTF8(artist, ?) > 0 OR work_key = ?)"
		args = append(args, q, q, q)
	}
	order := "plays DESC"
	switch sort {
	case "listeners":
		order = "listeners DESC"
	case "completion":
		order = "if(ends >= 5, completes / ends, 0) DESC"
	case "skip":
		order = "if(ends >= 5, skips / ends, 0) DESC"
	}
	var total uint64
	if err := c.conn.QueryRow(ctx, "SELECT uniqExact(work_key) FROM events FINAL WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := c.conn.Query(ctx, `
SELECT work_key, argMax(source, ts), argMax(song_id, ts), argMax(title, ts), argMax(artist, ts), argMax(album, ts), argMax(cover, ts),
       max(duration_ms), countIf(`+sqlIsPlay+`) AS plays, uniqExactIf(user_id, type = 'play_end') AS listeners,
       sumIf(played_ms, type = 'play_end'), countIf(`+sqlIsComplete+`) AS completes, countIf(type = 'play_end') AS ends,
       countIf(`+sqlIsSkip+`) AS skips, countIf(type = 'like')
FROM events FINAL WHERE `+where+` GROUP BY work_key ORDER BY `+order+`, work_key LIMIT ? OFFSET ?`,
		append(args, pageSize, (page-1)*pageSize)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []TrackRowStat{}
	for rows.Next() {
		var r TrackRowStat
		var dur uint32
		var plays, listeners, msum, comp, ends, skips, likes uint64
		if err := rows.Scan(&r.WorkKey, &r.Track.Source, &r.Track.SongID, &r.Track.Title, &r.Track.Artist, &r.Track.Album, &r.Track.Cover,
			&dur, &plays, &listeners, &msum, &comp, &ends, &skips, &likes); err != nil {
			return nil, 0, err
		}
		r.Track.DurationMs = int64(dur)
		r.Plays, r.Listeners, r.Minutes, r.Likes = int64(plays), int64(listeners), int64(msum/60000), int64(likes)
		r.CompletionRate, r.SkipRate = rate(comp, ends), rate(skips, ends)
		out = append(out, r)
	}
	return out, int64(total), rows.Err()
}

// TrackStats 作品统计。
type TrackStats struct {
	Plays          int64   `json:"plays"`
	Listeners      int64   `json:"listeners"`
	Minutes        int64   `json:"minutes"`
	CompletionRate float64 `json:"completionRate"`
	SkipRate       float64 `json:"skipRate"`
	Likes          int64   `json:"likes"`
	Dislikes       int64   `json:"dislikes"`
}

// TrackDay 作品每日。
type TrackDay struct {
	Day       string `json:"day"`
	Plays     int64  `json:"plays"`
	Listeners int64  `json:"listeners"`
}

// VariantPlays 每个音源版本的播放数。
type VariantPlays struct {
	Source string
	SongID string
	Plays  int64
}

// TrackDetail 作品统计、每日曲线、各音源版本播放数（近 days 天）。
func (c *CH) TrackDetail(ctx context.Context, appID int64, workKey string, days int) (*TrackStats, []TrackDay, []VariantPlays, *contract.Track, error) {
	from := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	app := uint64(appID)
	var plays, listeners, msum, comp, ends, skips, likes, dislikes uint64
	var t contract.Track
	var dur uint32
	if err := c.conn.QueryRow(ctx, `
SELECT countIf(`+sqlIsPlay+`), uniqExactIf(user_id, type = 'play_end'), sumIf(played_ms, type = 'play_end'),
       countIf(`+sqlIsComplete+`), countIf(type = 'play_end'), countIf(`+sqlIsSkip+`), countIf(type = 'like'), countIf(type = 'dislike'),
       argMax(source, ts), argMax(song_id, ts), argMax(title, ts), argMax(artist, ts), argMax(album, ts), argMax(cover, ts), max(duration_ms)
FROM events FINAL WHERE app_id = ? AND work_key = ? AND ts >= ?`, app, workKey, from.UTC()).Scan(
		&plays, &listeners, &msum, &comp, &ends, &skips, &likes, &dislikes, &t.Source, &t.SongID, &t.Title, &t.Artist, &t.Album, &t.Cover, &dur); err != nil {
		return nil, nil, nil, nil, err
	}
	t.DurationMs = int64(dur)
	stats := &TrackStats{Plays: int64(plays), Listeners: int64(listeners), Minutes: int64(msum / 60000),
		CompletionRate: rate(comp, ends), SkipRate: rate(skips, ends), Likes: int64(likes), Dislikes: int64(dislikes)}
	rows, err := c.conn.Query(ctx, `
SELECT toString(toDate(ts, ?)) AS d, countIf(`+sqlIsPlay+`), uniqExactIf(user_id, type = 'play_end')
FROM events FINAL WHERE app_id = ? AND work_key = ? AND ts >= ? GROUP BY d ORDER BY d`, c.TZ, app, workKey, from.UTC())
	if err != nil {
		return nil, nil, nil, nil, err
	}
	byDay := map[string]TrackDay{}
	for rows.Next() {
		var d TrackDay
		var p, l uint64
		if err := rows.Scan(&d.Day, &p, &l); err != nil {
			rows.Close()
			return nil, nil, nil, nil, err
		}
		d.Plays, d.Listeners = int64(p), int64(l)
		byDay[d.Day] = d
	}
	rows.Close()
	daily := []TrackDay{}
	loc := loadLoc(c.TZ)
	for d := from.In(loc); !d.After(time.Now().In(loc)); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		row, ok := byDay[key]
		if !ok {
			row = TrackDay{Day: key}
		}
		daily = append(daily, row)
	}
	rows, err = c.conn.Query(ctx, `SELECT source, song_id, countIf(`+sqlIsPlay+`) FROM events FINAL
WHERE app_id = ? AND work_key = ? AND ts >= ? AND type = 'play_end' GROUP BY source, song_id`, app, workKey, from.UTC())
	if err != nil {
		return nil, nil, nil, nil, err
	}
	defer rows.Close()
	var variants []VariantPlays
	for rows.Next() {
		var v VariantPlays
		var p uint64
		if err := rows.Scan(&v.Source, &v.SongID, &p); err != nil {
			return nil, nil, nil, nil, err
		}
		v.Plays = int64(p)
		variants = append(variants, v)
	}
	var tp *contract.Track
	if t.Source != "" {
		tp = &t
	}
	return stats, daily, variants, tp, rows.Err()
}
