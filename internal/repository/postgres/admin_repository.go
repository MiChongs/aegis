package postgres

import (
	"context"
	"encoding/json"
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
	query := `SELECT id, account, display_name, email, avatar, phone, birthday, bio, COALESCE(contacts,'[]'::jsonb), status, COALESCE(auth_source,'password'), is_super_admin, last_login_at, created_at, updated_at, password_hash
FROM admin_accounts
WHERE account = $1
LIMIT 1`
	return scanAdminAuthRecord(r.pool.QueryRow(ctx, query, strings.TrimSpace(account)))
}

func (r *Repository) GetAdminAuthByID(ctx context.Context, adminID int64) (*admindomain.AuthRecord, error) {
	query := `SELECT id, account, display_name, email, avatar, phone, birthday, bio, COALESCE(contacts,'[]'::jsonb), status, COALESCE(auth_source,'password'), is_super_admin, last_login_at, created_at, updated_at, password_hash
FROM admin_accounts
WHERE id = $1
LIMIT 1`
	return scanAdminAuthRecord(r.pool.QueryRow(ctx, query, adminID))
}

func (r *Repository) GetAdminAccessByID(ctx context.Context, adminID int64) (*admindomain.Profile, error) {
	query := `SELECT id, account, display_name, email, avatar, phone, birthday, bio, COALESCE(contacts,'[]'::jsonb), status, COALESCE(auth_source,'password'), is_super_admin, last_login_at, created_at, updated_at
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
RETURNING id, account, display_name, email, avatar, phone, birthday, bio, COALESCE(contacts,'[]'::jsonb), status, COALESCE(auth_source,'password'), is_super_admin, last_login_at, created_at, updated_at`
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

func (r *Repository) UpdateAdminProfile(ctx context.Context, adminID int64, input admindomain.ProfileUpdate) (*admindomain.Profile, error) {
	contactsJSON, _ := json.Marshal(input.Contacts)
	// birthday: 空串 → nil，否则解析日期
	var birthday any
	if b := strings.TrimSpace(input.Birthday); b != "" {
		if t, err := time.Parse("2006-01-02", b); err == nil {
			birthday = t
		}
	}
	tag, err := r.pool.Exec(ctx, `UPDATE admin_accounts
SET display_name = COALESCE(NULLIF($2, ''), display_name),
    email       = COALESCE(NULLIF($3, ''), email),
    avatar      = COALESCE(NULLIF($4, ''), avatar),
    phone       = COALESCE(NULLIF($5, ''), phone),
    birthday    = COALESCE($6, birthday),
    bio         = COALESCE(NULLIF($7, ''), bio),
    contacts    = CASE WHEN $8::jsonb IS NULL THEN contacts ELSE $8 END,
    updated_at  = NOW()
WHERE id = $1`,
		adminID,
		strings.TrimSpace(input.DisplayName),
		strings.TrimSpace(input.Email),
		strings.TrimSpace(input.Avatar),
		strings.TrimSpace(input.Phone),
		birthday,
		strings.TrimSpace(input.Bio),
		contactsJSON,
	)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, apperrors.New(40450, http.StatusNotFound, "管理员不存在")
	}
	return r.GetAdminAccessByID(ctx, adminID)
}

func (r *Repository) CountAdminAccounts(ctx context.Context) (int64, error) {
	var count int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_accounts`).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// CountActiveSuperAdmins 统计状态为 active 的超级管理员数量
func (r *Repository) CountActiveSuperAdmins(ctx context.Context) (int64, error) {
	var count int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_accounts WHERE is_super_admin = TRUE AND status = 'active'`).Scan(&count); err != nil {
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
	// 超级管理员已存在：仅确保 is_super_admin 标志为 TRUE，不覆盖密码、显示名、邮箱等已有数据
	if !existing.Account.IsSuperAdmin {
		if _, err := r.pool.Exec(ctx,
			`UPDATE admin_accounts SET is_super_admin = TRUE, updated_at = NOW() WHERE id = $1`,
			existing.Account.ID); err != nil {
			return nil, err
		}
	}
	return r.GetAdminAccessByID(ctx, existing.Account.ID)
}

func (r *Repository) ListAdminAccounts(ctx context.Context) ([]admindomain.Profile, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, account, display_name, email, avatar, phone, birthday, bio, COALESCE(contacts,'[]'::jsonb), status, COALESCE(auth_source,'password'), is_super_admin, last_login_at, created_at, updated_at FROM admin_accounts ORDER BY id ASC`)
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

// AddAdminAssignment 为管理员追加单个角色分配（不删除已有角色）
func (r *Repository) AddAdminAssignment(ctx context.Context, adminID int64, roleKey string, appID *int64) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO admin_assignments (admin_id, role_key, appid, created_at, updated_at)
		 VALUES ($1, $2, $3, NOW(), NOW())
		 ON CONFLICT (admin_id, role_key, COALESCE(appid, 0)) DO NOTHING`,
		adminID, roleKey, nullableInt64Value(appID))
	return err
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
	var contactsRaw []byte
	if err := row.Scan(
		&item.ID,
		&item.Account,
		&item.DisplayName,
		&item.Email,
		&item.Avatar,
		&item.Phone,
		&item.Birthday,
		&item.Bio,
		&contactsRaw,
		&item.Status,
		&item.AuthSource,
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
	_ = json.Unmarshal(contactsRaw, &item.Contacts)
	return &item, nil
}

func scanAdminAuthRecord(row pgx.Row) (*admindomain.AuthRecord, error) {
	var item admindomain.AuthRecord
	var contactsRaw []byte
	if err := row.Scan(
		&item.Account.ID,
		&item.Account.Account,
		&item.Account.DisplayName,
		&item.Account.Email,
		&item.Account.Avatar,
		&item.Account.Phone,
		&item.Account.Birthday,
		&item.Account.Bio,
		&contactsRaw,
		&item.Account.Status,
		&item.Account.AuthSource,
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
	_ = json.Unmarshal(contactsRaw, &item.Account.Contacts)
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

// CreateExternalAdminAccount 为外部认证用户创建本地管理员（无密码，非超管）
func (r *Repository) CreateExternalAdminAccount(ctx context.Context, account, displayName, email, phone, authSource string) (*admindomain.Profile, error) {
	query := `INSERT INTO admin_accounts (account, password_hash, display_name, email, phone, auth_source, status, is_super_admin, created_at, updated_at)
VALUES ($1, '', $2, $3, $4, $5, 'active', false, NOW(), NOW())
RETURNING id, account, display_name, email, avatar, phone, birthday, bio, COALESCE(contacts,'[]'::jsonb), status, COALESCE(auth_source,'password'), is_super_admin, last_login_at, created_at, updated_at`
	acct, err := scanAdminAccount(r.pool.QueryRow(ctx, query,
		strings.TrimSpace(account),
		strings.TrimSpace(displayName),
		strings.TrimSpace(email),
		strings.TrimSpace(phone),
		authSource,
	))
	if err != nil {
		if isDuplicateKeyError(err) {
			// 并发创建，回退到查询
			return r.GetAdminAccessByID(ctx, 0)
		}
		return nil, err
	}
	return &admindomain.Profile{Account: *acct}, nil
}

// UpdateAdminExternalSync 从外部认证源同步管理员资料（仅更新本地为空的字段 + 更新 auth_source）
func (r *Repository) UpdateAdminExternalSync(ctx context.Context, adminID int64, displayName, email, phone, authSource string) error {
	_, err := r.pool.Exec(ctx, `UPDATE admin_accounts SET
		display_name = CASE WHEN COALESCE(display_name,'') = '' AND $2 != '' THEN $2 ELSE display_name END,
		email = CASE WHEN COALESCE(email,'') = '' AND $3 != '' THEN $3 ELSE email END,
		phone = CASE WHEN COALESCE(phone,'') = '' AND $4 != '' THEN $4 ELSE phone END,
		auth_source = $5,
		updated_at = NOW()
	WHERE id = $1`, adminID, strings.TrimSpace(displayName), strings.TrimSpace(email), strings.TrimSpace(phone), authSource)
	return err
}
