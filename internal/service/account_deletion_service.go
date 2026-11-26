package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"aegis/internal/domain/accountdeletion"
	authdomain "aegis/internal/domain/auth"
	captchadomain "aegis/internal/domain/captcha"
	notificationdomain "aegis/internal/domain/notification"
	securitydomain "aegis/internal/domain/security"
	userdomain "aegis/internal/domain/user"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
	riskevents "aegis/services/risk/events"
)

// 账号注销。
//
// 一条申请的生命周期（accountdeletion 包的状态常量）：
//
//	用户提交 ──┬─ 需要审核 ──────────────→ reviewing ─┬─ 批准 ─┬─ 冷静期 → scheduled
//	           │                                      │        └─ 立即   → processing
//	           │                                      └─ 驳回 → rejected
//	           ├─ 冷静期 ───────────────→ scheduled ──┬─ 到期（后台）/ 管理员立即执行 → processing
//	           │                                      └─ 本人撤销 / 冷静期内登录（按配置）/ 管理员撤销 → cancelled
//	           └─ 立即注销 ─────────────→ processing ─┬─ 成功 → completed
//	                                                  └─ 失败 → failed（管理员重试或撤销；本人也可以撤销）
//
// 提交前要过两道关：前置条件（EvaluateGuards，block 档挡住）与身份验证（PlanVerification：
// 密码 / 邮箱码 / 短信码任选其一，开了两步验证再加动态码；一样都没有时按配置凭最近登录放行）。
// 刚换绑 72 小时内的联系方式不算数 —— 偷到会话的人绑上自己的邮箱就能注销别人的账号，冷静期挡的就是这条链路。
//
// 执行（execute）是硬删除：吊销全部登录与第三方授权、清空云存储、删 users 行（关联表 CASCADE，
// 订单与退款 SET NULL 留存），按配置写墓碑限制重新注册，最后给绑定邮箱发一封告知。
// 申请行不挂外键，账号没了它还在。

const (
	errCodeDeletionDisabled       = 40371 // 应用未开放注销
	errCodeDeletionVerifyFailed   = 40127 // 密码 / 验证码 / 动态码不对
	errCodeDeletionCodeMissing    = 40128 // 还没发码或码已失效
	errCodeDeletionVerifyRequired = 40129 // 缺少必须的验证，或账号没有可用的验证方式
	errCodeDeletionNotFound       = 40448
	errCodeDeletionOpenExists     = 40915 // 已有进行中的申请
	errCodeDeletionStateChanged   = 40916 // 申请的当前状态不允许这次操作
	errCodeDeletionBlocked        = 40917 // 前置条件没过
	errCodeDeletionReregister     = 40918 // 注销未满限制期，不能重新注册
	errCodeDeletionResendTooSoon  = 42911
	errCodeDeletionTooManyFailure = 42912
	errCodeDeletionExecuteFailed  = 50071

	accountDeletionCodeTTL        = 10 * time.Minute
	accountDeletionResendInterval = 60 * time.Second
	accountDeletionMaxCodeTries   = 5
	accountDeletionSendsPerDay    = 10
	accountDeletionMaxFailures    = 8
	accountDeletionFailureWindow  = time.Hour
	accountDeletionTickInterval   = time.Minute
	accountDeletionBatch          = 20
	accountDeletionStaleAfter     = 30 * time.Minute
	accountDeletionExecuteTimeout = 2 * time.Minute
	// accountDeletionRejectedVisible 驳回的申请在 App 注销页上展示多久
	accountDeletionRejectedVisible = 30 * 24 * time.Hour
)

// accountDeletionMailer 注销用到的发信能力。EmailService 实现它；测试换成记录验证码的假实现。
type accountDeletionMailer interface {
	RecoveryMailReady(ctx context.Context, appID int64) bool
	SendAccountDeletionCode(ctx context.Context, appID int64, email string, code string, expireMinutes int) error
	SendAccountDeletionNotice(ctx context.Context, appID int64, email string, notice AccountDeletionNotice) error
}

// AccountDeletionService 账号注销：配置、提交与验证、审核、冷静期调度、执行、重新注册限制。
type AccountDeletionService struct {
	riskHook
	log        *zap.Logger
	pg         *pgrepo.Repository
	sessions   *redisrepo.SessionRepository
	auth       *AuthService
	users      *UserService
	security   *SecurityService
	mailer     accountDeletionMailer
	captcha    *CaptchaService
	apps       *AppService
	cloud      *CloudStorageService
	notifier   notificationDeliverer
	userEvents UserEventPublisher
	pepper     []byte

	stop     chan struct{}
	stopOnce sync.Once
	now      func() time.Time
}

// NewAccountDeletionService pepper 是墓碑摘要的 HMAC 密钥（平台主密钥），为空时退化为普通 SHA-256。
func NewAccountDeletionService(log *zap.Logger, pg *pgrepo.Repository, sessions *redisrepo.SessionRepository, pepper string) *AccountDeletionService {
	if log == nil {
		log = zap.NewNop()
	}
	return &AccountDeletionService{
		log: log, pg: pg, sessions: sessions, pepper: []byte(pepper),
		stop: make(chan struct{}), now: timeutil.NowUTC,
	}
}

// SetAccountServices 接上账号相关的服务：吊销登录、删用户、两步验证。
func (s *AccountDeletionService) SetAccountServices(auth *AuthService, users *UserService, security *SecurityService) {
	s.auth, s.users, s.security = auth, users, security
}

// SetVerificationChannels 接上发信与短信（以及读应用短信配置的 AppService）。
func (s *AccountDeletionService) SetVerificationChannels(email *EmailService, captcha *CaptchaService, apps *AppService) {
	if email != nil {
		s.mailer = email
	}
	s.captcha, s.apps = captcha, apps
}

// SetCloudStorage 接上云存储：注销时清空用户的云端条目与对象。
func (s *AccountDeletionService) SetCloudStorage(cloud *CloudStorageService) { s.cloud = cloud }

// SetNotifier 接上站内信。
func (s *AccountDeletionService) SetNotifier(notifier notificationDeliverer) { s.notifier = notifier }

// SetEventPublisher 接上实时推送：状态变化与「账号已注销」推给在线设备。
func (s *AccountDeletionService) SetEventPublisher(publisher UserEventPublisher) {
	s.userEvents = publisher
}

// ════════════════════════════════════════════════════════════
//  配置
// ════════════════════════════════════════════════════════════

// AccountDeletionCatalog 控制台配置表单的枚举与默认值。
type AccountDeletionCatalog struct {
	Defaults       accountdeletion.Config `json:"defaults"`
	Modes          []string               `json:"modes"`
	VerifyMethods  []string               `json:"verifyMethods"`
	GuardKeys      []string               `json:"guardKeys"`
	GuardLevels    []string               `json:"guardLevels"`
	LoginBehaviors []string               `json:"loginBehaviors"`
	Statuses       []string               `json:"statuses"`
	Limits         map[string]int         `json:"limits"`
}

// Catalog 枚举与默认值。
func (s *AccountDeletionService) Catalog() AccountDeletionCatalog {
	return AccountDeletionCatalog{
		Defaults:       accountdeletion.DefaultConfig(),
		Modes:          accountdeletion.Modes(),
		VerifyMethods:  accountdeletion.VerifyMethodList(),
		GuardKeys:      accountdeletion.GuardKeys(),
		GuardLevels:    accountdeletion.GuardLevels(),
		LoginBehaviors: accountdeletion.LoginBehaviors(),
		Statuses:       accountdeletion.Statuses(),
		Limits: map[string]int{
			"minCoolingDays": accountdeletion.MinCoolingDays, "maxCoolingDays": accountdeletion.MaxCoolingDays,
			"maxRecentLoginMinutes": accountdeletion.MaxRecentLoginMinutes, "maxRemindBeforeHours": accountdeletion.MaxRemindBeforeHours,
			"maxReregisterDays": accountdeletion.MaxReregisterDays, "maxReasons": accountdeletion.MaxReasons,
			"maxNoticeRunes": accountdeletion.MaxNoticeRunes, "maxReasonTextRunes": accountdeletion.MaxReasonTextRunes,
			"maxReasonCodeRunes": accountdeletion.MaxReasonCodeRunes, "maxReasonLabelRunes": accountdeletion.MaxReasonLabelRunes,
			"maxNoticeTitleRunes": accountdeletion.MaxNoticeTitleRunes, "maxAgreementRunes": accountdeletion.MaxAgreementRunes,
			"maxSupportRunes": accountdeletion.MaxSupportRunes, "maxNoteRunes": accountDeletionNoteMaxRunes,
		},
	}
}

// Settings 应用的注销配置。
func (s *AccountDeletionService) Settings(ctx context.Context, appID int64) (*accountdeletion.Settings, error) {
	return s.pg.GetAccountDeletionSettings(ctx, appID)
}

// SaveSettings 清洗、校验并保存配置。
func (s *AccountDeletionService) SaveSettings(ctx context.Context, appID int64, enabled bool, cfg accountdeletion.Config, operator string) (*accountdeletion.Settings, error) {
	cfg = accountdeletion.NormalizeConfig(cfg)
	if err := accountdeletion.ValidateConfig(cfg); err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	return s.pg.SaveAccountDeletionSettings(ctx, appID, enabled, cfg, operator)
}

// ════════════════════════════════════════════════════════════
//  App：注销页
// ════════════════════════════════════════════════════════════

// accountSubject 判定要用到的账号事实。
type accountSubject struct {
	user         *userdomain.User
	profile      *userdomain.Profile
	email, phone string
	available    accountdeletion.Availability
}

func (s *AccountDeletionService) loadSubject(ctx context.Context, appID, userID int64) (*accountSubject, error) {
	user, err := s.pg.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.AppID != appID {
		return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	profile, err := s.pg.GetUserProfileByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	subject := &accountSubject{user: user, profile: profile}
	if profile != nil {
		subject.email = strings.TrimSpace(profile.Email)
		subject.phone = normalizePhone(profile.Phone)
	}
	// 短信注册的账号手机号就是账号名，档案里未必另存了一份
	if subject.phone == "" && looksLikePhone(user.Account) {
		subject.phone = normalizePhone(user.Account)
	}
	subject.available.Password = user.PasswordHash != ""
	if subject.email != "" && s.mailer != nil && s.mailer.RecoveryMailReady(ctx, appID) && !s.contactCooling(ctx, appID, userID, recoveryFactorEmail) {
		subject.available.Email = maskEmail(subject.email)
	}
	if subject.phone != "" && s.smsUsable(ctx, appID) && !s.contactCooling(ctx, appID, userID, recoveryFactorPhone) {
		subject.available.Phone = maskPhoneValue(subject.phone)
	}
	if record, err := s.pg.GetUserTOTPSecret(ctx, appID, userID); err != nil {
		return nil, err
	} else if record != nil && record.Enabled {
		subject.available.TOTP = true
	}
	return subject, nil
}

// contactCooling 联系方式刚换绑（72 小时内）时不能用来验证注销。
func (s *AccountDeletionService) contactCooling(ctx context.Context, appID, userID int64, field string) bool {
	if s.sessions == nil {
		return false
	}
	remaining, err := s.sessions.ContactChangeCooldown(ctx, appID, userID, field)
	return err == nil && remaining > 0
}

func (s *AccountDeletionService) smsProvider(ctx context.Context, appID int64) (*captchadomain.SMSProviderConfig, error) {
	if s.captcha == nil || !s.captcha.SMSAvailable() || s.apps == nil {
		return nil, apperrors.New(50310, http.StatusServiceUnavailable, "短信验证服务暂不可用")
	}
	cfg, err := s.apps.GetCaptchaConfig(ctx, appID)
	if err != nil {
		return nil, err
	}
	return BuildSMSProviderConfig(appID, captchadomain.PurposeVerifyIdentity, cfg.SMS)
}

func (s *AccountDeletionService) smsUsable(ctx context.Context, appID int64) bool {
	_, err := s.smsProvider(ctx, appID)
	return err == nil
}

// ClientView GET /me/deletion。未开放时只返回 enabled=false；
// 已有进行中的申请时照常返回（入口要显示「冷静期中」，即便管理员事后关掉了注销）。
func (s *AccountDeletionService) ClientView(ctx context.Context, session *authdomain.Session) (*accountdeletion.ClientView, error) {
	settings, err := s.pg.GetAccountDeletionSettings(ctx, session.AppID)
	if err != nil {
		return nil, err
	}
	open, err := s.pg.GetOpenAccountDeletionRequest(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	if !settings.Enabled && open == nil {
		return &accountdeletion.ClientView{Enabled: false}, nil
	}
	cfg := settings.Config
	view := &accountdeletion.ClientView{
		Enabled:           settings.Enabled,
		Modes:             accountdeletion.ModeOptions(cfg),
		DefaultMode:       cfg.DefaultMode,
		CoolingDays:       cfg.CoolingDays,
		RequireReview:     cfg.RequireReview,
		Notice:            accountdeletion.Notice{Title: cfg.NoticeTitle, Content: cfg.NoticeContent, AgreementLabel: cfg.AgreementLabel},
		Reasons:           cfg.Reasons,
		ReasonRequired:    cfg.ReasonRequired,
		AllowCustomReason: cfg.AllowCustomReason,
		ReregisterDays:    cfg.ReregisterDays,
		LoginCancels:      cfg.LoginDuringCooling == accountdeletion.LoginCancel,
		SupportContact:    cfg.SupportContact,
		Checks:            []accountdeletion.Check{},
		Verification:      accountdeletion.Verification{Methods: []accountdeletion.VerifyOption{}},
	}
	if open != nil {
		view.Request = accountdeletion.ToClient(open)
		return view, nil
	}
	if latest, err := s.pg.GetLatestAccountDeletionRequest(ctx, session.AppID, session.UserID); err != nil {
		return nil, err
	} else if latest != nil && latest.Status == accountdeletion.StatusRejected && latest.ReviewedAt != nil &&
		s.now().Sub(*latest.ReviewedAt) < accountDeletionRejectedVisible {
		// 驳回的结论留在页面上一段时间，用户能看到理由；不影响重新提交
		view.Request = accountdeletion.ToClient(latest)
	}
	subject, err := s.loadSubject(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	facts, err := s.pg.AccountDeletionFacts(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	view.Checks = accountdeletion.EvaluateGuards(cfg, *facts, s.now())
	view.Blocked = accountdeletion.FirstBlocking(view.Checks) != nil
	view.Verification = accountdeletion.PlanVerification(cfg, subject.available)
	return view, nil
}

// ── 发验证码 ──

// AccountDeletionCodeResult POST /me/deletion/code 的结果。
type AccountDeletionCodeResult struct {
	Channel   string    `json:"channel"`
	Masked    string    `json:"masked"`
	ExpiresAt time.Time `json:"expiresAt"`
	ResendAt  time.Time `json:"resendAt"`
}

// SendCode 向绑定的邮箱或手机号发注销验证码。只能发给 PlanVerification 列出的渠道。
func (s *AccountDeletionService) SendCode(ctx context.Context, session *authdomain.Session, channel, ip string) (*AccountDeletionCodeResult, error) {
	channel = strings.ToLower(strings.TrimSpace(channel))
	if channel != accountdeletion.VerifyEmail && channel != accountdeletion.VerifyPhone {
		return nil, apperrors.New(40000, http.StatusBadRequest, "channel 只能是 email 或 phone")
	}
	settings, err := s.pg.GetAccountDeletionSettings(ctx, session.AppID)
	if err != nil {
		return nil, err
	}
	if !settings.Enabled {
		return nil, apperrors.New(errCodeDeletionDisabled, http.StatusForbidden, "账号注销暂未开放")
	}
	subject, err := s.loadSubject(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	plan := accountdeletion.PlanVerification(settings.Config, subject.available)
	var masked string
	for _, method := range plan.Methods {
		if method.Type == channel {
			masked = method.Masked
		}
	}
	if masked == "" {
		return nil, apperrors.New(errCodeDeletionVerifyRequired, http.StatusBadRequest, "这种验证方式当前不可用")
	}
	if err := s.ensureNotLocked(ctx, session.AppID, session.UserID); err != nil {
		return nil, err
	}
	now := s.now()
	if previous, err := s.sessions.GetAccountDeletionCode(ctx, session.AppID, session.UserID, channel); err != nil {
		return nil, err
	} else if previous != nil && now.Sub(previous.SentAt) < accountDeletionResendInterval {
		wait := accountDeletionResendInterval - now.Sub(previous.SentAt)
		return nil, apperrors.New(errCodeDeletionResendTooSoon, http.StatusTooManyRequests,
			fmt.Sprintf("发送过于频繁，请 %d 秒后再试", int(wait.Seconds())+1))
	}
	if n, err := s.sessions.HitAccountDeletionCounter(ctx, "send", session.AppID, session.UserID, 24*time.Hour); err == nil && n > accountDeletionSendsPerDay {
		return nil, apperrors.New(errCodeDeletionResendTooSoon, http.StatusTooManyRequests, "今日验证码发送次数已达上限，请明天再试")
	}

	state := redisrepo.AccountDeletionCode{Channel: channel, SentAt: now, ExpiresAt: now.Add(accountDeletionCodeTTL)}
	switch channel {
	case accountdeletion.VerifyEmail:
		code := generateEmailCode()
		state.CodeHash = s.codeDigest(session.AppID, session.UserID, code)
		if err := s.sessions.SaveAccountDeletionCode(ctx, session.AppID, session.UserID, state, accountDeletionCodeTTL); err != nil {
			return nil, err
		}
		if err := s.mailer.SendAccountDeletionCode(ctx, session.AppID, subject.email, code, int(accountDeletionCodeTTL/time.Minute)); err != nil {
			_ = s.sessions.DeleteAccountDeletionCode(ctx, session.AppID, session.UserID, channel)
			return nil, err
		}
	case accountdeletion.VerifyPhone:
		provider, err := s.smsProvider(ctx, session.AppID)
		if err != nil {
			return nil, err
		}
		if _, err := s.captcha.SendTrustedSMSCode(ctx, captchadomain.SMSSendRequest{
			AppID: session.AppID, Phone: subject.phone, Purpose: captchadomain.PurposeVerifyIdentity, ClientIP: ip,
		}, provider); err != nil {
			return nil, err
		}
		// 短信码由 CaptchaService 保管，这里只记「发过了、什么时候发的」
		if err := s.sessions.SaveAccountDeletionCode(ctx, session.AppID, session.UserID, state, accountDeletionCodeTTL); err != nil {
			return nil, err
		}
	}
	return &AccountDeletionCodeResult{
		Channel: channel, Masked: masked, ExpiresAt: state.ExpiresAt, ResendAt: now.Add(accountDeletionResendInterval),
	}, nil
}

func (s *AccountDeletionService) codeDigest(appID, userID int64, code string) string {
	mac := hmac.New(sha256.New, s.pepper)
	_, _ = fmt.Fprintf(mac, "deletion-code:%d:%d:%s", appID, userID, strings.TrimSpace(code))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *AccountDeletionService) ensureNotLocked(ctx context.Context, appID, userID int64) error {
	n, ttl, err := s.sessions.PeekAccountDeletionCounter(ctx, "fail", appID, userID)
	if err != nil || n < accountDeletionMaxFailures {
		return nil
	}
	minutes := int(ttl.Minutes()) + 1
	return apperrors.New(errCodeDeletionTooManyFailure, http.StatusTooManyRequests,
		fmt.Sprintf("验证失败次数过多，请 %d 分钟后再试", minutes))
}

func (s *AccountDeletionService) recordFailure(ctx context.Context, appID, userID int64, cause error) error {
	n, err := s.sessions.HitAccountDeletionCounter(ctx, "fail", appID, userID, accountDeletionFailureWindow)
	if err == nil && n >= accountDeletionMaxFailures {
		return apperrors.New(errCodeDeletionTooManyFailure, http.StatusTooManyRequests, "验证失败次数过多，请 1 小时后再试")
	}
	return cause
}

// ── 提交 ──

// AccountDeletionSubmitInput POST /me/deletion。
type AccountDeletionSubmitInput struct {
	Session      *authdomain.Session
	Mode         string
	ReasonCode   string
	ReasonText   string
	Agree        bool
	Password     string
	Channel      string
	Code         string
	TOTPCode     string
	RecoveryCode string
	IP           string
	DeviceID     string
	Device       string
	UserAgent    string
}

// Submit 提交注销申请：先过风控（注销是账号被盗后的常见收尾），成功后上报
func (s *AccountDeletionService) Submit(ctx context.Context, input AccountDeletionSubmitInput) (*accountdeletion.SubmitResult, error) {
	if sess := input.Session; sess != nil {
		if err := s.riskGuard(ctx, securitydomain.RiskEvalRequest{Scene: securitydomain.SceneAccountDelete, AppID: &sess.AppID, UserID: &sess.UserID,
			Account: sess.Account, Extra: map[string]any{"channel": input.Channel, "mode": input.Mode}},
			RiskGuardOption{Message: "当前环境存在风险，暂时无法提交注销申请"}); err != nil {
			return nil, err
		}
	}
	result, err := s.submit(ctx, input)
	if err == nil && input.Session != nil {
		s.riskReport(ctx, riskevents.Event{Type: riskevents.TypeAccountDelete, Scene: securitydomain.SceneAccountDelete, AppID: input.Session.AppID,
			UserID: input.Session.UserID, Account: input.Session.Account, Props: map[string]any{"mode": input.Mode, "reason": input.ReasonCode}})
	}
	return result, err
}

// submit 提交注销申请：校验 → 前置条件 → 身份验证 → 落库 →（立即注销时）执行。
func (s *AccountDeletionService) submit(ctx context.Context, input AccountDeletionSubmitInput) (*accountdeletion.SubmitResult, error) {
	session := input.Session
	settings, err := s.pg.GetAccountDeletionSettings(ctx, session.AppID)
	if err != nil {
		return nil, err
	}
	if !settings.Enabled {
		return nil, apperrors.New(errCodeDeletionDisabled, http.StatusForbidden, "账号注销暂未开放")
	}
	cfg := settings.Config
	if open, err := s.pg.GetOpenAccountDeletionRequest(ctx, session.AppID, session.UserID); err != nil {
		return nil, err
	} else if open != nil {
		return nil, apperrors.New(errCodeDeletionOpenExists, http.StatusConflict, "已有进行中的注销申请")
	}
	mode := strings.TrimSpace(input.Mode)
	if mode == "" {
		mode = cfg.DefaultMode
	}
	if !accountdeletion.ModeAllowed(cfg, mode) {
		return nil, apperrors.New(40000, http.StatusBadRequest, "不支持这种注销方式")
	}
	reasonCode, reasonText, err := accountdeletion.ResolveReason(cfg, input.ReasonCode, input.ReasonText)
	if err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	if !input.Agree {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请先阅读并同意注销须知")
	}
	subject, err := s.loadSubject(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	facts, err := s.pg.AccountDeletionFacts(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	checks := accountdeletion.EvaluateGuards(cfg, *facts, s.now())
	if blocking := accountdeletion.FirstBlocking(checks); blocking != nil {
		return nil, apperrors.New(errCodeDeletionBlocked, http.StatusConflict, blocking.Message)
	}
	verifiedBy, err := s.verify(ctx, cfg, subject, input)
	if err != nil {
		return nil, err
	}

	now := s.now()
	status := accountdeletion.InitialStatus(mode, cfg.RequireReview)
	record := accountdeletion.RequestInput{
		AppID: session.AppID, UserID: session.UserID, Account: subject.user.Account, Nickname: subjectNickname(subject),
		Mode: mode, Status: status, Source: accountdeletion.SourceUser, ReasonCode: reasonCode, ReasonText: reasonText,
		VerifiedBy: verifiedBy, IP: input.IP, DeviceID: input.DeviceID, Device: input.Device, UserAgent: input.UserAgent,
		Snapshot: s.snapshot(subject, facts, checks),
	}
	if mode == accountdeletion.ModeCooling {
		record.CoolingDays = cfg.CoolingDays
	}
	if status == accountdeletion.StatusScheduled {
		due, remind := accountdeletion.Schedule(now, cfg.CoolingDays, cfg.RemindBeforeHours)
		record.ScheduledAt, record.RemindAt = &due, remind
	}
	req, err := s.pg.CreateAccountDeletionRequest(ctx, record)
	if err != nil {
		return nil, mapDeletionStoreError(err)
	}
	s.log.Info("账号注销申请已提交", zap.Int64("appid", req.AppID), zap.Int64("userId", req.UserID),
		zap.Int64("requestId", req.ID), zap.String("mode", mode), zap.String("status", status))

	result := &accountdeletion.SubmitResult{}
	if status == accountdeletion.StatusProcessing {
		done, execErr := s.execute(ctx, req, cfg)
		if execErr != nil {
			return nil, apperrors.New(errCodeDeletionExecuteFailed, http.StatusInternalServerError, "注销执行失败，已转交管理员处理")
		}
		result.Request = accountdeletion.ToClient(done)
		result.Deleted = done.Status == accountdeletion.StatusCompleted
		return result, nil
	}

	if cfg.SignOutOthers && s.auth != nil {
		result.SignedOut = s.auth.revokeUserCredentials(ctx, session.AppID, session.UserID, session.TokenID, session.RefreshFamilyID, "account_deletion")
	}
	result.Request = accountdeletion.ToClient(req)
	kind := AccountDeletionNoticeScheduled
	if status == accountdeletion.StatusReviewing {
		kind = AccountDeletionNoticeSubmitted
	}
	s.announce(req, cfg, kind, subject.email, "")
	return result, nil
}

// verify 按计划核验提交里带的凭据，返回通过的方式。失败计入锁定计数；全部通过后清零。
func (s *AccountDeletionService) verify(ctx context.Context, cfg accountdeletion.Config, subject *accountSubject, input AccountDeletionSubmitInput) ([]string, error) {
	session := input.Session
	appID, userID := session.AppID, session.UserID
	plan := accountdeletion.PlanVerification(cfg, subject.available)
	if !plan.Available {
		return nil, apperrors.New(errCodeDeletionVerifyRequired, http.StatusBadRequest, plan.Message)
	}
	if err := s.ensureNotLocked(ctx, appID, userID); err != nil {
		return nil, err
	}
	verified := make([]string, 0, 2)
	offered := func(kind string) bool {
		for _, method := range plan.Methods {
			if method.Type == kind {
				return true
			}
		}
		return false
	}

	switch {
	case len(plan.Methods) > 0:
		channel := strings.ToLower(strings.TrimSpace(input.Channel))
		switch {
		case input.Password != "" && offered(accountdeletion.VerifyPassword):
			if !verifyPassword(subject.user.PasswordHash, input.Password) {
				return nil, s.recordFailure(ctx, appID, userID,
					apperrors.New(errCodeDeletionVerifyFailed, http.StatusBadRequest, "登录密码不正确"))
			}
			verified = append(verified, accountdeletion.VerifyPassword)
		case strings.TrimSpace(input.Code) != "" && (channel == accountdeletion.VerifyEmail || channel == accountdeletion.VerifyPhone) && offered(channel):
			if err := s.verifyContactCode(ctx, subject, appID, userID, channel, input.Code); err != nil {
				return nil, err
			}
			verified = append(verified, channel)
		default:
			return nil, apperrors.New(errCodeDeletionVerifyRequired, http.StatusBadRequest, "请完成身份验证")
		}
	case plan.Session:
		if session.IssuedAt.IsZero() || s.now().Sub(session.IssuedAt) > time.Duration(cfg.RecentLoginMinutes)*time.Minute {
			return nil, apperrors.New(errCodeDeletionVerifyRequired, http.StatusBadRequest,
				fmt.Sprintf("为确认是本人操作，请重新登录后在 %d 分钟内完成注销", cfg.RecentLoginMinutes))
		}
		verified = append(verified, accountdeletion.VerifySession)
	}

	if plan.TOTP {
		code, recovery := strings.TrimSpace(input.TOTPCode), strings.TrimSpace(input.RecoveryCode)
		if code == "" && recovery == "" {
			return nil, apperrors.New(errCodeDeletionVerifyRequired, http.StatusBadRequest, "请输入身份验证器动态码或恢复码")
		}
		if s.security == nil {
			return nil, apperrors.New(50321, http.StatusServiceUnavailable, "两步验证服务暂不可用")
		}
		if err := s.security.verifyUserSecondFactor(ctx, appID, userID, code, recovery); err != nil {
			if countsAsRecoveryFailure(err) {
				return nil, s.recordFailure(ctx, appID, userID,
					apperrors.New(errCodeDeletionVerifyFailed, http.StatusBadRequest, "动态码或恢复码不正确"))
			}
			return nil, err
		}
		if code != "" {
			verified = append(verified, accountdeletion.VerifyTOTP)
		} else {
			verified = append(verified, accountdeletion.VerifyRecovery)
		}
	}
	_ = s.sessions.ResetAccountDeletionCounter(ctx, "fail", appID, userID)
	return verified, nil
}

func (s *AccountDeletionService) verifyContactCode(ctx context.Context, subject *accountSubject, appID, userID int64, channel, code string) error {
	state, err := s.sessions.GetAccountDeletionCode(ctx, appID, userID, channel)
	if err != nil {
		return err
	}
	if state == nil || s.now().After(state.ExpiresAt) {
		return apperrors.New(errCodeDeletionCodeMissing, http.StatusBadRequest, "验证码已失效，请重新获取")
	}
	switch channel {
	case accountdeletion.VerifyEmail:
		if subtle.ConstantTimeCompare([]byte(state.CodeHash), []byte(s.codeDigest(appID, userID, code))) != 1 {
			state.Attempts++
			if state.Attempts >= accountDeletionMaxCodeTries {
				_ = s.sessions.DeleteAccountDeletionCode(ctx, appID, userID, channel)
			} else if ttl := state.ExpiresAt.Sub(s.now()); ttl > 0 {
				_ = s.sessions.SaveAccountDeletionCode(ctx, appID, userID, *state, ttl)
			}
			return s.recordFailure(ctx, appID, userID,
				apperrors.New(errCodeDeletionVerifyFailed, http.StatusBadRequest, "邮箱验证码错误"))
		}
	case accountdeletion.VerifyPhone:
		if s.captcha == nil {
			return apperrors.New(50310, http.StatusServiceUnavailable, "短信验证服务暂不可用")
		}
		valid, err := s.captcha.VerifySMSCode(ctx, captchadomain.SMSVerifyRequest{
			AppID: appID, Phone: subject.phone, Code: strings.TrimSpace(code), Purpose: captchadomain.PurposeVerifyIdentity,
		})
		if err != nil {
			if countsAsRecoveryFailure(err) {
				return s.recordFailure(ctx, appID, userID, err)
			}
			return err
		}
		if !valid {
			return s.recordFailure(ctx, appID, userID,
				apperrors.New(errCodeDeletionVerifyFailed, http.StatusBadRequest, "短信验证码错误"))
		}
	}
	_ = s.sessions.DeleteAccountDeletionCode(ctx, appID, userID, channel)
	return nil
}

func subjectNickname(subject *accountSubject) string {
	if subject.profile != nil {
		return strings.TrimSpace(subject.profile.Nickname)
	}
	return ""
}

// snapshot 提交那一刻的账号概况：只用于控制台展示，联系方式打码。
func (s *AccountDeletionService) snapshot(subject *accountSubject, facts *accountdeletion.Facts, checks []accountdeletion.Check) map[string]any {
	snap := map[string]any{
		"registeredAt": subject.user.CreatedAt,
		"points":       facts.Points,
		"wallet":       facts.WalletBalance,
		"lifetime":     facts.Lifetime,
		"hasPassword":  subject.user.PasswordHash != "",
		"totp":         subject.available.TOTP,
		"checks":       checks,
	}
	if facts.VipExpireAt != nil {
		snap["vipExpireAt"] = facts.VipExpireAt
	}
	if subject.email != "" {
		snap["email"] = maskEmail(subject.email)
	}
	if subject.phone != "" {
		snap["phone"] = maskPhoneValue(subject.phone)
	}
	return snap
}

// ── 撤销 ──

// CancelByUser 本人撤销进行中的申请（待审核、冷静期中、执行失败）。
func (s *AccountDeletionService) CancelByUser(ctx context.Context, session *authdomain.Session) (*accountdeletion.ClientRequest, error) {
	open, err := s.pg.GetOpenAccountDeletionRequest(ctx, session.AppID, session.UserID)
	if err != nil {
		return nil, err
	}
	if open == nil {
		return nil, apperrors.New(errCodeDeletionNotFound, http.StatusNotFound, "没有进行中的注销申请")
	}
	if !accountdeletion.UserCanCancel(open.Status) {
		return nil, apperrors.New(errCodeDeletionStateChanged, http.StatusConflict, "注销正在执行，无法撤销")
	}
	req, err := s.pg.TransitionAccountDeletionRequest(ctx, session.AppID, open.ID, pgrepo.DeletionTransition{
		From: []string{accountdeletion.StatusReviewing, accountdeletion.StatusScheduled, accountdeletion.StatusFailed},
		To:   accountdeletion.StatusCancelled,
		Set:  map[string]any{"cancelled_at": s.now(), "cancelled_by": accountdeletion.CancelledByUser},
	})
	if err != nil {
		return nil, mapDeletionStoreError(err)
	}
	s.afterCancel(ctx, req, "")
	return accountdeletion.ToClient(req), nil
}

func (s *AccountDeletionService) afterCancel(ctx context.Context, req *accountdeletion.Request, note string) {
	settings, err := s.pg.GetAccountDeletionSettings(ctx, req.AppID)
	if err != nil {
		s.log.Warn("load deletion settings failed", zap.Error(err))
		return
	}
	s.announce(req, settings.Config, AccountDeletionNoticeCancelled, "", note)
}

// ── 登录 ──

// OnLogin 登录成功后调用：冷静期 / 待审核中的账号在登录结果里带上注销状态；
// 应用配置为「登录即撤销」时，冷静期中的申请在这里撤销。
func (s *AccountDeletionService) OnLogin(ctx context.Context, user *userdomain.User) *accountdeletion.LoginNotice {
	if user == nil {
		return nil
	}
	open, err := s.pg.GetOpenAccountDeletionRequest(ctx, user.AppID, user.ID)
	if err != nil || open == nil {
		return nil
	}
	notice := &accountdeletion.LoginNotice{Status: open.Status, Mode: open.Mode, ScheduledAt: open.ScheduledAt}
	if open.Status != accountdeletion.StatusScheduled {
		return notice
	}
	settings, err := s.pg.GetAccountDeletionSettings(ctx, user.AppID)
	if err != nil || settings.Config.LoginDuringCooling != accountdeletion.LoginCancel {
		return notice
	}
	req, err := s.pg.TransitionAccountDeletionRequest(ctx, user.AppID, open.ID, pgrepo.DeletionTransition{
		From: []string{accountdeletion.StatusScheduled},
		To:   accountdeletion.StatusCancelled,
		Set: map[string]any{"cancelled_at": s.now(), "cancelled_by": accountdeletion.CancelledByLogin,
			"cancel_reason": "冷静期内登录，注销自动撤销"},
	})
	if err != nil {
		return notice
	}
	s.announce(req, settings.Config, AccountDeletionNoticeCancelled, "", "冷静期内登录，注销已自动撤销")
	return &accountdeletion.LoginNotice{Status: req.Status, Mode: req.Mode, ScheduledAt: open.ScheduledAt, Cancelled: true}
}

// ── 重新注册限制 ──

// RegisterIdentifier 注册时要核对的一个标识。
type RegisterIdentifier struct {
	Kind  string
	Value string
}

// CheckReregister 注册前核对：这些标识里有没有刚注销、还在限制期内的。
// 查询出错时放行（不让墓碑表的故障挡住所有注册）。
func (s *AccountDeletionService) CheckReregister(ctx context.Context, appID int64, identifiers ...RegisterIdentifier) error {
	digests := make([]string, 0, len(identifiers))
	for _, item := range identifiers {
		if strings.TrimSpace(item.Value) == "" {
			continue
		}
		digests = append(digests, s.identifierDigest(appID, item.Kind, item.Value))
	}
	tomb, err := s.pg.FindAccountDeletionTombstone(ctx, appID, digests)
	if err != nil {
		s.log.Warn("check deletion tombstone failed", zap.Int64("appid", appID), zap.Error(err))
		return nil
	}
	if tomb == nil {
		return nil
	}
	noun := map[string]string{accountdeletion.TombstoneAccount: "账号", accountdeletion.TombstoneEmail: "邮箱", accountdeletion.TombstonePhone: "手机号"}[tomb.Kind]
	return apperrors.New(errCodeDeletionReregister, http.StatusConflict,
		fmt.Sprintf("该%s已注销，%s 之后才能重新注册", noun, tomb.ExpiresAt.In(timeutil.DefaultLocation()).Format("2006-01-02")))
}

func (s *AccountDeletionService) identifierDigest(appID int64, kind, value string) string {
	normalized := accountdeletion.NormalizeIdentifier(kind, value)
	if len(s.pepper) == 0 {
		sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s:%s", appID, kind, normalized)))
		return hex.EncodeToString(sum[:])
	}
	mac := hmac.New(sha256.New, s.pepper)
	_, _ = fmt.Fprintf(mac, "deletion-tombstone:%d:%s:%s", appID, kind, normalized)
	return hex.EncodeToString(mac.Sum(nil))
}

// ════════════════════════════════════════════════════════════
//  执行
// ════════════════════════════════════════════════════════════

// execute 删除账号。调用前申请必须已经是 processing（认领过）。成功返回 completed 的申请；
// 失败时申请转为 failed 并返回错误。账号已经不在时直接记为完成。
func (s *AccountDeletionService) execute(ctx context.Context, req *accountdeletion.Request, cfg accountdeletion.Config) (*accountdeletion.Request, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), accountDeletionExecuteTimeout)
	defer cancel()
	appID, userID := req.AppID, req.UserID

	user, err := s.pg.GetUserByID(ctx, userID)
	if err != nil {
		return s.fail(ctx, req, err)
	}
	var email, phone string
	if user != nil && user.AppID == appID {
		if profile, err := s.pg.GetUserProfileByUserID(ctx, userID); err == nil && profile != nil {
			email = strings.TrimSpace(profile.Email)
			phone = normalizePhone(profile.Phone)
		}
		if phone == "" && looksLikePhone(user.Account) {
			phone = normalizePhone(user.Account)
		}
		// 在线设备先收到「账号已注销」，再断开
		if s.userEvents != nil {
			_ = s.userEvents.PublishUserEvent(ctx, appID, userID, "account.deleted", map[string]any{"requestId": req.ID})
		}
		if s.auth != nil {
			s.auth.revokeUserCredentials(ctx, appID, userID, "", "", "account_deletion")
		}
		if s.cloud != nil {
			if _, err := s.cloud.AdminPurgeUser(ctx, appID, userID); err != nil {
				s.log.Warn("注销时清空云存储失败", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
			}
		}
		if s.users == nil {
			return s.fail(ctx, req, errors.New("user service not wired"))
		}
		if err := s.users.PurgeUser(ctx, appID, userID); err != nil {
			return s.fail(ctx, req, err)
		}
	}

	now := s.now()
	done, err := s.pg.TransitionAccountDeletionRequest(ctx, appID, req.ID, pgrepo.DeletionTransition{
		From: []string{accountdeletion.StatusProcessing},
		To:   accountdeletion.StatusCompleted,
		Set:  map[string]any{"completed_at": now, "failure": ""},
	})
	if err != nil {
		return nil, err
	}
	if cfg.ReregisterDays > 0 && user != nil {
		expires := now.Add(time.Duration(cfg.ReregisterDays) * 24 * time.Hour)
		tombs := []accountdeletion.TombstoneInput{{
			Kind: accountdeletion.TombstoneAccount, Digest: s.identifierDigest(appID, accountdeletion.TombstoneAccount, user.Account),
			Hint: maskTombstoneAccount(user.Account), ExpiresAt: expires,
		}}
		if email != "" {
			tombs = append(tombs, accountdeletion.TombstoneInput{Kind: accountdeletion.TombstoneEmail,
				Digest: s.identifierDigest(appID, accountdeletion.TombstoneEmail, email), Hint: maskEmail(email), ExpiresAt: expires})
		}
		if phone != "" {
			tombs = append(tombs, accountdeletion.TombstoneInput{Kind: accountdeletion.TombstonePhone,
				Digest: s.identifierDigest(appID, accountdeletion.TombstonePhone, phone), Hint: maskPhoneValue(phone), ExpiresAt: expires})
		}
		if err := s.pg.CreateAccountDeletionTombstones(ctx, appID, req.ID, tombs); err != nil {
			s.log.Warn("写入注销墓碑失败", zap.Int64("requestId", req.ID), zap.Error(err))
		}
	}
	if s.sessions != nil {
		_ = s.sessions.DeleteAccountDeletionCode(ctx, appID, userID, accountdeletion.VerifyEmail)
		_ = s.sessions.DeleteAccountDeletionCode(ctx, appID, userID, accountdeletion.VerifyPhone)
	}
	s.log.Warn("账号已注销", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Int64("requestId", req.ID),
		zap.String("mode", req.Mode), zap.String("source", req.Source))
	if cfg.NotifyEmail && email != "" {
		s.sendNotice(appID, email, AccountDeletionNotice{
			Kind: AccountDeletionNoticeCompleted, Account: done.Account, At: now,
			Reason: accountdeletion.ReasonLabel(cfg, done.ReasonCode),
		})
	}
	return done, nil
}

func (s *AccountDeletionService) fail(ctx context.Context, req *accountdeletion.Request, cause error) (*accountdeletion.Request, error) {
	s.log.Error("账号注销执行失败", zap.Int64("appid", req.AppID), zap.Int64("userId", req.UserID),
		zap.Int64("requestId", req.ID), zap.Error(cause))
	message := cause.Error()
	if len([]rune(message)) > 500 {
		message = string([]rune(message)[:500])
	}
	if _, err := s.pg.TransitionAccountDeletionRequest(ctx, req.AppID, req.ID, pgrepo.DeletionTransition{
		From: []string{accountdeletion.StatusProcessing},
		To:   accountdeletion.StatusFailed,
		Set:  map[string]any{"failure": message},
	}); err != nil {
		s.log.Warn("mark deletion failed", zap.Int64("requestId", req.ID), zap.Error(err))
	}
	return nil, cause
}

func maskTombstoneAccount(account string) string {
	runes := []rune(strings.TrimSpace(account))
	switch {
	case len(runes) <= 2:
		return string(runes[:min(1, len(runes))]) + "*"
	case len(runes) <= 6:
		return string(runes[:1]) + strings.Repeat("*", len(runes)-2) + string(runes[len(runes)-1:])
	default:
		return string(runes[:2]) + "****" + string(runes[len(runes)-2:])
	}
}

// ── 通知 ──

// announce 状态变化后的站内信、邮件与实时推送，都不阻塞调用方。email 为空时现查。
func (s *AccountDeletionService) announce(req *accountdeletion.Request, cfg accountdeletion.Config, kind, email, note string) {
	appID, userID := req.AppID, req.UserID
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		defer func() {
			if r := recover(); r != nil {
				s.log.Error("deletion announce panic", zap.Any("panic", r))
			}
		}()
		if s.userEvents != nil {
			data := map[string]any{"requestId": req.ID, "status": req.Status, "mode": req.Mode}
			if req.ScheduledAt != nil {
				data["scheduledAt"] = req.ScheduledAt
			}
			_ = s.userEvents.PublishUserEvent(ctx, appID, userID, "account.deletion", data)
		}
		title, content, level := deletionNotificationText(req, kind, note)
		if s.notifier != nil && title != "" {
			if _, err := s.notifier.Deliver(ctx, notificationdomain.DeliverInput{
				AppID: appID, UserID: userID, Type: "account_deletion",
				Title: title, Content: content, Level: level,
				Metadata: map[string]any{"module": "account_deletion", "event": kind, "requestId": req.ID},
				Action:   notificationdomain.RouteAction(notificationdomain.RouteSecurity, "查看注销进度"),
			}); err != nil {
				s.log.Warn("deletion notification failed", zap.Int64("userId", userID), zap.Error(err))
			}
		}
		if !cfg.NotifyEmail || s.mailer == nil {
			return
		}
		if email == "" {
			if profile, err := s.pg.GetUserProfileByUserID(ctx, userID); err == nil && profile != nil {
				email = strings.TrimSpace(profile.Email)
			}
		}
		if email == "" {
			return
		}
		notice := AccountDeletionNotice{
			Kind: kind, Account: req.Account, At: s.now(), ScheduledAt: req.ScheduledAt,
			Reason: accountdeletion.ReasonLabel(cfg, req.ReasonCode), Note: note, IP: req.IP, Device: req.Device,
		}
		if kind != AccountDeletionNoticeSubmitted && kind != AccountDeletionNoticeScheduled {
			notice.IP, notice.Device = "", ""
		}
		if err := s.mailer.SendAccountDeletionNotice(ctx, appID, email, notice); err != nil {
			s.log.Warn("deletion notice email failed", zap.Int64("userId", userID), zap.Error(err))
		}
	}()
}

func (s *AccountDeletionService) sendNotice(appID int64, email string, notice AccountDeletionNotice) {
	if s.mailer == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := s.mailer.SendAccountDeletionNotice(ctx, appID, email, notice); err != nil {
			s.log.Warn("deletion notice email failed", zap.Int64("appid", appID), zap.Error(err))
		}
	}()
}

func deletionNotificationText(req *accountdeletion.Request, kind, note string) (string, string, string) {
	due := ""
	if req.ScheduledAt != nil {
		due = req.ScheduledAt.In(timeutil.DefaultLocation()).Format("2006-01-02 15:04")
	}
	switch kind {
	case AccountDeletionNoticeSubmitted:
		return "注销申请已提交", "账号注销申请已提交，正在等待审核。审核期间可以在账号安全中撤销申请。", "warning"
	case AccountDeletionNoticeScheduled:
		return "账号进入注销冷静期", fmt.Sprintf("账号将于 %s 注销，冷静期内可以在账号安全中撤销申请。", due), "warning"
	case AccountDeletionNoticeReminder:
		return "账号即将注销", fmt.Sprintf("冷静期即将结束，账号将于 %s 注销。如需保留账号，请在此之前撤销申请。", due), "warning"
	case AccountDeletionNoticeCancelled:
		content := "账号注销申请已撤销，账号可以照常使用。"
		if note != "" {
			content = note + "。账号可以照常使用。"
		}
		return "注销申请已撤销", content, "info"
	case AccountDeletionNoticeRejected:
		content := "账号注销申请未通过审核，账号可以照常使用。"
		if note != "" {
			content = "账号注销申请未通过审核：" + note
		}
		return "注销申请未通过", content, "info"
	}
	return "", "", ""
}

func mapDeletionStoreError(err error) error {
	switch {
	case errors.Is(err, pgrepo.ErrDeletionRequestNotFound):
		return apperrors.New(errCodeDeletionNotFound, http.StatusNotFound, "注销申请不存在")
	case errors.Is(err, pgrepo.ErrDeletionOpenExists):
		return apperrors.New(errCodeDeletionOpenExists, http.StatusConflict, "已有进行中的注销申请")
	case errors.Is(err, pgrepo.ErrDeletionStateChanged):
		return apperrors.New(errCodeDeletionStateChanged, http.StatusConflict, "申请状态已变化，请刷新后重试")
	}
	return err
}
