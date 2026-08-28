package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	aidomain "aegis/internal/domain/ai"
)

// 这组测试盯子代理的编排语义：独立现场（角色提示词、收窄的工具面、
// 全新转录）、与主代理共享的草稿、活动直播事件、报告回喂、用量归并 ——
// LLM 仍是脚本替身（chatStream 注入点被父子会话共享，按调用顺序消费）。

// TestSubagentDispatchFullCycle 一次完整的「主代理 → coder 子代理 → 主代理」：
// 主代理派活，子代理用真实 edit_draft 改草稿并交报告，主代理收尾。
func TestSubagentDispatchFullCycle(t *testing.T) {
	steps := []einoScriptStep{
		{ // 1. 主代理：派遣 coder
			events: []aidomain.StreamEvent{
				{Type: aidomain.StreamToolStart, ToolID: "call-1", ToolName: "run_subagent"},
			},
			response: aidomain.ChatResponse{
				ToolCalls: []aidomain.ToolCall{{
					ID: "call-1", Name: "run_subagent",
					Arguments: json.RawMessage(`{"agentType":"coder","task":"把 a 的初值改成 2","context":"作者已确认"}`),
				}},
				Usage: aidomain.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
			},
		},
		{ // 2. 子代理：真实 edit_draft 修改共享草稿
			events: []aidomain.StreamEvent{
				{Type: aidomain.StreamToolStart, ToolID: "sub-1", ToolName: "edit_draft"},
			},
			response: aidomain.ChatResponse{
				ToolCalls: []aidomain.ToolCall{{
					ID: "sub-1", Name: "edit_draft",
					Arguments: json.RawMessage(`{"oldText":"const a = 1;","newText":"const a = 2;","note":"改初值"}`),
				}},
				Usage: aidomain.Usage{InputTokens: 30, OutputTokens: 8, TotalTokens: 38},
			},
		},
		{ // 3. 子代理：结果报告
			events: []aidomain.StreamEvent{
				{Type: aidomain.StreamText, Delta: "报告：已把 a 的初值改成 2 并核对。"},
			},
			response: aidomain.ChatResponse{
				Text:  "报告：已把 a 的初值改成 2 并核对。",
				Usage: aidomain.Usage{InputTokens: 40, OutputTokens: 12, TotalTokens: 52},
			},
		},
		{ // 4. 主代理：收尾
			events: []aidomain.StreamEvent{
				{Type: aidomain.StreamText, Delta: "子代理已完成修改。"},
			},
			response: aidomain.ChatResponse{
				Text:  "子代理已完成修改。",
				Usage: aidomain.Usage{InputTokens: 50, OutputTokens: 6, TotalTokens: 106},
			},
		},
	}
	service, session, chunks, _ := newEinoTestSession(t, steps, []aiAgentTool{subagentTool()})
	session.run.DraftSource = "function main() {\n  const a = 1;\n  return a;\n}\n"
	session.run.session = session

	// 包一层 chatStream 记录每次调用的 System 与工具清单：
	// 子会话复制的是包装后的函数，父子两侧的调用都会被记录。
	inner := session.chatStream
	var systems []string
	var toolNames [][]string
	session.chatStream = func(ctx context.Context, args aiChatArgs,
		onEvent func(aidomain.StreamEvent) error) (*aidomain.ChatResponse, *resolvedChannel, error) {
		systems = append(systems, args.Request.System)
		names := make([]string, 0, len(args.Request.Tools))
		for _, tool := range args.Request.Tools {
			names = append(names, tool.Name)
		}
		toolNames = append(toolNames, names)
		return inner(ctx, args, onEvent)
	}

	initial := chatToEinoMessages("主提示词", []aidomain.ChatMessage{
		aidomain.TextMessage(aidomain.RoleUser, "让 coder 改一下初值"),
	})
	if err := service.runEinoAgent(context.Background(), session, initial); err != nil {
		t.Fatalf("runEinoAgent 失败：%v", err)
	}

	// 草稿经共享 run 被子代理真实修改。
	if !strings.Contains(session.run.DraftSource, "const a = 2;") {
		t.Fatalf("子代理的编辑没有落到共享草稿：%s", session.run.DraftSource)
	}

	// 子代理那步（第 2 次模型调用）：角色提示词 + 收窄的工具面。
	if len(systems) != 4 {
		t.Fatalf("想要 4 次模型调用，得到 %d", len(systems))
	}
	subSystem := systems[1]
	for _, want := range []string{"编码专家", "子代理纪律", "const a = 1;"} {
		if !strings.Contains(subSystem, want) {
			t.Fatalf("子代理系统提示词缺「%s」：%s", want, subSystem[:min(400, len(subSystem))])
		}
	}
	subTools := strings.Join(toolNames[1], ",")
	if strings.Contains(subTools, "run_subagent") || strings.Contains(subTools, "update_plan") {
		t.Fatalf("子代理工具面不该含 run_subagent / update_plan：%s", subTools)
	}
	if !strings.Contains(subTools, "edit_draft") || !strings.Contains(subTools, "read_draft") {
		t.Fatalf("子代理工具面应含草稿读写：%s", subTools)
	}

	// 活动直播：瞬态 data-subagent 事件对齐主消息里的工具卡（id = call-1）。
	var kinds []string
	for _, chunk := range *chunks {
		if chunk["type"] != "data-subagent" {
			continue
		}
		if chunk["transient"] != true {
			t.Fatalf("data-subagent 必须是瞬态分片：%v", chunk)
		}
		data, _ := chunk["data"].(map[string]any)
		if data["id"] != "call-1" || data["agent"] != "coder" {
			t.Fatalf("直播事件没对上卡：%v", data)
		}
		kind, _ := data["kind"].(string)
		state, _ := data["state"].(string)
		kinds = append(kinds, kind+"/"+state)
	}
	want := "tool/run,tool/ok,text/"
	if strings.Join(kinds, ",") != want {
		t.Fatalf("直播事件序列不对：想要 %s，得到 %s", want, strings.Join(kinds, ","))
	}

	// 主消息里 run_subagent 的工具结果：界面通道带全量（报告、更新后的草稿），
	// 模型通道只带事实标记。
	parts, usage := session.snapshot()
	var subPart *agentUIPart
	for i := range parts {
		if parts[i].Type == "dynamic-tool" && parts[i].ToolName == "run_subagent" {
			subPart = &parts[i]
		}
	}
	if subPart == nil {
		t.Fatal("落库分片缺 run_subagent 工具卡")
	}
	ui := string(subPart.Output)
	if !strings.Contains(ui, "报告：已把 a 的初值改成 2") || !strings.Contains(ui, "编码专家") ||
		!strings.Contains(ui, "const a = 2;") {
		t.Fatalf("界面通道应带报告、角色名与更新后草稿：%s", ui)
	}
	model := string(subPart.ModelOutput)
	if strings.Contains(model, "const a = 2;") {
		t.Fatalf("模型通道不该带整篇草稿：%s", model)
	}
	if !strings.Contains(model, `"draftUpdated":true`) {
		t.Fatalf("模型通道应标记草稿已更新：%s", model)
	}

	// 用量归并：四次调用全记在主会话总账上。
	if usage.TotalTokens != 15+38+52+106 {
		t.Fatalf("用量应含子代理消耗（%d），得到 %d", 15+38+52+106, usage.TotalTokens)
	}
}

// TestSubagentRejectsBadInput 角色未知与空任务都要在派遣前拦下。
func TestSubagentRejectsBadInput(t *testing.T) {
	_, session, _, _ := newEinoTestSession(t, nil, nil)
	session.run.session = session

	if _, err := session.dispatchSubagent(context.Background(),
		json.RawMessage(`{"agentType":"pm","task":"x"}`)); err == nil ||
		!strings.Contains(err.Error(), "未知的子代理角色") {
		t.Fatalf("未知角色应报错，得到 %v", err)
	}
	if _, err := session.dispatchSubagent(context.Background(),
		json.RawMessage(`{"agentType":"coder","task":"  "}`)); err == nil ||
		!strings.Contains(err.Error(), "task 不能为空") {
		t.Fatalf("空任务应报错，得到 %v", err)
	}
}

// TestSubagentReadOnlyRoleDropsMutatingTools 只读角色（reviewer）的工具面
// 不含写操作；作者的只读开关同样叠加。
func TestSubagentReadOnlyRoleDropsMutatingTools(t *testing.T) {
	steps := []einoScriptStep{
		{ // 子代理直接交报告，不用工具
			events: []aidomain.StreamEvent{{Type: aidomain.StreamText, Delta: "审查通过"}},
			response: aidomain.ChatResponse{
				Text: "审查通过", Usage: aidomain.Usage{TotalTokens: 10},
			},
		},
	}
	_, session, _, _ := newEinoTestSession(t, steps, nil)
	session.run.session = session

	inner := session.chatStream
	var toolNames []string
	session.chatStream = func(ctx context.Context, args aiChatArgs,
		onEvent func(aidomain.StreamEvent) error) (*aidomain.ChatResponse, *resolvedChannel, error) {
		for _, tool := range args.Request.Tools {
			toolNames = append(toolNames, tool.Name)
		}
		return inner(ctx, args, onEvent)
	}

	result, err := session.dispatchSubagent(context.Background(),
		json.RawMessage(`{"agentType":"reviewer","task":"审查当前草稿"}`))
	if err != nil {
		t.Fatalf("派遣失败：%v", err)
	}
	joined := strings.Join(toolNames, ",")
	for _, banned := range []string{"create_function", "update_function_settings", "publish_version"} {
		if strings.Contains(joined, banned) {
			t.Fatalf("只读角色不该有写工具 %s：%s", banned, joined)
		}
	}

	rich, ok := result.(aiToolRichResult)
	if !ok {
		t.Fatalf("应返回双通道信封，实际 %T", result)
	}
	encoded, _ := json.Marshal(rich.Model)
	if !strings.Contains(string(encoded), "审查通过") {
		t.Fatalf("模型通道应带报告：%s", encoded)
	}
}

// TestExtractSubagentReport 报告 = 最后一次工具调用之后的全部文字。
func TestExtractSubagentReport(t *testing.T) {
	parts := []agentUIPart{
		{Type: "step-start"},
		{Type: "text", Text: "中间碎碎念"},
		{Type: "dynamic-tool", ToolName: "read_draft"},
		{Type: "step-start"},
		{Type: "text", Text: "结论 A"},
		{Type: "text", Text: "结论 B"},
	}
	if got := extractSubagentReport(parts); got != "结论 A\n\n结论 B" {
		t.Fatalf("报告提取不对：%q", got)
	}
	if got := extractSubagentReport(nil); got != "" {
		t.Fatalf("空分片应得空报告：%q", got)
	}
}
