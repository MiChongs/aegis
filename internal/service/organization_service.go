package service

import (
	"context"
	"time"

	systemdomain "aegis/internal/domain/system"
	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// OrganizationService 组织架构服务
type OrganizationService struct {
	log      *zap.Logger
	pg       *pgrepo.Repository
	realtime *RealtimeService
}

func NewOrganizationService(log *zap.Logger, pg *pgrepo.Repository) *OrganizationService {
	return &OrganizationService{log: log, pg: pg}
}

// SetRealtimeService 注入实时推送服务
func (s *OrganizationService) SetRealtimeService(rt *RealtimeService) {
	s.realtime = rt
}

// ── 组织 ──

func (s *OrganizationService) ListOrganizations(ctx context.Context, adminID int64, isSuperAdmin bool) ([]systemdomain.Organization, error) {
	if isSuperAdmin {
		return s.pg.ListOrganizations(ctx)
	}
	return s.pg.ListOrganizationsForAdmin(ctx, adminID)
}

func (s *OrganizationService) CreateOrganization(ctx context.Context, input systemdomain.CreateOrgInput, createdBy int64) (*systemdomain.Organization, error) {
	return s.pg.CreateOrganization(ctx, input, createdBy)
}

func (s *OrganizationService) UpdateOrganization(ctx context.Context, orgID int64, input systemdomain.UpdateOrgInput) (*systemdomain.Organization, error) {
	return s.pg.UpdateOrganization(ctx, orgID, input)
}

func (s *OrganizationService) DeleteOrganization(ctx context.Context, orgID int64) error {
	return s.pg.DeleteOrganization(ctx, orgID)
}

// ── 部门 ──

func (s *OrganizationService) GetDepartmentTree(ctx context.Context, orgID int64) ([]systemdomain.DepartmentTree, error) {
	departments, err := s.pg.ListDepartments(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return systemdomain.BuildDepartmentTree(departments), nil
}

func (s *OrganizationService) CreateDepartment(ctx context.Context, orgID int64, input systemdomain.CreateDeptInput) (*systemdomain.Department, error) {
	return s.pg.CreateDepartment(ctx, orgID, input)
}

func (s *OrganizationService) UpdateDepartment(ctx context.Context, deptID int64, input systemdomain.UpdateDeptInput) (*systemdomain.Department, error) {
	return s.pg.UpdateDepartment(ctx, deptID, input)
}

func (s *OrganizationService) MoveDepartment(ctx context.Context, deptID int64, parentID *int64) error {
	return s.pg.MoveDepartment(ctx, deptID, parentID)
}

func (s *OrganizationService) DeleteDepartment(ctx context.Context, deptID int64) error {
	return s.pg.DeleteDepartment(ctx, deptID)
}

// ── 成员 ──

func (s *OrganizationService) ListDepartmentMembers(ctx context.Context, deptID int64) ([]systemdomain.DepartmentMember, error) {
	return s.pg.ListDepartmentMembers(ctx, deptID)
}

func (s *OrganizationService) AddDepartmentMember(ctx context.Context, deptID, adminID int64, isLeader bool) error {
	return s.pg.AddDepartmentMember(ctx, deptID, adminID, isLeader)
}

func (s *OrganizationService) RemoveDepartmentMember(ctx context.Context, deptID, adminID int64) error {
	return s.pg.RemoveDepartmentMember(ctx, deptID, adminID)
}

func (s *OrganizationService) ListAdminDepartments(ctx context.Context, adminID int64) ([]systemdomain.Department, error) {
	return s.pg.ListAdminDepartments(ctx, adminID)
}

// ── 成员身份校验 ──

func (s *OrganizationService) IsDepartmentLeader(ctx context.Context, deptID, adminID int64) (bool, error) {
	return s.pg.IsDepartmentLeader(ctx, deptID, adminID)
}

func (s *OrganizationService) IsDepartmentMember(ctx context.Context, deptID, adminID int64) (bool, error) {
	return s.pg.IsDepartmentMember(ctx, deptID, adminID)
}

func (s *OrganizationService) IsOrganizationMember(ctx context.Context, orgID, adminID int64) (bool, error) {
	return s.pg.IsOrganizationMember(ctx, orgID, adminID)
}

// ── 邀请流程 ──

func (s *OrganizationService) InviteMember(ctx context.Context, deptID, inviteeID int64, isLeader bool, message string, inviterID int64) (*systemdomain.DeptInvitation, error) {
	inv, err := s.pg.CreateDeptInvitation(ctx, systemdomain.CreateInvitationInput{
		DepartmentID: deptID, InviteeID: inviteeID, IsLeader: isLeader, Message: message,
	}, inviterID)
	if err != nil {
		return nil, err
	}
	// 实时通知被邀人
	if s.realtime != nil && inv != nil {
		go s.realtime.PublishUserEvent(context.Background(), 0, inviteeID, "dept.invitation.received", map[string]any{
			"invitationId": inv.ID, "deptName": inv.DeptName, "orgName": inv.OrgName,
			"inviterName": inv.InviterName, "message": inv.Message,
		})
	}
	return inv, nil
}

func (s *OrganizationService) AcceptInvitation(ctx context.Context, invitationID, adminID int64) error {
	inv, err := s.pg.GetDeptInvitation(ctx, invitationID)
	if err != nil || inv == nil {
		return err
	}
	if err := s.pg.AcceptDeptInvitation(ctx, invitationID, adminID); err != nil {
		return err
	}
	// 实时通知邀请人
	if s.realtime != nil {
		go s.realtime.PublishUserEvent(context.Background(), 0, inv.InviterID, "dept.invitation.responded", map[string]any{
			"invitationId": inv.ID, "inviteeName": inv.InviteeName, "status": "accepted", "deptName": inv.DeptName,
		})
	}
	return nil
}

func (s *OrganizationService) RejectInvitation(ctx context.Context, invitationID, adminID int64) error {
	inv, err := s.pg.GetDeptInvitation(ctx, invitationID)
	if err != nil || inv == nil {
		return err
	}
	if err := s.pg.RejectDeptInvitation(ctx, invitationID, adminID); err != nil {
		return err
	}
	// 实时通知邀请人
	if s.realtime != nil {
		go s.realtime.PublishUserEvent(context.Background(), 0, inv.InviterID, "dept.invitation.responded", map[string]any{
			"invitationId": inv.ID, "inviteeName": inv.InviteeName, "status": "rejected", "deptName": inv.DeptName,
		})
	}
	return nil
}

func (s *OrganizationService) CancelInvitation(ctx context.Context, invitationID, adminID int64) error {
	return s.pg.CancelDeptInvitation(ctx, invitationID, adminID)
}

func (s *OrganizationService) ListMyInvitations(ctx context.Context, adminID int64, role, status string, page, limit int) (*systemdomain.InvitationListResult, error) {
	return s.pg.ListDeptInvitations(ctx, systemdomain.InvitationListQuery{
		AdminID: adminID, Role: role, Status: status, Page: page, Limit: limit,
	})
}

func (s *OrganizationService) CountPendingInvitations(ctx context.Context, adminID int64) (int64, error) {
	return s.pg.CountPendingInvitations(ctx, adminID)
}

// ── 岗位 ──

func (s *OrganizationService) CreatePosition(ctx context.Context, input systemdomain.CreatePositionInput) (*systemdomain.Position, error) {
	return s.pg.CreatePosition(ctx, input)
}

func (s *OrganizationService) ListPositions(ctx context.Context, orgID int64) ([]systemdomain.Position, error) {
	return s.pg.ListPositions(ctx, orgID)
}

func (s *OrganizationService) UpdatePosition(ctx context.Context, id int64, name, code, description string, level int) (*systemdomain.Position, error) {
	return s.pg.UpdatePosition(ctx, id, name, code, description, level)
}

func (s *OrganizationService) DeletePosition(ctx context.Context, id int64) error {
	return s.pg.DeletePosition(ctx, id)
}

// ── 成员增强 ──

func (s *OrganizationService) UpdateMemberPosition(ctx context.Context, deptID, adminID int64, positionID *int64, jobTitle string) error {
	return s.pg.UpdateMemberPosition(ctx, deptID, adminID, positionID, jobTitle)
}

func (s *OrganizationService) SetMemberReporting(ctx context.Context, deptID, adminID, reportingTo int64) error {
	return s.pg.SetMemberReporting(ctx, deptID, adminID, reportingTo)
}

func (s *OrganizationService) SetMemberDelegate(ctx context.Context, deptID, adminID, delegateTo int64, expiresAt *time.Time) error {
	return s.pg.SetMemberDelegate(ctx, deptID, adminID, delegateTo, expiresAt)
}

func (s *OrganizationService) ClearMemberDelegate(ctx context.Context, deptID, adminID int64) error {
	return s.pg.ClearMemberDelegate(ctx, deptID, adminID)
}

func (s *OrganizationService) GetReportingChain(ctx context.Context, deptID, adminID int64) ([]systemdomain.ReportingChainNode, error) {
	return s.pg.GetReportingChain(ctx, deptID, adminID)
}

func (s *OrganizationService) BatchInviteMembers(ctx context.Context, deptID, inviterID int64, adminIDs []int64, message string) ([]systemdomain.DeptInvitation, error) {
	invitations, err := s.pg.BatchCreateInvitations(ctx, deptID, inviterID, adminIDs, message)
	if err != nil {
		return nil, err
	}
	// 批量实时通知被邀人
	if s.realtime != nil {
		for _, inv := range invitations {
			go s.realtime.PublishUserEvent(context.Background(), 0, inv.InviteeID, "dept.invitation.received", map[string]any{
				"invitationId": inv.ID, "deptName": inv.DeptName, "orgName": inv.OrgName,
				"inviterName": inv.InviterName, "message": inv.Message,
			})
		}
	}
	return invitations, nil
}

// GetDepartmentOrgID 查询部门所属组织 ID
func (s *OrganizationService) GetDepartmentOrgID(ctx context.Context, deptID int64) (int64, error) {
	return s.pg.GetDepartmentOrgID(ctx, deptID)
}
