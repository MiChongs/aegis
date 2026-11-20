package service

// 用户消息中心（应用用户站内信）。规则见 docs/notification-center.md。
//
// 一切写入只走 Deliver：校验 → 落库（返回完整一行）→ 失效缓存 → 实时事件 → 插件钩子。
// 其它服务想给用户发消息，注入 *NotificationService 调 Deliver / NotifyUser，
// 不得直接调 pg.CreateUserNotification（TestOnlyNotificationServiceCreatesUserNotifications 钉住）。
//
// 实时事件（notification.created / read / deleted，外加兼容的 notification.state.changed）
// 全部异步发、不阻塞请求；同时在发的事件任务有并发上限，群发上千人也不会把数据库连接占满。

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	authdomain "aegis/internal/domain/auth"
	notificationdomain "aegis/internal/domain/notification"
	platformdomain "aegis/internal/domain/platform"
	plugindomain "aegis/internal/domain/plugin"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"go.uber.org/zap"
)

// 消息中心错误码（参数类沿用 40010 / 40000）。
const (
	errCodeNotificationNotFound        = 40455
	errCodeNotificationPushLocked      = 40347
	errCodeNotificationActionInvalid   = 42230
	errCodeNotificationCategoryInvalid = 42231
)

const (
	// notificationEventConcurrency 同时在发的实时事件任务上限（每个任务可能要查一次计数与偏好）
	notificationEventConcurrency = 16
	notificationCacheTTL         = redisrepo.NotificationCacheMaxTTL
	notificationEventTimeout     = 5 * time.Second
)

// notificationDeliverer 站内信投递出口（由 *NotificationService 实现）。
// 其它服务给用户发消息只依赖它，测试里可以换成记录用的假实现。
type notificationDeliverer interface {
	Deliver(ctx context.Context, in notificationdomain.DeliverInput) (*notificationdomain.Item, error)
}

var _ notificationDeliverer = (*NotificationService)(nil)

func errNotificationNotFound() error {
	return apperrors.New(errCodeNotificationNotFound, http.StatusNotFound, "消息不存在")
}

func errNotificationCategoryInvalid(category string) error {
	return apperrors.New(errCodeNotificationCategoryInvalid, http.StatusUnprocessableEntity, "分类不存在："+category)
}

type NotificationService struct {
	log      *zap.Logger
	pg       *pgrepo.Repository
	sessions *redisrepo.SessionRepository
	realtime UserEventPublisher
	plugin   *PluginService
	// governance 平台治理判定：被治理应用不得再对外发消息
	governance *PlatformGovernanceService
	// eventSlots 实时事件任务的并发闸门
	eventSlots chan struct{}
	events     sync.WaitGroup
	// campaigns 群发任务执行器（notification_campaign.go）
	campaigns *notificationCampaignRunner
	now       func() time.Time
}

// SetGovernanceService 注入平台治理服务（bootstrap 中调用）。
func (s *NotificationService) SetGovernanceService(g *PlatformGovernanceService) { s.governance = g }

// ensureNotificationAllowed 站内信写入的统一治理闸门。
//
// 只挡"新产生的消息"，已投递的站内信仍可读可删 —— 冻结一个应用不该顺手删掉
// 用户已经收到的通知。
func (s *NotificationService) ensureNotificationAllowed(appID int64) error {
	if s.governance == nil {
		return nil
	}
	return s.governance.EnsureCapability(appID, platformdomain.CapabilityNotification)
}

// SetPluginService 注入插件服务
func (s *NotificationService) SetPluginService(p *PluginService) {
	s.plugin = p
}

func NewNotificationService(log *zap.Logger, pg *pgrepo.Repository, sessions *redisrepo.SessionRepository, realtime UserEventPublisher) *NotificationService {
	return &NotificationService{
		log: log, pg: pg, sessions: sessions, realtime: realtime,
		eventSlots: make(chan struct{}, notificationEventConcurrency),
		campaigns:  newNotificationCampaignRunner(),
		now:        time.Now,
	}
}

// ── 投递 ──

// Deliver 投递一条站内信，返回落库后的完整一行。所有写入点的唯一入口。
func (s *NotificationService) Deliver(ctx context.Context, in notificationdomain.DeliverInput) (*notificationdomain.Item, error) {
	return s.deliver(ctx, in, true)
}

// deliver hook=false 用于群发：逐人的 created 钩子不发，整批发一次 HookNotificationSent。
func (s *NotificationService) deliver(ctx context.Context, in notificationdomain.DeliverInput, hook bool) (*notificationdomain.Item, error) {
	if in.AppID <= 0 || in.UserID <= 0 {
		return nil, apperrors.New(40010, http.StatusBadRequest, "通知目标无效")
	}
	normalized, err := s.normalizeDeliverInput(in)
	if err != nil {
		return nil, err
	}
	if err := s.ensureNotificationAllowed(normalized.AppID); err != nil {
		return nil, err
	}
	item, err := s.pg.CreateUserNotification(ctx, normalized)
	if err != nil {
		return nil, err
	}
	s.invalidateCaches(ctx, item.AppID, normalized.UserID)
	s.publishCreatedAsync(*item, normalized.UserID)
	if hook && s.plugin != nil {
		appID, userID := item.AppID, normalized.UserID
		go s.plugin.ExecuteHook(context.Background(), HookNotificationCreated, map[string]any{
			"appId": appID, "userId": userID, "id": item.ID, "type": item.Type, "category": item.Category, "title": item.Title,
		}, plugindomain.HookMetadata{AppID: &appID, UserID: &userID})
	}
	return item, nil
}

// normalizeDeliverInput 校验与整理（不含收件人）：类型默认 system、正文空用标题兜底、级别归一、
// 分类空则由类型推出、动作与配图校验、过期时间必须在未来一年内。
func (s *NotificationService) normalizeDeliverInput(in notificationdomain.DeliverInput) (notificationdomain.DeliverInput, error) {
	out := in
	out.Type = strings.TrimSpace(in.Type)
	if out.Type == "" {
		out.Type = notificationdomain.TypeSystem
	}
	if utf8.RuneCountInString(out.Type) > 64 {
		return out, apperrors.New(40010, http.StatusBadRequest, "通知类型过长")
	}
	out.Title = strings.TrimSpace(in.Title)
	if out.Title == "" {
		return out, apperrors.New(40010, http.StatusBadRequest, "通知标题不能为空")
	}
	if utf8.RuneCountInString(out.Title) > notificationdomain.TitleMaxRunes {
		return out, apperrors.New(40010, http.StatusBadRequest, "通知标题不能超过 200 个字")
	}
	out.Content = strings.TrimSpace(in.Content)
	if out.Content == "" {
		// 正文可以为空（如纯状态提醒），用标题兜底以免客户端展示空白
		out.Content = out.Title
	}
	level, ok := notificationdomain.NormalizeLevel(in.Level)
	if !ok {
		return out, apperrors.New(40010, http.StatusBadRequest, "通知级别只能是 info、success、warning 或 error")
	}
	out.Level = level
	out.Category = strings.TrimSpace(in.Category)
	if out.Category == "" {
		out.Category = notificationdomain.CategoryOf(out.Type)
	} else if !notificationdomain.ValidCategory(out.Category) {
		return out, errNotificationCategoryInvalid(out.Category)
	}
	action, msg := notificationdomain.NormalizeAction(in.Action)
	if msg != "" {
		return out, apperrors.New(errCodeNotificationActionInvalid, http.StatusUnprocessableEntity, msg)
	}
	out.Action = action
	out.Image = strings.TrimSpace(in.Image)
	if out.Image != "" {
		if msg := notificationdomain.ValidateHTTPSURL(out.Image, notificationdomain.ImageURLMaxLength); msg != "" {
			return out, apperrors.New(40010, http.StatusBadRequest, "配图地址"+msg)
		}
	}
	if in.ExpiresAt != nil {
		now := s.now()
		expiresAt := in.ExpiresAt.UTC()
		if !expiresAt.After(now) {
			return out, apperrors.New(40010, http.StatusBadRequest, "过期时间必须晚于现在")
		}
		if expiresAt.After(now.Add(notificationdomain.ExpiresAtMaxDuration)) {
			return out, apperrors.New(40010, http.StatusBadRequest, "过期时间不能超过一年")
		}
		out.ExpiresAt = &expiresAt
	}
	return out, nil
}

// SendUserNotification 给当前会话的用户发一条（自测通知等）。
func (s *NotificationService) SendUserNotification(ctx context.Context, session *authdomain.Session, notificationType string, title string, content string, level string, metadata map[string]any) error {
	if strings.TrimSpace(content) == "" {
		return apperrors.New(40010, http.StatusBadRequest, "通知内容不能为空")
	}
	_, err := s.Deliver(ctx, notificationdomain.DeliverInput{
		AppID: session.AppID, UserID: session.UserID, Type: notificationType,
		Title: title, Content: content, Level: level, Metadata: metadata,
	})
	return err
}

// NotifyUser 按 appID + userID 投递站内信，无需用户会话；分类由类型推出、没有动作。
// 供 NotifyHub 的 inapp 渠道使用；需要分类或动作的写入点直接调 Deliver。
func (s *NotificationService) NotifyUser(ctx context.Context, appID int64, userID int64,
	notificationType string, title string, content string, level string, metadata map[string]any) error {
	_, err := s.Deliver(ctx, notificationdomain.DeliverInput{
		AppID: appID, UserID: userID, Type: notificationType,
		Title: title, Content: content, Level: level, Metadata: metadata,
	})
	return err
}

// ── 用户端读取 ──

func (s *NotificationService) List(ctx context.Context, session *authdomain.Session, query notificationdomain.UserListQuery) (*notificationdomain.ListResponse, error) {
	status, err := normalizeNotificationStatus(query.Status, true)
	if err != nil {
		return nil, err
	}
	query.Status = status
	category, err := normalizeNotificationCategoryFilter(query.Category)
	if err != nil {
		return nil, err
	}
	query.Category = category
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
			if cached.CategoryUnread == nil {
				cached.CategoryUnread = map[string]int64{}
			}
			return &cached, nil
		}
	}

	items, total, nextExpiry, err := s.pg.ListUserNotifications(ctx, session.AppID, session.UserID, query)
	if err != nil {
		return nil, err
	}
	counts, err := s.pg.NotificationUnreadCounts(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	response := &notificationdomain.ListResponse{
		Items:      items,
		Page:       query.Page,
		Limit:      query.Limit,
		Total:      total,
		TotalPages: totalPages(total, query.Limit),
		Counts:     counts,
		NextExpiry: nextExpiry,
	}
	if s.sessions != nil {
		if ttl := s.cacheTTL(nextExpiry, counts.NextExpiry); ttl > 0 {
			if err := s.sessions.SetNotificationListCache(ctx, session.AppID, session.UserID, cacheKey, query.Page, query.Limit, *response, ttl); err != nil {
				s.log.Warn("cache notification list failed", zap.Error(err))
			}
		}
		s.cacheCounts(ctx, session.AppID, session.UserID, counts)
	}
	return response, nil
}

// Get 单条详情；不存在、不属于本人或已过期都是 40455。
func (s *NotificationService) Get(ctx context.Context, session *authdomain.Session, notificationID int64) (*notificationdomain.Item, error) {
	if notificationID <= 0 {
		return nil, errNotificationNotFound()
	}
	item, err := s.pg.GetUserNotification(ctx, session.AppID, session.UserID, notificationID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, errNotificationNotFound()
	}
	return item, nil
}

// UnreadCount 总未读与按分类未读（走缓存）。
func (s *NotificationService) UnreadCount(ctx context.Context, session *authdomain.Session) (*notificationdomain.UnreadCountResult, error) {
	counts, err := s.cachedCounts(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	return &notificationdomain.UnreadCountResult{Unread: counts.Unread, Categories: counts.CategoryUnread}, nil
}

// Summary 消息中心首页：全部分类都列出（顺序即目录顺序），没有消息的 total=0、latest=null。
func (s *NotificationService) Summary(ctx context.Context, session *authdomain.Session) (*notificationdomain.Summary, error) {
	stats, latest, err := s.pg.NotificationCategoryStats(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	result := &notificationdomain.Summary{Categories: make([]notificationdomain.CategorySummary, 0, len(notificationdomain.Categories()))}
	for _, spec := range notificationdomain.Categories() {
		stat := stats[spec.Key]
		row := notificationdomain.CategorySummary{Key: spec.Key, Label: spec.Label, Unread: stat.Unread, Total: stat.Total}
		if item, ok := latest[spec.Key]; ok {
			row.Latest = &item
		}
		result.Unread += stat.Unread
		result.Categories = append(result.Categories, row)
	}
	return result, nil
}

// ── 用户端写操作：都带回 unread 与 categoryUnread ──

// MarkRead 标为已读。
func (s *NotificationService) MarkRead(ctx context.Context, session *authdomain.Session, notificationID int64) (*notificationdomain.ReadResult, error) {
	return s.setRead(ctx, session, notificationID, true)
}

// MarkUnread 标为未读。
func (s *NotificationService) MarkUnread(ctx context.Context, session *authdomain.Session, notificationID int64) (*notificationdomain.ReadResult, error) {
	return s.setRead(ctx, session, notificationID, false)
}

func (s *NotificationService) setRead(ctx context.Context, session *authdomain.Session, notificationID int64, read bool) (*notificationdomain.ReadResult, error) {
	if notificationID <= 0 {
		return nil, apperrors.New(40010, http.StatusBadRequest, "通知ID无效")
	}
	found, err := s.pg.SetUserNotificationRead(ctx, session.AppID, session.UserID, notificationID, read)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errNotificationNotFound()
	}
	counts, err := s.afterMutation(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	reason := "mark_read"
	if !read {
		reason = "mark_unread"
	}
	s.publishAsync(session.AppID, session.UserID, notificationdomain.EventRead, map[string]any{
		"ids": []int64{notificationID}, "all": false, "read": read,
	}, &counts, reason)
	return &notificationdomain.ReadResult{NotificationID: notificationID, Read: read, Counts: counts}, nil
}

func (s *NotificationService) MarkReadBatch(ctx context.Context, session *authdomain.Session, ids []int64) (*notificationdomain.ReadBatchResult, error) {
	deduped, err := normalizeNotificationIDs(ids)
	if err != nil {
		return nil, err
	}
	updated, err := s.pg.MarkNotificationsRead(ctx, session.AppID, session.UserID, deduped)
	if err != nil {
		return nil, err
	}
	counts, err := s.afterMutation(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	if len(updated) > 0 {
		s.publishAsync(session.AppID, session.UserID, notificationdomain.EventRead, map[string]any{
			"ids": updated, "all": false, "read": true,
		}, &counts, "mark_read_batch")
	}
	return &notificationdomain.ReadBatchResult{
		AppID:     session.AppID,
		UserID:    session.UserID,
		Requested: len(deduped),
		Updated:   int64(len(updated)),
		IDs:       deduped,
		Counts:    counts,
	}, nil
}

// MarkAllRead 全部标为已读；category 非空只处理这一类。
func (s *NotificationService) MarkAllRead(ctx context.Context, session *authdomain.Session, category string) (*notificationdomain.ReadAllResult, error) {
	category, err := normalizeNotificationCategoryFilter(category)
	if err != nil {
		return nil, err
	}
	updated, err := s.pg.MarkAllNotificationsRead(ctx, session.AppID, session.UserID, category)
	if err != nil {
		return nil, err
	}
	counts, err := s.afterMutation(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	if updated > 0 {
		data := map[string]any{"ids": []int64{}, "all": true, "read": true}
		if category != "" {
			data["category"] = category
		}
		s.publishAsync(session.AppID, session.UserID, notificationdomain.EventRead, data, &counts, "mark_all_read")
	}
	return &notificationdomain.ReadAllResult{ReadAll: true, Updated: updated, Category: category, Counts: counts}, nil
}

func (s *NotificationService) Delete(ctx context.Context, session *authdomain.Session, notificationID int64) (*notificationdomain.DeleteResult, error) {
	if notificationID <= 0 {
		return nil, apperrors.New(40010, http.StatusBadRequest, "通知ID无效")
	}
	deleted, err := s.pg.DeleteUserNotification(ctx, session.AppID, session.UserID, notificationID)
	if err != nil {
		return nil, err
	}
	if deleted == 0 {
		return nil, errNotificationNotFound()
	}
	counts, err := s.afterMutation(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	s.publishAsync(session.AppID, session.UserID, notificationdomain.EventDeleted, map[string]any{
		"ids": []int64{notificationID}, "cleared": false,
	}, &counts, "delete")
	return &notificationdomain.DeleteResult{
		AppID:   session.AppID,
		UserID:  session.UserID,
		Deleted: deleted,
		Counts:  counts,
	}, nil
}

// DeleteBatch 批量删除（≤ 200 条）；别人的、过期的、不存在的不计入。
func (s *NotificationService) DeleteBatch(ctx context.Context, session *authdomain.Session, ids []int64) (*notificationdomain.DeleteBatchResult, error) {
	deduped, err := normalizeNotificationIDs(ids)
	if err != nil {
		return nil, err
	}
	deleted, err := s.pg.DeleteUserNotificationsByIDs(ctx, session.AppID, session.UserID, deduped)
	if err != nil {
		return nil, err
	}
	counts, err := s.afterMutation(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	if len(deleted) > 0 {
		s.publishAsync(session.AppID, session.UserID, notificationdomain.EventDeleted, map[string]any{
			"ids": deleted, "cleared": false,
		}, &counts, "delete_batch")
	}
	return &notificationdomain.DeleteBatchResult{Requested: len(deduped), Deleted: int64(len(deleted)), IDs: deleted, Counts: counts}, nil
}

func (s *NotificationService) Clear(ctx context.Context, session *authdomain.Session, status string) (*notificationdomain.DeleteResult, error) {
	return s.ClearFiltered(ctx, session, status, "", "", "")
}

// ClearFiltered 按条件清空。status：all | read | unread（空即 all）；category 为空即全部分类。
func (s *NotificationService) ClearFiltered(ctx context.Context, session *authdomain.Session, status string, category string, notificationType string, level string) (*notificationdomain.DeleteResult, error) {
	status, err := normalizeNotificationStatus(status, true)
	if err != nil {
		return nil, err
	}
	category, err = normalizeNotificationCategoryFilter(category)
	if err != nil {
		return nil, err
	}
	notificationType = strings.TrimSpace(notificationType)
	level = strings.TrimSpace(level)
	deleted, err := s.pg.DeleteUserNotifications(ctx, session.AppID, session.UserID, status, category, notificationType, level)
	if err != nil {
		return nil, err
	}
	counts, err := s.afterMutation(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	if deleted > 0 {
		data := map[string]any{"ids": []int64{}, "cleared": true, "status": status}
		if category != "" {
			data["category"] = category
		}
		s.publishAsync(session.AppID, session.UserID, notificationdomain.EventDeleted, data, &counts, "clear")
	}
	return &notificationdomain.DeleteResult{
		AppID:      session.AppID,
		UserID:     session.UserID,
		Deleted:    deleted,
		Status:     status,
		Category:   category,
		Type:       notificationType,
		Level:      level,
		ClearedAll: true,
		Counts:     counts,
	}, nil
}

// ── 推送偏好 ──

// Preferences 各分类的提醒开关（全部分类都列出）。
func (s *NotificationService) Preferences(ctx context.Context, session *authdomain.Session) (*notificationdomain.Preferences, error) {
	prefs, err := s.pg.GetNotificationPreferences(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	return buildNotificationPreferences(prefs), nil
}

// UpdatePreferences 改提醒开关；只改传了的分类。关不可关的分类报 40347，打开它则忽略（本来就开着）。
func (s *NotificationService) UpdatePreferences(ctx context.Context, session *authdomain.Session, changes map[string]bool) (*notificationdomain.Preferences, error) {
	if len(changes) == 0 {
		return nil, apperrors.New(40010, http.StatusBadRequest, "没有要修改的分类")
	}
	writes := make(map[string]bool, len(changes))
	for key, push := range changes {
		key = strings.TrimSpace(key)
		spec, ok := notificationdomain.FindCategory(key)
		if !ok {
			return nil, errNotificationCategoryInvalid(key)
		}
		if !spec.Mutable {
			if !push {
				return nil, apperrors.New(errCodeNotificationPushLocked, http.StatusForbidden, "该类消息不能关闭提醒")
			}
			continue
		}
		writes[key] = push
	}
	if err := s.pg.SaveNotificationPreferences(ctx, session.AppID, session.UserID, writes); err != nil {
		return nil, err
	}
	return s.Preferences(ctx, session)
}

func buildNotificationPreferences(prefs map[string]bool) *notificationdomain.Preferences {
	specs := notificationdomain.Categories()
	result := &notificationdomain.Preferences{Categories: make([]notificationdomain.PreferenceItem, 0, len(specs))}
	for _, spec := range specs {
		result.Categories = append(result.Categories, notificationdomain.PreferenceItem{
			Key: spec.Key, Label: spec.Label, Mutable: spec.Mutable,
			Push: notificationdomain.PushEnabled(spec.Key, prefs),
		})
	}
	return result
}

// ── 管理端 ──

func (s *NotificationService) AdminList(ctx context.Context, appID int64, query notificationdomain.AdminListQuery) (*notificationdomain.AdminListResponse, error) {
	category, err := normalizeNotificationCategoryFilter(query.Category)
	if err != nil {
		return nil, err
	}
	status, err := normalizeAdminNotificationStatus(query.Status)
	if err != nil {
		return nil, err
	}
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
	query.Category, query.Status, query.Page, query.Limit = category, status, page, limit
	items, total, err := s.pg.ListAppNotifications(ctx, appID, query)
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
	deleted, affected, err := s.adminDelete(ctx, appID, ids, "admin_delete")
	if err != nil {
		return nil, err
	}
	return &notificationdomain.AdminDeleteResult{
		AppID:         appID,
		Requested:     len(ids),
		Deleted:       deleted,
		AffectedUsers: affected,
	}, nil
}

// adminDelete 删除并对每个受影响的用户失效缓存、推送 notification.deleted。
func (s *NotificationService) adminDelete(ctx context.Context, appID int64, ids []int64, reason string) (int64, []int64, error) {
	deleted, owners, err := s.pg.DeleteAppNotifications(ctx, appID, ids)
	if err != nil {
		return 0, nil, err
	}
	affected := make([]int64, 0, len(owners))
	byUser := make(map[int64][]int64, len(owners))
	for _, owner := range owners {
		affected = append(affected, owner.UserID)
		byUser[owner.UserID] = owner.IDs
	}
	s.invalidateCachesBatch(ctx, appID, affected)
	if len(byUser) > 0 {
		s.runEvent(func(eventCtx context.Context) { s.publishDeletedBatch(eventCtx, appID, byUser, reason) })
	}
	return deleted, affected, nil
}

func (s *NotificationService) AdminExport(ctx context.Context, appID int64, query notificationdomain.AdminExportQuery) ([]notificationdomain.AdminItem, error) {
	category, err := normalizeNotificationCategoryFilter(query.Category)
	if err != nil {
		return nil, err
	}
	query.Category = category
	if query.Status, err = normalizeAdminNotificationStatus(query.Status); err != nil {
		return nil, err
	}
	if query.Limit <= 0 {
		query.Limit = 5000
	}
	if query.Limit > 20000 {
		query.Limit = 20000
	}
	return s.pg.ListAppNotificationsForExport(ctx, appID, query)
}

func (s *NotificationService) AdminDeleteByFilter(ctx context.Context, appID int64, query notificationdomain.AdminExportQuery) (*notificationdomain.AdminDeleteFilterResult, error) {
	category, err := normalizeNotificationCategoryFilter(query.Category)
	if err != nil {
		return nil, err
	}
	query.Category = category
	if query.Status, err = normalizeAdminNotificationStatus(query.Status); err != nil {
		return nil, err
	}
	if query.Limit <= 0 {
		query.Limit = 5000
	}
	if query.Limit > 20000 {
		query.Limit = 20000
	}
	ids, err := s.pg.ResolveAppNotificationIDs(ctx, appID, query)
	if err != nil {
		return nil, err
	}
	result := &notificationdomain.AdminDeleteFilterResult{
		AppID:     appID,
		Requested: len(ids),
		Keyword:   query.Keyword,
		Category:  query.Category,
		Type:      query.Type,
		Level:     query.Level,
	}
	if len(ids) == 0 {
		return result, nil
	}
	deleted, affected, err := s.adminDelete(ctx, appID, ids, "admin_delete_by_filter")
	if err != nil {
		return nil, err
	}
	result.Deleted = deleted
	result.AffectedUsers = affected
	return result, nil
}

// ── 缓存 ──

func (s *NotificationService) invalidateCaches(ctx context.Context, appID int64, userID int64) {
	s.invalidateCachesBatch(ctx, appID, []int64{userID})
}

// invalidateCachesBatch 一批用户的列表与计数缓存、总未读、「我的」页聚合一起失效（一次管道往返）。
func (s *NotificationService) invalidateCachesBatch(ctx context.Context, appID int64, userIDs []int64) {
	if s.sessions == nil || len(userIDs) == 0 {
		return
	}
	if err := s.sessions.InvalidateNotificationCaches(ctx, appID, userIDs); err != nil {
		s.log.Warn("invalidate notification caches failed", zap.Error(err), zap.Int("users", len(userIDs)))
	}
}

// afterMutation 写操作之后：失效缓存，再从库里算一遍最新计数（不回填缓存，避免与并发写交错写回旧值）。
func (s *NotificationService) afterMutation(ctx context.Context, appID int64, userID int64) (notificationdomain.Counts, error) {
	s.invalidateCaches(ctx, appID, userID)
	return s.pg.NotificationUnreadCounts(ctx, appID, userID)
}

func (s *NotificationService) cachedCounts(ctx context.Context, appID int64, userID int64) (notificationdomain.Counts, error) {
	if s.sessions != nil {
		var cached notificationdomain.Counts
		found, err := s.sessions.GetNotificationCountsCache(ctx, appID, userID, &cached)
		if err != nil {
			s.log.Warn("load notification counts cache failed", zap.Error(err))
		} else if found {
			if cached.CategoryUnread == nil {
				cached.CategoryUnread = map[string]int64{}
			}
			return cached, nil
		}
	}
	counts, err := s.pg.NotificationUnreadCounts(ctx, appID, userID)
	if err != nil {
		return counts, err
	}
	s.cacheCounts(ctx, appID, userID, counts)
	return counts, nil
}

// cacheCounts 计数缓存的寿命不超过最早一条未读的过期时间：消息一过期，计数立刻跟着变。
func (s *NotificationService) cacheCounts(ctx context.Context, appID int64, userID int64, counts notificationdomain.Counts) {
	if s.sessions == nil {
		return
	}
	ttl := s.cacheTTL(counts.NextExpiry)
	if ttl <= 0 {
		return
	}
	if err := s.sessions.SetNotificationCountsCache(ctx, appID, userID, counts, ttl); err != nil {
		s.log.Warn("cache notification counts failed", zap.Error(err))
	}
	if err := s.sessions.SetNotificationUnreadCount(ctx, appID, userID, counts.Unread, ttl); err != nil {
		s.log.Warn("cache notification unread failed", zap.Error(err))
	}
}

// cacheTTL 默认 60 秒，遇到更早的过期时间就缩短到那一刻。
func (s *NotificationService) cacheTTL(expiries ...*time.Time) time.Duration {
	ttl := notificationCacheTTL
	now := s.now()
	for _, expiry := range expiries {
		if expiry == nil {
			continue
		}
		if until := expiry.Sub(now); until < ttl {
			ttl = until
		}
	}
	return ttl
}

// ── 实时事件 ──

// runEvent 异步跑一个事件任务：不阻塞调用方，同时在跑的任务受 eventSlots 限制。
func (s *NotificationService) runEvent(task func(ctx context.Context)) {
	if s.realtime == nil {
		return
	}
	s.events.Add(1)
	go func() {
		defer s.events.Done()
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("notification realtime event panic", zap.Any("panic", r))
			}
		}()
		s.eventSlots <- struct{}{}
		defer func() { <-s.eventSlots }()
		ctx, cancel := context.WithTimeout(context.Background(), notificationEventTimeout)
		defer cancel()
		task(ctx)
	}()
}

// waitPendingEvents 等已排队的事件任务发完（测试用）。
func (s *NotificationService) waitPendingEvents() { s.events.Wait() }

// publishCreatedAsync 新消息：notification.created（完整 Item + 计数 + 是否静默）与兼容的 state.changed。
func (s *NotificationService) publishCreatedAsync(item notificationdomain.Item, userID int64) {
	s.runEvent(func(ctx context.Context) {
		// 不在线的人收不到推送，也就不必为他查计数与偏好
		if len(s.onlineOnly(ctx, item.AppID, []int64{userID})) == 0 {
			return
		}
		counts, err := s.pg.NotificationUnreadCounts(ctx, item.AppID, userID)
		if err != nil {
			s.log.Debug("count notifications for realtime failed", zap.Error(err), zap.Int64("userId", userID))
			counts = notificationdomain.Counts{Unread: -1, CategoryUnread: map[string]int64{}}
		}
		silent := false
		if notificationdomain.PushMutable(item.Category) {
			prefs, err := s.pg.GetNotificationPreferences(ctx, item.AppID, userID)
			if err != nil {
				s.log.Debug("load notification preferences for realtime failed", zap.Error(err), zap.Int64("userId", userID))
			}
			silent = !notificationdomain.PushEnabled(item.Category, prefs)
		}
		s.emit(ctx, item.AppID, userID, notificationdomain.EventCreated, map[string]any{
			"notification": item, "silent": silent,
		}, &counts, "created")
	})
}

// publishAsync 已读 / 删除类事件。counts 为 nil 时在任务里现算（管理端删除不需要在响应里给计数）。
func (s *NotificationService) publishAsync(appID int64, userID int64, eventType string, data map[string]any, counts *notificationdomain.Counts, reason string) {
	var snapshot *notificationdomain.Counts
	if counts != nil {
		copied := *counts
		snapshot = &copied
	}
	s.runEvent(func(ctx context.Context) {
		if snapshot == nil {
			fresh, err := s.pg.NotificationUnreadCounts(ctx, appID, userID)
			if err != nil {
				s.log.Debug("count notifications for realtime failed", zap.Error(err), zap.Int64("userId", userID))
				fresh = notificationdomain.Counts{Unread: -1, CategoryUnread: map[string]int64{}}
			}
			snapshot = &fresh
		}
		s.emit(ctx, appID, userID, eventType, data, snapshot, reason)
	})
}

// emit 发新事件，再发兼容的 notification.state.changed。计数查失败（Unread = -1）时不带计数字段。
func (s *NotificationService) emit(ctx context.Context, appID int64, userID int64, eventType string, data map[string]any, counts *notificationdomain.Counts, reason string) {
	if counts.Unread >= 0 {
		data["unread"] = counts.Unread
		data["categoryUnread"] = counts.CategoryUnread
	}
	if err := s.realtime.PublishUserEvent(ctx, appID, userID, eventType, data); err != nil {
		s.log.Debug("publish realtime notification event failed", zap.Error(err), zap.String("type", eventType),
			zap.Int64("appid", appID), zap.Int64("userId", userID))
	}
	legacy := map[string]any{
		"reason":          reason,
		"refreshRequired": true,
		"stateChangedAt":  time.Now().UTC(),
	}
	if counts.Unread >= 0 {
		legacy["unread"] = counts.Unread
	}
	if err := s.realtime.PublishUserEvent(ctx, appID, userID, notificationdomain.EventStateChanged, legacy); err != nil {
		s.log.Debug("publish realtime notification state failed", zap.Error(err), zap.Int64("appid", appID), zap.Int64("userId", userID))
	}
}

// ── 小工具 ──

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

// normalizeNotificationIDs 去重、去掉非正数，空或超过 200 条报 40010。
func normalizeNotificationIDs(ids []int64) ([]int64, error) {
	deduped := dedupeNotificationIDs(ids)
	if len(deduped) == 0 {
		return nil, apperrors.New(40010, http.StatusBadRequest, "通知标识不能为空")
	}
	if len(deduped) > notificationdomain.MaxBatchIDs {
		return nil, apperrors.New(40010, http.StatusBadRequest, "一次最多处理 200 条")
	}
	return deduped, nil
}

// normalizeNotificationStatus 列表与清空的 status：空即 all；allowUnread 控制是否接受 unread。
func normalizeNotificationStatus(status string, allowUnread bool) (string, error) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "all":
		return "all", nil
	case "read":
		return "read", nil
	case "unread":
		if allowUnread {
			return "unread", nil
		}
	}
	return "", apperrors.New(40010, http.StatusBadRequest, "状态只能是 all、unread 或 read")
}

// normalizeAdminNotificationStatus 管理端筛选的已读状态：空或 all 不限，read / unread。
func normalizeAdminNotificationStatus(status string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "all":
		return "", nil
	case "read":
		return "read", nil
	case "unread":
		return "unread", nil
	}
	return "", apperrors.New(40010, http.StatusBadRequest, "状态只能是 all、unread 或 read")
}

// normalizeNotificationCategoryFilter 筛选用的分类：空或 all 即不限（返回空串），未知分类 42231。
func normalizeNotificationCategoryFilter(category string) (string, error) {
	category = strings.TrimSpace(category)
	if category == "" || category == "all" {
		return "", nil
	}
	if !notificationdomain.ValidCategory(category) {
		return "", errNotificationCategoryInvalid(category)
	}
	return category, nil
}

// cacheNotificationQueryKey 列表缓存键：筛选条件全部进键，换个分类不会读到别的分类的缓存。
func cacheNotificationQueryKey(query notificationdomain.UserListQuery) string {
	part := func(value string) string {
		if value = strings.TrimSpace(value); value == "" {
			return "all"
		}
		return value
	}
	return part(query.Status) + "|" + part(query.Category) + "|" + part(query.Type) + "|" + part(query.Level)
}
