package service

import (
	"context"
	"net/http"

	paymentdomain "aegis/internal/domain/payment"
	apperrors "aegis/pkg/errors"
)

// ── 余额支付 Provider（内部钱包通道）──
// 实际扣款 / 支付确认 / 履约由 PaymentService.payOrderWithBalance 经仓储层单事务完成；
// 本 Provider 只承担配置语义、方法列表展示与防御性兜底，CreateOrder 不应被直接调用
// （服务层在调用 Provider 之前已对 balance 方法分流）。

type balanceProvider struct{}

func newBalanceProvider() *balanceProvider { return &balanceProvider{} }

func (p *balanceProvider) Name() string { return paymentdomain.MethodBalance }

func (p *balanceProvider) Describe() paymentdomain.ProviderMeta {
	return finalizeMeta(paymentdomain.ProviderMeta{
		Method:      paymentdomain.MethodBalance,
		Name:        "余额支付",
		Description: "内部钱包通道：下单即在单个数据库事务内完成扣款、订单确认与权益发放，无第三方依赖、无回调延迟。",
		Category:    paymentdomain.CategoryInternal,
		Icon:        "wallet",
		BrandColor:  "#0EA5E9",
		Regions:     []string{"不限"},
		Currencies:  []string{"CNY"},
		Capabilities: paymentdomain.ProviderCapabilities{
			// 内部通道：无跳转、无回调、无上游查单；退款直接原路退回钱包
			Refund: true, PartialRefund: true,
		},
		PayTypes: []paymentdomain.PayTypeOption{
			payType("balance", "钱包余额", "实时扣减用户钱包余额"),
		},
		Fields: fields(
			limitFields("0.01", "50000"),
		),
	})
}

func (p *balanceProvider) ValidateConfig(data map[string]any) error {
	// 内部通道无必填配置；保留 map 以便未来扩展（如单笔限额）
	return nil
}

func (p *balanceProvider) TestConnection(ctx context.Context, data map[string]any) (map[string]any, error) {
	return map[string]any{
		"config_valid":   true,
		"api_accessible": true,
		"message":        "余额支付为内部通道，无需外部连通性测试",
	}, nil
}

func (p *balanceProvider) CreateOrder(ctx context.Context, data map[string]any, req PaymentOrderRequest) (*paymentdomain.PaymentPayload, error) {
	// 防御性兜底：服务层应在进入 Provider 前完成余额支付分流
	return nil, apperrors.New(50081, http.StatusInternalServerError, "余额支付应由内部通道处理")
}

func (p *balanceProvider) QueryRemoteOrder(ctx context.Context, data map[string]any, orderNo string) (map[string]any, error) {
	return map[string]any{"message": "余额支付为内部通道，订单状态以本地订单为准"}, nil
}

func (p *balanceProvider) HandleCallback(ctx context.Context, data map[string]any, callbackData map[string]string, clientIP string) (*paymentdomain.CallbackResult, error) {
	return nil, apperrors.New(40097, http.StatusBadRequest, "余额支付无外部回调")
}

// Refund 防御性兜底：余额退款是纯内部账务，由 PaymentService.refundToWallet 在单事务内
// 完成「退款单结算 + 钱包入账 + 履约冲正」，不应走到 Provider。
// 实现本方法是为了让能力矩阵（Capabilities.Refund）与接口实现保持一致。
func (p *balanceProvider) Refund(ctx context.Context, data map[string]any, req paymentdomain.RefundRequest) (*paymentdomain.RefundResult, error) {
	return nil, apperrors.New(50081, http.StatusInternalServerError, "余额退款应由内部通道处理")
}
