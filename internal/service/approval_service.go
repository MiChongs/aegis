package service

import (
	"context"
	"fmt"

	systemdomain "aegis/internal/domain/system"
	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// ApprovalService 审批与权限中心服务
type ApprovalService struct {
	log      *zap.Logger
	pg       *pgrepo.Repository
	realtime *RealtimeService
	org      *OrganizationService
}

// NewApprovalService 创建审批服务
func NewApprovalService(log *zap.Logger, pg *pgrepo.Repository, realtime *RealtimeService, org *OrganizationService) *ApprovalService {
	return &ApprovalService{log: log, pg: pg, realtime: realtime, org: org}
}

// ── 审批链 ──

func (s *ApprovalService) ListApprovalChains(ctx context.Context, orgID int64) ([]systemdomain.ApprovalChain, error) {
	return s.pg.ListApprovalChains(ctx, orgID)
}

func (s *ApprovalService) CreateApprovalChain(ctx context.Context, orgID int64, input systemdomain.CreateApprovalChainInput) (*systemdomain.ApprovalChain, error) {
	return s.pg.CreateApprovalChain(ctx, orgID, input)
}

func (s *ApprovalService) UpdateApprovalChain(ctx context.Context, chainID int64, name *string, steps *[]systemdomain.ApprovalStep, isActive *bool) error {
	return s.pg.UpdateApprovalChain(ctx, chainID, name, steps, isActive)
}

func (s *ApprovalService) DeleteApprovalChain(ctx context.Context, chainID int64) error {
	return s.pg.DeleteApprovalChain(ctx, chainID)
}

// ── 审批实例 ──

func (s *ApprovalService) ListApprovalInstances(ctx context.Context, orgID int64, status string, page, limit int) ([]systemdomain.ApprovalInstance, int64, error) {
	return s.pg.ListApprovalInstances(ctx, orgID, status, page, limit)
}

func (s *ApprovalService) GetApprovalInstance(ctx context.Context, instanceID int64) (*systemdomain.ApprovalInstance, error) {
	return s.pg.GetApprovalInstance(ctx, instanceID)
}

func (s *ApprovalService) ListMyPendingApprovals(ctx context.Context, adminID int64) ([]systemdomain.ApprovalInstance, error) {
	return s.pg.ListMyPendingApprovals(ctx, adminID)
}

// CreateApprovalInstance 创建审批实例并通知第一步审批人
func (s *ApprovalService) CreateApprovalInstance(ctx context.Context, chainID, orgID int64, triggerType string, requesterID int64, subjectData map[string]any) (*systemdomain.ApprovalInstance, error) {
	inst, err := s.pg.CreateApprovalInstance(ctx, chainID, orgID, triggerType, requesterID, subjectData)
	if err != nil {
		return nil, err
	}

	// 向第一步审批人发送通知
	s.notifyStepApprover(ctx, chainID, 0, inst)
	return inst, nil
}

// AdvanceApprovalStep 推进审批步骤
func (s *ApprovalService) AdvanceApprovalStep(ctx context.Context, instanceID, approverID int64, action, comment string) (*systemdomain.ApprovalInstance, error) {
	inst, err := s.pg.AdvanceApprovalStep(ctx, instanceID, approverID, action, comment)
	if err != nil {
		return nil, err
	}

	switch inst.Status {
	case "approved":
		// 全部通过，通知发起人
		s.notifyRequester(ctx, inst, "approval.completed")
	case "rejected":
		// 被驳回，通知发起人
		s.notifyRequester(ctx, inst, "approval.rejected")
	case "pending":
		// 还有下一步，通知下一步审批人
		s.notifyStepApprover(ctx, inst.ChainID, inst.CurrentStep, inst)
	}
	return inst, nil
}

// TriggerApproval 触发审批流程：查找匹配的审批链 → 创建实例 → 通知审批人
// 若无匹配审批链，返回 nil 表示无需审批
func (s *ApprovalService) TriggerApproval(ctx context.Context, orgID int64, triggerType string, requesterID int64, subjectData map[string]any) (*systemdomain.ApprovalInstance, error) {
	chain, err := s.pg.GetApprovalChainByTrigger(ctx, orgID, triggerType)
	if err != nil {
		return nil, err
	}
	if chain == nil {
		return nil, nil // 无匹配审批链，直接放行
	}
	return s.CreateApprovalInstance(ctx, chain.ID, orgID, triggerType, requesterID, subjectData)
}

// ── 权限模板 ──

func (s *ApprovalService) ListOrgPermTemplates(ctx context.Context, orgID int64) ([]systemdomain.OrgPermissionTemplate, error) {
	return s.pg.ListOrgPermTemplates(ctx, orgID)
}

func (s *ApprovalService) CreateOrgPermTemplate(ctx context.Context, orgID int64, input systemdomain.CreatePermTemplateInput) (*systemdomain.OrgPermissionTemplate, error) {
	return s.pg.CreateOrgPermTemplate(ctx, orgID, input)
}

func (s *ApprovalService) DeleteOrgPermTemplate(ctx context.Context, templateID int64) error {
	return s.pg.DeleteOrgPermTemplate(ctx, templateID)
}

// ── 资源绑定 ──

func (s *ApprovalService) ListOrgApps(ctx context.Context, orgID int64) ([]systemdomain.OrgAppBinding, error) {
	return s.pg.ListOrgApps(ctx, orgID)
}

func (s *ApprovalService) BindOrgApp(ctx context.Context, orgID, appID int64) (*systemdomain.OrgAppBinding, error) {
	return s.pg.BindOrgApp(ctx, orgID, appID)
}

func (s *ApprovalService) UnbindOrgApp(ctx context.Context, orgID, appID int64) error {
	return s.pg.UnbindOrgApp(ctx, orgID, appID)
}

// ── 协作组 ──

func (s *ApprovalService) ListCollabGroups(ctx context.Context, orgID int64) ([]systemdomain.CollaborationGroup, error) {
	return s.pg.ListCollabGroups(ctx, orgID)
}

func (s *ApprovalService) CreateCollabGroup(ctx context.Context, orgID int64, input systemdomain.CreateCollabGroupInput) (*systemdomain.CollaborationGroup, error) {
	return s.pg.CreateCollabGroup(ctx, orgID, input)
}

func (s *ApprovalService) UpdateCollabGroup(ctx context.Context, groupID int64, name, desc *string, deptIDs *[]int64, perms *[]string) error {
	return s.pg.UpdateCollabGroup(ctx, groupID, name, desc, deptIDs, perms)
}

func (s *ApprovalService) DeleteCollabGroup(ctx context.Context, groupID int64) error {
	return s.pg.DeleteCollabGroup(ctx, groupID)
}

// ── 成员导入 / 导出 ──

func (s *ApprovalService) BatchAddDepartmentMembers(ctx context.Context, deptID int64, adminIDs []int64) (int64, error) {
	return s.pg.BatchAddDepartmentMembers(ctx, deptID, adminIDs)
}

func (s *ApprovalService) ExportOrgMembers(ctx context.Context, orgID int64) ([]systemdomain.DepartmentMember, error) {
	return s.pg.ExportOrgMembers(ctx, orgID)
}

// ── 内部辅助 ──

// notifyStepApprover 通知指定步骤的审批人
func (s *ApprovalService) notifyStepApprover(ctx context.Context, chainID int64, step int, inst *systemdomain.ApprovalInstance) {
	if s.realtime == nil {
		return
	}
	chain, err := s.pg.GetApprovalChainByTrigger(ctx, inst.OrgID, inst.TriggerType)
	if err != nil || chain == nil {
		// 回退：直接读 chain
		return
	}
	if step >= len(chain.Steps) {
		return
	}
	approver := chain.Steps[step]
	data := map[string]any{
		"instanceId":  inst.ID,
		"triggerType": inst.TriggerType,
		"orgId":       inst.OrgID,
		"step":        step,
	}
	go func() {
		if err := s.realtime.PublishUserEvent(context.Background(), 0, approver.ApproverID, "approval.pending", data); err != nil {
			s.log.Warn("发送审批通知失败", zap.Error(err), zap.Int64("approverID", approver.ApproverID))
		}
	}()
}

// notifyRequester 通知审批发起人
func (s *ApprovalService) notifyRequester(ctx context.Context, inst *systemdomain.ApprovalInstance, eventType string) {
	if s.realtime == nil || inst == nil {
		return
	}
	go func() {
		data := map[string]any{
			"instanceId":  inst.ID,
			"triggerType": inst.TriggerType,
			"status":      inst.Status,
			"orgId":       inst.OrgID,
		}
		if err := s.realtime.PublishUserEvent(context.Background(), 0, inst.RequesterID, eventType, data); err != nil {
			s.log.Warn("发送审批结果通知失败", zap.Error(err), zap.Int64("requesterID", inst.RequesterID))
		}
	}()
}

// GetDepartmentOrgID 获取部门所属组织 ID（代理到 Repository）
func (s *ApprovalService) GetDepartmentOrgID(ctx context.Context, deptID int64) (int64, error) {
	return s.pg.GetDepartmentOrgID(ctx, deptID)
}

// IsOrganizationMember 检查管理员是否属于指定组织（代理到 OrganizationService）
func (s *ApprovalService) IsOrganizationMember(ctx context.Context, orgID, adminID int64) (bool, error) {
	return s.org.IsOrganizationMember(ctx, orgID, adminID)
}

// GetOrgPermTemplate 获取单个权限模板
func (s *ApprovalService) GetOrgPermTemplate(ctx context.Context, templateID int64) (*systemdomain.OrgPermissionTemplate, error) {
	// 通过列表查找
	// 这里直接用 SQL 查询
	templates, err := s.pg.ListOrgPermTemplates(ctx, 0)
	if err != nil {
		return nil, err
	}
	// 使用 ID 过滤 — 或者实现一个直接查询方法
	for _, t := range templates {
		if t.ID == templateID {
			return &t, nil
		}
	}
	return nil, fmt.Errorf("权限模板不存在")
}
