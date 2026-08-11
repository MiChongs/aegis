package service

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	paymentdomain "aegis/internal/domain/payment"
	apperrors "aegis/pkg/errors"

	"github.com/shopspring/decimal"
	"github.com/wechatpay-apiv3/wechatpay-go/core"
	"github.com/wechatpay-apiv3/wechatpay-go/core/auth/verifiers"
	"github.com/wechatpay-apiv3/wechatpay-go/core/downloader"
	"github.com/wechatpay-apiv3/wechatpay-go/core/notify"
	"github.com/wechatpay-apiv3/wechatpay-go/core/option"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments"
	"github.com/wechatpay-apiv3/wechatpay-go/services/payments/native"
	"github.com/wechatpay-apiv3/wechatpay-go/services/refunddomestic"
	"github.com/wechatpay-apiv3/wechatpay-go/utils"
)

// ── 微信支付原生 Provider（官方 SDK wechatpay-apiv3/wechatpay-go，APIv3）──
// 下单走 Native 预下单（返回二维码内容 code_url），回调为加密 JSON 通知：
// 平台证书自动下载 → RSA 验签 → AES-256-GCM 解密。
// 注意：微信 v3 通知地址不允许携带查询参数，因此回调路由使用路径参数
// /api/public/pay/callback/wechat_native/{appid} 定位应用配置。

type wechatNativeProvider struct{}

func newWechatNativeProvider(_ any) *wechatNativeProvider { return &wechatNativeProvider{} }

func (p *wechatNativeProvider) Name() string { return paymentdomain.MethodWechatNative }

func (p *wechatNativeProvider) Describe() paymentdomain.ProviderMeta {
	return finalizeMeta(paymentdomain.ProviderMeta{
		Method:       paymentdomain.MethodWechatNative,
		Name:         "微信支付原生",
		Description:  "微信支付商户平台 APIv3 官方直连，Native 预下单扫码支付，支持服务商子商户模式。",
		Category:     paymentdomain.CategoryOfficialCN,
		Icon:         "wechat",
		BrandColor:   "#07C160",
		DocURL:       "https://pay.weixin.qq.com/doc/v3/merchant/4012791877",
		CallbackPath: "/api/public/pay/callback/" + paymentdomain.MethodWechatNative,
		CallbackNote: "微信 v3 通知地址禁止携带查询参数：只填到 /callback/wechat_native 为止，系统下单时会自动以路径段追加应用 ID。回调经平台证书 RSA 验签 + AES-256-GCM 解密。",
		Regions:      []string{"中国大陆"},
		Currencies:   []string{"CNY"},
		Capabilities: paymentdomain.ProviderCapabilities{
			QRCode: true, Webhook: true, WebhookSignature: true, RemoteQuery: true, Sandbox: true, SubMerchant: true,
			Refund: true, PartialRefund: true, RefundQuery: true,
		},
		PayTypes: []paymentdomain.PayTypeOption{
			payType("wxpay", "微信扫码 (Native)", "预下单返回 code_url 二维码内容"),
		},
		Fields: fields(
			inGroup(paymentdomain.GroupCredential,
				fText("appId", "应用 AppID", "wx...", "公众号 / 小程序 / 开放平台应用 ID", true),
				fText("mchId", "商户号", "14000...", "微信支付商户平台的商户号", true),
				fSecret("apiKeyV3", "APIv3 密钥", "32 位 APIv3 密钥", "商户平台 API 安全中设置，回调解密必需", true),
				fText("serialNo", "商户证书序列号", "证书 Serial No.", "apiclient_cert.pem 对应的序列号", true),
				fArea("privateKey", "商户私钥", "-----BEGIN PRIVATE KEY-----", "apiclient_key.pem 内容，仅存于服务端", true),
			),
			callbackFields(
				"填到 /api/public/pay/callback/wechat_native 为止，不要加任何查询参数",
				"用户支付完成后跳转的前端页面",
			),
			limitFields("0.01", "50000"),
			advanced(inGroup(paymentdomain.GroupAdvanced,
				fSecret("apiKey", "APIv2 密钥", "32 位 API 密钥", "仅在需要兼容 v2 接口时填写", false),
				fSwitch("isSandbox", "沙箱环境", "开启后请求微信支付沙箱网关", false),
				fText("subAppId", "子商户 AppID", "wx...", "服务商模式下的特约商户 AppID", false),
				fText("subMchId", "子商户号", "19000...", "服务商模式下的特约商户号", false),
			)...),
		),
	})
}

func (p *wechatNativeProvider) decodeConfig(data map[string]any) (*paymentdomain.WechatNativeConfig, error) {
	cfg, err := decodeProviderConfig[paymentdomain.WechatNativeConfig](data)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.AppID) == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "微信支付配置不完整：缺少 appId")
	}
	if strings.TrimSpace(cfg.MchID) == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "微信支付配置不完整：缺少商户号 mchId")
	}
	if strings.TrimSpace(cfg.APIKeyV3) == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "微信支付配置不完整：缺少 APIv3 密钥 apiKeyV3")
	}
	if strings.TrimSpace(cfg.SerialNo) == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "微信支付配置不完整：缺少商户证书序列号 serialNo")
	}
	if strings.TrimSpace(cfg.PrivateKey) == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "微信支付配置不完整：缺少商户私钥 privateKey")
	}
	return cfg, nil
}

func (p *wechatNativeProvider) ValidateConfig(data map[string]any) error {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return err
	}
	if _, err := utils.LoadPrivateKey(cfg.PrivateKey); err != nil {
		return apperrors.New(40078, http.StatusBadRequest, "微信支付商户私钥无法解析："+err.Error())
	}
	return nil
}

// newClient 构建官方客户端（自动管理平台证书下载与请求签名）
func (p *wechatNativeProvider) newClient(ctx context.Context, cfg *paymentdomain.WechatNativeConfig) (*core.Client, error) {
	privateKey, err := utils.LoadPrivateKey(cfg.PrivateKey)
	if err != nil {
		return nil, apperrors.New(40078, http.StatusBadRequest, "微信支付商户私钥无法解析："+err.Error())
	}
	client, err := core.NewClient(ctx, option.WithWechatPayAutoAuthCipher(cfg.MchID, cfg.SerialNo, privateKey, cfg.APIKeyV3))
	if err != nil {
		return nil, apperrors.New(50083, http.StatusBadGateway, "微信支付客户端初始化失败："+err.Error())
	}
	return client, nil
}

func (p *wechatNativeProvider) TestConnection(ctx context.Context, data map[string]any) (map[string]any, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return map[string]any{"config_valid": false, "error": err.Error()}, nil
	}
	// 客户端初始化会真实下载平台证书，等价于完整连通性与凭据校验
	if _, err := p.newClient(ctx, cfg); err != nil {
		return map[string]any{"config_valid": true, "api_accessible": false, "error": err.Error()}, nil
	}
	return map[string]any{"config_valid": true, "api_accessible": true, "message": "平台证书下载成功"}, nil
}

func (p *wechatNativeProvider) CreateOrder(ctx context.Context, data map[string]any, req PaymentOrderRequest) (*paymentdomain.PaymentPayload, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	client, err := p.newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	notifyBase := pickString(req.NotifyURL, cfg.NotifyURL)
	if notifyBase == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "微信支付需要配置 notifyUrl（公网回调地址）")
	}
	// 微信 v3 通知地址禁止查询参数：应用标识以路径段附加，配合 /callback/:method/:appid 路由
	notifyURL := strings.TrimRight(notifyBase, "/")
	if req.AppID > 0 {
		notifyURL += "/" + strconv.FormatInt(req.AppID, 10)
	}

	svc := native.NativeApiService{Client: client}
	prepay := native.PrepayRequest{
		Appid:       core.String(cfg.AppID),
		Mchid:       core.String(cfg.MchID),
		Description: core.String(req.Subject),
		OutTradeNo:  core.String(req.OrderNo),
		NotifyUrl:   core.String(notifyURL),
		Amount: &native.Amount{
			Total:    core.Int64(wechatFen(req.Amount)),
			Currency: core.String("CNY"),
		},
	}
	if req.ExpireAt != nil {
		prepay.TimeExpire = req.ExpireAt
	}
	resp, _, err := svc.Prepay(ctx, prepay)
	if err != nil {
		return nil, apperrors.New(50083, http.StatusBadGateway, "微信支付下单失败："+err.Error())
	}
	codeURL := ""
	if resp != nil && resp.CodeUrl != nil {
		codeURL = *resp.CodeUrl
	}
	return &paymentdomain.PaymentPayload{
		Success:      true,
		OrderNo:      req.OrderNo,
		PaymentURL:   codeURL,
		ProviderType: "wxpay_native",
		Message:      "请使用微信扫描 code_url 二维码完成支付",
		FormData:     map[string]any{"codeUrl": codeURL},
	}, nil
}

func (p *wechatNativeProvider) QueryRemoteOrder(ctx context.Context, data map[string]any, orderNo string) (map[string]any, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	client, err := p.newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	svc := native.NativeApiService{Client: client}
	txn, _, err := svc.QueryOrderByOutTradeNo(ctx, native.QueryOrderByOutTradeNoRequest{
		OutTradeNo: core.String(orderNo),
		Mchid:      core.String(cfg.MchID),
	})
	if err != nil {
		return nil, apperrors.New(50083, http.StatusBadGateway, "微信支付查询失败："+err.Error())
	}
	result := map[string]any{}
	if txn != nil {
		if txn.TradeState != nil {
			result["trade_state"] = *txn.TradeState
			result["paid"] = *txn.TradeState == "SUCCESS"
		}
		if txn.TransactionId != nil {
			result["transaction_id"] = *txn.TransactionId
		}
		if txn.Amount != nil && txn.Amount.Total != nil {
			result["total_fen"] = *txn.Amount.Total
		}
	}
	return result, nil
}

// Refund 微信支付退款（APIv3）。
// 微信可能返回 PROCESSING（受理中），此时结果由后续查询或退款通知确定，不能当作成功。
func (p *wechatNativeProvider) Refund(ctx context.Context, data map[string]any, req paymentdomain.RefundRequest) (*paymentdomain.RefundResult, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	client, err := p.newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	svc := refunddomestic.RefundsApiService{Client: client}
	createReq := refunddomestic.CreateRequest{
		OutTradeNo:  core.String(req.OrderNo),
		OutRefundNo: core.String(req.RefundNo),
		Amount: &refunddomestic.AmountReq{
			Refund:   core.Int64(wechatFen(req.RefundAmount)),
			Total:    core.Int64(wechatFen(req.TotalAmount)),
			Currency: core.String("CNY"),
		},
	}
	if reason := strings.TrimSpace(req.Reason); reason != "" {
		createReq.Reason = core.String(reason)
	}
	if txnID := strings.TrimSpace(req.ProviderOrderNo); txnID != "" {
		createReq.TransactionId = core.String(txnID)
	}
	if subMchID := strings.TrimSpace(cfg.SubMchID); subMchID != "" {
		createReq.SubMchid = core.String(subMchID)
	}
	resp, _, err := svc.Create(ctx, createReq)
	if err != nil {
		return nil, apperrors.New(50091, http.StatusBadGateway, "微信支付退款请求失败："+err.Error())
	}
	return wechatRefundResult(resp), nil
}

// QueryRefund 按商户退款单号查询退款结果（PROCESSING 态的补偿通道）
func (p *wechatNativeProvider) QueryRefund(ctx context.Context, data map[string]any, query paymentdomain.RefundQuery) (*paymentdomain.RefundResult, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	client, err := p.newClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	svc := refunddomestic.RefundsApiService{Client: client}
	queryReq := refunddomestic.QueryByOutRefundNoRequest{OutRefundNo: core.String(query.RefundNo)}
	if subMchID := strings.TrimSpace(cfg.SubMchID); subMchID != "" {
		queryReq.SubMchid = core.String(subMchID)
	}
	resp, _, err := svc.QueryByOutRefundNo(ctx, queryReq)
	if err != nil {
		return nil, apperrors.New(50091, http.StatusBadGateway, "微信支付退款查询失败："+err.Error())
	}
	return wechatRefundResult(resp), nil
}

// wechatRefundResult 把微信退款单映射为统一退款结果
func wechatRefundResult(refund *refunddomestic.Refund) *paymentdomain.RefundResult {
	if refund == nil {
		return &paymentdomain.RefundResult{Status: paymentdomain.RefundProcessing, Message: "微信支付未返回退款单"}
	}
	state := ""
	if refund.Status != nil {
		state = string(*refund.Status)
	}
	result := &paymentdomain.RefundResult{
		Status: wechatRefundStatus(state),
		Raw:    map[string]any{"status": state},
	}
	if refund.RefundId != nil {
		result.ProviderRefundNo = *refund.RefundId
		result.Raw["refund_id"] = *refund.RefundId
	}
	if refund.Amount != nil && refund.Amount.Refund != nil {
		result.Amount = decimal.NewFromInt(*refund.Amount.Refund).Div(decimal.NewFromInt(100))
	}
	if result.Status != paymentdomain.RefundSuccess {
		result.Message = "微信退款状态：" + state
	}
	return result
}

// wechatRefundStatus 微信退款状态 → 本地终态判定。
// ABNORMAL（退款异常，需商户平台人工处理）归入 processing 而非 failed：
// 资金可能已经在途，直接判失败会错误地把额度释放给下一笔退款。
func wechatRefundStatus(state string) string {
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case "SUCCESS":
		return paymentdomain.RefundSuccess
	case "CLOSED":
		return paymentdomain.RefundClosed
	default:
		return paymentdomain.RefundProcessing
	}
}

func (p *wechatNativeProvider) HandleCallback(ctx context.Context, data map[string]any, callbackData map[string]string, clientIP string) (*paymentdomain.CallbackResult, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	rawBody := callbackData[CallbackRawBodyKey]
	if rawBody == "" {
		return nil, apperrors.New(40075, http.StatusBadRequest, "微信支付通知缺少原始报文")
	}
	privateKey, err := utils.LoadPrivateKey(cfg.PrivateKey)
	if err != nil {
		return nil, apperrors.New(40078, http.StatusBadRequest, "微信支付商户私钥无法解析")
	}
	// 注册平台证书下载器（按商户号幂等注册）
	mgr := downloader.MgrInstance()
	if !mgr.HasDownloader(ctx, cfg.MchID) {
		if err := mgr.RegisterDownloaderWithPrivateKey(ctx, privateKey, cfg.SerialNo, cfg.MchID, cfg.APIKeyV3); err != nil {
			return nil, apperrors.New(50083, http.StatusBadGateway, "微信平台证书下载失败："+err.Error())
		}
	}
	handler := notify.NewNotifyHandler(cfg.APIKeyV3, verifiers.NewSHA256WithRSAVerifier(mgr.GetCertificateVisitor(cfg.MchID)))

	// 由透传的原始报文 + 签名头重建请求供 SDK 验签解密
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "/", strings.NewReader(rawBody))
	if err != nil {
		return nil, err
	}
	for _, name := range []string{"Wechatpay-Signature", "Wechatpay-Serial", "Wechatpay-Timestamp", "Wechatpay-Nonce", "Wechatpay-Signature-Type"} {
		if v := callbackHeader(callbackData, name); v != "" {
			httpReq.Header.Set(name, v)
		}
	}
	transaction := new(payments.Transaction)
	if _, err := handler.ParseNotifyRequest(ctx, httpReq, transaction); err != nil {
		return nil, apperrors.New(40076, http.StatusBadRequest, "微信支付通知验签/解密失败："+err.Error())
	}

	tradeState := ""
	if transaction.TradeState != nil {
		tradeState = *transaction.TradeState
	}
	orderNo := ""
	if transaction.OutTradeNo != nil {
		orderNo = *transaction.OutTradeNo
	}
	providerOrderNo := ""
	if transaction.TransactionId != nil {
		providerOrderNo = *transaction.TransactionId
	}
	amount := decimal.Zero
	if transaction.Amount != nil && transaction.Amount.Total != nil {
		amount = decimal.NewFromInt(*transaction.Amount.Total).Div(decimal.NewFromInt(100))
	}
	return &paymentdomain.CallbackResult{
		Success:         true,
		Paid:            tradeState == "SUCCESS",
		OrderNo:         orderNo,
		ProviderOrderNo: providerOrderNo,
		TradeStatus:     tradeState,
		PaymentMethod:   "wxpay",
		Amount:          amount,
		RawData:         map[string]any{"trade_state": tradeState, "transaction_id": providerOrderNo, "out_trade_no": orderNo},
	}, nil
}

// wechatFen 主单位元 → 分
func wechatFen(amount decimal.Decimal) int64 {
	return amount.Mul(decimal.NewFromInt(100)).Round(0).IntPart()
}
