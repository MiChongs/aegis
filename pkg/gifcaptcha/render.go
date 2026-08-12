package gifcaptcha

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"math"
	mrand "math/rand/v2"

	"github.com/fogleman/gg"
	colorful "github.com/lucasb-eyer/go-colorful"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/math/f64"
)

// 字形按 2 倍尺寸光栅化再缩回，避免旋转/缩放重采样后边缘发糊
const superSample = 2.0

// ────────────────────── 场景 ──────────────────────
//
// 一次生成 = 一个场景 + 一条 0→1 的时间轴。周期运动的相位一律写成「整数圈 × t」，
// 于是 t=1 与 t=0 重合，GIF 循环播放时交界处不跳变。

type scene struct {
	opts   Options
	bg     color.RGBA
	glyphs []*glyph
	dots   []dot
	curves []curve
	warp   warp
	sweep  sweep
}

// glyph 一个字符的渲染参数
type glyph struct {
	mask *image.RGBA // 白色预乘蒙版（超采样尺寸）
	buf  *image.RGBA // 上色缓冲，与 mask 同尺寸，逐帧复用

	cx, cy    float64 // 画布上的中心点
	baseScale float64 // 蒙版 → 画布的基准缩放
	shear     float64 // 静态斜切
	tilt      float64 // 静态倾角（弧度）

	rotAmp, rotPhase, rotCycles       float64 // 摆动
	bobAmp, bobPhase, bobCycles       float64 // 上下浮动
	swayAmp, swayPhase, swayCycles    float64 // 左右浮动
	pulseAmp, pulsePhase, pulseCycles float64 // 呼吸缩放

	hue      float64 // 起始色相
	hueTurns float64 // 每轮转过的整圈数（0 = 不变色）
	chroma   float64
	lum      float64
}

// dot 漂移噪点。归一化坐标，每轮走整数个画布宽/高，首尾无缝。
type dot struct {
	x0, y0 float64
	kx, ky float64
	radius float64
	col    color.RGBA
	alpha  float64
}

// curve 干扰曲线。整条正弦横跨画布，并逐帧沿 x 平移。
type curve struct {
	y0     float64 // 基准高度（0-1）
	amp    float64 // 振幅（像素）
	waves  float64 // 画布宽度内的波数
	cycles float64 // 每轮平移的周期数（整数）
	width  float64
	col    color.RGBA
	alpha  float64
	front  bool // true = 画在字符上层（划过字形，OCR 更难切分）
}

// warp 全画面水波扭曲：横向位移随 y 变化、纵向位移随 x 变化，两者相位都在走。
type warp struct {
	ampX, ampY     float64
	lamX, lamY     float64
	cycX, cycY     float64
	phaseX, phaseY float64
}

// sweep 扫过画面的色带：让整片背景逐帧变化，逐帧相减消不掉干扰
type sweep struct {
	enabled bool
	width   float64
	alpha   float64
	col     color.RGBA
}

// ────────────────────── 入口 ──────────────────────

// render 按参数与答案画出一段 GIF。
func render(opts Options, answer string, rng *mrand.Rand) ([]byte, error) {
	sc, err := buildScene(opts, answer, rng)
	if err != nil {
		return nil, err
	}

	bounds := image.Rect(0, 0, opts.Width, opts.Height)
	canvas := image.NewRGBA(bounds)
	warped := image.NewRGBA(bounds)
	dc := gg.NewContextForRGBA(canvas)
	dc.SetLineCapRound()
	dc.SetLineJoinRound()

	// GIF 延迟单位是 1/100 秒；低于 2 会被浏览器改判成 10（100ms）
	delay := int(math.Round(opts.FrameDelay.Seconds() * 100))
	if delay < 2 {
		delay = 2
	}

	out := &gif.GIF{
		Image:     make([]*image.Paletted, 0, opts.Frames),
		Delay:     make([]int, 0, opts.Frames),
		Disposal:  make([]byte, 0, opts.Frames),
		LoopCount: 0, // 0 = 无限循环
		Config: image.Config{
			ColorModel: sharedPalette,
			Width:      opts.Width,
			Height:     opts.Height,
		},
	}

	for i := 0; i < opts.Frames; i++ {
		t := float64(i) / float64(opts.Frames)
		drawFrame(dc, canvas, sc, t)
		applyWarp(warped, canvas, sc.warp, t)

		frame := image.NewPaletted(bounds, sharedPalette)
		quantize(frame, warped)
		out.Image = append(out.Image, frame)
		out.Delay = append(out.Delay, delay)
		// 每帧都铺满且不透明，直接盖上去即可
		out.Disposal = append(out.Disposal, gif.DisposalNone)
	}

	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, out); err != nil {
		return nil, fmt.Errorf("gifcaptcha: 编码 GIF 失败: %w", err)
	}
	return buf.Bytes(), nil
}

// ────────────────────── 场景构造 ──────────────────────

func buildScene(opts Options, answer string, rng *mrand.Rand) (*scene, error) {
	glyphs, err := buildGlyphs(opts, answer, rng)
	if err != nil {
		return nil, err
	}
	bgHue := rng.Float64() * 360
	sc := &scene{
		opts:   opts,
		bg:     hcl(bgHue, 0.04+rng.Float64()*0.03, 0.93+rng.Float64()*0.04),
		glyphs: glyphs,
		dots:   buildDots(opts, rng),
		curves: buildCurves(opts, bgHue, rng),
		warp:   buildWarp(opts, rng),
		sweep:  buildSweep(opts, bgHue, rng),
	}
	return sc, nil
}

func buildGlyphs(opts Options, answer string, rng *mrand.Rand) ([]*glyph, error) {
	faces, err := loadFonts()
	if err != nil {
		return nil, err
	}
	runes := []rune(answer)
	if len(runes) == 0 {
		return nil, fmt.Errorf("gifcaptcha: 答案为空")
	}

	wob := float64(opts.Wobble) / 100
	w := float64(opts.Width)
	h := float64(opts.Height)
	margin := w * 0.06
	cell := (w - 2*margin) / float64(len(runes))
	// 字号同时受画布高度与单格宽度约束
	baseSize := math.Min(h*0.68, cell*1.45)

	glyphs := make([]*glyph, 0, len(runes))
	for i, r := range runes {
		fnt := faces[rng.IntN(len(faces))]
		size := baseSize * (0.88 + rng.Float64()*0.28)

		face, err := newFace(fnt, size*superSample)
		if err != nil {
			return nil, fmt.Errorf("gifcaptcha: 创建字形句柄失败: %w", err)
		}
		mask, err := renderGlyph(face, r)
		closeErr := face.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, fmt.Errorf("gifcaptcha: 释放字形句柄失败: %w", closeErr)
		}

		g := &glyph{
			mask:      mask,
			buf:       image.NewRGBA(mask.Bounds()),
			baseScale: 1 / superSample,
			shear:     (rng.Float64() - 0.5) * 0.45,
			tilt:      (rng.Float64() - 0.5) * 0.42,

			rotAmp:    wob * 0.36 * (0.6 + rng.Float64()*0.4),
			rotPhase:  rng.Float64(),
			rotCycles: float64(1 + rng.IntN(2)),

			bobAmp:    wob * h * 0.10 * (0.5 + rng.Float64()*0.5),
			bobPhase:  rng.Float64(),
			bobCycles: float64(1 + rng.IntN(2)),

			swayAmp:    wob * cell * 0.12 * (0.4 + rng.Float64()*0.6),
			swayPhase:  rng.Float64(),
			swayCycles: float64(1 + rng.IntN(2)),

			pulseAmp:    wob * 0.13 * (0.5 + rng.Float64()*0.5),
			pulsePhase:  rng.Float64(),
			pulseCycles: float64(1 + rng.IntN(2)),

			hue:      rng.Float64() * 360,
			hueTurns: float64(1 + rng.IntN(2)),
			chroma:   0.42 + rng.Float64()*0.20,
			lum:      0.30 + rng.Float64()*0.16,
		}
		if rng.IntN(2) == 0 {
			g.hueTurns = -g.hueTurns
		}

		// 落点夹回画布内时要算上运动与缩放的最大幅度，否则某一帧会切掉半个字
		g.cx = margin + cell*(float64(i)+0.5) + (rng.Float64()-0.5)*cell*0.16
		g.cy = h/2 + (rng.Float64()-0.5)*h*0.12

		maxScale := g.baseScale * (1 + g.pulseAmp)
		halfW := float64(mask.Bounds().Dx()) / 2 * maxScale
		halfH := float64(mask.Bounds().Dy()) / 2 * maxScale
		// 旋转让外接盒变大：取两边投影之和的上界。
		rot := math.Abs(g.tilt) + g.rotAmp
		extX := halfW*math.Cos(rot) + halfH*math.Sin(rot) + g.swayAmp
		extY := halfW*math.Sin(rot) + halfH*math.Cos(rot) + g.bobAmp
		g.cx = clampFloat(g.cx, extX*0.72, w-extX*0.72)
		g.cy = clampFloat(g.cy, extY*0.72, h-extY*0.72)

		glyphs = append(glyphs, g)
	}
	return glyphs, nil
}

func buildDots(opts Options, rng *mrand.Rand) []dot {
	count := int(float64(opts.Noise)/100*55) + 8
	// 按画布面积缩放，否则同一 noise 值在大画布上稀得看不见
	count = count * opts.Width * opts.Height / (DefaultWidth * DefaultHeight)
	if count > 400 {
		count = 400
	}
	dots := make([]dot, 0, count)
	for i := 0; i < count; i++ {
		d := dot{
			x0:     rng.Float64(),
			y0:     rng.Float64(),
			kx:     float64(rng.IntN(3) - 1),
			ky:     float64(rng.IntN(3) - 1),
			radius: 0.8 + rng.Float64()*1.6,
			col:    hcl(rng.Float64()*360, 0.25+rng.Float64()*0.35, 0.35+rng.Float64()*0.45),
			alpha:  0.25 + rng.Float64()*0.45,
		}
		// 静止噪点可被逐帧相减消掉
		if d.kx == 0 && d.ky == 0 {
			d.kx = 1
		}
		dots = append(dots, d)
	}
	return dots
}

func buildCurves(opts Options, bgHue float64, rng *mrand.Rand) []curve {
	back := 2 + opts.Noise/40 // 2~4 条
	front := 0
	if opts.Noise >= 35 {
		front = 1
	}
	if opts.Noise >= 75 {
		front = 2
	}
	h := float64(opts.Height)
	curves := make([]curve, 0, back+front)
	for i := 0; i < back+front; i++ {
		isFront := i >= back
		alpha := 0.30 + rng.Float64()*0.25
		if isFront {
			alpha = 0.22 + rng.Float64()*0.16 // 前景线更淡更细，不压住字
		}
		width := 1.0 + rng.Float64()*1.6
		if isFront {
			width = 0.9 + rng.Float64()*1.0
		}
		curves = append(curves, curve{
			y0:     0.15 + rng.Float64()*0.7,
			amp:    h * (0.06 + rng.Float64()*0.16),
			waves:  0.6 + rng.Float64()*1.8,
			cycles: float64(1 + rng.IntN(2)),
			width:  width,
			col:    hcl(math.Mod(bgHue+60+rng.Float64()*240, 360), 0.30+rng.Float64()*0.35, 0.35+rng.Float64()*0.35),
			alpha:  alpha,
			front:  isFront,
		})
	}
	return curves
}

func buildWarp(opts Options, rng *mrand.Rand) warp {
	wob := float64(opts.Wobble) / 100
	w := float64(opts.Width)
	h := float64(opts.Height)
	return warp{
		ampX:   wob * w * 0.018 * (0.5 + rng.Float64()),
		ampY:   wob * h * 0.055 * (0.5 + rng.Float64()),
		lamX:   w / (0.8 + rng.Float64()*1.2),
		lamY:   h / (0.6 + rng.Float64()*0.9),
		cycX:   float64(1 + rng.IntN(2)),
		cycY:   float64(1 + rng.IntN(2)),
		phaseX: rng.Float64(),
		phaseY: rng.Float64(),
	}
}

func buildSweep(opts Options, bgHue float64, rng *mrand.Rand) sweep {
	return sweep{
		enabled: opts.Noise > 0,
		width:   float64(opts.Width) * (0.16 + rng.Float64()*0.14),
		alpha:   0.14 + rng.Float64()*0.10,
		// 比背景暗、色相拉开：白色高光在浅色底上看不见
		col: hcl(math.Mod(bgHue+150+rng.Float64()*60, 360), 0.12+rng.Float64()*0.10, 0.62+rng.Float64()*0.12),
	}
}

// ────────────────────── 单帧渲染 ──────────────────────

func drawFrame(dc *gg.Context, canvas *image.RGBA, sc *scene, t float64) {
	w := float64(sc.opts.Width)
	h := float64(sc.opts.Height)

	dc.SetColor(sc.bg)
	dc.Clear()

	drawDots(dc, sc, t, w, h)
	drawCurves(dc, sc, t, w, false)
	drawGlyphs(canvas, sc, t)
	drawCurves(dc, sc, t, w, true)
	drawSweep(dc, sc, t, w, h)
}

func drawDots(dc *gg.Context, sc *scene, t float64, w, h float64) {
	for _, d := range sc.dots {
		x := frac(d.x0+d.kx*t) * w
		y := frac(d.y0+d.ky*t) * h
		dc.SetRGBA255(int(d.col.R), int(d.col.G), int(d.col.B), int(d.alpha*255))
		dc.DrawCircle(x, y, d.radius)
		dc.Fill()
	}
}

func drawCurves(dc *gg.Context, sc *scene, t float64, w float64, front bool) {
	const step = 3.0
	for _, c := range sc.curves {
		if c.front != front {
			continue
		}
		dc.SetRGBA255(int(c.col.R), int(c.col.G), int(c.col.B), int(c.alpha*255))
		dc.SetLineWidth(c.width)
		baseY := c.y0 * float64(sc.opts.Height)
		for x := 0.0; x <= w; x += step {
			y := baseY + c.amp*math.Sin(2*math.Pi*(c.waves*x/w+c.cycles*t))
			if x == 0 {
				dc.MoveTo(x, y)
				continue
			}
			dc.LineTo(x, y)
		}
		dc.Stroke()
	}
}

func drawGlyphs(canvas *image.RGBA, sc *scene, t float64) {
	for _, g := range sc.glyphs {
		col := hcl(math.Mod(g.hue+g.hueTurns*360*t+720, 360), g.chroma, g.lum)
		colorize(g.buf, g.mask, col)

		angle := g.tilt + g.rotAmp*math.Sin(2*math.Pi*(g.rotCycles*t+g.rotPhase))
		dx := g.swayAmp * math.Sin(2*math.Pi*(g.swayCycles*t+g.swayPhase))
		dy := g.bobAmp * math.Sin(2*math.Pi*(g.bobCycles*t+g.bobPhase))
		s := g.baseScale * (1 + g.pulseAmp*math.Sin(2*math.Pi*(g.pulseCycles*t+g.pulsePhase)))

		mw := float64(g.mask.Bounds().Dx())
		mh := float64(g.mask.Bounds().Dy())
		m := affMul(
			affTranslate(g.cx+dx, g.cy+dy),
			affMul(
				affRotate(angle),
				affMul(
					affShearX(g.shear),
					affMul(affScale(s, s), affTranslate(-mw/2, -mh/2)),
				),
			),
		)
		// ApproxBiLinear 而非带核重采样：后者 2 倍缩小时每像素积分十几个源像素，
		// 实测占整条链路四成时间（41ms → 10.6ms），画质差别在这个尺寸上看不出来
		xdraw.ApproxBiLinear.Transform(canvas, m, g.buf, g.buf.Bounds(), xdraw.Over, nil)
	}
}

func drawSweep(dc *gg.Context, sc *scene, t float64, w, h float64) {
	if !sc.sweep.enabled {
		return
	}
	// 从画布左外扫到右外：t=0 与 t=1 时都在画面外，循环处不跳变
	cx := -0.4*w + 1.8*w*t
	half := sc.sweep.width / 2
	c := sc.sweep.col

	// 两边渐隐。停靠色必须用 NRGBA：color.RGBA 是预乘的，{R,G,B,0} 是非法值，
	// gg 按预乘插值会把它当黑色混进来。
	grad := gg.NewLinearGradient(cx-half, 0, cx+half, 0)
	grad.AddColorStop(0, color.NRGBA{R: c.R, G: c.G, B: c.B, A: 0})
	grad.AddColorStop(0.5, color.NRGBA{R: c.R, G: c.G, B: c.B, A: uint8(sc.sweep.alpha * 255)})
	grad.AddColorStop(1, color.NRGBA{R: c.R, G: c.G, B: c.B, A: 0})

	dc.SetFillStyle(grad)
	dc.DrawRectangle(cx-half, 0, sc.sweep.width, h)
	dc.Fill()
	dc.SetColor(sc.bg) // 复位填充样式，否则后续绘制会拿到这条渐变
}

// colorize 把白色预乘蒙版染成指定颜色（结果仍是预乘 RGBA）。
func colorize(dst, src *image.RGBA, c color.RGBA) {
	for i := 0; i+3 < len(src.Pix); i += 4 {
		a := uint32(src.Pix[i+3])
		if a == 0 {
			dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = 0, 0, 0, 0
			continue
		}
		dst.Pix[i] = uint8(uint32(c.R) * a / 255)
		dst.Pix[i+1] = uint8(uint32(c.G) * a / 255)
		dst.Pix[i+2] = uint8(uint32(c.B) * a / 255)
		dst.Pix[i+3] = uint8(a)
	}
}

// ────────────────────── 水波扭曲 ──────────────────────

// applyWarp 按正弦位移重采样。位移只跟行/列有关，先各算一遍，内层只剩加法与取样。
func applyWarp(dst, src *image.RGBA, w warp, t float64) {
	b := src.Bounds()
	width := b.Dx()
	height := b.Dy()

	if w.ampX < 0.05 && w.ampY < 0.05 {
		copy(dst.Pix, src.Pix)
		return
	}

	rowShift := make([]float64, height) // 横向位移随 y 变
	for y := 0; y < height; y++ {
		rowShift[y] = w.ampX * math.Sin(2*math.Pi*(float64(y)/w.lamY+w.cycX*t+w.phaseX))
	}
	colShift := make([]float64, width) // 纵向位移随 x 变
	for x := 0; x < width; x++ {
		colShift[x] = w.ampY * math.Sin(2*math.Pi*(float64(x)/w.lamX+w.cycY*t+w.phaseY))
	}

	for y := 0; y < height; y++ {
		dstOff := dst.PixOffset(b.Min.X, b.Min.Y+y)
		sy := float64(y) + 0.0
		for x := 0; x < width; x++ {
			sampleBilinear(src, float64(x)+rowShift[y], sy+colShift[x], dst.Pix[dstOff:dstOff+4])
			dstOff += 4
		}
	}
}

// sampleBilinear 在 src 上做一次双线性取样，越界按边缘像素夹取。
func sampleBilinear(src *image.RGBA, fx, fy float64, out []uint8) {
	b := src.Bounds()
	maxX := b.Dx() - 1
	maxY := b.Dy() - 1

	fx = clampFloat(fx, 0, float64(maxX))
	fy = clampFloat(fy, 0, float64(maxY))
	x0 := int(fx)
	y0 := int(fy)
	x1 := x0 + 1
	y1 := y0 + 1
	if x1 > maxX {
		x1 = maxX
	}
	if y1 > maxY {
		y1 = maxY
	}
	tx := fx - float64(x0)
	ty := fy - float64(y0)

	o00 := src.PixOffset(b.Min.X+x0, b.Min.Y+y0)
	o10 := src.PixOffset(b.Min.X+x1, b.Min.Y+y0)
	o01 := src.PixOffset(b.Min.X+x0, b.Min.Y+y1)
	o11 := src.PixOffset(b.Min.X+x1, b.Min.Y+y1)

	for c := 0; c < 4; c++ {
		top := float64(src.Pix[o00+c])*(1-tx) + float64(src.Pix[o10+c])*tx
		bottom := float64(src.Pix[o01+c])*(1-tx) + float64(src.Pix[o11+c])*tx
		out[c] = uint8(top*(1-ty) + bottom*ty + 0.5)
	}
}

// ────────────────────── 小工具 ──────────────────────

// hcl 在 HCL 空间取色并夹回 sRGB。亮度是显式参数，字底对比度由构造方式保证。
func hcl(h, c, l float64) color.RGBA {
	r, g, b := colorful.Hcl(h, c, l).Clamped().RGB255()
	return color.RGBA{R: r, G: g, B: b, A: 255}
}

func frac(v float64) float64 {
	v = math.Mod(v, 1)
	if v < 0 {
		v += 1
	}
	return v
}

func clampFloat(v, lo, hi float64) float64 {
	if hi < lo {
		return (lo + hi) / 2
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ────────────────────── 仿射变换（f64.Aff3：行主序 2×3，源 → 目标） ──────────────────────

func affTranslate(tx, ty float64) f64.Aff3 {
	return f64.Aff3{1, 0, tx, 0, 1, ty}
}

func affScale(sx, sy float64) f64.Aff3 {
	return f64.Aff3{sx, 0, 0, 0, sy, 0}
}

func affRotate(a float64) f64.Aff3 {
	s, c := math.Sincos(a)
	return f64.Aff3{c, -s, 0, s, c, 0}
}

func affShearX(k float64) f64.Aff3 {
	return f64.Aff3{1, k, 0, 0, 1, 0}
}

func affMul(p, q f64.Aff3) f64.Aff3 {
	return f64.Aff3{
		p[0]*q[0] + p[1]*q[3],
		p[0]*q[1] + p[1]*q[4],
		p[0]*q[2] + p[1]*q[5] + p[2],
		p[3]*q[0] + p[4]*q[3],
		p[3]*q[1] + p[4]*q[4],
		p[3]*q[2] + p[4]*q[5] + p[5],
	}
}
