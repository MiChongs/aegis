package service

import (
	emaildomain "aegis/internal/domain/email"
	"aegis/pkg/circuitbreaker"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/resilience"
	"aegis/pkg/timeutil"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	mailgunlib "github.com/mailgun/mailgun-go/v5"
	"github.com/mailgun/mailgun-go/v5/mtypes"
	"go.uber.org/zap"
)

const mailgunHTTPTimeout = 20

// mailgunEmailSender 走 Mailgun 的官方 Go SDK。
type mailgunEmailSender struct {
	log *zap.Logger
}

func newMailgunEmailSender(log *zap.Logger) *mailgunEmailSender {
	return &mailgunEmailSender{log: log}
}

func (s *mailgunEmailSender) Provider() string { return emaildomain.ProviderMailgun }

func (s *mailgunEmailSender) SupportsAttachments() bool { return true }

func (s *mailgunEmailSender) Describe() emaildomain.ProviderMeta {
	return emaildomain.ProviderMeta{
		Provider:    emaildomain.ProviderMailgun,
		Name:        "Mailgun",
		Description: "按域名维度管理的事务邮件服务，日志与事件查询能力强。",
		Category:    emaildomain.CategoryInternational,
		Icon:        "mailgun",
		BrandColor:  "#F0654E",
		DocURL:      "https://documentation.mailgun.com/docs/mailgun/api-reference/openapi-final/tag/Messages/",
		Capabilities: emaildomain.ProviderCapabilities{
			Attachments: true,
			Webhook:     true,
			Tags:        true,
			Tracking:    true,
		},
		WebhookPath: "/api/email/webhook/mailgun/{scope}/{config}",
		WebhookNote: "在 Sending → Webhooks 里为 delivered / permanent_fail / temporary_fail / complained / opened / clicked 各填一次这个地址。",
		Notes: []string{
			"**地域必须与域名所在区一致**：欧盟区域名走美国端点会直接 404，而那句报错完全看不出是地域配错了。",
			"沙箱域名（sandbox*.mailgun.org）只能发给已授权的收件人。",
			"Mailgun 的标签是**纯字符串**而不是键值对，平台会把键值对拼成 `键:值` 后上报，单条上限 128 字符。",
		},
		Fields: emFields(
			emIn(emaildomain.GroupCredential,
				emSecret("apiKey", "API Key", "key-... 或 Sending API Key", "在 Mailgun 控制台的 API Security 中获取", true),
				emText("domain", "发信域名", "mg.example.com", "Mailgun 控制台里那个已验证的 Sending Domain", true),
				emSelect("region", "地域", "与创建域名时选择的区一致，选错会得到一个看不出原因的 404", mailgunRegionUS,
					emOption(mailgunRegionUS, "美国（api.mailgun.net）"),
					emOption(mailgunRegionEU, "欧盟（api.eu.mailgun.net）"),
				),
			),
			senderIdentityFields("必须属于上面那个发信域名"),
			emIn(emaildomain.GroupWebhook,
				webhookSecretField("Webhook 签名密钥", "在 API Security 页的 HTTP webhook signing key",
					"Mailgun 全账号共用一个签名密钥"),
			),
			emIn(emaildomain.GroupAdvanced,
				tagsField("拼成 `键:值` 后作为 Mailgun 标签上报；平台会自动补一个 purpose 标签"),
			),
		),
	}
}

const (
	mailgunRegionUS = "us"
	mailgunRegionEU = "eu"
)

func (s *mailgunEmailSender) Validate(config emaildomain.Config) error {
	if err := validateByCatalog(s.Describe(), config); err != nil {
		return err
	}
	if !config.HasSecret("apiKey") {
		return apperrors.New(40068, http.StatusBadRequest, "Mailgun API Key 不能为空")
	}
	if config.Setting("domain") == "" {
		return apperrors.New(40078, http.StatusBadRequest, "Mailgun 发信域名不能为空")
	}
	return nil
}

func (s *mailgunEmailSender) Send(ctx context.Context, config *emaildomain.Config, out emailOutbound) (emailSendResult, error) {
	apiKey := config.Secret("apiKey")
	if apiKey == "" {
		return emailSendResult{}, apperrors.New(40068, http.StatusBadRequest, "Mailgun API Key 未配置或解密失败，请重新填写")
	}
	domain := config.Setting("domain")

	client := mailgunlib.NewMailgun(apiKey)
	client.SetHTTPClient(newEmailHTTPClient("email.mailgun", mailgunHTTPTimeout))
	if strings.EqualFold(config.Setting("region"), mailgunRegionEU) {
		if err := client.SetAPIBase(mailgunlib.APIBaseEU); err != nil {
			return emailSendResult{}, apperrors.New(50060, http.StatusInternalServerError, "设置 Mailgun 端点失败："+err.Error())
		}
	}

	fromAddress, fromName, replyTo := config.SenderIdentity()
	message := mailgunlib.NewMessage(domain,
		formatMailAddress(fromAddress, fromName),
		truncateSubject(out.Subject),
		out.Text,
		strings.TrimSpace(out.To))
	message.SetHTML(out.HTML)
	if replyTo != "" {
		message.SetReplyTo(replyTo)
	}
	for _, attachment := range out.Attachments {
		message.AddBufferAttachment(attachment.Filename, attachment.Content)
	}
	if tags := mailgunTags(config.SettingMap(emaildomain.KeyTags), out.Purpose); len(tags) > 0 {
		// 标签超过 Mailgun 的条数上限时它会整个请求报错。这里失败只记日志、不阻断发信 ——
		// 一封验证码邮件不该因为一个用于统计的标签而发不出去。
		if err := message.AddTag(tags...); err != nil {
			s.log.Warn("mailgun tag rejected, sending without tags",
				zap.Int64("appid", config.AppID), zap.Error(err))
		}
	}

	breakerName := circuitbreaker.Name("email-mailgun", fmt.Sprintf("scope-%d", config.AppID), config.Name)
	response, err := resilience.Execute(ctx, breakerName, resilience.Options{
		Timeout:     timeutil.Seconds(mailgunHTTPTimeout),
		MaxRetries:  2,
		BaseBackoff: timeutil.Milliseconds(300),
		MaxBackoff:  timeutil.Milliseconds(1500),
		ShouldRetry: mailgunShouldRetry,
	}, func(attemptCtx context.Context) (mtypes.SendMessageResponse, error) {
		return client.Send(attemptCtx, message)
	})
	if err != nil {
		s.log.Error("mailgun email send failed",
			zap.Int64("appid", config.AppID), zap.String("config", config.Name), zap.Error(err))
		return emailSendResult{}, wrapMailgunError(err)
	}

	return emailSendResult{
		// Mailgun 返回的 ID 形如 <20240101...@mg.example.com>，它既是 RFC 5322 的
		// Message-ID，也是事件回执里的关联键（事件里那份**不带尖括号**）。
		// 两处都存下来，回填时按去掉尖括号的形态匹配。
		MessageID:         response.ID,
		ProviderMessageID: strings.Trim(strings.TrimSpace(response.ID), "<>"),
		Status:            emaildomain.DeliveryStatusPending,
	}, nil
}

// mailgunTags 把键值对拼成 Mailgun 认的纯字符串标签。
// 超长的直接丢掉：Mailgun 对超长标签是整个请求报错，而不是截断。
func mailgunTags(configured map[string]string, purpose string) []string {
	merged := buildProviderTags(configured, purpose)
	if len(merged) == 0 {
		return nil
	}
	tags := make([]string, 0, len(merged))
	for key, value := range merged {
		tag := key
		if value != "" {
			tag = key + ":" + value
		}
		if len(tag) > 128 {
			continue
		}
		tags = append(tags, tag)
	}
	return tags
}

func mailgunShouldRetry(err error) bool {
	if status := mailgunlib.GetStatusFromErr(err); status == http.StatusTooManyRequests ||
		(status >= 400 && status < 500) {
		return false
	}
	return resilience.RetryableError(err)
}

func wrapMailgunError(err error) error {
	if circuitbreaker.IsOpenError(err) {
		return apperrors.New(50314, http.StatusServiceUnavailable, "邮件服务暂不可用，Mailgun 通道正在熔断保护中，请稍后重试")
	}
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	detail := clampDetail(err.Error())
	switch mailgunlib.GetStatusFromErr(err) {
	case http.StatusUnauthorized:
		return apperrors.New(40071, http.StatusUnauthorized, withDetail("Mailgun API Key 无效", detail))
	case http.StatusForbidden:
		return apperrors.New(40072, http.StatusForbidden,
			withDetail("Mailgun 拒绝发信：API Key 无该域名权限，或账号未完成付费验证", detail))
	case http.StatusNotFound:
		return apperrors.New(40473, http.StatusNotFound,
			withDetail("Mailgun 找不到该发信域名 —— 最常见的原因是**地域选错了**（欧盟区域名必须选欧盟端点）", detail))
	case http.StatusBadRequest:
		return apperrors.New(40070, http.StatusBadRequest,
			withDetail("Mailgun 拒绝了本次请求（沙箱域名只能发给已授权收件人，或参数校验未通过）", detail))
	case http.StatusTooManyRequests:
		return apperrors.New(42974, http.StatusTooManyRequests, withDetail("Mailgun 限频或配额已用尽", detail))
	default:
		return apperrors.New(50060, http.StatusBadGateway, withDetail("调用 Mailgun 接口失败", detail))
	}
}
