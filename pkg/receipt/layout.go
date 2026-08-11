package receipt

import (
	"strings"
	"unicode"

	"github.com/signintech/gopdf"
)

// fontFamily 注册进 gopdf 的字体族名。常规体与粗体注册在同一族的不同 Style 下，
// 之后用 SetFont(family, "B", size) 切换。
const fontFamily = "receipt"

// canvas 在 gopdf 之上薄薄一层排版助手：游标、翻页、换行、对齐。
//
// gopdf 本身只提供「在某个坐标画一段文字」，凭证需要的是「按流式往下排，排不下就翻页」。
// 这层的存在是为了让渲染代码读起来是「画一段、往下走」，而不是满屏的坐标算术。
type canvas struct {
	pdf  *gopdf.GoPdf
	th   theme
	y    float64
	page int

	// bold 是否真的注册了粗体字型。多数中文字体没有粗体伴侣，
	// 此时全部退回常规体，靠字号与颜色区分层级 —— 假粗体（描边加粗）在小字号下会糊成一团。
	bold bool

	// pageHeader 续页页眉，翻页后立即调用
	pageHeader func(c *canvas)
	// pageTokens 每页页码占位符的名字，渲染结束后统一回填「第 N / 共 M 页」
	pageTokens []string
}

func newCanvas(pdf *gopdf.GoPdf, th theme, hasBold bool) *canvas {
	return &canvas{pdf: pdf, th: th, bold: hasBold}
}

// ── 字体与度量 ──

func (c *canvas) setFont(size float64, bold bool) {
	style := ""
	if bold && c.bold {
		style = "B"
	}
	// 字体族在渲染开始时就已注册，这里失败只可能是字号非法，不该让整份凭证失败
	_ = c.pdf.SetFont(fontFamily, style, size)
}

func (c *canvas) setColor(color rgb) {
	c.pdf.SetTextColor(color.r, color.g, color.b)
}

// measure 当前字体下这段文字的宽度。
func (c *canvas) measure(text string) float64 {
	w, err := c.pdf.MeasureTextWidth(text)
	if err != nil {
		// 度量失败时给一个偏保守的估算，宁可排得松一点也不要让文字互相压住
		return float64(len([]rune(text))) * 6
	}
	return w
}

// ── 绘制 ──

// text 在 (x, y) 处以左对齐绘制一行。y 为行盒顶部。
func (c *canvas) text(x, y float64, s string) {
	if s == "" {
		return
	}
	c.pdf.SetXY(x, y)
	_ = c.pdf.Cell(nil, s)
}

// textRight 右边缘对齐到 right。
func (c *canvas) textRight(right, y float64, s string) {
	if s == "" {
		return
	}
	c.text(right-c.measure(s), y, s)
}

// textCenter 以 center 为中线居中。
func (c *canvas) textCenter(center, y float64, s string) {
	if s == "" {
		return
	}
	c.text(center-c.measure(s)/2, y, s)
}

// styled 一次设定字号/粗细/颜色再绘制，省掉调用点重复三行。
func (c *canvas) styled(size float64, bold bool, color rgb) *canvas {
	c.setFont(size, bold)
	c.setColor(color)
	return c
}

func (c *canvas) line(x1, y1, x2, y2 float64, color rgb) {
	c.pdf.SetStrokeColor(color.r, color.g, color.b)
	c.pdf.SetLineWidth(0.6)
	c.pdf.Line(x1, y1, x2, y2)
}

func (c *canvas) rule(y float64, color rgb) {
	c.line(c.th.contentLeft(), y, c.th.contentRight(), y, color)
}

func (c *canvas) fillRect(x, y, w, h float64, color rgb) {
	c.pdf.SetFillColor(color.r, color.g, color.b)
	c.pdf.RectFromUpperLeftWithStyle(x, y, w, h, "F")
}

// setTracking 设置字距。全大写的小标签不拉字距会挤成一团黑块，
// 这是这套版式里辨识度最高的一处细节。用完必须归零，否则会渗到后续文字上。
func (c *canvas) setTracking(spacing float64) {
	_ = c.pdf.SetCharSpacing(spacing)
}

// label 画一个全大写、带字距的小标签（区块标题、表头）。
func (c *canvas) label(x, y float64, text string, color rgb) {
	c.styled(c.th.sizeLabel, true, color)
	c.setTracking(c.th.trackingLabel)
	c.text(x, y, text)
	c.setTracking(0)
}

// labelRight 同 label，但右对齐。
func (c *canvas) labelRight(right, y float64, text string, color rgb) {
	c.styled(c.th.sizeLabel, true, color)
	c.setTracking(c.th.trackingLabel)
	c.textRight(right, y, text)
	c.setTracking(0)
}

// badge 画一枚状态徽标：圆角胶囊 + 浅色底 + 同色深字，返回其宽度。
// right 为右边缘，徽标向左生长 —— 状态永远贴着页面右侧，扫一眼就能看到。
func (c *canvas) badge(right, y float64, text string, color rgb) float64 {
	const padX, height = 10, 17
	c.setFont(c.th.sizeLabel, true)
	c.setTracking(c.th.trackingLabel)
	w := c.measure(text) + padX*2 + c.th.trackingLabel
	// 底色在 Lab 空间调向纸色：sRGB 插值出来的浅色会发灰，衬不住上面的深色字
	c.fillRounded(right-w, y, w, height, height/2, tint(color, 0.88))
	c.setColor(shade(color, 0.22))
	c.text(right-w+padX, y+4.2, text)
	c.setTracking(0)
	return w
}

// ── 换行 ──

// wrap 按宽度折行。中日韩文字逐字可断，拉丁文字优先按词断 ——
// 这两套规则不能混用：对中文按词断会整段不换行，对英文逐字断会把单词劈成两半。
func (c *canvas) wrap(s string, width float64) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var lines []string
	for _, paragraph := range strings.Split(s, "\n") {
		lines = append(lines, c.wrapParagraph(paragraph, width)...)
	}
	return lines
}

func (c *canvas) wrapParagraph(s string, width float64) []string {
	if s == "" {
		return []string{""}
	}
	if c.measure(s) <= width {
		return []string{s}
	}
	var (
		lines   []string
		line    []rune
		lastGap = -1 // 最近一个可断点在 line 中的下标（该处之后可以断行）
	)
	flush := func(upto int) {
		lines = append(lines, strings.TrimRight(string(line[:upto]), " "))
		line = append([]rune{}, line...)[upto:]
		// 断行后行首的空格没有意义，去掉
		for len(line) > 0 && line[0] == ' ' {
			line = line[1:]
		}
		lastGap = -1
	}
	for _, r := range s {
		line = append(line, r)
		if isWideBreakable(r) || r == ' ' || r == '-' || r == '/' {
			lastGap = len(line)
		}
		if c.measure(string(line)) <= width {
			continue
		}
		switch {
		case lastGap > 0 && lastGap < len(line):
			flush(lastGap)
		case len(line) > 1:
			// 一个词长过整行（长订单号、长 URL）：硬断，总比溢出到页边外好
			flush(len(line) - 1)
		}
	}
	if len(line) > 0 {
		lines = append(lines, strings.TrimRight(string(line), " "))
	}
	return lines
}

// isWideBreakable 中日韩表意文字与假名逐字可断。
func isWideBreakable(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) ||
		(r >= 0x3000 && r <= 0x303F) // 中日韩标点
}

// truncate 单行截断并加省略号，用于表格里绝不能折行的窄列。
func (c *canvas) truncate(s string, width float64) string {
	if s == "" || c.measure(s) <= width {
		return s
	}
	runes := []rune(s)
	for len(runes) > 1 {
		runes = runes[:len(runes)-1]
		if c.measure(string(runes)+"…") <= width {
			return string(runes) + "…"
		}
	}
	return "…"
}

// ── 分页 ──

// need 确保剩余高度够画 h；不够就翻页。返回是否发生了翻页 ——
// 表格据此决定要不要在新页重画表头。
func (c *canvas) need(h float64) bool {
	if c.y+h <= c.th.bodyBottom {
		return false
	}
	c.newPage()
	return true
}

// newPage 翻到新页并画上页眉页脚骨架。
func (c *canvas) newPage() {
	c.pdf.AddPage()
	c.page++
	c.y = c.th.contTop
	c.drawFooterFrame()
	if c.pageHeader != nil {
		c.pageHeader(c)
	}
}

// drawFooterFrame 画页脚分隔线并占好页码位置。
//
// 页码要写「第 N 页 / 共 M 页」，而总页数在最后一页画完前是未知的，
// 因此这里只占位，渲染结束后统一回填。
func (c *canvas) drawFooterFrame() {
	c.rule(c.th.footerY-10, c.th.hairline)
	c.styled(c.th.sizeFooter, false, c.th.muted)
	token := "page-" + itoa(c.page)
	c.pageTokens = append(c.pageTokens, token)
	// 占位宽度按最宽的页码文案预留；右对齐回填时会在这个宽度内靠右
	const slot = 120
	c.pdf.SetXY(c.th.contentRight()-slot, c.th.footerY)
	_ = c.pdf.PlaceHolderText(token, slot)
}

// fillPageNumbers 回填页码。必须在所有页画完之后调用，且当前字体要与占位时一致 ——
// gopdf 回填时用「当前字体」度量文字来做对齐。
func (c *canvas) fillPageNumbers(render func(page, total int) string) {
	c.styled(c.th.sizeFooter, false, c.th.muted)
	total := c.page
	for i, token := range c.pageTokens {
		_ = c.pdf.FillInPlaceHoldText(token, render(i+1, total), gopdf.Right)
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [12]byte
	pos := len(buf)
	for v > 0 {
		pos--
		buf[pos] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[pos:])
}
