package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	storagedomain "aegis/internal/domain/storage"
	"aegis/pkg/circuitbreaker"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/resilience"
	"aegis/pkg/timeutil"
)

type circuitBreakeredStorageProvider struct {
	name     string
	upstream storageProvider
}

func wrapStorageProvider(cfg *storagedomain.Config, provider storageProvider) storageProvider {
	if cfg == nil || provider == nil {
		return provider
	}
	if provider.Name() == storagedomain.ProviderLocal {
		return provider
	}
	return &circuitBreakeredStorageProvider{
		name:     storageBreakerName(cfg, provider.Name()),
		upstream: provider,
	}
}

func (p *circuitBreakeredStorageProvider) Name() string {
	return p.upstream.Name()
}

func (p *circuitBreakeredStorageProvider) HealthCheck(ctx context.Context, cfg *storagedomain.Config) (map[string]any, error) {
	breakerName := circuitbreaker.Name(p.name, "health")
	value, err := resilience.Execute(ctx, breakerName, storageResilienceOptions("health"), func(callCtx context.Context) (map[string]any, error) {
		return p.upstream.HealthCheck(callCtx, cfg)
	})
	if err != nil {
		return nil, classifyStorageBreakerError(p.upstream.Name(), err)
	}
	return value, nil
}

func (p *circuitBreakeredStorageProvider) Upload(ctx context.Context, cfg *storagedomain.Config, input storagedomain.UploadInput) (*storagedomain.StoredObject, error) {
	breakerName := circuitbreaker.Name(p.name, "upload")
	value, err := resilience.Execute(ctx, breakerName, storageResilienceOptions("upload"), func(callCtx context.Context) (*storagedomain.StoredObject, error) {
		return p.upstream.Upload(callCtx, cfg, input)
	})
	if err != nil {
		return nil, classifyStorageBreakerError(p.upstream.Name(), err)
	}
	return value, nil
}

func (p *circuitBreakeredStorageProvider) Open(ctx context.Context, cfg *storagedomain.Config, objectKey string) (*storagedomain.ObjectReader, error) {
	breakerName := circuitbreaker.Name(p.name, "open")
	value, err := resilience.Execute(ctx, breakerName, storageResilienceOptions("open"), func(callCtx context.Context) (*storagedomain.ObjectReader, error) {
		return p.upstream.Open(callCtx, cfg, objectKey)
	})
	if err != nil {
		return nil, classifyStorageBreakerError(p.upstream.Name(), err)
	}
	return value, nil
}

func (p *circuitBreakeredStorageProvider) PublicURL(ctx context.Context, cfg *storagedomain.Config, objectKey string, expiresIn time.Duration) (string, error) {
	breakerName := circuitbreaker.Name(p.name, "public-url")
	value, err := resilience.Execute(ctx, breakerName, storageResilienceOptions("public-url"), func(callCtx context.Context) (string, error) {
		return p.upstream.PublicURL(callCtx, cfg, objectKey, expiresIn)
	})
	if err != nil {
		return "", classifyStorageBreakerError(p.upstream.Name(), err)
	}
	return value, nil
}

func storageBreakerName(cfg *storagedomain.Config, provider string) string {
	scope := "global"
	appID := "app-0"
	if cfg != nil {
		if trimmed := strings.TrimSpace(cfg.Scope); trimmed != "" {
			scope = trimmed
		}
		if cfg.AppID != nil {
			appID = fmt.Sprintf("app-%d", *cfg.AppID)
		}
	}
	configID := "config-0"
	configName := "default"
	if cfg != nil {
		if cfg.ID > 0 {
			configID = fmt.Sprintf("config-%d", cfg.ID)
		}
		if trimmed := strings.TrimSpace(cfg.ConfigName); trimmed != "" {
			configName = trimmed
		}
	}
	return circuitbreaker.Name("storage", provider, scope, appID, configID, configName)
}

func classifyStorageBreakerError(provider string, err error) error {
	if circuitbreaker.IsOpenError(err) {
		return apperrors.New(50313, http.StatusServiceUnavailable, fmt.Sprintf("%s 存储通道暂不可用，请稍后再试", storageProviderDisplayName(provider)))
	}
	return err
}

func storageProviderDisplayName(provider string) string {
	switch strings.TrimSpace(provider) {
	case storagedomain.ProviderS3:
		return "S3"
	case storagedomain.ProviderMinIO:
		return "MinIO"
	case storagedomain.ProviderAliyunOSS:
		return "阿里云 OSS"
	case storagedomain.ProviderTencentCOS:
		return "腾讯云 COS"
	case storagedomain.ProviderQiniuKodo:
		return "七牛云 Kodo"
	case storagedomain.ProviderWebDAV:
		return "WebDAV"
	case storagedomain.ProviderOneDrive:
		return "OneDrive"
	case storagedomain.ProviderDropbox:
		return "Dropbox"
	case storagedomain.ProviderGoogleDrive:
		return "Google Drive"
	case storagedomain.ProviderAzureBlob:
		return "Azure Blob"
	default:
		return "对象存储"
	}
}

func storageResilienceOptions(operation string) resilience.Options {
	options := resilience.Options{
		Timeout:     timeutil.Seconds(12),
		MaxRetries:  2,
		BaseBackoff: timeutil.Milliseconds(200),
		MaxBackoff:  timeutil.Milliseconds(1200),
		RatePerSec:  12,
		Burst:       24,
	}
	switch operation {
	case "upload", "open":
		options.Timeout = timeutil.Seconds(20)
		options.MaxRetries = 1
		options.RatePerSec = 8
		options.Burst = 16
	}
	return options
}
