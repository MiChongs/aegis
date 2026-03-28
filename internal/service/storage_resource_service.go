package service

import (
	"context"
	"time"

	storagedomain "aegis/internal/domain/storage"
	pgrepo "aegis/internal/repository/postgres"

	"go.uber.org/zap"
)

// StorageResourceService 存储资源中心业务逻辑
type StorageResourceService struct {
	log *zap.Logger
	pg  *pgrepo.Repository
}

// NewStorageResourceService 创建存储资源服务
func NewStorageResourceService(log *zap.Logger, pg *pgrepo.Repository) *StorageResourceService {
	return &StorageResourceService{log: log, pg: pg}
}

// ════════════════════════════════════════════════════════════
//  文件管理
// ════════════════════════════════════════════════════════════

func (s *StorageResourceService) IndexStorageObject(ctx context.Context, obj storagedomain.StorageObject) (*storagedomain.StorageObject, error) {
	return s.pg.IndexStorageObject(ctx, obj)
}

func (s *StorageResourceService) ListStorageObjects(ctx context.Context, query storagedomain.ObjectListQuery) ([]storagedomain.StorageObject, int64, error) {
	return s.pg.ListStorageObjects(ctx, query)
}

func (s *StorageResourceService) GetStorageObject(ctx context.Context, id int64) (*storagedomain.StorageObject, error) {
	return s.pg.GetStorageObject(ctx, id)
}

func (s *StorageResourceService) SoftDeleteStorageObject(ctx context.Context, id int64) error {
	return s.pg.SoftDeleteStorageObject(ctx, id)
}

func (s *StorageResourceService) RestoreStorageObject(ctx context.Context, id int64) error {
	return s.pg.RestoreStorageObject(ctx, id)
}

func (s *StorageResourceService) PermanentDeleteStorageObject(ctx context.Context, id int64) error {
	return s.pg.PermanentDeleteStorageObject(ctx, id)
}

func (s *StorageResourceService) ListDeletedObjects(ctx context.Context, configID *int64, page, limit int) ([]storagedomain.StorageObject, int64, error) {
	return s.pg.ListDeletedObjects(ctx, configID, page, limit)
}

func (s *StorageResourceService) CleanupDeletedObjects(ctx context.Context, olderThan time.Duration) (int64, error) {
	return s.pg.CleanupDeletedObjects(ctx, olderThan)
}

// ════════════════════════════════════════════════════════════
//  规则管理
// ════════════════════════════════════════════════════════════

func (s *StorageResourceService) CreateStorageRule(ctx context.Context, input storagedomain.CreateRuleInput) (*storagedomain.StorageRule, error) {
	return s.pg.CreateStorageRule(ctx, input)
}

func (s *StorageResourceService) ListStorageRules(ctx context.Context, configID *int64, appID *int64) ([]storagedomain.StorageRule, error) {
	return s.pg.ListStorageRules(ctx, configID, appID)
}

func (s *StorageResourceService) UpdateStorageRule(ctx context.Context, id int64, name string, ruleData map[string]any, isActive bool) error {
	return s.pg.UpdateStorageRule(ctx, id, name, ruleData, isActive)
}

func (s *StorageResourceService) DeleteStorageRule(ctx context.Context, id int64) error {
	return s.pg.DeleteStorageRule(ctx, id)
}

func (s *StorageResourceService) GetActiveUploadRules(ctx context.Context, configID int64, appID *int64) ([]storagedomain.StorageRule, error) {
	return s.pg.GetActiveUploadRules(ctx, configID, appID)
}

// ════════════════════════════════════════════════════════════
//  CDN 配置
// ════════════════════════════════════════════════════════════

func (s *StorageResourceService) UpsertCDNConfig(ctx context.Context, configID int64, input storagedomain.UpsertCDNConfigInput) (*storagedomain.CDNConfig, error) {
	return s.pg.UpsertCDNConfig(ctx, configID, input)
}

func (s *StorageResourceService) GetCDNConfig(ctx context.Context, configID int64) (*storagedomain.CDNConfig, error) {
	return s.pg.GetCDNConfig(ctx, configID)
}

func (s *StorageResourceService) DeleteCDNConfig(ctx context.Context, configID int64) error {
	return s.pg.DeleteCDNConfig(ctx, configID)
}

// ════════════════════════════════════════════════════════════
//  图片规则
// ════════════════════════════════════════════════════════════

func (s *StorageResourceService) CreateImageRule(ctx context.Context, input storagedomain.CreateImageRuleInput) (*storagedomain.ImageRule, error) {
	return s.pg.CreateImageRule(ctx, input)
}

func (s *StorageResourceService) ListImageRules(ctx context.Context, configID *int64) ([]storagedomain.ImageRule, error) {
	return s.pg.ListImageRules(ctx, configID)
}

func (s *StorageResourceService) DeleteImageRule(ctx context.Context, id int64) error {
	return s.pg.DeleteImageRule(ctx, id)
}

// ════════════════════════════════════════════════════════════
//  用量统计
// ════════════════════════════════════════════════════════════

func (s *StorageResourceService) CreateUsageSnapshot(ctx context.Context, snapshot storagedomain.UsageSnapshot) error {
	return s.pg.CreateUsageSnapshot(ctx, snapshot)
}

func (s *StorageResourceService) GetLatestUsageSnapshot(ctx context.Context, configID int64) (*storagedomain.UsageSnapshot, error) {
	return s.pg.GetLatestUsageSnapshot(ctx, configID)
}

func (s *StorageResourceService) GetUsageHistory(ctx context.Context, configID int64, days int) ([]storagedomain.UsageSnapshot, error) {
	return s.pg.GetUsageHistory(ctx, configID, days)
}

func (s *StorageResourceService) GetObjectTypeStats(ctx context.Context, configID *int64) ([]storagedomain.TypeStat, error) {
	return s.pg.GetObjectTypeStats(ctx, configID)
}

// CollectUsageSnapshots 遍历所有存储配置，采集用量快照
func (s *StorageResourceService) CollectUsageSnapshots(ctx context.Context) error {
	configs, err := s.pg.ListStorageConfigs(ctx, storagedomain.ListQuery{})
	if err != nil {
		return err
	}
	for _, cfg := range configs {
		// 统计该配置下的文件数量和大小
		objects, _, err := s.pg.ListStorageObjects(ctx, storagedomain.ObjectListQuery{ConfigID: &cfg.ID, Status: "active", Page: 1, Limit: 1})
		if err != nil {
			s.log.Warn("采集用量快照失败", zap.Int64("configId", cfg.ID), zap.Error(err))
			continue
		}
		_ = objects // 仅用于触发查询

		// 使用聚合查询获取精确数据
		var totalFiles, totalSize, activeFiles, deletedFiles int64
		typeStats, err := s.pg.GetObjectTypeStats(ctx, &cfg.ID)
		if err != nil {
			s.log.Warn("获取类型统计失败", zap.Int64("configId", cfg.ID), zap.Error(err))
			continue
		}
		for _, ts := range typeStats {
			activeFiles += ts.Count
			totalSize += ts.Size
		}
		totalFiles = activeFiles

		// 统计已删除文件数
		_, deletedCount, err := s.pg.ListDeletedObjects(ctx, &cfg.ID, 1, 1)
		if err == nil {
			deletedFiles = deletedCount
			totalFiles += deletedFiles
		}

		snapshot := storagedomain.UsageSnapshot{
			ConfigID:     cfg.ID,
			AppID:        cfg.AppID,
			TotalFiles:   totalFiles,
			TotalSize:    totalSize,
			ActiveFiles:  activeFiles,
			DeletedFiles: deletedFiles,
		}
		if err := s.pg.CreateUsageSnapshot(ctx, snapshot); err != nil {
			s.log.Warn("写入用量快照失败", zap.Int64("configId", cfg.ID), zap.Error(err))
		}
	}
	return nil
}

// GetUsageStats 获取存储用量统计（组合最新快照 + 类型统计）
// GetUsageStats 获取用量统计（configID=0 时返回全局汇总，实时从 storage_objects 聚合）
func (s *StorageResourceService) GetUsageStats(ctx context.Context, configID int64) (*storagedomain.UsageStats, error) {
	stats := &storagedomain.UsageStats{ConfigID: configID}

	// 实时聚合（不依赖快照，始终准确）
	realtime, err := s.pg.GetRealtimeUsageStats(ctx, configID)
	if err != nil {
		return nil, err
	}
	if realtime != nil {
		stats.TotalFiles = realtime.TotalFiles
		stats.TotalSize = realtime.TotalSize
		stats.ActiveFiles = realtime.ActiveFiles
		stats.DeletedFiles = realtime.DeletedFiles
	}

	// 获取类型统计
	var cfgPtr *int64
	if configID > 0 {
		cfgPtr = &configID
	}
	typeStats, err := s.pg.GetObjectTypeStats(ctx, cfgPtr)
	if err != nil {
		return nil, err
	}
	stats.TopTypes = typeStats
	if stats.TopTypes == nil {
		stats.TopTypes = []storagedomain.TypeStat{}
	}
	return stats, nil
}
