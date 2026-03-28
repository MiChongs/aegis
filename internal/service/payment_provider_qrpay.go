package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	paymentdomain "aegis/internal/domain/payment"
	apperrors "aegis/pkg/errors"

	"github.com/go-resty/resty/v2"
)

// ── 码支付 Provider ──

type qrpayProvider struct {
	client *resty.Client
}

func newQRPayProvider(client *resty.Client) *qrpayProvider {
	return &qrpayProvider{client: client}
}

func (p *qrpayProvider) Name() string  { return paymentdomain.MethodQRPay }
func (p *qrpayProvider) Label() string { return "码支付 (QRPay)" }

func (p *qrpayProvider) ValidateConfig(data map[string]any) error {
	cfg, err := decodeProviderConfig[paymentdomain.QRPayConfig](data)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.UID) == "" || strings.TrimSpace(cfg.Token) == "" || strings.TrimSpace(cfg.APIURL) == "" {
		return apperrors.New(40078, http.StatusBadRequest, "码支付配置不完整：缺少 uid、token 或 apiUrl")
	}
	return nil
}

func (p *qrpayProvider) TestConnection(ctx context.Context, data map[string]any) (map[string]any, error) {
	cfg, err := decodeProviderConfig[paymentdomain.QRPayConfig](data)
	if err != nil {
		return nil, err
	}
	apiURL := strings.TrimRight(cfg.APIURL, "/")
	resp, err := p.client.R().SetContext(ctx).Get(apiURL)
	if err != nil {
		return map[string]any{"config_valid": true, "api_accessible": false, "error": err.Error()}, nil
	}
	return map[string]any{"config_valid": true, "api_accessible": resp.IsSuccess(), "status": resp.StatusCode()}, nil
}

func (p *qrpayProvider) CreateOrder(ctx context.Context, data map[string]any, req PaymentOrderRequest) (*paymentdomain.PaymentPayload, error) {
	cfg, err := decodeProviderConfig[paymentdomain.QRPayConfig](data)
	if err != nil {
		return nil, err
	}
	apiURL := strings.TrimRight(cfg.APIURL, "/") + "/creat_order"

	// 码支付签名：md5(payId + param + type + price + token)
	payType := qrpayType(req.ProviderType)
	price := req.Amount.StringFixed(2)
	signRaw := req.OrderNo + "" + payType + price + cfg.Token
	sum := md5.Sum([]byte(signRaw))
	sign := hex.EncodeToString(sum[:])

	params := map[string]string{
		"uid":       cfg.UID,
		"payId":     req.OrderNo,
		"type":      payType,
		"price":     price,
		"sign":      sign,
		"notifyUrl": pickString(req.NotifyURL, cfg.NotifyURL),
		"returnUrl": pickString(req.ReturnURL, cfg.ReturnURL),
		"isHtml":    "0",
	}

	resp, err := p.client.R().SetContext(ctx).SetFormData(params).Post(apiURL)
	if err != nil {
		return nil, apperrors.New(50080, http.StatusInternalServerError, "码支付下单请求失败: "+err.Error())
	}
	var result map[string]any
	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return nil, apperrors.New(50081, http.StatusInternalServerError, "码支付响应解析失败")
	}

	code, _ := result["code"].(float64)
	if code != 1 {
		msg, _ := result["msg"].(string)
		return nil, apperrors.New(40080, http.StatusBadRequest, "码支付下单失败: "+msg)
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

func (p *qrpayProvider) QueryRemoteOrder(ctx context.Context, data map[string]any, orderNo string) (map[string]any, error) {
	cfg, err := decodeProviderConfig[paymentdomain.QRPayConfig](data)
	if err != nil {
		return nil, err
	}
	apiURL := strings.TrimRight(cfg.APIURL, "/") + "/check_order"
	resp, err := p.client.R().SetContext(ctx).SetQueryParams(map[string]string{
		"uid":   cfg.UID,
		"token": cfg.Token,
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

func (p *qrpayProvider) HandleCallback(ctx context.Context, data map[string]any, callbackData map[string]string, clientIP string) (*paymentdomain.CallbackResult, error) {
	cfg, err := decodeProviderConfig[paymentdomain.QRPayConfig](data)
	if err != nil {
		return nil, err
	}

	sign := callbackData["sign"]
	if sign == "" {
		return nil, apperrors.New(40075, http.StatusBadRequest, "缺少签名")
	}

	// 码支付回调签名验证
	payID := callbackData["payId"]
	price := callbackData["price"]
	reallyPrice := callbackData["reallyPrice"]
	signRaw := payID + price + reallyPrice + cfg.Token
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
		ProviderOrderNo: fmt.Sprintf("%s_%s", payID, time.Now().UTC().Format("20060102150405")),
		TradeStatus:     "TRADE_SUCCESS",
		PaymentMethod:   payType,
		RawData:         mapStringAny(callbackData),
	}, nil
}

func (p *qrpayProvider) SupportedPayTypes() []string {
	return []string{"alipay", "wxpay", "qqpay"}
}

func qrpayType(providerType string) string {
	switch strings.ToLower(strings.TrimSpace(providerType)) {
	case "alipay":
		return "1"
	case "wxpay", "wechat":
		return "2"
	case "qqpay":
		return "3"
	default:
		return "2"
	}
}
