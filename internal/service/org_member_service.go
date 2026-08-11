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
)

// 组织成员 / 邀请 / 岗位 / 角色，都挂在 OrganizationService 上，
// 共用同一套 orgContext 权限解析，不另起服务 —— 拆开只会让「谁能改成员」
// 这条判定在两个地方各写一遍。

// ── 组织成员 ──

// ListMembers 组织成员列表
func (s *OrganizationService) ListMembers(ctx context.Context, access *admindomain.AccessContext, orgUUID string, q orgdomain.MemberListQuery) (*orgdomain.Page[orgdomain.Member], error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermMemberRead)
	if err != nil {
		return nil, err
	}
	// 被限定部门范围的管理员，成员列表也跟着收敛
	if oc.Access.ScopedToDepts() && q.DeptUUID == "" {
		depts, err := s.scopedDeptUUIDs(ctx, oc)
		if err != nil {
			return nil, err
		}
		if len(depts) == 0 {
			return &orgdomain.Page[orgdomain.Member]{Items: []orgdomain.Member{}, Page: 1, Limit: q.Limit}, nil
		}
		q.DeptUUID = depts[0]
		q.IncludeSubDept = true
	}
	return s.pg.ListOrgMembers(ctx, oc.OrgID, q)
}

// GetMember 组织成员详情
func (s *OrganizationService) GetMember(ctx context.Context, access *admindomain.AccessContext, orgUUID string, adminID int64) (*orgdomain.Member, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermMemberRead)
	if err != nil {
		return nil, err
	}
	member, err := s.pg.GetOrgMember(ctx, oc.OrgID, adminID)
	if err != nil {
		return nil, err
	}
	if member == nil {
		return nil, pgrepo.ErrMemberNotFound
	}
	return member, nil
}

// AddMember 直接把管理员加入组织（跳过邀请）
func (s *OrganizationService) AddMember(ctx context.Context, access *admindomain.AccessContext, orgUUID string, input orgdomain.AddMemberInput) (*orgdomain.Member, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermMemberWrite)
	if err != nil {
		return nil, err
	}
	if err := assertWritable(oc.Org); err != nil {
		return nil, err
	}
	if input.AdminID <= 0 {
		return nil, apperrors.New(40062, http.StatusBadRequest, "请指定要加入的管理员")
	}
	// 不能凭空造出一个比自己权限更大的成员
	if input.OrgRole != "" {
		if err := s.access.CanActOnMember(oc.Access, input.OrgRole); err != nil {
			return nil, err
		}
	}
	if err := s.checkMemberQuota(ctx, oc); err != nil {
		return nil, err
	}

	member, err := s.pg.AddOrgMember(ctx, oc.OrgID, input, access.AdminID)
	if err != nil {
		return nil, err
	}
	s.recordActivity(ctx, oc.OrgID, access.AdminID, "member.add", "member", fmt.Sprint(input.AdminID),
		fmt.Sprintf("将 %s 加入组织", member.DisplayName), map[string]any{"orgRole": member.OrgRole})
	s.notifyAdmins(ctx, []int64{input.AdminID}, "org.member.added", "加入组织",
		fmt.Sprintf("你已被加入组织「%s」", oc.Org.Name), map[string]any{"orgId": oc.UUID})
	return member, nil
}

// UpdateMember 更新成员档案（角色 / 工号 / 职位 / 状态）
func (s *OrganizationService) UpdateMember(ctx context.Context, access *admindomain.AccessContext, orgUUID string, adminID int64, input orgdomain.UpdateMemberInput) (*orgdomain.Member, error) {
	oc, current, err := s.memberContext(ctx, access, orgUUID, adminID, orgdomain.PermMemberWrite)
	if err != nil {
		return nil, err
	}
	if input.OrgRole != nil {
		if !orgdomain.IsValidRole(*input.OrgRole) {
			return nil, apperrors.New(40063, http.StatusBadRequest, "组织角色取值无效")
		}
		// 目标角色也要在自己能操作的范围内，否则等于自助提权
		if err := s.access.CanActOnMember(oc.Access, *input.OrgRole); err != nil {
			return nil, err
		}
	}
	if input.Status != nil && !orgdomain.IsValidMemberStatus(*input.Status) {
		return nil, apperrors.New(40064, http.StatusBadRequest, "成员状态取值无效")
	}

	member, err := s.pg.UpdateOrgMember(ctx, oc.OrgID, adminID, input)
	if err != nil {
		return nil, err
	}
	s.recordActivity(ctx, oc.OrgID, access.AdminID, "member.update", "member", fmt.Sprint(adminID),
		fmt.Sprintf("更新成员 %s", current.DisplayName), nil)
	return member, nil
}

// RemoveMember 移出组织
func (s *OrganizationService) RemoveMember(ctx context.Context, access *admindomain.AccessContext, orgUUID string, adminID int64) error {
	oc, current, err := s.memberContext(ctx, access, orgUUID, adminID, orgdomain.PermMemberWrite)
	if err != nil {
		return err
	}
	if err := s.pg.RemoveOrgMember(ctx, oc.OrgID, adminID); err != nil {
		return err
	}
	s.recordActivity(ctx, oc.OrgID, access.AdminID, "member.remove", "member", fmt.Sprint(adminID),
		fmt.Sprintf("将 %s 移出组织", current.DisplayName), nil)
	s.notifyAdmins(ctx, []int64{adminID}, "org.member.removed", "退出组织",
		fmt.Sprintf("你已被移出组织「%s」", oc.Org.Name), map[string]any{"orgId": oc.UUID})
	return nil
}

// AssignMemberDepartments 调整成员的部门归属
func (s *OrganizationService) AssignMemberDepartments(ctx context.Context, access *admindomain.AccessContext, orgUUID string, adminID int64, input orgdomain.AssignDeptInput) error {
	oc, _, err := s.memberContext(ctx, access, orgUUID, adminID, orgdomain.PermMemberWrite)
	if err != nil {
		return err
	}
	for _, deptUUID := range input.DeptUUIDs {
		deptID, err := s.pg.ResolveDeptID(ctx, oc.OrgID, deptUUID)
		if err != nil {
			return err
		}
		if !oc.Access.AllowsDept(deptID) {
			return apperrors.New(40391, http.StatusForbidden, "该部门不在你的管理范围内")
		}
	}
	if err := s.pg.AssignMemberDepartments(ctx, oc.OrgID, adminID, input); err != nil {
		return err
	}
	s.recordActivity(ctx, oc.OrgID, access.AdminID, "member.assign_dept", "member", fmt.Sprint(adminID),
		"调整成员部门归属", map[string]any{"deptIds": input.DeptUUIDs, "replace": input.Replace})
	return nil
}

// memberContext 解析成员并校验「能否操作这个人」
func (s *OrganizationService) memberContext(ctx context.Context, access *admindomain.AccessContext, orgUUID string, adminID int64, permission string) (*orgContext, *orgdomain.Member, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, permission)
	if err != nil {
		return nil, nil, err
	}
	if err := assertWritable(oc.Org); err != nil {
		return nil, nil, err
	}
	member, err := s.pg.GetOrgMember(ctx, oc.OrgID, adminID)
	if err != nil {
		return nil, nil, err
	}
	if member == nil {
		return nil, nil, pgrepo.ErrMemberNotFound
	}
	// 改自己的档案不受层级限制（改不了自己的角色，那由 CanActOnMember 之外的角色校验兜住）
	if adminID != access.AdminID {
		if err := s.access.CanActOnMember(oc.Access, member.OrgRole); err != nil {
			return nil, nil, err
		}
	}
	return oc, member, nil
}

func (s *OrganizationService) checkMemberQuota(ctx context.Context, oc *orgContext) error {
	if oc.Org.Quota.MemberLimit <= 0 {
		return nil
	}
	count, err := s.pg.CountOrgMembers(ctx, oc.OrgID)
	if err != nil {
		return err
	}
	if count >= oc.Org.Quota.MemberLimit {
		return apperrors.New(40947, http.StatusConflict,
			fmt.Sprintf("已达组织成员配额上限（%d）", oc.Org.Quota.MemberLimit))
	}
	return nil
}

func (s *OrganizationService) scopedDeptUUIDs(ctx context.Context, oc *orgContext) ([]string, error) {
	depts, err := s.pg.ListDepartments(ctx, oc.OrgID, "")
	if err != nil {
		return nil, err
	}
	var uuids []string
	for _, d := range depts {
		if oc.Access.AllowsDept(d.ID) {
			uuids = append(uuids, d.UUID)
		}
	}
	return uuids, nil
}

// SearchAssignableAdmins 成员选择器：搜索可加入组织的管理员。
// 需要 org:member:write —— 能看到平台管理员名录本身就是敏感信息。
func (s *OrganizationService) SearchAssignableAdmins(ctx context.Context, access *admindomain.AccessContext, orgUUID, keyword string, excludeExisting bool, limit int) ([]orgdomain.Member, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermMemberWrite)
	if err != nil {
		return nil, err
	}
	excludeOrgID := int64(0)
	if excludeExisting {
		excludeOrgID = oc.OrgID
	}
	return s.pg.SearchAssignableAdmins(ctx, keyword, excludeOrgID, limit)
}

// ── 部门成员 ──

// ListDepartmentMembers 部门内成员
func (s *OrganizationService) ListDepartmentMembers(ctx context.Context, access *admindomain.AccessContext, orgUUID, deptUUID string) ([]orgdomain.DepartmentMember, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermMemberRead)
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
	return s.pg.ListDepartmentMembers(ctx, oc.OrgID, deptID)
}

// SetDepartmentMember 设置部门内成员属性（岗位 / 汇报线 / 代理 / 负责人）
func (s *OrganizationService) SetDepartmentMember(ctx context.Context, access *admindomain.AccessContext, orgUUID, deptUUID string, adminID int64, input orgdomain.SetDeptMemberInput) error {
	oc, deptID, err := s.deptContext(ctx, access, orgUUID, deptUUID, orgdomain.PermMemberWrite)
	if err != nil {
		return err
	}
	// 任命部门负责人是人事动作，要求主管及以上
	if input.IsLeader != nil && *input.IsLeader {
		if err := s.access.RequireRoleAtLeast(oc.Access, orgdomain.RoleManager); err != nil {
			return err
		}
	}
	if err := s.pg.SetDepartmentMember(ctx, oc.OrgID, deptID, adminID, input); err != nil {
		return err
	}
	s.recordActivity(ctx, oc.OrgID, access.AdminID, "dept.member_update", "member", fmt.Sprint(adminID),
		"更新部门成员属性", map[string]any{"deptId": deptUUID})
	return nil
}

// RemoveDepartmentMember 把成员移出部门（保留组织籍）
func (s *OrganizationService) RemoveDepartmentMember(ctx context.Context, access *admindomain.AccessContext, orgUUID, deptUUID string, adminID int64) error {
	oc, deptID, err := s.deptContext(ctx, access, orgUUID, deptUUID, orgdomain.PermMemberWrite)
	if err != nil {
		return err
	}
	if err := s.pg.RemoveDepartmentMember(ctx, oc.OrgID, deptID, adminID); err != nil {
		return err
	}
	s.recordActivity(ctx, oc.OrgID, access.AdminID, "dept.member_remove", "member", fmt.Sprint(adminID),
		"将成员移出部门", map[string]any{"deptId": deptUUID})
	return nil
}

// ReportingChain 汇报链
func (s *OrganizationService) ReportingChain(ctx context.Context, access *admindomain.AccessContext, orgUUID, deptUUID string, adminID int64) ([]orgdomain.ReportingNode, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermMemberRead)
	if err != nil {
		return nil, err
	}
	deptID, err := s.pg.ResolveDeptID(ctx, oc.OrgID, deptUUID)
	if err != nil {
		return nil, err
	}
	return s.pg.GetReportingChain(ctx, oc.OrgID, deptID, adminID)
}

// DirectReports 直接下属
func (s *OrganizationService) DirectReports(ctx context.Context, access *admindomain.AccessContext, orgUUID, deptUUID string, adminID int64) ([]orgdomain.ReportingNode, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermMemberRead)
	if err != nil {
		return nil, err
	}
	deptID, err := s.pg.ResolveDeptID(ctx, oc.OrgID, deptUUID)
	if err != nil {
		return nil, err
	}
	return s.pg.ListDirectReports(ctx, deptID, adminID)
}

// ── 邀请 ──

// InviteMembers 发起邀请（可同时邀多人）
func (s *OrganizationService) InviteMembers(ctx context.Context, access *admindomain.AccessContext, orgUUID string, input orgdomain.InviteInput) ([]orgdomain.Invitation, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermMemberInvite)
	if err != nil {
		return nil, err
	}
	if err := assertWritable(oc.Org); err != nil {
		return nil, err
	}
	if len(input.AdminIDs) == 0 {
		return nil, apperrors.New(40065, http.StatusBadRequest, "请至少选择一位受邀人")
	}
	if len(input.AdminIDs) > 200 {
		return nil, apperrors.New(40066, http.StatusBadRequest, "单次最多邀请 200 人")
	}
	if input.OrgRole != "" {
		if err := s.access.CanActOnMember(oc.Access, input.OrgRole); err != nil {
			return nil, err
		}
	}
	if input.DeptUUID != "" {
		deptID, err := s.pg.ResolveDeptID(ctx, oc.OrgID, input.DeptUUID)
		if err != nil {
			return nil, err
		}
		if !oc.Access.AllowsDept(deptID) {
			return nil, apperrors.New(40391, http.StatusForbidden, "该部门不在你的管理范围内")
		}
	}

	invitations, err := s.pg.CreateInvitations(ctx, oc.OrgID, input, access.AdminID)
	if err != nil {
		return nil, err
	}
	for _, inv := range invitations {
		target := inv.OrgName
		if inv.DeptName != "" {
			target = inv.OrgName + " · " + inv.DeptName
		}
		s.notifyAdmins(ctx, []int64{inv.InviteeID}, "org.invitation.received", "组织邀请",
			fmt.Sprintf("%s 邀请你加入 %s", inv.InviterName, target),
			map[string]any{"invitationId": inv.UUID, "orgId": inv.OrgUUID, "deptId": inv.DeptUUID})
	}
	if len(invitations) > 0 {
		s.recordActivity(ctx, oc.OrgID, access.AdminID, "member.invite", "invitation", "",
			fmt.Sprintf("发出 %d 份邀请", len(invitations)), nil)
	}
	return invitations, nil
}

// ListMyInvitations 我收到 / 发出的邀请
func (s *OrganizationService) ListMyInvitations(ctx context.Context, access *admindomain.AccessContext, q orgdomain.InvitationQuery) (*orgdomain.Page[orgdomain.Invitation], error) {
	if access == nil {
		return nil, apperrors.New(40110, http.StatusUnauthorized, "管理员未认证")
	}
	q.AdminID = access.AdminID
	return s.pg.ListInvitations(ctx, q)
}

// CountPendingInvitations 待处理邀请数
func (s *OrganizationService) CountPendingInvitations(ctx context.Context, access *admindomain.AccessContext) (int64, error) {
	if access == nil {
		return 0, apperrors.New(40110, http.StatusUnauthorized, "管理员未认证")
	}
	return s.pg.CountPendingInvitations(ctx, access.AdminID)
}

// RespondInvitation 接受 / 拒绝 / 取消邀请。
// 权限不看组织角色 —— 被邀请的人本来就还不在组织里。
func (s *OrganizationService) RespondInvitation(ctx context.Context, access *admindomain.AccessContext, inviteUUID, action string) (*orgdomain.Invitation, error) {
	if access == nil {
		return nil, apperrors.New(40110, http.StatusUnauthorized, "管理员未认证")
	}
	switch action {
	case "accepted":
		inv, err := s.pg.AcceptInvitation(ctx, inviteUUID, access.AdminID)
		if err != nil {
			return nil, err
		}
		orgID, err := s.pg.ResolveOrgID(ctx, inv.OrgUUID)
		if err == nil {
			s.recordActivity(ctx, orgID, access.AdminID, "member.invite_accept", "invitation", inv.UUID,
				fmt.Sprintf("%s 接受了邀请", inv.InviteeName), nil)
		}
		s.notifyAdmins(ctx, []int64{inv.InviterID}, "org.invitation.responded", "邀请已被接受",
			fmt.Sprintf("%s 接受了你的邀请", inv.InviteeName),
			map[string]any{"invitationId": inv.UUID, "status": "accepted"})
		return inv, nil

	case "rejected", "cancelled":
		inv, err := s.pg.RespondInvitation(ctx, inviteUUID, access.AdminID, action)
		if err != nil {
			return nil, err
		}
		if action == "rejected" {
			s.notifyAdmins(ctx, []int64{inv.InviterID}, "org.invitation.responded", "邀请被拒绝",
				fmt.Sprintf("%s 拒绝了你的邀请", inv.InviteeName),
				map[string]any{"invitationId": inv.UUID, "status": "rejected"})
		}
		return inv, nil

	default:
		return nil, apperrors.New(40067, http.StatusBadRequest, "无效的邀请操作")
	}
}

// ── 岗位 ──

// ListPositions 组织岗位
func (s *OrganizationService) ListPositions(ctx context.Context, access *admindomain.AccessContext, orgUUID string) ([]orgdomain.Position, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermPositionRead)
	if err != nil {
		return nil, err
	}
	return s.pg.ListPositions(ctx, oc.OrgID)
}

// CreatePosition 创建岗位
func (s *OrganizationService) CreatePosition(ctx context.Context, access *admindomain.AccessContext, orgUUID string, input orgdomain.PositionInput) (*orgdomain.Position, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermPositionWrite)
	if err != nil {
		return nil, err
	}
	if err := assertWritable(oc.Org); err != nil {
		return nil, err
	}
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Code) == "" {
		return nil, apperrors.New(40068, http.StatusBadRequest, "岗位名称与代码不能为空")
	}
	pos, err := s.pg.CreatePosition(ctx, oc.OrgID, input)
	if err != nil {
		return nil, err
	}
	s.recordActivity(ctx, oc.OrgID, access.AdminID, "position.create", "position", pos.UUID,
		"创建岗位 "+pos.Name, nil)
	return pos, nil
}

// UpdatePosition 更新岗位
func (s *OrganizationService) UpdatePosition(ctx context.Context, access *admindomain.AccessContext, orgUUID, posUUID string, input orgdomain.PositionInput) (*orgdomain.Position, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermPositionWrite)
	if err != nil {
		return nil, err
	}
	if err := assertWritable(oc.Org); err != nil {
		return nil, err
	}
	posID, err := s.pg.ResolvePositionID(ctx, oc.OrgID, posUUID)
	if err != nil {
		return nil, err
	}
	pos, err := s.pg.UpdatePosition(ctx, oc.OrgID, posID, input)
	if err != nil {
		return nil, err
	}
	s.recordActivity(ctx, oc.OrgID, access.AdminID, "position.update", "position", pos.UUID,
		"更新岗位 "+pos.Name, nil)
	return pos, nil
}

// DeletePosition 删除岗位
func (s *OrganizationService) DeletePosition(ctx context.Context, access *admindomain.AccessContext, orgUUID, posUUID string) error {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermPositionWrite)
	if err != nil {
		return err
	}
	if err := assertWritable(oc.Org); err != nil {
		return err
	}
	posID, err := s.pg.ResolvePositionID(ctx, oc.OrgID, posUUID)
	if err != nil {
		return err
	}
	if err := s.pg.DeletePosition(ctx, oc.OrgID, posID); err != nil {
		return err
	}
	s.recordActivity(ctx, oc.OrgID, access.AdminID, "position.delete", "position", posUUID, "删除岗位", nil)
	return nil
}

// ── 组织角色 ──

// ListRoles 组织角色（内置 + 自定义）
func (s *OrganizationService) ListRoles(ctx context.Context, access *admindomain.AccessContext, orgUUID string) ([]orgdomain.Role, []orgdomain.BuiltinRoleMeta, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermRoleRead)
	if err != nil {
		return nil, nil, err
	}
	roles, err := s.pg.ListOrgRoles(ctx, oc.OrgID)
	if err != nil {
		return nil, nil, err
	}
	return roles, orgdomain.BuiltinRoles(), nil
}

// CreateRole 创建组织自定义角色
func (s *OrganizationService) CreateRole(ctx context.Context, access *admindomain.AccessContext, orgUUID string, input orgdomain.RoleInput) (*orgdomain.Role, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermRoleWrite)
	if err != nil {
		return nil, err
	}
	if err := assertWritable(oc.Org); err != nil {
		return nil, err
	}
	if err := s.validateRoleInput(oc, input); err != nil {
		return nil, err
	}
	role, err := s.pg.CreateOrgRole(ctx, oc.OrgID, input, access.AdminID)
	if err != nil {
		return nil, err
	}
	// 策略必须立刻重载，否则新角色的权限要等重启才生效
	if err := s.access.Reload(ctx); err != nil {
		return nil, err
	}
	s.recordActivity(ctx, oc.OrgID, access.AdminID, "role.create", "role", role.UUID,
		"创建组织角色 "+role.Name, map[string]any{"permissions": role.Permissions})
	return role, nil
}

// UpdateRole 更新组织角色
func (s *OrganizationService) UpdateRole(ctx context.Context, access *admindomain.AccessContext, orgUUID, roleUUID string, input orgdomain.RoleInput) (*orgdomain.Role, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermRoleWrite)
	if err != nil {
		return nil, err
	}
	if err := assertWritable(oc.Org); err != nil {
		return nil, err
	}
	if err := s.validateRoleInput(oc, input); err != nil {
		return nil, err
	}
	roleID, err := s.pg.ResolveOrgRoleID(ctx, oc.OrgID, roleUUID)
	if err != nil {
		return nil, err
	}
	role, err := s.pg.UpdateOrgRole(ctx, oc.OrgID, roleID, input)
	if err != nil {
		return nil, err
	}
	if err := s.access.Reload(ctx); err != nil {
		return nil, err
	}
	s.recordActivity(ctx, oc.OrgID, access.AdminID, "role.update", "role", role.UUID,
		"更新组织角色 "+role.Name, map[string]any{"permissions": role.Permissions})
	return role, nil
}

// DeleteRole 删除组织角色
func (s *OrganizationService) DeleteRole(ctx context.Context, access *admindomain.AccessContext, orgUUID, roleUUID string) error {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermRoleWrite)
	if err != nil {
		return err
	}
	roleID, err := s.pg.ResolveOrgRoleID(ctx, oc.OrgID, roleUUID)
	if err != nil {
		return err
	}
	if err := s.pg.DeleteOrgRole(ctx, oc.OrgID, roleID); err != nil {
		return err
	}
	if err := s.access.Reload(ctx); err != nil {
		return err
	}
	s.recordActivity(ctx, oc.OrgID, access.AdminID, "role.delete", "role", roleUUID, "删除组织角色", nil)
	return nil
}

// GrantRole 授予组织角色
func (s *OrganizationService) GrantRole(ctx context.Context, access *admindomain.AccessContext, orgUUID, roleUUID string, input orgdomain.GrantRoleInput) (int, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermRoleWrite)
	if err != nil {
		return 0, err
	}
	if err := assertWritable(oc.Org); err != nil {
		return 0, err
	}
	roleID, err := s.pg.ResolveOrgRoleID(ctx, oc.OrgID, roleUUID)
	if err != nil {
		return 0, err
	}
	granted, err := s.pg.GrantOrgRole(ctx, oc.OrgID, roleID, input, access.AdminID)
	if err != nil {
		return 0, err
	}
	s.recordActivity(ctx, oc.OrgID, access.AdminID, "role.grant", "role", roleUUID,
		fmt.Sprintf("向 %d 位成员授予角色", granted), map[string]any{"adminIds": input.AdminIDs})
	return granted, nil
}

// RevokeRole 撤销组织角色
func (s *OrganizationService) RevokeRole(ctx context.Context, access *admindomain.AccessContext, orgUUID, roleUUID string, adminID int64) error {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermRoleWrite)
	if err != nil {
		return err
	}
	roleID, err := s.pg.ResolveOrgRoleID(ctx, oc.OrgID, roleUUID)
	if err != nil {
		return err
	}
	if err := s.pg.RevokeOrgRole(ctx, oc.OrgID, roleID, adminID); err != nil {
		return err
	}
	s.recordActivity(ctx, oc.OrgID, access.AdminID, "role.revoke", "role", roleUUID,
		fmt.Sprintf("撤销管理员 #%d 的角色", adminID), nil)
	return nil
}

// ListRoleGrants 角色授予记录
func (s *OrganizationService) ListRoleGrants(ctx context.Context, access *admindomain.AccessContext, orgUUID, roleUUID string) ([]orgdomain.RoleGrant, error) {
	oc, err := s.orgContext(ctx, access, orgUUID, orgdomain.PermRoleRead)
	if err != nil {
		return nil, err
	}
	roleID, err := s.pg.ResolveOrgRoleID(ctx, oc.OrgID, roleUUID)
	if err != nil {
		return nil, err
	}
	return s.pg.ListRoleGrants(ctx, oc.OrgID, roleID)
}

// validateRoleInput 角色权限不得超出授予者自己持有的范围 —— 否则一个主管
// 可以造一个「拥有 org:delete」的角色再授给自己，绕过全部层级约束。
func (s *OrganizationService) validateRoleInput(oc *orgContext, input orgdomain.RoleInput) error {
	if strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.RoleKey) == "" {
		return apperrors.New(40069, http.StatusBadRequest, "角色名称与标识不能为空")
	}
	if len(input.Permissions) == 0 {
		return apperrors.New(40070, http.StatusBadRequest, "请至少选择一项权限")
	}
	valid := map[string]bool{}
	for _, p := range orgdomain.AllPermissions() {
		valid[p] = true
	}
	for _, p := range input.Permissions {
		if !valid[p] {
			return apperrors.New(40071, http.StatusBadRequest, "包含未知的权限点："+p)
		}
		if !oc.Access.Can(p) {
			return apperrors.New(40389, http.StatusForbidden, "不能授予你自己不具备的权限："+p)
		}
	}
	return nil
}
