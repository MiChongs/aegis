package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"aegis/internal/domain/itemgrant"
	lotterydomain "aegis/internal/domain/lottery"
)

// 物品发放：规则、执行、候选人与单人发放事务（docs/item-grant.md）。

var (
	ErrItemGrantRuleNotFound   = errors.New("item grant: rule not found")
	ErrItemGrantRunNotFound    = errors.New("item grant: run not found")
	ErrItemGrantRecordNotFound = errors.New("item grant: record not found")
	// ErrItemGrantRunBusy 这条规则已经有一次在执行
	ErrItemGrantRunBusy = errors.New("item grant: rule already running")
	// ErrItemGrantRuleHasRecords 有发放记录的规则不能删除（只能归档）
	ErrItemGrantRuleHasRecords = errors.New("item grant: rule has records")
	// ErrItemGrantRecordState 记录的状态不允许这个操作
	ErrItemGrantRecordState = errors.New("item grant: record state does not allow this")
)

// ── 规则 ──

const itemGrantRuleColumns = `r.id, r.appid, r.name, r.description, r.status, r.trigger_kind, r.trigger_event, r.cron,
r.starts_at, r.ends_at, r.graph, r.version, r.next_run_at, r.last_run_at, r.created_by, r.updated_by, r.created_at, r.updated_at`

func scanItemGrantRule(row interface{ Scan(dest ...any) error }) (*itemgrant.Rule, error) {
	var rule itemgrant.Rule
	var graph []byte
	if err := row.Scan(&rule.ID, &rule.AppID, &rule.Name, &rule.Description, &rule.Status, &rule.Trigger, &rule.Event,
		&rule.Cron, &rule.StartsAt, &rule.EndsAt, &graph, &rule.Version, &rule.NextRunAt, &rule.LastRunAt,
		&rule.CreatedBy, &rule.UpdatedBy, &rule.CreatedAt, &rule.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrItemGrantRuleNotFound
		}
		return nil, err
	}
	if err := json.Unmarshal(graph, &rule.Graph); err != nil {
		return nil, fmt.Errorf("item grant: decode graph: %w", err)
	}
	if rule.Graph.Nodes == nil {
		rule.Graph.Nodes = []itemgrant.Node{}
	}
	if rule.Graph.Edges == nil {
		rule.Graph.Edges = []itemgrant.Edge{}
	}
	return &rule, nil
}

// CreateItemGrantRule 新建规则（草稿）。
func (r *Repository) CreateItemGrantRule(ctx context.Context, rule itemgrant.Rule) (*itemgrant.Rule, error) {
	graph, err := json.Marshal(rule.Graph)
	if err != nil {
		return nil, err
	}
	return scanItemGrantRule(r.pool.QueryRow(ctx, `INSERT INTO item_grant_rules AS r
(appid, name, description, status, trigger_kind, trigger_event, cron, starts_at, ends_at, graph, next_run_at, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$12)
RETURNING `+itemGrantRuleColumns,
		rule.AppID, rule.Name, rule.Description, rule.Status, rule.Trigger, rule.Event, rule.Cron, rule.StartsAt, rule.EndsAt,
		graph, rule.NextRunAt, rule.CreatedBy))
}

// UpdateItemGrantRule 更新规则的内容（版本号加一；状态另行修改）。
func (r *Repository) UpdateItemGrantRule(ctx context.Context, rule itemgrant.Rule) (*itemgrant.Rule, error) {
	graph, err := json.Marshal(rule.Graph)
	if err != nil {
		return nil, err
	}
	return scanItemGrantRule(r.pool.QueryRow(ctx, `UPDATE item_grant_rules AS r SET
name = $3, description = $4, trigger_kind = $5, trigger_event = $6, cron = $7, starts_at = $8, ends_at = $9,
graph = $10, next_run_at = $11, updated_by = $12, version = r.version + 1, updated_at = NOW()
WHERE r.appid = $1 AND r.id = $2
RETURNING `+itemGrantRuleColumns,
		rule.AppID, rule.ID, rule.Name, rule.Description, rule.Trigger, rule.Event, rule.Cron, rule.StartsAt, rule.EndsAt,
		graph, rule.NextRunAt, rule.UpdatedBy))
}

// SetItemGrantRuleStatus 改状态（与下一次定时执行的时刻一起改）。
func (r *Repository) SetItemGrantRuleStatus(ctx context.Context, appID, ruleID int64, status string,
	nextRunAt *time.Time, operator string) (*itemgrant.Rule, error) {
	return scanItemGrantRule(r.pool.QueryRow(ctx, `UPDATE item_grant_rules AS r
SET status = $3, next_run_at = $4, updated_by = $5, updated_at = NOW()
WHERE r.appid = $1 AND r.id = $2
RETURNING `+itemGrantRuleColumns, appID, ruleID, status, nextRunAt, operator))
}

// GetItemGrantRule 取一条规则（必须属于这个应用）。
func (r *Repository) GetItemGrantRule(ctx context.Context, appID, ruleID int64) (*itemgrant.Rule, error) {
	return scanItemGrantRule(r.pool.QueryRow(ctx, `SELECT `+itemGrantRuleColumns+`
FROM item_grant_rules r WHERE r.appid = $1 AND r.id = $2`, appID, ruleID))
}

// GetItemGrantRuleByID 按 ID 取规则（后台循环用，不限应用）。
func (r *Repository) GetItemGrantRuleByID(ctx context.Context, ruleID int64) (*itemgrant.Rule, error) {
	return scanItemGrantRule(r.pool.QueryRow(ctx, `SELECT `+itemGrantRuleColumns+`
FROM item_grant_rules r WHERE r.id = $1`, ruleID))
}

// ListItemGrantRules 应用的全部规则（附发放统计与正在进行的执行），新的在前。
func (r *Repository) ListItemGrantRules(ctx context.Context, appID int64, dayStart time.Time) ([]itemgrant.Rule, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+itemGrantRuleColumns+`
FROM item_grant_rules r WHERE r.appid = $1 ORDER BY (r.status = 'archived'), r.updated_at DESC, r.id DESC`, appID)
	if err != nil {
		return nil, err
	}
	rules := []itemgrant.Rule{}
	index := map[int64]int{}
	for rows.Next() {
		rule, err := scanItemGrantRule(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		rule.Stats = &itemgrant.RuleStats{}
		index[rule.ID] = len(rules)
		rules = append(rules, *rule)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(rules) == 0 {
		return rules, err
	}

	statRows, err := r.pool.Query(ctx, `SELECT rule_id, COUNT(*), COUNT(DISTINCT user_id),
COUNT(*) FILTER (WHERE created_at >= $2),
COUNT(*) FILTER (WHERE status = 'pending'), COUNT(*) FILTER (WHERE status = 'failed'),
MAX(created_at) FILTER (WHERE status <> 'failed')
FROM item_grant_records WHERE appid = $1 GROUP BY rule_id`, appID, dayStart)
	if err != nil {
		return nil, err
	}
	for statRows.Next() {
		var ruleID int64
		var stats itemgrant.RuleStats
		if err := statRows.Scan(&ruleID, &stats.Records, &stats.Users, &stats.Today, &stats.Pending, &stats.Failed,
			&stats.LastGrant); err != nil {
			statRows.Close()
			return nil, err
		}
		if i, ok := index[ruleID]; ok {
			rules[i].Stats = &stats
		}
	}
	statRows.Close()
	if err := statRows.Err(); err != nil {
		return nil, err
	}

	runRows, err := r.pool.Query(ctx, `SELECT `+itemGrantRunColumns+` `+itemGrantRunJoins+`
WHERE g.appid = $1 AND g.status = 'running'`, appID)
	if err != nil {
		return nil, err
	}
	defer runRows.Close()
	for runRows.Next() {
		run, err := scanItemGrantRun(runRows, false)
		if err != nil {
			return nil, err
		}
		if i, ok := index[run.RuleID]; ok {
			rules[i].Running = run
		}
	}
	return rules, runRows.Err()
}

// DeleteItemGrantRule 删除规则：有发放记录的拒删（记录是账，删规则会连带删掉），只能归档。
func (r *Repository) DeleteItemGrantRule(ctx context.Context, appID, ruleID int64) error {
	var hasRecords bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM item_grant_records WHERE rule_id = $1)`, ruleID).
		Scan(&hasRecords); err != nil {
		return err
	}
	if hasRecords {
		return ErrItemGrantRuleHasRecords
	}
	tag, err := r.pool.Exec(ctx, `DELETE FROM item_grant_rules WHERE appid = $1 AND id = $2`, appID, ruleID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrItemGrantRuleNotFound
	}
	return nil
}

// ActiveItemGrantEventRules 应用里由这个事件触发、生效中的规则。
func (r *Repository) ActiveItemGrantEventRules(ctx context.Context, appID int64, event string) ([]itemgrant.Rule, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+itemGrantRuleColumns+` FROM item_grant_rules r
WHERE r.appid = $1 AND r.status = 'active' AND r.trigger_kind = 'event' AND r.trigger_event = $2
ORDER BY r.id`, appID, event)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []itemgrant.Rule{}
	for rows.Next() {
		rule, err := scanItemGrantRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rule)
	}
	return out, rows.Err()
}

// DueItemGrantScheduleRules 到点的定时规则（只取 ID 与到点时刻，抢占另行 CAS）。
func (r *Repository) DueItemGrantScheduleRules(ctx context.Context, now time.Time, limit int) ([]itemgrant.Rule, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+itemGrantRuleColumns+` FROM item_grant_rules r
WHERE r.status = 'active' AND r.trigger_kind = 'schedule' AND r.next_run_at IS NOT NULL AND r.next_run_at <= $1
ORDER BY r.next_run_at LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []itemgrant.Rule{}
	for rows.Next() {
		rule, err := scanItemGrantRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rule)
	}
	return out, rows.Err()
}

// AdvanceItemGrantSchedule 抢占一次定时执行：只有 next_run_at 仍是读到的那个值时才推进（多实例只有一个成功）。
func (r *Repository) AdvanceItemGrantSchedule(ctx context.Context, ruleID int64, expected time.Time, next *time.Time) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE item_grant_rules SET next_run_at = $3, last_run_at = NOW()
WHERE id = $1 AND next_run_at = $2 AND status = 'active'`, ruleID, expected, next)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// ── 执行 ──

const itemGrantRunColumns = `g.id, g.appid, g.rule_id, COALESCE(r.name, ''), g.rule_version, g.trigger_kind, g.status,
g.total, g.processed, g.granted, g.skipped, g.failed, g.cursor_user_id, g.error, g.operator, g.started_at, g.heartbeat_at,
g.finished_at, g.graph`

const itemGrantRunJoins = `FROM item_grant_runs g LEFT JOIN item_grant_rules r ON r.id = g.rule_id`

func scanItemGrantRun(row interface{ Scan(dest ...any) error }, withGraph bool) (*itemgrant.Run, error) {
	var run itemgrant.Run
	var graph []byte
	if err := row.Scan(&run.ID, &run.AppID, &run.RuleID, &run.RuleName, &run.RuleVersion, &run.Trigger, &run.Status,
		&run.Total, &run.Processed, &run.Granted, &run.Skipped, &run.Failed, &run.Cursor, &run.Error, &run.Operator,
		&run.StartedAt, &run.HeartbeatAt, &run.FinishedAt, &graph); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrItemGrantRunNotFound
		}
		return nil, err
	}
	if withGraph {
		var snapshot itemgrant.Graph
		if err := json.Unmarshal(graph, &snapshot); err == nil {
			run.Graph = &snapshot
		}
	}
	return &run, nil
}

// CreateItemGrantRun 开始一次执行（同一条规则同时只能有一次）。
func (r *Repository) CreateItemGrantRun(ctx context.Context, run itemgrant.Run, graph itemgrant.Graph) (*itemgrant.Run, error) {
	raw, err := json.Marshal(graph)
	if err != nil {
		return nil, err
	}
	var id int64
	err = r.pool.QueryRow(ctx, `INSERT INTO item_grant_runs (appid, rule_id, rule_version, trigger_kind, graph, total, operator)
VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`, run.AppID, run.RuleID, run.RuleVersion, run.Trigger, raw, run.Total, run.Operator).Scan(&id)
	if err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" {
			return nil, ErrItemGrantRunBusy
		}
		return nil, err
	}
	return r.GetItemGrantRun(ctx, run.AppID, id)
}

// GetItemGrantRun 取一次执行（含画布快照）。
func (r *Repository) GetItemGrantRun(ctx context.Context, appID, runID int64) (*itemgrant.Run, error) {
	return scanItemGrantRun(r.pool.QueryRow(ctx, `SELECT `+itemGrantRunColumns+` `+itemGrantRunJoins+`
WHERE g.appid = $1 AND g.id = $2`, appID, runID), true)
}

// ListItemGrantRuns 执行记录分页，新的在前。
func (r *Repository) ListItemGrantRuns(ctx context.Context, query itemgrant.RunQuery) (*itemgrant.Page[itemgrant.Run], error) {
	where := "g.appid = $1"
	args := []any{query.AppID}
	if query.RuleID > 0 {
		args = append(args, query.RuleID)
		where += " AND g.rule_id = $2"
	}
	page, limit := normalizePage(query.Page, query.Limit)
	result := &itemgrant.Page[itemgrant.Run]{Items: []itemgrant.Run{}, Page: page, Limit: limit}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM item_grant_runs g WHERE `+where, args...).Scan(&result.Total); err != nil {
		return nil, err
	}
	result.TotalPages = int((result.Total + int64(limit) - 1) / int64(limit))
	if result.Total == 0 {
		return result, nil
	}
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, `SELECT `+itemGrantRunColumns+` `+itemGrantRunJoins+` WHERE `+where+`
ORDER BY g.id DESC LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		run, err := scanItemGrantRun(rows, false)
		if err != nil {
			return nil, err
		}
		result.Items = append(result.Items, *run)
	}
	return result, rows.Err()
}

// ItemGrantRunProgress 一批处理完的增量。
type ItemGrantRunProgress struct {
	Processed, Granted, Skipped, Failed int64
	Cursor                              int64
}

// AdvanceItemGrantRun 记一批的进度并续心跳，返回执行此刻的状态（被取消时调用方停下）。
func (r *Repository) AdvanceItemGrantRun(ctx context.Context, runID int64, progress ItemGrantRunProgress) (string, error) {
	var status string
	err := r.pool.QueryRow(ctx, `UPDATE item_grant_runs SET processed = processed + $2, granted = granted + $3,
skipped = skipped + $4, failed = failed + $5, cursor_user_id = GREATEST(cursor_user_id, $6), heartbeat_at = NOW()
WHERE id = $1 RETURNING status`, runID, progress.Processed, progress.Granted, progress.Skipped, progress.Failed,
		progress.Cursor).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrItemGrantRunNotFound
	}
	return status, err
}

// FinishItemGrantRun 结束一次执行（只结束仍在执行的，取消的保持取消）。
func (r *Repository) FinishItemGrantRun(ctx context.Context, runID int64, status, message string) error {
	_, err := r.pool.Exec(ctx, `UPDATE item_grant_runs SET status = $2, error = $3, finished_at = NOW(), heartbeat_at = NOW()
WHERE id = $1 AND status = 'running'`, runID, status, truncateColumn(message, 500))
	return err
}

// CancelItemGrantRun 取消一次执行：正在跑的批次处理完当前这批就停。
func (r *Repository) CancelItemGrantRun(ctx context.Context, appID, runID int64, operator string) (*itemgrant.Run, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE item_grant_runs SET status = 'cancelled', finished_at = NOW(),
error = $3 WHERE appid = $1 AND id = $2 AND status = 'running'`, appID, runID, "由 "+operator+" 取消")
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		if _, err := r.GetItemGrantRun(ctx, appID, runID); err != nil {
			return nil, err
		}
		return nil, ErrItemGrantRecordState
	}
	return r.GetItemGrantRun(ctx, appID, runID)
}

// ClaimStaleItemGrantRuns 接管心跳超时的执行（进程重启或崩溃留下的），续上心跳后由调用方从游标接着跑。
func (r *Repository) ClaimStaleItemGrantRuns(ctx context.Context, staleBefore time.Time, limit int) ([]itemgrant.Run, error) {
	rows, err := r.pool.Query(ctx, `UPDATE item_grant_runs g SET heartbeat_at = NOW()
FROM (SELECT id FROM item_grant_runs WHERE status = 'running' AND heartbeat_at < $1
      ORDER BY id LIMIT $2 FOR UPDATE SKIP LOCKED) stale
WHERE g.id = stale.id
RETURNING g.id`, staleBefore, limit)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]itemgrant.Run, 0, len(ids))
	for _, id := range ids {
		run, err := scanItemGrantRun(r.pool.QueryRow(ctx, `SELECT `+itemGrantRunColumns+` `+itemGrantRunJoins+`
WHERE g.id = $1`, id), true)
		if err != nil {
			return nil, err
		}
		out = append(out, *run)
	}
	return out, nil
}

// ── 候选人 ──

// itemGrantBasicsSelect 基础信息（别名 u = users）：账号、昵称、注册时间、会员、等级、最近活跃、账号可用。
const itemGrantBasicsSelect = `SELECT u.id, u.account, COALESCE(up.nickname, ''), u.created_at,
(COALESCE(u.vip_expire_at > NOW(), FALSE) OR u.vip_lifetime_at IS NOT NULL),
u.vip_lifetime_at IS NOT NULL,
COALESCE(lv.current_level, 1),
GREATEST((SELECT MAX(d.last_seen_at) FROM user_login_devices d WHERE d.appid = u.appid AND d.user_id = u.id),
         (SELECT MAX(o.updated_at) FROM user_online_daily o WHERE o.user_id = u.id)),
` + leaderboardEligibleSQL + `,
` + itemGrantHeldTiersSQL + `
FROM users u
LEFT JOIN user_profiles up ON up.user_id = u.id
LEFT JOIN user_level_records lv ON lv.user_id = u.id AND lv.appid = u.appid`

func scanItemGrantBasics(row interface{ Scan(dest ...any) error }) (itemgrant.Basics, error) {
	var b itemgrant.Basics
	var tiers []byte
	err := row.Scan(&b.UserID, &b.Account, &b.Nickname, &b.RegisteredAt, &b.Member, &b.Lifetime, &b.Level,
		&b.LastActiveAt, &b.Eligible, &tiers)
	if err == nil {
		b.TierIDs, b.LifetimeTierIDs = decodeHeldTiers(tiers)
	}
	return b, err
}

// itemGrantHeldTiersSQL 此刻仍生效的会员段所在的档位：[[档位, 是否永久], …]（与 vipLiveSegmentsSQL 同一口径）。
const itemGrantHeldTiersSQL = `COALESCE((SELECT jsonb_agg(DISTINCT jsonb_build_array(vt.tier_id, vt.lifetime))
  FROM vip_transactions vt
  WHERE ` + itemGrantHeldTierWhere + `), '[]'::jsonb)`

// itemGrantHeldTierWhere 一段仍生效、挂了档位的会员期（别名 vt，外层 u = users）。
const itemGrantHeldTierWhere = `vt.appid = u.appid AND vt.user_id = u.id AND vt.tier_id IS NOT NULL AND ` + vipSegmentScopeSQL + `
    AND ` + vipSegmentUntilSQL + ` > NOW() AND ` + vipSegmentUntilSQL + ` > ` + vipSegmentFromSQL

func decodeHeldTiers(raw []byte) ([]int64, []int64) {
	var pairs [][2]any
	if err := json.Unmarshal(raw, &pairs); err != nil {
		return nil, nil
	}
	var held, lifetime []int64
	for _, pair := range pairs {
		id, ok := pair[0].(float64)
		if !ok {
			continue
		}
		if !slices.Contains(held, int64(id)) {
			held = append(held, int64(id))
		}
		if isLifetime, _ := pair[1].(bool); isLifetime && !slices.Contains(lifetime, int64(id)) {
			lifetime = append(lifetime, int64(id))
		}
	}
	slices.Sort(held)
	slices.Sort(lifetime)
	return held, lifetime
}

// itemGrantAudienceWhere 目标人群的 SQL 条件（与 itemgrant.MatchAudience 同一口径）。
func itemGrantAudienceWhere(audience *itemgrant.AudienceConfig, args *[]any) string {
	add := func(clause string, value any) string {
		*args = append(*args, value)
		return strings.ReplaceAll(clause, "?", "$"+strconv.Itoa(len(*args)))
	}
	where := []string{leaderboardEligibleSQL}
	if audience == nil {
		return strings.Join(where, " AND ")
	}
	switch audience.Scope {
	case itemgrant.ScopeMember:
		where = append(where, "(COALESCE(u.vip_expire_at > NOW(), FALSE) OR u.vip_lifetime_at IS NOT NULL)")
	case itemgrant.ScopeNonMember:
		where = append(where, "NOT (COALESCE(u.vip_expire_at > NOW(), FALSE) OR u.vip_lifetime_at IS NOT NULL)")
	case itemgrant.ScopeLifetime:
		where = append(where, "u.vip_lifetime_at IS NOT NULL")
	case itemgrant.ScopeNonLifetime:
		where = append(where, "u.vip_lifetime_at IS NULL")
	}
	if audience.RegisteredWithinDays > 0 {
		where = append(where, add("u.created_at >= NOW() - make_interval(days => ?)", audience.RegisteredWithinDays))
	}
	if audience.RegisteredBeforeDays > 0 {
		where = append(where, add("u.created_at <= NOW() - make_interval(days => ?)", audience.RegisteredBeforeDays))
	}
	if audience.ActiveWithinDays > 0 {
		where = append(where, add(`(EXISTS (SELECT 1 FROM user_login_devices d WHERE d.appid = u.appid AND d.user_id = u.id
      AND d.last_seen_at >= NOW() - make_interval(days => ?))
   OR EXISTS (SELECT 1 FROM user_online_daily o WHERE o.user_id = u.id AND o.updated_at >= NOW() - make_interval(days => ?)))`,
			audience.ActiveWithinDays))
	}
	if audience.MinLevel > 0 {
		where = append(where, add("COALESCE(lv.current_level, 1) >= ?", audience.MinLevel))
	}
	if audience.MaxLevel > 0 {
		where = append(where, add("COALESCE(lv.current_level, 1) <= ?", audience.MaxLevel))
	}
	if len(audience.UserIDs) > 0 {
		where = append(where, add("u.id = ANY(?)", audience.UserIDs))
	}
	if len(audience.ExcludeUserIDs) > 0 {
		where = append(where, add("NOT (u.id = ANY(?))", audience.ExcludeUserIDs))
	}
	if len(audience.TierIDs) > 0 {
		clause := "EXISTS (SELECT 1 FROM vip_transactions vt WHERE " + itemGrantHeldTierWhere + " AND vt.tier_id = ANY(?)"
		if audience.TierLifetimeOnly {
			clause += " AND vt.lifetime"
		}
		where = append(where, add(clause+")", audience.TierIDs))
	}
	return strings.Join(where, " AND ")
}

// ItemGrantCandidates 批量执行的下一批候选人（按 ID 升序，从 afterID 之后取）。
func (r *Repository) ItemGrantCandidates(ctx context.Context, appID int64, audience *itemgrant.AudienceConfig,
	afterID int64, limit int) ([]itemgrant.Basics, error) {
	args := []any{appID, afterID}
	where := itemGrantAudienceWhere(audience, &args)
	args = append(args, limit)
	return r.queryItemGrantBasics(ctx, itemGrantBasicsSelect+`
WHERE u.appid = $1 AND u.id > $2 AND `+where+` ORDER BY u.id LIMIT $`+strconv.Itoa(len(args)), args...)
}

// SampleItemGrantCandidates 随机抽样候选人（预演用）。
func (r *Repository) SampleItemGrantCandidates(ctx context.Context, appID int64, audience *itemgrant.AudienceConfig,
	limit int) ([]itemgrant.Basics, error) {
	args := []any{appID}
	where := itemGrantAudienceWhere(audience, &args)
	args = append(args, limit)
	return r.queryItemGrantBasics(ctx, itemGrantBasicsSelect+`
WHERE u.appid = $1 AND `+where+` ORDER BY random() LIMIT $`+strconv.Itoa(len(args)), args...)
}

// CountItemGrantCandidates 目标人群的人数。
func (r *Repository) CountItemGrantCandidates(ctx context.Context, appID int64, audience *itemgrant.AudienceConfig) (int64, error) {
	args := []any{appID}
	where := itemGrantAudienceWhere(audience, &args)
	var total int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users u
LEFT JOIN user_level_records lv ON lv.user_id = u.id AND lv.appid = u.appid
WHERE u.appid = $1 AND `+where, args...).Scan(&total)
	return total, err
}

// ItemGrantBasics 一个人的基础信息。
func (r *Repository) ItemGrantBasics(ctx context.Context, appID, userID int64) (itemgrant.Basics, error) {
	basics, err := scanItemGrantBasics(r.pool.QueryRow(ctx, itemGrantBasicsSelect+`
WHERE u.appid = $1 AND u.id = $2`, appID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return basics, ErrUserNotFound
	}
	return basics, err
}

// FindItemGrantUser 按用户 ID 或账号找人（单人预演用）。
func (r *Repository) FindItemGrantUser(ctx context.Context, appID int64, keyword string) (itemgrant.Basics, error) {
	keyword = strings.TrimSpace(keyword)
	if id, err := strconv.ParseInt(keyword, 10, 64); err == nil && id > 0 {
		if basics, err := r.ItemGrantBasics(ctx, appID, id); err == nil {
			return basics, nil
		}
	}
	basics, err := scanItemGrantBasics(r.pool.QueryRow(ctx, itemGrantBasicsSelect+`
WHERE u.appid = $1 AND u.account = $2`, appID, keyword))
	if errors.Is(err, pgx.ErrNoRows) {
		return basics, ErrUserNotFound
	}
	return basics, err
}

func (r *Repository) queryItemGrantBasics(ctx context.Context, sql string, args ...any) ([]itemgrant.Basics, error) {
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []itemgrant.Basics{}
	for rows.Next() {
		basics, err := scanItemGrantBasics(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, basics)
	}
	return out, rows.Err()
}

// ── 判定所需的事实 ──

// itemGrantFacts 判定时的事实：行为画像（事务外预取）与限额用量（事务内、锁住用户之后数）。
type itemGrantFacts struct {
	ctx     context.Context
	q       queryExecutor
	ruleID  int64
	userID  int64
	profile *lotterydomain.BehaviorProfile
	load    func() (lotterydomain.BehaviorProfile, error)
	// lockGlobal 正式发放时，数全站用量前先拿这个限额节点的事务级咨询锁（并发执行时不超发）
	lockGlobal bool
}

func (f *itemGrantFacts) Profile() (lotterydomain.BehaviorProfile, error) {
	if f.profile != nil {
		return *f.profile, nil
	}
	if f.load == nil {
		return lotterydomain.BehaviorProfile{}, nil
	}
	profile, err := f.load()
	if err != nil {
		return profile, err
	}
	f.profile = &profile
	return profile, nil
}

func (f *itemGrantFacts) Usage(nodeID string, since time.Time, needUser, needGlobal bool) (int64, int64, error) {
	var user, global int64
	if needUser {
		if err := f.q.QueryRow(f.ctx, `SELECT COUNT(*) FROM item_grant_records
WHERE rule_id = $1 AND user_id = $2 AND $3 = ANY(limit_nodes) AND status <> 'failed' AND created_at >= $4`,
			f.ruleID, f.userID, nodeID, since).Scan(&user); err != nil {
			return 0, 0, err
		}
	}
	if needGlobal {
		if f.lockGlobal {
			if _, err := f.q.Exec(f.ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`,
				"itemgrant:"+strconv.FormatInt(f.ruleID, 10)+":"+nodeID); err != nil {
				return 0, 0, err
			}
		}
		if err := f.q.QueryRow(f.ctx, `SELECT COUNT(*) FROM item_grant_records
WHERE rule_id = $1 AND $2 = ANY(limit_nodes) AND status <> 'failed' AND created_at >= $3`,
			f.ruleID, nodeID, since).Scan(&global); err != nil {
			return 0, 0, err
		}
	}
	return user, global, nil
}

func itemGrantDayStart(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

// newItemGrantRNG 每次判定一个新的随机源（crypto 播种的 ChaCha8：分流与随机礼包不可预测）。
func newItemGrantRNG() *rand.Rand {
	var seed [32]byte
	for i := range seed {
		seed[i] = byte(rand.Uint32())
	}
	return rand.New(rand.NewChaCha8(seed))
}

// PreviewItemGrant 只判定、不发放（单人预演与抽样预演）：限额用量按此刻的记录数，不加锁。
func (r *Repository) PreviewItemGrant(ctx context.Context, compiled *itemgrant.Compiled, rule itemgrant.Rule,
	basics itemgrant.Basics, eventData map[string]any, now time.Time) (itemgrant.Outcome, error) {
	facts := &itemGrantFacts{ctx: ctx, q: r.pool, ruleID: rule.ID, userID: basics.UserID}
	facts.load = func() (lotterydomain.BehaviorProfile, error) {
		return r.LotteryBehaviorProfile(ctx, rule.AppID, basics.UserID, now, itemGrantDayStart(now))
	}
	return compiled.Evaluate(itemgrant.Subject{
		RuleID: rule.ID, Basics: basics, Trigger: compiled.Trigger.Kind, Event: eventData, Now: now,
	}, facts, newItemGrantRNG())
}

// ── 单人发放事务 ──

// ExecuteItemGrant 给一个人执行一条规则：锁用户 → 判定 → 写记录（占幂等键）→ 逐项发放 → 提交。
//
// 任何一项发不出去（套餐被删、名下没有授权卡……）整笔回滚，另写一条 failed 记录说明原因；
// 同一幂等键只会成功一次（重复的直接返回 duplicate，不再判定）。
func (r *Repository) ExecuteItemGrant(ctx context.Context, input itemgrant.ExecuteInput) (*itemgrant.ExecuteResult, error) {
	loc := input.Location
	if loc == nil {
		loc = time.Local
	}
	now := input.Now.In(loc)

	// 行为画像在事务外取：都是只读的历史事实，不该占着用户行锁跑一串查询
	var profile *lotterydomain.BehaviorProfile
	if input.Compiled.NeedsProfile {
		p, err := r.LotteryBehaviorProfile(ctx, input.AppID, input.UserID, now, itemGrantDayStart(now))
		if err != nil {
			return nil, err
		}
		profile = &p
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if err := lockUserTx(ctx, tx, input.AppID, input.UserID); err != nil {
		return nil, err
	}
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM item_grant_records WHERE rule_id = $1 AND dedupe_key = $2)`,
		input.Rule.ID, input.DedupeKey).Scan(&exists); err != nil {
		return nil, err
	}
	if exists {
		return &itemgrant.ExecuteResult{Status: itemgrant.ExecDuplicate}, nil
	}
	basics, err := scanItemGrantBasics(tx.QueryRow(ctx, itemGrantBasicsSelect+` WHERE u.appid = $1 AND u.id = $2`,
		input.AppID, input.UserID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	facts := &itemGrantFacts{ctx: ctx, q: tx, ruleID: input.Rule.ID, userID: input.UserID, profile: profile, lockGlobal: true}
	outcome, err := input.Compiled.Evaluate(itemgrant.Subject{
		RuleID: input.Rule.ID, Basics: basics, Trigger: input.Trigger, Event: input.EventData, Now: now,
	}, facts, newItemGrantRNG())
	if err != nil {
		return nil, err
	}
	if !outcome.Granting() {
		return &itemgrant.ExecuteResult{Status: itemgrant.ExecSkipped, Outcome: outcome}, nil
	}

	status := itemgrant.RecordGranted
	for _, planned := range outcome.Items {
		if spec, ok := itemgrant.FindItemSpec(planned.Item.Type); ok && spec.Pending {
			status = itemgrant.RecordPending
		}
	}
	recordNo := generateTransactionNo("IG")
	record := itemGrantRecordRow{
		RecordNo: recordNo, AppID: input.AppID, RuleID: input.Rule.ID, RuleVersion: input.Rule.Version, RunID: input.RunID,
		UserID: input.UserID, Trigger: input.Trigger, Event: input.Event, DedupeKey: input.DedupeKey, Status: status,
		Steps: outcome.Steps, Items: outcome.Items, Limits: outcome.Limits, EventData: input.EventData,
	}
	recordID, inserted, err := insertItemGrantRecord(ctx, tx, record)
	if err != nil {
		return nil, err
	}
	if !inserted {
		return &itemgrant.ExecuteResult{Status: itemgrant.ExecDuplicate}, nil
	}

	refs, err := itemGrantRefsQ(ctx, tx, input.AppID)
	if err != nil {
		return nil, err
	}
	grant := itemGrantContext{
		AppID: input.AppID, UserID: input.UserID, RecordID: recordID, RecordNo: recordNo,
		Title: input.Rule.Name, Source: "物品发放 " + input.Rule.Name, Refs: refs, Now: input.Now,
		Meta: map[string]any{"itemGrantRule": input.Rule.ID, "itemGrantRecord": recordNo},
	}
	results := make([]itemgrant.ItemResult, 0, len(outcome.Items))
	for i, planned := range outcome.Items {
		result, err := r.grantItemTx(ctx, tx, grant, i, planned)
		if err != nil {
			_ = tx.Rollback(ctx)
			tx = nil
			return r.recordItemGrantFailure(ctx, record, outcome, err)
		}
		results = append(results, result)
	}
	rawResults, _ := json.Marshal(results)
	if _, err := tx.Exec(ctx, `UPDATE item_grant_records SET results = $2 WHERE id = $1`, recordID, rawResults); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	saved, err := r.GetItemGrantRecord(ctx, input.AppID, recordID)
	if err != nil {
		return nil, err
	}
	return &itemgrant.ExecuteResult{Status: status, Outcome: outcome, Record: saved}, nil
}

// recordItemGrantFailure 发放回滚之后写一条 failed 记录（占住幂等键，等管理员重试）。
func (r *Repository) recordItemGrantFailure(ctx context.Context, record itemGrantRecordRow, outcome itemgrant.Outcome,
	cause error) (*itemgrant.ExecuteResult, error) {
	message := itemGrantFailureMessage(cause)
	record.Status = itemgrant.RecordFailed
	record.Error = message
	record.RecordNo = generateTransactionNo("IG")
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	recordID, inserted, err := insertItemGrantRecord(ctx, tx, record)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	result := &itemgrant.ExecuteResult{Status: itemgrant.RecordFailed, Outcome: outcome, Error: message}
	if inserted {
		if saved, err := r.GetItemGrantRecord(ctx, record.AppID, recordID); err == nil {
			result.Record = saved
		}
	}
	return result, nil
}

// itemGrantFailureMessage 把发放失败翻成管理员看得懂的一句话。
func itemGrantFailureMessage(err error) string {
	switch {
	case errors.Is(err, ErrItemGrantRefMissing):
		return "引用的会员套餐或功能不存在、已停用或是试用套餐：" + err.Error()
	case errors.Is(err, ErrCardKeyNoLoginCard):
		return "名下没有可续期的授权卡，授权时长 / 设备位发不出去"
	case errors.Is(err, ErrCardKeyLicensePermanent):
		return "名下只有永久授权卡，授权时长没有意义"
	case errors.Is(err, ErrUserNotFound):
		return "用户不存在"
	}
	return truncateColumn(err.Error(), 300)
}

type itemGrantRecordRow struct {
	RecordNo    string
	AppID       int64
	RuleID      int64
	RuleVersion int
	RunID       *int64
	UserID      int64
	Trigger     string
	Event       string
	DedupeKey   string
	Status      string
	Steps       []itemgrant.Step
	Items       []itemgrant.PlannedItem
	Limits      []string
	EventData   map[string]any
	Error       string
}

func insertItemGrantRecord(ctx context.Context, tx pgx.Tx, row itemGrantRecordRow) (int64, bool, error) {
	steps, _ := json.Marshal(row.Steps)
	items, _ := json.Marshal(row.Items)
	var eventData []byte
	if len(row.EventData) > 0 {
		eventData, _ = json.Marshal(row.EventData)
	}
	limits := row.Limits
	if limits == nil {
		limits = []string{}
	}
	var id int64
	err := tx.QueryRow(ctx, `INSERT INTO item_grant_records
(record_no, appid, rule_id, rule_version, run_id, user_id, trigger_kind, trigger_event, dedupe_key, status, steps, items,
 limit_nodes, event_data, error)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
ON CONFLICT (rule_id, dedupe_key) DO NOTHING RETURNING id`,
		row.RecordNo, row.AppID, row.RuleID, row.RuleVersion, row.RunID, row.UserID, row.Trigger, row.Event, row.DedupeKey,
		row.Status, steps, items, limits, eventData, row.Error).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	return id, err == nil, err
}

// ── 记录 ──

const itemGrantRecordColumns = `d.id, d.record_no, d.appid, d.rule_id, COALESCE(r.name, ''), d.rule_version, d.run_id, d.user_id,
COALESCE(u.account, ''), COALESCE(up.nickname, ''), d.trigger_kind, d.trigger_event, d.status, d.steps, d.items, d.results,
d.error, d.event_data, d.note, d.handled_by, d.handled_at, d.created_at`

const itemGrantRecordJoins = `FROM item_grant_records d
LEFT JOIN item_grant_rules r ON r.id = d.rule_id
LEFT JOIN users u ON u.id = d.user_id
LEFT JOIN user_profiles up ON up.user_id = d.user_id`

func scanItemGrantRecord(row interface{ Scan(dest ...any) error }) (*itemgrant.Record, error) {
	var record itemgrant.Record
	var steps, items, results, eventData []byte
	if err := row.Scan(&record.ID, &record.RecordNo, &record.AppID, &record.RuleID, &record.RuleName, &record.RuleVersion,
		&record.RunID, &record.UserID, &record.Account, &record.Nickname, &record.Trigger, &record.Event, &record.Status,
		&steps, &items, &results, &record.Error, &eventData, &record.Note, &record.HandledBy, &record.HandledAt,
		&record.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrItemGrantRecordNotFound
		}
		return nil, err
	}
	record.Steps, record.Items, record.Results = []itemgrant.Step{}, []itemgrant.PlannedItem{}, []itemgrant.ItemResult{}
	_ = json.Unmarshal(steps, &record.Steps)
	_ = json.Unmarshal(items, &record.Items)
	_ = json.Unmarshal(results, &record.Results)
	if len(eventData) > 0 {
		_ = json.Unmarshal(eventData, &record.EventData)
	}
	return &record, nil
}

// GetItemGrantRecord 取一条记录（必须属于这个应用）。
func (r *Repository) GetItemGrantRecord(ctx context.Context, appID, recordID int64) (*itemgrant.Record, error) {
	return scanItemGrantRecord(r.pool.QueryRow(ctx, `SELECT `+itemGrantRecordColumns+` `+itemGrantRecordJoins+`
WHERE d.appid = $1 AND d.id = $2`, appID, recordID))
}

// ListItemGrantRecords 发放记录分页，新的在前。
func (r *Repository) ListItemGrantRecords(ctx context.Context, query itemgrant.RecordQuery) (*itemgrant.Page[itemgrant.Record], error) {
	where := []string{"d.appid = $1"}
	args := []any{query.AppID}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, strings.ReplaceAll(clause, "?", "$"+strconv.Itoa(len(args))))
	}
	if query.RuleID > 0 {
		add("d.rule_id = ?", query.RuleID)
	}
	if query.RunID > 0 {
		add("d.run_id = ?", query.RunID)
	}
	if query.UserID > 0 {
		add("d.user_id = ?", query.UserID)
	}
	if query.Status != "" {
		add("d.status = ?", query.Status)
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		add("(d.record_no = ? OR u.account ILIKE '%' || ? || '%' OR up.nickname ILIKE '%' || ? || '%')", keyword)
	}
	if query.Start != nil {
		add("d.created_at >= ?", *query.Start)
	}
	if query.End != nil {
		add("d.created_at < ?", *query.End)
	}
	whereSQL := strings.Join(where, " AND ")
	page, limit := normalizePage(query.Page, query.Limit)
	result := &itemgrant.Page[itemgrant.Record]{Items: []itemgrant.Record{}, Page: page, Limit: limit}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) `+itemGrantRecordJoins+` WHERE `+whereSQL, args...).Scan(&result.Total); err != nil {
		return nil, err
	}
	result.TotalPages = int((result.Total + int64(limit) - 1) / int64(limit))
	if result.Total == 0 {
		return result, nil
	}
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, `SELECT `+itemGrantRecordColumns+` `+itemGrantRecordJoins+` WHERE `+whereSQL+`
ORDER BY d.created_at DESC, d.id DESC LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		record, err := scanItemGrantRecord(rows)
		if err != nil {
			return nil, err
		}
		result.Items = append(result.Items, *record)
	}
	return result, rows.Err()
}

// FulfillItemGrantRecord 把一条待发放（含人工发放物品）的记录标记为已发放。
func (r *Repository) FulfillItemGrantRecord(ctx context.Context, input itemgrant.HandleInput) (*itemgrant.Record, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE item_grant_records SET status = 'fulfilled', note = $3, handled_by = $4, handled_at = NOW()
WHERE appid = $1 AND id = $2 AND status = 'pending'`, input.AppID, input.RecordID, truncateColumn(input.Note, 200), input.Operator)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		if _, err := r.GetItemGrantRecord(ctx, input.AppID, input.RecordID); err != nil {
			return nil, err
		}
		return nil, ErrItemGrantRecordState
	}
	return r.GetItemGrantRecord(ctx, input.AppID, input.RecordID)
}

// ReleaseFailedItemGrantRecord 删除一条失败记录、让出它的幂等键（重试前调用），返回被删的记录。
func (r *Repository) ReleaseFailedItemGrantRecord(ctx context.Context, appID, recordID int64) (*itemgrant.Record, error) {
	record, err := r.GetItemGrantRecord(ctx, appID, recordID)
	if err != nil {
		return nil, err
	}
	if record.Status != itemgrant.RecordFailed {
		return nil, ErrItemGrantRecordState
	}
	tag, err := r.pool.Exec(ctx, `DELETE FROM item_grant_records WHERE id = $1 AND status = 'failed'`, recordID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrItemGrantRecordState
	}
	return record, nil
}

// ItemGrantRecordDedupeKey 一条记录的幂等键（重试时沿用）。
func (r *Repository) ItemGrantRecordDedupeKey(ctx context.Context, recordID int64) (string, error) {
	var key string
	err := r.pool.QueryRow(ctx, `SELECT dedupe_key FROM item_grant_records WHERE id = $1`, recordID).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrItemGrantRecordNotFound
	}
	return key, err
}

// ItemGrantOverview 应用的发放总览：规则数、记录数、近 days 天趋势、近 30 天按物品汇总。
func (r *Repository) ItemGrantOverview(ctx context.Context, appID int64, dayStart time.Time, days int) (*itemgrant.Overview, error) {
	out := &itemgrant.Overview{Trend: []itemgrant.TrendPoint{}, ByItem: []itemgrant.ItemAggregate{}}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*), COUNT(*) FILTER (WHERE status = 'active')
FROM item_grant_rules WHERE appid = $1`, appID).Scan(&out.Rules, &out.ActiveRules); err != nil {
		return nil, err
	}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FILTER (WHERE status <> 'failed'),
COUNT(DISTINCT user_id) FILTER (WHERE status <> 'failed'),
COUNT(*) FILTER (WHERE status <> 'failed' AND created_at >= $2),
COUNT(*) FILTER (WHERE status = 'pending'), COUNT(*) FILTER (WHERE status = 'failed')
FROM item_grant_records WHERE appid = $1`, appID, dayStart).Scan(&out.Records, &out.Users, &out.Today, &out.Pending,
		&out.Failed); err != nil {
		return nil, err
	}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM item_grant_runs WHERE appid = $1 AND status = 'running'`, appID).
		Scan(&out.Running); err != nil {
		return nil, err
	}
	since := dayStart.AddDate(0, 0, -(days - 1))
	tz := dayStart.Location().String()
	rows, err := r.pool.Query(ctx, `SELECT to_char((created_at AT TIME ZONE $3)::date, 'YYYY-MM-DD'), COUNT(*), COUNT(DISTINCT user_id)
FROM item_grant_records WHERE appid = $1 AND created_at >= $2 AND status <> 'failed'
GROUP BY 1 ORDER BY 1`, appID, since, tz)
	if err != nil {
		return nil, err
	}
	byDay := map[string]itemgrant.TrendPoint{}
	for rows.Next() {
		var point itemgrant.TrendPoint
		if err := rows.Scan(&point.Date, &point.Records, &point.Users); err != nil {
			rows.Close()
			return nil, err
		}
		byDay[point.Date] = point
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range days {
		date := since.AddDate(0, 0, i).Format(time.DateOnly)
		point, ok := byDay[date]
		if !ok {
			point = itemgrant.TrendPoint{Date: date}
		}
		out.Trend = append(out.Trend, point)
	}

	itemRows, err := r.pool.Query(ctx, `SELECT item->'item'->>'type', COUNT(*), COALESCE(SUM((item->'item'->>'amount')::bigint), 0)
FROM item_grant_records d, jsonb_array_elements(d.items) item
WHERE d.appid = $1 AND d.created_at >= $2 AND d.status <> 'failed'
GROUP BY 1 ORDER BY 2 DESC`, appID, dayStart.AddDate(0, 0, -29))
	if err != nil {
		return nil, err
	}
	defer itemRows.Close()
	for itemRows.Next() {
		var agg itemgrant.ItemAggregate
		if err := itemRows.Scan(&agg.Type, &agg.Count, &agg.Amount); err != nil {
			return nil, err
		}
		if spec, ok := itemgrant.FindItemSpec(agg.Type); ok {
			agg.Label = spec.Label
		}
		out.ByItem = append(out.ByItem, agg)
	}
	return out, itemRows.Err()
}
