package service

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	legaldomain "aegis/internal/domain/legal"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/i18n"
	"go.uber.org/zap"
	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

// LegalService 法律文本（用户协议 / 隐私政策）。
//
// 三条结构性约束：
//
//  1. **对外永远有内容可读。** 管理员没写过的语言回落到内置默认全文，
//     内置也没有的语言回落到内置默认语言。登录页那两个链接是一直在的，
//     点开落到空白页比没有链接更糟。返回值里的 `source` 说明这一份是谁写的。
//
//  2. **语言协商与其它模块同一套实现**（`pkg/i18n.Negotiate`）。
//     协商规则散成两份的表现是「接口按一套规则挑语言、PDF 按另一套」，
//     而这种分歧只在某几个特定语言下才暴露。
//
//  3. **富文本在写入时净化，不在读取时。** 正文会被注入到门户页面、
//     控制台预览和邮件里；净化放在读取端意味着每个消费方都要记得做一次。
//     与应用级内容中心共用同一个策略（`sanitizeRichText`）。
type LegalService struct {
	log      *zap.Logger
	pg       *pgrepo.Repository
	settings *PlatformSettingsService

	// 兜底品牌名与联系邮箱。品牌名优先取平台配置里的，取不到才用这里的。
	fallbackName string
	contactEmail string
	// authoritative 准据语言。十种内置语言是同一份条款的十个译本，
	// 出现歧义时以哪一版为准必须指明，否则等于十份效力不明的合同。
	authoritative language.Tag
}

func NewLegalService(log *zap.Logger, pg *pgrepo.Repository, settings *PlatformSettingsService, fallbackName, contactEmail, authoritativeLocale string) *LegalService {
	authoritative := i18n.ParseTag(authoritativeLocale)
	if authoritative == language.Und {
		authoritative = legalDefaultLocale
	}
	return &LegalService{
		log:           log,
		pg:            pg,
		settings:      settings,
		fallbackName:  strings.TrimSpace(fallbackName),
		contactEmail:  strings.TrimSpace(contactEmail),
		authoritative: authoritative,
	}
}

// AuthoritativeLocale 准据语言。
func (s *LegalService) AuthoritativeLocale() string { return s.authoritative.String() }

// legalDefaultLocale 回落终点。内置文本一定有这个语言，否则整条回落链是悬空的。
var legalDefaultLocale = language.MustParse("zh-Hans")

// ── 公开读取 ──────────────────────────────────────────────

// Document 按偏好取一份文本。prefs 依次为显式 locale、用户设置、Accept-Language。
func (s *LegalService) Document(ctx context.Context, docType legaldomain.DocType, prefs ...string) (*legaldomain.DocumentView, error) {
	if !docType.Valid() {
		return nil, apperrors.New(40474, http.StatusNotFound, "未知的法律文本类型")
	}

	custom, err := s.publishedByLocale(ctx, docType)
	if err != nil {
		return nil, err
	}
	options := s.localeOptions(docType, custom)
	if len(options) == 0 {
		return nil, apperrors.New(40474, http.StatusNotFound, "该法律文本暂未提供")
	}

	available := make([]language.Tag, 0, len(options))
	for _, option := range options {
		if tag := i18n.ParseTag(option.Locale); tag != language.Und {
			available = append(available, tag)
		}
	}
	chosen := i18n.Negotiate(available, s.fallbackTag(options), prefs...)
	locale := chosen.String()

	doc, err := s.resolve(ctx, docType, locale, custom)
	if err != nil {
		return nil, err
	}

	view := &legaldomain.DocumentView{
		Document:            *doc,
		Locales:             options,
		AuthoritativeLocale: s.authoritative.String(),
	}
	for _, pref := range prefs {
		if strings.TrimSpace(pref) != "" {
			view.Requested = strings.TrimSpace(pref)
			break
		}
	}
	return view, nil
}

// Catalog 列出全部法律文本及其可用语言，供门户与页脚渲染入口。
func (s *LegalService) Catalog(ctx context.Context) ([]legaldomain.CatalogEntry, error) {
	entries := make([]legaldomain.CatalogEntry, 0, len(legaldomain.DocTypes()))
	for _, docType := range legaldomain.DocTypes() {
		custom, err := s.publishedByLocale(ctx, docType)
		if err != nil {
			return nil, err
		}
		options := s.localeOptions(docType, custom)
		if len(options) == 0 {
			continue
		}
		// 目录里的标题用回落语言那一份的标题 —— 目录本身没有语言上下文
		doc, err := s.resolve(ctx, docType, s.fallbackTag(options).String(), custom)
		if err != nil {
			return nil, err
		}
		entries = append(entries, legaldomain.CatalogEntry{DocType: docType, Title: doc.Title, Locales: options})
	}
	return entries, nil
}

// ── 管理端 ────────────────────────────────────────────────

// AdminList 管理端列表：自定义文本与内置文本合并，内置的标 source=default。
//
// 合并而不是只列自定义的：管理员要看的是「这个文档在每种语言下现在是什么」，
// 只列自己写过的会让「英文版还是内置的」这件事完全看不出来。
func (s *LegalService) AdminList(ctx context.Context) ([]legaldomain.Document, error) {
	rows, err := s.pg.ListAllLegalDocuments(ctx)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(rows))
	items := make([]legaldomain.Document, 0, len(rows)+len(legalDefaultDocs))
	for _, row := range rows {
		seen[string(row.DocType)+"/"+row.Locale] = true
		row.Body = s.render(ctx, row.Body, row.Locale)
		items = append(items, row)
	}
	for _, def := range legalDefaultDocs {
		if seen[string(def.DocType)+"/"+def.Locale] {
			continue
		}
		doc, err := s.defaultDocument(ctx, def)
		if err != nil {
			return nil, err
		}
		items = append(items, *doc)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].DocType != items[j].DocType {
			return items[i].DocType < items[j].DocType
		}
		return items[i].Locale < items[j].Locale
	})
	return items, nil
}

// AdminGet 取一份文本用于编辑。没有自定义版本时返回内置版本作为起草底稿。
func (s *LegalService) AdminGet(ctx context.Context, docType legaldomain.DocType, locale string) (*legaldomain.Document, error) {
	if !docType.Valid() {
		return nil, apperrors.New(40474, http.StatusNotFound, "未知的法律文本类型")
	}
	normalized, err := normalizeLegalLocale(locale)
	if err != nil {
		return nil, err
	}
	return s.resolve(ctx, docType, normalized, nil)
}

// Save 写入一份文本。
func (s *LegalService) Save(ctx context.Context, input legaldomain.SaveInput) (*legaldomain.Document, error) {
	if !input.DocType.Valid() {
		return nil, apperrors.New(40474, http.StatusNotFound, "未知的法律文本类型")
	}
	locale, err := normalizeLegalLocale(input.Locale)
	if err != nil {
		return nil, err
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, apperrors.New(40041, http.StatusBadRequest, "标题不能为空")
	}
	body := sanitizeRichText(input.Body)
	// 净化后为空说明整段正文都被白名单挡下了，或者本来就只有一个空段落。
	// 直接存进去的结果是一个点开什么都没有的法律文本页面。
	if richTextIsEmpty(body) {
		return nil, apperrors.New(40042, http.StatusBadRequest, "正文不能为空")
	}

	record := legaldomain.Document{
		DocType:     input.DocType,
		Locale:      locale,
		Title:       title,
		Summary:     legalSummary(body),
		Body:        body,
		Version:     strings.TrimSpace(input.Version),
		EffectiveAt: input.EffectiveAt,
		Published:   input.Published,
		UpdatedBy:   input.UpdatedBy,
	}
	saved, err := s.pg.UpsertLegalDocument(ctx, record)
	if err != nil {
		return nil, err
	}
	saved.Body = s.render(ctx, saved.Body, saved.Locale)
	return saved, nil
}

// Delete 删除一份自定义文本，该语言随即回落到内置版本（或从可选语言里消失）。
func (s *LegalService) Delete(ctx context.Context, docType legaldomain.DocType, locale string) error {
	if !docType.Valid() {
		return apperrors.New(40474, http.StatusNotFound, "未知的法律文本类型")
	}
	normalized, err := normalizeLegalLocale(locale)
	if err != nil {
		return err
	}
	removed, err := s.pg.DeleteLegalDocument(ctx, docType, normalized)
	if err != nil {
		return err
	}
	if !removed {
		return apperrors.New(40475, http.StatusNotFound, "该语言没有自定义版本")
	}
	return nil
}

// BuiltinLocales 内置了哪些语言，供控制台的「新增语言」选择器使用。
//
// 让服务端给而不是前端硬编码一份：语言集是后端的 legalBuiltinLocales 说了算，
// 前端另抄一份的结果是「选择器里有日文、存进去发现没有内置底稿」。
func (s *LegalService) BuiltinLocales() []legaldomain.LocaleOption {
	options := make([]legaldomain.LocaleOption, 0, len(legalBuiltinLocales))
	for _, item := range legalBuiltinLocales {
		tag := i18n.ParseTag(item.Locale)
		if tag == language.Und {
			continue
		}
		options = append(options, legaldomain.LocaleOption{
			Locale:        item.Locale,
			NativeName:    display.Self.Name(tag),
			Name:          display.English.Tags().Name(tag),
			Source:        legaldomain.SourceDefault,
			Default:       tag == s.authoritative,
			Authoritative: tag == s.authoritative,
		})
	}
	return options
}

// RenderTokens 把一段草稿里的占位符按当前部署的值替换掉。
//
// 控制台的实时预览用它 —— 预览里如果还留着 `{{platformName}}`，
// 管理员就没法判断这一版排版对不对；而前端自己拼的话，
// 平台名与联系邮箱这两个值就得再下发一次、再各自维护一套替换规则。
func (s *LegalService) RenderTokens(ctx context.Context, body, locale string) string {
	return s.render(ctx, body, locale)
}

// ContactConfigured 联系邮箱是否已配置。控制台据此提示 ——
// 内置文本的「联系我们」一节引用它，没配就会印出一句占位文字。
func (s *LegalService) ContactConfigured() bool { return s.contactEmail != "" }

// ── 内部 ──────────────────────────────────────────────────

// resolve 按「自定义 → 内置该语言 → 内置默认语言」的顺序取一份文本。
func (s *LegalService) resolve(ctx context.Context, docType legaldomain.DocType, locale string, cached map[string]legaldomain.Document) (*legaldomain.Document, error) {
	if cached != nil {
		if doc, ok := cached[locale]; ok {
			doc.Body = s.render(ctx, doc.Body, doc.Locale)
			return &doc, nil
		}
	} else {
		row, err := s.pg.GetLegalDocument(ctx, docType, locale)
		if err != nil {
			return nil, err
		}
		if row != nil {
			row.Body = s.render(ctx, row.Body, row.Locale)
			return row, nil
		}
	}

	if def, ok := findLegalDefault(docType, locale); ok {
		return s.defaultDocument(ctx, def)
	}
	if def, ok := findLegalDefault(docType, s.authoritative.String()); ok {
		return s.defaultDocument(ctx, def)
	}
	if def, ok := findLegalDefault(docType, legalDefaultLocale.String()); ok {
		return s.defaultDocument(ctx, def)
	}
	return nil, apperrors.New(40474, http.StatusNotFound, "该法律文本暂未提供")
}

func (s *LegalService) defaultDocument(ctx context.Context, def legalDefaultDoc) (*legaldomain.Document, error) {
	body, err := legalDefaultBody(def)
	if err != nil {
		return nil, err
	}
	rendered := s.render(ctx, body, def.Locale)
	return &legaldomain.Document{
		DocType:     def.DocType,
		Locale:      def.Locale,
		Title:       def.Title,
		Summary:     legalSummary(rendered),
		Body:        rendered,
		Version:     def.Version,
		EffectiveAt: parseLegalEffective(def.Effective),
		Published:   true,
		Source:      legaldomain.SourceDefault,
	}, nil
}

// publishedByLocale 取某文档已发布的自定义文本，按语言索引。
func (s *LegalService) publishedByLocale(ctx context.Context, docType legaldomain.DocType) (map[string]legaldomain.Document, error) {
	rows, err := s.pg.ListLegalDocuments(ctx, docType, true)
	if err != nil {
		return nil, err
	}
	byLocale := make(map[string]legaldomain.Document, len(rows))
	for _, row := range rows {
		byLocale[row.Locale] = row
	}
	return byLocale, nil
}

// localeOptions 该文档现在能提供哪些语言：自定义的 + 内置的，自定义覆盖内置。
func (s *LegalService) localeOptions(docType legaldomain.DocType, custom map[string]legaldomain.Document) []legaldomain.LocaleOption {
	sources := make(map[string]string, len(custom)+2)
	for _, locale := range legalDefaultLocales(docType) {
		sources[locale] = legaldomain.SourceDefault
	}
	for locale := range custom {
		sources[locale] = legaldomain.SourceCustom
	}

	options := make([]legaldomain.LocaleOption, 0, len(sources))
	for locale, source := range sources {
		tag := i18n.ParseTag(locale)
		if tag == language.Und {
			continue
		}
		options = append(options, legaldomain.LocaleOption{
			Locale:        locale,
			NativeName:    display.Self.Name(tag),
			Name:          display.English.Tags().Name(tag),
			Source:        source,
			Default:       tag == s.authoritative,
			Authoritative: tag == s.authoritative,
		})
	}
	sort.Slice(options, func(i, j int) bool {
		// 默认语言排首位，其余按标签字典序 —— 语言切换器每次的顺序必须一致
		if options[i].Default != options[j].Default {
			return options[i].Default
		}
		return options[i].Locale < options[j].Locale
	})
	return options
}

// fallbackTag 这一批可选语言的回落终点。内置默认语言在其中就用它，
// 否则用列表首项 —— 管理员可能删光了中文版只留日文版。
func (s *LegalService) fallbackTag(options []legaldomain.LocaleOption) language.Tag {
	for _, option := range options {
		if i18n.ParseTag(option.Locale) == s.authoritative {
			return s.authoritative
		}
	}
	if len(options) > 0 {
		if tag := i18n.ParseTag(options[0].Locale); tag != language.Und {
			return tag
		}
	}
	return legalDefaultLocale
}

// render 替换正文里的占位符。品牌名以平台配置为准，让改了品牌名的部署方
// 不必把两份条款逐句改一遍。
func (s *LegalService) render(ctx context.Context, body, locale string) string {
	return renderLegalTokens(body, s.platformName(ctx), s.contactEmail, locale)
}

func (s *LegalService) platformName(ctx context.Context) string {
	if s.settings != nil {
		if name := strings.TrimSpace(s.settings.BrandingPlatformName(ctx)); name != "" {
			return name
		}
	}
	if s.fallbackName != "" {
		return s.fallbackName
	}
	return "Aegis"
}

// normalizeLegalLocale 归一化语言标签。
//
// 不归一化的表现是 "zh-hans" 与 "zh-Hans" 变成两行，而协商时只认得出其中一行 ——
// 管理员会看到「明明写了中文版，页面上还是英文」。
func normalizeLegalLocale(raw string) (string, error) {
	tag := i18n.ParseTag(raw)
	if tag == language.Und {
		return "", apperrors.New(40043, http.StatusBadRequest, "语言标签无效，请使用 BCP 47 格式（如 zh-Hans、en、ja）")
	}
	return tag.String(), nil
}

// legalSummary 从正文提取纯文本摘要。按字符数截断而不是字节 ——
// 中文摘要按字节截会把最后一个字切成乱码。
func legalSummary(body string) string {
	text := plainTextFromHTML(body)
	runes := []rune(text)
	if len(runes) <= noticeSummaryMaxRunes {
		return text
	}
	return strings.TrimSpace(string(runes[:noticeSummaryMaxRunes])) + "…"
}

// LegalEffectiveFromString 解析管理端传来的生效日期，空串表示不设置。
//
// 导出给 transport 层用：日期格式的宽松程度是业务约定（接受 YYYY-MM-DD 也接受
// RFC3339），放在 handler 里各写一份必然会出现两个接口接受的格式不一样。
func LegalEffectiveFromString(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
		return &parsed, nil
	}
	parsed, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return nil, apperrors.New(40044, http.StatusBadRequest, "生效日期格式无效，请使用 YYYY-MM-DD")
	}
	return &parsed, nil
}
