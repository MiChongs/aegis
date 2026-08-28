package service

// Gemini 原生协议适配器的翻译契约：地址归一（与 OpenAI 侧相反，要剥版本段）、
// 统一转录 → contents 的角色/工具映射、配置构建、分片聚合与事件发射。

import (
	"encoding/json"
	"strings"
	"testing"

	aidomain "aegis/internal/domain/ai"

	"google.golang.org/genai"
)

func geminiConfigWith(base string) aidomain.Config {
	return aidomain.Config{
		Provider: aidomain.ProviderGemini,
		Settings: map[string]string{aidomain.KeyBaseURL: base},
	}
}

func TestGeminiBaseURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"留空走 SDK 官方缺省", "", ""},
		{"官方地址带版本段 → 剥掉", "https://generativelanguage.googleapis.com/v1beta", "https://generativelanguage.googleapis.com"},
		{"老配置的 OpenAI 兼容端点 → 剥 /openai 与版本段", "https://generativelanguage.googleapis.com/v1beta/openai", "https://generativelanguage.googleapis.com"},
		{"代理根地址原样", "https://gemini-proxy.example.com", "https://gemini-proxy.example.com"},
		{"无 scheme 自动补 https", "gemini-proxy.example.com", "https://gemini-proxy.example.com"},
		{"带挂载前缀的代理保留前缀", "https://gw.internal/gemini", "https://gw.internal/gemini"},
		{"挂载前缀 + 版本段 → 只剥版本段", "https://gw.internal/gemini/v1beta", "https://gw.internal/gemini"},
	}
	for _, tc := range cases {
		if got := geminiBaseURL(geminiConfigWith(tc.in)); got != tc.want {
			t.Errorf("%s：geminiBaseURL(%q) = %q，想要 %q", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestBuildGeminiContents(t *testing.T) {
	request := aidomain.ChatRequest{
		Messages: []aidomain.ChatMessage{
			aidomain.TextMessage(aidomain.RoleUser, "帮我查天气"),
			{
				Role:    aidomain.RoleAssistant,
				Content: []aidomain.ContentPart{{Type: aidomain.PartText, Text: "我来查一下。"}},
				ToolCalls: []aidomain.ToolCall{
					{ID: "gemini_call_1", Name: "get_weather", Arguments: json.RawMessage(`{"city":"北京"}`)},
					{ID: "real-id-2", Name: "get_time", Arguments: json.RawMessage(`{}`)},
				},
			},
			toolResultMessage("gemini_call_1", `{"temp":21}`),
			toolResultMessage("real-id-2", "早上八点"),
		},
	}
	contents := buildGeminiContents(request)
	if len(contents) != 3 {
		t.Fatalf("应合并成 user / model / user 三条，实际 %d 条", len(contents))
	}

	assistant := contents[1]
	if assistant.Role != genai.RoleModel {
		t.Fatalf("助手消息角色应为 model：%s", assistant.Role)
	}
	if len(assistant.Parts) != 3 {
		t.Fatalf("助手消息应有正文 + 两个函数调用，实际 %d 个分片", len(assistant.Parts))
	}
	first := assistant.Parts[1].FunctionCall
	if first == nil || first.Name != "get_weather" {
		t.Fatalf("第一个函数调用不符：%+v", assistant.Parts[1])
	}
	if first.ID != "" {
		t.Fatalf("合成 ID 不该回传给 Gemini：%q", first.ID)
	}
	second := assistant.Parts[2].FunctionCall
	if second == nil || second.ID != "real-id-2" {
		t.Fatalf("真实 ID 应原样带回：%+v", second)
	}

	results := contents[2]
	if results.Role != genai.RoleUser {
		t.Fatalf("工具结果角色应为 user：%s", results.Role)
	}
	if len(results.Parts) != 2 {
		t.Fatalf("两条工具结果应合并进一条 user 消息，实际 %d 个分片", len(results.Parts))
	}
	weather := results.Parts[0].FunctionResponse
	if weather == nil || weather.Name != "get_weather" {
		t.Fatalf("工具结果应按 ID 找回函数名：%+v", weather)
	}
	if weather.Response["temp"] != float64(21) {
		t.Fatalf("JSON 对象结果应原样成为 response：%+v", weather.Response)
	}
	clock := results.Parts[1].FunctionResponse
	if clock == nil || clock.ID != "real-id-2" {
		t.Fatalf("真实 ID 的结果应带回 ID：%+v", clock)
	}
	if clock.Response["result"] != "早上八点" {
		t.Fatalf("纯文本结果应包成 {result:…}：%+v", clock.Response)
	}
}

func TestBuildGeminiConfig(t *testing.T) {
	temperature := 0.3
	request := aidomain.ChatRequest{
		System:      "你是助手",
		MaxTokens:   2048,
		Temperature: &temperature,
		JSONMode:    true,
		Tools: []aidomain.Tool{{
			Name: "get_weather", Description: "查天气",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}`),
		}},
		ToolChoice: aidomain.ToolChoiceRequired,
	}
	config := buildGeminiConfig(request)

	if config.SystemInstruction == nil || config.SystemInstruction.Parts[0].Text != "你是助手" {
		t.Fatalf("系统提示词未落位：%+v", config.SystemInstruction)
	}
	if config.MaxOutputTokens != 2048 {
		t.Fatalf("MaxOutputTokens 不符：%d", config.MaxOutputTokens)
	}
	if config.ResponseMIMEType != "application/json" {
		t.Fatalf("JSON 模式应设置 responseMimeType：%q", config.ResponseMIMEType)
	}
	if len(config.Tools) != 1 || len(config.Tools[0].FunctionDeclarations) != 1 {
		t.Fatalf("工具声明缺失：%+v", config.Tools)
	}
	declaration := config.Tools[0].FunctionDeclarations[0]
	schema, ok := declaration.ParametersJsonSchema.(map[string]any)
	if !ok || schema["required"] == nil {
		t.Fatalf("原始 JSON Schema 应透传 ParametersJsonSchema：%+v", declaration.ParametersJsonSchema)
	}
	if config.ToolConfig == nil || config.ToolConfig.FunctionCallingConfig.Mode != genai.FunctionCallingConfigModeAny {
		t.Fatalf("ToolChoiceRequired 应映射为 ANY：%+v", config.ToolConfig)
	}
}

func TestAggregateGeminiChunk(t *testing.T) {
	result := &aidomain.ChatResponse{FinishReason: aidomain.FinishOther}
	var texts, reasons []string
	var calls []aidomain.ToolCall

	aggregateGeminiChunk(result, &genai.GenerateContentResponse{
		ResponseID: "resp-1", ModelVersion: "gemini-2.5-flash",
		Candidates: []*genai.Candidate{{
			Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{
				{Text: "思考中…", Thought: true},
				{Text: "答案是"},
			}},
		}},
	}, func(delta string) { texts = append(texts, delta) },
		func(delta string) { reasons = append(reasons, delta) },
		func(_ int, call aidomain.ToolCall) { calls = append(calls, call) })

	aggregateGeminiChunk(result, &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{
			FinishReason: genai.FinishReasonStop,
			Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{
				{FunctionCall: &genai.FunctionCall{Name: "get_weather", Args: map[string]any{"city": "北京"}}},
			}},
		}},
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount: 100, CandidatesTokenCount: 40, ThoughtsTokenCount: 10, TotalTokenCount: 150,
		},
	}, func(delta string) { texts = append(texts, delta) },
		func(delta string) { reasons = append(reasons, delta) },
		func(_ int, call aidomain.ToolCall) { calls = append(calls, call) })

	if result.Text != "答案是" || result.Reasoning != "思考中…" {
		t.Fatalf("正文/思考聚合不符：text=%q reasoning=%q", result.Text, result.Reasoning)
	}
	if len(texts) != 1 || len(reasons) != 1 {
		t.Fatalf("增量事件次数不符：texts=%d reasons=%d", len(texts), len(reasons))
	}
	if len(calls) != 1 || calls[0].Name != "get_weather" {
		t.Fatalf("函数调用事件不符：%+v", calls)
	}
	if !strings.HasPrefix(calls[0].ID, geminiSyntheticIDPrefix) {
		t.Fatalf("无 ID 的调用应合成带前缀的 ID：%q", calls[0].ID)
	}
	if string(calls[0].Arguments) != `{"city":"北京"}` {
		t.Fatalf("入参应整体 JSON 化：%s", calls[0].Arguments)
	}
	if result.FinishReason != aidomain.FinishToolCalls {
		t.Fatalf("带函数调用收尾应归一为 tool_calls：%s", result.FinishReason)
	}
	if result.Usage.OutputTokens != 50 || result.Usage.TotalTokens != 150 {
		t.Fatalf("用量应含思考 token：%+v", result.Usage)
	}
}
