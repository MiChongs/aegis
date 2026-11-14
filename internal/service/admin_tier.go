package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"aegis/internal/authz"
	admindomain "aegis/internal/domain/admin"
	apperrors "aegis/pkg/errors"
	"go.uber.org/zap"
)

// 管理员分级与操作守卫。
//
// 两条正交的约束叠在一起，缺一条都会出现越权：
//
//  1. **档位（谁能管谁）**：系统管理员 > 超级管理员 > 管理员。只能管理档位严格低于自己的人；
//     系统管理员之间可以互相管理，但谁都不能停用 / 降级自己，也不能动最后一位系统管理员。
//     于是超级管理员停用不了系统管理员，也停用不了自己。
//  2. **授权上限（能授予什么）**：非超管只能授予自己此刻持有的权限点（含角色里展开出来的）。
//     否则一个被授了「角色编辑」的人，可以先给自己的角色勾上 `*`，再用它做任何事。
//
// 档位守卫的落点是「对某个管理员动手」的全部入口：停用、改授权、改档位、按人授权、
// 临时权限、强制下线、把人加进 / 移出某个角色。权限点只决定能不能进这个入口。

// AdminAction 对某个管理员动手的种类（守卫文案按动作区分）。
type AdminAction string

const (
	AdminActionStatus   AdminAction = "status"
	AdminActionAccess   AdminAction = "access"
	AdminActionTier     AdminAction = "tier"
	AdminActionGrant    AdminAction = "grant"
	AdminActionSession  AdminAction = "session"
	AdminActionTempPerm AdminAction = "temp_perm"
)

func adminActionName(action AdminAction) string {
	switch action {
	case AdminActionStatus:
		return "启用或停用"
	case AdminActionAccess:
		return "修改角色授权"
	case AdminActionTier:
		return "调整身份"
	case AdminActionGrant:
		return "修改个人授权"
	case AdminActionSession:
		return "强制下线"
	case AdminActionTempPerm:
		return "授予临时权限"
	}
	return "管理"
}

// tierOfAccess 会话的档位。
func tierOfAccess(access *admindomain.AccessContext) string {
	if access == nil {
		return admindomain.TierAdmin
	}
	return admindomain.TierOf(access.IsSuperAdmin, access.IsSystemAdmin)
}

// manageDecision 档位层面的结论（不含权限点）。
func manageDecision(actor *admindomain.AccessContext, target admindomain.Account, action AdminAction) (bool, int, string) {
	if actor == nil {
		return false, 40110, "管理员未认证"
	}
	self := actor.AdminID == target.ID
	if self {
		switch action {
		case AdminActionSession:
			return true, 0, ""
		case AdminActionStatus:
			return false, 40392, "不能停用或启用自己的账号"
		default:
			return false, 40393, fmt.Sprintf("不能%s自己：授权变更必须由他人完成", adminActionName(action))
		}
	}
	actorTier := tierOfAccess(actor)
	targetTier := admindomain.TierOf(target.IsSuperAdmin, target.IsSystemAdmin)
	if targetTier == admindomain.TierSystem && actorTier != admindomain.TierSystem {
		return false, 40394, fmt.Sprintf("系统管理员只能由系统管理员%s", adminActionName(action))
	}
	if targetTier == admindomain.TierSuper && actorTier != admindomain.TierSystem {
		return false, 40395, fmt.Sprintf("超级管理员只能由系统管理员%s", adminActionName(action))
	}
	if admindomain.TierRank(actorTier) < admindomain.TierRank(targetTier) {
		return false, 40396, fmt.Sprintf("不能%s身份高于自己的管理员", adminActionName(action))
	}
	return true, 0, ""
}

// GuardAdminTarget 档位守卫：返回目标管理员资料，不允许时返回带原因的 403。
func (s *AdminService) GuardAdminTarget(ctx context.Context, actor *admindomain.AccessContext, targetID int64, action AdminAction) (*admindomain.Profile, error) {
	if targetID <= 0 {
		return nil, apperrors.New(40058, http.StatusBadRequest, "缺少有效的管理员标识")
	}
	target, err := s.pg.GetAdminAccessByID(ctx, targetID)
	if err != nil {
		return nil, err
	}
	if target == nil {
		return nil, apperrors.New(40450, http.StatusNotFound, "管理员不存在")
	}
	if ok, code, message := manageDecision(actor, target.Account, action); !ok {
		status := http.StatusForbidden
		if code == 40110 {
			status = http.StatusUnauthorized
		}
		return nil, apperrors.New(code, status, message)
	}
	return target, nil
}

// AdminCapabilities 操作者对某个管理员能做什么（列表与详情的按钮显隐）。
func (s *AdminService) AdminCapabilities(ctx context.Context, actor *admindomain.AccessContext, target admindomain.Account) admindomain.AdminCapabilities {
	caps := admindomain.AdminCapabilities{IsSelf: actor != nil && actor.AdminID == target.ID}
	can := func(action AdminAction, permission string) bool {
		ok, _, message := manageDecision(actor, target, action)
		if !ok {
			if caps.ManageReason == "" && action != AdminActionSession {
				caps.ManageReason = message
			}
			return false
		}
		return s.HasPermission(ctx, actor, permission, nil)
	}
	caps.CanStatus = can(AdminActionStatus, authz.PermSystemAdminStatus)
	caps.CanAccess = can(AdminActionAccess, authz.PermSystemAdminEdit)
	caps.CanGrant = can(AdminActionGrant, authz.PermSystemAdminGrant)
	caps.CanSession = can(AdminActionSession, authz.PermSystemSessionRevoke)
	caps.CanTier = tierOfAccess(actor) == admindomain.TierSystem && !caps.IsSelf
	return caps
}

// ── 档位变更 ──

// normalizeTier 由请求里的 tier / isSuperAdmin 推导目标档位（tier 优先，兼容只传布尔的旧调用方）。
func normalizeTier(tier string, isSuper bool) (string, error) {
	switch strings.TrimSpace(tier) {
	case "":
		if isSuper {
			return admindomain.TierSuper, nil
		}
		return admindomain.TierAdmin, nil
	case admindomain.TierSystem, admindomain.TierSuper, admindomain.TierAdmin:
		return strings.TrimSpace(tier), nil
	}
	return "", apperrors.New(40068, http.StatusBadRequest, "无效的管理员身份，只能是 system / super / admin")
}

// ensureTierChangeAllowed 档位变更的规则：只有系统管理员能授予或收回超管 / 系统管理员身份，
// 且不能让系统里失去最后一位活跃的系统管理员。
func (s *AdminService) ensureTierChangeAllowed(ctx context.Context, actor *admindomain.AccessContext, target *admindomain.Account, next string) error {
	current := admindomain.TierAdmin
	if target != nil {
		current = admindomain.TierOf(target.IsSuperAdmin, target.IsSystemAdmin)
	}
	if current == next {
		return nil
	}
	if tierOfAccess(actor) != admindomain.TierSystem &&
		(next != admindomain.TierAdmin || current != admindomain.TierAdmin) {
		return apperrors.New(40397, http.StatusForbidden, "只有系统管理员可以授予或收回超级管理员、系统管理员身份")
	}
	if current == admindomain.TierSystem && target != nil && target.Status == "active" {
		count, err := s.pg.CountActiveSystemAdmins(ctx)
		if err != nil {
			return err
		}
		if count <= 1 {
			return apperrors.New(40398, http.StatusForbidden, "这是最后一位活跃的系统管理员，不能降级")
		}
	}
	return nil
}

// ── 授权上限 ──

// actorHolds 操作者是否持有某个权限点（作用域按权限点自身的判定域）。
//
// appID 为空时：平台级权限点看平台域；只在应用域判定的权限点要求「在任意应用里都有」——
// 即全局角色或「全部应用」数据权限给的，绑定到某一个应用的不算（否则一个只管应用 A 的人
// 可以把应用级权限授给一个全局角色，变相扩到所有应用）。
func (s *AdminService) actorHolds(ctx context.Context, actor *admindomain.AccessContext, code string, appID *int64) bool {
	if actor == nil {
		return false
	}
	if actor.IsSuperAdmin {
		return true
	}
	if appID != nil {
		return s.HasPermission(ctx, actor, code, appID)
	}
	if authz.PermissionScope(code) == authz.ScopeAppOnly {
		sentinel := anyAppSentinel
		return s.authz.Decide(s.subjectsFor(actor, &sentinel), authz.AppDomain(sentinel), code).Allowed
	}
	return s.HasPermission(ctx, actor, code, nil)
}

// anyAppSentinel 「任意一个未单独授权的应用」的占位标识：只有全局授权与「全部应用」数据权限能匹配它。
const anyAppSentinel int64 = -1

// EnsureCanGrant 授权上限：非超管授予的每一项都必须是自己持有的。
//
// patterns 可以是细分权限点、旧权限点或前缀通配；一律先展开成细分权限点再逐项核对，
// 于是「授予 app:*」等价于「授予 app: 下的每一项」，不存在用通配绕过上限的写法。
func (s *AdminService) EnsureCanGrant(ctx context.Context, actor *admindomain.AccessContext, patterns []string, appID *int64) error {
	if actor == nil {
		return apperrors.New(40110, http.StatusUnauthorized, "管理员未认证")
	}
	if actor.IsSuperAdmin {
		return nil
	}
	missing := make([]string, 0)
	seen := map[string]bool{}
	for _, pattern := range patterns {
		for _, code := range authz.ExpandPattern(strings.TrimSpace(pattern)) {
			if seen[code] {
				continue
			}
			seen[code] = true
			if !s.actorHolds(ctx, actor, code, appID) {
				missing = append(missing, code)
			}
		}
	}
	if len(missing) == 0 {
		return nil
	}
	names := make([]string, 0, 3)
	for i, code := range missing {
		if i == 3 {
			break
		}
		names = append(names, "「"+authz.PermissionName(code)+"」")
	}
	suffix := ""
	if len(missing) > 3 {
		suffix = fmt.Sprintf(" 等 %d 项", len(missing))
	}
	return apperrors.New(40399, http.StatusForbidden,
		"只能授予自己持有的权限，缺少 "+strings.Join(names, "、")+suffix)
}

// EnsureCanAssignRole 授权上限之于角色：角色展开后的每一项都必须是操作者持有的。
func (s *AdminService) EnsureCanAssignRole(ctx context.Context, actor *admindomain.AccessContext, roleKey string, appID *int64) error {
	if actor != nil && actor.IsSuperAdmin {
		return nil
	}
	role, ok := s.roleMeta(roleKey)
	if !ok {
		return apperrors.New(40053, http.StatusBadRequest, "包含无效的管理员角色")
	}
	effective := s.authz.PermissionsFor([]string{authz.RoleSubject(roleKey)}, authz.AppDomain(1), authz.AllPermissionCodes())
	if role.Scope == "global" {
		// 全局角色的平台级权限只在平台域成立，补展开一次
		effective = append(effective,
			s.authz.PermissionsFor([]string{authz.RoleSubject(roleKey)}, authz.PlatformDomain, authz.AllPermissionCodes())...)
	}
	if err := s.EnsureCanGrant(ctx, actor, effective, appID); err != nil {
		return apperrors.New(40399, http.StatusForbidden,
			fmt.Sprintf("不能授予角色「%s」：%s", role.Name, strings.TrimPrefix(err.Error(), "只能授予自己持有的权限，")))
	}
	return nil
}

// ── 系统管理员兜底 ──

// EnsureSystemAdmin 保证系统里至少有一位活跃的系统管理员。
//
// 迁移已经把最早的超管提升过一次；这里兜的是迁移之后的情形（比如库是从备份恢复的、
// 或者唯一的系统管理员账号被直接在库里改坏了）。配置了引导账号时优先提升它。
func (s *AdminService) EnsureSystemAdmin(ctx context.Context) error {
	count, err := s.pg.CountActiveSystemAdmins(ctx)
	if err != nil || count > 0 {
		return err
	}
	var candidate int64
	if account := strings.TrimSpace(s.cfg.AdminBootstrap.Account); account != "" {
		if record, err := s.pg.GetAdminAuthByAccount(ctx, account); err == nil && record != nil &&
			record.Account.IsSuperAdmin && record.Account.Status == "active" {
			candidate = record.Account.ID
		}
	}
	if candidate == 0 {
		if candidate, err = s.pg.EarliestActiveSuperAdminID(ctx); err != nil {
			return err
		}
	}
	if candidate == 0 {
		s.log.Warn("系统中没有任何活跃的超级管理员，无法指定系统管理员；请用 promote-system-admin 命令指定")
		return nil
	}
	if err := s.pg.PromoteSystemAdmin(ctx, candidate); err != nil {
		return err
	}
	s.log.Warn("系统中没有活跃的系统管理员，已自动提升", zap.Int64("adminId", candidate))
	return nil
}

// PromoteSystemAdminByAccount 运维命令：把指定账号提升为系统管理员（不经过会话与权限判定）。
func (s *AdminService) PromoteSystemAdminByAccount(ctx context.Context, account string) (*admindomain.Profile, error) {
	record, err := s.pg.GetAdminAuthByAccount(ctx, strings.TrimSpace(account))
	if err != nil {
		return nil, err
	}
	if record == nil {
		return nil, apperrors.New(40450, http.StatusNotFound, "管理员不存在")
	}
	if err := s.pg.PromoteSystemAdmin(ctx, record.Account.ID); err != nil {
		return nil, err
	}
	return s.pg.GetAdminAccessByID(ctx, record.Account.ID)
}
