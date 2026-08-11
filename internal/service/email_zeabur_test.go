package service

import (
	emaildomain "aegis/internal/domain/email"
	apperrors "aegis/pkg/errors"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

func signZeaburPayload(secret string, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyZeaburWebhookSignature(t *testing.T) {
	const secret = "whsec_test_key"
	body := []byte(`{"event":"delivery","email":{"id":"abc123"}}`)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	signature := signZeaburPayload(secret, timestamp, body)

	if err := verifyZeaburWebhookSignature(secret, timestamp, signature, body); err != nil {
		t.Fatalf("合法签名应通过校验，实际: %v", err)
	}

	// 不带 sha256= 前缀的裸 hex 也应接受（不同 SDK 版本写法有差异）
	bare := strings.TrimPrefix(signature, "sha256=")
	if err := verifyZeaburWebhookSignature(secret, timestamp, bare, body); err != nil {
		t.Fatalf("裸 hex 签名应通过校验，实际: %v", err)
	}

	// 正文被改动一个字节即应失败：这正是签名要防的事
	tampered := []byte(`{"event":"delivery","email":{"id":"abc124"}}`)
	if err := verifyZeaburWebhookSignature(secret, timestamp, signature, tampered); err == nil {
		t.Fatal("篡改正文后签名仍通过，验签形同虚设")
	}

	if err := verifyZeaburWebhookSignature("another-secret", timestamp, signature, body); err == nil {
		t.Fatal("错误密钥不应通过校验")
	}
}

func TestVerifyZeaburWebhookSignatureRejectsReplay(t *testing.T) {
	const secret = "whsec_test_key"
	body := []byte(`{"event":"open"}`)
	// 一小时前的合法签名：签名本身没问题，但超出容忍窗口，必须按重放拒绝。
	stale := strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10)
	signature := signZeaburPayload(secret, stale, body)

	err := verifyZeaburWebhookSignature(secret, stale, signature, body)
	if err == nil {
		t.Fatal("超出时间窗口的回调应被拒绝")
	}
	var appErr *apperrors.AppError
	if !errors.As(err, &appErr) || appErr.HTTPStatus != http.StatusUnauthorized {
		t.Fatalf("重放应返回 401，实际: %v", err)
	}
}

func TestVerifyZeaburWebhookSignatureRequiresHeaders(t *testing.T) {
	body := []byte(`{}`)
	if err := verifyZeaburWebhookSignature("secret", "", "sha256=deadbeef", body); err == nil {
		t.Fatal("缺时间戳应被拒绝")
	}
	if err := verifyZeaburWebhookSignature("secret", strconv.FormatInt(time.Now().Unix(), 10), "", body); err == nil {
		t.Fatal("缺签名应被拒绝")
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	if err := verifyZeaburWebhookSignature("secret", timestamp, "md5=abcd", body); err == nil {
		t.Fatal("非 sha256 算法应被拒绝")
	}
}

func TestBuildZeaburDeliveryUpdateMapsEvents(t *testing.T) {
	newEvent := func(id string) emaildomain.WebhookEvent {
		var event emaildomain.WebhookEvent
		event.Email.ID = id
		event.Timestamp = time.Now().UTC().Format(time.RFC3339)
		event.Data = map[string]any{"bounce_type": "Permanent"}
		return event
	}

	cases := []struct {
		event         string
		wantStatus    string
		wantDelivered bool
		wantOpen      bool
		wantClick     bool
	}{
		{emaildomain.EventSend, emaildomain.DeliveryStatusSent, false, false, false},
		{emaildomain.EventDelivery, emaildomain.DeliveryStatusDelivered, true, false, false},
		{emaildomain.EventBounce, emaildomain.DeliveryStatusBounced, false, false, false},
		{emaildomain.EventComplaint, emaildomain.DeliveryStatusComplained, false, false, false},
		{emaildomain.EventReject, emaildomain.DeliveryStatusRejected, false, false, false},
		// open / click 只累计计数，不得改动主状态
		{emaildomain.EventOpen, "", false, true, false},
		{emaildomain.EventClick, "", false, false, true},
	}
	for _, tc := range cases {
		update, ok := buildZeaburDeliveryUpdate(tc.event, newEvent("mail-1"))
		if !ok {
			t.Fatalf("%s 事件应被接受", tc.event)
		}
		if update.Status != tc.wantStatus {
			t.Errorf("%s: 状态期望 %q，实际 %q", tc.event, tc.wantStatus, update.Status)
		}
		if update.MarkDelivered != tc.wantDelivered {
			t.Errorf("%s: MarkDelivered 期望 %v", tc.event, tc.wantDelivered)
		}
		if update.IncrementOpen != tc.wantOpen || update.IncrementClick != tc.wantClick {
			t.Errorf("%s: 计数标记不符", tc.event)
		}
		if update.ProviderMessageID != "mail-1" {
			t.Errorf("%s: 未带上服务商邮件 ID", tc.event)
		}
	}

	if _, ok := buildZeaburDeliveryUpdate("rendering_failure", newEvent("mail-1")); ok {
		t.Error("未知事件应被忽略，而不是当成状态推进")
	}
	if _, ok := buildZeaburDeliveryUpdate(emaildomain.EventDelivery, emaildomain.WebhookEvent{}); ok {
		t.Error("缺少邮件 ID 时无法定位记录，应忽略")
	}
}

func TestClassifyZeaburHTTPError(t *testing.T) {
	cases := []struct {
		statusCode int
		body       string
		wantStatus int
		wantRetry  bool
	}{
		{http.StatusBadRequest, `{"error":"validation error","message":"subject too long"}`, http.StatusBadRequest, false},
		{http.StatusUnauthorized, `{"error":"unauthorized"}`, http.StatusUnauthorized, false},
		{http.StatusForbidden, `{"error":"permission denied"}`, http.StatusForbidden, false},
		// 配额耗尽要等 UTC 次日重置，重试毫无意义
		{http.StatusTooManyRequests, `{"error":"daily quota exceeded (55/55)"}`, http.StatusTooManyRequests, false},
		{http.StatusBadGateway, `{"error":"upstream"}`, http.StatusBadGateway, true},
		{http.StatusInternalServerError, `boom`, http.StatusBadGateway, true},
	}
	for _, tc := range cases {
		err := classifyZeaburHTTPError(tc.statusCode, []byte(tc.body))
		var appErr *apperrors.AppError
		if !errors.As(err, &appErr) {
			t.Fatalf("HTTP %d 应映射为 AppError", tc.statusCode)
		}
		if appErr.HTTPStatus != tc.wantStatus {
			t.Errorf("HTTP %d: 期望映射到 %d，实际 %d", tc.statusCode, tc.wantStatus, appErr.HTTPStatus)
		}
		if got := zeaburShouldRetry(err); got != tc.wantRetry {
			t.Errorf("HTTP %d: 重试判定期望 %v，实际 %v", tc.statusCode, tc.wantRetry, got)
		}
	}
}

func TestClassifyZeaburHTTPErrorKeepsUpstreamDetail(t *testing.T) {
	err := classifyZeaburHTTPError(http.StatusBadRequest,
		[]byte(`{"error":"validation error","message":"subject length (1200) exceeds maximum (998 characters)"}`))
	if !strings.Contains(err.Error(), "998") {
		t.Fatalf("上游细节应保留在错误里，便于直接定位，实际: %v", err)
	}
}

func TestZeaburEndpointFallsBackToDefault(t *testing.T) {
	if got := zeaburEndpoint("", zeaburSendPath); got != emaildomain.ZeaburDefaultBaseURL+"/emails" {
		t.Fatalf("空配置应回落到默认地址，实际: %s", got)
	}
	if got := zeaburEndpoint("https://api.example.com/api/v1/zsend/", zeaburSendPath); got != "https://api.example.com/api/v1/zsend/emails" {
		t.Fatalf("自定义地址尾部斜杠应被归一化，实际: %s", got)
	}
}

func TestFormatZeaburFrom(t *testing.T) {
	if got := formatZeaburFrom("noreply@example.com", ""); got != "noreply@example.com" {
		t.Fatalf("无发件人名应只给地址，实际: %s", got)
	}
	got := formatZeaburFrom("noreply@example.com", "Aegis 平台")
	if !strings.Contains(got, "<noreply@example.com>") {
		t.Fatalf("带名字时应为 RFC 5322 形式，实际: %s", got)
	}
}

func TestBuildZeaburTagsMergesPurpose(t *testing.T) {
	tags := buildZeaburTags(map[string]string{"env": "prod"}, "register")
	if tags["env"] != "prod" || tags["purpose"] != "register" {
		t.Fatalf("用途应并入标签，实际: %v", tags)
	}
	// 用户显式配置的 purpose 不应被业务用途覆盖
	tags = buildZeaburTags(map[string]string{"purpose": "custom"}, "register")
	if tags["purpose"] != "custom" {
		t.Fatalf("显式配置的标签优先级更高，实际: %v", tags)
	}
	if buildZeaburTags(nil, "") != nil {
		t.Fatal("无标签时应返回 nil，避免发送空对象")
	}
}

func TestZeaburSenderValidate(t *testing.T) {
	sender := newZeaburEmailSender(zap.NewNop())

	err := sender.Validate(emaildomain.Config{Provider: emaildomain.ProviderZeabur})
	if err == nil {
		t.Fatal("缺 API Key 应校验失败")
	}

	// 编辑既有配置时前端不回传明文 Key，仅凭密文也应判定为已配置
	config := emaildomain.Config{
		Provider: emaildomain.ProviderZeabur,
		Zeabur: emaildomain.ZeaburConfig{
			APIKeyCipher: "cipher-text",
			FromAddress:  "noreply@example.com",
		},
	}
	if err := sender.Validate(config); err != nil {
		t.Fatalf("已有密文的配置应通过校验，实际: %v", err)
	}

	config.Zeabur.FromAddress = "not-an-email"
	if err := sender.Validate(config); err == nil {
		t.Fatal("非法发件地址应校验失败")
	}

	config.Zeabur.FromAddress = "noreply@example.com"
	config.Zeabur.BaseURL = "http://api.example.com"
	if err := sender.Validate(config); err == nil {
		t.Fatal("非 https 的 API 地址应被拒绝")
	}
}

func TestHTMLToPlainText(t *testing.T) {
	source := `<html><head><style>p{color:red}</style><title>忽略我</title></head>
<body><h1>验证码</h1><p>您的验证码是 <b>123456</b>，5 分钟内有效。</p>
<script>alert(1)</script></body></html>`
	text := htmlToPlainText(source)

	for _, unwanted := range []string{"alert(1)", "color:red", "忽略我", "<"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("纯文本不应包含 %q，实际: %q", unwanted, text)
		}
	}
	for _, wanted := range []string{"验证码", "123456"} {
		if !strings.Contains(text, wanted) {
			t.Errorf("纯文本应保留 %q，实际: %q", wanted, text)
		}
	}
}

func TestNormalizeEmailProviderDefaultsToSMTP(t *testing.T) {
	if got := normalizeEmailProvider(""); got != emaildomain.ProviderSMTP {
		t.Fatalf("空 provider 应回落到 smtp，实际: %s", got)
	}
	if got := normalizeEmailProvider("  ZEABUR "); got != emaildomain.ProviderZeabur {
		t.Fatalf("provider 应大小写无关并去空白，实际: %s", got)
	}
}

func TestSanitizeEmailConfigStripsSecrets(t *testing.T) {
	config := emaildomain.Config{
		Provider: emaildomain.ProviderZeabur,
		SMTP:     emaildomain.SMTPConfig{Password: "smtp-secret"},
		Zeabur: emaildomain.ZeaburConfig{
			APIKey:              "zb_live_key",
			APIKeyCipher:        "cipher",
			WebhookSecret:       "whsec",
			WebhookSecretCipher: "cipher2",
		},
	}
	sanitizeEmailConfig(&config)

	if config.SMTP.Password != "" || config.Zeabur.APIKey != "" || config.Zeabur.APIKeyCipher != "" ||
		config.Zeabur.WebhookSecret != "" || config.Zeabur.WebhookSecretCipher != "" {
		t.Fatalf("密钥不得随响应出网，实际: %+v", config)
	}
	if !config.Zeabur.APIKeySet || !config.Zeabur.WebhookSecretSet {
		t.Fatal("脱敏后仍应保留「已配置」的布尔位，否则前端无法区分未配置与不回显")
	}
}

func TestSenderIdentityFollowsProvider(t *testing.T) {
	config := emaildomain.Config{
		Provider: emaildomain.ProviderZeabur,
		SMTP:     emaildomain.SMTPConfig{FromAddress: "smtp@example.com"},
		Zeabur:   emaildomain.ZeaburConfig{FromAddress: "zeabur@example.com", FromName: "Aegis"},
	}
	from, name, _ := config.SenderIdentity()
	if from != "zeabur@example.com" || name != "Aegis" {
		t.Fatalf("zeabur 配置应取 Zeabur 段的发件人，实际: %s / %s", from, name)
	}

	config.Provider = emaildomain.ProviderSMTP
	if from, _, _ = config.SenderIdentity(); from != "smtp@example.com" {
		t.Fatalf("smtp 配置应取 SMTP 段的发件人，实际: %s", from)
	}
}

func TestDescribeZeaburFailure(t *testing.T) {
	message := describeZeaburFailure(map[string]any{"bounce_type": "Permanent", "smtp_response": "550 5.1.1"}, "邮件被退回")
	if !strings.Contains(message, "邮件被退回") || !strings.Contains(message, "Permanent") {
		t.Fatalf("退信原因应带上上游细节，实际: %s", message)
	}
	if got := describeZeaburFailure(nil, "邮件被退回"); got != "邮件被退回" {
		t.Fatalf("无细节时应保持兜底文案，实际: %s", got)
	}
}

func TestSMTPBlockedHintMentionsZeabur(t *testing.T) {
	// SMTP 出站被平台封禁时表现就是纯超时，提示必须点破根因，
	// 否则排查会一路走偏到邮箱服务商那边。
	hint := smtpBlockedHint("smtp.example.com:587")
	if !strings.Contains(hint, "Zeabur") || !strings.Contains(hint, "zeabur") {
		t.Fatalf("超时提示应指向 Zeabur 渠道，实际: %s", hint)
	}
}

func TestCheckZeaburTimestampAcceptsRFC3339(t *testing.T) {
	if err := checkZeaburTimestamp(time.Now().UTC().Format(time.RFC3339)); err != nil {
		t.Fatalf("RFC3339 时间戳应被接受，实际: %v", err)
	}
	if err := checkZeaburTimestamp("not-a-timestamp"); err == nil {
		t.Fatal("非法时间戳应被拒绝")
	}
	if err := checkZeaburTimestamp(fmt.Sprintf("%d", time.Now().Add(2*time.Hour).Unix())); err == nil {
		t.Fatal("未来过远的时间戳同样应被拒绝")
	}
}
