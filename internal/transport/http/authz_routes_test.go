package httptransport

import (
	"strings"
	"testing"

	"aegis/internal/authz"
)

// 每一条走 AdminAccess 的路由都必须在权限规则表里有归属。
//
// 这条测试补上的是"快照"补不了的一环：黄金快照守的是**路由表本身**没少条目，
// 它守不住「新加的管理端路由忘了配权限」。忘配的表现是 40315，
// 也就是那条接口在控制台上直接不可用 —— 但只有打开对应页面才看得出来。
//
// 判据取路由前缀而不是中间件链：中间件链只在 gin 的 debug 档采集得到，
// 而这几个前缀恰好就是 router.go 里挂 AdminAccess 的那几个路由组。
func TestEveryAdminRouteHasAuthzRule(t *testing.T) {
	engine := newTestRouter(t)

	// 只挂 AdminAuth（不做权限点判定）的路由组，不经过规则表。
	exemptPrefixes := []string{
		"/api/admin/auth/",
		"/api/admin/captcha/",
		"/api/admin/profile",
		"/api/admin/avatar",
		"/api/admin/notifications",
	}
	guardedPrefixes := []string{
		"/api/admin/",
		"/api/app/password-policy",
		"/api/app/points",
		"/api/app/workflow",
	}

	var missing []string
	for _, route := range engine.Routes() {
		if !hasAnyPrefix(route.Path, guardedPrefixes) || hasAnyPrefix(route.Path, exemptPrefixes) {
			continue
		}
		if _, ok := authz.ResolveRoute(route.Method, route.Path); !ok {
			missing = append(missing, route.Method+" "+route.Path)
		}
	}
	for _, item := range missing {
		t.Errorf("管理端路由未登记权限规则（会被 40315 默认拒绝）：%s —— "+
			"请在 internal/authz/routes.go 的 adminRouteRules 里补一行", item)
	}
}

func hasAnyPrefix(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}
