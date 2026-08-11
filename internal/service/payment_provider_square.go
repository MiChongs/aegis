package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	paymentdomain "aegis/internal/domain/payment"
	apperrors "aegis/pkg/errors"

	"github.com/go-resty/resty/v2"
)

// ── Square Provider（北美线上收单）──
//
// 下单：POST /v2/online-checkout/payment-links 创建 Quick Pay 支付链接，
//       本地订单号写入 payment_note，回调与查单据此定位订单。
// 回调：x-square-hmacsha256-signature 为 base64(HMAC-SHA256(签名密钥, 通知地址 + 原始报文))。
//       **签名原文包含通知地址本身**，因此必须在配置里填写与 Square 后台登记完全一致的 URL。

// squareAPIVersion 固定 API 版本，避免上游默认版本变更导致字段漂移
const squareAPIVersion = "2025-01-23"

type squareProvider struct {
	client *resty.Client
}

func newSquareProvider(client *resty.Client) *squareProvider {
	return &squareProvider{client: client}
}

func (p *squareProvider) Name() string { return paymentdomain.MethodSquare }

func (p *squareProvider) Describe() paymentdomain.ProviderMeta {
	return finalizeMeta(paymentdomain.ProviderMeta{
		Method:       paymentdomain.MethodSquare,
		Name:         "Square",
		Description:  "北美中小商户主流收单，Quick Pay 支付链接开箱即用，支持 Apple Pay / Google Pay / 银行卡。",
		Category:     paymentdomain.CategoryInternational,
		Icon:         "square",
		BrandColor:   "#3E4348",
		DocURL:       "https://developer.squareup.com/reference/square/checkout-api/create-payment-link",
		CallbackPath: "/api/public/pay/callback/" + paymentdomain.MethodSquare,
		CallbackNote: "在 Developer Dashboard → Webhooks 新建订阅指向该地址，勾选 payment.updated 事件；把 Signature key 填入 Webhook 密钥，并把该地址原样填入「通知地址（验签用）」——Square 的签名原文包含 URL，两者不一致会导致验签必然失败。",
		Regions:      []string{"美国", "加拿大", "英国", "澳大利亚", "日本"},
		Currencies:   []string{"USD", "CAD", "GBP", "AUD", "JPY"},
		Capabilities: paymentdomain.ProviderCapabilities{
			Redirect: true, Webhook: true, WebhookSignature: true, RemoteQuery: true, Sandbox: true,
			Refund: true, PartialRefund: true, RefundQuery: true,
		},
		PayTypes: []paymentdomain.PayTypeOption{
			payType("card", "银行卡", "Square 托管收银台"),
			payType("wallet", "移动钱包", "Apple Pay / Google Pay"),
		},
		Fields: fields(
			inGroup(paymentdomain.GroupCredential,
				fSecret("accessToken", "Access Token", "EAAA...", "Developer Dashboard 的应用访问令牌", true),
				fText("locationId", "Location ID", "L1234ABCD", "收款门店标识，可在 Dashboard → Locations 查看", true),
				fSecret("webhookSecret", "Webhook 密钥", "Signature key", "Webhook 订阅的签名密钥，验签必需", true),
			),
			inGroup(paymentdomain.GroupGateway,
				fSelect("currency", "结算币种", "需与门店所在国家一致", "USD",
					opt("USD", "USD 美元"), opt("CAD", "CAD 加元"), opt("GBP", "GBP 英镑"),
					opt("AUD", "AUD 澳元"), opt("JPY", "JPY 日元")),
				fURL("webhookUrl", "通知地址（验签用）", "https://your-domain.com/api/public/pay/callback/square", "必须与 Square 后台登记的地址逐字符一致，否则验签失败"),
				fURL("notifyUrl", "Webhook 地址", "https://your-domain.com/api/public/pay/callback/square", "登记用途，留空则复用上方验签地址"),
				fURL("returnUrl", "支付成功跳转", "https://your-domain.com/pay/success", "付款完成后的回跳地址"),
			),
			limitFields("1.00", "50000"),
			advanced(inGroup(paymentdomain.GroupAdvanced,
				fSwitch("isSandbox", "沙箱环境", "开启后请求 connect.squareupsandbox.com", false),
			)...),
		),
	})
}

func (p *squareProvider) decodeConfig(data map[string]any) (*paymentdomain.SquareConfig, error) {
	cfg, err := decodeProviderConfig[paymentdomain.SquareConfig](data)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.AccessToken) == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "Square 配置不完整：缺少 accessToken")
	}
	if strings.TrimSpace(cfg.LocationID) == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "Square 配置不完整：缺少 locationId")
	}
	if cfg.Currency = strings.ToUpper(strings.TrimSpace(cfg.Currency)); cfg.Currency == "" {
		cfg.Currency = "USD"
	}
	return cfg, nil
}

func (p *squareProvider) ValidateConfig(data map[string]any) error {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.WebhookSecret) == "" {
		return apperrors.New(40078, http.StatusBadRequest, "Square 配置不完整：缺少 webhookSecret（Webhook 验签必需）")
	}
	if p.signatureURL(cfg) == "" {
		return apperrors.New(40078, http.StatusBadRequest, "Square 配置不完整：缺少 webhookUrl（签名原文包含通知地址，验签必需）")
	}
	return nil
}

// signatureURL 验签用的通知地址：优先取显式配置，其次退回登记地址
func (p *squareProvider) signatureURL(cfg *paymentdomain.SquareConfig) string {
	return pickString(cfg.WebhookURL, cfg.NotifyURL)
}

func (p *squareProvider) baseURL(cfg *paymentdomain.SquareConfig) string {
	if cfg.IsSandbox {
		return "https://connect.squareupsandbox.com"
	}
	return "https://connect.squareup.com"
}

func (p *squareProvider) request(ctx context.Context, cfg *paymentdomain.SquareConfig) *resty.Request {
	return p.client.R().
		SetContext(ctx).
		SetHeader("Authorization", "Bearer "+strings.TrimSpace(cfg.AccessToken)).
		SetHeader("Square-Version", squareAPIVersion).
		SetHeader("Content-Type", "application/json")
}

func (p *squareProvider) TestConnection(ctx context.Context, data map[string]any) (map[string]any, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return map[string]any{"config_valid": false, "error": err.Error()}, nil
	}
	resp, err := p.request(ctx, cfg).Get(p.baseURL(cfg) + "/v2/locations/" + strings.TrimSpace(cfg.LocationID))
	if err != nil {
		return map[string]any{"config_valid": true, "api_accessible": false, "error": err.Error()}, nil
	}
	if !resp.IsSuccess() {
		return map[string]any{
			"config_valid": true, "api_accessible": false,
			"status": resp.StatusCode(), "error": resp.String(),
		}, nil
	}
	var parsed struct {
		Location struct {
			Name     string `json:"name"`
			Currency string `json:"currency"`
			Status   string `json:"status"`
		} `json:"location"`
	}
	_ = json.Unmarshal(resp.Body(), &parsed)
	return map[string]any{
		"config_valid": true, "api_accessible": true,
		"location_name":     parsed.Location.Name,
		"location_currency": parsed.Location.Currency,
		"location_status":   parsed.Location.Status,
	}, nil
}

func (p *squareProvider) CreateOrder(ctx context.Context, data map[string]any, req PaymentOrderRequest) (*paymentdomain.PaymentPayload, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		// 订单号天然唯一，直接用作幂等键，重复下单不会产生第二条支付链接
		"idempotency_key": req.OrderNo,
		"quick_pay": map[string]any{
			"name": req.Subject,
			"price_money": map[string]any{
				"amount":   stripeMinorUnits(req.Amount, cfg.Currency),
				"currency": cfg.Currency,
			},
			"location_id": strings.TrimSpace(cfg.LocationID),
		},
		// 本地订单号写入备注，回调与查单据此定位
		"payment_note": req.OrderNo,
	}
	if returnURL := pickString(req.ReturnURL, cfg.ReturnURL); returnURL != "" {
		body["checkout_options"] = map[string]any{"redirect_url": returnURL}
	}

	resp, err := p.request(ctx, cfg).SetBody(body).Post(p.baseURL(cfg) + "/v2/online-checkout/payment-links")
	if err != nil {
		return nil, apperrors.New(50089, http.StatusBadGateway, "Square 下单失败："+err.Error())
	}
	if !resp.IsSuccess() {
		return nil, apperrors.New(50089, http.StatusBadGateway, "Square 下单失败："+resp.String())
	}
	var parsed struct {
		PaymentLink struct {
			ID      string `json:"id"`
			URL     string `json:"url"`
			OrderID string `json:"order_id"`
		} `json:"payment_link"`
	}
	if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
		return nil, apperrors.New(50089, http.StatusBadGateway, "Square 响应解析失败")
	}
	if parsed.PaymentLink.URL == "" {
		return nil, apperrors.New(50089, http.StatusBadGateway, "Square 未返回支付链接")
	}
	return &paymentdomain.PaymentPayload{
		Success:      true,
		OrderNo:      req.OrderNo,
		PaymentURL:   parsed.PaymentLink.URL,
		RedirectURL:  parsed.PaymentLink.URL,
		ProviderType: "square_payment_link",
		FormData: map[string]any{
			"paymentLinkId": parsed.PaymentLink.ID,
			"squareOrderId": parsed.PaymentLink.OrderID,
		},
	}, nil
}

func (p *squareProvider) QueryRemoteOrder(ctx context.Context, data map[string]any, orderNo string) (map[string]any, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	// Square 不支持按备注过滤，扫描最近支付记录匹配本地订单号
	resp, err := p.request(ctx, cfg).
		SetQueryParam("location_id", strings.TrimSpace(cfg.LocationID)).
		SetQueryParam("limit", "100").
		SetQueryParam("sort_order", "DESC").
		Get(p.baseURL(cfg) + "/v2/payments")
	if err != nil {
		return nil, apperrors.New(50089, http.StatusBadGateway, "Square 查询失败："+err.Error())
	}
	if !resp.IsSuccess() {
		return nil, apperrors.New(50089, http.StatusBadGateway, "Square 查询失败："+resp.String())
	}
	var parsed struct {
		Payments []squarePayment `json:"payments"`
	}
	if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
		return nil, apperrors.New(50089, http.StatusBadGateway, "Square 响应解析失败")
	}
	for _, payment := range parsed.Payments {
		if strings.TrimSpace(payment.Note) != orderNo {
			continue
		}
		return map[string]any{
			"payment_id": payment.ID,
			"order_id":   payment.OrderID,
			"status":     payment.Status,
			"amount":     stripeMajorUnits(payment.AmountMoney.Amount, payment.AmountMoney.Currency),
			"currency":   payment.AmountMoney.Currency,
			"paid":       payment.Status == "COMPLETED",
		}, nil
	}
	return map[string]any{
		"paid":    false,
		"message": "最近 100 笔 Square 支付记录中未找到该订单，可能已超出查询窗口",
	}, nil
}

// Refund Square 退款。退款针对 payment_id（回调落库的 provider_order_no），
// idempotency_key 用本地退款单号保证重试安全。
func (p *squareProvider) Refund(ctx context.Context, data map[string]any, req paymentdomain.RefundRequest) (*paymentdomain.RefundResult, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	paymentID := strings.TrimSpace(req.ProviderOrderNo)
	if paymentID == "" {
		return nil, apperrors.New(40098, http.StatusBadRequest, "Square 退款需要原支付单号（payment_id），订单缺少上游单号")
	}
	body := map[string]any{
		"idempotency_key": req.RefundNo,
		"payment_id":      paymentID,
		"amount_money": map[string]any{
			"amount":   stripeMinorUnits(req.RefundAmount, cfg.Currency),
			"currency": cfg.Currency,
		},
	}
	if reason := strings.TrimSpace(req.Reason); reason != "" {
		body["reason"] = reason
	}
	resp, err := p.request(ctx, cfg).SetBody(body).Post(p.baseURL(cfg) + "/v2/refunds")
	if err != nil {
		return nil, apperrors.New(50097, http.StatusBadGateway, "Square 退款请求失败："+err.Error())
	}
	if !resp.IsSuccess() {
		return &paymentdomain.RefundResult{
			Status:  paymentdomain.RefundFailed,
			Message: "Square 拒绝退款：" + resp.String(),
			Raw:     map[string]any{"status": resp.StatusCode(), "body": resp.String()},
		}, nil
	}
	return squareRefundResult(resp.Body(), cfg.Currency)
}

// QueryRefund 查询 Square 退款单状态（PENDING 态的补偿通道）
func (p *squareProvider) QueryRefund(ctx context.Context, data map[string]any, query paymentdomain.RefundQuery) (*paymentdomain.RefundResult, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	refundID := strings.TrimSpace(query.ProviderRefundNo)
	if refundID == "" {
		return nil, apperrors.New(40098, http.StatusBadRequest, "Square 退款查询需要上游退款单号")
	}
	resp, err := p.request(ctx, cfg).Get(p.baseURL(cfg) + "/v2/refunds/" + refundID)
	if err != nil {
		return nil, apperrors.New(50097, http.StatusBadGateway, "Square 退款查询失败："+err.Error())
	}
	if !resp.IsSuccess() {
		return nil, apperrors.New(50097, http.StatusBadGateway, "Square 退款查询失败："+resp.String())
	}
	return squareRefundResult(resp.Body(), cfg.Currency)
}

// squareRefundResult 解析 Square 退款单
func squareRefundResult(raw []byte, fallbackCurrency string) (*paymentdomain.RefundResult, error) {
	var parsed struct {
		Refund struct {
			ID          string `json:"id"`
			Status      string `json:"status"`
			PaymentID   string `json:"payment_id"`
			AmountMoney struct {
				Amount   int64  `json:"amount"`
				Currency string `json:"currency"`
			} `json:"amount_money"`
		} `json:"refund"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, apperrors.New(50097, http.StatusBadGateway, "Square 退款响应解析失败")
	}
	currency := firstNonEmpty(parsed.Refund.AmountMoney.Currency, fallbackCurrency)
	result := &paymentdomain.RefundResult{
		ProviderRefundNo: parsed.Refund.ID,
		Amount:           stripeMajorUnits(parsed.Refund.AmountMoney.Amount, currency),
		Raw: map[string]any{
			"refund_id": parsed.Refund.ID, "status": parsed.Refund.Status, "payment_id": parsed.Refund.PaymentID,
		},
	}
	switch strings.ToUpper(strings.TrimSpace(parsed.Refund.Status)) {
	case "COMPLETED":
		result.Status = paymentdomain.RefundSuccess
	case "REJECTED", "FAILED":
		result.Status = paymentdomain.RefundFailed
		result.Message = "Square 退款状态：" + parsed.Refund.Status
	default: // PENDING
		result.Status = paymentdomain.RefundProcessing
		result.Message = "Square 退款状态：" + parsed.Refund.Status
	}
	return result, nil
}

// squarePayment 支付记录（仅取所需字段）
type squarePayment struct {
	ID          string `json:"id"`
	OrderID     string `json:"order_id"`
	Status      string `json:"status"`
	Note        string `json:"note"`
	AmountMoney struct {
		Amount   int64  `json:"amount"`
		Currency string `json:"currency"`
	} `json:"amount_money"`
}

// squareEvent Webhook 报文结构
type squareEvent struct {
	Type string `json:"type"`
	Data struct {
		Object struct {
			Payment squarePayment `json:"payment"`
		} `json:"object"`
	} `json:"data"`
}

// ExtractOrderNo 从未验签报文预提取本地订单号（仅用于订单路由）
func (p *squareProvider) ExtractOrderNo(callbackData map[string]string) string {
	rawBody := callbackData[CallbackRawBodyKey]
	if rawBody == "" {
		return ""
	}
	var event squareEvent
	if err := json.Unmarshal([]byte(rawBody), &event); err != nil {
		return ""
	}
	return strings.TrimSpace(event.Data.Object.Payment.Note)
}

func (p *squareProvider) HandleCallback(ctx context.Context, data map[string]any, callbackData map[string]string, clientIP string) (*paymentdomain.CallbackResult, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	rawBody := callbackData[CallbackRawBodyKey]
	if rawBody == "" {
		return nil, apperrors.New(40075, http.StatusBadRequest, "Square Webhook 缺少原始报文")
	}
	notificationURL := p.signatureURL(cfg)
	if notificationURL == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "Square 未配置通知地址，无法验签（签名原文包含 URL）")
	}
	// Square 签名原文 = 通知地址 + 原始报文，编码为 Base64
	if err := verifyWebhookHMAC(cfg.WebhookSecret, notificationURL+rawBody,
		callbackHeader(callbackData, "x-square-hmacsha256-signature"), "base64", "Square"); err != nil {
		return nil, err
	}

	var event squareEvent
	if err := json.Unmarshal([]byte(rawBody), &event); err != nil {
		return nil, apperrors.New(40076, http.StatusBadRequest, "Square 事件解析失败")
	}
	payment := event.Data.Object.Payment
	currency := firstNonEmpty(payment.AmountMoney.Currency, cfg.Currency)
	paid := event.Type == "payment.updated" && payment.Status == "COMPLETED"
	return &paymentdomain.CallbackResult{
		Success:         true,
		Paid:            paid,
		OrderNo:         strings.TrimSpace(payment.Note),
		ProviderOrderNo: firstNonEmpty(payment.ID, payment.OrderID),
		TradeStatus:     firstNonEmpty(payment.Status, event.Type),
		PaymentMethod:   paymentdomain.MethodSquare,
		Amount:          stripeMajorUnits(payment.AmountMoney.Amount, currency),
		RawData: map[string]any{
			"event_type": event.Type,
			"payment_id": payment.ID,
			"order_id":   payment.OrderID,
			"status":     payment.Status,
			"currency":   currency,
		},
	}, nil
}

var _ callbackOrderExtractor = (*squareProvider)(nil)
