package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"aegis/services/music/contract"
)

// CH ClickHouse 存储（库 aegis_music）：事件是权威记录，分析与训练都从这里读。
//
// 表是 ReplacingMergeTree，按 (app_id, user_id, event_id) 去重：ingest 重投的批次再写一遍不会多算，
// 但合并是后台异步的，所以凡是计数的查询一律带 FINAL。
type CH struct {
	conn driver.Conn
	db   string
	// TZ 按天、按小时分桶用的时区（服务器时区，默认 Asia/Shanghai）。
	TZ string
}

// CHOptions 连接参数。
type CHOptions struct {
	Addr     []string
	DB       string
	User     string
	Password string
	TZ       string
}

// OpenCH 连接、建库建表（幂等）。
func OpenCH(ctx context.Context, o CHOptions) (*CH, error) {
	if o.DB == "" {
		o.DB = "aegis_music"
	}
	if o.TZ == "" {
		o.TZ = "Asia/Shanghai"
	}
	boot, err := openCH(o, "default")
	if err != nil {
		return nil, err
	}
	err = boot.Exec(ctx, "CREATE DATABASE IF NOT EXISTS "+quoteIdent(o.DB))
	_ = boot.Close()
	if err != nil {
		return nil, fmt.Errorf("ClickHouse 建库失败：%w", err)
	}
	conn, err := openCH(o, o.DB)
	if err != nil {
		return nil, err
	}
	c := &CH{conn: conn, db: o.DB, TZ: o.TZ}
	for _, stmt := range chSchema {
		if err := conn.Exec(ctx, stmt); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("ClickHouse 建表失败：%w\n%s", err, stmt)
		}
	}
	return c, nil
}

func openCH(o CHOptions, db string) (driver.Conn, error) {
	conn, err := clickhouse.Open(&clickhouse.Options{
		Addr:            o.Addr,
		Auth:            clickhouse.Auth{Database: db, Username: o.User, Password: o.Password},
		DialTimeout:     5 * time.Second,
		ReadTimeout:     5 * time.Minute,
		MaxOpenConns:    16,
		MaxIdleConns:    4,
		ConnMaxLifetime: time.Hour,
		Compression:     &clickhouse.Compression{Method: clickhouse.CompressionLZ4},
		Settings:        clickhouse.Settings{"max_execution_time": 300},
	})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := conn.Ping(ctx); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("连接 ClickHouse 失败：%w", err)
	}
	return conn, nil
}

func quoteIdent(s string) string { return "`" + strings.ReplaceAll(s, "`", "") + "`" }

// Close 关闭。
func (c *CH) Close() error { return c.conn.Close() }

// Ping 健康检查。
func (c *CH) Ping(ctx context.Context) error { return c.conn.Ping(ctx) }

var chSchema = []string{
	`CREATE TABLE IF NOT EXISTS events (
		app_id UInt64, user_id UInt64, event_id String,
		type LowCardinality(String), ts DateTime64(3, 'UTC'), received_at DateTime64(3, 'UTC'),
		device_id String, session_id String, ip String,
		work_key String, track_key String,
		source LowCardinality(String), song_id String, title String, artist String, album String, cover String,
		duration_ms UInt32, played_ms UInt32, position_ms UInt32, reason LowCardinality(String),
		page LowCardinality(String), section LowCardinality(String), position Int32, query String,
		rec_id String, scene LowCardinality(String), playlist_id String, queue_source LowCardinality(String),
		platform LowCardinality(String), app_version LowCardinality(String), network LowCardinality(String),
		props Map(String, String),
		INDEX idx_ts ts TYPE minmax GRANULARITY 4,
		INDEX idx_work work_key TYPE bloom_filter GRANULARITY 4,
		INDEX idx_rec rec_id TYPE bloom_filter GRANULARITY 4
	) ENGINE = ReplacingMergeTree(received_at)
	PARTITION BY toYYYYMM(ts)
	ORDER BY (app_id, user_id, event_id)
	TTL toDateTime(ts) + INTERVAL 400 DAY`,
	`CREATE TABLE IF NOT EXISTS impressions (
		app_id UInt64, user_id UInt64, rec_id String, scene LowCardinality(String),
		model_version UInt64, personalized UInt8, cold_start UInt8, items Array(String),
		ts DateTime64(3, 'UTC')
	) ENGINE = ReplacingMergeTree(ts)
	PARTITION BY toYYYYMM(ts)
	ORDER BY (app_id, user_id, rec_id)
	TTL toDateTime(ts) + INTERVAL 180 DAY`,
	// 第二期：实验分桶、精排、运营规则
	`ALTER TABLE impressions ADD COLUMN IF NOT EXISTS experiment_id UInt64 DEFAULT 0`,
	`ALTER TABLE impressions ADD COLUMN IF NOT EXISTS variant LowCardinality(String) DEFAULT ''`,
	`ALTER TABLE impressions ADD COLUMN IF NOT EXISTS ranker UInt8 DEFAULT 0`,
	`ALTER TABLE impressions ADD COLUMN IF NOT EXISTS rules Array(UInt64) DEFAULT []`,
	// 精排训练样本：出推荐时逐首记录特征（与线上打分用的完全一致），训练时按 rec_id + work_key 关联后续行为得到标签
	`CREATE TABLE IF NOT EXISTS rank_samples (
		app_id UInt64, user_id UInt64, rec_id String, scene LowCardinality(String),
		position UInt16, work_key String, model_version UInt64, features Array(Float32),
		ts DateTime64(3, 'UTC')
	) ENGINE = ReplacingMergeTree(ts)
	PARTITION BY toYYYYMM(ts)
	ORDER BY (app_id, rec_id, work_key)
	TTL toDateTime(ts) + INTERVAL 60 DAY`,
}

func ms(v int64) time.Time { return time.UnixMilli(v).UTC() }

// InsertEvents 同步批量写事件（返回即已落盘）。
func (c *CH) InsertEvents(ctx context.Context, events []contract.Event) error {
	if len(events) == 0 {
		return nil
	}
	b, err := c.conn.PrepareBatch(ctx, `INSERT INTO events (app_id, user_id, event_id, type, ts, received_at, device_id, session_id, ip,
		work_key, track_key, source, song_id, title, artist, album, cover, duration_ms, played_ms, position_ms, reason,
		page, section, position, query, rec_id, scene, playlist_id, queue_source, platform, app_version, network, props)`)
	if err != nil {
		return err
	}
	for i := range events {
		e := &events[i]
		var t contract.Track
		if e.Track != nil {
			t = *e.Track
		}
		props := e.Props
		if props == nil {
			props = map[string]string{}
		}
		if err := b.Append(uint64(e.AppID), uint64(e.UserID), e.ID, e.Type, ms(e.TS), ms(e.ReceivedAt), e.DeviceID, e.SessionID, e.IP,
			e.WorkKey, e.TrackKey, t.Source, t.SongID, t.Title, t.Artist, t.Album, t.Cover,
			clampU32(t.DurationMs), clampU32(e.PlayedMs), clampU32(e.PositionMs), e.Reason,
			e.Context.Page, e.Context.Section, e.Context.Position, e.Context.Query, e.Context.RecID, e.Context.Scene,
			e.Context.PlaylistID, e.Context.QueueSource, e.Platform, e.AppVersion, e.Network, props); err != nil {
			_ = b.Abort()
			return err
		}
	}
	return b.Send()
}

// InsertServed 同步批量写推荐结果。
func (c *CH) InsertServed(ctx context.Context, served []contract.Served) error {
	if len(served) == 0 {
		return nil
	}
	b, err := c.conn.PrepareBatch(ctx, `INSERT INTO impressions (app_id, user_id, rec_id, scene, model_version, personalized, cold_start, items, ts,
		experiment_id, variant, ranker, rules)`)
	if err != nil {
		return err
	}
	samples := 0
	for _, s := range served {
		items := s.Items
		if items == nil {
			items = []string{}
		}
		rules := make([]uint64, len(items))
		for k := range items {
			if k < len(s.Rules) && s.Rules[k] > 0 {
				rules[k] = uint64(s.Rules[k])
			}
		}
		if err := b.Append(uint64(s.AppID), uint64(s.UserID), s.RecID, s.Scene, uint64(max(s.ModelVersion, 0)),
			boolU8(s.Personalized), boolU8(s.ColdStart), items, ms(s.TS),
			uint64(max(s.ExperimentID, 0)), s.Variant, boolU8(s.Ranker), rules); err != nil {
			_ = b.Abort()
			return err
		}
		if len(s.Features) == len(items) {
			samples += len(items)
		}
	}
	if err := b.Send(); err != nil {
		return err
	}
	if samples == 0 {
		return nil
	}
	sb, err := c.conn.PrepareBatch(ctx, `INSERT INTO rank_samples (app_id, user_id, rec_id, scene, position, work_key, model_version, features, ts)`)
	if err != nil {
		return err
	}
	for _, s := range served {
		if len(s.Features) != len(s.Items) {
			continue
		}
		for k, key := range s.Items {
			f := s.Features[k]
			if f == nil {
				f = []float32{}
			}
			if err := sb.Append(uint64(s.AppID), uint64(s.UserID), s.RecID, s.Scene, uint16(min(k, 65535)), key,
				uint64(max(s.ModelVersion, 0)), f, ms(s.TS)); err != nil {
				_ = sb.Abort()
				return err
			}
		}
	}
	return sb.Send()
}

// ForgetUser 删掉一个人的事件与曝光（轻量删除，立即对查询不可见）。
func (c *CH) ForgetUser(ctx context.Context, appID, userID int64) error {
	for _, table := range []string{"events", "impressions", "rank_samples"} {
		if err := c.conn.Exec(ctx, "DELETE FROM "+table+" WHERE app_id = ? AND user_id = ?", uint64(appID), uint64(userID)); err != nil {
			return fmt.Errorf("删除 %s 失败：%w", table, err)
		}
	}
	return nil
}

func clampU32(v int64) uint32 {
	if v <= 0 {
		return 0
	}
	if v > 1<<32-1 {
		return 1<<32 - 1
	}
	return uint32(v)
}

func boolU8(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}

// SQL 片段：与 signal 包的判据一致（听完 ≥ 80% 或 completed；早跳 < 30 秒且 < 30%）。
const (
	sqlIsPlay     = `(type = 'play_end' AND (played_ms >= 3000 OR reason = 'completed'))`
	sqlIsComplete = `(type = 'play_end' AND (reason = 'completed' OR (duration_ms > 0 AND played_ms >= duration_ms * 0.8)))`
	sqlIsSkip     = `(type = 'play_end' AND reason IN ('skipped', 'switched') AND played_ms < 30000 AND (duration_ms = 0 OR played_ms < duration_ms * 0.3))`
)

// ── 训练读取 ──

// InteractionRow 训练用的一条事件（按用户、时间排序）。
type InteractionRow struct {
	UserID     int64
	WorkKey    string
	Type       string
	TS         int64
	PlayedMs   int64
	DurationMs int64
	PositionMs int64
	Reason     string
	SeekFrom   string
	SessionID  string
	Title      string
	Artist     string
	// 第二期：dislike / undislike 的原因（props.reason / props.scope）与音源版本
	DislikeReason string
	DislikeScope  string
	TrackKey      string
}

// StreamInteractions 按 (user_id, ts) 顺序流式读出窗口内与偏好有关的事件。
func (c *CH) StreamInteractions(ctx context.Context, appID int64, from time.Time, fn func(*InteractionRow) error) error {
	rows, err := c.conn.Query(ctx, `
SELECT user_id, work_key, type, toUnixTimestamp64Milli(ts), played_ms, duration_ms, position_ms, reason,
       props['from'], session_id, title, artist, props['reason'], props['scope'], track_key
FROM events FINAL
WHERE app_id = ? AND ts >= ? AND work_key != ''
  AND type IN ('play_end','like','unlike','dislike','undislike','playlist_add','download','share','search_click','click','seek')
ORDER BY user_id, ts`, uint64(appID), from.UTC())
	if err != nil {
		return err
	}
	defer rows.Close()
	var r InteractionRow
	for rows.Next() {
		var uid uint64
		var played, dur, pos uint32
		if err := rows.Scan(&uid, &r.WorkKey, &r.Type, &r.TS, &played, &dur, &pos, &r.Reason, &r.SeekFrom, &r.SessionID, &r.Title, &r.Artist,
			&r.DislikeReason, &r.DislikeScope, &r.TrackKey); err != nil {
			return err
		}
		r.UserID, r.PlayedMs, r.DurationMs, r.PositionMs = int64(uid), int64(played), int64(dur), int64(pos)
		if err := fn(&r); err != nil {
			return err
		}
	}
	return rows.Err()
}

// ── 画像 ──

// PlayRow 一次播放结束（画像用）。
type PlayRow struct {
	TS       int64
	WorkKey  string
	Track    contract.Track
	PlayedMs int64
	Reason   string
	Platform string
}

// UserPlays 一个人窗口内的全部 play_end。
func (c *CH) UserPlays(ctx context.Context, appID, userID int64, from time.Time) ([]PlayRow, error) {
	rows, err := c.conn.Query(ctx, `
SELECT toUnixTimestamp64Milli(ts), work_key, source, song_id, title, artist, album, cover, duration_ms, played_ms, reason, platform
FROM events FINAL
WHERE app_id = ? AND user_id = ? AND ts >= ? AND type = 'play_end' AND work_key != ''
ORDER BY ts LIMIT 200000`, uint64(appID), uint64(userID), from.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PlayRow
	for rows.Next() {
		var r PlayRow
		var dur, played uint32
		if err := rows.Scan(&r.TS, &r.WorkKey, &r.Track.Source, &r.Track.SongID, &r.Track.Title, &r.Track.Artist, &r.Track.Album,
			&r.Track.Cover, &dur, &played, &r.Reason, &r.Platform); err != nil {
			return nil, err
		}
		r.Track.DurationMs, r.PlayedMs = int64(dur), int64(played)
		out = append(out, r)
	}
	return out, rows.Err()
}

// UserCounts 一个人窗口内的收藏数、以前听过（窗口开始前出现过）的作品集合大小。
func (c *CH) UserCounts(ctx context.Context, appID, userID int64, from time.Time) (likes, oldTracks int64, err error) {
	var l int64
	var o uint64
	err = c.conn.QueryRow(ctx, `
SELECT
  (SELECT countIf(type = 'like') - countIf(type = 'unlike') FROM events FINAL WHERE app_id = ? AND user_id = ? AND ts >= ?),
  (SELECT uniqExact(work_key) FROM events FINAL WHERE app_id = ? AND user_id = ? AND ts < ? AND type = 'play_end'
     AND work_key IN (SELECT work_key FROM events FINAL WHERE app_id = ? AND user_id = ? AND ts >= ? AND type = 'play_end'))`,
		uint64(appID), uint64(userID), from.UTC(), uint64(appID), uint64(userID), from.UTC(), uint64(appID), uint64(userID), from.UTC()).Scan(&l, &o)
	return max(l, 0), int64(o), err
}

// ── 冷启动热度（还没有模型时） ──

// Scored 作品与分数。
type Scored struct {
	WorkKey string
	Score   float64
}

// Popular 近 days 天按听众数排的作品（模型还没训练出来时的兜底热度）。
func (c *CH) Popular(ctx context.Context, appID int64, days, limit int) ([]Scored, error) {
	rows, err := c.conn.Query(ctx, `
SELECT work_key, uniqExact(user_id) AS listeners, countIf(`+sqlIsComplete+`) AS completes, countIf(`+sqlIsPlay+`) AS plays
FROM events FINAL
WHERE app_id = ? AND ts >= now64(3) - toIntervalDay(?) AND type = 'play_end' AND work_key != ''
GROUP BY work_key ORDER BY listeners DESC, completes DESC LIMIT ?`, uint64(appID), days, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Scored
	for rows.Next() {
		var key string
		var listeners, completes, plays uint64
		if err := rows.Scan(&key, &listeners, &completes, &plays); err != nil {
			return nil, err
		}
		rate := (float64(completes) + 1.2) / (float64(plays) + 2)
		out = append(out, Scored{WorkKey: key, Score: float64(listeners) * rate})
	}
	return out, rows.Err()
}
