// Package store 音乐服务的三种存储：PostgreSQL（曲库维表、应用配置、训练记录与模型）、
// ClickHouse（事件与曝光，见 clickhouse.go）。Redis 实时状态在 realtime 包。
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"aegis/services/music/contract"
	"aegis/services/music/internal/appcfg"
)

// PG PostgreSQL 存储（库 aegis_music）。
type PG struct{ Pool *pgxpool.Pool }

// OpenPG 连接并建表（幂等）。
func OpenPG(ctx context.Context, dsn string) (*PG, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("MUSIC_PG_DSN 无效：%w", err)
	}
	if cfg.MaxConns < 8 {
		cfg.MaxConns = 8
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		// 库还不存在（首次部署）：连同一实例的 postgres 库建出来再连
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "3D000" {
			return nil, fmt.Errorf("连接 PostgreSQL 失败：%w", err)
		}
		if err := createDatabase(ctx, cfg.ConnConfig.Copy(), cfg.ConnConfig.Database); err != nil {
			return nil, err
		}
		if pool, err = pgxpool.NewWithConfig(ctx, cfg); err != nil {
			return nil, err
		}
		if err := pool.Ping(ctx); err != nil {
			pool.Close()
			return nil, fmt.Errorf("连接 PostgreSQL 失败：%w", err)
		}
	}
	pg := &PG{Pool: pool}
	if err := pg.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pg, nil
}

func createDatabase(ctx context.Context, cc *pgx.ConnConfig, name string) error {
	cc.Database = "postgres"
	conn, err := pgx.ConnectConfig(ctx, cc)
	if err != nil {
		return fmt.Errorf("库 %s 不存在，且连不上 postgres 库来创建它：%w", name, err)
	}
	defer conn.Close(context.Background())
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		var pgErr *pgconn.PgError
		// 别的实例刚建好：已存在是 42P04；几个实例同时 CREATE DATABASE 时后到的撞上目录唯一索引（23505）
		if errors.As(err, &pgErr) && (pgErr.Code == "42P04" || pgErr.Code == "23505") {
			return nil
		}
		return fmt.Errorf("创建库 %s 失败（账号需要 CREATEDB 权限，或先手动建库）：%w", name, err)
	}
	return nil
}

// Close 关闭连接池。
func (p *PG) Close() { p.Pool.Close() }

const pgSchema = `
CREATE TABLE IF NOT EXISTS music_tracks (
  app_id      BIGINT      NOT NULL,
  work_key    TEXT        NOT NULL,
  source      TEXT        NOT NULL,
  song_id     TEXT        NOT NULL,
  title       TEXT        NOT NULL,
  artist      TEXT        NOT NULL DEFAULT '',
  album       TEXT        NOT NULL DEFAULT '',
  cover       TEXT        NOT NULL DEFAULT '',
  duration_ms BIGINT      NOT NULL DEFAULT 0,
  first_seen  TIMESTAMPTZ NOT NULL,
  last_seen   TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (app_id, work_key)
);
ALTER TABLE music_tracks ADD COLUMN IF NOT EXISTS lang TEXT NOT NULL DEFAULT '';
CREATE TABLE IF NOT EXISTS music_track_variants (
  app_id    BIGINT      NOT NULL,
  source    TEXT        NOT NULL,
  song_id   TEXT        NOT NULL,
  work_key  TEXT        NOT NULL,
  last_seen TIMESTAMPTZ NOT NULL,
  PRIMARY KEY (app_id, source, song_id)
);
CREATE INDEX IF NOT EXISTS music_track_variants_work ON music_track_variants (app_id, work_key);
CREATE TABLE IF NOT EXISTS music_configs (
  app_id     BIGINT      PRIMARY KEY,
  config     JSONB       NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_by TEXT        NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS music_model_runs (
  id           BIGSERIAL   PRIMARY KEY,
  app_id       BIGINT      NOT NULL,
  status       TEXT        NOT NULL,
  trigger      TEXT        NOT NULL,
  note         TEXT        NOT NULL DEFAULT '',
  operator     TEXT        NOT NULL DEFAULT '',
  params       JSONB,
  metrics      JSONB,
  stages       JSONB       NOT NULL DEFAULT '[]',
  log          JSONB       NOT NULL DEFAULT '[]',
  users        INT         NOT NULL DEFAULT 0,
  items        INT         NOT NULL DEFAULT 0,
  interactions BIGINT      NOT NULL DEFAULT 0,
  size_bytes   BIGINT      NOT NULL DEFAULT 0,
  error        TEXT        NOT NULL DEFAULT '',
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  started_at   TIMESTAMPTZ,
  finished_at  TIMESTAMPTZ,
  published_at TIMESTAMPTZ,
  heartbeat_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS music_model_runs_app ON music_model_runs (app_id, id DESC);
CREATE INDEX IF NOT EXISTS music_model_runs_published ON music_model_runs (app_id, published_at DESC) WHERE published_at IS NOT NULL;
-- 一个应用同一时刻最多一个排队或进行中的训练
CREATE UNIQUE INDEX IF NOT EXISTS music_model_runs_active ON music_model_runs (app_id) WHERE status IN ('queued', 'running');
CREATE TABLE IF NOT EXISTS music_model_blobs (
  run_id BIGINT PRIMARY KEY REFERENCES music_model_runs (id) ON DELETE CASCADE,
  data   BYTEA  NOT NULL
);
CREATE TABLE IF NOT EXISTS music_forget_log (
  id         BIGSERIAL   PRIMARY KEY,
  app_id     BIGINT      NOT NULL,
  user_id    BIGINT      NOT NULL,
  reason     TEXT        NOT NULL,
  operator   TEXT        NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS music_forget_log_app ON music_forget_log (app_id, created_at DESC);
`

func (p *PG) migrate(ctx context.Context) error {
	// 多个实例同时启动时串行建表
	conn, err := p.Pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(778801)`); err != nil {
		return err
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock(778801)`) //nolint:errcheck
	if _, err := conn.Exec(ctx, pgSchema); err != nil {
		return fmt.Errorf("PostgreSQL 建表失败：%w", err)
	}
	if _, err := conn.Exec(ctx, pgSchema2); err != nil {
		return fmt.Errorf("PostgreSQL 建表失败（第二期）：%w", err)
	}
	return nil
}

// ── 曲库 ──

// TrackRow 曲库里的一个作品（展示用它最近被播放的那个音源版本）。
type TrackRow struct {
	WorkKey   string
	Track     contract.Track
	FirstSeen time.Time
	LastSeen  time.Time
}

// UpsertTracks 批量 upsert 作品与音源版本（ingest 每批调一次；同一批里同一作品取最后一次）。
func (p *PG) UpsertTracks(ctx context.Context, appTracks map[int64]map[string]TrackRow) error {
	if len(appTracks) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for appID, rows := range appTracks {
		for _, row := range rows {
			t := row.Track
			batch.Queue(`
INSERT INTO music_tracks (app_id, work_key, source, song_id, title, artist, album, cover, duration_ms, first_seen, last_seen, lang)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10,$11)
ON CONFLICT (app_id, work_key) DO UPDATE SET
  lang = CASE WHEN music_tracks.lang = '' OR EXCLUDED.last_seen >= music_tracks.last_seen THEN EXCLUDED.lang ELSE music_tracks.lang END,
  source = CASE WHEN EXCLUDED.last_seen >= music_tracks.last_seen THEN EXCLUDED.source ELSE music_tracks.source END,
  song_id = CASE WHEN EXCLUDED.last_seen >= music_tracks.last_seen THEN EXCLUDED.song_id ELSE music_tracks.song_id END,
  title = CASE WHEN EXCLUDED.last_seen >= music_tracks.last_seen THEN EXCLUDED.title ELSE music_tracks.title END,
  artist = CASE WHEN EXCLUDED.last_seen >= music_tracks.last_seen AND EXCLUDED.artist <> '' THEN EXCLUDED.artist ELSE music_tracks.artist END,
  album = CASE WHEN EXCLUDED.last_seen >= music_tracks.last_seen AND EXCLUDED.album <> '' THEN EXCLUDED.album ELSE music_tracks.album END,
  cover = CASE WHEN EXCLUDED.cover <> '' AND (EXCLUDED.last_seen >= music_tracks.last_seen OR music_tracks.cover = '') THEN EXCLUDED.cover ELSE music_tracks.cover END,
  duration_ms = CASE WHEN EXCLUDED.duration_ms > 0 THEN EXCLUDED.duration_ms ELSE music_tracks.duration_ms END,
  first_seen = LEAST(music_tracks.first_seen, EXCLUDED.first_seen),
  last_seen = GREATEST(music_tracks.last_seen, EXCLUDED.last_seen)`,
				appID, row.WorkKey, t.Source, t.SongID, t.Title, t.Artist, t.Album, t.Cover, t.DurationMs, row.LastSeen,
				contract.DetectLang(t.Title, t.Artist))
			batch.Queue(`
INSERT INTO music_track_variants (app_id, source, song_id, work_key, last_seen) VALUES ($1,$2,$3,$4,$5)
ON CONFLICT (app_id, source, song_id) DO UPDATE SET work_key = EXCLUDED.work_key,
  last_seen = GREATEST(music_track_variants.last_seen, EXCLUDED.last_seen)`,
				appID, t.Source, t.SongID, row.WorkKey, row.LastSeen)
		}
	}
	return p.Pool.SendBatch(ctx, batch).Close()
}

// Tracks 按作品键取曲库（缺的不返回）。
func (p *PG) Tracks(ctx context.Context, appID int64, keys []string) (map[string]contract.Track, error) {
	out := make(map[string]contract.Track, len(keys))
	if len(keys) == 0 {
		return out, nil
	}
	rows, err := p.Pool.Query(ctx, `
SELECT work_key, source, song_id, title, artist, album, cover, duration_ms
FROM music_tracks WHERE app_id = $1 AND work_key = ANY($2)`, appID, keys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var t contract.Track
		if err := rows.Scan(&key, &t.Source, &t.SongID, &t.Title, &t.Artist, &t.Album, &t.Cover, &t.DurationMs); err != nil {
			return nil, err
		}
		out[key] = t
	}
	return out, rows.Err()
}

// WorkKeyOf 音源版本 → 作品键（相似歌曲的种子）。没有返回空串。
func (p *PG) WorkKeyOf(ctx context.Context, appID int64, source, songID string) (string, error) {
	var key string
	err := p.Pool.QueryRow(ctx, `SELECT work_key FROM music_track_variants WHERE app_id=$1 AND source=$2 AND song_id=$3`,
		appID, source, songID).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return key, err
}

// Variant 作品的一个音源版本。
type Variant struct {
	Source     string `json:"source"`
	SongID     string `json:"songId"`
	LastSeenAt int64  `json:"lastSeenAt"`
}

// Variants 作品的全部音源版本。
func (p *PG) Variants(ctx context.Context, appID int64, workKey string) ([]Variant, error) {
	rows, err := p.Pool.Query(ctx, `SELECT source, song_id, last_seen FROM music_track_variants
WHERE app_id=$1 AND work_key=$2 ORDER BY last_seen DESC LIMIT 50`, appID, workKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Variant{}
	for rows.Next() {
		var v Variant
		var at time.Time
		if err := rows.Scan(&v.Source, &v.SongID, &at); err != nil {
			return nil, err
		}
		v.LastSeenAt = at.UnixMilli()
		out = append(out, v)
	}
	return out, rows.Err()
}

// ── 应用配置 ──

// Config 应用配置（没有记录时是默认值）。
func (p *PG) Config(ctx context.Context, appID int64) (appcfg.Config, error) {
	cfg := appcfg.Default()
	var raw []byte
	var at time.Time
	var by string
	err := p.Pool.QueryRow(ctx, `SELECT config, updated_at, updated_by FROM music_configs WHERE app_id=$1`, appID).Scan(&raw, &at, &by)
	if errors.Is(err, pgx.ErrNoRows) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	// 先铺默认值再覆盖：老记录缺的新字段取默认
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return appcfg.Default(), fmt.Errorf("应用 %d 的音乐配置损坏：%w", appID, err)
	}
	cfg.UpdatedAt = at.UnixMilli()
	cfg.UpdatedBy = by
	return cfg, nil
}

// SaveConfig 保存应用配置（调用方先 Validate）。
func (p *PG) SaveConfig(ctx context.Context, appID int64, cfg appcfg.Config, operator string) (appcfg.Config, error) {
	cfg.UpdatedAt, cfg.UpdatedBy = 0, ""
	raw, err := json.Marshal(cfg)
	if err != nil {
		return cfg, err
	}
	var at time.Time
	err = p.Pool.QueryRow(ctx, `
INSERT INTO music_configs (app_id, config, updated_at, updated_by) VALUES ($1,$2,now(),$3)
ON CONFLICT (app_id) DO UPDATE SET config=EXCLUDED.config, updated_at=now(), updated_by=EXCLUDED.updated_by
RETURNING updated_at`, appID, raw, operator).Scan(&at)
	if err != nil {
		return cfg, err
	}
	cfg.UpdatedAt, cfg.UpdatedBy = at.UnixMilli(), operator
	return cfg, nil
}

// ConfiguredApps 有配置记录或有过训练的应用（定时训练遍历用）。
func (p *PG) ConfiguredApps(ctx context.Context) ([]int64, error) {
	rows, err := p.Pool.Query(ctx, `SELECT app_id FROM music_configs UNION SELECT DISTINCT app_id FROM music_tracks`)
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

// ── 遗忘 ──

// LogForget 记一次清除（模型加载时据此跳过这个人）。
func (p *PG) LogForget(ctx context.Context, appID, userID int64, reason, operator string) error {
	_, err := p.Pool.Exec(ctx, `INSERT INTO music_forget_log (app_id, user_id, reason, operator) VALUES ($1,$2,$3,$4)`,
		appID, userID, reason, operator)
	return err
}

// ForgottenSince 某时刻之后被清除的用户。
func (p *PG) ForgottenSince(ctx context.Context, appID int64, since time.Time) (map[int64]bool, error) {
	rows, err := p.Pool.Query(ctx, `SELECT DISTINCT user_id FROM music_forget_log WHERE app_id=$1 AND created_at >= $2`, appID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// ── 训练记录 ──

// Stage 训练的一个阶段。
type Stage struct {
	Name string `json:"name"`
	Ms   int64  `json:"ms"`
}

// Metrics 离线评估指标。
type Metrics struct {
	Recall20  float64     `json:"recall20"`
	NDCG20    float64     `json:"ndcg20"`
	HitRate20 float64     `json:"hitRate20"`
	Coverage  float64     `json:"coverage"`
	Baseline  BaseMetrics `json:"baseline"`
	TestUsers int         `json:"testUsers"`
	AUC       float64     `json:"auc,omitempty"` // 精排留出集 AUC（启用精排时才有）
}

// BaseMetrics 热度基线。
type BaseMetrics struct {
	Recall20  float64 `json:"recall20"`
	NDCG20    float64 `json:"ndcg20"`
	HitRate20 float64 `json:"hitRate20"`
	Coverage  float64 `json:"coverage"`
}

// Run 一次训练。
type Run struct {
	ID           int64          `json:"id"`
	AppID        int64          `json:"-"`
	Status       string         `json:"status"`
	Trigger      string         `json:"trigger"`
	Note         string         `json:"note"`
	Operator     string         `json:"operator"`
	CreatedAt    int64          `json:"createdAt"`
	StartedAt    *int64         `json:"startedAt"`
	FinishedAt   *int64         `json:"finishedAt"`
	PublishedAt  *int64         `json:"publishedAt"`
	Current      bool           `json:"current"`
	Users        int            `json:"users"`
	Items        int            `json:"items"`
	Interactions int64          `json:"interactions"`
	SizeBytes    int64          `json:"sizeBytes"`
	Metrics      *Metrics       `json:"metrics"`
	Ranker       *RankerInfo    `json:"ranker"`
	Error        string         `json:"error"`
	Params       *appcfg.Config `json:"params,omitempty"`
	Stages       []Stage        `json:"stages,omitempty"`
	Log          []string       `json:"log,omitempty"`
	heartbeatAt  *time.Time
}

// 训练状态。
const (
	RunQueued    = "queued"
	RunRunning   = "running"
	RunSucceeded = "succeeded"
	RunFailed    = "failed"
)

// ErrRunActive 已有排队或进行中的训练。
var ErrRunActive = errors.New("该应用已有排队或进行中的训练")

// CreateRun 新建一次排队中的训练；已有排队 / 进行中的返回那一次与 ErrRunActive。
func (p *PG) CreateRun(ctx context.Context, appID int64, trigger, note, operator string) (*Run, error) {
	var id int64
	err := p.Pool.QueryRow(ctx, `
INSERT INTO music_model_runs (app_id, status, trigger, note, operator) VALUES ($1,'queued',$2,$3,$4)
ON CONFLICT (app_id) WHERE status IN ('queued','running') DO NOTHING RETURNING id`,
		appID, trigger, note, operator).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		run, err := p.activeRun(ctx, appID)
		if err != nil {
			return nil, err
		}
		return run, ErrRunActive
	}
	if err != nil {
		return nil, err
	}
	return p.Run(ctx, appID, id)
}

func (p *PG) activeRun(ctx context.Context, appID int64) (*Run, error) {
	var id int64
	if err := p.Pool.QueryRow(ctx, `SELECT id FROM music_model_runs WHERE app_id=$1 AND status IN ('queued','running') ORDER BY id DESC LIMIT 1`, appID).Scan(&id); err != nil {
		return nil, err
	}
	return p.Run(ctx, appID, id)
}

const runColumns = `id, app_id, status, trigger, note, operator, params, metrics, stages, log, users, items, interactions,
size_bytes, error, created_at, started_at, finished_at, published_at, heartbeat_at, ranker,
published_at IS NOT NULL AND published_at = (SELECT max(published_at) FROM music_model_runs c WHERE c.app_id = r.app_id)`

func scanRun(row pgx.Row, full bool) (*Run, error) {
	var r Run
	var params, metrics, stages, logs, ranker []byte
	var created time.Time
	var started, finished, published, heartbeat *time.Time
	if err := row.Scan(&r.ID, &r.AppID, &r.Status, &r.Trigger, &r.Note, &r.Operator, &params, &metrics, &stages, &logs,
		&r.Users, &r.Items, &r.Interactions, &r.SizeBytes, &r.Error, &created, &started, &finished, &published, &heartbeat, &ranker, &r.Current); err != nil {
		return nil, err
	}
	if len(ranker) > 0 && string(ranker) != "null" {
		r.Ranker = &RankerInfo{}
		_ = json.Unmarshal(ranker, r.Ranker)
	}
	r.CreatedAt = created.UnixMilli()
	r.StartedAt, r.FinishedAt, r.PublishedAt = millis(started), millis(finished), millis(published)
	r.heartbeatAt = heartbeat
	if len(metrics) > 0 && string(metrics) != "null" {
		r.Metrics = &Metrics{}
		_ = json.Unmarshal(metrics, r.Metrics)
	}
	if full {
		if len(params) > 0 && string(params) != "null" {
			r.Params = &appcfg.Config{}
			_ = json.Unmarshal(params, r.Params)
		}
		r.Stages, r.Log = []Stage{}, []string{}
		_ = json.Unmarshal(stages, &r.Stages)
		_ = json.Unmarshal(logs, &r.Log)
	}
	return &r, nil
}

func millis(t *time.Time) *int64 {
	if t == nil {
		return nil
	}
	v := t.UnixMilli()
	return &v
}

// Run 训练详情（含参数、阶段、日志）。
func (p *PG) Run(ctx context.Context, appID, id int64) (*Run, error) {
	row := p.Pool.QueryRow(ctx, `SELECT `+runColumns+` FROM music_model_runs r WHERE app_id=$1 AND id=$2`, appID, id)
	run, err := scanRun(row, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return run, err
}

// RunByID 训练详情（trainer 用，不限应用）。
func (p *PG) RunByID(ctx context.Context, id int64) (*Run, error) {
	row := p.Pool.QueryRow(ctx, `SELECT `+runColumns+` FROM music_model_runs r WHERE id=$1`, id)
	run, err := scanRun(row, true)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return run, err
}

// ErrNotFound 记录不存在。
var ErrNotFound = errors.New("not found")

// Runs 训练记录分页。
func (p *PG) Runs(ctx context.Context, appID int64, page, pageSize int) ([]Run, int, error) {
	var total int
	if err := p.Pool.QueryRow(ctx, `SELECT count(*) FROM music_model_runs WHERE app_id=$1`, appID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := p.Pool.Query(ctx, `SELECT `+runColumns+` FROM music_model_runs r WHERE app_id=$1 ORDER BY id DESC LIMIT $2 OFFSET $3`,
		appID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		run, err := scanRun(rows, false)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *run)
	}
	return out, total, rows.Err()
}

// CurrentRun 当前发布的模型（没有返回 nil）。
func (p *PG) CurrentRun(ctx context.Context, appID int64) (*Run, error) {
	row := p.Pool.QueryRow(ctx, `SELECT `+runColumns+` FROM music_model_runs r
WHERE app_id=$1 AND published_at IS NOT NULL ORDER BY published_at DESC LIMIT 1`, appID)
	run, err := scanRun(row, false)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return run, err
}

// PublishedRuns 每个应用当前发布的版本号（music-rec 轮询比对用）。
func (p *PG) PublishedRuns(ctx context.Context) (map[int64]int64, error) {
	rows, err := p.Pool.Query(ctx, `SELECT DISTINCT ON (app_id) app_id, id FROM music_model_runs
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

// StartRun 认领一次训练：queued → running；已经 running 但心跳超时（上一个 trainer 崩了）也能认领。
func (p *PG) StartRun(ctx context.Context, id int64, params appcfg.Config, staleAfter time.Duration) (bool, error) {
	raw, _ := json.Marshal(params)
	tag, err := p.Pool.Exec(ctx, `
UPDATE music_model_runs SET status='running', started_at=now(), heartbeat_at=now(), params=$2, stages='[]', log='[]', error=''
WHERE id=$1 AND (status='queued' OR (status='running' AND heartbeat_at < now() - $3::interval))`,
		id, raw, fmt.Sprintf("%d seconds", int(staleAfter.Seconds())))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// Heartbeat 训练心跳，并追加日志。
func (p *PG) Heartbeat(ctx context.Context, id int64, line string) error {
	if line == "" {
		_, err := p.Pool.Exec(ctx, `UPDATE music_model_runs SET heartbeat_at=now() WHERE id=$1`, id)
		return err
	}
	_, err := p.Pool.Exec(ctx, `UPDATE music_model_runs SET heartbeat_at=now(), log = log || to_jsonb($2::text) WHERE id=$1`, id, line)
	return err
}

// FinishRun 训练成功：写指标、规模、模型。
func (p *PG) FinishRun(ctx context.Context, id int64, metrics *Metrics, ranker *RankerInfo, stages []Stage, users, items int, interactions int64, blob []byte) error {
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background()) //nolint:errcheck
	if _, err := tx.Exec(ctx, `INSERT INTO music_model_blobs (run_id, data) VALUES ($1,$2)
ON CONFLICT (run_id) DO UPDATE SET data=EXCLUDED.data`, id, blob); err != nil {
		return err
	}
	m, _ := json.Marshal(metrics)
	s, _ := json.Marshal(stages)
	rk, _ := json.Marshal(ranker)
	if _, err := tx.Exec(ctx, `UPDATE music_model_runs SET status='succeeded', finished_at=now(), heartbeat_at=now(),
metrics=$2, stages=$3, users=$4, items=$5, interactions=$6, size_bytes=$7, ranker=$8 WHERE id=$1`,
		id, m, s, users, items, interactions, len(blob), rk); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// FailRun 训练失败。
func (p *PG) FailRun(ctx context.Context, id int64, stages []Stage, msg string) error {
	s, _ := json.Marshal(stages)
	_, err := p.Pool.Exec(ctx, `UPDATE music_model_runs SET status='failed', finished_at=now(), stages=$2, error=$3 WHERE id=$1`,
		id, s, strings.TrimSpace(msg))
	return err
}

// PublishRun 发布一个成功的版本（回滚也是它）。
func (p *PG) PublishRun(ctx context.Context, appID, id int64) error {
	tag, err := p.Pool.Exec(ctx, `UPDATE music_model_runs SET published_at=now()
WHERE app_id=$1 AND id=$2 AND status='succeeded' AND EXISTS (SELECT 1 FROM music_model_blobs b WHERE b.run_id = $2)`, appID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Blob 模型快照。
func (p *PG) Blob(ctx context.Context, runID int64) ([]byte, error) {
	var data []byte
	err := p.Pool.QueryRow(ctx, `SELECT data FROM music_model_blobs WHERE run_id=$1`, runID).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return data, err
}

// PruneBlobs 只保留每个应用最近 keep 个成功版本与当前发布版本的模型文件（记录本身保留）。
func (p *PG) PruneBlobs(ctx context.Context, appID int64, keep int) error {
	_, err := p.Pool.Exec(ctx, `
DELETE FROM music_model_blobs b USING music_model_runs r
WHERE b.run_id = r.id AND r.app_id = $1
  AND r.id NOT IN (SELECT id FROM music_model_runs WHERE app_id=$1 AND status='succeeded' ORDER BY id DESC LIMIT $2)
  AND r.id NOT IN (SELECT id FROM music_model_runs WHERE app_id=$1 AND published_at IS NOT NULL ORDER BY published_at DESC LIMIT 1)`,
		appID, keep)
	return err
}

// FailStaleRuns 心跳超时的进行中训练标记失败（trainer 崩溃且任务消息也过期时的兜底）。
func (p *PG) FailStaleRuns(ctx context.Context, staleAfter time.Duration) (int64, error) {
	tag, err := p.Pool.Exec(ctx, `UPDATE music_model_runs SET status='failed', finished_at=now(), error='训练进程中断'
WHERE (status='running' AND heartbeat_at < now() - $1::interval)
   OR (status='queued' AND created_at < now() - interval '6 hours')`, fmt.Sprintf("%d seconds", int(staleAfter.Seconds())))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
