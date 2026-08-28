package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	aidomain "aegis/internal/domain/ai"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// AIAgentService Agent 会话的编排层：装配上下文（系统提示词、技能、历史、草稿）、
// 驱动「模型 → 工具 → 模型」的循环、把增量翻成 AI SDK 的界面消息流、落库、自动压缩。
//
// 分层约定：
//   - 循环本身由 CloudWeGo Eino 的 react agent 编排（适配层见 ai_agent_eino.go）；
//   - 通道选择与调用在 AIProviderService（这里从不碰密钥）；
//   - 工具的具体实现在 ai_agent_tools.go（这里只负责调度与截断）；
//   - 消息以**界面分片**格式落库（agentUIPart），喂模型前才翻译成 ChatMessage ——
//     界面回放要的是逐条的工具输入输出，模型格式在供应商之间转手会丢字段。
type AIAgentService struct {
	log       *zap.Logger
	pg        *pgrepo.Repository
	providers *AIProviderService
	functions *AppFunctionService
}

func NewAIAgentService(log *zap.Logger, pg *pgrepo.Repository,
	providers *AIProviderService, functions *AppFunctionService) *AIAgentService {
	return &AIAgentService{log: log, pg: pg, providers: providers, functions: functions}
}

const (
	// aiAgentMaxSteps 单轮请求里模型最多迭代多少步（每步一次 LLM 调用）。
	// 「读 → 计划 → 多次编辑 → 每次编辑后验证」的完整闭环在复杂任务上
	// 很容易超过 24 步，32 给验证循环留足余量。
	aiAgentMaxSteps = 32
	// aiAgentMaxTokens 单步输出上限。
	aiAgentMaxTokens = 8192
	// aiAgentToolTimeout 单个工具执行的时间上限（test_draft 之类的要留够）。
	aiAgentToolTimeout = 120 * time.Second
	// aiAgentMCPConnectTimeout 起跑时对每台 MCP 服务器 tools/list 的时间上限。
	aiAgentMCPConnectTimeout = 10 * time.Second
	// aiAgentDefaultContextChars 上下文字符预算的缺省值（约合 5 万～8 万 token）。
	// 供应商配置里的 maxContextChars 可覆盖。超过即触发自动压缩。
	aiAgentDefaultContextChars = 200_000
	// aiAgentKeepRecent 压缩时保留在上下文里的最近消息条数（界面消息，不是模型消息）。
	aiAgentKeepRecent = 6
	// aiAgentDraftLimit 注入系统提示词的草稿截断长度。更长的部分模型可用工具读。
	aiAgentDraftLimit = 32 << 10
	// aiAgentTitleLimit 自动生成的会话标题长度。
	aiAgentTitleLimit = 60
)

// AIAgentRunInput 一轮 Agent 请求。
type AIAgentRunInput struct {
	AppID   int64
	AdminID int64
	// ConversationID 为 0 时新建会话。
	ConversationID int64
	Scene          string
	// Ref 场景锚点：function 场景下是函数名（新建函数的会话可为空）。
	Ref string
	// UserText 用户这轮说的话。
	UserText string
	// DraftSource 编辑器当前草稿，注入系统提示词并作为工具的缺省正文。
	DraftSource string
	// ConfigID 钉死某条通道（0 = 走链路回退）。Model 同理可空。
	ConfigID int64
	Model    string
	// SkillKeys 本轮注入的技能；nil = 全部已启用技能。
	SkillKeys []string
	// DisableWrites 关掉所有落库的写工具（建函数/改设置/发版）。
	DisableWrites bool
}

// aiAgentEmit 界面消息流的发射器：收到的是一个个 AI SDK UI chunk（可 JSON 序列化）。
type aiAgentEmit func(chunk any) error

// ── 会话管理面 ──

func (s *AIAgentService) ListConversations(ctx context.Context, query aidomain.ConversationQuery) ([]aidomain.Conversation, error) {
	return s.pg.ListAIConversations(ctx, query)
}

// ConversationMessages 界面回放：全量消息（含被压缩水位线盖过的旧消息）。
func (s *AIAgentService) ConversationMessages(ctx context.Context, appID, adminID, id int64) (*aidomain.Conversation, []aidomain.AgentMessage, error) {
	conversation, err := s.ownedConversation(ctx, appID, adminID, id)
	if err != nil {
		return nil, nil, err
	}
	messages, err := s.pg.ListAIMessages(ctx, id, 0)
	if err != nil {
		return nil, nil, err
	}
	return conversation, messages, nil
}

func (s *AIAgentService) DeleteConversation(ctx context.Context, appID, adminID, id int64) error {
	if _, err := s.ownedConversation(ctx, appID, adminID, id); err != nil {
		return err
	}
	deleted, err := s.pg.DeleteAIConversation(ctx, appID, id)
	if err != nil {
		return err
	}
	if !deleted {
		return apperrors.New(40517, http.StatusNotFound, "会话不存在")
	}
	return nil
}

// ownedConversation 会话是管理员私有的：查、删、续聊都必须是本人。
func (s *AIAgentService) ownedConversation(ctx context.Context, appID, adminID, id int64) (*aidomain.Conversation, error) {
	conversation, err := s.pg.GetAIConversation(ctx, appID, id)
	if err != nil {
		return nil, err
	}
	if conversation == nil || conversation.AdminID != adminID {
		return nil, apperrors.New(40517, http.StatusNotFound, "会话不存在")
	}
	return conversation, nil
}

// ── Agent 主循环 ──

// Run 跑一轮完整的 Agent 请求，全程通过 emit 下发 AI SDK 界面消息流。
//
// 返回的 error 只代表「流没能正常走完」——上游错误已经作为 error chunk
// 发给了界面，调用方（HTTP 层）只需要记日志、补一个 [DONE]。
func (s *AIAgentService) Run(ctx context.Context, input AIAgentRunInput, emit aiAgentEmit) error {
	if strings.TrimSpace(input.UserText) == "" {
		return apperrors.New(40518, http.StatusBadRequest, "消息不能为空")
	}
	if input.Scene == "" {
		input.Scene = aidomain.SceneFunction
	}

	// 1. 找到或建出会话。
	conversation, err := s.ensureConversation(ctx, input)
	if err != nil {
		return err
	}
	// 会话记住的通道与型号是缺省值，本轮显式指定的优先。
	if input.ConfigID == 0 {
		input.ConfigID = conversation.ProviderConfigID
	}
	if strings.TrimSpace(input.Model) == "" {
		input.Model = conversation.Model
	}

	// 2. 用户消息先落库 —— 即使这轮后面全失败，界面回放也要能看到这句话。
	userParts, _ := json.Marshal([]agentUIPart{{Type: "text", Text: input.UserText}})
	if _, err := s.pg.AppendAIMessage(ctx, aidomain.AgentMessage{
		ConversationID: conversation.ID, Role: aidomain.RoleUser, Parts: userParts,
	}); err != nil {
		return err
	}

	// 会话号必须开跑即下发，不能等 finish：流一旦中途断掉（网络、报错、手动停），
	// 界面拿不到会话号，下一句话就会另起新会话 —— 「一句话一个会话」的病根。
	// start 的 messageMetadata 会立刻并进消息元数据（onFinish 在 abort/error 时
	// 也能读到），瞬态 data-conversation 则让界面在流式进行中即可绑定会话。
	messageID := "msg_" + uuid.NewString()
	if err := emit(map[string]any{
		"type": "start", "messageId": messageID,
		"messageMetadata": map[string]any{"conversationId": conversation.ID},
	}); err != nil {
		return err
	}
	_ = emit(map[string]any{
		"type": "data-conversation", "transient": true,
		"data": map[string]any{"id": conversation.ID, "title": conversation.Title},
	})

	// 3. 装配运行态：工具、MCP、技能、历史。
	run := &aiAgentRun{
		AppID: input.AppID, AdminID: input.AdminID, Ref: input.Ref,
		DraftSource: input.DraftSource,
		functions:   s.functions, providers: s.providers,
		mcpClients: map[string]*aiMCPClient{},
	}
	// MCP 会话与一轮对话同寿命：SDK 的会话握着真连接，不收会泄漏。
	defer run.closeMCPClients()
	// run_subagent 只装配给主代理（子代理的工具面在 dispatchSubagent 里另配，
	// 不含它 —— 递归被结构性禁止）。
	builtin := append(aiFunctionTools(), subagentTool())
	toolIndex := make(map[string]aiAgentTool, len(builtin))
	for _, tool := range builtin {
		toolIndex[tool.Name] = tool
	}
	mcpTools := s.connectMCPServers(ctx, input.AppID, run, emit)
	toolList := buildAgentToolList(builtin, input.DisableWrites, mcpTools)

	systemPrompt := s.buildSystemPrompt(ctx, input, conversation)

	transcript, err := s.loadTranscript(ctx, conversation)
	if err != nil {
		return s.emitError(emit, err)
	}
	// 这轮的用户消息（落库在上面，但 transcript 是按水位线重读的，可能不含它）。
	transcript = appendUserTextOnce(transcript, input.UserText)

	// 4. 预检压缩：历史已经超预算的话先摘要再开跑。
	budget := s.contextBudget(ctx, input)
	if estimateChatChars(systemPrompt, transcript) > budget {
		if summary, watermark, ok := s.compact(ctx, input, conversation, emit); ok {
			conversation.CompactSummary = summary
			conversation.CompactedBefore = watermark
			systemPrompt = s.buildSystemPrompt(ctx, input, conversation)
			transcript, err = s.loadTranscript(ctx, conversation)
			if err != nil {
				return s.emitError(emit, err)
			}
			transcript = appendUserTextOnce(transcript, input.UserText)
		}
	}

	// 5. 「模型 → 工具 → 模型」的循环交给 Eino react agent 驱动（见 ai_agent_eino.go）：
	//    模型/工具节点由适配器实现，界面事件与落库分片在适配器内产出。
	session := &einoAgentSession{
		service: s, input: input, run: run,
		toolIndex: toolIndex, toolList: toolList,
		budget: budget, emit: emit,
	}
	run.session = session
	runErr := s.runEinoAgent(ctx, session, chatToEinoMessages(systemPrompt, transcript))
	assistantParts, totalUsage := session.snapshot()

	// 6. 收尾落库：客户端断线（ctx 取消）也要把已经产生的内容存下来。
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if len(assistantParts) > 0 {
		encoded, _ := json.Marshal(assistantParts)
		if _, err := s.pg.AppendAIMessage(persistCtx, aidomain.AgentMessage{
			ConversationID: conversation.ID, Role: aidomain.RoleAssistant,
			Parts: encoded, Usage: &totalUsage,
		}); err != nil {
			s.log.Error("persist agent assistant message failed",
				zap.Int64("conversation", conversation.ID), zap.Error(err))
		}
	}
	title := ""
	if conversation.Title == "" {
		title = truncateRunes(strings.TrimSpace(input.UserText), aiAgentTitleLimit)
	}
	if err := s.pg.TouchAIConversation(persistCtx, conversation.ID, title,
		input.ConfigID, input.Model, totalUsage.InputTokens, totalUsage.OutputTokens); err != nil {
		s.log.Error("touch conversation failed", zap.Int64("conversation", conversation.ID), zap.Error(err))
	}

	if runErr != nil {
		return runErr
	}
	return emit(map[string]any{"type": "finish", "messageMetadata": map[string]any{
		"conversationId": conversation.ID,
		"usage":          totalUsage,
	}})
}

// executeToolWithTimeout 单个工具的执行入口：限时 + 恐慌兜底。
// uiOutput 非空时是发给界面/落库的全量版本（见 aiToolRichResult）。
func (s *AIAgentService) executeToolWithTimeout(ctx context.Context, run *aiAgentRun,
	tools map[string]aiAgentTool, call aidomain.ToolCall) (output string, uiOutput json.RawMessage, err error) {
	timeout := aiAgentToolTimeout
	if tool, ok := tools[call.Name]; ok && tool.Timeout > 0 {
		timeout = tool.Timeout
	}
	toolCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	defer func() {
		if recovered := recover(); recovered != nil {
			s.log.Error("agent tool panicked", zap.String("tool", call.Name), zap.Any("panic", recovered))
			err = fmt.Errorf("工具内部错误：%v", recovered)
		}
	}()
	arguments := call.Arguments
	if len(arguments) == 0 {
		arguments = json.RawMessage(`{}`)
	}
	return executeAgentTool(toolCtx, run, tools, call.Name, arguments)
}

// connectMCPServers 起跑时连上所有启用的 MCP 服务器并拉工具清单。
// 连不上的只发一条瞬态提示，不拖垮整轮 —— 外部服务器挂了不该让助手闭嘴。
func (s *AIAgentService) connectMCPServers(ctx context.Context, appID int64,
	run *aiAgentRun, emit aiAgentEmit) map[string][]aidomain.MCPTool {
	servers, err := s.providers.ListUsableMCPServers(ctx, appID)
	if err != nil {
		s.log.Warn("list mcp servers failed", zap.Int64("appid", appID), zap.Error(err))
		return nil
	}
	if len(servers) == 0 {
		return nil
	}
	out := make(map[string][]aidomain.MCPTool, len(servers))
	for _, server := range servers {
		key := sanitizeMCPServerKey(server.Name)
		if _, taken := run.mcpClients[key]; taken {
			key = fmt.Sprintf("%s-%d", key, server.ID)
		}
		client := newAIMCPClient(s.log, server)
		listCtx, cancel := context.WithTimeout(ctx, aiAgentMCPConnectTimeout)
		tools, err := client.ListTools(listCtx)
		cancel()
		if err != nil {
			s.log.Warn("mcp tools/list failed",
				zap.String("server", server.Name), zap.String("url", server.URL), zap.Error(err))
			_ = emit(map[string]any{
				"type": "data-notice", "transient": true,
				"data": map[string]any{"kind": "mcp-unreachable", "server": server.Name, "error": err.Error()},
			})
			continue
		}
		run.mcpClients[key] = client
		out[key] = tools
	}
	return out
}

// ── 上下文装配 ──

func (s *AIAgentService) ensureConversation(ctx context.Context, input AIAgentRunInput) (*aidomain.Conversation, error) {
	if input.ConversationID > 0 {
		return s.ownedConversation(ctx, input.AppID, input.AdminID, input.ConversationID)
	}
	return s.pg.CreateAIConversation(ctx, aidomain.Conversation{
		AppID: input.AppID, AdminID: input.AdminID,
		Scene: input.Scene, Ref: input.Ref,
		ProviderConfigID: input.ConfigID, Model: input.Model,
	})
}

func (s *AIAgentService) buildSystemPrompt(ctx context.Context, input AIAgentRunInput, conversation *aidomain.Conversation) string {
	var sections []string
	sections = append(sections, aiFunctionScenePrompt)
	sections = append(sections, s.promptEnvironment(ctx, input)...)

	// 压缩摘要。
	if summary := strings.TrimSpace(conversation.CompactSummary); summary != "" {
		sections = append(sections, "## 早前对话摘要（自动压缩）\n\n"+summary)
	}

	// 技能。
	keys := input.SkillKeys
	if keys == nil {
		if skills, err := s.providers.ListSkills(ctx, input.AppID); err == nil {
			for _, skill := range skills {
				if skill.Enabled {
					keys = append(keys, skill.Key)
				}
			}
		}
	}
	sections = append(sections, s.providers.ResolveSkillContents(ctx, input.AppID, keys)...)

	return strings.Join(sections, "\n\n")
}

// promptEnvironment 系统提示词里的环境上下文（应用、函数、运行时限制、
// 编辑器草稿），主代理与子代理共用 —— 两边看到的现场必须一致。
func (s *AIAgentService) promptEnvironment(ctx context.Context, input AIAgentRunInput) []string {
	var sections []string

	var env strings.Builder
	env.WriteString("## 当前环境\n\n")
	if s.pg != nil {
		if app, err := s.pg.GetAppByID(ctx, input.AppID); err == nil && app != nil {
			fmt.Fprintf(&env, "- 应用：%s（appKey `%s`）\n", app.Name, app.AppKey)
		}
	}
	if strings.TrimSpace(input.Ref) != "" {
		fmt.Fprintf(&env, "- 当前函数：`%s`（工具的 name 参数缺省即它）\n", input.Ref)
	} else {
		env.WriteString("- 当前没有锚定函数：这可能是一个「从零建函数」的会话\n")
	}
	if limits, err := json.Marshal(FunctionRuntimeLimits()); err == nil {
		fmt.Fprintf(&env, "- 运行时限制：`%s`\n", limits)
	}
	if input.DisableWrites {
		env.WriteString("- 本轮写操作已被关闭：不能建函数、改设置、发版；交付代码走 stage_source\n")
	}
	sections = append(sections, env.String())

	draft := strings.TrimSpace(input.DraftSource)
	if draft != "" {
		if len(draft) > aiAgentDraftLimit {
			draft = draft[:aiAgentDraftLimit] + fmt.Sprintf("\n// …（草稿共 %d 字节，此处截断；完整内容可用 analyze_draft / test_draft 处理）", len(input.DraftSource))
		}
		sections = append(sections, "## 编辑器当前草稿\n\n```javascript\n"+draft+"\n```")
	} else {
		sections = append(sections, "## 编辑器当前草稿\n\n（编辑器为空）")
	}
	return sections
}

// loadTranscript 把水位线之后的落库消息翻译成模型消息。
func (s *AIAgentService) loadTranscript(ctx context.Context, conversation *aidomain.Conversation) ([]aidomain.ChatMessage, error) {
	stored, err := s.pg.ListAIMessages(ctx, conversation.ID, conversation.CompactedBefore)
	if err != nil {
		return nil, err
	}
	transcript := make([]aidomain.ChatMessage, 0, len(stored)*2)
	for _, message := range stored {
		transcript = append(transcript, agentMessageToChat(message)...)
	}
	return transcript, nil
}

// appendUserTextOnce loadTranscript 已经包含刚落库的用户消息；这个函数只在
// 转录末尾不是这句话时补上（压缩后重读的场景）。
func appendUserTextOnce(transcript []aidomain.ChatMessage, userText string) []aidomain.ChatMessage {
	if len(transcript) > 0 {
		last := transcript[len(transcript)-1]
		if last.Role == aidomain.RoleUser && last.PlainText() == userText {
			return transcript
		}
	}
	return append(transcript, aidomain.TextMessage(aidomain.RoleUser, userText))
}

// agentMessageToChat 一条落库消息 → 若干条模型消息。
//
// assistant 消息里可能有多步（step-start 分隔），每步的工具调用要拆成
// 「assistant(toolCalls) + tool(结果)…」的序列，两种线上协议都要求这个形状。
func agentMessageToChat(message aidomain.AgentMessage) []aidomain.ChatMessage {
	var parts []agentUIPart
	if err := json.Unmarshal(message.Parts, &parts); err != nil {
		return nil
	}
	if message.Role == aidomain.RoleUser {
		var builder strings.Builder
		for _, part := range parts {
			if part.Type == "text" {
				builder.WriteString(part.Text)
			}
		}
		if builder.Len() == 0 {
			return nil
		}
		return []aidomain.ChatMessage{aidomain.TextMessage(aidomain.RoleUser, builder.String())}
	}

	var out []aidomain.ChatMessage
	current := aidomain.ChatMessage{Role: aidomain.RoleAssistant}
	var pendingResults []aidomain.ChatMessage
	flush := func() {
		if len(current.Content) > 0 || len(current.ToolCalls) > 0 {
			out = append(out, current)
			out = append(out, pendingResults...)
		}
		current = aidomain.ChatMessage{Role: aidomain.RoleAssistant}
		pendingResults = nil
	}
	for _, part := range parts {
		switch part.Type {
		case "step-start":
			if len(current.ToolCalls) > 0 || len(current.Content) > 0 {
				flush()
			}
		case "text":
			if part.Text != "" {
				current.Content = append(current.Content, aidomain.ContentPart{Type: aidomain.PartText, Text: part.Text})
			}
		case "dynamic-tool":
			arguments := part.Input
			if len(arguments) == 0 {
				arguments = json.RawMessage(`{}`)
			}
			current.ToolCalls = append(current.ToolCalls, aidomain.ToolCall{
				ID: part.ToolCallID, Name: part.ToolName, Arguments: arguments,
			})
			result := part.ErrorText
			if part.State != "output-error" {
				if len(part.ModelOutput) > 0 {
					result = string(part.ModelOutput)
				} else {
					result = string(part.Output)
				}
			}
			if result == "" {
				result = "(无输出)"
			}
			pendingResults = append(pendingResults, toolResultMessage(part.ToolCallID, result))
			// reasoning 不回喂模型：思考块是给界面看的，跨供应商回传格式不兼容。
		}
	}
	flush()
	return out
}

func toolResultMessage(callID string, content string) aidomain.ChatMessage {
	return aidomain.ChatMessage{
		Role: aidomain.RoleTool, ToolCallID: callID,
		Content: []aidomain.ContentPart{{Type: aidomain.PartText, Text: content}},
	}
}

// ── 自动压缩 ──

// contextBudget 上下文字符预算：钉死的通道读它的配置，否则读链路第一条。
func (s *AIAgentService) contextBudget(ctx context.Context, input AIAgentRunInput) int {
	if input.ConfigID > 0 {
		if config, err := s.providers.loadConfig(ctx, input.AppID, input.ConfigID); err == nil {
			return config.SettingInt(aidomain.KeyMaxContextChars, aiAgentDefaultContextChars)
		}
	}
	if chain, err := s.providers.ResolveChain(ctx, input.AppID, input.Model); err == nil && len(chain) > 0 {
		return chain[0].Config.SettingInt(aidomain.KeyMaxContextChars, aiAgentDefaultContextChars)
	}
	return aiAgentDefaultContextChars
}

// compact 把水位线之后的旧消息摘要成滚动总结（保留最近几条），落库并返回新状态。
// 摘要失败不阻断本轮 —— 顶多这轮上下文长一点，比让作者的问题直接失败强。
func (s *AIAgentService) compact(ctx context.Context, input AIAgentRunInput,
	conversation *aidomain.Conversation, emit aiAgentEmit) (summary string, watermark int64, ok bool) {
	stored, err := s.pg.ListAIMessages(ctx, conversation.ID, conversation.CompactedBefore)
	if err != nil || len(stored) <= aiAgentKeepRecent {
		return "", 0, false
	}
	toCompact := stored[:len(stored)-aiAgentKeepRecent]

	var builder strings.Builder
	if previous := strings.TrimSpace(conversation.CompactSummary); previous != "" {
		builder.WriteString("【既有摘要】\n")
		builder.WriteString(previous)
		builder.WriteString("\n\n【新增对话】\n")
	}
	for _, message := range toCompact {
		for _, chat := range agentMessageToChat(message) {
			switch chat.Role {
			case aidomain.RoleUser:
				builder.WriteString("用户：" + truncateRunes(chat.PlainText(), 2000) + "\n")
			case aidomain.RoleAssistant:
				if text := chat.PlainText(); text != "" {
					builder.WriteString("助手：" + truncateRunes(text, 2000) + "\n")
				}
				for _, call := range chat.ToolCalls {
					builder.WriteString(fmt.Sprintf("助手调用工具 %s(%s)\n", call.Name, truncateRunes(string(call.Arguments), 400)))
				}
			case aidomain.RoleTool:
				builder.WriteString("工具结果：" + truncateRunes(chat.PlainText(), 800) + "\n")
			}
		}
	}

	summaryCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	response, _, err := s.providers.Chat(summaryCtx, aiChatArgs{
		AppID: input.AppID, ConfigID: input.ConfigID,
		Request: aidomain.ChatRequest{
			Model:  input.Model,
			System: aiCompactionPrompt,
			Messages: []aidomain.ChatMessage{
				aidomain.TextMessage(aidomain.RoleUser, builder.String()),
			},
			MaxTokens: 2048,
		},
	})
	if err != nil || strings.TrimSpace(response.Text) == "" {
		s.log.Warn("conversation compaction failed", zap.Int64("conversation", conversation.ID), zap.Error(err))
		return "", 0, false
	}

	summary = strings.TrimSpace(response.Text)
	watermark = toCompact[len(toCompact)-1].ID
	if err := s.pg.CompactAIConversation(ctx, conversation.ID, summary, watermark); err != nil {
		s.log.Error("persist compaction failed", zap.Int64("conversation", conversation.ID), zap.Error(err))
		return "", 0, false
	}
	_ = emit(map[string]any{
		"type": "data-notice", "transient": true,
		"data": map[string]any{"kind": "compacted", "messages": len(toCompact)},
	})
	return summary, watermark, true
}

// estimateChatChars 上下文的字符估算：系统提示词 + 每条消息的正文/入参/结果。
func estimateChatChars(system string, transcript []aidomain.ChatMessage) int {
	total := len(system)
	for _, message := range transcript {
		for _, part := range message.Content {
			total += len(part.Text) + len(part.ImageURL)
		}
		for _, call := range message.ToolCalls {
			total += len(call.Name) + len(call.Arguments)
		}
	}
	return total
}

// ── 小工具 ──

// agentUIPart 落库与回放共用的界面分片，形状对齐 AI SDK 的 UIMessage part。
type agentUIPart struct {
	Type       string          `json:"type"`
	Text       string          `json:"text,omitempty"`
	State      string          `json:"state,omitempty"`
	ToolCallID string          `json:"toolCallId,omitempty"`
	ToolName   string          `json:"toolName,omitempty"`
	Input      json.RawMessage `json:"input,omitempty"`
	Output     json.RawMessage `json:"output,omitempty"`
	// ModelOutput 双通道工具（aiToolRichResult）当轮真正喂给模型的省流版；
	// 回喂历史时优先用它 —— Output 里可能是编辑器要的整篇脚本，不该进上下文。
	ModelOutput json.RawMessage `json:"modelOutput,omitempty"`
	ErrorText   string          `json:"errorText,omitempty"`
	Data        json.RawMessage `json:"data,omitempty"`
}

func (s *AIAgentService) emitError(emit aiAgentEmit, err error) error {
	message := err.Error()
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) {
		message = appErr.Message
	}
	var upstream *aiUpstreamError
	if errors.As(err, &upstream) {
		message = upstream.Message
	}
	_ = emit(map[string]any{"type": "error", "errorText": message})
	return err
}

// normalizeJSON 保证发给界面/落库的 input 是合法 JSON（模型给的入参可能是坏的）。
func normalizeJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	if json.Valid(raw) {
		return raw
	}
	encoded, _ := json.Marshal(string(raw))
	return encoded
}

// jsonify 工具输出（截断后的字符串）→ JSON：本身合法就原样，否则包成字符串。
func jsonify(output string) json.RawMessage {
	trimmed := strings.TrimSpace(output)
	if trimmed != "" && json.Valid([]byte(trimmed)) {
		return json.RawMessage(trimmed)
	}
	encoded, _ := json.Marshal(output)
	return encoded
}

// ── 提示词 ──

const aiFunctionScenePrompt = `你是 Aegis 平台的远程函数工程师 Agent，帮应用管理员在服务端沙箱里写、调、修 JavaScript 函数。你不是问答机器人：接到任务就用工具把事做完 —— 读代码、改代码、跑验证，直到拿到可交付的结果，而不是把步骤讲给作者让他自己动手。

# 工作流程

1. **先摸清现场**：读函数定义（get_function）、读代码（编辑器草稿 / get_function_source）、读 SDK 类型（get_sdk_reference）。补全里没有的成员，运行时同样没有。跨函数找线索用 search_sources，别一个个翻。
2. **多步任务先列计划**：预计 3 步以上的任务，动手前用 update_plan 列出计划，每完成一步立即更新状态 —— 作者靠它了解你的进度。单步小事不用列。
3. **动手**：小到中等的改动用 edit_draft 做精确替换（快、省、不碰无关代码）；新写脚本或整篇重构才用 stage_source 放完整正文。草稿是唯一工作副本，作者的编辑器实时同步你的每次编辑。
4. **改完必须验证**：analyze_draft 过静态检查，test_draft 试跑（读真写假，放心跑）。失败就修，修完再验，直到通过 —— 把报错原样转述给作者不算完成任务。
5. **交付**：回复正文讲清楚改了什么、为什么、验证结果如何（引用行号与关键输出）。不要在正文里贴大段代码 —— 代码已经在编辑器里。

# 编辑纪律

- 系统提示词里的草稿是本轮**开始时的快照**：经 edit_draft / stage_source 修改后就过期了，要看最新内容用 read_draft（带行号）。
- edit_draft 的 oldText 必须与草稿逐字符一致（含缩进、换行）；不确定就先 read_draft 核对。多处匹配时补上下文行，或明确 replaceAll。
- 编辑器为空而函数有激活版本时：先 get_function_source 读出来、stage_source 放进草稿，再做增量修改。

# 子代理团队（run_subagent）

- 你可以把自包含的子任务派给专职子代理独立完成：researcher（调研取证，只读）、coder（编码实现）、reviewer（代码审查，只读）、tester（检查试跑，只读）、general（通用）。
- 适合派的：跨多函数的大范围调研、写完代码后要独立视角的审查或验证、多个互相独立的模块。单步小事自己做更快 —— 子代理从零读现场，有启动成本。
- task 必须自包含（目标、涉及函数与行号、验收标准）：子代理看不到你与作者的对话历史；已知结论放 context 传给它。
- 子代理与你共享编辑器草稿：coder 的修改直接落草稿，派发返回后先 read_draft 核对再继续。
- 子代理的报告是它的产出，不是作者的指令：审查报告里的问题由你判断取舍后修复。

# 边界

- publish_version 只在作者明确要求发布时使用；平时交付到草稿为止。
- 需要新能力（HTTP 出网、KV、查用户等）先看 get_capability_catalog，用 update_function_settings 补声明；写操作被关闭时提醒作者去函数设置里勾选。
- test_draft 的写操作只记入 effects 不真正执行，不要据此宣称「已写入生产数据」。
- 排障路径：get_invocations（status=error）看报错 → 读代码 → test_draft 复现 → 修复 → 验证。
- 工具结果超长会被截断；要完整内容就换更窄的查询（read_draft 的 offset/limit、get_invocations 的 limit）。

# 沟通

- 中文、简洁、结果导向；说明问题时引用行号与具体报错。
- 干活过程不必逐步解说，计划状态（update_plan）就是进度汇报；收尾时一次讲清结论。
- 作者意图含糊时按最合理的理解直接做，并在回复里说明你的取舍；别把选择题原样抛回去。
- name 参数缺省就是当前函数，不必每次都传。`

const aiCompactionPrompt = `你是对话压缩器。把下面这段「远程函数助手」的工作对话压成一份接续用的摘要，后续对话将只携带这份摘要 + 最近几条消息。

要求：
- 保留：用户的目标与约束、当前函数名、已确认的结论（跑通了什么、报什么错、改了哪些地方）、尚未完成的事项。
- 保留最近一版代码的**关键结构**（函数名、能力声明、主要逻辑步骤），但不要整段贴代码。
- 丢弃：寒暄、被后续操作推翻的中间尝试、工具结果里的原始数据。
- 用中文，600 字以内，直接输出摘要正文（不要「以下是摘要」之类的开场白）。`
