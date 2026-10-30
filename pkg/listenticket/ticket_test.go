package listenticket

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func testSigner(t *testing.T) *Signer {
	t.Helper()
	seed, err := DeriveSeed("unit-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	key, err := KeyFromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := NewSigner([]Key{key}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func TestIssueAndParseRoundTrip(t *testing.T) {
	signer := testSigner(t)
	raw, err := json.Marshal(signer.JWKS())
	if err != nil {
		t.Fatal(err)
	}
	keys, err := ParseJWKS(raw)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	token, expires, err := signer.Issue(Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "1234567890123456"},
		AppKey:           "voyage", Nickname: "小明", Features: []string{Feature},
	}, now, "jti-1")
	if err != nil {
		t.Fatal(err)
	}
	if !expires.After(now) {
		t.Fatalf("expires %v not after now", expires)
	}
	claims, err := Parse(token, StaticKeyfunc(keys), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if claims.UserID() != 1234567890123456 || claims.AppKey != "voyage" || claims.Nickname != "小明" {
		t.Fatalf("unexpected claims %+v", claims)
	}
}

func TestParseRejectsMissingFeatureAndForeignKey(t *testing.T) {
	signer := testSigner(t)
	keys, _ := ParseJWKS(mustJSON(t, signer.JWKS()))
	token, _, err := signer.Issue(Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "42"}, AppKey: "voyage",
	}, time.Now(), "jti-2")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(token, StaticKeyfunc(keys), 0); !errors.Is(err, ErrMissingFeature) {
		t.Fatalf("want ErrMissingFeature, got %v", err)
	}

	other, _ := KeyFromSeed(make([]byte, 32))
	foreign, _ := NewSigner([]Key{other}, time.Minute)
	token, _, _ = foreign.Issue(Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "42"}, AppKey: "voyage", Features: []string{Feature},
	}, time.Now(), "jti-3")
	if _, err := Parse(token, StaticKeyfunc(keys), 0); err == nil {
		t.Fatal("token signed by an unknown key must be rejected")
	}
}

func TestParseRejectsExpired(t *testing.T) {
	signer := testSigner(t)
	keys, _ := ParseJWKS(mustJSON(t, signer.JWKS()))
	token, _, _ := signer.Issue(Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "42"}, AppKey: "voyage", Features: []string{Feature},
	}, time.Now().Add(-time.Hour), "jti-4")
	if _, err := Parse(token, StaticKeyfunc(keys), time.Second); err == nil {
		t.Fatal("expired token must be rejected")
	}
}

func TestParseSeedsAcceptsBothAlphabets(t *testing.T) {
	keys, err := ParseSeeds("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=, __________________________________________8")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || keys[0].ID == keys[1].ID {
		t.Fatalf("unexpected keys %+v", keys)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
