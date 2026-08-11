package postgres

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	orgdomain "aegis/internal/domain/organization"
	apperrors "aegis/pkg/errors"

	"github.com/jackc/pgx/v5"
)

// 部门树同时维护「物化路径」与「闭包表」：
//   - path 负责子树范围查询（LIKE 前缀）与环检测，一次比较搞定
//   - closure 负责带 depth 的关系查询（直接下级 / 祖先链 / 第 N 层后代）
// 两者必须在同一事务里一起改，任何一边漏更新都会让树的两个视图互相打架。

const deptSelectColumns = `d.id, d.uuid::text, d.org_id, o.uuid::text, d.parent_id,
	COALESCE(pd.uuid::text, ''), d.path, d.depth, d.name, d.code, d.kind, d.description,
	d.sort_order, d.leader_id, COALESCE(NULLIF(la.display_name, ''), la.account, ''),
	d.status, d.member_limit, d.settings, d.created_at, d.updated_at,
	(SELECT COUNT(*) FROM department_members dm WHERE dm.department_id = d.id),
	(SELECT COUNT(DISTINCT dm2.admin_id) FROM department_closure c
		JOIN department_members dm2 ON dm2.department_id = c.descendant_id
		WHERE c.ancestor_id = d.id),
	(SELECT COUNT(*) FROM departments cd WHERE cd.parent_id = d.id)`

const deptFromClause = ` FROM departments d
	JOIN organizations o ON o.id = d.org_id
	LEFT JOIN departments pd ON pd.id = d.parent_id
	LEFT JOIN admin_accounts la ON la.id = d.leader_id`

func scanDepartment(row pgx.Row) (*orgdomain.Department, error) {
	var d orgdomain.Department
	err := row.Scan(
		&d.ID, &d.UUID, &d.OrgID, &d.OrgUUID, &d.ParentID, &d.ParentUUID,
		&d.Path, &d.Depth, &d.Name, &d.Code, &d.Kind, &d.Description,
		&d.SortOrder, &d.LeaderID, &d.LeaderName,
		&d.Status, &d.MemberLimit, &d.Settings, &d.CreatedAt, &d.UpdatedAt,
		&d.MemberCount, &d.TotalMemberCount, &d.ChildCount,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

// ── 读取 ──

// ResolveDeptID 把部门 UUID 解析为内部主键，并强制校验它属于 orgID。
//
// 这是组织隔离的关键闸门：只要所有部门写操作都从这里拿 ID，
// 「拿 A 组织的会话去改 B 组织的部门」在服务层之前就已经被挡下。
func (r *Repository) ResolveDeptID(ctx context.Context, orgID int64, deptUUID string) (int64, error) {
	if !isUUIDLike(deptUUID) {
		return 0, ErrDeptNotFound
	}
	var id int64
	err := r.pool.QueryRow(ctx, `SELECT id FROM departments WHERE uuid = $1::uuid AND org_id = $2`,
		deptUUID, orgID).Scan(&id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, ErrDeptNotFound
		}
		return 0, err
	}
	return id, nil
}

// GetDepartmentOrgID 查部门所属组织（用于从部门反查组织上下文）
func (r *Repository) GetDepartmentOrgID(ctx context.Context, deptUUID string) (int64, string, error) {
	if !isUUIDLike(deptUUID) {
		return 0, "", ErrDeptNotFound
	}
	var orgID int64
	var orgUUID string
	err := r.pool.QueryRow(ctx, `SELECT o.id, o.uuid::text FROM departments d
		JOIN organizations o ON o.id = d.org_id WHERE d.uuid = $1::uuid`, deptUUID).Scan(&orgID, &orgUUID)
	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, "", ErrDeptNotFound
		}
		return 0, "", err
	}
	return orgID, orgUUID, nil
}

// GetDepartment 读单个部门
func (r *Repository) GetDepartment(ctx context.Context, orgID, deptID int64) (*orgdomain.Department, error) {
	return scanDepartment(r.pool.QueryRow(ctx,
		`SELECT `+deptSelectColumns+deptFromClause+` WHERE d.id = $1 AND d.org_id = $2`, deptID, orgID))
}

// ListDepartments 组织的全部部门（平铺，按物化路径排序即天然的展示序）
func (r *Repository) ListDepartments(ctx context.Context, orgID int64, status string) ([]orgdomain.Department, error) {
	query := `SELECT ` + deptSelectColumns + deptFromClause + ` WHERE d.org_id = $1`
	args := []any{orgID}
	if status != "" {
		query += ` AND d.status = $2`
		args = append(args, status)
	}
	query += ` ORDER BY d.sort_order, d.path, d.id`
	return r.queryDepartments(ctx, query, args...)
}

// ListDepartmentSubtree 某部门及其全部后代
func (r *Repository) ListDepartmentSubtree(ctx context.Context, orgID, rootDeptID int64) ([]orgdomain.Department, error) {
	return r.queryDepartments(ctx, `SELECT `+deptSelectColumns+deptFromClause+`
		JOIN department_closure c ON c.descendant_id = d.id
		WHERE c.ancestor_id = $1 AND d.org_id = $2
		ORDER BY d.sort_order, d.path, d.id`, rootDeptID, orgID)
}

// ListDepartmentAncestors 祖先链（自根到自身）
func (r *Repository) ListDepartmentAncestors(ctx context.Context, deptID int64) ([]orgdomain.Department, error) {
	return r.queryDepartments(ctx, `SELECT `+deptSelectColumns+deptFromClause+`
		JOIN department_closure c ON c.ancestor_id = d.id
		WHERE c.descendant_id = $1
		ORDER BY c.depth DESC`, deptID)
}

func (r *Repository) queryDepartments(ctx context.Context, query string, args ...any) ([]orgdomain.Department, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]orgdomain.Department, 0)
	for rows.Next() {
		d, err := scanDepartment(rows)
		if err != nil {
			return nil, err
		}
		if d != nil {
			items = append(items, *d)
		}
	}
	return items, rows.Err()
}

// SubtreeDeptIDs 子树内的全部部门 ID（成员范围过滤用）
func (r *Repository) SubtreeDeptIDs(ctx context.Context, deptID int64) ([]int64, error) {
	rows, err := r.pool.Query(ctx, `SELECT descendant_id FROM department_closure WHERE ancestor_id = $1`, deptID)
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

// DeptFullNames 批量取部门的「技术中心 / 平台组」全名
func (r *Repository) DeptFullNames(ctx context.Context, deptIDs []int64) (map[int64]string, error) {
	result := map[int64]string{}
	if len(deptIDs) == 0 {
		return result, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT c.descendant_id,
		string_agg(a.name, ' / ' ORDER BY c.depth DESC)
		FROM department_closure c JOIN departments a ON a.id = c.ancestor_id
		WHERE c.descendant_id = ANY($1) GROUP BY c.descendant_id`, deptIDs)
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

// ── 写入 ──

// CreateDepartment 建部门：主记录 → 物化路径 → 闭包表，一个事务。
func (r *Repository) CreateDepartment(ctx context.Context, orgID int64, input orgdomain.CreateDeptInput) (*orgdomain.Department, error) {
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
		err := tx.QueryRow(ctx, `SELECT id, path, depth FROM departments WHERE uuid = $1::uuid AND org_id = $2`,
			input.ParentUUID, orgID).Scan(&pid, &ppath, &pdepth)
		if err != nil {
			if err == pgx.ErrNoRows {
				return nil, apperrors.New(40478, http.StatusNotFound, "上级部门不存在或不属于该组织")
			}
			return nil, err
		}
		parentID = &pid
		parentPath = ppath
		parentDepth = pdepth
	}

	// 负责人必须已是该组织成员，否则会出现「部门主管不在组织里」
	var leaderID *int64
	if input.LeaderAdmin > 0 {
		if err := assertOrgMember(ctx, tx, orgID, input.LeaderAdmin); err != nil {
			return nil, err
		}
		leaderID = &input.LeaderAdmin
	}

	settings := input.Settings
	if settings == nil {
		settings = map[string]any{}
	}

	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO departments
		(org_id, parent_id, name, code, kind, description, sort_order, leader_id, member_limit, settings, path, depth)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'',$11) RETURNING id`,
		orgID, parentID, input.Name, input.Code, defaultString(input.Kind, "department"),
		input.Description, input.SortOrder, leaderID, input.MemberLimit, settings, parentDepth+1,
	).Scan(&id)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, apperrors.New(40971, http.StatusConflict, "部门代码在该组织内已存在")
		}
		return nil, err
	}

	if _, err := tx.Exec(ctx, `UPDATE departments SET path = $2 WHERE id = $1`,
		id, orgdomain.MaterializePath(parentPath, id)); err != nil {
		return nil, err
	}

	// 闭包：自身深度 0，再继承父节点的全部祖先并各加一层
	if _, err := tx.Exec(ctx, `INSERT INTO department_closure (ancestor_id, descendant_id, depth)
		VALUES ($1, $1, 0)`, id); err != nil {
		return nil, err
	}
	if parentID != nil {
		if _, err := tx.Exec(ctx, `INSERT INTO department_closure (ancestor_id, descendant_id, depth)
			SELECT c.ancestor_id, $1, c.depth + 1 FROM department_closure c WHERE c.descendant_id = $2`,
			id, *parentID); err != nil {
			return nil, err
		}
	}

	// 负责人自动成为部门成员
	if leaderID != nil {
		if _, err := tx.Exec(ctx, `INSERT INTO department_members (department_id, org_id, admin_id, is_leader)
			VALUES ($1,$2,$3,TRUE) ON CONFLICT (department_id, admin_id) DO UPDATE SET is_leader = TRUE`,
			id, orgID, *leaderID); err != nil {
			return nil, err
		}
	}

	touchOrgUpdatedAt(ctx, tx, orgID)
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetDepartment(ctx, orgID, id)
}

// UpdateDepartment 更新部门属性（不含层级变更，移动走 MoveDepartment）
func (r *Repository) UpdateDepartment(ctx context.Context, orgID, deptID int64, input orgdomain.UpdateDeptInput) (*orgdomain.Department, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	sets := []string{"updated_at = NOW()"}
	args := []any{deptID, orgID}
	idx := 3
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
	if input.SortOrder != nil {
		addSet("sort_order", *input.SortOrder)
	}
	if input.Status != nil {
		addSet("status", *input.Status)
	}
	if input.MemberLimit != nil {
		addSet("member_limit", *input.MemberLimit)
	}
	if input.Settings != nil {
		addSet("settings", input.Settings)
	}
	if input.ClearLeader {
		sets = append(sets, "leader_id = NULL")
	} else if input.LeaderAdmin != nil {
		if err := assertOrgMember(ctx, tx, orgID, *input.LeaderAdmin); err != nil {
			return nil, err
		}
		addSet("leader_id", *input.LeaderAdmin)
	}

	tag, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE departments SET %s WHERE id = $1 AND org_id = $2`,
		strings.Join(sets, ", ")), args...)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, apperrors.New(40971, http.StatusConflict, "部门代码在该组织内已存在")
		}
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrDeptNotFound
	}

	// 新负责人同时落成部门成员并标记 leader；换人时把旧 leader 标记降下来
	if !input.ClearLeader && input.LeaderAdmin != nil {
		if _, err := tx.Exec(ctx, `UPDATE department_members SET is_leader = FALSE
			WHERE department_id = $1 AND admin_id <> $2`, deptID, *input.LeaderAdmin); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO department_members (department_id, org_id, admin_id, is_leader)
			VALUES ($1,$2,$3,TRUE) ON CONFLICT (department_id, admin_id) DO UPDATE SET is_leader = TRUE`,
			deptID, orgID, *input.LeaderAdmin); err != nil {
			return nil, err
		}
	}
	if input.ClearLeader {
		if _, err := tx.Exec(ctx, `UPDATE department_members SET is_leader = FALSE WHERE department_id = $1`, deptID); err != nil {
			return nil, err
		}
	}

	touchOrgUpdatedAt(ctx, tx, orgID)
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetDepartment(ctx, orgID, deptID)
}

// MoveDepartment 移动部门（含整棵子树）。
//
// 环检测：新父节点若落在被移动节点的子树内，就会把这棵子树从树上剪下来接到自己身上，
// 形成一个谁都够不着的环。旧实现直接 UPDATE parent_id，没有任何检查。
func (r *Repository) MoveDepartment(ctx context.Context, orgID, deptID int64, input orgdomain.MoveDeptInput) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var oldPath string
	var oldDepth int
	var oldParent *int64
	if err := tx.QueryRow(ctx, `SELECT path, depth, parent_id FROM departments
		WHERE id = $1 AND org_id = $2 FOR UPDATE`, deptID, orgID).Scan(&oldPath, &oldDepth, &oldParent); err != nil {
		if err == pgx.ErrNoRows {
			return ErrDeptNotFound
		}
		return err
	}

	var newParentID *int64
	newParentPath := ""
	newDepth := 0
	if input.ParentUUID != "" {
		var pid int64
		var ppath string
		var pdepth int
		err := tx.QueryRow(ctx, `SELECT id, path, depth FROM departments WHERE uuid = $1::uuid AND org_id = $2`,
			input.ParentUUID, orgID).Scan(&pid, &ppath, &pdepth)
		if err != nil {
			if err == pgx.ErrNoRows {
				return apperrors.New(40478, http.StatusNotFound, "目标上级部门不存在或不属于该组织")
			}
			return err
		}
		if pid == deptID {
			return apperrors.New(40046, http.StatusBadRequest, "不能把部门移动到它自己下面")
		}
		if orgdomain.IsDescendantPath(oldPath, ppath) {
			return apperrors.New(40047, http.StatusBadRequest, "不能把部门移动到它自己的子部门下面")
		}
		newParentID = &pid
		newParentPath = ppath
		newDepth = pdepth + 1
	}

	if err := moveDepartmentTx(ctx, tx, deptID, oldPath, oldDepth, newParentID, newParentPath, newDepth); err != nil {
		return err
	}
	if input.SortOrder != nil {
		if _, err := tx.Exec(ctx, `UPDATE departments SET sort_order = $2 WHERE id = $1`, deptID, *input.SortOrder); err != nil {
			return err
		}
	}
	touchOrgUpdatedAt(ctx, tx, orgID)
	return tx.Commit(ctx)
}

// moveDepartmentTx 把 deptID 连同子树挂到新父节点下：重写物化路径 + 重建闭包。
func moveDepartmentTx(ctx context.Context, tx pgx.Tx, deptID int64, oldPath string, oldDepth int,
	newParentID *int64, newParentPath string, newDepth int) error {

	newPath := orgdomain.MaterializePath(newParentPath, deptID)
	delta := newDepth - oldDepth

	if _, err := tx.Exec(ctx, `UPDATE departments SET parent_id = $2, updated_at = NOW() WHERE id = $1`,
		deptID, newParentID); err != nil {
		return err
	}

	// 子树内每个节点：新路径 = 新自身路径 + 它相对旧自身路径的后缀。
	// 对节点自己后缀为空，恰好还原成 newPath。
	if _, err := tx.Exec(ctx, `UPDATE departments
		SET path = $1 || substring(path from $2), depth = depth + $3, updated_at = NOW()
		WHERE path LIKE $4 || '%'`,
		newPath, len(oldPath)+1, delta, oldPath); err != nil {
		return err
	}

	// 闭包第一步：切断子树与所有旧祖先的连线，子树内部的连线保持不动
	if _, err := tx.Exec(ctx, `DELETE FROM department_closure
		WHERE descendant_id IN (SELECT descendant_id FROM department_closure WHERE ancestor_id = $1)
		  AND ancestor_id NOT IN (SELECT descendant_id FROM department_closure WHERE ancestor_id = $1)`,
		deptID); err != nil {
		return err
	}

	// 闭包第二步：新祖先链 × 子树 的笛卡尔积
	if newParentID != nil {
		if _, err := tx.Exec(ctx, `INSERT INTO department_closure (ancestor_id, descendant_id, depth)
			SELECT sup.ancestor_id, sub.descendant_id, sup.depth + sub.depth + 1
			  FROM department_closure sup CROSS JOIN department_closure sub
			 WHERE sup.descendant_id = $1 AND sub.ancestor_id = $2
			ON CONFLICT DO NOTHING`, *newParentID, deptID); err != nil {
			return err
		}
	}
	return nil
}

// DeleteDepartment 删除部门，按策略处置子部门与成员。
func (r *Repository) DeleteDepartment(ctx context.Context, orgID, deptID int64, strategy orgdomain.DeleteDeptStrategy) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var path string
	var depth int
	var parentID *int64
	if err := tx.QueryRow(ctx, `SELECT path, depth, parent_id FROM departments
		WHERE id = $1 AND org_id = $2 FOR UPDATE`, deptID, orgID).Scan(&path, &depth, &parentID); err != nil {
		if err == pgx.ErrNoRows {
			return ErrDeptNotFound
		}
		return err
	}

	var childCount, memberCount int64
	if err := tx.QueryRow(ctx, `SELECT
		(SELECT COUNT(*) FROM departments WHERE parent_id = $1),
		(SELECT COUNT(*) FROM department_members WHERE department_id = $1)`, deptID).
		Scan(&childCount, &memberCount); err != nil {
		return err
	}

	switch strategy {
	case orgdomain.DeleteCascade:
		// 先断开所有 parent_id 引用再整批删 —— 父子外键是 RESTRICT，
		// 同一条 DELETE 里删父子的先后顺序不确定，不断开会随机失败
		if _, err := tx.Exec(ctx, `UPDATE departments SET parent_id = NULL
			WHERE org_id = $1 AND path LIKE $2 || '%'`, orgID, path); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM departments WHERE org_id = $1 AND path LIKE $2 || '%'`,
			orgID, path); err != nil {
			return err
		}

	case orgdomain.DeleteReparent:
		// 直接下级逐个上移到被删部门的父节点
		rows, err := tx.Query(ctx, `SELECT id, path, depth FROM departments WHERE parent_id = $1 ORDER BY id`, deptID)
		if err != nil {
			return err
		}
		type childRow struct {
			id    int64
			path  string
			depth int
		}
		var children []childRow
		for rows.Next() {
			var c childRow
			if err := rows.Scan(&c.id, &c.path, &c.depth); err != nil {
				rows.Close()
				return err
			}
			children = append(children, c)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		newParentPath := ""
		newChildDepth := 0
		if parentID != nil {
			if err := tx.QueryRow(ctx, `SELECT path, depth + 1 FROM departments WHERE id = $1`, *parentID).
				Scan(&newParentPath, &newChildDepth); err != nil {
				return err
			}
		}
		for _, c := range children {
			if err := moveDepartmentTx(ctx, tx, c.id, c.path, c.depth, parentID, newParentPath, newChildDepth); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM departments WHERE id = $1 AND org_id = $2`, deptID, orgID); err != nil {
			return err
		}

	default: // restrict
		if childCount > 0 {
			return apperrors.New(40941, http.StatusConflict,
				fmt.Sprintf("该部门下还有 %d 个子部门，请先移走或选择级联删除", childCount))
		}
		if memberCount > 0 {
			return apperrors.New(40942, http.StatusConflict,
				fmt.Sprintf("该部门还有 %d 名成员，请先移出成员", memberCount))
		}
		if _, err := tx.Exec(ctx, `DELETE FROM departments WHERE id = $1 AND org_id = $2`, deptID, orgID); err != nil {
			return err
		}
	}

	touchOrgUpdatedAt(ctx, tx, orgID)
	return tx.Commit(ctx)
}

// ReorderDepartments 批量调整同级排序
func (r *Repository) ReorderDepartments(ctx context.Context, orgID int64, orderByUUID map[string]int) error {
	if len(orderByUUID) == 0 {
		return nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for deptUUID, order := range orderByUUID {
		if !isUUIDLike(deptUUID) {
			continue
		}
		if _, err := tx.Exec(ctx, `UPDATE departments SET sort_order = $3, updated_at = NOW()
			WHERE uuid = $1::uuid AND org_id = $2`, deptUUID, orgID, order); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// assertOrgMember 校验管理员确实是该组织的在职成员
func assertOrgMember(ctx context.Context, exec queryExecutor, orgID, adminID int64) error {
	var ok bool
	if err := exec.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM org_members
		WHERE org_id = $1 AND admin_id = $2 AND status = 'active')`, orgID, adminID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return apperrors.New(40048, http.StatusBadRequest, "该管理员不是本组织的在职成员，请先加入组织")
	}
	return nil
}
