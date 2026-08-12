package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"path"
	"strings"

	avatardomain "aegis/internal/domain/avatar"
	apperrors "aegis/pkg/errors"

	blurhash "github.com/bbrks/go-blurhash"
	"github.com/disintegration/imaging"

	// webp 只有解码实现（x/image 不提供编码器），注册进 image 的格式表即可让
	// image.DecodeConfig / imaging.Decode 认得它。不注册的表现是用户传了
	// 一张 .webp，服务端报「格式不支持」——而那正是 iOS 相册导出的默认格式之一。
	_ "golang.org/x/image/webp"
)

// 头像处理管线。
//
// 收进来的是**用户设备上的任意一张图**：可能是竖拍但 EXIF 标着旋转的手机照片、
// 可能带 GPS 坐标、可能是 12000×9000 的原图、也可能是一个改了后缀名的 zip。
// 出去的必须是一批尺寸确定、无元数据、能直接当头像用的图。中间这一段的每一步
// 都有一个「不做会怎样」：
//
//	不读 EXIF 方向  → 所有竖拍的自拍在网页上是躺着的
//	不重新编码      → 头像里带着拍摄地点的经纬度，谁下载谁就知道用户家在哪
//	不限像素总数    → 一张 32000×32000 的 PNG 解出来是 4GB，一个请求打死一台机器
//	不出多尺寸      → 列表页每一行都在下载 512×512
//	不算 blurhash   → 弱网下头像位置先空着再"跳"出来
const (
	// avatarMaxPixels 解码前的像素总数闸门。
	// 5000 万像素约等于 200MB 的 RGBA 内存，已经远超任何真实头像的需要；
	// 判定用 DecodeConfig，**在真正解码之前**做，否则闸门就开在爆炸之后了。
	avatarMaxPixels = 50 << 20
	// avatarMaxSide 单边像素上限，挡住 1×100000 这种绕开总数闸门的形状。
	avatarMaxSide = 20000
	// avatarAnimatedMaxBytes 动图保留原样的体积上限，超过则拍平成静态首帧。
	// 一张 8MB 的动图当头像，意味着每个打开列表页的人都要下载 8MB。
	avatarAnimatedMaxBytes = 2 << 20
	// avatarBlurhashSampleSide blurhash 编码前的降采样边长。
	// 编码复杂度与像素数成正比，而 blurhash 本身只有 4×3 个分量 ——
	// 拿 512×512 去算和拿 32×32 去算得到的哈希肉眼无差别，耗时差两个量级。
	avatarBlurhashSampleSide = 32
)

// renderedAvatarImage 一个尺寸档的编码产物。
type renderedAvatarImage struct {
	Size        int // 0 表示归一化后的原图
	Ext         string
	ContentType string
	Data        []byte
}

// processedAvatar 一次上传处理完的全部产物。
type processedAvatar struct {
	Base          renderedAvatarImage
	Variants      []renderedAvatarImage
	Width         int
	Height        int
	Checksum      string
	Blurhash      string
	DominantColor string
	Animated      bool
	// Flattened 动图因为超限被拍平成静态图。上传结果里如实告知，
	// 否则用户只会看到"我传的动图怎么不动了"而无从判断是不是坏了。
	Flattened bool
}

// avatarEncodeOptions 编码策略，由配置注入。
type avatarEncodeOptions struct {
	// JPEGQuality 无透明通道时的 JPEG 质量。
	JPEGQuality int
	// KeepAnimated 允许保留动图原样。关掉时一律拍平。
	KeepAnimated bool
	// Sizes 要生成的尺寸档。
	Sizes []int
}

// processAvatarImage 把上传的原始字节变成一批可直接投产的头像。
//
// content 必须能读完 —— 图像处理没法流式做（缩放要随机访问像素），
// 因此调用方要先用 avatarMaxUploadSize 把体积挡在外面。
func processAvatarImage(raw []byte, fileName string, opts avatardomain.UploadOptions, enc avatarEncodeOptions) (*processedAvatar, error) {
	if len(raw) == 0 {
		return nil, apperrors.New(40087, http.StatusBadRequest, "头像文件不能为空")
	}

	// ① 先只读文件头。DecodeConfig 不分配像素缓冲，是唯一能在"解码炸弹"
	//    真正展开之前拿到尺寸的办法。
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, apperrors.New(40089, http.StatusBadRequest, "头像必须是有效的图片文件（支持 JPG / PNG / GIF / WEBP）")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, apperrors.New(40089, http.StatusBadRequest, "头像图片尺寸无效")
	}
	if cfg.Width > avatarMaxSide || cfg.Height > avatarMaxSide || int64(cfg.Width)*int64(cfg.Height) > avatarMaxPixels {
		return nil, apperrors.New(40091, http.StatusBadRequest,
			fmt.Sprintf("头像图片分辨率过大（%d×%d），请先压缩后再上传", cfg.Width, cfg.Height))
	}

	// ② 动图判定要在解码之前做：image.Decode 对 GIF 只会给出第一帧，
	//    到那一步就再也分不出"单帧 GIF"和"动图"了。
	animated := false
	if format == "gif" {
		if frames, err := gif.DecodeAll(bytes.NewReader(raw)); err == nil && len(frames.Image) > 1 {
			animated = true
		}
	}

	// ③ 解码并按 EXIF 方向纠正。AutoOrientation 之后的坐标系才是用户
	//    在自己手机上看到的那个，客户端传来的裁剪框也是按那个画的。
	decoded, err := imaging.Decode(bytes.NewReader(raw), imaging.AutoOrientation(true))
	if err != nil {
		return nil, apperrors.New(40089, http.StatusBadRequest, "头像图片无法解析，请换一张试试")
	}

	square := cropAvatarSquare(decoded, opts.Crop)
	bounds := square.Bounds()
	side := bounds.Dx()

	result := &processedAvatar{Width: side, Height: side}

	// ④ blurhash 与主色都在方图上算，这样客户端拿占位色块去填的正是
	//    最终会显示的那块区域。
	sample := imaging.Resize(square, avatarBlurhashSampleSide, avatarBlurhashSampleSide, imaging.Box)
	if hash, err := blurhash.Encode(4, 3, sample); err == nil {
		result.Blurhash = hash
	}
	result.DominantColor = dominantAvatarColor(sample)

	sizes := normalizeAvatarSizes(enc.Sizes)
	baseSide := side
	if baseSide > avatardomain.MaxRenderSize {
		baseSide = avatardomain.MaxRenderSize
	}

	// ⑤ 动图：够小就原样留着（否则用户传的动图会莫名其妙变成静态），
	//    超限则拍平并如实上报。
	if animated && enc.KeepAnimated && len(raw) <= avatarAnimatedMaxBytes &&
		cfg.Width <= avatardomain.MaxRenderSize && cfg.Height <= avatardomain.MaxRenderSize {
		result.Animated = true
		result.Base = renderedAvatarImage{Ext: ".gif", ContentType: "image/gif", Data: raw}
		result.Width, result.Height = cfg.Width, cfg.Height
	} else {
		result.Flattened = animated
		base, err := encodeAvatarImage(imaging.Resize(square, baseSide, baseSide, imaging.Lanczos), enc.JPEGQuality)
		if err != nil {
			return nil, err
		}
		result.Base = base
		result.Width, result.Height = baseSide, baseSide
	}

	sum := sha256.Sum256(result.Base.Data)
	result.Checksum = hex.EncodeToString(sum[:])

	// ⑥ 各尺寸档一律从方图重采样，而不是从上一档链式缩小 ——
	//    链式缩小会把每一次重采样的损失累加到最小的那一档上，
	//    而最小的那一档恰恰是列表页里出现最多次的。
	for _, size := range sizes {
		item, err := encodeAvatarImage(imaging.Resize(square, size, size, imaging.Lanczos), enc.JPEGQuality)
		if err != nil {
			return nil, err
		}
		item.Size = size
		result.Variants = append(result.Variants, item)
	}
	_ = fileName
	return result, nil
}

// cropAvatarSquare 把任意长宽比的图变成方图。
//
// 客户端给了裁剪框就用它（先夹回图像范围内 —— 越界的框来自前端的
// 缩放换算误差，直接用会 panic），没给就居中取最大的方形。
func cropAvatarSquare(img image.Image, crop *avatardomain.CropRect) image.Image {
	bounds := img.Bounds()
	if !crop.Empty() {
		rect := image.Rect(
			bounds.Min.X+crop.X,
			bounds.Min.Y+crop.Y,
			bounds.Min.X+crop.X+crop.Width,
			bounds.Min.Y+crop.Y+crop.Height,
		).Intersect(bounds)
		if rect.Dx() > 0 && rect.Dy() > 0 {
			cropped := imaging.Crop(img, rect)
			side := cropped.Bounds().Dx()
			if cropped.Bounds().Dy() < side {
				side = cropped.Bounds().Dy()
			}
			return imaging.CropAnchor(cropped, side, side, imaging.Center)
		}
	}
	side := bounds.Dx()
	if bounds.Dy() < side {
		side = bounds.Dy()
	}
	return imaging.CropAnchor(img, side, side, imaging.Center)
}

// encodeAvatarImage 按有没有透明通道决定编码格式。
//
// 一律 JPEG 会把带透明的 PNG 头像的背景压成黑块；一律 PNG 会让一张
// 普通自拍从 40KB 涨到 400KB。判据只能是像素本身 —— 上传时声明的
// content-type 和文件后缀都可能与内容不符。
func encodeAvatarImage(img image.Image, quality int) (renderedAvatarImage, error) {
	var buf bytes.Buffer
	if avatarImageHasAlpha(img) {
		encoder := png.Encoder{CompressionLevel: png.BestCompression}
		if err := encoder.Encode(&buf, img); err != nil {
			return renderedAvatarImage{}, apperrors.New(50085, http.StatusInternalServerError, "头像编码失败")
		}
		return renderedAvatarImage{Ext: ".png", ContentType: "image/png", Data: buf.Bytes()}, nil
	}
	if quality <= 0 || quality > 100 {
		quality = 88
	}
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return renderedAvatarImage{}, apperrors.New(50085, http.StatusInternalServerError, "头像编码失败")
	}
	return renderedAvatarImage{Ext: ".jpg", ContentType: "image/jpeg", Data: buf.Bytes()}, nil
}

// avatarImageHasAlpha 图像里是否真的有非不透明像素。
//
// 与缩略图那条链路上的 imageHasAlpha 刻意不同：那边用 Opaque() 做保守估计
// （猜错只是多编一张 PNG），头像这边每一张都要落库长期存着，
// 值得为此逐像素扫一遍拿到确切答案。
//
// 不能只看类型：imaging 的产物恒为 *image.NRGBA，按类型判断的话每一张头像
// 都会被当成带透明通道，全部编码成 PNG。
func avatarImageHasAlpha(img image.Image) bool {
	bounds := img.Bounds()
	if nrgba, ok := img.(*image.NRGBA); ok {
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			row := nrgba.Pix[(y-nrgba.Rect.Min.Y)*nrgba.Stride:]
			for x := 0; x < bounds.Dx(); x++ {
				if row[x*4+3] != 0xff {
					return true
				}
			}
		}
		return false
	}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a != 0xffff {
				return true
			}
		}
	}
	return false
}

// dominantAvatarColor 取一个能代表这张头像的颜色，形如 #RRGGBB。
//
// 用**加权平均**而不是最频繁色：头像的背景往往占面积最大但信息最少
// （一面白墙），取频次最高的颜色会让几乎所有头像的主色都是灰白。
// 这里按饱和度给权重，让"那件红衣服"压过"白墙"。
func dominantAvatarColor(img image.Image) string {
	bounds := img.Bounds()
	var sumR, sumG, sumB, sumW float64
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r16, g16, b16, a16 := img.At(x, y).RGBA()
			if a16 == 0 {
				continue
			}
			r, g, b := float64(r16>>8), float64(g16>>8), float64(b16>>8)
			maxC, minC := r, r
			for _, c := range []float64{g, b} {
				if c > maxC {
					maxC = c
				}
				if c < minC {
					minC = c
				}
			}
			// 权重 = 饱和度 + 一个底数，底数保证纯灰度图也能算出结果
			weight := (maxC-minC)/255.0 + 0.15
			sumR += r * weight
			sumG += g * weight
			sumB += b * weight
			sumW += weight
		}
	}
	if sumW == 0 {
		return "#9ca3af"
	}
	return fmt.Sprintf("#%02x%02x%02x",
		clampColorChannel(sumR/sumW), clampColorChannel(sumG/sumW), clampColorChannel(sumB/sumW))
}

func clampColorChannel(value float64) uint8 {
	if value <= 0 {
		return 0
	}
	if value >= 255 {
		return 255
	}
	return uint8(value + 0.5)
}

// normalizeAvatarSizes 去重、排序、剔除越界值。
//
// 配置项直接拿来用是不行的：重复的档会重复上传同一张图，
// 大于 MaxRenderSize 的档会把小图放大存一份（比原图还大且更糊）。
func normalizeAvatarSizes(sizes []int) []int {
	if len(sizes) == 0 {
		sizes = avatardomain.StandardSizes
	}
	seen := make(map[int]bool, len(sizes))
	result := make([]int, 0, len(sizes))
	for _, size := range sizes {
		if size <= 0 || size > avatardomain.MaxRenderSize || seen[size] {
			continue
		}
		seen[size] = true
		result = append(result, size)
	}
	for i := 1; i < len(result); i++ {
		for j := i; j > 0 && result[j] < result[j-1]; j-- {
			result[j], result[j-1] = result[j-1], result[j]
		}
	}
	if len(result) == 0 {
		return []int{avatardomain.DefaultRenderSize}
	}
	return result
}

// readAvatarUpload 把上传流读进内存，并在超过上限时给出**可执行**的错误。
//
// 用 LimitReader 多读一个字节来判超限：先读完再看长度，等于把攻击者
// 声明的 Content-Length 当真 —— 那个值可以是假的。
func readAvatarUpload(content io.Reader, limit int64) ([]byte, error) {
	if content == nil {
		return nil, apperrors.New(40087, http.StatusBadRequest, "头像文件不能为空")
	}
	if limit <= 0 {
		limit = avatarMaxUploadSize
	}
	raw, err := io.ReadAll(io.LimitReader(content, limit+1))
	if err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, "读取上传文件失败")
	}
	if int64(len(raw)) > limit {
		return nil, apperrors.New(40088, http.StatusBadRequest,
			fmt.Sprintf("头像文件不能超过 %dMB", limit>>20))
	}
	if len(raw) == 0 {
		return nil, apperrors.New(40087, http.StatusBadRequest, "头像文件不能为空")
	}
	return raw, nil
}

// avatarVariantKey 由基准键派生某个尺寸档的对象键。
//
// 派生而不是各存一个随机键：这样即便资产表那一行丢了，
// 光凭基准键也能把所有变体找回来。
func avatarVariantKey(baseKey string, size int, ext string) string {
	trimmed := strings.TrimSuffix(baseKey, path.Ext(baseKey))
	return fmt.Sprintf("%s_%d%s", trimmed, size, ext)
}
