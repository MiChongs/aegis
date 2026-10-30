// Package ticket 验证主服务签发的一起听票据。
package ticket

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"time"

	"github.com/MicahParks/keyfunc/v3"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/time/rate"

	"aegis/pkg/listenticket"
)

// Verifier 验票器。JWKS 地址模式下由 keyfunc 在后台按间隔刷新公钥，
// 遇到不认识的 kid 时（主服务刚轮换了密钥）也会立刻补拉一次（有限流）。
type Verifier struct {
	keyfunc jwt.Keyfunc
	leeway  time.Duration
	ready   func() bool
}

// NewFromURL 从主服务的 JWKS 地址取公钥。
func NewFromURL(ctx context.Context, url string, leeway time.Duration) (*Verifier, error) {
	kf, err := keyfunc.NewDefaultOverrideCtx(ctx, []string{url}, keyfunc.Override{
		RefreshInterval:   10 * time.Minute,
		RateLimitWaitMax:  time.Minute,
		RefreshUnknownKID: rate.NewLimiter(rate.Every(30*time.Second), 1),
	})
	if err != nil {
		return nil, fmt.Errorf("load listen ticket jwks: %w", err)
	}
	v := &Verifier{keyfunc: kf.Keyfunc, leeway: leeway}
	v.ready = func() bool {
		keys, err := kf.Storage().KeyReadAll(context.Background())
		return err == nil && len(keys) > 0
	}
	return v, nil
}

// NewFromJWKS 用固定的 JWKS 文本（离线部署、测试）。
func NewFromJWKS(raw string, leeway time.Duration) (*Verifier, error) {
	keys, err := listenticket.ParseJWKS([]byte(raw))
	if err != nil {
		return nil, err
	}
	return NewStatic(keys, leeway), nil
}

// NewStatic 用固定公钥表。
func NewStatic(keys map[string]ed25519.PublicKey, leeway time.Duration) *Verifier {
	return &Verifier{keyfunc: listenticket.StaticKeyfunc(keys), leeway: leeway, ready: func() bool { return len(keys) > 0 }}
}

// Verify 验一张票据。
func (v *Verifier) Verify(raw string) (*listenticket.Claims, error) {
	if raw == "" {
		return nil, errors.New("empty ticket")
	}
	return listenticket.Parse(raw, v.keyfunc, v.leeway)
}

// Ready 是否已经拿到公钥（就绪探针用）。
func (v *Verifier) Ready() bool { return v.ready == nil || v.ready() }
