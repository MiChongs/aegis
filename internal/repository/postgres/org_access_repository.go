package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"

	orgdomain "aegis/internal/domain/organization"
	apperrors "aegis/pkg/errors"

	"github.com/jackc/pgx/v5"
)

// ── 审批链 ──

const approvalChainColumns = `c.id, c.uuid::text, o.uuid::text, c.name, c.trigger_type,
	c.steps, c.is_active, c.created_at, c.updated_at`

const approvalChainFrom = ` FROM approval_chains c JOIN organizations o ON o.id = c.org_id`

func scanApprovalChain(row pgx.Row) (*orgdomain.ApprovalChain, error) {
	var c orgdomain.ApprovalChain
	var stepsRaw []byte
	err := row.Scan(&c.ID, &c.UUID, &c.OrgUUID, &c.Name, &c.TriggerType,
		&stepsRaw, &c.IsActive, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	c.Steps = []orgdomain.ApprovalStep{}
	if len(stepsRaw) > 0 {
		_ = json.Unmarshal(stepsRaw, &c.Steps)
	}
	return &c, nil
}

// ResolveApprovalChainID 审批链 UUID → 内部主键，强制校验组织归属
func (r *Repository) ResolveApprovalChainID(ctx context.Context, orgID int64, chainUUID string) (int64, error) {
	if !isUUIDLike(chainUUID) {
		return 0, apperrors.New(40480, http.StatusNotFound, "审批链不存在")
	}
	var id int64
	err := r.pool.QueryRow(ctx, `SELECT id FROM approval_chains WHERE uuid = $1::uuid AND org_id = $2`,
		chainUUID, orgID).Scan(&id)
	if err != nil {
		if err == pgx.ErrNoRows {
			return 0, apperrors.New(40480, http.StatusNotFound, "审批链不存在")
		}
		return 0, err
	}
	return id, nil
}

// ListApprovalChains 组织审批链
func (r *Repository) ListApprovalChains(ctx context.Context, orgID int64) ([]orgdomain.ApprovalChain, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+approvalChainColumns+approvalChainFrom+
		` WHERE c.org_id = $1 ORDER BY c.id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]orgdomain.ApprovalChain, 0)
	for rows.Next() {
		c, err := scanApprovalChain(rows)
		if err != nil {
			return nil, err
		}
		if c != nil {
			items = append(items, *c)
		}
	}
	return items, rows.Err()
}

// GetApprovalChainByTrigger 取该场景下启用中的审批链（唯一索引保证至多一条）
func (r *Repository) GetApprovalChainByTrigger(ctx context.Context, orgID int64, triggerType string) (*orgdomain.ApprovalChain, error) {
	return scanApprovalChain(r.pool.QueryRow(ctx, `SELECT `+approvalChainColumns+approvalChainFrom+
		` WHERE c.org_id = $1 AND c.trigger_type = $2 AND c.is_active LIMIT 1`, orgID, triggerType))
}

// CreateApprovalChain 创建审批链
func (r *Repository) CreateApprovalChain(ctx context.Context, orgID int64, input orgdomain.CreateApprovalChainInput) (*orgdomain.ApprovalChain, error) {
	steps, err := json.Marshal(input.Steps)
	if err != nil {
		return nil, err
	}
	active := true
	if input.IsActive != nil {
		active = *input.IsActive
	}
	var id int64
	err = r.pool.QueryRow(ctx, `INSERT INTO approval_chains (org_id, name, trigger_type, steps, is_active)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`, orgID, input.Name, input.TriggerType, steps, active).Scan(&id)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, apperrors.New(40974, http.StatusConflict, "该场景已有启用中的审批链，请先停用原有的")
		}
		return nil, err
	}
	return scanApprovalChain(r.pool.QueryRow(ctx, `SELECT `+approvalChainColumns+approvalChainFrom+` WHERE c.id = $1`, id))
}

// UpdateApprovalChain 更新审批链
func (r *Repository) UpdateApprovalChain(ctx context.Context, orgID, chainID int64, input orgdomain.UpdateApprovalChainInput) (*orgdomain.ApprovalChain, error) {
	var stepsJSON any
	if input.Steps != nil {
		raw, err := json.Marshal(*input.Steps)
		if err != nil {
			return nil, err
		}
		stepsJSON = raw
	}
	tag, err := r.pool.Exec(ctx, `UPDATE approval_chains SET
		name = COALESCE($3, name),
		steps = COALESCE($4, steps),
		is_active = COALESCE($5, is_active),
		updated_at = NOW()
		WHERE id = $1 AND org_id = $2`, chainID, orgID, input.Name, stepsJSON, input.IsActive)
	if err != nil {
		if isDuplicateKeyError(err) {
			return nil, apperrors.New(40974, http.StatusConflict, "该场景已有启用中的审批链，请先停用原有的")
		}
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, apperrors.New(40480, http.StatusNotFound, "审批链不存在")
	}
	return scanApprovalChain(r.pool.QueryRow(ctx, `SELECT `+approvalChainColumns+approvalChainFrom+` WHERE c.id = $1`, chainID))
}

// DeleteApprovalChain 删除审批链
func (r *Repository) DeleteApprovalChain(ctx context.Context, orgID, chainID int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM approval_chains WHERE id = $1 AND org_id = $2`, chainID, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperrors.New(40480, http.StatusNotFound, "审批链不存在")
	}
	return nil
}

// ── 审批实例 ──

const approvalInstColumns = `i.id, i.uuid::text, COALESCE(c.uuid::text, ''), COALESCE(c.name, ''),
	o.uuid::text, i.trigger_type, i.requester_id,
	COALESCE(NULLIF(rq.display_name, ''), rq.account, ''),
	i.subject_data, i.current_step, i.status, i.steps_result, i.created_at, i.updated_at,
	COALESCE(jsonb_array_length(c.steps), 0)`

const approvalInstFrom = ` FROM approval_instances i
	JOIN organizations o ON o.id = i.org_id
	LEFT JOIN approval_chains c ON c.id = i.chain_id
	LEFT JOIN admin_accounts rq ON rq.id = i.requester_id`

func scanApprovalInstance(row pgx.Row) (*orgdomain.ApprovalInstance, error) {
	var inst orgdomain.ApprovalInstance
	var subjectRaw, resultRaw []byte
	err := row.Scan(&inst.ID, &inst.UUID, &inst.ChainUUID, &inst.ChainName, &inst.OrgUUID,
		&inst.TriggerType, &inst.RequesterID, &inst.RequesterName,
		&subjectRaw, &inst.CurrentStep, &inst.Status, &resultRaw,
		&inst.CreatedAt, &inst.UpdatedAt, &inst.TotalSteps)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	inst.SubjectData = map[string]any{}
	inst.StepsResult = []orgdomain.StepResult{}
	if len(subjectRaw) > 0 {
		_ = json.Unmarshal(subjectRaw, &inst.SubjectData)
	}
	if len(resultRaw) > 0 {
		_ = json.Unmarshal(resultRaw, &inst.StepsResult)
	}
	return &inst, nil
}

// CreateApprovalInstance 发起审批实例
func (r *Repository) CreateApprovalInstance(ctx context.Context, chainID, orgID int64, triggerType string, requesterID int64, subjectData map[string]any) (*orgdomain.ApprovalInstance, error) {
	if subjectData == nil {
		subjectData = map[string]any{}
	}
	payload, err := json.Marshal(subjectData)
	if err != nil {
		return nil, err
	}
	var id int64
	if err := r.pool.QueryRow(ctx, `INSERT INTO approval_instances
		(chain_id, org_id, trigger_type, requester_id, subject_data)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		chainID, orgID, triggerType, requesterID, payload).Scan(&id); err != nil {
		return nil, err
	}
	return scanApprovalInstance(r.pool.QueryRow(ctx, `SELECT `+approvalInstColumns+approvalInstFrom+` WHERE i.id = $1`, id))
}

// GetApprovalInstance 按 UUID 读审批实例
func (r *Repository) GetApprovalInstance(ctx context.Context, orgID int64, instUUID string) (*orgdomain.ApprovalInstance, error) {
	if !isUUIDLike(instUUID) {
		return nil, nil
	}
	return scanApprovalInstance(r.pool.QueryRow(ctx, `SELECT `+approvalInstColumns+approvalInstFrom+
		` WHERE i.uuid = $1::uuid AND i.org_id = $2`, instUUID, orgID))
}

// ListApprovalInstances 审批记录分页
func (r *Repository) ListApprovalInstances(ctx context.Context, orgID int64, status string, page, limit int) (*orgdomain.Page[orgdomain.ApprovalInstance], error) {
	where := "i.org_id = $1"
	args := []any{orgID}
	if status != "" {
		where += " AND i.status = $2"
		args = append(args, status)
	}

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+approvalInstFrom+` WHERE `+where, args...).Scan(&total); err != nil {
		return nil, err
	}

	page, limit = normalizeOrgPaging(page, limit)
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(
		`SELECT %s%s WHERE %s ORDER BY i.created_at DESC LIMIT $%d OFFSET $%d`,
		approvalInstColumns, approvalInstFrom, where, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]orgdomain.ApprovalInstance, 0, limit)
	for rows.Next() {
		inst, err := scanApprovalInstance(rows)
		if err != nil {
			return nil, err
		}
		if inst != nil {
			items = append(items, *inst)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &orgdomain.Page[orgdomain.ApprovalInstance]{
		Items: items, Page: page, Limit: limit, Total: total,
		TotalPages: int(math.Ceil(float64(total) / float64(limit))),
	}, nil
}

// ListPendingApprovalsFor 待我审批的实例。
//
// 审批人解析发生在 SQL 里：当前步骤的 approverType 决定比对哪张表 ——
// admin 直接比 ID，leader 比申请人所在部门的负责人，org_role 比组织角色，
// position 比岗位持有者。放到 Go 里做会退化成「把全部 pending 拉回来逐条判断」。
func (r *Repository) ListPendingApprovalsFor(ctx context.Context, adminID int64) ([]orgdomain.ApprovalInstance, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+approvalInstColumns+approvalInstFrom+`
		WHERE i.status = 'pending' AND c.is_active AND EXISTS (
			SELECT 1 FROM jsonb_array_elements(c.steps) WITH ORDINALITY AS s(step, ord)
			WHERE ord = i.current_step + 1 AND (
				(s.step->>'approverType' = 'admin'    AND (s.step->>'approverId')::bigint = $1)
			 OR (s.step->>'approverType' = 'leader'   AND EXISTS (
					SELECT 1 FROM department_members dm
					JOIN departments d ON d.id = dm.department_id
					WHERE dm.admin_id = i.requester_id AND d.org_id = i.org_id AND d.leader_id = $1))
			 OR (s.step->>'approverType' = 'org_role' AND EXISTS (
					SELECT 1 FROM org_members om
					WHERE om.org_id = i.org_id AND om.admin_id = $1
					  AND om.status = 'active' AND om.org_role = s.step->>'approverRole'))
			 OR (s.step->>'approverType' = 'position' AND EXISTS (
					SELECT 1 FROM department_members dm
					WHERE dm.org_id = i.org_id AND dm.admin_id = $1
					  AND dm.position_id = (s.step->>'approverId')::bigint))
			)
		) ORDER BY i.created_at DESC LIMIT 200`, adminID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]orgdomain.ApprovalInstance, 0)
	for rows.Next() {
		inst, err := scanApprovalInstance(rows)
		if err != nil {
			return nil, err
		}
		if inst != nil {
			items = append(items, *inst)
		}
	}
	return items, rows.Err()
}

// ResolveStepApprovers 解析某个实例当前步骤的实际审批人。
//
// 与 ListPendingApprovalsFor 用的是同一套解析规则，只是方向相反：
// 一个问「这条审批该谁处理」，一个问「哪些审批该我处理」。
// 两边必须同源，否则会出现「通知发给了 A，但系统只认 B 的操作」。
func (r *Repository) ResolveStepApprovers(ctx context.Context, instUUID string) ([]orgdomain.ApproverRef, error) {
	if !isUUIDLike(instUUID) {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `
		WITH inst AS (
			SELECT i.id, i.org_id, i.requester_id, i.current_step, c.steps
			  FROM approval_instances i JOIN approval_chains c ON c.id = i.chain_id
			 WHERE i.uuid = $1::uuid AND i.status = 'pending'
		), step AS (
			SELECT inst.*, s.step FROM inst,
				 jsonb_array_elements(inst.steps) WITH ORDINALITY AS s(step, ord)
			 WHERE s.ord = inst.current_step + 1
		)
		SELECT DISTINCT a.id, a.account, COALESCE(NULLIF(a.display_name, ''), a.account, '')
		  FROM step JOIN admin_accounts a ON (
			(step.step->>'approverType' = 'admin'    AND a.id = (step.step->>'approverId')::bigint)
		 OR (step.step->>'approverType' = 'leader'   AND a.id IN (
				SELECT d.leader_id FROM department_members dm
				  JOIN departments d ON d.id = dm.department_id
				 WHERE dm.admin_id = step.requester_id AND d.org_id = step.org_id
				   AND d.leader_id IS NOT NULL))
		 OR (step.step->>'approverType' = 'org_role' AND a.id IN (
				SELECT om.admin_id FROM org_members om
				 WHERE om.org_id = step.org_id AND om.status = 'active'
				   AND om.org_role = step.step->>'approverRole'))
		 OR (step.step->>'approverType' = 'position' AND a.id IN (
				SELECT dm.admin_id FROM department_members dm
				 WHERE dm.org_id = step.org_id
				   AND dm.position_id = (step.step->>'approverId')::bigint))
		  )`, instUUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]orgdomain.ApproverRef, 0)
	for rows.Next() {
		var a orgdomain.ApproverRef
		if err := rows.Scan(&a.AdminID, &a.Account, &a.DisplayName); err != nil {
			return nil, err
		}
		items = append(items, a)
	}
	return items, rows.Err()
}

// AdvanceApprovalStep 推进一步审批。
//
// 用 FOR UPDATE 锁住实例行：两个审批人同时点「通过」时，
// 不锁会让 current_step 各加一次，直接跳过中间步骤。
func (r *Repository) AdvanceApprovalStep(ctx context.Context, orgID int64, instUUID string, approverID int64, action, comment string) (*orgdomain.ApprovalInstance, error) {
	if !isUUIDLike(instUUID) {
		return nil, apperrors.New(40481, http.StatusNotFound, "审批实例不存在")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var id, chainID int64
	var currentStep int
	var status string
	var resultRaw []byte
	err = tx.QueryRow(ctx, `SELECT id, chain_id, current_step, status, steps_result
		FROM approval_instances WHERE uuid = $1::uuid AND org_id = $2 FOR UPDATE`,
		instUUID, orgID).Scan(&id, &chainID, &currentStep, &status, &resultRaw)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apperrors.New(40481, http.StatusNotFound, "审批实例不存在")
		}
		return nil, err
	}
	if status != "pending" {
		return nil, apperrors.New(40943, http.StatusConflict, "该审批已结束")
	}

	var totalSteps int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(jsonb_array_length(steps), 0) FROM approval_chains WHERE id = $1`,
		chainID).Scan(&totalSteps); err != nil {
		return nil, err
	}

	results := []orgdomain.StepResult{}
	if len(resultRaw) > 0 {
		_ = json.Unmarshal(resultRaw, &results)
	}
	results = append(results, orgdomain.StepResult{
		Step: currentStep, ApproverID: approverID, Action: action, Comment: comment,
	})
	resultJSON, err := json.Marshal(results)
	if err != nil {
		return nil, err
	}

	newStatus := "pending"
	newStep := currentStep
	switch action {
	case "rejected":
		newStatus = "rejected"
	default:
		newStep = currentStep + 1
		if newStep >= totalSteps {
			newStatus = "approved"
		}
	}

	if _, err := tx.Exec(ctx, `UPDATE approval_instances
		SET current_step = $2, status = $3, steps_result = $4, updated_at = NOW() WHERE id = $1`,
		id, newStep, newStatus, resultJSON); err != nil {
		return nil, err
	}

	inst, err := scanApprovalInstance(tx.QueryRow(ctx, `SELECT `+approvalInstColumns+approvalInstFrom+` WHERE i.id = $1`, id))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	// 结果里的审批人姓名补齐（列表查询不带这一层 JOIN，单条时值得多打一次库）
	if inst != nil {
		r.fillApproverNames(ctx, inst)
	}
	return inst, nil
}

func (r *Repository) fillApproverNames(ctx context.Context, inst *orgdomain.ApprovalInstance) {
	if len(inst.StepsResult) == 0 {
		return
	}
	ids := make([]int64, 0, len(inst.StepsResult))
	for _, s := range inst.StepsResult {
		ids = append(ids, s.ApproverID)
	}
	rows, err := r.pool.Query(ctx, `SELECT id, COALESCE(NULLIF(display_name, ''), account, '')
		FROM admin_accounts WHERE id = ANY($1)`, ids)
	if err != nil {
		return
	}
	defer rows.Close()
	names := map[int64]string{}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err == nil {
			names[id] = name
		}
	}
	for i := range inst.StepsResult {
		inst.StepsResult[i].ApproverName = names[inst.StepsResult[i].ApproverID]
	}
}

// ── 权限模板 ──

// ListPermTemplates 组织权限模板
func (r *Repository) ListPermTemplates(ctx context.Context, orgID int64) ([]orgdomain.PermissionTemplate, error) {
	rows, err := r.pool.Query(ctx, `SELECT t.id, t.uuid::text, o.uuid::text, t.name, t.description,
		t.permissions, t.is_default, t.created_at
		FROM org_permission_templates t JOIN organizations o ON o.id = t.org_id
		WHERE t.org_id = $1 ORDER BY t.is_default DESC, t.id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]orgdomain.PermissionTemplate, 0)
	for rows.Next() {
		var t orgdomain.PermissionTemplate
		if err := rows.Scan(&t.ID, &t.UUID, &t.OrgUUID, &t.Name, &t.Description,
			&t.Permissions, &t.IsDefault, &t.CreatedAt); err != nil {
			return nil, err
		}
		if t.Permissions == nil {
			t.Permissions = []string{}
		}
		items = append(items, t)
	}
	return items, rows.Err()
}

// GetPermTemplate 按 UUID 读模板（组织隔离）
func (r *Repository) GetPermTemplate(ctx context.Context, orgID int64, templateUUID string) (*orgdomain.PermissionTemplate, error) {
	if !isUUIDLike(templateUUID) {
		return nil, nil
	}
	var t orgdomain.PermissionTemplate
	err := r.pool.QueryRow(ctx, `SELECT t.id, t.uuid::text, o.uuid::text, t.name, t.description,
		t.permissions, t.is_default, t.created_at
		FROM org_permission_templates t JOIN organizations o ON o.id = t.org_id
		WHERE t.uuid = $1::uuid AND t.org_id = $2`, templateUUID, orgID).
		Scan(&t.ID, &t.UUID, &t.OrgUUID, &t.Name, &t.Description, &t.Permissions, &t.IsDefault, &t.CreatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	if t.Permissions == nil {
		t.Permissions = []string{}
	}
	return &t, nil
}

// CreatePermTemplate 创建权限模板
func (r *Repository) CreatePermTemplate(ctx context.Context, orgID int64, input orgdomain.CreatePermTemplateInput) (*orgdomain.PermissionTemplate, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	if input.IsDefault {
		if _, err := tx.Exec(ctx, `UPDATE org_permission_templates SET is_default = FALSE WHERE org_id = $1`, orgID); err != nil {
			return nil, err
		}
	}
	var uuidStr string
	if err := tx.QueryRow(ctx, `INSERT INTO org_permission_templates (org_id, name, description, permissions, is_default)
		VALUES ($1,$2,$3,$4,$5) RETURNING uuid::text`,
		orgID, input.Name, input.Description, input.Permissions, input.IsDefault).Scan(&uuidStr); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetPermTemplate(ctx, orgID, uuidStr)
}

// DeletePermTemplate 删除权限模板
func (r *Repository) DeletePermTemplate(ctx context.Context, orgID int64, templateUUID string) error {
	if !isUUIDLike(templateUUID) {
		return apperrors.New(40482, http.StatusNotFound, "权限模板不存在")
	}
	tag, err := r.pool.Exec(ctx, `DELETE FROM org_permission_templates WHERE uuid = $1::uuid AND org_id = $2`,
		templateUUID, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperrors.New(40482, http.StatusNotFound, "权限模板不存在")
	}
	return nil
}

// ── 协作组 ──

// ListCollabGroups 跨部门协作组
func (r *Repository) ListCollabGroups(ctx context.Context, orgID int64) ([]orgdomain.CollaborationGroup, error) {
	rows, err := r.pool.Query(ctx, `SELECT g.id, g.uuid::text, o.uuid::text, g.name, g.description,
		g.dept_ids, g.permissions, g.created_at,
		(SELECT COUNT(DISTINCT dm.admin_id) FROM department_members dm WHERE dm.department_id = ANY(g.dept_ids))
		FROM collaboration_groups g JOIN organizations o ON o.id = g.org_id
		WHERE g.org_id = $1 ORDER BY g.id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]orgdomain.CollaborationGroup, 0)
	allDeptIDs := map[int64]bool{}
	type pending struct {
		idx     int
		deptIDs []int64
	}
	var pendings []pending

	for rows.Next() {
		var g orgdomain.CollaborationGroup
		var deptIDs []int64
		if err := rows.Scan(&g.ID, &g.UUID, &g.OrgUUID, &g.Name, &g.Description,
			&deptIDs, &g.Permissions, &g.CreatedAt, &g.MemberCount); err != nil {
			return nil, err
		}
		if g.Permissions == nil {
			g.Permissions = []string{}
		}
		g.Depts = []orgdomain.DeptRef{}
		for _, id := range deptIDs {
			allDeptIDs[id] = true
		}
		pendings = append(pendings, pending{idx: len(items), deptIDs: deptIDs})
		items = append(items, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 部门引用一次批量解析，避免每个协作组打一次库
	if len(allDeptIDs) > 0 {
		ids := make([]int64, 0, len(allDeptIDs))
		for id := range allDeptIDs {
			ids = append(ids, id)
		}
		refRows, err := r.pool.Query(ctx, `SELECT id, uuid::text, name FROM departments WHERE id = ANY($1)`, ids)
		if err != nil {
			return nil, err
		}
		defer refRows.Close()
		refs := map[int64]orgdomain.DeptRef{}
		for refRows.Next() {
			var id int64
			var ref orgdomain.DeptRef
			if err := refRows.Scan(&id, &ref.UUID, &ref.Name); err != nil {
				return nil, err
			}
			refs[id] = ref
		}
		for _, p := range pendings {
			for _, id := range p.deptIDs {
				if ref, ok := refs[id]; ok {
					items[p.idx].Depts = append(items[p.idx].Depts, ref)
				}
			}
		}
	}
	return items, rows.Err()
}

// CreateCollabGroup 创建协作组
func (r *Repository) CreateCollabGroup(ctx context.Context, orgID int64, input orgdomain.CollabGroupInput) (*orgdomain.CollaborationGroup, error) {
	deptIDs, err := r.resolveDeptIDs(ctx, orgID, input.DeptUUIDs)
	if err != nil {
		return nil, err
	}
	var id int64
	if err := r.pool.QueryRow(ctx, `INSERT INTO collaboration_groups (org_id, name, description, dept_ids, permissions)
		VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		orgID, input.Name, input.Description, deptIDs, input.Permissions).Scan(&id); err != nil {
		return nil, err
	}
	groups, err := r.ListCollabGroups(ctx, orgID)
	if err != nil {
		return nil, err
	}
	for i := range groups {
		if groups[i].ID == id {
			return &groups[i], nil
		}
	}
	return nil, nil
}

// UpdateCollabGroup 更新协作组
func (r *Repository) UpdateCollabGroup(ctx context.Context, orgID int64, groupUUID string, input orgdomain.CollabGroupInput) error {
	if !isUUIDLike(groupUUID) {
		return apperrors.New(40483, http.StatusNotFound, "协作组不存在")
	}
	deptIDs, err := r.resolveDeptIDs(ctx, orgID, input.DeptUUIDs)
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `UPDATE collaboration_groups
		SET name = $3, description = $4, dept_ids = $5, permissions = $6
		WHERE uuid = $1::uuid AND org_id = $2`,
		groupUUID, orgID, input.Name, input.Description, deptIDs, input.Permissions)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperrors.New(40483, http.StatusNotFound, "协作组不存在")
	}
	return nil
}

// DeleteCollabGroup 删除协作组
func (r *Repository) DeleteCollabGroup(ctx context.Context, orgID int64, groupUUID string) error {
	if !isUUIDLike(groupUUID) {
		return apperrors.New(40483, http.StatusNotFound, "协作组不存在")
	}
	tag, err := r.pool.Exec(ctx, `DELETE FROM collaboration_groups WHERE uuid = $1::uuid AND org_id = $2`,
		groupUUID, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperrors.New(40483, http.StatusNotFound, "协作组不存在")
	}
	return nil
}

// resolveDeptIDs 批量把部门 UUID 解析为内部 ID，顺带确认它们都属于该组织
func (r *Repository) resolveDeptIDs(ctx context.Context, orgID int64, uuids []string) ([]int64, error) {
	ids := make([]int64, 0, len(uuids))
	for _, u := range uuids {
		if !isUUIDLike(u) {
			continue
		}
		id, err := r.ResolveDeptID(ctx, orgID, u)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}
