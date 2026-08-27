package httptransport

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	aidomain "aegis/internal/domain/ai"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/response"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// OpenAI / Anthropic 兼容网关。
//
// 接入方服务端把任意 OpenAI / Anthropic SDK 的 baseURL 指到
// `/api/apps/{appkey}/ai/v1`，api key 填应用的函数调用密钥，即可复用
// 应用在 Aegis 上配置的 AI 通道（含链路回退与平台共享兜底）——
// 密钥、供应商、型号的管理全部留在控制台，业务代码零改动换供应商。
//
// 凭据与远程函数调用是同一把钥匙（X-Aegis-Function-Key），也接受
// OpenAI SDK 的 `Authorization: Bearer` 与 Anthropic SDK 的 `x-api-key`
// 惯用位置 —— 三个头装的都是函数密钥。

const aiGatewayMaxBodyBytes = 4 << 20

// bindAIGatewayJSON 与 bindLimitedJSON 的区别是**容忍未知字段**：
// OpenAI / Anthropic SDK 会带上 stream_options、metadata、user 等一大批
// 本网关用不上的字段，逐一建模没有意义，严格模式则会把合法请求全部拒掉。
func bindAIGatewayJSON(c *gin.Context, target any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, aiGatewayMaxBodyBytes)
	return json.NewDecoder(c.Request.Body).Decode(target)
}

// authenticateAIGateway 网关准入：函数密钥（三个惯用头位置任一）。
func (h *Handler) authenticateAIGateway(c *gin.Context, appID int64) bool {
	secret := strings.TrimSpace(c.GetHeader("X-Aegis-Function-Key"))
	if secret == "" {
		secret = strings.TrimSpace(c.GetHeader("x-api-key"))
	}
	if secret == "" {
		authorization := strings.TrimSpace(c.GetHeader("Authorization"))
		if len(authorization) > 7 && strings.EqualFold(authorization[:7], "Bearer ") {
			secret = strings.TrimSpace(authorization[7:])
		}
	}
	if secret == "" {
		response.Error(c, http.StatusUnauthorized, 40100,
			"缺少凭据：请在 X-Aegis-Function-Key / x-api-key / Authorization Bearer 里携带应用的函数调用密钥")
		return false
	}
	if _, err := h.appFunction.AuthenticateKey(c.Request.Context(), appID, secret); err != nil {
		h.writeError(c, err)
		return false
	}
	return true
}

// writeAIGatewayError 按协议的错误信封回错，SDK 才能解析出 message。
func writeAIGatewayError(c *gin.Context, protocol string, err error) {
	status := http.StatusBadGateway
	message := err.Error()
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) {
		status = appErr.HTTPStatus
		message = appErr.Message
	}
	if protocol == aidomain.ProtocolAnthropic {
		c.JSON(status, gin.H{
			"type":  "error",
			"error": gin.H{"type": "api_error", "message": message},
		})
		return
	}
	c.JSON(status, gin.H{
		"error": gin.H{"message": message, "type": "api_error", "code": nil},
	})
}

// ═══════════════════════ OpenAI Chat Completions 入站 ═══════════════════════

type openAIGatewayMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	ToolCalls  []struct {
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
	ToolCallID string `json:"tool_call_id"`
}

type openAIGatewayRequest struct {
	Model               string                 `json:"model"`
	Messages            []openAIGatewayMessage `json:"messages"`
	Tools               []struct {
		Type     string `json:"type"`
		Function struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
		} `json:"function"`
	} `json:"tools"`
	ToolChoice          json.RawMessage `json:"tool_choice"`
	MaxTokens           int             `json:"max_tokens"`
	MaxCompletionTokens int             `json:"max_completion_tokens"`
	Temperature         *float64        `json:"temperature"`
	TopP                *float64        `json:"top_p"`
	Stop                json.RawMessage `json:"stop"`
	Stream              bool            `json:"stream"`
	ResponseFormat      *struct {
		Type string `json:"type"`
	} `json:"response_format"`
}

// AppAIChatCompletions POST /api/apps/:appkey/ai/v1/chat/completions
func (h *Handler) AppAIChatCompletions(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	if !h.authenticateAIGateway(c, appID) {
		return
	}
	var req openAIGatewayRequest
	if err := bindAIGatewayJSON(c, &req); err != nil {
		writeAIGatewayError(c, aidomain.ProtocolOpenAI,
			apperrors.New(40000, http.StatusBadRequest, "请求体不是合法的 Chat Completions JSON："+err.Error()))
		return
	}
	request, err := openAIGatewayToChatRequest(req)
	if err != nil {
		writeAIGatewayError(c, aidomain.ProtocolOpenAI, err)
		return
	}

	if !req.Stream {
		result, err := h.aiProvider.GatewayChat(c.Request.Context(), appID, request)
		if err != nil {
			writeAIGatewayError(c, aidomain.ProtocolOpenAI, err)
			return
		}
		c.JSON(http.StatusOK, openAIGatewayResponse(result))
		return
	}
	h.streamOpenAIGateway(c, appID, request)
}

func openAIGatewayToChatRequest(req openAIGatewayRequest) (aidomain.ChatRequest, error) {
	out := aidomain.ChatRequest{
		Model:       strings.TrimSpace(req.Model),
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
	}
	if out.MaxTokens == 0 {
		out.MaxTokens = req.MaxCompletionTokens
	}
	if req.ResponseFormat != nil && req.ResponseFormat.Type == "json_object" {
		out.JSONMode = true
	}
	if len(req.Stop) > 0 {
		var single string
		var many []string
		if json.Unmarshal(req.Stop, &single) == nil && single != "" {
			out.Stop = []string{single}
		} else if json.Unmarshal(req.Stop, &many) == nil {
			out.Stop = many
		}
	}
	if len(req.ToolChoice) > 0 {
		var mode string
		if json.Unmarshal(req.ToolChoice, &mode) == nil {
			switch mode {
			case "none":
				out.ToolChoice = aidomain.ToolChoiceNone
			case "required":
				out.ToolChoice = aidomain.ToolChoiceRequired
			}
		}
	}
	for _, tool := range req.Tools {
		out.Tools = append(out.Tools, aidomain.Tool{
			Name:        tool.Function.Name,
			Description: tool.Function.Description,
			InputSchema: tool.Function.Parameters,
		})
	}
	if len(req.Messages) == 0 {
		return out, apperrors.New(40000, http.StatusBadRequest, "messages 不能为空")
	}
	for _, message := range req.Messages {
		switch message.Role {
		case "system", "developer":
			text := openAIGatewayContentText(message.Content)
			if out.System == "" {
				out.System = text
			} else {
				out.System += "\n\n" + text
			}
		case "tool":
			out.Messages = append(out.Messages, aidomain.ChatMessage{
				Role: aidomain.RoleTool, ToolCallID: message.ToolCallID,
				Content: []aidomain.ContentPart{{Type: aidomain.PartText, Text: openAIGatewayContentText(message.Content)}},
			})
		case "assistant":
			item := aidomain.ChatMessage{Role: aidomain.RoleAssistant}
			if text := openAIGatewayContentText(message.Content); text != "" {
				item.Content = []aidomain.ContentPart{{Type: aidomain.PartText, Text: text}}
			}
			for _, call := range message.ToolCalls {
				arguments := strings.TrimSpace(call.Function.Arguments)
				if arguments == "" {
					arguments = "{}"
				}
				item.ToolCalls = append(item.ToolCalls, aidomain.ToolCall{
					ID: call.ID, Name: call.Function.Name, Arguments: json.RawMessage(arguments),
				})
			}
			out.Messages = append(out.Messages, item)
		default: // user 与未知角色一律按 user 处理
			out.Messages = append(out.Messages, aidomain.ChatMessage{
				Role: aidomain.RoleUser, Content: openAIGatewayContentParts(message.Content),
			})
		}
	}
	return out, nil
}

// openAIGatewayContentParts content 字段两种形状（字符串 / 分片数组）都收。
func openAIGatewayContentParts(raw json.RawMessage) []aidomain.ContentPart {
	if len(raw) == 0 {
		return nil
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return []aidomain.ContentPart{{Type: aidomain.PartText, Text: text}}
	}
	var parts []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL struct {
			URL string `json:"url"`
		} `json:"image_url"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		return nil
	}
	out := make([]aidomain.ContentPart, 0, len(parts))
	for _, part := range parts {
		switch part.Type {
		case "image_url":
			out = append(out, aidomain.ContentPart{Type: aidomain.PartImage, ImageURL: part.ImageURL.URL})
		default:
			if part.Text != "" {
				out = append(out, aidomain.ContentPart{Type: aidomain.PartText, Text: part.Text})
			}
		}
	}
	return out
}

func openAIGatewayContentText(raw json.RawMessage) string {
	var builder strings.Builder
	for _, part := range openAIGatewayContentParts(raw) {
		if part.Type == aidomain.PartText {
			builder.WriteString(part.Text)
		}
	}
	return builder.String()
}

func openAIGatewayFinish(reason string) string {
	switch reason {
	case aidomain.FinishLength:
		return "length"
	case aidomain.FinishToolCalls:
		return "tool_calls"
	case aidomain.FinishFiltered:
		return "content_filter"
	default:
		return "stop"
	}
}

func openAIGatewayResponse(result *aidomain.ChatResponse) gin.H {
	message := gin.H{"role": "assistant", "content": result.Text}
	if result.Reasoning != "" {
		message["reasoning_content"] = result.Reasoning
	}
	if len(result.ToolCalls) > 0 {
		calls := make([]gin.H, 0, len(result.ToolCalls))
		for _, call := range result.ToolCalls {
			calls = append(calls, gin.H{
				"id": call.ID, "type": "function",
				"function": gin.H{"name": call.Name, "arguments": string(call.Arguments)},
			})
		}
		message["tool_calls"] = calls
	}
	id := result.ID
	if id == "" {
		id = "chatcmpl-" + uuid.NewString()
	}
	return gin.H{
		"id": id, "object": "chat.completion", "created": time.Now().Unix(),
		"model": result.Model,
		"choices": []gin.H{{
			"index": 0, "message": message,
			"finish_reason": openAIGatewayFinish(result.FinishReason),
		}},
		"usage": gin.H{
			"prompt_tokens":     result.Usage.InputTokens,
			"completion_tokens": result.Usage.OutputTokens,
			"total_tokens":      result.Usage.TotalTokens,
		},
	}
}

// streamOpenAIGateway 把统一流事件翻回 Chat Completions 的 SSE chunk。
func (h *Handler) streamOpenAIGateway(c *gin.Context, appID int64, request aidomain.ChatRequest) {
	header := c.Writer.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	header.Set("Cache-Control", "no-cache")
	header.Set("X-Accel-Buffering", "no")

	id := "chatcmpl-" + uuid.NewString()
	created := time.Now().Unix()
	wrote := false
	writeChunk := func(payload any) error {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if _, err := c.Writer.WriteString("data: " + string(encoded) + "\n\n"); err != nil {
			return err
		}
		c.Writer.Flush()
		wrote = true
		return nil
	}
	chunk := func(delta gin.H, finish any) gin.H {
		return gin.H{
			"id": id, "object": "chat.completion.chunk", "created": created,
			"model": request.Model,
			"choices": []gin.H{{"index": 0, "delta": delta, "finish_reason": finish}},
		}
	}

	// 工具调用在 OpenAI chunk 里按 index 编号；统一事件里的 ToolIndex
	// 来自上游，不一定从 0 连续，这里重新编号。
	toolIndexes := map[string]int{}
	sentRole := false
	result, err := h.aiProvider.GatewayChatStream(c.Request.Context(), appID, request,
		func(event aidomain.StreamEvent) error {
			if !sentRole {
				sentRole = true
				if err := writeChunk(chunk(gin.H{"role": "assistant"}, nil)); err != nil {
					return err
				}
			}
			switch event.Type {
			case aidomain.StreamText:
				return writeChunk(chunk(gin.H{"content": event.Delta}, nil))
			case aidomain.StreamReasoning:
				return writeChunk(chunk(gin.H{"reasoning_content": event.Delta}, nil))
			case aidomain.StreamToolStart:
				index := len(toolIndexes)
				toolIndexes[event.ToolID] = index
				return writeChunk(chunk(gin.H{"tool_calls": []gin.H{{
					"index": index, "id": event.ToolID, "type": "function",
					"function": gin.H{"name": event.ToolName, "arguments": ""},
				}}}, nil))
			case aidomain.StreamToolDelta:
				index, ok := toolIndexes[event.ToolID]
				if !ok {
					return nil
				}
				return writeChunk(chunk(gin.H{"tool_calls": []gin.H{{
					"index":    index,
					"function": gin.H{"arguments": event.Delta},
				}}}, nil))
			}
			return nil
		})
	if err != nil {
		if !wrote {
			writeAIGatewayError(c, aidomain.ProtocolOpenAI, err)
			return
		}
		// 流已经开了只能以数据块收尾，HTTP 状态改不了。
		_ = writeChunk(gin.H{"error": gin.H{"message": err.Error(), "type": "api_error"}})
		_, _ = c.Writer.WriteString("data: [DONE]\n\n")
		c.Writer.Flush()
		return
	}
	_ = writeChunk(chunk(gin.H{}, openAIGatewayFinish(result.FinishReason)))
	_ = writeChunk(gin.H{
		"id": id, "object": "chat.completion.chunk", "created": created,
		"model": result.Model, "choices": []gin.H{},
		"usage": gin.H{
			"prompt_tokens":     result.Usage.InputTokens,
			"completion_tokens": result.Usage.OutputTokens,
			"total_tokens":      result.Usage.TotalTokens,
		},
	})
	_, _ = c.Writer.WriteString("data: [DONE]\n\n")
	c.Writer.Flush()
}

// ═══════════════════════ Anthropic Messages 入站 ═══════════════════════

type anthropicGatewayRequest struct {
	Model     string          `json:"model"`
	System    json.RawMessage `json:"system"`
	Messages  []struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"messages"`
	MaxTokens int `json:"max_tokens"`
	Tools     []struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
	} `json:"tools"`
	ToolChoice *struct {
		Type string `json:"type"`
	} `json:"tool_choice"`
	Temperature   *float64 `json:"temperature"`
	TopP          *float64 `json:"top_p"`
	StopSequences []string `json:"stop_sequences"`
	Stream        bool     `json:"stream"`
}

// AppAIMessages POST /api/apps/:appkey/ai/v1/messages
func (h *Handler) AppAIMessages(c *gin.Context) {
	appID, ok := resolveAppID(c, h.app)
	if !ok {
		return
	}
	if !h.authenticateAIGateway(c, appID) {
		return
	}
	var req anthropicGatewayRequest
	if err := bindAIGatewayJSON(c, &req); err != nil {
		writeAIGatewayError(c, aidomain.ProtocolAnthropic,
			apperrors.New(40000, http.StatusBadRequest, "请求体不是合法的 Messages JSON："+err.Error()))
		return
	}
	request, err := anthropicGatewayToChatRequest(req)
	if err != nil {
		writeAIGatewayError(c, aidomain.ProtocolAnthropic, err)
		return
	}

	if !req.Stream {
		result, err := h.aiProvider.GatewayChat(c.Request.Context(), appID, request)
		if err != nil {
			writeAIGatewayError(c, aidomain.ProtocolAnthropic, err)
			return
		}
		c.JSON(http.StatusOK, anthropicGatewayResponse(result))
		return
	}
	h.streamAnthropicGateway(c, appID, request)
}

func anthropicGatewayToChatRequest(req anthropicGatewayRequest) (aidomain.ChatRequest, error) {
	out := aidomain.ChatRequest{
		Model:       strings.TrimSpace(req.Model),
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		Stop:        req.StopSequences,
	}
	// system 两种形状：字符串或 text 块数组。
	if len(req.System) > 0 {
		var text string
		if json.Unmarshal(req.System, &text) == nil {
			out.System = text
		} else {
			var blocks []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if json.Unmarshal(req.System, &blocks) == nil {
				var builder strings.Builder
				for _, block := range blocks {
					if block.Type == "text" {
						builder.WriteString(block.Text)
					}
				}
				out.System = builder.String()
			}
		}
	}
	for _, tool := range req.Tools {
		out.Tools = append(out.Tools, aidomain.Tool{
			Name: tool.Name, Description: tool.Description, InputSchema: tool.InputSchema,
		})
	}
	if req.ToolChoice != nil {
		switch req.ToolChoice.Type {
		case "none":
			out.ToolChoice = aidomain.ToolChoiceNone
		case "any", "tool":
			out.ToolChoice = aidomain.ToolChoiceRequired
		}
	}
	if len(req.Messages) == 0 {
		return out, apperrors.New(40000, http.StatusBadRequest, "messages 不能为空")
	}

	type anthropicBlock struct {
		Type      string          `json:"type"`
		Text      string          `json:"text"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Input     json.RawMessage `json:"input"`
		ToolUseID string          `json:"tool_use_id"`
		Content   json.RawMessage `json:"content"`
		Source    struct {
			Type      string `json:"type"`
			URL       string `json:"url"`
			MediaType string `json:"media_type"`
			Data      string `json:"data"`
		} `json:"source"`
	}
	blockText := func(raw json.RawMessage) string {
		var text string
		if json.Unmarshal(raw, &text) == nil {
			return text
		}
		var blocks []anthropicBlock
		if json.Unmarshal(raw, &blocks) != nil {
			return ""
		}
		var builder strings.Builder
		for _, block := range blocks {
			if block.Type == "text" {
				builder.WriteString(block.Text)
			}
		}
		return builder.String()
	}

	for _, message := range req.Messages {
		var blocks []anthropicBlock
		var plain string
		if json.Unmarshal(message.Content, &plain) == nil {
			blocks = []anthropicBlock{{Type: "text", Text: plain}}
		} else if err := json.Unmarshal(message.Content, &blocks); err != nil {
			return out, apperrors.New(40000, http.StatusBadRequest, "消息 content 既不是字符串也不是块数组")
		}

		if message.Role == "assistant" {
			item := aidomain.ChatMessage{Role: aidomain.RoleAssistant}
			for _, block := range blocks {
				switch block.Type {
				case "text":
					if block.Text != "" {
						item.Content = append(item.Content, aidomain.ContentPart{Type: aidomain.PartText, Text: block.Text})
					}
				case "tool_use":
					arguments := block.Input
					if len(arguments) == 0 {
						arguments = json.RawMessage("{}")
					}
					item.ToolCalls = append(item.ToolCalls, aidomain.ToolCall{
						ID: block.ID, Name: block.Name, Arguments: arguments,
					})
				}
			}
			out.Messages = append(out.Messages, item)
			continue
		}

		// user 消息：tool_result 拆成独立的 tool 消息，其余聚成一条 user。
		user := aidomain.ChatMessage{Role: aidomain.RoleUser}
		for _, block := range blocks {
			switch block.Type {
			case "tool_result":
				out.Messages = append(out.Messages, aidomain.ChatMessage{
					Role: aidomain.RoleTool, ToolCallID: block.ToolUseID,
					Content: []aidomain.ContentPart{{Type: aidomain.PartText, Text: blockText(block.Content)}},
				})
			case "image":
				imageURL := block.Source.URL
				if block.Source.Type == "base64" && block.Source.Data != "" {
					imageURL = "data:" + block.Source.MediaType + ";base64," + block.Source.Data
				}
				user.Content = append(user.Content, aidomain.ContentPart{Type: aidomain.PartImage, ImageURL: imageURL})
			case "text":
				if block.Text != "" {
					user.Content = append(user.Content, aidomain.ContentPart{Type: aidomain.PartText, Text: block.Text})
				}
			}
		}
		if len(user.Content) > 0 {
			out.Messages = append(out.Messages, user)
		}
	}
	return out, nil
}

func anthropicGatewayFinish(reason string) string {
	switch reason {
	case aidomain.FinishLength:
		return "max_tokens"
	case aidomain.FinishToolCalls:
		return "tool_use"
	case aidomain.FinishFiltered:
		return "refusal"
	default:
		return "end_turn"
	}
}

func anthropicGatewayResponse(result *aidomain.ChatResponse) gin.H {
	content := make([]gin.H, 0, 2)
	if result.Text != "" {
		content = append(content, gin.H{"type": "text", "text": result.Text})
	}
	for _, call := range result.ToolCalls {
		var input any = map[string]any{}
		_ = json.Unmarshal(call.Arguments, &input)
		content = append(content, gin.H{
			"type": "tool_use", "id": call.ID, "name": call.Name, "input": input,
		})
	}
	id := result.ID
	if id == "" {
		id = "msg_" + uuid.NewString()
	}
	return gin.H{
		"id": id, "type": "message", "role": "assistant",
		"model": result.Model, "content": content,
		"stop_reason": anthropicGatewayFinish(result.FinishReason), "stop_sequence": nil,
		"usage": gin.H{
			"input_tokens":  result.Usage.InputTokens,
			"output_tokens": result.Usage.OutputTokens,
		},
	}
}

// streamAnthropicGateway 把统一流事件翻回 Messages 的 SSE 事件序列。
func (h *Handler) streamAnthropicGateway(c *gin.Context, appID int64, request aidomain.ChatRequest) {
	header := c.Writer.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	header.Set("Cache-Control", "no-cache")
	header.Set("X-Accel-Buffering", "no")

	id := "msg_" + uuid.NewString()
	wrote := false
	writeEvent := func(event string, payload any) error {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, encoded); err != nil {
			return err
		}
		c.Writer.Flush()
		wrote = true
		return nil
	}

	// Anthropic 流是按内容块组织的：进入新块前要关上一块。
	blockIndex := -1
	blockOpen := ""
	closeBlock := func() error {
		if blockOpen == "" {
			return nil
		}
		blockOpen = ""
		return writeEvent("content_block_stop", gin.H{"type": "content_block_stop", "index": blockIndex})
	}
	openBlock := func(kind string, block gin.H) error {
		if blockOpen == kind && kind != "tool_use" {
			return nil
		}
		if err := closeBlock(); err != nil {
			return err
		}
		blockIndex++
		blockOpen = kind
		return writeEvent("content_block_start", gin.H{
			"type": "content_block_start", "index": blockIndex, "content_block": block,
		})
	}

	started := false
	ensureStart := func() error {
		if started {
			return nil
		}
		started = true
		return writeEvent("message_start", gin.H{
			"type": "message_start",
			"message": gin.H{
				"id": id, "type": "message", "role": "assistant", "model": request.Model,
				"content": []gin.H{}, "stop_reason": nil, "stop_sequence": nil,
				"usage": gin.H{"input_tokens": 0, "output_tokens": 0},
			},
		})
	}

	result, err := h.aiProvider.GatewayChatStream(c.Request.Context(), appID, request,
		func(event aidomain.StreamEvent) error {
			if err := ensureStart(); err != nil {
				return err
			}
			switch event.Type {
			case aidomain.StreamText:
				if err := openBlock("text", gin.H{"type": "text", "text": ""}); err != nil {
					return err
				}
				return writeEvent("content_block_delta", gin.H{
					"type": "content_block_delta", "index": blockIndex,
					"delta": gin.H{"type": "text_delta", "text": event.Delta},
				})
			case aidomain.StreamReasoning:
				if err := openBlock("thinking", gin.H{"type": "thinking", "thinking": ""}); err != nil {
					return err
				}
				return writeEvent("content_block_delta", gin.H{
					"type": "content_block_delta", "index": blockIndex,
					"delta": gin.H{"type": "thinking_delta", "thinking": event.Delta},
				})
			case aidomain.StreamToolStart:
				return openBlock("tool_use", gin.H{
					"type": "tool_use", "id": event.ToolID, "name": event.ToolName, "input": gin.H{},
				})
			case aidomain.StreamToolDelta:
				if blockOpen != "tool_use" {
					return nil
				}
				return writeEvent("content_block_delta", gin.H{
					"type": "content_block_delta", "index": blockIndex,
					"delta": gin.H{"type": "input_json_delta", "partial_json": event.Delta},
				})
			}
			return nil
		})
	if err != nil {
		if !wrote {
			writeAIGatewayError(c, aidomain.ProtocolAnthropic, err)
			return
		}
		_ = writeEvent("error", gin.H{
			"type":  "error",
			"error": gin.H{"type": "api_error", "message": err.Error()},
		})
		return
	}
	_ = ensureStart()
	_ = closeBlock()
	_ = writeEvent("message_delta", gin.H{
		"type":  "message_delta",
		"delta": gin.H{"stop_reason": anthropicGatewayFinish(result.FinishReason), "stop_sequence": nil},
		"usage": gin.H{"output_tokens": result.Usage.OutputTokens},
	})
	_ = writeEvent("message_stop", gin.H{"type": "message_stop"})
}
