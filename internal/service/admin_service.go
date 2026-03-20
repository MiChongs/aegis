package service

import (
	"context"
	"crypto/subtle"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"aegis/internal/config"
	admindomain "aegis/internal/domain/admin"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

type AdminService struct {
	cfg      config.Config
	log      *zap.Logger
	pg       *pgrepo.Repository
	sessions *redisrepo.SessionRepository
	enforcer *casbin.Enforcer
	roles    map[string]admindomain.RoleDefinition
}

func NewAdminService(cfg config.Config, log *zap.Logger, pg *pgrepo.Repository, sessions *redisrepo.SessionRepository) (*AdminService, error) {
	if log == nil {
		log = zap.NewNop()
	}
	enforcer, err := newAdminEnforcer()
	if err != nil {
		return nil, err
	}
	return &AdminService{
		cfg:      cfg,
		log:      log,
		pg:       pg,
		sessions: sessions,
		enforcer: enforcer,
		roles:    builtInAdminRoles(),
	}, nil
}

func (s *AdminService) EnsureBootstrapSuperAdmin(ctx context.Context) error {
	password := strings.TrimSpace(s.cfg.AdminBootstrap.Password)
	if password == "" {
		password = strings.TrimSpace(s.cfg.AdminAPIToken)
	}
	if password == "" {
		return nil
	}
	input := admindomain.CreateInput{
		Account:      strings.TrimSpace(s.cfg.AdminBootstrap.Account),
		Password:     password,
		DisplayName:  strings.TrimSpace(s.cfg.AdminBootstrap.DisplayName),
		Email:        strings.TrimSpace(s.cfg.AdminBootstrap.Email),
		IsSuperAdmin: true,
	}
	hash, err := adminHashPassword(input.Password)
	if err != nil {
		return err
	}
	profile, err := s.pg.UpsertBootstrapAdmin(ctx, input, hash)
	if err != nil {
		return err
	}
	if profile != nil {
		s.log.Info("bootstrap super admin ensured",
			zap.Int64("admin_id", profile.Account.ID),
			zap.String("account", profile.Account.Account),
			zap.Bool("fallback_admin_api_token", strings.TrimSpace(s.cfg.AdminBootstrap.Password) == "" && strings.TrimSpace(s.cfg.AdminAPIToken) != ""),
		)
	}
	return nil
}

func (s *AdminService) Login(ctx context.Context, account, password string) (*admindomain.LoginResult, error) {
	record, err := s.pg.GetAdminAuthByAccount(ctx, strings.TrimSpace(account))
	if err != nil {
		return nil, err
	}
	if record == nil || !adminVerifyPassword(record.PasswordHash, password) {
		return nil, apperrors.New(40110, http.StatusUnauthorized, "管理员账号或密码错误")
	}
	if record.Account.Status != "active" {
		return nil, apperrors.New(40310, http.StatusForbidden, "管理员账户不可用")
	}
	profile, err := s.pg.GetAdminAccessByID(ctx, record.Account.ID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, apperrors.New(40450, http.StatusNotFound, "管理员不存在")
	}
	return s.issueSession(ctx, profile)
}

func (s *AdminService) ValidateAccessToken(ctx context.Context, token string) (*admindomain.AccessContext, error) {
	if access := s.fallbackAccess(token); access != nil {
		return access, nil
	}

	claims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (interface{}, error) {
		return []byte(s.cfg.JWT.Secret), nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(s.cfg.JWT.Issuer))
	if err != nil || !parsed.Valid {
		return nil, apperrors.New(40110, http.StatusUnauthorized, "管理员令牌无效")
	}
	if typ, _ := claims["typ"].(string); typ != "admin" {
		return nil, apperrors.New(40110, http.StatusUnauthorized, "管理员令牌无效")
	}
	tokenID, _ := claims["jti"].(string)
	blacklisted, err := s.sessions.IsBlacklisted(ctx, tokenID)
	if err != nil {
		return nil, err
	}
	if blacklisted {
		return nil, apperrors.New(40111, http.StatusUnauthorized, "管理员令牌已失效")
	}
	session, err := s.sessions.GetAdminSession(ctx, token)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, apperrors.New(40112, http.StatusUnauthorized, "管理员会话不存在或已过期")
	}
	profile, err := s.pg.GetAdminAccessByID(ctx, session.AdminID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, apperrors.New(40450, http.StatusNotFound, "管理员不存在")
	}
	if profile.Account.Status != "active" {
		return nil, apperrors.New(40310, http.StatusForbidden, "管理员账户不可用")
	}
	return &admindomain.AccessContext{
		Session: admindomain.Session{
			AdminID:       profile.Account.ID,
			Account:       profile.Account.Account,
			DisplayName:   profile.Account.DisplayName,
			TokenID:       session.TokenID,
			IssuedAt:      session.IssuedAt,
			ExpiresAt:     session.ExpiresAt,
			IsSuperAdmin:  profile.Account.IsSuperAdmin,
			FallbackToken: false,
		},
		Assignments: profile.Assignments,
	}, nil
}

func (s *AdminService) Logout(ctx context.Context, token string) error {
	access, err := s.ValidateAccessToken(ctx, token)
	if err != nil {
		return err
	}
	if access.FallbackToken {
		return nil
	}
	_ = s.sessions.DeleteAdminSession(ctx, token)
	return s.sessions.BlacklistToken(ctx, access.TokenID, time.Until(access.ExpiresAt))
}

func (s *AdminService) Authorize(access *admindomain.AccessContext, permission string, appID *int64) error {
	if access == nil {
		return apperrors.New(40110, http.StatusUnauthorized, "管理员未认证")
	}
	if access.IsSuperAdmin || access.FallbackToken {
		return nil
	}
	if permission == "" {
		return nil
	}
	for _, assignment := range access.Assignments {
		if !scopeMatches(assignment.AppID, appID) {
			continue
		}
		allowed, err := s.enforcer.Enforce(assignment.RoleKey, permission)
		if err != nil {
			return err
		}
		if allowed {
			return nil
		}
	}
	return apperrors.New(40311, http.StatusForbidden, "当前管理员无权执行此操作")
}

func (s *AdminService) ListAdmins(ctx context.Context) ([]admindomain.Profile, error) {
	return s.pg.ListAdminAccounts(ctx)
}

func (s *AdminService) CreateAdmin(ctx context.Context, input admindomain.CreateInput) (*admindomain.Profile, error) {
	input.Account = strings.TrimSpace(input.Account)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Email = strings.TrimSpace(input.Email)
	if err := s.validateCreateInput(input); err != nil {
		return nil, err
	}
	hash, err := adminHashPassword(input.Password)
	if err != nil {
		return nil, err
	}
	input.Assignments, err = s.normalizeAssignments(input.Assignments, input.IsSuperAdmin)
	if err != nil {
		return nil, err
	}
	return s.pg.CreateAdminAccount(ctx, input, hash)
}

func (s *AdminService) UpdateAdminStatus(ctx context.Context, adminID int64, status string) error {
	status = strings.TrimSpace(strings.ToLower(status))
	if status != "active" && status != "disabled" {
		return apperrors.New(40050, http.StatusBadRequest, "无效的管理员状态")
	}
	return s.pg.UpdateAdminStatus(ctx, adminID, status)
}

func (s *AdminService) UpdateAdminAccess(ctx context.Context, adminID int64, input admindomain.UpdateAccessInput) error {
	assignments, err := s.normalizeAssignments(input.Assignments, input.IsSuperAdmin)
	if err != nil {
		return err
	}
	input.Assignments = assignments
	return s.pg.UpdateAdminAccess(ctx, adminID, input)
}

func (s *AdminService) ListRoles() []admindomain.RoleDefinition {
	items := make([]admindomain.RoleDefinition, 0, len(s.roles))
	for _, item := range s.roles {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Level == items[j].Level {
			return items[i].Key < items[j].Key
		}
		return items[i].Level > items[j].Level
	})
	return items
}

func (s *AdminService) validateCreateInput(input admindomain.CreateInput) error {
	input.Account = strings.TrimSpace(input.Account)
	if input.Account == "" {
		return apperrors.New(40051, http.StatusBadRequest, "管理员账号不能为空")
	}
	if len(input.Account) < 3 || len(input.Account) > 64 {
		return apperrors.New(40052, http.StatusBadRequest, "管理员账号长度必须在 3 到 64 个字符之间")
	}
	if err := validateAdminPassword(input.Password); err != nil {
		return err
	}
	return nil
}

func (s *AdminService) normalizeAssignments(assignments []admindomain.AssignmentMutation, isSuperAdmin bool) ([]admindomain.AssignmentMutation, error) {
	if isSuperAdmin {
		return nil, nil
	}
	items := make([]admindomain.AssignmentMutation, 0, len(assignments))
	seen := map[string]struct{}{}
	for _, item := range assignments {
		roleKey := strings.TrimSpace(item.RoleKey)
		role, ok := s.roles[roleKey]
		if !ok || roleKey == "super_admin" {
			return nil, apperrors.New(40053, http.StatusBadRequest, "包含无效的管理员角色")
		}
		if role.Scope == "app" && item.AppID == nil {
			return nil, apperrors.New(40054, http.StatusBadRequest, "应用级角色必须绑定应用")
		}
		if role.Scope == "global" {
			item.AppID = nil
		}
		key := roleKey + ":*"
		if item.AppID != nil {
			key = roleKey + ":" + strconvInt64(*item.AppID)
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		items = append(items, admindomain.AssignmentMutation{RoleKey: roleKey, AppID: item.AppID})
	}
	if len(items) == 0 {
		return nil, apperrors.New(40055, http.StatusBadRequest, "至少需要一个管理员角色")
	}
	return items, nil
}

func (s *AdminService) issueSession(ctx context.Context, profile *admindomain.Profile) (*admindomain.LoginResult, error) {
	now := time.Now().UTC()
	expiresAt := now.Add(s.cfg.AdminSessionTTL)
	tokenID := uuid.NewString()
	claims := jwt.MapClaims{
		"aid":     profile.Account.ID,
		"account": profile.Account.Account,
		"super":   profile.Account.IsSuperAdmin,
		"typ":     "admin",
		"jti":     tokenID,
		"iss":     s.cfg.JWT.Issuer,
		"iat":     now.Unix(),
		"exp":     expiresAt.Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(s.cfg.JWT.Secret))
	if err != nil {
		return nil, err
	}
	session := admindomain.Session{
		AdminID:      profile.Account.ID,
		Account:      profile.Account.Account,
		DisplayName:  profile.Account.DisplayName,
		TokenID:      tokenID,
		IssuedAt:     now,
		ExpiresAt:    expiresAt,
		IsSuperAdmin: profile.Account.IsSuperAdmin,
	}
	if err := s.sessions.SetAdminSession(ctx, signed, session, time.Until(expiresAt)); err != nil {
		return nil, err
	}
	_ = s.pg.UpdateAdminLastLogin(ctx, profile.Account.ID, now)
	return &admindomain.LoginResult{
		AccessToken: signed,
		ExpiresAt:   expiresAt,
		TokenType:   "Bearer",
		Admin:       profile.Account,
		Assignments: profile.Assignments,
	}, nil
}

func (s *AdminService) fallbackAccess(token string) *admindomain.AccessContext {
	expected := strings.TrimSpace(s.cfg.AdminAPIToken)
	token = strings.TrimSpace(token)
	if expected == "" || token == "" {
		return nil
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
		return nil
	}
	now := time.Now().UTC()
	return &admindomain.AccessContext{
		Session: admindomain.Session{
			AdminID:       0,
			Account:       "static-token",
			DisplayName:   "Emergency Admin Token",
			TokenID:       "static-admin-token",
			IssuedAt:      now,
			ExpiresAt:     now.Add(24 * time.Hour),
			IsSuperAdmin:  true,
			FallbackToken: true,
		},
	}
}

func newAdminEnforcer() (*casbin.Enforcer, error) {
	m, err := model.NewModelFromString(`
[request_definition]
r = sub, obj

[policy_definition]
p = sub, obj

[role_definition]
g = _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub) && r.obj == p.obj
`)
	if err != nil {
		return nil, err
	}
	e, err := casbin.NewEnforcer(m)
	if err != nil {
		return nil, err
	}
	for _, role := range builtInAdminRoles() {
		for _, permission := range role.Permissions {
			if _, err := e.AddPermissionForUser(role.Key, permission); err != nil {
				return nil, err
			}
		}
	}
	return e, nil
}

func builtInAdminRoles() map[string]admindomain.RoleDefinition {
	return map[string]admindomain.RoleDefinition{
		"super_admin": {
			Key:         "super_admin",
			Name:        "超级管理员",
			Description: "平台最高管理权限",
			Level:       100,
			Scope:       "global",
			Permissions: []string{},
		},
		"platform_admin": {
			Key:         "platform_admin",
			Name:        "平台管理员",
			Description: "全局平台与运维配置管理",
			Level:       90,
			Scope:       "global",
			Permissions: []string{"platform:app:read", "platform:app:write", "platform:storage:read", "platform:storage:write", "system:user_setting:read", "system:user_setting:write", "email:read", "email:write", "payment:read", "payment:write", "workflow:read", "workflow:write", "version:read", "version:write", "site:read", "site:write", "site:audit", "role_application:read", "role_application:review", "points:read", "points:write", "content:read", "content:write", "user:read", "user:write", "audit:read", "app:read", "app:write", "storage:read", "storage:write"},
		},
		"app_admin": {
			Key:         "app_admin",
			Name:        "应用管理员",
			Description: "单应用全量管理权限",
			Level:       70,
			Scope:       "app",
			Permissions: []string{"app:read", "app:write", "content:read", "content:write", "user:read", "user:write", "audit:read", "points:read", "points:write", "version:read", "version:write", "site:read", "site:write", "site:audit", "workflow:read", "workflow:write", "email:read", "email:write", "payment:read", "payment:write", "role_application:read", "role_application:review", "storage:read", "storage:write"},
		},
		"app_operator": {
			Key:         "app_operator",
			Name:        "应用运营管理员",
			Description: "运营、内容、用户与版本维护",
			Level:       60,
			Scope:       "app",
			Permissions: []string{"app:read", "content:read", "content:write", "user:read", "user:write", "audit:read", "points:read", "points:write", "version:read", "version:write", "site:read", "site:write", "workflow:read", "workflow:write", "email:read", "payment:read", "role_application:read", "storage:read", "storage:write"},
		},
		"app_auditor": {
			Key:         "app_auditor",
			Name:        "应用审核管理员",
			Description: "审计、审核与只读分析权限",
			Level:       40,
			Scope:       "app",
			Permissions: []string{"app:read", "content:read", "user:read", "audit:read", "points:read", "version:read", "site:read", "site:audit", "workflow:read", "email:read", "payment:read", "role_application:read", "role_application:review", "storage:read"},
		},
		"app_viewer": {
			Key:         "app_viewer",
			Name:        "应用观察员",
			Description: "只读查看权限",
			Level:       20,
			Scope:       "app",
			Permissions: []string{"app:read", "content:read", "user:read", "audit:read", "points:read", "version:read", "site:read", "workflow:read", "email:read", "payment:read", "role_application:read", "storage:read"},
		},
	}
}

func scopeMatches(assignmentAppID *int64, requestAppID *int64) bool {
	if requestAppID == nil {
		return assignmentAppID == nil
	}
	if assignmentAppID == nil {
		return true
	}
	return *assignmentAppID == *requestAppID
}

func adminVerifyPassword(hash, password string) bool {
	if hash == "" {
		return false
	}
	if strings.HasPrefix(hash, "$2a$") || strings.HasPrefix(hash, "$2b$") || strings.HasPrefix(hash, "$2y$") {
		return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	}
	return subtle.ConstantTimeCompare([]byte(hash), []byte(password)) == 1
}

func adminHashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}

func validateAdminPassword(password string) error {
	password = strings.TrimSpace(password)
	if len(password) < 8 {
		return apperrors.New(40056, http.StatusBadRequest, "管理员密码长度不能少于 8 位")
	}
	if len(password) > 72 {
		return apperrors.New(40057, http.StatusBadRequest, "管理员密码长度不能超过 72 位")
	}
	return nil
}

func strconvInt64(value int64) string {
	return strconv.FormatInt(value, 10)
}
