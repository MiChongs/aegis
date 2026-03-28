package service

import (
	"context"
	"net/http"

	"aegis/internal/event"
	systemdomain "aegis/internal/domain/system"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"go.uber.org/zap"
)

// AnnouncementService 全站系统公告管理（仅管理员后台）
type AnnouncementService struct {
	log       *zap.Logger
	pg        *pgrepo.Repository
	publisher *event.Publisher
}

func NewAnnouncementService(log *zap.Logger, pg *pgrepo.Repository, publisher *event.Publisher) *AnnouncementService {
	return &AnnouncementService{log: log, pg: pg, publisher: publisher}
}

// List 分页列表
func (s *AnnouncementService) List(ctx context.Context, query systemdomain.AnnouncementListQuery) (*systemdomain.AnnouncementListResult, error) {
	return s.pg.ListAnnouncements(ctx, query)
}

// Detail 获取详情
func (s *AnnouncementService) Detail(ctx context.Context, id int64) (*systemdomain.Announcement, error) {
	item, err := s.pg.GetAnnouncementByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40470, http.StatusNotFound, "公告不存在")
	}
	return item, nil
}

// Save 创建或更新
func (s *AnnouncementService) Save(ctx context.Context, mutation systemdomain.AnnouncementMutation) (*systemdomain.Announcement, error) {
	return s.pg.UpsertAnnouncement(ctx, mutation)
}

// Delete 删除
func (s *AnnouncementService) Delete(ctx context.Context, id int64) error {
	affected, err := s.pg.DeleteAnnouncement(ctx, id)
	if err != nil {
		return err
	}
	if affected == 0 {
		return apperrors.New(40470, http.StatusNotFound, "公告不存在")
	}
	return nil
}

// Publish 发布并广播实时事件
func (s *AnnouncementService) Publish(ctx context.Context, id int64) (*systemdomain.Announcement, error) {
	item, err := s.pg.PublishAnnouncement(ctx, id)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40470, http.StatusNotFound, "公告不存在")
	}
	// 广播到 NATS：所有管理员 WebSocket 客户端会收到
	if s.publisher != nil {
		if pubErr := s.publisher.PublishFire(ctx, event.SubjectSystemAnnouncement, map[string]any{
			"type":           "system.announcement",
			"action":         "published",
			"announcementId": item.ID,
			"announcementType": item.Type,
			"title":          item.Title,
			"level":          item.Level,
			"pinned":         item.Pinned,
		}); pubErr != nil {
			s.log.Warn("广播系统公告失败", zap.Error(pubErr))
		}
	}
	return item, nil
}

// Archive 归档
func (s *AnnouncementService) Archive(ctx context.Context, id int64) (*systemdomain.Announcement, error) {
	item, err := s.pg.ArchiveAnnouncement(ctx, id)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40470, http.StatusNotFound, "公告不存在")
	}
	// 广播归档事件
	if s.publisher != nil {
		_ = s.publisher.PublishFire(ctx, event.SubjectSystemAnnouncement, map[string]any{
			"type":           "system.announcement",
			"action":         "archived",
			"announcementId": item.ID,
		})
	}
	return item, nil
}

// ListActive 获取当前生效公告（公开端点）
func (s *AnnouncementService) ListActive(ctx context.Context) ([]systemdomain.Announcement, error) {
	return s.pg.ListActiveAnnouncements(ctx)
}
