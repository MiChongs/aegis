package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	aidomain "aegis/internal/domain/ai"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	anthropicsse "github.com/anthropics/anthropic-sdk-go/packages/ssestream"
)

// ═══════════════════════════ Anthropic Messages（anthropic-sdk-go） ═══════════════════════════

// anthropicClient 按通道配置组装一个 SDK 客户端。
//
// 与 OpenAI 侧同一原则：baseUrl / apiKey / http.Client 全部显式来自通道配置。
// WithoutEnvironmentDefaults 必须带上 —— 这个 SDK 的默认凭据链很重
// （环境变量 / 本地 profile / 工作负载联邦），还会在无凭据时直接拒绝请求，
// 而我们的自定义兼容端点允许无密钥访问。
func (c *aiLLMClient) anthropicClient(cfg aidomain.Config, meta aidomain.ProviderMeta) (anthropic.Client, error) {
	// SDK 固定请求「{base}/v1/messages」，与供应商目录约定的「baseUrl + /messages」
	// 语义不同（目录里的 baseUrl 自带版本段，如 https://api.anthropic.com/v1）。
	// 这里按目录语义算出最终地址，在中间件里改写 —— 库中已存配置无需迁移，
	// 自定义兼容端点（任意挂载前缀）也照常工作。
	endpoint, err := url.Parse(c.baseURL(cfg, meta) + "/messages")
	if err != nil {
		return anthropic.Client{}, &aiUpstreamError{Provider: meta.Provider, Message: "端点地址无效：" + err.Error()}
	}
	options := []anthropicoption.RequestOption{
		anthropicoption.WithoutEnvironmentDefaults(),
		anthropicoption.WithHTTPClient(c.httpClient(meta.Provider)),
		// 单通道内不重试：链路层的职责就是「这家不行换下一家」。
		anthropicoption.WithMaxRetries(0),
		anthropicoption.WithMiddleware(func(request *http.Request, next anthropicoption.MiddlewareNext) (*http.Response, error) {
			target := *endpoint
			request.URL = &target
			request.Host = ""
			return next(request)
		}),
		anthropicoption.WithMiddleware(upstreamErrorMiddleware(meta.Provider)),
	}
	// 密钥留空不带鉴权头（自建兼容端点）；SDK 的 WithAPIKey 对空串也会发头，所以要判空。
	if key := cfg.Secret(aidomain.KeyAPIKey); key != "" {
		options = append(options, anthropicoption.WithAPIKey(key))
	}
	// 附加请求头最后应用，允许显式覆盖鉴权头。
	for headerKey, headerValue := range cfg.SettingMap(aidomain.KeyExtraHeaders) {
		headerKey = strings.TrimSpace(headerKey)
		if headerKey == "" {
			continue
		}
		options = append(options, anthropicoption.WithHeader(headerKey, headerValue))
	}
	return anthropic.NewClient(options...), nil
}

// buildAnthropicParams 把统一请求翻成 SDK 的 Messages 参数。
//
// Anthropic 有两条 OpenAI 没有的硬约束，都在这里消化：
//  1. max_tokens 必填 —— 缺省给一个宽裕值；
//  2. user / assistant 必须交替 —— 工具结果是 user 角色，连续同角色消息要合并。
func buildAnthropicParams(req aidomain.ChatRequest) anthropic.MessageNewParams {
	messages := make([]anthropic.MessageParam, 0, len(req.Messages))
	push := func(role anthropic.MessageParamRole, blocks ...anthropic.ContentBlockParamUnion) {
		if len(blocks) == 0 {
			return
		}
		if len(messages) > 0 && messages[len(messages)-1].Role == role {
			last := len(messages) - 1
			messages[last].Content = append(messages[last].Content, blocks...)
			return
		}
		messages = append(messages, anthropic.MessageParam{Role: role, Content: blocks})
	}

	for _, item := range req.Messages {
		switch item.Role {
		case aidomain.RoleTool:
			content := item.PlainText()
			if content == "" {
				content = "(空结果)"
			}
			push(anthropic.MessageParamRoleUser, anthropic.ContentBlockParamUnion{
				OfToolResult: &anthropic.ToolResultBlockParam{
					ToolUseID: item.ToolCallID,
					Content: []anthropic.ToolResultBlockParamContentUnion{
						{OfText: &anthropic.TextBlockParam{Text: content}},
					},
				},
			})
		case aidomain.RoleAssistant:
			blocks := make([]anthropic.ContentBlockParamUnion, 0, 1+len(item.ToolCalls))
			if text := item.PlainText(); text != "" {
				blocks = append(blocks, anthropic.NewTextBlock(text))
			}
			for _, call := range item.ToolCalls {
				var input any = map[string]any{}
				if len(call.Arguments) > 0 {
					var parsed any
					if err := json.Unmarshal(call.Arguments, &parsed); err == nil && parsed != nil {
						input = parsed
					}
				}
				blocks = append(blocks, anthropic.NewToolUseBlock(call.ID, input, call.Name))
			}
			push(anthropic.MessageParamRoleAssistant, blocks...)
		default:
			blocks := make([]anthropic.ContentBlockParamUnion, 0, len(item.Content))
			for _, part := range item.Content {
				switch part.Type {
				case aidomain.PartImage:
					blocks = append(blocks, anthropicImageBlock(part.ImageURL))
				default:
					if part.Text != "" {
						blocks = append(blocks, anthropic.NewTextBlock(part.Text))
					}
				}
			}
			push(anthropic.MessageParamRoleUser, blocks...)
		}
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 8192
	}
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: int64(maxTokens),
		Messages:  messages,
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
		params.System = []anthropic.TextBlockParam{{Text: system}}
	}
	if len(req.Tools) > 0 {
		for _, tool := range req.Tools {
			toolParam := anthropic.ToolParam{
				Name:        tool.Name,
				InputSchema: anthropicInputSchema(tool.InputSchema),
			}
			if tool.Description != "" {
				toolParam.Description = anthropic.String(tool.Description)
			}
			params.Tools = append(params.Tools, anthropic.ToolUnionParam{OfTool: &toolParam})
		}
		switch req.ToolChoice {
		case aidomain.ToolChoiceNone:
			params.ToolChoice = anthropic.ToolChoiceUnionParam{OfNone: &anthropic.ToolChoiceNoneParam{}}
		case aidomain.ToolChoiceRequired:
			params.ToolChoice = anthropic.ToolChoiceUnionParam{OfAny: &anthropic.ToolChoiceAnyParam{}}
		}
	}
	if req.Temperature != nil {
		params.Temperature = anthropic.Float(*req.Temperature)
	}
	if req.TopP != nil {
		params.TopP = anthropic.Float(*req.TopP)
	}
	if len(req.Stop) > 0 {
		params.StopSequences = req.Stop
	}
	return params
}

// anthropicInputSchema 把任意 JSON Schema 塞进 SDK 的 input_schema 参数。
// SDK 只给 properties / required 建了型，其余键（additionalProperties、
// definitions、oneOf …）走 ExtraFields 原样透传。
func anthropicInputSchema(raw json.RawMessage) anthropic.ToolInputSchemaParam {
	schema := anthropic.ToolInputSchemaParam{Properties: map[string]any{}}
	if len(raw) == 0 {
		return schema
	}
	var parsed map[string]any
	if err := json.Unmarshal(raw, &parsed); err != nil || parsed == nil {
		return schema
	}
	if properties, ok := parsed["properties"]; ok && properties != nil {
		schema.Properties = properties
	}
	if required, ok := parsed["required"].([]any); ok {
		for _, item := range required {
			if name, ok := item.(string); ok {
				schema.Required = append(schema.Required, name)
			}
		}
	}
	extras := map[string]any{}
	for key, value := range parsed {
		switch key {
		case "type", "properties", "required":
			continue
		}
		extras[key] = value
	}
	if len(extras) > 0 {
		schema.ExtraFields = extras
	}
	return schema
}

// anthropicImageBlock 图像分片：data: URL 拆成 base64 源，其余走 url 源。
func anthropicImageBlock(imageURL string) anthropic.ContentBlockParamUnion {
	if data, ok := strings.CutPrefix(imageURL, "data:"); ok {
		mediaType, encoded, found := strings.Cut(data, ";base64,")
		if found {
			// 校验一遍 base64 —— 坏数据在这里报错比在供应商那边报 400 好定位。
			if _, err := base64.StdEncoding.DecodeString(encoded); err == nil {
				return anthropic.NewImageBlockBase64(mediaType, encoded)
			}
		}
	}
	return anthropic.NewImageBlock(anthropic.URLImageSourceParam{URL: imageURL})
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
	default:
		return aidomain.FinishOther
	}
}

// mapAnthropicSDKError 把 SDK 返回的错误收敛成 aiUpstreamError。
// 非 2xx 已被中间件就地包好；SDK 自解析的错误（流内 error 事件）取信封消息；
// 其余按网络层失败处理，一律可重试。
func mapAnthropicSDKError(provider string, err error) error {
	var upstream *aiUpstreamError
	if errors.As(err, &upstream) {
		return upstream
	}
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		body := []byte(apiErr.RawJSON())
		message := parseUpstreamErrorMessage(body)
		if message == "" {
			message = summarizeOpaqueUpstreamBody(body, fmt.Sprintf("%d", apiErr.StatusCode))
		}
		if message == "" {
			message = err.Error()
		}
		return &aiUpstreamError{
			Provider: provider, Status: apiErr.StatusCode, Message: message,
			Retryable: apiErr.StatusCode != http.StatusBadRequest,
		}
	}
	return &aiUpstreamError{Provider: provider, Message: err.Error(), Retryable: true}
}

func (c *aiLLMClient) anthropicChat(ctx context.Context, cfg aidomain.Config, meta aidomain.ProviderMeta,
	req aidomain.ChatRequest, onEvent func(aidomain.StreamEvent) error) (*aidomain.ChatResponse, error) {
	client, err := c.anthropicClient(cfg, meta)
	if err != nil {
		return nil, err
	}
	params := buildAnthropicParams(req)

	if onEvent == nil {
		// SDK 的非流式路径会按 max_tokens 推算超时、超过十分钟直接拒绝；
		// 旧实现不设单请求超时（交给 ctx 与出站网关），显式给宽裕上限恢复这一语义。
		message, err := client.Messages.New(ctx, params, anthropicoption.WithRequestTimeout(time.Hour))
		if err != nil {
			return nil, mapAnthropicSDKError(meta.Provider, err)
		}
		return anthropicResponseFromMessage(message), nil
	}

	stream := client.Messages.NewStreaming(ctx, params)
	defer stream.Close()
	return consumeAnthropicStream(stream, meta.Provider, onEvent)
}

func anthropicResponseFromMessage(message *anthropic.Message) *aidomain.ChatResponse {
	result := &aidomain.ChatResponse{
		ID:           message.ID,
		Model:        string(message.Model),
		FinishReason: normalizeAnthropicFinish(string(message.StopReason)),
		Usage: aidomain.Usage{
			InputTokens:  message.Usage.InputTokens,
			OutputTokens: message.Usage.OutputTokens,
			TotalTokens:  message.Usage.InputTokens + message.Usage.OutputTokens,
		},
	}
	var text, reasoning strings.Builder
	for _, block := range message.Content {
		switch block.Type {
		case "text":
			text.WriteString(block.Text)
		case "thinking":
			reasoning.WriteString(block.Thinking)
		case "tool_use":
			arguments := json.RawMessage(block.Input)
			if len(arguments) == 0 {
				arguments = json.RawMessage("{}")
			}
			result.ToolCalls = append(result.ToolCalls, aidomain.ToolCall{
				ID: block.ID, Name: block.Name, Arguments: arguments,
			})
		}
	}
	result.Text = text.String()
	result.Reasoning = reasoning.String()
	return result
}

// consumeAnthropicStream 逐事件消费 SSE，边转发增量边聚合终态。
// 流内的 error 事件由 SDK 转成错误从 stream.Err() 冒出来，统一走错误收敛。
func consumeAnthropicStream(stream *anthropicsse.Stream[anthropic.MessageStreamEventUnion], provider string,
	onEvent func(aidomain.StreamEvent) error) (*aidomain.ChatResponse, error) {
	result := &aidomain.ChatResponse{FinishReason: aidomain.FinishOther}
	var text, reasoning strings.Builder
	type toolBuffer struct {
		id, name  string
		arguments strings.Builder
	}
	tools := map[int]*toolBuffer{}
	order := make([]int, 0, 2)

	for stream.Next() {
		event := stream.Current()
		index := int(event.Index)
		switch event.Type {
		case "message_start":
			result.ID = event.Message.ID
			result.Model = string(event.Message.Model)
			result.Usage.InputTokens = event.Message.Usage.InputTokens
		case "content_block_start":
			if event.ContentBlock.Type == "tool_use" {
				buffer := &toolBuffer{id: event.ContentBlock.ID, name: event.ContentBlock.Name}
				tools[index] = buffer
				order = append(order, index)
				if err := onEvent(aidomain.StreamEvent{
					Type: aidomain.StreamToolStart, ToolIndex: index,
					ToolID: buffer.id, ToolName: buffer.name,
				}); err != nil {
					return nil, &aiUpstreamError{Provider: provider, Message: err.Error(), Retryable: true}
				}
			}
		case "content_block_delta":
			switch event.Delta.Type {
			case "text_delta":
				text.WriteString(event.Delta.Text)
				if err := onEvent(aidomain.StreamEvent{Type: aidomain.StreamText, Delta: event.Delta.Text}); err != nil {
					return nil, &aiUpstreamError{Provider: provider, Message: err.Error(), Retryable: true}
				}
			case "thinking_delta":
				reasoning.WriteString(event.Delta.Thinking)
				if err := onEvent(aidomain.StreamEvent{Type: aidomain.StreamReasoning, Delta: event.Delta.Thinking}); err != nil {
					return nil, &aiUpstreamError{Provider: provider, Message: err.Error(), Retryable: true}
				}
			case "input_json_delta":
				if buffer, ok := tools[index]; ok {
					buffer.arguments.WriteString(event.Delta.PartialJSON)
					if err := onEvent(aidomain.StreamEvent{
						Type: aidomain.StreamToolDelta, ToolIndex: index,
						ToolID: buffer.id, ToolName: buffer.name, Delta: event.Delta.PartialJSON,
					}); err != nil {
						return nil, &aiUpstreamError{Provider: provider, Message: err.Error(), Retryable: true}
					}
				}
			}
		case "message_delta":
			if event.Delta.StopReason != "" {
				result.FinishReason = normalizeAnthropicFinish(string(event.Delta.StopReason))
			}
			if event.Usage.OutputTokens > 0 {
				result.Usage.OutputTokens = event.Usage.OutputTokens
			}
		}
	}
	if err := stream.Err(); err != nil {
		return nil, mapAnthropicSDKError(provider, err)
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
