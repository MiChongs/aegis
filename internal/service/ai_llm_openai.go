package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	aidomain "aegis/internal/domain/ai"

	openai "github.com/openai/openai-go/v3"
	openaioption "github.com/openai/openai-go/v3/option"
	openaisse "github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/shared"

	"github.com/openai/openai-go/v3/packages/respjson"
)

// ═══════════════════════════ OpenAI Chat Completions（openai-go） ═══════════════════════════
//
// 国内外网关式供应商（DeepSeek / Kimi / GLM / 通义 / OpenRouter / Ollama …）
// 暴露的都是这一协议，这条路径必须对「兼容端点」保持最大容错：
// 端点地址原样尊重通道配置、密钥留空不带鉴权头、reasoning 扩展字段照收。

// openAIClient 按通道配置组装一个 SDK 客户端。
//
// 三件事必须显式钉死，不能依赖 SDK 默认值：
//  1. baseUrl / apiKey / http.Client 全部来自通道配置 —— SDK 会自动读
//     OPENAI_API_KEY / OPENAI_BASE_URL 等环境变量，多租户服务进程的
//     环境变量绝不能混进租户请求；
//  2. 单通道内不重试：链路层的职责就是「这家不行换下一家」，
//     SDK 默认的两次重试只会拖慢切换；
//  3. 非 2xx 由 upstreamErrorMiddleware 收敛，绕开 SDK 只认标准信封的错误解析。
func (c *aiLLMClient) openAIClient(cfg aidomain.Config, meta aidomain.ProviderMeta, model string) openai.Client {
	options := []openaioption.RequestOption{
		openaioption.WithHTTPClient(c.httpClient(meta.Provider)),
		openaioption.WithMaxRetries(0),
		openaioption.WithMiddleware(upstreamErrorMiddleware(meta.Provider)),
		// 环境里可能残留 OPENAI_ORG_ID / OPENAI_PROJECT_ID，对应的头一并挡掉。
		openaioption.WithHeaderDel("OpenAI-Organization"),
		openaioption.WithHeaderDel("OpenAI-Project"),
	}

	base := c.baseURL(cfg, meta)
	key := cfg.Secret(aidomain.KeyAPIKey)
	if cfg.Provider == aidomain.ProviderAzureOpenAI {
		// Azure 的路径形状与标准 OpenAI 不同：
		// {endpoint}/openai/deployments/{model}/chat/completions?api-version=…
		// SDK 以「BaseURL + 相对路径 chat/completions」解析最终地址，
		// 把部署段并进 BaseURL、版本参数挂查询串即可复原这一形状。
		version := cfg.Setting(aidomain.KeyAPIVersion)
		if version == "" {
			version = "2024-10-21"
		}
		options = append(options,
			openaioption.WithBaseURL(base+"/openai/deployments/"+url.PathEscape(model)+"/"),
			openaioption.WithQuery("api-version", version),
			// Azure 用 api-key 头鉴权；显式清空 APIKey，防止环境变量补出 Bearer 头。
			openaioption.WithAPIKey(""),
		)
		if key != "" {
			options = append(options, openaioption.WithHeader("api-key", key))
		}
	} else {
		options = append(options,
			// 末尾斜杠必须有：SDK 按相对引用解析路径，「…/v1」缺斜杠会被吞掉最后一段。
			openaioption.WithBaseURL(base+"/"),
			// 空串等于不带鉴权头（本地 Ollama、内网网关），同时覆盖环境变量。
			openaioption.WithAPIKey(key),
		)
	}

	// 附加请求头最后应用，允许显式覆盖鉴权头（网关代签之类的场景）。
	for headerKey, headerValue := range cfg.SettingMap(aidomain.KeyExtraHeaders) {
		headerKey = strings.TrimSpace(headerKey)
		if headerKey == "" {
			continue
		}
		options = append(options, openaioption.WithHeader(headerKey, headerValue))
	}
	return openai.NewClient(options...)
}

// buildOpenAIParams 把统一请求翻成 SDK 的 Chat Completions 参数。
func buildOpenAIParams(req aidomain.ChatRequest) openai.ChatCompletionNewParams {
	messages := make([]openai.ChatCompletionMessageParamUnion, 0, len(req.Messages)+1)
	if strings.TrimSpace(req.System) != "" {
		messages = append(messages, openai.SystemMessage(req.System))
	}
	for _, item := range req.Messages {
		switch item.Role {
		case aidomain.RoleTool:
			messages = append(messages, openai.ToolMessage(item.PlainText(), item.ToolCallID))
		case aidomain.RoleAssistant:
			assistant := openai.ChatCompletionAssistantMessageParam{}
			if text := item.PlainText(); text != "" {
				assistant.Content.OfString = openai.String(text)
			}
			for _, call := range item.ToolCalls {
				arguments := string(call.Arguments)
				if strings.TrimSpace(arguments) == "" {
					arguments = "{}"
				}
				assistant.ToolCalls = append(assistant.ToolCalls, openai.ChatCompletionMessageToolCallUnionParam{
					OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
						ID: call.ID,
						Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
							Name:      call.Name,
							Arguments: arguments,
						},
					},
				})
			}
			messages = append(messages, openai.ChatCompletionMessageParamUnion{OfAssistant: &assistant})
		case aidomain.RoleSystem:
			messages = append(messages, openai.SystemMessage(item.PlainText()))
		default:
			messages = append(messages, openAIUserMessage(item.Content))
		}
	}

	params := openai.ChatCompletionNewParams{
		Model:    shared.ChatModel(req.Model),
		Messages: messages,
	}
	if len(req.Tools) > 0 {
		for _, tool := range req.Tools {
			parameters := shared.FunctionParameters{}
			if len(tool.InputSchema) > 0 {
				_ = json.Unmarshal(tool.InputSchema, &parameters)
			}
			if len(parameters) == 0 {
				parameters = shared.FunctionParameters{
					"type": "object", "properties": map[string]any{},
				}
			}
			definition := shared.FunctionDefinitionParam{Name: tool.Name, Parameters: parameters}
			if tool.Description != "" {
				definition.Description = openai.String(tool.Description)
			}
			params.Tools = append(params.Tools, openai.ChatCompletionFunctionTool(definition))
		}
		switch req.ToolChoice {
		case aidomain.ToolChoiceNone:
			params.ToolChoice = openai.ChatCompletionToolChoiceOptionUnionParam{OfAuto: openai.String("none")}
		case aidomain.ToolChoiceRequired:
			params.ToolChoice = openai.ChatCompletionToolChoiceOptionUnionParam{OfAuto: openai.String("required")}
		}
	}
	if req.MaxTokens > 0 {
		params.MaxTokens = openai.Int(int64(req.MaxTokens))
	}
	if req.Temperature != nil {
		params.Temperature = openai.Float(*req.Temperature)
	}
	if req.TopP != nil {
		params.TopP = openai.Float(*req.TopP)
	}
	if len(req.Stop) > 0 {
		params.Stop = openai.ChatCompletionNewParamsStopUnion{OfStringArray: req.Stop}
	}
	if req.JSONMode {
		params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
			OfJSONObject: &shared.ResponseFormatJSONObjectParam{},
		}
	}
	return params
}

// openAIUserMessage 单段纯文本压成字符串（最大兼容面），多模态才用分片数组。
func openAIUserMessage(parts []aidomain.ContentPart) openai.ChatCompletionMessageParamUnion {
	if len(parts) == 1 && parts[0].Type == aidomain.PartText {
		return openai.UserMessage(parts[0].Text)
	}
	out := make([]openai.ChatCompletionContentPartUnionParam, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case aidomain.PartImage:
			out = append(out, openai.ImageContentPart(
				openai.ChatCompletionContentPartImageImageURLParam{URL: part.ImageURL}))
		default:
			out = append(out, openai.TextContentPart(part.Text))
		}
	}
	return openai.UserMessage(out)
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
	default:
		return aidomain.FinishOther
	}
}

// mapOpenAISDKError 把 SDK 返回的错误收敛成 aiUpstreamError。
// 非 2xx 已被中间件就地包好，这里只需透传；其余（连接失败、流中断、ctx 取消）
// 一律按网络层失败处理 —— 可重试，链路的意义正是「这家断了换下一家」。
func mapOpenAISDKError(provider string, err error) error {
	var upstream *aiUpstreamError
	if errors.As(err, &upstream) {
		return upstream
	}
	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		body := []byte(apiErr.RawJSON())
		message := strings.TrimSpace(apiErr.Message)
		if message == "" {
			message = parseUpstreamErrorMessage(body)
		}
		if message == "" {
			message = summarizeOpaqueUpstreamBody(body, fmt.Sprintf("%d", apiErr.StatusCode))
		}
		if message == "" {
			message = err.Error()
		}
		return &aiUpstreamError{
			Provider: provider, Status: apiErr.StatusCode, Message: message,
			Retryable: apiErr.StatusCode != 400,
		}
	}
	return &aiUpstreamError{Provider: provider, Message: err.Error(), Retryable: true}
}

func (c *aiLLMClient) openAIChat(ctx context.Context, cfg aidomain.Config, meta aidomain.ProviderMeta,
	req aidomain.ChatRequest, onEvent func(aidomain.StreamEvent) error) (*aidomain.ChatResponse, error) {
	client := c.openAIClient(cfg, meta, req.Model)
	params := buildOpenAIParams(req)

	if onEvent == nil {
		completion, err := client.Chat.Completions.New(ctx, params)
		if err != nil {
			return nil, mapOpenAISDKError(meta.Provider, err)
		}
		return openAIResponseFromCompletion(completion), nil
	}

	// 没有这一句，多数实现的流里不带用量 —— 而计费与压缩判定都要它。
	params.StreamOptions = openai.ChatCompletionStreamOptionsParam{IncludeUsage: openai.Bool(true)}
	stream := client.Chat.Completions.NewStreaming(ctx, params)
	defer stream.Close()
	return consumeOpenAIStream(stream, meta.Provider, onEvent)
}

func openAIResponseFromCompletion(completion *openai.ChatCompletion) *aidomain.ChatResponse {
	result := &aidomain.ChatResponse{
		ID:    completion.ID,
		Model: completion.Model,
		Usage: aidomain.Usage{
			InputTokens:  completion.Usage.PromptTokens,
			OutputTokens: completion.Usage.CompletionTokens,
			TotalTokens:  completion.Usage.TotalTokens,
		},
		FinishReason: aidomain.FinishOther,
	}
	if len(completion.Choices) > 0 {
		choice := completion.Choices[0]
		result.Text = choice.Message.Content
		result.Reasoning = extraFieldText(choice.Message.JSON.ExtraFields, "reasoning_content", "reasoning")
		result.FinishReason = normalizeOpenAIFinish(string(choice.FinishReason))
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
	return result
}

// consumeOpenAIStream 逐块消费 SSE，边转发增量边聚合终态。
func consumeOpenAIStream(stream *openaisse.Stream[openai.ChatCompletionChunk], provider string,
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

	for stream.Next() {
		chunk := stream.Current()
		if chunk.ID != "" {
			result.ID = chunk.ID
		}
		if chunk.Model != "" {
			result.Model = chunk.Model
		}
		if chunk.JSON.Usage.Valid() {
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
					return nil, &aiUpstreamError{Provider: provider, Message: err.Error(), Retryable: true}
				}
			}
			// reasoning_content / reasoning 是 DeepSeek、OpenRouter 一系的扩展字段，
			// 不在官方类型里 —— SDK 把未识别字段留在 ExtraFields，从那里捞。
			if delta := extraFieldText(choice.Delta.JSON.ExtraFields, "reasoning_content", "reasoning"); delta != "" {
				reasoning.WriteString(delta)
				if err := onEvent(aidomain.StreamEvent{Type: aidomain.StreamReasoning, Delta: delta}); err != nil {
					return nil, &aiUpstreamError{Provider: provider, Message: err.Error(), Retryable: true}
				}
			}
			for _, call := range choice.Delta.ToolCalls {
				index := int(call.Index)
				buffer, ok := tools[index]
				if !ok {
					buffer = &toolBuffer{}
					tools[index] = buffer
					order = append(order, index)
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
						Type: aidomain.StreamToolStart, ToolIndex: index,
						ToolID: buffer.id, ToolName: buffer.name,
					}); err != nil {
						return nil, &aiUpstreamError{Provider: provider, Message: err.Error(), Retryable: true}
					}
				}
				if call.Function.Arguments != "" {
					buffer.arguments.WriteString(call.Function.Arguments)
					if err := onEvent(aidomain.StreamEvent{
						Type: aidomain.StreamToolDelta, ToolIndex: index,
						ToolID: buffer.id, ToolName: buffer.name, Delta: call.Function.Arguments,
					}); err != nil {
						return nil, &aiUpstreamError{Provider: provider, Message: err.Error(), Retryable: true}
					}
				}
			}
			if choice.FinishReason != "" {
				result.FinishReason = normalizeOpenAIFinish(string(choice.FinishReason))
			}
		}
	}
	if err := stream.Err(); err != nil {
		return nil, mapOpenAISDKError(provider, err)
	}

	result.Text = text.String()
	result.Reasoning = reasoning.String()
	for fallback, index := range order {
		buffer := tools[index]
		arguments := strings.TrimSpace(buffer.arguments.String())
		if arguments == "" {
			arguments = "{}"
		}
		id := buffer.id
		if id == "" {
			id = fmt.Sprintf("call_%d", fallback)
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

// extraFieldText 从 SDK 响应的未识别字段里取字符串值，按给定键序取第一个非空。
func extraFieldText(fields map[string]respjson.Field, keys ...string) string {
	for _, key := range keys {
		field, ok := fields[key]
		if !ok || !field.Valid() {
			continue
		}
		var value string
		if err := json.Unmarshal([]byte(field.Raw()), &value); err == nil && value != "" {
			return value
		}
	}
	return ""
}
