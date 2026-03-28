package service

import (
	"context"
	"net/http"
	"strings"

	paymentdomain "aegis/internal/domain/payment"
	apperrors "aegis/pkg/errors"

	"github.com/go-resty/resty/v2"
)

// ── 微信支付原生 Provider ──
// 注：完整的微信支付对接需要引入官方 SDK（如 github.com/wechatpay-apiv3/wechatpay-go）。
// 当前实现为配置验证 + 占位逻辑，实际支付对接后续根据需求完善。

type wechatNativeProvider struct {
	client *resty.Client
}

func newWechatNativeProvider(client *resty.Client) *wechatNativeProvider {
	return &wechatNativeProvider{client: client}
}

func (p *wechatNativeProvider) Name() string  { return paymentdomain.MethodWechatNative }
func (p *wechatNativeProvider) Label() string { return "微信支付原生" }

func (p *wechatNativeProvider) ValidateConfig(data map[string]any) error {
	cfg, err := decodeProviderConfig[paymentdomain.WechatNativeConfig](data)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.AppID) == "" {
		return apperrors.New(40078, http.StatusBadRequest, "微信支付配置不完整：缺少 appId")
	}
	if strings.TrimSpace(cfg.MchID) == "" {
		return apperrors.New(40078, http.StatusBadRequest, "微信支付配置不完整：缺少商户号 mchId")
	}
	if strings.TrimSpace(cfg.APIKey) == "" && strings.TrimSpace(cfg.APIKeyV3) == "" {
		return apperrors.New(40078, http.StatusBadRequest, "微信支付配置不完整：缺少 API 密钥")
	}
	return nil
}

func (p *wechatNativeProvider) TestConnection(ctx context.Context, data map[string]any) (map[string]any, error) {
	if err := p.ValidateConfig(data); err != nil {
		return map[string]any{"config_valid": false, "error": err.Error()}, nil
	}
	return map[string]any{"config_valid": true, "api_accessible": true, "message": "配置验证通过，需引入微信支付 SDK 进行完整连通性测试"}, nil
}

func (p *wechatNativeProvider) CreateOrder(ctx context.Context, data map[string]any, req PaymentOrderRequest) (*paymentdomain.PaymentPayload, error) {
	return nil, apperrors.New(50180, http.StatusNotImplemented, "微信支付原生对接尚未完成，请使用易支付等聚合支付方式")
}

func (p *wechatNativeProvider) QueryRemoteOrder(ctx context.Context, data map[string]any, orderNo string) (map[string]any, error) {
	return map[string]any{"message": "微信支付原生订单查询尚未实现"}, nil
}

func (p *wechatNativeProvider) HandleCallback(ctx context.Context, data map[string]any, callbackData map[string]string, clientIP string) (*paymentdomain.CallbackResult, error) {
	return nil, apperrors.New(50180, http.StatusNotImplemented, "微信支付原生回调处理尚未完成")
}

func (p *wechatNativeProvider) SupportedPayTypes() []string {
	return []string{"wxpay"}
}
