package routetable

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"aegis/pkg/banner"

	"github.com/jedib0t/go-pretty/v6/list"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

// Format 是输出形态。
type Format string

const (
	// FormatTable 分组表格，终端里读的默认形态。
	FormatTable Format = "table"
	// FormatTree 树形，按命名空间层级缩进，窄终端里比表格好读。
	FormatTree Format = "tree"
	// FormatMarkdown Markdown 表格，粘进 issue / 文档用。
	FormatMarkdown Format = "markdown"
	// FormatCSV 逗号分隔，导进表格软件做接口盘点用。
	FormatCSV Format = "csv"
	// FormatHTML HTML 表格。
	FormatHTML Format = "html"
	// FormatJSON 机器可读，给脚本与 CI 用。
	FormatJSON Format = "json"
)

// ColorMode 复用 pkg/banner 的着色档位定义，避免同一个三值枚举在两个包里各写一份。
type ColorMode = banner.ColorMode

const (
	ColorAuto   = banner.ColorAuto
	ColorAlways = banner.ColorAlways
	ColorNever  = banner.ColorNever
)

const (
	// defaultWidth 探测不到终端宽度时的回退列数
	defaultWidth = 100
	// handlerColumnWidth 低于这个宽度就整列不摆「处理器」。
	//
	// 不是压窄，是不摆：列宽有下限（floorHandler），压到下限之后再窄下去
	// 整张表就会比终端还宽、被终端硬折，那比少一列难读得多。
	// 处理器是排障辅助信息，路径不是，所以让它先走。
	handlerColumnWidth = 100
	maxWidth           = 220
	indentWidth        = 2
)

// Options 控制渲染行为。
type Options struct {
	Format Format
	Color  ColorMode
	Width  int       // 0 = 自动探测终端宽度
	Writer io.Writer // nil = os.Stdout

	// ShowHandler / ShowChain 允许显式关掉两列辅助信息。
	// 零值（false）不代表关闭——由 normalize 按宽度自适应决定，
	// 想强制关掉用 Hide* 字段。
	HideHandler bool
	HideChain   bool

	// 以下字段由 normalize 推导
	ascii       bool
	color       bool
	showHandler bool
}

func (o Options) normalize() Options {
	if o.Writer == nil {
		o.Writer = os.Stdout
	}
	if o.Format == "" {
		o.Format = FormatTable
	}
	if o.Color == "" {
		o.Color = ColorAuto
	}

	tty := banner.IsTerminal(o.Writer)
	if o.Width <= 0 {
		o.Width = banner.TerminalWidth(o.Writer)
	}
	if o.Width <= 0 {
		o.Width = defaultWidth
	}
	if o.Width > maxWidth {
		o.Width = maxWidth
	}

	switch o.Color {
	case ColorAlways:
		o.color = true
	case ColorNever:
		o.color = false
	default:
		o.color = banner.ColorsAvailable() && tty
	}
	// 导出形态一律纯文本：Markdown / CSV / HTML / JSON 里混进 ANSI 转义序列，
	// 消费方看到的是乱码而不是颜色。
	switch o.Format {
	case FormatMarkdown, FormatCSV, FormatHTML, FormatJSON:
		o.color = false
	}

	o.ascii = !tty
	o.showHandler = o.Width >= handlerColumnWidth && !o.HideHandler
	// 导出形态不受终端宽度约束，该给的列全给
	switch o.Format {
	case FormatMarkdown, FormatCSV, FormatHTML:
		o.showHandler = !o.HideHandler
	}
	return o
}

// Print 渲染并写出清单。
func Print(inv Inventory, opt Options) error {
	out := opt.Writer
	if out == nil {
		out = os.Stdout
	}
	rendered, err := Render(inv, opt)
	if err != nil {
		return err
	}
	_, err = io.WriteString(out, rendered)
	return err
}

// Render 把清单渲染成字符串。
func Render(inv Inventory, opt Options) (string, error) {
	opt = opt.normalize()

	if opt.Format == FormatJSON {
		body, err := inv.MarshalJSONIndent()
		if err != nil {
			return "", err
		}
		return string(body), nil
	}

	if inv.Total() == 0 {
		return "\n  没有匹配的路由。\n\n", nil
	}

	// 着色是 go-pretty 的进程级全局开关，成对设置避免污染其他使用者
	restore := banner.ApplyColors(opt.color)
	defer restore()

	switch opt.Format {
	case FormatTree:
		return renderTree(inv, opt), nil
	case FormatMarkdown:
		return buildTable(inv, opt).RenderMarkdown() + "\n", nil
	case FormatCSV:
		return buildTable(inv, opt).RenderCSV() + "\n", nil
	case FormatHTML:
		return buildTable(inv, opt).RenderHTML() + "\n", nil
	case FormatTable:
		return "\n" + indent(buildTable(inv, opt).Render(), strings.Repeat(" ", indentWidth)) + "\n", nil
	default:
		return "", fmt.Errorf("未知的输出格式 %q（可选 table / tree / markdown / csv / html / json）", opt.Format)
	}
}

// ── 表格 ───────────────────────────────────────────────────────────────

// 列种类。SetColumnConfigs 是按**列号**定位的，而列号会随「处理器」这一列
// 的有无而整体移位——写死 colChain = 5 在窄终端里会把配置挂到鉴权列上去。
// 所以这里只定义种类，列号由 tableColumns 按实际顺序现算。
type columnKind int

const (
	colGroup columnKind = iota
	colMethod
	colPath
	colHandler
	colChain
	colAuth
)

var columnHeaders = map[columnKind]string{
	colGroup:   "分组",
	colMethod:  "方法",
	colPath:    "路径",
	colHandler: "处理器",
	colChain:   "链",
	colAuth:    "鉴权",
}

// tableColumns 返回本次渲染实际摆出的列，顺序即列序。
// buildTable 拼表头/表体与 columnConfigs 配列宽都读它，两边因此不可能错位。
func tableColumns(opt Options, flatAuth bool) []columnKind {
	cols := []columnKind{colGroup, colMethod, colPath}
	if opt.showHandler {
		cols = append(cols, colHandler)
	}
	cols = append(cols, colChain)
	if flatAuth {
		cols = append(cols, colAuth)
	}
	return cols
}

// buildTable 排出表格。
//
// 终端形态与导出形态的列不同，是刻意的：
//
//	终端   分组（名称/条数/鉴权 三行合并成一格） 方法 路径 处理器 链
//	导出   分组 方法 路径 处理器 链 鉴权（逐行平铺）
//
// 鉴权在终端里进分组单元格，因为它是**分组**的属性——同一组里每行印一遍
// 「登录注册公开 / 其余管理员会话」是纯重复，而占掉的正是路径需要的宽度。
// 导出形态（Markdown / CSV / HTML）反过来要平铺：合并单元格进了 CSV 就没了，
// 消费方按行读，缺了这一列就得自己去猜某条路由要不要带令牌。
func buildTable(inv Inventory, opt Options) table.Writer {
	t := table.NewWriter()
	t.SetStyle(tableStyle(opt))
	t.SetTitle(tableTitle(inv))

	flat := opt.flatAuth()
	columns := tableColumns(opt, flat)

	header := make(table.Row, 0, len(columns))
	for _, col := range columns {
		header = append(header, columnHeaders[col])
	}
	t.AppendHeader(header)

	for i, g := range inv.Groups {
		cell := groupCell(g, flat)
		for _, r := range g.Routes {
			row := make(table.Row, 0, len(columns))
			for _, col := range columns {
				switch col {
				case colGroup:
					row = append(row, cell)
				case colMethod:
					row = append(row, methodCell(r.Method, opt))
				case colPath:
					row = append(row, r.Path)
				case colHandler:
					row = append(row, r.Handler)
				case colChain:
					row = append(row, chainCell(r.Chain))
				case colAuth:
					row = append(row, firstNonEmpty(r.Auth, g.Auth))
				}
			}
			t.AppendRow(row)
		}
		if i < len(inv.Groups)-1 {
			t.AppendSeparator()
		}
	}

	t.SetColumnConfigs(columnConfigs(opt, inv, columns, flat))
	// 链深度只有 debug 档采集得到，处理器名也可能整表为空——
	// 交给 go-pretty 自己把空列抹掉，比在这里写一堆 if 判断该不该加列可靠
	t.SuppressEmptyColumns()
	t.SuppressTrailingSpaces()
	return t
}

// flatAuth 报告是否把鉴权平铺成独立一列（导出形态）而不是并进分组单元格。
func (o Options) flatAuth() bool {
	switch o.Format {
	case FormatMarkdown, FormatCSV, FormatHTML:
		return true
	default:
		return false
	}
}

func tableTitle(inv Inventory) string {
	title := inv.Title
	if title == "" {
		title = "路由清单"
	}
	return fmt.Sprintf("%s%s%d 条%s%d 组", title, banner.Sep, inv.Total(), banner.Sep, len(inv.Groups))
}

// groupCell 是首列的内容。首列开启 AutoMerge，同组的多行纵向合并成这一格，
// 因此这里写的东西每组只出现一次——正好用来放「对整组成立」的事实。
//
// 分三行（组名 / 条数 / 鉴权）而不是挤成一行：这一格是整组的节标题，
// 它紧贴在分组分隔线下方，三行读起来是个信息块，挤成一行则会把首列撑宽，
// 而首列每宽一列，路径列就窄一列。
//
// 刻意不放公共前缀：路径列里每一行都完整带着它，再列一遍纯属重复。
// 前缀只在树形里出现，那里它是组标题，起的是「以下都挂在这个前缀下」的作用。
func groupCell(g Group, flatAuth bool) string {
	lines := []string{g.Title, strconv.Itoa(len(g.Routes)) + " 条"}
	// 平铺形态下鉴权自己占一列，这里再放一遍就是重复
	if !flatAuth && g.Auth != "" {
		lines = append(lines, g.Auth)
	}
	return strings.Join(lines, "\n")
}

func chainCell(chain int) string {
	if chain <= 0 {
		return ""
	}
	return strconv.Itoa(chain)
}

// methodColors 按 HTTP 语义着色：读绿、写青、改黄紫、删红、元信息灰。
// 一眼扫过去能分辨「这一组里有几个删除接口」，这是分组表格最实际的用处。
var methodColors = map[string]text.Colors{
	"GET":     {text.FgHiGreen},
	"HEAD":    {text.FgGreen},
	"POST":    {text.FgHiCyan},
	"PUT":     {text.FgHiYellow},
	"PATCH":   {text.FgHiMagenta},
	"DELETE":  {text.FgHiRed, text.Bold},
	"OPTIONS": {text.FgHiBlack},
}

// methodCell 着色在这里逐格做而不是走 ColumnConfig.Colors：同一列里每行的方法不同。
func methodCell(method string, opt Options) string {
	method = strings.ToUpper(strings.TrimSpace(method))
	if !opt.color {
		return method
	}
	if colors, ok := methodColors[method]; ok {
		return colors.Sprint(method)
	}
	return method
}

func tableStyle(opt Options) table.Style {
	st := table.StyleRounded
	if opt.ascii {
		// 非交互式输出走纯 ASCII 边框，日志采集端不会因为制表符断字
		st = table.StyleDefault
	}
	st.Options.SeparateRows = false
	st.Options.DrawBorder = true
	st.Format.Header = text.FormatDefault
	st.Format.Footer = text.FormatDefault
	st.Title.Align = text.AlignLeft
	st.Title.Format = text.FormatDefault
	st.Title.Colors = text.Colors{text.FgHiCyan, text.Bold}
	st.Color.Header = text.Colors{text.FgHiWhite, text.Bold}
	st.Color.Border = text.Colors{text.FgHiBlack}
	st.Color.Separator = text.Colors{text.FgHiBlack}
	return st
}

// 方法列按最长的方法名 OPTIONS 定宽；链深度不会超过三位数。
const (
	methodWidth = 7
	chainWidth  = 4
)

// 列宽下限。压到下限之后就只能折行了，所以这几个数的含义是
// 「折行也要保住这么宽」，而不是「理想宽度」。
const (
	floorGroup   = 14
	floorPath    = 28
	floorHandler = 12
	floorAuth    = 10
)

// 有界列的展示上限。
//
// 分组名、处理器名、鉴权标注的长度都天然有界（人取的名字不会无限长），
// 给上限只为防住个别超长值——一条 `PlatformSetRestrictionsForApplication`
// 不该让其余九百条路由的路径都跟着折行。
const (
	capGroup   = 24
	capHandler = 28
	capAuth    = 30
)

// columnConfigs 按可用宽度分配各列。
//
// 判据是**这一列的内容长度有没有上界**，而不是它有多重要：
//
//	有界列（分组 / 方法 / 处理器 / 链 / 鉴权）  各给自然宽度，另设一个上限防极端值
//	无界列（路径）                              吸收剩下的全部宽度
//
// 路径是这里唯一没有上界的东西，`/api/admin/system/organizations/:orgId/
// departments/:deptId/members` 这种长度在管理端很常见。
//
// 走到这一版之前踩了两次：
//
//	按「每列不超过表宽 1/4」平均分 → 150 列终端里处理器与鉴权各占 37 列、
//	  路径只剩 24 列，几乎每行都折。列宽都在上限内，表却是废的。
//	改成「路径优先，其余按优先级让宽」→ 路径按**单条最长路径**要走 93 列，
//	  为那一条把其余九百行的处理器都压到折行。
//
// 教训是：给某一列它的「自然宽度」只有在这个宽度对多数行都成立时才对。
// 路径的自然宽度由最长的那一条决定，不代表典型值，所以它该拿余量而不是拿自然宽度。
func columnConfigs(opt Options, inv Inventory, columns []columnKind, flatAuth bool) []table.ColumnConfig {
	width := opt.Width - indentWidth
	// 边框 + 每列左右各一个内边距
	chrome := (len(columns) + 1) + len(columns)*2

	// 自然宽度：这一列装下全部内容需要多少
	natural := map[columnKind]int{}
	for _, col := range columns {
		natural[col] = text.StringWidthWithoutEscSequences(columnHeaders[col])
	}
	natural[colMethod] = methodWidth
	natural[colChain] = chainWidth
	for _, g := range inv.Groups {
		for line := range strings.SplitSeq(groupCell(g, flatAuth), "\n") {
			natural[colGroup] = max(natural[colGroup], text.StringWidthWithoutEscSequences(line))
		}
		if flatAuth {
			natural[colAuth] = max(natural[colAuth], text.StringWidthWithoutEscSequences(g.Auth))
		}
		for _, r := range g.Routes {
			natural[colPath] = max(natural[colPath], text.StringWidthWithoutEscSequences(r.Path))
			if opt.showHandler {
				natural[colHandler] = max(natural[colHandler], text.StringWidthWithoutEscSequences(r.Handler))
			}
			if flatAuth {
				natural[colAuth] = max(natural[colAuth], text.StringWidthWithoutEscSequences(r.Auth))
			}
		}
	}

	// 有界列先各自封顶
	natural[colGroup] = min(natural[colGroup], capGroup)
	natural[colHandler] = min(natural[colHandler], capHandler)
	natural[colAuth] = min(natural[colAuth], capAuth)

	// 路径吸收余量：给它剩下的全部，但不超过它自己真正需要的宽度
	// （给多了不会更好看，只是让 go-pretty 的上限失去意义）
	bounded := chrome
	for _, col := range columns {
		if col != colPath {
			bounded += natural[col]
		}
	}
	natural[colPath] = max(floorPath, min(natural[colPath], width-bounded))

	// 极窄终端：连有界列加路径下限都塞不下，这时才从最不重要的一列开始压
	floors := map[columnKind]int{
		colGroup: floorGroup, colHandler: floorHandler, colAuth: floorAuth,
	}
	total := bounded + natural[colPath]
	if deficit := total - width; deficit > 0 {
		for _, col := range []columnKind{colHandler, colAuth, colGroup} {
			if deficit <= 0 {
				break
			}
			room := natural[col] - floors[col]
			if room <= 0 {
				continue
			}
			give := min(deficit, room)
			natural[col] -= give
			deficit -= give
		}
	}

	configs := make([]table.ColumnConfig, 0, len(columns))
	for i, col := range columns {
		cfg := table.ColumnConfig{Number: i + 1, Align: text.AlignLeft, WidthMax: natural[col]}
		switch col {
		case colGroup:
			cfg.AutoMerge = true
			// 顶对齐是被 AppendSeparator 决定的，不是随手挑的：
			// go-pretty 只在合并块之间没有分隔线时才会把标签纵向居中，
			// 一旦逐组画横线，标签就固定落在该组第一行。
			// 这里选择保留横线——几十个分组、上千行，没有横线会糊成一片，
			// 而紧贴分隔线的信息块本身就读作节标题，比居中更清楚。
			cfg.VAlign = text.VAlignTop
			cfg.Colors = text.Colors{text.FgHiCyan, text.Bold}
		case colHandler:
			cfg.Colors = text.Colors{text.FgWhite}
		case colChain:
			cfg.Align = text.AlignRight
			cfg.Colors = text.Colors{text.FgHiBlack}
		case colAuth:
			cfg.Colors = text.Colors{text.FgHiBlack}
		}
		configs = append(configs, cfg)
	}
	return configs
}

// ── 树形 ───────────────────────────────────────────────────────────────

// renderTree 用 go-pretty 的 list 排版。
// 窄终端里表格会被挤成面条，而树形只依赖缩进，宽度不够时最多折路径这一列。
func renderTree(inv Inventory, opt Options) string {
	l := list.NewWriter()
	if opt.ascii {
		l.SetStyle(list.StyleDefault)
	} else {
		l.SetStyle(list.StyleConnectedRounded)
	}

	l.AppendItem(text.Colors{text.FgHiCyan, text.Bold}.Sprint(tableTitle(inv)))
	l.Indent()
	for _, g := range inv.Groups {
		l.AppendItem(treeGroupLine(g, opt))
		l.Indent()
		for _, r := range g.Routes {
			l.AppendItem(treeRouteLine(r, g, opt))
		}
		l.UnIndent()
	}
	l.UnIndent()

	return "\n" + indent(l.Render(), strings.Repeat(" ", indentWidth)) + "\n"
}

func treeGroupLine(g Group, opt Options) string {
	parts := []string{g.Title}
	if g.Prefix != "" {
		parts = append(parts, g.Prefix)
	}
	parts = append(parts, strconv.Itoa(len(g.Routes))+" 条")
	if g.Auth != "" {
		parts = append(parts, g.Auth)
	}
	line := banner.Join(parts...)
	if !opt.color {
		return line
	}
	return text.Colors{text.FgHiWhite, text.Bold}.Sprint(line)
}

func treeRouteLine(r Route, g Group, opt Options) string {
	// 方法在树形里靠手工补空格对齐：list 每一项就是一个字符串，
	// 没有列的概念，go-pretty 帮不上忙。OPTIONS 是最长的方法名，补到 7 列。
	method := methodCell(r.Method, opt)
	pad := 7 - text.StringWidthWithoutEscSequences(method)
	if pad > 0 {
		method += strings.Repeat(" ", pad)
	}
	line := method + " " + r.Path

	notes := []string{}
	if r.Handler != "" && !opt.HideHandler {
		notes = append(notes, r.Handler)
	}
	if r.Chain > 0 && !opt.HideChain {
		notes = append(notes, "链 "+strconv.Itoa(r.Chain))
	}
	// 与组一致的鉴权不重复标，只标不一样的那几条——
	// 每行都跟着一句「需管理员会话」时，真正的例外反而看不见了
	if auth := r.Auth; auth != "" && auth != g.Auth {
		notes = append(notes, auth)
	}
	if len(notes) == 0 {
		return line
	}
	note := banner.Sep + banner.Join(notes...)
	if opt.color {
		note = text.Colors{text.FgHiBlack}.Sprint(note)
	}
	return line + note
}

func indent(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n") + "\n"
}
