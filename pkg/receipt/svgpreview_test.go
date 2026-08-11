package receipt

import (
	"fmt"
	"html"
	"strconv"
	"strings"
)

// PDF → SVG 还原。
//
// 它解析的是**生成出来的 PDF 自身的内容流**（矢量算子 + 文字坐标 + 颜色），
// 不是照着渲染器的代码再画一遍 —— 后者只能证明「我以为我画了什么」，
// 前者才能证明「纸上真的有什么」。色块位置、圆角形状、文字坐标与字号全部来自产物。
//
// 唯一不还原的是字形本身：PDF 里嵌的是字体子集，SVG 交给浏览器用系统字体渲染，
// 因此字宽会与 PDF 有细微差异。版面结构、配色、对齐关系都是准确的。

// svgPage 把一页内容流转成 SVG。
func pdfToSVG(pdf []byte) []string {
	toUnicode := map[uint16]rune{}
	var contents []string
	for _, stream := range pdfStreams(pdf) {
		switch {
		case strings.Contains(stream, "beginbfrange"):
			parseToUnicode(stream, toUnicode)
		case strings.Contains(stream, " TJ"):
			contents = append(contents, stream)
		}
	}
	th := defaultTheme()
	pages := make([]string, 0, len(contents))
	for _, content := range contents {
		pages = append(pages, renderSVG(content, toUnicode, th.pageW, th.pageH))
	}
	return pages
}

// svgState 内容流解析过程中的图形状态。
type svgState struct {
	fill, stroke string
	lineWidth    float64
	fontSize     float64
	charSpacing  float64
	textColor    string
}

func renderSVG(content string, toUnicode map[uint16]rune, pageW, pageH float64) string {
	var body strings.Builder
	state := svgState{fill: "#000000", stroke: "#000000", lineWidth: 1, textColor: "#000000"}

	tokens := strings.Fields(content)
	var nums []float64
	var path []string
	inText := false
	var textX, textY float64

	flushNums := func() { nums = nums[:0] }
	num := func(i int) float64 {
		if i < 0 || i >= len(nums) {
			return 0
		}
		return nums[i]
	}

	for i := 0; i < len(tokens); i++ {
		token := tokens[i]
		if value, err := strconv.ParseFloat(token, 64); err == nil {
			nums = append(nums, value)
			continue
		}
		switch token {
		case "rg": // 填充色
			color := svgColor(num(len(nums)-3), num(len(nums)-2), num(len(nums)-1))
			if inText {
				state.textColor = color
			} else {
				state.fill = color
			}
			flushNums()
		case "RG": // 描边色
			state.stroke = svgColor(num(len(nums)-3), num(len(nums)-2), num(len(nums)-1))
			flushNums()
		case "w":
			state.lineWidth = num(len(nums) - 1)
			flushNums()
		case "re": // 矩形：x y w h re <style>
			x, y, w, h := num(0), num(1), num(2), num(3)
			style := ""
			if i+1 < len(tokens) {
				style = tokens[i+1]
			}
			fmt.Fprintf(&body, `<rect x="%.2f" y="%.2f" width="%.2f" height="%.2f" %s/>`+"\n",
				x, pageH-y-h, w, h, svgPaint(style, state))
			flushNums()
		case "m": // 起点
			path = path[:0]
			path = append(path, fmt.Sprintf("M %.2f %.2f", num(len(nums)-2), pageH-num(len(nums)-1)))
			flushNums()
		case "l":
			path = append(path, fmt.Sprintf("L %.2f %.2f", num(len(nums)-2), pageH-num(len(nums)-1)))
			flushNums()
		case "f", "s", "b", "S", "F":
			if len(path) > 0 {
				closing := ""
				if token != "S" {
					closing = " Z"
				}
				fmt.Fprintf(&body, `<path d="%s%s" %s/>`+"\n",
					strings.Join(path, " "), closing, svgPaint(token, state))
				path = path[:0]
			}
			flushNums()
		case "BT":
			inText = true
			flushNums()
		case "ET":
			inText = false
			flushNums()
		case "TD", "Td":
			textX, textY = num(len(nums)-2), num(len(nums)-1)
			flushNums()
		case "Tf":
			state.fontSize = num(len(nums) - 1)
			flushNums()
		case "Tc":
			state.charSpacing = num(len(nums) - 1)
			flushNums()
		case "TJ":
			if i > 0 {
				text := decodeGlyphs(tokens[i-1], toUnicode)
				if strings.TrimSpace(text) != "" {
					spacing := ""
					if state.charSpacing > 0.01 {
						spacing = fmt.Sprintf(` letter-spacing="%.2f"`, state.charSpacing)
					}
					fmt.Fprintf(&body, `<text x="%.2f" y="%.2f" font-size="%.2f" fill="%s"%s>%s</text>`+"\n",
						textX, pageH-textY, state.fontSize, state.textColor, spacing, html.EscapeString(text))
				}
			}
			flushNums()
		default:
			flushNums()
		}
	}

	return fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %.2f %.2f" width="%.0f" height="%.0f" font-family="'Helvetica Neue',Helvetica,Arial,'PingFang SC','Microsoft YaHei','Noto Sans CJK SC',sans-serif">
<rect x="0" y="0" width="%.2f" height="%.2f" fill="#ffffff"/>
%s</svg>`, pageW, pageH, pageW, pageH, pageW, pageH, body.String())
}

// svgPaint 把 PDF 的绘制样式翻成 SVG 的 fill/stroke 属性。
func svgPaint(style string, state svgState) string {
	switch style {
	case "F", "f":
		return fmt.Sprintf(`fill="%s"`, state.fill)
	case "D", "S", "s":
		return fmt.Sprintf(`fill="none" stroke="%s" stroke-width="%.2f"`, state.stroke, state.lineWidth)
	case "b", "B":
		return fmt.Sprintf(`fill="%s" stroke="%s" stroke-width="%.2f"`, state.fill, state.stroke, state.lineWidth)
	default:
		return fmt.Sprintf(`fill="%s"`, state.fill)
	}
}

func svgColor(r, g, b float64) string {
	clamp := func(v float64) int { return int(max(0, min(255, v*255+0.5))) }
	return fmt.Sprintf("#%02X%02X%02X", clamp(r), clamp(g), clamp(b))
}
