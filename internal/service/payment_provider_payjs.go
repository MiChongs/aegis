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
	"github.com/shopspring/decimal"
)

// ── PAYJS Provider ──

type payjsProvider struct {
	client *resty.Client
}

func newPayjsProvider(client *resty.Client) *payjsProvider {
	return &payjsProvider{client: client}
}

func (p *payjsProvider) Name() string  { return paymentdomain.MethodPayjs }
func (p *payjsProvider) Label() string { return "PAYJS" }

func (p *payjsProvider) ValidateConfig(data map[string]any) error {
	cfg, err := decodePayjsConfig(data)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.MchID) == "" || strings.TrimSpace(cfg.Key) == "" {
		return apperrors.New(40078, http.StatusBadRequest, "PAYJS 配置不完整：缺少 mchId 或 key")
	}
	return nil
}

func (p *payjsProvider) TestConnection(ctx context.Context, data map[string]any) (map[string]any, error) {
	cfg, err := decodePayjsConfig(data)
	if err != nil {
		return nil, err
	}
	apiURL := strings.TrimRight(pickString(cfg.APIURL, "https://payjs.cn/api"), "/")
	params := map[string]string{"mchid": cfg.MchID}
	params["sign"] = payjsSign(params, cfg.Key)
	resp, err := p.client.R().SetContext(ctx).SetFormData(params).Post(apiURL + "/info")
	if err != nil {
		return map[string]any{"config_valid": true, "api_accessible": false, "error": err.Error()}, nil
	}
	return map[string]any{"config_valid": true, "api_accessible": resp.IsSuccess(), "status": resp.StatusCode(), "body": resp.String()}, nil
}

func (p *payjsProvider) CreateOrder(ctx context.Context, data map[string]any, req PaymentOrderRequest) (*paymentdomain.PaymentPayload, error) {
	cfg, err := decodePayjsConfig(data)
	if err != nil {
		return nil, err
	}
	apiURL := strings.TrimRight(pickString(cfg.APIURL, "https://payjs.cn/api"), "/")

	// PAYJS 金额单位为分
	amountFen := req.Amount.Mul(decimal.NewFromInt(100)).IntPart()
	params := map[string]string{
		"mchid":       cfg.MchID,
		"total_fee":   fmt.Sprintf("%d", amountFen),
		"out_trade_no": req.OrderNo,
		"body":        req.Subject,
		"notify_url":  pickString(req.NotifyURL, cfg.NotifyURL),
	}
	params["sign"] = payjsSign(params, cfg.Key)

	resp, err := p.client.R().SetContext(ctx).SetFormData(params).Post(apiURL + "/native")
	if err != nil {
		return nil, apperrors.New(50080, http.StatusInternalServerError, "PAYJS 下单请求失败: "+err.Error())
	}

	var result map[string]any
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return nil, apperrors.New(50081, http.StatusInternalServerError, "PAYJS 响应解析失败")
	}

	if code, _ := result["return_code"].(float64); code != 1 {
		msg, _ := result["return_msg"].(string)
		return nil, apperrors.New(40080, http.StatusBadRequest, "PAYJS 下单失败: "+msg)
	}

	payURL, _ := result["code_url"].(string)
	qrcode, _ := result["qrcode"].(string)

	return &paymentdomain.PaymentPayload{
		Success:      true,
		OrderNo:      req.OrderNo,
		PaymentURL:   payURL,
		RedirectURL:  qrcode,
		ProviderType: "wxpay",
	}, nil
}

func (p *payjsProvider) QueryRemoteOrder(ctx context.Context, data map[string]any, orderNo string) (map[string]any, error) {
	cfg, err := decodePayjsConfig(data)
	if err != nil {
		return nil, err
	}
	apiURL := strings.TrimRight(pickString(cfg.APIURL, "https://payjs.cn/api"), "/")
	params := map[string]string{"payjs_order_id": orderNo}
	params["sign"] = payjsSign(params, cfg.Key)

	resp, err := p.client.R().SetContext(ctx).SetFormData(params).Post(apiURL + "/check")
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		result = map[string]any{"raw": resp.String()}
	}
	return result, nil
}

func (p *payjsProvider) HandleCallback(ctx context.Context, data map[string]any, callbackData map[string]string, clientIP string) (*paymentdomain.CallbackResult, error) {
	cfg, err := decodePayjsConfig(data)
	if err != nil {
		return nil, err
	}

	sign := callbackData["sign"]
	if sign == "" {
		return nil, apperrors.New(40075, http.StatusBadRequest, "缺少签名")
	}
	verifyData := map[string]string{}
	for k, v := range callbackData {
		if k == "sign" || strings.TrimSpace(v) == "" {
			continue
		}
		verifyData[k] = v
	}
	if !strings.EqualFold(payjsSign(verifyData, cfg.Key), sign) {
		return nil, apperrors.New(40076, http.StatusBadRequest, "签名验证失败")
	}

	status := callbackData["return_code"]
	paid := status == "1"

	return &paymentdomain.CallbackResult{
		Success:         true,
		Paid:            paid,
		OrderNo:         callbackData["out_trade_no"],
		ProviderOrderNo: callbackData["payjs_order_id"],
		TradeStatus:     status,
		PaymentMethod:   "wxpay",
		RawData:         mapStringAny(callbackData),
	}, nil
}

func (p *payjsProvider) SupportedPayTypes() []string {
	return []string{"wxpay"}
}

func decodePayjsConfig(data map[string]any) (*paymentdomain.PayjsConfig, error) {
	cfg, err := decodeProviderConfig[paymentdomain.PayjsConfig](data)
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

func payjsSign(params map[string]string, key string) string {
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
	raw := strings.Join(parts, "&") + "&key=" + key
	sum := md5.Sum([]byte(raw))
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

