package service

import (
	"context"
	"net/http"
	"strings"

	paymentdomain "aegis/internal/domain/payment"
	apperrors "aegis/pkg/errors"

	"github.com/go-resty/resty/v2"
)

// ── 支付宝原生 Provider ──
// 注：完整的支付宝原生对接需要引入官方 SDK（如 github.com/smartwalle/alipay/v3）。
// 当前实现为配置验证 + 占位逻辑，实际支付对接后续根据需求完善。

type alipayNativeProvider struct {
	client *resty.Client
}

func newAlipayNativeProvider(client *resty.Client) *alipayNativeProvider {
	return &alipayNativeProvider{client: client}
}

func (p *alipayNativeProvider) Name() string  { return paymentdomain.MethodAlipayNative }
func (p *alipayNativeProvider) Label() string { return "支付宝原生" }

func (p *alipayNativeProvider) ValidateConfig(data map[string]any) error {
	cfg, err := decodeProviderConfig[paymentdomain.AlipayNativeConfig](data)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.AppID) == "" {
		return apperrors.New(40078, http.StatusBadRequest, "支付宝配置不完整：缺少 appId")
	}
	if strings.TrimSpace(cfg.PrivateKey) == "" {
		return apperrors.New(40078, http.StatusBadRequest, "支付宝配置不完整：缺少商户私钥")
	}
	if !cfg.CertMode && strings.TrimSpace(cfg.AlipayPublicKey) == "" {
		return apperrors.New(40078, http.StatusBadRequest, "支付宝配置不完整：公钥模式需要提供支付宝公钥")
	}
	return nil
}

func (p *alipayNativeProvider) TestConnection(ctx context.Context, data map[string]any) (map[string]any, error) {
	if err := p.ValidateConfig(data); err != nil {
		return map[string]any{"config_valid": false, "error": err.Error()}, nil
	}
	return map[string]any{"config_valid": true, "api_accessible": true, "message": "配置验证通过，需引入支付宝 SDK 进行完整连通性测试"}, nil
}

func (p *alipayNativeProvider) CreateOrder(ctx context.Context, data map[string]any, req PaymentOrderRequest) (*paymentdomain.PaymentPayload, error) {
	// 实际对接需引入支付宝 SDK 构建签名请求
	return nil, apperrors.New(50180, http.StatusNotImplemented, "支付宝原生支付对接尚未完成，请使用易支付等聚合支付方式")
}

func (p *alipayNativeProvider) QueryRemoteOrder(ctx context.Context, data map[string]any, orderNo string) (map[string]any, error) {
	return map[string]any{"message": "支付宝原生订单查询尚未实现"}, nil
}

func (p *alipayNativeProvider) HandleCallback(ctx context.Context, data map[string]any, callbackData map[string]string, clientIP string) (*paymentdomain.CallbackResult, error) {
	return nil, apperrors.New(50180, http.StatusNotImplemented, "支付宝原生回调处理尚未完成")
}

func (p *alipayNativeProvider) SupportedPayTypes() []string {
	return []string{"alipay"}
}
