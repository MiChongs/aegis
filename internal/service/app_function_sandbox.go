package service

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	functiondomain "aegis/internal/domain/appfunction"

	"github.com/tetratelabs/wazero"
)

const maxWASMModuleBytes = 2 << 20

// AppFunctionSandbox 在无 WASI、无宿主函数的隔离运行时中执行纯计算 WASM。
// 每次调用都实例化独立模块，避免跨应用及跨请求共享线性内存。
type AppFunctionSandbox struct{}

func NewAppFunctionSandbox() *AppFunctionSandbox {
	return &AppFunctionSandbox{}
}

func (s *AppFunctionSandbox) Validate(ctx context.Context, module []byte) error {
	if len(module) == 0 || len(module) > maxWASMModuleBytes {
		return fmt.Errorf("WASM 模块大小必须在 1 字节到 2MB 之间")
	}
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().
		WithMemoryLimitPages(256).
		WithCloseOnContextDone(true))
	defer runtime.Close(ctx)
	compiled, err := runtime.CompileModule(ctx, module)
	if err != nil {
		return fmt.Errorf("WASM 编译失败: %w", err)
	}
	defer compiled.Close(ctx)
	return nil
}

func (s *AppFunctionSandbox) Execute(ctx context.Context, module, payload []byte, maxOutput int) ([]byte, error) {
	runtime := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().
		WithMemoryLimitPages(256).
		WithCloseOnContextDone(true))
	defer runtime.Close(context.Background())

	compiled, err := runtime.CompileModule(ctx, module)
	if err != nil {
		return nil, fmt.Errorf("WASM 编译失败: %w", err)
	}
	defer compiled.Close(context.Background())
	mod, err := runtime.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithStartFunctions())
	if err != nil {
		return nil, fmt.Errorf("WASM 实例化失败: %w", err)
	}
	defer mod.Close(context.Background())

	handle := mod.ExportedFunction("handle")
	malloc := mod.ExportedFunction("malloc")
	free := mod.ExportedFunction("free")
	if handle == nil || malloc == nil || free == nil || mod.Memory() == nil {
		return nil, errors.New("WASM 必须导出 memory、malloc、free 和 handle")
	}
	allocated, err := malloc.Call(ctx, uint64(len(payload)))
	if err != nil || len(allocated) == 0 {
		return nil, fmt.Errorf("WASM 输入内存分配失败: %w", err)
	}
	inputPtr := uint32(allocated[0])
	if !mod.Memory().Write(inputPtr, payload) {
		return nil, errors.New("WASM 输入越界")
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		_, _ = free.Call(cleanupCtx, uint64(inputPtr))
	}()

	result, err := handle.Call(ctx, uint64(inputPtr), uint64(len(payload)))
	if err != nil {
		return nil, fmt.Errorf("WASM 执行失败: %w", err)
	}
	if len(result) == 0 || result[0] == 0 {
		return []byte(`{}`), nil
	}
	outputPtr := uint32(result[0])
	lengthBytes, ok := mod.Memory().Read(outputPtr, 4)
	if !ok {
		return nil, errors.New("WASM 输出长度越界")
	}
	outputLength := int(binary.LittleEndian.Uint32(lengthBytes))
	if outputLength < 0 || outputLength > maxOutput {
		return nil, errors.New("WASM 输出超过函数限制")
	}
	output, ok := mod.Memory().Read(outputPtr+4, uint32(outputLength))
	if !ok {
		return nil, errors.New("WASM 输出越界")
	}
	return append([]byte(nil), output...), nil
}

type AppFunctionHTTPExecutor struct {
	client     *http.Client
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
}

func NewAppFunctionHTTPExecutor(rootSecret string) *AppFunctionHTTPExecutor {
	seedMaterial := sha256.Sum256([]byte("aegis.app-functions.ed25519.v1\x00" + rootSecret))
	privateKey := ed25519.NewKeyFromSeed(seedMaterial[:])
	transport := &http.Transport{
		Proxy:               nil,
		DialContext:         safeFunctionDialContext,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        32,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     30 * time.Second,
	}
	return &AppFunctionHTTPExecutor{
		client: &http.Client{
			Transport: transport,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return errors.New("远程函数禁止 HTTP 重定向")
			},
		},
		privateKey: privateKey,
		publicKey:  privateKey.Public().(ed25519.PublicKey),
	}
}

func (e *AppFunctionHTTPExecutor) PublicKey() string {
	return base64.RawURLEncoding.EncodeToString(e.publicKey)
}

func (e *AppFunctionHTTPExecutor) ValidateEndpoint(ctx context.Context, rawURL string) error {
	parsed, err := parseFunctionEndpoint(rawURL)
	if err != nil {
		return err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, parsed.Hostname())
	if err != nil || len(ips) == 0 {
		return errors.New("无法解析远程函数域名")
	}
	for _, item := range ips {
		if !isPublicFunctionIP(item.IP) {
			return errors.New("远程函数地址解析到私有或保留网络")
		}
	}
	return nil
}

func (e *AppFunctionHTTPExecutor) Execute(ctx context.Context, endpoint, responsePublicKey string, payload []byte, eventID string, maxOutput int) ([]byte, error) {
	if _, err := parseFunctionEndpoint(endpoint); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return nil, err
	}
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	digest := sha256.Sum256(payload)
	signingInput := timestamp + "\n" + eventID + "\n" + hex.EncodeToString(digest[:])
	signature := ed25519.Sign(e.privateKey, []byte(signingInput))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Aegis-Event-ID", eventID)
	req.Header.Set("X-Aegis-Timestamp", timestamp)
	req.Header.Set("X-Aegis-Content-SHA256", hex.EncodeToString(digest[:]))
	req.Header.Set("X-Aegis-Signature", base64.RawURLEncoding.EncodeToString(signature))

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("远程函数请求失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("远程函数返回 HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxOutput)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxOutput {
		return nil, errors.New("远程函数响应超过限制")
	}
	if err := verifyFunctionResponse(responsePublicKey, eventID, body, resp.Header.Get("X-Aegis-Response-Signature")); err != nil {
		return nil, err
	}
	return body, nil
}

// FetchWithHeaders 供 script 运行时的 aegis.fetch 使用。
//
// 复用 Execute 的同一个 client，因此继承全部 SSRF 防护：仅 HTTPS、禁止重定向、
// 连接时重新解析 IP 并拒绝环回/私网/链路本地/云元数据地址。
// 与 Execute 不同的是不做 Ed25519 双向签名 —— 目标是任意第三方接口，
// 因此非 2xx 也照常返回状态码，由脚本自行判断。
//
// 响应头一并回传：分页游标（Link）、限流剩余（X-RateLimit-*）、内容类型这些
// 只存在于头里，拿不到它们的脚本要么猜、要么把翻页写死成一次。
func (e *AppFunctionHTTPExecutor) FetchWithHeaders(
	ctx context.Context,
	method string,
	endpoint string,
	headers map[string]string,
	body []byte,
	maxOutput int,
) (int, map[string]string, []byte, error) {
	if _, err := parseFunctionEndpoint(endpoint); err != nil {
		return 0, nil, nil, err
	}
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost,
		http.MethodPut, http.MethodPatch, http.MethodDelete:
	default:
		return 0, nil, nil, fmt.Errorf("不支持的 HTTP 方法: %s", method)
	}

	var reader io.Reader
	if len(body) > 0 {
		reader = strings.NewReader(string(body))
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return 0, nil, nil, err
	}
	if len(body) > 0 && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for name, value := range headers {
		// Host 由 URL 决定，禁止脚本覆写以免绕过域名校验
		if strings.EqualFold(name, "Host") {
			continue
		}
		req.Header.Set(name, value)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return 0, nil, nil, fmt.Errorf("出站请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 多值头只取第一个：脚本侧一个 Record<string,string> 已经覆盖了
	// 除 Set-Cookie 之外的全部实际用途，而 Set-Cookie 本来就不该回给脚本
	// （沙箱没有 cookie jar，回它只会诱导作者去手工拼会话）。
	responseHeaders := make(map[string]string, len(resp.Header))
	for name, values := range resp.Header {
		if strings.EqualFold(name, "Set-Cookie") || len(values) == 0 {
			continue
		}
		responseHeaders[name] = values[0]
	}

	payload, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxOutput)+1))
	if err != nil {
		return resp.StatusCode, responseHeaders, nil, err
	}
	if len(payload) > maxOutput {
		return resp.StatusCode, responseHeaders, nil, errors.New("出站响应超过大小限制")
	}
	return resp.StatusCode, responseHeaders, payload, nil
}

func parseFunctionEndpoint(rawURL string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return nil, errors.New("远程函数必须使用无用户信息的 HTTPS URL")
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return nil, errors.New("远程函数不允许本地域名")
	}
	if parsed.Fragment != "" {
		return nil, errors.New("远程函数 URL 不允许 fragment")
	}
	return parsed, nil
}

func safeFunctionDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return nil, errors.New("无法解析远程函数域名")
	}
	dialer := net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	var lastErr error
	for _, item := range ips {
		if !isPublicFunctionIP(item.IP) {
			return nil, errors.New("远程函数地址解析到私有或保留网络")
		}
		conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(item.IP.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	return nil, lastErr
}

func isPublicFunctionIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return false
	}
	if v4 := ip.To4(); v4 != nil {
		// 0/8、共享地址空间、文档网段、基准测试网段和保留网段。
		blocked := [][2]uint32{
			{0x00000000, 0x00ffffff}, {0x64400000, 0x647fffff},
			{0xc0000000, 0xc00000ff}, {0xc0000200, 0xc00002ff},
			{0xc6120000, 0xc613ffff}, {0xc6336400, 0xc63364ff},
			{0xcb007100, 0xcb0071ff}, {0xe9fc0000, 0xe9fc00ff},
			{0xf0000000, 0xffffffff},
		}
		value := binary.BigEndian.Uint32(v4)
		for _, span := range blocked {
			if value >= span[0] && value <= span[1] {
				return false
			}
		}
	}
	return true
}

func verifyFunctionResponse(encodedKey, eventID string, body []byte, encodedSignature string) error {
	key, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(encodedKey))
	if err != nil || len(key) != ed25519.PublicKeySize {
		return errors.New("远程函数响应公钥无效")
	}
	signature, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(encodedSignature))
	if err != nil || len(signature) != ed25519.SignatureSize {
		return errors.New("远程函数响应缺少有效签名")
	}
	digest := sha256.Sum256(body)
	message := eventID + "\n" + hex.EncodeToString(digest[:])
	valid := ed25519.Verify(ed25519.PublicKey(key), []byte(message), signature)
	if subtle.ConstantTimeByteEq(boolByte(valid), 1) != 1 {
		return errors.New("远程函数响应签名验证失败")
	}
	return nil
}

func boolByte(value bool) uint8 {
	if value {
		return 1
	}
	return 0
}

func decodeFunctionResult(raw []byte, eventID, version string) (*functiondomain.InvocationResult, error) {
	var result functiondomain.InvocationResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, errors.New("函数响应不是有效 JSON")
	}
	result.EventID = eventID
	result.Version = version
	if len(result.Output) > 0 && !json.Valid(result.Output) {
		return nil, errors.New("函数 output 不是有效 JSON")
	}
	return &result, nil
}
