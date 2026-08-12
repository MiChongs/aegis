package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	appdomain "aegis/internal/domain/app"
	storagedomain "aegis/internal/domain/storage"
	apperrors "aegis/pkg/errors"

	"github.com/inbucket/html2text"
	"github.com/microcosm-cc/bluemonday"
	"go.uber.org/zap"
)

// 应用级内容中心：Banner（投放位）与公告。
//
// 三条结构性约束，改这个文件前先读完：
//
//  1. **富文本一律在写入时净化，不在读取时。** 公告正文来自控制台的富文本编辑器，
//     最终会被注入到控制台的预览、应用客户端的 WebView 和公告邮件里。
//     净化放在读取端意味着每一个消费方都要自己记得做一次，漏一个就是一次存储型 XSS；
//     放在写入端只有这一个入口。用 bluemonday 而不是自己写白名单：
//     标签闭合、属性大小写、URL 协议、实体编码这些坑它全踩过了。
//
//  2. **摘要由服务端提取并落库。** 列表页、推送、客户端通知栏要的都是纯文本一段。
//     让每一端各自去解析富文本，既慢又必然解析出不同结果（有的连 `<strong>`
//     的标签名一起显示出来）。
//
//  3. **图片走对象存储，落库的是 `storage://` 引用而不是可访问 URL。**
//     可访问 URL 是带票据、会过期的，存进去过两天就是死链。读取时现解析。

const (
	contentImageMaxUploadSize = 10 << 20 // 10 MB
	contentImageProxyTTL      = 30 * time.Minute
	noticeSummaryMaxRunes     = 160
	contentCacheTTL           = 2 * time.Minute
)

// contentSanitizer 富文本净化策略。构造一次全局复用 —— bluemonday 的 Policy
// 构造完之后是只读的，可以并发使用；每次请求新建一份会把正则全部重编译一遍。
var contentSanitizer = newContentSanitizer()

func newContentSanitizer() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	// tiptap StarterKit + Underline 会产出这几个 UGCPolicy 默认不放行的标签。
	// 不放行的表现是「编辑器里有下划线、存完就没了」，而且没有任何提示。
	p.AllowElements("u", "s", "del", "ins", "mark", "small", "figure", "figcaption")
	// 对齐、高亮这类排版信息在 tiptap 里是 class，丢了排版就全塌了。
	// 只放行 class，不放行 style —— 后者是 XSS 的老入口（expression / url(javascript:)）。
	p.AllowAttrs("class").Matching(bluemonday.SpaceSeparatedTokens).Globally()
	p.AllowAttrs("start", "type").OnElements("ol")
	// 公告正文里的外链是给用户点的，加 nofollow 与 target=_blank：
	// 前者防 SEO 注入，后者避免点一下就把宿主 WebView 导航走、退不回来。
	p.RequireNoFollowOnLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)
	return p
}

// sanitizeRichText 净化富文本，并判断净化后是否还剩内容。
func sanitizeRichText(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	return strings.TrimSpace(contentSanitizer.Sanitize(trimmed))
}

// richTextIsEmpty 判断一段富文本是不是「看起来有东西、其实一个字都没有」。
// 富文本编辑器在用户清空之后通常留下 `<p></p>`，长度不为 0 但内容为空 ——
// 只判空串会让这种公告存进去，客户端上就是一条点开什么都没有的公告。
func richTextIsEmpty(html string) bool {
	return strings.TrimSpace(plainTextFromHTML(html)) == ""
}

// plainTextFromHTML 提取纯文本。OmitLinks=true：摘要里带一串 URL 会把有效信息挤掉。
func plainTextFromHTML(source string) string {
	if strings.TrimSpace(source) == "" {
		return ""
	}
	text, err := html2text.FromString(source, html2text.Options{OmitLinks: true})
	if err != nil {
		return strings.TrimSpace(source)
	}
	return strings.TrimSpace(text)
}

// deriveNoticeSummary 从正文提取列表用摘要，压掉多余空白并按字符数截断。
// 按 rune 截断而不是 byte：按字节切会把一个汉字劈成两半，尾巴上出现乱码方块。
func deriveNoticeSummary(html string) string {
	text := strings.Join(strings.Fields(plainTextFromHTML(html)), " ")
	runes := []rune(text)
	if len(runes) <= noticeSummaryMaxRunes {
		return text
	}
	return strings.TrimSpace(string(runes[:noticeSummaryMaxRunes])) + "…"
}

/* ───────────────────── 展示端（免登录） ───────────────────── */

// GetBanners 展示端：当前生效的 Banner，带 2 分钟缓存。
func (s *AppService) GetBanners(ctx context.Context, appID int64) ([]appdomain.Banner, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	if s.sessions != nil {
		cached, err := s.sessions.GetBanners(ctx, appID)
		if err != nil {
			s.log.Warn("load banners cache failed", zap.Int64("appid", appID), zap.Error(err))
		} else if cached != nil {
			return cached, nil
		}
	}
	items, err := s.pg.ListActiveBanners(ctx, appID, time.Now().In(s.location))
	if err != nil {
		return nil, err
	}
	// 展示端拿到的必须是能直接 <img> 的地址：storage:// 引用客户端解析不了。
	s.ResolveBannerImages(ctx, items)
	if s.sessions != nil {
		if err := s.sessions.SetBanners(ctx, appID, items, contentCacheTTL); err != nil {
			s.log.Warn("cache banners failed", zap.Int64("appid", appID), zap.Error(err))
		}
	}
	return items, nil
}

// GetNotices 展示端：已发布且在投放窗口内的公告，置顶优先，带 2 分钟缓存。
func (s *AppService) GetNotices(ctx context.Context, appID int64) ([]appdomain.Notice, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	if s.sessions != nil {
		cached, err := s.sessions.GetNotices(ctx, appID)
		if err != nil {
			s.log.Warn("load notices cache failed", zap.Int64("appid", appID), zap.Error(err))
		} else if cached != nil {
			return cached, nil
		}
	}
	items, err := s.pg.ListActiveNotices(ctx, appID, time.Now().In(s.location))
	if err != nil {
		return nil, err
	}
	if s.sessions != nil {
		if err := s.sessions.SetNotices(ctx, appID, items, contentCacheTTL); err != nil {
			s.log.Warn("cache notices failed", zap.Int64("appid", appID), zap.Error(err))
		}
	}
	return items, nil
}

// TrackBannerClick 点击上报。
//
// click_count 这一列从建表起就存在，却从来没有任何代码写过它 ——
// 于是控制台上「点击 0」既可能是真没人点，也可能是根本没在统计，
// 而这两件事会导出完全相反的投放决定。补上这条上报口是为了让那一列有意义。
func (s *AppService) TrackBannerClick(ctx context.Context, appID int64, bannerID int64) error {
	if bannerID <= 0 {
		return apperrors.New(40000, http.StatusBadRequest, "无效的 Banner 标识")
	}
	if _, err := s.GetApp(ctx, appID); err != nil {
		return err
	}
	ok, err := s.pg.IncrementBannerClick(ctx, appID, bannerID)
	if err != nil {
		return err
	}
	if !ok {
		return apperrors.New(40411, http.StatusNotFound, "Banner 不存在")
	}
	return nil
}

/* ───────────────────── Banner 管理端 ───────────────────── */

func (s *AppService) ListBannersForAdmin(ctx context.Context, appID int64, filter appdomain.BannerFilter) ([]appdomain.Banner, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	items, err := s.pg.ListBanners(ctx, appID, filter)
	if err != nil {
		return nil, err
	}
	s.ResolveBannerImages(ctx, items)
	return items, nil
}

func (s *AppService) SaveBanner(ctx context.Context, appID int64, mutation appdomain.BannerMutation) (*appdomain.Banner, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	current, err := s.pg.GetBannerByID(ctx, appID, mutation.ID)
	if err != nil {
		return nil, err
	}
	if mutation.ID > 0 && current == nil {
		return nil, apperrors.New(40411, http.StatusNotFound, "Banner 不存在")
	}

	item := appdomain.Banner{
		ID:     mutation.ID,
		Type:   appdomain.BannerSlotHero,
		Status: true,
	}
	isNew := current == nil
	if current != nil {
		item = *current
	}

	if mutation.Header != nil {
		item.Header = strings.TrimSpace(*mutation.Header)
	}
	if mutation.Title != nil {
		item.Title = strings.TrimSpace(*mutation.Title)
	}
	if item.Title == "" {
		return nil, apperrors.New(40022, http.StatusBadRequest, "Banner 标题不能为空")
	}
	if mutation.Content != nil {
		// Banner 的描述是一行短说明，按纯文本处理即可；
		// 允许富文本反而会让各端的轮播卡片渲染出五花八门的高度。
		item.Content = strings.TrimSpace(plainTextFromHTML(*mutation.Content))
	}
	if mutation.URL != nil {
		item.URL = strings.TrimSpace(*mutation.URL)
	}
	if mutation.Type != nil {
		item.Type = strings.ToLower(strings.TrimSpace(*mutation.Type))
	}
	if item.Type == "" {
		item.Type = appdomain.BannerSlotHero
	}
	if _, ok := appdomain.ValidBannerTypes[item.Type]; !ok {
		return nil, apperrors.New(40027, http.StatusBadRequest, "不支持的 Banner 展示位")
	}
	if mutation.Status != nil {
		item.Status = *mutation.Status
	}
	if mutation.StartTime != nil {
		item.StartTime = normalizeContentTime(mutation.StartTime)
	}
	if mutation.EndTime != nil {
		item.EndTime = normalizeContentTime(mutation.EndTime)
	}
	if err := validateContentWindow(item.StartTime, item.EndTime); err != nil {
		return nil, err
	}

	switch {
	case mutation.Position != nil:
		item.Position = max(0, *mutation.Position)
	case isNew:
		// 新建时排到末位。默认落在 0 会与既有 Banner 抢同一个次序，
		// 表现为「新建的 Banner 随机插在中间」，而管理员并没有动过顺序。
		next, err := s.pg.NextBannerPosition(ctx, appID)
		if err != nil {
			return nil, err
		}
		item.Position = next
	}

	saved, err := s.pg.UpsertBanner(ctx, appID, item)
	if err != nil {
		return nil, err
	}
	s.invalidateBannerCache(ctx, appID)
	s.ResolveBannerImage(ctx, saved)
	return saved, nil
}

func (s *AppService) DeleteBanner(ctx context.Context, appID int64, bannerID int64) error {
	deleted, err := s.pg.DeleteBanner(ctx, appID, bannerID)
	if err != nil {
		return err
	}
	if !deleted {
		return apperrors.New(40411, http.StatusNotFound, "Banner 不存在")
	}
	s.invalidateBannerCache(ctx, appID)
	return nil
}

func (s *AppService) DeleteBanners(ctx context.Context, appID int64, bannerIDs []int64) (int64, []int64, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return 0, nil, err
	}
	ids := normalizeUniqueIDs(bannerIDs)
	if len(ids) == 0 {
		return 0, nil, apperrors.New(40025, http.StatusBadRequest, "Banner 标识不能为空")
	}
	deleted, err := s.pg.DeleteBanners(ctx, appID, ids)
	if err != nil {
		return 0, nil, err
	}
	s.invalidateBannerCache(ctx, appID)
	return deleted, ids, nil
}

// ReorderBanners 按传入的 id 顺序重写投放次序。
//
// 前端拖完之后一次性把整个可见顺序发过来，而不是发「把第 3 条移到第 1 条」——
// 后者在两个管理员同时拖拽时会算出谁也没想要的第三种顺序。
func (s *AppService) ReorderBanners(ctx context.Context, appID int64, bannerIDs []int64) ([]appdomain.Banner, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	ids := normalizeUniqueIDs(bannerIDs)
	if len(ids) == 0 {
		return nil, apperrors.New(40025, http.StatusBadRequest, "Banner 标识不能为空")
	}
	if _, err := s.pg.ReorderBanners(ctx, appID, ids); err != nil {
		return nil, err
	}
	s.invalidateBannerCache(ctx, appID)
	return s.ListBannersForAdmin(ctx, appID, appdomain.BannerFilter{})
}

/* ───────────────────── 公告管理端 ───────────────────── */

func (s *AppService) ListNoticesForAdmin(ctx context.Context, appID int64, filter appdomain.NoticeFilter) ([]appdomain.Notice, int64, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, 0, err
	}
	return s.pg.ListNotices(ctx, appID, filter)
}

func (s *AppService) SaveNotice(ctx context.Context, appID int64, mutation appdomain.NoticeMutation) (*appdomain.Notice, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	current, err := s.pg.GetNoticeByID(ctx, appID, mutation.ID)
	if err != nil {
		return nil, err
	}
	if mutation.ID > 0 && current == nil {
		return nil, apperrors.New(40412, http.StatusNotFound, "公告不存在")
	}

	item := appdomain.Notice{
		ID:     mutation.ID,
		Type:   "notice",
		Level:  "normal",
		Status: appdomain.NoticeStatusDraft,
	}
	if current != nil {
		item = *current
	}
	previousStatus := item.Status

	if mutation.Title != nil {
		item.Title = strings.TrimSpace(*mutation.Title)
	}
	if item.Title == "" {
		return nil, apperrors.New(40024, http.StatusBadRequest, "公告标题不能为空")
	}
	if mutation.Content != nil {
		item.Content = sanitizeRichText(*mutation.Content)
	}
	if richTextIsEmpty(item.Content) {
		return nil, apperrors.New(40023, http.StatusBadRequest, "公告内容不能为空")
	}
	item.Summary = deriveNoticeSummary(item.Content)

	if mutation.Type != nil {
		item.Type = strings.ToLower(strings.TrimSpace(*mutation.Type))
	}
	if item.Type == "" {
		item.Type = "notice"
	}
	if _, ok := appdomain.ValidNoticeTypes[item.Type]; !ok {
		return nil, apperrors.New(40028, http.StatusBadRequest, "不支持的公告类型")
	}
	if mutation.Level != nil {
		item.Level = strings.ToLower(strings.TrimSpace(*mutation.Level))
	}
	if item.Level == "" {
		item.Level = "normal"
	}
	if _, ok := appdomain.ValidNoticeLevels[item.Level]; !ok {
		return nil, apperrors.New(40029, http.StatusBadRequest, "不支持的公告级别")
	}
	if mutation.Status != nil {
		item.Status = strings.ToLower(strings.TrimSpace(*mutation.Status))
	}
	if item.Status == "" {
		item.Status = appdomain.NoticeStatusDraft
	}
	if _, ok := appdomain.ValidNoticeStatuses[item.Status]; !ok {
		return nil, apperrors.New(40030, http.StatusBadRequest, "不支持的公告状态")
	}
	if mutation.Pinned != nil {
		item.Pinned = *mutation.Pinned
	}
	if mutation.StartTime != nil {
		item.StartTime = normalizeContentTime(mutation.StartTime)
	}
	if mutation.EndTime != nil {
		item.EndTime = normalizeContentTime(mutation.EndTime)
	}
	if err := validateContentWindow(item.StartTime, item.EndTime); err != nil {
		return nil, err
	}
	if mutation.CreatedBy != nil && item.ID == 0 {
		item.CreatedBy = mutation.CreatedBy
	}

	// 首次发布才盖发布时间戳。归档后重新发布沿用原时间 ——
	// 那是同一条公告，改一次状态就把它顶到列表最前面会误导所有人。
	if item.Status == appdomain.NoticeStatusPublished && item.PublishedAt == nil {
		now := time.Now().In(s.location)
		item.PublishedAt = &now
	}
	_ = previousStatus

	saved, err := s.pg.UpsertNotice(ctx, appID, item)
	if err != nil {
		return nil, err
	}
	s.invalidateNoticeCache(ctx, appID)
	return saved, nil
}

func (s *AppService) DeleteNotice(ctx context.Context, appID int64, noticeID int64) error {
	deleted, err := s.pg.DeleteNotice(ctx, appID, noticeID)
	if err != nil {
		return err
	}
	if !deleted {
		return apperrors.New(40412, http.StatusNotFound, "公告不存在")
	}
	s.invalidateNoticeCache(ctx, appID)
	return nil
}

func (s *AppService) DeleteNotices(ctx context.Context, appID int64, noticeIDs []int64) (int64, []int64, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return 0, nil, err
	}
	ids := normalizeUniqueIDs(noticeIDs)
	if len(ids) == 0 {
		return 0, nil, apperrors.New(40026, http.StatusBadRequest, "公告标识不能为空")
	}
	deleted, err := s.pg.DeleteNotices(ctx, appID, ids)
	if err != nil {
		return 0, nil, err
	}
	s.invalidateNoticeCache(ctx, appID)
	return deleted, ids, nil
}

/* ───────────────────── 总览 ───────────────────── */

func (s *AppService) ContentOverview(ctx context.Context, appID int64) (*appdomain.ContentOverview, error) {
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	return s.pg.ContentOverview(ctx, appID, time.Now().In(s.location))
}

/* ───────────────────── 图片上传与解析 ───────────────────── */

// ContentImageUploadInput Banner 图片上传入参。
type ContentImageUploadInput struct {
	ConfigName    string
	FileName      string
	ContentType   string
	ContentLength int64
	Content       io.Reader
	UploadedBy    *int64
}

// ContentImageUploadResult Reference 落库，URL 立即可预览。
type ContentImageUploadResult struct {
	Reference string `json:"reference"`
	URL       string `json:"url"`
}

// UploadBannerImage 把管理员拖进来的图片推到该应用的对象存储。
//
// 升级前这里只有一个「头图 URL」输入框：要放一张图得先自己找地方托管、
// 再把链接抄进来。图床挂掉或链接过期时，Banner 在客户端上就是一块白。
func (s *AppService) UploadBannerImage(ctx context.Context, appID int64, baseURL string, input ContentImageUploadInput) (*ContentImageUploadResult, error) {
	if s.storage == nil {
		return nil, apperrors.New(50380, http.StatusServiceUnavailable, "存储服务未启用")
	}
	if _, err := s.GetApp(ctx, appID); err != nil {
		return nil, err
	}
	contentType, ext, err := validateContentImageUpload(input.FileName, input.ContentType, input.ContentLength)
	if err != nil {
		return nil, err
	}
	key, err := contentImageObjectKey(appID, ext)
	if err != nil {
		return nil, err
	}

	stored, err := s.storage.UploadForApp(ctx, appID, storagedomain.UploadInput{
		AppID:         appID,
		ConfigName:    strings.TrimSpace(input.ConfigName),
		ObjectKey:     key,
		FileName:      strings.TrimSpace(input.FileName),
		ContentType:   contentType,
		ContentLength: input.ContentLength,
		CacheControl:  "public, max-age=31536000, immutable",
		Metadata:      map[string]string{"module": "app-banner"},
		Content:       input.Content,
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
	display := s.resolveContentStorageURL(ctx, appID, stored.ConfigID, stored.Key)
	if display == "" {
		display = strings.TrimSpace(stored.URL)
	}
	return &ContentImageUploadResult{Reference: reference, URL: display}, nil
}

// ResolveBannerImages 批量填充展示 URL。
func (s *AppService) ResolveBannerImages(ctx context.Context, items []appdomain.Banner) {
	for i := range items {
		s.ResolveBannerImage(ctx, &items[i])
	}
}

// ResolveBannerImage 把落库的 header 解析成浏览器能直接访问的地址。
// 外链原样返回；`storage://` 引用换成带票据的代理路径。
func (s *AppService) ResolveBannerImage(ctx context.Context, item *appdomain.Banner) {
	if item == nil {
		return
	}
	stored := strings.TrimSpace(item.Header)
	if stored == "" {
		item.HeaderDisplayURL = ""
		return
	}
	configID, objectKey, ok := parseStorageReference(stored)
	if !ok {
		item.HeaderDisplayURL = stored
		return
	}
	item.HeaderDisplayURL = s.resolveContentStorageURL(ctx, 0, configID, objectKey)
}

func (s *AppService) resolveContentStorageURL(ctx context.Context, appID int64, configID int64, objectKey string) string {
	if s.storage == nil || configID <= 0 || strings.TrimSpace(objectKey) == "" {
		return ""
	}
	result, ticketID, err := s.storage.CreateObjectLinkByConfigID(ctx, appID, configID, storagedomain.LinkRequest{
		ObjectKey: objectKey,
		ExpiresIn: contentImageProxyTTL,
	})
	if err != nil {
		s.log.Warn("resolve banner image url failed",
			zap.Int64("config_id", configID), zap.String("object_key", objectKey), zap.Error(err))
		return ""
	}
	if result == nil {
		return ""
	}
	if ticketID != "" {
		// 返回相对路径：next/image 会把 `/api/...` 当同源图片，
		// 不走 Next 的 upstream 图片管线，也就不会被它的私网 IP 防护拦下。
		return "/api/storage/proxy/" + url.PathEscape(ticketID)
	}
	return strings.TrimSpace(result.URL)
}

/* ───────────────────── 缓存与小工具 ───────────────────── */

func (s *AppService) invalidateBannerCache(ctx context.Context, appID int64) {
	if s.sessions == nil {
		return
	}
	if err := s.sessions.DeleteBanners(ctx, appID); err != nil {
		s.log.Warn("delete banner cache failed", zap.Int64("appid", appID), zap.Error(err))
	}
}

func (s *AppService) invalidateNoticeCache(ctx context.Context, appID int64) {
	if s.sessions == nil {
		return
	}
	if err := s.sessions.DeleteNotices(ctx, appID); err != nil {
		s.log.Warn("delete notice cache failed", zap.Int64("appid", appID), zap.Error(err))
	}
}

// normalizeContentTime 把零值时间当成「清空」。
// 前端删掉时间输入框时发来的是零值，照原样存下去会得到 0001-01-01，
// 那个时间永远早于 now，于是「不限开始时间」和「从公元一年开始」在库里长得一样。
func normalizeContentTime(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	return value
}

func validateContentWindow(start *time.Time, end *time.Time) error {
	if start == nil || end == nil {
		return nil
	}
	if end.Before(*start) || end.Equal(*start) {
		return apperrors.New(40031, http.StatusBadRequest, "结束时间必须晚于开始时间")
	}
	return nil
}

var contentImageContentTypeByExt = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
	".webp": "image/webp",
	".svg":  "image/svg+xml",
}

var contentImageExtByContentType = map[string]string{
	"image/jpeg":    ".jpg",
	"image/png":     ".png",
	"image/gif":     ".gif",
	"image/webp":    ".webp",
	"image/svg+xml": ".svg",
}

func validateContentImageUpload(fileName, contentType string, size int64) (string, string, error) {
	if size <= 0 {
		return "", "", apperrors.New(40087, http.StatusBadRequest, "上传文件不能为空")
	}
	if size > contentImageMaxUploadSize {
		return "", "", apperrors.New(40088, http.StatusBadRequest, "Banner 图片不能超过 10MB")
	}
	ext := strings.ToLower(strings.TrimSpace(path.Ext(fileName)))
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if mapped, ok := contentImageContentTypeByExt[ext]; ok {
		return mapped, ext, nil
	}
	if mappedExt, ok := contentImageExtByContentType[contentType]; ok {
		return contentType, mappedExt, nil
	}
	return "", "", apperrors.New(40089, http.StatusBadRequest, "仅支持 JPG / PNG / GIF / WEBP / SVG 图片")
}

func contentImageObjectKey(appID int64, ext string) (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("apps/%d/banners/%s/%s%s", appID, time.Now().UTC().Format("200601"), hex.EncodeToString(buf), ext), nil
}
