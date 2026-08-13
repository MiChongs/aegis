package service

import (
	emaildomain "aegis/internal/domain/email"
	"aegis/pkg/circuitbreaker"
	"aegis/pkg/egress"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/resilience"
	"aegis/pkg/timeutil"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	resendlib "github.com/resend/resend-go/v2"
	"go.uber.org/zap"
)

const resendHTTPTimeout = 15

// resendEmailSender 走 Resend 的官方 Go SDK。
type resendEmailSender struct {
	log *zap.Logger
}

func newResendEmailSender(log *zap.Logger) *resendEmailSender {
	return &resendEmailSender{log: log}
}

func (s *resendEmailSender) Provider() string { return emaildomain.ProviderResend }

func (s *resendEmailSender) SupportsAttachments() bool { return true }

func (s *resendEmailSender) Describe() emaildomain.ProviderMeta {
	return emaildomain.ProviderMeta{
		Provider:    emaildomain.ProviderResend,
		Name:        "Resend",
		Description: "面向开发者的事务邮件服务，接入最简单：一个 API Key 加一个已验证域名即可。",
		Category:    emaildomain.CategoryInternational,
		Icon:        "resend",
		BrandColor:  "#000000",
		DocURL:      "https://resend.com/docs/api-reference/emails/send-email",
		Capabilities: emaildomain.ProviderCapabilities{
			Attachments: true,
			Webhook:     true,
			Tags:        true,
			Tracking:    true,
		},
		WebhookPath: "/api/email/webhook/resend/{scope}/{config}",
		WebhookNote: "在 Resend 控制台的 Webhooks 里新建一个端点指向这个地址，勾上 email.* 全部事件。",
		Notes: []string{
			"免费额度每天 100 封、每月 3000 封；超出后接口直接返回 429。",
			"未验证域名只能发给注册 Resend 时用的那个邮箱，测试时很容易被这一条卡住。",
			"标签的键与值只允许 ASCII 字母、数字与下划线，平台会自动过滤掉不合规的标签。",
		},
		Fields: emFields(
			emIn(emaildomain.GroupCredential,
				emSecret("apiKey", "API Key", "re_...", "在 Resend 控制台的 API Keys 中创建，需要 Sending access 权限", true),
			),
			senderIdentityFields("必须属于已在 Resend 完成验证的域名"),
			emIn(emaildomain.GroupWebhook,
				webhookSecretField("Webhook 签名密钥", "whsec_...", "新建 Webhook 端点后 Resend 会给出的 Signing Secret"),
			),
			emIn(emaildomain.GroupAdvanced,
				tagsField("随邮件上报给 Resend，可在其控制台按标签筛选；平台会自动补一个 purpose 标签"),
			),
		),
	}
}

func (s *resendEmailSender) Validate(config emaildomain.Config) error {
	if err := validateByCatalog(s.Describe(), config); err != nil {
		return err
	}
	if !config.HasSecret("apiKey") {
		return apperrors.New(40068, http.StatusBadRequest, "Resend API Key 不能为空")
	}
	return nil
}

func (s *resendEmailSender) Send(ctx context.Context, config *emaildomain.Config, out emailOutbound) (emailSendResult, error) {
	apiKey := config.Secret("apiKey")
	if apiKey == "" {
		return emailSendResult{}, apperrors.New(40068, http.StatusBadRequest, "Resend API Key 未配置或解密失败，请重新填写")
	}
	client := resendlib.NewCustomClient(
		egress.NewClient(egress.Profile{Name: "email.resend", Timeout: timeutil.Seconds(resendHTTPTimeout)}),
		apiKey)

	fromAddress, fromName, replyTo := config.SenderIdentity()
	params := &resendlib.SendEmailRequest{
		From:        formatMailAddress(fromAddress, fromName),
		To:          []string{strings.TrimSpace(out.To)},
		Subject:     truncateSubject(out.Subject),
		Html:        out.HTML,
		Text:        out.Text,
		ReplyTo:     replyTo,
		Tags:        resendTags(config.SettingMap(emaildomain.KeyTags), out.Purpose),
		Attachments: resendAttachments(out.Attachments),
	}

	breakerName := circuitbreaker.Name("email-resend", fmt.Sprintf("scope-%d", config.AppID), config.Name)
	response, err := resilience.Execute(ctx, breakerName, resilience.Options{
		Timeout:     timeutil.Seconds(resendHTTPTimeout),
		MaxRetries:  2,
		BaseBackoff: timeutil.Milliseconds(300),
		MaxBackoff:  timeutil.Milliseconds(1500),
		ShouldRetry: resendShouldRetry,
	}, func(attemptCtx context.Context) (*resendlib.SendEmailResponse, error) {
		return client.Emails.SendWithContext(attemptCtx, params)
	})
	if err != nil {
		s.log.Error("resend email send failed",
			zap.Int64("appid", config.AppID), zap.String("config", config.Name), zap.Error(err))
		return emailSendResult{}, wrapResendError(err)
	}
	if response == nil || strings.TrimSpace(response.Id) == "" {
		return emailSendResult{}, apperrors.New(50060, http.StatusBadGateway, "Resend 未返回邮件 ID，无法追踪投递状态")
	}
	return emailSendResult{
		ProviderMessageID: response.Id,
		// Resend 接收即返回，投递结果由 webhook 回填。
		Status: emaildomain.DeliveryStatusPending,
	}, nil
}

func resendAttachments(attachments []emailAttachment) []*resendlib.Attachment {
	if len(attachments) == 0 {
		return nil
	}
	out := make([]*resendlib.Attachment, 0, len(attachments))
	for _, attachment := range attachments {
		out = append(out, &resendlib.Attachment{
			Content:     attachment.Content,
			Filename:    attachment.Filename,
			ContentType: attachment.ContentType,
		})
	}
	return out
}

func resendTags(configured map[string]string, purpose string) []resendlib.Tag {
	merged := buildProviderTags(configured, purpose)
	if len(merged) == 0 {
		return nil
	}
	tags := make([]resendlib.Tag, 0, len(merged))
	for key, value := range merged {
		// Resend 的标签字符集比 SES 还严（只认 ASCII 字母数字与下划线），
		// 不合规时整个请求 422。过滤掉比让一封验证码邮件因为标签而发不出去好。
		if !sesTagPattern.MatchString(key) || !sesTagPattern.MatchString(value) {
			continue
		}
		tags = append(tags, resendlib.Tag{Name: key, Value: value})
	}
	if len(tags) == 0 {
		return nil
	}
	return tags
}

// resendShouldRetry 限频不重试：Resend 的免费额度按天与按月计，
// 重试只会把剩余额度烧得更快。SDK 为此提供了 ErrRateLimit 哨兵。
func resendShouldRetry(err error) bool {
	if errors.Is(err, resendlib.ErrRateLimit) {
		return false
	}
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) && appErr.HTTPStatus == http.StatusTooManyRequests {
		return false
	}
	return resilience.RetryableError(err)
}

func wrapResendError(err error) error {
	if circuitbreaker.IsOpenError(err) {
		return apperrors.New(50314, http.StatusServiceUnavailable, "邮件服务暂不可用，Resend 通道正在熔断保护中，请稍后重试")
	}
	var rateLimitErr *resendlib.RateLimitError
	if errors.As(err, &rateLimitErr) {
		return apperrors.New(42974, http.StatusTooManyRequests,
			withDetail("Resend 限频或配额已用尽（免费档每天 100 封、每月 3000 封）", rateLimitErr.Message))
	}
	// SDK 把 API 错误原样包成 error，只能按报文里的关键词分类。
	// 分类的价值在于把「该改配置」和「该等一会儿」分开 —— 前者重试一万次也没用。
	detail := clampDetail(err.Error())
	lower := strings.ToLower(detail)
	switch {
	case strings.Contains(lower, "api key is invalid") || strings.Contains(lower, "restricted_api_key") ||
		strings.Contains(lower, "missing_api_key") || strings.Contains(lower, "unauthorized"):
		return apperrors.New(40071, http.StatusUnauthorized, withDetail("Resend API Key 无效或权限不足", detail))
	case strings.Contains(lower, "domain is not verified") || strings.Contains(lower, "not_verified"):
		return apperrors.New(40070, http.StatusBadRequest,
			withDetail("发件域名尚未在 Resend 完成验证；未验证时只能发给注册账号用的那个邮箱", detail))
	case strings.Contains(lower, "validation_error") || strings.Contains(lower, "invalid_") ||
		strings.Contains(lower, "422"):
		return apperrors.New(40070, http.StatusBadRequest, withDetail("Resend 拒绝了本次请求（参数校验未通过）", detail))
	default:
		return apperrors.New(50060, http.StatusBadGateway, withDetail("调用 Resend 接口失败", detail))
	}
}
