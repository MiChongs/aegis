package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"

	storagedomain "aegis/internal/domain/storage"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
)

// 迁移：把一个存储配置里**已索引**的对象逐个复制到另一个配置，键不变。
//
// 复制成功的对象，索引行改记到目标配置下（文件管理、用量都跟着走）。散落在各处的
// storage:// 引用、云存储修订、头像记录里写的仍是源配置 —— 不去改它们，而是在迁移完成后
// 给源配置设「读取转向」：在源配置读不到的对象转去目标配置读。于是迁移不需要知道
// 有哪些表引用了对象，以后新加的引用方也自动适用。
//
// 断点：按 storage_objects.id 游标推进，进程重启后从游标继续。复制失败的对象索引行留在源配置，
// 「继续」把游标归零重扫源配置，只会碰到它们和还没处理的对象 —— 继续即重试失败项。

const (
	storageMigrationBatch       = 50
	storageMigrationConcurrency = 4
	storageMigrationObjectLimit = 30 * time.Minute
)

type storageMigrationRunner struct {
	s       *StorageService
	mu      sync.Mutex
	running map[int64]context.CancelFunc
}

func newStorageMigrationRunner(s *StorageService) *storageMigrationRunner {
	return &storageMigrationRunner{s: s, running: map[int64]context.CancelFunc{}}
}

// resume 接着跑没跑完的任务：进程启动时一次；接上集群后还会周期性地再扫（storage_pool_runtime.go 的 loop），
// 接手宕机实例留下的任务。已经有别的实例持有且心跳新鲜的，认领会失败，run 直接返回。
func (r *storageMigrationRunner) resume(ctx context.Context) {
	items, err := r.s.pg.StorageMigrationsWithStatus(ctx, storagedomain.MigrationPending, storagedomain.MigrationRunning)
	if err != nil {
		r.s.log.Warn("恢复存储迁移任务失败", zap.Error(err))
		return
	}
	for _, m := range items {
		r.start(m.ID)
	}
}

func (r *storageMigrationRunner) start(id int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.running[id]; ok {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.running[id] = cancel
	go func() {
		defer func() {
			r.mu.Lock()
			delete(r.running, id)
			r.mu.Unlock()
		}()
		r.run(ctx, id)
	}()
}

// cancel 取消本实例上正在跑的任务；返回本实例上是否确实有这个任务在跑。
func (r *storageMigrationRunner) cancel(id int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cancel, ok := r.running[id]; ok {
		cancel()
		return true
	}
	return false
}

// stopAll 进程退出：停掉所有任务，状态保持 running，下次启动接着跑。
func (r *storageMigrationRunner) stopAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, cancel := range r.running {
		cancel()
	}
}

func (r *storageMigrationRunner) run(ctx context.Context, id int64) {
	s := r.s
	// 先认领再干活：认领把状态置为 running 并记下持有者，同一个任务同一时刻只有一个实例能认领成功。
	// 原先这里直接 running → running，多副本启动时每个实例都会续跑同一个任务，同一批对象被并发复制、计数翻倍。
	//
	// 只从 pending / running 认领：进入 run 的三条路（新建、「继续」先把状态改回 pending、续跑扫描只列 pending / running）
	// 都满足这一点。原先这里还允许从 paused / failed 直接拉起；多副本下每分钟都有实例在扫描续跑，
	// 「刚列出来时还是 running、认领前一刻被管理员暂停」就会被它改回 running —— 把暂停给吞了。
	owner := s.migrationOwner()
	ok, err := s.pg.ClaimStorageMigration(ctx, id, owner,
		[]string{storagedomain.MigrationPending, storagedomain.MigrationRunning},
		storageMigrationStaleAfter, s.migrationClaimForced())
	if err != nil || !ok {
		if err != nil {
			s.log.Warn("启动存储迁移任务失败", zap.Int64("migration", id), zap.Error(err))
		}
		return
	}
	defer func() {
		// 停下就放手（正常结束、失败、暂停、进程退出都一样），别的实例或「继续」之后不必等心跳过期
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.pg.ReleaseStorageMigration(releaseCtx, id, owner); err != nil {
			s.log.Warn("释放存储迁移任务失败，其他实例将在心跳过期后接手", zap.Int64("migration", id), zap.Error(err))
		}
	}()
	ctx, stopRun := context.WithCancel(ctx)
	defer stopRun()
	go s.heartbeatMigration(ctx, id, owner, func() {
		s.log.Info("存储迁移任务已不归本实例执行（被暂停、取消或由其他实例接手），停止", zap.Int64("migration", id))
		stopRun()
	})
	m, err := s.pg.GetStorageMigration(ctx, id)
	if err != nil {
		s.log.Warn("读取存储迁移任务失败", zap.Int64("migration", id), zap.Error(err))
		return
	}
	fail := func(message string) {
		_, _ = s.pg.SetStorageMigrationStatus(context.Background(), id, storagedomain.MigrationFailed, nil, message)
		s.emitPoolEvent(context.Background(), 0, m.SourceConfigID, storagedomain.EventLevelError, storagedomain.EventMigration,
			fmt.Sprintf("迁移 #%d（%s → %s）失败：%s", id, m.SourceName, m.TargetName, message), nil, fmt.Sprintf("%d-fail", id))
	}
	source, err := s.pg.GetStorageConfigByID(ctx, m.SourceConfigID)
	if err != nil || source == nil {
		fail("源存储配置不存在")
		return
	}
	target, err := s.pg.GetStorageConfigByID(ctx, m.TargetConfigID)
	if err != nil || target == nil {
		fail("目标存储配置不存在")
		return
	}
	if !target.Enabled {
		fail("目标存储配置已停用")
		return
	}
	sourceProvider, err := s.buildProvider(source)
	if err != nil {
		fail(err.Error())
		return
	}
	targetProvider, err := s.buildProvider(target)
	if err != nil {
		fail(err.Error())
		return
	}
	s.emitPoolEvent(ctx, 0, source.ID, storagedomain.EventLevelInfo, storagedomain.EventMigration,
		fmt.Sprintf("迁移 #%d 开始：%s → %s，共 %d 个对象（%s）", id, source.ConfigName, target.ConfigName,
			m.TotalObjects, formatStorageBytes(m.TotalBytes)), nil, fmt.Sprintf("%d-start", id))

	cursor := m.LastObjectID
	for {
		if ctx.Err() != nil {
			return
		}
		batch, err := s.pg.StorageObjectsAfter(ctx, source.ID, cursor, storageMigrationBatch)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			fail("读取对象索引失败：" + truncateStorageError(err))
			return
		}
		if len(batch) == 0 {
			break
		}
		progress := pgrepo.StorageMigrationProgress{}
		var mu sync.Mutex
		var wg sync.WaitGroup
		slots := make(chan struct{}, storageMigrationConcurrency)
		for _, obj := range batch {
			obj := obj
			if obj.ID > progress.LastObjectID {
				progress.LastObjectID = obj.ID
			}
			wg.Add(1)
			slots <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-slots }()
				copied, outcome, err := r.copyObject(ctx, m, source, target, sourceProvider, targetProvider, obj)
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err != nil:
					progress.FailedObjects++
					progress.LastError = fmt.Sprintf("%s：%s", obj.ObjectKey, truncateStorageError(err))
				case outcome == migrationObjectMissing:
					progress.SkippedObjects++
				default:
					progress.CopiedObjects++
					progress.CopiedBytes += copied
					if outcome == migrationObjectRecovered {
						progress.RecoveredObjects++
					}
				}
			}()
		}
		wg.Wait()
		if ctx.Err() != nil {
			// 暂停 / 取消 / 进程退出：这一批里没完成的对象索引还在源配置，下次接着扫到。
			_ = s.pg.AdvanceStorageMigration(context.Background(), id, pgrepo.StorageMigrationProgress{
				CopiedObjects: progress.CopiedObjects, CopiedBytes: progress.CopiedBytes, RecoveredObjects: progress.RecoveredObjects,
				SkippedObjects: progress.SkippedObjects, FailedObjects: progress.FailedObjects, LastError: progress.LastError,
			})
			return
		}
		if err := s.pg.AdvanceStorageMigration(ctx, id, progress); err != nil {
			fail("记录迁移进度失败：" + truncateStorageError(err))
			return
		}
		cursor = progress.LastObjectID
		// 整批全失败多半是目标存储整体不可用：停下来，而不是把剩下的几万个对象一个个试一遍。
		if progress.FailedObjects == int64(len(batch)) {
			fail("一整批对象都复制失败，已暂停：" + progress.LastError)
			return
		}
	}

	final, err := s.pg.GetStorageMigration(ctx, id)
	if err != nil {
		return
	}
	if final.SetRedirect {
		if err := s.pg.SetStorageConfigRedirect(ctx, source.ID, &target.ID); err != nil {
			fail("设置读取转向失败：" + truncateStorageError(err))
			return
		}
		s.invalidateResolvedCache(ctx, source)
	}
	_, _ = s.pg.SetStorageMigrationStatus(ctx, id, storagedomain.MigrationCompleted, nil, "")
	level := storagedomain.EventLevelInfo
	if final.FailedObjects > 0 || final.SkippedObjects > 0 {
		level = storagedomain.EventLevelWarn
	}
	s.emitPoolEvent(ctx, 0, source.ID, level, storagedomain.EventMigration,
		fmt.Sprintf("迁移 #%d 完成：复制 %d 个（%s，其中 %d 个从数据库副本恢复），源已丢失 %d 个，失败 %d 个", id,
			final.CopiedObjects, formatStorageBytes(final.CopiedBytes), final.RecoveredObjects, final.SkippedObjects,
			final.FailedObjects), nil, fmt.Sprintf("%d-done", id))
	s.pools.InvalidateUsage()
}

type migrationObjectOutcome int

const (
	migrationObjectCopied migrationObjectOutcome = iota
	// migrationObjectRecovered 源里已经没有，从数据库副本写回了目标。
	migrationObjectRecovered
	// migrationObjectMissing 源里已经没有，也没有副本：索引改到目标并标记 missing。
	migrationObjectMissing
)

// copyObject 复制一个对象。
//
// 源里已经没有这个对象时（典型是容器本地盘随部署清空），先找数据库里的字节副本
// （头像副本、云存储修订内容）写回目标；没有副本就把索引改到目标并标记 missing，免得反复扫到。
func (r *storageMigrationRunner) copyObject(ctx context.Context, m *storagedomain.Migration, source, target *storagedomain.Config,
	sourceProvider, targetProvider storageProvider, obj storagedomain.StorageObject) (int64, migrationObjectOutcome, error) {
	s := r.s
	objectCtx, cancel := context.WithTimeout(ctx, storageMigrationObjectLimit)
	defer cancel()
	reader, err := sourceProvider.Open(objectCtx, source, obj.ObjectKey)
	if err != nil {
		if !isStorageObjectNotFound(err) {
			return 0, migrationObjectCopied, err
		}
		contentType, data, copyErr := s.pg.StorageObjectCopy(objectCtx, source.ID, obj.ObjectKey)
		if copyErr != nil {
			return 0, migrationObjectCopied, copyErr
		}
		if data == nil {
			if markErr := s.pg.MarkStorageObjectMissing(objectCtx, obj.ID, target.ID); markErr != nil {
				return 0, migrationObjectCopied, markErr
			}
			return 0, migrationObjectMissing, nil
		}
		if obj.ContentType != "" {
			contentType = obj.ContentType
		}
		if _, err := targetProvider.Upload(objectCtx, target, storagedomain.UploadInput{
			ObjectKey:     obj.ObjectKey,
			FileName:      obj.FileName,
			ContentType:   contentType,
			ContentLength: int64(len(data)),
			Content:       bytes.NewReader(data),
			SkipRules:     true,
		}); err != nil {
			return 0, migrationObjectCopied, err
		}
		if err := s.pg.MoveStorageObject(objectCtx, obj.ID, target.ID); err != nil {
			return 0, migrationObjectCopied, err
		}
		return int64(len(data)), migrationObjectRecovered, nil
	}
	defer closeSilently(reader.Body)
	size := obj.Size
	if size <= 0 {
		size = reader.Size
	}
	contentType := obj.ContentType
	if contentType == "" {
		contentType = reader.ContentType
	}
	if _, err := targetProvider.Upload(objectCtx, target, storagedomain.UploadInput{
		ObjectKey:     obj.ObjectKey,
		FileName:      obj.FileName,
		ContentType:   contentType,
		CacheControl:  reader.CacheControl,
		ContentLength: size,
		Content:       reader.Body,
		SkipRules:     true,
	}); err != nil {
		return 0, migrationObjectCopied, err
	}
	if err := s.pg.MoveStorageObject(objectCtx, obj.ID, target.ID); err != nil {
		return 0, migrationObjectCopied, err
	}
	if m.DeleteSource {
		if err := deleteStoredObject(objectCtx, sourceProvider, source, obj.ObjectKey); err != nil && !errors.Is(err, errStorageDeleteUnsupported) {
			s.log.Warn("迁移后删除源对象失败", zap.Int64("migration", m.ID), zap.String("key", obj.ObjectKey), zap.Error(err))
		}
	}
	return size, migrationObjectCopied, nil
}

// ── 对外接口 ──────────────────────────

// CreateMigration 新建并启动迁移。
func (s *StorageService) CreateMigration(ctx context.Context, input storagedomain.MigrationInput, operator string) (*storagedomain.Migration, error) {
	if input.SourceConfigID == input.TargetConfigID {
		return nil, apperrors.New(42279, http.StatusUnprocessableEntity, "源与目标不能是同一个存储")
	}
	source, err := s.pg.GetStorageConfigByID(ctx, input.SourceConfigID)
	if err != nil {
		return nil, err
	}
	target, err := s.pg.GetStorageConfigByID(ctx, input.TargetConfigID)
	if err != nil {
		return nil, err
	}
	if source == nil || target == nil {
		return nil, apperrors.New(40480, http.StatusNotFound, "存储配置不存在")
	}
	if target.Scope != storagedomain.ScopeGlobal {
		return nil, apperrors.New(42279, http.StatusUnprocessableEntity, "迁移目标需为平台级存储")
	}
	if !target.Enabled {
		return nil, apperrors.New(42279, http.StatusUnprocessableEntity, "目标存储已停用")
	}
	if input.DeleteSource && !input.SetRedirect {
		return nil, apperrors.New(42279, http.StatusUnprocessableEntity,
			"删除源对象时必须同时开启读取转向，否则仍指向源存储的引用将无法读取")
	}
	if active, err := s.pg.ActiveStorageMigrationFor(ctx, source.ID); err != nil {
		return nil, err
	} else if active {
		return nil, apperrors.New(40988, http.StatusConflict, "这个存储已有进行中的迁移任务")
	}
	if _, err := s.buildProvider(source); err != nil {
		return nil, err
	}
	if _, err := s.buildProvider(target); err != nil {
		return nil, err
	}
	m, err := s.pg.CreateStorageMigration(ctx, input, operator)
	if err != nil {
		return nil, err
	}
	s.migrations.start(m.ID)
	return m, nil
}

// ListMigrations 最近的迁移任务。
func (s *StorageService) ListMigrations(ctx context.Context) ([]storagedomain.Migration, error) {
	return s.pg.ListStorageMigrations(ctx, 50)
}

// GetMigration 一个迁移任务。
func (s *StorageService) GetMigration(ctx context.Context, id int64) (*storagedomain.Migration, error) {
	m, err := s.pg.GetStorageMigration(ctx, id)
	if errors.Is(err, pgrepo.ErrStorageMigrationNotFound) {
		return nil, apperrors.New(40488, http.StatusNotFound, "迁移任务不存在")
	}
	return m, err
}

// ControlMigration 暂停 / 继续 / 取消。
func (s *StorageService) ControlMigration(ctx context.Context, id int64, action string) (*storagedomain.Migration, error) {
	if _, err := s.GetMigration(ctx, id); err != nil {
		return nil, err
	}
	var ok bool
	var err error
	switch action {
	case "pause":
		ok, err = s.pg.SetStorageMigrationStatus(ctx, id, storagedomain.MigrationPaused,
			[]string{storagedomain.MigrationPending, storagedomain.MigrationRunning}, "")
		if ok {
			s.stopMigration(id)
		}
	case "resume":
		ok, err = s.pg.RestartStorageMigration(ctx, id)
		if ok {
			s.migrations.start(id)
		}
	case "cancel":
		ok, err = s.pg.SetStorageMigrationStatus(ctx, id, storagedomain.MigrationCancelled,
			[]string{storagedomain.MigrationPending, storagedomain.MigrationRunning, storagedomain.MigrationPaused, storagedomain.MigrationFailed}, "")
		if ok {
			s.stopMigration(id)
		}
	default:
		return nil, apperrors.New(42279, http.StatusUnprocessableEntity, "不支持的操作")
	}
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, apperrors.New(40989, http.StatusConflict, "任务当前的状态不允许这个操作")
	}
	return s.GetMigration(ctx, id)
}

// ClearRedirect 撤销一个存储配置的读取转向。
func (s *StorageService) ClearRedirect(ctx context.Context, configID int64) error {
	cfg, err := s.pg.GetStorageConfigByID(ctx, configID)
	if err != nil {
		return err
	}
	if cfg == nil {
		return apperrors.New(40480, http.StatusNotFound, "存储配置不存在")
	}
	if err := s.pg.SetStorageConfigRedirect(ctx, configID, nil); err != nil {
		return err
	}
	s.invalidateResolvedCache(ctx, cfg)
	return nil
}
