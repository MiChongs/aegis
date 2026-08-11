package receipt

import (
	"math"

	"github.com/boombuler/barcode/qr"
	"github.com/signintech/gopdf"
)

// 版式用到的矢量图元。
//
// gopdf 只给了直线、贝塞尔曲线与多边形，没有圆角矩形，也没有二维码。
// 这里把它们补齐 —— 全部走矢量而不是位图：凭证会被打印，
// 一张 300dpi 的位图二维码放大后是糊的，矢量的在任何缩放下都锐利。

// roundedRectPath 生成圆角矩形的多边形近似。
//
// 每个圆角用 quarterSegments 段折线逼近。8 段在 A4 的 6pt 半径下，
// 最大偏差约 0.03pt —— 远低于 300dpi 打印的一个像素（0.24pt），肉眼与打印都看不出折线。
// 用多边形而不是贝塞尔，是因为 gopdf 的 Curve 每次调用都自成一条路径，
// 拼不出「一个闭合的圆角矩形」这样的单一填充区域。
func roundedRectPath(x, y, w, h, radius float64) []gopdf.Point {
	const quarterSegments = 8
	radius = min(radius, min(w, h)/2)
	if radius <= 0 {
		return []gopdf.Point{{X: x, Y: y}, {X: x + w, Y: y}, {X: x + w, Y: y + h}, {X: x, Y: y + h}}
	}
	points := make([]gopdf.Point, 0, 4*(quarterSegments+1))
	// 四个圆心，按左上 → 右上 → 右下 → 左下 顺时针
	corners := [4]struct{ cx, cy, start float64 }{
		{x + radius, y + radius, math.Pi},           // 左上
		{x + w - radius, y + radius, 1.5 * math.Pi}, // 右上
		{x + w - radius, y + h - radius, 0},         // 右下
		{x + radius, y + h - radius, 0.5 * math.Pi}, // 左下
	}
	for _, corner := range corners {
		for i := 0; i <= quarterSegments; i++ {
			angle := corner.start + (math.Pi/2)*float64(i)/quarterSegments
			points = append(points, gopdf.Point{
				X: corner.cx + radius*math.Cos(angle),
				Y: corner.cy + radius*math.Sin(angle),
			})
		}
	}
	return points
}

// fillRounded 填充一个圆角矩形。
func (c *canvas) fillRounded(x, y, w, h, radius float64, color rgb) {
	c.pdf.SetFillColor(color.r, color.g, color.b)
	c.pdf.Polygon(roundedRectPath(x, y, w, h, radius), "F")
}

// fillRoundedTop 只有上方两角是圆的（表头行贴着表体，下方必须是直角）。
func (c *canvas) fillRoundedTop(x, y, w, h, radius float64, color rgb) {
	radius = min(radius, min(w, h))
	const quarterSegments = 8
	points := make([]gopdf.Point, 0, 2*(quarterSegments+1)+2)
	for i := 0; i <= quarterSegments; i++ { // 左上角
		angle := math.Pi + (math.Pi/2)*float64(i)/quarterSegments
		points = append(points, gopdf.Point{X: x + radius + radius*math.Cos(angle), Y: y + radius + radius*math.Sin(angle)})
	}
	for i := 0; i <= quarterSegments; i++ { // 右上角
		angle := 1.5*math.Pi + (math.Pi/2)*float64(i)/quarterSegments
		points = append(points, gopdf.Point{X: x + w - radius + radius*math.Cos(angle), Y: y + radius + radius*math.Sin(angle)})
	}
	points = append(points, gopdf.Point{X: x + w, Y: y + h}, gopdf.Point{X: x, Y: y + h})
	c.pdf.SetFillColor(color.r, color.g, color.b)
	c.pdf.Polygon(points, "F")
}

// strokeRounded 描一个圆角矩形的边。
func (c *canvas) strokeRounded(x, y, w, h, radius float64, color rgb, width float64) {
	c.pdf.SetStrokeColor(color.r, color.g, color.b)
	c.pdf.SetLineWidth(width)
	c.pdf.Polygon(roundedRectPath(x, y, w, h, radius), "D")
}

// brandMark 画一枚品牌标记：圆角方块 + 品牌首字母。
//
// 刻意是一个中性的几何标记，**不复刻任何第三方商标** ——
// 这份凭证上印的是使用方自己的品牌，借用别家的标志既不合适也没有意义。
func (c *canvas) brandMark(x, y, size float64, letter string, bg, fg rgb) {
	if letter == "" {
		return
	}
	c.fillRounded(x, y, size, size, size*0.28, bg)
	c.styled(size*0.52, true, fg)
	c.textCenter(x+size/2, y+size*0.27, letter)
}

// drawQRCode 以矢量方式画一个二维码，返回是否成功。
//
// 逐模块画填充小方块而不是嵌一张位图：凭证是要打印的，
// 位图二维码在纸上放大后边缘发糊，矢量的在任何分辨率下都是锐利的黑白边界。
// 相邻模块之间刻意留 0 间隙（用同一格宽），否则扫码器会读不出来。
func (c *canvas) drawQRCode(x, y, size float64, content string, dark rgb) bool {
	if content == "" || size <= 0 {
		return false
	}
	// 纠错等级取 M（约 15% 冗余）：凭证会被打印、折叠、复印，
	// L 档在纸质件上失败率明显更高；Q/H 会让模块变密，小尺寸下反而更难扫。
	code, err := qr.Encode(content, qr.M, qr.Auto)
	if err != nil {
		return false
	}
	bounds := code.Bounds()
	modules := bounds.Dx()
	if modules <= 0 {
		return false
	}
	// 静区：规范要求四周留 4 个模块宽的空白，少了扫码器定位不到
	const quiet = 4
	cell := size / float64(modules+2*quiet)
	c.pdf.SetFillColor(dark.r, dark.g, dark.b)
	for my := range modules {
		for mx := range modules {
			r, g, b, _ := code.At(bounds.Min.X+mx, bounds.Min.Y+my).RGBA()
			if r|g|b != 0 { // 非黑即留白
				continue
			}
			c.pdf.RectFromUpperLeftWithStyle(
				x+(float64(mx+quiet))*cell,
				y+(float64(my+quiet))*cell,
				cell+0.02, cell+0.02, // 轻微重叠，消除相邻模块间的白缝
				"F")
		}
	}
	return true
}
