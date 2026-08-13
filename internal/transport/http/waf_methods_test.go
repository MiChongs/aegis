package httptransport

import (
	"sort"
	"strings"
	"testing"

	"aegis/internal/middleware"
)

// WAF 的方法白名单（middleware.CorazaAllowedMethods）与真实路由表必须对齐。
//
// 这两份清单分处两个包，谁也看不见谁：新增一种 HTTP 方法的路由时，
// CRS 911100 会给该方法的**每个**请求加 5 分异常分，表现是"平时好用、
// 偶尔 403"—— 既不指向这条路由，也不指向 WAF 配置。
// 反过来，白名单里多写一个路由表根本不用的方法则毫无收益，只是放宽了
// 方法校验的覆盖面，所以两边取严格相等。
func TestRegisteredRouteMethodsAreAllowedByWAF(t *testing.T) {
	engine := newTestRouter(t)

	allowed := map[string]bool{}
	for _, method := range strings.Fields(middleware.CorazaAllowedMethods) {
		allowed[method] = true
	}

	registered := map[string]bool{}
	for _, route := range engine.Routes() {
		registered[route.Method] = true
	}

	for method := range registered {
		if !allowed[method] {
			t.Errorf("路由表用了 %s，但它不在 middleware.CorazaAllowedMethods 里："+
				"该方法的所有接口都会被 CRS 911100 持续扣异常分", method)
		}
	}

	// OPTIONS 不出现在 Routes() 里：带 Origin 的预检由 CORS 中间件短路，
	// 其余由 NoMethod 兜底成 204 + Allow（见 route_methods.go）。
	// 它照样是客户端真会发的方法，白名单必须留着。
	//
	// HEAD 则**不是**隐式的 —— gin 按方法分树，GET 不会顺带响应 HEAD，
	// 探针与头像那几条是显式注册的，所以它已经出现在 registered 里，
	// 无须在这里豁免。留在这个集合里只是为了：万一那几条被删光，
	// 白名单里的 HEAD 也不该被判成"该收回"。
	implicit := map[string]bool{"HEAD": true, "OPTIONS": true}
	var stale []string
	for method := range allowed {
		if !registered[method] && !implicit[method] {
			stale = append(stale, method)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("白名单里的 %v 在路由表中已无对应路由，应当收回", stale)
	}
}
