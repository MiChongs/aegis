package service

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"sync"

	admindomain "aegis/internal/domain/admin"
	orgdomain "aegis/internal/domain/organization"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"

	"github.com/casbin/casbin/v2"
	"github.com/casbin/casbin/v2/model"
	"go.uber.org/zap"
)

// OrgAccessControl 组织域权限判定。
//
// 与平台级 RBAC（AdminService.Authorize）的分工：
//
//	平台级  sub = 角色key,              obj = 权限点          —— 全局 / 应用作用域
//	组织级  sub = 角色key, dom = org:N, obj = 权限点          —— 组织作用域
//
// Casbin 里只装载「角色 → 权限」这一层策略（数量 = 角色数 × 权限数，可控），
// 「管理员 → 角色」由 org_members / org_role_members 现查。
// 把用户关系也灌进内存 enforcer 会随成员数线性膨胀，且每次成员变更都要同步。
type OrgAccessControl struct {
	log   *zap.Logger
	pg    *pgrepo.Repository
	admin *AdminService

	mu       sync.RWMutex
	enforcer *casbin.Enforcer
}

// NewOrgAccessControl 创建组织权限判定器
func NewOrgAccessControl(log *zap.Logger, pg *pgrepo.Repository, admin *AdminService) (*OrgAccessControl, error) {
	c := &OrgAccessControl{log: log, pg: pg, admin: admin}
	enforcer, err := newOrgEnforcer()
	if err != nil {
		return nil, err
	}
	c.enforcer = enforcer
	return c, nil
}

// newOrgEnforcer 组织域 Casbin enforcer。
//
// dom 为 "*" 的策略对所有组织生效（内置角色）；
// obj 为 "*" 表示该角色在此域内不受权限点约束（owner）。
func newOrgEnforcer() (*casbin.Enforcer, error) {
	m, err := model.NewModelFromString(`
[request_definition]
r = sub, dom, obj

[policy_definition]
p = sub, dom, obj

[role_definition]
g = _, _, _

[policy_effect]
e = some(where (p.eft == allow))

[matchers]
m = g(r.sub, p.sub, r.dom) && (p.dom == "*" || p.dom == r.dom) && (p.obj == "*" || p.obj == r.obj)
`)
	if err != nil {
		return nil, err
	}
	enforcer, err := casbin.NewEnforcer(m)
	if err != nil {
		return nil, err
	}
	// 内置角色对所有组织通用
	for roleKey, permissions := range orgdomain.BuiltinRolePermissions() {
		for _, permission := range permissions {
			if _, err := enforcer.AddPolicy(roleKey, "*", permission); err != nil {
				return nil, err
			}
		}
	}
	return enforcer, nil
}

// Reload 从数据库重新装载组织自定义角色策略。
// 角色的增删改都要调用它，否则新配的权限要等到重启才生效。
func (c *OrgAccessControl) Reload(ctx context.Context) error {
	if c == nil || c.pg == nil {
		return nil
	}
	policies, err := c.pg.ListAllOrgRolePolicies(ctx)
	if err != nil {
		return err
	}
	enforcer, err := newOrgEnforcer()
	if err != nil {
		return err
	}
	for _, p := range policies {
		domain := orgDomainKey(p.OrgID)
		subject := orgRoleSubject(p.OrgID, p.RoleKey)
		for _, permission := range p.Permissions {
			if _, err := enforcer.AddPolicy(subject, domain, permission); err != nil {
				return err
			}
		}
	}

	c.mu.Lock()
	c.enforcer = enforcer
	c.mu.Unlock()
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

	subjects := make([]string, 0, len(grants)+1)
	if orgRole != "" {
		subjects = append(subjects, orgRole)
	}
	scoped := false
	for _, g := range grants {
		result.CustomRoles = append(result.CustomRoles, g.RoleKey)
		subjects = append(subjects, orgRoleSubject(orgID, g.RoleKey))
		if g.ScopeDeptID != nil {
			scoped = true
		}
	}
	if len(subjects) == 0 {
		return result, nil // 非成员：权限集为空
	}

	domain := orgDomainKey(orgID)
	c.mu.RLock()
	enforcer := c.enforcer
	c.mu.RUnlock()

	for _, permission := range orgdomain.AllPermissions() {
		for _, subject := range subjects {
			allowed, err := enforcer.Enforce(subject, domain, permission)
			if err != nil {
				return nil, err
			}
			if allowed {
				result.permSet[permission] = true
				break
			}
		}
	}
	result.Permissions = make([]string, 0, len(result.permSet))
	for p := range result.permSet {
		result.Permissions = append(result.Permissions, p)
	}
	sort.Strings(result.Permissions)

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

func orgDomainKey(orgID int64) string {
	return "org:" + strconv.FormatInt(orgID, 10)
}

func orgRoleSubject(orgID int64, roleKey string) string {
	return "org:" + strconv.FormatInt(orgID, 10) + ":" + roleKey
}

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
