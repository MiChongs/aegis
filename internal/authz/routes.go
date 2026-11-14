package authz

import (
	"net/http"
	"sort"
	"strings"
)

// 路由 → 权限点的映射表。
//
// 取代 internal/middleware/admin.go 里那段 250 行的 switch。旧写法有三个structural问题，
// 都不会在编译期暴露、只会在某个页面上表现为「空白」或「点了才 403」：
//
//  1. **`strings.Contains` 不锚定。** 判「这是不是用户相关接口」写成
//     `strings.Contains(path, "/users")`，于是任何路径里出现 users 的接口都被算进去。
//  2. **`isCompatReadPath` 是后缀匹配。** 一条以 `/list` 结尾的写接口会被判成读，
//     反过来漏登记一个读路径（`/deliveries`）会让只读管理员打开列表就吃 403。
//  3. **分支顺序即优先级，但顺序藏在嵌套 switch 里。** 一条新加的规则很容易
//     被上面某条宽泛的前缀整个遮住，而被遮住的规则不会报错，只是永不生效。
//
// 现在是一张**有序的、锚定的**规则表：模式按段匹配，`:param` 吃一段，`*` 吃剩余全部
// （含零段）。顺序仍是先到先得，但有两条测试守着：
// route_permissions.json 逐条钉住 941 条真实路由的判定结果不变，
// TestNoUnreachableRouteRule 保证没有哪条规则被上面的规则完全遮住。

// Scope 权限判定的作用域。
type Scope uint8

const (
	// ScopeGlobal 平台作用域：判定时 appID 传 nil。
	ScopeGlobal Scope = iota
	// ScopeApp 应用作用域：需要从路径 / query / body 里解析出 appid。
	//
	// **判错这一项不会报错，只会让页面静默空掉** —— 链路是
	// ScopeApp → 解析不到应用标识 → 40058，而控制台的 React Query
	// 只是拿不到数据，面板照常渲染成「暂无」。
	ScopeApp
)

// RouteRule 一条路由权限规则。
type RouteRule struct {
	// Methods 为空表示任意方法。
	Methods []string
	// Pattern 路径模式，按 `/` 分段匹配；`:name` 匹配任意一段，`*` 匹配剩余任意段数（含零段）。
	Pattern string
	// Permission 为空表示"进模块即可"，只要求已登录 —— 细粒度判定下沉到 service 层。
	Permission string
	Scope      Scope
	// Note 说明这条规则为什么是这样，尤其是"为什么不要权限点"和"为什么不是应用级"。
	Note string
}

// RouteDecision 路由解析结果。
type RouteDecision struct {
	Permission string
	Scope      Scope
	// Matched 命中的规则（供管理端自检展示"这条路由是被哪条规则判的"）。
	Matched *RouteRule
}

// AppScoped 是否按应用作用域判定。
func (d RouteDecision) AppScoped() bool { return d.Scope == ScopeApp }

var readMethods = []string{http.MethodGet, http.MethodHead, http.MethodOptions}

// ResolveRoute 解析一条管理端路由需要的权限点与作用域。
//
// 第二个返回值为 false 表示这条路由**没有登记**：调用方必须按默认拒绝处理。
// 默认放行会让一条忘了登记的新管理接口以"人人可调"的形式上线，且没有任何地方报错。
func ResolveRoute(method, path string) (RouteDecision, bool) {
	for i := range adminRouteRules {
		rule := &adminRouteRules[i]
		if !rule.matches(method, path) {
			continue
		}
		return RouteDecision{Permission: rule.Permission, Scope: rule.Scope, Matched: rule}, true
	}
	return RouteDecision{}, false
}

// AdminRouteRules 返回规则表的只读快照，供管理端自检与测试使用。
func AdminRouteRules() []RouteRule {
	items := make([]RouteRule, len(adminRouteRules))
	copy(items, adminRouteRules)
	return items
}

func (r *RouteRule) matches(method, path string) bool {
	if len(r.Methods) > 0 && !containsFold(r.Methods, method) {
		return false
	}
	return matchRoutePattern(path, r.Pattern)
}

func containsFold(items []string, want string) bool {
	for _, item := range items {
		if strings.EqualFold(item, want) {
			return true
		}
	}
	return false
}

// matchRoutePattern 按段匹配路径模式。
//
// 刻意不用 Casbin 的 util.KeyMatch2：它把 `/*` 换成正则 `/.*`，于是
// `/a/b/*` **匹配不到** `/a/b` 本身（少了那个斜杠），而路由表里
// 「集合根」与「集合下的子资源」几乎总是同一组权限。用自己的分段匹配可以让
// `*` 表示"剩余零段或多段"，一条规则就覆盖两者，也不必为此再补一条特例。
//
// 另外这里匹配的是 gin 的**路由模板**（`c.FullPath()`，形如 /api/admin/apps/:appkey/users），
// 不是具体 URL。因此模式里的 `:appkey` 与模板里的 `:appkey` 是字面相等的，
// 而 `:name` 这条通配规则用来吃掉那些名字不一样的参数段（:userId / :id / :vid …）。
func matchRoutePattern(path, pattern string) bool {
	pathSegments := splitPath(path)
	patternSegments := splitPath(pattern)

	for i, segment := range patternSegments {
		if segment == "*" {
			return true // 尾部通配吃掉剩余全部（含零段）
		}
		if i >= len(pathSegments) {
			return false
		}
		if strings.HasPrefix(segment, ":") {
			continue // 参数段吃掉一段，内容不限
		}
		if segment != pathSegments[i] {
			return false
		}
	}
	return len(pathSegments) == len(patternSegments)
}

func splitPath(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

// adminRouteRules 规则表：开放接口 + 权限树上每个操作节点登记的接口，按特异性排序。
//
// 不再手排顺序。排序键（先到先得）：
//  1. 精确模式（不含 `*`）先于前缀模式；
//  2. 字面段多的先于少的（`/apps/:appkey/users/:userId/bans/*` 先于 `/apps/:appkey/users/*`）；
//  3. 总段数多的在前；
//  4. 限定了方法的先于不限方法的（`GET /x/*` 先于 `* /x/*`）；
//  5. 其余按声明顺序（开放接口在前，然后是树序）。
//
// 手排的老问题是「新规则被上面一条更宽的规则整个遮住、永不生效且不报错」，
// 排序之后剩下的歧义由 TestNoUnreachableRouteRule 与逐条基线比对兜住。
var adminRouteRules = buildRouteRules()

func buildRouteRules() []RouteRule {
	rules := make([]RouteRule, 0, 1024)
	rules = append(rules, openRoutes...)
	var walk func(nodes []PermissionNode)
	walk = func(nodes []PermissionNode) {
		for _, node := range nodes {
			if node.Kind == NodeAction {
				for _, spec := range node.routes {
					rules = append(rules, RouteRule{
						Methods:    spec.methods,
						Pattern:    spec.pattern,
						Permission: node.Code,
						Scope:      scopeForPattern(spec.pattern),
						Note:       spec.note,
					})
				}
				continue
			}
			walk(node.Children)
		}
	}
	walk(permissionTree())
	sort.SliceStable(rules, func(i, j int) bool {
		return routeRuleLess(rules[i], rules[j])
	})
	return rules
}

func routeRuleLess(a, b RouteRule) bool {
	aw, bw := strings.Contains(a.Pattern, "*"), strings.Contains(b.Pattern, "*")
	if aw != bw {
		return !aw
	}
	al, bl := literalSegments(a.Pattern), literalSegments(b.Pattern)
	if al != bl {
		return al > bl
	}
	at, bt := len(splitPath(a.Pattern)), len(splitPath(b.Pattern))
	if at != bt {
		return at > bt
	}
	am, bm := len(a.Methods) > 0, len(b.Methods) > 0
	if am != bm {
		return am
	}
	return false
}

func literalSegments(pattern string) int {
	count := 0
	for _, segment := range splitPath(pattern) {
		if segment != "*" && !strings.HasPrefix(segment, ":") {
			count++
		}
	}
	return count
}

// scopeForPattern 由路由前缀决定作用域。
func scopeForPattern(pattern string) Scope {
	for _, exception := range globalScopeExceptions {
		if pattern == exception {
			return ScopeGlobal
		}
	}
	for _, prefix := range appScopedPrefixes {
		if pattern == prefix || strings.HasPrefix(pattern, strings.TrimSuffix(prefix, "/")+"/") {
			return ScopeApp
		}
	}
	return ScopeGlobal
}

// routeCountsByPermission 每个权限点覆盖的规则条数（控制台在树上展示「涵盖 N 个接口」）。
func routeCountsByPermission() map[string]int {
	counts := map[string]int{}
	for _, rule := range adminRouteRules {
		if rule.Permission != "" {
			counts[rule.Permission]++
		}
	}
	return counts
}

// RoutesForPermission 某个权限点覆盖的路由规则（控制台的权限点详情里列出来）。
func RoutesForPermission(code string) []RouteRule {
	items := make([]RouteRule, 0, 4)
	for _, rule := range adminRouteRules {
		if rule.Permission == code {
			items = append(items, rule)
		}
	}
	return items
}
