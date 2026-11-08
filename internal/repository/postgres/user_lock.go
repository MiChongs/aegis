package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// lockUserRowTx 锁住用户行：同一个人涉及余额、会员、次数的写操作在这把锁下串行。
//
// 锁要拿在事务的开头、持有到提交，所以持锁的事务里不能有按记录聚合的查询（一次几百毫秒，同一个人的
// 其余请求就全排在后面）。见 docs/database.md「行锁纪律」。
func lockUserRowTx(ctx context.Context, tx pgx.Tx, userID, appID int64) error {
	var locked int64
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`, userID, appID).
		Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserNotFound
		}
		return err
	}
	return nil
}

// IsLockContention 错误是不是锁等待超时、死锁或串行化冲突（换句话说：稍后重试大概率能成功）。
func IsLockContention(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	switch pgErr.Code {
	case "55P03", // lock_not_available：lock_timeout 到点
		"40P01", // deadlock_detected
		"40001": // serialization_failure
		return true
	}
	return false
}
