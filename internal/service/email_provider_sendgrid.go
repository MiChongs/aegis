package service

import (
	emaildomain "aegis/internal/domain/email"
	"aegis/pkg/circuitbreaker"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/resilience"
	"aegis/pkg/timeutil"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	sendgridlib "github.com/sendgrid/sendgrid-go"
	sgmail "github.com/sendgrid/sendgrid-go/helpers/mail"
	"go.uber.org/zap"
)

const (
	sendgridHTTPTimeout = 15
	sendgridSendPath    = "/v3/mail/send"
)

// sendgridEmailSender 走 Twilio SendGrid 的官方 Go SDK（helpers/mail 组装 v3 报文）。
//
// SDK 的 sendgrid.API() 用的是包级默认 HTTP 客户端，出海代理网关接不进去，
// 因此这里只用它组装与解析报文，请求本身走平台统一的出网客户端。
// 这样 SendGrid 与其它服务商共用同一张域名路由表 —— 少了这一步，
// 开了出海代理的部署里会出现「别的服务商都能发、只有 SendGrid 超时」。
type sendgridEmailSender struct {
	log    *zap.Logger
	client *http.Client
}

func newSendGridEmailSender(log *zap.Logger) *sendgridEmailSender {
	return &sendgridEmailSender{
		log:    log,
		client: newEmailHTTPClient("email.sendgrid", sendgridHTTPTimeout),
	}
}

func (s *sendgridEmailSender) Provider() string { return emaildomain.ProviderSendGrid }

func (s *sendgridEmailSender) SupportsAttachments() bool { return true }

func (s *sendgridEmailSender) Describe() emaildomain.ProviderMeta {
	return emaildomain.ProviderMeta{
		Provider:    emaildomain.ProviderSendGrid,
		Name:        "SendGrid",
		Description: "Twilio SendGrid，老牌事务邮件服务，配额与送达率报表完善。",
		Category:    emaildomain.CategoryInternational,
		Icon:        "twilio",
		BrandColor:  "#1A82E2",
		DocURL:      "https://www.twilio.com/docs/sendgrid/api-reference/mail-send/mail-send",
		Capabilities: emaildomain.ProviderCapabilities{
			Attachments: true,
			Webhook:     true,
			Tags:        true,
			Tracking:    true,
		},
		WebhookPath: "/api/email/webhook/sendgrid/{scope}/{config}",
		WebhookNote: "在 Settings → Mail Settings → Event Webhook 里填这个地址，并**打开 Signed Event Webhook**，把公钥填到下面。",
		Notes: []string{
			"发件地址必须完成 Single Sender Verification 或域名认证，否则一律 403。",
			"事件回执要求打开 Signed Event Webhook：不签名的回执可被任意伪造，平台会拒收。",
		},
		Fields: emFields(
			emIn(emaildomain.GroupCredential,
				emSecret("apiKey", "API Key", "SG....", "需要 Mail Send 权限；SendGrid 只在创建时显示一次", true),
			),
			senderIdentityFields("需完成 Single Sender Verification 或所属域名已认证"),
			emIn(emaildomain.GroupWebhook,
				emaildomain.ConfigField{
					Key: "webhookPublicKey", Label: "事件签名公钥", Type: emaildomain.FieldTextarea,
					Group: emaildomain.GroupWebhook,
					Placeholder: "MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE...",
					Help: "打开 Signed Event Webhook 后 SendGrid 给出的 base64 公钥（ECDSA P-256）；" +
						"未配置时回调一律拒收 —— 无法验签的回执可被任意伪造",
				},
			),
			emIn(emaildomain.GroupAdvanced,
				emText("ipPoolName", "IP 池", "transactional", "把这条通道的信固定到某个专用 IP 池，留空即用默认池", false),
				emSwitch("sandbox", "沙箱模式", "打开后 SendGrid 只做校验不真的投递，用来验证配置是否正确", false),
				tagsField("作为 SendGrid 的 custom_args 上报，可在事件回执里原样取回；平台会自动补一个 purpose"),
			),
		),
	}
}

func (s *sendgridEmailSender) Validate(config emaildomain.Config) error {
	if err := validateByCatalog(s.Describe(), config); err != nil {
		return err
	}
	if !config.HasSecret("apiKey") {
		return apperrors.New(40068, http.StatusBadRequest, "SendGrid API Key 不能为空")
	}
	return nil
}

func (s *sendgridEmailSender) Send(ctx context.Context, config *emaildomain.Config, out emailOutbound) (emailSendResult, error) {
	apiKey := config.Secret("apiKey")
	if apiKey == "" {
		return emailSendResult{}, apperrors.New(40068, http.StatusBadRequest, "SendGrid API Key 未配置或解密失败，请重新填写")
	}

	fromAddress, fromName, replyTo := config.SenderIdentity()
	message := sgmail.NewV3Mail()
	message.SetFrom(sgmail.NewEmail(fromName, fromAddress))
	message.Subject = truncateSubject(out.Subject)
	if replyTo != "" {
		message.SetReplyTo(sgmail.NewEmail("", replyTo))
	}

	personalization := sgmail.NewPersonalization()
	personalization.AddTos(sgmail.NewEmail("", strings.TrimSpace(out.To)))
	message.AddPersonalizations(personalization)

	// 纯文本必须排在 HTML 之前：SendGrid 按 content 数组顺序决定 multipart/alternative
	// 的段序，而 RFC 2046 规定「最后一段是最优先展示的」。顺序反了的话，
	// 纯文本客户端固然还能看，但富文本客户端也可能挑中纯文本那一段。
	if text := strings.TrimSpace(out.Text); text != "" {
		message.AddContent(sgmail.NewContent("text/plain", text))
	}
	message.AddContent(sgmail.NewContent("text/html", out.HTML))

	for _, attachment := range out.Attachments {
		file := sgmail.NewAttachment()
		file.SetContent(base64.StdEncoding.EncodeToString(attachment.Content))
		file.SetFilename(attachment.Filename)
		file.SetDisposition("attachment")
		if contentType := strings.TrimSpace(attachment.ContentType); contentType != "" {
			file.SetType(contentType)
		}
		message.AddAttachment(file)
	}

	for key, value := range buildProviderTags(config.SettingMap(emaildomain.KeyTags), out.Purpose) {
		message.SetCustomArg(key, value)
	}
	if pool := config.Setting("ipPoolName"); pool != "" {
		message.SetIPPoolID(pool)
	}
	if config.SettingBool("sandbox") {
		settings := sgmail.NewMailSettings()
		settings.SetSandboxMode(sgmail.NewSetting(true))
		message.SetMailSettings(settings)
	}

	body := sgmail.GetRequestBody(message)
	breakerName := circuitbreaker.Name("email-sendgrid", fmt.Sprintf("scope-%d", config.AppID), config.Name)
	messageID, err := resilience.Execute(ctx, breakerName, resilience.Options{
		Timeout:     timeutil.Seconds(sendgridHTTPTimeout),
		MaxRetries:  2,
		BaseBackoff: timeutil.Milliseconds(300),
		MaxBackoff:  timeutil.Milliseconds(1500),
		ShouldRetry: sendgridShouldRetry,
	}, func(attemptCtx context.Context) (string, error) {
		return s.postSend(attemptCtx, apiKey, body)
	})
	if err != nil {
		s.log.Error("sendgrid email send failed",
			zap.Int64("appid", config.AppID), zap.String("config", config.Name), zap.Error(err))
		return emailSendResult{}, wrapSendGridError(err)
	}
	return emailSendResult{
		ProviderMessageID: messageID,
		// SendGrid 返回 202 Accepted，真正的投递结果由 Event Webhook 回填。
		Status: emaildomain.DeliveryStatusPending,
	}, nil
}

func (s *sendgridEmailSender) postSend(ctx context.Context, apiKey string, body []byte) (string, error) {
	// 用 SDK 的 GetRequest 拿到官方端点与鉴权头，避免把 host 与头名写死在这里。
	sgRequest := sendgridlib.GetRequest(apiKey, sendgridSendPath, "")
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, sgRequest.BaseURL, strings.NewReader(string(body)))
	if err != nil {
		return "", apperrors.New(50060, http.StatusInternalServerError, "构造 SendGrid 请求失败")
	}
	for key, value := range sgRequest.Headers {
		request.Header.Set(key, value)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := s.client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	raw := readLimitedBody(response.Body)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", classifySendGridHTTPError(response.StatusCode, raw)
	}
	// SendGrid 的邮件 ID 只在响应头里，且事件回执用的是它的前缀部分
	// （`X-Message-Id` 形如 `abc123`，事件里的 sg_message_id 是 `abc123.filterdrecv...`）。
	// 留痕存前缀，回填时按前缀匹配，见 email_webhook_providers.go。
	return strings.TrimSpace(response.Header.Get("X-Message-Id")), nil
}

func classifySendGridHTTPError(statusCode int, raw []byte) error {
	detail := clampDetail(sendgridErrorDetail(raw))
	switch statusCode {
	case http.StatusUnauthorized:
		return apperrors.New(40071, http.StatusUnauthorized, withDetail("SendGrid API Key 无效或已被吊销", detail))
	case http.StatusForbidden:
		return apperrors.New(40072, http.StatusForbidden,
			withDetail("SendGrid 拒绝发信：发件地址未通过 Single Sender Verification / 域名认证，或 API Key 缺少 Mail Send 权限", detail))
	case http.StatusBadRequest:
		return apperrors.New(40070, http.StatusBadRequest, withDetail("SendGrid 拒绝了本次请求（参数校验未通过）", detail))
	case http.StatusRequestEntityTooLarge:
		return apperrors.New(40070, http.StatusBadRequest, withDetail("邮件体积超过 SendGrid 上限（含附件 30MB）", detail))
	case http.StatusTooManyRequests:
		return apperrors.New(42974, http.StatusTooManyRequests, withDetail("SendGrid 限频或配额已用尽", detail))
	default:
		if statusCode >= 500 {
			return apperrors.New(50060, http.StatusBadGateway, withDetail(fmt.Sprintf("SendGrid 返回 %d，请稍后重试", statusCode), detail))
		}
		return apperrors.New(50060, http.StatusBadGateway, withDetail(fmt.Sprintf("SendGrid 发送失败（HTTP %d）", statusCode), detail))
	}
}

// sendgridErrorDetail 从 {"errors":[{"message":..,"field":..}]} 里拼出可读的原因。
func sendgridErrorDetail(raw []byte) string {
	var parsed struct {
		Errors []struct {
			Message string `json:"message"`
			Field   string `json:"field"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || len(parsed.Errors) == 0 {
		return string(raw)
	}
	parts := make([]string, 0, len(parsed.Errors))
	for _, item := range parsed.Errors {
		if field := strings.TrimSpace(item.Field); field != "" {
			parts = append(parts, field+": "+item.Message)
			continue
		}
		parts = append(parts, item.Message)
	}
	return strings.Join(parts, "; ")
}

func sendgridShouldRetry(err error) bool {
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) && appErr.HTTPStatus == http.StatusTooManyRequests {
		return false
	}
	return resilience.RetryableError(err)
}

func wrapSendGridError(err error) error {
	if circuitbreaker.IsOpenError(err) {
		return apperrors.New(50314, http.StatusServiceUnavailable, "邮件服务暂不可用，SendGrid 通道正在熔断保护中，请稍后重试")
	}
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	return apperrors.New(50060, http.StatusBadGateway, "调用 SendGrid 接口失败："+err.Error())
}
