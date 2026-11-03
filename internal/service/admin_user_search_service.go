package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"aegis/internal/config"
	userdomain "aegis/internal/domain/user"
	pgrepo "aegis/internal/repository/postgres"
	"aegis/pkg/taskpool"
	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/mozillazg/go-pinyin"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
)

// 管理端用户搜索（bleve 全文 + 拼音 / 首字母）。
//
// 第 4 版起索引按用户 ID 取模分成 N 个分片（默认等于 CPU 核数，2–16 之间），查询经 bleve.IndexAlias
// 同时打到全部分片、各分片在自己的 goroutine 里检索后归并排序 —— 大应用的关键词搜索按核数并行。
// 写入按用户 ID 落到固定分片；全量 / 增量同步是三段流水线：
//
//	读库（游标分页，下一页与本页的建索引重叠）→ 多核构建文档（拼音展开最耗 CPU）→ 各分片并行提交批次
//
// 同一个检索条件在 15 秒内重复出现时直接用缓存（控制台翻页、搜索框防抖前后的重复请求）；
// 任何写入（建号、改资料、删除、同步）都让该应用的缓存代次 +1，缓存立即作废。
const (
	adminUserSearchBatchSize     = 2000
	adminUserSearchIndexVersion  = 4
	adminUserSearchMaxPinyinPath = 16
	adminUserSearchMaxShards     = 16
	adminUserSearchCacheTTL      = 15 * time.Second
	adminUserSearchCacheMax      = 4096
)

var adminUserSearchPinyinPathLimit = adminUserSearchMaxPinyinPath

// 旧版本（单一索引）目录名：新版本打开后在后台删掉，它们只是可重建的派生数据。
var adminUserSearchLegacyDir = regexp.MustCompile(`^admin_users_v[0-3]\.bleve$`)

type AdminUserSearchService struct {
	log *zap.Logger
	pg  *pgrepo.Repository
	// shards 写入按用户 ID 取模落到其中一个；index 是覆盖全部分片的只读别名，检索走它（并行）
	shards            []bleve.Index
	index             bleve.Index
	indexPath         string
	batchSize         int
	buildWorkers      int
	warmupEnabled     bool
	warmupConcurrency int
	indexedApps       sync.Map
	buildFlight       singleflight.Group
	warmupOnce        sync.Once
	closeOnce         sync.Once
	closeErr          error

	// 结果缓存：key → adminUserSearchCacheEntry；appGen：appID → *atomic.Uint64
	cache     sync.Map
	cacheSize atomic.Int64
	appGen    sync.Map
}

type adminUserSearchCacheEntry struct {
	gen     uint64
	ids     []int64
	total   int64
	expires time.Time
}

type adminUserSearchCheckpoint struct {
	AppID        int64     `json:"appid"`
	LastSyncedAt time.Time `json:"lastSyncedAt"`
	LastUserID   int64     `json:"lastUserId"`
	Initialized  bool      `json:"initialized"`
	SyncedAt     time.Time `json:"syncedAt"`
}

type adminUserSearchDocument struct {
	AppIDTerm             string    `json:"app_id_term"`
	Enabled               bool      `json:"enabled"`
	UserID                int64     `json:"user_id"`
	UserIDText            string    `json:"user_id_text"`
	Account               string    `json:"account"`
	AccountKeyword        string    `json:"account_keyword"`
	AccountPinyin         string    `json:"account_pinyin"`
	AccountInitials       string    `json:"account_initials"`
	AccountPinyinTerms    string    `json:"account_pinyin_terms"`
	AccountInitialsTerms  string    `json:"account_initials_terms"`
	Nickname              string    `json:"nickname"`
	NicknameKeyword       string    `json:"nickname_keyword"`
	NicknamePinyin        string    `json:"nickname_pinyin"`
	NicknameInitials      string    `json:"nickname_initials"`
	NicknamePinyinTerms   string    `json:"nickname_pinyin_terms"`
	NicknameInitialsTerms string    `json:"nickname_initials_terms"`
	Email                 string    `json:"email"`
	EmailKeyword          string    `json:"email_keyword"`
	Phone                 string    `json:"phone"`
	PhoneKeyword          string    `json:"phone_keyword"`
	InviteCode            string    `json:"invite_code"`
	InviteCodeKeyword     string    `json:"invite_code_keyword"`
	RegisterIP            string    `json:"register_ip"`
	RegisterIPKeyword     string    `json:"register_ip_keyword"`
	Searchable            string    `json:"searchable"`
	PinyinSearchable      string    `json:"pinyin_searchable"`
	InitialsSearchable    string    `json:"initials_searchable"`
	CreatedAt             time.Time `json:"created_at"`
}

// adminUserSearchShardCount 分片数：配置值优先，否则取 CPU 核数，限制在 [1, 16]。
func adminUserSearchShardCount(configured int) int {
	n := configured
	if n <= 0 {
		n = min(max(runtime.NumCPU(), 2), adminUserSearchMaxShards)
	}
	return min(max(n, 1), adminUserSearchMaxShards)
}

func NewAdminUserSearchService(log *zap.Logger, pg *pgrepo.Repository, cfg config.AdminUserSearchConfig) (*AdminUserSearchService, error) {
	if log == nil {
		log = zap.NewNop()
	}
	if pg == nil {
		return nil, fmt.Errorf("postgres repository is required")
	}
	rootDir := strings.TrimSpace(cfg.RootDir)
	if rootDir == "" {
		rootDir = filepath.Join("data", "search")
	}
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		return nil, err
	}
	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = adminUserSearchBatchSize
	}
	if cfg.MaxPinyinPaths > 0 {
		adminUserSearchPinyinPathLimit = cfg.MaxPinyinPaths
	}
	warmupConcurrency := cfg.WarmupConcurrency
	if warmupConcurrency <= 0 {
		warmupConcurrency = 2
	}
	shardCount := adminUserSearchShardCount(cfg.Shards)

	// 分片数写进目录名：改了分片数就是一份新索引（取模路由变了），旧目录由下面的清理删除
	indexPath := filepath.Join(rootDir, fmt.Sprintf("admin_users_v%d_s%d", adminUserSearchIndexVersion, shardCount))
	if err := os.MkdirAll(indexPath, 0o755); err != nil {
		return nil, err
	}
	shards := make([]bleve.Index, shardCount)
	created := make([]bool, shardCount)
	group := errgroup.Group{}
	for i := range shards {
		group.Go(func() error {
			path := filepath.Join(indexPath, fmt.Sprintf("shard-%02d.bleve", i))
			index, err := bleve.Open(path)
			if err != nil {
				if !errors.Is(err, bleve.ErrorIndexPathDoesNotExist) {
					return fmt.Errorf("open search shard %d: %w", i, err)
				}
				if index, err = bleve.New(path, newAdminUserSearchMapping()); err != nil {
					return fmt.Errorf("create search shard %d: %w", i, err)
				}
				created[i] = true
			}
			shards[i] = index
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		for _, shard := range shards {
			if shard != nil {
				_ = shard.Close()
			}
		}
		return nil, err
	}
	// 任何一个分片是新建的，其它分片里的检查点就不可信（分片 0 存检查点）：全部清空重建
	anyCreated := false
	for _, c := range created {
		anyCreated = anyCreated || c
	}

	alias := bleve.NewIndexAlias(shards...)
	if err := alias.SetIndexMapping(newAdminUserSearchMapping()); err != nil {
		log.Debug("admin user search alias mapping not set", zap.Error(err))
	}

	service := &AdminUserSearchService{
		log:               log,
		pg:                pg,
		shards:            shards,
		index:             alias,
		indexPath:         indexPath,
		batchSize:         batchSize,
		buildWorkers:      max(runtime.NumCPU(), 2),
		warmupEnabled:     cfg.WarmupEnabled,
		warmupConcurrency: warmupConcurrency,
	}
	if anyCreated {
		service.resetCheckpoints()
	}
	go service.removeLegacyIndexes(rootDir)

	docCount, countErr := alias.DocCount()
	if countErr != nil {
		log.Warn("admin user search doc count failed", zap.String("indexPath", indexPath), zap.Error(countErr))
	}
	log.Info("admin user bleve search initialized",
		zap.String("indexPath", indexPath),
		zap.Int("shards", shardCount),
		zap.Bool("created", anyCreated),
		zap.Int("batchSize", batchSize),
		zap.Int("buildWorkers", service.buildWorkers),
		zap.Int("maxPinyinPaths", adminUserSearchPinyinPathLimit),
		zap.Bool("warmupEnabled", cfg.WarmupEnabled),
		zap.Int("warmupConcurrency", warmupConcurrency),
		zap.Uint64("docCount", docCount),
	)
	return service, nil
}

// resetCheckpoints 分片集合不完整时（有分片是新建的）作废全部检查点，下次查询该应用时全量同步。
func (s *AdminUserSearchService) resetCheckpoints() {
	apps, err := s.pg.ListApps(context.Background())
	if err != nil {
		return
	}
	for _, app := range apps {
		_ = s.shards[0].DeleteInternal(adminUserSearchCheckpointKey(app.ID))
	}
}

// removeLegacyIndexes 删除旧版本的单一索引目录（只匹配 admin_users_v0–v3.bleve 与其它分片数的 v4 目录）。
func (s *AdminUserSearchService) removeLegacyIndexes(rootDir string) {
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return
	}
	current := filepath.Base(s.indexPath)
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || name == current {
			continue
		}
		legacy := adminUserSearchLegacyDir.MatchString(name) ||
			(strings.HasPrefix(name, fmt.Sprintf("admin_users_v%d_s", adminUserSearchIndexVersion)))
		if !legacy {
			continue
		}
		if err := os.RemoveAll(filepath.Join(rootDir, name)); err != nil {
			s.log.Warn("remove legacy admin user search index failed", zap.String("dir", name), zap.Error(err))
			continue
		}
		s.log.Info("removed legacy admin user search index", zap.String("dir", name))
	}
}

func (s *AdminUserSearchService) Close() error {
	if s == nil || s.index == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		s.log.Info("closing admin user bleve search", zap.String("indexPath", s.indexPath), zap.Int("shards", len(s.shards)))
		_ = s.index.Close()
		var errs []error
		for _, shard := range s.shards {
			if err := shard.Close(); err != nil {
				errs = append(errs, err)
			}
		}
		s.closeErr = errors.Join(errs...)
	})
	return s.closeErr
}

func (s *AdminUserSearchService) StartWarmup(ctx context.Context) {
	if s == nil || s.index == nil || !s.warmupEnabled {
		return
	}
	s.warmupOnce.Do(func() {
		go s.warmupApps(ctx)
	})
}

// shardFor 用户落在哪个分片。
func (s *AdminUserSearchService) shardFor(userID int64) bleve.Index {
	n := int64(len(s.shards))
	return s.shards[((userID%n)+n)%n]
}

// generation 应用的缓存代次。
func (s *AdminUserSearchService) generation(appID int64) *atomic.Uint64 {
	if value, ok := s.appGen.Load(appID); ok {
		return value.(*atomic.Uint64)
	}
	value, _ := s.appGen.LoadOrStore(appID, new(atomic.Uint64))
	return value.(*atomic.Uint64)
}

// invalidate 应用的检索结果缓存作废。
func (s *AdminUserSearchService) invalidate(appID int64) { s.generation(appID).Add(1) }

func adminUserSearchCacheKey(appID int64, adminQuery userdomain.AdminUserQuery) string {
	raw, _ := json.Marshal(adminQuery)
	return strconv.FormatInt(appID, 10) + "|" + string(raw)
}

func (s *AdminUserSearchService) cached(key string, gen uint64) ([]int64, int64, bool) {
	value, ok := s.cache.Load(key)
	if !ok {
		return nil, 0, false
	}
	entry := value.(adminUserSearchCacheEntry)
	if entry.gen != gen || time.Now().After(entry.expires) {
		s.cache.Delete(key)
		s.cacheSize.Add(-1)
		return nil, 0, false
	}
	return append([]int64(nil), entry.ids...), entry.total, true
}

func (s *AdminUserSearchService) remember(key string, gen uint64, ids []int64, total int64) {
	if s.cacheSize.Load() >= adminUserSearchCacheMax {
		// 满了整体清空：条目都是 15 秒内的，清掉的代价只是下一次重新检索
		s.cache.Range(func(k, _ any) bool {
			s.cache.Delete(k)
			return true
		})
		s.cacheSize.Store(0)
	}
	if _, loaded := s.cache.Swap(key, adminUserSearchCacheEntry{
		gen: gen, ids: append([]int64(nil), ids...), total: total, expires: time.Now().Add(adminUserSearchCacheTTL),
	}); !loaded {
		s.cacheSize.Add(1)
	}
}

func (s *AdminUserSearchService) SearchUsers(ctx context.Context, appID int64, adminQuery userdomain.AdminUserQuery) ([]int64, int64, error) {
	if s == nil || s.index == nil {
		return nil, 0, fmt.Errorf("admin user search is unavailable")
	}
	if appID <= 0 {
		return nil, 0, fmt.Errorf("invalid app id")
	}
	if adminQuery.Page < 1 {
		adminQuery.Page = 1
	}
	if adminQuery.Limit <= 0 {
		adminQuery.Limit = 20
	}
	if adminQuery.Limit > adminUserListMaxLimit {
		adminQuery.Limit = adminUserListMaxLimit
	}
	if !hasAdminUserSearchConditions(adminQuery) {
		return nil, 0, nil
	}

	startedAt := time.Now()
	if err := s.ensureAppIndexed(ctx, appID); err != nil {
		return nil, 0, err
	}

	gen := s.generation(appID).Load()
	cacheKey := adminUserSearchCacheKey(appID, adminQuery)
	if ids, total, ok := s.cached(cacheKey, gen); ok {
		return ids, total, nil
	}

	rootQuery := bleve.NewConjunctionQuery(buildAdminUserSearchQueries(appID, adminQuery)...)
	req := bleve.NewSearchRequestOptions(rootQuery, adminQuery.Limit, (adminQuery.Page-1)*adminQuery.Limit, false)
	req.SortBy([]string{"-_score", "-created_at", "-user_id"})

	// 别名把请求同时发给全部分片（每个分片一个 goroutine），再按排序键归并
	result, err := s.index.SearchInContext(ctx, req)
	if err != nil {
		s.log.Warn("admin user bleve search failed",
			zap.Int64("appid", appID),
			zap.Duration("elapsed", time.Since(startedAt)),
			zap.Error(err),
		)
		return nil, 0, err
	}

	ids := make([]int64, 0, len(result.Hits))
	for _, hit := range result.Hits {
		userID, ok := parseAdminUserSearchDocID(hit.ID)
		if !ok {
			s.log.Debug("skip invalid admin user search hit id", zap.String("hitID", hit.ID))
			continue
		}
		ids = append(ids, userID)
	}
	s.remember(cacheKey, gen, ids, int64(result.Total))

	s.log.Debug("admin user bleve search completed",
		zap.Int64("appid", appID),
		zap.String("keyword", strings.TrimSpace(adminQuery.Keyword)),
		zap.Int("shards", len(s.shards)),
		zap.Int("page", adminQuery.Page),
		zap.Int("limit", adminQuery.Limit),
		zap.Int("hits", len(ids)),
		zap.Uint64("total", result.Total),
		zap.Duration("elapsed", time.Since(startedAt)),
	)
	return ids, int64(result.Total), nil
}

func (s *AdminUserSearchService) IndexUser(ctx context.Context, appID int64, userID int64) error {
	if s == nil || s.index == nil || userID <= 0 || appID <= 0 {
		return nil
	}
	defer s.invalidate(appID)
	item, err := s.pg.GetAdminUserByApp(ctx, appID, userID)
	if err != nil {
		s.log.Warn("load admin user for bleve indexing failed",
			zap.Int64("appid", appID),
			zap.Int64("userId", userID),
			zap.Error(err),
		)
		return err
	}
	docID := adminUserSearchDocID(appID, userID)
	shard := s.shardFor(userID)
	if item == nil {
		if err := shard.Delete(docID); err != nil {
			s.log.Warn("delete missing admin user bleve doc failed",
				zap.Int64("appid", appID),
				zap.Int64("userId", userID),
				zap.Error(err),
			)
			return err
		}
		s.log.Debug("deleted stale admin user bleve doc", zap.Int64("appid", appID), zap.Int64("userId", userID))
		return nil
	}

	doc := buildAdminUserSearchDocument(userdomain.AdminUserSearchSource{
		UserID:     item.ID,
		AppID:      item.AppID,
		Account:    item.Account,
		Nickname:   item.Nickname,
		Email:      item.Email,
		Phone:      item.Phone,
		InviteCode: item.InviteCode,
		RegisterIP: item.RegisterIP,
		Enabled:    item.Enabled,
		CreatedAt:  item.CreatedAt,
	})
	if err := shard.Index(docID, doc); err != nil {
		s.log.Warn("index admin user bleve doc failed",
			zap.Int64("appid", appID),
			zap.Int64("userId", userID),
			zap.Error(err),
		)
		return err
	}
	s.log.Debug("indexed admin user bleve doc", zap.Int64("appid", appID), zap.Int64("userId", userID))
	return nil
}

func (s *AdminUserSearchService) DeleteUser(appID int64, userID int64) error {
	if s == nil || s.index == nil || userID <= 0 || appID <= 0 {
		return nil
	}
	defer s.invalidate(appID)
	if err := s.shardFor(userID).Delete(adminUserSearchDocID(appID, userID)); err != nil {
		s.log.Warn("delete admin user bleve doc failed",
			zap.Int64("appid", appID),
			zap.Int64("userId", userID),
			zap.Error(err),
		)
		return err
	}
	s.log.Debug("deleted admin user bleve doc", zap.Int64("appid", appID), zap.Int64("userId", userID))
	return nil
}

func (s *AdminUserSearchService) ensureAppIndexed(ctx context.Context, appID int64) error {
	if s == nil || s.index == nil || appID <= 0 {
		return nil
	}
	if _, ok := s.indexedApps.Load(appID); ok {
		return nil
	}

	_, err, _ := s.buildFlight.Do(strconv.FormatInt(appID, 10), func() (any, error) {
		if _, ok := s.indexedApps.Load(appID); ok {
			return nil, nil
		}
		startedAt := time.Now()
		checkpoint, checkpointErr := s.loadCheckpoint(appID)
		if checkpointErr != nil {
			return nil, checkpointErr
		}
		s.log.Info("starting admin user bleve sync",
			zap.Int64("appid", appID),
			zap.Bool("initialized", checkpoint.Initialized),
			zap.Time("lastSyncedAt", checkpoint.LastSyncedAt),
			zap.Int64("lastUserId", checkpoint.LastUserID),
		)
		count, nextCheckpoint, err := s.rebuildApp(ctx, appID, checkpoint)
		s.invalidate(appID)
		if err != nil {
			s.log.Warn("admin user bleve sync failed",
				zap.Int64("appid", appID),
				zap.Duration("elapsed", time.Since(startedAt)),
				zap.Error(err),
			)
			return nil, err
		}
		if err := s.saveCheckpoint(nextCheckpoint); err != nil {
			return nil, err
		}
		s.indexedApps.Store(appID, struct{}{})
		elapsed := time.Since(startedAt)
		rate := 0.0
		if elapsed > 0 {
			rate = float64(count) / elapsed.Seconds()
		}
		s.log.Info("admin user bleve sync completed",
			zap.Int64("appid", appID),
			zap.Int("documents", count),
			zap.Int("shards", len(s.shards)),
			zap.Float64("docsPerSecond", rate),
			zap.Bool("initialized", nextCheckpoint.Initialized),
			zap.Time("lastSyncedAt", nextCheckpoint.LastSyncedAt),
			zap.Int64("lastUserId", nextCheckpoint.LastUserID),
			zap.Duration("elapsed", elapsed),
		)
		return nil, nil
	})
	return err
}

// adminUserSearchPage 读库得到的一页，与读这一页之后的检查点。
type adminUserSearchPage struct {
	items      []userdomain.AdminUserSearchSource
	checkpoint adminUserSearchCheckpoint
}

// rebuildApp 同步一个应用：读库协程按游标往前读（最多领先一页），本协程把每页的文档多核构建后按分片并行提交。
func (s *AdminUserSearchService) rebuildApp(ctx context.Context, appID int64, checkpoint adminUserSearchCheckpoint) (int, adminUserSearchCheckpoint, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	pages := make(chan adminUserSearchPage, 1)
	readErr := make(chan error, 1)
	go func() {
		defer close(pages)
		cursor := checkpoint
		for {
			items, err := s.pg.ListAdminUserSearchSourcesByApp(ctx, appID, cursor.LastSyncedAt, cursor.LastUserID, s.batchSize)
			if err != nil {
				readErr <- err
				return
			}
			if len(items) > 0 {
				last := items[len(items)-1]
				cursor.LastSyncedAt, cursor.LastUserID = last.SourceUpdatedAt.UTC(), last.UserID
			}
			select {
			case pages <- adminUserSearchPage{items: items, checkpoint: cursor}:
			case <-ctx.Done():
				return
			}
			if len(items) < s.batchSize {
				return
			}
		}
	}()

	total := 0
	for page := range pages {
		if len(page.items) > 0 {
			if err := s.indexPage(ctx, page.items); err != nil {
				return total, checkpoint, err
			}
			checkpoint.LastSyncedAt, checkpoint.LastUserID = page.checkpoint.LastSyncedAt, page.checkpoint.LastUserID
			checkpoint.AppID = appID
			checkpoint.SyncedAt = time.Now().UTC()
			if err := s.saveCheckpoint(checkpoint); err != nil {
				return total, checkpoint, err
			}
			total += len(page.items)
			s.log.Debug("admin user bleve sync batch indexed",
				zap.Int64("appid", appID),
				zap.Int("batchSize", len(page.items)),
				zap.Time("lastSyncedAt", checkpoint.LastSyncedAt),
				zap.Int64("lastUserId", checkpoint.LastUserID),
			)
		}
		if len(page.items) < s.batchSize {
			checkpoint.AppID = appID
			checkpoint.Initialized = true
			checkpoint.SyncedAt = time.Now().UTC()
			return total, checkpoint, nil
		}
	}
	select {
	case err := <-readErr:
		return total, checkpoint, err
	default:
	}
	if err := ctx.Err(); err != nil {
		return total, checkpoint, err
	}
	checkpoint.AppID = appID
	checkpoint.Initialized = true
	checkpoint.SyncedAt = time.Now().UTC()
	return total, checkpoint, nil
}

// indexPage 一页用户：文档构建分给 buildWorkers 个协程（拼音多音字展开是 CPU 密集的），再按分片分组、各分片并行提交一个批次。
func (s *AdminUserSearchService) indexPage(ctx context.Context, items []userdomain.AdminUserSearchSource) error {
	docs := make([]adminUserSearchDocument, len(items))
	workers := min(s.buildWorkers, len(items))
	chunk := (len(items) + workers - 1) / workers
	build, _ := errgroup.WithContext(ctx)
	for start := 0; start < len(items); start += chunk {
		end := min(start+chunk, len(items))
		build.Go(func() error {
			for i := start; i < end; i++ {
				docs[i] = buildAdminUserSearchDocument(items[i])
			}
			return nil
		})
	}
	_ = build.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}

	buckets := make([][]int, len(s.shards))
	n := int64(len(s.shards))
	for i, item := range items {
		shard := ((item.UserID % n) + n) % n
		buckets[shard] = append(buckets[shard], i)
	}
	commit, _ := errgroup.WithContext(ctx)
	for shardIndex, members := range buckets {
		if len(members) == 0 {
			continue
		}
		commit.Go(func() error {
			shard := s.shards[shardIndex]
			batch := shard.NewBatch()
			for _, i := range members {
				if err := batch.Index(adminUserSearchDocID(items[i].AppID, items[i].UserID), docs[i]); err != nil {
					return err
				}
			}
			return shard.Batch(batch)
		})
	}
	return commit.Wait()
}

func (s *AdminUserSearchService) warmupApps(ctx context.Context) {
	if s == nil || s.index == nil {
		return
	}
	apps, err := s.pg.ListApps(ctx)
	if err != nil {
		s.log.Warn("load apps for admin user search warmup failed", zap.Error(err))
		return
	}
	if len(apps) == 0 {
		return
	}

	s.log.Info("starting admin user search warmup",
		zap.Int("apps", len(apps)),
		zap.Int("concurrency", s.warmupConcurrency),
	)
	startedAt := time.Now()
	workItems := make([]int64, 0, len(apps))
	for _, app := range apps {
		if app.ID > 0 {
			workItems = append(workItems, app.ID)
		}
	}
	if err := taskpool.DispatchWithError(ctx, s.warmupConcurrency, workItems, func(taskCtx context.Context, appID int64) error {
		warmupCtx, cancel := context.WithTimeout(taskCtx, 30*time.Minute)
		defer cancel()
		if err := s.ensureAppIndexed(warmupCtx, appID); err != nil {
			s.log.Warn("admin user search warmup app failed", zap.Int64("appid", appID), zap.Error(err))
			return err
		}
		return nil
	}); err != nil {
		if errors.Is(err, context.Canceled) {
			s.log.Warn("admin user search warmup canceled", zap.Error(err))
			return
		}
		s.log.Warn("admin user search warmup stopped", zap.Error(err))
	}
	s.log.Info("admin user search warmup completed",
		zap.Int("apps", len(workItems)),
		zap.Duration("elapsed", time.Since(startedAt)),
	)
}

// 检查点存在分片 0 的内部键里。
func (s *AdminUserSearchService) loadCheckpoint(appID int64) (adminUserSearchCheckpoint, error) {
	checkpoint := adminUserSearchCheckpoint{AppID: appID}
	if s == nil || len(s.shards) == 0 || appID <= 0 {
		return checkpoint, nil
	}
	raw, err := s.shards[0].GetInternal(adminUserSearchCheckpointKey(appID))
	if err != nil || len(raw) == 0 {
		return checkpoint, err
	}
	if err := json.Unmarshal(raw, &checkpoint); err != nil {
		return adminUserSearchCheckpoint{}, err
	}
	return checkpoint, nil
}

func (s *AdminUserSearchService) saveCheckpoint(checkpoint adminUserSearchCheckpoint) error {
	if s == nil || len(s.shards) == 0 || checkpoint.AppID <= 0 {
		return nil
	}
	raw, err := json.Marshal(checkpoint)
	if err != nil {
		return err
	}
	return s.shards[0].SetInternal(adminUserSearchCheckpointKey(checkpoint.AppID), raw)
}

const adminUserSearchCheckpointPrefix = "admin_user_search:checkpoint:"

func adminUserSearchCheckpointKey(appID int64) []byte {
	return []byte(adminUserSearchCheckpointPrefix + strconv.FormatInt(appID, 10))
}

func newAdminUserSearchMapping() mapping.IndexMapping {
	indexMapping := bleve.NewIndexMapping()
	indexMapping.DefaultAnalyzer = "standard"

	doc := bleve.NewDocumentStaticMapping()

	addKeywordField(doc, "app_id_term")
	addBooleanField(doc, "enabled")
	addNumericField(doc, "user_id")
	addDateField(doc, "created_at")

	for _, field := range []string{
		"user_id_text",
		"account_keyword",
		"account_pinyin",
		"account_initials",
		"nickname_keyword",
		"nickname_pinyin",
		"nickname_initials",
		"email_keyword",
		"phone_keyword",
		"invite_code_keyword",
		"register_ip_keyword",
	} {
		addKeywordField(doc, field)
	}

	for _, field := range []string{
		"account",
		"nickname",
		"email",
		"phone",
		"invite_code",
		"register_ip",
		"searchable",
		"pinyin_searchable",
		"initials_searchable",
		"account_pinyin_terms",
		"account_initials_terms",
		"nickname_pinyin_terms",
		"nickname_initials_terms",
	} {
		addTextField(doc, field)
	}

	indexMapping.DefaultMapping = doc
	return indexMapping
}

func buildAdminUserSearchDocument(item userdomain.AdminUserSearchSource) adminUserSearchDocument {
	account := strings.TrimSpace(item.Account)
	nickname := strings.TrimSpace(item.Nickname)
	email := strings.TrimSpace(item.Email)
	phone := normalizeSearchKeyword(item.Phone)
	inviteCode := normalizeSearchKeyword(item.InviteCode)
	registerIP := normalizeSearchKeyword(item.RegisterIP)

	accountPinyin := buildPinyinSearchValue(account)
	nicknamePinyin := buildPinyinSearchValue(nickname)

	searchable := joinSearchParts(
		account,
		nickname,
		email,
		item.Phone,
		item.InviteCode,
		item.RegisterIP,
		strconv.FormatInt(item.UserID, 10),
	)
	pinyinSearchable := joinSearchParts(accountPinyin.Terms, nicknamePinyin.Terms)
	initialsSearchable := joinSearchParts(accountPinyin.InitialTerms, nicknamePinyin.InitialTerms)

	return adminUserSearchDocument{
		AppIDTerm:             strconv.FormatInt(item.AppID, 10),
		Enabled:               item.Enabled,
		UserID:                item.UserID,
		UserIDText:            strconv.FormatInt(item.UserID, 10),
		Account:               account,
		AccountKeyword:        normalizeSearchKeyword(account),
		AccountPinyin:         accountPinyin.Joined,
		AccountInitials:       accountPinyin.Initials,
		AccountPinyinTerms:    accountPinyin.Terms,
		AccountInitialsTerms:  accountPinyin.InitialTerms,
		Nickname:              nickname,
		NicknameKeyword:       normalizeSearchKeyword(nickname),
		NicknamePinyin:        nicknamePinyin.Joined,
		NicknameInitials:      nicknamePinyin.Initials,
		NicknamePinyinTerms:   nicknamePinyin.Terms,
		NicknameInitialsTerms: nicknamePinyin.InitialTerms,
		Email:                 email,
		EmailKeyword:          normalizeSearchKeyword(email),
		Phone:                 item.Phone,
		PhoneKeyword:          phone,
		InviteCode:            item.InviteCode,
		InviteCodeKeyword:     inviteCode,
		RegisterIP:            item.RegisterIP,
		RegisterIPKeyword:     registerIP,
		Searchable:            searchable,
		PinyinSearchable:      pinyinSearchable,
		InitialsSearchable:    initialsSearchable,
		CreatedAt:             item.CreatedAt.UTC(),
	}
}

func buildAdminUserSearchQueries(appID int64, adminQuery userdomain.AdminUserQuery) []query.Query {
	queries := make([]query.Query, 0, 10)

	appQuery := bleve.NewTermQuery(strconv.FormatInt(appID, 10))
	appQuery.SetField("app_id_term")
	queries = append(queries, appQuery)

	if adminQuery.Enabled != nil {
		boolQuery := bleve.NewBoolFieldQuery(*adminQuery.Enabled)
		boolQuery.SetField("enabled")
		queries = append(queries, boolQuery)
	}

	if adminQuery.UserID != nil && *adminQuery.UserID > 0 {
		userIDQuery := bleve.NewTermQuery(strconv.FormatInt(*adminQuery.UserID, 10))
		userIDQuery.SetField("user_id_text")
		queries = append(queries, userIDQuery)
	}

	if adminQuery.CreatedFrom != nil || adminQuery.CreatedTo != nil {
		rangeQuery := bleve.NewDateRangeStringQuery("", "")
		rangeQuery.SetField("created_at")
		if adminQuery.CreatedFrom != nil {
			rangeQuery.Start = adminQuery.CreatedFrom.UTC().Format(time.RFC3339)
		}
		if adminQuery.CreatedTo != nil {
			rangeQuery.End = adminQuery.CreatedTo.UTC().Format(time.RFC3339)
		}
		queries = append(queries, rangeQuery)
	}

	addSpecificFieldQuery(&queries, adminQuery.Account, adminUserFieldQueryOptions{
		TextField:          "account",
		NormalizedFields:   []string{"account_keyword"},
		PinyinFields:       []string{"account_pinyin"},
		InitialsFields:     []string{"account_initials"},
		PinyinTermsFields:  []string{"account_pinyin_terms"},
		InitialsTermFields: []string{"account_initials_terms"},
	})
	addSpecificFieldQuery(&queries, adminQuery.Nickname, adminUserFieldQueryOptions{
		TextField:          "nickname",
		NormalizedFields:   []string{"nickname_keyword"},
		PinyinFields:       []string{"nickname_pinyin"},
		InitialsFields:     []string{"nickname_initials"},
		PinyinTermsFields:  []string{"nickname_pinyin_terms"},
		InitialsTermFields: []string{"nickname_initials_terms"},
	})
	addSpecificFieldQuery(&queries, adminQuery.Email, adminUserFieldQueryOptions{
		TextField:        "email",
		NormalizedFields: []string{"email_keyword"},
	})
	addSpecificFieldQuery(&queries, adminQuery.Phone, adminUserFieldQueryOptions{
		TextField:        "phone",
		NormalizedFields: []string{"phone_keyword"},
	})
	addSpecificFieldQuery(&queries, adminQuery.InviteCode, adminUserFieldQueryOptions{
		TextField:        "invite_code",
		NormalizedFields: []string{"invite_code_keyword"},
	})
	addSpecificFieldQuery(&queries, adminQuery.RegisterIP, adminUserFieldQueryOptions{
		TextField:        "register_ip",
		NormalizedFields: []string{"register_ip_keyword"},
	})

	if keyword := strings.TrimSpace(adminQuery.Keyword); keyword != "" {
		queries = append(queries, buildKeywordSearchQuery(keyword))
	}

	return queries
}

func buildKeywordSearchQuery(keyword string) query.Query {
	searchQueries := make([]query.Query, 0, 16)

	appendMatchFieldQuery(&searchQueries, keyword, "searchable")
	appendMatchFieldQuery(&searchQueries, keyword, "pinyin_searchable")
	appendMatchFieldQuery(&searchQueries, keyword, "initials_searchable")

	cleaned := normalizeSearchKeyword(keyword)
	if cleaned != "" {
		appendTermAndPrefixQueries(&searchQueries, cleaned,
			"account_keyword",
			"nickname_keyword",
			"email_keyword",
			"phone_keyword",
			"invite_code_keyword",
			"register_ip_keyword",
			"user_id_text",
			"account_pinyin",
			"account_initials",
			"nickname_pinyin",
			"nickname_initials",
		)
		appendPrefixQueries(&searchQueries, cleaned, "pinyin_searchable", "initials_searchable")
	}

	return bleve.NewDisjunctionQuery(searchQueries...)
}

type adminUserFieldQueryOptions struct {
	TextField          string
	NormalizedFields   []string
	PinyinFields       []string
	InitialsFields     []string
	PinyinTermsFields  []string
	InitialsTermFields []string
}

func addSpecificFieldQuery(queries *[]query.Query, value string, options adminUserFieldQueryOptions) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}

	fieldQueries := make([]query.Query, 0, 8)
	appendMatchFieldQuery(&fieldQueries, value, options.TextField)
	appendMatchFieldQueries(&fieldQueries, value, options.PinyinTermsFields...)
	appendMatchFieldQueries(&fieldQueries, value, options.InitialsTermFields...)

	cleaned := normalizeSearchKeyword(value)
	if cleaned != "" {
		appendTermAndPrefixQueries(&fieldQueries, cleaned, options.NormalizedFields...)
		appendTermAndPrefixQueries(&fieldQueries, cleaned, options.PinyinFields...)
		appendTermAndPrefixQueries(&fieldQueries, cleaned, options.InitialsFields...)
		appendPrefixQueries(&fieldQueries, cleaned, options.PinyinTermsFields...)
		appendPrefixQueries(&fieldQueries, cleaned, options.InitialsTermFields...)
	}
	*queries = append(*queries, bleve.NewDisjunctionQuery(fieldQueries...))
}

func appendMatchFieldQueries(queries *[]query.Query, value string, fields ...string) {
	for _, field := range fields {
		appendMatchFieldQuery(queries, value, field)
	}
}

func appendMatchFieldQuery(queries *[]query.Query, value string, field string) {
	value = strings.TrimSpace(value)
	field = strings.TrimSpace(field)
	if value == "" || field == "" {
		return
	}
	matchQuery := bleve.NewMatchQuery(value)
	matchQuery.SetField(field)
	*queries = append(*queries, matchQuery)
}

func appendTermAndPrefixQueries(queries *[]query.Query, value string, fields ...string) {
	for _, field := range fields {
		appendTermAndPrefixQuery(queries, value, field)
	}
}

func appendTermAndPrefixQuery(queries *[]query.Query, value string, field string) {
	value = strings.TrimSpace(value)
	field = strings.TrimSpace(field)
	if value == "" || field == "" {
		return
	}
	termQuery := bleve.NewTermQuery(value)
	termQuery.SetField(field)
	*queries = append(*queries, termQuery)

	if !shouldUsePrefixQuery(value) {
		return
	}
	prefixQuery := bleve.NewPrefixQuery(value)
	prefixQuery.SetField(field)
	*queries = append(*queries, prefixQuery)
}

func appendPrefixQueries(queries *[]query.Query, value string, fields ...string) {
	for _, field := range fields {
		appendPrefixQuery(queries, value, field)
	}
}

func appendPrefixQuery(queries *[]query.Query, value string, field string) {
	value = strings.TrimSpace(value)
	field = strings.TrimSpace(field)
	if value == "" || field == "" || !shouldUsePrefixQuery(value) {
		return
	}
	prefixQuery := bleve.NewPrefixQuery(value)
	prefixQuery.SetField(field)
	*queries = append(*queries, prefixQuery)
}

func addKeywordField(doc *mapping.DocumentMapping, field string) {
	fieldMapping := bleve.NewKeywordFieldMapping()
	fieldMapping.Store = false
	doc.AddFieldMappingsAt(field, fieldMapping)
}

func addTextField(doc *mapping.DocumentMapping, field string) {
	fieldMapping := bleve.NewTextFieldMapping()
	fieldMapping.Store = false
	doc.AddFieldMappingsAt(field, fieldMapping)
}

func addBooleanField(doc *mapping.DocumentMapping, field string) {
	fieldMapping := bleve.NewBooleanFieldMapping()
	fieldMapping.Store = false
	doc.AddFieldMappingsAt(field, fieldMapping)
}

func addNumericField(doc *mapping.DocumentMapping, field string) {
	fieldMapping := bleve.NewNumericFieldMapping()
	fieldMapping.Store = false
	fieldMapping.DocValues = true
	doc.AddFieldMappingsAt(field, fieldMapping)
}

func addDateField(doc *mapping.DocumentMapping, field string) {
	fieldMapping := bleve.NewDateTimeFieldMapping()
	fieldMapping.Store = false
	fieldMapping.DocValues = true
	doc.AddFieldMappingsAt(field, fieldMapping)
}

func adminUserSearchDocID(appID int64, userID int64) string {
	return fmt.Sprintf("%d:%d", appID, userID)
}

func parseAdminUserSearchDocID(value string) (int64, bool) {
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 {
		return 0, false
	}
	userID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, false
	}
	return userID, true
}

func hasAdminUserSearchConditions(adminQuery userdomain.AdminUserQuery) bool {
	return strings.TrimSpace(adminQuery.Keyword) != "" ||
		strings.TrimSpace(adminQuery.Account) != "" ||
		strings.TrimSpace(adminQuery.Nickname) != "" ||
		strings.TrimSpace(adminQuery.Email) != "" ||
		strings.TrimSpace(adminQuery.Phone) != "" ||
		strings.TrimSpace(adminQuery.InviteCode) != "" ||
		strings.TrimSpace(adminQuery.RegisterIP) != "" ||
		adminQuery.UserID != nil ||
		adminQuery.Enabled != nil ||
		adminQuery.CreatedFrom != nil ||
		adminQuery.CreatedTo != nil
}

func normalizeSearchKeyword(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	replacer := strings.NewReplacer(" ", "", "*", "", "?", "", "-", "", "_", "", ".", "", ":", "")
	return replacer.Replace(value)
}

func shouldUsePrefixQuery(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	runeCount := 0
	for _, r := range value {
		runeCount++
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return runeCount >= 2
}

func joinSearchParts(parts ...string) string {
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			items = append(items, part)
		}
	}
	return strings.Join(items, " ")
}

type pinyinSearchValue struct {
	Joined       string
	Initials     string
	Terms        string
	InitialTerms string
}

type pinyinVariant struct {
	Joined   string
	Initials string
	Tokens   []string
}

func buildPinyinSearchValue(value string) pinyinSearchValue {
	value = strings.TrimSpace(value)
	if value == "" {
		return pinyinSearchValue{}
	}
	if !containsHanText(value) {
		return pinyinSearchValue{}
	}

	variants := buildPinyinVariants(value)
	if len(variants) == 0 {
		return pinyinSearchValue{}
	}

	fullTerms := make([]string, 0, len(variants)*2)
	initialTerms := make([]string, 0, len(variants))
	fullSeen := make(map[string]struct{}, len(variants)*3)
	initialSeen := make(map[string]struct{}, len(variants))
	for _, variant := range variants {
		appendUniqueString(&fullTerms, fullSeen, variant.Joined)
		for _, token := range variant.Tokens {
			appendUniqueString(&fullTerms, fullSeen, token)
		}
		appendUniqueString(&initialTerms, initialSeen, variant.Initials)
	}

	return pinyinSearchValue{
		Joined:       variants[0].Joined,
		Initials:     variants[0].Initials,
		Terms:        strings.Join(fullTerms, " "),
		InitialTerms: strings.Join(initialTerms, " "),
	}
}

func buildPinyinVariants(value string) []pinyinVariant {
	args := pinyin.NewArgs()
	args.Heteronym = true
	args.Fallback = func(r rune, _ pinyin.Args) []string {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return nil
		}
		return []string{strings.ToLower(string(r))}
	}
	parts := pinyin.Pinyin(value, args)

	variants := make([]pinyinVariant, 0, 4)
	seen := make(map[string]struct{}, 8)
	var current []string
	var walk func(int)
	walk = func(index int) {
		if len(variants) >= adminUserSearchPinyinPathLimit {
			return
		}
		if index >= len(parts) {
			fullParts := make([]string, 0, len(current))
			initials := make([]string, 0, len(current))
			for _, part := range current {
				part = normalizeSearchKeyword(part)
				if part == "" {
					continue
				}
				fullParts = append(fullParts, part)
				initials = append(initials, part[:1])
			}
			if len(fullParts) == 0 {
				return
			}
			joined := strings.Join(fullParts, "")
			if joined == "" {
				return
			}
			if _, ok := seen[joined]; ok {
				return
			}
			seen[joined] = struct{}{}
			variants = append(variants, pinyinVariant{
				Joined:   joined,
				Initials: strings.Join(initials, ""),
				Tokens:   fullParts,
			})
			return
		}

		options := uniqueNormalizedPinyinOptions(parts[index])
		if len(options) == 0 {
			walk(index + 1)
			return
		}
		for _, option := range options {
			current = append(current, option)
			walk(index + 1)
			current = current[:len(current)-1]
			if len(variants) >= adminUserSearchPinyinPathLimit {
				return
			}
		}
	}
	walk(0)
	return variants
}

func uniqueNormalizedPinyinOptions(options []string) []string {
	if len(options) == 0 {
		return nil
	}
	unique := make([]string, 0, len(options))
	seen := make(map[string]struct{}, len(options))
	for _, option := range options {
		option = normalizeSearchKeyword(option)
		if option == "" {
			continue
		}
		appendUniqueString(&unique, seen, option)
	}
	return unique
}

func appendUniqueString(target *[]string, seen map[string]struct{}, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	if _, ok := seen[value]; ok {
		return
	}
	seen[value] = struct{}{}
	*target = append(*target, value)
}

func containsHanText(value string) bool {
	for _, r := range value {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}
