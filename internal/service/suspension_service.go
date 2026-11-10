package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	authdomain "aegis/internal/domain/auth"
	"aegis/internal/domain/suspension"
	userdomain "aegis/internal/domain/user"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"

	"go.uber.org/zap"
)

// SuspensionService 账号封禁对用户的那一面：被拒时带上封禁详情与申诉凭证、全站封禁、申诉与复审、封禁通知。
// 契约见 docs/account-suspension.md。
type SuspensionService struct {
	log       *zap.Logger
	pg        *pgrepo.Repository
	masterKey []byte

	mu     sync.RWMutex
	bans   *AccountBanService
	email  *EmailService
	notify *NotificationService
	ai     *AIProviderService
	risk   *RiskService

	stop chan struct{}
	wg   sync.WaitGroup
	// reviewing 正在跑决策模型的申诉（同一条不并发审）
	reviewing sync.Map
}

// NewSuspensionService masterKey 用来签申诉凭证、算全站封禁主体的摘要
func NewSuspensionService(log *zap.Logger, pg *pgrepo.Repository, masterKey string) *SuspensionService {
	if log == nil {
		log = zap.NewNop()
	}
	key := sha256.Sum256([]byte("aegis.suspension|" + masterKey))
	return &SuspensionService{log: log, pg: pg, masterKey: key[:], stop: make(chan struct{})}
}

// SetDependencies 接封禁、邮件、站内消息、AI 通道、风控平台（都可为 nil）
func (s *SuspensionService) SetDependencies(bans *AccountBanService, email *EmailService, notify *NotificationService, ai *AIProviderService, risk *RiskService) {
	s.mu.Lock()
	s.bans, s.email, s.notify, s.ai, s.risk = bans, email, notify, ai, risk
	s.mu.Unlock()
	if bans != nil {
		bans.SetSuspensionService(s)
	}
}

// ── 包级挂点：enforceAccountBan 与注册前核对是包函数，经这里拿到服务 ──

var suspensionHooks atomic.Pointer[SuspensionService]

// InstallSuspensionHooks 让封禁拦截带上封禁详情与申诉凭证、登录时核对全站封禁
func InstallSuspensionHooks(s *SuspensionService) { suspensionHooks.Store(s) }

// ── 语言 ──

type localeKey struct{}

// WithRequestLocale 把请求的语言放进上下文（中间件按 Accept-Language 调）
func WithRequestLocale(ctx context.Context, acceptLanguage string) context.Context {
	if l := suspension.NormalizeLocale(acceptLanguage); l != "" {
		return context.WithValue(ctx, localeKey{}, l)
	}
	return ctx
}

// RequestLocale 请求的语言；没有时空串
func RequestLocale(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	l, _ := ctx.Value(localeKey{}).(string)
	return l
}

// rememberLocale 登录成功时记下用户语言，之后的通知按它发
func rememberLocale(ctx context.Context, pg *pgrepo.Repository, userID int64) {
	if pg == nil || userID <= 0 {
		return
	}
	if l := RequestLocale(ctx); l != "" {
		_ = pg.SetUserLocale(context.WithoutCancel(ctx), userID, l)
	}
}

// localeFor 通知用的语言：请求语言 → 用户记下的语言 → 默认
func localeFor(ctx context.Context, stored string) string {
	if l := RequestLocale(ctx); l != "" {
		return l
	}
	if l := suspension.NormalizeLocale(stored); l != "" {
		return l
	}
	return suspension.DefaultLocale
}

// ── 被拒时的错误 ──

// banErrorFor 把生效中的封禁换成带详情的错误；没装挂点时退回旧的纯文本错误
func banErrorFor(ctx context.Context, ban *userdomain.AccountBan) error {
	s := suspensionHooks.Load()
	if s == nil {
		return accountBanError(ban)
	}
	locale := localeFor(ctx, "")
	permanent := ban.BanType == userdomain.AccountBanTypePermanent
	code := 40302
	if permanent {
		code = 40301
	}
	view := s.viewOf(ctx, ban, locale)
	return apperrors.New(code, http.StatusForbidden, suspension.ErrorMessage(permanent, locale)).WithData(map[string]any{"suspension": view})
}

// viewOf 给客户端的封禁详情（含申诉状态与凭证）
func (s *SuspensionService) viewOf(ctx context.Context, ban *userdomain.AccountBan, locale string) suspension.View {
	reason := suspension.Reason(ban.ReasonCode, locale)
	v := suspension.View{
		ID: ban.ID, Level: firstNonEmpty(ban.Level, userdomain.AccountBanLevelApp), Type: ban.BanType, Scope: ban.BanScope,
		ReasonCode: firstNonEmpty(ban.ReasonCode, "policy"), ReasonTitle: reason.Title, Reason: firstNonEmpty(ban.PublicReason, reason.Description),
		StartAt: ban.StartAt, EndAt: ban.EndAt,
	}
	settings := s.settingsFor(ctx, ban.AppID)
	info := suspension.AppealInfo{Status: "none", Remaining: settings.MaxAppealsPerBan, MinLength: settings.MinLength, MaxLength: settings.MaxLength}
	appeals, _ := s.pg.AppealsOfBan(ctx, ban.ID)
	if len(appeals) > 0 {
		last := appeals[0]
		id, at := last.ID, last.CreatedAt
		info.Status, info.AppealID, info.SubmittedAt, info.DecidedAt, info.Reply = last.Status, &id, &at, last.DecidedAt, last.Reply
	}
	used := 0
	for _, a := range appeals {
		if a.Status != suspension.AppealWithdrawn {
			used++
		}
	}
	info.Remaining = max(settings.MaxAppealsPerBan-used, 0)
	switch {
	case !settings.Enabled:
		info.BlockedReason = suspension.BlockedDisabled
	case !ban.Appealable:
		info.BlockedReason = suspension.BlockedNotAppealable
	case len(appeals) > 0 && appeals[0].Open():
		info.BlockedReason = suspension.BlockedOpen
	case info.Remaining <= 0:
		info.BlockedReason = suspension.BlockedLimit
	default:
		// 冷却只在申诉被驳回之后算（自己撤回的不算）
		if len(appeals) > 0 && settings.CooldownHours > 0 && appeals[0].Status == suspension.AppealRejected && appeals[0].DecidedAt != nil {
			next := appeals[0].DecidedAt.Add(time.Duration(settings.CooldownHours) * time.Hour)
			if next.After(time.Now()) {
				info.BlockedReason = suspension.BlockedCooldown
				info.NextAllowedAt = &next
			}
		}
	}
	info.Allowed = info.BlockedReason == ""
	if ban.Status == userdomain.AccountBanStatusActive {
		info.Token = s.signToken(ban.AppID, ban.UserID, ban.ID, time.Now().Add(7*24*time.Hour))
	}
	v.Appeal = info
	return v
}

// ── 申诉凭证：sus1.<载荷>.<签名>，不登录也能查看与申诉 ──

type tokenPayload struct {
	App  int64 `json:"a"`
	User int64 `json:"u"`
	Ban  int64 `json:"b"`
	Exp  int64 `json:"e"`
}

func (s *SuspensionService) signToken(appID, userID, banID int64, exp time.Time) string {
	raw, _ := json.Marshal(tokenPayload{App: appID, User: userID, Ban: banID, Exp: exp.Unix()})
	body := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, s.masterKey)
	mac.Write([]byte("sus1." + body))
	return "sus1." + body + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)[:20])
}

func (s *SuspensionService) parseToken(token string) (*tokenPayload, bool) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 || parts[0] != "sus1" {
		return nil, false
	}
	mac := hmac.New(sha256.New, s.masterKey)
	mac.Write([]byte("sus1." + parts[1]))
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(sig, mac.Sum(nil)[:20]) {
		return nil, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, false
	}
	var p tokenPayload
	if json.Unmarshal(raw, &p) != nil || p.Exp < time.Now().Unix() || p.App <= 0 || p.User <= 0 {
		return nil, false
	}
	return &p, true
}

// ── 主体摘要（全站封禁） ──

func (s *SuspensionService) digest(kind, value string) string {
	mac := hmac.New(sha256.New, s.masterKey)
	mac.Write([]byte("pb1|" + kind + "|" + value))
	return hex.EncodeToString(mac.Sum(nil))
}

// ── 客户端接口 ──

// SuspensionCredential 客户端凭证：申诉凭证或会话（二选一）
type SuspensionCredential struct {
	AppID   int64
	Token   string
	Session *authdomain.Session
}

// resolve 凭证对应的账号与封禁；凭证无效返回 40150
func (s *SuspensionService) resolve(ctx context.Context, cred SuspensionCredential) (userID int64, ban *userdomain.AccountBan, err error) {
	if strings.TrimSpace(cred.Token) != "" {
		p, ok := s.parseToken(cred.Token)
		if !ok || p.App != cred.AppID {
			return 0, nil, apperrors.New(40150, http.StatusUnauthorized, "申诉凭证无效或已过期")
		}
		b, err := s.pg.GetUserAccountBan(ctx, cred.AppID, p.Ban)
		if err != nil {
			return 0, nil, err
		}
		if b == nil || b.UserID != p.User {
			return p.User, nil, nil
		}
		// 同一账号后来又被封了一次：以最新的为准
		if latest, err := s.pg.RefreshUserAccountBanState(ctx, cred.AppID, p.User); err == nil && latest != nil {
			return p.User, latest, nil
		}
		if b.Status != userdomain.AccountBanStatusActive {
			return p.User, nil, nil
		}
		return p.User, b, nil
	}
	if cred.Session == nil || cred.Session.UserID <= 0 || cred.Session.AppID != cred.AppID {
		return 0, nil, apperrors.New(40150, http.StatusUnauthorized, "需要登录或申诉凭证")
	}
	b, err := s.pg.RefreshUserAccountBanState(ctx, cred.AppID, cred.Session.UserID)
	if err != nil {
		return 0, nil, err
	}
	return cred.Session.UserID, b, nil
}

// SuspensionStatus POST /suspension：封禁详情与申诉记录
type SuspensionStatus struct {
	Suspension *suspension.View        `json:"suspension"`
	Appeals    []suspension.AppealView `json:"appeals"`
}

// Status 封禁详情与申诉记录
func (s *SuspensionService) Status(ctx context.Context, cred SuspensionCredential) (*SuspensionStatus, error) {
	userID, ban, err := s.resolve(ctx, cred)
	if err != nil {
		return nil, err
	}
	out := &SuspensionStatus{Appeals: []suspension.AppealView{}}
	locale := localeFor(ctx, "")
	if ban != nil {
		v := s.viewOf(ctx, ban, locale)
		out.Suspension = &v
	}
	appeals, err := s.pg.AppealsOfUser(ctx, cred.AppID, userID, 20)
	if err != nil {
		return nil, err
	}
	for _, a := range appeals {
		out.Appeals = append(out.Appeals, a.ToView())
	}
	return out, nil
}

// AppealSubmitInput 提交申诉
type AppealSubmitInput struct {
	Credential   SuspensionCredential
	Content      string
	ContactEmail string
	Source       string // app / web
	IP           string
	UserAgent    string
}

// AppealResult 提交与撤回的返回
type AppealResult struct {
	Appeal     suspension.AppealView `json:"appeal"`
	Suspension *suspension.View      `json:"suspension"`
}

// SubmitAppeal 提交申诉，随后异步跑决策模型复审
func (s *SuspensionService) SubmitAppeal(ctx context.Context, in AppealSubmitInput) (*AppealResult, error) {
	userID, ban, err := s.resolve(ctx, in.Credential)
	if err != nil {
		return nil, err
	}
	if ban == nil {
		return nil, apperrors.New(40422, http.StatusNotFound, "没有可申诉的封禁")
	}
	settings := s.settingsFor(ctx, ban.AppID)
	if !settings.Enabled {
		return nil, apperrors.New(42222, http.StatusUnprocessableEntity, "应用未开放申诉")
	}
	content := strings.TrimSpace(in.Content)
	if n := utf8.RuneCountInString(content); n < settings.MinLength || n > settings.MaxLength {
		return nil, apperrors.New(42220, http.StatusBadRequest, fmt.Sprintf("申诉内容需要 %d 到 %d 个字", settings.MinLength, settings.MaxLength)).
			WithData(map[string]any{"minLength": settings.MinLength, "maxLength": settings.MaxLength})
	}
	email := strings.TrimSpace(in.ContactEmail)
	if email != "" {
		if _, err := mail.ParseAddress(email); err != nil || len(email) > 254 {
			return nil, apperrors.New(42221, http.StatusBadRequest, "联系邮箱格式不正确")
		}
	}
	view := s.viewOf(ctx, ban, localeFor(ctx, ""))
	switch view.Appeal.BlockedReason {
	case "":
	case suspension.BlockedNotAppealable:
		return nil, apperrors.New(40935, http.StatusConflict, "这条封禁不允许申诉")
	case suspension.BlockedOpen:
		return nil, apperrors.New(40933, http.StatusConflict, "已有进行中的申诉")
	case suspension.BlockedLimit:
		return nil, apperrors.New(42920, http.StatusTooManyRequests, "申诉次数已用完")
	case suspension.BlockedCooldown:
		return nil, apperrors.New(42921, http.StatusTooManyRequests, "请稍后再提交申诉").WithData(map[string]any{"nextAllowedAt": view.Appeal.NextAllowedAt})
	case suspension.BlockedDisabled:
		return nil, apperrors.New(42222, http.StatusUnprocessableEntity, "应用未开放申诉")
	}
	source := in.Source
	if source != "web" {
		source = "app"
	}
	banID := ban.ID
	appeal, err := s.pg.InsertAppeal(ctx, suspension.Appeal{
		AppID: ban.AppID, UserID: userID, BanID: &banID, PlatformBanID: ban.PlatformBanID, Content: content, ContactEmail: email,
		Locale: localeFor(ctx, ""), Source: source, ClientIP: truncateString(in.IP, 64), UserAgent: truncateString(in.UserAgent, 512),
	})
	if errors.Is(err, pgrepo.ErrAppealOpenExists) {
		return nil, apperrors.New(40933, http.StatusConflict, "已有进行中的申诉")
	}
	if err != nil {
		return nil, err
	}
	s.log.Info("收到封禁申诉", zap.Int64("appid", ban.AppID), zap.Int64("userId", userID), zap.Int64("appealId", appeal.ID))
	go s.notifyAppeal(context.WithoutCancel(ctx), suspension.NoticeAppealReceived, appeal, ban)
	// 决策模型复审：立即在后台跑一次，失败的由定时任务兜底
	s.reviewAsync(appeal.ID)
	after := s.viewOf(ctx, ban, localeFor(ctx, ""))
	return &AppealResult{Appeal: appeal.ToView(), Suspension: &after}, nil
}

// WithdrawAppeal 撤回还没结案的申诉（待复审、复审中都可以）
func (s *SuspensionService) WithdrawAppeal(ctx context.Context, cred SuspensionCredential, appealID int64) (*AppealResult, error) {
	userID, ban, err := s.resolve(ctx, cred)
	if err != nil {
		return nil, err
	}
	a, err := s.pg.GetAppeal(ctx, cred.AppID, appealID)
	if err != nil {
		return nil, err
	}
	if a == nil || a.UserID != userID {
		return nil, apperrors.New(40423, http.StatusNotFound, "申诉不存在")
	}
	if !a.Open() {
		return nil, apperrors.New(40949, http.StatusConflict, "申诉已结案，不能撤回")
	}
	updated, err := s.pg.DecideAppeal(ctx, a.ID, suspension.AppealWithdrawn, "", userdomain.BanOperator{}, "用户撤回", "")
	if errors.Is(err, pgrepo.ErrAppealClosed) {
		return nil, apperrors.New(40949, http.StatusConflict, "申诉已结案，不能撤回")
	}
	if err != nil {
		return nil, err
	}
	out := &AppealResult{Appeal: updated.ToView()}
	if ban != nil {
		v := s.viewOf(ctx, ban, localeFor(ctx, ""))
		out.Suspension = &v
	}
	return out, nil
}

// ── 申诉设置 ──

func (s *SuspensionService) settingsFor(ctx context.Context, appID int64) suspension.Settings {
	st, _, err := s.pg.GetAppealSettings(ctx, appID)
	if err != nil || st == nil {
		return suspension.DefaultSettings()
	}
	n, problem := st.Normalize()
	if problem != "" {
		return suspension.DefaultSettings()
	}
	return n
}

// AppealSettingsView 设置与它是不是这个应用自己的（false = 继承平台默认或内置默认）
type AppealSettingsView struct {
	Settings suspension.Settings `json:"settings"`
	Own      bool                `json:"own"`
}

// GetSettings 应用的申诉设置
func (s *SuspensionService) GetSettings(ctx context.Context, appID int64) (*AppealSettingsView, error) {
	st, own, err := s.pg.GetAppealSettings(ctx, appID)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return &AppealSettingsView{Settings: suspension.DefaultSettings()}, nil
	}
	n, _ := st.Normalize()
	return &AppealSettingsView{Settings: n, Own: own}, nil
}

// SaveSettings 保存应用的申诉设置
func (s *SuspensionService) SaveSettings(ctx context.Context, appID int64, in suspension.Settings, by string) (*AppealSettingsView, error) {
	n, problem := in.Normalize()
	if problem != "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, problem)
	}
	if err := s.pg.SaveAppealSettings(ctx, appID, n, by); err != nil {
		return nil, err
	}
	return &AppealSettingsView{Settings: n, Own: true}, nil
}

func truncateString(v string, n int) string {
	if len(v) <= n {
		return v
	}
	return v[:n]
}

// Contact 账号的应用名、登录名、邮箱、手机号、语言（管理端补展示用）
func (s *SuspensionService) Contact(ctx context.Context, appID, userID int64) (*pgrepo.SuspensionContact, error) {
	return s.pg.SuspensionContactOf(ctx, appID, userID)
}

// Ban 一条账号封禁
func (s *SuspensionService) Ban(ctx context.Context, appID, banID int64) (*userdomain.AccountBan, error) {
	return s.pg.GetUserAccountBan(ctx, appID, banID)
}
