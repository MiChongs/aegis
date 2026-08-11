package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	paymentdomain "aegis/internal/domain/payment"
	walletdomain "aegis/internal/domain/wallet"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// ── 退款额度与状态错误 ──

var (
	// ErrOrderNotRefundable 订单不处于可退款状态（未支付 / 已过期 / 已关闭）
	ErrOrderNotRefundable = errors.New("postgres: payment order not refundable")
	// ErrRefundAmountExceeded 本次退款额超出订单剩余可退额度
	ErrRefundAmountExceeded = errors.New("postgres: refund amount exceeds refundable balance")
	// ErrRefundNotSettleable 退款单已处于终态，不可重复结算
	ErrRefundNotSettleable = errors.New("postgres: payment refund already settled")
)

const paymentRefundColumns = `id, appid, order_id, order_no, refund_no, COALESCE(provider_refund_no, ''), user_id,
payment_method, amount, COALESCE(reason, ''), status, reversal_status, COALESCE(reversal_message, ''),
COALESCE(operator, ''), COALESCE(client_ip, ''), COALESCE(error_message, ''),
COALESCE(raw_response, '{}'::jsonb), refunded_at, created_at, updated_at`

// CreatePaymentRefund 创建退款单并**预占退款额度**（单事务）。
//
// 关键点：`payment_orders.refunded_amount` 记录的是「已占用额度」而不是「已成功退款额」。
// 创建退款单时即累加，上游失败时再释放（SettlePaymentRefund）。
// 这样两个并发退款请求不可能把同一笔钱退两次 —— 第二个请求在行锁后看到的是
// 已被第一个请求抬高的 refunded_amount，超额即被拒。
func (r *Repository) CreatePaymentRefund(ctx context.Context, creation paymentdomain.RefundCreation) (*paymentdomain.Refund, error) {
	if creation.Order == nil {
		return nil, fmt.Errorf("create payment refund: order missing")
	}
	if !creation.Amount.IsPositive() {
		return nil, fmt.Errorf("create payment refund: amount must be positive")
	}

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	// 锁定订单行：并发退款在此串行化
	var status string
	var amountRaw, refundedRaw string
	if err := tx.QueryRow(ctx,
		`SELECT status, amount, refunded_amount FROM payment_orders WHERE id = $1 AND appid = $2 FOR UPDATE`,
		creation.Order.ID, creation.AppID).Scan(&status, &amountRaw, &refundedRaw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrOrderNotRefundable
		}
		return nil, err
	}
	if status != "paid" {
		return nil, ErrOrderNotRefundable
	}

	orderAmount := decimal.RequireFromString(amountRaw)
	refunded := decimal.RequireFromString(refundedRaw)
	if refunded.Add(creation.Amount).GreaterThan(orderAmount) {
		return nil, ErrRefundAmountExceeded
	}

	newRefunded := refunded.Add(creation.Amount)
	if _, err := tx.Exec(ctx,
		`UPDATE payment_orders SET refunded_amount = $2, refund_status = $3, updated_at = NOW() WHERE id = $1`,
		creation.Order.ID, newRefunded.StringFixed(2), orderRefundStatus(newRefunded, orderAmount)); err != nil {
		return nil, err
	}

	refund, err := scanPaymentRefund(tx.QueryRow(ctx,
		`INSERT INTO payment_refunds (appid, order_id, order_no, refund_no, user_id, payment_method,
amount, reason, status, reversal_status, operator, client_ip, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW(), NOW())
RETURNING `+paymentRefundColumns,
		creation.AppID, creation.Order.ID, creation.Order.OrderNo, creation.RefundNo,
		creation.Order.UserID, creation.Order.PaymentMethod, creation.Amount.StringFixed(2),
		nullableString(creation.Reason), paymentdomain.RefundPending, paymentdomain.ReversalNone,
		nullableString(creation.Operator), nullableString(creation.ClientIP)))
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return refund, nil
}

// SettlePaymentRefund 回写上游退款结果（单事务，幂等）。
//
//   - success：置成功并按需执行履约冲正，额度保持占用
//   - failed / closed：**释放**预占额度，订单重新可退（同一笔钱可换渠道重试）
//   - processing：仅记录上游单号，额度继续占用（结果由后续查询/通知确定）
//
// reverseEnabled=false 表示操作方显式选择不冲正，此时若订单确有履约则记 skipped。
func (r *Repository) SettlePaymentRefund(
	ctx context.Context,
	settlement paymentdomain.RefundSettlement,
	order *paymentdomain.Order,
	reverse *paymentdomain.FulfillmentInstruction,
	reverseEnabled bool,
) (*paymentdomain.Refund, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	// 锁定退款单，拦下重复结算（上游重试 / 补偿轮询与通知并发）
	var currentStatus string
	var refundAmountRaw string
	var orderID int64
	if err := tx.QueryRow(ctx,
		`SELECT status, amount, order_id FROM payment_refunds WHERE id = $1 FOR UPDATE`,
		settlement.RefundID).Scan(&currentStatus, &refundAmountRaw, &orderID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRefundNotSettleable
		}
		return nil, err
	}
	if paymentdomain.RefundStatusIsFinal(currentStatus) {
		return nil, ErrRefundNotSettleable
	}
	refundAmount := decimal.RequireFromString(refundAmountRaw)

	reversalStatus := paymentdomain.ReversalNone
	reversalMessage := ""

	switch settlement.Status {
	case paymentdomain.RefundSuccess:
		reversalStatus, reversalMessage = applyFulfillmentReversalTx(ctx, tx, order, reverse, refundAmount, reverseEnabled)

	case paymentdomain.RefundFailed, paymentdomain.RefundClosed:
		// 释放预占额度：失败的退款不应继续占着订单的可退余额
		if err := releaseRefundReservationTx(ctx, tx, orderID, refundAmount); err != nil {
			return nil, err
		}
	}

	refundedAt := "NULL"
	if settlement.Status == paymentdomain.RefundSuccess {
		refundedAt = "NOW()"
	}
	rawResponse, _ := json.Marshal(settlement.RawResponse)
	refund, err := scanPaymentRefund(tx.QueryRow(ctx,
		`UPDATE payment_refunds SET status = $2, provider_refund_no = COALESCE(NULLIF($3, ''), provider_refund_no),
error_message = $4, raw_response = $5, reversal_status = $6, reversal_message = $7,
refunded_at = `+refundedAt+`, updated_at = NOW()
WHERE id = $1
RETURNING `+paymentRefundColumns,
		settlement.RefundID, settlement.Status, strings.TrimSpace(settlement.ProviderRefundNo),
		nullableString(settlement.ErrorMessage), rawResponse,
		reversalStatus, nullableString(reversalMessage)))
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return refund, nil
}

// releaseRefundReservationTx 释放失败退款单预占的额度并重算订单退款状态
func releaseRefundReservationTx(ctx context.Context, tx pgx.Tx, orderID int64, amount decimal.Decimal) error {
	var amountRaw, refundedRaw string
	if err := tx.QueryRow(ctx,
		`SELECT amount, refunded_amount FROM payment_orders WHERE id = $1 FOR UPDATE`,
		orderID).Scan(&amountRaw, &refundedRaw); err != nil {
		return err
	}
	orderAmount := decimal.RequireFromString(amountRaw)
	released := decimal.RequireFromString(refundedRaw).Sub(amount)
	if released.IsNegative() {
		released = decimal.Zero
	}
	_, err := tx.Exec(ctx,
		`UPDATE payment_orders SET refunded_amount = $2, refund_status = $3, updated_at = NOW() WHERE id = $1`,
		orderID, released.StringFixed(2), orderRefundStatus(released, orderAmount))
	return err
}

// applyFulfillmentReversalTx 退款成功后冲正已发放的权益。
//
// 冲正失败**不回滚整个事务**：上游的钱已经退出去了，此时让退款单结算失败会造成
// 「钱退了但系统记为未退」的更严重错位。因此这里把失败如实记进 reversal_status/message
// 交由人工处理，并在控制台以醒目状态呈现。
func applyFulfillmentReversalTx(
	ctx context.Context,
	tx pgx.Tx,
	order *paymentdomain.Order,
	reverse *paymentdomain.FulfillmentInstruction,
	refundAmount decimal.Decimal,
	reverseEnabled bool,
) (status string, message string) {
	if order == nil || reverse == nil {
		return paymentdomain.ReversalNone, ""
	}
	if !reverseEnabled {
		return paymentdomain.ReversalSkipped, "操作方选择不回收已发放权益"
	}
	// 以库内履约状态为准：调用方传入的订单快照可能早于最近一次履约
	var fulfillmentStatus string
	if err := tx.QueryRow(ctx, `SELECT fulfillment_status FROM payment_orders WHERE id = $1`,
		order.ID).Scan(&fulfillmentStatus); err != nil {
		return paymentdomain.ReversalFailed, "读取履约状态失败：" + err.Error()
	}
	if fulfillmentStatus != paymentdomain.FulfillmentDone {
		return paymentdomain.ReversalNone, "订单未履约，无需冲正"
	}
	if order.UserID == nil || *order.UserID <= 0 {
		return paymentdomain.ReversalFailed, "订单缺少归属用户，无法冲正"
	}
	userID := *order.UserID
	fullRefund := refundAmount.GreaterThanOrEqual(order.Amount)

	switch reverse.Purpose {
	case paymentdomain.PurposeWalletRecharge:
		// 充值为 1:1 入账，故按退款额等额扣回；余额不足则如实记失败
		if _, err := applyWalletChangeTx(ctx, tx, walletdomain.Change{
			UserID:         userID,
			AppID:          order.AppID,
			Type:           walletdomain.TxnTypeRefund,
			Amount:         refundAmount.Neg(),
			RelatedOrderNo: order.OrderNo,
			IdempotencyKey: "refundrev:" + order.OrderNo + ":" + refundAmount.StringFixed(2),
			Title:          "充值退款冲正",
			Remark:         order.Subject,
			Metadata:       map[string]any{"reason": "payment_refund", "orderNo": order.OrderNo},
		}); err != nil {
			return paymentdomain.ReversalFailed, "扣回充值余额失败：" + err.Error()
		}
		return paymentdomain.ReversalDone, ""

	case paymentdomain.PurposeIntegralPurchase:
		// 部分退款不按比例扣积分：积分可能已被消费，比例回收既难解释也易失败
		if !fullRefund {
			return paymentdomain.ReversalSkipped, "部分退款不自动回收积分，请按需人工调整"
		}
		if _, _, _, err := applyIntegralChangeTx(ctx, tx, userID, order.AppID, -reverse.IntegralAmount,
			"consume", "refund", "订单退款回收积分", order.Subject, "payment_order", &order.ID,
			map[string]any{"orderNo": order.OrderNo, "reason": "payment_refund"}); err != nil {
			return paymentdomain.ReversalFailed, "回收积分失败：" + err.Error()
		}
		return paymentdomain.ReversalDone, ""

	case paymentdomain.PurposeVipPurchase:
		if !fullRefund {
			return paymentdomain.ReversalSkipped, "部分退款不自动回收会员时长，请按需人工调整"
		}
		if reverse.VipDays <= 0 {
			return paymentdomain.ReversalFailed, "订单缺少会员时长快照，无法冲正"
		}
		// 精确逆运算：开通时加了多少天，这里就减多少天
		var expireAfter *time.Time
		if err := tx.QueryRow(ctx,
			`UPDATE users SET vip_expire_at = vip_expire_at - make_interval(days => $3), updated_at = NOW()
WHERE id = $1 AND appid = $2 AND vip_expire_at IS NOT NULL
RETURNING vip_expire_at`,
			userID, order.AppID, reverse.VipDays).Scan(&expireAfter); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return paymentdomain.ReversalFailed, "用户当前无会员有效期，无法回收时长"
			}
			return paymentdomain.ReversalFailed, "回收会员时长失败：" + err.Error()
		}
		// 赠送积分一并回收；不足则只记警告，不阻断（时长已回收成功）
		if reverse.VipBonus > 0 {
			if _, _, _, err := applyIntegralChangeTx(ctx, tx, userID, order.AppID, -reverse.VipBonus,
				"consume", "refund", "订单退款回收赠送积分", order.Subject, "payment_order", &order.ID,
				map[string]any{"orderNo": order.OrderNo, "reason": "payment_refund"}); err != nil {
				return paymentdomain.ReversalDone, "会员时长已回收，但赠送积分回收失败：" + err.Error()
			}
		}
		return paymentdomain.ReversalDone, ""

	default:
		return paymentdomain.ReversalNone, ""
	}
}

// RefundPaymentOrderToWallet 余额支付订单的退款：原路退回钱包并按需冲正履约（单事务）。
//
// 与第三方渠道退款的区别在于「打款」这一步也在本地事务内完成，
// 因此退款单结算、钱包入账、履约冲正三者要么全成功要么全回滚，不存在中间态。
func (r *Repository) RefundPaymentOrderToWallet(
	ctx context.Context,
	refundID int64,
	order *paymentdomain.Order,
	amount decimal.Decimal,
	refundNo string,
	reverse *paymentdomain.FulfillmentInstruction,
	reverseEnabled bool,
) (*paymentdomain.Refund, *walletdomain.Transaction, error) {
	if order == nil || order.UserID == nil || *order.UserID <= 0 {
		return nil, nil, fmt.Errorf("refund order to wallet: order user missing")
	}
	userID := *order.UserID

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	var currentStatus string
	if err := tx.QueryRow(ctx, `SELECT status FROM payment_refunds WHERE id = $1 FOR UPDATE`,
		refundID).Scan(&currentStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrRefundNotSettleable
		}
		return nil, nil, err
	}
	if paymentdomain.RefundStatusIsFinal(currentStatus) {
		return nil, nil, ErrRefundNotSettleable
	}

	// 锁序与支付路径一致：orders(已在创建退款单时锁过) → users → wallets
	var lockedUserID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`,
		userID, order.AppID).Scan(&lockedUserID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, ErrUserNotFound
		}
		return nil, nil, err
	}

	walletResult, err := applyWalletChangeTx(ctx, tx, walletdomain.Change{
		UserID:         userID,
		AppID:          order.AppID,
		Type:           walletdomain.TxnTypeRefund,
		Amount:         amount,
		RelatedOrderNo: order.OrderNo,
		IdempotencyKey: "refund:" + refundNo,
		Title:          "订单退款",
		Remark:         order.Subject,
		Metadata:       map[string]any{"orderNo": order.OrderNo, "refundNo": refundNo},
	})
	if err != nil {
		return nil, nil, err
	}

	reversalStatus, reversalMessage := applyFulfillmentReversalTx(ctx, tx, order, reverse, amount, reverseEnabled)

	rawResponse, _ := json.Marshal(map[string]any{
		"channel":             "balance",
		"walletTransactionNo": walletResult.Transaction.TransactionNo,
		"balanceAfter":        walletResult.Transaction.BalanceAfter.String(),
	})
	refund, err := scanPaymentRefund(tx.QueryRow(ctx,
		`UPDATE payment_refunds SET status = $2, provider_refund_no = $3, raw_response = $4,
reversal_status = $5, reversal_message = $6, refunded_at = NOW(), updated_at = NOW()
WHERE id = $1
RETURNING `+paymentRefundColumns,
		refundID, paymentdomain.RefundSuccess, walletResult.Transaction.TransactionNo, rawResponse,
		reversalStatus, nullableString(reversalMessage)))
	if err != nil {
		return nil, nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}
	tx = nil
	return refund, &walletResult.Transaction, nil
}

// ── 查询 ──

func (r *Repository) GetPaymentRefundByNo(ctx context.Context, appID int64, refundNo string) (*paymentdomain.Refund, error) {
	return scanPaymentRefund(r.pool.QueryRow(ctx,
		`SELECT `+paymentRefundColumns+` FROM payment_refunds WHERE appid = $1 AND refund_no = $2 LIMIT 1`,
		appID, strings.TrimSpace(refundNo)))
}

func (r *Repository) ListPaymentRefundsByOrder(ctx context.Context, orderID int64) ([]paymentdomain.Refund, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+paymentRefundColumns+` FROM payment_refunds WHERE order_id = $1 ORDER BY created_at DESC, id DESC`,
		orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectPaymentRefunds(rows)
}

// ListPaymentRefundsByApp 管理端分页查询退款单
func (r *Repository) ListPaymentRefundsByApp(ctx context.Context, appID int64, query paymentdomain.RefundListQuery) ([]paymentdomain.Refund, int64, error) {
	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit < 1 {
		limit = 20
	}

	args := []any{appID}
	where := ` WHERE appid = $1`
	if status := strings.TrimSpace(query.Status); status != "" {
		args = append(args, status)
		where += fmt.Sprintf(" AND status = $%d", len(args))
	}
	if method := strings.TrimSpace(query.Method); method != "" {
		args = append(args, method)
		where += fmt.Sprintf(" AND payment_method = $%d", len(args))
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		where += fmt.Sprintf(" AND (refund_no ILIKE $%d OR order_no ILIKE $%d OR provider_refund_no ILIKE $%d)",
			len(args), len(args), len(args))
	}

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM payment_refunds`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx,
		`SELECT `+paymentRefundColumns+` FROM payment_refunds`+where+
			fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)),
		args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items, err := collectPaymentRefunds(rows)
	return items, total, err
}

// ListUnsettledPaymentRefunds 拉取未达终态的退款单（补偿轮询用），按创建时间升序
func (r *Repository) ListUnsettledPaymentRefunds(ctx context.Context, limit int) ([]paymentdomain.Refund, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx,
		`SELECT `+paymentRefundColumns+` FROM payment_refunds
WHERE status IN ($1, $2) ORDER BY created_at ASC LIMIT $3`,
		paymentdomain.RefundPending, paymentdomain.RefundProcessing, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectPaymentRefunds(rows)
}

// ── 扫描辅助 ──

func collectPaymentRefunds(rows pgx.Rows) ([]paymentdomain.Refund, error) {
	items := make([]paymentdomain.Refund, 0)
	for rows.Next() {
		item, err := scanPaymentRefund(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func scanPaymentRefund(row interface{ Scan(dest ...any) error }) (*paymentdomain.Refund, error) {
	var item paymentdomain.Refund
	var amount string
	var rawResponse []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.OrderID, &item.OrderNo, &item.RefundNo,
		&item.ProviderRefundNo, &item.UserID, &item.PaymentMethod, &amount, &item.Reason,
		&item.Status, &item.ReversalStatus, &item.ReversalMessage, &item.Operator, &item.ClientIP,
		&item.ErrorMessage, &rawResponse, &item.RefundedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	item.Amount = decimal.RequireFromString(amount)
	_ = json.Unmarshal(rawResponse, &item.RawResponse)
	return &item, nil
}

// orderRefundStatus 按已占用退款额推导订单退款汇总状态
func orderRefundStatus(refunded decimal.Decimal, orderAmount decimal.Decimal) string {
	switch {
	case !refunded.IsPositive():
		return paymentdomain.OrderRefundNone
	case refunded.GreaterThanOrEqual(orderAmount):
		return paymentdomain.OrderRefundFull
	default:
		return paymentdomain.OrderRefundPartial
	}
}
