package service

import (
	storagedomain "aegis/internal/domain/storage"
	apperrors "aegis/pkg/errors"
	"net/http"
	"strings"
)

func decodeS3StorageConfig(data map[string]any) (*storagedomain.S3Config, error) {
	var cfg storagedomain.S3Config
	if err := decodeStorageConfigData(data, &cfg); err != nil {
		return nil, apperrors.New(40087, http.StatusBadRequest, "S3 配置解析失败")
	}
	if strings.TrimSpace(cfg.Endpoint) == "" || strings.TrimSpace(cfg.Region) == "" || strings.TrimSpace(cfg.Bucket) == "" || strings.TrimSpace(cfg.AccessKeyID) == "" || strings.TrimSpace(cfg.SecretAccessKey) == "" {
		return nil, apperrors.New(40088, http.StatusBadRequest, "S3 配置不完整")
	}
	return &cfg, nil
}

func decodeAliyunOSSConfig(data map[string]any) (*storagedomain.AliyunOSSConfig, error) {
	var cfg storagedomain.AliyunOSSConfig
	if err := decodeStorageConfigData(data, &cfg); err != nil {
		return nil, apperrors.New(40089, http.StatusBadRequest, "阿里云 OSS 配置解析失败")
	}
	if strings.TrimSpace(cfg.Endpoint) == "" || strings.TrimSpace(cfg.Bucket) == "" || strings.TrimSpace(cfg.AccessKeyID) == "" || strings.TrimSpace(cfg.AccessKeySecret) == "" {
		return nil, apperrors.New(40090, http.StatusBadRequest, "阿里云 OSS 配置不完整")
	}
	return &cfg, nil
}

func decodeTencentCOSConfig(data map[string]any) (*storagedomain.TencentCOSConfig, error) {
	var cfg storagedomain.TencentCOSConfig
	if err := decodeStorageConfigData(data, &cfg); err != nil {
		return nil, apperrors.New(40091, http.StatusBadRequest, "腾讯云 COS 配置解析失败")
	}
	if strings.TrimSpace(cfg.BucketURL) == "" && strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, apperrors.New(40092, http.StatusBadRequest, "腾讯云 COS BucketURL 不能为空")
	}
	if strings.TrimSpace(cfg.SecretID) == "" || strings.TrimSpace(cfg.SecretKey) == "" {
		return nil, apperrors.New(40093, http.StatusBadRequest, "腾讯云 COS 配置不完整")
	}
	return &cfg, nil
}

func decodeQiniuKodoConfig(data map[string]any) (*storagedomain.QiniuKodoConfig, error) {
	var cfg storagedomain.QiniuKodoConfig
	if err := decodeStorageConfigData(data, &cfg); err != nil {
		return nil, apperrors.New(40094, http.StatusBadRequest, "七牛云 Kodo 配置解析失败")
	}
	if strings.TrimSpace(cfg.Region) == "" || strings.TrimSpace(cfg.Bucket) == "" || strings.TrimSpace(cfg.AccessKey) == "" || strings.TrimSpace(cfg.SecretKey) == "" || strings.TrimSpace(cfg.Domain) == "" {
		return nil, apperrors.New(40095, http.StatusBadRequest, "七牛云 Kodo 配置不完整")
	}
	return &cfg, nil
}

func decodeWebDAVConfig(data map[string]any) (*storagedomain.WebDAVConfig, error) {
	var cfg storagedomain.WebDAVConfig
	if err := decodeStorageConfigData(data, &cfg); err != nil {
		return nil, apperrors.New(40096, http.StatusBadRequest, "WebDAV 配置解析失败")
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, apperrors.New(40097, http.StatusBadRequest, "WebDAV Endpoint 不能为空")
	}
	return &cfg, nil
}

func decodeOneDriveConfig(data map[string]any) (*storagedomain.OneDriveConfig, error) {
	var cfg storagedomain.OneDriveConfig
	if err := decodeStorageConfigData(data, &cfg); err != nil {
		return nil, apperrors.New(40099, http.StatusBadRequest, "OneDrive 配置解析失败")
	}
	if strings.TrimSpace(cfg.DriveID) == "" {
		return nil, apperrors.New(40100, http.StatusBadRequest, "OneDrive DriveID 不能为空")
	}
	if strings.TrimSpace(cfg.AccessToken) == "" && strings.TrimSpace(cfg.RefreshToken) == "" {
		return nil, apperrors.New(40101, http.StatusBadRequest, "OneDrive AccessToken 或 RefreshToken 至少需要一个")
	}
	return &cfg, nil
}

func decodeDropboxConfig(data map[string]any) (*storagedomain.DropboxConfig, error) {
	var cfg storagedomain.DropboxConfig
	if err := decodeStorageConfigData(data, &cfg); err != nil {
		return nil, apperrors.New(40106, http.StatusBadRequest, "Dropbox 配置解析失败")
	}
	if strings.TrimSpace(cfg.AccessToken) == "" && strings.TrimSpace(cfg.RefreshToken) == "" {
		return nil, apperrors.New(40107, http.StatusBadRequest, "Dropbox AccessToken 或 RefreshToken 至少需要一个")
	}
	return &cfg, nil
}

func decodeGoogleDriveConfig(data map[string]any) (*storagedomain.GoogleDriveConfig, error) {
	var cfg storagedomain.GoogleDriveConfig
	if err := decodeStorageConfigData(data, &cfg); err != nil {
		return nil, apperrors.New(40108, http.StatusBadRequest, "Google Drive 配置解析失败")
	}
	if strings.TrimSpace(cfg.AccessToken) == "" && strings.TrimSpace(cfg.RefreshToken) == "" {
		return nil, apperrors.New(40109, http.StatusBadRequest, "Google Drive AccessToken 或 RefreshToken 至少需要一个")
	}
	return &cfg, nil
}

func decodeAzureBlobConfig(data map[string]any) (*storagedomain.AzureBlobConfig, error) {
	var cfg storagedomain.AzureBlobConfig
	if err := decodeStorageConfigData(data, &cfg); err != nil {
		return nil, apperrors.New(40110, http.StatusBadRequest, "Azure Blob 配置解析失败")
	}
	if strings.TrimSpace(cfg.Container) == "" {
		return nil, apperrors.New(40111, http.StatusBadRequest, "Azure Blob Container 不能为空")
	}
	if strings.TrimSpace(cfg.ConnectionString) == "" {
		if strings.TrimSpace(cfg.AccountURL) == "" || strings.TrimSpace(cfg.AccountName) == "" || strings.TrimSpace(cfg.AccountKey) == "" {
			return nil, apperrors.New(40112, http.StatusBadRequest, "Azure Blob 配置不完整")
		}
	}
	return &cfg, nil
}
