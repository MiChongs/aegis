package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appdomain "aegis/internal/domain/app"
	authprotocol "aegis/internal/domain/authprotocol"
	apperrors "aegis/pkg/errors"

	"github.com/gin-gonic/gin"
)

// 网关只认路径上的 appKey，不接受任何别处的来源。
func TestAppKeyFromGatewayPath(t *testing.T) {
	cases := map[string]string{
		"/api/v1/apps/demo_app/config":     "demo_app",
		"/api/v1/apps/demo_app/auth/login": "demo_app",
		"/api/v1/apps/demo_app":            "demo_app",
		"/api/v1/apps/":                    "",
		"/api/v2/apps/demo_app/bootstrap":  "",
		"/api/auth/login/password":         "",
	}
	for path, want := range cases {
		if got := appKeyFromGatewayPath(path); got != want {
			t.Fatalf("path %s: 期望 appKey %q，实际 %q", path, want, got)
		}
	}
}

type stubGatewayService struct {
	level        string
	signatureErr error
	signedCalls  int
	openCalls    int
	// 记录最后一次拆包的输入，供用例断言「签的是什么、解的是什么」。
	lastSignature authprotocol.SignatureMetadata
	lastRequest   authprotocol.RequestMetadata
	lastCipher    string
	plaintext     string
}

func (s *stubGatewayService) ResolveAppAndPolicy(context.Context, string) (*appdomain.App, *authprotocol.Policy, error) {
	return &appdomain.App{ID: 7, AppKey: "demo_app", Status: true},
		&authprotocol.Policy{AppID: 7, SecurityLevel: s.level}, nil
}

func (s *stubGatewayService) VerifySignature(_ context.Context, meta authprotocol.SignatureMetadata) error {
	s.signedCalls++
	s.lastSignature = meta
	return s.signatureErr
}

func (s *stubGatewayService) OpenRequest(_ context.Context, meta authprotocol.RequestMetadata, cipher []byte) ([]byte, *authprotocol.CryptoContext, error) {
	s.openCalls++
	s.lastRequest = meta
	s.lastCipher = string(cipher)
	plaintext := s.plaintext
	if plaintext == "" {
		plaintext = `{"unwrapped":true}`
	}
	return []byte(plaintext), &authprotocol.CryptoContext{AppID: 7, AppKey: "demo_app"}, nil
}

func (s *stubGatewayService) SealResponse(_ *authprotocol.CryptoContext, _ int, plaintext []byte) ([]byte, string, error) {
	return plaintext, "response-nonce", nil
}

func newGatewayRouter(service appGatewayService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(AppGateway(service))
	handler := func(c *gin.Context) {
		body, _ := json.Marshal(gin.H{"seen": true})
		c.Data(http.StatusOK, "application/json", body)
	}
	router.GET("/api/v1/apps/:appkey/config", handler)
	router.GET("/api/v1/apps/:appkey/auth/oauth/callback", handler)
	router.POST("/api/v1/apps/:appkey/auth/login", handler)
	// 回显 handler 看到的 query 与 Content-Type：拆包是否正确只有下游能证明。
	router.GET("/api/v1/apps/:appkey/me/sessions", func(c *gin.Context) {
		body, _ := json.Marshal(gin.H{"query": c.Request.URL.RawQuery, "page": c.Query("page")})
		c.Data(http.StatusOK, "application/json", body)
	})
	router.POST("/api/v1/apps/:appkey/me/avatar", func(c *gin.Context) {
		body, _ := json.Marshal(gin.H{"contentType": c.GetHeader("Content-Type")})
		c.Data(http.StatusOK, "application/json", body)
	})
	return router
}

// standard 档必须真的什么都不要求——这是"6 行 fetch 就能接入"的底线。
func TestAppGatewayStandardLevelRequiresNothing(t *testing.T) {
	service := &stubGatewayService{level: authprotocol.LevelStandard}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/apps/demo_app/auth/login",
		bytes.NewReader([]byte(`{"account":"alice","password":"x"}`)))
	request.Header.Set("Content-Type", "application/json")
	newGatewayRouter(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("standard 档不应拦截任何请求，实际 %d：%s", recorder.Code, recorder.Body.String())
	}
	if service.signedCalls != 0 || service.openCalls != 0 {
		t.Fatal("standard 档不应触发签名或解密")
	}
}

// 两条免包装路径在任何等级下都必须可达：
// /config 是"要读配置得先按配置包装"的死锁出口；
// OAuth 回跳由第三方重定向浏览器发起，客户端根本没机会包装它。
func TestAppGatewayUnwrappedPathsReachableAtEveryLevel(t *testing.T) {
	paths := []string{
		"/api/v1/apps/demo_app/config",
		"/api/v1/apps/demo_app/auth/oauth/callback",
	}
	for _, level := range authprotocol.SecurityLevels {
		for _, path := range paths {
			service := &stubGatewayService{level: level}
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, path, nil)
			newGatewayRouter(service).ServeHTTP(recorder, request)
			if recorder.Code != http.StatusOK {
				t.Fatalf("等级 %s 下 %s 应免包装可读，实际 %d", level, path, recorder.Code)
			}
			if service.signedCalls != 0 {
				t.Fatalf("等级 %s 下 %s 不应要求签名", level, path)
			}
		}
	}
}

func TestAppGatewaySignedLevelRejectsBadSignature(t *testing.T) {
	service := &stubGatewayService{
		level:        authprotocol.LevelSigned,
		signatureErr: apperrors.New(40175, http.StatusUnauthorized, "请求签名校验失败"),
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/apps/demo_app/auth/login",
		bytes.NewReader([]byte(`{"account":"alice"}`)))
	newGatewayRouter(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("签名错误应返回 401，实际 %d：%s", recorder.Code, recorder.Body.String())
	}
	if service.openCalls != 0 {
		t.Fatal("签名未通过时不应继续解密载荷")
	}
}

// sealed 是 signed 的超集：先验签证明调用方持有 appSecret，再拆加密载荷。
func TestAppGatewaySealedLevelVerifiesSignatureBeforeDecrypting(t *testing.T) {
	service := &stubGatewayService{level: authprotocol.LevelSealed}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/apps/demo_app/auth/login",
		bytes.NewReader([]byte("ciphertext")))
	request.Header.Set(gwHeaderProtocol, authprotocol.TransportV2)
	newGatewayRouter(service).ServeHTTP(recorder, request)

	if service.signedCalls != 1 {
		t.Fatalf("sealed 档必须验签一次，实际 %d 次", service.signedCalls)
	}
	if service.openCalls != 1 {
		t.Fatalf("sealed 档必须解密一次，实际 %d 次", service.openCalls)
	}
	if recorder.Header().Get(gwHeaderResponseNonce) == "" {
		t.Fatal("sealed 响应必须带上 X-Aegis-Response-Nonce")
	}
}

// sealed 档收到明文请求要给出可操作的 426，而不是含糊的 400。
func TestAppGatewaySealedLevelRejectsPlaintext(t *testing.T) {
	service := &stubGatewayService{level: authprotocol.LevelSealed}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/apps/demo_app/auth/login",
		bytes.NewReader([]byte(`{"account":"alice"}`)))
	newGatewayRouter(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUpgradeRequired {
		t.Fatalf("sealed 档遇明文应返回 426，实际 %d：%s", recorder.Code, recorder.Body.String())
	}
}

// sealed 档的 GET 曾经必然失败：中间件要求 body 非空，而 GET 没有 body。
// 现在密文走 `?_payload=`，解出来的明文回填成真正的 query string。
func TestAppGatewaySealedLevelOpensBodylessRequestFromQuery(t *testing.T) {
	service := &stubGatewayService{level: authprotocol.LevelSealed, plaintext: "page=3&pageSize=20"}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/apps/demo_app/me/sessions?"+authprotocol.SealedPayloadParam+"=Y2lwaGVydGV4dA", nil)
	request.Header.Set(gwHeaderProtocol, authprotocol.TransportV2)
	newGatewayRouter(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("sealed 档的 GET 应当可用，实际 %d：%s", recorder.Code, recorder.Body.String())
	}
	if service.openCalls != 1 {
		t.Fatalf("sealed 档的 GET 必须解密一次，实际 %d 次", service.openCalls)
	}
	if service.lastCipher != "Y2lwaGVydGV4dA" {
		t.Fatalf("密文应取自 %s 查询参数，实际 %q", authprotocol.SealedPayloadParam, service.lastCipher)
	}
	var payload struct {
		Query string `json:"query"`
		Page  string `json:"page"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("回显解析失败：%v", err)
	}
	if payload.Query != "page=3&pageSize=20" || payload.Page != "3" {
		t.Fatalf("解密后的 query 未回填给 handler：%+v", payload)
	}
}

// 无请求体的方法漏带 _payload 时要明确说清密文该放哪，而不是含糊的「载荷为空」。
func TestAppGatewaySealedLevelRejectsBodylessRequestWithoutPayload(t *testing.T) {
	service := &stubGatewayService{level: authprotocol.LevelSealed}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/apps/demo_app/me/sessions", nil)
	request.Header.Set(gwHeaderProtocol, authprotocol.TransportV2)
	newGatewayRouter(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("缺少加密载荷应返回 400，实际 %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), authprotocol.SealedPayloadParam) {
		t.Fatalf("错误信息应指出密文该放哪，实际 %s", recorder.Body.String())
	}
	if service.openCalls != 0 {
		t.Fatal("没有密文就不该调用解密")
	}
}

// 原样 query 必须进签名，否则 `?page=1` 能被改成 `?page=999` 而签名照过。
func TestAppGatewaySignsRawQueryString(t *testing.T) {
	service := &stubGatewayService{level: authprotocol.LevelSigned}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/apps/demo_app/me/sessions?page=1&pageSize=20", nil)
	newGatewayRouter(service).ServeHTTP(recorder, request)

	if service.lastSignature.Query != "page=1&pageSize=20" {
		t.Fatalf("签名未覆盖 query，实际 %q", service.lastSignature.Query)
	}
	if service.lastSignature.Path != "/api/v1/apps/demo_app/me/sessions" {
		t.Fatalf("签名路径不应含 query，实际 %q", service.lastSignature.Path)
	}
}

// signed 档只验签、不碰载荷：Content-Type 原样保留，否则 multipart 上传
// 会被下游当成 JSON 解析，而报错指向的是「字段绑定失败」，与文件毫无关系。
func TestAppGatewaySignedLevelPreservesMultipartContentType(t *testing.T) {
	service := &stubGatewayService{level: authprotocol.LevelSigned}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/apps/demo_app/me/avatar",
		bytes.NewReader([]byte("--x\r\nContent-Disposition: form-data; name=\"f\"\r\n\r\n1\r\n--x--\r\n")))
	request.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	newGatewayRouter(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("signed 档应放行 multipart 上传，实际 %d：%s", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		ContentType string `json:"contentType"`
	}
	_ = json.Unmarshal(recorder.Body.Bytes(), &payload)
	if !strings.HasPrefix(payload.ContentType, "multipart/form-data") {
		t.Fatalf("Content-Type 被改写成了 %q", payload.ContentType)
	}
}

// sealed 档的上传：密文照旧走 body，原始类型由 X-Aegis-Plain-Content-Type 声明，
// 拆包后必须还原回去，否则下游同样把 multipart 当 JSON。
func TestAppGatewaySealedLevelRestoresPlainContentType(t *testing.T) {
	service := &stubGatewayService{
		level:     authprotocol.LevelSealed,
		plaintext: "--x\r\nContent-Disposition: form-data; name=\"f\"\r\n\r\n1\r\n--x--\r\n",
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/apps/demo_app/me/avatar",
		bytes.NewReader([]byte("ciphertext")))
	request.Header.Set(gwHeaderProtocol, authprotocol.TransportV2)
	request.Header.Set(gwHeaderPlainContentType, "multipart/form-data; boundary=x")
	newGatewayRouter(service).ServeHTTP(recorder, request)

	if service.lastRequest.PlainContentType != "multipart/form-data; boundary=x" {
		t.Fatalf("原始 Content-Type 未透传给解密层，实际 %q", service.lastRequest.PlainContentType)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("sealed 档应放行上传，实际 %d：%s", recorder.Code, recorder.Body.String())
	}
}

// 路径与头指向不同应用时必须拒绝，避免越权指向别的应用。
func TestAppGatewayRejectsMismatchedAppKeyHeader(t *testing.T) {
	service := &stubGatewayService{level: authprotocol.LevelStandard}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/apps/demo_app/auth/login",
		bytes.NewReader([]byte(`{}`)))
	request.Header.Set(gwHeaderAppKey, "other_app")
	newGatewayRouter(service).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("appKey 不一致应返回 400，实际 %d", recorder.Code)
	}
}
