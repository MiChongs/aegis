package postgres

import (
	"context"
	"net/http"

	admindomain "aegis/internal/domain/admin"
	apperrors "aegis/pkg/errors"

	"github.com/jackc/pgx/v5"
)

func (r *Repository) ListCustomRoles(ctx context.Context) ([]admindomain.CustomRole, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, role_key, name, description, level, scope, COALESCE(base_role,''), created_by, created_at, updated_at FROM admin_roles ORDER BY level DESC, role_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []admindomain.CustomRole
	for rows.Next() {
		var cr admindomain.CustomRole
		if err := rows.Scan(&cr.ID, &cr.RoleKey, &cr.Name, &cr.Description, &cr.Level, &cr.Scope, &cr.BaseRole, &cr.CreatedBy, &cr.CreatedAt, &cr.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, cr)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 批量加载权限
	for i := range items {
		perms, err := r.ListCustomRolePermissions(ctx, items[i].RoleKey)
		if err != nil {
			return nil, err
		}
		items[i].Permissions = perms
	}
	return items, nil
}

func (r *Repository) GetCustomRole(ctx context.Context, roleKey string) (*admindomain.CustomRole, error) {
	var cr admindomain.CustomRole
	err := r.pool.QueryRow(ctx, `SELECT id, role_key, name, description, level, scope, COALESCE(base_role,''), created_by, created_at, updated_at FROM admin_roles WHERE role_key = $1`, roleKey).
		Scan(&cr.ID, &cr.RoleKey, &cr.Name, &cr.Description, &cr.Level, &cr.Scope, &cr.BaseRole, &cr.CreatedBy, &cr.CreatedAt, &cr.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	perms, err := r.ListCustomRolePermissions(ctx, roleKey)
	if err != nil {
		return nil, err
	}
	cr.Permissions = perms
	return &cr, nil
}

func (r *Repository) CreateCustomRole(ctx context.Context, input admindomain.CreateCustomRoleInput, createdBy int64) (*admindomain.CustomRole, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var cr admindomain.CustomRole
	err = tx.QueryRow(ctx, `INSERT INTO admin_roles (role_key, name, description, level, scope, base_role, created_by) VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),$7) RETURNING id, role_key, name, description, level, scope, COALESCE(base_role,''), created_by, created_at, updated_at`,
		input.RoleKey, input.Name, input.Description, input.Level, input.Scope, input.BaseRole, createdBy,
	).Scan(&cr.ID, &cr.RoleKey, &cr.Name, &cr.Description, &cr.Level, &cr.Scope, &cr.BaseRole, &cr.CreatedBy, &cr.CreatedAt, &cr.UpdatedAt)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, apperrors.New(40960, http.StatusConflict, "角色标识已存在")
		}
		return nil, err
	}

	if err := r.replaceRolePermissions(ctx, tx, input.RoleKey, input.Permissions); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	cr.Permissions = input.Permissions
	return &cr, nil
}

func (r *Repository) UpdateCustomRole(ctx context.Context, roleKey string, input admindomain.UpdateCustomRoleInput) (*admindomain.CustomRole, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var cr admindomain.CustomRole
	err = tx.QueryRow(ctx, `UPDATE admin_roles SET name=$2, description=$3, level=$4, updated_at=NOW() WHERE role_key=$1 RETURNING id, role_key, name, description, level, scope, COALESCE(base_role,''), created_by, created_at, updated_at`,
		roleKey, input.Name, input.Description, input.Level,
	).Scan(&cr.ID, &cr.RoleKey, &cr.Name, &cr.Description, &cr.Level, &cr.Scope, &cr.BaseRole, &cr.CreatedBy, &cr.CreatedAt, &cr.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apperrors.New(40461, http.StatusNotFound, "自定义角色不存在")
		}
		return nil, err
	}

	if err := r.replaceRolePermissions(ctx, tx, roleKey, input.Permissions); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	cr.Permissions = input.Permissions
	return &cr, nil
}

func (r *Repository) DeleteCustomRole(ctx context.Context, roleKey string) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM admin_roles WHERE role_key = $1`, roleKey)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperrors.New(40461, http.StatusNotFound, "自定义角色不存在")
	}
	return nil
}

func (r *Repository) ListCustomRolePermissions(ctx context.Context, roleKey string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT permission FROM admin_role_permissions WHERE role_key = $1 ORDER BY permission`, roleKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var perms []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		perms = append(perms, p)
	}
	return perms, rows.Err()
}

func (r *Repository) replaceRolePermissions(ctx context.Context, tx pgx.Tx, roleKey string, permissions []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM admin_role_permissions WHERE role_key = $1`, roleKey); err != nil {
		return err
	}
	for _, perm := range permissions {
		if _, err := tx.Exec(ctx, `INSERT INTO admin_role_permissions (role_key, permission) VALUES ($1, $2)`, roleKey, perm); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) CountAdminsByRoleKey(ctx context.Context, roleKey string) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_assignments WHERE role_key = $1`, roleKey).Scan(&count)
	return count, err
}

func (r *Repository) ListAdminsByRoleKey(ctx context.Context, roleKey string) ([]admindomain.ImpactAdmin, error) {
	rows, err := r.pool.Query(ctx, `SELECT a.id, a.account, a.display_name FROM admin_assignments aa JOIN admin_accounts a ON a.id = aa.admin_id WHERE aa.role_key = $1 ORDER BY a.account`, roleKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []admindomain.ImpactAdmin
	for rows.Next() {
		var item admindomain.ImpactAdmin
		if err := rows.Scan(&item.AdminID, &item.Account, &item.DisplayName); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
