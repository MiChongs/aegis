package service

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	authdomain "aegis/internal/domain/auth"
	captchadomain "aegis/internal/domain/captcha"
	notificationdomain "aegis/internal/domain/notification"
	securitydomain "aegis/internal/domain/security"
	userdomain "aegis/internal/domain/user"
	"aegis/internal/event"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"

	"go.uber.org/zap"
)

// 找回密码。
//
// 一次找回是服务端保管的一个流程（redisrepo.PasswordRecoveryFlow），客户端只拿到
// 一个不透明的 flowToken。流程在发起那一刻就按账号当时的绑定情况定好「要过哪几项
// 验证」，客户端不能挑、也不能跳；全部满足之后才允许设新密码，设完流程即作废。
//
// 要过哪几项（recoveryRequirements）：
//
//	未登录发起（recover，凭账号 / 邮箱 / 手机号认人，发起前过图形验证码）
//	  - 一项联系方式（邮箱或手机）的验证码。发码前要填出**完整**的联系方式，
//	    页面上只有打码后的提示：既证明「知道」也证明「收得到」，这条接口也就
//	    没法被拿来给任意号码发短信。
//	  - 开了两步验证：再加身份验证器动态码（或一个恢复码）。
//	  - 没开两步验证、邮箱和手机都绑了：两项都要验。单一渠道失守（邮箱被盗、
//	    手机号被补卡）不足以拿走账号。
//	已登录发起（session，当前会话本身算一项）
//	  - 一项联系方式；开了两步验证再加动态码。没有可用联系方式时只验动态码。
//
// 刚换绑 72 小时之内的联系方式不算数：偷到会话的人可以先绑一个自己的手机号再来
// 找回密码，冷静期就是为这条链路准备的。
//
// 设新密码之后：其余会话与刷新令牌全部吊销（已登录发起时保留当前这一个），第三方
// 应用授权一并撤销，账号维度的登录锁定解除，站内信与绑定邮箱各收到一封告知。
// 响应里不签发令牌 —— 未登录找回的人要用新密码重新登录一次。

const (
	passwordRecoveryModeRecover = "recover"
	passwordRecoveryModeSession = "session"

	recoveryFactorEmail    = "email"
	recoveryFactorPhone    = "phone"
	recoveryFactorTOTP     = "totp"
	recoveryFactorRecovery = "recovery"

	passwordRecoveryFlowTTL        = 15 * time.Minute
	passwordRecoveryCompleteWindow = 10 * time.Minute
	passwordRecoveryCodeTTL        = 10 * time.Minute
	passwordRecoveryResendInterval = 60 * time.Second
	passwordRecoveryMaxSends       = 5
	passwordRecoveryMaxCodeTries   = 5
	passwordRecoveryMaxFailures    = 8
	// ContactChangeCooldown 换绑之后多久这个联系方式才能用于找回密码。
	ContactChangeCooldown = 72 * time.Hour

	passwordRecoveryStartsPerIPHour        = 10
	passwordRecoveryStartsPerIdentifierDay = 10
	passwordRecoveryStartsPerUserDay       = 6
	passwordRecoveryCompletesPerUserDay    = 3

	errCodeRecoveryFlowInvalid    = 40120 // 流程不存在、已过期或已用完
	errCodeRecoveryDeviceMismatch = 40121 // 换了设备继续流程
	errCodeRecoveryTargetMismatch = 40122 // 填的联系方式与绑定的不一致
	errCodeRecoveryFactorInvalid  = 40123 // 这一项不在本次要求里，或已经验过
	errCodeRecoveryCodeInvalid    = 40124 // 验证码错误
	errCodeRecoveryNotVerified    = 40125 // 验证尚未全部完成
	errCodeRecoveryCodeMissing    = 40126 // 还没发码，或码已失效
	errCodeRecoveryUnavailable    = 40327 // 账号没有可用于找回的验证方式
	errCodeRecoveryAccountMissing = 40414 // 找不到账号
	errCodeRecoveryResendTooSoon  = 42907
	errCodeRecoveryDailyLimit     = 42908
	errCodeRecoveryTooManyFailure = 42909
)

// PasswordRecoveryStartInput 未登录发起找回。
type PasswordRecoveryStartInput struct {
	AppID      int64
	Identifier string
	DeviceID   string
	IP         string
	UserAgent  string
}

// PasswordRecoveryChannelView 一项联系方式，只给打码后的样子。
type PasswordRecoveryChannelView struct {
	Type     string `json:"type"`
	Masked   string `json:"masked"`
	Verified bool   `json:"verified"`
	// CodeSent 本流程里已经向它发过码
	CodeSent bool `json:"codeSent"`
	// ResendAt 下一次可以再发的时刻；没发过时省略
	ResendAt *time.Time `json:"resendAt,omitempty"`
}

// PasswordRecoveryRequirementView 一项验证要求：any 里任意一种通过即满足。
type PasswordRecoveryRequirementView struct {
	Any       []string `json:"any"`
	Satisfied bool     `json:"satisfied"`
	By        string   `json:"by,omitempty"`
}

// PasswordRecoveryView 流程的当前状态。flowToken 只在发起响应里出现。
type PasswordRecoveryView struct {
	FlowToken    string                            `json:"flowToken,omitempty"`
	Mode         string                            `json:"mode"`
	Account      string                            `json:"account"`
	Channels     []PasswordRecoveryChannelView     `json:"channels"`
	TOTP         bool                              `json:"totp"`
	Requirements []PasswordRecoveryRequirementView `json:"requirements"`
	// Ready 全部要求已满足，可以设新密码
	Ready bool `json:"ready"`
	// AttemptsLeft 流程作废之前还能错几次
	AttemptsLeft int       `json:"attemptsLeft"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

// PasswordRecoveryStepInput 发码 / 核验 / 设新密码共用的流程定位。
type PasswordRecoveryStepInput struct {
	AppID     int64
	FlowToken string
	DeviceID  string
	IP        string
}

// PasswordRecoverySendResult /auth/password/recovery/send 的响应。
type PasswordRecoverySendResult struct {
	Channel   string    `json:"channel"`
	Masked    string    `json:"masked"`
	ExpiresAt time.Time `json:"expiresAt"`
	ResendAt  time.Time `json:"resendAt"`
}

// PasswordRecoveryCompleteInput 设新密码。
type PasswordRecoveryCompleteInput struct {
	PasswordRecoveryStepInput
	NewPassword string
	Device      string
	UserAgent   string
	Location    string
}

// PasswordRecoveryCompleteResult /auth/password/recovery/complete 的响应。
type PasswordRecoveryCompleteResult struct {
	// Account 登录用的账号名。凭邮箱或手机号找回的人可能并不记得它
	Account string `json:"account"`
	// SignedOut 被吊销的登录（会话与刷新令牌按登录计）
	SignedOut int `json:"signedOut"`
	// KeptCurrentSession 已登录发起时当前会话被保留
	KeptCurrentSession bool `json:"keptCurrentSession"`
}

// passwordRecoveryMailer 找回密码用到的发信能力。EmailService 实现它；测试换成记录验证码的假实现。
type passwordRecoveryMailer interface {
	RecoveryMailReady(ctx context.Context, appID int64) bool
	SendRecoveryCode(ctx context.Context, appID int64, email string, code string, expireMinutes int) error
	SendPasswordResetNotice(ctx context.Context, appID int64, email string, notice PasswordResetNotice) error
}

// SetPasswordRecovery 注入找回密码要用到的发信、短信与会话治理。
// 未注入时邮箱一项不可用，而不是空指针。
func (s *AuthService) SetPasswordRecovery(email *EmailService, captcha *CaptchaService, users *UserService) {
	if email != nil {
		s.recoveryEmail = email
	}
	s.recoveryCaptcha = captcha
	s.recoveryUsers = users
}

func (s *AuthService) ensureRecoveryReady() error {
	if s.sessions == nil || s.security == nil {
		return apperrors.New(50320, http.StatusServiceUnavailable, "找回密码服务暂不可用")
	}
	return nil
}

// StartPasswordRecovery 未登录发起：认出账号，定好要过的验证。
func (s *AuthService) StartPasswordRecovery(ctx context.Context, input PasswordRecoveryStartInput) (*PasswordRecoveryView, error) {
	if err := s.ensureRecoveryReady(); err != nil {
		return nil, err
	}
	identifier := normalizeAccount(input.Identifier)
	if identifier == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请输入账号、邮箱或手机号")
	}
	if s.app != nil {
		if _, err := s.app.EnsureLoginAllowed(ctx, input.AppID); err != nil {
			return nil, err
		}
	}
	if err := s.hitRecoveryLimit(ctx, "ip", fmt.Sprintf("%d:%s", input.AppID, input.IP),
		time.Hour, passwordRecoveryStartsPerIPHour, "当前网络环境找回密码过于频繁，请稍后再试"); err != nil {
		return nil, err
	}
	if err := s.hitRecoveryLimit(ctx, "ident", fmt.Sprintf("%d:%s", input.AppID, strings.ToLower(identifier)),
		24*time.Hour, passwordRecoveryStartsPerIdentifierDay, "该账号今日找回密码次数已达上限"); err != nil {
		return nil, err
	}
	// 先查账号再评估：风控据此只把真实存在的账号数进「出现的账号」。找不到账号的错误仍排在风控之后返回
	user, err := s.resolveRecoveryUser(ctx, input.AppID, identifier)
	if s.risk != nil {
		appID := input.AppID
		req := securitydomain.RiskEvalRequest{
			Scene: securitydomain.ScenePasswordReset, AppID: &appID, IP: input.IP, DeviceID: input.DeviceID,
			UserAgent: input.UserAgent, Account: identifier,
		}
		if err == nil && user != nil {
			req.AccountUserID = user.ID
		}
		if _, gerr := s.risk.Guard(ctx, req, RiskGuardOption{Message: "当前请求被风控拦截，请稍后重试"}); gerr != nil {
			return nil, gerr
		}
	}
	if err != nil {
		return nil, err
	}
	if err := s.ensureUserLoginState(ctx, user); err != nil {
		return nil, err
	}
	if err := s.hitRecoveryLimit(ctx, "user", fmt.Sprintf("%d:%d", user.AppID, user.ID),
		24*time.Hour, passwordRecoveryStartsPerUserDay, "该账号今日找回密码次数已达上限"); err != nil {
		return nil, err
	}
	return s.openRecoveryFlow(ctx, user, redisrepo.PasswordRecoveryFlow{
		Mode:     passwordRecoveryModeRecover,
		DeviceID: strings.TrimSpace(input.DeviceID),
		IP:       input.IP,
	})
}

// StartSessionPasswordRecovery 已登录发起：忘了原密码、又不想退出登录。
//
// 当前会话已经证明了一项，所以这里不要图形验证码；设新密码之后只保留这一个会话。
func (s *AuthService) StartSessionPasswordRecovery(ctx context.Context, session *authdomain.Session, deviceID, ip string) (*PasswordRecoveryView, error) {
	if err := s.ensureRecoveryReady(); err != nil {
		return nil, err
	}
	user, err := s.pg.GetUserByID(ctx, session.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.AppID != session.AppID {
		return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	if err := s.ensureUserLoginState(ctx, user); err != nil {
		return nil, err
	}
	if err := s.hitRecoveryLimit(ctx, "user", fmt.Sprintf("%d:%d", user.AppID, user.ID),
		24*time.Hour, passwordRecoveryStartsPerUserDay, "该账号今日找回密码次数已达上限"); err != nil {
		return nil, err
	}
	if deviceID = strings.TrimSpace(deviceID); deviceID == "" {
		deviceID = session.DeviceID
	}
	return s.openRecoveryFlow(ctx, user, redisrepo.PasswordRecoveryFlow{
		Mode:         passwordRecoveryModeSession,
		DeviceID:     deviceID,
		IP:           ip,
		KeepTokenID:  session.TokenID,
		KeepFamilyID: session.RefreshFamilyID,
	})
}

// resolveRecoveryUser 账号名优先（手机号、邮箱也可能就是账号名），再按档案里的邮箱 / 手机号找。
// 一个邮箱或手机号挂在多个账号上时不替用户挑，要他改填账号名。
func (s *AuthService) resolveRecoveryUser(ctx context.Context, appID int64, identifier string) (*userdomain.User, error) {
	user, err := s.pg.GetUserByAppAndAccount(ctx, appID, identifier)
	if err != nil {
		return nil, err
	}
	if user != nil {
		return user, nil
	}
	var ids []int64
	switch {
	case strings.Contains(identifier, "@"):
		ids, err = s.pg.ListUserIDsByProfileEmail(ctx, appID, identifier, 2)
	case looksLikePhone(identifier):
		ids, err = s.pg.ListUserIDsByProfilePhone(ctx, appID, normalizePhone(identifier), 2)
	}
	if err != nil {
		return nil, err
	}
	switch len(ids) {
	case 0:
		return nil, apperrors.New(errCodeRecoveryAccountMissing, http.StatusNotFound, "未找到该账号")
	case 1:
		user, err = s.pg.GetUserByID(ctx, ids[0])
		if err != nil {
			return nil, err
		}
		if user == nil || user.AppID != appID {
			return nil, apperrors.New(errCodeRecoveryAccountMissing, http.StatusNotFound, "未找到该账号")
		}
		return user, nil
	default:
		return nil, apperrors.New(40986, http.StatusConflict, "该邮箱或手机号关联了多个账号，请改用账号名找回")
	}
}

// openRecoveryFlow 盘点账号的可用验证方式，定好要求，写入流程。
func (s *AuthService) openRecoveryFlow(ctx context.Context, user *userdomain.User, flow redisrepo.PasswordRecoveryFlow) (*PasswordRecoveryView, error) {
	channels, cooling, err := s.recoveryChannels(ctx, user)
	if err != nil {
		return nil, err
	}
	totpEnabled := false
	if record, err := s.pg.GetUserTOTPSecret(ctx, user.AppID, user.ID); err != nil {
		return nil, err
	} else if record != nil && record.Enabled {
		totpEnabled = true
	}
	requirements, err := recoveryRequirements(flow.Mode, channels, totpEnabled)
	if err != nil {
		if cooling > 0 {
			return nil, apperrors.New(errCodeRecoveryUnavailable, http.StatusForbidden,
				fmt.Sprintf("邮箱或手机号刚刚变更，%s后才能用于找回密码", humanizeHours(cooling)))
		}
		return nil, err
	}
	token, err := randomURLToken(32)
	if err != nil {
		return nil, err
	}
	now := timeutil.NowUTC()
	flow.AppID = user.AppID
	flow.UserID = user.ID
	flow.Account = user.Account
	flow.Channels = channels
	flow.TOTP = totpEnabled
	flow.Requirements = requirements
	flow.CreatedAt = now
	flow.ExpiresAt = now.Add(passwordRecoveryFlowTTL)
	if err := s.sessions.CreatePasswordRecoveryFlow(ctx, hashPollToken(token), flow, passwordRecoveryFlowTTL); err != nil {
		return nil, err
	}
	view := recoveryView(&flow)
	view.FlowToken = token
	return view, nil
}

// recoveryChannels 能收验证码、且不在换绑冷静期内的联系方式。第二个返回值是被冷静期
// 挡掉的那些里最长的剩余时间，用来在「一项都不剩」时说清原因。
func (s *AuthService) recoveryChannels(ctx context.Context, user *userdomain.User) ([]redisrepo.PasswordRecoveryChannel, time.Duration, error) {
	profile, err := s.pg.GetUserProfileByUserID(ctx, user.ID)
	if err != nil {
		return nil, 0, err
	}
	var email, phone string
	if profile != nil {
		email = strings.TrimSpace(profile.Email)
		phone = normalizePhone(profile.Phone)
	}
	// 短信注册的账号手机号就是账号名，档案里未必另存了一份
	if phone == "" && looksLikePhone(user.Account) {
		phone = normalizePhone(user.Account)
	}
	var (
		channels []redisrepo.PasswordRecoveryChannel
		cooling  time.Duration
	)
	consider := func(kind, value, masked string, usable bool) error {
		if value == "" || !usable {
			return nil
		}
		remaining, err := s.sessions.ContactChangeCooldown(ctx, user.AppID, user.ID, kind)
		if err != nil {
			return err
		}
		if remaining > 0 {
			if remaining > cooling {
				cooling = remaining
			}
			return nil
		}
		channels = append(channels, redisrepo.PasswordRecoveryChannel{Type: kind, Value: value, Masked: masked})
		return nil
	}
	if err := consider(recoveryFactorEmail, email, maskEmail(email), s.recoveryEmailUsable(ctx, user.AppID)); err != nil {
		return nil, 0, err
	}
	if err := consider(recoveryFactorPhone, phone, maskPhoneValue(phone), s.recoverySMSUsable(ctx, user.AppID)); err != nil {
		return nil, 0, err
	}
	return channels, cooling, nil
}

func (s *AuthService) recoveryEmailUsable(ctx context.Context, appID int64) bool {
	return s.recoveryEmail != nil && s.recoveryEmail.RecoveryMailReady(ctx, appID)
}

func (s *AuthService) recoverySMSUsable(ctx context.Context, appID int64) bool {
	_, err := s.recoverySMSProvider(ctx, appID)
	return err == nil
}

func (s *AuthService) recoverySMSProvider(ctx context.Context, appID int64) (*captchadomain.SMSProviderConfig, error) {
	if s.recoveryCaptcha == nil || !s.recoveryCaptcha.SMSAvailable() || s.app == nil {
		return nil, apperrors.New(50310, http.StatusServiceUnavailable, "短信验证服务暂不可用")
	}
	cfg, err := s.app.GetCaptchaConfig(ctx, appID)
	if err != nil {
		return nil, err
	}
	return BuildSMSProviderConfig(appID, captchadomain.PurposeResetPassword, cfg.SMS)
}

// recoveryRequirements 规则见文件头。纯函数，测试直接覆盖各种绑定组合。
func recoveryRequirements(mode string, channels []redisrepo.PasswordRecoveryChannel, totp bool) ([]redisrepo.PasswordRecoveryRequirement, error) {
	contact := make([]string, 0, len(channels))
	for _, item := range channels {
		contact = append(contact, item.Type)
	}
	var out []redisrepo.PasswordRecoveryRequirement
	switch mode {
	case passwordRecoveryModeSession:
		if len(contact) > 0 {
			out = append(out, redisrepo.PasswordRecoveryRequirement{Any: contact})
		}
		if totp {
			out = append(out, redisrepo.PasswordRecoveryRequirement{Any: []string{recoveryFactorTOTP}})
		}
		if len(out) == 0 {
			return nil, apperrors.New(errCodeRecoveryUnavailable, http.StatusForbidden,
				"请先绑定邮箱或手机号，或使用原密码修改密码")
		}
	default:
		if len(contact) == 0 {
			return nil, apperrors.New(errCodeRecoveryUnavailable, http.StatusForbidden,
				"该账号未绑定可用于找回的邮箱或手机号，请联系客服")
		}
		switch {
		case totp:
			out = append(out,
				redisrepo.PasswordRecoveryRequirement{Any: contact},
				redisrepo.PasswordRecoveryRequirement{Any: []string{recoveryFactorTOTP}})
		case len(contact) >= 2:
			for _, kind := range contact {
				out = append(out, redisrepo.PasswordRecoveryRequirement{Any: []string{kind}})
			}
		default:
			out = append(out, redisrepo.PasswordRecoveryRequirement{Any: contact})
		}
	}
	return out, nil
}

// PasswordRecoveryStatus 读流程状态（客户端恢复页面时用）。
func (s *AuthService) PasswordRecoveryStatus(ctx context.Context, input PasswordRecoveryStepInput) (*PasswordRecoveryView, error) {
	if err := s.ensureRecoveryReady(); err != nil {
		return nil, err
	}
	flow, _, err := s.loadRecoveryFlow(ctx, input)
	if err != nil {
		return nil, err
	}
	return recoveryView(flow), nil
}

// SendPasswordRecoveryCode 向流程里的一项联系方式发验证码。
//
// 未登录发起时 target 必填，必须与绑定的完整值一致；对不上算一次失败。
func (s *AuthService) SendPasswordRecoveryCode(ctx context.Context, input PasswordRecoveryStepInput, channel, target string) (*PasswordRecoverySendResult, error) {
	if err := s.ensureRecoveryReady(); err != nil {
		return nil, err
	}
	channel = strings.ToLower(strings.TrimSpace(channel))
	if channel != recoveryFactorEmail && channel != recoveryFactorPhone {
		return nil, apperrors.New(errCodeRecoveryFactorInvalid, http.StatusBadRequest, "channel 只能是 email 或 phone")
	}
	_, hash, err := s.loadRecoveryFlow(ctx, input)
	if err != nil {
		return nil, err
	}

	now := timeutil.NowUTC()
	code := generateEmailCode()
	var (
		reserved     redisrepo.PasswordRecoveryChannel
		previousSent *time.Time
	)
	flow, err := s.sessions.UpdatePasswordRecoveryFlow(ctx, input.AppID, hash, func(flow *redisrepo.PasswordRecoveryFlow) (bool, time.Duration, error) {
		if flow.VerifiedAt != nil {
			return true, 0, apperrors.New(errCodeRecoveryFactorInvalid, http.StatusConflict, "验证已完成，请直接设置新密码")
		}
		item := findRecoveryChannel(flow, channel)
		if item == nil || !recoveryFactorPending(flow, channel) {
			return true, 0, apperrors.New(errCodeRecoveryFactorInvalid, http.StatusBadRequest, "这一项不需要验证或已经验证过")
		}
		if flow.Mode == passwordRecoveryModeRecover || strings.TrimSpace(target) != "" {
			if !recoveryTargetMatches(channel, item.Value, target) {
				return recordRecoveryFailure(flow, apperrors.New(errCodeRecoveryTargetMismatch, http.StatusBadRequest,
					fmt.Sprintf("填写的%s与账号绑定的不一致", recoveryChannelNoun(channel))))
			}
		}
		if item.LastSentAt != nil && now.Sub(*item.LastSentAt) < passwordRecoveryResendInterval {
			wait := passwordRecoveryResendInterval - now.Sub(*item.LastSentAt)
			return true, 0, apperrors.New(errCodeRecoveryResendTooSoon, http.StatusTooManyRequests,
				fmt.Sprintf("发送过于频繁，请 %d 秒后再试", int(wait.Seconds())+1))
		}
		if item.Sends >= passwordRecoveryMaxSends {
			return true, 0, apperrors.New(errCodeRecoveryResendTooSoon, http.StatusTooManyRequests, "本次找回发送次数已达上限，请重新开始")
		}
		previousSent = item.LastSentAt
		item.Sends++
		item.LastSentAt = &now
		item.CodeAttempts = 0
		if channel == recoveryFactorEmail {
			expires := now.Add(passwordRecoveryCodeTTL)
			item.CodeHash = recoveryCodeHash(hash, code)
			item.CodeExpiresAt = &expires
		}
		reserved = *item
		return true, 0, nil
	})
	if err != nil {
		return nil, mapRecoveryStoreError(err)
	}

	var sendErr error
	switch channel {
	case recoveryFactorEmail:
		if s.recoveryEmail == nil {
			sendErr = apperrors.New(50310, http.StatusServiceUnavailable, "邮箱验证服务暂不可用")
		} else {
			sendErr = s.recoveryEmail.SendRecoveryCode(ctx, flow.AppID, reserved.Value, code, int(passwordRecoveryCodeTTL/time.Minute))
		}
	case recoveryFactorPhone:
		providerCfg, err := s.recoverySMSProvider(ctx, flow.AppID)
		if err != nil {
			sendErr = err
		} else {
			_, sendErr = s.recoveryCaptcha.SendTrustedSMSCode(ctx, captchadomain.SMSSendRequest{
				AppID: flow.AppID, Phone: reserved.Value, Purpose: captchadomain.PurposeResetPassword, ClientIP: input.IP,
			}, providerCfg)
		}
	}
	if sendErr != nil {
		// 没发出去就把这次占位退回去，否则人要白等一分钟、还白占一次额度
		_, _ = s.sessions.UpdatePasswordRecoveryFlow(ctx, input.AppID, hash, func(flow *redisrepo.PasswordRecoveryFlow) (bool, time.Duration, error) {
			if item := findRecoveryChannel(flow, channel); item != nil {
				item.Sends--
				item.LastSentAt = previousSent
				item.CodeHash = ""
				item.CodeExpiresAt = nil
			}
			return true, 0, nil
		})
		return nil, sendErr
	}
	return &PasswordRecoverySendResult{
		Channel:   channel,
		Masked:    reserved.Masked,
		ExpiresAt: now.Add(passwordRecoveryCodeTTL),
		ResendAt:  now.Add(passwordRecoveryResendInterval),
	}, nil
}

// VerifyPasswordRecoveryFactor 核验一项：邮箱 / 短信验证码、身份验证器动态码或恢复码。
func (s *AuthService) VerifyPasswordRecoveryFactor(ctx context.Context, input PasswordRecoveryStepInput, factor, code string) (*PasswordRecoveryView, error) {
	if err := s.ensureRecoveryReady(); err != nil {
		return nil, err
	}
	factor = strings.ToLower(strings.TrimSpace(factor))
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请输入验证码")
	}
	current, hash, err := s.loadRecoveryFlow(ctx, input)
	if err != nil {
		return nil, err
	}
	slot := factor
	if factor == recoveryFactorRecovery {
		slot = recoveryFactorTOTP
	}
	if !recoveryFactorPending(current, slot) {
		return nil, apperrors.New(errCodeRecoveryFactorInvalid, http.StatusBadRequest, "这一项不需要验证或已经验证过")
	}

	// 短信码与动态码由各自的服务核验（各有自己的尝试上限）；这里只把结论记进流程。
	var external error
	switch factor {
	case recoveryFactorEmail:
	case recoveryFactorPhone:
		item := findRecoveryChannel(current, recoveryFactorPhone)
		if item == nil || item.LastSentAt == nil {
			return nil, apperrors.New(errCodeRecoveryCodeMissing, http.StatusBadRequest, "请先获取短信验证码")
		}
		if s.recoveryCaptcha == nil {
			return nil, apperrors.New(50310, http.StatusServiceUnavailable, "短信验证服务暂不可用")
		}
		valid, err := s.recoveryCaptcha.VerifySMSCode(ctx, captchadomain.SMSVerifyRequest{
			AppID: current.AppID, Phone: item.Value, Code: code, Purpose: captchadomain.PurposeResetPassword,
		})
		switch {
		case err != nil:
			external = err
		case !valid:
			external = apperrors.New(errCodeRecoveryCodeInvalid, http.StatusBadRequest, "短信验证码错误")
		}
	case recoveryFactorTOTP:
		external = s.security.verifyUserSecondFactor(ctx, current.AppID, current.UserID, code, "")
	case recoveryFactorRecovery:
		external = s.security.verifyUserSecondFactor(ctx, current.AppID, current.UserID, "", code)
	default:
		return nil, apperrors.New(errCodeRecoveryFactorInvalid, http.StatusBadRequest, "factor 只能是 email、phone、totp 或 recovery")
	}
	if external != nil && !countsAsRecoveryFailure(external) {
		// 服务不可用之类不是猜错，不记失败
		return nil, external
	}

	now := timeutil.NowUTC()
	flow, err := s.sessions.UpdatePasswordRecoveryFlow(ctx, input.AppID, hash, func(flow *redisrepo.PasswordRecoveryFlow) (bool, time.Duration, error) {
		if !recoveryFactorPending(flow, slot) {
			return true, 0, apperrors.New(errCodeRecoveryFactorInvalid, http.StatusBadRequest, "这一项不需要验证或已经验证过")
		}
		if factor == recoveryFactorEmail {
			item := findRecoveryChannel(flow, recoveryFactorEmail)
			if item == nil || item.CodeHash == "" || item.CodeExpiresAt == nil || now.After(*item.CodeExpiresAt) {
				return true, 0, apperrors.New(errCodeRecoveryCodeMissing, http.StatusBadRequest, "验证码已失效，请重新获取")
			}
			if subtle.ConstantTimeCompare([]byte(item.CodeHash), []byte(recoveryCodeHash(hash, code))) != 1 {
				item.CodeAttempts++
				if item.CodeAttempts >= passwordRecoveryMaxCodeTries {
					item.CodeHash = ""
					item.CodeExpiresAt = nil
				}
				return recordRecoveryFailure(flow, apperrors.New(errCodeRecoveryCodeInvalid, http.StatusBadRequest, "邮箱验证码错误"))
			}
			item.CodeHash = ""
			item.CodeExpiresAt = nil
		} else if external != nil {
			return recordRecoveryFailure(flow, external)
		}
		if item := findRecoveryChannel(flow, factor); item != nil {
			item.Verified = true
		}
		satisfyRecoveryRequirement(flow, slot, factor)
		if recoveryFlowSatisfied(flow) {
			flow.VerifiedAt = &now
			flow.ExpiresAt = now.Add(passwordRecoveryCompleteWindow)
			return true, passwordRecoveryCompleteWindow, nil
		}
		return true, 0, nil
	})
	if err != nil {
		return nil, mapRecoveryStoreError(err)
	}
	return recoveryView(flow), nil
}

// CompletePasswordRecovery 设新密码，作废流程，吊销其余登录，发出告知。
func (s *AuthService) CompletePasswordRecovery(ctx context.Context, input PasswordRecoveryCompleteInput) (*PasswordRecoveryCompleteResult, error) {
	if err := s.ensureRecoveryReady(); err != nil {
		return nil, err
	}
	current, hash, err := s.loadRecoveryFlow(ctx, input.PasswordRecoveryStepInput)
	if err != nil {
		return nil, err
	}
	if current.VerifiedAt == nil {
		return nil, apperrors.New(errCodeRecoveryNotVerified, http.StatusForbidden, "请先完成身份验证")
	}
	user, err := s.pg.GetUserByID(ctx, current.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.AppID != current.AppID {
		return nil, apperrors.New(errCodeRecoveryFlowInvalid, http.StatusGone, "找回流程已失效，请重新开始")
	}
	if err := s.ensureUserLoginState(ctx, user); err != nil {
		return nil, err
	}

	// 密码不合格时流程原样保留，改一个密码再交即可
	pctx := s.userPasswordContext(ctx, user)
	if err := s.validatePasswordPolicyWithContext(ctx, user.AppID, input.NewPassword, pctx); err != nil {
		return nil, err
	}
	if user.PasswordHash != "" && verifyPassword(user.PasswordHash, input.NewPassword) {
		return nil, apperrors.New(40006, http.StatusBadRequest, "新密码不能与当前密码相同")
	}
	if s.app != nil {
		if err := s.app.EnsurePasswordNotReused(ctx, user.AppID, user.ID, input.NewPassword); err != nil {
			return nil, err
		}
	}
	passwordHash, err := hashPassword(input.NewPassword)
	if err != nil {
		return nil, err
	}

	// 先作废流程再落密码：并发的两次提交只有一次能拿到流程
	if _, err := s.sessions.UpdatePasswordRecoveryFlow(ctx, input.AppID, hash, func(flow *redisrepo.PasswordRecoveryFlow) (bool, time.Duration, error) {
		if flow.VerifiedAt == nil {
			return true, 0, apperrors.New(errCodeRecoveryNotVerified, http.StatusForbidden, "请先完成身份验证")
		}
		return false, 0, nil
	}); err != nil {
		return nil, mapRecoveryStoreError(err)
	}
	if err := s.hitRecoveryLimit(ctx, "complete", fmt.Sprintf("%d:%d", user.AppID, user.ID),
		24*time.Hour, passwordRecoveryCompletesPerUserDay, "该账号今日重置密码次数已达上限，请明天再试"); err != nil {
		return nil, err
	}

	changedAt := timeutil.NowUTC()
	var lifecycle PasswordLifecycle
	if s.app != nil {
		lifecycle = s.app.ResolvePasswordLifecycle(ctx, user.AppID, changedAt)
	}
	if err := s.pg.UpdateUserPassword(ctx, user.ID, passwordHash, changedAt, lifecycle.ExpiresAt); err != nil {
		return nil, err
	}
	if err := s.pg.AppendPasswordHistory(ctx, user.ID, user.PasswordHash, lifecycle.HistoryKeep); err != nil {
		s.log.Warn("密码历史写入失败", zap.Int64("user_id", user.ID), zap.Error(err))
	}
	analysis := AnalyzePasswordStrengthWithContext(input.NewPassword, pctx)
	if err := s.pg.PatchUserSecurityState(ctx, user.ID, map[string]any{
		"password_changed_at":      changedAt,
		"password_strength_score":  analysis.Score,
		"password_change_required": false,
	}); err != nil {
		s.log.Warn("密码安全状态写入失败", zap.Int64("user_id", user.ID), zap.Error(err))
	}

	keepCurrent := current.Mode == passwordRecoveryModeSession
	keepToken, keepFamily := "", ""
	if keepCurrent {
		keepToken, keepFamily = current.KeepTokenID, current.KeepFamilyID
	}
	signedOut := s.revokeUserCredentials(ctx, user.AppID, user.ID, keepToken, keepFamily, "password_reset")
	if s.loginGuard != nil {
		s.loginGuard.ResetAccount(ctx, user.AppID, user.Account)
	}
	_ = s.sessions.DeleteSecurityStatus(ctx, user.AppID, user.ID)
	_ = s.sessions.DeleteUserProfile(ctx, user.AppID, user.ID)
	_ = s.sessions.DeleteMyView(ctx, user.AppID, user.ID)
	if s.publisher != nil {
		_ = s.publisher.PublishJSON(ctx, event.SubjectSessionAuditRequested, map[string]any{
			"user_id":    user.ID,
			"appid":      user.AppID,
			"token_jti":  keepToken,
			"event_type": "password_reset",
			"ip":         input.IP,
			"device_id":  input.DeviceID,
			"user_agent": input.UserAgent,
			"provider":   current.Mode,
			"changed_at": changedAt.Format(time.RFC3339),
		})
	}
	s.notifyPasswordReset(user, current, input, changedAt, signedOut)

	return &PasswordRecoveryCompleteResult{
		Account:            user.Account,
		SignedOut:          signedOut,
		KeptCurrentSession: keepCurrent,
	}, nil
}

// revokeUserCredentials 吊销访问会话与刷新令牌（keep 指定的那一组除外）并撤销第三方授权，
// 返回下线的登录数。
//
// 刷新令牌必须单独吊销：只删访问会话的话，对方拿着刷新令牌换一对新令牌就又回来了。
func (s *AuthService) revokeUserCredentials(ctx context.Context, appID, userID int64, keepTokenID, keepFamilyID, eventType string) int {
	keep := func(tokenID, familyID string) bool {
		if keepTokenID != "" && tokenID == keepTokenID {
			return true
		}
		return keepFamilyID != "" && familyID == keepFamilyID
	}
	families := map[string]struct{}{}
	signedOut := 0
	if items, err := s.sessions.ListUserSessions(ctx, appID, userID); err != nil {
		s.log.Warn("list sessions for password reset failed", zap.Int64("userId", userID), zap.Error(err))
	} else {
		for _, item := range items {
			if keep(item.Session.TokenID, item.Session.RefreshFamilyID) {
				continue
			}
			_ = s.sessions.DeleteSessionByHash(ctx, appID, userID, item.TokenHash)
			if ttl := time.Until(item.Session.ExpiresAt); ttl > 0 {
				_ = s.sessions.BlacklistToken(ctx, item.Session.TokenID, ttl)
			}
			if item.Session.RefreshFamilyID != "" {
				families[item.Session.RefreshFamilyID] = struct{}{}
			} else {
				signedOut++
			}
			if s.publisher != nil {
				_ = s.publisher.PublishJSON(ctx, event.SubjectSessionAuditRequested, map[string]any{
					"user_id": userID, "appid": appID, "token_jti": item.Session.TokenID,
					"event_type": "revoked_by_" + eventType, "ip": item.Session.IP,
					"device_id": item.Session.DeviceID, "user_agent": item.Session.UserAgent,
					"provider": item.Session.Provider,
				})
			}
		}
	}
	if items, err := s.sessions.ListIndexedRefreshSessions(ctx, appID, userID); err != nil {
		s.log.Warn("list refresh sessions for password reset failed", zap.Int64("userId", userID), zap.Error(err))
	} else {
		for _, item := range items {
			if keep(item.Session.TokenID, item.Session.FamilyID) {
				continue
			}
			_ = s.sessions.DeleteRefreshSessionByHash(ctx, appID, userID, item.TokenHash)
			if ttl := time.Until(item.Session.ExpiresAt); ttl > 0 {
				_ = s.sessions.BlacklistToken(ctx, item.Session.TokenID, ttl)
			}
			if item.Session.FamilyID != "" {
				families[item.Session.FamilyID] = struct{}{}
			}
		}
	}
	for family := range families {
		_ = s.sessions.RevokeRefreshFamily(ctx, appID, userID, family, s.cfg.JWT.RefreshTTL)
	}
	if s.recoveryUsers != nil && s.recoveryUsers.oauth2 != nil {
		s.recoveryUsers.oauth2.RevokeUserEverywhere(appID, userID)
	}
	return signedOut + len(families)
}

// notifyPasswordReset 站内信 + 绑定邮箱告知，都不阻塞响应。
func (s *AuthService) notifyPasswordReset(user *userdomain.User, flow *redisrepo.PasswordRecoveryFlow, input PasswordRecoveryCompleteInput, at time.Time, signedOut int) {
	appID, userID := user.AppID, user.ID
	var email string
	for _, item := range flow.Channels {
		if item.Type == recoveryFactorEmail {
			email = item.Value
		}
	}
	device := strings.TrimSpace(input.Device)
	s.runDetached("password_reset.notify", 20*time.Second, func(ctx context.Context) {
		content := "登录密码已通过找回流程重新设置。"
		if signedOut > 0 {
			content = fmt.Sprintf("登录密码已通过找回流程重新设置，%d 个登录中的设备已退出。", signedOut)
		}
		if s.notifications != nil {
			if _, err := s.notifications.Deliver(ctx, notificationdomain.DeliverInput{
				AppID: appID, UserID: userID, Type: "security", Title: "密码已重置",
				Content:  content + "如非本人操作，请立即再次找回密码。",
				Level:    "warning",
				Metadata: map[string]any{"module": "security", "event": "password_reset", "ip": input.IP},
				Action:   notificationdomain.RouteAction(notificationdomain.RouteSecurity, "查看账号安全"),
			}); err != nil {
				s.log.Warn("password reset notification failed", zap.Int64("userId", userID), zap.Error(err))
			}
		}
		if email == "" {
			// 冷静期里的邮箱没进流程，但它同样是这个账号的；告知信照发
			if profile, err := s.pg.GetUserProfileByUserID(ctx, userID); err == nil && profile != nil {
				email = strings.TrimSpace(profile.Email)
			}
		}
		if email == "" || s.recoveryEmail == nil {
			return
		}
		if err := s.recoveryEmail.SendPasswordResetNotice(ctx, appID, email, PasswordResetNotice{
			Account: user.Account, At: at, IP: input.IP, Location: input.Location,
			Device: device, SignedOut: signedOut,
		}); err != nil {
			s.log.Warn("password reset notice email failed", zap.Int64("userId", userID), zap.Error(err))
		}
	})
}

// loadRecoveryFlow 按令牌取流程并核对应用与设备。
func (s *AuthService) loadRecoveryFlow(ctx context.Context, input PasswordRecoveryStepInput) (*redisrepo.PasswordRecoveryFlow, string, error) {
	token := strings.TrimSpace(input.FlowToken)
	if !qrLoginTicketPattern.MatchString(token) {
		return nil, "", apperrors.New(errCodeRecoveryFlowInvalid, http.StatusGone, "找回流程已失效，请重新开始")
	}
	hash := hashPollToken(token)
	flow, err := s.sessions.GetPasswordRecoveryFlow(ctx, input.AppID, hash)
	if err != nil {
		return nil, "", err
	}
	if flow == nil || flow.AppID != input.AppID {
		return nil, "", apperrors.New(errCodeRecoveryFlowInvalid, http.StatusGone, "找回流程已失效，请重新开始")
	}
	if flow.DeviceID != "" && strings.TrimSpace(input.DeviceID) != flow.DeviceID {
		return nil, "", apperrors.New(errCodeRecoveryDeviceMismatch, http.StatusForbidden, "请在发起找回的设备上继续操作")
	}
	return flow, hash, nil
}

func (s *AuthService) hitRecoveryLimit(ctx context.Context, scope, subject string, window time.Duration, limit int64, message string) error {
	n, err := s.sessions.HitPasswordRecoveryCounter(ctx, scope, subject, window)
	if err != nil {
		// 计数器故障不挡人：后面还有逐流程的失败上限与验证码各自的限额
		s.log.Warn("password recovery counter failed", zap.String("scope", scope), zap.Error(err))
		return nil
	}
	if n > limit {
		return apperrors.New(errCodeRecoveryDailyLimit, http.StatusTooManyRequests, message)
	}
	return nil
}

// recordRecoveryFailure 记一次失败；到上限就作废流程并换成「次数过多」。
func recordRecoveryFailure(flow *redisrepo.PasswordRecoveryFlow, cause error) (bool, time.Duration, error) {
	flow.Failures++
	if flow.Failures >= passwordRecoveryMaxFailures {
		return false, 0, apperrors.New(errCodeRecoveryTooManyFailure, http.StatusTooManyRequests, "验证失败次数过多，本次找回已作废")
	}
	return true, 0, cause
}

// countsAsRecoveryFailure 猜错才算失败；服务不可用、验证码过期之类不算。
func countsAsRecoveryFailure(err error) bool {
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) {
		return false
	}
	switch appErr.Code {
	case errCodeRecoveryCodeInvalid, 40108, 40109, 40021, 42903:
		return true
	}
	return false
}

func mapRecoveryStoreError(err error) error {
	switch {
	case errors.Is(err, redisrepo.ErrPasswordRecoveryFlowMissing):
		return apperrors.New(errCodeRecoveryFlowInvalid, http.StatusGone, "找回流程已失效，请重新开始")
	case errors.Is(err, redisrepo.ErrPasswordRecoveryFlowBusy):
		return apperrors.New(42907, http.StatusTooManyRequests, "操作过于频繁，请稍后再试")
	}
	return err
}

func findRecoveryChannel(flow *redisrepo.PasswordRecoveryFlow, kind string) *redisrepo.PasswordRecoveryChannel {
	for i := range flow.Channels {
		if flow.Channels[i].Type == kind {
			return &flow.Channels[i]
		}
	}
	return nil
}

// recoveryFactorPending 这种方式能否满足某一项尚未满足的要求。
func recoveryFactorPending(flow *redisrepo.PasswordRecoveryFlow, factor string) bool {
	for _, req := range flow.Requirements {
		if req.By != "" {
			continue
		}
		for _, option := range req.Any {
			if option == factor {
				return true
			}
		}
	}
	return false
}

// satisfyRecoveryRequirement 把第一项能被 slot 满足的要求记为已满足。
// 优先记到选项最少的那一项上：两项分别是 [email] 与 [email, phone] 时，邮箱要留给只认邮箱的那一项。
func satisfyRecoveryRequirement(flow *redisrepo.PasswordRecoveryFlow, slot, by string) {
	best := -1
	for i, req := range flow.Requirements {
		if req.By != "" {
			continue
		}
		for _, option := range req.Any {
			if option == slot && (best < 0 || len(req.Any) < len(flow.Requirements[best].Any)) {
				best = i
			}
		}
	}
	if best >= 0 {
		flow.Requirements[best].By = by
	}
}

func recoveryFlowSatisfied(flow *redisrepo.PasswordRecoveryFlow) bool {
	for _, req := range flow.Requirements {
		if req.By == "" {
			return false
		}
	}
	return len(flow.Requirements) > 0
}

func recoveryView(flow *redisrepo.PasswordRecoveryFlow) *PasswordRecoveryView {
	view := &PasswordRecoveryView{
		Mode:         flow.Mode,
		Account:      maskRecoveryAccount(flow.Account),
		TOTP:         flow.TOTP,
		Ready:        flow.VerifiedAt != nil,
		AttemptsLeft: max(passwordRecoveryMaxFailures-flow.Failures, 0),
		ExpiresAt:    flow.ExpiresAt,
		Channels:     make([]PasswordRecoveryChannelView, 0, len(flow.Channels)),
		Requirements: make([]PasswordRecoveryRequirementView, 0, len(flow.Requirements)),
	}
	if flow.Mode == passwordRecoveryModeSession {
		view.Account = flow.Account
	}
	for _, item := range flow.Channels {
		channel := PasswordRecoveryChannelView{
			Type: item.Type, Masked: item.Masked, Verified: item.Verified, CodeSent: item.LastSentAt != nil,
		}
		if item.LastSentAt != nil {
			resend := item.LastSentAt.Add(passwordRecoveryResendInterval)
			channel.ResendAt = &resend
		}
		view.Channels = append(view.Channels, channel)
	}
	for _, req := range flow.Requirements {
		view.Requirements = append(view.Requirements, PasswordRecoveryRequirementView{
			Any: append([]string(nil), req.Any...), Satisfied: req.By != "", By: req.By,
		})
	}
	return view
}

// recoveryTargetMatches 用户填的完整联系方式是否就是绑定的那个。
func recoveryTargetMatches(channel, bound, target string) bool {
	target = strings.TrimSpace(target)
	if target == "" {
		return false
	}
	if channel == recoveryFactorEmail {
		return strings.EqualFold(target, strings.TrimSpace(bound))
	}
	digits := func(value string) string {
		value = normalizePhone(value)
		value = strings.TrimPrefix(value, "+86")
		return strings.TrimPrefix(value, "0086")
	}
	return digits(target) == digits(bound)
}

func recoveryChannelNoun(channel string) string {
	if channel == recoveryFactorEmail {
		return "邮箱"
	}
	return "手机号"
}

// recoveryCodeHash 邮箱验证码的摘要，按流程加盐：同一串码换个流程就对不上。
func recoveryCodeHash(flowHash, code string) string {
	sum := sha256.Sum256([]byte(flowHash + ":" + strings.TrimSpace(code)))
	return hex.EncodeToString(sum[:])
}

// maskRecoveryAccount 未登录时只给账号的首尾：凭邮箱找回的人能认出是哪个号，旁人拿不到全名。
func maskRecoveryAccount(account string) string {
	runes := []rune(strings.TrimSpace(account))
	switch {
	case len(runes) == 0:
		return ""
	case len(runes) <= 2:
		return string(runes[:1]) + "***"
	case looksLikePhone(string(runes)):
		return maskPhoneValue(string(runes))
	case strings.Contains(string(runes), "@"):
		return maskEmail(string(runes))
	default:
		return string(runes[:1]) + "***" + string(runes[len(runes)-1:])
	}
}

func looksLikePhone(value string) bool {
	value = strings.TrimPrefix(normalizePhone(value), "+")
	if len(value) < 6 || len(value) > 16 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func humanizeHours(d time.Duration) string {
	hours := int(d.Hours() + 0.999)
	if hours <= 1 {
		return "1 小时"
	}
	return fmt.Sprintf("%d 小时", hours)
}
