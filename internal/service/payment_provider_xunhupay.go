package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	paymentdomain "aegis/internal/domain/payment"
	apperrors "aegis/pkg/errors"

	"github.com/go-resty/resty/v2"
)

// ── 虎皮椒 Provider ──

type xunhupayProvider struct {
	client *resty.Client
}

func newXunhupayProvider(client *resty.Client) *xunhupayProvider {
	return &xunhupayProvider{client: client}
}

func (p *xunhupayProvider) Name() string  { return paymentdomain.MethodXunhupay }
func (p *xunhupayProvider) Label() string { return "虎皮椒 (XunhuPay)" }

func (p *xunhupayProvider) ValidateConfig(data map[string]any) error {
	cfg, err := decodeXunhupayConfig(data)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.AppID) == "" || strings.TrimSpace(cfg.AppSecret) == "" {
		return apperrors.New(40078, http.StatusBadRequest, "虎皮椒配置不完整：缺少 appId 或 appSecret")
	}
	return nil
}

func (p *xunhupayProvider) TestConnection(ctx context.Context, data map[string]any) (map[string]any, error) {
	cfg, err := decodeXunhupayConfig(data)
	if err != nil {
		return nil, err
	}
	apiURL := strings.TrimRight(pickString(cfg.APIURL, "https://api.xunhupay.com"), "/")
	resp, err := p.client.R().SetContext(ctx).
		SetFormData(map[string]string{"appid": cfg.AppID}).
		Post(apiURL + "/payment/query.html")
	if err != nil {
		return map[string]any{"config_valid": true, "api_accessible": false, "error": err.Error()}, nil
	}
	return map[string]any{"config_valid": true, "api_accessible": resp.IsSuccess(), "status": resp.StatusCode()}, nil
}

func (p *xunhupayProvider) CreateOrder(ctx context.Context, data map[string]any, req PaymentOrderRequest) (*paymentdomain.PaymentPayload, error) {
	cfg, err := decodeXunhupayConfig(data)
	if err != nil {
		return nil, err
	}
	apiURL := strings.TrimRight(pickString(cfg.APIURL, "https://api.xunhupay.com"), "/")

	params := map[string]string{
		"version":        "1.1",
		"appid":          cfg.AppID,
		"trade_order_id": req.OrderNo,
		"total_fee":      req.Amount.StringFixed(2),
		"title":          req.Subject,
		"notify_url":     pickString(req.NotifyURL, cfg.NotifyURL),
		"return_url":     pickString(req.ReturnURL, cfg.ReturnURL),
		"nonce_str":      randomDigits(16),
		"time":           fmt.Sprintf("%d", req.ExpireAt.Unix()),
	}
	// 虎皮椒签名：按 key 排序，拼接 appSecret，MD5
	params["hash"] = xunhupaySign(params, cfg.AppSecret)

	// 根据类型选择网关
	endpoint := apiURL + "/payment/do.html"
	if cfg.WxpayURL != "" && (req.ProviderType == "wxpay" || req.ProviderType == "wechat") {
		endpoint = strings.TrimRight(cfg.WxpayURL, "/") + "/payment/do.html"
	}

	resp, err := p.client.R().SetContext(ctx).SetFormData(params).Post(endpoint)
	if err != nil {
		return nil, apperrors.New(50080, http.StatusInternalServerError, "虎皮椒下单请求失败: "+err.Error())
	}

	var result map[string]any
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return nil, apperrors.New(50081, http.StatusInternalServerError, "虎皮椒响应解析失败")
	}

	if code, _ := result["errcode"].(float64); code != 0 {
		msg, _ := result["errmsg"].(string)
		return nil, apperrors.New(40080, http.StatusBadRequest, "虎皮椒下单失败: "+msg)
	}

	payURL, _ := result["url_qrcode"].(string)
	if payURL == "" {
		payURL, _ = result["url"].(string)
	}

	return &paymentdomain.PaymentPayload{
		Success:      true,
		OrderNo:      req.OrderNo,
		PaymentURL:   payURL,
		RedirectURL:  payURL,
		ProviderType: req.ProviderType,
	}, nil
}

func (p *xunhupayProvider) QueryRemoteOrder(ctx context.Context, data map[string]any, orderNo string) (map[string]any, error) {
	cfg, err := decodeXunhupayConfig(data)
	if err != nil {
		return nil, err
	}
	apiURL := strings.TrimRight(pickString(cfg.APIURL, "https://api.xunhupay.com"), "/")
	params := map[string]string{
		"appid":          cfg.AppID,
		"out_trade_order": orderNo,
		"nonce_str":      randomDigits(16),
	}
	params["hash"] = xunhupaySign(params, cfg.AppSecret)

	resp, err := p.client.R().SetContext(ctx).SetFormData(params).Post(apiURL + "/payment/query.html")
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		result = map[string]any{"raw": resp.String()}
	}
	return result, nil
}

func (p *xunhupayProvider) HandleCallback(ctx context.Context, data map[string]any, callbackData map[string]string, clientIP string) (*paymentdomain.CallbackResult, error) {
	cfg, err := decodeXunhupayConfig(data)
	if err != nil {
		return nil, err
	}

	hash := callbackData["hash"]
	if hash == "" {
		return nil, apperrors.New(40075, http.StatusBadRequest, "缺少签名")
	}
	verifyData := map[string]string{}
	for k, v := range callbackData {
		if k == "hash" || strings.TrimSpace(v) == "" {
			continue
		}
		verifyData[k] = v
	}
	if !strings.EqualFold(xunhupaySign(verifyData, cfg.AppSecret), hash) {
		return nil, apperrors.New(40076, http.StatusBadRequest, "签名验证失败")
	}

	status := callbackData["status"]
	paid := status == "OD" // OD = Order Done

	return &paymentdomain.CallbackResult{
		Success:         true,
		Paid:            paid,
		OrderNo:         callbackData["trade_order_id"],
		ProviderOrderNo: callbackData["transaction_id"],
		TradeStatus:     status,
		PaymentMethod:   callbackData["payment_type"],
		RawData:         mapStringAny(callbackData),
	}, nil
}

func (p *xunhupayProvider) SupportedPayTypes() []string {
	return []string{"alipay", "wxpay"}
}

func decodeXunhupayConfig(data map[string]any) (*paymentdomain.XunhupayConfig, error) {
	cfg, err := decodeProviderConfig[paymentdomain.XunhupayConfig](data)
	if err != nil {
		return nil, err
	}
	if cfg.MinAmount <= 0 {
		cfg.MinAmount = 0.01
	}
	if cfg.MaxAmount <= 0 {
		cfg.MaxAmount = 50000
	}
	return cfg, nil
}

func xunhupaySign(params map[string]string, secret string) string {
	keys := make([]string, 0, len(params))
	for k, v := range params {
		if strings.TrimSpace(v) == "" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, params[k]))
	}
	raw := strings.Join(parts, "&") + secret
	sum := md5.Sum([]byte(raw))
	return hex.EncodeToString(sum[:])
}
