package service

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"aegis/internal/authz"
	admindomain "aegis/internal/domain/admin"
	systemdomain "aegis/internal/domain/system"
	apperrors "aegis/pkg/errors"
	"go.uber.org/zap"
)

// 管理端权限判定与「自助能力」。
//
// 这套 RBAC 有一个结构性的起点问题：自助注册出来的管理员**一条 assignment 都没有**，
// 于是 Authorize 对任何权限点都返回 40311。而唯一能把他从这个状态里带出来的动作 ——
// 建一个属于自己的应用、成为它的 app_admin —— 以前恰恰要求 app:write，
// 那是只有平台管理员才有的权限点。结果是一个死锁：
//
//	注册 → 什么都没有 → 想建应用 → 需要 app:write → 只有已经有权限的人才有
//	                     ↑                                        │
//	                     └────────────────────────────────────────┘
//
// 解法不是"给新用户发个角色"（那会让每个注册账号一上来就带着一份平台级授权），
// 而是把「建自己的第一个应用」从**权限点判定**里拿出来，归入**自助能力**：
// 它不读写任何既有租户的数据，产物只属于发起人。闸门换成配额与开关，
// 执行点只有一个（EnsureCanCreateApp），配置只有一处（platform_settings 的 admin.self_service）。
//
// 与之配套的另外两件事：
//   - 拒绝时必须说清楚缺什么（denyPermission）。"当前管理员无权执行此操作"
//     对着一个刚注册完的用户等于什么都没说，他既不知道缺哪个权限点，
//     也不知道该找谁要。
//   - 会话要能自报家门（EffectivePermissions）。只下发角色 key，控制台就得自己
//     维护一份"角色 → 权限"的副本，那份副本必然与后端漂移，
//     表现为"按钮在、点了 403"。

const (
	// PermissionAppRead 应用信息查看。
	PermissionAppRead = "app:read"
	// PermissionAppWrite 应用信息修改。
	//
	// 它同时是「常规授权能不能建应用」的判据：持有全局作用域的 app:write
	// 即为平台侧管理员，建应用不走自助配额。
	PermissionAppWrite = "app:write"
)

// SetPlatformSettings 注入平台设置服务（在 bootstrap 中调用，避免循环初始化）。
// 自助能力的开关与配额存在平台设置里，权限层要读它。
func (s *AdminService) SetPlatformSettings(settings *PlatformSettingsService) {
	s.settings = settings
}

// Authorize 管理端权限判定的唯一入口。
//
// 判定顺序固定：超管 → 无权限点要求 → 角色授权（Casbin，按作用域匹配）→ 临时权限。
// 全部落空时返回一条**说得出缺什么**的 403。
func (s *AdminService) Authorize(ctx context.Context, access *admindomain.AccessContext, permission string, appID *int64) error {
	if access == nil {
		return apperrors.New(40110, http.StatusUnauthorized, "管理员未认证")
	}
	// 超管短路仍留在 Go 里：IsSuperAdmin 是 admin_accounts 上的一列，
	// 每次请求随会话现查。把它做成策略会让"撤销超管身份"要等策略重载才生效。
	if access.IsSuperAdmin {
		return nil
	}
	if permission == "" {
		return nil
	}
	decision := s.authz.Decide(s.subjectsFor(access, appID), authz.DomainForApp(appID), permission)
	if decision.Allowed {
		return nil
	}
	// 授权引擎拒绝后，检查带过期时间的临时权限。
	//
	// 临时权限**刻意不进策略表**：策略是带缓存的（进程内 + 跨实例广播），
	// 而临时授权的价值恰恰在于"到点自动失效"。放进缓存意味着过期时刻起
	// 还有一个窗口它仍然有效，那正是这类授权最不能接受的性质。
	tempPerms, err := s.pg.GetActiveTempPermissions(ctx, access.AdminID)
	if err != nil {
		return err
	}
	for _, tp := range tempPerms {
		if scopeMatches(tp.AppID, appID) && tp.Permission == permission {
			return nil
		}
	}
	return denyPermission(access, permission, appID, decision)
}

// subjectsFor 组装一次判定的主体集合。
//
// 两类主体：管理员本人（直接授予/禁止的落点）与他**在该作用域下生效**的角色。
//
// 作用域过滤（scopeMatches）刻意留在 Go 里而不是做成 Casbin 的域内角色关系：
// 「谁有哪个角色、绑在哪个应用上」每次请求都随会话从库里现查，
// 灌进带缓存的策略表就会引入"撤销了角色但还能用一段时间"的窗口 ——
// 这是授权系统里最不该有的那种延迟。
func (s *AdminService) subjectsFor(access *admindomain.AccessContext, appID *int64) []string {
	subjects := make([]string, 0, len(access.Assignments)+1)
	subjects = append(subjects, authz.AdminSubject(access.AdminID))
	for _, assignment := range access.Assignments {
		if scopeMatches(assignment.AppID, appID) {
			subjects = append(subjects, authz.RoleSubject(assignment.RoleKey))
		}
	}
	return subjects
}

// HasPermission 是 Authorize 的布尔形态，供只需要分支不需要错误的调用方使用。
func (s *AdminService) HasPermission(ctx context.Context, access *admindomain.AccessContext, permission string, appID *int64) bool {
	return s.Authorize(ctx, access, permission, appID) == nil
}

// denyPermission 构造一条能照着办的 403。
//
// 旧文案是一句光秃秃的「当前管理员无权执行此操作」。它同时隐去了三件事：
// 缺的是哪个权限点、判定在哪个作用域、以及该怎么拿到 —— 于是排查只能靠翻代码，
// 而对最需要这条信息的人（刚注册、什么都没有的管理员）来说根本无从下手。
func denyPermission(access *admindomain.AccessContext, permission string, appID *int64, decision authz.Decision) error {
	scope := "平台级"
	if appID != nil {
		scope = fmt.Sprintf("应用 #%d", *appID)
	}
	label := permissionDisplayName(permission)

	// 被一条显式拒绝策略压住时，说清楚是"被禁止"而不是"没配到" ——
	// 两者的处理动作完全相反：前者要去找那条 deny，后者要去加授权。
	if decision.Effect == authz.EffectDeny {
		return apperrors.New(40311, http.StatusForbidden, fmt.Sprintf(
			"当前管理员被显式禁止执行此操作：「%s」（%s，作用域 %s）被策略 %v 拒绝。"+
				"这是一条人为配置的拒绝规则，需要在「角色与权限」里移除它才能恢复。",
			label, permission, scope, decision.Rule))
	}

	var hint string
	switch {
	case access != nil && len(access.Assignments) == 0:
		// 零角色账号：这是自助注册后的常态，单独给一句话说明该往哪走，
		// 否则用户只会反复重试同一个必然失败的操作。
		hint = "当前账号尚未被授予任何角色。请联系超级管理员为你授权；" +
			"若只是想管理自己的应用，可以先在「应用管理」里创建一个属于自己的应用。"
	case appID != nil:
		hint = "该权限需要绑定到这个应用的角色，请联系超级管理员为你的账号追加授权。"
	default:
		hint = "该权限只在平台级作用域下生效，应用级角色无法满足，请联系超级管理员授权。"
	}

	return apperrors.New(40311, http.StatusForbidden,
		fmt.Sprintf("当前管理员无权执行此操作：缺少「%s」（%s，作用域 %s）。%s", label, permission, scope, hint))
}

// permissionDisplayName 把权限代码翻成权限目录里的中文名，未登记时原样返回。
func permissionDisplayName(permission string) string { return authz.PermissionName(permission) }

// ── 生效权限展开 ──

// EffectivePermissions 展开一次会话在各作用域下的**生效**权限集。
//
// Apps 里的每一项是该应用下的完整集合（已并入全局权限），不是增量：
// scopeMatches 里"全局授权覆盖应用级操作"这条规则如果要调用方自己复现，
// 就等于把同一条判定写了两遍，而两遍迟早会不一致。
func (s *AdminService) EffectivePermissions(ctx context.Context, access *admindomain.AccessContext) admindomain.PermissionSnapshot {
	var temp []admindomain.TempPermission
	if access != nil && !access.IsSuperAdmin {
		var err error
		// 临时权限与角色授权同权：判定读它，展开也必须读它，
		// 否则控制台会把一个临时授权期内确实能点的按钮藏起来。
		if temp, err = s.pg.GetActiveTempPermissions(ctx, access.AdminID); err != nil {
			s.log.Warn("临时权限读取失败，生效权限快照按角色授权计算", zap.Int64("adminId", access.AdminID), zap.Error(err))
		}
	}
	return s.expandPermissions(access, temp)
}

// expandPermissions 走**与真实判定完全相同的那段代码**把权限集展开。
//
// 这一点是刻意的，也是这次重构解决的一类老问题：以前展开是"把角色的权限列表
// 并起来"，判定是"跑 Casbin"，两段各写一遍。现在角色权限里可以写 `content:*`
// 这样的前缀通配，还可能被一条 deny 压掉、或经由 base_role 继承而来 ——
// 任何自行并集的实现都会与判定结果对不上，表现为控制台显示的按钮点了才 403
// （或者更糟：真正有权限的按钮被藏起来）。
func (s *AdminService) expandPermissions(access *admindomain.AccessContext, temp []admindomain.TempPermission) admindomain.PermissionSnapshot {
	snapshot := admindomain.PermissionSnapshot{Global: []string{}, Apps: map[string][]string{}}
	if access == nil {
		return snapshot
	}
	catalog := authz.AllPermissionCodes()
	if access.IsSuperAdmin {
		// 超管不走权限点判定，枚举一份"全部权限"只会让调用方以为它是可撤销的集合。
		snapshot.All = true
		snapshot.Global = append([]string(nil), catalog...)
		sort.Strings(snapshot.Global)
		return snapshot
	}

	// 全局作用域
	snapshot.Global = mergeTempPermissions(
		s.authz.PermissionsFor(s.subjectsFor(access, nil), authz.PlatformDomain, catalog), temp, nil)

	// 每个"这个人在里面有点什么"的应用各展开一次。Apps[x] 是该应用下的**完整**集合
	// （全局授权在 subjectsFor 里已经并进去了），不是增量：
	// 让调用方自己复现"全局覆盖应用级"这条规则等于把同一条判定写两遍。
	//
	// 应用清单要同时取自角色分配**与**临时权限：一次临时授权完全可能落在
	// 一个此人没有任何角色的应用上（那正是临时授权的典型用法 ——
	// 「临时进去处理一下」），只扫角色分配会让那个应用整个不出现在快照里，
	// 于是控制台把他确实能点的按钮全藏了。
	for _, appID := range scopedAppIDs(access.Assignments, temp) {
		permissions := s.authz.PermissionsFor(s.subjectsFor(access, &appID), authz.AppDomain(appID), catalog)
		snapshot.Apps[strconv.FormatInt(appID, 10)] = mergeTempPermissions(permissions, temp, &appID)
	}
	return snapshot
}

// scopedAppIDs 汇总该管理员涉及的应用 ID（角色分配 + 临时权限），有序去重。
func scopedAppIDs(assignments []admindomain.Assignment, temp []admindomain.TempPermission) []int64 {
	seen := map[int64]struct{}{}
	ids := make([]int64, 0, len(assignments)+len(temp))
	add := func(appID *int64) {
		if appID == nil || *appID <= 0 {
			return
		}
		if _, ok := seen[*appID]; ok {
			return
		}
		seen[*appID] = struct{}{}
		ids = append(ids, *appID)
	}
	for _, assignment := range assignments {
		add(assignment.AppID)
	}
	for _, tp := range temp {
		add(tp.AppID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// mergeTempPermissions 把该作用域下生效的临时权限并进集合。
//
// 临时权限不进策略表（见 Authorize 里的说明），因此展开时要单独并一次 ——
// 漏掉它，控制台会把一个临时授权期内确实能点的按钮藏起来。
func mergeTempPermissions(permissions []string, temp []admindomain.TempPermission, appID *int64) []string {
	if len(temp) == 0 {
		return permissions
	}
	set := make(map[string]bool, len(permissions)+len(temp))
	for _, item := range permissions {
		set[item] = true
	}
	for _, tp := range temp {
		if scopeMatches(tp.AppID, appID) {
			set[tp.Permission] = true
		}
	}
	return sortedPermissionCodes(set)
}

func sortedPermissionCodes(set map[string]bool) []string {
	items := make([]string, 0, len(set))
	for code, ok := range set {
		if ok {
			items = append(items, code)
		}
	}
	sort.Strings(items)
	return items
}

// ── 自助能力 ──

// selfServiceAppDecision 是「能不能自助建应用」这一问的完整答案。
type selfServiceAppDecision struct {
	Allowed bool
	// Privileged 为真表示走的是 app:write 常规授权，配额字段不参与判定。
	Privileged bool
	Code       int
	Message    string
}

// evaluateSelfServiceAppCreation 是自助建应用的**唯一**判定逻辑，写成纯函数。
//
// 判定与取数分开：取数要打两次库（配置 + 已建数量），而这段规则本身
// 是这次改动里最需要被测试直接盯着的部分 —— 配额差一个就是"永远建不了"
// 或者"配额形同虚设"。
func evaluateSelfServiceAppCreation(policy systemdomain.SelfServiceSettingsView, privileged bool, appsCreated int) selfServiceAppDecision {
	if privileged {
		return selfServiceAppDecision{Allowed: true, Privileged: true}
	}
	if !policy.AllowAppCreation {
		return selfServiceAppDecision{
			Code: 40318,
			Message: "平台已关闭自助创建应用。请联系超级管理员为你的账号授予应用权限，" +
				"或由他代为创建应用后把你加为应用管理员。",
		}
	}
	if policy.MaxAppsPerAdmin > 0 && appsCreated >= policy.MaxAppsPerAdmin {
		return selfServiceAppDecision{
			Code: 40319,
			Message: fmt.Sprintf("自助创建的应用数已达上限（%d 个，已创建 %d 个）。"+
				"如需更多应用，请联系超级管理员提高上限或直接为你授权。", policy.MaxAppsPerAdmin, appsCreated),
		}
	}
	return selfServiceAppDecision{Allowed: true}
}

// selfServicePolicy 读取自助能力配置；未注入设置服务时按默认值放行。
func (s *AdminService) selfServicePolicy(ctx context.Context) systemdomain.SelfServiceSettingsView {
	if s.settings == nil {
		return selfServiceDefaults
	}
	return s.settings.GetSelfServiceConfig(ctx)
}

// decideSelfServiceAppCreation 取齐事实后做一次判定。
func (s *AdminService) decideSelfServiceAppCreation(ctx context.Context, access *admindomain.AccessContext) (selfServiceAppDecision, systemdomain.SelfServiceSettingsView, int) {
	policy := s.selfServicePolicy(ctx)
	if access == nil {
		return selfServiceAppDecision{Code: 40110, Message: "管理员未认证"}, policy, 0
	}
	// 持有全局 app:write 的（超管 / 平台管理员）不受自助配额约束 ——
	// 那是常规授权路径，配额管的是"没有授权的人"。
	if s.HasPermission(ctx, access, PermissionAppWrite, nil) {
		return selfServiceAppDecision{Allowed: true, Privileged: true}, policy, 0
	}
	created, err := s.pg.CountAppsCreatedBy(ctx, access.AdminID)
	if err != nil {
		// 数不出来就不放行：数不出来时放行等于配额在库抖动期间失效，
		// 而这条链路是**免授权**的，失效的代价是任何注册账号都能刷应用。
		s.log.Warn("自助建应用配额统计失败", zap.Int64("adminId", access.AdminID), zap.Error(err))
		return selfServiceAppDecision{Code: 50031, Message: "暂时无法确认自助创建配额，请稍后重试"}, policy, 0
	}
	return evaluateSelfServiceAppCreation(policy, false, created), policy, created
}

// EnsureCanCreateApp 是自助建应用的执行点：不通过就返回一条说得出原因的错误。
//
// 中间件对 POST /api/admin/apps 不再要求权限点（要求 app:write 会让新账号
// 永远建不了第一个应用），闸门整个落在这里。
func (s *AdminService) EnsureCanCreateApp(ctx context.Context, access *admindomain.AccessContext) error {
	decision, _, _ := s.decideSelfServiceAppCreation(ctx, access)
	if decision.Allowed {
		return nil
	}
	status := http.StatusForbidden
	if decision.Code >= 50000 {
		status = http.StatusServiceUnavailable
	} else if decision.Code == 40110 {
		status = http.StatusUnauthorized
	}
	return apperrors.New(decision.Code, status, decision.Message)
}

// SelfServiceState 汇报当前账号的自助能力，供 /me 下发给控制台。
//
// 控制台据此决定「新建应用」按钮是显示、置灰还是隐藏，并把 Reason 直接摆出来。
// 少了它，前端只能让用户填完整张表单再拿一句 403 —— 也就是截图里的那一幕。
func (s *AdminService) SelfServiceState(ctx context.Context, access *admindomain.AccessContext) admindomain.SelfServiceState {
	decision, policy, created := s.decideSelfServiceAppCreation(ctx, access)
	state := admindomain.SelfServiceState{
		CanCreateApp: decision.Allowed,
		Privileged:   decision.Privileged,
		AppQuota:     policy.MaxAppsPerAdmin,
		AppsCreated:  created,
		Reason:       decision.Message,
	}
	if decision.Privileged {
		// 走常规授权时配额没有意义，摆出来只会让人以为自己受限于它。
		state.AppQuota = 0
		state.AppsCreated = 0
	}
	return state
}

// SelfServiceCreatorRole 创建者自动获得的应用级角色。
//
// 配置值不是 scope=app 的角色时回落到 app_admin：把一个全局角色发给
// "建了个应用的人"等于一次静默提权，而拒绝创建又会把人卡在零权限里。
func (s *AdminService) SelfServiceCreatorRole(ctx context.Context) string {
	roleKey := strings.TrimSpace(s.selfServicePolicy(ctx).CreatorRoleKey)
	if role, ok := s.roles[roleKey]; ok && role.Scope == "app" {
		return roleKey
	}
	s.rolesMu.RLock()
	role, ok := s.customRoles[roleKey]
	s.rolesMu.RUnlock()
	if ok && role.Scope == "app" {
		return roleKey
	}
	if roleKey != "" && roleKey != selfServiceDefaults.CreatorRoleKey {
		s.log.Warn("自助创建者角色无效，回落到 app_admin", zap.String("roleKey", roleKey))
	}
	return selfServiceDefaults.CreatorRoleKey
}

// RegistrationEnabled 自助注册开关的读取入口（登录页与注册路由共用同一份判定）。
func (s *AdminService) RegistrationEnabled(ctx context.Context) bool {
	return s.selfServicePolicy(ctx).AllowRegistration
}

// AccessSnapshot 组装 /api/admin/auth/me 的完整回包。
func (s *AdminService) AccessSnapshot(ctx context.Context, access *admindomain.AccessContext) *admindomain.AccessSnapshot {
	if access == nil {
		return nil
	}
	return &admindomain.AccessSnapshot{
		AccessContext: *access,
		Permissions:   s.EffectivePermissions(ctx, access),
		SelfService:   s.SelfServiceState(ctx, access),
	}
}
