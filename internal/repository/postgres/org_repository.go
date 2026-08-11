package postgres

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	orgdomain "aegis/internal/domain/organization"
	apperrors "aegis/pkg/errors"

	"github.com/jackc/pgx/v5"
)

// ── 错误码 ──
//
// 组织域统一占用 404x1 / 409x1 段，与既有段位不重叠。

var (
	ErrOrgNotFound    = apperrors.New(40471, http.StatusNotFound, "组织不存在")
	ErrDeptNotFound   = apperrors.New(40472, http.StatusNotFound, "部门不存在")
	ErrPositionNoFind = apperrors.New(40473, http.StatusNotFound, "岗位不存在")
	ErrMemberNotFound = apperrors.New(40474, http.StatusNotFound, "成员不存在")
	ErrRoleNotFound   = apperrors.New(40475, http.StatusNotFound, "组织角色不存在")
	ErrInviteNotFound = apperrors.New(40461, http.StatusNotFound, "邀请不存在或已处理")
)

// ── 列定义 ──

const orgSelectColumns = `o.id, o.uuid::text, o.parent_id, COALESCE(p.uuid::text, ''), COALESCE(p.name, ''),
	o.path, o.depth, o.name, o.code, o.kind, o.description, o.logo_url, o.status,
	o.owner_id, COALESCE(NULLIF(ow.display_name, ''), ow.account, ''), COALESCE(ow.account, ''),
	o.contact_name, o.contact_email, o.contact_phone, o.industry, o.region,
	o.member_limit, o.dept_limit, o.app_limit, o.expires_at, o.settings,
	o.created_by, o.created_at, o.updated_at,
	(SELECT COUNT(*) FROM org_members m WHERE m.org_id = o.id AND m.status <> 'left'),
	(SELECT COUNT(*) FROM departments d WHERE d.org_id = o.id),
	(SELECT COUNT(*) FROM apps a WHERE a.org_id = o.id),
	(SELECT COUNT(*) FROM organizations c WHERE c.parent_id = o.id)`

const orgFromClause = ` FROM organizations o
	LEFT JOIN organizations p ON p.id = o.parent_id
	LEFT JOIN admin_accounts ow ON ow.id = o.owner_id`

func scanOrganization(row pgx.Row) (*orgdomain.Organization, error) {
	var o orgdomain.Organization
	err := row.Scan(
		&o.ID, &o.UUID, &o.ParentID, &o.ParentUUID, &o.ParentName,
		&o.Path, &o.Depth, &o.Name, &o.Code, &o.Kind, &o.Description, &o.LogoURL, &o.Status,
		&o.OwnerID, &o.OwnerName, &o.OwnerAccount,
		&o.Contact.Name, &o.Contact.Email, &o.Contact.Phone, &o.Industry, &o.Region,
		&o.Quota.MemberLimit, &o.Quota.DeptLimit, &o.Quota.AppLimit, &o.ExpiresAt, &o.Settings,
		&o.CreatedBy, &o.CreatedAt, &o.UpdatedAt,
		&o.Stats.MemberCount, &o.Stats.DeptCount, &o.Stats.AppCount, &o.Stats.ChildCount,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &o, nil
}

// ── 组织读取 ──

// ResolveOrgID 把对外 UUID 解析为内部主键。所有 handler 拿到的都是 UUID，
// 内部 JOIN 一律用自增 ID —— 这个函数是两者之间唯一的转换口。
func (r *Repository) ResolveOrgID(ctx context.Context, orgUUID string) (int64, error) {
	if strings.TrimSpace(orgUUID) == "" {
		return 0, ErrOrgNotFound
	}
	var id int64
	err := r.pool.QueryRow(ctx, `SELECT id FROM organizations WHERE uuid = $1::uuid`, orgUUID).Scan(&id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, ErrOrgNotFound
		}
		if isInvalidUUIDError(err) {
			return 0, ErrOrgNotFound
		}
		return 0, err
	}
	return id, nil
}

// GetOrganization 按 UUID 读组织
func (r *Repository) GetOrganization(ctx context.Context, orgUUID string) (*orgdomain.Organization, error) {
	if !isUUIDLike(orgUUID) {
		return nil, nil
	}
	return scanOrganization(r.pool.QueryRow(ctx,
		`SELECT `+orgSelectColumns+orgFromClause+` WHERE o.uuid = $1::uuid`, orgUUID))
}

// GetOrganizationByID 按内部主键读组织（服务层内部流转用）
func (r *Repository) GetOrganizationByID(ctx context.Context, orgID int64) (*orgdomain.Organization, error) {
	return scanOrganization(r.pool.QueryRow(ctx,
		`SELECT `+orgSelectColumns+orgFromClause+` WHERE o.id = $1`, orgID))
}

// ListOrganizations 组织列表。visibleTo > 0 时只返回该管理员所属的组织
// （超管传 0 表示看全部），过滤在 SQL 层完成而非查完再筛。
func (r *Repository) ListOrganizations(ctx context.Context, q orgdomain.OrgListQuery, visibleTo int64) (*orgdomain.Page[orgdomain.Organization], error) {
	where := []string{"1=1"}
	args := []any{}
	idx := 1

	if visibleTo > 0 {
		where = append(where, fmt.Sprintf(`EXISTS (SELECT 1 FROM org_members vm
			WHERE vm.org_id = o.id AND vm.admin_id = $%d AND vm.status = 'active')`, idx))
		args = append(args, visibleTo)
		idx++
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		where = append(where, fmt.Sprintf("(o.name ILIKE $%d OR o.code ILIKE $%d)", idx, idx))
		args = append(args, "%"+kw+"%")
		idx++
	}
	if q.Status != "" {
		where = append(where, fmt.Sprintf("o.status = $%d", idx))
		args = append(args, q.Status)
		idx++
	}
	if q.Kind != "" {
		where = append(where, fmt.Sprintf("o.kind = $%d", idx))
		args = append(args, q.Kind)
		idx++
	}
	if q.ParentUUID != "" && isUUIDLike(q.ParentUUID) {
		where = append(where, fmt.Sprintf("o.parent_id = (SELECT id FROM organizations WHERE uuid = $%d::uuid)", idx))
		args = append(args, q.ParentUUID)
		idx++
	}
	whereSQL := strings.Join(where, " AND ")

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+orgFromClause+` WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, err
	}

	page, limit := normalizeOrgPaging(q.Page, q.Limit)
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(
		`SELECT %s%s WHERE %s ORDER BY o.path, o.id LIMIT $%d OFFSET $%d`,
		orgSelectColumns, orgFromClause, whereSQL, idx, idx+1), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]orgdomain.Organization, 0, limit)
	for rows.Next() {
		o, err := scanOrganization(rows)
		if err != nil {
			return nil, err
		}
		if o != nil {
			items = append(items, *o)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &orgdomain.Page[orgdomain.Organization]{
		Items: items, Page: page, Limit: limit, Total: total,
		TotalPages: int(math.Ceil(float64(total) / float64(limit))),
	}, nil
}

// ListAllOrganizations 不分页取全部（构建组织层级树用）
func (r *Repository) ListAllOrganizations(ctx context.Context, visibleTo int64) ([]orgdomain.Organization, error) {
	query := `SELECT ` + orgSelectColumns + orgFromClause + ` ORDER BY o.path, o.id`
	args := []any{}
	if visibleTo > 0 {
		query = `SELECT ` + orgSelectColumns + orgFromClause + `
			WHERE EXISTS (SELECT 1 FROM org_members vm WHERE vm.org_id = o.id AND vm.admin_id = $1 AND vm.status = 'active')
			ORDER BY o.path, o.id`
		args = append(args, visibleTo)
	}
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []orgdomain.Organization
	for rows.Next() {
		o, err := scanOrganization(rows)
		if err != nil {
			return nil, err
		}
		if o != nil {
			items = append(items, *o)
		}
	}
	return items, rows.Err()
}

// ── 组织写入 ──

// CreateOrganization 建组织：写主记录 → 回填物化路径 → 落所有者为 owner 成员。
// 三步必须同事务，否则会出现「组织建好了但没有主人」的孤儿组织。
func (r *Repository) CreateOrganization(ctx context.Context, input orgdomain.CreateOrgInput, ownerID, createdBy int64) (*orgdomain.Organization, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var parentID *int64
	parentPath := ""
	parentDepth := -1
	if input.ParentUUID != "" {
		var pid int64
		var ppath string
		var pdepth int
		err := tx.QueryRow(ctx, `SELECT id, path, depth FROM organizations WHERE uuid = $1::uuid`, input.ParentUUID).
			Scan(&pid, &ppath, &pdepth)
		if err != nil {
			if err == pgx.ErrNoRows {
				return nil, apperrors.New(40476, http.StatusNotFound, "上级组织不存在")
			}
			return nil, err
		}
		parentID = &pid
		parentPath = ppath
		parentDepth = pdepth
	}

	settings := input.Settings
	if settings == nil {
		settings = map[string]any{}
	}

	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO organizations
		(name, code, kind, description, logo_url, parent_id, depth, path,
		 owner_id, contact_name, contact_email, contact_phone, industry, region,
		 member_limit, dept_limit, app_limit, expires_at, settings, created_by, updated_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'',$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$19)
		RETURNING id`,
		input.Name, input.Code, defaultString(input.Kind, "enterprise"), input.Description, input.LogoURL,
		parentID, parentDepth+1,
		nullableID(ownerID), input.Contact.Name, input.Contact.Email, input.Contact.Phone,
		input.Industry, input.Region,
		input.Quota.MemberLimit, input.Quota.DeptLimit, input.Quota.AppLimit,
		input.ExpiresAt, settings, nullableID(createdBy),
	).Scan(&id)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, apperrors.New(40970, http.StatusConflict, "组织代码已存在")
		}
		return nil, err
	}

	if _, err := tx.Exec(ctx, `UPDATE organizations SET path = $2 WHERE id = $1`,
		id, orgdomain.MaterializePath(parentPath, id)); err != nil {
		return nil, err
	}

	if ownerID > 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO org_members (org_id, admin_id, org_role, invited_by)
			VALUES ($1, $2, 'owner', $3)
			ON CONFLICT (org_id, admin_id) DO UPDATE SET org_role = 'owner', status = 'active'`,
			id, ownerID, nullableID(createdBy)); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetOrganizationByID(ctx, id)
}

// UpdateOrganization 更新组织资料
func (r *Repository) UpdateOrganization(ctx context.Context, orgID int64, input orgdomain.UpdateOrgInput, updatedBy int64) (*orgdomain.Organization, error) {
	sets := []string{"updated_at = NOW()"}
	args := []any{orgID}
	idx := 2

	addSet := func(column string, value any) {
		sets = append(sets, fmt.Sprintf("%s = $%d", column, idx))
		args = append(args, value)
		idx++
	}
	if input.Name != nil {
		addSet("name", *input.Name)
	}
	if input.Code != nil {
		addSet("code", *input.Code)
	}
	if input.Kind != nil {
		addSet("kind", *input.Kind)
	}
	if input.Description != nil {
		addSet("description", *input.Description)
	}
	if input.LogoURL != nil {
		addSet("logo_url", *input.LogoURL)
	}
	if input.Status != nil {
		addSet("status", *input.Status)
	}
	if input.Contact != nil {
		addSet("contact_name", input.Contact.Name)
		addSet("contact_email", input.Contact.Email)
		addSet("contact_phone", input.Contact.Phone)
	}
	if input.Industry != nil {
		addSet("industry", *input.Industry)
	}
	if input.Region != nil {
		addSet("region", *input.Region)
	}
	if input.Quota != nil {
		addSet("member_limit", input.Quota.MemberLimit)
		addSet("dept_limit", input.Quota.DeptLimit)
		addSet("app_limit", input.Quota.AppLimit)
	}
	if input.ClearExpiry {
		sets = append(sets, "expires_at = NULL")
	} else if input.ExpiresAt != nil {
		addSet("expires_at", *input.ExpiresAt)
	}
	if input.Settings != nil {
		addSet("settings", input.Settings)
	}
	if updatedBy > 0 {
		addSet("updated_by", updatedBy)
	}

	tag, err := r.pool.Exec(ctx, fmt.Sprintf(`UPDATE organizations SET %s WHERE id = $1`,
		strings.Join(sets, ", ")), args...)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, apperrors.New(40970, http.StatusConflict, "组织代码已存在")
		}
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrOrgNotFound
	}
	return r.GetOrganizationByID(ctx, orgID)
}

// TransferOrganizationOwner 转让所有者：新主人必须已是组织成员，
// 旧主人降为 admin（而非踢出），避免转让即失联。
func (r *Repository) TransferOrganizationOwner(ctx context.Context, orgID, newOwnerID int64) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM org_members WHERE org_id = $1 AND admin_id = $2 AND status = 'active')`,
		orgID, newOwnerID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return apperrors.New(40045, http.StatusBadRequest, "新所有者必须是该组织的在职成员")
	}

	// 先把旧 owner 降级，再提升新 owner —— 反过来会撞上「每组织至多一个 owner」的唯一索引
	if _, err := tx.Exec(ctx, `UPDATE org_members SET org_role = 'admin', updated_at = NOW()
		WHERE org_id = $1 AND org_role = 'owner' AND admin_id <> $2`, orgID, newOwnerID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE org_members SET org_role = 'owner', updated_at = NOW()
		WHERE org_id = $1 AND admin_id = $2`, orgID, newOwnerID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE organizations SET owner_id = $2, updated_at = NOW() WHERE id = $1`,
		orgID, newOwnerID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// DeleteOrganization 删除组织。有子组织时拒绝（外键 RESTRICT 也会拦，
// 这里提前给出可读的错误）。
func (r *Repository) DeleteOrganization(ctx context.Context, orgID int64) error {
	var childCount int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM organizations WHERE parent_id = $1`, orgID).Scan(&childCount); err != nil {
		return err
	}
	if childCount > 0 {
		return apperrors.New(40940, http.StatusConflict, fmt.Sprintf("该组织下还有 %d 个下级组织，请先处理", childCount))
	}
	tag, err := r.pool.Exec(ctx, `DELETE FROM organizations WHERE id = $1`, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrOrgNotFound
	}
	return nil
}

// CountOrgMembers 组织在职成员数（配额校验用）
func (r *Repository) CountOrgMembers(ctx context.Context, orgID int64) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM org_members WHERE org_id = $1 AND status <> 'left'`, orgID).Scan(&n)
	return n, err
}

// CountOrgDepartments 组织部门数（配额校验用）
func (r *Repository) CountOrgDepartments(ctx context.Context, orgID int64) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM departments WHERE org_id = $1`, orgID).Scan(&n)
	return n, err
}

// ── 组织概览 ──

// GetOrgOverview 一次取齐控制台首屏所需的全部统计
func (r *Repository) GetOrgOverview(ctx context.Context, orgID int64) (*orgdomain.OverviewStats, map[string]int, error) {
	var s orgdomain.OverviewStats
	err := r.pool.QueryRow(ctx, `SELECT
		(SELECT COUNT(*) FROM org_members WHERE org_id = $1 AND status <> 'left'),
		(SELECT COUNT(*) FROM org_members WHERE org_id = $1 AND status = 'active'),
		(SELECT COUNT(*) FROM org_members WHERE org_id = $1 AND status = 'suspended'),
		(SELECT COUNT(*) FROM departments WHERE org_id = $1),
		(SELECT COALESCE(MAX(depth), 0) + 1 FROM departments WHERE org_id = $1),
		(SELECT COUNT(*) FROM positions WHERE org_id = $1),
		(SELECT COUNT(*) FROM apps WHERE org_id = $1),
		(SELECT COUNT(*) FROM department_invitations WHERE org_id = $1 AND status = 'pending' AND expires_at > NOW()),
		(SELECT COUNT(*) FROM org_members m WHERE m.org_id = $1 AND m.status <> 'left'
			AND NOT EXISTS (SELECT 1 FROM department_members dm WHERE dm.org_id = $1 AND dm.admin_id = m.admin_id)),
		(SELECT COUNT(*) FROM organizations WHERE parent_id = $1)`, orgID).
		Scan(&s.MemberTotal, &s.MemberActive, &s.MemberSuspended, &s.DeptTotal, &s.DeptMaxDepth,
			&s.PositionTotal, &s.AppTotal, &s.PendingInvites, &s.Unassigned, &s.ChildOrgs)
	if err != nil {
		return nil, nil, err
	}
	if s.DeptTotal == 0 {
		s.DeptMaxDepth = 0
	}

	rows, err := r.pool.Query(ctx, `SELECT org_role, COUNT(*) FROM org_members
		WHERE org_id = $1 AND status <> 'left' GROUP BY org_role`, orgID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	breakdown := map[string]int{}
	for rows.Next() {
		var role string
		var n int
		if err := rows.Scan(&role, &n); err != nil {
			return nil, nil, err
		}
		breakdown[role] = n
	}
	return &s, breakdown, rows.Err()
}

// ── 活动留痕 ──

// RecordOrgActivity 写组织操作日志。留痕失败不应反噬业务，
// 调用方一律忽略返回的错误（只记日志）。
func (r *Repository) RecordOrgActivity(ctx context.Context, orgID int64, actorID int64, action, targetType, targetID, summary string, detail map[string]any) error {
	if detail == nil {
		detail = map[string]any{}
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO org_activity_logs
		(org_id, actor_id, action, target_type, target_id, summary, detail)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		orgID, nullableID(actorID), action, targetType, targetID, summary, detail)
	return err
}

// ListOrgActivity 组织操作日志
func (r *Repository) ListOrgActivity(ctx context.Context, orgID int64, action string, page, limit int) (*orgdomain.Page[orgdomain.ActivityLog], error) {
	where := []string{"l.org_id = $1"}
	args := []any{orgID}
	idx := 2
	if action != "" {
		where = append(where, fmt.Sprintf("l.action = $%d", idx))
		args = append(args, action)
		idx++
	}
	whereSQL := strings.Join(where, " AND ")

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM org_activity_logs l WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, err
	}

	page, limit = normalizeOrgPaging(page, limit)
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`SELECT o.uuid::text, l.actor_id,
		COALESCE(NULLIF(a.display_name, ''), a.account, ''), l.action, l.target_type, l.target_id,
		l.summary, l.detail, l.created_at
		FROM org_activity_logs l
		JOIN organizations o ON o.id = l.org_id
		LEFT JOIN admin_accounts a ON a.id = l.actor_id
		WHERE %s ORDER BY l.created_at DESC LIMIT $%d OFFSET $%d`, whereSQL, idx, idx+1), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]orgdomain.ActivityLog, 0, limit)
	for rows.Next() {
		var l orgdomain.ActivityLog
		if err := rows.Scan(&l.OrgUUID, &l.ActorID, &l.ActorName, &l.Action,
			&l.TargetType, &l.TargetID, &l.Summary, &l.Detail, &l.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, l)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &orgdomain.Page[orgdomain.ActivityLog]{
		Items: items, Page: page, Limit: limit, Total: total,
		TotalPages: int(math.Ceil(float64(total) / float64(limit))),
	}, nil
}

// ── 应用绑定 ──

// ListOrgApps 组织的应用：owned 来自 apps.org_id（归属），
// 其余来自 org_app_bindings（跨组织授权访问）
func (r *Repository) ListOrgApps(ctx context.Context, orgID int64) ([]orgdomain.AppBinding, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT o.uuid::text, a.id, a.name, COALESCE(a.app_key, ''), TRUE, a.created_at
		  FROM apps a JOIN organizations o ON o.id = a.org_id
		 WHERE a.org_id = $1
		UNION ALL
		SELECT o.uuid::text, a.id, a.name, COALESCE(a.app_key, ''), FALSE, b.created_at
		  FROM org_app_bindings b
		  JOIN apps a ON a.id = b.app_id
		  JOIN organizations o ON o.id = b.org_id
		 WHERE b.org_id = $1 AND (a.org_id IS NULL OR a.org_id <> $1)
		ORDER BY 5 DESC, 2`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]orgdomain.AppBinding, 0)
	for rows.Next() {
		var b orgdomain.AppBinding
		if err := rows.Scan(&b.OrgUUID, &b.AppID, &b.AppName, &b.AppKey, &b.Owned, &b.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, b)
	}
	return items, rows.Err()
}

// BindOrgApp 绑定应用。owned 为 true 时改写 apps.org_id（转移归属），
// 否则只登记一条授权访问记录。
func (r *Repository) BindOrgApp(ctx context.Context, orgID, appID int64, owned bool) error {
	if owned {
		tag, err := r.pool.Exec(ctx, `UPDATE apps SET org_id = $2, updated_at = NOW() WHERE id = $1`, appID, orgID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return apperrors.New(40477, http.StatusNotFound, "应用不存在")
		}
		return nil
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO org_app_bindings (org_id, app_id) VALUES ($1,$2)
		ON CONFLICT (org_id, app_id) DO NOTHING`, orgID, appID)
	return err
}

// UnbindOrgApp 解绑：归属与授权两条链路都清掉
func (r *Repository) UnbindOrgApp(ctx context.Context, orgID, appID int64) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `UPDATE apps SET org_id = NULL, updated_at = NOW() WHERE id = $1 AND org_id = $2`, appID, orgID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM org_app_bindings WHERE org_id = $1 AND app_id = $2`, orgID, appID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ── 内部辅助 ──

func normalizeOrgPaging(page, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	return page, limit
}

func defaultString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func nullableID(id int64) *int64 {
	if id <= 0 {
		return nil
	}
	return &id
}

// isUUIDLike 粗筛 UUID 形状，避免把明显不是 UUID 的串丢给 Postgres 换来一个
// 22P02 语法错误 —— 那会被上层当成 500，而实际语义只是「没找到」。
func isUUIDLike(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
			if !isHex {
				return false
			}
		}
	}
	return true
}

func isInvalidUUIDError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "22P02")
}

// touchOrgUpdatedAt 组织下任意子实体变更后刷新组织的 updated_at
func touchOrgUpdatedAt(ctx context.Context, exec queryExecutor, orgID int64) {
	_, _ = exec.Exec(ctx, `UPDATE organizations SET updated_at = $2 WHERE id = $1`, orgID, time.Now())
}
