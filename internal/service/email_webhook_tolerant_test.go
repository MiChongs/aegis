package service

import (
	emaildomain "aegis/internal/domain/email"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/mailgun/mailgun-go/v5/events"
)

// ── 回调令牌（不签名的三家共用的准入）──

func tokenTestRequest(query string, headers map[string]string) ProviderWebhookRequest {
	parsed, _ := url.ParseQuery(query)
	header := http.Header{}
	for key, value := range headers {
		header.Set(key, value)
	}
	return ProviderWebhookRequest{Query: parsed, Headers: header}
}

// 令牌可以从三个地方来，任一命中即放行。
// 少任何一个来源都会挡住一类真实接法：阿里云 / 腾讯云只能给一个 URL，
// Postmark 习惯用 Basic Auth，自建转发层则更愿意走请求头。
func TestVerifyWebhookTokenAcceptsAllThreeSources(t *testing.T) {
	t.Parallel()

	service := newCatalogTestService(t)
	config := &emaildomain.Config{
		Name:     "default",
		Provider: emaildomain.ProviderTencent,
		Secrets:  map[string]string{emaildomain.KeyWebhookSecret: "s3cr3t-token"},
	}

	cases := map[string]ProviderWebhookRequest{
		"query":  tokenTestRequest("token=s3cr3t-token", nil),
		"header": tokenTestRequest("", map[string]string{AegisWebhookTokenHeader: "s3cr3t-token"}),
		// Basic Auth 只认密码位：用户名是管理员随手填的，
		// 拿它当凭据的一部分只会制造一类查不出的失败。
		"basic": tokenTestRequest("", map[string]string{
			"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte("whoever:s3cr3t-token")),
		}),
	}
	for name, request := range cases {
		if err := service.verifyWebhookToken(config, request, "腾讯云"); err != nil {
			t.Errorf("%s 来源的令牌应被接受，实际: %v", name, err)
		}
	}
}

func TestVerifyWebhookTokenRejectsWrongAndMissing(t *testing.T) {
	t.Parallel()

	service := newCatalogTestService(t)
	config := &emaildomain.Config{
		Name:     "default",
		Provider: emaildomain.ProviderPostmark,
		Secrets:  map[string]string{emaildomain.KeyWebhookSecret: "s3cr3t-token"},
	}

	for name, request := range map[string]ProviderWebhookRequest{
		"错误令牌":  tokenTestRequest("token=wrong", nil),
		"空令牌":   tokenTestRequest("token=", nil),
		"完全没带":  tokenTestRequest("", nil),
		"只带用户名": tokenTestRequest("", map[string]string{"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte("s3cr3t-token:"))}),
	} {
		if err := service.verifyWebhookToken(config, request, "Postmark"); err == nil {
			t.Errorf("%s 应被拒绝", name)
		}
	}

	// 没配令牌时一律拒收：不拒的话，任何人都能往这条地址上灌伪造的回执，
	// 而伪造一条 delivered 就能把一封退信的邮件显示成送达。
	unconfigured := &emaildomain.Config{Name: "default", Provider: emaildomain.ProviderPostmark}
	err := service.verifyWebhookToken(unconfigured, tokenTestRequest("token=anything", nil), "Postmark")
	if err == nil || !strings.Contains(err.Error(), "回调令牌") {
		t.Fatalf("未配置令牌时应拒收并说清要配什么，实际: %v", err)
	}
}

// ── Mailgun ──

func mailgunEvent(t *testing.T, raw string) events.Event {
	t.Helper()
	event, err := events.ParseEvent([]byte(raw))
	if err != nil {
		t.Fatalf("解析 Mailgun 事件失败：%v", err)
	}
	return event
}

func TestMailgunDeliveryUpdateNormalizesEvents(t *testing.T) {
	t.Parallel()

	const headers = `"message":{"headers":{"message-id":"20260101.abc@mg.example.com"}}`

	delivered := mailgunEvent(t, `{"event":"delivered","timestamp":1767225600,`+headers+`}`)
	update, ok := buildMailgunDeliveryUpdate(delivered)
	if !ok || update.Status != emaildomain.DeliveryStatusDelivered || !update.MarkDelivered {
		t.Fatalf("delivered 应推进到已送达：%+v", update)
	}
	// 发信时留痕存的是**去掉尖括号**的形态，事件里那份本来就不带尖括号 ——
	// 两边对不上的表现是「webhook 明明在推，投递记录永远停在已发送」。
	if update.ProviderMessageID != "20260101.abc@mg.example.com" {
		t.Fatalf("关联键取错：%q", update.ProviderMessageID)
	}

	permanent := mailgunEvent(t, `{"event":"failed","severity":"permanent","reason":"suppress-bounce","timestamp":1767225600,`+
		`"delivery-status":{"message":"550 5.1.1 unknown"},`+headers+`}`)
	update, ok = buildMailgunDeliveryUpdate(permanent)
	if !ok || update.Status != emaildomain.DeliveryStatusBounced || !strings.Contains(update.ErrorMessage, "suppress-bounce") {
		t.Fatalf("permanent 失败应推进到已退信并带上原因：%+v", update)
	}

	// severity=temporary 之后仍会重投；写成终态会让一封最终送达的信
	// 永远显示成退信（状态是单向推进的）。
	temporary := mailgunEvent(t, `{"event":"failed","severity":"temporary","reason":"greylisted","timestamp":1767225600,`+headers+`}`)
	update, ok = buildMailgunDeliveryUpdate(temporary)
	if !ok || update.Status != "" || update.Event != "delay" {
		t.Fatalf("temporary 失败不该改主状态：%+v", update)
	}

	opened := mailgunEvent(t, `{"event":"opened","timestamp":1767225600,`+headers+`}`)
	update, ok = buildMailgunDeliveryUpdate(opened)
	if !ok || !update.IncrementOpen || update.Status != "" {
		t.Fatalf("opened 只该累加打开数：%+v", update)
	}

	// 与投递状态无关的事件静默忽略，不该被当成未知错误。
	unsubscribed := mailgunEvent(t, `{"event":"unsubscribed","timestamp":1767225600,`+headers+`}`)
	if _, ok := buildMailgunDeliveryUpdate(unsubscribed); ok {
		t.Fatal("退订事件不该产生状态推进")
	}
}

// ── Postmark ──

func TestPostmarkDeliveryUpdateSeparatesTransientBounces(t *testing.T) {
	t.Parallel()

	update, ok := buildPostmarkDeliveryUpdate(postmarkWebhookEvent{
		RecordType: "Delivery", MessageID: "pm-1", DeliveredAt: "2026-01-01T00:00:00Z",
	})
	if !ok || update.Status != emaildomain.DeliveryStatusDelivered || !update.MarkDelivered {
		t.Fatalf("Delivery 应推进到已送达：%+v", update)
	}

	update, ok = buildPostmarkDeliveryUpdate(postmarkWebhookEvent{
		RecordType: "Bounce", MessageID: "pm-2", Type: "HardBounce",
		Description: "The server was unable to deliver your message",
	})
	if !ok || update.Status != emaildomain.DeliveryStatusBounced ||
		!strings.Contains(update.ErrorMessage, "HardBounce") {
		t.Fatalf("硬退信应推进到已退信并带上类型：%+v", update)
	}

	// 软退信之后仍可能送达，因此只记事件不改主状态。
	update, ok = buildPostmarkDeliveryUpdate(postmarkWebhookEvent{
		RecordType: "Bounce", MessageID: "pm-3", Type: "SoftBounce", Description: "Mailbox full",
	})
	if !ok || update.Status != "" || update.Event != "delay" {
		t.Fatalf("软退信不该改主状态：%+v", update)
	}

	update, ok = buildPostmarkDeliveryUpdate(postmarkWebhookEvent{RecordType: "SpamComplaint", MessageID: "pm-4"})
	if !ok || update.Status != emaildomain.DeliveryStatusComplained {
		t.Fatalf("投诉应推进到被投诉：%+v", update)
	}

	if _, ok := buildPostmarkDeliveryUpdate(postmarkWebhookEvent{RecordType: "SubscriptionChange", MessageID: "pm-5"}); ok {
		t.Fatal("订阅变更与投递状态无关，不该产生状态推进")
	}
	if _, ok := buildPostmarkDeliveryUpdate(postmarkWebhookEvent{RecordType: "Delivery"}); ok {
		t.Fatal("缺少 MessageID 的回执无法关联，应被忽略")
	}
}

// ── 阿里云 / 腾讯云：拼写容忍 ──

// 阿里云的回执字段在不同投递方式与文档版本里有三种拼写。
// 钉死一种的代价是换个投递方式就一条也匹配不上，而那个失败是静默的
// （HTTP 200，零条匹配）—— 因此这条测试逐种拼写都跑一遍。
func TestAliyunDeliveryUpdateToleratesFieldSpellings(t *testing.T) {
	t.Parallel()

	variants := []string{
		`{"envId":"env-1","status":"0","time":1767225600}`,
		`{"EnvId":"env-1","Status":"0","Time":1767225600}`,
		`{"env_id":"env-1","status":0,"timestamp":1767225600}`,
	}
	for _, raw := range variants {
		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			t.Fatalf("样例报文不合法：%v", err)
		}
		update, ok := buildAliyunDeliveryUpdate(payload)
		if !ok || update.ProviderMessageID != "env-1" || update.Status != emaildomain.DeliveryStatusDelivered {
			t.Fatalf("%s 应解出送达：%+v", raw, update)
		}
	}

	// 没有事件名时回落到状态码：非 0 即失败，且原因要带上。
	var failed map[string]any
	_ = json.Unmarshal([]byte(`{"envId":"env-2","status":"3","message":"user unknown"}`), &failed)
	update, ok := buildAliyunDeliveryUpdate(failed)
	if !ok || update.Status != emaildomain.DeliveryStatusBounced ||
		!strings.Contains(update.ErrorMessage, "user unknown") ||
		!strings.Contains(update.ErrorMessage, "3") {
		t.Fatalf("非 0 状态码应判退信并带上原因：%+v", update)
	}

	// 有显式事件名时以事件名为准。
	var opened map[string]any
	_ = json.Unmarshal([]byte(`{"envId":"env-3","messageEventType":"Opened"}`), &opened)
	update, ok = buildAliyunDeliveryUpdate(opened)
	if !ok || !update.IncrementOpen || update.Status != "" {
		t.Fatalf("Opened 只该累加打开数：%+v", update)
	}

	// 既没有事件名也没有状态码时认不出来，必须如实返回 false 让上层记日志，
	// 而不是猜一个状态写进留痕。
	var unknown map[string]any
	_ = json.Unmarshal([]byte(`{"envId":"env-4","somethingElse":"x"}`), &unknown)
	if _, ok := buildAliyunDeliveryUpdate(unknown); ok {
		t.Fatal("认不出的报文不该被猜成某个状态")
	}
}

func TestTencentDeliveryUpdateSeparatesSoftBounce(t *testing.T) {
	t.Parallel()

	var delivered map[string]any
	_ = json.Unmarshal([]byte(`{"message_id":"tc-1","event_type":"delivered","timestamp":1767225600}`), &delivered)
	update, ok := buildTencentDeliveryUpdate(delivered)
	if !ok || update.Status != emaildomain.DeliveryStatusDelivered {
		t.Fatalf("delivered 应推进到已送达：%+v", update)
	}

	var hard map[string]any
	_ = json.Unmarshal([]byte(`{"MessageId":"tc-2","EventType":"bounced","bounce_type":"HardBounce","bounce_reason":"no such user"}`), &hard)
	update, ok = buildTencentDeliveryUpdate(hard)
	if !ok || update.Status != emaildomain.DeliveryStatusBounced ||
		!strings.Contains(update.ErrorMessage, "no such user") {
		t.Fatalf("硬退信应推进到已退信：%+v", update)
	}

	var soft map[string]any
	_ = json.Unmarshal([]byte(`{"message_id":"tc-3","event_type":"bounced","bounce_type":"SoftBounce"}`), &soft)
	update, ok = buildTencentDeliveryUpdate(soft)
	if !ok || update.Status != "" || update.Event != "delay" {
		t.Fatalf("软退信不该改主状态：%+v", update)
	}
}

// ── 共用解析工具 ──

// 阿里云与腾讯云都可能一次推一条或一批，两种形状在文档里都出现过。
// 只认其中一种的代价是另一种全军覆没。
func TestDecodeWebhookPayloadsAcceptsObjectAndArray(t *testing.T) {
	t.Parallel()

	single, err := decodeWebhookPayloads([]byte(`{"messageId":"a"}`))
	if err != nil || len(single) != 1 {
		t.Fatalf("单个对象应解出一条：%v / %v", single, err)
	}
	batch, err := decodeWebhookPayloads([]byte(`[{"messageId":"a"},{"messageId":"b"}]`))
	if err != nil || len(batch) != 2 {
		t.Fatalf("数组应解出两条：%v / %v", batch, err)
	}
	if _, err := decodeWebhookPayloads([]byte("   ")); err == nil {
		t.Fatal("空报文应报错")
	}
	if _, err := decodeWebhookPayloads([]byte("not json")); err == nil {
		t.Fatal("非法报文应报错")
	}
}

func TestLookupStringIsSpellingTolerant(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"Message_Id": "m-1",
		"Timestamp":  float64(1767225600),
		"ratio":      1.5,
		"data":       map[string]any{"bounceReason": "mailbox full"},
	}
	if got := lookupString(payload, "messageId"); got != "m-1" {
		t.Fatalf("应忽略大小写与下划线，实际: %q", got)
	}
	// 时间戳是最常见的数值字段，走 %v 会拼出 1.7672256e+09 这种科学计数法，
	// 那个字符串没有任何一个时间解析器认得。
	if got := lookupString(payload, "timestamp"); got != "1767225600" {
		t.Fatalf("整数不该被格式化成科学计数法，实际: %q", got)
	}
	if got := lookupString(payload, "ratio"); got != "1.5" {
		t.Fatalf("小数应原样给出，实际: %q", got)
	}
	// 下钻一层：不少服务商把正文裹在 data 里。
	if got := lookupString(payload, "bounceReason"); got != "mailbox full" {
		t.Fatalf("应能从 data 里取到，实际: %q", got)
	}
	if got := lookupString(payload, "missing"); got != "" {
		t.Fatalf("取不到应返回空串，实际: %q", got)
	}
}

func TestNormalizeEventTokenStripsNamespace(t *testing.T) {
	t.Parallel()

	for input, want := range map[string]string{
		"email.delivered": "delivered",
		"Email_Delivered": "delivered",
		"DELIVERED":       "delivered",
		"  bounced  ":     "bounced",
		"":                "",
	} {
		if got := normalizeEventToken(input); got != want {
			t.Errorf("normalizeEventToken(%q) = %q，期望 %q", input, got, want)
		}
	}
}

// 认不出的报文要留下键名线索：这类回执在界面上完全看不出「来过」，
// 只有日志里那一行能说明「东西到了，但字段对不上」。
func TestPayloadKeysAreSortedForStableLogging(t *testing.T) {
	t.Parallel()

	keys := payloadKeys(map[string]any{"zeta": 1, "alpha": 2, "mid": 3})
	if strings.Join(keys, ",") != "alpha,mid,zeta" {
		t.Fatalf("键名应排序输出以便逐次对比，实际: %v", keys)
	}
}
