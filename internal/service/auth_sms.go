package service

import (
	"aegis/internal/domain/accountdeletion"
	securitydomain "aegis/internal/domain/security"
	"context"
	"net/http"
	"strings"
	"time"

	appdomain "aegis/internal/domain/app"
	authdomain "aegis/internal/domain/auth"
	plugindomain "aegis/internal/domain/plugin"
	"aegis/internal/domain/regip"
	userdomain "aegis/internal/domain/user"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
)

// 短信验证码登录。
//
// 与 OAuth 在结构上是同一件事：用一个**外部已验证的身份**（手机号 / 第三方账号）
// 换取会话，而不是校验本地密码。因此这里复用 loginWithOAuthProfile 的同款骨架：
//   查绑定 → 没有则按策略自动注册 → ensureUserLoginState → completeLogin
//
// 验证码本身由 CaptchaService 在 Handler 层校验后才会走到这里 ——
// 与密码登录把图形验证码留在 Handler 的分层一致，服务层只负责"这个手机号是谁"。

// SMSLoginInput 短信登录/注册入参。
type SMSLoginInput struct {
	AppID    int64
	Phone    string
	Nickname string
	// Profile 注册时写入 user_profiles.extra 的自定义字段，登录既有账号时忽略。
	Profile map[string]any
	// AllowRegister 手机号未注册时是否自动建号。由应用的 registerMethods 决定。
	AllowRegister bool
	// SuppressSession 注册后不签发令牌，只回用户标识（对应 autoLoginAfterRegister=false）。
	SuppressSession bool
	DeviceID        string
	Device          string
	IP              string
	UserAgent       string
}

// SMSLogin 用已验证的手机号登录；未注册且允许时自动建号。
func (s *AuthService) SMSLogin(ctx context.Context, input SMSLoginInput) (*authdomain.LoginResult, error) {
	phone := normalizePhone(input.Phone)
	if phone == "" {
		return nil, apperrors.New(40005, http.StatusBadRequest, "手机号不能为空")
	}

	var app *appdomain.App
	if s.app != nil {
		loaded, err := s.app.EnsureLoginAllowed(ctx, input.AppID)
		if err != nil {
			return nil, err
		}
		if err := s.validateLoginPolicy(loaded, input.DeviceID, input.Device); err != nil {
			return nil, err
		}
		app = loaded
	}

	user, err := s.findUserByPhone(ctx, input.AppID, phone)
	if err != nil {
		return nil, err
	}

	registered := false
	if user == nil {
		if !input.AllowRegister {
			return nil, apperrors.New(40394, http.StatusForbidden,
				"该手机号尚未注册，且当前应用未开放短信注册")
		}
		if app != nil {
			if _, err := s.app.EnsureRegisterAllowed(ctx, input.AppID); err != nil {
				return nil, err
			}
		}
		ticket, err := s.admitRegister(ctx, app, regip.MethodSMS, input.IP, phone, input.DeviceID, input.UserAgent)
		if err != nil {
			return nil, err
		}
		user, err = s.createPhoneUser(ctx, input, phone)
		if err != nil {
			ticket.Release()
			return nil, err
		}
		ticket.Commit(user.ID, user.Account)
		registered = true
	} else {
		// 既有账号：把手机号补进档案，兼容"先用密码注册、后绑手机"的历史数据
		if err := s.ensureProfilePhone(ctx, user.ID, phone); err != nil {
			return nil, err
		}
	}

	if !registered {
		if err := s.ensureUserLoginState(ctx, user); err != nil {
			return nil, err
		}
		// 风控：短信验证码登录走登录场景（新号走注册准入，已在 admitRegister 里评估）
		uid := user.ID
		if err := s.riskCheck(ctx, securitydomain.RiskEvalRequest{Scene: securitydomain.SceneLogin, AppID: &input.AppID, UserID: &uid,
			Account: user.Account, Phone: phone, IP: input.IP, DeviceID: input.DeviceID, UserAgent: input.UserAgent,
			Extra: map[string]any{"method": "sms"}}, "当前请求被风控拦截，请稍后重试"); err != nil {
			return nil, err
		}
	}
	s.syncAdminUserSearch(input.AppID, user.ID)

	if registered && input.SuppressSession {
		return &authdomain.LoginResult{UserID: user.ID, PublicUserID: user.PublicID, Account: user.Account}, nil
	}
	return s.completeLogin(ctx, app, user, "sms", "sms",
		input.DeviceID, input.Device, input.IP, input.UserAgent)
}

// findUserByPhone 手机号既可能就是账号本身（短信注册），
// 也可能只记在档案里（密码注册后补绑），两条都要查。
func (s *AuthService) findUserByPhone(ctx context.Context, appID int64, phone string) (*userdomain.User, error) {
	user, err := s.pg.GetUserByAppAndAccount(ctx, appID, phone)
	if err != nil {
		return nil, err
	}
	if user != nil {
		return user, nil
	}
	userID, err := s.pg.FindUserIDByProfilePhone(ctx, appID, phone)
	if err != nil {
		return nil, err
	}
	if userID <= 0 {
		return nil, nil
	}
	return s.pg.GetUserByID(ctx, userID)
}

// createPhoneUser 以手机号为账号建号。没有密码：这类账号只能靠短信或后续绑定的方式登录，
// 想用密码登录需要走「设置密码」流程。
func (s *AuthService) createPhoneUser(ctx context.Context, input SMSLoginInput, phone string) (*userdomain.User, error) {
	if err := s.checkReregister(ctx, input.AppID,
		RegisterIdentifier{Kind: accountdeletion.TombstoneAccount, Value: phone},
		RegisterIdentifier{Kind: accountdeletion.TombstonePhone, Value: phone},
	); err != nil {
		return nil, err
	}
	now := timeutil.NowUTC()
	profile := userdomain.Profile{
		Nickname: strings.TrimSpace(input.Nickname),
		Phone:    phone,
		Extra: map[string]any{
			"register_ip":         input.IP,
			"register_user_agent": input.UserAgent,
			"register_method":     "sms",
		},
	}
	for key, value := range input.Profile {
		key = strings.TrimSpace(key)
		if key == "" || strings.HasPrefix(strings.ToLower(key), "register_") {
			continue
		}
		profile.Extra[key] = value
	}

	// 与密码注册同款：邀请码撞码则换码整体重试，靠唯一约束兜底而非预查询。
	var user *userdomain.User
	for attempt := 0; attempt < inviteCodeMaxAttempts; attempt++ {
		inviteCode, codeErr := randomInviteCode(inviteCodeLength)
		if codeErr != nil {
			return nil, codeErr
		}
		profile.InviteCode = inviteCode
		created, createErr := s.pg.CreateUserWithProfile(
			ctx, input.AppID, phone, "", profile, userdomain.ProfileSecurityState{})
		if createErr == nil {
			user = created
			break
		}
		if createErr == pgrepo.ErrAccountAlreadyExists {
			return nil, apperrors.New(40901, http.StatusConflict, "该手机号已注册")
		}
		if pgrepo.IsUniqueViolation(createErr) {
			continue
		}
		return nil, createErr
	}
	if user == nil {
		return nil, apperrors.New(50000, http.StatusInternalServerError, "创建账号失败，请重试")
	}

	userID := user.ID
	appID := input.AppID
	s.trackAccountSetup(ctx, appID, userID, appdomain.AccountSetupSourceSMS, "")
	s.runDetached("register.sms.audit", 3*time.Second, func(actx context.Context) {
		_ = s.pg.InsertLoginAudit(actx, appID, userID, "register", "sms", "",
			input.IP, input.DeviceID, input.UserAgent, "success", map[string]any{
				"phone":  phone,
				"device": input.Device,
			})
	})
	s.recordFirstDevice(ctx, authdomain.FirstDevice{
		UserID: userID, AppID: appID, DeviceID: input.DeviceID, Device: input.Device,
		IP: input.IP, UserAgent: input.UserAgent, Provider: "sms",
		Scene: "register", FirstSeenAt: now,
	})
	if s.plugin != nil {
		s.runDetached("register.sms.hook", 5*time.Second, func(actx context.Context) {
			s.plugin.ExecuteHook(actx, HookUserRegistered, map[string]any{
				"userId": userID, "account": phone, "appId": appID, "method": "sms",
			}, plugindomain.HookMetadata{IP: input.IP, AppID: &appID, UserID: &userID})
		})
	}
	return user, nil
}

// ensureProfilePhone 幂等地把手机号补进既有档案；已经一致就不写库。
func (s *AuthService) ensureProfilePhone(ctx context.Context, userID int64, phone string) error {
	existing, err := s.pg.GetUserProfileByUserID(ctx, userID)
	if err != nil {
		return err
	}
	if existing != nil && normalizePhone(existing.Phone) == phone {
		return nil
	}
	profile := userdomain.Profile{UserID: userID, Phone: phone}
	if existing != nil {
		profile = *existing
		profile.Phone = phone
	}
	_, err = s.upsertUserProfileWithInviteCode(ctx, profile)
	return err
}

// normalizePhone 去掉空白与常见分隔符；不做号段校验，各国格式差异交给短信服务商。
func normalizePhone(phone string) string {
	replacer := strings.NewReplacer(" ", "", "-", "", "(", "", ")", "")
	return replacer.Replace(strings.TrimSpace(phone))
}
