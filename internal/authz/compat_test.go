package authz

import (
	"bufio"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
)

// 权限拆分的生产安全网。
//
// testdata/legacy_route_permissions.json 是拆分**之前**用旧规则表对全部注册管理端路由跑出来的快照，
// testdata/legacy_builtin_role_definitions.json 是当时的内置角色定义。
// 这几条测试保证：任何持有旧权限点（或旧前缀通配）的角色、策略、MCP 密钥、临时权限，
// 在新规则表 + 新权限树下能进的接口与以前**逐条一致** —— 一项不多、一项不少。

type goldenRoute struct {
	Method string
	Path   string
}

func loadGoldenAdminRoutes(t *testing.T) []routeBaselineRow {
	t.Helper()
	f, err := os.Open("../transport/http/testdata/routes.golden")
	if err != nil {
		t.Fatalf("读取路由表快照失败：%v", err)
	}
	defer f.Close()
	var rows []routeBaselineRow
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), "\t")
		if len(parts) < 2 {
			continue
		}
		if !strings.HasPrefix(parts[1], "/api/admin/") && !strings.HasPrefix(parts[1], "/api/app/") {
			continue
		}
		rows = append(rows, routeBaselineRow{Method: parts[0], Path: parts[1]})
	}
	return rows
}

// 旧规则表认得的路由，新规则表都要认得，作用域不变；旧规则不要权限点的，新规则也不要。
func TestLegacyRoutesKeepScopeAndOpenness(t *testing.T) {
	t.Parallel()
	for _, row := range loadRouteBaseline(t) {
		decision, ok := ResolveRoute(row.Method, row.Path)
		if row.Unmapped {
			if ok {
				t.Errorf("%s %s：旧规则按未登记处理（这组路由不经过 AdminAccess），新规则却给出了 %q", row.Method, row.Path, decision.Permission)
			}
			continue
		}
		if !ok {
			t.Errorf("%s %s：新规则表没覆盖到（旧权限 %q）", row.Method, row.Path, row.Permission)
			continue
		}
		if decision.AppScoped() != row.AppScoped {
			t.Errorf("%s %s：作用域从 appScoped=%v 变成了 %v（命中 %q）", row.Method, row.Path, row.AppScoped, decision.AppScoped(), matchedPattern(decision))
		}
		if (row.Permission == "") != (decision.Permission == "") {
			t.Errorf("%s %s：旧权限 %q，新权限 %q —— 开放接口与鉴权接口不能互换", row.Method, row.Path, row.Permission, decision.Permission)
		}
	}
}

// 每条路由的新权限点必须「是」或「隶属于」它的旧权限点。
func TestNewPermissionDescendsFromLegacy(t *testing.T) {
	t.Parallel()
	for _, row := range loadRouteBaseline(t) {
		if row.Unmapped || row.Permission == "" {
			continue
		}
		decision, ok := ResolveRoute(row.Method, row.Path)
		if !ok {
			continue
		}
		got := decision.Permission
		if got == row.Permission || LegacyOf(got) == row.Permission {
			continue
		}
		t.Errorf("%s %s：旧权限 %q → 新权限 %q（隶属 %q，命中 %q）",
			row.Method, row.Path, row.Permission, got, LegacyOf(got), matchedPattern(decision))
	}
}

// legacyPatterns 线上可能存在的全部授予写法：旧目录里的每个权限点、它们的每一级前缀通配、`*`。
func legacyPatterns(t *testing.T) []string {
	t.Helper()
	set := map[string]bool{AnyPermission: true}
	for _, row := range loadRouteBaseline(t) {
		if row.Permission != "" {
			set[row.Permission] = true
		}
	}
	raw, err := os.ReadFile("testdata/legacy_builtin_role_definitions.json")
	if err != nil {
		t.Fatalf("读取旧内置角色定义失败：%v", err)
	}
	var roles map[string][]string
	if err := json.Unmarshal(raw, &roles); err != nil {
		t.Fatal(err)
	}
	for _, perms := range roles {
		for _, p := range perms {
			set[p] = true
		}
	}
	for code := range LegacyPermissionNames() {
		set[code] = true
	}
	// 每一级前缀通配
	for code := range set {
		segments := strings.Split(code, ":")
		for i := 1; i < len(segments); i++ {
			set[strings.Join(segments[:i], ":")+":*"] = true
		}
	}
	items := make([]string, 0, len(set))
	for code := range set {
		items = append(items, code)
	}
	sort.Strings(items)
	return items
}

// 持有某条旧授予模式的主体，在新体系下能进的路由集合与以前完全相同。
func TestLegacyHoldersKeepRouteAccess(t *testing.T) {
	t.Parallel()
	rows := loadRouteBaseline(t)
	for _, pattern := range legacyPatterns(t) {
		engine := newTestEngine(t, []PolicyRule{allow(RoleSubject("holder"), AnyDomain, pattern)})
		subjects := []string{RoleSubject("holder")}
		var gained, lost []string
		for _, row := range rows {
			if row.Unmapped || row.Permission == "" {
				continue
			}
			before := patternMatches(pattern, row.Permission)
			decision, _ := ResolveRoute(row.Method, row.Path)
			after := engine.Allow(subjects, AppDomain(1), decision.Permission)
			switch {
			case after && !before:
				gained = append(gained, row.Method+" "+row.Path+" → "+decision.Permission)
			case before && !after:
				lost = append(lost, row.Method+" "+row.Path+" → "+decision.Permission)
			}
		}
		if len(gained) > 0 || len(lost) > 0 {
			t.Errorf("授予 %q 的主体：多出 %d 条 %v，丢失 %d 条 %v", pattern, len(gained), head(gained), len(lost), head(lost))
		}
	}
}

// 显式拒绝旧代码要压住它名下的全部细分项。
func TestLegacyDenyCoversChildren(t *testing.T) {
	t.Parallel()
	engine := newTestEngine(t, []PolicyRule{
		allow(RoleSubject("r"), AnyDomain, AnyPermission),
		deny(RoleSubject("r"), AnyDomain, LegacyAppWrite),
	})
	subjects := []string{RoleSubject("r")}
	for _, child := range LegacyChildren(LegacyAppWrite) {
		if engine.Allow(subjects, AppDomain(1), child) {
			t.Errorf("deny app:write 之后 %s 仍被放行", child)
		}
	}
	if !engine.Allow(subjects, AppDomain(1), "app:lottery:read") {
		t.Error("deny app:write 不该压到读权限")
	}
}

// 内置角色在新体系下的路由访问与拆分前逐条一致（作用域分平台 / 应用两档判）。
func TestBuiltinRolesKeepRouteAccess(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("testdata/legacy_builtin_role_definitions.json")
	if err != nil {
		t.Fatal(err)
	}
	var legacy map[string][]string
	if err := json.Unmarshal(raw, &legacy); err != nil {
		t.Fatal(err)
	}
	engine := newTestEngine(t, BuiltinPolicies())
	for roleKey, perms := range legacy {
		subjects := []string{RoleSubject(roleKey)}
		for _, row := range loadRouteBaseline(t) {
			if row.Unmapped || row.Permission == "" {
				continue
			}
			before := false
			for _, p := range perms {
				if patternMatches(p, row.Permission) {
					before = true
					break
				}
			}
			decision, _ := ResolveRoute(row.Method, row.Path)
			after := engine.Allow(subjects, AppDomain(1), decision.Permission)
			if before != after {
				t.Errorf("角色 %s 对 %s %s：拆分前 %v，拆分后 %v（%s）", roleKey, row.Method, row.Path, before, after, decision.Permission)
			}
		}
	}
}

// 新注册的管理端路由（不在拆分前快照里的）也必须登记了权限规则。
func TestEveryAdminRouteRegistered(t *testing.T) {
	t.Parallel()
	known := map[string]bool{}
	for _, row := range loadRouteBaseline(t) {
		known[row.Method+" "+row.Path] = true
	}
	for _, row := range loadGoldenAdminRoutes(t) {
		if known[row.Method+" "+row.Path] {
			continue
		}
		if _, ok := ResolveRoute(row.Method, row.Path); !ok {
			t.Errorf("%s %s：新路由没有登记权限规则（在 tree.go 的某个操作节点里登记）", row.Method, row.Path)
		}
	}
}

// 权限树自身的约束。
func TestPermissionTreeInvariants(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	var walk func(nodes []PermissionNode, depth int)
	walk = func(nodes []PermissionNode, depth int) {
		for _, node := range nodes {
			if node.Key == "" || node.Name == "" {
				t.Errorf("节点缺少 key 或名称：%+v", node)
			}
			if node.Kind != NodeAction {
				if len(node.Children) == 0 {
					t.Errorf("%s：目录 / 菜单下没有任何操作", node.Key)
				}
				walk(node.Children, depth+1)
				continue
			}
			code := node.Code
			if seen[code] {
				t.Errorf("权限点重复登记：%s", code)
			}
			seen[code] = true
			if IsLegacyPermission(code) {
				t.Errorf("%s 既是树上的操作又是旧权限点（兼容组）", code)
			}
			if node.Legacy != "" && !IsLegacyPermission(node.Legacy) {
				t.Errorf("%s 隶属的旧权限点 %s 没有登记在 legacyPermissionNames", code, node.Legacy)
			}
			if node.Name == "查看" && !strings.HasSuffix(code, ":read") {
				t.Errorf("%s：查看类权限点必须以 :read 结尾（只读 MCP 密钥靠这个后缀识别）", code)
			}
			if len(node.routes) == 0 && !serviceLevelPermissions[code] {
				t.Errorf("%s：没有登记任何路由，也不在 serviceLevelPermissions 里", code)
			}
		}
	}
	walk(permissionTree(), 0)
	for code := range serviceLevelPermissions {
		if !seen[code] {
			t.Errorf("serviceLevelPermissions 里的 %s 不在树上", code)
		}
	}
}

// 每个细分权限点都要有至少一条命中的路由（服务层权限点除外）——否则它只是一个勾了也没用的选项。
func TestEveryPermissionReachable(t *testing.T) {
	t.Parallel()
	hit := map[string]bool{}
	for _, row := range loadGoldenAdminRoutes(t) {
		if decision, ok := ResolveRoute(row.Method, row.Path); ok && decision.Permission != "" {
			hit[decision.Permission] = true
		}
	}
	for _, code := range AllPermissionCodes() {
		if !hit[code] && !serviceLevelPermissions[code] {
			t.Errorf("%s 没有任何路由会判到它（被更具体的规则全部截走了？）", code)
		}
	}
}

func head(items []string) []string {
	if len(items) > 6 {
		return items[:6]
	}
	return items
}
