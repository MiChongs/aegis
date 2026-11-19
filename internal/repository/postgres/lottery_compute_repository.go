package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	lotterydomain "aegis/internal/domain/lottery"
)

// 抽奖的大规模推演任务（表 lottery_compute_jobs，迁移 000157）。
//
// 任务的归属靠「认领 + 心跳」：认领把状态置为 running 并写上实例标识；执行期间按固定周期续心跳、
// 顺带写进度；续心跳的那条语句带着「仍归我、仍在执行」的条件，改不到行就说明任务被取消或被别的实例
// 接手了，执行方立刻停下。心跳过期的任务任何实例都能重新认领 —— 推演由场景里的种子完全确定，
// 从头重跑得到的是同一份结果，所以不需要断点。

// ErrLotteryComputeBusy 这个应用排队与执行中的推演任务已到上限。
var ErrLotteryComputeBusy = errors.New("lottery: too many pending compute jobs")

// ErrLotteryComputeActive 任务还在排队或执行，不能删除。
var ErrLotteryComputeActive = errors.New("lottery: compute job is still active")

const lotteryComputeColumns = `id, appid, activity_id, activity_name, name, status, summary, progress, total, message,
attempts, duration_ms, created_by, created_at, started_at, finished_at, owner`

// scanLotteryComputeJob 解析一行；withBlobs 时多读场景与结果两列。
func scanLotteryComputeJob(row pgx.Row, withBlobs bool) (*lotterydomain.ComputeJob, error) {
	job := &lotterydomain.ComputeJob{}
	var summary, scenario, result []byte
	dest := []any{&job.ID, &job.AppID, &job.ActivityID, &job.ActivityName, &job.Name, &job.Status, &summary,
		&job.Progress, &job.Total, &job.Message, &job.Attempts, &job.DurationMs, &job.CreatedBy, &job.CreatedAt,
		&job.StartedAt, &job.FinishedAt, &job.Owner}
	if withBlobs {
		dest = append(dest, &scenario, &result)
	}
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	if len(summary) > 0 {
		if err := json.Unmarshal(summary, &job.Summary); err != nil {
			return nil, err
		}
	}
	if job.Summary.Personas == nil {
		job.Summary.Personas = []lotterydomain.ComputePersonaSummary{}
	}
	if len(scenario) > 0 {
		job.Scenario = &lotterydomain.ComputeScenario{}
		if err := json.Unmarshal(scenario, job.Scenario); err != nil {
			return nil, err
		}
	}
	if len(result) > 0 {
		job.Result = &lotterydomain.ComputeResult{}
		if err := json.Unmarshal(result, job.Result); err != nil {
			return nil, err
		}
	}
	return job, nil
}

// CreateLotteryComputeJob 新建一个排队中的推演任务。同一个应用排队与执行中的任务到上限时返回
// ErrLotteryComputeBusy；顺手清掉这个应用最旧的已结束任务，只留最近 ComputeJobsKept 个。
func (r *Repository) CreateLotteryComputeJob(ctx context.Context, job lotterydomain.ComputeJob,
	scenario lotterydomain.ComputeScenario) (*lotterydomain.ComputeJob, error) {
	scenarioRaw, err := json.Marshal(scenario)
	if err != nil {
		return nil, err
	}
	summaryRaw, err := json.Marshal(job.Summary)
	if err != nil {
		return nil, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// 同一个应用的新建串行：数一数再插入，不然并发的两个请求都能数到「还差一个」
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('lottery.compute:' || $1::bigint::text, 0))`, job.AppID); err != nil {
		return nil, err
	}
	var pending int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM lottery_compute_jobs WHERE appid = $1 AND status IN ('queued', 'running')`,
		job.AppID).Scan(&pending); err != nil {
		return nil, err
	}
	if pending >= lotterydomain.MaxComputePending {
		return nil, ErrLotteryComputeBusy
	}
	created, err := scanLotteryComputeJob(tx.QueryRow(ctx, `INSERT INTO lottery_compute_jobs
(appid, activity_id, activity_name, name, status, scenario, summary, total, created_by)
VALUES ($1, $2, $3, $4, 'queued', $5, $6, $7, $8)
RETURNING `+lotteryComputeColumns, job.AppID, job.ActivityID, truncateColumn(job.ActivityName, 128),
		truncateColumn(job.Name, 64), scenarioRaw, summaryRaw, scenario.Replications, truncateColumn(job.CreatedBy, 128)), false)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM lottery_compute_jobs WHERE appid = $1 AND status NOT IN ('queued', 'running')
AND id NOT IN (SELECT id FROM lottery_compute_jobs WHERE appid = $1 AND status NOT IN ('queued', 'running')
               ORDER BY id DESC LIMIT $2)`, job.AppID, lotterydomain.ComputeJobsKept); err != nil {
		return nil, err
	}
	return created, tx.Commit(ctx)
}

// GetLotteryComputeJob 一个任务，带结果；不属于这个应用时返回 pgx.ErrNoRows。
func (r *Repository) GetLotteryComputeJob(ctx context.Context, appID, jobID int64) (*lotterydomain.ComputeJob, error) {
	job, err := scanLotteryComputeJob(r.pool.QueryRow(ctx, `SELECT `+lotteryComputeColumns+`, NULL::jsonb, result
FROM lottery_compute_jobs WHERE id = $1 AND appid = $2`, jobID, appID), true)
	if err != nil {
		return nil, err
	}
	return job, nil
}

// ListLotteryComputeJobs 一个应用的推演任务，新的在前；activityID 大于 0 时只看这个活动的。不带场景与结果。
func (r *Repository) ListLotteryComputeJobs(ctx context.Context, appID, activityID int64, page, limit int) (
	[]lotterydomain.ComputeJob, int64, error) {
	page, limit = max(page, 1), min(max(limit, 1), 100)
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM lottery_compute_jobs
WHERE appid = $1 AND ($2 = 0 OR activity_id = $2)`, appID, activityID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT `+lotteryComputeColumns+` FROM lottery_compute_jobs
WHERE appid = $1 AND ($2 = 0 OR activity_id = $2) ORDER BY id DESC LIMIT $3 OFFSET $4`,
		appID, activityID, limit, (page-1)*limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]lotterydomain.ComputeJob, 0, limit)
	for rows.Next() {
		job, err := scanLotteryComputeJob(rows, false)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *job)
	}
	return items, total, rows.Err()
}

// ClaimLotteryComputeJobs 认领最多 limit 个任务：排队中的，以及执行中但心跳早于 staleBefore 的（执行它的实例没了）。
// 认领即置为 running、写上 owner、认领次数加一，进度清零（从头重跑）。返回的任务带场景。
//
// 认领次数已经用完的不再认领，由 FailExhaustedLotteryComputeJobs 判失败。
func (r *Repository) ClaimLotteryComputeJobs(ctx context.Context, owner string, staleBefore time.Time, limit int) (
	[]lotterydomain.ComputeJob, error) {
	rows, err := r.pool.Query(ctx, `UPDATE lottery_compute_jobs SET status = 'running', owner = $1, heartbeat_at = NOW(),
started_at = NOW(), attempts = attempts + 1, progress = 0, message = ''
WHERE id IN (
    SELECT id FROM lottery_compute_jobs
    WHERE (status = 'queued' OR (status = 'running' AND (heartbeat_at IS NULL OR heartbeat_at < $2)))
      AND attempts < $3
    ORDER BY id LIMIT $4 FOR UPDATE SKIP LOCKED)
RETURNING `+lotteryComputeColumns+`, scenario, NULL::jsonb`, owner, staleBefore, lotterydomain.MaxComputeAttempts, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []lotterydomain.ComputeJob{}
	for rows.Next() {
		job, err := scanLotteryComputeJob(rows, true)
		if err != nil {
			return nil, err
		}
		out = append(out, *job)
	}
	return out, rows.Err()
}

// FailExhaustedLotteryComputeJobs 认领次数用完、又没人在跑的任务判失败（心跳早于 staleBefore 的执行中任务，
// 与次数用完却还在排队的任务）。返回判了几个。
func (r *Repository) FailExhaustedLotteryComputeJobs(ctx context.Context, staleBefore time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE lottery_compute_jobs SET status = 'failed', finished_at = NOW(), owner = '',
message = '执行多次都被中断，已停止重试'
WHERE attempts >= $2 AND (status = 'queued' OR (status = 'running' AND (heartbeat_at IS NULL OR heartbeat_at < $1)))`,
		staleBefore, lotterydomain.MaxComputeAttempts)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// HeartbeatLotteryComputeJob 执行方续心跳并写进度。返回 false 表示任务已经不归自己跑了
// （被取消、被别的实例接手）—— 调用方应当立刻停下。
func (r *Repository) HeartbeatLotteryComputeJob(ctx context.Context, jobID int64, owner string, progress int) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE lottery_compute_jobs SET heartbeat_at = NOW(), progress = GREATEST(progress, $3)
WHERE id = $1 AND owner = $2 AND status = 'running'`, jobID, owner, progress)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// FinishLotteryComputeJob 执行方写回结果（succeeded）或失败原因（failed）。只在任务仍归自己、仍在执行时生效，
// 返回是否写上了：被取消或被接手的任务不会被迟到的结果覆盖。
func (r *Repository) FinishLotteryComputeJob(ctx context.Context, jobID int64, owner, status string,
	result *lotterydomain.ComputeResult, message string, duration time.Duration) (bool, error) {
	var raw []byte
	if result != nil {
		var err error
		if raw, err = json.Marshal(result); err != nil {
			return false, err
		}
	}
	tag, err := r.pool.Exec(ctx, `UPDATE lottery_compute_jobs SET status = $3::varchar, result = $4, message = $5, duration_ms = $6,
finished_at = NOW(), progress = CASE WHEN $3::varchar = 'succeeded' THEN total ELSE progress END, owner = ''
WHERE id = $1 AND owner = $2 AND status = 'running'`, jobID, owner, status, raw, truncateColumn(message, 2000),
		duration.Milliseconds())
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ReleaseLotteryComputeJob 执行方正常退出（实例关闭）时把任务放回排队，不必等心跳过期；
// 这一次没有跑完不算它的错，认领次数退回去。
func (r *Repository) ReleaseLotteryComputeJob(ctx context.Context, jobID int64, owner string) error {
	_, err := r.pool.Exec(ctx, `UPDATE lottery_compute_jobs SET status = 'queued', owner = '', heartbeat_at = NULL,
progress = 0, attempts = GREATEST(attempts - 1, 0)
WHERE id = $1 AND owner = $2 AND status = 'running'`, jobID, owner)
	return err
}

// CancelLotteryComputeJob 取消一个排队或执行中的任务，返回取消后的任务；已经结束的原样返回。
// 执行它的实例在下一次心跳时发现状态变了，随即停下。
func (r *Repository) CancelLotteryComputeJob(ctx context.Context, appID, jobID int64, operator string) (*lotterydomain.ComputeJob, error) {
	if _, err := r.pool.Exec(ctx, `UPDATE lottery_compute_jobs SET status = 'cancelled', finished_at = NOW(), owner = '',
message = $3 WHERE id = $1 AND appid = $2 AND status IN ('queued', 'running')`, jobID, appID,
		truncateColumn("已由 "+operator+" 取消", 2000)); err != nil {
		return nil, err
	}
	return r.GetLotteryComputeJob(ctx, appID, jobID)
}

// DeleteLotteryComputeJob 删除一个已经结束的任务。不存在时返回 pgx.ErrNoRows，还在排队或执行时返回 ErrLotteryComputeActive。
func (r *Repository) DeleteLotteryComputeJob(ctx context.Context, appID, jobID int64) error {
	var status string
	err := r.pool.QueryRow(ctx, `DELETE FROM lottery_compute_jobs WHERE id = $1 AND appid = $2
AND status NOT IN ('queued', 'running') RETURNING status`, jobID, appID).Scan(&status)
	if err == nil {
		return nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM lottery_compute_jobs WHERE id = $1 AND appid = $2)`,
		jobID, appID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return ErrLotteryComputeActive
	}
	return pgx.ErrNoRows
}
