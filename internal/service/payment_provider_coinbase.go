package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	paymentdomain "aegis/internal/domain/payment"
	apperrors "aegis/pkg/errors"

	"github.com/go-resty/resty/v2"
	"github.com/shopspring/decimal"
)

// ── Coinbase Commerce Provider（加密货币收款）──
//
// 下单：POST /charges 以法币计价创建收款单，买家可用 BTC / ETH / USDC 等按实时汇率付款，
//       返回托管收款页地址。金额以法币十进制字符串传递（非最小货币单位）。
// 回调：X-CC-Webhook-Signature 为原始报文的 HMAC-SHA256（hex），密钥为 Webhook 共享密钥。
//       仅 charge:confirmed / charge:resolved 视为到账（charge:pending 表示链上待确认）。

const coinbaseAPI = "https://api.commerce.coinbase.com"

type coinbaseProvider struct {
	client *resty.Client
}

func newCoinbaseProvider(client *resty.Client) *coinbaseProvider {
	return &coinbaseProvider{client: client}
}

func (p *coinbaseProvider) Name() string { return paymentdomain.MethodCoinbase }

func (p *coinbaseProvider) Describe() paymentdomain.ProviderMeta {
	return finalizeMeta(paymentdomain.ProviderMeta{
		Method:       paymentdomain.MethodCoinbase,
		Name:         "Coinbase Commerce",
		Description:  "加密货币收款：以法币计价，买家用 BTC / ETH / USDC / LTC 等按实时汇率付款，无拒付风险。",
		Category:     paymentdomain.CategoryCrypto,
		Icon:         "coinbase",
		BrandColor:   "#0052FF",
		DocURL:       "https://docs.cdp.coinbase.com/commerce-onchain/docs/welcome",
		CallbackPath: "/api/public/pay/callback/" + paymentdomain.MethodCoinbase,
		CallbackNote: "在 Settings → Webhook subscriptions 新建端点指向该地址，并把 Shared secret 填入下方 Webhook 密钥。仅 charge:confirmed / charge:resolved 会被判定为已到账。",
		Regions:      []string{"全球"},
		Currencies:   []string{"USD", "EUR", "GBP"},
		Capabilities: paymentdomain.ProviderCapabilities{
			Redirect: true, QRCode: true, Webhook: true, WebhookSignature: true, RemoteQuery: true,
		},
		PayTypes: []paymentdomain.PayTypeOption{
			payType("crypto", "加密货币", "托管收款页自动展示各链地址与二维码"),
		},
		Fields: fields(
			inGroup(paymentdomain.GroupCredential,
				fSecret("apiKey", "API Key", "Commerce API Key", "Settings → Security 生成", true),
				fSecret("webhookSecret", "Webhook 密钥", "Shared secret", "Webhook 订阅的共享密钥，验签必需", true),
			),
			inGroup(paymentdomain.GroupGateway,
				fSelect("currency", "计价法币", "以该法币标价，买家按实时汇率支付等值加密货币", "USD",
					opt("USD", "USD 美元"), opt("EUR", "EUR 欧元"), opt("GBP", "GBP 英镑")),
				fURL("notifyUrl", "Webhook 地址", "https://your-domain.com/api/public/pay/callback/coinbase", "需与后台登记的端点一致"),
				fURL("returnUrl", "支付成功跳转", "https://your-domain.com/pay/success", "到账后的回跳地址"),
				fURL("cancelUrl", "取消支付跳转", "https://your-domain.com/pay/cancel", "买家放弃付款后的回跳地址"),
			),
			limitFields("1.00", "100000"),
		),
	})
}

func (p *coinbaseProvider) decodeConfig(data map[string]any) (*paymentdomain.CoinbaseConfig, error) {
	cfg, err := decodeProviderConfig[paymentdomain.CoinbaseConfig](data)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "Coinbase Commerce 配置不完整：缺少 apiKey")
	}
	if cfg.Currency = strings.ToUpper(strings.TrimSpace(cfg.Currency)); cfg.Currency == "" {
		cfg.Currency = "USD"
	}
	return cfg, nil
}

func (p *coinbaseProvider) ValidateConfig(data map[string]any) error {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.WebhookSecret) == "" {
		return apperrors.New(40078, http.StatusBadRequest, "Coinbase Commerce 配置不完整：缺少 webhookSecret（Webhook 验签必需）")
	}
	return nil
}

func (p *coinbaseProvider) request(ctx context.Context, cfg *paymentdomain.CoinbaseConfig) *resty.Request {
	return p.client.R().
		SetContext(ctx).
		SetHeader("X-CC-Api-Key", strings.TrimSpace(cfg.APIKey)).
		SetHeader("X-CC-Version", "2018-03-22").
		SetHeader("Content-Type", "application/json")
}

func (p *coinbaseProvider) TestConnection(ctx context.Context, data map[string]any) (map[string]any, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return map[string]any{"config_valid": false, "error": err.Error()}, nil
	}
	resp, err := p.request(ctx, cfg).SetQueryParam("limit", "1").Get(coinbaseAPI + "/charges")
	if err != nil {
		return map[string]any{"config_valid": true, "api_accessible": false, "error": err.Error()}, nil
	}
	if !resp.IsSuccess() {
		return map[string]any{
			"config_valid": true, "api_accessible": false,
			"status": resp.StatusCode(), "error": resp.String(),
		}, nil
	}
	return map[string]any{"config_valid": true, "api_accessible": true}, nil
}

func (p *coinbaseProvider) CreateOrder(ctx context.Context, data map[string]any, req PaymentOrderRequest) (*paymentdomain.PaymentPayload, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"name":         req.Subject,
		"description":  pickString(req.Body, req.Subject),
		"pricing_type": "fixed_price",
		"local_price": map[string]any{
			// Coinbase 收十进制法币金额字符串，而非最小货币单位
			"amount":   req.Amount.StringFixed(2),
			"currency": cfg.Currency,
		},
		"metadata": map[string]any{"order_no": req.OrderNo},
	}
	if returnURL := pickString(req.ReturnURL, cfg.ReturnURL); returnURL != "" {
		body["redirect_url"] = returnURL
	}
	if cancelURL := strings.TrimSpace(cfg.CancelURL); cancelURL != "" {
		body["cancel_url"] = cancelURL
	}

	resp, err := p.request(ctx, cfg).SetBody(body).Post(coinbaseAPI + "/charges")
	if err != nil {
		return nil, apperrors.New(50088, http.StatusBadGateway, "Coinbase Commerce 下单失败："+err.Error())
	}
	if !resp.IsSuccess() {
		return nil, apperrors.New(50088, http.StatusBadGateway, "Coinbase Commerce 下单失败："+resp.String())
	}
	var parsed struct {
		Data struct {
			ID        string `json:"id"`
			Code      string `json:"code"`
			HostedURL string `json:"hosted_url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
		return nil, apperrors.New(50088, http.StatusBadGateway, "Coinbase Commerce 响应解析失败")
	}
	if parsed.Data.HostedURL == "" {
		return nil, apperrors.New(50088, http.StatusBadGateway, "Coinbase Commerce 未返回收款页地址")
	}
	return &paymentdomain.PaymentPayload{
		Success:      true,
		OrderNo:      req.OrderNo,
		PaymentURL:   parsed.Data.HostedURL,
		RedirectURL:  parsed.Data.HostedURL,
		ProviderType: "coinbase_charge",
		Message:      "请在收款页选择币种并完成链上转账，到账确认后订单自动完成",
		FormData:     map[string]any{"chargeId": parsed.Data.ID, "chargeCode": parsed.Data.Code},
	}, nil
}

func (p *coinbaseProvider) QueryRemoteOrder(ctx context.Context, data map[string]any, orderNo string) (map[string]any, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	// Coinbase 不支持按 metadata 过滤，扫描最近收款单匹配本地订单号
	resp, err := p.request(ctx, cfg).SetQueryParam("limit", "100").Get(coinbaseAPI + "/charges")
	if err != nil {
		return nil, apperrors.New(50088, http.StatusBadGateway, "Coinbase Commerce 查询失败："+err.Error())
	}
	if !resp.IsSuccess() {
		return nil, apperrors.New(50088, http.StatusBadGateway, "Coinbase Commerce 查询失败："+resp.String())
	}
	var parsed struct {
		Data []coinbaseCharge `json:"data"`
	}
	if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
		return nil, apperrors.New(50088, http.StatusBadGateway, "Coinbase Commerce 响应解析失败")
	}
	for _, charge := range parsed.Data {
		if charge.Metadata.OrderNo != orderNo {
			continue
		}
		status := charge.latestStatus()
		return map[string]any{
			"charge_id":   charge.ID,
			"charge_code": charge.Code,
			"status":      status,
			"amount":      charge.Pricing.Local.Amount,
			"currency":    charge.Pricing.Local.Currency,
			"paid":        coinbaseStatusPaid(status),
		}, nil
	}
	return map[string]any{
		"paid":    false,
		"message": "最近 100 笔 Coinbase 收款单中未找到该订单，可能已超出查询窗口",
	}, nil
}

// coinbaseCharge 收款单结构（仅取所需字段）
type coinbaseCharge struct {
	ID       string `json:"id"`
	Code     string `json:"code"`
	Metadata struct {
		OrderNo string `json:"order_no"`
	} `json:"metadata"`
	Pricing struct {
		Local struct {
			Amount   string `json:"amount"`
			Currency string `json:"currency"`
		} `json:"local"`
	} `json:"pricing"`
	Timeline []struct {
		Status string `json:"status"`
	} `json:"timeline"`
}

// latestStatus 取时间线上的最新状态
func (c *coinbaseCharge) latestStatus() string {
	if len(c.Timeline) == 0 {
		return ""
	}
	return c.Timeline[len(c.Timeline)-1].Status
}

// coinbaseStatusPaid 判定链上到账：仅 COMPLETED / RESOLVED 视为成功，
// PENDING（待区块确认）与 NEW / EXPIRED 均不算
func coinbaseStatusPaid(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "COMPLETED", "RESOLVED":
		return true
	default:
		return false
	}
}

// coinbaseEvent Webhook 报文结构
type coinbaseEvent struct {
	Event struct {
		ID   string         `json:"id"`
		Type string         `json:"type"`
		Data coinbaseCharge `json:"data"`
	} `json:"event"`
}

// ExtractOrderNo 从未验签报文预提取本地订单号（仅用于订单路由）
func (p *coinbaseProvider) ExtractOrderNo(callbackData map[string]string) string {
	rawBody := callbackData[CallbackRawBodyKey]
	if rawBody == "" {
		return ""
	}
	var event coinbaseEvent
	if err := json.Unmarshal([]byte(rawBody), &event); err != nil {
		return ""
	}
	return event.Event.Data.Metadata.OrderNo
}

func (p *coinbaseProvider) HandleCallback(ctx context.Context, data map[string]any, callbackData map[string]string, clientIP string) (*paymentdomain.CallbackResult, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	rawBody := callbackData[CallbackRawBodyKey]
	if rawBody == "" {
		return nil, apperrors.New(40075, http.StatusBadRequest, "Coinbase Commerce Webhook 缺少原始报文")
	}
	if err := verifyWebhookHMAC(cfg.WebhookSecret, rawBody, callbackHeader(callbackData, "X-CC-Webhook-Signature"), "hex", "Coinbase Commerce"); err != nil {
		return nil, err
	}

	var event coinbaseEvent
	if err := json.Unmarshal([]byte(rawBody), &event); err != nil {
		return nil, apperrors.New(40076, http.StatusBadRequest, "Coinbase Commerce 事件解析失败")
	}
	amount := decimal.Zero
	if parsed, perr := decimal.NewFromString(strings.TrimSpace(event.Event.Data.Pricing.Local.Amount)); perr == nil {
		amount = parsed
	}
	// 事件类型是到账判定的真实来源；charge:pending 表示链上待确认，不发货
	paid := event.Event.Type == "charge:confirmed" || event.Event.Type == "charge:resolved"
	return &paymentdomain.CallbackResult{
		Success:         true,
		Paid:            paid,
		OrderNo:         event.Event.Data.Metadata.OrderNo,
		ProviderOrderNo: firstNonEmpty(event.Event.Data.Code, event.Event.Data.ID),
		TradeStatus:     event.Event.Type,
		PaymentMethod:   paymentdomain.MethodCoinbase,
		Amount:          amount,
		RawData: map[string]any{
			"event_id":    event.Event.ID,
			"event_type":  event.Event.Type,
			"charge_code": event.Event.Data.Code,
			"currency":    event.Event.Data.Pricing.Local.Currency,
		},
	}, nil
}

var _ callbackOrderExtractor = (*coinbaseProvider)(nil)
