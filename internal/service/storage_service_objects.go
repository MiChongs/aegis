package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	storagedomain "aegis/internal/domain/storage"
	apperrors "aegis/pkg/errors"

	"go.uber.org/zap"
)

// 存储服务给其它业务模块用的两个对象级入口：解析写往哪里、删掉一个对象。
//
// 上传仍走 UploadForApp（治理闸门、魔数识别、索引都在那条路径上），
// 这里补的是那条路径之外缺的两块。

// ResolveTarget 解析应用按 configName 会写往的存储配置（不出网）。
// 空 configName 与 /storage/upload 不带 config_name 时的解析完全一致。
func (s *StorageService) ResolveTarget(ctx context.Context, appID int64, configName string) (*storagedomain.Config, error) {
	return s.resolveConfig(ctx, storagedomain.ResolveOptions{AppID: appID, ConfigName: strings.TrimSpace(configName)})
}

// DeleteObject 从存储桶里物理删除一个对象，并移除它在存储中心的索引行。
//
// objectKey 是索引里存的完整键（已含 root_path），与 CreateIndexedObjectLink 同一口径。
// 提供商不支持删除时对象留在桶里、只记一条日志，索引照样移除 —— 返回 nil，
// 因为调用方（修订裁剪、回收站清理）对此无能为力，报错只会让它们反复重试。
// 存储配置本身已被删除时同理：对象再也够不着了，只能清索引。
func (s *StorageService) DeleteObject(ctx context.Context, configID int64, objectKey string) error {
	if err := validateStorageObjectKey(objectKey); err != nil {
		return err
	}
	cfg, err := s.pg.GetStorageConfigByID(ctx, configID)
	if err != nil {
		return err
	}
	if cfg != nil {
		provider, err := s.buildProvider(cfg)
		if err != nil {
			return err
		}
		if err := deleteStoredObject(ctx, provider, cfg, objectKey); err != nil {
			if !errors.Is(err, errStorageDeleteUnsupported) {
				return err
			}
			s.log.Warn("存储提供商不支持物理删除，对象保留在存储桶中",
				zap.Int64("config_id", configID), zap.String("provider", cfg.Provider), zap.String("key", objectKey))
		}
	} else {
		s.log.Warn("存储配置已不存在，仅移除对象索引",
			zap.Int64("config_id", configID), zap.String("key", objectKey))
	}
	return s.pg.DeleteStorageObjectIndex(ctx, configID, objectKey)
}

// CreatePrivateObjectLink 为对象签发**代理**下载地址，无论存储配置是不是公开访问。
//
// 公开配置下 signObjectLink 会交出一个永久直链；用户的私有数据（云存储）
// 不能因为管理员把应用的默认存储设成了公开桶，就变成一个永远有效的地址。
// 代理票据有时效，过期即失效。
func (s *StorageService) CreatePrivateObjectLink(ctx context.Context, appID int64, configID int64, objectKey string, download bool, fileName string, expiresIn time.Duration) (*storagedomain.LinkResult, string, error) {
	cfg, err := s.pg.GetStorageConfigByID(ctx, configID)
	if err != nil {
		return nil, "", err
	}
	if cfg == nil || !cfg.Enabled {
		return nil, "", apperrors.New(40482, http.StatusNotFound, "未配置可用存储服务")
	}
	private := *cfg
	private.AccessMode = storagedomain.AccessPrivate
	private.ProxyDownload = true
	return s.signObjectLink(ctx, appID, &private, objectKey, download, fileName, expiresIn)
}
