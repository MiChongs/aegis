package banner

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/jedib0t/go-pretty/v6/text"
)

func sampleBanner() Banner {
	return Banner{
		Logo:       "AEGIS",
		Tagline:    JoinText("多租户用户系统平台", "unified (API + Worker)", "development"),
		Highlights: []string{JoinText("版本 v1.4.0 (7ab26a5)", "go1.26.5 windows/amd64", "PID 12345")},
		Title:      "Aegis 运行时",
		Sections: []Section{
			{Title: "构建", Fields: []Field{
				{Key: "版本", Value: "v1.4.0", Note: "aegis"},
				{Key: "工具链", Value: Join("go1.26.5", "linux/amd64"), Note: Countf("依赖模块 %d", 312)},
			}},
			{Title: "数据面", Fields: []Field{
				{Key: "PostgreSQL", Value: "aegis@127.0.0.1:5432/aegis", Note: "连接 2/10（空闲 2）", State: StateOK},
				{Key: "遗留 MySQL", Value: "未配置", Note: "仅 sync-legacy-* 命令需要", State: StateOff},
				{Key: "NATS", Value: "nats://127.0.0.1:4222", Note: "流 AEGIS_EVENTS", State: StateWarn},
				{Key: "Temporal", Value: "127.0.0.1:7233", Note: Join("命名空间 default", "队列 aegis-workflow"), State: StateError},
			}},
		},
		Footer: []string{"停止  Ctrl+C 优雅关闭"},
	}
}

// TestRenderTableAlignment 钉住表格对齐。
//
// go-pretty 靠计算显示宽度补空格，一旦单元格里混入 East Asian Ambiguous 字符
// （「·」「•」「●」「▲」……），中日韩控制台会算 2 列而多数终端渲染 1 列，
// 整张表就会错位。这里在两种 locale 判定下都跑一遍：
// 只要横幅里出现了宽度有歧义的字符，两次里必然有一次对不齐。
func TestRenderTableAlignment(t *testing.T) {
	for _, eastAsian := range []bool{false, true} {
		restore := overrideEastAsianWidth(t, eastAsian)

		out := Render(sampleBanner(), Options{
			Writer: &bytes.Buffer{}, // 非 *os.File，走非终端分支
			Style:  StyleFull,
			Color:  ColorNever,
			Width:  110,
		})

		var widths []int
		var samples []string
		for line := range strings.SplitSeq(out, "\n") {
			trimmed := strings.TrimLeft(line, " ")
			if !strings.HasPrefix(trimmed, "|") && !strings.HasPrefix(trimmed, "+") {
				continue
			}
			widths = append(widths, text.StringWidthWithoutEscSequences(line))
			samples = append(samples, line)
		}
		if len(widths) < 5 {
			t.Fatalf("eastAsian=%v: 没有渲染出表格，只拿到 %d 行边框", eastAsian, len(widths))
		}
		for i, w := range widths {
			if w != widths[0] {
				t.Errorf("eastAsian=%v: 第 %d 行宽度 %d，期望 %d\n%s\n%s",
					eastAsian, i, w, widths[0], samples[0], samples[i])
			}
		}
		restore()
	}
}

// overrideEastAsianWidth 切换 go-pretty 的宽度判定，并返回还原函数。
// go-pretty 没有读接口，用一次已知的歧义字符探测当前值。
func overrideEastAsianWidth(t *testing.T, val bool) func() {
	t.Helper()
	previous := text.StringWidthWithoutEscSequences("·") == 2
	text.OverrideRuneWidthEastAsianWidth(val)
	return func() { text.OverrideRuneWidthEastAsianWidth(previous) }
}

// TestSeparatorsAreWidthStable 直接钉住分隔符与状态符号本身。
// 这些常量是最容易被「换个更好看的圆点」改坏的地方。
func TestSeparatorsAreWidthStable(t *testing.T) {
	candidates := map[string]string{"Sep": Sep}
	for _, s := range []State{StateOK, StateWarn, StateError, StateOff, StateNeutral} {
		candidates["glyph/unicode"+string(rune('0'+s))] = s.glyph(false)
		candidates["glyph/ascii"+string(rune('0'+s))] = s.glyph(true)
	}

	for name, value := range candidates {
		restore := overrideEastAsianWidth(t, false)
		west := text.StringWidthWithoutEscSequences(value)
		restore()

		restore = overrideEastAsianWidth(t, true)
		east := text.StringWidthWithoutEscSequences(value)
		restore()

		if west != east {
			t.Errorf("%s = %q 宽度有歧义（west=%d east=%d），会在中日韩控制台里把表格撑歪", name, value, west, east)
		}
	}
}

func TestRenderStyles(t *testing.T) {
	b := sampleBanner()

	if out := Render(b, Options{Writer: &bytes.Buffer{}, Style: StyleOff}); out != "" {
		t.Errorf("StyleOff 应当什么都不输出，实际得到 %q", out)
	}

	full := Render(b, Options{Writer: &bytes.Buffer{}, Style: StyleFull, Color: ColorNever, Width: 110})
	compact := Render(b, Options{Writer: &bytes.Buffer{}, Style: StyleCompact, Color: ColorNever, Width: 110})
	minimal := Render(b, Options{Writer: &bytes.Buffer{}, Style: StyleMinimal, Color: ColorNever, Width: 110})

	if !strings.Contains(full, "PostgreSQL") {
		t.Error("full 档应当包含分区表格")
	}
	if strings.Contains(compact, "PostgreSQL") {
		t.Error("compact 档不应当包含分区表格")
	}
	if !strings.Contains(compact, "___") {
		t.Error("compact 档应当包含艺术字")
	}
	if strings.Contains(minimal, "___") {
		t.Error("minimal 档不应当包含艺术字")
	}
	if !strings.Contains(minimal, "PID 12345") {
		t.Error("minimal 档应当保留摘要行")
	}
}

// TestNarrowTerminalDegradesToCompact 窄终端里四列表格会被折成面条，应自动退档。
func TestNarrowTerminalDegradesToCompact(t *testing.T) {
	out := Render(sampleBanner(), Options{Writer: &bytes.Buffer{}, Style: StyleFull, Color: ColorNever, Width: 40})
	if strings.Contains(out, "PostgreSQL") {
		t.Error("宽度不足时应当退回 compact，不再渲染表格")
	}
	if strings.Contains(out, "___") {
		t.Error("艺术字比终端还宽时应当整段丢掉，而不是折行")
	}
}

func TestColorModes(t *testing.T) {
	const esc = "\x1b["

	never := Render(sampleBanner(), Options{Writer: &bytes.Buffer{}, Style: StyleFull, Color: ColorNever, Width: 110})
	if strings.Contains(never, esc) {
		t.Error("ColorNever 不应当输出任何 ANSI 转义序列")
	}

	always := Render(sampleBanner(), Options{Writer: &bytes.Buffer{}, Style: StyleFull, Color: ColorAlways, Width: 110})
	if !strings.Contains(always, esc) {
		t.Error("ColorAlways 应当输出 ANSI 转义序列")
	}

	// 着色是 go-pretty 的进程级全局开关，渲染结束必须还原，否则会污染同进程其他使用者
	colorized := text.Colors{text.FgRed}.Sprint("x") != "x"
	if colorized != defaultColorsEnabled {
		t.Errorf("渲染后未还原全局着色开关：当前 %v，期望 %v", colorized, defaultColorsEnabled)
	}
}

// TestUnknownFontFallsBack go-figure 对未知字体直接 panic，而字体名来自配置。
func TestUnknownFontFallsBack(t *testing.T) {
	out := Render(sampleBanner(), Options{
		Writer: &bytes.Buffer{},
		Style:  StyleCompact,
		Color:  ColorNever,
		Width:  110,
		Font:   "这个字体不存在",
	})
	if !strings.Contains(out, "___") {
		t.Errorf("未知字体应当回退到默认字体，实际输出：\n%s", out)
	}
}

func TestPrintWritesToWriter(t *testing.T) {
	var buf bytes.Buffer
	Print(sampleBanner(), Options{Writer: &buf, Style: StyleMinimal, Color: ColorNever, Width: 110})
	if !strings.Contains(buf.String(), "PID 12345") {
		t.Errorf("Print 未写入目标 writer，实际内容：%q", buf.String())
	}
}

func TestJoinSkipsEmpty(t *testing.T) {
	if got, want := Join("a", "", "  ", "b"), "a"+Sep+"b"; got != want {
		t.Errorf("Join = %q，期望 %q", got, want)
	}
	if got := JoinText(); got != "" {
		t.Errorf("JoinText() = %q，期望空串", got)
	}
}

func TestDurationFormatting(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "-"},
		{-time.Second, "-"},
		{45 * time.Second, "45秒"},
		{90 * time.Second, "1分30秒"},
		{3*time.Hour + 20*time.Minute, "3小时20分"},
		{50 * time.Hour, "2天2小时"},
	}
	for _, c := range cases {
		if got := Duration(c.in); got != c.want {
			t.Errorf("Duration(%v) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

func TestBytesAndMemoryLimit(t *testing.T) {
	if got := Bytes(0); got != "0 B" {
		t.Errorf("Bytes(0) = %q", got)
	}
	if got := Bytes(1536); got != "1.5 KiB" {
		t.Errorf("Bytes(1536) = %q", got)
	}
	if got := MemoryLimit(0); got != "未设置" {
		t.Errorf("MemoryLimit(0) = %q", got)
	}
	if got := MemoryLimit(1 << 30); got != "1.0 GiB" {
		t.Errorf("MemoryLimit(1GiB) = %q", got)
	}
}

// TestVersionNormalization 未打 tag 的构建里 Main.Version 是一串伪版本，
// 和旁边的短提交完全重复，应当收敛成 dev；真实 tag 必须原样保留。
func TestVersionNormalization(t *testing.T) {
	cases := map[string]string{
		"":                                   "dev",
		"(devel)":                            "dev",
		"v0.0.0-20260329193906-7ab26a5c607a": "dev",
		"v0.0.0-20260329193906-7ab26a5c607a+dirty": "dev",
		"v1.4.1-0.20260329193906-7ab26a5c607a":     "dev",
		"v1.4.0":                                   "v1.4.0",
		"v2.0.0-rc.1":                              "v2.0.0-rc.1",
	}
	for in, want := range cases {
		if got := (BuildFacts{Version: in}).normalize().Version; got != want {
			t.Errorf("normalize(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestReadBuildFactsAlwaysUsable(t *testing.T) {
	f := ReadBuildFacts()
	if f.Version == "" {
		t.Error("版本号不应为空，至少要回退到 dev")
	}
	if f.Platform() == "/" {
		t.Error("平台信息不应为空")
	}
	if f.Release() == "" {
		t.Error("Release 不应为空")
	}
}
