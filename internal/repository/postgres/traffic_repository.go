package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	plugindomain "aegis/internal/domain/plugin"
	trafficdomain "aegis/internal/domain/traffic"
	"aegis/pkg/traffic"

	"github.com/jackc/pgx/v5"
)

// 流量管理的持久化（migrations/postgres/000148_traffic_management.up.sql）。
//
// 写配置只有一个入口 TrafficPublish：锁住 traffic_settings 那一行，读出当前完整
// 配置，交给调用方的 mutate 改，再整份写回并记一条修订快照。规则表很小（几十条），
// 整份重写换来的是「任意一次修改都是一个完整、可回滚的版本」，并发修改由行锁串行化。

// ErrTrafficRevisionConflict 调用方期望的修订号与库里不一致。
var ErrTrafficRevisionConflict = errors.New("traffic revision conflict")

// TrafficLoadConfig 读当前配置。exists=false 表示从未初始化。
func (r *Repository) TrafficLoadConfig(ctx context.Context) (trafficdomain.ConfigView, bool, error) {
	return r.trafficLoad(ctx, r.pool, false)
}

type pgxQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func (r *Repository) trafficLoad(ctx context.Context, q pgxQuerier, forUpdate bool) (trafficdomain.ConfigView, bool, error) {
	var view trafficdomain.ConfigView
	query := `SELECT revision, settings, updated_by, updated_at FROM traffic_settings WHERE id = 1`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	var raw []byte
	var updatedAt time.Time
	err := q.QueryRow(ctx, query).Scan(&view.Revision, &raw, &view.UpdatedBy, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		view.Settings = traffic.DefaultSettings()
		view.Rules = []traffic.Rule{}
		return view, false, nil
	}
	if err != nil {
		return view, false, err
	}
	view.UpdatedAt = &updatedAt
	settings := traffic.DefaultSettings()
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &settings); err != nil {
			return view, true, fmt.Errorf("decode traffic settings: %w", err)
		}
	}
	view.Settings = traffic.NormalizeSettings(settings)

	rows, err := q.Query(ctx, `SELECT id, spec, updated_by, updated_at FROM traffic_rules ORDER BY position, id`)
	if err != nil {
		return view, true, err
	}
	defer rows.Close()
	view.Rules = []traffic.Rule{}
	for rows.Next() {
		var id string
		var spec []byte
		var by *int64
		var at time.Time
		if err := rows.Scan(&id, &spec, &by, &at); err != nil {
			return view, true, err
		}
		var rule traffic.Rule
		if err := json.Unmarshal(spec, &rule); err != nil {
			continue // 单条坏数据不拖垮整份配置
		}
		rule.ID, rule.UpdatedAt, rule.UpdatedBy = id, at, by
		view.Rules = append(view.Rules, traffic.NormalizeRule(rule))
	}
	return view, true, rows.Err()
}

// TrafficRevision 读当前修订号（轻量，用于各实例对账）。
func (r *Repository) TrafficRevision(ctx context.Context) (int64, error) {
	var rev int64
	err := r.pool.QueryRow(ctx, `SELECT revision FROM traffic_settings WHERE id = 1`).Scan(&rev)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return rev, err
}

// TrafficMutation 一次修改：在 cur 上就地改，返回动作与备注。
type TrafficMutation func(cur *traffic.Config, exists bool) (action string, note string, err error)

// TrafficPublish 串行化地修改并发布一个新修订。expected > 0 时校验当前修订号。
func (r *Repository) TrafficPublish(ctx context.Context, expected int64, actor *int64, mutate TrafficMutation) (trafficdomain.ConfigView, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return trafficdomain.ConfigView{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 先保证那一行存在，才能用行锁串行化（首次初始化时两个实例同时写也安全）。
	if _, err := tx.Exec(ctx, `INSERT INTO traffic_settings (id, revision, settings) VALUES (1, 0, '{}'::jsonb) ON CONFLICT (id) DO NOTHING`); err != nil {
		return trafficdomain.ConfigView{}, err
	}
	cur, _, err := r.trafficLoad(ctx, tx, true)
	if err != nil {
		return trafficdomain.ConfigView{}, err
	}
	exists := cur.Revision > 0
	if expected > 0 && cur.Revision != expected {
		return trafficdomain.ConfigView{}, ErrTrafficRevisionConflict
	}
	cfg := cur.Config
	action, note, err := mutate(&cfg, exists)
	if err != nil {
		return trafficdomain.ConfigView{}, err
	}
	cfg.Revision = cur.Revision + 1
	cfg.Settings = traffic.NormalizeSettings(cfg.Settings)

	settingsJSON, err := json.Marshal(cfg.Settings)
	if err != nil {
		return trafficdomain.ConfigView{}, err
	}
	var updatedAt time.Time
	if err := tx.QueryRow(ctx, `UPDATE traffic_settings SET revision = $1, settings = $2, updated_by = $3, updated_at = NOW() WHERE id = 1 RETURNING updated_at`,
		cfg.Revision, settingsJSON, actor).Scan(&updatedAt); err != nil {
		return trafficdomain.ConfigView{}, err
	}

	// 规则：删掉不在新表里的，其余按新位置 upsert。规则自身的 updatedAt 只在内容变化时刷新。
	ids := make([]string, 0, len(cfg.Rules))
	prevByID := make(map[string]traffic.Rule, len(cur.Rules))
	for _, rr := range cur.Rules {
		prevByID[rr.ID] = rr
	}
	batch := &pgx.Batch{}
	now := time.Now().UTC()
	for i := range cfg.Rules {
		rule := &cfg.Rules[i]
		ids = append(ids, rule.ID)
		prev, had := prevByID[rule.ID]
		if !had || !sameRuleContent(prev, *rule) {
			rule.UpdatedAt, rule.UpdatedBy = now, actor
		} else {
			rule.UpdatedAt, rule.UpdatedBy = prev.UpdatedAt, prev.UpdatedBy
		}
		spec, err := json.Marshal(stripRuleMeta(*rule))
		if err != nil {
			return trafficdomain.ConfigView{}, err
		}
		batch.Queue(`INSERT INTO traffic_rules (id, position, name, enabled, spec, created_by, updated_by, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $6, NOW(), $7)
ON CONFLICT (id) DO UPDATE SET position = EXCLUDED.position, name = EXCLUDED.name, enabled = EXCLUDED.enabled,
	spec = EXCLUDED.spec, updated_by = $8, updated_at = EXCLUDED.updated_at`,
			rule.ID, i, rule.Name, rule.Enabled, spec, actor, rule.UpdatedAt, rule.UpdatedBy)
	}
	batch.Queue(`DELETE FROM traffic_rules WHERE NOT (id = ANY($1))`, ids)
	snapshot, err := json.Marshal(cfg)
	if err != nil {
		return trafficdomain.ConfigView{}, err
	}
	batch.Queue(`INSERT INTO traffic_revisions (revision, action, note, snapshot, rule_count, enabled, created_by, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())`,
		cfg.Revision, action, truncateRunes(note, 200), snapshot, len(cfg.Rules), cfg.Settings.Enabled, actor)
	br := tx.SendBatch(ctx, batch)
	for i := 0; i < batch.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			_ = br.Close()
			return trafficdomain.ConfigView{}, err
		}
	}
	if err := br.Close(); err != nil {
		return trafficdomain.ConfigView{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return trafficdomain.ConfigView{}, err
	}
	return trafficdomain.ConfigView{Config: cfg, UpdatedAt: &updatedAt, UpdatedBy: actor}, nil
}

func stripRuleMeta(r traffic.Rule) traffic.Rule {
	r.UpdatedAt = time.Time{}
	r.UpdatedBy = nil
	return r
}

func sameRuleContent(a, b traffic.Rule) bool {
	ja, _ := json.Marshal(stripRuleMeta(a))
	jb, _ := json.Marshal(stripRuleMeta(b))
	return string(ja) == string(jb)
}

// TrafficRevisions 最近的修订。
func (r *Repository) TrafficRevisions(ctx context.Context, limit int) ([]trafficdomain.RevisionSummary, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `SELECT revision, action, note, rule_count, enabled, created_by, created_at
FROM traffic_revisions ORDER BY revision DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []trafficdomain.RevisionSummary{}
	for rows.Next() {
		var s trafficdomain.RevisionSummary
		if err := rows.Scan(&s.Revision, &s.Action, &s.Note, &s.RuleCount, &s.Enabled, &s.CreatedBy, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// TrafficRevisionDetail 一个修订的完整快照。不存在时返回 nil。
func (r *Repository) TrafficRevisionDetail(ctx context.Context, revision int64) (*trafficdomain.RevisionDetail, error) {
	var d trafficdomain.RevisionDetail
	var snap []byte
	err := r.pool.QueryRow(ctx, `SELECT revision, action, note, rule_count, enabled, created_by, created_at, snapshot
FROM traffic_revisions WHERE revision = $1`, revision).Scan(
		&d.Revision, &d.Action, &d.Note, &d.RuleCount, &d.Enabled, &d.CreatedBy, &d.CreatedAt, &snap)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(snap, &d.Config); err != nil {
		return nil, fmt.Errorf("decode traffic revision %d: %w", revision, err)
	}
	if d.Config.Rules == nil {
		d.Config.Rules = []traffic.Rule{}
	}
	return &d, nil
}

// TrafficInsertMinutes 写一批分钟样本（同一实例同一分钟重复写时累加）。
func (r *Repository) TrafficInsertMinutes(ctx context.Context, batches []trafficdomain.MinuteBatch) error {
	b := &pgx.Batch{}
	for _, mb := range batches {
		for _, s := range mb.Samples {
			c := s.Counts
			b.Queue(`INSERT INTO traffic_metrics_minute (bucket, instance, rule_id, arrivals, admitted, queued, bypassed, observed,
	rejected_rate, rejected_concurrency, rejected_queue, rejected_timeout, rejected_overload, p50_ms, p99_ms, max_inflight, peak_rps)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
ON CONFLICT (bucket, instance, rule_id) DO UPDATE SET
	arrivals = traffic_metrics_minute.arrivals + EXCLUDED.arrivals,
	admitted = traffic_metrics_minute.admitted + EXCLUDED.admitted,
	queued = traffic_metrics_minute.queued + EXCLUDED.queued,
	bypassed = traffic_metrics_minute.bypassed + EXCLUDED.bypassed,
	observed = traffic_metrics_minute.observed + EXCLUDED.observed,
	rejected_rate = traffic_metrics_minute.rejected_rate + EXCLUDED.rejected_rate,
	rejected_concurrency = traffic_metrics_minute.rejected_concurrency + EXCLUDED.rejected_concurrency,
	rejected_queue = traffic_metrics_minute.rejected_queue + EXCLUDED.rejected_queue,
	rejected_timeout = traffic_metrics_minute.rejected_timeout + EXCLUDED.rejected_timeout,
	rejected_overload = traffic_metrics_minute.rejected_overload + EXCLUDED.rejected_overload,
	p50_ms = GREATEST(traffic_metrics_minute.p50_ms, EXCLUDED.p50_ms),
	p99_ms = GREATEST(traffic_metrics_minute.p99_ms, EXCLUDED.p99_ms),
	max_inflight = GREATEST(traffic_metrics_minute.max_inflight, EXCLUDED.max_inflight),
	peak_rps = GREATEST(traffic_metrics_minute.peak_rps, EXCLUDED.peak_rps)`,
				s.Minute, mb.Instance, s.RuleID, c.Arrivals, c.Admitted, c.Queued, c.Bypassed, c.Observed,
				c.RejectedRate, c.RejectedConcurrency, c.RejectedQueue, c.RejectedTimeout, c.RejectedOverload,
				s.P50Ms, s.P99Ms, s.MaxInflight, s.PeakRPS)
		}
	}
	if b.Len() == 0 {
		return nil
	}
	return r.pool.SendBatch(ctx, b).Close()
}

// TrafficHistory 集群合计的历史曲线：先按分钟把各实例相加，再按 step 合并。
// 分位取各实例的最大值（偏保守），峰值 RPS 取分钟内各实例峰值之和的最大值。
func (r *Repository) TrafficHistory(ctx context.Context, from, to time.Time, step time.Duration, ruleID string) ([]trafficdomain.HistoryPoint, traffic.Counts, error) {
	rows, err := r.pool.Query(ctx, `WITH per_minute AS (
	SELECT bucket,
		SUM(arrivals) AS arrivals, SUM(admitted) AS admitted, SUM(queued) AS queued, SUM(observed) AS observed,
		SUM(rejected_rate + rejected_concurrency + rejected_queue + rejected_timeout + rejected_overload) AS rejected,
		SUM(rejected_rate) AS r_rate, SUM(rejected_concurrency) AS r_conc, SUM(rejected_queue) AS r_queue,
		SUM(rejected_timeout) AS r_timeout, SUM(rejected_overload) AS r_overload, SUM(bypassed) AS bypassed,
		MAX(p50_ms) AS p50, MAX(p99_ms) AS p99, SUM(peak_rps) AS peak, SUM(max_inflight) AS inflight
	FROM traffic_metrics_minute
	WHERE rule_id = $1 AND bucket >= $2 AND bucket < $3
	GROUP BY bucket
)
SELECT date_bin($4::interval, bucket, TIMESTAMPTZ '2000-01-01') AS b,
	SUM(arrivals), SUM(admitted), SUM(queued), SUM(rejected), SUM(observed),
	COALESCE(SUM(p50 * arrivals) / NULLIF(SUM(arrivals), 0), MAX(p50)), MAX(p99), MAX(peak), MAX(inflight),
	SUM(r_rate), SUM(r_conc), SUM(r_queue), SUM(r_timeout), SUM(r_overload), SUM(bypassed)
FROM per_minute GROUP BY b ORDER BY b`, ruleID, from, to, fmt.Sprintf("%d seconds", int(step.Seconds())))
	if err != nil {
		return nil, traffic.Counts{}, err
	}
	defer rows.Close()
	points := []trafficdomain.HistoryPoint{}
	var totals traffic.Counts
	for rows.Next() {
		var p trafficdomain.HistoryPoint
		var b time.Time
		var rr, rc, rq, rt, ro, bp int64
		if err := rows.Scan(&b, &p.Arrivals, &p.Admitted, &p.Queued, &p.Rejected, &p.Observed,
			&p.P50, &p.P99, &p.PeakRPS, &p.MaxInflight, &rr, &rc, &rq, &rt, &ro, &bp); err != nil {
			return nil, totals, err
		}
		p.Ts = b.Unix()
		p.P50 = float64(int64(p.P50*100)) / 100
		points = append(points, p)
		totals = totals.Add(traffic.Counts{
			Arrivals: p.Arrivals, Admitted: p.Admitted, Queued: p.Queued, Observed: p.Observed, Bypassed: bp,
			RejectedRate: rr, RejectedConcurrency: rc, RejectedQueue: rq, RejectedTimeout: rt, RejectedOverload: ro,
		})
	}
	return points, totals, rows.Err()
}

// TrafficInsertEvents 写一批事件。
func (r *Repository) TrafficInsertEvents(ctx context.Context, instance string, events []traffic.Event) error {
	if len(events) == 0 {
		return nil
	}
	b := &pgx.Batch{}
	for _, ev := range events {
		detail, _ := json.Marshal(ev.Detail)
		if len(detail) == 0 || string(detail) == "null" {
			detail = []byte("{}")
		}
		b.Queue(`INSERT INTO traffic_events (at, instance, kind, rule_id, rule_name, detail) VALUES ($1,$2,$3,$4,$5,$6)`,
			ev.At, instance, ev.Kind, ev.RuleID, truncateRunes(ev.RuleName, 64), detail)
	}
	return r.pool.SendBatch(ctx, b).Close()
}

// TrafficEvents 最近的事件，kind 为空不过滤（支持前缀，如 "warmup."）。
func (r *Repository) TrafficEvents(ctx context.Context, limit int, kind string) ([]trafficdomain.EventRecord, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `SELECT id, at, instance, kind, rule_id, rule_name, detail FROM traffic_events
WHERE ($1 = '' OR kind LIKE $1 || '%') ORDER BY at DESC, id DESC LIMIT $2`, kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []trafficdomain.EventRecord{}
	for rows.Next() {
		var ev trafficdomain.EventRecord
		var detail []byte
		if err := rows.Scan(&ev.ID, &ev.At, &ev.Instance, &ev.Kind, &ev.RuleID, &ev.RuleName, &detail); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(detail, &ev.Detail)
		out = append(out, ev)
	}
	return out, rows.Err()
}

// TrafficPurge 清理过期的分钟样本与事件。
func (r *Repository) TrafficPurge(ctx context.Context, metricsBefore, eventsBefore time.Time) error {
	if _, err := r.pool.Exec(ctx, `DELETE FROM traffic_metrics_minute WHERE bucket < $1`, metricsBefore); err != nil {
		return err
	}
	_, err := r.pool.Exec(ctx, `DELETE FROM traffic_events WHERE at < $1`, eventsBefore)
	return err
}

// ── 内置插件状态 ──

// LoadNativePlugins 读全部内置插件的启停记录。
func (r *Repository) LoadNativePlugins(ctx context.Context) (map[string]plugindomain.NativeRecord, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, enabled, updated_by, updated_at FROM native_plugins`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]plugindomain.NativeRecord{}
	for rows.Next() {
		var rec plugindomain.NativeRecord
		if err := rows.Scan(&rec.ID, &rec.Enabled, &rec.UpdatedBy, &rec.UpdatedAt); err != nil {
			return nil, err
		}
		out[rec.ID] = rec
	}
	return out, rows.Err()
}

// SaveNativePlugin 记录一个内置插件的启停。
func (r *Repository) SaveNativePlugin(ctx context.Context, id string, enabled bool, actor *int64) (plugindomain.NativeRecord, error) {
	var rec plugindomain.NativeRecord
	err := r.pool.QueryRow(ctx, `INSERT INTO native_plugins (id, enabled, updated_by, updated_at) VALUES ($1, $2, $3, NOW())
ON CONFLICT (id) DO UPDATE SET enabled = EXCLUDED.enabled, updated_by = EXCLUDED.updated_by, updated_at = NOW()
RETURNING id, enabled, updated_by, updated_at`, id, enabled, actor).Scan(&rec.ID, &rec.Enabled, &rec.UpdatedBy, &rec.UpdatedAt)
	return rec, err
}
