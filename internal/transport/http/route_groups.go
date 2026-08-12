package httptransport

import (
	"slices"
	"sort"
	"strings"

	"aegis/pkg/routetable"

	"github.com/gin-gonic/gin"
)

// 路由命名空间规则表 —— 本文件是「一条路由属于哪个命名空间」的**单一事实源**。
//
// 同一张表同时喂两个消费方，刻意不许它们各有一套：
//
//	deriveTags      OpenAPI 标签（英文，对外规范与生成式客户端的分组依据）
//	RouteInventory  路由清单（中文，启动横幅与 routes 子命令的分组依据）
//
// 分成两张表的后果是可预见的：新增一个命名空间时只改了一处，于是控制台的
// 接口浏览器把它归进 Admin，启动清单却把它算进「未分组」，两边都不算错也都不对。
//
// 匹配单位与展示单位是分开的：
//
//	规则（routeGroup）是匹配单位——一条规则一个 Tag，因为 OpenAPI 标签的粒度
//	                             是既定事实，改动会动到对外规范
//	分组（Title）是展示单位——多条规则可以共用一个 Title
//
// 这个分离是必需的：「公开元数据」这一个展示分组里既有 /api/app/public
// （历史上归 App Compat）也有 /api/public/branding（归 API），
// 硬要一组一个 Tag 就只能二选一，那就改动了对外规范。
//
// 顺序即优先级，先命中先算，因此**具体规则必须排在宽泛规则之前**。
var routeGroups = []routeGroup{
	// ── 公开：不需要任何凭据 ────────────────────────────────────────────
	{Realm: realmPublic, Title: "站点页面", Tag: tagAPI, Auth: authPublic,
		Exact: []string{"/", "/status", "/announcements", "/cons-env"}},
	{Realm: realmPublic, Title: "健康探针", Tag: "System", Auth: authPublic,
		Exact: []string{"/healthz", "/readyz"}},
	{Realm: realmPublic, Title: "文档出口", Tag: tagAPI, Auth: authPublic,
		Exact: []string{"/openapi.json", "/docs"}, Prefix: "/docs/"},
	// /api/app/public 单独一条：它历史上归 App Compat 标签，
	// 与同一展示分组里的其他公开接口（归 API）不同，只能各写一条规则。
	{Realm: realmPublic, Title: "公开元数据", Tag: tagAppCompat, Auth: authPublic,
		Exact: []string{"/api/app/public"}},
	{Realm: realmPublic, Title: "公开元数据", Tag: tagAPI, Auth: authPublic,
		Exact:  []string{"/api/public/branding", "/api/system/announcements/active"},
		Prefix: "/api/avatar/"},
	// 永久头像地址。免登录是这条路由的**前提**而不是疏漏：它出现在 <img src>
	// 与邮件正文里，那两处都没有机会带上 Authorization 头。防遍历靠地址
	// 自带的签名，见 internal/service/avatar_link.go。
	{Realm: realmPublic, Title: "头像", Tag: tagAPI, Auth: authPublic,
		Prefix: "/api/avatars/"},
	// 登录页与注册页在用户还没有账号时就要链到条款，要求登录才能读是荒谬的；
	// 路径刻意不挂在 /api/admin/* 下，正是为了不落进任何管理端鉴权规则
	{Realm: realmPublic, Title: "法律文本-公开读", Tag: tagAPI, Auth: authPublic,
		Prefix: "/api/legal/"},

	// ── 网关：接入方唯一需要认识的命名空间（docs/app-integration.md）──────
	// /config 必须免包装可读，否则客户端陷入「要读配置得先按配置包装」的死锁
	{Realm: realmGateway, Title: "网关-配置与握手", Tag: tagAPI, Auth: authGatewayPlain,
		Exact: []string{"/api/v1/apps/*/config"}},
	// 回跳由第三方平台重定向浏览器发起，客户端没机会给它签名或加密。
	// 单独成组而不是混进「认证生命周期」，是因为鉴权标注在清单里是**分组**属性
	// （见 routetable 的分组单元格）：一组里混着两种鉴权，标注就只能说其中一种。
	{Realm: realmGateway, Title: "网关-认证回跳", Tag: tagAPI, Auth: authGatewayPlain,
		Exact: []string{"/api/v1/apps/*/auth/oauth/callback"}},
	{Realm: realmGateway, Title: "网关-认证生命周期", Tag: tagAPI, Auth: authGatewayWrap,
		Prefix: "/api/v1/apps/*/auth/"},
	{Realm: realmGateway, Title: "网关-账户与资料", Tag: tagAPI, Auth: authGatewayBearer,
		Prefix: "/api/v1/apps/*/me"},
	{Realm: realmGateway, Title: "网关-应用能力", Tag: tagAPI, Auth: authGatewayWrap,
		Prefix: "/api/v1/apps/"},

	// ── 管理端：/api/admin/* ───────────────────────────────────────────
	{Realm: realmAdmin, Title: "管理端认证", Tag: "Admin Auth", Auth: authAdminLogin,
		Prefix: "/api/admin/auth/"},
	// 恒全局作用域：一旦变成应用级，被冻结应用自己的管理员就能给自己解封
	{Realm: realmAdmin, Title: "平台治理", Tag: tagAdmin, Auth: authPlatformGovern,
		Prefix: "/api/admin/platform"},
	{Realm: realmAdmin, Title: "组织架构", Tag: tagAdminSystem, Auth: authAdminRBAC,
		Prefix: "/api/admin/system/organizations"},
	{Realm: realmAdmin, Title: "组织架构", Tag: tagAdminSystem, Auth: authAdminRBAC,
		Prefix: "/api/admin/system/org-invitations"},
	{Realm: realmAdmin, Title: "用户主库", Tag: tagAdminSystem, Auth: authAdminRBAC,
		Prefix: "/api/admin/system/user-master"},
	{Realm: realmAdmin, Title: "风控中心", Tag: tagAdminSystem, Auth: authAdminRBAC,
		Prefix: "/api/admin/system/risk"},
	{Realm: realmAdmin, Title: "存储管理", Tag: tagAdminSystem, Auth: authAdminRBAC,
		Prefix: "/api/admin/system/storage"},
	{Realm: realmAdmin, Title: "防火墙与封禁", Tag: tagAdminSystem, Auth: authAdminRBAC,
		Prefix: "/api/admin/system/firewall"},
	{Realm: realmAdmin, Title: "出海网关", Tag: tagAdminSystem, Auth: authSuperAdmin,
		Prefix: "/api/admin/system/egress"},
	{Realm: realmAdmin, Title: "法律文本", Tag: tagAdminSystem, Auth: authSuperAdmin,
		Prefix: "/api/admin/system/legal"},
	{Realm: realmAdmin, Title: "平台设置", Tag: tagAdminSystem, Auth: authAdminRBAC,
		Prefix: "/api/admin/system/"},
	{Realm: realmAdmin, Title: "应用管理", Tag: tagAdmin, Auth: authAdminRBAC,
		Prefix: "/api/admin/apps"},
	{Realm: realmAdmin, Title: "应用配置-兼容", Tag: tagAdmin, Auth: authAdminRBAC,
		Prefix: "/api/admin/app/"},
	{Realm: realmAdmin, Title: "工单管理端", Tag: tagAdmin, Auth: authAdminTicket,
		Prefix: "/api/admin/tickets"},
	{Realm: realmAdmin, Title: "统一通知出口", Tag: tagAdmin, Auth: authAdminNotify,
		Prefix: "/api/admin/notify"},
	{Realm: realmAdmin, Title: "管理员自身资料", Tag: tagAdmin, Auth: authAdminSession,
		Prefix: "/api/admin/profile"},
	{Realm: realmAdmin, Title: "管理端其他", Tag: tagAdmin, Auth: authAdminRBAC,
		Prefix: "/api/admin/"},

	// ── 应用兼容：/api/app/*（旧管理端命名空间）─────────────────────────
	{Realm: realmAppCompat, Title: "工作流", Tag: "Workflow", Auth: authAdminRBAC,
		Prefix: "/api/app/workflow"},
	{Realm: realmAppCompat, Title: "密码策略", Tag: tagAppCompat, Auth: authAdminRBAC,
		Prefix: "/api/app/password-policy"},
	{Realm: realmAppCompat, Title: "积分管理", Tag: tagAppCompat, Auth: authAdminRBAC,
		Prefix: "/api/app/points"},
	// 公钥公开可取（接入方要用它验 Aegis 发出的回调签名），调用本身要用户令牌。
	// 两种鉴权 → 两个分组，理由同「网关-认证回跳」。
	{Realm: realmAppCompat, Title: "应用函数-公钥", Tag: tagAPI, Auth: authPublic,
		Exact: []string{"/api/functions/signing-key"}},
	{Realm: realmAppCompat, Title: "应用函数-调用", Tag: tagAPI, Auth: authBearer,
		Prefix: "/api/apps/"},
	{Realm: realmAppCompat, Title: "应用兼容其他", Tag: tagAppCompat, Auth: authAdminRBAC,
		Prefix: "/api/app/", Fallback: true},

	// ── 用户端 ────────────────────────────────────────────────────────
	// 旧明文认证命名空间，由每个应用的 allowLegacy 开关控制
	{Realm: realmUser, Title: "旧明文认证", Tag: "Auth", Auth: authLegacyAuth,
		Prefix: "/api/auth/"},
	{Realm: realmUser, Title: "用户设置", Tag: "User Settings", Auth: authBearer,
		Prefix: "/api/user-settings"},
	{Realm: realmUser, Title: "用户工单", Tag: "User", Auth: authBearer,
		Prefix: "/api/user/tickets"},
	{Realm: realmUser, Title: "用户资料与安全", Tag: "User", Auth: authBearer,
		Prefix: "/api/user/"},
	{Realm: realmUser, Title: "积分与签到", Tag: "Points", Auth: authBearer,
		Prefix: "/api/points"},
	{Realm: realmUser, Title: "站内信", Tag: "Notifications", Auth: authBearer,
		Prefix: "/api/notifications"},
	{Realm: realmUser, Title: "邮件", Tag: "Email", Auth: authEmail,
		Prefix: "/api/email"},
	// 渠道回调由支付平台服务端发起，没有用户会话，靠签名校验而不是 Bearer
	{Realm: realmUser, Title: "支付回调", Tag: "Public Payment", Auth: authPayCallback,
		Prefix: "/api/public/pay"},
	{Realm: realmUser, Title: "支付", Tag: "Payment", Auth: authBearer,
		Prefix: "/api/pay"},
	{Realm: realmUser, Title: "存储", Tag: "Storage", Auth: authBearer,
		Prefix: "/api/storage"},
	{Realm: realmUser, Title: "钱包", Tag: tagAPI, Auth: authBearer,
		Prefix: "/api/wallet"},
	{Realm: realmUser, Title: "会员", Tag: tagAPI, Auth: authBearer,
		Prefix: "/api/vip"},
	{Realm: realmUser, Title: "抽奖", Tag: tagAPI, Auth: authBearer,
		Prefix: "/api/lottery"},
	{Realm: realmUser, Title: "排行榜", Tag: tagAPI, Auth: authBearer,
		Prefix: "/api/leaderboard"},
	{Realm: realmUser, Title: "验证码", Tag: tagAPI, Auth: authPublic,
		Prefix: "/api/captcha"},

	// ── 系统与实时 ────────────────────────────────────────────────────
	{Realm: realmSystem, Title: "实时通信", Tag: "Realtime", Auth: authWebSocket,
		Exact: []string{"/api/ws"}},
	{Realm: realmSystem, Title: "系统监控", Tag: tagAPI, Auth: authSuperAdmin,
		Prefix: "/api/system/monitor"},
	{Realm: realmSystem, Title: "系统其他", Tag: tagAPI, Auth: authAdminSession,
		Prefix: "/api/system/", Fallback: true},
}

// 顶层域。粒度刻意比分组粗一档：启动横幅只放得下几行。
const (
	realmPublic    = "公开"
	realmGateway   = "接入网关"
	realmAdmin     = "管理端"
	realmAppCompat = "应用兼容"
	realmUser      = "用户端"
	realmSystem    = "系统"
	realmUngrouped = "未分组"
)

// OpenAPI 标签。这批字面量是对外规范的一部分（生成式客户端按它分包），
// 抽成常量是为了改动时能一眼看到影响面，而不是散在几十条规则里。
const (
	tagAPI         = "API"
	tagAdmin       = "Admin"
	tagAdminSystem = "Admin System"
	tagAppCompat   = "App Compat"
)

// 鉴权标注。
//
// 一条硬约束：**不得含 U+00B7「·」这类 East Asian Ambiguous 宽度字符**。
// 它们在中日韩控制台里被算成 2 列、实际渲染 1 列，会把路由表的右边框顶歪，
// 而且只在特定 locale 下发作。分隔一律用 ASCII 的 " / " 或 " + "。
// pkg/routetable 的 TestTableRowsAlignExactly 从渲染侧盯这件事。
const (
	authPublic         = "公开"
	authGatewayPlain   = "免包装"
	authGatewayWrap    = "网关拆包"
	authGatewayBearer  = "网关拆包 + Bearer"
	authAdminLogin     = "登录注册公开 / 其余管理员会话"
	authPlatformGovern = "超管或全局 platform:app:govern"
	authAdminSession   = "管理员会话"
	authAdminRBAC      = "管理员会话 + RBAC"
	authAdminTicket    = "管理员会话 + 细粒度在 service 层"
	authAdminNotify    = "管理员会话 / 写操作叠加超管"
	authSuperAdmin     = "超管"
	authLegacyAuth     = "视接口而定 / 受 allowLegacy 控制"
	authBearer         = "Bearer"
	authEmail          = "部分公开 / 其余 Bearer"
	authPayCallback    = "渠道回调签名"
	authWebSocket      = "Bearer 或 query token"
)

// routeGroup 是一条命名空间规则。
type routeGroup struct {
	Realm string
	Title string
	Tag   string
	Auth  string
	// Exact 是精确路径（已规范化，路径参数写成 *）。
	Exact []string
	// Prefix 是前缀（已规范化）。写不写尾斜杠是有讲究的：
	// 带尾斜杠只匹配子路径，不带则连该段本身也匹配。
	Prefix string
	// Fallback 标记「命名空间兜底」规则：允许它当前一条路由都匹配不到。
	//
	// 这类规则不是死代码，它保住的是整个命名空间的 OpenAPI 标签语义。
	// 以 `/api/app/` 为例：现存路由已被 workflow / password-policy / points
	// 三条更具体的规则分完，但把这条兜底删掉之后，将来新增的 `/api/app/foo`
	// 会落进全局兜底、拿到 `API` 而不是历史上的 `App Compat` ——
	// 那是一次没人会注意到的对外规范变更。
	//
	// TestRuleTableHasNoUnreachableRule 只对没打这个标记的规则要求「必须命中」。
	Fallback bool
}

// matches 报告规则是否命中某条**已规范化**的路径。
func (g routeGroup) matches(canonical string) bool {
	if slices.Contains(g.Exact, canonical) {
		return true
	}
	return g.Prefix != "" && strings.HasPrefix(canonical, g.Prefix)
}

// ungroupedRule 是兜底规则。它永远不该被命中——
// TestEveryRouteMatchesAnExplicitGroup 钉着这条。
// 存在的意义是新增命名空间时给出一个显眼的「未分组」而不是静默归进某个现有分组：
// 静默归错比显式未分组难发现得多。
var ungroupedRule = routeGroup{Realm: realmUngrouped, Title: "未分组", Tag: tagAPI}

// canonicalRoutePath 把路径参数统一成 `*`，让规则表只写一遍。
//
// 同一条路由在两个消费方手里长得不一样：gin 的路由表是 `:appkey`，
// OpenAPI 规范里是 `{appkey}`，gin 的catch-all 又是 `*filepath`。
// 规则表要是按其中一种写法来，另一种就整片匹配不上——
// 而匹配不上的后果是静默归进兜底分组，不报错。
func canonicalRoutePath(path string) string {
	if !strings.ContainsAny(path, ":{*") {
		return path
	}
	parts := strings.Split(path, "/")
	for i, part := range parts {
		switch {
		case strings.HasPrefix(part, ":"),
			strings.HasPrefix(part, "*"),
			strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}"):
			parts[i] = "*"
		}
	}
	return strings.Join(parts, "/")
}

// matchRouteGroup 返回路径所属的规则，未命中任何规则时返回兜底规则。
func matchRouteGroup(path string) routeGroup {
	canonical := canonicalRoutePath(path)
	for _, rule := range routeGroups {
		if rule.matches(canonical) {
			return rule
		}
	}
	return ungroupedRule
}

// deriveTags 返回路径的 OpenAPI 标签。
//
// 这里读的就是上面那张规则表，因此控制台接口浏览器的分组、生成式客户端的分包
// 与启动时打印的路由清单永远说同一件事。
func deriveTags(path string) []string {
	return []string{matchRouteGroup(path).Tag}
}

// ── 路由清单 ───────────────────────────────────────────────────────────

// RouteInventory 把 gin 的路由表整理成可渲染的分组清单。
//
// 组的顺序跟随规则表的声明顺序（公开 → 网关 → 管理端 → 用户端 → 系统），
// 这个层次本身携带信息，按名字重排会把它打散。
func RouteInventory(engine *gin.Engine) routetable.Inventory {
	if engine == nil {
		return routetable.Inventory{Title: routeInventoryTitle}
	}

	chains := routeChainDepths()

	// 规则表里多条规则可以共用一个 Title（见文件头注释），
	// 因此这里按 (Realm, Title) 归并，并用规则表的出现顺序定组序。
	type groupKey struct{ realm, title string }
	index := map[groupKey]int{}
	groups := make([]routetable.Group, 0, len(routeGroups))

	for _, route := range engine.Routes() {
		rule := matchRouteGroup(route.Path)
		key := groupKey{rule.Realm, rule.Title}
		i, ok := index[key]
		if !ok {
			index[key] = len(groups)
			groups = append(groups, routetable.Group{
				Realm: rule.Realm,
				Title: rule.Title,
				Auth:  rule.Auth,
			})
			i = len(groups) - 1
		}
		groups[i].Routes = append(groups[i].Routes, routetable.Route{
			Method:  route.Method,
			Path:    route.Path,
			Handler: shortHandlerName(route.Handler),
			Chain:   chains[routeKey(route.Method, route.Path)],
			Auth:    rule.Auth,
		})
	}

	// 组序按规则表的声明顺序，而不是路由被 gin 遍历到的顺序——
	// gin 的 Routes() 顺序由前缀树的形状决定，读起来是随机的。
	sortGroupsByRuleOrder(groups)
	for i := range groups {
		groups[i].Prefix = commonPathPrefix(groups[i].Routes)
	}
	return routetable.Inventory{Title: routeInventoryTitle, Groups: groups}
}

const routeInventoryTitle = "Aegis 路由清单"

// sortGroupsByRuleOrder 按规则表里首次出现的位置给组排序。
func sortGroupsByRuleOrder(groups []routetable.Group) {
	rank := func(g routetable.Group) int {
		for i, rule := range routeGroups {
			if rule.Realm == g.Realm && rule.Title == g.Title {
				return i
			}
		}
		return len(routeGroups) // 兜底分组排最后，正好最显眼
	}
	sort.SliceStable(groups, func(i, j int) bool { return rank(groups[i]) < rank(groups[j]) })
}

// commonPathPrefix 求一组路由路径的最长公共前缀，按 `/` 分段取整。
//
// 自动求而不是在规则表里手写：手写的前缀会随路由增删悄悄失真，
// 而这一列一旦不准，读表的人会以为某个分组下没有别的路径。
func commonPathPrefix(routes []routetable.Route) string {
	if len(routes) < 2 {
		return ""
	}
	segments := strings.Split(strings.Trim(routes[0].Path, "/"), "/")
	for _, r := range routes[1:] {
		other := strings.Split(strings.Trim(r.Path, "/"), "/")
		if len(other) < len(segments) {
			segments = segments[:len(other)]
		}
		for i := range segments {
			if segments[i] != other[i] {
				segments = segments[:i]
				break
			}
		}
		if len(segments) == 0 {
			return ""
		}
	}
	if len(segments) == 0 {
		return ""
	}
	return "/" + strings.Join(segments, "/")
}

// shortHandlerName 从 gin 记下的完整符号名里取出可读的处理器名。
//
// gin 存的是运行时符号名，例如
//
//	aegis/internal/transport/http.(*Handler).AppConfig-fm   → AppConfig
//	aegis/internal/transport/http.NewRouter.func3           → NewRouter.func3
//
// 其中 `-fm` 是方法值（method value）的包装函数后缀。整串打出来会占掉半张表，
// 而有用的信息只有最后那一两截。
//
// 匿名函数要多留一截：只剩 `func3` 的话，清单里会出现好几个都叫 func1/func2
// 的处理器，读者无法分辨它们是不是同一个东西；带上外层函数名就能定位到源码。
func shortHandlerName(full string) string {
	segments := strings.Split(full, ".")
	if len(segments) == 0 {
		return full
	}
	last := strings.TrimSuffix(segments[len(segments)-1], "-fm")
	if !isAnonymousFuncSegment(last) || len(segments) < 2 {
		return last
	}
	return segments[len(segments)-2] + "." + last
}

// isAnonymousFuncSegment 判断某一截是不是 Go 给匿名函数生成的 funcN 形式。
func isAnonymousFuncSegment(segment string) bool {
	digits, ok := strings.CutPrefix(segment, "func")
	if !ok || digits == "" {
		return false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
