package service

import (
	"context"
	"time"

	paymentdomain "aegis/internal/domain/payment"

	"github.com/shopspring/decimal"
)

// paymentProvider 支付提供商统一接口
type paymentProvider interface {
	// Name 返回提供商标识（与 paymentdomain.Method* 常量对应）
	Name() string

	// Label 返回提供商中文名称
	Label() string

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

	// SupportedPayTypes 该提供商支持的子支付类型
	SupportedPayTypes() []string
}

// PaymentOrderRequest 统一创建订单请求
type PaymentOrderRequest struct {
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
