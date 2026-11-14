package service

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"aegis/internal/authz"
	admindomain "aegis/internal/domain/admin"
	apperrors "aegis/pkg/errors"
	"go.uber.org/zap"
)

// 角色管理。
//
// 角色分两类：
//   - 内置角色：定义在代码里（authz/roles.go），每次启动整组重刷，保证升级能到达所有部署。
//     不可删、不可停用；调整走 override（额外允许 / 显式禁止），启动时不会被冲掉。
//   - 自定义角色：admin_roles 表，可编辑允许 / 禁止、继承、数据权限、状态与排序。
//
// 角色的权限在策略表里（authz_policies），这里缓存的只是展示与数据权限要用的元数据。
// 多实例部署下元数据靠引擎的重载钩子跟着策略一起刷新（见 SetupRoleSync）。

var customRoleKeyPattern = regexp.MustCompile(`^custom_[a-z0-9_]{2,56}$`)

// SetupRoleSync 让角色元数据缓存跟着策略重载刷新。
//
// 角色的增删改都会写策略并广播；其余实例收到广播重载策略时，若不顺带刷新元数据，
// 就会出现「策略是新的、数据权限与停用状态还是旧的」—— 比如某个角色在 A 实例上已停用，
// B 实例照样按它展开数据权限。
func (s *AdminService) SetupRoleSync() {
	s.authz.OnReload(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.syncCustomRoles(ctx, false); err != nil {
			s.log.Warn("策略重载后刷新自定义角色元数据失败", zap.Error(err))
		}
	})
}

// roleMeta 角色元数据（内置或自定义）。
func (s *AdminService) roleMeta(roleKey string) (admindomain.RoleDefinition, bool) {
	if role, ok := s.roles[roleKey]; ok {
		if role.DataScope == "" {
			role.DataScope = admindomain.DataScopeAssigned
		}
		if role.Status == "" {
			role.Status = admindomain.RoleStatusActive
		}
		return role, true
	}
	s.rolesMu.RLock()
	defer s.rolesMu.RUnlock()
	role, ok := s.customRoles[roleKey]
	return role, ok
}

// customRoleAppNames 数据权限「指定应用」展开时的应用名缓存（随元数据一起刷新）。
func (s *AdminService) customRoleAppName(appID int64) string {
	s.rolesMu.RLock()
	defer s.rolesMu.RUnlock()
	return s.dataAppNames[appID]
}

// accessFromProfile 由账号资料组装一次判定用的访问上下文（分配按数据权限展开）。
func (s *AdminService) accessFromProfile(ctx context.Context, profile *admindomain.Profile) *admindomain.AccessContext {
	return &admindomain.AccessContext{
		Session: admindomain.Session{
			AdminID:       profile.Account.ID,
			Account:       profile.Account.Account,
			DisplayName:   profile.Account.DisplayName,
			IsSuperAdmin:  profile.Account.IsSuperAdmin,
			IsSystemAdmin: profile.Account.IsSystemAdmin,
			Tier:          admindomain.TierOf(profile.Account.IsSuperAdmin, profile.Account.IsSystemAdmin),
		},
		Assignments: s.resolveAssignments(ctx, profile.Account.ID, profile.Assignments),
	}
}

// resolveAssignments 把库里的角色分配按角色的数据权限展开成「在哪些应用生效」。
//
// 展开放在会话组装这一处，下游（判定、应用列表过滤、工单可见范围、治理视图……）
// 拿到的就已经是具体应用，不必每处各自理解一遍数据权限。
// 停用的角色在这里整个丢掉：它的授权关系保留，但不贡献任何权限与可见范围。
func (s *AdminService) resolveAssignments(ctx context.Context, adminID int64, raw []admindomain.Assignment) []admindomain.Assignment {
	out := make([]admindomain.Assignment, 0, len(raw))
	seen := map[string]bool{}
	add := func(item admindomain.Assignment) {
		key := item.RoleKey + ":" + fmt.Sprint(item.AllApps)
		if item.AppID != nil {
			key += ":" + strconvInt64(*item.AppID)
		}
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, item)
	}
	var created []admindomain.Assignment
	createdLoaded := false
	for _, item := range raw {
		role, ok := s.roleMeta(item.RoleKey)
		if !ok {
			add(item) // 角色已删：策略为空，保留分配只为让「为什么没权限」查得到
			continue
		}
		if role.Status == admindomain.RoleStatusDisabled {
			continue
		}
		if role.Scope == "global" {
			item.AppID, item.AppName = nil, ""
			add(item)
			continue
		}
		scope := role.DataScope
		if scope == "" {
			scope = admindomain.DataScopeAssigned
		}
		switch scope {
		case admindomain.DataScopeAll:
			add(admindomain.Assignment{RoleKey: item.RoleKey, AllApps: true, DataScope: scope})
		case admindomain.DataScopeCreated:
			if !createdLoaded {
				createdLoaded = true
				var err error
				if created, err = s.pg.ListAppIDsCreatedBy(ctx, adminID); err != nil {
					s.log.Warn("数据权限「本人创建」展开失败，该角色本次不生效", zap.Int64("adminId", adminID), zap.Error(err))
				}
			}
			for _, app := range created {
				add(admindomain.Assignment{RoleKey: item.RoleKey, AppID: app.AppID, AppName: app.AppName, DataScope: scope})
			}
		case admindomain.DataScopeCustom:
			for _, appID := range role.DataAppIDs {
				id := appID
				add(admindomain.Assignment{RoleKey: item.RoleKey, AppID: &id, AppName: s.customRoleAppName(id), DataScope: scope})
			}
		default:
			if item.AppID == nil {
				continue // 「按授权绑定」却没有绑定应用：历史脏数据，不能当成全局授权
			}
			item.DataScope = admindomain.DataScopeAssigned
			add(item)
		}
	}
	return out
}

// ── 列表 / 详情 ──

// ListRoles 全部角色，`Permissions` 是展开后的生效集合（与真实判定同一段代码）。
func (s *AdminService) ListRoles() []admindomain.RoleDefinition {
	return s.listRoles(context.Background())
}

func (s *AdminService) listRoles(ctx context.Context) []admindomain.RoleDefinition {
	items := make([]admindomain.RoleDefinition, 0, len(s.roles)+len(s.customRoles))
	for key := range s.roles {
		role, _ := s.roleMeta(key)
		role.Allow = append([]string(nil), role.Permissions...)
		role.Deny = []string{}
		role.DataAppIDs = []int64{}
		items = append(items, role)
	}
	s.rolesMu.RLock()
	for _, item := range s.customRoles {
		items = append(items, item)
	}
	s.rolesMu.RUnlock()
	counts := map[string]int{}
	if s.pg != nil {
		var err error
		if counts, err = s.pg.CountRoleMembers(ctx); err != nil {
			s.log.Warn("角色成员数统计失败", zap.Error(err))
		}
	}
	catalog := authz.AllPermissionCodes()
	for i := range items {
		items[i].Permissions = s.roleEffective(items[i].Key, items[i].Scope, catalog)
		items[i].MemberCount = counts[items[i].Key]
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].IsCustom != items[j].IsCustom {
			return !items[i].IsCustom
		}
		if items[i].SortOrder != items[j].SortOrder {
			return items[i].SortOrder < items[j].SortOrder
		}
		if items[i].Level != items[j].Level {
			return items[i].Level > items[j].Level
		}
		return items[i].Key < items[j].Key
	})
	return items
}

// roleEffective 按角色作用域展开生效权限点：应用级角色看应用域，平台级角色两个域取并集。
func (s *AdminService) roleEffective(roleKey, scope string, catalog []string) []string {
	subjects := []string{authz.RoleSubject(roleKey)}
	items := s.authz.PermissionsFor(subjects, authz.AppDomain(1), catalog)
	if scope == "global" {
		items = append(items, s.authz.PermissionsFor(subjects, authz.PlatformDomain, catalog)...)
	}
	return dedupeSorted(items)
}

func dedupeSorted(items []string) []string {
	set := make(map[string]bool, len(items))
	for _, item := range items {
		set[item] = true
	}
	return sortedPermissionCodes(set)
}

// GetRoleDetail 角色编辑器的一次完整取数。
func (s *AdminService) GetRoleDetail(ctx context.Context, actor *admindomain.AccessContext, roleKey string) (*admindomain.RoleDetail, error) {
	roleKey = strings.TrimSpace(roleKey)
	var role admindomain.RoleDefinition
	found := false
	for _, item := range s.listRoles(ctx) {
		if item.Key == roleKey {
			role, found = item, true
			break
		}
	}
	if !found {
		return nil, apperrors.New(40463, http.StatusNotFound, "角色不存在")
	}
	detail := &admindomain.RoleDetail{
		Role:          role,
		Builtin:       !role.IsCustom,
		OverrideAllow: []string{},
		OverrideDeny:  []string{},
		Effective:     role.Permissions,
		Legacy:        []string{},
		Editable:      true,
	}
	rules, err := s.pg.ListAuthzPoliciesBySubject(ctx, authz.RoleSubject(roleKey))
	if err != nil {
		return nil, err
	}
	legacy := map[string]bool{}
	for _, rule := range rules {
		if rule.PType != "p" || len(rule.Values) < 4 {
			continue
		}
		code := rule.Values[2]
		if authz.IsLegacyPermission(code) {
			legacy[code] = true
		}
		if rule.Source != authz.SourceOverride {
			continue
		}
		if rule.Values[3] == authz.EffectDeny {
			detail.OverrideDeny = append(detail.OverrideDeny, code)
		} else {
			detail.OverrideAllow = append(detail.OverrideAllow, code)
		}
	}
	for code := range legacy {
		detail.Legacy = append(detail.Legacy, code)
	}
	sort.Strings(detail.Legacy)
	if detail.Members, err = s.pg.ListRoleMembers(ctx, roleKey); err != nil {
		return nil, err
	}
	if roleKey == authz.RoleSuperAdmin {
		detail.Editable, detail.Reason = false, "超级管理员不走权限点判定，不可编辑"
	} else if actor != nil && !s.HasPermission(ctx, actor, authz.PermSystemRoleEdit, nil) {
		detail.Editable, detail.Reason = false, "缺少「角色权限：编辑」权限"
	}
	return detail, nil
}

// ListRoleMembers 持有某角色的全部授权。
func (s *AdminService) ListRoleMembers(ctx context.Context, roleKey string) ([]admindomain.RoleMember, error) {
	if !s.roleExists(roleKey) {
		return nil, apperrors.New(40463, http.StatusNotFound, "角色不存在")
	}
	return s.pg.ListRoleMembers(ctx, roleKey)
}

// ListRolesWithPermissionTree 兼容旧接口：每个角色带一份扁平分组的权限勾选状态。
func (s *AdminService) ListRolesWithPermissionTree() []admindomain.RoleWithPermissions {
	roles := s.ListRoles()
	allGroups := allPermissionGroups()
	result := make([]admindomain.RoleWithPermissions, 0, len(roles))
	for _, role := range roles {
		granted := make(map[string]bool, len(role.Permissions))
		for _, p := range role.Permissions {
			granted[p] = true
		}
		groups := make([]admindomain.PermissionGroup, 0, len(allGroups))
		for _, g := range allGroups {
			perms := make([]admindomain.Permission, len(g.Permissions))
			copy(perms, g.Permissions)
			for i := range perms {
				if granted[perms[i].Code] {
					perms[i].Description = "granted"
				}
			}
			groups = append(groups, admindomain.PermissionGroup{Key: g.Key, Name: g.Name, Permissions: perms})
		}
		result = append(result, admindomain.RoleWithPermissions{RoleDefinition: role, PermissionGroups: groups})
	}
	return result
}

// ── 元数据同步 ──

// LoadCustomRoles 装载自定义角色元数据，并把它们投影成策略。
func (s *AdminService) LoadCustomRoles(ctx context.Context) error {
	return s.syncCustomRoles(ctx, true)
}

func (s *AdminService) syncCustomRoles(ctx context.Context, syncPolicies bool) error {
	customRoles, err := s.pg.ListCustomRoles(ctx)
	if err != nil {
		return err
	}
	customMap := make(map[string]admindomain.RoleDefinition, len(customRoles))
	appIDs := make([]int64, 0)
	for _, cr := range customRoles {
		updated := cr.UpdatedAt
		customMap[cr.RoleKey] = admindomain.RoleDefinition{
			Key: cr.RoleKey, Name: cr.Name, Description: cr.Description,
			Level: cr.Level, Scope: cr.Scope, Permissions: cr.Permissions,
			IsCustom: true, BaseRole: cr.BaseRole, CreatedBy: cr.CreatedBy,
			Status: defaultString(cr.Status, admindomain.RoleStatusActive), SortOrder: cr.SortOrder,
			DataScope: defaultString(cr.DataScope, admindomain.DataScopeAssigned), DataAppIDs: nonNilIDs(cr.DataAppIDs),
			Allow: nonNilStrings(cr.Permissions), Deny: nonNilStrings(cr.Deny), UpdatedAt: &updated,
		}
		appIDs = append(appIDs, cr.DataAppIDs...)
		if syncPolicies {
			if err := s.writeCustomRolePolicy(ctx, cr, nil); err != nil {
				return err
			}
		}
	}
	names, err := s.pg.AppNamesByIDs(ctx, appIDs)
	if err != nil {
		s.log.Warn("数据权限应用名读取失败", zap.Error(err))
		names = map[int64]string{}
	}
	s.rolesMu.Lock()
	s.customRoles = customMap
	s.dataAppNames = names
	s.rolesMu.Unlock()
	return nil
}

// writeCustomRolePolicy 把一个自定义角色写成策略。停用的角色写成空集（含继承边）：
// 授权关系保留，但它不贡献任何权限，且任何继承它的角色也拿不到它的权限。
func (s *AdminService) writeCustomRolePolicy(ctx context.Context, role admindomain.CustomRole, updatedBy *int64) error {
	policy := authz.RolePolicy{Note: role.Name}
	if role.Status != admindomain.RoleStatusDisabled {
		policy.Allow = role.Permissions
		policy.Deny = role.Deny
		if base := strings.TrimSpace(role.BaseRole); base != "" && base != role.RoleKey {
			policy.Inherits = []string{authz.RoleSubject(base)}
		}
	}
	return s.authz.SetRolePolicy(ctx, authz.SourceCustom, authz.RoleSubject(role.RoleKey), policy, updatedBy)
}

// ── 增删改 ──

func (s *AdminService) validateRoleShape(scope, dataScope string, dataApps []int64, baseRole, selfKey string) error {
	if scope != "global" && scope != "app" {
		return apperrors.New(40069, http.StatusBadRequest, "角色作用域只能是 global 或 app")
	}
	if dataScope == "" {
		dataScope = admindomain.DataScopeAssigned
	}
	if !admindomain.ValidDataScope(dataScope) {
		return apperrors.New(40070, http.StatusBadRequest, "无效的数据权限")
	}
	if scope == "global" && dataScope != admindomain.DataScopeAssigned {
		return apperrors.New(40070, http.StatusBadRequest, "平台级角色对全部应用生效，不能再设置数据权限")
	}
	if dataScope == admindomain.DataScopeCustom && len(dataApps) == 0 {
		return apperrors.New(40070, http.StatusBadRequest, "数据权限为「指定应用」时至少选择一个应用")
	}
	if base := strings.TrimSpace(baseRole); base != "" {
		if base == selfKey {
			return apperrors.New(40064, http.StatusBadRequest, "角色不能继承自己")
		}
		if !s.roleExists(base) {
			return apperrors.New(40063, http.StatusBadRequest, "父角色不存在："+base)
		}
		if base == authz.RoleSuperAdmin {
			return apperrors.New(40063, http.StatusBadRequest, "不能继承超级管理员")
		}
		if selfKey != "" && s.inheritsFrom(base, selfKey) {
			return apperrors.New(40064, http.StatusBadRequest, "继承关系成环：父角色已经直接或间接继承了本角色")
		}
	}
	return nil
}

// inheritsFrom 角色 child 是否（直接或间接）继承了 ancestor。
func (s *AdminService) inheritsFrom(child, ancestor string) bool {
	visited := map[string]bool{}
	current := child
	for current != "" && !visited[current] {
		if current == ancestor {
			return true
		}
		visited[current] = true
		role, ok := s.roleMeta(current)
		if !ok {
			return false
		}
		current = strings.TrimSpace(role.BaseRole)
	}
	return false
}

func normalizeRoleStatus(status string) (string, error) {
	switch strings.TrimSpace(status) {
	case "", admindomain.RoleStatusActive:
		return admindomain.RoleStatusActive, nil
	case admindomain.RoleStatusDisabled:
		return admindomain.RoleStatusDisabled, nil
	}
	return "", apperrors.New(40071, http.StatusBadRequest, "角色状态只能是 active 或 disabled")
}

func (s *AdminService) CreateCustomRole(ctx context.Context, actor *admindomain.AccessContext, input admindomain.CreateCustomRoleInput) (*admindomain.CustomRole, error) {
	input.RoleKey = strings.TrimSpace(input.RoleKey)
	input.Name = strings.TrimSpace(input.Name)
	if !customRoleKeyPattern.MatchString(input.RoleKey) {
		return nil, apperrors.New(40058, http.StatusBadRequest, "角色标识必须以 custom_ 开头，只含小写字母、数字与下划线")
	}
	if input.Name == "" {
		return nil, apperrors.New(40072, http.StatusBadRequest, "角色名称不能为空")
	}
	if input.Level < 1 || input.Level >= 20 {
		return nil, apperrors.New(40059, http.StatusBadRequest, "自定义角色级别必须在 1-19 之间")
	}
	if _, ok := s.roles[input.RoleKey]; ok {
		return nil, apperrors.New(40960, http.StatusConflict, "角色标识与内置角色冲突")
	}
	if input.DataScope == "" {
		input.DataScope = admindomain.DataScopeAssigned
	}
	var err error
	if input.Status, err = normalizeRoleStatus(input.Status); err != nil {
		return nil, err
	}
	if err := s.validateRoleShape(input.Scope, input.DataScope, input.DataAppIDs, input.BaseRole, input.RoleKey); err != nil {
		return nil, err
	}
	if err := validatePermissionPatterns(append(append([]string{}, input.Permissions...), input.Deny...)); err != nil {
		return nil, err
	}
	if err := s.ensureRolePermissionsGrantable(ctx, actor, input.Scope, input.Permissions, input.BaseRole); err != nil {
		return nil, err
	}
	var createdBy int64
	if actor != nil {
		createdBy = actor.AdminID
	}
	cr, err := s.pg.CreateCustomRole(ctx, input, createdBy)
	if err != nil {
		return nil, err
	}
	// 策略写入失败必须上抛：角色建出来却没有任何策略，表现是「勾了一堆权限、授给谁都不生效」。
	if err := s.writeCustomRolePolicy(ctx, *cr, &createdBy); err != nil {
		return nil, err
	}
	if err := s.syncCustomRoles(ctx, false); err != nil {
		s.log.Warn("自定义角色元数据刷新失败", zap.Error(err))
	}
	return cr, nil
}

// CopyRole 以任意角色（含内置）为模板复制出一个自定义角色：权限按展开后的生效集复制。
func (s *AdminService) CopyRole(ctx context.Context, actor *admindomain.AccessContext, sourceKey, newKey, newName string) (*admindomain.CustomRole, error) {
	source, ok := s.roleMeta(strings.TrimSpace(sourceKey))
	if !ok {
		return nil, apperrors.New(40463, http.StatusNotFound, "角色不存在")
	}
	if source.Key == authz.RoleSuperAdmin {
		return nil, apperrors.New(40073, http.StatusBadRequest, "超级管理员不能复制")
	}
	level := source.Level
	if level >= 20 {
		level = 19
	}
	if level < 1 {
		level = 1
	}
	input := admindomain.CreateCustomRoleInput{
		RoleKey: newKey, Name: newName, Description: source.Description, Level: level,
		Scope:       source.Scope,
		Permissions: s.roleEffective(source.Key, source.Scope, authz.AllPermissionCodes()),
		Deny:        []string{},
		Status:      admindomain.RoleStatusActive,
		SortOrder:   source.SortOrder + 1,
		DataScope:   defaultString(source.DataScope, admindomain.DataScopeAssigned),
		DataAppIDs:  source.DataAppIDs,
	}
	return s.CreateCustomRole(ctx, actor, input)
}

func (s *AdminService) UpdateCustomRole(ctx context.Context, actor *admindomain.AccessContext, roleKey string, input admindomain.UpdateCustomRoleInput) (*admindomain.CustomRole, error) {
	role, ok := s.roleMeta(roleKey)
	if !ok || !role.IsCustom {
		return nil, apperrors.New(40061, http.StatusBadRequest, "仅可编辑自定义角色")
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return nil, apperrors.New(40072, http.StatusBadRequest, "角色名称不能为空")
	}
	if input.Level < 1 || input.Level >= 20 {
		return nil, apperrors.New(40059, http.StatusBadRequest, "自定义角色级别必须在 1-19 之间")
	}
	if input.DataScope == "" {
		input.DataScope = admindomain.DataScopeAssigned
	}
	var err error
	if input.Status, err = normalizeRoleStatus(input.Status); err != nil {
		return nil, err
	}
	baseRole := role.BaseRole
	if input.BaseRole != nil {
		baseRole = *input.BaseRole
	}
	if err := s.validateRoleShape(role.Scope, input.DataScope, input.DataAppIDs, baseRole, roleKey); err != nil {
		return nil, err
	}
	if err := validatePermissionPatterns(append(append([]string{}, input.Permissions...), input.Deny...)); err != nil {
		return nil, err
	}
	// 授权上限只核对**新增**的允许项：既有的那部分操作者本来就动不了，不该挡住他改其余内容
	added := make([]string, 0)
	previous := map[string]bool{}
	for _, code := range role.Allow {
		for _, expanded := range authz.ExpandPattern(code) {
			previous[expanded] = true
		}
	}
	for _, pattern := range input.Permissions {
		for _, code := range authz.ExpandPattern(pattern) {
			if !previous[code] {
				added = append(added, code)
			}
		}
	}
	newBase := ""
	if strings.TrimSpace(baseRole) != strings.TrimSpace(role.BaseRole) {
		newBase = baseRole
	}
	if err := s.ensureRolePermissionsGrantable(ctx, actor, role.Scope, added, newBase); err != nil {
		return nil, err
	}
	var updatedBy *int64
	if actor != nil {
		updatedBy = &actor.AdminID
	}
	cr, err := s.pg.UpdateCustomRole(ctx, roleKey, input, updatedBy)
	if err != nil {
		return nil, err
	}
	if err := s.writeCustomRolePolicy(ctx, *cr, updatedBy); err != nil {
		return nil, err
	}
	if err := s.syncCustomRoles(ctx, false); err != nil {
		s.log.Warn("自定义角色元数据刷新失败", zap.Error(err))
	}
	return cr, nil
}

// SetRoleStatus 启用 / 停用自定义角色。内置角色不可停用（停掉 app_admin 等于把全部应用管理员一起锁死）。
func (s *AdminService) SetRoleStatus(ctx context.Context, actor *admindomain.AccessContext, roleKey, status string) error {
	role, ok := s.roleMeta(roleKey)
	if !ok {
		return apperrors.New(40463, http.StatusNotFound, "角色不存在")
	}
	if !role.IsCustom {
		return apperrors.New(40074, http.StatusBadRequest, "内置角色不可停用")
	}
	status, err := normalizeRoleStatus(status)
	if err != nil {
		return err
	}
	var updatedBy *int64
	if actor != nil {
		updatedBy = &actor.AdminID
	}
	if err := s.pg.SetCustomRoleStatus(ctx, roleKey, status, updatedBy); err != nil {
		return err
	}
	cr, err := s.pg.GetCustomRole(ctx, roleKey)
	if err != nil || cr == nil {
		return err
	}
	if err := s.writeCustomRolePolicy(ctx, *cr, updatedBy); err != nil {
		return err
	}
	return s.syncCustomRoles(ctx, false)
}

// SetRolePermissions 角色编辑器保存「功能权限」页签：自定义角色改定义，内置角色改 override。
//
// 内置角色的 allow 是代码定义的下限，界面上锁定为已勾选；这里收到的 allow 只取超出定义的部分
// 写进 override，deny 原样写进 override（压过定义）。
func (s *AdminService) SetRolePermissions(ctx context.Context, actor *admindomain.AccessContext, roleKey string, allow, deny []string) error {
	role, ok := s.roleMeta(roleKey)
	if !ok {
		return apperrors.New(40463, http.StatusNotFound, "角色不存在")
	}
	if roleKey == authz.RoleSuperAdmin {
		return apperrors.New(40073, http.StatusBadRequest, "超级管理员不走权限点判定，不可编辑")
	}
	if role.IsCustom {
		cr, err := s.pg.GetCustomRole(ctx, roleKey)
		if err != nil {
			return err
		}
		if cr == nil {
			return apperrors.New(40461, http.StatusNotFound, "自定义角色不存在")
		}
		baseRole := cr.BaseRole
		_, err = s.UpdateCustomRole(ctx, actor, roleKey, admindomain.UpdateCustomRoleInput{
			Name: cr.Name, Description: cr.Description, Level: cr.Level,
			Permissions: allow, Deny: deny, BaseRole: &baseRole,
			Status: cr.Status, SortOrder: cr.SortOrder, DataScope: cr.DataScope, DataAppIDs: cr.DataAppIDs,
		})
		return err
	}
	base := map[string]bool{}
	for _, pattern := range authz.BuiltinRoles()[roleKey].Permissions {
		for _, code := range authz.ExpandPattern(pattern) {
			base[code] = true
		}
	}
	extra := make([]string, 0)
	for _, pattern := range allow {
		for _, code := range authz.ExpandPattern(pattern) {
			if !base[code] {
				extra = append(extra, code)
			}
		}
	}
	extra = dedupeSorted(extra)
	if err := validatePermissionPatterns(append(append([]string{}, extra...), deny...)); err != nil {
		return err
	}
	if err := s.ensureRolePermissionsGrantable(ctx, actor, role.Scope, extra, ""); err != nil {
		return err
	}
	var updatedBy *int64
	if actor != nil {
		updatedBy = &actor.AdminID
	}
	return s.SetRoleOverride(ctx, PolicyOverrideInput{RoleKey: roleKey, Allow: extra, Deny: dedupeSorted(deny), Note: "角色编辑器"}, updatedBy)
}

// ensureRolePermissionsGrantable 授权上限之于角色定义：新加的每一项、以及新继承的父角色的全部权限，
// 都必须是操作者持有的。
func (s *AdminService) ensureRolePermissionsGrantable(ctx context.Context, actor *admindomain.AccessContext, scope string, allow []string, baseRole string) error {
	if actor == nil || actor.IsSuperAdmin {
		return nil
	}
	if err := s.EnsureCanGrant(ctx, actor, allow, nil); err != nil {
		return err
	}
	if base := strings.TrimSpace(baseRole); base != "" {
		return s.EnsureCanAssignRole(ctx, actor, base, nil)
	}
	return nil
}

func (s *AdminService) DeleteCustomRole(ctx context.Context, roleKey string, force bool) error {
	role, ok := s.roleMeta(roleKey)
	if !ok || !role.IsCustom {
		return apperrors.New(40061, http.StatusBadRequest, "仅可删除自定义角色")
	}
	count, err := s.pg.CountAdminsByRoleKey(ctx, roleKey)
	if err != nil {
		return err
	}
	if count > 0 && !force {
		return apperrors.New(40962, http.StatusConflict, fmt.Sprintf("该角色正在被 %d 位管理员使用，请先移除分配或使用强制删除", count))
	}
	for key := range s.customRolesSnapshot() {
		if child, _ := s.roleMeta(key); strings.TrimSpace(child.BaseRole) == roleKey {
			return apperrors.New(40963, http.StatusConflict, fmt.Sprintf("角色「%s」继承了它，请先解除继承", child.Name))
		}
	}
	if err := s.pg.DeleteCustomRole(ctx, roleKey); err != nil {
		return err
	}
	// 授权关系没有外键：不一并删掉的话，这些分配会指向一个不存在的角色，留在名单里成为谁也解释不了的残留
	if count > 0 {
		if _, err := s.pg.DeleteAssignmentsByRole(ctx, roleKey); err != nil {
			return err
		}
	}
	if err := s.authz.RemoveRolePolicy(ctx, authz.SourceCustom, authz.RoleSubject(roleKey)); err != nil {
		return err
	}
	if err := s.authz.RemoveRolePolicy(ctx, authz.SourceOverride, authz.RoleSubject(roleKey)); err != nil {
		return err
	}
	if err := s.syncCustomRoles(ctx, false); err != nil {
		s.log.Warn("自定义角色元数据刷新失败", zap.Error(err))
	}
	return nil
}

func (s *AdminService) customRolesSnapshot() map[string]admindomain.RoleDefinition {
	s.rolesMu.RLock()
	defer s.rolesMu.RUnlock()
	out := make(map[string]admindomain.RoleDefinition, len(s.customRoles))
	for key, role := range s.customRoles {
		out[key] = role
	}
	return out
}

// ── 成员 ──

// AddRoleMembers 把一批管理员加进某个角色（应用级「按授权绑定」的角色必须给 appID）。
func (s *AdminService) AddRoleMembers(ctx context.Context, actor *admindomain.AccessContext, roleKey string, adminIDs []int64, appID *int64) (int, error) {
	role, ok := s.roleMeta(roleKey)
	if !ok || roleKey == authz.RoleSuperAdmin {
		return 0, apperrors.New(40053, http.StatusBadRequest, "包含无效的管理员角色")
	}
	normalized, err := s.normalizeAssignments([]admindomain.AssignmentMutation{{RoleKey: roleKey, AppID: appID}}, false, false)
	if err != nil {
		return 0, err
	}
	appID = normalized[0].AppID
	if err := s.EnsureCanAssignRole(ctx, actor, roleKey, appID); err != nil {
		return 0, err
	}
	added := 0
	for _, adminID := range adminIDs {
		target, err := s.GuardAdminTarget(ctx, actor, adminID, AdminActionAccess)
		if err != nil {
			return added, err
		}
		if target.Account.IsSuperAdmin {
			return added, apperrors.New(40075, http.StatusBadRequest,
				fmt.Sprintf("%s「%s」不需要角色授权", admindomain.TierName(target.Account.Tier), target.Account.Account))
		}
		if err := s.pg.AddAdminAssignment(ctx, adminID, roleKey, appID); err != nil {
			return added, err
		}
		added++
	}
	s.log.Info("角色成员已添加", zap.String("role", role.Key), zap.Int("count", added))
	return added, nil
}

// RemoveRoleMember 把某个管理员从角色里移出。
func (s *AdminService) RemoveRoleMember(ctx context.Context, actor *admindomain.AccessContext, roleKey string, adminID int64, appID *int64) error {
	if _, err := s.GuardAdminTarget(ctx, actor, adminID, AdminActionAccess); err != nil {
		return err
	}
	removed, err := s.pg.RemoveAdminAssignment(ctx, adminID, roleKey, appID)
	if err != nil {
		return err
	}
	if !removed {
		return apperrors.New(40464, http.StatusNotFound, "该管理员没有这条角色授权")
	}
	return nil
}

// ── 矩阵 / 关系图 ──

func (s *AdminService) GetRoleMatrix() admindomain.RoleMatrix {
	roles := s.ListRoles()
	groups := allPermissionGroups()
	permSets := make(map[string]map[string]bool, len(roles))
	for _, r := range roles {
		m := make(map[string]bool, len(r.Permissions))
		for _, p := range r.Permissions {
			m[p] = true
		}
		permSets[r.Key] = m
	}
	var rows []admindomain.RoleMatrixRow
	for _, g := range groups {
		for _, p := range g.Permissions {
			grants := make(map[string]bool, len(roles))
			for _, r := range roles {
				grants[r.Key] = permSets[r.Key][p.Code]
			}
			rows = append(rows, admindomain.RoleMatrixRow{
				PermissionCode: p.Code, PermissionName: p.Name,
				GroupKey: g.Key, GroupName: g.Name, Grants: grants,
			})
		}
	}
	return admindomain.RoleMatrix{Roles: roles, Groups: groups, Rows: rows}
}

func (s *AdminService) GetRoleGraph() admindomain.RoleGraph {
	roles := s.ListRoles()
	permSets := make(map[string]map[string]bool, len(roles))
	for _, r := range roles {
		m := make(map[string]bool, len(r.Permissions))
		for _, p := range r.Permissions {
			m[p] = true
		}
		permSets[r.Key] = m
	}
	var nodes []admindomain.RoleGraphNode
	for _, r := range roles {
		nodes = append(nodes, admindomain.RoleGraphNode{
			Key: r.Key, Name: r.Name, Level: r.Level,
			Scope: r.Scope, IsCustom: r.IsCustom, PermCount: len(r.Permissions),
		})
	}
	var edges []admindomain.RoleGraphEdge
	for _, r := range roles {
		if r.BaseRole != "" {
			edges = append(edges, admindomain.RoleGraphEdge{Source: r.BaseRole, Target: r.Key, Relation: "inherits"})
		}
	}
	for i, a := range roles {
		for j, b := range roles {
			if i == j || a.Level <= b.Level {
				continue
			}
			if !isSuperset(permSets[a.Key], permSets[b.Key]) || len(permSets[a.Key]) <= len(permSets[b.Key]) {
				continue
			}
			direct := true
			for k, c := range roles {
				if k == i || k == j || c.Level <= b.Level || c.Level >= a.Level {
					continue
				}
				if isSuperset(permSets[a.Key], permSets[c.Key]) && isSuperset(permSets[c.Key], permSets[b.Key]) {
					direct = false
					break
				}
			}
			if direct {
				edges = append(edges, admindomain.RoleGraphEdge{Source: a.Key, Target: b.Key, Relation: "includes"})
			}
		}
	}
	return admindomain.RoleGraph{Nodes: nodes, Edges: edges}
}

func (s *AdminService) GetRoleImpactPreview(ctx context.Context, roleKey string) (*admindomain.ImpactPreview, error) {
	admins, err := s.pg.ListAdminsByRoleKey(ctx, roleKey)
	if err != nil {
		return nil, err
	}
	return &admindomain.ImpactPreview{AffectedAdmins: admins, TotalAffected: len(admins)}, nil
}

func isSuperset(a, b map[string]bool) bool {
	for k := range b {
		if !a[k] {
			return false
		}
	}
	return true
}

func nonNilIDs(items []int64) []int64 {
	if items == nil {
		return []int64{}
	}
	return items
}
