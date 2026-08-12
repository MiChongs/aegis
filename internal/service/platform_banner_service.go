package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	storagedomain "aegis/internal/domain/storage"
	systemdomain "aegis/internal/domain/system"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"

	"go.uber.org/zap"
)

const (
	platformBannerMaxUploadSize = 10 << 20 // 10 MB
	platformBannerProxyTTL      = 30 * time.Minute

	// storageRefPrefix 是「图片存在对象存储里」的规范持久化形态：
	// `storage://{configID}/{escapedObjectKey}`。平台横幅与应用 Banner 共用它 ——
	// 两处各定义一份的话，同一个引用会在一处解析得出、在另一处解析不出来。
	storageRefPrefix = "storage://"
)

// PlatformBannerService 负责管理平台级 Banner（超级管理员专属管理）。
// 展示端（总览页）的 GetActive 带 60s Redis 缓存；管理端写操作会清理缓存。
// 图片上传经由 StorageService 转发到已配置的对象存储（OSS/S3/COS/...）。
type PlatformBannerService struct {
	log      *zap.Logger
	pg       *pgrepo.Repository
	sessions *redisrepo.SessionRepository
	storage  *StorageService
	location *time.Location
}

func NewPlatformBannerService(log *zap.Logger, pg *pgrepo.Repository, sessions *redisrepo.SessionRepository, storage *StorageService) *PlatformBannerService {
	return &PlatformBannerService{log: log, pg: pg, sessions: sessions, storage: storage, location: timeutil.DefaultLocation()}
}

// GetActiveBanners 展示用：返回当前生效的平台 Banner。
func (s *PlatformBannerService) GetActiveBanners(ctx context.Context) ([]systemdomain.PlatformBanner, error) {
	if s.sessions != nil {
		if cached, err := s.sessions.GetPlatformBanners(ctx); err != nil {
			s.log.Warn("load platform banner cache failed", zap.Error(err))
		} else if cached != nil {
			return cached, nil
		}
	}
	items, err := s.pg.ListActivePlatformBanners(ctx, time.Now().In(s.location))
	if err != nil {
		return nil, err
	}
	if s.sessions != nil {
		if err := s.sessions.SetPlatformBanners(ctx, items, 60*time.Second); err != nil {
			s.log.Warn("cache platform banners failed", zap.Error(err))
		}
	}
	return items, nil
}

// ListForAdmin 管理端列表（分页 + 过滤 + 总数）。
func (s *PlatformBannerService) ListForAdmin(ctx context.Context, filter systemdomain.PlatformBannerFilter) ([]systemdomain.PlatformBanner, int64, error) {
	return s.pg.ListPlatformBanners(ctx, filter)
}

// Get 单条查询。
func (s *PlatformBannerService) Get(ctx context.Context, id int64) (*systemdomain.PlatformBanner, error) {
	if id <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "非法 ID")
	}
	item, err := s.pg.GetPlatformBannerByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40410, http.StatusNotFound, "Banner 不存在")
	}
	return item, nil
}

// Save 新建或更新；成功后清理缓存。
func (s *PlatformBannerService) Save(ctx context.Context, mutation systemdomain.PlatformBannerMutation) (*systemdomain.PlatformBanner, error) {
	item := systemdomain.PlatformBanner{ID: mutation.ID}
	if mutation.ID > 0 {
		existing, err := s.pg.GetPlatformBannerByID(ctx, mutation.ID)
		if err != nil {
			return nil, err
		}
		if existing == nil {
			return nil, apperrors.New(40410, http.StatusNotFound, "Banner 不存在")
		}
		item = *existing
	}

	if mutation.Title != nil {
		item.Title = strings.TrimSpace(*mutation.Title)
	}
	if mutation.Description != nil {
		item.Description = strings.TrimSpace(*mutation.Description)
	}
	if mutation.ImageURL != nil {
		item.ImageURL = strings.TrimSpace(*mutation.ImageURL)
	}
	if mutation.ClickURL != nil {
		item.ClickURL = strings.TrimSpace(*mutation.ClickURL)
	}
	if mutation.Type != nil {
		item.Type = strings.TrimSpace(*mutation.Type)
	}
	if mutation.Position != nil {
		item.Position = *mutation.Position
	}
	if mutation.Status != nil {
		item.Status = *mutation.Status
	}
	if mutation.StartTime != nil {
		item.StartTime = mutation.StartTime
	}
	if mutation.EndTime != nil {
		item.EndTime = mutation.EndTime
	}
	if mutation.CreatedBy != nil && item.ID == 0 {
		item.CreatedBy = mutation.CreatedBy
	}

	// 校验必填 & 类型白名单
	if item.Title == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "标题不能为空")
	}
	if item.ImageURL == "" {
		return nil, apperrors.New(40000, http.StatusBadRequest, "图片 URL 不能为空")
	}
	if item.Type == "" {
		item.Type = "info"
	}
	if _, ok := systemdomain.ValidPlatformBannerTypes[item.Type]; !ok {
		return nil, apperrors.New(40000, http.StatusBadRequest, "不支持的 Banner 类型")
	}
	if item.StartTime != nil && item.EndTime != nil && item.EndTime.Before(*item.StartTime) {
		return nil, apperrors.New(40000, http.StatusBadRequest, "结束时间必须晚于开始时间")
	}

	saved, err := s.pg.UpsertPlatformBanner(ctx, item)
	if err != nil {
		return nil, err
	}
	s.invalidateCache(ctx)
	return saved, nil
}

// Delete 单条删除。
func (s *PlatformBannerService) Delete(ctx context.Context, id int64) error {
	if id <= 0 {
		return apperrors.New(40000, http.StatusBadRequest, "非法 ID")
	}
	ok, err := s.pg.DeletePlatformBanner(ctx, id)
	if err != nil {
		return err
	}
	if !ok {
		return apperrors.New(40410, http.StatusNotFound, "Banner 不存在")
	}
	s.invalidateCache(ctx)
	return nil
}

// DeleteMany 批量删除，返回影响行数。
func (s *PlatformBannerService) DeleteMany(ctx context.Context, ids []int64) (int64, error) {
	affected, err := s.pg.DeletePlatformBanners(ctx, ids)
	if err != nil {
		return 0, err
	}
	if affected > 0 {
		s.invalidateCache(ctx)
	}
	return affected, nil
}

func (s *PlatformBannerService) invalidateCache(ctx context.Context) {
	if s.sessions == nil {
		return
	}
	if err := s.sessions.DeletePlatformBanners(ctx); err != nil {
		s.log.Warn("delete platform banner cache failed", zap.Error(err))
	}
}

// PlatformBannerUploadInput 图片上传入参。
type PlatformBannerUploadInput struct {
	ConfigName    string
	FileName      string
	ContentType   string
	ContentLength int64
	Content       io.Reader
	UploadedBy    *int64
}

// PlatformBannerUploadResult 上传成功后返回给前端。
//   - Reference：写入 DB 的规范形态（`storage://{configID}/{objectKey}`）
//   - URL：立即可用于预览的代理地址（带 ticket，30 分钟内有效）
//   - Storage：原始 StoredObject（调试用）
type PlatformBannerUploadResult struct {
	Reference string                      `json:"reference"`
	URL       string                      `json:"url"`
	Storage   *storagedomain.StoredObject `json:"storage,omitempty"`
}

// UploadImage 接收前端拖拽/选择的图片，经 StorageService 推至已配置的对象存储。
//
// 重要：不再直接返回存储 provider 的原始 URL（local provider 会返回 /api/storage/proxy/{key}，
// 但该端点实际要求 :ticket，导致 404）。改为统一返回 `storage://` 引用 + 通过 CreateObjectLinkByConfigID
// 即时换取的带 ticket 代理 URL。
//
// 约束：
//   - 仅允许 image/jpeg、image/png、image/gif、image/webp、image/svg+xml
//   - 大小上限 10MB
//   - 使用平台级存储配置（appID=0）
func (s *PlatformBannerService) UploadImage(ctx context.Context, baseURL string, input PlatformBannerUploadInput) (*PlatformBannerUploadResult, error) {
	if s.storage == nil {
		return nil, apperrors.New(50380, http.StatusServiceUnavailable, "存储服务未启用")
	}
	contentType, ext, err := validatePlatformBannerUpload(input.FileName, input.ContentType, input.ContentLength)
	if err != nil {
		return nil, err
	}
	key, err := platformBannerObjectKey(ext)
	if err != nil {
		return nil, err
	}

	stored, err := s.storage.UploadForApp(ctx, 0, storagedomain.UploadInput{
		AppID:         0,
		ConfigName:    strings.TrimSpace(input.ConfigName),
		ObjectKey:     key,
		FileName:      strings.TrimSpace(input.FileName),
		ContentType:   contentType,
		ContentLength: input.ContentLength,
		CacheControl:  "public, max-age=31536000, immutable",
		Metadata: map[string]string{
			"module": "platform-banner",
		},
		Content:      input.Content,
		UploadedBy:   input.UploadedBy,
		UploaderType: "admin",
	})
	if err != nil {
		return nil, err
	}
	if stored == nil || stored.ConfigID <= 0 || strings.TrimSpace(stored.Key) == "" {
		return nil, apperrors.New(50381, http.StatusServiceUnavailable, "存储返回结果异常")
	}

	reference := buildStorageReference(stored.ConfigID, stored.Key)
	displayURL := s.resolveStorageURL(ctx, baseURL, stored.ConfigID, stored.Key)
	if displayURL == "" {
		// 理论上不应发生；兜底返回 storage provider 给出的原始 URL
		displayURL = strings.TrimSpace(stored.URL)
	}

	return &PlatformBannerUploadResult{
		Reference: reference,
		URL:       displayURL,
		Storage:   stored,
	}, nil
}

// ResolveDisplayURLs 为一批 Banner 填充 ImageDisplayURL。
// `storage://` 引用 → 通过 CreateObjectLinkByConfigID 换取 ticket 代理 URL；
// 其它（例如手动粘贴的外链）→ 原值。
func (s *PlatformBannerService) ResolveDisplayURLs(ctx context.Context, baseURL string, items []systemdomain.PlatformBanner) {
	for i := range items {
		items[i].ImageDisplayURL = s.resolveImageURL(ctx, baseURL, items[i].ImageURL)
	}
}

// ResolveOne 单条填充。
func (s *PlatformBannerService) ResolveOne(ctx context.Context, baseURL string, item *systemdomain.PlatformBanner) {
	if item == nil {
		return
	}
	item.ImageDisplayURL = s.resolveImageURL(ctx, baseURL, item.ImageURL)
}

func (s *PlatformBannerService) resolveImageURL(ctx context.Context, baseURL string, stored string) string {
	stored = strings.TrimSpace(stored)
	if stored == "" {
		return ""
	}
	if configID, objectKey, ok := parseStorageReference(stored); ok {
		if resolved := s.resolveStorageURL(ctx, baseURL, configID, objectKey); resolved != "" {
			return resolved
		}
		return ""
	}
	return stored
}

func (s *PlatformBannerService) resolveStorageURL(ctx context.Context, baseURL string, configID int64, objectKey string) string {
	if s.storage == nil || configID <= 0 || strings.TrimSpace(objectKey) == "" {
		return ""
	}
	// baseURL 在同源反代部署时其实没必要拼回；保留参数占位但不使用，
	// 避免生成的 URL 被 next/image 判定为跨域 upstream 而触发 SSRF 私网 IP 拦截。
	_ = baseURL
	result, ticketID, err := s.storage.CreateObjectLinkByConfigID(ctx, 0, configID, storagedomain.LinkRequest{
		ObjectKey: objectKey,
		ExpiresIn: platformBannerProxyTTL,
	})
	if err != nil {
		s.log.Warn("resolve platform banner url failed", zap.Int64("config_id", configID), zap.String("object_key", objectKey), zap.Error(err))
		return ""
	}
	if result == nil {
		return ""
	}
	if ticketID != "" {
		// 直接返回相对路径：前端 next/image 会把 `/api/...` 当作 local image
		// 走同源 fetch，不再走 Next upstream image 管线的私网 IP 防护检查。
		return "/api/storage/proxy/" + url.PathEscape(ticketID)
	}
	return strings.TrimSpace(result.URL)
}

// ───── 校验与对象键辅助 ─────

var platformBannerContentTypeByExt = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
}

var platformBannerExtByContentType = map[string]string{
	"image/jpeg":    ".jpg",
	"image/png":     ".png",
	"image/gif":     ".gif",
	"image/webp":    ".webp",
	"image/svg+xml": ".svg",
}

func validatePlatformBannerUpload(fileName, contentType string, size int64) (string, string, error) {
	if size <= 0 {
		return "", "", apperrors.New(40087, http.StatusBadRequest, "上传文件不能为空")
	}
	if size > platformBannerMaxUploadSize {
		return "", "", apperrors.New(40088, http.StatusBadRequest, "Banner 图片不能超过 10MB")
	}
	ext := strings.ToLower(strings.TrimSpace(path.Ext(fileName)))
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if mapped, ok := platformBannerContentTypeByExt[ext]; ok {
		return mapped, ext, nil
	}
	if mappedExt, ok := platformBannerExtByContentType[contentType]; ok {
		return contentType, mappedExt, nil
	}
	return "", "", apperrors.New(40089, http.StatusBadRequest, "仅支持 JPG / PNG / GIF / WEBP / SVG 图片")
}

func platformBannerObjectKey(ext string) (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("platform/banners/%s/%s%s", time.Now().UTC().Format("200601"), hex.EncodeToString(buf), ext), nil
}

// buildStorageReference 拼接 `storage://{configID}/{escapedKey}`。
func buildStorageReference(configID int64, objectKey string) string {
	objectKey = strings.TrimSpace(objectKey)
	if configID <= 0 || objectKey == "" {
		return ""
	}
	return fmt.Sprintf("%s%d/%s", storageRefPrefix, configID, url.PathEscape(objectKey))
}

// parseStorageReference 解析 `storage://{configID}/{objectKey}`，返回是否命中。
func parseStorageReference(raw string) (int64, string, bool) {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, storageRefPrefix) {
		return 0, "", false
	}
	rest := strings.TrimPrefix(trimmed, storageRefPrefix)
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 {
		return 0, "", false
	}
	configID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || configID <= 0 {
		return 0, "", false
	}
	objectKey, err := url.PathUnescape(parts[1])
	if err != nil || strings.TrimSpace(objectKey) == "" {
		return 0, "", false
	}
	return configID, objectKey, true
}

// joinBaseURL 把相对路径拼到 baseURL。baseURL 为空时返回原路径（浏览器侧相对路径也能用）。
func joinBaseURL(baseURL, path string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return baseURL + path
}
