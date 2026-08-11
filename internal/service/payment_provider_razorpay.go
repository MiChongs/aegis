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

// ── Razorpay Provider（印度主流收单）──
//
// 下单：POST /v1/payment_links 创建支付链接，reference_id 直接使用本地订单号，
//       因此**支持按商户订单号反查**（多数国际渠道做不到这点）。
// 回调：X-Razorpay-Signature 为原始报文的 HMAC-SHA256（hex），密钥为 Webhook secret。

const razorpayAPI = "https://api.razorpay.com/v1"

type razorpayProvider struct {
	client *resty.Client
}

func newRazorpayProvider(client *resty.Client) *razorpayProvider {
	return &razorpayProvider{client: client}
}

func (p *razorpayProvider) Name() string { return paymentdomain.MethodRazorpay }

func (p *razorpayProvider) Describe() paymentdomain.ProviderMeta {
	return finalizeMeta(paymentdomain.ProviderMeta{
		Method:       paymentdomain.MethodRazorpay,
		Name:         "Razorpay",
		Description:  "印度市场占有率最高的收单平台，覆盖 UPI / 银行卡 / 网银 / 钱包，支持按商户订单号反查。",
		Category:     paymentdomain.CategoryInternational,
		Icon:         "razorpay",
		BrandColor:   "#0C2451",
		DocURL:       "https://razorpay.com/docs/api/payments/payment-links/",
		CallbackPath: "/api/public/pay/callback/" + paymentdomain.MethodRazorpay,
		CallbackNote: "在 Dashboard → Settings → Webhooks 新建端点指向该地址，订阅 payment_link.paid 与 payment.captured，并把 Secret 填入下方 Webhook 密钥。",
		Regions:      []string{"印度"},
		Currencies:   []string{"INR", "USD"},
		Capabilities: paymentdomain.ProviderCapabilities{
			Redirect: true, Webhook: true, WebhookSignature: true, RemoteQuery: true, Sandbox: true,
			Refund: true, PartialRefund: true, RefundQuery: true,
		},
		PayTypes: []paymentdomain.PayTypeOption{
			payType("upi", "UPI", "印度统一支付接口"),
			payType("card", "银行卡", "Visa / Mastercard / RuPay"),
			payType("netbanking", "网银", ""),
			payType("wallet", "钱包", "Paytm / PhonePe 等"),
		},
		Fields: fields(
			inGroup(paymentdomain.GroupCredential,
				fText("keyId", "Key ID", "rzp_live_...", "Dashboard → API Keys 生成；rzp_test_ 前缀为测试环境", true),
				fSecret("keySecret", "Key Secret", "API Secret", "与 Key ID 成对使用的服务端密钥", true),
				fSecret("webhookSecret", "Webhook 密钥", "Webhook Secret", "Webhook 端点的签名密钥，验签必需", true),
			),
			inGroup(paymentdomain.GroupGateway,
				fSelect("currency", "结算币种", "Razorpay 主账户通常为 INR", "INR",
					opt("INR", "INR 印度卢比"), opt("USD", "USD 美元")),
				fURL("notifyUrl", "Webhook 地址", "https://your-domain.com/api/public/pay/callback/razorpay", "需与后台登记的端点一致"),
				fURL("returnUrl", "支付成功跳转", "https://your-domain.com/pay/success", "付款完成后的回跳地址"),
			),
			limitFields("1.00", "500000"),
		),
	})
}

func (p *razorpayProvider) decodeConfig(data map[string]any) (*paymentdomain.RazorpayConfig, error) {
	cfg, err := decodeProviderConfig[paymentdomain.RazorpayConfig](data)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.KeyID) == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "Razorpay 配置不完整：缺少 keyId")
	}
	if strings.TrimSpace(cfg.KeySecret) == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "Razorpay 配置不完整：缺少 keySecret")
	}
	if cfg.Currency = strings.ToUpper(strings.TrimSpace(cfg.Currency)); cfg.Currency == "" {
		cfg.Currency = "INR"
	}
	return cfg, nil
}

func (p *razorpayProvider) ValidateConfig(data map[string]any) error {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.WebhookSecret) == "" {
		return apperrors.New(40078, http.StatusBadRequest, "Razorpay 配置不完整：缺少 webhookSecret（Webhook 验签必需）")
	}
	return nil
}

func (p *razorpayProvider) request(ctx context.Context, cfg *paymentdomain.RazorpayConfig) *resty.Request {
	return p.client.R().
		SetContext(ctx).
		SetBasicAuth(strings.TrimSpace(cfg.KeyID), strings.TrimSpace(cfg.KeySecret)).
		SetHeader("Content-Type", "application/json")
}

func (p *razorpayProvider) TestConnection(ctx context.Context, data map[string]any) (map[string]any, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return map[string]any{"config_valid": false, "error": err.Error()}, nil
	}
	// 拉取一条支付记录验证密钥有效性（只读、无副作用）
	resp, err := p.request(ctx, cfg).SetQueryParam("count", "1").Get(razorpayAPI + "/payments")
	if err != nil {
		return map[string]any{"config_valid": true, "api_accessible": false, "error": err.Error()}, nil
	}
	if !resp.IsSuccess() {
		return map[string]any{
			"config_valid": true, "api_accessible": false,
			"status": resp.StatusCode(), "error": resp.String(),
		}, nil
	}
	return map[string]any{
		"config_valid": true, "api_accessible": true,
		"environment": map[bool]string{true: "test", false: "live"}[strings.HasPrefix(cfg.KeyID, "rzp_test")],
	}, nil
}

func (p *razorpayProvider) CreateOrder(ctx context.Context, data map[string]any, req PaymentOrderRequest) (*paymentdomain.PaymentPayload, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"amount":       stripeMinorUnits(req.Amount, cfg.Currency), // 派萨（1/100 卢比）
		"currency":     cfg.Currency,
		"description":  pickString(req.Subject, req.OrderNo),
		"reference_id": req.OrderNo,
		"notes":        map[string]any{"order_no": req.OrderNo},
	}
	if returnURL := pickString(req.ReturnURL, cfg.ReturnURL); returnURL != "" {
		body["callback_url"] = returnURL
		body["callback_method"] = "get"
	}
	if req.ExpireAt != nil {
		body["expire_by"] = req.ExpireAt.Unix()
	}

	resp, err := p.request(ctx, cfg).SetBody(body).Post(razorpayAPI + "/payment_links")
	if err != nil {
		return nil, apperrors.New(50087, http.StatusBadGateway, "Razorpay 下单失败："+err.Error())
	}
	if !resp.IsSuccess() {
		return nil, apperrors.New(50087, http.StatusBadGateway, "Razorpay 下单失败："+resp.String())
	}
	var parsed struct {
		ID       string `json:"id"`
		ShortURL string `json:"short_url"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
		return nil, apperrors.New(50087, http.StatusBadGateway, "Razorpay 响应解析失败")
	}
	if parsed.ShortURL == "" {
		return nil, apperrors.New(50087, http.StatusBadGateway, "Razorpay 未返回支付链接")
	}
	return &paymentdomain.PaymentPayload{
		Success:      true,
		OrderNo:      req.OrderNo,
		PaymentURL:   parsed.ShortURL,
		RedirectURL:  parsed.ShortURL,
		ProviderType: "razorpay_link",
		FormData:     map[string]any{"paymentLinkId": parsed.ID, "status": parsed.Status},
	}, nil
}

func (p *razorpayProvider) QueryRemoteOrder(ctx context.Context, data map[string]any, orderNo string) (map[string]any, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	// reference_id 即本地订单号，Razorpay 原生支持按其过滤
	resp, err := p.request(ctx, cfg).
		SetQueryParam("reference_id", orderNo).
		Get(razorpayAPI + "/payment_links")
	if err != nil {
		return nil, apperrors.New(50087, http.StatusBadGateway, "Razorpay 查询失败："+err.Error())
	}
	if !resp.IsSuccess() {
		return nil, apperrors.New(50087, http.StatusBadGateway, "Razorpay 查询失败："+resp.String())
	}
	var parsed struct {
		PaymentLinks []struct {
			ID          string `json:"id"`
			Status      string `json:"status"`
			Amount      int64  `json:"amount"`
			AmountPaid  int64  `json:"amount_paid"`
			Currency    string `json:"currency"`
			ReferenceID string `json:"reference_id"`
		} `json:"payment_links"`
	}
	if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
		return nil, apperrors.New(50087, http.StatusBadGateway, "Razorpay 响应解析失败")
	}
	if len(parsed.PaymentLinks) == 0 {
		return map[string]any{"paid": false, "message": "未找到对应的 Razorpay 支付链接"}, nil
	}
	link := parsed.PaymentLinks[0]
	return map[string]any{
		"payment_link_id": link.ID,
		"status":          link.Status,
		"amount":          stripeMajorUnits(link.Amount, link.Currency),
		"amount_paid":     stripeMajorUnits(link.AmountPaid, link.Currency),
		"currency":        link.Currency,
		"paid":            link.Status == "paid",
	}, nil
}

// Refund Razorpay 退款。
// 退款针对 payment_id（回调时落库的 provider_order_no）；
// 本地退款单号写入 notes 并作为幂等键头，避免重试产生第二笔退款。
func (p *razorpayProvider) Refund(ctx context.Context, data map[string]any, req paymentdomain.RefundRequest) (*paymentdomain.RefundResult, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	paymentID := strings.TrimSpace(req.ProviderOrderNo)
	if paymentID == "" {
		return nil, apperrors.New(40098, http.StatusBadRequest, "Razorpay 退款需要原支付单号（payment_id），订单缺少上游单号")
	}
	body := map[string]any{
		"amount": stripeMinorUnits(req.RefundAmount, cfg.Currency),
		"speed":  "normal",
		"notes":  map[string]any{"order_no": req.OrderNo, "refund_no": req.RefundNo},
	}
	resp, err := p.request(ctx, cfg).
		SetHeader("X-Payment-Idempotency-Key", req.RefundNo).
		SetBody(body).
		Post(razorpayAPI + "/payments/" + paymentID + "/refund")
	if err != nil {
		return nil, apperrors.New(50096, http.StatusBadGateway, "Razorpay 退款请求失败："+err.Error())
	}
	if !resp.IsSuccess() {
		return &paymentdomain.RefundResult{
			Status:  paymentdomain.RefundFailed,
			Message: "Razorpay 拒绝退款：" + resp.String(),
			Raw:     map[string]any{"status": resp.StatusCode(), "body": resp.String()},
		}, nil
	}
	return razorpayRefundResult(resp.Body(), cfg.Currency)
}

// QueryRefund 查询 Razorpay 退款单状态
func (p *razorpayProvider) QueryRefund(ctx context.Context, data map[string]any, query paymentdomain.RefundQuery) (*paymentdomain.RefundResult, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	refundID := strings.TrimSpace(query.ProviderRefundNo)
	if refundID == "" {
		return nil, apperrors.New(40098, http.StatusBadRequest, "Razorpay 退款查询需要上游退款单号")
	}
	resp, err := p.request(ctx, cfg).Get(razorpayAPI + "/refunds/" + refundID)
	if err != nil {
		return nil, apperrors.New(50096, http.StatusBadGateway, "Razorpay 退款查询失败："+err.Error())
	}
	if !resp.IsSuccess() {
		return nil, apperrors.New(50096, http.StatusBadGateway, "Razorpay 退款查询失败："+resp.String())
	}
	return razorpayRefundResult(resp.Body(), cfg.Currency)
}

// razorpayRefundResult 解析 Razorpay 退款单
func razorpayRefundResult(raw []byte, fallbackCurrency string) (*paymentdomain.RefundResult, error) {
	var parsed struct {
		ID       string `json:"id"`
		Amount   int64  `json:"amount"`
		Currency string `json:"currency"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, apperrors.New(50096, http.StatusBadGateway, "Razorpay 退款响应解析失败")
	}
	currency := firstNonEmpty(parsed.Currency, fallbackCurrency)
	result := &paymentdomain.RefundResult{
		ProviderRefundNo: parsed.ID,
		Amount:           stripeMajorUnits(parsed.Amount, currency),
		Raw:              map[string]any{"refund_id": parsed.ID, "status": parsed.Status, "currency": currency},
	}
	switch strings.ToLower(strings.TrimSpace(parsed.Status)) {
	case "processed":
		result.Status = paymentdomain.RefundSuccess
	case "failed":
		result.Status = paymentdomain.RefundFailed
		result.Message = "Razorpay 退款失败"
	default: // pending
		result.Status = paymentdomain.RefundProcessing
		result.Message = "Razorpay 退款状态：" + parsed.Status
	}
	return result, nil
}

// razorpayEvent Webhook 报文结构（仅取所需字段）
type razorpayEvent struct {
	Event   string `json:"event"`
	Payload struct {
		PaymentLink struct {
			Entity struct {
				ID          string `json:"id"`
				Status      string `json:"status"`
				ReferenceID string `json:"reference_id"`
				Amount      int64  `json:"amount"`
				AmountPaid  int64  `json:"amount_paid"`
				Currency    string `json:"currency"`
			} `json:"entity"`
		} `json:"payment_link"`
		Payment struct {
			Entity struct {
				ID       string            `json:"id"`
				Status   string            `json:"status"`
				Amount   int64             `json:"amount"`
				Currency string            `json:"currency"`
				Notes    map[string]string `json:"notes"`
			} `json:"entity"`
		} `json:"payment"`
	} `json:"payload"`
}

// orderNo 从事件中解析本地订单号：优先支付链接的 reference_id，其次 payment.notes
func (e *razorpayEvent) orderNo() string {
	if ref := strings.TrimSpace(e.Payload.PaymentLink.Entity.ReferenceID); ref != "" {
		return ref
	}
	return strings.TrimSpace(e.Payload.Payment.Entity.Notes["order_no"])
}

// ExtractOrderNo 从未验签报文预提取本地订单号（仅用于订单路由）
func (p *razorpayProvider) ExtractOrderNo(callbackData map[string]string) string {
	rawBody := callbackData[CallbackRawBodyKey]
	if rawBody == "" {
		return ""
	}
	var event razorpayEvent
	if err := json.Unmarshal([]byte(rawBody), &event); err != nil {
		return ""
	}
	return event.orderNo()
}

func (p *razorpayProvider) HandleCallback(ctx context.Context, data map[string]any, callbackData map[string]string, clientIP string) (*paymentdomain.CallbackResult, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	rawBody := callbackData[CallbackRawBodyKey]
	if rawBody == "" {
		return nil, apperrors.New(40075, http.StatusBadRequest, "Razorpay Webhook 缺少原始报文")
	}
	if err := verifyWebhookHMAC(cfg.WebhookSecret, rawBody, callbackHeader(callbackData, "X-Razorpay-Signature"), "hex", "Razorpay"); err != nil {
		return nil, err
	}

	var event razorpayEvent
	if err := json.Unmarshal([]byte(rawBody), &event); err != nil {
		return nil, apperrors.New(40076, http.StatusBadRequest, "Razorpay 事件解析失败")
	}

	// 金额优先取支付链接实付额，其次取单笔支付额
	minor := event.Payload.PaymentLink.Entity.AmountPaid
	currency := firstNonEmpty(event.Payload.PaymentLink.Entity.Currency, event.Payload.Payment.Entity.Currency, cfg.Currency)
	providerOrderNo := event.Payload.PaymentLink.Entity.ID
	if minor <= 0 {
		minor = event.Payload.Payment.Entity.Amount
	}
	if paymentID := strings.TrimSpace(event.Payload.Payment.Entity.ID); paymentID != "" {
		providerOrderNo = paymentID
	}

	paid := event.Event == "payment_link.paid" ||
		(event.Event == "payment.captured" && event.Payload.Payment.Entity.Status == "captured")

	return &paymentdomain.CallbackResult{
		Success:         true,
		Paid:            paid,
		OrderNo:         event.orderNo(),
		ProviderOrderNo: providerOrderNo,
		TradeStatus:     event.Event,
		PaymentMethod:   paymentdomain.MethodRazorpay,
		Amount:          stripeMajorUnits(minor, currency),
		RawData: map[string]any{
			"event":           event.Event,
			"payment_link_id": event.Payload.PaymentLink.Entity.ID,
			"payment_id":      event.Payload.Payment.Entity.ID,
			"currency":        currency,
		},
	}, nil
}

var _ callbackOrderExtractor = (*razorpayProvider)(nil)
