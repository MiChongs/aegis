package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	lotterydomain "aegis/internal/domain/lottery"
)

// 抽奖的人群特权与免广告时长（见 internal/domain/lottery/perks.go、docs/lottery.md）。

// lotteryPerkFactsSQL 判定特权的轻量事实：会员、永久会员、免广告时长、今天签到、今天在线分钟。
// $3 此刻，$4 应用时区的今天（user_online_daily 按 UTC 日累计，与行为画像同一个近似）。
const lotteryPerkFactsSQL = `SELECT
  COALESCE(u.vip_expire_at > $3::timestamptz, FALSE) OR u.vip_lifetime_at IS NOT NULL,
  u.vip_lifetime_at IS NOT NULL,
  EXISTS (SELECT 1 FROM user_ad_free_passes p WHERE p.appid = $2 AND p.user_id = $1 AND p.expires_at > $3::timestamptz),
  EXISTS (SELECT 1 FROM daily_signins WHERE user_id = $1 AND appid = $2 AND sign_date = $4::date),
  COALESCE((SELECT SUM(seconds) FROM user_online_daily WHERE user_id = $1 AND day = $4::date), 0) / 60.0
FROM users u WHERE u.id = $1 AND u.appid = $2`

func lotteryPerkFacts(ctx context.Context, q queryExecutor, appID, userID int64, now, today time.Time) (lotterydomain.PerkFacts, error) {
	return scanLotteryPerkFacts(q.QueryRow(ctx, lotteryPerkFactsSQL, userID, appID, now, today.Format(time.DateOnly)), now)
}

// scanLotteryPerkFacts 解析 lotteryPerkFactsSQL 的一行（单发与批量管道共用）。
func scanLotteryPerkFacts(row pgx.Row, now time.Time) (lotterydomain.PerkFacts, error) {
	facts := lotterydomain.PerkFacts{Now: now}
	if err := row.Scan(&facts.Member, &facts.Lifetime, &facts.AdFree, &facts.SignedToday, &facts.OnlineMinutesToday); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return facts, ErrUserNotFound
		}
		return facts, err
	}
	return facts, nil
}

// LotteryPerkFacts 见 lotteryPerkFacts（活动页用；抽奖时在事务里另取一份）。
func (r *Repository) LotteryPerkFacts(ctx context.Context, appID, userID int64, now, today time.Time) (lotterydomain.PerkFacts, error) {
	return lotteryPerkFacts(ctx, r.pool, appID, userID, now, today)
}

// grantAdFreePassTx 发放免广告时长：没有就建，有就从「现在与原到期时间里较晚的那个」起顺延。返回新的到期时间。
func grantAdFreePassTx(ctx context.Context, tx pgx.Tx, appID, userID int64, days int, now time.Time) (time.Time, error) {
	var expires time.Time
	err := tx.QueryRow(ctx, `INSERT INTO user_ad_free_passes (appid, user_id, expires_at, granted_days)
VALUES ($1, $2, $3::timestamptz + make_interval(days => $4::int), $4::int)
ON CONFLICT (appid, user_id) DO UPDATE
SET expires_at = GREATEST(user_ad_free_passes.expires_at, $3::timestamptz) + make_interval(days => $4::int),
    granted_days = user_ad_free_passes.granted_days + $4::int,
    updated_at = NOW()
RETURNING expires_at`, appID, userID, now, days).Scan(&expires)
	return expires, err
}

// AdFreePassUntil 免广告时长的到期时间；没有或已过期时为 nil。
func (r *Repository) AdFreePassUntil(ctx context.Context, appID, userID int64) (*time.Time, error) {
	var expires time.Time
	err := r.pool.QueryRow(ctx, `SELECT expires_at FROM user_ad_free_passes
WHERE appid = $1 AND user_id = $2 AND expires_at > NOW()`, appID, userID).Scan(&expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &expires, nil
}
