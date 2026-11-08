package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// BackfillRun 一次关系图回灌
type BackfillRun struct {
	ID         int64           `json:"id"`
	Operator   string          `json:"operator"`
	Status     string          `json:"status"`
	Progress   json.RawMessage `json:"progress"`
	StartedAt  time.Time       `json:"startedAt"`
	FinishedAt *time.Time      `json:"finishedAt,omitempty"`
}

// StartBackfill 记一次开始，返回 ID
func (s *Store) StartBackfill(ctx context.Context, operator string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, "INSERT INTO graph_backfills (operator) VALUES ($1) RETURNING id", operator).Scan(&id)
	return id, err
}

// SaveBackfill 更新进度；finished 为真时记结束时间
func (s *Store) SaveBackfill(ctx context.Context, id int64, status string, progress any, finished bool) error {
	raw, err := json.Marshal(progress)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE graph_backfills SET status = $2, progress = $3,
		finished_at = CASE WHEN $4 THEN now() ELSE finished_at END WHERE id = $1`, id, status, raw, finished)
	return err
}

// LastBackfill 最近一次（没有时返回 nil）
func (s *Store) LastBackfill(ctx context.Context) (*BackfillRun, error) {
	var r BackfillRun
	err := s.pool.QueryRow(ctx, `SELECT id, operator, status, progress, started_at, finished_at FROM graph_backfills ORDER BY id DESC LIMIT 1`).
		Scan(&r.ID, &r.Operator, &r.Status, &r.Progress, &r.StartedAt, &r.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &r, err
}

// BackfillDone 有没有成功跑完过
func (s *Store) BackfillDone(ctx context.Context) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM graph_backfills WHERE status = 'done')").Scan(&ok)
	return ok, err
}

// AbandonBackfills 进程重启时把还标着 running 的记录收尾（它们的进程已经不在了）
func (s *Store) AbandonBackfills(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `UPDATE graph_backfills SET status = 'failed', finished_at = now(),
		progress = jsonb_set(progress, '{error}', '"服务重启，回灌中断"') WHERE status = 'running'`)
	return err
}
