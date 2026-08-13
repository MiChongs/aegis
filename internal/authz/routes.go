package authz

import (
	"net/http"
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

// adminRouteRules 规则表。**先到先得**，因此排列顺序是从具体到宽泛。
//
// 加一条新的管理端路由时往这里补一行即可；忘了补的表现是 40315
// 「该管理端接口尚未登记权限规则」，而不是静默放行。
var adminRouteRules = []RouteRule{
	// ── 工作台 ──
	{Pattern: "/api/admin/dashboard", Note: "任意已登录管理员可见，内容按身份裁剪"},

	// ── 平台治理：恒全局作用域 ──
	//
	// 这一组接口的语义就是"跨应用"。带上 appid 会让作用域匹配认可
	// 「绑定到该应用的角色」所持有的权限点，于是被治理应用自己的管理员
	// 就能给自己解封 —— 而那正是这套机制存在的意义所反对的。
	{Pattern: "/api/admin/platform/catalog", Note: "状态/能力目录是编译进二进制的静态表，被治理方也要靠它读懂自己的处境"},
	{Pattern: "/api/admin/platform/storage-config/list", Permission: PermPlatformStorageRead},
	{Pattern: "/api/admin/platform/storage-config/detail", Permission: PermPlatformStorageRead},
	{Pattern: "/api/admin/platform/storage-config/*", Permission: PermPlatformStorageWrite},
	{Pattern: "/api/admin/platform/apps/:appkey/revoke-sessions", Permission: PermPlatformAppDanger},
	{Pattern: "/api/admin/platform/governance/appeals/:appealId/review", Permission: PermPlatformAppealReview},
	{Methods: readMethods, Pattern: "/api/admin/platform/*", Permission: PermPlatformAppRead},
	{Pattern: "/api/admin/platform/*", Permission: PermPlatformAppGovern},

	// ── 工单：中间件只做"能不能进模块"的粗粒度闸门 ──
	//
	// 「能不能看/改这一条工单」由 TicketService 的 Scope + ActionSet 判定，
	// 因此不带任何权限点、但属于某处理组的管理员也必须放行进来。
	{Methods: readMethods, Pattern: "/api/admin/tickets/export", Permission: PermTicketExport},
	{Methods: readMethods, Pattern: "/api/admin/tickets/categories/*"},
	{Methods: readMethods, Pattern: "/api/admin/tickets/groups/*"},
	{Methods: readMethods, Pattern: "/api/admin/tickets/sla-policies/*"},
	{Methods: readMethods, Pattern: "/api/admin/tickets/quick-replies/*"},
	{Pattern: "/api/admin/tickets/categories/*", Permission: PermTicketManage},
	{Pattern: "/api/admin/tickets/groups/*", Permission: PermTicketManage},
	{Pattern: "/api/admin/tickets/sla-policies/*", Permission: PermTicketManage},
	{Pattern: "/api/admin/tickets/quick-replies/*", Permission: PermTicketManage},
	{Pattern: "/api/admin/tickets/*", Note: "列表/详情/回复/指派等：进模块即可，细粒度在 TicketService"},

	// ── 统一通知出口：渠道内含 IM 凭据 ──
	// 读要权限点，写由 RequireSuperAdmin 二次把关。
	{Pattern: "/api/admin/notify/catalog", Note: "静态目录，任意已登录管理员可读"},
	{Methods: readMethods, Pattern: "/api/admin/notify/deliveries/*", Permission: PermNotifyDeliveryRead},
	{Pattern: "/api/admin/notify/deliveries/*", Permission: PermNotifyChannelWrite},
	{Methods: readMethods, Pattern: "/api/admin/notify/*", Permission: PermNotifyChannelRead},
	{Pattern: "/api/admin/notify/*", Permission: PermNotifyChannelWrite},

	// ── 组织架构：GET 所有管理员可读，写操作需权限 ──
	{Methods: readMethods, Pattern: "/api/admin/system/organizations/*"},
	{Methods: []string{http.MethodPost}, Pattern: "/api/admin/system/organizations/*", Permission: PermOrgCreate},
	{Pattern: "/api/admin/system/organizations/*", Permission: PermOrgWrite},

	// ── 用户设置 ──
	{Pattern: "/api/admin/user-settings/stats", Permission: PermSystemUserSettingRead},
	{Pattern: "/api/admin/user-settings/user", Permission: PermSystemUserSettingRead},
	{Pattern: "/api/admin/user-settings/check-integrity", Permission: PermSystemUserSettingRead},
	{Pattern: "/api/admin/user-settings/*", Permission: PermSystemUserSettingWrite},

	// ── 法律文本 ──
	//
	// 单独一条而不是落进下面那个大桶：桶对应的权限点叫「管理员管理」，
	// 被它挡下时错误信息会说「缺少管理员管理权限」—— 而管理员想改的是隐私政策，
	// 这句提示会把人引到完全无关的地方去要权限。
	{Pattern: "/api/admin/system/legal/*", Permission: PermSystemLegalManage},

	// ── 平台级邮件通道 ──
	//
	// 与法律文本同理，单独登记而不是落进下面那个大桶：桶对应的权限点叫
	// 「管理员管理」，被它挡下时错误信息会说「缺少管理员管理权限」——
	// 而管理员想改的是发信配置，这句提示会把人引到完全无关的地方去要权限。
	//
	// 作用域恒为全局。平台通道对所有应用生效（还能被共享成应用的兜底出口），
	// 按应用作用域判定的话，任何一个应用管理员都能改到全站的发信出口 ——
	// 与 /api/admin/platform/* 那条「恒全局」是同一个理由。
	{Pattern: "/api/admin/system/email/providers", Note: "邮件服务商静态目录，不含租户数据与凭据"},
	{Methods: readMethods, Pattern: "/api/admin/system/email/*", Permission: PermEmailRead},
	{Pattern: "/api/admin/system/email/*", Permission: PermEmailWrite},

	// ── 其余 /api/admin/system/*：管理员与平台运维 ──
	//
	// 这是一个很粗的桶（两百多条路由共用一个权限点）。保持原样是为了让这次
	// 重构可逐条比对；要细化的话现在只需在上面按需插入更具体的规则，
	// 不必再动任何 Go 控制流。
	{Pattern: "/api/admin/system/*", Permission: PermSystemAdminManage},

	// ── 静态目录：不含租户数据与凭据，任意已登录管理员可读 ──
	//
	// 这类接口控制台是**不带 appid** 调用的，按 ScopeApp 判定会被 40058 拦掉；
	// 而只把作用域改成全局仍不够 —— 全局作用域只认全局授权，
	// 应用级管理员会从 400 变成 403，页面同样是空的。因此权限点也要一并去掉。
	{Pattern: "/api/admin/oauth-providers/templates", Note: "第三方登录渠道模板"},
	{Pattern: "/api/admin/app/payment-config/methods", Note: "平台支持哪些支付渠道（Provider.Describe() 的静态目录）"},
	{Pattern: "/api/app/workflow/node-types", Note: "工作流节点类型目录"},
	{Pattern: "/api/app/workflow/engine/status", Note: "Temporal 连通性"},

	// ── 策略模板（平台级只读目录）──
	{Pattern: "/api/app/password-policy/templates", Permission: PermPlatformAppRead},
	{Pattern: "/api/admin/apps/password-policy/templates", Permission: PermPlatformAppRead},
	{Pattern: "/api/admin/apps/signin-reward/templates", Permission: PermPlatformAppRead},

	// ── 应用兼容命名空间（POST 动词式旧接口）──
	{Pattern: "/api/app/password-policy/get", Permission: PermAppRead, Scope: ScopeApp},
	{Pattern: "/api/app/password-policy/*", Permission: PermAppWrite, Scope: ScopeApp},
	{Pattern: "/api/app/points/stats", Permission: PermPointsRead, Scope: ScopeApp},
	{Pattern: "/api/app/points/*", Permission: PermPointsWrite, Scope: ScopeApp},

	{Pattern: "/api/app/workflow/list", Permission: PermWorkflowRead, Scope: ScopeApp},
	{Pattern: "/api/app/workflow/detail", Permission: PermWorkflowRead, Scope: ScopeApp},
	{Pattern: "/api/app/workflow/info", Permission: PermWorkflowRead, Scope: ScopeApp},
	{Pattern: "/api/app/workflow/instances", Permission: PermWorkflowRead, Scope: ScopeApp},
	{Pattern: "/api/app/workflow/instances/list", Permission: PermWorkflowRead, Scope: ScopeApp},
	{Pattern: "/api/app/workflow/instances/info", Permission: PermWorkflowRead, Scope: ScopeApp},
	{Pattern: "/api/app/workflow/instance/detail", Permission: PermWorkflowRead, Scope: ScopeApp},
	{Pattern: "/api/app/workflow/tasks/todo", Permission: PermWorkflowRead, Scope: ScopeApp},
	{Pattern: "/api/app/workflow/task/detail", Permission: PermWorkflowRead, Scope: ScopeApp},
	{Pattern: "/api/app/workflow/task/history", Permission: PermWorkflowRead, Scope: ScopeApp},
	{Pattern: "/api/app/workflow/templates", Permission: PermWorkflowRead, Scope: ScopeApp},
	{Pattern: "/api/app/workflow/templates/list", Permission: PermWorkflowRead, Scope: ScopeApp},
	{Pattern: "/api/app/workflow/validate", Permission: PermWorkflowRead, Scope: ScopeApp},
	{Pattern: "/api/app/workflow/statistics", Permission: PermWorkflowRead, Scope: ScopeApp},
	{Pattern: "/api/app/workflow/logs", Permission: PermWorkflowRead, Scope: ScopeApp},
	{Pattern: "/api/app/workflow/*", Permission: PermWorkflowWrite, Scope: ScopeApp},

	{Pattern: "/api/admin/app/version/list", Permission: PermVersionRead, Scope: ScopeApp},
	{Pattern: "/api/admin/app/version/detail", Permission: PermVersionRead, Scope: ScopeApp},
	{Pattern: "/api/admin/app/version/stats", Permission: PermVersionRead, Scope: ScopeApp},
	{Pattern: "/api/admin/app/version/channel/list", Permission: PermVersionRead, Scope: ScopeApp},
	{Pattern: "/api/admin/app/version/channel/detail", Permission: PermVersionRead, Scope: ScopeApp},
	{Pattern: "/api/admin/app/version/channel/users", Permission: PermVersionRead, Scope: ScopeApp},
	{Pattern: "/api/admin/app/version/channel/preview-match", Permission: PermVersionRead, Scope: ScopeApp},
	{Pattern: "/api/admin/app/version/*", Permission: PermVersionWrite, Scope: ScopeApp},

	{Pattern: "/api/admin/app/site/list", Permission: PermSiteRead, Scope: ScopeApp},
	{Pattern: "/api/admin/app/site/detail", Permission: PermSiteRead, Scope: ScopeApp},
	{Pattern: "/api/admin/app/site/audit-list", Permission: PermSiteRead, Scope: ScopeApp},
	{Pattern: "/api/admin/app/site/audit-stats", Permission: PermSiteRead, Scope: ScopeApp},
	{Pattern: "/api/admin/app/site/user-sites", Permission: PermSiteRead, Scope: ScopeApp},
	{Pattern: "/api/admin/app/site/audit", Permission: PermSiteAudit, Scope: ScopeApp},
	{Pattern: "/api/admin/app/site/batch-audit", Permission: PermSiteWrite, Scope: ScopeApp,
		Note: "批量审核落在写权限：与旧实现一致（/audit 后缀判定只吃精确的 /audit）"},
	{Pattern: "/api/admin/app/site/*", Permission: PermSiteWrite, Scope: ScopeApp},

	{Pattern: "/api/admin/app/role-application/list", Permission: PermRoleApplicationRead, Scope: ScopeApp},
	{Pattern: "/api/admin/app/role-application/detail", Permission: PermRoleApplicationRead, Scope: ScopeApp},
	{Pattern: "/api/admin/app/role-application/statistics", Permission: PermRoleApplicationRead, Scope: ScopeApp},
	{Pattern: "/api/admin/app/role-application/*", Permission: PermRoleApplicationReview, Scope: ScopeApp},

	// 服务商目录是静态自述，不含租户数据也不含凭据；控制台**不带 appid** 调它，
	// 按 ScopeApp 判定会被 40058 拦掉（与支付渠道目录同一条）。
	{Pattern: "/api/admin/app/email-config/providers", Note: "平台支持哪些邮件服务商（发送器 Describe() 的静态目录）"},
	{Pattern: "/api/admin/app/email-config/list", Permission: PermEmailRead, Scope: ScopeApp},
	{Pattern: "/api/admin/app/email-config/detail", Permission: PermEmailRead, Scope: ScopeApp},
	{Pattern: "/api/admin/app/email-config/deliveries", Permission: PermEmailRead, Scope: ScopeApp},
	{Pattern: "/api/admin/app/email-config/stats", Permission: PermEmailRead, Scope: ScopeApp},
	{Pattern: "/api/admin/app/email-config/channel", Permission: PermEmailRead, Scope: ScopeApp},
	{Pattern: "/api/admin/app/email-config/*", Permission: PermEmailWrite, Scope: ScopeApp},

	{Pattern: "/api/admin/app/storage-config/list", Permission: PermStorageRead, Scope: ScopeApp},
	{Pattern: "/api/admin/app/storage-config/detail", Permission: PermStorageRead, Scope: ScopeApp},
	{Pattern: "/api/admin/app/storage-config/*", Permission: PermStorageWrite, Scope: ScopeApp},

	{Pattern: "/api/admin/app/payment-config/list", Permission: PermPaymentRead, Scope: ScopeApp},
	{Pattern: "/api/admin/app/payment-config/detail", Permission: PermPaymentRead, Scope: ScopeApp},
	{Pattern: "/api/admin/app/payment-config/orders/list", Permission: PermPaymentRead, Scope: ScopeApp},
	{Pattern: "/api/admin/app/payment-config/orders/detail", Permission: PermPaymentRead, Scope: ScopeApp},
	{Pattern: "/api/admin/app/payment-config/refunds/list", Permission: PermPaymentRead, Scope: ScopeApp},
	{Pattern: "/api/admin/app/payment-config/refunds/order", Permission: PermPaymentRead, Scope: ScopeApp},
	{Pattern: "/api/admin/app/payment-config/refunds/refundable", Permission: PermPaymentRead, Scope: ScopeApp},
	{Pattern: "/api/admin/app/payment-config/*", Permission: PermPaymentWrite, Scope: ScopeApp},

	// ── 应用集合根：列表按 app:read（handler 内按授权过滤），建应用见下 ──
	{Methods: readMethods, Pattern: "/api/admin/apps", Permission: PermAppRead},
	// 建应用**不要求权限点**：要求 app:write 会造出死锁 —— 自助注册出来的管理员
	// 一条角色都没有，而唯一能让他拿到权限的动作（建自己的第一个应用、成为它的
	// app_admin）本身就被 app:write 挡住。真正的闸门（平台开关 + 每人配额）
	// 在 AdminService.EnsureCanCreateApp，它要打库，这张纯内存表做不了。
	{Pattern: "/api/admin/apps", Note: "自助创建：闸门在 AdminService.EnsureCanCreateApp"},

	// ── 单个应用下的子资源（全部为应用作用域）──
	{Pattern: "/api/admin/apps/:appkey/users/:userId/audits/login", Permission: PermAuditLoginRead, Scope: ScopeApp},
	{Pattern: "/api/admin/apps/:appkey/users/:userId/audits/*", Permission: PermAuditSessionRead, Scope: ScopeApp},
	{Pattern: "/api/admin/apps/:appkey/audits/login/*", Permission: PermAuditLoginRead, Scope: ScopeApp},
	{Pattern: "/api/admin/apps/:appkey/audits/*", Permission: PermAuditSessionRead, Scope: ScopeApp},

	// 绑定记录是用户数据，按用户权限点判定；渠道配置本身按应用配置权限点判定
	{Methods: readMethods, Pattern: "/api/admin/apps/:appkey/oauth-bindings/*", Permission: PermAppUserRead, Scope: ScopeApp},
	{Pattern: "/api/admin/apps/:appkey/oauth-bindings/*", Permission: PermAppUserWrite, Scope: ScopeApp},
	{Methods: readMethods, Pattern: "/api/admin/apps/:appkey/oauth-providers/*", Permission: PermAppRead, Scope: ScopeApp},
	{Pattern: "/api/admin/apps/:appkey/oauth-providers/*", Permission: PermAppWrite, Scope: ScopeApp},

	{Methods: readMethods, Pattern: "/api/admin/apps/:appkey/users/*", Permission: PermAppUserRead, Scope: ScopeApp},
	{Pattern: "/api/admin/apps/:appkey/users/*", Permission: PermAppUserWrite, Scope: ScopeApp},
	// 渠道下的用户名单同样是用户数据（旧实现里靠 Contains "/users" 覆盖到）
	{Methods: readMethods, Pattern: "/api/admin/apps/:appkey/channels/:cid/users/*", Permission: PermAppUserRead, Scope: ScopeApp},
	{Pattern: "/api/admin/apps/:appkey/channels/:cid/users/*", Permission: PermAppUserWrite, Scope: ScopeApp},

	{Methods: readMethods, Pattern: "/api/admin/apps/:appkey/notifications/*", Permission: PermAppNotificationRead, Scope: ScopeApp},
	{Pattern: "/api/admin/apps/:appkey/notifications/*", Permission: PermAppNotifWrite, Scope: ScopeApp},
	{Methods: readMethods, Pattern: "/api/admin/apps/:appkey/banners/*", Permission: PermContentBannerRead, Scope: ScopeApp},
	{Pattern: "/api/admin/apps/:appkey/banners/*", Permission: PermContentBannerWrite, Scope: ScopeApp},
	{Methods: readMethods, Pattern: "/api/admin/apps/:appkey/notices/*", Permission: PermContentNoticeRead, Scope: ScopeApp},
	{Pattern: "/api/admin/apps/:appkey/notices/*", Permission: PermContentNoticeWrite, Scope: ScopeApp},

	// 应用自己的治理视图：看自己被怎么了、以及提交申诉。
	// 只读用 app:read、申诉用 app:write，都留在应用作用域 ——
	// 应用管理员在这里改不了治理结论，改结论要走 /api/admin/platform/*。
	{Methods: readMethods, Pattern: "/api/admin/apps/:appkey/governance/*", Permission: PermAppRead, Scope: ScopeApp},
	{Pattern: "/api/admin/apps/:appkey/governance/*", Permission: PermAppWrite, Scope: ScopeApp},

	{Methods: readMethods, Pattern: "/api/admin/apps/:appkey/*", Permission: PermAppRead, Scope: ScopeApp},
	{Pattern: "/api/admin/apps/:appkey/*", Permission: PermAppWrite, Scope: ScopeApp},
}
