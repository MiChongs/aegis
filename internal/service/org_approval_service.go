package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	admindomain "aegis/internal/domain/admin"
	orgdomain "aegis/internal/domain/organization"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"

	"go.uber.org/zap"
)

// OrgApprovalService 组织审批、权限模板与协作组。
//
// 与 OrganizationService 拆开是因为审批有自己的生命周期（触发 → 流转 → 回调），
// 但权限判定仍然复用同一个 OrgAccessControl，不另立一套规则。
type OrgApprovalService struct {
	log      *zap.Logger
	pg       *pgrepo.Repository
	access   *OrgAccessControl
	org      *OrganizationService
	realtime *RealtimeService
	inbox    *AdminInboxService
}

// NewOrgApprovalService 创建审批服务
func NewOrgApprovalService(log *zap.Logger, pg *pgrepo.Repository, access *OrgAccessControl, org *OrganizationService) *OrgApprovalService {
	return &OrgApprovalService{log: log, pg: pg, access: access, org: org}
}

// SetRealtimeService 注入实时推送
func (s *OrgApprovalService) SetRealtimeService(rt *RealtimeService) { s.realtime = rt }

// SetAdminInbox 注入管理员收件箱
func (s *OrgApprovalService) SetAdminInbox(inbox *AdminInboxService) { s.inbox = inbox }

func (s *OrgApprovalService) orgContext(ctx context.Context, access *admindomain.AccessContext, orgUUID, permission string) (*orgContext, error) {
	return s.org.orgContext(ctx, access, orgUUID, permission)
}

// ── 审批链 ──

// ListChains 组织审批链
func (s *OrgApprovalService) ListChains(ctx context.Context, access *admindomain.AccessContext, orgUUID string) ([]orgdomain.ApprovalChain, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermApprovalRead)
	if err != nil {
		return nil, err
	}
	return s.pg.ListApprovalChains(ctx, oc.OrgID)
}

// CreateChain 创建审批链
func (s *OrgApprovalService) CreateChain(ctx context.Context, access *admindomain.AccessContext, orgUUID string, input orgdomain.CreateApprovalChainInput) (*orgdomain.ApprovalChain, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermApprovalManage)
	if err != nil {
		return nil, err
	}
	if err := assertWritable(oc.Org); err != nil {
		return nil, err
	}
	if err := validateChainInput(input.Name, input.TriggerType, input.Steps); err != nil {
		return nil, err
	}
	chain, err := s.pg.CreateApprovalChain(ctx, oc.OrgID, input)
	if err != nil {
		return nil, err
	}
	s.org.recordActivity(ctx, oc.OrgID, access.AdminID, "approval.chain_create", "approval_chain", chain.UUID,
		"创建审批链 "+chain.Name, map[string]any{"triggerType": chain.TriggerType})
	return chain, nil
}

// UpdateChain 更新审批链
func (s *OrgApprovalService) UpdateChain(ctx context.Context, access *admindomain.AccessContext, orgUUID, chainUUID string, input orgdomain.UpdateApprovalChainInput) (*orgdomain.ApprovalChain, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermApprovalManage)
	if err != nil {
		return nil, err
	}
	if err := assertWritable(oc.Org); err != nil {
		return nil, err
	}
	if input.Steps != nil {
		if err := validateSteps(*input.Steps); err != nil {
			return nil, err
		}
	}
	chainID, err := s.pg.ResolveApprovalChainID(ctx, oc.OrgID, chainUUID)
	if err != nil {
		return nil, err
	}
	chain, err := s.pg.UpdateApprovalChain(ctx, oc.OrgID, chainID, input)
	if err != nil {
		return nil, err
	}
	s.org.recordActivity(ctx, oc.OrgID, access.AdminID, "approval.chain_update", "approval_chain", chain.UUID,
		"更新审批链 "+chain.Name, nil)
	return chain, nil
}

// DeleteChain 删除审批链
func (s *OrgApprovalService) DeleteChain(ctx context.Context, access *admindomain.AccessContext, orgUUID, chainUUID string) error {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermApprovalManage)
	if err != nil {
		return err
	}
	chainID, err := s.pg.ResolveApprovalChainID(ctx, oc.OrgID, chainUUID)
	if err != nil {
		return err
	}
	if err := s.pg.DeleteApprovalChain(ctx, oc.OrgID, chainID); err != nil {
		return err
	}
	s.org.recordActivity(ctx, oc.OrgID, access.AdminID, "approval.chain_delete", "approval_chain", chainUUID,
		"删除审批链", nil)
	return nil
}

// ── 审批实例 ──

// ListInstances 审批记录
func (s *OrgApprovalService) ListInstances(ctx context.Context, access *admindomain.AccessContext, orgUUID, status string, page, limit int) (*orgdomain.Page[orgdomain.ApprovalInstance], error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermApprovalRead)
	if err != nil {
		return nil, err
	}
	return s.pg.ListApprovalInstances(ctx, oc.OrgID, status, page, limit)
}

// GetInstance 审批详情
func (s *OrgApprovalService) GetInstance(ctx context.Context, access *admindomain.AccessContext, orgUUID, instUUID string) (*orgdomain.ApprovalInstance, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermApprovalRead)
	if err != nil {
		return nil, err
	}
	inst, err := s.pg.GetApprovalInstance(ctx, oc.OrgID, instUUID)
	if err != nil {
		return nil, err
	}
	if inst == nil {
		return nil, apperrors.New(40481, http.StatusNotFound, "审批实例不存在")
	}
	return inst, nil
}

// ListMyPending 待我审批。
// 不做组织权限判定 —— 审批人身份本身就是授权依据，
// 而且待办要跨组织聚合在一个入口里。
func (s *OrgApprovalService) ListMyPending(ctx context.Context, access *admindomain.AccessContext) ([]orgdomain.ApprovalInstance, error) {
	if access == nil {
		return nil, apperrors.New(40110, http.StatusUnauthorized, "管理员未认证")
	}
	return s.pg.ListPendingApprovalsFor(ctx, access.AdminID)
}

// Advance 推进审批。
//
// 授权依据是「你是否出现在当前步骤的待审批人里」，不是组织角色 ——
// 一个 member 也可能因为是部门负责人而成为审批人。
func (s *OrgApprovalService) Advance(ctx context.Context, access *admindomain.AccessContext, instUUID, action, comment string) (*orgdomain.ApprovalInstance, error) {
	if access == nil {
		return nil, apperrors.New(40110, http.StatusUnauthorized, "管理员未认证")
	}
	if action != "approved" && action != "rejected" {
		return nil, apperrors.New(40072, http.StatusBadRequest, "审批动作只能是 approved 或 rejected")
	}

	pending, err := s.pg.ListPendingApprovalsFor(ctx, access.AdminID)
	if err != nil {
		return nil, err
	}
	var target *orgdomain.ApprovalInstance
	for i := range pending {
		if pending[i].UUID == instUUID {
			target = &pending[i]
			break
		}
	}
	if target == nil {
		return nil, apperrors.New(40388, http.StatusForbidden, "该审批当前不需要你处理")
	}

	orgID, err := s.pg.ResolveOrgID(ctx, target.OrgUUID)
	if err != nil {
		return nil, err
	}
	inst, err := s.pg.AdvanceApprovalStep(ctx, orgID, instUUID, access.AdminID, action, comment)
	if err != nil {
		return nil, err
	}

	switch inst.Status {
	case "approved":
		s.notify([]int64{inst.RequesterID}, "org.approval.completed", "审批已通过",
			fmt.Sprintf("你发起的「%s」审批已全部通过", triggerLabel(inst.TriggerType)), inst)
	case "rejected":
		s.notify([]int64{inst.RequesterID}, "org.approval.rejected", "审批被驳回",
			fmt.Sprintf("你发起的「%s」审批被驳回：%s", triggerLabel(inst.TriggerType), comment), inst)
	case "pending":
		s.notifyNextApprovers(ctx, inst)
	}
	s.org.recordActivity(ctx, orgID, access.AdminID, "approval.advance", "approval", inst.UUID,
		fmt.Sprintf("审批 %s", map[string]string{"approved": "通过", "rejected": "驳回"}[action]),
		map[string]any{"status": inst.Status, "comment": comment})
	return inst, nil
}

// Trigger 触发审批流程。没有匹配的启用审批链时返回 nil，表示无需审批直接放行。
func (s *OrgApprovalService) Trigger(ctx context.Context, orgID int64, triggerType string, requesterID int64, subject map[string]any) (*orgdomain.ApprovalInstance, error) {
	chain, err := s.pg.GetApprovalChainByTrigger(ctx, orgID, triggerType)
	if err != nil {
		return nil, err
	}
	if chain == nil || len(chain.Steps) == 0 {
		return nil, nil
	}
	inst, err := s.pg.CreateApprovalInstance(ctx, chain.ID, orgID, triggerType, requesterID, subject)
	if err != nil {
		return nil, err
	}
	s.notifyNextApprovers(ctx, inst)
	return inst, nil
}

// ── 权限模板 ──

// ListTemplates 权限模板
func (s *OrgApprovalService) ListTemplates(ctx context.Context, access *admindomain.AccessContext, orgUUID string) ([]orgdomain.PermissionTemplate, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermRoleRead)
	if err != nil {
		return nil, err
	}
	return s.pg.ListPermTemplates(ctx, oc.OrgID)
}

// CreateTemplate 创建权限模板
func (s *OrgApprovalService) CreateTemplate(ctx context.Context, access *admindomain.AccessContext, orgUUID string, input orgdomain.CreatePermTemplateInput) (*orgdomain.PermissionTemplate, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermRoleWrite)
	if err != nil {
		return nil, err
	}
	if err := assertWritable(oc.Org); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Name) == "" {
		return nil, apperrors.New(40073, http.StatusBadRequest, "模板名称不能为空")
	}
	if len(input.Permissions) == 0 {
		return nil, apperrors.New(40070, http.StatusBadRequest, "请至少选择一项权限")
	}
	// 与自建角色同一条底线：不能造出自己都没有的权限
	for _, p := range input.Permissions {
		if !oc.Access.Can(p) {
			return nil, apperrors.New(40389, http.StatusForbidden, "不能授予你自己不具备的权限："+p)
		}
	}
	tpl, err := s.pg.CreatePermTemplate(ctx, oc.OrgID, input)
	if err != nil {
		return nil, err
	}
	s.org.recordActivity(ctx, oc.OrgID, access.AdminID, "template.create", "perm_template", tpl.UUID,
		"创建权限模板 "+tpl.Name, nil)
	return tpl, nil
}

// DeleteTemplate 删除权限模板
func (s *OrgApprovalService) DeleteTemplate(ctx context.Context, access *admindomain.AccessContext, orgUUID, templateUUID string) error {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermRoleWrite)
	if err != nil {
		return err
	}
	if err := s.pg.DeletePermTemplate(ctx, oc.OrgID, templateUUID); err != nil {
		return err
	}
	s.org.recordActivity(ctx, oc.OrgID, access.AdminID, "template.delete", "perm_template", templateUUID,
		"删除权限模板", nil)
	return nil
}

// ApplyTemplate 套用模板：落成一个组织角色并授予指定成员。
//
// 旧实现只是把模板读出来就结束了，没有任何落地动作 —— 「应用模板」按钮点了
// 什么都不会发生。这里把它接到真正的角色体系上。
func (s *OrgApprovalService) ApplyTemplate(ctx context.Context, access *admindomain.AccessContext, orgUUID, templateUUID string, input orgdomain.ApplyTemplateInput) (*orgdomain.Role, int, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermRoleWrite)
	if err != nil {
		return nil, 0, err
	}
	if err := assertWritable(oc.Org); err != nil {
		return nil, 0, err
	}
	tpl, err := s.pg.GetPermTemplate(ctx, oc.OrgID, templateUUID)
	if err != nil {
		return nil, 0, err
	}
	if tpl == nil {
		return nil, 0, apperrors.New(40482, http.StatusNotFound, "权限模板不存在")
	}
	if len(input.AdminIDs) == 0 {
		return nil, 0, apperrors.New(40074, http.StatusBadRequest, "请选择要套用模板的成员")
	}

	roleName := strings.TrimSpace(input.RoleName)
	if roleName == "" {
		roleName = tpl.Name
	}
	roleKey := "tpl_" + strings.ReplaceAll(strings.ToLower(roleName), " ", "_")

	roleInput := orgdomain.RoleInput{
		RoleKey: roleKey, Name: roleName,
		Description: "由权限模板「" + tpl.Name + "」生成",
		Permissions: tpl.Permissions,
	}
	if err := s.org.validateRoleInput(oc, roleInput); err != nil {
		return nil, 0, err
	}

	// 同名角色已存在就复用，重复套用模板不该每次都堆一个新角色
	roles, err := s.pg.ListOrgRoles(ctx, oc.OrgID)
	if err != nil {
		return nil, 0, err
	}
	var role *orgdomain.Role
	for i := range roles {
		if roles[i].RoleKey == roleKey {
			updated, err := s.pg.UpdateOrgRole(ctx, oc.OrgID, roles[i].ID, roleInput)
			if err != nil {
				return nil, 0, err
			}
			role = updated
			break
		}
	}
	if role == nil {
		role, err = s.pg.CreateOrgRole(ctx, oc.OrgID, roleInput, access.AdminID)
		if err != nil {
			return nil, 0, err
		}
	}
	if err := s.access.Reload(ctx); err != nil {
		return nil, 0, err
	}

	granted, err := s.pg.GrantOrgRole(ctx, oc.OrgID, role.ID,
		orgdomain.GrantRoleInput{AdminIDs: input.AdminIDs, ScopeDeptUUID: input.ScopeDeptUUID}, access.AdminID)
	if err != nil {
		return nil, 0, err
	}
	s.org.recordActivity(ctx, oc.OrgID, access.AdminID, "template.apply", "perm_template", tpl.UUID,
		fmt.Sprintf("套用模板「%s」给 %d 位成员", tpl.Name, granted),
		map[string]any{"roleId": role.UUID, "adminIds": input.AdminIDs})
	return role, granted, nil
}

// ── 协作组 ──

// ListCollabGroups 跨部门协作组
func (s *OrgApprovalService) ListCollabGroups(ctx context.Context, access *admindomain.AccessContext, orgUUID string) ([]orgdomain.CollaborationGroup, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermDeptRead)
	if err != nil {
		return nil, err
	}
	return s.pg.ListCollabGroups(ctx, oc.OrgID)
}

// CreateCollabGroup 创建协作组
func (s *OrgApprovalService) CreateCollabGroup(ctx context.Context, access *admindomain.AccessContext, orgUUID string, input orgdomain.CollabGroupInput) (*orgdomain.CollaborationGroup, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermDeptWrite)
	if err != nil {
		return nil, err
	}
	if err := assertWritable(oc.Org); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Name) == "" {
		return nil, apperrors.New(40075, http.StatusBadRequest, "协作组名称不能为空")
	}
	group, err := s.pg.CreateCollabGroup(ctx, oc.OrgID, input)
	if err != nil {
		return nil, err
	}
	s.org.recordActivity(ctx, oc.OrgID, access.AdminID, "collab.create", "collab_group", group.UUID,
		"创建协作组 "+group.Name, nil)
	return group, nil
}

// UpdateCollabGroup 更新协作组
func (s *OrgApprovalService) UpdateCollabGroup(ctx context.Context, access *admindomain.AccessContext, orgUUID, groupUUID string, input orgdomain.CollabGroupInput) error {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermDeptWrite)
	if err != nil {
		return err
	}
	if err := assertWritable(oc.Org); err != nil {
		return err
	}
	if err := s.pg.UpdateCollabGroup(ctx, oc.OrgID, groupUUID, input); err != nil {
		return err
	}
	s.org.recordActivity(ctx, oc.OrgID, access.AdminID, "collab.update", "collab_group", groupUUID,
		"更新协作组", nil)
	return nil
}

// DeleteCollabGroup 删除协作组
func (s *OrgApprovalService) DeleteCollabGroup(ctx context.Context, access *admindomain.AccessContext, orgUUID, groupUUID string) error {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermDeptWrite)
	if err != nil {
		return err
	}
	if err := s.pg.DeleteCollabGroup(ctx, oc.OrgID, groupUUID); err != nil {
		return err
	}
	s.org.recordActivity(ctx, oc.OrgID, access.AdminID, "collab.delete", "collab_group", groupUUID,
		"删除协作组", nil)
	return nil
}

// ── 内部辅助 ──

// notifyNextApprovers 通知当前步骤的实际审批人。
// 审批人解析交给同一条 SQL（ListPendingApprovalsFor 的逆向），
// 免得「谁该审」在通知侧和判定侧各算一次、算出不同结果。
func (s *OrgApprovalService) notifyNextApprovers(ctx context.Context, inst *orgdomain.ApprovalInstance) {
	if inst == nil || s.org == nil {
		return
	}
	approvers, err := s.pg.ResolveStepApprovers(ctx, inst.UUID)
	if err != nil {
		s.log.Warn("解析审批人失败", zap.Error(err), zap.String("instanceId", inst.UUID))
		return
	}
	ids := make([]int64, 0, len(approvers))
	for _, a := range approvers {
		ids = append(ids, a.AdminID)
	}
	inst.PendingApprovers = approvers
	s.notify(ids, "org.approval.pending", "待你审批",
		fmt.Sprintf("有一条「%s」审批等待你处理", triggerLabel(inst.TriggerType)), inst)
}

func (s *OrgApprovalService) notify(adminIDs []int64, event, title, content string, inst *orgdomain.ApprovalInstance) {
	if len(adminIDs) == 0 || s.org == nil {
		return
	}
	data := map[string]any{}
	if inst != nil {
		data["instanceId"] = inst.UUID
		data["orgId"] = inst.OrgUUID
		data["triggerType"] = inst.TriggerType
		data["status"] = inst.Status
	}
	s.org.notifyAdmins(context.Background(), adminIDs, event, title, content, data)
}

func validateChainInput(name, triggerType string, steps []orgdomain.ApprovalStep) error {
	if strings.TrimSpace(name) == "" {
		return apperrors.New(40076, http.StatusBadRequest, "审批链名称不能为空")
	}
	switch triggerType {
	case orgdomain.TriggerMemberJoin, orgdomain.TriggerMemberLeave, orgdomain.TriggerDeptCreate,
		orgdomain.TriggerDeptDelete, orgdomain.TriggerRoleChange, orgdomain.TriggerAppBind:
	default:
		return apperrors.New(40077, http.StatusBadRequest, "审批触发场景取值无效")
	}
	return validateSteps(steps)
}

// validateSteps 审批步骤必须能解析出确定的审批人。
// 旧实现允许存 approverType=admin 且 approverId=0 这种步骤，
// 结果是审批发起后永远卡在第一步，谁也看不到它。
func validateSteps(steps []orgdomain.ApprovalStep) error {
	if len(steps) == 0 {
		return apperrors.New(40078, http.StatusBadRequest, "审批链至少要有一个步骤")
	}
	if len(steps) > 10 {
		return apperrors.New(40079, http.StatusBadRequest, "审批步骤最多 10 步")
	}
	for i, step := range steps {
		switch step.ApproverType {
		case orgdomain.ApproverAdmin, orgdomain.ApproverPosition:
			if step.ApproverID <= 0 {
				return apperrors.New(40080, http.StatusBadRequest,
					fmt.Sprintf("第 %d 步未指定审批人", i+1))
			}
		case orgdomain.ApproverOrgRole:
			if !orgdomain.IsValidRole(step.ApproverRole) {
				return apperrors.New(40081, http.StatusBadRequest,
					fmt.Sprintf("第 %d 步的审批角色无效", i+1))
			}
		case orgdomain.ApproverLeader:
			// 由申请人所在部门的负责人动态解析，无需额外参数
		default:
			return apperrors.New(40082, http.StatusBadRequest,
				fmt.Sprintf("第 %d 步的审批人类型无效", i+1))
		}
	}
	return nil
}

func triggerLabel(trigger string) string {
	switch trigger {
	case orgdomain.TriggerMemberJoin:
		return "成员加入"
	case orgdomain.TriggerMemberLeave:
		return "成员离开"
	case orgdomain.TriggerDeptCreate:
		return "部门创建"
	case orgdomain.TriggerDeptDelete:
		return "部门删除"
	case orgdomain.TriggerRoleChange:
		return "角色变更"
	case orgdomain.TriggerAppBind:
		return "应用绑定"
	}
	return trigger
}
