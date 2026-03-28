package service

import (
	"context"
	"net/http"
	"strings"

	paymentdomain "aegis/internal/domain/payment"
	apperrors "aegis/pkg/errors"

	"github.com/go-resty/resty/v2"
)

// ── PayPal Provider ──
// 注：完整的 PayPal 对接使用 REST API v2。
// 当前实现为配置验证 + 占位逻辑，实际支付对接后续根据需求完善。

type paypalProvider struct {
	client *resty.Client
}

func newPaypalProvider(client *resty.Client) *paypalProvider {
	return &paypalProvider{client: client}
}

func (p *paypalProvider) Name() string  { return paymentdomain.MethodPaypal }
func (p *paypalProvider) Label() string { return "PayPal" }

func (p *paypalProvider) ValidateConfig(data map[string]any) error {
	cfg, err := decodeProviderConfig[paymentdomain.PaypalConfig](data)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		return apperrors.New(40078, http.StatusBadRequest, "PayPal 配置不完整：缺少 clientId")
	}
	if strings.TrimSpace(cfg.ClientSecret) == "" {
		return apperrors.New(40078, http.StatusBadRequest, "PayPal 配置不完整：缺少 clientSecret")
	}
	return nil
}

func (p *paypalProvider) TestConnection(ctx context.Context, data map[string]any) (map[string]any, error) {
	cfg, err := decodeProviderConfig[paymentdomain.PaypalConfig](data)
	if err != nil {
		return map[string]any{"config_valid": false, "error": err.Error()}, nil
	}
	baseURL := "https://api-m.paypal.com"
	if cfg.IsSandbox {
		baseURL = "https://api-m.sandbox.paypal.com"
	}
	// OAuth2 获取 access_token 来验证连通性
	resp, err := p.client.R().SetContext(ctx).
		SetBasicAuth(cfg.ClientID, cfg.ClientSecret).
		SetFormData(map[string]string{"grant_type": "client_credentials"}).
		Post(baseURL + "/v1/oauth2/token")
	if err != nil {
		return map[string]any{"config_valid": true, "api_accessible": false, "error": err.Error()}, nil
	}
	return map[string]any{"config_valid": true, "api_accessible": resp.IsSuccess(), "status": resp.StatusCode()}, nil
}

func (p *paypalProvider) CreateOrder(ctx context.Context, data map[string]any, req PaymentOrderRequest) (*paymentdomain.PaymentPayload, error) {
	return nil, apperrors.New(50180, http.StatusNotImplemented, "PayPal 支付对接尚未完成")
}

func (p *paypalProvider) QueryRemoteOrder(ctx context.Context, data map[string]any, orderNo string) (map[string]any, error) {
	return map[string]any{"message": "PayPal 订单查询尚未实现"}, nil
}

func (p *paypalProvider) HandleCallback(ctx context.Context, data map[string]any, callbackData map[string]string, clientIP string) (*paymentdomain.CallbackResult, error) {
	return nil, apperrors.New(50180, http.StatusNotImplemented, "PayPal Webhook 处理尚未完成")
}

func (p *paypalProvider) SupportedPayTypes() []string {
	return []string{"paypal"}
}
