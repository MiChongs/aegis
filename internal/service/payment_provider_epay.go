package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	paymentdomain "aegis/internal/domain/payment"
	apperrors "aegis/pkg/errors"

	"github.com/go-resty/resty/v2"
)

// ── Epay Provider ──

type epayProvider struct {
	client *resty.Client
	name   string
	label  string
}

func newEpayProvider(client *resty.Client) *epayProvider {
	return &epayProvider{client: client, name: paymentdomain.MethodEpay, label: "易支付 (Epay)"}
}

func newRainbowEpayProvider(client *resty.Client) *epayProvider {
	return &epayProvider{client: client, name: paymentdomain.MethodRainbowEpay, label: "彩虹易支付"}
}

func (p *epayProvider) Name() string  { return p.name }
func (p *epayProvider) Label() string { return p.label }

func (p *epayProvider) ValidateConfig(data map[string]any) error {
	_, err := decodeEpayConfig(data)
	return err
}

func (p *epayProvider) TestConnection(ctx context.Context, data map[string]any) (map[string]any, error) {
	epay, err := decodeEpayConfig(data)
	if err != nil {
		return nil, err
	}
	queryURL := strings.TrimRight(epay.APIURL, "/") + "/api.php"
	resp, err := p.client.R().SetQueryParams(map[string]string{
		"act":          "order",
		"pid":          epay.PID,
		"key":          epay.Key,
		"out_trade_no": "TEST_" + time.Now().UTC().Format("20060102150405"),
	}).SetContext(ctx).Get(queryURL)
	if err != nil {
		return map[string]any{"config_valid": true, "api_accessible": false, "error": err.Error()}, nil
	}
	return map[string]any{"config_valid": true, "api_accessible": resp.IsSuccess(), "status": resp.StatusCode(), "body": resp.String()}, nil
}

func (p *epayProvider) CreateOrder(ctx context.Context, data map[string]any, req PaymentOrderRequest) (*paymentdomain.PaymentPayload, error) {
	epay, err := decodeEpayConfig(data)
	if err != nil {
		return nil, err
	}
	params := map[string]string{
		"pid":          epay.PID,
		"type":         normalizeProviderType(req.ProviderType),
		"out_trade_no": req.OrderNo,
		"notify_url":   pickString(req.NotifyURL, epay.NotifyURL),
		"return_url":   pickString(req.ReturnURL, epay.ReturnURL),
		"name":         req.Subject,
		"money":        req.Amount.StringFixed(2),
		"sign_type":    normalizeSignType(epay.SignType),
	}
	if len(req.Metadata) > 0 {
		raw, _ := json.Marshal(req.Metadata)
		params["param"] = string(raw)
	}
	params["sign"] = generatePaymentSign(params, epay.Key, params["sign_type"])
	submitURL := strings.TrimRight(epay.APIURL, "/") + "/submit.php"
	return &paymentdomain.PaymentPayload{
		Success:      true,
		OrderNo:      req.OrderNo,
		PaymentURL:   submitURL,
		RedirectURL:  submitURL + "?" + url.Values(mapStringSlice(params)).Encode(),
		HTML:         buildPaymentFormHTML(submitURL, params),
		FormData:     mapStringAny(params),
		ProviderType: params["type"],
	}, nil
}

func (p *epayProvider) QueryRemoteOrder(ctx context.Context, data map[string]any, orderNo string) (map[string]any, error) {
	epay, err := decodeEpayConfig(data)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.R().SetContext(ctx).SetQueryParams(map[string]string{
		"act":          "order",
		"pid":          epay.PID,
		"key":          epay.Key,
		"out_trade_no": orderNo,
	}).Get(strings.TrimRight(epay.APIURL, "/") + "/api.php")
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		result = map[string]any{"raw": resp.String()}
	}
	return result, nil
}

func (p *epayProvider) HandleCallback(ctx context.Context, data map[string]any, callbackData map[string]string, clientIP string) (*paymentdomain.CallbackResult, error) {
	epay, err := decodeEpayConfig(data)
	if err != nil {
		return nil, err
	}

	// 签名验证
	sign := callbackData["sign"]
	signType := normalizeSignType(firstNonEmpty(callbackData["sign_type"], epay.SignType))
	if sign == "" {
		return nil, apperrors.New(40075, http.StatusBadRequest, "缺少签名")
	}

	// IP 白名单验证
	if epay.VerifyIP && len(epay.AllowedIPs) > 0 && !containsString(epay.AllowedIPs, clientIP) {
		return nil, apperrors.New(40370, http.StatusForbidden, "回调IP未授权")
	}

	// 构建验签数据
	verifyData := map[string]string{}
	for k, v := range callbackData {
		if k == "sign" || k == "sign_type" || strings.TrimSpace(v) == "" {
			continue
		}
		verifyData[k] = v
	}
	if !strings.EqualFold(generatePaymentSign(verifyData, epay.Key, signType), sign) {
		return nil, apperrors.New(40076, http.StatusBadRequest, "签名验证失败")
	}

	tradeStatus := callbackData["trade_status"]
	paid := tradeStatus == "TRADE_SUCCESS" || tradeStatus == "TRADE_FINISHED"

	return &paymentdomain.CallbackResult{
		Success:         true,
		Paid:            paid,
		OrderNo:         callbackData["out_trade_no"],
		ProviderOrderNo: callbackData["trade_no"],
		TradeStatus:     tradeStatus,
		PaymentMethod:   normalizeProviderType(callbackData["type"]),
		RawData:         mapStringAny(callbackData),
	}, nil
}

func (p *epayProvider) SupportedPayTypes() []string {
	return []string{"alipay", "wxpay", "qqpay", "bank"}
}

// ── Epay 配置解码 ──

func decodeEpayConfig(data map[string]any) (*paymentdomain.EpayConfig, error) {
	cfg, err := decodeProviderConfig[paymentdomain.EpayConfig](data)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.PID) == "" || strings.TrimSpace(cfg.Key) == "" || strings.TrimSpace(cfg.APIURL) == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "易支付配置不完整")
	}
	if cfg.SignType == "" {
		cfg.SignType = "MD5"
	}
	if cfg.ExpireMinutes <= 0 {
		cfg.ExpireMinutes = 30
	}
	if cfg.MinAmount <= 0 {
		cfg.MinAmount = 0.01
	}
	if cfg.MaxAmount <= 0 {
		cfg.MaxAmount = 50000
	}
	if len(cfg.SupportedTypes) == 0 {
		cfg.SupportedTypes = []string{"alipay", "wxpay", "qqpay", "bank"}
	}
	return cfg, nil
}
