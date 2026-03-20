package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	authdomain "aegis/internal/domain/auth"
	storagedomain "aegis/internal/domain/storage"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type storageProvider interface {
	Name() string
	HealthCheck(ctx context.Context, cfg *storagedomain.Config) (map[string]any, error)
	Upload(ctx context.Context, cfg *storagedomain.Config, input storagedomain.UploadInput) (*storagedomain.StoredObject, error)
	Open(ctx context.Context, cfg *storagedomain.Config, objectKey string) (*storagedomain.ObjectReader, error)
	PublicURL(ctx context.Context, cfg *storagedomain.Config, objectKey string, expiresIn time.Duration) (string, error)
}

type StorageService struct {
	log        *zap.Logger
	pg         *pgrepo.Repository
	redis      *redislib.Client
	keyPrefix  string
	httpClient *http.Client
}

func NewStorageService(log *zap.Logger, pg *pgrepo.Repository, redis *redislib.Client, keyPrefix string) *StorageService {
	return &StorageService{
		log:       log,
		pg:        pg,
		redis:     redis,
		keyPrefix: keyPrefix,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (s *StorageService) ListConfigs(ctx context.Context, scope string, appID *int64, provider string) ([]storagedomain.Config, error) {
	if err := s.validateScopeApp(scope, appID); err != nil {
		return nil, err
	}
	if scope == storagedomain.ScopeApp && appID != nil {
		if _, err := s.requireApp(ctx, *appID); err != nil {
			return nil, err
		}
	}
	return s.pg.ListStorageConfigs(ctx, storagedomain.ListQuery{Scope: scope, AppID: appID, Provider: provider})
}

func (s *StorageService) Detail(ctx context.Context, scope string, appID *int64, id int64) (*storagedomain.Config, error) {
	if err := s.validateScopeApp(scope, appID); err != nil {
		return nil, err
	}
	if scope == storagedomain.ScopeApp && appID != nil {
		if _, err := s.requireApp(ctx, *appID); err != nil {
			return nil, err
		}
	}
	item, err := s.pg.GetStorageConfigByScopeID(ctx, scope, appID, id)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40480, http.StatusNotFound, "存储配置不存在")
	}
	return item, nil
}

func (s *StorageService) Save(ctx context.Context, mutation storagedomain.ConfigMutation) (*storagedomain.Config, error) {
	scope := strings.TrimSpace(mutation.Scope)
	if scope == "" {
		scope = storagedomain.ScopeApp
	}
	if err := s.validateScopeApp(scope, mutation.AppID); err != nil {
		return nil, err
	}
	if scope == storagedomain.ScopeApp && mutation.AppID != nil {
		if _, err := s.requireApp(ctx, *mutation.AppID); err != nil {
			return nil, err
		}
	}

	current, err := s.pg.GetStorageConfigByScopeID(ctx, scope, mutation.AppID, mutation.ID)
	if err != nil {
		return nil, err
	}
	item := storagedomain.Config{
		ID:            mutation.ID,
		Scope:         scope,
		AppID:         mutation.AppID,
		Provider:      storagedomain.ProviderS3,
		ConfigName:    "default",
		AccessMode:    storagedomain.AccessPublic,
		Enabled:       true,
		IsDefault:     mutation.ID == 0,
		ProxyDownload: true,
		RootPath:      "",
		ConfigData:    map[string]any{},
	}
	if current != nil {
		item = *current
	}
	if mutation.Provider != nil {
		item.Provider = strings.TrimSpace(*mutation.Provider)
	}
	if mutation.ConfigName != nil {
		item.ConfigName = strings.TrimSpace(*mutation.ConfigName)
	}
	if mutation.AccessMode != nil {
		item.AccessMode = strings.TrimSpace(*mutation.AccessMode)
	}
	if mutation.Enabled != nil {
		item.Enabled = *mutation.Enabled
	}
	if mutation.IsDefault != nil {
		item.IsDefault = *mutation.IsDefault
	}
	if mutation.ProxyDownload != nil {
		item.ProxyDownload = *mutation.ProxyDownload
	}
	if mutation.BaseURL != nil {
		item.BaseURL = strings.TrimSpace(*mutation.BaseURL)
	}
	if mutation.RootPath != nil {
		item.RootPath = normalizeRootPath(*mutation.RootPath)
	}
	if mutation.Description != nil {
		item.Description = strings.TrimSpace(*mutation.Description)
	}
	if mutation.ConfigData != nil {
		item.ConfigData = mutation.ConfigData
	}
	if item.Scope == storagedomain.ScopeGlobal {
		item.AppID = nil
	}
	if strings.TrimSpace(item.ConfigName) == "" {
		return nil, apperrors.New(40080, http.StatusBadRequest, "存储配置名称不能为空")
	}
	if !isSupportedStorageProvider(item.Provider) {
		return nil, apperrors.New(40081, http.StatusBadRequest, "存储提供商不受支持")
	}
	if !isSupportedAccessMode(item.AccessMode) {
		return nil, apperrors.New(40082, http.StatusBadRequest, "存储访问模式无效")
	}
	if item.ProxyDownload == false && item.AccessMode == storagedomain.AccessPrivate {
		item.ProxyDownload = true
	}
	if item.ConfigData == nil {
		item.ConfigData = map[string]any{}
	}
	if _, err := s.buildProvider(&item); err != nil {
		return nil, err
	}
	saved, err := s.pg.UpsertStorageConfig(ctx, item)
	if err != nil {
		return nil, err
	}
	s.invalidateResolvedCache(ctx, saved)
	return saved, nil
}

func (s *StorageService) Delete(ctx context.Context, scope string, appID *int64, id int64) error {
	if err := s.validateScopeApp(scope, appID); err != nil {
		return err
	}
	item, err := s.pg.GetStorageConfigByScopeID(ctx, scope, appID, id)
	if err != nil {
		return err
	}
	if item == nil {
		return apperrors.New(40480, http.StatusNotFound, "存储配置不存在")
	}
	deleted, err := s.pg.DeleteStorageConfig(ctx, scope, appID, id)
	if err != nil {
		return err
	}
	if !deleted {
		return apperrors.New(40480, http.StatusNotFound, "存储配置不存在")
	}
	s.invalidateResolvedCache(ctx, item)
	return nil
}

func (s *StorageService) TestConfig(ctx context.Context, scope string, appID *int64, id int64) (*storagedomain.TestResult, error) {
	item, err := s.Detail(ctx, scope, appID, id)
	if err != nil {
		return nil, err
	}
	provider, err := s.buildProvider(item)
	if err != nil {
		return nil, err
	}
	meta, err := provider.HealthCheck(ctx, item)
	if err != nil {
		return nil, apperrors.New(50080, http.StatusBadGateway, "存储配置测试失败")
	}
	return &storagedomain.TestResult{
		Success:  true,
		Provider: item.Provider,
		Scope:    item.Scope,
		AppID:    item.AppID,
		Message:  "存储配置可用",
		Metadata: meta,
	}, nil
}

func (s *StorageService) Upload(ctx context.Context, session *authdomain.Session, input storagedomain.UploadInput) (*storagedomain.StoredObject, error) {
	if session == nil {
		return nil, apperrors.New(40180, http.StatusUnauthorized, "未认证")
	}
	cfg, err := s.resolveConfig(ctx, storagedomain.ResolveOptions{AppID: session.AppID, ConfigName: input.ConfigName})
	if err != nil {
		return nil, err
	}
	provider, err := s.buildProvider(cfg)
	if err != nil {
		return nil, err
	}
	input.AppID = session.AppID
	input.ObjectKey = strings.TrimSpace(input.ObjectKey)
	if input.ObjectKey == "" {
		input.ObjectKey = buildUploadedObjectKey(input.FileName)
	}
	input.ObjectKey = normalizeObjectKey(cfg.RootPath, input.ObjectKey)
	if input.ContentType == "" && input.FileName != "" {
		input.ContentType = mime.TypeByExtension(strings.ToLower(path.Ext(input.FileName)))
	}
	item, err := provider.Upload(ctx, cfg, input)
	if err != nil {
		s.log.Warn("storage upload failed", zap.Int64("appid", session.AppID), zap.Int64("config_id", cfg.ID), zap.String("provider", cfg.Provider), zap.Error(err))
		return nil, apperrors.New(50081, http.StatusBadGateway, "文件上传失败")
	}
	item.ConfigID = cfg.ID
	item.Provider = cfg.Provider
	item.AccessMode = cfg.AccessMode
	item.ProxyRequired = cfg.AccessMode == storagedomain.AccessPrivate || cfg.ProxyDownload
	return item, nil
}

func (s *StorageService) CreateObjectLink(ctx context.Context, session *authdomain.Session, req storagedomain.LinkRequest) (*storagedomain.LinkResult, string, error) {
	if session == nil {
		return nil, "", apperrors.New(40180, http.StatusUnauthorized, "未认证")
	}
	cfg, err := s.resolveConfig(ctx, storagedomain.ResolveOptions{AppID: session.AppID, ConfigName: req.ConfigName})
	if err != nil {
		return nil, "", err
	}
	objectKey := normalizeObjectKey(cfg.RootPath, req.ObjectKey)
	if objectKey == "" {
		return nil, "", apperrors.New(40083, http.StatusBadRequest, "对象路径不能为空")
	}
	expiresIn := req.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 10 * time.Minute
	}
	if expiresIn > time.Hour {
		expiresIn = time.Hour
	}

	result := &storagedomain.LinkResult{
		ConfigID:      cfg.ID,
		Provider:      cfg.Provider,
		Key:           objectKey,
		AccessMode:    cfg.AccessMode,
		ProxyRequired: cfg.AccessMode == storagedomain.AccessPrivate || cfg.ProxyDownload,
		ExpiresAt:     time.Now().Add(expiresIn),
	}
	if cfg.AccessMode == storagedomain.AccessPublic && !cfg.ProxyDownload {
		provider, err := s.buildProvider(cfg)
		if err != nil {
			return nil, "", err
		}
		link, err := provider.PublicURL(ctx, cfg, objectKey, expiresIn)
		if err != nil {
			return nil, "", apperrors.New(50082, http.StatusBadGateway, "生成文件地址失败")
		}
		result.URL = link
		result.ProxyRequired = false
		return result, "", nil
	}

	ticketID, err := s.issueProxyTicket(ctx, storagedomain.ProxyTicket{
		AppID:      session.AppID,
		ConfigID:   cfg.ID,
		ObjectKey:  objectKey,
		Download:   req.Download,
		FileName:   strings.TrimSpace(req.FileName),
		ExpiresAt:  result.ExpiresAt,
		IssuedAt:   time.Now(),
		Provider:   cfg.Provider,
		AccessMode: cfg.AccessMode,
	})
	if err != nil {
		return nil, "", err
	}
	return result, ticketID, nil
}

func (s *StorageService) OpenProxyObject(ctx context.Context, ticketID string) (*storagedomain.ProxyTicket, *storagedomain.Config, *storagedomain.ObjectReader, error) {
	ticket, err := s.readProxyTicket(ctx, ticketID)
	if err != nil {
		return nil, nil, nil, err
	}
	cfg, err := s.pg.GetStorageConfigByID(ctx, ticket.ConfigID)
	if err != nil {
		return nil, nil, nil, err
	}
	if cfg == nil || !cfg.Enabled {
		return nil, nil, nil, apperrors.New(40481, http.StatusNotFound, "资源不可用")
	}
	provider, err := s.buildProvider(cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	reader, err := provider.Open(ctx, cfg, ticket.ObjectKey)
	if err != nil {
		s.log.Warn("storage proxy open failed", zap.Int64("config_id", cfg.ID), zap.String("provider", cfg.Provider), zap.String("key", ticket.ObjectKey), zap.Error(err))
		return nil, nil, nil, apperrors.New(40481, http.StatusNotFound, "资源不可用")
	}
	if reader.FileName == "" {
		reader.FileName = ticket.FileName
	}
	return ticket, cfg, reader, nil
}

func (s *StorageService) resolveConfig(ctx context.Context, opts storagedomain.ResolveOptions) (*storagedomain.Config, error) {
	cacheKey := s.resolvedConfigKey(opts.AppID, opts.ConfigName, opts.Provider)
	if s.redis != nil {
		if raw, err := s.redis.Get(ctx, cacheKey).Bytes(); err == nil && len(raw) > 0 {
			var item storagedomain.Config
			if json.Unmarshal(raw, &item) == nil && item.ID > 0 && item.Enabled {
				return &item, nil
			}
		}
	}
	item, err := s.pg.ResolveStorageConfig(ctx, opts.AppID, opts.ConfigName, opts.Provider)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40482, http.StatusNotFound, "未配置可用存储服务")
	}
	if !item.Enabled {
		return nil, apperrors.New(40084, http.StatusBadRequest, "存储配置未启用")
	}
	if s.redis != nil {
		if raw, err := json.Marshal(item); err == nil {
			_ = s.redis.Set(ctx, cacheKey, raw, 5*time.Minute).Err()
		}
	}
	return item, nil
}

func (s *StorageService) buildProvider(cfg *storagedomain.Config) (storageProvider, error) {
	switch cfg.Provider {
	case storagedomain.ProviderS3:
		if _, err := decodeS3StorageConfig(cfg.ConfigData); err != nil {
			return nil, err
		}
		return newS3StorageProvider(s.httpClient), nil
	case storagedomain.ProviderAliyunOSS:
		if _, err := decodeAliyunOSSConfig(cfg.ConfigData); err != nil {
			return nil, err
		}
		return newAliyunOSSProvider(s.httpClient), nil
	case storagedomain.ProviderTencentCOS:
		if _, err := decodeTencentCOSConfig(cfg.ConfigData); err != nil {
			return nil, err
		}
		return newTencentCOSProvider(s.httpClient), nil
	case storagedomain.ProviderQiniuKodo:
		if _, err := decodeQiniuKodoConfig(cfg.ConfigData); err != nil {
			return nil, err
		}
		return newQiniuKodoProvider(s.httpClient), nil
	case storagedomain.ProviderWebDAV:
		if _, err := decodeWebDAVConfig(cfg.ConfigData); err != nil {
			return nil, err
		}
		return newWebDAVProvider(), nil
	case storagedomain.ProviderOneDrive:
		if _, err := decodeOneDriveConfig(cfg.ConfigData); err != nil {
			return nil, err
		}
		return newOneDriveProvider(s.httpClient, s.redis, s.keyPrefix), nil
	case storagedomain.ProviderDropbox:
		if _, err := decodeDropboxConfig(cfg.ConfigData); err != nil {
			return nil, err
		}
		return newDropboxProvider(s.httpClient, s.redis, s.keyPrefix), nil
	case storagedomain.ProviderGoogleDrive:
		if _, err := decodeGoogleDriveConfig(cfg.ConfigData); err != nil {
			return nil, err
		}
		return newGoogleDriveProvider(s.redis, s.keyPrefix), nil
	case storagedomain.ProviderAzureBlob:
		if _, err := decodeAzureBlobConfig(cfg.ConfigData); err != nil {
			return nil, err
		}
		return newAzureBlobProvider(), nil
	default:
		return nil, apperrors.New(40081, http.StatusBadRequest, "存储提供商不受支持")
	}
}

func (s *StorageService) requireApp(ctx context.Context, appID int64) (appNameHolder, error) {
	app, err := s.pg.GetAppByID(ctx, appID)
	if err != nil {
		return appNameHolder{}, err
	}
	if app == nil {
		return appNameHolder{}, apperrors.New(40410, http.StatusNotFound, "无法找到该应用")
	}
	return appNameHolder{Name: app.Name}, nil
}

func (s *StorageService) validateScopeApp(scope string, appID *int64) error {
	scope = strings.TrimSpace(scope)
	switch scope {
	case storagedomain.ScopeGlobal:
		return nil
	case storagedomain.ScopeApp:
		if appID == nil || *appID <= 0 {
			return apperrors.New(40085, http.StatusBadRequest, "缺少有效的应用标识")
		}
		return nil
	default:
		return apperrors.New(40086, http.StatusBadRequest, "存储作用域无效")
	}
}

func (s *StorageService) issueProxyTicket(ctx context.Context, ticket storagedomain.ProxyTicket) (string, error) {
	if s.redis == nil {
		return "", apperrors.New(50083, http.StatusInternalServerError, "代理票据服务不可用")
	}
	id, err := randomHex(16)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(ticket)
	if err != nil {
		return "", err
	}
	ttl := time.Until(ticket.ExpiresAt)
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	if err := s.redis.Set(ctx, s.proxyTicketKey(id), raw, ttl).Err(); err != nil {
		return "", apperrors.New(50084, http.StatusInternalServerError, "代理票据写入失败")
	}
	return id, nil
}

func (s *StorageService) readProxyTicket(ctx context.Context, ticketID string) (*storagedomain.ProxyTicket, error) {
	if strings.TrimSpace(ticketID) == "" {
		return nil, apperrors.New(40481, http.StatusNotFound, "资源不可用")
	}
	if s.redis == nil {
		return nil, apperrors.New(50083, http.StatusInternalServerError, "代理票据服务不可用")
	}
	raw, err := s.redis.Get(ctx, s.proxyTicketKey(ticketID)).Bytes()
	if err != nil {
		if err == redislib.Nil {
			return nil, apperrors.New(40481, http.StatusNotFound, "资源不可用")
		}
		return nil, apperrors.New(50084, http.StatusInternalServerError, "代理票据读取失败")
	}
	var ticket storagedomain.ProxyTicket
	if err := json.Unmarshal(raw, &ticket); err != nil {
		return nil, apperrors.New(40481, http.StatusNotFound, "资源不可用")
	}
	if !ticket.ExpiresAt.IsZero() && time.Now().After(ticket.ExpiresAt) {
		_ = s.redis.Del(ctx, s.proxyTicketKey(ticketID)).Err()
		return nil, apperrors.New(40481, http.StatusNotFound, "资源不可用")
	}
	return &ticket, nil
}

func (s *StorageService) resolvedConfigKey(appID int64, configName string, provider string) string {
	return fmt.Sprintf("%s:storage:resolved:%d:%s:%s", s.keyPrefix, appID, strings.TrimSpace(configName), strings.TrimSpace(provider))
}

func (s *StorageService) proxyTicketKey(ticketID string) string {
	return fmt.Sprintf("%s:storage:proxy:%s", s.keyPrefix, strings.TrimSpace(ticketID))
}

func (s *StorageService) invalidateResolvedCache(ctx context.Context, cfg *storagedomain.Config) {
	if s.redis == nil || cfg == nil {
		return
	}
	keys := []string{
		s.resolvedConfigKey(0, cfg.ConfigName, cfg.Provider),
		s.resolvedConfigKey(0, cfg.ConfigName, ""),
	}
	if cfg.AppID != nil {
		keys = append(keys,
			s.resolvedConfigKey(*cfg.AppID, cfg.ConfigName, cfg.Provider),
			s.resolvedConfigKey(*cfg.AppID, cfg.ConfigName, ""),
			s.resolvedConfigKey(*cfg.AppID, "", cfg.Provider),
			s.resolvedConfigKey(*cfg.AppID, "", ""),
		)
	}
	_ = s.redis.Del(ctx, keys...).Err()
}

func normalizeRootPath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.Trim(value, "/")
	if value == "" {
		return ""
	}
	return value
}

func normalizeObjectKey(rootPath string, objectKey string) string {
	rootPath = normalizeRootPath(rootPath)
	objectKey = strings.Trim(strings.ReplaceAll(strings.TrimSpace(objectKey), "\\", "/"), "/")
	if rootPath == "" {
		return objectKey
	}
	if objectKey == "" {
		return rootPath
	}
	return rootPath + "/" + objectKey
}

func buildUploadedObjectKey(fileName string) string {
	ext := strings.ToLower(path.Ext(strings.TrimSpace(fileName)))
	now := time.Now().UTC()
	randomPart, _ := randomHex(8)
	return path.Join(now.Format("2006/01/02"), now.Format("150405")+"_"+randomPart+ext)
}

func proxyURLFromRequest(r *http.Request, ticketID string) string {
	if r == nil {
		return "/api/storage/proxy/" + url.PathEscape(ticketID)
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = forwarded
	}
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if host == "" {
		host = r.Host
	}
	return scheme + "://" + host + "/api/storage/proxy/" + url.PathEscape(ticketID)
}

func randomHex(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func isSupportedStorageProvider(value string) bool {
	switch strings.TrimSpace(value) {
	case storagedomain.ProviderS3, storagedomain.ProviderAliyunOSS, storagedomain.ProviderTencentCOS, storagedomain.ProviderQiniuKodo, storagedomain.ProviderWebDAV, storagedomain.ProviderOneDrive, storagedomain.ProviderDropbox, storagedomain.ProviderGoogleDrive, storagedomain.ProviderAzureBlob:
		return true
	default:
		return false
	}
}

func isSupportedAccessMode(value string) bool {
	switch strings.TrimSpace(value) {
	case storagedomain.AccessPublic, storagedomain.AccessPrivate:
		return true
	default:
		return false
	}
}

func decodeStorageConfigData(data map[string]any, target any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, target)
}

func closeSilently(body io.Closer) {
	if body != nil {
		_ = body.Close()
	}
}
