package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	aidomain "aegis/internal/domain/ai"

	"go.uber.org/zap"
)

// 这组测试盯的是 Eino react agent 的接线本身：Invoke 模式下的分支判定、
// 工具按名分发、未知工具兜底、步数封顶、消息在迭代间的累积 —— LLM 用脚本
// 替身（session.chatStream 注入点），工具用假实现，全程离线。

// einoScriptStep 替身模型的一步：先按顺序发流事件，再返回聚合结论。
type einoScriptStep struct {
	events   []aidomain.StreamEvent
	response aidomain.ChatResponse
}

// newEinoTestSession 搭一个可跑的会话现场：脚本模型 + 注入的工具表。
// 返回的 chunks 收集所有发给「界面」的 UI chunk（序列化后按序存放）。
func newEinoTestSession(t *testing.T, steps []einoScriptStep,
	tools []aiAgentTool) (*AIAgentService, *einoAgentSession, *[]map[string]any, *[][]aidomain.ChatMessage) {
	t.Helper()

	service := &AIAgentService{log: zap.NewNop()}

	var chunks []map[string]any
	emit := func(chunk any) error {
		encoded, err := json.Marshal(chunk)
		if err != nil {
			return err
		}
		parsed := map[string]any{}
		if err := json.Unmarshal(encoded, &parsed); err != nil {
			return err
		}
		chunks = append(chunks, parsed)
		return nil
	}

	toolIndex := make(map[string]aiAgentTool, len(tools))
	toolList := make([]aidomain.Tool, 0, len(tools))
	for _, item := range tools {
		toolIndex[item.Name] = item
		toolList = append(toolList, aidomain.Tool{
			Name: item.Name, Description: item.Description, InputSchema: item.InputSchema,
		})
	}

	// transcripts 记录每一步喂给「模型」的转录，供断言消息累积形状。
	var transcripts [][]aidomain.ChatMessage
	step := 0
	session := &einoAgentSession{
		service:   service,
		input:     AIAgentRunInput{AppID: 1, AdminID: 1, Model: "test-model"},
		run:       &aiAgentRun{AppID: 1, AdminID: 1},
		toolIndex: toolIndex,
		toolList:  toolList,
		budget:    aiAgentDefaultContextChars,
		emit:      emit,
		chatStream: func(_ context.Context, args aiChatArgs,
			onEvent func(aidomain.StreamEvent) error) (*aidomain.ChatResponse, *resolvedChannel, error) {
			transcripts = append(transcripts, args.Request.Messages)
			if step >= len(steps) {
				return nil, nil, fmt.Errorf("脚本只有 %d 步，第 %d 步不该发生", len(steps), step+1)
			}
			current := steps[step]
			step++
			for _, event := range current.events {
				if err := onEvent(event); err != nil {
					return nil, nil, err
				}
			}
			response := current.response
			return &response, nil, nil
		},
	}
	return service, session, &chunks, &transcripts
}

func chunkTypes(chunks []map[string]any) []string {
	types := make([]string, 0, len(chunks))
	for _, chunk := range chunks {
		kind, _ := chunk["type"].(string)
		types = append(types, kind)
	}
	return types
}

// TestEinoAgentToolRoundTrip 一次完整的「模型 → 工具 → 模型」回合：
// 界面事件序列、落库分片、用量累计、回喂转录的形状都要对。
func TestEinoAgentToolRoundTrip(t *testing.T) {
	echoTool := aiAgentTool{
		Name:        "echo",
		Description: "回显入参",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}}}`),
		Execute: func(_ context.Context, _ *aiAgentRun, args json.RawMessage) (any, error) {
			var input struct {
				Value string `json:"value"`
			}
			_ = json.Unmarshal(args, &input)
			return map[string]any{"echoed": input.Value}, nil
		},
	}
	steps := []einoScriptStep{
		{
			events: []aidomain.StreamEvent{
				{Type: aidomain.StreamText, Delta: "先调用工具"},
				{Type: aidomain.StreamToolStart, ToolID: "call-1", ToolName: "echo"},
			},
			response: aidomain.ChatResponse{
				Text: "先调用工具",
				ToolCalls: []aidomain.ToolCall{
					{ID: "call-1", Name: "echo", Arguments: json.RawMessage(`{"value":"hi"}`)},
				},
				Usage: aidomain.Usage{InputTokens: 10, OutputTokens: 5, TotalTokens: 15},
			},
		},
		{
			events: []aidomain.StreamEvent{
				{Type: aidomain.StreamReasoning, Delta: "结果拿到了"},
				{Type: aidomain.StreamText, Delta: "完成"},
			},
			response: aidomain.ChatResponse{
				Text: "完成", Reasoning: "结果拿到了",
				Usage: aidomain.Usage{InputTokens: 20, OutputTokens: 6, TotalTokens: 26},
			},
		},
	}
	service, session, chunks, transcripts := newEinoTestSession(t, steps, []aiAgentTool{echoTool})

	initial := chatToEinoMessages("系统提示词", []aidomain.ChatMessage{
		aidomain.TextMessage(aidomain.RoleUser, "试一下工具"),
	})
	if err := service.runEinoAgent(context.Background(), session, initial); err != nil {
		t.Fatalf("runEinoAgent 失败：%v", err)
	}

	wantTypes := []string{
		"start-step",
		"text-start", "text-delta", "text-end",
		"tool-input-start", "tool-input-available",
		"tool-output-available",
		"finish-step", "start-step",
		"reasoning-start", "reasoning-delta", "reasoning-end",
		"text-start", "text-delta", "text-end",
		"finish-step",
	}
	gotTypes := chunkTypes(*chunks)
	if strings.Join(gotTypes, ",") != strings.Join(wantTypes, ",") {
		t.Fatalf("UI 事件序列不对：\n想要 %v\n得到 %v", wantTypes, gotTypes)
	}

	// 第二步喂给模型的转录必须是「…user, assistant(toolCalls), tool(结果)」。
	if len(*transcripts) != 2 {
		t.Fatalf("想要 2 次模型调用，得到 %d", len(*transcripts))
	}
	second := (*transcripts)[1]
	if len(second) < 3 {
		t.Fatalf("第二步转录太短：%d 条", len(second))
	}
	assistant := second[len(second)-2]
	if assistant.Role != aidomain.RoleAssistant || len(assistant.ToolCalls) != 1 ||
		assistant.ToolCalls[0].Name != "echo" {
		t.Fatalf("倒数第二条应是带工具调用的 assistant 消息：%+v", assistant)
	}
	toolMsg := second[len(second)-1]
	if toolMsg.Role != aidomain.RoleTool || toolMsg.ToolCallID != "call-1" ||
		!strings.Contains(toolMsg.PlainText(), `"echoed":"hi"`) {
		t.Fatalf("末条应是 call-1 的工具结果：%+v", toolMsg)
	}

	parts, usage := session.snapshot()
	if usage.TotalTokens != 41 {
		t.Fatalf("用量应累计两步（41），得到 %d", usage.TotalTokens)
	}
	var kinds []string
	for _, part := range parts {
		kinds = append(kinds, part.Type)
	}
	wantKinds := "step-start,text,dynamic-tool,step-start,reasoning,text"
	if strings.Join(kinds, ",") != wantKinds {
		t.Fatalf("落库分片顺序不对：想要 %s，得到 %s", wantKinds, strings.Join(kinds, ","))
	}
}

// TestEinoAgentToolErrorFeedsBack 工具业务失败不允许中断整轮：
// 错误进界面（output-error）也回喂模型（「错误：」前缀），下一步照常进行。
func TestEinoAgentToolErrorFeedsBack(t *testing.T) {
	failTool := aiAgentTool{
		Name:        "boom",
		Description: "总是失败",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		Execute: func(_ context.Context, _ *aiAgentRun, _ json.RawMessage) (any, error) {
			return nil, fmt.Errorf("函数版本不存在")
		},
	}
	steps := []einoScriptStep{
		{
			response: aidomain.ChatResponse{
				ToolCalls: []aidomain.ToolCall{
					{ID: "call-boom", Name: "boom", Arguments: json.RawMessage(`{}`)},
				},
			},
		},
		{response: aidomain.ChatResponse{Text: "我换个方式"}},
	}
	service, session, chunks, transcripts := newEinoTestSession(t, steps, []aiAgentTool{failTool})

	initial := chatToEinoMessages("s", []aidomain.ChatMessage{
		aidomain.TextMessage(aidomain.RoleUser, "跑一下"),
	})
	if err := service.runEinoAgent(context.Background(), session, initial); err != nil {
		t.Fatalf("工具业务失败不该让整轮失败：%v", err)
	}

	var sawError bool
	for _, chunk := range *chunks {
		if chunk["type"] == "tool-output-error" {
			sawError = true
			if text, _ := chunk["errorText"].(string); !strings.Contains(text, "函数版本不存在") {
				t.Fatalf("errorText 应带业务错误原文：%v", chunk)
			}
		}
	}
	if !sawError {
		t.Fatal("没看到 tool-output-error 事件")
	}

	second := (*transcripts)[1]
	toolMsg := second[len(second)-1]
	if !strings.HasPrefix(toolMsg.PlainText(), "错误：") {
		t.Fatalf("回喂模型的工具结果应带「错误：」前缀：%q", toolMsg.PlainText())
	}
}

// TestEinoAgentUnknownTool 模型幻觉出的工具名走 UnknownToolsHandler：
// 不炸整轮，错误文本回喂模型。
func TestEinoAgentUnknownTool(t *testing.T) {
	known := aiAgentTool{
		Name: "known", Description: "存在的工具",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		Execute: func(_ context.Context, _ *aiAgentRun, _ json.RawMessage) (any, error) {
			return "ok", nil
		},
	}
	steps := []einoScriptStep{
		{
			response: aidomain.ChatResponse{
				ToolCalls: []aidomain.ToolCall{
					{ID: "call-x", Name: "made_up_tool", Arguments: json.RawMessage(`{"a":1}`)},
				},
			},
		},
		{response: aidomain.ChatResponse{Text: "好的，不用那个工具了"}},
	}
	service, session, _, transcripts := newEinoTestSession(t, steps, []aiAgentTool{known})

	initial := chatToEinoMessages("s", []aidomain.ChatMessage{
		aidomain.TextMessage(aidomain.RoleUser, "试试"),
	})
	if err := service.runEinoAgent(context.Background(), session, initial); err != nil {
		t.Fatalf("未知工具不该让整轮失败：%v", err)
	}
	second := (*transcripts)[1]
	toolMsg := second[len(second)-1]
	if !strings.Contains(toolMsg.PlainText(), "未知工具：made_up_tool") {
		t.Fatalf("应回喂「未知工具」提示：%q", toolMsg.PlainText())
	}
}

// TestEinoAgentMaxSteps 模型一直要求调工具时：到步数上限后不算失败，
// 界面收到提示语，最后一步的工具结果照常落分片。
func TestEinoAgentMaxSteps(t *testing.T) {
	counter := 0
	loopTool := aiAgentTool{
		Name: "again", Description: "循环工具",
		InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		Execute: func(_ context.Context, _ *aiAgentRun, _ json.RawMessage) (any, error) {
			counter++
			return counter, nil
		},
	}
	steps := make([]einoScriptStep, aiAgentMaxSteps)
	for i := range steps {
		steps[i] = einoScriptStep{
			response: aidomain.ChatResponse{
				ToolCalls: []aidomain.ToolCall{
					{ID: fmt.Sprintf("call-%d", i), Name: "again", Arguments: json.RawMessage(`{}`)},
				},
			},
		}
	}
	service, session, chunks, transcripts := newEinoTestSession(t, steps, []aiAgentTool{loopTool})

	initial := chatToEinoMessages("s", []aidomain.ChatMessage{
		aidomain.TextMessage(aidomain.RoleUser, "一直跑"),
	})
	if err := service.runEinoAgent(context.Background(), session, initial); err != nil {
		t.Fatalf("触达步数上限应按成功收尾：%v", err)
	}
	if len(*transcripts) != aiAgentMaxSteps {
		t.Fatalf("模型调用应正好 %d 次，得到 %d", aiAgentMaxSteps, len(*transcripts))
	}
	if counter != aiAgentMaxSteps {
		t.Fatalf("工具应执行 %d 次，得到 %d", aiAgentMaxSteps, counter)
	}
	var sawNote bool
	for _, chunk := range *chunks {
		if chunk["type"] == "text-delta" {
			if delta, _ := chunk["delta"].(string); strings.Contains(delta, "已达到单轮最大步数") {
				sawNote = true
			}
		}
	}
	if !sawNote {
		t.Fatal("没看到步数上限提示语")
	}
}

// TestEinoMessageRoundTrip 统一转录 ⇄ Eino 消息的双向转换要保住
// 系统提示词、工具调用与工具结果。
func TestEinoMessageRoundTrip(t *testing.T) {
	transcript := []aidomain.ChatMessage{
		aidomain.TextMessage(aidomain.RoleUser, "你好"),
		{
			Role:    aidomain.RoleAssistant,
			Content: []aidomain.ContentPart{{Type: aidomain.PartText, Text: "我查一下"}},
			ToolCalls: []aidomain.ToolCall{
				{ID: "c1", Name: "lookup", Arguments: json.RawMessage(`{"q":"x"}`)},
			},
		},
		toolResultMessage("c1", `{"hit":true}`),
	}
	messages := chatToEinoMessages("系统", transcript)
	if len(messages) != 4 {
		t.Fatalf("想要 4 条（含系统），得到 %d", len(messages))
	}

	system, back := einoMessagesToChat(messages)
	if system != "系统" {
		t.Fatalf("系统提示词丢了：%q", system)
	}
	if len(back) != 3 {
		t.Fatalf("想要 3 条转录，得到 %d", len(back))
	}
	if back[1].ToolCalls[0].Name != "lookup" || string(back[1].ToolCalls[0].Arguments) != `{"q":"x"}` {
		t.Fatalf("工具调用转换丢字段：%+v", back[1].ToolCalls)
	}
	if back[2].Role != aidomain.RoleTool || back[2].ToolCallID != "c1" {
		t.Fatalf("工具结果转换不对：%+v", back[2])
	}
}

// TestTrimEinoMessages 超预算时只掐最旧的工具结果，最近 4 条保持完整。
func TestTrimEinoMessages(t *testing.T) {
	long := strings.Repeat("A", 500)
	transcript := []aidomain.ChatMessage{aidomain.TextMessage(aidomain.RoleUser, "开始")}
	for i := 0; i < 6; i++ {
		transcript = append(transcript, aidomain.ChatMessage{
			Role: aidomain.RoleAssistant,
			ToolCalls: []aidomain.ToolCall{
				{ID: fmt.Sprintf("c%d", i), Name: "t", Arguments: json.RawMessage(`{}`)},
			},
		})
		transcript = append(transcript, toolResultMessage(fmt.Sprintf("c%d", i), long))
	}
	messages := chatToEinoMessages("s", transcript)

	// 预算充足：原样返回。
	untouched := trimEinoMessages(messages, 1<<20)
	for _, message := range untouched {
		if strings.Contains(message.Content, "结果已省略") {
			t.Fatal("预算内不该裁剪")
		}
	}

	trimmed := trimEinoMessages(messages, 100)
	var intact, replaced int
	for _, message := range trimmed {
		if message.Role != "tool" {
			continue
		}
		if strings.Contains(message.Content, "结果已省略") {
			replaced++
		} else {
			intact++
		}
	}
	if intact != 4 || replaced != 2 {
		t.Fatalf("想要保 4 掐 2，得到保 %d 掐 %d", intact, replaced)
	}
}

// TestEinoParams JSON Schema 解析成功给出参数声明，失败退化为 nil。
func TestEinoParams(t *testing.T) {
	if einoParams(json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}}}`)) == nil {
		t.Fatal("合法 schema 不该返回 nil")
	}
	if einoParams(nil) != nil {
		t.Fatal("空 schema 应返回 nil")
	}
	if einoParams(json.RawMessage(`{not json`)) != nil {
		t.Fatal("坏 schema 应退化为 nil")
	}
}
