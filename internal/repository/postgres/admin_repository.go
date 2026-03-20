package postgres

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	admindomain "aegis/internal/domain/admin"
	apperrors "aegis/pkg/errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (r *Repository) GetAdminAuthByAccount(ctx context.Context, account string) (*admindomain.AuthRecord, error) {
	query := `SELECT id, account, display_name, email, status, is_super_admin, last_login_at, created_at, updated_at, password_hash
FROM admin_accounts
WHERE account = $1
LIMIT 1`
	return scanAdminAuthRecord(r.pool.QueryRow(ctx, query, strings.TrimSpace(account)))
}

func (r *Repository) GetAdminAccessByID(ctx context.Context, adminID int64) (*admindomain.Profile, error) {
	query := `SELECT id, account, display_name, email, status, is_super_admin, last_login_at, created_at, updated_at
FROM admin_accounts
WHERE id = $1
LIMIT 1`
	account, err := scanAdminAccount(r.pool.QueryRow(ctx, query, adminID))
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, nil
	}
	assignments, err := r.ListAdminAssignments(ctx, adminID)
	if err != nil {
		return nil, err
	}
	return &admindomain.Profile{Account: *account, Assignments: assignments}, nil
}

func (r *Repository) CreateAdminAccount(ctx context.Context, input admindomain.CreateInput, passwordHash string) (*admindomain.Profile, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	query := `INSERT INTO admin_accounts (account, password_hash, display_name, email, status, is_super_admin, created_at, updated_at)
VALUES ($1, $2, $3, $4, 'active', $5, NOW(), NOW())
RETURNING id, account, display_name, email, status, is_super_admin, last_login_at, created_at, updated_at`
	account, err := scanAdminAccount(tx.QueryRow(ctx, query, strings.TrimSpace(input.Account), passwordHash, strings.TrimSpace(input.DisplayName), strings.TrimSpace(input.Email), input.IsSuperAdmin))
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, apperrors.New(40950, http.StatusConflict, "管理员账号已存在")
		}
		return nil, err
	}
	if err := r.replaceAdminAssignments(ctx, tx, account.ID, input.Assignments); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetAdminAccessByID(ctx, account.ID)
}

func (r *Repository) UpdateAdminStatus(ctx context.Context, adminID int64, status string) error {
	status = strings.TrimSpace(strings.ToLower(status))
	if status == "" {
		status = "active"
	}
	tag, err := r.pool.Exec(ctx, `UPDATE admin_accounts SET status = $2, updated_at = NOW() WHERE id = $1`, adminID, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperrors.New(40450, http.StatusNotFound, "管理员不存在")
	}
	return nil
}

func (r *Repository) UpdateAdminAccess(ctx context.Context, adminID int64, input admindomain.UpdateAccessInput) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `UPDATE admin_accounts SET is_super_admin = $2, updated_at = NOW() WHERE id = $1`, adminID, input.IsSuperAdmin)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperrors.New(40450, http.StatusNotFound, "管理员不存在")
	}
	if err := r.replaceAdminAssignments(ctx, tx, adminID, input.Assignments); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *Repository) UpdateAdminLastLogin(ctx context.Context, adminID int64, at time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE admin_accounts SET last_login_at = $2, updated_at = NOW() WHERE id = $1`, adminID, at)
	return err
}

func (r *Repository) CountAdminAccounts(ctx context.Context) (int64, error) {
	var count int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_accounts`).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (r *Repository) UpsertBootstrapAdmin(ctx context.Context, input admindomain.CreateInput, passwordHash string) (*admindomain.Profile, error) {
	existing, err := r.GetAdminAuthByAccount(ctx, input.Account)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return r.CreateAdminAccount(ctx, input, passwordHash)
	}
	update := admindomain.UpdateAccessInput{
		IsSuperAdmin: true,
		Assignments:  nil,
	}
	if err := r.UpdateAdminAccess(ctx, existing.Account.ID, update); err != nil {
		return nil, err
	}
	if _, err := r.pool.Exec(ctx, `UPDATE admin_accounts SET password_hash = $2, display_name = $3, email = $4, status = 'active', is_super_admin = TRUE, updated_at = NOW() WHERE id = $1`,
		existing.Account.ID, passwordHash, strings.TrimSpace(input.DisplayName), strings.TrimSpace(input.Email)); err != nil {
		return nil, err
	}
	return r.GetAdminAccessByID(ctx, existing.Account.ID)
}

func (r *Repository) ListAdminAccounts(ctx context.Context) ([]admindomain.Profile, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, account, display_name, email, status, is_super_admin, last_login_at, created_at, updated_at FROM admin_accounts ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]admindomain.Profile, 0, 8)
	for rows.Next() {
		account, err := scanAdminAccount(rows)
		if err != nil {
			return nil, err
		}
		assignments, err := r.ListAdminAssignments(ctx, account.ID)
		if err != nil {
			return nil, err
		}
		items = append(items, admindomain.Profile{Account: *account, Assignments: assignments})
	}
	return items, rows.Err()
}

func (r *Repository) ListAdminAssignments(ctx context.Context, adminID int64) ([]admindomain.Assignment, error) {
	query := `SELECT aa.role_key, aa.appid, COALESCE(a.name, '')
FROM admin_assignments aa
LEFT JOIN apps a ON a.id = aa.appid
WHERE aa.admin_id = $1
ORDER BY aa.role_key ASC, aa.appid ASC NULLS FIRST`
	rows, err := r.pool.Query(ctx, query, adminID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]admindomain.Assignment, 0, 4)
	for rows.Next() {
		var item admindomain.Assignment
		var appID *int64
		if err := rows.Scan(&item.RoleKey, &appID, &item.AppName); err != nil {
			return nil, err
		}
		item.AppID = appID
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) replaceAdminAssignments(ctx context.Context, tx pgx.Tx, adminID int64, assignments []admindomain.AssignmentMutation) error {
	if _, err := tx.Exec(ctx, `DELETE FROM admin_assignments WHERE admin_id = $1`, adminID); err != nil {
		return err
	}
	for _, item := range assignments {
		roleKey := strings.TrimSpace(item.RoleKey)
		if roleKey == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO admin_assignments (admin_id, role_key, appid, created_at, updated_at) VALUES ($1, $2, $3, NOW(), NOW())`,
			adminID, roleKey, nullableInt64Value(item.AppID)); err != nil {
			return err
		}
	}
	return nil
}

func scanAdminAccount(row pgx.Row) (*admindomain.Account, error) {
	var item admindomain.Account
	if err := row.Scan(
		&item.ID,
		&item.Account,
		&item.DisplayName,
		&item.Email,
		&item.Status,
		&item.IsSuperAdmin,
		&item.LastLoginAt,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func scanAdminAuthRecord(row pgx.Row) (*admindomain.AuthRecord, error) {
	var item admindomain.AuthRecord
	if err := row.Scan(
		&item.Account.ID,
		&item.Account.Account,
		&item.Account.DisplayName,
		&item.Account.Email,
		&item.Account.Status,
		&item.Account.IsSuperAdmin,
		&item.Account.LastLoginAt,
		&item.Account.CreatedAt,
		&item.Account.UpdatedAt,
		&item.PasswordHash,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &item, nil
}

func nullableInt64Value(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func isDuplicateKeyError(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
