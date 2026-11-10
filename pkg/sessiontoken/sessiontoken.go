// Package sessiontoken 签发与校验终端用户的登录令牌（访问令牌、刷新令牌）。
//
// 格式是 PASETO v4.local（XChaCha20 加密 + BLAKE2b-MAC 认证，库 aidanwoods.dev/go-paseto）：
//
//		v4.local.<加密的载荷>.<页脚>
//
//	  - 载荷加密，客户端读不到里面的任何字段（令牌对客户端是不透明的字符串，过期时间看登录接口返回的
//	    expiresAt / refreshExpiresAt）。
//	  - 页脚明文但受认证：{"kid":"…","app":123}。kid 选解密密钥（支持轮换），app 给加密传输中间件在
//	    解密请求体之前定位应用用；改动页脚会让令牌校验失败。
//	  - 令牌种类（access / refresh）写进 PASETO 的隐式断言，不在令牌里：拿访问令牌当刷新令牌用，
//	    解密直接失败，不存在「类型字段被篡改」或算法混淆一类的问题。
//
// 服务端仍以 Redis 会话（按令牌摘要）为准，令牌本身只是带有效期的、不可伪造的会话凭据。
package sessiontoken

import (
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"aidanwoods.dev/go-paseto"
	"github.com/golang-jwt/jwt/v5"
)

// Prefix 是本格式令牌的固定前缀。
const Prefix = "v4.local."

// 令牌种类。
const (
	TypeAccess  = "access"
	TypeRefresh = "refresh"
)

// DerivedKeyName 用在 PreviousKeys 里，表示「由 JWT_SECRET 派生的密钥」。
// 从未配置 SESSION_TOKEN_KEY 切到显式密钥时，把它放进旧密钥列表，已签发的令牌就能继续用到过期。
const DerivedKeyName = "jwt-secret"

// claimsVersion 载荷结构版本（旧 JWT 是 1）。
const claimsVersion = 2

// ErrInvalid 令牌无效：格式不对、密钥不认识、解密或认证失败、过期、签发方或种类不符。
var ErrInvalid = errors.New("sessiontoken: invalid token")

// Claims 是令牌里服务端要用的内容。
type Claims struct {
	TokenID   string // jti，会话与黑名单都按它找
	Type      string // access | refresh
	FamilyID  string // 刷新令牌族
	AppID     int64
	IssuedAt  time.Time
	ExpiresAt time.Time
	Legacy    bool // 旧 HS256 JWT（过渡期内仍接受）
}

// Options 构造 Codec 的配置。
type Options struct {
	// Issuer 签发方（iss），校验时必须一致。
	Issuer string
	// Key 当前签发用的密钥：64 位十六进制或 base64（标准 / URL 安全，有无填充都行）编码的 32 字节。
	// 为空时由 Secret 经 HKDF-SHA256 派生。
	Key string
	// PreviousKeys 只用来校验的旧密钥（轮换用），格式同 Key；DerivedKeyName 表示由 Secret 派生的那把。
	PreviousKeys []string
	// Secret 即 JWT_SECRET：派生默认密钥，并在 AcceptLegacyJWT 时校验旧 HS256 令牌。
	Secret string
	// AcceptLegacyJWT 过渡期内接受升级前签发的 HS256 JWT（它们的 Redis 会话仍在）。
	// 新令牌一律是 PASETO；旧令牌在刷新时换成新格式，最迟到旧访问令牌的有效期结束就可以关掉。
	AcceptLegacyJWT bool
	// Now 测试用的时钟；为空用 time.Now。
	Now func() time.Time
}

type ringKey struct {
	id  string
	key paseto.V4SymmetricKey
}

// Codec 签发与校验令牌，可并发使用。
type Codec struct {
	issuer       string
	current      ringKey
	keys         map[string]paseto.V4SymmetricKey
	legacySecret []byte
	now          func() time.Time
}

type footer struct {
	KeyID string `json:"kid"`
	AppID int64  `json:"app"`
}

// New 按配置构造 Codec。密钥格式不对时报错（启动时就该失败，而不是签出验不过的令牌）。
func New(opts Options) (*Codec, error) {
	if strings.TrimSpace(opts.Secret) == "" && strings.TrimSpace(opts.Key) == "" {
		return nil, errors.New("sessiontoken: a key or secret is required")
	}
	c := &Codec{issuer: opts.Issuer, keys: map[string]paseto.V4SymmetricKey{}, now: opts.Now}
	if c.now == nil {
		c.now = time.Now
	}
	derived := func() ([]byte, error) {
		if strings.TrimSpace(opts.Secret) == "" {
			return nil, errors.New("sessiontoken: cannot derive a key without a secret")
		}
		return DeriveKey(opts.Secret)
	}
	add := func(raw []byte) (ringKey, error) {
		key, err := paseto.V4SymmetricKeyFromBytes(raw)
		if err != nil {
			return ringKey{}, fmt.Errorf("sessiontoken: %w", err)
		}
		rk := ringKey{id: keyID(raw), key: key}
		c.keys[rk.id] = key
		return rk, nil
	}

	var currentRaw []byte
	var err error
	if strings.TrimSpace(opts.Key) != "" {
		currentRaw, err = ParseKey(opts.Key)
	} else {
		currentRaw, err = derived()
	}
	if err != nil {
		return nil, err
	}
	if c.current, err = add(currentRaw); err != nil {
		return nil, err
	}
	for _, value := range opts.PreviousKeys {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		var raw []byte
		if strings.EqualFold(value, DerivedKeyName) {
			raw, err = derived()
		} else {
			raw, err = ParseKey(value)
		}
		if err != nil {
			return nil, err
		}
		if _, err := add(raw); err != nil {
			return nil, err
		}
	}
	if opts.AcceptLegacyJWT && strings.TrimSpace(opts.Secret) != "" {
		c.legacySecret = []byte(opts.Secret)
	}
	return c, nil
}

// DeriveKey 由 JWT_SECRET 派生 32 字节的令牌密钥（HKDF-SHA256，用途标签与其他派生密钥区分开）。
func DeriveKey(secret string) ([]byte, error) {
	return hkdf.Key(sha256.New, []byte(secret), []byte("aegis/session-token"), "paseto v4.local session token key", 32)
}

// ParseKey 解析配置里的密钥：64 位十六进制，或 base64 编码的 32 字节。
func ParseKey(value string) ([]byte, error) {
	value = strings.TrimSpace(value)
	if len(value) == 64 {
		if raw, err := hex.DecodeString(value); err == nil {
			return raw, nil
		}
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if raw, err := enc.DecodeString(value); err == nil && len(raw) == 32 {
			return raw, nil
		}
	}
	return nil, errors.New("sessiontoken: key must be 32 bytes, hex (64 chars) or base64 encoded")
}

// keyID 由密钥算出的短标识，放进页脚选密钥用；不泄露密钥本身。
func keyID(raw []byte) string {
	sum := sha256.Sum256(append([]byte("aegis/session-token/kid\x00"), raw...))
	return base64.RawURLEncoding.EncodeToString(sum[:6])
}

func implicit(tokenType string) []byte {
	return []byte("aegis/session/" + tokenType)
}

// CurrentKeyID 当前签发密钥的标识（运维排查用）。
func (c *Codec) CurrentKeyID() string { return c.current.id }

// Issue 签发一枚令牌。
func (c *Codec) Issue(tokenType, tokenID, familyID string, appID int64, issuedAt, expiresAt time.Time) (string, error) {
	if tokenType != TypeAccess && tokenType != TypeRefresh {
		return "", fmt.Errorf("sessiontoken: unknown token type %q", tokenType)
	}
	if tokenID == "" {
		return "", errors.New("sessiontoken: token id is required")
	}
	token := paseto.NewToken()
	token.SetJti(tokenID)
	token.SetIssuer(c.issuer)
	token.SetIssuedAt(issuedAt)
	token.SetNotBefore(issuedAt.Add(-time.Minute)) // 容忍多实例之间的轻微时钟偏差
	token.SetExpiration(expiresAt)
	token.SetString("typ", tokenType)
	if familyID != "" {
		token.SetString("fam", familyID)
	}
	if err := token.Set("app", appID); err != nil {
		return "", err
	}
	if err := token.Set("sv", claimsVersion); err != nil {
		return "", err
	}
	foot, err := json.Marshal(footer{KeyID: c.current.id, AppID: appID})
	if err != nil {
		return "", err
	}
	token.SetFooter(foot)
	return token.V4Encrypt(c.current.key, implicit(tokenType)), nil
}

// Parse 校验令牌并取出内容；expectedType 必须是 TypeAccess 或 TypeRefresh。
func (c *Codec) Parse(raw, expectedType string) (*Claims, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, Prefix) {
		return c.parsePaseto(raw, expectedType)
	}
	if c.legacySecret != nil && strings.HasPrefix(raw, "eyJ") {
		return c.parseLegacy(raw, expectedType)
	}
	return nil, ErrInvalid
}

func (c *Codec) parsePaseto(raw, expectedType string) (*Claims, error) {
	foot, ok := readFooter(raw)
	if !ok {
		return nil, ErrInvalid
	}
	key, ok := c.keys[foot.KeyID]
	if !ok {
		return nil, ErrInvalid
	}
	parser := paseto.MakeParser([]paseto.Rule{c.validNow, paseto.IssuedBy(c.issuer)})
	token, err := parser.ParseV4Local(key, raw, implicit(expectedType))
	if err != nil {
		return nil, ErrInvalid
	}
	claims := &Claims{Type: expectedType}
	if claims.TokenID, err = token.GetJti(); err != nil || claims.TokenID == "" {
		return nil, ErrInvalid
	}
	if typ, err := token.GetString("typ"); err != nil || typ != expectedType {
		return nil, ErrInvalid
	}
	if err := token.Get("app", &claims.AppID); err != nil || claims.AppID != foot.AppID {
		return nil, ErrInvalid
	}
	claims.FamilyID, _ = token.GetString("fam")
	claims.IssuedAt, _ = token.GetIssuedAt()
	claims.ExpiresAt, _ = token.GetExpiration()
	return claims, nil
}

// validNow 要求未过期、已到 nbf。不检查 iat：多实例时签发方时钟略快会让刚签出的令牌被拒，
// nbf 已经比 iat 提前了一分钟。
func (c *Codec) validNow(token paseto.Token) error {
	now := c.now()
	exp, err := token.GetExpiration()
	if err != nil || !now.Before(exp) {
		return ErrInvalid
	}
	nbf, err := token.GetNotBefore()
	if err != nil || now.Before(nbf) {
		return ErrInvalid
	}
	return nil
}

func (c *Codec) parseLegacy(raw, expectedType string) (*Claims, error) {
	mapClaims := jwt.MapClaims{}
	parsed, err := jwt.ParseWithClaims(raw, mapClaims, func(*jwt.Token) (any, error) {
		return c.legacySecret, nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithIssuer(c.issuer), jwt.WithTimeFunc(c.now))
	if err != nil || !parsed.Valid {
		return nil, ErrInvalid
	}
	tokenType, _ := mapClaims["typ"].(string)
	// 早期访问令牌没有 typ。管理员令牌（typ=admin）同一密钥签发，这里必须拒掉。
	if tokenType == "" && expectedType == TypeAccess {
		tokenType = TypeAccess
	}
	if tokenType != expectedType {
		return nil, ErrInvalid
	}
	claims := &Claims{Type: tokenType, Legacy: true}
	claims.TokenID, _ = mapClaims["jti"].(string)
	if claims.TokenID == "" {
		return nil, ErrInvalid
	}
	claims.FamilyID, _ = mapClaims["family"].(string)
	if v, ok := mapClaims["appid"].(float64); ok {
		claims.AppID = int64(v)
	}
	if t, err := mapClaims.GetIssuedAt(); err == nil && t != nil {
		claims.IssuedAt = t.Time
	}
	if t, err := mapClaims.GetExpirationTime(); err == nil && t != nil {
		claims.ExpiresAt = t.Time
	}
	return claims, nil
}

// PeekAppID 不校验、只读页脚里的应用 ID：加密传输中间件要在解密请求体之前知道是哪个应用。
// 页脚受认证，伪造它的令牌在随后的鉴权里一定失败；这里读错只会让解密失败，不会越权。
func PeekAppID(raw string) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if !strings.HasPrefix(raw, Prefix) {
		return 0, false
	}
	foot, ok := readFooter(raw)
	if !ok || foot.AppID <= 0 {
		return 0, false
	}
	return foot.AppID, true
}

func readFooter(raw string) (footer, bool) {
	// v4.local.<payload>.<footer>：没有页脚的令牌不是我们签的。
	rest := raw[len(Prefix):]
	dot := strings.IndexByte(rest, '.')
	if dot <= 0 || dot == len(rest)-1 || strings.IndexByte(rest[dot+1:], '.') >= 0 {
		return footer{}, false
	}
	data, err := base64.RawURLEncoding.DecodeString(rest[dot+1:])
	if err != nil || len(data) > 512 {
		return footer{}, false
	}
	var foot footer
	if err := json.Unmarshal(data, &foot); err != nil || foot.KeyID == "" {
		return footer{}, false
	}
	return foot, true
}
