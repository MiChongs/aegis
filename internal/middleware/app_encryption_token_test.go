package middleware

import (
	"testing"
	"time"

	"aegis/pkg/sessiontoken"
	"github.com/golang-jwt/jwt/v5"
)

// 加密传输要在解密请求体之前知道应用：新的 PASETO 令牌从页脚读，过渡期的旧 JWT 从载荷读。
func TestBearerAppID(t *testing.T) {
	codec, err := sessiontoken.New(sessiontoken.Options{Issuer: "aegis", Secret: "middleware-test-secret-0123456789"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	token, err := codec.Issue(sessiontoken.TypeAccess, "jti", "fam", 10086, now, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if app, ok := bearerAppID("Bearer " + token); !ok || app != 10086 {
		t.Fatalf("paseto app id = %d %v", app, ok)
	}
	legacy, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"appid": 77}).SignedString([]byte("x"))
	if app, ok := bearerAppID("Bearer " + legacy); !ok || app != 77 {
		t.Fatalf("legacy app id = %d %v", app, ok)
	}
	for _, header := range []string{"", "Bearer ", "Bearer v4.local.abc", "Basic abc"} {
		if _, ok := bearerAppID(header); ok {
			t.Fatalf("bearerAppID(%q) should fail", header)
		}
	}
}
