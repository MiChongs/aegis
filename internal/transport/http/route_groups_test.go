package httptransport

import (
	"strings"
	"testing"

	"aegis/pkg/routetable"

	"golang.org/x/text/width"
)

// deriveTagsLegacy 是规则表之前那版 deriveTags 的**逐字拷贝**。
//
// 留着它不是为了兼容，而是为了让「把两张表合成一张」这件事可验证：
// OpenAPI 标签是对外规范的一部分（生成式客户端按它分包、门户按它反查分组），
// 收敛分组规则时哪怕只有一条路由的标签变了，下游都会跟着变，
// 而那种变化在代码评审里是看不出来的——它藏在几十条 prefix 的相对顺序里。
//
// 这个函数只应该被下面那条测试引用。规则表要新增分组时，
// 只要新分组的 Tag 与它所在命名空间原本的标签一致，这条测试就会继续绿。
func deriveTagsLegacy(path string) []string {
	switch {
	case path == "/healthz" || path == "/readyz":
		return []string{"System"}
	case path == "/api/ws":
		return []string{"Realtime"}
	case strings.HasPrefix(path, "/api/admin/auth/"):
		return []string{"Admin Auth"}
	case strings.HasPrefix(path, "/api/admin/system/"):
		return []string{"Admin System"}
	case strings.HasPrefix(path, "/api/admin/"):
		return []string{"Admin"}
	case strings.HasPrefix(path, "/api/auth/"):
		return []string{"Auth"}
	case strings.HasPrefix(path, "/api/user-settings"):
		return []string{"User Settings"}
	case strings.HasPrefix(path, "/api/user/"):
		return []string{"User"}
	case strings.HasPrefix(path, "/api/points"):
		return []string{"Points"}
	case strings.HasPrefix(path, "/api/notifications"):
		return []string{"Notifications"}
	case strings.HasPrefix(path, "/api/email"):
		return []string{"Email"}
	case strings.HasPrefix(path, "/api/public/pay"):
		return []string{"Public Payment"}
	case strings.HasPrefix(path, "/api/pay"):
		return []string{"Payment"}
	case strings.HasPrefix(path, "/api/storage"):
		return []string{"Storage"}
	case strings.HasPrefix(path, "/api/app/workflow"):
		return []string{"Workflow"}
	case strings.HasPrefix(path, "/api/app/"):
		return []string{"App Compat"}
	default:
		return []string{"API"}
	}
}

// TestDeriveTagsMatchesLegacySwitch 逐条比对规则表与旧 switch 的结论。
func TestDeriveTagsMatchesLegacySwitch(t *testing.T) {
	engine := newTestRouter(t)

	checked := 0
	for _, route := range engine.Routes() {
		// docs.go 喂给 deriveTags 的是 OpenAPI 规范化后的路径（`{appkey}` 而非 `:appkey`），
		// 所以这里也要按那个形状比，否则测的不是真实调用路径
		path := normalizeOpenAPIPath(route.Path)
		got := deriveTags(path)
		want := deriveTagsLegacy(path)
		if len(got) != 1 || len(want) != 1 || got[0] != want[0] {
			t.Errorf("%s %s 的 OpenAPI 标签变了：规则表给 %v，旧 switch 给 %v", route.Method, path, got, want)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("一条路由都没比到，说明测试本身失效了")
	}
	t.Logf("已比对 %d 条路由的 OpenAPI 标签", checked)
}

// TestEveryRouteMatchesAnExplicitGroup 禁止任何路由落进兜底分组。
//
// 兜底规则存在的意义是「显式地承认没分类」，而不是给新命名空间一个默认归宿。
// 新增一批路由却忘了往规则表里加一行时，这条测试会指名道姓地报出来——
// 否则它们会安静地聚在「未分组」里，而路由清单看上去一切正常。
func TestEveryRouteMatchesAnExplicitGroup(t *testing.T) {
	engine := newTestRouter(t)

	var orphans []string
	for _, route := range engine.Routes() {
		if matchRouteGroup(route.Path).Realm == realmUngrouped {
			orphans = append(orphans, route.Method+" "+route.Path)
		}
	}
	if len(orphans) > 0 {
		t.Errorf("以下 %d 条路由没有命中任何命名空间规则，请往 routeGroups 里补一行：\n  %s",
			len(orphans), strings.Join(orphans, "\n  "))
	}
}

// TestRouteInventoryCoversEveryRoute 清单必须不多不少地覆盖整张路由表。
func TestRouteInventoryCoversEveryRoute(t *testing.T) {
	engine := newTestRouter(t)
	inv := RouteInventory(engine)

	if got, want := inv.Total(), len(engine.Routes()); got != want {
		t.Errorf("清单共 %d 条，路由表共 %d 条", got, want)
	}

	// 逐条核对，防止「总数对上了但内容错位」
	registered := map[string]bool{}
	for _, route := range engine.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	for _, g := range inv.Groups {
		for _, r := range g.Routes {
			key := r.Method + " " + r.Path
			if !registered[key] {
				t.Errorf("清单里的 %s 在路由表里不存在", key)
			}
			delete(registered, key)
		}
	}
	for key := range registered {
		t.Errorf("路由 %s 没有出现在清单里", key)
	}
}

func TestRouteInventoryHandlesNilEngine(t *testing.T) {
	// 横幅在装配失败的路径上可能拿到 nil engine，这时候少一个分区，而不是 panic
	inv := RouteInventory(nil)
	if inv.Total() != 0 || len(inv.Groups) != 0 {
		t.Errorf("nil engine 应产出空清单，得到 %d 条 / %d 组", inv.Total(), len(inv.Groups))
	}
}

// TestRouteInventoryGroupsFollowRuleOrder 组序必须跟随规则表的声明顺序。
//
// gin 的 Routes() 顺序由前缀树形状决定，读起来是随机的；
// 而「公开 → 网关 → 管理端 → 应用兼容 → 用户端 → 系统」这个层次本身是信息。
func TestRouteInventoryGroupsFollowRuleOrder(t *testing.T) {
	inv := RouteInventory(newTestRouter(t))

	lastRank := -1
	for _, g := range inv.Groups {
		rank := -1
		for i, rule := range routeGroups {
			if rule.Realm == g.Realm && rule.Title == g.Title {
				rank = i
				break
			}
		}
		if rank < 0 {
			t.Fatalf("分组 %q/%q 在规则表里找不到对应规则", g.Realm, g.Title)
		}
		if rank < lastRank {
			t.Errorf("分组 %q/%q 的位置早于前一个分组，组序没有跟随规则表", g.Realm, g.Title)
		}
		lastRank = rank
	}

	// 顶层域的相对顺序也要稳定
	realms := []string{}
	seen := map[string]bool{}
	for _, r := range inv.Realms() {
		if !seen[r.Realm] {
			seen[r.Realm] = true
			realms = append(realms, r.Realm)
		}
	}
	want := []string{realmPublic, realmGateway, realmAdmin, realmAppCompat, realmUser, realmSystem}
	if len(realms) != len(want) {
		t.Fatalf("顶层域 = %v，期望 %v", realms, want)
	}
	for i := range want {
		if realms[i] != want[i] {
			t.Errorf("顶层域顺序 = %v，期望 %v", realms, want)
			break
		}
	}
}

// TestGroupPrefixActuallyPrefixesItsRoutes 公共前缀必须真的是公共前缀。
//
// 它是算出来的而不是手写的，正因为手写的会随路由增删悄悄失真；
// 但算法本身也可能算错（尤其是段边界的处理），所以拿真实路由表验一遍。
func TestGroupPrefixActuallyPrefixesItsRoutes(t *testing.T) {
	inv := RouteInventory(newTestRouter(t))
	for _, g := range inv.Groups {
		if g.Prefix == "" {
			continue
		}
		for _, r := range g.Routes {
			if !strings.HasPrefix(r.Path, g.Prefix) {
				t.Errorf("分组 %q 的公共前缀 %q 并不是 %q 的前缀", g.Title, g.Prefix, r.Path)
			}
		}
	}
}

func TestCommonPathPrefixStopsAtSegmentBoundary(t *testing.T) {
	// `/api/admin/apps` 与 `/api/admin/app/site` 的公共前缀是 `/api/admin`，
	// 不是 `/api/admin/app`——后者是把两个不同的段切了一半拼出来的假前缀
	got := commonPathPrefix([]routetable.Route{
		{Path: "/api/admin/apps"},
		{Path: "/api/admin/app/site"},
	})
	if got != "/api/admin" {
		t.Errorf("公共前缀 = %q，期望 /api/admin（必须按 / 分段取整）", got)
	}
}

func TestCommonPathPrefixSingleRouteHasNone(t *testing.T) {
	// 只有一条路由时前缀等于它自己，打出来是重复信息
	if got := commonPathPrefix([]routetable.Route{{Path: "/healthz"}}); got != "" {
		t.Errorf("单条路由的公共前缀应为空，得到 %q", got)
	}
}

func TestCanonicalRoutePath(t *testing.T) {
	cases := map[string]string{
		"/api/v1/apps/:appkey/config":   "/api/v1/apps/*/config",
		"/api/v1/apps/{appkey}/config":  "/api/v1/apps/*/config",
		"/api/storage/proxy/*path":      "/api/storage/proxy/*",
		"/healthz":                      "/healthz",
		"/api/admin/system/legal/:slug": "/api/admin/system/legal/*",
	}
	for input, want := range cases {
		if got := canonicalRoutePath(input); got != want {
			t.Errorf("canonicalRoutePath(%q) = %q，期望 %q", input, got, want)
		}
	}
}

// TestGatewayRoutesShareOneRealm 网关是接入方唯一需要认识的命名空间，
// 它的每一条路由都必须归在同一个顶层域下，否则清单会把「接入方要看的东西」打散。
func TestGatewayRoutesShareOneRealm(t *testing.T) {
	engine := newTestRouter(t)
	for _, route := range engine.Routes() {
		if !strings.HasPrefix(route.Path, "/api/v1/apps/") {
			continue
		}
		if realm := matchRouteGroup(route.Path).Realm; realm != realmGateway {
			t.Errorf("%s %s 归进了 %q，应属 %q", route.Method, route.Path, realm, realmGateway)
		}
	}
}

// TestPlatformGovernanceRoutesAreLabelledGlobal 平台治理的鉴权标注必须说清「全局」。
//
// 这不是文案洁癖：/api/admin/platform/* 恒按全局作用域鉴权是整套平台治理的地基
// （否则被冻结应用自己的管理员能给自己解封），而路由清单是运维排查时第一眼看的东西。
// 标注说得含糊，那道地基就不会被人注意到。
func TestPlatformGovernanceRoutesAreLabelledGlobal(t *testing.T) {
	engine := newTestRouter(t)
	found := 0
	for _, route := range engine.Routes() {
		if !strings.HasPrefix(route.Path, "/api/admin/platform") {
			continue
		}
		found++
		if auth := matchRouteGroup(route.Path).Auth; !strings.Contains(auth, "全局") {
			t.Errorf("%s %s 的鉴权标注 %q 没有点明全局作用域", route.Method, route.Path, auth)
		}
	}
	if found == 0 {
		t.Fatal("没扫到平台治理路由，测试失效")
	}
}

// TestRuleLabelsAvoidAmbiguousWidthRunes 是这个文件里最不像测试的一条，但它守着一个真实的坑。
//
// 路由清单靠计算字符宽度补空格对齐，而 go-runewidth 在中日韩控制台
// （GetConsoleOutputCP 命中 932/936/949/950）里会把 East Asian Ambiguous 宽度的字符
// 算成 2 列，Windows Terminal 这类按字体度量渲染的终端却只占 1 列。
// 于是「·」「×」「○」这类看着很适合做分隔符的字符一旦进了单元格，
// 整张表的右边框就会参差不齐——而且只在特定 locale 下发作，本机看着好好的。
//
// 判定交给 x/text/width（Unicode East Asian Width 属性的权威实现），
// 而不是维护一张「禁用字符表」：后者永远差一个字符。
func TestRuleLabelsAvoidAmbiguousWidthRunes(t *testing.T) {
	check := func(what, value string) {
		for _, r := range value {
			if width.LookupRune(r).Kind() == width.EastAsianAmbiguous {
				t.Errorf("%s %q 含 East Asian Ambiguous 宽度字符 %q (U+%04X)，会把路由表的边框顶歪；"+
					"分隔请用 ASCII 的 \" / \" 或 \" + \"", what, value, r, r)
			}
		}
	}
	for _, rule := range routeGroups {
		check("分组名", rule.Title)
		check("鉴权标注", rule.Auth)
		check("顶层域", rule.Realm)
	}
}

// TestRuleTableHasNoUnreachableRule 找出被前面的规则完全遮住、永远轮不到的规则。
//
// 一条永远匹配不到的规则是纯粹的误导：读代码的人以为某个前缀有专门的处理，
// 实际上它早在上面某一行就被吃掉了。
func TestRuleTableHasNoUnreachableRule(t *testing.T) {
	engine := newTestRouter(t)
	hit := make([]bool, len(routeGroups))
	for _, route := range engine.Routes() {
		canonical := canonicalRoutePath(route.Path)
		for i, rule := range routeGroups {
			if rule.matches(canonical) {
				hit[i] = true
				break
			}
		}
	}
	for i, used := range hit {
		// 打了 Fallback 的是命名空间兜底规则，允许暂时空着（理由见 routeGroup.Fallback）
		if !used && !routeGroups[i].Fallback {
			t.Errorf("规则 #%d（%s / %s，前缀 %q）没有匹配到任何路由："+
				"要么被上面的规则遮住了，要么它对应的路由已经不存在。"+
				"如果它是有意留的命名空间兜底，请标上 Fallback: true",
				i, routeGroups[i].Realm, routeGroups[i].Title, routeGroups[i].Prefix)
		}
	}
}
