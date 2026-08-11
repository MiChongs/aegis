package service

import (
	"context"
	"fmt"
	"net/http"
	"sort"

	admindomain "aegis/internal/domain/admin"
	ticketdomain "aegis/internal/domain/ticket"
	apperrors "aegis/pkg/errors"
)

// 工单权限模型。三层叠加，任一层通过即可看到/处理工单：
//
//	① 全局层：拥有全局作用域的 ticket:read → 看全部（超管天然满足）
//	② 应用层：应用作用域角色的 ticket:read → 看该应用的工单
//	③ 人员层：受理人 / 提单人 / 关注人 / 所在处理组 → 无论有没有权限点，都看得到自己名下的工单
//
// 第③层是"特定人员处理工单"的落点：把某人加进处理组，他立刻能处理组内工单，
// 但绝不会因此看到组外的任何工单。

// 工单权限点
const (
	PermTicketRead     = "ticket:read"     // 查看工单（受可见范围约束）
	PermTicketWrite    = "ticket:write"    // 建单 / 改标题、分类、优先级、标签
	PermTicketReply    = "ticket:reply"    // 对外回复
	PermTicketInternal = "ticket:internal" // 查看与发表内部备注
	PermTicketAssign   = "ticket:assign"   // 指派 / 转派
	PermTicketClose    = "ticket:close"    // 解决 / 关闭 / 重开
	PermTicketDelete   = "ticket:delete"   // 删除工单
	PermTicketManage   = "ticket:manage"   // 分类 / SLA / 处理组 / 快捷回复配置
	PermTicketExport   = "ticket:export"   // 导出
)

// 通知渠道权限点
const (
	PermNotifyChannelRead  = "notify:channel:read"
	PermNotifyChannelWrite = "notify:channel:write"
	PermNotifyDeliveryRead = "notify:delivery:read"
	PermNotifyTest         = "notify:test"
)

// can 判定管理员是否拥有某权限点。appID 为 nil 表示要求全局作用域。
func (s *TicketService) can(ctx context.Context, access *admindomain.AccessContext, permission string, appID *int64) bool {
	if access == nil {
		return false
	}
	if access.IsSuperAdmin {
		return true
	}
	if s.admin == nil {
		return false
	}
	return s.admin.Authorize(ctx, access, permission, appID) == nil
}

// ResolveScope 推导当前管理员的工单可见范围。
func (s *TicketService) ResolveScope(ctx context.Context, access *admindomain.AccessContext) (ticketdomain.Scope, error) {
	scope := ticketdomain.Scope{}
	if access == nil {
		return scope, apperrors.New(40110, http.StatusUnauthorized, "管理员未认证")
	}
	scope.AdminID = access.AdminID

	// 处理组归属：无论权限如何都要拿，它是"人员层"的依据
	groupIDs, err := s.pg.ListTicketGroupIDsForAdmin(ctx, access.AdminID)
	if err != nil {
		return scope, err
	}
	scope.GroupIDs = groupIDs

	if access.IsSuperAdmin {
		scope.All = true
		return scope, nil
	}
	// 全局作用域的 ticket:read → 看全部
	if s.can(ctx, access, PermTicketRead, nil) {
		scope.All = true
		return scope, nil
	}

	// 逐个应用作用域判定
	appIDs := make([]int64, 0, len(access.Assignments))
	seen := make(map[int64]struct{}, len(access.Assignments))
	for _, assignment := range access.Assignments {
		if assignment.AppID == nil {
			continue
		}
		if _, ok := seen[*assignment.AppID]; ok {
			continue
		}
		if !s.can(ctx, access, PermTicketRead, assignment.AppID) {
			continue
		}
		seen[*assignment.AppID] = struct{}{}
		appIDs = append(appIDs, *assignment.AppID)
	}
	sort.Slice(appIDs, func(i, j int) bool { return appIDs[i] < appIDs[j] })
	scope.AppIDs = appIDs
	scope.PersonalOnly = len(appIDs) == 0
	return scope, nil
}

// ScopeInfo 把范围翻译成给前端展示的说明。
func ScopeInfo(scope ticketdomain.Scope) *ticketdomain.ScopeInfo {
	info := &ticketdomain.ScopeInfo{AppIDs: scope.AppIDs, GroupIDs: scope.GroupIDs}
	switch {
	case scope.All:
		info.Level = "all"
		info.Label = "可查看全部工单"
	case len(scope.AppIDs) > 0:
		info.Level = "app"
		info.Label = fmt.Sprintf("可查看 %d 个授权应用的工单，以及指派给你或你所在处理组的工单", len(scope.AppIDs))
	default:
		info.Level = "personal"
		info.Label = "仅可查看指派给你、你提交的，或你所在处理组的工单"
	}
	return info
}

// visibleTo 判定某工单是否落在可见范围内（与 SQL 层的 ticketScopeClause 语义一致）。
func visibleTo(item *ticketdomain.Ticket, scope ticketdomain.Scope) bool {
	if item == nil {
		return false
	}
	if scope.All {
		return true
	}
	if personallyRelated(item, scope) {
		return true
	}
	if scope.PersonalOnly {
		return false
	}
	for _, appID := range scope.AppIDs {
		if item.AppID == appID {
			return true
		}
	}
	return false
}

// personallyRelated 「与我有关」：受理人 / 管理员提单人 / 代提人 / 我所在的处理组。
// 关注人关系需要查库，由调用方在必要时补充。
func personallyRelated(item *ticketdomain.Ticket, scope ticketdomain.Scope) bool {
	if item.AssigneeAdminID != nil && *item.AssigneeAdminID == scope.AdminID {
		return true
	}
	if item.RequesterAdminID != nil && *item.RequesterAdminID == scope.AdminID {
		return true
	}
	if item.CreatedByAdminID != nil && *item.CreatedByAdminID == scope.AdminID {
		return true
	}
	if item.GroupID != nil {
		for _, groupID := range scope.GroupIDs {
			if groupID == *item.GroupID {
				return true
			}
		}
	}
	return false
}

// ResolveActions 计算当前管理员对某工单能做什么，供前端控制按钮显隐。
//
// 处理动作（回复 / 指派 / 关闭）的判定是「权限点 OR 人员归属」：
// 组成员即使没有 ticket:reply，也能回复指派给自己或本组的工单——
// 这正是"特定人员支持处理工单"的语义；反过来，与自己无关的工单必须有权限点才能动。
func (s *TicketService) ResolveActions(ctx context.Context, access *admindomain.AccessContext,
	item *ticketdomain.Ticket, scope ticketdomain.Scope, isWatcher bool) *ticketdomain.ActionSet {

	if access == nil || item == nil {
		return &ticketdomain.ActionSet{}
	}
	if access.IsSuperAdmin {
		return &ticketdomain.ActionSet{
			View: true, Reply: true, InternalNote: true, ViewInternal: true, Edit: true,
			Assign: true, ChangeStatus: true, Close: true, Reopen: true, Delete: true,
			Watch: true, ManageWatchers: true, UploadAttachment: true,
		}
	}

	appID := item.AppID
	scoped := &appID
	related := personallyRelated(item, scope) || isWatcher
	terminal := ticketdomain.IsTerminal(item.Status)

	actions := &ticketdomain.ActionSet{}
	actions.View = visibleTo(item, scope) || related
	if !actions.View {
		return actions
	}

	handlePermitted := func(permission string) bool {
		return related || s.can(ctx, access, permission, scoped)
	}

	actions.Reply = !item.Locked && !terminal && handlePermitted(PermTicketReply)
	actions.ViewInternal = s.can(ctx, access, PermTicketInternal, scoped) || related
	actions.InternalNote = actions.ViewInternal && !terminal
	actions.Edit = handlePermitted(PermTicketWrite) && !terminal
	actions.Assign = s.can(ctx, access, PermTicketAssign, scoped) || s.isGroupLeaderOf(ctx, access.AdminID, item)
	actions.ChangeStatus = handlePermitted(PermTicketClose) && !terminal
	actions.Close = actions.ChangeStatus
	actions.Reopen = terminal && (s.can(ctx, access, PermTicketClose, scoped) || related)
	actions.Delete = s.can(ctx, access, PermTicketDelete, scoped)
	actions.Watch = actions.View
	actions.ManageWatchers = actions.Assign
	actions.UploadAttachment = actions.Reply || actions.InternalNote
	return actions
}

// isGroupLeaderOf 组负责人可以在组内自由转派。
func (s *TicketService) isGroupLeaderOf(ctx context.Context, adminID int64, item *ticketdomain.Ticket) bool {
	if item.GroupID == nil {
		return false
	}
	ok, err := s.pg.IsTicketGroupLeader(ctx, adminID, *item.GroupID)
	if err != nil {
		return false
	}
	return ok
}

// requireTicketAccess 取工单并校验可见性，返回工单 + 动作集。
func (s *TicketService) requireTicketAccess(ctx context.Context, access *admindomain.AccessContext, ticketID int64) (
	*ticketdomain.Ticket, ticketdomain.Scope, *ticketdomain.ActionSet, error) {

	scope, err := s.ResolveScope(ctx, access)
	if err != nil {
		return nil, scope, nil, err
	}
	item, err := s.pg.GetTicketByID(ctx, ticketID)
	if err != nil {
		return nil, scope, nil, err
	}
	if item == nil {
		return nil, scope, nil, apperrors.New(40460, http.StatusNotFound, "工单不存在")
	}
	isWatcher := false
	if !visibleTo(item, scope) {
		// 可能是关注人：单独查一次再判定
		watchers, werr := s.pg.ListTicketWatchers(ctx, ticketID)
		if werr == nil {
			for _, watcher := range watchers {
				if watcher.AdminID == access.AdminID {
					isWatcher = true
					break
				}
			}
		}
		if !isWatcher {
			return nil, scope, nil, apperrors.New(40313, http.StatusForbidden, "无权访问该工单")
		}
	}
	actions := s.ResolveActions(ctx, access, item, scope, isWatcher)
	return item, scope, actions, nil
}
