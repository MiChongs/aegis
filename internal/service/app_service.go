package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	appdomain "aegis/internal/domain/app"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"go.uber.org/zap"
)

type AppService struct {
	log      *zap.Logger
	pg       *pgrepo.Repository
	sessions *redisrepo.SessionRepository
	location *time.Location
}

func NewAppService(log *zap.Logger, pg *pgrepo.Repository, sessions *redisrepo.SessionRepository) *AppService {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		location = time.FixedZone("CST", 8*3600)
	}
	return &AppService{log: log, pg: pg, sessions: sessions, location: location}
}

func (s *AppService) GetApp(ctx context.Context, appID int64) (*appdomain.App, error) {
	if s.sessions != nil {
		cached, err := s.sessions.GetAppByID(ctx, appID)
		if err != nil {
			s.log.Warn("load app cache failed", zap.Int64("appid", appID), zap.Error(err))
		} else if cached != nil {
			return cached, nil
		}
	}
	app, err := s.pg.GetAppByID(ctx, appID)
	if err != nil {
		return nil, err
	}
	if app == nil {
		return nil, apperrors.New(40410, http.StatusNotFound, "无法找到该应用")
	}
	if s.sessions != nil {
		if err := s.sessions.SetAppByID(ctx, appID, *app, 5*time.Minute); err != nil {
			s.log.Warn("cache app failed", zap.Int64("appid", appID), zap.Error(err))
		}
	}
	return app, nil
}

func (s *AppService) ResolvePolicy(app *appdomain.App) appdomain.Policy {
	policy := appdomain.Policy{
		MultiDeviceLogin: true,
	}
	if app == nil || app.Settings == nil {
		return policy
	}
	policy.LoginCheckDevice = boolSetting(app.Settings, "loginCheckDevice")
	policy.LoginCheckUser = boolSetting(app.Settings, "loginCheckUser")
	policy.LoginCheckIP = boolSetting(app.Settings, "loginCheckIp")
	policy.LoginCheckDeviceTimeout = intSetting(app.Settings, "loginCheckDeviceTimeOut")
	policy.RegisterCaptcha = boolSetting(app.Settings, "registerCaptcha")
	policy.RegisterCaptchaTimeout = intSetting(app.Settings, "registerCaptchaTimeOut")
	policy.RegisterCheckIP = boolSetting(app.Settings, "registerCheckIp")
	if value, ok := lookupBool(app.Settings, "multiDeviceLogin"); ok {
		policy.MultiDeviceLogin = value
	}
	policy.MultiDeviceLimit = intSetting(app.Settings, "multiDeviceLoginNum")
	if !policy.MultiDeviceLogin {
		policy.MultiDeviceLimit = 1
	}
	return policy
}

func (s *AppService) ResolveTransportEncryption(app *appdomain.App) appdomain.TransportEncryptionPolicy {
	policy := appdomain.TransportEncryptionPolicy{
		Strict:             true,
		ResponseEncryption: true,
	}
	if app == nil || app.Settings == nil {
		return policy
	}

	rawConfig := lookupMap(app.Settings, "transportEncryption")
	if value, ok := lookupNestedBool(rawConfig, "enabled"); ok {
		policy.Enabled = value
	} else if value, ok := lookupBool(app.Settings, "transportEncryptionEnabled"); ok {
		policy.Enabled = value
	}
	if value, ok := lookupNestedBool(rawConfig, "strict"); ok {
		policy.Strict = value
	}
	if value, ok := lookupNestedBool(rawConfig, "responseEncryption"); ok {
		policy.ResponseEncryption = value
	} else if value, ok := lookupNestedBool(rawConfig, "responseEncrypt"); ok {
		policy.ResponseEncryption = value
	}
	if secret, ok := lookupNestedString(rawConfig, "secret"); ok {
		policy.Secret = secret
	} else if secret, ok := lookupNestedString(rawConfig, "key"); ok {
		policy.Secret = secret
	} else if secret, ok := lookupNestedString(rawConfig, "passphrase"); ok {
		policy.Secret = secret
	}
	if strings.TrimSpace(policy.Secret) == "" {
		policy.Secret = strings.TrimSpace(app.AppKey)
	}
	return policy
}

func (s *AppService) PublicSettings(app *appdomain.App) map[string]any {
	if app == nil || app.Settings == nil {
		return map[string]any{}
	}
	settings := appSettingsDeepCloneMap(app.Settings)
	transport := lookupMap(settings, "transportEncryption")
	if len(transport) > 0 {
		delete(transport, "secret")
		delete(transport, "key")
		delete(transport, "passphrase")
		settings["transportEncryption"] = transport
	}
	delete(settings, "transportEncryptionSecret")
	delete(settings, "transportEncryptionKey")
	return settings
}

func (s *AppService) EnsureLoginAllowed(ctx context.Context, appID int64) (*appdomain.App, error) {
	app, err := s.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	if !app.Status {
		message := app.DisabledReason
		if message == "" {
			message = "应用已被禁用"
		}
		return nil, apperrors.New(40310, http.StatusForbidden, message)
	}
	if !app.LoginStatus {
		message := app.DisabledLoginReason
		if message == "" {
			message = "当前应用暂时关闭登录"
		}
		return nil, apperrors.New(40311, http.StatusForbidden, message)
	}
	return app, nil
}

func (s *AppService) EnsureRegisterAllowed(ctx context.Context, appID int64) (*appdomain.App, error) {
	app, err := s.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	if !app.Status {
		message := app.DisabledReason
		if message == "" {
			message = "应用已被禁用"
		}
		return nil, apperrors.New(40310, http.StatusForbidden, message)
	}
	if !app.RegisterStatus {
		message := app.DisabledRegisterReason
		if message == "" {
			message = "当前应用暂时关闭注册"
		}
		return nil, apperrors.New(40312, http.StatusForbidden, message)
	}
	return app, nil
}

func (s *AppService) GetBanners(ctx context.Context, appID int64) ([]appdomain.Banner, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	if s.sessions != nil {
		cached, err := s.sessions.GetBanners(ctx, appID)
		if err != nil {
			s.log.Warn("load banners cache failed", zap.Int64("appid", appID), zap.Error(err))
		} else if cached != nil {
			return cached, nil
		}
	}
	items, err := s.pg.ListActiveBanners(ctx, appID, time.Now().In(s.location))
	if err != nil {
		return nil, err
	}
	if s.sessions != nil {
		if err := s.sessions.SetBanners(ctx, appID, items, 2*time.Minute); err != nil {
			s.log.Warn("cache banners failed", zap.Int64("appid", appID), zap.Error(err))
		}
	}
	return items, nil
}

func (s *AppService) GetNotices(ctx context.Context, appID int64) ([]appdomain.Notice, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	if s.sessions != nil {
		cached, err := s.sessions.GetNotices(ctx, appID)
		if err != nil {
			s.log.Warn("load notices cache failed", zap.Int64("appid", appID), zap.Error(err))
		} else if cached != nil {
			return cached, nil
		}
	}
	items, err := s.pg.ListNotices(ctx, appID)
	if err != nil {
		return nil, err
	}
	if s.sessions != nil {
		if err := s.sessions.SetNotices(ctx, appID, items, 2*time.Minute); err != nil {
			s.log.Warn("cache notices failed", zap.Int64("appid", appID), zap.Error(err))
		}
	}
	return items, nil
}

func (s *AppService) ListApps(ctx context.Context) ([]appdomain.App, error) {
	return s.pg.ListApps(ctx)
}

func (s *AppService) GetStats(ctx context.Context, appID int64) (*appdomain.Stats, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	return s.pg.GetAppStats(ctx, appID)
}

func (s *AppService) GetPolicy(ctx context.Context, appID int64) (*appdomain.Policy, error) {
	app, err := s.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	policy := s.ResolvePolicy(app)
	return &policy, nil
}

func (s *AppService) GetUserTrend(ctx context.Context, appID int64, days int) (*appdomain.UserTrend, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	if days <= 0 {
		days = 7
	}
	if days > 365 {
		days = 365
	}
	return s.pg.GetAppUserTrend(ctx, appID, days)
}

func (s *AppService) GetRegionStats(ctx context.Context, appID int64, query appdomain.RegionStatsQuery) (*appdomain.RegionStatsResult, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	if query.Limit <= 0 {
		query.Limit = 20
	}
	if query.Limit > 100 {
		query.Limit = 100
	}
	if strings.TrimSpace(query.Type) == "" {
		query.Type = "province"
	}
	return s.pg.GetAppRegionStats(ctx, appID, query)
}

func (s *AppService) GetAuthSourceStats(ctx context.Context, appID int64) (*appdomain.AuthSourceStats, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	return s.pg.GetAppAuthSourceStats(ctx, appID)
}

func (s *AppService) ListLoginAudits(ctx context.Context, appID int64, query appdomain.LoginAuditQuery) (*appdomain.LoginAuditListResult, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
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
	items, total, err := s.pg.ListLoginAuditsByApp(ctx, appID, appdomain.LoginAuditQuery{
		Keyword: query.Keyword,
		Status:  query.Status,
		Page:    page,
		Limit:   limit,
	})
	if err != nil {
		return nil, err
	}
	return &appdomain.LoginAuditListResult{
		Items:      items,
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: calcPagesForService(total, limit),
	}, nil
}

func (s *AppService) ExportLoginAudits(ctx context.Context, appID int64, query appdomain.LoginAuditExportQuery) ([]appdomain.LoginAuditItem, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 5000
	}
	if limit > 20000 {
		limit = 20000
	}
	return s.pg.ListLoginAuditsByAppForExport(ctx, appID, appdomain.LoginAuditExportQuery{
		Keyword: query.Keyword,
		Status:  query.Status,
		Limit:   limit,
	})
}

func (s *AppService) ListSessionAudits(ctx context.Context, appID int64, query appdomain.SessionAuditQuery) (*appdomain.SessionAuditListResult, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
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
	items, total, err := s.pg.ListSessionAuditsByApp(ctx, appID, appdomain.SessionAuditQuery{
		Keyword:   query.Keyword,
		EventType: query.EventType,
		Page:      page,
		Limit:     limit,
	})
	if err != nil {
		return nil, err
	}
	return &appdomain.SessionAuditListResult{
		Items:      items,
		Page:       page,
		Limit:      limit,
		Total:      total,
		TotalPages: calcPagesForService(total, limit),
	}, nil
}

func (s *AppService) ExportSessionAudits(ctx context.Context, appID int64, query appdomain.SessionAuditExportQuery) ([]appdomain.SessionAuditItem, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 5000
	}
	if limit > 20000 {
		limit = 20000
	}
	return s.pg.ListSessionAuditsByAppForExport(ctx, appID, appdomain.SessionAuditExportQuery{
		Keyword:   query.Keyword,
		EventType: query.EventType,
		Limit:     limit,
	})
}

func (s *AppService) SaveApp(ctx context.Context, mutation appdomain.AppMutation) (*appdomain.App, error) {
	if mutation.ID <= 0 {
		return nil, apperrors.New(40020, http.StatusBadRequest, "应用标识不能为空")
	}
	current, err := s.pg.GetAppByID(ctx, mutation.ID)
	if err != nil {
		return nil, err
	}

	item := appdomain.App{
		ID:             mutation.ID,
		Status:         true,
		RegisterStatus: true,
		LoginStatus:    true,
		Settings:       map[string]any{},
	}
	if current != nil {
		item = *current
		if item.Settings == nil {
			item.Settings = map[string]any{}
		}
	}

	if mutation.Name != nil {
		item.Name = strings.TrimSpace(*mutation.Name)
	}
	if strings.TrimSpace(item.Name) == "" {
		return nil, apperrors.New(40021, http.StatusBadRequest, "应用名称不能为空")
	}
	if mutation.AppKey != nil {
		item.AppKey = strings.TrimSpace(*mutation.AppKey)
	}
	if mutation.Status != nil {
		item.Status = *mutation.Status
	}
	if mutation.DisabledReason != nil {
		item.DisabledReason = strings.TrimSpace(*mutation.DisabledReason)
	}
	if mutation.RegisterStatus != nil {
		item.RegisterStatus = *mutation.RegisterStatus
	}
	if mutation.DisabledRegisterReason != nil {
		item.DisabledRegisterReason = strings.TrimSpace(*mutation.DisabledRegisterReason)
	}
	if mutation.LoginStatus != nil {
		item.LoginStatus = *mutation.LoginStatus
	}
	if mutation.DisabledLoginReason != nil {
		item.DisabledLoginReason = strings.TrimSpace(*mutation.DisabledLoginReason)
	}
	if mutation.Settings != nil {
		item.Settings = mutation.Settings
	}

	saved, err := s.pg.UpsertApp(ctx, item)
	if err != nil {
		return nil, err
	}
	s.invalidateAppCache(ctx, saved.ID)
	return saved, nil
}

func (s *AppService) UpdatePolicy(ctx context.Context, appID int64, policy appdomain.Policy) (*appdomain.Policy, error) {
	app, err := s.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	settings := cloneSettingsMap(app.Settings)
	settings["loginCheckDevice"] = policy.LoginCheckDevice
	settings["loginCheckUser"] = policy.LoginCheckUser
	settings["loginCheckIp"] = policy.LoginCheckIP
	settings["loginCheckDeviceTimeOut"] = policy.LoginCheckDeviceTimeout
	settings["multiDeviceLogin"] = policy.MultiDeviceLogin
	settings["multiDeviceLoginNum"] = policy.MultiDeviceLimit
	settings["registerCaptcha"] = policy.RegisterCaptcha
	settings["registerCaptchaTimeOut"] = policy.RegisterCaptchaTimeout
	settings["registerCheckIp"] = policy.RegisterCheckIP

	if _, err := s.SaveApp(ctx, appdomain.AppMutation{
		ID:       appID,
		Settings: settings,
	}); err != nil {
		return nil, err
	}
	updated := s.ResolvePolicy(&appdomain.App{Settings: settings})
	return &updated, nil
}

func (s *AppService) ListBannersForAdmin(ctx context.Context, appID int64) ([]appdomain.Banner, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	return s.pg.ListBanners(ctx, appID)
}

func (s *AppService) SaveBanner(ctx context.Context, appID int64, mutation appdomain.BannerMutation) (*appdomain.Banner, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	current, err := s.pg.GetBannerByID(ctx, appID, mutation.ID)
	if err != nil {
		return nil, err
	}
	item := appdomain.Banner{
		ID:       mutation.ID,
		Type:     "url",
		Status:   true,
		Position: 0,
	}
	if current != nil {
		item = *current
	}

	if mutation.Header != nil {
		item.Header = strings.TrimSpace(*mutation.Header)
	}
	if mutation.Title != nil {
		item.Title = strings.TrimSpace(*mutation.Title)
	}
	if strings.TrimSpace(item.Title) == "" {
		return nil, apperrors.New(40022, http.StatusBadRequest, "Banner 标题不能为空")
	}
	if mutation.Content != nil {
		item.Content = strings.TrimSpace(*mutation.Content)
	}
	if mutation.URL != nil {
		item.URL = strings.TrimSpace(*mutation.URL)
	}
	if mutation.Type != nil {
		item.Type = strings.TrimSpace(*mutation.Type)
	}
	if item.Type == "" {
		item.Type = "url"
	}
	if mutation.Position != nil {
		item.Position = *mutation.Position
	}
	if mutation.Status != nil {
		item.Status = *mutation.Status
	}
	if mutation.StartTime != nil {
		item.StartTime = mutation.StartTime
	}
	if mutation.EndTime != nil {
		item.EndTime = mutation.EndTime
	}

	saved, err := s.pg.UpsertBanner(ctx, appID, item)
	if err != nil {
		return nil, err
	}
	s.invalidateBannerCache(ctx, appID)
	return saved, nil
}

func (s *AppService) DeleteBanner(ctx context.Context, appID int64, bannerID int64) error {
	deleted, err := s.pg.DeleteBanner(ctx, appID, bannerID)
	if err != nil {
		return err
	}
	if !deleted {
		return apperrors.New(40411, http.StatusNotFound, "Banner 不存在")
	}
	s.invalidateBannerCache(ctx, appID)
	return nil
}

func (s *AppService) DeleteBanners(ctx context.Context, appID int64, bannerIDs []int64) (int64, []int64, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return 0, nil, err
	}
	ids := normalizeUniqueIDs(bannerIDs)
	if len(ids) == 0 {
		return 0, nil, apperrors.New(40025, http.StatusBadRequest, "Banner 标识不能为空")
	}
	deleted, err := s.pg.DeleteBanners(ctx, appID, ids)
	if err != nil {
		return 0, nil, err
	}
	s.invalidateBannerCache(ctx, appID)
	return deleted, ids, nil
}

func (s *AppService) ListNoticesForAdmin(ctx context.Context, appID int64) ([]appdomain.Notice, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	return s.pg.ListNotices(ctx, appID)
}

func (s *AppService) SaveNotice(ctx context.Context, appID int64, mutation appdomain.NoticeMutation) (*appdomain.Notice, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	current, err := s.pg.GetNoticeByID(ctx, appID, mutation.ID)
	if err != nil {
		return nil, err
	}
	item := appdomain.Notice{ID: mutation.ID}
	if current != nil {
		item = *current
	}

	if mutation.Title != nil {
		item.Title = strings.TrimSpace(*mutation.Title)
	}
	if mutation.Content != nil {
		item.Content = strings.TrimSpace(*mutation.Content)
	}
	if strings.TrimSpace(item.Content) == "" {
		return nil, apperrors.New(40023, http.StatusBadRequest, "公告内容不能为空")
	}

	saved, err := s.pg.UpsertNotice(ctx, appID, item)
	if err != nil {
		return nil, err
	}
	s.invalidateNoticeCache(ctx, appID)
	return saved, nil
}

func (s *AppService) DeleteNotice(ctx context.Context, appID int64, noticeID int64) error {
	deleted, err := s.pg.DeleteNotice(ctx, appID, noticeID)
	if err != nil {
		return err
	}
	if !deleted {
		return apperrors.New(40412, http.StatusNotFound, "公告不存在")
	}
	s.invalidateNoticeCache(ctx, appID)
	return nil
}

func (s *AppService) DeleteNotices(ctx context.Context, appID int64, noticeIDs []int64) (int64, []int64, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return 0, nil, err
	}
	ids := normalizeUniqueIDs(noticeIDs)
	if len(ids) == 0 {
		return 0, nil, apperrors.New(40026, http.StatusBadRequest, "公告标识不能为空")
	}
	deleted, err := s.pg.DeleteNotices(ctx, appID, ids)
	if err != nil {
		return 0, nil, err
	}
	s.invalidateNoticeCache(ctx, appID)
	return deleted, ids, nil
}

func (s *AppService) invalidateAppCache(ctx context.Context, appID int64) {
	if s.sessions == nil {
		return
	}
	if err := s.sessions.DeleteAppByID(ctx, appID); err != nil {
		s.log.Warn("delete app cache failed", zap.Int64("appid", appID), zap.Error(err))
	}
}

func (s *AppService) invalidateBannerCache(ctx context.Context, appID int64) {
	if s.sessions == nil {
		return
	}
	if err := s.sessions.DeleteBanners(ctx, appID); err != nil {
		s.log.Warn("delete banner cache failed", zap.Int64("appid", appID), zap.Error(err))
	}
}

func (s *AppService) invalidateNoticeCache(ctx context.Context, appID int64) {
	if s.sessions == nil {
		return
	}
	if err := s.sessions.DeleteNotices(ctx, appID); err != nil {
		s.log.Warn("delete notice cache failed", zap.Int64("appid", appID), zap.Error(err))
	}
}

func lookupBool(settings map[string]any, key string) (bool, bool) {
	if settings == nil {
		return false, false
	}
	value, ok := settings[key]
	if !ok || value == nil {
		return false, false
	}
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		switch strings.TrimSpace(strings.ToLower(typed)) {
		case "1", "true", "yes", "on":
			return true, true
		case "0", "false", "no", "off":
			return false, true
		}
	case float64:
		return typed != 0, true
	case int:
		return typed != 0, true
	case int64:
		return typed != 0, true
	}
	return false, false
}

func boolSetting(settings map[string]any, key string) bool {
	value, _ := lookupBool(settings, key)
	return value
}

func intSetting(settings map[string]any, key string) int {
	if settings == nil {
		return 0
	}
	value, ok := settings[key]
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
	}
	return 0
}

func calcPagesForService(total int64, limit int) int {
	if limit <= 0 {
		return 1
	}
	pages := int((total + int64(limit) - 1) / int64(limit))
	if pages == 0 {
		return 1
	}
	return pages
}

func cloneSettingsMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func appSettingsDeepCloneMap(input map[string]any) map[string]any {
	if input == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = appSettingsDeepCloneValue(value)
	}
	return result
}

func appSettingsDeepCloneSlice(input []any) []any {
	if input == nil {
		return nil
	}
	result := make([]any, len(input))
	for index, value := range input {
		result[index] = appSettingsDeepCloneValue(value)
	}
	return result
}

func appSettingsDeepCloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return appSettingsDeepCloneMap(typed)
	case []any:
		return appSettingsDeepCloneSlice(typed)
	default:
		return typed
	}
}

func lookupMap(settings map[string]any, key string) map[string]any {
	if settings == nil {
		return nil
	}
	value, ok := settings[key]
	if !ok || value == nil {
		return nil
	}
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case map[string]string:
		result := make(map[string]any, len(typed))
		for nestedKey, nestedValue := range typed {
			result[nestedKey] = nestedValue
		}
		return result
	}
	return nil
}

func lookupNestedBool(settings map[string]any, key string) (bool, bool) {
	if settings == nil {
		return false, false
	}
	value, ok := settings[key]
	if !ok || value == nil {
		return false, false
	}
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		switch strings.TrimSpace(strings.ToLower(typed)) {
		case "1", "true", "yes", "on":
			return true, true
		case "0", "false", "no", "off":
			return false, true
		}
	case float64:
		return typed != 0, true
	case int:
		return typed != 0, true
	case int64:
		return typed != 0, true
	}
	return false, false
}

func lookupNestedString(settings map[string]any, key string) (string, bool) {
	if settings == nil {
		return "", false
	}
	value, ok := settings[key]
	if !ok || value == nil {
		return "", false
	}
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return "", false
		}
		return trimmed, true
	case fmt.Stringer:
		trimmed := strings.TrimSpace(typed.String())
		if trimmed == "" {
			return "", false
		}
		return trimmed, true
	}
	return "", false
}

func normalizeUniqueIDs(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	result := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
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
