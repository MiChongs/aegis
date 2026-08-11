package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	admindomain "aegis/internal/domain/admin"
	notificationdomain "aegis/internal/domain/notification"
	orgdomain "aegis/internal/domain/organization"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"

	"go.uber.org/zap"
)

// OrganizationService 组织架构服务。
//
// 权限判定下沉在这一层（与 TicketService 同一约定）：handler 只负责取参数，
// 每个入口第一步都是 orgContext(...) 拿到「组织 + 调用者在该组织内的权限集」。
// 把判定留在 handler 会让「谁能改这个部门」散落在几十个函数里，漏一个就是越权。
type OrganizationService struct {
	log      *zap.Logger
	pg       *pgrepo.Repository
	access   *OrgAccessControl
	realtime *RealtimeService
	inbox    *AdminInboxService
	approval *OrgApprovalService
}

// NewOrganizationService 创建组织服务
func NewOrganizationService(log *zap.Logger, pg *pgrepo.Repository, access *OrgAccessControl) *OrganizationService {
	return &OrganizationService{log: log, pg: pg, access: access}
}

// SetRealtimeService 注入实时推送
func (s *OrganizationService) SetRealtimeService(rt *RealtimeService) { s.realtime = rt }

// SetAdminInbox 注入管理员收件箱
func (s *OrganizationService) SetAdminInbox(inbox *AdminInboxService) { s.inbox = inbox }

// SetApprovalService 注入审批服务（成员变更等场景会触发审批链）
func (s *OrganizationService) SetApprovalService(approval *OrgApprovalService) { s.approval = approval }

// AccessControl 暴露权限判定器，供其他服务复用
func (s *OrganizationService) AccessControl() *OrgAccessControl { return s.access }

// orgContext 一次解析出组织实体与调用者在其中的权限集
type orgContext struct {
	OrgID  int64
	UUID   string
	Org    *orgdomain.Organization
	Access *OrgAccess
}

func (s *OrganizationService) orgContext(ctx context.Context, access *admindomain.AccessContext, orgUUID, permission string) (*orgContext, error) {
	org, err := s.pg.GetOrganization(ctx, orgUUID)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, pgrepo.ErrOrgNotFound
	}
	orgAccess, err := s.access.Require(ctx, access, org.ID, org.UUID, permission)
	if err != nil {
		return nil, err
	}
	return &orgContext{OrgID: org.ID, UUID: org.UUID, Org: org, Access: orgAccess}, nil
}

// assertWritable 归档的组织一律只读 —— 归档的意义就是「冻结现状留作查档」，
// 允许继续写入等于没归档。
func assertWritable(org *orgdomain.Organization) error {
	if org == nil {
		return pgrepo.ErrOrgNotFound
	}
	if org.Status == "archived" {
		return apperrors.New(40944, http.StatusConflict, "该组织已归档，仅可查看")
	}
	return nil
}

// ── 组织 ──

// ListOrganizations 组织列表。非超管 / 非平台管理员只能看到自己所属的组织。
func (s *OrganizationService) ListOrganizations(ctx context.Context, access *admindomain.AccessContext, q orgdomain.OrgListQuery) (*orgdomain.Page[orgdomain.Organization], error) {
	if access == nil {
		return nil, apperrors.New(40110, http.StatusUnauthorized, "管理员未认证")
	}
	visibleTo := access.AdminID
	if s.isPlatformScope(ctx, access) && !q.OnlyMine {
		visibleTo = 0
	}
	page, err := s.pg.ListOrganizations(ctx, q, visibleTo)
	if err != nil {
		return nil, err
	}
	return s.decorateViewerRole(ctx, access, page)
}

// OrganizationTree 组织层级树
func (s *OrganizationService) OrganizationTree(ctx context.Context, access *admindomain.AccessContext) ([]orgdomain.OrganizationNode, error) {
	if access == nil {
		return nil, apperrors.New(40110, http.StatusUnauthorized, "管理员未认证")
	}
	visibleTo := access.AdminID
	if s.isPlatformScope(ctx, access) {
		visibleTo = 0
	}
	orgs, err := s.pg.ListAllOrganizations(ctx, visibleTo)
	if err != nil {
		return nil, err
	}
	return orgdomain.BuildOrganizationTree(orgs), nil
}

// GetOrganization 组织详情
func (s *OrganizationService) GetOrganization(ctx context.Context, access *admindomain.AccessContext, orgUUID string) (*orgdomain.Organization, *OrgAccess, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermOrgRead)
	if err != nil {
		return nil, nil, err
	}
	oc.Org.ViewerRole = oc.Access.OrgRole
	return oc.Org, oc.Access, nil
}

// GetOverview 组织概览（控制台首屏）
func (s *OrganizationService) GetOverview(ctx context.Context, access *admindomain.AccessContext, orgUUID string) (*orgdomain.Overview, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermOrgRead)
	if err != nil {
		return nil, err
	}
	stats, breakdown, err := s.pg.GetOrgOverview(ctx, oc.OrgID)
	if err != nil {
		return nil, err
	}
	oc.Org.ViewerRole = oc.Access.OrgRole

	overview := &orgdomain.Overview{
		Organization:  *oc.Org,
		Stats:         *stats,
		RoleBreakdown: breakdown,
		TopDepts:      []orgdomain.DeptRef{},
		RecentLogs:    []orgdomain.ActivityLog{},
	}

	depts, err := s.pg.ListDepartments(ctx, oc.OrgID, "")
	if err != nil {
		return nil, err
	}
	// 人数最多的前 5 个部门
	sortDeptsByMembers(depts)
	for i, d := range depts {
		if i >= 5 {
			break
		}
		overview.TopDepts = append(overview.TopDepts, orgdomain.DeptRef{UUID: d.UUID, Name: d.Name})
	}

	if oc.Access.Can(orgdomain.PermActivityRead) {
		logs, err := s.pg.ListOrgActivity(ctx, oc.OrgID, "", 1, 10)
		if err != nil {
			return nil, err
		}
		overview.RecentLogs = logs.Items
	}
	return overview, nil
}

func sortDeptsByMembers(depts []orgdomain.Department) {
	for i := 1; i < len(depts); i++ {
		for j := i; j > 0 && depts[j].TotalMemberCount > depts[j-1].TotalMemberCount; j-- {
			depts[j], depts[j-1] = depts[j-1], depts[j]
		}
	}
}

// CreateOrganization 创建组织。
//
// 建根组织是平台级动作（只有超管 / 平台管理员能做）；
// 建子组织则要求调用者在父组织里有 org:write —— 这样「分公司自己开分部」
// 不必每次找平台超管。
func (s *OrganizationService) CreateOrganization(ctx context.Context, access *admindomain.AccessContext, input orgdomain.CreateOrgInput) (*orgdomain.Organization, error) {
	if access == nil {
		return nil, apperrors.New(40110, http.StatusUnauthorized, "管理员未认证")
	}
	if err := validateOrgInput(input); err != nil {
		return nil, err
	}

	if input.ParentUUID != "" {
		if _, err := s.orgContext(ctx, access, input.ParentUUID, orgdomain.PermOrgWrite); err != nil {
			return nil, err
		}
	} else if !s.isPlatformScope(ctx, access) {
		return nil, apperrors.New(40399, http.StatusForbidden, "只有平台管理员可以创建顶级组织")
	}

	ownerID := input.OwnerAdminID
	if ownerID <= 0 {
		ownerID = access.AdminID
	}

	org, err := s.pg.CreateOrganization(ctx, input, ownerID, access.AdminID)
	if err != nil {
		return nil, err
	}
	s.recordActivity(ctx, org.ID, access.AdminID, "org.create", "organization", org.UUID,
		"创建组织 "+org.Name, map[string]any{"code": org.Code, "kind": org.Kind})
	return org, nil
}

// UpdateOrganization 更新组织资料
func (s *OrganizationService) UpdateOrganization(ctx context.Context, access *admindomain.AccessContext, orgUUID string, input orgdomain.UpdateOrgInput) (*orgdomain.Organization, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermOrgWrite)
	if err != nil {
		return nil, err
	}
	if input.Status != nil {
		if !orgdomain.IsValidStatus(*input.Status) {
			return nil, apperrors.New(40056, http.StatusBadRequest, "组织状态取值无效")
		}
		// 归档 / 恢复归档是所有者级动作
		if *input.Status == "archived" || oc.Org.Status == "archived" {
			if err := s.access.RequireRoleAtLeast(oc.Access, orgdomain.RoleOwner); err != nil {
				return nil, err
			}
		}
	} else if err := assertWritable(oc.Org); err != nil {
		return nil, err
	}
	if input.Kind != nil && !orgdomain.IsValidOrgKind(*input.Kind) {
		return nil, apperrors.New(40057, http.StatusBadRequest, "组织类型取值无效")
	}
	// 配额只有平台侧能改 —— 否则组织自己把上限调高，配额就形同虚设
	if input.Quota != nil && !oc.Access.IsSuperAdmin && !oc.Access.IsPlatform {
		return nil, apperrors.New(40390, http.StatusForbidden, "组织配额只能由平台管理员调整")
	}

	org, err := s.pg.UpdateOrganization(ctx, oc.OrgID, input, access.AdminID)
	if err != nil {
		return nil, err
	}
	s.recordActivity(ctx, oc.OrgID, access.AdminID, "org.update", "organization", org.UUID,
		"更新组织资料", nil)
	return org, nil
}

// TransferOwnership 转让组织所有权
func (s *OrganizationService) TransferOwnership(ctx context.Context, access *admindomain.AccessContext, orgUUID string, newOwnerID int64) error {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermOrgTransfer)
	if err != nil {
		return err
	}
	if err := assertWritable(oc.Org); err != nil {
		return err
	}
	// 组织内部转让必须由现任所有者发起；平台侧可以代为处理（原主人失联的场景）
	if !oc.Access.IsSuperAdmin && !oc.Access.IsPlatform {
		if err := s.access.RequireRoleAtLeast(oc.Access, orgdomain.RoleOwner); err != nil {
			return err
		}
	}
	if err := s.pg.TransferOrganizationOwner(ctx, oc.OrgID, newOwnerID); err != nil {
		return err
	}
	s.recordActivity(ctx, oc.OrgID, access.AdminID, "org.transfer", "organization", oc.UUID,
		fmt.Sprintf("转让组织所有权给管理员 #%d", newOwnerID), map[string]any{"newOwnerId": newOwnerID})
	s.notifyAdmins(ctx, []int64{newOwnerID}, "org.ownership.transferred", "组织所有权转让",
		fmt.Sprintf("你已成为组织「%s」的所有者", oc.Org.Name), map[string]any{"orgId": oc.UUID})
	return nil
}

// DeleteOrganization 删除组织。数据一并销毁，因此只给平台侧和组织所有者。
func (s *OrganizationService) DeleteOrganization(ctx context.Context, access *admindomain.AccessContext, orgUUID string) error {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermOrgDelete)
	if err != nil {
		return err
	}
	if !oc.Access.IsSuperAdmin && !oc.Access.IsPlatform {
		if err := s.access.RequireRoleAtLeast(oc.Access, orgdomain.RoleOwner); err != nil {
			return err
		}
	}
	if err := s.pg.DeleteOrganization(ctx, oc.OrgID); err != nil {
		return err
	}
	s.log.Warn("组织已删除",
		zap.String("orgId", oc.UUID), zap.String("name", oc.Org.Name), zap.Int64("actor", access.AdminID))
	return nil
}

// ── 部门 ──

// DepartmentTree 部门树
func (s *OrganizationService) DepartmentTree(ctx context.Context, access *admindomain.AccessContext, orgUUID, status string) ([]orgdomain.DepartmentNode, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermDeptRead)
	if err != nil {
		return nil, err
	}
	depts, err := s.pg.ListDepartments(ctx, oc.OrgID, status)
	if err != nil {
		return nil, err
	}
	// 被限定到某些部门的管理员，只看得到自己范围内的子树
	if oc.Access.ScopedToDepts() {
		filtered := make([]orgdomain.Department, 0, len(depts))
		for _, d := range depts {
			if oc.Access.AllowsDept(d.ID) {
				filtered = append(filtered, d)
			}
		}
		depts = filtered
	}
	return orgdomain.BuildDepartmentTree(depts), nil
}

// GetDepartment 部门详情
func (s *OrganizationService) GetDepartment(ctx context.Context, access *admindomain.AccessContext, orgUUID, deptUUID string) (*orgdomain.Department, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermDeptRead)
	if err != nil {
		return nil, err
	}
	deptID, err := s.pg.ResolveDeptID(ctx, oc.OrgID, deptUUID)
	if err != nil {
		return nil, err
	}
	if !oc.Access.AllowsDept(deptID) {
		return nil, apperrors.New(40391, http.StatusForbidden, "该部门不在你的管理范围内")
	}
	return s.pg.GetDepartment(ctx, oc.OrgID, deptID)
}

// CreateDepartment 创建部门
func (s *OrganizationService) CreateDepartment(ctx context.Context, access *admindomain.AccessContext, orgUUID string, input orgdomain.CreateDeptInput) (*orgdomain.Department, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermDeptWrite)
	if err != nil {
		return nil, err
	}
	if err := assertWritable(oc.Org); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Code) == "" {
		return nil, apperrors.New(40058, http.StatusBadRequest, "部门名称与代码不能为空")
	}
	if input.Kind != "" && !orgdomain.IsValidDeptKind(input.Kind) {
		return nil, apperrors.New(40059, http.StatusBadRequest, "部门类型取值无效")
	}
	if err := s.checkDeptQuota(ctx, oc); err != nil {
		return nil, err
	}
	if input.ParentUUID != "" {
		parentID, err := s.pg.ResolveDeptID(ctx, oc.OrgID, input.ParentUUID)
		if err != nil {
			return nil, err
		}
		if !oc.Access.AllowsDept(parentID) {
			return nil, apperrors.New(40391, http.StatusForbidden, "该部门不在你的管理范围内")
		}
	} else if oc.Access.ScopedToDepts() {
		return nil, apperrors.New(40392, http.StatusForbidden, "你的管理范围内不能创建顶级部门")
	}

	dept, err := s.pg.CreateDepartment(ctx, oc.OrgID, input)
	if err != nil {
		return nil, err
	}
	s.recordActivity(ctx, oc.OrgID, access.AdminID, "dept.create", "department", dept.UUID,
		"创建部门 "+dept.Name, map[string]any{"code": dept.Code})
	return dept, nil
}

// UpdateDepartment 更新部门
func (s *OrganizationService) UpdateDepartment(ctx context.Context, access *admindomain.AccessContext, orgUUID, deptUUID string, input orgdomain.UpdateDeptInput) (*orgdomain.Department, error) {
	oc, deptID, err := s.deptContext(ctx, access, orgUUID, deptUUID, orgdomain.PermDeptWrite)
	if err != nil {
		return nil, err
	}
	if input.Kind != nil && !orgdomain.IsValidDeptKind(*input.Kind) {
		return nil, apperrors.New(40059, http.StatusBadRequest, "部门类型取值无效")
	}
	dept, err := s.pg.UpdateDepartment(ctx, oc.OrgID, deptID, input)
	if err != nil {
		return nil, err
	}
	s.recordActivity(ctx, oc.OrgID, access.AdminID, "dept.update", "department", dept.UUID,
		"更新部门 "+dept.Name, nil)
	return dept, nil
}

// MoveDepartment 移动部门（含子树）
func (s *OrganizationService) MoveDepartment(ctx context.Context, access *admindomain.AccessContext, orgUUID, deptUUID string, input orgdomain.MoveDeptInput) error {
	oc, deptID, err := s.deptContext(ctx, access, orgUUID, deptUUID, orgdomain.PermDeptWrite)
	if err != nil {
		return err
	}
	if input.ParentUUID != "" {
		targetID, err := s.pg.ResolveDeptID(ctx, oc.OrgID, input.ParentUUID)
		if err != nil {
			return err
		}
		if !oc.Access.AllowsDept(targetID) {
			return apperrors.New(40391, http.StatusForbidden, "目标上级部门不在你的管理范围内")
		}
	}
	if err := s.pg.MoveDepartment(ctx, oc.OrgID, deptID, input); err != nil {
		return err
	}
	s.recordActivity(ctx, oc.OrgID, access.AdminID, "dept.move", "department", deptUUID,
		"移动部门", map[string]any{"parentId": input.ParentUUID})
	return nil
}

// DeleteDepartment 删除部门
func (s *OrganizationService) DeleteDepartment(ctx context.Context, access *admindomain.AccessContext, orgUUID, deptUUID string, strategy orgdomain.DeleteDeptStrategy) error {
	oc, deptID, err := s.deptContext(ctx, access, orgUUID, deptUUID, orgdomain.PermDeptWrite)
	if err != nil {
		return err
	}
	dept, err := s.pg.GetDepartment(ctx, oc.OrgID, deptID)
	if err != nil {
		return err
	}
	// 级联删除会连带干掉整棵子树，风险等级明显高于删一个空部门
	if strategy == orgdomain.DeleteCascade && !oc.Access.IsSuperAdmin && !oc.Access.IsPlatform {
		if err := s.access.RequireRoleAtLeast(oc.Access, orgdomain.RoleAdmin); err != nil {
			return err
		}
	}
	if err := s.pg.DeleteDepartment(ctx, oc.OrgID, deptID, strategy); err != nil {
		return err
	}
	name := ""
	if dept != nil {
		name = dept.Name
	}
	s.recordActivity(ctx, oc.OrgID, access.AdminID, "dept.delete", "department", deptUUID,
		"删除部门 "+name, map[string]any{"strategy": string(strategy)})
	return nil
}

// ReorderDepartments 批量调整同级顺序（前端拖拽后一次提交）
func (s *OrganizationService) ReorderDepartments(ctx context.Context, access *admindomain.AccessContext, orgUUID string, order map[string]int) error {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermDeptWrite)
	if err != nil {
		return err
	}
	if err := assertWritable(oc.Org); err != nil {
		return err
	}
	return s.pg.ReorderDepartments(ctx, oc.OrgID, order)
}

// deptContext 解析部门并校验组织归属 + 管理范围 + 组织可写
func (s *OrganizationService) deptContext(ctx context.Context, access *admindomain.AccessContext, orgUUID, deptUUID, permission string) (*orgContext, int64, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, permission)
	if err != nil {
		return nil, 0, err
	}
	if err := assertWritable(oc.Org); err != nil {
		return nil, 0, err
	}
	deptID, err := s.pg.ResolveDeptID(ctx, oc.OrgID, deptUUID)
	if err != nil {
		return nil, 0, err
	}
	if !oc.Access.AllowsDept(deptID) {
		return nil, 0, apperrors.New(40391, http.StatusForbidden, "该部门不在你的管理范围内")
	}
	return oc, deptID, nil
}

// ── 应用绑定 ──

// ListOrgApps 组织绑定的应用
func (s *OrganizationService) ListOrgApps(ctx context.Context, access *admindomain.AccessContext, orgUUID string) ([]orgdomain.AppBinding, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermAppRead)
	if err != nil {
		return nil, err
	}
	return s.pg.ListOrgApps(ctx, oc.OrgID)
}

// BindOrgApp 绑定应用。转移归属属于平台级动作 —— 应用是平台资源，
// 组织管理员只能申请授权访问，不能把别家的应用划到自己名下。
func (s *OrganizationService) BindOrgApp(ctx context.Context, access *admindomain.AccessContext, orgUUID string, appID int64, owned bool) error {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermAppBind)
	if err != nil {
		return err
	}
	if err := assertWritable(oc.Org); err != nil {
		return err
	}
	if owned && !oc.Access.IsSuperAdmin && !oc.Access.IsPlatform {
		return apperrors.New(40393, http.StatusForbidden, "应用归属只能由平台管理员调整")
	}
	if oc.Org.Quota.AppLimit > 0 {
		apps, err := s.pg.ListOrgApps(ctx, oc.OrgID)
		if err != nil {
			return err
		}
		if len(apps) >= oc.Org.Quota.AppLimit {
			return apperrors.New(40945, http.StatusConflict,
				fmt.Sprintf("已达组织应用配额上限（%d）", oc.Org.Quota.AppLimit))
		}
	}
	if err := s.pg.BindOrgApp(ctx, oc.OrgID, appID, owned); err != nil {
		return err
	}
	s.recordActivity(ctx, oc.OrgID, access.AdminID, "org.app_bind", "app", fmt.Sprint(appID),
		fmt.Sprintf("绑定应用 #%d", appID), map[string]any{"owned": owned})
	return nil
}

// UnbindOrgApp 解绑应用
func (s *OrganizationService) UnbindOrgApp(ctx context.Context, access *admindomain.AccessContext, orgUUID string, appID int64) error {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermAppBind)
	if err != nil {
		return err
	}
	if err := assertWritable(oc.Org); err != nil {
		return err
	}
	if err := s.pg.UnbindOrgApp(ctx, oc.OrgID, appID); err != nil {
		return err
	}
	s.recordActivity(ctx, oc.OrgID, access.AdminID, "org.app_unbind", "app", fmt.Sprint(appID),
		fmt.Sprintf("解绑应用 #%d", appID), nil)
	return nil
}

// ── 活动日志 ──

// ListActivity 组织操作日志
func (s *OrganizationService) ListActivity(ctx context.Context, access *admindomain.AccessContext, orgUUID, action string, page, limit int) (*orgdomain.Page[orgdomain.ActivityLog], error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermActivityRead)
	if err != nil {
		return nil, err
	}
	return s.pg.ListOrgActivity(ctx, oc.OrgID, action, page, limit)
}

// ── 内部辅助 ──

// isPlatformScope 调用者是否具备跨组织视角
func (s *OrganizationService) isPlatformScope(ctx context.Context, access *admindomain.AccessContext) bool {
	if access == nil {
		return false
	}
	if access.IsSuperAdmin {
		return true
	}
	if s.access == nil || s.access.admin == nil {
		return false
	}
	return s.access.admin.Authorize(ctx, access, orgdomain.PermOrgWrite, nil) == nil
}

// decorateViewerRole 给列表里的每个组织补上「我在其中的角色」
func (s *OrganizationService) decorateViewerRole(ctx context.Context, access *admindomain.AccessContext, page *orgdomain.Page[orgdomain.Organization]) (*orgdomain.Page[orgdomain.Organization], error) {
	if page == nil || len(page.Items) == 0 {
		return page, nil
	}
	for i := range page.Items {
		role, err := s.pg.GetMemberRole(ctx, page.Items[i].ID, access.AdminID)
		if err != nil {
			return nil, err
		}
		page.Items[i].ViewerRole = role
	}
	return page, nil
}

func (s *OrganizationService) checkDeptQuota(ctx context.Context, oc *orgContext) error {
	if oc.Org.Quota.DeptLimit <= 0 {
		return nil
	}
	count, err := s.pg.CountOrgDepartments(ctx, oc.OrgID)
	if err != nil {
		return err
	}
	if count >= oc.Org.Quota.DeptLimit {
		return apperrors.New(40946, http.StatusConflict,
			fmt.Sprintf("已达组织部门配额上限（%d）", oc.Org.Quota.DeptLimit))
	}
	return nil
}

// recordActivity 写操作留痕。失败只记日志 —— 业务已经做完了，
// 此时报错会让调用方以为操作没成功而重试。
func (s *OrganizationService) recordActivity(ctx context.Context, orgID, actorID int64, action, targetType, targetID, summary string, detail map[string]any) {
	if err := s.pg.RecordOrgActivity(context.WithoutCancel(ctx), orgID, actorID, action, targetType, targetID, summary, detail); err != nil {
		s.log.Warn("组织操作留痕失败", zap.Error(err), zap.String("action", action))
	}
}

// notifyAdmins 给管理员发站内信 + 实时推送。
// 管理员的实时命名空间 appid 恒为 0，与应用用户的主键空间互不相通。
func (s *OrganizationService) notifyAdmins(ctx context.Context, adminIDs []int64, event, title, content string, data map[string]any) {
	if len(adminIDs) == 0 {
		return
	}
	if s.inbox != nil {
		pushes := make([]notificationdomain.AdminInboxPush, 0, len(adminIDs))
		for _, id := range adminIDs {
			pushes = append(pushes, notificationdomain.AdminInboxPush{
				AdminID: id, Type: event, Title: title, Content: content,
				Level: "info", Resource: "organization", Metadata: data,
			})
		}
		if _, err := s.inbox.Push(context.WithoutCancel(ctx), pushes); err != nil {
			s.log.Warn("组织通知写入收件箱失败", zap.Error(err), zap.String("event", event))
		}
	}
	if s.realtime == nil {
		return
	}
	payload := map[string]any{"title": title, "content": content}
	for k, v := range data {
		payload[k] = v
	}
	for _, id := range adminIDs {
		adminID := id
		go func() {
			pushCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := s.realtime.PublishUserEvent(pushCtx, 0, adminID, event, payload); err != nil {
				s.log.Warn("组织事件推送失败", zap.Error(err), zap.Int64("adminId", adminID))
			}
		}()
	}
}

func validateOrgInput(input orgdomain.CreateOrgInput) error {
	if strings.TrimSpace(input.Name) == "" {
		return apperrors.New(40060, http.StatusBadRequest, "组织名称不能为空")
	}
	if strings.TrimSpace(input.Code) == "" {
		return apperrors.New(40061, http.StatusBadRequest, "组织代码不能为空")
	}
	if input.Kind != "" && !orgdomain.IsValidOrgKind(input.Kind) {
		return apperrors.New(40057, http.StatusBadRequest, "组织类型取值无效")
	}
	return nil
}
