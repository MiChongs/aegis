package postgres

import (
	"context"
	"net/http"

	admindomain "aegis/internal/domain/admin"
	apperrors "aegis/pkg/errors"

	"github.com/jackc/pgx/v5"
)

func (r *Repository) ListCustomRoles(ctx context.Context) ([]admindomain.CustomRole, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, role_key, name, description, level, scope, COALESCE(base_role,''), COALESCE(status,'active'), COALESCE(sort_order,0), COALESCE(data_scope,'assigned'), COALESCE(data_app_ids,'{}'), created_by, updated_by, created_at, updated_at FROM admin_roles ORDER BY sort_order ASC, level DESC, role_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []admindomain.CustomRole
	for rows.Next() {
		var cr admindomain.CustomRole
		if err := rows.Scan(&cr.ID, &cr.RoleKey, &cr.Name, &cr.Description, &cr.Level, &cr.Scope, &cr.BaseRole, &cr.Status, &cr.SortOrder, &cr.DataScope, &cr.DataAppIDs, &cr.CreatedBy, &cr.UpdatedBy, &cr.CreatedAt, &cr.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, cr)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 批量加载权限
	for i := range items {
		allow, deny, err := r.listCustomRoleEffects(ctx, items[i].RoleKey)
		if err != nil {
			return nil, err
		}
		items[i].Permissions, items[i].Deny = allow, deny
	}
	return items, nil
}

func (r *Repository) GetCustomRole(ctx context.Context, roleKey string) (*admindomain.CustomRole, error) {
	var cr admindomain.CustomRole
	err := r.pool.QueryRow(ctx, `SELECT id, role_key, name, description, level, scope, COALESCE(base_role,''), COALESCE(status,'active'), COALESCE(sort_order,0), COALESCE(data_scope,'assigned'), COALESCE(data_app_ids,'{}'), created_by, updated_by, created_at, updated_at FROM admin_roles WHERE role_key = $1`, roleKey).
		Scan(&cr.ID, &cr.RoleKey, &cr.Name, &cr.Description, &cr.Level, &cr.Scope, &cr.BaseRole, &cr.Status, &cr.SortOrder, &cr.DataScope, &cr.DataAppIDs, &cr.CreatedBy, &cr.UpdatedBy, &cr.CreatedAt, &cr.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	allow, deny, err := r.listCustomRoleEffects(ctx, roleKey)
	if err != nil {
		return nil, err
	}
	cr.Permissions, cr.Deny = allow, deny
	return &cr, nil
}

func (r *Repository) CreateCustomRole(ctx context.Context, input admindomain.CreateCustomRoleInput, createdBy int64) (*admindomain.CustomRole, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var cr admindomain.CustomRole
	err = tx.QueryRow(ctx, `INSERT INTO admin_roles (role_key, name, description, level, scope, base_role, status, sort_order, data_scope, data_app_ids, created_by, updated_by)
VALUES ($1,$2,$3,$4,$5,NULLIF($6,''),$7,$8,$9,$10,$11,$11) RETURNING id, role_key, name, description, level, scope, COALESCE(base_role,''), COALESCE(status,'active'), COALESCE(sort_order,0), COALESCE(data_scope,'assigned'), COALESCE(data_app_ids,'{}'), created_by, updated_by, created_at, updated_at`,
		input.RoleKey, input.Name, input.Description, input.Level, input.Scope, input.BaseRole,
		input.Status, input.SortOrder, input.DataScope, int64Slice(input.DataAppIDs), createdBy,
	).Scan(&cr.ID, &cr.RoleKey, &cr.Name, &cr.Description, &cr.Level, &cr.Scope, &cr.BaseRole, &cr.Status, &cr.SortOrder, &cr.DataScope, &cr.DataAppIDs, &cr.CreatedBy, &cr.UpdatedBy, &cr.CreatedAt, &cr.UpdatedAt)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, apperrors.New(40960, http.StatusConflict, "角色标识已存在")
		}
		return nil, err
	}

	if err := r.replaceRolePermissions(ctx, tx, input.RoleKey, input.Permissions, input.Deny); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	cr.Permissions, cr.Deny = input.Permissions, input.Deny
	return &cr, nil
}

func (r *Repository) UpdateCustomRole(ctx context.Context, roleKey string, input admindomain.UpdateCustomRoleInput, updatedBy *int64) (*admindomain.CustomRole, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var cr admindomain.CustomRole
	baseRole := ""
	keepBase := input.BaseRole == nil
	if input.BaseRole != nil {
		baseRole = *input.BaseRole
	}
	err = tx.QueryRow(ctx, `UPDATE admin_roles
SET name=$2, description=$3, level=$4, status=$5, sort_order=$6, data_scope=$7, data_app_ids=$8,
    base_role = CASE WHEN $9 THEN base_role ELSE NULLIF($10,'') END,
    updated_by=$11, updated_at=NOW()
WHERE role_key=$1 RETURNING id, role_key, name, description, level, scope, COALESCE(base_role,''), COALESCE(status,'active'), COALESCE(sort_order,0), COALESCE(data_scope,'assigned'), COALESCE(data_app_ids,'{}'), created_by, updated_by, created_at, updated_at`,
		roleKey, input.Name, input.Description, input.Level, input.Status, input.SortOrder,
		input.DataScope, int64Slice(input.DataAppIDs), keepBase, baseRole, updatedBy,
	).Scan(&cr.ID, &cr.RoleKey, &cr.Name, &cr.Description, &cr.Level, &cr.Scope, &cr.BaseRole, &cr.Status, &cr.SortOrder, &cr.DataScope, &cr.DataAppIDs, &cr.CreatedBy, &cr.UpdatedBy, &cr.CreatedAt, &cr.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apperrors.New(40461, http.StatusNotFound, "自定义角色不存在")
		}
		return nil, err
	}

	if err := r.replaceRolePermissions(ctx, tx, roleKey, input.Permissions, input.Deny); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	cr.Permissions, cr.Deny = input.Permissions, input.Deny
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

// SetCustomRoleStatus 启用 / 停用自定义角色。
func (r *Repository) SetCustomRoleStatus(ctx context.Context, roleKey, status string, updatedBy *int64) error {
	tag, err := r.pool.Exec(ctx, `UPDATE admin_roles SET status = $2, updated_by = $3, updated_at = NOW() WHERE role_key = $1`, roleKey, status, updatedBy)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperrors.New(40461, http.StatusNotFound, "自定义角色不存在")
	}
	return nil
}

// listCustomRoleEffects 角色定义里的允许集与禁止集。
func (r *Repository) listCustomRoleEffects(ctx context.Context, roleKey string) ([]string, []string, error) {
	rows, err := r.pool.Query(ctx, `SELECT permission, COALESCE(effect, 'allow') FROM admin_role_permissions WHERE role_key = $1 ORDER BY permission`, roleKey)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	allow, deny := []string{}, []string{}
	for rows.Next() {
		var permission, effect string
		if err := rows.Scan(&permission, &effect); err != nil {
			return nil, nil, err
		}
		if effect == "deny" {
			deny = append(deny, permission)
		} else {
			allow = append(allow, permission)
		}
	}
	return allow, deny, rows.Err()
}

func (r *Repository) ListCustomRolePermissions(ctx context.Context, roleKey string) ([]string, error) {
	rows, err := r.pool.Query(ctx, `SELECT permission FROM admin_role_permissions WHERE role_key = $1 AND COALESCE(effect, 'allow') = 'allow' ORDER BY permission`, roleKey)
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

func (r *Repository) replaceRolePermissions(ctx context.Context, tx pgx.Tx, roleKey string, allow, deny []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM admin_role_permissions WHERE role_key = $1`, roleKey); err != nil {
		return err
	}
	denied := make(map[string]bool, len(deny))
	for _, perm := range deny {
		denied[perm] = true
		if _, err := tx.Exec(ctx, `INSERT INTO admin_role_permissions (role_key, permission, effect) VALUES ($1, $2, 'deny') ON CONFLICT (role_key, permission) DO UPDATE SET effect = 'deny'`, roleKey, perm); err != nil {
			return err
		}
	}
	for _, perm := range allow {
		if denied[perm] {
			continue // 同一项既勾允许又勾禁止时以禁止为准
		}
		if _, err := tx.Exec(ctx, `INSERT INTO admin_role_permissions (role_key, permission, effect) VALUES ($1, $2, 'allow') ON CONFLICT (role_key, permission) DO NOTHING`, roleKey, perm); err != nil {
			return err
		}
	}
	return nil
}

func int64Slice(items []int64) []int64 {
	if items == nil {
		return []int64{}
	}
	return items
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
