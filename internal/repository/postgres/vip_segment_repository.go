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
const (
	vipSegmentFromSQL  = `COALESCE(vt.active_from, GREATEST(COALESCE(vt.expire_before, vt.created_at), vt.created_at))`
	vipSegmentUntilSQL = `COALESCE(vt.active_until, vt.expire_after)`
)

// vipSegmentScopeSQL 「这一行是一段会员期」：正时长、未作废。
// 扣减记录（admin_revoke）是负时长的账，不是一段会员期。
const vipSegmentScopeSQL = `vt.duration_days > 0 AND vt.revoked_at IS NULL`

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
        'activeUntil', ` + vipSegmentUntilSQL + `
    ) ORDER BY vt.id)
    FROM vip_transactions vt
    LEFT JOIN vip_plans p ON p.id = vt.plan_id AND p.appid = vt.appid
    WHERE vt.appid = u.appid AND vt.user_id = u.id AND ` + vipSegmentScopeSQL + `
      AND ` + vipSegmentUntilSQL + ` > NOW()
      AND ` + vipSegmentUntilSQL + ` > ` + vipSegmentFromSQL + `
), '[]'::jsonb)`

// vipFeatureCatalogSQL 本应用启用中的功能标识。功能权益只在这个集合里取。
const vipFeatureCatalogSQL = `ARRAY(SELECT f.tag FROM vip_features f WHERE f.appid = u.appid AND f.is_active ORDER BY f.tag)`

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
  AND vt.pay_channel = $4 AND `+vipSegmentScopeSQL+`
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
WHERE vt.appid = $1 AND vt.user_id = $2 AND vt.id <> $3 AND `+vipSegmentScopeSQL+`
  AND `+vipSegmentFromSQL+` >= $5`,
		appID, userID, segmentID, days, segmentUntil)
	return true, err
}

// truncateVipSegmentsTx 扣减天数：把会员链截断在 newEnd。
//
// 扣减是从链尾往回截的，所以：终点在 newEnd 之后的段收到 newEnd，
// 起点就在 newEnd 之后的段整段失效（窗口收成空，不再算数）。
// 不截的话，扣掉的恰好是排在最后的高级版那段时，它的功能还会一直生效到原来的终点。
func truncateVipSegmentsTx(ctx context.Context, tx pgx.Tx, appID int64, userID int64, newEnd time.Time) error {
	_, err := tx.Exec(ctx, `UPDATE vip_transactions AS vt SET
    active_from = `+vipSegmentFromSQL+`,
    active_until = GREATEST(`+vipSegmentFromSQL+`, LEAST(`+vipSegmentUntilSQL+`, $3))
WHERE vt.appid = $1 AND vt.user_id = $2 AND `+vipSegmentScopeSQL+`
  AND `+vipSegmentUntilSQL+` > $3`,
		appID, userID, newEnd)
	return err
}
