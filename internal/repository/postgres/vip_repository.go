package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	vipdomain "aegis/internal/domain/vip"
	walletdomain "aegis/internal/domain/wallet"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

const vipPlanColumns = `id, appid, name, kind, trial_device_limited, features, duration_days, price, original_price, bonus_integral,
COALESCE(description, ''), is_active, sort_order, created_at, updated_at`

const vipTxnColumns = `id, transaction_no, user_id, appid, plan_id, plan_name, features, duration_days, pay_channel,
pay_amount, COALESCE(related_order_no, ''), bonus_integral, expire_before, expire_after,
COALESCE(operator, ''), COALESCE(metadata, '{}'::jsonb), revoked_at, COALESCE(revoke_reason, ''), created_at`

// ── 套餐管理 ──

func (r *Repository) ListVipPlans(ctx context.Context, appID int64, activeOnly bool) ([]vipdomain.Plan, error) {
	query := `SELECT ` + vipPlanColumns + ` FROM vip_plans WHERE appid = $1`
	if activeOnly {
		query += ` AND is_active = TRUE`
	}
	query += ` ORDER BY sort_order ASC, id ASC`
	rows, err := r.pool.Query(ctx, query, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]vipdomain.Plan, 0, 8)
	for rows.Next() {
		item, err := scanVipPlan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// ListPurchasableVipPlans 用户可见的**在售**套餐 —— 不含试用。
//
// 试用套餐是 0 元的，且发放要过资格判定。把它混在售卖列表里，客户端会渲染出
// 一张点了必然报 40376 的卡片，而"为什么这个 0 元套餐买不了"没有任何提示说得清。
// 试用由 /vip/status 的 trialOffer 描述（含套餐名、天数、能不能领、不能领的原因）。
func (r *Repository) ListPurchasableVipPlans(ctx context.Context, appID int64) ([]vipdomain.Plan, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+vipPlanColumns+
		` FROM vip_plans WHERE appid = $1 AND is_active = TRUE AND kind <> 'trial'
ORDER BY sort_order ASC, id ASC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]vipdomain.Plan, 0, 8)
	for rows.Next() {
		item, err := scanVipPlan(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetVipPlan(ctx context.Context, appID int64, planID int64) (*vipdomain.Plan, error) {
	return scanVipPlan(r.pool.QueryRow(ctx,
		`SELECT `+vipPlanColumns+` FROM vip_plans WHERE appid = $1 AND id = $2 LIMIT 1`, appID, planID))
}

func (r *Repository) UpsertVipPlan(ctx context.Context, mutation vipdomain.PlanMutation) (*vipdomain.Plan, error) {
	current := &vipdomain.Plan{AppID: mutation.AppID, Kind: vipdomain.KindPaid, IsActive: true, Price: decimal.Zero}
	if mutation.ID > 0 {
		existing, err := r.GetVipPlan(ctx, mutation.AppID, mutation.ID)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			// 目标套餐不存在：返回 nil 交由服务层翻译为业务错误
			return nil, nil
		}
		current = existing
	}
	if mutation.Name != nil {
		current.Name = strings.TrimSpace(*mutation.Name)
	}
	if mutation.Kind != nil {
		current.Kind = strings.TrimSpace(*mutation.Kind)
	}
	if mutation.TrialDeviceLimited != nil {
		current.TrialDeviceLimited = *mutation.TrialDeviceLimited
	}
	if mutation.Features != nil {
		current.Features = vipdomain.NormalizeFeatureTags(*mutation.Features)
	}
	if mutation.DurationDays != nil {
		current.DurationDays = *mutation.DurationDays
	}
	if mutation.Price != nil {
		current.Price = *mutation.Price
	}
	if mutation.OriginalPrice != nil {
		current.OriginalPrice = mutation.OriginalPrice
	}
	if mutation.BonusIntegral != nil {
		current.BonusIntegral = *mutation.BonusIntegral
	}
	if mutation.Description != nil {
		current.Description = strings.TrimSpace(*mutation.Description)
	}
	if mutation.IsActive != nil {
		current.IsActive = *mutation.IsActive
	}
	if mutation.SortOrder != nil {
		current.SortOrder = *mutation.SortOrder
	}
	var originalPrice any
	if current.OriginalPrice != nil {
		originalPrice = current.OriginalPrice.StringFixed(2)
	}
	return scanVipPlan(r.pool.QueryRow(ctx,
		`INSERT INTO vip_plans (id, appid, name, kind, trial_device_limited, features, duration_days, price, original_price, bonus_integral, description, is_active, sort_order, created_at, updated_at)
VALUES (COALESCE(NULLIF($1, 0), nextval(pg_get_serial_sequence('vip_plans', 'id'))), $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NOW(), NOW())
ON CONFLICT (id) DO UPDATE SET
	name = EXCLUDED.name,
	kind = EXCLUDED.kind,
	trial_device_limited = EXCLUDED.trial_device_limited,
	features = EXCLUDED.features,
	duration_days = EXCLUDED.duration_days,
	price = EXCLUDED.price,
	original_price = EXCLUDED.original_price,
	bonus_integral = EXCLUDED.bonus_integral,
	description = EXCLUDED.description,
	is_active = EXCLUDED.is_active,
	sort_order = EXCLUDED.sort_order,
	updated_at = NOW()
RETURNING `+vipPlanColumns,
		mutation.ID, current.AppID, current.Name, current.Kind, current.TrialDeviceLimited,
		vipdomain.NormalizeFeatureTags(current.Features), current.DurationDays,
		current.Price.StringFixed(2), originalPrice, current.BonusIntegral,
		nullableString(current.Description), current.IsActive, current.SortOrder))
}

// DeleteVipPlan 删除套餐，并把仍在期内的开通记录的功能定格成套餐最后的配置。
//
// 权益跟随套餐的当前配置（见 vipdomain.Segment），套餐一删就没有"当前配置"了，
// 读取端只能回落到开通时的快照。不在这里定格的话会出现一个反直觉的结果：
// 运营先把 export 从套餐里拿掉（老用户随之失去它），再删掉套餐 ——
// 老用户的 export 反而回来了，因为开通时的快照里有它。
// 删除是整理商品目录，不是改权益；要改权益请改套餐配置，要停售请下架。
//
// 已经结束的记录不动：它们的快照是「当时卖出去的是什么」的留档。
func (r *Repository) DeleteVipPlan(ctx context.Context, appID int64, planID int64) (bool, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if _, err := tx.Exec(ctx, `UPDATE vip_transactions AS vt SET features = p.features
FROM vip_plans p
WHERE p.appid = $1 AND p.id = $2 AND vt.appid = p.appid AND vt.plan_id = p.id
  AND `+vipSegmentScopeSQL+` AND `+vipSegmentUntilSQL+` > NOW()`, appID, planID); err != nil {
		return false, err
	}
	result, err := tx.Exec(ctx, `DELETE FROM vip_plans WHERE appid = $1 AND id = $2`, appID, planID)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	tx = nil
	return result.RowsAffected() > 0, nil
}

// ── VIP 状态 / 记录 ──

// 会员状态查询走 `GetVipEntitlementFacts` + `vipdomain.Evaluate`（vip_trial_repository.go）。
// 这里刻意不再留一个"只读 vip_expire_at 判个是/否"的简版：它回答不了
// 「是不是试用」，而两个入口给出不同深浅的结论，迟早会有人拿浅的那个去做判断。

// buildVipStatus 购买结果里的会员状态快照（不是判定入口）。
func buildVipStatus(expireAt *time.Time) *vipdomain.Status {
	status := &vipdomain.Status{ExpireAt: expireAt}
	if expireAt != nil && expireAt.After(time.Now()) {
		status.IsVIP = true
		status.RemainingDays = int(time.Until(*expireAt).Hours() / 24)
	}
	return status
}

func (r *Repository) ListVipTransactions(ctx context.Context, userID int64, appID int64, page int, limit int) ([]vipdomain.Transaction, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	args := []any{appID}
	where := ` WHERE appid = $1`
	if userID > 0 {
		args = append(args, userID)
		where += fmt.Sprintf(` AND user_id = $%d`, len(args))
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM vip_transactions`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, `SELECT `+vipTxnColumns+` FROM vip_transactions`+where+
		fmt.Sprintf(` ORDER BY id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]vipdomain.Transaction, 0, limit)
	for rows.Next() {
		item, err := scanVipTxn(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

// ── 开通 / 续期 ──

// GrantVip 管理员授予 / 系统发放（独立事务）
func (r *Repository) GrantVip(ctx context.Context, grant vipdomain.Grant) (*vipdomain.Transaction, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	txn, err := extendUserVipTx(ctx, tx, grant)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return txn, nil
}

// extendUserVipTx VIP 续期核心（必须在事务内调用）：
// 锁定用户行 → 未过期则顺延、已过期/未开通则从当前时刻起算 → 更新到期时间 →
// 写入 VIP 记录 → 套餐含赠送积分时在同一事务内发放（同一把用户行锁，天然无死锁）。
func extendUserVipTx(ctx context.Context, tx pgx.Tx, grant vipdomain.Grant) (*vipdomain.Transaction, error) {
	if grant.DurationDays <= 0 {
		return nil, fmt.Errorf("vip duration days must be positive")
	}
	var expireBefore *time.Time
	if err := tx.QueryRow(ctx, `SELECT vip_expire_at FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`,
		grant.UserID, grant.AppID).Scan(&expireBefore); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	now := time.Now().UTC()
	base := now
	if expireBefore != nil && expireBefore.After(now) {
		base = expireBefore.UTC()
	}
	expireAfter := base.AddDate(0, 0, grant.DurationDays)
	if _, err := tx.Exec(ctx, `UPDATE users SET vip_expire_at = $1, updated_at = NOW() WHERE id = $2 AND appid = $3`,
		expireAfter, grant.UserID, grant.AppID); err != nil {
		return nil, err
	}

	txn := vipdomain.Transaction{
		TransactionNo: generateTransactionNo("VIP"),
		UserID:        grant.UserID,
		AppID:         grant.AppID,
		PlanID:        grant.PlanID,
		PlanName:      truncateColumn(strings.TrimSpace(grant.PlanName), vipPlanNameMaxRunes),
		// 功能留档：套餐还在时权益跟随套餐当前配置，这份只在套餐被删 / 非套餐发放时生效
		Features:       vipdomain.NormalizeFeatureTags(grant.Features),
		DurationDays:   grant.DurationDays,
		PayChannel:     grant.PayChannel,
		PayAmount:      grant.PayAmount,
		RelatedOrderNo: strings.TrimSpace(grant.RelatedOrderNo),
		BonusIntegral:  grant.BonusIntegral,
		ExpireBefore:   expireBefore,
		ExpireAfter:    expireAfter,
		Operator:       grant.Operator,
		Metadata:       grant.Metadata,
	}
	metaJSON, _ := json.Marshal(grant.Metadata)
	// active_from / active_until 这段会员期在链上的位置：顺延时接在旧到期时间之后，否则从此刻起
	if err := tx.QueryRow(ctx,
		`INSERT INTO vip_transactions (transaction_no, user_id, appid, plan_id, plan_name, features, duration_days, pay_channel,
pay_amount, related_order_no, bonus_integral, expire_before, expire_after, operator, metadata,
active_from, active_until, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $13, NOW())
RETURNING id, created_at`,
		txn.TransactionNo, txn.UserID, txn.AppID, txn.PlanID, txn.PlanName, txn.Features, txn.DurationDays,
		txn.PayChannel, txn.PayAmount.StringFixed(2), nullableString(txn.RelatedOrderNo), txn.BonusIntegral,
		txn.ExpireBefore, txn.ExpireAfter, nullableString(txn.Operator), metaJSON, base).
		Scan(&txn.ID, &txn.CreatedAt); err != nil {
		return nil, err
	}

	// 赠送积分与 VIP 续期同事务：要么全部生效，要么全部回滚
	if grant.BonusIntegral > 0 {
		if _, _, _, err := applyIntegralChangeTx(ctx, tx, grant.UserID, grant.AppID, grant.BonusIntegral,
			"earn", "vip_bonus", "VIP 套餐赠送积分", grant.PlanName, "vip_transaction", &txn.ID,
			map[string]any{"vipTransactionNo": txn.TransactionNo, "planName": grant.PlanName}); err != nil {
			return nil, err
		}
	}
	return &txn, nil
}

// ErrVipNotActive 扣减天数时该用户当前不是会员（没有可扣的时长）
var ErrVipNotActive = errors.New("postgres: vip not active")

// vipPlanNameMaxRunes vip_transactions.plan_name 的列宽（VARCHAR(64)）。
// 自定义发放 / 扣减把「原因」写进这一列，原因是自由文本，不截断会让发放直接报 22001。
const vipPlanNameMaxRunes = 64

// RevokeVipDays 扣减会员天数（单事务）：锁用户 → 从到期时间往回截 → 截断会员段 → 记账。
//
// 截过当前时刻即视为会员立即结束（到期时间落在此刻），不会被推到过去 ——
// 推到过去并不会让下一次开通少给（开通从此刻起算），只是账上多了一段说不清的负数。
//
// 账本记一条负时长的 admin_revoke 记录：它不是一段会员期（不贡献功能、不成为当前套餐），
// 只是让「到期时间为什么变早了」在账上有据可查。
func (r *Repository) RevokeVipDays(ctx context.Context, revoke vipdomain.Revoke) (*vipdomain.Transaction, error) {
	if revoke.Days <= 0 {
		return nil, fmt.Errorf("vip revoke days must be positive")
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

	var expireBefore *time.Time
	if err := tx.QueryRow(ctx, `SELECT vip_expire_at FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`,
		revoke.UserID, revoke.AppID).Scan(&expireBefore); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	now := time.Now().UTC()
	if expireBefore == nil || !expireBefore.After(now) {
		return nil, ErrVipNotActive
	}
	expireAfter := expireBefore.UTC().AddDate(0, 0, -revoke.Days)
	clamped := false
	if expireAfter.Before(now) {
		expireAfter = now
		clamped = true
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET vip_expire_at = $1, updated_at = NOW() WHERE id = $2 AND appid = $3`,
		expireAfter, revoke.UserID, revoke.AppID); err != nil {
		return nil, err
	}
	if err := truncateVipSegmentsTx(ctx, tx, revoke.AppID, revoke.UserID, expireAfter); err != nil {
		return nil, err
	}

	planName := truncateColumn(strings.TrimSpace(revoke.Reason), vipPlanNameMaxRunes)
	if planName == "" {
		planName = "扣减会员天数"
	}
	metadata := make(map[string]any, len(revoke.Metadata)+2)
	for key, value := range revoke.Metadata {
		metadata[key] = value
	}
	metadata["reason"] = strings.TrimSpace(revoke.Reason)
	if clamped {
		// 剩余时长不足扣：实际扣掉的比请求的少，到期时间落在此刻
		metadata["clamped"] = true
	}
	txn := vipdomain.Transaction{
		TransactionNo: generateTransactionNo("VIP"),
		UserID:        revoke.UserID,
		AppID:         revoke.AppID,
		PlanName:      planName,
		Features:      []string{},
		DurationDays:  -revoke.Days,
		PayChannel:    vipdomain.ChannelAdminRevoke,
		PayAmount:     decimal.Zero,
		ExpireBefore:  expireBefore,
		ExpireAfter:   expireAfter,
		Operator:      revoke.Operator,
		Metadata:      metadata,
	}
	metaJSON, _ := json.Marshal(metadata)
	if err := tx.QueryRow(ctx,
		`INSERT INTO vip_transactions (transaction_no, user_id, appid, plan_id, plan_name, features, duration_days, pay_channel,
pay_amount, related_order_no, bonus_integral, expire_before, expire_after, operator, metadata, created_at)
VALUES ($1, $2, $3, NULL, $4, $5, $6, $7, $8, NULL, 0, $9, $10, $11, $12, NOW())
RETURNING id, created_at`,
		txn.TransactionNo, txn.UserID, txn.AppID, txn.PlanName, txn.Features, txn.DurationDays,
		txn.PayChannel, txn.PayAmount.StringFixed(2), txn.ExpireBefore, txn.ExpireAfter,
		nullableString(txn.Operator), metaJSON).
		Scan(&txn.ID, &txn.CreatedAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return &txn, nil
}

// PurchaseVipWithWallet 余额购买 VIP（单事务原子完成）：
// 钱包扣款（幂等）→ VIP 续期 → 赠送积分。幂等键命中时直接返回首次购买结果，不重复扣款。
func (r *Repository) PurchaseVipWithWallet(ctx context.Context, userID int64, appID int64, plan vipdomain.Plan, idempotencyKey string, clientIP string) (*vipdomain.PurchaseResult, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	// 先锁定用户行：同一用户的购买请求全量串行化，使后续幂等检查无并发穿透窗口。
	// 全库锁序统一为 users → wallets（extendUserVipTx 对同行重复加锁为可重入），不构成死锁环
	var lockedUserID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`,
		userID, appID).Scan(&lockedUserID); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	// 前置幂等检查：无论套餐是否免费，同一幂等键的购买只生效一次
	// （钱包扣款幂等是付费套餐的第二道防线；免费套餐仅靠这里拦截）
	if key := strings.TrimSpace(idempotencyKey); key != "" {
		existing, err := scanVipTxn(tx.QueryRow(ctx, `SELECT `+vipTxnColumns+
			` FROM vip_transactions WHERE appid = $1 AND user_id = $2 AND metadata->>'idempotencyKey' = $3 ORDER BY id DESC LIMIT 1`,
			appID, userID, key))
		if err != nil {
			return nil, err
		}
		if existing != nil {
			var expireAt *time.Time
			if err := tx.QueryRow(ctx, `SELECT vip_expire_at FROM users WHERE id = $1 AND appid = $2`,
				userID, appID).Scan(&expireAt); err != nil {
				return nil, err
			}
			wallet, err := scanWallet(tx.QueryRow(ctx,
				`SELECT user_id, appid, balance, frozen, total_recharged, total_consumed, created_at, updated_at
FROM user_wallets WHERE user_id = $1 AND appid = $2 LIMIT 1`, userID, appID))
			if err != nil {
				return nil, err
			}
			walletTxnNo := findWalletTxnNoByIdemKey(ctx, tx, key)
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			tx = nil
			balance := decimal.Zero
			if wallet != nil {
				balance = wallet.Balance
			}
			return &vipdomain.PurchaseResult{
				Transaction:         *existing,
				Status:              *buildVipStatus(expireAt),
				WalletBalance:       balance,
				BonusIntegral:       existing.BonusIntegral,
				WalletTransactionNo: walletTxnNo,
				Replayed:            true,
			}, nil
		}
	}

	change := walletdomain.Change{
		UserID:         userID,
		AppID:          appID,
		Type:           walletdomain.TxnTypeVipPurchase,
		Amount:         plan.Price.Neg(),
		IdempotencyKey: strings.TrimSpace(idempotencyKey),
		Title:          "购买 VIP：" + plan.Name,
		ClientIP:       clientIP,
		Metadata:       map[string]any{"planId": plan.ID, "planName": plan.Name, "durationDays": plan.DurationDays},
	}
	// 0 元套餐不动钱包，直接开通
	var walletResult *walletdomain.ChangeResult
	if plan.Price.IsPositive() {
		walletResult, err = applyWalletChangeTx(ctx, tx, change)
		if err != nil {
			return nil, err
		}
		if walletResult.Replayed {
			// 幂等重放：查回首次购买对应的 VIP 记录与当前状态
			txn, err := scanVipTxn(tx.QueryRow(ctx, `SELECT `+vipTxnColumns+
				` FROM vip_transactions WHERE appid = $1 AND user_id = $2 AND metadata->>'idempotencyKey' = $3 ORDER BY id DESC LIMIT 1`,
				appID, userID, change.IdempotencyKey))
			if err != nil {
				return nil, err
			}
			var expireAt *time.Time
			if err := tx.QueryRow(ctx, `SELECT vip_expire_at FROM users WHERE id = $1 AND appid = $2`,
				userID, appID).Scan(&expireAt); err != nil {
				return nil, err
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			tx = nil
			result := &vipdomain.PurchaseResult{
				Status:              *buildVipStatus(expireAt),
				WalletBalance:       walletResult.Wallet.Balance,
				WalletTransactionNo: walletResult.Transaction.TransactionNo,
				Replayed:            true,
			}
			if txn != nil {
				result.Transaction = *txn
				result.BonusIntegral = txn.BonusIntegral
			}
			return result, nil
		}
	}

	planID := plan.ID
	grant := vipdomain.Grant{
		UserID:        userID,
		AppID:         appID,
		PlanID:        &planID,
		PlanName:      plan.Name,
		Features:      plan.Features,
		DurationDays:  plan.DurationDays,
		PayChannel:    vipdomain.ChannelWallet,
		PayAmount:     plan.Price,
		BonusIntegral: plan.BonusIntegral,
		Metadata: map[string]any{
			"idempotencyKey": strings.TrimSpace(idempotencyKey),
			"clientIp":       clientIP,
		},
	}
	txn, err := extendUserVipTx(ctx, tx, grant)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil

	balance := decimal.Zero
	walletTxnNo := ""
	if walletResult != nil {
		balance = walletResult.Wallet.Balance
		walletTxnNo = walletResult.Transaction.TransactionNo
	}
	return &vipdomain.PurchaseResult{
		Transaction:         *txn,
		Status:              *buildVipStatus(&txn.ExpireAfter),
		WalletBalance:       balance,
		BonusIntegral:       plan.BonusIntegral,
		WalletTransactionNo: walletTxnNo,
	}, nil
}

// findWalletTxnNoByIdemKey 幂等重放时回查扣款流水号。
//
// 只在重放分支上跑，因此多这一次查询不影响正常购买路径；
// 没有它的话，重试一次购买请求返回的结果里就没有凭证入口 ——
// 而重试恰恰是最容易发生在「用户没看到结果」之后的那次点击上。
func findWalletTxnNoByIdemKey(ctx context.Context, tx pgx.Tx, idemKey string) string {
	if idemKey = strings.TrimSpace(idemKey); idemKey == "" {
		return ""
	}
	var transactionNo string
	if err := tx.QueryRow(ctx,
		`SELECT transaction_no FROM wallet_transactions WHERE idempotency_key = $1 LIMIT 1`,
		idemKey).Scan(&transactionNo); err != nil {
		return ""
	}
	return transactionNo
}

func scanVipPlan(row interface{ Scan(dest ...any) error }) (*vipdomain.Plan, error) {
	var p vipdomain.Plan
	var price string
	var originalPrice *string
	if err := row.Scan(&p.ID, &p.AppID, &p.Name, &p.Kind, &p.TrialDeviceLimited, &p.Features,
		&p.DurationDays, &price, &originalPrice, &p.BonusIntegral, &p.Description, &p.IsActive,
		&p.SortOrder, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	p.Price = decimal.RequireFromString(price)
	if originalPrice != nil {
		op := decimal.RequireFromString(*originalPrice)
		p.OriginalPrice = &op
	}
	return &p, nil
}

func scanVipTxn(row interface{ Scan(dest ...any) error }) (*vipdomain.Transaction, error) {
	var t vipdomain.Transaction
	var payAmount string
	var meta []byte
	if err := row.Scan(&t.ID, &t.TransactionNo, &t.UserID, &t.AppID, &t.PlanID, &t.PlanName, &t.Features,
		&t.DurationDays, &t.PayChannel, &payAmount, &t.RelatedOrderNo, &t.BonusIntegral, &t.ExpireBefore,
		&t.ExpireAfter, &t.Operator, &meta, &t.RevokedAt, &t.RevokeReason, &t.CreatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	t.PayAmount = decimal.RequireFromString(payAmount)
	_ = json.Unmarshal(meta, &t.Metadata)
	return &t, nil
}
