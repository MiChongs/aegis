package postgres

import (
	"context"
	"net/http"
	"strings"
	"time"

	admindomain "aegis/internal/domain/admin"
	apperrors "aegis/pkg/errors"
	"github.com/jackc/pgx/v5"
)

// 管理员账号自助：一次性改名与修改密码的持久化。
//
// 账号名一律按 lower() 判重，并且把别人改名前的旧名也算作已占用 ——
// 审计日志里记的是名字快照，旧名一旦被别人拿走，同一个名字就指向了两个人。

// adminAccountNameLockKey 改名与建号共用的事务级咨询锁，把「判重 → 写入」串行化。
// 判重条件里有 previous_account，单靠唯一索引兜不住，所以要锁。
const adminAccountNameLockKey = "aegis.admin_accounts.account"

func (r *Repository) getAdminAuthByPreviousAccount(ctx context.Context, account string) (*admindomain.AuthRecord, error) {
	account = strings.TrimSpace(account)
	if account == "" {
		return nil, nil
	}
	query := `SELECT id, account, display_name, email, avatar, phone, birthday, bio, COALESCE(contacts,'[]'::jsonb), status, COALESCE(auth_source,'password'), is_super_admin, last_login_at, created_at, updated_at, previous_account, account_changed_at, password_changed_at, password_hash
FROM admin_accounts
WHERE lower(previous_account) = lower($1)
ORDER BY id ASC
LIMIT 1`
	return scanAdminAuthRecord(r.pool.QueryRow(ctx, query, account))
}

// AdminAccountNameTaken 判断账号名是否已被占用（不区分大小写，含他人改名前的旧名）。
// excludeID 为当前管理员自己，传 0 表示不排除。
func (r *Repository) AdminAccountNameTaken(ctx context.Context, account string, excludeID int64) (bool, error) {
	return adminAccountNameTaken(ctx, r.pool, account, excludeID)
}

type queryRower interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func adminAccountNameTaken(ctx context.Context, q queryRower, account string, excludeID int64) (bool, error) {
	var taken bool
	err := q.QueryRow(ctx, `SELECT EXISTS (
	SELECT 1 FROM admin_accounts
	WHERE id <> $2
	  AND (lower(account) = lower($1) OR lower(COALESCE(previous_account, '')) = lower($1))
)`, strings.TrimSpace(account), excludeID).Scan(&taken)
	return taken, err
}

// RenameAdminAccount 执行唯一一次改名。
//
// 「只能改一次」「本地账号才能改」「新名未被占用」三个条件都在同一条 UPDATE 的
// WHERE 里，并由咨询锁串行化，不存在先查后写的竞态。
func (r *Repository) RenameAdminAccount(ctx context.Context, adminID int64, newAccount string) (*admindomain.Profile, error) {
	newAccount = strings.TrimSpace(newAccount)
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, adminAccountNameLockKey); err != nil {
		return nil, err
	}

	var current string
	var changedAt *time.Time
	var authSource string
	err = tx.QueryRow(ctx, `SELECT account, account_changed_at, COALESCE(auth_source, 'password')
FROM admin_accounts WHERE id = $1 FOR UPDATE`, adminID).Scan(&current, &changedAt, &authSource)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apperrors.New(40450, http.StatusNotFound, "管理员不存在")
		}
		return nil, err
	}
	if authSource != "password" {
		return nil, ErrAdminExternalAccount
	}
	if changedAt != nil {
		return nil, ErrAdminAccountRenameUsed
	}
	taken, err := adminAccountNameTaken(ctx, tx, newAccount, adminID)
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, ErrAdminAccountTaken
	}

	tag, err := tx.Exec(ctx, `UPDATE admin_accounts
SET previous_account = account,
    account = $2,
    account_changed_at = NOW(),
    updated_at = NOW()
WHERE id = $1 AND account_changed_at IS NULL`, adminID, newAccount)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, ErrAdminAccountTaken
		}
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrAdminAccountRenameUsed
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetAdminAccessByID(ctx, adminID)
}

// UpdateAdminPassword 写入新的密码哈希并记录修改时间。
func (r *Repository) UpdateAdminPassword(ctx context.Context, adminID int64, passwordHash string) (time.Time, error) {
	var changedAt time.Time
	err := r.pool.QueryRow(ctx, `UPDATE admin_accounts
SET password_hash = $2, password_changed_at = NOW(), updated_at = NOW()
WHERE id = $1
RETURNING password_changed_at`, adminID, passwordHash).Scan(&changedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return time.Time{}, apperrors.New(40450, http.StatusNotFound, "管理员不存在")
		}
		return time.Time{}, err
	}
	return changedAt, nil
}

// RevokeAdminSessionsExcept 撤销某管理员除 keepID 之外的全部活跃会话，返回被撤销的会话 ID（即 JWT jti）。
func (r *Repository) RevokeAdminSessionsExcept(ctx context.Context, adminID int64, keepID string, revokedBy int64) ([]string, error) {
	rows, err := r.pool.Query(ctx, `UPDATE admin_sessions
SET is_revoked = TRUE, revoked_by = $3, revoked_at = NOW()
WHERE admin_id = $1 AND id <> $2 AND NOT is_revoked
RETURNING id`, adminID, keepID, revokedBy)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0, 4)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

var (
	// ErrAdminAccountRenameUsed 唯一一次改名机会已经用掉
	ErrAdminAccountRenameUsed = apperrors.New(40951, http.StatusConflict, "用户名仅可修改一次，你已使用过这次机会")
	// ErrAdminAccountTaken 新用户名已被占用
	ErrAdminAccountTaken = apperrors.New(40952, http.StatusConflict, "该用户名已被占用")
	// ErrAdminExternalAccount 外部身份源账号的用户名与密码由身份源管理
	ErrAdminExternalAccount = apperrors.New(40325, http.StatusForbidden, "该账号由外部身份源管理，无法在此修改用户名或密码")
)
