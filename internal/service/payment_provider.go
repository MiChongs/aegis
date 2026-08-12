package service

import (
	"context"
	"strings"
	"time"

	paymentdomain "aegis/internal/domain/payment"

	"github.com/shopspring/decimal"
)

// ── 回调保留键 ──
// 传输层把 JSON Webhook（Stripe / 微信 / PayPal）的原始报文与签名头注入 callbackData，
// 提供商据此完成验签；表单类回调（易支付系 / 支付宝）不注入，避免污染其签名参数集。
const (
	CallbackRawBodyKey   = "__raw_body"
	CallbackHeaderPrefix = "__header__"
)

// CallbackSignatureHeaders 需要透传给提供商的签名相关请求头
var CallbackSignatureHeaders = []string{
	"Stripe-Signature",
	"Wechatpay-Signature", "Wechatpay-Serial", "Wechatpay-Timestamp", "Wechatpay-Nonce", "Wechatpay-Signature-Type",
	"Paypal-Transmission-Id", "Paypal-Transmission-Time", "Paypal-Transmission-Sig", "Paypal-Cert-Url", "Paypal-Auth-Algo",
	"Paddle-Signature",
	"X-Signature",                   // Lemon Squeezy
	"X-Event-Name",                  // Lemon Squeezy 事件名
	"X-Razorpay-Signature",          // Razorpay
	"X-Cc-Webhook-Signature",        // Coinbase Commerce
	"X-Square-Hmacsha256-Signature", // Square
	"Content-Type",
}

// callbackOrderExtractor 可选能力：Webhook 报文中没有 out_trade_no 表单字段的提供商
// （Stripe / PayPal 的 Webhook 在平台侧全局配置）实现此接口，从原始报文预提取本地订单号。
// 仅用于订单路由定位；签名校验仍在 HandleCallback 内基于配置完成。
type callbackOrderExtractor interface {
	ExtractOrderNo(callbackData map[string]string) string
}

// paymentReturnVerifier 可选能力：能从「同步跳转」（return_url）的查询参数里
// 验出订单归属的渠道实现此接口，返回其中的本地订单号。
//
// 只有把回调参数原样带在 return_url 上、且带签名的渠道做得到 —— 易支付系是这样，
// Stripe / PayPal 的跳转不带任何可验证凭据。**不实现比假装实现好**：
// 结果页宁可说「这条路证实不了，请回到应用内查看」，也不能凭一串谁都能编的
// query 就告诉用户「支付成功」。
type paymentReturnVerifier interface {
	VerifyReturn(configData map[string]any, params map[string]string) (orderNo string, err error)
}

// callbackHeader 读取传输层注入的请求头（大小写不敏感）
func callbackHeader(callbackData map[string]string, name string) string {
	return callbackData[CallbackHeaderPrefix+strings.ToLower(name)]
}

// paymentProvider 支付提供商统一接口
type paymentProvider interface {
	// Name 返回提供商标识（与 paymentdomain.Method* 常量对应）
	Name() string

	// Describe 返回渠道自描述元数据：展示信息、能力矩阵、子支付类型与配置字段 schema。
	// 控制台的渠道市场与动态配置表单完全由此驱动，因此新增渠道时前端无需任何改动。
	Describe() paymentdomain.ProviderMeta

	// ValidateConfig 验证配置数据有效性
	ValidateConfig(data map[string]any) error

	// TestConnection 测试配置连通性
	TestConnection(ctx context.Context, data map[string]any) (map[string]any, error)

	// CreateOrder 创建支付订单
	CreateOrder(ctx context.Context, data map[string]any, req PaymentOrderRequest) (*paymentdomain.PaymentPayload, error)

	// QueryRemoteOrder 向上游查询订单状态
	QueryRemoteOrder(ctx context.Context, data map[string]any, orderNo string) (map[string]any, error)

	// HandleCallback 处理回调数据（验签+解析）
	HandleCallback(ctx context.Context, data map[string]any, callbackData map[string]string, clientIP string) (*paymentdomain.CallbackResult, error)
}

// paymentRefunder 可选能力：支持发起退款的渠道实现此接口。
//
// 是否实现该接口必须与 Describe().Capabilities.Refund 一致 —— 二者由测试强制对齐，
// 避免出现「控制台显示可退款、实际调用时才报不支持」的假象。
type paymentRefunder interface {
	// Refund 向上游发起退款。RefundNo 是商户退款单号，同时充当上游幂等键，
	// 因此同一 RefundNo 重复提交必须只退一笔（各渠道均依赖此语义做重试安全）。
	Refund(ctx context.Context, data map[string]any, req paymentdomain.RefundRequest) (*paymentdomain.RefundResult, error)
}

// refundQuerier 可选能力：支持向上游查询退款单最终状态的渠道实现此接口。
// 用于「上游受理但结果异步返回」（微信/支付宝/Paddle）场景下的补偿轮询。
type refundQuerier interface {
	QueryRefund(ctx context.Context, data map[string]any, query paymentdomain.RefundQuery) (*paymentdomain.RefundResult, error)
}

// providerLabel 取渠道展示名（Describe 的轻量包装，便于日志与错误信息拼接）
func providerLabel(p paymentProvider) string {
	if p == nil {
		return ""
	}
	return p.Describe().Name
}

// PaymentOrderRequest 统一创建订单请求
type PaymentOrderRequest struct {
	AppID        int64 // 应用标识（微信等通知地址禁带查询参数的渠道以路径段携带）
	OrderNo      string
	Subject      string
	Body         string
	Amount       decimal.Decimal
	ProviderType string // 子支付类型：alipay, wxpay 等
	NotifyURL    string
	ReturnURL    string
	ClientIP     string
	Metadata     map[string]any
	ExpireAt     *time.Time
}
