package payment

import "time"

// 凭证类型。默认按订单状态推导：已支付出「收据」，未支付出「账单」，
// 全额退款出「退款凭证」—— 给一笔没收到的钱开收据是在伪造凭证。
const (
	ReceiptTypeReceipt    = "receipt"
	ReceiptTypeInvoice    = "invoice"
	ReceiptTypeCreditNote = "credit_note"
)

// ReceiptOptions 一次凭证导出的请求参数。
type ReceiptOptions struct {
	// Locale 期望语言。按 Accept-Language 语法解析，因此可以直接传请求头。
	// 留空则按「用户设置 → 请求头 → 英文」依次决定。
	Locale string
	// AcceptLanguage 请求头原文，作为语言协商的次选来源
	AcceptLanguage string
	// DocumentType 凭证类型；留空按订单状态推导
	DocumentType string
	// Timezone IANA 时区名，留空则按用户设置，再空则 UTC
	Timezone string
	// TTL 导出文件的有效期；0 或超过平台上限时取平台配置
	TTL time.Duration
}

// ReceiptExport 一次凭证导出的结果。
//
// JSON 里保留 billId / downloadUrl 这两个旧键名：下载路由
// /api/pay/bills/:billId/download 已经发出去了，改键名会让在用的客户端下不到文件。
type ReceiptExport struct {
	BillID       string `json:"billId"`
	OrderNo      string `json:"orderNo"`
	FileName     string `json:"fileName"`
	DownloadURL  string `json:"downloadUrl"`
	DocumentType string `json:"documentType"`

	// Locale 凭证实际使用的语言
	Locale string `json:"locale"`
	// RequestedLocale 协商到的语言。与 Locale 不同即表示发生了降级。
	RequestedLocale string `json:"requestedLocale,omitempty"`
	// LocaleFallback 是否因缺少字体而降级为默认语言
	LocaleFallback bool `json:"localeFallback,omitempty"`
	// DegradedGlyphs 凭证上未能正确渲染的字符。非空说明有内容被替换成了占位符，
	// 客户端应当提示用户换一种语言或联系管理员安装字体。
	DegradedGlyphs string `json:"degradedGlyphs,omitempty"`

	Currency  string    `json:"currency,omitempty"`
	Pages     int       `json:"pages"`
	Size      int       `json:"size"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// BillExport 旧名保留：早期接口以此命名，服务与处理器仍在按这个名字组织代码。
type BillExport = ReceiptExport

// OrderReceipt 挂在订单上的凭证入口。
//
// 之所以由服务端算好而不是让客户端自己拼地址：凭证类型随订单状态变化
// （未支付出的是账单不是收据），能不能寄送取决于用户有没有绑邮箱、
// 邮件通道通不通。这些判断放到客户端就会各端各写一套，而且很快就会不一致。
type OrderReceipt struct {
	// Available 该订单能否出具凭证
	Available bool `json:"available"`
	// DocumentType 当前会出具哪种凭证：receipt / invoice / credit_note
	DocumentType string `json:"documentType"`
	// Locale 按当前请求协商出的推荐语言
	Locale string `json:"locale"`
	// Currency 凭证上的币种
	Currency string `json:"currency,omitempty"`
	// DownloadURL 直接取 PDF
	DownloadURL string `json:"downloadUrl,omitempty"`
	// ExportURL 生成可分享的一次性下载凭据
	ExportURL string `json:"exportUrl,omitempty"`
	// EmailURL 寄送到账号邮箱
	EmailURL string `json:"emailUrl,omitempty"`
	// Emailable 能否寄送（账号已绑邮箱且邮件出口可用）
	Emailable bool `json:"emailable"`
	// EmailHint 不能寄送时的原因，直接可展示给用户
	EmailHint string `json:"emailHint,omitempty"`
}

// UserOrderView 用户侧订单视图：订单本体 + 凭证入口。
// Order 内嵌，JSON 上是扁平的，因此对既有客户端是纯增量。
type UserOrderView struct {
	Order
	Receipt OrderReceipt `json:"receipt"`
}

// UserOrderListResult 用户侧订单分页
type UserOrderListResult struct {
	Items      []UserOrderView `json:"items"`
	Page       int             `json:"page"`
	Limit      int             `json:"limit"`
	Total      int64           `json:"total"`
	TotalPages int             `json:"totalPages"`
}

// ReceiptCapability 凭证能力自述，供控制台与运维核对「这台机器能不能出中文凭证」。
type ReceiptCapability struct {
	// Locales 支持的语言
	Locales []ReceiptLocale `json:"locales"`
	// DefaultLocale 默认语言
	DefaultLocale string `json:"defaultLocale"`
	// SupportsCJK 当前环境是否具备中日韩字形能力
	SupportsCJK bool `json:"supportsCJK"`
	// FontStatus 字体解析结果的一行描述
	FontStatus string `json:"fontStatus"`
	// FontNotes 字体解析过程中的诊断信息（缺字体、.otf 不支持等）
	FontNotes []string `json:"fontNotes,omitempty"`
}

// ReceiptLocale 一个可选语言。
type ReceiptLocale struct {
	Tag        string `json:"tag"`
	Name       string `json:"name"`
	NativeName string `json:"nativeName"`
	Direction  string `json:"direction"`
	Script     string `json:"script"`
	Default    bool   `json:"default"`
	// NeedsFont 该语言需要中日韩字体；当前环境不具备时选它会被降级为默认语言
	NeedsFont bool `json:"needsFont"`
	// Available 当前环境能否真正输出该语言
	Available bool `json:"available"`
}
