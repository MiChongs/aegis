package service

import (
	"context"
	"net/http"
	"strings"

	paymentdomain "aegis/internal/domain/payment"
	apperrors "aegis/pkg/errors"

	"github.com/go-resty/resty/v2"
)

// ── Stripe Provider ──
// 注：完整的 Stripe 对接需要引入官方 SDK（github.com/stripe/stripe-go/v78）。
// 当前实现为配置验证 + 占位逻辑，实际支付对接后续根据需求完善。

type stripeProvider struct {
	client *resty.Client
}

func newStripeProvider(client *resty.Client) *stripeProvider {
	return &stripeProvider{client: client}
}

func (p *stripeProvider) Name() string  { return paymentdomain.MethodStripe }
func (p *stripeProvider) Label() string { return "Stripe" }

func (p *stripeProvider) ValidateConfig(data map[string]any) error {
	cfg, err := decodeProviderConfig[paymentdomain.StripeConfig](data)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.SecretKey) == "" {
		return apperrors.New(40078, http.StatusBadRequest, "Stripe 配置不完整：缺少 secretKey")
	}
	if strings.TrimSpace(cfg.PublishableKey) == "" {
		return apperrors.New(40078, http.StatusBadRequest, "Stripe 配置不完整：缺少 publishableKey")
	}
	return nil
}

func (p *stripeProvider) TestConnection(ctx context.Context, data map[string]any) (map[string]any, error) {
	cfg, err := decodeProviderConfig[paymentdomain.StripeConfig](data)
	if err != nil {
		return map[string]any{"config_valid": false, "error": err.Error()}, nil
	}
	// 使用 REST API 测试 balance 端点
	resp, err := p.client.R().SetContext(ctx).
		SetHeader("Authorization", "Bearer "+cfg.SecretKey).
		Get("https://api.stripe.com/v1/balance")
	if err != nil {
		return map[string]any{"config_valid": true, "api_accessible": false, "error": err.Error()}, nil
	}
	return map[string]any{"config_valid": true, "api_accessible": resp.IsSuccess(), "status": resp.StatusCode()}, nil
}

func (p *stripeProvider) CreateOrder(ctx context.Context, data map[string]any, req PaymentOrderRequest) (*paymentdomain.PaymentPayload, error) {
	return nil, apperrors.New(50180, http.StatusNotImplemented, "Stripe 支付对接尚未完成")
}

func (p *stripeProvider) QueryRemoteOrder(ctx context.Context, data map[string]any, orderNo string) (map[string]any, error) {
	return map[string]any{"message": "Stripe 订单查询尚未实现"}, nil
}

func (p *stripeProvider) HandleCallback(ctx context.Context, data map[string]any, callbackData map[string]string, clientIP string) (*paymentdomain.CallbackResult, error) {
	return nil, apperrors.New(50180, http.StatusNotImplemented, "Stripe Webhook 处理尚未完成")
}

func (p *stripeProvider) SupportedPayTypes() []string {
	return []string{"card", "alipay", "wechat_pay"}
}
