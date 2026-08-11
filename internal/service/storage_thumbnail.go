package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	storagedomain "aegis/internal/domain/storage"
	apperrors "aegis/pkg/errors"

	// 解码器按需注册（jpeg / png 已在上面具名引入，注册在包初始化时完成）。
	// x/image 补上标准库没有的三种格式 —— 少了 webp 这一条，
	// 现在图床里绝大多数图都出不了缩略图。
	_ "image/gif"

	_ "golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"

	"go.uber.org/zap"
)

// 缩略图的四道闸门。渲染的输入完全由上传方决定，没有闸门的话
// 一张 50000×50000 的 PNG（压缩后可能只有几百 KB）就能让进程申请十几 GB。
const (
	thumbnailMinWidth = 32
	thumbnailMaxWidth = 512
	// thumbnailMaxSourceBytes 单张源图读入内存的上限
	thumbnailMaxSourceBytes = 32 << 20 // 32 MiB
	// thumbnailMaxPixels 解码前用 DecodeConfig 先量一次，超了直接拒绝。
	// 这一步是关键：字节数小不代表解开来小（图像炸弹正是这么做的）。
	thumbnailMaxPixels = 40_000_000
	// thumbnailConcurrency 同时进行的解码数。缩放是 CPU 密集的，
	// 一屏 40 个格子同时请求会把所有核吃满，进而拖慢正常的 API 请求。
	thumbnailConcurrency = 4
	thumbnailCacheTTL    = 6 * time.Hour
)

// thumbnailSlots 进程级渲染并发闸。缓冲通道而不是 worker 池：
// 这里要的是"排队等一个名额"，不是"把任务扔出去异步跑"。
var thumbnailSlots = make(chan struct{}, thumbnailConcurrency)

// ThumbnailResult 一张渲染好的缩略图
type ThumbnailResult struct {
	Data        []byte
	ContentType string
	Width       int
	Height      int
	ETag        string
	FromCache   bool
}

// RenderThumbnail 读取存储对象并渲染成指定宽度的缩略图。
//
// 之所以在服务端做而不是让控制台直接 <img> 原图：一屏 40 个格子拉 40 张原图
// 可能是几百 MB，而缩略图一张只有几 KB。同时它也是私有桶唯一可行的预览方式 ——
// 私有对象没有可直接嵌进 <img> 的公开地址。
//
// cacheTag 用对象的 ETag：内容变了 ETag 就变，缓存键自然失效，
// 不需要额外的失效逻辑。ETag 为空时退化成按对象键 + 尺寸缓存。
func (s *StorageService) RenderThumbnail(ctx context.Context, configID int64, objectKey string, cacheTag string, width int) (*ThumbnailResult, error) {
	width = clampThumbnailWidth(width)
	cacheKey := s.thumbnailCacheKey(configID, objectKey, cacheTag, width)

	if cached := s.readCachedThumbnail(ctx, cacheKey); cached != nil {
		return cached, nil
	}

	cfg, err := s.pg.GetStorageConfigByID(ctx, configID)
	if err != nil {
		return nil, err
	}
	if cfg == nil || !cfg.Enabled {
		return nil, apperrors.New(40482, http.StatusNotFound, "未配置可用存储服务")
	}
	if err := validateStorageObjectKey(objectKey); err != nil {
		return nil, err
	}
	provider, err := s.buildProvider(cfg)
	if err != nil {
		return nil, err
	}

	reader, err := provider.Open(ctx, cfg, objectKey)
	if err != nil {
		s.log.Warn("缩略图读取源对象失败", zap.Int64("config_id", configID),
			zap.String("provider", cfg.Provider), zap.String("key", objectKey), zap.Error(err))
		return nil, apperrors.New(40481, http.StatusNotFound, "资源不可用")
	}
	defer closeSilently(reader.Body)

	// 多读 1 字节用来分辨"正好等于上限"与"超过上限"
	raw, err := io.ReadAll(io.LimitReader(reader.Body, thumbnailMaxSourceBytes+1))
	if err != nil {
		return nil, apperrors.New(40481, http.StatusNotFound, "资源不可用")
	}
	if len(raw) > thumbnailMaxSourceBytes {
		return nil, apperrors.New(41381, http.StatusRequestEntityTooLarge, "源文件过大，无法生成缩略图")
	}

	result, err := renderThumbnailBytes(raw, width)
	if err != nil {
		return nil, err
	}
	s.writeCachedThumbnail(ctx, cacheKey, result)
	return result, nil
}

// renderThumbnailBytes 纯内存的解码 + 缩放 + 编码，不碰任何外部依赖，便于单测
func renderThumbnailBytes(raw []byte, width int) (*ThumbnailResult, error) {
	width = clampThumbnailWidth(width)

	// 先量后解：DecodeConfig 只读文件头，拿到的是声明的宽高。
	// 越过这一步直接 Decode，图像炸弹就已经把内存申请出去了。
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, apperrors.New(41580, http.StatusUnsupportedMediaType, "该文件不是可识别的图片格式")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return nil, apperrors.New(41580, http.StatusUnsupportedMediaType, "图片尺寸无效")
	}
	if int64(cfg.Width)*int64(cfg.Height) > thumbnailMaxPixels {
		return nil, apperrors.New(41381, http.StatusRequestEntityTooLarge, "图片像素过多，无法生成缩略图")
	}

	thumbnailSlots <- struct{}{}
	defer func() { <-thumbnailSlots }()

	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, apperrors.New(41580, http.StatusUnsupportedMediaType, "图片解码失败")
	}

	bounds := src.Bounds()
	dstW, dstH := fitWithin(bounds.Dx(), bounds.Dy(), width)
	dst := image.NewNRGBA(image.Rect(0, 0, dstW, dstH))
	// CatmullRom 是 x/image/draw 里质量最好的一档。缩略图缩放比例往往很大
	// （2000px → 192px），用 NearestNeighbor 会糊到看不出内容，
	// 那样这个功能就白做了。
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, bounds, xdraw.Over, nil)

	var buf bytes.Buffer
	contentType := "image/jpeg"
	if imageHasAlpha(src) {
		// 带透明通道的图编成 JPEG，透明区会变成黑块 —— 图标类素材尤其明显
		contentType = "image/png"
		if err := png.Encode(&buf, dst); err != nil {
			return nil, apperrors.New(50085, http.StatusInternalServerError, "缩略图编码失败")
		}
	} else if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 82}); err != nil {
		return nil, apperrors.New(50085, http.StatusInternalServerError, "缩略图编码失败")
	}

	data := buf.Bytes()
	sum := sha256.Sum256(data)
	return &ThumbnailResult{
		Data:        data,
		ContentType: contentType,
		Width:       dstW,
		Height:      dstH,
		ETag:        `"` + hex.EncodeToString(sum[:16]) + `"`,
	}, nil
}

// fitWithin 等比缩放到边长为 box 的方框内，且**绝不放大**。
// 放大一张 48px 的图标到 192px 只会得到一团马赛克，还白白多传 16 倍字节。
func fitWithin(srcW, srcH, box int) (int, int) {
	if srcW <= box && srcH <= box {
		return srcW, srcH
	}
	if srcW >= srcH {
		h := int(float64(srcH) * float64(box) / float64(srcW))
		return box, maxInt(h, 1)
	}
	w := int(float64(srcW) * float64(box) / float64(srcH))
	return maxInt(w, 1), box
}

// imageHasAlpha 判断源图是否含透明像素。
// 绝大多数 image 实现都提供 Opaque()（内部按像素扫描），拿不到就保守地
// 按"可能透明"处理 —— 多编一张 PNG 只是大一点，编错成 JPEG 是画面出错。
func imageHasAlpha(img image.Image) bool {
	if opaque, ok := img.(interface{ Opaque() bool }); ok {
		return !opaque.Opaque()
	}
	return true
}

func clampThumbnailWidth(width int) int {
	if width < thumbnailMinWidth {
		return 192
	}
	if width > thumbnailMaxWidth {
		return thumbnailMaxWidth
	}
	return width
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ── 缓存 ──
//
// 缩略图存 Redis 而不是落盘：它随时可以从源对象重新算出来，
// 属于典型的可再生数据，落盘只会多一份需要清理的东西。

func (s *StorageService) thumbnailCacheKey(configID int64, objectKey string, cacheTag string, width int) string {
	// 对象键可能很长且含任意字符，取摘要当键的一部分
	sum := sha256.Sum256([]byte(objectKey + "\x00" + cacheTag))
	return fmt.Sprintf("%s:storage:thumb:%d:%s:%d", s.keyPrefix, configID, hex.EncodeToString(sum[:12]), width)
}

// 缓存值的帧格式：`contentType|width|height|etag\n` + 原始字节。
// 不用 JSON 是因为 base64 会让每张图白白多占 33% 内存。
func (s *StorageService) readCachedThumbnail(ctx context.Context, key string) *ThumbnailResult {
	if s.redis == nil {
		return nil
	}
	raw, err := s.redis.Get(ctx, key).Bytes()
	if err != nil || len(raw) == 0 {
		return nil
	}
	idx := bytes.IndexByte(raw, '\n')
	if idx <= 0 {
		return nil
	}
	parts := strings.Split(string(raw[:idx]), "|")
	if len(parts) != 4 {
		return nil
	}
	width, _ := strconv.Atoi(parts[1])
	height, _ := strconv.Atoi(parts[2])
	return &ThumbnailResult{
		Data:        raw[idx+1:],
		ContentType: parts[0],
		Width:       width,
		Height:      height,
		ETag:        parts[3],
		FromCache:   true,
	}
}

func (s *StorageService) writeCachedThumbnail(ctx context.Context, key string, result *ThumbnailResult) {
	if s.redis == nil || result == nil {
		return
	}
	header := fmt.Sprintf("%s|%d|%d|%s\n", result.ContentType, result.Width, result.Height, result.ETag)
	payload := append([]byte(header), result.Data...)
	if err := s.redis.Set(ctx, key, payload, thumbnailCacheTTL).Err(); err != nil {
		// 缓存写失败只影响下次要重算，绝不该让这次请求失败
		s.log.Debug("缩略图缓存写入失败", zap.String("key", key), zap.Error(err))
	}
}

// CanRenderThumbnail 判断某个存储对象是否值得尝试渲染缩略图。
// 供传输层在签发链接前先判一次，免得给一个必然 415 的地址。
func CanRenderThumbnail(obj *storagedomain.StorageObject) bool {
	if obj == nil || obj.Size <= 0 || obj.Size > thumbnailMaxSourceBytes {
		return false
	}
	if !isImageContentType(obj.ContentType) {
		return false
	}
	// SVG 是矢量描述而非位图，image.Decode 解不了；它由前端直接内联渲染。
	return contentTypeMediaType(obj.ContentType) != "image/svg+xml"
}
