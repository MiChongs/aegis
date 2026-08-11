package service

import (
	"aegis/pkg/egress"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	paymentdomain "aegis/internal/domain/payment"
	apperrors "aegis/pkg/errors"

	"github.com/shopspring/decimal"
	stripe "github.com/stripe/stripe-go/v82"
	stripeclient "github.com/stripe/stripe-go/v82/client"
	"github.com/stripe/stripe-go/v82/webhook"
)

// ── Stripe Provider（官方 SDK stripe-go/v82）──
// 下单走 Checkout Session（托管收银台，覆盖卡 / Alipay / WeChat Pay 等渠道），
// 回调走 Webhook（checkout.session.completed），基于原始报文 + Stripe-Signature 验签。
// 多租户安全：每个应用的密钥独立，使用 client.API 实例而非包级全局 Key。

type stripeProvider struct{}

func newStripeProvider(_ any) *stripeProvider { return &stripeProvider{} }

func (p *stripeProvider) Name() string { return paymentdomain.MethodStripe }

func (p *stripeProvider) Describe() paymentdomain.ProviderMeta {
	return finalizeMeta(paymentdomain.ProviderMeta{
		Method:       paymentdomain.MethodStripe,
		Name:         "Stripe",
		Description:  "国际主流收单，托管 Checkout 收银台覆盖信用卡 / Apple Pay / Google Pay / 支付宝 / 微信，支持 135+ 币种。",
		Category:     paymentdomain.CategoryInternational,
		Icon:         "stripe",
		BrandColor:   "#635BFF",
		DocURL:       "https://docs.stripe.com/payments/checkout",
		CallbackPath: "/api/public/pay/callback/" + paymentdomain.MethodStripe,
		CallbackNote: "在 Stripe 后台创建 Webhook 指向该地址，订阅 checkout.session.completed 与 payment_intent.succeeded，并把 Signing secret 填入下方 Webhook Secret。",
		Regions:      []string{"全球"},
		Currencies:   []string{"USD", "EUR", "GBP", "JPY", "HKD", "SGD", "AUD", "CAD", "CNY"},
		Capabilities: paymentdomain.ProviderCapabilities{
			Redirect: true, Webhook: true, WebhookSignature: true, RemoteQuery: true, Sandbox: true,
			Refund: true, PartialRefund: true, RefundQuery: true,
		},
		PayTypes: []paymentdomain.PayTypeOption{
			payType("card", "银行卡", "Visa / Mastercard / Amex 等"),
			payType("alipay", "支付宝", "需在 Stripe 后台开通"),
			payType("wechat_pay", "微信支付", "需在 Stripe 后台开通"),
		},
		Fields: fields(
			inGroup(paymentdomain.GroupCredential,
				fSecret("secretKey", "Secret Key", "sk_live_...", "服务端密钥，切勿下发到客户端", true),
				fText("publishableKey", "Publishable Key", "pk_live_...", "可公开的前端密钥", true),
				fSecret("webhookSecret", "Webhook Secret", "whsec_...", "Webhook 端点的签名密钥，验签必需", true),
			),
			inGroup(paymentdomain.GroupGateway,
				fSelect("currency", "结算币种", "Checkout Session 的计价币种", "usd",
					opt("usd", "USD 美元"), opt("eur", "EUR 欧元"), opt("gbp", "GBP 英镑"),
					opt("jpy", "JPY 日元"), opt("hkd", "HKD 港币"), opt("sgd", "SGD 新加坡元"),
					opt("aud", "AUD 澳元"), opt("cad", "CAD 加元"), opt("cny", "CNY 人民币")),
				fURL("notifyUrl", "Webhook 地址", "https://your-domain.com/api/public/pay/callback/stripe", "需与 Stripe 后台登记的端点一致"),
				fURL("returnUrl", "支付成功跳转", "https://your-domain.com/pay/success", "Checkout 成功后的 success_url，必填"),
				fURL("cancelUrl", "取消支付跳转", "https://your-domain.com/pay/cancel", "留空则复用成功跳转地址"),
			),
			limitFields("0.50", "999999"),
		),
	})
}

func (p *stripeProvider) api(secretKey string) *stripeclient.API {
	sc := &stripeclient.API{}
	// 官方 SDK 默认自建 http.Client；用 Backends 把出口换成出海网关。
	sc.Init(secretKey, stripe.NewBackends(egress.NewClient(egress.Profile{
		Name: "payment.stripe", Timeout: 30 * time.Second,
	})))
	return sc
}

func (p *stripeProvider) decodeConfig(data map[string]any) (*paymentdomain.StripeConfig, error) {
	cfg, err := decodeProviderConfig[paymentdomain.StripeConfig](data)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.SecretKey) == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "Stripe 配置不完整：缺少 secretKey")
	}
	if cfg.Currency = strings.ToLower(strings.TrimSpace(cfg.Currency)); cfg.Currency == "" {
		cfg.Currency = "usd"
	}
	return cfg, nil
}

func (p *stripeProvider) ValidateConfig(data map[string]any) error {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.PublishableKey) == "" {
		return apperrors.New(40078, http.StatusBadRequest, "Stripe 配置不完整：缺少 publishableKey")
	}
	if strings.TrimSpace(cfg.WebhookSecret) == "" {
		return apperrors.New(40078, http.StatusBadRequest, "Stripe 配置不完整：缺少 webhookSecret（Webhook 验签必需）")
	}
	return nil
}

func (p *stripeProvider) TestConnection(ctx context.Context, data map[string]any) (map[string]any, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return map[string]any{"config_valid": false, "error": err.Error()}, nil
	}
	// 拉取账户余额验证密钥有效性与连通性
	bal, err := p.api(cfg.SecretKey).Balance.Get(&stripe.BalanceParams{Params: stripe.Params{Context: ctx}})
	if err != nil {
		return map[string]any{"config_valid": true, "api_accessible": false, "error": err.Error()}, nil
	}
	return map[string]any{"config_valid": true, "api_accessible": true, "livemode": bal.Livemode}, nil
}

func (p *stripeProvider) CreateOrder(ctx context.Context, data map[string]any, req PaymentOrderRequest) (*paymentdomain.PaymentPayload, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	successURL := pickString(req.ReturnURL, cfg.ReturnURL)
	if successURL == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "Stripe 需要配置 returnUrl（支付完成跳转地址）")
	}
	cancelURL := pickString(cfg.CancelURL, successURL)

	params := &stripe.CheckoutSessionParams{
		Params:            stripe.Params{Context: ctx},
		Mode:              stripe.String(string(stripe.CheckoutSessionModePayment)),
		ClientReferenceID: stripe.String(req.OrderNo),
		SuccessURL:        stripe.String(successURL),
		CancelURL:         stripe.String(cancelURL),
		LineItems: []*stripe.CheckoutSessionLineItemParams{{
			Quantity: stripe.Int64(1),
			PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
				Currency:   stripe.String(cfg.Currency),
				UnitAmount: stripe.Int64(stripeMinorUnits(req.Amount, cfg.Currency)),
				ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
					Name: stripe.String(req.Subject),
				},
			},
		}},
		PaymentIntentData: &stripe.CheckoutSessionPaymentIntentDataParams{
			Metadata: map[string]string{"order_no": req.OrderNo},
		},
	}
	params.AddMetadata("order_no", req.OrderNo)

	sess, err := p.api(cfg.SecretKey).CheckoutSessions.New(params)
	if err != nil {
		return nil, apperrors.New(50082, http.StatusBadGateway, "Stripe 下单失败："+err.Error())
	}
	return &paymentdomain.PaymentPayload{
		Success:      true,
		OrderNo:      req.OrderNo,
		PaymentURL:   sess.URL,
		RedirectURL:  sess.URL,
		ProviderType: "stripe_checkout",
		FormData: map[string]any{
			"sessionId":      sess.ID,
			"publishableKey": cfg.PublishableKey,
		},
	}, nil
}

func (p *stripeProvider) QueryRemoteOrder(ctx context.Context, data map[string]any, orderNo string) (map[string]any, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	// 通过 PaymentIntent 元数据检索（下单时已写入 order_no）
	iter := p.api(cfg.SecretKey).PaymentIntents.Search(&stripe.PaymentIntentSearchParams{
		SearchParams: stripe.SearchParams{
			Context: ctx,
			Query:   "metadata['order_no']:'" + orderNo + "'",
		},
	})
	for iter.Next() {
		pi := iter.PaymentIntent()
		return map[string]any{
			"payment_intent_id": pi.ID,
			"status":            string(pi.Status),
			"amount":            pi.Amount,
			"currency":          string(pi.Currency),
			"paid":              pi.Status == stripe.PaymentIntentStatusSucceeded,
		}, nil
	}
	if err := iter.Err(); err != nil {
		return nil, apperrors.New(50082, http.StatusBadGateway, "Stripe 查询失败："+err.Error())
	}
	return map[string]any{"message": "未找到对应的 Stripe 支付记录", "paid": false}, nil
}

// Refund Stripe 退款。
// 退款针对 PaymentIntent（下单时写入订单的 provider_order_no），
// 幂等键用本地退款单号，避免网络重试产生第二笔退款。
func (p *stripeProvider) Refund(ctx context.Context, data map[string]any, req paymentdomain.RefundRequest) (*paymentdomain.RefundResult, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	paymentIntent := strings.TrimSpace(req.ProviderOrderNo)
	if paymentIntent == "" {
		return nil, apperrors.New(40098, http.StatusBadRequest, "Stripe 退款需要原支付的 PaymentIntent，订单缺少上游单号")
	}
	params := &stripe.RefundParams{
		Params:        stripe.Params{Context: ctx},
		PaymentIntent: stripe.String(paymentIntent),
		Amount:        stripe.Int64(stripeMinorUnits(req.RefundAmount, cfg.Currency)),
	}
	params.SetIdempotencyKey(req.RefundNo)
	params.AddMetadata("order_no", req.OrderNo)
	params.AddMetadata("refund_no", req.RefundNo)
	if reason := strings.TrimSpace(req.Reason); reason != "" {
		params.AddMetadata("reason", reason)
	}

	refund, err := p.api(cfg.SecretKey).Refunds.New(params)
	if err != nil {
		return nil, apperrors.New(50092, http.StatusBadGateway, "Stripe 退款请求失败："+err.Error())
	}
	return stripeRefundResult(refund, cfg.Currency), nil
}

// QueryRefund 查询 Stripe 退款单状态（pending 态的补偿通道）
func (p *stripeProvider) QueryRefund(ctx context.Context, data map[string]any, query paymentdomain.RefundQuery) (*paymentdomain.RefundResult, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	refundID := strings.TrimSpace(query.ProviderRefundNo)
	if refundID == "" {
		return nil, apperrors.New(40098, http.StatusBadRequest, "Stripe 退款查询需要上游退款单号")
	}
	refund, err := p.api(cfg.SecretKey).Refunds.Get(refundID, &stripe.RefundParams{Params: stripe.Params{Context: ctx}})
	if err != nil {
		return nil, apperrors.New(50092, http.StatusBadGateway, "Stripe 退款查询失败："+err.Error())
	}
	return stripeRefundResult(refund, cfg.Currency), nil
}

// stripeRefundResult 把 Stripe 退款对象映射为统一退款结果
func stripeRefundResult(refund *stripe.Refund, fallbackCurrency string) *paymentdomain.RefundResult {
	if refund == nil {
		return &paymentdomain.RefundResult{Status: paymentdomain.RefundProcessing, Message: "Stripe 未返回退款单"}
	}
	currency := firstNonEmpty(string(refund.Currency), fallbackCurrency)
	result := &paymentdomain.RefundResult{
		ProviderRefundNo: refund.ID,
		Amount:           stripeMajorUnits(refund.Amount, currency),
		Raw:              map[string]any{"refund_id": refund.ID, "status": string(refund.Status), "currency": currency},
	}
	switch refund.Status {
	case stripe.RefundStatusSucceeded:
		result.Status = paymentdomain.RefundSuccess
	case stripe.RefundStatusFailed, stripe.RefundStatusCanceled:
		result.Status = paymentdomain.RefundFailed
		result.Message = "Stripe 退款状态：" + string(refund.Status)
		if refund.FailureReason != "" {
			result.Message += "（" + string(refund.FailureReason) + "）"
		}
	default: // pending / requires_action
		result.Status = paymentdomain.RefundProcessing
		result.Message = "Stripe 退款状态：" + string(refund.Status)
	}
	return result
}

// ExtractOrderNo 从未验签的 Webhook 报文预提取本地订单号（仅用于订单路由）
func (p *stripeProvider) ExtractOrderNo(callbackData map[string]string) string {
	rawBody := callbackData[CallbackRawBodyKey]
	if rawBody == "" {
		return ""
	}
	var event struct {
		Data struct {
			Object struct {
				ClientReferenceID string            `json:"client_reference_id"`
				Metadata          map[string]string `json:"metadata"`
			} `json:"object"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(rawBody), &event); err != nil {
		return ""
	}
	if event.Data.Object.ClientReferenceID != "" {
		return event.Data.Object.ClientReferenceID
	}
	return event.Data.Object.Metadata["order_no"]
}

func (p *stripeProvider) HandleCallback(ctx context.Context, data map[string]any, callbackData map[string]string, clientIP string) (*paymentdomain.CallbackResult, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	rawBody := callbackData[CallbackRawBodyKey]
	sigHeader := callbackHeader(callbackData, "Stripe-Signature")
	if rawBody == "" || sigHeader == "" {
		return nil, apperrors.New(40075, http.StatusBadRequest, "Stripe Webhook 缺少原始报文或签名头")
	}
	if strings.TrimSpace(cfg.WebhookSecret) == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "Stripe 未配置 webhookSecret，无法验签")
	}
	// 官方验签：基于逐字节原文 + Stripe-Signature + endpoint secret
	event, err := webhook.ConstructEvent([]byte(rawBody), sigHeader, cfg.WebhookSecret)
	if err != nil {
		return nil, apperrors.New(40076, http.StatusBadRequest, "Stripe Webhook 验签失败："+err.Error())
	}

	rawData := map[string]any{"event_id": event.ID, "event_type": string(event.Type)}
	switch event.Type {
	case "checkout.session.completed", "checkout.session.async_payment_succeeded":
		var sess stripe.CheckoutSession
		if err := json.Unmarshal(event.Data.Raw, &sess); err != nil {
			return nil, apperrors.New(40076, http.StatusBadRequest, "Stripe 事件解析失败")
		}
		orderNo := sess.ClientReferenceID
		if orderNo == "" && sess.Metadata != nil {
			orderNo = sess.Metadata["order_no"]
		}
		providerOrderNo := sess.ID
		if sess.PaymentIntent != nil {
			providerOrderNo = sess.PaymentIntent.ID
		}
		paid := sess.PaymentStatus == stripe.CheckoutSessionPaymentStatusPaid
		amount := stripeMajorUnits(sess.AmountTotal, string(sess.Currency))
		rawData["session_id"] = sess.ID
		rawData["payment_status"] = string(sess.PaymentStatus)
		return &paymentdomain.CallbackResult{
			Success:         true,
			Paid:            paid,
			OrderNo:         orderNo,
			ProviderOrderNo: providerOrderNo,
			TradeStatus:     string(event.Type),
			PaymentMethod:   "stripe",
			Amount:          amount,
			RawData:         rawData,
		}, nil
	case "payment_intent.succeeded":
		var pi stripe.PaymentIntent
		if err := json.Unmarshal(event.Data.Raw, &pi); err != nil {
			return nil, apperrors.New(40076, http.StatusBadRequest, "Stripe 事件解析失败")
		}
		rawData["payment_intent_id"] = pi.ID
		return &paymentdomain.CallbackResult{
			Success:         true,
			Paid:            true,
			OrderNo:         pi.Metadata["order_no"],
			ProviderOrderNo: pi.ID,
			TradeStatus:     string(event.Type),
			PaymentMethod:   "stripe",
			Amount:          stripeMajorUnits(pi.Amount, string(pi.Currency)),
			RawData:         rawData,
		}, nil
	default:
		// 其他事件（如 checkout.session.expired）：确认收到但不视为支付成功
		return &paymentdomain.CallbackResult{
			Success:     true,
			Paid:        false,
			TradeStatus: string(event.Type),
			Message:     "事件已接收：" + string(event.Type),
			RawData:     rawData,
		}, nil
	}
}

// stripeZeroDecimalCurrencies Stripe 零小数位币种（金额不乘 100）
var stripeZeroDecimalCurrencies = map[string]bool{
	"bif": true, "clp": true, "djf": true, "gnf": true, "jpy": true, "kmf": true,
	"krw": true, "mga": true, "pyg": true, "rwf": true, "ugx": true, "vnd": true,
	"vuv": true, "xaf": true, "xof": true, "xpf": true,
}

// stripeMinorUnits 主单位金额 → Stripe 最小货币单位
func stripeMinorUnits(amount decimal.Decimal, currency string) int64 {
	if stripeZeroDecimalCurrencies[strings.ToLower(currency)] {
		return amount.Round(0).IntPart()
	}
	return amount.Mul(decimal.NewFromInt(100)).Round(0).IntPart()
}

// stripeMajorUnits Stripe 最小货币单位 → 主单位金额
func stripeMajorUnits(minor int64, currency string) decimal.Decimal {
	if stripeZeroDecimalCurrencies[strings.ToLower(currency)] {
		return decimal.NewFromInt(minor)
	}
	return decimal.NewFromInt(minor).Div(decimal.NewFromInt(100))
}
