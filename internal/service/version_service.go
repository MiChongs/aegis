package service

import (
	"context"
	"net/http"

	appdomain "aegis/internal/domain/app"
	authdomain "aegis/internal/domain/auth"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
)

type VersionService struct {
	pg *pgrepo.Repository
}

func NewVersionService(pg *pgrepo.Repository) *VersionService {
	return &VersionService{pg: pg}
}

func (s *VersionService) ensureDefaultChannel(ctx context.Context, appID int64) error {
	_, err := s.pg.EnsureDefaultVersionChannel(ctx, appID)
	return err
}

func (s *VersionService) CheckForUpdate(ctx context.Context, appID int64, versionCode int64, platform string, session *authdomain.Session) (*appdomain.AppVersionCheckResult, error) {
	if appID <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "应用ID不能为空")
	}
	if err := s.ensureDefaultChannel(ctx, appID); err != nil {
		return nil, err
	}
	var channelID *int64
	var channelName string
	if session != nil && session.AppID == appID {
		channel, err := s.pg.ResolveVersionChannelForUser(ctx, appID, session.UserID)
		if err != nil {
			return nil, err
		}
		if channel != nil {
			channelID = &channel.ID
			channelName = channel.Name
		}
	}
	version, err := s.pg.FindLatestVersionForUpdate(ctx, appID, channelID, versionCode, platform)
	if err != nil {
		return nil, err
	}
	return &appdomain.AppVersionCheckResult{Version: version, ChannelName: channelName}, nil
}

func (s *VersionService) List(ctx context.Context, appID int64, query appdomain.AppVersionListQuery) (*appdomain.AppVersionListResult, error) {
	if err := s.ensureDefaultChannel(ctx, appID); err != nil {
		return nil, err
	}
	return s.pg.ListAppVersions(ctx, appID, query)
}

func (s *VersionService) Detail(ctx context.Context, versionID int64, appID int64) (*appdomain.AppVersion, error) {
	item, err := s.pg.GetAppVersionByID(ctx, versionID, appID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40430, http.StatusNotFound, "版本不存在")
	}
	return item, nil
}

func (s *VersionService) Save(ctx context.Context, mutation appdomain.AppVersionMutation) (*appdomain.AppVersion, error) {
	if err := s.ensureDefaultChannel(ctx, mutation.AppID); err != nil {
		return nil, err
	}
	return s.pg.UpsertAppVersion(ctx, mutation)
}

func (s *VersionService) Delete(ctx context.Context, appID int64, versionID int64) error {
	affected, err := s.pg.DeleteAppVersion(ctx, versionID, appID)
	if err != nil {
		return err
	}
	if affected == 0 {
		return apperrors.New(40430, http.StatusNotFound, "版本不存在")
	}
	return nil
}

func (s *VersionService) ListChannels(ctx context.Context, appID int64) ([]appdomain.AppVersionChannel, error) {
	if err := s.ensureDefaultChannel(ctx, appID); err != nil {
		return nil, err
	}
	return s.pg.ListVersionChannels(ctx, appID)
}

func (s *VersionService) ChannelDetail(ctx context.Context, channelID int64, appID int64) (*appdomain.AppVersionChannel, error) {
	item, err := s.pg.GetVersionChannelByID(ctx, channelID, appID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40431, http.StatusNotFound, "渠道不存在")
	}
	return item, nil
}

func (s *VersionService) SaveChannel(ctx context.Context, mutation appdomain.AppVersionChannelMutation) (*appdomain.AppVersionChannel, error) {
	if err := s.ensureDefaultChannel(ctx, mutation.AppID); err != nil {
		return nil, err
	}
	return s.pg.UpsertVersionChannel(ctx, mutation)
}

func (s *VersionService) DeleteChannel(ctx context.Context, appID int64, channelID int64) error {
	affected, err := s.pg.DeleteVersionChannel(ctx, channelID, appID)
	if err != nil {
		return err
	}
	if affected == 0 {
		return apperrors.New(40431, http.StatusNotFound, "渠道不存在")
	}
	return nil
}

func (s *VersionService) AddChannelUsers(ctx context.Context, appID int64, channelID int64, userIDs []int64) (int64, error) {
	return s.pg.AddUsersToVersionChannel(ctx, channelID, appID, userIDs)
}

func (s *VersionService) RemoveChannelUsers(ctx context.Context, appID int64, channelID int64, userIDs []int64) (int64, error) {
	return s.pg.RemoveUsersFromVersionChannel(ctx, channelID, appID, userIDs)
}

func (s *VersionService) ListChannelUsers(ctx context.Context, appID int64, channelID int64, page int, limit int) ([]appdomain.Site, int64, error) {
	return s.pg.ListVersionChannelUsers(ctx, channelID, appID, page, limit)
}

func (s *VersionService) Stats(ctx context.Context, appID int64) (*appdomain.AppVersionStats, error) {
	return s.pg.GetAppVersionStats(ctx, appID)
}

// Publish 将版本状态设为 published
func (s *VersionService) Publish(ctx context.Context, appID int64, versionID int64) (*appdomain.AppVersion, error) {
	item, err := s.pg.GetAppVersionByID(ctx, versionID, appID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40430, http.StatusNotFound, "版本不存在")
	}
	status := "published"
	return s.pg.UpsertAppVersion(ctx, appdomain.AppVersionMutation{
		ID:     versionID,
		AppID:  appID,
		Status: &status,
	})
}

// Revoke 将版本状态设为 revoked（撤回）
func (s *VersionService) Revoke(ctx context.Context, appID int64, versionID int64) (*appdomain.AppVersion, error) {
	item, err := s.pg.GetAppVersionByID(ctx, versionID, appID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40430, http.StatusNotFound, "版本不存在")
	}
	status := "revoked"
	return s.pg.UpsertAppVersion(ctx, appdomain.AppVersionMutation{
		ID:     versionID,
		AppID:  appID,
		Status: &status,
	})
}
