package service

import (
	"context"
	"encoding/json"
	"fmt"
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

func (p *epayProvider) Name() string { return p.name }

func (p *epayProvider) Describe() paymentdomain.ProviderMeta {
	desc := "兼容易支付协议的第四方聚合网关，一套商户凭据同时覆盖支付宝 / 微信 / QQ钱包 / 网银。"
	icon := "alipay"
	if p.name == paymentdomain.MethodRainbowEpay {
		desc = "彩虹易支付（改版易支付），协议与通用易支付一致，独立注册以便同应用内并存两套商户。"
	}
	return finalizeMeta(paymentdomain.ProviderMeta{
		Method:       p.name,
		Name:         p.label,
		Description:  desc,
		Category:     paymentdomain.CategoryAggregate,
		Icon:         icon,
		BrandColor:   "#1677FF",
		DocURL:       "https://github.com/HeFang-China/EPay-API",
		CallbackPath: "/api/public/pay/callback/" + p.name,
		CallbackNote: "回调为表单 POST，按参数字典序 + 商户密钥做 MD5/SHA 验签；通用易支付亦兼容旧路径 /api/public/pay/epay。",
		Regions:      []string{"中国大陆"},
		Currencies:   []string{"CNY"},
		Capabilities: paymentdomain.ProviderCapabilities{
			Redirect: true, QRCode: true, Webhook: true, WebhookSignature: true, RemoteQuery: true,
			Refund: true, PartialRefund: true,
		},
		PayTypes: []paymentdomain.PayTypeOption{
			payType("alipay", "支付宝", "跳转支付宝收银台"),
			payType("wxpay", "微信支付", "跳转微信收银台"),
			payType("qqpay", "QQ 钱包", ""),
			payType("bank", "网银", "银行卡快捷支付"),
		},
		Fields: fields(
			inGroup(paymentdomain.GroupCredential,
				fText("pid", "商户 PID", "1001", "易支付后台的商户 ID", true),
				fSecret("key", "商户密钥", "商户 KEY", "用于请求签名与回调验签", true),
				fURL("apiUrl", "网关地址", "https://pay.example.com", "易支付站点根地址，不含具体接口路径"),
				fText("sitename", "站点名称", "My Site", "部分易支付站点要求提交站点名", false),
			),
			callbackFields(
				"留空则使用平台默认回调地址；建议在易支付后台同步填写",
				"用户支付完成后跳转的前端页面",
			),
			limitFields("0.01", "50000"),
			advanced(fields(
				inGroup(paymentdomain.GroupAdvanced,
					fSelect("signType", "签名算法", "需与易支付站点设置一致", "MD5",
						opt("MD5", "MD5"), opt("SHA1", "SHA1"), opt("SHA256", "SHA256")),
					fNum("expireMinutes", "订单有效期（分钟）", "30", "超时未支付自动关单", 30),
					fSwitch("verifyIP", "校验回调来源 IP", "开启后仅接受下方白名单 IP 的回调", false),
					fTags("allowedIPs", "回调 IP 白名单", "1.2.3.4, 5.6.7.8", "逗号分隔；仅在开启来源校验时生效"),
					fTags("supportedTypes", "启用的支付类型", "alipay, wxpay, qqpay, bank", "留空表示放行全部类型"),
				),
			)...),
		),
	})
}

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
	// 限额校验已上移到网关层（enforceAmountLimits），此处不再重复
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

// Refund 易支付退款（act=refund）。
//
// 易支付各站点实现不完全统一，但 `act=refund` + pid/key/out_trade_no/money 是通行约定，
// 响应形如 {"code":1,"msg":"..."}。上游同步返回结果，无退款单号，故以本地退款单号占位。
func (p *epayProvider) Refund(ctx context.Context, data map[string]any, req paymentdomain.RefundRequest) (*paymentdomain.RefundResult, error) {
	epay, err := decodeEpayConfig(data)
	if err != nil {
		return nil, err
	}
	resp, err := p.client.R().SetContext(ctx).SetFormData(map[string]string{
		"act":          "refund",
		"pid":          epay.PID,
		"key":          epay.Key,
		"out_trade_no": req.OrderNo,
		"trade_no":     strings.TrimSpace(req.ProviderOrderNo),
		"money":        req.RefundAmount.StringFixed(2),
	}).Post(strings.TrimRight(epay.APIURL, "/") + "/api.php")
	if err != nil {
		return nil, apperrors.New(50094, http.StatusBadGateway, p.label+"退款请求失败："+err.Error())
	}
	if !resp.IsSuccess() {
		return nil, apperrors.New(50094, http.StatusBadGateway,
			fmt.Sprintf("%s退款请求失败：HTTP %d", p.label, resp.StatusCode()))
	}
	var parsed struct {
		Code json.Number `json:"code"`
		Msg  string      `json:"msg"`
	}
	if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
		return nil, apperrors.New(50094, http.StatusBadGateway, p.label+"退款响应解析失败："+resp.String())
	}
	if parsed.Code.String() != "1" {
		return &paymentdomain.RefundResult{
			Status:  paymentdomain.RefundFailed,
			Message: pickString(parsed.Msg, "上游拒绝退款"),
			Raw:     map[string]any{"code": parsed.Code.String(), "msg": parsed.Msg},
		}, nil
	}
	return &paymentdomain.RefundResult{
		Status:           paymentdomain.RefundSuccess,
		ProviderRefundNo: req.RefundNo,
		Amount:           req.RefundAmount,
		Raw:              map[string]any{"code": parsed.Code.String(), "msg": parsed.Msg},
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

	// 构建验签数据（跳过框架注入的保留键 __*，它们不属于上游签名参数集）
	verifyData := map[string]string{}
	for k, v := range callbackData {
		if k == "sign" || k == "sign_type" || strings.HasPrefix(k, "__") || strings.TrimSpace(v) == "" {
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
