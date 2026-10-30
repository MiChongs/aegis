package httptransport

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	aidomain "aegis/internal/domain/ai"
	"aegis/internal/domain/aigateway"
	authdomain "aegis/internal/domain/auth"
	pgrepo "aegis/internal/repository/postgres"
	"aegis/internal/service"
)

// /api/ai/v1 的 HTTP 级集成测试：SDK 实际看到的响应形状、错误信封、SSE、请求编号头，
// 以及 multipart 重组（换模型名、文件原样）与转写 text 格式的转换。计费数值规则由
// service 包的 TestAIGatewayBillingIntegration 覆盖，这里只核对传输层。
//
// AEGIS_TEST_PG_DSN=postgres://…/aegis_aig_test go test ./internal/transport/http -run TestAIRelayHTTPIntegration
func TestAIRelayHTTPIntegration(t *testing.T) {
	dsn := os.Getenv("AEGIS_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("未设置 AEGIS_TEST_PG_DSN")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	resetRelayTestDatabase(t, ctx, pool)

	pg := pgrepo.New(pool)
	log := zap.NewNop()
	ai := service.NewAIProviderService(log, pg, "relay-test-master-key")
	gw := service.NewAIGatewayService(log, pg, nil, ai)
	upstream := newRelayFakeUpstream(t)

	const appID = int64(32000)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'relay-test')`, appID); err != nil {
		t.Fatal(err)
	}
	// 链路：先走一条总是 503 的通道（可重试），再回退到正常的 fake —— 追踪里应有两次尝试。
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("x-request-id", "bad-1")
		http.Error(w, `{"error":{"message":"overloaded"}}`, http.StatusServiceUnavailable)
	}))
	t.Cleanup(broken.Close)
	for _, channel := range []struct {
		name, url string
		priority  int
	}{{"broken", broken.URL + "/v1", 1}, {"fake", upstream.server.URL + "/v1", 10}} {
		name, provider, enabled, priority := channel.name, aidomain.ProviderCustomOpenAI, true, channel.priority
		if _, err := ai.Save(ctx, aidomain.ConfigMutation{AppID: appID, Name: &name, Provider: &provider, Enabled: &enabled,
			Priority: &priority, Settings: map[string]string{aidomain.KeyBaseURL: channel.url},
			Secrets: map[string]string{aidomain.KeyAPIKey: "upstream-secret"}}); err != nil {
			t.Fatal(err)
		}
	}
	settings := aigateway.DefaultSettings()
	settings.Enabled = true
	settings.Sources = []string{aigateway.SourceIntegral}
	settings.RPM = 0
	if _, err := gw.SaveSettings(ctx, appID, settings, "test"); err != nil {
		t.Fatal(err)
	}
	for _, input := range []aigateway.ModelInput{
		{Model: "chat", UpstreamModel: "up-chat", Kind: aigateway.KindChat, InputPrice: decimal.NewFromInt(1000), OutputPrice: decimal.NewFromInt(1000)},
		{Model: "cut", UpstreamModel: "up-cut", Kind: aigateway.KindChat, InputPrice: decimal.NewFromInt(1000), OutputPrice: decimal.NewFromInt(1000)},
		{Model: "edit", UpstreamModel: "up-edit", Kind: aigateway.KindImage, ImagePrice: decimal.NewFromInt(1)},
		{Model: "whisper", UpstreamModel: "up-whisper", Kind: aigateway.KindTranscription, MinutePrice: decimal.NewFromInt(2)},
		{Model: "draw", UpstreamModel: "up-draw", Kind: aigateway.KindImage, ImagePrice: decimal.NewFromInt(2)},
		{Model: "tts", UpstreamModel: "up-tts", Kind: aigateway.KindSpeech, CharPrice: decimal.NewFromInt(1000)},
	} {
		if _, err := gw.CreateModel(ctx, appID, input); err != nil {
			t.Fatal(err)
		}
	}
	var userID int64
	if err := pool.QueryRow(ctx, `INSERT INTO users (appid, account, enabled, integral) VALUES ($1, 'relay-user', TRUE, 100) RETURNING id`,
		appID).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	created, err := gw.CreateKey(ctx, &authdomain.Session{AppID: appID, UserID: userID}, aigateway.KeyInput{})
	if err != nil {
		t.Fatal(err)
	}
	secret := created.Secret

	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := &Handler{aiGateway: gw}
	relay := router.Group("/api/ai/v1")
	relay.GET("/models", h.AIRelayModels)
	relay.POST("/chat/completions", h.AIRelayChatCompletions)
	relay.POST("/messages", h.AIRelayMessages)
	relay.POST("/images/edits", h.AIRelayImageEdits)
	relay.POST("/audio/transcriptions", h.AIRelayTranscriptions)
	relay.POST("/images/generations", h.AIRelayImageGenerations)
	relay.POST("/audio/speech", h.AIRelaySpeech)
	relay.GET("/jobs/:jobId", h.AIRelayJob)
	relay.GET("/jobs/:jobId/content", h.AIRelayJobContent)
	relay.DELETE("/jobs/:jobId", h.AIRelayCancelJob)
	router.GET("/live", func(c *gin.Context) { h.streamAIGatewayLive(c, appID) })
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	// 管理端 SSE：先 ready，再收到调用的 created / finished。
	liveCtx, stopLive := context.WithCancel(ctx)
	defer stopLive()
	liveReq, _ := http.NewRequestWithContext(liveCtx, "GET", server.URL+"/live", nil)
	liveResp, err := http.DefaultClient.Do(liveReq)
	if err != nil || liveResp.StatusCode != 200 || !strings.HasPrefix(liveResp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("SSE 连接失败：%v %+v", err, liveResp)
	}
	defer liveResp.Body.Close()
	sseLines := make(chan string, 256)
	go func() {
		scanner := bufio.NewScanner(liveResp.Body)
		scanner.Buffer(make([]byte, 1<<20), 1<<20)
		for scanner.Scan() {
			sseLines <- scanner.Text()
		}
		close(sseLines)
	}()
	waitSSE := func(prefix string) string {
		t.Helper()
		deadline := time.After(3 * time.Second)
		for {
			select {
			case line, ok := <-sseLines:
				if !ok {
					t.Fatalf("SSE 断开，未等到 %q", prefix)
				}
				if strings.HasPrefix(line, prefix) {
					return line
				}
			case <-deadline:
				t.Fatalf("SSE 未等到 %q", prefix)
			}
		}
	}
	waitSSE("event: ready")

	do := func(method, path, contentType string, body io.Reader, headers map[string]string) (*http.Response, []byte) {
		t.Helper()
		req, _ := http.NewRequest(method, server.URL+path, body)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		for key, value := range headers {
			req.Header.Set(key, value)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return resp, data
	}
	bearer := map[string]string{"Authorization": "Bearer " + secret}

	// 模型列表
	resp, body := do("GET", "/api/ai/v1/models", "", nil, bearer)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"id":"chat"`) || !strings.Contains(string(body), `"object":"list"`) {
		t.Fatalf("模型列表不对：%d %s", resp.StatusCode, body)
	}

	// 非流式对话：OpenAI 形状、对外模型名、请求编号头
	live, cancelLive := gw.SubscribeLive(appID)
	defer cancelLive()
	chatBody := `{"model":"chat","messages":[{"role":"user","content":"hi"}],"max_tokens":20}`
	resp, body = do("POST", "/api/ai/v1/chat/completions", "application/json", strings.NewReader(chatBody), bearer)
	var completion struct {
		Object  string `json:"object"`
		Model   string `json:"model"`
		Choices []struct {
			Message struct{ Content string } `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &completion); err != nil || resp.StatusCode != 200 || completion.Object != "chat.completion" ||
		completion.Model != "chat" || len(completion.Choices) != 1 || completion.Usage.PromptTokens != 10 {
		t.Fatalf("对话响应不对：%d %s", resp.StatusCode, body)
	}
	requestNo := resp.Header.Get("X-Aegis-Request-No")
	if !strings.HasPrefix(requestNo, "AIG") {
		t.Fatalf("应带请求编号头，得到 %q", requestNo)
	}

	// 实时事件：created → attempt… → finished，finished 带两次尝试与上游请求编号。
	var kinds []string
	var finished service.AIGatewayLiveEvent
	for finished.Type == "" {
		select {
		case event := <-live:
			if event.RequestNo != requestNo {
				continue
			}
			kinds = append(kinds, event.Type)
			if event.Type == service.AIGatewayLiveFinished {
				finished = event
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("没有收到 finished 事件，已收到 %v", kinds)
		}
	}
	if kinds[0] != service.AIGatewayLiveCreated || !slices.Contains(kinds, service.AIGatewayLiveAttempt) ||
		finished.Request == nil || finished.Request.Attempts != 2 || finished.Request.Account != "relay-user" ||
		finished.Request.UpstreamRequestID != "up-123" || finished.Request.Status != aigateway.StatusSucceeded {
		t.Fatalf("实时事件不对：%v %+v", kinds, finished.Request)
	}

	waitSSE("event: created")
	if line := waitSSE("event: finished"); line == "" {
		t.Fatal("SSE 应推 finished")
	}
	if data := waitSSE("data: "); !strings.Contains(data, requestNo) || !strings.Contains(data, `"upstreamRequestId":"up-123"`) {
		t.Fatalf("SSE finished 数据不对：%s", data)
	}

	// 追踪：第一次尝试 503 失败（可重试），第二次成功；请求与响应摘要。
	traced, err := gw.GetRequest(ctx, appID, requestNo)
	if err != nil || traced.Trace == nil || len(traced.Trace.Attempts) != 2 {
		t.Fatalf("追踪应有两次尝试：%+v %v", traced, err)
	}
	first, second := traced.Trace.Attempts[0], traced.Trace.Attempts[1]
	if first.Status != aigateway.AttemptFailed || first.HTTPStatus != 503 || !first.Retryable || first.ConfigName != "broken" ||
		first.UpstreamRequestID != "bad-1" || !strings.HasSuffix(first.URL, "/v1/chat/completions") {
		t.Fatalf("第一次尝试不对：%+v", first)
	}
	if second.Status != aigateway.AttemptSucceeded || second.HTTPStatus != 200 || second.ConfigName != "fake" ||
		second.UpstreamRequestID != "up-123" || second.RateLimit["x-ratelimit-remaining-requests"] != "99" ||
		second.ResponseBytes == 0 || second.RequestBytes == 0 || second.HeadersMs == 0 || second.Model != "up-chat" {
		t.Fatalf("第二次尝试不对：%+v", second)
	}
	if traced.Attempts != 2 || traced.ConfigName != "fake" || traced.FinishReason != "stop" || traced.RequestBytes == 0 ||
		traced.Trace.Response["text"] != "Hello" || traced.Trace.Request["messageCount"] != float64(1) {
		t.Fatalf("追踪摘要不对：%+v %+v %+v", traced.TraceSummary, traced.Trace.Request, traced.Trace.Response)
	}
	if page, err := gw.ListRequests(ctx, aigateway.RequestQuery{AppID: appID, Keyword: "up-12"}); err != nil || page.Total != 1 {
		t.Fatalf("应能按上游请求编号查到：%+v %v", page, err)
	}

	// 流式：SSE chunk、用量块、[DONE]
	streamBody := `{"model":"chat","stream":true,"messages":[{"role":"user","content":"hi"}],"max_tokens":20}`
	resp, body = do("POST", "/api/ai/v1/chat/completions", "application/json", strings.NewReader(streamBody), bearer)
	text := string(body)
	if resp.StatusCode != 200 || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") ||
		!strings.Contains(text, `"content":"Hel"`) || !strings.Contains(text, `"prompt_tokens":10`) ||
		!strings.HasSuffix(strings.TrimSpace(text), "data: [DONE]") || !strings.Contains(text, `"model":"chat"`) {
		t.Fatalf("流式响应不对：%d %s", resp.StatusCode, text)
	}
	if streamed, err := gw.GetRequest(ctx, appID, resp.Header.Get("X-Aegis-Request-No")); err != nil ||
		streamed.FirstTokenMs <= 0 || streamed.Trace.Attempts[1].FirstTokenMs <= 0 || streamed.Trace.Response["text"] != "Hel" {
		t.Fatalf("流式应记首字耗时与已生成内容：%+v %v", streamed, err)
	}

	// 上游的流半路断了（没有 finish_reason 也没有 [DONE]）：错误事件收尾、不写 [DONE]，按失败放回不扣费
	cutBody := `{"model":"cut","stream":true,"messages":[{"role":"user","content":"hi"}],"max_tokens":20}`
	resp, body = do("POST", "/api/ai/v1/chat/completions", "application/json", strings.NewReader(cutBody), bearer)
	text = string(body)
	if resp.StatusCode != 200 || !strings.Contains(text, `"content":"Hel"`) || !strings.Contains(text, `data: {"error":{`) ||
		strings.Contains(text, "[DONE]") || strings.Contains(text, `"finish_reason":"stop"`) {
		t.Fatalf("截断的上游流应以错误事件收尾：%d %s", resp.StatusCode, text)
	}
	if cut, err := gw.GetRequest(ctx, appID, resp.Header.Get("X-Aegis-Request-No")); err != nil || cut.Status != aigateway.StatusFailed {
		t.Fatalf("截断的上游流应判失败、不扣费：%+v %v", cut, err)
	}

	// Anthropic Messages：x-api-key 鉴权、Messages 形状
	messagesBody := `{"model":"chat","max_tokens":20,"messages":[{"role":"user","content":"hi"}]}`
	resp, body = do("POST", "/api/ai/v1/messages", "application/json", strings.NewReader(messagesBody), map[string]string{"x-api-key": secret})
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"type":"message"`) || !strings.Contains(string(body), `"input_tokens":10`) {
		t.Fatalf("Messages 响应不对：%d %s", resp.StatusCode, body)
	}

	// 错误信封：无效密钥 401、未知模型 404、余额不足 402（OpenAI 与 Anthropic 两种形状）
	resp, body = do("POST", "/api/ai/v1/chat/completions", "application/json", strings.NewReader(chatBody),
		map[string]string{"Authorization": "Bearer " + aigateway.KeyPrefix + strings.Repeat("Z", 43)})
	if resp.StatusCode != 401 || !strings.Contains(string(body), `"code":"invalid_api_key"`) {
		t.Fatalf("无效密钥应 401：%d %s", resp.StatusCode, body)
	}
	resp, body = do("POST", "/api/ai/v1/chat/completions", "application/json",
		strings.NewReader(`{"model":"nope","messages":[{"role":"user","content":"hi"}]}`), bearer)
	if resp.StatusCode != 404 || !strings.Contains(string(body), `"code":"model_not_found"`) {
		t.Fatalf("未知模型应 404：%d %s", resp.StatusCode, body)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET integral = 0 WHERE id = $1`, userID); err != nil {
		t.Fatal(err)
	}
	resp, body = do("POST", "/api/ai/v1/chat/completions", "application/json", strings.NewReader(chatBody), bearer)
	if resp.StatusCode != 402 || !strings.Contains(string(body), `"type":"insufficient_quota"`) || !strings.Contains(string(body), `"aegis_code":40201`) {
		t.Fatalf("余额不足应 402：%d %s", resp.StatusCode, body)
	}
	resp, body = do("POST", "/api/ai/v1/messages", "application/json", strings.NewReader(messagesBody), map[string]string{"x-api-key": secret})
	if resp.StatusCode != 402 || !strings.Contains(string(body), `"type":"billing_error"`) {
		t.Fatalf("Anthropic 形状的余额不足不对：%d %s", resp.StatusCode, body)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET integral = 100 WHERE id = $1`, userID); err != nil {
		t.Fatal(err)
	}

	// 图像编辑（multipart）：上游收到换过的模型名与原样的文件
	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	_ = writer.WriteField("model", "edit")
	_ = writer.WriteField("prompt", "make it green")
	part, _ := writer.CreateFormFile("image", "apple.png")
	_, _ = part.Write([]byte("PNGDATA"))
	_ = writer.Close()
	resp, body = do("POST", "/api/ai/v1/images/edits", writer.FormDataContentType(), &form, bearer)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"data"`) {
		t.Fatalf("图像编辑失败：%d %s", resp.StatusCode, body)
	}
	if got := upstream.field("model"); got != "up-edit" {
		t.Fatalf("上游应收到上游模型名，得到 %q", got)
	}
	if got := upstream.field("prompt"); got != "make it green" || upstream.file("image") != "PNGDATA" {
		t.Fatalf("表单字段与文件应原样转发：prompt=%q file=%q", got, upstream.file("image"))
	}
	if edited, err := gw.GetRequest(ctx, appID, resp.Header.Get("X-Aegis-Request-No")); err != nil ||
		edited.Trace.Request["prompt"] != "make it green" || edited.Trace.Request["files"] == nil || edited.Trace.Response["data"] == nil {
		t.Fatalf("表单请求的追踪不对：%+v %v", edited.Trace, err)
	}

	// 转写：客户端要 text，上游收到 json（带时长），客户端拿到纯文本；按 90 秒计 3 点
	form.Reset()
	writer = multipart.NewWriter(&form)
	_ = writer.WriteField("model", "whisper")
	_ = writer.WriteField("response_format", "text")
	part, _ = writer.CreateFormFile("file", "a.mp3")
	_, _ = part.Write(bytes.Repeat([]byte{1}, 1000))
	_ = writer.Close()
	resp, body = do("POST", "/api/ai/v1/audio/transcriptions", writer.FormDataContentType(), &form, bearer)
	if resp.StatusCode != 200 || string(body) != "hello transcript" || !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/plain") {
		t.Fatalf("转写应回纯文本：%d %q %s", resp.StatusCode, body, resp.Header.Get("Content-Type"))
	}
	if got := upstream.field("response_format"); got != "json" {
		t.Fatalf("上游应收到 json 格式，得到 %q", got)
	}
	record, err := gw.GetRequest(ctx, appID, resp.Header.Get("X-Aegis-Request-No"))
	if err != nil || record.AudioMillis != 90_000 || record.CostMicro != 3_000_000 || record.UsageEstimated {
		t.Fatalf("转写应按上游报的 90 秒计 3 点：%+v %v", record, err)
	}

	// ── 异步任务 ──
	integral := func() int64 {
		t.Helper()
		var value int64
		if err := pool.QueryRow(ctx, `SELECT integral FROM users WHERE id = $1`, userID).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	asyncHeaders := map[string]string{"Authorization": "Bearer " + secret, "Prefer": "respond-async"}
	before := integral()
	resp, body = do("POST", "/api/ai/v1/images/generations", "application/json",
		strings.NewReader(`{"model":"draw","prompt":"a cat","n":1}`), asyncHeaders)
	var job aigateway.JobView
	if err := json.Unmarshal(body, &job); err != nil || resp.StatusCode != 202 || job.Status != aigateway.JobInProgress ||
		resp.Header.Get("Location") != "/api/ai/v1/jobs/"+job.ID || job.Object != "aegis.job" {
		t.Fatalf("异步提交应回 202 与进行中的任务：%d %s", resp.StatusCode, body)
	}
	resp, body = do("GET", "/api/ai/v1/jobs/"+job.ID+"?wait=5", "", nil, bearer)
	if err := json.Unmarshal(body, &job); err != nil || resp.StatusCode != 200 || job.Status != aigateway.JobCompleted ||
		!strings.Contains(string(job.Result), `"b64_json":"AAAA"`) || job.CompletedAt == nil {
		t.Fatalf("长轮询应拿到完成的任务与内嵌结果：%d %s", resp.StatusCode, body)
	}
	if after := integral(); after != before-2 {
		t.Fatalf("异步生图应扣 2 积分：%d → %d", before, after)
	}
	other, err := gw.CreateKey(ctx, &authdomain.Session{AppID: appID, UserID: userID}, aigateway.KeyInput{})
	if err != nil {
		t.Fatal(err)
	}
	if resp, _ = do("GET", "/api/ai/v1/jobs/"+job.ID, "", nil, map[string]string{"Authorization": "Bearer " + other.Secret}); resp.StatusCode != 200 {
		t.Fatalf("同一用户的另一把密钥应能取到任务：%d", resp.StatusCode)
	}
	if resp, body = do("GET", "/api/ai/v1/jobs/AIGnope", "", nil, bearer); resp.StatusCode != 404 || !strings.Contains(string(body), "40226") {
		t.Fatalf("不存在的任务应 404：%d %s", resp.StatusCode, body)
	}

	// 取消：上游挂着不回，DELETE 后任务为 canceled、积分不变。
	before = integral()
	resp, body = do("POST", "/api/ai/v1/images/generations", "application/json",
		strings.NewReader(`{"model":"draw","prompt":"slow cat","n":1}`), asyncHeaders)
	if err := json.Unmarshal(body, &job); err != nil || resp.StatusCode != 202 {
		t.Fatalf("提交慢任务失败：%d %s", resp.StatusCode, body)
	}
	time.Sleep(200 * time.Millisecond)
	resp, body = do("DELETE", "/api/ai/v1/jobs/"+job.ID, "", nil, bearer)
	if err := json.Unmarshal(body, &job); err != nil || job.Status != aigateway.JobCanceled || job.Error == nil || job.Error.Code != "job_canceled" {
		t.Fatalf("取消后应为 canceled：%d %s", resp.StatusCode, body)
	}
	if after := integral(); after != before {
		t.Fatalf("取消的任务不应扣费：%d → %d", before, after)
	}
	if canceled, _ := gw.GetRequest(ctx, appID, job.ID); canceled.Status != aigateway.StatusFailed {
		t.Fatalf("取消的调用应已放回：%+v", canceled)
	}

	// 上游拒绝：任务 failed，错误信封与同步调用一致（上游 400 原样报 400）。
	resp, body = do("POST", "/api/ai/v1/images/generations", "application/json",
		strings.NewReader(`{"model":"draw","prompt":"reject me","n":1}`), asyncHeaders)
	_ = json.Unmarshal(body, &job)
	resp, body = do("GET", "/api/ai/v1/jobs/"+job.ID+"?wait=5", "", nil, bearer)
	if err := json.Unmarshal(body, &job); err != nil || job.Status != aigateway.JobFailed || job.Error == nil ||
		job.Error.Status != 400 || !strings.Contains(job.Error.Message, "unsafe prompt") {
		t.Fatalf("上游拒绝的任务应 failed：%d %s", resp.StatusCode, body)
	}

	// 二进制结果：语音合成走 result_url。
	resp, body = do("POST", "/api/ai/v1/audio/speech", "application/json",
		strings.NewReader(`{"model":"tts","input":"你好","voice":"alloy"}`), asyncHeaders)
	_ = json.Unmarshal(body, &job)
	resp, body = do("GET", "/api/ai/v1/jobs/"+job.ID+"?wait=5", "", nil, bearer)
	if err := json.Unmarshal(body, &job); err != nil || job.Status != aigateway.JobCompleted ||
		job.ResultURL != "/api/ai/v1/jobs/"+job.ID+"/content" || job.ContentType != "audio/mpeg" {
		t.Fatalf("语音任务应给 result_url：%s", body)
	}
	if resp, body = do("GET", job.ResultURL, "", nil, bearer); resp.StatusCode != 200 || string(body) != "MP3DATA" ||
		resp.Header.Get("Content-Type") != "audio/mpeg" {
		t.Fatalf("取结果应回原始音频：%d %q", resp.StatusCode, body)
	}

	// 清理：超时仍在进行中的任务判为中断；结果过期的任务删掉。
	if _, err := pool.Exec(ctx, `INSERT INTO ai_gateway_jobs (request_id, request_no, appid, user_id, endpoint, status, created_at, expires_at)
SELECT id, request_no, appid, user_id, endpoint, 'in_progress', NOW() - interval '1 hour', NOW() - interval '1 minute'
FROM ai_gateway_requests WHERE request_no = $1`, requestNo); err != nil {
		t.Fatal(err)
	}
	if _, interrupted, err := pg.SweepAIGatewayJobs(ctx, time.Now()); err != nil || interrupted != 1 {
		t.Fatalf("应判一个中断：%d %v", interrupted, err)
	}
	resp, body = do("GET", "/api/ai/v1/jobs/"+requestNo, "", nil, bearer)
	if !strings.Contains(string(body), `"code":"job_interrupted"`) || !strings.Contains(string(body), `"status":"failed"`) {
		t.Fatalf("中断的任务应 failed：%s", body)
	}
	if _, err := pool.Exec(ctx, `UPDATE ai_gateway_jobs SET expires_at = NOW() - interval '1 second' WHERE request_no = $1`, requestNo); err != nil {
		t.Fatal(err)
	}
	if deleted, _, err := pg.SweepAIGatewayJobs(ctx, time.Now()); err != nil || deleted != 1 {
		t.Fatalf("过期结果应删掉：%d %v", deleted, err)
	}

	// 同步保活：上游慢于阈值时先回 200 头，再写空格，最后是 JSON。
	aiRelayKeepaliveAfter, aiRelayKeepaliveEvery = 150*time.Millisecond, 100*time.Millisecond
	defer func() { aiRelayKeepaliveAfter, aiRelayKeepaliveEvery = 20*time.Second, 15*time.Second }()
	resp, body = do("POST", "/api/ai/v1/images/generations", "application/json",
		strings.NewReader(`{"model":"draw","prompt":"slowish cat","n":1}`), bearer)
	var images struct {
		Data []map[string]any `json:"data"`
	}
	if resp.StatusCode != 200 || resp.Header.Get("X-Aegis-Keepalive") != "whitespace" || !strings.HasPrefix(string(body), " ") ||
		json.Unmarshal(body, &images) != nil || len(images.Data) != 1 || !strings.HasPrefix(resp.Header.Get("X-Aegis-Request-No"), "AIG") {
		t.Fatalf("保活响应不对：%d %v %q", resp.StatusCode, resp.Header, body)
	}
	resp, body = do("POST", "/api/ai/v1/images/generations", "application/json",
		strings.NewReader(`{"model":"draw","prompt":"slowish reject me","n":1}`), bearer)
	if resp.StatusCode != 200 || !strings.Contains(string(body), "unsafe prompt") || !strings.HasPrefix(string(body), " ") {
		t.Fatalf("保活开始后出错应是 200 + 错误信封：%d %q", resp.StatusCode, body)
	}
	resp, body = do("POST", "/api/ai/v1/images/generations", "application/json",
		strings.NewReader(`{"model":"draw","prompt":"slowish cat","n":1}`),
		map[string]string{"Authorization": "Bearer " + secret, "X-Aegis-Keepalive": "off"})
	if resp.StatusCode != 200 || strings.HasPrefix(string(body), " ") || resp.Header.Get("X-Aegis-Keepalive") != "" {
		t.Fatalf("关掉保活后应是普通响应：%d %q", resp.StatusCode, body)
	}

	// 隐藏内容：追踪只留参数与长度。
	settings.RedactContent = true
	if _, err := gw.SaveSettings(ctx, appID, settings, "test"); err != nil {
		t.Fatal(err)
	}
	resp, _ = do("POST", "/api/ai/v1/chat/completions", "application/json", strings.NewReader(chatBody), bearer)
	hidden, err := gw.GetRequest(ctx, appID, resp.Header.Get("X-Aegis-Request-No"))
	if err != nil || hidden.Trace == nil || hidden.Trace.Response["text"] != "[已隐藏 5 字]" ||
		!strings.Contains(fmt.Sprint(hidden.Trace.Request["messages"]), "[已隐藏 2 字]") {
		t.Fatalf("隐藏内容时不应留正文：%+v %v", hidden.Trace, err)
	}

	// 概况：按通道汇总上游尝试，broken 每次都失败。
	summary, err := gw.LiveSummary(ctx, appID, 60)
	if err != nil || summary.Total < 6 || summary.Retried < 5 || summary.InFlight != 0 || len(summary.Upstreams) != 2 {
		t.Fatalf("概况不对：%+v %v", summary, err)
	}
	for _, item := range summary.Upstreams {
		if item.ConfigName == "broken" && (item.Failed != item.Attempts || item.LastError == "") {
			t.Fatalf("broken 通道应全部失败并带最后的错误：%+v", item)
		}
	}
}

func resetRelayTestDatabase(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob("../../../migrations/postgres/*.up.sql")
	sort.Strings(files)
	for _, file := range files {
		content, _ := os.ReadFile(file)
		if sql := strings.TrimSpace(string(content)); sql != "" {
			if _, err := pool.Exec(ctx, sql); err != nil {
				msg := err.Error()
				if strings.Contains(msg, "vector") || strings.Contains(msg, "postgis") ||
					strings.Contains(msg, "geography") || strings.Contains(msg, "geometry") {
					continue
				}
				t.Fatalf("apply %s: %v", filepath.Base(file), err)
			}
		}
	}
}

type relayFakeUpstream struct {
	server *httptest.Server
	mu     sync.Mutex
	fields map[string]string
	files  map[string]string
}

func (f *relayFakeUpstream) field(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fields[key]
}

func (f *relayFakeUpstream) file(key string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.files[key]
}

func newRelayFakeUpstream(t *testing.T) *relayFakeUpstream {
	t.Helper()
	fake := &relayFakeUpstream{fields: map[string]string{}, files: map[string]string{}}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("x-request-id", "up-123")
		w.Header().Set("x-ratelimit-remaining-requests", "99")
		if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
			if err := r.ParseMultipartForm(8 << 20); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			fake.mu.Lock()
			for key, values := range r.MultipartForm.Value {
				fake.fields[key] = values[0]
			}
			for key, headers := range r.MultipartForm.File {
				file, _ := headers[0].Open()
				data, _ := io.ReadAll(file)
				_ = file.Close()
				fake.files[key] = string(data)
			}
			fake.mu.Unlock()
		}
		var payload struct {
			Prompt string `json:"prompt"`
		}
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") && strings.HasSuffix(r.URL.Path, "/images/generations") {
			_ = json.NewDecoder(r.Body).Decode(&payload)
		}
		switch {
		case strings.Contains(payload.Prompt, "slow cat"):
			select {
			case <-r.Context().Done():
			case <-time.After(10 * time.Second):
			}
			_, _ = w.Write([]byte(`{"created":1,"data":[{"b64_json":"AAAA"}]}`))
		case strings.Contains(payload.Prompt, "slowish"):
			time.Sleep(450 * time.Millisecond)
			if strings.Contains(payload.Prompt, "reject") {
				w.WriteHeader(400)
				_, _ = w.Write([]byte(`{"error":{"message":"unsafe prompt"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"created":1,"data":[{"b64_json":"AAAA"}]}`))
		case strings.Contains(payload.Prompt, "reject me"):
			w.WriteHeader(400)
			_, _ = w.Write([]byte(`{"error":{"message":"unsafe prompt"}}`))
		case strings.HasSuffix(r.URL.Path, "/images/generations"):
			_, _ = w.Write([]byte(`{"created":1,"data":[{"b64_json":"AAAA"}]}`))
		case strings.HasSuffix(r.URL.Path, "/audio/speech"):
			w.Header().Set("Content-Type", "audio/mpeg")
			_, _ = w.Write([]byte("MP3DATA"))
		case strings.HasSuffix(r.URL.Path, "/images/edits"):
			_, _ = w.Write([]byte(`{"created":1,"data":[{"b64_json":"AAAA"}]}`))
		case strings.HasSuffix(r.URL.Path, "/audio/transcriptions"):
			_, _ = w.Write([]byte(`{"text":"hello transcript","usage":{"type":"duration","seconds":90}}`))
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			var req struct {
				Model  string `json:"model"`
				Stream bool   `json:"stream"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			usage := `"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}`
			if !req.Stream {
				_, _ = fmt.Fprintf(w, `{"id":"c","object":"chat.completion","model":%q,"choices":[{"index":0,`+
					`"message":{"role":"assistant","content":"Hello"},"finish_reason":"stop"}],%s}`, req.Model, usage)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"model\":%q,"+
				"\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hel\"},\"finish_reason\":null}]}\n\n", req.Model)
			if req.Model == "up-cut" {
				return // 连接半路断开：没有 finish_reason，也没有 [DONE]
			}
			_, _ = fmt.Fprintf(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"model\":%q,"+
				"\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n", req.Model)
			_, _ = fmt.Fprintf(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"model\":%q,\"choices\":[],%s}\n\n", req.Model, usage)
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.server.Close)
	return fake
}
