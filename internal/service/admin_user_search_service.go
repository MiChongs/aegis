package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"aegis/internal/config"
	userdomain "aegis/internal/domain/user"
	pgrepo "aegis/internal/repository/postgres"
	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"
	"github.com/mozillazg/go-pinyin"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

const (
	adminUserSearchBatchSize     = 2000
	adminUserSearchIndexVersion  = 2
	adminUserSearchMaxPinyinPath = 16
)

var adminUserSearchPinyinPathLimit = adminUserSearchMaxPinyinPath

type AdminUserSearchService struct {
	log               *zap.Logger
	pg                *pgrepo.Repository
	index             bleve.Index
	indexPath         string
	batchSize         int
	warmupEnabled     bool
	warmupConcurrency int
	indexedApps       sync.Map
	buildFlight       singleflight.Group
	warmupOnce        sync.Once
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
	RegisterIP            string    `json:"register_ip"`
	RegisterIPKeyword     string    `json:"register_ip_keyword"`
	Searchable            string    `json:"searchable"`
	PinyinSearchable      string    `json:"pinyin_searchable"`
	InitialsSearchable    string    `json:"initials_searchable"`
	CreatedAt             time.Time `json:"created_at"`
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

	indexPath := filepath.Join(rootDir, fmt.Sprintf("admin_users_v%d.bleve", adminUserSearchIndexVersion))
	index, err := bleve.Open(indexPath)
	created := false
	if err != nil {
		if !errors.Is(err, bleve.ErrorIndexPathDoesNotExist) {
			return nil, err
		}
		index, err = bleve.New(indexPath, newAdminUserSearchMapping())
		if err != nil {
			return nil, err
		}
		created = true
	}

	service := &AdminUserSearchService{
		log:               log,
		pg:                pg,
		index:             index,
		indexPath:         indexPath,
		batchSize:         batchSize,
		warmupEnabled:     cfg.WarmupEnabled,
		warmupConcurrency: warmupConcurrency,
	}

	docCount, countErr := index.DocCount()
	if countErr != nil {
		log.Warn("admin user search doc count failed", zap.String("indexPath", indexPath), zap.Error(countErr))
	}
	log.Info("admin user bleve search initialized",
		zap.String("indexPath", indexPath),
		zap.Bool("created", created),
		zap.Int("batchSize", batchSize),
		zap.Int("maxPinyinPaths", adminUserSearchPinyinPathLimit),
		zap.Bool("warmupEnabled", cfg.WarmupEnabled),
		zap.Int("warmupConcurrency", warmupConcurrency),
		zap.Uint64("docCount", docCount),
	)
	return service, nil
}

func (s *AdminUserSearchService) Close() error {
	if s == nil || s.index == nil {
		return nil
	}
	s.log.Info("closing admin user bleve search", zap.String("indexPath", s.indexPath))
	return s.index.Close()
}

func (s *AdminUserSearchService) StartWarmup(ctx context.Context) {
	if s == nil || s.index == nil || !s.warmupEnabled {
		return
	}
	s.warmupOnce.Do(func() {
		go s.warmupApps(ctx)
	})
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
	if adminQuery.Limit > 100 {
		adminQuery.Limit = 100
	}
	if !hasAdminUserSearchConditions(adminQuery) {
		return nil, 0, nil
	}

	startedAt := time.Now()
	if err := s.ensureAppIndexed(ctx, appID); err != nil {
		return nil, 0, err
	}

	rootQuery := bleve.NewConjunctionQuery(buildAdminUserSearchQueries(appID, adminQuery)...)
	req := bleve.NewSearchRequestOptions(rootQuery, adminQuery.Limit, (adminQuery.Page-1)*adminQuery.Limit, false)
	req.SortBy([]string{"-_score", "-created_at", "-user_id"})

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

	s.log.Debug("admin user bleve search completed",
		zap.Int64("appid", appID),
		zap.String("keyword", strings.TrimSpace(adminQuery.Keyword)),
		zap.String("account", strings.TrimSpace(adminQuery.Account)),
		zap.String("nickname", strings.TrimSpace(adminQuery.Nickname)),
		zap.String("email", strings.TrimSpace(adminQuery.Email)),
		zap.String("phone", strings.TrimSpace(adminQuery.Phone)),
		zap.String("registerIp", strings.TrimSpace(adminQuery.RegisterIP)),
		zap.Bool("hasUserId", adminQuery.UserID != nil),
		zap.Bool("hasEnabled", adminQuery.Enabled != nil),
		zap.Bool("hasCreatedFrom", adminQuery.CreatedFrom != nil),
		zap.Bool("hasCreatedTo", adminQuery.CreatedTo != nil),
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
	if item == nil {
		if err := s.index.Delete(docID); err != nil {
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
		RegisterIP: item.RegisterIP,
		Enabled:    item.Enabled,
		CreatedAt:  item.CreatedAt,
	})
	if err := s.index.Index(docID, doc); err != nil {
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
	if err := s.index.Delete(adminUserSearchDocID(appID, userID)); err != nil {
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
		s.log.Info("admin user bleve sync completed",
			zap.Int64("appid", appID),
			zap.Int("documents", count),
			zap.Bool("initialized", nextCheckpoint.Initialized),
			zap.Time("lastSyncedAt", nextCheckpoint.LastSyncedAt),
			zap.Int64("lastUserId", nextCheckpoint.LastUserID),
			zap.Duration("elapsed", time.Since(startedAt)),
		)
		return nil, nil
	})
	return err
}

func (s *AdminUserSearchService) rebuildApp(ctx context.Context, appID int64, checkpoint adminUserSearchCheckpoint) (int, adminUserSearchCheckpoint, error) {
	var (
		total = 0
		batch = s.index.NewBatch()
	)
	for {
		items, err := s.pg.ListAdminUserSearchSourcesByApp(ctx, appID, checkpoint.LastSyncedAt, checkpoint.LastUserID, s.batchSize)
		if err != nil {
			return total, checkpoint, err
		}
		if len(items) == 0 {
			checkpoint.AppID = appID
			checkpoint.Initialized = true
			checkpoint.SyncedAt = time.Now().UTC()
			return total, checkpoint, nil
		}

		batch.Reset()
		for _, item := range items {
			if err := batch.Index(adminUserSearchDocID(item.AppID, item.UserID), buildAdminUserSearchDocument(item)); err != nil {
				return total, checkpoint, err
			}
			checkpoint.LastSyncedAt = item.SourceUpdatedAt.UTC()
			checkpoint.LastUserID = item.UserID
		}
		if err := s.index.Batch(batch); err != nil {
			return total, checkpoint, err
		}
		checkpoint.AppID = appID
		checkpoint.SyncedAt = time.Now().UTC()
		if err := s.saveCheckpoint(checkpoint); err != nil {
			return total, checkpoint, err
		}
		total += len(items)
		s.log.Debug("admin user bleve sync batch indexed",
			zap.Int64("appid", appID),
			zap.Int("batchSize", len(items)),
			zap.Time("lastSyncedAt", checkpoint.LastSyncedAt),
			zap.Int64("lastUserId", checkpoint.LastUserID),
		)
		if len(items) < s.batchSize {
			checkpoint.Initialized = true
			checkpoint.SyncedAt = time.Now().UTC()
			return total, checkpoint, nil
		}
	}
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
	sem := make(chan struct{}, s.warmupConcurrency)
	var wg sync.WaitGroup
	for _, app := range apps {
		appID := app.ID
		if appID <= 0 {
			continue
		}
		select {
		case <-ctx.Done():
			s.log.Warn("admin user search warmup canceled", zap.Error(ctx.Err()))
			wg.Wait()
			return
		default:
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			warmupCtx, cancel := context.WithTimeout(ctx, 30*time.Minute)
			defer cancel()
			if err := s.ensureAppIndexed(warmupCtx, appID); err != nil {
				s.log.Warn("admin user search warmup app failed", zap.Int64("appid", appID), zap.Error(err))
			}
		}()
	}
	wg.Wait()
	s.log.Info("admin user search warmup completed",
		zap.Int("apps", len(apps)),
		zap.Duration("elapsed", time.Since(startedAt)),
	)
}

func (s *AdminUserSearchService) loadCheckpoint(appID int64) (adminUserSearchCheckpoint, error) {
	checkpoint := adminUserSearchCheckpoint{AppID: appID}
	if s == nil || s.index == nil || appID <= 0 {
		return checkpoint, nil
	}
	raw, err := s.index.GetInternal(adminUserSearchCheckpointKey(appID))
	if err != nil || len(raw) == 0 {
		return checkpoint, err
	}
	if err := json.Unmarshal(raw, &checkpoint); err != nil {
		return adminUserSearchCheckpoint{}, err
	}
	return checkpoint, nil
}

func (s *AdminUserSearchService) saveCheckpoint(checkpoint adminUserSearchCheckpoint) error {
	if s == nil || s.index == nil || checkpoint.AppID <= 0 {
		return nil
	}
	raw, err := json.Marshal(checkpoint)
	if err != nil {
		return err
	}
	return s.index.SetInternal(adminUserSearchCheckpointKey(checkpoint.AppID), raw)
}

func adminUserSearchCheckpointKey(appID int64) []byte {
	return []byte(fmt.Sprintf("admin_user_search:checkpoint:%d", appID))
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
		"register_ip_keyword",
	} {
		addKeywordField(doc, field)
	}

	for _, field := range []string{
		"account",
		"nickname",
		"email",
		"phone",
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
	registerIP := normalizeSearchKeyword(item.RegisterIP)

	accountPinyin := buildPinyinSearchValue(account)
	nicknamePinyin := buildPinyinSearchValue(nickname)

	searchable := joinSearchParts(
		account,
		nickname,
		email,
		item.Phone,
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
