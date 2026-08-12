package gifcaptcha

import (
	"image"
	"image/color"
)

// GIF 只认调色板图。固定调色板 = 216 色立方体 + 40 级灰阶：
// 所有帧共用一张全局色表（文件更小），且索引可直接算出来
// —— color.Palette.Index 是 O(像素 × 256) 的最近色搜索，在登录热路径上太贵。

const (
	cubeLevels = 6
	cubeSize   = cubeLevels * cubeLevels * cubeLevels // 216
	grayLevels = 40
	grayBase   = cubeSize
	// grayThreshold 三通道极差小于它走灰阶档（步进 ~6.5，比立方体的 51 细，抗锯齿边缘不出台阶）
	grayThreshold = 10
)

// sharedPalette 全局共享调色板，恰好 256 色。
var sharedPalette = buildPalette()

func buildPalette() color.Palette {
	pal := make(color.Palette, 0, cubeSize+grayLevels)
	for r := 0; r < cubeLevels; r++ {
		for g := 0; g < cubeLevels; g++ {
			for b := 0; b < cubeLevels; b++ {
				pal = append(pal, color.RGBA{
					R: uint8(r * 51), G: uint8(g * 51), B: uint8(b * 51), A: 255,
				})
			}
		}
	}
	for i := 0; i < grayLevels; i++ {
		v := uint8(i * 255 / (grayLevels - 1))
		pal = append(pal, color.RGBA{R: v, G: v, B: v, A: 255})
	}
	return pal
}

// paletteIndex 由 RGB 直接算出调色板下标。
func paletteIndex(r, g, b uint8) uint8 {
	mx, mn := r, r
	if g > mx {
		mx = g
	}
	if b > mx {
		mx = b
	}
	if g < mn {
		mn = g
	}
	if b < mn {
		mn = b
	}
	if mx-mn <= grayThreshold {
		v := (int(r) + int(g) + int(b)) / 3
		return uint8(grayBase + v*(grayLevels-1)/255)
	}
	return uint8(cubeLevel(r)*cubeLevels*cubeLevels + cubeLevel(g)*cubeLevels + cubeLevel(b))
}

// cubeLevel 把 0-255 四舍五入到 6 个等距档位之一。
func cubeLevel(v uint8) int {
	return (int(v)*(cubeLevels-1) + 127) / 255
}

// quantize 把一帧不透明 RGBA 压成索引图
func quantize(dst *image.Paletted, src *image.RGBA) {
	b := src.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		si := src.PixOffset(b.Min.X, y)
		di := dst.PixOffset(b.Min.X, y)
		for x := b.Min.X; x < b.Max.X; x++ {
			dst.Pix[di] = paletteIndex(src.Pix[si], src.Pix[si+1], src.Pix[si+2])
			si += 4
			di++
		}
	}
}
