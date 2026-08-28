package service

// 新增 Agent 编码工具的行为契约：精确编辑（edit_draft）、带行号读取（read_draft）、
// 任务计划（update_plan），以及双通道结果信封在执行入口与历史回喂上的分流 ——
// 模型只该拿省流版，整篇脚本只走界面/落库通道。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	aidomain "aegis/internal/domain/ai"
)

// findAgentTool 按名取内置工具，找不到直接失败。
func findAgentTool(t *testing.T, name string) aiAgentTool {
	t.Helper()
	for _, tool := range aiFunctionTools() {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("内置工具不存在：%s", name)
	return aiAgentTool{}
}

func TestEditDraftReplacesUniqueMatch(t *testing.T) {
	tool := findAgentTool(t, "edit_draft")
	// 首行是远离编辑点的标记：snippet 只该带 ±3 行上下文，不该扫到它。
	draft := "// FAR-AWAY-MARKER\n" + strings.Repeat("// filler\n", 10) +
		"function main() {\n  const a = 1;\n  return a;\n}\n"
	run := &aiAgentRun{DraftSource: draft}

	result, err := tool.Execute(context.Background(), run, json.RawMessage(
		`{"oldText":"const a = 1;","newText":"const a = 2;","note":"改初值"}`))
	if err != nil {
		t.Fatalf("edit_draft 执行失败：%v", err)
	}
	if !strings.Contains(run.DraftSource, "const a = 2;") {
		t.Fatalf("草稿未被替换：%s", run.DraftSource)
	}
	if run.StagedSource != run.DraftSource {
		t.Fatalf("StagedSource 应与草稿同步（前端编辑器靠它更新）")
	}

	rich, ok := result.(aiToolRichResult)
	if !ok {
		t.Fatalf("edit_draft 应返回双通道信封，实际 %T", result)
	}
	model, _ := json.Marshal(rich.Model)
	if strings.Contains(string(model), "FAR-AWAY-MARKER") {
		t.Fatalf("模型通道只该带编辑处上下文，不该携带整篇脚本：%s", model)
	}
	if !strings.Contains(string(model), "const a = 2;") {
		t.Fatalf("snippet 应包含编辑后的行：%s", model)
	}
	ui, _ := json.Marshal(rich.UI)
	if !strings.Contains(string(ui), "FAR-AWAY-MARKER") || !strings.Contains(string(ui), "const a = 2;") {
		t.Fatalf("界面通道应携带完整更新后脚本：%s", ui)
	}
}

func TestEditDraftRejectsAmbiguousAndMissing(t *testing.T) {
	tool := findAgentTool(t, "edit_draft")

	run := &aiAgentRun{DraftSource: "let x = 1;\nlet x = 1;\n"}
	if _, err := tool.Execute(context.Background(), run,
		json.RawMessage(`{"oldText":"let x = 1;","newText":"let x = 2;"}`)); err == nil {
		t.Fatalf("多处匹配且未传 replaceAll 应报错")
	} else if !strings.Contains(err.Error(), "2 次") {
		t.Fatalf("报错应点明出现次数：%v", err)
	}

	if _, err := tool.Execute(context.Background(), run,
		json.RawMessage(`{"oldText":"不存在的行","newText":"x"}`)); err == nil {
		t.Fatalf("找不到 oldText 应报错")
	}

	empty := &aiAgentRun{}
	if _, err := tool.Execute(context.Background(), empty,
		json.RawMessage(`{"oldText":"a","newText":"b"}`)); err == nil {
		t.Fatalf("空草稿应报错并指路 stage_source")
	}
}

func TestEditDraftReplaceAll(t *testing.T) {
	tool := findAgentTool(t, "edit_draft")
	run := &aiAgentRun{DraftSource: "log('a');\nlog('a');\nlog('b');\n"}

	result, err := tool.Execute(context.Background(), run,
		json.RawMessage(`{"oldText":"log('a');","newText":"log('c');","replaceAll":true}`))
	if err != nil {
		t.Fatalf("replaceAll 执行失败：%v", err)
	}
	if strings.Contains(run.DraftSource, "log('a');") {
		t.Fatalf("replaceAll 后不应残留旧文本：%s", run.DraftSource)
	}
	rich := result.(aiToolRichResult)
	model, _ := json.Marshal(rich.Model)
	if !strings.Contains(string(model), `"replacements":2`) {
		t.Fatalf("应报告替换了 2 处：%s", model)
	}
}

func TestReadDraftPagination(t *testing.T) {
	tool := findAgentTool(t, "read_draft")
	lines := make([]string, 0, 10)
	for i := 1; i <= 10; i++ {
		lines = append(lines, strings.Repeat("x", i))
	}
	run := &aiAgentRun{DraftSource: strings.Join(lines, "\n")}

	result, err := tool.Execute(context.Background(), run, json.RawMessage(`{"offset":3,"limit":2}`))
	if err != nil {
		t.Fatalf("read_draft 执行失败：%v", err)
	}
	encoded, _ := json.Marshal(result)
	text := string(encoded)
	if !strings.Contains(text, `"totalLines":10`) {
		t.Fatalf("应报告总行数：%s", text)
	}
	if !strings.Contains(text, "3| xxx") || !strings.Contains(text, "4| xxxx") {
		t.Fatalf("应带行号返回第 3、4 行：%s", text)
	}
	if strings.Contains(text, "5| xxxxx") {
		t.Fatalf("limit=2 不应包含第 5 行：%s", text)
	}

	if _, err := tool.Execute(context.Background(), run, json.RawMessage(`{"offset":99}`)); err == nil {
		t.Fatalf("offset 越界应报错")
	}
	if _, err := tool.Execute(context.Background(), &aiAgentRun{}, json.RawMessage(`{}`)); err == nil {
		t.Fatalf("空草稿应报错")
	}
}

func TestUpdatePlanValidatesAndSplitsChannels(t *testing.T) {
	tool := findAgentTool(t, "update_plan")
	run := &aiAgentRun{}

	result, err := tool.Execute(context.Background(), run, json.RawMessage(
		`{"items":[{"step":"读现有代码","status":"done"},{"step":"改校验逻辑","status":"active"},{"step":"试跑验证"}]}`))
	if err != nil {
		t.Fatalf("update_plan 执行失败：%v", err)
	}
	rich, ok := result.(aiToolRichResult)
	if !ok {
		t.Fatalf("update_plan 应返回双通道信封，实际 %T", result)
	}
	model, _ := json.Marshal(rich.Model)
	if !strings.Contains(string(model), `"steps":3`) || !strings.Contains(string(model), `"done":1`) {
		t.Fatalf("模型通道应只带统计：%s", model)
	}
	ui, _ := json.Marshal(rich.UI)
	if !strings.Contains(string(ui), "改校验逻辑") || !strings.Contains(string(ui), `"status":"active"`) {
		t.Fatalf("界面通道应带完整清单：%s", ui)
	}
	// 未知状态归一化为 pending，缺省亦然。
	if !strings.Contains(string(ui), `"status":"pending"`) {
		t.Fatalf("缺省状态应归一化为 pending：%s", ui)
	}

	if _, err := tool.Execute(context.Background(), run, json.RawMessage(`{"items":[]}`)); err == nil {
		t.Fatalf("空计划应报错")
	}
	if _, err := tool.Execute(context.Background(), run, json.RawMessage(`{"items":[{"step":"  "}]}`)); err == nil {
		t.Fatalf("空白 step 应报错")
	}
}

func TestExecuteAgentToolSplitsRichResult(t *testing.T) {
	tools := map[string]aiAgentTool{
		"rich": {
			Name: "rich",
			Execute: func(context.Context, *aiAgentRun, json.RawMessage) (any, error) {
				return aiToolRichResult{
					Model: map[string]any{"ok": true},
					UI:    map[string]any{"source": "整篇脚本"},
				}, nil
			},
		},
		"plain": {
			Name: "plain",
			Execute: func(context.Context, *aiAgentRun, json.RawMessage) (any, error) {
				return map[string]any{"value": 42}, nil
			},
		},
	}

	model, ui, err := executeAgentTool(context.Background(), &aiAgentRun{}, tools, "rich", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("rich 工具执行失败：%v", err)
	}
	if strings.Contains(model, "整篇脚本") {
		t.Fatalf("模型输出不应包含界面通道内容：%s", model)
	}
	if !strings.Contains(string(ui), "整篇脚本") {
		t.Fatalf("界面输出应为全量版本：%s", ui)
	}

	model, ui, err = executeAgentTool(context.Background(), &aiAgentRun{}, tools, "plain", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("plain 工具执行失败：%v", err)
	}
	if ui != nil {
		t.Fatalf("普通工具的界面输出应为空（两通道同一份）：%s", ui)
	}
	if !strings.Contains(model, "42") {
		t.Fatalf("普通工具输出不符：%s", model)
	}
}

func TestAgentMessageToChatPrefersModelOutput(t *testing.T) {
	parts, _ := json.Marshal([]agentUIPart{{
		Type: "dynamic-tool", ToolCallID: "call-1", ToolName: "edit_draft",
		State:       "output-available",
		Input:       json.RawMessage(`{"oldText":"a","newText":"b"}`),
		Output:      json.RawMessage(`{"source":"整篇脚本不该回喂"}`),
		ModelOutput: json.RawMessage(`{"ok":true,"replacements":1}`),
	}})
	messages := agentMessageToChat(aidomain.AgentMessage{Role: aidomain.RoleAssistant, Parts: parts})

	var toolResult string
	for _, message := range messages {
		if message.ToolCallID == "call-1" {
			toolResult = message.PlainText()
		}
	}
	if toolResult == "" {
		t.Fatalf("应生成工具结果消息")
	}
	if strings.Contains(toolResult, "整篇脚本不该回喂") {
		t.Fatalf("历史回喂应使用省流版 ModelOutput：%s", toolResult)
	}
	if !strings.Contains(toolResult, `"replacements":1`) {
		t.Fatalf("历史回喂内容不符：%s", toolResult)
	}
}
