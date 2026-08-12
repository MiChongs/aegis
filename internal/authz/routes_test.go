package authz

import (
	"encoding/json"
	"os"
	"testing"
)

type routeBaselineRow struct {
	Method     string `json:"method"`
	Path       string `json:"path"`
	Permission string `json:"permission"`
	AppScoped  bool   `json:"appScoped"`
	Unmapped   bool   `json:"unmapped"`
}

func loadRouteBaseline(t *testing.T) []routeBaselineRow {
	t.Helper()
	raw, err := os.ReadFile("testdata/route_permissions.json")
	if err != nil {
		t.Fatalf("读取路由判定基线失败：%v", err)
	}
	var rows []routeBaselineRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("解析路由判定基线失败：%v", err)
	}
	if len(rows) < 500 {
		t.Fatalf("基线只有 %d 条，明显不完整 —— 它应覆盖全部注册路由", len(rows))
	}
	return rows
}

// 规则表必须逐条复现旧实现（250 行 switch）对**全部注册路由**的判定。
//
// 这条测试是这次重构唯一的安全网。授权判定改错不会有编译错误、多数也不会有运行时
// 报错，只会表现为某个页面空白（判成了应用作用域却拿不到 appid）或者某个人
// 突然能点他不该点的按钮 —— 两者都可能几个月后才被发现。
//
// testdata/route_permissions.json 是重构**之前**用旧实现跑出来的快照。
// 以后要改某条路由的权限，改规则表的同时更新基线里对应那一行，
// diff 会把改动摆在评审者面前，而不是藏在一个嵌套 switch 的第三层里。
func TestRouteRulesMatchBaseline(t *testing.T) {
	t.Parallel()

	var mismatches int
	for _, row := range loadRouteBaseline(t) {
		decision, ok := ResolveRoute(row.Method, row.Path)
		if row.Unmapped {
			if ok {
				t.Errorf("%s %s：旧实现按未登记处理（默认拒绝），新规则表却给出了 %q/%v",
					row.Method, row.Path, decision.Permission, decision.AppScoped())
				mismatches++
			}
			continue
		}
		if !ok {
			t.Errorf("%s %s：规则表没覆盖到，旧实现给的是 %q（appScoped=%v）",
				row.Method, row.Path, row.Permission, row.AppScoped)
			mismatches++
			continue
		}
		if decision.Permission != row.Permission || decision.AppScoped() != row.AppScoped {
			t.Errorf("%s %s：期望 %q/appScoped=%v，得到 %q/appScoped=%v（命中规则 %q）",
				row.Method, row.Path, row.Permission, row.AppScoped,
				decision.Permission, decision.AppScoped(), matchedPattern(decision))
			mismatches++
		}
		if mismatches > 40 {
			t.Fatal("差异过多，先修前面的再继续")
		}
	}
}

func matchedPattern(decision RouteDecision) string {
	if decision.Matched == nil {
		return ""
	}
	return decision.Matched.Pattern
}

// 没有哪条规则应该被上面的规则完全遮住。
//
// 被遮住的规则不会报错，只是**永不生效** —— 旧实现里就沉淀了三段这样的死代码
// （/api/admin/system/departments、/invitations、/positions 三个分支，
// 其对应路由早已改挂到 organizations 之下），而它们看起来一直像是在管着什么。
func TestNoUnreachableRouteRule(t *testing.T) {
	t.Parallel()

	rows := loadRouteBaseline(t)
	hit := make([]bool, len(adminRouteRules))
	for _, row := range rows {
		for i := range adminRouteRules {
			if adminRouteRules[i].matches(row.Method, row.Path) {
				hit[i] = true
				break
			}
		}
	}
	for i, used := range hit {
		if !used {
			t.Errorf("规则 #%d（%q，权限 %q）没有匹配到任何注册路由："+
				"要么被上面更宽的规则遮住了，要么它对应的路由已经不存在",
				i, adminRouteRules[i].Pattern, adminRouteRules[i].Permission)
		}
	}
}

// 分段匹配的语义（尤其是 `*` 吃零段）必须钉死：
// 差这一条，「集合根」与「集合下的子资源」就要各写一条规则，而漏写的那条会静默落到下面更宽的规则上。
func TestMatchRoutePattern(t *testing.T) {
	t.Parallel()

	cases := []struct {
		path, pattern string
		want          bool
	}{
		{"/api/admin/apps", "/api/admin/apps", true},
		{"/api/admin/apps/:appkey", "/api/admin/apps", false},
		{"/api/admin/apps/:appkey/users", "/api/admin/apps/:appkey/users/*", true},         // * 吃零段
		{"/api/admin/apps/:appkey/users/:userId", "/api/admin/apps/:appkey/users/*", true}, // * 吃多段
		{"/api/admin/apps/:appkey/usersx", "/api/admin/apps/:appkey/users/*", false},       // 锚定在段边界
		{"/api/admin/appsx/y", "/api/admin/apps/*", false},                                 // 前缀不能吃半个段
		{"/api/admin/apps/:appkey/channels/:cid/users", "/api/admin/apps/:appkey/users/*", false},
		{"/api/admin/tickets", "/api/admin/tickets/*", true},
		{"/a/b/c", "/a/:x/c", true},
		{"/a/b/c/d", "/a/:x/c", false},
	}
	for _, tc := range cases {
		if got := matchRoutePattern(tc.path, tc.pattern); got != tc.want {
			t.Errorf("matchRoutePattern(%q, %q) = %v，期望 %v", tc.path, tc.pattern, got, tc.want)
		}
	}
}

// 规则表里出现的每个权限点都必须登记在目录里。
//
// 写错一个字母（app:user:wirte）的表现是：那条路由永远没人有权限，
// 而角色编辑器里根本找不到这一项可以勾。
func TestRouteRulePermissionsExistInCatalog(t *testing.T) {
	t.Parallel()

	for _, rule := range adminRouteRules {
		if rule.Permission == "" {
			continue
		}
		if !PermissionExists(rule.Permission) {
			t.Errorf("规则 %q 引用了目录里不存在的权限点 %q", rule.Pattern, rule.Permission)
		}
	}
}
