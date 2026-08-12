package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	walletdomain "aegis/internal/domain/wallet"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// ErrInsufficientBalance 钱包余额不足
var ErrInsufficientBalance = errors.New("postgres: insufficient wallet balance")

const walletTxnColumns = `id, transaction_no, user_id, appid, type, amount, balance_before, balance_after,
COALESCE(related_order_no, ''), COALESCE(idempotency_key, ''), title, COALESCE(remark, ''),
COALESCE(operator, ''), COALESCE(client_ip, ''), COALESCE(metadata, '{}'::jsonb), created_at`

// GetWalletByUser 查询用户钱包；从未发生过余额变动时返回零值钱包（不落库）
func (r *Repository) GetWalletByUser(ctx context.Context, userID int64, appID int64) (*walletdomain.Wallet, error) {
	w, err := scanWallet(r.pool.QueryRow(ctx,
		`SELECT user_id, appid, balance, frozen, total_recharged, total_consumed, created_at, updated_at
FROM user_wallets WHERE user_id = $1 AND appid = $2 LIMIT 1`, userID, appID))
	if err != nil {
		return nil, err
	}
	if w == nil {
		// 确认用户存在后返回零值钱包，避免为只读请求创建行
		var exists bool
		if err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = $1 AND appid = $2)`, userID, appID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, ErrUserNotFound
		}
		return &walletdomain.Wallet{UserID: userID, AppID: appID,
			Balance: decimal.Zero, Frozen: decimal.Zero,
			TotalRecharged: decimal.Zero, TotalConsumed: decimal.Zero}, nil
	}
	return w, nil
}

// ApplyWalletChange 执行一次余额变更（独立事务）。
// 语义见 applyWalletChangeTx；这是业务侧（消费 / 管理员调整 / 退款）的统一入口。
func (r *Repository) ApplyWalletChange(ctx context.Context, change walletdomain.Change) (*walletdomain.ChangeResult, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	result, err := applyWalletChangeTx(ctx, tx, change)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

// applyWalletChangeTx 余额变更核心（必须在事务内调用）：
//  1. 惰性建钱包（仅当用户存在且属于该应用）
//  2. SELECT ... FOR UPDATE 锁定钱包行（同一用户的所有变更串行化）
//  3. 幂等检查：idempotency_key 已存在则原样返回首次流水，不重复入账
//  4. 余额校验（出账不允许为负）→ 更新余额与累计值 → 写入流水
func applyWalletChangeTx(ctx context.Context, tx pgx.Tx, change walletdomain.Change) (*walletdomain.ChangeResult, error) {
	if change.Amount.IsZero() {
		return nil, fmt.Errorf("wallet change amount cannot be zero")
	}
	// 惰性建行：用户校验与建行合一，FK + appid 匹配双保险
	if _, err := tx.Exec(ctx,
		`INSERT INTO user_wallets (user_id, appid) SELECT id, appid FROM users WHERE id = $1 AND appid = $2
ON CONFLICT (user_id) DO NOTHING`, change.UserID, change.AppID); err != nil {
		return nil, err
	}
	var w walletdomain.Wallet
	var balance, frozen, recharged, consumed string
	err := tx.QueryRow(ctx,
		`SELECT user_id, appid, balance, frozen, total_recharged, total_consumed, created_at, updated_at
FROM user_wallets WHERE user_id = $1 AND appid = $2 FOR UPDATE`, change.UserID, change.AppID).
		Scan(&w.UserID, &w.AppID, &balance, &frozen, &recharged, &consumed, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	w.Balance = decimal.RequireFromString(balance)
	w.Frozen = decimal.RequireFromString(frozen)
	w.TotalRecharged = decimal.RequireFromString(recharged)
	w.TotalConsumed = decimal.RequireFromString(consumed)

	// 幂等命中：钱包行已锁定，此检查与插入之间不存在并发窗口
	idemKey := strings.TrimSpace(change.IdempotencyKey)
	if idemKey != "" {
		existing, err := scanWalletTxn(tx.QueryRow(ctx,
			`SELECT `+walletTxnColumns+` FROM wallet_transactions WHERE idempotency_key = $1 LIMIT 1`, idemKey))
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return &walletdomain.ChangeResult{Transaction: *existing, Wallet: w, Replayed: true}, nil
		}
	}

	before := w.Balance
	after := before.Add(change.Amount)
	if after.IsNegative() {
		return nil, ErrInsufficientBalance
	}

	// 累计值：入账计入 total_recharged（仅充值/退款类），出账计入 total_consumed
	addRecharged := decimal.Zero
	addConsumed := decimal.Zero
	if change.Amount.IsPositive() {
		if change.Type == walletdomain.TxnTypeRecharge || change.Type == walletdomain.TxnTypeRefund {
			addRecharged = change.Amount
		}
	} else {
		addConsumed = change.Amount.Neg()
	}
	if _, err := tx.Exec(ctx,
		`UPDATE user_wallets SET balance = $3, total_recharged = total_recharged + $4,
total_consumed = total_consumed + $5, updated_at = NOW() WHERE user_id = $1 AND appid = $2`,
		change.UserID, change.AppID, after.StringFixed(2), addRecharged.StringFixed(2), addConsumed.StringFixed(2)); err != nil {
		return nil, err
	}

	metaJSON, _ := json.Marshal(change.Metadata)
	txn := walletdomain.Transaction{
		TransactionNo:  generateTransactionNo("WAL"),
		UserID:         change.UserID,
		AppID:          change.AppID,
		Type:           change.Type,
		Amount:         change.Amount,
		BalanceBefore:  before,
		BalanceAfter:   after,
		RelatedOrderNo: strings.TrimSpace(change.RelatedOrderNo),
		IdempotencyKey: idemKey,
		Title:          change.Title,
		Remark:         change.Remark,
		Operator:       change.Operator,
		ClientIP:       change.ClientIP,
		Metadata:       change.Metadata,
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO wallet_transactions (transaction_no, user_id, appid, type, amount, balance_before, balance_after,
related_order_no, idempotency_key, title, remark, operator, client_ip, metadata, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, NOW())
RETURNING id, created_at`,
		txn.TransactionNo, txn.UserID, txn.AppID, txn.Type,
		txn.Amount.StringFixed(2), before.StringFixed(2), after.StringFixed(2),
		nullableString(txn.RelatedOrderNo), nullableString(idemKey),
		txn.Title, nullableString(txn.Remark), nullableString(txn.Operator), nullableString(txn.ClientIP), metaJSON).
		Scan(&txn.ID, &txn.CreatedAt); err != nil {
		return nil, err
	}

	w.Balance = after
	w.TotalRecharged = w.TotalRecharged.Add(addRecharged)
	w.TotalConsumed = w.TotalConsumed.Add(addConsumed)
	return &walletdomain.ChangeResult{Transaction: txn, Wallet: w}, nil
}

// ListWalletTransactions 分页查询钱包流水
func (r *Repository) ListWalletTransactions(ctx context.Context, userID int64, appID int64, txnType string, page int, limit int) ([]walletdomain.Transaction, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	args := []any{userID, appID}
	where := ` WHERE user_id = $1 AND appid = $2`
	if txnType = strings.TrimSpace(txnType); txnType != "" {
		args = append(args, txnType)
		where += fmt.Sprintf(" AND type = $%d", len(args))
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM wallet_transactions`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx,
		`SELECT `+walletTxnColumns+` FROM wallet_transactions`+where+
			fmt.Sprintf(` ORDER BY id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]walletdomain.Transaction, 0, limit)
	for rows.Next() {
		item, err := scanWalletTxn(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

// ── 单条流水定位（凭证出具的取数入口）──

// GetWalletTransactionByNo 按流水号取一条流水（管理端）。
// 流水号全局唯一，但仍按 appid 收敛：跨应用取到别人家的流水就是越权。
func (r *Repository) GetWalletTransactionByNo(ctx context.Context, appID int64, transactionNo string) (*walletdomain.Transaction, error) {
	return scanWalletTxn(r.pool.QueryRow(ctx,
		`SELECT `+walletTxnColumns+` FROM wallet_transactions WHERE appid = $1 AND transaction_no = $2 LIMIT 1`,
		appID, strings.TrimSpace(transactionNo)))
}

// GetWalletTransactionByNoForUser 用户侧取自己的流水。
//
// 归属校验写在 SQL 里而不是取回来再比：先查后比会让「流水存在但不是你的」
// 和「流水不存在」走两条不同的分支，多一处就多一次写错的机会。
func (r *Repository) GetWalletTransactionByNoForUser(ctx context.Context, appID int64, userID int64, transactionNo string) (*walletdomain.Transaction, error) {
	return scanWalletTxn(r.pool.QueryRow(ctx,
		`SELECT `+walletTxnColumns+` FROM wallet_transactions
WHERE appid = $1 AND user_id = $2 AND transaction_no = $3 LIMIT 1`,
		appID, userID, strings.TrimSpace(transactionNo)))
}

// ── 管理端全应用流水 ──

// walletTxnColumnsAliased 与 walletTxnColumns 同序，只是带表别名。
// 管理端列表要联表取账号，不带别名会撞上 users 的同名列。
const walletTxnColumnsAliased = `wt.id, wt.transaction_no, wt.user_id, wt.appid, wt.type, wt.amount,
wt.balance_before, wt.balance_after, COALESCE(wt.related_order_no, ''), COALESCE(wt.idempotency_key, ''),
wt.title, COALESCE(wt.remark, ''), COALESCE(wt.operator, ''), COALESCE(wt.client_ip, ''),
COALESCE(wt.metadata, '{}'::jsonb), wt.created_at`

// walletAdminWhere 拼出管理端流水查询的过滤条件与参数。
// 计数与取数必须用同一份条件，因此单独成函数 —— 两处各拼一遍，
// 迟早出现「总数 100、翻到第 2 页却空了」。
func walletAdminWhere(appID int64, query walletdomain.AdminListQuery) (string, []any) {
	args := []any{appID}
	where := ` WHERE wt.appid = $1`
	if query.UserID > 0 {
		args = append(args, query.UserID)
		where += fmt.Sprintf(" AND wt.user_id = $%d", len(args))
	}
	if txnType := strings.TrimSpace(query.Type); txnType != "" {
		args = append(args, txnType)
		where += fmt.Sprintf(" AND wt.type = $%d", len(args))
	}
	switch strings.TrimSpace(query.Direction) {
	case walletdomain.DirectionIn:
		where += " AND wt.amount > 0"
	case walletdomain.DirectionOut:
		where += " AND wt.amount < 0"
	}
	if start := query.Start; start != nil && !start.IsZero() {
		args = append(args, *start)
		where += fmt.Sprintf(" AND wt.created_at >= $%d", len(args))
	}
	if end := query.End; end != nil && !end.IsZero() {
		args = append(args, *end)
		where += fmt.Sprintf(" AND wt.created_at <= $%d", len(args))
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		where += fmt.Sprintf(` AND (wt.transaction_no ILIKE $%d OR wt.related_order_no ILIKE $%d
OR wt.title ILIKE $%d OR wt.remark ILIKE $%d OR u.account ILIKE $%d)`,
			len(args), len(args), len(args), len(args), len(args))
	}
	return where, args
}

// ListWalletTransactionsByApp 管理端按应用分页查询流水（含账号信息）。
func (r *Repository) ListWalletTransactionsByApp(ctx context.Context, appID int64, query walletdomain.AdminListQuery) ([]walletdomain.AdminTransactionItem, int64, error) {
	page, limit := query.Page, query.Limit
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	// 账号联表在计数时也要在：keyword 会命中 u.account，两边条件必须完全一致
	const from = ` FROM wallet_transactions wt
LEFT JOIN users u ON u.id = wt.user_id
LEFT JOIN user_profiles p ON p.user_id = wt.user_id`
	where, args := walletAdminWhere(appID, query)

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+from+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx,
		`SELECT `+walletTxnColumnsAliased+`, COALESCE(u.account, ''), COALESCE(p.nickname, '')`+from+where+
			fmt.Sprintf(` ORDER BY wt.id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]walletdomain.AdminTransactionItem, 0, limit)
	for rows.Next() {
		item, err := scanAdminWalletTxn(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

// WalletStats 应用维度的资金面板。
//
// 入账与出账分开统计：净额为零既可能是没有交易，也可能是充了一万又花了一万，
// 这两种情况的运营含义完全相反。余额合计不受时间窗影响 ——
// 它是一个时点值（平台此刻的待兑付负债），不是区间累计。
func (r *Repository) WalletStats(ctx context.Context, appID int64, start *time.Time, end *time.Time) (*walletdomain.Stats, error) {
	args := []any{appID}
	where := ` WHERE appid = $1`
	if start != nil && !start.IsZero() {
		args = append(args, *start)
		where += fmt.Sprintf(" AND created_at >= $%d", len(args))
	}
	if end != nil && !end.IsZero() {
		args = append(args, *end)
		where += fmt.Sprintf(" AND created_at <= $%d", len(args))
	}

	stats := &walletdomain.Stats{ByType: make([]walletdomain.TypeStat, 0, 6)}
	var totalIn, totalOut string
	if err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(amount) FILTER (WHERE amount > 0), 0)::text,
COALESCE(-SUM(amount) FILTER (WHERE amount < 0), 0)::text,
COUNT(*), COUNT(DISTINCT user_id) FROM wallet_transactions`+where, args...).
		Scan(&totalIn, &totalOut, &stats.Count, &stats.UserCount); err != nil {
		return nil, err
	}
	stats.TotalIn = decimal.RequireFromString(totalIn)
	stats.TotalOut = decimal.RequireFromString(totalOut)
	stats.Net = stats.TotalIn.Sub(stats.TotalOut)

	rows, err := r.pool.Query(ctx,
		`SELECT type, COUNT(*), COALESCE(SUM(amount), 0)::text FROM wallet_transactions`+where+
			` GROUP BY type ORDER BY type`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item walletdomain.TypeStat
		var amount string
		if err := rows.Scan(&item.Type, &item.Count, &amount); err != nil {
			return nil, err
		}
		item.Amount = decimal.RequireFromString(amount)
		stats.ByType = append(stats.ByType, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	var balance string
	if err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(balance), 0)::text FROM user_wallets WHERE appid = $1`, appID).Scan(&balance); err != nil {
		return nil, err
	}
	stats.Balance = decimal.RequireFromString(balance)
	return stats, nil
}

func scanWallet(row interface{ Scan(dest ...any) error }) (*walletdomain.Wallet, error) {
	var w walletdomain.Wallet
	var balance, frozen, recharged, consumed string
	if err := row.Scan(&w.UserID, &w.AppID, &balance, &frozen, &recharged, &consumed, &w.CreatedAt, &w.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	w.Balance = decimal.RequireFromString(balance)
	w.Frozen = decimal.RequireFromString(frozen)
	w.TotalRecharged = decimal.RequireFromString(recharged)
	w.TotalConsumed = decimal.RequireFromString(consumed)
	return &w, nil
}

func scanWalletTxn(row interface{ Scan(dest ...any) error }) (*walletdomain.Transaction, error) {
	var t walletdomain.Transaction
	var amount, before, after string
	var meta []byte
	if err := row.Scan(&t.ID, &t.TransactionNo, &t.UserID, &t.AppID, &t.Type, &amount, &before, &after,
		&t.RelatedOrderNo, &t.IdempotencyKey, &t.Title, &t.Remark, &t.Operator, &t.ClientIP, &meta, &t.CreatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	t.Amount = decimal.RequireFromString(amount)
	t.BalanceBefore = decimal.RequireFromString(before)
	t.BalanceAfter = decimal.RequireFromString(after)
	_ = json.Unmarshal(meta, &t.Metadata)
	return &t, nil
}

// scanAdminWalletTxn 管理端行：流水各列之后再跟账号与昵称，顺序与查询里的
// walletTxnColumnsAliased + 两个 COALESCE 一一对应。
func scanAdminWalletTxn(row interface{ Scan(dest ...any) error }) (*walletdomain.AdminTransactionItem, error) {
	var item walletdomain.AdminTransactionItem
	var amount, before, after string
	var meta []byte
	if err := row.Scan(&item.ID, &item.TransactionNo, &item.UserID, &item.AppID, &item.Type,
		&amount, &before, &after, &item.RelatedOrderNo, &item.IdempotencyKey, &item.Title,
		&item.Remark, &item.Operator, &item.ClientIP, &meta, &item.CreatedAt,
		&item.Account, &item.Nickname); err != nil {
		return nil, normalizeNotFound(err)
	}
	item.Amount = decimal.RequireFromString(amount)
	item.BalanceBefore = decimal.RequireFromString(before)
	item.BalanceAfter = decimal.RequireFromString(after)
	_ = json.Unmarshal(meta, &item.Metadata)
	return &item, nil
}

// applyIntegralChangeTx 事务内积分变更（与管理端调整同构：锁行 → 校验 → 更新 → 记账）。
// 供 VIP 赠送积分、积分直购履约等跨系统场景在同一事务中复用。
func applyIntegralChangeTx(ctx context.Context, tx pgx.Tx, userID int64, appID int64, amount int64,
	txnType string, category string, title string, description string, sourceType string, sourceID *int64,
	extra map[string]any) (balanceBefore int64, balanceAfter int64, transactionNo string, err error) {
	var account string
	if err = tx.QueryRow(ctx, `SELECT account, integral FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`,
		userID, appID).Scan(&account, &balanceBefore); err != nil {
		if err == pgx.ErrNoRows {
			err = ErrUserNotFound
		}
		return 0, 0, "", err
	}
	balanceAfter = balanceBefore + amount
	if balanceAfter < 0 {
		return 0, 0, "", ErrInsufficientIntegral
	}
	if _, err = tx.Exec(ctx, `UPDATE users SET integral = $1, updated_at = NOW() WHERE id = $2 AND appid = $3`,
		balanceAfter, userID, appID); err != nil {
		return 0, 0, "", err
	}
	transactionNo = generateTransactionNo("INT")
	extraJSON, _ := json.Marshal(extra)
	if _, err = tx.Exec(ctx,
		`INSERT INTO integral_transactions (transaction_no, user_id, appid, type, category, amount, balance_before, balance_after,
status, title, description, source_id, source_type, multiplier, extra_data, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'completed', $9, $10, $11, $12, 1, $13, NOW(), NOW())`,
		transactionNo, userID, appID, txnType, category, amount, balanceBefore, balanceAfter,
		title, nullableString(description), sourceID, nullableString(sourceType), extraJSON); err != nil {
		return 0, 0, "", err
	}
	return balanceBefore, balanceAfter, transactionNo, nil
}
