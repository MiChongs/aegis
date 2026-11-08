package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	lotterydomain "aegis/internal/domain/lottery"
)

// 限量奖品的库存分片（迁移 000149，docs/lottery.md「容量与并发」）。
//
// 总库存与每日限量原来各是全站一行（lottery_prizes.used、lottery_prize_daily.used）。在抽奖事务里更新它，
// 行锁要一直持有到提交：同一时刻抽中同一个限量奖品的人全部排队，压测里 2000 人并发时这是最大的锁等待。
//
// 现在每个上限拆成最多 16 个分片行（lottery_prize_stock），各自有上限 cap 与已出 used，之和就是奖品的上限与已出。
// 抽中时在事务里挑一个有余量、没被别人锁着的分片 +1（FOR UPDATE SKIP LOCKED），挑不到再等锁；
// 表上的 CHECK (used <= cap) 兜底，任何情况下都不会超发。仍然在抽奖事务里：回滚即还原，没有补偿，没有丢失。

const (
	lotteryStockTotal int16 = 0
	lotteryStockDaily int16 = 1
	// lotteryStockBuckets 每个上限最多拆几片
	lotteryStockBuckets = 16
	// lotteryStockAttempts 取一份最多试几轮（每轮：不等锁地挑一片，挑不到就在一片上等）
	lotteryStockAttempts = 8
)

// lotteryStockEpoch 总库存分片的 day_start（固定值）。
var lotteryStockEpoch = time.Unix(0, 0).UTC()

// splitLotteryStock 把上限 limit（已出 used）分成分片：余量平均分到各片，已出的全部记在 0 号分片。
func splitLotteryStock(limit, used int64) (caps, useds []int32) {
	used = min(max(used, 0), max(limit, 0))
	free := max(limit-used, 0)
	n := min(int64(lotteryStockBuckets), max(free, 1))
	caps, useds = make([]int32, n), make([]int32, n)
	for index := range n {
		caps[index] = int32(free / n)
		if index < free%n {
			caps[index]++
		}
	}
	caps[0] += int32(used)
	useds[0] = int32(used)
	return caps, useds
}

// writeLotteryStockTx 删掉旧分片、按 limit / used 建新分片。
func writeLotteryStockTx(ctx context.Context, tx pgx.Tx, prizeID int64, scope int16, day time.Time, limit, used int64) error {
	if _, err := tx.Exec(ctx, `DELETE FROM lottery_prize_stock WHERE prize_id = $1 AND scope = $2 AND day_start = $3`,
		prizeID, scope, day); err != nil {
		return err
	}
	caps, useds := splitLotteryStock(limit, used)
	_, err := tx.Exec(ctx, `INSERT INTO lottery_prize_stock (prize_id, scope, day_start, bucket, cap, used)
SELECT $1, $2, $3, (b.ord - 1)::smallint, b.cap, b.used
FROM unnest($4::int[], $5::int[]) WITH ORDINALITY AS b(cap, used, ord)
ON CONFLICT DO NOTHING`, prizeID, scope, day, caps, useds)
	return err
}

// rebalanceLotteryStockTx 奖品的库存或每日限量改了之后重新分配分片（已出的保留）。
// 调用方持有活动的独占咨询锁：此时没有进行中的抽奖。
func rebalanceLotteryStockTx(ctx context.Context, tx pgx.Tx, prizeID int64, quantity, dailyLimit int64) error {
	var used int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE((SELECT SUM(used) FROM lottery_prize_stock
WHERE prize_id = $1 AND scope = 0), (SELECT used FROM lottery_prizes WHERE id = $1), 0)`, prizeID).Scan(&used); err != nil {
		return err
	}
	if quantity < 0 {
		// 不限量：分片删掉，已出的数记回奖品行（展示用）
		if _, err := tx.Exec(ctx, `DELETE FROM lottery_prize_stock WHERE prize_id = $1 AND scope = 0`, prizeID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE lottery_prizes SET used = $2 WHERE id = $1`, prizeID, used); err != nil {
			return err
		}
	} else {
		if err := writeLotteryStockTx(ctx, tx, prizeID, lotteryStockTotal, lotteryStockEpoch, quantity, used); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE lottery_prizes SET used = $2 WHERE id = $1`, prizeID, min(used, quantity)); err != nil {
			return err
		}
	}

	// 每日限量：只动今天与之后已经建出来的日分片（更早的日子已经过去）
	rows, err := tx.Query(ctx, `SELECT day_start, SUM(used) FROM lottery_prize_stock
WHERE prize_id = $1 AND scope = 1 AND day_start > NOW() - INTERVAL '2 days' GROUP BY day_start`, prizeID)
	if err != nil {
		return err
	}
	type day struct {
		start time.Time
		used  int64
	}
	days := make([]day, 0, 2)
	for rows.Next() {
		var item day
		if err := rows.Scan(&item.start, &item.used); err != nil {
			rows.Close()
			return err
		}
		days = append(days, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, item := range days {
		if dailyLimit <= 0 {
			if _, err := tx.Exec(ctx, `DELETE FROM lottery_prize_stock WHERE prize_id = $1 AND scope = 1 AND day_start = $2`,
				prizeID, item.start); err != nil {
				return err
			}
			continue
		}
		if err := writeLotteryStockTx(ctx, tx, prizeID, lotteryStockDaily, item.start, dailyLimit, item.used); err != nil {
			return err
		}
	}
	return nil
}

// ensureLotteryStockTx 分片还没建时按上限与已出建出来：每日限量是当天第一个抽中它的人建（000149 之前当天已出的
// 从 lottery_prize_daily 接上）；总库存一般在保存奖品时就建好了，滚动发布期间旧版本新建的奖品在这里补上
// （已出取奖品行）。并发建的人撞主键时什么都不做。
func ensureLotteryStockTx(ctx context.Context, tx pgx.Tx, prizeID int64, scope int16, day time.Time, limit int64) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM lottery_prize_stock WHERE prize_id = $1 AND scope = $2 AND day_start = $3)`,
		prizeID, scope, day).Scan(&exists); err != nil || exists {
		return err
	}
	seedSQL := `SELECT COALESCE((SELECT used FROM lottery_prize_daily WHERE prize_id = $1 AND day_start = $2), 0)::bigint`
	args := []any{prizeID, day}
	if scope == lotteryStockTotal {
		seedSQL, args = `SELECT COALESCE((SELECT used FROM lottery_prizes WHERE id = $1), 0)::bigint`, []any{prizeID}
	}
	var seeded int64
	if err := tx.QueryRow(ctx, seedSQL, args...).Scan(&seeded); err != nil {
		return err
	}
	caps, useds := splitLotteryStock(limit, seeded)
	_, err := tx.Exec(ctx, `INSERT INTO lottery_prize_stock (prize_id, scope, day_start, bucket, cap, used)
SELECT $1, $2, $3, (b.ord - 1)::smallint, b.cap, b.used
FROM unnest($4::int[], $5::int[]) WITH ORDINALITY AS b(cap, used, ord)
ON CONFLICT DO NOTHING`, prizeID, scope, day, caps, useds)
	return err
}

// takeLotteryStockTx 从一个上限里取一份。
//
// 先不等锁地挑一个有余量、没被锁着的分片 +1（FOR UPDATE SKIP LOCKED 只锁它挑中的那一行）。有余量的分片都被
// 别人锁着时，不加锁地读出其中一片，只在这一行上等（单行 UPDATE）；等到时它可能刚被取满，再从头挑。
// 任何时刻一个事务在一个上限里最多持有一个分片的锁（它取到的那一片），等锁的事务手里没有分片锁，
// 所以不会互相死锁；每日限量先于总库存取，跨上限也是同一顺序。确认所有分片都没有余量时返回 false。
func takeLotteryStockTx(ctx context.Context, tx pgx.Tx, prizeID int64, scope int16, day time.Time, salt int64) (bool, error) {
	offset := int16(uint64(salt) % lotteryStockBuckets)
	for range lotteryStockAttempts {
		var bucket int16
		err := tx.QueryRow(ctx, `WITH pick AS (
  SELECT bucket FROM lottery_prize_stock
  WHERE prize_id = $1 AND scope = $2 AND day_start = $3 AND used < cap
  ORDER BY (bucket + $4) % 16 LIMIT 1 FOR UPDATE SKIP LOCKED
)
UPDATE lottery_prize_stock s SET used = s.used + 1 FROM pick
WHERE s.prize_id = $1 AND s.scope = $2 AND s.day_start = $3 AND s.bucket = pick.bucket AND s.used < s.cap
RETURNING s.bucket`, prizeID, scope, day, offset).Scan(&bucket)
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return false, err
		}
		// 有余量的都被锁着（或者真的没有余量了）：读出一片，在它上面等
		err = tx.QueryRow(ctx, `SELECT bucket FROM lottery_prize_stock
WHERE prize_id = $1 AND scope = $2 AND day_start = $3 AND used < cap
ORDER BY (bucket + $4) % 16 LIMIT 1`, prizeID, scope, day, offset).Scan(&bucket)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		tag, err := tx.Exec(ctx, `UPDATE lottery_prize_stock SET used = used + 1
WHERE prize_id = $1 AND scope = $2 AND day_start = $3 AND bucket = $4 AND used < cap`, prizeID, scope, day, bucket)
		if err != nil {
			return false, err
		}
		if tag.RowsAffected() == 1 {
			return true, nil
		}
	}
	return false, nil
}

// reserveLotteryPrizeTx 给选中的奖品占一份：每日限量与总库存各取一份，两步在一个保存点里，
// 任何一步取不到都回到保存点（另一步也不算），返回 false，调用方改发兜底奖。不限量、不限每日的奖品什么都不写。
func reserveLotteryPrizeTx(ctx context.Context, tx pgx.Tx, prize lotterydomain.Prize, dayStart time.Time, salt int64) (bool, error) {
	if prize.DailyLimit <= 0 && prize.Quantity < 0 {
		return true, nil
	}
	if prize.DailyLimit > 0 {
		if err := ensureLotteryStockTx(ctx, tx, prize.ID, lotteryStockDaily, dayStart, int64(prize.DailyLimit)); err != nil {
			return false, err
		}
	}
	if prize.Quantity >= 0 {
		if err := ensureLotteryStockTx(ctx, tx, prize.ID, lotteryStockTotal, lotteryStockEpoch, prize.Quantity); err != nil {
			return false, err
		}
	}
	sp, err := tx.Begin(ctx)
	if err != nil {
		return false, err
	}
	ok := true
	if prize.DailyLimit > 0 {
		if ok, err = takeLotteryStockTx(ctx, sp, prize.ID, lotteryStockDaily, dayStart, salt); err != nil {
			_ = sp.Rollback(ctx)
			return false, err
		}
	}
	if ok && prize.Quantity >= 0 {
		if ok, err = takeLotteryStockTx(ctx, sp, prize.ID, lotteryStockTotal, lotteryStockEpoch, salt); err != nil {
			_ = sp.Rollback(ctx)
			return false, err
		}
	}
	if !ok {
		return false, sp.Rollback(ctx)
	}
	return true, sp.Commit(ctx)
}

// lotteryPrizeDailyUsed 这些奖品某一天全站已出几份（分片之和；还没建分片的那天取 000149 之前的 lottery_prize_daily）。
func lotteryPrizeDailyUsed(ctx context.Context, q queryExecutor, prizeIDs []int64, day time.Time) (map[int64]int64, error) {
	out := make(map[int64]int64, len(prizeIDs))
	rows, err := q.Query(ctx, `SELECT p.id, COALESCE(
  (SELECT SUM(s.used) FROM lottery_prize_stock s WHERE s.prize_id = p.id AND s.scope = 1 AND s.day_start = $2),
  (SELECT d.used FROM lottery_prize_daily d WHERE d.prize_id = p.id AND d.day_start = $2), 0)
FROM unnest($1::bigint[]) AS p(id)`, prizeIDs, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, used int64
		if err := rows.Scan(&id, &used); err != nil {
			return nil, err
		}
		out[id] = used
	}
	return out, rows.Err()
}

// lotteryPrizeUsedSQL 奖品已出几份：限量的取总库存分片之和（还没建分片时取奖品行），不限量的取奖品行。
const lotteryPrizeUsedSQL = `CASE WHEN p.quantity >= 0 THEN COALESCE(
  (SELECT SUM(s.used) FROM lottery_prize_stock s WHERE s.prize_id = p.id AND s.scope = 0), p.used)::int ELSE p.used END`
