package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	aidomain "aegis/internal/domain/ai"

	"google.golang.org/genai"
)

// ═══════════════════════════ Google Gemini（google.golang.org/genai） ═══════════════════════════
//
// 第三条协议路径：Gemini 原生 GenerateContent。此前 Gemini 走它的 OpenAI 兼容层，
// 工具调用与思考内容都打了折扣 —— 官方 SDK 直连后，函数调用、思考分片、
// 用量统计（含思考 token）都是一等公民，Agent 循环在 Gemini 通道上完整可用。
//
// 与另两个适配器同一套约束：端点、密钥、HTTP 客户端全部显式来自通道配置，
// 出网走 egress 网关，错误收敛成 aiUpstreamError 交给链路层决定要不要换通道。

// geminiSyntheticIDPrefix Gemini 的函数调用常不带 ID，而统一格式靠 ID 关联
// 调用与结果 —— 缺失时按序合成。回喂历史时合成 ID 会被剥掉（Gemini 侧
// 从未见过它们），真实 ID 原样带回。
const geminiSyntheticIDPrefix = "gemini_call_"

// geminiSkipSignature 拿不到真签名时 Gemini 文档允许的占位：跳过思考签名校验。
const geminiSkipSignature = "skip_thought_signature_validator"

// geminiClient 按通道配置组装官方 SDK 客户端。
func (c *aiLLMClient) geminiClient(ctx context.Context, cfg aidomain.Config, meta aidomain.ProviderMeta) (*genai.Client, error) {
	options := genai.HTTPOptions{}
	if base := geminiBaseURL(cfg); base != "" {
		options.BaseURL = base
	}
	headers := http.Header{}
	for headerKey, headerValue := range cfg.SettingMap(aidomain.KeyExtraHeaders) {
		headerKey = strings.TrimSpace(headerKey)
		if headerKey == "" {
			continue
		}
		headers.Set(headerKey, headerValue)
	}
	if len(headers) > 0 {
		options.Headers = headers
	}
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:      cfg.Secret(aidomain.KeyAPIKey),
		Backend:     genai.BackendGeminiAPI,
		HTTPClient:  c.httpClient(meta.Provider),
		HTTPOptions: options,
	})
	if err != nil {
		return nil, &aiUpstreamError{Provider: meta.Provider, Message: err.Error()}
	}
	return client, nil
}

// geminiBaseURL 通道配置的地址 → SDK 的 BaseURL（scheme://host[/前缀]）。
//
// SDK 自己拼 /{apiVersion}/models/… 路径，所以这里与 OpenAI 侧的 /v1 补全
// 恰好相反：要的是**不带版本段**的根地址。两类历史形状都透明消化：
//   - 老配置粘的是 OpenAI 兼容端点（…/v1beta/openai）→ 剥掉 /openai 与版本段；
//   - 用户照官方文档粘了 …/v1beta → 剥掉版本段。
//
// 留空返回空串，交给 SDK 用官方缺省端点。
func geminiBaseURL(cfg aidomain.Config) string {
	base := strings.TrimRight(strings.TrimSpace(cfg.Setting(aidomain.KeyBaseURL)), "/")
	if base == "" {
		return ""
	}
	if !strings.Contains(base, "://") {
		base = "https://" + base
	}
	if trimmed, ok := strings.CutSuffix(base, "/openai"); ok {
		base = trimmed
	}
	if index := strings.LastIndex(base, "/"); index > len("https://") {
		if aiVersionSegment(base[index+1:]) {
			base = base[:index]
		}
	}
	return strings.TrimRight(base, "/")
}

// buildGeminiContents 统一转录 → Gemini contents。
//
// Gemini 的工具结果按 FunctionResponse.Name（可选 ID）关联，而统一格式只带
// ToolCallID —— 先扫一遍全部工具调用建 ID→名字 索引再翻。相邻同角色消息
// 合并（Gemini 要求 user / model 轮替）。
func buildGeminiContents(req aidomain.ChatRequest) []*genai.Content {
	callNames := make(map[string]string)
	for _, message := range req.Messages {
		for _, call := range message.ToolCalls {
			callNames[call.ID] = call.Name
		}
	}

	contents := make([]*genai.Content, 0, len(req.Messages))
	push := func(role string, parts ...*genai.Part) {
		if len(parts) == 0 {
			return
		}
		if len(contents) > 0 && contents[len(contents)-1].Role == role {
			last := contents[len(contents)-1]
			last.Parts = append(last.Parts, parts...)
			return
		}
		contents = append(contents, &genai.Content{Role: role, Parts: parts})
	}

	for _, item := range req.Messages {
		switch item.Role {
		case aidomain.RoleTool:
			payload := item.PlainText()
			response := map[string]any{}
			var parsed any
			if err := json.Unmarshal([]byte(payload), &parsed); err == nil {
				if object, ok := parsed.(map[string]any); ok {
					response = object
				} else {
					response = map[string]any{"result": parsed}
				}
			} else {
				response = map[string]any{"result": payload}
			}
			functionResponse := &genai.FunctionResponse{
				Name: callNames[item.ToolCallID], Response: response,
			}
			// 合成 ID 不回传：Gemini 侧从未签发过它们。
			if !strings.HasPrefix(item.ToolCallID, geminiSyntheticIDPrefix) {
				functionResponse.ID = item.ToolCallID
			}
			push(genai.RoleUser, &genai.Part{FunctionResponse: functionResponse})
		case aidomain.RoleAssistant:
			parts := make([]*genai.Part, 0, 1+len(item.ToolCalls))
			if text := item.PlainText(); text != "" {
				parts = append(parts, genai.NewPartFromText(text))
			}
			for _, call := range item.ToolCalls {
				arguments := map[string]any{}
				if len(call.Arguments) > 0 {
					_ = json.Unmarshal(call.Arguments, &arguments)
				}
				functionCall := &genai.FunctionCall{Name: call.Name, Args: arguments}
				if !strings.HasPrefix(call.ID, geminiSyntheticIDPrefix) {
					functionCall.ID = call.ID
				}
				part := &genai.Part{FunctionCall: functionCall}
				// 思考签名原样带回；Gemini 3 缺签名直接 400，补不回来时用官方给的跳过校验占位。
				if signature, err := base64.StdEncoding.DecodeString(call.Signature); err == nil && len(signature) > 0 {
					part.ThoughtSignature = signature
				} else if isGemini3(req.Model) {
					part.ThoughtSignature = []byte(geminiSkipSignature)
				}
				parts = append(parts, part)
			}
			push(genai.RoleModel, parts...)
		default:
			parts := make([]*genai.Part, 0, len(item.Content))
			for _, part := range item.Content {
				switch part.Type {
				case aidomain.PartImage:
					parts = append(parts, geminiImagePart(part.ImageURL))
				default:
					if part.Text != "" {
						parts = append(parts, genai.NewPartFromText(part.Text))
					}
				}
			}
			push(genai.RoleUser, parts...)
		}
	}
	return contents
}

// geminiImagePart 图像分片：data: URL 拆成内联字节，其余按远端 URI 引用。
func geminiImagePart(imageURL string) *genai.Part {
	if data, ok := strings.CutPrefix(imageURL, "data:"); ok {
		mediaType, encoded, found := strings.Cut(data, ";base64,")
		if found {
			if raw, err := base64.StdEncoding.DecodeString(encoded); err == nil {
				return &genai.Part{InlineData: &genai.Blob{MIMEType: mediaType, Data: raw}}
			}
		}
	}
	return &genai.Part{FileData: &genai.FileData{FileURI: imageURL}}
}

// buildGeminiConfig 统一请求 → GenerateContent 配置。
func buildGeminiConfig(req aidomain.ChatRequest) *genai.GenerateContentConfig {
	config := &genai.GenerateContentConfig{}
	if system := strings.TrimSpace(req.System); system != "" {
		config.SystemInstruction = &genai.Content{Parts: []*genai.Part{genai.NewPartFromText(system)}}
	}
	if req.MaxTokens > 0 {
		config.MaxOutputTokens = int32(req.MaxTokens)
	}
	if req.Temperature != nil {
		config.Temperature = genai.Ptr(float32(*req.Temperature))
	}
	if req.TopP != nil {
		config.TopP = genai.Ptr(float32(*req.TopP))
	}
	if len(req.Stop) > 0 {
		config.StopSequences = req.Stop
	}
	if req.JSONMode {
		config.ResponseMIMEType = "application/json"
	}
	config.ThinkingConfig = geminiThinking(req.Model, req.Reasoning)
	if len(req.Tools) > 0 {
		declarations := make([]*genai.FunctionDeclaration, 0, len(req.Tools))
		for _, tool := range req.Tools {
			declaration := &genai.FunctionDeclaration{Name: tool.Name, Description: tool.Description}
			// 原始 JSON Schema 直接透传（ParametersJsonSchema），
			// 不经 SDK 的类型化 Schema 转手 —— 转一趟只会丢字段。
			if len(tool.InputSchema) > 0 {
				var schema map[string]any
				if err := json.Unmarshal(tool.InputSchema, &schema); err == nil && len(schema) > 0 {
					declaration.ParametersJsonSchema = schema
				}
			}
			declarations = append(declarations, declaration)
		}
		config.Tools = []*genai.Tool{{FunctionDeclarations: declarations}}
		switch req.ToolChoice {
		case aidomain.ToolChoiceNone:
			config.ToolConfig = &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{
				Mode: genai.FunctionCallingConfigModeNone,
			}}
		case aidomain.ToolChoiceRequired:
			config.ToolConfig = &genai.ToolConfig{FunctionCallingConfig: &genai.FunctionCallingConfig{
				Mode: genai.FunctionCallingConfigModeAny,
			}}
		}
	}
	return config
}

// geminiThinking 思考设置 → thinkingConfig。
//
// Gemini 3 用档位（thinkingLevel），2.x 用预算（thinkingBudget），两者不能同时出现。
// 关闭：2.x 预算给 0（Pro 不能关，最低 128）；3.x 不能关，取最低一档。开着时一律要思考摘要，客户端才看得到思考过程。
func geminiThinking(model string, reasoning *aidomain.ReasoningConfig) *genai.ThinkingConfig {
	if reasoning == nil {
		return nil
	}
	pro := strings.Contains(strings.ToLower(model), "pro")
	if isGemini3(model) {
		config := &genai.ThinkingConfig{IncludeThoughts: reasoning.On()}
		switch reasoning.EffectiveEffort() {
		case aidomain.ReasoningNone, aidomain.ReasoningMinimal:
			if pro {
				config.ThinkingLevel = genai.ThinkingLevelLow
			} else {
				config.ThinkingLevel = genai.ThinkingLevelMinimal
			}
		case aidomain.ReasoningLow:
			config.ThinkingLevel = genai.ThinkingLevelLow
		case aidomain.ReasoningMedium:
			config.ThinkingLevel = genai.ThinkingLevelMedium
		case aidomain.ReasoningHigh, aidomain.ReasoningXHigh, aidomain.ReasoningMax:
			config.ThinkingLevel = genai.ThinkingLevelHigh
		}
		return config
	}
	if reasoning.Off() {
		budget := int32(0)
		if pro {
			budget = 128
		}
		return &genai.ThinkingConfig{IncludeThoughts: false, ThinkingBudget: &budget}
	}
	config := &genai.ThinkingConfig{IncludeThoughts: true}
	if reasoning.Effort != "" || reasoning.BudgetTokens > 0 {
		budget := int32(min(reasoning.Budget(), 32768))
		config.ThinkingBudget = &budget
	}
	return config
}

func normalizeGeminiFinish(reason genai.FinishReason) string {
	switch reason {
	case genai.FinishReasonStop:
		return aidomain.FinishStop
	case genai.FinishReasonMaxTokens:
		return aidomain.FinishLength
	case genai.FinishReasonSafety, genai.FinishReasonRecitation, genai.FinishReasonBlocklist,
		genai.FinishReasonProhibitedContent, genai.FinishReasonSPII, genai.FinishReasonImageSafety:
		return aidomain.FinishFiltered
	default:
		return aidomain.FinishOther
	}
}

// mapGeminiSDKError SDK 错误 → aiUpstreamError。
// 非 JSON 信封（Cloudflare 拦截页之类）同样过 summarizeOpaqueUpstreamBody 收敛。
func mapGeminiSDKError(provider string, err error) error {
	var upstream *aiUpstreamError
	if errors.As(err, &upstream) {
		return upstream
	}
	var apiErr genai.APIError
	if errors.As(err, &apiErr) {
		message := strings.TrimSpace(apiErr.Message)
		if summarized := summarizeOpaqueUpstreamBody([]byte(message), fmt.Sprintf("%d", apiErr.Code)); summarized != "" {
			message = summarized
		}
		if message == "" {
			message = apiErr.Status
		}
		return &aiUpstreamError{
			Provider: provider, Status: apiErr.Code, Message: message,
			Retryable: apiErr.Code != http.StatusBadRequest,
		}
	}
	return &aiUpstreamError{Provider: provider, Message: err.Error(), Retryable: true}
}

func (c *aiLLMClient) geminiChat(ctx context.Context, cfg aidomain.Config, meta aidomain.ProviderMeta,
	req aidomain.ChatRequest, onEvent func(aidomain.StreamEvent) error) (*aidomain.ChatResponse, error) {
	client, err := c.geminiClient(ctx, cfg, meta)
	if err != nil {
		return nil, err
	}
	contents := buildGeminiContents(req)
	config := buildGeminiConfig(req)

	if onEvent == nil {
		response, err := client.Models.GenerateContent(ctx, req.Model, contents, config)
		if err != nil {
			return nil, mapGeminiSDKError(meta.Provider, err)
		}
		result := &aidomain.ChatResponse{FinishReason: aidomain.FinishOther}
		aggregateGeminiChunk(result, response, nil, nil, nil)
		if err := geminiFinishError(meta.Provider, result); err != nil {
			return nil, err
		}
		return result, nil
	}

	result := &aidomain.ChatResponse{FinishReason: aidomain.FinishOther}
	var streamErr error
	emit := func(event aidomain.StreamEvent) {
		if streamErr != nil {
			return
		}
		if err := onEvent(event); err != nil {
			streamErr = &aiUpstreamError{Provider: meta.Provider, Message: err.Error(), Retryable: true}
		}
	}
	for chunk, err := range client.Models.GenerateContentStream(ctx, req.Model, contents, config) {
		if err != nil {
			return nil, mapGeminiSDKError(meta.Provider, err)
		}
		aggregateGeminiChunk(result, chunk,
			func(delta string) { emit(aidomain.StreamEvent{Type: aidomain.StreamText, Delta: delta}) },
			func(delta string) { emit(aidomain.StreamEvent{Type: aidomain.StreamReasoning, Delta: delta}) },
			func(index int, call aidomain.ToolCall) {
				emit(aidomain.StreamEvent{
					Type: aidomain.StreamToolStart, ToolIndex: index,
					ToolID: call.ID, ToolName: call.Name,
				})
				// Gemini 的函数入参一次性到齐：立即补一条完整 delta，
				// 下游聚合器（网关的协议回translate）就能拿到完整 JSON。
				emit(aidomain.StreamEvent{
					Type: aidomain.StreamToolDelta, ToolIndex: index,
					ToolID: call.ID, ToolName: call.Name, Delta: string(call.Arguments),
				})
			})
		if streamErr != nil {
			return nil, streamErr
		}
	}
	// 完整的流最后一个分片带 finishReason；一直没见到就是流被半路切断了。
	if result.FinishReason == aidomain.FinishOther && result.FinishRaw == "" {
		return nil, &aiUpstreamError{Provider: meta.Provider, Retryable: true,
			Message: "上游的流没有给出结束原因就断开了（被上游或中间代理切断），本次结果不完整"}
	}
	if err := geminiFinishError(meta.Provider, result); err != nil {
		return nil, err
	}
	return result, nil
}

// geminiFinishError 以失败告终的结束原因。MALFORMED_FUNCTION_CALL 是模型把函数调用写坏了：
// 既没有正文也没有可执行的调用，当成正常结束交给下游只会让 Agent 停在原地；按上游失败处理，
// 不扣费，客户端重试一次多半就好。
func geminiFinishError(provider string, result *aidomain.ChatResponse) error {
	if result.FinishRaw == string(genai.FinishReasonMalformedFunctionCall) {
		return &aiUpstreamError{Provider: provider, Retryable: true,
			Message: "模型生成的函数调用格式有误（MALFORMED_FUNCTION_CALL），请重试"}
	}
	return nil
}

// aggregateGeminiChunk 把一个响应分片并进聚合结果，可选地向外发增量。
// 非流式与流式共用：非流式就是「单分片的流」。
func aggregateGeminiChunk(result *aidomain.ChatResponse, chunk *genai.GenerateContentResponse,
	onText func(string), onReasoning func(string), onToolCall func(int, aidomain.ToolCall)) {
	if chunk == nil {
		return
	}
	if chunk.ResponseID != "" {
		result.ID = chunk.ResponseID
	}
	if chunk.ModelVersion != "" {
		result.Model = chunk.ModelVersion
	}
	if usage := chunk.UsageMetadata; usage != nil {
		result.Usage = aidomain.Usage{
			InputTokens:  int64(usage.PromptTokenCount),
			OutputTokens: int64(usage.CandidatesTokenCount) + int64(usage.ThoughtsTokenCount),
			TotalTokens:  int64(usage.TotalTokenCount),
		}
	}
	for _, candidate := range chunk.Candidates {
		if candidate == nil {
			continue
		}
		if candidate.FinishReason != "" {
			result.FinishReason = normalizeGeminiFinish(candidate.FinishReason)
			result.FinishRaw = ""
			if result.FinishReason == aidomain.FinishOther {
				result.FinishRaw = string(candidate.FinishReason)
			}
		}
		if candidate.Content == nil {
			continue
		}
		for _, part := range candidate.Content.Parts {
			if part == nil {
				continue
			}
			switch {
			case part.FunctionCall != nil:
				call := part.FunctionCall
				id := call.ID
				if id == "" {
					// 带随机段：签名按调用编号记（ai_llm_reasoning.go），序号会在不同对话之间撞上。
					id = fmt.Sprintf("%s%s_%d", geminiSyntheticIDPrefix, randomCallSuffix(), len(result.ToolCalls)+1)
				}
				arguments, err := json.Marshal(call.Args)
				if err != nil || len(arguments) == 0 || string(arguments) == "null" {
					arguments = json.RawMessage("{}")
				}
				toolCall := aidomain.ToolCall{ID: id, Name: call.Name, Arguments: arguments}
				if len(part.ThoughtSignature) > 0 {
					toolCall.Signature = base64.StdEncoding.EncodeToString(part.ThoughtSignature)
				}
				index := len(result.ToolCalls)
				result.ToolCalls = append(result.ToolCalls, toolCall)
				if onToolCall != nil {
					onToolCall(index, toolCall)
				}
			case part.Text != "":
				if part.Thought {
					result.Reasoning += part.Text
					if onReasoning != nil {
						onReasoning(part.Text)
					}
				} else {
					result.Text += part.Text
					if onText != nil {
						onText(part.Text)
					}
				}
			}
		}
	}
	// Gemini 带函数调用收尾时 finishReason 仍是 STOP；统一语义按「有调用即工具收尾」。
	if len(result.ToolCalls) > 0 && result.FinishReason == aidomain.FinishStop {
		result.FinishReason = aidomain.FinishToolCalls
	}
}
