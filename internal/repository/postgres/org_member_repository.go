package postgres

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"

	orgdomain "aegis/internal/domain/organization"
	apperrors "aegis/pkg/errors"

	"github.com/jackc/pgx/v5"
)

// 组织成员是独立于部门的一层：管理员先「入组织」，再决定进哪些部门。
// 没有这层的话，「已加入公司但还没分配部门」这种再常见不过的状态无处安放，
// 而且组织角色（owner/admin/…）也没有落脚点。

const orgMemberColumns = `m.id, m.uuid::text, o.uuid::text, m.admin_id,
	a.account, COALESCE(NULLIF(a.display_name, ''), a.account, ''), COALESCE(a.email, ''), COALESCE(a.avatar, ''),
	m.org_role, m.employee_no, m.title, m.status, m.joined_at, m.left_at, m.invited_by,
	COALESCE(a.is_super_admin, FALSE), m.primary_dept_id,
	COALESCE(pd.uuid::text, ''), COALESCE(pd.name, '')`

const orgMemberFrom = ` FROM org_members m
	JOIN organizations o ON o.id = m.org_id
	JOIN admin_accounts a ON a.id = m.admin_id
	LEFT JOIN departments pd ON pd.id = m.primary_dept_id`

func scanOrgMember(row pgx.Row) (*orgdomain.Member, error) {
	var m orgdomain.Member
	var primaryDeptID *int64
	var primaryUUID, primaryName string
	err := row.Scan(&m.ID, &m.UUID, &m.OrgUUID, &m.AdminID,
		&m.Account, &m.DisplayName, &m.Email, &m.Avatar,
		&m.OrgRole, &m.EmployeeNo, &m.Title, &m.Status, &m.JoinedAt, &m.LeftAt, &m.InvitedBy,
		&m.IsSuperAdmin, &primaryDeptID, &primaryUUID, &primaryName)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if primaryDeptID != nil && primaryUUID != "" {
		m.PrimaryDept = &orgdomain.DeptRef{UUID: primaryUUID, Name: primaryName}
	}
	m.Departments = []orgdomain.DeptRef{}
	return &m, nil
}

// ── 读取 ──

// ListOrgMembers 组织成员分页查询
func (r *Repository) ListOrgMembers(ctx context.Context, orgID int64, q orgdomain.MemberListQuery) (*orgdomain.Page[orgdomain.Member], error) {
	where := []string{"m.org_id = $1"}
	args := []any{orgID}
	idx := 2

	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		where = append(where, fmt.Sprintf(
			"(a.account ILIKE $%d OR a.display_name ILIKE $%d OR a.email ILIKE $%d OR m.employee_no ILIKE $%d)",
			idx, idx, idx, idx))
		args = append(args, "%"+kw+"%")
		idx++
	}
	if q.OrgRole != "" {
		where = append(where, fmt.Sprintf("m.org_role = $%d", idx))
		args = append(args, q.OrgRole)
		idx++
	}
	if q.Status != "" {
		where = append(where, fmt.Sprintf("m.status = $%d", idx))
		args = append(args, q.Status)
		idx++
	} else {
		where = append(where, "m.status <> 'left'")
	}
	if q.DeptUUID != "" {
		deptID, err := r.ResolveDeptID(ctx, orgID, q.DeptUUID)
		if err != nil {
			return nil, err
		}
		if q.IncludeSubDept {
			where = append(where, fmt.Sprintf(`EXISTS (SELECT 1 FROM department_members dm
				JOIN department_closure c ON c.descendant_id = dm.department_id
				WHERE dm.admin_id = m.admin_id AND dm.org_id = m.org_id AND c.ancestor_id = $%d)`, idx))
		} else {
			where = append(where, fmt.Sprintf(`EXISTS (SELECT 1 FROM department_members dm
				WHERE dm.admin_id = m.admin_id AND dm.department_id = $%d)`, idx))
		}
		args = append(args, deptID)
		idx++
	}
	if q.Unassigned {
		where = append(where, `NOT EXISTS (SELECT 1 FROM department_members dm
			WHERE dm.org_id = m.org_id AND dm.admin_id = m.admin_id)`)
	}
	whereSQL := strings.Join(where, " AND ")

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+orgMemberFrom+` WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, err
	}

	page, limit := normalizeOrgPaging(q.Page, q.Limit)
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(
		`SELECT %s%s WHERE %s ORDER BY
			CASE m.org_role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 WHEN 'manager' THEN 2
				WHEN 'member' THEN 3 ELSE 4 END, a.account
		 LIMIT $%d OFFSET $%d`, orgMemberColumns, orgMemberFrom, whereSQL, idx, idx+1), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]orgdomain.Member, 0, limit)
	adminIDs := make([]int64, 0, limit)
	for rows.Next() {
		m, err := scanOrgMember(rows)
		if err != nil {
			return nil, err
		}
		if m != nil {
			items = append(items, *m)
			adminIDs = append(adminIDs, m.AdminID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 部门归属一次批量取回并回填，避免每个成员打一次库
	deptMap, err := r.memberDepartments(ctx, orgID, adminIDs)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if refs, ok := deptMap[items[i].AdminID]; ok {
			items[i].Departments = refs
		}
	}

	return &orgdomain.Page[orgdomain.Member]{
		Items: items, Page: page, Limit: limit, Total: total,
		TotalPages: int(math.Ceil(float64(total) / float64(limit))),
	}, nil
}

// memberDepartments 批量取成员的部门归属（含全名路径）
func (r *Repository) memberDepartments(ctx context.Context, orgID int64, adminIDs []int64) (map[int64][]orgdomain.DeptRef, error) {
	result := map[int64][]orgdomain.DeptRef{}
	if len(adminIDs) == 0 {
		return result, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT dm.admin_id, d.uuid::text, d.name, dm.is_leader,
		(SELECT string_agg(anc.name, ' / ' ORDER BY c.depth DESC)
		   FROM department_closure c JOIN departments anc ON anc.id = c.ancestor_id
		  WHERE c.descendant_id = d.id)
		FROM department_members dm JOIN departments d ON d.id = dm.department_id
		WHERE dm.org_id = $1 AND dm.admin_id = ANY($2)
		ORDER BY dm.is_leader DESC, d.path`, orgID, adminIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var adminID int64
		var ref orgdomain.DeptRef
		var fullName *string
		if err := rows.Scan(&adminID, &ref.UUID, &ref.Name, &ref.IsLeader, &fullName); err != nil {
			return nil, err
		}
		if fullName != nil {
			ref.FullName = *fullName
		}
		result[adminID] = append(result[adminID], ref)
	}
	return result, rows.Err()
}

// GetOrgMember 读单个组织成员
func (r *Repository) GetOrgMember(ctx context.Context, orgID, adminID int64) (*orgdomain.Member, error) {
	m, err := scanOrgMember(r.pool.QueryRow(ctx,
		`SELECT `+orgMemberColumns+orgMemberFrom+` WHERE m.org_id = $1 AND m.admin_id = $2`, orgID, adminID))
	if err != nil || m == nil {
		return nil, err
	}
	deptMap, err := r.memberDepartments(ctx, orgID, []int64{adminID})
	if err != nil {
		return nil, err
	}
	if refs, ok := deptMap[adminID]; ok {
		m.Departments = refs
	}
	return m, nil
}

// GetMemberRole 取管理员在组织中的角色，非成员返回空串。
// 权限判定的入口，走得非常频繁，因此只查一列。
func (r *Repository) GetMemberRole(ctx context.Context, orgID, adminID int64) (string, error) {
	var role string
	err := r.pool.QueryRow(ctx, `SELECT org_role FROM org_members
		WHERE org_id = $1 AND admin_id = $2 AND status = 'active'`, orgID, adminID).Scan(&role)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return role, nil
}

// ListAdminOrganizations 管理员所属的全部组织（含其角色）
func (r *Repository) ListAdminOrganizations(ctx context.Context, adminID int64) ([]orgdomain.Organization, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+orgSelectColumns+`, m.org_role`+orgFromClause+`
		JOIN org_members m ON m.org_id = o.id AND m.admin_id = $1 AND m.status = 'active'
		ORDER BY o.path, o.id`, adminID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]orgdomain.Organization, 0)
	for rows.Next() {
		var o orgdomain.Organization
		if err := rows.Scan(
			&o.ID, &o.UUID, &o.ParentID, &o.ParentUUID, &o.ParentName,
			&o.Path, &o.Depth, &o.Name, &o.Code, &o.Kind, &o.Description, &o.LogoURL, &o.Status,
			&o.OwnerID, &o.OwnerName, &o.OwnerAccount,
			&o.Contact.Name, &o.Contact.Email, &o.Contact.Phone, &o.Industry, &o.Region,
			&o.Quota.MemberLimit, &o.Quota.DeptLimit, &o.Quota.AppLimit, &o.ExpiresAt, &o.Settings,
			&o.CreatedBy, &o.CreatedAt, &o.UpdatedAt,
			&o.Stats.MemberCount, &o.Stats.DeptCount, &o.Stats.AppCount, &o.Stats.ChildCount,
			&o.ViewerRole,
		); err != nil {
			return nil, err
		}
		items = append(items, o)
	}
	return items, rows.Err()
}

// ResolveAdminIDsByAccount 批量把登录账号解析为管理员 ID（Excel 导入用）。
// 表里没有的账号不会出现在结果 map 里，调用方据此报「账号不存在」。
func (r *Repository) ResolveAdminIDsByAccount(ctx context.Context, accounts []string) (map[string]int64, error) {
	result := map[string]int64{}
	if len(accounts) == 0 {
		return result, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT account, id FROM admin_accounts WHERE account = ANY($1)`, accounts)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var account string
		var id int64
		if err := rows.Scan(&account, &id); err != nil {
			return nil, err
		}
		result[account] = id
	}
	return result, rows.Err()
}

// SearchAssignableAdmins 搜索可加入组织的管理员（成员选择器用）。
// excludeOrgID > 0 时排除已在该组织中的人 —— 选择器里列出已经在里面的人
// 只会让操作者反复试错。
func (r *Repository) SearchAssignableAdmins(ctx context.Context, keyword string, excludeOrgID int64, limit int) ([]orgdomain.Member, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	where := []string{"a.status = 'active'"}
	args := []any{}
	idx := 1
	if kw := strings.TrimSpace(keyword); kw != "" {
		where = append(where, fmt.Sprintf("(a.account ILIKE $%d OR a.display_name ILIKE $%d OR a.email ILIKE $%d)", idx, idx, idx))
		args = append(args, "%"+kw+"%")
		idx++
	}
	if excludeOrgID > 0 {
		where = append(where, fmt.Sprintf(`NOT EXISTS (SELECT 1 FROM org_members m
			WHERE m.org_id = $%d AND m.admin_id = a.id AND m.status <> 'left')`, idx))
		args = append(args, excludeOrgID)
		idx++
	}
	args = append(args, limit)

	rows, err := r.pool.Query(ctx, fmt.Sprintf(`SELECT a.id, a.account,
		COALESCE(NULLIF(a.display_name, ''), a.account, ''), COALESCE(a.email, ''), COALESCE(a.avatar, ''),
		COALESCE(a.is_super_admin, FALSE)
		FROM admin_accounts a WHERE %s ORDER BY a.account LIMIT $%d`,
		strings.Join(where, " AND "), idx), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]orgdomain.Member, 0, limit)
	for rows.Next() {
		var m orgdomain.Member
		if err := rows.Scan(&m.AdminID, &m.Account, &m.DisplayName, &m.Email, &m.Avatar, &m.IsSuperAdmin); err != nil {
			return nil, err
		}
		m.Departments = []orgdomain.DeptRef{}
		items = append(items, m)
	}
	return items, rows.Err()
}

// ── 写入 ──

// AddOrgMember 直接把管理员加入组织并可选地分配部门
func (r *Repository) AddOrgMember(ctx context.Context, orgID int64, input orgdomain.AddMemberInput, invitedBy int64) (*orgdomain.Member, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	role := input.OrgRole
	if !orgdomain.IsValidRole(role) {
		role = orgdomain.RoleMember
	}
	// owner 只能通过「转让」产生，避免出现两个主人
	if role == orgdomain.RoleOwner {
		return nil, apperrors.New(40049, http.StatusBadRequest, "所有者只能通过转让产生，请先加为管理员")
	}

	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM admin_accounts WHERE id = $1)`, input.AdminID).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, apperrors.New(40450, http.StatusNotFound, "管理员不存在")
	}

	if _, err := tx.Exec(ctx, `INSERT INTO org_members (org_id, admin_id, org_role, employee_no, title, invited_by)
		VALUES ($1,$2,$3,$4,$5,$6)
		ON CONFLICT (org_id, admin_id) DO UPDATE SET
			org_role = EXCLUDED.org_role, employee_no = EXCLUDED.employee_no,
			title = EXCLUDED.title, status = 'active', left_at = NULL, updated_at = NOW()`,
		orgID, input.AdminID, role, input.EmployeeNo, input.Title, nullableID(invitedBy)); err != nil {
		return nil, err
	}

	if len(input.DeptUUIDs) > 0 {
		if err := replaceMemberDepartments(ctx, tx, orgID, input.AdminID, input.DeptUUIDs, true); err != nil {
			return nil, err
		}
	}
	if input.PrimaryDept != "" {
		if err := setPrimaryDepartment(ctx, tx, orgID, input.AdminID, input.PrimaryDept); err != nil {
			return nil, err
		}
	}

	touchOrgUpdatedAt(ctx, tx, orgID)
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetOrgMember(ctx, orgID, input.AdminID)
}

// UpdateOrgMember 更新成员档案
func (r *Repository) UpdateOrgMember(ctx context.Context, orgID, adminID int64, input orgdomain.UpdateMemberInput) (*orgdomain.Member, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	sets := []string{"updated_at = NOW()"}
	args := []any{orgID, adminID}
	idx := 3
	addSet := func(column string, value any) {
		sets = append(sets, fmt.Sprintf("%s = $%d", column, idx))
		args = append(args, value)
		idx++
	}
	if input.OrgRole != nil {
		if *input.OrgRole == orgdomain.RoleOwner {
			return nil, apperrors.New(40049, http.StatusBadRequest, "所有者只能通过转让产生")
		}
		addSet("org_role", *input.OrgRole)
	}
	if input.EmployeeNo != nil {
		addSet("employee_no", *input.EmployeeNo)
	}
	if input.Title != nil {
		addSet("title", *input.Title)
	}
	if input.Status != nil {
		addSet("status", *input.Status)
		if *input.Status == "left" {
			sets = append(sets, "left_at = NOW()")
		} else {
			sets = append(sets, "left_at = NULL")
		}
	}

	tag, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE org_members SET %s WHERE org_id = $1 AND admin_id = $2`,
		strings.Join(sets, ", ")), args...)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrMemberNotFound
	}

	if input.PrimaryDept != nil {
		if err := setPrimaryDepartment(ctx, tx, orgID, adminID, *input.PrimaryDept); err != nil {
			return nil, err
		}
	}
	// 离职即退出所有部门，但保留组织籍以便留痕与恢复
	if input.Status != nil && *input.Status == "left" {
		if _, err := tx.Exec(ctx, `DELETE FROM department_members WHERE org_id = $1 AND admin_id = $2`,
			orgID, adminID); err != nil {
			return nil, err
		}
	}

	touchOrgUpdatedAt(ctx, tx, orgID)
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetOrgMember(ctx, orgID, adminID)
}

// RemoveOrgMember 移出组织。owner 不可移除，必须先转让。
func (r *Repository) RemoveOrgMember(ctx context.Context, orgID, adminID int64) error {
	var role string
	err := r.pool.QueryRow(ctx, `SELECT org_role FROM org_members WHERE org_id = $1 AND admin_id = $2`,
		orgID, adminID).Scan(&role)
	if err != nil {
		if err == pgx.ErrNoRows {
			return ErrMemberNotFound
		}
		return err
	}
	if role == orgdomain.RoleOwner {
		return apperrors.New(40050, http.StatusBadRequest, "不能移除组织所有者，请先转让所有权")
	}
	// department_members 通过 (org_id, admin_id) 复合外键级联清理
	_, err = r.pool.Exec(ctx, `DELETE FROM org_members WHERE org_id = $1 AND admin_id = $2`, orgID, adminID)
	return err
}

// AssignMemberDepartments 调整成员的部门归属
func (r *Repository) AssignMemberDepartments(ctx context.Context, orgID, adminID int64, input orgdomain.AssignDeptInput) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := assertOrgMember(ctx, tx, orgID, adminID); err != nil {
		return err
	}
	if err := replaceMemberDepartments(ctx, tx, orgID, adminID, input.DeptUUIDs, input.Replace); err != nil {
		return err
	}
	if input.PrimaryDept != "" {
		if err := setPrimaryDepartment(ctx, tx, orgID, adminID, input.PrimaryDept); err != nil {
			return err
		}
	}
	touchOrgUpdatedAt(ctx, tx, orgID)
	return tx.Commit(ctx)
}

func replaceMemberDepartments(ctx context.Context, tx pgx.Tx, orgID, adminID int64, deptUUIDs []string, replace bool) error {
	deptIDs := make([]int64, 0, len(deptUUIDs))
	for _, u := range deptUUIDs {
		if !isUUIDLike(u) {
			continue
		}
		var id int64
		err := tx.QueryRow(ctx, `SELECT id FROM departments WHERE uuid = $1::uuid AND org_id = $2`, u, orgID).Scan(&id)
		if err != nil {
			if err == pgx.ErrNoRows {
				return ErrDeptNotFound
			}
			return err
		}
		deptIDs = append(deptIDs, id)
	}

	if replace {
		if len(deptIDs) == 0 {
			if _, err := tx.Exec(ctx, `DELETE FROM department_members WHERE org_id = $1 AND admin_id = $2`,
				orgID, adminID); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `DELETE FROM department_members
			WHERE org_id = $1 AND admin_id = $2 AND department_id <> ALL($3)`, orgID, adminID, deptIDs); err != nil {
			return err
		}
	}
	for _, id := range deptIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO department_members (department_id, org_id, admin_id)
			VALUES ($1,$2,$3) ON CONFLICT (department_id, admin_id) DO NOTHING`, id, orgID, adminID); err != nil {
			return err
		}
	}
	// 主部门若已不在归属列表中，清掉以免指向一个已退出的部门
	if _, err := tx.Exec(ctx, `UPDATE org_members SET primary_dept_id = NULL
		WHERE org_id = $1 AND admin_id = $2 AND primary_dept_id IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM department_members dm
			WHERE dm.org_id = $1 AND dm.admin_id = $2 AND dm.department_id = org_members.primary_dept_id)`,
		orgID, adminID); err != nil {
		return err
	}
	return nil
}

func setPrimaryDepartment(ctx context.Context, tx pgx.Tx, orgID, adminID int64, deptUUID string) error {
	if deptUUID == "" {
		_, err := tx.Exec(ctx, `UPDATE org_members SET primary_dept_id = NULL, updated_at = NOW()
			WHERE org_id = $1 AND admin_id = $2`, orgID, adminID)
		return err
	}
	if !isUUIDLike(deptUUID) {
		return ErrDeptNotFound
	}
	var deptID int64
	err := tx.QueryRow(ctx, `SELECT id FROM departments WHERE uuid = $1::uuid AND org_id = $2`, deptUUID, orgID).Scan(&deptID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return ErrDeptNotFound
		}
		return err
	}
	// 主部门必须是已归属的部门之一
	if _, err := tx.Exec(ctx, `INSERT INTO department_members (department_id, org_id, admin_id)
		VALUES ($1,$2,$3) ON CONFLICT (department_id, admin_id) DO NOTHING`, deptID, orgID, adminID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE org_members SET primary_dept_id = $3, updated_at = NOW()
		WHERE org_id = $1 AND admin_id = $2`, orgID, adminID, deptID)
	return err
}

// ── 部门成员 ──

// ListDepartmentMembers 部门内成员（含岗位 / 汇报线 / 代理）
func (r *Repository) ListDepartmentMembers(ctx context.Context, orgID, deptID int64) ([]orgdomain.DepartmentMember, error) {
	rows, err := r.pool.Query(ctx, `SELECT a.id, a.account,
		COALESCE(NULLIF(a.display_name, ''), a.account, ''), COALESCE(a.avatar, ''),
		COALESCE(om.org_role, 'member'), dm.is_leader, dm.joined_at,
		COALESCE(p.uuid::text, ''), COALESCE(p.name, ''), COALESCE(dm.job_title, ''),
		dm.reporting_to, COALESCE(NULLIF(rpt.display_name, ''), rpt.account, ''),
		dm.delegate_to, COALESCE(NULLIF(dlg.display_name, ''), dlg.account, ''), dm.delegate_expires_at
		FROM department_members dm
		JOIN admin_accounts a ON a.id = dm.admin_id
		LEFT JOIN org_members om ON om.org_id = dm.org_id AND om.admin_id = dm.admin_id
		LEFT JOIN positions p ON p.id = dm.position_id
		LEFT JOIN admin_accounts rpt ON rpt.id = dm.reporting_to
		LEFT JOIN admin_accounts dlg ON dlg.id = dm.delegate_to
		WHERE dm.department_id = $1 AND dm.org_id = $2
		ORDER BY dm.is_leader DESC, a.account`, deptID, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]orgdomain.DepartmentMember, 0)
	for rows.Next() {
		var m orgdomain.DepartmentMember
		if err := rows.Scan(&m.AdminID, &m.Account, &m.DisplayName, &m.Avatar,
			&m.OrgRole, &m.IsLeader, &m.JoinedAt,
			&m.PositionUUID, &m.PositionName, &m.JobTitle,
			&m.ReportingTo, &m.ReportingName,
			&m.DelegateTo, &m.DelegateName, &m.DelegateExpiresAt); err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

// SetDepartmentMember 设置部门内成员属性（岗位 / 职位 / 汇报线 / 代理 / 负责人）
func (r *Repository) SetDepartmentMember(ctx context.Context, orgID, deptID, adminID int64, input orgdomain.SetDeptMemberInput) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	sets := []string{}
	args := []any{deptID, adminID, orgID}
	idx := 4
	addSet := func(column string, value any) {
		sets = append(sets, fmt.Sprintf("%s = $%d", column, idx))
		args = append(args, value)
		idx++
	}

	if input.PositionUUID != nil {
		if *input.PositionUUID == "" {
			sets = append(sets, "position_id = NULL")
		} else {
			if !isUUIDLike(*input.PositionUUID) {
				return ErrPositionNoFind
			}
			var posID int64
			err := tx.QueryRow(ctx, `SELECT id FROM positions WHERE uuid = $1::uuid AND org_id = $2`,
				*input.PositionUUID, orgID).Scan(&posID)
			if err != nil {
				if err == pgx.ErrNoRows {
					return ErrPositionNoFind
				}
				return err
			}
			addSet("position_id", posID)
		}
	}
	if input.JobTitle != nil {
		addSet("job_title", *input.JobTitle)
	}
	if input.ClearReport {
		sets = append(sets, "reporting_to = NULL")
	} else if input.ReportingTo != nil {
		if *input.ReportingTo == adminID {
			return apperrors.New(40051, http.StatusBadRequest, "不能把自己设为自己的上级")
		}
		if err := assertDeptMember(ctx, tx, deptID, *input.ReportingTo); err != nil {
			return err
		}
		if err := assertNoReportingCycle(ctx, tx, deptID, adminID, *input.ReportingTo); err != nil {
			return err
		}
		addSet("reporting_to", *input.ReportingTo)
	}
	if input.ClearDeleg {
		sets = append(sets, "delegate_to = NULL", "delegate_expires_at = NULL")
	} else if input.DelegateTo != nil {
		if *input.DelegateTo == adminID {
			return apperrors.New(40052, http.StatusBadRequest, "不能把权限代理给自己")
		}
		if err := assertOrgMember(ctx, tx, orgID, *input.DelegateTo); err != nil {
			return err
		}
		addSet("delegate_to", *input.DelegateTo)
		if input.DelegateTill != nil {
			addSet("delegate_expires_at", *input.DelegateTill)
		} else {
			sets = append(sets, "delegate_expires_at = NULL")
		}
	}
	if input.IsLeader != nil {
		addSet("is_leader", *input.IsLeader)
	}

	if len(sets) == 0 {
		return nil
	}
	tag, err := tx.Exec(ctx, fmt.Sprintf(
		`UPDATE department_members SET %s WHERE department_id = $1 AND admin_id = $2 AND org_id = $3`,
		strings.Join(sets, ", ")), args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrMemberNotFound
	}

	// 负责人标记与 departments.leader_id 保持同步，否则两处会各说各话
	if input.IsLeader != nil {
		if *input.IsLeader {
			if _, err := tx.Exec(ctx, `UPDATE department_members SET is_leader = FALSE
				WHERE department_id = $1 AND admin_id <> $2`, deptID, adminID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE departments SET leader_id = $2, updated_at = NOW() WHERE id = $1`,
				deptID, adminID); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `UPDATE departments SET leader_id = NULL, updated_at = NOW()
			WHERE id = $1 AND leader_id = $2`, deptID, adminID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// RemoveDepartmentMember 把成员移出部门（仍保留组织籍）
func (r *Repository) RemoveDepartmentMember(ctx context.Context, orgID, deptID, adminID int64) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `DELETE FROM department_members
		WHERE department_id = $1 AND admin_id = $2 AND org_id = $3`, deptID, adminID, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrMemberNotFound
	}
	if _, err := tx.Exec(ctx, `UPDATE departments SET leader_id = NULL, updated_at = NOW()
		WHERE id = $1 AND leader_id = $2`, deptID, adminID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE org_members SET primary_dept_id = NULL, updated_at = NOW()
		WHERE org_id = $1 AND admin_id = $2 AND primary_dept_id = $3`, orgID, adminID, deptID); err != nil {
		return err
	}
	// 被移走的人若是别人的上级，把悬空的汇报线一并清掉
	if _, err := tx.Exec(ctx, `UPDATE department_members SET reporting_to = NULL
		WHERE department_id = $1 AND reporting_to = $2`, deptID, adminID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// GetReportingChain 汇报链（自本人向上，最多 20 层）
func (r *Repository) GetReportingChain(ctx context.Context, orgID, deptID, adminID int64) ([]orgdomain.ReportingNode, error) {
	rows, err := r.pool.Query(ctx, `WITH RECURSIVE chain AS (
		SELECT dm.admin_id, a.account, COALESCE(NULLIF(a.display_name, ''), a.account, '') AS display_name,
		       COALESCE(a.avatar, '') AS avatar, COALESCE(dm.job_title, '') AS job_title,
		       0 AS depth, dm.reporting_to
		  FROM department_members dm JOIN admin_accounts a ON a.id = dm.admin_id
		 WHERE dm.department_id = $1 AND dm.admin_id = $2 AND dm.org_id = $3
		UNION ALL
		SELECT dm2.admin_id, a2.account, COALESCE(NULLIF(a2.display_name, ''), a2.account, ''),
		       COALESCE(a2.avatar, ''), COALESCE(dm2.job_title, ''), chain.depth + 1, dm2.reporting_to
		  FROM chain
		  JOIN department_members dm2 ON dm2.department_id = $1 AND dm2.admin_id = chain.reporting_to
		  JOIN admin_accounts a2 ON a2.id = dm2.admin_id
		 WHERE chain.reporting_to IS NOT NULL AND chain.depth < 20
	) SELECT admin_id, account, display_name, avatar, job_title, depth FROM chain ORDER BY depth`,
		deptID, adminID, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]orgdomain.ReportingNode, 0)
	for rows.Next() {
		var n orgdomain.ReportingNode
		if err := rows.Scan(&n.AdminID, &n.Account, &n.DisplayName, &n.Avatar, &n.JobTitle, &n.Depth); err != nil {
			return nil, err
		}
		items = append(items, n)
	}
	return items, rows.Err()
}

// ListDirectReports 直接下属
func (r *Repository) ListDirectReports(ctx context.Context, deptID, adminID int64) ([]orgdomain.ReportingNode, error) {
	rows, err := r.pool.Query(ctx, `SELECT dm.admin_id, a.account,
		COALESCE(NULLIF(a.display_name, ''), a.account, ''), COALESCE(a.avatar, ''), COALESCE(dm.job_title, ''), 1
		FROM department_members dm JOIN admin_accounts a ON a.id = dm.admin_id
		WHERE dm.department_id = $1 AND dm.reporting_to = $2 ORDER BY a.account`, deptID, adminID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]orgdomain.ReportingNode, 0)
	for rows.Next() {
		var n orgdomain.ReportingNode
		if err := rows.Scan(&n.AdminID, &n.Account, &n.DisplayName, &n.Avatar, &n.JobTitle, &n.Depth); err != nil {
			return nil, err
		}
		items = append(items, n)
	}
	return items, rows.Err()
}

// ClearExpiredDelegates 清理到期的权限代理（Worker 定时调用）
func (r *Repository) ClearExpiredDelegates(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE department_members
		SET delegate_to = NULL, delegate_expires_at = NULL
		WHERE delegate_expires_at IS NOT NULL AND delegate_expires_at < NOW()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ── 内部辅助 ──

func assertDeptMember(ctx context.Context, exec queryExecutor, deptID, adminID int64) error {
	var ok bool
	if err := exec.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM department_members
		WHERE department_id = $1 AND admin_id = $2)`, deptID, adminID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return apperrors.New(40053, http.StatusBadRequest, "上级必须是同一部门的成员")
	}
	return nil
}

// assertNoReportingCycle 顺着候选上级的汇报线往上走，撞见自己就说明会成环。
// 没有这道检查，A→B→A 会让汇报链查询在递归上限处才停下，
// 而组织架构图会画出一个死循环。
func assertNoReportingCycle(ctx context.Context, exec queryExecutor, deptID, adminID, newSupervisor int64) error {
	var cyclic bool
	err := exec.QueryRow(ctx, `WITH RECURSIVE up AS (
		SELECT admin_id, reporting_to, 0 AS depth FROM department_members
		 WHERE department_id = $1 AND admin_id = $2
		UNION ALL
		SELECT dm.admin_id, dm.reporting_to, up.depth + 1 FROM up
		  JOIN department_members dm ON dm.department_id = $1 AND dm.admin_id = up.reporting_to
		 WHERE up.reporting_to IS NOT NULL AND up.depth < 20
	) SELECT EXISTS(SELECT 1 FROM up WHERE admin_id = $3)`, deptID, newSupervisor, adminID).Scan(&cyclic)
	if err != nil {
		return err
	}
	if cyclic {
		return apperrors.New(40054, http.StatusBadRequest, "该设置会让汇报关系成环")
	}
	return nil
}
