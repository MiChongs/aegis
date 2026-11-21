package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	functiondomain "aegis/internal/domain/appfunction"
)

// ── 触发器 ──

const appFunctionTriggerColumns = `t.id, t.appid, t.function_id, COALESCE(f.name, ''), t.kind, t.event, t.cron, t.timezone,
t.description, t.input, t.enabled, t.next_run_at, t.last_fire_at, t.last_status, t.last_error,
t.fire_count, t.fail_count, t.created_by, t.created_at, t.updated_at`

const appFunctionTriggerFrom = ` FROM app_function_triggers t LEFT JOIN app_functions f ON f.id = t.function_id`

func scanAppFunctionTrigger(row interface{ Scan(...any) error }) (*functiondomain.Trigger, error) {
	var item functiondomain.Trigger
	var input []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.FunctionID, &item.Function, &item.Kind, &item.Event,
		&item.Cron, &item.Timezone, &item.Description, &input, &item.Enabled, &item.NextRunAt,
		&item.LastFireAt, &item.LastStatus, &item.LastError, &item.FireCount, &item.FailCount,
		&item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	item.Input = json.RawMessage(emptyJSONObject(input))
	return &item, nil
}

func (r *Repository) collectAppFunctionTriggers(ctx context.Context, sql string, args ...any) ([]functiondomain.Trigger, error) {
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]functiondomain.Trigger, 0, 4)
	for rows.Next() {
		item, err := scanAppFunctionTrigger(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// CreateAppFunctionTrigger 新建触发器。
func (r *Repository) CreateAppFunctionTrigger(ctx context.Context, item functiondomain.Trigger) (*functiondomain.Trigger, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `INSERT INTO app_function_triggers
(appid, function_id, kind, event, cron, timezone, description, input, enabled, next_run_at, created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) RETURNING id`,
		item.AppID, item.FunctionID, item.Kind, item.Event, item.Cron, item.Timezone, item.Description,
		emptyJSONObject(item.Input), item.Enabled, item.NextRunAt, item.CreatedBy).Scan(&id)
	if err != nil {
		return nil, err
	}
	return r.GetAppFunctionTrigger(ctx, item.AppID, item.FunctionID, id)
}

// UpdateAppFunctionTrigger 整体覆盖可改字段（种类不可变）。
func (r *Repository) UpdateAppFunctionTrigger(ctx context.Context, item functiondomain.Trigger) (*functiondomain.Trigger, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE app_function_triggers SET
event=$4, cron=$5, timezone=$6, description=$7, input=$8, enabled=$9, next_run_at=$10, updated_at=NOW()
WHERE appid=$1 AND function_id=$2 AND id=$3`,
		item.AppID, item.FunctionID, item.ID, item.Event, item.Cron, item.Timezone, item.Description,
		emptyJSONObject(item.Input), item.Enabled, item.NextRunAt)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, normalizeNotFound(nil)
	}
	return r.GetAppFunctionTrigger(ctx, item.AppID, item.FunctionID, item.ID)
}

// GetAppFunctionTrigger 单查（functionID 为 0 时不限函数）。
func (r *Repository) GetAppFunctionTrigger(ctx context.Context, appID, functionID, triggerID int64) (*functiondomain.Trigger, error) {
	return scanAppFunctionTrigger(r.pool.QueryRow(ctx, `SELECT `+appFunctionTriggerColumns+appFunctionTriggerFrom+`
WHERE t.appid=$1 AND ($2::bigint = 0 OR t.function_id=$2) AND t.id=$3`, appID, functionID, triggerID))
}

// DeleteAppFunctionTrigger 删除触发器（排队中的任务随外键级联删除）。
func (r *Repository) DeleteAppFunctionTrigger(ctx context.Context, appID, functionID, triggerID int64) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM app_function_triggers WHERE appid=$1 AND function_id=$2 AND id=$3`,
		appID, functionID, triggerID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ListAppFunctionTriggers 一个函数（functionID 为 0 时为整个应用）的触发器。
func (r *Repository) ListAppFunctionTriggers(ctx context.Context, appID, functionID int64) ([]functiondomain.Trigger, error) {
	return r.collectAppFunctionTriggers(ctx, `SELECT `+appFunctionTriggerColumns+appFunctionTriggerFrom+`
WHERE t.appid=$1 AND ($2::bigint = 0 OR t.function_id=$2) ORDER BY t.function_id, t.id`, appID, functionID)
}

// CountAppFunctionTriggers 一个函数的触发器数量（新建时做上限判定）。
func (r *Repository) CountAppFunctionTriggers(ctx context.Context, appID, functionID int64) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM app_function_triggers WHERE appid=$1 AND function_id=$2`,
		appID, functionID).Scan(&count)
	return count, err
}

// ActiveAppFunctionEventTriggers 某个应用里由这个事件拉起、且函数处于激活状态的触发器。
func (r *Repository) ActiveAppFunctionEventTriggers(ctx context.Context, appID int64, event string) ([]functiondomain.Trigger, error) {
	return r.collectAppFunctionTriggers(ctx, `SELECT `+appFunctionTriggerColumns+appFunctionTriggerFrom+`
WHERE t.appid=$1 AND t.kind='event' AND t.event=$2 AND t.enabled AND f.status='active' AND f.active_version<>''
ORDER BY t.id`, appID, event)
}

// DueAppFunctionScheduleTriggers 到点的定时触发器（跨应用）。
func (r *Repository) DueAppFunctionScheduleTriggers(ctx context.Context, now time.Time, limit int) ([]functiondomain.Trigger, error) {
	return r.collectAppFunctionTriggers(ctx, `SELECT `+appFunctionTriggerColumns+appFunctionTriggerFrom+`
WHERE t.kind='schedule' AND t.enabled AND t.next_run_at IS NOT NULL AND t.next_run_at <= $1
ORDER BY t.next_run_at LIMIT $2`, now, limit)
}

// AdvanceAppFunctionSchedule 把定时触发器推到下一次（比较交换）：多实例同时扫到同一条时只有一个推得动，
// 推得动的那个负责入队 —— 这就是定时触发不会在 N 个副本上各跑一次的原因。
func (r *Repository) AdvanceAppFunctionSchedule(ctx context.Context, triggerID int64, expected time.Time, next *time.Time) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE app_function_triggers SET next_run_at=$3
WHERE id=$1 AND next_run_at=$2 AND enabled`, triggerID, expected, next)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// RecordAppFunctionTriggerFire 记下一次触发的结果（控制台触发器卡片上的「上次 / 累计 / 失败」）。
func (r *Repository) RecordAppFunctionTriggerFire(ctx context.Context, triggerID int64, ok bool, message string) error {
	status, failed := functiondomain.JobSucceeded, 0
	if !ok {
		status, failed = functiondomain.JobFailed, 1
	}
	_, err := r.pool.Exec(ctx, `UPDATE app_function_triggers SET
last_fire_at=NOW(), last_status=$2, last_error=$3, fire_count=fire_count+1, fail_count=fail_count+$4
WHERE id=$1`, triggerID, status, truncateRunes(message, 500), failed)
	return err
}

// ── 持久触发队列 ──

const appFunctionTriggerJobColumns = `id, appid, function_id, trigger_id, event_id::text, kind, event, user_id, data,
status, attempts, last_error, run_after, fired_at, finished_at`

func scanAppFunctionTriggerJob(row interface{ Scan(...any) error }) (*functiondomain.TriggerJob, error) {
	var item functiondomain.TriggerJob
	var data []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.FunctionID, &item.TriggerID, &item.EventID, &item.Kind,
		&item.Event, &item.UserID, &data, &item.Status, &item.Attempts, &item.LastError, &item.RunAfter,
		&item.FiredAt, &item.FinishedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	item.Data = json.RawMessage(emptyJSONObject(data))
	return &item, nil
}

// EnqueueAppFunctionTriggerJob 入队。同一触发器内同一个幂等键只入一次（返回 false 表示已经有了）。
func (r *Repository) EnqueueAppFunctionTriggerJob(ctx context.Context, job functiondomain.TriggerJob, dedupeKey string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `INSERT INTO app_function_trigger_jobs
(appid, function_id, trigger_id, event_id, kind, event, user_id, data, dedupe_key, run_after, fired_at)
VALUES ($1,$2,$3,$4::uuid,$5,$6,$7,$8,$9,NOW(),NOW())
ON CONFLICT (trigger_id, dedupe_key) DO NOTHING`,
		job.AppID, job.FunctionID, job.TriggerID, job.EventID, job.Kind, job.Event, job.UserID,
		emptyJSONObject(job.Data), truncateRunes(dedupeKey, 160))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// ClaimAppFunctionTriggerJobs 抢占一批到点的任务：待执行的、以及租约过期还挂在 running 的（执行它的实例挂了）。
// SKIP LOCKED 让多个实例各抢各的，不会互相等。
func (r *Repository) ClaimAppFunctionTriggerJobs(ctx context.Context, limit int, lease time.Duration) ([]functiondomain.TriggerJob, error) {
	rows, err := r.pool.Query(ctx, `UPDATE app_function_trigger_jobs j SET
status='running', attempts=j.attempts+1, locked_until=NOW() + $2::interval
WHERE j.id IN (
  SELECT id FROM app_function_trigger_jobs
  WHERE (status='pending' AND run_after <= NOW()) OR (status='running' AND locked_until < NOW())
  ORDER BY run_after LIMIT $1 FOR UPDATE SKIP LOCKED
) RETURNING `+appFunctionTriggerJobColumns, limit, fmt.Sprintf("%d seconds", int(lease.Seconds())))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]functiondomain.TriggerJob, 0, limit)
	for rows.Next() {
		item, err := scanAppFunctionTriggerJob(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// FinishAppFunctionTriggerJob 结束一个任务。retryAt 非空时退回待执行、到点再抢。
func (r *Repository) FinishAppFunctionTriggerJob(ctx context.Context, jobID int64, status, message string, retryAt *time.Time) error {
	if retryAt != nil {
		_, err := r.pool.Exec(ctx, `UPDATE app_function_trigger_jobs SET
status='pending', last_error=$2, run_after=$3, locked_until=NULL WHERE id=$1`,
			jobID, truncateRunes(message, 500), *retryAt)
		return err
	}
	_, err := r.pool.Exec(ctx, `UPDATE app_function_trigger_jobs SET
status=$2, last_error=$3, locked_until=NULL, finished_at=NOW() WHERE id=$1`,
		jobID, status, truncateRunes(message, 500))
	return err
}

// RetryAppFunctionTriggerJob 控制台「重试」：把一条失败的任务放回队列（次数清零）。
func (r *Repository) RetryAppFunctionTriggerJob(ctx context.Context, appID, functionID, jobID int64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE app_function_trigger_jobs SET
status='pending', attempts=0, run_after=NOW(), locked_until=NULL, finished_at=NULL
WHERE appid=$1 AND function_id=$2 AND id=$3 AND status='failed'`, appID, functionID, jobID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// ListAppFunctionTriggerJobs 一个函数的触发任务。
func (r *Repository) ListAppFunctionTriggerJobs(ctx context.Context, appID, functionID int64,
	query functiondomain.TriggerJobQuery) (*functiondomain.TriggerJobPage, error) {
	limit := query.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	page := max(query.Page, 1)
	where := []string{"appid=$1", "function_id=$2"}
	args := []any{appID, functionID}
	if query.TriggerID > 0 {
		args = append(args, query.TriggerID)
		where = append(where, fmt.Sprintf("trigger_id=$%d", len(args)))
	}
	if query.Status != "" {
		args = append(args, query.Status)
		where = append(where, fmt.Sprintf("status=$%d", len(args)))
	}
	clause := " WHERE " + strings.Join(where, " AND ")
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM app_function_trigger_jobs`+clause, args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, `SELECT `+appFunctionTriggerJobColumns+` FROM app_function_trigger_jobs`+clause+
		fmt.Sprintf(` ORDER BY fired_at DESC, id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]functiondomain.TriggerJob, 0, limit)
	for rows.Next() {
		item, err := scanAppFunctionTriggerJob(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &functiondomain.TriggerJobPage{Items: items, Total: total, Page: page, Limit: limit}, nil
}

// PurgeAppFunctionTriggerJobs 清掉结束已久的任务（调用记录里另有完整留痕）。
func (r *Repository) PurgeAppFunctionTriggerJobs(ctx context.Context, before time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM app_function_trigger_jobs WHERE finished_at IS NOT NULL AND finished_at < $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ── 全景 ──

// AppFunctionOverview 一个应用全部函数近 windowHours 小时的运行状况。
func (r *Repository) AppFunctionOverview(ctx context.Context, appID int64, windowHours int) (*functiondomain.AppOverview, error) {
	if windowHours <= 0 || windowHours > 24*7 {
		windowHours = 24
	}
	since := time.Now().Add(-time.Duration(windowHours) * time.Hour)
	out := &functiondomain.AppOverview{
		WindowHours: windowHours, Sources: []functiondomain.OverviewSource{},
		Capabilities: []functiondomain.OverviewCapability{}, Buckets: []functiondomain.StatsBucket{},
		Items: []functiondomain.OverviewFunction{}, RecentErrors: []functiondomain.OverviewError{},
	}

	functions, err := r.ListAppFunctions(ctx, appID)
	if err != nil {
		return nil, err
	}
	index := make(map[int64]int, len(functions))
	capabilityCount := map[string]int{}
	capabilityOrder := []string{}
	for _, fn := range functions {
		index[fn.ID] = len(out.Items)
		if fn.Status == functiondomain.StatusActive && fn.ActiveVersion != "" {
			out.Active++
		}
		for _, key := range fn.Capabilities {
			if _, seen := capabilityCount[key]; !seen {
				capabilityOrder = append(capabilityOrder, key)
			}
			capabilityCount[key]++
		}
		out.Items = append(out.Items, functiondomain.OverviewFunction{
			Name: fn.Name, Description: fn.Description, Runtime: fn.Runtime, Status: fn.Status,
			ActiveVersion: fn.ActiveVersion, Capabilities: fn.Capabilities, UpdatedAt: fn.UpdatedAt,
			Spark: make([]int64, windowHours+1),
		})
	}
	out.Functions = len(functions)
	for _, key := range capabilityOrder {
		out.Capabilities = append(out.Capabilities, functiondomain.OverviewCapability{Key: key, Count: capabilityCount[key]})
	}

	// 每个函数的汇总
	rows, err := r.pool.Query(ctx, `SELECT function_id, COUNT(*),
COUNT(*) FILTER (WHERE status='success'), COUNT(*) FILTER (WHERE status='error'),
COALESCE(PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY duration_ms) FILTER (WHERE status<>'running'), 0),
MAX(created_at)
FROM app_function_invocations WHERE appid=$1 AND created_at>=$2 GROUP BY function_id`, appID, since)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var functionID, total, success, failed int64
		var p95 float64
		var last *time.Time
		if err := rows.Scan(&functionID, &total, &success, &failed, &p95, &last); err != nil {
			rows.Close()
			return nil, err
		}
		if i, ok := index[functionID]; ok {
			item := &out.Items[i]
			item.Total, item.Failed, item.P95Ms, item.LastInvokedAt = total, failed, p95, last
			if finished := success + failed; finished > 0 {
				item.SuccessRate = float64(success) / float64(finished)
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 全应用汇总
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*), COUNT(*) FILTER (WHERE status='success'), COUNT(*) FILTER (WHERE status='error'),
COALESCE(PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY duration_ms) FILTER (WHERE status<>'running'), 0)
FROM app_function_invocations WHERE appid=$1 AND created_at>=$2`, appID, since).
		Scan(&out.Total, &out.Success, &out.Failed, &out.P95Ms); err != nil {
		return nil, err
	}
	if finished := out.Success + out.Failed; finished > 0 {
		out.SuccessRate = float64(out.Success) / float64(finished)
	}

	// 按来源
	sourceRows, err := r.pool.Query(ctx, `SELECT source, COUNT(*) FROM app_function_invocations
WHERE appid=$1 AND created_at>=$2 GROUP BY source ORDER BY COUNT(*) DESC`, appID, since)
	if err != nil {
		return nil, err
	}
	for sourceRows.Next() {
		var row functiondomain.OverviewSource
		if err := sourceRows.Scan(&row.Source, &row.Count); err != nil {
			sourceRows.Close()
			return nil, err
		}
		out.Sources = append(out.Sources, row)
	}
	sourceRows.Close()

	// 分桶（全应用）+ 每个函数的火花线
	start := since.Truncate(time.Hour)
	bucketRows, err := r.pool.Query(ctx, `
WITH slots AS (
  SELECT generate_series(date_trunc('hour', $2::timestamptz), date_trunc('hour', NOW()), INTERVAL '1 hour') AS at
)
SELECT slots.at,
  COUNT(i.id) FILTER (WHERE i.status='success'),
  COUNT(i.id) FILTER (WHERE i.status='error')
FROM slots
LEFT JOIN app_function_invocations i
  ON i.appid=$1 AND date_trunc('hour', i.created_at)=slots.at
GROUP BY slots.at ORDER BY slots.at`, appID, since)
	if err != nil {
		return nil, err
	}
	for bucketRows.Next() {
		var row functiondomain.StatsBucket
		if err := bucketRows.Scan(&row.At, &row.Success, &row.Failed); err != nil {
			bucketRows.Close()
			return nil, err
		}
		out.Buckets = append(out.Buckets, row)
	}
	bucketRows.Close()

	sparkRows, err := r.pool.Query(ctx, `SELECT function_id, date_trunc('hour', created_at), COUNT(*)
FROM app_function_invocations WHERE appid=$1 AND created_at>=$2 GROUP BY 1, 2`, appID, since)
	if err != nil {
		return nil, err
	}
	for sparkRows.Next() {
		var functionID, count int64
		var at time.Time
		if err := sparkRows.Scan(&functionID, &at, &count); err != nil {
			sparkRows.Close()
			return nil, err
		}
		i, ok := index[functionID]
		if !ok {
			continue
		}
		slot := int(at.Sub(start) / time.Hour)
		if spark := out.Items[i].Spark; slot >= 0 && slot < len(spark) {
			spark[slot] += count
		}
	}
	sparkRows.Close()

	// 触发器
	triggerRows, err := r.pool.Query(ctx, `SELECT function_id, COUNT(*), COUNT(*) FILTER (WHERE enabled)
FROM app_function_triggers WHERE appid=$1 GROUP BY function_id`, appID)
	if err != nil {
		return nil, err
	}
	for triggerRows.Next() {
		var functionID int64
		var total, enabled int
		if err := triggerRows.Scan(&functionID, &total, &enabled); err != nil {
			triggerRows.Close()
			return nil, err
		}
		out.Triggers += total
		out.TriggersEnabled += enabled
		if i, ok := index[functionID]; ok {
			out.Items[i].Triggers = total
		}
	}
	triggerRows.Close()
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FILTER (WHERE status IN ('pending','running')), COUNT(*) FILTER (WHERE status='failed')
FROM app_function_trigger_jobs WHERE appid=$1`, appID).Scan(&out.PendingJobs, &out.FailedJobs); err != nil {
		return nil, err
	}

	// 最近失败
	errorRows, err := r.pool.Query(ctx, `SELECT f.name, i.event_id::text, i.id, i.source, i.error_message, i.created_at
FROM app_function_invocations i JOIN app_functions f ON f.id = i.function_id
WHERE i.appid=$1 AND i.status='error' AND i.created_at>=$2 ORDER BY i.created_at DESC LIMIT 8`, appID, since)
	if err != nil {
		return nil, err
	}
	defer errorRows.Close()
	for errorRows.Next() {
		var row functiondomain.OverviewError
		if err := errorRows.Scan(&row.Function, &row.EventID, &row.ID, &row.Source, &row.Message, &row.CreatedAt); err != nil {
			return nil, err
		}
		out.RecentErrors = append(out.RecentErrors, row)
	}
	return out, errorRows.Err()
}
