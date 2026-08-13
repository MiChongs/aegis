package service

import (
	emaildomain "aegis/internal/domain/email"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"strconv"
	"strings"
	"testing"
	"time"
)

// 发信留痕存的是响应头 X-Message-Id（`abc123`），而事件里的 sg_message_id 是
// `abc123.filterdrecv-...`。不截断的话**每一条**回执都匹配不到留痕，
// 表现为「webhook 明明在推，投递记录永远停在已发送」——
// 而那种失败没有任何报错，只能靠盯着这个函数防住。
func TestSendGridMessageKeyStripsEventSuffix(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"abc123.filterdrecv-p3mdw1-756b745b58-c6d4f-18-5F6E1B9A-2A.0": "abc123",
		"abc123": "abc123",
		"  abc123.suffix  ": "abc123",
		"": "",
	}
	for input, want := range cases {
		if got := sendgridMessageKey(input); got != want {
			t.Errorf("sendgridMessageKey(%q) = %q，期望 %q", input, got, want)
		}
	}
}

// SNS 验签的**安全根**是主机名校验：不校验的话，攻击者只要把 SigningCertURL
// 指向自己的服务器，就能用自己的私钥签出一条「通过验签」的回执。
func TestSNSCertificateHostIsRestrictedToAWS(t *testing.T) {
	t.Parallel()

	allowed := []string{
		"sns.us-east-1.amazonaws.com",
		"sns.ap-southeast-1.amazonaws.com",
		"sns.cn-north-1.amazonaws.com.cn",
	}
	for _, host := range allowed {
		if !snsCertHost.MatchString(host) {
			t.Errorf("%s 是合法的 AWS SNS 域名，不该被拒", host)
		}
	}
	rejected := []string{
		"sns.us-east-1.amazonaws.com.evil.test",
		"evil.test",
		"sns.amazonaws.com.attacker.io",
		"notsns.us-east-1.amazonaws.com",
		"sns.us-east-1.amazonaws.com:8443",
	}
	for _, host := range rejected {
		if snsCertHost.MatchString(host) {
			t.Errorf("%s 不是 AWS SNS 域名，必须拒收", host)
		}
	}
}

// 待签名串的字段集合按消息类型不同，缺一个或多一个都会验不过 ——
// 而验不过与「密钥填错了」在日志里长得一模一样。
func TestSNSStringToSignFollowsSpec(t *testing.T) {
	t.Parallel()

	notification := snsEnvelope{
		Type:      "Notification",
		MessageID: "msg-1",
		TopicArn:  "arn:aws:sns:us-east-1:1:topic",
		Message:   `{"eventType":"Delivery"}`,
		Timestamp: "2026-01-01T00:00:00.000Z",
		// 订阅确认才有的字段，出现在 Notification 里必须被忽略
		Token:        "should-be-ignored",
		SubscribeURL: "https://sns.us-east-1.amazonaws.com/?Action=ConfirmSubscription",
	}
	got := string(snsStringToSign(notification))
	if strings.Contains(got, "Token") || strings.Contains(got, "SubscribeURL") {
		t.Fatalf("Notification 的待签名串不该含订阅确认字段：\n%s", got)
	}
	want := "Message\n" + notification.Message + "\n" +
		"MessageId\nmsg-1\n" +
		"Timestamp\n2026-01-01T00:00:00.000Z\n" +
		"TopicArn\narn:aws:sns:us-east-1:1:topic\n" +
		"Type\nNotification\n"
	if got != want {
		t.Fatalf("待签名串不符合规格：\n得到 %q\n期望 %q", got, want)
	}

	confirmation := notification
	confirmation.Type = "SubscriptionConfirmation"
	confirmed := string(snsStringToSign(confirmation))
	for _, field := range []string{"Token", "SubscribeURL"} {
		if !strings.Contains(confirmed, field) {
			t.Errorf("订阅确认的待签名串缺少 %s", field)
		}
	}
	if strings.Contains(confirmed, "Subject") {
		t.Error("订阅确认的待签名串不含 Subject")
	}
}

// SendGrid 的验签是 ECDSA P-256（不是 HMAC），签名覆盖「时间戳直接拼上原始报文」。
// 这里用真实密钥对跑一遍，防止签名拼接方式被顺手改成带分隔符。
func TestVerifySendGridSignature(t *testing.T) {
	t.Parallel()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("生成测试密钥失败：%v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatalf("编码公钥失败：%v", err)
	}
	publicKey := base64.StdEncoding.EncodeToString(der)

	body := []byte(`[{"event":"delivered","sg_message_id":"abc.suffix"}]`)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	digest := sha256.Sum256(append([]byte(timestamp), body...))
	signature, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatalf("签名失败：%v", err)
	}
	encoded := base64.StdEncoding.EncodeToString(signature)

	if err := verifySendGridSignature(publicKey, timestamp, encoded, body); err != nil {
		t.Fatalf("合法签名应通过校验，实际: %v", err)
	}
	// 报文被改一个字节就必须验不过 —— 这正是签名的全部意义。
	if err := verifySendGridSignature(publicKey, timestamp, encoded, append(body, ' ')); err == nil {
		t.Fatal("报文被篡改后签名仍然通过")
	}
	// 时间戳参与签名，因此换一个时间戳同样验不过（也顺带挡住重放）。
	if err := verifySendGridSignature(publicKey, strconv.FormatInt(time.Now().Unix()-1, 10), encoded, body); err == nil {
		t.Fatal("时间戳被改后签名仍然通过")
	}
	// 超出容忍窗口的时间戳，即使签名自洽也要拒收。
	stale := strconv.FormatInt(time.Now().Add(-2*time.Hour).Unix(), 10)
	staleDigest := sha256.Sum256(append([]byte(stale), body...))
	staleSignature, _ := ecdsa.SignASN1(rand.Reader, key, staleDigest[:])
	if err := verifySendGridSignature(publicKey, stale, base64.StdEncoding.EncodeToString(staleSignature), body); err == nil {
		t.Fatal("超出容忍窗口的回调应按重放攻击拒绝")
	}
}

// 各家的事件名不同，必须归一化到同一组状态；且**延迟不是终态** ——
// 把它写成失败会让一封最终送达的信永远显示成失败（状态是单向推进的）。
func TestDeliveryEventsNormalizeAcrossProviders(t *testing.T) {
	t.Parallel()

	ses := sesEventNotification{EventType: "Delivery"}
	ses.Mail.MessageID = "m-1"
	update, ok := buildSESDeliveryUpdate(ses)
	if !ok || update.Status != emaildomain.DeliveryStatusDelivered || !update.MarkDelivered {
		t.Fatalf("SES Delivery 应推进到已送达：%+v", update)
	}

	ses.EventType = "DeliveryDelay"
	update, ok = buildSESDeliveryUpdate(ses)
	if !ok || update.Status != "" {
		t.Fatalf("SES DeliveryDelay 不该改主状态：%+v", update)
	}

	resend := resendWebhookEvent{Type: "email.bounced"}
	resend.Data.EmailID = "r-1"
	update, ok = buildResendDeliveryUpdate(resend)
	if !ok || update.Status != emaildomain.DeliveryStatusBounced {
		t.Fatalf("Resend email.bounced 应推进到已退信：%+v", update)
	}

	update, ok = buildResendDeliveryUpdate(resendWebhookEvent{Type: "email.delivery_delayed"})
	if ok {
		t.Fatalf("缺少 email_id 的事件应被忽略：%+v", update)
	}

	update, ok = buildSendGridDeliveryUpdate(sendgridWebhookEvent{
		Event: "bounce", SGMessageID: "sg-1.suffix", Reason: "550 mailbox unavailable",
	})
	if !ok || update.Status != emaildomain.DeliveryStatusBounced ||
		update.ProviderMessageID != "sg-1" ||
		!strings.Contains(update.ErrorMessage, "550") {
		t.Fatalf("SendGrid bounce 归一化有误：%+v", update)
	}

	// 与投递状态无关的事件（退订）静默忽略，不该被当成未知错误。
	if _, ok := buildSendGridDeliveryUpdate(sendgridWebhookEvent{Event: "unsubscribe", SGMessageID: "sg-2"}); ok {
		t.Fatal("退订事件不该产生状态推进")
	}
}

// 原因为空时不能拼出以冒号结尾的半句话 —— 它会原样进投递记录列表。
func TestWithReasonSkipsEmptyCandidates(t *testing.T) {
	t.Parallel()

	if got := withReason("邮件被退回", "", "  "); got != "邮件被退回" {
		t.Fatalf("全空时应只留说明，实际: %q", got)
	}
	if got := withReason("邮件被退回", "", "550 5.1.1"); got != "邮件被退回：550 5.1.1" {
		t.Fatalf("应取第一个非空原因，实际: %q", got)
	}
}
