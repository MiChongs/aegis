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

	// RootPath 已由 StorageService 统一合并，provider 不再重复拼接。
	fullKey := input.ObjectKey

	if err := os.MkdirAll(localCfg.RootDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建存储根目录失败: %w", err)
	}
	absPath, err := secureLocalStoragePath(localCfg.RootDir, fullKey)
	if err != nil {
		return nil, err
	}

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

	absPath, err := secureLocalStoragePath(localCfg.RootDir, objectKey)
	if err != nil {
		return nil, err
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

// secureLocalStoragePath 对本地存储路径做词法与符号链接双重约束。
// 返回值一定处于 rootDir 内部；不存在的末级目录会以最近存在的父目录做符号链接校验。
func secureLocalStoragePath(rootDir string, objectKey string) (string, error) {
	if err := validateStorageObjectKey(objectKey); err != nil {
		return "", err
	}
	absRoot, err := filepath.Abs(strings.TrimSpace(rootDir))
	if err != nil {
		return "", apperrors.New(50083, http.StatusInternalServerError, "本地存储根目录无效")
	}
	absTarget, err := filepath.Abs(filepath.Join(absRoot, filepath.FromSlash(objectKey)))
	if err != nil || !pathWithinRoot(absRoot, absTarget) {
		return "", apperrors.New(40300, http.StatusForbidden, "非法的文件路径")
	}

	resolvedRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", apperrors.New(50083, http.StatusInternalServerError, "本地存储根目录不可访问")
	}
	existing := absTarget
	for {
		if _, statErr := os.Lstat(existing); statErr == nil {
			break
		} else if !os.IsNotExist(statErr) {
			return "", apperrors.New(40300, http.StatusForbidden, "非法的文件路径")
		}
		parent := filepath.Dir(existing)
		if parent == existing {
			return "", apperrors.New(40300, http.StatusForbidden, "非法的文件路径")
		}
		existing = parent
	}
	resolvedExisting, err := filepath.EvalSymlinks(existing)
	if err != nil || !pathWithinRoot(resolvedRoot, resolvedExisting) {
		return "", apperrors.New(40300, http.StatusForbidden, "非法的文件路径")
	}
	return absTarget, nil
}

func pathWithinRoot(root string, target string) bool {
	rel, err := filepath.Rel(root, target)
	if err != nil || filepath.IsAbs(rel) {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
