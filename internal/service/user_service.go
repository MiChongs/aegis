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
	captchadomain "aegis/internal/domain/captcha"
	plugindomain "aegis/internal/domain/plugin"
	userdomain "aegis/internal/domain/user"
	"aegis/internal/event"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

func hashUserPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

type UserService struct {
	log       *zap.Logger
	pg        *pgrepo.Repository
	sessions  *redisrepo.SessionRepository
	publisher *event.Publisher
	security  *SecurityService
	email     *EmailService
	captcha   *CaptchaService
	plugin    *PluginService
	search    *AdminUserSearchService
	ban       *AccountBanService
}

const (
	profileChangeTTL          = 15 * time.Minute
	profileChangeFieldEmail   = "email"
	profileChangeFieldPhone   = "phone"
	profileChangeEmailPurpose = "profile_email_change"
	profileChangePhonePurpose = "profile_phone_change"
)

func (s *UserService) SetPluginService(p *PluginService) { s.plugin = p }

func (s *UserService) SetVerificationServices(email *EmailService, captcha *CaptchaService) {
	s.email = email
	s.captcha = captcha
}

func (s *UserService) SetAdminUserSearchService(search *AdminUserSearchService) {
	s.search = search
}

func (s *UserService) SetAccountBanService(ban *AccountBanService) {
	s.ban = ban
}

func NewUserService(log *zap.Logger, pg *pgrepo.Repository, sessions *redisrepo.SessionRepository, publisher *event.Publisher, security *SecurityService) *UserService {
	return &UserService{log: log, pg: pg, sessions: sessions, publisher: publisher, security: security}
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

func (s *UserService) UpdateProfile(ctx context.Context, session *authdomain.Session, input userdomain.ProfileUpdate) (*userdomain.ProfileUpdateResult, error) {
	user, profile, err := s.loadActiveUser(ctx, session)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		profile = &userdomain.Profile{UserID: user.ID, Extra: map[string]any{}}
		profile.UserID = user.ID
	}
	profileChanged := false
	if v := strings.TrimSpace(input.Nickname); v != "" {
		profile.Nickname = v
		profileChanged = true
	}
	if v := strings.TrimSpace(input.Avatar); v != "" {
		profile.Avatar = v
		profileChanged = true
	}
	if v := strings.TrimSpace(input.Birthday); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			profile.Birthday = &t
			profileChanged = true
		}
	}
	if v := strings.TrimSpace(input.Bio); v != "" {
		profile.Bio = v
		profileChanged = true
	}
	if input.Contacts != nil {
		profile.Contacts = input.Contacts
		profileChanged = true
	}
	if profile.Extra == nil {
		profile.Extra = map[string]any{}
	}
	if v := strings.TrimSpace(input.Email); v != "" && !strings.EqualFold(v, strings.TrimSpace(profile.Email)) {
		if err := s.queueSensitiveProfileChange(ctx, session, profileChangeFieldEmail, v); err != nil {
			return nil, err
		}
	}
	if v := strings.TrimSpace(input.Phone); v != "" && v != strings.TrimSpace(profile.Phone) {
		if err := s.queueSensitiveProfileChange(ctx, session, profileChangeFieldPhone, v); err != nil {
			return nil, err
		}
	}
	if profileChanged {
		if err := s.persistProfileUpdate(ctx, session, user.ID, profile); err != nil {
			return nil, err
		}
	}
	pendingChanges, err := s.loadPendingProfileChanges(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	return &userdomain.ProfileUpdateResult{
		Profile:        profile,
		PendingChanges: pendingChanges,
	}, nil
}

func (s *UserService) ConfirmSensitiveProfileChange(ctx context.Context, session *authdomain.Session, field string, code string) (*userdomain.ProfileUpdateResult, error) {
	field = strings.TrimSpace(strings.ToLower(field))
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "验证码不能为空")
	}
	user, profile, err := s.loadActiveUser(ctx, session)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		profile = &userdomain.Profile{UserID: user.ID, Extra: map[string]any{}}
	}
	change, err := s.sessions.GetPendingProfileChange(ctx, session.AppID, session.UserID, field)
	if err != nil {
		return nil, err
	}
	if change == nil {
		return nil, apperrors.New(40401, http.StatusNotFound, "待确认资料变更不存在或已过期")
	}
	switch field {
	case profileChangeFieldEmail:
		if s.email == nil {
			return nil, apperrors.New(50310, http.StatusServiceUnavailable, "邮箱验证服务暂不可用")
		}
		valid, err := s.email.VerifyCode(ctx, session.AppID, change.Value, code, change.Purpose)
		if err != nil {
			return nil, err
		}
		if !valid {
			return nil, apperrors.New(40021, http.StatusBadRequest, "邮箱验证码错误或已失效")
		}
		ownerID, err := s.pg.FindUserIDByProfileEmail(ctx, session.AppID, change.Value)
		if err != nil {
			return nil, err
		}
		if ownerID > 0 && ownerID != session.UserID {
			return nil, apperrors.New(40901, http.StatusConflict, "邮箱已被其他账号占用")
		}
		profile.Email = change.Value
	case profileChangeFieldPhone:
		if s.captcha == nil {
			return nil, apperrors.New(50310, http.StatusServiceUnavailable, "短信验证服务暂不可用")
		}
		valid, err := s.captcha.VerifySMSCode(ctx, captchadomain.SMSVerifyRequest{
			AppID:   session.AppID,
			Phone:   change.Value,
			Code:    code,
			Purpose: captchadomain.Purpose(change.Purpose),
		})
		if err != nil {
			return nil, err
		}
		if !valid {
			return nil, apperrors.New(40021, http.StatusBadRequest, "短信验证码错误或已失效")
		}
		ownerID, err := s.pg.FindUserIDByProfilePhone(ctx, session.AppID, change.Value)
		if err != nil {
			return nil, err
		}
		if ownerID > 0 && ownerID != session.UserID {
			return nil, apperrors.New(40901, http.StatusConflict, "手机号已被其他账号占用")
		}
		profile.Phone = change.Value
	default:
		return nil, apperrors.New(40000, http.StatusBadRequest, "不支持的资料变更字段")
	}
	if err := s.persistProfileUpdate(ctx, session, user.ID, profile); err != nil {
		return nil, err
	}
	if err := s.sessions.DeletePendingProfileChange(ctx, session.AppID, session.UserID, field); err != nil {
		s.log.Warn("delete pending profile change failed", zap.Int64("appid", session.AppID), zap.Int64("userId", session.UserID), zap.String("field", field), zap.Error(err))
	}
	pendingChanges, err := s.loadPendingProfileChanges(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	return &userdomain.ProfileUpdateResult{
		Profile:        profile,
		PendingChanges: pendingChanges,
	}, nil
}

func (s *UserService) queueSensitiveProfileChange(ctx context.Context, session *authdomain.Session, field string, value string) error {
	value = strings.TrimSpace(value)
	now := time.Now().UTC()
	change := userdomain.PendingProfileChange{
		Field:       field,
		Value:       value,
		ExpiresAt:   now.Add(profileChangeTTL),
		RequestedAt: now,
	}
	switch field {
	case profileChangeFieldEmail:
		if s.email == nil {
			return apperrors.New(50310, http.StatusServiceUnavailable, "邮箱验证服务暂不可用")
		}
		ownerID, err := s.pg.FindUserIDByProfileEmail(ctx, session.AppID, value)
		if err != nil {
			return err
		}
		if ownerID > 0 && ownerID != session.UserID {
			return apperrors.New(40901, http.StatusConflict, "邮箱已被其他账号占用")
		}
		change.Purpose = profileChangeEmailPurpose
		change.MaskedValue = maskEmail(value)
	case profileChangeFieldPhone:
		if s.captcha == nil {
			return apperrors.New(50310, http.StatusServiceUnavailable, "短信验证服务暂不可用")
		}
		ownerID, err := s.pg.FindUserIDByProfilePhone(ctx, session.AppID, value)
		if err != nil {
			return err
		}
		if ownerID > 0 && ownerID != session.UserID {
			return apperrors.New(40901, http.StatusConflict, "手机号已被其他账号占用")
		}
		change.Purpose = profileChangePhonePurpose
		change.MaskedValue = maskPhoneValue(value)
	default:
		return apperrors.New(40000, http.StatusBadRequest, "不支持的资料变更字段")
	}
	return s.sessions.SetPendingProfileChange(ctx, session.AppID, session.UserID, change, profileChangeTTL)
}

func (s *UserService) loadPendingProfileChanges(ctx context.Context, appID int64, userID int64) ([]userdomain.PendingProfileChange, error) {
	items, err := s.sessions.ListPendingProfileChanges(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].RequestedAt.Before(items[j].RequestedAt)
	})
	return items, nil
}

func (s *UserService) persistProfileUpdate(ctx context.Context, session *authdomain.Session, userID int64, profile *userdomain.Profile) error {
	if err := s.pg.UpsertUserProfile(ctx, *profile); err != nil {
		return err
	}
	_ = s.sessions.DeleteMyView(ctx, session.AppID, session.UserID)
	_ = s.sessions.DeleteUserProfile(ctx, session.AppID, session.UserID)
	_ = s.publisher.PublishJSON(ctx, event.SubjectUserProfileRefresh, map[string]any{"user_id": session.UserID, "appid": session.AppID})
	profile.UpdatedAt = time.Now().UTC()
	if s.plugin != nil {
		go s.plugin.ExecuteHook(context.Background(), HookUserProfileUpdated, map[string]any{
			"userId": userID,
			"appId":  session.AppID,
		}, plugindomain.HookMetadata{UserID: &userID, AppID: &session.AppID})
	}
	s.syncAdminUserSearch(session.AppID, userID)
	return nil
}

func maskEmail(value string) string {
	parts := strings.Split(strings.TrimSpace(value), "@")
	if len(parts) != 2 {
		return value
	}
	local := parts[0]
	if len(local) == 0 {
		return "***@" + parts[1]
	}
	if len(local) <= 2 {
		return local[:1] + "***@" + parts[1]
	}
	return local[:1] + "***" + local[len(local)-1:] + "@" + parts[1]
}

func maskPhoneValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 7 {
		return "***"
	}
	return value[:3] + "****" + value[len(value)-4:]
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
	query.Page = page
	query.Limit = limit

	if shouldUseAdminUserSearch(query) && s.search != nil {
		ids, total, err := s.search.SearchUsers(ctx, appID, query)
		if err == nil {
			items, err := s.pg.ListAdminUsersByIDs(ctx, appID, ids)
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
		s.log.Warn("admin user search fallback to postgres", zap.Int64("appid", appID), zap.Error(err))
	}

	items, total, err := s.pg.ListAdminUsersByAppQuery(ctx, appID, query, page, limit)
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
	query.Page = 1
	query.Limit = limit
	if shouldUseAdminUserSearch(query) && s.search != nil {
		ids, _, err := s.search.SearchUsers(ctx, appID, query)
		if err == nil {
			return s.pg.ListAdminUsersByIDs(ctx, appID, ids)
		}
		s.log.Warn("admin user export search fallback to postgres", zap.Int64("appid", appID), zap.Error(err))
	}
	return s.pg.ListAdminUsersForExportQuery(ctx, appID, query, limit)
}

func (s *UserService) UpdateAdminUserStatus(ctx context.Context, appID int64, userID int64, mutation userdomain.AdminUserStatusMutation, operator userdomain.BanOperator) (*userdomain.AdminUserView, error) {
	if mutation.Enabled == nil && mutation.DisabledEndTime == nil && !mutation.ClearDisabledEndTime && mutation.DisabledReason == nil {
		return nil, apperrors.New(40024, http.StatusBadRequest, "缺少可更新的状态字段")
	}
	if s.ban != nil {
		if changed, err := s.applyStatusMutationToBan(ctx, appID, userID, mutation, operator); err != nil {
			return nil, err
		} else if changed {
			s.invalidateAdminUserCaches(ctx, appID, userID)
			s.syncAdminUserSearch(appID, userID)
			item, err := s.pg.GetAdminUserByApp(ctx, appID, userID)
			if err != nil {
				return nil, err
			}
			if item == nil {
				return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
			}
			return item, nil
		}
	}
	item, err := s.pg.UpdateAdminUserStatus(ctx, appID, userID, mutation)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}

	s.invalidateAdminUserCaches(ctx, appID, userID)
	s.syncAdminUserSearch(appID, userID)

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

	if shouldRevoke && s.plugin != nil {
		go s.plugin.ExecuteHook(context.Background(), HookUserBanned, map[string]any{
			"userId": userID,
			"status": item.Enabled,
		}, plugindomain.HookMetadata{UserID: &userID, AppID: &appID})
	}

	return item, nil
}

func (s *UserService) BatchUpdateAdminUserStatus(ctx context.Context, appID int64, mutation userdomain.AdminUserBatchStatusMutation, operator userdomain.BanOperator) (*userdomain.AdminUserBatchStatusResult, error) {
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
	if s.ban != nil {
		if changed, updated, err := s.applyBatchStatusMutationToBan(ctx, appID, deduped, mutation.AdminUserStatusMutation, operator); err != nil {
			return nil, err
		} else if changed {
			for _, userID := range deduped {
				s.invalidateAdminUserCaches(ctx, appID, userID)
				s.syncAdminUserSearch(appID, userID)
			}
			return &userdomain.AdminUserBatchStatusResult{
				AppID:            appID,
				Requested:        len(mutation.UserIDs),
				Updated:          updated,
				ProcessedUserIDs: deduped,
			}, nil
		}
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

func (s *UserService) CreateAdminUserBan(ctx context.Context, appID int64, userID int64, input userdomain.AccountBanCreateInput) (*userdomain.AccountBan, error) {
	if s.ban == nil {
		return nil, apperrors.New(50321, http.StatusServiceUnavailable, "账户封禁模块未启用")
	}
	item, err := s.ban.BanUser(ctx, appID, userID, input)
	if err != nil {
		return nil, err
	}
	s.invalidateAdminUserCaches(ctx, appID, userID)
	s.syncAdminUserSearch(appID, userID)
	return item, nil
}

func (s *UserService) BatchCreateAdminUserBan(ctx context.Context, appID int64, input userdomain.AccountBanBatchCreateInput) (*userdomain.AccountBanBatchCreateResult, error) {
	if s.ban == nil {
		return nil, apperrors.New(50321, http.StatusServiceUnavailable, "账户封禁模块未启用")
	}
	item, err := s.ban.BatchBanUsers(ctx, appID, input)
	if err != nil {
		return nil, err
	}
	for _, userID := range item.ProcessedUserIDs {
		s.invalidateAdminUserCaches(ctx, appID, userID)
		s.syncAdminUserSearch(appID, userID)
	}
	return item, nil
}

func (s *UserService) ListAdminUserBans(ctx context.Context, appID int64, userID int64, query userdomain.AccountBanQuery) (*userdomain.AccountBanListResult, error) {
	if s.ban == nil {
		return nil, apperrors.New(50321, http.StatusServiceUnavailable, "账户封禁模块未启用")
	}
	return s.ban.ListBans(ctx, appID, userID, query)
}

func (s *UserService) GetAdminUserActiveBan(ctx context.Context, appID int64, userID int64) (*userdomain.AccountBan, error) {
	if s.ban == nil {
		return nil, apperrors.New(50321, http.StatusServiceUnavailable, "账户封禁模块未启用")
	}
	return s.ban.GetActiveBan(ctx, appID, userID)
}

func (s *UserService) RevokeAdminUserBan(ctx context.Context, appID int64, userID int64, banID int64, input userdomain.AccountBanRevokeInput) (*userdomain.AccountBan, error) {
	if s.ban == nil {
		return nil, apperrors.New(50321, http.StatusServiceUnavailable, "账户封禁模块未启用")
	}
	item, err := s.ban.RevokeBan(ctx, appID, userID, banID, input)
	if err != nil {
		return nil, err
	}
	s.invalidateAdminUserCaches(ctx, appID, userID)
	s.syncAdminUserSearch(appID, userID)
	return item, nil
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
	if s.security != nil {
		return s.security.GetSecurityStatus(ctx, session)
	}

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
	if ban, err := s.pg.RefreshUserAccountBanState(ctx, user.AppID, user.ID); err != nil {
		s.log.Warn("refresh user account ban state failed", zap.Int64("appid", user.AppID), zap.Int64("userId", user.ID), zap.Error(err))
	} else if ban != nil {
		if ban.BanType == userdomain.AccountBanTypePermanent {
			return nil, nil, apperrors.New(40301, http.StatusForbidden, BanMessageFromRecord(ban))
		}
		return nil, nil, apperrors.New(40302, http.StatusForbidden, BanMessageFromRecord(ban))
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

func (s *UserService) applyStatusMutationToBan(ctx context.Context, appID int64, userID int64, mutation userdomain.AdminUserStatusMutation, operator userdomain.BanOperator) (bool, error) {
	action, createInput, revokeInput, reasonOnly := deriveBanActionFromStatusMutation(mutation, operator)
	switch action {
	case "create":
		_, err := s.ban.BanUser(ctx, appID, userID, createInput)
		return true, err
	case "revoke":
		active, err := s.ban.GetActiveBan(ctx, appID, userID)
		if err != nil {
			return false, err
		}
		if active == nil {
			return false, nil
		}
		_, err = s.ban.RevokeBan(ctx, appID, userID, active.ID, revokeInput)
		return true, err
	case "reason":
		if reasonOnly == nil {
			return false, nil
		}
		active, err := s.ban.UpdateActiveBanReason(ctx, appID, userID, *reasonOnly)
		if err != nil {
			return false, err
		}
		return active != nil, nil
	default:
		return false, nil
	}
}

func (s *UserService) applyBatchStatusMutationToBan(ctx context.Context, appID int64, userIDs []int64, mutation userdomain.AdminUserStatusMutation, operator userdomain.BanOperator) (bool, int64, error) {
	action, createInput, revokeInput, reasonOnly := deriveBanActionFromStatusMutation(mutation, operator)
	switch action {
	case "create":
		result, err := s.ban.BatchBanUsers(ctx, appID, userdomain.AccountBanBatchCreateInput{
			UserIDs:               userIDs,
			AccountBanCreateInput: createInput,
		})
		if err != nil {
			return false, 0, err
		}
		return true, result.Created, nil
	case "revoke":
		var updated int64
		for _, userID := range userIDs {
			active, err := s.ban.GetActiveBan(ctx, appID, userID)
			if err != nil {
				return false, updated, err
			}
			if active == nil {
				continue
			}
			if _, err := s.ban.RevokeBan(ctx, appID, userID, active.ID, revokeInput); err != nil {
				return false, updated, err
			}
			updated++
		}
		return true, updated, nil
	case "reason":
		var updated int64
		for _, userID := range userIDs {
			active, err := s.ban.UpdateActiveBanReason(ctx, appID, userID, *reasonOnly)
			if err != nil {
				return false, updated, err
			}
			if active != nil {
				updated++
			}
		}
		return updated > 0, updated, nil
	default:
		return false, 0, nil
	}
}

func deriveBanActionFromStatusMutation(mutation userdomain.AdminUserStatusMutation, operator userdomain.BanOperator) (string, userdomain.AccountBanCreateInput, userdomain.AccountBanRevokeInput, *string) {
	now := time.Now()
	if mutation.DisabledEndTime != nil && mutation.DisabledEndTime.After(now) {
		return "create", userdomain.AccountBanCreateInput{
			BanType:  userdomain.AccountBanTypeTemporary,
			BanScope: userdomain.AccountBanScopeLogin,
			Reason:   statusStringValue(mutation.DisabledReason),
			EndAt:    mutation.DisabledEndTime,
			Operator: operator,
		}, userdomain.AccountBanRevokeInput{}, nil
	}
	if mutation.Enabled != nil && !*mutation.Enabled {
		return "create", userdomain.AccountBanCreateInput{
			BanType:  userdomain.AccountBanTypePermanent,
			BanScope: userdomain.AccountBanScopeLogin,
			Reason:   statusStringValue(mutation.DisabledReason),
			Operator: operator,
		}, userdomain.AccountBanRevokeInput{}, nil
	}
	if mutation.ClearDisabledEndTime || (mutation.Enabled != nil && *mutation.Enabled) || (mutation.DisabledEndTime != nil && !mutation.DisabledEndTime.After(now)) {
		return "revoke", userdomain.AccountBanCreateInput{}, userdomain.AccountBanRevokeInput{
			Reason:   statusStringValue(mutation.DisabledReason),
			Operator: operator,
		}, nil
	}
	if mutation.DisabledReason != nil {
		return "reason", userdomain.AccountBanCreateInput{}, userdomain.AccountBanRevokeInput{}, mutation.DisabledReason
	}
	return "", userdomain.AccountBanCreateInput{}, userdomain.AccountBanRevokeInput{}, nil
}

func statusStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
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

// ──────────────────────────────────────
// 管理员用户控制
// ──────────────────────────────────────

// AdminUpdateUserProfile 管理员编辑用户资料（昵称、邮箱）
func (s *UserService) AdminUpdateUserProfile(ctx context.Context, appID int64, userID int64, nickname string, email string) error {
	user, err := s.pg.GetAdminUserByApp(ctx, appID, userID)
	if err != nil {
		return err
	}
	if user == nil {
		return apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	if err := s.pg.UpsertUserProfile(ctx, userdomain.Profile{
		UserID:   userID,
		Nickname: strings.TrimSpace(nickname),
		Email:    strings.TrimSpace(email),
	}); err != nil {
		return fmt.Errorf("更新用户资料失败: %w", err)
	}
	s.invalidateAdminUserCaches(ctx, appID, userID)
	s.syncAdminUserSearch(appID, userID)
	s.log.Info("管理员更新用户资料", zap.Int64("appid", appID), zap.Int64("userId", userID))
	return nil
}

// AdminResetUserPassword 管理员重置用户密码
func (s *UserService) AdminResetUserPassword(ctx context.Context, appID int64, userID int64, newPassword string) error {
	newPassword = strings.TrimSpace(newPassword)
	if len(newPassword) < 6 {
		return apperrors.New(40025, http.StatusBadRequest, "密码长度不能少于 6 位")
	}
	user, err := s.pg.GetAdminUserByApp(ctx, appID, userID)
	if err != nil {
		return err
	}
	if user == nil {
		return apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	hash, err := hashUserPassword(newPassword)
	if err != nil {
		return fmt.Errorf("密码哈希失败: %w", err)
	}
	if err := s.pg.UpdateUserPassword(ctx, userID, hash, time.Now().UTC()); err != nil {
		return fmt.Errorf("更新密码失败: %w", err)
	}
	// 吊销所有会话，强制用户重新登录
	s.revokeAllUserSessions(ctx, appID, userID)
	s.invalidateAdminUserCaches(ctx, appID, userID)
	s.log.Info("管理员重置用户密码", zap.Int64("appid", appID), zap.Int64("userId", userID))
	return nil
}

// AdminDeleteUser 管理员删除用户（硬删除）
func (s *UserService) AdminDeleteUser(ctx context.Context, appID int64, userID int64) error {
	user, err := s.pg.GetAdminUserByApp(ctx, appID, userID)
	if err != nil {
		return err
	}
	if user == nil {
		return apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	// 先吊销会话
	s.revokeAllUserSessions(ctx, appID, userID)
	// 清理安全凭证
	if s.security != nil {
		_ = s.pg.DeleteUserTOTPSecret(ctx, appID, userID)
		_ = s.pg.DeleteUserRecoveryCodes(ctx, appID, userID)
	}
	// 删除用户（CASCADE 会清理 user_profiles、user_settings 等）
	if err := s.pg.DeleteUserByApp(ctx, appID, userID); err != nil {
		return fmt.Errorf("删除用户失败: %w", err)
	}
	s.invalidateAdminUserCaches(ctx, appID, userID)
	s.deleteAdminUserSearch(appID, userID)
	s.log.Warn("管理员删除用户", zap.Int64("appid", appID), zap.Int64("userId", userID))
	if s.plugin != nil {
		go s.plugin.ExecuteHook(context.Background(), HookUserDeleted, map[string]any{
			"userId": userID,
		}, plugindomain.HookMetadata{UserID: &userID, AppID: &appID})
	}
	return nil
}

// AdminRevokeUserSessions 管理员踢出用户所有会话
func (s *UserService) AdminRevokeUserSessions(ctx context.Context, appID int64, userID int64) error {
	user, err := s.pg.GetAdminUserByApp(ctx, appID, userID)
	if err != nil {
		return err
	}
	if user == nil {
		return apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	s.revokeAllUserSessions(ctx, appID, userID)
	s.log.Info("管理员踢出用户会话", zap.Int64("appid", appID), zap.Int64("userId", userID))
	return nil
}

// AdminListUserSessions 管理员查看用户所有活跃会话（不含位置，位置由 Handler 层解析）
func (s *UserService) AdminListUserSessions(ctx context.Context, appID int64, userID int64) ([]userdomain.SessionDetailView, error) {
	if s.sessions == nil {
		return nil, apperrors.New(50301, http.StatusServiceUnavailable, "会话管理未启用")
	}
	user, err := s.pg.GetAdminUserByApp(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	items, err := s.sessions.ListUserSessions(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	views := make([]userdomain.SessionDetailView, 0, len(items))
	for _, item := range items {
		views = append(views, userdomain.SessionDetailView{
			TokenHash: item.TokenHash,
			TokenID:   item.Session.TokenID,
			Account:   item.Session.Account,
			DeviceID:  item.Session.DeviceID,
			IP:        item.Session.IP,
			UserAgent: item.Session.UserAgent,
			Provider:  item.Session.Provider,
			IssuedAt:  item.Session.IssuedAt,
			ExpiresAt: item.Session.ExpiresAt,
		})
	}
	sort.Slice(views, func(i, j int) bool { return views[i].IssuedAt.After(views[j].IssuedAt) })
	return views, nil
}

// AdminRevokeUserSession 管理员撤销用户单个会话
func (s *UserService) AdminRevokeUserSession(ctx context.Context, appID int64, userID int64, tokenHash string) error {
	if s.sessions == nil {
		return apperrors.New(50301, http.StatusServiceUnavailable, "会话管理未启用")
	}
	tokenHash = strings.TrimSpace(tokenHash)
	if tokenHash == "" {
		return apperrors.New(40026, http.StatusBadRequest, "会话标识不能为空")
	}
	items, err := s.sessions.ListUserSessions(ctx, appID, userID)
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.TokenHash == tokenHash {
			return s.revokeIndexedSession(ctx, appID, userID, item, "revoked_by_admin")
		}
	}
	return apperrors.New(40413, http.StatusNotFound, "会话不存在")
}

// AdminRevokeUserSessionsBatch 管理员批量撤销指定会话
func (s *UserService) AdminRevokeUserSessionsBatch(ctx context.Context, appID int64, userID int64, tokenHashes []string) (int, error) {
	if s.sessions == nil {
		return 0, apperrors.New(50301, http.StatusServiceUnavailable, "会话管理未启用")
	}
	hashSet := make(map[string]struct{}, len(tokenHashes))
	for _, h := range tokenHashes {
		h = strings.TrimSpace(h)
		if h != "" {
			hashSet[h] = struct{}{}
		}
	}
	if len(hashSet) == 0 {
		return 0, apperrors.New(40026, http.StatusBadRequest, "会话标识不能为空")
	}
	items, err := s.sessions.ListUserSessions(ctx, appID, userID)
	if err != nil {
		return 0, err
	}
	revoked := 0
	for _, item := range items {
		if _, ok := hashSet[item.TokenHash]; ok {
			if err := s.revokeIndexedSession(ctx, appID, userID, item, "revoked_by_admin"); err == nil {
				revoked++
			}
		}
	}
	return revoked, nil
}

func (s *UserService) revokeAllUserSessions(ctx context.Context, appID int64, userID int64) {
	if s.sessions == nil {
		return
	}
	sessions, err := s.sessions.ListUserSessions(ctx, appID, userID)
	if err != nil {
		s.log.Warn("list user sessions failed", zap.Error(err))
		return
	}
	for _, session := range sessions {
		_ = s.sessions.DeleteSessionByHash(ctx, appID, userID, session.TokenHash)
	}
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

func (s *UserService) syncAdminUserSearch(appID int64, userID int64) {
	if s.search == nil || appID <= 0 || userID <= 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.search.IndexUser(ctx, appID, userID); err != nil {
			s.log.Warn("sync admin user search failed", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
		}
	}()
}

func (s *UserService) deleteAdminUserSearch(appID int64, userID int64) {
	if s.search == nil || appID <= 0 || userID <= 0 {
		return
	}
	if err := s.search.DeleteUser(appID, userID); err != nil {
		s.log.Warn("delete admin user search doc failed", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
	}
}

func shouldUseAdminUserSearch(query userdomain.AdminUserQuery) bool {
	return strings.TrimSpace(query.Keyword) != "" ||
		strings.TrimSpace(query.Account) != "" ||
		strings.TrimSpace(query.Nickname) != "" ||
		strings.TrimSpace(query.Email) != "" ||
		strings.TrimSpace(query.Phone) != "" ||
		strings.TrimSpace(query.RegisterIP) != "" ||
		query.UserID != nil ||
		query.Enabled != nil ||
		query.CreatedFrom != nil ||
		query.CreatedTo != nil
}
