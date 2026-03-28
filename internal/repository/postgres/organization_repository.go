package postgres

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	systemdomain "aegis/internal/domain/system"
	apperrors "aegis/pkg/errors"

	"github.com/jackc/pgx/v5"
)

// ── 组织 CRUD ──

func (r *Repository) ListOrganizations(ctx context.Context) ([]systemdomain.Organization, error) {
	return r.listOrganizationsQuery(ctx, `SELECT id, name, code, description, logo_url, status, created_by, created_at, updated_at FROM organizations ORDER BY id`)
}

// ListOrganizationsForAdmin 仅返回管理员所属的组织
func (r *Repository) ListOrganizationsForAdmin(ctx context.Context, adminID int64) ([]systemdomain.Organization, error) {
	return r.listOrganizationsQuery(ctx, `SELECT DISTINCT o.id, o.name, o.code, o.description, o.logo_url, o.status, o.created_by, o.created_at, o.updated_at
		FROM organizations o
		JOIN departments d ON d.org_id = o.id
		JOIN department_members dm ON dm.department_id = d.id
		WHERE dm.admin_id = $1
		ORDER BY o.id`, adminID)
}

func (r *Repository) listOrganizationsQuery(ctx context.Context, query string, args ...any) ([]systemdomain.Organization, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []systemdomain.Organization
	for rows.Next() {
		var o systemdomain.Organization
		if err := rows.Scan(&o.ID, &o.Name, &o.Code, &o.Description, &o.LogoURL, &o.Status, &o.CreatedBy, &o.CreatedAt, &o.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, o)
	}
	return items, rows.Err()
}

func (r *Repository) GetOrganization(ctx context.Context, orgID int64) (*systemdomain.Organization, error) {
	var o systemdomain.Organization
	err := r.pool.QueryRow(ctx, `SELECT id, name, code, description, logo_url, status, created_by, created_at, updated_at FROM organizations WHERE id = $1`, orgID).
		Scan(&o.ID, &o.Name, &o.Code, &o.Description, &o.LogoURL, &o.Status, &o.CreatedBy, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &o, nil
}

func (r *Repository) CreateOrganization(ctx context.Context, input systemdomain.CreateOrgInput, createdBy int64) (*systemdomain.Organization, error) {
	var o systemdomain.Organization
	err := r.pool.QueryRow(ctx, `INSERT INTO organizations (name, code, description, logo_url, created_by) VALUES ($1,$2,$3,$4,$5) RETURNING id, name, code, description, logo_url, status, created_by, created_at, updated_at`,
		input.Name, input.Code, input.Description, input.LogoURL, createdBy).
		Scan(&o.ID, &o.Name, &o.Code, &o.Description, &o.LogoURL, &o.Status, &o.CreatedBy, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, apperrors.New(40970, http.StatusConflict, "组织代码已存在")
		}
		return nil, err
	}
	return &o, nil
}

func (r *Repository) UpdateOrganization(ctx context.Context, orgID int64, input systemdomain.UpdateOrgInput) (*systemdomain.Organization, error) {
	var o systemdomain.Organization
	err := r.pool.QueryRow(ctx, `UPDATE organizations SET
		name = COALESCE($2, name), code = COALESCE($3, code),
		description = COALESCE($4, description), logo_url = COALESCE($5, logo_url),
		status = COALESCE($6, status), updated_at = NOW()
		WHERE id = $1 RETURNING id, name, code, description, logo_url, status, created_by, created_at, updated_at`,
		orgID, input.Name, input.Code, input.Description, input.LogoURL, input.Status).
		Scan(&o.ID, &o.Name, &o.Code, &o.Description, &o.LogoURL, &o.Status, &o.CreatedBy, &o.CreatedAt, &o.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apperrors.New(40471, http.StatusNotFound, "组织不存在")
		}
		return nil, err
	}
	return &o, nil
}

func (r *Repository) DeleteOrganization(ctx context.Context, orgID int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperrors.New(40471, http.StatusNotFound, "组织不存在")
	}
	return nil
}

// ── 部门 CRUD ──

func (r *Repository) ListDepartments(ctx context.Context, orgID int64) ([]systemdomain.Department, error) {
	rows, err := r.pool.Query(ctx, `SELECT d.id, d.org_id, d.parent_id, d.name, d.code, d.description, d.sort_order, d.leader_id, COALESCE(a.display_name,''), d.status, d.created_at, d.updated_at,
		(SELECT COUNT(*) FROM department_members dm WHERE dm.department_id = d.id)
		FROM departments d LEFT JOIN admin_accounts a ON a.id = d.leader_id
		WHERE d.org_id = $1 ORDER BY d.sort_order, d.id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []systemdomain.Department
	for rows.Next() {
		var d systemdomain.Department
		if err := rows.Scan(&d.ID, &d.OrgID, &d.ParentID, &d.Name, &d.Code, &d.Description, &d.SortOrder, &d.LeaderID, &d.LeaderName, &d.Status, &d.CreatedAt, &d.UpdatedAt, &d.MemberCount); err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return items, rows.Err()
}

func (r *Repository) CreateDepartment(ctx context.Context, orgID int64, input systemdomain.CreateDeptInput) (*systemdomain.Department, error) {
	var d systemdomain.Department
	err := r.pool.QueryRow(ctx, `INSERT INTO departments (org_id, parent_id, name, code, description, sort_order, leader_id) VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id, org_id, parent_id, name, code, description, sort_order, leader_id, status, created_at, updated_at`,
		orgID, input.ParentID, input.Name, input.Code, input.Description, input.SortOrder, input.LeaderID).
		Scan(&d.ID, &d.OrgID, &d.ParentID, &d.Name, &d.Code, &d.Description, &d.SortOrder, &d.LeaderID, &d.Status, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, apperrors.New(40971, http.StatusConflict, "部门代码已存在")
		}
		return nil, err
	}
	return &d, nil
}

func (r *Repository) UpdateDepartment(ctx context.Context, deptID int64, input systemdomain.UpdateDeptInput) (*systemdomain.Department, error) {
	var d systemdomain.Department
	err := r.pool.QueryRow(ctx, `UPDATE departments SET
		name = COALESCE($2, name), code = COALESCE($3, code),
		description = COALESCE($4, description), sort_order = COALESCE($5, sort_order),
		leader_id = COALESCE($6, leader_id), status = COALESCE($7, status), updated_at = NOW()
		WHERE id = $1 RETURNING id, org_id, parent_id, name, code, description, sort_order, leader_id, status, created_at, updated_at`,
		deptID, input.Name, input.Code, input.Description, input.SortOrder, input.LeaderID, input.Status).
		Scan(&d.ID, &d.OrgID, &d.ParentID, &d.Name, &d.Code, &d.Description, &d.SortOrder, &d.LeaderID, &d.Status, &d.CreatedAt, &d.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apperrors.New(40472, http.StatusNotFound, "部门不存在")
		}
		return nil, err
	}
	return &d, nil
}

func (r *Repository) MoveDepartment(ctx context.Context, deptID int64, parentID *int64) error {
	_, err := r.pool.Exec(ctx, `UPDATE departments SET parent_id = $2, updated_at = NOW() WHERE id = $1`, deptID, parentID)
	return err
}

func (r *Repository) DeleteDepartment(ctx context.Context, deptID int64) error {
	// 子部门 parent_id 会被 ON DELETE SET NULL 处理（上移到根）
	tag, err := r.pool.Exec(ctx, `DELETE FROM departments WHERE id = $1`, deptID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperrors.New(40472, http.StatusNotFound, "部门不存在")
	}
	return nil
}

// ── 成员管理 ──

func (r *Repository) ListDepartmentMembers(ctx context.Context, deptID int64) ([]systemdomain.DepartmentMember, error) {
	rows, err := r.pool.Query(ctx, `SELECT a.id, a.account, a.display_name, COALESCE(a.avatar,''),
		dm.is_leader, dm.joined_at,
		dm.position_id, COALESCE(p.name,''), COALESCE(dm.job_title,''),
		dm.reporting_to, COALESCE(rpt.display_name,''),
		dm.delegate_to, COALESCE(dlg.display_name,''), dm.delegate_expires_at
		FROM department_members dm
		JOIN admin_accounts a ON a.id = dm.admin_id
		LEFT JOIN positions p ON p.id = dm.position_id
		LEFT JOIN admin_accounts rpt ON rpt.id = dm.reporting_to
		LEFT JOIN admin_accounts dlg ON dlg.id = dm.delegate_to
		WHERE dm.department_id = $1 ORDER BY dm.is_leader DESC, a.account`, deptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []systemdomain.DepartmentMember
	for rows.Next() {
		var m systemdomain.DepartmentMember
		var joinedAt interface{}
		if err := rows.Scan(
			&m.AdminID, &m.Account, &m.DisplayName, &m.Avatar,
			&m.IsLeader, &joinedAt,
			&m.PositionID, &m.PositionName, &m.JobTitle,
			&m.ReportingTo, &m.ReportingName,
			&m.DelegateTo, &m.DelegateName, &m.DelegateExpiresAt,
		); err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

func (r *Repository) AddDepartmentMember(ctx context.Context, deptID, adminID int64, isLeader bool) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO department_members (department_id, admin_id, is_leader) VALUES ($1,$2,$3) ON CONFLICT (department_id, admin_id) DO UPDATE SET is_leader = $3`, deptID, adminID, isLeader)
	return err
}

func (r *Repository) RemoveDepartmentMember(ctx context.Context, deptID, adminID int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM department_members WHERE department_id = $1 AND admin_id = $2`, deptID, adminID)
	return err
}

// IsDepartmentLeader 检查管理员是否是指定部门的负责人
func (r *Repository) IsDepartmentLeader(ctx context.Context, deptID, adminID int64) (bool, error) {
	var isLeader bool
	err := r.pool.QueryRow(ctx, `SELECT COALESCE(is_leader, false) FROM department_members WHERE department_id = $1 AND admin_id = $2`, deptID, adminID).Scan(&isLeader)
	if err != nil {
		return false, nil
	}
	return isLeader, nil
}

// IsDepartmentMember 检查管理员是否是指定部门的成员
func (r *Repository) IsDepartmentMember(ctx context.Context, deptID, adminID int64) (bool, error) {
	var count int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM department_members WHERE department_id = $1 AND admin_id = $2`, deptID, adminID).Scan(&count)
	return count > 0, err
}

// IsOrganizationMember 检查管理员是否属于指定组织（通过任意部门）
func (r *Repository) IsOrganizationMember(ctx context.Context, orgID, adminID int64) (bool, error) {
	var count int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM department_members dm
		JOIN departments d ON d.id = dm.department_id
		WHERE d.org_id = $1 AND dm.admin_id = $2`, orgID, adminID).Scan(&count)
	return count > 0, err
}

func (r *Repository) ListAdminDepartments(ctx context.Context, adminID int64) ([]systemdomain.Department, error) {
	rows, err := r.pool.Query(ctx, `SELECT d.id, d.org_id, d.parent_id, d.name, d.code, d.description, d.sort_order, d.leader_id, '', d.status, d.created_at, d.updated_at, 0
		FROM department_members dm JOIN departments d ON d.id = dm.department_id
		WHERE dm.admin_id = $1 ORDER BY d.name`, adminID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []systemdomain.Department
	for rows.Next() {
		var d systemdomain.Department
		if err := rows.Scan(&d.ID, &d.OrgID, &d.ParentID, &d.Name, &d.Code, &d.Description, &d.SortOrder, &d.LeaderID, &d.LeaderName, &d.Status, &d.CreatedAt, &d.UpdatedAt, &d.MemberCount); err != nil {
			return nil, err
		}
		items = append(items, d)
	}
	return items, rows.Err()
}

// ── 部门邀请 ──

const invitationSelectCols = `i.id, i.department_id, i.inviter_id, i.invitee_id, i.is_leader, i.status, COALESCE(i.message,''),
	COALESCE(d.name,''), COALESCE(o.name,''),
	COALESCE(inviter.display_name, inviter.account, ''), COALESCE(invitee.display_name, invitee.account, ''),
	i.responded_at, i.expires_at, i.created_at`

const invitationJoins = ` FROM department_invitations i
	LEFT JOIN departments d ON d.id = i.department_id
	LEFT JOIN organizations o ON o.id = d.org_id
	LEFT JOIN admin_accounts inviter ON inviter.id = i.inviter_id
	LEFT JOIN admin_accounts invitee ON invitee.id = i.invitee_id`

func scanInvitation(row pgx.Row) (*systemdomain.DeptInvitation, error) {
	var inv systemdomain.DeptInvitation
	if err := row.Scan(
		&inv.ID, &inv.DepartmentID, &inv.InviterID, &inv.InviteeID, &inv.IsLeader, &inv.Status, &inv.Message,
		&inv.DeptName, &inv.OrgName, &inv.InviterName, &inv.InviteeName,
		&inv.RespondedAt, &inv.ExpiresAt, &inv.CreatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &inv, nil
}

func (r *Repository) CreateDeptInvitation(ctx context.Context, input systemdomain.CreateInvitationInput, inviterID int64) (*systemdomain.DeptInvitation, error) {
	var id int64
	if err := r.pool.QueryRow(ctx,
		`INSERT INTO department_invitations (department_id, inviter_id, invitee_id, is_leader, message, expires_at)
VALUES ($1, $2, $3, $4, $5, NOW() + INTERVAL '7 days') RETURNING id`,
		input.DepartmentID, inviterID, input.InviteeID, input.IsLeader, input.Message,
	).Scan(&id); err != nil {
		if isDuplicateKeyError(err) {
			return nil, apperrors.New(40960, http.StatusConflict, "该成员已有待处理的邀请")
		}
		return nil, err
	}
	return r.GetDeptInvitation(ctx, id)
}

func (r *Repository) GetDeptInvitation(ctx context.Context, id int64) (*systemdomain.DeptInvitation, error) {
	return scanInvitation(r.pool.QueryRow(ctx, `SELECT `+invitationSelectCols+invitationJoins+` WHERE i.id = $1`, id))
}

func (r *Repository) ListDeptInvitations(ctx context.Context, q systemdomain.InvitationListQuery) (*systemdomain.InvitationListResult, error) {
	where := []string{"1=1"}
	args := []any{}
	idx := 1
	if q.Role == "sent" {
		where = append(where, fmt.Sprintf("i.inviter_id = $%d", idx))
	} else {
		where = append(where, fmt.Sprintf("i.invitee_id = $%d", idx))
	}
	args = append(args, q.AdminID)
	idx++
	if q.Status != "" {
		where = append(where, fmt.Sprintf("i.status = $%d", idx))
		args = append(args, q.Status)
		idx++
	}
	whereStr := strings.Join(where, " AND ")

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+invitationJoins+` WHERE `+whereStr, args...).Scan(&total); err != nil {
		return nil, err
	}
	page, limit := q.Page, q.Limit
	if page < 1 { page = 1 }
	if limit < 1 || limit > 50 { limit = 20 }
	offset := (page - 1) * limit
	args = append(args, limit, offset)

	rows, err := r.pool.Query(ctx, fmt.Sprintf(`SELECT %s%s WHERE %s ORDER BY i.created_at DESC LIMIT $%d OFFSET $%d`, invitationSelectCols, invitationJoins, whereStr, idx, idx+1), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []systemdomain.DeptInvitation
	for rows.Next() {
		inv, err := scanInvitation(rows)
		if err != nil { return nil, err }
		items = append(items, *inv)
	}
	if items == nil { items = []systemdomain.DeptInvitation{} }
	return &systemdomain.InvitationListResult{Items: items, Page: page, Limit: limit, Total: total, TotalPages: int(math.Ceil(float64(total) / float64(limit)))}, nil
}

func (r *Repository) AcceptDeptInvitation(ctx context.Context, invitationID, inviteeID int64) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil { return err }
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE department_invitations SET status = 'accepted', responded_at = NOW() WHERE id = $1 AND invitee_id = $2 AND status = 'pending'`, invitationID, inviteeID)
	if err != nil { return err }
	if tag.RowsAffected() == 0 { return apperrors.New(40461, http.StatusNotFound, "邀请不存在或已处理") }
	var deptID int64
	var isLeader bool
	if err := tx.QueryRow(ctx, `SELECT department_id, is_leader FROM department_invitations WHERE id = $1`, invitationID).Scan(&deptID, &isLeader); err != nil { return err }
	if _, err = tx.Exec(ctx, `INSERT INTO department_members (department_id, admin_id, is_leader, joined_at) VALUES ($1, $2, $3, NOW()) ON CONFLICT (department_id, admin_id) DO UPDATE SET is_leader = EXCLUDED.is_leader`, deptID, inviteeID, isLeader); err != nil { return err }
	return tx.Commit(ctx)
}

func (r *Repository) RejectDeptInvitation(ctx context.Context, invitationID, inviteeID int64) error {
	tag, err := r.pool.Exec(ctx, `UPDATE department_invitations SET status = 'rejected', responded_at = NOW() WHERE id = $1 AND invitee_id = $2 AND status = 'pending'`, invitationID, inviteeID)
	if err != nil { return err }
	if tag.RowsAffected() == 0 { return apperrors.New(40461, http.StatusNotFound, "邀请不存在或已处理") }
	return nil
}

func (r *Repository) CancelDeptInvitation(ctx context.Context, invitationID, inviterID int64) error {
	tag, err := r.pool.Exec(ctx, `UPDATE department_invitations SET status = 'cancelled', responded_at = NOW() WHERE id = $1 AND inviter_id = $2 AND status = 'pending'`, invitationID, inviterID)
	if err != nil { return err }
	if tag.RowsAffected() == 0 { return apperrors.New(40461, http.StatusNotFound, "邀请不存在或无权取消") }
	return nil
}

func (r *Repository) ExpirePendingInvitations(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE department_invitations SET status = 'expired' WHERE status = 'pending' AND expires_at < NOW()`)
	if err != nil { return 0, err }
	return tag.RowsAffected(), nil
}

func (r *Repository) CountPendingInvitations(ctx context.Context, adminID int64) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM department_invitations WHERE invitee_id = $1 AND status = 'pending' AND expires_at > $2`, adminID, time.Now()).Scan(&count)
	return count, err
}

// ── 岗位 CRUD ──

func (r *Repository) CreatePosition(ctx context.Context, input systemdomain.CreatePositionInput) (*systemdomain.Position, error) {
	var p systemdomain.Position
	err := r.pool.QueryRow(ctx,
		`INSERT INTO positions (org_id, name, code, description, level) VALUES ($1,$2,$3,$4,$5)
		 RETURNING id, org_id, name, code, level, COALESCE(description,''), created_at`,
		input.OrgID, input.Name, input.Code, input.Description, input.Level,
	).Scan(&p.ID, &p.OrgID, &p.Name, &p.Code, &p.Level, &p.Description, &p.CreatedAt)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, apperrors.New(40972, http.StatusConflict, "岗位代码已存在")
		}
		return nil, err
	}
	return &p, nil
}

func (r *Repository) ListPositions(ctx context.Context, orgID int64) ([]systemdomain.Position, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, org_id, name, code, level, COALESCE(description,''), created_at FROM positions WHERE org_id = $1 ORDER BY level, id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []systemdomain.Position
	for rows.Next() {
		var p systemdomain.Position
		if err := rows.Scan(&p.ID, &p.OrgID, &p.Name, &p.Code, &p.Level, &p.Description, &p.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, p)
	}
	return items, rows.Err()
}

func (r *Repository) UpdatePosition(ctx context.Context, id int64, name, code, description string, level int) (*systemdomain.Position, error) {
	var p systemdomain.Position
	err := r.pool.QueryRow(ctx,
		`UPDATE positions SET name = $2, code = $3, description = $4, level = $5
		 WHERE id = $1 RETURNING id, org_id, name, code, level, COALESCE(description,''), created_at`,
		id, name, code, description, level,
	).Scan(&p.ID, &p.OrgID, &p.Name, &p.Code, &p.Level, &p.Description, &p.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apperrors.New(40473, http.StatusNotFound, "岗位不存在")
		}
		if isDuplicateKeyError(err) {
			return nil, apperrors.New(40972, http.StatusConflict, "岗位代码已存在")
		}
		return nil, err
	}
	return &p, nil
}

func (r *Repository) DeletePosition(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM positions WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperrors.New(40473, http.StatusNotFound, "岗位不存在")
	}
	return nil
}

// ── 成员增强 ──

func (r *Repository) UpdateMemberPosition(ctx context.Context, deptID, adminID int64, positionID *int64, jobTitle string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE department_members SET position_id = $3, job_title = $4 WHERE department_id = $1 AND admin_id = $2`,
		deptID, adminID, positionID, jobTitle)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperrors.New(40474, http.StatusNotFound, "成员不存在")
	}
	return nil
}

func (r *Repository) SetMemberReporting(ctx context.Context, deptID, adminID, reportingTo int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE department_members SET reporting_to = $3 WHERE department_id = $1 AND admin_id = $2`,
		deptID, adminID, reportingTo)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperrors.New(40474, http.StatusNotFound, "成员不存在")
	}
	return nil
}

func (r *Repository) ClearMemberReporting(ctx context.Context, deptID, adminID int64) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE department_members SET reporting_to = NULL WHERE department_id = $1 AND admin_id = $2`,
		deptID, adminID)
	return err
}

func (r *Repository) SetMemberDelegate(ctx context.Context, deptID, adminID, delegateTo int64, expiresAt *time.Time) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE department_members SET delegate_to = $3, delegate_expires_at = $4 WHERE department_id = $1 AND admin_id = $2`,
		deptID, adminID, delegateTo, expiresAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperrors.New(40474, http.StatusNotFound, "成员不存在")
	}
	return nil
}

func (r *Repository) ClearMemberDelegate(ctx context.Context, deptID, adminID int64) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE department_members SET delegate_to = NULL, delegate_expires_at = NULL WHERE department_id = $1 AND admin_id = $2`,
		deptID, adminID)
	return err
}

func (r *Repository) ClearExpiredDelegates(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE department_members SET delegate_to = NULL, delegate_expires_at = NULL WHERE delegate_expires_at IS NOT NULL AND delegate_expires_at < NOW()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (r *Repository) GetReportingChain(ctx context.Context, deptID, adminID int64) ([]systemdomain.ReportingChainNode, error) {
	rows, err := r.pool.Query(ctx, `WITH RECURSIVE chain AS (
		SELECT dm.admin_id, a.account, a.display_name, COALESCE(dm.job_title,'') AS job_title, 0 AS depth, dm.reporting_to
		FROM department_members dm JOIN admin_accounts a ON a.id = dm.admin_id
		WHERE dm.department_id = $1 AND dm.admin_id = $2
		UNION ALL
		SELECT dm2.admin_id, a2.account, a2.display_name, COALESCE(dm2.job_title,''), chain.depth + 1, dm2.reporting_to
		FROM chain
		JOIN department_members dm2 ON dm2.department_id = $1 AND dm2.admin_id = chain.reporting_to
		JOIN admin_accounts a2 ON a2.id = dm2.admin_id
		WHERE chain.reporting_to IS NOT NULL AND chain.depth < 20
	) SELECT admin_id, account, display_name, job_title, depth FROM chain ORDER BY depth`, deptID, adminID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []systemdomain.ReportingChainNode
	for rows.Next() {
		var n systemdomain.ReportingChainNode
		if err := rows.Scan(&n.AdminID, &n.Account, &n.DisplayName, &n.JobTitle, &n.Depth); err != nil {
			return nil, err
		}
		items = append(items, n)
	}
	return items, rows.Err()
}

func (r *Repository) BatchCreateInvitations(ctx context.Context, deptID, inviterID int64, adminIDs []int64, message string) ([]systemdomain.DeptInvitation, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var results []systemdomain.DeptInvitation
	for _, aid := range adminIDs {
		var id int64
		err := tx.QueryRow(ctx,
			`INSERT INTO department_invitations (department_id, inviter_id, invitee_id, is_leader, message, expires_at)
			 VALUES ($1, $2, $3, FALSE, $4, NOW() + INTERVAL '7 days')
			 ON CONFLICT DO NOTHING RETURNING id`,
			deptID, inviterID, aid, message,
		).Scan(&id)
		if err != nil {
			if err == pgx.ErrNoRows {
				continue // 已存在待处理邀请，跳过
			}
			return nil, err
		}
		// 查询完整邀请信息
		inv, err := scanInvitation(tx.QueryRow(ctx, `SELECT `+invitationSelectCols+invitationJoins+` WHERE i.id = $1`, id))
		if err != nil {
			return nil, err
		}
		if inv != nil {
			results = append(results, *inv)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return results, nil
}
