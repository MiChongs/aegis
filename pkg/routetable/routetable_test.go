package routetable

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"aegis/pkg/banner"

	"github.com/jedib0t/go-pretty/v6/text"
)

func sampleInventory() Inventory {
	return Inventory{
		Title: "路由清单",
		Groups: []Group{
			{
				Realm: "网关", Title: "应用接入网关", Prefix: "/api/v1/apps/:appkey", Auth: "网关拆包",
				Routes: []Route{
					{Method: "POST", Path: "/api/v1/apps/:appkey/auth/login", Handler: "AppLogin", Chain: 14},
					{Method: "GET", Path: "/api/v1/apps/:appkey/config", Handler: "AppConfig", Chain: 14, Auth: "免包装"},
					{Method: "DELETE", Path: "/api/v1/apps/:appkey/me/sessions/:id", Handler: "AppRevokeSession", Chain: 16, Auth: "Bearer"},
				},
			},
			{
				Realm: "管理端", Title: "平台治理", Prefix: "/api/admin/platform", Auth: "超管/全局",
				Routes: []Route{
					{Method: "GET", Path: "/api/admin/platform/overview", Handler: "PlatformOverview", Chain: 16},
					{Method: "POST", Path: "/api/admin/platform/apps/:appkey/freeze", Handler: "PlatformFreezeApp", Chain: 16},
				},
			},
			{
				Realm: "管理端", Title: "组织架构", Prefix: "/api/admin/system/organizations", Auth: "管理员会话",
				Routes: []Route{
					{Method: "GET", Path: "/api/admin/system/organizations/:orgId/departments", Handler: "ListDepartments", Chain: 16},
				},
			},
		},
	}
}

func TestTotalAndRealms(t *testing.T) {
	inv := sampleInventory()
	if got := inv.Total(); got != 6 {
		t.Fatalf("Total() = %d，期望 6", got)
	}

	realms := inv.Realms()
	if len(realms) != 2 {
		t.Fatalf("Realms() 返回 %d 个域，期望 2（网关 / 管理端）", len(realms))
	}
	// 顺序必须保持 Groups 的声明顺序，不能按名字重排
	if realms[0].Realm != "网关" || realms[1].Realm != "管理端" {
		t.Errorf("Realms() 顺序 = %q / %q，期望 网关 / 管理端", realms[0].Realm, realms[1].Realm)
	}
	if realms[1].Routes != 3 || realms[1].Groups != 2 {
		t.Errorf("管理端域 = %d 条 / %d 组，期望 3 条 / 2 组", realms[1].Routes, realms[1].Groups)
	}
	if len(realms[1].Auth) != 2 {
		t.Errorf("管理端域鉴权标注 = %v，期望两种去重后的标注", realms[1].Auth)
	}
}

func TestRealmsFallsBackToTitleAndSkipsEmptyGroups(t *testing.T) {
	inv := Inventory{Groups: []Group{
		{Title: "没写 Realm 的组", Routes: []Route{{Method: "GET", Path: "/a"}}},
		{Realm: "空组", Title: "空组", Routes: nil},
	}}
	realms := inv.Realms()
	if len(realms) != 1 {
		t.Fatalf("Realms() = %d 个域，期望 1（空组不该出现）", len(realms))
	}
	if realms[0].Realm != "没写 Realm 的组" {
		t.Errorf("Realm 缺省应回落到 Title，得到 %q", realms[0].Realm)
	}
}

func TestFilterByPathMethodGroup(t *testing.T) {
	inv := sampleInventory()

	if got := inv.Filter(Query{Path: "PLATFORM"}).Total(); got != 2 {
		t.Errorf("按路径过滤（大小写不敏感）= %d 条，期望 2", got)
	}
	if got := inv.Filter(Query{Methods: []string{"GET"}}).Total(); got != 3 {
		t.Errorf("按方法过滤 = %d 条，期望 3", got)
	}
	if got := inv.Filter(Query{Group: "网关"}).Total(); got != 3 {
		t.Errorf("按分组名过滤 = %d 条，期望 3", got)
	}
	// Realm 也应该能命中：--group 管理端 是个很自然的用法
	if got := inv.Filter(Query{Group: "管理端"}).Total(); got != 3 {
		t.Errorf("按顶层域过滤 = %d 条，期望 3", got)
	}

	// 整组被过滤空时该组必须消失，否则渲染出来的「N 组」与看到的行数对不上
	filtered := inv.Filter(Query{Path: "/api/admin/platform"})
	if len(filtered.Groups) != 1 {
		t.Errorf("过滤后剩 %d 组，期望 1（被滤空的组必须消失）", len(filtered.Groups))
	}
}

func TestFilterByAuthFallsBackToGroupAuth(t *testing.T) {
	inv := sampleInventory()
	// /api/admin/platform 的两条路由自己没有 Auth 字段，鉴权来自所属组。
	// 回落判定漏掉的话，这类「按组鉴权」的路由会被 --auth 整片漏掉。
	got := inv.Filter(Query{Auth: "超管"}).Total()
	if got != 2 {
		t.Errorf("按鉴权过滤 = %d 条，期望 2（应回落到组的鉴权标注）", got)
	}
}

func TestFilterEmptyQueryReturnsEverything(t *testing.T) {
	inv := sampleInventory()
	if got := inv.Filter(Query{}).Total(); got != inv.Total() {
		t.Errorf("空查询改变了条数：%d != %d", got, inv.Total())
	}
}

func TestSortOrdersByPathThenMethodSemantics(t *testing.T) {
	inv := Inventory{Groups: []Group{{Title: "组", Routes: []Route{
		{Method: "DELETE", Path: "/a"},
		{Method: "GET", Path: "/a"},
		{Method: "POST", Path: "/a"},
		{Method: "GET", Path: "/b"},
	}}}}
	sorted := inv.Sort()
	got := []string{}
	for _, r := range sorted.Groups[0].Routes {
		got = append(got, r.Method+" "+r.Path)
	}
	want := []string{"GET /a", "POST /a", "DELETE /a", "GET /b"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("排序结果 = %v，期望 %v（同路径下按读→写→删的语义序，不是字典序）", got, want)
		}
	}
}

func TestSortDoesNotReorderGroups(t *testing.T) {
	// 组的声明顺序本身携带层次信息（公开 → 网关 → 管理端），排序不得打散它
	inv := sampleInventory()
	sorted := inv.Sort()
	for i := range inv.Groups {
		if sorted.Groups[i].Title != inv.Groups[i].Title {
			t.Fatalf("第 %d 组被重排：%q → %q", i, inv.Groups[i].Title, sorted.Groups[i].Title)
		}
	}
}

func TestSortDoesNotMutateInput(t *testing.T) {
	inv := Inventory{Groups: []Group{{Title: "组", Routes: []Route{
		{Method: "DELETE", Path: "/b"},
		{Method: "GET", Path: "/a"},
	}}}}
	_ = inv.Sort()
	if inv.Groups[0].Routes[0].Path != "/b" {
		t.Error("Sort() 修改了入参切片，调用方手里的清单被就地改掉了")
	}
}

func TestParseMethods(t *testing.T) {
	got := ParseMethods(" get , post ,, ")
	if len(got) != 2 || got[0] != "GET" || got[1] != "POST" {
		t.Errorf("ParseMethods = %v，期望 [GET POST]", got)
	}
	if len(ParseMethods("")) != 0 {
		t.Error("空输入应得到空白名单（表示不过滤）")
	}
}

// TestRenderAllFormats 保证六种形态都能产出内容且带上关键事实。
func TestRenderAllFormats(t *testing.T) {
	inv := sampleInventory().Sort()
	for _, format := range []Format{FormatTable, FormatTree, FormatMarkdown, FormatCSV, FormatHTML, FormatJSON} {
		out, err := Render(inv, Options{Format: format, Width: 160, Color: ColorNever, Writer: &bytes.Buffer{}})
		if err != nil {
			t.Fatalf("format=%s 渲染失败：%v", format, err)
		}
		if !strings.Contains(out, "/api/v1/apps/:appkey/config") {
			t.Errorf("format=%s 的输出里找不到路径，说明这一档漏掉了路由本体", format)
		}
		if strings.Contains(out, "\x1b[") {
			t.Errorf("format=%s 在 ColorNever 档下仍写出了 ANSI 转义序列", format)
		}
	}
}

func TestRenderJSONCarriesTotal(t *testing.T) {
	out, err := Render(sampleInventory(), Options{Format: FormatJSON})
	if err != nil {
		t.Fatalf("渲染失败：%v", err)
	}
	var payload struct {
		Total  int `json:"total"`
		Groups []struct {
			Title  string `json:"title"`
			Routes []struct {
				Method string `json:"method"`
				Path   string `json:"path"`
			} `json:"routes"`
		} `json:"groups"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("JSON 不可解析：%v", err)
	}
	if payload.Total != 6 {
		t.Errorf("total = %d，期望 6", payload.Total)
	}
	if len(payload.Groups) != 3 {
		t.Errorf("groups = %d，期望 3", len(payload.Groups))
	}
}

func TestRenderUnknownFormatErrors(t *testing.T) {
	if _, err := Render(sampleInventory(), Options{Format: "yaml"}); err == nil {
		t.Error("未知格式应当报错，而不是静默产出空输出")
	}
}

func TestRenderEmptyInventorySaysSo(t *testing.T) {
	out, err := Render(Inventory{}, Options{Format: FormatTable})
	if err != nil {
		t.Fatalf("渲染失败：%v", err)
	}
	if !strings.Contains(out, "没有匹配的路由") {
		t.Errorf("空清单应明确说明，得到 %q", out)
	}
}

// TestTableRowsAlignExactly 是这个包最重要的一条断言。
//
// 表格靠计算出的字符宽度补空格对齐，而 go-runewidth 在中日韩控制台里会把
// East Asian Ambiguous 字符（「·」「•」「–」「○」）算成 2 列，实际渲染却是 1 列。
// 一旦有这类字符混进单元格或分隔符，整张表右边框就会参差不齐——
// 而这种错位只在特定 locale 下出现，本机看着好好的。
// 逐行比对显示宽度能把它当场钉住。
func TestTableRowsAlignExactly(t *testing.T) {
	inv := sampleInventory().Sort()
	out, err := Render(inv, Options{Format: FormatTable, Width: 160, Color: ColorNever})
	if err != nil {
		t.Fatalf("渲染失败：%v", err)
	}

	var width = -1
	for line := range strings.SplitSeq(strings.Trim(out, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		got := text.StringWidthWithoutEscSequences(line)
		if width == -1 {
			width = got
			continue
		}
		if got != width {
			t.Fatalf("表格行宽不一致：%d != %d\n该行：%s\n（多半是 East Asian Ambiguous 宽度字符混进了单元格）", got, width, line)
		}
	}
	if width <= 0 {
		t.Fatal("渲染结果里没有可比对的行")
	}
}

// TestRenderRestoresGlobalColorSwitch 盯住 go-pretty 的进程级着色开关被成对还原。
// 漏掉还原会让同进程里其他使用者（启动横幅、CLI 的其他输出）跟着变成纯文本。
func TestRenderRestoresGlobalColorSwitch(t *testing.T) {
	before := banner.ColorsAvailable()
	if _, err := Render(sampleInventory(), Options{Format: FormatTable, Color: ColorAlways}); err != nil {
		t.Fatalf("渲染失败：%v", err)
	}
	after := text.Colors{text.FgRed}.Sprint("x") != "x"
	if after != before {
		t.Errorf("渲染后未还原全局着色开关：当前 %v，期望 %v", after, before)
	}
}

func TestRenderColorAlwaysEmitsColors(t *testing.T) {
	out, err := Render(sampleInventory(), Options{Format: FormatTable, Width: 160, Color: ColorAlways})
	if err != nil {
		t.Fatalf("渲染失败：%v", err)
	}
	if !strings.Contains(out, "\x1b[") {
		t.Error("ColorAlways 档下应当写出 ANSI 转义序列")
	}
}

// TestNarrowWidthDropsHandlerColumn 钉住窄终端里的自适应降级：
// 处理器是排障辅助信息，宽度不够时整列让位，而路径一列不能省。
func TestNarrowWidthDropsHandlerColumn(t *testing.T) {
	inv := sampleInventory().Sort()
	narrow, err := Render(inv, Options{Format: FormatTable, Width: 90, Color: ColorNever})
	if err != nil {
		t.Fatalf("渲染失败：%v", err)
	}
	if strings.Contains(narrow, "处理器") {
		t.Error("90 列下不该再摆「处理器」列")
	}
	if !strings.Contains(narrow, "/api/v1/apps/:appkey/me/sessions/:id") {
		t.Error("路径不能因为窄就被折断——这张表的主要用途是把路径整段复制走")
	}

	wide, err := Render(inv, Options{Format: FormatTable, Width: 160, Color: ColorNever})
	if err != nil {
		t.Fatalf("渲染失败：%v", err)
	}
	if !strings.Contains(wide, "处理器") {
		t.Error("160 列下应当摆出「处理器」列")
	}
}

// TestAuthLivesInGroupCellForTerminal 鉴权在终端形态里进分组单元格，
// 因为它是分组的属性——同组每行印一遍是纯重复，占掉的正是路径需要的宽度。
func TestAuthLivesInGroupCellForTerminal(t *testing.T) {
	inv := sampleInventory().Sort()
	out, err := Render(inv, Options{Format: FormatTable, Width: 160, Color: ColorNever})
	if err != nil {
		t.Fatalf("渲染失败：%v", err)
	}
	if strings.Contains(out, "| 鉴权") {
		t.Error("终端形态不该有独立的鉴权列")
	}
	// 但鉴权本身必须还在——只是换了位置，不是丢了
	if !strings.Contains(out, "网关拆包") {
		t.Error("分组单元格里缺少鉴权标注")
	}
	// 每组只出现一次（AutoMerge 合并了首列）
	if n := strings.Count(out, "超管/全局"); n != 1 {
		t.Errorf("鉴权标注出现 %d 次，应当每组只出现 1 次", n)
	}
}

// TestExportFormatsFlattenAuth 导出形态反过来要平铺：
// 合并单元格进了 CSV 就没了，消费方按行读，缺这一列就得自己猜要不要带令牌。
func TestExportFormatsFlattenAuth(t *testing.T) {
	inv := sampleInventory().Sort()
	for _, format := range []Format{FormatMarkdown, FormatCSV, FormatHTML} {
		out, err := Render(inv, Options{Format: format, Color: ColorNever})
		if err != nil {
			t.Fatalf("format=%s 渲染失败：%v", format, err)
		}
		if !strings.Contains(out, "鉴权") {
			t.Errorf("format=%s 缺少鉴权列", format)
		}
		// 平铺意味着每一行都带上，而不是每组一次
		if n := strings.Count(out, "超管/全局"); n != 2 {
			t.Errorf("format=%s 的鉴权出现 %d 次，该组有 2 条路由，平铺应为 2 次", format, n)
		}
	}
}

// TestChainColumnDisappearsWhenUnknown 覆盖 release 档：
// gin 只在 debug 档回调 DebugPrintRouteFunc，链深度全为 0 时那一列应自动消失。
func TestChainColumnDisappearsWhenUnknown(t *testing.T) {
	inv := sampleInventory()
	for gi := range inv.Groups {
		for ri := range inv.Groups[gi].Routes {
			inv.Groups[gi].Routes[ri].Chain = 0
		}
	}
	out, err := Render(inv.Sort(), Options{Format: FormatTable, Width: 160, Color: ColorNever})
	if err != nil {
		t.Fatalf("渲染失败：%v", err)
	}
	if strings.Contains(out, "链") {
		t.Error("链深度整表未知时，该列应由 SuppressEmptyColumns 抹掉")
	}
}

func TestPrintWritesToWriter(t *testing.T) {
	var buf bytes.Buffer
	if err := Print(sampleInventory().Sort(), Options{Format: FormatTree, Width: 120, Color: ColorNever, Writer: &buf}); err != nil {
		t.Fatalf("Print 失败：%v", err)
	}
	if !strings.Contains(buf.String(), "应用接入网关") {
		t.Error("Print 没有把内容写进传入的 Writer")
	}
}

// TestTreeOmitsRedundantAuthNote 覆盖树形里的一条刻意取舍：
// 与所属组相同的鉴权标注不重复打，否则真正的例外会被淹没。
func TestTreeOmitsRedundantAuthNote(t *testing.T) {
	inv := Inventory{Groups: []Group{{
		Title: "组", Auth: "需管理员会话",
		Routes: []Route{
			{Method: "GET", Path: "/same", Auth: "需管理员会话"},
			{Method: "GET", Path: "/exception", Auth: "匿名可读"},
		},
	}}}
	out, err := Render(inv, Options{Format: FormatTree, Width: 120, Color: ColorNever})
	if err != nil {
		t.Fatalf("渲染失败：%v", err)
	}
	for line := range strings.SplitSeq(out, "\n") {
		if strings.Contains(line, "/same") && strings.Contains(line, "需管理员会话") {
			t.Error("与组一致的鉴权标注被重复打在了路由行上")
		}
	}
	if !strings.Contains(out, "匿名可读") {
		t.Error("与组不同的鉴权标注必须标出来")
	}
}
