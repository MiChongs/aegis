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

	paypalsdk "github.com/plutov/paypal/v4"
	"github.com/shopspring/decimal"
)

// ── PayPal Provider（主流 SDK plutov/paypal/v4，REST API v2）──
// 下单创建 Order(intent=CAPTURE) 返回审批跳转链接；
// Webhook 验签走官方 verify-webhook-signature 接口（需配置 webhookId）；
// 买家审批完成（CHECKOUT.ORDER.APPROVED）时服务端自动 Capture 完成扣款闭环。

type paypalProvider struct{}

func newPaypalProvider(_ any) *paypalProvider { return &paypalProvider{} }

func (p *paypalProvider) Name() string { return paymentdomain.MethodPaypal }
func (p *paypalProvider) Describe() paymentdomain.ProviderMeta {
	return finalizeMeta(paymentdomain.ProviderMeta{
		Method:       paymentdomain.MethodPaypal,
		Name:         "PayPal",
		Description:  "全球覆盖最广的在线钱包，买家可用 PayPal 余额或绑定的银行卡付款，支持 200+ 国家/地区。",
		Category:     paymentdomain.CategoryInternational,
		Icon:         "paypal",
		BrandColor:   "#002991",
		DocURL:       "https://developer.paypal.com/docs/api/orders/v2/",
		CallbackPath: "/api/public/pay/callback/" + paymentdomain.MethodPaypal,
		CallbackNote: "在 PayPal 后台创建 Webhook 指向该地址，订阅 CHECKOUT.ORDER.APPROVED 与 PAYMENT.CAPTURE.COMPLETED，并把 Webhook ID 填入下方（验签必需）。",
		Regions:      []string{"全球"},
		Currencies:   []string{"USD", "EUR", "GBP", "JPY", "HKD", "AUD", "CAD", "SGD"},
		Capabilities: paymentdomain.ProviderCapabilities{
			Redirect: true, Webhook: true, WebhookSignature: true, RemoteQuery: true, Sandbox: true,
			Refund: true, PartialRefund: true,
		},
		PayTypes: []paymentdomain.PayTypeOption{
			payType("paypal", "PayPal 账户", "跳转 PayPal 收银台"),
		},
		Fields: fields(
			inGroup(paymentdomain.GroupCredential,
				fText("clientId", "Client ID", "PayPal Client ID", "开发者后台应用凭据", true),
				fSecret("clientSecret", "Client Secret", "PayPal Client Secret", "开发者后台应用密钥", true),
				fText("webhookId", "Webhook ID", "WH-...", "后台创建 Webhook 后获得，回调验签必需", true),
			),
			inGroup(paymentdomain.GroupGateway,
				fSelect("currency", "结算币种", "订单计价币种", "USD",
					opt("USD", "USD 美元"), opt("EUR", "EUR 欧元"), opt("GBP", "GBP 英镑"),
					opt("JPY", "JPY 日元"), opt("HKD", "HKD 港币"), opt("AUD", "AUD 澳元"),
					opt("CAD", "CAD 加元"), opt("SGD", "SGD 新加坡元")),
				fURL("notifyUrl", "Webhook 地址", "https://your-domain.com/api/public/pay/callback/paypal", "需与 PayPal 后台登记的端点一致"),
				fURL("returnUrl", "支付成功跳转", "https://your-domain.com/pay/success", "买家批准付款后的回跳地址"),
				fURL("cancelUrl", "取消支付跳转", "https://your-domain.com/pay/cancel", "买家取消付款后的回跳地址"),
			),
			limitFields("1.00", "10000"),
			advanced(inGroup(paymentdomain.GroupAdvanced,
				fSwitch("isSandbox", "沙箱环境", "开启后请求 api-m.sandbox.paypal.com", false),
			)...),
		),
	})
}

func (p *paypalProvider) decodeConfig(data map[string]any) (*paymentdomain.PaypalConfig, error) {
	cfg, err := decodeProviderConfig[paymentdomain.PaypalConfig](data)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.ClientID) == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "PayPal 配置不完整：缺少 clientId")
	}
	if strings.TrimSpace(cfg.ClientSecret) == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "PayPal 配置不完整：缺少 clientSecret")
	}
	if cfg.Currency = strings.ToUpper(strings.TrimSpace(cfg.Currency)); cfg.Currency == "" {
		cfg.Currency = "USD"
	}
	return cfg, nil
}

func (p *paypalProvider) ValidateConfig(data map[string]any) error {
	_, err := p.decodeConfig(data)
	return err
}

func (p *paypalProvider) newClient(ctx context.Context, cfg *paymentdomain.PaypalConfig) (*paypalsdk.Client, error) {
	base := paypalsdk.APIBaseLive
	if cfg.IsSandbox {
		base = paypalsdk.APIBaseSandBox
	}
	client, err := paypalsdk.NewClient(cfg.ClientID, cfg.ClientSecret, base)
	if err != nil {
		return nil, apperrors.New(40078, http.StatusBadRequest, "PayPal 客户端初始化失败："+err.Error())
	}
	client.SetHTTPClient(egress.NewClient(egress.Profile{Name: "payment.paypal", Timeout: 30 * time.Second}))
	if _, err := client.GetAccessToken(ctx); err != nil {
		return nil, apperrors.New(50085, http.StatusBadGateway, "PayPal 获取访问令牌失败："+err.Error())
	}
	return client, nil
}

func (p *paypalProvider) TestConnection(ctx context.Context, data map[string]any) (map[string]any, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return map[string]any{"config_valid": false, "error": err.Error()}, nil
	}
	if _, err := p.newClient(ctx, cfg); err != nil {
		return map[string]any{"config_valid": true, "api_accessible": false, "error": err.Error()}, nil
	}
	result := map[string]any{"config_valid": true, "api_accessible": true}
	if strings.TrimSpace(cfg.WebhookID) == "" {
		result["warning"] = "未配置 webhookId，支付回调将无法验签"
	}
	return result, nil
}

func (p *paypalProvider) CreateOrder(ctx context.Context, data map[string]any, req PaymentOrderRequest) (*paymentdomain.PaymentPayload, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	client, err := p.newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	returnURL := pickString(req.ReturnURL, cfg.ReturnURL)
	cancelURL := pickString(cfg.CancelURL, returnURL)
	units := []paypalsdk.PurchaseUnitRequest{{
		ReferenceID: req.OrderNo,
		InvoiceID:   req.OrderNo,
		CustomID:    req.OrderNo,
		Description: req.Subject,
		Amount: &paypalsdk.PurchaseUnitAmount{
			Currency: cfg.Currency,
			Value:    req.Amount.StringFixed(2),
		},
	}}
	order, err := client.CreateOrder(ctx, paypalsdk.OrderIntentCapture, units, nil, &paypalsdk.ApplicationContext{
		ReturnURL: returnURL,
		CancelURL: cancelURL,
	})
	if err != nil {
		return nil, apperrors.New(50085, http.StatusBadGateway, "PayPal 下单失败："+err.Error())
	}
	approveURL := ""
	for _, link := range order.Links {
		if strings.EqualFold(link.Rel, "approve") {
			approveURL = link.Href
			break
		}
	}
	if approveURL == "" {
		return nil, apperrors.New(50085, http.StatusBadGateway, "PayPal 未返回审批链接")
	}
	return &paymentdomain.PaymentPayload{
		Success:      true,
		OrderNo:      req.OrderNo,
		PaymentURL:   approveURL,
		RedirectURL:  approveURL,
		ProviderType: "paypal",
		FormData:     map[string]any{"paypalOrderId": order.ID},
	}, nil
}

func (p *paypalProvider) QueryRemoteOrder(ctx context.Context, data map[string]any, orderNo string) (map[string]any, error) {
	// PayPal 不支持按商户单号检索订单；PayPal 订单号在支付回调后落库 provider_order_no，
	// 远程核单请使用回调记录中的 PayPal 订单号或以本地订单状态为准
	return map[string]any{
		"message": "PayPal 不支持按本地订单号远程检索，请以本地订单状态（回调驱动）为准",
		"orderNo": orderNo,
	}, nil
}

// Refund PayPal 退款（针对 Capture）。
//
// 订单落库的 provider_order_no 视回调路径不同，可能是 Capture ID
// （PAYMENT.CAPTURE.COMPLETED）或 Order ID（CHECKOUT.ORDER.APPROVED 下服务端主动
// Capture 的返回值）。这里先按 Order 解析取真正的 Capture ID，解析不到再按 Capture ID 直用。
func (p *paypalProvider) Refund(ctx context.Context, data map[string]any, req paymentdomain.RefundRequest) (*paymentdomain.RefundResult, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	client, err := p.newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	providerNo := strings.TrimSpace(req.ProviderOrderNo)
	if providerNo == "" {
		return nil, apperrors.New(40098, http.StatusBadRequest, "PayPal 退款需要原支付单号，订单缺少上游单号")
	}
	captureID := providerNo
	if order, oerr := client.GetOrder(ctx, providerNo); oerr == nil && order != nil {
		if resolved := paypalCaptureIDFromOrder(order); resolved != "" {
			captureID = resolved
		}
	}

	refund, err := client.RefundCapture(ctx, captureID, paypalsdk.RefundCaptureRequest{
		Amount: &paypalsdk.Money{
			Currency: cfg.Currency,
			Value:    req.RefundAmount.StringFixed(2),
		},
		InvoiceID:   req.RefundNo,
		NoteToPayer: req.Reason,
	})
	if err != nil {
		return nil, apperrors.New(50093, http.StatusBadGateway, "PayPal 退款请求失败："+err.Error())
	}
	if refund == nil {
		return &paymentdomain.RefundResult{Status: paymentdomain.RefundProcessing, Message: "PayPal 未返回退款单"}, nil
	}
	amount := decimal.Zero
	if refund.Amount != nil {
		if parsed, perr := decimal.NewFromString(strings.TrimSpace(refund.Amount.Value)); perr == nil {
			amount = parsed
		}
	}
	result := &paymentdomain.RefundResult{
		ProviderRefundNo: refund.ID,
		Amount:           amount,
		Raw:              map[string]any{"refund_id": refund.ID, "status": refund.Status, "capture_id": captureID},
	}
	switch strings.ToUpper(strings.TrimSpace(refund.Status)) {
	case "COMPLETED":
		result.Status = paymentdomain.RefundSuccess
	case "CANCELLED", "FAILED":
		result.Status = paymentdomain.RefundFailed
		result.Message = "PayPal 退款状态：" + refund.Status
	default: // PENDING
		result.Status = paymentdomain.RefundProcessing
		result.Message = "PayPal 退款状态：" + refund.Status
	}
	return result, nil
}

// paypalCaptureIDFromOrder 从订单详情中取第一笔 Capture 的 ID
func paypalCaptureIDFromOrder(order *paypalsdk.Order) string {
	for _, unit := range order.PurchaseUnits {
		if unit.Payments == nil {
			continue
		}
		for _, capture := range unit.Payments.Captures {
			if strings.TrimSpace(capture.ID) != "" {
				return capture.ID
			}
		}
	}
	return ""
}

// paypalWebhookEvent Webhook 事件报文（取用字段子集）
type paypalWebhookEvent struct {
	ID        string `json:"id"`
	EventType string `json:"event_type"`
	Resource  struct {
		ID        string `json:"id"`
		Status    string `json:"status"`
		InvoiceID string `json:"invoice_id"`
		CustomID  string `json:"custom_id"`
		Amount    struct {
			CurrencyCode string `json:"currency_code"`
			Value        string `json:"value"`
		} `json:"amount"`
		PurchaseUnits []struct {
			ReferenceID string `json:"reference_id"`
			InvoiceID   string `json:"invoice_id"`
			CustomID    string `json:"custom_id"`
		} `json:"purchase_units"`
	} `json:"resource"`
}

func parsePaypalEvent(rawBody string) (*paypalWebhookEvent, error) {
	var event paypalWebhookEvent
	if err := json.Unmarshal([]byte(rawBody), &event); err != nil {
		return nil, err
	}
	return &event, nil
}

func (e *paypalWebhookEvent) localOrderNo() string {
	if e.Resource.InvoiceID != "" {
		return e.Resource.InvoiceID
	}
	if e.Resource.CustomID != "" {
		return e.Resource.CustomID
	}
	for _, unit := range e.Resource.PurchaseUnits {
		if unit.InvoiceID != "" {
			return unit.InvoiceID
		}
		if unit.CustomID != "" {
			return unit.CustomID
		}
		if unit.ReferenceID != "" {
			return unit.ReferenceID
		}
	}
	return ""
}

// ExtractOrderNo 从未验签的 Webhook 报文预提取本地订单号（仅用于订单路由）
func (p *paypalProvider) ExtractOrderNo(callbackData map[string]string) string {
	rawBody := callbackData[CallbackRawBodyKey]
	if rawBody == "" {
		return ""
	}
	event, err := parsePaypalEvent(rawBody)
	if err != nil {
		return ""
	}
	return event.localOrderNo()
}

func (p *paypalProvider) HandleCallback(ctx context.Context, data map[string]any, callbackData map[string]string, clientIP string) (*paymentdomain.CallbackResult, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	rawBody := callbackData[CallbackRawBodyKey]
	if rawBody == "" {
		return nil, apperrors.New(40075, http.StatusBadRequest, "PayPal Webhook 缺少原始报文")
	}
	if strings.TrimSpace(cfg.WebhookID) == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "PayPal 未配置 webhookId，无法验签")
	}
	client, err := p.newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	// 重建请求供官方验签接口使用（基于原始报文 + Paypal-* 签名头）
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "/", strings.NewReader(rawBody))
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"Paypal-Transmission-Id", "Paypal-Transmission-Time", "Paypal-Transmission-Sig", "Paypal-Cert-Url", "Paypal-Auth-Algo"} {
		if v := callbackHeader(callbackData, name); v != "" {
			httpReq.Header.Set(name, v)
		}
	}
	verify, err := client.VerifyWebhookSignature(ctx, httpReq, cfg.WebhookID)
	if err != nil {
		return nil, apperrors.New(40076, http.StatusBadRequest, "PayPal Webhook 验签请求失败："+err.Error())
	}
	if !strings.EqualFold(verify.VerificationStatus, "SUCCESS") {
		return nil, apperrors.New(40076, http.StatusBadRequest, "PayPal Webhook 验签未通过")
	}

	event, err := parsePaypalEvent(rawBody)
	if err != nil {
		return nil, apperrors.New(40076, http.StatusBadRequest, "PayPal 事件解析失败")
	}
	orderNo := event.localOrderNo()
	rawData := map[string]any{"event_id": event.ID, "event_type": event.EventType, "resource_id": event.Resource.ID}

	switch event.EventType {
	case "PAYMENT.CAPTURE.COMPLETED":
		amount := decimal.Zero
		if parsed, perr := decimal.NewFromString(strings.TrimSpace(event.Resource.Amount.Value)); perr == nil {
			amount = parsed
		}
		return &paymentdomain.CallbackResult{
			Success:         true,
			Paid:            true,
			OrderNo:         orderNo,
			ProviderOrderNo: event.Resource.ID,
			TradeStatus:     event.EventType,
			PaymentMethod:   "paypal",
			Amount:          amount,
			RawData:         rawData,
		}, nil
	case "CHECKOUT.ORDER.APPROVED":
		// 买家已审批：服务端立即请求扣款（Capture），完成支付闭环
		capture, err := client.CaptureOrder(ctx, event.Resource.ID, paypalsdk.CaptureOrderRequest{})
		if err != nil {
			return nil, apperrors.New(50085, http.StatusBadGateway, "PayPal Capture 失败："+err.Error())
		}
		rawData["capture_status"] = capture.Status
		return &paymentdomain.CallbackResult{
			Success:         true,
			Paid:            strings.EqualFold(capture.Status, "COMPLETED"),
			OrderNo:         orderNo,
			ProviderOrderNo: capture.ID,
			TradeStatus:     "CHECKOUT.ORDER.APPROVED/" + capture.Status,
			PaymentMethod:   "paypal",
			RawData:         rawData,
		}, nil
	default:
		return &paymentdomain.CallbackResult{
			Success:     true,
			Paid:        false,
			OrderNo:     orderNo,
			TradeStatus: event.EventType,
			Message:     "事件已接收：" + event.EventType,
			RawData:     rawData,
		}, nil
	}
}
