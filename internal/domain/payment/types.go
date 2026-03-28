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
)

// ── 提供商元数据 ──

// ProviderMeta 提供商描述信息（用于前端展示）
type ProviderMeta struct {
	Method         string   `json:"method"`
	Name           string   `json:"name"`
	Description    string   `json:"description,omitempty"`
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
	IsSandbox    bool    `json:"isSandbox"`
	Currency     string  `json:"currency"` // 默认 USD
	NotifyURL    string  `json:"notifyUrl"`
	ReturnURL    string  `json:"returnUrl"`
	CancelURL    string  `json:"cancelUrl"`
	MinAmount    float64 `json:"minAmount"`
	MaxAmount    float64 `json:"maxAmount"`
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
	PaymentMethod   string          `json:"payment_method"`
	ProviderType    string          `json:"provider_type"`
	Status          string          `json:"status"`
	NotifyStatus    string          `json:"notify_status"`
	ClientIP        string          `json:"client_ip,omitempty"`
	NotifyURL       string          `json:"notify_url,omitempty"`
	ReturnURL       string          `json:"return_url,omitempty"`
	Metadata        map[string]any  `json:"metadata,omitempty"`
	RawCallback     map[string]any  `json:"raw_callback,omitempty"`
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

type BillExport struct {
	BillID      string    `json:"billId"`
	OrderNo     string    `json:"orderNo"`
	FileName    string    `json:"fileName"`
	DownloadURL string    `json:"downloadUrl"`
	CreatedAt   time.Time `json:"createdAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

type OrderMutation struct {
	AppID         int64
	UserID        *int64
	ConfigID      int64
	OrderNo       string
	Subject       string
	Body          string
	Amount        decimal.Decimal
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
