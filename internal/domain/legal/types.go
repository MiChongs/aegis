// Package legal 定义法律文本（用户协议 / 隐私政策）的领域类型。
//
// 一份文本由「文档类型 × 语言」唯一确定。管理员没有为某个语言写过时，
// 服务端发内置的默认全文 —— 因此对外永远有内容可读，`Source` 字段说明
// 这一份是管理员自定义的还是内置的。
package legal

import "time"

// DocType 文档类型。取值范围由这里的白名单管着，不落到数据库枚举上。
type DocType string

const (
	DocTerms   DocType = "terms"
	DocPrivacy DocType = "privacy"
)

// DocTypes 全部合法的文档类型，顺序即控制台与门户的展示顺序。
func DocTypes() []DocType { return []DocType{DocTerms, DocPrivacy} }

// Valid 是否是已知的文档类型。
func (t DocType) Valid() bool {
	for _, item := range DocTypes() {
		if item == t {
			return true
		}
	}
	return false
}

// Source 这一份文本的来源。
//
// 对使用者是有意义的信息而不是实现细节：看到 `default` 就知道
// 「这是这套系统自带的通用条款，本部署方还没有写自己的」。
const (
	SourceDefault = "default"
	SourceCustom  = "custom"
)

// Document 一份法律文本。
type Document struct {
	ID          int64      `json:"id,omitempty"`
	DocType     DocType    `json:"docType"`
	Locale      string     `json:"locale"`
	Title       string     `json:"title"`
	Summary     string     `json:"summary"`
	Body        string     `json:"body"` // 已净化的 HTML
	Version     string     `json:"version"`
	EffectiveAt *time.Time `json:"effectiveAt,omitempty"`
	Published   bool       `json:"published"`
	Source      string     `json:"source"`
	UpdatedBy   *int64     `json:"updatedBy,omitempty"`
	CreatedAt   *time.Time `json:"createdAt,omitempty"`
	UpdatedAt   *time.Time `json:"updatedAt,omitempty"`
}

// LocaleOption 一个可选语言，供前端渲染语言切换器。
type LocaleOption struct {
	Locale     string `json:"locale"`
	NativeName string `json:"nativeName"`
	Name       string `json:"name"`
	Source     string `json:"source"`
	Default    bool   `json:"default"`
	// Authoritative 是否是准据文本。多语言法律文本必须指定其中一版为准 ——
	// 十个译本各自生效等于十份效力不明的合同，出现歧义时无从裁断。
	Authoritative bool `json:"authoritative"`
}

// DocumentView 公开接口返回的一份文本 + 它的语言清单。
//
// 语言清单和文本一起下发，而不是让前端再问一次：切语言这件事只有拿到
// 「有哪些语言」才能做，分两次请求会出现「正文已经是英文、切换器还在转」的画面。
type DocumentView struct {
	Document
	Locales []LocaleOption `json:"locales"`
	// Requested 是请求方要的语言，Locale 是协商后真正给的那一份。
	// 两者不同就说明发生了回落，前端据此提示「暂无该语言版本，以下为 X 版」。
	Requested string `json:"requested,omitempty"`
	// AuthoritativeLocale 准据文本的语言。当前这一份不是它时，
	// 页面上必须写明「本页为译文」—— 否则读者会以为译文与原文同等效力。
	AuthoritativeLocale string `json:"authoritativeLocale"`
}

// Summary 文档类型在目录里的一条，供门户列出「有哪些法律文本」。
type CatalogEntry struct {
	DocType DocType        `json:"docType"`
	Title   string         `json:"title"`
	Locales []LocaleOption `json:"locales"`
}

// SaveInput 管理端写入一份文本。
type SaveInput struct {
	DocType     DocType
	Locale      string
	Title       string
	Body        string
	Version     string
	EffectiveAt *time.Time
	Published   bool
	UpdatedBy   *int64
}
