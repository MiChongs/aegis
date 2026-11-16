package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"aegis/internal/domain/growth"
	pointdomain "aegis/internal/domain/points"
	walletdomain "aegis/internal/domain/wallet"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// ErrInsufficientBalance 钱包余额不足
var ErrInsufficientBalance = errors.New("postgres: insufficient wallet balance")

// ErrInsufficientCashBalance 充值余额（本金）不足：只扣本金的出账（兑换积分、管理员按桶扣减）
var ErrInsufficientCashBalance = errors.New("postgres: insufficient wallet cash balance")

// ErrInsufficientBonusBalance 赠送余额不足：管理员指定只从赠送余额扣减
var ErrInsufficientBonusBalance = errors.New("postgres: insufficient wallet bonus balance")

const walletTxnColumns = `id, transaction_no, user_id, appid, type, amount, balance_before, balance_after,
COALESCE(related_order_no, ''), COALESCE(idempotency_key, ''), title, COALESCE(remark, ''),
COALESCE(operator, ''), COALESCE(client_ip, ''), COALESCE(metadata, '{}'::jsonb), created_at,
bonus_amount, COALESCE(restores_txn_no, '')`

// walletColumns user_wallets 的读取列，顺序与 scanWallet 一致。
const walletColumns = `user_id, appid, balance, frozen, total_recharged, total_consumed, created_at, updated_at,
bonus_balance, frozen_bonus, total_bonus, total_exchanged`

// GetWalletByUser 查询用户钱包；从未发生过余额变动时返回零值钱包（不落库）
func (r *Repository) GetWalletByUser(ctx context.Context, userID int64, appID int64) (*walletdomain.Wallet, error) {
	w, err := scanWallet(r.pool.QueryRow(ctx,
		`SELECT `+walletColumns+` FROM user_wallets WHERE user_id = $1 AND appid = $2 LIMIT 1`, userID, appID))
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
			Balance: decimal.Zero, CashBalance: decimal.Zero, BonusBalance: decimal.Zero,
			Frozen: decimal.Zero, FrozenBonus: decimal.Zero,
			TotalRecharged: decimal.Zero, TotalConsumed: decimal.Zero,
			TotalBonus: decimal.Zero, TotalExchanged: decimal.Zero}, nil
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
//  4. 按桶分配（walletdomain.Allocate）：入账落本金还是赠送余额、出账先扣哪一桶，
//     不够则拒绝 → 更新余额、赠送余额与累计值 → 写入流水（记下赠送余额上的那一份）
func applyWalletChangeTx(ctx context.Context, tx pgx.Tx, change walletdomain.Change) (*walletdomain.ChangeResult, error) {
	if change.Amount.IsZero() {
		return nil, fmt.Errorf("wallet change amount cannot be zero")
	}
	w, err := lockWalletTx(ctx, tx, change.UserID, change.AppID)
	if err != nil {
		return nil, err
	}

	// 幂等命中：钱包行已锁定，此检查与插入之间不存在并发窗口
	idemKey := strings.TrimSpace(change.IdempotencyKey)
	if idemKey != "" {
		existing, err := scanWalletTxn(tx.QueryRow(ctx,
			`SELECT `+walletTxnColumns+` FROM wallet_transactions WHERE idempotency_key = $1 LIMIT 1`, idemKey))
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return &walletdomain.ChangeResult{Transaction: *existing, Wallet: *w, Replayed: true}, nil
		}
	}

	// 退回：找到原出账，算出它扣掉的赠送余额还剩多少没退回
	var restorable *decimal.Decimal
	restoresNo := ""
	if change.Amount.IsPositive() && change.Restores != nil {
		origNo, remaining, err := restorableBonusTx(ctx, tx, change.UserID, change.AppID, *change.Restores)
		if err != nil {
			return nil, err
		}
		if origNo != "" {
			restoresNo = origNo
			restorable = &remaining
		}
	}

	alloc, err := walletdomain.Allocate(w.Balance, w.BonusBalance, change, restorable)
	if err != nil {
		switch {
		case errors.Is(err, walletdomain.ErrInsufficientCash):
			return nil, ErrInsufficientCashBalance
		case errors.Is(err, walletdomain.ErrInsufficientBonus):
			return nil, ErrInsufficientBonusBalance
		default:
			return nil, ErrInsufficientBalance
		}
	}

	before := w.Balance
	after := before.Add(change.Amount)
	bonusAfter := w.BonusBalance.Add(alloc.Bonus)

	// 累计值：充值 / 退款入账计入 total_recharged；兑换积分计入 total_exchanged，
	// 其余出账计入 total_consumed；非退回的赠送入账计入 total_bonus。
	addRecharged, addConsumed, addExchanged, addBonus := decimal.Zero, decimal.Zero, decimal.Zero, decimal.Zero
	if change.Amount.IsPositive() {
		if change.Type == walletdomain.TxnTypeRecharge || change.Type == walletdomain.TxnTypeRefund {
			addRecharged = change.Amount
		}
		if restoresNo == "" {
			addBonus = alloc.Bonus
		}
	} else if change.Type == walletdomain.TxnTypeIntegralExchange {
		addExchanged = change.Amount.Neg()
	} else {
		addConsumed = change.Amount.Neg()
	}
	if _, err := tx.Exec(ctx,
		`UPDATE user_wallets SET balance = $3, bonus_balance = $4, total_recharged = total_recharged + $5,
total_consumed = total_consumed + $6, total_exchanged = total_exchanged + $7, total_bonus = total_bonus + $8,
updated_at = NOW() WHERE user_id = $1 AND appid = $2`,
		change.UserID, change.AppID, after.StringFixed(2), bonusAfter.StringFixed(2), addRecharged.StringFixed(2),
		addConsumed.StringFixed(2), addExchanged.StringFixed(2), addBonus.StringFixed(2)); err != nil {
		return nil, err
	}

	metaJSON, _ := json.Marshal(change.Metadata)
	if change.Metadata == nil {
		metaJSON = []byte("{}")
	}
	txn := walletdomain.Transaction{
		TransactionNo:  generateTransactionNo("WAL"),
		UserID:         change.UserID,
		AppID:          change.AppID,
		Type:           change.Type,
		Amount:         change.Amount,
		CashAmount:     alloc.Cash,
		BonusAmount:    alloc.Bonus,
		BalanceBefore:  before,
		BalanceAfter:   after,
		RelatedOrderNo: strings.TrimSpace(change.RelatedOrderNo),
		RestoresTxnNo:  restoresNo,
		IdempotencyKey: idemKey,
		Title:          change.Title,
		Remark:         change.Remark,
		Operator:       change.Operator,
		ClientIP:       change.ClientIP,
		Metadata:       change.Metadata,
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO wallet_transactions (transaction_no, user_id, appid, type, amount, balance_before, balance_after,
related_order_no, idempotency_key, title, remark, operator, client_ip, metadata, bonus_amount, restores_txn_no, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, NOW())
RETURNING id, created_at`,
		txn.TransactionNo, txn.UserID, txn.AppID, txn.Type,
		txn.Amount.StringFixed(2), before.StringFixed(2), after.StringFixed(2),
		nullableString(txn.RelatedOrderNo), nullableString(idemKey),
		txn.Title, nullableString(txn.Remark), nullableString(txn.Operator), nullableString(txn.ClientIP), metaJSON,
		alloc.Bonus.StringFixed(2), nullableString(restoresNo)).
		Scan(&txn.ID, &txn.CreatedAt); err != nil {
		return nil, err
	}

	w.Balance = after
	w.BonusBalance = bonusAfter
	w.TotalRecharged = w.TotalRecharged.Add(addRecharged)
	w.TotalConsumed = w.TotalConsumed.Add(addConsumed)
	w.TotalExchanged = w.TotalExchanged.Add(addExchanged)
	w.TotalBonus = w.TotalBonus.Add(addBonus)
	w.Normalize()
	return &walletdomain.ChangeResult{Transaction: txn, Wallet: *w}, nil
}

// lockWalletTx 惰性建行并锁定钱包行（用户校验与建行合一，FK + appid 匹配双保险）。
func lockWalletTx(ctx context.Context, tx pgx.Tx, userID, appID int64) (*walletdomain.Wallet, error) {
	if _, err := tx.Exec(ctx,
		`INSERT INTO user_wallets (user_id, appid) SELECT id, appid FROM users WHERE id = $1 AND appid = $2
ON CONFLICT (user_id) DO NOTHING`, userID, appID); err != nil {
		return nil, err
	}
	w, err := scanWallet(tx.QueryRow(ctx,
		`SELECT `+walletColumns+` FROM user_wallets WHERE user_id = $1 AND appid = $2 FOR UPDATE`, userID, appID))
	if err != nil {
		return nil, err
	}
	if w == nil {
		return nil, ErrUserNotFound
	}
	return w, nil
}

// restorableBonusTx 定位被退回的原出账，返回其流水号与「扣掉的赠送余额里还没退回的部分」。
// 找不到原出账返回空流水号，调用方按入账类型的默认桶处理。
func restorableBonusTx(ctx context.Context, tx pgx.Tx, userID, appID int64, ref walletdomain.RestoreRef) (string, decimal.Decimal, error) {
	var query string
	args := []any{userID, appID}
	switch {
	case strings.TrimSpace(ref.TransactionNo) != "":
		query = `transaction_no = $3`
		args = append(args, strings.TrimSpace(ref.TransactionNo))
	case strings.TrimSpace(ref.IdempotencyKey) != "":
		query = `idempotency_key = $3`
		args = append(args, strings.TrimSpace(ref.IdempotencyKey))
	case strings.TrimSpace(ref.OrderNo) != "" && ref.Type != "":
		query = `related_order_no = $3 AND type = $4`
		args = append(args, strings.TrimSpace(ref.OrderNo), ref.Type)
	default:
		return "", decimal.Zero, nil
	}
	var txnNo, spent string
	err := tx.QueryRow(ctx,
		`SELECT transaction_no, (-bonus_amount)::text FROM wallet_transactions
WHERE user_id = $1 AND appid = $2 AND amount < 0 AND `+query+` ORDER BY id LIMIT 1`, args...).
		Scan(&txnNo, &spent)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", decimal.Zero, nil
	}
	if err != nil {
		return "", decimal.Zero, err
	}
	var restored string
	if err := tx.QueryRow(ctx,
		`SELECT COALESCE(SUM(bonus_amount), 0)::text FROM wallet_transactions
WHERE restores_txn_no = $1 AND amount > 0`, txnNo).Scan(&restored); err != nil {
		return "", decimal.Zero, err
	}
	remaining := decimal.RequireFromString(spent).Sub(decimal.RequireFromString(restored))
	return txnNo, decimal.Max(remaining, decimal.Zero), nil
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
	if types := splitWalletTypes(txnType); len(types) > 0 {
		args = append(args, types)
		where += fmt.Sprintf(" AND type = ANY($%d)", len(args))
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
COALESCE(wt.metadata, '{}'::jsonb), wt.created_at, wt.bonus_amount, COALESCE(wt.restores_txn_no, '')`

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
	if types := splitWalletTypes(query.Type); len(types) > 0 {
		args = append(args, types)
		where += fmt.Sprintf(" AND wt.type = ANY($%d)", len(args))
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
	var balance, frozen, recharged, consumed, bonus, frozenBonus, totalBonus, exchanged string
	if err := row.Scan(&w.UserID, &w.AppID, &balance, &frozen, &recharged, &consumed, &w.CreatedAt, &w.UpdatedAt,
		&bonus, &frozenBonus, &totalBonus, &exchanged); err != nil {
		return nil, normalizeNotFound(err)
	}
	w.Balance = decimal.RequireFromString(balance)
	w.Frozen = decimal.RequireFromString(frozen)
	w.TotalRecharged = decimal.RequireFromString(recharged)
	w.TotalConsumed = decimal.RequireFromString(consumed)
	w.BonusBalance = decimal.RequireFromString(bonus)
	w.FrozenBonus = decimal.RequireFromString(frozenBonus)
	w.TotalBonus = decimal.RequireFromString(totalBonus)
	w.TotalExchanged = decimal.RequireFromString(exchanged)
	w.Normalize()
	return &w, nil
}

// splitWalletTypes 流水类型筛选：支持逗号分隔多个类型（钱包页「赠送」一栏是好几种入账）。
func splitWalletTypes(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if part = strings.TrimSpace(part); part != "" && len(out) < 20 {
			out = append(out, part)
		}
	}
	return out
}

func scanWalletTxn(row interface{ Scan(dest ...any) error }) (*walletdomain.Transaction, error) {
	var t walletdomain.Transaction
	var amount, before, after, bonus string
	var meta []byte
	if err := row.Scan(&t.ID, &t.TransactionNo, &t.UserID, &t.AppID, &t.Type, &amount, &before, &after,
		&t.RelatedOrderNo, &t.IdempotencyKey, &t.Title, &t.Remark, &t.Operator, &t.ClientIP, &meta, &t.CreatedAt,
		&bonus, &t.RestoresTxnNo); err != nil {
		return nil, normalizeNotFound(err)
	}
	t.Amount = decimal.RequireFromString(amount)
	t.BonusAmount = decimal.RequireFromString(bonus)
	t.CashAmount = t.Amount.Sub(t.BonusAmount)
	t.BalanceBefore = decimal.RequireFromString(before)
	t.BalanceAfter = decimal.RequireFromString(after)
	_ = json.Unmarshal(meta, &t.Metadata)
	return &t, nil
}

// scanAdminWalletTxn 管理端行：流水各列之后再跟账号与昵称，顺序与查询里的
// walletTxnColumnsAliased + 两个 COALESCE 一一对应。
func scanAdminWalletTxn(row interface{ Scan(dest ...any) error }) (*walletdomain.AdminTransactionItem, error) {
	var item walletdomain.AdminTransactionItem
	var amount, before, after, bonus string
	var meta []byte
	if err := row.Scan(&item.ID, &item.TransactionNo, &item.UserID, &item.AppID, &item.Type,
		&amount, &before, &after, &item.RelatedOrderNo, &item.IdempotencyKey, &item.Title,
		&item.Remark, &item.Operator, &item.ClientIP, &meta, &item.CreatedAt, &bonus, &item.RestoresTxnNo,
		&item.Account, &item.Nickname); err != nil {
		return nil, normalizeNotFound(err)
	}
	item.Amount = decimal.RequireFromString(amount)
	item.BonusAmount = decimal.RequireFromString(bonus)
	item.CashAmount = item.Amount.Sub(item.BonusAmount)
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
	change, err := applyIntegralChangeTxNo(ctx, tx, integralChange{
		UserID: userID, AppID: appID, Amount: amount, TxnType: txnType, Category: category,
		Title: title, Description: description, SourceType: sourceType, SourceID: sourceID, Extra: extra,
	})
	if err != nil {
		return 0, 0, "", err
	}
	return change.Before, change.After, change.TransactionNo, nil
}

// integralChange 一次积分变更的全部参数；TransactionNo 为空时自动生成。
type integralChange struct {
	UserID        int64
	AppID         int64
	Amount        int64
	TxnType       string
	Category      string
	Title         string
	Description   string
	SourceType    string
	SourceID      *int64
	Extra         map[string]any
	TransactionNo string
	ClientIP      string
	UserAgent     string
}

type integralChangeResult struct {
	Account       string
	Before        int64
	After         int64
	TransactionNo string
	// Granted 实际入账的数量（成长调控规则作用之后，可能与请求的不同）
	Granted int64
}

// applyIntegralChangeTxNo 锁行、按 pointdomain.ApplyDelta 计算新余额（不足或超过上限都拒绝）、更新、记账。
func applyIntegralChangeTxNo(ctx context.Context, tx pgx.Tx, in integralChange) (*integralChangeResult, error) {
	var out integralChangeResult
	if err := tx.QueryRow(ctx, `SELECT account, integral FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`,
		in.UserID, in.AppID).Scan(&out.Account, &out.Before); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	// 成长调控：受管来源的收入按规则换算（管理员调整、充值、会员赠送、折算、退回不受管）
	multiplier := 1.0
	if in.Amount > 0 && in.TxnType == "earn" {
		source := growth.SourceFor(growth.KindIntegral, in.SourceType, strings.Contains(in.Category, "refund"))
		outcome, touched, err := applyGrowthTx(ctx, tx, in.AppID, in.UserID, growth.KindIntegral, source, in.Amount)
		if err != nil {
			return nil, err
		}
		if touched {
			in.Amount, in.Extra, multiplier = outcome.Granted, growthExtra(in.Extra, outcome), outcome.Multiplier()
		}
	}
	out.Granted = in.Amount
	after, err := pointdomain.ApplyDelta(out.Before, in.Amount)
	if err != nil {
		if errors.Is(err, pointdomain.ErrBalanceInsufficient) {
			return nil, &BalanceError{Err: ErrInsufficientIntegral, Kind: pointdomain.KindIntegral, Balance: out.Before}
		}
		return nil, &BalanceError{Err: pointdomain.ErrBalanceOverflow, Kind: pointdomain.KindIntegral, Balance: out.Before}
	}
	out.After = after
	if _, err := tx.Exec(ctx, `UPDATE users SET integral = $1, updated_at = NOW() WHERE id = $2 AND appid = $3`,
		out.After, in.UserID, in.AppID); err != nil {
		return nil, err
	}
	out.TransactionNo = in.TransactionNo
	if out.TransactionNo == "" {
		out.TransactionNo = generateTransactionNo("INT")
	}
	extraJSON, _ := json.Marshal(in.Extra)
	if in.Extra == nil {
		extraJSON = []byte("{}")
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO integral_transactions (transaction_no, user_id, appid, type, category, amount, balance_before, balance_after,
status, title, description, source_id, source_type, multiplier, client_ip, user_agent, extra_data, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'completed', $9, $10, $11, $12, $16, $13, $14, $15, NOW(), NOW())`,
		out.TransactionNo, in.UserID, in.AppID, in.TxnType, in.Category, in.Amount, out.Before, out.After,
		in.Title, nullableString(in.Description), in.SourceID, nullableString(in.SourceType),
		nullableString(in.ClientIP), nullableString(in.UserAgent), extraJSON, multiplier); err != nil {
		return nil, err
	}
	return &out, nil
}
