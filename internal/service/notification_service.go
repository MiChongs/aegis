package service

import (
	"context"
	"net/http"
	"strings"
	"time"

	authdomain "aegis/internal/domain/auth"
	notificationdomain "aegis/internal/domain/notification"
	plugindomain "aegis/internal/domain/plugin"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"go.uber.org/zap"
)

type NotificationService struct {
	log      *zap.Logger
	pg       *pgrepo.Repository
	sessions *redisrepo.SessionRepository
	realtime UserEventPublisher
	plugin   *PluginService
}

// SetPluginService 注入插件服务
func (s *NotificationService) SetPluginService(p *PluginService) {
	s.plugin = p
}

func NewNotificationService(log *zap.Logger, pg *pgrepo.Repository, sessions *redisrepo.SessionRepository, realtime UserEventPublisher) *NotificationService {
	return &NotificationService{log: log, pg: pg, sessions: sessions, realtime: realtime}
}

func (s *NotificationService) List(ctx context.Context, session *authdomain.Session, query notificationdomain.UserListQuery) (*notificationdomain.ListResponse, error) {
	if query.Status == "" {
		query.Status = "all"
	}
	if query.Page < 1 {
		query.Page = 1
	}
	if query.Limit <= 0 {
		query.Limit = 20
	}
	if query.Limit > 100 {
		query.Limit = 100
	}
	cacheKey := cacheNotificationQueryKey(query)
	if s.sessions != nil {
		var cached notificationdomain.ListResponse
		found, err := s.sessions.GetNotificationListCache(ctx, session.AppID, session.UserID, cacheKey, query.Page, query.Limit, &cached)
		if err != nil {
			s.log.Warn("load notification cache failed", zap.Error(err))
		} else if found {
			return &cached, nil
		}
	}

	items, total, unread, err := s.pg.ListUserNotifications(ctx, session.AppID, session.UserID, query)
	if err != nil {
		return nil, err
	}
	response := &notificationdomain.ListResponse{
		Items:      items,
		Page:       query.Page,
		Limit:      query.Limit,
		Total:      total,
		Unread:     unread,
		TotalPages: totalPages(total, query.Limit),
	}
	if s.sessions != nil {
		if err := s.sessions.SetNotificationListCache(ctx, session.AppID, session.UserID, cacheKey, query.Page, query.Limit, *response, 60*time.Second); err != nil {
			s.log.Warn("cache notification list failed", zap.Error(err))
		}
		if err := s.sessions.SetNotificationUnreadCount(ctx, session.AppID, session.UserID, unread, 60*time.Second); err != nil {
			s.log.Warn("cache notification unread failed", zap.Error(err))
		}
	}
	return response, nil
}

func (s *NotificationService) UnreadCount(ctx context.Context, session *authdomain.Session) (int64, error) {
	if s.sessions != nil {
		count, found, err := s.sessions.GetNotificationUnreadCount(ctx, session.AppID, session.UserID)
		if err != nil {
			s.log.Warn("load unread notification cache failed", zap.Error(err))
		} else if found {
			return count, nil
		}
	}
	count, err := s.pg.CountUnreadNotifications(ctx, session.AppID, session.UserID)
	if err != nil {
		return 0, err
	}
	if s.sessions != nil {
		if err := s.sessions.SetNotificationUnreadCount(ctx, session.AppID, session.UserID, count, 60*time.Second); err != nil {
			s.log.Warn("cache unread notification count failed", zap.Error(err))
		}
	}
	return count, nil
}

func (s *NotificationService) MarkRead(ctx context.Context, session *authdomain.Session, notificationID int64) error {
	if notificationID <= 0 {
		return apperrors.New(40010, http.StatusBadRequest, "通知ID无效")
	}
	if err := s.pg.MarkNotificationRead(ctx, session.AppID, session.UserID, notificationID); err != nil {
		return err
	}
	s.invalidateCaches(ctx, session.AppID, session.UserID)
	s.publishNotificationStateAsync(session.AppID, session.UserID, "mark_read")
	return nil
}

func (s *NotificationService) MarkReadBatch(ctx context.Context, session *authdomain.Session, ids []int64) (*notificationdomain.ReadBatchResult, error) {
	deduped := dedupeNotificationIDs(ids)
	if len(deduped) == 0 {
		return nil, apperrors.New(40010, http.StatusBadRequest, "通知标识不能为空")
	}
	updated, err := s.pg.MarkNotificationsRead(ctx, session.AppID, session.UserID, deduped)
	if err != nil {
		return nil, err
	}
	s.invalidateCaches(ctx, session.AppID, session.UserID)
	s.publishNotificationStateAsync(session.AppID, session.UserID, "mark_read_batch")
	return &notificationdomain.ReadBatchResult{
		AppID:     session.AppID,
		UserID:    session.UserID,
		Requested: len(deduped),
		Updated:   updated,
		IDs:       deduped,
	}, nil
}

func (s *NotificationService) MarkAllRead(ctx context.Context, session *authdomain.Session) error {
	if err := s.pg.MarkAllNotificationsRead(ctx, session.AppID, session.UserID); err != nil {
		return err
	}
	s.invalidateCaches(ctx, session.AppID, session.UserID)
	s.publishNotificationStateAsync(session.AppID, session.UserID, "mark_all_read")
	return nil
}

func (s *NotificationService) Delete(ctx context.Context, session *authdomain.Session, notificationID int64) (*notificationdomain.DeleteResult, error) {
	if notificationID <= 0 {
		return nil, apperrors.New(40010, http.StatusBadRequest, "通知ID无效")
	}
	deleted, err := s.pg.DeleteUserNotification(ctx, session.AppID, session.UserID, notificationID)
	if err != nil {
		return nil, err
	}
	s.invalidateCaches(ctx, session.AppID, session.UserID)
	s.publishNotificationStateAsync(session.AppID, session.UserID, "delete")
	return &notificationdomain.DeleteResult{
		AppID:      session.AppID,
		UserID:     session.UserID,
		Deleted:    deleted,
		ClearedAll: false,
	}, nil
}

func (s *NotificationService) SendUserNotification(ctx context.Context, session *authdomain.Session, notificationType string, title string, content string, level string, metadata map[string]any) error {
	notificationType = strings.TrimSpace(notificationType)
	title = strings.TrimSpace(title)
	content = strings.TrimSpace(content)
	level = strings.TrimSpace(strings.ToLower(level))
	if notificationType == "" {
		return apperrors.New(40010, http.StatusBadRequest, "通知类型不能为空")
	}
	if title == "" {
		return apperrors.New(40010, http.StatusBadRequest, "通知标题不能为空")
	}
	if content == "" {
		return apperrors.New(40010, http.StatusBadRequest, "通知内容不能为空")
	}
	if level == "" {
		level = "info"
	}
	if err := s.pg.CreateUserNotification(ctx, session.AppID, session.UserID, notificationType, title, content, level, metadata); err != nil {
		return err
	}
	s.invalidateCaches(ctx, session.AppID, session.UserID)
	s.publishNotificationStateAsync(session.AppID, session.UserID, "created")
	if s.plugin != nil {
		appID := session.AppID
		userID := session.UserID
		go s.plugin.ExecuteHook(context.Background(), HookNotificationCreated, map[string]any{
			"appId": appID, "userId": userID, "type": notificationType, "title": title,
		}, plugindomain.HookMetadata{AppID: &appID, UserID: &userID})
	}
	return nil
}

func (s *NotificationService) Clear(ctx context.Context, session *authdomain.Session, status string) (*notificationdomain.DeleteResult, error) {
	return s.ClearFiltered(ctx, session, status, "", "")
}

func (s *NotificationService) ClearFiltered(ctx context.Context, session *authdomain.Session, status string, notificationType string, level string) (*notificationdomain.DeleteResult, error) {
	status = strings.TrimSpace(status)
	notificationType = strings.TrimSpace(notificationType)
	level = strings.TrimSpace(level)
	if status == "" {
		status = "all"
	}
	deleted, err := s.pg.DeleteUserNotifications(ctx, session.AppID, session.UserID, status, notificationType, level)
	if err != nil {
		return nil, err
	}
	s.invalidateCaches(ctx, session.AppID, session.UserID)
	s.publishNotificationStateAsync(session.AppID, session.UserID, "clear")
	return &notificationdomain.DeleteResult{
		AppID:      session.AppID,
		UserID:     session.UserID,
		Deleted:    deleted,
		Status:     status,
		Type:       notificationType,
		Level:      level,
		ClearedAll: true,
	}, nil
}

func (s *NotificationService) AdminBulkSend(ctx context.Context, appID int64, cmd notificationdomain.AdminBulkSendCommand) (*notificationdomain.AdminBulkSendResult, error) {
	cmd.Type = strings.TrimSpace(cmd.Type)
	cmd.Title = strings.TrimSpace(cmd.Title)
	cmd.Content = strings.TrimSpace(cmd.Content)
	cmd.Level = strings.TrimSpace(strings.ToLower(cmd.Level))
	if cmd.Type == "" {
		return nil, apperrors.New(40010, http.StatusBadRequest, "通知类型不能为空")
	}
	if cmd.Title == "" {
		return nil, apperrors.New(40010, http.StatusBadRequest, "通知标题不能为空")
	}
	if cmd.Content == "" {
		return nil, apperrors.New(40010, http.StatusBadRequest, "通知内容不能为空")
	}
	if cmd.Level == "" {
		cmd.Level = "info"
	}
	if cmd.Limit <= 0 {
		cmd.Limit = 200
	}
	if cmd.Limit > 2000 {
		cmd.Limit = 2000
	}

	targets, err := s.resolveRecipients(ctx, appID, cmd)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, apperrors.New(40401, http.StatusNotFound, "未找到可发送的目标用户")
	}

	delivered := 0
	for _, userID := range targets {
		if err := s.pg.CreateUserNotification(ctx, appID, userID, cmd.Type, cmd.Title, cmd.Content, cmd.Level, cmd.Metadata); err != nil {
			return nil, err
		}
		s.invalidateCaches(ctx, appID, userID)
		s.publishNotificationStateAsync(appID, userID, "admin_bulk_send")
		delivered++
	}

	if s.plugin != nil {
		go s.plugin.ExecuteHook(context.Background(), HookNotificationSent, map[string]any{
			"appId": appID, "delivered": delivered, "type": cmd.Type, "title": cmd.Title,
		}, plugindomain.HookMetadata{AppID: &appID})
	}
	return &notificationdomain.AdminBulkSendResult{
		AppID:        appID,
		Requested:    len(targets),
		Delivered:    delivered,
		RecipientIDs: targets,
	}, nil
}

func (s *NotificationService) AdminList(ctx context.Context, appID int64, query notificationdomain.AdminListQuery) (*notificationdomain.AdminListResponse, error) {
	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	items, total, err := s.pg.ListAppNotifications(ctx, appID, notificationdomain.AdminListQuery{
		Keyword: query.Keyword,
		Type:    query.Type,
		Level:   query.Level,
		Page:    page,
		Limit:   limit,
	})
	if err != nil {
		return nil, err
	}
	return &notificationdomain.AdminListResponse{
		Items:      items,
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: totalPages(total, limit),
	}, nil
}

func (s *NotificationService) AdminDelete(ctx context.Context, appID int64, ids []int64) (*notificationdomain.AdminDeleteResult, error) {
	if len(ids) == 0 {
		return nil, apperrors.New(40010, http.StatusBadRequest, "通知标识不能为空")
	}
	deleted, affectedUsers, err := s.pg.DeleteAppNotifications(ctx, appID, ids)
	if err != nil {
		return nil, err
	}
	for _, userID := range affectedUsers {
		s.invalidateCaches(ctx, appID, userID)
		s.publishNotificationStateAsync(appID, userID, "admin_delete")
	}
	return &notificationdomain.AdminDeleteResult{
		AppID:         appID,
		Requested:     len(ids),
		Deleted:       deleted,
		AffectedUsers: affectedUsers,
	}, nil
}

func (s *NotificationService) AdminExport(ctx context.Context, appID int64, query notificationdomain.AdminExportQuery) ([]notificationdomain.AdminItem, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 5000
	}
	if limit > 20000 {
		limit = 20000
	}
	return s.pg.ListAppNotificationsForExport(ctx, appID, notificationdomain.AdminExportQuery{
		Keyword: query.Keyword,
		Type:    query.Type,
		Level:   query.Level,
		Limit:   limit,
	})
}

func (s *NotificationService) AdminDeleteByFilter(ctx context.Context, appID int64, query notificationdomain.AdminExportQuery) (*notificationdomain.AdminDeleteFilterResult, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 5000
	}
	if limit > 20000 {
		limit = 20000
	}
	ids, err := s.pg.ResolveAppNotificationIDs(ctx, appID, notificationdomain.AdminExportQuery{
		Keyword: query.Keyword,
		Type:    query.Type,
		Level:   query.Level,
		Limit:   limit,
	})
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return &notificationdomain.AdminDeleteFilterResult{
			AppID:     appID,
			Requested: 0,
			Deleted:   0,
			Keyword:   query.Keyword,
			Type:      query.Type,
			Level:     query.Level,
		}, nil
	}
	deleted, affectedUsers, err := s.pg.DeleteAppNotifications(ctx, appID, ids)
	if err != nil {
		return nil, err
	}
	for _, userID := range affectedUsers {
		s.invalidateCaches(ctx, appID, userID)
		s.publishNotificationStateAsync(appID, userID, "admin_delete_by_filter")
	}
	return &notificationdomain.AdminDeleteFilterResult{
		AppID:         appID,
		Requested:     len(ids),
		Deleted:       deleted,
		AffectedUsers: affectedUsers,
		Keyword:       query.Keyword,
		Type:          query.Type,
		Level:         query.Level,
	}, nil
}

func (s *NotificationService) invalidateCaches(ctx context.Context, appID int64, userID int64) {
	if s.sessions == nil {
		return
	}
	if err := s.sessions.DeleteNotificationListCache(ctx, appID, userID); err != nil {
		s.log.Warn("delete notification list cache failed", zap.Error(err))
	}
	if err := s.sessions.DeleteNotificationUnreadCount(ctx, appID, userID); err != nil {
		s.log.Warn("delete notification unread cache failed", zap.Error(err))
	}
	if err := s.sessions.DeleteMyView(ctx, appID, userID); err != nil {
		s.log.Warn("delete my view cache failed after notification mutation", zap.Error(err))
	}
}

func (s *NotificationService) resolveRecipients(ctx context.Context, appID int64, cmd notificationdomain.AdminBulkSendCommand) ([]int64, error) {
	if len(cmd.UserIDs) > 0 {
		return s.pg.FilterExistingUserIDsByApp(ctx, appID, cmd.UserIDs)
	}
	items, err := s.pg.ListAdminUsersForExport(ctx, appID, cmd.Keyword, cmd.Enabled, cmd.Limit)
	if err != nil {
		return nil, err
	}
	userIDs := make([]int64, 0, len(items))
	for _, item := range items {
		userIDs = append(userIDs, item.ID)
	}
	return userIDs, nil
}

func (s *NotificationService) publishNotificationStateAsync(appID int64, userID int64, reason string) {
	if s.realtime == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		unread, err := s.pg.CountUnreadNotifications(ctx, appID, userID)
		if err != nil {
			s.log.Debug("count unread notification for realtime failed", zap.Error(err), zap.Int64("appid", appID), zap.Int64("userId", userID))
			unread = -1
		}
		payload := map[string]any{
			"reason":          reason,
			"refreshRequired": true,
			"stateChangedAt":  time.Now().UTC(),
		}
		if unread >= 0 {
			payload["unread"] = unread
		}
		if err := s.realtime.PublishUserEvent(ctx, appID, userID, "notification.state.changed", payload); err != nil {
			s.log.Debug("publish realtime notification state failed", zap.Error(err), zap.Int64("appid", appID), zap.Int64("userId", userID))
		}
	}()
}

func totalPages(total int64, limit int) int {
	if limit <= 0 {
		return 1
	}
	pages := int((total + int64(limit) - 1) / int64(limit))
	if pages == 0 {
		return 1
	}
	return pages
}

func dedupeNotificationIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	result := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}

func cacheNotificationQueryKey(query notificationdomain.UserListQuery) string {
	status := strings.TrimSpace(query.Status)
	if status == "" {
		status = "all"
	}
	notificationType := strings.TrimSpace(query.Type)
	if notificationType == "" {
		notificationType = "all"
	}
	level := strings.TrimSpace(query.Level)
	if level == "" {
		level = "all"
	}
	return status + "|" + notificationType + "|" + level
}
