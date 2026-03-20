package service

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	appdomain "aegis/internal/domain/app"
	authdomain "aegis/internal/domain/auth"
	userdomain "aegis/internal/domain/user"
	"aegis/internal/event"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"go.uber.org/zap"
)

type UserService struct {
	log       *zap.Logger
	pg        *pgrepo.Repository
	sessions  *redisrepo.SessionRepository
	publisher *event.Publisher
}

func NewUserService(log *zap.Logger, pg *pgrepo.Repository, sessions *redisrepo.SessionRepository, publisher *event.Publisher) *UserService {
	return &UserService{log: log, pg: pg, sessions: sessions, publisher: publisher}
}

func (s *UserService) GetMy(ctx context.Context, session *authdomain.Session) (*userdomain.MyView, error) {
	cached, err := s.sessions.GetMyView(ctx, session.AppID, session.UserID)
	if err != nil {
		s.log.Warn("load my cache failed", zap.Error(err))
	}
	if cached != nil {
		return cached, nil
	}

	user, profile, err := s.loadActiveUser(ctx, session)
	if err != nil {
		return nil, err
	}

	view := &userdomain.MyView{
		ID:           user.ID,
		AppID:        user.AppID,
		Account:      user.Account,
		Integral:     user.Integral,
		Experience:   user.Experience,
		Enabled:      user.Enabled,
		VIPExpireAt:  user.VIPExpireAt,
		IsVIP:        user.VIPExpireAt != nil && user.VIPExpireAt.After(time.Now()),
		TokenSource:  session.Provider,
		LastLoginIP:  session.IP,
		LastDeviceID: session.DeviceID,
	}
	if profile != nil {
		view.Nickname = profile.Nickname
		view.Avatar = profile.Avatar
		view.Email = profile.Email
	}
	unreadNotifications, err := s.loadUnreadNotificationCount(ctx, session)
	if err != nil {
		return nil, err
	}
	view.UnreadNotifications = unreadNotifications
	if err := s.sessions.SetMyView(ctx, session.AppID, session.UserID, *view, 60*time.Second); err != nil {
		s.log.Warn("cache my view failed", zap.Error(err))
	}
	_ = s.publisher.PublishJSON(ctx, event.SubjectUserMyAccessed, map[string]any{
		"user_id":   user.ID,
		"appid":     user.AppID,
		"token_jti": session.TokenID,
		"ip":        session.IP,
	})
	return view, nil
}

func (s *UserService) loadUnreadNotificationCount(ctx context.Context, session *authdomain.Session) (int64, error) {
	count, found, err := s.sessions.GetNotificationUnreadCount(ctx, session.AppID, session.UserID)
	if err != nil {
		s.log.Warn("load unread notification cache failed", zap.Error(err))
	} else if found {
		return count, nil
	}
	count, err = s.pg.CountUnreadNotifications(ctx, session.AppID, session.UserID)
	if err != nil {
		return 0, err
	}
	if err := s.sessions.SetNotificationUnreadCount(ctx, session.AppID, session.UserID, count, 60*time.Second); err != nil {
		s.log.Warn("cache unread notification count failed", zap.Error(err))
	}
	return count, nil
}

func (s *UserService) GetProfile(ctx context.Context, session *authdomain.Session) (*userdomain.Profile, error) {
	cached, err := s.sessions.GetUserProfile(ctx, session.AppID, session.UserID)
	if err != nil {
		s.log.Warn("load profile cache failed", zap.Error(err))
	}
	if cached != nil {
		return cached, nil
	}

	_, profile, err := s.loadActiveUser(ctx, session)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		profile = &userdomain.Profile{UserID: session.UserID, Extra: map[string]any{}}
	}
	if err := s.sessions.SetUserProfile(ctx, session.AppID, session.UserID, *profile, 60*time.Second); err != nil {
		s.log.Warn("cache profile failed", zap.Error(err))
	}
	return profile, nil
}

func (s *UserService) UpdateProfile(ctx context.Context, session *authdomain.Session, input userdomain.ProfileUpdate) (*userdomain.Profile, error) {
	user, profile, err := s.loadActiveUser(ctx, session)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		profile = &userdomain.Profile{UserID: user.ID, Extra: map[string]any{}}
		profile.UserID = user.ID
	}
	profile.Nickname = strings.TrimSpace(input.Nickname)
	profile.Avatar = strings.TrimSpace(input.Avatar)
	profile.Email = strings.TrimSpace(input.Email)
	if profile.Extra == nil {
		profile.Extra = map[string]any{}
	}
	if err := s.pg.UpsertUserProfile(ctx, *profile); err != nil {
		return nil, err
	}
	_ = s.sessions.DeleteMyView(ctx, session.AppID, session.UserID)
	_ = s.sessions.DeleteUserProfile(ctx, session.AppID, session.UserID)
	_ = s.publisher.PublishJSON(ctx, event.SubjectUserProfileRefresh, map[string]any{"user_id": session.UserID, "appid": session.AppID})
	profile.UpdatedAt = time.Now().UTC()
	return profile, nil
}

func (s *UserService) ListAdminUsers(ctx context.Context, appID int64, query userdomain.AdminUserQuery) (*userdomain.AdminUserListResult, error) {
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

	items, total, err := s.pg.ListAdminUsersByApp(ctx, appID, query.Keyword, query.Enabled, page, limit)
	if err != nil {
		return nil, err
	}
	return &userdomain.AdminUserListResult{
		Items:      items,
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: calcPages(total, limit),
	}, nil
}

func (s *UserService) GetAdminUser(ctx context.Context, appID int64, userID int64) (*userdomain.AdminUserView, error) {
	item, err := s.pg.GetAdminUserByApp(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	return item, nil
}

func (s *UserService) ExportAdminUsers(ctx context.Context, appID int64, query userdomain.AdminUserQuery) ([]userdomain.AdminUserView, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 5000
	}
	if limit > 20000 {
		limit = 20000
	}
	return s.pg.ListAdminUsersForExport(ctx, appID, query.Keyword, query.Enabled, limit)
}

func (s *UserService) UpdateAdminUserStatus(ctx context.Context, appID int64, userID int64, mutation userdomain.AdminUserStatusMutation) (*userdomain.AdminUserView, error) {
	if mutation.Enabled == nil && mutation.DisabledEndTime == nil && !mutation.ClearDisabledEndTime && mutation.DisabledReason == nil {
		return nil, apperrors.New(40024, http.StatusBadRequest, "缺少可更新的状态字段")
	}
	item, err := s.pg.UpdateAdminUserStatus(ctx, appID, userID, mutation)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}

	s.invalidateAdminUserCaches(ctx, appID, userID)

	shouldRevoke := !item.Enabled
	if item.DisabledEndTime != nil && item.DisabledEndTime.After(time.Now()) {
		shouldRevoke = true
	}
	if shouldRevoke && s.sessions != nil {
		sessions, err := s.sessions.ListUserSessions(ctx, appID, userID)
		if err != nil {
			s.log.Warn("list user sessions failed", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
		} else {
			for _, session := range sessions {
				if err := s.sessions.DeleteSessionByHash(ctx, appID, userID, session.TokenHash); err != nil {
					s.log.Warn("delete user session failed", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.String("tokenHash", session.TokenHash), zap.Error(err))
				}
			}
		}
	}

	return item, nil
}

func (s *UserService) BatchUpdateAdminUserStatus(ctx context.Context, appID int64, mutation userdomain.AdminUserBatchStatusMutation) (*userdomain.AdminUserBatchStatusResult, error) {
	if len(mutation.UserIDs) == 0 {
		return nil, apperrors.New(40024, http.StatusBadRequest, "用户标识不能为空")
	}
	if mutation.Enabled == nil && mutation.DisabledEndTime == nil && !mutation.ClearDisabledEndTime && mutation.DisabledReason == nil {
		return nil, apperrors.New(40024, http.StatusBadRequest, "缺少可更新的状态字段")
	}

	deduped := make([]int64, 0, len(mutation.UserIDs))
	seen := make(map[int64]struct{}, len(mutation.UserIDs))
	for _, userID := range mutation.UserIDs {
		if userID <= 0 {
			continue
		}
		if _, ok := seen[userID]; ok {
			continue
		}
		seen[userID] = struct{}{}
		deduped = append(deduped, userID)
	}
	if len(deduped) == 0 {
		return nil, apperrors.New(40024, http.StatusBadRequest, "用户标识不能为空")
	}

	updated, err := s.pg.BatchUpdateAdminUserStatus(ctx, appID, deduped, mutation.AdminUserStatusMutation)
	if err != nil {
		return nil, err
	}

	shouldRevoke := mutation.Enabled != nil && !*mutation.Enabled
	if mutation.DisabledEndTime != nil && mutation.DisabledEndTime.After(time.Now()) {
		shouldRevoke = true
	}

	for _, userID := range deduped {
		s.invalidateAdminUserCaches(ctx, appID, userID)
		if shouldRevoke && s.sessions != nil {
			sessions, err := s.sessions.ListUserSessions(ctx, appID, userID)
			if err != nil {
				s.log.Warn("list user sessions failed", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
				continue
			}
			for _, session := range sessions {
				if err := s.sessions.DeleteSessionByHash(ctx, appID, userID, session.TokenHash); err != nil {
					s.log.Warn("delete user session failed", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.String("tokenHash", session.TokenHash), zap.Error(err))
				}
			}
		}
	}

	return &userdomain.AdminUserBatchStatusResult{
		AppID:            appID,
		Requested:        len(mutation.UserIDs),
		Updated:          updated,
		ProcessedUserIDs: deduped,
	}, nil
}

func (s *UserService) GetSettings(ctx context.Context, session *authdomain.Session, category string) (*userdomain.Settings, error) {
	category = normalizeSettingsCategory(category)
	cached, err := s.sessions.GetUserSettings(ctx, session.AppID, session.UserID, category)
	if err != nil {
		s.log.Warn("load settings cache failed", zap.Error(err), zap.String("category", category))
	}
	if cached != nil {
		return cached, nil
	}

	user, _, err := s.loadActiveUser(ctx, session)
	if err != nil {
		return nil, err
	}
	item, err := s.pg.GetUserSettings(ctx, user.ID, category)
	if err != nil {
		return nil, err
	}
	if item == nil {
		item = &userdomain.Settings{
			UserID:    user.ID,
			Category:  category,
			Settings:  defaultSettings(category),
			Version:   1,
			IsActive:  true,
			UpdatedAt: time.Now().UTC(),
		}
	}
	if err := s.sessions.SetUserSettings(ctx, session.AppID, session.UserID, category, *item, 60*time.Second); err != nil {
		s.log.Warn("cache settings failed", zap.Error(err), zap.String("category", category))
	}
	return item, nil
}

func (s *UserService) ResetSettings(ctx context.Context, session *authdomain.Session, category string) (*userdomain.Settings, error) {
	category = normalizeSettingsCategory(category)
	return s.UpdateSettings(ctx, session, category, defaultSettings(category))
}

func (s *UserService) ListSettingCategories() []string {
	return []string{"general", "autoSign", "notifications", "privacy", "ui", "security"}
}

func (s *UserService) UpdateSettings(ctx context.Context, session *authdomain.Session, category string, payload map[string]any) (*userdomain.Settings, error) {
	category = normalizeSettingsCategory(category)
	user, _, err := s.loadActiveUser(ctx, session)
	if err != nil {
		return nil, err
	}
	current, err := s.pg.GetUserSettings(ctx, user.ID, category)
	if err != nil {
		return nil, err
	}
	version := 1
	if current != nil {
		version = current.Version + 1
	}
	item := userdomain.Settings{
		UserID:    user.ID,
		Category:  category,
		Settings:  cloneMap(payload),
		Version:   version,
		IsActive:  true,
		UpdatedAt: time.Now().UTC(),
	}
	if err := s.pg.UpsertUserSettings(ctx, item); err != nil {
		return nil, err
	}
	_ = s.sessions.DeleteUserSettings(ctx, session.AppID, session.UserID, category)
	_ = s.sessions.DeleteMyView(ctx, session.AppID, session.UserID)
	if err := s.sessions.SetUserSettings(ctx, session.AppID, session.UserID, category, item, 60*time.Second); err != nil {
		s.log.Warn("cache settings after update failed", zap.Error(err), zap.String("category", category))
	}
	if category == "autoSign" {
		_ = s.publisher.PublishJSON(ctx, event.SubjectUserAutoSignSync, map[string]any{
			"user_id": session.UserID,
			"appid":   session.AppID,
		})
	}
	return &item, nil
}

func (s *UserService) GetSecurityStatus(ctx context.Context, session *authdomain.Session) (*userdomain.SecurityStatus, error) {
	cached, err := s.sessions.GetSecurityStatus(ctx, session.AppID, session.UserID)
	if err != nil {
		s.log.Warn("load security cache failed", zap.Error(err))
	}
	if cached != nil {
		return cached, nil
	}

	user, profile, err := s.loadActiveUser(ctx, session)
	if err != nil {
		return nil, err
	}
	providers, err := s.pg.ListOAuthProvidersByUserID(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	status := userdomain.SecurityStatus{
		HasPassword:            user.PasswordHash != "",
		TwoFactorEnabled:       boolFromExtra(profile, "two_factor_enabled"),
		TwoFactorMethod:        stringFromExtra(profile, "two_factor_method"),
		PasskeyEnabled:         boolFromExtra(profile, "passkey_enabled"),
		PasswordStrengthScore:  intFromExtra(profile, "password_strength_score"),
		PasswordChangeRequired: boolFromExtra(profile, "password_change_required"),
		PasswordChangedAt:      timeFromExtra(profile, "password_changed_at"),
		PasswordExpiresAt:      timeFromExtra(profile, "password_expires_at"),
		OAuth2Bindings:         len(providers),
		OAuth2Providers:        providers,
	}
	if err := s.sessions.SetSecurityStatus(ctx, session.AppID, session.UserID, status, 60*time.Second); err != nil {
		s.log.Warn("cache security status failed", zap.Error(err))
	}
	return &status, nil
}

func (s *UserService) ListSessions(ctx context.Context, session *authdomain.Session) (*userdomain.SessionListResult, error) {
	if s.sessions == nil {
		return nil, apperrors.New(50301, http.StatusServiceUnavailable, "会话管理未启用")
	}
	if _, _, err := s.loadActiveUser(ctx, session); err != nil {
		return nil, err
	}

	items, err := s.sessions.ListUserSessions(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	result := &userdomain.SessionListResult{
		Items: make([]userdomain.SessionView, 0, len(items)),
		Total: len(items),
	}
	for _, item := range items {
		view := userdomain.SessionView{
			TokenHash: item.TokenHash,
			Current:   item.Session.TokenID == session.TokenID,
			Account:   item.Session.Account,
			Provider:  item.Session.Provider,
			DeviceID:  item.Session.DeviceID,
			IP:        item.Session.IP,
			UserAgent: item.Session.UserAgent,
			IssuedAt:  item.Session.IssuedAt,
			ExpiresAt: item.Session.ExpiresAt,
		}
		result.Items = append(result.Items, view)
	}
	sort.Slice(result.Items, func(i, j int) bool {
		return result.Items[i].IssuedAt.After(result.Items[j].IssuedAt)
	})
	return result, nil
}

func (s *UserService) RevokeSession(ctx context.Context, session *authdomain.Session, tokenHash string) (*userdomain.SessionRevokeResult, error) {
	if s.sessions == nil {
		return nil, apperrors.New(50301, http.StatusServiceUnavailable, "会话管理未启用")
	}
	tokenHash = strings.TrimSpace(tokenHash)
	if tokenHash == "" {
		return nil, apperrors.New(40026, http.StatusBadRequest, "会话标识不能为空")
	}
	if _, _, err := s.loadActiveUser(ctx, session); err != nil {
		return nil, err
	}

	items, err := s.sessions.ListUserSessions(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	var target *authdomain.IndexedSession
	for i := range items {
		if items[i].TokenHash == tokenHash {
			target = &items[i]
			break
		}
	}
	if target == nil {
		return nil, apperrors.New(40413, http.StatusNotFound, "会话不存在")
	}

	if err := s.revokeIndexedSession(ctx, session.AppID, session.UserID, *target, "revoked_by_user"); err != nil {
		return nil, err
	}
	return &userdomain.SessionRevokeResult{
		AppID:         session.AppID,
		UserID:        session.UserID,
		Revoked:       1,
		RevokedTokens: []string{target.TokenHash},
		CurrentKilled: target.Session.TokenID == session.TokenID,
	}, nil
}

func (s *UserService) RevokeAllSessions(ctx context.Context, session *authdomain.Session, includeCurrent bool) (*userdomain.SessionRevokeResult, error) {
	if s.sessions == nil {
		return nil, apperrors.New(50301, http.StatusServiceUnavailable, "会话管理未启用")
	}
	if _, _, err := s.loadActiveUser(ctx, session); err != nil {
		return nil, err
	}

	items, err := s.sessions.ListUserSessions(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	result := &userdomain.SessionRevokeResult{
		AppID:         session.AppID,
		UserID:        session.UserID,
		RevokedTokens: make([]string, 0, len(items)),
	}
	for _, item := range items {
		if !includeCurrent && item.Session.TokenID == session.TokenID {
			continue
		}
		if err := s.revokeIndexedSession(ctx, session.AppID, session.UserID, item, "revoked_all_by_user"); err != nil {
			return nil, err
		}
		result.Revoked++
		result.RevokedTokens = append(result.RevokedTokens, item.TokenHash)
		if item.Session.TokenID == session.TokenID {
			result.CurrentKilled = true
		}
	}
	return result, nil
}

func (s *UserService) ListLoginAudits(ctx context.Context, session *authdomain.Session, query userdomain.LoginAuditQuery) (*userdomain.LoginAuditListResult, error) {
	if _, _, err := s.loadActiveUser(ctx, session); err != nil {
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
	items, total, err := s.pg.ListLoginAuditsByUser(ctx, session.AppID, session.UserID, userdomain.LoginAuditQuery{
		Status: query.Status,
		Page:   page,
		Limit:  limit,
	})
	if err != nil {
		return nil, err
	}
	return &userdomain.LoginAuditListResult{
		Items:      mapLoginAuditItems(items),
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: calcPages(total, limit),
	}, nil
}

func (s *UserService) ExportLoginAudits(ctx context.Context, session *authdomain.Session, query userdomain.LoginAuditExportQuery) ([]userdomain.LoginAuditItem, error) {
	if _, _, err := s.loadActiveUser(ctx, session); err != nil {
		return nil, err
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 5000
	}
	if limit > 20000 {
		limit = 20000
	}
	items, err := s.pg.ListLoginAuditsByUserForExport(ctx, session.AppID, session.UserID, userdomain.LoginAuditExportQuery{
		Status: query.Status,
		Limit:  limit,
	})
	if err != nil {
		return nil, err
	}
	return mapLoginAuditItems(items), nil
}

func (s *UserService) ListSessionAudits(ctx context.Context, session *authdomain.Session, query userdomain.SessionAuditQuery) (*userdomain.SessionAuditListResult, error) {
	if _, _, err := s.loadActiveUser(ctx, session); err != nil {
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
	items, total, err := s.pg.ListSessionAuditsByUser(ctx, session.AppID, session.UserID, userdomain.SessionAuditQuery{
		EventType: query.EventType,
		Page:      page,
		Limit:     limit,
	})
	if err != nil {
		return nil, err
	}
	return &userdomain.SessionAuditListResult{
		Items:      mapSessionAuditItems(items),
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: calcPages(total, limit),
	}, nil
}

func (s *UserService) ExportSessionAudits(ctx context.Context, session *authdomain.Session, query userdomain.SessionAuditExportQuery) ([]userdomain.SessionAuditItem, error) {
	if _, _, err := s.loadActiveUser(ctx, session); err != nil {
		return nil, err
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 5000
	}
	if limit > 20000 {
		limit = 20000
	}
	items, err := s.pg.ListSessionAuditsByUserForExport(ctx, session.AppID, session.UserID, userdomain.SessionAuditExportQuery{
		EventType: query.EventType,
		Limit:     limit,
	})
	if err != nil {
		return nil, err
	}
	return mapSessionAuditItems(items), nil
}

func (s *UserService) loadActiveUser(ctx context.Context, session *authdomain.Session) (*userdomain.User, *userdomain.Profile, error) {
	user, err := s.pg.GetUserByID(ctx, session.UserID)
	if err != nil {
		return nil, nil, err
	}
	if user == nil || user.AppID != session.AppID {
		return nil, nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	if !user.Enabled {
		return nil, nil, apperrors.New(40301, http.StatusForbidden, "用户账户已被禁用")
	}
	if user.DisabledEndTime != nil && user.DisabledEndTime.After(time.Now()) {
		return nil, nil, apperrors.New(40302, http.StatusForbidden, "用户账户暂时被冻结")
	}
	profile, err := s.pg.GetUserProfileByUserID(ctx, user.ID)
	if err != nil {
		return nil, nil, err
	}
	return user, profile, nil
}

func normalizeSettingsCategory(category string) string {
	category = strings.TrimSpace(category)
	if category == "" {
		return "general"
	}
	return category
}

func defaultSettings(category string) map[string]any {
	switch category {
	case "general":
		return map[string]any{"language": "zh-CN", "displayName": "", "bio": "", "profileVisibility": "public"}
	case "autoSign":
		return map[string]any{"enabled": false, "time": "00:00", "retryOnFail": true, "maxRetries": 3, "notifyOnSuccess": true, "notifyOnFail": true, "disableLocationTracking": true}
	case "notifications":
		return map[string]any{"enabled": true, "types": map[string]any{"payment": true, "system": true, "security": true, "promotion": false}, "methods": map[string]any{"websocket": true, "email": false, "sms": false}}
	case "privacy":
		return map[string]any{"showOnlineStatus": true, "allowDirectMessage": true, "shareActivityStatus": false}
	case "ui":
		return map[string]any{"theme": "auto", "language": "zh-CN", "timezone": "Asia/Shanghai", "dateFormat": "YYYY-MM-DD", "timeFormat": "24h"}
	case "security":
		return map[string]any{"twoFactorEnabled": false, "loginNotification": true, "suspiciousActivityAlert": true, "sessionTimeout": 30}
	default:
		return map[string]any{}
	}
}

func cloneMap(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(value))
	for k, v := range value {
		result[k] = v
	}
	return result
}

func boolFromExtra(profile *userdomain.Profile, key string) bool {
	if profile == nil || profile.Extra == nil {
		return false
	}
	value, ok := profile.Extra[key]
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return typed == "true" || typed == "1"
	case float64:
		return typed != 0
	case int:
		return typed != 0
	default:
		return false
	}
}

func stringFromExtra(profile *userdomain.Profile, key string) string {
	if profile == nil || profile.Extra == nil {
		return ""
	}
	value, ok := profile.Extra[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", value))
}

func intFromExtra(profile *userdomain.Profile, key string) int {
	if profile == nil || profile.Extra == nil {
		return 0
	}
	value, ok := profile.Extra[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		var parsed int
		_, _ = fmt.Sscanf(strings.TrimSpace(typed), "%d", &parsed)
		return parsed
	default:
		return 0
	}
}

func timeFromExtra(profile *userdomain.Profile, key string) *time.Time {
	if profile == nil || profile.Extra == nil {
		return nil
	}
	value, ok := profile.Extra[key]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case time.Time:
		v := typed.UTC()
		return &v
	case string:
		for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05", time.RFC3339Nano} {
			parsed, err := time.Parse(layout, strings.TrimSpace(typed))
			if err == nil {
				v := parsed.UTC()
				return &v
			}
		}
	}
	return nil
}

func calcPages(total int64, limit int) int {
	if limit <= 0 {
		return 1
	}
	pages := int((total + int64(limit) - 1) / int64(limit))
	if pages == 0 {
		return 1
	}
	return pages
}

func (s *UserService) revokeIndexedSession(ctx context.Context, appID int64, userID int64, item authdomain.IndexedSession, eventType string) error {
	if err := s.sessions.DeleteSessionByHash(ctx, appID, userID, item.TokenHash); err != nil {
		return err
	}
	if ttl := time.Until(item.Session.ExpiresAt); ttl > 0 {
		if err := s.sessions.BlacklistToken(ctx, item.Session.TokenID, ttl); err != nil {
			return err
		}
	}
	_ = s.publisher.PublishJSON(ctx, event.SubjectSessionAuditRequested, map[string]any{
		"user_id":    userID,
		"appid":      appID,
		"token_jti":  item.Session.TokenID,
		"event_type": eventType,
		"ip":         item.Session.IP,
		"device_id":  item.Session.DeviceID,
		"user_agent": item.Session.UserAgent,
		"provider":   item.Session.Provider,
	})
	return nil
}

func mapLoginAuditItems(items []appdomain.LoginAuditItem) []userdomain.LoginAuditItem {
	result := make([]userdomain.LoginAuditItem, 0, len(items))
	for _, item := range items {
		result = append(result, userdomain.LoginAuditItem{
			ID:        item.ID,
			AppID:     item.AppID,
			LoginType: item.LoginType,
			Provider:  item.Provider,
			TokenJTI:  item.TokenJTI,
			LoginIP:   item.LoginIP,
			DeviceID:  item.DeviceID,
			UserAgent: item.UserAgent,
			Status:    item.Status,
			Metadata:  item.Metadata,
			CreatedAt: item.CreatedAt,
		})
	}
	return result
}

func mapSessionAuditItems(items []appdomain.SessionAuditItem) []userdomain.SessionAuditItem {
	result := make([]userdomain.SessionAuditItem, 0, len(items))
	for _, item := range items {
		result = append(result, userdomain.SessionAuditItem{
			ID:        item.ID,
			AppID:     item.AppID,
			TokenJTI:  item.TokenJTI,
			EventType: item.EventType,
			Metadata:  item.Metadata,
			CreatedAt: item.CreatedAt,
		})
	}
	return result
}

func (s *UserService) invalidateAdminUserCaches(ctx context.Context, appID int64, userID int64) {
	if s.sessions == nil {
		return
	}
	_ = s.sessions.DeleteMyView(ctx, appID, userID)
	_ = s.sessions.DeleteUserProfile(ctx, appID, userID)
	_ = s.sessions.DeleteSecurityStatus(ctx, appID, userID)
	_ = s.sessions.DeleteNotificationUnreadCount(ctx, appID, userID)
	for _, category := range []string{"general", "autoSign", "notifications", "privacy", "ui", "security"} {
		_ = s.sessions.DeleteUserSettings(ctx, appID, userID, category)
	}
}
