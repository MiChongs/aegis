package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	platformdomain "aegis/internal/domain/platform"
	"github.com/jackc/pgx/v5"
)

const appGovernanceColumns = `
g.appid,
g.state,
COALESCE(g.reason, ''),
COALESCE(g.restrictions, '{}'::jsonb),
COALESCE(g.evidence, '{}'::jsonb),
g.start_at,
g.end_at,
g.operator_admin_id,
COALESCE(g.operator_name, ''),
COALESCE(g.last_action, ''),
COALESCE(g.appeal_status, 'none'),
g.created_at,
g.updated_at`

const appGovernanceActionColumns = `
a.id,
a.appid,
a.action,
a.from_state,
a.to_state,
COALESCE(a.reason, ''),
COALESCE(a.restrictions, '{}'::jsonb),
COALESCE(a.evidence, '{}'::jsonb),
a.end_at,
a.operator_admin_id,
COALESCE(a.operator_name, ''),
COALESCE(a.operator_ip, ''),
a.revoked_sessions,
a.created_at`

const appGovernanceAppealColumns = `
p.id,
p.appid,
p.action_id,
COALESCE(p.state_snapshot, ''),
COALESCE(p.content, ''),
COALESCE(p.attachments, '[]'::jsonb),
p.submitted_by_admin_id,
COALESCE(p.submitted_by_name, ''),
p.status,
p.review_admin_id,
COALESCE(p.review_admin_name, ''),
COALESCE(p.review_note, ''),
p.reviewed_at,
p.created_at,
p.updated_at`

// GetAppGovernance 读取单个应用的治理状态；无记录返回 nil（等价于 active）。
func (r *Repository) GetAppGovernance(ctx context.Context, appID int64) (*platformdomain.Governance, error) {
	item, err := scanAppGovernance(r.pool.QueryRow(ctx, `SELECT `+appGovernanceColumns+`, COALESCE(app.name, ''), COALESCE(app.app_key, '')
FROM app_governance_states g
LEFT JOIN apps app ON app.id = g.appid
WHERE g.appid = $1`, appID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return item, nil
}

// ListAppGovernance 加载全部非 active 的治理状态（服务层内存快照的数据源）。
//
// 只取非 active：绝大多数应用没有任何治理，把它们也装进内存纯属浪费，
// 判定时"查不到即放行"与"查到 active"等价。
func (r *Repository) ListAppGovernance(ctx context.Context) ([]platformdomain.Governance, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+appGovernanceColumns+`, COALESCE(app.name, ''), COALESCE(app.app_key, '')
FROM app_governance_states g
LEFT JOIN apps app ON app.id = g.appid
WHERE g.state <> 'active'
ORDER BY g.appid ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]platformdomain.Governance, 0, 8)
	for rows.Next() {
		item, err := scanAppGovernance(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// ApplyAppGovernance 在单个事务内写入新的治理状态并追加一条流水。
//
// 状态与流水必须同事务：只写状态会失去追责依据，只写流水会让判定读到旧结论。
func (r *Repository) ApplyAppGovernance(
	ctx context.Context,
	next platformdomain.Governance,
	action platformdomain.ActionRecord,
) (*platformdomain.Governance, *platformdomain.ActionRecord, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	// 锁住应用行，串行化同一应用上的并发治理动作
	var appName, appKey string
	if err := tx.QueryRow(ctx, `SELECT name, COALESCE(app_key, '') FROM apps WHERE id = $1 FOR UPDATE`, next.AppID).
		Scan(&appName, &appKey); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, nil
		}
		return nil, nil, err
	}

	restrictionsJSON, _ := json.Marshal(next.Restrictions)
	evidenceJSON, _ := json.Marshal(cloneMap(next.Evidence))
	appealStatus := strings.TrimSpace(next.AppealStatus)
	if appealStatus == "" {
		appealStatus = platformdomain.AppealStatusNone
	}

	saved, err := scanAppGovernanceRowOnly(tx.QueryRow(ctx, `INSERT INTO app_governance_states (
    appid, state, reason, restrictions, evidence, start_at, end_at,
    operator_admin_id, operator_name, last_action, appeal_status, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW(), NOW())
ON CONFLICT (appid) DO UPDATE SET
    state = EXCLUDED.state,
    reason = EXCLUDED.reason,
    restrictions = EXCLUDED.restrictions,
    evidence = EXCLUDED.evidence,
    start_at = EXCLUDED.start_at,
    end_at = EXCLUDED.end_at,
    operator_admin_id = EXCLUDED.operator_admin_id,
    operator_name = EXCLUDED.operator_name,
    last_action = EXCLUDED.last_action,
    appeal_status = EXCLUDED.appeal_status,
    updated_at = NOW()
RETURNING appid, state, COALESCE(reason, ''), COALESCE(restrictions, '{}'::jsonb), COALESCE(evidence, '{}'::jsonb),
          start_at, end_at, operator_admin_id, COALESCE(operator_name, ''), COALESCE(last_action, ''),
          COALESCE(appeal_status, 'none'), created_at, updated_at`,
		next.AppID,
		next.State,
		strings.TrimSpace(next.Reason),
		restrictionsJSON,
		evidenceJSON,
		nullableTimePtr(next.StartAt),
		nullableTimePtr(next.EndAt),
		nullableInt64Ptr(next.OperatorAdminID),
		strings.TrimSpace(next.OperatorName),
		strings.TrimSpace(next.LastAction),
		appealStatus,
	))
	if err != nil {
		return nil, nil, err
	}

	actionRestrictionsJSON, _ := json.Marshal(action.Restrictions)
	actionEvidenceJSON, _ := json.Marshal(cloneMap(action.Evidence))
	record, err := scanAppGovernanceActionRowOnly(tx.QueryRow(ctx, `INSERT INTO app_governance_actions (
    appid, action, from_state, to_state, reason, restrictions, evidence, end_at,
    operator_admin_id, operator_name, operator_ip, revoked_sessions, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW())
RETURNING id, appid, action, from_state, to_state, COALESCE(reason, ''), COALESCE(restrictions, '{}'::jsonb),
          COALESCE(evidence, '{}'::jsonb), end_at, operator_admin_id, COALESCE(operator_name, ''),
          COALESCE(operator_ip, ''), revoked_sessions, created_at`,
		action.AppID,
		action.Action,
		action.FromState,
		action.ToState,
		strings.TrimSpace(action.Reason),
		actionRestrictionsJSON,
		actionEvidenceJSON,
		nullableTimePtr(action.EndAt),
		nullableInt64Ptr(action.OperatorAdminID),
		strings.TrimSpace(action.OperatorName),
		strings.TrimSpace(action.OperatorIP),
		action.RevokedSessions,
	))
	if err != nil {
		return nil, nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	tx = nil

	saved.AppName, saved.AppKey = appName, appKey
	record.AppName, record.AppKey = appName, appKey
	return saved, record, nil
}

// RecordAppGovernanceAction 只追加流水、不改状态（如强制下线全站会话）。
func (r *Repository) RecordAppGovernanceAction(ctx context.Context, action platformdomain.ActionRecord) (*platformdomain.ActionRecord, error) {
	restrictionsJSON, _ := json.Marshal(action.Restrictions)
	evidenceJSON, _ := json.Marshal(cloneMap(action.Evidence))
	return scanAppGovernanceActionRowOnly(r.pool.QueryRow(ctx, `INSERT INTO app_governance_actions (
    appid, action, from_state, to_state, reason, restrictions, evidence, end_at,
    operator_admin_id, operator_name, operator_ip, revoked_sessions, created_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW())
RETURNING id, appid, action, from_state, to_state, COALESCE(reason, ''), COALESCE(restrictions, '{}'::jsonb),
          COALESCE(evidence, '{}'::jsonb), end_at, operator_admin_id, COALESCE(operator_name, ''),
          COALESCE(operator_ip, ''), revoked_sessions, created_at`,
		action.AppID,
		action.Action,
		action.FromState,
		action.ToState,
		strings.TrimSpace(action.Reason),
		restrictionsJSON,
		evidenceJSON,
		nullableTimePtr(action.EndAt),
		nullableInt64Ptr(action.OperatorAdminID),
		strings.TrimSpace(action.OperatorName),
		strings.TrimSpace(action.OperatorIP),
		action.RevokedSessions,
	))
}

// UpdateGovernanceActionRevokedSessions 回写某条流水实际踢掉的会话数。
//
// 会话清扫发生在事务提交之后（Redis 与 Postgres 不共享事务，把它放进事务里
// 只会让"库里已封、Redis 没清"和"清了但事务回滚"两种错位都变得可能），
// 因此数字只能事后补记。
func (r *Repository) UpdateGovernanceActionRevokedSessions(ctx context.Context, actionID int64, revoked int) error {
	_, err := r.pool.Exec(ctx, `UPDATE app_governance_actions SET revoked_sessions = $2 WHERE id = $1`, actionID, revoked)
	return err
}

// ExpireDueAppGovernance 把已到期的治理状态批量恢复为 active，并为每条写入 expire 流水。
// 返回被恢复的应用治理快照，供服务层刷新内存并通知。
func (r *Repository) ExpireDueAppGovernance(ctx context.Context, limit int) ([]platformdomain.Governance, error) {
	if limit <= 0 {
		limit = 100
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

	rows, err := tx.Query(ctx, `SELECT appid, state, COALESCE(reason, '') FROM app_governance_states
WHERE state <> 'active' AND end_at IS NOT NULL AND end_at <= NOW()
ORDER BY end_at ASC
LIMIT $1
FOR UPDATE SKIP LOCKED`, limit)
	if err != nil {
		return nil, err
	}
	type dueRow struct {
		appID  int64
		state  string
		reason string
	}
	due := make([]dueRow, 0, limit)
	for rows.Next() {
		var item dueRow
		if err := rows.Scan(&item.appID, &item.state, &item.reason); err != nil {
			rows.Close()
			return nil, err
		}
		due = append(due, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(due) == 0 {
		return nil, nil
	}

	restored := make([]platformdomain.Governance, 0, len(due))
	for _, item := range due {
		saved, err := scanAppGovernanceRowOnly(tx.QueryRow(ctx, `UPDATE app_governance_states SET
    state = 'active',
    reason = '',
    restrictions = '{}'::jsonb,
    start_at = NULL,
    end_at = NULL,
    operator_admin_id = NULL,
    operator_name = '',
    last_action = $2,
    updated_at = NOW()
WHERE appid = $1
RETURNING appid, state, COALESCE(reason, ''), COALESCE(restrictions, '{}'::jsonb), COALESCE(evidence, '{}'::jsonb),
          start_at, end_at, operator_admin_id, COALESCE(operator_name, ''), COALESCE(last_action, ''),
          COALESCE(appeal_status, 'none'), created_at, updated_at`, item.appID, platformdomain.ActionExpire))
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO app_governance_actions (
    appid, action, from_state, to_state, reason, operator_name, created_at
) VALUES ($1, $2, $3, 'active', $4, '系统', NOW())`,
			item.appID, platformdomain.ActionExpire, item.state, "治理期限到期，自动恢复"); err != nil {
			return nil, err
		}
		restored = append(restored, *saved)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return restored, nil
}

// ListAppGovernanceActions 治理流水分页查询。
func (r *Repository) ListAppGovernanceActions(ctx context.Context, query platformdomain.ActionQuery) ([]platformdomain.ActionRecord, int64, error) {
	conditions := make([]string, 0, 4)
	args := make([]any, 0, 4)
	if query.AppID > 0 {
		args = append(args, query.AppID)
		conditions = append(conditions, fmt.Sprintf("a.appid = $%d", len(args)))
	}
	if action := strings.TrimSpace(query.Action); action != "" {
		args = append(args, action)
		conditions = append(conditions, fmt.Sprintf("a.action = $%d", len(args)))
	}
	if state := strings.TrimSpace(query.State); state != "" {
		args = append(args, state)
		conditions = append(conditions, fmt.Sprintf("a.to_state = $%d", len(args)))
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		idx := len(args)
		conditions = append(conditions, fmt.Sprintf("(app.name ILIKE $%d OR a.reason ILIKE $%d OR a.operator_name ILIKE $%d)", idx, idx, idx))
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM app_governance_actions a
LEFT JOIN apps app ON app.id = a.appid`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []platformdomain.ActionRecord{}, 0, nil
	}

	offset := (query.Page - 1) * query.Limit
	args = append(args, query.Limit, offset)
	rows, err := r.pool.Query(ctx, `SELECT `+appGovernanceActionColumns+`, COALESCE(app.name, ''), COALESCE(app.app_key, '')
FROM app_governance_actions a
LEFT JOIN apps app ON app.id = a.appid`+where+fmt.Sprintf(`
ORDER BY a.created_at DESC, a.id DESC
LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]platformdomain.ActionRecord, 0, query.Limit)
	for rows.Next() {
		item, err := scanAppGovernanceAction(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

// ListAppGovernanceOverview 全站应用总览：应用元数据 + 治理状态 + 用量指标。
//
// 一次查询取齐，避免 N 个应用打 N 次统计查询。
func (r *Repository) ListAppGovernanceOverview(ctx context.Context, query platformdomain.OverviewQuery, allowedAppIDs []int64) ([]platformdomain.AppOverviewItem, int64, error) {
	conditions := make([]string, 0, 4)
	args := make([]any, 0, 5)
	if len(allowedAppIDs) > 0 {
		args = append(args, allowedAppIDs)
		conditions = append(conditions, fmt.Sprintf("app.id = ANY($%d)", len(args)))
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		idx := len(args)
		conditions = append(conditions, fmt.Sprintf("(app.name ILIKE $%d OR app.app_key ILIKE $%d)", idx, idx))
	}
	if state := strings.TrimSpace(query.State); state != "" {
		args = append(args, state)
		conditions = append(conditions, fmt.Sprintf("COALESCE(g.state, 'active') = $%d", len(args)))
	} else if query.Governed {
		conditions = append(conditions, "COALESCE(g.state, 'active') <> 'active'")
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM apps app
LEFT JOIN app_governance_states g ON g.appid = app.id`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []platformdomain.AppOverviewItem{}, 0, nil
	}

	orderBy := "app.id ASC"
	descending := strings.EqualFold(strings.TrimSpace(query.SortOrder), "desc")
	direction := "ASC"
	if descending {
		direction = "DESC"
	}
	switch strings.TrimSpace(query.SortBy) {
	case "name":
		orderBy = "app.name " + direction
	case "users":
		orderBy = "stats.total_users " + direction
	case "newUsers":
		orderBy = "stats.new_users_today " + direction
	case "createdAt":
		orderBy = "app.created_at " + direction
	case "state":
		orderBy = "COALESCE(g.state, 'active') " + direction + ", app.id ASC"
	}

	args = append(args, query.Limit, (query.Page-1)*query.Limit)
	rows, err := r.pool.Query(ctx, `SELECT
    app.id, app.name, COALESCE(app.app_key, ''), app.status, app.login_status, app.register_status,
    COALESCE(g.state, 'active'), COALESCE(g.reason, ''), COALESCE(g.restrictions, '{}'::jsonb),
    g.start_at, g.end_at, COALESCE(g.operator_name, ''), COALESCE(g.last_action, ''), COALESCE(g.appeal_status, 'none'),
    COALESCE(stats.total_users, 0), COALESCE(stats.disabled_users, 0), COALESCE(stats.banned_users, 0),
    COALESCE(stats.new_users_today, 0),
    COALESCE(logins.success_today, 0), COALESCE(logins.failure_today, 0),
    COALESCE(admins.admin_count, 0),
    app.created_at, app.updated_at
FROM apps app
LEFT JOIN app_governance_states g ON g.appid = app.id
LEFT JOIN LATERAL (
    SELECT
      COUNT(*) AS total_users,
      COUNT(*) FILTER (WHERE enabled = false) AS disabled_users,
      COUNT(*) FILTER (WHERE disabled_end_time IS NOT NULL AND disabled_end_time > NOW()) AS banned_users,
      COUNT(*) FILTER (WHERE created_at >= date_trunc('day', NOW())) AS new_users_today
    FROM users WHERE users.appid = app.id
) stats ON TRUE
LEFT JOIN LATERAL (
    SELECT
      COUNT(*) FILTER (WHERE status = 'success') AS success_today,
      COUNT(*) FILTER (WHERE status <> 'success') AS failure_today
    FROM login_audit_logs
    WHERE login_audit_logs.appid = app.id AND created_at >= date_trunc('day', NOW())
) logins ON TRUE
LEFT JOIN LATERAL (
    SELECT COUNT(DISTINCT admin_id) AS admin_count
    FROM admin_assignments WHERE admin_assignments.appid = app.id
) admins ON TRUE`+where+fmt.Sprintf(`
ORDER BY %s
LIMIT $%d OFFSET $%d`, orderBy, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]platformdomain.AppOverviewItem, 0, query.Limit)
	for rows.Next() {
		var item platformdomain.AppOverviewItem
		var restrictions []byte
		if err := rows.Scan(
			&item.AppID, &item.Name, &item.AppKey, &item.Status, &item.LoginStatus, &item.RegisterStatus,
			&item.State, &item.Reason, &restrictions,
			&item.StartAt, &item.EndAt, &item.OperatorName, &item.LastAction, &item.AppealStatus,
			&item.TotalUsers, &item.DisabledUsers, &item.BannedUsers, &item.NewUsersToday,
			&item.LoginSuccessToday, &item.LoginFailureToday,
			&item.AdminCount,
			&item.CreatedAt, &item.UpdatedAt,
		); err != nil {
			return nil, 0, err
		}
		_ = json.Unmarshal(restrictions, &item.Restrictions)
		items = append(items, item)
	}
	return items, total, rows.Err()
}

// GetAppGovernanceSummary 全站聚合指标。
func (r *Repository) GetAppGovernanceSummary(ctx context.Context, allowedAppIDs []int64) (*platformdomain.OverviewSummary, error) {
	scopeClause := ""
	args := make([]any, 0, 1)
	if len(allowedAppIDs) > 0 {
		args = append(args, allowedAppIDs)
		scopeClause = " WHERE app.id = ANY($1)"
	}

	summary := platformdomain.OverviewSummary{StateCounts: map[string]int64{}}
	rows, err := r.pool.Query(ctx, `SELECT COALESCE(g.state, 'active') AS state, COUNT(*)
FROM apps app
LEFT JOIN app_governance_states g ON g.appid = app.id`+scopeClause+`
GROUP BY 1`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var state string
		var count int64
		if err := rows.Scan(&state, &count); err != nil {
			rows.Close()
			return nil, err
		}
		summary.StateCounts[state] = count
		summary.TotalApps += count
		if state == platformdomain.StateActive {
			summary.ActiveApps += count
		} else {
			summary.GovernedApps += count
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	userScope, loginScope, appealScope, expiringScope := "", "", "", ""
	if len(allowedAppIDs) > 0 {
		userScope = " WHERE appid = ANY($1)"
		loginScope = " AND appid = ANY($1)"
		appealScope = " AND appid = ANY($1)"
		expiringScope = " AND appid = ANY($1)"
	}
	if err := r.pool.QueryRow(ctx, `SELECT
    (SELECT COUNT(*) FROM users`+userScope+`),
    (SELECT COUNT(*) FROM users WHERE created_at >= date_trunc('day', NOW())`+strings.Replace(loginScope, " AND ", " AND ", 1)+`),
    (SELECT COUNT(*) FROM login_audit_logs WHERE created_at >= date_trunc('day', NOW()) AND status = 'success'`+loginScope+`),
    (SELECT COUNT(*) FROM app_governance_appeals WHERE status = 'pending'`+appealScope+`),
    (SELECT COUNT(*) FROM app_governance_states WHERE state <> 'active' AND end_at IS NOT NULL AND end_at <= NOW() + INTERVAL '24 hour'`+expiringScope+`)`,
		args...).Scan(
		&summary.TotalUsers,
		&summary.NewUsersToday,
		&summary.LoginsToday,
		&summary.PendingAppeals,
		&summary.ExpiringSoon,
	); err != nil {
		return nil, err
	}
	return &summary, nil
}

// CountPendingGovernanceAppeals 待审申诉数量。
func (r *Repository) CountPendingGovernanceAppeals(ctx context.Context) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM app_governance_appeals WHERE status = 'pending'`).Scan(&count)
	return count, err
}

// CreateGovernanceAppeal 提交申诉，并把状态表的 appeal_status 推进为 pending。
func (r *Repository) CreateGovernanceAppeal(ctx context.Context, appID int64, stateSnapshot string, actionID *int64, input platformdomain.AppealCreateInput, adminID int64, adminName string) (*platformdomain.Appeal, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	attachments := input.Attachments
	if attachments == nil {
		attachments = []string{}
	}
	attachmentsJSON, _ := json.Marshal(attachments)
	appeal, err := scanGovernanceAppealRowOnly(tx.QueryRow(ctx, `INSERT INTO app_governance_appeals (
    appid, action_id, state_snapshot, content, attachments, submitted_by_admin_id, submitted_by_name, status, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, 'pending', NOW(), NOW())
RETURNING id, appid, action_id, COALESCE(state_snapshot, ''), COALESCE(content, ''), COALESCE(attachments, '[]'::jsonb),
          submitted_by_admin_id, COALESCE(submitted_by_name, ''), status, review_admin_id, COALESCE(review_admin_name, ''),
          COALESCE(review_note, ''), reviewed_at, created_at, updated_at`,
		appID, nullableInt64Ptr(actionID), stateSnapshot, strings.TrimSpace(input.Content), attachmentsJSON,
		nullableInt64(adminID), strings.TrimSpace(adminName)))
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE app_governance_states SET appeal_status = 'pending', updated_at = NOW() WHERE appid = $1`, appID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return appeal, nil
}

// GetGovernanceAppeal 读取单条申诉。
func (r *Repository) GetGovernanceAppeal(ctx context.Context, appealID int64) (*platformdomain.Appeal, error) {
	item, err := scanGovernanceAppeal(r.pool.QueryRow(ctx, `SELECT `+appGovernanceAppealColumns+`, COALESCE(app.name, ''), COALESCE(app.app_key, '')
FROM app_governance_appeals p
LEFT JOIN apps app ON app.id = p.appid
WHERE p.id = $1`, appealID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return item, nil
}

// GetLatestPendingAppeal 读取某应用当前待审的申诉。
func (r *Repository) GetLatestPendingAppeal(ctx context.Context, appID int64) (*platformdomain.Appeal, error) {
	item, err := scanGovernanceAppeal(r.pool.QueryRow(ctx, `SELECT `+appGovernanceAppealColumns+`, COALESCE(app.name, ''), COALESCE(app.app_key, '')
FROM app_governance_appeals p
LEFT JOIN apps app ON app.id = p.appid
WHERE p.appid = $1 AND p.status = 'pending'
ORDER BY p.created_at DESC
LIMIT 1`, appID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return item, nil
}

// ListGovernanceAppeals 申诉分页查询。
func (r *Repository) ListGovernanceAppeals(ctx context.Context, query platformdomain.AppealQuery, allowedAppIDs []int64) ([]platformdomain.Appeal, int64, error) {
	conditions := make([]string, 0, 4)
	args := make([]any, 0, 4)
	if len(allowedAppIDs) > 0 {
		args = append(args, allowedAppIDs)
		conditions = append(conditions, fmt.Sprintf("p.appid = ANY($%d)", len(args)))
	}
	if query.AppID > 0 {
		args = append(args, query.AppID)
		conditions = append(conditions, fmt.Sprintf("p.appid = $%d", len(args)))
	}
	if status := strings.TrimSpace(query.Status); status != "" {
		args = append(args, status)
		conditions = append(conditions, fmt.Sprintf("p.status = $%d", len(args)))
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		idx := len(args)
		conditions = append(conditions, fmt.Sprintf("(app.name ILIKE $%d OR p.content ILIKE $%d OR p.submitted_by_name ILIKE $%d)", idx, idx, idx))
	}
	where := ""
	if len(conditions) > 0 {
		where = " WHERE " + strings.Join(conditions, " AND ")
	}

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM app_governance_appeals p
LEFT JOIN apps app ON app.id = p.appid`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []platformdomain.Appeal{}, 0, nil
	}

	args = append(args, query.Limit, (query.Page-1)*query.Limit)
	rows, err := r.pool.Query(ctx, `SELECT `+appGovernanceAppealColumns+`, COALESCE(app.name, ''), COALESCE(app.app_key, '')
FROM app_governance_appeals p
LEFT JOIN apps app ON app.id = p.appid`+where+fmt.Sprintf(`
ORDER BY CASE WHEN p.status = 'pending' THEN 0 ELSE 1 END, p.created_at DESC
LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]platformdomain.Appeal, 0, query.Limit)
	for rows.Next() {
		item, err := scanGovernanceAppeal(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

// ReviewGovernanceAppeal 裁决申诉并同步治理状态表的 appeal_status。
func (r *Repository) ReviewGovernanceAppeal(ctx context.Context, appealID int64, decision string, note string, adminID int64, adminName string) (*platformdomain.Appeal, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	appeal, err := scanGovernanceAppealRowOnly(tx.QueryRow(ctx, `UPDATE app_governance_appeals SET
    status = $2,
    review_admin_id = $3,
    review_admin_name = $4,
    review_note = $5,
    reviewed_at = NOW(),
    updated_at = NOW()
WHERE id = $1 AND status = 'pending'
RETURNING id, appid, action_id, COALESCE(state_snapshot, ''), COALESCE(content, ''), COALESCE(attachments, '[]'::jsonb),
          submitted_by_admin_id, COALESCE(submitted_by_name, ''), status, review_admin_id, COALESCE(review_admin_name, ''),
          COALESCE(review_note, ''), reviewed_at, created_at, updated_at`,
		appealID, decision, nullableInt64(adminID), strings.TrimSpace(adminName), strings.TrimSpace(note)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE app_governance_states SET appeal_status = $2, updated_at = NOW() WHERE appid = $1`, appeal.AppID, decision); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return appeal, nil
}

// WithdrawGovernanceAppeal 撤回自己提交的待审申诉。
func (r *Repository) WithdrawGovernanceAppeal(ctx context.Context, appealID int64, adminID int64) (*platformdomain.Appeal, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	appeal, err := scanGovernanceAppealRowOnly(tx.QueryRow(ctx, `UPDATE app_governance_appeals SET
    status = 'withdrawn', updated_at = NOW()
WHERE id = $1 AND status = 'pending' AND submitted_by_admin_id = $2
RETURNING id, appid, action_id, COALESCE(state_snapshot, ''), COALESCE(content, ''), COALESCE(attachments, '[]'::jsonb),
          submitted_by_admin_id, COALESCE(submitted_by_name, ''), status, review_admin_id, COALESCE(review_admin_name, ''),
          COALESCE(review_note, ''), reviewed_at, created_at, updated_at`, appealID, adminID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE app_governance_states SET appeal_status = 'none', updated_at = NOW() WHERE appid = $1`, appeal.AppID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return appeal, nil
}

// ListAppAdminIDs 列出对某应用具有角色分配的管理员（治理通知的收件人）。
func (r *Repository) ListAppAdminIDs(ctx context.Context, appID int64) ([]int64, error) {
	rows, err := r.pool.Query(ctx, `SELECT DISTINCT a.admin_id
FROM admin_assignments a
JOIN admin_accounts acc ON acc.id = a.admin_id
WHERE a.appid = $1 AND acc.status = 'active'`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]int64, 0, 4)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ── 扫描辅助 ──

func scanAppGovernance(row interface{ Scan(dest ...any) error }) (*platformdomain.Governance, error) {
	var item platformdomain.Governance
	var restrictions, evidence []byte
	if err := row.Scan(
		&item.AppID, &item.State, &item.Reason, &restrictions, &evidence,
		&item.StartAt, &item.EndAt, &item.OperatorAdminID, &item.OperatorName, &item.LastAction,
		&item.AppealStatus, &item.CreatedAt, &item.UpdatedAt,
		&item.AppName, &item.AppKey,
	); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(restrictions, &item.Restrictions)
	_ = json.Unmarshal(evidence, &item.Evidence)
	return &item, nil
}

func scanAppGovernanceRowOnly(row interface{ Scan(dest ...any) error }) (*platformdomain.Governance, error) {
	var item platformdomain.Governance
	var restrictions, evidence []byte
	if err := row.Scan(
		&item.AppID, &item.State, &item.Reason, &restrictions, &evidence,
		&item.StartAt, &item.EndAt, &item.OperatorAdminID, &item.OperatorName, &item.LastAction,
		&item.AppealStatus, &item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(restrictions, &item.Restrictions)
	_ = json.Unmarshal(evidence, &item.Evidence)
	return &item, nil
}

func scanAppGovernanceAction(row interface{ Scan(dest ...any) error }) (*platformdomain.ActionRecord, error) {
	var item platformdomain.ActionRecord
	var restrictions, evidence []byte
	if err := row.Scan(
		&item.ID, &item.AppID, &item.Action, &item.FromState, &item.ToState, &item.Reason,
		&restrictions, &evidence, &item.EndAt, &item.OperatorAdminID, &item.OperatorName,
		&item.OperatorIP, &item.RevokedSessions, &item.CreatedAt,
		&item.AppName, &item.AppKey,
	); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(restrictions, &item.Restrictions)
	_ = json.Unmarshal(evidence, &item.Evidence)
	return &item, nil
}

func scanAppGovernanceActionRowOnly(row interface{ Scan(dest ...any) error }) (*platformdomain.ActionRecord, error) {
	var item platformdomain.ActionRecord
	var restrictions, evidence []byte
	if err := row.Scan(
		&item.ID, &item.AppID, &item.Action, &item.FromState, &item.ToState, &item.Reason,
		&restrictions, &evidence, &item.EndAt, &item.OperatorAdminID, &item.OperatorName,
		&item.OperatorIP, &item.RevokedSessions, &item.CreatedAt,
	); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(restrictions, &item.Restrictions)
	_ = json.Unmarshal(evidence, &item.Evidence)
	return &item, nil
}

func scanGovernanceAppeal(row interface{ Scan(dest ...any) error }) (*platformdomain.Appeal, error) {
	var item platformdomain.Appeal
	var attachments []byte
	if err := row.Scan(
		&item.ID, &item.AppID, &item.ActionID, &item.StateSnapshot, &item.Content, &attachments,
		&item.SubmittedByAdminID, &item.SubmittedByName, &item.Status,
		&item.ReviewAdminID, &item.ReviewAdminName, &item.ReviewNote, &item.ReviewedAt,
		&item.CreatedAt, &item.UpdatedAt,
		&item.AppName, &item.AppKey,
	); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(attachments, &item.Attachments)
	return &item, nil
}

func scanGovernanceAppealRowOnly(row interface{ Scan(dest ...any) error }) (*platformdomain.Appeal, error) {
	var item platformdomain.Appeal
	var attachments []byte
	if err := row.Scan(
		&item.ID, &item.AppID, &item.ActionID, &item.StateSnapshot, &item.Content, &attachments,
		&item.SubmittedByAdminID, &item.SubmittedByName, &item.Status,
		&item.ReviewAdminID, &item.ReviewAdminName, &item.ReviewNote, &item.ReviewedAt,
		&item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(attachments, &item.Attachments)
	return &item, nil
}

func nullableInt64Ptr(value *int64) any {
	if value == nil || *value <= 0 {
		return nil
	}
	return *value
}

var _ = time.Time{}
