package service

import (
	emaildomain "aegis/internal/domain/email"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/sha1" //nolint:gosec // SNS SignatureVersion 1 规定用 SHA1，不是我们的选择
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	mailgunlib "github.com/mailgun/mailgun-go/v5"
	"github.com/mailgun/mailgun-go/v5/events"
	"github.com/mailgun/mailgun-go/v5/mtypes"
	resendlib "github.com/resend/resend-go/v2"
	"go.uber.org/zap"
)

// 各家投递回执的请求头。
const (
	ResendWebhookIDHeader        = "svix-id"
	ResendWebhookTimestampHeader = "svix-timestamp"
	ResendWebhookSignatureHeader = "svix-signature"

	SendGridWebhookSignatureHeader = "X-Twilio-Email-Event-Webhook-Signature"
	SendGridWebhookTimestampHeader = "X-Twilio-Email-Event-Webhook-Timestamp"
)

// ProviderWebhookRequest 是传输层递交上来的原始回调，各服务商共用。
//
// Body 必须是**未经解析的原始字节**：带签名的几家覆盖的都是原文，
// 任何重新序列化（键序、空白、转义差异）都会让验签失败。
type ProviderWebhookRequest struct {
	// AppID 为 0 即平台级通道（emaildomain.PlatformAppID）
	AppID      int64
	ConfigName string
	Headers    http.Header
	// Query 用于取回调令牌。不签名的那三家（Postmark / 阿里云 / 腾讯云）
	// 只能把凭据放在地址里 —— 它们的报文里没有任何可验证的东西。
	Query url.Values
	Body  []byte
}

// ProviderWebhookResult 回执处理结论，给控制台与日志一个可观测的回执。
type ProviderWebhookResult struct {
	Provider string `json:"provider"`
	// Events 本次报文里包含的事件数（SendGrid 一次推一批）
	Events   int    `json:"events"`
	Matched  int    `json:"matched"`
	Ignored  int    `json:"ignored"`
	Received bool   `json:"received"`
	Note     string `json:"note,omitempty"`
}

// ── AWS SES（经 SNS）──

// snsEnvelope 是 SNS 推送的信封。SES 的事件正文在 Message 这个**字符串**字段里，
// 需要二次反序列化 —— 这是 SNS 的既有形状，不是我们的选择。
type snsEnvelope struct {
	Type             string `json:"Type"`
	MessageID        string `json:"MessageId"`
	TopicArn         string `json:"TopicArn"`
	Subject          string `json:"Subject"`
	Message          string `json:"Message"`
	Timestamp        string `json:"Timestamp"`
	SignatureVersion string `json:"SignatureVersion"`
	Signature        string `json:"Signature"`
	SigningCertURL   string `json:"SigningCertURL"`
	SubscribeURL     string `json:"SubscribeURL"`
	Token            string `json:"Token"`
}

// sesEventNotification 是 SES 事件发布的报文。
type sesEventNotification struct {
	EventType string `json:"eventType"`
	Mail      struct {
		MessageID string `json:"messageId"`
		Timestamp string `json:"timestamp"`
	} `json:"mail"`
	Bounce *struct {
		BounceType        string `json:"bounceType"`
		BounceSubType     string `json:"bounceSubType"`
		Timestamp         string `json:"timestamp"`
		BouncedRecipients []struct {
			DiagnosticCode string `json:"diagnosticCode"`
			Status         string `json:"status"`
		} `json:"bouncedRecipients"`
	} `json:"bounce"`
	Complaint *struct {
		ComplaintFeedbackType string `json:"complaintFeedbackType"`
		Timestamp             string `json:"timestamp"`
	} `json:"complaint"`
	Delivery *struct {
		Timestamp    string `json:"timestamp"`
		SMTPResponse string `json:"smtpResponse"`
	} `json:"delivery"`
	Reject *struct {
		Reason string `json:"reason"`
	} `json:"reject"`
	DeliveryDelay *struct {
		DelayType string `json:"delayType"`
	} `json:"deliveryDelay"`
}

// HandleSESWebhook 校验并落地一次 AWS SES（经 SNS）的投递回执。
//
// 与 Zeabur 那条不同，SNS 的准入不是共享密钥而是**证书签名**：
// 报文里带一个 SigningCertURL，取回证书验签即可确认来源是 AWS。
// 因此这条通道不需要管理员配置密钥 —— 但也正因如此，
// 必须自己钉死两件事，否则任何人都能伪造回执：
//   - SigningCertURL 的主机名必须是 AWS 的 SNS 域（否则攻击者给自己的证书）
//   - 配了主题 ARN 时必须匹配（否则别人用他自己的 SNS 主题发给你也算数）
func (s *EmailService) HandleSESWebhook(ctx context.Context, request ProviderWebhookRequest) (*ProviderWebhookResult, error) {
	config, err := s.resolveWebhookConfig(ctx, request.AppID, request.ConfigName, emaildomain.ProviderSES)
	if err != nil {
		return nil, err
	}

	var envelope snsEnvelope
	if err := json.Unmarshal(request.Body, &envelope); err != nil {
		return nil, apperrors.New(40074, http.StatusBadRequest, "SNS 回调报文解析失败")
	}
	if expected := config.Setting("snsTopicArn"); expected != "" &&
		!strings.EqualFold(strings.TrimSpace(envelope.TopicArn), expected) {
		return nil, apperrors.New(40176, http.StatusUnauthorized,
			"SNS 主题 ARN 与配置不符，已拒收：收到 "+clampDetail(envelope.TopicArn))
	}
	if err := s.verifySNSSignature(ctx, envelope); err != nil {
		s.log.Warn("sns signature rejected",
			zap.Int64("appid", request.AppID), zap.String("config", config.Name), zap.Error(err))
		return nil, err
	}

	switch envelope.Type {
	case "SubscriptionConfirmation":
		// 自动回执订阅确认：不确认的话 SNS 永远不会推事件过来，
		// 而管理员在 AWS 控制台上看到的只是一个「Pending confirmation」。
		// 走到这里说明签名已验过且主题 ARN 已匹配，因此是安全的。
		if err := s.confirmSNSSubscription(ctx, envelope.SubscribeURL); err != nil {
			return nil, err
		}
		s.log.Info("sns subscription confirmed",
			zap.Int64("appid", request.AppID), zap.String("topic", envelope.TopicArn))
		return &ProviderWebhookResult{Provider: emaildomain.ProviderSES, Received: true, Note: "订阅已确认"}, nil
	case "UnsubscribeConfirmation":
		s.log.Warn("sns unsubscribe confirmation received",
			zap.Int64("appid", request.AppID), zap.String("topic", envelope.TopicArn))
		return &ProviderWebhookResult{Provider: emaildomain.ProviderSES, Received: true, Note: "订阅已取消"}, nil
	}

	var event sesEventNotification
	if err := json.Unmarshal([]byte(envelope.Message), &event); err != nil {
		return nil, apperrors.New(40074, http.StatusBadRequest, "SES 事件报文解析失败")
	}
	update, ok := buildSESDeliveryUpdate(event)
	if !ok {
		return &ProviderWebhookResult{Provider: emaildomain.ProviderSES, Received: true, Events: 1, Ignored: 1}, nil
	}
	update.AppID = request.AppID
	matched, err := s.pg.ApplyEmailDeliveryEvent(ctx, update)
	if err != nil {
		return nil, err
	}
	return &ProviderWebhookResult{
		Provider: emaildomain.ProviderSES, Received: true, Events: 1, Matched: boolToInt(matched),
	}, nil
}

func buildSESDeliveryUpdate(event sesEventNotification) (pgrepo.EmailDeliveryStatusUpdate, bool) {
	update := pgrepo.EmailDeliveryStatusUpdate{
		ProviderMessageID: strings.TrimSpace(event.Mail.MessageID),
		OccurredAt:        timeutil.Now(),
	}
	if update.ProviderMessageID == "" {
		return update, false
	}
	switch strings.ToLower(strings.TrimSpace(event.EventType)) {
	case "send":
		update.Event = emaildomain.EventSend
		update.Status = emaildomain.DeliveryStatusSent
	case "delivery":
		update.Event = emaildomain.EventDelivery
		update.Status = emaildomain.DeliveryStatusDelivered
		update.MarkDelivered = true
		if event.Delivery != nil {
			update.OccurredAt = parseEventTime(event.Delivery.Timestamp)
		}
	case "bounce":
		update.Event = emaildomain.EventBounce
		update.Status = emaildomain.DeliveryStatusBounced
		update.ErrorMessage = describeSESBounce(event)
		if event.Bounce != nil {
			update.OccurredAt = parseEventTime(event.Bounce.Timestamp)
		}
	case "complaint":
		update.Event = emaildomain.EventComplaint
		update.Status = emaildomain.DeliveryStatusComplained
		update.ErrorMessage = "收件人投诉为垃圾邮件"
		if event.Complaint != nil && event.Complaint.ComplaintFeedbackType != "" {
			update.ErrorMessage += "（" + event.Complaint.ComplaintFeedbackType + "）"
		}
	case "reject":
		update.Event = emaildomain.EventReject
		update.Status = emaildomain.DeliveryStatusRejected
		update.ErrorMessage = "邮件被 SES 拒绝发送"
		if event.Reject != nil && event.Reject.Reason != "" {
			update.ErrorMessage += "：" + event.Reject.Reason
		}
	case "open":
		update.Event = emaildomain.EventOpen
		update.IncrementOpen = true
	case "click":
		update.Event = emaildomain.EventClick
		update.IncrementClick = true
	case "deliverydelay":
		// 延迟不是终态：SES 之后仍可能投递成功或退信。
		// 把它写成失败会让一封最终送达的信永远显示成失败（状态是单向推进的）。
		update.Event = "delay"
		update.ErrorMessage = "投递延迟，SES 仍在重试"
		if event.DeliveryDelay != nil && event.DeliveryDelay.DelayType != "" {
			update.ErrorMessage += "（" + event.DeliveryDelay.DelayType + "）"
		}
	default:
		return update, false
	}
	return update, true
}

func describeSESBounce(event sesEventNotification) string {
	if event.Bounce == nil {
		return "邮件被退回"
	}
	parts := []string{event.Bounce.BounceType}
	if event.Bounce.BounceSubType != "" {
		parts = append(parts, event.Bounce.BounceSubType)
	}
	for _, recipient := range event.Bounce.BouncedRecipients {
		if code := strings.TrimSpace(recipient.DiagnosticCode); code != "" {
			parts = append(parts, code)
			break
		}
	}
	return "邮件被退回（" + strings.Join(parts, ", ") + "）"
}

// snsCertHost 只接受 AWS 自己的 SNS 域名。
//
// 这一条是整个 SNS 验签的**安全根**：不校验主机名的话，攻击者只要把
// SigningCertURL 指向自己的服务器，就能用自己的私钥签出一条「通过验签」的回执。
// 覆盖国际区与中国区（amazonaws.com.cn）。
var snsCertHost = regexp.MustCompile(`^sns\.[a-z0-9-]+\.amazonaws\.com(\.cn)?$`)

// snsCertCache 缓存已取回的签名证书。
// SNS 每条消息都带同一个证书地址，不缓存的话每条回执都要多一次出网请求。
var snsCertCache sync.Map // string -> *x509.Certificate

func (s *EmailService) verifySNSSignature(ctx context.Context, envelope snsEnvelope) error {
	certURL, err := url.Parse(strings.TrimSpace(envelope.SigningCertURL))
	if err != nil || certURL.Scheme != "https" || !snsCertHost.MatchString(certURL.Host) {
		return apperrors.New(40176, http.StatusUnauthorized,
			"SNS 签名证书地址不是 AWS 域名，已按伪造回执拒收")
	}
	if signature := strings.TrimSpace(envelope.Signature); signature == "" {
		return apperrors.New(40176, http.StatusUnauthorized, "SNS 回调缺少签名")
	}
	// 时间窗口：SNS 的签名本身不带有效期，卡住 Timestamp 才能挡住事后重放。
	if err := checkEventTimestampDrift(envelope.Timestamp); err != nil {
		return err
	}

	certificate, err := s.fetchSNSCertificate(ctx, certURL.String())
	if err != nil {
		return err
	}
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(envelope.Signature))
	if err != nil {
		return apperrors.New(40176, http.StatusUnauthorized, "SNS 回调签名格式非法")
	}

	payload := snsStringToSign(envelope)
	publicKey, ok := certificate.PublicKey.(*rsa.PublicKey)
	if !ok {
		return apperrors.New(40176, http.StatusUnauthorized, "SNS 签名证书的公钥类型不受支持")
	}
	hash := crypto.SHA256
	digest := sha256.Sum256(payload)
	sum := digest[:]
	if strings.TrimSpace(envelope.SignatureVersion) == "1" {
		hash = crypto.SHA1
		legacy := sha1.Sum(payload) //nolint:gosec // SignatureVersion 1 规定用 SHA1
		sum = legacy[:]
	}
	if err := rsa.VerifyPKCS1v15(publicKey, hash, sum, signature); err != nil {
		return apperrors.New(40176, http.StatusUnauthorized, "SNS 回调签名校验失败")
	}
	return nil
}

// snsStringToSign 按 SNS 规格拼待签名串：字段名字典序、每项 "key\nvalue\n"。
// 字段集合按消息类型不同，缺一个或多一个都会验不过。
func snsStringToSign(envelope snsEnvelope) []byte {
	var builder strings.Builder
	appendField := func(key string, value string) {
		if value == "" {
			return
		}
		builder.WriteString(key)
		builder.WriteString("\n")
		builder.WriteString(value)
		builder.WriteString("\n")
	}
	if envelope.Type == "SubscriptionConfirmation" || envelope.Type == "UnsubscribeConfirmation" {
		appendField("Message", envelope.Message)
		appendField("MessageId", envelope.MessageID)
		appendField("SubscribeURL", envelope.SubscribeURL)
		appendField("Timestamp", envelope.Timestamp)
		appendField("Token", envelope.Token)
		appendField("TopicArn", envelope.TopicArn)
		appendField("Type", envelope.Type)
		return []byte(builder.String())
	}
	appendField("Message", envelope.Message)
	appendField("MessageId", envelope.MessageID)
	appendField("Subject", envelope.Subject)
	appendField("Timestamp", envelope.Timestamp)
	appendField("TopicArn", envelope.TopicArn)
	appendField("Type", envelope.Type)
	return []byte(builder.String())
}

func (s *EmailService) fetchSNSCertificate(ctx context.Context, certURL string) (*x509.Certificate, error) {
	if cached, ok := snsCertCache.Load(certURL); ok {
		return cached.(*x509.Certificate), nil
	}
	client := newEmailHTTPClient("email.ses.sns", 10)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, certURL, nil)
	if err != nil {
		return nil, apperrors.New(50060, http.StatusInternalServerError, "构造 SNS 证书请求失败")
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, apperrors.New(50060, http.StatusBadGateway, "取回 SNS 签名证书失败："+err.Error())
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, apperrors.New(50060, http.StatusBadGateway,
			fmt.Sprintf("取回 SNS 签名证书失败（HTTP %d）", response.StatusCode))
	}
	block, _ := pem.Decode(readLimitedBody(response.Body))
	if block == nil {
		return nil, apperrors.New(50060, http.StatusBadGateway, "SNS 签名证书格式非法")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, apperrors.New(50060, http.StatusBadGateway, "解析 SNS 签名证书失败："+err.Error())
	}
	snsCertCache.Store(certURL, certificate)
	return certificate, nil
}

func (s *EmailService) confirmSNSSubscription(ctx context.Context, subscribeURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(subscribeURL))
	if err != nil || parsed.Scheme != "https" || !snsCertHost.MatchString(parsed.Host) {
		return apperrors.New(40176, http.StatusUnauthorized, "SNS 订阅确认地址不是 AWS 域名，已拒绝")
	}
	client := newEmailHTTPClient("email.ses.sns", 10)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return apperrors.New(50060, http.StatusInternalServerError, "构造 SNS 订阅确认请求失败")
	}
	response, err := client.Do(request)
	if err != nil {
		return apperrors.New(50060, http.StatusBadGateway, "确认 SNS 订阅失败："+err.Error())
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return apperrors.New(50060, http.StatusBadGateway,
			fmt.Sprintf("确认 SNS 订阅失败（HTTP %d）", response.StatusCode))
	}
	return nil
}

// ── Resend ──

// resendWebhookEvent 是 Resend 推送的事件报文。
type resendWebhookEvent struct {
	Type      string `json:"type"`
	CreatedAt string `json:"created_at"`
	Data      struct {
		EmailID string `json:"email_id"`
		// Resend 的退信/投诉细节放在 bounce / click 等子对象里，
		// 字段随事件类型而变，因此收成一个通用 map 存进留痕。
		Bounce map[string]any `json:"bounce"`
		Click  map[string]any `json:"click"`
	} `json:"data"`
}

// HandleResendWebhook 校验并落地一次 Resend 投递回执。
//
// 验签直接用官方 SDK 的 Verify（svix 规格：HMAC-SHA256 over "{id}.{timestamp}.{body}"）。
// 自己实现的话要同时踩对三件事：密钥的 whsec_ 前缀要剥掉再 base64 解码、
// 签名头是空格分隔的多个候选、以及时间窗口 —— 任何一处写错的表现都是
// 「一律验不过」，而那与「密钥填错了」长得一模一样。
func (s *EmailService) HandleResendWebhook(ctx context.Context, request ProviderWebhookRequest) (*ProviderWebhookResult, error) {
	config, err := s.resolveWebhookConfig(ctx, request.AppID, request.ConfigName, emaildomain.ProviderResend)
	if err != nil {
		return nil, err
	}
	secret := config.Secret(emaildomain.KeyWebhookSecret)
	if secret == "" {
		return nil, apperrors.New(40073, http.StatusPreconditionFailed,
			"该邮件配置尚未设置 Resend Webhook 签名密钥，无法校验回调来源")
	}
	verifier := resendlib.NewClient("unused").Webhooks
	if err := verifier.Verify(&resendlib.VerifyWebhookOptions{
		Payload: string(request.Body),
		Headers: resendlib.WebhookHeaders{
			Id:        request.Headers.Get(ResendWebhookIDHeader),
			Timestamp: request.Headers.Get(ResendWebhookTimestampHeader),
			Signature: request.Headers.Get(ResendWebhookSignatureHeader),
		},
		WebhookSecret: secret,
	}); err != nil {
		s.log.Warn("resend webhook signature rejected",
			zap.Int64("appid", request.AppID), zap.String("config", config.Name), zap.Error(err))
		return nil, apperrors.New(40176, http.StatusUnauthorized, "Resend 回调验签失败："+clampDetail(err.Error()))
	}

	var event resendWebhookEvent
	if err := json.Unmarshal(request.Body, &event); err != nil {
		return nil, apperrors.New(40074, http.StatusBadRequest, "Resend 回调报文解析失败")
	}
	update, ok := buildResendDeliveryUpdate(event)
	if !ok {
		return &ProviderWebhookResult{Provider: emaildomain.ProviderResend, Received: true, Events: 1, Ignored: 1}, nil
	}
	update.AppID = request.AppID
	matched, err := s.pg.ApplyEmailDeliveryEvent(ctx, update)
	if err != nil {
		return nil, err
	}
	return &ProviderWebhookResult{
		Provider: emaildomain.ProviderResend, Received: true, Events: 1, Matched: boolToInt(matched),
	}, nil
}

func buildResendDeliveryUpdate(event resendWebhookEvent) (pgrepo.EmailDeliveryStatusUpdate, bool) {
	update := pgrepo.EmailDeliveryStatusUpdate{
		ProviderMessageID: strings.TrimSpace(event.Data.EmailID),
		OccurredAt:        parseEventTime(event.CreatedAt),
	}
	if update.ProviderMessageID == "" {
		return update, false
	}
	switch strings.ToLower(strings.TrimSpace(event.Type)) {
	case "email.sent":
		update.Event = emaildomain.EventSend
		update.Status = emaildomain.DeliveryStatusSent
	case "email.delivered":
		update.Event = emaildomain.EventDelivery
		update.Status = emaildomain.DeliveryStatusDelivered
		update.MarkDelivered = true
	case "email.bounced":
		update.Event = emaildomain.EventBounce
		update.Status = emaildomain.DeliveryStatusBounced
		update.ErrorMessage = describeMapReason(event.Data.Bounce, "邮件被退回")
		update.Payload = event.Data.Bounce
	case "email.complained":
		update.Event = emaildomain.EventComplaint
		update.Status = emaildomain.DeliveryStatusComplained
		update.ErrorMessage = "收件人投诉为垃圾邮件"
	case "email.delivery_delayed":
		// 与 SES 同理：延迟不是终态，只记事件与原因，不改主状态。
		update.Event = "delay"
		update.ErrorMessage = "投递延迟，Resend 仍在重试"
	case "email.opened":
		update.Event = emaildomain.EventOpen
		update.IncrementOpen = true
	case "email.clicked":
		update.Event = emaildomain.EventClick
		update.IncrementClick = true
		update.Payload = event.Data.Click
	default:
		return update, false
	}
	return update, true
}

// ── SendGrid ──

// sendgridWebhookEvent 是 Event Webhook 里的一条事件。SendGrid 一次推一个数组。
type sendgridWebhookEvent struct {
	Event       string `json:"event"`
	SGMessageID string `json:"sg_message_id"`
	SMTPID      string `json:"smtp-id"`
	Timestamp   int64  `json:"timestamp"`
	Reason      string `json:"reason"`
	Status      string `json:"status"`
	Response    string `json:"response"`
	Type        string `json:"type"`
}

// HandleSendGridWebhook 校验并落地一批 SendGrid 投递回执。
//
// SendGrid 的验签是 ECDSA P-256（不是 HMAC）：签名覆盖 "时间戳 + 原始报文"，
// 公钥由管理员从控制台复制过来。没有公钥时一律拒收 ——
// SendGrid 默认**不签名**，而不签名的回执谁都能伪造，
// 伪造一条 delivered 就能把一封退信的邮件显示成送达。
func (s *EmailService) HandleSendGridWebhook(ctx context.Context, request ProviderWebhookRequest) (*ProviderWebhookResult, error) {
	config, err := s.resolveWebhookConfig(ctx, request.AppID, request.ConfigName, emaildomain.ProviderSendGrid)
	if err != nil {
		return nil, err
	}
	publicKey := config.Setting("webhookPublicKey")
	if publicKey == "" {
		return nil, apperrors.New(40073, http.StatusPreconditionFailed,
			"该邮件配置尚未填写 SendGrid 事件签名公钥，无法校验回调来源；请先在 SendGrid 打开 Signed Event Webhook")
	}
	if err := verifySendGridSignature(publicKey,
		request.Headers.Get(SendGridWebhookTimestampHeader),
		request.Headers.Get(SendGridWebhookSignatureHeader),
		request.Body); err != nil {
		s.log.Warn("sendgrid webhook signature rejected",
			zap.Int64("appid", request.AppID), zap.String("config", config.Name), zap.Error(err))
		return nil, err
	}

	var events []sendgridWebhookEvent
	if err := json.Unmarshal(request.Body, &events); err != nil {
		return nil, apperrors.New(40074, http.StatusBadRequest, "SendGrid 回调报文解析失败")
	}
	result := &ProviderWebhookResult{Provider: emaildomain.ProviderSendGrid, Received: true, Events: len(events)}
	for _, event := range events {
		update, ok := buildSendGridDeliveryUpdate(event)
		if !ok {
			result.Ignored++
			continue
		}
		update.AppID = request.AppID
		matched, err := s.pg.ApplyEmailDeliveryEvent(ctx, update)
		if err != nil {
			return nil, err
		}
		if matched {
			result.Matched++
		}
	}
	return result, nil
}

func buildSendGridDeliveryUpdate(event sendgridWebhookEvent) (pgrepo.EmailDeliveryStatusUpdate, bool) {
	update := pgrepo.EmailDeliveryStatusUpdate{
		ProviderMessageID: sendgridMessageKey(event.SGMessageID),
		OccurredAt:        time.Unix(event.Timestamp, 0),
	}
	if update.ProviderMessageID == "" {
		return update, false
	}
	if event.Timestamp <= 0 {
		update.OccurredAt = timeutil.Now()
	}
	switch strings.ToLower(strings.TrimSpace(event.Event)) {
	case "processed":
		update.Event = emaildomain.EventSend
		update.Status = emaildomain.DeliveryStatusSent
	case "delivered":
		update.Event = emaildomain.EventDelivery
		update.Status = emaildomain.DeliveryStatusDelivered
		update.MarkDelivered = true
	case "bounce", "blocked":
		update.Event = emaildomain.EventBounce
		update.Status = emaildomain.DeliveryStatusBounced
		update.ErrorMessage = withReason("邮件被退回", event.Reason, event.Response)
	case "dropped":
		update.Event = emaildomain.EventReject
		update.Status = emaildomain.DeliveryStatusRejected
		update.ErrorMessage = withReason("SendGrid 丢弃了这封邮件", event.Reason)
	case "spamreport":
		update.Event = emaildomain.EventComplaint
		update.Status = emaildomain.DeliveryStatusComplained
		update.ErrorMessage = "收件人投诉为垃圾邮件"
	case "deferred":
		update.Event = "delay"
		update.ErrorMessage = withReason("投递延迟，SendGrid 仍在重试", event.Response, event.Reason)
	case "open":
		update.Event = emaildomain.EventOpen
		update.IncrementOpen = true
	case "click":
		update.Event = emaildomain.EventClick
		update.IncrementClick = true
	default:
		// unsubscribe / group_unsubscribe 等与投递状态无关，静默放行。
		return update, false
	}
	return update, true
}

// sendgridMessageKey 把事件里的 sg_message_id 收敛成发信时留痕的那个键。
//
// 发信响应头 X-Message-Id 给的是 `abc123`，而事件里的 sg_message_id 是
// `abc123.filterdrecv-...`。不截断的话，**每一条**回执都匹配不到留痕，
// 表现为「webhook 明明在推，投递记录永远停在 pending」。
func sendgridMessageKey(raw string) string {
	raw = strings.TrimSpace(raw)
	if index := strings.Index(raw, "."); index > 0 {
		return raw[:index]
	}
	return raw
}

func verifySendGridSignature(publicKeyBase64 string, timestamp string, signature string, body []byte) error {
	timestamp = strings.TrimSpace(timestamp)
	signature = strings.TrimSpace(signature)
	if timestamp == "" || signature == "" {
		return apperrors.New(40176, http.StatusUnauthorized, "SendGrid 回调缺少签名或时间戳请求头")
	}
	if err := checkEventTimestampDrift(timestamp); err != nil {
		return err
	}
	keyBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(publicKeyBase64))
	if err != nil {
		return apperrors.New(40073, http.StatusPreconditionFailed, "SendGrid 事件签名公钥不是合法的 base64，请从控制台重新复制")
	}
	parsed, err := x509.ParsePKIXPublicKey(keyBytes)
	if err != nil {
		return apperrors.New(40073, http.StatusPreconditionFailed, "SendGrid 事件签名公钥解析失败，请从控制台重新复制")
	}
	publicKey, ok := parsed.(*ecdsa.PublicKey)
	if !ok {
		return apperrors.New(40073, http.StatusPreconditionFailed, "SendGrid 事件签名公钥不是 ECDSA 公钥")
	}
	signatureBytes, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		return apperrors.New(40176, http.StatusUnauthorized, "SendGrid 回调签名格式非法")
	}
	// 签名覆盖的是「时间戳直接拼上原始报文」，中间没有分隔符。
	digest := sha256.Sum256(append([]byte(timestamp), body...))
	if !ecdsa.VerifyASN1(publicKey, digest[:], signatureBytes) {
		return apperrors.New(40176, http.StatusUnauthorized, "SendGrid 回调签名校验失败，请核对事件签名公钥")
	}
	return nil
}

// ── Mailgun ──

// HandleMailgunWebhook 校验并落地一次 Mailgun 投递回执。
//
// 验签用官方 SDK 的 VerifyWebhookSignature（HMAC-SHA256 over `timestamp + token`，
// 恒定时间比较）。签名之外还必须自己加**一次性 token 的防重放**：
// Mailgun 的签名不覆盖报文体，只覆盖时间戳与随机 token，
// 因此一条被截获的回调在时间窗口内可以原样重放任意多次，
// 而每次重放都会再推进一次状态、再累加一次打开数。
func (s *EmailService) HandleMailgunWebhook(ctx context.Context, request ProviderWebhookRequest) (*ProviderWebhookResult, error) {
	config, err := s.resolveWebhookConfig(ctx, request.AppID, request.ConfigName, emaildomain.ProviderMailgun)
	if err != nil {
		return nil, err
	}
	signingKey := config.Secret(emaildomain.KeyWebhookSecret)
	if signingKey == "" {
		return nil, apperrors.New(40073, http.StatusPreconditionFailed,
			"该邮件配置尚未设置 Mailgun Webhook 签名密钥，无法校验回调来源")
	}

	var payload mtypes.WebhookPayload
	if err := json.Unmarshal(request.Body, &payload); err != nil {
		return nil, apperrors.New(40074, http.StatusBadRequest, "Mailgun 回调报文解析失败")
	}
	if err := checkEventTimestampDrift(payload.Signature.TimeStamp); err != nil {
		return nil, err
	}

	// SDK 的验签挂在 Client 上，因此造一个只用来验签的实例：
	// 自己抄一遍 HMAC 是能写，但把「签的是哪几段、按什么顺序」这种细节
	// 复制到第二个地方，迟早会与 SDK 漂移，而漂移的表现是一律验不过。
	verifier := mailgunlib.NewMailgun("unused")
	verifier.SetWebhookSigningKey(signingKey)
	verified, err := verifier.VerifyWebhookSignature(payload.Signature)
	if err != nil || !verified {
		s.log.Warn("mailgun webhook signature rejected",
			zap.Int64("appid", request.AppID), zap.String("config", config.Name), zap.Error(err))
		return nil, apperrors.New(40176, http.StatusUnauthorized,
			"Mailgun 回调签名校验失败，请核对 API Security 页里的 HTTP webhook signing key")
	}
	if err := s.guardWebhookReplay(ctx, emaildomain.ProviderMailgun, request.AppID, payload.Signature.Token); err != nil {
		return nil, err
	}

	event, err := events.ParseEvent(payload.EventData)
	if err != nil {
		return nil, apperrors.New(40074, http.StatusBadRequest, "Mailgun 事件报文解析失败："+clampDetail(err.Error()))
	}
	update, ok := buildMailgunDeliveryUpdate(event)
	if !ok {
		s.log.Info("mailgun webhook event ignored",
			zap.Int64("appid", request.AppID), zap.String("event", event.GetName()))
		return &ProviderWebhookResult{Provider: emaildomain.ProviderMailgun, Received: true, Events: 1, Ignored: 1}, nil
	}
	update.AppID = request.AppID
	matched, err := s.pg.ApplyEmailDeliveryEvent(ctx, update)
	if err != nil {
		return nil, err
	}
	return &ProviderWebhookResult{
		Provider: emaildomain.ProviderMailgun, Received: true, Events: 1, Matched: boolToInt(matched),
	}, nil
}

// buildMailgunDeliveryUpdate 把 SDK 解出来的类型化事件翻成一次状态推进。
//
// 关联键取 `message.headers.message-id`：Mailgun 发信时返回的是带尖括号的
// `<...@mg.example.com>`，事件里那份**不带尖括号**，因此留痕存的就是去掉尖括号的形态。
func buildMailgunDeliveryUpdate(event events.Event) (pgrepo.EmailDeliveryStatusUpdate, bool) {
	update := pgrepo.EmailDeliveryStatusUpdate{
		Event:      strings.ToLower(strings.TrimSpace(event.GetName())),
		OccurredAt: event.GetTimestamp(),
	}
	if update.OccurredAt.IsZero() {
		update.OccurredAt = timeutil.Now()
	}

	switch typed := event.(type) {
	case *events.Accepted:
		update.ProviderMessageID = mailgunMessageKey(typed.Message.Headers.MessageID)
		update.Event = emaildomain.EventSend
		update.Status = emaildomain.DeliveryStatusSent
	case *events.Delivered:
		update.ProviderMessageID = mailgunMessageKey(typed.Message.Headers.MessageID)
		update.Event = emaildomain.EventDelivery
		update.Status = emaildomain.DeliveryStatusDelivered
		update.MarkDelivered = true
	case *events.Failed:
		update.ProviderMessageID = mailgunMessageKey(typed.Message.Headers.MessageID)
		// severity=temporary 是**暂时**失败，Mailgun 之后仍会重投。
		// 写成终态会让一封最终送达的信永远显示成退信（状态是单向推进的）。
		if strings.EqualFold(typed.Severity, "temporary") {
			update.Event = "delay"
			update.ErrorMessage = withReason("投递延迟，Mailgun 仍在重试",
				typed.Reason, typed.DeliveryStatus.Message, typed.DeliveryStatus.Description)
			break
		}
		update.Event = emaildomain.EventBounce
		update.Status = emaildomain.DeliveryStatusBounced
		update.ErrorMessage = withReason("邮件被退回",
			typed.Reason, typed.DeliveryStatus.Message, typed.DeliveryStatus.Description)
	case *events.Rejected:
		update.ProviderMessageID = mailgunMessageKey(typed.Message.Headers.MessageID)
		update.Event = emaildomain.EventReject
		update.Status = emaildomain.DeliveryStatusRejected
		update.ErrorMessage = withReason("Mailgun 拒绝发送这封邮件", typed.Reject.Reason, typed.Reject.Description)
	case *events.Complained:
		update.ProviderMessageID = mailgunMessageKey(typed.Message.Headers.MessageID)
		update.Event = emaildomain.EventComplaint
		update.Status = emaildomain.DeliveryStatusComplained
		update.ErrorMessage = "收件人投诉为垃圾邮件"
	case *events.Opened:
		update.ProviderMessageID = mailgunMessageKey(typed.Message.Headers.MessageID)
		update.Event = emaildomain.EventOpen
		update.IncrementOpen = true
	case *events.Clicked:
		update.ProviderMessageID = mailgunMessageKey(typed.Message.Headers.MessageID)
		update.Event = emaildomain.EventClick
		update.IncrementClick = true
	default:
		// stored / unsubscribed / 列表类事件与投递状态无关，静默放行。
		return update, false
	}
	if update.ProviderMessageID == "" {
		return update, false
	}
	return update, true
}

func mailgunMessageKey(raw string) string {
	return strings.Trim(strings.TrimSpace(raw), "<>")
}

// ── Postmark ──

// postmarkWebhookEvent Postmark 按 RecordType 区分事件类型，一次推一条。
type postmarkWebhookEvent struct {
	RecordType  string `json:"RecordType"`
	MessageID   string `json:"MessageID"`
	Recipient   string `json:"Recipient"`
	DeliveredAt string `json:"DeliveredAt"`
	ReceivedAt  string `json:"ReceivedAt"`
	BouncedAt   string `json:"BouncedAt"`
	Type        string `json:"Type"`
	Description string `json:"Description"`
	Details     string `json:"Details"`
}

// HandlePostmarkWebhook 校验并落地一次 Postmark 投递回执。
//
// Postmark **不对回执签名**（官方只提供「在回调地址里放 Basic Auth 凭据」这一种做法），
// 因此准入靠平台自己下发的回调令牌：地址即凭据。
func (s *EmailService) HandlePostmarkWebhook(ctx context.Context, request ProviderWebhookRequest) (*ProviderWebhookResult, error) {
	config, err := s.resolveWebhookConfig(ctx, request.AppID, request.ConfigName, emaildomain.ProviderPostmark)
	if err != nil {
		return nil, err
	}
	if err := s.verifyWebhookToken(config, request, "Postmark"); err != nil {
		return nil, err
	}

	var event postmarkWebhookEvent
	if err := json.Unmarshal(request.Body, &event); err != nil {
		return nil, apperrors.New(40074, http.StatusBadRequest, "Postmark 回调报文解析失败")
	}
	update, ok := buildPostmarkDeliveryUpdate(event)
	if !ok {
		s.log.Info("postmark webhook event ignored",
			zap.Int64("appid", request.AppID), zap.String("recordType", event.RecordType))
		return &ProviderWebhookResult{Provider: emaildomain.ProviderPostmark, Received: true, Events: 1, Ignored: 1}, nil
	}
	update.AppID = request.AppID
	matched, err := s.pg.ApplyEmailDeliveryEvent(ctx, update)
	if err != nil {
		return nil, err
	}
	return &ProviderWebhookResult{
		Provider: emaildomain.ProviderPostmark, Received: true, Events: 1, Matched: boolToInt(matched),
	}, nil
}

func buildPostmarkDeliveryUpdate(event postmarkWebhookEvent) (pgrepo.EmailDeliveryStatusUpdate, bool) {
	update := pgrepo.EmailDeliveryStatusUpdate{
		ProviderMessageID: strings.TrimSpace(event.MessageID),
		OccurredAt:        timeutil.Now(),
	}
	if update.ProviderMessageID == "" {
		return update, false
	}
	switch strings.ToLower(strings.TrimSpace(event.RecordType)) {
	case "delivery":
		update.Event = emaildomain.EventDelivery
		update.Status = emaildomain.DeliveryStatusDelivered
		update.MarkDelivered = true
		update.OccurredAt = parseEventTime(event.DeliveredAt)
	case "bounce":
		update.OccurredAt = parseEventTime(event.BouncedAt)
		// Postmark 的退信类型里有一批是**暂时性**的（软退信 / DNS 抖动 / 自动回复），
		// 之后仍可能送达，因此只记事件不改主状态 —— 与 Mailgun 的 temporary 同一条。
		if postmarkTransientBounce(event.Type) {
			update.Event = "delay"
			update.ErrorMessage = withReason("投递延迟，Postmark 仍在重试", event.Description, event.Details)
			break
		}
		update.Event = emaildomain.EventBounce
		update.Status = emaildomain.DeliveryStatusBounced
		update.ErrorMessage = withReason("邮件被退回（"+strings.TrimSpace(event.Type)+"）", event.Description, event.Details)
	case "spamcomplaint":
		update.Event = emaildomain.EventComplaint
		update.Status = emaildomain.DeliveryStatusComplained
		update.ErrorMessage = "收件人投诉为垃圾邮件"
		update.OccurredAt = parseEventTime(event.BouncedAt)
	case "open":
		update.Event = emaildomain.EventOpen
		update.IncrementOpen = true
		update.OccurredAt = parseEventTime(event.ReceivedAt)
	case "click":
		update.Event = emaildomain.EventClick
		update.IncrementClick = true
		update.OccurredAt = parseEventTime(event.ReceivedAt)
	default:
		// SubscriptionChange / Inbound 等与投递状态无关。
		return update, false
	}
	return update, true
}

// postmarkTransientBounce 暂时性退信类型（Postmark 的 Type 字段取值）。
func postmarkTransientBounce(bounceType string) bool {
	switch strings.ToLower(strings.TrimSpace(bounceType)) {
	case "softbounce", "transient", "dnserror", "smtpapierror", "autoresponder", "challengeverification":
		return true
	default:
		return false
	}
}

// ── 阿里云邮件推送 ──

// HandleAliyunWebhook 校验并落地一次阿里云邮件推送的回执。
//
// 与 Postmark 同理：阿里云的回执报文里没有任何可验证的凭据，准入靠地址里的回调令牌。
func (s *EmailService) HandleAliyunWebhook(ctx context.Context, request ProviderWebhookRequest) (*ProviderWebhookResult, error) {
	config, err := s.resolveWebhookConfig(ctx, request.AppID, request.ConfigName, emaildomain.ProviderAliyun)
	if err != nil {
		return nil, err
	}
	if err := s.verifyWebhookToken(config, request, "阿里云邮件推送"); err != nil {
		return nil, err
	}
	return s.applyTolerantWebhook(ctx, request, emaildomain.ProviderAliyun, buildAliyunDeliveryUpdate)
}

// buildAliyunDeliveryUpdate 把阿里云回执翻成一次状态推进。
//
// 阿里云的回执在不同投递方式（MNS 队列 / 主题推送 / 事件通知）下字段拼写不一致
// （`EnvId` / `envId`、`Status` / `status`，有的带 `MessageEventType` 有的只有状态码），
// 因此按**别名集合**取值而不是钉死一种拼写：钉死一种的代价是换个投递方式
// 就一条也匹配不上，而那个失败是静默的（HTTP 200，零条匹配）。
func buildAliyunDeliveryUpdate(payload map[string]any) (pgrepo.EmailDeliveryStatusUpdate, bool) {
	update := pgrepo.EmailDeliveryStatusUpdate{
		ProviderMessageID: lookupString(payload, "envId", "messageId", "mailId"),
		Payload:           payload,
		OccurredAt:        parseEventTime(lookupString(payload, "time", "timestamp", "eventTime")),
	}
	if update.ProviderMessageID == "" {
		return update, false
	}
	reason := lookupString(payload, "message", "errorMessage", "reason", "description")

	// 优先看显式的事件名；没有事件名时回落到 status（0 = 成功，非 0 = 失败）。
	switch normalizeEventToken(lookupString(payload, "messageEventType", "eventType", "event", "type")) {
	case "delivered", "delivery", "success":
		return markDelivered(update), true
	case "bounced", "bounce", "fail", "failed":
		return markBounced(update, reason), true
	case "complained", "complaint", "spam":
		return markComplained(update), true
	case "opened", "open":
		update.Event = emaildomain.EventOpen
		update.IncrementOpen = true
		return update, true
	case "clicked", "click":
		update.Event = emaildomain.EventClick
		update.IncrementClick = true
		return update, true
	case "":
		status := lookupString(payload, "status", "statusCode", "code")
		if status == "" {
			return update, false
		}
		if status == "0" {
			return markDelivered(update), true
		}
		return markBounced(update, withReason("状态码 "+status, reason)), true
	default:
		return update, false
	}
}

// ── 腾讯云 SES ──

// HandleTencentWebhook 校验并落地一次腾讯云 SES 的回执。
func (s *EmailService) HandleTencentWebhook(ctx context.Context, request ProviderWebhookRequest) (*ProviderWebhookResult, error) {
	config, err := s.resolveWebhookConfig(ctx, request.AppID, request.ConfigName, emaildomain.ProviderTencent)
	if err != nil {
		return nil, err
	}
	if err := s.verifyWebhookToken(config, request, "腾讯云邮件推送"); err != nil {
		return nil, err
	}
	return s.applyTolerantWebhook(ctx, request, emaildomain.ProviderTencent, buildTencentDeliveryUpdate)
}

// buildTencentDeliveryUpdate 把腾讯云回执翻成一次状态推进。
// 同样按别名取值：腾讯云的回执字段在文档不同版本里有 snake_case 与 PascalCase 两种拼写。
func buildTencentDeliveryUpdate(payload map[string]any) (pgrepo.EmailDeliveryStatusUpdate, bool) {
	update := pgrepo.EmailDeliveryStatusUpdate{
		ProviderMessageID: lookupString(payload, "messageId", "mailId"),
		Payload:           payload,
		OccurredAt:        parseEventTime(lookupString(payload, "timestamp", "time", "eventTime")),
	}
	if update.ProviderMessageID == "" {
		return update, false
	}
	reason := lookupString(payload, "bounceReason", "reason", "message", "description")
	bounceType := lookupString(payload, "bounceType", "bounceSubType")

	switch normalizeEventToken(lookupString(payload, "eventType", "event", "type")) {
	case "delivered", "delivery", "send", "sent":
		return markDelivered(update), true
	case "bounced", "bounce", "failed", "fail":
		// 腾讯云把「软退信 / 暂时失败」也归在 bounce 下，用 bounceType 区分。
		// 与 Mailgun / Postmark 同一条：暂时失败不是终态。
		lowered := strings.ToLower(bounceType)
		if strings.Contains(lowered, "soft") || strings.Contains(lowered, "transient") {
			update.Event = "delay"
			update.ErrorMessage = withReason("投递延迟，腾讯云仍在重试", reason)
			return update, true
		}
		return markBounced(update, withReason(bounceType, reason)), true
	case "complained", "complaint", "spam":
		return markComplained(update), true
	case "opened", "open":
		update.Event = emaildomain.EventOpen
		update.IncrementOpen = true
		return update, true
	case "clicked", "click":
		update.Event = emaildomain.EventClick
		update.IncrementClick = true
		return update, true
	case "deferred", "delay", "delayed":
		update.Event = "delay"
		update.ErrorMessage = withReason("投递延迟，腾讯云仍在重试", reason)
		return update, true
	default:
		return update, false
	}
}

// ── 不签名服务商的共用准入与解析 ──

// AegisWebhookTokenHeader 回调令牌的可选请求头。
//
// Postmark 支持在回调地址里放 Basic Auth 凭据，阿里云 / 腾讯云只能给一个 URL，
// 因此令牌的**主**通道是 query。留一个请求头是给自建转发层用的：
// 有些团队不愿意让带密钥的地址出现在网关访问日志里。
const AegisWebhookTokenHeader = "X-Aegis-Webhook-Token"

// verifyWebhookToken 校验回调令牌。
//
// 三个来源任一命中即可：query 的 `token`、`X-Aegis-Webhook-Token` 请求头、
// HTTP Basic Auth 的**密码位**（用户名不参与判定 —— Postmark 回调地址里的
// 用户名是管理员随手填的，拿它当凭据的一部分只会制造一类查不出的失败）。
//
// 比较必须是恒定时间的，否则令牌可被逐字节试探出来。
func (s *EmailService) verifyWebhookToken(config *emaildomain.Config, request ProviderWebhookRequest, providerName string) error {
	expected := config.Secret(emaildomain.KeyWebhookSecret)
	if expected == "" {
		return apperrors.New(40073, http.StatusPreconditionFailed,
			providerName+" 不对回执签名，因此必须先在这条邮件配置里设置「回调令牌」，否则回执可被任意伪造")
	}

	candidates := make([]string, 0, 3)
	if request.Query != nil {
		candidates = append(candidates, request.Query.Get("token"))
	}
	if request.Headers != nil {
		candidates = append(candidates, request.Headers.Get(AegisWebhookTokenHeader))
		if _, password, ok := basicAuthFrom(request.Headers); ok {
			candidates = append(candidates, password)
		}
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(candidate), []byte(expected)) == 1 {
			return nil
		}
	}
	s.log.Warn("email webhook token rejected",
		zap.Int64("appid", request.AppID), zap.String("config", config.Name),
		zap.String("provider", config.Provider))
	return apperrors.New(40176, http.StatusUnauthorized,
		providerName+" 回执的回调令牌不正确；请确认服务商后台填的地址与控制台给出的完全一致")
}

// basicAuthFrom 解析 Authorization: Basic 头。
// 不用 http.Request.BasicAuth 是因为这一层只拿到了 Header，没有 Request。
func basicAuthFrom(headers http.Header) (string, string, bool) {
	value := strings.TrimSpace(headers.Get("Authorization"))
	const prefix = "basic "
	if len(value) < len(prefix) || !strings.EqualFold(value[:len(prefix)], prefix) {
		return "", "", false
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(value[len(prefix):]))
	if err != nil {
		return "", "", false
	}
	user, password, ok := strings.Cut(string(decoded), ":")
	return user, password, ok
}

// applyTolerantWebhook 解析「一个对象或一个对象数组」的回执报文并逐条落地。
//
// 阿里云与腾讯云都可能一次推一条或一批，且两种形状在文档里都出现过。
// 只认其中一种的代价是另一种全军覆没，而那个失败是静默的（HTTP 200，零条匹配）。
func (s *EmailService) applyTolerantWebhook(
	ctx context.Context,
	request ProviderWebhookRequest,
	provider string,
	build func(map[string]any) (pgrepo.EmailDeliveryStatusUpdate, bool),
) (*ProviderWebhookResult, error) {
	payloads, err := decodeWebhookPayloads(request.Body)
	if err != nil {
		return nil, err
	}
	result := &ProviderWebhookResult{Provider: provider, Received: true, Events: len(payloads)}
	for _, payload := range payloads {
		update, ok := build(payload)
		if !ok {
			result.Ignored++
			// 认不出来的报文要留下线索：这类回执在界面上完全看不出「来过」，
			// 只有日志里这一行能说明「东西到了，但字段对不上」。
			s.log.Info("email webhook payload not recognized",
				zap.String("provider", provider), zap.Int64("appid", request.AppID),
				zap.Strings("keys", payloadKeys(payload)))
			continue
		}
		update.AppID = request.AppID
		matched, err := s.pg.ApplyEmailDeliveryEvent(ctx, update)
		if err != nil {
			return nil, err
		}
		if matched {
			result.Matched++
		}
	}
	return result, nil
}

// decodeWebhookPayloads 同时接受单个对象与对象数组。
func decodeWebhookPayloads(body []byte) ([]map[string]any, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return nil, apperrors.New(40074, http.StatusBadRequest, "回调报文为空")
	}
	if strings.HasPrefix(trimmed, "[") {
		var items []map[string]any
		if err := json.Unmarshal(body, &items); err != nil {
			return nil, apperrors.New(40074, http.StatusBadRequest, "回调报文解析失败："+clampDetail(err.Error()))
		}
		return items, nil
	}
	var single map[string]any
	if err := json.Unmarshal(body, &single); err != nil {
		return nil, apperrors.New(40074, http.StatusBadRequest, "回调报文解析失败："+clampDetail(err.Error()))
	}
	return []map[string]any{single}, nil
}

// lookupString 按别名集合取字符串值，**忽略大小写与 `_`/`-` 分隔符**，
// 并会下钻一层常见的包装键。
//
// 之所以不钉死一种拼写：阿里云与腾讯云的回执字段在不同投递方式与不同版本的文档里
// 有 camelCase / snake_case / PascalCase 三种写法，钉死一种的代价是换个投递方式
// 就一条也匹配不上 —— 而那个失败是静默的（HTTP 200，零条匹配）。
func lookupString(payload map[string]any, aliases ...string) string {
	if len(payload) == 0 {
		return ""
	}
	normalized := make(map[string]any, len(payload))
	for key, value := range payload {
		normalized[normalizeLookupKey(key)] = value
	}
	for _, alias := range aliases {
		if value, ok := normalized[normalizeLookupKey(alias)]; ok {
			if text := stringifyScalar(value); text != "" {
				return text
			}
		}
	}
	// 下钻一层：不少服务商把正文裹在 data / event-data 里。
	for _, wrapper := range []string{"data", "eventdata", "event"} {
		nested, ok := normalized[wrapper].(map[string]any)
		if !ok {
			continue
		}
		if value := lookupString(nested, aliases...); value != "" {
			return value
		}
	}
	return ""
}

func normalizeLookupKey(key string) string {
	var builder strings.Builder
	for _, r := range key {
		if r == '_' || r == '-' || r == ' ' {
			continue
		}
		builder.WriteRune(r)
	}
	return strings.ToLower(builder.String())
}

// stringifyScalar 只接受标量。数值走 %v 会把 1.6e+09 这种科学计数法带进来，
// 而时间戳恰恰是最常见的数值字段，因此整数单独处理。
func stringifyScalar(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(typed)
	case json.Number:
		return typed.String()
	default:
		return ""
	}
}

// normalizeEventToken 归一化事件名：取最后一段并统一小写。
// `email.delivered` / `Email_Delivered` / `DELIVERED` 是同一件事。
func normalizeEventToken(raw string) string {
	token := strings.ToLower(strings.TrimSpace(raw))
	if index := strings.LastIndexAny(token, "._-"); index >= 0 && index+1 < len(token) {
		token = token[index+1:]
	}
	return token
}

// payloadKeys 认不出报文时打进日志的线索，排序后输出以便逐次对比。
func payloadKeys(payload map[string]any) []string {
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func markDelivered(update pgrepo.EmailDeliveryStatusUpdate) pgrepo.EmailDeliveryStatusUpdate {
	update.Event = emaildomain.EventDelivery
	update.Status = emaildomain.DeliveryStatusDelivered
	update.MarkDelivered = true
	return update
}

func markBounced(update pgrepo.EmailDeliveryStatusUpdate, reason string) pgrepo.EmailDeliveryStatusUpdate {
	update.Event = emaildomain.EventBounce
	update.Status = emaildomain.DeliveryStatusBounced
	update.ErrorMessage = withReason("邮件被退回", reason)
	return update
}

func markComplained(update pgrepo.EmailDeliveryStatusUpdate) pgrepo.EmailDeliveryStatusUpdate {
	update.Event = emaildomain.EventComplaint
	update.Status = emaildomain.DeliveryStatusComplained
	update.ErrorMessage = "收件人投诉为垃圾邮件"
	return update
}

// guardWebhookReplay 用一次性 nonce 挡住重放。
//
// 只对**签名不覆盖报文体**的服务商需要（目前是 Mailgun：它的签名只覆盖
// timestamp + token）。一条被截获的回调在 5 分钟窗口内可以原样重放任意多次，
// 而每次重放都会再推进一次状态、再累加一次打开数。
//
// Redis 不可用时放行并记 warn：拒收会让一次缓存抖动变成整批回执丢失，
// 而回执丢失是不可补的 —— 服务商不会因为你 5xx 就无限重推。
func (s *EmailService) guardWebhookReplay(ctx context.Context, provider string, appID int64, nonce string) error {
	nonce = strings.TrimSpace(nonce)
	if nonce == "" || s.redis == nil {
		return nil
	}
	key := fmt.Sprintf("%s:email:webhook:%s:%d:%s", s.keyPrefix, provider, appID, nonce)
	ok, err := s.redis.SetNX(ctx, key, "1", webhookTolerance).Result()
	if err != nil {
		s.log.Warn("email webhook replay guard unavailable, accepting anyway",
			zap.String("provider", provider), zap.Error(err))
		return nil
	}
	if !ok {
		return apperrors.New(40176, http.StatusUnauthorized, "该回执已经处理过，按重放拒绝")
	}
	return nil
}


// ── 共用工具 ──

// webhookTolerance 时间戳容忍窗口，三家共用。
// 签名本身不带有效期，卡住时间戳才能让被截获的回调无法在事后重放。
const webhookTolerance = 5 * time.Minute

// checkEventTimestampDrift 同时接受 Unix 秒与 RFC3339，前者是多数服务商的规格，后者是 SNS 的。
func checkEventTimestampDrift(timestamp string) error {
	timestamp = strings.TrimSpace(timestamp)
	if timestamp == "" {
		return apperrors.New(40176, http.StatusUnauthorized, "回调缺少时间戳")
	}
	var issuedAt time.Time
	if seconds, err := strconv.ParseInt(timestamp, 10, 64); err == nil {
		issuedAt = time.Unix(seconds, 0)
	} else if parsed, parseErr := time.Parse(time.RFC3339, timestamp); parseErr == nil {
		issuedAt = parsed
	} else if parsed, parseErr := time.Parse("2006-01-02T15:04:05.000Z", timestamp); parseErr == nil {
		issuedAt = parsed
	} else {
		return apperrors.New(40176, http.StatusUnauthorized, "回调时间戳格式非法")
	}
	drift := timeutil.Now().Sub(issuedAt)
	if drift < 0 {
		drift = -drift
	}
	if drift > webhookTolerance {
		return apperrors.New(40176, http.StatusUnauthorized,
			fmt.Sprintf("回调时间戳超出 %.0f 分钟容忍窗口，已按重放攻击拒绝", webhookTolerance.Minutes()))
	}
	return nil
}

func parseEventTime(value string) time.Time {
	value = strings.TrimSpace(value)
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano, "2006-01-02T15:04:05.000Z"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed
		}
	}
	return timeutil.Now()
}

// describeMapReason 从事件的细节对象里挑出最能说明原因的字段。
func describeMapReason(data map[string]any, fallback string) string {
	if len(data) == 0 {
		return fallback
	}
	parts := make([]string, 0, 3)
	for _, key := range []string{"type", "subType", "message", "reason", "diagnosticCode"} {
		value, ok := data[key]
		if !ok {
			continue
		}
		if text := strings.TrimSpace(fmt.Sprintf("%v", value)); text != "" {
			parts = append(parts, key+"="+text)
		}
		if len(parts) >= 3 {
			break
		}
	}
	if len(parts) == 0 {
		return fallback
	}
	return fallback + "（" + strings.Join(parts, ", ") + "）"
}

// withReason 把服务商给的原因接在中文说明后面，全空时只留说明。
//
// 不写成通用的 firstNonEmpty：那样在 reason 为空时会拼出
// 「邮件被退回：」这种以冒号结尾的半句话，而它会原样进投递记录列表。
func withReason(message string, candidates ...string) string {
	for _, candidate := range candidates {
		if trimmed := clampDetail(candidate); trimmed != "" {
			return message + "：" + trimmed
		}
	}
	return message
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
