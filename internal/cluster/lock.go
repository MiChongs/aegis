package cluster

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// WithAdvisoryLock 在一条独占连接上持有会话级 advisory lock 执行 fn，fn 返回后释放。
//
// 多个副本同时启动时，迁移、内置策略重建这类动作靠它排队：后到的实例会阻塞到前一个做完，
// 然后看到「已经做过」直接跳过。fn 拿到的 conn 就是持锁的那条，里面的语句与锁同会话。
// 锁键用 hashtextextended 从字符串算出，避免各处手挑整数撞号。
func WithAdvisoryLock(ctx context.Context, pool *pgxpool.Pool, key string, fn func(ctx context.Context, conn *pgxpool.Conn) error) error {
	if pool == nil {
		return fmt.Errorf("cluster: advisory lock %q 需要 Postgres 连接", key)
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(hashtextextended($1, 0))`, key); err != nil {
		return fmt.Errorf("cluster: 获取 advisory lock %q 失败: %w", key, err)
	}
	defer func() {
		// 用独立的 ctx 解锁：调用方 ctx 已取消时也要把锁还掉，否则这条连接回池后锁还挂着。
		_, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(hashtextextended($1, 0))`, key)
	}()
	return fn(ctx, conn)
}
