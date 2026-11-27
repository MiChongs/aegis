package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"aegis/internal/domain/coupon"
	paymentdomain "aegis/internal/domain/payment"
)

// 订单上的优惠（docs/coupons.md#订单上的优惠）：
//
//	下单：锁用户 → 预占活动名额与预算 → 占用券（available → locked）→ 写订单 → 写优惠记录，同一事务
//	付款：SettleOrderPromotion —— 券 locked → used、模板计入使用、返券返积分；记录 reserved → settled（幂等）
//	没付款就关闭：ReleaseOrderPromotions —— 订单过期一段时间后券还给用户、名额与预算退回；reserved → released
//	全额退款：reverseOrderPromotionTx —— 名额与预算退回，券按模板设置返还，返出去的券与积分收回；settled → refunded
//
// 释放之后才到账的付款（极少见：渠道过了失效时间还收钱）照样结算：重新计入名额与预算；
// 券若还没被别的订单用掉就记为这一单用了，已经被用掉则在记录上写明（同一张券优惠了两次，钱已经收了，不追）。

var (
	// ErrCouponQuoteChanged 下单时券或活动已经变了（被别的订单占用、名额满、预算用完、活动结束）
	ErrCouponQuoteChanged     = errors.New("coupon quote changed")
	ErrOrderPromotionNotFound = errors.New("order promotion not found")
)

// CouponQuoteChangedError 带上具体哪一项变了。
type CouponQuoteChangedError struct{ Reason string }

func (e *CouponQuoteChangedError) Error() string { return e.Reason }
func (e *CouponQuoteChangedError) Unwrap() error { return ErrCouponQuoteChanged }

func quoteChanged(reason string) error { return &CouponQuoteChangedError{Reason: reason} }

const orderPromotionColumns = `p.id, p.appid, p.user_id, p.order_id, p.order_no, p.purpose, p.original_amount::text, p.promotion_discount::text,
p.coupon_discount::text, p.payable_amount::text, p.promotion_id, p.promotion_name, p.user_coupon_id, p.coupon_name, p.template_id,
p.rebate_promotion_id, p.rebate_promotion_name, p.rebate_status, p.rebate_note, p.rebate_coupon_ids, p.rebate_integral,
p.status, p.note, p.created_at, p.settled_at, p.released_at`

type orderPromotionRow struct {
	coupon.OrderPromotion
	RebateCouponIDs []int64
	RebateIntegral  int64
}

func scanOrderPromotion(row pgx.Row, extra ...any) (*orderPromotionRow, error) {
	var (
		out                                orderPromotionRow
		original, promoD, couponD, payable string
	)
	p := &out.OrderPromotion
	dest := []any{&p.ID, &p.AppID, &p.UserID, &p.OrderID, &p.OrderNo, &p.Purpose, &original, &promoD, &couponD, &payable,
		&p.PromotionID, &p.PromotionName, &p.UserCouponID, &p.CouponName, &p.TemplateID,
		&p.RebatePromotionID, &p.RebatePromotionName, &p.RebateStatus, &p.RebateNote, &out.RebateCouponIDs, &out.RebateIntegral,
		&p.Status, &p.Note, &p.CreatedAt, &p.SettledAt, &p.ReleasedAt}
	dest = append(dest, extra...)
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	p.OriginalAmount, p.PromotionDiscount = decimal.RequireFromString(original), decimal.RequireFromString(promoD)
	p.CouponDiscount, p.PayableAmount = decimal.RequireFromString(couponD), decimal.RequireFromString(payable)
	return &out, nil
}

func idPtr(id int64) *int64 {
	if id <= 0 {
		return nil
	}
	return &id
}

// CreatePaymentOrderWithPromotion 带优惠的下单（单事务）。任何一项预占不上都整笔回滚、返回 *CouponQuoteChangedError。
func (r *Repository) CreatePaymentOrderWithPromotion(ctx context.Context, item paymentdomain.OrderMutation, res coupon.Reservation) (*paymentdomain.Order, error) {
	if item.UserID == nil || *item.UserID <= 0 {
		return nil, fmt.Errorf("create order with promotion: user missing")
	}
	userID := *item.UserID
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := couponLockUserTx(ctx, tx, item.AppID, userID); err != nil {
		return nil, err
	}

	reservePromotion := func(id int64, perUser int, discount decimal.Decimal, column string) (bool, error) {
		if perUser > 0 {
			var used int64
			if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM payment_order_promotions
WHERE appid = $1 AND user_id = $2 AND `+column+` = $3 AND status IN ('reserved', 'settled')`, item.AppID, userID, id).Scan(&used); err != nil {
				return false, err
			}
			if used >= int64(perUser) {
				return false, nil
			}
		}
		tag, err := tx.Exec(ctx, `UPDATE coupon_promotions SET used_count = used_count + 1, discount_total = discount_total + $3, updated_at = NOW()
WHERE id = $1 AND appid = $2 AND status = 'active'
  AND (starts_at IS NULL OR starts_at <= NOW()) AND (ends_at IS NULL OR ends_at > NOW())
  AND (total_limit = 0 OR used_count < total_limit)
  AND (budget = 0 OR discount_total + $3 <= budget)`, id, item.AppID, discount.StringFixed(2))
		if err != nil {
			return false, err
		}
		return tag.RowsAffected() > 0, nil
	}

	if res.PromotionID > 0 {
		ok, err := reservePromotion(res.PromotionID, res.PromotionPerUserLimit, res.PromotionDiscount, "promotion_id")
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, quoteChanged("「" + res.PromotionName + "」已结束或名额已满，请重新确认价格")
		}
	}
	if res.RebatePromotionID > 0 {
		ok, err := reservePromotion(res.RebatePromotionID, res.RebatePerUserLimit, decimal.Zero, "rebate_promotion_id")
		if err != nil {
			return nil, err
		}
		if !ok {
			// 返券是额外的好处，名额满了不挡下单，只是这一单不再返
			res.RebatePromotionID, res.RebatePromotionName = 0, ""
		}
	}
	if res.UserCouponID > 0 {
		tag, err := tx.Exec(ctx, `UPDATE user_coupons SET status = 'locked', order_no = $4, updated_at = NOW()
WHERE id = $1 AND appid = $2 AND user_id = $3 AND status = 'available' AND valid_from <= NOW() AND valid_until > NOW()`,
			res.UserCouponID, item.AppID, userID, item.OrderNo)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 0 {
			return nil, quoteChanged("这张券已被使用或已过期，请重新选择")
		}
	}

	meta, _ := json.Marshal(item.Metadata)
	order, err := scanPaymentOrder(tx.QueryRow(ctx, `INSERT INTO payment_orders (appid, user_id, config_id, order_no, subject, body, amount, payment_method, provider_type, status, notify_status, client_ip, notify_url, return_url, metadata, expire_at, currency, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'pending', 'pending', $10, $11, $12, $13, $14, $15, NOW(), NOW())
RETURNING id, appid, user_id, config_id, order_no, COALESCE(provider_order_no, ''), subject, COALESCE(body, ''), amount, payment_method, provider_type, status, notify_status, COALESCE(client_ip, ''), COALESCE(notify_url, ''), COALESCE(return_url, ''), COALESCE(metadata, '{}'::jsonb), COALESCE(raw_callback, '{}'::jsonb), refunded_amount, refund_status, COALESCE(currency, ''), paid_at, expire_at, created_at, updated_at, paid_amount, COALESCE(review_reason, ''), COALESCE(review_note, ''), review_at, fulfillment_status, fulfilled_at`,
		item.AppID, item.UserID, item.ConfigID, item.OrderNo, item.Subject, nullableString(item.Body), item.Amount.StringFixed(2), item.PaymentMethod,
		item.ProviderType, nullableString(item.ClientIP), nullableString(item.NotifyURL), nullableString(item.ReturnURL), meta, item.ExpireAt,
		strings.ToUpper(strings.TrimSpace(item.Currency))))
	if err != nil {
		return nil, err
	}
	rebateStatus := coupon.RebateNone
	if res.RebatePromotionID > 0 {
		rebateStatus = coupon.RebatePending
	}
	if _, err := tx.Exec(ctx, `INSERT INTO payment_order_promotions (appid, user_id, order_id, order_no, purpose, original_amount, promotion_discount,
  coupon_discount, payable_amount, promotion_id, promotion_name, user_coupon_id, template_id, coupon_name, rebate_promotion_id, rebate_promotion_name, rebate_status)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		item.AppID, userID, order.ID, order.OrderNo, res.Purpose, res.OriginalAmount.StringFixed(2), res.PromotionDiscount.StringFixed(2),
		res.CouponDiscount.StringFixed(2), res.PayableAmount.StringFixed(2), idPtr(res.PromotionID), res.PromotionName,
		idPtr(res.UserCouponID), idPtr(res.TemplateID), res.CouponName, idPtr(res.RebatePromotionID), res.RebatePromotionName, rebateStatus); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return order, nil
}

// SettleOrderPromotion 已付款订单的优惠结算（幂等：只有 reserved / released 的记录会被处理）。没有优惠记录返回 nil, nil。
func (r *Repository) SettleOrderPromotion(ctx context.Context, orderID int64) (*coupon.OrderPromotion, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// 锁序 users → … → payment_order_promotions：先不加锁读出归属用户，锁住用户行后再锁记录
	var userID, appID int64
	if err := tx.QueryRow(ctx, `SELECT user_id, appid FROM payment_order_promotions WHERE order_id = $1`, orderID).Scan(&userID, &appID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if err := couponLockUserTx(ctx, tx, appID, userID); err != nil && !errors.Is(err, ErrUserNotFound) {
		return nil, err
	}
	rec, err := scanOrderPromotion(tx.QueryRow(ctx, `SELECT `+orderPromotionColumns+` FROM payment_order_promotions p WHERE p.order_id = $1 FOR UPDATE`, orderID))
	if err != nil {
		return nil, err
	}
	if rec.Status != coupon.OrderReserved && rec.Status != coupon.OrderReleased {
		return &rec.OrderPromotion, nil
	}
	var orderStatus, refundStatus string
	var paidAmountRaw *string
	if err := tx.QueryRow(ctx, `SELECT status, refund_status, paid_amount::text FROM payment_orders WHERE id = $1`, orderID).Scan(&orderStatus, &refundStatus, &paidAmountRaw); err != nil {
		return nil, err
	}
	if orderStatus != "paid" {
		return &rec.OrderPromotion, nil
	}
	if refundStatus == paymentdomain.OrderRefundFull {
		// 已全额退款（退款时这条记录还没结算）：不再核销、不再返利
		refundUnsettledTx(ctx, tx, rec)
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		rec.Status = coupon.OrderRefunded
		return &rec.OrderPromotion, nil
	}
	released := rec.Status == coupon.OrderReleased
	notes := []string{}

	// 1. 释放过的要重新计入名额与预算（钱已经收了）
	if released {
		if rec.PromotionID != nil {
			if _, err := tx.Exec(ctx, `UPDATE coupon_promotions SET used_count = used_count + 1, discount_total = discount_total + $2, updated_at = NOW() WHERE id = $1`,
				*rec.PromotionID, rec.PromotionDiscount.StringFixed(2)); err != nil {
				return nil, err
			}
		}
		if rec.RebatePromotionID != nil {
			if _, err := tx.Exec(ctx, `UPDATE coupon_promotions SET used_count = used_count + 1, updated_at = NOW() WHERE id = $1`, *rec.RebatePromotionID); err != nil {
				return nil, err
			}
		}
		notes = append(notes, "订单关闭后才到账，已重新计入活动")
	}

	// 2. 券：本单占着的 → 已使用；释放后还没被别人用掉的 → 记为本单使用；已被别的订单用掉 → 写明
	if rec.UserCouponID != nil {
		tag, err := tx.Exec(ctx, `UPDATE user_coupons SET status = 'used', order_no = $2, used_at = NOW(), discount = $3, updated_at = NOW()
WHERE id = $1 AND ((status = 'locked' AND order_no = $2) OR status IN ('available', 'expired'))`,
			*rec.UserCouponID, rec.OrderNo, rec.CouponDiscount.StringFixed(2))
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() > 0 {
			if rec.TemplateID != nil {
				if _, err := tx.Exec(ctx, `UPDATE coupon_templates SET used_count = used_count + 1, discount_total = discount_total + $2, updated_at = NOW() WHERE id = $1`,
					*rec.TemplateID, rec.CouponDiscount.StringFixed(2)); err != nil {
					return nil, err
				}
			}
		} else {
			notes = append(notes, "这张券在订单关闭后已被另一笔订单使用（本单仍按下单时的优惠价收款）")
		}
	}

	// 3. 返券返积分
	if released && rec.RebateStatus == coupon.RebateSkipped {
		// 释放时标成了不再返；钱既然到了，名额也重新计入了，返利照发
		rec.RebateStatus = coupon.RebatePending
	}
	rebateStatus, rebateNote := rec.RebateStatus, rec.RebateNote
	var rebateCouponIDs []int64
	var rebateIntegral int64
	if rec.RebatePromotionID != nil && rec.RebateStatus == coupon.RebatePending {
		paid := rec.PayableAmount
		if paidAmountRaw != nil {
			if v, err := decimal.NewFromString(*paidAmountRaw); err == nil && v.IsPositive() {
				paid = v
			}
		}
		rebateStatus, rebateNote, rebateCouponIDs, rebateIntegral = r.grantOrderRebate(ctx, tx, rec, paid)
	}

	if _, err := tx.Exec(ctx, `UPDATE payment_order_promotions SET status = 'settled', settled_at = NOW(), rebate_status = $2, rebate_note = $3,
  rebate_coupon_ids = $4, rebate_integral = $5, note = $6, updated_at = NOW() WHERE id = $1`,
		rec.ID, rebateStatus, couponTruncate(rebateNote, 400), nonNilIDs(rebateCouponIDs), rebateIntegral, couponTruncate(strings.Join(notes, "；"), 400)); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	rec.Status, rec.RebateStatus, rec.RebateNote = coupon.OrderSettled, rebateStatus, rebateNote
	return &rec.OrderPromotion, nil
}

func nonNilIDs(ids []int64) []int64 {
	if ids == nil {
		return []int64{}
	}
	return ids
}

// grantOrderRebate 发返券返积分。放在保存点里：券发完了、活动删了之类的失败只记在这一单上，不挡结单。
func (r *Repository) grantOrderRebate(ctx context.Context, outer pgx.Tx, rec *orderPromotionRow, paid decimal.Decimal) (string, string, []int64, int64) {
	p, err := scanCouponPromotion(outer.QueryRow(ctx, `SELECT `+couponPromotionColumns+` FROM coupon_promotions WHERE id = $1`, *rec.RebatePromotionID))
	if err != nil {
		return coupon.RebateFailed, "返券活动已不存在", nil, 0
	}
	if paid.LessThan(p.Rule.MinAmount) {
		return coupon.RebateSkipped, "实付 " + paid.StringFixed(2) + " 未达到返券门槛 " + p.Rule.MinAmount.StringFixed(2), nil, 0
	}
	sp, err := outer.Begin(ctx)
	if err != nil {
		return coupon.RebateFailed, "发放失败：" + err.Error(), nil, 0
	}
	ids, integral, err := grantRebateItemsTx(ctx, sp, rec, p)
	if err != nil {
		_ = sp.Rollback(ctx)
		var unavailable *CouponUnavailableError
		if errors.As(err, &unavailable) {
			return coupon.RebateFailed, unavailable.Reason, nil, 0
		}
		return coupon.RebateFailed, "发放失败：" + err.Error(), nil, 0
	}
	if err := sp.Commit(ctx); err != nil {
		return coupon.RebateFailed, "发放失败：" + err.Error(), nil, 0
	}
	parts := []string{}
	if len(ids) > 0 {
		parts = append(parts, fmt.Sprintf("返券 %d 张", len(ids)))
	}
	if integral > 0 {
		parts = append(parts, fmt.Sprintf("返积分 %d", integral))
	}
	return coupon.RebateGranted, strings.Join(parts, "，"), ids, integral
}

func grantRebateItemsTx(ctx context.Context, tx pgx.Tx, rec *orderPromotionRow, p *coupon.Promotion) ([]int64, int64, error) {
	ids := []int64{}
	for _, item := range p.Rule.RebateCoupons {
		granted, err := grantCouponTx(ctx, tx, coupon.GrantInput{
			AppID: rec.AppID, UserID: rec.UserID, TemplateID: item.TemplateID, Count: item.Count,
			Source: coupon.SourceRebate, SourceRef: rec.OrderNo, IgnoreStatus: true,
		})
		if err != nil {
			return nil, 0, err
		}
		for _, c := range granted.Coupons {
			ids = append(ids, c.ID)
		}
	}
	var integral int64
	if p.Rule.RebateIntegral > 0 {
		orderID := rec.OrderID
		if _, _, _, err := applyIntegralChangeTx(ctx, tx, rec.UserID, rec.AppID, p.Rule.RebateIntegral,
			"earn", "coupon_rebate", "下单返积分", p.Name, "payment_order", &orderID,
			map[string]any{"orderNo": rec.OrderNo, "promotionId": p.ID}); err != nil {
			return nil, 0, err
		}
		integral = p.Rule.RebateIntegral
	}
	return ids, integral, nil
}

// ReleaseOrderPromotions 没付款的订单过期满 grace 之后，把它占着的券与活动名额还回去。返回处理了几笔。
func (r *Repository) ReleaseOrderPromotions(ctx context.Context, grace time.Duration, limit int) (int, error) {
	if limit < 1 {
		limit = 200
	}
	rows, err := r.pool.Query(ctx, `SELECT p.order_id FROM payment_order_promotions p JOIN payment_orders o ON o.id = p.order_id
WHERE p.status = 'reserved' AND o.status <> 'paid' AND o.expire_at IS NOT NULL AND o.expire_at < $1
ORDER BY p.created_at LIMIT $2`, time.Now().Add(-grace), limit)
	if err != nil {
		return 0, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	done := 0
	for _, id := range ids {
		ok, err := r.releaseOrderPromotion(ctx, id, grace, false)
		if err != nil {
			return done, err
		}
		if ok {
			done++
		}
	}
	return done, nil
}

// ReleaseFailedOrderPromotion 下单后没能交给付款方的订单（渠道下单报错、余额不足）：把订单标成失败并立刻还回券与名额。
// 这种单从没出现在用户的付款页上，不会有晚到的付款，不必等过期。
func (r *Repository) ReleaseFailedOrderPromotion(ctx context.Context, orderID int64) error {
	if _, err := r.pool.Exec(ctx, `UPDATE payment_orders SET status = 'failed', updated_at = NOW() WHERE id = $1 AND status = 'pending'`, orderID); err != nil {
		return err
	}
	_, err := r.releaseOrderPromotion(ctx, orderID, 0, true)
	return err
}

func (r *Repository) releaseOrderPromotion(ctx context.Context, orderID int64, grace time.Duration, force bool) (bool, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var userID, appID int64
	if err := tx.QueryRow(ctx, `SELECT user_id, appid FROM payment_order_promotions WHERE order_id = $1`, orderID).Scan(&userID, &appID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	if err := couponLockUserTx(ctx, tx, appID, userID); err != nil && !errors.Is(err, ErrUserNotFound) {
		return false, err
	}
	rec, err := scanOrderPromotion(tx.QueryRow(ctx, `SELECT `+orderPromotionColumns+` FROM payment_order_promotions p WHERE p.order_id = $1 FOR UPDATE SKIP LOCKED`, orderID))
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if rec.Status != coupon.OrderReserved {
		return false, nil
	}
	// 订单行上再确认一次：没付款、确实过期满了宽限期
	var releasable bool
	if err := tx.QueryRow(ctx, `SELECT status <> 'paid' AND ($3 OR (expire_at IS NOT NULL AND expire_at < $2)) FROM payment_orders WHERE id = $1 FOR UPDATE`,
		orderID, time.Now().Add(-grace), force).Scan(&releasable); err != nil {
		return false, err
	}
	if !releasable {
		return false, nil
	}
	if err := unreservePromotionsTx(ctx, tx, rec); err != nil {
		return false, err
	}
	if rec.UserCouponID != nil {
		if _, err := tx.Exec(ctx, `UPDATE user_coupons SET status = CASE WHEN valid_until <= NOW() THEN 'expired' ELSE 'available' END,
  order_no = NULL, updated_at = NOW() WHERE id = $1 AND status = 'locked' AND order_no = $2`, *rec.UserCouponID, rec.OrderNo); err != nil {
			return false, err
		}
	}
	rebate := rec.RebateStatus
	if rebate == coupon.RebatePending {
		rebate = coupon.RebateSkipped
	}
	if _, err := tx.Exec(ctx, `UPDATE payment_order_promotions SET status = 'released', released_at = NOW(), rebate_status = $2, updated_at = NOW() WHERE id = $1`,
		rec.ID, rebate); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

func unreservePromotionsTx(ctx context.Context, tx pgx.Tx, rec *orderPromotionRow) error {
	if rec.PromotionID != nil {
		if _, err := tx.Exec(ctx, `UPDATE coupon_promotions SET used_count = GREATEST(used_count - 1, 0),
  discount_total = GREATEST(discount_total - $2, 0), updated_at = NOW() WHERE id = $1`, *rec.PromotionID, rec.PromotionDiscount.StringFixed(2)); err != nil {
			return err
		}
	}
	if rec.RebatePromotionID != nil {
		if _, err := tx.Exec(ctx, `UPDATE coupon_promotions SET used_count = GREATEST(used_count - 1, 0), updated_at = NOW() WHERE id = $1`, *rec.RebatePromotionID); err != nil {
			return err
		}
	}
	return nil
}

// reverseOrderPromotionTx 订单全额退款后的优惠冲正，在退款结算的事务里调用。
//
// 放在保存点里、失败只写进记录：退款已经成功了，不能因为收不回一张返券就让退款结算失败（与履约冲正同一个原则）。
// current 是这一笔正在结算成功的退款金额（它的状态在调用之后才写成 success）。
// 是否全额按「已成功的退款 + 这一笔」判：payment_orders.refunded_amount 是建退款单时就占用的额度，
// 还有别的退款在处理中时它已经等于订单金额，拿它判会把部分退款当成全额。
// 返回一句给退款单看的说明（没有优惠记录或不是全额退款时为空）。
func reverseOrderPromotionTx(ctx context.Context, outer pgx.Tx, orderID int64, current decimal.Decimal) string {
	var amountRaw, succeededRaw string
	if err := outer.QueryRow(ctx, `SELECT o.amount::text,
  COALESCE((SELECT SUM(r.amount) FROM payment_refunds r WHERE r.order_id = o.id AND r.status = 'success'), 0)::text
FROM payment_orders o WHERE o.id = $1`, orderID).Scan(&amountRaw, &succeededRaw); err != nil {
		return ""
	}
	if decimal.RequireFromString(succeededRaw).Add(current).LessThan(decimal.RequireFromString(amountRaw)) {
		return ""
	}
	sp, err := outer.Begin(ctx)
	if err != nil {
		return ""
	}
	note, err := reverseOrderPromotionInner(ctx, sp, orderID)
	if err != nil {
		_ = sp.Rollback(ctx)
		return "优惠冲正失败：" + err.Error()
	}
	if err := sp.Commit(ctx); err != nil {
		return "优惠冲正失败：" + err.Error()
	}
	return note
}

func reverseOrderPromotionInner(ctx context.Context, tx pgx.Tx, orderID int64) (string, error) {
	// 锁序与下单、结算一致：先用户行，再优惠记录
	var userID, appID int64
	if err := tx.QueryRow(ctx, `SELECT user_id, appid FROM payment_order_promotions WHERE order_id = $1`, orderID).Scan(&userID, &appID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	if err := couponLockUserTx(ctx, tx, appID, userID); err != nil && !errors.Is(err, ErrUserNotFound) {
		return "", err
	}
	rec, err := scanOrderPromotion(tx.QueryRow(ctx, `SELECT `+orderPromotionColumns+` FROM payment_order_promotions p WHERE p.order_id = $1 FOR UPDATE`, orderID))
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	switch rec.Status {
	case coupon.OrderSettled:
	case coupon.OrderReserved, coupon.OrderReleased:
		// 付了款却还没来得及结算就全额退了：不再结算，按没用过处理
		return "优惠：" + refundUnsettledTx(ctx, tx, rec), nil
	default:
		return "", nil
	}
	if err := unreservePromotionsTx(ctx, tx, rec); err != nil {
		return "", err
	}
	notes := []string{}
	if rec.UserCouponID != nil && rec.TemplateID != nil {
		var refundReturn bool
		if err := tx.QueryRow(ctx, `SELECT refund_return FROM coupon_templates WHERE id = $1`, *rec.TemplateID).Scan(&refundReturn); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", err
		}
		if refundReturn {
			tag, err := tx.Exec(ctx, `UPDATE user_coupons SET status = 'available', order_no = NULL, used_at = NULL, discount = 0, updated_at = NOW()
WHERE id = $1 AND status = 'used' AND order_no = $2 AND valid_until > NOW()`, *rec.UserCouponID, rec.OrderNo)
			if err != nil {
				return "", err
			}
			if tag.RowsAffected() > 0 {
				if _, err := tx.Exec(ctx, `UPDATE coupon_templates SET used_count = GREATEST(used_count - 1, 0),
  discount_total = GREATEST(discount_total - $2, 0), updated_at = NOW() WHERE id = $1`, *rec.TemplateID, rec.CouponDiscount.StringFixed(2)); err != nil {
					return "", err
				}
				notes = append(notes, "券已返还")
			} else {
				notes = append(notes, "券已过期，未返还")
			}
		}
	}
	rebateStatus := rec.RebateStatus
	if rec.RebateStatus == coupon.RebateGranted {
		if len(rec.RebateCouponIDs) > 0 {
			// 被别的待支付订单占着的也收回：那笔订单关闭后券不会再回到可用；它要是付了款，结算时会写明券已不在
			tag, err := tx.Exec(ctx, `UPDATE user_coupons SET status = 'revoked', revoked_reason = '订单退款，收回返券', updated_at = NOW()
WHERE id = ANY($1) AND status IN ('available', 'expired', 'locked')`, rec.RebateCouponIDs)
			if err != nil {
				return "", err
			}
			if kept := int64(len(rec.RebateCouponIDs)) - tag.RowsAffected(); kept > 0 {
				notes = append(notes, fmt.Sprintf("%d 张返券已被使用，未收回", kept))
			}
		}
		if rec.RebateIntegral > 0 {
			if note := revokeRebateIntegralTx(ctx, tx, rec); note != "" {
				notes = append(notes, note)
			}
		}
		rebateStatus = coupon.RebateRevoked
	}
	if _, err := tx.Exec(ctx, `UPDATE payment_order_promotions SET status = 'refunded', rebate_status = $2,
  note = CASE WHEN note = '' THEN $3 ELSE note || '；' || $3 END, updated_at = NOW() WHERE id = $1`,
		rec.ID, rebateStatus, couponTruncate("全额退款："+strings.Join(append(notes, "活动名额已退回"), "，"), 200)); err != nil {
		return "", err
	}
	return "优惠：" + strings.Join(append(notes, "活动名额已退回"), "，"), nil
}

// ListUnsettledOrderPromotions 已付款但优惠还没结算的订单（结算失败后的重试用）。
func (r *Repository) ListUnsettledOrderPromotions(ctx context.Context, limit int) ([]int64, error) {
	if limit < 1 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `SELECT p.order_id FROM payment_order_promotions p JOIN payment_orders o ON o.id = p.order_id
WHERE p.status IN ('reserved', 'released') AND o.status = 'paid' AND o.refund_status <> 'full' ORDER BY p.created_at LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetOrderPromotion 按订单号取优惠记录。
func (r *Repository) GetOrderPromotion(ctx context.Context, appID int64, orderNo string) (*coupon.OrderPromotion, error) {
	rec, err := scanOrderPromotion(r.pool.QueryRow(ctx, `SELECT `+orderPromotionColumns+` FROM payment_order_promotions p WHERE p.appid = $1 AND p.order_no = $2`, appID, orderNo))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrOrderPromotionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &rec.OrderPromotion, nil
}

// ListOrderPromotions 订单优惠记录（管理端，带用户与订单状态）。
func (r *Repository) ListOrderPromotions(ctx context.Context, q coupon.OrderPromotionQuery) (coupon.Page[coupon.OrderPromotion], error) {
	page, limit, offset := couponPageBounds(q.Page, q.Limit, 200)
	where := `WHERE p.appid = $1`
	args := []any{q.AppID}
	add := func(clause string, value any) {
		args = append(args, value)
		where += fmt.Sprintf(clause, len(args))
	}
	if q.UserID > 0 {
		add(" AND p.user_id = $%d", q.UserID)
	}
	if q.PromotionID > 0 {
		args = append(args, q.PromotionID)
		where += fmt.Sprintf(" AND (p.promotion_id = $%d OR p.rebate_promotion_id = $%d)", len(args), len(args))
	}
	if q.TemplateID > 0 {
		add(" AND p.template_id = $%d", q.TemplateID)
	}
	if q.Status != "" {
		add(" AND p.status = $%d", q.Status)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		args = append(args, "%"+kw+"%")
		n := len(args)
		where += fmt.Sprintf(" AND (p.order_no ILIKE $%d OR u.account ILIKE $%d OR COALESCE(up.nickname, '') ILIKE $%d OR o.subject ILIKE $%d)", n, n, n, n)
	}
	from := ` FROM payment_order_promotions p JOIN payment_orders o ON o.id = p.order_id JOIN users u ON u.id = p.user_id LEFT JOIN user_profiles up ON up.user_id = u.id `
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+from+where, args...).Scan(&total); err != nil {
		return coupon.Page[coupon.OrderPromotion]{}, err
	}
	args = append(args, limit, offset)
	rows, err := r.pool.Query(ctx, `SELECT `+orderPromotionColumns+`, o.subject, o.status, u.account, COALESCE(up.nickname, '')`+from+where+
		fmt.Sprintf(` ORDER BY p.id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return coupon.Page[coupon.OrderPromotion]{}, err
	}
	defer rows.Close()
	items := []coupon.OrderPromotion{}
	for rows.Next() {
		var subject, status, account, nickname string
		rec, err := scanOrderPromotion(rows, &subject, &status, &account, &nickname)
		if err != nil {
			return coupon.Page[coupon.OrderPromotion]{}, err
		}
		rec.Subject, rec.OrderStatus, rec.UserAccount, rec.UserNickname = subject, status, account, nickname
		items = append(items, rec.OrderPromotion)
	}
	if err := rows.Err(); err != nil {
		return coupon.Page[coupon.OrderPromotion]{}, err
	}
	return coupon.NewPage(items, total, page, limit), nil
}

// CouponOverview 优惠概览：总量、近 days 天趋势（按 tz 分日）、排行与发券来源。
func (r *Repository) CouponOverview(ctx context.Context, appID int64, tz string, days int) (*coupon.Overview, error) {
	if days < 1 || days > 90 {
		days = 14
	}
	if tz == "" {
		tz = "UTC"
	}
	if _, err := time.LoadLocation(tz); err != nil {
		tz = "UTC"
	}
	out := &coupon.Overview{Trend: []coupon.TrendPoint{}, TopTemplates: []coupon.RankItem{}, TopPromotions: []coupon.RankItem{}, Sources: []coupon.SourceCount{}}
	var paid, original, couponD, promoD string
	if err := r.pool.QueryRow(ctx, `SELECT
  (SELECT COUNT(*) FROM coupon_templates WHERE appid = $1),
  (SELECT COUNT(*) FROM coupon_templates WHERE appid = $1 AND status = 'active'),
  (SELECT COUNT(*) FROM coupon_promotions WHERE appid = $1),
  (SELECT COUNT(*) FROM coupon_promotions WHERE appid = $1 AND status = 'active' AND (ends_at IS NULL OR ends_at > NOW())),
  (SELECT COUNT(*) FROM user_coupons WHERE appid = $1),
  (SELECT COUNT(*) FROM user_coupons WHERE appid = $1 AND status = 'used'),
  (SELECT COUNT(*) FROM user_coupons WHERE appid = $1 AND status IN ('available', 'locked') AND valid_until > NOW()),
  (SELECT COUNT(*) FROM user_coupons WHERE appid = $1 AND (status = 'expired' OR (status = 'available' AND valid_until <= NOW())))`, appID).
		Scan(&out.Templates, &out.ActiveTemplates, &out.Promotions, &out.ActivePromotions, &out.Issued, &out.Used, &out.Available, &out.Expired); err != nil {
		return nil, err
	}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*), COALESCE(SUM(payable_amount), 0)::text, COALESCE(SUM(original_amount), 0)::text,
  COALESCE(SUM(coupon_discount), 0)::text, COALESCE(SUM(promotion_discount), 0)::text, COALESCE(SUM(rebate_integral), 0),
  COALESCE(SUM(cardinality(rebate_coupon_ids)), 0)
FROM payment_order_promotions WHERE appid = $1 AND status = 'settled'`, appID).
		Scan(&out.Orders, &paid, &original, &couponD, &promoD, &out.RebateIntegral, &out.RebateCoupons); err != nil {
		return nil, err
	}
	out.PaidAmount, out.OriginalAmount = decimal.RequireFromString(paid), decimal.RequireFromString(original)
	out.CouponDiscount, out.PromoDiscount = decimal.RequireFromString(couponD), decimal.RequireFromString(promoD)

	rows, err := r.pool.Query(ctx, `WITH days AS (
  SELECT generate_series((NOW() AT TIME ZONE $2)::date - ($3::int - 1), (NOW() AT TIME ZONE $2)::date, '1 day')::date AS d
), issued AS (
  SELECT (created_at AT TIME ZONE $2)::date AS d, COUNT(*) AS n FROM user_coupons
  WHERE appid = $1 AND created_at >= NOW() - make_interval(days => $3::int + 1) GROUP BY 1
), used AS (
  SELECT (used_at AT TIME ZONE $2)::date AS d, COUNT(*) AS n FROM user_coupons
  WHERE appid = $1 AND status = 'used' AND used_at >= NOW() - make_interval(days => $3::int + 1) GROUP BY 1
), orders AS (
  SELECT (settled_at AT TIME ZONE $2)::date AS d, COUNT(*) AS n, SUM(payable_amount) AS paid, SUM(promotion_discount + coupon_discount) AS disc
  FROM payment_order_promotions WHERE appid = $1 AND status = 'settled' AND settled_at >= NOW() - make_interval(days => $3::int + 1) GROUP BY 1
)
SELECT to_char(days.d, 'YYYY-MM-DD'), COALESCE(issued.n, 0), COALESCE(used.n, 0), COALESCE(orders.n, 0),
  COALESCE(orders.paid, 0)::text, COALESCE(orders.disc, 0)::text
FROM days LEFT JOIN issued ON issued.d = days.d LEFT JOIN used ON used.d = days.d LEFT JOIN orders ON orders.d = days.d
ORDER BY days.d`, appID, tz, days)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var point coupon.TrendPoint
		var paidRaw, discRaw string
		if err := rows.Scan(&point.Date, &point.Issued, &point.Used, &point.Orders, &paidRaw, &discRaw); err != nil {
			rows.Close()
			return nil, err
		}
		point.Paid, point.Discount = decimal.RequireFromString(paidRaw), decimal.RequireFromString(discRaw)
		out.Trend = append(out.Trend, point)
	}
	rows.Close()

	rank := func(query string, into *[]coupon.RankItem) error {
		rows, err := r.pool.Query(ctx, query, appID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item coupon.RankItem
			var discRaw, paidRaw string
			if err := rows.Scan(&item.ID, &item.Name, &item.Count, &discRaw, &paidRaw); err != nil {
				return err
			}
			item.Discount, item.Paid = decimal.RequireFromString(discRaw), decimal.RequireFromString(paidRaw)
			*into = append(*into, item)
		}
		return rows.Err()
	}
	if err := rank(`SELECT t.id, t.name, COUNT(p.id), COALESCE(SUM(p.coupon_discount), 0)::text, COALESCE(SUM(p.payable_amount), 0)::text
FROM coupon_templates t JOIN payment_order_promotions p ON p.template_id = t.id AND p.status = 'settled'
WHERE t.appid = $1 GROUP BY t.id, t.name ORDER BY COUNT(p.id) DESC, t.id DESC LIMIT 6`, &out.TopTemplates); err != nil {
		return nil, err
	}
	if err := rank(`SELECT c.id, c.name, COUNT(p.id), COALESCE(SUM(CASE WHEN p.promotion_id = c.id THEN p.promotion_discount ELSE 0 END), 0)::text,
  COALESCE(SUM(p.payable_amount), 0)::text
FROM coupon_promotions c JOIN payment_order_promotions p ON (p.promotion_id = c.id OR p.rebate_promotion_id = c.id) AND p.status = 'settled'
WHERE c.appid = $1 GROUP BY c.id, c.name ORDER BY COUNT(p.id) DESC, c.id DESC LIMIT 6`, &out.TopPromotions); err != nil {
		return nil, err
	}
	srcRows, err := r.pool.Query(ctx, `SELECT source, COUNT(*) FROM user_coupons WHERE appid = $1 GROUP BY source ORDER BY COUNT(*) DESC`, appID)
	if err != nil {
		return nil, err
	}
	defer srcRows.Close()
	for srcRows.Next() {
		var item coupon.SourceCount
		if err := srcRows.Scan(&item.Source, &item.Count); err != nil {
			return nil, err
		}
		out.Sources = append(out.Sources, item)
	}
	return out, srcRows.Err()
}

// revokeRebateIntegralTx 退款收回返积分，尽力而为：积分已被花掉时扣到 0 为止，差额写进说明。
// 各自在保存点里做：扣不动不能连累名额退回与券的返还。
func revokeRebateIntegralTx(ctx context.Context, tx pgx.Tx, rec *orderPromotionRow) string {
	deduct := func(amount int64) error {
		sp, err := tx.Begin(ctx)
		if err != nil {
			return err
		}
		if err := deductRebateIntegralTx(ctx, sp, rec, amount); err != nil {
			_ = sp.Rollback(ctx)
			return err
		}
		return sp.Commit(ctx)
	}
	if err := deduct(rec.RebateIntegral); err == nil {
		return ""
	}
	var balance int64
	if err := tx.QueryRow(ctx, `SELECT integral FROM users WHERE id = $1 AND appid = $2`, rec.UserID, rec.AppID).Scan(&balance); err != nil {
		return "返积分未扣回：" + err.Error()
	}
	taken := min(balance, rec.RebateIntegral)
	if taken > 0 {
		if err := deduct(taken); err != nil {
			return "返积分未扣回：" + err.Error()
		}
	}
	return fmt.Sprintf("返积分已被使用，扣回 %d、差 %d", taken, rec.RebateIntegral-taken)
}

// refundUnsettledTx 付了款、还没结算优惠就全额退款的订单：名额与预算退回（还占着的话），券还给用户，不再发返利。
func refundUnsettledTx(ctx context.Context, tx pgx.Tx, rec *orderPromotionRow) string {
	if rec.Status == coupon.OrderReserved {
		if err := unreservePromotionsTx(ctx, tx, rec); err != nil {
			return "退回活动名额失败：" + err.Error()
		}
	}
	if rec.UserCouponID != nil {
		if _, err := tx.Exec(ctx, `UPDATE user_coupons SET status = CASE WHEN valid_until <= NOW() THEN 'expired' ELSE 'available' END,
  order_no = NULL, updated_at = NOW() WHERE id = $1 AND status = 'locked' AND order_no = $2`, *rec.UserCouponID, rec.OrderNo); err != nil {
			return "返还券失败：" + err.Error()
		}
	}
	rebate := rec.RebateStatus
	if rebate == coupon.RebatePending {
		rebate = coupon.RebateSkipped
	}
	if _, err := tx.Exec(ctx, `UPDATE payment_order_promotions SET status = 'refunded', rebate_status = $2,
  note = '付款后未结算即全额退款：券已返还、名额已退回、不再返利', updated_at = NOW() WHERE id = $1`, rec.ID, rebate); err != nil {
		return "更新记录失败：" + err.Error()
	}
	return "付款后未结算即全额退款：券已返还、名额已退回"
}

func deductRebateIntegralTx(ctx context.Context, tx pgx.Tx, rec *orderPromotionRow, amount int64) error {
	orderID := rec.OrderID
	_, _, _, err := applyIntegralChangeTx(ctx, tx, rec.UserID, rec.AppID, -amount,
		"consume", "coupon_rebate_refund", "订单退款收回返积分", rec.RebatePromotionName, "payment_order", &orderID,
		map[string]any{"orderNo": rec.OrderNo, "reason": "payment_refund"})
	return err
}
