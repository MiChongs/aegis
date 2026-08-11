package postgres

import (
	"context"
	"net/http"

	orgdomain "aegis/internal/domain/organization"
	apperrors "aegis/pkg/errors"

	"github.com/jackc/pgx/v5"
)

// 组织自定义角色。内置角色（owner/admin/…）不落库 —— 它们由代码定义、
// 全组织一致，落库只会带来「某个组织的 admin 权限被人改小了」这种诡异故障。
// 这里存的是组织自己加出来的角色，与内置角色在 Casbin 里共用同一个组织域。

const orgRoleColumns = `r.id, r.uuid::text, o.uuid::text, r.role_key, r.name, r.description,
	r.permissions, r.is_builtin, r.created_at, r.updated_at,
	(SELECT COUNT(*) FROM org_role_members rm WHERE rm.org_role_id = r.id)`

const orgRoleFrom = ` FROM org_roles r JOIN organizations o ON o.id = r.org_id`

func scanOrgRole(row pgx.Row) (*orgdomain.Role, error) {
	var r orgdomain.Role
	err := row.Scan(&r.ID, &r.UUID, &r.OrgUUID, &r.RoleKey, &r.Name, &r.Description,
		&r.Permissions, &r.IsBuiltin, &r.CreatedAt, &r.UpdatedAt, &r.MemberCount)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if r.Permissions == nil {
		r.Permissions = []string{}
	}
	return &r, nil
}

// ResolveOrgRoleID 角色 UUID → 内部主键，强制校验组织归属
func (r *Repository) ResolveOrgRoleID(ctx context.Context, orgID int64, roleUUID string) (int64, error) {
	if !isUUIDLike(roleUUID) {
		return 0, ErrRoleNotFound
	}
	var id int64
	err := r.pool.QueryRow(ctx, `SELECT id FROM org_roles WHERE uuid = $1::uuid AND org_id = $2`,
		roleUUID, orgID).Scan(&id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, ErrRoleNotFound
		}
		return 0, err
	}
	return id, nil
}

// ListOrgRoles 组织自定义角色
func (r *Repository) ListOrgRoles(ctx context.Context, orgID int64) ([]orgdomain.Role, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+orgRoleColumns+orgRoleFrom+
		` WHERE r.org_id = $1 ORDER BY r.id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]orgdomain.Role, 0)
	for rows.Next() {
		role, err := scanOrgRole(rows)
		if err != nil {
			return nil, err
		}
		if role != nil {
			items = append(items, *role)
		}
	}
	return items, rows.Err()
}

// CreateOrgRole 创建组织角色
func (r *Repository) CreateOrgRole(ctx context.Context, orgID int64, input orgdomain.RoleInput, createdBy int64) (*orgdomain.Role, error) {
	if orgdomain.IsValidRole(input.RoleKey) {
		return nil, apperrors.New(40055, http.StatusBadRequest, "角色标识与内置角色冲突，请换一个")
	}
	var id int64
	err := r.pool.QueryRow(ctx, `INSERT INTO org_roles (org_id, role_key, name, description, permissions, created_by)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`,
		orgID, input.RoleKey, input.Name, input.Description, input.Permissions, nullableID(createdBy)).Scan(&id)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, apperrors.New(40973, http.StatusConflict, "角色标识在该组织内已存在")
		}
		return nil, err
	}
	return scanOrgRole(r.pool.QueryRow(ctx, `SELECT `+orgRoleColumns+orgRoleFrom+` WHERE r.id = $1`, id))
}

// UpdateOrgRole 更新组织角色
func (r *Repository) UpdateOrgRole(ctx context.Context, orgID, roleID int64, input orgdomain.RoleInput) (*orgdomain.Role, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE org_roles
		SET name = $3, description = $4, permissions = $5, updated_at = NOW()
		WHERE id = $1 AND org_id = $2 AND is_builtin = FALSE`,
		roleID, orgID, input.Name, input.Description, input.Permissions)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrRoleNotFound
	}
	return scanOrgRole(r.pool.QueryRow(ctx, `SELECT `+orgRoleColumns+orgRoleFrom+` WHERE r.id = $1`, roleID))
}

// DeleteOrgRole 删除组织角色（授予记录级联清理）
func (r *Repository) DeleteOrgRole(ctx context.Context, orgID, roleID int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM org_roles WHERE id = $1 AND org_id = $2 AND is_builtin = FALSE`,
		roleID, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrRoleNotFound
	}
	return nil
}

// GrantOrgRole 授予组织角色，可限定到某个部门子树
func (r *Repository) GrantOrgRole(ctx context.Context, orgID, roleID int64, input orgdomain.GrantRoleInput, grantedBy int64) (int, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var scopeDeptID *int64
	if input.ScopeDeptUUID != "" {
		if !isUUIDLike(input.ScopeDeptUUID) {
			return 0, ErrDeptNotFound
		}
		var id int64
		err := tx.QueryRow(ctx, `SELECT id FROM departments WHERE uuid = $1::uuid AND org_id = $2`,
			input.ScopeDeptUUID, orgID).Scan(&id)
		if err != nil {
			if err == pgx.ErrNoRows {
				return 0, ErrDeptNotFound
			}
			return 0, err
		}
		scopeDeptID = &id
	}

	granted := 0
	for _, adminID := range input.AdminIDs {
		if adminID <= 0 {
			continue
		}
		if err := assertOrgMember(ctx, tx, orgID, adminID); err != nil {
			return 0, err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO org_role_members (org_role_id, admin_id, scope_dept_id, granted_by)
			VALUES ($1,$2,$3,$4) ON CONFLICT DO NOTHING`, roleID, adminID, scopeDeptID, nullableID(grantedBy))
		if err != nil {
			return 0, err
		}
		granted += int(tag.RowsAffected())
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return granted, nil
}

// RevokeOrgRole 撤销授予
func (r *Repository) RevokeOrgRole(ctx context.Context, orgID, roleID, adminID int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM org_role_members rm
		USING org_roles r
		WHERE rm.org_role_id = r.id AND r.id = $1 AND r.org_id = $2 AND rm.admin_id = $3`,
		roleID, orgID, adminID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperrors.New(40479, http.StatusNotFound, "该成员未被授予此角色")
	}
	return nil
}

// ListRoleGrants 角色的授予记录
func (r *Repository) ListRoleGrants(ctx context.Context, orgID, roleID int64) ([]orgdomain.RoleGrant, error) {
	rows, err := r.pool.Query(ctx, `SELECT ro.uuid::text, ro.name, rm.admin_id, a.account,
		COALESCE(NULLIF(a.display_name, ''), a.account, ''),
		COALESCE(d.uuid::text, ''), COALESCE(d.name, ''), rm.granted_by, rm.granted_at
		FROM org_role_members rm
		JOIN org_roles ro ON ro.id = rm.org_role_id
		JOIN admin_accounts a ON a.id = rm.admin_id
		LEFT JOIN departments d ON d.id = rm.scope_dept_id
		WHERE ro.id = $1 AND ro.org_id = $2 ORDER BY rm.granted_at DESC`, roleID, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]orgdomain.RoleGrant, 0)
	for rows.Next() {
		var g orgdomain.RoleGrant
		var deptUUID, deptName string
		if err := rows.Scan(&g.RoleUUID, &g.RoleName, &g.AdminID, &g.Account, &g.DisplayName,
			&deptUUID, &deptName, &g.GrantedBy, &g.GrantedAt); err != nil {
			return nil, err
		}
		if deptUUID != "" {
			g.ScopeDept = &orgdomain.DeptRef{UUID: deptUUID, Name: deptName}
		}
		items = append(items, g)
	}
	return items, rows.Err()
}

// OrgRoleGrant 管理员在某组织持有的自定义角色（含部门限定）
type OrgRoleGrant struct {
	RoleKey     string
	Permissions []string
	ScopeDeptID *int64
}

// ListAdminOrgRoleGrants 管理员在指定组织持有的自定义角色。
// Casbin 判定时先把这些角色的权限并进来，再叠加内置角色的权限。
func (r *Repository) ListAdminOrgRoleGrants(ctx context.Context, orgID, adminID int64) ([]OrgRoleGrant, error) {
	rows, err := r.pool.Query(ctx, `SELECT ro.role_key, ro.permissions, rm.scope_dept_id
		FROM org_role_members rm JOIN org_roles ro ON ro.id = rm.org_role_id
		WHERE ro.org_id = $1 AND rm.admin_id = $2`, orgID, adminID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]OrgRoleGrant, 0)
	for rows.Next() {
		var g OrgRoleGrant
		if err := rows.Scan(&g.RoleKey, &g.Permissions, &g.ScopeDeptID); err != nil {
			return nil, err
		}
		items = append(items, g)
	}
	return items, rows.Err()
}

// ListAllOrgRolePolicies 全部组织角色策略，供 Casbin 启动时批量装载
func (r *Repository) ListAllOrgRolePolicies(ctx context.Context) ([]OrgRolePolicy, error) {
	rows, err := r.pool.Query(ctx, `SELECT r.org_id, r.role_key, r.permissions FROM org_roles r`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]OrgRolePolicy, 0)
	for rows.Next() {
		var p OrgRolePolicy
		if err := rows.Scan(&p.OrgID, &p.RoleKey, &p.Permissions); err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}

// OrgRolePolicy 组织角色的权限策略
type OrgRolePolicy struct {
	OrgID       int64
	RoleKey     string
	Permissions []string
}

// AdminDeptScopes 管理员在组织内被限定的部门范围（角色授予时指定了 scope_dept_id）。
// 返回空切片表示不受部门限制。
func (r *Repository) AdminDeptScopes(ctx context.Context, orgID, adminID int64) ([]int64, error) {
	rows, err := r.pool.Query(ctx, `SELECT DISTINCT c.descendant_id
		FROM org_role_members rm
		JOIN org_roles ro ON ro.id = rm.org_role_id
		JOIN department_closure c ON c.ancestor_id = rm.scope_dept_id
		WHERE ro.org_id = $1 AND rm.admin_id = $2 AND rm.scope_dept_id IS NOT NULL`, orgID, adminID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
