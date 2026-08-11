package service

import (
	"context"
	"net/http"
	"strings"
	"time"

	notificationdomain "aegis/internal/domain/notification"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"

	"go.uber.org/zap"
)

// AdminInboxService 管理员站内收件箱。
//
// 与 NotificationService（应用用户站内信）是对称的两套，区别在收件人主键空间：
// 前者写 admin_notifications（admin_accounts），后者写 notifications（users）。
//
// 每次投递成功都会顺带推一条实时事件 `admin.notification.created`，
// 控制台的 WebSocket 客户端据此即时刷新角标 —— 不需要轮询。
type AdminInboxService struct {
	log      *zap.Logger
	pg       *pgrepo.Repository
	realtime UserEventPublisher
}

// RealtimeAdminNotificationEvent 控制台监听的实时事件类型。
// 前端 realtime-provider.tsx 与此常量一一对应，改动需两边同步。
const RealtimeAdminNotificationEvent = "admin.notification.created"

// realtimeAdminAppID 管理员实时命名空间。
// RealtimeService.AuthenticateRequest 把管理员会话映射为 {AppID: 0, UserID: adminID}，
// 因此推给管理员必须用 appID=0（与 organization_service 的部门邀请推送同一约定）。
const realtimeAdminAppID int64 = 0

func NewAdminInboxService(log *zap.Logger, pg *pgrepo.Repository, realtime UserEventPublisher) *AdminInboxService {
	if log == nil {
		log = zap.NewNop()
	}
	return &AdminInboxService{log: log, pg: pg, realtime: realtime}
}

// Push 批量投递管理员通知。返回实际入库条数（去重后）。
//
// 停用账号会被过滤掉：给已停用的管理员堆通知没有意义，还会让收件箱统计失真。
func (s *AdminInboxService) Push(ctx context.Context, items []notificationdomain.AdminInboxPush) (int64, error) {
	if s == nil || s.pg == nil || len(items) == 0 {
		return 0, nil
	}

	adminIDs := make([]int64, 0, len(items))
	for _, item := range items {
		if item.AdminID > 0 {
			adminIDs = append(adminIDs, item.AdminID)
		}
	}
	active, err := s.pg.FilterActiveAdminIDs(ctx, adminIDs)
	if err != nil {
		return 0, err
	}
	if len(active) == 0 {
		return 0, nil
	}
	allowed := make(map[int64]struct{}, len(active))
	for _, id := range active {
		allowed[id] = struct{}{}
	}

	prepared := make([]notificationdomain.AdminInboxPush, 0, len(items))
	targets := make(map[int64]struct{}, len(items))
	for _, item := range items {
		if _, ok := allowed[item.AdminID]; !ok {
			continue
		}
		item.Type = strings.TrimSpace(item.Type)
		if item.Type == "" {
			item.Type = "system"
		}
		item.Level = strings.TrimSpace(strings.ToLower(item.Level))
		if _, ok := notificationdomain.ValidAdminLevels[item.Level]; !ok {
			item.Level = notificationdomain.AdminLevelInfo
		}
		item.Title = strings.TrimSpace(item.Title)
		if item.Title == "" {
			continue
		}
		if strings.TrimSpace(item.Content) == "" {
			// 正文留空时用标题兜底，避免收件箱里出现空条目
			item.Content = item.Title
		}
		prepared = append(prepared, item)
		targets[item.AdminID] = struct{}{}
	}
	if len(prepared) == 0 {
		return 0, nil
	}

	inserted, err := s.pg.InsertAdminNotifications(ctx, prepared)
	if err != nil {
		return 0, err
	}
	if inserted > 0 {
		s.publishBadgeRefresh(targets)
	}
	return inserted, nil
}

// publishBadgeRefresh 给每个收件人推一条角标刷新事件（异步，失败不影响入库）。
func (s *AdminInboxService) publishBadgeRefresh(adminIDs map[int64]struct{}) {
	if s.realtime == nil || len(adminIDs) == 0 {
		return
	}
	ids := make([]int64, 0, len(adminIDs))
	for id := range adminIDs {
		ids = append(ids, id)
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, adminID := range ids {
			unread, err := s.pg.CountAdminUnread(ctx, adminID)
			if err != nil {
				unread = -1
			}
			payload := map[string]any{"refreshRequired": true}
			if unread >= 0 {
				payload["unread"] = unread
			}
			if err := s.realtime.PublishUserEvent(ctx, realtimeAdminAppID, adminID,
				RealtimeAdminNotificationEvent, payload); err != nil {
				s.log.Debug("推送管理员通知角标失败", zap.Int64("adminId", adminID), zap.Error(err))
			}
		}
	}()
}

// List 收件箱分页。
func (s *AdminInboxService) List(ctx context.Context, adminID int64, query notificationdomain.AdminInboxQuery) (*notificationdomain.AdminInboxPage, error) {
	if adminID <= 0 {
		return nil, apperrors.New(40110, http.StatusUnauthorized, "管理员未认证")
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
	items, total, unread, err := s.pg.ListAdminNotifications(ctx, adminID, query)
	if err != nil {
		return nil, err
	}
	return &notificationdomain.AdminInboxPage{
		Items: items, Page: query.Page, Limit: query.Limit,
		Total: total, TotalPages: totalPages(total, query.Limit), Unread: unread,
	}, nil
}

// UnreadCount 未读数（角标轮询兜底；正常路径由实时事件驱动）。
func (s *AdminInboxService) UnreadCount(ctx context.Context, adminID int64) (int64, error) {
	if adminID <= 0 {
		return 0, apperrors.New(40110, http.StatusUnauthorized, "管理员未认证")
	}
	return s.pg.CountAdminUnread(ctx, adminID)
}

// MarkRead 标记已读；ids 为空表示全部已读。
func (s *AdminInboxService) MarkRead(ctx context.Context, adminID int64, ids []int64) (*notificationdomain.AdminInboxMutationResult, error) {
	if adminID <= 0 {
		return nil, apperrors.New(40110, http.StatusUnauthorized, "管理员未认证")
	}
	affected, err := s.pg.MarkAdminNotificationsRead(ctx, adminID, dedupeNotificationIDs(ids))
	if err != nil {
		return nil, err
	}
	unread, err := s.pg.CountAdminUnread(ctx, adminID)
	if err != nil {
		return nil, err
	}
	return &notificationdomain.AdminInboxMutationResult{AdminID: adminID, Affected: affected, Unread: unread}, nil
}

// Delete 删除通知；ids 为空时按 onlyRead 决定是清空已读还是清空全部。
func (s *AdminInboxService) Delete(ctx context.Context, adminID int64, ids []int64, onlyRead bool) (*notificationdomain.AdminInboxMutationResult, error) {
	if adminID <= 0 {
		return nil, apperrors.New(40110, http.StatusUnauthorized, "管理员未认证")
	}
	affected, err := s.pg.DeleteAdminNotifications(ctx, adminID, dedupeNotificationIDs(ids), onlyRead)
	if err != nil {
		return nil, err
	}
	unread, err := s.pg.CountAdminUnread(ctx, adminID)
	if err != nil {
		return nil, err
	}
	return &notificationdomain.AdminInboxMutationResult{AdminID: adminID, Affected: affected, Unread: unread}, nil
}

// Purge 清理 N 天前的已读通知，供 Worker 定时调用。
func (s *AdminInboxService) Purge(ctx context.Context, days int) (int64, error) {
	return s.pg.PurgeAdminNotifications(ctx, days)
}
