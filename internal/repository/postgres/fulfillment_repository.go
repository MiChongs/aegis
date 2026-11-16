package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	paymentdomain "aegis/internal/domain/payment"
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
		// 锁序 users → wallets：附赠积分要锁 users，先锁住再动钱包，与全库其它事务同序
		if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`, userID, order.AppID); err != nil {
			return false, err
		}
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
			Metadata:       rechargeMetadata(order, instr),
		}); err != nil {
			return false, err
		}
		if err := grantRechargeBonusTx(ctx, tx, order, instr, userID); err != nil {
			return false, err
		}
	case paymentdomain.PurposeVipPurchase:
		if _, err := extendUserVipTx(ctx, tx, vipdomain.Grant{
			UserID:         userID,
			AppID:          order.AppID,
			PlanID:         instr.VipPlanID,
			PlanName:       instr.VipPlanName,
			Features:       instr.VipFeatures,
			DurationDays:   instr.VipDays,
			Lifetime:       instr.VipLifetime,
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
				Features:       instr.VipFeatures,
				DurationDays:   instr.VipDays,
				Lifetime:       instr.VipLifetime,
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

// rechargeMetadata 充值流水上留档的订单信息（档位名称让流水列表能显示「充 100 送 20」是哪一档）。
func rechargeMetadata(order *paymentdomain.Order, instr paymentdomain.FulfillmentInstruction) map[string]any {
	meta := map[string]any{"paymentMethod": order.PaymentMethod}
	if instr.WalletPackageID > 0 {
		meta["packageId"] = instr.WalletPackageID
		meta["packageTitle"] = instr.WalletPackageTitle
	}
	return meta
}

// grantRechargeBonusTx 发放充值档位的附赠：赠送余额（进赠送余额桶）与赠送积分。
// 幂等键挂在订单号上，与本金入账同事务，要么一起到账要么都不到。
func grantRechargeBonusTx(ctx context.Context, tx pgx.Tx, order *paymentdomain.Order, instr paymentdomain.FulfillmentInstruction, userID int64) error {
	meta := map[string]any{"orderNo": order.OrderNo, "packageId": instr.WalletPackageID, "packageTitle": instr.WalletPackageTitle}
	if instr.WalletBonus.IsPositive() {
		if _, err := applyWalletChangeTx(ctx, tx, walletdomain.Change{
			UserID:         userID,
			AppID:          order.AppID,
			Type:           walletdomain.TxnTypeRechargeBonus,
			Amount:         instr.WalletBonus,
			Bucket:         walletdomain.BucketBonus,
			RelatedOrderNo: order.OrderNo,
			IdempotencyKey: "orderbonus:" + order.OrderNo,
			Title:          "充值赠送",
			Remark:         instr.WalletPackageTitle,
			ClientIP:       order.ClientIP,
			Metadata:       meta,
		}); err != nil {
			return err
		}
	}
	if instr.WalletBonusIntegral > 0 {
		if _, _, _, err := applyIntegralChangeTx(ctx, tx, userID, order.AppID, instr.WalletBonusIntegral,
			"earn", "wallet_recharge_bonus", "充值赠送积分", instr.WalletPackageTitle, "wallet_recharge", &order.ID, meta); err != nil {
			return err
		}
	}
	return nil
}
