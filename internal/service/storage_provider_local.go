package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	storagedomain "aegis/internal/domain/storage"
	apperrors "aegis/pkg/errors"
)

// localStorageProvider 本地文件系统存储提供商
type localStorageProvider struct{}

func newLocalStorageProvider() storageProvider {
	return &localStorageProvider{}
}

func (p *localStorageProvider) Name() string { return storagedomain.ProviderLocal }

func (p *localStorageProvider) HealthCheck(_ context.Context, cfg *storagedomain.Config) (map[string]any, error) {
	localCfg, err := decodeLocalConfig(cfg.ConfigData)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(localCfg.RootDir)
	if err != nil {
		return nil, fmt.Errorf("存储目录不可访问: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("存储路径不是目录: %s", localCfg.RootDir)
	}
	return map[string]any{
		"root_dir": localCfg.RootDir,
		"writable": true,
	}, nil
}

func (p *localStorageProvider) Upload(_ context.Context, cfg *storagedomain.Config, input storagedomain.UploadInput) (*storagedomain.StoredObject, error) {
	localCfg, err := decodeLocalConfig(cfg.ConfigData)
	if err != nil {
		return nil, err
	}

	// 构建完整路径
	rootPath := strings.TrimSpace(cfg.RootPath)
	objectKey := input.ObjectKey
	fullKey := objectKey
	if rootPath != "" {
		fullKey = strings.TrimRight(rootPath, "/") + "/" + strings.TrimLeft(objectKey, "/")
	}

	absPath := filepath.Join(localCfg.RootDir, filepath.FromSlash(fullKey))

	// 确保目录存在
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建存储目录失败: %w", err)
	}

	// 写文件
	f, err := os.Create(absPath)
	if err != nil {
		return nil, fmt.Errorf("创建文件失败: %w", err)
	}
	defer f.Close()

	written, err := io.Copy(f, input.Content)
	if err != nil {
		return nil, fmt.Errorf("写入文件失败: %w", err)
	}

	// 构建访问 URL
	url := ""
	if cfg.BaseURL != "" {
		baseURL := strings.TrimRight(cfg.BaseURL, "/")
		url = baseURL + "/" + strings.TrimLeft(fullKey, "/")
	}

	return &storagedomain.StoredObject{
		ConfigID:      cfg.ID,
		Provider:      storagedomain.ProviderLocal,
		Key:           fullKey,
		FileName:      input.FileName,
		Size:          written,
		ContentType:   input.ContentType,
		URL:           url,
		AccessMode:    cfg.AccessMode,
		ProxyRequired: cfg.ProxyDownload,
	}, nil
}

func (p *localStorageProvider) Open(_ context.Context, cfg *storagedomain.Config, objectKey string) (*storagedomain.ObjectReader, error) {
	localCfg, err := decodeLocalConfig(cfg.ConfigData)
	if err != nil {
		return nil, err
	}

	absPath := filepath.Join(localCfg.RootDir, filepath.FromSlash(objectKey))

	// 安全检查：防止路径遍历
	absRoot, _ := filepath.Abs(localCfg.RootDir)
	absTarget, _ := filepath.Abs(absPath)
	if !strings.HasPrefix(absTarget, absRoot) {
		return nil, apperrors.New(40300, http.StatusForbidden, "非法的文件路径")
	}

	f, err := os.Open(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, apperrors.New(40401, http.StatusNotFound, "文件不存在")
		}
		return nil, fmt.Errorf("打开文件失败: %w", err)
	}

	stat, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("获取文件信息失败: %w", err)
	}

	ct := mime.TypeByExtension(filepath.Ext(objectKey))
	if ct == "" {
		ct = "application/octet-stream"
	}

	modTime := stat.ModTime()
	return &storagedomain.ObjectReader{
		Body:         f,
		Size:         stat.Size(),
		ContentType:  ct,
		FileName:     filepath.Base(objectKey),
		LastModified: &modTime,
	}, nil
}

func (p *localStorageProvider) PublicURL(_ context.Context, cfg *storagedomain.Config, objectKey string, _ time.Duration) (string, error) {
	if cfg.BaseURL == "" {
		return "", apperrors.New(40083, http.StatusBadRequest, "本地存储未配置 BaseURL")
	}
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	return baseURL + "/" + strings.TrimLeft(objectKey, "/"), nil
}

// decodeLocalConfig 解析本地存储配置
func decodeLocalConfig(data map[string]any) (*storagedomain.LocalConfig, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, apperrors.New(40082, http.StatusBadRequest, "存储配置格式错误")
	}
	var cfg storagedomain.LocalConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, apperrors.New(40082, http.StatusBadRequest, "存储配置格式错误")
	}
	if strings.TrimSpace(cfg.RootDir) == "" {
		cfg.RootDir = "data/storage"
	}
	return &cfg, nil
}
