package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	userdomain "aegis/internal/domain/user"
	"github.com/jackc/pgx/v5"
)

const userAccountBanColumns = `
id,
appid,
user_id,
ban_type,
ban_scope,
status,
COALESCE(reason, ''),
COALESCE(evidence, '{}'::jsonb),
banned_by_admin_id,
COALESCE(banned_by_admin_name, ''),
revoked_by_admin_id,
COALESCE(revoked_by_admin_name, ''),
COALESCE(revoke_reason, ''),
start_at,
end_at,
revoked_at,
created_at,
updated_at`

func (r *Repository) CreateUserAccountBan(ctx context.Context, appID int64, userID int64, input userdomain.AccountBanCreateInput) (*userdomain.AccountBan, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	user, err := scanUser(tx.QueryRow(ctx, `SELECT id, appid, account, COALESCE(password_hash, ''), integral, experience, enabled, disabled_end_time, vip_expire_at, created_at, updated_at FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`, userID, appID))
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, nil
	}

	if err := revokeExistingActiveUserBanTx(ctx, tx, appID, userID, input.Operator, "superseded by newer ban"); err != nil {
		return nil, err
	}

	startAt := time.Now().UTC()
	if input.StartAt != nil {
		startAt = input.StartAt.UTC()
	}
	var endAt any
	if input.EndAt != nil {
		value := input.EndAt.UTC()
		endAt = value
	}
	evidenceJSON, _ := json.Marshal(cloneMap(input.Evidence))
	ban, err := scanAccountBan(tx.QueryRow(ctx, `INSERT INTO user_account_bans (
    appid, user_id, ban_type, ban_scope, status, reason, evidence,
    banned_by_admin_id, banned_by_admin_name, start_at, end_at, created_at, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW(), NOW())
RETURNING `+userAccountBanColumns, appID, userID, input.BanType, input.BanScope, userdomain.AccountBanStatusActive, strings.TrimSpace(input.Reason), evidenceJSON, nullableInt64(input.Operator.AdminID), strings.TrimSpace(input.Operator.AdminName), startAt, endAt))
	if err != nil {
		return nil, err
	}

	if err := applyUserAccountBanProjectionTx(ctx, tx, userID, ban); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return ban, nil
}

func (r *Repository) GetActiveUserAccountBan(ctx context.Context, appID int64, userID int64) (*userdomain.AccountBan, error) {
	return scanAccountBan(r.pool.QueryRow(ctx, `SELECT `+userAccountBanColumns+`
FROM user_account_bans
WHERE appid = $1 AND user_id = $2 AND status = 'active' AND start_at <= NOW() AND (end_at IS NULL OR end_at > NOW())
ORDER BY created_at DESC, id DESC
LIMIT 1`, appID, userID))
}

func (r *Repository) RefreshUserAccountBanState(ctx context.Context, appID int64, userID int64) (*userdomain.AccountBan, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	var lockedUserID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`, userID, appID).Scan(&lockedUserID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	tag, err := tx.Exec(ctx, `UPDATE user_account_bans
SET status = 'expired', updated_at = NOW()
WHERE appid = $1 AND user_id = $2 AND status = 'active' AND end_at IS NOT NULL AND end_at <= NOW()`, appID, userID)
	if err != nil {
		return nil, err
	}

	active, err := getActiveUserAccountBanTx(ctx, tx, appID, userID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() > 0 || active != nil {
		if err := applyUserAccountBanProjectionTx(ctx, tx, userID, active); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return active, nil
}

func (r *Repository) UpdateActiveUserAccountBanReason(ctx context.Context, appID int64, userID int64, reason string) (*userdomain.AccountBan, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	var lockedUserID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`, userID, appID).Scan(&lockedUserID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	tag, err := tx.Exec(ctx, `UPDATE user_account_bans
SET reason = $3, updated_at = NOW()
WHERE appid = $1 AND user_id = $2 AND status = 'active' AND start_at <= NOW() AND (end_at IS NULL OR end_at > NOW())`, appID, userID, strings.TrimSpace(reason))
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		tx = nil
		return nil, nil
	}

	active, err := getActiveUserAccountBanTx(ctx, tx, appID, userID)
	if err != nil {
		return nil, err
	}
	if err := applyUserAccountBanProjectionTx(ctx, tx, userID, active); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return active, nil
}

func (r *Repository) RevokeUserAccountBan(ctx context.Context, appID int64, userID int64, banID int64, input userdomain.AccountBanRevokeInput) (*userdomain.AccountBan, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	var lockedUserID int64
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`, userID, appID).Scan(&lockedUserID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	current, err := scanAccountBan(tx.QueryRow(ctx, `SELECT `+userAccountBanColumns+` FROM user_account_bans WHERE id = $1 AND appid = $2 AND user_id = $3 FOR UPDATE`, banID, appID, userID))
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, nil
	}

	if current.Status == userdomain.AccountBanStatusActive {
		if _, err := tx.Exec(ctx, `UPDATE user_account_bans
SET status = 'revoked',
    revoked_at = NOW(),
    revoked_by_admin_id = $4,
    revoked_by_admin_name = $5,
    revoke_reason = $6,
    updated_at = NOW()
WHERE id = $1 AND appid = $2 AND user_id = $3`, banID, appID, userID, nullableInt64(input.Operator.AdminID), strings.TrimSpace(input.Operator.AdminName), strings.TrimSpace(input.Reason)); err != nil {
			return nil, err
		}
	}

	active, err := getActiveUserAccountBanTx(ctx, tx, appID, userID)
	if err != nil {
		return nil, err
	}
	if err := applyUserAccountBanProjectionTx(ctx, tx, userID, active); err != nil {
		return nil, err
	}

	updated, err := scanAccountBan(tx.QueryRow(ctx, `SELECT `+userAccountBanColumns+` FROM user_account_bans WHERE id = $1`, banID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return updated, nil
}

func (r *Repository) ListUserAccountBans(ctx context.Context, appID int64, userID int64, query userdomain.AccountBanQuery) ([]userdomain.AccountBan, int64, error) {
	args := []any{appID, userID}
	where := `WHERE appid = $1 AND user_id = $2`
	if status := strings.TrimSpace(query.Status); status != "" {
		args = append(args, status)
		where += fmt.Sprintf(" AND status = $%d", len(args))
	}

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_account_bans `+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := (page - 1) * limit

	args = append(args, limit, offset)
	rows, err := r.pool.Query(ctx, `SELECT `+userAccountBanColumns+`
FROM user_account_bans
`+where+`
ORDER BY created_at DESC, id DESC
LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]userdomain.AccountBan, 0, limit)
	for rows.Next() {
		item, err := scanAccountBan(rows)
		if err != nil {
			return nil, 0, err
		}
		if item != nil {
			items = append(items, *item)
		}
	}
	return items, total, rows.Err()
}

func (r *Repository) ExpireOverdueUserAccountBans(ctx context.Context, limit int) (int64, error) {
	if limit <= 0 {
		limit = 200
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	rows, err := tx.Query(ctx, `WITH targets AS (
    SELECT id, appid, user_id
    FROM user_account_bans
    WHERE status = 'active' AND end_at IS NOT NULL AND end_at <= NOW()
    ORDER BY end_at ASC, id ASC
    LIMIT $1
    FOR UPDATE SKIP LOCKED
),
updated AS (
    UPDATE user_account_bans b
    SET status = 'expired', updated_at = NOW()
    FROM targets t
    WHERE b.id = t.id
    RETURNING b.id, b.appid, b.user_id
)
SELECT id, appid, user_id FROM updated`, limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	type affectedUser struct {
		AppID  int64
		UserID int64
	}
	affected := make(map[string]affectedUser)
	var expired int64
	for rows.Next() {
		var id int64
		var appID int64
		var userID int64
		if err := rows.Scan(&id, &appID, &userID); err != nil {
			return 0, err
		}
		expired++
		affected[fmt.Sprintf("%d:%d", appID, userID)] = affectedUser{AppID: appID, UserID: userID}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}

	for _, item := range affected {
		active, err := getActiveUserAccountBanTx(ctx, tx, item.AppID, item.UserID)
		if err != nil {
			return 0, err
		}
		if err := applyUserAccountBanProjectionTx(ctx, tx, item.UserID, active); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	tx = nil
	return expired, nil
}

func getActiveUserAccountBanTx(ctx context.Context, tx pgx.Tx, appID int64, userID int64) (*userdomain.AccountBan, error) {
	return scanAccountBan(tx.QueryRow(ctx, `SELECT `+userAccountBanColumns+`
FROM user_account_bans
WHERE appid = $1 AND user_id = $2 AND status = 'active' AND start_at <= NOW() AND (end_at IS NULL OR end_at > NOW())
ORDER BY created_at DESC, id DESC
LIMIT 1`, appID, userID))
}

func revokeExistingActiveUserBanTx(ctx context.Context, tx pgx.Tx, appID int64, userID int64, operator userdomain.BanOperator, reason string) error {
	_, err := tx.Exec(ctx, `UPDATE user_account_bans
SET status = 'revoked',
    revoked_at = NOW(),
    revoked_by_admin_id = COALESCE($3, revoked_by_admin_id),
    revoked_by_admin_name = CASE WHEN $4 = '' THEN revoked_by_admin_name ELSE $4 END,
    revoke_reason = CASE WHEN COALESCE(revoke_reason, '') = '' THEN $5 ELSE revoke_reason END,
    updated_at = NOW()
WHERE appid = $1 AND user_id = $2 AND status = 'active'`, appID, userID, nullableInt64(operator.AdminID), strings.TrimSpace(operator.AdminName), strings.TrimSpace(reason))
	return err
}

func applyUserAccountBanProjectionTx(ctx context.Context, tx pgx.Tx, userID int64, ban *userdomain.AccountBan) error {
	enabled := true
	var disabledEndTime any
	reason := ""
	if ban != nil {
		reason = strings.TrimSpace(ban.Reason)
		if ban.BanType == userdomain.AccountBanTypePermanent {
			enabled = false
		} else if ban.EndAt != nil {
			value := ban.EndAt.UTC()
			disabledEndTime = value
		}
	}

	if _, err := tx.Exec(ctx, `UPDATE users SET enabled = $2, disabled_end_time = $3, updated_at = NOW() WHERE id = $1`, userID, enabled, disabledEndTime); err != nil {
		return err
	}

	extra := map[string]any{"disabled_reason": nil}
	if reason != "" {
		extra["disabled_reason"] = reason
	}
	extraJSON, _ := json.Marshal(extra)
	if _, err := tx.Exec(ctx, `INSERT INTO user_profiles (user_id, extra, updated_at)
VALUES ($1, $2, NOW())
ON CONFLICT (user_id) DO UPDATE SET
    extra = COALESCE(user_profiles.extra, '{}'::jsonb) || EXCLUDED.extra,
    updated_at = NOW()`, userID, extraJSON); err != nil {
		return err
	}
	return nil
}

func scanAccountBan(row interface{ Scan(dest ...any) error }) (*userdomain.AccountBan, error) {
	var item userdomain.AccountBan
	var evidenceRaw []byte
	var bannedByAdminID sql.NullInt64
	var revokedByAdminID sql.NullInt64
	var endAt sql.NullTime
	var revokedAt sql.NullTime
	if err := row.Scan(
		&item.ID,
		&item.AppID,
		&item.UserID,
		&item.BanType,
		&item.BanScope,
		&item.Status,
		&item.Reason,
		&evidenceRaw,
		&bannedByAdminID,
		&item.BannedByAdminName,
		&revokedByAdminID,
		&item.RevokedByAdminName,
		&item.RevokeReason,
		&item.StartAt,
		&endAt,
		&revokedAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	item.Evidence = map[string]any{}
	if len(evidenceRaw) > 0 {
		_ = json.Unmarshal(evidenceRaw, &item.Evidence)
	}
	if bannedByAdminID.Valid {
		value := bannedByAdminID.Int64
		item.BannedByAdminID = &value
	}
	if revokedByAdminID.Valid {
		value := revokedByAdminID.Int64
		item.RevokedByAdminID = &value
	}
	if endAt.Valid {
		value := endAt.Time.UTC()
		item.EndAt = &value
	}
	if revokedAt.Valid {
		value := revokedAt.Time.UTC()
		item.RevokedAt = &value
	}
	item.StartAt = item.StartAt.UTC()
	item.CreatedAt = item.CreatedAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()
	return &item, nil
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	output := make(map[string]any, len(input))
	for k, v := range input {
		output[k] = v
	}
	return output
}
