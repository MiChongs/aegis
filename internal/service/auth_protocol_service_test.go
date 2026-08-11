package service

import (
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"testing"

	authprotocol "aegis/internal/domain/authprotocol"

	"golang.org/x/crypto/chacha20poly1305"
)

func TestValidateAuthProtocolPolicySecurityInvariants(t *testing.T) {
	policy := defaultAuthProtocolPolicy(42)
	if err := validateAuthProtocolPolicy(policy); err != nil {
		t.Fatalf("default policy must be valid: %v", err)
	}
	if policy.SecurityLevel != authprotocol.LevelStandard {
		t.Fatalf("新应用必须默认落在 standard 档，实际 %q", policy.SecurityLevel)
	}

	// 关掉旧接口不再需要任何互锁前置条件：它与新协议等级正交。
	policy.AllowLegacy = false
	if err := validateAuthProtocolPolicy(policy); err != nil {
		t.Fatalf("关闭旧接口不应被拒绝: %v", err)
	}

	policy.SecurityLevel = "paranoid"
	if err := validateAuthProtocolPolicy(policy); err == nil {
		t.Fatal("未知安全等级必须被拒绝")
	}

	policy.SecurityLevel = authprotocol.LevelSealed
	policy.RegistrationSchema = append(policy.RegistrationSchema, authprotocol.RegistrationField{
		Name: "register_ip", Type: "text", Mutable: true,
	})
	if err := validateAuthProtocolPolicy(policy); err == nil {
		t.Fatal("reserved registration fields must be rejected")
	}
}

func TestValidateAuthProtocolPolicyAuthMethods(t *testing.T) {
	base := func() *authprotocol.Policy {
		policy := defaultAuthProtocolPolicy(42)
		policy.Identifiers = []string{"username", "email", "phone"}
		return policy
	}

	// 三种登录方式都必须放行，否则控制台勾了也存不下去。
	full := base()
	full.LoginMethods = []string{
		authprotocol.MethodPassword, authprotocol.MethodSMS, authprotocol.MethodOAuth,
	}
	full.RegisterMethods = []string{authprotocol.MethodPassword, authprotocol.MethodSMS}
	if err := validateAuthProtocolPolicy(full); err != nil {
		t.Fatalf("password/sms/oauth 登录 + password/sms 注册应当合法: %v", err)
	}

	// oauth 不是注册方式：自动建号由渠道级 allowRegister 决定，不能有第二处开关。
	oauthRegister := base()
	oauthRegister.RegisterMethods = []string{authprotocol.MethodOAuth}
	if err := validateAuthProtocolPolicy(oauthRegister); err == nil {
		t.Fatal("oauth 不应作为注册方式被接受")
	}

	// 短信认证以手机号为身份，标识符缺 phone 时客户端拿不到可用入口。
	smsWithoutPhone := base()
	smsWithoutPhone.Identifiers = []string{"username", "email"}
	smsWithoutPhone.LoginMethods = []string{authprotocol.MethodSMS}
	if err := validateAuthProtocolPolicy(smsWithoutPhone); err == nil {
		t.Fatal("启用短信登录但标识符缺少 phone 时必须被拒绝")
	}

	unknown := base()
	unknown.LoginMethods = []string{"magic_link"}
	if err := validateAuthProtocolPolicy(unknown); err == nil {
		t.Fatal("未实现的登录方式必须被拒绝")
	}
}

func TestSignRequestBindsEveryCanonicalField(t *testing.T) {
	const secret = "sk_test_secret_value"
	base := authprotocol.SignatureMetadata{
		AppKey: "demo_app", Method: http.MethodPost,
		Path: "/api/v1/apps/demo_app/auth/login", Query: "page=1&pageSize=20",
		Timestamp: "1716175200",
		Nonce:     "nonce-1", Body: []byte(`{"account":"alice"}`),
	}
	signature := SignRequest(secret, base)
	if !strings.HasPrefix(signature, authprotocol.SignaturePrefixV2) {
		t.Fatalf("签名必须带 v2 版本前缀，实际 %q", signature)
	}
	if SignRequest(secret, base) != signature {
		t.Fatal("同样的输入必须得到同样的签名")
	}

	// 逐个字段扰动：任何一处变化都必须改变签名，否则该字段没有真正被绑定。
	mutations := map[string]func(m *authprotocol.SignatureMetadata){
		"appKey":    func(m *authprotocol.SignatureMetadata) { m.AppKey = "other_app" },
		"method":    func(m *authprotocol.SignatureMetadata) { m.Method = http.MethodGet },
		"path":      func(m *authprotocol.SignatureMetadata) { m.Path = "/api/v1/apps/demo_app/auth/register" },
		"timestamp": func(m *authprotocol.SignatureMetadata) { m.Timestamp = "1716175201" },
		"nonce":     func(m *authprotocol.SignatureMetadata) { m.Nonce = "nonce-2" },
		"body":      func(m *authprotocol.SignatureMetadata) { m.Body = []byte(`{"account":"bob"}`) },
		// query 是 v2 相对 v1 唯一新增的一行：漏了它，`?page=1` 就能被改成
		// `?page=999` 而签名照过。分页与筛选类接口铺开后这是必守的一条。
		"query": func(m *authprotocol.SignatureMetadata) { m.Query = "page=999&pageSize=20" },
	}
	for name, mutate := range mutations {
		altered := base
		mutate(&altered)
		if SignRequest(secret, altered) == signature {
			t.Fatalf("字段 %s 未被纳入签名", name)
		}
	}
	if SignRequest("sk_another_secret", base) == signature {
		t.Fatal("不同密钥必须产出不同签名")
	}
}

// v1 签名不覆盖 query string，因此只允许出现在没有 query 的请求上。
// 放宽这条等于把「改分页 / 改目标 ID 不破坏签名」的洞永久留着。
func TestSignatureVersionRejectsLegacyPrefixWhenQueryPresent(t *testing.T) {
	withQuery := authprotocol.SignatureMetadata{
		Signature: authprotocol.SignaturePrefix + strings.Repeat("ab", 32),
		Query:     "page=1",
	}
	if _, _, err := splitSignatureVersion(withQuery); err == nil {
		t.Fatal("带 query 的 v1 签名必须被拒绝")
	}

	withoutQuery := withQuery
	withoutQuery.Query = ""
	version, digest, err := splitSignatureVersion(withoutQuery)
	if err != nil {
		t.Fatalf("无 query 的 v1 签名应继续被接受：%v", err)
	}
	if version != authprotocol.SignaturePrefix || digest != strings.Repeat("ab", 32) {
		t.Fatalf("v1 拆解结果不正确：%q / %q", version, digest)
	}

	v2 := authprotocol.SignatureMetadata{
		Signature: authprotocol.SignaturePrefixV2 + strings.Repeat("cd", 32),
		Query:     "page=1",
	}
	if version, _, err := splitSignatureVersion(v2); err != nil || version != authprotocol.SignaturePrefixV2 {
		t.Fatalf("v2 签名应被接受，实际 version=%q err=%v", version, err)
	}
}

func TestDeriveTransportKeyMatchesBothX25519Peers(t *testing.T) {
	curve := ecdh.X25519()
	serverPrivate, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientPrivate, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serverShared, err := serverPrivate.ECDH(clientPrivate.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	clientShared, err := clientPrivate.ECDH(serverPrivate.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	aad := []byte("aegis-transport-test")
	serverKey, err := deriveTransportKey(serverShared, "demo_app", "atk_test", aad)
	if err != nil {
		t.Fatal(err)
	}
	clientKey, err := deriveTransportKey(clientShared, "demo_app", "atk_test", aad)
	if err != nil {
		t.Fatal(err)
	}
	if string(serverKey) != string(clientKey) {
		t.Fatal("both peers must derive the same key")
	}
	// 盐里绑定了 appKey：换一个应用就必须派生出不同的密钥。
	otherKey, err := deriveTransportKey(clientShared, "other_app", "atk_test", aad)
	if err != nil {
		t.Fatal(err)
	}
	if string(otherKey) == string(clientKey) {
		t.Fatal("不同 appKey 必须派生出不同的传输密钥")
	}
}

func TestOpenTransportRequestPayloadRoundTripAndAADBinding(t *testing.T) {
	curve := ecdh.X25519()
	serverPrivate, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientPrivate, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	clientShared, err := clientPrivate.ECDH(serverPrivate.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	serverShared, err := serverPrivate.ECDH(clientPrivate.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	meta := authprotocol.RequestMetadata{
		AppKey: "demo", KeyID: "atk_test", Method: "POST",
		Path:      "/api/v1/apps/demo/auth/login",
		Timestamp: "1716175200", Nonce: "request_nonce",
	}
	aad := transportRequestAAD(meta)
	clientKey, err := deriveTransportKey(clientShared, meta.AppKey, meta.KeyID, aad)
	if err != nil {
		t.Fatal(err)
	}
	aead, err := chacha20poly1305.NewX(clientKey)
	if err != nil {
		t.Fatal(err)
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatal(err)
	}
	expected := []byte(`{"account":"alice","password":"secret"}`)
	encoded := []byte(base64.RawURLEncoding.EncodeToString(aead.Seal(nil, nonce, expected, aad)))
	plaintext, serverKey, _, err := openTransportRequestPayload(serverShared, meta.AppKey, meta, nonce, encoded)
	if err != nil {
		t.Fatal(err)
	}
	if string(plaintext) != string(expected) || string(serverKey) != string(clientKey) {
		t.Fatal("server did not recover the client payload and key")
	}

	tampered := meta
	tampered.Path = "/api/v1/apps/demo/auth/register"
	if _, _, _, err := openTransportRequestPayload(serverShared, meta.AppKey, tampered, nonce, encoded); err == nil {
		t.Fatal("ciphertext must not authenticate for a different route")
	}
}

func TestSealResponseCanOnlyOpenWithBoundAAD(t *testing.T) {
	requestKey := make([]byte, chacha20poly1305.KeySize)
	if _, err := rand.Read(requestKey); err != nil {
		t.Fatal(err)
	}
	requestNonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(requestNonce); err != nil {
		t.Fatal(err)
	}
	cryptoContext := &authprotocol.CryptoContext{
		Key: requestKey, AppKey: "app_public", KeyID: "atk_test", RequestNonce: requestNonce,
	}
	service := NewAuthProtocolService(nil, nil, nil, "stable-master-key")
	sealedEncoded, responseNonceEncoded, err := service.SealResponse(cryptoContext, 200, []byte(`{"ok":true}`))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := base64.RawURLEncoding.DecodeString(string(sealedEncoded))
	if err != nil {
		t.Fatal(err)
	}
	responseNonce, err := base64.RawURLEncoding.DecodeString(responseNonceEncoded)
	if err != nil {
		t.Fatal(err)
	}
	responseKey := sha256.Sum256(append(append([]byte(nil), requestKey...), []byte("aegis-response-v2")...))
	aead, err := chacha20poly1305.NewX(responseKey[:])
	if err != nil {
		t.Fatal(err)
	}
	aad := []byte(fmt.Sprintf("%s\n%s\n%s\n%d\n%s\n%s",
		authprotocol.TransportV2, cryptoContext.AppKey, cryptoContext.KeyID, 200,
		base64.RawURLEncoding.EncodeToString(requestNonce), responseNonceEncoded))
	plaintext, err := aead.Open(nil, responseNonce, sealed, aad)
	if err != nil {
		t.Fatal(err)
	}
	if string(plaintext) != `{"ok":true}` {
		t.Fatalf("unexpected plaintext: %s", plaintext)
	}
	if _, err := aead.Open(nil, responseNonce, sealed, append(aad, '!')); err == nil {
		t.Fatal("tampered response AAD must fail authentication")
	}
}
