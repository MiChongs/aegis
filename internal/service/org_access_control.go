package service

import (
	"context"
	"fmt"
	"net/http"

	"aegis/internal/authz"
	admindomain "aegis/internal/domain/admin"
	orgdomain "aegis/internal/domain/organization"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"

	"go.uber.org/zap"
)

// OrgAccessControl 组织域权限判定。
//
// **不再自带 enforcer。** 这里曾经有平台里第二个 Casbin 实例、第二套模型、
// 第二份重载逻辑，两套判定语义各自演化：平台侧不支持通配与拒绝，组织侧
// 支持 `*` 但只有"全部/精确"两档域匹配。同一个平台里两种授权语义，
// 排查问题时第一步永远是"先搞清楚这条路径走的是哪一套"。
//
// 现在两者共用 internal/authz 的同一个引擎、同一个模型、同一张策略表，
// 靠**域**（org:N）与**主体前缀**（orgrole:）区分：
//
//	平台 / 应用   sub = role:<key>              dom = platform | app:N
//	组织          sub = orgrole:<key>           dom = *        （内置角色，对所有组织通用）
//	              sub = orgrole:<orgID>:<key>   dom = org:N    （某组织的自定义角色）
//
// 「管理员 → 组织角色」仍然现查（org_members / org_role_grants），不进策略表：
// 成员关系随组织规模线性增长，且撤销必须立即生效。
type OrgAccessControl struct {
	log   *zap.Logger
	pg    *pgrepo.Repository
	admin *AdminService
	authz *authz.Engine
}

// NewOrgAccessControl 创建组织权限判定器
func NewOrgAccessControl(log *zap.Logger, pg *pgrepo.Repository, admin *AdminService, engine *authz.Engine) (*OrgAccessControl, error) {
	if engine == nil {
		return nil, fmt.Errorf("授权引擎未初始化")
	}
	return &OrgAccessControl{log: log, pg: pg, admin: admin, authz: engine}, nil
}

// OrgBuiltinPolicies 组织内置角色的策略（对所有组织通用，域取 *）。
//
// 与平台内置角色一样由代码给出、启动时整组重刷，因此"给 org_admin 加一个权限点"
// 能随版本到达所有既有部署。
func OrgBuiltinPolicies() []authz.PolicyRule {
	rules := make([]authz.PolicyRule, 0, 64)
	for roleKey, permissions := range orgdomain.BuiltinRolePermissions() {
		subject := authz.OrgRoleSubject(roleKey)
		for _, permission := range permissions {
			rules = append(rules, authz.PolicyRule{
				PType:  "p",
				Values: []string{subject, authz.AnyDomain, permission, authz.EffectAllow},
				Source: authz.SourceBuiltin,
				Owner:  subject,
			})
		}
	}
	return rules
}

// Reload 把组织自定义角色的权限同步进策略表。
//
// 角色的增删改都要调用它，否则新配的权限要等到重启才生效。
func (c *OrgAccessControl) Reload(ctx context.Context) error {
	if c == nil || c.pg == nil {
		return nil
	}
	policies, err := c.pg.ListAllOrgRolePolicies(ctx)
	if err != nil {
		return err
	}
	for _, policy := range policies {
		subject := authz.OrgCustomRoleSubject(policy.OrgID, policy.RoleKey)
		if err := c.authz.SetRolePolicy(ctx, authz.SourceOrg, subject, authz.RolePolicy{
			Allow:  policy.Permissions,
			Domain: authz.OrgDomain(policy.OrgID),
		}, nil); err != nil {
			return err
		}
	}
	c.log.Info("组织角色策略已装载", zap.Int("roles", len(policies)))
	return nil
}

// OrgAccess 管理员在某个组织内的有效访问上下文。
//
// Permissions 是展开后的权限集：控制台据此决定按钮显隐，
// 与服务端用的是同一次判定结果，不会出现「点了才 403」。
type OrgAccess struct {
	OrgID        int64           `json:"-"`
	OrgUUID      string          `json:"orgId"`
	AdminID      int64           `json:"adminId"`
	IsSuperAdmin bool            `json:"isSuperAdmin"`
	IsPlatform   bool            `json:"isPlatformAdmin"`
	OrgRole      string          `json:"orgRole"`
	CustomRoles  []string        `json:"customRoles"`
	Permissions  []string        `json:"permissions"`
	DeptScopes   []int64         `json:"-"`
	permSet      map[string]bool `json:"-"`
}

// Can 判定是否拥有某权限点
func (a *OrgAccess) Can(permission string) bool {
	if a == nil {
		return false
	}
	if a.IsSuperAdmin || a.IsPlatform {
		return true
	}
	return a.permSet[permission]
}

// IsMember 是否为组织成员（平台管理员跨组织访问时不算成员）
func (a *OrgAccess) IsMember() bool { return a != nil && a.OrgRole != "" }

// ScopedToDepts 该管理员是否被限定在部分部门内
func (a *OrgAccess) ScopedToDepts() bool {
	return a != nil && !a.IsSuperAdmin && !a.IsPlatform && len(a.DeptScopes) > 0
}

// AllowsDept 部门是否落在管理员的可见范围内
func (a *OrgAccess) AllowsDept(deptID int64) bool {
	if !a.ScopedToDepts() {
		return true
	}
	for _, id := range a.DeptScopes {
		if id == deptID {
			return true
		}
	}
	return false
}

// Resolve 解析管理员在指定组织内的访问上下文。
//
// 三条来源叠加，任一给出权限即放行：
//  1. 平台超管 —— 无条件放行
//  2. 平台级组织权限点（admin_assignments 里的 org:*）—— 跨组织管理能力的来源
//  3. 组织内角色（内置 org_role + 自定义 org_roles）
func (c *OrgAccessControl) Resolve(ctx context.Context, access *admindomain.AccessContext, orgID int64, orgUUID string) (*OrgAccess, error) {
	if access == nil {
		return nil, apperrors.New(40110, http.StatusUnauthorized, "管理员未认证")
	}

	result := &OrgAccess{
		OrgID:        orgID,
		OrgUUID:      orgUUID,
		AdminID:      access.AdminID,
		IsSuperAdmin: access.IsSuperAdmin,
		CustomRoles:  []string{},
		permSet:      map[string]bool{},
	}

	if access.IsSuperAdmin {
		result.Permissions = orgdomain.AllPermissions()
		for _, p := range result.Permissions {
			result.permSet[p] = true
		}
		// 超管即便不是成员也按 owner 视角展示
		role, err := c.pg.GetMemberRole(ctx, orgID, access.AdminID)
		if err != nil {
			return nil, err
		}
		result.OrgRole = role
		return result, nil
	}

	// 平台级组织权限：管理员在全局作用域下持有 org:write 即视为平台组织管理员
	if c.admin != nil {
		if err := c.admin.Authorize(ctx, access, orgdomain.PermOrgWrite, nil); err == nil {
			result.IsPlatform = true
			result.Permissions = orgdomain.AllPermissions()
			for _, p := range result.Permissions {
				result.permSet[p] = true
			}
			role, err := c.pg.GetMemberRole(ctx, orgID, access.AdminID)
			if err != nil {
				return nil, err
			}
			result.OrgRole = role
			return result, nil
		}
	}

	orgRole, err := c.pg.GetMemberRole(ctx, orgID, access.AdminID)
	if err != nil {
		return nil, err
	}
	result.OrgRole = orgRole

	grants, err := c.pg.ListAdminOrgRoleGrants(ctx, orgID, access.AdminID)
	if err != nil {
		return nil, err
	}

	subjects := make([]string, 0, len(grants)+2)
	// 管理员本人也是主体：组织域同样支持"只给这一个人"的直接授予/禁止。
	subjects = append(subjects, authz.AdminSubject(access.AdminID))
	if orgRole != "" {
		subjects = append(subjects, authz.OrgRoleSubject(orgRole))
	}
	scoped := false
	for _, g := range grants {
		result.CustomRoles = append(result.CustomRoles, g.RoleKey)
		subjects = append(subjects, authz.OrgCustomRoleSubject(orgID, g.RoleKey))
		if g.ScopeDeptID != nil {
			scoped = true
		}
	}
	if orgRole == "" && len(grants) == 0 {
		return result, nil // 非成员：权限集为空
	}

	// 一次展开，与真实判定同一段代码 —— 控制台按这份集合决定按钮显隐，
	// 两段各写一遍必然漂移成"点了才 403"。
	result.Permissions = c.authz.PermissionsFor(subjects, authz.OrgDomain(orgID), orgdomain.AllPermissions())
	for _, p := range result.Permissions {
		result.permSet[p] = true
	}

	// 只有当授予记录带部门限定时才去查范围 —— 绝大多数成员不受限，不必多打一次库
	if scoped {
		deptIDs, err := c.pg.AdminDeptScopes(ctx, orgID, access.AdminID)
		if err != nil {
			return nil, err
		}
		result.DeptScopes = deptIDs
	}
	return result, nil
}

// Require 解析访问上下文并要求持有指定权限，是 handler 的统一入口
func (c *OrgAccessControl) Require(ctx context.Context, access *admindomain.AccessContext, orgID int64, orgUUID, permission string) (*OrgAccess, error) {
	orgAccess, err := c.Resolve(ctx, access, orgID, orgUUID)
	if err != nil {
		return nil, err
	}
	if !orgAccess.Can(permission) {
		if !orgAccess.IsMember() {
			return nil, apperrors.New(40394, http.StatusForbidden, "你不是该组织的成员")
		}
		return nil, apperrors.New(40395, http.StatusForbidden,
			fmt.Sprintf("当前组织角色（%s）无权执行此操作", orgRoleLabel(orgAccess.OrgRole)))
	}
	return orgAccess, nil
}

// RequireRoleAtLeast 要求组织角色不低于指定层级（转让所有权等高危操作用）
func (c *OrgAccessControl) RequireRoleAtLeast(access *OrgAccess, minRole string) error {
	if access == nil {
		return apperrors.New(40110, http.StatusUnauthorized, "管理员未认证")
	}
	if access.IsSuperAdmin || access.IsPlatform {
		return nil
	}
	if orgdomain.RoleLevel(access.OrgRole) < orgdomain.RoleLevel(minRole) {
		return apperrors.New(40396, http.StatusForbidden,
			fmt.Sprintf("该操作需要「%s」及以上的组织角色", orgRoleLabel(minRole)))
	}
	return nil
}

// CanActOnMember 判定能否操作目标成员。
//
// 不能操作与自己同级或更高级的成员 —— 否则两个管理员可以互相降权，
// 而组织所有者会被自己任命的管理员踢掉。
func (c *OrgAccessControl) CanActOnMember(access *OrgAccess, targetRole string) error {
	if access == nil {
		return apperrors.New(40110, http.StatusUnauthorized, "管理员未认证")
	}
	if access.IsSuperAdmin || access.IsPlatform {
		return nil
	}
	if targetRole == orgdomain.RoleOwner {
		return apperrors.New(40397, http.StatusForbidden, "不能操作组织所有者")
	}
	if orgdomain.RoleLevel(access.OrgRole) <= orgdomain.RoleLevel(targetRole) {
		return apperrors.New(40398, http.StatusForbidden, "不能操作与自己同级或更高级别的成员")
	}
	return nil
}

// ── 内部辅助 ──

func orgRoleLabel(role string) string {
	for _, meta := range orgdomain.BuiltinRoles() {
		if meta.Key == role {
			return meta.Name
		}
	}
	if role == "" {
		return "非成员"
	}
	return role
}
