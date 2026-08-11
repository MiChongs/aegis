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

// ── 岗位 ──

const positionColumns = `p.id, p.uuid::text, o.uuid::text, p.name, p.code, p.level,
	COALESCE(p.description, ''), p.created_at,
	(SELECT COUNT(*) FROM department_members dm WHERE dm.position_id = p.id)`

const positionFrom = ` FROM positions p JOIN organizations o ON o.id = p.org_id`

func scanPosition(row pgx.Row) (*orgdomain.Position, error) {
	var p orgdomain.Position
	err := row.Scan(&p.ID, &p.UUID, &p.OrgUUID, &p.Name, &p.Code, &p.Level,
		&p.Description, &p.CreatedAt, &p.MemberCount)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// ResolvePositionID 岗位 UUID → 内部主键，强制校验组织归属
func (r *Repository) ResolvePositionID(ctx context.Context, orgID int64, posUUID string) (int64, error) {
	if !isUUIDLike(posUUID) {
		return 0, ErrPositionNoFind
	}
	var id int64
	err := r.pool.QueryRow(ctx, `SELECT id FROM positions WHERE uuid = $1::uuid AND org_id = $2`,
		posUUID, orgID).Scan(&id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, ErrPositionNoFind
		}
		return 0, err
	}
	return id, nil
}

// ListPositions 组织岗位列表
func (r *Repository) ListPositions(ctx context.Context, orgID int64) ([]orgdomain.Position, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+positionColumns+positionFrom+
		` WHERE p.org_id = $1 ORDER BY p.level DESC, p.id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]orgdomain.Position, 0)
	for rows.Next() {
		p, err := scanPosition(rows)
		if err != nil {
			return nil, err
		}
		if p != nil {
			items = append(items, *p)
		}
	}
	return items, rows.Err()
}

// CreatePosition 创建岗位
func (r *Repository) CreatePosition(ctx context.Context, orgID int64, input orgdomain.PositionInput) (*orgdomain.Position, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `INSERT INTO positions (org_id, name, code, level, description)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		orgID, input.Name, input.Code, input.Level, input.Description).Scan(&id)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, apperrors.New(40972, http.StatusConflict, "岗位代码在该组织内已存在")
		}
		return nil, err
	}
	return scanPosition(r.pool.QueryRow(ctx, `SELECT `+positionColumns+positionFrom+` WHERE p.id = $1`, id))
}

// UpdatePosition 更新岗位
func (r *Repository) UpdatePosition(ctx context.Context, orgID, posID int64, input orgdomain.PositionInput) (*orgdomain.Position, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE positions SET name = $3, code = $4, level = $5, description = $6
		WHERE id = $1 AND org_id = $2`, posID, orgID, input.Name, input.Code, input.Level, input.Description)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, apperrors.New(40972, http.StatusConflict, "岗位代码在该组织内已存在")
		}
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrPositionNoFind
	}
	return scanPosition(r.pool.QueryRow(ctx, `SELECT `+positionColumns+positionFrom+` WHERE p.id = $1`, posID))
}

// DeletePosition 删除岗位。持有者的 position_id 由外键 SET NULL 自动解除。
func (r *Repository) DeletePosition(ctx context.Context, orgID, posID int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM positions WHERE id = $1 AND org_id = $2`, posID, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrPositionNoFind
	}
	return nil
}

// ── 邀请 ──

const inviteColumns = `i.id, i.uuid::text, o.uuid::text, o.name,
	COALESCE(d.uuid::text, ''), COALESCE(d.name, ''),
	i.inviter_id, COALESCE(NULLIF(inviter.display_name, ''), inviter.account, ''),
	i.invitee_id, COALESCE(NULLIF(invitee.display_name, ''), invitee.account, ''),
	i.org_role, i.is_leader, i.status, COALESCE(i.message, ''),
	i.responded_at, i.expires_at, i.created_at`

const inviteFrom = ` FROM department_invitations i
	JOIN organizations o ON o.id = i.org_id
	LEFT JOIN departments d ON d.id = i.department_id
	LEFT JOIN admin_accounts inviter ON inviter.id = i.inviter_id
	LEFT JOIN admin_accounts invitee ON invitee.id = i.invitee_id`

func scanInvitation(row pgx.Row) (*orgdomain.Invitation, error) {
	var inv orgdomain.Invitation
	err := row.Scan(&inv.ID, &inv.UUID, &inv.OrgUUID, &inv.OrgName,
		&inv.DeptUUID, &inv.DeptName,
		&inv.InviterID, &inv.InviterName, &inv.InviteeID, &inv.InviteeName,
		&inv.OrgRole, &inv.IsLeader, &inv.Status, &inv.Message,
		&inv.RespondedAt, &inv.ExpiresAt, &inv.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &inv, nil
}

// CreateInvitations 批量发起邀请。已是成员的、已有待处理邀请的一律跳过，
// 返回真正新建的那些，好让调用方只对它们发通知。
func (r *Repository) CreateInvitations(ctx context.Context, orgID int64, input orgdomain.InviteInput, inviterID int64) ([]orgdomain.Invitation, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var deptID *int64
	if input.DeptUUID != "" {
		if !isUUIDLike(input.DeptUUID) {
			return nil, ErrDeptNotFound
		}
		var id int64
		err := tx.QueryRow(ctx, `SELECT id FROM departments WHERE uuid = $1::uuid AND org_id = $2`,
			input.DeptUUID, orgID).Scan(&id)
		if err != nil {
			if err == pgx.ErrNoRows {
				return nil, ErrDeptNotFound
			}
			return nil, err
		}
		deptID = &id
	}

	role := input.OrgRole
	if !orgdomain.IsValidRole(role) || role == orgdomain.RoleOwner {
		role = orgdomain.RoleMember
	}

	created := make([]orgdomain.Invitation, 0, len(input.AdminIDs))
	for _, adminID := range input.AdminIDs {
		if adminID <= 0 || adminID == inviterID {
			continue
		}
		// 已经在组织里且已在目标部门里的，没必要再邀请一次
		var already bool
		if deptID != nil {
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM department_members
				WHERE department_id = $1 AND admin_id = $2)`, *deptID, adminID).Scan(&already); err != nil {
				return nil, err
			}
		} else {
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM org_members
				WHERE org_id = $1 AND admin_id = $2 AND status = 'active')`, orgID, adminID).Scan(&already); err != nil {
				return nil, err
			}
		}
		if already {
			continue
		}

		var id int64
		err := tx.QueryRow(ctx, `INSERT INTO department_invitations
			(org_id, department_id, inviter_id, invitee_id, org_role, is_leader, message, expires_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7, NOW() + INTERVAL '7 days')
			ON CONFLICT DO NOTHING RETURNING id`,
			orgID, deptID, inviterID, adminID, role, input.IsLeader, input.Message).Scan(&id)
		if err != nil {
			if err == pgx.ErrNoRows {
				continue // 已有待处理邀请
			}
			return nil, err
		}
		inv, err := scanInvitation(tx.QueryRow(ctx, `SELECT `+inviteColumns+inviteFrom+` WHERE i.id = $1`, id))
		if err != nil {
			return nil, err
		}
		if inv != nil {
			created = append(created, *inv)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return created, nil
}

// GetInvitation 按 UUID 读邀请
func (r *Repository) GetInvitation(ctx context.Context, inviteUUID string) (*orgdomain.Invitation, error) {
	if !isUUIDLike(inviteUUID) {
		return nil, nil
	}
	return scanInvitation(r.pool.QueryRow(ctx, `SELECT `+inviteColumns+inviteFrom+` WHERE i.uuid = $1::uuid`, inviteUUID))
}

// ListInvitations 邀请列表（收到 / 发出）
func (r *Repository) ListInvitations(ctx context.Context, q orgdomain.InvitationQuery) (*orgdomain.Page[orgdomain.Invitation], error) {
	where := []string{}
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
	if q.OrgUUID != "" && isUUIDLike(q.OrgUUID) {
		where = append(where, fmt.Sprintf("o.uuid = $%d::uuid", idx))
		args = append(args, q.OrgUUID)
		idx++
	}
	whereSQL := strings.Join(where, " AND ")

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+inviteFrom+` WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, err
	}

	page, limit := normalizeOrgPaging(q.Page, q.Limit)
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`SELECT %s%s WHERE %s ORDER BY i.created_at DESC LIMIT $%d OFFSET $%d`,
		inviteColumns, inviteFrom, whereSQL, idx, idx+1), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]orgdomain.Invitation, 0, limit)
	for rows.Next() {
		inv, err := scanInvitation(rows)
		if err != nil {
			return nil, err
		}
		if inv != nil {
			items = append(items, *inv)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &orgdomain.Page[orgdomain.Invitation]{
		Items: items, Page: page, Limit: limit, Total: total,
		TotalPages: int(math.Ceil(float64(total) / float64(limit))),
	}, nil
}

// AcceptInvitation 接受邀请：状态推进 + 落组织籍 + 可选落部门，同一事务。
func (r *Repository) AcceptInvitation(ctx context.Context, inviteUUID string, inviteeID int64) (*orgdomain.Invitation, error) {
	if !isUUIDLike(inviteUUID) {
		return nil, ErrInviteNotFound
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var id, orgID int64
	var deptID *int64
	var orgRole string
	var isLeader bool
	err = tx.QueryRow(ctx, `UPDATE department_invitations SET status = 'accepted', responded_at = NOW()
		WHERE uuid = $1::uuid AND invitee_id = $2 AND status = 'pending' AND expires_at > NOW()
		RETURNING id, org_id, department_id, org_role, is_leader`,
		inviteUUID, inviteeID).Scan(&id, &orgID, &deptID, &orgRole, &isLeader)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrInviteNotFound
		}
		return nil, err
	}

	if _, err := tx.Exec(ctx, `INSERT INTO org_members (org_id, admin_id, org_role)
		VALUES ($1,$2,$3)
		ON CONFLICT (org_id, admin_id) DO UPDATE SET status = 'active', left_at = NULL, updated_at = NOW()`,
		orgID, inviteeID, orgRole); err != nil {
		return nil, err
	}

	if deptID != nil {
		if _, err := tx.Exec(ctx, `INSERT INTO department_members (department_id, org_id, admin_id, is_leader)
			VALUES ($1,$2,$3,$4)
			ON CONFLICT (department_id, admin_id) DO UPDATE SET is_leader = EXCLUDED.is_leader`,
			*deptID, orgID, inviteeID, isLeader); err != nil {
			return nil, err
		}
		if isLeader {
			if _, err := tx.Exec(ctx, `UPDATE departments SET leader_id = $2, updated_at = NOW() WHERE id = $1`,
				*deptID, inviteeID); err != nil {
				return nil, err
			}
		}
	}

	inv, err := scanInvitation(tx.QueryRow(ctx, `SELECT `+inviteColumns+inviteFrom+` WHERE i.id = $1`, id))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return inv, nil
}

// RespondInvitation 拒绝 / 取消邀请。action 为 rejected 时校验被邀人，
// cancelled 时校验邀请人 —— 两者都不能替对方做决定。
func (r *Repository) RespondInvitation(ctx context.Context, inviteUUID string, adminID int64, action string) (*orgdomain.Invitation, error) {
	if !isUUIDLike(inviteUUID) {
		return nil, ErrInviteNotFound
	}
	actorColumn := "invitee_id"
	if action == "cancelled" {
		actorColumn = "inviter_id"
	}
	var id int64
	err := r.pool.QueryRow(ctx, fmt.Sprintf(
		`UPDATE department_invitations SET status = $3, responded_at = NOW()
		 WHERE uuid = $1::uuid AND %s = $2 AND status = 'pending' RETURNING id`, actorColumn),
		inviteUUID, adminID, action).Scan(&id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrInviteNotFound
		}
		return nil, err
	}
	return scanInvitation(r.pool.QueryRow(ctx, `SELECT `+inviteColumns+inviteFrom+` WHERE i.id = $1`, id))
}

// CountPendingInvitations 待处理邀请数（顶栏角标用）
func (r *Repository) CountPendingInvitations(ctx context.Context, adminID int64) (int64, error) {
	var count int64
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM department_invitations
		WHERE invitee_id = $1 AND status = 'pending' AND expires_at > NOW()`, adminID).Scan(&count)
	return count, err
}

// ExpirePendingInvitations 过期清理（Worker 定时调用）
func (r *Repository) ExpirePendingInvitations(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE department_invitations SET status = 'expired'
		WHERE status = 'pending' AND expires_at < NOW()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
