package service

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"testing"

	appdomain "aegis/internal/domain/app"

	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	webauthnlib "github.com/go-webauthn/webauthn/webauthn"
)

// 安卓原生 Passkey 的整条验签链：Credential Manager 生成的 clientDataJSON 里
// origin 是 android:apk-key-hash:<证书 SHA-256 的 base64url>，请求里没有 Origin 头。
// 这里用一个软件认证器按 WebAuthn 规范拼出注册与登录响应，走应用级依赖方的实例校验，
// 钉住三件事：RP ID 用配置值、apk-key-hash 来源被接受、别的证书签的 App 被拒绝。

const testReleaseFingerprint = "97:DB:8B:0A:BA:EC:88:77:30:25:2B:42:AA:27:82:7F:85:20:5F:07:18:D7:7B:E6:8F:90:8F:B5:AD:AD:AD:18"

type softAuthenticator struct {
	key          *ecdsa.PrivateKey
	credentialID []byte
	signCount    uint32
}

func newSoftAuthenticator(t *testing.T) *softAuthenticator {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	return &softAuthenticator{key: key, credentialID: id}
}

func b64(data []byte) string { return base64.RawURLEncoding.EncodeToString(data) }

func (a *softAuthenticator) authData(rpID string, flags byte, attested bool) []byte {
	rpHash := sha256.Sum256([]byte(rpID))
	var buf bytes.Buffer
	buf.Write(rpHash[:])
	buf.WriteByte(flags)
	_ = binary.Write(&buf, binary.BigEndian, a.signCount)
	if attested {
		buf.Write(make([]byte, 16)) // AAGUID
		_ = binary.Write(&buf, binary.BigEndian, uint16(len(a.credentialID)))
		buf.Write(a.credentialID)
		x := a.key.PublicKey.X.FillBytes(make([]byte, 32))
		y := a.key.PublicKey.Y.FillBytes(make([]byte, 32))
		cose, _ := webauthncbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: x, -3: y})
		buf.Write(cose)
	}
	return buf.Bytes()
}

func clientData(t *testing.T, ceremony, challenge, origin string) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"type": ceremony, "challenge": challenge, "origin": origin, "crossOrigin": false})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func (a *softAuthenticator) register(t *testing.T, rpID, challenge, origin string) []byte {
	t.Helper()
	authData := a.authData(rpID, 0x45, true) // UP | UV | AT
	attestation, err := webauthncbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": authData})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"id": b64(a.credentialID), "rawId": b64(a.credentialID), "type": "public-key",
		"response": map[string]any{
			"clientDataJSON":    b64(clientData(t, "webauthn.create", challenge, origin)),
			"attestationObject": b64(attestation),
		},
		"clientExtensionResults": map[string]any{},
	})
	return body
}

func (a *softAuthenticator) assert(t *testing.T, rpID, challenge, origin string, userHandle []byte) []byte {
	t.Helper()
	return a.assertWithFlags(t, rpID, challenge, origin, userHandle, 0x05) // UP | UV
}

// assertWithFlags 同 assert，可指定认证器数据的标志位（例如只有 UP、没有 UV 的 0x01）。
func (a *softAuthenticator) assertWithFlags(t *testing.T, rpID, challenge, origin string, userHandle []byte, flags byte) []byte {
	t.Helper()
	a.signCount++
	authData := a.authData(rpID, flags, false)
	cdj := clientData(t, "webauthn.get", challenge, origin)
	cdjHash := sha256.Sum256(cdj)
	digest := sha256.Sum256(append(append([]byte{}, authData...), cdjHash[:]...))
	signature, err := ecdsa.SignASN1(rand.Reader, a.key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"id": b64(a.credentialID), "rawId": b64(a.credentialID), "type": "public-key",
		"response": map[string]any{
			"clientDataJSON":    b64(cdj),
			"authenticatorData": b64(authData),
			"signature":         b64(signature),
			"userHandle":        b64(userHandle),
		},
		"clientExtensionResults": map[string]any{},
	})
	return body
}

type testPasskeyUser struct {
	id          []byte
	credentials []webauthnlib.Credential
}

func (u *testPasskeyUser) WebAuthnID() []byte                            { return u.id }
func (u *testPasskeyUser) WebAuthnName() string                          { return "voyager" }
func (u *testPasskeyUser) WebAuthnDisplayName() string                   { return "Voyager" }
func (u *testPasskeyUser) WebAuthnCredentials() []webauthnlib.Credential { return u.credentials }

func jsonRequest(t *testing.T, body []byte) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestAndroidPasskeyCeremonyWithAppRelyingParty(t *testing.T) {
	policy, err := appdomain.NormalizePasskeyPolicy(appdomain.PasskeyPolicy{
		Enabled: true, RPID: "voyage.example.com", RPName: "Voyage",
		Origins: []string{"https://voyage.example.com"},
		Android: []appdomain.AndroidPasskeyApp{{PackageName: "com.example.voyage", SHA256CertFingerprints: []string{testReleaseFingerprint}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	androidOrigin := appdomain.AndroidApkKeyHashOrigin(testReleaseFingerprint)

	svc := &SecurityService{}
	instance, err := svc.buildAppPasskeyWebAuthn(policy, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	user := &testPasskeyUser{id: []byte("user-handle-1")}
	auth := newSoftAuthenticator(t)

	creation, regSession, err := instance.BeginRegistration(user)
	if err != nil {
		t.Fatal(err)
	}
	if creation.Response.RelyingParty.ID != "voyage.example.com" || regSession.RelyingPartyID != "voyage.example.com" {
		t.Fatalf("RP ID 应为配置值，得到 %q / %q", creation.Response.RelyingParty.ID, regSession.RelyingPartyID)
	}

	// Finish 阶段按 Begin 下发的 RP ID 重新造实例，与服务里的走法一致
	finishInstance, err := svc.buildAppPasskeyWebAuthn(policy, nil, regSession.RelyingPartyID)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := finishInstance.FinishRegistration(user, *regSession,
		jsonRequest(t, auth.register(t, "voyage.example.com", creation.Response.Challenge.String(), androidOrigin)))
	if err != nil {
		t.Fatalf("安卓来源的注册应当通过：%s", passkeyFailureDetail(err))
	}
	user.credentials = []webauthnlib.Credential{*credential}

	assertion, loginSession, err := instance.BeginDiscoverableLogin()
	if err != nil {
		t.Fatal(err)
	}
	handler := func(rawID, userHandle []byte) (webauthnlib.User, error) { return user, nil }
	_, _, err = finishInstance.FinishPasskeyLogin(handler, *loginSession,
		jsonRequest(t, auth.assert(t, "voyage.example.com", assertion.Response.Challenge.String(), androidOrigin, user.id)))
	if err != nil {
		t.Fatalf("安卓来源的登录应当通过：%s", passkeyFailureDetail(err))
	}

	// 别的证书签的 App（重打包、冒用包名）推出的来源不同，必须被拒
	other := appdomain.AndroidApkKeyHashOrigin("00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF")
	_, again, _ := instance.BeginDiscoverableLogin()
	_, _, err = finishInstance.FinishPasskeyLogin(handler, *again,
		jsonRequest(t, auth.assert(t, "voyage.example.com", again.Challenge, other, user.id)))
	if err == nil {
		t.Fatal("未登记证书的来源不应通过")
	}

	// RP ID 改过之后，老挑战不能再拿新配置去凑
	changed := policy
	changed.RPID = "other.example.com"
	if _, err := svc.buildAppPasskeyWebAuthn(changed, nil, regSession.RelyingPartyID); err == nil {
		t.Fatal("RP ID 变更后应拒绝老挑战")
	}
}

func TestAndroidApkKeyHashOrigin(t *testing.T) {
	// 由 keytool 输出的 SHA-256 推出的值，与 Credential Manager 写进 clientDataJSON 的一致
	got := appdomain.AndroidApkKeyHashOrigin(testReleaseFingerprint)
	want := "android:apk-key-hash:l9uLCrrsiHcwJStCqieCf4UgXwcY13vmj5CPta2trRg"
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	if appdomain.AndroidApkKeyHashOrigin(" 97db8b0abaec887730252b42aa27827f85205f0718d77be68f908fb5adadad18 ") != want {
		t.Fatal("无分隔小写十六进制应得到同一来源")
	}
}

func TestNormalizePasskeyPolicy(t *testing.T) {
	cases := []struct {
		name   string
		policy appdomain.PasskeyPolicy
		ok     bool
	}{
		{"网页来源必须落在 RP ID 下", appdomain.PasskeyPolicy{Enabled: true, RPID: "example.com", Origins: []string{"https://evil.com"}}, false},
		{"子域来源可以", appdomain.PasskeyPolicy{Enabled: true, RPID: "example.com", Origins: []string{"https://app.example.com"}}, true},
		{"非 https 拒绝", appdomain.PasskeyPolicy{Enabled: true, RPID: "example.com", Origins: []string{"http://example.com"}}, false},
		{"RP ID 不能带协议", appdomain.PasskeyPolicy{Enabled: true, RPID: "https://example.com", Origins: []string{"https://example.com"}}, false},
		{"安卓包要有指纹", appdomain.PasskeyPolicy{Enabled: true, RPID: "example.com", Android: []appdomain.AndroidPasskeyApp{{PackageName: "com.a.b"}}}, false},
		{"指纹长度不对", appdomain.PasskeyPolicy{Enabled: true, RPID: "example.com", Android: []appdomain.AndroidPasskeyApp{{PackageName: "com.a.b", SHA256CertFingerprints: []string{"AB:CD"}}}}, false},
		{"有来源没 RP ID", appdomain.PasskeyPolicy{Enabled: true, Origins: []string{"https://example.com"}}, false},
		{"关闭且留空", appdomain.PasskeyPolicy{Enabled: false}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := appdomain.NormalizePasskeyPolicy(tc.policy)
			if (err == nil) != tc.ok {
				t.Fatalf("ok=%v err=%v", tc.ok, err)
			}
		})
	}

	// 写回设置再读出来不变
	policy, _ := appdomain.NormalizePasskeyPolicy(appdomain.PasskeyPolicy{
		Enabled: true, RPID: "Example.COM.", Origins: []string{"https://example.com/"},
		Android: []appdomain.AndroidPasskeyApp{{PackageName: "com.a.b", SHA256CertFingerprints: []string{testReleaseFingerprint, testReleaseFingerprint}}},
	})
	roundTrip := appdomain.PasskeyPolicyFromSettings(map[string]any{appdomain.PasskeySettingsKey: jsonRoundTrip(t, policy.ToMap())})
	if roundTrip.RPID != "example.com" || len(roundTrip.Android) != 1 || len(roundTrip.Android[0].SHA256CertFingerprints) != 1 || !roundTrip.Configured() {
		t.Fatalf("round trip mismatch: %+v", roundTrip)
	}
	links := roundTrip.AssetLinks()
	if len(links) != 1 {
		t.Fatalf("assetlinks: %+v", links)
	}
}

// jsonRoundTrip 模拟 apps.settings 从 JSONB 读回来的形状（[]any / map[string]any）。
func jsonRoundTrip(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(value)
	out := map[string]any{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
