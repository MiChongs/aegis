package service

import (
	"context"
	"net/http"
	"strings"

	authdomain "aegis/internal/domain/auth"
	userdomain "aegis/internal/domain/user"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
)

type RoleApplicationService struct {
	pg *pgrepo.Repository
}

func NewRoleApplicationService(pg *pgrepo.Repository) *RoleApplicationService {
	return &RoleApplicationService{pg: pg}
}

func (s *RoleApplicationService) ensureDefaults(ctx context.Context, appID int64) error {
	return s.pg.EnsureDefaultRoleDefinitions(ctx, appID)
}

func (s *RoleApplicationService) AvailableRoles(ctx context.Context, session *authdomain.Session, appID int64) ([]userdomain.RoleDefinition, error) {
	if err := ensureAuthedApp(session, appID); err != nil {
		return nil, err
	}
	if err := s.ensureDefaults(ctx, appID); err != nil {
		return nil, err
	}
	return s.pg.ListAvailableRoles(ctx, appID)
}

func (s *RoleApplicationService) Submit(ctx context.Context, session *authdomain.Session, appID int64, requestedRole string, reason string, priority string, validDays int, deviceInfo map[string]any) (*userdomain.RoleApplication, error) {
	if err := ensureAuthedApp(session, appID); err != nil {
		return nil, err
	}
	if err := s.ensureDefaults(ctx, appID); err != nil {
		return nil, err
	}
	if strings.TrimSpace(requestedRole) == "" || strings.TrimSpace(reason) == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "申请角色和理由不能为空")
	}
	if validDays <= 0 {
		validDays = 30
	}
	if priority == "" {
		priority = "normal"
	}
	exists, err := s.pg.HasPendingRoleApplication(ctx, appID, session.UserID, requestedRole)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, apperrors.New(40000, http.StatusBadRequest, "已有待处理的同角色申请")
	}
	profile, err := s.pg.GetUserProfileByUserID(ctx, session.UserID)
	if err != nil {
		return nil, err
	}
	currentRole := pgrepo.CurrentRoleFromProfile(profile)
	return s.pg.CreateRoleApplication(ctx, userdomain.RoleApplication{
		AppID:         appID,
		UserID:        session.UserID,
		RequestedRole: requestedRole,
		CurrentRole:   currentRole,
		Reason:        reason,
		Priority:      priority,
		ValidDays:     validDays,
		DeviceInfo:    deviceInfo,
	})
}

func (s *RoleApplicationService) UserList(ctx context.Context, session *authdomain.Session, appID int64, query userdomain.RoleApplicationListQuery) (*userdomain.RoleApplicationListResult, error) {
	if err := ensureAuthedApp(session, appID); err != nil {
		return nil, err
	}
	return s.pg.ListRoleApplicationsByUser(ctx, session.UserID, appID, query)
}

func (s *RoleApplicationService) UserDetail(ctx context.Context, session *authdomain.Session, appID int64, id int64) (*userdomain.RoleApplication, error) {
	if err := ensureAuthedApp(session, appID); err != nil {
		return nil, err
	}
	item, err := s.pg.GetRoleApplicationByID(ctx, id, appID)
	if err != nil {
		return nil, err
	}
	if item == nil || item.UserID != session.UserID {
		return nil, apperrors.New(40440, http.StatusNotFound, "申请记录不存在")
	}
	return item, nil
}

func (s *RoleApplicationService) Cancel(ctx context.Context, session *authdomain.Session, appID int64, id int64) (*userdomain.RoleApplication, error) {
	if err := ensureAuthedApp(session, appID); err != nil {
		return nil, err
	}
	item, err := s.pg.CancelRoleApplication(ctx, id, session.UserID, appID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40440, http.StatusNotFound, "申请记录不存在或不可取消")
	}
	return item, nil
}

func (s *RoleApplicationService) Resubmit(ctx context.Context, session *authdomain.Session, appID int64, id int64, reason string) (*userdomain.RoleApplication, error) {
	if err := ensureAuthedApp(session, appID); err != nil {
		return nil, err
	}
	item, err := s.pg.ResubmitRoleApplication(ctx, id, session.UserID, appID, reason)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40440, http.StatusNotFound, "申请记录不存在或不可重新提交")
	}
	return item, nil
}

func (s *RoleApplicationService) AdminList(ctx context.Context, appID int64, query userdomain.RoleApplicationListQuery) (*userdomain.RoleApplicationListResult, error) {
	return s.pg.ListRoleApplicationsByApp(ctx, appID, query)
}

func (s *RoleApplicationService) AdminDetail(ctx context.Context, appID int64, id int64) (*userdomain.RoleApplication, error) {
	item, err := s.pg.GetRoleApplicationByID(ctx, id, appID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40440, http.StatusNotFound, "申请记录不存在")
	}
	return item, nil
}

func (s *RoleApplicationService) Review(ctx context.Context, appID int64, id int64, adminID int64, adminName string, action string, reason string) (*userdomain.RoleApplication, error) {
	status := ""
	switch action {
	case "approve", "approved":
		status = "approved"
	case "reject", "rejected":
		status = "rejected"
	default:
		return nil, apperrors.New(40000, http.StatusBadRequest, "审核动作无效")
	}
	item, err := s.pg.ReviewRoleApplication(ctx, id, appID, adminID, adminName, status, reason)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40440, http.StatusNotFound, "申请记录不存在或不可审核")
	}
	return item, nil
}

func (s *RoleApplicationService) Statistics(ctx context.Context, appID int64) (*userdomain.RoleApplicationStatistics, error) {
	if err := s.ensureDefaults(ctx, appID); err != nil {
		return nil, err
	}
	return s.pg.GetRoleApplicationStatistics(ctx, appID)
}
