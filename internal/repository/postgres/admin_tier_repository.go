package postgres

import (
	"context"
	"net/http"

	admindomain "aegis/internal/domain/admin"
	apperrors "aegis/pkg/errors"
	"github.com/jackc/pgx/v5"
)

// 管理员分级与角色授权的数据访问（见 service/admin_tier.go）。

// CountActiveSystemAdmins 统计活跃的系统管理员数量。
func (r *Repository) CountActiveSystemAdmins(ctx context.Context) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM admin_accounts WHERE is_system_admin = TRUE AND status = 'active'`).Scan(&count)
	return count, err
}

// EarliestActiveSuperAdminID 最早创建的活跃超级管理员；没有时返回 0。
func (r *Repository) EarliestActiveSuperAdminID(ctx context.Context) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx,
		`SELECT id FROM admin_accounts WHERE is_super_admin = TRUE AND status = 'active' ORDER BY id ASC LIMIT 1`).Scan(&id)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	return id, err
}

// PromoteSystemAdmin 把一个管理员提升为系统管理员（同时置超级管理员，并清掉角色分配：
// 超管不走权限点判定，留着角色分配只会让界面上出现一份没有意义的授权）。
func (r *Repository) PromoteSystemAdmin(ctx context.Context, adminID int64) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx,
		`UPDATE admin_accounts SET is_super_admin = TRUE, is_system_admin = TRUE, status = 'active', updated_at = NOW() WHERE id = $1`,
		adminID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperrors.New(40450, http.StatusNotFound, "管理员不存在")
	}
	if _, err := tx.Exec(ctx, `DELETE FROM admin_assignments WHERE admin_id = $1`, adminID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ListAppIDsCreatedBy 某管理员自己创建的应用（数据权限「本人创建」）。
func (r *Repository) ListAppIDsCreatedBy(ctx context.Context, adminID int64) ([]admindomain.Assignment, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, name FROM apps WHERE created_by = $1 ORDER BY id`, adminID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]admindomain.Assignment, 0, 4)
	for rows.Next() {
		var id int64
		var item admindomain.Assignment
		if err := rows.Scan(&id, &item.AppName); err != nil {
			return nil, err
		}
		item.AppID = &id
		items = append(items, item)
	}
	return items, rows.Err()
}

// AppNamesByIDs 应用名（数据权限「指定应用」展开时补名称）。
func (r *Repository) AppNamesByIDs(ctx context.Context, ids []int64) (map[int64]string, error) {
	result := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT id, name FROM apps WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		result[id] = name
	}
	return result, rows.Err()
}

// RemoveAdminAssignment 移除一条角色分配（appID 为空时只删全局那条）。
func (r *Repository) RemoveAdminAssignment(ctx context.Context, adminID int64, roleKey string, appID *int64) (bool, error) {
	tag, err := r.pool.Exec(ctx,
		`DELETE FROM admin_assignments WHERE admin_id = $1 AND role_key = $2 AND COALESCE(appid, 0) = COALESCE($3::bigint, 0)`,
		adminID, roleKey, nullableInt64Value(appID))
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ListRoleMembers 持有某角色的全部授权（含账号状态与档位）。
func (r *Repository) ListRoleMembers(ctx context.Context, roleKey string) ([]admindomain.RoleMember, error) {
	rows, err := r.pool.Query(ctx, `SELECT a.id, a.account, a.display_name, a.avatar, a.status,
       a.is_super_admin, COALESCE(a.is_system_admin, FALSE), aa.appid, COALESCE(p.name, ''), aa.created_at, a.last_login_at
FROM admin_assignments aa
JOIN admin_accounts a ON a.id = aa.admin_id
LEFT JOIN apps p ON p.id = aa.appid
WHERE aa.role_key = $1
ORDER BY a.account ASC, aa.appid ASC NULLS FIRST`, roleKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]admindomain.RoleMember, 0, 8)
	for rows.Next() {
		var item admindomain.RoleMember
		var isSuper, isSystem bool
		if err := rows.Scan(&item.AdminID, &item.Account, &item.DisplayName, &item.Avatar, &item.Status,
			&isSuper, &isSystem, &item.AppID, &item.AppName, &item.GrantedAt, &item.LastLoginAt); err != nil {
			return nil, err
		}
		item.Tier = admindomain.TierOf(isSuper, isSystem)
		items = append(items, item)
	}
	return items, rows.Err()
}

// CountRoleMembers 各角色的持有人数（按人去重）。
func (r *Repository) CountRoleMembers(ctx context.Context) (map[string]int, error) {
	rows, err := r.pool.Query(ctx, `SELECT role_key, COUNT(DISTINCT admin_id) FROM admin_assignments GROUP BY role_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]int{}
	for rows.Next() {
		var key string
		var count int
		if err := rows.Scan(&key, &count); err != nil {
			return nil, err
		}
		result[key] = count
	}
	return result, rows.Err()
}

// TempPermissionOwner 临时权限归属的管理员；不存在时返回 0。
func (r *Repository) TempPermissionOwner(ctx context.Context, permID int64) (int64, error) {
	var adminID int64
	err := r.pool.QueryRow(ctx, `SELECT admin_id FROM admin_temp_permissions WHERE id = $1`, permID).Scan(&adminID)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	return adminID, err
}

// DeleteAssignmentsByRole 删除某个角色的全部分配（强制删除角色时用）。
func (r *Repository) DeleteAssignmentsByRole(ctx context.Context, roleKey string) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM admin_assignments WHERE role_key = $1`, roleKey)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
