package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"aegis/services/music/contract"
)

// 第三期：内容理解（大模型标签与语义向量）、歌手知识、大模型用量、深度模型训练记录（docs/music-personalization-3.md）。
// music_deep_* 两张表 music-deep（Python）启动时也会建，DDL 两边必须一致。
const pgSchema3 = `
CREATE TABLE IF NOT EXISTS music_track_tags (
  app_id     BIGINT      NOT NULL,
  work_key   TEXT        NOT NULL,
  genres     TEXT[]      NOT NULL DEFAULT '{}',
  moods      TEXT[]      NOT NULL DEFAULT '{}',
  scenes     TEXT[]      NOT NULL DEFAULT '{}',
  era        TEXT        NOT NULL DEFAULT '',
  energy     REAL        NOT NULL DEFAULT 0,
  vocal      TEXT        NOT NULL DEFAULT '',
  flags      TEXT[]      NOT NULL DEFAULT '{}',
  confidence REAL        NOT NULL DEFAULT 0,
  embedding  REAL[],
  model      TEXT        NOT NULL DEFAULT '',
  tagged_at  TIMESTAMPTZ,
  manual     BOOLEAN     NOT NULL DEFAULT false,
  attempts   INT         NOT NULL DEFAULT 0,
  failed_at  TIMESTAMPTZ,
  last_error TEXT        NOT NULL DEFAULT '',
  PRIMARY KEY (app_id, work_key)
);
CREATE TABLE IF NOT EXISTS music_artist_tags (
  app_id    BIGINT      NOT NULL,
  artist    TEXT        NOT NULL,
  name      TEXT        NOT NULL,
  similar_artists TEXT[] NOT NULL DEFAULT '{}',
  genres    TEXT[]      NOT NULL DEFAULT '{}',
  lang      TEXT        NOT NULL DEFAULT '',
  tagged_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (app_id, artist)
);
CREATE TABLE IF NOT EXISTS music_llm_usage (
  app_id BIGINT NOT NULL,
  day    DATE   NOT NULL,
  kind   TEXT   NOT NULL,
  calls  INT    NOT NULL DEFAULT 0,
  tokens BIGINT NOT NULL DEFAULT 0,
  items  INT    NOT NULL DEFAULT 0,
  PRIMARY KEY (app_id, day, kind)
);
CREATE TABLE IF NOT EXISTS music_deep_runs (
  id BIGSERIAL PRIMARY KEY, app_id BIGINT NOT NULL, status TEXT NOT NULL, trigger TEXT NOT NULL, note TEXT NOT NULL DEFAULT '',
  operator TEXT NOT NULL DEFAULT '', params JSONB, metrics JSONB, ranker JSONB, parity JSONB, stages JSONB NOT NULL DEFAULT '[]',
  log JSONB NOT NULL DEFAULT '[]', users INT NOT NULL DEFAULT 0, items INT NOT NULL DEFAULT 0, sequences INT NOT NULL DEFAULT 0,
  error TEXT NOT NULL DEFAULT '', created_at TIMESTAMPTZ NOT NULL DEFAULT now(), started_at TIMESTAMPTZ, finished_at TIMESTAMPTZ,
  published_at TIMESTAMPTZ, heartbeat_at TIMESTAMPTZ);
CREATE INDEX IF NOT EXISTS music_deep_runs_app ON music_deep_runs (app_id, id DESC);
CREATE UNIQUE INDEX IF NOT EXISTS music_deep_runs_active ON music_deep_runs (app_id) WHERE status IN ('queued','running');
CREATE TABLE IF NOT EXISTS music_deep_blobs (run_id BIGINT NOT NULL REFERENCES music_deep_runs (id) ON DELETE CASCADE,
  kind TEXT NOT NULL, data BYTEA NOT NULL, PRIMARY KEY (run_id, kind));
`

// ── 内容标签 ──

// TrackToTag 等待打标签的作品。
type TrackToTag struct {
	WorkKey string
	Track   contract.Track
}

// TagApps 曲库里有作品的应用。
func (p *PG) TagApps(ctx context.Context) ([]int64, error) {
	rows, err := p.Pool.Query(ctx, `SELECT DISTINCT app_id FROM music_tracks`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// PendingTracks 还没打标签的作品（新的在前）；失败过的 1 小时后重试，累计失败 3 次后当天不再试。
func (p *PG) PendingTracks(ctx context.Context, appID int64, limit int) ([]TrackToTag, error) {
	rows, err := p.Pool.Query(ctx, `
SELECT t.work_key, t.source, t.song_id, t.title, t.artist, t.album, t.cover, t.duration_ms
FROM music_tracks t LEFT JOIN music_track_tags g ON g.app_id = t.app_id AND g.work_key = t.work_key
WHERE t.app_id = $1 AND (g.work_key IS NULL OR (g.tagged_at IS NULL AND (g.failed_at IS NULL OR g.failed_at < now() - interval '1 hour')
  AND (g.attempts < 3 OR g.failed_at < now() - interval '1 day')))
ORDER BY t.last_seen DESC LIMIT $2`, appID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrackToTag
	for rows.Next() {
		var r TrackToTag
		t := &r.Track
		if err := rows.Scan(&r.WorkKey, &t.Source, &t.SongID, &t.Title, &t.Artist, &t.Album, &t.Cover, &t.DurationMs); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// TagRow 一首歌的标签与向量。
type TagRow struct {
	WorkKey   string
	Tags      contract.Tags
	Embedding []float32
	Model     string
}

// SaveTags 写标签（人工改过的不覆盖）。
func (p *PG) SaveTags(ctx context.Context, appID int64, rows []TagRow) error {
	batch := &pgx.Batch{}
	for _, r := range rows {
		t := r.Tags
		batch.Queue(`
INSERT INTO music_track_tags (app_id, work_key, genres, moods, scenes, era, energy, vocal, flags, confidence, embedding, model, tagged_at, attempts, failed_at, last_error)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12, now(), 0, NULL, '')
ON CONFLICT (app_id, work_key) DO UPDATE SET genres = EXCLUDED.genres, moods = EXCLUDED.moods, scenes = EXCLUDED.scenes,
  era = EXCLUDED.era, energy = EXCLUDED.energy, vocal = EXCLUDED.vocal, flags = EXCLUDED.flags, confidence = EXCLUDED.confidence,
  embedding = EXCLUDED.embedding, model = EXCLUDED.model, tagged_at = now(), attempts = 0, failed_at = NULL, last_error = ''
WHERE NOT music_track_tags.manual`,
			appID, r.WorkKey, t.Genres, t.Moods, t.Scenes, t.Era, t.Energy, t.Vocal, t.Flags, t.Confidence, r.Embedding, r.Model)
	}
	return p.Pool.SendBatch(ctx, batch).Close()
}

// MarkTagFailed 记一次打标签失败。
func (p *PG) MarkTagFailed(ctx context.Context, appID int64, keys []string, msg string) error {
	if len(msg) > 500 {
		msg = msg[:500]
	}
	_, err := p.Pool.Exec(ctx, `
INSERT INTO music_track_tags (app_id, work_key, attempts, failed_at, last_error)
SELECT $1, k, 1, now(), $3 FROM unnest($2::text[]) AS k
ON CONFLICT (app_id, work_key) DO UPDATE SET attempts = music_track_tags.attempts + 1, failed_at = now(), last_error = EXCLUDED.last_error
WHERE music_track_tags.tagged_at IS NULL`, appID, keys, msg)
	return err
}

// SetManualTags 人工修改标签（向量由调用方重算后一并写入）。
func (p *PG) SetManualTags(ctx context.Context, appID int64, workKey string, t contract.Tags, emb []float32) error {
	_, err := p.Pool.Exec(ctx, `
INSERT INTO music_track_tags (app_id, work_key, genres, moods, scenes, era, energy, vocal, flags, confidence, embedding, model, tagged_at, manual)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,1,$10,'manual', now(), true)
ON CONFLICT (app_id, work_key) DO UPDATE SET genres = EXCLUDED.genres, moods = EXCLUDED.moods, scenes = EXCLUDED.scenes,
  era = EXCLUDED.era, energy = EXCLUDED.energy, vocal = EXCLUDED.vocal, flags = EXCLUDED.flags, confidence = 1,
  embedding = COALESCE(EXCLUDED.embedding, music_track_tags.embedding), model = 'manual', tagged_at = now(), manual = true,
  attempts = 0, failed_at = NULL, last_error = ''`,
		appID, workKey, t.Genres, t.Moods, t.Scenes, t.Era, t.Energy, t.Vocal, t.Flags, emb)
	return err
}

// Retag 清掉标签让 enricher 重打（人工改过的也清）；keys 为空且 all=true 时整个应用。
func (p *PG) Retag(ctx context.Context, appID int64, keys []string, all bool) (int64, error) {
	var tag interface{ RowsAffected() int64 }
	var err error
	if all {
		tag, err = p.Pool.Exec(ctx, `DELETE FROM music_track_tags WHERE app_id = $1`, appID)
	} else {
		tag, err = p.Pool.Exec(ctx, `DELETE FROM music_track_tags WHERE app_id = $1 AND work_key = ANY($2)`, appID, keys)
	}
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// TagInfo 控制台看到的标签。
type TagInfo struct {
	contract.Tags
	Model    string `json:"model"`
	TaggedAt int64  `json:"taggedAt"`
	Manual   bool   `json:"manual"`
}

// ItemContent 推荐时用的内容特征。
type ItemContent struct {
	Keys      []string
	Embedding []float32
	Tags      contract.Tags
}

// AllContent 一个应用全部已打标签作品的标签键与向量（music-rec 内存缓存用）。
func (p *PG) AllContent(ctx context.Context, appID int64) (map[string]*ItemContent, error) {
	rows, err := p.Pool.Query(ctx, `
SELECT work_key, genres, moods, scenes, era, energy, vocal, flags, confidence, embedding
FROM music_track_tags WHERE app_id = $1 AND tagged_at IS NOT NULL`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]*ItemContent{}
	for rows.Next() {
		var key string
		var t contract.Tags
		var emb []float32
		if err := rows.Scan(&key, &t.Genres, &t.Moods, &t.Scenes, &t.Era, &t.Energy, &t.Vocal, &t.Flags, &t.Confidence, &emb); err != nil {
			return nil, err
		}
		out[key] = &ItemContent{Keys: t.Keys(), Embedding: emb, Tags: t}
	}
	return out, rows.Err()
}

// TrackTags 一批作品的标签（控制台）。
func (p *PG) TrackTags(ctx context.Context, appID int64, keys []string) (map[string]TagInfo, error) {
	rows, err := p.Pool.Query(ctx, `
SELECT work_key, genres, moods, scenes, era, energy, vocal, flags, confidence, model, tagged_at, manual
FROM music_track_tags WHERE app_id = $1 AND work_key = ANY($2) AND tagged_at IS NOT NULL`, appID, keys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]TagInfo{}
	for rows.Next() {
		var key string
		var ti TagInfo
		var at time.Time
		t := &ti.Tags
		if err := rows.Scan(&key, &t.Genres, &t.Moods, &t.Scenes, &t.Era, &t.Energy, &t.Vocal, &t.Flags, &t.Confidence, &ti.Model, &at, &ti.Manual); err != nil {
			return nil, err
		}
		ti.TaggedAt = at.UnixMilli()
		out[key] = ti
	}
	return out, rows.Err()
}

// ContentRow 控制台作品标签列表的一行。
type ContentRow struct {
	WorkKey  string          `json:"workKey"`
	Track    *contract.Track `json:"track"`
	Tags     *TagInfo        `json:"tags"`
	Status   string          `json:"status"` // tagged | pending | failed
	Attempts int             `json:"attempts"`
	Error    string          `json:"error,omitempty"`
}

// ContentTracks 作品标签列表。status：tagged / pending / failed / 空（全部）；q 按歌名或歌手。
func (p *PG) ContentTracks(ctx context.Context, appID int64, status, q string, page, pageSize int) ([]ContentRow, int64, error) {
	where := `t.app_id = $1 AND ($2 = '' OR t.title ILIKE '%' || $2 || '%' OR t.artist ILIKE '%' || $2 || '%')`
	switch status {
	case "tagged":
		where += ` AND g.tagged_at IS NOT NULL`
	case "pending":
		where += ` AND g.tagged_at IS NULL AND (g.attempts IS NULL OR g.attempts < 3)`
	case "failed":
		where += ` AND g.tagged_at IS NULL AND g.attempts >= 3`
	}
	from := ` FROM music_tracks t LEFT JOIN music_track_tags g ON g.app_id = t.app_id AND g.work_key = t.work_key WHERE ` + where
	var total int64
	if err := p.Pool.QueryRow(ctx, `SELECT count(*)`+from, appID, q).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := p.Pool.Query(ctx, `
SELECT t.work_key, t.source, t.song_id, t.title, t.artist, t.album, t.cover, t.duration_ms,
  g.genres, g.moods, g.scenes, g.era, g.energy, g.vocal, g.flags, g.confidence, g.model, g.tagged_at, g.manual,
  COALESCE(g.attempts, 0), COALESCE(g.last_error, '')`+from+` ORDER BY t.last_seen DESC LIMIT $3 OFFSET $4`,
		appID, q, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []ContentRow{}
	for rows.Next() {
		var r ContentRow
		var tr contract.Track
		var genres, moods, scenes, flags []string
		var era, vocal, model *string
		var energy, conf *float32
		var at *time.Time
		var manual *bool
		if err := rows.Scan(&r.WorkKey, &tr.Source, &tr.SongID, &tr.Title, &tr.Artist, &tr.Album, &tr.Cover, &tr.DurationMs,
			&genres, &moods, &scenes, &era, &energy, &vocal, &flags, &conf, &model, &at, &manual, &r.Attempts, &r.Error); err != nil {
			return nil, 0, err
		}
		r.Track = &tr
		switch {
		case at != nil:
			r.Status = "tagged"
			ti := &TagInfo{Tags: contract.Tags{Genres: genres, Moods: moods, Scenes: scenes, Era: deref(era), Energy: float64(derefF(energy)),
				Vocal: deref(vocal), Flags: flags, Confidence: float64(derefF(conf))}, Model: deref(model), TaggedAt: at.UnixMilli(), Manual: manual != nil && *manual}
			ti.Tags.Clean()
			r.Tags = ti
		case r.Attempts >= 3:
			r.Status = "failed"
		default:
			r.Status = "pending"
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefF(f *float32) float32 {
	if f == nil {
		return 0
	}
	return *f
}

// ContentStats 内容理解总览的计数与分布。
type ContentStats struct {
	Tracks   int64      `json:"tracks"`
	Tagged   int64      `json:"tagged"`
	Embedded int64      `json:"embedded"`
	Pending  int64      `json:"pending"`
	Failed   int64      `json:"failed"`
	Genres   []TagCount `json:"genres"`
	Moods    []TagCount `json:"moods"`
	Today    UsageRow   `json:"today"`
	Daily    []UsageDay `json:"daily"`
}

// TagCount 标签计数。
type TagCount struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

// UsageRow 用量。
type UsageRow struct {
	Calls  int64 `json:"calls"`
	Tokens int64 `json:"tokens"`
	Items  int64 `json:"items"`
}

// UsageDay 一天的用量。
type UsageDay struct {
	Day string `json:"day"`
	UsageRow
}

// Content 内容理解总览。
func (p *PG) Content(ctx context.Context, appID int64, days int) (*ContentStats, error) {
	out := &ContentStats{Genres: []TagCount{}, Moods: []TagCount{}, Daily: []UsageDay{}}
	if err := p.Pool.QueryRow(ctx, `
SELECT count(*), count(g.tagged_at), count(g.embedding) FILTER (WHERE g.tagged_at IS NOT NULL),
  count(*) FILTER (WHERE g.tagged_at IS NULL AND COALESCE(g.attempts, 0) < 3),
  count(*) FILTER (WHERE g.tagged_at IS NULL AND g.attempts >= 3)
FROM music_tracks t LEFT JOIN music_track_tags g ON g.app_id = t.app_id AND g.work_key = t.work_key WHERE t.app_id = $1`, appID).
		Scan(&out.Tracks, &out.Tagged, &out.Embedded, &out.Pending, &out.Failed); err != nil {
		return nil, err
	}
	for _, f := range []struct {
		col string
		dst *[]TagCount
	}{{"genres", &out.Genres}, {"moods", &out.Moods}} {
		rows, err := p.Pool.Query(ctx, `SELECT k, count(*) FROM music_track_tags, unnest(`+f.col+`) AS k
WHERE app_id = $1 AND tagged_at IS NOT NULL GROUP BY k ORDER BY count(*) DESC, k`, appID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var c TagCount
			if err := rows.Scan(&c.Key, &c.Count); err != nil {
				rows.Close()
				return nil, err
			}
			*f.dst = append(*f.dst, c)
		}
		rows.Close()
	}
	// 「今天」以数据库的 current_date 为准（与写入用量时一致），不用本进程的时区
	if err := p.Pool.QueryRow(ctx, `SELECT COALESCE(sum(calls), 0), COALESCE(sum(tokens), 0), COALESCE(sum(items), 0)
FROM music_llm_usage WHERE app_id = $1 AND day = current_date`, appID).Scan(&out.Today.Calls, &out.Today.Tokens, &out.Today.Items); err != nil {
		return nil, err
	}
	rows, err := p.Pool.Query(ctx, `SELECT to_char(day, 'YYYY-MM-DD'), sum(calls), sum(tokens), sum(items) FROM music_llm_usage
WHERE app_id = $1 AND day > current_date - $2::int GROUP BY day ORDER BY day`, appID, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var d UsageDay
		if err := rows.Scan(&d.Day, &d.Calls, &d.Tokens, &d.Items); err != nil {
			return nil, err
		}
		out.Daily = append(out.Daily, d)
	}
	return out, rows.Err()
}

// AddUsage 记大模型用量。
func (p *PG) AddUsage(ctx context.Context, appID int64, kind string, calls int, tokens int64, items int) error {
	_, err := p.Pool.Exec(ctx, `
INSERT INTO music_llm_usage (app_id, day, kind, calls, tokens, items) VALUES ($1, current_date, $2, $3, $4, $5)
ON CONFLICT (app_id, day, kind) DO UPDATE SET calls = music_llm_usage.calls + EXCLUDED.calls,
  tokens = music_llm_usage.tokens + EXCLUDED.tokens, items = music_llm_usage.items + EXCLUDED.items`, appID, kind, calls, tokens, items)
	return err
}

// TaggedToday 今天已打标签的首数（每日上限用）。
func (p *PG) TaggedToday(ctx context.Context, appID int64) (int64, error) {
	var n int64
	err := p.Pool.QueryRow(ctx, `SELECT COALESCE(sum(items), 0) FROM music_llm_usage WHERE app_id = $1 AND day = current_date AND kind = 'tag'`, appID).Scan(&n)
	return n, err
}

// ── 歌手知识 ──

// ArtistKnowledge 大模型给的歌手知识。
type ArtistKnowledge struct {
	Artist  string   `json:"artist"`
	Name    string   `json:"name"`
	Similar []string `json:"similar"`
	Genres  []string `json:"genres"`
	Lang    string   `json:"lang"`
}

// PendingArtists 曲库里出现、还没有歌手知识的歌手（按作品数多少）。
func (p *PG) PendingArtists(ctx context.Context, appID int64, limit int) ([]string, error) {
	rows, err := p.Pool.Query(ctx, `
SELECT artist FROM music_tracks WHERE app_id = $1 AND artist <> '' GROUP BY artist ORDER BY count(*) DESC LIMIT 2000`, appID)
	if err != nil {
		return nil, err
	}
	var names []string
	seen := map[string]bool{}
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			rows.Close()
			return nil, err
		}
		first := contract.FirstArtist(a)
		if n := contract.NormalizeArtist(first); n != "" && !seen[n] {
			seen[n] = true
			names = append(names, first)
		}
	}
	rows.Close()
	have := map[string]bool{}
	r2, err := p.Pool.Query(ctx, `SELECT artist FROM music_artist_tags WHERE app_id = $1`, appID)
	if err != nil {
		return nil, err
	}
	for r2.Next() {
		var a string
		_ = r2.Scan(&a)
		have[a] = true
	}
	r2.Close()
	var out []string
	for _, n := range names {
		if !have[contract.NormalizeArtist(n)] {
			out = append(out, n)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

// SaveArtists 写歌手知识。
func (p *PG) SaveArtists(ctx context.Context, appID int64, list []ArtistKnowledge) error {
	batch := &pgx.Batch{}
	for _, a := range list {
		batch.Queue(`INSERT INTO music_artist_tags (app_id, artist, name, similar_artists, genres, lang) VALUES ($1,$2,$3,$4,$5,$6)
ON CONFLICT (app_id, artist) DO UPDATE SET name = EXCLUDED.name, similar_artists = EXCLUDED.similar_artists, genres = EXCLUDED.genres,
  lang = EXCLUDED.lang, tagged_at = now()`, appID, a.Artist, a.Name, a.Similar, a.Genres, a.Lang)
	}
	return p.Pool.SendBatch(ctx, batch).Close()
}

// AllArtists 一个应用的全部歌手知识（规范化名 → 知识）。
func (p *PG) AllArtists(ctx context.Context, appID int64) (map[string]ArtistKnowledge, error) {
	rows, err := p.Pool.Query(ctx, `SELECT artist, name, similar_artists, genres, lang FROM music_artist_tags WHERE app_id = $1`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ArtistKnowledge{}
	for rows.Next() {
		var a ArtistKnowledge
		if err := rows.Scan(&a.Artist, &a.Name, &a.Similar, &a.Genres, &a.Lang); err != nil {
			return nil, err
		}
		out[a.Artist] = a
	}
	return out, rows.Err()
}

// ── 深度模型训练记录 ──

// DeepRun 一次深度模型训练。
type DeepRun struct {
	ID          int64           `json:"id"`
	AppID       int64           `json:"-"`
	Status      string          `json:"status"`
	Trigger     string          `json:"trigger"`
	Note        string          `json:"note"`
	Operator    string          `json:"operator"`
	CreatedAt   int64           `json:"createdAt"`
	StartedAt   *int64          `json:"startedAt"`
	FinishedAt  *int64          `json:"finishedAt"`
	PublishedAt *int64          `json:"publishedAt"`
	Current     bool            `json:"current"`
	Users       int             `json:"users"`
	Items       int             `json:"items"`
	Sequences   int             `json:"sequences"`
	Error       string          `json:"error"`
	Metrics     json.RawMessage `json:"metrics"`
	Ranker      json.RawMessage `json:"ranker"`
	Params      json.RawMessage `json:"params,omitempty"`
	Stages      json.RawMessage `json:"stages,omitempty"`
	Log         json.RawMessage `json:"log,omitempty"`
	Parity      json.RawMessage `json:"parity,omitempty"`
}

const deepColumns = `id, app_id, status, trigger, note, operator, created_at, started_at, finished_at, published_at,
users, items, sequences, error, metrics, ranker, params, stages, log, parity,
published_at IS NOT NULL AND published_at = (SELECT max(published_at) FROM music_deep_runs c WHERE c.app_id = r.app_id)`

func rawOrNull(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("null")
	}
	return json.RawMessage(b)
}

func scanDeep(row pgx.Row, full bool) (*DeepRun, error) {
	var r DeepRun
	var created time.Time
	var started, finished, published *time.Time
	var metrics, ranker, params, stages, logs, parity []byte
	if err := row.Scan(&r.ID, &r.AppID, &r.Status, &r.Trigger, &r.Note, &r.Operator, &created, &started, &finished, &published,
		&r.Users, &r.Items, &r.Sequences, &r.Error, &metrics, &ranker, &params, &stages, &logs, &parity, &r.Current); err != nil {
		return nil, err
	}
	r.CreatedAt = created.UnixMilli()
	r.StartedAt, r.FinishedAt, r.PublishedAt = millis(started), millis(finished), millis(published)
	r.Metrics, r.Ranker = rawOrNull(metrics), rawOrNull(ranker)
	if full {
		r.Params, r.Stages, r.Log, r.Parity = rawOrNull(params), rawOrNull(stages), rawOrNull(logs), rawOrNull(parity)
	}
	return &r, nil
}

// ErrDeepActive 已有排队或进行中的深度模型训练。
var ErrDeepActive = errors.New("该应用已有排队或进行中的深度模型训练")

// CreateDeepRun 建一条排队中的训练记录。
func (p *PG) CreateDeepRun(ctx context.Context, appID int64, trigger, note, operator string, params any) (*DeepRun, error) {
	raw, _ := json.Marshal(params)
	r, err := scanDeep(p.Pool.QueryRow(ctx, `INSERT INTO music_deep_runs (app_id, status, trigger, note, operator, params)
VALUES ($1, 'queued', $2, $3, $4, $5) RETURNING `+deepColumnsFor("music_deep_runs"), appID, trigger, note, operator, raw), false)
	if err != nil && isUnique(err) {
		return nil, ErrDeepActive
	}
	return r, err
}

// deepColumnsFor RETURNING 里没有别名 r，子查询改用表名。
func deepColumnsFor(table string) string {
	return `id, app_id, status, trigger, note, operator, created_at, started_at, finished_at, published_at,
users, items, sequences, error, metrics, ranker, params, stages, log, parity,
published_at IS NOT NULL AND published_at = (SELECT max(published_at) FROM music_deep_runs c WHERE c.app_id = ` + table + `.app_id)`
}

func isUnique(err error) bool {
	type sqlState interface{ SQLState() string }
	var s sqlState
	return errors.As(err, &s) && s.SQLState() == "23505"
}

// DeepRuns 训练记录分页。
func (p *PG) DeepRuns(ctx context.Context, appID int64, page, pageSize int) ([]DeepRun, int, error) {
	var total int
	if err := p.Pool.QueryRow(ctx, `SELECT count(*) FROM music_deep_runs WHERE app_id = $1`, appID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := p.Pool.Query(ctx, `SELECT `+deepColumns+` FROM music_deep_runs r WHERE app_id = $1 ORDER BY id DESC LIMIT $2 OFFSET $3`,
		appID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []DeepRun{}
	for rows.Next() {
		r, err := scanDeep(rows, false)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *r)
	}
	return out, total, rows.Err()
}

// DeepRunOf 一条训练记录（全部字段）。
func (p *PG) DeepRunOf(ctx context.Context, appID, id int64) (*DeepRun, error) {
	r, err := scanDeep(p.Pool.QueryRow(ctx, `SELECT `+deepColumns+` FROM music_deep_runs r WHERE app_id = $1 AND id = $2`, appID, id), true)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}

// CurrentDeepRun 当前发布的深度模型（没有为 nil）。
func (p *PG) CurrentDeepRun(ctx context.Context, appID int64) (*DeepRun, error) {
	r, err := scanDeep(p.Pool.QueryRow(ctx, `SELECT `+deepColumns+` FROM music_deep_runs r
WHERE app_id = $1 AND published_at IS NOT NULL ORDER BY published_at DESC LIMIT 1`, appID), false)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

// PublishDeepRun 发布（回滚也用它）；只能发布成功的。
func (p *PG) PublishDeepRun(ctx context.Context, appID, id int64) error {
	tag, err := p.Pool.Exec(ctx, `UPDATE music_deep_runs SET published_at = now() WHERE app_id = $1 AND id = $2 AND status = 'succeeded'`, appID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w：只能发布训练成功的版本", ErrConflict)
	}
	return nil
}

// PublishedDeepRuns 每个应用当前发布的深度模型版本。
func (p *PG) PublishedDeepRuns(ctx context.Context) (map[int64]int64, error) {
	rows, err := p.Pool.Query(ctx, `SELECT DISTINCT ON (app_id) app_id, id FROM music_deep_runs
WHERE published_at IS NOT NULL ORDER BY app_id, published_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int64{}
	for rows.Next() {
		var app, id int64
		if err := rows.Scan(&app, &id); err != nil {
			return nil, err
		}
		out[app] = id
	}
	return out, rows.Err()
}

// DeepBlobs 一次训练的全部产物（kind → 数据）。
func (p *PG) DeepBlobs(ctx context.Context, runID int64) (map[string][]byte, error) {
	rows, err := p.Pool.Query(ctx, `SELECT kind, data FROM music_deep_blobs WHERE run_id = $1`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]byte{}
	for rows.Next() {
		var k string
		var d []byte
		if err := rows.Scan(&k, &d); err != nil {
			return nil, err
		}
		out[k] = d
	}
	return out, rows.Err()
}
