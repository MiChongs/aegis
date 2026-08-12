package service

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	admindomain "aegis/internal/domain/admin"
	authdomain "aegis/internal/domain/auth"
	avatardomain "aegis/internal/domain/avatar"
	storagedomain "aegis/internal/domain/storage"
	userdomain "aegis/internal/domain/user"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"

	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// AvatarService 头像服务。
//
// 一个人的头像可能来自三处，服务的职责就是把它们收敛成**一个永久地址**：
//
//	自定义上传  → 对象存储里的一组变体，落库 storage:// 引用
//	第三方带来  → 一条外链（OAuth 渠道给的）
//	什么都没有  → 服务端按标识确定性生成的默认头像
//
// 三种情况出网的都是 `/api/avatars/{token}`，因此客户端无需知道区别，
// 也不会因为用户换了头像来源而拿到一个变了的地址。详见 avatar_link.go 开头。
const (
	// avatarMaxUploadSize 上传体积的兜底上限（可由配置覆盖）。
	avatarMaxUploadSize = 10 << 20
	// avatarObjectReadLimit 取图时从对象存储读入内存的硬上限。
	// 与上传上限分开：它管的是**存量**对象，不能跟着"以后能传多大"一起变。
	avatarObjectReadLimit = 32 << 20
)

var phoneIdentifierPattern = regexp.MustCompile(`^[+]?\d[\d\s\-()]{5,}$`)

// AvatarUploadInput 上传入参。
type AvatarUploadInput struct {
	ConfigName    string
	FileName      string
	ContentType   string
	ContentLength int64
	Content       io.Reader
	UploadedBy    *int64
	UploaderType  string // "user" / "admin"
	// Options 裁剪等处理参数。
	Options avatardomain.UploadOptions
}

// AvatarUploadResult 上传结果。
//
// 除了地址还回传 blurhash / 主色 / 尺寸档：这些都是客户端拿不到就只能
// 自己再算一遍的东西，而客户端算出来的必然与服务端存的那份不一致。
type AvatarUploadResult struct {
	// AvatarURL 的 JSON 名仍是 `avatar` 且仍是**字符串** —— 老客户端读的就是它，
	// 把它改成对象会让所有已发布的 App 在上传成功后把一个 [object Object]
	// 当成图片地址去加载。新增的结构一律挂在别的键上。
	AvatarURL string              `json:"avatar"`
	Reference string              `json:"reference"`
	View      avatardomain.View   `json:"view"`
	Asset     *avatardomain.Asset `json:"asset,omitempty"`
	// Flattened 传上来的动图因为超限被拍平成静态图。
	Flattened bool `json:"flattened,omitempty"`
}

// AvatarIdentity 取默认头像时用得上的身份信息。
type AvatarIdentity struct {
	// Label 画首字母用的展示名。
	Label string
	// Identifiers 邮箱 / 手机号，仅在默认样式为 gravatar 时用于算哈希。
	Identifiers []string
}

// avatarTarget 一次解析的结论：这个人的头像现在到底指向什么。
type avatarTarget struct {
	Kind     string
	Asset    *avatardomain.Asset
	Ref      string
	ConfigID int64
	Key      string
	External string
	Version  string
	Label    string
	// Identifiers 供 gravatar 兜底
	Identifiers []string
}

type AvatarService struct {
	log       *zap.Logger
	storage   *StorageService
	user      *UserService
	admin     *AdminService
	pg        *pgrepo.Repository
	redis     *redislib.Client
	keyPrefix string
	signer    *avatarLinkSigner
	cfg       AvatarSettings
}

// AvatarSettings 是 config.AvatarConfig 在服务层的镜像。
//
// 服务层不 import internal/config（那会让每个服务都认识整个配置树），
// 由 bootstrap 拷一份进来 —— 与 PaymentReceipt 那套做法一致。
type AvatarSettings struct {
	DefaultStyle      string
	GravatarBaseURL   string
	GravatarHashAlgo  string
	Sizes             []int
	JPEGQuality       int
	KeepAnimated      bool
	MaxUploadBytes    int64
	UploadsPerHour    int
	StorageConfigName string
	CacheTTL          time.Duration
	PublicBaseURL     string
	SigningKey        string
}

func NewAvatarService(log *zap.Logger, storage *StorageService, user *UserService, admin *AdminService,
	pg *pgrepo.Repository, redis *redislib.Client, keyPrefix string, settings AvatarSettings) *AvatarService {
	if log == nil {
		log = zap.NewNop()
	}
	settings.DefaultStyle = normalizeAvatarDefaultStyle(settings.DefaultStyle)
	if settings.MaxUploadBytes <= 0 {
		settings.MaxUploadBytes = avatarMaxUploadSize
	}
	if settings.CacheTTL <= 0 {
		settings.CacheTTL = 6 * time.Hour
	}
	if strings.TrimSpace(settings.GravatarBaseURL) == "" {
		settings.GravatarBaseURL = "https://weavatar.com/avatar/"
	}
	return &AvatarService{
		log: log, storage: storage, user: user, admin: admin,
		pg: pg, redis: redis, keyPrefix: keyPrefix,
		signer: newAvatarLinkSigner(settings.SigningKey),
		cfg:    settings,
	}
}

// ════════════════════════════════════════════════════════════
//  解析：主体 → 永久地址
// ════════════════════════════════════════════════════════════

// ResolveUserAvatar 应用用户的头像地址。**恒不为空**（默认样式为 none 时除外）。
func (s *AvatarService) ResolveUserAvatar(ctx context.Context, baseURL string, appID int64, userID int64, rawAvatar string, identifiers ...string) string {
	return s.UserAvatarView(ctx, baseURL, appID, userID, rawAvatar, AvatarIdentity{Identifiers: identifiers}).URL
}

// ResolveAdminAvatar 管理员的头像地址。
func (s *AvatarService) ResolveAdminAvatar(ctx context.Context, baseURL string, adminID int64, rawAvatar string, identifiers ...string) string {
	return s.AdminAvatarView(ctx, baseURL, adminID, rawAvatar, AvatarIdentity{Identifiers: identifiers}).URL
}

// UserAvatarView 应用用户头像的完整视图。
func (s *AvatarService) UserAvatarView(ctx context.Context, baseURL string, appID int64, userID int64, rawAvatar string, identity AvatarIdentity) avatardomain.View {
	return s.view(ctx, baseURL, avatardomain.Owner{Type: avatardomain.OwnerUser, AppID: appID, ID: userID}, rawAvatar, identity)
}

// AdminAvatarView 管理员头像的完整视图。
func (s *AvatarService) AdminAvatarView(ctx context.Context, baseURL string, adminID int64, rawAvatar string, identity AvatarIdentity) avatardomain.View {
	return s.view(ctx, baseURL, avatardomain.Owner{Type: avatardomain.OwnerAdmin, ID: adminID}, rawAvatar, identity)
}

// view 把「库里那一列的值」翻译成出网视图。
//
// 注意它**不做存储访问**：既不签票据也不读对象。那些留到真的有人来取图时
// （AvatarService.Open）再做。这一条是本次重构的关键 ——
// 原来每读一次资料就要往 Redis 写一个票据，一个 20 行的用户列表
// 就是 20 次 Redis 写入，而其中大多数图根本不会被下载。
func (s *AvatarService) view(ctx context.Context, baseURL string, owner avatardomain.Owner, rawAvatar string, identity AvatarIdentity) avatardomain.View {
	if s == nil || !owner.Valid() {
		return avatardomain.View{}
	}
	raw := strings.TrimSpace(rawAvatar)

	// 外链原样给出去。它本来就是永久地址，绕一圈经过我们只会多一跳。
	if isExternalAvatarLink(raw) {
		return avatardomain.View{
			Kind:    avatardomain.KindExternal,
			URL:     raw,
			Version: avatarVersionOf(raw),
		}
	}

	token := s.signer.EncodeOwner(owner)
	if token == "" {
		return avatardomain.View{}
	}

	// 有自定义头像：版本取内容摘要，这样同一张图重复上传不会让缓存失效。
	if configID, key, ok := parseStorageReference(raw); ok {
		version := avatarVersionOf(raw)
		view := avatardomain.View{Kind: avatardomain.KindCustom, Sizes: s.sizes()}
		// 拿不到资产元数据（仓储未装配、或这是升级前上传的存量头像）时不影响出地址：
		// 少的只是 blurhash 与主色这类锦上添花的字段，头像本身照常可取。
		if asset, err := s.avatarAssetByKey(ctx, configID, key); err == nil && asset != nil {
			view.Blurhash = asset.Blurhash
			view.DominantColor = asset.DominantColor
			view.Animated = asset.Animated
			if asset.Checksum != "" {
				version = asset.Checksum[:min(len(asset.Checksum), 12)]
			}
		}
		view.Version = version
		view.URL = buildAvatarLink(s.publicBase(baseURL), token, version)
		return view
	}

	// 走到这里说明库里那一列不是引用也不是外链：要么是空的，要么是被
	// 写坏了（老版本交出去的临时票据地址被客户端原样 PUT 回来）。
	// 后一种情况资产表里还留着线索，自愈回去。
	if asset := s.healOwnerAvatar(ctx, owner, raw); asset != nil {
		version := asset.Checksum
		if len(version) > 12 {
			version = version[:12]
		}
		return avatardomain.View{
			Kind:          avatardomain.KindCustom,
			URL:           buildAvatarLink(s.publicBase(baseURL), token, version),
			Version:       version,
			Blurhash:      asset.Blurhash,
			DominantColor: asset.DominantColor,
			Animated:      asset.Animated,
			Sizes:         s.sizes(),
		}
	}

	// 默认头像。
	switch s.cfg.DefaultStyle {
	case AvatarStyleNone:
		return avatardomain.View{Kind: avatardomain.KindDefault}
	case AvatarStyleGravatar:
		for _, identifier := range identity.Identifiers {
			if link := s.BuildWeAvatarURL(identifier, s.cfg.GravatarHashAlgo); link != "" {
				return avatardomain.View{Kind: avatardomain.KindDefault, URL: link, Version: avatarVersionOf(link)}
			}
		}
	}
	version := avatarVersionOf(s.cfg.DefaultStyle + "\x00" + identity.Label + "\x00" + avatarOwnerPayload(owner))
	return avatardomain.View{
		Kind:    avatardomain.KindDefault,
		URL:     buildAvatarLink(s.publicBase(baseURL), token, version),
		Version: version,
		Sizes:   s.sizes(),
	}
}

// healOwnerAvatar 把被写坏（或丢失）的头像引用找回来。
//
// 只在两种输入下动手：空值、以及**明显是临时票据地址**的值。其它形态一律
// 不碰 —— 用户可能就是想把头像设成某个外链，自作主张改回去比丢了更糟。
//
// 两条线索按顺序试：
//
//	avatar_assets      —— 本次改动之后上传的，带全套元数据
//	storage_objects    —— 本次改动**之前**上传的，主体信息编码在对象键里
//
// 第二条是必须的：那张资产表这次才建，而丢头像的恰恰是升级前那批用户。
// 只认第一条的话，他们唯一的出路是重新上传一次 —— 而他们根本不知道自己
// 需要这么做（界面上只是没有头像，不像出了错）。
//
// 回写是尽力而为：失败只影响下次还得再自愈一次，绝不能让一次读资料失败。
func (s *AvatarService) healOwnerAvatar(ctx context.Context, owner avatardomain.Owner, raw string) *avatardomain.Asset {
	if s.pg == nil {
		return nil
	}
	if raw != "" && !isEphemeralStorageLink(raw) && !isAegisAvatarLink(raw) {
		return nil
	}
	asset, err := s.pg.GetActiveAvatarAsset(ctx, owner)
	if err != nil {
		return nil
	}
	if asset == nil {
		asset = s.adoptLegacyAvatarObject(ctx, owner)
	}
	if asset == nil {
		return nil
	}
	ref := buildStorageReference(asset.ConfigID, asset.BaseKey)
	if ref == "" || ref == raw {
		return asset
	}
	if err := s.writeAvatarReference(ctx, owner, ref); err != nil {
		s.log.Warn("头像引用自愈回写失败",
			zap.String("owner", avatarOwnerPayload(owner)), zap.Error(err))
		return asset
	}
	s.log.Info("已修复被覆盖的头像引用",
		zap.String("owner", avatarOwnerPayload(owner)),
		zap.String("broken", raw), zap.String("restored", ref))
	return asset
}

// adoptLegacyAvatarObject 把升级前上传的那张头像收编成一条资产记录。
//
// 线索是对象键：`avatars/apps/{appID}/users/{userID}/…` 与
// `avatars/admins/{adminID}/…` —— 这个命名在改动之前就是这样，
// 所以主体信息一直都在，只是从来没有地方去读它。
//
// 收编出来的记录没有变体、没有 blurhash（那些要重新解码原图才算得出来，
// 不值得在一次读资料的路径上做）。它的价值是让引用回到库里、
// 让下一次解析不用再扫一遍 storage_objects。用户下次换头像时自然升级成完整记录。
func (s *AvatarService) adoptLegacyAvatarObject(ctx context.Context, owner avatardomain.Owner) *avatardomain.Asset {
	configID, objectKey, err := s.pg.FindLatestAvatarObjectByKeyPart(ctx, legacyAvatarKeyPart(owner))
	if err != nil || configID <= 0 || objectKey == "" {
		return nil
	}
	saved, err := s.pg.ReplaceAvatarAsset(ctx, avatardomain.Asset{
		Owner:       owner,
		ConfigID:    configID,
		BaseKey:     objectKey,
		ContentType: avatarContentTypeByExt(strings.ToLower(path.Ext(objectKey))),
		Checksum:    avatarVersionOf(buildStorageReference(configID, objectKey)),
		Source:      avatardomain.SourceMigrated,
	})
	if err != nil {
		s.log.Warn("收编历史头像对象失败",
			zap.String("owner", avatarOwnerPayload(owner)), zap.String("key", objectKey), zap.Error(err))
		return nil
	}
	s.log.Info("已从存储索引里找回历史头像",
		zap.String("owner", avatarOwnerPayload(owner)), zap.String("key", objectKey))
	return saved
}

// legacyAvatarKeyPart 主体在对象键里的那一段。
func legacyAvatarKeyPart(owner avatardomain.Owner) string {
	if owner.Type == avatardomain.OwnerAdmin {
		return fmt.Sprintf("avatars/admins/%d/", owner.ID)
	}
	return fmt.Sprintf("avatars/apps/%d/users/%d/", owner.AppID, owner.ID)
}

// avatarContentTypeByExt 由扩展名推 content-type，认不出时按 JPEG 处理
// （历史头像只可能是当年那四种格式之一，而 JPEG 是其中最常见的）。
func avatarContentTypeByExt(ext string) string {
	switch ext {
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return "image/jpeg"
	}
}

// avatarAssetByKey 查资产元数据，仓储没装配时安静返回空。
//
// 单独包一层是因为解析链路上**没有仓储也必须能出地址** —— 地址由主体令牌
// 算出来，与资产表无关；资产表只提供 blurhash / 主色 / 精确版本这些额外信息。
// 让解析在这里 panic，等于让「导出 OpenAPI」这类零依赖的场景也跑不起来。
func (s *AvatarService) avatarAssetByKey(ctx context.Context, configID int64, key string) (*avatardomain.Asset, error) {
	if s.pg == nil {
		return nil, nil
	}
	return s.pg.GetAvatarAssetByKey(ctx, configID, key)
}

func (s *AvatarService) writeAvatarReference(ctx context.Context, owner avatardomain.Owner, ref string) error {
	if owner.Type == avatardomain.OwnerAdmin {
		return s.pg.SetAdminAvatar(ctx, owner.ID, ref)
	}
	return s.pg.SetUserProfileAvatar(ctx, owner.ID, ref)
}

// ════════════════════════════════════════════════════════════
//  取图：永久地址 → 字节
// ════════════════════════════════════════════════════════════

// AvatarImage 一次取图的结果。
type AvatarImage struct {
	Data        []byte
	ContentType string
	// ETag 已经带引号，可直接写进响应头。
	ETag string
	// Version 该主体头像的当前版本，供传输层判断请求里的 v 是否还新鲜。
	Version string
	// Redirect 非空时表示该跳转到外部地址（第三方头像 / gravatar）。
	Redirect string
	// NotModified 客户端手上那份已经是最新的，Data 为空。
	NotModified bool
}

// OpenAvatar 按主体令牌取图。这是永久地址背后的实现。
//
// 它**不认**地址里的 v：那个参数只用来破客户端与 CDN 的缓存，
// 服务端永远返回这个人当前的头像。正因如此，两年前发出去的那份副本
// 今天点开仍然有效 —— 这就是"链接不会消失"的全部含义。
// ifNoneMatch 是客户端手上那份的 ETag。**在读对象之前**比对：头像是缓存命中率
// 最高的资源之一，把 200KB 从对象存储拉回来只为了发现"客户端已经有了"，
// 是这条链路上最容易避免的浪费。
func (s *AvatarService) OpenAvatar(ctx context.Context, token string, size int, ifNoneMatch string) (*AvatarImage, error) {
	owner, ok := s.signer.DecodeOwner(token)
	if !ok {
		return nil, apperrors.New(40481, http.StatusNotFound, "头像不存在")
	}
	target, err := s.resolveTarget(ctx, owner)
	if err != nil {
		return nil, err
	}
	size = clampAvatarRenderSize(size)

	if target.Kind == avatardomain.KindExternal {
		return &AvatarImage{Redirect: target.External, Version: target.Version}, nil
	}
	if etag := avatarETag(target.Version, size); avatarETagMatches(ifNoneMatch, etag) {
		return &AvatarImage{ETag: etag, Version: target.Version, NotModified: true}, nil
	}
	if target.Kind == avatardomain.KindCustom {
		image, err := s.openStoredAvatar(ctx, target, size)
		if err == nil {
			return image, nil
		}
		// 对象取不到（存储配置被删、桶里的文件被清了）时不能 404：
		// 界面上会变成一个碎图标，而用户什么都没做错。退回默认头像，
		// 并把真实原因留在日志里。
		s.log.Warn("头像对象读取失败，回落默认头像",
			zap.String("owner", avatarOwnerPayload(owner)),
			zap.Int64("config_id", target.ConfigID), zap.String("key", target.Key), zap.Error(err))
	}
	return s.renderDefault(ctx, owner, target, size)
}

func (s *AvatarService) openStoredAvatar(ctx context.Context, target avatarTarget, size int) (*AvatarImage, error) {
	configID, key, contentType := target.ConfigID, target.Key, ""
	if target.Asset != nil {
		if variant := target.Asset.VariantFor(size); variant != nil && variant.Key != "" {
			key, contentType = variant.Key, variant.ContentType
		} else {
			contentType = target.Asset.ContentType
		}
		// 动图只有原图那一份是动的，请求任何尺寸都给它 ——
		// 给一个静态变体等于悄悄把用户的动图换成了静态图。
		if target.Asset.Animated {
			key, contentType = target.Asset.BaseKey, target.Asset.ContentType
		}
	}
	cacheKey := s.imageCacheKey(configID, key)
	if data, ct := s.readCachedAvatar(ctx, cacheKey); len(data) > 0 {
		return s.buildImage(data, firstNonEmptyAvatarString(ct, contentType), target.Version, size), nil
	}
	// 读取上限刻意**不**用 MaxUploadBytes：那是「以后能传多大」，
	// 而这里要读的是「当年已经传上去的那张」。把上限调小之后，
	// 存量里超过新上限的头像会集体读不出来、静默退化成默认头像 ——
	// 一次配置调整不该让已经存在的头像消失，那正是这次要根治的那类问题。
	data, ct, err := s.storage.ReadObjectBytes(ctx, configID, key, avatarObjectReadLimit)
	if err != nil {
		return nil, err
	}
	s.writeCachedAvatar(ctx, cacheKey, data, firstNonEmptyAvatarString(ct, contentType))
	return s.buildImage(data, firstNonEmptyAvatarString(ct, contentType), target.Version, size), nil
}

func (s *AvatarService) renderDefault(ctx context.Context, owner avatardomain.Owner, target avatarTarget, size int) (*AvatarImage, error) {
	if s.cfg.DefaultStyle == AvatarStyleGravatar {
		for _, identifier := range target.Identifiers {
			if link := s.BuildWeAvatarURL(identifier, s.cfg.GravatarHashAlgo); link != "" {
				return &AvatarImage{Redirect: link, Version: target.Version}, nil
			}
		}
	}
	if s.cfg.DefaultStyle == AvatarStyleNone {
		return nil, apperrors.New(40481, http.StatusNotFound, "该用户未设置头像")
	}
	seed := avatarOwnerPayload(owner)
	cacheKey := s.defaultCacheKey(seed, target.Label, size)
	if data, ct := s.readCachedAvatar(ctx, cacheKey); len(data) > 0 {
		return s.buildImage(data, ct, target.Version, size), nil
	}
	data, contentType, err := renderDefaultAvatar(avatarIdentityRequest{
		Seed:  seed,
		Label: target.Label,
		Style: s.cfg.DefaultStyle,
		Size:  size,
	})
	if err != nil {
		return nil, apperrors.New(50085, http.StatusInternalServerError, "默认头像生成失败")
	}
	s.writeCachedAvatar(ctx, cacheKey, data, contentType)
	return s.buildImage(data, contentType, target.Version, size), nil
}

func (s *AvatarService) buildImage(data []byte, contentType string, version string, size int) *AvatarImage {
	if contentType == "" {
		contentType = "image/jpeg"
	}
	return &AvatarImage{
		Data:        data,
		ContentType: contentType,
		ETag:        avatarETag(version, size),
		Version:     version,
	}
}

// avatarETag 版本 + 尺寸。
//
// 尺寸必须进去：同一个版本的 64 与 512 是两份不同的字节，共用一个 ETag
// 会让中间缓存把小图当大图返回 —— 表现是「头像有时候特别糊」，且只在
// 经过 CDN 的部署里发作。
func avatarETag(version string, size int) string {
	return fmt.Sprintf(`"%s-%d"`, version, size)
}

// avatarETagMatches 按 RFC 9110 的 If-None-Match 语义比对。
//
// 请求头可以是逗号分隔的一串，也可以是 `*`，还可能带 `W/` 弱校验前缀 ——
// 直接做字符串相等会让绝大多数真实的条件请求判不中，于是每次刷新都重传整张图。
func avatarETagMatches(ifNoneMatch string, etag string) bool {
	ifNoneMatch = strings.TrimSpace(ifNoneMatch)
	if ifNoneMatch == "" || etag == "" {
		return false
	}
	if ifNoneMatch == "*" {
		return true
	}
	for _, candidate := range strings.Split(ifNoneMatch, ",") {
		candidate = strings.TrimSpace(candidate)
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == etag {
			return true
		}
	}
	return false
}

// resolveTarget 主体 → 当前头像指向。带一层短缓存。
//
// 缓存 60 秒：头像地址是页面上出现次数最多的资源之一（一屏列表 20~50 个），
// 每个都查两次库是不划算的。60 秒也短到「换了头像立刻看见」仍然成立 ——
// 何况换头像会同时换 v，客户端拿到的是新地址，本来就不会命中旧缓存。
func (s *AvatarService) resolveTarget(ctx context.Context, owner avatardomain.Owner) (avatarTarget, error) {
	if s.pg == nil {
		return avatarTarget{}, apperrors.New(50380, http.StatusServiceUnavailable, "头像服务暂不可用")
	}
	var (
		raw         string
		label       string
		identifiers []string
	)
	if owner.Type == avatardomain.OwnerAdmin {
		profile, err := s.pg.GetAdminAccessByID(ctx, owner.ID)
		if err != nil {
			return avatarTarget{}, err
		}
		if profile == nil {
			return avatarTarget{}, apperrors.New(40481, http.StatusNotFound, "头像不存在")
		}
		raw = strings.TrimSpace(profile.Account.Avatar)
		label = firstNonEmptyAvatarString(profile.Account.DisplayName, profile.Account.Account, profile.Account.Email)
		identifiers = []string{profile.Account.Email, profile.Account.Phone}
	} else {
		user, err := s.pg.GetUserByID(ctx, owner.ID)
		if err != nil {
			return avatarTarget{}, err
		}
		// 跨应用校验：令牌里带着 appid，与用户真实归属对不上就当作不存在。
		// 少了这一条，A 应用的管理员拿 B 应用用户的 ID 拼一个令牌就能取到图。
		if user == nil || user.AppID != owner.AppID {
			return avatarTarget{}, apperrors.New(40481, http.StatusNotFound, "头像不存在")
		}
		profile, err := s.pg.GetUserProfileByUserID(ctx, owner.ID)
		if err != nil {
			return avatarTarget{}, err
		}
		if profile != nil {
			raw = strings.TrimSpace(profile.Avatar)
			label = firstNonEmptyAvatarString(profile.Nickname, user.Account, profile.Email)
			identifiers = []string{profile.Email, profile.Phone}
		} else {
			label = user.Account
		}
	}

	target := avatarTarget{Label: label, Identifiers: identifiers, Ref: raw}
	if isExternalAvatarLink(raw) {
		target.Kind = avatardomain.KindExternal
		target.External = raw
		target.Version = avatarVersionOf(raw)
		return target, nil
	}
	if configID, key, ok := parseStorageReference(raw); ok {
		target.Kind = avatardomain.KindCustom
		target.ConfigID, target.Key = configID, key
		target.Version = avatarVersionOf(raw)
		if asset, err := s.avatarAssetByKey(ctx, configID, key); err == nil && asset != nil {
			target.Asset = asset
			if asset.Checksum != "" {
				target.Version = asset.Checksum[:min(len(asset.Checksum), 12)]
			}
		}
		return target, nil
	}
	if asset := s.healOwnerAvatar(ctx, owner, raw); asset != nil {
		target.Kind = avatardomain.KindCustom
		target.Asset = asset
		target.ConfigID, target.Key = asset.ConfigID, asset.BaseKey
		target.Version = asset.Checksum[:min(len(asset.Checksum), 12)]
		return target, nil
	}
	target.Kind = avatardomain.KindDefault
	target.Version = avatarVersionOf(s.cfg.DefaultStyle + "\x00" + label + "\x00" + avatarOwnerPayload(owner))
	return target, nil
}

// ════════════════════════════════════════════════════════════
//  上传与移除
// ════════════════════════════════════════════════════════════

// UploadUserAvatar 应用用户上传头像。
func (s *AvatarService) UploadUserAvatar(ctx context.Context, baseURL string, session *authdomain.Session, input AvatarUploadInput) (*userdomain.Profile, *AvatarUploadResult, error) {
	if session == nil {
		return nil, nil, apperrors.New(40100, http.StatusUnauthorized, "未认证")
	}
	owner := avatardomain.Owner{Type: avatardomain.OwnerUser, AppID: session.AppID, ID: session.UserID}
	if err := s.ensureUploadQuota(ctx, owner); err != nil {
		return nil, nil, err
	}
	current, err := s.user.GetProfile(ctx, session)
	if err != nil {
		return nil, nil, err
	}
	asset, result, err := s.store(ctx, owner, session.AppID,
		avatarObjectKeyForUser(session.AppID, session.UserID), input)
	if err != nil {
		return nil, nil, err
	}
	ref := buildStorageReference(asset.ConfigID, asset.BaseKey)
	if err := s.pg.SetUserProfileAvatar(ctx, session.UserID, ref); err != nil {
		return nil, nil, err
	}
	s.user.InvalidateProfileCache(ctx, session.AppID, session.UserID)

	profile := current
	profile.Avatar = ref
	identity := AvatarIdentity{Label: firstNonEmptyAvatarString(profile.Nickname, session.Account),
		Identifiers: []string{profile.Email, session.Account}}
	view := s.view(ctx, baseURL, owner, ref, identity)
	profile.Avatar = view.URL

	result.View = view
	result.AvatarURL = view.URL
	result.Reference = ref
	result.Asset = asset
	return profile, result, nil
}

// UploadAdminAvatar 管理员上传头像。
func (s *AvatarService) UploadAdminAvatar(ctx context.Context, baseURL string, access *admindomain.AccessContext, input AvatarUploadInput) (*admindomain.Profile, *AvatarUploadResult, error) {
	if access == nil {
		return nil, nil, apperrors.New(40110, http.StatusUnauthorized, "管理员未认证")
	}
	owner := avatardomain.Owner{Type: avatardomain.OwnerAdmin, ID: access.AdminID}
	if err := s.ensureUploadQuota(ctx, owner); err != nil {
		return nil, nil, err
	}
	// 管理员头像走平台级存储（appID = 0）：挂到某个应用下面的话，
	// 那个应用被归档时管理员的头像会跟着一起没了。
	asset, result, err := s.store(ctx, owner, 0, avatarObjectKeyForAdmin(access.AdminID), input)
	if err != nil {
		return nil, nil, err
	}
	ref := buildStorageReference(asset.ConfigID, asset.BaseKey)
	if err := s.pg.SetAdminAvatar(ctx, access.AdminID, ref); err != nil {
		return nil, nil, err
	}
	profile, err := s.admin.GetProfile(ctx, access.AdminID)
	if err != nil {
		return nil, nil, err
	}
	view := s.view(ctx, baseURL, owner, ref, AvatarIdentity{
		Label:       firstNonEmptyAvatarString(profile.Account.DisplayName, profile.Account.Account),
		Identifiers: []string{profile.Account.Email, profile.Account.Account},
	})
	profile.Account.Avatar = view.URL

	result.View = view
	result.AvatarURL = view.URL
	result.Reference = ref
	result.Asset = asset
	return profile, result, nil
}

// store 处理图像 + 上传全部变体 + 落资产表。
func (s *AvatarService) store(ctx context.Context, owner avatardomain.Owner, appID int64, baseKeyStem string, input AvatarUploadInput) (*avatardomain.Asset, *AvatarUploadResult, error) {
	raw, err := readAvatarUpload(input.Content, s.cfg.MaxUploadBytes)
	if err != nil {
		return nil, nil, err
	}
	processed, err := processAvatarImage(raw, input.FileName, input.Options, avatarEncodeOptions{
		JPEGQuality:  s.cfg.JPEGQuality,
		KeepAnimated: s.cfg.KeepAnimated,
		Sizes:        s.cfg.Sizes,
	})
	if err != nil {
		return nil, nil, err
	}

	configName := firstNonEmptyAvatarString(strings.TrimSpace(input.ConfigName), s.cfg.StorageConfigName)
	baseKey := baseKeyStem + processed.Base.Ext
	stored, err := s.putObject(ctx, appID, configName, baseKey, processed.Base, input)
	if err != nil {
		return nil, nil, err
	}

	variants := make([]avatardomain.Variant, 0, len(processed.Variants))
	for _, item := range processed.Variants {
		key := avatarVariantKey(stored.Key, item.Size, item.Ext)
		// 变体传失败不让整次上传失败：主图已经在了，缺的那一档
		// 会在取图时自动回落到更大的一档，用户完全无感。
		saved, err := s.putObject(ctx, appID, configName, key, item, input)
		if err != nil {
			s.log.Warn("头像变体上传失败", zap.Int("size", item.Size), zap.String("key", key), zap.Error(err))
			continue
		}
		variants = append(variants, avatardomain.Variant{
			Size: item.Size, Key: saved.Key, ContentType: item.ContentType, Bytes: int64(len(item.Data)),
		})
	}

	asset := avatardomain.Asset{
		Owner:         owner,
		ConfigID:      stored.ConfigID,
		BaseKey:       stored.Key,
		ContentType:   processed.Base.ContentType,
		Width:         processed.Width,
		Height:        processed.Height,
		Bytes:         int64(len(processed.Base.Data)),
		Checksum:      processed.Checksum,
		Blurhash:      processed.Blurhash,
		DominantColor: processed.DominantColor,
		Animated:      processed.Animated,
		Variants:      variants,
		FileName:      strings.TrimSpace(input.FileName),
		Source:        avatardomain.SourceUpload,
	}
	saved, err := s.pg.ReplaceAvatarAsset(ctx, asset)
	if err != nil {
		return nil, nil, err
	}
	return saved, &AvatarUploadResult{Flattened: processed.Flattened}, nil
}

func (s *AvatarService) putObject(ctx context.Context, appID int64, configName string, key string, item renderedAvatarImage, input AvatarUploadInput) (*storagedomain.StoredObject, error) {
	uploaderType := input.UploaderType
	if uploaderType == "" {
		uploaderType = "user"
	}
	stored, err := s.storage.UploadForApp(ctx, appID, storagedomain.UploadInput{
		AppID:       appID,
		ConfigName:  configName,
		ObjectKey:   key,
		FileName:    path.Base(key),
		ContentType: item.ContentType,
		// 对象键里带着内容摘要之外的时间戳，同一份字节不会被覆盖写，
		// 因此可以放心让 CDN 长期缓存**对象**本身。
		CacheControl:  "public, max-age=31536000, immutable",
		ContentLength: int64(len(item.Data)),
		Metadata:      map[string]string{"module": "avatar"},
		Content:       bytes.NewReader(item.Data),
		UploadedBy:    input.UploadedBy,
		UploaderType:  uploaderType,
	})
	if err != nil {
		return nil, err
	}
	return stored, nil
}

// RemoveUserAvatar 移除应用用户的自定义头像，回到默认头像。
//
// 之前没有这个入口：更新资料时空字符串的语义是"不修改"，
// 于是**一旦传过头像就再也回不去**了。
func (s *AvatarService) RemoveUserAvatar(ctx context.Context, baseURL string, session *authdomain.Session) (*userdomain.Profile, *avatardomain.View, error) {
	if session == nil {
		return nil, nil, apperrors.New(40100, http.StatusUnauthorized, "未认证")
	}
	owner := avatardomain.Owner{Type: avatardomain.OwnerUser, AppID: session.AppID, ID: session.UserID}
	if err := s.pg.ClearActiveAvatarAsset(ctx, owner); err != nil {
		return nil, nil, err
	}
	if err := s.pg.SetUserProfileAvatar(ctx, session.UserID, ""); err != nil {
		return nil, nil, err
	}
	s.user.InvalidateProfileCache(ctx, session.AppID, session.UserID)
	profile, err := s.user.GetProfile(ctx, session)
	if err != nil {
		return nil, nil, err
	}
	view := s.view(ctx, baseURL, owner, "", AvatarIdentity{
		Label:       firstNonEmptyAvatarString(profile.Nickname, session.Account),
		Identifiers: []string{profile.Email, session.Account},
	})
	profile.Avatar = view.URL
	return profile, &view, nil
}

// RemoveAdminAvatar 移除管理员的自定义头像。
func (s *AvatarService) RemoveAdminAvatar(ctx context.Context, baseURL string, adminID int64) (*admindomain.Profile, *avatardomain.View, error) {
	owner := avatardomain.Owner{Type: avatardomain.OwnerAdmin, ID: adminID}
	if err := s.pg.ClearActiveAvatarAsset(ctx, owner); err != nil {
		return nil, nil, err
	}
	if err := s.pg.SetAdminAvatar(ctx, adminID, ""); err != nil {
		return nil, nil, err
	}
	profile, err := s.admin.GetProfile(ctx, adminID)
	if err != nil {
		return nil, nil, err
	}
	view := s.view(ctx, baseURL, owner, "", AvatarIdentity{
		Label:       firstNonEmptyAvatarString(profile.Account.DisplayName, profile.Account.Account),
		Identifiers: []string{profile.Account.Email, profile.Account.Account},
	})
	profile.Account.Avatar = view.URL
	return profile, &view, nil
}

// ListUserAvatarHistory 用户的头像历史（含当前），用于「换回上一张」。
func (s *AvatarService) ListUserAvatarHistory(ctx context.Context, baseURL string, session *authdomain.Session, limit int) ([]avatardomain.Asset, error) {
	if session == nil {
		return nil, apperrors.New(40100, http.StatusUnauthorized, "未认证")
	}
	_ = baseURL
	return s.pg.ListAvatarAssetHistory(ctx,
		avatardomain.Owner{Type: avatardomain.OwnerUser, AppID: session.AppID, ID: session.UserID}, limit)
}

// RestoreUserAvatar 把历史里的某一张重新设为当前头像。
func (s *AvatarService) RestoreUserAvatar(ctx context.Context, baseURL string, session *authdomain.Session, assetID int64) (*userdomain.Profile, *avatardomain.View, error) {
	if session == nil {
		return nil, nil, apperrors.New(40100, http.StatusUnauthorized, "未认证")
	}
	owner := avatardomain.Owner{Type: avatardomain.OwnerUser, AppID: session.AppID, ID: session.UserID}
	existing, err := s.pg.GetAvatarAssetByID(ctx, owner, assetID)
	if err != nil {
		return nil, nil, err
	}
	if existing == nil {
		return nil, nil, apperrors.New(40481, http.StatusNotFound, "该头像记录不存在")
	}
	asset, err := s.pg.ActivateAvatarAsset(ctx, owner, assetID)
	if err != nil || asset == nil {
		if err == nil {
			err = apperrors.New(40481, http.StatusNotFound, "该头像记录不存在")
		}
		return nil, nil, err
	}
	ref := buildStorageReference(asset.ConfigID, asset.BaseKey)
	if err := s.pg.SetUserProfileAvatar(ctx, session.UserID, ref); err != nil {
		return nil, nil, err
	}
	s.user.InvalidateProfileCache(ctx, session.AppID, session.UserID)
	profile, err := s.user.GetProfile(ctx, session)
	if err != nil {
		return nil, nil, err
	}
	view := s.view(ctx, baseURL, owner, ref, AvatarIdentity{
		Label:       firstNonEmptyAvatarString(profile.Nickname, session.Account),
		Identifiers: []string{profile.Email, session.Account},
	})
	profile.Avatar = view.URL
	return profile, &view, nil
}

// ensureUploadQuota 每主体每小时的换头像次数闸门。
//
// 滑动窗口做不了也不必要 —— 这里防的是脚本刷存储，不是精确计费。
// Redis 不可用时**放行**：头像上传不该因为缓存故障而不可用。
func (s *AvatarService) ensureUploadQuota(ctx context.Context, owner avatardomain.Owner) error {
	if s.redis == nil || s.cfg.UploadsPerHour <= 0 {
		return nil
	}
	key := fmt.Sprintf("%s:avatar:quota:%s", s.keyPrefix, avatarOwnerPayload(owner))
	count, err := s.redis.Incr(ctx, key).Result()
	if err != nil {
		return nil
	}
	if count == 1 {
		_ = s.redis.Expire(ctx, key, time.Hour).Err()
	}
	if count > int64(s.cfg.UploadsPerHour) {
		return apperrors.New(42901, http.StatusTooManyRequests,
			fmt.Sprintf("头像更换过于频繁，每小时最多 %d 次，请稍后再试", s.cfg.UploadsPerHour))
	}
	return nil
}

// ════════════════════════════════════════════════════════════
//  Gravatar / WeAvatar 兼容
// ════════════════════════════════════════════════════════════

// BuildWeAvatarURL 由邮箱 / 手机号算出第三方头像地址。
// 仅在默认样式配成 gravatar 时才会被用到。
func (s *AvatarService) BuildWeAvatarURL(identifier string, algo string) string {
	normalized := normalizeAvatarIdentifier(identifier)
	if normalized == "" {
		return ""
	}
	return s.BuildWeAvatarURLByHash(hashAvatarIdentifier(normalized, algo))
}

func (s *AvatarService) BuildWeAvatarURLByHash(hash string) string {
	hash = sanitizeAvatarHash(hash)
	if hash == "" {
		return ""
	}
	base := s.cfg.GravatarBaseURL
	if strings.TrimSpace(base) == "" {
		base = "https://weavatar.com/avatar/"
	}
	return strings.TrimRight(base, "/") + "/" + hash
}

// ════════════════════════════════════════════════════════════
//  内部工具
// ════════════════════════════════════════════════════════════

func (s *AvatarService) sizes() []int { return normalizeAvatarSizes(s.cfg.Sizes) }

func (s *AvatarService) publicBase(requestBase string) string {
	if base := strings.TrimSpace(s.cfg.PublicBaseURL); base != "" {
		return strings.TrimRight(base, "/")
	}
	return strings.TrimRight(strings.TrimSpace(requestBase), "/")
}

func (s *AvatarService) imageCacheKey(configID int64, objectKey string) string {
	sum := sha256.Sum256([]byte(objectKey))
	return fmt.Sprintf("%s:avatar:obj:%d:%s", s.keyPrefix, configID, hex.EncodeToString(sum[:12]))
}

func (s *AvatarService) defaultCacheKey(seed string, label string, size int) string {
	sum := sha256.Sum256([]byte(seed + "\x00" + label + "\x00" + s.cfg.DefaultStyle))
	return fmt.Sprintf("%s:avatar:def:%s:%d", s.keyPrefix, hex.EncodeToString(sum[:12]), size)
}

// 缓存帧格式与缩略图那边一致：`contentType\n` + 原始字节。
// 不用 JSON 是因为 base64 会让每张图白白多占三分之一内存。
func (s *AvatarService) readCachedAvatar(ctx context.Context, key string) ([]byte, string) {
	if s.redis == nil {
		return nil, ""
	}
	raw, err := s.redis.Get(ctx, key).Bytes()
	if err != nil || len(raw) == 0 {
		return nil, ""
	}
	// 在字节上找分隔符，不要先 string(raw) —— 那会把一张 200KB 的图整个复制一遍，
	// 而这条路径是头像命中缓存时的热路径。
	idx := bytes.IndexByte(raw, '\n')
	if idx <= 0 {
		return nil, ""
	}
	return raw[idx+1:], string(raw[:idx])
}

func (s *AvatarService) writeCachedAvatar(ctx context.Context, key string, data []byte, contentType string) {
	if s.redis == nil || len(data) == 0 {
		return
	}
	payload := append([]byte(contentType+"\n"), data...)
	if err := s.redis.Set(ctx, key, payload, s.cfg.CacheTTL).Err(); err != nil {
		s.log.Debug("头像缓存写入失败", zap.String("key", key), zap.Error(err))
	}
}

// clampAvatarRenderSize 把请求的尺寸收敛进允许范围。
// 不收敛的话 `?s=100000` 会让默认头像的渲染申请 40GB。
func clampAvatarRenderSize(size int) int {
	if size <= 0 {
		return avatardomain.DefaultRenderSize
	}
	if size < 16 {
		return 16
	}
	if size > avatardomain.MaxRenderSize {
		return avatardomain.MaxRenderSize
	}
	return size
}

// isExternalAvatarLink 是不是一条**别人家的**永久地址。
// 我们自己发出去的 /api/avatars/ 不算 —— 那是展示形态，不是头像来源，
// 把它当外链会绕出一个自己指向自己的死循环。
func isExternalAvatarLink(raw string) bool {
	if raw == "" || isAegisAvatarLink(raw) || isEphemeralStorageLink(raw) {
		return false
	}
	lower := strings.ToLower(raw)
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func firstNonEmptyAvatarString(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func normalizeAvatarIdentifier(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	if strings.Contains(value, "@") {
		return value
	}
	if !phoneIdentifierPattern.MatchString(value) {
		return ""
	}
	replacer := strings.NewReplacer(" ", "", "-", "", "(", "", ")", "")
	value = replacer.Replace(value)
	if len(strings.TrimLeft(value, "+")) < 6 {
		return ""
	}
	return value
}

func hashAvatarIdentifier(identifier string, algo string) string {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return ""
	}
	switch strings.ToLower(strings.TrimSpace(algo)) {
	case "md5":
		sum := md5.Sum([]byte(identifier))
		return hex.EncodeToString(sum[:])
	default:
		sum := sha256.Sum256([]byte(identifier))
		return hex.EncodeToString(sum[:])
	}
}

func sanitizeAvatarHash(hash string) string {
	hash = strings.ToLower(strings.TrimSpace(hash))
	if len(hash) != 32 && len(hash) != 64 {
		return ""
	}
	for _, ch := range hash {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return ""
		}
	}
	return hash
}

// 对象键里带时间戳而不是只带用户 ID：同一个人换头像会得到新的键，
// 于是旧对象可以继续被历史记录引用（「换回上一张」靠的就是它），
// 也不会出现"新头像传上去了但 CDN 还缓存着同一个键的旧内容"。
func avatarObjectKeyForUser(appID int64, userID int64) string {
	now := time.Now().UTC()
	return path.Join("avatars", "apps", fmt.Sprintf("%d", appID), "users", fmt.Sprintf("%d", userID),
		now.Format("2006"), now.Format("01"), now.Format("02150405")+"_avatar")
}

func avatarObjectKeyForAdmin(adminID int64) string {
	now := time.Now().UTC()
	return path.Join("avatars", "admins", fmt.Sprintf("%d", adminID),
		now.Format("2006"), now.Format("01"), now.Format("02150405")+"_avatar")
}

func joinURL(baseURL string, relative string) string {
	relative = strings.TrimSpace(relative)
	if relative == "" {
		return ""
	}
	if strings.HasPrefix(relative, "http://") || strings.HasPrefix(relative, "https://") {
		return relative
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		if strings.HasPrefix(relative, "/") {
			return relative
		}
		return "/" + relative
	}
	if strings.HasPrefix(relative, "/") {
		return baseURL + relative
	}
	return baseURL + "/" + relative
}
