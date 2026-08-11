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

// ── Lemon Squeezy Provider ──
//
// 同为 MoR（记录商户）模式，面向独立开发者与小型 SaaS，开通门槛低于 Paddle。
//
// 下单：POST /v1/checkouts 以 custom_price 覆盖商品原价创建结账会话，返回托管收银台地址。
// 回调：X-Signature 头为原始报文的 HMAC-SHA256（hex），密钥为 Webhook 的 signing secret。

const lemonSqueezyAPI = "https://api.lemonsqueezy.com/v1"

type lemonSqueezyProvider struct {
	client *resty.Client
}

func newLemonSqueezyProvider(client *resty.Client) *lemonSqueezyProvider {
	return &lemonSqueezyProvider{client: client}
}

func (p *lemonSqueezyProvider) Name() string { return paymentdomain.MethodLemonSqueezy }

func (p *lemonSqueezyProvider) Describe() paymentdomain.ProviderMeta {
	return finalizeMeta(paymentdomain.ProviderMeta{
		Method:       paymentdomain.MethodLemonSqueezy,
		Name:         "Lemon Squeezy",
		Description:  "面向独立开发者的记录商户（MoR）平台，代收款并处理全球税务，开通门槛低、结账页开箱即用。",
		Category:     paymentdomain.CategoryInternational,
		Icon:         "lemonsqueezy",
		BrandColor:   "#FFC233",
		DocURL:       "https://docs.lemonsqueezy.com/api/checkouts/create-checkout",
		CallbackPath: "/api/public/pay/callback/" + paymentdomain.MethodLemonSqueezy,
		CallbackNote: "在 Settings → Webhooks 新建端点指向该地址，勾选 order_created 事件，并把 Signing secret 填入下方 Webhook 密钥。",
		Regions:      []string{"全球"},
		Currencies:   []string{"USD", "EUR", "GBP", "AUD", "CAD", "JPY"},
		Capabilities: paymentdomain.ProviderCapabilities{
			Redirect: true, Webhook: true, WebhookSignature: true, Sandbox: true,
			Refund: true, PartialRefund: true,
		},
		PayTypes: []paymentdomain.PayTypeOption{
			payType("card", "银行卡", "Lemon Squeezy 托管收银台"),
			payType("paypal", "PayPal", "需在店铺设置中开通"),
		},
		Fields: fields(
			inGroup(paymentdomain.GroupCredential,
				fSecret("apiKey", "API Key", "eyJ0eXAiOi...", "Settings → API 生成的服务端密钥", true),
				fText("storeId", "Store ID", "12345", "店铺数字 ID", true),
				fText("variantId", "Variant ID", "67890", "用于承载订单的商品变体 ID；金额由下单时的自定义价格覆盖", true),
				fSecret("webhookSecret", "Webhook 密钥", "Signing secret", "Webhook 端点的签名密钥，验签必需", true),
			),
			inGroup(paymentdomain.GroupGateway,
				fSelect("currency", "结算币种", "店铺结算币种（以 Lemon Squeezy 店铺设置为准）", "USD",
					opt("USD", "USD 美元"), opt("EUR", "EUR 欧元"), opt("GBP", "GBP 英镑"),
					opt("AUD", "AUD 澳元"), opt("CAD", "CAD 加元"), opt("JPY", "JPY 日元")),
				fURL("notifyUrl", "Webhook 地址", "https://your-domain.com/api/public/pay/callback/lemonsqueezy", "需与后台登记的端点一致"),
				fURL("returnUrl", "支付成功跳转", "https://your-domain.com/pay/success", "结账完成后的回跳地址"),
			),
			limitFields("1.00", "100000"),
			advanced(inGroup(paymentdomain.GroupAdvanced,
				fSwitch("testMode", "测试模式", "开启后创建测试结账会话，不产生真实扣款", false),
			)...),
		),
	})
}

func (p *lemonSqueezyProvider) decodeConfig(data map[string]any) (*paymentdomain.LemonSqueezyConfig, error) {
	cfg, err := decodeProviderConfig[paymentdomain.LemonSqueezyConfig](data)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "Lemon Squeezy 配置不完整：缺少 apiKey")
	}
	if strings.TrimSpace(cfg.StoreID) == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "Lemon Squeezy 配置不完整：缺少 storeId")
	}
	if strings.TrimSpace(cfg.VariantID) == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "Lemon Squeezy 配置不完整：缺少 variantId")
	}
	if cfg.Currency = strings.ToUpper(strings.TrimSpace(cfg.Currency)); cfg.Currency == "" {
		cfg.Currency = "USD"
	}
	return cfg, nil
}

func (p *lemonSqueezyProvider) ValidateConfig(data map[string]any) error {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.WebhookSecret) == "" {
		return apperrors.New(40078, http.StatusBadRequest, "Lemon Squeezy 配置不完整：缺少 webhookSecret（Webhook 验签必需）")
	}
	return nil
}

func (p *lemonSqueezyProvider) request(ctx context.Context, cfg *paymentdomain.LemonSqueezyConfig) *resty.Request {
	return p.client.R().
		SetContext(ctx).
		SetHeader("Authorization", "Bearer "+cfg.APIKey).
		SetHeader("Accept", "application/vnd.api+json").
		SetHeader("Content-Type", "application/vnd.api+json")
}

func (p *lemonSqueezyProvider) TestConnection(ctx context.Context, data map[string]any) (map[string]any, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return map[string]any{"config_valid": false, "error": err.Error()}, nil
	}
	resp, err := p.request(ctx, cfg).Get(lemonSqueezyAPI + "/stores/" + strings.TrimSpace(cfg.StoreID))
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
		Data struct {
			Attributes struct {
				Name     string `json:"name"`
				Currency string `json:"currency"`
			} `json:"attributes"`
		} `json:"data"`
	}
	_ = json.Unmarshal(resp.Body(), &parsed)
	return map[string]any{
		"config_valid": true, "api_accessible": true,
		"store_name":     parsed.Data.Attributes.Name,
		"store_currency": parsed.Data.Attributes.Currency,
	}, nil
}

func (p *lemonSqueezyProvider) CreateOrder(ctx context.Context, data map[string]any, req PaymentOrderRequest) (*paymentdomain.PaymentPayload, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	productOptions := map[string]any{"name": req.Subject}
	if desc := strings.TrimSpace(req.Body); desc != "" {
		productOptions["description"] = desc
	}
	if returnURL := pickString(req.ReturnURL, cfg.ReturnURL); returnURL != "" {
		productOptions["redirect_url"] = returnURL
	}

	body := map[string]any{
		"data": map[string]any{
			"type": "checkouts",
			"attributes": map[string]any{
				// custom_price 为最小货币单位整数，覆盖变体原价
				"custom_price":    stripeMinorUnits(req.Amount, cfg.Currency),
				"product_options": productOptions,
				"checkout_data": map[string]any{
					"custom": map[string]any{"order_no": req.OrderNo},
				},
				"test_mode": cfg.TestMode,
			},
			"relationships": map[string]any{
				"store": map[string]any{
					"data": map[string]any{"type": "stores", "id": strings.TrimSpace(cfg.StoreID)},
				},
				"variant": map[string]any{
					"data": map[string]any{"type": "variants", "id": strings.TrimSpace(cfg.VariantID)},
				},
			},
		},
	}

	resp, err := p.request(ctx, cfg).SetBody(body).Post(lemonSqueezyAPI + "/checkouts")
	if err != nil {
		return nil, apperrors.New(50086, http.StatusBadGateway, "Lemon Squeezy 下单失败："+err.Error())
	}
	if !resp.IsSuccess() {
		return nil, apperrors.New(50086, http.StatusBadGateway, "Lemon Squeezy 下单失败："+resp.String())
	}
	var parsed struct {
		Data struct {
			ID         string `json:"id"`
			Attributes struct {
				URL string `json:"url"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
		return nil, apperrors.New(50086, http.StatusBadGateway, "Lemon Squeezy 响应解析失败")
	}
	if parsed.Data.Attributes.URL == "" {
		return nil, apperrors.New(50086, http.StatusBadGateway, "Lemon Squeezy 未返回结账地址")
	}
	return &paymentdomain.PaymentPayload{
		Success:      true,
		OrderNo:      req.OrderNo,
		PaymentURL:   parsed.Data.Attributes.URL,
		RedirectURL:  parsed.Data.Attributes.URL,
		ProviderType: "lemonsqueezy_checkout",
		FormData:     map[string]any{"checkoutId": parsed.Data.ID},
	}, nil
}

func (p *lemonSqueezyProvider) QueryRemoteOrder(ctx context.Context, data map[string]any, orderNo string) (map[string]any, error) {
	// Lemon Squeezy 的订单列表接口不回显 checkout 自定义数据，无法按本地订单号反查。
	// 支付状态以 Webhook 回调 + 本地订单为准。
	_ = ctx
	_ = data
	return map[string]any{
		"paid":    false,
		"message": "Lemon Squeezy 不支持按商户订单号反查，订单状态以 Webhook 回调结果为准",
		"orderNo": orderNo,
	}, nil
}

// Refund Lemon Squeezy 退款。
// 退款针对订单 ID（Webhook 落库的 provider_order_no），金额以最小货币单位提交。
func (p *lemonSqueezyProvider) Refund(ctx context.Context, data map[string]any, req paymentdomain.RefundRequest) (*paymentdomain.RefundResult, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	orderID := strings.TrimSpace(req.ProviderOrderNo)
	if orderID == "" {
		return nil, apperrors.New(40098, http.StatusBadRequest, "Lemon Squeezy 退款需要上游订单号，订单缺少该字段")
	}
	body := map[string]any{
		"data": map[string]any{
			"type": "orders",
			"id":   orderID,
			"attributes": map[string]any{
				"amount": stripeMinorUnits(req.RefundAmount, cfg.Currency),
			},
		},
	}
	resp, err := p.request(ctx, cfg).SetBody(body).Post(lemonSqueezyAPI + "/orders/" + orderID + "/refund")
	if err != nil {
		return nil, apperrors.New(50099, http.StatusBadGateway, "Lemon Squeezy 退款请求失败："+err.Error())
	}
	if !resp.IsSuccess() {
		return &paymentdomain.RefundResult{
			Status:  paymentdomain.RefundFailed,
			Message: "Lemon Squeezy 拒绝退款：" + resp.String(),
			Raw:     map[string]any{"status": resp.StatusCode(), "body": resp.String()},
		}, nil
	}
	var parsed struct {
		Data struct {
			ID         string `json:"id"`
			Attributes struct {
				Status         string `json:"status"`
				RefundedAmount int64  `json:"refunded_amount"`
				Currency       string `json:"currency"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
		return nil, apperrors.New(50099, http.StatusBadGateway, "Lemon Squeezy 退款响应解析失败")
	}
	currency := firstNonEmpty(parsed.Data.Attributes.Currency, cfg.Currency)
	result := &paymentdomain.RefundResult{
		ProviderRefundNo: firstNonEmpty(parsed.Data.ID, req.RefundNo),
		Amount:           req.RefundAmount,
		Raw: map[string]any{
			"order_id": parsed.Data.ID, "status": parsed.Data.Attributes.Status, "currency": currency,
		},
	}
	// 接口 2xx 即表示退款已受理；order 状态转为 refunded/partial_refunded
	if strings.Contains(strings.ToLower(parsed.Data.Attributes.Status), "refund") {
		result.Status = paymentdomain.RefundSuccess
	} else {
		result.Status = paymentdomain.RefundSuccess
		result.Message = "Lemon Squeezy 已受理退款（订单状态：" + parsed.Data.Attributes.Status + "）"
	}
	if parsed.Data.Attributes.RefundedAmount > 0 {
		result.Amount = stripeMajorUnits(parsed.Data.Attributes.RefundedAmount, currency)
	}
	return result, nil
}

// lemonSqueezyEvent Webhook 报文结构（仅取所需字段）
type lemonSqueezyEvent struct {
	Meta struct {
		EventName  string `json:"event_name"`
		CustomData struct {
			OrderNo string `json:"order_no"`
		} `json:"custom_data"`
	} `json:"meta"`
	Data struct {
		ID         string `json:"id"`
		Attributes struct {
			Identifier string `json:"identifier"`
			Status     string `json:"status"`
			Currency   string `json:"currency"`
			Total      int64  `json:"total"`
		} `json:"attributes"`
	} `json:"data"`
}

// ExtractOrderNo 从未验签报文预提取本地订单号（仅用于订单路由）
func (p *lemonSqueezyProvider) ExtractOrderNo(callbackData map[string]string) string {
	rawBody := callbackData[CallbackRawBodyKey]
	if rawBody == "" {
		return ""
	}
	var event lemonSqueezyEvent
	if err := json.Unmarshal([]byte(rawBody), &event); err != nil {
		return ""
	}
	return event.Meta.CustomData.OrderNo
}

func (p *lemonSqueezyProvider) HandleCallback(ctx context.Context, data map[string]any, callbackData map[string]string, clientIP string) (*paymentdomain.CallbackResult, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	rawBody := callbackData[CallbackRawBodyKey]
	if rawBody == "" {
		return nil, apperrors.New(40075, http.StatusBadRequest, "Lemon Squeezy Webhook 缺少原始报文")
	}
	if err := verifyWebhookHMAC(cfg.WebhookSecret, rawBody, callbackHeader(callbackData, "X-Signature"), "hex", "Lemon Squeezy"); err != nil {
		return nil, err
	}

	var event lemonSqueezyEvent
	if err := json.Unmarshal([]byte(rawBody), &event); err != nil {
		return nil, apperrors.New(40076, http.StatusBadRequest, "Lemon Squeezy 事件解析失败")
	}
	currency := firstNonEmpty(event.Data.Attributes.Currency, cfg.Currency)
	// order_created 事件中 status=paid 才视为已支付
	paid := event.Meta.EventName == "order_created" && strings.EqualFold(event.Data.Attributes.Status, "paid")
	return &paymentdomain.CallbackResult{
		Success:         true,
		Paid:            paid,
		OrderNo:         event.Meta.CustomData.OrderNo,
		ProviderOrderNo: firstNonEmpty(event.Data.Attributes.Identifier, event.Data.ID),
		TradeStatus:     event.Meta.EventName,
		PaymentMethod:   paymentdomain.MethodLemonSqueezy,
		Amount:          stripeMajorUnits(event.Data.Attributes.Total, currency),
		RawData: map[string]any{
			"event_name": event.Meta.EventName,
			"order_id":   event.Data.ID,
			"identifier": event.Data.Attributes.Identifier,
			"status":     event.Data.Attributes.Status,
			"currency":   currency,
		},
	}, nil
}

// 编译期确认实现了订单号预提取能力（Webhook 报文无 out_trade_no 表单字段）
var _ callbackOrderExtractor = (*lemonSqueezyProvider)(nil)
