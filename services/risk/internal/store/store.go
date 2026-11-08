package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"aegis/services/risk/internal/bundle"
	"aegis/services/risk/internal/lists"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrNotFound 不存在
var ErrNotFound = errors.New("不存在")

// Store 配置库
type Store struct {
	pool *pgxpool.Pool
}

// Open 连库
func Open(ctx context.Context, dsn string) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 16
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool}, nil
}

// Pool 底层连接池
func (s *Store) Pool() *pgxpool.Pool { return s.pool }

// Close 关闭
func (s *Store) Close() { s.pool.Close() }

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// ── 场景 ──

// Scene 场景配置（草稿）
type Scene struct {
	bundle.Scene
	ListTypes []string           `json:"listTypes"`
	Features  []string           `json:"features"`
	Models    []string           `json:"models"`
	Bands     []bundle.ScoreBand `json:"bands"`
	Flow      json.RawMessage    `json:"flow,omitempty"`
	Policy    string             `json:"policy"`
	Builtin   bool               `json:"builtin"`
	Enabled   bool               `json:"enabled"`
	UpdatedAt time.Time          `json:"updatedAt"`
	UpdatedBy string             `json:"updatedBy"`
}

const sceneCols = `key, name, description, app_ids, fields, timeout_ms, fail_mode, captcha_supported, default_action, enforcement,
	list_types, features, models, bands, flow, policy, builtin, enabled, updated_at, updated_by`

func scanScene(row pgx.Row) (*Scene, error) {
	var sc Scene
	var fields, enf, bands []byte
	var flow []byte
	err := row.Scan(&sc.Key, &sc.Name, &sc.Description, &sc.AppIDs, &fields, &sc.TimeoutMS, &sc.FailMode, &sc.CaptchaSupported,
		&sc.DefaultAction, &enf, &sc.ListTypes, &sc.Features, &sc.Models, &bands, &flow, &sc.Policy, &sc.Builtin, &sc.Enabled,
		&sc.UpdatedAt, &sc.UpdatedBy)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(fields, &sc.Fields)
	_ = json.Unmarshal(enf, &sc.Enforcement)
	_ = json.Unmarshal(bands, &sc.Bands)
	if len(flow) > 0 && string(flow) != "null" {
		sc.Flow = flow
	}
	return &sc, nil
}

// ListScenes 全部场景
func (s *Store) ListScenes(ctx context.Context) ([]Scene, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+sceneCols+" FROM scenes ORDER BY builtin DESC, key")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Scene
	for rows.Next() {
		sc, err := scanScene(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *sc)
	}
	return out, rows.Err()
}

// GetScene 一个场景
func (s *Store) GetScene(ctx context.Context, key string) (*Scene, error) {
	sc, err := scanScene(s.pool.QueryRow(ctx, "SELECT "+sceneCols+" FROM scenes WHERE key = $1", key))
	return sc, notFound(err)
}

var sceneKey = regexp.MustCompile(`^[a-z][a-z0-9_]{1,47}$`)

// ValidateScene 保存前校验
func ValidateScene(sc *Scene) error {
	if !sceneKey.MatchString(sc.Key) {
		return errors.New("场景键只能用小写字母、数字、下划线，2-48 位，字母开头")
	}
	if strings.TrimSpace(sc.Name) == "" {
		return errors.New("场景名称为空")
	}
	if sc.TimeoutMS <= 0 {
		sc.TimeoutMS = 200
	}
	if sc.TimeoutMS > 3000 {
		return errors.New("超时预算最多 3000 毫秒")
	}
	if sc.FailMode != "open" && sc.FailMode != "closed" {
		sc.FailMode = "open"
	}
	if sc.DefaultAction != "" && !bundle.ValidAction(sc.DefaultAction) {
		return errors.New("默认处置不合法")
	}
	for _, t := range sc.Enforcement.Targets {
		switch t {
		case "device", "ip", "account", "firewall":
		default:
			return fmt.Errorf("处置对象 %q 不合法", t)
		}
	}
	for _, f := range sc.Fields {
		switch f.Type {
		case bundle.FieldString, bundle.FieldNumber, bundle.FieldBool, bundle.FieldList, bundle.FieldObject:
		default:
			return fmt.Errorf("字段 %s 的类型 %q 不合法", f.Key, f.Type)
		}
	}
	return nil
}

// UpsertScene 保存场景
func (s *Store) UpsertScene(ctx context.Context, sc *Scene, by string) error {
	if err := ValidateScene(sc); err != nil {
		return err
	}
	fields, _ := json.Marshal(nonNil(sc.Fields))
	enf, _ := json.Marshal(sc.Enforcement)
	bands, _ := json.Marshal(nonNil(sc.Bands))
	var flow any
	if len(sc.Flow) > 0 && string(sc.Flow) != "null" {
		flow = []byte(sc.Flow)
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO scenes (key, name, description, app_ids, fields, timeout_ms, fail_mode, captcha_supported, default_action,
		enforcement, list_types, features, models, bands, flow, policy, builtin, enabled, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
		ON CONFLICT (key) DO UPDATE SET name=$2, description=$3, app_ids=$4, fields=$5, timeout_ms=$6, fail_mode=$7, captcha_supported=$8,
		default_action=$9, enforcement=$10, list_types=$11, features=$12, models=$13, bands=$14, flow=$15, policy=$16, enabled=$18,
		updated_at=now(), updated_by=$19`,
		sc.Key, sc.Name, sc.Description, nonNil(sc.AppIDs), fields, sc.TimeoutMS, sc.FailMode, sc.CaptchaSupported, sc.DefaultAction,
		enf, nonNil(sc.ListTypes), nonNil(sc.Features), nonNil(sc.Models), bands, flow, sc.Policy, sc.Builtin, sc.Enabled, by)
	return err
}

// DeleteScene 删场景（内置场景不能删）
func (s *Store) DeleteScene(ctx context.Context, key string) error {
	tag, err := s.pool.Exec(ctx, "DELETE FROM scenes WHERE key = $1 AND NOT builtin", key)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("场景不存在或是内置场景")
	}
	_, err = s.pool.Exec(ctx, "DELETE FROM rules WHERE scene = $1", key)
	return err
}

func nonNil[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

// ── 特征 ──

// Feature 特征（带说明与审计字段）
type Feature struct {
	bundle.Feature
	Description string    `json:"description"`
	UpdatedAt   time.Time `json:"updatedAt"`
	UpdatedBy   string    `json:"updatedBy"`
}

type featureDef struct {
	Velocity *bundle.VelocityDef `json:"velocity,omitempty"`
	Stream   *bundle.StreamDef   `json:"stream,omitempty"`
	Graph    *bundle.GraphDef    `json:"graph,omitempty"`
	Vector   *bundle.VectorDef   `json:"vector,omitempty"`
	Expr     string              `json:"expr,omitempty"`
}

func scanFeature(row pgx.Row) (*Feature, error) {
	var f Feature
	var def []byte
	if err := row.Scan(&f.Key, &f.Name, &f.Description, &f.Kind, &f.Entity, &f.Global, &f.Default, &def, &f.UpdatedAt, &f.UpdatedBy); err != nil {
		return nil, err
	}
	var d featureDef
	_ = json.Unmarshal(def, &d)
	f.Velocity, f.Stream, f.Graph, f.Vector, f.Expr = d.Velocity, d.Stream, d.Graph, d.Vector, d.Expr
	return &f, nil
}

const featureCols = "key, name, description, kind, entity, global, default_value, def, updated_at, updated_by"

// ListFeatures 全部特征
func (s *Store) ListFeatures(ctx context.Context) ([]Feature, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+featureCols+" FROM features ORDER BY kind, key")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Feature
	for rows.Next() {
		f, err := scanFeature(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

// GetFeature 一个特征
func (s *Store) GetFeature(ctx context.Context, key string) (*Feature, error) {
	f, err := scanFeature(s.pool.QueryRow(ctx, "SELECT "+featureCols+" FROM features WHERE key = $1", key))
	return f, notFound(err)
}

// UpsertFeature 保存特征
func (s *Store) UpsertFeature(ctx context.Context, f *Feature, by string) error {
	def, _ := json.Marshal(featureDef{Velocity: f.Velocity, Stream: f.Stream, Graph: f.Graph, Vector: f.Vector, Expr: f.Expr})
	_, err := s.pool.Exec(ctx, `INSERT INTO features (key, name, description, kind, entity, global, default_value, def, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (key) DO UPDATE SET name=$2, description=$3, kind=$4, entity=$5, global=$6, default_value=$7, def=$8, updated_at=now(), updated_by=$9`,
		f.Key, f.Name, f.Description, f.Kind, f.Entity, f.Global, f.Default, def, by)
	return err
}

// DeleteFeature 删特征（被场景选用时拒绝）
func (s *Store) DeleteFeature(ctx context.Context, key string) error {
	var users []string
	rows, err := s.pool.Query(ctx, "SELECT key FROM scenes WHERE $1 = ANY(features)", key)
	if err == nil {
		for rows.Next() {
			var k string
			_ = rows.Scan(&k)
			users = append(users, k)
		}
		rows.Close()
	}
	if len(users) > 0 {
		return fmt.Errorf("特征仍被场景使用：%s", strings.Join(users, "、"))
	}
	_, err = s.pool.Exec(ctx, "DELETE FROM features WHERE key = $1", key)
	return err
}

// ── 规则 ──

// Rule 规则（带场景、开关、审计）
type Rule struct {
	bundle.Rule
	ID          int64     `json:"id"`
	Scene       string    `json:"scene"` // * = 全部场景
	Description string    `json:"description"`
	Enabled     bool      `json:"enabled"`
	TemplateKey string    `json:"templateKey,omitempty"`
	UpdatedAt   time.Time `json:"updatedAt"`
	UpdatedBy   string    `json:"updatedBy"`
}

const ruleCols = "id, scene, key, name, description, dimension, expr, score, mode, decision, priority, reason, tags, enabled, template_key, updated_at, updated_by"

func scanRule(row pgx.Row) (*Rule, error) {
	var r Rule
	err := row.Scan(&r.ID, &r.Scene, &r.Key, &r.Name, &r.Description, &r.Dimension, &r.Expr, &r.Score, &r.Mode, &r.Decision,
		&r.Priority, &r.Reason, &r.Tags, &r.Enabled, &r.TemplateKey, &r.UpdatedAt, &r.UpdatedBy)
	return &r, err
}

// ListRules 场景的规则（含 * 全场景规则）；scene 为空 = 全部
func (s *Store) ListRules(ctx context.Context, scene string) ([]Rule, error) {
	q := "SELECT " + ruleCols + " FROM rules"
	var args []any
	if scene != "" {
		q += " WHERE scene = $1 OR scene = '*'"
		args = append(args, scene)
	}
	rows, err := s.pool.Query(ctx, q+" ORDER BY priority DESC, id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// GetRule 一条规则
func (s *Store) GetRule(ctx context.Context, id int64) (*Rule, error) {
	r, err := scanRule(s.pool.QueryRow(ctx, "SELECT "+ruleCols+" FROM rules WHERE id = $1", id))
	return r, notFound(err)
}

// SaveRule 新建（ID 为 0）或更新
func (s *Store) SaveRule(ctx context.Context, r *Rule, by string) error {
	if r.Mode == "" {
		r.Mode = bundle.ModeEnforce
	}
	if r.Dimension == "" {
		r.Dimension = bundle.DimCustom
	}
	if r.ID == 0 {
		return s.pool.QueryRow(ctx, `INSERT INTO rules (scene, key, name, description, dimension, expr, score, mode, decision, priority, reason, tags, enabled, template_key, updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING id`,
			r.Scene, r.Key, r.Name, r.Description, r.Dimension, r.Expr, r.Score, r.Mode, r.Decision, r.Priority, r.Reason, nonNil(r.Tags),
			r.Enabled, r.TemplateKey, by).Scan(&r.ID)
	}
	tag, err := s.pool.Exec(ctx, `UPDATE rules SET scene=$2, key=$3, name=$4, description=$5, dimension=$6, expr=$7, score=$8, mode=$9, decision=$10,
		priority=$11, reason=$12, tags=$13, enabled=$14, updated_at=now(), updated_by=$15 WHERE id=$1`,
		r.ID, r.Scene, r.Key, r.Name, r.Description, r.Dimension, r.Expr, r.Score, r.Mode, r.Decision, r.Priority, r.Reason, nonNil(r.Tags), r.Enabled, by)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// DeleteRule 删规则
func (s *Store) DeleteRule(ctx context.Context, id int64) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM rules WHERE id = $1", id)
	return err
}

// ── 模型 ──

// Model 模型
type Model struct {
	bundle.Model
	Description string    `json:"description"`
	NFeatures   int       `json:"nFeatures"`
	SizeBytes   int64     `json:"sizeBytes"`
	UpdatedAt   time.Time `json:"updatedAt"`
	UpdatedBy   string    `json:"updatedBy"`
}

const modelCols = "key, name, description, format, object_key, checksum, inputs, transform, n_features, size_bytes, updated_at, updated_by"

func scanModel(row pgx.Row) (*Model, error) {
	var m Model
	err := row.Scan(&m.Key, &m.Name, &m.Description, &m.Format, &m.ObjectKey, &m.Checksum, &m.Inputs, &m.Transform, &m.NFeatures, &m.SizeBytes, &m.UpdatedAt, &m.UpdatedBy)
	return &m, err
}

// ListModels 全部模型
func (s *Store) ListModels(ctx context.Context) ([]Model, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+modelCols+" FROM models ORDER BY key")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Model
	for rows.Next() {
		m, err := scanModel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// GetModel 一个模型
func (s *Store) GetModel(ctx context.Context, key string) (*Model, error) {
	m, err := scanModel(s.pool.QueryRow(ctx, "SELECT "+modelCols+" FROM models WHERE key = $1", key))
	return m, notFound(err)
}

// UpsertModel 保存模型
func (s *Store) UpsertModel(ctx context.Context, m *Model, by string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO models (key, name, description, format, object_key, checksum, inputs, transform, n_features, size_bytes, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (key) DO UPDATE SET name=$2, description=$3, format=$4, object_key=$5, checksum=$6, inputs=$7, transform=$8, n_features=$9,
		size_bytes=$10, updated_at=now(), updated_by=$11`,
		m.Key, m.Name, m.Description, m.Format, m.ObjectKey, m.Checksum, nonNil(m.Inputs), m.Transform, m.NFeatures, m.SizeBytes, by)
	return err
}

// DeleteModel 删模型
func (s *Store) DeleteModel(ctx context.Context, key string) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM models WHERE key = $1", key)
	return err
}

// ── 名单 ──

// ListQuery 名单查询
type ListQuery struct {
	ListType string
	Kind     string
	Value    string // 前缀匹配
	AppID    *int64
	Source   string
	Active   bool
	Page     int
	Limit    int
}

const entryCols = "id, list_type, value, kind, app_id, scenes, reason, source, created_by, decision_id, expires_at, created_at"

func scanEntry(row pgx.Row) (lists.Entry, error) {
	var e lists.Entry
	err := row.Scan(&e.ID, &e.ListType, &e.Value, &e.Kind, &e.AppID, &e.Scenes, &e.Reason, &e.Source, &e.CreatedBy, &e.DecisionID, &e.ExpiresAt, &e.CreatedAt)
	return e, err
}

// ListEntries 分页查名单
func (s *Store) ListEntries(ctx context.Context, q ListQuery) ([]lists.Entry, int64, error) {
	where := []string{"removed_at IS NULL"}
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, strings.ReplaceAll(cond, "?", "$"+strconv.Itoa(len(args))))
	}
	if q.ListType != "" {
		add("list_type = ?", q.ListType)
	}
	if q.Kind != "" {
		add("kind = ?", q.Kind)
	}
	if q.Value != "" {
		add("value LIKE ?", strings.ReplaceAll(strings.ReplaceAll(q.Value, `\`, `\\`), "%", `\%`)+"%")
	}
	if q.AppID != nil {
		// 按应用看名单时，全平台条目（app_id = 0）对这个应用同样生效，一并列出
		add("(app_id = ? OR app_id = 0)", *q.AppID)
	}
	if q.Source != "" {
		add("source = ?", q.Source)
	}
	if q.Active {
		where = append(where, "(expires_at IS NULL OR expires_at > now())")
	}
	cond := strings.Join(where, " AND ")
	var total int64
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM list_entries WHERE "+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 50
	}
	if q.Page <= 0 {
		q.Page = 1
	}
	args = append(args, q.Limit, (q.Page-1)*q.Limit)
	rows, err := s.pool.Query(ctx, fmt.Sprintf("SELECT %s FROM list_entries WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d", entryCols, cond, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []lists.Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

// EntriesFor 某个取值上的有效条目
func (s *Store) EntriesFor(ctx context.Context, listType, value string) ([]lists.Entry, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+entryCols+" FROM list_entries WHERE removed_at IS NULL AND list_type = $1 AND value = $2 AND (expires_at IS NULL OR expires_at > now())", listType, value)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []lists.Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// PutEntries 写入（同 ID 幂等，重放不会重复）
func (s *Store) PutEntries(ctx context.Context, es []lists.Entry) error {
	if len(es) == 0 {
		return nil
	}
	b := &pgx.Batch{}
	for _, e := range es {
		b.Queue(`INSERT INTO list_entries (id, list_type, value, kind, app_id, scenes, reason, source, created_by, decision_id, expires_at, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
			ON CONFLICT (id) DO UPDATE SET kind=$4, scenes=$6, reason=$7, expires_at=$11, removed_at=NULL`,
			e.ID, e.ListType, e.Value, e.Kind, e.AppID, nonNil(e.Scenes), e.Reason, e.Source, e.CreatedBy, e.DecisionID, e.ExpiresAt, e.CreatedAt)
	}
	return s.pool.SendBatch(ctx, b).Close()
}

// RemoveEntries 软删除，返回被删的条目（Redis 索引据此清理）
func (s *Store) RemoveEntries(ctx context.Context, ids []string, by string) ([]lists.Entry, error) {
	rows, err := s.pool.Query(ctx, "UPDATE list_entries SET removed_at = now(), removed_by = $2 WHERE id = ANY($1) AND removed_at IS NULL RETURNING "+entryCols, ids, by)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []lists.Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// EachActiveEntry 遍历全部有效条目（重建 Redis 索引）
func (s *Store) EachActiveEntry(ctx context.Context, fn func([]lists.Entry) error) error {
	last := ""
	for {
		rows, err := s.pool.Query(ctx, "SELECT "+entryCols+" FROM list_entries WHERE removed_at IS NULL AND (expires_at IS NULL OR expires_at > now()) AND id > $1 ORDER BY id LIMIT 1000", last)
		if err != nil {
			return err
		}
		var batch []lists.Entry
		for rows.Next() {
			e, err := scanEntry(rows)
			if err != nil {
				rows.Close()
				return err
			}
			batch = append(batch, e)
		}
		rows.Close()
		if len(batch) == 0 {
			return nil
		}
		if err := fn(batch); err != nil {
			return err
		}
		last = batch[len(batch)-1].ID
	}
}

// ExpiredEntries 已过期还没清理的条目
func (s *Store) ExpiredEntries(ctx context.Context, limit int) ([]lists.Entry, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+entryCols+" FROM list_entries WHERE removed_at IS NULL AND expires_at <= now() ORDER BY expires_at LIMIT $1", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []lists.Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ── CEP 模式 ──

// CEPPattern 模式（带审计）
type CEPPattern struct {
	bundle.CEPPattern
	UpdatedAt time.Time `json:"updatedAt"`
	UpdatedBy string    `json:"updatedBy"`
}

// ListPatterns 全部模式；enabledOnly 只要启用的
func (s *Store) ListPatterns(ctx context.Context, enabledOnly bool) ([]CEPPattern, error) {
	q := "SELECT key, name, description, enabled, key_by, app_ids, steps, within_seconds, severity, actions, updated_at, updated_by FROM cep_patterns"
	if enabledOnly {
		q += " WHERE enabled"
	}
	rows, err := s.pool.Query(ctx, q+" ORDER BY key")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CEPPattern
	for rows.Next() {
		var p CEPPattern
		var steps, actions []byte
		if err := rows.Scan(&p.Key, &p.Name, &p.Description, &p.Enabled, &p.KeyBy, &p.AppIDs, &steps, &p.WithinSeconds, &p.Severity, &actions, &p.UpdatedAt, &p.UpdatedBy); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(steps, &p.Steps)
		_ = json.Unmarshal(actions, &p.Actions)
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpsertPattern 保存模式
func (s *Store) UpsertPattern(ctx context.Context, p *bundle.CEPPattern, by string) error {
	if err := p.Validate(); err != nil {
		return err
	}
	steps, _ := json.Marshal(p.Steps)
	actions, _ := json.Marshal(nonNil(p.Actions))
	_, err := s.pool.Exec(ctx, `INSERT INTO cep_patterns (key, name, description, enabled, key_by, app_ids, steps, within_seconds, severity, actions, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (key) DO UPDATE SET name=$2, description=$3, enabled=$4, key_by=$5, app_ids=$6, steps=$7, within_seconds=$8, severity=$9,
		actions=$10, updated_at=now(), updated_by=$11`,
		p.Key, p.Name, p.Description, p.Enabled, p.KeyBy, nonNil(p.AppIDs), steps, p.WithinSeconds, p.Severity, actions, by)
	return err
}

// DeletePattern 删模式
func (s *Store) DeletePattern(ctx context.Context, key string) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM cep_patterns WHERE key = $1", key)
	return err
}

// PatternsVersion 模式集合的版本（Flink 作业据此判断要不要重载）
func (s *Store) PatternsVersion(ctx context.Context) (string, error) {
	var n int64
	var t *time.Time
	if err := s.pool.QueryRow(ctx, "SELECT count(*), max(updated_at) FROM cep_patterns").Scan(&n, &t); err != nil {
		return "", err
	}
	v := strconv.FormatInt(n, 10)
	if t != nil {
		v += "-" + strconv.FormatInt(t.UnixMicro(), 10)
	}
	return v, nil
}

// ── 发布 ──

// Release 一次发布
type Release struct {
	ID          int64         `json:"id"`
	Scene       string        `json:"scene"`
	Version     string        `json:"version"`
	Bundle      bundle.Bundle `json:"bundle"`
	Checksum    string        `json:"checksum"`
	Note        string        `json:"note"`
	PublishedBy string        `json:"publishedBy"`
	PublishedAt time.Time     `json:"publishedAt"`
}

// State 场景的发布状态
type State struct {
	Scene       string    `json:"scene"`
	PrimaryID   *int64    `json:"primaryId"`
	CandidateID *int64    `json:"candidateId"`
	Canary      int       `json:"canaryPercent"`
	Shadow      bool      `json:"shadow"`
	UpdatedAt   time.Time `json:"updatedAt"`
	UpdatedBy   string    `json:"updatedBy"`
}

// CreateRelease 落一个新版本（版本号按场景递增：v1、v2……）
func (s *Store) CreateRelease(ctx context.Context, b bundle.Bundle, note, by string) (*Release, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('risk_release:' || $1))", b.Scene.Key); err != nil {
		return nil, err
	}
	var n int
	if err := tx.QueryRow(ctx, "SELECT count(*) FROM releases WHERE scene = $1", b.Scene.Key).Scan(&n); err != nil {
		return nil, err
	}
	b.Version = "v" + strconv.Itoa(n+1)
	b.PublishedAt = time.Now().UTC()
	b.PublishedBy = by
	b.Note = note
	b.Checksum = b.ComputeChecksum()
	raw, _ := json.Marshal(b)
	r := &Release{Scene: b.Scene.Key, Version: b.Version, Bundle: b, Checksum: b.Checksum, Note: note, PublishedBy: by, PublishedAt: b.PublishedAt}
	if err := tx.QueryRow(ctx, "INSERT INTO releases (scene, version, bundle, checksum, note, published_by, published_at) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id",
		r.Scene, r.Version, raw, r.Checksum, note, by, r.PublishedAt).Scan(&r.ID); err != nil {
		return nil, err
	}
	return r, tx.Commit(ctx)
}

// GetRelease 一个版本
func (s *Store) GetRelease(ctx context.Context, id int64) (*Release, error) {
	var r Release
	var raw []byte
	err := s.pool.QueryRow(ctx, "SELECT id, scene, version, bundle, checksum, note, published_by, published_at FROM releases WHERE id = $1", id).
		Scan(&r.ID, &r.Scene, &r.Version, &raw, &r.Checksum, &r.Note, &r.PublishedBy, &r.PublishedAt)
	if err != nil {
		return nil, notFound(err)
	}
	_ = json.Unmarshal(raw, &r.Bundle)
	return &r, nil
}

// ListReleases 场景的历史版本（不含发布包正文）
func (s *Store) ListReleases(ctx context.Context, scene string) ([]Release, error) {
	rows, err := s.pool.Query(ctx, "SELECT id, scene, version, checksum, note, published_by, published_at FROM releases WHERE scene = $1 ORDER BY id DESC LIMIT 100", scene)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Release
	for rows.Next() {
		var r Release
		if err := rows.Scan(&r.ID, &r.Scene, &r.Version, &r.Checksum, &r.Note, &r.PublishedBy, &r.PublishedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// GetState 发布状态
func (s *Store) GetState(ctx context.Context, scene string) (*State, error) {
	var st State
	err := s.pool.QueryRow(ctx, "SELECT scene, primary_id, candidate_id, canary_percent, shadow, updated_at, updated_by FROM release_state WHERE scene = $1", scene).
		Scan(&st.Scene, &st.PrimaryID, &st.CandidateID, &st.Canary, &st.Shadow, &st.UpdatedAt, &st.UpdatedBy)
	if err != nil {
		return nil, notFound(err)
	}
	return &st, nil
}

// ListStates 全部场景的发布状态
func (s *Store) ListStates(ctx context.Context) (map[string]State, error) {
	rows, err := s.pool.Query(ctx, "SELECT scene, primary_id, candidate_id, canary_percent, shadow, updated_at, updated_by FROM release_state")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]State{}
	for rows.Next() {
		var st State
		if err := rows.Scan(&st.Scene, &st.PrimaryID, &st.CandidateID, &st.Canary, &st.Shadow, &st.UpdatedAt, &st.UpdatedBy); err != nil {
			return nil, err
		}
		out[st.Scene] = st
	}
	return out, rows.Err()
}

// SetState 写发布状态
func (s *Store) SetState(ctx context.Context, st State, by string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO release_state (scene, primary_id, candidate_id, canary_percent, shadow, updated_by) VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (scene) DO UPDATE SET primary_id=$2, candidate_id=$3, canary_percent=$4, shadow=$5, updated_at=now(), updated_by=$6`,
		st.Scene, st.PrimaryID, st.CandidateID, st.Canary, st.Shadow, by)
	return err
}

// ── 复核 ──

// Case 待复核
type Case struct {
	ID         int64      `json:"id"`
	DecisionID string     `json:"decisionId"`
	AppID      int64      `json:"appId"`
	Scene      string     `json:"scene"`
	UserID     int64      `json:"userId"`
	Account    string     `json:"account"`
	IP         string     `json:"ip"`
	DeviceID   string     `json:"deviceId"`
	Action     string     `json:"action"`
	Score      int        `json:"score"`
	Reasons    []string   `json:"reasons"`
	Source     string     `json:"source"`
	Status     string     `json:"status"`
	Verdict    string     `json:"verdict"`
	Reviewer   string     `json:"reviewer"`
	Note       string     `json:"note"`
	CreatedAt  time.Time  `json:"createdAt"`
	ReviewedAt *time.Time `json:"reviewedAt"`
}

const caseCols = "id, decision_id, app_id, scene, user_id, account, ip, device_id, action, score, reasons, source, status, verdict, reviewer, note, created_at, reviewed_at"

func scanCase(row pgx.Row) (*Case, error) {
	var c Case
	err := row.Scan(&c.ID, &c.DecisionID, &c.AppID, &c.Scene, &c.UserID, &c.Account, &c.IP, &c.DeviceID, &c.Action, &c.Score, &c.Reasons,
		&c.Source, &c.Status, &c.Verdict, &c.Reviewer, &c.Note, &c.CreatedAt, &c.ReviewedAt)
	return &c, err
}

// InsertCases 复核单（决策 ID 唯一，重放不重复）
func (s *Store) InsertCases(ctx context.Context, cs []Case) error {
	if len(cs) == 0 {
		return nil
	}
	b := &pgx.Batch{}
	for _, c := range cs {
		b.Queue(`INSERT INTO cases (decision_id, app_id, scene, user_id, account, ip, device_id, action, score, reasons, source, created_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT (decision_id) DO NOTHING`,
			c.DecisionID, c.AppID, c.Scene, c.UserID, c.Account, c.IP, c.DeviceID, c.Action, c.Score, nonNil(c.Reasons), c.Source, c.CreatedAt)
	}
	return s.pool.SendBatch(ctx, b).Close()
}

// CaseQuery 复核单查询
type CaseQuery struct {
	Status string
	AppID  *int64
	UserID int64
	Scene  string
	Page   int
	Limit  int
}

// ListCases 复核单
func (s *Store) ListCases(ctx context.Context, q CaseQuery) ([]Case, int64, error) {
	where := []string{"true"}
	var args []any
	add := func(cond string, v any) {
		args = append(args, v)
		where = append(where, strings.ReplaceAll(cond, "?", "$"+strconv.Itoa(len(args))))
	}
	if q.Status != "" {
		add("status = ?", q.Status)
	}
	if q.AppID != nil {
		add("app_id = ?", *q.AppID)
	}
	if q.UserID > 0 {
		add("user_id = ?", q.UserID)
	}
	if q.Scene != "" {
		add("scene = ?", q.Scene)
	}
	cond := strings.Join(where, " AND ")
	var total int64
	if err := s.pool.QueryRow(ctx, "SELECT count(*) FROM cases WHERE "+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 50
	}
	if q.Page <= 0 {
		q.Page = 1
	}
	args = append(args, q.Limit, (q.Page-1)*q.Limit)
	rows, err := s.pool.Query(ctx, fmt.Sprintf("SELECT %s FROM cases WHERE %s ORDER BY id DESC LIMIT $%d OFFSET $%d", caseCols, cond, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Case
	for rows.Next() {
		c, err := scanCase(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, *c)
	}
	return out, total, rows.Err()
}

// GetCase 一张复核单
func (s *Store) GetCase(ctx context.Context, id int64) (*Case, error) {
	c, err := scanCase(s.pool.QueryRow(ctx, "SELECT "+caseCols+" FROM cases WHERE id = $1", id))
	return c, notFound(err)
}

// ReviewCase 结案（只有待复核的能结）
func (s *Store) ReviewCase(ctx context.Context, id int64, verdict, note, by string) error {
	tag, err := s.pool.Exec(ctx, "UPDATE cases SET status = 'closed', verdict = $2, note = $3, reviewer = $4, reviewed_at = now() WHERE id = $1 AND status = 'pending'",
		id, verdict, note, by)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("复核单不存在或已结案")
	}
	return nil
}

// ── 杂项 ──

// MarkProcessed 命令幂等：第一次返回 true
func (s *Store) MarkProcessed(ctx context.Context, id string) (bool, error) {
	tag, err := s.pool.Exec(ctx, "INSERT INTO processed_commands (id) VALUES ($1) ON CONFLICT DO NOTHING", id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// Audit 操作留痕
func (s *Store) Audit(ctx context.Context, operator, action, target string, detail any) {
	raw, _ := json.Marshal(detail)
	_, _ = s.pool.Exec(ctx, "INSERT INTO audit_log (operator, action, target, detail) VALUES ($1,$2,$3,$4)", operator, action, target, raw)
}

// AuditEntry 一条操作记录
type AuditEntry struct {
	ID       int64           `json:"id"`
	Operator string          `json:"operator"`
	Action   string          `json:"action"`
	Target   string          `json:"target"`
	Detail   json.RawMessage `json:"detail"`
	At       time.Time       `json:"at"`
}

// ListAudit 最近的操作
func (s *Store) ListAudit(ctx context.Context, limit int) ([]AuditEntry, error) {
	rows, err := s.pool.Query(ctx, "SELECT id, operator, action, target, detail, at FROM audit_log ORDER BY id DESC LIMIT $1", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var a AuditEntry
		if err := rows.Scan(&a.ID, &a.Operator, &a.Action, &a.Target, &a.Detail, &a.At); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ── 编发布包 ──

var refPattern = regexp.MustCompile(`\bF\.([a-z][a-z0-9_]*)|\bF\["([a-z][a-z0-9_]*)"\]|features\.([a-z][a-z0-9_]*)`)

func refs(src string) []string {
	var out []string
	for _, m := range refPattern.FindAllStringSubmatch(src, -1) {
		for _, g := range m[1:] {
			if g != "" {
				out = append(out, g)
			}
		}
	}
	return out
}

// BuildBundle 从草稿编出场景的发布包：场景选用的特征 + 规则 / 派生特征 / 模型 / 决策流里引用到的特征（递归）
func (s *Store) BuildBundle(ctx context.Context, key string) (bundle.Bundle, error) {
	sc, err := s.GetScene(ctx, key)
	if err != nil {
		return bundle.Bundle{}, err
	}
	rules, err := s.ListRules(ctx, key)
	if err != nil {
		return bundle.Bundle{}, err
	}
	all, err := s.ListFeatures(ctx)
	if err != nil {
		return bundle.Bundle{}, err
	}
	byKey := map[string]bundle.Feature{}
	for _, f := range all {
		byKey[f.Key] = f.Feature
	}
	b := bundle.Bundle{Scene: sc.Scene, Bands: sc.Bands, Flow: sc.Flow, Policy: sc.Policy, ListTypes: sc.ListTypes}
	need := map[string]bool{}
	var queue []string
	want := func(k string) {
		if !need[k] {
			need[k] = true
			queue = append(queue, k)
		}
	}
	for _, k := range sc.Features {
		want(k)
	}
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		b.Rules = append(b.Rules, r.Rule)
		for _, k := range refs(r.Expr + " " + r.Reason) {
			want(k)
		}
	}
	for _, k := range refs(string(sc.Flow) + " " + sc.Policy) {
		want(k)
	}
	for _, mk := range sc.Models {
		m, err := s.GetModel(ctx, mk)
		if err != nil {
			return bundle.Bundle{}, fmt.Errorf("模型 %s：%w", mk, err)
		}
		b.Models = append(b.Models, m.Model)
		for _, in := range m.Inputs {
			if _, ok := byKey[in]; ok {
				want(in)
			}
		}
	}
	for len(queue) > 0 {
		k := queue[0]
		queue = queue[1:]
		f, ok := byKey[k]
		if !ok {
			continue // 不存在的特征交给编译时报错
		}
		if f.Kind == bundle.FeatureDerived {
			for _, d := range refs(f.Expr) {
				want(d)
			}
		}
	}
	keys := make([]string, 0, len(need))
	for k := range need {
		if _, ok := byKey[k]; ok {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	// 派生特征排在它依赖的特征之后：先放非派生，再放派生（派生之间按依赖拓扑）
	var base, derived []bundle.Feature
	for _, k := range keys {
		if byKey[k].Kind == bundle.FeatureDerived {
			derived = append(derived, byKey[k])
		} else {
			base = append(base, byKey[k])
		}
	}
	b.Features = append(base, topo(derived)...)
	return b, nil
}

func topo(fs []bundle.Feature) []bundle.Feature {
	idx := map[string]bundle.Feature{}
	for _, f := range fs {
		idx[f.Key] = f
	}
	var out []bundle.Feature
	state := map[string]int{}
	var visit func(k string)
	visit = func(k string) {
		if state[k] != 0 {
			return
		}
		state[k] = 1
		for _, d := range refs(idx[k].Expr) {
			if _, ok := idx[d]; ok {
				visit(d)
			}
		}
		state[k] = 2
		out = append(out, idx[k])
	}
	for _, f := range fs {
		visit(f.Key)
	}
	return out
}
