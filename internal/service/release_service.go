package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	appdomain "aegis/internal/domain/app"
	storagedomain "aegis/internal/domain/storage"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"

	"github.com/inbucket/html2text"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// ReleaseService 发布中心：版本的定向、灰度、定时发布、多安装包与漏斗统计。
//
// 状态流转：draft →（发布）→ published 或 scheduled（到点即视为 published，无需定时任务）
// published ⇄ paused（暂停下发，已收到的人不受影响）；任何状态 → revoked（撤回）。
// 检测接口只认「此刻在下发」的版本，判定见 release_engine.go。
type ReleaseService struct {
	log      *zap.Logger
	pg       *pgrepo.Repository
	redis    *redis.Client
	storage  *StorageService
	events   AppEventPublisher
	location *time.Location

	cacheMu sync.Mutex
	cache   map[int64]releaseCacheEntry
}

type releaseCacheEntry struct {
	items     []appdomain.Release
	fetchedAt time.Time
}

const (
	releaseCacheTTL       = 30 * time.Second
	releaseAssetLinkTTL   = 6 * time.Hour
	releaseAssetMaxSize   = 2 << 30 // 2 GB
	releaseAssetMaxCount  = 12
	releaseEventDedupeTTL = 36 * time.Hour
	releaseStatsDays      = 14
)

var (
	releaseSHA256Pattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	releaseAbiPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,31}$`)
	releasePlatforms     = map[string]struct{}{"all": {}, "android": {}, "ios": {}, "harmony": {}, "windows": {}, "macos": {}, "linux": {}, "web": {}}
)

func NewReleaseService(log *zap.Logger, pg *pgrepo.Repository, redisClient *redis.Client, location *time.Location) *ReleaseService {
	if log == nil {
		log = zap.NewNop()
	}
	if location == nil {
		location = timeutil.DefaultLocation()
	}
	return &ReleaseService{log: log, pg: pg, redis: redisClient, location: location, cache: map[int64]releaseCacheEntry{}}
}

func (s *ReleaseService) SetStorageService(st *StorageService)     { s.storage = st }
func (s *ReleaseService) SetAppEventPublisher(p AppEventPublisher) { s.events = p }

func releaseNotFound() error { return apperrors.New(40430, http.StatusNotFound, "版本不存在") }

/* ───────────────────── 管理端 ───────────────────── */

func (s *ReleaseService) List(ctx context.Context, appID int64, query appdomain.ReleaseListQuery, baseURL string) (*appdomain.ReleaseListResult, error) {
	if query.Page <= 0 {
		query.Page = 1
	}
	if query.Limit <= 0 || query.Limit > 100 {
		query.Limit = 20
	}
	items, total, err := s.pg.ListReleases(ctx, appID, query)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for i := range items {
		s.decorate(ctx, &items[i], baseURL, now)
	}
	pages := int((total + int64(query.Limit) - 1) / int64(query.Limit))
	return &appdomain.ReleaseListResult{Items: items, Page: query.Page, Limit: query.Limit, Total: total, TotalPages: pages}, nil
}

func (s *ReleaseService) Detail(ctx context.Context, appID int64, releaseID int64, baseURL string) (*appdomain.Release, error) {
	item, err := s.pg.GetRelease(ctx, appID, releaseID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, releaseNotFound()
	}
	s.decorate(ctx, item, baseURL, time.Now())
	return item, nil
}

// Save 新建或更新。新建一律落在草稿，发布走 Publish。
func (s *ReleaseService) Save(ctx context.Context, mutation appdomain.ReleaseMutation, baseURL string) (*appdomain.Release, error) {
	item := appdomain.Release{
		AppID:      mutation.AppID,
		Platform:   "android",
		UpdateType: appdomain.ReleaseUpdateOptional,
		Status:     appdomain.ReleaseStatusDraft,
		Visibility: appdomain.ReleaseVisibilityPublic,
		RolloutPct: 100,
		CreatedBy:  mutation.CreatedBy,
	}
	if mutation.ID > 0 {
		current, err := s.pg.GetRelease(ctx, mutation.AppID, mutation.ID)
		if err != nil {
			return nil, err
		}
		if current == nil {
			return nil, releaseNotFound()
		}
		item = *current
	}

	if mutation.Version != nil {
		item.Version = strings.TrimSpace(*mutation.Version)
	}
	if item.Version == "" || len([]rune(item.Version)) > 64 {
		return nil, apperrors.New(40045, http.StatusBadRequest, "版本名不能为空且不超过 64 个字符")
	}
	if mutation.VersionCode != nil {
		item.VersionCode = *mutation.VersionCode
	}
	if item.VersionCode <= 0 {
		return nil, apperrors.New(40045, http.StatusBadRequest, "版本码必须为正整数")
	}
	if mutation.Title != nil {
		item.Title = strings.TrimSpace(*mutation.Title)
	}
	if len([]rune(item.Title)) > 120 {
		return nil, apperrors.New(40045, http.StatusBadRequest, "版本标题不超过 120 个字符")
	}
	if mutation.Notes != nil {
		item.Notes = sanitizeRichText(*mutation.Notes)
		if richTextIsEmpty(item.Notes) {
			item.Notes = ""
		}
	}
	item.Summary = releaseSummary(item.Notes)
	if mutation.Platform != nil {
		item.Platform = strings.ToLower(strings.TrimSpace(*mutation.Platform))
	}
	if _, ok := releasePlatforms[item.Platform]; !ok {
		return nil, apperrors.New(40045, http.StatusBadRequest, "不支持的平台")
	}
	if mutation.MinOSVersion != nil {
		item.MinOSVersion = strings.TrimSpace(*mutation.MinOSVersion)
	}
	if mutation.UpdateType != nil {
		item.UpdateType = strings.TrimSpace(*mutation.UpdateType)
	}
	if _, ok := appdomain.ValidReleaseUpdateTypes[item.UpdateType]; !ok {
		return nil, apperrors.New(40045, http.StatusBadRequest, "更新类型只能是 optional、recommended 或 force")
	}
	if mutation.MinSupportedCode != nil {
		item.MinSupportedCode = *mutation.MinSupportedCode
	}
	if item.MinSupportedCode < 0 || item.MinSupportedCode > item.VersionCode {
		return nil, apperrors.New(40045, http.StatusBadRequest, "最低支持版本码不能高于本版本")
	}
	if mutation.Visibility != nil {
		item.Visibility = strings.TrimSpace(*mutation.Visibility)
	}
	if _, ok := appdomain.ValidReleaseVisibilities[item.Visibility]; !ok {
		return nil, apperrors.New(40045, http.StatusBadRequest, "可见范围只能是 public、signed_in 或 testers")
	}
	if mutation.Targeting != nil {
		item.Targeting = normalizeTargeting(*mutation.Targeting)
	}
	if err := validateTargeting(item.Targeting); err != nil {
		return nil, err
	}
	if mutation.RolloutPct != nil {
		item.RolloutPct = *mutation.RolloutPct
	}
	if item.RolloutPct < 0 || item.RolloutPct > 100 {
		return nil, apperrors.New(40045, http.StatusBadRequest, "灰度比例应在 0 到 100 之间")
	}

	switch {
	case mutation.ClearChannel:
		item.Channel = nil
	case mutation.ChannelID != nil && *mutation.ChannelID > 0:
		channel, err := s.pg.GetVersionChannelByID(ctx, *mutation.ChannelID, mutation.AppID)
		if err != nil {
			return nil, err
		}
		if channel == nil {
			return nil, apperrors.New(40431, http.StatusNotFound, "渠道不存在")
		}
		item.Channel = &appdomain.ReleaseChannelRef{ID: channel.ID, Code: channel.Code, Name: channel.Name, Level: channel.Level, Color: channel.Color}
	}

	var assets []appdomain.ReleaseAssetInput
	if mutation.Assets != nil {
		normalized, err := normalizeReleaseAssets(mutation.Assets)
		if err != nil {
			return nil, err
		}
		assets = normalized
	}

	var channelID *int64
	if item.Channel != nil {
		channelID = &item.Channel.ID
	}
	taken, err := s.pg.ReleaseVersionCodeTaken(ctx, item.AppID, item.Platform, channelID, item.VersionCode, item.ID)
	if err != nil {
		return nil, err
	}
	if taken {
		return nil, apperrors.New(40936, http.StatusConflict, fmt.Sprintf("同平台同渠道下已有版本码 %d 的版本", item.VersionCode))
	}

	id, err := s.pg.SaveRelease(ctx, item, assets)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, releaseNotFound()
	}
	if err != nil {
		return nil, err
	}
	s.invalidate(item.AppID)
	saved, err := s.Detail(ctx, item.AppID, id, baseURL)
	if err == nil && saved.IsLive(time.Now()) {
		s.publishEvent(ctx, saved, "updated")
	}
	return saved, err
}

// Publish 立即发布，或在 publishAt 定时发布。
func (s *ReleaseService) Publish(ctx context.Context, appID int64, releaseID int64, publishAt *time.Time, baseURL string) (*appdomain.Release, error) {
	item, err := s.pg.GetRelease(ctx, appID, releaseID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, releaseNotFound()
	}
	if len(item.Assets) == 0 && item.Platform != "web" {
		return nil, apperrors.New(40937, http.StatusConflict, "请先添加安装包再发布")
	}
	now := time.Now()
	status := appdomain.ReleaseStatusPublished
	publishedAt := &now
	var at *time.Time
	if publishAt != nil && publishAt.After(now.Add(30*time.Second)) {
		status = appdomain.ReleaseStatusScheduled
		at = publishAt
		publishedAt = publishAt
	}
	if _, err := s.pg.SetReleaseStatus(ctx, appID, releaseID, status, at, publishedAt); err != nil {
		return nil, err
	}
	s.invalidate(appID)
	saved, err := s.Detail(ctx, appID, releaseID, baseURL)
	if err == nil && status == appdomain.ReleaseStatusPublished {
		s.publishEvent(ctx, saved, "published")
	}
	return saved, err
}

// Transition 暂停 / 恢复 / 撤回。
func (s *ReleaseService) Transition(ctx context.Context, appID int64, releaseID int64, action string, baseURL string) (*appdomain.Release, error) {
	item, err := s.pg.GetRelease(ctx, appID, releaseID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, releaseNotFound()
	}
	now := time.Now()
	var status string
	switch action {
	case "pause":
		if !item.IsLive(now) && item.Status != appdomain.ReleaseStatusScheduled {
			return nil, apperrors.New(40938, http.StatusConflict, "只有下发中或待发布的版本可以暂停")
		}
		status = appdomain.ReleaseStatusPaused
	case "resume":
		if item.Status != appdomain.ReleaseStatusPaused {
			return nil, apperrors.New(40938, http.StatusConflict, "只有已暂停的版本可以恢复")
		}
		status = appdomain.ReleaseStatusPublished
	case "revoke":
		status = appdomain.ReleaseStatusRevoked
	default:
		return nil, apperrors.New(40000, http.StatusBadRequest, "未知操作")
	}
	var publishedAt *time.Time
	if status == appdomain.ReleaseStatusPublished {
		publishedAt = &now
	}
	if _, err := s.pg.SetReleaseStatus(ctx, appID, releaseID, status, nil, publishedAt); err != nil {
		return nil, err
	}
	s.invalidate(appID)
	saved, err := s.Detail(ctx, appID, releaseID, baseURL)
	if err == nil {
		eventAction := map[string]string{"pause": "paused", "resume": "published", "revoke": "revoked"}[action]
		s.publishEvent(ctx, saved, eventAction)
	}
	return saved, err
}

func (s *ReleaseService) SetRollout(ctx context.Context, appID int64, releaseID int64, pct int, baseURL string) (*appdomain.Release, error) {
	if pct < 0 || pct > 100 {
		return nil, apperrors.New(40045, http.StatusBadRequest, "灰度比例应在 0 到 100 之间")
	}
	ok, err := s.pg.SetReleaseRollout(ctx, appID, releaseID, pct)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, releaseNotFound()
	}
	s.invalidate(appID)
	saved, err := s.Detail(ctx, appID, releaseID, baseURL)
	if err == nil && saved.IsLive(time.Now()) {
		s.publishEvent(ctx, saved, "updated")
	}
	return saved, err
}

func (s *ReleaseService) Delete(ctx context.Context, appID int64, releaseID int64) error {
	item, err := s.pg.GetRelease(ctx, appID, releaseID)
	if err != nil {
		return err
	}
	if item == nil {
		return releaseNotFound()
	}
	if item.IsLive(time.Now()) {
		return apperrors.New(40938, http.StatusConflict, "下发中的版本不能删除，请先撤回")
	}
	if _, err := s.pg.DeleteRelease(ctx, appID, releaseID); err != nil {
		return err
	}
	s.invalidate(appID)
	return nil
}

func (s *ReleaseService) Stats(ctx context.Context, appID int64, releaseID int64) (*appdomain.ReleaseStats, error) {
	item, err := s.pg.GetRelease(ctx, appID, releaseID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, releaseNotFound()
	}
	since := time.Now().In(s.location).AddDate(0, 0, -(releaseStatsDays - 1))
	return s.pg.ReleaseStats(ctx, releaseID, since)
}

func (s *ReleaseService) Overview(ctx context.Context, appID int64, baseURL string) (*appdomain.ReleaseOverview, error) {
	now := time.Now()
	overview, err := s.pg.ReleaseOverviewCounts(ctx, appID, now)
	if err != nil {
		return nil, err
	}
	candidates, err := s.candidates(ctx, appID)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for i := range candidates {
		rel := candidates[i]
		if !rel.IsLive(now) {
			continue
		}
		key := rel.Platform + "|"
		if rel.Channel != nil {
			key += rel.Channel.Code
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		s.decorate(ctx, &rel, baseURL, now)
		overview.Latest = append(overview.Latest, rel)
	}
	return overview, nil
}

// Simulate 以给定的客户端身份跑一遍检测，并给出每个候选版本的判定理由。
func (s *ReleaseService) Simulate(ctx context.Context, appID int64, client appdomain.ReleaseClient, baseURL string) (*appdomain.ReleaseSimulation, error) {
	viewer, err := s.viewer(ctx, appID, client)
	if err != nil {
		return nil, err
	}
	candidates, err := s.candidates(ctx, appID)
	if err != nil {
		return nil, err
	}
	result, decisions := checkReleases(candidates, viewer, time.Now())
	s.resolvePublic(ctx, &result, baseURL)
	return &appdomain.ReleaseSimulation{Result: result, Decisions: decisions}, nil
}

// UploadAsset 把安装包推到该应用的对象存储，边传边算 SHA-256。
func (s *ReleaseService) UploadAsset(ctx context.Context, appID int64, baseURL string, input ContentImageUploadInput) (*ReleaseAssetUploadResult, error) {
	if s.storage == nil {
		return nil, apperrors.New(50380, http.StatusServiceUnavailable, "存储服务未启用")
	}
	if input.ContentLength <= 0 || input.ContentLength > releaseAssetMaxSize {
		return nil, apperrors.New(40045, http.StatusBadRequest, "安装包大小应在 2 GB 以内")
	}
	name := path.Base(strings.ReplaceAll(strings.TrimSpace(input.FileName), "\\", "/"))
	ext := strings.ToLower(path.Ext(name))
	contentType := strings.TrimSpace(input.ContentType)
	if ext == ".apk" || contentType == "" || contentType == "application/octet-stream" {
		contentType = releaseContentType(ext)
	}
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	key := fmt.Sprintf("apps/%d/releases/%s/%s-%s", appID, time.Now().UTC().Format("200601"), hex.EncodeToString(buf), safeObjectName(name))
	hasher := sha256.New()
	stored, err := s.storage.UploadForApp(ctx, appID, storagedomain.UploadInput{
		AppID:         appID,
		ConfigName:    strings.TrimSpace(input.ConfigName),
		ObjectKey:     key,
		FileName:      name,
		ContentType:   contentType,
		ContentLength: input.ContentLength,
		CacheControl:  "public, max-age=31536000, immutable",
		Metadata:      map[string]string{"module": "app-release"},
		Content:       io.TeeReader(input.Content, hasher),
		UploadedBy:    input.UploadedBy,
		UploaderType:  "admin",
	})
	if err != nil {
		return nil, err
	}
	if stored == nil || stored.ConfigID <= 0 || strings.TrimSpace(stored.Key) == "" {
		return nil, apperrors.New(50381, http.StatusServiceUnavailable, "存储返回结果异常")
	}
	reference := buildStorageReference(stored.ConfigID, stored.Key)
	return &ReleaseAssetUploadResult{
		Reference:   reference,
		DownloadURL: s.resolveAssetURL(ctx, appID, reference, baseURL),
		FileName:    name,
		FileSize:    input.ContentLength,
		SHA256:      hex.EncodeToString(hasher.Sum(nil)),
		Abi:         guessAbi(name),
	}, nil
}

// ReleaseAssetUploadResult Reference 填进安装包的 url 字段。
type ReleaseAssetUploadResult struct {
	Reference   string `json:"reference"`
	DownloadURL string `json:"downloadUrl"`
	FileName    string `json:"fileName"`
	FileSize    int64  `json:"fileSize"`
	SHA256      string `json:"sha256"`
	Abi         string `json:"abi"`
}

/* ───────────────────── 客户端 ───────────────────── */

// Check 检测更新。下发时计一次曝光（同一个人一天只计一次）。
func (s *ReleaseService) Check(ctx context.Context, appID int64, client appdomain.ReleaseClient, baseURL string) (*appdomain.ReleaseCheckResult, error) {
	if client.VersionCode < 0 {
		return nil, apperrors.New(40000, http.StatusBadRequest, "versionCode 无效")
	}
	viewer, err := s.viewer(ctx, appID, client)
	if err != nil {
		return nil, err
	}
	candidates, err := s.candidates(ctx, appID)
	if err != nil {
		return nil, err
	}
	result, _ := checkReleases(candidates, viewer, time.Now())
	if result.HasUpdate {
		s.resolvePublic(ctx, &result, baseURL)
		s.record(ctx, result.Release.ID, appdomain.ReleaseEventOffered, 0, releaseSubject(client))
	}
	return &result, nil
}

// Latest 这个人此刻能拿到的最新版本（不比较版本码）。官网下载区用；没有返回 nil。
func (s *ReleaseService) Latest(ctx context.Context, appID int64, client appdomain.ReleaseClient, baseURL string) (*appdomain.PublicRelease, error) {
	items, err := s.visible(ctx, appID, client)
	if err != nil || len(items) == 0 {
		return nil, err
	}
	public := publicRelease(&items[0])
	s.resolveAssets(ctx, appID, public.Assets, baseURL)
	stripAssetSources(public.Assets)
	return &public, nil
}

// History 这个人可见的版本历史，按版本码倒序分页。
func (s *ReleaseService) History(ctx context.Context, appID int64, client appdomain.ReleaseClient, page int, limit int, baseURL string) (map[string]any, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	items, err := s.visible(ctx, appID, client)
	if err != nil {
		return nil, err
	}
	total := len(items)
	start := min((page-1)*limit, total)
	end := min(start+limit, total)
	out := make([]appdomain.PublicRelease, 0, end-start)
	for i := start; i < end; i++ {
		public := publicRelease(&items[i])
		s.resolveAssets(ctx, appID, public.Assets, baseURL)
		stripAssetSources(public.Assets)
		out = append(out, public)
	}
	return map[string]any{
		"items": out, "page": page, "limit": limit, "total": total,
		"totalPages": (total + limit - 1) / limit,
	}, nil
}

// RecordEvent 客户端上报漏斗事件。版本必须是该应用下的，否则静默忽略 —— 上报不该成为探测版本存在与否的口子。
func (s *ReleaseService) RecordEvent(ctx context.Context, appID int64, releaseID int64, event string, assetID int64, client appdomain.ReleaseClient) error {
	if _, ok := appdomain.ValidReleaseEvents[event]; !ok {
		return apperrors.New(40045, http.StatusBadRequest, "event 只能是 downloaded、installed、failed 或 dismissed")
	}
	candidates, err := s.candidates(ctx, appID)
	if err != nil {
		return err
	}
	known := slices.ContainsFunc(candidates, func(rel appdomain.Release) bool { return rel.ID == releaseID })
	if !known {
		item, err := s.pg.GetRelease(ctx, appID, releaseID)
		if err != nil || item == nil {
			return err
		}
	}
	s.record(ctx, releaseID, event, assetID, releaseSubject(client))
	return nil
}

// Channels 客户端可见的渠道：开放自助加入的，以及他已经在的。
func (s *ReleaseService) Channels(ctx context.Context, appID int64, userID int64) ([]appdomain.ReleaseChannelView, error) {
	channels, err := s.pg.ListVersionChannels(ctx, appID)
	if err != nil {
		return nil, err
	}
	joined := map[int64]bool{}
	if userID > 0 {
		ids, err := s.pg.ReleaseUserChannelIDs(ctx, appID, userID)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			joined[id] = true
		}
	}
	out := make([]appdomain.ReleaseChannelView, 0, len(channels))
	for _, channel := range channels {
		if !channel.Status || (!channel.SelfJoin && !channel.IsDefault && !joined[channel.ID]) {
			continue
		}
		out = append(out, appdomain.ReleaseChannelView{
			Code: channel.Code, Name: channel.Name, Description: channel.Description, Level: channel.Level,
			Color: channel.Color, IsDefault: channel.IsDefault, SelfJoin: channel.SelfJoin,
			Joined: channel.IsDefault || joined[channel.ID],
		})
	}
	return out, nil
}

// SetChannelMembership 自助加入或退出渠道。只对开放自助加入的渠道有效。
func (s *ReleaseService) SetChannelMembership(ctx context.Context, appID int64, userID int64, code string, join bool) ([]appdomain.ReleaseChannelView, error) {
	channel, err := s.pg.GetVersionChannelByCode(ctx, appID, strings.TrimSpace(code))
	if err != nil {
		return nil, err
	}
	if channel == nil || !channel.Status {
		return nil, apperrors.New(40431, http.StatusNotFound, "渠道不存在")
	}
	if !channel.SelfJoin || channel.IsDefault {
		return nil, apperrors.New(40338, http.StatusForbidden, "该渠道不支持自助加入或退出")
	}
	if join {
		err = s.pg.JoinVersionChannel(ctx, appID, channel.ID, userID)
	} else {
		err = s.pg.LeaveVersionChannel(ctx, appID, channel.ID, userID)
	}
	if err != nil {
		return nil, err
	}
	return s.Channels(ctx, appID, userID)
}

/* ───────────────────── 内部 ───────────────────── */

func (s *ReleaseService) candidates(ctx context.Context, appID int64) ([]appdomain.Release, error) {
	s.cacheMu.Lock()
	entry, ok := s.cache[appID]
	s.cacheMu.Unlock()
	if ok && time.Since(entry.fetchedAt) < releaseCacheTTL {
		return slices.Clone(entry.items), nil
	}
	items, err := s.pg.ListDeliverableReleases(ctx, appID)
	if err != nil {
		return nil, err
	}
	s.cacheMu.Lock()
	s.cache[appID] = releaseCacheEntry{items: items, fetchedAt: time.Now()}
	s.cacheMu.Unlock()
	return slices.Clone(items), nil
}

func (s *ReleaseService) invalidate(appID int64) {
	s.cacheMu.Lock()
	delete(s.cache, appID)
	s.cacheMu.Unlock()
}

func (s *ReleaseService) viewer(ctx context.Context, appID int64, client appdomain.ReleaseClient) (releaseViewer, error) {
	if strings.TrimSpace(client.Platform) == "" {
		client.Platform = "android"
	}
	viewer := releaseViewer{Client: client, ChannelIDs: map[int64]bool{}}
	channel, err := s.pg.EnsureDefaultVersionChannel(ctx, appID)
	if err != nil {
		return viewer, err
	}
	if channel != nil {
		viewer.DefaultChannelID = channel.ID
	}
	if client.UserID > 0 {
		ids, err := s.pg.ReleaseUserChannelIDs(ctx, appID, client.UserID)
		if err != nil {
			return viewer, err
		}
		for _, id := range ids {
			viewer.ChannelIDs[id] = true
		}
	}
	return viewer, nil
}

func (s *ReleaseService) visible(ctx context.Context, appID int64, client appdomain.ReleaseClient) ([]appdomain.Release, error) {
	viewer, err := s.viewer(ctx, appID, client)
	if err != nil {
		return nil, err
	}
	candidates, err := s.candidates(ctx, appID)
	if err != nil {
		return nil, err
	}
	return visibleReleases(candidates, viewer, time.Now()), nil
}

// record 计一次漏斗事件；有主体时同一主体同一天同一事件只计一次。
func (s *ReleaseService) record(ctx context.Context, releaseID int64, event string, assetID int64, subject string) {
	day := time.Now().In(s.location)
	if subject != "" && s.redis != nil {
		key := fmt.Sprintf("release:event:%d:%s:%s:%s", releaseID, event, day.Format("20060102"), subject)
		fresh, err := s.redis.SetNX(ctx, key, 1, releaseEventDedupeTTL).Result()
		if err == nil && !fresh {
			return
		}
	}
	if err := s.pg.RecordReleaseEvent(ctx, releaseID, event, assetID, day); err != nil {
		s.log.Warn("record release event failed", zap.Int64("release_id", releaseID), zap.String("event", event), zap.Error(err))
	}
}

func (s *ReleaseService) publishEvent(ctx context.Context, rel *appdomain.Release, action string) {
	if s.events == nil || rel == nil {
		return
	}
	if err := s.events.PublishAppEvent(ctx, rel.AppID, "release.changed", map[string]any{
		"action": action, "releaseId": rel.ID, "version": rel.Version,
		"versionCode": rel.VersionCode, "updateType": rel.UpdateType, "platform": rel.Platform,
	}); err != nil {
		s.log.Warn("publish release event failed", zap.Int64("appid", rel.AppID), zap.Error(err))
	}
}

func (s *ReleaseService) decorate(ctx context.Context, rel *appdomain.Release, baseURL string, now time.Time) {
	rel.EffectiveStatus = rel.Status
	if rel.IsLive(now) {
		rel.EffectiveStatus = appdomain.ReleaseStatusPublished
	}
	s.resolveAssets(ctx, rel.AppID, rel.Assets, baseURL)
}

// resolvePublic 解析检测结果里的下载地址并清掉落库值。result.Asset 指向 Release.Assets 内部，随之更新。
func (s *ReleaseService) resolvePublic(ctx context.Context, result *appdomain.ReleaseCheckResult, baseURL string) {
	if result.Release == nil {
		return
	}
	s.resolveAssets(ctx, 0, result.Release.Assets, baseURL)
	stripAssetSources(result.Release.Assets)
}

// resolveAssets 由落库值（外链或 storage:// 引用）填充可访问地址。
func (s *ReleaseService) resolveAssets(ctx context.Context, appID int64, assets []appdomain.ReleaseAsset, baseURL string) {
	for i := range assets {
		assets[i].DownloadURL = s.resolveAssetURL(ctx, appID, assets[i].URL, baseURL)
	}
}

func (s *ReleaseService) resolveAssetURL(ctx context.Context, appID int64, stored string, baseURL string) string {
	configID, objectKey, ok := parseStorageReference(stored)
	if !ok {
		return strings.TrimSpace(stored)
	}
	if s.storage == nil {
		return ""
	}
	result, ticketID, err := s.storage.CreateObjectLinkByConfigID(ctx, appID, configID, storagedomain.LinkRequest{
		ObjectKey: objectKey,
		Download:  true,
		FileName:  path.Base(objectKey),
		ExpiresIn: releaseAssetLinkTTL,
	})
	if err != nil || result == nil {
		s.log.Warn("resolve release asset url failed", zap.Int64("config_id", configID), zap.Error(err))
		return ""
	}
	if ticketID != "" {
		return strings.TrimRight(baseURL, "/") + "/api/storage/proxy/" + url.PathEscape(ticketID)
	}
	return strings.TrimSpace(result.URL)
}

func normalizeTargeting(t appdomain.ReleaseTargeting) appdomain.ReleaseTargeting {
	clean := func(values []string, upper bool) []string {
		out := make([]string, 0, len(values))
		for _, value := range values {
			value = strings.TrimSpace(value)
			if upper {
				value = strings.ToUpper(value)
			}
			if value != "" && !slices.Contains(out, value) {
				out = append(out, value)
			}
		}
		return out
	}
	ids := func(values []int64) []int64 {
		out := make([]int64, 0, len(values))
		for _, value := range values {
			if value > 0 && !slices.Contains(out, value) {
				out = append(out, value)
			}
		}
		return out
	}
	t.TesterUserIDs = ids(t.TesterUserIDs)
	t.ExcludeUserIDs = ids(t.ExcludeUserIDs)
	t.TesterDeviceIDs = clean(t.TesterDeviceIDs, false)
	t.ExcludeDeviceIDs = clean(t.ExcludeDeviceIDs, false)
	t.Regions = clean(t.Regions, true)
	t.Locales = clean(t.Locales, false)
	t.DeviceModels = clean(t.DeviceModels, false)
	t.Abis = clean(t.Abis, false)
	for i := range t.Abis {
		t.Abis[i] = strings.ToLower(t.Abis[i])
	}
	return t
}

func validateTargeting(t appdomain.ReleaseTargeting) error {
	if t.MinOSVersion < 0 || t.MaxOSVersion < 0 || (t.MaxOSVersion > 0 && t.MinOSVersion > t.MaxOSVersion) {
		return apperrors.New(40045, http.StatusBadRequest, "系统版本范围无效")
	}
	if t.MinSourceVersionCode < 0 || t.MaxSourceVersionCode < 0 ||
		(t.MaxSourceVersionCode > 0 && t.MinSourceVersionCode > t.MaxSourceVersionCode) {
		return apperrors.New(40045, http.StatusBadRequest, "来源版本范围无效")
	}
	if len(t.TesterUserIDs)+len(t.TesterDeviceIDs)+len(t.ExcludeUserIDs)+len(t.ExcludeDeviceIDs) > 5000 {
		return apperrors.New(40045, http.StatusBadRequest, "名单总数不超过 5000 条")
	}
	return nil
}

func normalizeReleaseAssets(inputs []appdomain.ReleaseAssetInput) ([]appdomain.ReleaseAssetInput, error) {
	if len(inputs) > releaseAssetMaxCount {
		return nil, apperrors.New(40045, http.StatusBadRequest, fmt.Sprintf("一个版本最多 %d 个安装包", releaseAssetMaxCount))
	}
	out := make([]appdomain.ReleaseAssetInput, 0, len(inputs))
	seen := map[string]bool{}
	for _, asset := range inputs {
		asset.Abi = strings.ToLower(strings.TrimSpace(asset.Abi))
		if asset.Abi == "" {
			asset.Abi = appdomain.ReleaseAssetUniversal
		}
		if !releaseAbiPattern.MatchString(asset.Abi) {
			return nil, apperrors.New(40045, http.StatusBadRequest, "安装包类型只能由小写字母、数字、- _ . 组成")
		}
		if seen[asset.Abi] {
			return nil, apperrors.New(40045, http.StatusBadRequest, fmt.Sprintf("安装包类型 %s 重复", asset.Abi))
		}
		seen[asset.Abi] = true
		asset.Label = strings.TrimSpace(asset.Label)
		asset.URL = strings.TrimSpace(asset.URL)
		if _, _, isRef := parseStorageReference(asset.URL); !isRef {
			parsed, err := url.Parse(asset.URL)
			if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
				return nil, apperrors.New(40045, http.StatusBadRequest, fmt.Sprintf("安装包 %s 的下载地址无效", asset.Abi))
			}
		}
		asset.SHA256 = strings.ToLower(strings.TrimSpace(asset.SHA256))
		if asset.SHA256 != "" && !releaseSHA256Pattern.MatchString(asset.SHA256) {
			return nil, apperrors.New(40045, http.StatusBadRequest, fmt.Sprintf("安装包 %s 的 SHA-256 格式不正确", asset.Abi))
		}
		if asset.FileSize < 0 {
			asset.FileSize = 0
		}
		out = append(out, asset)
	}
	return out, nil
}

// releaseSummary 更新说明的纯文本摘要：客户端通知栏与列表用，不带 html2text 默认的 * 与 --- 修饰。
func releaseSummary(notes string) string {
	if strings.TrimSpace(notes) == "" {
		return ""
	}
	text, err := html2text.FromString(notes, html2text.Options{OmitLinks: true, TextOnly: true})
	if err != nil {
		text = notes
	}
	runes := []rune(strings.Join(strings.Fields(text), " "))
	if len(runes) <= noticeSummaryMaxRunes {
		return string(runes)
	}
	return strings.TrimSpace(string(runes[:noticeSummaryMaxRunes])) + "…"
}

func releaseContentType(ext string) string {
	switch ext {
	case ".apk":
		return "application/vnd.android.package-archive"
	case ".ipa":
		return "application/octet-stream"
	case ".exe", ".msi":
		return "application/x-msdownload"
	case ".dmg":
		return "application/x-apple-diskimage"
	case ".zip":
		return "application/zip"
	default:
		return "application/octet-stream"
	}
}

var unsafeObjectChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func safeObjectName(name string) string {
	cleaned := strings.Trim(unsafeObjectChars.ReplaceAllString(name, "-"), "-.")
	if cleaned == "" {
		return "package"
	}
	if len(cleaned) > 96 {
		cleaned = cleaned[len(cleaned)-96:]
	}
	return cleaned
}

// guessAbi 从文件名猜安装包类型：voyage-release-1.0.0-arm64-v8a.apk → arm64-v8a。
func guessAbi(name string) string {
	lower := strings.ToLower(name)
	for _, abi := range []string{"arm64-v8a", "armeabi-v7a", "x86_64", "x86", "universal"} {
		if strings.Contains(lower, abi) {
			return abi
		}
	}
	return appdomain.ReleaseAssetUniversal
}
