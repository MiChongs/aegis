package service

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	paymentdomain "aegis/internal/domain/payment"
	apperrors "aegis/pkg/errors"

	"github.com/shopspring/decimal"
	alipaysdk "github.com/smartwalle/alipay/v3"
)

// ── 支付宝原生 Provider（主流 SDK smartwalle/alipay/v3）──
// 支持三种子类型：page（电脑网站跳转）/ wap（手机网站跳转）/ qrcode（当面付扫码，默认）。
// 回调为表单 POST，DecodeNotification 内部完成 RSA2 验签。

type alipayNativeProvider struct{}

func newAlipayNativeProvider(_ any) *alipayNativeProvider { return &alipayNativeProvider{} }

func (p *alipayNativeProvider) Name() string { return paymentdomain.MethodAlipayNative }

func (p *alipayNativeProvider) Describe() paymentdomain.ProviderMeta {
	return finalizeMeta(paymentdomain.ProviderMeta{
		Method:       paymentdomain.MethodAlipayNative,
		Name:         "支付宝原生",
		Description:  "支付宝开放平台官方直连，资金直达商户账户，支持电脑网站 / 手机网站 / 当面付扫码。",
		Category:     paymentdomain.CategoryOfficialCN,
		Icon:         "alipay",
		BrandColor:   "#1677FF",
		DocURL:       "https://opendocs.alipay.com/open/270/105898",
		CallbackPath: "/api/public/pay/callback/" + paymentdomain.MethodAlipayNative,
		CallbackNote: "回调为表单 POST，SDK 内部完成 RSA2 验签；需在支付宝开放平台的应用网关中登记该地址。",
		Regions:      []string{"中国大陆"},
		Currencies:   []string{"CNY"},
		Capabilities: paymentdomain.ProviderCapabilities{
			Redirect: true, QRCode: true, Webhook: true, WebhookSignature: true, RemoteQuery: true, Sandbox: true,
			Refund: true, PartialRefund: true, RefundQuery: true,
		},
		PayTypes: []paymentdomain.PayTypeOption{
			payType("qrcode", "当面付扫码", "预下单返回二维码内容，默认方式"),
			payType("page", "电脑网站支付", "跳转 PC 收银台"),
			payType("wap", "手机网站支付", "跳转 H5 收银台"),
		},
		Fields: fields(
			inGroup(paymentdomain.GroupCredential,
				fText("appId", "应用 AppID", "2021...", "开放平台创建的应用 ID", true),
				fArea("privateKey", "应用私钥", "MIIEvgIBADANBg...", "PKCS#8 格式应用私钥，仅存于服务端", true),
				fArea("alipayPublicKey", "支付宝公钥", "MIIBIjANBg...", "公钥模式必填；证书模式可留空", false),
			),
			callbackFields(
				"支付宝异步通知地址（notify_url）",
				"用户支付完成后跳转的前端页面（return_url）",
			),
			limitFields("0.01", "50000"),
			advanced(inGroup(paymentdomain.GroupAdvanced,
				fSelect("signType", "签名算法", "支付宝当前统一使用 RSA2", "RSA2",
					opt("RSA2", "RSA2（推荐）"), opt("RSA", "RSA（已废弃）")),
				fSwitch("isSandbox", "沙箱环境", "开启后请求支付宝沙箱网关", false),
				fSwitch("certMode", "证书模式", "开启后改用应用证书 + 根证书 + 支付宝公钥证书验签", false),
				fText("appCertPath", "应用证书路径", "cert/appCertPublicKey.crt", "证书模式必填，服务端本地绝对/相对路径", false),
				fText("alipayCertPath", "支付宝公钥证书路径", "cert/alipayCertPublicKey.crt", "证书模式必填", false),
				fText("rootCertPath", "支付宝根证书路径", "cert/alipayRootCert.crt", "证书模式必填", false),
			)...),
		),
	})
}

func (p *alipayNativeProvider) decodeConfig(data map[string]any) (*paymentdomain.AlipayNativeConfig, error) {
	cfg, err := decodeProviderConfig[paymentdomain.AlipayNativeConfig](data)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.AppID) == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "支付宝配置不完整：缺少 appId")
	}
	if strings.TrimSpace(cfg.PrivateKey) == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "支付宝配置不完整：缺少商户私钥")
	}
	if !cfg.CertMode && strings.TrimSpace(cfg.AlipayPublicKey) == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "支付宝配置不完整：公钥模式需要提供支付宝公钥")
	}
	if cfg.CertMode && (strings.TrimSpace(cfg.AppCertPath) == "" || strings.TrimSpace(cfg.RootCertPath) == "" || strings.TrimSpace(cfg.AlipayCertPath) == "") {
		return nil, apperrors.New(40078, http.StatusBadRequest, "支付宝配置不完整：证书模式需要应用证书 / 根证书 / 支付宝证书路径")
	}
	return cfg, nil
}

func (p *alipayNativeProvider) ValidateConfig(data map[string]any) error {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return err
	}
	// 真实构建一次客户端验证私钥与证书可解析
	_, err = p.newClient(cfg)
	return err
}

func (p *alipayNativeProvider) newClient(cfg *paymentdomain.AlipayNativeConfig) (*alipaysdk.Client, error) {
	client, err := alipaysdk.New(cfg.AppID, cfg.PrivateKey, !cfg.IsSandbox)
	if err != nil {
		return nil, apperrors.New(40078, http.StatusBadRequest, "支付宝客户端初始化失败（私钥无法解析）："+err.Error())
	}
	if cfg.CertMode {
		if err := client.LoadAppCertPublicKeyFromFile(cfg.AppCertPath); err != nil {
			return nil, apperrors.New(40078, http.StatusBadRequest, "支付宝应用证书加载失败："+err.Error())
		}
		if err := client.LoadAliPayRootCertFromFile(cfg.RootCertPath); err != nil {
			return nil, apperrors.New(40078, http.StatusBadRequest, "支付宝根证书加载失败："+err.Error())
		}
		if err := client.LoadAlipayCertPublicKeyFromFile(cfg.AlipayCertPath); err != nil {
			return nil, apperrors.New(40078, http.StatusBadRequest, "支付宝公钥证书加载失败："+err.Error())
		}
	} else {
		if err := client.LoadAliPayPublicKey(cfg.AlipayPublicKey); err != nil {
			return nil, apperrors.New(40078, http.StatusBadRequest, "支付宝公钥加载失败："+err.Error())
		}
	}
	return client, nil
}

func (p *alipayNativeProvider) TestConnection(ctx context.Context, data map[string]any) (map[string]any, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return map[string]any{"config_valid": false, "error": err.Error()}, nil
	}
	client, err := p.newClient(cfg)
	if err != nil {
		return map[string]any{"config_valid": false, "error": err.Error()}, nil
	}
	// 查询一笔不存在的订单：网关返回业务错误（如 TRADE_NOT_EXIST）即证明签名与连通性正常
	rsp, err := client.TradeQuery(ctx, alipaysdk.TradeQuery{OutTradeNo: "AEGIS_CONN_TEST"})
	if err != nil {
		return map[string]any{"config_valid": true, "api_accessible": false, "error": err.Error()}, nil
	}
	return map[string]any{
		"config_valid":   true,
		"api_accessible": true,
		"gateway_code":   string(rsp.Code),
		"gateway_msg":    rsp.Msg,
	}, nil
}

func (p *alipayNativeProvider) CreateOrder(ctx context.Context, data map[string]any, req PaymentOrderRequest) (*paymentdomain.PaymentPayload, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	client, err := p.newClient(cfg)
	if err != nil {
		return nil, err
	}
	trade := alipaysdk.Trade{
		Subject:     req.Subject,
		OutTradeNo:  req.OrderNo,
		TotalAmount: req.Amount.StringFixed(2),
		NotifyURL:   pickString(req.NotifyURL, cfg.NotifyURL),
		ReturnURL:   pickString(req.ReturnURL, cfg.ReturnURL),
	}
	if req.ExpireAt != nil {
		// 支付宝绝对超时时间要求北京时间（GMT+8），不依赖服务器本地时区
		trade.TimeExpire = req.ExpireAt.In(time.FixedZone("CST", 8*3600)).Format("2006-01-02 15:04:05")
	}

	switch strings.ToLower(strings.TrimSpace(req.ProviderType)) {
	case "page", "pc":
		trade.ProductCode = "FAST_INSTANT_TRADE_PAY"
		payURL, err := client.TradePagePay(alipaysdk.TradePagePay{Trade: trade})
		if err != nil {
			return nil, apperrors.New(50084, http.StatusBadGateway, "支付宝下单失败："+err.Error())
		}
		return &paymentdomain.PaymentPayload{
			Success: true, OrderNo: req.OrderNo,
			PaymentURL: payURL.String(), RedirectURL: payURL.String(),
			ProviderType: "alipay_page",
		}, nil
	case "wap", "h5":
		trade.ProductCode = "QUICK_WAP_WAY"
		payURL, err := client.TradeWapPay(alipaysdk.TradeWapPay{Trade: trade})
		if err != nil {
			return nil, apperrors.New(50084, http.StatusBadGateway, "支付宝下单失败："+err.Error())
		}
		return &paymentdomain.PaymentPayload{
			Success: true, OrderNo: req.OrderNo,
			PaymentURL: payURL.String(), RedirectURL: payURL.String(),
			ProviderType: "alipay_wap",
		}, nil
	default: // qrcode / precreate：当面付扫码
		rsp, err := client.TradePreCreate(ctx, alipaysdk.TradePreCreate{Trade: trade})
		if err != nil {
			return nil, apperrors.New(50084, http.StatusBadGateway, "支付宝下单失败："+err.Error())
		}
		if rsp.IsFailure() {
			return nil, apperrors.New(50084, http.StatusBadGateway, "支付宝下单失败："+rsp.Msg+" "+rsp.SubMsg)
		}
		return &paymentdomain.PaymentPayload{
			Success: true, OrderNo: req.OrderNo,
			PaymentURL:   rsp.QRCode,
			ProviderType: "alipay_qrcode",
			Message:      "请使用支付宝扫描二维码完成支付",
			FormData:     map[string]any{"qrCode": rsp.QRCode},
		}, nil
	}
}

func (p *alipayNativeProvider) QueryRemoteOrder(ctx context.Context, data map[string]any, orderNo string) (map[string]any, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	client, err := p.newClient(cfg)
	if err != nil {
		return nil, err
	}
	rsp, err := client.TradeQuery(ctx, alipaysdk.TradeQuery{OutTradeNo: orderNo})
	if err != nil {
		return nil, apperrors.New(50084, http.StatusBadGateway, "支付宝查询失败："+err.Error())
	}
	return map[string]any{
		"code":         string(rsp.Code),
		"msg":          rsp.Msg,
		"trade_no":     rsp.TradeNo,
		"trade_status": string(rsp.TradeStatus),
		"total_amount": rsp.TotalAmount,
		"paid":         rsp.TradeStatus == alipaysdk.TradeStatusSuccess || rsp.TradeStatus == alipaysdk.TradeStatusFinished,
	}, nil
}

// Refund 支付宝退款（同步返回结果）。
// OutRequestNo 传本地退款单号：支付宝以「交易号 + 退款请求号」做幂等，
// 同一请求号重复提交只退一笔，因此网络重试是安全的。
func (p *alipayNativeProvider) Refund(ctx context.Context, data map[string]any, req paymentdomain.RefundRequest) (*paymentdomain.RefundResult, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	client, err := p.newClient(cfg)
	if err != nil {
		return nil, err
	}
	rsp, err := client.TradeRefund(ctx, alipaysdk.TradeRefund{
		OutTradeNo:   req.OrderNo,
		TradeNo:      strings.TrimSpace(req.ProviderOrderNo),
		RefundAmount: req.RefundAmount.StringFixed(2),
		RefundReason: req.Reason,
		OutRequestNo: req.RefundNo,
	})
	if err != nil {
		return nil, apperrors.New(50090, http.StatusBadGateway, "支付宝退款请求失败："+err.Error())
	}
	if rsp.IsFailure() {
		return &paymentdomain.RefundResult{
			Status:  paymentdomain.RefundFailed,
			Message: strings.TrimSpace(rsp.Msg + " " + rsp.SubMsg),
			Raw:     map[string]any{"code": string(rsp.Code), "sub_code": rsp.SubCode, "sub_msg": rsp.SubMsg},
		}, nil
	}
	amount := decimal.Zero
	if parsed, perr := decimal.NewFromString(strings.TrimSpace(rsp.RefundFee)); perr == nil {
		amount = parsed
	}
	return &paymentdomain.RefundResult{
		Status:           paymentdomain.RefundSuccess,
		ProviderRefundNo: firstNonEmpty(rsp.TradeNo, req.RefundNo),
		Amount:           amount,
		Raw: map[string]any{
			"trade_no":    rsp.TradeNo,
			"fund_change": rsp.FundChange,
			"refund_fee":  rsp.RefundFee,
		},
	}, nil
}

// QueryRefund 查询退款单状态（用于同步失败后的补偿核对）
func (p *alipayNativeProvider) QueryRefund(ctx context.Context, data map[string]any, query paymentdomain.RefundQuery) (*paymentdomain.RefundResult, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	client, err := p.newClient(cfg)
	if err != nil {
		return nil, err
	}
	rsp, err := client.TradeFastPayRefundQuery(ctx, alipaysdk.TradeFastPayRefundQuery{
		OutTradeNo:   query.OrderNo,
		OutRequestNo: query.RefundNo,
	})
	if err != nil {
		return nil, apperrors.New(50090, http.StatusBadGateway, "支付宝退款查询失败："+err.Error())
	}
	if rsp.IsFailure() {
		return &paymentdomain.RefundResult{
			Status:  paymentdomain.RefundProcessing,
			Message: strings.TrimSpace(rsp.Msg + " " + rsp.SubMsg),
		}, nil
	}
	amount := decimal.Zero
	if parsed, perr := decimal.NewFromString(strings.TrimSpace(rsp.RefundAmount)); perr == nil {
		amount = parsed
	}
	// 支付宝文档：未返回 refund_status 表示「退款请求未收到或退款失败」，
	// 只有明确 REFUND_SUCCESS 才算成功，不能仅凭金额非零判定
	status := paymentdomain.RefundProcessing
	if strings.EqualFold(strings.TrimSpace(rsp.RefundStatus), "REFUND_SUCCESS") {
		status = paymentdomain.RefundSuccess
	}
	return &paymentdomain.RefundResult{
		Status:           status,
		ProviderRefundNo: rsp.TradeNo,
		Amount:           amount,
		Raw:              map[string]any{"refund_amount": rsp.RefundAmount, "refund_status": rsp.RefundStatus},
	}, nil
}

func (p *alipayNativeProvider) HandleCallback(ctx context.Context, data map[string]any, callbackData map[string]string, clientIP string) (*paymentdomain.CallbackResult, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	client, err := p.newClient(cfg)
	if err != nil {
		return nil, err
	}
	// 还原表单参数（剔除框架注入的保留键），DecodeNotification 内部完成验签
	values := url.Values{}
	for k, v := range callbackData {
		if strings.HasPrefix(k, "__") {
			continue
		}
		values.Set(k, v)
	}
	// v3.2.31 起 DecodeNotification 需要 context（内部完成 RSA2 验签，可能回源拉取支付宝公钥）
	noti, err := client.DecodeNotification(ctx, values)
	if err != nil {
		return nil, apperrors.New(40076, http.StatusBadRequest, "支付宝回调验签失败："+err.Error())
	}
	paid := noti.TradeStatus == alipaysdk.TradeStatusSuccess || noti.TradeStatus == alipaysdk.TradeStatusFinished
	amount := decimal.Zero
	if parsed, perr := decimal.NewFromString(strings.TrimSpace(noti.TotalAmount)); perr == nil {
		amount = parsed
	}
	return &paymentdomain.CallbackResult{
		Success:         true,
		Paid:            paid,
		OrderNo:         noti.OutTradeNo,
		ProviderOrderNo: noti.TradeNo,
		TradeStatus:     string(noti.TradeStatus),
		PaymentMethod:   "alipay",
		Amount:          amount,
		RawData:         mapStringAny(callbackData),
	}, nil
}
