package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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

// user_profiles 各列的长度上限，与建表迁移里的列宽一一对应。
//
// 这些字段与签到留痕那一类**不能同等对待**：留痕字段截断后依然可读，而截断过的昵称
// 不是用户填的那个，截断过的邮箱是别人的地址。所以这里的做法是当场拒绝并说清楚
// 哪一项超了、上限是多少 —— 而不是截断，更不是留给数据库报 22001。
//
// 上限按**字符数**判定：Postgres 的 VARCHAR(n) 数的是码点而不是字节，
// 按字节判会让 42 个汉字的昵称被误判为超限。
const (
	profileNicknameMaxLen  = 128
	profileEmailMaxLen     = 255
	profilePhoneMaxLen     = 32
	profileAvatarMaxLen    = 1024
	profileBioMaxLen       = 2000
	profileContactMaxLen   = 128
	profileContactMaxCount = 20
)

// validateProfileUpdate 挡下所有会撞上列宽的输入。
//
// avatar 与 bio 在库里是 TEXT，本来撞不到上限，但同样给一个界：这两个字段由客户端
// 自由填写，不设界就意味着任何人都能往一行里塞进任意大小的内容。
func validateProfileUpdate(input userdomain.ProfileUpdate) error {
	limits := []struct {
		label string
		value string
		limit int
	}{
		{"昵称", input.Nickname, profileNicknameMaxLen},
		{"邮箱", input.Email, profileEmailMaxLen},
		{"手机号", input.Phone, profilePhoneMaxLen},
		{"头像地址", input.Avatar, profileAvatarMaxLen},
		{"个人简介", input.Bio, profileBioMaxLen},
	}
	for _, item := range limits {
		if err := ensureFieldLength(item.label, item.value, item.limit); err != nil {
			return err
		}
	}
	if len(input.Contacts) > profileContactMaxCount {
		return apperrors.New(40000, http.StatusBadRequest,
			fmt.Sprintf("联系方式最多 %d 条", profileContactMaxCount))
	}
	for index, contact := range input.Contacts {
		position := index + 1
		fields := []struct {
			label string
			value string
		}{
			{fmt.Sprintf("第 %d 条联系方式的平台", position), contact.Platform},
			{fmt.Sprintf("第 %d 条联系方式的账号", position), contact.Value},
			{fmt.Sprintf("第 %d 条联系方式的备注", position), contact.Label},
		}
		for _, field := range fields {
			if err := ensureFieldLength(field.label, field.value, profileContactMaxLen); err != nil {
				return err
			}
		}
	}
	return nil
}

func ensureFieldLength(label string, value string, limit int) error {
	if utf8.RuneCountInString(strings.TrimSpace(value)) <= limit {
		return nil
	}
	return apperrors.New(40000, http.StatusBadRequest,
		fmt.Sprintf("%s最多 %d 个字符", label, limit))
}

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
	location  *LocationService
	email     *EmailService
	captcha   *CaptchaService
	plugin    *PluginService
	search    *AdminUserSearchService
	ban       *AccountBanService
	app       *AppService
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

func (s *UserService) SetLocationService(location *LocationService) {
	s.location = location
}

func (s *UserService) SetAdminUserSearchService(search *AdminUserSearchService) {
	s.search = search
}

func (s *UserService) SetAccountBanService(ban *AccountBanService) {
	s.ban = ban
}

// SetAppService 注入应用服务，用于按应用密码策略推导密码生命周期
// （过期时间 / 历史留存条数）。未注入时管理员重置密码不写过期与历史。
func (s *UserService) SetAppService(app *AppService) {
	s.app = app
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

// InvalidateProfileCache 清掉该用户的资料与 /me 缓存。
//
// 头像那条链路只改 user_profiles.avatar 一列（不走整份档案写回），
// 因此需要一个独立入口来失效缓存 —— 少了它，换完头像后的 60 秒内
// 读到的仍是旧引用，用户看到的是「传成功了但没变」。
func (s *UserService) InvalidateProfileCache(ctx context.Context, appID int64, userID int64) {
	if s == nil || s.sessions == nil {
		return
	}
	_ = s.sessions.DeleteMyView(ctx, appID, userID)
	_ = s.sessions.DeleteUserProfile(ctx, appID, userID)
}

func (s *UserService) UpdateProfile(ctx context.Context, session *authdomain.Session, input userdomain.ProfileUpdate) (*userdomain.ProfileUpdateResult, error) {
	// 先校验再落库：超长的输入到了数据库那一层只会得到
	// `value too long for type character varying(N)`(22001)，那句话对用户毫无意义，
	// 也说不出是哪一项超了。
	if err := validateProfileUpdate(input); err != nil {
		return nil, err
	}
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
	// 头像这一项不能照单全收。客户端最常见的写法是「读回整份资料 → 改一个字段
	// → 整份 PUT 回来」，而我们下发的 avatar 是**展示地址**不是**存储引用**。
	// 照原样写进去，就等于亲手把库里唯一那份 storage:// 引用覆盖掉 ——
	// 老版本下发的还是 30 分钟就失效的代理票据地址，覆盖之后头像永久丢失。
	// NormalizeAvatarInput 负责把这类值判回"不修改"，详见 avatar_link.go。
	if strings.TrimSpace(input.Avatar) != "" {
		normalized, err := NormalizeAvatarInput(input.Avatar, profile.Avatar)
		if err != nil {
			return nil, err
		}
		if normalized != "" && normalized != profile.Avatar {
			profile.Avatar = normalized
			profileChanged = true
		}
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
	queuedSensitiveFields := make([]string, 0, 2)
	if v := strings.TrimSpace(input.Email); v != "" && !strings.EqualFold(v, strings.TrimSpace(profile.Email)) {
		if err := s.queueSensitiveProfileChange(ctx, session, profileChangeFieldEmail, v); err != nil {
			s.rollbackPendingProfileChanges(ctx, session, queuedSensitiveFields)
			return nil, err
		}
		queuedSensitiveFields = append(queuedSensitiveFields, profileChangeFieldEmail)
	}
	if v := strings.TrimSpace(input.Phone); v != "" && v != strings.TrimSpace(profile.Phone) {
		if err := s.queueSensitiveProfileChange(ctx, session, profileChangeFieldPhone, v); err != nil {
			s.rollbackPendingProfileChanges(ctx, session, queuedSensitiveFields)
			return nil, err
		}
		queuedSensitiveFields = append(queuedSensitiveFields, profileChangeFieldPhone)
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
	var (
		notificationEmail    string
		notificationOldValue string
		notificationNewValue string
		secondaryEmail       string
	)
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
		notificationOldValue = profile.Email
		notificationNewValue = change.Value
		notificationEmail = change.Value
		secondaryEmail = strings.TrimSpace(profile.Email)
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
		notificationOldValue = profile.Phone
		notificationNewValue = change.Value
		notificationEmail = strings.TrimSpace(profile.Email)
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
	s.notifyProfileChangeCompleted(session.AppID, session.UserID, field, notificationEmail, notificationOldValue, notificationNewValue, secondaryEmail)
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
	if err := s.sessions.SetPendingProfileChange(ctx, session.AppID, session.UserID, change, profileChangeTTL); err != nil {
		return err
	}
	if err := s.dispatchSensitiveProfileChangeCode(ctx, session, change); err != nil {
		if deleteErr := s.sessions.DeletePendingProfileChange(ctx, session.AppID, session.UserID, field); deleteErr != nil {
			s.log.Warn("rollback pending profile change failed", zap.Int64("appid", session.AppID), zap.Int64("userId", session.UserID), zap.String("field", field), zap.Error(deleteErr))
		}
		return err
	}
	return nil
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

func (s *UserService) dispatchSensitiveProfileChangeCode(ctx context.Context, session *authdomain.Session, change userdomain.PendingProfileChange) error {
	switch change.Field {
	case profileChangeFieldEmail:
		if s.email == nil {
			return apperrors.New(50310, http.StatusServiceUnavailable, "邮箱验证服务暂不可用")
		}
		_, err := s.email.SendVerificationCode(ctx, session.AppID, change.Value, change.Purpose, int(profileChangeTTL/time.Minute), "")
		return err
	case profileChangeFieldPhone:
		if s.captcha == nil {
			return apperrors.New(50310, http.StatusServiceUnavailable, "短信验证服务暂不可用")
		}
		providerCfg, err := s.resolveProfileChangeSMSProviderConfig(ctx, session.AppID, captchadomain.Purpose(change.Purpose))
		if err != nil {
			return err
		}
		_, err = s.captcha.SendTrustedSMSCode(ctx, captchadomain.SMSSendRequest{
			AppID:    session.AppID,
			Phone:    change.Value,
			Purpose:  captchadomain.Purpose(change.Purpose),
			ClientIP: strings.TrimSpace(session.IP),
		}, providerCfg)
		return err
	default:
		return apperrors.New(40000, http.StatusBadRequest, "不支持的资料变更字段")
	}
}

func (s *UserService) resolveProfileChangeSMSProviderConfig(ctx context.Context, appID int64, purpose captchadomain.Purpose) (*captchadomain.SMSProviderConfig, error) {
	app, err := s.pg.GetAppByID(ctx, appID)
	if err != nil {
		return nil, err
	}
	if app == nil {
		return nil, apperrors.New(40410, http.StatusNotFound, "无法找到该应用")
	}

	cfg := captchadomain.DefaultCaptchaAppConfig()
	if raw, ok := app.Settings["captcha"]; ok && raw != nil {
		payload, err := json.Marshal(raw)
		if err != nil {
			return nil, apperrors.New(50000, http.StatusInternalServerError, "短信验证配置解析失败")
		}
		if err := json.Unmarshal(payload, &cfg); err != nil {
			return nil, apperrors.New(50000, http.StatusInternalServerError, "短信验证配置解析失败")
		}
	}

	if !cfg.SMSEnabled {
		return nil, apperrors.New(50310, http.StatusServiceUnavailable, "短信验证服务暂不可用")
	}
	providerCfg, err := BuildSMSProviderConfig(appID, purpose, cfg.SMS)
	if err != nil {
		return nil, apperrors.New(50310, http.StatusServiceUnavailable, "短信验证服务尚未配置完整")
	}
	return providerCfg, nil
}

func (s *UserService) rollbackPendingProfileChanges(ctx context.Context, session *authdomain.Session, fields []string) {
	for _, field := range fields {
		if err := s.sessions.DeletePendingProfileChange(ctx, session.AppID, session.UserID, field); err != nil {
			s.log.Warn("rollback pending profile change failed", zap.Int64("appid", session.AppID), zap.Int64("userId", session.UserID), zap.String("field", field), zap.Error(err))
		}
	}
}

func (s *UserService) notifyProfileChangeCompleted(appID int64, userID int64, field string, primaryEmail string, oldValue string, newValue string, secondaryEmail string) {
	if s.email == nil {
		return
	}

	recipients := make([]string, 0, 2)
	if email := strings.TrimSpace(primaryEmail); email != "" {
		recipients = append(recipients, email)
	}
	if email := strings.TrimSpace(secondaryEmail); email != "" {
		duplicate := false
		for _, item := range recipients {
			if strings.EqualFold(item, email) {
				duplicate = true
				break
			}
		}
		if !duplicate {
			recipients = append(recipients, email)
		}
	}
	if len(recipients) == 0 {
		return
	}

	go func(recipients []string) {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		for _, recipient := range recipients {
			if err := s.email.SendProfileChangeCompletedEmail(ctx, appID, recipient, field, oldValue, newValue, ""); err != nil {
				s.log.Warn("send profile change completed email failed",
					zap.Int64("appid", appID),
					zap.Int64("userId", userID),
					zap.String("field", field),
					zap.String("recipient", recipient),
					zap.Error(err),
				)
			}
		}
	}(append([]string(nil), recipients...))
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

// adminUserListMaxLimit 管理端用户列表单页上限。
const adminUserListMaxLimit = 500

func (s *UserService) ListAdminUsers(ctx context.Context, appID int64, query userdomain.AdminUserQuery) (*userdomain.AdminUserListResult, error) {
	page := query.Page
	if page < 1 {
		page = 1
	}
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}
	// 上限 500：控制台列表用虚拟滚动承载大页，200/500 是真实档位。
	// 再高就该用导出而不是列表了（导出另有 20000 的上限）。
	if limit > adminUserListMaxLimit {
		limit = adminUserListMaxLimit
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

func (s *UserService) GetAdminUserDetail(ctx context.Context, appID int64, userID int64) (*userdomain.AdminUserDetail, error) {
	item, err := s.GetAdminUser(ctx, appID, userID)
	if err != nil {
		return nil, err
	}

	profile, err := s.pg.GetUserProfileByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		profile = &userdomain.Profile{
			UserID: userID,
			Extra:  map[string]any{},
		}
	} else if profile.Extra == nil {
		profile.Extra = map[string]any{}
	}
	if item.Extra == nil {
		item.Extra = map[string]any{}
	}
	mergeAdminUserProfileFields(item, profile)
	s.enrichProfileRegisterLocationAsync(appID, userID, profile)

	settings, err := s.GetAdminUserSettings(ctx, appID, userID)
	if err != nil {
		return nil, err
	}

	security, err := s.GetSecurityStatus(ctx, &authdomain.Session{
		AppID:  appID,
		UserID: userID,
	})
	if err != nil {
		return nil, err
	}

	return &userdomain.AdminUserDetail{
		AdminUserView: *item,
		Profile:       profile,
		Settings:      settings,
		Security:      security,
	}, nil
}

func (s *UserService) backfillProfileRegisterIPAsync(appID int64, userID int64, profile *userdomain.Profile, requestIP string) *userdomain.Profile {
	profile, registerIP, shouldPersist := applyRegisterIPFallback(userID, profile, requestIP)
	if !shouldPersist || s.pg == nil {
		return profile
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.pg.PatchUserProfileExtra(ctx, userID, map[string]any{"register_ip": registerIP}); err != nil {
			s.log.Warn("async backfill register ip failed",
				zap.Int64("appid", appID),
				zap.Int64("userId", userID),
				zap.String("registerIp", registerIP),
				zap.Error(err),
			)
			return
		}
		if s.sessions != nil {
			_ = s.sessions.DeleteMyView(ctx, appID, userID)
			_ = s.sessions.DeleteUserProfile(ctx, appID, userID)
		}
		s.log.Debug("async backfilled register ip",
			zap.Int64("appid", appID),
			zap.Int64("userId", userID),
			zap.String("registerIp", registerIP),
		)
	}()
	return profile
}

func applyRegisterIPFallback(userID int64, profile *userdomain.Profile, requestIP string) (*userdomain.Profile, string, bool) {
	requestIP = sanitizeIP(requestIP)
	if profile == nil {
		if requestIP == "" {
			return nil, "", false
		}
		profile = &userdomain.Profile{UserID: userID, Extra: map[string]any{}}
	}
	if profile.Extra == nil {
		profile.Extra = map[string]any{}
	}
	if registerIP := sanitizeIP(profile.RegisterIP); registerIP != "" {
		profile.RegisterIP = registerIP
		if _, exists := profile.Extra["register_ip"]; !exists {
			profile.Extra["register_ip"] = registerIP
		}
		return profile, "", false
	}
	if value, ok := profile.Extra["register_ip"].(string); ok {
		if registerIP := sanitizeIP(value); registerIP != "" {
			profile.RegisterIP = registerIP
			profile.Extra["register_ip"] = registerIP
			return profile, "", false
		}
	}
	if requestIP == "" {
		return profile, "", false
	}
	profile.RegisterIP = requestIP
	profile.Extra["register_ip"] = requestIP
	return profile, requestIP, true
}

func (s *UserService) enrichProfileRegisterLocationAsync(appID int64, userID int64, profile *userdomain.Profile) {
	ip, shouldEnrich := shouldEnrichProfileRegisterLocation(profile)
	if !shouldEnrich || s.location == nil || s.pg == nil {
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		loc := s.location.Resolve(ctx, ip)
		patch := buildRegisterLocationPatch(ip, loc)
		if len(patch) == 0 {
			return
		}
		if err := s.pg.PatchUserProfileExtra(ctx, userID, patch); err != nil {
			s.log.Warn("async enrich register location failed",
				zap.Int64("appid", appID),
				zap.Int64("userId", userID),
				zap.String("registerIp", ip),
				zap.Error(err),
			)
			return
		}
		if s.sessions != nil {
			_ = s.sessions.DeleteMyView(ctx, appID, userID)
			_ = s.sessions.DeleteUserProfile(ctx, appID, userID)
		}
		s.log.Debug("async enriched register location",
			zap.Int64("appid", appID),
			zap.Int64("userId", userID),
			zap.String("registerIp", ip),
			zap.String("province", strings.TrimSpace(loc.Region)),
			zap.String("city", strings.TrimSpace(loc.City)),
			zap.String("isp", strings.TrimSpace(loc.ISP)),
		)
	}()
}

func shouldEnrichProfileRegisterLocation(profile *userdomain.Profile) (string, bool) {
	if profile == nil {
		return "", false
	}
	ip := strings.TrimSpace(profile.RegisterIP)
	if ip == "" && profile.Extra != nil {
		if value, ok := profile.Extra["register_ip"].(string); ok {
			ip = strings.TrimSpace(value)
		}
	}
	if ip == "" {
		return "", false
	}
	if strings.TrimSpace(profile.RegisterProvince) != "" &&
		strings.TrimSpace(profile.RegisterCity) != "" &&
		strings.TrimSpace(profile.RegisterISP) != "" {
		return "", false
	}
	return ip, true
}

func buildRegisterLocationPatch(ip string, loc IPLocation) map[string]any {
	ip = strings.TrimSpace(ip)
	if ip == "" || loc.IsPrivate {
		return nil
	}

	patch := map[string]any{
		"register_ip": ip,
	}
	if value := strings.TrimSpace(loc.Region); value != "" {
		patch["register_province"] = value
	}
	if value := strings.TrimSpace(loc.City); value != "" {
		patch["register_city"] = value
	}
	if value := strings.TrimSpace(loc.ISP); value != "" {
		patch["register_isp"] = value
	}
	if len(patch) == 1 {
		return nil
	}
	return patch
}

func mergeAdminUserProfileFields(item *userdomain.AdminUserView, profile *userdomain.Profile) {
	if item == nil || profile == nil {
		return
	}
	if item.Extra == nil {
		item.Extra = map[string]any{}
	}
	if profile.Extra == nil {
		profile.Extra = map[string]any{}
	}
	for key, value := range item.Extra {
		if _, exists := profile.Extra[key]; !exists {
			profile.Extra[key] = value
		}
	}
	for key, value := range profile.Extra {
		if _, exists := item.Extra[key]; !exists {
			item.Extra[key] = value
		}
	}
	if strings.TrimSpace(item.Nickname) == "" {
		item.Nickname = profile.Nickname
	}
	if strings.TrimSpace(item.Avatar) == "" {
		item.Avatar = profile.Avatar
	}
	if strings.TrimSpace(item.Email) == "" {
		item.Email = profile.Email
	}
	if strings.TrimSpace(item.Phone) == "" {
		item.Phone = profile.Phone
	}
	if strings.TrimSpace(item.InviteCode) == "" {
		item.InviteCode = profile.InviteCode
	}
	if strings.TrimSpace(item.RegisterIP) == "" {
		item.RegisterIP = profile.RegisterIP
	}
	if strings.TrimSpace(item.RegisterIP) == "" {
		if value, ok := profile.Extra["register_ip"].(string); ok {
			item.RegisterIP = strings.TrimSpace(value)
		}
	}
	if item.RegisterTime == nil {
		item.RegisterTime = profile.RegisterTime
	}
	if item.RegisterTime == nil {
		item.RegisterTime = timeFromAny(profile.Extra["register_time"])
	}
	if strings.TrimSpace(item.RegisterProvince) == "" {
		item.RegisterProvince = profile.RegisterProvince
	}
	if strings.TrimSpace(item.RegisterProvince) == "" {
		if value, ok := profile.Extra["register_province"].(string); ok {
			item.RegisterProvince = strings.TrimSpace(value)
		}
	}
	if strings.TrimSpace(item.RegisterCity) == "" {
		item.RegisterCity = profile.RegisterCity
	}
	if strings.TrimSpace(item.RegisterCity) == "" {
		if value, ok := profile.Extra["register_city"].(string); ok {
			item.RegisterCity = strings.TrimSpace(value)
		}
	}
	if strings.TrimSpace(item.RegisterISP) == "" {
		item.RegisterISP = profile.RegisterISP
	}
	if strings.TrimSpace(item.RegisterISP) == "" {
		if value, ok := profile.Extra["register_isp"].(string); ok {
			item.RegisterISP = strings.TrimSpace(value)
		}
	}
	if strings.TrimSpace(item.DisabledReason) == "" {
		item.DisabledReason = profile.DisabledReason
	}
	if strings.TrimSpace(item.MarkCode) == "" {
		item.MarkCode = profile.MarkCode
	}
	if strings.TrimSpace(profile.Nickname) == "" {
		profile.Nickname = item.Nickname
	}
	if strings.TrimSpace(profile.Avatar) == "" {
		profile.Avatar = item.Avatar
	}
	if strings.TrimSpace(profile.Email) == "" {
		profile.Email = item.Email
	}
	if strings.TrimSpace(profile.Phone) == "" {
		profile.Phone = item.Phone
	}
	if strings.TrimSpace(profile.InviteCode) == "" {
		profile.InviteCode = item.InviteCode
	}
	if strings.TrimSpace(profile.RegisterIP) == "" {
		profile.RegisterIP = item.RegisterIP
	}
	if strings.TrimSpace(profile.RegisterIP) == "" {
		if value, ok := item.Extra["register_ip"].(string); ok {
			profile.RegisterIP = strings.TrimSpace(value)
		}
	}
	if profile.RegisterTime == nil {
		profile.RegisterTime = item.RegisterTime
	}
	if profile.RegisterTime == nil {
		profile.RegisterTime = timeFromAny(item.Extra["register_time"])
	}
	if strings.TrimSpace(profile.RegisterProvince) == "" {
		profile.RegisterProvince = item.RegisterProvince
	}
	if strings.TrimSpace(profile.RegisterProvince) == "" {
		if value, ok := item.Extra["register_province"].(string); ok {
			profile.RegisterProvince = strings.TrimSpace(value)
		}
	}
	if strings.TrimSpace(profile.RegisterCity) == "" {
		profile.RegisterCity = item.RegisterCity
	}
	if strings.TrimSpace(profile.RegisterCity) == "" {
		if value, ok := item.Extra["register_city"].(string); ok {
			profile.RegisterCity = strings.TrimSpace(value)
		}
	}
	if strings.TrimSpace(profile.RegisterISP) == "" {
		profile.RegisterISP = item.RegisterISP
	}
	if strings.TrimSpace(profile.RegisterISP) == "" {
		if value, ok := item.Extra["register_isp"].(string); ok {
			profile.RegisterISP = strings.TrimSpace(value)
		}
	}
	if strings.TrimSpace(profile.DisabledReason) == "" {
		profile.DisabledReason = item.DisabledReason
	}
	if strings.TrimSpace(profile.MarkCode) == "" {
		profile.MarkCode = item.MarkCode
	}
}

func timeFromAny(raw any) *time.Time {
	switch value := raw.(type) {
	case nil:
		return nil
	case time.Time:
		v := value.UTC()
		return &v
	case *time.Time:
		if value == nil {
			return nil
		}
		v := value.UTC()
		return &v
	case string:
		text := strings.TrimSpace(value)
		if text == "" {
			return nil
		}
		if parsed, err := time.Parse(time.RFC3339, text); err == nil {
			v := parsed.UTC()
			return &v
		}
		if unix, err := strconv.ParseInt(text, 10, 64); err == nil {
			if unix > 1_000_000_000_000 {
				t := time.UnixMilli(unix).UTC()
				return &t
			}
			t := time.Unix(unix, 0).UTC()
			return &t
		}
	case int64:
		t := time.Unix(value, 0).UTC()
		return &t
	case int:
		t := time.Unix(int64(value), 0).UTC()
		return &t
	case float64:
		unix := int64(value)
		if unix > 1_000_000_000_000 {
			t := time.UnixMilli(unix).UTC()
			return &t
		}
		t := time.Unix(unix, 0).UTC()
		return &t
	}
	return nil
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

	user, _, err := s.loadActiveUser(ctx, session)
	if err != nil {
		return nil, err
	}
	securityState, err := s.pg.GetUserSecurityStateByUserID(ctx, session.UserID)
	if err != nil {
		return nil, err
	}
	providers, err := s.pg.ListOAuthProvidersByUserID(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	totpRecord, err := s.pg.GetUserTOTPSecret(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	passkeyItems, err := s.pg.ListUserPasskeys(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	twoFactorEnabled := totpRecord != nil && totpRecord.Enabled
	twoFactorMethod := ""
	if twoFactorEnabled {
		twoFactorMethod = "totp"
	}
	status := userdomain.SecurityStatus{
		HasPassword:            user.PasswordHash != "",
		TwoFactorEnabled:       twoFactorEnabled,
		TwoFactorMethod:        twoFactorMethod,
		PasskeyEnabled:         len(passkeyItems) > 0,
		PasswordStrengthScore:  securityIntValue(securityState, "password_strength_score"),
		PasswordChangeRequired: securityBoolValue(securityState, "password_change_required"),
		PasswordChangedAt:      securityTimeValue(securityState, "password_changed_at"),
		PasswordExpiresAt:      securityTimeValue(securityState, "password_expires_at"),
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

// AdminListUserLoginAudits 管理端：某个用户的登录审计。
//
//	与 AppService.ListLoginAudits（应用级、按 keyword 模糊搜）的区别是
//	这里按 user_id 精确过滤。控制台的用户详情页需要「这一个人的登录历史」，
//	用应用级接口 + keyword=账号 拼出来的结果既会混进他人记录
//	（keyword 同时匹配 UA / IP / deviceId），分页总数也是错的。
func (s *UserService) AdminListUserLoginAudits(ctx context.Context, appID int64, userID int64, query userdomain.LoginAuditQuery) (*userdomain.LoginAuditListResult, error) {
	page, limit := normalizeAuditPaging(query.Page, query.Limit)
	items, total, err := s.pg.ListLoginAuditsByUser(ctx, appID, userID, userdomain.LoginAuditQuery{
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

// AdminListUserSessionAudits 管理端：某个用户的会话事件审计。
func (s *UserService) AdminListUserSessionAudits(ctx context.Context, appID int64, userID int64, query userdomain.SessionAuditQuery) (*userdomain.SessionAuditListResult, error) {
	page, limit := normalizeAuditPaging(query.Page, query.Limit)
	items, total, err := s.pg.ListSessionAuditsByUser(ctx, appID, userID, userdomain.SessionAuditQuery{
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

func normalizeAuditPaging(page int, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return page, limit
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
	profile = s.backfillProfileRegisterIPAsync(user.AppID, user.ID, profile, session.IP)
	s.enrichProfileRegisterLocationAsync(user.AppID, user.ID, profile)
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
	changedAt := time.Now().UTC()
	// 管理员重置同样受应用密码策略的生命周期约束（过期时间 / 历史留存），
	// 否则「管理员帮用户改一次密码」就绕过了整套策略。
	var lifecycle PasswordLifecycle
	if s.app != nil {
		lifecycle = s.app.ResolvePasswordLifecycle(ctx, appID, changedAt)
	}
	// 旧哈希要在覆盖之前取出来，否则记进历史的是新密码
	previousHash := ""
	if current, err := s.pg.GetUserByID(ctx, userID); err == nil && current != nil {
		previousHash = current.PasswordHash
	}
	if err := s.pg.UpdateUserPassword(ctx, userID, hash, changedAt, lifecycle.ExpiresAt); err != nil {
		return fmt.Errorf("更新密码失败: %w", err)
	}
	if err := s.pg.AppendPasswordHistory(ctx, userID, previousHash, lifecycle.HistoryKeep); err != nil {
		s.log.Warn("密码历史写入失败", zap.Int64("userId", userID), zap.Error(err))
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
	// 显式指定排序时一律走 Postgres。
	//
	// Bleve 索引里只有 user_id / created_at / enabled 三个可排序字段
	//（integral / experience / updated_at / vip_expire_at 根本没进索引），
	// 且 SearchUsers 把排序写死成 `-_score, -created_at, -user_id`。
	// 让排序请求经过它，结果是**参数被静默丢弃**：界面上箭头切了、列表没动。
	// 这比不支持排序糟得多 —— 宁可放弃这一次的搜索加速，也不能让控件说谎。
	if strings.TrimSpace(query.Sort) != "" {
		return false
	}
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
