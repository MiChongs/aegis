package service

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	appdomain "aegis/internal/domain/app"
	authdomain "aegis/internal/domain/auth"
	platformdomain "aegis/internal/domain/platform"
	userdomain "aegis/internal/domain/user"
	apperrors "aegis/pkg/errors"
)

// 授权服务器（Ory Hydra）登录页使用的认证入口。
//
// 与 App 网关登录的区别只有一处：这里**不签发 Aegis 会话**。认人这一步完全复用
// authenticatePassword / 二次认证挑战 / 登录一致性校验，认完把用户交还给
// OAuth2ServerService，由它告诉 Hydra「这是谁」，令牌由 Hydra 签发。

// oauth2LoginType 写进登录审计与二次认证挑战的登录类型，用来在审计里区分授权登录。
const oauth2LoginType = "oauth2_authorize"

// OAuth2Authentication 登录页认证一步的结果：要么认出了用户，要么需要二次认证。
type OAuth2Authentication struct {
	User      *userdomain.User
	Challenge *authdomain.SecondFactorChallenge
	// AMR 认证方式（RFC 8176），写进 ID Token：pwd / otp
	AMR []string
}

// AuthenticatePasswordForOAuth2 校验账号密码；开启了两步验证的账号返回挑战，由页面继续收验证码。
func (s *AuthService) AuthenticatePasswordForOAuth2(ctx context.Context, appID int64, account, password, deviceID, device, ip, userAgent string) (*OAuth2Authentication, error) {
	app, user, account, err := s.authenticatePassword(ctx, appID, account, password, deviceID, device, ip, userAgent)
	if err != nil {
		return nil, err
	}
	if s.security != nil {
		pending, err := s.security.MaybeCreateSecondFactorChallenge(ctx, user, "password", oauth2LoginType, deviceID, ip, userAgent)
		if err != nil {
			return nil, err
		}
		if pending != nil && pending.Challenge != nil {
			return &OAuth2Authentication{Challenge: pending.Challenge}, nil
		}
	}
	if err := s.enforceLoginConsistency(ctx, app, user, deviceID, ip); err != nil {
		return nil, err
	}
	s.afterPasswordLoginSuccess(appID, user, account, ip)
	s.recordOAuth2Login(appID, user.ID, "password", deviceID, ip, userAgent)
	return &OAuth2Authentication{User: user, AMR: []string{"pwd"}}, nil
}

// VerifySecondFactorForOAuth2 完成二次认证。挑战必须属于 appID 这个应用 ——
// 否则一个应用的登录页可以拿另一个应用的挑战来认人。
func (s *AuthService) VerifySecondFactorForOAuth2(ctx context.Context, appID int64, challengeID, code, recoveryCode string) (*OAuth2Authentication, error) {
	if s.security == nil {
		return nil, apperrors.New(50321, http.StatusServiceUnavailable, "双因子认证模块未启用")
	}
	user, challenge, err := s.security.VerifySecondFactorChallenge(ctx, challengeID, code, recoveryCode)
	if err != nil {
		return nil, err
	}
	if user.AppID != appID || challenge.LoginType != oauth2LoginType {
		return nil, apperrors.New(40056, http.StatusBadRequest, "二次认证挑战不存在或已过期")
	}
	if err := s.ensureUserLoginState(ctx, user); err != nil {
		return nil, err
	}
	var app *appdomain.App
	if s.app != nil {
		if app, err = s.app.EnsureLoginAllowed(ctx, appID); err != nil {
			return nil, err
		}
	}
	if err := s.enforceLoginConsistency(ctx, app, user, challenge.DeviceID, challenge.IP); err != nil {
		return nil, err
	}
	s.afterPasswordLoginSuccess(appID, user, user.Account, challenge.IP)
	s.recordOAuth2Login(appID, user.ID, "password+mfa", challenge.DeviceID, challenge.IP, challenge.UserAgent)
	method := "otp"
	if strings.TrimSpace(recoveryCode) != "" {
		method = "kba"
	}
	return &OAuth2Authentication{User: user, AMR: []string{"pwd", method}}, nil
}

// ResolveOAuth2Subject 把 Hydra 的 subject（即用户 ID）解析回用户，并重新判定此刻还能不能登录：
// 用户属于该应用、账号未被封禁冻结、应用未被停用或治理冻结。
//
// Hydra 记住登录态（skip=true）、记住同意、刷新令牌时都不会再来问 Aegis 密码，
// 这里就是那几条路径上唯一的状态关卡。refresh 决定应用侧判定用哪一档：
// 新的登录要求应用开放登录，刷新只要求应用未停用、接口能力未被冻结（与 Refresh 一致）。
func (s *AuthService) ResolveOAuth2Subject(ctx context.Context, appID int64, subject string, refresh bool) (*userdomain.User, error) {
	userID, err := strconv.ParseInt(strings.TrimSpace(subject), 10, 64)
	if err != nil || userID <= 0 {
		return nil, apperrors.New(40103, http.StatusUnauthorized, "会话用户不存在")
	}
	user, err := s.pg.GetUserByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil || user.AppID != appID {
		return nil, apperrors.New(40103, http.StatusUnauthorized, "会话用户不存在")
	}
	if err := s.ensureUserLoginState(ctx, user); err != nil {
		return nil, err
	}
	if refresh {
		if s.governance != nil {
			if err := s.governance.EnsureCapability(appID, platformdomain.CapabilityAPI); err != nil {
				return nil, err
			}
		}
		if s.app != nil {
			app, err := s.app.GetApp(ctx, appID)
			if err != nil {
				return nil, err
			}
			if !app.Status {
				return nil, apperrors.New(40310, http.StatusForbidden, "应用已被禁用")
			}
		}
		return user, nil
	}
	if s.app != nil {
		if _, err := s.app.EnsureLoginAllowed(ctx, appID); err != nil {
			return nil, err
		}
	}
	return user, nil
}

// OAuth2Subject 用户在授权服务器里的主体标识：十进制用户 ID，与 Aegis 其余接口里的 userId 一致，
// 接入方据此就能把 ID Token 里的 sub 与 Aegis 服务端接口对上。
func OAuth2Subject(userID int64) string {
	return strconv.FormatInt(userID, 10)
}

func (s *AuthService) recordOAuth2Login(appID, userID int64, provider, deviceID, ip, userAgent string) {
	s.runDetached("login.oauth2_audit", 3*time.Second, func(actx context.Context) {
		_ = s.pg.InsertLoginAudit(actx, appID, userID, oauth2LoginType, provider, "", ip, deviceID, userAgent, "success", nil)
	})
}
