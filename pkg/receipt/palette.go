package receipt

import (
	"math"

	"github.com/lucasb-eyer/go-colorful"
)

// 凭证配色。
//
// 取向是「暖象牙纸面 + 暖近黑文字 + 单一珊瑚强调色」——
// 与 Claude / Anthropic 的视觉语言一致：整份文档只有一个彩色，
// 其余全部由暖中性灰承担层级。这比多色方案更耐看，也更像一份正式凭证
// 而不是一张宣传单。
//
// 派生色（徽标底、面板底、强调文字）一律用 go-colorful 在 **Lab 空间**计算。
// 在 sRGB 里直接线性插值算出来的浅色会发灰发脏 —— 珊瑚色尤其明显，
// 它的浅色调在 sRGB 插值下会偏向粉褐，在 Lab 下才保持干净的暖橘。

// 品牌基准色。数值来自 Anthropic 品牌规范里的暖中性 + 珊瑚强调。
var (
	// 纸面：极浅暖白，用于卡片与斑马底
	colorPaper = mustHex("#FAF9F5")
	// 象牙：抬头带、表头行、汇总面板
	colorBand = mustHex("#F0EEE6")
	// 更深一档的象牙，用于面板描边
	colorBandEdge = mustHex("#E5E2D6")
	// 细线
	colorHairline = mustHex("#DEDCD3")
	// 主文字：暖近黑。纯黑在暖底上会显得脏而硬
	colorInk = mustHex("#141413")
	// 次级文字
	colorInkSoft = mustHex("#3D3D3A")
	// 说明性文字
	colorMuted = mustHex("#6B6B66")
	// 最弱一级（区块标签、页脚）。刻意没有取更浅的灰：
	// 这一级承载的是 8pt 的小字标签，浅到 #91918D 时对白底只有 3.2:1，
	// 达不到 WCAG 对小字的 4.5:1 —— 一份会被打印、会被老花眼阅读的凭证不该这样。
	colorFaint = mustHex("#76766F")
	// 珊瑚强调色：整份凭证唯一的彩色
	colorAccent = mustHex("#D97757")

	// 状态色。刻意都往暖里调，与珊瑚同处一个色温，
	// 否则一个标准的品红或亮绿会把整页的调子打散。
	colorPaid     = mustHex("#3F7A55")
	colorRefund   = mustHex("#B0742A")
	colorFailed   = mustHex("#B4432F")
	colorNeutral  = mustHex("#6B6B66")
	colorOnAccent = mustHex("#FFFFFF")
)

// rgb 一个 8 位色。
type rgb struct{ r, g, b uint8 }

func mustHex(value string) rgb {
	parsed, err := colorful.Hex(value)
	if err != nil {
		panic("receipt: 非法的调色值 " + value)
	}
	r, g, b := parsed.RGB255()
	return rgb{r, g, b}
}

func (c rgb) colorful() colorful.Color {
	return colorful.Color{R: float64(c.r) / 255, G: float64(c.g) / 255, B: float64(c.b) / 255}
}

func fromColorful(c colorful.Color) rgb {
	r, g, b := c.Clamped().RGB255()
	return rgb{r, g, b}
}

// tint 把颜色按比例调向纸色，用于徽标与面板底。
// 在 Lab 空间插值：sRGB 插值的浅色调会发灰，珊瑚色尤其明显。
func tint(base rgb, ratio float64) rgb {
	return fromColorful(base.colorful().BlendLab(colorPaper.colorful(), clamp01(ratio)).Clamped())
}

// shade 把颜色按比例调向主文字色，用于「彩色但必须能读」的文字。
//
// 珊瑚色本身在象牙底上只有 2.6:1 的对比度，够画一条线，不够印一行字。
// 强调文字必须用它的深色调 —— 这不是审美偏好，是可读性下限。
func shade(base rgb, ratio float64) rgb {
	return fromColorful(base.colorful().BlendLab(colorInk.colorful(), clamp01(ratio)).Clamped())
}

// contrastRatio WCAG 相对对比度（1:1 ~ 21:1）。
// 用于把「配色好看」变成可断言的事实：正文 ≥ 4.5，大字与图形 ≥ 3。
func contrastRatio(fg, bg rgb) float64 {
	l1, l2 := relativeLuminance(fg), relativeLuminance(bg)
	if l1 < l2 {
		l1, l2 = l2, l1
	}
	return (l1 + 0.05) / (l2 + 0.05)
}

func relativeLuminance(c rgb) float64 {
	channel := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(c.r) + 0.7152*channel(c.g) + 0.0722*channel(c.b)
}

func clamp01(v float64) float64 {
	return max(0, min(1, v))
}
