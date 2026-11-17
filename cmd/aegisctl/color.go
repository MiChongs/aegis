package main

import (
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

// Palette 终端着色：NO_COLOR、--no-color、非终端输出时全部关闭
type Palette struct{ On bool }

// NewPalette 按输出目标决定是否着色
func NewPalette(w io.Writer, disabled bool) Palette {
	if disabled || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return Palette{}
	}
	if f, ok := w.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		return Palette{On: true}
	}
	return Palette{}
}

func (p Palette) wrap(code, s string) string {
	if !p.On {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (p Palette) Red(s string) string    { return p.wrap("31", s) }
func (p Palette) Green(s string) string  { return p.wrap("32", s) }
func (p Palette) Yellow(s string) string { return p.wrap("33", s) }
func (p Palette) Bold(s string) string   { return p.wrap("1", s) }
func (p Palette) Dim(s string) string    { return p.wrap("2", s) }

// Level 按等级着色：ok 绿、warn 黄、fail 红
func (p Palette) Level(level, s string) string {
	switch level {
	case "ok":
		return p.Green(s)
	case "warn":
		return p.Yellow(s)
	case "fail":
		return p.Red(s)
	}
	return s
}

// displayWidth 终端显示宽度：中日韩字符占两格，去掉颜色控制码
func displayWidth(s string) int {
	w := 0
	inEsc := false
	for _, r := range s {
		switch {
		case r == '\x1b':
			inEsc = true
		case inEsc:
			if r == 'm' {
				inEsc = false
			}
		case r >= 0x1100 && (r <= 0x115f || (r >= 0x2e80 && r <= 0xa4cf) || (r >= 0xac00 && r <= 0xd7a3) ||
			(r >= 0xf900 && r <= 0xfaff) || (r >= 0xfe30 && r <= 0xfe4f) || (r >= 0xff00 && r <= 0xff60) || (r >= 0xffe0 && r <= 0xffe6)):
			w += 2
		default:
			if utf8.RuneLen(r) > 0 {
				w++
			}
		}
	}
	return w
}

// Table 简单的对齐表格
type Table struct {
	Header []string
	Rows   [][]string
}

// Render 按显示宽度对齐（列间两个空格）
func (t Table) Render(p Palette) string {
	widths := make([]int, len(t.Header))
	all := append([][]string{t.Header}, t.Rows...)
	for _, row := range all {
		for i, c := range row {
			if i < len(widths) && displayWidth(c) > widths[i] {
				widths[i] = displayWidth(c)
			}
		}
	}
	var b strings.Builder
	for ri, row := range all {
		for i, c := range row {
			if i >= len(widths) {
				break
			}
			cell := c
			if ri == 0 {
				cell = p.Bold(c)
			}
			b.WriteString(cell)
			if i < len(row)-1 {
				b.WriteString(strings.Repeat(" ", widths[i]-displayWidth(c)+2))
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}
