package service

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net"
	"testing"

	functiondomain "aegis/internal/domain/appfunction"
)

func TestAppFunctionSandboxRejectsInvalidModule(t *testing.T) {
	t.Parallel()
	err := NewAppFunctionSandbox().Validate(context.Background(), []byte("not-wasm"))
	if err == nil {
		t.Fatal("无效 WASM 不应通过校验")
	}
}

func TestParseFunctionEndpoint(t *testing.T) {
	t.Parallel()
	for _, rawURL := range []string{
		"http://example.com/function",
		"https://localhost/function",
		"https://worker.local/function",
		"https://user:pass@example.com/function",
	} {
		if _, err := parseFunctionEndpoint(rawURL); err == nil {
			t.Fatalf("危险地址不应通过校验: %s", rawURL)
		}
	}
	if _, err := parseFunctionEndpoint("https://example.workers.dev/function"); err != nil {
		t.Fatalf("合法 Worker 地址被拒绝: %v", err)
	}
}

func TestIsPublicFunctionIP(t *testing.T) {
	t.Parallel()
	blocked := []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "192.0.2.1", "::1", "fe80::1"}
	for _, raw := range blocked {
		if isPublicFunctionIP(net.ParseIP(raw)) {
			t.Fatalf("保留地址不应被允许: %s", raw)
		}
	}
	if !isPublicFunctionIP(net.ParseIP("1.1.1.1")) {
		t.Fatal("公网地址应被允许")
	}
}

func TestVerifyFunctionResponse(t *testing.T) {
	t.Parallel()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	eventID := "4ed57288-55bd-4625-9c70-88989f95ec0b"
	body := []byte(`{"output":{"ok":true}}`)
	digest := sha256.Sum256(body)
	message := eventID + "\n" + hex.EncodeToString(digest[:])
	signature := ed25519.Sign(privateKey, []byte(message))

	if err := verifyFunctionResponse(
		base64.RawURLEncoding.EncodeToString(publicKey),
		eventID,
		body,
		base64.RawURLEncoding.EncodeToString(signature),
	); err != nil {
		t.Fatalf("合法响应签名验证失败: %v", err)
	}
	if err := verifyFunctionResponse(
		base64.RawURLEncoding.EncodeToString(publicKey),
		eventID,
		[]byte(`{"output":{"ok":false}}`),
		base64.RawURLEncoding.EncodeToString(signature),
	); err == nil {
		t.Fatal("被篡改的响应不应通过签名校验")
	}
}

func TestValidateEffectsRequiresCapability(t *testing.T) {
	t.Parallel()
	effects := []functiondomain.Effect{{
		Type:      "notification.send",
		Arguments: []byte(`{"template":"welcome"}`),
	}}
	if err := validateEffects(effects, nil); err == nil {
		t.Fatal("未授权 effect 不应通过")
	}
	if err := validateEffects(effects, []string{"notification.send"}); err != nil {
		t.Fatalf("已授权 effect 应通过: %v", err)
	}
}
