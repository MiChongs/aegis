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
	raw, err := os.ReadFile("testdata/legacy_route_permissions.json")
	if err != nil {
		t.Fatalf("读取路由判定基线失败：%v", err)
	}
	var rows []routeBaselineRow
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatalf("解析路由判定基线失败：%v", err)
	}
	if len(rows) < 1000 {
		t.Fatalf("基线只有 %d 条，明显不完整 —— 它应覆盖全部注册的管理端路由", len(rows))
	}
	return rows
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

	rows := loadGoldenAdminRoutes(t)
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
