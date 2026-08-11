// Package banner 渲染进程启动横幅。
//
// 分工：本包只负责「怎么画」——ASCII 艺术字、表格排版、着色与终端能力降级；
// 「画什么」由调用方（internal/bootstrap）从 config.Config 与运行期状态组装成
// Banner 值传进来。这样 pkg/ 不依赖任何业务配置结构，也能被 CLI 子命令复用。
//
// 全部交给依赖库，不自己造轮子：
//
//	github.com/common-nighthawk/go-figure  FIGlet 艺术字，内嵌 148 种字体，
//	                                       无需随二进制分发 .flf 文件
//	github.com/jedib0t/go-pretty/v6        表格排版与 ANSI 着色。它自带
//	                                       NO_COLOR / FORCE_COLOR / TERM=dumb 识别，
//	                                       Windows 下会主动开启 VT 处理，
//	                                       宽度计算走 go-runewidth（中文按 2 列算）
//	github.com/shirou/gopsutil/v4          主机 / CPU / 内存 / 磁盘事实采集
//	github.com/dustin/go-humanize          字节数与相对时间的人类可读化
//	github.com/mattn/go-isatty + x/term    TTY 判定与终端宽度
package banner

import (
	"fmt"
	"io"
	"os"
	"strings"

	figure "github.com/common-nighthawk/go-figure"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/jedib0t/go-pretty/v6/text"
)

const (
	// defaultFont 与项目此前手写的 ASCII 艺术字是同一种 FIGlet 字体，
	// 换成库生成后观感不变，但从此支持任意文案而不用手工画图
	defaultFont = "slant"
	// defaultWidth 探测不到终端宽度时的回退列数（docker logs / systemd / CI 都算这一档）
	defaultWidth = 100
	// minTableWidth 低于这个宽度表格会被挤成一团，直接降级为 compact
	minTableWidth = 72
	maxTableWidth = 160
	// indentWidth 表格与文本行的左缩进
	indentWidth = 2
)

// 分隔符分两套，因为「宽度算得准不准」在表格内外的后果完全不同。
//
// go-pretty 用 go-runewidth 算宽度，而后者在中日韩控制台（GetConsoleOutputCP
// 命中 932/936/949/950）里会把 East Asian Ambiguous 字符——「·」「•」「–」「○」
// 「▲」都在此列——算成 2 列。但 Windows Terminal 这类终端按字体度量渲染，
// 通常只占 1 列。表格靠计算宽度补空格，两边一旦不一致整张表就会错位。
//
// 所以单元格里只用两类字符：ASCII，或 East Asian Width 明确为 Wide/Narrow
// （不是 Ambiguous）的字符。U+2219 BULLET OPERATOR 就属于后者——
// 长得和「·」几乎一样，但两种 locale 下都稳定算 1 列。
// 表格外的自由文本不参与对齐，用回观感最好的「·」。
const (
	// Sep 表格单元格内的分隔符
	Sep = " ∙ "
	// SepText 自由文本行（艺术字副标题、摘要行、页脚）的分隔符
	SepText = "  ·  "
)

// Style 决定横幅的详略程度。
type Style string

const (
	// StyleAuto 按终端能力自动挑：交互式终端给全量，其余给不着色的全量
	StyleAuto Style = "auto"
	// StyleFull 艺术字 + 摘要行 + 明细表格 + 页脚
	StyleFull Style = "full"
	// StyleCompact 艺术字 + 摘要行，不打表格
	StyleCompact Style = "compact"
	// StyleMinimal 只打摘要行，适合日志采集侧
	StyleMinimal Style = "minimal"
	// StyleOff 完全不打印
	StyleOff Style = "off"
)

// ColorMode 决定是否着色。
type ColorMode string

const (
	// ColorAuto 交给 go-pretty 的环境判定，再叠加一层 TTY 判定
	ColorAuto ColorMode = "auto"
	// ColorAlways 强制着色（输出会被转成 HTML/日志高亮的场景）
	ColorAlways ColorMode = "always"
	// ColorNever 强制纯文本
	ColorNever ColorMode = "never"
)

// State 是一项事实的健康度，决定它的颜色与前缀符号。
type State uint8

const (
	StateNeutral State = iota // 普通事实，不着色
	StateOK                   // 已就绪 / 已启用
	StateWarn                 // 可用但需要留意（降级、未加固、用了默认值）
	StateError                // 不可用
	StateOff                  // 显式关闭
)

// Field 是表格里的一行：项 / 值 / 说明。
type Field struct {
	Key   string
	Value string
	Note  string
	State State
}

// Section 是表格里的一个分区，标题会在首列纵向合并成一格。
type Section struct {
	Title  string
	Fields []Field
}

// Banner 是一次横幅渲染的完整输入。
type Banner struct {
	// Logo 用于生成 FIGlet 艺术字，只取 ASCII 可打印字符
	Logo string
	// Tagline 紧跟在艺术字下方的一行副标题
	Tagline string
	// Highlights 摘要行，minimal 档下也会打印，因此把最关键的事实放这里
	Highlights []string
	// Title 表格标题
	Title string
	// Sections 表格分区
	Sections []Section
	// Footer 表格下方的收尾行（入口地址、提示）
	Footer []string
}

// Options 控制渲染行为，全部由调用方从配置注入。
type Options struct {
	Style  Style
	Font   string
	Color  ColorMode
	Width  int       // 0 = 自动探测终端宽度
	Writer io.Writer // nil = os.Stdout

	// 以下字段由 normalize 推导，调用方无需填写
	ascii bool // 非终端时用 ASCII 边框与符号，避免日志采集端乱码
	color bool
}

// defaultColorsEnabled 记录 go-pretty 在进程启动时的着色判定结果。
// 它已经识别了 NO_COLOR / FORCE_COLOR / TERM=dumb，并在 Windows 上尝试开启 VT 处理，
// 但只暴露了 Enable/DisableColors 两个写接口、没有读接口——
// 这里用一次着色试探把初值探出来，渲染结束后好还原回去。
var defaultColorsEnabled = text.Colors{text.FgRed}.Sprint("x") != "x"

// Print 渲染并输出横幅。渲染出错或被关闭时静默返回——
// 横幅永远不该成为进程起不来的理由。
func Print(b Banner, opt Options) {
	out := opt.Writer
	if out == nil {
		out = os.Stdout
	}
	if s := Render(b, opt); s != "" {
		_, _ = io.WriteString(out, s)
	}
}

// Render 渲染横幅为字符串。
func Render(b Banner, opt Options) string {
	opt = opt.normalize()
	if opt.Style == StyleOff {
		return ""
	}

	// 着色是 go-pretty 的进程级全局开关，成对设置避免污染其他使用者
	restore := applyColors(opt.color)
	defer restore()

	var sb strings.Builder
	sb.WriteString("\n")
	if opt.Style == StyleFull || opt.Style == StyleCompact {
		sb.WriteString(renderLogo(b, opt))
	}
	sb.WriteString(renderHighlights(b, opt))
	if opt.Style == StyleFull {
		sb.WriteString(renderSections(b, opt))
		sb.WriteString(renderFooter(b, opt))
	}
	sb.WriteString("\n")
	return sb.String()
}

// normalize 补齐默认值并推导终端能力。
func (o Options) normalize() Options {
	if o.Writer == nil {
		o.Writer = os.Stdout
	}
	if o.Font == "" {
		o.Font = defaultFont
	}
	if o.Style == "" {
		o.Style = StyleAuto
	}
	if o.Color == "" {
		o.Color = ColorAuto
	}

	tty := isTerminal(o.Writer)

	if o.Width <= 0 {
		o.Width = terminalWidth(o.Writer)
	}
	if o.Width <= 0 {
		o.Width = defaultWidth
	}
	if o.Width > maxTableWidth {
		o.Width = maxTableWidth
	}

	switch o.Color {
	case ColorAlways:
		o.color = true
	case ColorNever:
		o.color = false
	default:
		// go-pretty 已经判过环境变量，这里只补它没判的一条：输出不是终端就别写转义序列
		o.color = defaultColorsEnabled && tty
	}

	// 非交互式输出（docker logs / systemd journal / CI）改用纯 ASCII 边框与符号：
	// 信息一条不少，只是不赌采集链路认得 Unicode 制表符
	o.ascii = !tty

	if o.Style == StyleAuto {
		o.Style = StyleFull
	}
	// 窄终端里四列表格会被折成面条，退一档只留艺术字与摘要
	if o.Style == StyleFull && o.Width < minTableWidth {
		o.Style = StyleCompact
	}
	return o
}

func applyColors(enable bool) func() {
	if enable {
		text.EnableColors()
	} else {
		text.DisableColors()
	}
	return func() {
		if defaultColorsEnabled {
			text.EnableColors()
		} else {
			text.DisableColors()
		}
	}
}

// ── 艺术字 ─────────────────────────────────────────────────────────────

// logoPalette 是艺术字自上而下的渐变色（256 色青→蓝）。
// 终端不支持 256 色时 go-pretty 照常输出转义序列，绝大多数终端会退化成近似色；
// 完全不支持着色时整段转义会被 DisableColors 抹掉。
var logoPalette = []text.Color{
	text.Fg256Color(51),
	text.Fg256Color(45),
	text.Fg256Color(39),
	text.Fg256Color(38),
	text.Fg256Color(32),
	text.Fg256Color(26),
	text.Fg256Color(25),
}

func renderLogo(b Banner, opt Options) string {
	rows := figureRows(asciiOnly(b.Logo), opt.Font)
	if len(rows) == 0 {
		return ""
	}
	// 艺术字比终端还宽就整段丢掉：折行后的 FIGlet 完全不可读，不如不画
	pad := strings.Repeat(" ", indentWidth)
	for _, row := range rows {
		if text.StringWidthWithoutEscSequences(row)+indentWidth > opt.Width {
			return ""
		}
	}

	var sb strings.Builder
	for i, row := range rows {
		color := logoPalette[i*len(logoPalette)/len(rows)]
		sb.WriteString(pad)
		sb.WriteString(text.Colors{color, text.Bold}.Sprint(row))
		sb.WriteString("\n")
	}
	if b.Tagline != "" {
		sb.WriteString(pad)
		sb.WriteString(text.Colors{text.FgHiBlack}.Sprint(b.Tagline))
		sb.WriteString("\n")
	}
	return sb.String()
}

// figureRows 生成 FIGlet 行。
// go-figure 对未知字体直接 panic（newFont 里 Asset 找不到就炸），
// 而字体名来自配置，必须兜住并回退到默认字体。
func figureRows(phrase, font string) []string {
	if phrase == "" {
		return nil
	}
	rows := func() (rows []string) {
		defer func() {
			if recover() != nil {
				rows = nil
			}
		}()
		// strict=false：遇到字体缺字时替换成 '?'，而不是 log.Fatal 掉整个进程
		return figure.NewFigure(phrase, font, false).Slicify()
	}()
	if len(rows) == 0 && font != defaultFont {
		return figureRows(phrase, defaultFont)
	}
	return rows
}

// asciiOnly 过滤掉 FIGlet 字体覆盖不到的字符（中文、emoji 等）。
func asciiOnly(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if r >= ' ' && r <= '~' {
			sb.WriteRune(r)
		}
	}
	return strings.TrimSpace(sb.String())
}

// ── 摘要行 ─────────────────────────────────────────────────────────────

func renderHighlights(b Banner, opt Options) string {
	if len(b.Highlights) == 0 {
		return ""
	}
	var sb strings.Builder
	// 只有紧跟在艺术字后面时才需要空一行把两者隔开
	if opt.Style != StyleMinimal && b.Logo != "" {
		sb.WriteString("\n")
	}
	sb.WriteString(renderTextLines(b.Highlights, opt, text.Colors{text.FgHiWhite}))
	return sb.String()
}

func renderFooter(b Banner, opt Options) string {
	if len(b.Footer) == 0 {
		return ""
	}
	return renderTextLines(b.Footer, opt, text.Colors{text.FgHiBlack})
}

// renderTextLines 打印自由文本行：按终端宽度软折行、缩进、着色。
// 折行交给 go-pretty 的 text.WrapSoft——它按词边界折，且认识 CJK 宽度与
// 已有的 ANSI 转义序列，自己按 len() 切会把中文和颜色都切坏。
func renderTextLines(lines []string, opt Options, colors text.Colors) string {
	pad := strings.Repeat(" ", indentWidth)
	wrapAt := opt.Width - indentWidth
	var sb strings.Builder
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		for wrapped := range strings.SplitSeq(text.WrapSoft(line, wrapAt), "\n") {
			wrapped = strings.TrimRight(wrapped, " ")
			if wrapped == "" {
				continue
			}
			sb.WriteString(pad)
			sb.WriteString(colors.Sprint(wrapped))
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// ── 明细表格 ───────────────────────────────────────────────────────────

func renderSections(b Banner, opt Options) string {
	sections := make([]Section, 0, len(b.Sections))
	for _, s := range b.Sections {
		if len(s.Fields) > 0 {
			sections = append(sections, s)
		}
	}
	if len(sections) == 0 {
		return ""
	}

	t := table.NewWriter()
	t.SetStyle(tableStyle(opt))
	if b.Title != "" {
		t.SetTitle(b.Title)
	}
	t.AppendHeader(table.Row{"分区", "项", "值", "说明"})

	for i, s := range sections {
		for _, f := range s.Fields {
			t.AppendRow(table.Row{s.Title, f.Key, decorate(f, opt), f.Note})
		}
		if i < len(sections)-1 {
			t.AppendSeparator()
		}
	}

	t.SetColumnConfigs(columnConfigs(opt.Width-indentWidth, sections))
	// 某类横幅可能一条说明都没有（例如 worker 的精简版），此时整列自动消失
	t.SuppressEmptyColumns()
	t.SuppressTrailingSpaces()

	return "\n" + indent(t.Render(), strings.Repeat(" ", indentWidth)) + "\n"
}

func tableStyle(opt Options) table.Style {
	st := table.StyleRounded
	if opt.ascii {
		// 非交互式输出走纯 ASCII 边框，日志采集端不会因为制表符断字
		st = table.StyleDefault
	}
	st.Options.SeparateRows = false
	st.Options.DrawBorder = true
	// 默认会把表头转成大写，中文表头没意义，英文表头也没必要吼
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

// columnConfigs 按可用宽度分配四列。
//
// 前两列按实际内容量宽——写死上限会让「项」列白占十几列，剩下的宽度不够
// 值和说明用，表格明明没占满终端却在疯狂折行。剩余宽度再按 55/45 分给值与说明。
//
// 首列开启 AutoMerge：同一分区的多行纵向合并成一格，
// 这是 go-pretty 自带的能力，比自己在行里塞空字符串可靠得多。
func columnConfigs(width int, sections []Section) []table.ColumnConfig {
	// 边框 + 每列左右各一个内边距：5 条竖线 + 8 个空格
	const chrome = 13

	sectionWidth := text.StringWidthWithoutEscSequences("分区")
	keyWidth := text.StringWidthWithoutEscSequences("项")
	for _, s := range sections {
		sectionWidth = max(sectionWidth, text.StringWidthWithoutEscSequences(s.Title))
		for _, f := range s.Fields {
			keyWidth = max(keyWidth, text.StringWidthWithoutEscSequences(f.Key))
		}
	}
	// 单个超长的分区名/项名不该把值和说明挤没，各自封顶在四分之一表宽
	sectionWidth = min(sectionWidth, width/4)
	keyWidth = min(keyWidth, width/4)

	remaining := max(width-chrome-sectionWidth-keyWidth, 30)
	valueWidth := remaining * 55 / 100
	noteWidth := remaining - valueWidth

	return []table.ColumnConfig{
		{
			Number:    1,
			AutoMerge: true,
			Align:     text.AlignCenter,
			VAlign:    text.VAlignMiddle,
			Colors:    text.Colors{text.FgHiCyan, text.Bold},
			WidthMax:  sectionWidth,
		},
		{
			Number:   2,
			Align:    text.AlignLeft,
			Colors:   text.Colors{text.FgWhite},
			WidthMax: keyWidth,
		},
		{
			Number:   3,
			Align:    text.AlignLeft,
			WidthMax: valueWidth,
		},
		{
			Number:   4,
			Align:    text.AlignLeft,
			Colors:   text.Colors{text.FgHiBlack},
			WidthMax: noteWidth,
		},
	}
}

// decorate 给值加上状态符号与颜色。
// 着色在这里逐格做而不是走 ColumnConfig.Colors：同一列里每行的健康度并不相同。
func decorate(f Field, opt Options) string {
	value := f.Value
	if value == "" {
		value = "-"
	}
	if f.State == StateNeutral {
		return value
	}
	return f.State.colors().Sprint(f.State.glyph(opt.ascii) + " " + value)
}

// glyph 返回状态前缀符号。
//
// Unicode 档只挑 East Asian Width 为 Narrow/Neutral、且没有 emoji 变体的字符：
// 「●」「▲」「○」这些更好看的圆点方块都是 Ambiguous，会在中日韩控制台里被算成
// 2 列而实际渲染 1 列，把表格撑歪；带 emoji 变体的（⚠ 之类）则可能被终端
// 按彩色 emoji 渲染成双宽。宁可朴素也不要错位。
func (s State) glyph(ascii bool) string {
	if ascii {
		switch s {
		case StateOK:
			return "+"
		case StateWarn:
			return "!"
		case StateError:
			return "x"
		case StateOff:
			return "-"
		}
		return " "
	}
	switch s {
	case StateOK:
		return "✓"
	case StateWarn:
		return "!"
	case StateError:
		return "✗"
	case StateOff:
		return "-"
	}
	return " "
}

func (s State) colors() text.Colors {
	switch s {
	case StateOK:
		return text.Colors{text.FgHiGreen}
	case StateWarn:
		return text.Colors{text.FgHiYellow}
	case StateError:
		return text.Colors{text.FgHiRed, text.Bold}
	case StateOff:
		return text.Colors{text.FgHiBlack}
	}
	return nil
}

// ── 组装助手（给调用方用，免得每处都写一遍三元表达式）───────────────────

// Enabled 把布尔开关渲染成「已启用 / 已关闭」并带上对应状态色。
func Enabled(on bool, onText, offText string) (string, State) {
	if on {
		return onText, StateOK
	}
	return offText, StateOff
}

// Toggle 是 Enabled 的常用默认文案版本。
func Toggle(on bool) (string, State) { return Enabled(on, "已启用", "已关闭") }

// Fallback 在值为空时返回占位文案。
func Fallback(value, placeholder string) string {
	if strings.TrimSpace(value) == "" {
		return placeholder
	}
	return value
}

// Countf 生成「3 个端点 / 12 条规则」这类计数说明。
func Countf(format string, args ...any) string { return fmt.Sprintf(format, args...) }

// Join 用单元格分隔符拼接若干片段，自动跳过空串。
// 单元格里必须用它而不是自己写 strings.Join(" · ")，理由见 Sep 的注释。
func Join(parts ...string) string { return join(Sep, parts) }

// JoinText 用自由文本分隔符拼接若干片段，自动跳过空串。
func JoinText(parts ...string) string { return join(SepText, parts) }

func join(sep string, parts []string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, sep)
}

func indent(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n") + "\n"
}
