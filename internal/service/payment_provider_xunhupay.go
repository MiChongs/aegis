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
	"time"

	paymentdomain "aegis/internal/domain/payment"
	apperrors "aegis/pkg/errors"

	"github.com/go-resty/resty/v2"
	"github.com/shopspring/decimal"
)

// ── 虎皮椒 Provider ──

type xunhupayProvider struct {
	client *resty.Client
}

func newXunhupayProvider(client *resty.Client) *xunhupayProvider {
	return &xunhupayProvider{client: client}
}

func (p *xunhupayProvider) Name() string { return paymentdomain.MethodXunhupay }
func (p *xunhupayProvider) Describe() paymentdomain.ProviderMeta {
	return finalizeMeta(paymentdomain.ProviderMeta{
		Method:       paymentdomain.MethodXunhupay,
		Name:         "虎皮椒 (XunhuPay)",
		Description:  "面向个人开发者的免签约聚合网关，支持支付宝与微信当面付，开通门槛低。",
		Category:     paymentdomain.CategoryAggregate,
		Icon:         "wechat",
		BrandColor:   "#07C160",
		DocURL:       "https://admin.xunhupay.com/page/client/developer_document.html",
		CallbackPath: "/api/public/pay/callback/" + paymentdomain.MethodXunhupay,
		CallbackNote: "回调为表单 POST，按参数字典序 + AppSecret 做 MD5 验签。",
		Regions:      []string{"中国大陆"},
		Currencies:   []string{"CNY"},
		Capabilities: paymentdomain.ProviderCapabilities{
			Redirect: true, QRCode: true, Webhook: true, WebhookSignature: true, RemoteQuery: true,
		},
		PayTypes: []paymentdomain.PayTypeOption{
			payType("alipay", "支付宝", ""),
			payType("wxpay", "微信支付", "可配置独立的微信支付网关地址"),
		},
		Fields: fields(
			inGroup(paymentdomain.GroupCredential,
				fText("appId", "应用 ID", "虎皮椒 App ID", "商户后台的应用标识", true),
				fSecret("appSecret", "应用密钥", "App Secret", "用于请求签名与回调验签", true),
				fURL("apiUrl", "网关地址", "https://api.xunhupay.com/payment/do.html", "留空使用官方默认网关"),
			),
			callbackFields("虎皮椒服务端异步通知地址", ""),
			limitFields("0.01", "50000"),
			advanced(inGroup(paymentdomain.GroupAdvanced,
				fURL("wxpayUrl", "微信独立网关", "https://api.xunhupay.com/payment/do.html", "部分商户的微信通道使用独立域名，留空则复用主网关"),
			)...),
		),
	})
}

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
	// 发送带合法签名的查单请求：返回「订单不存在」类业务错误即证明凭据与连通性正常
	params := map[string]string{
		"appid":           cfg.AppID,
		"out_trade_order": "AEGIS_CONN_TEST",
		"time":            fmt.Sprintf("%d", time.Now().Unix()),
		"nonce_str":       randomDigits(16),
	}
	params["hash"] = xunhupaySign(params, cfg.AppSecret)
	resp, err := p.client.R().SetContext(ctx).SetFormData(params).Post(apiURL + "/payment/query.html")
	if err != nil {
		return map[string]any{"config_valid": true, "api_accessible": false, "error": err.Error()}, nil
	}
	return map[string]any{"config_valid": true, "api_accessible": resp.IsSuccess(), "status": resp.StatusCode(), "body": resp.String()}, nil
}

func (p *xunhupayProvider) CreateOrder(ctx context.Context, data map[string]any, req PaymentOrderRequest) (*paymentdomain.PaymentPayload, error) {
	cfg, err := decodeXunhupayConfig(data)
	if err != nil {
		return nil, err
	}
	// 限额校验已上移到网关层（enforceAmountLimits），此处不再重复
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
		// 协议要求：time 为请求发起的当前 Unix 时间戳（原实现误用订单过期时间，且 ExpireAt 为空时会崩溃）
		"time": fmt.Sprintf("%d", time.Now().Unix()),
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
		"appid":           cfg.AppID,
		"out_trade_order": orderNo,
		"time":            fmt.Sprintf("%d", time.Now().Unix()),
		"nonce_str":       randomDigits(16),
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
	// 验签参数集排除签名本身与框架注入的保留键
	verifyData := map[string]string{}
	for k, v := range callbackData {
		if k == "hash" || strings.HasPrefix(k, "__") || strings.TrimSpace(v) == "" {
			continue
		}
		verifyData[k] = v
	}
	if !strings.EqualFold(xunhupaySign(verifyData, cfg.AppSecret), hash) {
		return nil, apperrors.New(40076, http.StatusBadRequest, "签名验证失败")
	}

	status := callbackData["status"]
	paid := status == "OD" // OD = Order Done

	// 回填回调金额，启用服务层「金额与订单一致」防线
	amount := decimal.Zero
	if parsed, perr := decimal.NewFromString(strings.TrimSpace(callbackData["total_fee"])); perr == nil {
		amount = parsed
	}

	return &paymentdomain.CallbackResult{
		Success:         true,
		Paid:            paid,
		OrderNo:         callbackData["trade_order_id"],
		ProviderOrderNo: callbackData["transaction_id"],
		TradeStatus:     status,
		PaymentMethod:   callbackData["payment_type"],
		Amount:          amount,
		RawData:         mapStringAny(callbackData),
	}, nil
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
