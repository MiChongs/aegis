package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	storagedomain "aegis/internal/domain/storage"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"

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
	return s.pg.ListStorageObjects(ctx, normalizeObjectListQuery(query))
}

// BrowseStorageObjects 目录浏览：一次返回文件、子目录与筛选汇总。
// 三者共用同一组筛选条件，分三个接口取会出现「文件已翻到第 3 页、
// 目录树还停在上一次筛选」这种自相矛盾的画面。
func (s *StorageResourceService) BrowseStorageObjects(ctx context.Context, query storagedomain.ObjectListQuery) (*storagedomain.ObjectListResult, error) {
	query = normalizeObjectListQuery(query)

	items, total, err := s.pg.ListStorageObjects(ctx, query)
	if err != nil {
		return nil, err
	}
	summary, err := s.pg.SummarizeStorageObjects(ctx, query)
	if err != nil {
		return nil, err
	}
	result := &storagedomain.ObjectListResult{
		Items:   items,
		Folders: []storagedomain.ObjectFolder{},
		Total:   total,
		Page:    query.Page,
		Limit:   query.Limit,
		Summary: *summary,
	}
	// 只有目录浏览模式才需要子目录；平铺检索（关键字 / 全局筛选）下
	// 目录聚合既没有意义，还要多扫一次全表。
	if query.FolderView {
		folders, err := s.pg.ListObjectFolders(ctx, query)
		if err != nil {
			return nil, err
		}
		if folders != nil {
			result.Folders = folders
		}
	}
	return result, nil
}

// BatchMutateObjects 批量软删 / 恢复 / 永久删除。
// 逐条调用单条接口是重构前控制台的做法：批量删 20 个文件打 20 个请求，
// 中途失败留下一半状态，且 toast 报的是"已删除 20 个"。
func (s *StorageResourceService) BatchMutateObjects(ctx context.Context, action string, ids []int64) (*storagedomain.BatchObjectResult, error) {
	ids = dedupeInt64(ids)
	result := &storagedomain.BatchObjectResult{Action: action, Requested: len(ids)}
	if len(ids) == 0 {
		return result, nil
	}
	if len(ids) > batchObjectLimit {
		return nil, apperrors.New(40087, http.StatusBadRequest, fmt.Sprintf("单次批量操作最多 %d 个对象", batchObjectLimit))
	}

	var affected int64
	var err error
	switch action {
	case storagedomain.BatchActionDelete:
		affected, err = s.pg.BatchSetObjectStatus(ctx, ids, "deleted")
	case storagedomain.BatchActionRestore:
		affected, err = s.pg.BatchSetObjectStatus(ctx, ids, "active")
	case storagedomain.BatchActionPurge:
		affected, err = s.pg.BatchPermanentDeleteObjects(ctx, ids)
	default:
		return nil, apperrors.New(40088, http.StatusBadRequest, "不支持的批量操作")
	}
	if err != nil {
		return nil, err
	}
	result.Affected = affected
	result.Skipped = int64(len(ids)) - affected
	if result.Skipped < 0 {
		result.Skipped = 0
	}
	return result, nil
}

const batchObjectLimit = 500

// normalizeObjectListQuery 归一化查询参数。
// 分页与排序的兜底放在服务层而不是 handler：管理端、后台任务、
// 未来的批量导出都会调到这里，兜底放在入口处才只写一遍。
func normalizeObjectListQuery(query storagedomain.ObjectListQuery) storagedomain.ObjectListQuery {
	if query.Page < 1 {
		query.Page = 1
	}
	if query.Limit < 1 || query.Limit > 200 {
		query.Limit = 20
	}
	switch query.Sort {
	case storagedomain.ObjectSortSize, storagedomain.ObjectSortFileName,
		storagedomain.ObjectSortObjectKey, storagedomain.ObjectSortDeletedAt:
		// 白名单内，原样保留
	default:
		query.Sort = storagedomain.ObjectSortCreatedAt
	}
	if !strings.EqualFold(query.Order, "asc") {
		query.Order = "desc"
	} else {
		query.Order = "asc"
	}
	query.Folder = strings.Trim(strings.TrimSpace(strings.ReplaceAll(query.Folder, "\\", "/")), "/")
	query.Keyword = strings.TrimSpace(query.Keyword)
	query.Prefix = strings.TrimSpace(query.Prefix)
	query.ContentType = strings.TrimSpace(query.ContentType)
	return query
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
