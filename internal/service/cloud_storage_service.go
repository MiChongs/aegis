package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	cloudstorage "aegis/internal/domain/cloudstorage"
	storagedomain "aegis/internal/domain/storage"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
)

// 用户云存储的业务错误码（见 docs/cloud-storage.md 与网关错误目录）。
const (
	errCodeCloudDisabled          = 40360 // 403 应用没有开启云存储
	errCodeCloudFrozen            = 40361 // 403 该用户的云存储已被冻结
	errCodeCloudNamespaceDenied   = 40362 // 403 命名空间不在允许范围内
	errCodeCloudItemNotFound      = 40465 // 404
	errCodeCloudRevisionNotFound  = 40466 // 404
	errCodeCloudRevisionConflict  = 40965 // 409 ifRevision 与当前修订不一致
	errCodeCloudNotInTrash        = 40966 // 409 条目不在回收站里
	errCodeCloudQuotaExceeded     = 41360 // 413 配额不足
	errCodeCloudItemTooLarge      = 41361 // 413 单条目超过上限
	errCodeCloudItemLimit         = 41362 // 413 条目数已达上限
	errCodeCloudInvalidNamespace  = 42260 // 422
	errCodeCloudInvalidKey        = 42261 // 422
	errCodeCloudInvalidContent    = 42262 // 422 内容与声明的编码对不上
	errCodeCloudInvalidMetadata   = 42263 // 422 元数据过大或不是对象
	errCodeCloudContentUnreadable = 50260 // 502 存储桶里读不出内容
)

const (
	cloudStatsDefaultDays = 14
	cloudStatsMaxDays     = 90
	cloudTopUsers         = 8
	// cloudMetadataLimit 元数据序列化后的上限：它进数据库、随列表整页返回，不是放内容的地方。
	cloudMetadataLimit = 4 << 10
	// cloudPurgeInterval 回收站清理周期；cloudPurgeBatch 单个应用每轮最多清多少条。
	cloudPurgeInterval = time.Hour
	cloudPurgeBatch    = 200
	cloudLinkTTL       = 10 * time.Minute
)

// CloudStorageService 应用级用户云存储。
//
// 一次写入分三步，顺序是刻意的：
//
//  1. 把内容传到存储桶（慢、可能失败，但不持有任何数据库锁）
//  2. 事务里校验并发 / 条目数 / 配额，落修订、裁旧修订、重算账目
//  3. 提交之后删掉被裁掉的旧对象；事务失败则删掉第 1 步刚传的那个
//
// 反过来（先事务后上传）要么在事务里等上传、把用户行锁住几秒，要么留下一个
// 指向不存在对象的修订。现在的顺序最坏只是在桶里留一个孤儿对象（删除失败时），
// 不会让任何一个修订读不出来。
type CloudStorageService struct {
	log     *zap.Logger
	pg      *pgrepo.Repository
	storage *StorageService

	stopOnce sync.Once
	stop     chan struct{}
}

// NewCloudStorageService 创建云存储服务。
func NewCloudStorageService(log *zap.Logger, pg *pgrepo.Repository, storage *StorageService) *CloudStorageService {
	if log == nil {
		log = zap.NewNop()
	}
	return &CloudStorageService{log: log, pg: pg, storage: storage, stop: make(chan struct{})}
}

// Start 启动回收站清理循环。多实例同时跑是安全的：清除逐条进事务并重新确认过期。
func (s *CloudStorageService) Start(ctx context.Context) {
	go func() {
		timer := time.NewTimer(time.Minute)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stop:
				return
			case <-timer.C:
				if purged, err := s.PurgeExpired(ctx, 0); err != nil {
					s.log.Warn("云存储回收站清理失败", zap.Error(err))
				} else if purged > 0 {
					s.log.Info("云存储回收站清理完成", zap.Int("purged", purged))
				}
				timer.Reset(cloudPurgeInterval)
			}
		}
	}()
}

// Stop 停止后台循环。
func (s *CloudStorageService) Stop() {
	s.stopOnce.Do(func() { close(s.stop) })
}

// ════════════════════════════════════════════════════════════
//  配置
// ════════════════════════════════════════════════════════════

// CloudStorageOption 控制台下拉框里的一个存储配置。
type CloudStorageOption struct {
	ConfigID   int64  `json:"configId"`
	ConfigName string `json:"configName"`
	Provider   string `json:"provider"`
	Scope      string `json:"scope"`
	AccessMode string `json:"accessMode"`
	Enabled    bool   `json:"enabled"`
	IsDefault  bool   `json:"isDefault"`
}

// CloudStorageAdminConfig 管理端配置视图：配置本身 + 实际写往哪里 + 可选的存储配置。
type CloudStorageAdminConfig struct {
	cloudstorage.Config
	Target         *cloudstorage.StorageTarget `json:"target"`
	StorageOptions []CloudStorageOption        `json:"storageOptions"`
	Limits         CloudStorageCaps            `json:"caps"`
}

// CloudStorageCaps 各项配置的硬上限（控制台表单据此限制输入）。
type CloudStorageCaps struct {
	MaxQuotaBytes         int64 `json:"maxQuotaBytes"`
	MaxItemBytes          int64 `json:"maxItemBytes"`
	MaxItems              int   `json:"maxItems"`
	MaxRevisions          int   `json:"maxRevisions"`
	MaxTrashRetentionDays int   `json:"maxTrashRetentionDays"`
	MaxNamespaces         int   `json:"maxNamespaces"`
}

func (s *CloudStorageService) config(ctx context.Context, appID int64) (cloudstorage.Config, error) {
	cfg, err := s.pg.GetCloudStorageConfig(ctx, appID)
	if err != nil {
		return cloudstorage.Config{}, err
	}
	if cfg == nil {
		return cloudstorage.DefaultConfig(appID), nil
	}
	return *cfg, nil
}

func (s *CloudStorageService) enabledConfig(ctx context.Context, appID int64) (cloudstorage.Config, error) {
	cfg, err := s.config(ctx, appID)
	if err != nil {
		return cfg, err
	}
	if !cfg.Enabled {
		return cfg, apperrors.New(errCodeCloudDisabled, http.StatusForbidden, "云存储暂未开放")
	}
	return cfg, nil
}

// AdminConfig 管理端读取配置。
func (s *CloudStorageService) AdminConfig(ctx context.Context, appID int64) (*CloudStorageAdminConfig, error) {
	cfg, err := s.config(ctx, appID)
	if err != nil {
		return nil, err
	}
	return s.adminConfigView(ctx, cfg)
}

// SaveConfig 管理端保存配置。
func (s *CloudStorageService) SaveConfig(ctx context.Context, input cloudstorage.Config, operator string) (*CloudStorageAdminConfig, error) {
	if err := input.Normalize(); err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	if input.Enabled {
		// 开启时必须能解析出一个可用的存储配置，否则用户的第一次写入才报「未配置存储」。
		if _, err := s.storage.ResolveTarget(ctx, input.AppID, input.StorageConfigName); err != nil {
			if input.StorageConfigName != "" {
				return nil, apperrors.New(40000, http.StatusBadRequest,
					fmt.Sprintf("存储配置 %q 不存在或未启用", input.StorageConfigName))
			}
			return nil, apperrors.New(40000, http.StatusBadRequest, "当前应用没有可用的存储配置，请先在存储管理中添加")
		}
	}
	input.UpdatedBy = operator
	saved, err := s.pg.SaveCloudStorageConfig(ctx, input)
	if err != nil {
		return nil, err
	}
	return s.adminConfigView(ctx, *saved)
}

func (s *CloudStorageService) adminConfigView(ctx context.Context, cfg cloudstorage.Config) (*CloudStorageAdminConfig, error) {
	view := &CloudStorageAdminConfig{
		Config:         cfg,
		Target:         s.resolveTarget(ctx, cfg),
		StorageOptions: []CloudStorageOption{},
		Limits: CloudStorageCaps{
			MaxQuotaBytes:         cloudstorage.MaxQuotaBytes,
			MaxItemBytes:          cloudstorage.MaxItemBytesCap,
			MaxItems:              cloudstorage.MaxItemsCap,
			MaxRevisions:          cloudstorage.MaxRevisionsCap,
			MaxTrashRetentionDays: cloudstorage.MaxTrashRetentionDays,
			MaxNamespaces:         cloudstorage.MaxNamespaces,
		},
	}
	appID := cfg.AppID
	for _, query := range []storagedomain.ListQuery{
		{Scope: storagedomain.ScopeApp, AppID: &appID},
		{Scope: storagedomain.ScopeGlobal},
	} {
		items, err := s.pg.ListStorageConfigs(ctx, query)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			view.StorageOptions = append(view.StorageOptions, CloudStorageOption{
				ConfigID: item.ID, ConfigName: item.ConfigName, Provider: item.Provider, Scope: item.Scope,
				AccessMode: item.AccessMode, Enabled: item.Enabled, IsDefault: item.IsDefault,
			})
		}
	}
	return view, nil
}

// resolveTarget 内容实际会写往哪里；解析失败时把原因带回去给控制台展示。
func (s *CloudStorageService) resolveTarget(ctx context.Context, cfg cloudstorage.Config) *cloudstorage.StorageTarget {
	target, err := s.storage.ResolveTarget(ctx, cfg.AppID, cfg.StorageConfigName)
	if err != nil {
		message := "未找到可用的存储配置"
		if appErr, ok := errors.AsType[*apperrors.AppError](err); ok {
			message = appErr.Message
		}
		return &cloudstorage.StorageTarget{ConfigName: cfg.StorageConfigName, Error: message}
	}
	return &cloudstorage.StorageTarget{
		ConfigID: target.ID, ConfigName: target.ConfigName, Provider: target.Provider, Scope: target.Scope,
	}
}

func limitsOf(cfg cloudstorage.Config, quota int64) cloudstorage.Limits {
	return cloudstorage.Limits{
		QuotaBytes:         quota,
		MaxItemBytes:       cfg.MaxItemBytes,
		MaxItems:           cfg.MaxItems,
		MaxRevisions:       cfg.MaxRevisions,
		TrashRetentionDays: cfg.TrashRetentionDays,
		InlineContentBytes: cloudstorage.InlineContentLimit,
		JSONWriteBytes:     min(cloudstorage.JSONWriteLimit, cfg.MaxItemBytes),
	}
}

// ════════════════════════════════════════════════════════════
//  用户端
// ════════════════════════════════════════════════════════════

// Status 用户端状态。没开启时返回 enabled=false 而不是报错 —— 客户端据此隐藏入口。
func (s *CloudStorageService) Status(ctx context.Context, appID, userID int64) (*cloudstorage.Status, error) {
	cfg, err := s.config(ctx, appID)
	if err != nil {
		return nil, err
	}
	status := &cloudstorage.Status{
		Enabled:            cfg.Enabled,
		Namespaces:         []cloudstorage.NamespaceUsage{},
		Catalog:            cfg.Namespaces,
		RestrictNamespaces: cfg.RestrictNamespaces,
	}
	state, err := s.pg.GetCloudUserState(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	if state == nil {
		state = &cloudstorage.UserState{AppID: appID, UserID: userID}
	}
	status.Frozen = state.Frozen
	status.FrozenReason = state.FrozenReason
	status.Writable = cfg.Enabled && !state.Frozen
	status.UsedBytes = state.UsedBytes
	status.ItemCount = state.ItemCount
	status.TrashCount = state.TrashCount
	status.LastWriteAt = state.LastWriteAt
	status.Limits = limitsOf(cfg, state.EffectiveQuota(cfg))
	if !cfg.Enabled {
		return status, nil
	}
	usage, err := s.pg.CloudNamespaceUsage(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	for i := range usage {
		usage[i].Name = cfg.NamespaceName(usage[i].Namespace)
	}
	status.Namespaces = usage
	return status, nil
}

// ListItems 用户的条目列表。
func (s *CloudStorageService) ListItems(ctx context.Context, query cloudstorage.ItemQuery) (*cloudstorage.ItemPage, error) {
	cfg, err := s.enabledConfig(ctx, query.AppID)
	if err != nil {
		return nil, err
	}
	if query.Namespace != "" && !cloudstorage.ValidNamespace(query.Namespace) {
		return nil, invalidNamespaceError()
	}
	return s.listItems(ctx, cfg, query)
}

func (s *CloudStorageService) listItems(ctx context.Context, cfg cloudstorage.Config, query cloudstorage.ItemQuery) (*cloudstorage.ItemPage, error) {
	if query.Page < 1 {
		query.Page = 1
	}
	if query.Limit < 1 || query.Limit > 100 {
		query.Limit = 50
	}
	page, err := s.pg.ListCloudItems(ctx, query)
	if err != nil {
		return nil, err
	}
	for i := range page.Items {
		decorateItem(&page.Items[i], cfg)
	}
	return page, nil
}

// GetItem 读取条目（revision <= 0 读当前修订）。在回收站里的条目对用户不可读。
func (s *CloudStorageService) GetItem(ctx context.Context, appID, userID int64, namespace, key string, revision int64) (*cloudstorage.ItemContent, error) {
	cfg, err := s.enabledConfig(ctx, appID)
	if err != nil {
		return nil, err
	}
	item, err := s.liveItem(ctx, appID, userID, namespace, key)
	if err != nil {
		return nil, err
	}
	decorateItem(item, cfg)
	return s.itemContent(ctx, item, revision)
}

// CloudWriteInput 一次写入。
type CloudWriteInput struct {
	AppID       int64
	UserID      int64
	Namespace   string
	Key         string
	Data        []byte
	Encoding    string
	ContentType string
	Metadata    map[string]any
	DeviceID    string
	IfRevision  *int64
	Source      string
}

// PutItem 写入条目（新建或覆盖）。
func (s *CloudStorageService) PutItem(ctx context.Context, input CloudWriteInput) (*cloudstorage.Item, error) {
	cfg, err := s.enabledConfig(ctx, input.AppID)
	if err != nil {
		return nil, err
	}
	if input.Source == "" {
		input.Source = cloudstorage.SourceWrite
	}
	return s.write(ctx, cfg, input, cloudWriteOptions{})
}

// DeleteItem 删除条目：默认进回收站；permanent 或回收站保留期为 0 时直接清除。
func (s *CloudStorageService) DeleteItem(ctx context.Context, appID, userID int64, namespace, key string, ifRevision *int64, permanent bool) (*cloudstorage.Item, error) {
	cfg, err := s.enabledConfig(ctx, appID)
	if err != nil {
		return nil, err
	}
	item, err := s.anyItem(ctx, appID, userID, namespace, key)
	if err != nil {
		return nil, err
	}
	return s.deleteItem(ctx, cfg, item, ifRevision, permanent, false)
}

// RestoreItem 从回收站恢复条目。
func (s *CloudStorageService) RestoreItem(ctx context.Context, appID, userID int64, namespace, key string) (*cloudstorage.Item, error) {
	cfg, err := s.enabledConfig(ctx, appID)
	if err != nil {
		return nil, err
	}
	item, err := s.anyItem(ctx, appID, userID, namespace, key)
	if err != nil {
		return nil, err
	}
	restored, err := s.pg.RestoreCloudItem(ctx, appID, userID, item.ID, cfg.MaxItems, false)
	if err != nil {
		return nil, mapCloudError(err)
	}
	decorateItem(restored, cfg)
	return restored, nil
}

// ListRevisions 条目的留存修订。
func (s *CloudStorageService) ListRevisions(ctx context.Context, appID, userID int64, namespace, key string) ([]cloudstorage.Revision, error) {
	if _, err := s.enabledConfig(ctx, appID); err != nil {
		return nil, err
	}
	item, err := s.anyItem(ctx, appID, userID, namespace, key)
	if err != nil {
		return nil, err
	}
	return s.pg.ListCloudRevisions(ctx, item.ID)
}

// Rollback 以某个历史修订的内容生成一个新修订（历史不改写，回滚本身也可回滚）。
func (s *CloudStorageService) Rollback(ctx context.Context, appID, userID int64, namespace, key string, revision int64, ifRevision *int64, deviceID string) (*cloudstorage.Item, error) {
	cfg, err := s.enabledConfig(ctx, appID)
	if err != nil {
		return nil, err
	}
	item, err := s.anyItem(ctx, appID, userID, namespace, key)
	if err != nil {
		return nil, err
	}
	return s.rollback(ctx, cfg, item, revision, ifRevision, deviceID, cloudWriteOptions{})
}

// ItemLink 为条目内容签发一个短时下载地址（大内容不内联时用）。返回值的第二项是代理票据。
func (s *CloudStorageService) ItemLink(ctx context.Context, appID, userID int64, namespace, key string, revision int64, download bool) (*storagedomain.LinkResult, string, error) {
	if _, err := s.enabledConfig(ctx, appID); err != nil {
		return nil, "", err
	}
	item, err := s.liveItem(ctx, appID, userID, namespace, key)
	if err != nil {
		return nil, "", err
	}
	return s.itemLink(ctx, item, revision, download)
}

// ════════════════════════════════════════════════════════════
//  管理端
// ════════════════════════════════════════════════════════════

// Stats 应用级概览。
func (s *CloudStorageService) Stats(ctx context.Context, appID int64, days int) (*cloudstorage.Stats, error) {
	if days <= 0 {
		days = cloudStatsDefaultDays
	}
	if days > cloudStatsMaxDays {
		days = cloudStatsMaxDays
	}
	cfg, err := s.config(ctx, appID)
	if err != nil {
		return nil, err
	}
	loc := timeutil.DefaultLocation()
	now := time.Now().In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	since := today.AddDate(0, 0, -(days - 1))

	stats, err := s.pg.CloudStorageStats(ctx, appID, since, today, loc.String(), cloudTopUsers)
	if err != nil {
		return nil, err
	}
	byDay := make(map[string]cloudstorage.DailyWrites, len(stats.Trend))
	for _, day := range stats.Trend {
		byDay[day.Day] = day
	}
	trend := make([]cloudstorage.DailyWrites, 0, days)
	for offset := 0; offset < days; offset++ {
		date := since.AddDate(0, 0, offset).Format("2006-01-02")
		day, ok := byDay[date]
		if !ok {
			day = cloudstorage.DailyWrites{Day: date}
		}
		trend = append(trend, day)
	}
	stats.Trend = trend
	for i := range stats.Namespaces {
		stats.Namespaces[i].Name = cfg.NamespaceName(stats.Namespaces[i].Namespace)
	}
	for i := range stats.TopUsers {
		stats.TopUsers[i].QuotaBytes = stats.TopUsers[i].EffectiveQuota(cfg)
	}
	stats.StorageTarget = s.resolveTarget(ctx, cfg)
	return stats, nil
}

// ListUsers 管理端用户列表。
func (s *CloudStorageService) ListUsers(ctx context.Context, query cloudstorage.UserQuery) (*cloudstorage.UserPage, error) {
	if query.Page < 1 {
		query.Page = 1
	}
	if query.Limit < 1 || query.Limit > 100 {
		query.Limit = 20
	}
	cfg, err := s.config(ctx, query.AppID)
	if err != nil {
		return nil, err
	}
	page, err := s.pg.ListCloudUsers(ctx, query)
	if err != nil {
		return nil, err
	}
	for i := range page.Items {
		page.Items[i].QuotaBytes = page.Items[i].EffectiveQuota(cfg)
	}
	return page, nil
}

// CloudStorageAdminUser 管理端单用户视图。
type CloudStorageAdminUser struct {
	cloudstorage.UserState
	Enabled    bool                          `json:"enabled"`
	Limits     cloudstorage.Limits           `json:"limits"`
	Namespaces []cloudstorage.NamespaceUsage `json:"namespaces"`
}

// AdminUser 单用户的账目、限制与各命名空间用量。从没用过的用户返回空账目而不是 404。
func (s *CloudStorageService) AdminUser(ctx context.Context, appID, userID int64) (*CloudStorageAdminUser, error) {
	if err := s.requireAppUser(ctx, appID, userID); err != nil {
		return nil, err
	}
	cfg, err := s.config(ctx, appID)
	if err != nil {
		return nil, err
	}
	state, err := s.pg.GetCloudUserState(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	if state == nil {
		state = &cloudstorage.UserState{AppID: appID, UserID: userID}
	}
	state.QuotaBytes = state.EffectiveQuota(cfg)
	usage, err := s.pg.CloudNamespaceUsage(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	for i := range usage {
		usage[i].Name = cfg.NamespaceName(usage[i].Namespace)
	}
	return &CloudStorageAdminUser{
		UserState:  *state,
		Enabled:    cfg.Enabled,
		Limits:     limitsOf(cfg, state.QuotaBytes),
		Namespaces: usage,
	}, nil
}

// CloudUserOverrides 管理端对单个用户的设置。
type CloudUserOverrides struct {
	QuotaBytes   *int64
	Frozen       bool
	FrozenReason string
	Note         string
}

// AdminSaveUser 保存配额覆盖、冻结与备注。
func (s *CloudStorageService) AdminSaveUser(ctx context.Context, appID, userID int64, input CloudUserOverrides, operator string) (*CloudStorageAdminUser, error) {
	if err := s.requireAppUser(ctx, appID, userID); err != nil {
		return nil, err
	}
	if input.QuotaBytes != nil && (*input.QuotaBytes <= 0 || *input.QuotaBytes > cloudstorage.MaxQuotaBytes) {
		return nil, apperrors.New(40000, http.StatusBadRequest,
			fmt.Sprintf("配额需在 1 字节到 %s 之间", cloudstorage.FormatBytes(cloudstorage.MaxQuotaBytes)))
	}
	reason := strings.TrimSpace(input.FrozenReason)
	if !input.Frozen {
		reason = ""
	}
	if len([]rune(reason)) > 255 || len([]rune(strings.TrimSpace(input.Note))) > 255 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "原因或备注过长")
	}
	if _, err := s.pg.SaveCloudUserOverrides(ctx, appID, userID, input.QuotaBytes, input.Frozen, reason,
		strings.TrimSpace(input.Note), operator); err != nil {
		return nil, err
	}
	return s.AdminUser(ctx, appID, userID)
}

// AdminListItems 管理端查看某个用户的条目（含回收站）。
func (s *CloudStorageService) AdminListItems(ctx context.Context, query cloudstorage.ItemQuery) (*cloudstorage.ItemPage, error) {
	if err := s.requireAppUser(ctx, query.AppID, query.UserID); err != nil {
		return nil, err
	}
	cfg, err := s.config(ctx, query.AppID)
	if err != nil {
		return nil, err
	}
	return s.listItems(ctx, cfg, query)
}

// AdminGetItem 管理端读取条目内容（含回收站里的）。
func (s *CloudStorageService) AdminGetItem(ctx context.Context, appID, userID, itemID, revision int64) (*cloudstorage.ItemContent, error) {
	cfg, item, err := s.adminItem(ctx, appID, userID, itemID)
	if err != nil {
		return nil, err
	}
	decorateItem(item, cfg)
	return s.itemContent(ctx, item, revision)
}

// AdminRevisions 管理端查看修订。
func (s *CloudStorageService) AdminRevisions(ctx context.Context, appID, userID, itemID int64) ([]cloudstorage.Revision, error) {
	if _, _, err := s.adminItem(ctx, appID, userID, itemID); err != nil {
		return nil, err
	}
	return s.pg.ListCloudRevisions(ctx, itemID)
}

// AdminRollback 管理端回滚（不受冻结约束，修订上记操作人）。
func (s *CloudStorageService) AdminRollback(ctx context.Context, appID, userID, itemID, revision int64, operator string) (*cloudstorage.Item, error) {
	cfg, item, err := s.adminItem(ctx, appID, userID, itemID)
	if err != nil {
		return nil, err
	}
	return s.rollback(ctx, cfg, item, revision, nil, "", cloudWriteOptions{admin: true, operator: operator})
}

// AdminRestore 管理端从回收站恢复。
func (s *CloudStorageService) AdminRestore(ctx context.Context, appID, userID, itemID int64) (*cloudstorage.Item, error) {
	cfg, _, err := s.adminItem(ctx, appID, userID, itemID)
	if err != nil {
		return nil, err
	}
	restored, err := s.pg.RestoreCloudItem(ctx, appID, userID, itemID, cfg.MaxItems, true)
	if err != nil {
		return nil, mapCloudError(err)
	}
	decorateItem(restored, cfg)
	return restored, nil
}

// AdminDelete 管理端删除：默认进回收站，permanent 时直接清除。
func (s *CloudStorageService) AdminDelete(ctx context.Context, appID, userID, itemID int64, permanent bool) (*cloudstorage.Item, error) {
	cfg, item, err := s.adminItem(ctx, appID, userID, itemID)
	if err != nil {
		return nil, err
	}
	return s.deleteItem(ctx, cfg, item, nil, permanent, true)
}

// AdminItemLink 管理端下载地址。
func (s *CloudStorageService) AdminItemLink(ctx context.Context, appID, userID, itemID, revision int64, download bool) (*storagedomain.LinkResult, string, error) {
	_, item, err := s.adminItem(ctx, appID, userID, itemID)
	if err != nil {
		return nil, "", err
	}
	return s.itemLink(ctx, item, revision, download)
}

// AdminPurgeUser 清空一个用户的全部云存储数据。返回清除的对象数。
func (s *CloudStorageService) AdminPurgeUser(ctx context.Context, appID, userID int64) (int, error) {
	if err := s.requireAppUser(ctx, appID, userID); err != nil {
		return 0, err
	}
	blobs, err := s.pg.PurgeCloudUser(ctx, appID, userID)
	if err != nil {
		return 0, err
	}
	s.deleteBlobs(ctx, blobs)
	return len(blobs), nil
}

// PurgeExpired 清除过了保留期的回收站条目。appID 为 0 时遍历全部应用。返回清除的条目数。
func (s *CloudStorageService) PurgeExpired(ctx context.Context, appID int64) (int, error) {
	var configs []cloudstorage.Config
	if appID > 0 {
		cfg, err := s.config(ctx, appID)
		if err != nil {
			return 0, err
		}
		configs = []cloudstorage.Config{cfg}
	} else {
		items, err := s.pg.ListCloudStorageConfigs(ctx)
		if err != nil {
			return 0, err
		}
		configs = items
	}
	purged := 0
	for _, cfg := range configs {
		before := time.Now().Add(-time.Duration(cfg.TrashRetentionDays) * 24 * time.Hour)
		candidates, err := s.pg.ListExpiredCloudTrash(ctx, cfg.AppID, before, cloudPurgeBatch)
		if err != nil {
			return purged, err
		}
		for _, candidate := range candidates {
			blobs, err := s.pg.PurgeCloudItem(ctx, cfg.AppID, candidate.UserID, candidate.ItemID, &before, true)
			if err != nil {
				if errors.Is(err, pgrepo.ErrCloudItemNotFound) {
					continue
				}
				return purged, err
			}
			if blobs != nil {
				purged++
				s.deleteBlobs(ctx, blobs)
			}
		}
	}
	return purged, nil
}

// ════════════════════════════════════════════════════════════
//  内部
// ════════════════════════════════════════════════════════════

type cloudWriteOptions struct {
	admin        bool
	operator     string
	restoredFrom *int64
}

func (s *CloudStorageService) write(ctx context.Context, cfg cloudstorage.Config, input CloudWriteInput, opts cloudWriteOptions) (*cloudstorage.Item, error) {
	input.Namespace = strings.TrimSpace(input.Namespace)
	input.Key = strings.TrimSpace(input.Key)
	if !cloudstorage.ValidNamespace(input.Namespace) {
		return nil, invalidNamespaceError()
	}
	if !cloudstorage.ValidKey(input.Key) {
		return nil, invalidKeyError()
	}
	if !opts.admin && !cfg.AllowsNamespace(input.Namespace) {
		return nil, apperrors.New(errCodeCloudNamespaceDenied, http.StatusForbidden, "该命名空间不允许写入")
	}
	if input.Encoding == "" {
		input.Encoding = cloudstorage.EncodingJSON
	}
	if !cloudstorage.ValidEncoding(input.Encoding) {
		return nil, apperrors.New(errCodeCloudInvalidContent, http.StatusUnprocessableEntity, "不支持的内容编码")
	}
	input.ContentType = strings.TrimSpace(input.ContentType)
	if input.ContentType == "" {
		input.ContentType = cloudstorage.DefaultContentType(input.Encoding)
	}
	if !cloudstorage.ValidContentType(input.ContentType) {
		return nil, apperrors.New(errCodeCloudInvalidContent, http.StatusUnprocessableEntity, "内容类型格式无效")
	}
	if err := validateCloudMetadata(input.Metadata); err != nil {
		return nil, err
	}
	if len(input.DeviceID) > 128 {
		input.DeviceID = input.DeviceID[:128]
	}
	size := int64(len(input.Data))
	if size > cfg.MaxItemBytes {
		return nil, apperrors.New(errCodeCloudItemTooLarge, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("单个条目不能超过 %s", cloudstorage.FormatBytes(cfg.MaxItemBytes)))
	}

	// 冻结与明显超额先拦一道：否则要先把内容传到存储桶，事务里才报出来。
	state, err := s.pg.GetCloudUserState(ctx, input.AppID, input.UserID)
	if err != nil {
		return nil, err
	}
	if state != nil {
		if state.Frozen && !opts.admin {
			return nil, mapCloudError(pgrepo.ErrCloudFrozen)
		}
		if size > state.EffectiveQuota(cfg) {
			return nil, mapCloudError(&pgrepo.CloudQuotaError{Used: state.UsedBytes, Quota: state.EffectiveQuota(cfg), Need: size})
		}
	}

	digest := sha256.Sum256(input.Data)
	uploaderType := "user"
	if opts.admin {
		uploaderType = "admin"
	}
	stamp, _ := randomHex(6)
	uploaded, err := s.storage.UploadForApp(ctx, input.AppID, storagedomain.UploadInput{
		ConfigName: cfg.StorageConfigName,
		// 每个修订一个独立对象：覆盖写同一个键会让「上传成功、事务失败」把上一个修订的内容毁掉。
		ObjectKey: fmt.Sprintf("cloud/%d/%d/%s/%s/%s-%s", input.AppID, input.UserID, input.Namespace, input.Key,
			time.Now().UTC().Format("20060102T150405"), stamp),
		// 不声明类型、文件名不带扩展名：类型交给魔数识别，免得 JSON 字符串被当成「谎报的文本」留证。
		FileName:      input.Key,
		ContentLength: size,
		Content:       bytes.NewReader(input.Data),
		Metadata:      map[string]string{"aegis-source": "cloud-storage", "aegis-namespace": input.Namespace},
		UploadedBy:    &input.UserID,
		UploaderType:  uploaderType,
	})
	if err != nil {
		return nil, err
	}

	source := input.Source
	if source == "" {
		source = cloudstorage.SourceWrite
	}
	result, err := s.pg.CommitCloudWrite(ctx, pgrepo.CloudWritePlan{
		AppID:           input.AppID,
		UserID:          input.UserID,
		Namespace:       input.Namespace,
		Key:             input.Key,
		IfRevision:      input.IfRevision,
		ContentType:     input.ContentType,
		Encoding:        input.Encoding,
		Size:            size,
		SHA256:          hex.EncodeToString(digest[:]),
		Metadata:        input.Metadata,
		DeviceID:        input.DeviceID,
		Source:          source,
		RestoredFrom:    opts.restoredFrom,
		Operator:        opts.operator,
		StorageConfigID: uploaded.ConfigID,
		ObjectKey:       uploaded.Key,
		QuotaBytes:      cfg.QuotaBytes,
		MaxItems:        cfg.MaxItems,
		MaxRevisions:    cfg.MaxRevisions,
		BypassFrozen:    opts.admin,
	})
	if err != nil {
		s.deleteBlobs(ctx, []cloudstorage.StoredBlob{{StorageConfigID: uploaded.ConfigID, ObjectKey: uploaded.Key}})
		return nil, mapCloudError(err)
	}
	s.deleteBlobs(ctx, result.Pruned)
	decorateItem(result.Item, cfg)
	return result.Item, nil
}

func (s *CloudStorageService) rollback(ctx context.Context, cfg cloudstorage.Config, item *cloudstorage.Item, revision int64, ifRevision *int64, deviceID string, opts cloudWriteOptions) (*cloudstorage.Item, error) {
	if revision <= 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "请指定要回滚到的修订")
	}
	rev, err := s.pg.GetCloudRevision(ctx, item.ID, revision)
	if err != nil {
		return nil, mapCloudError(err)
	}
	data, _, err := s.storage.ReadObjectBytes(ctx, rev.StorageConfigID, rev.ObjectKey, cloudstorage.MaxItemBytesCap)
	if err != nil {
		s.log.Warn("云存储修订读取失败", zap.Int64("item_id", item.ID), zap.Int64("revision", revision), zap.Error(err))
		return nil, apperrors.New(errCodeCloudContentUnreadable, http.StatusBadGateway, "历史修订的内容读取失败")
	}
	from := rev.Revision
	opts.restoredFrom = &from
	return s.write(ctx, cfg, CloudWriteInput{
		AppID:       item.AppID,
		UserID:      item.UserID,
		Namespace:   item.Namespace,
		Key:         item.Key,
		Data:        data,
		Encoding:    rev.Encoding,
		ContentType: rev.ContentType,
		Metadata:    rev.Metadata,
		DeviceID:    deviceID,
		IfRevision:  ifRevision,
		Source:      cloudstorage.SourceRollback,
	}, opts)
}

func (s *CloudStorageService) deleteItem(ctx context.Context, cfg cloudstorage.Config, item *cloudstorage.Item, ifRevision *int64, permanent bool, admin bool) (*cloudstorage.Item, error) {
	if permanent || cfg.TrashRetentionDays == 0 {
		if ifRevision != nil && !item.Deleted && *ifRevision != item.Revision {
			return nil, mapCloudError(&pgrepo.CloudConflictError{Current: item.Revision, Exists: true})
		}
		blobs, err := s.pg.PurgeCloudItem(ctx, item.AppID, item.UserID, item.ID, nil, admin)
		if err != nil {
			return nil, mapCloudError(err)
		}
		s.deleteBlobs(ctx, blobs)
		now := time.Now()
		item.Deleted = true
		item.DeletedAt = &now
		item.PurgeAt = &now
		item.RevisionCount = 0
		item.StoredBytes = 0
		return item, nil
	}
	trashed, err := s.pg.TrashCloudItem(ctx, item.AppID, item.UserID, item.ID, ifRevision, admin)
	if err != nil {
		return nil, mapCloudError(err)
	}
	decorateItem(trashed, cfg)
	return trashed, nil
}

func (s *CloudStorageService) itemContent(ctx context.Context, item *cloudstorage.Item, revision int64) (*cloudstorage.ItemContent, error) {
	rev, err := s.pg.GetCloudRevision(ctx, item.ID, revision)
	if err != nil {
		return nil, mapCloudError(err)
	}
	result := &cloudstorage.ItemContent{Item: *item, ContentRevision: rev.Revision}
	if rev.Size > cloudstorage.InlineContentLimit {
		result.ContentOmitted = true
		return result, nil
	}
	data, _, err := s.storage.ReadObjectBytes(ctx, rev.StorageConfigID, rev.ObjectKey, cloudstorage.InlineContentLimit)
	if err != nil {
		s.log.Warn("云存储内容读取失败", zap.Int64("item_id", item.ID), zap.Int64("revision", rev.Revision), zap.Error(err))
		return nil, apperrors.New(errCodeCloudContentUnreadable, http.StatusBadGateway, "内容读取失败，请稍后重试")
	}
	content, err := cloudstorage.EncodeContent(rev.Encoding, data)
	if err != nil {
		return nil, apperrors.New(errCodeCloudContentUnreadable, http.StatusBadGateway, "内容与记录的编码不符")
	}
	result.Content = content
	return result, nil
}

func (s *CloudStorageService) itemLink(ctx context.Context, item *cloudstorage.Item, revision int64, download bool) (*storagedomain.LinkResult, string, error) {
	rev, err := s.pg.GetCloudRevision(ctx, item.ID, revision)
	if err != nil {
		return nil, "", mapCloudError(err)
	}
	return s.storage.CreatePrivateObjectLink(ctx, item.AppID, rev.StorageConfigID, rev.ObjectKey, download,
		item.Key, cloudLinkTTL)
}

// liveItem 取不在回收站里的条目。
func (s *CloudStorageService) liveItem(ctx context.Context, appID, userID int64, namespace, key string) (*cloudstorage.Item, error) {
	item, err := s.anyItem(ctx, appID, userID, namespace, key)
	if err != nil {
		return nil, err
	}
	if item.Deleted {
		return nil, mapCloudError(pgrepo.ErrCloudItemNotFound)
	}
	return item, nil
}

func (s *CloudStorageService) anyItem(ctx context.Context, appID, userID int64, namespace, key string) (*cloudstorage.Item, error) {
	if !cloudstorage.ValidNamespace(namespace) {
		return nil, invalidNamespaceError()
	}
	if !cloudstorage.ValidKey(key) {
		return nil, invalidKeyError()
	}
	item, err := s.pg.GetCloudItem(ctx, appID, userID, namespace, key)
	if err != nil {
		return nil, mapCloudError(err)
	}
	return item, nil
}

func (s *CloudStorageService) adminItem(ctx context.Context, appID, userID, itemID int64) (cloudstorage.Config, *cloudstorage.Item, error) {
	if err := s.requireAppUser(ctx, appID, userID); err != nil {
		return cloudstorage.Config{}, nil, err
	}
	cfg, err := s.config(ctx, appID)
	if err != nil {
		return cfg, nil, err
	}
	item, err := s.pg.GetCloudItemByID(ctx, appID, userID, itemID)
	if err != nil {
		return cfg, nil, mapCloudError(err)
	}
	return cfg, item, nil
}

// requireAppUser 路径上的 userId 必须属于路径上的应用：否则 A 应用的管理员
// 拿 B 应用用户的 ID 就能读到别人的数据。
func (s *CloudStorageService) requireAppUser(ctx context.Context, appID, userID int64) error {
	if userID <= 0 {
		return apperrors.New(40000, http.StatusBadRequest, "userId 无效")
	}
	ok, err := s.pg.CloudUserBelongsToApp(ctx, appID, userID)
	if err != nil {
		return err
	}
	if !ok {
		return apperrors.New(40410, http.StatusNotFound, "用户不存在")
	}
	return nil
}

// deleteBlobs 删掉存储桶里的对象。尽力而为：失败只记日志 ——
// 账目已经不再算它们，留下的只是一个孤儿对象，不影响任何读写。
// 用一个脱离请求的上下文：客户端断开不该让清理半途而废。
func (s *CloudStorageService) deleteBlobs(ctx context.Context, blobs []cloudstorage.StoredBlob) {
	if len(blobs) == 0 {
		return
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	go func() {
		defer cancel()
		for _, blob := range blobs {
			if err := s.storage.DeleteObject(cleanup, blob.StorageConfigID, blob.ObjectKey); err != nil {
				s.log.Warn("云存储对象删除失败，存储桶里留下一个孤儿对象",
					zap.Int64("config_id", blob.StorageConfigID), zap.String("key", blob.ObjectKey), zap.Error(err))
			}
		}
	}()
}

func decorateItem(item *cloudstorage.Item, cfg cloudstorage.Config) {
	if item == nil || item.DeletedAt == nil {
		return
	}
	purgeAt := item.DeletedAt.Add(time.Duration(cfg.TrashRetentionDays) * 24 * time.Hour)
	item.PurgeAt = &purgeAt
}

func validateCloudMetadata(metadata map[string]any) error {
	if len(metadata) == 0 {
		return nil
	}
	raw, err := json.Marshal(metadata)
	if err != nil || len(raw) > cloudMetadataLimit {
		return apperrors.New(errCodeCloudInvalidMetadata, http.StatusUnprocessableEntity,
			fmt.Sprintf("元数据需为不超过 %s 的 JSON 对象", cloudstorage.FormatBytes(cloudMetadataLimit)))
	}
	return nil
}

func invalidNamespaceError() error {
	return apperrors.New(errCodeCloudInvalidNamespace, http.StatusUnprocessableEntity,
		"命名空间格式无效：需为小写字母或数字开头，可含 . _ -，至多 64 位")
}

func invalidKeyError() error {
	return apperrors.New(errCodeCloudInvalidKey, http.StatusUnprocessableEntity,
		"条目键格式无效：需为字母或数字开头，可含 . _ -，至多 128 位")
}

// mapCloudError 把仓储层的哨兵错误翻译成对外错误码。
func mapCloudError(err error) error {
	if err == nil {
		return nil
	}
	if conflict, ok := errors.AsType[*pgrepo.CloudConflictError](err); ok {
		message := "版本冲突：服务端当前没有这个条目"
		if conflict.Exists {
			message = fmt.Sprintf("版本冲突：服务端当前为第 %d 版", conflict.Current)
		}
		return apperrors.New(errCodeCloudRevisionConflict, http.StatusConflict, message)
	}
	if quota, ok := errors.AsType[*pgrepo.CloudQuotaError](err); ok {
		return apperrors.New(errCodeCloudQuotaExceeded, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("云存储空间不足：已用 %s，共 %s", cloudstorage.FormatBytes(quota.Used), cloudstorage.FormatBytes(quota.Quota)))
	}
	switch {
	case errors.Is(err, pgrepo.ErrCloudItemNotFound):
		return apperrors.New(errCodeCloudItemNotFound, http.StatusNotFound, "条目不存在")
	case errors.Is(err, pgrepo.ErrCloudRevisionNotFound):
		return apperrors.New(errCodeCloudRevisionNotFound, http.StatusNotFound, "修订不存在或已被清理")
	case errors.Is(err, pgrepo.ErrCloudFrozen):
		return apperrors.New(errCodeCloudFrozen, http.StatusForbidden, "云存储已被冻结，暂时只能读取")
	case errors.Is(err, pgrepo.ErrCloudItemLimit):
		return apperrors.New(errCodeCloudItemLimit, http.StatusRequestEntityTooLarge, "条目数量已达上限")
	case errors.Is(err, pgrepo.ErrCloudNotInTrash):
		return apperrors.New(errCodeCloudNotInTrash, http.StatusConflict, "条目不在回收站中")
	}
	return err
}
