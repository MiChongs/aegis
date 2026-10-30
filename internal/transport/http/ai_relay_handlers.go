package httptransport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
	"unicode/utf8"

	aidomain "aegis/internal/domain/ai"
	"aegis/internal/domain/aigateway"
	"aegis/internal/service"
	apperrors "aegis/pkg/errors"

	"github.com/gin-gonic/gin"
)

// 用户 AI 中转 /api/ai/v1/*（docs/ai-gateway.md）。
//
// 应用的终端用户把任意 OpenAI / Anthropic SDK 的 baseURL 指到 {站点}/api/ai/v1，
// api key 填自己在 App 里领的 sk-ag- 密钥（或登录会话的访问令牌），按用量从自己的
// 免费额度、会员额度、赠送额度、积分、余额里扣费。
//
// 与 /api/apps/{appkey}/ai/v1 的区别：那一组是**接入方服务端**拿函数密钥调用，花的是应用的钱、
// 不计费；这一组是**终端用户**调用，花的是用户自己的额度。
//
// 响应一律按 OpenAI（或 Anthropic）的原生形状回，不套 Aegis 的 {code, data} 信封 ——
// SDK 只认原生形状。每个响应带 X-Aegis-Request-No，对得上用户的调用记录与扣费流水。

const (
	// aiRelayJSONLimitDefault JSON 请求体上限的缺省（AI_RELAY_MAX_BODY_BYTES 覆盖，见 config/ai_relay.go）：
	// Agent 多轮对话带着工具结果与截图，历史越滚越大。
	aiRelayJSONLimitDefault   = 32 << 20
	aiRelayMultipartLimit     = 32 << 20
	aiRelayResponseLimit      = 32 << 20
	aiRelayImageResponseLimit = 128 << 20
	// aiRelayImageOutputEstimate gpt-image 系列按 token 计价时，每张图的输出 token 按最大档估（1536 尺寸高质量）。
	aiRelayImageOutputEstimate = 6240
	aiRelayMaxImages           = 10
)

// aiRelayToken 凭据：Authorization Bearer（OpenAI SDK）、x-api-key（Anthropic SDK）、api-key（Azure 客户端）。
func aiRelayToken(c *gin.Context) string {
	authorization := strings.TrimSpace(c.GetHeader("Authorization"))
	if len(authorization) > 7 && strings.EqualFold(authorization[:7], "Bearer ") {
		return strings.TrimSpace(authorization[7:])
	}
	if key := strings.TrimSpace(c.GetHeader("x-api-key")); key != "" {
		return key
	}
	return strings.TrimSpace(c.GetHeader("api-key"))
}

func (h *Handler) aiRelayPrincipal(c *gin.Context, protocol string) (*service.AIGatewayPrincipal, bool) {
	if h.aiGateway == nil {
		writeAIRelayError(c, protocol, apperrors.New(40202, http.StatusServiceUnavailable, "AI 服务未开启"))
		return nil, false
	}
	principal, err := h.aiGateway.Authenticate(c.Request.Context(), aiRelayToken(c), c.ClientIP(), c.GetHeader("User-Agent"))
	if err != nil {
		writeAIRelayError(c, protocol, err)
		return nil, false
	}
	return principal, true
}

// aiRelayErrorShape 一个错误在 OpenAI / Anthropic 信封里的状态码、类型与错误码。
type aiRelayErrorShape struct {
	status    int
	aegisCode int
	message   string
	openAI    string
	code      string
	anthropic string
}

func aiRelayErrorOf(err error) aiRelayErrorShape {
	shape := aiRelayErrorShape{status: http.StatusBadGateway, message: err.Error()}
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) {
		shape.status, shape.aegisCode, shape.message = appErr.HTTPStatus, appErr.Code, appErr.Message
	} else if status, ok := service.AIUpstreamStatus(err); ok {
		// 上游的 4xx 多半是请求本身的问题（参数不合法、内容太长），原样告诉调用方；
		// 401 / 403 是通道凭据的问题，与调用方无关，按 502 报。
		switch {
		case status == http.StatusBadRequest || status == http.StatusRequestEntityTooLarge || status == http.StatusUnprocessableEntity:
			shape.status = status
		case status == http.StatusTooManyRequests:
			shape.status = http.StatusTooManyRequests
		default:
			shape.status = http.StatusBadGateway
		}
	}
	switch {
	case shape.status == http.StatusPaymentRequired:
		shape.openAI, shape.code, shape.anthropic = "insufficient_quota", "insufficient_quota", "billing_error"
	case shape.status == http.StatusUnauthorized:
		shape.openAI, shape.code, shape.anthropic = "authentication_error", "invalid_api_key", "authentication_error"
	case shape.status == http.StatusForbidden:
		shape.openAI, shape.code, shape.anthropic = "permission_error", "permission_denied", "permission_error"
	case shape.status == http.StatusNotFound:
		shape.openAI, shape.code, shape.anthropic = "invalid_request_error", "model_not_found", "not_found_error"
	case shape.status == http.StatusTooManyRequests:
		shape.openAI, shape.code, shape.anthropic = "rate_limit_error", "rate_limit_exceeded", "rate_limit_error"
	case shape.status == http.StatusConflict:
		shape.openAI, shape.code, shape.anthropic = "invalid_request_error", "conflict", "invalid_request_error"
	case shape.status >= 400 && shape.status < 500:
		shape.openAI, shape.code, shape.anthropic = "invalid_request_error", "invalid_request", "invalid_request_error"
	default:
		shape.openAI, shape.code, shape.anthropic = "api_error", "upstream_error", "api_error"
	}
	if shape.aegisCode == 40203 {
		shape.code = "model_not_found"
	}
	return shape
}

// aiRelayJSONLimit 本实例的 JSON 请求体上限。
func (h *Handler) aiRelayJSONLimit() int64 {
	if h.aiRelayMaxBody > 0 {
		return h.aiRelayMaxBody
	}
	return aiRelayJSONLimitDefault
}

// aiRelayErrorBody 按协议的错误信封。Anthropic 形状也带 aegis_code（官方 SDK 忽略多余字段）。
func aiRelayErrorBody(protocol string, shape aiRelayErrorShape) gin.H {
	if protocol == aidomain.ProtocolAnthropic {
		inner := gin.H{"type": shape.anthropic, "message": shape.message}
		if shape.aegisCode != 0 {
			inner["aegis_code"] = shape.aegisCode
		}
		return gin.H{"type": "error", "error": inner}
	}
	body := gin.H{"message": shape.message, "type": shape.openAI, "code": shape.code, "param": nil}
	if shape.aegisCode != 0 {
		body["aegis_code"] = shape.aegisCode
	}
	return gin.H{"error": body}
}

// writeAIRelayError 按协议的错误信封回错。
func writeAIRelayError(c *gin.Context, protocol string, err error) {
	shape := aiRelayErrorOf(err)
	c.JSON(shape.status, aiRelayErrorBody(protocol, shape))
}

// aiRelayJobError 异步任务失败时的错误，与同步调用会回的错误信封一致。
func aiRelayJobError(err error) aigateway.JobError {
	shape := aiRelayErrorOf(err)
	return aigateway.JobError{Message: shape.message, Type: shape.openAI, Code: shape.code,
		AegisCode: shape.aegisCode, Status: shape.status}
}

func (h *Handler) aiRelayOnReserved(c *gin.Context) func(string) {
	return func(requestNo string) { c.Header("X-Aegis-Request-No", requestNo) }
}

func aiRelayIdempotencyKey(c *gin.Context) string {
	return strings.TrimSpace(c.GetHeader("Idempotency-Key"))
}

// readAIRelayJSON 读一个 JSON 对象，保留未知字段（转发给上游时原样带上）。
func (h *Handler) readAIRelayJSON(c *gin.Context, protocol string) (map[string]json.RawMessage, bool) {
	limit := h.aiRelayJSONLimit()
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		writeAIRelayError(c, protocol, apperrors.New(40223, http.StatusRequestEntityTooLarge,
			fmt.Sprintf("请求体超过 %d MB 或读取失败", limit>>20)))
		return nil, false
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil || payload == nil {
		writeAIRelayError(c, protocol, apperrors.New(40223, http.StatusBadRequest, "请求体不是合法的 JSON 对象"))
		return nil, false
	}
	return payload, true
}

func rawString(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	return ""
}

func rawInt(raw json.RawMessage, fallback int64) int64 {
	var value int64
	if len(raw) > 0 && json.Unmarshal(raw, &value) == nil {
		return value
	}
	return fallback
}

// jsonBuilder 用上游模型名重写请求体里的 model。
func jsonBuilder(payload map[string]json.RawMessage) func(string) ([]byte, string, error) {
	return func(upstream string) ([]byte, string, error) {
		encoded, _ := json.Marshal(upstream)
		payload["model"] = encoded
		body, err := json.Marshal(payload)
		return body, "application/json", err
	}
}

// rewriteResponseModel 把上游 JSON 响应顶层的 model 换回对外名（不暴露上游的命名）。
func rewriteResponseModel(body []byte, model string) []byte {
	if len(body) == 0 || body[0] != '{' {
		return body
	}
	var payload map[string]json.RawMessage
	if json.Unmarshal(body, &payload) != nil {
		return body
	}
	if _, ok := payload["model"]; !ok {
		return body
	}
	encoded, _ := json.Marshal(model)
	payload["model"] = encoded
	rewritten, err := json.Marshal(payload)
	if err != nil {
		return body
	}
	return rewritten
}

// jsonDescriber 请求体的追踪摘要；model 记调用方传的对外名（请求体里的会被换成上游名）。
func jsonDescriber(payload map[string]json.RawMessage) func(aigateway.SanitizeOptions) aigateway.TraceContent {
	model := rawString(payload["model"])
	return func(opts aigateway.SanitizeOptions) aigateway.TraceContent {
		out := aigateway.SanitizeMap(payload, opts)
		if model != "" {
			out["model"] = model
		}
		return out
	}
}

func aiRelayRequestBytes(c *gin.Context) int64 {
	return max(c.Request.ContentLength, 0)
}

// aiRelayRender 把上游结果变成交给调用方的响应体。同步调用与异步任务共用，保证两条路拿到的结果一模一样。
type aiRelayRender func(result *service.AIGatewayPassthroughResult) (contentType string, body []byte)

// jsonRender 原样交回，JSON 响应顶层的 model 换回对外名。
func jsonRender(model string) aiRelayRender {
	return func(result *service.AIGatewayPassthroughResult) (string, []byte) {
		contentType := result.ContentType
		if contentType == "" {
			contentType = "application/json"
		}
		body := result.Body
		if strings.HasPrefix(contentType, "application/json") {
			body = rewriteResponseModel(body, model)
		}
		return contentType, body
	}
}

// aiRelayWantsAsync 调用方要异步任务：`Prefer: respond-async`（RFC 7240）或 `?async=true`。
func aiRelayWantsAsync(c *gin.Context) bool {
	for _, value := range c.Request.Header.Values("Prefer") {
		for _, token := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(token), "respond-async") {
				return true
			}
		}
	}
	switch strings.ToLower(strings.TrimSpace(c.Query("async"))) {
	case "1", "true", "yes":
		return true
	}
	return false
}

// relayPassthrough 原样转发的统一出口：异步任务、带保活的同步调用、普通同步调用三选一。
// keepalive 只对 JSON 响应开（保活写的是空格，JSON 允许前导空白）。
func (h *Handler) relayPassthrough(c *gin.Context, principal *service.AIGatewayPrincipal,
	input service.AIGatewayPassthroughInput, render aiRelayRender, keepalive bool) {
	ctx := c.Request.Context()
	if aiRelayWantsAsync(c) {
		job, err := h.aiGateway.SubmitJob(ctx, principal, service.AIGatewayJobInput{
			Passthrough: input, Render: render, Fail: aiRelayJobError,
		})
		if err != nil {
			writeAIRelayError(c, aidomain.ProtocolOpenAI, err)
			return
		}
		c.Header("Location", service.RelayBasePath+"/jobs/"+job.ID)
		c.JSON(http.StatusAccepted, job)
		return
	}
	if keepalive && aiRelayKeepaliveEnabled(c) {
		runAIRelayWithKeepalive(c, aidomain.ProtocolOpenAI, func(onReserved func(string)) (string, []byte, error) {
			input.OnReserved = onReserved
			result, err := h.aiGateway.RelayPassthrough(ctx, principal, input)
			if err != nil {
				return "", nil, err
			}
			contentType, body := render(result)
			return contentType, body, nil
		})
		return
	}
	result, err := h.aiGateway.RelayPassthrough(ctx, principal, input)
	if err != nil {
		writeAIRelayError(c, aidomain.ProtocolOpenAI, err)
		return
	}
	contentType, body := render(result)
	c.Data(http.StatusOK, contentType, body)
}

// ── 模型 ──

// AIRelayModels GET /api/ai/v1/models
func (h *Handler) AIRelayModels(c *gin.Context) {
	principal, ok := h.aiRelayPrincipal(c, aidomain.ProtocolOpenAI)
	if !ok {
		return
	}
	models, err := h.aiGateway.RelayModels(c.Request.Context(), principal)
	if err != nil {
		writeAIRelayError(c, aidomain.ProtocolOpenAI, err)
		return
	}
	data := make([]gin.H, 0, len(models))
	for _, model := range models {
		item := gin.H{"id": model.Model, "object": "model", "created": model.CreatedAt.Unix(), "owned_by": "aegis"}
		// 扩展字段：对话模型的能力声明，OpenAI 客户端会忽略，认得的客户端据此配置界面。
		if model.Kind == aigateway.KindChat {
			item["capabilities"] = model.Capabilities
			if model.Capabilities.ContextLength > 0 {
				item["context_length"] = model.Capabilities.ContextLength
			}
		}
		if model.MaxOutputTokens > 0 {
			item["max_output_tokens"] = model.MaxOutputTokens
		}
		data = append(data, item)
	}
	c.JSON(http.StatusOK, gin.H{"object": "list", "data": data})
}

// ── 对话 ──

// AIRelayChatCompletions POST /api/ai/v1/chat/completions
func (h *Handler) AIRelayChatCompletions(c *gin.Context) {
	principal, ok := h.aiRelayPrincipal(c, aidomain.ProtocolOpenAI)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.aiRelayJSONLimit())
	var req openAIGatewayRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		writeAIRelayError(c, aidomain.ProtocolOpenAI,
			apperrors.New(40223, http.StatusBadRequest, "请求体不是合法的 Chat Completions JSON："+err.Error()))
		return
	}
	request, err := openAIGatewayToChatRequest(req)
	if err != nil {
		writeAIRelayError(c, aidomain.ProtocolOpenAI, err)
		return
	}
	h.relayChat(c, principal, aidomain.ProtocolOpenAI, aigateway.EndpointChat, request, req.Stream)
}

// AIRelayMessages POST /api/ai/v1/messages
func (h *Handler) AIRelayMessages(c *gin.Context) {
	principal, ok := h.aiRelayPrincipal(c, aidomain.ProtocolAnthropic)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.aiRelayJSONLimit())
	var req anthropicGatewayRequest
	if err := json.NewDecoder(c.Request.Body).Decode(&req); err != nil {
		writeAIRelayError(c, aidomain.ProtocolAnthropic,
			apperrors.New(40223, http.StatusBadRequest, "请求体不是合法的 Messages JSON："+err.Error()))
		return
	}
	request, err := anthropicGatewayToChatRequest(req)
	if err != nil {
		writeAIRelayError(c, aidomain.ProtocolAnthropic, err)
		return
	}
	h.relayChat(c, principal, aidomain.ProtocolAnthropic, aigateway.EndpointMessages, request, req.Stream)
}

func (h *Handler) relayChat(c *gin.Context, principal *service.AIGatewayPrincipal, protocol, endpoint string,
	request aidomain.ChatRequest, stream bool) {
	input := service.AIGatewayChatInput{
		Endpoint: endpoint, Request: request, Stream: stream, RequestBytes: aiRelayRequestBytes(c),
		IdempotencyKey: aiRelayIdempotencyKey(c), OnReserved: h.aiRelayOnReserved(c),
	}
	writeErr := func(err error) { writeAIRelayError(c, protocol, err) }
	if !stream {
		run := func(onReserved func(string)) (string, []byte, error) {
			input.OnReserved = onReserved
			result, err := h.aiGateway.RelayChat(c.Request.Context(), principal, input, nil)
			if err != nil {
				return "", nil, err
			}
			var payload any = openAIGatewayResponse(result.Response)
			if protocol == aidomain.ProtocolAnthropic {
				payload = anthropicGatewayResponse(result.Response)
			}
			body, err := json.Marshal(payload)
			return "application/json; charset=utf-8", body, err
		}
		if aiRelayKeepaliveEnabled(c) {
			runAIRelayWithKeepalive(c, protocol, run)
			return
		}
		contentType, body, err := run(h.aiRelayOnReserved(c))
		if err != nil {
			writeErr(err)
			return
		}
		c.Data(http.StatusOK, contentType, body)
		return
	}
	// 流式：暂扣成功就开流（200 + SSE 响应头 + 心跳），之后的错误以协议里的错误事件收尾。
	run := func(begin func(string), onEvent func(aidomain.StreamEvent) error) (*aidomain.ChatResponse, error) {
		input.OnReserved = begin
		result, err := h.aiGateway.RelayChat(c.Request.Context(), principal, input, onEvent)
		if err != nil {
			return nil, err
		}
		return result.Response, nil
	}
	errorBody := func(err error) (int, gin.H) {
		shape := aiRelayErrorOf(err)
		return shape.status, aiRelayErrorBody(protocol, shape)
	}
	if protocol == aidomain.ProtocolAnthropic {
		streamAnthropicChat(c, request.Model, run, errorBody)
		return
	}
	streamOpenAIChat(c, request.Model, run, errorBody)
}

// ── 嵌入 ──

// AIRelayEmbeddings POST /api/ai/v1/embeddings
func (h *Handler) AIRelayEmbeddings(c *gin.Context) {
	principal, ok := h.aiRelayPrincipal(c, aidomain.ProtocolOpenAI)
	if !ok {
		return
	}
	payload, ok := h.readAIRelayJSON(c, aidomain.ProtocolOpenAI)
	if !ok {
		return
	}
	if len(payload["input"]) == 0 {
		writeAIRelayError(c, aidomain.ProtocolOpenAI, apperrors.New(40223, http.StatusBadRequest, "缺少 input 字段"))
		return
	}
	model := rawString(payload["model"])
	estimate := aigateway.Meters{InputTokens: max(aigateway.EstimateTokens(string(payload["input"])), 1)}
	h.relayPassthrough(c, principal, service.AIGatewayPassthroughInput{
		Endpoint: aigateway.EndpointEmbeddings, Path: "embeddings", Model: model, Build: jsonBuilder(payload),
		Describe: jsonDescriber(payload), RequestBytes: aiRelayRequestBytes(c),
		Estimate: estimate, MaxResponseBytes: aiRelayResponseLimit,
		Meter: func(_ string, body []byte) (aigateway.Meters, bool, error) {
			return aigateway.MeterEmbeddings(body, estimate)
		},
		IdempotencyKey: aiRelayIdempotencyKey(c), OnReserved: h.aiRelayOnReserved(c),
	}, jsonRender(model), true)
}

// ── 图像 ──

func imageEstimate(n int64, size, quality, prompt string) aigateway.Meters {
	return aigateway.Meters{
		Images: n, ImageSize: size, ImageQuality: quality,
		InputTokens:  aigateway.EstimateTokens(prompt),
		OutputTokens: n * aiRelayImageOutputEstimate,
	}
}

// AIRelayImageGenerations POST /api/ai/v1/images/generations
func (h *Handler) AIRelayImageGenerations(c *gin.Context) {
	principal, ok := h.aiRelayPrincipal(c, aidomain.ProtocolOpenAI)
	if !ok {
		return
	}
	payload, ok := h.readAIRelayJSON(c, aidomain.ProtocolOpenAI)
	if !ok {
		return
	}
	n := rawInt(payload["n"], 1)
	if n < 1 || n > aiRelayMaxImages {
		writeAIRelayError(c, aidomain.ProtocolOpenAI,
			apperrors.New(40223, http.StatusBadRequest, fmt.Sprintf("n 应在 1-%d 之间", aiRelayMaxImages)))
		return
	}
	model := rawString(payload["model"])
	size, quality := rawString(payload["size"]), rawString(payload["quality"])
	h.relayImages(c, principal, aigateway.EndpointImageGenerate, "images/generations", model,
		jsonBuilder(payload), jsonDescriber(payload), imageEstimate(n, size, quality, rawString(payload["prompt"])), size, quality)
}

// AIRelayImageEdits POST /api/ai/v1/images/edits —— multipart（OpenAI 原生）与 JSON（images[].image_url）都收。
func (h *Handler) AIRelayImageEdits(c *gin.Context) {
	principal, ok := h.aiRelayPrincipal(c, aidomain.ProtocolOpenAI)
	if !ok {
		return
	}
	if isMultipartRequest(c) {
		form, ok := readAIRelayMultipart(c)
		if !ok {
			return
		}
		n, err := strconv.ParseInt(form.value("n", "1"), 10, 64)
		if err != nil || n < 1 || n > aiRelayMaxImages {
			writeAIRelayError(c, aidomain.ProtocolOpenAI,
				apperrors.New(40223, http.StatusBadRequest, fmt.Sprintf("n 应在 1-%d 之间", aiRelayMaxImages)))
			return
		}
		size, quality := form.value("size", ""), form.value("quality", "")
		h.relayImages(c, principal, aigateway.EndpointImageEdit, "images/edits", form.value("model", ""),
			form.builder(nil), form.describer(), imageEstimate(n, size, quality, form.value("prompt", "")), size, quality)
		return
	}
	payload, ok := h.readAIRelayJSON(c, aidomain.ProtocolOpenAI)
	if !ok {
		return
	}
	n := rawInt(payload["n"], 1)
	if n < 1 || n > aiRelayMaxImages {
		writeAIRelayError(c, aidomain.ProtocolOpenAI,
			apperrors.New(40223, http.StatusBadRequest, fmt.Sprintf("n 应在 1-%d 之间", aiRelayMaxImages)))
		return
	}
	size, quality := rawString(payload["size"]), rawString(payload["quality"])
	h.relayImages(c, principal, aigateway.EndpointImageEdit, "images/edits", rawString(payload["model"]),
		jsonBuilder(payload), jsonDescriber(payload), imageEstimate(n, size, quality, rawString(payload["prompt"])), size, quality)
}

func (h *Handler) relayImages(c *gin.Context, principal *service.AIGatewayPrincipal, endpoint, path, model string,
	build func(string) ([]byte, string, error), describe func(aigateway.SanitizeOptions) aigateway.TraceContent,
	estimate aigateway.Meters, size, quality string) {
	h.relayPassthrough(c, principal, service.AIGatewayPassthroughInput{
		Endpoint: endpoint, Path: path, Model: model, Build: build, Estimate: estimate,
		Describe: describe, RequestBytes: aiRelayRequestBytes(c),
		MaxResponseBytes: aiRelayImageResponseLimit,
		Meter: func(_ string, body []byte) (aigateway.Meters, bool, error) {
			return aigateway.MeterImages(body, size, quality)
		},
		IdempotencyKey: aiRelayIdempotencyKey(c), OnReserved: h.aiRelayOnReserved(c),
	}, jsonRender(model), true)
}

// ── 语音 ──

// AIRelaySpeech POST /api/ai/v1/audio/speech —— 按输入文本的字符数计费。
func (h *Handler) AIRelaySpeech(c *gin.Context) {
	principal, ok := h.aiRelayPrincipal(c, aidomain.ProtocolOpenAI)
	if !ok {
		return
	}
	payload, ok := h.readAIRelayJSON(c, aidomain.ProtocolOpenAI)
	if !ok {
		return
	}
	text := rawString(payload["input"])
	if text == "" {
		writeAIRelayError(c, aidomain.ProtocolOpenAI, apperrors.New(40223, http.StatusBadRequest, "缺少 input 字段"))
		return
	}
	// 网关整段缓冲后再交回，SSE 形式的流式合成在这里没有意义。
	delete(payload, "stream_format")
	characters := int64(utf8.RuneCountInString(text))
	model := rawString(payload["model"])
	h.relayPassthrough(c, principal, service.AIGatewayPassthroughInput{
		Endpoint: aigateway.EndpointSpeech, Path: "audio/speech", Model: model, Build: jsonBuilder(payload),
		Describe: jsonDescriber(payload), RequestBytes: aiRelayRequestBytes(c),
		Estimate: aigateway.Meters{Characters: characters}, MaxResponseBytes: aiRelayResponseLimit,
		Meter: func(_ string, body []byte) (aigateway.Meters, bool, error) {
			if len(body) == 0 {
				return aigateway.Meters{}, false, aigateway.ErrEmptyResult
			}
			return aigateway.Meters{Characters: characters}, false, nil
		},
		IdempotencyKey: aiRelayIdempotencyKey(c), OnReserved: h.aiRelayOnReserved(c),
	}, func(result *service.AIGatewayPassthroughResult) (string, []byte) {
		contentType := result.ContentType
		if contentType == "" {
			contentType = "audio/mpeg"
		}
		return contentType, result.Body
	}, false)
}

// AIRelayTranscriptions POST /api/ai/v1/audio/transcriptions
func (h *Handler) AIRelayTranscriptions(c *gin.Context) {
	h.relayTranscription(c, aigateway.EndpointTranscription, "audio/transcriptions")
}

// AIRelayTranslations POST /api/ai/v1/audio/translations
func (h *Handler) AIRelayTranslations(c *gin.Context) {
	h.relayTranscription(c, aigateway.EndpointTranslation, "audio/translations")
}

// relayTranscription 转写 / 翻译按音频时长计费。客户端要 text 格式时向上游要 json（带 usage 时长），
// 再把正文作为纯文本交回 —— text 格式不带任何用量，原样转发就只能按估算计费。
func (h *Handler) relayTranscription(c *gin.Context, endpoint, path string) {
	principal, ok := h.aiRelayPrincipal(c, aidomain.ProtocolOpenAI)
	if !ok {
		return
	}
	if !isMultipartRequest(c) {
		writeAIRelayError(c, aidomain.ProtocolOpenAI,
			apperrors.New(40223, http.StatusBadRequest, "语音转写请用 multipart/form-data 上传音频文件"))
		return
	}
	form, ok := readAIRelayMultipart(c)
	if !ok {
		return
	}
	if form.fileBytes == 0 {
		writeAIRelayError(c, aidomain.ProtocolOpenAI, apperrors.New(40223, http.StatusBadRequest, "缺少音频文件（file）"))
		return
	}
	estimate := aigateway.Meters{AudioMillis: aigateway.EstimateAudioMillis(form.fileBytes)}
	wantsText := strings.EqualFold(form.value("response_format", ""), "text")
	overrides := map[string]string{}
	if wantsText {
		overrides["response_format"] = "json"
	}
	model := form.value("model", "")
	render := jsonRender(model)
	format := strings.ToLower(form.value("response_format", "json"))
	h.relayPassthrough(c, principal, service.AIGatewayPassthroughInput{
		Endpoint: endpoint, Path: path, Model: model, Build: form.builder(overrides), Estimate: estimate,
		Describe: form.describer(), RequestBytes: aiRelayRequestBytes(c),
		MaxResponseBytes: aiRelayResponseLimit,
		Meter: func(contentType string, body []byte) (aigateway.Meters, bool, error) {
			return aigateway.MeterTranscription(contentType, body, estimate)
		},
		IdempotencyKey: aiRelayIdempotencyKey(c), OnReserved: h.aiRelayOnReserved(c),
	}, func(result *service.AIGatewayPassthroughResult) (string, []byte) {
		if wantsText {
			if text, ok := aigateway.TranscriptionText(result.Body); ok {
				return "text/plain; charset=utf-8", []byte(text)
			}
		}
		return render(result)
	}, format == "json" || format == "verbose_json")
}

// ── 重排 ──

// AIRelayRerank POST /api/ai/v1/rerank（Jina / Cohere / SiliconFlow 同形）
func (h *Handler) AIRelayRerank(c *gin.Context) {
	principal, ok := h.aiRelayPrincipal(c, aidomain.ProtocolOpenAI)
	if !ok {
		return
	}
	payload, ok := h.readAIRelayJSON(c, aidomain.ProtocolOpenAI)
	if !ok {
		return
	}
	if len(payload["query"]) == 0 || len(payload["documents"]) == 0 {
		writeAIRelayError(c, aidomain.ProtocolOpenAI, apperrors.New(40223, http.StatusBadRequest, "缺少 query 或 documents 字段"))
		return
	}
	model := rawString(payload["model"])
	estimate := aigateway.Meters{InputTokens: max(aigateway.EstimateTokens(string(payload["query"]))+
		aigateway.EstimateTokens(string(payload["documents"])), 1)}
	h.relayPassthrough(c, principal, service.AIGatewayPassthroughInput{
		Endpoint: aigateway.EndpointRerank, Path: "rerank", Model: model, Build: jsonBuilder(payload),
		Describe: jsonDescriber(payload), RequestBytes: aiRelayRequestBytes(c),
		Estimate: estimate, MaxResponseBytes: aiRelayResponseLimit,
		Meter: func(_ string, body []byte) (aigateway.Meters, bool, error) {
			return aigateway.MeterRerank(body, estimate)
		},
		IdempotencyKey: aiRelayIdempotencyKey(c), OnReserved: h.aiRelayOnReserved(c),
	}, jsonRender(model), true)
}

// ── multipart ──

func isMultipartRequest(c *gin.Context) bool {
	mediaType, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	return err == nil && mediaType == "multipart/form-data"
}

// aiRelayForm 解析好的 multipart 表单，可按上游模型名重新组装。
type aiRelayForm struct {
	form      *multipart.Form
	fileBytes int64
}

func readAIRelayMultipart(c *gin.Context) (*aiRelayForm, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, aiRelayMultipartLimit)
	if err := c.Request.ParseMultipartForm(aiRelayMultipartLimit); err != nil {
		writeAIRelayError(c, aidomain.ProtocolOpenAI, apperrors.New(40223, http.StatusBadRequest,
			fmt.Sprintf("表单解析失败或超过 %d MB：%v", aiRelayMultipartLimit>>20, err)))
		return nil, false
	}
	out := &aiRelayForm{form: c.Request.MultipartForm}
	for _, files := range out.form.File {
		for _, file := range files {
			out.fileBytes += file.Size
		}
	}
	return out, true
}

func (f *aiRelayForm) value(key, fallback string) string {
	if values := f.form.Value[key]; len(values) > 0 && strings.TrimSpace(values[0]) != "" {
		return strings.TrimSpace(values[0])
	}
	return fallback
}

// builder 用上游模型名重组表单；overrides 覆盖同名字段。文件按原样（含原 Content-Type）写回。
func (f *aiRelayForm) builder(overrides map[string]string) func(string) ([]byte, string, error) {
	return func(upstream string) ([]byte, string, error) {
		var buffer bytes.Buffer
		writer := multipart.NewWriter(&buffer)
		fields := map[string]string{"model": upstream}
		for key, value := range overrides {
			fields[key] = value
		}
		for key, values := range f.form.Value {
			if _, replaced := fields[key]; replaced {
				continue
			}
			for _, value := range values {
				if err := writer.WriteField(key, value); err != nil {
					return nil, "", err
				}
			}
		}
		for key, value := range fields {
			if err := writer.WriteField(key, value); err != nil {
				return nil, "", err
			}
		}
		for key, files := range f.form.File {
			for _, file := range files {
				header := textproto.MIMEHeader{}
				header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="%s"; filename="%s"`,
					escapeQuotes(key), escapeQuotes(file.Filename)))
				contentType := file.Header.Get("Content-Type")
				if contentType == "" {
					contentType = "application/octet-stream"
				}
				header.Set("Content-Type", contentType)
				part, err := writer.CreatePart(header)
				if err != nil {
					return nil, "", err
				}
				source, err := file.Open()
				if err != nil {
					return nil, "", err
				}
				_, err = io.Copy(part, source)
				_ = source.Close()
				if err != nil {
					return nil, "", err
				}
			}
		}
		if err := writer.Close(); err != nil {
			return nil, "", err
		}
		return buffer.Bytes(), writer.FormDataContentType(), nil
	}
}

// describer 表单的追踪摘要：字段按 JSON 值同样处理，文件只记字段名、文件名、类型与大小。
func (f *aiRelayForm) describer() func(aigateway.SanitizeOptions) aigateway.TraceContent {
	return func(opts aigateway.SanitizeOptions) aigateway.TraceContent {
		out := aigateway.TraceContent{}
		fields := map[string]any{}
		for key, values := range f.form.Value {
			if len(values) == 1 {
				fields[key] = values[0]
			} else {
				items := make([]any, 0, len(values))
				for _, value := range values {
					items = append(items, value)
				}
				fields[key] = items
			}
		}
		if sanitized, ok := aigateway.SanitizeValue(fields, opts).(map[string]any); ok {
			for key, value := range sanitized {
				out[key] = value
			}
		}
		files := make([]map[string]any, 0)
		for key, headers := range f.form.File {
			for _, file := range headers {
				files = append(files, map[string]any{"field": key, "name": aigateway.TruncateText(file.Filename, 120),
					"size": file.Size, "contentType": file.Header.Get("Content-Type")})
			}
		}
		if len(files) > 0 {
			out["files"] = files
		}
		return out
	}
}

var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

func escapeQuotes(s string) string { return quoteEscaper.Replace(s) }
