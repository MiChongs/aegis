package service

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"testing"

	storagedomain "aegis/internal/domain/storage"
)

// makePNG 造一张 w×h 的测试图；alpha=false 时全不透明
func makePNG(t *testing.T, w, h int, alpha bool) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			a := uint8(255)
			if alpha && x < w/2 {
				a = 0
			}
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: a})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("编码测试图失败: %v", err)
	}
	return buf.Bytes()
}

func TestRenderThumbnailFitsWithinBoxAndKeepsAspect(t *testing.T) {
	source := makePNG(t, 800, 400, false)

	result, err := renderThumbnailBytes(source, 192)
	if err != nil {
		t.Fatalf("渲染缩略图失败: %v", err)
	}
	if result.Width != 192 || result.Height != 96 {
		t.Fatalf("缩略图尺寸未按长边等比收敛，得到 %dx%d，期望 192x96", result.Width, result.Height)
	}
	if result.ContentType != "image/jpeg" {
		t.Fatalf("不透明源图应编成 JPEG，得到 %s", result.ContentType)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(result.Data))
	if err != nil {
		t.Fatalf("产物无法解码: %v", err)
	}
	if cfg.Width != result.Width || cfg.Height != result.Height {
		t.Fatalf("产物实际尺寸 %dx%d 与声明的 %dx%d 不符", cfg.Width, cfg.Height, result.Width, result.Height)
	}
}

// 放大一张小图只会得到马赛克，还白白多传十几倍字节
func TestRenderThumbnailNeverUpscales(t *testing.T) {
	result, err := renderThumbnailBytes(makePNG(t, 48, 32, false), 512)
	if err != nil {
		t.Fatalf("渲染缩略图失败: %v", err)
	}
	if result.Width != 48 || result.Height != 32 {
		t.Fatalf("小图被放大了：得到 %dx%d，期望保持 48x32", result.Width, result.Height)
	}
}

// 带透明通道的图编成 JPEG，透明区会变黑块 —— 图标类素材上尤其刺眼
func TestRenderThumbnailKeepsAlphaAsPNG(t *testing.T) {
	result, err := renderThumbnailBytes(makePNG(t, 300, 300, true), 128)
	if err != nil {
		t.Fatalf("渲染缩略图失败: %v", err)
	}
	if result.ContentType != "image/png" {
		t.Fatalf("含透明通道的源图应编成 PNG，得到 %s", result.ContentType)
	}
}

func TestRenderThumbnailRejectsNonImage(t *testing.T) {
	if _, err := renderThumbnailBytes([]byte("这不是图片，只是一段文本"), 192); err == nil {
		t.Fatal("非图片内容应当被拒绝，而不是渲染出一张空图")
	}
}

// 像素闸门必须在 Decode **之前** 生效：字节数小不代表解开来小，
// 图像炸弹正是靠这个差距把内存吃光的。
func TestRenderThumbnailRejectsPixelBomb(t *testing.T) {
	// 手工拼一个声明 30000x30000 的 PNG 头（不需要真实像素数据，
	// 因为 DecodeConfig 只读文件头就应该判定拒绝）
	var buf bytes.Buffer
	buf.WriteString("\x89PNG\r\n\x1a\n")
	ihdr := []byte{
		0, 0, 0, 13, // 长度
		'I', 'H', 'D', 'R',
		0, 0, 0x75, 0x30, // 宽 30000
		0, 0, 0x75, 0x30, // 高 30000
		8, 6, 0, 0, 0,
	}
	buf.Write(ihdr)
	buf.Write([]byte{0, 0, 0, 0}) // CRC 占位（DecodeConfig 不校验 IHDR 的 CRC）

	_, err := renderThumbnailBytes(buf.Bytes(), 192)
	if err == nil {
		t.Fatal("声明 9 亿像素的图片应被拒绝")
	}
	if !strings.Contains(err.Error(), "像素") && !strings.Contains(err.Error(), "图片") {
		t.Fatalf("拒绝原因应当可读，得到: %v", err)
	}
}

func TestFitWithin(t *testing.T) {
	cases := []struct {
		name                  string
		srcW, srcH, box, w, h int
	}{
		{"横图按宽收敛", 1000, 500, 200, 200, 100},
		{"竖图按高收敛", 500, 1000, 200, 100, 200},
		{"正方形", 900, 900, 300, 300, 300},
		{"极扁的图高度不塌成 0", 10000, 3, 100, 100, 1},
		{"小于方框时原样返回", 80, 60, 200, 80, 60},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w, h := fitWithin(tc.srcW, tc.srcH, tc.box)
			if w != tc.w || h != tc.h {
				t.Fatalf("得到 %dx%d，期望 %dx%d", w, h, tc.w, tc.h)
			}
		})
	}
}

// ── MIME 嗅探 ──

func TestResolveUploadContentTypePrefersMagicBytes(t *testing.T) {
	source := makePNG(t, 8, 8, false)

	reader, resolved, masquerade := resolveUploadContentType(bytes.NewReader(source), "text/plain", "shell.txt")
	if resolved != "image/png" {
		t.Fatalf("应以魔数为准判成 image/png，得到 %s", resolved)
	}
	if masquerade != "text/plain" {
		t.Fatalf("大类不同应报告谎报的声明值，得到 %q", masquerade)
	}

	// 嗅探不能吃掉内容：上传链路拿到的必须还是完整字节
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("读取包装后的 reader 失败: %v", err)
	}
	if !bytes.Equal(got, source) {
		t.Fatalf("嗅探后内容被截断：得到 %d 字节，期望 %d 字节", len(got), len(source))
	}
}

func TestResolveUploadContentTypeFallsBackToExtension(t *testing.T) {
	// 魔数认不出来（随机二进制）时才回落到扩展名
	_, resolved, masquerade := resolveUploadContentType(bytes.NewReader([]byte{0x00, 0x01, 0x02, 0x03, 0x7f}), "", "note.css")
	if !strings.HasPrefix(resolved, "text/css") {
		t.Fatalf("魔数无法识别时应按扩展名回落到 text/css，得到 %s", resolved)
	}
	if masquerade != "" {
		t.Fatalf("回落路径不该报告谎报，得到 %q", masquerade)
	}
}

// image/jpeg 写成 image/jpg 这类同大类的写法差异天天都有，
// 报出来只会让告警变噪音
func TestResolveUploadContentTypeIgnoresSameFamilyMismatch(t *testing.T) {
	_, resolved, masquerade := resolveUploadContentType(bytes.NewReader(makePNG(t, 8, 8, false)), "image/jpeg", "a.jpg")
	if resolved != "image/png" {
		t.Fatalf("应判成 image/png，得到 %s", resolved)
	}
	if masquerade != "" {
		t.Fatalf("同大类的差异不该被报成谎报，得到 %q", masquerade)
	}
}

func TestResolveUploadContentTypeEmptyBody(t *testing.T) {
	_, resolved, _ := resolveUploadContentType(bytes.NewReader(nil), "application/pdf", "empty.pdf")
	if resolved != "application/pdf" {
		t.Fatalf("空内容应保留声明值，得到 %s", resolved)
	}
}

func TestScriptableContentTypeDetection(t *testing.T) {
	for _, ct := range []string{"text/html", "image/svg+xml", "TEXT/HTML; charset=utf-8", "application/xhtml+xml"} {
		if !IsScriptableContentType(ct) {
			t.Fatalf("%s 应被识别为可执行脚本的类型", ct)
		}
	}
	for _, ct := range []string{"image/png", "application/pdf", "text/plain; charset=utf-8", ""} {
		if IsScriptableContentType(ct) {
			t.Fatalf("%s 不该被识别为可执行脚本的类型", ct)
		}
	}
}

func TestCanRenderThumbnail(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		size        int64
		want        bool
	}{
		{"普通图片", "image/png", 1024, true},
		{"带 charset 的图片", "image/jpeg", 2048, true},
		{"矢量图交给前端内联渲染", "image/svg+xml", 1024, false},
		{"非图片", "application/pdf", 1024, false},
		{"空文件", "image/png", 0, false},
		{"超过源文件上限", "image/png", thumbnailMaxSourceBytes + 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			obj := &storagedomain.StorageObject{ContentType: tc.contentType, Size: tc.size}
			if got := CanRenderThumbnail(obj); got != tc.want {
				t.Fatalf("CanRenderThumbnail(%s, %d) = %v，期望 %v", tc.contentType, tc.size, got, tc.want)
			}
		})
	}
	if CanRenderThumbnail(nil) {
		t.Fatal("空对象不该被判为可渲染")
	}
}
