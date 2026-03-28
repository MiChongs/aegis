package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	paymentdomain "aegis/internal/domain/payment"
	apperrors "aegis/pkg/errors"

	"github.com/go-resty/resty/v2"
)

// ── V免签 Provider ──

type vmqpayProvider struct {
	client *resty.Client
}

func newVMQPayProvider(client *resty.Client) *vmqpayProvider {
	return &vmqpayProvider{client: client}
}

func (p *vmqpayProvider) Name() string  { return paymentdomain.MethodVMQPay }
func (p *vmqpayProvider) Label() string { return "V免签 (VMQ)" }

func (p *vmqpayProvider) ValidateConfig(data map[string]any) error {
	cfg, err := decodeProviderConfig[paymentdomain.VMQPayConfig](data)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.APIURL) == "" || strings.TrimSpace(cfg.Key) == "" {
		return apperrors.New(40078, http.StatusBadRequest, "V免签配置不完整：缺少 apiUrl 或 key")
	}
	return nil
}

func (p *vmqpayProvider) TestConnection(ctx context.Context, data map[string]any) (map[string]any, error) {
	cfg, err := decodeProviderConfig[paymentdomain.VMQPayConfig](data)
	if err != nil {
		return nil, err
	}
	apiURL := strings.TrimRight(cfg.APIURL, "/")
	resp, err := p.client.R().SetContext(ctx).Get(apiURL + "/appHeart")
	if err != nil {
		return map[string]any{"config_valid": true, "api_accessible": false, "error": err.Error()}, nil
	}
	return map[string]any{"config_valid": true, "api_accessible": resp.IsSuccess(), "status": resp.StatusCode(), "body": resp.String()}, nil
}

func (p *vmqpayProvider) CreateOrder(ctx context.Context, data map[string]any, req PaymentOrderRequest) (*paymentdomain.PaymentPayload, error) {
	cfg, err := decodeProviderConfig[paymentdomain.VMQPayConfig](data)
	if err != nil {
		return nil, err
	}
	apiURL := strings.TrimRight(cfg.APIURL, "/") + "/createOrder"

	payType := vmqpayType(req.ProviderType)
	price := req.Amount.StringFixed(2)

	// V免签签名：md5(payId + param + type + price + key)
	signRaw := req.OrderNo + "" + payType + price + cfg.Key
	sum := md5.Sum([]byte(signRaw))
	sign := hex.EncodeToString(sum[:])

	params := map[string]string{
		"payId":     req.OrderNo,
		"type":      payType,
		"price":     price,
		"sign":      sign,
		"notifyUrl": pickString(req.NotifyURL, cfg.NotifyURL),
		"returnUrl": pickString(req.ReturnURL, cfg.ReturnURL),
		"param":     req.Subject,
		"isHtml":    "0",
	}

	resp, err := p.client.R().SetContext(ctx).SetFormData(params).Post(apiURL)
	if err != nil {
		return nil, apperrors.New(50080, http.StatusInternalServerError, "V免签下单请求失败: "+err.Error())
	}
	var result map[string]any
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return nil, apperrors.New(50081, http.StatusInternalServerError, "V免签响应解析失败")
	}

	code, _ := result["code"].(float64)
	if code != 1 {
		msg, _ := result["msg"].(string)
		return nil, apperrors.New(40080, http.StatusBadRequest, "V免签下单失败: "+msg)
	}

	payURL, _ := result["payUrl"].(string)
	return &paymentdomain.PaymentPayload{
		Success:      true,
		OrderNo:      req.OrderNo,
		PaymentURL:   payURL,
		RedirectURL:  payURL,
		ProviderType: req.ProviderType,
	}, nil
}

func (p *vmqpayProvider) QueryRemoteOrder(ctx context.Context, data map[string]any, orderNo string) (map[string]any, error) {
	cfg, err := decodeProviderConfig[paymentdomain.VMQPayConfig](data)
	if err != nil {
		return nil, err
	}
	apiURL := strings.TrimRight(cfg.APIURL, "/") + "/checkOrder"
	resp, err := p.client.R().SetContext(ctx).SetQueryParams(map[string]string{
		"payId": orderNo,
	}).Get(apiURL)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		result = map[string]any{"raw": resp.String()}
	}
	return result, nil
}

func (p *vmqpayProvider) HandleCallback(ctx context.Context, data map[string]any, callbackData map[string]string, clientIP string) (*paymentdomain.CallbackResult, error) {
	cfg, err := decodeProviderConfig[paymentdomain.VMQPayConfig](data)
	if err != nil {
		return nil, err
	}

	sign := callbackData["sign"]
	if sign == "" {
		return nil, apperrors.New(40075, http.StatusBadRequest, "缺少签名")
	}

	payID := callbackData["payId"]
	price := callbackData["price"]
	reallyPrice := callbackData["reallyPrice"]
	signRaw := payID + price + reallyPrice + cfg.Key
	sum := md5.Sum([]byte(signRaw))
	expectedSign := hex.EncodeToString(sum[:])
	if !strings.EqualFold(expectedSign, sign) {
		return nil, apperrors.New(40076, http.StatusBadRequest, "签名验证失败")
	}

	payType := callbackData["type"]
	return &paymentdomain.CallbackResult{
		Success:         true,
		Paid:            true,
		OrderNo:         payID,
		ProviderOrderNo: fmt.Sprintf("vmq_%s", payID),
		TradeStatus:     "TRADE_SUCCESS",
		PaymentMethod:   payType,
		RawData:         mapStringAny(callbackData),
	}, nil
}

func (p *vmqpayProvider) SupportedPayTypes() []string {
	return []string{"alipay", "wxpay"}
}

func vmqpayType(providerType string) string {
	switch strings.ToLower(strings.TrimSpace(providerType)) {
	case "alipay":
		return "1"
	case "wxpay", "wechat":
		return "2"
	default:
		return "2"
	}
}
