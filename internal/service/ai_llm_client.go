package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	aidomain "aegis/internal/domain/ai"
	"aegis/pkg/egress"

	"go.uber.org/zap"
)

// aiLLMClient 是两种线上协议（OpenAI Chat Completions / Anthropic Messages）的
// 完整客户端。所有供应商都归一到这两种协议之一，客户端因此只有两条代码路径 ——
// 新增供应商不新增代码。
//
// 协议实现基于官方 SDK（openai-go / anthropic-sdk-go），请求编排、SSE 解码、
// 类型安全交给 SDK；端点语义、鉴权行为、错误收敛必须与既有配置兼容，
// 这些差异全部在 ai_llm_openai.go / ai_llm_anthropic.go 的客户端组装处消化。
//
// 出网走 egress 网关：配好境外线路后，OpenAI / Anthropic 的调用与
// 支付、OAuth 走同一张路由表，不必单独给 AI 配代理。
type aiLLMClient struct {
	log *zap.Logger
}

func newAILLMClient(log *zap.Logger) *aiLLMClient {
	return &aiLLMClient{log: log}
}

// aiUpstreamError 供应商侧的失败。Retryable 决定链路要不要试下一条通道。
type aiUpstreamError struct {
	Provider  string
	Status    int
	Message   string
	Retryable bool
}

func (e *aiUpstreamError) Error() string {
	if e.Status > 0 {
		return fmt.Sprintf("%s 上游返回 %d：%s", e.Provider, e.Status, e.Message)
	}
	return fmt.Sprintf("%s 调用失败：%s", e.Provider, e.Message)
}

// httpClient 每次调用新建（egress transport 内部有连接池，客户端本身是轻量壳）。
// Timeout 恒为 0：流式响应可以持续几分钟，整体超时交给 ctx 控制；
// 响应头超时单独给足 —— 推理模型出第一个字前会思考很久。
func (c *aiLLMClient) httpClient(provider string) *http.Client {
	return egress.NewClient(egress.Profile{
		Name:                  "ai." + provider,
		Timeout:               0,
		DialTimeout:           15 * time.Second,
		ResponseHeaderTimeout: 5 * time.Minute,
	})
}

// Chat 非流式调用。
func (c *aiLLMClient) Chat(ctx context.Context, cfg aidomain.Config, req aidomain.ChatRequest) (*aidomain.ChatResponse, error) {
	meta, ok := aidomain.ProviderByKey(cfg.Provider)
	if !ok {
		return nil, &aiUpstreamError{Provider: cfg.Provider, Message: "未知的供应商"}
	}
	switch meta.Protocol {
	case aidomain.ProtocolAnthropic:
		return c.anthropicChat(ctx, cfg, meta, req, nil)
	default:
		return c.openAIChat(ctx, cfg, meta, req, nil)
	}
}

// ChatStream 流式调用：增量经 onEvent 回调，返回聚合好的完整结论。
func (c *aiLLMClient) ChatStream(ctx context.Context, cfg aidomain.Config, req aidomain.ChatRequest,
	onEvent func(aidomain.StreamEvent) error) (*aidomain.ChatResponse, error) {
	meta, ok := aidomain.ProviderByKey(cfg.Provider)
	if !ok {
		return nil, &aiUpstreamError{Provider: cfg.Provider, Message: "未知的供应商"}
	}
	if onEvent == nil {
		onEvent = func(aidomain.StreamEvent) error { return nil }
	}
	switch meta.Protocol {
	case aidomain.ProtocolAnthropic:
		return c.anthropicChat(ctx, cfg, meta, req, onEvent)
	default:
		return c.openAIChat(ctx, cfg, meta, req, onEvent)
	}
}

// ── 公共 ──

// baseURL 端点根地址：通道配置优先，留空回落到目录默认值。
// 语义与供应商目录的帮助文案一致 —— OpenAI 协议在其后拼 /chat/completions，
// Anthropic 协议拼 /messages，Azure 拼部署路径。
func (c *aiLLMClient) baseURL(cfg aidomain.Config, meta aidomain.ProviderMeta) string {
	base := cfg.Setting(aidomain.KeyBaseURL)
	if base == "" {
		base = meta.DefaultBaseURL
	}
	return strings.TrimRight(base, "/")
}

// upstreamErrorMiddleware 在 SDK 的请求链路里把非 2xx 就地收敛成 aiUpstreamError。
//
// 不走 SDK 自带的错误解析有两个原因：
//  1. SDK 只认标准错误信封（{"error":{…}}），Cloudflare 拦截页、网关的裸文本
//     这类响应会退化成一句「JSON 解析失败」，状态码与响应体全丢；
//  2. 错误消息要过 summarizeOpaqueUpstreamBody 收敛成一句人话，
//     不能让整页 HTML 顺着连通性测试与对话接口灌进控制台。
//
// 两个 SDK 的 Middleware 都是相同形状的函数别名，这里返回裸函数类型即可同时满足。
func upstreamErrorMiddleware(provider string) func(*http.Request, func(*http.Request) (*http.Response, error)) (*http.Response, error) {
	return func(request *http.Request, next func(*http.Request) (*http.Response, error)) (*http.Response, error) {
		response, err := next(request)
		if err != nil || response == nil {
			return response, err
		}
		if response.StatusCode >= 200 && response.StatusCode < 300 {
			return response, nil
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
		message := parseUpstreamErrorMessage(body)
		if message == "" {
			message = summarizeOpaqueUpstreamBody(body, response.Status)
		}
		if message == "" {
			message = response.Status
		}
		return nil, &aiUpstreamError{
			Provider: provider,
			Status:   response.StatusCode,
			Message:  message,
			// 401/403/404/408/429/5xx 都值得换下一条通道再试；
			// 400 多半是请求本身的问题，换通道会原样复发。
			Retryable: response.StatusCode != http.StatusBadRequest,
		}
	}
}

// summarizeOpaqueUpstreamBody 把无法按 API 错误信封解析的响应体收敛成一句人话。
//
// 反例是 Cloudflare 的「Just a moment…」挑战页：一整页 HTML 若原样进错误信息，
// 会顺着连通性测试与对话接口灌进控制台界面。HTML 一律不外传，只报状态与
// 拦截特征；纯文本截断到一眼能读完的长度。
func summarizeOpaqueUpstreamBody(body []byte, status string) string {
	text := strings.TrimSpace(string(body))
	if text == "" {
		return ""
	}
	head := strings.ToLower(text)
	if len(head) > 512 {
		head = head[:512]
	}
	if strings.HasPrefix(head, "<!doctype") || strings.HasPrefix(head, "<html") ||
		strings.Contains(head, "<head") {
		lower := strings.ToLower(text)
		if strings.Contains(lower, "cloudflare") || strings.Contains(lower, "just a moment") ||
			strings.Contains(lower, "cf_chl") || strings.Contains(lower, "cf-ray") {
			return fmt.Sprintf("上游返回 Cloudflare 人机验证页（HTTP %s）：服务端调用被拦截，请更换 API 地址或联系服务商关闭验证", status)
		}
		return fmt.Sprintf("上游返回 HTML 页面而非 API 响应（HTTP %s）：请检查 API 地址是否正确", status)
	}
	return truncateRunes(text, 300)
}

func parseUpstreamErrorMessage(body []byte) string {
	var envelope struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ""
	}
	if strings.TrimSpace(envelope.Error.Message) != "" {
		return envelope.Error.Message
	}
	return strings.TrimSpace(envelope.Message)
}
