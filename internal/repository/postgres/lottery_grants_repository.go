package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// 抽奖次数余额。
//
// 抽奖此前没有「次数」这个概念：dailyLimit / totalLimit 是数 lottery_draws 的历史行
// 数出来的，没有任何地方存得下「这个人还剩几次」。卡密要发抽奖次数就必须先有这个账户。

// grantLotteryDrawsTx 事务内发放抽奖次数（供卡密核销在同一事务里调用）。
//
// 用 UPSERT 而不是「先查再插」：后者在同一用户并发领两张卡时会有一个撞唯一约束，
// 而那次失败会把整个核销事务连带回滚 —— 用户看到的是「兑换失败」，卡却已经作废。
func grantLotteryDrawsTx(ctx context.Context, tx pgx.Tx, appID int64, userID int64, amount int64) (int64, error) {
	var balance int64
	err := tx.QueryRow(ctx, `INSERT INTO lottery_draw_grants (appid, user_id, balance, total_granted)
VALUES ($1, $2, $3, $3)
ON CONFLICT (appid, user_id) DO UPDATE
SET balance = lottery_draw_grants.balance + EXCLUDED.balance,
    total_granted = lottery_draw_grants.total_granted + EXCLUDED.total_granted,
    updated_at = NOW()
RETURNING balance`, appID, userID, amount).Scan(&balance)
	return balance, err
}

// ConsumeLotteryDrawGrant 消耗一次赠送次数；返回是否真的扣到了。
//
// 条件 UPDATE 而不是「读余额 → 判断 → 写回」：后者在同一用户连点两次时
// 两个请求会读到同一个余额，各自减一，结果扣了一次却抽了两把。
// `balance > 0` 写在 WHERE 里，判断与扣减是同一条语句。
func (r *Repository) ConsumeLotteryDrawGrant(ctx context.Context, appID int64, userID int64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE lottery_draw_grants
SET balance = balance - 1, total_consumed = total_consumed + 1, updated_at = NOW()
WHERE appid = $1 AND user_id = $2 AND balance > 0`, appID, userID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// RefundLotteryDrawGrant 把已扣的一次还回去。
//
// 抽奖链路在扣次数之后仍有可能失败（奖池被抽空、写记录失败）。次数是用户花钱买的，
// 失败还不还回去的区别，用户是能直接看出来的。
func (r *Repository) RefundLotteryDrawGrant(ctx context.Context, appID int64, userID int64) error {
	_, err := r.pool.Exec(ctx, `UPDATE lottery_draw_grants
SET balance = balance + 1, total_consumed = GREATEST(total_consumed - 1, 0), updated_at = NOW()
WHERE appid = $1 AND user_id = $2`, appID, userID)
	return err
}

// GetLotteryDrawBalance 查询剩余赠送次数；没有账户时返回 0。
func (r *Repository) GetLotteryDrawBalance(ctx context.Context, appID int64, userID int64) (int64, error) {
	var balance int64
	err := r.pool.QueryRow(ctx,
		`SELECT balance FROM lottery_draw_grants WHERE appid = $1 AND user_id = $2`,
		appID, userID).Scan(&balance)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	return balance, err
}
