package service

// 子代理与 Agent 团队。
//
// run_subagent 让主代理把一个自包含的子任务派给专职子代理独立完成：
// 子代理拥有**独立的上下文窗口**（全新系统提示词 + 只有任务描述的转录）、
// 按角色收窄的工具面与步数预算，由同一套 Eino react 循环驱动（模型调用
// 走同一条通道链，即官方 OpenAI / Anthropic / Gemini SDK）。
//
// 与主代理的共享面只有两个，都是有意为之：
//   - 运行态 run（编辑器草稿、MCP 会话）：coder 的编辑要落到作者的编辑器，
//     草稿是全队唯一的工作副本；
//   - 界面流：子代理的过程以瞬态 data-subagent 分片直播（不落库、不进
//     主代理转录），最终报告作为工具结果回给主代理与界面。
//
// 递归被结构性禁止：子代理的工具面不含 run_subagent。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	aidomain "aegis/internal/domain/ai"
)

const (
	// aiSubagentMaxSteps 子代理的步数预算。子任务应当是主任务的一角，
	// 用不满一半主预算；真用满说明任务拆得太大，报告里会带出这个信号。
	aiSubagentMaxSteps = 14
	// aiSubagentTimeout run_subagent 整体的时间上限（覆盖默认工具超时）。
	aiSubagentTimeout = 8 * time.Minute
	// aiSubagentEventTextLimit 直播事件里文本片段的截断长度。
	aiSubagentEventTextLimit = 240
)

// aiSubagentProfile 一个团队角色的档案。
type aiSubagentProfile struct {
	Key   string
	Label string
	// ReadOnly 角色天然只读（调研/审查不该动草稿），与作者的只读开关取并集。
	ReadOnly bool
	Prompt   string
}

// aiSubagentProfiles 团队编制。键是 run_subagent 的 agentType。
func aiSubagentProfiles() []aiSubagentProfile {
	return []aiSubagentProfile{
		{
			Key: "general", Label: "通用执行者",
			Prompt: "职责：按任务描述完成工程操作，读写权限与主代理一致。",
		},
		{
			Key: "researcher", Label: "调研员", ReadOnly: true,
			Prompt: "职责：调研与取证。跨函数检索（search_sources）、读函数定义与脚本、" +
				"查调用审计（get_invocations）与运行统计，把证据整理成结构化报告。" +
				"你是只读的：不修改草稿、不落库。",
		},
		{
			Key: "coder", Label: "编码专家",
			Prompt: "职责：编码。按任务实现或修改脚本（小改用 edit_draft 精确替换，" +
				"整篇才用 stage_source），改完必须 analyze_draft 过静态检查、test_draft 试跑，" +
				"报告里给出验证结果与关键行号。",
		},
		{
			Key: "reviewer", Label: "审查员", ReadOnly: true,
			Prompt: "职责：代码审查。read_draft / get_function_source 读代码，从正确性、" +
				"边界条件、安全、性能四个角度找问题，按严重程度列出（附行号与修复建议）。" +
				"你是只读的：发现的问题交给主代理修。",
		},
		{
			Key: "tester", Label: "测试员", ReadOnly: true,
			Prompt: "职责：验证。analyze_draft 静态检查 + test_draft 试跑（覆盖正常与" +
				"边界输入，可多组），报告通过 / 失败、关键输出与失败的复现细节。",
		},
	}
}

func subagentProfileByKey(key string) (aiSubagentProfile, bool) {
	for _, profile := range aiSubagentProfiles() {
		if profile.Key == key {
			return profile, true
		}
	}
	return aiSubagentProfile{}, false
}

// subagentTool run_subagent 的工具声明。Execute 经 run.session 回到本轮会话现场
// （工具执行签名只有 run，会话在 Run 里注入）。
func subagentTool() aiAgentTool {
	keys := aiSubagentProfiles()
	names := make([]string, 0, len(keys))
	for _, profile := range keys {
		names = append(names, profile.Key)
	}
	return aiAgentTool{
		Name: "run_subagent",
		Description: "把一个自包含的子任务派给专职子代理独立完成，返回结果报告。" +
			"agentType：researcher 调研取证（只读）/ coder 编码实现 / reviewer 代码审查（只读）/ " +
			"tester 检查试跑（只读）/ general 通用。适合跨多函数的大范围调研、改完需要独立视角的" +
			"审查或验证、多个互相独立的模块；单步小事自己做更快。子代理看不到对话历史，" +
			"task 必须自包含（目标、涉及函数、验收标准）。子代理与你共享编辑器草稿。",
		InputSchema: json.RawMessage(`{"type":"object","properties":{
			"agentType":{"type":"string","enum":["` + strings.Join(names, `","`) + `"],"description":"派遣的角色"},
			"task":{"type":"string","description":"子任务描述：目标、涉及的函数与行号、验收标准，必须自包含"},
			"context":{"type":"string","description":"补充材料（可选）：已知结论、相关片段、注意事项"}},
			"required":["agentType","task"]}`),
		Timeout: aiSubagentTimeout,
		Execute: func(ctx context.Context, run *aiAgentRun, args json.RawMessage) (any, error) {
			if run.session == nil {
				return nil, errors.New("子代理当前不可用")
			}
			return run.session.dispatchSubagent(ctx, args)
		},
	}
}

// dispatchSubagent 派遣一次子代理：装配独立现场 → 跑完 → 汇总报告。
func (s *einoAgentSession) dispatchSubagent(ctx context.Context, args json.RawMessage) (any, error) {
	var input struct {
		AgentType string `json:"agentType"`
		Task      string `json:"task"`
		Context   string `json:"context"`
	}
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("入参不是合法 JSON：%w", err)
	}
	profile, ok := subagentProfileByKey(strings.TrimSpace(input.AgentType))
	if !ok {
		return nil, fmt.Errorf("未知的子代理角色：%s", input.AgentType)
	}
	task := strings.TrimSpace(input.Task)
	if task == "" {
		return nil, errors.New("task 不能为空：子代理看不到对话历史，任务描述必须自包含")
	}

	// 工具面：内置集去掉主代理专属（计划是主代理的进度面板；run_subagent 不给，
	// 结构性禁止递归），并入主代理已连上的 MCP 工具（共享 run.mcpClients）。
	disableWrites := s.input.DisableWrites || profile.ReadOnly
	subIndex := map[string]aiAgentTool{}
	builtin := make([]aiAgentTool, 0, 16)
	for _, tool := range aiFunctionTools() {
		if tool.Name == "update_plan" {
			continue
		}
		builtin = append(builtin, tool)
		subIndex[tool.Name] = tool
	}
	subList := buildAgentToolList(builtin, disableWrites, nil)
	for _, spec := range s.toolList {
		if strings.HasPrefix(spec.Name, aiMCPToolPrefix) {
			subList = append(subList, spec)
		}
	}

	// 独立上下文：全新输入（草稿取派遣时刻的最新值）、全新系统提示词、
	// 只有任务描述的转录。
	subInput := s.input
	subInput.DisableWrites = disableWrites
	subInput.DraftSource = s.run.DraftSource
	systemPrompt := s.service.buildSubagentPrompt(ctx, subInput, profile)
	userText := task
	if extra := strings.TrimSpace(input.Context); extra != "" {
		userText += "\n\n## 主代理提供的补充材料\n\n" + extra
	}
	transcript := []aidomain.ChatMessage{aidomain.TextMessage(aidomain.RoleUser, userText)}

	callID := toolCallIDFromContext(ctx)
	sub := &einoAgentSession{
		service: s.service, input: subInput, run: s.run,
		toolIndex: subIndex, toolList: subList,
		budget:     s.budget,
		emit:       s.subagentEmitter(callID, profile),
		chatStream: s.chatStream,
		maxSteps:   aiSubagentMaxSteps,
	}

	draftBefore := s.run.DraftSource
	runErr := s.service.runEinoAgent(ctx, sub, chatToEinoMessages(systemPrompt, transcript))
	parts, usage := sub.snapshot()
	// 子代理烧的 token 记在主代理这轮的总账上（finish 的用量、会话累计都含它）。
	s.addUsage(usage)
	if runErr != nil {
		return nil, fmt.Errorf("子代理（%s）执行失败：%s", profile.Label, runErr.Error())
	}

	report := extractSubagentReport(parts)
	if report == "" {
		report = "（子代理没有产出文字报告）"
	}
	steps, toolCalls := 0, 0
	for _, part := range parts {
		switch part.Type {
		case "step-start":
			steps++
		case "dynamic-tool":
			toolCalls++
		}
	}

	model := map[string]any{
		"agentType": profile.Key, "report": report,
		"steps": steps, "toolCalls": toolCalls,
	}
	ui := map[string]any{
		"agentType": profile.Key, "label": profile.Label, "report": report,
		"steps": steps, "toolCalls": toolCalls, "usage": usage,
	}
	if s.run.DraftSource != draftBefore {
		// 草稿被子代理更新过：把最终全文带给界面（编辑器经 findStagedSource 同步），
		// 模型通道只带事实标记 —— 主代理要看内容自己 read_draft。
		model["draftUpdated"] = true
		ui["source"] = s.run.DraftSource
		ui["note"] = profile.Label + "（子代理）更新了草稿"
	}
	return aiToolRichResult{Model: model, UI: ui}, nil
}

// subagentEmitter 把子代理的 UI 流翻译成主流里的瞬态 data-subagent 事件：
// 工具启停、文本段落、错误，供界面的子代理卡片直播活动；步边界、
// 逐字增量、计划分片一律吞掉 —— 那是主消息流的协议，混进去会打乱状态机。
func (s *einoAgentSession) subagentEmitter(callID string, profile aiSubagentProfile) aiAgentEmit {
	var mu sync.Mutex
	toolNames := map[string]string{}
	textBuffers := map[string]*strings.Builder{}
	send := func(event map[string]any) error {
		event["id"] = callID
		event["agent"] = profile.Key
		event["label"] = profile.Label
		return s.emitChunk(map[string]any{"type": "data-subagent", "transient": true, "data": event})
	}
	return func(chunk any) error {
		frame, ok := chunk.(map[string]any)
		if !ok {
			return nil
		}
		mu.Lock()
		defer mu.Unlock()
		kind, _ := frame["type"].(string)
		switch kind {
		case "tool-input-available":
			id, _ := frame["toolCallId"].(string)
			name, _ := frame["toolName"].(string)
			toolNames[id] = name
			return send(map[string]any{"kind": "tool", "tool": name, "state": "run"})
		case "tool-output-available":
			id, _ := frame["toolCallId"].(string)
			return send(map[string]any{"kind": "tool", "tool": toolNames[id], "state": "ok"})
		case "tool-output-error":
			id, _ := frame["toolCallId"].(string)
			detail, _ := frame["errorText"].(string)
			return send(map[string]any{
				"kind": "tool", "tool": toolNames[id], "state": "error",
				"detail": truncateRunes(detail, aiSubagentEventTextLimit),
			})
		case "text-start":
			id, _ := frame["id"].(string)
			textBuffers[id] = &strings.Builder{}
		case "text-delta":
			id, _ := frame["id"].(string)
			if buffer, exists := textBuffers[id]; exists {
				delta, _ := frame["delta"].(string)
				buffer.WriteString(delta)
			}
		case "text-end":
			id, _ := frame["id"].(string)
			buffer, exists := textBuffers[id]
			if !exists {
				return nil
			}
			delete(textBuffers, id)
			if text := strings.TrimSpace(buffer.String()); text != "" {
				return send(map[string]any{"kind": "text", "text": truncateRunes(text, aiSubagentEventTextLimit)})
			}
		case "error":
			detail, _ := frame["errorText"].(string)
			return send(map[string]any{"kind": "error", "detail": truncateRunes(detail, aiSubagentEventTextLimit)})
		}
		return nil
	}
}

// extractSubagentReport 子代理的报告 = 最后一次工具调用之后的全部文字
// （中间步骤的碎碎念不要，跟主代理「收尾时一次讲清结论」同一纪律）。
func extractSubagentReport(parts []agentUIPart) string {
	var collected []string
	for i := len(parts) - 1; i >= 0; i-- {
		switch parts[i].Type {
		case "text":
			collected = append([]string{parts[i].Text}, collected...)
		case "step-start":
			continue
		default:
			return strings.TrimSpace(strings.Join(collected, "\n\n"))
		}
	}
	return strings.TrimSpace(strings.Join(collected, "\n\n"))
}

// buildSubagentPrompt 子代理的系统提示词：角色定位 + 团队纪律 + 与主代理
// 相同的环境上下文（应用、函数、草稿快照、运行时限制、技能）。
func (s *AIAgentService) buildSubagentPrompt(ctx context.Context, input AIAgentRunInput, profile aiSubagentProfile) string {
	sections := []string{fmt.Sprintf(aiSubagentScenePrompt, profile.Label, profile.Prompt)}
	sections = append(sections, s.promptEnvironment(ctx, input)...)

	if s.providers != nil {
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
	}
	return strings.Join(sections, "\n\n")
}

const aiSubagentScenePrompt = `你是 Aegis 平台远程函数工程团队的「%s」（子代理），由主代理派遣，独立完成一个明确的子任务。

%s

# 子代理纪律

- 只做被派的子任务，不扩大范围；做完就收。
- 你的最终回复是给主代理看的结果报告：结论先行、条目化，引用行号与关键输出；不要贴大段代码 —— 主代理会自己读草稿。
- 你看不到主代理与作者的对话，也不能向任何人提问：材料不够时按最合理的假设做，并在报告里注明假设。
- 编辑器草稿是与主代理共享的工作副本：你的每次编辑都会实时同步到作者的编辑器；编辑前先 read_draft 核对最新内容。
- 系统提示词里的草稿是派遣时刻的快照，经编辑后就过期了 —— 以 read_draft 为准。`

// ── 工具调用 ID 的上下文传递 ──
//
// 工具的 Execute 签名只有 (ctx, run, args)；run_subagent 需要本次调用的
// toolCallId 来给直播事件对卡。ID 在 executeTool 处入 ctx，这里取出。

type aiToolCallIDKey struct{}

func withToolCallID(ctx context.Context, callID string) context.Context {
	return context.WithValue(ctx, aiToolCallIDKey{}, callID)
}

func toolCallIDFromContext(ctx context.Context) string {
	callID, _ := ctx.Value(aiToolCallIDKey{}).(string)
	return callID
}
