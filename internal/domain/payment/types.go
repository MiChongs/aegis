package payment

import (
	"time"

	"github.com/shopspring/decimal"
)

// ── 支付方式标识常量 ──

const (
	MethodEpay         = "epay"          // 易支付（通用）
	MethodRainbowEpay  = "rainbow_epay"  // 彩虹易支付
	MethodXunhupay     = "xunhupay"      // 虎皮椒
	MethodPayjs        = "payjs"         // PAYJS
	MethodQRPay        = "qrpay"         // 码支付
	MethodVMQPay       = "vmqpay"        // V免签
	MethodAlipayNative = "alipay_native" // 支付宝原生
	MethodWechatNative = "wechat_native" // 微信支付原生
	MethodStripe       = "stripe"        // Stripe
	MethodPaypal       = "paypal"        // PayPal
	MethodBalance      = "balance"       // 余额支付（内部钱包，无第三方）

	MethodPaddle       = "paddle"       // Paddle Billing（MoR 记录商户）
	MethodLemonSqueezy = "lemonsqueezy" // Lemon Squeezy（MoR 记录商户）
	MethodRazorpay     = "razorpay"     // Razorpay（印度）
	MethodCoinbase     = "coinbase"     // Coinbase Commerce（加密货币）
	MethodSquare       = "square"       // Square（北美线上收单）
)

// ── 渠道分组（前端渠道市场按此归类）──

const (
	CategoryInternal      = "internal"      // 内部通道（无第三方）
	CategoryAggregate     = "aggregate"     // 聚合/第四方（易支付系）
	CategoryOfficialCN    = "official_cn"   // 国内官方直连
	CategoryInternational = "international" // 国际收单
	CategoryCrypto        = "crypto"        // 加密货币
)

// ── 提供商配置字段类型（驱动前端动态表单渲染）──

const (
	FieldText     = "text"     // 单行文本
	FieldSecret   = "secret"   // 密文（前端掩码 + 可见性切换）
	FieldNumber   = "number"   // 数值
	FieldSwitch   = "switch"   // 布尔开关
	FieldSelect   = "select"   // 单选下拉
	FieldTextarea = "textarea" // 多行文本（证书 / 私钥）
	FieldTags     = "tags"     // 字符串数组（逗号分隔录入）
	FieldURL      = "url"      // URL 文本
)

// ── 字段分组（前端表单分区渲染顺序即声明顺序）──

const (
	GroupCredential = "credential" // 商户凭据
	GroupGateway    = "gateway"    // 网关与回调
	GroupLimit      = "limit"      // 交易限额与风控
	GroupAdvanced   = "advanced"   // 高级选项
)

// ── 订单履约（purpose）──
// 订单创建时在 metadata 中固化 purpose 及其快照参数，支付成功后按 purpose 自动履约。

const (
	// PurposeWalletRecharge 余额充值：支付金额 1:1 入账钱包
	PurposeWalletRecharge = "wallet_recharge"
	// PurposeVipPurchase VIP 直购：按下单时锁定的套餐快照开通
	PurposeVipPurchase = "vip_purchase"
	// PurposeIntegralPurchase 积分直购：按下单时锁定的积分数量发放
	PurposeIntegralPurchase = "integral_purchase"
)

// 履约状态（payment_orders.fulfillment_status）
const (
	FulfillmentNone = "none" // 未履约（或无需履约的普通商品订单）
	FulfillmentDone = "done" // 已履约
)

// metadata 中的字段键
const (
	MetaKeyPurpose        = "purpose"
	MetaKeyVipPlanID      = "vipPlanId"
	MetaKeyVipPlanName    = "vipPlanName"
	MetaKeyVipDays        = "vipDurationDays"
	MetaKeyVipBonus       = "vipBonusIntegral"
	MetaKeyIntegralAmount = "integralAmount"
)

// FulfillmentInstruction 履约指令：服务层从订单 metadata 快照解析而来，
// 仓储层据此在单事务中完成「抢占履约权 + 发放」
type FulfillmentInstruction struct {
	Purpose        string
	WalletAmount   decimal.Decimal // wallet_recharge：入账金额
	VipPlanID      *int64          // vip_purchase：套餐快照
	VipPlanName    string
	VipDays        int
	VipBonus       int64
	IntegralAmount int64 // integral_purchase：发放积分
}

// ── 提供商元数据 ──

// FieldOption 下拉选项
type FieldOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// ConfigField 单个配置项的声明式描述。
// 前端据此动态渲染表单，因此**新增支付渠道时前端无需改动任何代码**。
type ConfigField struct {
	Key         string        `json:"key"`
	Label       string        `json:"label"`
	Type        string        `json:"type"`
	Group       string        `json:"group,omitempty"`
	Required    bool          `json:"required,omitempty"`
	Placeholder string        `json:"placeholder,omitempty"`
	Help        string        `json:"help,omitempty"`
	Default     any           `json:"default,omitempty"`
	Options     []FieldOption `json:"options,omitempty"`
	Advanced    bool          `json:"advanced,omitempty"`
}

// PayTypeOption 子支付类型（下单时的 provider_type）
type PayTypeOption struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// ProviderCapabilities 渠道能力矩阵（前端以能力标签可视化）
type ProviderCapabilities struct {
	Redirect         bool `json:"redirect"`         // 支持跳转收银台
	QRCode           bool `json:"qrcode"`           // 支持二维码/扫码
	Webhook          bool `json:"webhook"`          // 支持异步回调
	WebhookSignature bool `json:"webhookSignature"` // 回调带签名校验
	RemoteQuery      bool `json:"remoteQuery"`      // 支持向上游主动查单
	Sandbox          bool `json:"sandbox"`          // 支持沙箱环境
	SubMerchant      bool `json:"subMerchant"`      // 支持服务商/子商户
	Refund           bool `json:"refund"`           // 支持发起退款（须与 paymentRefunder 实现一致）
	PartialRefund    bool `json:"partialRefund"`    // 支持部分退款（false 表示仅能整单退）
	RefundQuery      bool `json:"refundQuery"`      // 支持向上游查询退款单状态
}

// ProviderMeta 提供商完整描述信息。
// 由各 Provider 的 Describe() 自描述，经 /payment-config/methods 下发给控制台，
// 驱动「渠道市场卡片 + 动态配置表单 + 回调地址提示」三处 UI。
type ProviderMeta struct {
	Method       string               `json:"method"`
	Name         string               `json:"name"`
	Description  string               `json:"description,omitempty"`
	Category     string               `json:"category,omitempty"`
	CategoryName string               `json:"categoryName,omitempty"`
	Icon         string               `json:"icon,omitempty"`       // Simple Icons slug
	BrandColor   string               `json:"brandColor,omitempty"` // #RRGGBB
	DocURL       string               `json:"docUrl,omitempty"`
	CallbackPath string               `json:"callbackPath,omitempty"` // 回调地址路径（前端拼接域名后一键复制）
	CallbackNote string               `json:"callbackNote,omitempty"`
	Regions      []string             `json:"regions,omitempty"`
	Currencies   []string             `json:"currencies,omitempty"`
	Capabilities ProviderCapabilities `json:"capabilities"`
	PayTypes     []PayTypeOption      `json:"payTypes,omitempty"`
	Fields       []ConfigField        `json:"fields,omitempty"`

	// SupportedTypes 兼容旧客户端的扁平子类型列表（等价于 PayTypes 的 Value 集合）
	SupportedTypes []string `json:"supportedTypes,omitempty"`
}

type Config struct {
	ID            int64          `json:"id"`
	AppID         int64          `json:"appid"`
	PaymentMethod string         `json:"payment_method"`
	ConfigName    string         `json:"config_name"`
	ConfigData    map[string]any `json:"config_data"`
	Enabled       bool           `json:"enabled"`
	IsDefault     bool           `json:"is_default"`
	Description   string         `json:"description,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

type ConfigMutation struct {
	ID            int64
	AppID         int64
	PaymentMethod *string
	ConfigName    *string
	ConfigData    map[string]any
	Enabled       *bool
	IsDefault     *bool
	Description   *string
}

type EpayConfig struct {
	PID            string   `json:"pid"`
	Key            string   `json:"key"`
	APIURL         string   `json:"apiUrl"`
	SiteName       string   `json:"sitename"`
	NotifyURL      string   `json:"notifyUrl"`
	ReturnURL      string   `json:"returnUrl"`
	SignType       string   `json:"signType"`
	SupportedTypes []string `json:"supportedTypes"`
	ExpireMinutes  int      `json:"expireMinutes"`
	MinAmount      float64  `json:"minAmount"`
	MaxAmount      float64  `json:"maxAmount"`
	AllowedIPs     []string `json:"allowedIPs"`
	VerifyIP       bool     `json:"verifyIP"`
}

// RainbowEpayConfig 彩虹易支付（与 EpayConfig 结构兼容）
type RainbowEpayConfig = EpayConfig

// XunhupayConfig 虎皮椒支付配置
type XunhupayConfig struct {
	AppID     string  `json:"appId"`
	AppSecret string  `json:"appSecret"`
	APIURL    string  `json:"apiUrl"`   // 默认 https://api.xunhupay.com/payment/do.html
	WxpayURL  string  `json:"wxpayUrl"` // 微信支付独立网关（可选）
	NotifyURL string  `json:"notifyUrl"`
	ReturnURL string  `json:"returnUrl"`
	MinAmount float64 `json:"minAmount"`
	MaxAmount float64 `json:"maxAmount"`
}

// PayjsConfig PAYJS 微信支付配置
type PayjsConfig struct {
	MchID     string  `json:"mchId"`
	Key       string  `json:"key"`
	APIURL    string  `json:"apiUrl"` // 默认 https://payjs.cn/api
	NotifyURL string  `json:"notifyUrl"`
	ReturnURL string  `json:"returnUrl"`
	MinAmount float64 `json:"minAmount"`
	MaxAmount float64 `json:"maxAmount"`
}

// QRPayConfig 码支付配置
type QRPayConfig struct {
	UID       string  `json:"uid"`
	Token     string  `json:"token"`
	APIURL    string  `json:"apiUrl"`
	NotifyURL string  `json:"notifyUrl"`
	ReturnURL string  `json:"returnUrl"`
	MinAmount float64 `json:"minAmount"`
	MaxAmount float64 `json:"maxAmount"`
}

// VMQPayConfig V免签配置
type VMQPayConfig struct {
	APIURL    string  `json:"apiUrl"` // V免签服务地址
	Key       string  `json:"key"`    // 通信密钥
	NotifyURL string  `json:"notifyUrl"`
	ReturnURL string  `json:"returnUrl"`
	MinAmount float64 `json:"minAmount"`
	MaxAmount float64 `json:"maxAmount"`
}

// AlipayNativeConfig 支付宝原生支付配置
type AlipayNativeConfig struct {
	AppID           string  `json:"appId"`
	PrivateKey      string  `json:"privateKey"`
	AlipayPublicKey string  `json:"alipayPublicKey"`
	AppCertPath     string  `json:"appCertPath,omitempty"`
	AlipayCertPath  string  `json:"alipayCertPath,omitempty"`
	RootCertPath    string  `json:"rootCertPath,omitempty"`
	CertMode        bool    `json:"certMode"`
	IsSandbox       bool    `json:"isSandbox"`
	SignType        string  `json:"signType"` // RSA2
	NotifyURL       string  `json:"notifyUrl"`
	ReturnURL       string  `json:"returnUrl"`
	MinAmount       float64 `json:"minAmount"`
	MaxAmount       float64 `json:"maxAmount"`
}

// WechatNativeConfig 微信支付原生配置
type WechatNativeConfig struct {
	AppID      string  `json:"appId"`
	MchID      string  `json:"mchId"`
	APIKey     string  `json:"apiKey"`     // v2 API 密钥
	APIKeyV3   string  `json:"apiKeyV3"`   // v3 API 密钥
	SerialNo   string  `json:"serialNo"`   // 证书序列号
	PrivateKey string  `json:"privateKey"` // 商户私钥
	IsSandbox  bool    `json:"isSandbox"`
	SubAppID   string  `json:"subAppId,omitempty"` // 服务商子商户 AppID
	SubMchID   string  `json:"subMchId,omitempty"` // 服务商子商户号
	NotifyURL  string  `json:"notifyUrl"`
	ReturnURL  string  `json:"returnUrl"`
	MinAmount  float64 `json:"minAmount"`
	MaxAmount  float64 `json:"maxAmount"`
}

// StripeConfig Stripe 支付配置
type StripeConfig struct {
	SecretKey      string  `json:"secretKey"`
	PublishableKey string  `json:"publishableKey"`
	WebhookSecret  string  `json:"webhookSecret"`
	Currency       string  `json:"currency"` // 默认 usd
	NotifyURL      string  `json:"notifyUrl"`
	ReturnURL      string  `json:"returnUrl"`
	CancelURL      string  `json:"cancelUrl"`
	MinAmount      float64 `json:"minAmount"`
	MaxAmount      float64 `json:"maxAmount"`
}

// PaypalConfig PayPal 支付配置
type PaypalConfig struct {
	ClientID     string  `json:"clientId"`
	ClientSecret string  `json:"clientSecret"`
	WebhookID    string  `json:"webhookId"` // Webhook 验签所需（PayPal 后台创建 Webhook 后获得）
	IsSandbox    bool    `json:"isSandbox"`
	Currency     string  `json:"currency"` // 默认 USD
	NotifyURL    string  `json:"notifyUrl"`
	ReturnURL    string  `json:"returnUrl"`
	CancelURL    string  `json:"cancelUrl"`
	MinAmount    float64 `json:"minAmount"`
	MaxAmount    float64 `json:"maxAmount"`
}

// PaddleConfig Paddle Billing（v2）配置。
// Paddle 是 MoR（Merchant of Record）模式：由 Paddle 代收并处理税务合规。
type PaddleConfig struct {
	APIKey        string  `json:"apiKey"`        // Paddle API Key（pdl_live_… / pdl_sdbx_…）
	PriceID       string  `json:"priceId"`       // 计价用 Price ID（pri_…）；自定义金额时作为基准价
	WebhookSecret string  `json:"webhookSecret"` // Notification Setting 的签名密钥（pdl_ntfset_…）
	IsSandbox     bool    `json:"isSandbox"`
	Currency      string  `json:"currency"` // 默认 USD
	NotifyURL     string  `json:"notifyUrl"`
	ReturnURL     string  `json:"returnUrl"`
	MinAmount     float64 `json:"minAmount"`
	MaxAmount     float64 `json:"maxAmount"`
}

// LemonSqueezyConfig Lemon Squeezy 配置（MoR 模式）
type LemonSqueezyConfig struct {
	APIKey        string  `json:"apiKey"`        // API Key（Bearer）
	StoreID       string  `json:"storeId"`       // 店铺 ID
	VariantID     string  `json:"variantId"`     // 商品变体 ID
	WebhookSecret string  `json:"webhookSecret"` // Webhook 签名密钥（X-Signature HMAC-SHA256）
	TestMode      bool    `json:"testMode"`
	Currency      string  `json:"currency"` // 默认 USD
	NotifyURL     string  `json:"notifyUrl"`
	ReturnURL     string  `json:"returnUrl"`
	MinAmount     float64 `json:"minAmount"`
	MaxAmount     float64 `json:"maxAmount"`
}

// RazorpayConfig Razorpay 配置（印度主流收单）
type RazorpayConfig struct {
	KeyID         string  `json:"keyId"`         // rzp_live_… / rzp_test_…
	KeySecret     string  `json:"keySecret"`     // API Secret（Basic Auth 密码位）
	WebhookSecret string  `json:"webhookSecret"` // Webhook 密钥（X-Razorpay-Signature）
	Currency      string  `json:"currency"`      // 默认 INR
	NotifyURL     string  `json:"notifyUrl"`
	ReturnURL     string  `json:"returnUrl"`
	MinAmount     float64 `json:"minAmount"`
	MaxAmount     float64 `json:"maxAmount"`
}

// CoinbaseConfig Coinbase Commerce 配置（加密货币收款）
type CoinbaseConfig struct {
	APIKey        string  `json:"apiKey"`        // X-CC-Api-Key
	WebhookSecret string  `json:"webhookSecret"` // Webhook 共享密钥（X-CC-Webhook-Signature）
	Currency      string  `json:"currency"`      // 计价法币，默认 USD
	NotifyURL     string  `json:"notifyUrl"`
	ReturnURL     string  `json:"returnUrl"`
	CancelURL     string  `json:"cancelUrl"`
	MinAmount     float64 `json:"minAmount"`
	MaxAmount     float64 `json:"maxAmount"`
}

// SquareConfig Square 配置（Payment Links + Webhook）
type SquareConfig struct {
	AccessToken   string  `json:"accessToken"`   // Bearer Token
	LocationID    string  `json:"locationId"`    // 收款门店 ID
	WebhookSecret string  `json:"webhookSecret"` // Webhook 签名密钥
	WebhookURL    string  `json:"webhookUrl"`    // 验签需与后台登记地址逐字符一致
	IsSandbox     bool    `json:"isSandbox"`
	Currency      string  `json:"currency"` // 默认 USD
	NotifyURL     string  `json:"notifyUrl"`
	ReturnURL     string  `json:"returnUrl"`
	MinAmount     float64 `json:"minAmount"`
	MaxAmount     float64 `json:"maxAmount"`
}

// ── 退款 ──

// 退款单状态（payment_refunds.status）
const (
	RefundPending    = "pending"    // 已创建，尚未提交上游
	RefundProcessing = "processing" // 上游已受理，结果待异步返回（微信/支付宝可能走此态）
	RefundSuccess    = "success"    // 退款成功
	RefundFailed     = "failed"     // 退款失败（额度已释放，可重新发起）
	RefundClosed     = "closed"     // 退款关闭（上游终态失败，不再重试）
)

// 订单退款汇总状态（payment_orders.refund_status）
const (
	OrderRefundNone    = "none"    // 未退款
	OrderRefundPartial = "partial" // 部分退款
	OrderRefundFull    = "full"    // 全额退款
)

// 履约冲正状态（payment_refunds.reversal_status）
const (
	ReversalNone    = "none"    // 订单本身无履约，无需冲正
	ReversalDone    = "done"    // 已冲正
	ReversalSkipped = "skipped" // 调用方显式要求不冲正
	ReversalFailed  = "failed"  // 冲正失败（如余额已被消费），需人工处理
)

// RefundStatusIsFinal 是否为终态（终态不再轮询上游）
func RefundStatusIsFinal(status string) bool {
	switch status {
	case RefundSuccess, RefundFailed, RefundClosed:
		return true
	default:
		return false
	}
}

// Refund 退款单
type Refund struct {
	ID               int64           `json:"id"`
	AppID            int64           `json:"appid"`
	OrderID          int64           `json:"order_id"`
	OrderNo          string          `json:"order_no"`
	RefundNo         string          `json:"refund_no"`
	ProviderRefundNo string          `json:"provider_refund_no,omitempty"`
	UserID           *int64          `json:"user_id,omitempty"`
	PaymentMethod    string          `json:"payment_method"`
	Amount           decimal.Decimal `json:"amount"`
	Reason           string          `json:"reason,omitempty"`
	Status           string          `json:"status"`
	ReversalStatus   string          `json:"reversal_status"`
	ReversalMessage  string          `json:"reversal_message,omitempty"`
	Operator         string          `json:"operator,omitempty"`
	ClientIP         string          `json:"client_ip,omitempty"`
	ErrorMessage     string          `json:"error_message,omitempty"`
	RawResponse      map[string]any  `json:"raw_response,omitempty"`
	RefundedAt       *time.Time      `json:"refunded_at,omitempty"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
}

// RefundCreation 退款单创建入参（仓储层在单事务内校验可退额度并落库）
type RefundCreation struct {
	AppID    int64
	Order    *Order
	RefundNo string
	Amount   decimal.Decimal
	Reason   string
	Operator string
	ClientIP string
}

// RefundSettlement 退款单结算入参（提交上游后回写结果）
type RefundSettlement struct {
	RefundID         int64
	Status           string
	ProviderRefundNo string
	ErrorMessage     string
	RawResponse      map[string]any
}

// RefundRequest 向上游发起退款的统一请求
type RefundRequest struct {
	AppID           int64
	OrderNo         string // 商户订单号
	ProviderOrderNo string // 上游支付单号（Stripe PaymentIntent / PayPal Capture 等）
	RefundNo        string // 商户退款单号（上游的 out_refund_no）
	Reason          string
	RefundAmount    decimal.Decimal // 本次退款金额
	TotalAmount     decimal.Decimal // 原订单总额（微信等要求同时提交）
	Currency        string
	NotifyURL       string
	Metadata        map[string]any
}

// RefundQuery 向上游查询退款单状态的统一请求
type RefundQuery struct {
	OrderNo          string
	ProviderOrderNo  string
	RefundNo         string
	ProviderRefundNo string
}

// RefundResult 上游退款结果
type RefundResult struct {
	Status           string          `json:"status"` // success / processing / failed / closed
	ProviderRefundNo string          `json:"provider_refund_no,omitempty"`
	Amount           decimal.Decimal `json:"amount"`
	Message          string          `json:"message,omitempty"`
	Raw              map[string]any  `json:"raw,omitempty"`
}

// RefundListQuery 退款单分页查询条件
type RefundListQuery struct {
	Status  string
	Method  string
	Keyword string
	Page    int
	Limit   int
}

// RefundListResult 退款单分页结果
type RefundListResult struct {
	Items      []Refund `json:"items"`
	Page       int      `json:"page"`
	Limit      int      `json:"limit"`
	Total      int64    `json:"total"`
	TotalPages int      `json:"totalPages"`
}

// RefundableInfo 订单可退款额度视图
type RefundableInfo struct {
	OrderNo        string          `json:"order_no"`
	PaymentMethod  string          `json:"payment_method"`
	Amount         decimal.Decimal `json:"amount"`
	RefundedAmount decimal.Decimal `json:"refunded_amount"`
	Refundable     decimal.Decimal `json:"refundable"`
	RefundStatus   string          `json:"refund_status"`
	Supported      bool            `json:"supported"`       // 渠道是否支持退款
	PartialAllowed bool            `json:"partial_allowed"` // 渠道是否支持部分退款
	Reason         string          `json:"reason,omitempty"`
}

type Order struct {
	ID              int64           `json:"id"`
	AppID           int64           `json:"appid"`
	UserID          *int64          `json:"user_id,omitempty"`
	ConfigID        int64           `json:"config_id"`
	OrderNo         string          `json:"order_no"`
	ProviderOrderNo string          `json:"provider_order_no,omitempty"`
	Subject         string          `json:"subject"`
	Body            string          `json:"body,omitempty"`
	Amount          decimal.Decimal `json:"amount"`
	// Currency ISO 4217 币种，下单时按渠道配置固化。历史订单为空串，
	// 开凭证时按渠道推断 —— 配置随时会改，已发生的交易不能跟着改。
	Currency      string `json:"currency,omitempty"`
	PaymentMethod string `json:"payment_method"`
	ProviderType    string          `json:"provider_type"`
	Status          string          `json:"status"`
	NotifyStatus    string          `json:"notify_status"`
	ClientIP        string          `json:"client_ip,omitempty"`
	NotifyURL       string          `json:"notify_url,omitempty"`
	ReturnURL       string          `json:"return_url,omitempty"`
	Metadata        map[string]any  `json:"metadata,omitempty"`
	RawCallback     map[string]any  `json:"raw_callback,omitempty"`
	RefundedAmount  decimal.Decimal `json:"refunded_amount"`
	RefundStatus    string          `json:"refund_status"`
	PaidAt          *time.Time      `json:"paid_at,omitempty"`
	ExpireAt        *time.Time      `json:"expire_at,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
}

type OrderListQuery struct {
	Status string `json:"status"`
	Page   int    `json:"page"`
	Limit  int    `json:"limit"`
}

type OrderListResult struct {
	Items      []Order `json:"items"`
	Page       int     `json:"page"`
	Limit      int     `json:"limit"`
	Total      int64   `json:"total"`
	TotalPages int     `json:"totalPages"`
}

// 凭证导出相关类型见 receipt.go

type OrderMutation struct {
	AppID         int64
	UserID        *int64
	ConfigID      int64
	OrderNo       string
	Subject       string
	Body          string
	Amount        decimal.Decimal
	Currency      string
	PaymentMethod string
	ProviderType  string
	ClientIP      string
	NotifyURL     string
	ReturnURL     string
	Metadata      map[string]any
	ExpireAt      *time.Time
}

type PaymentPayload struct {
	Success      bool           `json:"success"`
	OrderNo      string         `json:"order_no"`
	PaymentURL   string         `json:"payment_url,omitempty"`
	RedirectURL  string         `json:"redirect_url,omitempty"`
	HTML         string         `json:"html,omitempty"`
	FormData     map[string]any `json:"form_data,omitempty"`
	Message      string         `json:"message,omitempty"`
	ProviderType string         `json:"provider_type,omitempty"`
}

type CallbackResult struct {
	Success         bool            `json:"success"`
	Paid            bool            `json:"paid"`
	OrderNo         string          `json:"order_no"`
	ProviderOrderNo string          `json:"provider_order_no,omitempty"`
	TradeStatus     string          `json:"trade_status,omitempty"`
	PaymentMethod   string          `json:"payment_method,omitempty"`
	Amount          decimal.Decimal `json:"amount"`
	Message         string          `json:"message,omitempty"`
	RawData         map[string]any  `json:"raw_data,omitempty"`
}
