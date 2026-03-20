package middleware

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
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

	ctx, err := newAppEncryptionContext("transport-secret", appID, http.MethodGet, "/api/user/banner")
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

	ctx, err := newAppEncryptionContext("transport-secret", appID, http.MethodPost, "/api/auth/login/password")
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
