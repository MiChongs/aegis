package service

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"unicode"

	admindomain "aegis/internal/domain/admin"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"go.uber.org/zap"
)

// 管理员账号自助：一次性改名、用户名可用性检查、修改密码。
//
// 规则：
//   - 用户名注册后**永久只能改一次**，改过的旧名永久保留，不能被任何人再用；
//   - 用户名不区分大小写判重；
//   - 改名与改密码都要先验证当前密码；
//   - LDAP / OIDC / SAML 同步进来的账号由外部身份源管理，两者都不能在这里改。

// adminAccountPattern 新用户名格式：字母开头，3–32 位，只含字母、数字、下划线、点、连字符。
// 只约束新名字（注册、创建、改名），存量账号即使不符合也照常登录。
var adminAccountPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_.-]{2,31}$`)

// reservedAdminAccounts 系统保留名，容易被误认成平台官方身份。
var reservedAdminAccounts = map[string]struct{}{
	"admin": {}, "administrator": {}, "root": {}, "system": {}, "sysadmin": {},
	"superadmin": {}, "super_admin": {}, "aegis": {}, "support": {}, "security": {},
	"official": {}, "operator": {}, "service": {}, "anonymous": {}, "null": {}, "undefined": {},
}

var (
	errAdminAccountInvalid = apperrors.New(42201, http.StatusUnprocessableEntity,
		"用户名须以字母开头，长度 3 到 32 位，只能包含字母、数字、下划线、点和连字符")
	errAdminAccountReserved      = apperrors.New(42202, http.StatusUnprocessableEntity, "该用户名为系统保留，不能使用")
	errAdminAccountSame          = apperrors.New(42203, http.StatusUnprocessableEntity, "新用户名与当前用户名相同")
	errAdminPasswordSame         = apperrors.New(42204, http.StatusUnprocessableEntity, "新密码不能与当前密码相同")
	errAdminPasswordWeak         = apperrors.New(42205, http.StatusUnprocessableEntity, "密码须同时包含字母和数字")
	errAdminPasswordHasAccount   = apperrors.New(42206, http.StatusUnprocessableEntity, "密码不能包含用户名")
	errAdminCurrentPasswordWrong = apperrors.New(42207, http.StatusUnprocessableEntity, "当前密码不正确")
)

// validateAdminAccountName 校验新用户名的格式与保留名。
func validateAdminAccountName(account string) error {
	account = strings.TrimSpace(account)
	if account == "" {
		return apperrors.New(40051, http.StatusBadRequest, "管理员账号不能为空")
	}
	if !adminAccountPattern.MatchString(account) {
		return errAdminAccountInvalid
	}
	if _, reserved := reservedAdminAccounts[strings.ToLower(account)]; reserved {
		return errAdminAccountReserved
	}
	return nil
}

// validateAdminPasswordStrength 在长度校验之上要求同时含字母与数字，且不包含用户名。
func validateAdminPasswordStrength(account, password string) error {
	if err := validateAdminPassword(password); err != nil {
		return err
	}
	var hasLetter, hasDigit bool
	for _, r := range password {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return errAdminPasswordWeak
	}
	account = strings.ToLower(strings.TrimSpace(account))
	if len(account) >= 3 && strings.Contains(strings.ToLower(password), account) {
		return errAdminPasswordHasAccount
	}
	return nil
}

// CheckAccountAvailability 检查新用户名是否可用。不可用时给出原因，不报错。
func (s *AdminService) CheckAccountAvailability(ctx context.Context, adminID int64, account string) (*admindomain.AccountAvailability, error) {
	account = strings.TrimSpace(account)
	result := &admindomain.AccountAvailability{Account: account}
	// 先判「与当前相同」：引导超管默认叫 superadmin，本身就在保留名里，
	// 对自己的当前用户名说「系统保留」只会让人困惑
	profile, err := s.GetProfile(ctx, adminID)
	if err != nil {
		return nil, err
	}
	if profile.Account.Account == account {
		result.Reason = "same"
		result.Message = errAdminAccountSame.Message
		return result, nil
	}
	if err := validateAdminAccountName(account); err != nil {
		result.Reason = "invalid"
		if err == errAdminAccountReserved {
			result.Reason = "reserved"
		}
		result.Message = err.(*apperrors.AppError).Message
		return result, nil
	}
	taken, err := s.pg.AdminAccountNameTaken(ctx, account, adminID)
	if err != nil {
		return nil, err
	}
	// 自己的旧名不存在（改过名就不能再改），所以排除自己是安全的：
	// 仅大小写不同的改名（Bob → bob）也是一次合法的改名。
	if taken {
		result.Reason = "taken"
		result.Message = pgrepo.ErrAdminAccountTaken.Message
		return result, nil
	}
	result.Available = true
	return result, nil
}

// ChangeAccount 使用唯一一次改名机会。
func (s *AdminService) ChangeAccount(ctx context.Context, adminID int64, newAccount, currentPassword string) (*admindomain.Profile, error) {
	newAccount = strings.TrimSpace(newAccount)
	record, err := s.pg.GetAdminAuthByID(ctx, adminID)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, apperrors.New(40450, http.StatusNotFound, "管理员不存在")
	}
	if !record.Account.CanChangePassword {
		return nil, pgrepo.ErrAdminExternalAccount
	}
	if record.Account.AccountChangedAt != nil {
		return nil, pgrepo.ErrAdminAccountRenameUsed
	}
	if record.Account.Account == newAccount {
		return nil, errAdminAccountSame
	}
	if err := validateAdminAccountName(newAccount); err != nil {
		return nil, err
	}
	if !adminVerifyPassword(record.PasswordHash, currentPassword) {
		return nil, errAdminCurrentPasswordWrong
	}
	profile, err := s.pg.RenameAdminAccount(ctx, adminID, newAccount)
	if err != nil {
		return nil, err
	}
	s.log.Info("管理员修改用户名",
		zap.Int64("admin_id", adminID),
		zap.String("from", record.Account.Account),
		zap.String("to", newAccount),
	)
	return profile, nil
}

// ChangePassword 修改自己的密码。signOutOthers 为真时下线除当前会话外的所有会话。
func (s *AdminService) ChangePassword(ctx context.Context, access *admindomain.AccessContext, currentPassword, newPassword string, signOutOthers bool) (*admindomain.PasswordChangeResult, error) {
	record, err := s.pg.GetAdminAuthByID(ctx, access.AdminID)
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, apperrors.New(40450, http.StatusNotFound, "管理员不存在")
	}
	if !record.Account.CanChangePassword {
		return nil, pgrepo.ErrAdminExternalAccount
	}
	if !adminVerifyPassword(record.PasswordHash, currentPassword) {
		return nil, errAdminCurrentPasswordWrong
	}
	if newPassword == currentPassword {
		return nil, errAdminPasswordSame
	}
	if err := validateAdminPasswordStrength(record.Account.Account, newPassword); err != nil {
		return nil, err
	}
	hash, err := adminHashPassword(newPassword)
	if err != nil {
		return nil, err
	}
	changedAt, err := s.pg.UpdateAdminPassword(ctx, access.AdminID, hash)
	if err != nil {
		return nil, err
	}
	result := &admindomain.PasswordChangeResult{PasswordChangedAt: changedAt}
	if signOutOthers {
		ids, err := s.pg.RevokeAdminSessionsExcept(ctx, access.AdminID, access.TokenID, access.AdminID)
		if err != nil {
			// 密码已经改好了，会话撤销失败不回滚，只记日志
			s.log.Warn("修改密码后撤销其他会话失败", zap.Int64("admin_id", access.AdminID), zap.Error(err))
		}
		for _, id := range ids {
			// 校验会先查黑名单，所以拉黑 jti 即可让对应令牌立刻失效
			_ = s.sessions.BlacklistToken(ctx, id, s.cfg.AdminSessionTTL)
			_ = s.sessions.RemoveAdminOnline(ctx, access.AdminID, id)
		}
		result.RevokedSessions = int64(len(ids))
	}
	s.log.Info("管理员修改密码",
		zap.Int64("admin_id", access.AdminID),
		zap.Int64("revoked_sessions", result.RevokedSessions),
	)
	return result, nil
}
