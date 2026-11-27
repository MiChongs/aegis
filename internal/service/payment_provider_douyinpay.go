package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	paymentdomain "aegis/internal/domain/payment"
	apperrors "aegis/pkg/errors"

	"github.com/douyinpay/douyinpay-go/client"
	"github.com/douyinpay/douyinpay-go/tools/auth"
	"github.com/douyinpay/douyinpay-go/tools/auth/signers"
	"github.com/douyinpay/douyinpay-go/tools/auth/verifiers"
	"github.com/douyinpay/douyinpay-go/tools/consts"
	"github.com/douyinpay/douyinpay-go/tools/downloader"
	"github.com/douyinpay/douyinpay-go/utils"
	"github.com/go-resty/resty/v2"
	"github.com/shopspring/decimal"
	"github.com/tjfoc/gmsm/sm2"
	"github.com/tjfoc/gmsm/sm4"
	"github.com/tjfoc/gmsm/x509"
)

// ── 抖音支付 Provider ──
//
// 接口文档：https://pay.douyinpay.com/wiki（API 字典 · 基础支付 / 通用规则）。
// 协议与微信支付 APIv3 同构：请求用商户 API 私钥签名（Authorization: DouyinPay-RSA / DouyinPay-SM2），
// 应答与回调用平台证书验签，回调正文用「接口加密密钥」AES-256-GCM（国密商户为 SM4）加密。
//
// 签名、验签、平台证书下载与解析复用官方 Go SDK github.com/douyinpay/douyinpay-go（Apache-2.0）；
// 业务请求自己拼报文，因为直连商户与服务商两种模式只差路径和几个字段名，
// SDK 为两种模式各生成了一套 Service，照着它写会是八份几乎一样的代码。
//
// 付款方式（子类型）：
//
//	h5      H5 支付：返回 h5_url，打开即拉起抖音 App 收银台（网站应用 AppID）
//	native  Native 支付：返回 code_url，用抖音扫一扫付款
//	app     App 支付：返回调起 OpenSDK 所需的已签名参数（移动应用 AppID）
//	jsapi   JSAPI 支付：抖音内打开的网页，需付款人 openid（网站应用 AppID）
//	mini    小程序支付：tt.payOffline，需付款人 openid（小程序 AppID）
//
// 支付通知与退款通知共用下单时给的 notify_url，地址不许带查询参数，
// 所以与微信一样用路径段携带应用 ID：/api/public/pay/callback/douyinpay/{appid}。

const (
	douyinPayServer = consts.DouyinPayServer

	douyinPayTypeH5     = "h5"
	douyinPayTypeNative = "native"
	douyinPayTypeApp    = "app"
	douyinPayTypeJSAPI  = "jsapi"
	douyinPayTypeMini   = "mini"

	douyinSignRSA = consts.CRYPTO_TYPE_RSA
	douyinSignSM2 = consts.CRYPTO_TYPE_SM2

	// 平台证书定期重新下载的间隔：官方建议 6~12 小时，证书轮换时新证书会提前进列表
	douyinCertRefreshInterval = 12 * time.Hour
	// 验签时遇到不认识的证书序列号会立刻重新下载，但两次之间至少隔这么久，免得被假报文刷接口
	douyinCertMissRefreshGap = time.Minute
	// 回调时间戳允许的偏差（与 SDK 一致）
	douyinCallbackClockSkew = consts.MaxCallbackClockOffset

	// 后台补单每轮最多查这么多笔（单笔一次 GET），超出的留给下一轮
	douyinSettleBatchCap = 40
)

var douyinPayTypes = []paymentdomain.PayTypeOption{
	payType(douyinPayTypeH5, "抖音支付", "跳转抖音 App 收银台完成付款"),
	payType(douyinPayTypeNative, "抖音扫码", "用抖音扫一扫二维码付款"),
	payType(douyinPayTypeApp, "抖音 App 支付", "App 内集成抖音支付 OpenSDK 调起"),
	payType(douyinPayTypeJSAPI, "抖音网页支付", "抖音内打开的网页（JSAPI），需付款人 openid"),
	payType(douyinPayTypeMini, "抖音小程序支付", "抖音小程序内 tt.payOffline 调起，需付款人 openid"),
}

// douyinDefaultPayTypes 管理员没填 supportedTypes 时对用户开放的付款方式。
// App / JSAPI / 小程序都要客户端另做集成，不能默认放给不认识它们的客户端。
var douyinDefaultPayTypes = []string{douyinPayTypeH5, douyinPayTypeNative}

type douyinPayProvider struct {
	httpClient *http.Client // nil 时用 SDK 默认客户端

	certMu sync.Mutex
	certs  map[string]*douyinCertSource // 凭据指纹 → 平台证书来源

	settleMu   sync.Mutex
	settleSeen map[string]douyinSettleMark // 后台补单的退避记录
}

func newDouyinPayProvider(rc *resty.Client) *douyinPayProvider {
	p := &douyinPayProvider{
		certs:      map[string]*douyinCertSource{},
		settleSeen: map[string]douyinSettleMark{},
	}
	// 共用网关的出站传输层（出海代理等），超时单独给：resty 的超时设在它自己那层
	if rc != nil {
		if hc := rc.GetClient(); hc != nil && hc.Transport != nil {
			p.httpClient = &http.Client{Transport: hc.Transport, Timeout: 30 * time.Second}
		}
	}
	return p
}

func (p *douyinPayProvider) Name() string { return paymentdomain.MethodDouyinPay }

func (p *douyinPayProvider) Describe() paymentdomain.ProviderMeta {
	return finalizeMeta(paymentdomain.ProviderMeta{
		Method:       paymentdomain.MethodDouyinPay,
		Name:         "抖音支付",
		Description:  "抖音支付商家平台官方 API 直连：H5、Native 扫码、App、JSAPI、小程序五种付款方式，支持退款、查单与服务商模式，RSA 与国密 SM2 证书均可。",
		Category:     paymentdomain.CategoryOfficialCN,
		Icon:         "tiktok",
		BrandColor:   "#FE2C55",
		DocURL:       "https://pay.douyinpay.com/wiki/639fd48f17c2f3021d237f61/639fd4d3e0c64802240b4075",
		CallbackPath: "/api/public/pay/callback/" + paymentdomain.MethodDouyinPay,
		CallbackNote: "抖音支付通知地址必须是 https 且禁止带查询参数：只填到 /callback/douyinpay 为止，系统下单与退款时会自动以路径段追加应用 ID。支付与退款通知共用该地址，经平台证书验签后用接口加密密钥解密。",
		Regions:      []string{"中国大陆"},
		Currencies:   []string{"CNY"},
		Capabilities: paymentdomain.ProviderCapabilities{
			Redirect: true, QRCode: true, Webhook: true, WebhookSignature: true, RemoteQuery: true, SubMerchant: true,
			Refund: true, PartialRefund: true, RefundQuery: true,
		},
		PayTypes: douyinPayTypes,
		Fields: fields(
			inGroup(paymentdomain.GroupCredential,
				fText("mchId", "商户号", "6020…", "抖音支付商家平台的商户号；服务商模式填服务商商户号", true),
				fSelect("signType", "证书类型", "商家平台【产品中心 › 开发配置 › 密钥管理】里申请的证书算法；一个商户号只能用一种", douyinSignRSA,
					opt(douyinSignRSA, "RSA（国际算法）"), opt(douyinSignSM2, "SM2（国密）")),
				fText("serialNo", "商户 API 证书序列号", "41A48FEF2A9F04C5", "密钥管理 › 接口加签证书 中的序列号", true),
				fArea("privateKey", "商户 API 私钥", "-----BEGIN PRIVATE KEY-----", "申请接口加签证书时生成的私钥 PEM，仅存于服务端", true),
				fSecret("encryptKey", "接口加密密钥", "RSA 证书为 32 位，国密为 16 位", "密钥管理 › 接口加密密钥；用于解密回调与平台证书", true),
				fText("appId", "移动应用 AppID", "aw…", "抖音开放平台移动应用的 AppID（client_key）：App 支付、Native 支付使用", false),
				fText("webAppId", "网站应用 AppID", "aw…", "抖音开放平台网站应用的 AppID：H5 支付、JSAPI 支付使用", false),
				fText("miniAppId", "小程序 AppID", "tt…", "抖音小程序的 AppID：小程序支付使用", false),
			),
			inGroup(paymentdomain.GroupGateway,
				fTags("supportedTypes", "开放的付款方式", "h5, native", "可填 h5 / native / app / jsapi / mini，顺序即用户端列表顺序；留空开放 h5 与 native。app、jsapi、mini 需要客户端另行集成"),
			),
			callbackFields(
				"填到 /api/public/pay/callback/douyinpay 为止，必须是 https，不要加任何查询参数",
				"H5 支付完成后在收银台点「返回商家」跳转的页面",
			),
			limitFields("0.01", "50000"),
			advanced(inGroup(paymentdomain.GroupAdvanced,
				fSelect("h5Type", "H5 场景类型", "下单时客户端没说明平台时 h5_info.type 用的值", "Wap",
					opt("Wap", "Wap（手机网页）"), opt("Android", "Android"), opt("iOS", "iOS"), opt("HarmonyOS", "HarmonyOS")),
				fText("h5AppName", "H5 应用名称", "我的应用", "h5_info.app_name", false),
				fURL("h5AppUrl", "H5 网站 URL", "https://www.example.com", "h5_info.app_url"),
				fText("h5BundleId", "iOS BundleID", "com.example.app", "h5_info.bundle_id", false),
				fText("h5PackageName", "Android 包名", "com.example.app", "h5_info.package_name", false),
				fURL("h5Referer", "H5 支付授权域名", "https://www.example.com", "商家平台登记的 H5 支付域名。App 内 WebView 打开 h5_url 时要带这个 Referer，否则收银台报「鉴权失败」"),
				fText("h5ReturnSchema", "H5 回跳 App scheme", "myapp://", "付款完成后从抖音跳回的 App scheme（拼到 h5_url 的 return_schema）", false),
				fArea("platformCertificate", "平台证书（可选）", "-----BEGIN CERTIFICATE-----", "留空则调接口自动下载并定期更新；填写后固定用它验签", false),
				fText("subMchId", "特约商户号", "6020…", "服务商模式：子商户号。填写即按服务商接口下单", false),
				fText("subAppId", "特约商户移动应用 AppID", "aw…", "服务商模式：子商户的移动应用 AppID（可选）", false),
				fText("subWebAppId", "特约商户网站应用 AppID", "aw…", "服务商模式：子商户的网站应用 AppID（可选）", false),
				fText("subMiniAppId", "特约商户小程序 AppID", "tt…", "服务商模式：子商户的小程序 AppID（可选）", false),
			)...),
		),
	})
}

// ── 配置 ──

func (p *douyinPayProvider) decodeConfig(data map[string]any) (*paymentdomain.DouyinPayConfig, error) {
	cfg, err := decodeProviderConfig[paymentdomain.DouyinPayConfig](data)
	if err != nil {
		return nil, err
	}
	cfg.MchID = strings.TrimSpace(cfg.MchID)
	cfg.SerialNo = strings.TrimSpace(cfg.SerialNo)
	cfg.SubMchID = strings.TrimSpace(cfg.SubMchID)
	cfg.SignType = strings.ToUpper(strings.TrimSpace(cfg.SignType))
	if cfg.SignType == "" {
		cfg.SignType = douyinSignRSA
	}
	missing := func(what string) error {
		return apperrors.New(40078, http.StatusBadRequest, "抖音支付配置不完整：缺少"+what)
	}
	switch {
	case cfg.MchID == "":
		return nil, missing("商户号 mchId")
	case cfg.SerialNo == "":
		return nil, missing("商户 API 证书序列号 serialNo")
	case strings.TrimSpace(cfg.PrivateKey) == "":
		return nil, missing("商户 API 私钥 privateKey")
	case cfg.EncryptKey == "":
		return nil, missing("接口加密密钥 encryptKey")
	}
	if cfg.SignType != douyinSignRSA && cfg.SignType != douyinSignSM2 {
		return nil, apperrors.New(40078, http.StatusBadRequest, "抖音支付证书类型只能是 RSA 或 SM2")
	}
	return cfg, nil
}

func (p *douyinPayProvider) ValidateConfig(data map[string]any) error {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return err
	}
	if _, err := loadDouyinKeys(cfg); err != nil {
		return err
	}
	if want := douyinEncryptKeyLen(cfg.SignType); len(cfg.EncryptKey) != want {
		return apperrors.New(40078, http.StatusBadRequest,
			fmt.Sprintf("抖音支付接口加密密钥长度应为 %d 字节（%s 证书），当前为 %d", want, cfg.SignType, len(cfg.EncryptKey)))
	}
	if strings.TrimSpace(cfg.PlatformCertificate) != "" {
		if _, err := parseDouyinCertificate(cfg.SignType, cfg.PlatformCertificate); err != nil {
			return apperrors.New(40078, http.StatusBadRequest, "抖音支付平台证书无法解析："+err.Error())
		}
	}
	types, _ := p.RoutedPayTypes(data)
	if len(types) == 0 {
		return apperrors.New(40078, http.StatusBadRequest, "抖音支付没有可用的付款方式")
	}
	for _, t := range types {
		if douyinAppIDFor(cfg, t.Value) == "" {
			return apperrors.New(40078, http.StatusBadRequest,
				fmt.Sprintf("抖音支付开放了「%s」，但没填它要用的 %s", t.Label, douyinAppIDFieldLabel(t.Value)))
		}
	}
	return nil
}

func douyinEncryptKeyLen(signType string) int {
	if signType == douyinSignSM2 {
		return 16 // SM4
	}
	return 32 // AES-256
}

func douyinAppIDFieldLabel(payType string) string {
	switch payType {
	case douyinPayTypeH5, douyinPayTypeJSAPI:
		return "网站应用 AppID（webAppId）"
	case douyinPayTypeMini:
		return "小程序 AppID（miniAppId）"
	default:
		return "移动应用 AppID（appId）"
	}
}

// RoutedPayTypes 用户端按付款方式逐项列出：每种方式的拉起方式完全不同，客户端必须知道自己拿到的是哪种。
func (p *douyinPayProvider) RoutedPayTypes(config map[string]any) ([]paymentdomain.PayTypeOption, string) {
	if len(configStringList(config, "supportedTypes")) > 0 {
		return routedPayTypes(douyinPayTypes, config, false)
	}
	types := make([]paymentdomain.PayTypeOption, 0, len(douyinDefaultPayTypes))
	for _, value := range douyinDefaultPayTypes {
		if option, ok := findPayType(douyinPayTypes, value); ok {
			types = append(types, option)
		}
	}
	return types, types[0].Value
}

// douyinIsPartner 服务商模式：填了特约商户号
func douyinIsPartner(cfg *paymentdomain.DouyinPayConfig) bool { return cfg.SubMchID != "" }

// ── 密钥 ──

type douyinKeys struct {
	signType string
	rsa      *rsa.PrivateKey
	sm2      *sm2.PrivateKey
}

func loadDouyinKeys(cfg *paymentdomain.DouyinPayConfig) (*douyinKeys, error) {
	keys := &douyinKeys{signType: cfg.SignType}
	var err error
	if cfg.SignType == douyinSignSM2 {
		keys.sm2, err = utils.LoadSm2PrivateKey(strings.TrimSpace(cfg.PrivateKey))
	} else {
		keys.rsa, err = utils.LoadPrivateKey(strings.TrimSpace(cfg.PrivateKey))
	}
	if err != nil {
		return nil, apperrors.New(40078, http.StatusBadRequest, "抖音支付商户私钥无法解析（证书类型 "+cfg.SignType+"）："+err.Error())
	}
	return keys, nil
}

func (k *douyinKeys) signer(cfg *paymentdomain.DouyinPayConfig) auth.Signer {
	if k.signType == douyinSignSM2 {
		return &signers.Sm2Signer{MchID: cfg.MchID, CertificateSerialNo: cfg.SerialNo, PrivateKey: k.sm2}
	}
	return &signers.SHA256WithRSASigner{MchID: cfg.MchID, CertificateSerialNo: cfg.SerialNo, PrivateKey: k.rsa}
}

// sign 调起支付参数的签名（App / JSAPI / 小程序）：与请求签名同一把私钥、同一种算法
func (k *douyinKeys) sign(message string) (string, error) {
	if k.signType == douyinSignSM2 {
		return utils.SignSm2(message, k.sm2)
	}
	return utils.SignSHA256WithRSA(message, k.rsa)
}

// invokeSignType 调起参数里的 signType：RSA 为 DouyinPay-RSA，国密为 DouyinPay-SM2
func (k *douyinKeys) invokeSignType() string { return "DouyinPay-" + k.signType }

func douyinVerifier(signType string, getter client.CertificateGetter) auth.Verifier {
	if signType == douyinSignSM2 {
		return &verifiers.Sm2VerifierWithGetter{CertGetter: getter}
	}
	return &verifiers.SHA256WithRSAVerifierWithGetter{CertGetter: getter}
}

func parseDouyinCertificate(signType, pem string) (*x509.Certificate, error) {
	pem = strings.TrimSpace(pem)
	if signType == douyinSignSM2 {
		return utils.LoadSm2Certificate(pem)
	}
	return utils.LoadCertificate(pem)
}

// ── 平台证书 ──

// douyinCertSource 一套商户凭据对应的平台证书来源，实现 SDK 的 CertificateGetter。
//
// 自动模式下按需下载并每 12 小时刷新；遇到不认识的证书序列号（平台换证书）立即补下一次。
// 按凭据指纹缓存而不是按商户号：管理员换了私钥或加密密钥，旧的下载器就该作废，
// SDK 自带的单例按商户号注册、注册过就不再更新，换了凭据也一直用旧的。
type douyinCertSource struct {
	mch        string                     // 商户号：同一商户号换了凭据时据此淘汰旧来源
	static     *downloader.CertificateMap // 固定平台证书（配置里填了 platformCertificate）
	downloader *downloader.CertificateDownloader
	newDL      func(ctx context.Context) (*downloader.CertificateDownloader, error)

	mu          sync.Mutex
	refreshedAt time.Time
	missAt      time.Time
}

func (s *douyinCertSource) visitor() client.CertificateGetter {
	if s.static != nil {
		return s.static
	}
	return s.downloader
}

func (s *douyinCertSource) Get(ctx context.Context, serial string) (*x509.Certificate, bool) {
	serial = strings.ToUpper(strings.TrimSpace(serial))
	if cert, ok := s.visitor().Get(ctx, serial); ok {
		return cert, true
	}
	if s.static != nil {
		return nil, false
	}
	// 不认识的序列号：平台可能刚换了证书，补下一次（有节流）
	s.mu.Lock()
	if time.Since(s.missAt) < douyinCertMissRefreshGap {
		s.mu.Unlock()
		return nil, false
	}
	s.missAt = time.Now()
	s.mu.Unlock()
	if err := s.downloader.DownloadCertificates(ctx); err != nil {
		return nil, false
	}
	s.mu.Lock()
	s.refreshedAt = time.Now()
	s.mu.Unlock()
	return s.downloader.Get(ctx, serial)
}

func (s *douyinCertSource) GetAll(ctx context.Context) map[string]*x509.Certificate {
	return s.visitor().GetAll(ctx)
}

func (s *douyinCertSource) GetNewestSerial(ctx context.Context) string {
	return s.visitor().GetNewestSerial(ctx)
}

// refreshIfStale 到期的自动证书重新下载一次；失败时继续用手里的旧证书（还没过期就照样能验）
func (s *douyinCertSource) refreshIfStale(ctx context.Context) {
	if s.static != nil {
		return
	}
	s.mu.Lock()
	stale := time.Since(s.refreshedAt) > douyinCertRefreshInterval
	if stale {
		s.refreshedAt = time.Now() // 先占住，避免并发请求一起去下载
	}
	s.mu.Unlock()
	if stale {
		_ = s.downloader.DownloadCertificates(ctx)
	}
}

// certFingerprint 凭据指纹：任一项变了都要换一套平台证书来源
func douyinCertFingerprint(cfg *paymentdomain.DouyinPayConfig) string {
	h := sha256.New()
	for _, part := range []string{cfg.SignType, cfg.MchID, cfg.SerialNo, strings.TrimSpace(cfg.PrivateKey), cfg.EncryptKey, strings.TrimSpace(cfg.PlatformCertificate)} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// certSource 取（必要时创建）这套凭据的平台证书来源。自动模式下首次创建会真实下载一次证书。
func (p *douyinPayProvider) certSource(ctx context.Context, cfg *paymentdomain.DouyinPayConfig, keys *douyinKeys) (*douyinCertSource, error) {
	key := douyinCertFingerprint(cfg)
	p.certMu.Lock()
	src, ok := p.certs[key]
	p.certMu.Unlock()
	if ok {
		src.refreshIfStale(ctx)
		return src, nil
	}

	src = &douyinCertSource{}
	if pem := strings.TrimSpace(cfg.PlatformCertificate); pem != "" {
		cert, err := parseDouyinCertificate(cfg.SignType, pem)
		if err != nil {
			return nil, apperrors.New(40078, http.StatusBadRequest, "抖音支付平台证书无法解析："+err.Error())
		}
		src.static = downloader.NewCertificateMapWithList([]*x509.Certificate{cert})
	} else {
		dl, err := p.downloadCertificates(ctx, cfg, keys)
		if err != nil {
			return nil, err
		}
		src.downloader = dl
		src.refreshedAt = time.Now()
	}

	p.certMu.Lock()
	defer p.certMu.Unlock()
	if existing, ok := p.certs[key]; ok {
		return existing, nil
	}
	// 同一商户号换了凭据：旧来源作废（它的证书仍可能有效，但已没有理由再用）
	for k, old := range p.certs {
		if old.mch == cfg.MchID {
			delete(p.certs, k)
		}
	}
	src.mch = cfg.MchID
	p.certs[key] = src
	return src, nil
}

// downloadCertificates 调平台证书下载接口（应答本身不验签：证书正文用接口加密密钥 AEAD 解密，
// 密钥只有商户和平台知道，解得开即证明来源，这也是官方 SDK 的做法）
func (p *douyinPayProvider) downloadCertificates(ctx context.Context, cfg *paymentdomain.DouyinPayConfig, keys *douyinKeys) (*downloader.CertificateDownloader, error) {
	dlClient, err := client.NewClientWithDialSettings(ctx, &client.DialSettings{
		Signer:     keys.signer(cfg),
		Verifier:   &verifiers.NilVerifier{},
		HTTPClient: p.httpClient,
	})
	if err != nil {
		return nil, apperrors.New(50083, http.StatusBadGateway, "抖音支付客户端初始化失败："+err.Error())
	}
	dl, err := downloader.NewCertificateDownloaderWithClient(ctx, dlClient, cfg.SignType, cfg.EncryptKey)
	if err != nil {
		return nil, apperrors.New(50083, http.StatusBadGateway, "抖音支付平台证书下载失败："+douyinErrorText(err))
	}
	return dl, nil
}

// ── 请求 ──

// douyinSession 一次业务调用所需的全部上下文
type douyinSession struct {
	cfg  *paymentdomain.DouyinPayConfig
	keys *douyinKeys
	api  *client.Client
	src  *douyinCertSource
}

func (p *douyinPayProvider) open(ctx context.Context, data map[string]any) (*douyinSession, error) {
	cfg, err := p.decodeConfig(data)
	if err != nil {
		return nil, err
	}
	keys, err := loadDouyinKeys(cfg)
	if err != nil {
		return nil, err
	}
	src, err := p.certSource(ctx, cfg, keys)
	if err != nil {
		return nil, err
	}
	api, err := client.NewClientWithDialSettings(ctx, &client.DialSettings{
		Signer:     keys.signer(cfg),
		Verifier:   douyinVerifier(cfg.SignType, src),
		HTTPClient: p.httpClient,
	})
	if err != nil {
		return nil, apperrors.New(50083, http.StatusBadGateway, "抖音支付客户端初始化失败："+err.Error())
	}
	return &douyinSession{cfg: cfg, keys: keys, api: api, src: src}, nil
}

// call 发一个已签名请求并（在 out 不为空时）解析应答。应答签名由 SDK 客户端校验。
func (s *douyinSession) call(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	entity := &client.RequestEntity{
		Method:      method,
		RequestPath: douyinPayServer + path,
		QueryParams: query,
		ContentType: consts.ApplicationJSON,
		Header:      http.Header{"Accept": []string{consts.ApplicationJSON}},
	}
	if body != nil {
		entity.PostBody = body
	}
	result, err := s.api.Request(ctx, entity)
	if err != nil {
		return err
	}
	if out == nil {
		if result != nil && result.Response != nil {
			_ = result.Response.Body.Close()
		}
		return nil
	}
	return client.UnMarshalResponse(result.Response, out)
}

// douyinErrorText 上游错误的可读形式：业务错误给出 错误码 + 说明，别的原样
func douyinErrorText(err error) string {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		msg := strings.TrimSpace(apiErr.Message)
		if apiErr.Code != "" {
			if msg == "" {
				return fmt.Sprintf("%s（HTTP %d）", apiErr.Code, apiErr.StatusCode)
			}
			return fmt.Sprintf("%s：%s", apiErr.Code, msg)
		}
		return fmt.Sprintf("HTTP %d %s", apiErr.StatusCode, strings.TrimSpace(apiErr.Body))
	}
	return err.Error()
}

func douyinErrorCode(err error) (int, string) {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode, apiErr.Code
	}
	return 0, ""
}

// ── 连通性 ──

func (p *douyinPayProvider) TestConnection(ctx context.Context, data map[string]any) (map[string]any, error) {
	if err := p.ValidateConfig(data); err != nil {
		return map[string]any{"config_valid": false, "error": err.Error()}, nil
	}
	cfg, _ := p.decodeConfig(data)
	keys, _ := loadDouyinKeys(cfg)
	// 总是真实下载一次：它是唯一不需要订单就能证明「商户号 + 私钥 + 序列号 + 加密密钥」都对的接口
	dl, err := p.downloadCertificates(ctx, cfg, keys)
	if err != nil {
		return map[string]any{"config_valid": true, "api_accessible": false, "error": err.Error()}, nil
	}
	serials := make([]string, 0)
	for serial := range dl.GetAll(ctx) {
		serials = append(serials, serial)
	}
	result := map[string]any{
		"config_valid":   true,
		"api_accessible": true,
		"message":        fmt.Sprintf("平台证书下载成功（%d 张）", len(serials)),
		"certificates":   serials,
	}
	if pem := strings.TrimSpace(cfg.PlatformCertificate); pem != "" {
		if cert, perr := parseDouyinCertificate(cfg.SignType, pem); perr == nil {
			pinned := utils.ConvertSerailNo(cert.SerialNumber)
			if _, ok := dl.Get(ctx, pinned); !ok {
				result["warning"] = "配置里固定的平台证书（" + pinned + "）已不在平台证书列表中，验签会失败，请更新或清空"
			}
		}
	}
	return result, nil
}

// ── 下单 ──

// appIDFor 付款方式对应的 AppID（服务商模式下是服务商的 sp_appid）
func douyinAppIDFor(cfg *paymentdomain.DouyinPayConfig, payType string) string {
	switch payType {
	case douyinPayTypeH5, douyinPayTypeJSAPI:
		return strings.TrimSpace(cfg.WebAppID)
	case douyinPayTypeMini:
		return strings.TrimSpace(cfg.MiniAppID)
	case douyinPayTypeNative:
		// Native 文档两版分别写了移动应用与网站应用，两种都接受，移动应用优先
		return pickString(strings.TrimSpace(cfg.AppID), strings.TrimSpace(cfg.WebAppID))
	default:
		return strings.TrimSpace(cfg.AppID)
	}
}

// subAppIDFor 服务商模式下特约商户的 AppID（可选）
func douyinSubAppIDFor(cfg *paymentdomain.DouyinPayConfig, payType string) string {
	switch payType {
	case douyinPayTypeH5, douyinPayTypeJSAPI:
		return strings.TrimSpace(cfg.SubWebAppID)
	case douyinPayTypeMini:
		return strings.TrimSpace(cfg.SubMiniAppID)
	case douyinPayTypeNative:
		return pickString(strings.TrimSpace(cfg.SubAppID), strings.TrimSpace(cfg.SubWebAppID))
	default:
		return strings.TrimSpace(cfg.SubAppID)
	}
}

// douyinPrepayEndpoint 下单接口的最后一段；小程序与 JSAPI 共用 jsapi 下单
func douyinPrepayEndpoint(payType string) string {
	if payType == douyinPayTypeMini {
		return douyinPayTypeJSAPI
	}
	return payType
}

func douyinTransactionsBase(cfg *paymentdomain.DouyinPayConfig) string {
	if douyinIsPartner(cfg) {
		return "/v1/trade/partner/transactions"
	}
	return "/v1/trade/transactions"
}

// douyinNotifyURL 通知地址：https、不带查询参数，应用 ID 以路径段追加
func douyinNotifyURL(requested, configured string, appID int64) (string, error) {
	base := strings.TrimRight(pickString(strings.TrimSpace(requested), strings.TrimSpace(configured)), "/")
	if base == "" {
		return "", apperrors.New(40078, http.StatusBadRequest, "抖音支付需要配置 notifyUrl（公网 https 回调地址）")
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" {
		return "", apperrors.New(40078, http.StatusBadRequest, "抖音支付通知地址无效："+base)
	}
	if !strings.EqualFold(parsed.Scheme, "https") {
		return "", apperrors.New(40078, http.StatusBadRequest, "抖音支付通知地址必须是 https："+base)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", apperrors.New(40078, http.StatusBadRequest, "抖音支付通知地址不能带查询参数："+base)
	}
	if appID > 0 {
		suffix := "/" + strconv.FormatInt(appID, 10)
		if !strings.HasSuffix(base, suffix) {
			base += suffix
		}
	}
	return base, nil
}

// douyinH5Type h5_info.type：客户端在 metadata 里说明了平台就用它，否则用配置的兜底值
func douyinH5Type(cfg *paymentdomain.DouyinPayConfig, metadata map[string]any) string {
	for _, key := range []string{"h5_type", "h5Type", "platform", "client_platform", "clientPlatform", "os"} {
		raw := strings.ToLower(strings.TrimSpace(metaString(metadata, key)))
		switch {
		case raw == "":
			continue
		case strings.Contains(raw, "harmony") || raw == "ohos":
			return "HarmonyOS"
		case strings.Contains(raw, "android"):
			return "Android"
		case raw == "ios" || strings.Contains(raw, "iphone") || strings.Contains(raw, "ipad"):
			return "iOS"
		case raw == "wap" || raw == "web" || raw == "h5" || raw == "pc" || raw == "desktop" || raw == "windows" || raw == "macos":
			return "Wap"
		}
	}
	switch strings.ToLower(strings.TrimSpace(cfg.H5Type)) {
	case "android":
		return "Android"
	case "ios":
		return "iOS"
	case "harmonyos":
		return "HarmonyOS"
	default:
		return "Wap"
	}
}

// douyinPayerOpenID JSAPI / 小程序的付款人 openid，由客户端放在 metadata 里
func douyinPayerOpenID(metadata map[string]any) (openID string, subOpenID string) {
	subOpenID = pickString(metaString(metadata, "sub_openid"), metaString(metadata, "subOpenid"))
	for _, key := range []string{"openid", "openId", "douyin_openid", "douyinOpenid", "sp_openid"} {
		if v := strings.TrimSpace(metaString(metadata, key)); v != "" {
			return v, strings.TrimSpace(subOpenID)
		}
	}
	return "", strings.TrimSpace(subOpenID)
}

// douyinTimeExpire RFC 3339、东八区（文档示例都是 +08:00，平台按它解释）
func douyinTimeExpire(t time.Time) string {
	return t.In(time.FixedZone("CST", 8*3600)).Format(time.RFC3339)
}

type douyinPrepayResponse struct {
	PrepayID string `json:"prepay_id"`
	H5URL    string `json:"h5_url"`
	CodeURL  string `json:"code_url"`
}

func (p *douyinPayProvider) CreateOrder(ctx context.Context, data map[string]any, req PaymentOrderRequest) (*paymentdomain.PaymentPayload, error) {
	payType := canonicalPayType(req.ProviderType)
	if payType == "" {
		_, payType = p.RoutedPayTypes(data)
	}
	if _, ok := findPayType(douyinPayTypes, payType); !ok {
		return nil, apperrors.New(errCodePayTypeUnavailable, http.StatusBadRequest, "抖音支付不支持付款方式："+payType)
	}
	sess, err := p.open(ctx, data)
	if err != nil {
		return nil, err
	}
	cfg := sess.cfg
	appID := douyinAppIDFor(cfg, payType)
	if appID == "" {
		return nil, apperrors.New(40078, http.StatusBadRequest, "抖音支付配置不完整：缺少"+douyinAppIDFieldLabel(payType))
	}
	notifyURL, err := douyinNotifyURL(req.NotifyURL, cfg.NotifyURL, req.AppID)
	if err != nil {
		return nil, err
	}

	body := map[string]any{
		"description":  pickString(truncateRunes(strings.TrimSpace(req.Subject), 127), "订单 "+req.OrderNo),
		"out_trade_no": req.OrderNo,
		"notify_url":   notifyURL,
		"amount":       map[string]any{"total": wechatFen(req.Amount), "currency": "CNY"},
	}
	if req.ExpireAt != nil {
		body["time_expire"] = douyinTimeExpire(*req.ExpireAt)
	}
	subAppID := ""
	if douyinIsPartner(cfg) {
		body["sp_mchid"] = cfg.MchID
		body["sp_appid"] = appID
		body["sub_mchid"] = cfg.SubMchID
		if subAppID = douyinSubAppIDFor(cfg, payType); subAppID != "" {
			body["sub_appid"] = subAppID
		}
	} else {
		body["mchid"] = cfg.MchID
		body["appid"] = appID
	}

	scene := map[string]any{}
	if ip := strings.TrimSpace(req.ClientIP); ip != "" {
		scene["payer_client_ip"] = ip
	}
	switch payType {
	case douyinPayTypeH5:
		if _, ok := scene["payer_client_ip"]; !ok {
			return nil, apperrors.New(40078, http.StatusBadRequest, "抖音 H5 支付需要付款人 IP")
		}
		h5 := map[string]any{"type": douyinH5Type(cfg, req.Metadata)}
		for key, value := range map[string]string{
			"app_name": cfg.H5AppName, "app_url": cfg.H5AppURL, "bundle_id": cfg.H5BundleID, "package_name": cfg.H5PackageName,
		} {
			if v := strings.TrimSpace(value); v != "" {
				h5[key] = v
			}
		}
		scene["h5_info"] = h5
	case douyinPayTypeJSAPI, douyinPayTypeMini:
		openID, subOpenID := douyinPayerOpenID(req.Metadata)
		payer := map[string]any{}
		if douyinIsPartner(cfg) {
			if subOpenID != "" && subAppID != "" {
				payer["sub_openid"] = subOpenID
			} else if openID != "" && subAppID != "" && subOpenID == "" {
				// 只给了一个 openid、又配了特约商户 AppID：按子应用下的身份处理
				payer["sub_openid"] = openID
			} else if openID != "" {
				payer["sp_openid"] = openID
			}
		} else if openID != "" {
			payer["openid"] = openID
		}
		if len(payer) == 0 {
			return nil, apperrors.New(40078, http.StatusBadRequest, "抖音 JSAPI / 小程序支付需要付款人 openid（下单 metadata.openid）")
		}
		body["payer"] = payer
	}
	if len(scene) > 0 {
		body["scene_info"] = scene
	}

	var resp douyinPrepayResponse
	path := douyinTransactionsBase(cfg) + "/" + douyinPrepayEndpoint(payType)
	if err := sess.call(ctx, http.MethodPost, path, nil, body, &resp); err != nil {
		return nil, apperrors.New(50083, http.StatusBadGateway, "抖音支付下单失败："+douyinErrorText(err))
	}

	payload := &paymentdomain.PaymentPayload{
		Success:      true,
		OrderNo:      req.OrderNo,
		ProviderType: payType,
	}
	switch payType {
	case douyinPayTypeH5:
		if resp.H5URL == "" {
			return nil, apperrors.New(50083, http.StatusBadGateway, "抖音支付下单没有返回 h5_url")
		}
		link := douyinH5Link(resp.H5URL, req.ReturnURL, pickString(metaString(req.Metadata, "return_schema"), cfg.H5ReturnSchema))
		payload.PaymentURL = link
		payload.RedirectURL = link
		payload.Message = "打开链接拉起抖音完成付款（链接 5 分钟内有效）"
		payload.FormData = map[string]any{"h5Url": link}
		if referer := strings.TrimSpace(cfg.H5Referer); referer != "" {
			payload.FormData["referer"] = referer
		}
	case douyinPayTypeNative:
		if resp.CodeURL == "" {
			return nil, apperrors.New(50083, http.StatusBadGateway, "抖音支付下单没有返回 code_url")
		}
		payload.PaymentURL = resp.CodeURL
		payload.Message = "请使用抖音扫描二维码完成支付"
		payload.FormData = map[string]any{"codeUrl": resp.CodeURL}
	case douyinPayTypeApp:
		params, err := douyinAppInvokeParams(sess, pickString(subAppID, appID), resp.PrepayID)
		if err != nil {
			return nil, err
		}
		payload.Message = "用抖音支付 OpenSDK 调起收银台（DyPay.pay / DypayAPI openDypayWithInfo）"
		payload.FormData = map[string]any{"prepayId": resp.PrepayID, "sdkParams": params}
	case douyinPayTypeJSAPI, douyinPayTypeMini:
		params, err := douyinJSInvokeParams(sess, pickString(subAppID, appID), resp.PrepayID)
		if err != nil {
			return nil, err
		}
		if payType == douyinPayTypeMini {
			payload.Message = "在小程序内调用 tt.payOffline({ businessInfo }) 调起收银台"
			payload.FormData = map[string]any{"prepayId": resp.PrepayID, "businessInfo": params}
		} else {
			payload.Message = "在抖音内网页用 DouyinOpenJSBridge 调用 ttcjpay.dypay（sdk_info 为 JSON 字符串）"
			sdkInfo, _ := json.Marshal(params)
			payload.FormData = map[string]any{"prepayId": resp.PrepayID, "sdkParams": params, "sdkInfo": string(sdkInfo)}
		}
	}
	return payload, nil
}

// douyinH5Link 在 h5_url 后追加付款完成后的回跳页面与回跳 App（参数值整体 encodeURIComponent）
func douyinH5Link(h5URL, returnURL, returnSchema string) string {
	link := h5URL
	appendParam := func(key, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		sep := "&"
		if !strings.Contains(link, "?") {
			sep = "?"
		}
		link += sep + key + "=" + url.QueryEscape(value)
	}
	appendParam("return_schema", returnSchema)
	appendParam("return_url", returnURL)
	return link
}

func douyinNonce() (string, error) { return utils.GenerateNonce() }

// douyinAppInvokeParams App 调起支付参数：签名串为 appid\ntimestamp\nnoncestr\nprepayid\n
func douyinAppInvokeParams(sess *douyinSession, appID, prepayID string) (map[string]string, error) {
	if prepayID == "" {
		return nil, apperrors.New(50083, http.StatusBadGateway, "抖音支付下单没有返回 prepay_id")
	}
	nonce, err := douyinNonce()
	if err != nil {
		return nil, err
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	sign, err := sess.keys.sign(appID + "\n" + timestamp + "\n" + nonce + "\n" + prepayID + "\n")
	if err != nil {
		return nil, apperrors.New(50083, http.StatusBadGateway, "抖音支付调起参数签名失败："+err.Error())
	}
	partnerID := sess.cfg.MchID
	if douyinIsPartner(sess.cfg) {
		partnerID = sess.cfg.SubMchID
	}
	return map[string]string{
		"appid":     appID,
		"partnerid": partnerID,
		"prepayid":  prepayID,
		"package":   "Sign=DYPay",
		"noncestr":  nonce,
		"timestamp": timestamp,
		"sign":      sign,
	}, nil
}

// douyinJSInvokeParams JSAPI / 小程序调起参数：签名串为 appId\ntimeStamp\nnonceStr\npackage\n
func douyinJSInvokeParams(sess *douyinSession, appID, prepayID string) (map[string]string, error) {
	if prepayID == "" {
		return nil, apperrors.New(50083, http.StatusBadGateway, "抖音支付下单没有返回 prepay_id")
	}
	nonce, err := douyinNonce()
	if err != nil {
		return nil, err
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	pkg := "prepay_id=" + prepayID
	sign, err := sess.keys.sign(appID + "\n" + timestamp + "\n" + nonce + "\n" + pkg + "\n")
	if err != nil {
		return nil, apperrors.New(50083, http.StatusBadGateway, "抖音支付调起参数签名失败："+err.Error())
	}
	return map[string]string{
		"appId":     appID,
		"timeStamp": timestamp,
		"nonceStr":  nonce,
		"package":   pkg,
		"signType":  sess.keys.invokeSignType(),
		"paySign":   sign,
	}, nil
}

// ── 查单 / 关单 ──

// douyinInt 金额字段：文档示例里同一字段时而是数字时而是字符串，两种都认
type douyinInt int64

func (v *douyinInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		*v = 0
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		f, ferr := strconv.ParseFloat(s, 64)
		if ferr != nil {
			return err
		}
		n = int64(math.Round(f))
	}
	*v = douyinInt(n)
	return nil
}

type douyinTransaction struct {
	AppID          string `json:"appid"`
	MchID          string `json:"mchid"`
	SpAppID        string `json:"sp_appid"`
	SpMchID        string `json:"sp_mchid"`
	SubAppID       string `json:"sub_appid"`
	SubMchID       string `json:"sub_mchid"`
	OutTradeNo     string `json:"out_trade_no"`
	TransactionID  string `json:"transaction_id"`
	TradeType      string `json:"trade_type"`
	TradeState     string `json:"trade_state"`
	TradeStateDesc string `json:"trade_state_desc"`
	BankType       string `json:"bank_type"`
	SuccessTime    string `json:"success_time"`
	Amount         struct {
		Total      douyinInt `json:"total"`
		PayerTotal douyinInt `json:"payer_total"`
		Currency   string    `json:"currency"`
	} `json:"amount"`
}

// merchantMatches 交易属于这份配置的商户（服务商模式比对服务商号与子商户号）
func (t *douyinTransaction) merchantMatches(cfg *paymentdomain.DouyinPayConfig) bool {
	if douyinIsPartner(cfg) {
		return (t.SpMchID == "" || t.SpMchID == cfg.MchID) && (t.SubMchID == "" || t.SubMchID == cfg.SubMchID)
	}
	return t.MchID == "" || t.MchID == cfg.MchID
}

func (t *douyinTransaction) callbackResult(source string) *paymentdomain.CallbackResult {
	return &paymentdomain.CallbackResult{
		Success:         true,
		Paid:            t.TradeState == "SUCCESS",
		OrderNo:         t.OutTradeNo,
		ProviderOrderNo: t.TransactionID,
		TradeStatus:     t.TradeState,
		PaymentMethod:   paymentdomain.MethodDouyinPay,
		// 订单金额（含优惠），与本地订单比对；用户实付记在 RawData
		Amount: decimal.NewFromInt(int64(t.Amount.Total)).Div(decimal.NewFromInt(100)),
		RawData: map[string]any{
			"source":         source,
			"trade_state":    t.TradeState,
			"trade_type":     t.TradeType,
			"transaction_id": t.TransactionID,
			"out_trade_no":   t.OutTradeNo,
			"total_fen":      int64(t.Amount.Total),
			"payer_total":    int64(t.Amount.PayerTotal),
			"success_time":   t.SuccessTime,
			"bank_type":      t.BankType,
		},
	}
}

func (s *douyinSession) merchantQuery() url.Values {
	q := url.Values{}
	if douyinIsPartner(s.cfg) {
		q.Set("sp_mchid", s.cfg.MchID)
		q.Set("sub_mchid", s.cfg.SubMchID)
	} else {
		q.Set("mchid", s.cfg.MchID)
	}
	return q
}

func (s *douyinSession) queryTransaction(ctx context.Context, orderNo string) (*douyinTransaction, error) {
	path := douyinTransactionsBase(s.cfg) + "/out-trade-no/" + url.PathEscape(orderNo)
	var txn douyinTransaction
	if err := s.call(ctx, http.MethodGet, path, s.merchantQuery(), nil, &txn); err != nil {
		return nil, err
	}
	return &txn, nil
}

func (p *douyinPayProvider) QueryRemoteOrder(ctx context.Context, data map[string]any, orderNo string) (map[string]any, error) {
	sess, err := p.open(ctx, data)
	if err != nil {
		return nil, err
	}
	txn, err := sess.queryTransaction(ctx, orderNo)
	if err != nil {
		if status, code := douyinErrorCode(err); status == http.StatusNotFound || code == "ORDER_NOT_EXIST" {
			return map[string]any{"exists": false, "paid": false, "message": "抖音支付没有这笔订单（用户可能还没打开收银台）"}, nil
		}
		return nil, apperrors.New(50083, http.StatusBadGateway, "抖音支付查询失败："+douyinErrorText(err))
	}
	result := txn.callbackResult("query").RawData
	result["exists"] = true
	result["paid"] = txn.TradeState == "SUCCESS"
	result["trade_state_desc"] = txn.TradeStateDesc
	result["amount"] = decimal.NewFromInt(int64(txn.Amount.Total)).Div(decimal.NewFromInt(100)).StringFixed(2)
	return result, nil
}

// ── 主动补单 ──

// douyinSettleMark 后台补单对一笔订单的退避记录。
// 通知一般都会到，补单只是兜底：刚下单的订单每轮都查，放久了的逐渐查得稀，
// 否则 24 小时窗口里一笔弃单要被查 720 次。用户自己查单（on-demand）不受退避限制。
type douyinSettleMark struct {
	firstSeen   time.Time
	lastChecked time.Time
}

func douyinSettleInterval(age time.Duration) time.Duration {
	switch {
	case age < 10*time.Minute:
		return 0
	case age < time.Hour:
		return 10 * time.Minute
	default:
		return 30 * time.Minute
	}
}

func (p *douyinPayProvider) settleDue(orderNo string, now time.Time, onDemand bool) bool {
	p.settleMu.Lock()
	defer p.settleMu.Unlock()
	mark, ok := p.settleSeen[orderNo]
	if !ok {
		mark.firstSeen = now
	}
	due := onDemand || !ok || now.Sub(mark.lastChecked) >= douyinSettleInterval(now.Sub(mark.firstSeen))
	if due {
		mark.lastChecked = now
	}
	p.settleSeen[orderNo] = mark
	// 顺手清掉超出补单窗口的记录
	for no, m := range p.settleSeen {
		if now.Sub(m.firstSeen) > remoteSettleWindow+time.Hour {
			delete(p.settleSeen, no)
		}
	}
	return due
}

func (p *douyinPayProvider) SettleRemoteOrders(ctx context.Context, data map[string]any, orderNos []string) (map[string]*paymentdomain.CallbackResult, error) {
	results := map[string]*paymentdomain.CallbackResult{}
	onDemand := isOnDemandSettle(ctx)
	now := time.Now()
	due := make([]string, 0, len(orderNos))
	for _, no := range orderNos {
		if len(due) >= douyinSettleBatchCap {
			break
		}
		if p.settleDue(no, now, onDemand) {
			due = append(due, no)
		}
	}
	if len(due) == 0 {
		return results, nil
	}
	sess, err := p.open(ctx, data)
	if err != nil {
		return results, err
	}
	var firstErr error
	for _, no := range due {
		txn, err := sess.queryTransaction(ctx, no)
		if err != nil {
			status, code := douyinErrorCode(err)
			if status == http.StatusNotFound || code == "ORDER_NOT_EXIST" {
				continue
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("query %s: %s", no, douyinErrorText(err))
			}
			// 限频或上游故障：这一轮到此为止
			if status == http.StatusTooManyRequests || status >= 500 || status == 0 {
				break
			}
			continue
		}
		if txn.OutTradeNo != "" && txn.OutTradeNo != no {
			continue
		}
		if !txn.merchantMatches(sess.cfg) {
			continue
		}
		if txn.TradeState == "SUCCESS" {
			results[no] = txn.callbackResult("query")
		}
	}
	return results, firstErr
}

// SettleScope 指向同一商户（服务商下的同一特约商户）的配置合并成一轮查单
func (p *douyinPayProvider) SettleScope(data map[string]any) string {
	mch := strings.TrimSpace(configString(data, "mchId"))
	if mch == "" {
		return ""
	}
	if sub := strings.TrimSpace(configString(data, "subMchId")); sub != "" {
		return mch + "/" + sub
	}
	return mch
}

// ── 退款 ──

type douyinRefund struct {
	RefundID      string `json:"refund_id"`
	OutRefundNo   string `json:"out_refund_no"`
	TransactionID string `json:"transaction_id"`
	OutTradeNo    string `json:"out_trade_no"`
	Channel       string `json:"channel"`
	Status        string `json:"status"`
	SuccessTime   string `json:"success_time"`
	CreateTime    string `json:"create_time"`
	ReceivedTo    string `json:"user_received_account"`
	Amount        struct {
		Refund      douyinInt `json:"refund"`
		Total       douyinInt `json:"total"`
		PayerRefund douyinInt `json:"payer_refund"`
	} `json:"amount"`
}

// douyinRefundStatus 抖音退款状态 → 本地退款单状态。
// ABNORMAL（退到银行卡时卡已作废或冻结，需在商家平台处理）记为处理中而不是失败：
// 钱可能已经在途，判失败会把额度错放给下一笔退款。与微信的同名状态同样处理。
func douyinRefundStatus(state string) string {
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case "SUCCESS":
		return paymentdomain.RefundSuccess
	case "CLOSED":
		return paymentdomain.RefundClosed
	default:
		return paymentdomain.RefundProcessing
	}
}

func (r *douyinRefund) result() *paymentdomain.RefundResult {
	status := douyinRefundStatus(r.Status)
	result := &paymentdomain.RefundResult{
		Status:           status,
		ProviderRefundNo: r.RefundID,
		Amount:           decimal.NewFromInt(int64(r.Amount.Refund)).Div(decimal.NewFromInt(100)),
		Raw: map[string]any{
			"status":                r.Status,
			"refund_id":             r.RefundID,
			"out_refund_no":         r.OutRefundNo,
			"channel":               r.Channel,
			"success_time":          r.SuccessTime,
			"user_received_account": r.ReceivedTo,
			"refund_fen":            int64(r.Amount.Refund),
			"payer_refund_fen":      int64(r.Amount.PayerRefund),
		},
	}
	if status != paymentdomain.RefundSuccess {
		result.Message = "抖音支付退款状态：" + pickString(r.Status, "未知")
	}
	return result
}

func (p *douyinPayProvider) Refund(ctx context.Context, data map[string]any, req paymentdomain.RefundRequest) (*paymentdomain.RefundResult, error) {
	sess, err := p.open(ctx, data)
	if err != nil {
		return nil, err
	}
	// 退款通知地址文档写的是必填，与支付通知共用同一个回调入口
	notifyURL, err := douyinNotifyURL(req.NotifyURL, sess.cfg.NotifyURL, req.AppID)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"out_refund_no": req.RefundNo,
		"notify_url":    notifyURL,
		"amount": map[string]any{
			"refund":   wechatFen(req.RefundAmount),
			"total":    wechatFen(req.TotalAmount),
			"currency": "CNY",
		},
	}
	if douyinIsPartner(sess.cfg) {
		body["sp_mchid"] = sess.cfg.MchID
		body["sub_mchid"] = sess.cfg.SubMchID
	} else {
		body["mchid"] = sess.cfg.MchID
	}
	if txnID := strings.TrimSpace(req.ProviderOrderNo); txnID != "" {
		body["transaction_id"] = txnID
	} else {
		body["out_trade_no"] = req.OrderNo
	}
	if reason := truncateRunes(strings.TrimSpace(req.Reason), 80); reason != "" {
		body["reason"] = reason
	}
	var resp douyinRefund
	if err := sess.call(ctx, http.MethodPost, "/v1/trade/refund/domestic/refunds", nil, body, &resp); err != nil {
		// 只有上游明确拒绝（4xx 业务错误）才算失败、释放额度。超时、5xx、应答验签失败时
		// 请求可能已被受理：判失败会让管理员换个退款单号重退，同一笔钱就退了两次。
		// 退款单号是上游幂等键，记为处理中，交给退款查询收敛（查无此单再判失败）。
		if status, _ := douyinErrorCode(err); status == 0 || status >= 500 {
			return &paymentdomain.RefundResult{
				Status:  paymentdomain.RefundProcessing,
				Message: "抖音支付退款结果未知，稍后自动查询：" + douyinErrorText(err),
				Raw:     map[string]any{"error": douyinErrorText(err)},
			}, nil
		}
		return nil, apperrors.New(50091, http.StatusBadGateway, "抖音支付退款请求失败："+douyinErrorText(err))
	}
	return resp.result(), nil
}

func (p *douyinPayProvider) QueryRefund(ctx context.Context, data map[string]any, query paymentdomain.RefundQuery) (*paymentdomain.RefundResult, error) {
	sess, err := p.open(ctx, data)
	if err != nil {
		return nil, err
	}
	var resp douyinRefund
	path := "/v1/trade/refund/domestic/refunds/" + url.PathEscape(query.RefundNo)
	if err := sess.call(ctx, http.MethodGet, path, sess.merchantQuery(), nil, &resp); err != nil {
		if status, code := douyinErrorCode(err); status == http.StatusNotFound || code == "RESOURCE_NOT_EXISTS" {
			// 退款申请当时就没被受理（比如提交时网络中断）：判失败，释放额度
			return &paymentdomain.RefundResult{Status: paymentdomain.RefundFailed, Message: "抖音支付查无此退款单：" + douyinErrorText(err)}, nil
		}
		return nil, apperrors.New(50091, http.StatusBadGateway, "抖音支付退款查询失败："+douyinErrorText(err))
	}
	return resp.result(), nil
}

// ── 回调 ──

type douyinNotifyEnvelope struct {
	ID           string `json:"id"`
	CreateTime   string `json:"create_time"`
	EventType    string `json:"event_type"`
	ResourceType string `json:"resource_type"`
	Summary      string `json:"summary"`
	Resource     struct {
		OriginalType   string `json:"original_type"`
		Algorithm      string `json:"algorithm"`
		Ciphertext     string `json:"ciphertext"`
		AssociatedData string `json:"associated_data"`
		Nonce          string `json:"nonce"`
	} `json:"resource"`
}

type douyinRefundNotify struct {
	MchID         string `json:"mchid"`
	SpMchID       string `json:"sp_mchid"`
	SubMchID      string `json:"sub_mchid"`
	OutTradeNo    string `json:"out_trade_no"`
	TransactionID string `json:"transaction_id"`
	OutRefundNo   string `json:"out_refund_no"`
	RefundID      string `json:"refund_id"`
	RefundStatus  string `json:"refund_status"`
	SuccessTime   string `json:"success_time"`
	ReceivedTo    string `json:"user_received_account"`
	Amount        struct {
		Total       douyinInt `json:"total"`
		Refund      douyinInt `json:"refund"`
		PayerTotal  douyinInt `json:"payer_total"`
		PayerRefund douyinInt `json:"payer_refund"`
	} `json:"amount"`
}

// douyinDecrypt 解密回调 / 证书密文。算法名文档里写过 AEAD_AES_256_GCM 与 AEAD-AES-256-GCM 两种，
// 都按 AES-256-GCM；国密商户为 SM4。不用 SDK 的 DecryptSM4：输入不合法时它会 panic。
func douyinDecrypt(algorithm, key, nonce, associatedData, ciphertext string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(ciphertext))
	if err != nil {
		return nil, fmt.Errorf("密文不是 base64：%w", err)
	}
	algo := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(algorithm), "_", "-"))
	if strings.Contains(algo, "SM4") {
		return douyinDecryptSM4(key, nonce, raw)
	}
	if algo != "" && algo != consts.EncryptTypeAES256GCM {
		return nil, fmt.Errorf("不支持的加密算法 %s", algorithm)
	}
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, len(nonce))
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, []byte(nonce), raw, []byte(associatedData))
}

func douyinDecryptSM4(key, iv string, raw []byte) ([]byte, error) {
	block, err := sm4.NewCipher([]byte(key))
	if err != nil {
		return nil, err
	}
	if len(iv) != block.BlockSize() {
		return nil, fmt.Errorf("SM4 IV 长度应为 %d，实际 %d", block.BlockSize(), len(iv))
	}
	if len(raw) == 0 || len(raw)%block.BlockSize() != 0 {
		return nil, fmt.Errorf("SM4 密文长度 %d 不是分组长度的整数倍", len(raw))
	}
	out := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, []byte(iv)).CryptBlocks(out, raw)
	pad := int(out[len(out)-1])
	if pad <= 0 || pad > block.BlockSize() || pad > len(out) {
		return nil, fmt.Errorf("SM4 填充无效")
	}
	return out[:len(out)-pad], nil
}

// verifyNotify 验签：时间戳 + 随机串 + 原始报文，平台证书按 Douyinpay-Serial 选
func (s *douyinSession) verifyNotify(ctx context.Context, callbackData map[string]string, rawBody string) error {
	serial := strings.TrimSpace(callbackHeader(callbackData, "Douyinpay-Serial"))
	signature := strings.TrimSpace(callbackHeader(callbackData, "Douyinpay-Signature"))
	nonce := strings.TrimSpace(callbackHeader(callbackData, "Douyinpay-Nonce"))
	tsRaw := strings.TrimSpace(callbackHeader(callbackData, "Douyinpay-Timestamp"))
	if serial == "" || signature == "" || nonce == "" || tsRaw == "" {
		return errors.New("缺少 Douyinpay-Serial / Signature / Nonce / Timestamp 请求头")
	}
	ts, err := strconv.ParseInt(tsRaw, 10, 64)
	if err != nil {
		return errors.New("Douyinpay-Timestamp 不是整数")
	}
	if skew := time.Now().Unix() - ts; skew > douyinCallbackClockSkew || skew < -douyinCallbackClockSkew {
		return fmt.Errorf("通知时间戳偏差 %d 秒，超过 %d 秒", skew, douyinCallbackClockSkew)
	}
	message := fmt.Sprintf(consts.VerifyMessageFormat, ts, nonce, rawBody)
	return douyinVerifier(s.cfg.SignType, s.src).Verify(ctx, strings.ToUpper(serial), message, signature)
}

func (p *douyinPayProvider) HandleCallback(ctx context.Context, data map[string]any, callbackData map[string]string, clientIP string) (*paymentdomain.CallbackResult, error) {
	rawBody := callbackData[CallbackRawBodyKey]
	if strings.TrimSpace(rawBody) == "" {
		return nil, apperrors.New(40075, http.StatusBadRequest, "抖音支付通知缺少原始报文")
	}
	sess, err := p.open(ctx, data)
	if err != nil {
		return nil, err
	}
	if err := sess.verifyNotify(ctx, callbackData, rawBody); err != nil {
		return nil, apperrors.New(40076, http.StatusBadRequest, "抖音支付通知验签失败："+err.Error())
	}
	var env douyinNotifyEnvelope
	if err := json.Unmarshal([]byte(rawBody), &env); err != nil {
		return nil, apperrors.New(40075, http.StatusBadRequest, "抖音支付通知报文无法解析")
	}
	plain, err := douyinDecrypt(env.Resource.Algorithm, sess.cfg.EncryptKey, env.Resource.Nonce, env.Resource.AssociatedData, env.Resource.Ciphertext)
	if err != nil {
		return nil, apperrors.New(40076, http.StatusBadRequest, "抖音支付通知解密失败（检查接口加密密钥）："+err.Error())
	}

	eventType := strings.ToUpper(strings.TrimSpace(env.EventType))
	original := strings.ToLower(strings.TrimSpace(env.Resource.OriginalType))
	switch {
	case strings.HasPrefix(eventType, "REFUND.") || original == "refund":
		var n douyinRefundNotify
		if err := json.Unmarshal(plain, &n); err != nil {
			return nil, apperrors.New(40075, http.StatusBadRequest, "抖音支付退款通知正文无法解析")
		}
		if !douyinMerchantMatches(sess.cfg, n.MchID, n.SpMchID, n.SubMchID) {
			return nil, apperrors.New(40095, http.StatusBadRequest, "抖音支付退款通知的商户号与配置不一致")
		}
		state := pickString(n.RefundStatus, strings.TrimPrefix(eventType, "REFUND."))
		return &paymentdomain.CallbackResult{
			Success:         true,
			OrderNo:         n.OutTradeNo,
			ProviderOrderNo: n.TransactionID,
			TradeStatus:     "REFUND." + strings.ToUpper(state),
			PaymentMethod:   paymentdomain.MethodDouyinPay,
			RawData: map[string]any{
				"event_type": env.EventType, "notify_id": env.ID, "out_trade_no": n.OutTradeNo,
				"out_refund_no": n.OutRefundNo, "refund_id": n.RefundID, "refund_status": n.RefundStatus,
				"refund_fen": int64(n.Amount.Refund), "success_time": n.SuccessTime, "user_received_account": n.ReceivedTo,
			},
			Refund: &paymentdomain.RefundNotice{
				OrderNo:          n.OutTradeNo,
				RefundNo:         n.OutRefundNo,
				ProviderRefundNo: n.RefundID,
				Status:           douyinRefundStatus(state),
				Amount:           decimal.NewFromInt(int64(n.Amount.Refund)).Div(decimal.NewFromInt(100)),
			},
		}, nil
	case strings.HasPrefix(eventType, "TRANSACTION.") || original == "transaction" || eventType == "":
		var txn douyinTransaction
		if err := json.Unmarshal(plain, &txn); err != nil {
			return nil, apperrors.New(40075, http.StatusBadRequest, "抖音支付通知正文无法解析")
		}
		if !txn.merchantMatches(sess.cfg) {
			return nil, apperrors.New(40095, http.StatusBadRequest, "抖音支付通知的商户号与配置不一致")
		}
		result := txn.callbackResult("notify")
		result.RawData["event_type"] = env.EventType
		result.RawData["notify_id"] = env.ID
		return result, nil
	default:
		return nil, apperrors.New(40075, http.StatusBadRequest, "不支持的抖音支付通知类型："+env.EventType)
	}
}

func douyinMerchantMatches(cfg *paymentdomain.DouyinPayConfig, mchID, spMchID, subMchID string) bool {
	if douyinIsPartner(cfg) {
		return (spMchID == "" || spMchID == cfg.MchID) && (subMchID == "" || subMchID == cfg.SubMchID)
	}
	return mchID == "" || mchID == cfg.MchID
}
