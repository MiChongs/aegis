package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"aegis/internal/authz"
	admindomain "aegis/internal/domain/admin"
	apperrors "aegis/pkg/errors"
)

// 授权策略的管理面。
//
// 这一层存在的理由是「不能灵活配置」那条抱怨：改动前想调整任何权限，
// 只有两条路 —— 建一个自定义角色（一次只能表达"允许"，且必须整套重配），
// 或者改代码重新部署。没有任何办法表达：
//
//	· 给某个人单独加一个权限点（不建角色）
//	· 收回某个人的某项能力（不动他的角色）
//	· 给内置角色补一项 / 砍一项（内置角色根本不可编辑）
//	· 让一个角色继承另一个角色（base_role 只是一根装饰线）
//
// 这四件事现在都是一条策略。

// PolicyOverrideInput 对某个角色的人工增减。
type PolicyOverrideInput struct {
	// RoleKey 目标角色，内置与自定义都可以。
	RoleKey string `json:"roleKey"`
	// Allow 额外允许的权限点（支持前缀通配，如 ticket:*）。
	Allow []string `json:"allow"`
	// Deny 显式拒绝的权限点。它压倒该角色的一切放行来源，包括继承来的。
	Deny []string `json:"deny"`
	// Inherits 额外继承的父角色。
	Inherits []string `json:"inherits"`
	Note     string   `json:"note"`
}

// AdminGrantInput 直接授予/禁止到某个管理员。
type AdminGrantInput struct {
	// AppID 为 nil 表示对所有域生效；否则限定在该应用内。
	AppID      *int64 `json:"appid,omitempty"`
	Permission string `json:"permission"`
	// Effect allow / deny，留空按 allow。
	Effect string `json:"effect"`
	Note   string `json:"note,omitempty"`
}

// SetRoleOverride 写入（或清空）某个角色的人工增减策略。
//
// 与自定义角色的权限编辑分开存放（source=override）是刻意的：
// 内置角色的定义每次启动都会按代码重刷，人工调整放同一组里会被冲掉。
func (s *AdminService) SetRoleOverride(ctx context.Context, input PolicyOverrideInput, updatedBy *int64) error {
	roleKey := strings.TrimSpace(input.RoleKey)
	if roleKey == "" {
		return apperrors.New(40062, http.StatusBadRequest, "角色标识不能为空")
	}
	if !s.roleExists(roleKey) {
		return apperrors.New(40063, http.StatusBadRequest, fmt.Sprintf("角色不存在：%s", roleKey))
	}
	if err := validatePermissionPatterns(append(append([]string{}, input.Allow...), input.Deny...)); err != nil {
		return err
	}
	inherits := make([]string, 0, len(input.Inherits))
	for _, parent := range input.Inherits {
		parent = strings.TrimSpace(parent)
		if parent == "" {
			continue
		}
		if !s.roleExists(parent) {
			return apperrors.New(40063, http.StatusBadRequest, fmt.Sprintf("父角色不存在：%s", parent))
		}
		if parent == roleKey {
			return apperrors.New(40064, http.StatusBadRequest, "角色不能继承自己")
		}
		inherits = append(inherits, authz.RoleSubject(parent))
	}
	return s.authz.SetRolePolicy(ctx, authz.SourceOverride, authz.RoleSubject(roleKey), authz.RolePolicy{
		Allow:    input.Allow,
		Deny:     input.Deny,
		Inherits: inherits,
		Note:     input.Note,
	}, updatedBy)
}

// SetAdminGrants 整组替换某个管理员的直接授予/禁止。
//
// 传空数组即清空 —— 与角色编辑一样是"提交一份完整状态"而不是增量指令：
// 增量接口在两个人同时编辑时会算出谁也没想要的第三种结果。
func (s *AdminService) SetAdminGrants(ctx context.Context, adminID int64, grants []AdminGrantInput, updatedBy *int64) error {
	if adminID <= 0 {
		return apperrors.New(40058, http.StatusBadRequest, "缺少有效的管理员标识")
	}
	profile, err := s.pg.GetAdminAccessByID(ctx, adminID)
	if err != nil {
		return err
	}
	if profile == nil {
		return apperrors.New(40450, http.StatusNotFound, "管理员不存在")
	}
	items := make([]authz.DirectGrant, 0, len(grants))
	for _, grant := range grants {
		permission := strings.TrimSpace(grant.Permission)
		if err := validatePermissionPatterns([]string{permission}); err != nil {
			return err
		}
		effect := strings.TrimSpace(grant.Effect)
		if effect != "" && effect != authz.EffectAllow && effect != authz.EffectDeny {
			return apperrors.New(40065, http.StatusBadRequest, "策略效果只能是 allow 或 deny")
		}
		domain := authz.AnyDomain
		if grant.AppID != nil {
			if *grant.AppID <= 0 {
				return apperrors.New(40058, http.StatusBadRequest, "缺少有效的应用标识")
			}
			domain = authz.AppDomain(*grant.AppID)
		}
		items = append(items, authz.DirectGrant{
			Domain: domain, Permission: permission, Effect: effect, Note: grant.Note,
		})
	}
	return s.authz.SetAdminGrants(ctx, adminID, items, updatedBy)
}

// AuthorizationExplain 一次「为什么能/不能」的完整解释。
type AuthorizationExplain struct {
	AdminID      int64          `json:"adminId"`
	Permission   string         `json:"permission"`
	Domain       string         `json:"domain"`
	IsSuperAdmin bool           `json:"isSuperAdmin"`
	// Subjects 本次判定实际用到的主体（本人 + 该作用域下生效的角色）。
	// 角色为空时基本就能确定问题在"没授权"而不是"权限点配错了"。
	Subjects []string        `json:"subjects"`
	Decision authz.Decision  `json:"decision"`
	// TempPermission 命中的临时权限（如果是它放行的）。
	TempPermission bool   `json:"tempPermission"`
	Allowed        bool   `json:"allowed"`
	Summary        string `json:"summary"`
}

// ExplainAuthorization 回答「某个管理员在某个作用域下能不能做某件事，为什么」。
//
// 这是授权系统最缺的一件工具。没有它，一次 403 的排查是：翻角色定义 →
// 翻权限点常量 → 翻路由映射 → 猜作用域，四处都对不上号还得看代码。
// 现在一次请求就能拿到判定用的全部主体、命中的策略行、以及结论。
func (s *AdminService) ExplainAuthorization(ctx context.Context, adminID int64, permission string, appID *int64) (*AuthorizationExplain, error) {
	permission = strings.TrimSpace(permission)
	if permission == "" {
		return nil, apperrors.New(40066, http.StatusBadRequest, "请指定要检查的权限点")
	}
	profile, err := s.pg.GetAdminAccessByID(ctx, adminID)
	if err != nil {
		return nil, err
	}
	if profile == nil {
		return nil, apperrors.New(40450, http.StatusNotFound, "管理员不存在")
	}
	access := &admindomain.AccessContext{
		Session: admindomain.Session{
			AdminID: profile.Account.ID, Account: profile.Account.Account,
			IsSuperAdmin: profile.Account.IsSuperAdmin,
		},
		Assignments: profile.Assignments,
	}

	result := &AuthorizationExplain{
		AdminID: adminID, Permission: permission,
		Domain:       authz.DomainForApp(appID),
		IsSuperAdmin: access.IsSuperAdmin,
		Subjects:     s.subjectsFor(access, appID),
	}
	if access.IsSuperAdmin {
		result.Allowed = true
		result.Summary = "超级管理员，不受权限点约束"
		return result, nil
	}

	result.Decision = s.authz.Decide(result.Subjects, result.Domain, permission)
	result.Allowed = result.Decision.Allowed
	switch {
	case result.Decision.Effect == authz.EffectAllow:
		result.Summary = fmt.Sprintf("由主体 %s 的策略 %v 放行", result.Decision.Subject, result.Decision.Rule)
		return result, nil
	case result.Decision.Effect == authz.EffectDeny:
		result.Summary = fmt.Sprintf("被主体 %s 上的显式拒绝策略 %v 挡下 —— 移除该策略才能恢复",
			result.Decision.Subject, result.Decision.Rule)
		return result, nil
	}

	// 没有策略命中时再看临时权限，顺序与真实判定一致。
	tempPerms, err := s.pg.GetActiveTempPermissions(ctx, adminID)
	if err == nil {
		for _, tp := range tempPerms {
			if scopeMatches(tp.AppID, appID) && tp.Permission == permission {
				result.Allowed = true
				result.TempPermission = true
				result.Summary = "由一条未过期的临时权限放行"
				return result, nil
			}
		}
	}
	if len(access.Assignments) == 0 {
		result.Summary = "该账号没有任何角色分配，因此在任何作用域下都不持有权限点"
		return result, nil
	}
	result.Summary = fmt.Sprintf("在作用域 %s 下没有任何策略命中 %s —— "+
		"通常是角色没有这个权限点，或角色绑定的应用与本次请求不是同一个", result.Domain, permission)
	return result, nil
}

// PolicySnapshot 当前生效的全部策略，供管理端审阅与导出。
type PolicySnapshot struct {
	Policies  [][]string `json:"policies"`
	RoleLinks [][]string `json:"roleLinks"`
}

// PolicySnapshot 返回引擎内存里当前生效的策略。
//
// 给的是**引擎里的**而不是库里的：两者不一致（比如某个实例重载失败）
// 恰恰是最需要看见的那种故障，而查库看不出来。
func (s *AdminService) PolicySnapshot() PolicySnapshot {
	return PolicySnapshot{Policies: s.authz.Policies(), RoleLinks: s.authz.RoleLinks()}
}

// roleExists 判定角色是否存在（内置或自定义）。
func (s *AdminService) roleExists(roleKey string) bool {
	if _, ok := s.roles[roleKey]; ok {
		return true
	}
	s.rolesMu.RLock()
	defer s.rolesMu.RUnlock()
	_, ok := s.customRoles[roleKey]
	return ok
}

// validatePermissionPatterns 校验权限点写法。
//
// 通配只允许"整段前缀 + *"（ticket:* / app:user:* / *）。允许任意位置的 *
// 会让人写出 `*:read` 这种看起来精确、实际按前缀匹配后什么都不匹配的规则 ——
// 而这种规则不会报错，只会永远不生效。
func validatePermissionPatterns(patterns []string) error {
	for _, pattern := range patterns {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			return apperrors.New(40066, http.StatusBadRequest, "权限点不能为空")
		}
		if pattern == authz.AnyPermission {
			continue
		}
		if index := strings.Index(pattern, "*"); index >= 0 {
			if index != len(pattern)-1 || !strings.HasSuffix(pattern, ":*") {
				return apperrors.New(40067, http.StatusBadRequest,
					fmt.Sprintf("权限通配只支持结尾的 `:*`（如 ticket:*），得到：%s", pattern))
			}
			continue
		}
		if !authz.PermissionExists(pattern) {
			return apperrors.New(40060, http.StatusBadRequest, fmt.Sprintf("权限代码不存在: %s", pattern))
		}
	}
	return nil
}

// ListAdminPolicies 列出某个主体名下的策略行（角色或管理员）。
func (s *AdminService) ListAdminPolicies(ctx context.Context, subject string) ([]authz.PolicyRule, error) {
	return s.pg.ListAuthzPoliciesBySubject(ctx, strings.TrimSpace(subject))
}

// ReloadPolicies 手动触发一次策略重载（排障用）。
func (s *AdminService) ReloadPolicies(ctx context.Context) error {
	return s.authz.Reload(ctx)
}
