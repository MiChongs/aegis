package service

import (
	"aegis/internal/domain/accountdeletion"
	"context"
	"net/http"
	"strings"
	"time"

	appdomain "aegis/internal/domain/app"
	authdomain "aegis/internal/domain/auth"
	oauthdomain "aegis/internal/domain/oauth"
	plugindomain "aegis/internal/domain/plugin"
	"aegis/internal/domain/regip"
	userdomain "aegis/internal/domain/user"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"

	"github.com/google/uuid"
)

// 原生 SDK 第三方登录（QQ 互联 Android / iOS SDK 等）。
//
// 端上 SDK 授权后只交来 access_token（微信、微博另带 openid / uid），身份由服务端拿令牌
// 向服务商换回并核对令牌签给本应用（ProfileFromAccessToken），客户端自报的身份一概不作数。
//
// 身份核实后：
//   - 已绑定 → 直接登录；
//   - 未绑定、客户端带了 deferRegistration → 返回一张一次性关联凭证（LoginResult.OAuthLink），
//     用户选「快速注册」走 /auth/oauth/link/register，选「绑定已有账号」就先用已有账号
//     正常登录（含二次认证），再凭凭证调 /auth/oauth/link/bind；
//   - 未绑定、老客户端 → 沿用渠道 allowRegister 直接建号，不允许就报 40393。
//
// 已登录用户在设置里绑定走 /auth/oauth/bind/token：核实令牌后直接挂到当前账号。

const (
	// errCodeOAuthTokenInvalid 第三方令牌无效、已过期、不属于本应用，或与客户端声称的身份不一致
	errCodeOAuthTokenInvalid = 40380
	// errCodeOAuthLinkTicketInvalid 关联凭证不存在、已用过或已过期
	errCodeOAuthLinkTicketInvalid = 40495
	// errCodeOAuthIdentityTaken 该第三方账号已绑定本应用的其他账号
	errCodeOAuthIdentityTaken = 40950
	// errCodeOAuthProviderBound 当前账号在该渠道已绑定了另一个第三方账号
	errCodeOAuthProviderBound = 40953

	// oauthLinkTicketTTL 关联凭证有效期：要覆盖「选绑定 → 输密码 → 过二次认证」这一整段
	oauthLinkTicketTTL = 10 * time.Minute

	// AuthStateOAuthLinkRequired LoginResult.AuthenticationState：第三方身份待关联
	AuthStateOAuthLinkRequired = "oauth_link_required"
)

// NativeOAuthLoginInput 原生 SDK 换会话的入参。
type NativeOAuthLoginInput struct {
	AppID       int64
	Provider    string
	AccessToken string
	// OpenID 客户端声称的第三方 ID。QQ 不需要（服务端反查），微信、微博必填；传了就要与核实结果一致
	OpenID    string
	DeviceID  string
	Device    string
	IP        string
	UserAgent string
	// AllowRegister 调用方叠加的建号开关，与渠道 allowRegister 取与
	AllowRegister bool
	// DeferRegistration 客户端能让用户在「快速注册」与「绑定已有账号」之间选择：
	// 未绑定时返回关联凭证，而不是直接建号
	DeferRegistration bool
}

// OAuthLinkRegisterInput 凭关联凭证快速注册。
type OAuthLinkRegisterInput struct {
	AppID     int64
	Ticket    string
	DeviceID  string
	Device    string
	IP        string
	UserAgent string
}

// NativeOAuthLogin 原生 SDK 拿到的第三方令牌换 Aegis 会话。
func (s *AuthService) NativeOAuthLogin(ctx context.Context, input NativeOAuthLoginInput) (*authdomain.LoginResult, error) {
	p, resolved, err := s.resolveOAuthProvider(ctx, input.AppID, input.Provider)
	if err != nil {
		return nil, err
	}
	if !resolved.AllowLogin {
		return nil, apperrors.New(40391, http.StatusForbidden, "该渠道仅开放账号绑定，未开放直接登录")
	}
	app, err := s.ensureOAuthLoginAllowed(ctx, input.AppID, input.DeviceID, input.Device)
	if err != nil {
		return nil, err
	}
	profile, err := p.ProfileFromAccessToken(ctx, s.http, input.AccessToken, input.OpenID)
	if err != nil {
		return nil, err
	}
	profile.Provider = resolved.Provider
	allowRegister := resolved.AllowRegister && input.AllowRegister
	// 渠道不开放绑定时没有可选的，照旧直接建号或报 40393
	if input.DeferRegistration && resolved.AllowBind {
		boundUserID, err := s.pg.FindOAuthBinding(ctx, input.AppID, profile.Provider, profile.ProviderUserID)
		if err != nil {
			return nil, err
		}
		if boundUserID <= 0 {
			return s.issueOAuthLinkTicket(ctx, input.AppID, resolved, profile, input.DeviceID, allowRegister)
		}
	}
	return s.loginWithOAuthProfile(ctx, app, input.AppID, profile, input.DeviceID, input.Device,
		input.IP, input.UserAgent, "oauth_mobile", allowRegister)
}

// RegisterWithOAuthLink 用户选了「快速注册」：以关联凭证里的第三方身份建号并登录。
func (s *AuthService) RegisterWithOAuthLink(ctx context.Context, input OAuthLinkRegisterInput) (*authdomain.LoginResult, error) {
	payload, err := s.consumeOAuthLinkTicket(ctx, input.AppID, input.Ticket)
	if err != nil {
		return nil, err
	}
	_, resolved, err := s.resolveOAuthProvider(ctx, input.AppID, payload.Profile.Provider)
	if err != nil {
		return nil, err
	}
	if !resolved.AllowLogin {
		return nil, apperrors.New(40391, http.StatusForbidden, "该渠道仅开放账号绑定，未开放直接登录")
	}
	if !payload.AllowRegister || !resolved.AllowRegister {
		return nil, apperrors.New(40393, http.StatusForbidden,
			"该第三方账号尚未绑定，且当前渠道未开放注册，请先用已有账号登录后绑定")
	}
	deviceID := strings.TrimSpace(input.DeviceID)
	if deviceID == "" {
		deviceID = payload.DeviceID
	}
	app, err := s.ensureOAuthLoginAllowed(ctx, input.AppID, deviceID, input.Device)
	if err != nil {
		return nil, err
	}
	// 凭证签发后这个身份可能已被绑定（另一台设备上抢先注册）：loginWithOAuthProfile
	// 会先查绑定，查到就登录那个账号而不是再建一个
	return s.loginWithOAuthProfile(ctx, app, input.AppID, payload.Profile, deviceID, input.Device,
		input.IP, input.UserAgent, "oauth_register", true)
}

// BindOAuthLink 用户选了「绑定已有账号」并已登录：把关联凭证里的第三方身份挂到当前账号。
func (s *AuthService) BindOAuthLink(ctx context.Context, appID, userID int64, ticket string) (*oauthdomain.Binding, error) {
	if userID <= 0 {
		return nil, apperrors.New(40100, http.StatusUnauthorized, "请先登录")
	}
	payload, err := s.consumeOAuthLinkTicket(ctx, appID, ticket)
	if err != nil {
		return nil, err
	}
	_, resolved, err := s.resolveOAuthProvider(ctx, appID, payload.Profile.Provider)
	if err != nil {
		return nil, err
	}
	if !resolved.AllowBind {
		return nil, apperrors.New(40392, http.StatusForbidden, "该渠道未开放账号绑定")
	}
	return s.bindOAuthProfile(ctx, appID, userID, payload.Profile)
}

// BindOAuthWithToken 已登录用户在设置里绑定：用原生 SDK 拿到的令牌核实身份后挂到当前账号。
func (s *AuthService) BindOAuthWithToken(ctx context.Context, appID, userID int64, provider, accessToken, openID string) (*oauthdomain.Binding, error) {
	if userID <= 0 {
		return nil, apperrors.New(40100, http.StatusUnauthorized, "请先登录")
	}
	p, resolved, err := s.resolveOAuthProvider(ctx, appID, provider)
	if err != nil {
		return nil, err
	}
	if !resolved.AllowBind {
		return nil, apperrors.New(40392, http.StatusForbidden, "该渠道未开放账号绑定")
	}
	profile, err := p.ProfileFromAccessToken(ctx, s.http, accessToken, openID)
	if err != nil {
		return nil, err
	}
	profile.Provider = resolved.Provider
	return s.bindOAuthProfile(ctx, appID, userID, profile)
}

// ensureOAuthLoginAllowed 应用登录开关与设备策略。未注入 AppService（单测、离线导出）时不判定。
func (s *AuthService) ensureOAuthLoginAllowed(ctx context.Context, appID int64, deviceID, device string) (*appdomain.App, error) {
	if s.app == nil {
		return nil, nil
	}
	app, err := s.app.EnsureLoginAllowed(ctx, appID)
	if err != nil {
		return nil, err
	}
	if err := s.validateLoginPolicy(app, deviceID, device); err != nil {
		return nil, err
	}
	return app, nil
}

// issueOAuthLinkTicket 第三方身份已核实但未绑定：存下身份，发一张一次性凭证。
//
// AllowRegister 在这一刻就算好（渠道开关 ∧ 应用注册开关 ∧ 平台治理），
// 客户端据此决定显不显示「快速注册」，免得用户点了才知道不行。
func (s *AuthService) issueOAuthLinkTicket(ctx context.Context, appID int64, resolved *oauthdomain.Resolved, profile authdomain.ProviderProfile, deviceID string, allowRegister bool) (*authdomain.LoginResult, error) {
	if s.sessions == nil {
		return nil, apperrors.New(50390, http.StatusServiceUnavailable, "第三方登录服务不可用")
	}
	if allowRegister && s.app != nil {
		if _, err := s.app.EnsureRegisterAllowed(ctx, appID); err != nil {
			allowRegister = false
		}
	}
	ticket := uuid.NewString()
	expiresAt := timeutil.NowUTC().Add(oauthLinkTicketTTL)
	if err := s.sessions.SetOAuthLinkTicket(ctx, ticket, authdomain.OAuthLinkPayload{
		AppID: appID, Profile: profile, DeviceID: deviceID, AllowRegister: allowRegister,
	}, oauthLinkTicketTTL); err != nil {
		return nil, err
	}
	displayName := strings.TrimSpace(resolved.DisplayName)
	if displayName == "" {
		displayName = resolved.Provider
	}
	return &authdomain.LoginResult{
		Provider:            profile.Provider,
		AuthenticationState: AuthStateOAuthLinkRequired,
		OAuthLink: &authdomain.OAuthLinkTicket{
			Ticket:        ticket,
			Provider:      profile.Provider,
			DisplayName:   displayName,
			Nickname:      profile.Nickname,
			Avatar:        SanitizeExternalAvatar(profile.Avatar),
			AllowRegister: allowRegister,
			AllowBind:     true,
			ExpiresAt:     expiresAt,
		},
	}, nil
}

// consumeOAuthLinkTicket 取出并作废关联凭证。凭证只能用一次，跨应用使用视同无效。
func (s *AuthService) consumeOAuthLinkTicket(ctx context.Context, appID int64, ticket string) (*authdomain.OAuthLinkPayload, error) {
	ticket = strings.TrimSpace(ticket)
	if ticket == "" || s.sessions == nil {
		return nil, apperrors.New(errCodeOAuthLinkTicketInvalid, http.StatusNotFound, "第三方授权已过期，请重新授权")
	}
	var payload authdomain.OAuthLinkPayload
	found, err := s.sessions.ConsumeOAuthLinkTicket(ctx, ticket, &payload)
	if err != nil {
		return nil, err
	}
	if !found || payload.AppID != appID || payload.Profile.ProviderUserID == "" || payload.Profile.Provider == "" {
		return nil, apperrors.New(errCodeOAuthLinkTicketInvalid, http.StatusNotFound, "第三方授权已过期，请重新授权")
	}
	return &payload, nil
}

// createOAuthUser 第三方身份首次登录时建号。
//
// 与密码、短信注册过同样的闸门（应用注册开关、平台治理、注册 IP 策略）。
// 账号名取「渠道_第三方ID」；撞名（例如曾被管理员强制解绑后再次登录）时加随机后缀。
// 用户与档案在同一个事务里落库，邀请码撞码换码重试。
func (s *AuthService) createOAuthUser(ctx context.Context, appID int64, profile authdomain.ProviderProfile, deviceID, device, ip, userAgent string) (user *userdomain.User, err error) {
	if s.app != nil {
		app, appErr := s.app.EnsureRegisterAllowed(ctx, appID)
		if appErr != nil {
			return nil, appErr
		}
		ticket, admitErr := s.admitRegister(ctx, app, regip.MethodOAuth, ip, pgrepo.LegacyOAuthAccount(profile.Provider, profile.ProviderUserID), deviceID, userAgent)
		if admitErr != nil {
			return nil, admitErr
		}
		// 建号成功确认占位，任何一条失败路径都撤销
		defer func() {
			if err == nil && user != nil {
				ticket.Commit(user.ID, user.Account)
			} else {
				ticket.Release()
			}
		}()
	}
	now := timeutil.NowUTC()
	userProfile := userdomain.Profile{
		Nickname: strings.TrimSpace(profile.Nickname),
		// 头像来自第三方资料，过一道闸门：只收外部 http(s) 地址
		Avatar: SanitizeExternalAvatar(profile.Avatar),
		Email:  strings.TrimSpace(profile.Email),
		Extra: map[string]any{
			"provider":            profile.Provider,
			"register_ip":         ip,
			"register_user_agent": userAgent,
			"register_method":     "oauth",
		},
	}
	base := pgrepo.LegacyOAuthAccount(profile.Provider, profile.ProviderUserID)
	if err := s.checkReregister(ctx, appID,
		RegisterIdentifier{Kind: accountdeletion.TombstoneAccount, Value: base},
		RegisterIdentifier{Kind: accountdeletion.TombstoneEmail, Value: profile.Email},
	); err != nil {
		return nil, err
	}
	account := base
	for attempt := 0; attempt < inviteCodeMaxAttempts && user == nil; attempt++ {
		inviteCode, err := randomInviteCode(inviteCodeLength)
		if err != nil {
			return nil, err
		}
		userProfile.InviteCode = inviteCode
		created, err := s.pg.CreateUserWithProfile(ctx, appID, account, "", userProfile, userdomain.ProfileSecurityState{})
		switch {
		case err == nil:
			user = created
		case err == pgrepo.ErrAccountAlreadyExists:
			suffix, suffixErr := randomInviteCode(6)
			if suffixErr != nil {
				return nil, suffixErr
			}
			account = truncateBytes(base, 121) + "_" + strings.ToLower(suffix)
		case pgrepo.IsUniqueViolation(err):
			// 邀请码撞码：换码重试
		default:
			return nil, err
		}
	}
	if user == nil {
		return nil, apperrors.New(50000, http.StatusInternalServerError, "创建账号失败，请重试")
	}

	userID := user.ID
	s.trackAccountSetup(ctx, appID, userID, appdomain.AccountSetupSourceOAuth, profile.Provider)
	s.runDetached("register.oauth.audit", 3*time.Second, func(actx context.Context) {
		_ = s.pg.InsertLoginAudit(actx, appID, userID, "register", profile.Provider, "",
			ip, deviceID, userAgent, "success", map[string]any{
				"account": user.Account,
				"device":  device,
			})
	})
	s.recordFirstDevice(ctx, authdomain.FirstDevice{
		UserID: userID, AppID: appID, DeviceID: deviceID, Device: device,
		IP: ip, UserAgent: userAgent, Provider: profile.Provider,
		Scene: "register", FirstSeenAt: now,
	})
	if s.plugin != nil {
		account := user.Account
		s.runDetached("register.oauth.hook", 5*time.Second, func(actx context.Context) {
			s.plugin.ExecuteHook(actx, HookUserRegistered, map[string]any{
				"userId": userID, "account": account, "appId": appID, "method": "oauth", "provider": profile.Provider,
			}, plugindomain.HookMetadata{IP: ip, AppID: &appID, UserID: &userID})
		})
	}
	return user, nil
}

// fillOAuthProfileBlanks 已有账号经第三方登录：只补空着的昵称与头像。
//
// 用户自己改过的资料、绑定的邮箱手机号都不能被第三方资料覆盖 ——
// 档案写入是整行 upsert，这里必须以现有档案为底。
func (s *AuthService) fillOAuthProfileBlanks(ctx context.Context, userID int64, profile authdomain.ProviderProfile) error {
	nickname := strings.TrimSpace(profile.Nickname)
	avatar := SanitizeExternalAvatar(profile.Avatar)
	existing, err := s.pg.GetUserProfileByUserID(ctx, userID)
	if err != nil {
		return err
	}
	if existing == nil {
		_, err := s.upsertUserProfileWithInviteCode(ctx, userdomain.Profile{
			UserID: userID, Nickname: nickname, Avatar: avatar, Email: strings.TrimSpace(profile.Email),
			Extra: map[string]any{"provider": profile.Provider},
		})
		return err
	}
	updated := *existing
	changed := false
	if strings.TrimSpace(updated.Nickname) == "" && nickname != "" {
		updated.Nickname = nickname
		changed = true
	}
	if strings.TrimSpace(updated.Avatar) == "" && avatar != "" {
		updated.Avatar = avatar
		changed = true
	}
	if !changed {
		return nil
	}
	_, err = s.upsertUserProfileWithInviteCode(ctx, updated)
	return err
}

// truncateBytes 按字节截断但不切开 UTF-8 字符。
func truncateBytes(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	cut := limit
	for cut > 0 && (value[cut]&0xC0) == 0x80 {
		cut--
	}
	return value[:cut]
}
