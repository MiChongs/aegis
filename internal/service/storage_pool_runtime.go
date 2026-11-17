package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"go.uber.org/zap"

	storagedomain "aegis/internal/domain/storage"
	apperrors "aegis/pkg/errors"
)

// ════════════════════════════════════════════════════════════
//  后台循环
// ════════════════════════════════════════════════════════════

const (
	storageProbeInterval    = 30 * time.Second
	storageUsageInterval    = 5 * time.Minute
	storageSnapshotInterval = 24 * time.Hour
	storageEventRetention   = 30 * 24 * time.Hour
	storageProbeTimeout     = 15 * time.Second
)

// reportedUsage 提供商报告的真实用量（R2 分析接口），只在后台刷新。
var reportedUsage = struct {
	sync.Mutex
	values map[int64]storagedomain.MemberUsage
}{values: map[int64]storagedomain.MemberUsage{}}

func (s *StorageService) reportedUsageSnapshot() map[int64]storagedomain.MemberUsage {
	reportedUsage.Lock()
	defer reportedUsage.Unlock()
	copied := make(map[int64]storagedomain.MemberUsage, len(reportedUsage.values))
	for k, v := range reportedUsage.values {
		copied[k] = v
	}
	return copied
}

// Start 启动存储的后台循环：探活恢复、用量刷新与容量预警、事件清理、每日用量快照、恢复迁移任务。
func (s *StorageService) Start(ctx context.Context) {
	go s.loop(ctx)
	s.migrations.resume(ctx)
}

// Stop 停止后台循环与迁移任务。
func (s *StorageService) Stop() {
	s.stopOnce.Do(func() { close(s.stop) })
	s.migrations.stopAll()
}

func (s *StorageService) loop(ctx context.Context) {
	probe := time.NewTicker(storageProbeInterval)
	usage := time.NewTicker(storageUsageInterval)
	daily := time.NewTicker(storageSnapshotInterval)
	defer probe.Stop()
	defer usage.Stop()
	defer daily.Stop()
	// 接上集群后才需要周期性重扫迁移任务（接手宕机实例留下的）；单实例下这个通道永远不触发。
	var rescan <-chan time.Time
	if s.migrationRescanEnabled() {
		ticker := time.NewTicker(storageMigrationRescan)
		defer ticker.Stop()
		rescan = ticker.C
	}
	// 启动后先刷一次用量，让总览一打开就有数。
	s.refreshUsage(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stop:
			return
		case <-probe.C:
			// 探活结果记在本进程的健康表里（每个实例各自的熔断状态），每个实例都要探
			s.probeBackoffMembers(ctx)
		case <-usage.C:
			s.refreshUsage(ctx)
		case <-daily.C:
			if s.clusterState.dailyTask.owned() {
				s.dailyMaintenance(ctx)
			}
		case <-rescan:
			s.migrations.resume(ctx)
		}
	}
}

// probeBackoffMembers 冷却期满的成员主动做一次连通检测：成功就恢复，失败就继续退避。
// 不等真实写入去撞 —— 那样恢复前的第一个写入请求要多等一次超时。
func (s *StorageService) probeBackoffMembers(ctx context.Context) {
	configs, err := s.pg.ListStorageConfigs(ctx, storagedomain.ListQuery{})
	if err != nil {
		return
	}
	now := time.Now()
	for i := range configs {
		cfg := &configs[i]
		health := s.pools.Health(cfg.ID)
		if !cfg.Enabled || health.BackoffUntil == nil || now.Before(*health.BackoffUntil) {
			continue
		}
		provider, err := s.buildProvider(cfg)
		if err != nil {
			continue
		}
		probeCtx, cancel := context.WithTimeout(ctx, storageProbeTimeout)
		started := time.Now()
		_, err = provider.HealthCheck(probeCtx, cfg)
		cancel()
		if err == nil {
			if s.pools.RecordSuccess(cfg.ID, time.Since(started), true) {
				s.emitPoolEvent(ctx, 0, cfg.ID, storagedomain.EventLevelInfo, storagedomain.EventRecovered,
					fmt.Sprintf("%s 探活成功，已恢复写入", cfg.ConfigName), nil, "")
			}
			continue
		}
		cooldown := s.pools.RecordFailure(cfg.ID, err, true)
		s.log.Info("存储探活失败，继续退避", zap.Int64("config_id", cfg.ID), zap.Duration("cooldown", cooldown), zap.Error(err))
	}
}

// refreshUsage 重算索引用量，并向能报告真实用量的提供商（R2 + API 令牌）取一次，
// 然后检查各池成员是否越过预警线。
func (s *StorageService) refreshUsage(ctx context.Context) {
	indexed, err := s.pg.StorageConfigUsage(ctx)
	if err != nil {
		s.log.Warn("存储用量统计失败", zap.Error(err))
		return
	}
	configs, err := s.pg.ListStorageConfigs(ctx, storagedomain.ListQuery{})
	if err == nil {
		fresh := map[int64]storagedomain.MemberUsage{}
		for i := range configs {
			cfg := &configs[i]
			if !cfg.Enabled {
				continue
			}
			provider, err := s.buildProvider(cfg)
			if err != nil {
				continue
			}
			reporter, ok := provider.(storageUsageReporter)
			if !ok {
				continue
			}
			usageCtx, cancel := context.WithTimeout(ctx, storageProbeTimeout)
			usage, err := reporter.BucketUsage(usageCtx, cfg)
			cancel()
			if err != nil {
				if !errors.Is(err, errBucketUsageUnavailable) {
					s.log.Info("读取桶用量失败", zap.Int64("config_id", cfg.ID), zap.Error(err))
				}
				continue
			}
			fresh[cfg.ID] = storagedomain.MemberUsage{UsedBytes: usage.Bytes, Objects: usage.Objects, Source: usage.Source, UpdatedAt: time.Now()}
		}
		reportedUsage.Lock()
		reportedUsage.values = fresh
		reportedUsage.Unlock()
	}
	s.pools.SetUsage(indexed, s.reportedUsageSnapshot())

	// 用量缓存是每个实例自己的（写入选成员要用），上面每个实例都刷；
	// 越线预警写的是共享事件表，事件去重又是进程内的 —— 只让一个实例发，免得同一条预警出现 N 遍。
	if !s.clusterState.capacityTask.owned() {
		return
	}
	pools, err := s.pg.ListStoragePools(ctx)
	if err != nil {
		return
	}
	for i := range pools {
		pool := &pools[i]
		if !pool.Enabled {
			continue
		}
		for _, member := range pool.Members {
			cfg, err := s.pg.GetStorageConfigByID(ctx, member.ConfigID)
			if err != nil || cfg == nil {
				continue
			}
			usage, _ := s.pools.Usage(member.ConfigID)
			s.checkCapacity(ctx, pool, member, cfg, usage)
		}
	}
}

// dailyMaintenance 每日：清理过期事件、写用量快照。
func (s *StorageService) dailyMaintenance(ctx context.Context) {
	if n, err := s.pg.PruneStoragePoolEvents(ctx, time.Now().Add(-storageEventRetention)); err == nil && n > 0 {
		s.log.Info("已清理过期存储事件", zap.Int64("count", n))
	}
	if err := s.CollectUsageSnapshots(ctx); err != nil {
		s.log.Warn("存储用量快照失败", zap.Error(err))
	}
}

// CollectUsageSnapshots 给每个存储配置记一条用量快照（用量趋势图的数据点）。
func (s *StorageService) CollectUsageSnapshots(ctx context.Context) error {
	configs, err := s.pg.ListStorageConfigs(ctx, storagedomain.ListQuery{})
	if err != nil {
		return err
	}
	for _, cfg := range configs {
		stats, err := s.pg.GetRealtimeUsageStats(ctx, cfg.ID)
		if err != nil || stats == nil {
			continue
		}
		if err := s.pg.CreateUsageSnapshot(ctx, storagedomain.UsageSnapshot{
			ConfigID: cfg.ID, AppID: cfg.AppID, TotalFiles: stats.TotalFiles, TotalSize: stats.TotalSize,
			ActiveFiles: stats.ActiveFiles, DeletedFiles: stats.DeletedFiles,
		}); err != nil {
			s.log.Warn("写入用量快照失败", zap.Int64("config_id", cfg.ID), zap.Error(err))
		}
	}
	return nil
}

// ════════════════════════════════════════════════════════════
//  总览
// ════════════════════════════════════════════════════════════

// Overview 存储中心总览：全部平台级配置的用量与健康、全部池、最近的迁移。
func (s *StorageService) Overview(ctx context.Context) (*storagedomain.Overview, error) {
	configs, err := s.pg.ListStorageConfigs(ctx, storagedomain.ListQuery{Scope: storagedomain.ScopeGlobal})
	if err != nil {
		return nil, err
	}
	pools, err := s.ListPools(ctx)
	if err != nil {
		return nil, err
	}
	poolIDs, err := s.pg.StorageConfigPoolIDs(ctx)
	if err != nil {
		return nil, err
	}
	migrations, err := s.pg.ListStorageMigrations(ctx, 20)
	if err != nil {
		return nil, err
	}
	capacity := map[int64]int64{}
	for _, pool := range pools {
		for _, member := range pool.Members {
			if member.CapacityBytes > 0 && (capacity[member.ConfigID] == 0 || member.CapacityBytes < capacity[member.ConfigID]) {
				capacity[member.ConfigID] = member.CapacityBytes
			}
		}
	}
	overview := &storagedomain.Overview{Configs: []storagedomain.ConfigOverview{}, Pools: pools, Migrations: migrations, GeneratedAt: time.Now()}
	for _, cfg := range configs {
		usage, _ := s.pools.Usage(cfg.ID)
		health := s.pools.Health(cfg.ID)
		state := storagedomain.MemberStateDisabled
		if cfg.Enabled {
			state = s.pools.configState(cfg.ID, capacity[cfg.ID], storagedomain.PoolDefaultWarnPct)
		}
		item := storagedomain.ConfigOverview{
			Config:        storagedomain.RedactConfig(cfg),
			Usage:         &usage,
			Health:        &health,
			State:         state,
			Capabilities:  s.capabilities(&cfg),
			PoolIDs:       poolIDs[cfg.ID],
			CapacityBytes: capacity[cfg.ID],
		}
		if item.PoolIDs == nil {
			item.PoolIDs = []int64{}
		}
		overview.Configs = append(overview.Configs, item)
		overview.Totals.Configs++
		overview.Totals.UsedBytes += usage.UsedBytes
		overview.Totals.Objects += usage.Objects
		overview.Totals.CapacityBytes += capacity[cfg.ID]
		switch state {
		case storagedomain.MemberStateBackoff, storagedomain.MemberStateDegraded, storagedomain.MemberStateFull:
			overview.Totals.Unhealthy++
		case storagedomain.MemberStateDisabled:
		default:
			overview.Totals.Healthy++
		}
	}
	if target, err := s.DescribeWriteTarget(ctx, 0, ""); err == nil {
		overview.WriteTarget = target
	}
	return overview, nil
}

// capabilities 提供商能做什么。
func (s *StorageService) capabilities(cfg *storagedomain.Config) storagedomain.ConfigCapabilities {
	provider, err := s.buildProvider(cfg)
	if err != nil {
		return storagedomain.ConfigCapabilities{}
	}
	inner := provider
	if wrapped, ok := provider.(*circuitBreakeredStorageProvider); ok {
		inner = wrapped.upstream
	}
	_, direct := asDirectUploader(provider)
	_, deleter := inner.(storageObjectDeleter)
	_, cors := inner.(storageCORSConfigurer)
	usage := false
	if cfg.Provider == storagedomain.ProviderR2 {
		if raw, err := decodeR2Config(cfg.ConfigData); err == nil && raw.APIToken != "" {
			usage = true
		}
	}
	return storagedomain.ConfigCapabilities{DirectUpload: direct, Delete: deleter, CORS: cors, BucketUsage: usage}
}

// ════════════════════════════════════════════════════════════
//  深度检测
// ════════════════════════════════════════════════════════════

// ProbeConfig 深度检测：连通 → 写入一个探测对象 → 读回比对 → 删除。比「能列出桶」严格：
// 只读的密钥、写错的桶策略、不支持当前请求格式的兼容端点，在这里都会暴露出来。
func (s *StorageService) ProbeConfig(ctx context.Context, configID int64) (*storagedomain.ProbeResult, error) {
	cfg, err := s.pg.GetStorageConfigByID(ctx, configID)
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return nil, apperrors.New(40480, http.StatusNotFound, "存储配置不存在")
	}
	provider, err := s.buildProvider(cfg)
	if err != nil {
		return nil, err
	}
	result := &storagedomain.ProbeResult{ConfigID: cfg.ID, Provider: cfg.Provider, OK: true, Steps: []storagedomain.ProbeStep{}}
	step := func(name string, run func(context.Context) (string, error)) bool {
		stepCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		started := time.Now()
		message, err := run(stepCtx)
		item := storagedomain.ProbeStep{Name: name, OK: err == nil, LatencyMs: time.Since(started).Milliseconds(), Message: message}
		if err != nil {
			item.Message = err.Error()
			if len(item.Message) > storageErrorMaxLen {
				item.Message = item.Message[:storageErrorMaxLen]
			}
			result.OK = false
		}
		result.Steps = append(result.Steps, item)
		return err == nil
	}
	skip := func(name, reason string) {
		result.Steps = append(result.Steps, storagedomain.ProbeStep{Name: name, OK: true, Skipped: true, Message: reason})
	}

	token := make([]byte, 6)
	_, _ = rand.Read(token)
	key := normalizeObjectKey(cfg.RootPath, ".aegis-probe/"+time.Now().UTC().Format("20060102T150405")+"-"+hex.EncodeToString(token))
	payload := []byte("aegis storage probe " + hex.EncodeToString(token))

	connected := step("连通", func(c context.Context) (string, error) {
		meta, err := provider.HealthCheck(c, cfg)
		result.Metadata = meta
		return "", err
	})
	if !connected {
		skip("写入", "连通失败")
		skip("读回校验", "连通失败")
		skip("删除", "连通失败")
		s.pools.RecordFailure(cfg.ID, errors.New(result.Steps[0].Message), true)
		return result, nil
	}
	wrote := step("写入", func(c context.Context) (string, error) {
		_, err := provider.Upload(c, cfg, storagedomain.UploadInput{
			ObjectKey: key, FileName: "probe.txt", ContentType: "text/plain", ContentLength: int64(len(payload)),
			Content: bytes.NewReader(payload),
		})
		return key, err
	})
	if wrote {
		step("读回校验", func(c context.Context) (string, error) {
			reader, err := provider.Open(c, cfg, key)
			if err != nil {
				return "", err
			}
			defer closeSilently(reader.Body)
			data, err := io.ReadAll(io.LimitReader(reader.Body, 1<<16))
			if err != nil {
				return "", err
			}
			if !bytes.Equal(data, payload) {
				return "", fmt.Errorf("读回的内容与写入的不一致（%d 字节，应为 %d 字节）", len(data), len(payload))
			}
			return fmt.Sprintf("%d 字节一致", len(data)), nil
		})
		if s.capabilities(cfg).Delete {
			step("删除", func(c context.Context) (string, error) {
				return "", deleteStoredObject(c, provider, cfg, key)
			})
		} else {
			skip("删除", "该存储不支持删除对象，探测对象留在 "+key)
		}
	} else {
		skip("读回校验", "写入失败")
		skip("删除", "写入失败")
	}
	if reporter, ok := provider.(storageUsageReporter); ok {
		if usage, err := reporter.BucketUsage(ctx, cfg); err == nil {
			result.Usage = usage
		}
	}
	var total time.Duration
	for _, item := range result.Steps {
		total += time.Duration(item.LatencyMs) * time.Millisecond
	}
	if result.OK {
		s.pools.RecordSuccess(cfg.ID, total, true)
	} else {
		var message string
		for _, item := range result.Steps {
			if !item.OK {
				message = item.Name + "：" + item.Message
				break
			}
		}
		s.pools.RecordFailure(cfg.ID, errors.New(message), true)
	}
	return result, nil
}

// ════════════════════════════════════════════════════════════
//  CORS（浏览器直传需要）
// ════════════════════════════════════════════════════════════

// storageCORSConfigurer 能读写存储桶 CORS 规则的提供商。
type storageCORSConfigurer interface {
	GetCORS(ctx context.Context, cfg *storagedomain.Config) ([]storagedomain.CORSRule, error)
	PutCORS(ctx context.Context, cfg *storagedomain.Config, rules []storagedomain.CORSRule) error
}

// GetBucketCORS 读存储桶的 CORS 规则。
func (s *StorageService) GetBucketCORS(ctx context.Context, configID int64) ([]storagedomain.CORSRule, error) {
	cfg, configurer, err := s.corsTarget(ctx, configID)
	if err != nil {
		return nil, err
	}
	rules, err := configurer.GetCORS(ctx, cfg)
	if err != nil {
		return nil, apperrors.New(50385, http.StatusServiceUnavailable, "读取 CORS 规则失败："+truncateStorageError(err))
	}
	return rules, nil
}

// SetBucketCORS 写入允许浏览器直传的 CORS 规则（覆盖桶上已有的规则）。
func (s *StorageService) SetBucketCORS(ctx context.Context, configID int64, origins []string) ([]storagedomain.CORSRule, error) {
	cfg, configurer, err := s.corsTarget(ctx, configID)
	if err != nil {
		return nil, err
	}
	cleaned := make([]string, 0, len(origins))
	seen := map[string]bool{}
	for _, origin := range origins {
		origin = strings.TrimRight(strings.TrimSpace(origin), "/")
		if origin == "" || seen[origin] {
			continue
		}
		if origin != "*" && !strings.HasPrefix(origin, "https://") && !strings.HasPrefix(origin, "http://") {
			return nil, apperrors.New(42275, http.StatusUnprocessableEntity, "来源需以 http:// 或 https:// 开头："+origin)
		}
		seen[origin] = true
		cleaned = append(cleaned, origin)
	}
	if len(cleaned) == 0 {
		return nil, apperrors.New(42275, http.StatusUnprocessableEntity, "至少填写一个允许的来源")
	}
	rules := []storagedomain.CORSRule{{
		AllowedOrigins: cleaned,
		AllowedMethods: []string{"GET", "HEAD", "PUT"},
		AllowedHeaders: []string{"*"},
		ExposeHeaders:  []string{"ETag"},
		MaxAgeSeconds:  3600,
	}}
	if err := configurer.PutCORS(ctx, cfg, rules); err != nil {
		return nil, apperrors.New(50385, http.StatusServiceUnavailable, "写入 CORS 规则失败："+truncateStorageError(err))
	}
	return rules, nil
}

func (s *StorageService) corsTarget(ctx context.Context, configID int64) (*storagedomain.Config, storageCORSConfigurer, error) {
	cfg, err := s.pg.GetStorageConfigByID(ctx, configID)
	if err != nil {
		return nil, nil, err
	}
	if cfg == nil {
		return nil, nil, apperrors.New(40480, http.StatusNotFound, "存储配置不存在")
	}
	provider, err := s.buildProvider(cfg)
	if err != nil {
		return nil, nil, err
	}
	if wrapped, ok := provider.(*circuitBreakeredStorageProvider); ok {
		provider = wrapped.upstream
	}
	configurer, ok := provider.(storageCORSConfigurer)
	if !ok {
		return nil, nil, apperrors.New(42276, http.StatusUnprocessableEntity, "该存储不支持在 Aegis 里配置 CORS")
	}
	return cfg, configurer, nil
}

func truncateStorageError(err error) string {
	message := err.Error()
	if len(message) > 200 {
		message = message[:200]
	}
	return message
}

func (p *s3StorageProvider) GetCORS(ctx context.Context, cfg *storagedomain.Config) ([]storagedomain.CORSRule, error) {
	client, raw, err := p.client(ctx, cfg)
	if err != nil {
		return nil, err
	}
	output, err := client.GetBucketCors(ctx, &awss3.GetBucketCorsInput{Bucket: &raw.Bucket})
	if err != nil {
		if strings.Contains(err.Error(), "NoSuchCORSConfiguration") {
			return []storagedomain.CORSRule{}, nil
		}
		return nil, err
	}
	rules := make([]storagedomain.CORSRule, 0, len(output.CORSRules))
	for _, rule := range output.CORSRules {
		rules = append(rules, storagedomain.CORSRule{
			AllowedOrigins: rule.AllowedOrigins, AllowedMethods: rule.AllowedMethods,
			AllowedHeaders: rule.AllowedHeaders, ExposeHeaders: rule.ExposeHeaders,
			MaxAgeSeconds: int(aws.ToInt32(rule.MaxAgeSeconds)),
		})
	}
	return rules, nil
}

func (p *s3StorageProvider) PutCORS(ctx context.Context, cfg *storagedomain.Config, rules []storagedomain.CORSRule) error {
	client, raw, err := p.client(ctx, cfg)
	if err != nil {
		return err
	}
	converted := make([]s3types.CORSRule, 0, len(rules))
	for _, rule := range rules {
		converted = append(converted, s3types.CORSRule{
			AllowedOrigins: rule.AllowedOrigins, AllowedMethods: rule.AllowedMethods,
			AllowedHeaders: rule.AllowedHeaders, ExposeHeaders: rule.ExposeHeaders,
			MaxAgeSeconds: aws.Int32(int32(rule.MaxAgeSeconds)),
		})
	}
	_, err = client.PutBucketCors(ctx, &awss3.PutBucketCorsInput{
		Bucket:            &raw.Bucket,
		CORSConfiguration: &s3types.CORSConfiguration{CORSRules: converted},
	})
	return err
}
