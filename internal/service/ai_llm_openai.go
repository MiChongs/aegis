package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

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

// buildOpenAIParams 把统一请求翻成 SDK 的 Chat Completions 参数。[provider] 决定思考参数、
// 输出上限与思考回传的写法（各家兼容端点在这几处各不相同）。
func buildOpenAIParams(req aidomain.ChatRequest, provider string) openai.ChatCompletionNewParams {
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
			// 思考模式的工具循环：发起调用那一轮的思考要带回（DeepSeek、Kimi、GLM 等的硬约束）。
			// 只带有工具调用的那几轮，其余轮次的思考对上游没有用，只会多花输入 token。
			if key := openAIReasoningEchoKey(provider); key != "" && len(item.ToolCalls) > 0 && item.Reasoning != "" {
				assistant.SetExtraFields(map[string]any{key: item.Reasoning})
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
		// OpenAI 的推理模型只认 max_completion_tokens（max_tokens 直接 400），其余新模型两个都认；
		// 兼容端点大多只认 max_tokens。
		if openAIOfficial(provider) {
			params.MaxCompletionTokens = openai.Int(int64(req.MaxTokens))
		} else {
			params.MaxTokens = openai.Int(int64(req.MaxTokens))
		}
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
	applyOpenAIReasoning(&params, provider, req.Reasoning)
	return params
}

// openAIOfficial OpenAI 自家（含 Azure）：参数按官方最新约定。
func openAIOfficial(provider string) bool {
	return provider == aidomain.ProviderOpenAI || provider == aidomain.ProviderAzureOpenAI
}

// openAIReasoningEchoKey 工具循环里回传上一轮思考用的字段名；空串表示这家不收（官方 OpenAI 会拒绝未知字段）。
func openAIReasoningEchoKey(provider string) string {
	switch provider {
	case aidomain.ProviderOpenAI, aidomain.ProviderAzureOpenAI, aidomain.ProviderGroq, aidomain.ProviderXAI:
		return ""
	case aidomain.ProviderOpenRouter:
		return "reasoning"
	default:
		return "reasoning_content"
	}
}

// applyOpenAIReasoning 思考设置 → 各家兼容端点的写法。没给思考设置时什么都不带，交给上游默认。
//
//   - OpenAI / Azure / Groq / xAI / Ollama / 聚合站：reasoning_effort；
//   - OpenRouter：reasoning 对象（effort / max_tokens / enabled）；
//   - DeepSeek、智谱 GLM：thinking.type 开关；
//   - 豆包（火山方舟）：thinking.type 开关，另带 reasoning_effort（Seed 系列认）；
//   - 通义千问（百炼兼容模式）、硅基流动：enable_thinking 开关与 thinking_budget；
//   - Kimi：由型号决定（k2-thinking），没有开关。
func applyOpenAIReasoning(params *openai.ChatCompletionNewParams, provider string, reasoning *aidomain.ReasoningConfig) {
	if reasoning == nil {
		return
	}
	effort := reasoning.EffectiveEffort()
	extras := map[string]any{}
	thinkingType := "disabled"
	if reasoning.On() {
		thinkingType = "enabled"
	}
	switch provider {
	case aidomain.ProviderMoonshot:
		return
	case aidomain.ProviderOpenRouter:
		switch {
		case reasoning.Off():
			extras["reasoning"] = map[string]any{"enabled": false}
		case reasoning.BudgetTokens > 0:
			extras["reasoning"] = map[string]any{"max_tokens": reasoning.BudgetTokens}
		case effort != "":
			extras["reasoning"] = map[string]any{"effort": effort}
		default:
			extras["reasoning"] = map[string]any{"enabled": true}
		}
	case aidomain.ProviderDeepSeek, aidomain.ProviderZhipu:
		extras["thinking"] = map[string]any{"type": thinkingType}
	case aidomain.ProviderDoubao:
		extras["thinking"] = map[string]any{"type": thinkingType}
		if reasoning.On() && effort != "" {
			params.ReasoningEffort = shared.ReasoningEffort(clampEffort(effort, aidomain.ReasoningMinimal, aidomain.ReasoningHigh))
		}
	case aidomain.ProviderQwen, aidomain.ProviderSiliconFlow:
		extras["enable_thinking"] = reasoning.On()
		if reasoning.On() && (reasoning.BudgetTokens > 0 || effort != "") {
			extras["thinking_budget"] = reasoning.Budget()
		}
	default:
		if effort != "" {
			params.ReasoningEffort = shared.ReasoningEffort(effort)
		}
	}
	if len(extras) > 0 {
		params.SetExtraFields(extras)
	}
}

// clampEffort 把强度压进 [low, high] 之间（某家只认其中几档时用）。
func clampEffort(effort, low, high string) string {
	rank := func(value string) int {
		for index, item := range aidomain.ReasoningEfforts {
			if item == value {
				return index
			}
		}
		return -1
	}
	switch {
	case rank(effort) < rank(low):
		return low
	case rank(effort) > rank(high):
		return high
	}
	return effort
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
	case "stop", "end_turn", "stop_sequence", "eos":
		return aidomain.FinishStop
	case "length", "max_tokens":
		return aidomain.FinishLength
	case "tool_calls", "function_call", "tool_use":
		return aidomain.FinishToolCalls
	case "content_filter":
		return aidomain.FinishFiltered
	default:
		return aidomain.FinishOther
	}
}

// setOpenAIFinish 记结束原因；归一不了的把原样留在 FinishRaw。
func setOpenAIFinish(result *aidomain.ChatResponse, reason string) {
	result.FinishReason = normalizeOpenAIFinish(reason)
	result.FinishRaw = ""
	if result.FinishReason == aidomain.FinishOther {
		result.FinishRaw = reason
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
	params := buildOpenAIParams(req, meta.Provider)

	if onEvent == nil {
		completion, err := client.Chat.Completions.New(ctx, params)
		if err != nil {
			return nil, mapOpenAISDKError(meta.Provider, err)
		}
		return openAIResponseFromCompletion(completion), nil
	}

	// 没有这一句，多数实现的流里不带用量 —— 而计费与压缩判定都要它。
	params.StreamOptions = openai.ChatCompletionStreamOptionsParam{IncludeUsage: openai.Bool(true)}
	// SDK 读到 EOF 就当流正常结束，分不出「上游发完了 [DONE]」与「连接半路断了」；
	// 在响应体上挂个探针看有没有 [DONE]，consumeOpenAIStream 据此判断流是不是完整的。
	probe := &aiSSEDoneProbe{}
	stream := client.Chat.Completions.NewStreaming(ctx, params,
		openaioption.WithMiddleware(func(request *http.Request, next openaioption.MiddlewareNext) (*http.Response, error) {
			response, err := next(request)
			if response != nil && response.Body != nil {
				response.Body = probe.wrap(response.Body)
			}
			return response, err
		}))
	defer stream.Close()
	return consumeOpenAIStream(stream, meta.Provider, onEvent, probe.Done)
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
		setOpenAIFinish(result, string(choice.FinishReason))
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
//
// done 报告上游有没有发出 [DONE]（可为 nil）。流的完整性按两条判断：见过 finish_reason，
// 或者见过 [DONE]。两样都没有就是流被半路切断了 —— 按上游失败处理，而不是把半截内容
// 当成一次正常结束（下游会看到 finish_reason=stop，Agent 就此收工）。
//
// 工具调用按上游的 index 分组，对下游另编连续的序号（StreamEvent.ToolIndex）。调用编号在
// 发出 tool-start 时就定下来：上游没给就合成一个，流里与最终结论用的是同一个编号，
// 下一轮客户端带回的 tool_call_id 才对得上。名字到之前就来的入参先攒着，宣布调用时一并补发。
func consumeOpenAIStream(stream *openaisse.Stream[openai.ChatCompletionChunk], provider string,
	onEvent func(aidomain.StreamEvent) error, done func() bool) (*aidomain.ChatResponse, error) {
	result := &aidomain.ChatResponse{FinishReason: aidomain.FinishOther}
	var text, reasoning strings.Builder
	type toolBuffer struct {
		slot       int
		upstreamID string
		id, name   string
		arguments  strings.Builder
		emitted    int
		announced  bool
	}
	tools := map[int]*toolBuffer{}
	ordered := make([]*toolBuffer, 0, 2)
	sawFinish := false
	emit := func(event aidomain.StreamEvent) error {
		if err := onEvent(event); err != nil {
			return &aiUpstreamError{Provider: provider, Message: err.Error(), Retryable: true}
		}
		return nil
	}
	// flushArguments 把攒下还没发出的入参作为一条增量发出。
	flushArguments := func(buffer *toolBuffer) error {
		pending := buffer.arguments.String()[buffer.emitted:]
		if pending == "" {
			return nil
		}
		buffer.emitted = buffer.arguments.Len()
		return emit(aidomain.StreamEvent{
			Type: aidomain.StreamToolDelta, ToolIndex: buffer.slot,
			ToolID: buffer.id, ToolName: buffer.name, Delta: pending,
		})
	}

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
				if err := emit(aidomain.StreamEvent{Type: aidomain.StreamText, Delta: choice.Delta.Content}); err != nil {
					return nil, err
				}
			}
			// reasoning_content / reasoning 是 DeepSeek、OpenRouter 一系的扩展字段，
			// 不在官方类型里 —— SDK 把未识别字段留在 ExtraFields，从那里捞。
			if delta := extraFieldText(choice.Delta.JSON.ExtraFields, "reasoning_content", "reasoning"); delta != "" {
				reasoning.WriteString(delta)
				if err := emit(aidomain.StreamEvent{Type: aidomain.StreamReasoning, Delta: delta}); err != nil {
					return nil, err
				}
			}
			for _, call := range choice.Delta.ToolCalls {
				index := int(call.Index)
				buffer, ok := tools[index]
				// 有的实现把几次并行调用都标成同一个 index、只靠 id 区分：同一 index 上换了 id 就是新的一次。
				if ok && call.ID != "" && buffer.upstreamID != "" && call.ID != buffer.upstreamID {
					ok = false
				}
				if !ok {
					buffer = &toolBuffer{slot: len(ordered)}
					tools[index] = buffer
					ordered = append(ordered, buffer)
				}
				if call.ID != "" && buffer.upstreamID == "" {
					buffer.upstreamID = call.ID
				}
				if call.Function.Name != "" && !buffer.announced {
					buffer.name = call.Function.Name
				}
				buffer.arguments.WriteString(call.Function.Arguments)
				if !buffer.announced && buffer.name != "" {
					buffer.announced = true
					buffer.id = buffer.upstreamID
					if buffer.id == "" {
						buffer.id = "call_" + randomCallSuffix()
					}
					if err := emit(aidomain.StreamEvent{
						Type: aidomain.StreamToolStart, ToolIndex: buffer.slot,
						ToolID: buffer.id, ToolName: buffer.name,
					}); err != nil {
						return nil, err
					}
				}
				if buffer.announced {
					if err := flushArguments(buffer); err != nil {
						return nil, err
					}
				}
			}
			if choice.FinishReason != "" {
				sawFinish = true
				setOpenAIFinish(result, string(choice.FinishReason))
			}
		}
	}
	if err := stream.Err(); err != nil {
		return nil, mapOpenAISDKError(provider, err)
	}
	if !sawFinish {
		if done == nil || !done() {
			return nil, &aiUpstreamError{Provider: provider, Retryable: true,
				Message: "上游的流没有给出结束原因就断开了（被上游或中间代理切断），本次结果不完整"}
		}
		// 发了 [DONE] 却从不带 finish_reason 的兼容实现：流是完整的，按正常结束处理。
		result.FinishReason = aidomain.FinishStop
	}

	result.Text = text.String()
	result.Reasoning = reasoning.String()
	for _, buffer := range ordered {
		arguments := strings.TrimSpace(buffer.arguments.String())
		if arguments == "" {
			arguments = "{}"
		}
		id := buffer.id
		if id == "" {
			id = buffer.upstreamID
		}
		if id == "" {
			id = "call_" + randomCallSuffix()
		}
		result.ToolCalls = append(result.ToolCalls, aidomain.ToolCall{
			ID: id, Name: buffer.name, Arguments: json.RawMessage(arguments),
		})
	}
	if len(result.ToolCalls) > 0 && (result.FinishReason == aidomain.FinishOther || result.FinishReason == aidomain.FinishStop) {
		result.FinishReason, result.FinishRaw = aidomain.FinishToolCalls, ""
	}
	if result.Usage.TotalTokens == 0 {
		result.Usage.TotalTokens = result.Usage.InputTokens + result.Usage.OutputTokens
	}
	return result, nil
}

// aiSSEDoneProbe 旁听 SSE 响应体，记下有没有出现 `data: [DONE]` 这一行。
type aiSSEDoneProbe struct {
	mu   sync.Mutex
	done bool
	line []byte
}

func (p *aiSSEDoneProbe) wrap(body io.ReadCloser) io.ReadCloser {
	return &aiSSEDoneProbeBody{ReadCloser: body, probe: p}
}

// Done 上游是否发过 [DONE]。
func (p *aiSSEDoneProbe) Done() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.done || p.matchLocked()
}

func (p *aiSSEDoneProbe) observe(data []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, b := range data {
		if b == '\n' {
			if p.matchLocked() {
				p.done = true
			}
			p.line = p.line[:0]
			continue
		}
		// 只需要行首十几个字节就能认出 [DONE]，长行不必攒。
		if len(p.line) < 16 {
			p.line = append(p.line, b)
		}
	}
}

func (p *aiSSEDoneProbe) matchLocked() bool {
	line := strings.TrimSpace(string(p.line))
	return line == "data: [DONE]" || line == "data:[DONE]"
}

type aiSSEDoneProbeBody struct {
	io.ReadCloser
	probe *aiSSEDoneProbe
}

func (b *aiSSEDoneProbeBody) Read(data []byte) (int, error) {
	n, err := b.ReadCloser.Read(data)
	if n > 0 {
		b.probe.observe(data[:n])
	}
	return n, err
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
