package middleware

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appdomain "aegis/internal/domain/app"
	"aegis/pkg/response"
	"github.com/gin-gonic/gin"
)

type stubAppEncryptionService struct {
	app *appdomain.App
}

func (s *stubAppEncryptionService) GetApp(context.Context, int64) (*appdomain.App, error) {
	return s.app, nil
}

func (s *stubAppEncryptionService) ResolveTransportEncryption(app *appdomain.App) appdomain.TransportEncryptionPolicy {
	return appdomain.TransportEncryptionPolicy{
		Enabled:            true,
		Strict:             true,
		ResponseEncryption: true,
		Secret:             "transport-secret",
	}
}

func TestAppEncryptionRejectsPlaintextRequest(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(AppEncryption(&stubAppEncryptionService{
		app: &appdomain.App{ID: 100, AppKey: "app-key"},
	}))
	router.GET("/api/user/banner", func(c *gin.Context) {
		response.Success(c, 200, "ok", gin.H{"ok": true})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/user/banner?appid=100", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", recorder.Code)
	}

	var envelope response.Envelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if envelope.Message != appEncryptionPlaintextMessage {
		t.Fatalf("expected message %q, got %q", appEncryptionPlaintextMessage, envelope.Message)
	}
}

func TestAppEncryptionHandlesEncryptedQueryAndResponse(t *testing.T) {
	t.Parallel()

	const appID int64 = 100
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(AppEncryption(&stubAppEncryptionService{
		app: &appdomain.App{ID: appID, AppKey: "app-key"},
	}))
	router.GET("/api/user/banner", func(c *gin.Context) {
		response.Success(c, 200, "ok", gin.H{"query": c.Request.URL.RawQuery})
	})

	ctx, err := newAppEncryptionContext("transport-secret", appEncryptionRuntimeConfig{}, appID, http.MethodGet, "/api/user/banner", "", "")
	if err != nil {
		t.Fatalf("new encryption context: %v", err)
	}
	nonce := bytes.Repeat([]byte{7}, ctx.stream.NonceSize())
	ciphertext, err := encryptBytes(ctx.stream, []byte("appid=100&page=2"), nonce, ctx.associatedData("request-query"))
	if err != nil {
		t.Fatalf("encrypt query: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/user/banner?_payload="+base64.RawURLEncoding.EncodeToString(ciphertext), nil)
	req.Header.Set(appEncryptionHeaderEnabled, "1")
	req.Header.Set(appEncryptionHeaderAppID, "100")
	req.Header.Set(appEncryptionHeaderNonce, base64.RawURLEncoding.EncodeToString(nonce))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get(appEncryptionHeaderEnabled) != "1" {
		t.Fatalf("expected encrypted response header")
	}

	plaintext := decryptRecordedResponse(t, recorder, ctx)
	var payload map[string]any
	if err := json.Unmarshal(plaintext, &payload); err != nil {
		t.Fatalf("unmarshal plaintext response: %v", err)
	}
	data, _ := payload["data"].(map[string]any)
	if data["query"] != "appid=100&page=2" {
		t.Fatalf("unexpected query payload: %#v", data["query"])
	}
}

func TestAppEncryptionHandlesLargeEncryptedBody(t *testing.T) {
	t.Parallel()

	const appID int64 = 100
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(AppEncryption(&stubAppEncryptionService{
		app: &appdomain.App{ID: appID, AppKey: "app-key"},
	}))
	router.POST("/api/auth/login/password", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		response.Success(c, 200, "ok", gin.H{"size": len(body)})
	})

	ctx, err := newAppEncryptionContext("transport-secret", appEncryptionRuntimeConfig{}, appID, http.MethodPost, "/api/auth/login/password", "", "")
	if err != nil {
		t.Fatalf("new encryption context: %v", err)
	}

	plaintext := []byte(`{"appid":100,"payload":"` + strings.Repeat("x", 1<<20) + `"}`)
	nonce := bytes.Repeat([]byte{9}, ctx.stream.NonceSize())
	ciphertext, err := encryptBytes(ctx.stream, plaintext, nonce, ctx.associatedData("request-body"))
	if err != nil {
		t.Fatalf("encrypt body: %v", err)
	}
	verified, err := decryptBytes(ctx.stream, ciphertext, nonce, ctx.associatedData("request-body"))
	if err != nil {
		t.Fatalf("verify decrypt body: %v", err)
	}
	if !bytes.Equal(verified, plaintext) {
		t.Fatalf("decrypted plaintext mismatch")
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login/password", bytes.NewReader(ciphertext))
	req.Header.Set(appEncryptionHeaderEnabled, "1")
	req.Header.Set(appEncryptionHeaderAppID, "100")
	req.Header.Set(appEncryptionHeaderNonce, base64.RawURLEncoding.EncodeToString(nonce))
	req.Header.Set(appEncryptionHeaderPlainContentType, "application/json")
	verifyReq := httptest.NewRequest(http.MethodPost, "/api/auth/login/password", bytes.NewReader(ciphertext))
	verifyReq.Header = req.Header.Clone()
	if err := decryptEncryptedBody(verifyReq, ctx); err != nil {
		t.Fatalf("direct decrypt request body: %v", err)
	}
	verifyBody, err := io.ReadAll(verifyReq.Body)
	if err != nil {
		t.Fatalf("read direct decrypted request body: %v", err)
	}
	if !bytes.Equal(verifyBody, plaintext) {
		t.Fatalf("direct decrypted body mismatch")
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", recorder.Code, recorder.Body.String())
	}

	responsePlaintext := decryptRecordedResponse(t, recorder, ctx)
	var payload map[string]any
	if err := json.Unmarshal(responsePlaintext, &payload); err != nil {
		t.Fatalf("unmarshal plaintext response: %v", err)
	}
	data, _ := payload["data"].(map[string]any)
	if int(data["size"].(float64)) != len(plaintext) {
		t.Fatalf("unexpected response size: %#v", data["size"])
	}
}

func TestAppEncryptionHandlesHybridRSARequestAndResponse(t *testing.T) {
	t.Parallel()

	const appID int64 = 100
	publicKeyPEM, privateKeyPEM := generateTestRSAKeyPair(t)
	app := &appdomain.App{
		ID:     appID,
		AppKey: "app-key",
		Settings: map[string]any{
			"transportEncryption": map[string]any{
				"allowedAlgorithms": []any{AlgoHybridRSAXChaCha},
				"rsaPublicKey":      publicKeyPEM,
				"rsaPrivateKey":     privateKeyPEM,
			},
		},
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(AppEncryption(&stubAppEncryptionService{app: app}))
	router.POST("/api/auth/login/password", func(c *gin.Context) {
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		response.Success(c, 200, "ok", gin.H{"body": string(body)})
	})

	sessionKey := bytes.Repeat([]byte{3}, 32)
	clientCtx := mustNewClientEncryptionContext(t, appID, http.MethodPost, "/api/auth/login/password", AlgoHybridRSAXChaCha, sessionKey)
	transportKey := encryptRSAHybridKey(t, publicKeyPEM, sessionKey)

	plaintext := []byte(`{"appid":100,"account":"demo"}`)
	nonce := bytes.Repeat([]byte{4}, clientCtx.stream.NonceSize())
	ciphertext, err := encryptBytes(clientCtx.stream, plaintext, nonce, clientCtx.associatedData("request-body"))
	if err != nil {
		t.Fatalf("encrypt body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/auth/login/password", bytes.NewReader(ciphertext))
	req.Header.Set(appEncryptionHeaderEnabled, "1")
	req.Header.Set(appEncryptionHeaderAppID, "100")
	req.Header.Set(appEncryptionHeaderNonce, base64.RawURLEncoding.EncodeToString(nonce))
	req.Header.Set(appEncryptionHeaderAlgorithm, AlgoHybridRSAXChaCha)
	req.Header.Set(appEncryptionHeaderKey, base64.RawURLEncoding.EncodeToString(transportKey))
	req.Header.Set(appEncryptionHeaderPlainContentType, "application/json")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get(appEncryptionHeaderAlgorithm) != AlgoHybridRSAXChaCha {
		t.Fatalf("unexpected response algorithm: %s", recorder.Header().Get(appEncryptionHeaderAlgorithm))
	}

	responsePlaintext := decryptRecordedResponse(t, recorder, clientCtx)
	var payload map[string]any
	if err := json.Unmarshal(responsePlaintext, &payload); err != nil {
		t.Fatalf("unmarshal plaintext response: %v", err)
	}
	data, _ := payload["data"].(map[string]any)
	if data["body"] != string(plaintext) {
		t.Fatalf("unexpected response body: %#v", data["body"])
	}
}

func TestAppEncryptionHandlesHybridECDHRequestAndResponse(t *testing.T) {
	t.Parallel()

	const appID int64 = 100
	publicKeyPEM, privateKeyPEM, serverPublic := generateTestECDHKeyPair(t)
	app := &appdomain.App{
		ID:     appID,
		AppKey: "app-key",
		Settings: map[string]any{
			"transportEncryption": map[string]any{
				"allowedAlgorithms": []any{AlgoHybridECDHAES256},
				"ecdhPublicKey":     publicKeyPEM,
				"ecdhPrivateKey":    privateKeyPEM,
			},
		},
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(AppEncryption(&stubAppEncryptionService{app: app}))
	router.GET("/api/user/banner", func(c *gin.Context) {
		response.Success(c, 200, "ok", gin.H{"query": c.Request.URL.RawQuery})
	})

	clientPrivate, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate client ecdh key: %v", err)
	}
	shared, err := clientPrivate.ECDH(serverPublic)
	if err != nil {
		t.Fatalf("derive client shared key: %v", err)
	}
	sum := sha256.Sum256(shared)
	sessionKey := sum[:]

	clientCtx := mustNewClientEncryptionContext(t, appID, http.MethodGet, "/api/user/banner", AlgoHybridECDHAES256, sessionKey)
	nonce := bytes.Repeat([]byte{5}, clientCtx.stream.NonceSize())
	ciphertext, err := encryptBytes(clientCtx.stream, []byte("appid=100&page=9"), nonce, clientCtx.associatedData("request-query"))
	if err != nil {
		t.Fatalf("encrypt query: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/user/banner?_payload="+base64.RawURLEncoding.EncodeToString(ciphertext), nil)
	req.Header.Set(appEncryptionHeaderEnabled, "1")
	req.Header.Set(appEncryptionHeaderAppID, "100")
	req.Header.Set(appEncryptionHeaderNonce, base64.RawURLEncoding.EncodeToString(nonce))
	req.Header.Set(appEncryptionHeaderAlgorithm, AlgoHybridECDHAES256)
	req.Header.Set(appEncryptionHeaderKey, base64.RawURLEncoding.EncodeToString(clientPrivate.PublicKey().Bytes()))

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get(appEncryptionHeaderAlgorithm) != AlgoHybridECDHAES256 {
		t.Fatalf("unexpected response algorithm: %s", recorder.Header().Get(appEncryptionHeaderAlgorithm))
	}

	responsePlaintext := decryptRecordedResponse(t, recorder, clientCtx)
	var payload map[string]any
	if err := json.Unmarshal(responsePlaintext, &payload); err != nil {
		t.Fatalf("unmarshal plaintext response: %v", err)
	}
	data, _ := payload["data"].(map[string]any)
	if data["query"] != "appid=100&page=9" {
		t.Fatalf("unexpected query payload: %#v", data["query"])
	}
}

func TestAppEncryptionRejectsDisallowedAlgorithm(t *testing.T) {
	t.Parallel()

	const appID int64 = 100
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(AppEncryption(&stubAppEncryptionService{
		app: &appdomain.App{
			ID:     appID,
			AppKey: "app-key",
			Settings: map[string]any{
				"transportEncryption": map[string]any{
					"allowedAlgorithms": []any{AlgoAES256GCM},
				},
			},
		},
	}))
	router.GET("/api/user/banner", func(c *gin.Context) {
		response.Success(c, 200, "ok", gin.H{"ok": true})
	})

	ctx := mustNewClientEncryptionContext(t, appID, http.MethodGet, "/api/user/banner", AlgoXChaCha20, deriveAppEncryptionKey("transport-secret", appID))
	nonce := bytes.Repeat([]byte{6}, ctx.stream.NonceSize())
	ciphertext, err := encryptBytes(ctx.stream, []byte("appid=100"), nonce, ctx.associatedData("request-query"))
	if err != nil {
		t.Fatalf("encrypt query: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/user/banner?_payload="+base64.RawURLEncoding.EncodeToString(ciphertext), nil)
	req.Header.Set(appEncryptionHeaderEnabled, "1")
	req.Header.Set(appEncryptionHeaderAppID, "100")
	req.Header.Set(appEncryptionHeaderNonce, base64.RawURLEncoding.EncodeToString(nonce))
	req.Header.Set(appEncryptionHeaderAlgorithm, AlgoXChaCha20)

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", recorder.Code)
	}

	var envelope response.Envelope
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if envelope.Code != 40063 {
		t.Fatalf("unexpected error code: %d", envelope.Code)
	}
}

func decryptRecordedResponse(t *testing.T, recorder *httptest.ResponseRecorder, ctx *appEncryptionContext) []byte {
	t.Helper()

	nonce, err := base64.RawURLEncoding.DecodeString(recorder.Header().Get(appEncryptionHeaderNonce))
	if err != nil {
		t.Fatalf("decode response nonce: %v", err)
	}
	plaintext, err := decryptBytes(ctx.stream, recorder.Body.Bytes(), nonce, ctx.associatedData("response-body"))
	if err != nil {
		t.Fatalf("decrypt response body: %v", err)
	}
	return plaintext
}

func mustNewClientEncryptionContext(t *testing.T, appID int64, method string, path string, algorithm string, key []byte) *appEncryptionContext {
	t.Helper()

	symmetricAlgorithm := algorithm
	if IsHybridAlgorithm(algorithm) {
		symmetricAlgorithm = HybridSymmetricAlgorithm(algorithm)
	}
	stream, err := NewCryptoStream(symmetricAlgorithm, normalizeAppEncryptionKey(key))
	if err != nil {
		t.Fatalf("new crypto stream: %v", err)
	}
	return &appEncryptionContext{
		stream:    stream,
		appID:     appID,
		method:    strings.ToUpper(method),
		path:      path,
		algorithm: algorithm,
	}
}

func generateTestRSAKeyPair(t *testing.T) (string, string) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	privBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal rsa private key: %v", err)
	}
	pubBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal rsa public key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes}))
}

func encryptRSAHybridKey(t *testing.T, publicKeyPEM string, sessionKey []byte) []byte {
	t.Helper()

	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block == nil {
		t.Fatal("decode rsa public key pem")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatalf("parse rsa public key: %v", err)
	}
	publicKey, ok := parsed.(*rsa.PublicKey)
	if !ok {
		t.Fatal("unexpected rsa public key type")
	}
	ciphertext, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, publicKey, sessionKey, nil)
	if err != nil {
		t.Fatalf("encrypt rsa session key: %v", err)
	}
	return ciphertext
}

func generateTestECDHKeyPair(t *testing.T) (string, string, *ecdh.PublicKey) {
	t.Helper()

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ecdsa key: %v", err)
	}
	privBytes, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal ecdh private key: %v", err)
	}
	pubBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal ecdh public key: %v", err)
	}
	serverPrivate, err := privateKey.ECDH()
	if err != nil {
		t.Fatalf("convert server private key to ecdh: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubBytes})),
		string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privBytes})),
		serverPrivate.PublicKey()
}
