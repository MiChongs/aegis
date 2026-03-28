package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"time"

	systemdomain "aegis/internal/domain/system"
	apperrors "aegis/pkg/errors"

	"github.com/jackc/pgx/v5"
)

// ── 审批链 CRUD ──

// CreateApprovalChain 创建审批链
func (r *Repository) CreateApprovalChain(ctx context.Context, orgID int64, input systemdomain.CreateApprovalChainInput) (*systemdomain.ApprovalChain, error) {
	stepsJSON, err := json.Marshal(input.Steps)
	if err != nil {
		return nil, fmt.Errorf("序列化审批步骤失败: %w", err)
	}
	var chain systemdomain.ApprovalChain
	var stepsRaw []byte
	err = r.pool.QueryRow(ctx,
		`INSERT INTO approval_chains (org_id, name, trigger_type, steps) VALUES ($1,$2,$3,$4)
		 RETURNING id, org_id, name, trigger_type, steps, is_active, created_at, updated_at`,
		orgID, input.Name, input.TriggerType, stepsJSON,
	).Scan(&chain.ID, &chain.OrgID, &chain.Name, &chain.TriggerType, &stepsRaw, &chain.IsActive, &chain.CreatedAt, &chain.UpdatedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(stepsRaw, &chain.Steps)
	return &chain, nil
}

// ListApprovalChains 查询组织下所有审批链
func (r *Repository) ListApprovalChains(ctx context.Context, orgID int64) ([]systemdomain.ApprovalChain, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, org_id, name, trigger_type, steps, is_active, created_at, updated_at
		 FROM approval_chains WHERE org_id = $1 ORDER BY created_at DESC`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []systemdomain.ApprovalChain
	for rows.Next() {
		var chain systemdomain.ApprovalChain
		var stepsRaw []byte
		if err := rows.Scan(&chain.ID, &chain.OrgID, &chain.Name, &chain.TriggerType, &stepsRaw, &chain.IsActive, &chain.CreatedAt, &chain.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(stepsRaw, &chain.Steps)
		items = append(items, chain)
	}
	return items, rows.Err()
}

// GetApprovalChainByTrigger 根据触发类型查找活跃审批链
func (r *Repository) GetApprovalChainByTrigger(ctx context.Context, orgID int64, triggerType string) (*systemdomain.ApprovalChain, error) {
	var chain systemdomain.ApprovalChain
	var stepsRaw []byte
	err := r.pool.QueryRow(ctx,
		`SELECT id, org_id, name, trigger_type, steps, is_active, created_at, updated_at
		 FROM approval_chains WHERE org_id = $1 AND trigger_type = $2 AND is_active = TRUE
		 LIMIT 1`, orgID, triggerType,
	).Scan(&chain.ID, &chain.OrgID, &chain.Name, &chain.TriggerType, &stepsRaw, &chain.IsActive, &chain.CreatedAt, &chain.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	_ = json.Unmarshal(stepsRaw, &chain.Steps)
	return &chain, nil
}

// UpdateApprovalChain 更新审批链
func (r *Repository) UpdateApprovalChain(ctx context.Context, chainID int64, name *string, steps *[]systemdomain.ApprovalStep, isActive *bool) error {
	var stepsJSON []byte
	if steps != nil {
		var err error
		stepsJSON, err = json.Marshal(*steps)
		if err != nil {
			return fmt.Errorf("序列化审批步骤失败: %w", err)
		}
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE approval_chains SET
			name = COALESCE($2, name),
			steps = COALESCE($3, steps),
			is_active = COALESCE($4, is_active),
			updated_at = NOW()
		 WHERE id = $1`,
		chainID, name, stepsJSON, isActive)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperrors.New(40480, http.StatusNotFound, "审批链不存在")
	}
	return nil
}

// DeleteApprovalChain 删除审批链
func (r *Repository) DeleteApprovalChain(ctx context.Context, chainID int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM approval_chains WHERE id = $1`, chainID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperrors.New(40480, http.StatusNotFound, "审批链不存在")
	}
	return nil
}

// ── 审批实例 ──

// CreateApprovalInstance 创建审批实例
func (r *Repository) CreateApprovalInstance(ctx context.Context, chainID, orgID int64, triggerType string, requesterID int64, subjectData map[string]any) (*systemdomain.ApprovalInstance, error) {
	subjectJSON, err := json.Marshal(subjectData)
	if err != nil {
		return nil, fmt.Errorf("序列化审批数据失败: %w", err)
	}
	var inst systemdomain.ApprovalInstance
	var subjectRaw, stepsResultRaw []byte
	err = r.pool.QueryRow(ctx,
		`INSERT INTO approval_instances (chain_id, org_id, trigger_type, requester_id, subject_data)
		 VALUES ($1,$2,$3,$4,$5)
		 RETURNING id, chain_id, org_id, trigger_type, requester_id, subject_data, current_step, status, steps_result, created_at, updated_at`,
		chainID, orgID, triggerType, requesterID, subjectJSON,
	).Scan(&inst.ID, &inst.ChainID, &inst.OrgID, &inst.TriggerType, &inst.RequesterID, &subjectRaw, &inst.CurrentStep, &inst.Status, &stepsResultRaw, &inst.CreatedAt, &inst.UpdatedAt)
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(subjectRaw, &inst.SubjectData)
	_ = json.Unmarshal(stepsResultRaw, &inst.StepsResult)
	return &inst, nil
}

// GetApprovalInstance 获取审批实例详情
func (r *Repository) GetApprovalInstance(ctx context.Context, instanceID int64) (*systemdomain.ApprovalInstance, error) {
	var inst systemdomain.ApprovalInstance
	var subjectRaw, stepsResultRaw []byte
	err := r.pool.QueryRow(ctx,
		`SELECT id, chain_id, org_id, trigger_type, requester_id, subject_data, current_step, status, steps_result, created_at, updated_at
		 FROM approval_instances WHERE id = $1`, instanceID,
	).Scan(&inst.ID, &inst.ChainID, &inst.OrgID, &inst.TriggerType, &inst.RequesterID, &subjectRaw, &inst.CurrentStep, &inst.Status, &stepsResultRaw, &inst.CreatedAt, &inst.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	_ = json.Unmarshal(subjectRaw, &inst.SubjectData)
	_ = json.Unmarshal(stepsResultRaw, &inst.StepsResult)
	return &inst, nil
}

// ListApprovalInstances 分页查询审批实例
func (r *Repository) ListApprovalInstances(ctx context.Context, orgID int64, status string, page, limit int) ([]systemdomain.ApprovalInstance, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 50 {
		limit = 20
	}

	where := "org_id = $1"
	args := []any{orgID}
	idx := 2
	if status != "" {
		where += fmt.Sprintf(" AND status = $%d", idx)
		args = append(args, status)
		idx++
	}

	var total int64
	if err := r.pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM approval_instances WHERE %s`, where), args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * limit
	args = append(args, limit, offset)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(
		`SELECT id, chain_id, org_id, trigger_type, requester_id, subject_data, current_step, status, steps_result, created_at, updated_at
		 FROM approval_instances WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, where, idx, idx+1), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var items []systemdomain.ApprovalInstance
	for rows.Next() {
		var inst systemdomain.ApprovalInstance
		var subjectRaw, stepsResultRaw []byte
		if err := rows.Scan(&inst.ID, &inst.ChainID, &inst.OrgID, &inst.TriggerType, &inst.RequesterID, &subjectRaw, &inst.CurrentStep, &inst.Status, &stepsResultRaw, &inst.CreatedAt, &inst.UpdatedAt); err != nil {
			return nil, 0, err
		}
		_ = json.Unmarshal(subjectRaw, &inst.SubjectData)
		_ = json.Unmarshal(stepsResultRaw, &inst.StepsResult)
		items = append(items, inst)
	}
	return items, total, rows.Err()
}

// AdvanceApprovalStep 推进审批步骤（事务）
func (r *Repository) AdvanceApprovalStep(ctx context.Context, instanceID, approverID int64, action, comment string) (*systemdomain.ApprovalInstance, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// 加锁读取当前实例
	var inst systemdomain.ApprovalInstance
	var subjectRaw, stepsResultRaw []byte
	err = tx.QueryRow(ctx,
		`SELECT id, chain_id, org_id, trigger_type, requester_id, subject_data, current_step, status, steps_result, created_at, updated_at
		 FROM approval_instances WHERE id = $1 FOR UPDATE`, instanceID,
	).Scan(&inst.ID, &inst.ChainID, &inst.OrgID, &inst.TriggerType, &inst.RequesterID, &subjectRaw, &inst.CurrentStep, &inst.Status, &stepsResultRaw, &inst.CreatedAt, &inst.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, apperrors.New(40481, http.StatusNotFound, "审批实例不存在")
		}
		return nil, err
	}
	_ = json.Unmarshal(subjectRaw, &inst.SubjectData)
	_ = json.Unmarshal(stepsResultRaw, &inst.StepsResult)

	if inst.Status != "pending" {
		return nil, apperrors.New(40982, http.StatusConflict, "该审批已结束")
	}

	// 读取审批链获取总步骤数
	var chainStepsRaw []byte
	err = tx.QueryRow(ctx, `SELECT steps FROM approval_chains WHERE id = $1`, inst.ChainID).Scan(&chainStepsRaw)
	if err != nil {
		return nil, fmt.Errorf("读取审批链失败: %w", err)
	}
	var chainSteps []systemdomain.ApprovalStep
	_ = json.Unmarshal(chainStepsRaw, &chainSteps)

	// 追加步骤结果
	stepResult := systemdomain.StepResult{
		Step:       inst.CurrentStep,
		ApproverID: approverID,
		Action:     action,
		Comment:    comment,
		At:         time.Now(),
	}
	inst.StepsResult = append(inst.StepsResult, stepResult)
	newStepsResultJSON, _ := json.Marshal(inst.StepsResult)

	if action == "rejected" {
		inst.Status = "rejected"
	} else if inst.CurrentStep+1 >= len(chainSteps) {
		// 已通过所有步骤
		inst.Status = "approved"
	} else {
		// 推进到下一步
		inst.CurrentStep++
	}

	_, err = tx.Exec(ctx,
		`UPDATE approval_instances SET current_step = $2, status = $3, steps_result = $4, updated_at = NOW() WHERE id = $1`,
		instanceID, inst.CurrentStep, inst.Status, newStepsResultJSON)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &inst, nil
}

// ListMyPendingApprovals 查询当前管理员待审批的实例
func (r *Repository) ListMyPendingApprovals(ctx context.Context, adminID int64) ([]systemdomain.ApprovalInstance, error) {
	// 通过 JOIN 审批链，检查当前步骤对应的审批人
	rows, err := r.pool.Query(ctx,
		`SELECT ai.id, ai.chain_id, ai.org_id, ai.trigger_type, ai.requester_id, ai.subject_data,
			ai.current_step, ai.status, ai.steps_result, ai.created_at, ai.updated_at
		 FROM approval_instances ai
		 JOIN approval_chains ac ON ac.id = ai.chain_id
		 WHERE ai.status = 'pending'
		   AND ac.steps -> ai.current_step ->> 'approverId' = $1::text
		 ORDER BY ai.created_at DESC`,
		fmt.Sprintf("%d", adminID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []systemdomain.ApprovalInstance
	for rows.Next() {
		var inst systemdomain.ApprovalInstance
		var subjectRaw, stepsResultRaw []byte
		if err := rows.Scan(&inst.ID, &inst.ChainID, &inst.OrgID, &inst.TriggerType, &inst.RequesterID, &subjectRaw, &inst.CurrentStep, &inst.Status, &stepsResultRaw, &inst.CreatedAt, &inst.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(subjectRaw, &inst.SubjectData)
		_ = json.Unmarshal(stepsResultRaw, &inst.StepsResult)
		items = append(items, inst)
	}
	return items, rows.Err()
}

// ── 权限模板 ──

// CreateOrgPermTemplate 创建权限模板
func (r *Repository) CreateOrgPermTemplate(ctx context.Context, orgID int64, input systemdomain.CreatePermTemplateInput) (*systemdomain.OrgPermissionTemplate, error) {
	var tmpl systemdomain.OrgPermissionTemplate
	err := r.pool.QueryRow(ctx,
		`INSERT INTO org_permission_templates (org_id, name, description, permissions, is_default)
		 VALUES ($1,$2,$3,$4,$5)
		 RETURNING id, org_id, name, description, permissions, is_default, created_at`,
		orgID, input.Name, input.Description, input.Permissions, input.IsDefault,
	).Scan(&tmpl.ID, &tmpl.OrgID, &tmpl.Name, &tmpl.Description, &tmpl.Permissions, &tmpl.IsDefault, &tmpl.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &tmpl, nil
}

// ListOrgPermTemplates 查询组织权限模板列表
func (r *Repository) ListOrgPermTemplates(ctx context.Context, orgID int64) ([]systemdomain.OrgPermissionTemplate, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, org_id, name, description, permissions, is_default, created_at
		 FROM org_permission_templates WHERE org_id = $1 ORDER BY id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []systemdomain.OrgPermissionTemplate
	for rows.Next() {
		var tmpl systemdomain.OrgPermissionTemplate
		if err := rows.Scan(&tmpl.ID, &tmpl.OrgID, &tmpl.Name, &tmpl.Description, &tmpl.Permissions, &tmpl.IsDefault, &tmpl.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, tmpl)
	}
	return items, rows.Err()
}

// DeleteOrgPermTemplate 删除权限模板
func (r *Repository) DeleteOrgPermTemplate(ctx context.Context, templateID int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM org_permission_templates WHERE id = $1`, templateID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperrors.New(40482, http.StatusNotFound, "权限模板不存在")
	}
	return nil
}

// ── 资源绑定 ──

// BindOrgApp 绑定组织与应用
func (r *Repository) BindOrgApp(ctx context.Context, orgID, appID int64) (*systemdomain.OrgAppBinding, error) {
	var binding systemdomain.OrgAppBinding
	err := r.pool.QueryRow(ctx,
		`INSERT INTO org_app_bindings (org_id, app_id) VALUES ($1,$2)
		 ON CONFLICT (org_id, app_id) DO UPDATE SET org_id = EXCLUDED.org_id
		 RETURNING id, org_id, app_id, created_at`,
		orgID, appID,
	).Scan(&binding.ID, &binding.OrgID, &binding.AppID, &binding.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &binding, nil
}

// UnbindOrgApp 解绑组织与应用
func (r *Repository) UnbindOrgApp(ctx context.Context, orgID, appID int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM org_app_bindings WHERE org_id = $1 AND app_id = $2`, orgID, appID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperrors.New(40483, http.StatusNotFound, "绑定关系不存在")
	}
	return nil
}

// ListOrgApps 查询组织绑定的应用列表
func (r *Repository) ListOrgApps(ctx context.Context, orgID int64) ([]systemdomain.OrgAppBinding, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, org_id, app_id, created_at FROM org_app_bindings WHERE org_id = $1 ORDER BY id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []systemdomain.OrgAppBinding
	for rows.Next() {
		var b systemdomain.OrgAppBinding
		if err := rows.Scan(&b.ID, &b.OrgID, &b.AppID, &b.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, b)
	}
	return items, rows.Err()
}

// ── 协作组 ──

// CreateCollabGroup 创建协作组
func (r *Repository) CreateCollabGroup(ctx context.Context, orgID int64, input systemdomain.CreateCollabGroupInput) (*systemdomain.CollaborationGroup, error) {
	var grp systemdomain.CollaborationGroup
	err := r.pool.QueryRow(ctx,
		`INSERT INTO collaboration_groups (org_id, name, description, dept_ids, permissions)
		 VALUES ($1,$2,$3,$4,$5)
		 RETURNING id, org_id, name, description, dept_ids, permissions, created_at`,
		orgID, input.Name, input.Description, input.DeptIDs, input.Permissions,
	).Scan(&grp.ID, &grp.OrgID, &grp.Name, &grp.Description, &grp.DeptIDs, &grp.Permissions, &grp.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &grp, nil
}

// ListCollabGroups 查询组织协作组列表
func (r *Repository) ListCollabGroups(ctx context.Context, orgID int64) ([]systemdomain.CollaborationGroup, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, org_id, name, description, dept_ids, permissions, created_at
		 FROM collaboration_groups WHERE org_id = $1 ORDER BY id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []systemdomain.CollaborationGroup
	for rows.Next() {
		var grp systemdomain.CollaborationGroup
		if err := rows.Scan(&grp.ID, &grp.OrgID, &grp.Name, &grp.Description, &grp.DeptIDs, &grp.Permissions, &grp.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, grp)
	}
	return items, rows.Err()
}

// UpdateCollabGroup 更新协作组
func (r *Repository) UpdateCollabGroup(ctx context.Context, groupID int64, name, desc *string, deptIDs *[]int64, perms *[]string) error {
	// 构造可选更新字段
	var deptArr, permArr interface{}
	if deptIDs != nil {
		deptArr = *deptIDs
	}
	if perms != nil {
		permArr = *perms
	}
	tag, err := r.pool.Exec(ctx,
		`UPDATE collaboration_groups SET
			name = COALESCE($2, name),
			description = COALESCE($3, description),
			dept_ids = COALESCE($4, dept_ids),
			permissions = COALESCE($5, permissions)
		 WHERE id = $1`,
		groupID, name, desc, deptArr, permArr)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperrors.New(40484, http.StatusNotFound, "协作组不存在")
	}
	return nil
}

// DeleteCollabGroup 删除协作组
func (r *Repository) DeleteCollabGroup(ctx context.Context, groupID int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM collaboration_groups WHERE id = $1`, groupID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperrors.New(40484, http.StatusNotFound, "协作组不存在")
	}
	return nil
}

// ── 成员批量导入 ──

// BatchAddDepartmentMembers 批量添加部门成员，返回实际插入数
func (r *Repository) BatchAddDepartmentMembers(ctx context.Context, deptID int64, adminIDs []int64) (int64, error) {
	if len(adminIDs) == 0 {
		return 0, nil
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var inserted int64
	for _, aid := range adminIDs {
		tag, err := tx.Exec(ctx,
			`INSERT INTO department_members (department_id, admin_id, is_leader) VALUES ($1,$2,FALSE) ON CONFLICT (department_id, admin_id) DO NOTHING`,
			deptID, aid)
		if err != nil {
			return 0, err
		}
		inserted += tag.RowsAffected()
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return inserted, nil
}

// ── 成员导出 ──

// ExportOrgMembers 导出组织所有成员信息
func (r *Repository) ExportOrgMembers(ctx context.Context, orgID int64) ([]systemdomain.DepartmentMember, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT a.id, a.account, a.display_name, COALESCE(a.avatar,''),
			dm.is_leader, dm.joined_at,
			dm.position_id, COALESCE(p.name,''), COALESCE(dm.job_title,''),
			dm.reporting_to, COALESCE(rpt.display_name,''),
			dm.delegate_to, COALESCE(dlg.display_name,''), dm.delegate_expires_at
		 FROM department_members dm
		 JOIN admin_accounts a ON a.id = dm.admin_id
		 JOIN departments d ON d.id = dm.department_id
		 LEFT JOIN positions p ON p.id = dm.position_id
		 LEFT JOIN admin_accounts rpt ON rpt.id = dm.reporting_to
		 LEFT JOIN admin_accounts dlg ON dlg.id = dm.delegate_to
		 WHERE d.org_id = $1
		 ORDER BY d.name, a.account`, orgID)
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

// ── 部门辅助 ──

// GetDepartmentOrgID 查询部门所属组织 ID
func (r *Repository) GetDepartmentOrgID(ctx context.Context, deptID int64) (int64, error) {
	var orgID int64
	err := r.pool.QueryRow(ctx, `SELECT org_id FROM departments WHERE id = $1`, deptID).Scan(&orgID)
	return orgID, err
}

// ── 分页辅助 ──

// CalcTotalPages 计算总页数
func CalcTotalPages(total int64, limit int) int {
	return int(math.Ceil(float64(total) / float64(limit)))
}
