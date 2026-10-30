package httptransport

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	aidomain "aegis/internal/domain/ai"
	apperrors "aegis/pkg/errors"

	"github.com/gin-gonic/gin"
)

// 对话流写出层的契约（docs/ai-gateway.md「流式」）：开流即回头、心跳、错误事件、错误后不写 [DONE]、
// 工具调用按序号归组。用假的 runner 驱动，不经数据库与上游。

func runStream(t *testing.T, protocol string, run aiStreamRunner) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/ai/v1/chat/completions", nil)
	errorBody := func(err error) (int, gin.H) {
		shape := aiRelayErrorOf(err)
		return shape.status, aiRelayErrorBody(protocol, shape)
	}
	if protocol == aidomain.ProtocolAnthropic {
		streamAnthropicChat(c, "chat", run, errorBody)
	} else {
		streamOpenAIChat(c, "chat", run, errorBody)
	}
	return recorder
}

func withHeartbeat(t *testing.T, every time.Duration) {
	t.Helper()
	previous := aiStreamHeartbeatEvery
	aiStreamHeartbeatEvery = every
	t.Cleanup(func() { aiStreamHeartbeatEvery = previous })
}

// TestAIStreamHeartbeatWhileUpstreamSilent 暂扣后立刻回 200 与 SSE 头，上游不出字期间写 `: ping`。
func TestAIStreamHeartbeatWhileUpstreamSilent(t *testing.T) {
	withHeartbeat(t, 20*time.Millisecond)
	recorder := runStream(t, aidomain.ProtocolOpenAI, func(begin func(string), onEvent func(aidomain.StreamEvent) error) (*aidomain.ChatResponse, error) {
		begin("AIG123")
		time.Sleep(90 * time.Millisecond) // 推理模型在想
		if err := onEvent(aidomain.StreamEvent{Type: aidomain.StreamText, Delta: "Hi"}); err != nil {
			return nil, err
		}
		return &aidomain.ChatResponse{Text: "Hi", FinishReason: aidomain.FinishStop}, nil
	})
	body := recorder.Body.String()
	if recorder.Code != 200 || recorder.Header().Get("X-Aegis-Request-No") != "AIG123" ||
		!strings.HasPrefix(recorder.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("开流应回 200、SSE 头与调用编号：%d %v", recorder.Code, recorder.Header())
	}
	if !strings.HasPrefix(body, ": ping\n\n") || strings.Count(body, ": ping\n\n") < 2 {
		t.Fatalf("上游沉默期间应有心跳：%q", body)
	}
	if !strings.Contains(body, `"finish_reason":"stop"`) || !strings.HasSuffix(body, "data: [DONE]\n\n") {
		t.Fatalf("成功的流应带 finish_reason 并以 [DONE] 收尾：%q", body)
	}
}

// TestAIStreamErrorAfterBeginIsAnErrorEvent 开流后、第一个增量前失败：200 + 错误事件（带 aegis_code 与 status），不写 [DONE]。
func TestAIStreamErrorAfterBeginIsAnErrorEvent(t *testing.T) {
	recorder := runStream(t, aidomain.ProtocolOpenAI, func(begin func(string), _ func(aidomain.StreamEvent) error) (*aidomain.ChatResponse, error) {
		begin("AIG1")
		return nil, apperrors.New(40231, http.StatusServiceUnavailable, "服务正在更新")
	})
	body := recorder.Body.String()
	if recorder.Code != 200 || !strings.Contains(body, `data: {"error":{`) || !strings.Contains(body, `"aegis_code":40231`) ||
		!strings.Contains(body, `"status":503`) || !strings.Contains(body, `"message":"服务正在更新"`) {
		t.Fatalf("开流后的错误应是错误事件：%d %q", recorder.Code, body)
	}
	if strings.Contains(body, "[DONE]") {
		t.Fatalf("错误之后不该写 [DONE]：%q", body)
	}
}

// TestAIStreamErrorBeforeBeginKeepsStatus 还没开流就失败（鉴权、余额等）：照旧是带状态码的 JSON 错误。
func TestAIStreamErrorBeforeBeginKeepsStatus(t *testing.T) {
	recorder := runStream(t, aidomain.ProtocolOpenAI, func(func(string), func(aidomain.StreamEvent) error) (*aidomain.ChatResponse, error) {
		return nil, apperrors.New(40201, http.StatusPaymentRequired, "余额不足")
	})
	if recorder.Code != http.StatusPaymentRequired || !strings.HasPrefix(recorder.Header().Get("Content-Type"), "application/json") ||
		!strings.Contains(recorder.Body.String(), `"aegis_code":40201`) {
		t.Fatalf("开流前的错误应保持状态码与 JSON：%d %s %q", recorder.Code, recorder.Header().Get("Content-Type"), recorder.Body.String())
	}
}

// TestAIStreamMidStreamErrorHasNoDone 输出到一半上游失败：错误块收尾，不写收尾块与 [DONE]。
func TestAIStreamMidStreamErrorHasNoDone(t *testing.T) {
	recorder := runStream(t, aidomain.ProtocolOpenAI, func(begin func(string), onEvent func(aidomain.StreamEvent) error) (*aidomain.ChatResponse, error) {
		begin("")
		_ = onEvent(aidomain.StreamEvent{Type: aidomain.StreamText, Delta: "Hel"})
		return nil, errors.New("上游的流没有给出结束原因就断开了")
	})
	body := recorder.Body.String()
	if !strings.Contains(body, `"content":"Hel"`) || !strings.Contains(body, `"error":{`) || !strings.Contains(body, `"status":502`) {
		t.Fatalf("应先有已生成的增量、再有错误块：%q", body)
	}
	if strings.Contains(body, "[DONE]") || strings.Contains(body, `"finish_reason":"stop"`) {
		t.Fatalf("失败的流不该冒充正常结束：%q", body)
	}
}

// TestAIStreamToolCallsGroupedByIndex 按序号归组：两次调用同一个编号也不串。
func TestAIStreamToolCallsGroupedByIndex(t *testing.T) {
	recorder := runStream(t, aidomain.ProtocolOpenAI, func(begin func(string), onEvent func(aidomain.StreamEvent) error) (*aidomain.ChatResponse, error) {
		begin("")
		for _, event := range []aidomain.StreamEvent{
			{Type: aidomain.StreamToolStart, ToolIndex: 3, ToolID: "dup", ToolName: "a"},
			{Type: aidomain.StreamToolStart, ToolIndex: 4, ToolID: "dup", ToolName: "b"},
			{Type: aidomain.StreamToolDelta, ToolIndex: 3, ToolID: "dup", Delta: `{"x":1}`},
			{Type: aidomain.StreamToolDelta, ToolIndex: 4, ToolID: "dup", Delta: `{"y":2}`},
		} {
			if err := onEvent(event); err != nil {
				return nil, err
			}
		}
		return &aidomain.ChatResponse{FinishReason: aidomain.FinishToolCalls,
			ToolCalls: []aidomain.ToolCall{{ID: "dup", Name: "a"}, {ID: "dup", Name: "b"}}}, nil
	})
	body := recorder.Body.String()
	for _, want := range []string{
		`"tool_calls":[{"function":{"arguments":"","name":"a"},"id":"dup","index":0,"type":"function"}]`,
		`"tool_calls":[{"function":{"arguments":"","name":"b"},"id":"dup","index":1,"type":"function"}]`,
		`"tool_calls":[{"function":{"arguments":"{\"x\":1}"},"index":0}]`,
		`"tool_calls":[{"function":{"arguments":"{\"y\":2}"},"index":1}]`,
		`"finish_reason":"tool_calls"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("缺少 %s：%q", want, body)
		}
	}
}

// TestAIStreamUnknownFinishNotStop 认不出的结束原因原样转出，不冒充 stop / end_turn。
func TestAIStreamUnknownFinishNotStop(t *testing.T) {
	result := &aidomain.ChatResponse{FinishReason: aidomain.FinishOther, FinishRaw: "pause_turn"}
	if got := openAIGatewayFinish(result); got != "pause_turn" {
		t.Fatalf("OpenAI 形状应原样转出：%q", got)
	}
	if got := anthropicGatewayFinish(&aidomain.ChatResponse{FinishReason: aidomain.FinishOther}); got != "other" {
		t.Fatalf("没有原样原因时也不该是 end_turn：%q", got)
	}
}

// TestAIStreamAnthropicPingAndError Anthropic 形状：心跳是 event: ping，错误是 event: error、不写 message_stop。
func TestAIStreamAnthropicPingAndError(t *testing.T) {
	withHeartbeat(t, 20*time.Millisecond)
	recorder := runStream(t, aidomain.ProtocolAnthropic, func(begin func(string), onEvent func(aidomain.StreamEvent) error) (*aidomain.ChatResponse, error) {
		begin("")
		time.Sleep(50 * time.Millisecond)
		_ = onEvent(aidomain.StreamEvent{Type: aidomain.StreamText, Delta: "Hel"})
		return nil, apperrors.New(40231, http.StatusServiceUnavailable, "服务正在更新")
	})
	body := recorder.Body.String()
	if !strings.Contains(body, "event: ping\ndata: {\"type\":\"ping\"}\n\n") {
		t.Fatalf("应有 Anthropic 形状的心跳：%q", body)
	}
	if !strings.Contains(body, "event: error\ndata: {\"error\":{") || !strings.Contains(body, `"aegis_code":40231`) ||
		strings.Contains(body, "message_stop") {
		t.Fatalf("应以 error 事件收尾：%q", body)
	}
}

// TestAIStreamOutlivesServerWriteTimeout 真实服务器配了很短的 WriteTimeout：流式处理器按连接解除写期限，
// 长流照样完整收尾（[DONE]），而不是在期限处被静默截断。
func TestAIStreamOutlivesServerWriteTimeout(t *testing.T) {
	withHeartbeat(t, 50*time.Millisecond)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/chat", func(c *gin.Context) {
		streamOpenAIChat(c, "chat", func(begin func(string), onEvent func(aidomain.StreamEvent) error) (*aidomain.ChatResponse, error) {
			begin("")
			time.Sleep(400 * time.Millisecond)
			if err := onEvent(aidomain.StreamEvent{Type: aidomain.StreamText, Delta: "late"}); err != nil {
				return nil, err
			}
			return &aidomain.ChatResponse{Text: "late", FinishReason: aidomain.FinishStop}, nil
		}, func(err error) (int, gin.H) { return aiGatewayErrorBody(aidomain.ProtocolOpenAI, err) })
	})
	server := httptest.NewUnstartedServer(router)
	server.Config.WriteTimeout = 100 * time.Millisecond
	server.Start()
	t.Cleanup(server.Close)

	resp, err := http.Post(server.URL+"/chat", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || !strings.Contains(string(body), `"content":"late"`) || !strings.HasSuffix(string(body), "data: [DONE]\n\n") {
		t.Fatalf("流应活过 WriteTimeout 并完整收尾：%v %q", err, body)
	}
}
