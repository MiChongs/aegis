package gifcaptcha

import (
	"fmt"
	"image"
	"image/color"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomedium"
	"golang.org/x/image/font/gofont/gomediumitalic"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/gofont/gosmallcaps"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// 六款内嵌字体（随包发布的 []byte，不是磁盘文件）。逐字符随机挑一款，
// 同一串里字形宽窄粗细不同，模板匹配更难。
var builtinFontBlobs = [][]byte{
	goregular.TTF,
	gobold.TTF,
	gomedium.TTF,
	gosmallcaps.TTF,
	goitalic.TTF,
	gomediumitalic.TTF,
}

var (
	fontsOnce sync.Once
	fonts     []*sfnt.Font
	fontsErr  error
)

// loadFonts 解析内嵌字体，进程内只做一次。
// *sfnt.Font 可并发共用（各 Face 自带 Buffer），因此每次渲染新建 Face 而不缓存 Face。
func loadFonts() ([]*sfnt.Font, error) {
	fontsOnce.Do(func() {
		parsed := make([]*sfnt.Font, 0, len(builtinFontBlobs))
		for i, blob := range builtinFontBlobs {
			f, err := opentype.Parse(blob)
			if err != nil {
				fontsErr = fmt.Errorf("gifcaptcha: 解析内嵌字体 #%d 失败: %w", i, err)
				return
			}
			parsed = append(parsed, f)
		}
		fonts = parsed
	})
	return fonts, fontsErr
}

// newFace 按字号造字形句柄。Hinting=None：字形随后要旋转缩放，网格对齐会让粗细逐帧跳变。
func newFace(f *sfnt.Font, size float64) (font.Face, error) {
	return opentype.NewFace(f, &opentype.FaceOptions{
		Size:    size,
		DPI:     72,
		Hinting: font.HintingNone,
	})
}

// renderGlyph 把字符画成白色预乘 RGBA 蒙版。只光栅化一次，之后每帧只上色 + 仿射变换。
func renderGlyph(face font.Face, r rune) (*image.RGBA, error) {
	s := string(r)
	bounds, _ := font.BoundString(face, s)

	const pad = 4 // 给抗锯齿边缘与重采样留余量
	w := (bounds.Max.X - bounds.Min.X).Ceil() + 2*pad
	h := (bounds.Max.Y - bounds.Min.Y).Ceil() + 2*pad
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("gifcaptcha: 字符 %q 没有可渲染的字形", s)
	}

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	d := font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(color.White),
		Face: face,
		Dot: fixed.Point26_6{
			X: fixed.I(pad) - bounds.Min.X,
			Y: fixed.I(pad) - bounds.Min.Y,
		},
	}
	d.DrawString(s)
	return img, nil
}
