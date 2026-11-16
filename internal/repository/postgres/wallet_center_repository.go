package postgres

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	walletdomain "aegis/internal/domain/wallet"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// ErrWalletPackageNotFound 充值档位不存在
var ErrWalletPackageNotFound = errors.New("postgres: wallet recharge package not found")

// ── 余额兑换积分 ──

// ExchangeWalletToIntegral 充值余额兑换积分（单事务）：
//
//	锁 users → 幂等检查 → 锁钱包 → 定额度（全部 / 指定金额，按比例报价）→ 判下限、单笔上限、每日上限 →
//	只扣本金出账 → 积分入账（成长调控不管这一来源：这是用户花钱换的，不是赚的）
//
// 锁序 users → user_wallets 与全库一致。幂等键命中时原样返回首次结果，不重复扣款。
func (r *Repository) ExchangeWalletToIntegral(ctx context.Context, in walletdomain.ExchangeInput) (*walletdomain.ExchangeResult, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	var integral int64
	if err := tx.QueryRow(ctx, `SELECT integral FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`,
		in.UserID, in.AppID).Scan(&integral); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	idemKey := strings.TrimSpace(in.IdempotencyKey)
	if idemKey != "" {
		existing, err := scanWalletTxn(tx.QueryRow(ctx,
			`SELECT `+walletTxnColumns+` FROM wallet_transactions WHERE idempotency_key = $1 LIMIT 1`, idemKey))
		if err != nil {
			return nil, err
		}
		if existing != nil {
			if existing.UserID != in.UserID || existing.Type != walletdomain.TxnTypeIntegralExchange {
				return nil, ErrIdempotencyConflict
			}
			w, err := lockWalletTx(ctx, tx, in.UserID, in.AppID)
			if err != nil {
				return nil, err
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			tx = nil
			return &walletdomain.ExchangeResult{
				Transaction: *existing, Wallet: *w,
				Integral:              metaInt64Value(existing.Metadata, "integral"),
				IntegralBalance:       integral,
				IntegralTransactionNo: metaStringValue(existing.Metadata, "integralTransactionNo"),
				Rate:                  metaInt64Value(existing.Metadata, "rate"),
				Replayed:              true,
			}, nil
		}
	}

	w, err := lockWalletTx(ctx, tx, in.UserID, in.AppID)
	if err != nil {
		return nil, err
	}
	used, err := walletExchangedSinceQ(ctx, tx, in.UserID, in.AppID, in.DayStart)
	if err != nil {
		return nil, err
	}

	requested := in.Amount
	if in.All {
		// 「全部兑换」= 此刻能兑的上限：本金、单笔上限、今日剩余取小
		requested = walletdomain.BuildExchangeQuota(in.Settings, in.Rate, w.CashBalance, used).MaxNow
		if !requested.IsPositive() {
			if w.CashBalance.IsPositive() {
				return nil, walletdomain.ErrExchangeDailyLimit
			}
			return nil, ErrInsufficientCashBalance
		}
	}
	if err := walletdomain.CheckExchange(in.Settings, requested, used); err != nil {
		return nil, err
	}
	if requested.GreaterThan(w.CashBalance) {
		return nil, ErrInsufficientCashBalance
	}
	points, cost, err := walletdomain.QuoteExchange(requested, in.Rate)
	if err != nil {
		return nil, err
	}
	if points <= 0 {
		return nil, walletdomain.ErrExchangeNoPoints
	}

	integralNo := generateTransactionNo("INT")
	change, err := applyWalletChangeTx(ctx, tx, walletdomain.Change{
		UserID:         in.UserID,
		AppID:          in.AppID,
		Type:           walletdomain.TxnTypeIntegralExchange,
		Amount:         cost.Neg(),
		Spend:          walletdomain.SpendCashOnly,
		IdempotencyKey: idemKey,
		Title:          "兑换积分",
		Remark:         strconv.FormatInt(points, 10) + " 积分",
		ClientIP:       in.ClientIP,
		Metadata: map[string]any{
			"integral": points, "rate": in.Rate, "integralTransactionNo": integralNo,
		},
	})
	if err != nil {
		return nil, err
	}
	credit, err := applyIntegralChangeTxNo(ctx, tx, integralChange{
		UserID: in.UserID, AppID: in.AppID, Amount: points, TxnType: "earn",
		Category: "wallet_exchange", Title: "余额兑换积分",
		Description: "充值余额 " + cost.StringFixed(2) + " 兑换",
		SourceType:  "wallet_exchange", TransactionNo: integralNo,
		Extra:    map[string]any{"walletTransactionNo": change.Transaction.TransactionNo, "rate": in.Rate},
		ClientIP: in.ClientIP, UserAgent: in.UserAgent,
	})
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return &walletdomain.ExchangeResult{
		Transaction:           change.Transaction,
		Wallet:                change.Wallet,
		Integral:              credit.Granted,
		IntegralBalance:       credit.After,
		IntegralTransactionNo: credit.TransactionNo,
		Rate:                  in.Rate,
	}, nil
}

// ErrIdempotencyConflict 幂等键已被另一种操作占用
var ErrIdempotencyConflict = errors.New("postgres: idempotency key used by another operation")

// WalletExchangedSince 某用户自 since 起已兑换的本金。
func (r *Repository) WalletExchangedSince(ctx context.Context, userID, appID int64, since time.Time) (decimal.Decimal, error) {
	return walletExchangedSinceQ(ctx, r.pool, userID, appID, since)
}

func walletExchangedSinceQ(ctx context.Context, q queryRower, userID, appID int64, since time.Time) (decimal.Decimal, error) {
	var sum string
	if err := q.QueryRow(ctx,
		`SELECT COALESCE(-SUM(amount), 0)::text FROM wallet_transactions
WHERE user_id = $1 AND appid = $2 AND type = $3 AND created_at >= $4`,
		userID, appID, walletdomain.TxnTypeIntegralExchange, since).Scan(&sum); err != nil {
		return decimal.Zero, err
	}
	return decimal.RequireFromString(sum), nil
}

// GetUserIntegralBalance 用户当前积分。
func (r *Repository) GetUserIntegralBalance(ctx context.Context, userID, appID int64) (int64, error) {
	var integral int64
	err := r.pool.QueryRow(ctx, `SELECT integral FROM users WHERE id = $1 AND appid = $2`, userID, appID).Scan(&integral)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrUserNotFound
	}
	return integral, err
}

func metaInt64Value(meta map[string]any, key string) int64 {
	switch v := meta[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	}
	return 0
}

func metaStringValue(meta map[string]any, key string) string {
	if v, ok := meta[key].(string); ok {
		return v
	}
	return ""
}

// ── 充值档位 ──

const walletPackageColumns = `id, appid, title, amount::text, bonus_amount::text, bonus_integral, COALESCE(badge, ''),
COALESCE(description, ''), per_user_limit, is_active, sort_order, starts_at, ends_at, created_at, updated_at`

func scanWalletPackage(row interface{ Scan(dest ...any) error }) (*walletdomain.RechargePackage, error) {
	var p walletdomain.RechargePackage
	var amount, bonus string
	if err := row.Scan(&p.ID, &p.AppID, &p.Title, &amount, &bonus, &p.BonusIntegral, &p.Badge, &p.Description,
		&p.PerUserLimit, &p.IsActive, &p.SortOrder, &p.StartsAt, &p.EndsAt, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return nil, err
	}
	p.Amount = decimal.RequireFromString(amount)
	p.BonusAmount = decimal.RequireFromString(bonus)
	return &p, nil
}

// ListWalletPackages 应用的充值档位（activeOnly 时只列上架的），按排序值、ID 升序。
func (r *Repository) ListWalletPackages(ctx context.Context, appID int64, activeOnly bool) ([]walletdomain.RechargePackage, error) {
	query := `SELECT ` + walletPackageColumns + ` FROM wallet_recharge_packages WHERE appid = $1`
	if activeOnly {
		query += ` AND is_active`
	}
	rows, err := r.pool.Query(ctx, query+` ORDER BY sort_order, id`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]walletdomain.RechargePackage, 0, 8)
	for rows.Next() {
		p, err := scanWalletPackage(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *p)
	}
	return items, rows.Err()
}

// GetWalletPackage 取一个档位；不存在返回 ErrWalletPackageNotFound。
func (r *Repository) GetWalletPackage(ctx context.Context, appID, id int64) (*walletdomain.RechargePackage, error) {
	p, err := scanWalletPackage(r.pool.QueryRow(ctx,
		`SELECT `+walletPackageColumns+` FROM wallet_recharge_packages WHERE appid = $1 AND id = $2`, appID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrWalletPackageNotFound
	}
	return p, err
}

// SaveWalletPackage 新建（ID 为 0）或更新档位。
func (r *Repository) SaveWalletPackage(ctx context.Context, p walletdomain.RechargePackage) (*walletdomain.RechargePackage, error) {
	args := []any{p.AppID, p.Title, p.Amount.StringFixed(2), p.BonusAmount.StringFixed(2), p.BonusIntegral,
		nullableString(p.Badge), nullableString(p.Description), p.PerUserLimit, p.IsActive, p.SortOrder, p.StartsAt, p.EndsAt}
	if p.ID <= 0 {
		return scanWalletPackage(r.pool.QueryRow(ctx,
			`INSERT INTO wallet_recharge_packages (appid, title, amount, bonus_amount, bonus_integral, badge, description,
per_user_limit, is_active, sort_order, starts_at, ends_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING `+walletPackageColumns, args...))
	}
	args = append(args, p.ID)
	saved, err := scanWalletPackage(r.pool.QueryRow(ctx,
		`UPDATE wallet_recharge_packages SET title = $2, amount = $3, bonus_amount = $4, bonus_integral = $5, badge = $6,
description = $7, per_user_limit = $8, is_active = $9, sort_order = $10, starts_at = $11, ends_at = $12, updated_at = NOW()
WHERE appid = $1 AND id = $13
RETURNING `+walletPackageColumns, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrWalletPackageNotFound
	}
	return saved, err
}

// DeleteWalletPackage 删除档位。已下的订单不受影响：履约依据的是订单里的快照。
func (r *Repository) DeleteWalletPackage(ctx context.Context, appID, id int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM wallet_recharge_packages WHERE appid = $1 AND id = $2`, appID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrWalletPackageNotFound
	}
	return nil
}

// ReorderWalletPackages 按给定顺序重写排序值（只动传入的、属于该应用的档位）。
func (r *Repository) ReorderWalletPackages(ctx context.Context, appID int64, ids []int64) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE wallet_recharge_packages p SET sort_order = o.ord, updated_at = NOW()
FROM unnest($2::bigint[]) WITH ORDINALITY AS o(id, ord)
WHERE p.appid = $1 AND p.id = o.id`, appID, ids)
	return err
}

// WalletPackagePurchaseCounts 用户在各档位上已付款的充值订单数（限购依据）。
// 退款不让出名额：限购防的是「首充礼反复领」，退款再买正是那条路。
func (r *Repository) WalletPackagePurchaseCounts(ctx context.Context, appID, userID int64) (map[int64]int, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT metadata->>'walletPackageId', COUNT(*) FROM payment_orders
WHERE appid = $1 AND user_id = $2 AND paid_at IS NOT NULL AND metadata->>'walletPackageId' IS NOT NULL
GROUP BY 1`, appID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var raw string
		var n int
		if err := rows.Scan(&raw, &n); err != nil {
			return nil, err
		}
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
			out[id] = n
		}
	}
	return out, rows.Err()
}

// WalletPackageStats 每个档位的已付款订单数与金额（控制台档位列表用）。
func (r *Repository) WalletPackageStats(ctx context.Context, appID int64) (map[int64]walletdomain.PackageStat, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT metadata->>'walletPackageId', COUNT(*), COUNT(DISTINCT user_id), COALESCE(SUM(amount), 0)::text FROM payment_orders
WHERE appid = $1 AND paid_at IS NOT NULL AND metadata->>'walletPackageId' IS NOT NULL
GROUP BY 1`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]walletdomain.PackageStat{}
	for rows.Next() {
		var raw, amount string
		var stat walletdomain.PackageStat
		if err := rows.Scan(&raw, &stat.Orders, &stat.Users, &amount); err != nil {
			return nil, err
		}
		stat.Amount = decimal.RequireFromString(amount)
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
			out[id] = stat
		}
	}
	return out, rows.Err()
}
