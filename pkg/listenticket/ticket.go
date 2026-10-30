// Package listenticket 一起听的接入票据：Aegis 主服务签发、一起听微服务验证。
//
// ── 为什么是票据而不是让微服务回头问主服务 ──
//
// 一起听是独立部署的微服务（services/listentogether）。用户连它之前先找主服务换一张票据：
// 主服务在那一刻判过「已登录、令牌属于这个应用、拥有 listen_together 权益」，
// 把结论连同昵称头像签成一张短期 JWT（EdDSA / Ed25519）。微服务只拿公钥离线验签，
// 运行时不依赖主服务 —— 主服务重启或繁忙不影响正在一起听的人，一起听的流量也不经过主服务。
//
// 票据很短命（默认 10 分钟）。客户端在到期前回主服务换新票据并在连接上续期；
// 失去权益、被封禁、被强制下线的人换不到新票据，最迟一个有效期后被断开。
// 封禁与强制下线另外经 NATS 立刻通知（SubjectRevoke），不必等票据过期。
//
// ── 密钥 ──
//
// 签名私钥由主服务持有。公钥以 JWKS 发布（主服务 GET /api/listen-together/jwks.json），
// 微服务按 kid 取公钥并定期刷新，因此可以无缝轮换：新钥签发、旧钥在 JWKS 里多留一个有效期。
//
// 用户 ID 一律是对外的 16 位 ID（users.public_id）：票据对客户端可读，主键不能出服务端。
package listenticket

import (
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// Issuer 票据签发方。
	Issuer = "aegis"
	// Audience 票据只给一起听服务用，拿去别处验不过。
	Audience = "listen-together"
	// Feature 一起听的会员权益标识（会员功能目录里要建同名的功能并加进套餐）。
	Feature = "listen_together"
	// DefaultTTL 票据默认有效期。
	DefaultTTL = 10 * time.Minute

	// SubjectRevoke 主服务通知一起听断开某人（NATS，JSON 为 Revoke）。
	SubjectRevoke = "listen.control.revoke"
)

// Claims 票据内容。字段只增不改：老服务忽略不认识的字段。
type Claims struct {
	jwt.RegisteredClaims
	// AppKey 票据所属应用；房间按应用隔离。
	AppKey string `json:"app"`
	// Nickname / Avatar 房间里给别人看的资料，签进票据免得微服务回头查。
	Nickname string `json:"nick,omitempty"`
	Avatar   string `json:"avt,omitempty"`
	// Device 签发时会话绑定的设备。
	Device string `json:"dev,omitempty"`
	// Features 主服务判过的权益（至少包含 listen_together）。
	Features []string `json:"ent,omitempty"`
	// Lifetime 是否永久会员（只用于展示）。
	Lifetime bool `json:"ltm,omitempty"`
}

// UserID 票据主体，即对外用户 ID。
func (c *Claims) UserID() int64 {
	id, err := strconv.ParseInt(c.Subject, 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

// HasFeature 票据是否带某项权益。
func (c *Claims) HasFeature(feature string) bool {
	for _, item := range c.Features {
		if strings.EqualFold(item, feature) {
			return true
		}
	}
	return false
}

// Revoke 经 NATS 发给一起听服务的断开通知。
type Revoke struct {
	AppKey string `json:"appKey"`
	UserID int64  `json:"userId"`
	Reason string `json:"reason,omitempty"`
}

// Key 一把 Ed25519 签名密钥。
type Key struct {
	ID      string
	Private ed25519.PrivateKey
	Public  ed25519.PublicKey
}

// KeyFromSeed 由 32 字节种子得到密钥；kid 取公钥 SHA-256 的前 12 字节（base64url），
// 同一把钥匙在每个实例上算出的 kid 都相同。
func KeyFromSeed(seed []byte) (Key, error) {
	if len(seed) != ed25519.SeedSize {
		return Key{}, fmt.Errorf("ed25519 seed must be %d bytes, got %d", ed25519.SeedSize, len(seed))
	}
	private := ed25519.NewKeyFromSeed(seed)
	public := private.Public().(ed25519.PublicKey)
	sum := sha256.Sum256(public)
	return Key{ID: base64.RawURLEncoding.EncodeToString(sum[:12]), Private: private, Public: public}, nil
}

// ParseSeeds 解析配置里的种子列表（逗号分隔，标准或 URL 安全 base64 均可），第一把用于签名。
func ParseSeeds(raw string) ([]Key, error) {
	var keys []Key
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		seed, err := decodeBase64(item)
		if err != nil {
			return nil, fmt.Errorf("listen ticket key is not base64: %w", err)
		}
		key, err := KeyFromSeed(seed)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, nil
}

// DeriveSeed 没有单独配置密钥时，从主服务已有的密钥派生（HKDF-SHA256），
// 使多实例部署无需额外配置即得到同一把钥匙。
func DeriveSeed(secret string) ([]byte, error) {
	if strings.TrimSpace(secret) == "" {
		return nil, errors.New("listen ticket: no secret to derive from")
	}
	return hkdf.Key(sha256.New, []byte(secret), []byte("aegis-listen-together"), "ticket-signing-v1", ed25519.SeedSize)
}

func decodeBase64(value string) ([]byte, error) {
	value = strings.TrimRight(value, "=")
	if strings.ContainsAny(value, "-_") {
		return base64.RawURLEncoding.DecodeString(value)
	}
	return base64.RawStdEncoding.DecodeString(value)
}

// Signer 签发票据（主服务用）。
type Signer struct {
	active Key
	all    []Key
	ttl    time.Duration
}

// NewSigner keys[0] 签名，其余只出现在 JWKS 里供轮换期验证。
func NewSigner(keys []Key, ttl time.Duration) (*Signer, error) {
	if len(keys) == 0 {
		return nil, errors.New("listen ticket: no signing key")
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Signer{active: keys[0], all: keys, ttl: ttl}, nil
}

// TTL 票据有效期。
func (s *Signer) TTL() time.Duration { return s.ttl }

// Issue 签一张票据；claims 里的时间与 iss/aud/jti 由这里填。
func (s *Signer) Issue(claims Claims, now time.Time, jti string) (string, time.Time, error) {
	expires := now.Add(s.ttl)
	claims.Issuer = Issuer
	claims.Audience = jwt.ClaimStrings{Audience}
	claims.IssuedAt = jwt.NewNumericDate(now)
	claims.NotBefore = jwt.NewNumericDate(now.Add(-30 * time.Second))
	claims.ExpiresAt = jwt.NewNumericDate(expires)
	claims.ID = jti
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["kid"] = s.active.ID
	signed, err := token.SignedString(s.active.Private)
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, expires, nil
}

// JWK 一把公钥的 JWK 表示（RFC 8037 OKP）。
type JWK struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Use string `json:"use"`
}

// JWKS 公钥集合。
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// JWKS 全部公钥（含轮换中的旧钥）。
func (s *Signer) JWKS() JWKS {
	return KeysJWKS(s.all)
}

// KeysJWKS 把公钥列表编成 JWKS。
func KeysJWKS(keys []Key) JWKS {
	out := JWKS{Keys: make([]JWK, 0, len(keys))}
	for _, key := range keys {
		out.Keys = append(out.Keys, JWK{
			Kty: "OKP", Crv: "Ed25519", X: base64.RawURLEncoding.EncodeToString(key.Public),
			Kid: key.ID, Alg: "EdDSA", Use: "sig",
		})
	}
	return out
}

// ParseJWKS 解析 JWKS 文本里的 Ed25519 公钥（其它类型的钥匙忽略）。
func ParseJWKS(raw []byte) (map[string]ed25519.PublicKey, error) {
	var doc JWKS
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	out := make(map[string]ed25519.PublicKey, len(doc.Keys))
	for _, key := range doc.Keys {
		if key.Kty != "OKP" || key.Crv != "Ed25519" || key.Kid == "" {
			continue
		}
		x, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(key.X, "="))
		if err != nil || len(x) != ed25519.PublicKeySize {
			continue
		}
		out[key.Kid] = ed25519.PublicKey(x)
	}
	if len(out) == 0 {
		return nil, errors.New("jwks has no Ed25519 key")
	}
	return out, nil
}

// ErrMissingFeature 票据没带一起听权益。
var ErrMissingFeature = errors.New("listen ticket lacks the listen_together feature")

// Parse 用 keyfunc 验一张票据：算法只认 EdDSA，iss / aud / exp / nbf 都要对，并且要带一起听权益。
// leeway 是两端时钟的容差。
func Parse(raw string, keyfunc jwt.Keyfunc, leeway time.Duration) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(raw, claims, keyfunc,
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithIssuer(Issuer),
		jwt.WithAudience(Audience),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(leeway),
	)
	if err != nil {
		return nil, err
	}
	if claims.UserID() == 0 || strings.TrimSpace(claims.AppKey) == "" {
		return nil, errors.New("listen ticket has no subject or app")
	}
	if !claims.HasFeature(Feature) {
		return nil, ErrMissingFeature
	}
	return claims, nil
}

// StaticKeyfunc 由固定公钥表构造 keyfunc（测试与离线部署用）。
func StaticKeyfunc(keys map[string]ed25519.PublicKey) jwt.Keyfunc {
	return func(token *jwt.Token) (any, error) {
		kid, _ := token.Header["kid"].(string)
		key, ok := keys[kid]
		if !ok {
			return nil, fmt.Errorf("unknown listen ticket key %q", kid)
		}
		return key, nil
	}
}
