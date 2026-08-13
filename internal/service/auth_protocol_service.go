package service

import (
	"context"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	appdomain "aegis/internal/domain/app"
	authprotocol "aegis/internal/domain/authprotocol"
	oauthdomain "aegis/internal/domain/oauth"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
	apperrors "aegis/pkg/errors"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/crypto/chacha20poly1305"
	"golang.org/x/crypto/hkdf"
)

const (
	authProtocolClockSkew = 5 * time.Minute
	authProtocolKeyTTL    = 180 * 24 * time.Hour
	// signingSecretBytes 32 字节即 256 位，与 HMAC-SHA256 的输出等宽。
	signingSecretBytes = 32
	signingSecretPfx   = "sk_"
	// sealed 解密后的复查上限，与网关截断读取用的是同一份常量。
	sealedPayloadLimit = authprotocol.MaxUploadBytes
	sealedQueryLimit   = authprotocol.MaxQueryBytes
	// nonce 长度区间，校验与 /config 下发读同一份值。
	authProtocolNonceMin = 8
	authProtocolNonceMax = 128
)

type AuthProtocolService struct {
	log       *zap.Logger
	pg        *pgrepo.Repository
	replay    *redisrepo.ReplayRepository
	masterKey []byte
}

func NewAuthProtocolService(log *zap.Logger, pg *pgrepo.Repository, replay *redisrepo.ReplayRepository, rootSecret string) *AuthProtocolService {
	digest := sha256.Sum256([]byte("aegis.auth-protocol-v2.master\x00" + rootSecret))
	return &AuthProtocolService{log: log, pg: pg, replay: replay, masterKey: digest[:]}
}

// ─────────────────────────────────────────────────────────────────────
// 策略读写
// ─────────────────────────────────────────────────────────────────────

func (s *AuthProtocolService) GetPolicy(ctx context.Context, appID int64) (*authprotocol.Policy, error) {
	policy, err := s.pg.GetAppAuthProtocolPolicy(ctx, appID)
	if err != nil {
		return nil, err
	}
	if policy != nil {
		return policy, nil
	}
	return defaultAuthProtocolPolicy(appID), nil
}

func (s *AuthProtocolService) UpdatePolicy(ctx context.Context, appID int64, patch authprotocol.PolicyPatch) (*authprotocol.Policy, error) {
	current, err := s.GetPolicy(ctx, appID)
	if err != nil {
		return nil, err
	}
	if patch.Identifiers != nil {
		current.Identifiers = normalizeProtocolValues(patch.Identifiers)
	}
	if patch.LoginMethods != nil {
		current.LoginMethods = normalizeProtocolValues(patch.LoginMethods)
	}
	if patch.RegisterMethods != nil {
		current.RegisterMethods = normalizeProtocolValues(patch.RegisterMethods)
	}
	if patch.RegistrationSchema != nil {
		current.RegistrationSchema = patch.RegistrationSchema
	}
	if patch.RequireCaptcha != nil {
		current.RequireCaptcha = *patch.RequireCaptcha
	}
	if patch.AutoLogin != nil {
		current.AutoLogin = *patch.AutoLogin
	}
	if patch.SecurityLevel != nil {
		current.SecurityLevel = strings.ToLower(strings.TrimSpace(*patch.SecurityLevel))
	}
	if patch.AllowLegacy != nil {
		current.AllowLegacy = *patch.AllowLegacy
	}
	if err := validateAuthProtocolPolicy(current); err != nil {
		return nil, err
	}
	// 升到 signed/sealed 却没有密钥，等于把接入方挡在门外：这里自动补一把，
	// 管理员随后可以在控制台重新轮换取回明文。
	if current.SecurityLevel != authprotocol.LevelStandard && !current.SigningSecretSet {
		if _, _, err := s.RotateSigningSecret(ctx, appID); err != nil {
			return nil, err
		}
	}
	if current.SecurityLevel == authprotocol.LevelSealed {
		if _, err := s.ensureTransportKeys(ctx, appID); err != nil {
			return nil, err
		}
	}
	return s.pg.UpsertAppAuthProtocolPolicy(ctx, *current)
}

func (s *AuthProtocolService) ResolveAppAndPolicy(ctx context.Context, appKey string) (*appdomain.App, *authprotocol.Policy, error) {
	app, err := s.resolveApp(ctx, appKey)
	if err != nil {
		return nil, nil, err
	}
	policy, err := s.GetPolicy(ctx, app.ID)
	return app, policy, err
}

// ─────────────────────────────────────────────────────────────────────
// GET /config
// ─────────────────────────────────────────────────────────────────────

// BuildConfig 组装接入方唯一需要预先拉取的描述文档。
// 只下发当前安全等级真正用得上的东西：standard 档不会出现任何密码学参数。
// captcha 是各入口是否需要图形验证码的**结论**，由调用方用
// ResolveCaptchaRequirement 算好传入 —— 它要读应用验证码配置与平台短信配置，
// 而这两样都不在本服务的依赖里。
func (s *AuthProtocolService) BuildConfig(
	ctx context.Context,
	app *appdomain.App,
	policy *authprotocol.Policy,
	providers []oauthdomain.PublicProvider,
	captcha authprotocol.CaptchaRequirement,
) (*authprotocol.Config, error) {
	if app == nil || policy == nil {
		return nil, apperrors.New(40470, http.StatusNotFound, "应用不存在或已停用")
	}
	// 应用没启用 oauth 时不下发渠道列表：否则登录页会画出一排点了必 403 的按钮。
	if providers == nil || !slices.Contains(policy.LoginMethods, authprotocol.MethodOAuth) {
		providers = []oauthdomain.PublicProvider{}
	}
	base := "/api/v1/apps/" + app.AppKey
	endpoints, operations := buildGatewayCatalog(base)
	now := time.Now().UTC()
	config := &authprotocol.Config{
		ProtocolVersion: authprotocol.ProtocolVersion,
		App:             authprotocol.AppBrief{Key: app.AppKey, Name: app.Name, Status: app.Status},
		Auth: authprotocol.AuthCapability{
			Identifiers:        policy.Identifiers,
			LoginMethods:       policy.LoginMethods,
			RegisterMethods:    policy.RegisterMethods,
			RegistrationSchema: policy.RegistrationSchema,
			Captcha:            captcha,
			AutoLogin:          policy.AutoLogin,
			RegisterEnabled:    app.RegisterStatus,
			LoginEnabled:       app.LoginStatus,
			OAuthProviders:     providers,
		},
		Security: authprotocol.SecuritySpec{
			Level:        policy.SecurityLevel,
			AppKeyHeader: "X-Aegis-App-Key",
		},
		// 服务端时间随 /config 一起下发。/config 是唯一免包装可读的接口，
		// 也就是「还没签成功」时唯一能拿到服务端时间的地方 —— 移动设备时钟偏差
		// 超过 5 分钟就会一路 40071，而客户端自己根本不知道慢了多少。
		ServerTime: authprotocol.ServerTime{Unix: now.Unix(), ISO: now.Format(time.RFC3339)},
		Limits: authprotocol.Limits{
			MaxRequestBytes:  authprotocol.MaxRequestBytes,
			MaxUploadBytes:   authprotocol.MaxUploadBytes,
			ClockSkewSeconds: int(authProtocolClockSkew.Seconds()),
			NonceMinLength:   authProtocolNonceMin,
			NonceMaxLength:   authProtocolNonceMax,
		},
		// 完整相对路径直接给出，客户端不需要自己拼 appKey，也就拼不错。
		Endpoints:  endpoints,
		Operations: operations,
		Errors:     gatewayErrors,
	}

	if policy.SecurityLevel == authprotocol.LevelSigned || policy.SecurityLevel == authprotocol.LevelSealed {
		config.Security.Signature = &authprotocol.SignatureSpec{
			Scheme:          authprotocol.SignatureScheme,
			Header:          headerSignature,
			TimestampHeader: headerTimestamp,
			NonceHeader:     headerNonce,
			Version:         authprotocol.SignaturePrefixV2,
			Canonical:       signatureCanonicalTemplate,
			CanonicalLegacy: signatureCanonicalTemplateV1,
			MaxClockSkew:    int(authProtocolClockSkew.Seconds()),
		}
	}

	if policy.SecurityLevel == authprotocol.LevelSealed {
		keys, err := s.ensureTransportKeys(ctx, app.ID)
		if err != nil {
			return nil, err
		}
		spec := &authprotocol.TransportSpec{
			Protocol:     authprotocol.TransportV2,
			Algorithms:   []string{authprotocol.TransportAlgo},
			MaxClockSkew: int(authProtocolClockSkew.Seconds()),
			ReplayWindow: int(authProtocolClockSkew.Seconds()),
			HKDFSalt:     `SHA-256("{appKey}:{keyId}")`,
			// 无请求体的方法把密文放在 query 里 —— OkHttp / URLSession / fetch
			// 都拒绝构造带 body 的 GET，正好是 Android / iOS / Web 三端。
			PayloadParam:           authprotocol.SealedPayloadParam,
			BodylessMethods:        []string{http.MethodGet, http.MethodDelete, http.MethodHead},
			PlainContentTypeHeader: headerPlainContentType,
			PublicKeys:             make([]authprotocol.PublicTransportKey, 0, len(keys)),
		}
		for _, key := range keys {
			public := authprotocol.PublicTransportKey{
				KeyID: key.KeyID, Algorithm: key.Algorithm,
				PublicKey: base64.RawURLEncoding.EncodeToString(key.PublicKey),
				Status:    key.Status, NotBefore: key.NotBefore, NotAfter: key.NotAfter,
			}
			if key.Status == "active" {
				spec.ActiveKeyID = key.KeyID
			}
			spec.PublicKeys = append(spec.PublicKeys, public)
		}
		config.Security.Transport = spec
	}
	return config, nil
}

// ─────────────────────────────────────────────────────────────────────
// signed 档：HMAC-SHA256 请求签名
// ─────────────────────────────────────────────────────────────────────

const (
	headerSignature        = "X-Aegis-Signature"
	headerTimestamp        = "X-Aegis-Timestamp"
	headerNonce            = "X-Aegis-Nonce"
	headerPlainContentType = "X-Aegis-Plain-Content-Type"
)

// signatureCanonicalTemplate 待签名字符串模板，随 /config 原样下发，
// 接入方不需要读文档就能对齐每一个字节。
//
// v2 比 v1 多了一行 {query}：接口面从「只有 POST + JSON body」铺开到带分页、
// 带筛选的 GET 之后，不签 query 意味着 `?page=1` 能被中间人改成 `?page=999`
// 而签名照样通过。query 原样参与，不排序也不重新编码 —— 客户端签的就是它
// 放到线上的那串字节，任何语言都能逐字节复现。
const (
	signatureCanonicalTemplate   = "aegis-hmac-sha256\n{appKey}\n{METHOD}\n{path}\n{query}\n{timestamp}\n{nonce}\n{sha256Hex(body)}"
	signatureCanonicalTemplateV1 = "aegis-hmac-sha256\n{appKey}\n{METHOD}\n{path}\n{timestamp}\n{nonce}\n{sha256Hex(body)}"
)

// RotateSigningSecret 生成一把新的应用密钥，返回明文（仅此一次可见）与提示串。
func (s *AuthProtocolService) RotateSigningSecret(ctx context.Context, appID int64) (string, *authprotocol.Policy, error) {
	raw := make([]byte, signingSecretBytes)
	if _, err := io.ReadFull(rand.Reader, raw); err != nil {
		return "", nil, err
	}
	secret := signingSecretPfx + base64.RawURLEncoding.EncodeToString(raw)
	cipher, err := encryptSecret(s.masterKey, secret)
	if err != nil {
		return "", nil, err
	}
	policy, err := s.pg.SetAppAuthProtocolSigningSecret(ctx, appID, cipher, signingSecretHint(secret))
	if err != nil {
		return "", nil, err
	}
	return secret, policy, nil
}

// VerifySignature 校验 signed 档请求签名，含时间窗与 Nonce 防重放。
func (s *AuthProtocolService) VerifySignature(ctx context.Context, meta authprotocol.SignatureMetadata) error {
	app, policy, err := s.ResolveAppAndPolicy(ctx, meta.AppKey)
	if err != nil {
		return err
	}
	if !policy.SigningSecretSet {
		return apperrors.New(50372, http.StatusServiceUnavailable, "应用尚未签发签名密钥")
	}
	secret, err := decryptSecret(s.masterKey, policy.SigningSecretCipher)
	if err != nil {
		return errors.New("签名密钥解密失败")
	}
	if err := s.checkTimestampAndNonce(ctx, app.ID, "sig", meta.Timestamp, meta.Nonce); err != nil {
		return err
	}
	version, provided, err := splitSignatureVersion(meta)
	if err != nil {
		return err
	}
	providedBytes, err := hex.DecodeString(provided)
	if err != nil || len(providedBytes) != sha256.Size {
		return apperrors.New(40174, http.StatusUnauthorized, "请求签名格式无效")
	}
	expected := computeRequestSignature(secret, meta, version)
	if subtle.ConstantTimeCompare(providedBytes, expected) != 1 {
		return apperrors.New(40175, http.StatusUnauthorized, "请求签名校验失败")
	}
	return nil
}

// splitSignatureVersion 拆出签名版本与十六进制摘要。
//
// v1 的待签名字符串不含 query string，因此**只在请求没有 query 时**才被接受：
// 否则中间人改掉分页、筛选、目标 ID 都不会破坏签名。老客户端全是
// 「POST + 无 query」，这条规则对它们零影响，新接口自然被推到 v2。
func splitSignatureVersion(meta authprotocol.SignatureMetadata) (string, string, error) {
	raw := strings.TrimSpace(meta.Signature)
	switch {
	case strings.HasPrefix(raw, authprotocol.SignaturePrefixV2):
		return authprotocol.SignaturePrefixV2, strings.TrimPrefix(raw, authprotocol.SignaturePrefixV2), nil
	case strings.HasPrefix(raw, authprotocol.SignaturePrefix):
		if strings.TrimSpace(meta.Query) != "" {
			return "", "", apperrors.New(40176, http.StatusUnauthorized,
				"带 query 的请求必须使用 v2 签名（待签名字符串需包含 query 行）")
		}
		return authprotocol.SignaturePrefix, strings.TrimPrefix(raw, authprotocol.SignaturePrefix), nil
	default:
		return "", "", apperrors.New(40174, http.StatusUnauthorized, "请求签名格式无效")
	}
}

// SignRequest 供自检与官方 SDK 复用：按同一套规则算出签名头的值（v2）。
func SignRequest(secret string, meta authprotocol.SignatureMetadata) string {
	return authprotocol.SignaturePrefixV2 +
		hex.EncodeToString(computeRequestSignature(secret, meta, authprotocol.SignaturePrefixV2))
}

func computeRequestSignature(secret string, meta authprotocol.SignatureMetadata, version string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signatureCanonicalString(meta, version)))
	return mac.Sum(nil)
}

// signatureCanonicalString 构造待签名字符串。
//
// 单独一个函数是为了能被逐字节断言：签名对不上时错误信息只会说「不对」，
// 不会说「哪一行不对」，因此这串字节必须有测试直接盯着
// （auth_protocol_canonical_test.go，与 Kotlin SDK 共用同一批字面量）。
func signatureCanonicalString(meta authprotocol.SignatureMetadata, version string) string {
	bodyDigest := sha256.Sum256(meta.Body)
	fields := []string{
		authprotocol.SignatureScheme,
		strings.TrimSpace(meta.AppKey),
		strings.ToUpper(strings.TrimSpace(meta.Method)),
		meta.Path,
	}
	if version == authprotocol.SignaturePrefixV2 {
		// 没有 query 时这里追加的是**空串**，于是待签名字符串里留下一个空行。
		// 省掉这一行会让「有 query」和「没 query」两种情况的行数不同，
		// 客户端最容易在这里写错。
		fields = append(fields, meta.Query)
	}
	fields = append(fields,
		strings.TrimSpace(meta.Timestamp),
		strings.TrimSpace(meta.Nonce),
		hex.EncodeToString(bodyDigest[:]),
	)
	return strings.Join(fields, "\n")
}

// RevealSigningSecret 仅供服务端内部自检使用，绝不经由任何 Handler 出网。
func (s *AuthProtocolService) RevealSigningSecret(ctx context.Context, appID int64) (string, error) {
	policy, err := s.GetPolicy(ctx, appID)
	if err != nil {
		return "", err
	}
	if !policy.SigningSecretSet {
		return "", apperrors.New(50372, http.StatusServiceUnavailable, "应用尚未签发签名密钥")
	}
	return decryptSecret(s.masterKey, policy.SigningSecretCipher)
}

func signingSecretHint(secret string) string {
	if len(secret) <= 12 {
		return signingSecretPfx + "***"
	}
	return secret[:8] + "…" + secret[len(secret)-4:]
}

// checkTimestampAndNonce 时间窗 + 一次性 Nonce，signed 与 sealed 共用。
func (s *AuthProtocolService) checkTimestampAndNonce(ctx context.Context, appID int64, scope, rawTimestamp, nonce string) error {
	timestamp, err := strconv.ParseInt(strings.TrimSpace(rawTimestamp), 10, 64)
	if err != nil || absDuration(time.Since(time.Unix(timestamp, 0))) > authProtocolClockSkew {
		return apperrors.New(40071, http.StatusBadRequest, "请求时间戳无效或已过期")
	}
	nonce = strings.TrimSpace(nonce)
	if len(nonce) < authProtocolNonceMin || len(nonce) > authProtocolNonceMax {
		return apperrors.New(40072, http.StatusBadRequest, "请求 nonce 无效")
	}
	if s.replay == nil {
		return apperrors.New(50370, http.StatusServiceUnavailable, "防重放服务不可用")
	}
	acquired, err := s.replay.TryAcquireNonce(ctx,
		fmt.Sprintf("appv1:%s:%d:%s", scope, appID, nonce), authProtocolClockSkew)
	if err != nil || !acquired {
		return apperrors.New(40970, http.StatusConflict, "请求 nonce 已使用")
	}
	return nil
}

// ─────────────────────────────────────────────────────────────────────
// sealed 档：Transport v2 端到端加密
// ─────────────────────────────────────────────────────────────────────

func (s *AuthProtocolService) RotateTransportKey(ctx context.Context, appID int64) (*authprotocol.PublicTransportKey, error) {
	curve := ecdh.X25519()
	privateKey, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	encodedPrivate, err := encryptSecret(s.masterKey, base64.RawURLEncoding.EncodeToString(privateKey.Bytes()))
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	item, err := s.pg.RotateAppTransportKey(ctx, authprotocol.TransportKey{
		AppID: appID, KeyID: "atk_" + uuid.NewString(), Algorithm: authprotocol.TransportAlgo,
		PublicKey: privateKey.PublicKey().Bytes(), PrivateKeyCipher: encodedPrivate,
		NotBefore: now.Add(-time.Minute), NotAfter: now.Add(authProtocolKeyTTL),
	})
	if err != nil {
		return nil, err
	}
	return &authprotocol.PublicTransportKey{
		KeyID: item.KeyID, Algorithm: item.Algorithm,
		PublicKey: base64.RawURLEncoding.EncodeToString(item.PublicKey),
		Status:    item.Status, NotBefore: item.NotBefore, NotAfter: item.NotAfter,
	}, nil
}

func (s *AuthProtocolService) ListTransportKeys(ctx context.Context, appID int64) ([]authprotocol.PublicTransportKey, error) {
	items, err := s.pg.ListAppTransportKeys(ctx, appID)
	if err != nil {
		return nil, err
	}
	result := make([]authprotocol.PublicTransportKey, 0, len(items))
	for _, item := range items {
		result = append(result, authprotocol.PublicTransportKey{
			KeyID: item.KeyID, Algorithm: item.Algorithm,
			PublicKey: base64.RawURLEncoding.EncodeToString(item.PublicKey),
			Status:    item.Status, NotBefore: item.NotBefore, NotAfter: item.NotAfter,
		})
	}
	return result, nil
}

func (s *AuthProtocolService) RevokeTransportKey(ctx context.Context, appID int64, keyID string) error {
	keyID = strings.TrimSpace(keyID)
	if keyID == "" {
		return apperrors.New(40070, http.StatusBadRequest, "缺少传输密钥标识")
	}
	item, err := s.pg.GetUsableAppTransportKey(ctx, appID, keyID)
	if err != nil {
		return err
	}
	if item == nil {
		return apperrors.New(40471, http.StatusNotFound, "传输密钥不存在")
	}
	// active key 不能直接留下真空期：先创建替代密钥，再撤销旧密钥。
	if item.Status == "active" {
		if _, err := s.RotateTransportKey(ctx, appID); err != nil {
			return err
		}
	}
	return s.pg.RevokeAppTransportKey(ctx, appID, keyID)
}

func (s *AuthProtocolService) OpenRequest(ctx context.Context, meta authprotocol.RequestMetadata, encodedCiphertext []byte) ([]byte, *authprotocol.CryptoContext, error) {
	app, _, err := s.ResolveAppAndPolicy(ctx, meta.AppKey)
	if err != nil {
		return nil, nil, err
	}
	meta.KeyID = strings.TrimSpace(meta.KeyID)
	if len(meta.KeyID) < 5 || len(meta.KeyID) > 64 || !strings.HasPrefix(meta.KeyID, "atk_") {
		return nil, nil, apperrors.New(40070, http.StatusBadRequest, "传输密钥标识无效")
	}
	nonce, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(meta.Nonce))
	if err != nil || len(nonce) != chacha20poly1305.NonceSizeX {
		return nil, nil, apperrors.New(40072, http.StatusBadRequest, "请求 nonce 无效")
	}
	clientPublicBytes, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(meta.ClientPublicKey))
	if err != nil || len(clientPublicBytes) != 32 {
		return nil, nil, apperrors.New(40073, http.StatusBadRequest, "客户端临时公钥无效")
	}
	if err := s.checkTimestampAndNonce(ctx, app.ID, "sealed:"+meta.KeyID, meta.Timestamp, meta.Nonce); err != nil {
		return nil, nil, err
	}
	keyRecord, err := s.pg.GetUsableAppTransportKey(ctx, app.ID, meta.KeyID)
	if err != nil {
		return nil, nil, err
	}
	if keyRecord == nil {
		return nil, nil, apperrors.New(40074, http.StatusBadRequest, "传输密钥不存在、已撤销或已过期")
	}
	privateEncoded, err := decryptSecret(s.masterKey, keyRecord.PrivateKeyCipher)
	if err != nil {
		return nil, nil, errors.New("传输私钥解密失败")
	}
	privateBytes, err := base64.RawURLEncoding.DecodeString(privateEncoded)
	if err != nil {
		return nil, nil, errors.New("传输私钥格式无效")
	}
	curve := ecdh.X25519()
	privateKey, err := curve.NewPrivateKey(privateBytes)
	if err != nil {
		return nil, nil, err
	}
	clientPublic, err := curve.NewPublicKey(clientPublicBytes)
	if err != nil {
		return nil, nil, apperrors.New(40073, http.StatusBadRequest, "客户端临时公钥无效")
	}
	shared, err := privateKey.ECDH(clientPublic)
	if err != nil {
		return nil, nil, apperrors.New(40075, http.StatusBadRequest, "密钥协商失败")
	}
	plaintext, key, aad, err := openTransportRequestPayload(shared, app.AppKey, meta, nonce, encodedCiphertext)
	if err != nil {
		return nil, nil, err
	}
	return plaintext, &authprotocol.CryptoContext{
		Key: key, AppID: app.ID, AppKey: app.AppKey, KeyID: keyRecord.KeyID,
		RequestNonce: nonce, RequestAAD: aad,
	}, nil
}

func openTransportRequestPayload(shared []byte, appKey string, meta authprotocol.RequestMetadata, nonce, encodedCiphertext []byte) ([]byte, []byte, []byte, error) {
	aad := transportRequestAAD(meta)
	key, err := deriveTransportKey(shared, appKey, meta.KeyID, aad)
	if err != nil {
		return nil, nil, nil, err
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(string(encodedCiphertext)))
	if err != nil {
		return nil, nil, nil, apperrors.New(40076, http.StatusBadRequest, "加密载荷格式无效")
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, nil, nil, err
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, nil, nil, apperrors.New(40077, http.StatusBadRequest, "加密载荷认证失败")
	}
	if err := validateSealedPlaintext(meta, plaintext); err != nil {
		return nil, nil, nil, err
	}
	return plaintext, key, aad, nil
}

// validateSealedPlaintext 按载荷形态做最小必要校验。
//
// 载荷有三种形态，早年只有第二种，于是校验被写死成「必须是 JSON」：
//
//	query        —— GET / DELETE / HEAD，明文是 query string（见 SealedPayloadParam）
//	JSON         —— 绝大多数写接口
//	任意字节流   —— multipart 上传，由 X-Aegis-Plain-Content-Type 声明
//
// 把「必须是 JSON」留着，后两种就永远进不来：上传会在这里被判成载荷无效，
// 而错误码指向的是加密层，接入方会在密钥和 AAD 上白排查半天。
func validateSealedPlaintext(meta authprotocol.RequestMetadata, plaintext []byte) error {
	if authprotocol.BodylessMethod(meta.Method) {
		if len(plaintext) > sealedQueryLimit {
			return apperrors.New(40078, http.StatusBadRequest, "解密后的 query 超过限制")
		}
		if _, err := url.ParseQuery(string(plaintext)); err != nil {
			return apperrors.New(40078, http.StatusBadRequest, "解密载荷不是合法的 query string")
		}
		return nil
	}
	if len(plaintext) > sealedPayloadLimit {
		return apperrors.New(40078, http.StatusBadRequest, "解密载荷超过限制")
	}
	// 未声明原始类型 = 老客户端 = 一定是 JSON。在这里校验比让 handler 报
	// 「字段绑定失败」更接近病灶。
	if isJSONContentType(meta.PlainContentType) && !json.Valid(plaintext) {
		return apperrors.New(40078, http.StatusBadRequest, "解密载荷不是合法 JSON")
	}
	return nil
}

func isJSONContentType(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return true
	}
	if index := strings.IndexByte(value, ';'); index >= 0 {
		value = strings.TrimSpace(value[:index])
	}
	return value == "application/json" || strings.HasSuffix(value, "+json")
}

func (s *AuthProtocolService) SealResponse(cryptoContext *authprotocol.CryptoContext, status int, plaintext []byte) ([]byte, string, error) {
	if cryptoContext == nil {
		return nil, "", errors.New("缺少传输上下文")
	}
	responseNonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := io.ReadFull(rand.Reader, responseNonce); err != nil {
		return nil, "", err
	}
	responseKeyDigest := sha256.Sum256(append(append([]byte(nil), cryptoContext.Key...), []byte("aegis-response-v2")...))
	aead, err := chacha20poly1305.NewX(responseKeyDigest[:])
	if err != nil {
		return nil, "", err
	}
	aad := transportResponseAAD(cryptoContext.AppKey, cryptoContext.KeyID, status,
		base64.RawURLEncoding.EncodeToString(cryptoContext.RequestNonce),
		base64.RawURLEncoding.EncodeToString(responseNonce))
	sealed := aead.Seal(nil, responseNonce, plaintext, aad)
	return []byte(base64.RawURLEncoding.EncodeToString(sealed)),
		base64.RawURLEncoding.EncodeToString(responseNonce), nil
}

func (s *AuthProtocolService) ensureTransportKeys(ctx context.Context, appID int64) ([]authprotocol.TransportKey, error) {
	active, err := s.pg.GetActiveAppTransportKey(ctx, appID)
	if err != nil {
		return nil, err
	}
	if active == nil {
		if _, err := s.RotateTransportKey(ctx, appID); err != nil {
			return nil, err
		}
	}
	return s.pg.ListPublicAppTransportKeys(ctx, appID)
}

func (s *AuthProtocolService) resolveApp(ctx context.Context, appKey string) (*appdomain.App, error) {
	appKey = strings.TrimSpace(appKey)
	if appKey == "" {
		return nil, apperrors.New(40079, http.StatusBadRequest, "缺少 appKey")
	}
	app, err := s.pg.GetAppByKey(ctx, appKey)
	if err != nil {
		return nil, err
	}
	if app == nil || !app.Status {
		return nil, apperrors.New(40470, http.StatusNotFound, "应用不存在或已停用")
	}
	return app, nil
}

// ─────────────────────────────────────────────────────────────────────
// 默认值与校验
// ─────────────────────────────────────────────────────────────────────

func defaultAuthProtocolPolicy(appID int64) *authprotocol.Policy {
	return &authprotocol.Policy{
		AppID: appID, ProtocolVersion: authprotocol.ProtocolVersion,
		Identifiers:     []string{"username", "email"},
		LoginMethods:    []string{"password"},
		RegisterMethods: []string{"password"},
		RegistrationSchema: []authprotocol.RegistrationField{
			{Name: "account", Type: "text", Required: true, Label: "账号"},
			{Name: "password", Type: "password", Required: true, Mutable: true, Label: "密码"},
			{Name: "nickname", Type: "text", Mutable: true, Label: "昵称"},
		},
		AutoLogin: true, AllowLegacy: true,
		SecurityLevel: authprotocol.LevelStandard,
	}
}

func validateAuthProtocolPolicy(policy *authprotocol.Policy) error {
	if policy == nil || len(policy.Identifiers) == 0 || len(policy.LoginMethods) == 0 {
		return apperrors.New(40080, http.StatusBadRequest, "认证标识符和登录方式不能为空")
	}
	if !authprotocol.ValidSecurityLevel(policy.SecurityLevel) {
		return apperrors.New(40085, http.StatusBadRequest, "安全等级只能是 standard / signed / sealed")
	}
	allowedIdentifiers := map[string]bool{"username": true, "email": true, "phone": true}
	for _, value := range policy.Identifiers {
		if !allowedIdentifiers[value] {
			return apperrors.New(40081, http.StatusBadRequest, "包含不支持的认证标识符")
		}
	}
	// 只发布已经具备完整路由、策略校验与响应封装的方式。
	// 不能只在 /config 中"宣称支持"而让客户端撞上 403。
	for _, value := range policy.LoginMethods {
		if !authprotocol.ValidLoginMethod(value) {
			return apperrors.New(40082, http.StatusBadRequest,
				"登录方式只能是 password / sms / oauth / cardkey")
		}
	}
	// 第三方注册没有独立开关：是否允许自动建号由每个渠道自己的 allowRegister 决定，
	// 放在这里会变成两处配同一件事，接入方永远搞不清哪个生效。
	for _, value := range policy.RegisterMethods {
		if !authprotocol.ValidRegisterMethod(value) {
			return apperrors.New(40082, http.StatusBadRequest,
				"注册方式只能是 password / sms（第三方自动注册由渠道级 allowRegister 控制）")
		}
	}
	// 短信认证以手机号为身份，标识符里没有 phone 的话客户端拿不到可用的登录入口。
	if slices.Contains(policy.LoginMethods, authprotocol.MethodSMS) ||
		slices.Contains(policy.RegisterMethods, authprotocol.MethodSMS) {
		if !slices.Contains(policy.Identifiers, "phone") {
			return apperrors.New(40086, http.StatusBadRequest,
				"启用短信认证时，登录标识必须包含 phone")
		}
	}
	if len(policy.RegistrationSchema) > 32 {
		return apperrors.New(40083, http.StatusBadRequest, "注册字段不能超过 32 个")
	}
	seen := map[string]bool{}
	for _, field := range policy.RegistrationSchema {
		field.Name = strings.TrimSpace(field.Name)
		if field.Name == "" || len(field.Name) > 64 || seen[field.Name] ||
			strings.HasPrefix(strings.ToLower(field.Name), "register_") {
			return apperrors.New(40083, http.StatusBadRequest, "注册字段名称为空或重复")
		}
		switch strings.ToLower(strings.TrimSpace(field.Type)) {
		case "", "text", "password", "email", "phone", "boolean", "number":
		default:
			return apperrors.New(40083, http.StatusBadRequest, "包含不支持的注册字段类型")
		}
		seen[field.Name] = true
	}
	return nil
}

func normalizeProtocolValues(values []string) []string {
	result := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	return result
}

func transportRequestAAD(meta authprotocol.RequestMetadata) []byte {
	return []byte(strings.Join([]string{
		authprotocol.TransportV2, strings.TrimSpace(meta.AppKey), strings.TrimSpace(meta.KeyID),
		strings.ToUpper(meta.Method), meta.Path, strings.TrimSpace(meta.Timestamp),
		strings.TrimSpace(meta.Nonce),
	}, "\n"))
}

// transportResponseAAD 响应 AAD：六行，`\n` 分隔，末尾无换行。
//
// 绑定 HTTP 状态码与**请求** nonce —— 前者让 200 的响应体不能被重放成 403，
// 后者把响应钉死在这一次请求上。同样有逐字节测试盯着。
func transportResponseAAD(appKey, keyID string, status int, requestNonceB64, responseNonceB64 string) []byte {
	return []byte(strings.Join([]string{
		authprotocol.TransportV2,
		appKey,
		keyID,
		strconv.Itoa(status),
		requestNonceB64,
		responseNonceB64,
	}, "\n"))
}

// deriveTransportKey 盐取自公开的 appKey 而非数字主键：
// 客户端手上本来就有 appKey，不必再从 /config 里挖内部 ID，也就少了一处类型对不齐的坑。
func deriveTransportKey(shared []byte, appKey, keyID string, aad []byte) ([]byte, error) {
	salt := sha256.Sum256([]byte(appKey + ":" + keyID))
	reader := hkdf.New(sha256.New, shared, salt[:], aad)
	key := make([]byte, chacha20poly1305.KeySize)
	_, err := io.ReadFull(reader, key)
	return key, err
}

func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}
