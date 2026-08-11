package service

import (
	"net/http"
	"strings"
	"testing"

	authprotocol "aegis/internal/domain/authprotocol"
)

// 包装规格的**跨语言锚点**。
//
// 这里断言的是逐字节的字面量，Kotlin SDK 的
// sdk/kotlin/src/test/kotlin/dev/aegis/sdk/AegisCanonicalTest.kt 里有一份一模一样的。
// 两边钉住同一串字节，任何一方改了拼接方式都会当场红掉 —— 而不是等接入方
// 在生产上撞见 40175，再花半天去数换行符。
//
// 改协议的正确姿势：先改这两个测试里的字面量，两边同时变绿，才算改完。
// 第三处（控制台示例 aegis-console/src/lib/integration-snippets.ts）由
// 「接入自检」实跑兜住。

const (
	anchorAppKey    = "demo_app"
	anchorKeyID     = "atk_11111111-2222-3333-4444-555555555555"
	anchorPath      = "/api/v1/apps/demo_app/auth/login"
	anchorTimestamp = "1716175200"
	anchorBody      = `{"account":"alice"}`
	// 空请求体的 SHA-256 也要参与签名，不能跳过。
	emptyBodyDigest = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	aliceBodyDigest = "ebdd4c28e7af5634fac89a3b251466a49b62b50c4dce0df05ac98886934ad1ec"
)

func TestSignatureCanonicalBytesAreStable(t *testing.T) {
	meta := authprotocol.SignatureMetadata{
		AppKey: anchorAppKey, Method: "post", Path: anchorPath,
		Query: "page=1&pageSize=20", Timestamp: anchorTimestamp,
		Nonce: "nonce-1", Body: []byte(anchorBody),
	}
	want := strings.Join([]string{
		"aegis-hmac-sha256",
		"demo_app",
		"POST",
		"/api/v1/apps/demo_app/auth/login",
		"page=1&pageSize=20",
		"1716175200",
		"nonce-1",
		aliceBodyDigest,
	}, "\n")
	assertCanonical(t, want, signatureCanonicalString(meta, authprotocol.SignaturePrefixV2), 8)
}

// 没有 query 时那一行是**空行**，不是省略 —— 少一行签名就全错。
func TestSignatureCanonicalKeepsEmptyQueryLine(t *testing.T) {
	meta := authprotocol.SignatureMetadata{
		AppKey: anchorAppKey, Method: http.MethodGet, Path: "/api/v1/apps/demo_app/me",
		Query: "", Timestamp: anchorTimestamp, Nonce: "nonce-1",
	}
	want := strings.Join([]string{
		"aegis-hmac-sha256",
		"demo_app",
		"GET",
		"/api/v1/apps/demo_app/me",
		"",
		"1716175200",
		"nonce-1",
		emptyBodyDigest,
	}, "\n")
	assertCanonical(t, want, signatureCanonicalString(meta, authprotocol.SignaturePrefixV2), 8)
}

// v1 少 query 那一行。留着它是为了老客户端，而不是给新接入用的。
func TestLegacySignatureCanonicalHasNoQueryLine(t *testing.T) {
	meta := authprotocol.SignatureMetadata{
		AppKey: anchorAppKey, Method: http.MethodPost, Path: anchorPath,
		Timestamp: anchorTimestamp, Nonce: "nonce-1", Body: []byte(anchorBody),
	}
	want := strings.Join([]string{
		"aegis-hmac-sha256", "demo_app", "POST",
		"/api/v1/apps/demo_app/auth/login", "1716175200", "nonce-1", aliceBodyDigest,
	}, "\n")
	assertCanonical(t, want, signatureCanonicalString(meta, authprotocol.SignaturePrefix), 7)
}

func TestTransportRequestAADBytesAreStable(t *testing.T) {
	aad := string(transportRequestAAD(authprotocol.RequestMetadata{
		AppKey: anchorAppKey, KeyID: anchorKeyID, Method: "post", Path: anchorPath,
		Timestamp: anchorTimestamp, Nonce: "cmVxdWVzdC1ub25jZS0yNGJ5dGVzISE",
	}))
	want := strings.Join([]string{
		"aegis-transport-v2",
		"demo_app",
		anchorKeyID,
		"POST",
		"/api/v1/apps/demo_app/auth/login",
		"1716175200",
		"cmVxdWVzdC1ub25jZS0yNGJ5dGVzISE",
	}, "\n")
	assertCanonical(t, want, aad, 7)

	// 请求 AAD **不含 query**：无请求体的方法把密文放在 query 里，
	// 把 query 算进 AAD 会变成「要加密得先知道密文」的死循环。
	if strings.Contains(aad, "page=") {
		t.Fatal("请求 AAD 不应包含 query string")
	}
}

func TestTransportResponseAADBytesAreStable(t *testing.T) {
	aad := string(transportResponseAAD(anchorAppKey, anchorKeyID, http.StatusOK,
		"cmVxdWVzdC1ub25jZQ", "cmVzcG9uc2Utbm9uY2U"))
	want := strings.Join([]string{
		"aegis-transport-v2",
		"demo_app",
		anchorKeyID,
		"200",
		"cmVxdWVzdC1ub25jZQ",
		"cmVzcG9uc2Utbm9uY2U",
	}, "\n")
	assertCanonical(t, want, aad, 6)
}

func assertCanonical(t *testing.T, want, got string, lines int) {
	t.Helper()
	if got != want {
		t.Fatalf("逐字节不一致：\n期望：\n%q\n实际：\n%q", want, got)
	}
	if count := len(strings.Split(got, "\n")); count != lines {
		t.Fatalf("应为 %d 行，实际 %d 行", lines, count)
	}
	if strings.HasSuffix(got, "\n") {
		t.Fatal("末尾不能有换行")
	}
}
