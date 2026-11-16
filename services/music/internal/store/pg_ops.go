package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// 第二期：用户偏好、运营干预规则、A/B 实验（docs/music-personalization-2.md）。
const pgSchema2 = `
CREATE TABLE IF NOT EXISTS music_user_prefs (
  app_id     BIGINT      NOT NULL,
  user_id    BIGINT      NOT NULL,
  prefs      JSONB       NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (app_id, user_id)
);
CREATE TABLE IF NOT EXISTS music_rules (
  id         BIGSERIAL   PRIMARY KEY,
  app_id     BIGINT      NOT NULL,
  kind       TEXT        NOT NULL,
  target     JSONB       NOT NULL,
  scenes     TEXT[]      NOT NULL DEFAULT '{}',
  weight     DOUBLE PRECISION NOT NULL DEFAULT 1,
  position   INT         NOT NULL DEFAULT 1,
  starts_at  TIMESTAMPTZ,
  ends_at    TIMESTAMPTZ,
  enabled    BOOLEAN     NOT NULL DEFAULT true,
  note       TEXT        NOT NULL DEFAULT '',
  operator   TEXT        NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS music_rules_app ON music_rules (app_id, id DESC);
CREATE TABLE IF NOT EXISTS music_experiments (
  id          BIGSERIAL   PRIMARY KEY,
  app_id      BIGINT      NOT NULL,
  name        TEXT        NOT NULL,
  description TEXT        NOT NULL DEFAULT '',
  scenes      TEXT[]      NOT NULL DEFAULT '{}',
  status      TEXT        NOT NULL DEFAULT 'draft',
  variants    JSONB       NOT NULL DEFAULT '[]',
  started_at  TIMESTAMPTZ,
  stopped_at  TIMESTAMPTZ,
  operator    TEXT        NOT NULL DEFAULT '',
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS music_experiments_app ON music_experiments (app_id, id DESC);
ALTER TABLE music_model_runs ADD COLUMN IF NOT EXISTS ranker JSONB;
`

// RankerInfo 一次训练的精排结果（控制台模型详情）。
type RankerInfo struct {
	Enabled     bool            `json:"enabled"`
	Samples     int             `json:"samples"`
	Positives   int             `json:"positives"`
	AUC         float64         `json:"auc"`
	BaselineAUC float64         `json:"baselineAuc"`
	Features    []FeatureWeight `json:"features"`
	Reason      string          `json:"reason"`
}

// FeatureWeight 特征与标准化后的权重。
type FeatureWeight struct {
	Name   string  `json:"name"`
	Weight float64 `json:"weight"`
}

// ── 用户偏好 ──

// Prefs 用户的音乐偏好（多端同步）。
type Prefs struct {
	Personalized bool     `json:"personalized"`
	Languages    []string `json:"languages"`
	Artists      []string `json:"artists"`
	Autoplay     bool     `json:"autoplay"`
	Onboarded    bool     `json:"onboarded"`
	Skipped      bool     `json:"skipped,omitempty"` // 新用户偏好选择点了跳过（不再弹）
	UpdatedAt    int64    `json:"updatedAt"`
}

// DefaultPrefs 没设置过时的偏好。
func DefaultPrefs() Prefs {
	return Prefs{Personalized: true, Languages: []string{}, Artists: []string{}, Autoplay: true}
}

// UserPrefs 读偏好；没设置过返回默认值与 false。
func (p *PG) UserPrefs(ctx context.Context, appID, userID int64) (Prefs, bool, error) {
	var raw []byte
	var at time.Time
	err := p.Pool.QueryRow(ctx, `SELECT prefs, updated_at FROM music_user_prefs WHERE app_id = $1 AND user_id = $2`, appID, userID).Scan(&raw, &at)
	if errors.Is(err, pgx.ErrNoRows) {
		return DefaultPrefs(), false, nil
	}
	if err != nil {
		return DefaultPrefs(), false, err
	}
	out := DefaultPrefs()
	if err := json.Unmarshal(raw, &out); err != nil {
		return DefaultPrefs(), false, err
	}
	if out.Languages == nil {
		out.Languages = []string{}
	}
	if out.Artists == nil {
		out.Artists = []string{}
	}
	out.UpdatedAt = at.UnixMilli()
	return out, true, nil
}

// SaveUserPrefs 整体写入偏好。
func (p *PG) SaveUserPrefs(ctx context.Context, appID, userID int64, prefs Prefs) (Prefs, error) {
	prefs.UpdatedAt = 0
	raw, _ := json.Marshal(prefs)
	var at time.Time
	err := p.Pool.QueryRow(ctx, `
INSERT INTO music_user_prefs (app_id, user_id, prefs, updated_at) VALUES ($1, $2, $3, now())
ON CONFLICT (app_id, user_id) DO UPDATE SET prefs = EXCLUDED.prefs, updated_at = now()
RETURNING updated_at`, appID, userID, raw).Scan(&at)
	prefs.UpdatedAt = at.UnixMilli()
	return prefs, err
}

// DeleteUserPrefs 清除听歌数据时一起删。
func (p *PG) DeleteUserPrefs(ctx context.Context, appID, userID int64) error {
	_, err := p.Pool.Exec(ctx, `DELETE FROM music_user_prefs WHERE app_id = $1 AND user_id = $2`, appID, userID)
	return err
}

// ── 运营干预规则 ──

// RuleTarget 规则作用的对象。
type RuleTarget struct {
	Type    string `json:"type"` // work | artist
	WorkKey string `json:"workKey,omitempty"`
	Artist  string `json:"artist,omitempty"`
	Title   string `json:"title,omitempty"` // 作品的歌名（展示用，保存时由服务端补）
}

// Rule 一条运营干预规则。
type Rule struct {
	ID        int64      `json:"id"`
	AppID     int64      `json:"-"`
	Kind      string     `json:"kind"` // block | boost | pin
	Target    RuleTarget `json:"target"`
	Scenes    []string   `json:"scenes"`
	Weight    float64    `json:"weight"`
	Position  int        `json:"position"`
	StartsAt  *int64     `json:"startsAt"`
	EndsAt    *int64     `json:"endsAt"`
	Enabled   bool       `json:"enabled"`
	Note      string     `json:"note"`
	Operator  string     `json:"operator"`
	CreatedAt int64      `json:"createdAt"`
	UpdatedAt int64      `json:"updatedAt"`
}

// Active 此刻是否生效。
func (r *Rule) Active(now time.Time) bool {
	ms := now.UnixMilli()
	return r.Enabled && (r.StartsAt == nil || *r.StartsAt <= ms) && (r.EndsAt == nil || *r.EndsAt > ms)
}

const ruleColumns = `id, app_id, kind, target, scenes, weight, position, starts_at, ends_at, enabled, note, operator, created_at, updated_at`

func scanRule(row pgx.Row) (*Rule, error) {
	var r Rule
	var target []byte
	var starts, ends *time.Time
	var created, updated time.Time
	if err := row.Scan(&r.ID, &r.AppID, &r.Kind, &target, &r.Scenes, &r.Weight, &r.Position, &starts, &ends, &r.Enabled,
		&r.Note, &r.Operator, &created, &updated); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(target, &r.Target)
	r.StartsAt, r.EndsAt = millis(starts), millis(ends)
	r.CreatedAt, r.UpdatedAt = created.UnixMilli(), updated.UnixMilli()
	if r.Scenes == nil {
		r.Scenes = []string{}
	}
	return &r, nil
}

// Rules 某应用的全部规则（新在前）。
func (p *PG) Rules(ctx context.Context, appID int64) ([]Rule, error) {
	rows, err := p.Pool.Query(ctx, `SELECT `+ruleColumns+` FROM music_rules WHERE app_id = $1 ORDER BY id DESC LIMIT 1000`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Rule{}
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// Rule 一条规则。
func (p *PG) Rule(ctx context.Context, appID, id int64) (*Rule, error) {
	r, err := scanRule(p.Pool.QueryRow(ctx, `SELECT `+ruleColumns+` FROM music_rules WHERE app_id = $1 AND id = $2`, appID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return r, err
}

func tsOrNil(v *int64) *time.Time {
	if v == nil || *v <= 0 {
		return nil
	}
	t := time.UnixMilli(*v)
	return &t
}

// SaveRule 新建（ID 为 0）或修改。
func (p *PG) SaveRule(ctx context.Context, r Rule) (*Rule, error) {
	target, _ := json.Marshal(r.Target)
	if r.Scenes == nil {
		r.Scenes = []string{}
	}
	if r.ID == 0 {
		return scanRule(p.Pool.QueryRow(ctx, `
INSERT INTO music_rules (app_id, kind, target, scenes, weight, position, starts_at, ends_at, enabled, note, operator)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING `+ruleColumns,
			r.AppID, r.Kind, target, r.Scenes, r.Weight, r.Position, tsOrNil(r.StartsAt), tsOrNil(r.EndsAt), r.Enabled, r.Note, r.Operator))
	}
	out, err := scanRule(p.Pool.QueryRow(ctx, `
UPDATE music_rules SET kind = $3, target = $4, scenes = $5, weight = $6, position = $7, starts_at = $8, ends_at = $9,
  enabled = $10, note = $11, operator = $12, updated_at = now()
WHERE app_id = $1 AND id = $2 RETURNING `+ruleColumns,
		r.AppID, r.ID, r.Kind, target, r.Scenes, r.Weight, r.Position, tsOrNil(r.StartsAt), tsOrNil(r.EndsAt), r.Enabled, r.Note, r.Operator))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return out, err
}

// DeleteRule 删除。
func (p *PG) DeleteRule(ctx context.Context, appID, id int64) error {
	tag, err := p.Pool.Exec(ctx, `DELETE FROM music_rules WHERE app_id = $1 AND id = $2`, appID, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// ── A/B 实验 ──

// Overrides 变体覆盖的参数（nil = 用应用配置）。
type Overrides struct {
	Blend *struct {
		ALS      *float64 `json:"als,omitempty"`
		ItemCF   *float64 `json:"itemcf,omitempty"`
		Artist   *float64 `json:"artist,omitempty"`
		Trending *float64 `json:"trending,omitempty"`
		Explore  *float64 `json:"explore,omitempty"`
	} `json:"blend,omitempty"`
	ExploreRatio *float64 `json:"exploreRatio,omitempty"`
	ArtistCap    *int     `json:"artistCap,omitempty"`
	Ranker       *bool    `json:"ranker,omitempty"`
	FoldIn       *bool    `json:"foldIn,omitempty"`
	ModelRunID   *int64   `json:"modelRunId,omitempty"`
}

// ExpVariant 实验的一个变体。
type ExpVariant struct {
	Key       string    `json:"key"`
	Name      string    `json:"name"`
	Traffic   int       `json:"traffic"`
	Overrides Overrides `json:"overrides"`
}

// Experiment A/B 实验。
type Experiment struct {
	ID          int64        `json:"id"`
	AppID       int64        `json:"-"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Scenes      []string     `json:"scenes"`
	Status      string       `json:"status"` // draft | running | stopped
	Variants    []ExpVariant `json:"variants"`
	StartedAt   *int64       `json:"startedAt"`
	StoppedAt   *int64       `json:"stoppedAt"`
	Operator    string       `json:"operator"`
	CreatedAt   int64        `json:"createdAt"`
	UpdatedAt   int64        `json:"updatedAt"`
}

// 实验状态。
const (
	ExpDraft   = "draft"
	ExpRunning = "running"
	ExpStopped = "stopped"
)

// ErrConflict 状态冲突（同一场景已有运行中的实验、运行中不能删等）。
var ErrConflict = errors.New("conflict")

const expColumns = `id, app_id, name, description, scenes, status, variants, started_at, stopped_at, operator, created_at, updated_at`

func scanExp(row pgx.Row) (*Experiment, error) {
	var e Experiment
	var variants []byte
	var started, stopped *time.Time
	var created, updated time.Time
	if err := row.Scan(&e.ID, &e.AppID, &e.Name, &e.Description, &e.Scenes, &e.Status, &variants, &started, &stopped,
		&e.Operator, &created, &updated); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(variants, &e.Variants)
	if e.Variants == nil {
		e.Variants = []ExpVariant{}
	}
	if e.Scenes == nil {
		e.Scenes = []string{}
	}
	e.StartedAt, e.StoppedAt = millis(started), millis(stopped)
	e.CreatedAt, e.UpdatedAt = created.UnixMilli(), updated.UnixMilli()
	return &e, nil
}

// Experiments 某应用的实验（新在前）；status 为空时全部。
func (p *PG) Experiments(ctx context.Context, appID int64, status string) ([]Experiment, error) {
	rows, err := p.Pool.Query(ctx, `SELECT `+expColumns+` FROM music_experiments
WHERE app_id = $1 AND ($2 = '' OR status = $2) ORDER BY id DESC LIMIT 500`, appID, status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Experiment{}
	for rows.Next() {
		e, err := scanExp(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

// Experiment 一个实验。
func (p *PG) Experiment(ctx context.Context, appID, id int64) (*Experiment, error) {
	e, err := scanExp(p.Pool.QueryRow(ctx, `SELECT `+expColumns+` FROM music_experiments WHERE app_id = $1 AND id = $2`, appID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return e, err
}

// SaveExperiment 新建或修改（只改名称、描述、场景、变体；状态走 Start / Stop）。运行中的实验不能改变体与场景。
func (p *PG) SaveExperiment(ctx context.Context, e Experiment) (*Experiment, error) {
	variants, _ := json.Marshal(e.Variants)
	if e.Scenes == nil {
		e.Scenes = []string{}
	}
	if e.ID == 0 {
		return scanExp(p.Pool.QueryRow(ctx, `
INSERT INTO music_experiments (app_id, name, description, scenes, variants, operator) VALUES ($1,$2,$3,$4,$5,$6)
RETURNING `+expColumns, e.AppID, e.Name, e.Description, e.Scenes, variants, e.Operator))
	}
	cur, err := p.Experiment(ctx, e.AppID, e.ID)
	if err != nil {
		return nil, err
	}
	if cur.Status == ExpRunning {
		same, _ := json.Marshal(cur.Variants)
		if string(same) != string(variants) || fmt.Sprint(cur.Scenes) != fmt.Sprint(e.Scenes) {
			return nil, fmt.Errorf("%w：运行中的实验不能改变体与场景，先停止", ErrConflict)
		}
	}
	return scanExp(p.Pool.QueryRow(ctx, `
UPDATE music_experiments SET name = $3, description = $4, scenes = $5, variants = $6, operator = $7, updated_at = now()
WHERE app_id = $1 AND id = $2 RETURNING `+expColumns, e.AppID, e.ID, e.Name, e.Description, e.Scenes, variants, e.Operator))
}

// StartExperiment 开始：同一场景同时只能有一个运行中的实验（行锁里校验）。
func (p *PG) StartExperiment(ctx context.Context, appID, id int64, operator string) (*Experiment, error) {
	tx, err := p.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(778802, $1::int)`, int32(appID%2147483647)); err != nil {
		return nil, err
	}
	cur, err := scanExp(tx.QueryRow(ctx, `SELECT `+expColumns+` FROM music_experiments WHERE app_id = $1 AND id = $2`, appID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if cur.Status == ExpRunning {
		return cur, nil
	}
	var clash string
	err = tx.QueryRow(ctx, `SELECT name FROM music_experiments WHERE app_id = $1 AND id <> $2 AND status = 'running' AND scenes && $3 LIMIT 1`,
		appID, id, cur.Scenes).Scan(&clash)
	if err == nil {
		return nil, fmt.Errorf("%w：场景已被运行中的实验「%s」占用", ErrConflict, clash)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	out, err := scanExp(tx.QueryRow(ctx, `
UPDATE music_experiments SET status = 'running', started_at = now(), stopped_at = NULL, operator = $3, updated_at = now()
WHERE app_id = $1 AND id = $2 RETURNING `+expColumns, appID, id, operator))
	if err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}

// StopExperiment 停止。
func (p *PG) StopExperiment(ctx context.Context, appID, id int64, operator string) (*Experiment, error) {
	out, err := scanExp(p.Pool.QueryRow(ctx, `
UPDATE music_experiments SET status = CASE WHEN status = 'running' THEN 'stopped' ELSE status END,
  stopped_at = CASE WHEN status = 'running' THEN now() ELSE stopped_at END, operator = $3, updated_at = now()
WHERE app_id = $1 AND id = $2 RETURNING `+expColumns, appID, id, operator))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return out, err
}

// DeleteExperiment 删除（运行中不能删）。
func (p *PG) DeleteExperiment(ctx context.Context, appID, id int64) error {
	tag, err := p.Pool.Exec(ctx, `DELETE FROM music_experiments WHERE app_id = $1 AND id = $2 AND status <> 'running'`, appID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		if _, e := p.Experiment(ctx, appID, id); e == nil {
			return fmt.Errorf("%w：运行中的实验不能删除，先停止", ErrConflict)
		}
		return ErrNotFound
	}
	return nil
}

// WorkKeysOf 一批音源版本（source:songId）对应的作品键（曲库里没有的不返回）。
func (p *PG) WorkKeysOf(ctx context.Context, appID int64, trackKeys []string) (map[string]string, error) {
	out := map[string]string{}
	if len(trackKeys) == 0 {
		return out, nil
	}
	sources := make([]string, 0, len(trackKeys))
	ids := make([]string, 0, len(trackKeys))
	for _, tk := range trackKeys {
		src, id, ok := strings.Cut(tk, ":")
		if !ok || src == "" || id == "" {
			continue
		}
		sources = append(sources, src)
		ids = append(ids, id)
	}
	rows, err := p.Pool.Query(ctx, `
SELECT v.source, v.song_id, v.work_key FROM music_track_variants v
JOIN unnest($2::text[], $3::text[]) AS q(source, song_id) ON q.source = v.source AND q.song_id = v.song_id
WHERE v.app_id = $1`, appID, sources, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var src, id, key string
		if err := rows.Scan(&src, &id, &key); err != nil {
			return nil, err
		}
		out[src+":"+id] = key
	}
	return out, rows.Err()
}
