package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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

func (c *aiLLMClient) baseURL(cfg aidomain.Config, meta aidomain.ProviderMeta) string {
	base := cfg.Setting(aidomain.KeyBaseURL)
	if base == "" {
		base = meta.DefaultBaseURL
	}
	return strings.TrimRight(base, "/")
}

func (c *aiLLMClient) applyExtraHeaders(cfg aidomain.Config, header http.Header) {
	for key, value := range cfg.SettingMap(aidomain.KeyExtraHeaders) {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		header.Set(key, value)
	}
}

// doRequest 发出请求并处理非 2xx。两种协议的错误体形状相同（error.message）。
func (c *aiLLMClient) doRequest(ctx context.Context, cfg aidomain.Config, meta aidomain.ProviderMeta,
	endpoint string, header http.Header, payload []byte) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, &aiUpstreamError{Provider: meta.Provider, Message: err.Error()}
	}
	request.Header = header

	response, err := c.httpClient(meta.Provider).Do(request)
	if err != nil {
		// 网络层失败一律可重试：链路的意义正是「这家断了换下一家」。
		return nil, &aiUpstreamError{Provider: meta.Provider, Message: err.Error(), Retryable: true}
	}
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		return response, nil
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	message := parseUpstreamErrorMessage(body)
	if message == "" {
		message = strings.TrimSpace(string(body))
	}
	if message == "" {
		message = response.Status
	}
	return nil, &aiUpstreamError{
		Provider: meta.Provider,
		Status:   response.StatusCode,
		Message:  message,
		// 401/403/404/408/429/5xx 都值得换下一条通道再试；
		// 400 多半是请求本身的问题，换通道会原样复发。
		Retryable: response.StatusCode != http.StatusBadRequest,
	}
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

// sseScanner SSE 事件读取器：按空行切事件，聚合 event: 与多行 data:。
type sseEvent struct {
	Event string
	Data  string
}

func readSSE(body io.Reader, onEvent func(sseEvent) error) error {
	scanner := bufio.NewScanner(body)
	// 单条 data 行可能带整段 JSON（工具入参、长正文），默认 64KB 不够。
	scanner.Buffer(make([]byte, 0, 64<<10), 4<<20)

	var event sseEvent
	var dataLines []string
	flush := func() error {
		if len(dataLines) == 0 && event.Event == "" {
			return nil
		}
		event.Data = strings.Join(dataLines, "\n")
		err := onEvent(event)
		event = sseEvent{}
		dataLines = nil
		return err
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue // 注释/心跳
		}
		if value, ok := strings.CutPrefix(line, "event:"); ok {
			event.Event = strings.TrimSpace(value)
			continue
		}
		if value, ok := strings.CutPrefix(line, "data:"); ok {
			dataLines = append(dataLines, strings.TrimPrefix(value, " "))
		}
	}
	if err := flush(); err != nil {
		return err
	}
	return scanner.Err()
}

// errStopStream 消费方主动停止（例如收到 [DONE]）。
var errStopStream = errors.New("stop stream")

// ═══════════════════════════ OpenAI Chat Completions ═══════════════════════════

type openAIMessagePayload struct {
	Role       string              `json:"role"`
	Content    any                 `json:"content,omitempty"`
	ToolCalls  []openAIToolCallOut `json:"tool_calls,omitempty"`
	ToolCallID string              `json:"tool_call_id,omitempty"`
}

type openAIToolCallOut struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// openAIEndpoint 组装最终 URL。Azure 的路径形状与标准 OpenAI 不同：
// {endpoint}/openai/deployments/{model}/chat/completions?api-version=…
func (c *aiLLMClient) openAIEndpoint(cfg aidomain.Config, meta aidomain.ProviderMeta, model string) string {
	base := c.baseURL(cfg, meta)
	if cfg.Provider != aidomain.ProviderAzureOpenAI {
		return base + "/chat/completions"
	}
	version := cfg.Setting(aidomain.KeyAPIVersion)
	if version == "" {
		version = "2024-10-21"
	}
	return fmt.Sprintf("%s/openai/deployments/%s/chat/completions?api-version=%s",
		base, url.PathEscape(model), url.QueryEscape(version))
}

func (c *aiLLMClient) openAIHeaders(cfg aidomain.Config) http.Header {
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	header.Set("Accept", "application/json")
	if key := cfg.Secret(aidomain.KeyAPIKey); key != "" {
		if cfg.Provider == aidomain.ProviderAzureOpenAI {
			header.Set("api-key", key)
		} else {
			header.Set("Authorization", "Bearer "+key)
		}
	}
	c.applyExtraHeaders(cfg, header)
	return header
}

// buildOpenAIPayload 把统一请求翻成完整的 Chat Completions 载荷。
func buildOpenAIPayload(req aidomain.ChatRequest, stream bool) (map[string]any, error) {
	messages := make([]openAIMessagePayload, 0, len(req.Messages)+1)
	if strings.TrimSpace(req.System) != "" {
		messages = append(messages, openAIMessagePayload{Role: "system", Content: req.System})
	}
	for _, message := range req.Messages {
		switch message.Role {
		case aidomain.RoleTool:
			messages = append(messages, openAIMessagePayload{
				Role: "tool", ToolCallID: message.ToolCallID, Content: message.PlainText(),
			})
		case aidomain.RoleAssistant:
			payload := openAIMessagePayload{Role: "assistant"}
			if text := message.PlainText(); text != "" {
				payload.Content = text
			}
			for _, call := range message.ToolCalls {
				out := openAIToolCallOut{ID: call.ID, Type: "function"}
				out.Function.Name = call.Name
				out.Function.Arguments = string(call.Arguments)
				if out.Function.Arguments == "" {
					out.Function.Arguments = "{}"
				}
				payload.ToolCalls = append(payload.ToolCalls, out)
			}
			messages = append(messages, payload)
		default:
			messages = append(messages, openAIMessagePayload{
				Role: message.Role, Content: openAIContent(message.Content),
			})
		}
	}

	payload := map[string]any{
		"model":    req.Model,
		"messages": messages,
	}
	if stream {
		payload["stream"] = true
		// 没有这一句，多数实现的流里不带用量 —— 而计费与压缩判定都要它。
		payload["stream_options"] = map[string]any{"include_usage": true}
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, tool := range req.Tools {
			schema := tool.InputSchema
			if len(schema) == 0 {
				schema = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			tools = append(tools, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        tool.Name,
					"description": tool.Description,
					"parameters":  schema,
				},
			})
		}
		payload["tools"] = tools
		switch req.ToolChoice {
		case aidomain.ToolChoiceNone:
			payload["tool_choice"] = "none"
		case aidomain.ToolChoiceRequired:
			payload["tool_choice"] = "required"
		}
	}
	if req.MaxTokens > 0 {
		payload["max_tokens"] = req.MaxTokens
	}
	if req.Temperature != nil {
		payload["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		payload["top_p"] = *req.TopP
	}
	if len(req.Stop) > 0 {
		payload["stop"] = req.Stop
	}
	if req.JSONMode {
		payload["response_format"] = map[string]any{"type": "json_object"}
	}
	return payload, nil
}

// openAIContent 单段纯文本压成字符串（最大兼容面），多模态才用分片数组。
func openAIContent(parts []aidomain.ContentPart) any {
	if len(parts) == 1 && parts[0].Type == aidomain.PartText {
		return parts[0].Text
	}
	out := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case aidomain.PartImage:
			out = append(out, map[string]any{
				"type":      "image_url",
				"image_url": map[string]any{"url": part.ImageURL},
			})
		default:
			out = append(out, map[string]any{"type": "text", "text": part.Text})
		}
	}
	return out
}

func normalizeOpenAIFinish(reason string) string {
	switch reason {
	case "stop":
		return aidomain.FinishStop
	case "length":
		return aidomain.FinishLength
	case "tool_calls", "function_call":
		return aidomain.FinishToolCalls
	case "content_filter":
		return aidomain.FinishFiltered
	case "":
		return aidomain.FinishOther
	default:
		return aidomain.FinishOther
	}
}

func (c *aiLLMClient) openAIChat(ctx context.Context, cfg aidomain.Config, meta aidomain.ProviderMeta,
	req aidomain.ChatRequest, onEvent func(aidomain.StreamEvent) error) (*aidomain.ChatResponse, error) {
	stream := onEvent != nil
	payload, err := buildOpenAIPayload(req, stream)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	header := c.openAIHeaders(cfg)
	if stream {
		header.Set("Accept", "text/event-stream")
	}
	response, err := c.doRequest(ctx, cfg, meta, c.openAIEndpoint(cfg, meta, req.Model), header, encoded)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if !stream {
		return parseOpenAIResponse(response.Body, meta.Provider)
	}
	return c.consumeOpenAIStream(response.Body, meta.Provider, onEvent)
}

type openAIResponseBody struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			Reasoning        string `json:"reasoning"`
			ToolCalls        []struct {
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
		TotalTokens      int64 `json:"total_tokens"`
	} `json:"usage"`
}

func parseOpenAIResponse(body io.Reader, provider string) (*aidomain.ChatResponse, error) {
	raw, err := io.ReadAll(io.LimitReader(body, 16<<20))
	if err != nil {
		return nil, &aiUpstreamError{Provider: provider, Message: err.Error(), Retryable: true}
	}
	var parsed openAIResponseBody
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, &aiUpstreamError{Provider: provider, Message: "响应不是合法 JSON：" + err.Error()}
	}
	result := &aidomain.ChatResponse{
		ID:    parsed.ID,
		Model: parsed.Model,
		Usage: aidomain.Usage{
			InputTokens:  parsed.Usage.PromptTokens,
			OutputTokens: parsed.Usage.CompletionTokens,
			TotalTokens:  parsed.Usage.TotalTokens,
		},
		FinishReason: aidomain.FinishOther,
	}
	if len(parsed.Choices) > 0 {
		choice := parsed.Choices[0]
		result.Text = choice.Message.Content
		result.Reasoning = choice.Message.ReasoningContent
		if result.Reasoning == "" {
			result.Reasoning = choice.Message.Reasoning
		}
		result.FinishReason = normalizeOpenAIFinish(choice.FinishReason)
		for _, call := range choice.Message.ToolCalls {
			arguments := call.Function.Arguments
			if strings.TrimSpace(arguments) == "" {
				arguments = "{}"
			}
			result.ToolCalls = append(result.ToolCalls, aidomain.ToolCall{
				ID: call.ID, Name: call.Function.Name, Arguments: json.RawMessage(arguments),
			})
		}
	}
	if result.Usage.TotalTokens == 0 {
		result.Usage.TotalTokens = result.Usage.InputTokens + result.Usage.OutputTokens
	}
	return result, nil
}

type openAIStreamChunk struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			Reasoning        string `json:"reasoning"`
			ToolCalls        []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
		TotalTokens      int64 `json:"total_tokens"`
	} `json:"usage"`
}

// consumeOpenAIStream 逐块消费 SSE，边转发增量边聚合终态。
func (c *aiLLMClient) consumeOpenAIStream(body io.Reader, provider string,
	onEvent func(aidomain.StreamEvent) error) (*aidomain.ChatResponse, error) {
	result := &aidomain.ChatResponse{FinishReason: aidomain.FinishOther}
	var text, reasoning strings.Builder
	// 工具调用按 index 聚合：多数实现分很多块下发 id / name / arguments。
	type toolBuffer struct {
		id, name  string
		arguments strings.Builder
		announced bool
	}
	tools := map[int]*toolBuffer{}
	order := make([]int, 0, 2)

	err := readSSE(body, func(event sseEvent) error {
		data := strings.TrimSpace(event.Data)
		if data == "" {
			return nil
		}
		if data == "[DONE]" {
			return errStopStream
		}
		var chunk openAIStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			// 某些网关会插入非标准心跳块，跳过而不是中断整条流。
			return nil
		}
		if chunk.ID != "" {
			result.ID = chunk.ID
		}
		if chunk.Model != "" {
			result.Model = chunk.Model
		}
		if chunk.Usage != nil {
			result.Usage = aidomain.Usage{
				InputTokens:  chunk.Usage.PromptTokens,
				OutputTokens: chunk.Usage.CompletionTokens,
				TotalTokens:  chunk.Usage.TotalTokens,
			}
		}
		for _, choice := range chunk.Choices {
			if choice.Delta.Content != "" {
				text.WriteString(choice.Delta.Content)
				if err := onEvent(aidomain.StreamEvent{Type: aidomain.StreamText, Delta: choice.Delta.Content}); err != nil {
					return err
				}
			}
			if delta := choice.Delta.ReasoningContent + choice.Delta.Reasoning; delta != "" {
				reasoning.WriteString(delta)
				if err := onEvent(aidomain.StreamEvent{Type: aidomain.StreamReasoning, Delta: delta}); err != nil {
					return err
				}
			}
			for _, call := range choice.Delta.ToolCalls {
				buffer, ok := tools[call.Index]
				if !ok {
					buffer = &toolBuffer{}
					tools[call.Index] = buffer
					order = append(order, call.Index)
				}
				if call.ID != "" {
					buffer.id = call.ID
				}
				if call.Function.Name != "" {
					buffer.name = call.Function.Name
				}
				if !buffer.announced && buffer.name != "" {
					buffer.announced = true
					if err := onEvent(aidomain.StreamEvent{
						Type: aidomain.StreamToolStart, ToolIndex: call.Index,
						ToolID: buffer.id, ToolName: buffer.name,
					}); err != nil {
						return err
					}
				}
				if call.Function.Arguments != "" {
					buffer.arguments.WriteString(call.Function.Arguments)
					if err := onEvent(aidomain.StreamEvent{
						Type: aidomain.StreamToolDelta, ToolIndex: call.Index,
						ToolID: buffer.id, ToolName: buffer.name, Delta: call.Function.Arguments,
					}); err != nil {
						return err
					}
				}
			}
			if choice.FinishReason != "" {
				result.FinishReason = normalizeOpenAIFinish(choice.FinishReason)
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopStream) {
		return nil, &aiUpstreamError{Provider: provider, Message: err.Error(), Retryable: true}
	}

	result.Text = text.String()
	result.Reasoning = reasoning.String()
	for index, bufferIndex := range order {
		buffer := tools[bufferIndex]
		arguments := strings.TrimSpace(buffer.arguments.String())
		if arguments == "" {
			arguments = "{}"
		}
		id := buffer.id
		if id == "" {
			id = fmt.Sprintf("call_%d", index)
		}
		result.ToolCalls = append(result.ToolCalls, aidomain.ToolCall{
			ID: id, Name: buffer.name, Arguments: json.RawMessage(arguments),
		})
	}
	if len(result.ToolCalls) > 0 && result.FinishReason == aidomain.FinishOther {
		result.FinishReason = aidomain.FinishToolCalls
	}
	if result.Usage.TotalTokens == 0 {
		result.Usage.TotalTokens = result.Usage.InputTokens + result.Usage.OutputTokens
	}
	return result, nil
}

// ═══════════════════════════ Anthropic Messages ═══════════════════════════

func (c *aiLLMClient) anthropicHeaders(cfg aidomain.Config) http.Header {
	header := http.Header{}
	header.Set("Content-Type", "application/json")
	header.Set("Accept", "application/json")
	header.Set("anthropic-version", "2023-06-01")
	if key := cfg.Secret(aidomain.KeyAPIKey); key != "" {
		header.Set("x-api-key", key)
	}
	c.applyExtraHeaders(cfg, header)
	return header
}

// buildAnthropicPayload 把统一请求翻成完整的 Messages 载荷。
//
// Anthropic 有两条 OpenAI 没有的硬约束，都在这里消化：
//  1. max_tokens 必填 —— 缺省给一个宽裕值；
//  2. user / assistant 必须交替 —— 工具结果是 user 角色，连续同角色消息要合并。
func buildAnthropicPayload(req aidomain.ChatRequest, stream bool) (map[string]any, error) {
	type block = map[string]any
	type message struct {
		Role    string  `json:"role"`
		Content []block `json:"content"`
	}

	messages := make([]message, 0, len(req.Messages))
	push := func(role string, blocks ...block) {
		if len(blocks) == 0 {
			return
		}
		if len(messages) > 0 && messages[len(messages)-1].Role == role {
			messages[len(messages)-1].Content = append(messages[len(messages)-1].Content, blocks...)
			return
		}
		messages = append(messages, message{Role: role, Content: blocks})
	}

	for _, item := range req.Messages {
		switch item.Role {
		case aidomain.RoleTool:
			content := item.PlainText()
			if content == "" {
				content = "(空结果)"
			}
			push("user", block{
				"type": "tool_result", "tool_use_id": item.ToolCallID,
				"content": []block{{"type": "text", "text": content}},
			})
		case aidomain.RoleAssistant:
			blocks := make([]block, 0, 1+len(item.ToolCalls))
			if text := item.PlainText(); text != "" {
				blocks = append(blocks, block{"type": "text", "text": text})
			}
			for _, call := range item.ToolCalls {
				var input any = map[string]any{}
				if len(call.Arguments) > 0 {
					var parsed any
					if err := json.Unmarshal(call.Arguments, &parsed); err == nil && parsed != nil {
						input = parsed
					}
				}
				blocks = append(blocks, block{
					"type": "tool_use", "id": call.ID, "name": call.Name, "input": input,
				})
			}
			push("assistant", blocks...)
		default:
			blocks := make([]block, 0, len(item.Content))
			for _, part := range item.Content {
				switch part.Type {
				case aidomain.PartImage:
					blocks = append(blocks, anthropicImageBlock(part.ImageURL))
				default:
					if part.Text != "" {
						blocks = append(blocks, block{"type": "text", "text": part.Text})
					}
				}
			}
			push("user", blocks...)
		}
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 8192
	}
	payload := map[string]any{
		"model":      req.Model,
		"max_tokens": maxTokens,
		"messages":   messages,
	}
	system := strings.TrimSpace(req.System)
	// Anthropic 没有 response_format 开关，JSON 模式翻译成 system 里的硬约束。
	if req.JSONMode {
		jsonRule := "你必须只输出一个合法的 JSON 对象，不加任何解释、前后缀或代码围栏。"
		if system == "" {
			system = jsonRule
		} else {
			system += "\n\n" + jsonRule
		}
	}
	if system != "" {
		payload["system"] = system
	}
	if stream {
		payload["stream"] = true
	}
	if len(req.Tools) > 0 {
		tools := make([]map[string]any, 0, len(req.Tools))
		for _, tool := range req.Tools {
			schema := tool.InputSchema
			if len(schema) == 0 {
				schema = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			tools = append(tools, map[string]any{
				"name":         tool.Name,
				"description":  tool.Description,
				"input_schema": schema,
			})
		}
		payload["tools"] = tools
		switch req.ToolChoice {
		case aidomain.ToolChoiceNone:
			payload["tool_choice"] = map[string]any{"type": "none"}
		case aidomain.ToolChoiceRequired:
			payload["tool_choice"] = map[string]any{"type": "any"}
		}
	}
	if req.Temperature != nil {
		payload["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		payload["top_p"] = *req.TopP
	}
	if len(req.Stop) > 0 {
		payload["stop_sequences"] = req.Stop
	}
	return payload, nil
}

// anthropicImageBlock 图像分片：data: URL 拆成 base64 源，其余走 url 源。
func anthropicImageBlock(imageURL string) map[string]any {
	if data, ok := strings.CutPrefix(imageURL, "data:"); ok {
		mediaType, encoded, found := strings.Cut(data, ";base64,")
		if found {
			// 校验一遍 base64 —— 坏数据在这里报错比在供应商那边报 400 好定位。
			if _, err := base64.StdEncoding.DecodeString(encoded); err == nil {
				return map[string]any{
					"type": "image",
					"source": map[string]any{
						"type": "base64", "media_type": mediaType, "data": encoded,
					},
				}
			}
		}
	}
	return map[string]any{
		"type":   "image",
		"source": map[string]any{"type": "url", "url": imageURL},
	}
}

func normalizeAnthropicFinish(reason string) string {
	switch reason {
	case "end_turn", "stop_sequence":
		return aidomain.FinishStop
	case "max_tokens":
		return aidomain.FinishLength
	case "tool_use":
		return aidomain.FinishToolCalls
	case "refusal":
		return aidomain.FinishFiltered
	case "":
		return aidomain.FinishOther
	default:
		return aidomain.FinishOther
	}
}

func (c *aiLLMClient) anthropicChat(ctx context.Context, cfg aidomain.Config, meta aidomain.ProviderMeta,
	req aidomain.ChatRequest, onEvent func(aidomain.StreamEvent) error) (*aidomain.ChatResponse, error) {
	stream := onEvent != nil
	payload, err := buildAnthropicPayload(req, stream)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	header := c.anthropicHeaders(cfg)
	if stream {
		header.Set("Accept", "text/event-stream")
	}
	response, err := c.doRequest(ctx, cfg, meta, c.baseURL(cfg, meta)+"/messages", header, encoded)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if !stream {
		return parseAnthropicResponse(response.Body, meta.Provider)
	}
	return c.consumeAnthropicStream(response.Body, meta.Provider, onEvent)
}

type anthropicResponseBody struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Content []struct {
		Type     string          `json:"type"`
		Text     string          `json:"text"`
		Thinking string          `json:"thinking"`
		ID       string          `json:"id"`
		Name     string          `json:"name"`
		Input    json.RawMessage `json:"input"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
	} `json:"usage"`
}

func parseAnthropicResponse(body io.Reader, provider string) (*aidomain.ChatResponse, error) {
	raw, err := io.ReadAll(io.LimitReader(body, 16<<20))
	if err != nil {
		return nil, &aiUpstreamError{Provider: provider, Message: err.Error(), Retryable: true}
	}
	var parsed anthropicResponseBody
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, &aiUpstreamError{Provider: provider, Message: "响应不是合法 JSON：" + err.Error()}
	}
	result := &aidomain.ChatResponse{
		ID:           parsed.ID,
		Model:        parsed.Model,
		FinishReason: normalizeAnthropicFinish(parsed.StopReason),
		Usage: aidomain.Usage{
			InputTokens:  parsed.Usage.InputTokens,
			OutputTokens: parsed.Usage.OutputTokens,
			TotalTokens:  parsed.Usage.InputTokens + parsed.Usage.OutputTokens,
		},
	}
	var text, reasoning strings.Builder
	for _, item := range parsed.Content {
		switch item.Type {
		case "text":
			text.WriteString(item.Text)
		case "thinking":
			reasoning.WriteString(item.Thinking)
		case "tool_use":
			arguments := item.Input
			if len(arguments) == 0 {
				arguments = json.RawMessage("{}")
			}
			result.ToolCalls = append(result.ToolCalls, aidomain.ToolCall{
				ID: item.ID, Name: item.Name, Arguments: arguments,
			})
		}
	}
	result.Text = text.String()
	result.Reasoning = reasoning.String()
	return result, nil
}

type anthropicStreamPayload struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message struct {
		ID    string `json:"id"`
		Model string `json:"model"`
		Usage struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
	ContentBlock struct {
		Type string `json:"type"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"content_block"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		PartialJSON string `json:"partial_json"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage struct {
		OutputTokens int64 `json:"output_tokens"`
	} `json:"usage"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *aiLLMClient) consumeAnthropicStream(body io.Reader, provider string,
	onEvent func(aidomain.StreamEvent) error) (*aidomain.ChatResponse, error) {
	result := &aidomain.ChatResponse{FinishReason: aidomain.FinishOther}
	var text, reasoning strings.Builder
	type toolBuffer struct {
		id, name  string
		arguments strings.Builder
	}
	tools := map[int]*toolBuffer{}
	order := make([]int, 0, 2)
	var streamErr error

	err := readSSE(body, func(event sseEvent) error {
		data := strings.TrimSpace(event.Data)
		if data == "" {
			return nil
		}
		var payload anthropicStreamPayload
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			return nil
		}
		switch payload.Type {
		case "message_start":
			result.ID = payload.Message.ID
			result.Model = payload.Message.Model
			result.Usage.InputTokens = payload.Message.Usage.InputTokens
		case "content_block_start":
			if payload.ContentBlock.Type == "tool_use" {
				buffer := &toolBuffer{id: payload.ContentBlock.ID, name: payload.ContentBlock.Name}
				tools[payload.Index] = buffer
				order = append(order, payload.Index)
				return onEvent(aidomain.StreamEvent{
					Type: aidomain.StreamToolStart, ToolIndex: payload.Index,
					ToolID: buffer.id, ToolName: buffer.name,
				})
			}
		case "content_block_delta":
			switch payload.Delta.Type {
			case "text_delta":
				text.WriteString(payload.Delta.Text)
				return onEvent(aidomain.StreamEvent{Type: aidomain.StreamText, Delta: payload.Delta.Text})
			case "thinking_delta":
				reasoning.WriteString(payload.Delta.Thinking)
				return onEvent(aidomain.StreamEvent{Type: aidomain.StreamReasoning, Delta: payload.Delta.Thinking})
			case "input_json_delta":
				if buffer, ok := tools[payload.Index]; ok {
					buffer.arguments.WriteString(payload.Delta.PartialJSON)
					return onEvent(aidomain.StreamEvent{
						Type: aidomain.StreamToolDelta, ToolIndex: payload.Index,
						ToolID: buffer.id, ToolName: buffer.name, Delta: payload.Delta.PartialJSON,
					})
				}
			}
		case "message_delta":
			if payload.Delta.StopReason != "" {
				result.FinishReason = normalizeAnthropicFinish(payload.Delta.StopReason)
			}
			if payload.Usage.OutputTokens > 0 {
				result.Usage.OutputTokens = payload.Usage.OutputTokens
			}
		case "message_stop":
			return errStopStream
		case "error":
			streamErr = &aiUpstreamError{Provider: provider, Message: payload.Error.Message, Retryable: true}
			return errStopStream
		}
		return nil
	})
	if streamErr != nil {
		return nil, streamErr
	}
	if err != nil && !errors.Is(err, errStopStream) {
		return nil, &aiUpstreamError{Provider: provider, Message: err.Error(), Retryable: true}
	}

	result.Text = text.String()
	result.Reasoning = reasoning.String()
	for _, index := range order {
		buffer := tools[index]
		arguments := strings.TrimSpace(buffer.arguments.String())
		if arguments == "" {
			arguments = "{}"
		}
		result.ToolCalls = append(result.ToolCalls, aidomain.ToolCall{
			ID: buffer.id, Name: buffer.name, Arguments: json.RawMessage(arguments),
		})
	}
	result.Usage.TotalTokens = result.Usage.InputTokens + result.Usage.OutputTokens
	return result, nil
}
