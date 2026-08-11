package service

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	authprotocol "aegis/internal/domain/authprotocol"

	"github.com/google/uuid"
	"golang.org/x/crypto/chacha20poly1305"
)

// 接入自检 —— 服务端按当前安全等级，用真实 HTTP 把接入链路完整跑一遍。
//
// 这段代码同时是**三档协议的参考实现**：控制台上的示例代码、官方 SDK 与这里
// 必须逐字节一致，任何一方改了包装方式，自检会立刻红掉。
//
// 全程不产生副作用：探测登录故意用一个随机到不可能存在的账号，
// 只要服务端回的是"账号或密码错误"这类**业务**错误，就证明
// 网关拆包 → Handler → 响应封包 整条链路都是通的。

const selfTestTimeout = 12 * time.Second

func (s *AuthProtocolService) SelfTest(ctx context.Context, appKey, baseURL string) (*authprotocol.SelfTestResult, error) {
	app, policy, err := s.ResolveAppAndPolicy(ctx, appKey)
	if err != nil {
		return nil, err
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	started := time.Now()
	result := &authprotocol.SelfTestResult{
		SecurityLevel: policy.SecurityLevel,
		BaseURL:       baseURL,
		StartedAt:     started.UTC(),
		Steps:         make([]authprotocol.SelfTestStep, 0, 5),
	}
	defer func() {
		result.DurationMS = time.Since(started).Milliseconds()
		result.OK = true
		for _, step := range result.Steps {
			if !step.OK && !step.Skipped {
				result.OK = false
				break
			}
		}
	}()

	if baseURL == "" {
		result.Steps = append(result.Steps, authprotocol.SelfTestStep{
			Key: "baseUrl", Title: "确定服务地址", OK: false,
			Detail: "未能确定对外可访问的 Base URL",
			Hint:   "在控制台的接入卡片里填写对外服务地址后重试",
		})
		return result, nil
	}

	client := &http.Client{Timeout: selfTestTimeout}
	base := baseURL + "/api/v1/apps/" + app.AppKey

	// ── 1. /config 必须免包装可读 ──
	config, step := s.selfTestConfig(ctx, client, base, policy)
	result.Steps = append(result.Steps, step)
	if !step.OK {
		return result, nil
	}

	// ── 2. 应用密钥就绪（signed / sealed） ──
	secret := ""
	if policy.SecurityLevel != authprotocol.LevelStandard {
		start := time.Now()
		secret, err = s.RevealSigningSecret(ctx, app.ID)
		secretStep := authprotocol.SelfTestStep{
			Key: "secret", Title: "应用密钥就绪", OK: err == nil,
			DurationMS: time.Since(start).Milliseconds(),
		}
		if err != nil {
			secretStep.Detail = err.Error()
			secretStep.Hint = "在接入卡片里点「轮换应用密钥」生成一把新密钥"
		} else {
			secretStep.Detail = "已加载 " + policy.SigningSecretHint
		}
		result.Steps = append(result.Steps, secretStep)
		if !secretStep.OK {
			return result, nil
		}
	} else {
		result.Steps = append(result.Steps, authprotocol.SelfTestStep{
			Key: "secret", Title: "应用密钥就绪", OK: true, Skipped: true,
			Detail: "standard 等级不需要应用密钥",
		})
	}

	// ── 3. 传输公钥可用（sealed） ──
	if policy.SecurityLevel == authprotocol.LevelSealed {
		transportStep := authprotocol.SelfTestStep{Key: "transport", Title: "传输公钥可用"}
		switch {
		case config.Security.Transport == nil || config.Security.Transport.ActiveKeyID == "":
			transportStep.Detail = "/config 未下发可用的 active 传输公钥"
			transportStep.Hint = "在传输密钥卡片里点「轮换」生成一把新公钥"
		default:
			transportStep.OK = true
			transportStep.Detail = "active key = " + config.Security.Transport.ActiveKeyID
		}
		result.Steps = append(result.Steps, transportStep)
		if !transportStep.OK {
			return result, nil
		}
	} else {
		result.Steps = append(result.Steps, authprotocol.SelfTestStep{
			Key: "transport", Title: "传输公钥可用", OK: true, Skipped: true,
			Detail: policy.SecurityLevel + " 等级不使用端到端加密",
		})
	}

	// ── 4. 按等级包装一次真实登录请求（有请求体的链路） ──
	result.Steps = append(result.Steps, s.selfTestLogin(ctx, client, base, app.AppKey, secret, policy, config))

	// ── 5. 按等级包装一次真实 GET（无请求体的链路） ──
	//
	// 两条链路的包装方式不同：有 body 的走 body，没 body 的走 `?_payload=`。
	// 只跑登录会漏掉后者 —— sealed 档的 GET 曾经必然 400，正是因为自检没覆盖它。
	result.Steps = append(result.Steps, s.selfTestRead(ctx, client, base, app.AppKey, secret, policy, config))

	// ── 6. 注册开关（只读判定，不建账号） ──
	registerStep := authprotocol.SelfTestStep{Key: "register", Title: "注册入口可用", OK: true}
	switch {
	case !config.Auth.RegisterEnabled:
		registerStep.OK, registerStep.Detail = false, "应用已关闭注册"
		registerStep.Hint = "在「基本信息」里打开注册开关，或忽略本项（仅登录型应用属正常）"
	case len(config.Auth.RegisterMethods) == 0:
		registerStep.OK, registerStep.Detail = false, "未启用任何注册方式"
		registerStep.Hint = "在接入配置的「登录与注册方式」里勾选 password"
	default:
		registerStep.Detail = "已启用：" + strings.Join(config.Auth.RegisterMethods, ", ")
	}
	result.Steps = append(result.Steps, registerStep)

	return result, nil
}

func (s *AuthProtocolService) selfTestConfig(
	ctx context.Context, client *http.Client, base string, policy *authprotocol.Policy,
) (*authprotocol.Config, authprotocol.SelfTestStep) {
	start := time.Now()
	step := authprotocol.SelfTestStep{Key: "config", Title: "拉取 /config"}
	finish := func() { step.DurationMS = time.Since(start).Milliseconds() }
	defer finish()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/config", nil)
	if err != nil {
		step.Detail = err.Error()
		return nil, step
	}
	resp, err := client.Do(request)
	if err != nil {
		step.Detail = err.Error()
		step.Hint = "服务端无法回访自己的 Base URL，请确认地址与网络可达"
		return nil, step
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		step.Detail = fmt.Sprintf("HTTP %d：%s", resp.StatusCode, truncateForDisplay(body))
		return nil, step
	}
	var envelope struct {
		Data authprotocol.Config `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		step.Detail = "响应不是合法的 JSON 信封：" + err.Error()
		return nil, step
	}
	if envelope.Data.Security.Level != policy.SecurityLevel {
		step.Detail = fmt.Sprintf("下发等级 %q 与当前策略 %q 不一致",
			envelope.Data.Security.Level, policy.SecurityLevel)
		step.Hint = "可能命中了缓存，60 秒后重试"
		return nil, step
	}
	step.OK = true
	step.Detail = fmt.Sprintf("协议 %s，等级 %s", envelope.Data.ProtocolVersion, envelope.Data.Security.Level)
	return &envelope.Data, step
}

// selfTestLogin 用一个随机到不可能存在的账号发起真实登录：
// 收到业务级拒绝 = 整条链路通；收到网关级拒绝 = 包装方式对不上。
func (s *AuthProtocolService) selfTestLogin(
	ctx context.Context, client *http.Client, base, appKey, secret string,
	policy *authprotocol.Policy, config *authprotocol.Config,
) authprotocol.SelfTestStep {
	start := time.Now()
	step := authprotocol.SelfTestStep{Key: "login", Title: "登录链路连通"}
	defer func() { step.DurationMS = time.Since(start).Milliseconds() }()

	path := "/api/v1/apps/" + appKey + "/auth/login"
	payload, _ := json.Marshal(map[string]string{
		"method":   "password",
		"account":  "aegis-selftest-" + uuid.NewString(),
		"password": "Aegis-SelfTest-" + uuid.NewString(),
	})

	body := payload
	headers := map[string]string{
		"Content-Type":    "application/json",
		"X-Aegis-App-Key": appKey,
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	var requestKey []byte
	var keyID string
	if policy.SecurityLevel == authprotocol.LevelSealed {
		sealedBody, key, id, nonce, clientPublic, err := sealSelfTestPayload(
			appKey, http.MethodPost, path, timestamp, payload, config)
		if err != nil {
			step.Detail = "构造加密载荷失败：" + err.Error()
			return step
		}
		body, requestKey, keyID = sealedBody, key, id
		headers["Content-Type"] = "application/octet-stream"
		headers["X-Aegis-Protocol"] = authprotocol.TransportV2
		headers["X-Aegis-Key-Id"] = id
		headers["X-Aegis-Client-Key"] = clientPublic
		// sealed 档的 nonce 同时充当 XChaCha20 的 24 字节 nonce，必须复用同一个值。
		headers["X-Aegis-Nonce"] = nonce
	}

	if policy.SecurityLevel != authprotocol.LevelStandard {
		if headers["X-Aegis-Nonce"] == "" {
			headers["X-Aegis-Nonce"] = uuid.NewString()
		}
		headers["X-Aegis-Timestamp"] = timestamp
		headers["X-Aegis-Signature"] = SignRequest(secret, authprotocol.SignatureMetadata{
			AppKey: appKey, Method: http.MethodPost, Path: path,
			Timestamp: timestamp, Nonce: headers["X-Aegis-Nonce"], Body: body,
		})
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/auth/login", bytes.NewReader(body))
	if err != nil {
		step.Detail = err.Error()
		return step
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	resp, err := client.Do(request)
	if err != nil {
		step.Detail = err.Error()
		return step
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if policy.SecurityLevel == authprotocol.LevelSealed {
		plain, err := openSelfTestResponse(requestKey, appKey, keyID, resp, raw)
		if err != nil {
			step.Detail = "响应解密失败：" + err.Error()
			step.Hint = "服务端封包与客户端拆包规格不一致，请对照 /developers 的示例代码"
			return step
		}
		raw = plain
	}

	var envelope struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &envelope)

	// 网关级错误码：包装方式没对上，接入方会卡在这里。
	if hint, blocked := selfTestGatewayHint(envelope.Code); blocked {
		step.Detail = fmt.Sprintf("网关拒绝（%d）：%s", envelope.Code, envelope.Message)
		step.Hint = hint
		return step
	}
	if resp.StatusCode >= 500 {
		step.Detail = fmt.Sprintf("HTTP %d：%s", resp.StatusCode, truncateForDisplay(raw))
		return step
	}
	step.OK = true
	step.Detail = fmt.Sprintf("包装被正确拆解，服务端按业务规则拒绝了探测账号（%s）",
		strings.TrimSpace(envelope.Message))
	return step
}

// selfTestRead 按等级包装一次真实的 GET /me。
//
// 不带令牌，因此期望的是 40100「未认证」—— 一个**业务级**拒绝。
// 收到它就证明「网关按无请求体的规则拆包 → 到达 handler → 响应重新封包」整条通了；
// 收到网关级错误码则说明包装方式不对。与探测登录同样零副作用。
func (s *AuthProtocolService) selfTestRead(
	ctx context.Context, client *http.Client, base, appKey, secret string,
	policy *authprotocol.Policy, config *authprotocol.Config,
) authprotocol.SelfTestStep {
	start := time.Now()
	step := authprotocol.SelfTestStep{Key: "read", Title: "只读链路连通（无请求体）"}
	defer func() { step.DurationMS = time.Since(start).Milliseconds() }()

	path := "/api/v1/apps/" + appKey + "/me"
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	query := ""
	headers := map[string]string{"X-Aegis-App-Key": appKey}

	var requestKey []byte
	var keyID string
	if policy.SecurityLevel == authprotocol.LevelSealed {
		// 没有查询参数要传时，载荷就是**空串的密文** —— AEAD 对空明文照样产出
		// 16 字节 tag，于是「有没有参数」不构成分支，客户端一套代码走到底。
		sealedPayload, key, id, nonce, clientPublic, err := sealSelfTestPayload(
			appKey, http.MethodGet, path, timestamp, nil, config)
		if err != nil {
			step.Detail = "构造加密载荷失败：" + err.Error()
			return step
		}
		requestKey, keyID = key, id
		query = authprotocol.SealedPayloadParam + "=" + string(sealedPayload)
		headers["X-Aegis-Protocol"] = authprotocol.TransportV2
		headers["X-Aegis-Key-Id"] = id
		headers["X-Aegis-Client-Key"] = clientPublic
		headers["X-Aegis-Nonce"] = nonce
	}

	if policy.SecurityLevel != authprotocol.LevelStandard {
		if headers["X-Aegis-Nonce"] == "" {
			headers["X-Aegis-Nonce"] = uuid.NewString()
		}
		headers["X-Aegis-Timestamp"] = timestamp
		// query 必须进签名：密文本身就在 query 里，不签它等于让密文可被替换。
		headers["X-Aegis-Signature"] = SignRequest(secret, authprotocol.SignatureMetadata{
			AppKey: appKey, Method: http.MethodGet, Path: path, Query: query,
			Timestamp: timestamp, Nonce: headers["X-Aegis-Nonce"],
		})
	}

	target := base + "/me"
	if query != "" {
		target += "?" + query
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		step.Detail = err.Error()
		return step
	}
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	resp, err := client.Do(request)
	if err != nil {
		step.Detail = err.Error()
		return step
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if policy.SecurityLevel == authprotocol.LevelSealed {
		plain, err := openSelfTestResponse(requestKey, appKey, keyID, resp, raw)
		if err != nil {
			step.Detail = "响应解密失败：" + err.Error()
			step.Hint = "无请求体的方法把密文放在 " + authprotocol.SealedPayloadParam +
				" 查询参数里，响应封包规格与 POST 完全一致"
			return step
		}
		raw = plain
	}

	var envelope struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	_ = json.Unmarshal(raw, &envelope)
	if hint, blocked := selfTestGatewayHint(envelope.Code); blocked {
		step.Detail = fmt.Sprintf("网关拒绝（%d）：%s", envelope.Code, envelope.Message)
		step.Hint = hint
		return step
	}
	if resp.StatusCode >= 500 {
		step.Detail = fmt.Sprintf("HTTP %d：%s", resp.StatusCode, truncateForDisplay(raw))
		return step
	}
	step.OK = true
	step.Detail = fmt.Sprintf("包装被正确拆解，服务端按业务规则要求登录（%s）",
		strings.TrimSpace(envelope.Message))
	return step
}

// selfTestGatewayHint 把网关级错误码翻译成"接入方现在该改什么"。
func selfTestGatewayHint(code int) (string, bool) {
	switch code {
	case 40174, 40175:
		return "签名算错了：核对待签名字符串的换行与字段顺序，并确认用的是最新的 appSecret", true
	case 42670:
		return "该应用要求 sealed 加密载荷，请求却是明文", true
	case 40071:
		return "客户端时钟与服务端相差超过 5 分钟", true
	case 40970:
		return "nonce 重复，每个请求都必须换一个新的随机值", true
	case 40073, 40074, 40075, 40076, 40077, 40078:
		return "加密载荷构造有误：核对 AAD 七行拼接、HKDF 盐与 XChaCha20 的 24 字节 nonce", true
	case 40079, 40084:
		return "appKey 解析失败：确认路径与 X-Aegis-App-Key 头一致", true
	case 50370, 50372:
		return "服务端协议组件不可用，检查 Redis 与应用密钥是否就绪", true
	default:
		return "", false
	}
}

// sealSelfTestPayload 按 Transport v2 规格封装一次请求，返回密文与派生出的请求密钥。
//
// method 参与 AAD，因此有请求体与无请求体两条链路必须各自传入自己的方法；
// 写死 POST 会让 GET 的 AAD 对不上，表现为「密文能造出来但服务端解不开」。
func sealSelfTestPayload(appKey, method, path, timestamp string, payload []byte, config *authprotocol.Config) (
	body []byte, requestKey []byte, keyID, nonceB64, clientPublicB64 string, err error,
) {
	spec := config.Security.Transport
	if spec == nil || spec.ActiveKeyID == "" {
		return nil, nil, "", "", "", fmt.Errorf("config 未下发 active 传输公钥")
	}
	keyID = spec.ActiveKeyID
	var serverPublicB64 string
	for _, key := range spec.PublicKeys {
		if key.KeyID == keyID {
			serverPublicB64 = key.PublicKey
			break
		}
	}
	serverPublicBytes, err := base64.RawURLEncoding.DecodeString(serverPublicB64)
	if err != nil || len(serverPublicBytes) != 32 {
		return nil, nil, "", "", "", fmt.Errorf("active 公钥格式无效")
	}
	curve := ecdh.X25519()
	clientPrivate, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, "", "", "", err
	}
	serverPublic, err := curve.NewPublicKey(serverPublicBytes)
	if err != nil {
		return nil, nil, "", "", "", err
	}
	shared, err := clientPrivate.ECDH(serverPublic)
	if err != nil {
		return nil, nil, "", "", "", err
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, "", "", "", err
	}
	nonceB64 = base64.RawURLEncoding.EncodeToString(nonce)
	clientPublicB64 = base64.RawURLEncoding.EncodeToString(clientPrivate.PublicKey().Bytes())

	aad := transportRequestAAD(authprotocol.RequestMetadata{
		AppKey: appKey, KeyID: keyID, Method: method,
		Path: path, Timestamp: timestamp, Nonce: nonceB64,
	})
	requestKey, err = deriveTransportKey(shared, appKey, keyID, aad)
	if err != nil {
		return nil, nil, "", "", "", err
	}
	aead, err := chacha20poly1305.NewX(requestKey)
	if err != nil {
		return nil, nil, "", "", "", err
	}
	sealed := aead.Seal(nil, nonce, payload, aad)
	return []byte(base64.RawURLEncoding.EncodeToString(sealed)), requestKey, keyID, nonceB64, clientPublicB64, nil
}

func openSelfTestResponse(requestKey []byte, appKey, keyID string, resp *http.Response, raw []byte) ([]byte, error) {
	responseNonceB64 := strings.TrimSpace(resp.Header.Get("X-Aegis-Response-Nonce"))
	if responseNonceB64 == "" {
		return nil, fmt.Errorf("响应缺少 X-Aegis-Response-Nonce（服务端未加密返回）")
	}
	responseNonce, err := base64.RawURLEncoding.DecodeString(responseNonceB64)
	if err != nil || len(responseNonce) != chacha20poly1305.NonceSizeX {
		return nil, fmt.Errorf("响应 nonce 格式无效")
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, fmt.Errorf("响应密文不是 base64url")
	}
	responseKey := sha256.Sum256(append(append([]byte(nil), requestKey...), []byte("aegis-response-v2")...))
	aead, err := chacha20poly1305.NewX(responseKey[:])
	if err != nil {
		return nil, err
	}
	aad := []byte(fmt.Sprintf("%s\n%s\n%s\n%d\n%s\n%s",
		authprotocol.TransportV2, appKey, keyID, resp.StatusCode,
		requestNonceFromHeader(resp), responseNonceB64))
	return aead.Open(nil, responseNonce, ciphertext, aad)
}

// requestNonceFromHeader 响应 AAD 里绑定的是**请求** nonce，
// 自检把它回填到请求头上，避免再往上层传一个参数。
func requestNonceFromHeader(resp *http.Response) string {
	if resp.Request == nil {
		return ""
	}
	return strings.TrimSpace(resp.Request.Header.Get("X-Aegis-Nonce"))
}

func truncateForDisplay(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > 200 {
		return text[:200] + "…"
	}
	return text
}
