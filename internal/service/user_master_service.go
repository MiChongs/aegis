package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	userdomain "aegis/internal/domain/user"
	apperrors "aegis/pkg/errors"
	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// UserMasterService 用户主数据中心业务逻辑
type UserMasterService struct {
	log *zap.Logger
	pg  *pgrepo.Repository
}

// NewUserMasterService 创建用户主数据服务
func NewUserMasterService(log *zap.Logger, pg *pgrepo.Repository) *UserMasterService {
	return &UserMasterService{log: log, pg: pg}
}

// ── 统一身份 ──

// CreateGlobalIdentity 创建全局身份
func (s *UserMasterService) CreateGlobalIdentity(ctx context.Context, email, phone, displayName string) (*userdomain.GlobalIdentity, error) {
	email = strings.TrimSpace(email)
	phone = strings.TrimSpace(phone)
	displayName = strings.TrimSpace(displayName)
	if email == "" && phone == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "邮箱和手机号不能同时为空")
	}
	return s.pg.CreateGlobalIdentity(ctx, email, phone, displayName)
}

// GetGlobalIdentity 获取身份详情
func (s *UserMasterService) GetGlobalIdentity(ctx context.Context, id int64) (*userdomain.GlobalIdentity, error) {
	gi, err := s.pg.GetGlobalIdentity(ctx, id)
	if err != nil {
		return nil, err
	}
	if gi == nil {
		return nil, apperrors.New(40400, http.StatusNotFound, "身份不存在")
	}
	// 填充标签
	tags, err := s.pg.ListIdentityTags(ctx, id)
	if err != nil {
		s.log.Warn("获取身份标签失败", zap.Int64("identityID", id), zap.Error(err))
	} else {
		gi.Tags = tags
	}
	// 填充映射
	mappings, err := s.pg.ListMappingsByIdentity(ctx, id)
	if err != nil {
		s.log.Warn("获取身份映射失败", zap.Int64("identityID", id), zap.Error(err))
	} else {
		gi.Mappings = mappings
	}
	return gi, nil
}

// ListGlobalIdentities 分页查询身份列表
func (s *UserMasterService) ListGlobalIdentities(ctx context.Context, q userdomain.IdentityListQuery) ([]userdomain.GlobalIdentity, int64, error) {
	return s.pg.ListGlobalIdentities(ctx, q)
}

// UpdateIdentityStatus 更新身份状态
func (s *UserMasterService) UpdateIdentityStatus(ctx context.Context, id int64, status string) error {
	status = strings.TrimSpace(strings.ToLower(status))
	if status == "" {
		return apperrors.New(40000, http.StatusBadRequest, "状态不能为空")
	}
	return s.pg.UpdateIdentityStatus(ctx, id, status)
}

// UpdateIdentityLifecycle 更新身份生命周期
func (s *UserMasterService) UpdateIdentityLifecycle(ctx context.Context, id int64, state string) error {
	state = strings.TrimSpace(strings.ToLower(state))
	if state == "" {
		return apperrors.New(40000, http.StatusBadRequest, "生命周期状态不能为空")
	}
	return s.pg.UpdateIdentityLifecycle(ctx, id, state)
}

// UpdateIdentityRisk 更新身份风险评分
func (s *UserMasterService) UpdateIdentityRisk(ctx context.Context, id int64, score int, level string) error {
	level = strings.TrimSpace(strings.ToLower(level))
	if level == "" {
		return apperrors.New(40000, http.StatusBadRequest, "风险等级不能为空")
	}
	return s.pg.UpdateIdentityRisk(ctx, id, score, level)
}

// ── 映射 ──

// CreateIdentityMapping 创建身份-用户映射
func (s *UserMasterService) CreateIdentityMapping(ctx context.Context, identityID, appID, userID int64) error {
	return s.pg.CreateIdentityMapping(ctx, identityID, appID, userID)
}

// ListMappingsByIdentity 查询身份下的所有映射
func (s *UserMasterService) ListMappingsByIdentity(ctx context.Context, identityID int64) ([]userdomain.IdentityUserMapping, error) {
	return s.pg.ListMappingsByIdentity(ctx, identityID)
}

// ListMappingsByApp 查询应用下的所有映射
func (s *UserMasterService) ListMappingsByApp(ctx context.Context, appID int64) ([]userdomain.IdentityUserMapping, error) {
	return s.pg.ListMappingsByApp(ctx, appID)
}

// DeleteIdentityMapping 删除映射
func (s *UserMasterService) DeleteIdentityMapping(ctx context.Context, id int64) error {
	return s.pg.DeleteIdentityMapping(ctx, id)
}

// ── 标签 ──

// CreateUserTag 创建标签
func (s *UserMasterService) CreateUserTag(ctx context.Context, input userdomain.CreateTagInput, createdBy int64) (*userdomain.UserTag, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "标签名称不能为空")
	}
	if input.Color == "" {
		input.Color = "#6366f1"
	}
	return s.pg.CreateUserTag(ctx, input, createdBy)
}

// ListUserTags 列出所有标签
func (s *UserMasterService) ListUserTags(ctx context.Context) ([]userdomain.UserTag, error) {
	return s.pg.ListUserTags(ctx)
}

// DeleteUserTag 删除标签
func (s *UserMasterService) DeleteUserTag(ctx context.Context, id int64) error {
	return s.pg.DeleteUserTag(ctx, id)
}

// AssignTagToIdentity 给身份分配标签
func (s *UserMasterService) AssignTagToIdentity(ctx context.Context, identityID, tagID, assignedBy int64) error {
	return s.pg.AssignTagToIdentity(ctx, identityID, tagID, assignedBy)
}

// RemoveTagFromIdentity 移除身份标签
func (s *UserMasterService) RemoveTagFromIdentity(ctx context.Context, identityID, tagID int64) error {
	return s.pg.RemoveTagFromIdentity(ctx, identityID, tagID)
}

// ListIdentityTags 获取身份的所有标签
func (s *UserMasterService) ListIdentityTags(ctx context.Context, identityID int64) ([]userdomain.UserTag, error) {
	return s.pg.ListIdentityTags(ctx, identityID)
}

// ListIdentitiesByTag 查询某标签的所有身份
func (s *UserMasterService) ListIdentitiesByTag(ctx context.Context, tagID int64) ([]userdomain.GlobalIdentity, error) {
	return s.pg.ListIdentitiesByTag(ctx, tagID)
}

// ── 分群 ──

// CreateUserSegment 创建分群
func (s *UserMasterService) CreateUserSegment(ctx context.Context, input userdomain.CreateSegmentInput, createdBy int64) (*userdomain.UserSegment, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "分群名称不能为空")
	}
	if input.SegmentType == "" {
		input.SegmentType = "static"
	}
	return s.pg.CreateUserSegment(ctx, input, createdBy)
}

// ListUserSegments 列出所有分群
func (s *UserMasterService) ListUserSegments(ctx context.Context) ([]userdomain.UserSegment, error) {
	return s.pg.ListUserSegments(ctx)
}

// UpdateUserSegment 更新分群
func (s *UserMasterService) UpdateUserSegment(ctx context.Context, id int64, name, desc string, rules map[string]any) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return apperrors.New(40000, http.StatusBadRequest, "分群名称不能为空")
	}
	return s.pg.UpdateUserSegment(ctx, id, name, desc, rules)
}

// DeleteUserSegment 删除分群
func (s *UserMasterService) DeleteUserSegment(ctx context.Context, id int64) error {
	return s.pg.DeleteUserSegment(ctx, id)
}

// AddSegmentMember 添加分群成员
func (s *UserMasterService) AddSegmentMember(ctx context.Context, segmentID, identityID, addedBy int64) error {
	return s.pg.AddSegmentMember(ctx, segmentID, identityID, addedBy)
}

// RemoveSegmentMember 移除分群成员
func (s *UserMasterService) RemoveSegmentMember(ctx context.Context, segmentID, identityID int64) error {
	return s.pg.RemoveSegmentMember(ctx, segmentID, identityID)
}

// ListSegmentMembers 分页查询分群成员
func (s *UserMasterService) ListSegmentMembers(ctx context.Context, segmentID int64, page, limit int) ([]userdomain.GlobalIdentity, int64, error) {
	return s.pg.ListSegmentMembers(ctx, segmentID, page, limit)
}

// ── 黑白名单 ──

// CreateUserListEntry 创建黑白名单条目
func (s *UserMasterService) CreateUserListEntry(ctx context.Context, input userdomain.CreateListEntryInput, createdBy int64) (*userdomain.UserListEntry, error) {
	if input.ListType != "blacklist" && input.ListType != "whitelist" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "无效的名单类型，仅支持 blacklist/whitelist")
	}
	if input.Email == "" && input.Phone == "" && input.IP == "" && input.IdentityID == nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, "至少需要提供邮箱、手机号、IP 或身份 ID 之一")
	}
	return s.pg.CreateUserListEntry(ctx, input, createdBy)
}

// ListUserListEntries 分页查询名单列表
func (s *UserMasterService) ListUserListEntries(ctx context.Context, listType string, page, limit int) ([]userdomain.UserListEntry, int64, error) {
	return s.pg.ListUserListEntries(ctx, listType, page, limit)
}

// DeleteUserListEntry 删除名单条目
func (s *UserMasterService) DeleteUserListEntry(ctx context.Context, id int64) error {
	return s.pg.DeleteUserListEntry(ctx, id)
}

// CheckBlacklisted 检查是否在黑名单中
func (s *UserMasterService) CheckBlacklisted(ctx context.Context, email, phone, ip string) (bool, error) {
	return s.pg.CheckBlacklisted(ctx, email, phone, ip)
}

// ── 合并 ──

// ExecuteIdentityMerge 执行身份合并
func (s *UserMasterService) ExecuteIdentityMerge(ctx context.Context, primaryID, mergedID, mergedBy int64) (*userdomain.IdentityMerge, error) {
	if primaryID == mergedID {
		return nil, apperrors.New(40000, http.StatusBadRequest, "主身份和被合并身份不能相同")
	}
	return s.pg.ExecuteIdentityMerge(ctx, primaryID, mergedID, mergedBy)
}

// ListIdentityMerges 列出合并记录
func (s *UserMasterService) ListIdentityMerges(ctx context.Context) ([]userdomain.IdentityMerge, error) {
	return s.pg.ListIdentityMerges(ctx)
}

// ── 申诉 ──

// CreateUserAppeal 创建申诉
func (s *UserMasterService) CreateUserAppeal(ctx context.Context, identityID int64, input userdomain.CreateAppealInput) (*userdomain.UserAppeal, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if input.Reason == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "申诉理由不能为空")
	}
	if input.AppealType == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "申诉类型不能为空")
	}
	return s.pg.CreateUserAppeal(ctx, identityID, input)
}

// ListUserAppeals 分页查询申诉列表
func (s *UserMasterService) ListUserAppeals(ctx context.Context, status string, page, limit int) ([]userdomain.UserAppeal, int64, error) {
	return s.pg.ListUserAppeals(ctx, status, page, limit)
}

// ReviewUserAppeal 审核申诉
func (s *UserMasterService) ReviewUserAppeal(ctx context.Context, id, reviewerID int64, input userdomain.ReviewAppealInput) error {
	if input.Action != "approved" && input.Action != "rejected" {
		return apperrors.New(40000, http.StatusBadRequest, "审核结果仅支持 approved/rejected")
	}
	return s.pg.ReviewUserAppeal(ctx, id, reviewerID, input)
}

// ── 注销 ──

// CreateDeactivationRequest 创建注销请求
func (s *UserMasterService) CreateDeactivationRequest(ctx context.Context, identityID int64, reason string, coolingDays int) (*userdomain.DeactivationRequest, error) {
	if coolingDays < 1 {
		coolingDays = 14
	}
	return s.pg.CreateDeactivationRequest(ctx, identityID, reason, coolingDays)
}

// CancelDeactivation 取消注销请求
func (s *UserMasterService) CancelDeactivation(ctx context.Context, id int64) error {
	return s.pg.CancelDeactivation(ctx, id)
}

// ListPendingDeactivations 列出待处理的注销请求
func (s *UserMasterService) ListPendingDeactivations(ctx context.Context) ([]userdomain.DeactivationRequest, error) {
	return s.pg.ListPendingDeactivations(ctx)
}

// ── 同步 ──

// SyncIdentityFromUser 从应用用户同步到全局身份
func (s *UserMasterService) SyncIdentityFromUser(ctx context.Context, appID, userID int64) error {
	// 查用户 profile 获取 email/phone
	profile, err := s.pg.GetUserProfileByUserID(ctx, userID)
	if err != nil {
		return fmt.Errorf("获取用户资料失败: %w", err)
	}
	if profile == nil {
		return apperrors.New(40400, http.StatusNotFound, "用户资料不存在")
	}

	email := strings.TrimSpace(profile.Email)
	phone := strings.TrimSpace(profile.Phone)
	displayName := strings.TrimSpace(profile.Nickname)

	// 尝试按 email 或 phone 查找已有身份
	var identity *userdomain.GlobalIdentity
	if email != "" {
		identity, _ = s.pg.FindIdentityByEmail(ctx, email)
	}
	if identity == nil && phone != "" {
		identity, _ = s.pg.FindIdentityByPhone(ctx, phone)
	}

	// 不存在则创建
	if identity == nil {
		if email == "" && phone == "" {
			s.log.Debug("用户无邮箱和手机号，跳过身份同步", zap.Int64("userID", userID))
			return nil
		}
		identity, err = s.pg.CreateGlobalIdentity(ctx, email, phone, displayName)
		if err != nil {
			return fmt.Errorf("创建全局身份失败: %w", err)
		}
	}

	// 建立映射
	return s.pg.CreateIdentityMapping(ctx, identity.ID, appID, userID)
}

// BatchSyncIdentities 批量将应用用户同步到全局身份
func (s *UserMasterService) BatchSyncIdentities(ctx context.Context, appID int64) (int64, error) {
	users, err := s.pg.ListAllAppUsers(ctx, appID)
	if err != nil {
		return 0, fmt.Errorf("查询应用用户列表失败: %w", err)
	}

	var synced int64
	for _, u := range users {
		if err := s.SyncIdentityFromUser(ctx, appID, u.UserID); err != nil {
			s.log.Warn("同步用户身份失败", zap.Int64("appID", appID), zap.Int64("userID", u.UserID), zap.Error(err))
			continue
		}
		synced++
	}
	s.log.Info("批量同步身份完成", zap.Int64("appID", appID), zap.Int64("total", int64(len(users))), zap.Int64("synced", synced))
	return synced, nil
}
