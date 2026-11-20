package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"  // 素材尺寸识别
	_ "image/jpeg" // 素材尺寸识别
	_ "image/png"  // 素材尺寸识别
	"io"
	"net/http"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	_ "golang.org/x/image/webp" // 素材尺寸识别

	"aegis/internal/domain/cosmetic"
	notificationdomain "aegis/internal/domain/notification"
	storagedomain "aegis/internal/domain/storage"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
)

// 个性装扮（docs/cosmetics.md）。
//
// 管理端：装扮目录、设计器草稿与发布、版本回退、素材库、应用设置、给用户发放 / 收回 / 摘下、持有记录、统计。
// 用户端：装扮中心（每件的可戴判定与获取渠道）、我的佩戴、佩戴 / 摘下、我的持有记录、批量查别人身上的装扮。
//
// 「能不能戴」只有 cosmetic.Eligibility 一个判据：佩戴时判一次、读取时再判一次，过期 / 下架 / 收回后
// 佩戴行无需清理。各系统的发放分支（积分商城、抽奖、卡密、物品发放、邀请、排行榜、推广）都走仓储的
// grantCosmeticTx，与这里的管理员授予同一个入口。

// 错误码。
const (
	errCodeCosmeticNotFound      = 40425
	errCodeCosmeticAssetNotFound = 40426
	errCodeCosmeticOwnershipGone = 40427
	errCodeCosmeticRevisionGone  = 40428
	errCodeCosmeticNotWearable   = 40360
	errCodeCosmeticDisabled      = 40361
	errCodeCosmeticCodeTaken     = 40955
	errCodeCosmeticHasOwners     = 40956
	errCodeCosmeticAssetInUse    = 40958
	errCodeCosmeticNotPublished  = 40959
)

// 素材允许的类型。SVG 只给图片图层用（客户端按矢量绘制），序列帧必须是位图。
var cosmeticAssetTypes = map[string]string{
	".png":  "image/png",
	".webp": "image/webp",
	".gif":  "image/gif",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".svg":  "image/svg+xml",
}

const cosmeticLiveCacheTTL = 15 * time.Second

// CosmeticService 个性装扮服务。
type CosmeticService struct {
	log        *zap.Logger
	pg         *pgrepo.Repository
	storage    *StorageService
	notifier   notificationDeliverer
	userEvents UserEventPublisher

	mu    sync.Mutex
	cache map[int64]cosmeticLiveEntry
}

// cosmeticLiveEntry 一个应用上架中的装扮（设计稿里的素材地址已解析好，相对路径）。
type cosmeticLiveEntry struct {
	items    []cosmetic.Item
	byID     map[int64]cosmetic.Item
	settings cosmetic.Settings
	loaded   time.Time
}

func NewCosmeticService(log *zap.Logger, pg *pgrepo.Repository, storage *StorageService) *CosmeticService {
	return &CosmeticService{log: log, pg: pg, storage: storage, cache: make(map[int64]cosmeticLiveEntry)}
}

func (s *CosmeticService) SetNotifier(n notificationDeliverer)       { s.notifier = n }
func (s *CosmeticService) SetEventPublisher(user UserEventPublisher) { s.userEvents = user }

func (s *CosmeticService) invalidate(appID int64) {
	s.mu.Lock()
	delete(s.cache, appID)
	s.mu.Unlock()
}

func mapCosmeticError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgrepo.ErrCosmeticNotFound):
		return apperrors.New(errCodeCosmeticNotFound, http.StatusNotFound, "装扮不存在")
	case errors.Is(err, pgrepo.ErrCosmeticAssetNotFound):
		return apperrors.New(errCodeCosmeticAssetNotFound, http.StatusNotFound, "素材不存在")
	case errors.Is(err, pgrepo.ErrCosmeticOwnershipGone):
		return apperrors.New(errCodeCosmeticOwnershipGone, http.StatusNotFound, "持有记录不存在或已收回")
	case errors.Is(err, pgrepo.ErrCosmeticRevisionAbsent):
		return apperrors.New(errCodeCosmeticRevisionGone, http.StatusNotFound, "该版本不存在或已超出保留范围")
	case errors.Is(err, pgrepo.ErrCosmeticCodeTaken):
		return apperrors.New(errCodeCosmeticCodeTaken, http.StatusConflict, "装扮编码已被使用")
	case errors.Is(err, pgrepo.ErrCosmeticHasOwners):
		return apperrors.New(errCodeCosmeticHasOwners, http.StatusConflict, "已经有人持有过这件装扮，不能删除，请改为下架")
	case errors.Is(err, pgrepo.ErrCosmeticAssetInUse):
		return apperrors.New(errCodeCosmeticAssetInUse, http.StatusConflict, "素材仍被装扮引用，不能删除")
	case errors.Is(err, pgrepo.ErrCosmeticNotPublished):
		return apperrors.New(errCodeCosmeticNotPublished, http.StatusConflict, "装扮还没有发布过，不能上架")
	case errors.Is(err, pgrepo.ErrCosmeticUnavailable):
		return apperrors.New(errCodeCosmeticNotPublished, http.StatusConflict, "装扮未上架或未发布，不能发放")
	case errors.Is(err, pgrepo.ErrUserNotFound):
		return apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	return err
}

func badRequest(err error) error {
	return apperrors.New(40000, http.StatusBadRequest, err.Error())
}

// ── 管理端：目录与设置 ──

// Catalog 控制台装扮表单与设计器由它驱动：槽位、稀有度、获取方式、来源、规格枚举、模板，
// 以及本应用的会员档位与功能（获取方式的下拉框）。
func (s *CosmeticService) Catalog(ctx context.Context, appID int64) (map[string]any, error) {
	tiers, err := s.pg.ListVipTiers(ctx, appID)
	if err != nil {
		return nil, err
	}
	features, err := s.pg.ListVipFeatures(ctx, appID, false)
	if err != nil {
		return nil, err
	}
	tierRefs := make([]map[string]any, 0, len(tiers))
	for _, t := range tiers {
		tierRefs = append(tierRefs, map[string]any{"id": t.ID, "name": t.Name})
	}
	featureRefs := make([]map[string]any, 0, len(features))
	for _, f := range features {
		featureRefs = append(featureRefs, map[string]any{"tag": f.Tag, "name": f.Name, "isActive": f.IsActive})
	}
	settings, err := s.pg.GetCosmeticSettings(ctx, appID)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"slots":       cosmetic.Slots(),
		"rarities":    cosmetic.Rarities(),
		"unlockModes": cosmetic.UnlockModes(),
		"sources":     cosmetic.Sources(),
		"vocabulary":  cosmetic.DesignVocabulary(),
		"templates":   cosmetic.Templates(),
		"tiers":       tierRefs,
		"features":    featureRefs,
		"settings":    settings,
		"assetTypes":  []string{"image/png", "image/webp", "image/gif", "image/jpeg", "image/svg+xml"},
	}, nil
}

// Settings 应用设置。
func (s *CosmeticService) Settings(ctx context.Context, appID int64) (cosmetic.Settings, error) {
	return s.pg.GetCosmeticSettings(ctx, appID)
}

// SaveSettings 保存应用设置。
func (s *CosmeticService) SaveSettings(ctx context.Context, settings cosmetic.Settings) (cosmetic.Settings, error) {
	if err := cosmetic.ValidateSettings(settings); err != nil {
		return settings, badRequest(err)
	}
	saved, err := s.pg.SaveCosmeticSettings(ctx, settings)
	s.invalidate(settings.AppID)
	return saved, err
}

// ── 管理端：素材 ──

// AssetUploadInput 上传一个素材。
type AssetUploadInput struct {
	AppID       int64
	ConfigName  string
	Name        string
	FileName    string
	ContentType string
	Kind        string
	Content     io.Reader
	UploadedBy  *int64
	Operator    string
}

var svgSizePattern = regexp.MustCompile(`(?i)<svg[^>]*?\b(width|height)\s*=\s*"([0-9.]+)`)
var svgViewBoxPattern = regexp.MustCompile(`(?i)viewBox\s*=\s*"[-0-9.]+[ ,]+[-0-9.]+[ ,]+([0-9.]+)[ ,]+([0-9.]+)"`)

// UploadAsset 上传素材到应用的对象存储，按内容去重；返回带地址的素材。
func (s *CosmeticService) UploadAsset(ctx context.Context, in AssetUploadInput) (*cosmetic.Asset, error) {
	if s.storage == nil {
		return nil, apperrors.New(50380, http.StatusServiceUnavailable, "存储服务未启用")
	}
	ext := strings.ToLower(path.Ext(in.FileName))
	contentType, ok := cosmeticAssetTypes[ext]
	if !ok {
		return nil, apperrors.New(40089, http.StatusBadRequest, "素材仅支持 PNG / WEBP / GIF / JPG / SVG")
	}
	kind := strings.TrimSpace(in.Kind)
	if kind == "" {
		kind = cosmetic.AssetImage
	}
	if kind != cosmetic.AssetImage && kind != cosmetic.AssetSprite {
		return nil, badRequest(fmt.Errorf("未知的素材类型：%s", kind))
	}
	if kind == cosmetic.AssetSprite && contentType == "image/svg+xml" {
		return nil, badRequest(errors.New("序列帧素材必须是位图（PNG / WEBP）"))
	}
	data, err := io.ReadAll(io.LimitReader(in.Content, cosmetic.MaxAssetBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, apperrors.New(40087, http.StatusBadRequest, "上传文件不能为空")
	}
	if len(data) > cosmetic.MaxAssetBytes {
		return nil, apperrors.New(40088, http.StatusBadRequest, "素材不能超过 8MB")
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	if existing, err := s.pg.FindCosmeticAssetBySHA(ctx, in.AppID, digest); err == nil {
		s.resolveAssetURL(ctx, existing)
		return existing, nil
	}
	count, err := s.pg.CountCosmeticAssets(ctx, in.AppID)
	if err != nil {
		return nil, err
	}
	if count >= cosmetic.MaxAssetsPerApp {
		return nil, badRequest(fmt.Errorf("素材库最多 %d 个文件，请先清理不再使用的素材", cosmetic.MaxAssetsPerApp))
	}
	width, height := 0, 0
	if contentType == "image/svg+xml" {
		width, height = svgSize(data)
	} else {
		cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return nil, badRequest(errors.New("无法识别图片内容，文件可能已损坏"))
		}
		width, height = cfg.Width, cfg.Height
		if width > 8192 || height > 8192 {
			return nil, badRequest(errors.New("图片边长不能超过 8192 像素"))
		}
	}
	key, err := cosmeticObjectKey(in.AppID, "assets", ext)
	if err != nil {
		return nil, err
	}
	stored, err := s.storage.UploadForApp(ctx, in.AppID, storagedomain.UploadInput{
		AppID: in.AppID, ConfigName: strings.TrimSpace(in.ConfigName), ObjectKey: key, FileName: in.FileName,
		ContentType: contentType, ContentLength: int64(len(data)), CacheControl: "public, max-age=31536000, immutable",
		Metadata: map[string]string{"module": "cosmetic-asset"}, Content: bytes.NewReader(data),
		UploadedBy: in.UploadedBy, UploaderType: "admin",
	})
	if err != nil {
		return nil, err
	}
	if stored == nil || stored.ConfigID <= 0 || strings.TrimSpace(stored.Key) == "" {
		return nil, apperrors.New(50381, http.StatusServiceUnavailable, "存储返回结果异常")
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = strings.TrimSuffix(path.Base(in.FileName), ext)
	}
	asset, err := s.pg.InsertCosmeticAsset(ctx, cosmetic.Asset{
		AppID: in.AppID, Name: name, Kind: kind, ContentType: contentType, Width: width, Height: height,
		Size: int64(len(data)), SHA256: digest, Reference: buildStorageReference(stored.ConfigID, stored.Key),
		UploadedBy: in.Operator,
	})
	if err != nil {
		return nil, err
	}
	s.resolveAssetURL(ctx, asset)
	return asset, nil
}

func svgSize(data []byte) (int, int) {
	head := data
	if len(head) > 4096 {
		head = head[:4096]
	}
	w, h := 0, 0
	for _, m := range svgSizePattern.FindAllSubmatch(head, 2) {
		v, _ := strconv.ParseFloat(string(m[2]), 64)
		if strings.EqualFold(string(m[1]), "width") {
			w = int(v)
		} else {
			h = int(v)
		}
	}
	if (w == 0 || h == 0) && svgViewBoxPattern.Match(head) {
		m := svgViewBoxPattern.FindSubmatch(head)
		vw, _ := strconv.ParseFloat(string(m[1]), 64)
		vh, _ := strconv.ParseFloat(string(m[2]), 64)
		w, h = int(vw), int(vh)
	}
	return w, h
}

func cosmeticObjectKey(appID int64, folder, ext string) (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("apps/%d/cosmetics/%s/%s/%s%s", appID, folder, time.Now().UTC().Format("200601"),
		hex.EncodeToString(buf), ext), nil
}

func (s *CosmeticService) storageLink(ctx context.Context, appID int64, ref string) string {
	if s.storage == nil || strings.TrimSpace(ref) == "" {
		return ""
	}
	configID, key, ok := parseStorageReference(ref)
	if !ok {
		return ref
	}
	link, err := s.storage.PermanentObjectLink(ctx, appID, configID, key, false, "")
	if err != nil {
		s.log.Warn("resolve cosmetic asset url failed", zap.Int64("config_id", configID), zap.String("object_key", key), zap.Error(err))
		return ""
	}
	return link
}

func (s *CosmeticService) resolveAssetURL(ctx context.Context, a *cosmetic.Asset) {
	if a != nil {
		a.URL = s.storageLink(ctx, a.AppID, a.Reference)
	}
}

// ListAssets 素材库。
func (s *CosmeticService) ListAssets(ctx context.Context, f pgrepo.CosmeticAssetFilter) (map[string]any, error) {
	items, total, err := s.pg.ListCosmeticAssets(ctx, f)
	if err != nil {
		return nil, err
	}
	for i := range items {
		s.resolveAssetURL(ctx, &items[i])
	}
	return map[string]any{"items": items, "total": total}, nil
}

// DeleteAsset 删一个没被引用的素材（连同对象存储里的文件）。
func (s *CosmeticService) DeleteAsset(ctx context.Context, appID, assetID int64) error {
	asset, err := s.pg.DeleteCosmeticAsset(ctx, appID, assetID)
	if err != nil {
		return mapCosmeticError(err)
	}
	if configID, key, ok := parseStorageReference(asset.Reference); ok && s.storage != nil {
		if err := s.storage.DeleteObject(ctx, configID, key); err != nil {
			s.log.Warn("delete cosmetic asset object failed", zap.Int64("asset_id", assetID), zap.Error(err))
		}
	}
	return nil
}

// ── 管理端：装扮 ──

// ListItems 管理端装扮列表。
func (s *CosmeticService) ListItems(ctx context.Context, f pgrepo.CosmeticItemFilter) ([]cosmetic.Item, error) {
	items, err := s.pg.ListCosmeticItems(ctx, f)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Preview = s.storageLink(ctx, f.AppID, items[i].PreviewRef)
		// 列表不带设计稿：一页几十件，每件的图层与关键帧加起来很大，设计器打开时再取单件
		items[i].Design, items[i].Draft = nil, nil
	}
	return items, nil
}

// GetItem 管理端单件（含草稿与已发布的设计稿，素材地址已解析）。
func (s *CosmeticService) GetItem(ctx context.Context, appID, id int64) (*cosmetic.Item, error) {
	item, err := s.pg.GetCosmeticItem(ctx, appID, id)
	if err != nil {
		return nil, mapCosmeticError(err)
	}
	s.decorateItem(ctx, item)
	return item, nil
}

func (s *CosmeticService) decorateItem(ctx context.Context, item *cosmetic.Item) {
	item.Preview = s.storageLink(ctx, item.AppID, item.PreviewRef)
	ids := []int64{}
	if item.Design != nil {
		ids = append(ids, item.Design.AssetIDs()...)
	}
	if item.Draft != nil {
		ids = append(ids, item.Draft.AssetIDs()...)
	}
	if len(ids) == 0 {
		return
	}
	assets, err := s.pg.GetCosmeticAssets(ctx, item.AppID, ids)
	if err != nil {
		s.log.Warn("load cosmetic assets failed", zap.Int64("cosmetic_id", item.ID), zap.Error(err))
		return
	}
	s.fillDesignAssets(ctx, item.AppID, item.Design, assets)
	s.fillDesignAssets(ctx, item.AppID, item.Draft, assets)
}

func (s *CosmeticService) fillDesignAssets(ctx context.Context, appID int64, d *cosmetic.Design, assets map[int64]cosmetic.Asset) {
	if d == nil || len(d.Assets) == 0 {
		return
	}
	for key, ref := range d.Assets {
		if a, ok := assets[ref.ID]; ok {
			ref.URL = s.storageLink(ctx, appID, a.Reference)
			ref.Width, ref.Height, ref.ContentType, ref.SHA256 = a.Width, a.Height, a.ContentType, a.SHA256
			d.Assets[key] = ref
		}
	}
}

// checkUnlockRefs 获取方式引用的档位与功能必须存在。
func (s *CosmeticService) checkUnlockRefs(ctx context.Context, appID int64, u cosmetic.Unlock) error {
	switch u.Mode {
	case cosmetic.UnlockVIP:
		if len(u.TierIDs) == 0 {
			return nil
		}
		tiers, err := s.pg.ListVipTiers(ctx, appID)
		if err != nil {
			return err
		}
		for _, id := range u.TierIDs {
			if !slices.ContainsFunc(tiers, func(t vipdomain.Tier) bool { return t.ID == id }) {
				return badRequest(fmt.Errorf("会员档位 #%d 不存在", id))
			}
		}
	case cosmetic.UnlockFeature:
		features, err := s.pg.ListVipFeatures(ctx, appID, false)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(features, func(f vipdomain.Feature) bool { return f.Tag == u.Feature }) {
			return badRequest(fmt.Errorf("功能 %s 不存在", u.Feature))
		}
	}
	return nil
}

// CreateItem 新建装扮；templateKey 非空时草稿用模板（模板槽位必须一致），否则用空白设计稿。
func (s *CosmeticService) CreateItem(ctx context.Context, in cosmetic.ItemInput, templateKey string) (*cosmetic.Item, error) {
	in = cosmetic.NormalizeItemInput(in)
	if err := cosmetic.ValidateItemInput(in); err != nil {
		return nil, badRequest(err)
	}
	if err := s.checkUnlockRefs(ctx, in.AppID, in.Unlock); err != nil {
		return nil, err
	}
	count, err := s.pg.CountCosmeticItems(ctx, in.AppID)
	if err != nil {
		return nil, err
	}
	if count >= cosmetic.MaxItemsPerApp {
		return nil, badRequest(fmt.Errorf("一个应用最多 %d 件装扮", cosmetic.MaxItemsPerApp))
	}
	draft := cosmetic.NewDesign(in.Slot)
	if key := strings.TrimSpace(templateKey); key != "" {
		tpl, ok := cosmetic.FindTemplate(key)
		if !ok {
			return nil, badRequest(fmt.Errorf("模板 %s 不存在", key))
		}
		if tpl.Slot != in.Slot {
			return nil, badRequest(fmt.Errorf("模板「%s」是%s模板，不能用于%s", tpl.Name, cosmetic.SlotLabel(tpl.Slot), cosmetic.SlotLabel(in.Slot)))
		}
		draft = tpl.Design
	}
	draft = cosmetic.NormalizeDesign(in.Slot, draft)
	item, err := s.pg.CreateCosmeticItem(ctx, in, draft)
	if err != nil {
		return nil, mapCosmeticError(err)
	}
	s.decorateItem(ctx, item)
	return item, nil
}

// UpdateItem 改基本信息。
func (s *CosmeticService) UpdateItem(ctx context.Context, id int64, in cosmetic.ItemInput) (*cosmetic.Item, error) {
	current, err := s.pg.GetCosmeticItem(ctx, in.AppID, id)
	if err != nil {
		return nil, mapCosmeticError(err)
	}
	in.Slot = current.Slot
	in = cosmetic.NormalizeItemInput(in)
	if err := cosmetic.ValidateItemInput(in); err != nil {
		return nil, badRequest(err)
	}
	if err := s.checkUnlockRefs(ctx, in.AppID, in.Unlock); err != nil {
		return nil, err
	}
	item, err := s.pg.UpdateCosmeticItemMeta(ctx, id, in)
	if err != nil {
		return nil, mapCosmeticError(err)
	}
	s.invalidate(in.AppID)
	s.decorateItem(ctx, item)
	return item, nil
}

// checkDesignAssets 设计稿引用的素材必须属于本应用；序列帧图层必须引用序列帧素材。
func (s *CosmeticService) checkDesignAssets(ctx context.Context, appID int64, d cosmetic.Design) error {
	ids := d.AssetIDs()
	if len(ids) == 0 {
		return nil
	}
	assets, err := s.pg.GetCosmeticAssets(ctx, appID, ids)
	if err != nil {
		return err
	}
	for key, ref := range d.Assets {
		if _, ok := assets[ref.ID]; !ok {
			return badRequest(fmt.Errorf("素材「%s」（#%d）不存在或不属于本应用", key, ref.ID))
		}
	}
	for _, layer := range d.Layers {
		if layer.Type == cosmetic.LayerSprite {
			if a := assets[d.Assets[layer.Asset].ID]; a.ContentType == "image/svg+xml" {
				return badRequest(fmt.Errorf("图层「%s」：序列帧不能使用 SVG 素材", layer.Name))
			}
		}
	}
	return nil
}

// SaveDraft 保存设计器草稿。草稿也要整份通过校验：存进去的草稿就是发布时会被校验的那一份，
// 现在不拦着，发布时再报「第 12 个图层有问题」就晚了。
func (s *CosmeticService) SaveDraft(ctx context.Context, appID, id int64, draft cosmetic.Design) (*cosmetic.Item, error) {
	current, err := s.pg.GetCosmeticItem(ctx, appID, id)
	if err != nil {
		return nil, mapCosmeticError(err)
	}
	draft = cosmetic.NormalizeDesign(current.Slot, draft)
	if err := cosmetic.ValidateDesign(current.Slot, draft); err != nil {
		return nil, badRequest(err)
	}
	if err := s.checkDesignAssets(ctx, appID, draft); err != nil {
		return nil, err
	}
	item, err := s.pg.SaveCosmeticDraft(ctx, appID, id, draft)
	if err != nil {
		return nil, mapCosmeticError(err)
	}
	s.decorateItem(ctx, item)
	return item, nil
}

// Publish 发布草稿。
func (s *CosmeticService) Publish(ctx context.Context, appID, id int64, note, operator string) (*cosmetic.Item, error) {
	current, err := s.pg.GetCosmeticItem(ctx, appID, id)
	if err != nil {
		return nil, mapCosmeticError(err)
	}
	if current.Draft == nil {
		return nil, badRequest(errors.New("还没有设计稿"))
	}
	if !current.DraftDirty && current.Revision > 0 {
		return nil, badRequest(errors.New("草稿与已发布的版本相同，没有需要发布的修改"))
	}
	if err := cosmetic.ValidateDesign(current.Slot, *current.Draft); err != nil {
		return nil, badRequest(err)
	}
	if err := s.checkDesignAssets(ctx, appID, *current.Draft); err != nil {
		return nil, err
	}
	if len([]rune(note)) > cosmetic.MaxNoteRunes {
		return nil, badRequest(fmt.Errorf("发布说明不能超过 %d 字", cosmetic.MaxNoteRunes))
	}
	item, err := s.pg.PublishCosmeticItem(ctx, appID, id, note, "", operator)
	if err != nil {
		return nil, mapCosmeticError(err)
	}
	s.invalidate(appID)
	s.decorateItem(ctx, item)
	return item, nil
}

// UploadPreview 设计器渲染出的静态预览图（PNG / WEBP），直接替换当前预览。
func (s *CosmeticService) UploadPreview(ctx context.Context, appID, id int64, in AssetUploadInput) (string, error) {
	if s.storage == nil {
		return "", apperrors.New(50380, http.StatusServiceUnavailable, "存储服务未启用")
	}
	if _, err := s.pg.GetCosmeticItem(ctx, appID, id); err != nil {
		return "", mapCosmeticError(err)
	}
	ext := strings.ToLower(path.Ext(in.FileName))
	contentType, ok := cosmeticAssetTypes[ext]
	if !ok || (contentType != "image/png" && contentType != "image/webp") {
		return "", apperrors.New(40089, http.StatusBadRequest, "预览图仅支持 PNG / WEBP")
	}
	data, err := io.ReadAll(io.LimitReader(in.Content, cosmetic.MaxPreviewBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) == 0 || len(data) > cosmetic.MaxPreviewBytes {
		return "", apperrors.New(40088, http.StatusBadRequest, "预览图不能为空且不能超过 2MB")
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil {
		return "", badRequest(errors.New("无法识别预览图内容"))
	}
	key, err := cosmeticObjectKey(appID, "previews", ext)
	if err != nil {
		return "", err
	}
	stored, err := s.storage.UploadForApp(ctx, appID, storagedomain.UploadInput{
		AppID: appID, ConfigName: strings.TrimSpace(in.ConfigName), ObjectKey: key, FileName: in.FileName,
		ContentType: contentType, ContentLength: int64(len(data)), CacheControl: "public, max-age=31536000, immutable",
		Metadata: map[string]string{"module": "cosmetic-preview"}, Content: bytes.NewReader(data),
		UploadedBy: in.UploadedBy, UploaderType: "admin",
	})
	if err != nil {
		return "", err
	}
	ref := buildStorageReference(stored.ConfigID, stored.Key)
	if err := s.pg.SetCosmeticPreview(ctx, appID, id, ref); err != nil {
		return "", mapCosmeticError(err)
	}
	s.invalidate(appID)
	return s.storageLink(ctx, appID, ref), nil
}

// Revisions 发布历史。
func (s *CosmeticService) Revisions(ctx context.Context, appID, id int64) ([]cosmetic.Revision, error) {
	if _, err := s.pg.GetCosmeticItem(ctx, appID, id); err != nil {
		return nil, mapCosmeticError(err)
	}
	return s.pg.ListCosmeticRevisions(ctx, appID, id)
}

// RestoreRevision 把某个历史版本放回草稿（不直接发布：回退也要人看一眼再发）。
func (s *CosmeticService) RestoreRevision(ctx context.Context, appID, id int64, revision int) (*cosmetic.Item, error) {
	design, err := s.pg.GetCosmeticRevision(ctx, appID, id, revision)
	if err != nil {
		return nil, mapCosmeticError(err)
	}
	return s.SaveDraft(ctx, appID, id, *design)
}

// SetStatus 上架 / 下架 / 回到草稿。
func (s *CosmeticService) SetStatus(ctx context.Context, appID, id int64, status string) (*cosmetic.Item, error) {
	switch status {
	case cosmetic.StatusActive, cosmetic.StatusArchived, cosmetic.StatusDraft:
	default:
		return nil, badRequest(fmt.Errorf("未知的状态：%s", status))
	}
	item, err := s.pg.SetCosmeticItemStatus(ctx, appID, id, status)
	if err != nil {
		return nil, mapCosmeticError(err)
	}
	s.invalidate(appID)
	s.decorateItem(ctx, item)
	return item, nil
}

// Duplicate 复制一件装扮（基本信息 + 草稿），新编码自动加后缀，状态为草稿。
func (s *CosmeticService) Duplicate(ctx context.Context, appID, id int64, operator string) (*cosmetic.Item, error) {
	src, err := s.pg.GetCosmeticItem(ctx, appID, id)
	if err != nil {
		return nil, mapCosmeticError(err)
	}
	draft := src.Draft
	if draft == nil {
		empty := cosmetic.NewDesign(src.Slot)
		draft = &empty
	}
	for attempt := 1; attempt <= 20; attempt++ {
		code := src.Code + "-copy"
		if attempt > 1 {
			code += strconv.Itoa(attempt)
		}
		if len(code) > cosmetic.MaxCodeLen {
			code = code[len(code)-cosmetic.MaxCodeLen:]
		}
		name := []rune(src.Name + "（副本）")
		if len(name) > cosmetic.MaxNameRunes {
			name = name[:cosmetic.MaxNameRunes]
		}
		item, err := s.pg.CreateCosmeticItem(ctx, cosmetic.ItemInput{
			AppID: appID, Slot: src.Slot, Code: code, Name: string(name), Description: src.Description,
			Rarity: src.Rarity, Series: src.Series, Unlock: src.Unlock, Hidden: src.Hidden, Operator: operator,
		}, *draft)
		if errors.Is(err, pgrepo.ErrCosmeticCodeTaken) {
			continue
		}
		if err != nil {
			return nil, mapCosmeticError(err)
		}
		s.decorateItem(ctx, item)
		return item, nil
	}
	return nil, apperrors.New(errCodeCosmeticCodeTaken, http.StatusConflict, "副本编码都被占用了，请先改名")
}

// Reorder 重排。
func (s *CosmeticService) Reorder(ctx context.Context, appID int64, ids []int64) (int64, error) {
	if len(ids) == 0 || len(ids) > cosmetic.MaxItemsPerApp {
		return 0, badRequest(errors.New("排序的装扮数量不正确"))
	}
	n, err := s.pg.ReorderCosmeticItems(ctx, appID, ids)
	s.invalidate(appID)
	return n, err
}

// DeleteItem 删除。
func (s *CosmeticService) DeleteItem(ctx context.Context, appID, id int64) error {
	if err := s.pg.DeleteCosmeticItem(ctx, appID, id); err != nil {
		return mapCosmeticError(err)
	}
	s.invalidate(appID)
	return nil
}

// ── 管理端：用户 ──

// Grant 管理员给用户发一件装扮，站内信通知。
func (s *CosmeticService) Grant(ctx context.Context, in cosmetic.GrantInput) (*cosmetic.GrantResult, error) {
	if err := cosmetic.ValidateGrantDays(in.Days); err != nil {
		return nil, badRequest(err)
	}
	in.Source = cosmetic.SourceAdminGrant
	result, err := s.pg.GrantCosmetic(ctx, in)
	if err != nil {
		return nil, mapCosmeticError(err)
	}
	if !result.AlreadyPermanent {
		s.notifyGrant(ctx, in.AppID, in.UserID, result, in.Days)
	}
	s.publishChanged(ctx, in.AppID, in.UserID, "granted")
	return result, nil
}

func (s *CosmeticService) notifyGrant(ctx context.Context, appID, userID int64, result *cosmetic.GrantResult, days int) {
	if s.notifier == nil || result == nil {
		return
	}
	_, err := s.notifier.Deliver(ctx, notificationdomain.DeliverInput{
		AppID: appID, UserID: userID, Type: notificationdomain.TypeCosmetic,
		Title:    "获得新装扮",
		Content:  "你获得了" + cosmetic.SlotLabel(result.Slot) + "「" + result.CosmeticName + "」（" + cosmetic.TermLabel(days) + "），可以在个性装扮里佩戴。",
		Level:    "success",
		Metadata: map[string]any{"cosmeticName": result.CosmeticName, "slot": result.Slot, "days": days},
		Action:   &notificationdomain.Action{Type: notificationdomain.ActionRoute, Target: notificationdomain.RouteCosmetics},
	})
	if err != nil {
		s.log.Warn("deliver cosmetic notification failed", zap.Int64("user_id", userID), zap.Error(err))
	}
}

func (s *CosmeticService) publishChanged(ctx context.Context, appID, userID int64, action string) {
	if s.userEvents == nil {
		return
	}
	if err := s.userEvents.PublishUserEvent(ctx, appID, userID, "cosmetic.changed", map[string]any{"action": action}); err != nil {
		s.log.Debug("publish cosmetic event failed", zap.Error(err))
	}
}

// Revoke 收回一条持有。
func (s *CosmeticService) Revoke(ctx context.Context, appID, userID, ownershipID int64, operator, reason string) (*cosmetic.Ownership, error) {
	if len([]rune(reason)) > 256 {
		return nil, badRequest(errors.New("原因不能超过 256 字"))
	}
	own, err := s.pg.RevokeCosmeticOwnership(ctx, appID, userID, ownershipID, operator, reason)
	if err != nil {
		return nil, mapCosmeticError(err)
	}
	s.publishChanged(ctx, appID, userID, "revoked")
	return own, nil
}

// AdminUnequip 管理员摘下用户某个槽位（不合规的自定义内容、投诉处理）。position < 0 为整个槽位。
func (s *CosmeticService) AdminUnequip(ctx context.Context, appID, userID int64, slot string, position int) error {
	if _, ok := cosmetic.FindSlot(slot); !ok {
		return badRequest(fmt.Errorf("未登记的槽位：%s", slot))
	}
	if err := s.pg.ClearUserCosmeticEquip(ctx, appID, userID, slot, position); err != nil {
		return err
	}
	s.publishChanged(ctx, appID, userID, "unequipped")
	return nil
}

// UserProfile 管理端「用户详情 → 装扮」：持有记录、此刻身上的装扮、全部佩戴行与各自是否生效。
func (s *CosmeticService) UserProfile(ctx context.Context, appID, userID int64, baseURL string) (map[string]any, error) {
	owned, _, err := s.pg.ListCosmeticOwnerships(ctx, pgrepo.CosmeticOwnershipFilter{AppID: appID, UserID: userID, Limit: 200})
	if err != nil {
		return nil, err
	}
	facts, err := s.pg.GetCosmeticUserFacts(ctx, appID, []int64{userID}, false)
	if err != nil {
		return nil, err
	}
	f := facts[userID]
	if f == nil {
		return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	live, err := s.live(ctx, appID)
	if err != nil {
		return nil, err
	}
	all, err := s.pg.ListCosmeticItems(ctx, pgrepo.CosmeticItemFilter{AppID: appID})
	if err != nil {
		return nil, err
	}
	names := make(map[int64]cosmetic.Item, len(all))
	for _, it := range all {
		names[it.ID] = it
	}
	now := time.Now()
	equips := make([]map[string]any, 0, len(f.Equips))
	for _, e := range f.Equips {
		item, ok := live.byID[e.CosmeticID]
		effective, reason := false, cosmetic.ReasonUnavailable
		if ok {
			effective, reason = cosmetic.Eligibility(item, f.Facts, now)
		}
		equips = append(equips, map[string]any{"slot": e.Slot, "position": e.Position, "cosmeticId": e.CosmeticID,
			"name": names[e.CosmeticID].Name, "equippedAt": e.EquippedAt, "effective": effective, "reason": reason})
	}
	return map[string]any{
		"ownerships": owned,
		"equips":     equips,
		"loadout":    s.loadoutOf(live, f, now, baseURL),
		"facts": map[string]any{"isVip": f.Facts.IsVIP, "tierIds": f.Facts.TierIDs, "level": f.Facts.Level,
			"features": f.Facts.Features},
	}, nil
}

// Ownerships 管理端持有记录。
func (s *CosmeticService) Ownerships(ctx context.Context, f pgrepo.CosmeticOwnershipFilter) (map[string]any, error) {
	items, total, err := s.pg.ListCosmeticOwnerships(ctx, f)
	if err != nil {
		return nil, err
	}
	return map[string]any{"items": items, "total": total}, nil
}

// Stats 管理端统计。
func (s *CosmeticService) Stats(ctx context.Context, appID int64) (*cosmetic.Stats, error) {
	return s.pg.CosmeticStats(ctx, appID)
}

// ── 用户端 ──

// live 应用上架中的装扮与设置（短缓存：设计稿的素材地址已解析成相对路径）。
func (s *CosmeticService) live(ctx context.Context, appID int64) (cosmeticLiveEntry, error) {
	s.mu.Lock()
	entry, ok := s.cache[appID]
	s.mu.Unlock()
	if ok && time.Since(entry.loaded) < cosmeticLiveCacheTTL {
		return entry, nil
	}
	settings, err := s.pg.GetCosmeticSettings(ctx, appID)
	if err != nil {
		return entry, err
	}
	items, err := s.pg.ListLiveCosmeticItems(ctx, appID)
	if err != nil {
		return entry, err
	}
	ids := make([]int64, 0)
	for _, it := range items {
		if it.Design != nil {
			ids = append(ids, it.Design.AssetIDs()...)
		}
	}
	assets, err := s.pg.GetCosmeticAssets(ctx, appID, ids)
	if err != nil {
		return entry, err
	}
	entry = cosmeticLiveEntry{items: items, byID: make(map[int64]cosmetic.Item, len(items)), settings: settings, loaded: time.Now()}
	for i := range entry.items {
		it := &entry.items[i]
		it.Preview = s.storageLink(ctx, appID, it.PreviewRef)
		s.fillDesignAssets(ctx, appID, it.Design, assets)
		entry.byID[it.ID] = *it
	}
	s.mu.Lock()
	s.cache[appID] = entry
	s.mu.Unlock()
	return entry, nil
}

// absoluteView 给客户端的视图：预览图与素材地址拼成绝对地址（App 不认相对路径），设计稿深拷贝后再改。
func absoluteView(item cosmetic.Item, baseURL string) cosmetic.View {
	view := item.ToView()
	view.Preview = absoluteLink(baseURL, view.Preview)
	if item.Design != nil {
		d := *item.Design
		if len(d.Assets) > 0 {
			assets := make(map[string]cosmetic.AssetRef, len(d.Assets))
			for k, v := range d.Assets {
				v.URL = absoluteLink(baseURL, v.URL)
				assets[k] = v
			}
			d.Assets = assets
		}
		view.Design = &d
	}
	return view
}

func absoluteLink(baseURL, link string) string {
	if link == "" || strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") || baseURL == "" {
		return link
	}
	return joinBaseURL(baseURL, link)
}

func (s *CosmeticService) ensureEnabled(entry cosmeticLiveEntry) error {
	if !entry.settings.Enabled {
		return apperrors.New(errCodeCosmeticDisabled, http.StatusForbidden, "当前应用未开启个性装扮")
	}
	return nil
}

// loadoutOf 按资格过滤后的身上装扮。
func (s *CosmeticService) loadoutOf(entry cosmeticLiveEntry, f *pgrepo.CosmeticUserFacts, now time.Time, baseURL string) cosmetic.Loadout {
	out := cosmetic.Loadout{Badges: []cosmetic.View{}}
	if f == nil || !entry.settings.Enabled {
		return out
	}
	for _, e := range f.Equips {
		item, ok := entry.byID[e.CosmeticID]
		if !ok || item.Slot != e.Slot {
			continue
		}
		if usable, _ := cosmetic.Eligibility(item, f.Facts, now); !usable {
			continue
		}
		view := absoluteView(item, baseURL)
		switch e.Slot {
		case cosmetic.SlotAvatarFrame:
			out.AvatarFrame = &view
		case cosmetic.SlotProfileCard:
			out.ProfileCard = &view
		case cosmetic.SlotNameStyle:
			out.NameStyle = &view
		case cosmetic.SlotBadge:
			if e.Position < entry.settings.BadgeSlots {
				out.Badges = append(out.Badges, view)
			}
		}
	}
	return out
}

func (s *CosmeticService) userFacts(ctx context.Context, appID, userID int64) (*pgrepo.CosmeticUserFacts, error) {
	facts, err := s.pg.GetCosmeticUserFacts(ctx, appID, []int64{userID}, false)
	if err != nil {
		return nil, err
	}
	f := facts[userID]
	if f == nil {
		return nil, apperrors.New(40401, http.StatusNotFound, "用户不存在")
	}
	return f, nil
}

// Center 装扮中心：设置、槽位、每件装扮的外观与我的状态、获取渠道、我身上的装扮。
func (s *CosmeticService) Center(ctx context.Context, appID, userID int64, baseURL string) (map[string]any, error) {
	entry, err := s.live(ctx, appID)
	if err != nil {
		return nil, err
	}
	if err := s.ensureEnabled(entry); err != nil {
		return nil, err
	}
	f, err := s.userFacts(ctx, appID, userID)
	if err != nil {
		return nil, err
	}
	channels, err := s.pg.CosmeticChannels(ctx, appID)
	if err != nil {
		s.log.Warn("load cosmetic channels failed", zap.Int64("appid", appID), zap.Error(err))
		channels = nil
	}
	tierNames := map[int64]string{}
	if tiers, err := s.pg.ListVipTiers(ctx, appID); err == nil {
		for _, t := range tiers {
			tierNames[t.ID] = t.Name
		}
	}
	equippedAt := make(map[int64]cosmetic.Equip, len(f.Equips))
	for _, e := range f.Equips {
		equippedAt[e.CosmeticID] = e
	}
	ownedEver := map[int64]bool{}
	if history, _, err := s.pg.ListCosmeticOwnerships(ctx, pgrepo.CosmeticOwnershipFilter{AppID: appID, UserID: userID, Limit: 200}); err == nil {
		for _, o := range history {
			ownedEver[o.CosmeticID] = true
		}
	}
	now := time.Now()
	items := make([]cosmetic.CenterItem, 0, len(entry.items))
	for _, item := range entry.items {
		expires, owned := f.Facts.Owned[item.ID]
		usable, reason := cosmetic.Eligibility(item, f.Facts, now)
		// 只能发放获得、没有在身上的隐藏装扮不列出（活动限定、补偿专用）；持有过的照常列出
		if item.Hidden && !owned && !ownedEver[item.ID] {
			continue
		}
		if !usable && reason == cosmetic.ReasonNotOwned && ownedEver[item.ID] {
			reason = cosmetic.ReasonExpired
		}
		state := cosmetic.ItemState{Usable: usable, Owned: owned, Permanent: owned && expires == nil, ExpiresAt: expires, Reason: reason}
		if e, ok := equippedAt[item.ID]; ok && usable && (item.Slot != cosmetic.SlotBadge || e.Position < entry.settings.BadgeSlots) {
			state.Equipped, state.Position = true, e.Position
		}
		unlock := cosmetic.UnlockView{Mode: item.Unlock.Mode, Label: cosmetic.UnlockLabel(item.Unlock), Hint: item.Unlock.Hint,
			MinLevel: item.Unlock.MinLevel, Feature: item.Unlock.Feature}
		for _, id := range item.Unlock.TierIDs {
			if name, ok := tierNames[id]; ok {
				unlock.Tiers = append(unlock.Tiers, name)
			}
		}
		items = append(items, cosmetic.CenterItem{View: absoluteView(item, baseURL), Unlock: unlock, State: state,
			Channels: channels[item.ID]})
	}
	return map[string]any{
		"settings": map[string]any{"badgeSlots": entry.settings.BadgeSlots},
		"slots":    cosmetic.Slots(),
		"rarities": cosmetic.Rarities(),
		"items":    items,
		"loadout":  s.loadoutOf(entry, f, now, baseURL),
	}, nil
}

// MyLoadout 我此刻身上的装扮。应用没开启时返回空（不报错：每个页面都会取，关掉装扮不该让它们报错）。
func (s *CosmeticService) MyLoadout(ctx context.Context, appID, userID int64, baseURL string) (cosmetic.Loadout, error) {
	entry, err := s.live(ctx, appID)
	if err != nil {
		return cosmetic.Loadout{Badges: []cosmetic.View{}}, err
	}
	if !entry.settings.Enabled {
		return cosmetic.Loadout{Badges: []cosmetic.View{}}, nil
	}
	f, err := s.userFacts(ctx, appID, userID)
	if err != nil {
		return cosmetic.Loadout{Badges: []cosmetic.View{}}, err
	}
	return s.loadoutOf(entry, f, time.Now(), baseURL), nil
}

// EquipInput 佩戴 / 摘下。CosmeticID 为 0 即摘下这个槽位（勋章摘下 Position 那一位；Position 为 -1 摘下全部勋章）。
type EquipInput struct {
	AppID      int64
	UserID     int64
	Slot       string
	Position   int
	CosmeticID int64
	BaseURL    string
}

// Equip 佩戴或摘下，返回新的身上装扮。
func (s *CosmeticService) Equip(ctx context.Context, in EquipInput) (cosmetic.Loadout, error) {
	empty := cosmetic.Loadout{Badges: []cosmetic.View{}}
	entry, err := s.live(ctx, in.AppID)
	if err != nil {
		return empty, err
	}
	if err := s.ensureEnabled(entry); err != nil {
		return empty, err
	}
	spec, ok := cosmetic.FindSlot(in.Slot)
	if !ok {
		return empty, badRequest(fmt.Errorf("未登记的槽位：%s", in.Slot))
	}
	if !spec.Multiple {
		in.Position = 0
	} else if in.Position >= entry.settings.BadgeSlots || in.Position < -1 || (in.Position == -1 && in.CosmeticID != 0) {
		return empty, badRequest(fmt.Errorf("勋章位置需要在 0–%d 之间", entry.settings.BadgeSlots-1))
	}
	if in.CosmeticID == 0 {
		if err := s.pg.ClearUserCosmeticEquip(ctx, in.AppID, in.UserID, in.Slot, in.Position); err != nil {
			return empty, err
		}
	} else {
		item, ok := entry.byID[in.CosmeticID]
		if !ok {
			return empty, apperrors.New(errCodeCosmeticNotWearable, http.StatusForbidden, "这件装扮已下架").WithData(map[string]any{"reason": cosmetic.ReasonUnavailable})
		}
		if item.Slot != in.Slot {
			return empty, apperrors.New(errCodeCosmeticNotWearable, http.StatusBadRequest,
				"这件装扮是"+cosmetic.SlotLabel(item.Slot)+"，不能戴在"+spec.Label+"上").
				WithData(map[string]any{"reason": cosmetic.ReasonSlotMismatch})
		}
		f, err := s.userFacts(ctx, in.AppID, in.UserID)
		if err != nil {
			return empty, err
		}
		if usable, reason := cosmetic.Eligibility(item, f.Facts, time.Now()); !usable {
			return empty, apperrors.New(errCodeCosmeticNotWearable, http.StatusForbidden,
				"暂时不能佩戴："+cosmetic.ReasonLabel(reason)).WithData(map[string]any{"reason": reason})
		}
		if err := s.pg.SetUserCosmeticEquip(ctx, in.AppID, in.UserID, in.Slot, in.Position, in.CosmeticID); err != nil {
			return empty, err
		}
	}
	s.publishChanged(ctx, in.AppID, in.UserID, "equipped")
	return s.MyLoadout(ctx, in.AppID, in.UserID, in.BaseURL)
}

// History 我的持有记录。
func (s *CosmeticService) History(ctx context.Context, appID, userID int64, limit, offset int) (map[string]any, error) {
	items, total, err := s.pg.ListCosmeticOwnerships(ctx, pgrepo.CosmeticOwnershipFilter{AppID: appID, UserID: userID, Limit: limit, Offset: offset})
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i].UserID, items[i].AppID, items[i].UserAccount, items[i].UserNickname = 0, 0, "", ""
		items[i].Operator, items[i].RevokedBy = "", ""
	}
	return map[string]any{"items": items, "total": total}, nil
}

// Lookup 批量查别人身上的装扮（排行榜、一起听、评论区等显示别人头像的地方）。键是对外用户 ID。
func (s *CosmeticService) Lookup(ctx context.Context, appID int64, publicIDs []int64, baseURL string) (map[string]cosmetic.Loadout, error) {
	out := make(map[string]cosmetic.Loadout, len(publicIDs))
	if len(publicIDs) == 0 {
		return out, nil
	}
	if len(publicIDs) > cosmetic.MaxLookupUsers {
		return nil, badRequest(fmt.Errorf("一次最多查询 %d 个用户", cosmetic.MaxLookupUsers))
	}
	entry, err := s.live(ctx, appID)
	if err != nil {
		return nil, err
	}
	if !entry.settings.Enabled {
		return out, nil
	}
	ids, err := s.pg.ResolveCosmeticPublicIDs(ctx, appID, publicIDs)
	if err != nil {
		return nil, err
	}
	userIDs := make([]int64, 0, len(ids))
	for _, id := range ids {
		userIDs = append(userIDs, id)
	}
	facts, err := s.pg.GetCosmeticUserFacts(ctx, appID, userIDs, true)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for pub, uid := range ids {
		loadout := s.loadoutOf(entry, facts[uid], now, baseURL)
		if !loadout.Empty() {
			out[strconv.FormatInt(pub, 10)] = loadout
		}
	}
	return out, nil
}

// LookupByUserIDs 服务端内部按主键批量取（排行榜等在服务端把装扮并进行里时用）。键是主键。
func (s *CosmeticService) LookupByUserIDs(ctx context.Context, appID int64, userIDs []int64, baseURL string) map[int64]cosmetic.Loadout {
	out := make(map[int64]cosmetic.Loadout, len(userIDs))
	if s == nil || len(userIDs) == 0 {
		return out
	}
	entry, err := s.live(ctx, appID)
	if err != nil || !entry.settings.Enabled {
		return out
	}
	facts, err := s.pg.GetCosmeticUserFacts(ctx, appID, userIDs, true)
	if err != nil {
		s.log.Warn("lookup cosmetics failed", zap.Int64("appid", appID), zap.Error(err))
		return out
	}
	now := time.Now()
	for uid, f := range facts {
		if loadout := s.loadoutOf(entry, f, now, baseURL); !loadout.Empty() {
			out[uid] = loadout
		}
	}
	return out
}
