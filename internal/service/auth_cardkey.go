package service

import (
	"context"
	"net/http"
	"strings"
	"time"

	appdomain "aegis/internal/domain/app"
	authdomain "aegis/internal/domain/auth"
	cardkeydomain "aegis/internal/domain/cardkey"
	plugindomain "aegis/internal/domain/plugin"
	userdomain "aegis/internal/domain/user"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
)

// 卡密登录（软件授权码那一档）。
//
// 与短信登录在结构上是同一件事：用一个**外部已验证的凭据**换取会话，而不是校验本地密码。
// 因此复用同一副骨架：查卡 → 没绑过就建号 → 绑卡绑设备 → completeLogin。
//
// 与短信的两处不同，都源自「卡即账号」：
//
//  1. **账号名由卡面派生**（`CardKeyAccount`），因此建号是确定性的。
//     用随机账号名的话，同一张卡的两次并发首登会造出两个账号，其中一个成为孤儿；
//     用卡面则第二次会撞唯一约束，这里据此回读同一个用户。
//  2. **没有 AllowRegister 开关**。卡是运营发出去的，发出去就意味着允许它建号；
//     再加一个开关会造出「卡有效但登不进去」这种没人解释得清的状态。
//     真要停发就停用批次或作废卡，那两个动作的语义是明确的。

// CardKeyLoginInput 卡密登录入参。
type CardKeyLoginInput struct {
	AppID     int64
	Code      string
	DeviceID  string
	Device    string
	IP        string
	UserAgent string
}

// CardKeyLoginResult 登录结果 + 这张卡的授权快照。
//
// 授权快照必须随登录一起下发：客户端要靠它显示「授权剩余 12 天 / 已用 2 台设备」，
// 而那正是用户买了卡之后最想确认的两件事。让客户端再打一次接口去问，
// 意味着「登录成功但授权信息拿不到」是一种可能的状态。
type CardKeyLoginResult struct {
	Login         *authdomain.LoginResult
	Authorization *cardkeydomain.LoginAuthorization
}

// CardKeyLogin 用一张授权卡登录；卡未绑定时自动建号并绑定。
func (s *AuthService) CardKeyLogin(ctx context.Context, input CardKeyLoginInput) (*CardKeyLoginResult, error) {
	if s.cardKey == nil {
		return nil, apperrors.New(40370, http.StatusForbidden, "当前部署未启用卡密登录")
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

	card, err := s.cardKey.PrepareLogin(ctx, input.AppID, input.Code)
	if err != nil {
		return nil, err
	}

	user, registered, err := s.resolveCardKeyUser(ctx, input, card)
	if err != nil {
		return nil, err
	}

	authorization, err := s.cardKey.Activate(ctx, cardkeydomain.ActivateLoginInput{
		AppID:      input.AppID,
		CardID:     card.ID,
		UserID:     user.ID,
		DeviceID:   input.DeviceID,
		DeviceName: input.Device,
		ClientIP:   input.IP,
		UserAgent:  input.UserAgent,
	})
	if err != nil {
		return nil, err
	}

	if !registered {
		if err := s.ensureUserLoginState(ctx, user); err != nil {
			return nil, err
		}
	}
	s.syncAdminUserSearch(input.AppID, user.ID)

	login, err := s.completeLogin(ctx, app, user, "cardkey", "cardkey",
		input.DeviceID, input.Device, input.IP, input.UserAgent)
	if err != nil {
		return nil, err
	}
	return &CardKeyLoginResult{Login: login, Authorization: authorization}, nil
}

// resolveCardKeyUser 找出这张卡对应的账号，没有就建一个。
func (s *AuthService) resolveCardKeyUser(ctx context.Context, input CardKeyLoginInput,
	card *cardkeydomain.Card) (*userdomain.User, bool, error) {
	if card.BoundUserID != nil {
		user, err := s.pg.GetUserByID(ctx, *card.BoundUserID)
		if err != nil {
			return nil, false, err
		}
		if user == nil {
			// 绑定的账号被删了。卡还在，但它指向一个不存在的人 —— 这种状态
			// 只可能由人工删号造成，如实报错比默默建一个新号安全：
			// 后者会让「删掉的账号」以同一张卡复活，且资产全部归零。
			return nil, false, apperrors.New(errCodeCardKeyBoundOther, http.StatusForbidden,
				"该卡密绑定的账号已被删除，请联系客服")
		}
		return user, false, nil
	}

	account := CardKeyAccount(card.Code)
	if app := s.app; app != nil {
		if _, err := app.EnsureRegisterAllowed(ctx, input.AppID); err != nil {
			return nil, false, err
		}
	}

	user, err := s.createCardKeyUser(ctx, input, account)
	if err != nil {
		return nil, false, err
	}
	if user != nil {
		return user, true, nil
	}

	// 并发首登：账号已被同一张卡的另一个请求建出来了，回读即可。
	existing, err := s.pg.GetUserByAppAndAccount(ctx, input.AppID, account)
	if err != nil {
		return nil, false, err
	}
	if existing == nil {
		return nil, false, apperrors.New(50000, http.StatusInternalServerError, "创建账号失败，请重试")
	}
	return existing, false, nil
}

// createCardKeyUser 以卡面为账号建号；账号已存在时返回 (nil, nil) 交由调用方回读。
//
// 没有密码：这类账号只能靠卡登录。想改用密码登录需要走「设置密码」流程，
// 与短信建出来的账号同一条路径。
func (s *AuthService) createCardKeyUser(ctx context.Context, input CardKeyLoginInput, account string) (*userdomain.User, error) {
	profile := userdomain.Profile{
		Nickname: cardKeyNickname(account),
		Extra: map[string]any{
			"register_ip":         input.IP,
			"register_user_agent": input.UserAgent,
			"register_method":     "cardkey",
		},
	}

	// 与密码 / 短信注册同款：邀请码撞码则换码整体重试，靠唯一约束兜底而非预查询。
	for attempt := 0; attempt < inviteCodeMaxAttempts; attempt++ {
		inviteCode, codeErr := randomInviteCode(inviteCodeLength)
		if codeErr != nil {
			return nil, codeErr
		}
		profile.InviteCode = inviteCode
		created, createErr := s.pg.CreateUserWithProfile(
			ctx, input.AppID, account, "", profile, userdomain.ProfileSecurityState{})
		if createErr == nil {
			s.afterCardKeyRegister(ctx, input, created)
			return created, nil
		}
		if createErr == pgrepo.ErrAccountAlreadyExists {
			return nil, nil
		}
		if pgrepo.IsUniqueViolation(createErr) {
			continue
		}
		return nil, createErr
	}
	return nil, apperrors.New(50000, http.StatusInternalServerError, "创建账号失败，请重试")
}

// afterCardKeyRegister 建号之后的副作用，全部不反噬主链路。
func (s *AuthService) afterCardKeyRegister(ctx context.Context, input CardKeyLoginInput, user *userdomain.User) {
	userID := user.ID
	appID := input.AppID
	account := user.Account

	s.runDetached("register.cardkey.audit", 3*time.Second, func(actx context.Context) {
		_ = s.pg.InsertLoginAudit(actx, appID, userID, "register", "cardkey", "",
			input.IP, input.DeviceID, input.UserAgent, "success", map[string]any{
				"device": input.Device,
			})
	})
	// 注册链路里同步调用（与密码 / 短信注册一致）：异步的话，紧随其后的登录
	// 场景会先落 SetNX 键，首次设备就被记成了「登录」而不是「注册」。
	s.recordFirstDevice(ctx, authdomain.FirstDevice{
		UserID: userID, AppID: appID, DeviceID: input.DeviceID, Device: input.Device,
		IP: input.IP, UserAgent: input.UserAgent, Provider: "cardkey",
		Scene: "register", FirstSeenAt: timeutil.NowUTC(),
	})
	if s.plugin != nil {
		s.runDetached("register.cardkey.hook", 5*time.Second, func(actx context.Context) {
			s.plugin.ExecuteHook(actx, HookUserRegistered, map[string]any{
				"userId": userID, "account": account, "appId": appID, "method": "cardkey",
			}, plugindomain.HookMetadata{IP: input.IP, AppID: &appID, UserID: &userID})
		})
	}
}

// cardKeyNickname 由卡面派生一个可读昵称：取最后一段。
//
// 整张卡面当昵称会让用户列表里每一行都是一长串大写字母，谁也认不出谁；
// 而最后一段既短又足以区分。
func cardKeyNickname(account string) string {
	parts := strings.Split(account, "-")
	last := parts[len(parts)-1]
	if last == "" {
		return "卡密用户"
	}
	return "卡密 " + last
}
