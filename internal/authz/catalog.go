package authz

import (
	"sort"
	"strings"
	"sync"

	admindomain "aegis/internal/domain/admin"
)

// 权限点词汇表。
//
// 树形目录（目录 → 菜单 → 操作）定义在 tree.go，本文件只放：
//   - 被业务代码直接引用的权限点常量（service / middleware / handler 里的显式判定）；
//   - 基于树的查询函数（全部代码、中文名、旧权限点兼容关系、扁平分组）。
//
// 新增接口时在 tree.go 的某个操作节点里登记它的路由，忘了登记的表现是
// 40315「该管理端接口尚未登记权限规则」，而不是静默放行。

// 平台治理权限点：只在平台域判定。
const (
	PermPlatformAppRead      = "platform:app:read"
	PermPlatformAppGovern    = "platform:app:govern"
	PermPlatformAppDanger    = "platform:app:danger"
	PermPlatformAppealReview = "platform:appeal:review"
	PermPlatformStorageRead  = "platform:storage:read"
	PermPlatformStorageWrite = "platform:storage:write"
)

// 系统管理权限点（原 system:admin:manage 拆分而来）。
const (
	PermSystemAdminRead        = "system:admin:read"
	PermSystemAdminCreate      = "system:admin:create"
	PermSystemAdminEdit        = "system:admin:edit"
	PermSystemAdminStatus      = "system:admin:status"
	PermSystemAdminGrant       = "system:admin:grant"
	PermSystemSessionRead      = "system:session:read"
	PermSystemSessionRevoke    = "system:session:revoke"
	PermSystemRoleRead         = "system:role:read"
	PermSystemRoleCreate       = "system:role:create"
	PermSystemRoleEdit         = "system:role:edit"
	PermSystemRoleDelete       = "system:role:delete"
	PermSystemRoleReload       = "system:role:reload"
	PermSystemTempPermRead     = "system:temp_perm:read"
	PermSystemTempPermGrant    = "system:temp_perm:grant"
	PermSystemTempPermRevoke   = "system:temp_perm:revoke"
	PermSystemDelegationRead   = "system:delegation:read"
	PermSystemDelegationCreate = "system:delegation:create"
	PermSystemDelegationRevoke = "system:delegation:revoke"
	PermSystemOnlineRead       = "system:online:read"

	PermSystemLegalManage      = "system:legal:manage"
	PermSystemUserSettingRead  = "system:user_setting:read"
	PermSystemUserSettingWrite = "system:user_setting:write"

	// PermSystemAdminManage 旧的「系统管理」大桶，现为兼容组。保留常量名是为了让
	// 尚未迁移的调用点仍能编译，但**判定请求一律用细分权限点**：请求旧代码时，
	// 只持有细分权限点的人是满足不了它的（g2 的方向是「细分 → 旧组」）。
	PermSystemAdminManage   = LegacySystemAdminManage
	PermSystemSettingsRead  = LegacySettingsRead
	PermSystemSettingsWrite = LegacySettingsWrite
)

// 应用权限点。
const (
	// PermAppCreate 平台级持有时建应用不受自助配额约束。
	PermAppCreate = "app:create"
	// PermAppRead / PermAppWrite 旧的应用读写大桶（兼容组），只在策略里出现。
	PermAppRead  = LegacyAppRead
	PermAppWrite = LegacyAppWrite
	// PermAppUserRead / PermAppUserWrite 旧的应用用户读写大桶（兼容组）。
	PermAppUserRead  = LegacyAppUserRead
	PermAppUserWrite = LegacyAppUserWrite
	// PermAppUserInfoRead 应用用户查看（细分后的「用户列表 / 详情」）。
	PermAppUserInfoRead = "app:user:info:read"

	PermAppNotificationRead = "app:notification:read"
	PermAppNotifWrite       = "app:notification:write"

	PermContentBannerRead  = "content:banner:read"
	PermContentBannerWrite = "content:banner:write"
	PermContentNoticeRead  = "content:notice:read"
	PermContentNoticeWrite = "content:notice:write"

	PermAuditLoginRead   = "audit:login:read"
	PermAuditSessionRead = "audit:session:read"

	PermStorageRead  = "storage:read"
	PermStorageWrite = "storage:write"

	PermWorkflowRead  = "workflow:read"
	PermWorkflowWrite = "workflow:write"

	PermVersionRead  = LegacyVersionRead
	PermVersionWrite = LegacyVersionWrite

	PermSiteRead  = "site:read"
	PermSiteWrite = "site:write"
	PermSiteAudit = "site:audit"

	PermRoleApplicationRead   = "role_application:read"
	PermRoleApplicationReview = "role_application:review"

	PermPointsRead  = "points:read"
	PermPointsWrite = "points:write"

	PermEmailRead  = "email:read"
	PermEmailWrite = "email:write"

	PermAIRead  = "ai:read"
	PermAIWrite = "ai:write"

	PermPaymentRead  = "payment:read"
	PermPaymentWrite = "payment:write"

	PermTicketRead     = "ticket:read"
	PermTicketWrite    = "ticket:write"
	PermTicketReply    = "ticket:reply"
	PermTicketInternal = "ticket:internal"
	PermTicketAssign   = "ticket:assign"
	PermTicketClose    = "ticket:close"
	PermTicketDelete   = "ticket:delete"
	PermTicketManage   = "ticket:manage"
	PermTicketExport   = "ticket:export"

	PermNotifyChannelRead  = "notify:channel:read"
	PermNotifyChannelWrite = "notify:channel:write"
	PermNotifyDeliveryRead = "notify:delivery:read"
	PermNotifyTest         = "notify:test"

	PermOrgCreate       = "org:create"
	PermOrgWrite        = "org:write"
	PermOrgDeptRead     = "org:dept:read"
	PermOrgDeptWrite    = "org:dept:write"
	PermOrgMemberRead   = "org:member:read"
	PermOrgMemberWrite  = "org:member:write"
	PermOrgMemberInvite = "org:member:invite"
)

// serviceLevelPermissions 没有路由、只在 service 层判定的权限点（工单动作、组织成员等）。
// 目录测试据此区分「故意没有路由」与「忘了登记路由」。
var serviceLevelPermissions = map[string]bool{
	PermAppCreate:       true,
	PermTicketRead:      true,
	PermTicketWrite:     true,
	PermTicketReply:     true,
	PermTicketInternal:  true,
	PermTicketAssign:    true,
	PermTicketClose:     true,
	PermTicketDelete:    true,
	PermOrgDeptRead:     true,
	PermOrgDeptWrite:    true,
	PermOrgMemberRead:   true,
	PermOrgMemberWrite:  true,
	PermOrgMemberInvite: true,
	PermNotifyTest:      true,
}

// catalogIndex 树的派生索引，首次使用时构造一次（树是编译期常量）。
type catalogIndex struct {
	tree     []PermissionNode
	codes    []string
	nodes    map[string]*PermissionNode
	paths    map[string][]string // code → 祖先节点名（目录 / 菜单）
	legacy   map[string]string   // code → 旧权限点
	children map[string][]string // 旧权限点 → 细分权限点
}

var (
	catalogOnce sync.Once
	catalogData *catalogIndex
)

func catalog() *catalogIndex {
	catalogOnce.Do(func() {
		idx := &catalogIndex{
			tree:     permissionTree(),
			nodes:    map[string]*PermissionNode{},
			paths:    map[string][]string{},
			legacy:   map[string]string{},
			children: map[string][]string{},
		}
		counts := routeCountsByPermission()
		var walk func(nodes []PermissionNode, scope PermScope, path []string)
		walk = func(nodes []PermissionNode, scope PermScope, path []string) {
			for i := range nodes {
				node := &nodes[i]
				if node.Scope == "" {
					node.Scope = scope
				}
				if node.Kind == NodeAction {
					node.Routes = counts[node.Code]
					idx.codes = append(idx.codes, node.Code)
					idx.nodes[node.Code] = node
					idx.paths[node.Code] = append([]string(nil), path...)
					if node.Legacy != "" {
						idx.legacy[node.Code] = node.Legacy
						idx.children[node.Legacy] = append(idx.children[node.Legacy], node.Code)
					}
					continue
				}
				walk(node.Children, node.Scope, append(append([]string(nil), path...), node.Name))
			}
		}
		walk(idx.tree, ScopeBoth, nil)
		catalogData = idx
	})
	return catalogData
}

// PermissionTree 返回权限树的深拷贝（带每个操作节点覆盖的接口数）。
func PermissionTree() []PermissionNode {
	return cloneNodes(catalog().tree)
}

func cloneNodes(nodes []PermissionNode) []PermissionNode {
	out := make([]PermissionNode, len(nodes))
	for i, node := range nodes {
		node.routes = nil
		node.Children = cloneNodes(node.Children)
		out[i] = node
	}
	return out
}

// AllPermissionCodes 树上全部操作节点的代码（树序）。
func AllPermissionCodes() []string {
	return append([]string(nil), catalog().codes...)
}

// PermissionExists 代码是否为树上的操作节点。
func PermissionExists(code string) bool {
	_, ok := catalog().nodes[code]
	return ok
}

// IsLegacyPermission 代码是否为旧权限点（兼容组）。
func IsLegacyPermission(code string) bool {
	_, ok := legacyPermissionNames[code]
	return ok
}

// LegacyPermissionNames 旧权限点 → 中文名。
func LegacyPermissionNames() map[string]string {
	out := make(map[string]string, len(legacyPermissionNames))
	for code, name := range legacyPermissionNames {
		out[code] = name
	}
	return out
}

// LegacyOf 细分权限点所替换的旧权限点；未拆分的权限点返回空串。
func LegacyOf(code string) string { return catalog().legacy[code] }

// LegacyChildren 旧权限点名下的全部细分权限点。
func LegacyChildren(legacy string) []string {
	return append([]string(nil), catalog().children[legacy]...)
}

// PermissionScope 权限点的判定作用域。
func PermissionScope(code string) PermScope {
	if node, ok := catalog().nodes[code]; ok {
		return node.Scope
	}
	return ScopeBoth
}

// PermissionIsDanger 是否为高危操作。
func PermissionIsDanger(code string) bool {
	node, ok := catalog().nodes[code]
	return ok && node.Danger
}

// PermissionName 权限点的完整中文名（「菜单 · 操作」）；旧权限点给出兼容说明；都不是时原样返回。
func PermissionName(code string) string {
	idx := catalog()
	if node, ok := idx.nodes[code]; ok {
		path := idx.paths[code]
		if len(path) > 0 {
			return path[len(path)-1] + "：" + node.Name
		}
		return node.Name
	}
	if name, ok := legacyPermissionNames[code]; ok {
		return name
	}
	if strings.HasSuffix(code, ":*") {
		return "「" + strings.TrimSuffix(code, ":*") + "」下的全部权限"
	}
	if code == AnyPermission {
		return "全部权限"
	}
	return code
}

// PermissionPath 权限点在树上的位置（目录 / 菜单名）。
func PermissionPath(code string) []string {
	return append([]string(nil), catalog().paths[code]...)
}

// Implies 判定一条授予模式是否覆盖某个被请求的权限点。
//
// 规则与授权引擎完全一致：前缀通配（`ticket:*` / `*`）、全等、
// 以及「细分权限点 → 旧权限点」的兼容关系。临时权限与 MCP 密钥范围都用它，
// 两处自己写字符串比较的话，持有旧代码 `app:read` 的密钥会调不动 `app:lottery:read`。
func Implies(granted, requested string) bool {
	granted = strings.TrimSpace(granted)
	requested = strings.TrimSpace(requested)
	if granted == "" || requested == "" {
		return false
	}
	if patternMatches(granted, requested) {
		return true
	}
	if legacy := LegacyOf(requested); legacy != "" {
		return patternMatches(granted, legacy)
	}
	return false
}

func patternMatches(pattern, code string) bool {
	if pattern == AnyPermission || pattern == code {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(code, strings.TrimSuffix(pattern, "*"))
	}
	return false
}

// ExpandPattern 把一条授予模式展开成它覆盖的全部细分权限点（有序）。
func ExpandPattern(pattern string) []string {
	result := make([]string, 0)
	for _, code := range catalog().codes {
		if Implies(pattern, code) {
			result = append(result, code)
		}
	}
	return result
}

// resourceEdges 资源层级（g2）：细分权限点 → 旧权限点。
func resourceEdges() [][]string {
	idx := catalog()
	edges := make([][]string, 0, len(idx.legacy))
	for _, code := range idx.codes {
		if legacy := idx.legacy[code]; legacy != "" {
			edges = append(edges, []string{code, legacy})
		}
	}
	return edges
}

// PermissionCatalog 扁平的「菜单 → 操作」分组。
//
// 旧接口（权限矩阵、MCP 密钥签发页）按分组消费，这里把树压平到菜单一级：
// 每个菜单一组，组名带上所属目录。
func PermissionCatalog() []admindomain.PermissionGroup {
	idx := catalog()
	groups := make([]admindomain.PermissionGroup, 0, 96)
	var walk func(nodes []PermissionNode, parent string)
	walk = func(nodes []PermissionNode, parent string) {
		for _, node := range nodes {
			if node.Kind == NodeMenu {
				group := admindomain.PermissionGroup{Key: node.Key, Name: node.Name}
				if parent != "" {
					group.Name = parent + " · " + node.Name
				}
				for _, child := range node.Children {
					if child.Kind == NodeAction {
						group.Permissions = append(group.Permissions, admindomain.Permission{
							Code: child.Code, Name: child.Name, Description: child.Description,
						})
					}
				}
				groups = append(groups, group)
				continue
			}
			walk(node.Children, node.Name)
		}
	}
	walk(idx.tree, "")
	return groups
}

// sortedCodes 去重排序。
func sortedCodes(items []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func init() {
	// domain 层不能 import 本包（会成环），MCP 密钥范围的匹配经由这个钩子拿到兼容关系。
	admindomain.PermissionImplies = Implies
}
