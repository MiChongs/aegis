// Package i18n 是平台的通用国际化工具包：语言协商、消息目录、复数规则，
// 以及 locale 感知的数字 / 金额 / 日期格式化。
//
// 设计上的三条取向：
//
//   - **默认语言恒为兜底**。任何一条消息在目标语言缺失时都回落到默认语言，
//     再不行才回落到 key 本身。缺翻译是小事，缺内容是事故 —— 一份收据不能因为
//     少了一句译文就出现空白字段。
//   - **金额不走浮点**。货币金额用 decimal 精确格式化，分组与小数点分隔符来自目录。
//     x/text 的 number 包要求 float64，18 位精度的 NUMERIC 过一遍 float 就不再可靠了。
//   - **目录是数据不是代码**。译文放 JSON，用 embed.FS 装配，新增语言不需要改 Go 代码。
package i18n

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"sort"
	"strings"
	"sync"

	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"
)

// 目录 JSON 中的保留键：以 $ 开头，与普通消息键天然区隔。
const (
	metaKey   = "$meta"
	formatKey = "$format"
)

// ErrNoLocales 目录为空。构造 Bundle 时就该发现，而不是等到第一次渲染。
var ErrNoLocales = errors.New("i18n: 消息目录为空")

// Meta 一个语言的自描述信息。Script 决定它需要什么字体 ——
// 渲染 PDF 前据此判断当前字体能否承载该语言，从而在**渲染之前**决定是否降级。
type Meta struct {
	Name       string `json:"name"`       // 英文名，如 "Simplified Chinese"
	NativeName string `json:"nativeName"` // 本地名，如 "简体中文"
	Direction  string `json:"direction"`  // ltr / rtl
	Script     string `json:"script"`     // latin / han / kana / hangul / cyrillic
}

// FormatSpec 一个语言的数字与时间格式约定。
type FormatSpec struct {
	Group            string            `json:"group"`            // 千位分隔符
	Decimal          string            `json:"decimal"`          // 小数点
	GroupSizes       []int             `json:"groupSizes"`       // 分组位数，从低位起；如印度记数法 [3,2]
	Currency         string            `json:"currency"`         // 正数金额模板，占位符 {symbol} {value} {code}
	CurrencyNegative string            `json:"currencyNegative"` // 负数金额模板
	Symbols          map[string]string `json:"symbols"`          // ISO 代码 → 本语言下的货币符号覆盖
	Date             string            `json:"date"`             // Go 时间布局
	DateTime         string            `json:"dateTime"`
	Time             string            `json:"time"`
}

// Locale 一个已装载的语言。
type Locale struct {
	Tag    language.Tag
	Meta   Meta
	Format FormatSpec

	messages map[string]entry
}

// entry 一条消息：要么是单一文本，要么是一组复数形式。
type entry struct {
	text   string
	plural map[plural.Form]string
}

// Bundle 一组语言的消息目录 + 语言协商器。构造后只读，可并发使用。
type Bundle struct {
	defaultTag language.Tag
	locales    map[language.Tag]*Locale
	tags       []language.Tag
	matcher    language.Matcher

	mu    sync.RWMutex
	cache map[language.Tag]*Localizer
}

// LoadFS 从文件系统装配 Bundle。目录里每个 <BCP47 标签>.json 即一个语言，
// 文件名就是语言标签（en.json / zh-Hans.json / pt-BR.json）。
//
// defaultTag 必须在目录中存在 —— 它是所有回落的终点，缺了它整套回落链就是悬空的。
func LoadFS(fsys fs.FS, dir string, defaultTag language.Tag) (*Bundle, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("i18n: 读取消息目录失败：%w", err)
	}
	locales := make(map[language.Tag]*Locale)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		tag, err := language.Parse(name)
		if err != nil {
			return nil, fmt.Errorf("i18n: 文件名 %q 不是合法的语言标签：%w", entry.Name(), err)
		}
		raw, err := fs.ReadFile(fsys, path.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("i18n: 读取 %s 失败：%w", entry.Name(), err)
		}
		locale, err := parseLocale(tag, raw)
		if err != nil {
			return nil, fmt.Errorf("i18n: 解析 %s 失败：%w", entry.Name(), err)
		}
		locales[tag] = locale
	}
	return New(locales, defaultTag)
}

// New 用已解析好的语言表构造 Bundle。
func New(locales map[language.Tag]*Locale, defaultTag language.Tag) (*Bundle, error) {
	if len(locales) == 0 {
		return nil, ErrNoLocales
	}
	fallback, ok := locales[defaultTag]
	if !ok {
		return nil, fmt.Errorf("i18n: 默认语言 %s 不在目录中", defaultTag)
	}

	// 默认语言必须排在匹配器首位：x/text 以第一个标签为兜底，
	// 顺序错了会让「完全不认识的语言」落到一个随机的语言上。
	tags := []language.Tag{defaultTag}
	for tag := range locales {
		if tag != defaultTag {
			tags = append(tags, tag)
		}
	}
	sort.Slice(tags[1:], func(i, j int) bool { return tags[1+i].String() < tags[1+j].String() })

	// 缺项补默认：格式规范整段缺失时沿用默认语言的，避免出现「分隔符是空串」这种
	// 只在某个语言下才暴露的排版事故。
	for _, locale := range locales {
		locale.Format = mergeFormat(locale.Format, fallback.Format)
	}

	return &Bundle{
		defaultTag: defaultTag,
		locales:    locales,
		tags:       tags,
		matcher:    language.NewMatcher(tags),
		cache:      make(map[language.Tag]*Localizer, len(locales)),
	}, nil
}

// DefaultTag 默认语言。
func (b *Bundle) DefaultTag() language.Tag { return b.defaultTag }

// Tags 全部已装载的语言，默认语言在首位。
func (b *Bundle) Tags() []language.Tag { return slices.Clone(b.tags) }

// Locales 全部语言的自描述信息，供接口下发给前端做语言选择器。
func (b *Bundle) Locales() []LocaleInfo {
	infos := make([]LocaleInfo, 0, len(b.tags))
	for _, tag := range b.tags {
		locale := b.locales[tag]
		infos = append(infos, LocaleInfo{
			Tag:        tag.String(),
			Name:       locale.Meta.Name,
			NativeName: locale.Meta.NativeName,
			Direction:  orDefault(locale.Meta.Direction, "ltr"),
			Script:     orDefault(locale.Meta.Script, "latin"),
			Default:    tag == b.defaultTag,
		})
	}
	return infos
}

// LocaleInfo 对外暴露的语言描述。
type LocaleInfo struct {
	Tag        string `json:"tag"`
	Name       string `json:"name"`
	NativeName string `json:"nativeName"`
	Direction  string `json:"direction"`
	Script     string `json:"script"`
	Default    bool   `json:"default"`
}

// Has 是否精确装载了该语言。
func (b *Bundle) Has(tag language.Tag) bool {
	_, ok := b.locales[tag]
	return ok
}

// Match 按优先级协商语言。prefs 依次可以是显式指定的 locale、用户设置里的语言、
// Accept-Language 头 —— 前者为空或不认识时自动看后者，全不认识则回落到默认语言。
//
// 每个 pref 都按 Accept-Language 语法解析，因此可以直接把请求头整段传进来。
func (b *Bundle) Match(prefs ...string) language.Tag {
	for _, pref := range prefs {
		pref = strings.TrimSpace(pref)
		if pref == "" {
			continue
		}
		desired, _, err := language.ParseAcceptLanguage(pref)
		if err != nil || len(desired) == 0 {
			continue
		}
		// 先试精确/近似命中：x/text 的匹配器在完全不认识时会返回兜底项，
		// 因此要用 Confidence 区分「真的匹配上了」与「兜底」。
		_, index, confidence := b.matcher.Match(desired...)
		if confidence == language.No {
			continue
		}
		if index >= 0 && index < len(b.tags) {
			return b.tags[index]
		}
	}
	return b.defaultTag
}

// Localizer 取一个语言的本地化器；未装载的语言回落到默认语言。
func (b *Bundle) Localizer(tag language.Tag) *Localizer {
	b.mu.RLock()
	cached, ok := b.cache[tag]
	b.mu.RUnlock()
	if ok {
		return cached
	}

	locale, ok := b.locales[tag]
	if !ok {
		// 未精确装载时退到同语言的其它变体（zh-HK → zh-Hant），再不行退默认语言。
		if resolved := b.Match(tag.String()); resolved != tag {
			return b.Localizer(resolved)
		}
		locale = b.locales[b.defaultTag]
	}
	loc := &Localizer{
		bundle:   b,
		locale:   locale,
		fallback: b.locales[b.defaultTag],
	}

	b.mu.Lock()
	b.cache[tag] = loc
	b.mu.Unlock()
	return loc
}

// For 按优先级协商后直接返回本地化器，等价于 Localizer(Match(prefs...))。
func (b *Bundle) For(prefs ...string) *Localizer {
	return b.Localizer(b.Match(prefs...))
}

// parseLocale 解析单个语言目录。
func parseLocale(tag language.Tag, raw []byte) (*Locale, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	locale := &Locale{Tag: tag, messages: make(map[string]entry, len(doc))}
	for key, value := range doc {
		switch key {
		case metaKey:
			if err := json.Unmarshal(value, &locale.Meta); err != nil {
				return nil, fmt.Errorf("%s：%w", metaKey, err)
			}
		case formatKey:
			if err := json.Unmarshal(value, &locale.Format); err != nil {
				return nil, fmt.Errorf("%s：%w", formatKey, err)
			}
		default:
			msg, err := parseMessage(value)
			if err != nil {
				return nil, fmt.Errorf("消息 %q：%w", key, err)
			}
			locale.messages[key] = msg
		}
	}
	return locale, nil
}

// parseMessage 一条消息可以是字符串，也可以是 CLDR 复数形式对象。
func parseMessage(raw json.RawMessage) (entry, error) {
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		return entry{text: text}, nil
	}
	var forms map[string]string
	if err := json.Unmarshal(raw, &forms); err != nil {
		return entry{}, errors.New("既不是字符串也不是复数形式对象")
	}
	msg := entry{plural: make(map[plural.Form]string, len(forms))}
	for name, value := range forms {
		form, ok := pluralForms[name]
		if !ok {
			return entry{}, fmt.Errorf("未知的复数形式 %q（可用：zero/one/two/few/many/other）", name)
		}
		msg.plural[form] = value
	}
	if _, ok := msg.plural[plural.Other]; !ok {
		// CLDR 里 other 是唯一所有语言都存在的形式，缺了它就没有兜底分支。
		return entry{}, errors.New("复数形式必须包含 other")
	}
	return msg, nil
}

var pluralForms = map[string]plural.Form{
	"other": plural.Other,
	"zero":  plural.Zero,
	"one":   plural.One,
	"two":   plural.Two,
	"few":   plural.Few,
	"many":  plural.Many,
}

// mergeFormat 缺项补默认。这里刻意用「空串」而非「去空白后为空」判断缺失：
// 俄语、法语的千位分隔符本身就是空格（U+00A0），按空白判会把它们悄悄换成逗号。
func mergeFormat(spec, fallback FormatSpec) FormatSpec {
	spec.Group = orEmpty(spec.Group, fallback.Group)
	spec.Decimal = orEmpty(spec.Decimal, fallback.Decimal)
	spec.Currency = orEmpty(spec.Currency, fallback.Currency)
	spec.CurrencyNegative = orEmpty(spec.CurrencyNegative, fallback.CurrencyNegative)
	spec.Date = orEmpty(spec.Date, fallback.Date)
	spec.DateTime = orEmpty(spec.DateTime, fallback.DateTime)
	spec.Time = orEmpty(spec.Time, fallback.Time)
	if len(spec.GroupSizes) == 0 {
		spec.GroupSizes = fallback.GroupSizes
	}
	return spec
}

func orEmpty(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func orDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
