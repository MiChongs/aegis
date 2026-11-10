package sessiontoken

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const testSecret = "unit-test-secret-unit-test-secret"

func newCodec(t *testing.T, opts Options) *Codec {
	t.Helper()
	if opts.Issuer == "" {
		opts.Issuer = "aegis"
	}
	if opts.Secret == "" {
		opts.Secret = testSecret
	}
	c, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestIssueAndParse(t *testing.T) {
	c := newCodec(t, Options{})
	now := time.Now()
	token, err := c.Issue(TypeAccess, "jti-1", "fam-1", 10001, now, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, Prefix) || strings.Count(token, ".") != 3 {
		t.Fatalf("unexpected token shape: %s", token)
	}
	if strings.Contains(token, "eyJ") && strings.HasPrefix(token, "eyJ") {
		t.Fatal("token must not be a JWT")
	}
	claims, err := c.Parse(token, TypeAccess)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if claims.TokenID != "jti-1" || claims.FamilyID != "fam-1" || claims.AppID != 10001 || claims.Type != TypeAccess || claims.Legacy {
		t.Fatalf("claims = %+v", claims)
	}
	if got := claims.ExpiresAt.Unix(); got != now.Add(time.Hour).Unix() {
		t.Fatalf("exp = %d", got)
	}
	if app, ok := PeekAppID(token); !ok || app != 10001 {
		t.Fatalf("PeekAppID = %d %v", app, ok)
	}
}

func TestPayloadIsEncrypted(t *testing.T) {
	c := newCodec(t, Options{})
	now := time.Now()
	token, _ := c.Issue(TypeAccess, "very-identifiable-jti", "fam", 7, now, now.Add(time.Hour))
	payload := strings.Split(token, ".")[2]
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "very-identifiable-jti") || strings.Contains(string(raw), "access") {
		t.Fatal("payload leaks claims in plaintext")
	}
}

func TestTypeIsBound(t *testing.T) {
	c := newCodec(t, Options{})
	now := time.Now()
	access, _ := c.Issue(TypeAccess, "a", "f", 1, now, now.Add(time.Hour))
	refresh, _ := c.Issue(TypeRefresh, "r", "f", 1, now, now.Add(time.Hour))
	if _, err := c.Parse(access, TypeRefresh); err == nil {
		t.Fatal("access token accepted as refresh token")
	}
	if _, err := c.Parse(refresh, TypeAccess); err == nil {
		t.Fatal("refresh token accepted as access token")
	}
	if _, err := c.Parse(refresh, TypeRefresh); err != nil {
		t.Fatalf("refresh rejected: %v", err)
	}
}

func TestTamperingFails(t *testing.T) {
	c := newCodec(t, Options{})
	now := time.Now()
	token, _ := c.Issue(TypeAccess, "a", "f", 42, now, now.Add(time.Hour))
	parts := strings.Split(token, ".")

	// 改页脚里的应用 ID：页脚受认证，必须失败。
	forged := parts[0] + "." + parts[1] + "." + parts[2] + "." + base64.RawURLEncoding.EncodeToString([]byte(`{"kid":"`+c.CurrentKeyID()+`","app":43}`))
	if _, err := c.Parse(forged, TypeAccess); err == nil {
		t.Fatal("forged footer accepted")
	}
	// 翻转密文里的一个字节。
	body := []byte(parts[2])
	if body[10] == 'A' {
		body[10] = 'B'
	} else {
		body[10] = 'A'
	}
	if _, err := c.Parse(parts[0]+"."+parts[1]+"."+string(body)+"."+parts[3], TypeAccess); err == nil {
		t.Fatal("tampered payload accepted")
	}
	// 去掉页脚。
	if _, err := c.Parse(parts[0]+"."+parts[1]+"."+parts[2], TypeAccess); err == nil {
		t.Fatal("token without footer accepted")
	}
	if _, err := c.Parse("", TypeAccess); err == nil {
		t.Fatal("empty token accepted")
	}
}

func TestExpiryAndClockSkew(t *testing.T) {
	now := time.Now()
	clock := now
	c := newCodec(t, Options{Now: func() time.Time { return clock }})
	token, _ := c.Issue(TypeAccess, "a", "f", 1, now, now.Add(time.Hour))

	clock = now.Add(-30 * time.Second) // 校验方时钟比签发方慢半分钟
	if _, err := c.Parse(token, TypeAccess); err != nil {
		t.Fatalf("small clock skew rejected: %v", err)
	}
	clock = now.Add(-2 * time.Minute)
	if _, err := c.Parse(token, TypeAccess); err == nil {
		t.Fatal("token accepted before nbf")
	}
	clock = now.Add(time.Hour + time.Second)
	if _, err := c.Parse(token, TypeAccess); err == nil {
		t.Fatal("expired token accepted")
	}
}

func TestIssuerAndKeyMismatch(t *testing.T) {
	now := time.Now()
	a := newCodec(t, Options{Issuer: "aegis"})
	token, _ := a.Issue(TypeAccess, "a", "f", 1, now, now.Add(time.Hour))
	if _, err := newCodec(t, Options{Issuer: "other"}).Parse(token, TypeAccess); err == nil {
		t.Fatal("token accepted with another issuer")
	}
	if _, err := newCodec(t, Options{Secret: "another-secret-another-secret-xx"}).Parse(token, TypeAccess); err == nil {
		t.Fatal("token accepted with another key")
	}
}

func TestKeyRotation(t *testing.T) {
	now := time.Now()
	oldKey := strings.Repeat("ab", 32)
	newKey := base64.StdEncoding.EncodeToString(make([]byte, 32))

	derived := newCodec(t, Options{})
	fromDerived, _ := derived.Issue(TypeAccess, "d", "f", 1, now, now.Add(time.Hour))
	old := newCodec(t, Options{Key: oldKey})
	fromOld, _ := old.Issue(TypeAccess, "o", "f", 1, now, now.Add(time.Hour))

	rotated := newCodec(t, Options{Key: newKey, PreviousKeys: []string{oldKey, DerivedKeyName}})
	for name, token := range map[string]string{"old": fromOld, "derived": fromDerived} {
		if _, err := rotated.Parse(token, TypeAccess); err != nil {
			t.Fatalf("%s key token rejected after rotation: %v", name, err)
		}
	}
	fresh, _ := rotated.Issue(TypeAccess, "n", "f", 1, now, now.Add(time.Hour))
	if _, err := old.Parse(fresh, TypeAccess); err == nil {
		t.Fatal("new-key token accepted by a codec without the new key")
	}
	// 旧密钥移出列表以后，用它签的令牌就失效。
	if _, err := newCodec(t, Options{Key: newKey}).Parse(fromOld, TypeAccess); err == nil {
		t.Fatal("retired key still accepted")
	}
}

func TestParseKey(t *testing.T) {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i)
	}
	for _, value := range []string{
		hex.EncodeToString(raw),
		base64.StdEncoding.EncodeToString(raw),
		base64.RawURLEncoding.EncodeToString(raw),
	} {
		got, err := ParseKey(value)
		if err != nil || string(got) != string(raw) {
			t.Fatalf("ParseKey(%q) = %x, %v", value, got, err)
		}
	}
	for _, bad := range []string{"", "short", hex.EncodeToString(raw[:16]), base64.StdEncoding.EncodeToString(raw[:31])} {
		if _, err := ParseKey(bad); err == nil {
			t.Fatalf("ParseKey(%q) accepted", bad)
		}
	}
	if _, err := New(Options{Issuer: "aegis", Key: "not-a-key", Secret: testSecret}); err == nil {
		t.Fatal("New accepted a malformed key")
	}
}

func legacyJWT(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatal(err)
	}
	return signed
}

func TestLegacyJWT(t *testing.T) {
	now := time.Now()
	base := func(typ string) jwt.MapClaims {
		m := jwt.MapClaims{"appid": 9, "jti": "legacy-" + typ, "family": "fam", "iss": "aegis", "iat": now.Unix(), "exp": now.Add(time.Hour).Unix()}
		if typ != "" {
			m["typ"] = typ
		}
		return m
	}
	accepting := newCodec(t, Options{AcceptLegacyJWT: true})
	rejecting := newCodec(t, Options{})

	access := legacyJWT(t, base("access"))
	claims, err := accepting.Parse(access, TypeAccess)
	if err != nil || !claims.Legacy || claims.TokenID != "legacy-access" || claims.AppID != 9 || claims.FamilyID != "fam" {
		t.Fatalf("legacy access: %+v %v", claims, err)
	}
	if _, err := rejecting.Parse(access, TypeAccess); err == nil {
		t.Fatal("legacy JWT accepted with AcceptLegacyJWT off")
	}
	if _, err := accepting.Parse(access, TypeRefresh); err == nil {
		t.Fatal("legacy access accepted as refresh")
	}
	if _, err := accepting.Parse(legacyJWT(t, base("refresh")), TypeRefresh); err != nil {
		t.Fatalf("legacy refresh rejected: %v", err)
	}
	// 早期没有 typ 的访问令牌照旧可用。
	if _, err := accepting.Parse(legacyJWT(t, base("")), TypeAccess); err != nil {
		t.Fatalf("untyped legacy access rejected: %v", err)
	}
	// 管理员令牌同一密钥签发，不能当用户令牌用。
	admin := legacyJWT(t, jwt.MapClaims{"aid": 1, "typ": "admin", "jti": "x", "iss": "aegis", "exp": now.Add(time.Hour).Unix()})
	if _, err := accepting.Parse(admin, TypeAccess); err == nil {
		t.Fatal("admin JWT accepted as a user token")
	}
	// alg=none、过期的都不行。
	none, _ := jwt.NewWithClaims(jwt.SigningMethodNone, base("access")).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if _, err := accepting.Parse(none, TypeAccess); err == nil {
		t.Fatal("alg=none accepted")
	}
	expired := base("access")
	expired["exp"] = now.Add(-time.Minute).Unix()
	if _, err := accepting.Parse(legacyJWT(t, expired), TypeAccess); err == nil {
		t.Fatal("expired legacy JWT accepted")
	}
	if _, ok := PeekAppID(access); ok {
		t.Fatal("PeekAppID should not read JWTs")
	}
}
