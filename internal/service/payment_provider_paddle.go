package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	paymentdomain "aegis/internal/domain/payment"
	apperrors "aegis/pkg/errors"

	"github.com/go-resty/resty/v2"
	"github.com/shopspring/decimal"
)

// ── Paddle Billing (v2) Provider ──
//
// Paddle 属 MoR（Merchant of Record）模式：由 Paddle 作为记录商户代收并承担全球税务合规，
// 适合面向海外销售的 SaaS / 数字商品。
//
// 下单：POST /transactions 创建一笔含自定义价格的交易，返回托管收银台地址。
// 回调：Paddle-Signature 头形如 `ts=<秒级时间戳>;h1=<hex>`，
//       签名原文为 `<ts>:<原始报文>`，用通知密钥做 HMAC-SHA256。

type paddleProvider struct {
	client *resty.Client
}

func newPaddleProvider(client *resty.Client) *paddleProvider {
	return &paddleProvider{client: client}
}

func (p *paddleProvider) Name() string { return paymentdomain.MethodPaddle }

func (p *paddleProvider) Describe() paymentdomain.ProviderMeta {
	return finalizeMeta(paymentdomain.ProviderMeta{
		Method:       paymentdomain.MethodPaddle,
		Name:         "Paddle",
		Description:  "记录商户（MoR）模式的海外订阅收单，由 Paddle 代收款并处理全球增值税/销售税合规，是出海 SaaS 的主流选择。",
		Category:     paymentdomain.CategoryInternational,
		Icon:         "paddle",
		BrandColor:   "#FDDD35",
		DocURL:       "https://developer.paddle.com/api-reference/transactions/create-transaction",
		CallbackPath: "/api/public/pay/callback/" + paymentdomain.MethodPaddle,
		CallbackNote: "在 Paddle 后台 Notifications 中新建 Webhook 指向该地址，订阅 transaction.completed 与 transaction.paid，并把该通知设置的密钥（pdl_ntfset_…）填入下方 Webhook 密钥。",
		Regions:      []string{"全球"},
		Currencies:   []string{"USD", "EUR", "GBP", "JPY", "AUD", "CAD", "SGD", "HKD"},
		Capabilities: paymentdomain.ProviderCapabilities{
			Redirect: true, Webhook: true, WebhookSignature: true, RemoteQuery: true, Sandbox: true,
			// Paddle 退款需后台人工审批，提交后为 pending_approval，故只受理不即时到账
			Refund: true, PartialRefund: true, RefundQuery: true,
		},
		PayTypes: []paymentdomain.PayTypeOption{
			payType("card", "银行卡", "Paddle 托管收银台"),
			payType("paypal", "PayPal", "需在 Paddle 后台开通"),
		},
		Fields: fields(
			inGroup(paymentdomain.GroupCredential,
				fSecret("apiKey", "API Key", "pdl_live_...", "Paddle 后台 Authentication 生成的服务端密钥", true),
				fSecret("webhookSecret", "Webhook 密钥", "pdl_ntfset_...", "通知设置的签名密钥，验签必需", true),
			),
			inGroup(paymentdomain.GroupGateway,
				fSelect("currency", "结算币种", "交易计价币种", "USD",
					opt("USD", "USD 美元"), opt("EUR", "EUR 欧元"), opt("GBP", "GBP 英镑"),
					opt("JPY", "JPY 日元"), opt("AUD", "AUD 澳元"), opt("CAD", "CAD 加元"),
					opt("SGD", "SGD 新加坡元"), opt("HKD", "HKD 港币")),
				fURL("notifyUrl", "Webhook 地址", "https://your-domain.com/api/public/pay/callback/paddle", "需与 Paddle 后台登记的通知地址一致"),
				fURL("returnUrl", "支付成功跳转", "https://your-domain.com/pay/success", "结账完成后的回跳地址"),
			),
			limitFields("1.00", "100000"),
			advanced(inGroup(paymentdomain.GroupAdvanced,
				fSwitch("isSandbox", "沙箱环境", "开启后请求 sandbox-api.paddle.com", false),
				fText("priceId", "Price ID", "pri_...", "留空则每单动态创建价格；填写后以该目录价下单", false),
			)...),
		),
	})
}

func (p *paddleProvider) decodeConfig(data map[string]any) (*paymentdomain.PaddleConfig, error) {
	cfg, err := decodeProviderConfig[paymentdomain.PaddleConfig](data)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "Paddle 配置不完整：缺少 apiKey")
	}
	if cfg.Currency = strings.ToUpper(strings.TrimSpace(cfg.Currency)); cfg.Currency == "" {
		cfg.Currency = "USD"
	}
	return cfg, nil
}

func (p *paddleProvider) ValidateConfig(data map[string]any) error {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.WebhookSecret) == "" {
		return apperrors.New(40078, http.StatusBadRequest, "Paddle 配置不完整：缺少 webhookSecret（Webhook 验签必需）")
	}
	return nil
}

func (p *paddleProvider) baseURL(cfg *paymentdomain.PaddleConfig) string {
	if cfg.IsSandbox {
		return "https://sandbox-api.paddle.com"
	}
	return "https://api.paddle.com"
}

func (p *paddleProvider) request(ctx context.Context, cfg *paymentdomain.PaddleConfig) *resty.Request {
	return p.client.R().
		SetContext(ctx).
		SetHeader("Authorization", "Bearer "+cfg.APIKey).
		SetHeader("Content-Type", "application/json")
}

func (p *paddleProvider) TestConnection(ctx context.Context, data map[string]any) (map[string]any, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return map[string]any{"config_valid": false, "error": err.Error()}, nil
	}
	// 拉取一条事件类型列表验证密钥有效性（只读、无副作用）
	resp, err := p.request(ctx, cfg).Get(p.baseURL(cfg) + "/event-types")
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
		"environment": map[bool]string{true: "sandbox", false: "production"}[cfg.IsSandbox],
	}, nil
}

func (p *paddleProvider) CreateOrder(ctx context.Context, data map[string]any, req PaymentOrderRequest) (*paymentdomain.PaymentPayload, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}

	// Paddle 金额为字符串形式的最小货币单位
	item := map[string]any{"quantity": 1}
	if priceID := strings.TrimSpace(cfg.PriceID); priceID != "" {
		item["price_id"] = priceID
	} else {
		item["price"] = map[string]any{
			"description": pickString(req.Body, req.Subject),
			"name":        req.Subject,
			"unit_price": map[string]any{
				"amount":        strconv.FormatInt(stripeMinorUnits(req.Amount, cfg.Currency), 10),
				"currency_code": cfg.Currency,
			},
			"product": map[string]any{
				"name":         req.Subject,
				"tax_category": "standard",
			},
		}
	}

	body := map[string]any{
		"items":           []any{item},
		"collection_mode": "automatic",
		"custom_data":     map[string]any{"order_no": req.OrderNo},
	}
	if returnURL := pickString(req.ReturnURL, cfg.ReturnURL); returnURL != "" {
		body["checkout"] = map[string]any{"url": returnURL}
	}

	resp, err := p.request(ctx, cfg).SetBody(body).Post(p.baseURL(cfg) + "/transactions")
	if err != nil {
		return nil, apperrors.New(50085, http.StatusBadGateway, "Paddle 下单失败："+err.Error())
	}
	if !resp.IsSuccess() {
		return nil, apperrors.New(50085, http.StatusBadGateway, "Paddle 下单失败："+resp.String())
	}

	var parsed struct {
		Data struct {
			ID       string `json:"id"`
			Status   string `json:"status"`
			Checkout struct {
				URL string `json:"url"`
			} `json:"checkout"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
		return nil, apperrors.New(50085, http.StatusBadGateway, "Paddle 响应解析失败")
	}
	if parsed.Data.Checkout.URL == "" {
		return nil, apperrors.New(50085, http.StatusBadGateway, "Paddle 未返回收银台地址，请检查该交易是否允许托管结账")
	}
	return &paymentdomain.PaymentPayload{
		Success:      true,
		OrderNo:      req.OrderNo,
		PaymentURL:   parsed.Data.Checkout.URL,
		RedirectURL:  parsed.Data.Checkout.URL,
		ProviderType: "paddle_checkout",
		FormData:     map[string]any{"transactionId": parsed.Data.ID, "status": parsed.Data.Status},
	}, nil
}

func (p *paddleProvider) QueryRemoteOrder(ctx context.Context, data map[string]any, orderNo string) (map[string]any, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	// Paddle 不支持按 custom_data 过滤，改为扫描最近交易匹配本地订单号
	resp, err := p.request(ctx, cfg).
		SetQueryParam("per_page", "100").
		SetQueryParam("order_by", "created_at[DESC]").
		Get(p.baseURL(cfg) + "/transactions")
	if err != nil {
		return nil, apperrors.New(50085, http.StatusBadGateway, "Paddle 查询失败："+err.Error())
	}
	if !resp.IsSuccess() {
		return nil, apperrors.New(50085, http.StatusBadGateway, "Paddle 查询失败："+resp.String())
	}
	var parsed struct {
		Data []struct {
			ID         string          `json:"id"`
			Status     string          `json:"status"`
			CustomData paddleOrderMeta `json:"custom_data"`
			Details    struct {
				Totals struct {
					GrandTotal string `json:"grand_total"`
					Currency   string `json:"currency_code"`
				} `json:"totals"`
			} `json:"details"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
		return nil, apperrors.New(50085, http.StatusBadGateway, "Paddle 响应解析失败")
	}
	for _, txn := range parsed.Data {
		if txn.CustomData.OrderNo != orderNo {
			continue
		}
		return map[string]any{
			"transaction_id": txn.ID,
			"status":         txn.Status,
			"amount":         txn.Details.Totals.GrandTotal,
			"currency":       txn.Details.Totals.Currency,
			"paid":           txn.Status == "completed" || txn.Status == "paid",
		}, nil
	}
	return map[string]any{
		"paid":    false,
		"message": "最近 100 笔 Paddle 交易中未找到该订单，可能已超出查询窗口",
	}, nil
}

// Refund Paddle 退款（Adjustments API）。
//
// Paddle 的退款以「调整单（adjustment）」表达，且必须落到交易明细行（transaction item）上，
// 因此先取交易详情拿到 item_id，再按金额提交部分/全额退款。
// 注意：Paddle 退款需商户后台人工审批，提交后状态为 pending_approval，
// 因此这里返回 processing 而非 success —— 真正到账由后续查询或 adjustment.updated 事件确认。
func (p *paddleProvider) Refund(ctx context.Context, data map[string]any, req paymentdomain.RefundRequest) (*paymentdomain.RefundResult, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	transactionID := strings.TrimSpace(req.ProviderOrderNo)
	if transactionID == "" {
		return nil, apperrors.New(40098, http.StatusBadRequest, "Paddle 退款需要原交易号（transaction_id），订单缺少上游单号")
	}

	itemID, err := p.firstTransactionItemID(ctx, cfg, transactionID)
	if err != nil {
		return nil, err
	}

	item := map[string]any{"item_id": itemID}
	if req.RefundAmount.GreaterThanOrEqual(req.TotalAmount) {
		item["type"] = "full"
	} else {
		item["type"] = "partial"
		item["amount"] = strconv.FormatInt(stripeMinorUnits(req.RefundAmount, cfg.Currency), 10)
	}

	resp, err := p.request(ctx, cfg).SetBody(map[string]any{
		"action":         "refund",
		"transaction_id": transactionID,
		"reason":         pickString(req.Reason, "merchant refund"),
		"items":          []any{item},
	}).Post(p.baseURL(cfg) + "/adjustments")
	if err != nil {
		return nil, apperrors.New(50098, http.StatusBadGateway, "Paddle 退款请求失败："+err.Error())
	}
	if !resp.IsSuccess() {
		return &paymentdomain.RefundResult{
			Status:  paymentdomain.RefundFailed,
			Message: "Paddle 拒绝退款：" + resp.String(),
			Raw:     map[string]any{"status": resp.StatusCode(), "body": resp.String()},
		}, nil
	}
	return paddleAdjustmentResult(resp.Body(), cfg.Currency)
}

// QueryRefund 查询调整单状态（人工审批通过后转为 approved）
func (p *paddleProvider) QueryRefund(ctx context.Context, data map[string]any, query paymentdomain.RefundQuery) (*paymentdomain.RefundResult, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	adjustmentID := strings.TrimSpace(query.ProviderRefundNo)
	if adjustmentID == "" {
		return nil, apperrors.New(40098, http.StatusBadRequest, "Paddle 退款查询需要上游调整单号")
	}
	resp, err := p.request(ctx, cfg).
		SetQueryParam("id", adjustmentID).
		Get(p.baseURL(cfg) + "/adjustments")
	if err != nil {
		return nil, apperrors.New(50098, http.StatusBadGateway, "Paddle 退款查询失败："+err.Error())
	}
	if !resp.IsSuccess() {
		return nil, apperrors.New(50098, http.StatusBadGateway, "Paddle 退款查询失败："+resp.String())
	}
	// 列表接口返回数组，取首条
	var listed struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(resp.Body(), &listed); err != nil || len(listed.Data) == 0 {
		return &paymentdomain.RefundResult{
			Status:  paymentdomain.RefundProcessing,
			Message: "Paddle 未返回该调整单",
		}, nil
	}
	wrapped, _ := json.Marshal(map[string]any{"data": json.RawMessage(listed.Data[0])})
	return paddleAdjustmentResult(wrapped, cfg.Currency)
}

// firstTransactionItemID 取交易的第一条明细行 ID（Paddle 调整单必须挂在明细行上）
func (p *paddleProvider) firstTransactionItemID(ctx context.Context, cfg *paymentdomain.PaddleConfig, transactionID string) (string, error) {
	resp, err := p.request(ctx, cfg).Get(p.baseURL(cfg) + "/transactions/" + transactionID)
	if err != nil {
		return "", apperrors.New(50098, http.StatusBadGateway, "Paddle 交易详情获取失败："+err.Error())
	}
	if !resp.IsSuccess() {
		return "", apperrors.New(50098, http.StatusBadGateway, "Paddle 交易详情获取失败："+resp.String())
	}
	var parsed struct {
		Data struct {
			Details struct {
				LineItems []struct {
					ID string `json:"id"`
				} `json:"line_items"`
			} `json:"details"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
		return "", apperrors.New(50098, http.StatusBadGateway, "Paddle 交易详情解析失败")
	}
	for _, item := range parsed.Data.Details.LineItems {
		if strings.TrimSpace(item.ID) != "" {
			return item.ID, nil
		}
	}
	return "", apperrors.New(50098, http.StatusBadGateway, "Paddle 交易无可退明细行")
}

// paddleAdjustmentResult 解析调整单为统一退款结果
func paddleAdjustmentResult(raw []byte, fallbackCurrency string) (*paymentdomain.RefundResult, error) {
	var parsed struct {
		Data struct {
			ID           string `json:"id"`
			Status       string `json:"status"`
			CurrencyCode string `json:"currency_code"`
			Totals       struct {
				Total string `json:"total"`
			} `json:"totals"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, apperrors.New(50098, http.StatusBadGateway, "Paddle 调整单解析失败")
	}
	currency := firstNonEmpty(parsed.Data.CurrencyCode, fallbackCurrency)
	result := &paymentdomain.RefundResult{
		ProviderRefundNo: parsed.Data.ID,
		Raw:              map[string]any{"adjustment_id": parsed.Data.ID, "status": parsed.Data.Status, "currency": currency},
	}
	if minor, err := strconv.ParseInt(strings.TrimSpace(parsed.Data.Totals.Total), 10, 64); err == nil {
		result.Amount = stripeMajorUnits(minor, currency)
	}
	switch strings.ToLower(strings.TrimSpace(parsed.Data.Status)) {
	case "approved":
		result.Status = paymentdomain.RefundSuccess
	case "rejected":
		result.Status = paymentdomain.RefundFailed
		result.Message = "Paddle 退款被拒绝"
	default: // pending_approval
		result.Status = paymentdomain.RefundProcessing
		result.Message = "Paddle 退款待审批（状态：" + parsed.Data.Status + "）"
	}
	return result, nil
}

// paddleOrderMeta Paddle custom_data 中携带的本地订单号
type paddleOrderMeta struct {
	OrderNo string `json:"order_no"`
}

// paddleEvent Webhook 报文结构（仅取所需字段）
type paddleEvent struct {
	EventType string `json:"event_type"`
	Data      struct {
		ID           string          `json:"id"`
		Status       string          `json:"status"`
		CurrencyCode string          `json:"currency_code"`
		CustomData   paddleOrderMeta `json:"custom_data"`
		Details      struct {
			Totals struct {
				GrandTotal string `json:"grand_total"`
				Currency   string `json:"currency_code"`
			} `json:"totals"`
		} `json:"details"`
	} `json:"data"`
}

// ExtractOrderNo 从未验签报文预提取本地订单号（仅用于订单路由，验签仍在 HandleCallback 完成）
func (p *paddleProvider) ExtractOrderNo(callbackData map[string]string) string {
	rawBody := callbackData[CallbackRawBodyKey]
	if rawBody == "" {
		return ""
	}
	var event paddleEvent
	if err := json.Unmarshal([]byte(rawBody), &event); err != nil {
		return ""
	}
	return event.Data.CustomData.OrderNo
}

func (p *paddleProvider) HandleCallback(ctx context.Context, data map[string]any, callbackData map[string]string, clientIP string) (*paymentdomain.CallbackResult, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	rawBody := callbackData[CallbackRawBodyKey]
	if rawBody == "" {
		return nil, apperrors.New(40075, http.StatusBadRequest, "Paddle Webhook 缺少原始报文")
	}
	// Paddle-Signature: ts=<unix>;h1=<hex>；签名原文为 "<ts>:<body>"
	ts, h1 := parsePaddleSignature(callbackHeader(callbackData, "Paddle-Signature"))
	if ts == "" || h1 == "" {
		return nil, apperrors.New(40075, http.StatusBadRequest, "Paddle Webhook 签名头格式无效")
	}
	if err := verifyWebhookHMAC(cfg.WebhookSecret, ts+":"+rawBody, h1, "hex", "Paddle"); err != nil {
		return nil, err
	}

	var event paddleEvent
	if err := json.Unmarshal([]byte(rawBody), &event); err != nil {
		return nil, apperrors.New(40076, http.StatusBadRequest, "Paddle 事件解析失败")
	}
	currency := firstNonEmpty(event.Data.Details.Totals.Currency, event.Data.CurrencyCode, cfg.Currency)
	amount := decimal.Zero
	if minor, perr := strconv.ParseInt(strings.TrimSpace(event.Data.Details.Totals.GrandTotal), 10, 64); perr == nil {
		amount = stripeMajorUnits(minor, currency)
	}
	paid := event.EventType == "transaction.completed" || event.EventType == "transaction.paid"
	return &paymentdomain.CallbackResult{
		Success:         true,
		Paid:            paid,
		OrderNo:         event.Data.CustomData.OrderNo,
		ProviderOrderNo: event.Data.ID,
		TradeStatus:     event.EventType,
		PaymentMethod:   paymentdomain.MethodPaddle,
		Amount:          amount,
		RawData: map[string]any{
			"event_type":     event.EventType,
			"transaction_id": event.Data.ID,
			"status":         event.Data.Status,
			"currency":       currency,
		},
	}, nil
}

var _ callbackOrderExtractor = (*paddleProvider)(nil)

// parsePaddleSignature 解析 `ts=...;h1=...` 形式的签名头
func parsePaddleSignature(header string) (ts string, h1 string) {
	for _, part := range strings.Split(header, ";") {
		key, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if !found {
			continue
		}
		switch strings.TrimSpace(key) {
		case "ts":
			ts = strings.TrimSpace(value)
		case "h1":
			h1 = strings.TrimSpace(value)
		}
	}
	return ts, h1
}
