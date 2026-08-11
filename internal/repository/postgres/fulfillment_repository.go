package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	paymentdomain "aegis/internal/domain/payment"
	pointdomain "aegis/internal/domain/points"
	vipdomain "aegis/internal/domain/vip"
	walletdomain "aegis/internal/domain/wallet"
	"github.com/jackc/pgx/v5"
)

// FulfillPaymentOrder 按履约指令为已支付订单发放权益（单事务，恰好一次）。
//
// 幂等核心：第一步用条件 UPDATE 抢占履约权（status='paid' 且 fulfillment_status='none'），
// 抢不到说明已履约或不满足条件，直接返回 (false, nil)；
// 抢到后在同一事务内完成发放，任何一步失败整体回滚（履约权也随之释放），
// 下一次回调 / 查单会自动重试。返回 (true, nil) 表示本次完成了实际发放。
func (r *Repository) FulfillPaymentOrder(ctx context.Context, order *paymentdomain.Order, instr paymentdomain.FulfillmentInstruction) (bool, error) {
	if order == nil || order.UserID == nil || *order.UserID <= 0 {
		return false, fmt.Errorf("fulfill payment order: order user missing")
	}
	userID := *order.UserID

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	// 抢占履约权（行级锁 + 条件更新，重复回调在此被拦下）
	var claimedID int64
	err = tx.QueryRow(ctx,
		`UPDATE payment_orders SET fulfillment_status = $2, fulfilled_at = NOW(), updated_at = NOW()
WHERE id = $1 AND status = 'paid' AND fulfillment_status = $3
RETURNING id`,
		order.ID, paymentdomain.FulfillmentDone, paymentdomain.FulfillmentNone).Scan(&claimedID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, nil
		}
		return false, err
	}

	switch instr.Purpose {
	case paymentdomain.PurposeWalletRecharge:
		if _, err := applyWalletChangeTx(ctx, tx, walletdomain.Change{
			UserID:         userID,
			AppID:          order.AppID,
			Type:           walletdomain.TxnTypeRecharge,
			Amount:         instr.WalletAmount,
			RelatedOrderNo: order.OrderNo,
			IdempotencyKey: "order:" + order.OrderNo,
			Title:          "余额充值",
			Remark:         order.Subject,
			ClientIP:       order.ClientIP,
			Metadata:       map[string]any{"paymentMethod": order.PaymentMethod},
		}); err != nil {
			return false, err
		}
	case paymentdomain.PurposeVipPurchase:
		if _, err := extendUserVipTx(ctx, tx, vipdomain.Grant{
			UserID:         userID,
			AppID:          order.AppID,
			PlanID:         instr.VipPlanID,
			PlanName:       instr.VipPlanName,
			DurationDays:   instr.VipDays,
			PayChannel:     vipdomain.ChannelPaymentOrder,
			PayAmount:      order.Amount,
			RelatedOrderNo: order.OrderNo,
			BonusIntegral:  instr.VipBonus,
			Metadata:       map[string]any{"paymentMethod": order.PaymentMethod},
		}); err != nil {
			return false, err
		}
	case paymentdomain.PurposeIntegralPurchase:
		if _, _, _, err := applyIntegralChangeTx(ctx, tx, userID, order.AppID, instr.IntegralAmount,
			"earn", "purchase", "积分充值", order.Subject, "payment_order", &order.ID,
			map[string]any{"orderNo": order.OrderNo, "paymentMethod": order.PaymentMethod}); err != nil {
			return false, err
		}
	default:
		return false, fmt.Errorf("fulfill payment order: unknown purpose %q", instr.Purpose)
	}

	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	tx = nil
	return true, nil
}

// ErrOrderNotPayable 订单不可支付（非 pending、已过期或已被并发支付）
var ErrOrderNotPayable = errors.New("postgres: payment order not payable")

// PayPaymentOrderWithWallet 余额支付订单（单事务原子完成）：
// 锁定订单（必须 pending 且未过期）→ 锁定用户行 → 钱包扣款（幂等键 orderpay:订单号）→
// 订单置为已支付（provider_order_no 记钱包流水号）→ 如携带履约指令则同事务发放。
// 锁序固定为 orders → users → wallets，与回调履约/钱包购买共用同一全局顺序，无死锁环。
func (r *Repository) PayPaymentOrderWithWallet(ctx context.Context, order *paymentdomain.Order, instr *paymentdomain.FulfillmentInstruction) (*walletdomain.Transaction, error) {
	if order == nil || order.UserID == nil || *order.UserID <= 0 {
		return nil, fmt.Errorf("pay order with wallet: order user missing")
	}
	if instr != nil && instr.Purpose == paymentdomain.PurposeWalletRecharge {
		return nil, fmt.Errorf("pay order with wallet: recharge order cannot be paid by balance")
	}
	userID := *order.UserID

	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	// 锁定订单行并校验可支付状态（并发重复支付在此被拦下）
	var status string
	var expireAt *time.Time
	if err := tx.QueryRow(ctx, `SELECT status, expire_at FROM payment_orders WHERE id = $1 FOR UPDATE`,
		order.ID).Scan(&status, &expireAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrOrderNotPayable
		}
		return nil, err
	}
	if status != "pending" || (expireAt != nil && expireAt.Before(time.Now())) {
		return nil, ErrOrderNotPayable
	}

	// 锁定用户行（统一锁序：users 先于 wallets）
	var lockedUserID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`,
		userID, order.AppID).Scan(&lockedUserID); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	// 钱包扣款（幂等：网络重试不重复扣）
	walletResult, err := applyWalletChangeTx(ctx, tx, walletdomain.Change{
		UserID:         userID,
		AppID:          order.AppID,
		Type:           walletdomain.TxnTypeOrderPay,
		Amount:         order.Amount.Neg(),
		RelatedOrderNo: order.OrderNo,
		IdempotencyKey: "orderpay:" + order.OrderNo,
		Title:          "余额支付订单",
		Remark:         order.Subject,
		ClientIP:       order.ClientIP,
		Metadata:       map[string]any{"orderNo": order.OrderNo},
	})
	if err != nil {
		return nil, err
	}

	// 订单置为已支付；携带履约指令时同语句完成履约抢占
	fulfillNow := instr != nil
	if _, err := tx.Exec(ctx,
		`UPDATE payment_orders SET status = 'paid', notify_status = 'balance_paid', provider_order_no = $2,
paid_at = NOW(), raw_callback = $3,
fulfillment_status = CASE WHEN $4 THEN $5 ELSE fulfillment_status END,
fulfilled_at = CASE WHEN $4 THEN NOW() ELSE fulfilled_at END,
updated_at = NOW() WHERE id = $1`,
		order.ID, walletResult.Transaction.TransactionNo,
		[]byte(`{"channel":"balance"}`), fulfillNow, paymentdomain.FulfillmentDone); err != nil {
		return nil, err
	}

	if fulfillNow {
		switch instr.Purpose {
		case paymentdomain.PurposeVipPurchase:
			if _, err := extendUserVipTx(ctx, tx, vipdomain.Grant{
				UserID:         userID,
				AppID:          order.AppID,
				PlanID:         instr.VipPlanID,
				PlanName:       instr.VipPlanName,
				DurationDays:   instr.VipDays,
				PayChannel:     vipdomain.ChannelPaymentOrder,
				PayAmount:      order.Amount,
				RelatedOrderNo: order.OrderNo,
				BonusIntegral:  instr.VipBonus,
				Metadata:       map[string]any{"paymentMethod": paymentdomain.MethodBalance},
			}); err != nil {
				return nil, err
			}
		case paymentdomain.PurposeIntegralPurchase:
			if _, _, _, err := applyIntegralChangeTx(ctx, tx, userID, order.AppID, instr.IntegralAmount,
				"earn", "purchase", "积分充值", order.Subject, "payment_order", &order.ID,
				map[string]any{"orderNo": order.OrderNo, "paymentMethod": paymentdomain.MethodBalance}); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("pay order with wallet: unknown purpose %q", instr.Purpose)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return &walletResult.Transaction, nil
}

// ExpirePaymentOrders 将超过有效期仍未支付的订单批量置为 expired，返回处理数量。
func (r *Repository) ExpirePaymentOrders(ctx context.Context) (int64, error) {
	result, err := r.pool.Exec(ctx,
		`UPDATE payment_orders SET status = 'expired', updated_at = NOW()
WHERE status = 'pending' AND expire_at IS NOT NULL AND expire_at < NOW()`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

// BatchAdjustUserIntegralByAdmin 管理员批量调整积分（集合化单事务）。
// 原实现按用户逐个开事务（1000 用户 = 1000 事务 + 2000 余次往返）；
// 此实现一个事务、一条复合语句完成锁定/更新/记账，并逐用户返回成败明细：
//   - 不存在或不属于该应用的用户 → "用户不存在"
//   - 扣减后为负的用户 → "用户积分不足"（其余用户正常生效）
func (r *Repository) BatchAdjustUserIntegralByAdmin(ctx context.Context, userIDs []int64, appID int64, amount int64, reason string, options pointdomain.AdminAdjustOptions) ([]pointdomain.IntegralAdjustResult, []pointdomain.BatchAdjustFailure, error) {
	if len(userIDs) == 0 {
		return nil, nil, nil
	}
	title := "管理员批量调整"
	txnType := "earn"
	if amount < 0 {
		txnType = "consume"
		title = "管理员批量扣除"
	}
	if reason == "" {
		reason = title
	}
	extraJSON, _ := json.Marshal(map[string]any{
		"adminId":      options.AdminID,
		"adminAccount": options.AdminAccount,
		"reason":       reason,
		"batch":        true,
	})
	// 批次号前缀 + '-' + 用户ID 构成行级唯一流水号（总长受 VARCHAR(40) 约束）
	batchNo := "INTB" + time.Now().UTC().Format("060102150405") + randomDigits(4)

	rows, err := r.pool.Query(ctx, `WITH target AS (
    SELECT id, account, integral FROM users
    WHERE appid = $1 AND id = ANY($2)
    ORDER BY id
    FOR UPDATE
), updated AS (
    UPDATE users u
    SET integral = u.integral + $3, updated_at = NOW()
    FROM target t
    WHERE u.id = t.id AND t.integral + $3 >= 0
    RETURNING u.id, t.integral AS balance_before, u.integral AS balance_after
), logged AS (
    INSERT INTO integral_transactions (transaction_no, user_id, appid, type, category, amount,
        balance_before, balance_after, status, title, description, source_id, source_type, multiplier,
        client_ip, user_agent, extra_data, created_at, updated_at)
    SELECT $4 || '-' || u.id::text, u.id, $1, $5, 'admin_adjust', $3, u.balance_before, u.balance_after,
        'completed', $6, $7, $8, 'admin_manual', 1, $9, $10, $11::jsonb, NOW(), NOW()
    FROM updated u
    RETURNING user_id
)
SELECT t.id, t.account, t.integral AS balance_before,
       COALESCE(u.balance_after, t.integral) AS balance_after,
       (u.id IS NOT NULL) AS applied
FROM target t
LEFT JOIN updated u ON u.id = t.id
ORDER BY t.id`,
		appID, userIDs, amount, batchNo, txnType, title, nullableString(reason),
		nullableInt64(options.AdminID), nullableString(options.ClientIP), nullableString(options.UserAgent), extraJSON)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	now := time.Now().UTC()
	operationType := "add"
	if amount < 0 {
		operationType = "consume"
	}
	found := make(map[int64]bool, len(userIDs))
	results := make([]pointdomain.IntegralAdjustResult, 0, len(userIDs))
	failures := make([]pointdomain.BatchAdjustFailure, 0)
	for rows.Next() {
		var id, before, after int64
		var account string
		var applied bool
		if err := rows.Scan(&id, &account, &before, &after, &applied); err != nil {
			return nil, nil, err
		}
		found[id] = true
		if !applied {
			failures = append(failures, pointdomain.BatchAdjustFailure{UserID: id, Error: "用户积分不足"})
			continue
		}
		results = append(results, pointdomain.IntegralAdjustResult{
			UserID:        id,
			AppID:         appID,
			Account:       account,
			Amount:        amount,
			BeforeAmount:  before,
			AfterAmount:   after,
			Reason:        reason,
			OperationType: operationType,
			TransactionNo: fmt.Sprintf("%s-%d", batchNo, id),
			CreatedAt:     now,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	for _, id := range userIDs {
		if !found[id] {
			failures = append(failures, pointdomain.BatchAdjustFailure{UserID: id, Error: "用户不存在"})
		}
	}
	return results, failures, nil
}
