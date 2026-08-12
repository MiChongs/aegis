package gifcaptcha

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"image/png"
	mrand "math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fogleman/gg"
)

func TestGenerateProducesAnimatedGIF(t *testing.T) {
	opts := DefaultOptions()
	res, err := Generate(opts)
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	if res.MimeType != "image/gif" {
		t.Fatalf("MimeType = %q，应为 image/gif", res.MimeType)
	}
	if len(res.Answer) != opts.Length {
		t.Fatalf("答案长度 = %d，应为 %d", len(res.Answer), opts.Length)
	}

	anim, err := gif.DecodeAll(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatalf("产物不是合法 GIF: %v", err)
	}
	if len(anim.Image) != opts.Frames {
		t.Fatalf("帧数 = %d，应为 %d", len(anim.Image), opts.Frames)
	}
	if anim.LoopCount != 0 {
		t.Fatalf("LoopCount = %d，应为 0（无限循环）", anim.LoopCount)
	}
	for i, d := range anim.Delay {
		// 低于 2 的延迟会被浏览器统一改判成 100ms，配置值就此失效。
		if d < 2 {
			t.Fatalf("第 %d 帧延迟 = %d（1/100 秒），必须 ≥ 2", i, d)
		}
	}
	for i, frame := range anim.Image {
		if got := frame.Bounds().Dx(); got != opts.Width {
			t.Fatalf("第 %d 帧宽度 = %d，应为 %d", i, got, opts.Width)
		}
		if got := frame.Bounds().Dy(); got != opts.Height {
			t.Fatalf("第 %d 帧高度 = %d，应为 %d", i, got, opts.Height)
		}
	}

	// 全局色表标志位：所有帧共用一张调色板
	if len(res.Data) < 11 || res.Data[10]&0x80 == 0 {
		t.Fatal("GIF 没有写全局色表，说明帧调色板不一致")
	}
	t.Logf("默认参数产物：%d 帧 / %d 字节 / 一轮 %s", len(anim.Image), len(res.Data), res.Duration())
}

// TestFramesAreNotStatic 每一帧都必须真的不一样：静态图套进 GIF 容器
// 同样能通过「是合法 GIF」「有 N 帧」，只有逐帧比对挡得住。
func TestFramesAreNotStatic(t *testing.T) {
	res, err := Generate(DefaultOptions())
	if err != nil {
		t.Fatalf("生成失败: %v", err)
	}
	anim, err := gif.DecodeAll(bytes.NewReader(res.Data))
	if err != nil {
		t.Fatalf("解码失败: %v", err)
	}

	frames := flatten(anim)
	for i := 1; i < len(frames); i++ {
		ratio := diffRatio(frames[i-1], frames[i])
		if ratio < 0.02 {
			t.Fatalf("第 %d 帧与前一帧只有 %.4f 的像素不同，这不叫动态", i, ratio)
		}
	}
	// 首尾也必须不同
	if ratio := diffRatio(frames[0], frames[len(frames)-1]); ratio < 0.02 {
		t.Fatalf("首帧与末帧只有 %.4f 的像素不同", ratio)
	}
}

// TestAnimationLoopsSeamlessly 无限循环播放时 t=1 必须回到 t=0 的画面，否则交界处闪一下
func TestAnimationLoopsSeamlessly(t *testing.T) {
	opts := DefaultOptions().Normalize()
	rng := mrand.New(mrand.NewPCG(20260813, 42))
	sc, err := buildScene(opts, "AB34C", rng)
	if err != nil {
		t.Fatalf("构造场景失败: %v", err)
	}

	first := renderSingleFrame(sc, 0)
	wrapped := renderSingleFrame(sc, 1) // 下一轮的第一帧
	next := renderSingleFrame(sc, 1/float64(opts.Frames))

	if got := diffRatio(first, wrapped); got > 0.01 {
		t.Fatalf("循环回到起点时有 %.4f 的像素不同，动画会在每轮交界处跳变", got)
	}
	if got := diffRatio(first, next); got < 0.02 {
		t.Fatalf("相邻帧只有 %.4f 的像素不同，动画幅度过小", got)
	}
}

func TestAnswerUsesUnambiguousCharset(t *testing.T) {
	for _, mode := range []Mode{ModeAlnum, ModeAlpha, ModeDigit} {
		charset := mode.charset()
		if charset == "" {
			t.Fatalf("%s 档没有字符集", mode)
		}
		seen := map[rune]bool{}
		for _, r := range charset {
			if seen[r] {
				t.Fatalf("%s 档字符集里 %q 重复，会让该字符出现概率翻倍", mode, r)
			}
			seen[r] = true
		}
		if mode != ModeDigit {
			for _, bad := range "OIL01258BSZ" {
				if strings.ContainsRune(charset, bad) {
					t.Fatalf("%s 档字符集不该含易混字符 %q", mode, bad)
				}
			}
		}

		opts := DefaultOptions()
		opts.Mode = mode
		res, err := Generate(opts)
		if err != nil {
			t.Fatalf("%s 档生成失败: %v", mode, err)
		}
		for _, r := range res.Answer {
			if !strings.ContainsRune(charset, r) {
				t.Fatalf("%s 档答案里出现了字符集之外的 %q", mode, r)
			}
		}
	}
}

func TestNormalizeClampsEveryOption(t *testing.T) {
	got := Options{
		Width: 5, Height: 5, Length: 99, Frames: 999,
		FrameDelay: time.Microsecond, Mode: "不存在的档位", Noise: 500, Wobble: 500,
	}.Normalize()

	if got.Width != MinWidth || got.Height != MinHeight {
		t.Fatalf("尺寸未夹到下限: %dx%d", got.Width, got.Height)
	}
	if got.Length != MaxLength {
		t.Fatalf("长度未夹到上限: %d", got.Length)
	}
	if got.FrameDelay != MinFrameDelay {
		t.Fatalf("帧间隔未夹到下限: %s", got.FrameDelay)
	}
	if got.Mode != ModeAlnum {
		t.Fatalf("未知档位应回落到 alnum，实际 %q", got.Mode)
	}
	if got.Noise != 100 || got.Wobble != 100 {
		t.Fatalf("强度未夹到上限: noise=%d wobble=%d", got.Noise, got.Wobble)
	}
	if got != got.Normalize() {
		t.Fatal("Normalize 必须幂等")
	}

	// 像素预算
	big := Options{Width: MaxWidth, Height: MaxHeight, Frames: MaxFrames}.Normalize()
	if pixels := big.Width * big.Height * big.Frames; pixels > MaxPixelBudget {
		t.Fatalf("超出像素预算: %d > %d", pixels, MaxPixelBudget)
	}
	if big.Frames < MinFrames {
		t.Fatalf("减帧不能减到下限以下: %d", big.Frames)
	}
}

// TestFontsAreEmbedded 字形全部来自内嵌字体，不读系统字体目录
func TestFontsAreEmbedded(t *testing.T) {
	fonts, err := loadFonts()
	if err != nil {
		t.Fatalf("内嵌字体解析失败: %v", err)
	}
	if len(fonts) != len(builtinFontBlobs) {
		t.Fatalf("字体数 = %d，应为 %d", len(fonts), len(builtinFontBlobs))
	}

	// 每款字体都要能画出全部字符：缺字形在运行时是一个空白格子
	all := charsetAlnum + charsetDigit
	for i, f := range fonts {
		face, err := newFace(f, 40)
		if err != nil {
			t.Fatalf("字体 #%d 创建句柄失败: %v", i, err)
		}
		for _, r := range all {
			mask, err := renderGlyph(face, r)
			if err != nil {
				t.Fatalf("字体 #%d 渲染 %q 失败: %v", i, r, err)
			}
			if !hasInk(mask) {
				t.Fatalf("字体 #%d 渲染 %q 得到空白蒙版", i, r)
			}
		}
		if err := face.Close(); err != nil {
			t.Fatalf("字体 #%d 释放失败: %v", i, err)
		}
	}
}

func TestPaletteFitsGIFAndQuantizesCloseEnough(t *testing.T) {
	if len(sharedPalette) != 256 {
		t.Fatalf("调色板 %d 色，GIF 上限是 256", len(sharedPalette))
	}
	// 立方体步进 51，最坏误差是半步（26）
	for _, c := range []struct{ r, g, b uint8 }{
		{0, 0, 0}, {255, 255, 255}, {12, 200, 77}, {128, 128, 128}, {250, 12, 3}, {33, 33, 40},
	} {
		got := sharedPalette[paletteIndex(c.r, c.g, c.b)]
		gr, gg, gb, _ := got.RGBA()
		if delta := maxDelta(c.r, c.g, c.b, uint8(gr>>8), uint8(gg>>8), uint8(gb>>8)); delta > 26 {
			t.Fatalf("(%d,%d,%d) 量化误差 %d 过大", c.r, c.g, c.b, delta)
		}
	}
}

func TestGeneratedCaptchasDiffer(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 8; i++ {
		res, err := Generate(DefaultOptions())
		if err != nil {
			t.Fatalf("生成失败: %v", err)
		}
		if seen[res.Answer] {
			t.Logf("答案 %q 重复（%d 位字符集下属正常范围）", res.Answer, DefaultLength)
		}
		seen[res.Answer] = true
	}
	if len(seen) < 6 {
		t.Fatalf("8 次生成只得到 %d 个不同答案，随机性存疑", len(seen))
	}
}

// TestDumpDynamicCaptchaPreview 人工核对用（默认跳过）：
//
//	AEGIS_CAPTCHA_PREVIEW_DIR=./preview go test ./pkg/gifcaptcha -run TestDumpDynamicCaptchaPreview
//
// 产出每个样本的 .gif 与一张逐帧拼起来的 .png。
func TestDumpDynamicCaptchaPreview(t *testing.T) {
	dir := os.Getenv("AEGIS_CAPTCHA_PREVIEW_DIR")
	if dir == "" {
		t.Skip("设置 AEGIS_CAPTCHA_PREVIEW_DIR 后运行")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	for i := 0; i < 4; i++ {
		res, err := Generate(DefaultOptions())
		if err != nil {
			t.Fatalf("生成失败: %v", err)
		}
		name := filepath.Join(dir, fmt.Sprintf("captcha-%d-%s", i, res.Answer))
		if err := os.WriteFile(name+".gif", res.Data, 0o644); err != nil {
			t.Fatalf("写 GIF 失败: %v", err)
		}

		anim, err := gif.DecodeAll(bytes.NewReader(res.Data))
		if err != nil {
			t.Fatalf("解码失败: %v", err)
		}
		frames := flatten(anim)
		const cols = 4
		rows := (len(frames) + cols - 1) / cols
		w := frames[0].Bounds().Dx()
		h := frames[0].Bounds().Dy()
		sheet := image.NewRGBA(image.Rect(0, 0, w*cols, h*rows))
		for idx, frame := range frames {
			at := image.Pt((idx%cols)*w, (idx/cols)*h)
			draw.Draw(sheet, image.Rectangle{Min: at, Max: at.Add(image.Pt(w, h))}, frame, frame.Bounds().Min, draw.Src)
		}
		f, err := os.Create(name + ".png")
		if err != nil {
			t.Fatalf("建文件失败: %v", err)
		}
		err = png.Encode(f, sheet)
		_ = f.Close()
		if err != nil {
			t.Fatalf("写 PNG 失败: %v", err)
		}
		t.Logf("答案 %s → %s.gif", res.Answer, name)
	}
}

func BenchmarkGenerate(b *testing.B) {
	opts := DefaultOptions()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		res, err := Generate(opts)
		if err != nil {
			b.Fatal(err)
		}
		b.SetBytes(int64(len(res.Data)))
	}
}

// ────────────────────── 测试辅助 ──────────────────────

// renderSingleFrame 只画一帧（含扭曲）
func renderSingleFrame(sc *scene, t float64) *image.RGBA {
	bounds := image.Rect(0, 0, sc.opts.Width, sc.opts.Height)
	canvas := image.NewRGBA(bounds)
	warped := image.NewRGBA(bounds)
	dc := gg.NewContextForRGBA(canvas)
	dc.SetLineCapRound()
	dc.SetLineJoinRound()
	drawFrame(dc, canvas, sc, t)
	applyWarp(warped, canvas, sc.warp, t)
	return warped
}

// flatten 把 GIF 各帧铺回完整画布
func flatten(anim *gif.GIF) []*image.RGBA {
	out := make([]*image.RGBA, 0, len(anim.Image))
	for _, frame := range anim.Image {
		rgba := image.NewRGBA(frame.Bounds())
		draw.Draw(rgba, frame.Bounds(), frame, frame.Bounds().Min, draw.Src)
		out = append(out, rgba)
	}
	return out
}

// diffRatio 两张图中肉眼可见地不同的像素占比
func diffRatio(a, b *image.RGBA) float64 {
	if a.Bounds() != b.Bounds() {
		return 1
	}
	total := a.Bounds().Dx() * a.Bounds().Dy()
	if total == 0 {
		return 0
	}
	changed := 0
	for i := 0; i+3 < len(a.Pix) && i+3 < len(b.Pix); i += 4 {
		if maxDelta(a.Pix[i], a.Pix[i+1], a.Pix[i+2], b.Pix[i], b.Pix[i+1], b.Pix[i+2]) > 12 {
			changed++
		}
	}
	return float64(changed) / float64(total)
}

func maxDelta(r1, g1, b1, r2, g2, b2 uint8) int {
	d := absDiff(r1, r2)
	if v := absDiff(g1, g2); v > d {
		d = v
	}
	if v := absDiff(b1, b2); v > d {
		d = v
	}
	return d
}

func absDiff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

func hasInk(img *image.RGBA) bool {
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] > 32 {
			return true
		}
	}
	return false
}
