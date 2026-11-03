package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	pointdomain "aegis/internal/domain/points"
)

// 管理员调整积分 / 经验。
//
// 数值安全靠三层：
//  1. 服务层校验数量（非零、单次上限）与原因长度；
//  2. 这里在锁住用户行之后按 pointdomain.ApplyDelta 计算新余额，不足、超上限、int64 溢出都拒绝；
//  3. 数据库 users 上的 CHECK 约束（000098）兜底，任何绕过前两层的写入都会被拒。
//
// 防重复提交：带请求编号时，流水号由编号派生（ADI/ADE + SHA-256 前 32 位十六进制）。
// 先锁用户行再查流水号，同一编号的并发请求在行锁上排队，后到的那个必然看到前一个
// 已提交的流水并原样返回（Replayed=true），不会扣两次。

// adminAdjustTxnNo 由请求编号派生流水号；不同类型用不同前缀，避免两张流水表的号互相占用。
func adminAdjustTxnNo(kind pointdomain.Kind, requestID string) string {
	if requestID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(requestID))
	prefix := "ADI"
	if kind == pointdomain.KindExperience {
		prefix = "ADE"
	}
	return prefix + hex.EncodeToString(sum[:16])
}

// adminBatchNo 批量调整的批次号；行级流水号为「批次号-用户ID」，总长不超过 VARCHAR(40)。
func adminBatchNo(requestID string) string {
	if requestID == "" {
		return "INTB" + time.Now().UTC().Format("060102150405") + randomDigits(4)
	}
	sum := sha256.Sum256([]byte(requestID))
	return "IB" + hex.EncodeToString(sum[:8])
}

func adminAdjustExtra(opts pointdomain.AdminAdjustOptions, reason string, requestID string, batch bool) map[string]any {
	extra := map[string]any{
		"adminId":      opts.AdminID,
		"adminAccount": opts.AdminAccount,
		"reason":       reason,
		"clientIp":     opts.ClientIP,
		"userAgent":    opts.UserAgent,
	}
	if requestID != "" {
		extra["requestId"] = requestID
	}
	if batch {
		extra["batch"] = true
	}
	for key, value := range opts.Extra {
		if _, taken := extra[key]; !taken {
			extra[key] = value
		}
	}
	return extra
}

// AdminAdjustIntegral 管理员调整单个用户的积分（可加可减）。
func (r *Repository) AdminAdjustIntegral(ctx context.Context, in pointdomain.AdjustInput, opts pointdomain.AdminAdjustOptions) (*pointdomain.IntegralAdjustResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockUserRow(ctx, tx, in.UserID, in.AppID); err != nil {
		return nil, err
	}
	txnNo := adminAdjustTxnNo(pointdomain.KindIntegral, in.RequestID)
	if txnNo != "" {
		if replay, err := findIntegralReplay(ctx, tx, txnNo, in); err != nil || replay != nil {
			return replay, err
		}
	}

	title, txnType, operation := "管理员手动调整", "earn", "add"
	if in.Amount < 0 {
		title, txnType, operation = "管理员手动扣除", "consume", "consume"
	}
	reason := in.Reason
	if reason == "" {
		reason = title
	}
	change, err := applyIntegralChangeTxNo(ctx, tx, integralChange{
		UserID: in.UserID, AppID: in.AppID, Amount: in.Amount, TxnType: txnType, Category: "admin_adjust",
		Title: title, Description: reason, SourceType: "admin_manual", SourceID: nullableAdminID(opts.AdminID),
		Extra: adminAdjustExtra(opts, reason, in.RequestID, false), TransactionNo: txnNo,
		ClientIP: opts.ClientIP, UserAgent: opts.UserAgent,
	})
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &pointdomain.IntegralAdjustResult{
		UserID:        in.UserID,
		AppID:         in.AppID,
		Account:       change.Account,
		Amount:        in.Amount,
		BeforeAmount:  change.Before,
		AfterAmount:   change.After,
		Reason:        reason,
		OperationType: operation,
		TransactionNo: change.TransactionNo,
		CreatedAt:     time.Now().UTC(),
	}, nil
}

// AdminAdjustExperience 管理员调整单个用户的经验：正数增加，负数扣减（等级按 in.LevelMode 处理）。
func (r *Repository) AdminAdjustExperience(ctx context.Context, in pointdomain.AdjustInput, opts pointdomain.AdminAdjustOptions) (*pointdomain.ExperienceAdjustResult, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := lockUserRow(ctx, tx, in.UserID, in.AppID); err != nil {
		return nil, err
	}
	txnNo := adminAdjustTxnNo(pointdomain.KindExperience, in.RequestID)
	if txnNo != "" {
		if replay, err := findExperienceReplay(ctx, tx, txnNo, in); err != nil || replay != nil {
			return replay, err
		}
	}
	reason := in.Reason
	var result *pointdomain.ExperienceAdjustResult
	if in.Amount < 0 {
		if reason == "" {
			reason = "管理员手动扣减经验值"
		}
		result, err = r.applyExperienceDeductTx(ctx, tx, experienceDeduction{
			UserID: in.UserID, AppID: in.AppID, Amount: in.Amount, Category: "admin_adjust", Title: "管理员手动扣减",
			Description: reason, SourceType: "admin_manual", SourceID: nullableAdminID(opts.AdminID),
			Extra: adminAdjustExtra(opts, reason, in.RequestID, false), TransactionNo: txnNo,
			LevelMode: in.LevelMode, Operator: opts.AdminAccount,
		})
	} else {
		if reason == "" {
			reason = "管理员手动调整经验值"
		}
		result, err = r.applyExperienceChangeTxNo(ctx, tx, in.UserID, in.AppID, in.Amount,
			"admin_adjust", "管理员手动调整", reason, "admin_manual", nullableAdminID(opts.AdminID),
			adminAdjustExtra(opts, reason, in.RequestID, false), txnNo)
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return result, nil
}

// AdminBatchAdjustIntegral 管理员批量调整积分：一个事务、一条语句完成锁定、更新与记账。
//
// 逐用户返回成败：不存在或不属于该应用 → 用户不存在；扣减后为负 → 积分不足；
// 增加后超过 MaxBalance → 超过上限。其余用户正常生效。
// 带请求编号时，同一编号再次提交直接返回首次的结果。
func (r *Repository) AdminBatchAdjustIntegral(ctx context.Context, appID int64, userIDs []int64, amount int64, reason string, requestID string, opts pointdomain.AdminAdjustOptions) ([]pointdomain.IntegralAdjustResult, []pointdomain.BatchAdjustFailure, bool, error) {
	if len(userIDs) == 0 {
		return nil, nil, false, nil
	}
	title, txnType, operation := "管理员批量调整", "earn", "add"
	if amount < 0 {
		title, txnType, operation = "管理员批量扣除", "consume", "consume"
	}
	if reason == "" {
		reason = title
	}
	batchNo := adminBatchNo(requestID)

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, nil, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if requestID != "" {
		// 同一批次号的并发提交在这把事务级咨询锁上排队
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, batchNo); err != nil {
			return nil, nil, false, err
		}
		results, err := loadBatchReplay(ctx, tx, appID, batchNo, amount)
		if err != nil {
			return nil, nil, false, err
		}
		if results != nil {
			return results, nil, true, nil
		}
	}

	extraJSON, _ := json.Marshal(adminAdjustExtra(opts, reason, requestID, true))
	rows, err := tx.Query(ctx, `WITH target AS (
    SELECT id, account, integral FROM users
    WHERE appid = $1 AND id = ANY($2)
    ORDER BY id
    FOR UPDATE
), updated AS (
    UPDATE users u
    SET integral = u.integral + $3, updated_at = NOW()
    FROM target t
    WHERE u.id = t.id AND t.integral + $3 >= 0 AND t.integral <= $12 - GREATEST($3, 0)
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
		appID, userIDs, amount, batchNo, txnType, title, reason,
		nullableAdminID(opts.AdminID), nullableString(opts.ClientIP), nullableString(opts.UserAgent), extraJSON, pointdomain.MaxBalance)
	if err != nil {
		return nil, nil, false, err
	}
	now := time.Now().UTC()
	found := make(map[int64]bool, len(userIDs))
	results := make([]pointdomain.IntegralAdjustResult, 0, len(userIDs))
	failures := make([]pointdomain.BatchAdjustFailure, 0)
	for rows.Next() {
		var id, before, after int64
		var account string
		var applied bool
		if err := rows.Scan(&id, &account, &before, &after, &applied); err != nil {
			rows.Close()
			return nil, nil, false, err
		}
		found[id] = true
		if !applied {
			message := fmt.Sprintf("用户积分不足（当前 %s）", pointdomain.GroupDigits(before))
			if amount > 0 {
				message = "调整后积分将超过上限"
			}
			failures = append(failures, pointdomain.BatchAdjustFailure{UserID: id, Error: message})
			continue
		}
		results = append(results, pointdomain.IntegralAdjustResult{
			UserID: id, AppID: appID, Account: account, Amount: amount,
			BeforeAmount: before, AfterAmount: after, Reason: reason, OperationType: operation,
			TransactionNo: fmt.Sprintf("%s-%d", batchNo, id), CreatedAt: now,
		})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, nil, false, err
	}
	for _, id := range userIDs {
		if !found[id] {
			failures = append(failures, pointdomain.BatchAdjustFailure{UserID: id, Error: "用户不存在"})
		}
	}
	return results, failures, false, nil
}

func lockUserRow(ctx context.Context, tx pgx.Tx, userID, appID int64) error {
	var id int64
	err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`, userID, appID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrUserNotFound
	}
	return err
}

func findIntegralReplay(ctx context.Context, tx pgx.Tx, txnNo string, in pointdomain.AdjustInput) (*pointdomain.IntegralAdjustResult, error) {
	var (
		userID, appID, amount, before, after int64
		reason                               *string
		createdAt                            time.Time
		account                              string
	)
	err := tx.QueryRow(ctx, `SELECT t.user_id, t.appid, t.amount, t.balance_before, t.balance_after, t.description, t.created_at, u.account
FROM integral_transactions t JOIN users u ON u.id = t.user_id WHERE t.transaction_no = $1`, txnNo).
		Scan(&userID, &appID, &amount, &before, &after, &reason, &createdAt, &account)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if userID != in.UserID || appID != in.AppID || amount != in.Amount {
		return nil, ErrRequestIDConflict
	}
	operation := "add"
	if amount < 0 {
		operation = "consume"
	}
	return &pointdomain.IntegralAdjustResult{
		UserID: userID, AppID: appID, Account: account, Amount: amount, BeforeAmount: before, AfterAmount: after,
		Reason: derefString(reason), OperationType: operation, TransactionNo: txnNo, CreatedAt: createdAt, Replayed: true,
	}, nil
}

func findExperienceReplay(ctx context.Context, tx pgx.Tx, txnNo string, in pointdomain.AdjustInput) (*pointdomain.ExperienceAdjustResult, error) {
	var (
		userID, appID, amount, before, after int64
		levelBefore, levelAfter              int
		reason, levelMode                    *string
		createdAt                            time.Time
		account                              string
	)
	err := tx.QueryRow(ctx, `SELECT t.user_id, t.appid, t.amount, t.balance_before, t.balance_after, t.level_before, t.level_after,
t.description, t.extra_data->>'levelMode', t.created_at, u.account
FROM experience_transactions t JOIN users u ON u.id = t.user_id WHERE t.transaction_no = $1`, txnNo).
		Scan(&userID, &appID, &amount, &before, &after, &levelBefore, &levelAfter, &reason, &levelMode, &createdAt, &account)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if userID != in.UserID || appID != in.AppID || amount != in.Amount {
		return nil, ErrRequestIDConflict
	}
	operation := "add"
	if amount < 0 {
		operation = "consume"
	}
	return &pointdomain.ExperienceAdjustResult{
		UserID: userID, AppID: appID, Account: account, Amount: amount, BeforeAmount: before, AfterAmount: after,
		Reason: derefString(reason), OperationType: operation, TransactionNo: txnNo,
		LevelChanged: levelAfter != levelBefore, OldLevel: levelBefore, NewLevel: levelAfter,
		LevelMode: pointdomain.LevelMode(derefString(levelMode)), CreatedAt: createdAt, Replayed: true,
	}, nil
}

// loadBatchReplay 读出某批次已记账的流水；批次不存在返回 nil。
// 批次号相同但应用或数量不同，说明请求编号被挪用，按冲突处理。
func loadBatchReplay(ctx context.Context, tx pgx.Tx, appID int64, batchNo string, amount int64) ([]pointdomain.IntegralAdjustResult, error) {
	rows, err := tx.Query(ctx, `SELECT t.user_id, t.appid, t.amount, t.balance_before, t.balance_after, t.description, t.created_at, u.account
FROM integral_transactions t JOIN users u ON u.id = t.user_id
WHERE t.transaction_no LIKE $1 ORDER BY t.user_id`, strings.ReplaceAll(batchNo, "_", `\_`)+"-%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pointdomain.IntegralAdjustResult
	for rows.Next() {
		var item pointdomain.IntegralAdjustResult
		var reason *string
		if err := rows.Scan(&item.UserID, &item.AppID, &item.Amount, &item.BeforeAmount, &item.AfterAmount, &reason, &item.CreatedAt, &item.Account); err != nil {
			return nil, err
		}
		if item.AppID != appID || item.Amount != amount {
			return nil, ErrRequestIDConflict
		}
		item.Reason = derefString(reason)
		item.OperationType = "add"
		if item.Amount < 0 {
			item.OperationType = "consume"
		}
		item.TransactionNo = fmt.Sprintf("%s-%d", batchNo, item.UserID)
		item.Replayed = true
		out = append(out, item)
	}
	return out, rows.Err()
}

func nullableAdminID(id int64) *int64 {
	if id == 0 {
		return nil
	}
	return &id
}
