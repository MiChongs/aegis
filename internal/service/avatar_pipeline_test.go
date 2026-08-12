package service

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"

	avatardomain "aegis/internal/domain/avatar"
)

func encodeTestJPEG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("编码测试图失败：%v", err)
	}
	return buf.Bytes()
}

func encodeTestPNGWithAlpha(t *testing.T, side int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, side, side))
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			alpha := uint8(255)
			if x < side/2 {
				alpha = 0
			}
			img.Set(x, y, color.NRGBA{R: 200, G: 30, B: 30, A: alpha})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("编码测试图失败：%v", err)
	}
	return buf.Bytes()
}

func defaultTestEncodeOptions() avatarEncodeOptions {
	return avatarEncodeOptions{JPEGQuality: 85, KeepAnimated: true, Sizes: avatardomain.StandardSizes}
}

// 非方图必须被裁成方图：头像位一律是正方形或圆形，
// 交一张 800×450 给客户端等于让每一端各自决定裁哪一块。
func TestProcessAvatarProducesSquareVariants(t *testing.T) {
	result, err := processAvatarImage(encodeTestJPEG(t, 800, 450), "photo.jpg",
		avatardomain.UploadOptions{}, defaultTestEncodeOptions())
	if err != nil {
		t.Fatalf("处理失败：%v", err)
	}
	if result.Width != result.Height {
		t.Fatalf("原图变体不是方的：%d×%d", result.Width, result.Height)
	}
	if result.Width > avatardomain.MaxRenderSize {
		t.Fatalf("原图变体超过上限：%d", result.Width)
	}
	if len(result.Variants) != len(avatardomain.StandardSizes) {
		t.Fatalf("变体数量 %d，期望 %d", len(result.Variants), len(avatardomain.StandardSizes))
	}
	for _, variant := range result.Variants {
		cfg, _, err := image.DecodeConfig(bytes.NewReader(variant.Data))
		if err != nil {
			t.Fatalf("%d 档解不开：%v", variant.Size, err)
		}
		if cfg.Width != variant.Size || cfg.Height != variant.Size {
			t.Fatalf("%d 档实际是 %d×%d", variant.Size, cfg.Width, cfg.Height)
		}
	}
	if result.Checksum == "" {
		t.Fatal("缺少内容摘要，版本号无从派生")
	}
	if result.Blurhash == "" {
		t.Fatal("缺少 blurhash")
	}
	if len(result.DominantColor) != 7 || result.DominantColor[0] != '#' {
		t.Fatalf("主色格式不对：%q", result.DominantColor)
	}
}

// 带透明通道的图必须编成 PNG。编成 JPEG 的话透明区会变成黑块 ——
// 这正是"传了个透明底的 logo，结果头像四角是黑的"那类报障。
func TestProcessAvatarKeepsAlphaAsPNG(t *testing.T) {
	result, err := processAvatarImage(encodeTestPNGWithAlpha(t, 300), "logo.png",
		avatardomain.UploadOptions{}, defaultTestEncodeOptions())
	if err != nil {
		t.Fatalf("处理失败：%v", err)
	}
	if result.Base.ContentType != "image/png" {
		t.Fatalf("带透明的图被编成了 %s", result.Base.ContentType)
	}
	for _, variant := range result.Variants {
		if variant.ContentType != "image/png" {
			t.Fatalf("%d 档被编成了 %s", variant.Size, variant.ContentType)
		}
	}
}

// 不带透明的照片必须编成 JPEG：一张普通自拍编成 PNG 会从 40KB 涨到 400KB，
// 而头像是页面上出现次数最多的图片。
func TestProcessAvatarEncodesOpaqueAsJPEG(t *testing.T) {
	result, err := processAvatarImage(encodeTestJPEG(t, 400, 400), "photo.jpg",
		avatardomain.UploadOptions{}, defaultTestEncodeOptions())
	if err != nil {
		t.Fatalf("处理失败：%v", err)
	}
	if result.Base.ContentType != "image/jpeg" {
		t.Fatalf("不透明图被编成了 %s", result.Base.ContentType)
	}
}

// 裁剪框越界不能 panic。越界值来自前端的缩放换算误差，是常态而非攻击。
func TestProcessAvatarClampsOutOfBoundsCrop(t *testing.T) {
	result, err := processAvatarImage(encodeTestJPEG(t, 200, 200), "photo.jpg",
		avatardomain.UploadOptions{Crop: &avatardomain.CropRect{X: 150, Y: 150, Width: 500, Height: 500}},
		defaultTestEncodeOptions())
	if err != nil {
		t.Fatalf("处理失败：%v", err)
	}
	if result.Width <= 0 || result.Width != result.Height {
		t.Fatalf("越界裁剪后尺寸异常：%d×%d", result.Width, result.Height)
	}
}

// 非图片一律拒绝，且要在**解码之前**就拒绝。
func TestProcessAvatarRejectsNonImage(t *testing.T) {
	if _, err := processAvatarImage([]byte("PK\x03\x04 this is a zip"), "avatar.jpg",
		avatardomain.UploadOptions{}, defaultTestEncodeOptions()); err == nil {
		t.Fatal("改了后缀名的非图片文件被接受了")
	}
	if _, err := processAvatarImage(nil, "a.jpg", avatardomain.UploadOptions{}, defaultTestEncodeOptions()); err == nil {
		t.Fatal("空内容被接受了")
	}
}

// 动图在限内保持原样。悄悄拍平的表现是"我传的动图怎么不动了"，
// 而用户完全无从判断是不是自己传错了。
func TestProcessAvatarKeepsSmallAnimatedGIF(t *testing.T) {
	raw := encodeTestAnimatedGIF(t, 64, 3)
	result, err := processAvatarImage(raw, "fun.gif", avatardomain.UploadOptions{}, defaultTestEncodeOptions())
	if err != nil {
		t.Fatalf("处理失败：%v", err)
	}
	if !result.Animated {
		t.Fatal("小体积动图应保持动图")
	}
	if result.Base.ContentType != "image/gif" {
		t.Fatalf("动图原图被换成了 %s", result.Base.ContentType)
	}
	if len(result.Variants) == 0 {
		t.Fatal("动图仍应产出静态变体，供列表页使用")
	}
}

// 关掉动图开关时必须拍平，并且如实上报。
func TestProcessAvatarFlattensWhenAnimationDisabled(t *testing.T) {
	opts := defaultTestEncodeOptions()
	opts.KeepAnimated = false
	result, err := processAvatarImage(encodeTestAnimatedGIF(t, 64, 3), "fun.gif", avatardomain.UploadOptions{}, opts)
	if err != nil {
		t.Fatalf("处理失败：%v", err)
	}
	if result.Animated {
		t.Fatal("开关关掉后仍保留了动图")
	}
	if !result.Flattened {
		t.Fatal("拍平了却没有上报 Flattened，用户无从知道发生了什么")
	}
}

func encodeTestAnimatedGIF(t *testing.T, side int, frames int) []byte {
	t.Helper()
	anim := &gif.GIF{}
	palette := color.Palette{color.RGBA{A: 255}, color.RGBA{R: 255, A: 255}, color.RGBA{G: 255, A: 255}}
	for i := 0; i < frames; i++ {
		frame := image.NewPaletted(image.Rect(0, 0, side, side), palette)
		for y := 0; y < side; y++ {
			for x := 0; x < side; x++ {
				frame.SetColorIndex(x, y, uint8((x+y+i)%len(palette)))
			}
		}
		anim.Image = append(anim.Image, frame)
		anim.Delay = append(anim.Delay, 10)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, anim); err != nil {
		t.Fatalf("编码动图失败：%v", err)
	}
	return buf.Bytes()
}

// 上传体积闸门必须按**实际读到的字节**判，不能信 Content-Length。
func TestReadAvatarUploadEnforcesLimit(t *testing.T) {
	payload := bytes.Repeat([]byte("a"), 1024)
	if _, err := readAvatarUpload(bytes.NewReader(payload), 512); err == nil {
		t.Fatal("超限内容被接受了")
	}
	raw, err := readAvatarUpload(bytes.NewReader(payload), 4096)
	if err != nil {
		t.Fatalf("限内内容被拒：%v", err)
	}
	if len(raw) != len(payload) {
		t.Fatalf("读到 %d 字节，期望 %d", len(raw), len(payload))
	}
}

// 尺寸档必须去重、排序、剔除越界值：重复的档会重复上传同一张图，
// 超过上限的档会把小图放大存一份（比原图还大且更糊）。
func TestNormalizeAvatarSizes(t *testing.T) {
	got := normalizeAvatarSizes([]int{512, 64, 64, 0, -3, 9999, 128})
	want := []int{64, 128, 512}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	if len(normalizeAvatarSizes(nil)) == 0 {
		t.Fatal("空配置应回落到标准档而不是不生成任何变体")
	}
}

// 变体键必须能从基准键**推导**出来：万一资产表那一行丢了，
// 光凭引用也要能把所有尺寸找回来。
func TestAvatarVariantKeyDerivesFromBase(t *testing.T) {
	got := avatarVariantKey("avatars/apps/1/users/42/2026/08/13120000_avatar.jpg", 128, ".jpg")
	want := "avatars/apps/1/users/42/2026/08/13120000_avatar_128.jpg"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// 取变体只向上取：向下取会把 64px 的图拉到 256px 显示，
// 糊得比多下几 KB 明显得多。
func TestAssetVariantForPrefersLargerSize(t *testing.T) {
	asset := &avatardomain.Asset{Variants: []avatardomain.Variant{
		{Size: 64, Key: "a_64.jpg"}, {Size: 256, Key: "a_256.jpg"}, {Size: 512, Key: "a_512.jpg"},
	}}
	if got := asset.VariantFor(100); got == nil || got.Size != 256 {
		t.Fatalf("请求 100 应给 256，得到 %+v", got)
	}
	if got := asset.VariantFor(64); got == nil || got.Size != 64 {
		t.Fatalf("请求 64 应给 64，得到 %+v", got)
	}
	if got := asset.VariantFor(4096); got == nil || got.Size != 512 {
		t.Fatalf("超过最大档应回落到 512，得到 %+v", got)
	}
	if (&avatardomain.Asset{}).VariantFor(128) != nil {
		t.Fatal("没有变体时应返回 nil")
	}
}
