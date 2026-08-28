package service

// Agent 循环的 Eino 落地：「模型 → 工具 → 模型」交给 CloudWeGo Eino 的
// react agent 驱动，本文件是它与既有体系之间的全部适配层。
//
// 分工约定：
//   - Eino 负责：循环编排、工具按名分发、消息在迭代间的累积、最大步数闸门；
//   - 本侧保留：通道链与官方 SDK 调用（AIProviderService.ChatStream）、
//     AI SDK 界面消息流（UI chunk）、落库分片（agentUIPart）、上下文预算裁剪、
//     工具的限时/恐慌兜底 —— 这些全部藏在两个适配器（模型/工具）里，
//     Eino 图看到的只是普通的 ToolCallingChatModel 与 InvokableTool。
//
// 关键取舍：agent 以 **Invoke 模式**（Generate）运行，而不是 Stream 模式。
// 界面要的逐字增量在模型适配器内部直接经 emit 下发（供应商流 → UI chunk），
// 不经 Eino 的流复制机器转手 —— Invoke 模式下分支判定拿到的是完整消息，
// 默认的工具调用检查器天然可靠（Claude 这类「先正文后工具」的模型在
// Stream 模式下会被首帧检查器误判成没有工具调用）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	aidomain "aegis/internal/domain/ai"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/flow/agent/react"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
	"github.com/google/uuid"
)

// ── 会话现场 ──

// einoAgentSession 一轮 Agent 请求在 Eino 图里的共享现场。
//
// 模型与工具适配器都经它发界面事件、攒落库分片、累计用量。emit 落在
// HTTP 的 SSE writer 上，不可并发写 —— 所有出口统一走 emitChunk（互斥）。
type einoAgentSession struct {
	service *AIAgentService
	input   AIAgentRunInput
	run     *aiAgentRun
	// toolIndex 内置工具查表；toolList 是发给模型的统一清单（内置 + MCP）。
	toolIndex map[string]aiAgentTool
	toolList  []aidomain.Tool
	// budget 上下文字符预算，超出即触发 trimEinoMessages。
	budget int
	// chatStream 可注入的流式调用面（单测替身用）；nil 走真实通道链。
	chatStream func(ctx context.Context, args aiChatArgs,
		onEvent func(aidomain.StreamEvent) error) (*aidomain.ChatResponse, *resolvedChannel, error)

	mu         sync.Mutex
	emit       aiAgentEmit
	parts      []agentUIPart
	totalUsage aidomain.Usage
	// stepOpen 有一个 start-step 尚未配对 finish-step。界面的步边界由
	// 模型调用对齐：下一次模型调用开始前收上一步，收尾时收最后一步。
	stepOpen bool
}

func (s *einoAgentSession) emitChunk(chunk any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.emit(chunk)
}

func (s *einoAgentSession) appendPart(part agentUIPart) {
	s.mu.Lock()
	s.parts = append(s.parts, part)
	s.mu.Unlock()
}

func (s *einoAgentSession) addUsage(usage aidomain.Usage) {
	s.mu.Lock()
	s.totalUsage.Add(usage)
	s.mu.Unlock()
}

// snapshot 收尾落库用的最终分片与用量。
func (s *einoAgentSession) snapshot() ([]agentUIPart, aidomain.Usage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.parts, s.totalUsage
}

// beginStep 在每次模型调用前对齐界面的步边界。
func (s *einoAgentSession) beginStep() error {
	if s.stepOpen {
		if err := s.emitChunk(map[string]any{"type": "finish-step"}); err != nil {
			return err
		}
	}
	if err := s.emitChunk(map[string]any{"type": "start-step"}); err != nil {
		return err
	}
	s.appendPart(agentUIPart{Type: "step-start"})
	s.stepOpen = true
	return nil
}

// finishStep 收掉最后一个未配对的步（只在整轮成功结束时调用 ——
// 中途失败的流以 error chunk 结尾，与旧循环一致不再补 finish-step）。
func (s *einoAgentSession) finishStep() {
	if s.stepOpen {
		_ = s.emitChunk(map[string]any{"type": "finish-step"})
		s.stepOpen = false
	}
}

// ── Agent 主入口 ──

// runEinoAgent 把装配好的现场交给 Eino react agent 跑完一轮。
//
// 返回 error 的语义与旧循环一致：上游/取消类失败已作为 error chunk 发给界面，
// 调用方只需在落库后把错误往上抛；触达最大步数不算失败（发提示语后正常收尾）。
func (s *AIAgentService) runEinoAgent(ctx context.Context, session *einoAgentSession,
	initial []*schema.Message) error {

	einoTools := make([]tool.BaseTool, 0, len(session.toolList))
	for _, spec := range session.toolList {
		einoTools = append(einoTools, &einoAgentTool{session: session, spec: spec})
	}

	agent, err := react.NewAgent(ctx, &react.AgentConfig{
		ToolCallingModel: &einoChatModel{session: session},
		ToolsConfig: compose.ToolsNodeConfig{
			Tools: einoTools,
			// 与旧循环一致按声明顺序串行执行：工具会读写同一个运行态
			// （编辑器草稿），并行会把「先试跑再交付」跑成竞态。
			ExecuteSequentially: true,
			// 模型幻觉出的工具名不该炸掉整轮：错误文本回喂模型自行纠正。
			UnknownToolsHandler: session.handleUnknownTool,
		},
		// MessageRewriter 每次模型调用前作用于累积消息并写回状态，
		// 承接旧循环的 trimTranscriptInPlace：超预算时掐掉最旧的工具结果。
		MessageRewriter: func(_ context.Context, messages []*schema.Message) []*schema.Message {
			return trimEinoMessages(messages, session.budget)
		},
		// Eino 的步数按图节点执行数计：一轮迭代 = 模型 + 工具两步。
		MaxStep:   aiAgentMaxSteps * 2,
		GraphName: "AegisFunctionAgent",
	})
	if err != nil {
		return s.emitError(session.emit, err)
	}

	if _, err := agent.Generate(ctx, initial); err != nil {
		if errors.Is(err, compose.ErrExceedMaxSteps) {
			// 与旧循环的封顶行为对齐：最后一步的工具已经跑完，
			// 收步、发提示语，整轮按成功收尾。
			session.finishStep()
			session.emitLimitNote()
			return nil
		}
		return s.emitError(session.emit, err)
	}
	session.finishStep()
	return nil
}

// emitLimitNote 触达单轮步数上限时给界面的提示语（与落库分片同步）。
func (s *einoAgentSession) emitLimitNote() {
	note := fmt.Sprintf("已达到单轮最大步数（%d 步），请继续对话让我接着做。", aiAgentMaxSteps)
	s.appendPart(agentUIPart{Type: "text", Text: note, State: "done"})
	textID := "txt_" + uuid.NewString()
	_ = s.emitChunk(map[string]any{"type": "text-start", "id": textID})
	_ = s.emitChunk(map[string]any{"type": "text-delta", "id": textID, "delta": note})
	_ = s.emitChunk(map[string]any{"type": "text-end", "id": textID})
}

// ── 模型适配器 ──

// einoChatModel 把通道链（AIProviderService，内部是官方 OpenAI/Anthropic SDK）
// 适配成 Eino 的 ToolCallingChatModel。一次 Generate = 一次流式 LLM 调用，
// 增量在这里直接翻成 AI SDK UI chunk 下发。
type einoChatModel struct {
	session *einoAgentSession
}

var _ model.ToolCallingChatModel = (*einoChatModel)(nil)

// WithTools react.NewAgent 装配时会把各工具 Info() 的聚合传进来。
// 这里刻意不用 Eino 的 ToolInfo 反推 JSON Schema：适配器持有的原始 schema
// （session.toolList）与 Info() 本就同源，反推一趟只会经 jsonschema
// 序列化多丢一层字段 —— 送供应商的必须是原始字节。
func (m *einoChatModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return m, nil
}

func (m *einoChatModel) Generate(ctx context.Context, input []*schema.Message,
	_ ...model.Option) (*schema.Message, error) {
	response, err := m.session.streamOneStep(ctx, input)
	if err != nil {
		return nil, err
	}
	return chatResponseToEino(response), nil
}

// Stream 仅为接口完备：agent 以 Invoke 模式运行（见文件头），
// 界面增量已在 Generate 内部发出，这里返回单帧流。
func (m *einoChatModel) Stream(ctx context.Context, input []*schema.Message,
	opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	message, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{message}), nil
}

// streamOneStep 跑一次 LLM 调用：正文与思考逐字发给界面，工具入参聚合后
// 以 tool-input-available 一次性交付（入参 JSON 的增量对界面没有展示价值）。
// 与旧循环的同名方法逐事件对齐 —— 界面协议是前端已经落地的契约。
func (s *einoAgentSession) streamOneStep(ctx context.Context, input []*schema.Message) (*aidomain.ChatResponse, error) {
	if err := s.beginStep(); err != nil {
		return nil, err
	}
	system, transcript := einoMessagesToChat(input)

	var (
		textID      string
		reasoningID string
		textBuffer  strings.Builder
		reasonBuf   strings.Builder
		started     = map[string]bool{}
	)
	closeText := func() {
		if textID != "" {
			_ = s.emitChunk(map[string]any{"type": "text-end", "id": textID})
			if textBuffer.Len() > 0 {
				s.appendPart(agentUIPart{Type: "text", Text: textBuffer.String(), State: "done"})
			}
			textID = ""
			textBuffer.Reset()
		}
	}
	closeReasoning := func() {
		if reasoningID != "" {
			_ = s.emitChunk(map[string]any{"type": "reasoning-end", "id": reasoningID})
			if reasonBuf.Len() > 0 {
				s.appendPart(agentUIPart{Type: "reasoning", Text: reasonBuf.String(), State: "done"})
			}
			reasoningID = ""
			reasonBuf.Reset()
		}
	}

	stream := s.chatStream
	if stream == nil {
		stream = s.service.providers.ChatStream
	}
	response, _, err := stream(ctx, aiChatArgs{
		AppID: s.input.AppID, ConfigID: s.input.ConfigID,
		Request: aidomain.ChatRequest{
			Model: s.input.Model, System: system, Messages: transcript,
			Tools: s.toolList, MaxTokens: aiAgentMaxTokens,
		},
	}, func(event aidomain.StreamEvent) error {
		switch event.Type {
		case aidomain.StreamText:
			closeReasoning()
			if textID == "" {
				textID = "txt_" + uuid.NewString()
				if err := s.emitChunk(map[string]any{"type": "text-start", "id": textID}); err != nil {
					return err
				}
			}
			textBuffer.WriteString(event.Delta)
			return s.emitChunk(map[string]any{"type": "text-delta", "id": textID, "delta": event.Delta})
		case aidomain.StreamReasoning:
			closeText()
			if reasoningID == "" {
				reasoningID = "rsn_" + uuid.NewString()
				if err := s.emitChunk(map[string]any{"type": "reasoning-start", "id": reasoningID}); err != nil {
					return err
				}
			}
			reasonBuf.WriteString(event.Delta)
			return s.emitChunk(map[string]any{"type": "reasoning-delta", "id": reasoningID, "delta": event.Delta})
		case aidomain.StreamToolStart:
			closeText()
			closeReasoning()
			if !started[event.ToolID] {
				started[event.ToolID] = true
				return s.emitChunk(map[string]any{
					"type": "tool-input-start", "toolCallId": event.ToolID,
					"toolName": event.ToolName, "dynamic": true,
				})
			}
			return nil
		case aidomain.StreamToolDelta:
			if event.ToolID == "" || event.Delta == "" {
				return nil
			}
			return s.emitChunk(map[string]any{
				"type": "tool-input-delta", "toolCallId": event.ToolID, "inputTextDelta": event.Delta,
			})
		}
		return nil
	})
	closeText()
	closeReasoning()
	if err != nil {
		return nil, err
	}

	for _, call := range response.ToolCalls {
		if !started[call.ID] {
			// 非流式回退（或供应商不发工具增量）时补上 start。
			_ = s.emitChunk(map[string]any{
				"type": "tool-input-start", "toolCallId": call.ID,
				"toolName": call.Name, "dynamic": true,
			})
		}
		_ = s.emitChunk(map[string]any{
			"type": "tool-input-available", "toolCallId": call.ID, "toolName": call.Name,
			"input": normalizeJSON(call.Arguments), "dynamic": true,
		})
	}
	s.addUsage(response.Usage)
	return response, nil
}

// ── 工具适配器 ──

// einoAgentTool 把一个统一工具（内置或 MCP）适配成 Eino 的 InvokableTool。
// 执行、限时、恐慌兜底、界面事件、落库分片全在这里落点。
type einoAgentTool struct {
	session *einoAgentSession
	spec    aidomain.Tool
}

var _ tool.InvokableTool = (*einoAgentTool)(nil)

func (t *einoAgentTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name:        t.spec.Name,
		Desc:        t.spec.Description,
		ParamsOneOf: einoParams(t.spec.InputSchema),
	}, nil
}

func (t *einoAgentTool) InvokableRun(ctx context.Context, argumentsInJSON string,
	_ ...tool.Option) (string, error) {
	return t.session.executeTool(ctx, compose.GetToolCallID(ctx), t.spec.Name,
		json.RawMessage(argumentsInJSON))
}

// executeTool 统一的工具执行落点。业务失败不返回 error —— 错误文本回喂模型
// 让它自行纠正（Eino 的工具节点一报错整张图就停了，而「函数版本不存在」
// 这类信息模型完全能救回来）；只有 ctx 取消才让整轮失败。
func (s *einoAgentSession) executeTool(ctx context.Context, callID, name string,
	args json.RawMessage) (string, error) {
	output, execErr := s.service.executeToolWithTimeout(ctx, s.run, s.toolIndex, aidomain.ToolCall{
		ID: callID, Name: name, Arguments: args,
	})
	part := agentUIPart{Type: "dynamic-tool", ToolCallID: callID, ToolName: name, Input: normalizeJSON(args)}
	if execErr != nil {
		part.State = "output-error"
		part.ErrorText = execErr.Error()
		_ = s.emitChunk(map[string]any{
			"type": "tool-output-error", "toolCallId": callID,
			"errorText": execErr.Error(), "dynamic": true,
		})
		s.appendPart(part)
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "错误：" + execErr.Error(), nil
	}
	part.State = "output-available"
	part.Output = jsonify(output)
	_ = s.emitChunk(map[string]any{
		"type": "tool-output-available", "toolCallId": callID,
		"output": json.RawMessage(jsonify(output)), "dynamic": true,
	})
	s.appendPart(part)
	return output, nil
}

// handleUnknownTool 模型编造的工具名走这里：与内置查表失败同一文案。
func (s *einoAgentSession) handleUnknownTool(ctx context.Context, name, input string) (string, error) {
	callID := compose.GetToolCallID(ctx)
	message := fmt.Sprintf("未知工具：%s", name)
	_ = s.emitChunk(map[string]any{
		"type": "tool-output-error", "toolCallId": callID,
		"errorText": message, "dynamic": true,
	})
	s.appendPart(agentUIPart{
		Type: "dynamic-tool", ToolCallID: callID, ToolName: name,
		Input: normalizeJSON(json.RawMessage(input)), State: "output-error", ErrorText: message,
	})
	return "错误：" + message, nil
}

// ── 消息与工具声明的双向转换 ──

// chatToEinoMessages 统一转录 → Eino 消息序列（系统提示词占第一条）。
func chatToEinoMessages(system string, transcript []aidomain.ChatMessage) []*schema.Message {
	messages := make([]*schema.Message, 0, len(transcript)+1)
	if strings.TrimSpace(system) != "" {
		messages = append(messages, schema.SystemMessage(system))
	}
	for _, message := range transcript {
		switch message.Role {
		case aidomain.RoleUser:
			messages = append(messages, schema.UserMessage(message.PlainText()))
		case aidomain.RoleAssistant:
			assistant := &schema.Message{Role: schema.Assistant, Content: message.PlainText()}
			for _, call := range message.ToolCalls {
				assistant.ToolCalls = append(assistant.ToolCalls, einoToolCall(call))
			}
			messages = append(messages, assistant)
		case aidomain.RoleTool:
			messages = append(messages, schema.ToolMessage(message.PlainText(), message.ToolCallID))
		}
	}
	return messages
}

// einoMessagesToChat Eino 消息序列 → 统一转录。系统消息合并进 System 字段
// （通道层按供应商协议各自安放）；思考内容不回喂，跨供应商回传格式不兼容。
func einoMessagesToChat(messages []*schema.Message) (string, []aidomain.ChatMessage) {
	var systems []string
	transcript := make([]aidomain.ChatMessage, 0, len(messages))
	for _, message := range messages {
		if message == nil {
			continue
		}
		switch message.Role {
		case schema.System:
			if message.Content != "" {
				systems = append(systems, message.Content)
			}
		case schema.User:
			transcript = append(transcript, aidomain.TextMessage(aidomain.RoleUser, message.Content))
		case schema.Assistant:
			chat := aidomain.ChatMessage{Role: aidomain.RoleAssistant}
			if message.Content != "" {
				chat.Content = []aidomain.ContentPart{{Type: aidomain.PartText, Text: message.Content}}
			}
			for _, call := range message.ToolCalls {
				arguments := json.RawMessage(call.Function.Arguments)
				if len(arguments) == 0 {
					arguments = json.RawMessage(`{}`)
				}
				chat.ToolCalls = append(chat.ToolCalls, aidomain.ToolCall{
					ID: call.ID, Name: call.Function.Name, Arguments: arguments,
				})
			}
			transcript = append(transcript, chat)
		case schema.Tool:
			transcript = append(transcript, toolResultMessage(message.ToolCallID, message.Content))
		}
	}
	return strings.Join(systems, "\n\n"), transcript
}

// chatResponseToEino 一次模型结论 → Eino assistant 消息（含用量与完成原因）。
func chatResponseToEino(response *aidomain.ChatResponse) *schema.Message {
	message := &schema.Message{
		Role:             schema.Assistant,
		Content:          response.Text,
		ReasoningContent: response.Reasoning,
		ResponseMeta: &schema.ResponseMeta{
			FinishReason: response.FinishReason,
			Usage: &schema.TokenUsage{
				PromptTokens:     int(response.Usage.InputTokens),
				CompletionTokens: int(response.Usage.OutputTokens),
				TotalTokens:      int(response.Usage.TotalTokens),
			},
		},
	}
	for _, call := range response.ToolCalls {
		message.ToolCalls = append(message.ToolCalls, einoToolCall(call))
	}
	return message
}

func einoToolCall(call aidomain.ToolCall) schema.ToolCall {
	return schema.ToolCall{
		ID:   call.ID,
		Type: "function",
		Function: schema.FunctionCall{
			Name:      call.Name,
			Arguments: string(call.Arguments),
		},
	}
}

// einoParams 原始 JSON Schema → Eino 参数声明。解析失败退化为无参数 ——
// Info() 的产物只参与 react 的装配；真正送供应商的是原始 schema
// （见 einoChatModel.WithTools 的说明）。
func einoParams(raw json.RawMessage) *schema.ParamsOneOf {
	if len(raw) == 0 {
		return nil
	}
	parsed := &jsonschema.Schema{}
	if err := json.Unmarshal(raw, parsed); err != nil {
		return nil
	}
	return schema.NewParamsOneOfByJSONSchema(parsed)
}

// ── 上下文裁剪 ──

// trimEinoMessages 循环中途超预算时，把最旧的工具结果换成占位符（保留最近
// 4 条完整），不动落库数据。这是自动压缩之外的第二道闸：循环中途历史只会
// 越滚越长，而中途做 LLM 摘要既慢又贵 —— 掐旧工具结果是零成本的等价物。
func trimEinoMessages(messages []*schema.Message, budget int) []*schema.Message {
	if estimateEinoChars(messages) <= budget {
		return messages
	}
	const keepIntact = 4
	seen := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i] == nil || messages[i].Role != schema.Tool {
			continue
		}
		seen++
		if seen <= keepIntact {
			continue
		}
		if len(messages[i].Content) > 200 {
			trimmed := *messages[i]
			trimmed.Content = fmt.Sprintf("（结果已省略以节省上下文，原始长度 %d 字符；需要时请重新调用该工具）",
				len(messages[i].Content))
			messages[i] = &trimmed
		}
	}
	return messages
}

// estimateEinoChars 上下文字符估算：正文 + 工具调用的名字与入参。
func estimateEinoChars(messages []*schema.Message) int {
	total := 0
	for _, message := range messages {
		if message == nil {
			continue
		}
		total += len(message.Content)
		for _, call := range message.ToolCalls {
			total += len(call.Function.Name) + len(call.Function.Arguments)
		}
	}
	return total
}
