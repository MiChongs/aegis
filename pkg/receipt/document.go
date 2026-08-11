// Package receipt 生成多语言的支付凭证 PDF。
//
// 它只认识一个与业务无关的 Document 模型：谁收钱、谁付钱、买了什么、付了多少、
// 退了多少。业务侧（PaymentService）负责把订单、用户、应用、退款单装配成 Document，
// 本包负责把它排成一份任何语言下都读得懂的 A4 凭证。
//
// 两个刻意的设计约束：
//
//   - **默认语言恒为英文**。凭证可能被寄往任何地方，英文是最不会读不懂的兜底；
//     同时英文只需要内嵌的拉丁字体，因此「一份凭证出不来」这种事在任何环境下都不会发生。
//   - **字体在渲染前决定**。先扫一遍整份文档要画哪些字符，再挑能画出它们的字体；
//     挑不出来就降级语言并如实上报，而不是产出一份满是豆腐块的凭证。
package receipt

import (
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// DocType 凭证类型。它决定抬头文案与编号前缀，不改变版式。
type DocType string

const (
	// TypeReceipt 收据：钱已经收到
	TypeReceipt DocType = "receipt"
	// TypeInvoice 账单：钱还没收到，是一份付款要求
	TypeInvoice DocType = "invoice"
	// TypeCreditNote 退款凭证：钱退回去了
	TypeCreditNote DocType = "credit_note"
)

// Status 凭证上展示的交易状态。与订单状态一一对应，但用凭证的措辞表达。
type Status string

const (
	StatusPaid              Status = "paid"
	StatusPending           Status = "pending"
	StatusRefunded          Status = "refunded"
	StatusPartiallyRefunded Status = "partially_refunded"
	StatusExpired           Status = "expired"
	StatusFailed            Status = "failed"
	StatusCancelled         Status = "cancelled"
)

// Party 凭证上的一方（收款方或付款方）。
type Party struct {
	Name      string   // 主名称
	Subtitle  string   // 副标题，如应用标识
	Lines     []string // 自由文本行（地址、税号等）
	Email     string
	Reference string // 客户号 / 商户号
}

// LineItem 一行商品。数量与单价可以为零值 —— 单件商品的凭证不必强行凑出「x1」。
type LineItem struct {
	Name        string
	Description string
	Quantity    decimal.Decimal
	UnitPrice   decimal.Decimal
	Amount      decimal.Decimal
}

// PaymentInfo 支付详情。
type PaymentInfo struct {
	// MethodKey 渠道标识（alipay_native / stripe / …）。译文按 method.<key> 查，
	// 查不到时退回 MethodLabel —— 新增渠道不该因为漏翻译就在凭证上显示成空白。
	MethodKey       string
	MethodLabel     string
	ProviderType    string
	ProviderOrderNo string
	PaidAt          *time.Time
	ClientIP        string
}

// RefundInfo 一笔退款。
type RefundInfo struct {
	Number string
	Amount decimal.Decimal
	// Status 退款单状态键，译文按 refund.status.<status> 查
	Status string
	Reason string
	At     *time.Time
}

// KeyValue 附加信息区的一行。
//
// 标签恒为译文键（这些标签全部由平台生成），值默认是字面量。
// 之所以不用「以 @ 开头即译文键」这类约定：值里混着用户数据，
// 一个叫「@Gold」的套餐名会被当成译文键，翻不到就静默丢掉那个 @。
// 可翻译的值必须显式声明。
type KeyValue struct {
	// Key 标签的译文键；缺译文时露出键名本身，便于发现漏翻
	Key string
	// Value 字面值
	Value string
	// ValueKey 值的译文键（用于枚举值）；非空时取代 Value
	ValueKey string
}

// Note 一条备注。Key 非空时取译文，否则直接展示 Text。
type Note struct {
	Key  string
	Text string
}

// Document 一份凭证的完整内容。
type Document struct {
	Type   DocType
	Status Status

	// Number 凭证编号；空则由渲染器按订单号派生
	Number   string
	OrderNo  string
	IssuedAt time.Time

	Issuer   Party
	Customer Party

	// Currency ISO 4217 代码。空串时金额只显示数字，不带符号 ——
	// 与其猜一个币种印在凭证上，不如不印。
	Currency string

	Items []LineItem
	// Discount 折扣金额（正数表示减免）
	Discount decimal.Decimal
	// TaxRate 税率（0.06 表示 6%），仅用于展示标签
	TaxRate decimal.Decimal
	// TaxAmount 税额
	TaxAmount decimal.Decimal
	// Total 应付总额
	Total decimal.Decimal
	// RefundedTotal 已退金额
	RefundedTotal decimal.Decimal

	Payment PaymentInfo
	Refunds []RefundInfo

	// Notes 附注，每项一行
	Notes []Note
	// Metadata 附加信息表（订单 metadata、履约快照等）
	Metadata []KeyValue

	// Brand 抬头处的品牌名，通常是平台名
	Brand string
	// FooterNote 页脚补充说明；空则用译文里的默认免责声明
	FooterNote string
	// VerifyURL 核验地址，展示在页脚
	VerifyURL string
}

// Subtotal 行项目合计。没有行项目时退回总额 —— 凭证不该出现「小计 0，合计 100」。
func (d *Document) Subtotal() decimal.Decimal {
	if len(d.Items) == 0 {
		return d.Total.Add(d.Discount).Sub(d.TaxAmount)
	}
	sum := decimal.Zero
	for _, item := range d.Items {
		sum = sum.Add(item.Amount)
	}
	return sum
}

// NetPaid 实收净额 = 应付总额 - 已退金额。
func (d *Document) NetPaid() decimal.Decimal {
	return d.Total.Sub(d.RefundedTotal)
}

// HasRefunds 是否需要展示退款区块。
func (d *Document) HasRefunds() bool {
	return len(d.Refunds) > 0 || d.RefundedTotal.IsPositive()
}

// texts 收集整份文档里要绘制的**数据**文本（不含译文）。
// 字体选择据此判断「这份凭证需要什么字」，因此凡是会画到纸上的用户数据都要在这里出现，
// 漏一个字段就会在那个字段上出现豆腐块。
func (d *Document) texts() []string {
	out := []string{
		d.Number, d.OrderNo, d.Brand, d.FooterNote, d.VerifyURL, d.Currency,
		d.Issuer.Name, d.Issuer.Subtitle, d.Issuer.Email, d.Issuer.Reference,
		d.Customer.Name, d.Customer.Subtitle, d.Customer.Email, d.Customer.Reference,
		d.Payment.MethodLabel, d.Payment.ProviderType, d.Payment.ProviderOrderNo, d.Payment.ClientIP,
	}
	out = append(out, d.Issuer.Lines...)
	out = append(out, d.Customer.Lines...)
	for _, note := range d.Notes {
		out = append(out, note.Text)
	}
	for _, item := range d.Items {
		out = append(out, item.Name, item.Description)
	}
	for _, refund := range d.Refunds {
		out = append(out, refund.Number, refund.Reason)
	}
	for _, kv := range d.Metadata {
		out = append(out, kv.Value)
	}
	return out
}

// normalize 补齐可推导的字段，让渲染器不必到处判空。
func (d *Document) normalize() {
	if d.Type == "" {
		d.Type = TypeReceipt
	}
	if d.Status == "" {
		d.Status = StatusPaid
	}
	if d.IssuedAt.IsZero() {
		d.IssuedAt = time.Now().UTC()
	}
	if strings.TrimSpace(d.Number) == "" {
		d.Number = d.OrderNo
	}
	if d.Currency != "" {
		d.Currency = strings.ToUpper(strings.TrimSpace(d.Currency))
	}
}
