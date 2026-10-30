package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	aidomain "aegis/internal/domain/ai"

	"google.golang.org/genai"
)

// 流的完整性、工具调用归组、上游空闲看门狗：用假上游（httptest）走真实的 SDK 解码路径。

func sseServer(t *testing.T, handler func(w http.ResponseWriter, flush func())) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher := w.(http.Flusher)
		handler(w, flusher.Flush)
	}))
	t.Cleanup(server.Close)
	return server
}

func streamConfig(provider, base string) aidomain.Config {
	return aidomain.Config{
		Provider: provider,
		Settings: map[string]string{aidomain.KeyBaseURL: base},
		Secrets:  map[string]string{aidomain.KeyAPIKey: "k"},
	}
}

func collectStream(t *testing.T, client *aiLLMClient, cfg aidomain.Config) ([]aidomain.StreamEvent, *aidomain.ChatResponse, error) {
	t.Helper()
	var events []aidomain.StreamEvent
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	response, err := client.ChatStream(ctx, cfg, aidomain.ChatRequest{
		Model:    "m",
		Messages: []aidomain.ChatMessage{{Role: aidomain.RoleUser, Content: []aidomain.ContentPart{{Type: aidomain.PartText, Text: "hi"}}}},
	}, func(event aidomain.StreamEvent) error {
		events = append(events, event)
		return nil
	})
	return events, response, err
}

func openAIChunk(body string) string {
	return `data: {"id":"c","object":"chat.completion.chunk","model":"m","choices":[` + body + "]}\n\n"
}

// TestOpenAIStreamWithoutFinishIsAnError 没有 finish_reason 也没有 [DONE]：流被半路切断，按上游失败处理。
func TestOpenAIStreamWithoutFinishIsAnError(t *testing.T) {
	server := sseServer(t, func(w http.ResponseWriter, flush func()) {
		_, _ = w.Write([]byte(openAIChunk(`{"index":0,"delta":{"content":"Hel"},"finish_reason":null}`)))
		flush()
	})
	_, response, err := collectStream(t, newAILLMClient(nil), streamConfig(aidomain.ProviderCustomOpenAI, server.URL))
	var upstream *aiUpstreamError
	if err == nil || !errors.As(err, &upstream) || !strings.Contains(upstream.Message, "结束原因") {
		t.Fatalf("截断的流应报上游错误，得到 %+v %v", response, err)
	}
}

// TestOpenAIStreamDoneWithoutFinishIsComplete 发了 [DONE] 却从不带 finish_reason 的兼容实现按正常结束。
func TestOpenAIStreamDoneWithoutFinishIsComplete(t *testing.T) {
	server := sseServer(t, func(w http.ResponseWriter, flush func()) {
		_, _ = w.Write([]byte(openAIChunk(`{"index":0,"delta":{"content":"Hello"},"finish_reason":null}`)))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	})
	_, response, err := collectStream(t, newAILLMClient(nil), streamConfig(aidomain.ProviderCustomOpenAI, server.URL))
	if err != nil || response.FinishReason != aidomain.FinishStop || response.Text != "Hello" {
		t.Fatalf("有 [DONE] 的流应正常结束：%+v %v", response, err)
	}
}

// TestOpenAIStreamUnknownFinishKeepsRaw 认不出的结束原因原样留下，不冒充 stop。
func TestOpenAIStreamUnknownFinishKeepsRaw(t *testing.T) {
	server := sseServer(t, func(w http.ResponseWriter, flush func()) {
		_, _ = w.Write([]byte(openAIChunk(`{"index":0,"delta":{"content":"x"},"finish_reason":"insufficient_system_resource"}`)))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	})
	_, response, err := collectStream(t, newAILLMClient(nil), streamConfig(aidomain.ProviderCustomOpenAI, server.URL))
	if err != nil || response.FinishReason != aidomain.FinishOther || response.FinishRaw != "insufficient_system_resource" {
		t.Fatalf("应保留原样的结束原因：%+v %v", response, err)
	}
}

// TestOpenAIStreamToolCallGrouping 工具调用按 index 归组：入参先于名字到达时攒着补发；
// 上游不给 id 时合成一个，流里与最终结论同一个；同一 index 换了 id 是新的一次调用。
func TestOpenAIStreamToolCallGrouping(t *testing.T) {
	server := sseServer(t, func(w http.ResponseWriter, flush func()) {
		for _, body := range []string{
			// 第一次调用：入参先到、名字后到、没有 id
			`{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\""}}]}}`,
			`{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"name":"weather","arguments":":\"sh\"}"}}]}}`,
			// 第二、三次调用：都标 index 1、靠 id 区分
			`{"index":0,"delta":{"tool_calls":[{"index":1,"id":"A","function":{"name":"time","arguments":"{}"}}]}}`,
			`{"index":0,"delta":{"tool_calls":[{"index":1,"id":"B","function":{"name":"date","arguments":"{\"d\":1}"}}]}}`,
			`{"index":0,"delta":{},"finish_reason":"tool_calls"}`,
		} {
			_, _ = w.Write([]byte(openAIChunk(body)))
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	})
	events, response, err := collectStream(t, newAILLMClient(nil), streamConfig(aidomain.ProviderCustomOpenAI, server.URL))
	if err != nil {
		t.Fatal(err)
	}
	if response.FinishReason != aidomain.FinishToolCalls || len(response.ToolCalls) != 3 {
		t.Fatalf("应聚合出三次工具调用：%+v", response)
	}
	type streamed struct {
		id, name, args string
		started        bool
	}
	bySlot := map[int]*streamed{}
	for _, event := range events {
		switch event.Type {
		case aidomain.StreamToolStart:
			if bySlot[event.ToolIndex] != nil {
				t.Fatalf("同一序号宣布了两次：%+v", event)
			}
			bySlot[event.ToolIndex] = &streamed{id: event.ToolID, name: event.ToolName, started: true}
		case aidomain.StreamToolDelta:
			slot := bySlot[event.ToolIndex]
			if slot == nil {
				t.Fatalf("入参增量先于 tool-start：%+v", event)
			}
			if event.ToolID != slot.id {
				t.Fatalf("增量的调用编号与 tool-start 不一致：%+v", event)
			}
			slot.args += event.Delta
		}
	}
	for index, call := range response.ToolCalls {
		slot := bySlot[index]
		if slot == nil || slot.id == "" || slot.id != call.ID || slot.name != call.Name || slot.args != string(call.Arguments) {
			t.Fatalf("第 %d 次调用流里与结论不一致：流 %+v 结论 %+v", index, slot, call)
		}
	}
	if response.ToolCalls[0].Name != "weather" || string(response.ToolCalls[0].Arguments) != `{"city":"sh"}` ||
		!strings.HasPrefix(response.ToolCalls[0].ID, "call_") {
		t.Fatalf("入参先到的调用应补齐入参并合成编号：%+v", response.ToolCalls[0])
	}
	if response.ToolCalls[1].ID != "A" || response.ToolCalls[2].ID != "B" || response.ToolCalls[2].Name != "date" {
		t.Fatalf("同一 index 换 id 应拆成两次调用：%+v", response.ToolCalls)
	}
}

// TestUpstreamIdleWatchdog 响应开始后上游长时间不来字节：看门狗断开，报可识别的上游错误。
func TestUpstreamIdleWatchdog(t *testing.T) {
	release := make(chan struct{})
	server := sseServer(t, func(w http.ResponseWriter, flush func()) {
		_, _ = w.Write([]byte(openAIChunk(`{"index":0,"delta":{"content":"Hel"},"finish_reason":null}`)))
		flush()
		select {
		case <-release:
		case <-time.After(5 * time.Second):
		}
	})
	// 晚于 server 登记：清理按后进先出，先放走处理器，server.Close 才不会等它。
	t.Cleanup(func() { close(release) })
	client := newAILLMClient(nil)
	client.idleTimeout = 150 * time.Millisecond
	started := time.Now()
	events, _, err := collectStream(t, client, streamConfig(aidomain.ProviderCustomOpenAI, server.URL))
	var upstream *aiUpstreamError
	if err == nil || !errors.As(err, &upstream) || !strings.Contains(upstream.Message, "没有返回任何数据") {
		t.Fatalf("上游卡住应被看门狗断开，得到 %v", err)
	}
	if len(events) != 1 || time.Since(started) > 5*time.Second {
		t.Fatalf("应先收到已生成的增量、并在期限附近断开：%d 个事件，用时 %s", len(events), time.Since(started))
	}
}

// TestUpstreamIdleWatchdogResetsOnBytes 字节持续到来（哪怕只是注释保活）就不触发。
func TestUpstreamIdleWatchdogResetsOnBytes(t *testing.T) {
	server := sseServer(t, func(w http.ResponseWriter, flush func()) {
		for range 5 {
			_, _ = w.Write([]byte(": keepalive\n\n"))
			flush()
			time.Sleep(60 * time.Millisecond)
		}
		_, _ = w.Write([]byte(openAIChunk(`{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}`)))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	})
	client := newAILLMClient(nil)
	client.idleTimeout = 150 * time.Millisecond
	if _, response, err := collectStream(t, client, streamConfig(aidomain.ProviderCustomOpenAI, server.URL)); err != nil || response.Text != "ok" {
		t.Fatalf("持续有字节时不该断开：%+v %v", response, err)
	}
}

func anthropicEvent(event, data string) string {
	return "event: " + event + "\ndata: " + data + "\n\n"
}

// TestAnthropicStreamWithoutStopIsAnError 没有 stop_reason 也没有 message_stop：按上游失败处理。
func TestAnthropicStreamWithoutStopIsAnError(t *testing.T) {
	server := sseServer(t, func(w http.ResponseWriter, flush func()) {
		_, _ = w.Write([]byte(anthropicEvent("message_start", `{"type":"message_start","message":{"id":"m","type":"message","role":"assistant","model":"m","content":[],"usage":{"input_tokens":3,"output_tokens":0}}}`)))
		_, _ = w.Write([]byte(anthropicEvent("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)))
		_, _ = w.Write([]byte(anthropicEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hel"}}`)))
	})
	_, response, err := collectStream(t, newAILLMClient(nil), streamConfig(aidomain.ProviderCustomAnthropic, server.URL))
	if err == nil {
		t.Fatalf("截断的 Anthropic 流应报错，得到 %+v", response)
	}
}

// TestAnthropicStreamToolUseWithoutStopReason message_stop 到了、stop_reason 缺失，但发起了工具调用：按工具收尾。
func TestAnthropicStreamToolUseWithoutStopReason(t *testing.T) {
	server := sseServer(t, func(w http.ResponseWriter, flush func()) {
		_, _ = w.Write([]byte(anthropicEvent("message_start", `{"type":"message_start","message":{"id":"m","type":"message","role":"assistant","model":"m","content":[],"usage":{"input_tokens":3,"output_tokens":0}}}`)))
		_, _ = w.Write([]byte(anthropicEvent("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"weather","input":{}}}`)))
		_, _ = w.Write([]byte(anthropicEvent("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"city\":\"sh\"}"}}`)))
		_, _ = w.Write([]byte(anthropicEvent("content_block_stop", `{"type":"content_block_stop","index":0}`)))
		_, _ = w.Write([]byte(anthropicEvent("message_delta", `{"type":"message_delta","delta":{"stop_sequence":null},"usage":{"output_tokens":5}}`)))
		_, _ = w.Write([]byte(anthropicEvent("message_stop", `{"type":"message_stop"}`)))
	})
	_, response, err := collectStream(t, newAILLMClient(nil), streamConfig(aidomain.ProviderCustomAnthropic, server.URL))
	if err != nil || response.FinishReason != aidomain.FinishToolCalls || len(response.ToolCalls) != 1 || response.ToolCalls[0].ID != "toolu_1" {
		t.Fatalf("应按工具收尾：%+v %v", response, err)
	}
}

// TestGeminiMalformedFunctionCallIsAnError MALFORMED_FUNCTION_CALL 不能当正常结束交给下游。
func TestGeminiMalformedFunctionCallIsAnError(t *testing.T) {
	result := &aidomain.ChatResponse{FinishReason: aidomain.FinishOther}
	aggregateGeminiChunk(result, &genai.GenerateContentResponse{Candidates: []*genai.Candidate{{
		FinishReason: genai.FinishReasonMalformedFunctionCall,
	}}}, nil, nil, nil)
	if result.FinishRaw != "MALFORMED_FUNCTION_CALL" || geminiFinishError("gemini", result) == nil {
		t.Fatalf("MALFORMED_FUNCTION_CALL 应判失败：%+v", result)
	}
	ok := &aidomain.ChatResponse{FinishReason: aidomain.FinishOther}
	aggregateGeminiChunk(ok, &genai.GenerateContentResponse{Candidates: []*genai.Candidate{{FinishReason: genai.FinishReasonStop}}}, nil, nil, nil)
	if ok.FinishReason != aidomain.FinishStop || geminiFinishError("gemini", ok) != nil {
		t.Fatalf("STOP 应正常结束：%+v", ok)
	}
}
