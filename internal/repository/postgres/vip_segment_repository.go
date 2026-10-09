package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	vipdomain "aegis/internal/domain/vip"
	"github.com/jackc/pgx/v5"
)

// 会员段：权益判定读的是「此刻仍生效的各段 + 各段所引用套餐的当前配置」。
//
// 规则见 internal/domain/vip/segment.go。这个文件只管三件事：
// 把段取出来、退款时作废一段、扣减时截断链尾。三者都只动 active_from /
// active_until / revoked_at，expire_before / expire_after 是账本，不改写。

// vipSegmentFromSQL / vipSegmentUntilSQL 一段会员期在链上的实际位置。
//
// active_* 为 NULL 表示从未被调整过（上线前的历史记录、滚动发布期间旧实例写的记录），
// 此时按账本推导：起点 = 开通时的旧到期时间（仍在期内时顺延）或开通时刻，
// 终点 = 开通后的到期时间。与 extendUserVipTx 算 base 的方式一致。
//
// 永久开通没有终点，比较时按 'infinity' 算：所有「终点在某时刻之后」的筛选对它天然成立，
// 不必在每条 SQL 里另写一个分支。它永远不能被直接 Scan 进 time.Time（pgx 拒收 infinity），
// 需要取终点的地方一律先排除永久开通（vipSegmentTimedSQL）。
const (
	vipSegmentFromSQL  = `COALESCE(vt.active_from, GREATEST(COALESCE(vt.expire_before, vt.created_at), vt.created_at))`
	vipSegmentUntilSQL = `(CASE WHEN vt.lifetime THEN 'infinity'::timestamptz ELSE COALESCE(vt.active_until, vt.expire_after) END)`
)

// vipSegmentScopeSQL 「这一行是一段会员期」：正时长或永久、未作废。
// 扣减记录（admin_revoke）是留痕的账，不是一段会员期 —— 扣天数那种是负时长，
// 取消永久那种带着 lifetime 标记，所以按渠道排除，而不是只看时长。
const vipSegmentScopeSQL = `(vt.duration_days > 0 OR vt.lifetime) AND vt.revoked_at IS NULL AND vt.pay_channel <> 'admin_revoke'`

// vipSegmentTimedSQL 限时会员段：在顺延链上、有终点。退款前移与扣减截断只动这些。
const vipSegmentTimedSQL = `NOT vt.lifetime`

// vipLiveSegmentsSQL 此刻仍生效的各段，连同所引用套餐的**当前**配置，聚成一个 JSON 数组。
//
// 聚成 JSON 而不是另查一次：判定事实必须一次取齐（见 GetVipEntitlementFacts），
// 分两次查就有"中间刚好买了会员"的窗口。
//
// 套餐按 (id, appid) 关联：plan_id 没有外键，套餐被删后这一侧为 NULL，读取端回落到快照。
// 窗口判定与 Segment.LiveAt 同一口径，Evaluate 会用同一个 now 再筛一遍。
const vipLiveSegmentsSQL = `COALESCE((
    SELECT jsonb_agg(jsonb_build_object(
        'id', vt.id,
        'transactionNo', vt.transaction_no,
        'channel', vt.pay_channel,
        'planId', vt.plan_id,
        'planName', vt.plan_name,
        'features', to_jsonb(vt.features),
        'plan', CASE WHEN p.id IS NULL THEN NULL
                     ELSE jsonb_build_object('name', p.name, 'features', to_jsonb(p.features)) END,
        'activeFrom', ` + vipSegmentFromSQL + `,
        'activeUntil', CASE WHEN vt.lifetime THEN NULL ELSE ` + vipSegmentUntilSQL + ` END,
        'lifetime', vt.lifetime
    ) ORDER BY vt.id)
    FROM vip_transactions vt
    LEFT JOIN vip_plans p ON p.id = vt.plan_id AND p.appid = vt.appid
    WHERE vt.appid = u.appid AND vt.user_id = u.id AND ` + vipSegmentScopeSQL + `
      AND ` + vipSegmentUntilSQL + ` > NOW()
      AND ` + vipSegmentUntilSQL + ` > ` + vipSegmentFromSQL + `
), '[]'::jsonb)`

// vipFeatureCatalogSQL 本应用启用中的功能标识。功能权益只在这个集合里取。
const vipFeatureCatalogSQL = `ARRAY(SELECT f.tag FROM vip_features f WHERE f.appid = u.appid AND f.is_active ORDER BY f.tag)`

// vipLifetimeFeaturesSQL 某用户仍生效的永久开通的功能并集（按套餐当前配置，套餐已删除时按快照）。
//
// 只用于「永久会员是否已经包含某个套餐」的判断，因此不与功能目录取交集 ——
// 一个暂时停用的功能，用户的永久开通里有就是有，不该因为停用就允许他再买一次。
const vipLifetimeFeaturesSQL = `ARRAY(
    SELECT DISTINCT tag FROM (
        SELECT unnest(COALESCE(p.features, vt.features)) AS tag
        FROM vip_transactions vt
        LEFT JOIN vip_plans p ON p.id = vt.plan_id AND p.appid = vt.appid
        WHERE vt.appid = u.appid AND vt.user_id = u.id AND vt.lifetime AND ` + vipSegmentScopeSQL + `
    ) tags ORDER BY tag
)`

// vipLifetimeCoverage 某用户是不是永久会员、永久开通里有哪些功能（判「已包含」用）。
func vipLifetimeCoverage(ctx context.Context, q queryExecutor, appID int64, userID int64) (bool, []string, error) {
	var (
		lifetime bool
		features []string
	)
	err := q.QueryRow(ctx, `SELECT u.vip_lifetime_at IS NOT NULL, `+vipLifetimeFeaturesSQL+`
FROM users u WHERE u.id = $1 AND u.appid = $2`, userID, appID).Scan(&lifetime, &features)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil, ErrUserNotFound
	}
	return lifetime, features, err
}

// GetVipLifetimeCoverage 见 vipLifetimeCoverage（服务层在下单前用它拦「已包含」的套餐）。
func (r *Repository) GetVipLifetimeCoverage(ctx context.Context, appID int64, userID int64) (bool, []string, error) {
	return vipLifetimeCoverage(ctx, r.pool, appID, userID)
}

// ConvertLegacyLifetimeVip 把老系统的永久会员（到期时间不早于 LegacyLifetimeExpireAt）转成真正的永久会员。
//
// 与迁移 000091 末尾那条语句做的是同一件事：迁移负责部署时已经在库里的，
// 这里负责之后又导入进来的（同步老用户、直导 dump 的收尾）。可重复执行：
// 已有仍生效的永久开通的不再补记，到期时间清掉之后也不会再被选中。
// 返回本次转换的用户数。
func (r *Repository) ConvertLegacyLifetimeVip(ctx context.Context) (int64, error) {
	result, err := r.pool.Exec(ctx, `WITH legacy AS (
    SELECT id, appid, vip_expire_at FROM users WHERE vip_expire_at >= $1
), ledger AS (
    INSERT INTO vip_transactions (transaction_no, user_id, appid, plan_id, plan_name, features, duration_days,
        pay_channel, pay_amount, related_order_no, bonus_integral, expire_before, expire_after, operator, metadata,
        active_from, active_until, lifetime, created_at)
    SELECT 'VIPL' || l.id::text || '-' || floor(extract(epoch FROM NOW()))::bigint::text,
           l.id, l.appid, NULL, '永久会员', '{}', 0,
           $2, 0, NULL, 0, l.vip_expire_at, NULL, 'system:legacy-import',
           jsonb_build_object('legacyVipExpireAt', l.vip_expire_at),
           NOW(), NULL, TRUE, NOW()
    FROM legacy l
    WHERE NOT EXISTS (
        SELECT 1 FROM vip_transactions vt
        WHERE vt.appid = l.appid AND vt.user_id = l.id AND vt.lifetime AND `+vipSegmentScopeSQL+`
    )
    ON CONFLICT (transaction_no) DO NOTHING
    RETURNING user_id
)
UPDATE users u
SET vip_lifetime_at = COALESCE(u.vip_lifetime_at, NOW()), vip_expire_at = NULL, updated_at = NOW()
FROM legacy l
WHERE u.id = l.id`, vipdomain.LegacyLifetimeExpireAt, vipdomain.ChannelLegacyImport)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// decodeVipSegments 解开 vipLiveSegmentsSQL 聚出来的 JSON。
func decodeVipSegments(raw []byte) ([]vipdomain.Segment, error) {
	if len(raw) == 0 {
		return []vipdomain.Segment{}, nil
	}
	segments := make([]vipdomain.Segment, 0, 4)
	if err := json.Unmarshal(raw, &segments); err != nil {
		return nil, fmt.Errorf("decode vip segments: %w", err)
	}
	return segments, nil
}

// revokeVipSegmentForOrderTx 退款冲正：作废订单对应的那段会员期，排在它后面的各段前移同样天数。
//
// 调用方已经把 users.vip_expire_at 减掉了 days 天。这里让段的位置与之对齐：
// 不作废的话，退掉的高级版只要账上还有别的会员时长就照样生效；
// 不前移的话，后面那段（比如更早买的基础版之后续的一段）会晚 days 天才轮到。
//
// 找不到对应记录（历史订单、手工补单）返回 (false, nil)：到期时间已经扣过了，
// 这里没有可作废的东西，不该让整笔冲正失败。
func revokeVipSegmentForOrderTx(ctx context.Context, tx pgx.Tx, appID int64, userID int64, orderNo string, days int, reason string) (bool, error) {
	orderNo = strings.TrimSpace(orderNo)
	if orderNo == "" {
		return false, nil
	}
	var (
		segmentID    int64
		segmentUntil time.Time
	)
	err := tx.QueryRow(ctx, `SELECT vt.id, `+vipSegmentUntilSQL+`
FROM vip_transactions vt
WHERE vt.appid = $1 AND vt.user_id = $2 AND vt.related_order_no = $3
  AND vt.pay_channel = $4 AND `+vipSegmentScopeSQL+` AND `+vipSegmentTimedSQL+`
ORDER BY vt.id DESC LIMIT 1
FOR UPDATE`, appID, userID, orderNo, vipdomain.ChannelPaymentOrder).Scan(&segmentID, &segmentUntil)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	if _, err := tx.Exec(ctx,
		`UPDATE vip_transactions SET revoked_at = NOW(), revoke_reason = $2 WHERE id = $1`,
		segmentID, reason); err != nil {
		return false, err
	}
	if days <= 0 {
		return true, nil
	}
	// 排在它后面的段：起点不早于它的终点。顺延开通的段起点恰好等于前一段的终点；
	// 中间断档过的段起点更晚，同样随到期时间一起前移，二者与 vip_expire_at 的扣减保持一致。
	_, err = tx.Exec(ctx, `UPDATE vip_transactions AS vt SET
    active_from = `+vipSegmentFromSQL+` - make_interval(days => $4),
    active_until = `+vipSegmentUntilSQL+` - make_interval(days => $4)
WHERE vt.appid = $1 AND vt.user_id = $2 AND vt.id <> $3 AND `+vipSegmentScopeSQL+` AND `+vipSegmentTimedSQL+`
  AND `+vipSegmentFromSQL+` >= $5`,
		appID, userID, segmentID, days, segmentUntil)
	return true, err
}

// revokeLifetimeSegmentForOrderTx 永久开通的退款冲正：作废订单对应的那笔永久开通，
// 再按剩下的永久开通重算用户的永久身份。
//
// 永久开通不在顺延链上，不需要前移任何一段。找不到对应记录返回 (false, nil)，与限时那支一致。
func revokeLifetimeSegmentForOrderTx(ctx context.Context, tx pgx.Tx, appID int64, userID int64, orderNo string, reason string) (bool, error) {
	orderNo = strings.TrimSpace(orderNo)
	if orderNo == "" {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`, userID, appID); err != nil {
		return false, err
	}
	result, err := tx.Exec(ctx, `UPDATE vip_transactions AS vt SET revoked_at = NOW(), revoke_reason = $5
WHERE vt.id = (
    SELECT vt.id FROM vip_transactions vt
    WHERE vt.appid = $1 AND vt.user_id = $2 AND vt.related_order_no = $3
      AND vt.pay_channel = $4 AND vt.lifetime AND `+vipSegmentScopeSQL+`
    ORDER BY vt.id DESC LIMIT 1
)`, appID, userID, orderNo, vipdomain.ChannelPaymentOrder, reason)
	if err != nil {
		return false, err
	}
	if result.RowsAffected() == 0 {
		return false, nil
	}
	return true, syncVipLifetimeTx(ctx, tx, appID, userID)
}

// syncVipLifetimeTx 按仍生效的永久开通重算 users.vip_lifetime_at（调用方须已锁住用户行）。
//
// 永久身份的权威来源是这一列，但它必须与账本一致：最后一笔永久开通被退款作废之后，
// 这个人就不再是永久会员；还有别的永久开通（例如先送后买）则保持不变，
// 成为永久会员的时间也不变 —— 重新取最早那一笔会把它挪到别处。
func syncVipLifetimeTx(ctx context.Context, tx pgx.Tx, appID int64, userID int64) error {
	_, err := tx.Exec(ctx, `UPDATE users u SET
    vip_lifetime_at = CASE WHEN EXISTS (
        SELECT 1 FROM vip_transactions vt
        WHERE vt.appid = u.appid AND vt.user_id = u.id AND vt.lifetime AND `+vipSegmentScopeSQL+`
    ) THEN COALESCE(u.vip_lifetime_at, NOW()) ELSE NULL END,
    updated_at = NOW()
WHERE u.id = $1 AND u.appid = $2`, userID, appID)
	return err
}

// truncateVipSegmentsTx 扣减天数：把会员链截断在 newEnd。
//
// 扣减是从链尾往回截的，所以：终点在 newEnd 之后的段收到 newEnd，
// 起点就在 newEnd 之后的段整段失效（窗口收成空，不再算数）。
// 不截的话，扣掉的恰好是排在最后的高级版那段时，它的功能还会一直生效到原来的终点。
//
// 永久开通不在这条链上，扣天数动不到它。
func truncateVipSegmentsTx(ctx context.Context, tx pgx.Tx, appID int64, userID int64, newEnd time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE vip_transactions AS vt SET
    active_from = `+vipSegmentFromSQL+`,
    active_until = GREATEST(`+vipSegmentFromSQL+`, LEAST(`+vipSegmentUntilSQL+`, $3))
WHERE vt.appid = $1 AND vt.user_id = $2 AND `+vipSegmentScopeSQL+` AND `+vipSegmentTimedSQL+`
  AND `+vipSegmentUntilSQL+` > $3`,
		appID, userID, newEnd)
	return err
}
