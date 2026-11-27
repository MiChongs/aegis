package service

import (
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	stdx509 "crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	paymentdomain "aegis/internal/domain/payment"

	"github.com/shopspring/decimal"
	"github.com/tjfoc/gmsm/sm4"
)

// ── 模拟的抖音支付服务器 ──
//
// 它做真平台会做的三件事：校验请求签名（商户公钥）、给应答签名（平台私钥）、
// 下发用接口加密密钥 AES-256-GCM 加密的平台证书。provider 走的是真实的 SDK 客户端，
// 只是 HTTP 传输被改道到这里。

const (
	dyTestMchID      = "6020230307605084"
	dyTestSerial     = "41A48FEF2A9F04C5"
	dyTestEncryptKey = "0123456789abcdef0123456789abcdef"
	dyTestAppID      = "awmobile0000001"
	dyTestWebAppID   = "awweb000000001"
	dyTestMiniAppID  = "ttmini00000001"
)

type dyFakeServer struct {
	t            *testing.T
	srv          *httptest.Server
	merchantKey  *rsa.PrivateKey
	platformKey  *rsa.PrivateKey
	platformCert string // PEM
	platformSN   string

	mu        sync.Mutex
	requests  []dyRecordedRequest
	handlers  map[string]func(r *http.Request, body map[string]any) (int, any)
	certCalls int
}

type dyRecordedRequest struct {
	Method string
	Path   string
	Query  url.Values
	Body   map[string]any
}

func newDyFakeServer(t *testing.T) *dyFakeServer {
	t.Helper()
	merchantKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	platformKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	serial := new(big.Int)
	serial.SetString("763CC8F6EF3A8802", 16)
	tmpl := &stdx509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Douyinpay Test Platform"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := stdx509.CreateCertificate(rand.Reader, tmpl, tmpl, &platformKey.PublicKey, platformKey)
	if err != nil {
		t.Fatal(err)
	}
	f := &dyFakeServer{
		t:            t,
		merchantKey:  merchantKey,
		platformKey:  platformKey,
		platformCert: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		platformSN:   "763CC8F6EF3A8802",
		handlers:     map[string]func(*http.Request, map[string]any) (int, any){},
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *dyFakeServer) merchantKeyPEM() string {
	der, err := stdx509.MarshalPKCS8PrivateKey(f.merchantKey)
	if err != nil {
		f.t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func (f *dyFakeServer) provider() *douyinPayProvider {
	p := newDouyinPayProvider(nil)
	target, _ := url.Parse(f.srv.URL)
	p.httpClient = &http.Client{Transport: dyRewriteTransport{target: target}, Timeout: 10 * time.Second}
	return p
}

type dyRewriteTransport struct{ target *url.URL }

func (t dyRewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	clone.URL.Scheme = t.target.Scheme
	clone.URL.Host = t.target.Host
	clone.Host = t.target.Host
	return http.DefaultTransport.RoundTrip(clone)
}

func (f *dyFakeServer) config(extra map[string]any) map[string]any {
	cfg := map[string]any{
		"mchId":      dyTestMchID,
		"serialNo":   dyTestSerial,
		"privateKey": f.merchantKeyPEM(),
		"encryptKey": dyTestEncryptKey,
		"appId":      dyTestAppID,
		"webAppId":   dyTestWebAppID,
		"miniAppId":  dyTestMiniAppID,
		"notifyUrl":  "https://api.example.com/api/public/pay/callback/douyinpay",
		"h5AppName":  "测试应用",
		"h5Referer":  "https://www.example.com",
	}
	for k, v := range extra {
		cfg[k] = v
	}
	return cfg
}

var dyAuthPattern = regexp.MustCompile(`(\w+)="([^"]*)"`)

func (f *dyFakeServer) serve(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	// 1. 校验请求签名：DouyinPay-RSA mchid=…,nonce_str=…,timestamp=…,serial_no=…,signature=…
	authz := r.Header.Get("Authorization")
	if !strings.HasPrefix(authz, "DouyinPay-RSA ") {
		f.reply(w, http.StatusUnauthorized, map[string]any{"code": "SIGN_ERROR", "message": "缺少认证信息"})
		return
	}
	fieldsMap := map[string]string{}
	for _, m := range dyAuthPattern.FindAllStringSubmatch(authz, -1) {
		fieldsMap[m[1]] = m[2]
	}
	message := r.Method + "\n" + r.URL.RequestURI() + "\n" + fieldsMap["timestamp"] + "\n" + fieldsMap["nonce_str"] + "\n" + string(raw) + "\n"
	sig, _ := base64.StdEncoding.DecodeString(fieldsMap["signature"])
	digest := sha256.Sum256([]byte(message))
	if fieldsMap["mchid"] != dyTestMchID || fieldsMap["serial_no"] != dyTestSerial ||
		rsa.VerifyPKCS1v15(&f.merchantKey.PublicKey, crypto.SHA256, digest[:], sig) != nil {
		f.reply(w, http.StatusUnauthorized, map[string]any{"code": "SIGN_ERROR", "message": "签名错误"})
		return
	}

	var body map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &body)
	}
	f.mu.Lock()
	f.requests = append(f.requests, dyRecordedRequest{Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(), Body: body})
	f.mu.Unlock()

	if r.URL.Path == "/v1/merchant/certificates/getPlatformCertificates" {
		f.mu.Lock()
		f.certCalls++
		f.mu.Unlock()
		nonce := "6tKL7i5sEaO4"
		f.reply(w, http.StatusOK, map[string]any{"certificates": []map[string]any{{
			"cert_no":        f.platformSN,
			"effective_time": "20230322042245",
			"expire_time":    "20380320042245",
			"cert_type":      "RSA",
			"encrypt_certificate": map[string]any{
				"cipher_text": dyEncrypt(f.t, dyTestEncryptKey, nonce, "", f.platformCert),
				"algorithm":   "AEAD-AES-256-GCM",
				"nonce":       nonce,
			},
		}}})
		return
	}
	key := r.Method + " " + r.URL.Path
	f.mu.Lock()
	h, ok := f.handlers[key]
	if !ok {
		// 以 / 结尾的键按前缀匹配（订单号是运行时生成的）
		for k, candidate := range f.handlers {
			if strings.HasSuffix(k, "/") && strings.HasPrefix(key, k) {
				h, ok = candidate, true
				break
			}
		}
	}
	f.mu.Unlock()
	if !ok {
		f.reply(w, http.StatusNotFound, map[string]any{"code": "RESOURCE_NOT_EXISTS", "message": "未知接口 " + key})
		return
	}
	status, out := h(r, body)
	f.reply(w, status, out)
}

// reply 写应答并用平台私钥签名
func (f *dyFakeServer) reply(w http.ResponseWriter, status int, payload any) {
	body := []byte{}
	if payload != nil {
		body, _ = json.Marshal(payload)
	}
	for k, v := range f.signHeaders(string(body)) {
		w.Header().Set(k, v)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (f *dyFakeServer) signHeaders(body string) map[string]string {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	nonce := "5rZ6K4zP41JEcR7dfbtTdeK5jCaMOsAd"
	digest := sha256.Sum256([]byte(ts + "\n" + nonce + "\n" + body + "\n"))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.platformKey, crypto.SHA256, digest[:])
	if err != nil {
		f.t.Fatal(err)
	}
	return map[string]string{
		"Douyinpay-Timestamp": ts,
		"Douyinpay-Nonce":     nonce,
		"Douyinpay-Signature": base64.StdEncoding.EncodeToString(sig),
		"Douyinpay-Serial":    f.platformSN,
		"Request-Id":          "20221201163028010174243145060A205C",
	}
}

func (f *dyFakeServer) handle(key string, h func(r *http.Request, body map[string]any) (int, any)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[key] = h
}

func (f *dyFakeServer) lastRequest(path string) dyRecordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.requests) - 1; i >= 0; i-- {
		if f.requests[i].Path == path {
			return f.requests[i]
		}
	}
	f.t.Fatalf("没有发往 %s 的请求", path)
	return dyRecordedRequest{}
}

func dyEncrypt(t *testing.T, key, nonce, aad, plain string) string {
	t.Helper()
	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, len(nonce))
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(gcm.Seal(nil, []byte(nonce), []byte(plain), []byte(aad)))
}

// notify 构造一条已签名、正文加密的通知，返回传输层会交给 provider 的 callbackData
func (f *dyFakeServer) notify(eventType, originalType string, resource any) map[string]string {
	plain, _ := json.Marshal(resource)
	nonce := "ippNsxmNlvJm"
	env := map[string]any{
		"id":            "DY-202211302150394071133740",
		"create_time":   time.Now().Format(time.RFC3339),
		"event_type":    eventType,
		"resource_type": "encrypt-resource",
		"summary":       "通知",
		"resource": map[string]any{
			"original_type":   originalType,
			"algorithm":       "AEAD-AES-256-GCM",
			"ciphertext":      dyEncrypt(f.t, dyTestEncryptKey, nonce, "", string(plain)),
			"associated_data": "",
			"nonce":           nonce,
			"mchid":           dyTestMchID,
		},
	}
	raw, _ := json.Marshal(env)
	data := map[string]string{CallbackRawBodyKey: string(raw)}
	for k, v := range f.signHeaders(string(raw)) {
		data[CallbackHeaderPrefix+strings.ToLower(k)] = v
	}
	return data
}

func dyVerifyMerchantSign(t *testing.T, key *rsa.PrivateKey, message, signature string) {
	t.Helper()
	sig, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		t.Fatalf("签名不是 base64：%v", err)
	}
	digest := sha256.Sum256([]byte(message))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
		t.Fatalf("调起参数签名验不过：%v", err)
	}
}

func dyOrderRequest(payType string) PaymentOrderRequest {
	expire := time.Date(2026, 11, 27, 12, 30, 0, 0, time.UTC)
	return PaymentOrderRequest{
		AppID:        10004,
		OrderNo:      "P1000420261127120000123456",
		Subject:      "远航会员 · 月卡",
		Amount:       decimal.RequireFromString("19.90"),
		ProviderType: payType,
		ClientIP:     "14.23.150.211",
		ReturnURL:    "https://console.example.com/pay/result",
		ExpireAt:     &expire,
	}
}

func dyNested(t *testing.T, m map[string]any, path ...string) any {
	t.Helper()
	var cur any = m
	for _, key := range path {
		obj, ok := cur.(map[string]any)
		if !ok {
			t.Fatalf("%v 不是对象（在 %s）", cur, key)
		}
		cur = obj[key]
	}
	return cur
}

// ── 下单 ──

func TestDouyinPayCreateOrderH5(t *testing.T) {
	f := newDyFakeServer(t)
	f.handle("POST /v1/trade/transactions/h5", func(r *http.Request, body map[string]any) (int, any) {
		return 200, map[string]any{"h5_url": "https://cashier.douyinpay.com/bytepay-cashdesk/bytepay-invoke?prepay_id=dy11"}
	})
	p := f.provider()
	req := dyOrderRequest("h5")
	req.Metadata = map[string]any{"platform": "android"}
	payload, err := p.CreateOrder(context.Background(), f.config(map[string]any{"h5ReturnSchema": "voyage://"}), req)
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	sent := f.lastRequest("/v1/trade/transactions/h5").Body
	checks := map[string]any{
		"mchid":        dyTestMchID,
		"appid":        dyTestWebAppID, // H5 用网站应用 AppID
		"out_trade_no": req.OrderNo,
		"notify_url":   "https://api.example.com/api/public/pay/callback/douyinpay/10004",
		"time_expire":  "2026-11-27T20:30:00+08:00",
		"description":  "远航会员 · 月卡",
	}
	for k, want := range checks {
		if sent[k] != want {
			t.Errorf("%s = %v，期望 %v", k, sent[k], want)
		}
	}
	if total := dyNested(t, sent, "amount", "total"); total != float64(1990) {
		t.Errorf("amount.total = %v，期望 1990 分", total)
	}
	if typ := dyNested(t, sent, "scene_info", "h5_info", "type"); typ != "Android" {
		t.Errorf("h5_info.type = %v，期望 Android（取自 metadata.platform）", typ)
	}
	if ip := dyNested(t, sent, "scene_info", "payer_client_ip"); ip != "14.23.150.211" {
		t.Errorf("payer_client_ip = %v", ip)
	}
	wantLink := "https://cashier.douyinpay.com/bytepay-cashdesk/bytepay-invoke?prepay_id=dy11&return_schema=voyage%3A%2F%2F&return_url=https%3A%2F%2Fconsole.example.com%2Fpay%2Fresult"
	if payload.PaymentURL != wantLink || payload.RedirectURL != wantLink {
		t.Errorf("payment_url = %s\n期望 %s", payload.PaymentURL, wantLink)
	}
	if payload.FormData["referer"] != "https://www.example.com" {
		t.Errorf("form_data.referer = %v", payload.FormData["referer"])
	}
	if payload.ProviderType != "h5" {
		t.Errorf("provider_type = %s", payload.ProviderType)
	}
}

func TestDouyinPayCreateOrderNative(t *testing.T) {
	f := newDyFakeServer(t)
	f.handle("POST /v1/trade/transactions/native", func(r *http.Request, body map[string]any) (int, any) {
		return 200, map[string]any{"code_url": "https://qr.douyinpay.com/ntv03L53eWwNKwKkMfRvMlYvw5eJXg"}
	})
	payload, err := f.provider().CreateOrder(context.Background(), f.config(nil), dyOrderRequest("native"))
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if got := f.lastRequest("/v1/trade/transactions/native").Body["appid"]; got != dyTestAppID {
		t.Errorf("native appid = %v，期望移动应用 AppID", got)
	}
	if payload.FormData["codeUrl"] != "https://qr.douyinpay.com/ntv03L53eWwNKwKkMfRvMlYvw5eJXg" {
		t.Errorf("form_data.codeUrl = %v", payload.FormData["codeUrl"])
	}
}

func TestDouyinPayCreateOrderAppSignsInvokeParams(t *testing.T) {
	f := newDyFakeServer(t)
	f.handle("POST /v1/trade/transactions/app", func(r *http.Request, body map[string]any) (int, any) {
		return 200, map[string]any{"prepay_id": "dy11ys5s7sxyv43x10894ooso28squx66quv34u3qsx"}
	})
	payload, err := f.provider().CreateOrder(context.Background(), f.config(map[string]any{"supportedTypes": []any{"app"}}), dyOrderRequest("app"))
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	params, ok := payload.FormData["sdkParams"].(map[string]string)
	if !ok {
		t.Fatalf("form_data.sdkParams 类型 %T", payload.FormData["sdkParams"])
	}
	if params["appid"] != dyTestAppID || params["partnerid"] != dyTestMchID || params["package"] != "Sign=DYPay" ||
		params["prepayid"] != "dy11ys5s7sxyv43x10894ooso28squx66quv34u3qsx" {
		t.Errorf("sdkParams = %v", params)
	}
	dyVerifyMerchantSign(t, f.merchantKey,
		params["appid"]+"\n"+params["timestamp"]+"\n"+params["noncestr"]+"\n"+params["prepayid"]+"\n", params["sign"])
}

func TestDouyinPayCreateOrderJSAPIAndMini(t *testing.T) {
	f := newDyFakeServer(t)
	f.handle("POST /v1/trade/transactions/jsapi", func(r *http.Request, body map[string]any) (int, any) {
		return 200, map[string]any{"prepay_id": "dy96y894ox66yv43x10uquv34s5s7sx3oso"}
	})
	p := f.provider()
	cfg := f.config(map[string]any{"supportedTypes": []any{"jsapi", "mini"}})

	// 没有 openid：本地就拒，不去上游
	if _, err := p.CreateOrder(context.Background(), cfg, dyOrderRequest("jsapi")); err == nil {
		t.Fatal("JSAPI 缺 openid 应当报错")
	}

	for _, tc := range []struct{ payType, appID, key string }{
		{"jsapi", dyTestWebAppID, "sdkParams"},
		{"mini", dyTestMiniAppID, "businessInfo"},
	} {
		req := dyOrderRequest(tc.payType)
		req.Metadata = map[string]any{"openid": "0a74ba22-0779-4b8b-aa5d-14bb134f74ad"}
		payload, err := p.CreateOrder(context.Background(), cfg, req)
		if err != nil {
			t.Fatalf("%s CreateOrder: %v", tc.payType, err)
		}
		sent := f.lastRequest("/v1/trade/transactions/jsapi").Body
		if sent["appid"] != tc.appID {
			t.Errorf("%s appid = %v，期望 %s", tc.payType, sent["appid"], tc.appID)
		}
		if openid := dyNested(t, sent, "payer", "openid"); openid != "0a74ba22-0779-4b8b-aa5d-14bb134f74ad" {
			t.Errorf("%s payer.openid = %v", tc.payType, openid)
		}
		params := payload.FormData[tc.key].(map[string]string)
		if params["package"] != "prepay_id=dy96y894ox66yv43x10uquv34s5s7sx3oso" || params["signType"] != "DouyinPay-RSA" || params["appId"] != tc.appID {
			t.Errorf("%s 调起参数 = %v", tc.payType, params)
		}
		dyVerifyMerchantSign(t, f.merchantKey,
			params["appId"]+"\n"+params["timeStamp"]+"\n"+params["nonceStr"]+"\n"+params["package"]+"\n", params["paySign"])
	}
}

func TestDouyinPayPartnerMode(t *testing.T) {
	f := newDyFakeServer(t)
	f.handle("POST /v1/trade/partner/transactions/h5", func(r *http.Request, body map[string]any) (int, any) {
		return 200, map[string]any{"h5_url": "https://cashier.douyinpay.com/x?prepay_id=dy22"}
	})
	f.handle("GET /v1/trade/partner/transactions/out-trade-no/P1000420261127120000123456", func(r *http.Request, body map[string]any) (int, any) {
		return 200, map[string]any{
			"sp_mchid": dyTestMchID, "sub_mchid": "6020000000000999", "out_trade_no": "P1000420261127120000123456",
			"transaction_id": "TP1", "trade_state": "SUCCESS", "amount": map[string]any{"total": 1990},
		}
	})
	p := f.provider()
	cfg := f.config(map[string]any{"subMchId": "6020000000000999", "subWebAppId": "awsubweb00001"})
	if _, err := p.CreateOrder(context.Background(), cfg, dyOrderRequest("h5")); err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	sent := f.lastRequest("/v1/trade/partner/transactions/h5").Body
	if sent["sp_mchid"] != dyTestMchID || sent["sub_mchid"] != "6020000000000999" || sent["sp_appid"] != dyTestWebAppID || sent["sub_appid"] != "awsubweb00001" {
		t.Errorf("服务商下单字段 = %v", sent)
	}
	if _, ok := sent["mchid"]; ok {
		t.Error("服务商模式不该再带 mchid")
	}
	res, err := p.SettleRemoteOrders(withOnDemandSettle(context.Background()), cfg, []string{"P1000420261127120000123456"})
	if err != nil {
		t.Fatalf("SettleRemoteOrders: %v", err)
	}
	if r := res["P1000420261127120000123456"]; r == nil || !r.Paid || !r.Amount.Equal(decimal.RequireFromString("19.90")) {
		t.Errorf("补单结果 = %+v", r)
	}
	q := f.lastRequest("/v1/trade/partner/transactions/out-trade-no/P1000420261127120000123456").Query
	if q.Get("sp_mchid") != dyTestMchID || q.Get("sub_mchid") != "6020000000000999" {
		t.Errorf("服务商查单参数 = %v", q)
	}
}

func TestDouyinPayUpstreamErrorIsReadable(t *testing.T) {
	f := newDyFakeServer(t)
	f.handle("POST /v1/trade/transactions/h5", func(r *http.Request, body map[string]any) (int, any) {
		return 400, map[string]any{"code": "APPID_MCHID_NOT_MATCH", "message": "appid和mch_id不匹配"}
	})
	_, err := f.provider().CreateOrder(context.Background(), f.config(nil), dyOrderRequest("h5"))
	if err == nil || !strings.Contains(err.Error(), "APPID_MCHID_NOT_MATCH") || !strings.Contains(err.Error(), "appid和mch_id不匹配") {
		t.Fatalf("错误信息 = %v", err)
	}
}

// ── 查单与补单 ──

func TestDouyinPayQueryAndSettle(t *testing.T) {
	f := newDyFakeServer(t)
	state := "NOTPAY"
	f.handle("GET /v1/trade/transactions/out-trade-no/P1", func(r *http.Request, body map[string]any) (int, any) {
		// 文档示例里金额有时是字符串，provider 两种都得认
		return 200, map[string]any{
			"mchid": dyTestMchID, "appid": dyTestWebAppID, "out_trade_no": "P1", "transaction_id": "21000125010103000993845301123",
			"trade_type": "H5", "trade_state": state, "amount": map[string]any{"total": "1990", "payer_total": "1890", "currency": "CNY"},
		}
	})
	f.handle("GET /v1/trade/transactions/out-trade-no/P2", func(r *http.Request, body map[string]any) (int, any) {
		return 404, map[string]any{"code": "ORDER_NOT_EXIST", "message": "订单不存在"}
	})
	p := f.provider()
	cfg := f.config(nil)

	info, err := p.QueryRemoteOrder(context.Background(), cfg, "P1")
	if err != nil {
		t.Fatalf("QueryRemoteOrder: %v", err)
	}
	if info["paid"] != false || info["trade_state"] != "NOTPAY" || info["amount"] != "19.90" {
		t.Errorf("查单结果 = %v", info)
	}
	if q := f.lastRequest("/v1/trade/transactions/out-trade-no/P1").Query; q.Get("mchid") != dyTestMchID {
		t.Errorf("查单缺 mchid：%v", q)
	}

	state = "SUCCESS"
	res, err := p.SettleRemoteOrders(withOnDemandSettle(context.Background()), cfg, []string{"P1", "P2"})
	if err != nil {
		t.Fatalf("SettleRemoteOrders: %v", err)
	}
	if r := res["P1"]; r == nil || !r.Paid || r.ProviderOrderNo != "21000125010103000993845301123" || !r.Amount.Equal(decimal.RequireFromString("19.90")) {
		t.Errorf("P1 = %+v", res["P1"])
	}
	if _, ok := res["P2"]; ok {
		t.Error("上游没有的订单不该出现在结果里")
	}
}

func TestDouyinPaySettleBackoff(t *testing.T) {
	p := newDouyinPayProvider(nil)
	start := time.Now()
	if !p.settleDue("P1", start, false) {
		t.Fatal("第一次必须查")
	}
	if !p.settleDue("P1", start.Add(2*time.Minute), false) {
		t.Fatal("前 10 分钟每轮都查")
	}
	// 第 11 分钟：已进入「10 分钟一次」，距上次（第 2 分钟）才 9 分钟
	if p.settleDue("P1", start.Add(11*time.Minute), false) {
		t.Fatal("10 分钟后应退避到 10 分钟一次")
	}
	if !p.settleDue("P1", start.Add(11*time.Minute), true) {
		t.Fatal("用户主动查单不受退避限制")
	}
	if p.settleDue("P1", start.Add(2*time.Hour), false) != true {
		t.Fatal("退避间隔到了应当再查")
	}
}

// ── 退款 ──

func TestDouyinPayRefundAndQuery(t *testing.T) {
	f := newDyFakeServer(t)
	f.handle("POST /v1/trade/refund/domestic/refunds", func(r *http.Request, body map[string]any) (int, any) {
		switch body["out_refund_no"] {
		case "R500":
			return 500, map[string]any{"code": "SYSTEM_ERROR", "message": "系统超时"}
		case "R403":
			return 403, map[string]any{"code": "NOT_ENOUGH", "message": "余额不足"}
		}
		return 200, map[string]any{
			"refund_id": "21000225010103000993845301326", "out_refund_no": body["out_refund_no"], "out_trade_no": "P1",
			"status": "PROCESSING", "amount": map[string]any{"refund": 500, "total": 1990},
		}
	})
	f.handle("GET /v1/trade/refund/domestic/refunds/R1", func(r *http.Request, body map[string]any) (int, any) {
		return 200, map[string]any{
			"refund_id": "21000225010103000993845301326", "out_refund_no": "R1", "status": "SUCCESS",
			"channel": "ORIGINAL", "amount": map[string]any{"refund": "500", "total": "1990"},
		}
	})
	p := f.provider()
	cfg := f.config(map[string]any{"notifyUrl": ""})

	// 退款通知地址文档要求必填：一个都给不出来时本地就拒
	if _, err := p.Refund(context.Background(), cfg, paymentdomain.RefundRequest{AppID: 10004, OrderNo: "P1", RefundNo: "R1",
		RefundAmount: decimal.RequireFromString("5"), TotalAmount: decimal.RequireFromString("19.90")}); err == nil {
		t.Fatal("没有通知地址应当报错")
	}

	result, err := p.Refund(context.Background(), cfg, paymentdomain.RefundRequest{
		AppID: 10004, OrderNo: "P1", ProviderOrderNo: "21000125010103000993845301123", RefundNo: "R1",
		Reason: "用户申请退款", RefundAmount: decimal.RequireFromString("5"), TotalAmount: decimal.RequireFromString("19.90"),
		NotifyURL: "https://api.example.com/api/public/pay/callback/douyinpay",
	})
	if err != nil {
		t.Fatalf("Refund: %v", err)
	}
	if result.Status != paymentdomain.RefundProcessing || result.ProviderRefundNo != "21000225010103000993845301326" {
		t.Errorf("退款结果 = %+v", result)
	}
	sent := f.lastRequest("/v1/trade/refund/domestic/refunds").Body
	if sent["mchid"] != dyTestMchID || sent["transaction_id"] != "21000125010103000993845301123" ||
		sent["notify_url"] != "https://api.example.com/api/public/pay/callback/douyinpay/10004" || sent["reason"] != "用户申请退款" {
		t.Errorf("退款请求 = %v", sent)
	}
	if dyNested(t, sent, "amount", "refund") != float64(500) || dyNested(t, sent, "amount", "total") != float64(1990) {
		t.Errorf("退款金额 = %v", sent["amount"])
	}

	// 上游 5xx：可能已受理，记处理中等查询，不能判失败
	unknown, err := p.Refund(context.Background(), cfg, paymentdomain.RefundRequest{AppID: 10004, OrderNo: "P1", RefundNo: "R500",
		RefundAmount: decimal.RequireFromString("5"), TotalAmount: decimal.RequireFromString("19.90"),
		NotifyURL: "https://api.example.com/api/public/pay/callback/douyinpay"})
	if err != nil || unknown.Status != paymentdomain.RefundProcessing {
		t.Fatalf("5xx 应记为处理中：%+v, %v", unknown, err)
	}
	// 上游明确拒绝：报错（网关据此释放额度）
	if _, err := p.Refund(context.Background(), cfg, paymentdomain.RefundRequest{AppID: 10004, OrderNo: "P1", RefundNo: "R403",
		RefundAmount: decimal.RequireFromString("5"), TotalAmount: decimal.RequireFromString("19.90"),
		NotifyURL: "https://api.example.com/api/public/pay/callback/douyinpay"}); err == nil || !strings.Contains(err.Error(), "NOT_ENOUGH") {
		t.Fatalf("4xx 应报错：%v", err)
	}
	// 查无此退款单：判失败释放额度
	if missing, err := p.QueryRefund(context.Background(), cfg, paymentdomain.RefundQuery{RefundNo: "R404"}); err != nil || missing.Status != paymentdomain.RefundFailed {
		t.Fatalf("查无此单应判失败：%+v, %v", missing, err)
	}

	queried, err := p.QueryRefund(context.Background(), cfg, paymentdomain.RefundQuery{RefundNo: "R1"})
	if err != nil {
		t.Fatalf("QueryRefund: %v", err)
	}
	if queried.Status != paymentdomain.RefundSuccess || !queried.Amount.Equal(decimal.RequireFromString("5")) {
		t.Errorf("退款查询 = %+v", queried)
	}
}

func TestDouyinPayRefundStatus(t *testing.T) {
	cases := map[string]string{
		"SUCCESS":    paymentdomain.RefundSuccess,
		"CLOSED":     paymentdomain.RefundClosed,
		"PROCESSING": paymentdomain.RefundProcessing,
		"ABNORMAL":   paymentdomain.RefundProcessing, // 钱可能在途，不能判失败
		"":           paymentdomain.RefundProcessing,
	}
	for in, want := range cases {
		if got := douyinRefundStatus(in); got != want {
			t.Errorf("douyinRefundStatus(%q) = %s，期望 %s", in, got, want)
		}
	}
}

// ── 回调 ──

func TestDouyinPayPaymentNotify(t *testing.T) {
	f := newDyFakeServer(t)
	p := f.provider()
	cfg := f.config(nil)
	data := f.notify("TRANSACTION.SUCCESS", "transaction", map[string]any{
		"mchid": dyTestMchID, "appid": dyTestWebAppID, "out_trade_no": "P1", "transaction_id": "TP2022113021504111301402044101",
		"trade_type": "H5", "trade_state": "SUCCESS", "success_time": "2022-11-30T21:51:32+08:00",
		"amount": map[string]any{"total": 1990, "payer_total": 1890, "currency": "CNY"},
	})
	res, err := p.HandleCallback(context.Background(), cfg, data, "1.2.3.4")
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if !res.Paid || res.OrderNo != "P1" || res.ProviderOrderNo != "TP2022113021504111301402044101" ||
		!res.Amount.Equal(decimal.RequireFromString("19.90")) || res.Refund != nil {
		t.Errorf("支付通知结果 = %+v", res)
	}

	// 篡改正文：签名对不上
	tampered := map[string]string{}
	for k, v := range data {
		tampered[k] = v
	}
	tampered[CallbackRawBodyKey] = strings.Replace(data[CallbackRawBodyKey], "TRANSACTION.SUCCESS", "TRANSACTION.SUCCESS ", 1)
	if _, err := p.HandleCallback(context.Background(), cfg, tampered, ""); err == nil {
		t.Fatal("篡改过的通知必须验签失败")
	}

	// 别的商户号发来的（签名合法也不认）
	other := f.notify("TRANSACTION.SUCCESS", "transaction", map[string]any{
		"mchid": "6020000000000001", "out_trade_no": "P1", "trade_state": "SUCCESS", "amount": map[string]any{"total": 1990},
	})
	if _, err := p.HandleCallback(context.Background(), cfg, other, ""); err == nil {
		t.Fatal("商户号不一致的通知必须拒绝")
	}

	// 时间戳过期
	stale := map[string]string{}
	for k, v := range data {
		stale[k] = v
	}
	stale[CallbackHeaderPrefix+"douyinpay-timestamp"] = strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
	if _, err := p.HandleCallback(context.Background(), cfg, stale, ""); err == nil || !strings.Contains(err.Error(), "时间戳") {
		t.Fatalf("过期通知应当拒绝：%v", err)
	}
}

func TestDouyinPayRefundNotify(t *testing.T) {
	f := newDyFakeServer(t)
	p := f.provider()
	data := f.notify("REFUND.SUCCESS", "refund", map[string]any{
		"mchid": dyTestMchID, "out_trade_no": "P1", "transaction_id": "TP1", "out_refund_no": "R1",
		"refund_id": "TP2022113021442526324055484453", "refund_status": "SUCCESS", "success_time": "2022-11-30T21:45:20+08:00",
		"amount": map[string]any{"total": 1990, "refund": 500, "payer_total": 1990, "payer_refund": 500},
	})
	res, err := p.HandleCallback(context.Background(), f.config(nil), data, "")
	if err != nil {
		t.Fatalf("HandleCallback: %v", err)
	}
	if res.Paid || res.Refund == nil {
		t.Fatalf("退款通知不该被当成支付：%+v", res)
	}
	n := res.Refund
	if n.RefundNo != "R1" || n.OrderNo != "P1" || n.Status != paymentdomain.RefundSuccess ||
		n.ProviderRefundNo != "TP2022113021442526324055484453" || !n.Amount.Equal(decimal.RequireFromString("5")) {
		t.Errorf("退款通知 = %+v", n)
	}
}

func TestDouyinPayCertificateCache(t *testing.T) {
	f := newDyFakeServer(t)
	f.handle("GET /v1/trade/transactions/out-trade-no/P1", func(r *http.Request, body map[string]any) (int, any) {
		return 200, map[string]any{"mchid": dyTestMchID, "out_trade_no": "P1", "trade_state": "NOTPAY", "amount": map[string]any{"total": 1}}
	})
	p := f.provider()
	cfg := f.config(nil)
	for i := 0; i < 3; i++ {
		if _, err := p.QueryRemoteOrder(context.Background(), cfg, "P1"); err != nil {
			t.Fatal(err)
		}
	}
	if f.certCalls != 1 {
		t.Errorf("同一套凭据只该下载一次平台证书，实际 %d 次", f.certCalls)
	}
	// 固定平台证书：不再调下载接口
	pinned := f.config(map[string]any{"platformCertificate": f.platformCert, "serialNo": "PINNED0000000001"})
	before := f.certCalls
	if _, err := p.HandleCallback(context.Background(), pinned, f.notify("TRANSACTION.SUCCESS", "transaction", map[string]any{
		"mchid": dyTestMchID, "out_trade_no": "P9", "trade_state": "SUCCESS", "amount": map[string]any{"total": 1},
	}), ""); err != nil {
		t.Fatalf("固定证书验签：%v", err)
	}
	if f.certCalls != before {
		t.Error("填了平台证书就不该再下载")
	}
}

// ── 配置与工具 ──

func TestDouyinPayValidateConfig(t *testing.T) {
	f := newDyFakeServer(t)
	p := newDouyinPayProvider(nil)
	if err := p.ValidateConfig(f.config(nil)); err != nil {
		t.Fatalf("完整配置应当通过：%v", err)
	}
	cases := []struct {
		name  string
		extra map[string]any
	}{
		{"加密密钥长度不对", map[string]any{"encryptKey": "short"}},
		{"私钥不是 PEM", map[string]any{"privateKey": "not a key"}},
		{"开放了 H5 却没有网站应用 AppID", map[string]any{"webAppId": ""}},
		{"开放了小程序却没有小程序 AppID", map[string]any{"supportedTypes": []any{"mini"}, "miniAppId": ""}},
		{"证书类型不认识", map[string]any{"signType": "DSA"}},
	}
	for _, tc := range cases {
		if err := p.ValidateConfig(f.config(tc.extra)); err == nil {
			t.Errorf("%s：应当报错", tc.name)
		}
	}
}

func TestDouyinPayRoutedPayTypes(t *testing.T) {
	p := newDouyinPayProvider(nil)
	types, fallback := p.RoutedPayTypes(map[string]any{})
	if len(types) != 2 || types[0].Value != "h5" || types[1].Value != "native" || fallback != "h5" {
		t.Errorf("默认放行 = %v / %s", types, fallback)
	}
	types, fallback = p.RoutedPayTypes(map[string]any{"supportedTypes": []any{"app", "native", "bogus"}})
	if len(types) != 2 || types[0].Value != "app" || fallback != "app" {
		t.Errorf("按配置放行 = %v / %s", types, fallback)
	}
}

func TestDouyinNotifyURL(t *testing.T) {
	cases := []struct {
		requested, configured string
		want                  string
		ok                    bool
	}{
		{"", "https://a.com/api/public/pay/callback/douyinpay", "https://a.com/api/public/pay/callback/douyinpay/7", true},
		{"", "https://a.com/api/public/pay/callback/douyinpay/7", "https://a.com/api/public/pay/callback/douyinpay/7", true},
		{"", "http://a.com/cb", "", false},
		{"", "https://a.com/cb?x=1", "", false},
		{"", "", "", false},
	}
	for _, tc := range cases {
		got, err := douyinNotifyURL(tc.requested, tc.configured, 7)
		if (err == nil) != tc.ok || got != tc.want {
			t.Errorf("douyinNotifyURL(%q) = %q, %v", tc.configured, got, err)
		}
	}
}

func TestDouyinH5Type(t *testing.T) {
	cfg := &paymentdomain.DouyinPayConfig{H5Type: "iOS"}
	cases := map[string]string{"android": "Android", "iOS": "iOS", "harmonyos": "HarmonyOS", "web": "Wap", "": "iOS"}
	for platform, want := range cases {
		meta := map[string]any{}
		if platform != "" {
			meta["platform"] = platform
		}
		if got := douyinH5Type(cfg, meta); got != want {
			t.Errorf("platform=%q → %s，期望 %s", platform, got, want)
		}
	}
}

func TestDouyinDecrypt(t *testing.T) {
	// 文档的两种写法都按 AES-256-GCM
	for _, algo := range []string{"AEAD-AES-256-GCM", "AEAD_AES_256_GCM"} {
		ct := dyEncrypt(t, dyTestEncryptKey, "ippNsxmNlvJm", "transaction", `{"a":1}`)
		plain, err := douyinDecrypt(algo, dyTestEncryptKey, "ippNsxmNlvJm", "transaction", ct)
		if err != nil || string(plain) != `{"a":1}` {
			t.Errorf("%s 解密 = %q, %v", algo, plain, err)
		}
	}
	// SM4-CBC + PKCS#7
	key, iv := "1234567890abcdef", "abcdef1234567890"
	block, _ := sm4.NewCipher([]byte(key))
	msg := []byte(`{"trade_state":"SUCCESS"}`)
	pad := block.BlockSize() - len(msg)%block.BlockSize()
	padded := append(append([]byte{}, msg...), []byte(strings.Repeat(string(rune(pad)), pad))...)
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, []byte(iv)).CryptBlocks(out, padded)
	plain, err := douyinDecrypt("SM4", key, iv, "", base64.StdEncoding.EncodeToString(out))
	if err != nil || string(plain) != string(msg) {
		t.Errorf("SM4 解密 = %q, %v", plain, err)
	}
	// 不合法的 SM4 输入只报错，不 panic
	if _, err := douyinDecrypt("SM4", key, "short", "", base64.StdEncoding.EncodeToString([]byte("123"))); err == nil {
		t.Error("非法 SM4 输入应当报错")
	}
}

func TestDouyinPayDescribeMatchesCapabilities(t *testing.T) {
	var p paymentProvider = newDouyinPayProvider(nil)
	meta := p.Describe()
	if _, ok := p.(paymentRefunder); ok != meta.Capabilities.Refund {
		t.Error("Refund 能力与实现不一致")
	}
	if _, ok := p.(refundQuerier); ok != meta.Capabilities.RefundQuery {
		t.Error("RefundQuery 能力与实现不一致")
	}
	if _, ok := p.(payTypeRouter); !ok {
		t.Error("抖音支付应当按付款方式分项")
	}
	if _, ok := p.(remoteOrderSettler); !ok {
		t.Error("抖音支付应当支持主动补单")
	}
	if meta.CallbackPath != "/api/public/pay/callback/douyinpay" {
		t.Errorf("CallbackPath = %s", meta.CallbackPath)
	}
}
