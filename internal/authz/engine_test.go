package authz

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"testing"
)

// newTestEngine 构造一个纯内存引擎（store 为 nil），策略直接来自传入的规则。
func newTestEngine(t *testing.T, rules []PolicyRule) *Engine {
	t.Helper()
	engine, err := New(context.Background(), nil, nil, rules)
	if err != nil {
		t.Fatalf("构造授权引擎失败：%v", err)
	}
	return engine
}

func allow(subject, domain, permission string) PolicyRule {
	return PolicyRule{PType: "p", Values: []string{subject, domain, permission, EffectAllow}, Owner: subject}
}

func deny(subject, domain, permission string) PolicyRule {
	return PolicyRule{PType: "p", Values: []string{subject, domain, permission, EffectDeny}, Owner: subject}
}

func inherit(child, parent string) PolicyRule {
	return PolicyRule{PType: "g", Values: []string{child, parent}, Owner: child}
}

// 域匹配是整套多租户隔离的地基。
//
// 判错方向的后果不对称：`*` 的策略漏放行只是有人点不动按钮，
// 而应用级策略被平台级请求认下，等于「被冻结应用的管理员给自己解封」。
func TestDomainScoping(t *testing.T) {
	t.Parallel()

	engine := newTestEngine(t, []PolicyRule{
		allow(RoleSubject("platform_admin"), AnyDomain, PermAppWrite),
		allow(RoleSubject("app_admin"), AnyDomain, PermAppWrite),
		allow(AdminSubject(9), AppDomain(5), PermPaymentWrite),
	})

	cases := []struct {
		name    string
		subject string
		domain  string
		perm    string
		want    bool
	}{
		{"* 策略在平台域成立", RoleSubject("platform_admin"), PlatformDomain, PermAppWrite, true},
		{"* 策略在应用域成立", RoleSubject("platform_admin"), AppDomain(5), PermAppWrite, true},
		{"绑定到应用 5 的直接授予在该应用成立", AdminSubject(9), AppDomain(5), PermPaymentWrite, true},
		{"绑定到应用 5 的直接授予对应用 6 不成立", AdminSubject(9), AppDomain(6), PermPaymentWrite, false},
		{"绑定到应用的直接授予绝不能满足平台级请求", AdminSubject(9), PlatformDomain, PermPaymentWrite, false},
		{"没有策略的主体一律拒绝", RoleSubject("unknown"), PlatformDomain, PermAppWrite, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := engine.Allow([]string{tc.subject}, tc.domain, tc.perm); got != tc.want {
				t.Fatalf("Allow(%q, %q, %q) = %v，期望 %v", tc.subject, tc.domain, tc.perm, got, tc.want)
			}
		})
	}
}

// 前缀通配：一条 `ticket:*` 顶九条，且不能吃到隔壁前缀。
// 旧模型是字符串全等，每加一个权限点都要在每个该有它的角色里补一行。
func TestPermissionWildcard(t *testing.T) {
	t.Parallel()

	engine := newTestEngine(t, []PolicyRule{
		allow(RoleSubject("agent"), AnyDomain, "ticket:*"),
		allow(RoleSubject("root"), AnyDomain, AnyPermission),
	})

	cases := []struct {
		subject, permission string
		want                bool
	}{
		{RoleSubject("agent"), PermTicketClose, true},
		{RoleSubject("agent"), PermTicketRead, true},
		// 前缀必须锚在冒号上，否则 `ticket:*` 会连 ticketing:* 一起放行
		{RoleSubject("agent"), "ticketing:read", false},
		{RoleSubject("agent"), PermAppWrite, false},
		{RoleSubject("root"), PermAppWrite, true},
		{RoleSubject("root"), PermPlatformAppDanger, true},
	}
	for _, tc := range cases {
		if got := engine.Allow([]string{tc.subject}, PlatformDomain, tc.permission); got != tc.want {
			t.Errorf("%s / %s = %v，期望 %v", tc.subject, tc.permission, got, tc.want)
		}
	}
}

// 角色继承：base_role 终于是执行点。
// 旧实现里这一列只被拿去画关系图，于是控制台上标着「继承自 X」的角色
// 实际一个继承来的权限都没有。
func TestRoleInheritance(t *testing.T) {
	t.Parallel()

	engine := newTestEngine(t, []PolicyRule{
		allow(RoleSubject("app_operator"), AnyDomain, PermAppUserWrite),
		inherit(RoleSubject("custom_x"), RoleSubject("app_operator")),
		allow(RoleSubject("custom_x"), AnyDomain, PermTicketExport),
	})

	if !engine.Allow([]string{RoleSubject("custom_x")}, PlatformDomain, PermAppUserWrite) {
		t.Fatal("自定义角色应继承父角色的权限")
	}
	if !engine.Allow([]string{RoleSubject("custom_x")}, PlatformDomain, PermTicketExport) {
		t.Fatal("自定义角色自己的权限应保留")
	}
	if engine.Allow([]string{RoleSubject("app_operator")}, PlatformDomain, PermTicketExport) {
		t.Fatal("继承是单向的：父角色不该拿到子角色的权限")
	}
}

// 显式拒绝**跨主体**生效。
//
// 这是旧模型完全表达不了的一档，也是最容易实现错的一档：
// Casbin 的 deny-override 只在单次 Enforce（单个主体及其继承链）内成立，
// 照搬会让「禁止某人删工单」这条规则只要那个人再有一个别的角色就失效。
func TestExplicitDenyBeatsAllowAcrossSubjects(t *testing.T) {
	t.Parallel()

	engine := newTestEngine(t, []PolicyRule{
		allow(RoleSubject("app_admin"), AnyDomain, PermTicketDelete),
		deny(AdminSubject(7), AnyDomain, PermTicketDelete),
	})

	subjects := []string{AdminSubject(7), RoleSubject("app_admin")}
	decision := engine.Decide(subjects, PlatformDomain, PermTicketDelete)
	if decision.Allowed {
		t.Fatal("个人身上的拒绝必须压倒角色带来的放行，否则这条规则形同虚设")
	}
	if decision.Effect != EffectDeny {
		t.Fatalf("拒绝原因应为 deny（供文案区分「被禁止」与「没配到」），得到 %q", decision.Effect)
	}
	if len(decision.Rule) == 0 {
		t.Fatal("拒绝必须带上命中的策略行，否则运维无从知道是哪条规则挡住的")
	}
	// 换个人不受影响
	if !engine.Allow([]string{AdminSubject(8), RoleSubject("app_admin")}, PlatformDomain, PermTicketDelete) {
		t.Fatal("拒绝只应作用于被点名的主体")
	}
}

// 拒绝也能带通配：一条 deny 关掉一整片能力。
func TestDenyWithWildcard(t *testing.T) {
	t.Parallel()

	engine := newTestEngine(t, []PolicyRule{
		allow(RoleSubject("app_admin"), AnyDomain, "payment:*"),
		deny(AdminSubject(7), AppDomain(3), "payment:*"),
	})
	subjects := []string{AdminSubject(7), RoleSubject("app_admin")}

	if engine.Allow(subjects, AppDomain(3), PermPaymentWrite) {
		t.Fatal("应用 3 内的支付能力应被整片拒绝")
	}
	if !engine.Allow(subjects, AppDomain(4), PermPaymentWrite) {
		t.Fatal("拒绝限定在应用 3，不该外溢到应用 4")
	}
}

// 展开出来的权限集必须与逐条判定完全一致 —— 控制台按它决定按钮显隐。
func TestPermissionsForMatchesDecide(t *testing.T) {
	t.Parallel()

	engine := newTestEngine(t, []PolicyRule{
		allow(RoleSubject("app_admin"), AnyDomain, "content:*"),
		allow(RoleSubject("app_admin"), AnyDomain, "ticket:*"),
		deny(RoleSubject("app_admin"), AnyDomain, PermTicketDelete),
	})
	subjects := []string{RoleSubject("app_admin")}
	catalog := AllPermissionCodes()

	expanded := engine.PermissionsFor(subjects, AppDomain(1), catalog)
	set := map[string]bool{}
	for _, item := range expanded {
		set[item] = true
	}
	for _, permission := range catalog {
		want := engine.Decide(subjects, AppDomain(1), permission).Allowed
		if set[permission] != want {
			t.Errorf("%s：展开集合说 %v，逐条判定说 %v —— 两者不一致就会出现「按钮在、点了 403」",
				permission, set[permission], want)
		}
	}
	if set[PermTicketDelete] {
		t.Error("被 deny 压掉的权限不能出现在展开集合里")
	}
	if !set[PermTicketClose] {
		t.Error("通配授予的权限应出现在展开集合里")
	}
}

// 内置角色的权限集不能因为改用通配而发生变化。
//
// roles.go 里把一批显式列表换成了 `content:*` 这类前缀通配。通配一旦写宽一格
// （比如把 app_admin 的工单列表写成 ticket:*），就会多出一个 ticket:delete ——
// 而这种越权不会有任何报错。testdata/builtin_roles.json 是改动**之前**的快照。
func TestBuiltinRolePermissionsUnchanged(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("testdata/builtin_roles.json")
	if err != nil {
		t.Fatalf("读取内置角色基线失败：%v", err)
	}
	var baseline map[string][]string
	if err := json.Unmarshal(raw, &baseline); err != nil {
		t.Fatalf("解析内置角色基线失败：%v", err)
	}

	engine := newTestEngine(t, BuiltinPolicies())
	catalog := AllPermissionCodes()

	for roleKey, want := range baseline {
		t.Run(roleKey, func(t *testing.T) {
			got := engine.PermissionsFor([]string{RoleSubject(roleKey)}, AppDomain(1), catalog)
			if roleKey == RoleSuperAdmin {
				// 超管基线是空列表（旧实现靠 Go 里的短路），现在是一条 `*` 策略。
				// 这是刻意的差异：权限矩阵终于能如实显示"超管有全部权限"。
				if len(got) != len(catalog) {
					t.Fatalf("超管应展开成全部 %d 个权限点，得到 %d 个", len(catalog), len(got))
				}
				return
			}
			sort.Strings(want)
			if len(got) != len(want) {
				t.Fatalf("权限数从 %d 变成了 %d\n新增：%v\n丢失：%v",
					len(want), len(got), diffPermissions(got, want), diffPermissions(want, got))
			}
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("第 %d 项：期望 %q，得到 %q", i, want[i], got[i])
				}
			}
		})
	}
}

func diffPermissions(a, b []string) []string {
	set := map[string]bool{}
	for _, item := range b {
		set[item] = true
	}
	var extra []string
	for _, item := range a {
		if !set[item] {
			extra = append(extra, item)
		}
	}
	return extra
}

// 内置角色引用的权限点必须都在目录里（通配前缀除外）。
// 写错一个字母不会报错，只会让那条策略永远匹配不上任何请求。
func TestBuiltinRolePermissionsExistInCatalog(t *testing.T) {
	t.Parallel()

	for roleKey, role := range BuiltinRoles() {
		for _, permission := range role.Permissions {
			if permission == AnyPermission || len(permission) > 0 && permission[len(permission)-1] == '*' {
				continue
			}
			if !PermissionExists(permission) {
				t.Errorf("角色 %s 引用了目录里不存在的权限点 %q", roleKey, permission)
			}
		}
	}
}
