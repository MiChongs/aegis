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
	"regexp"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	sestypes "github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"github.com/aws/smithy-go"
	"go.uber.org/zap"
)

const sesHTTPTimeout = 20

// sesEmailSender 走 AWS Simple Email Service v2 的官方 SDK。
//
// 用 SendEmail 的 **Raw** 内容而不是 Simple：Simple 内容没有附件字段，
// 而凭证 PDF 是这条通道的主要用途之一。报文由 go-mail 组装
// （见 buildRawMIME），中文主题的编码、长行折叠、附件文件名这三处
// 各错一次的坑它已经踩完了。
type sesEmailSender struct {
	log *zap.Logger
}

func newSESEmailSender(log *zap.Logger) *sesEmailSender {
	return &sesEmailSender{log: log}
}

func (s *sesEmailSender) Provider() string { return emaildomain.ProviderSES }

func (s *sesEmailSender) SupportsAttachments() bool { return true }

func (s *sesEmailSender) Describe() emaildomain.ProviderMeta {
	return emaildomain.ProviderMeta{
		Provider:    emaildomain.ProviderSES,
		Name:        "AWS SES",
		Description: "Amazon Simple Email Service v2，按量计费、配额可申请提额，适合大批量事务邮件。",
		Category:    emaildomain.CategoryInternational,
		Icon:        "amazonwebservices",
		BrandColor:  "#FF9900",
		DocURL:      "https://docs.aws.amazon.com/ses/latest/dg/send-email-api.html",
		Capabilities: emaildomain.ProviderCapabilities{
			Attachments: true,
			Webhook:     true,
			Tags:        true,
			Tracking:    true,
		},
		WebhookPath: "/api/email/webhook/ses/{scope}/{config}",
		WebhookNote: "在 SES 配置集里建一个 SNS 事件目标，订阅这个地址；SNS 的订阅确认请求平台会自动回执。",
		Notes: []string{
			"新账号处于沙箱模式：只能发给已验证的地址，且日配额 200 封。上线前需在 SES 控制台申请退出沙箱。",
			"Access Key 两项都留空时走 AWS 默认凭据链（环境变量 / EC2 实例角色 / ECS 任务角色），部署在 AWS 上时这是更安全的做法。",
			"要收投递回执必须填「配置集」并在其中挂 SNS 事件目标 —— 不填的话 SES 不会发出任何事件。",
		},
		Fields: emFields(
			emIn(emaildomain.GroupCredential,
				emSelect("region", "地域", "必须与验证发件域名时所在的地域一致", "us-east-1",
					emOption("us-east-1", "美国东部（弗吉尼亚北部）"),
					emOption("us-east-2", "美国东部（俄亥俄）"),
					emOption("us-west-2", "美国西部（俄勒冈）"),
					emOption("eu-west-1", "欧洲（爱尔兰）"),
					emOption("eu-central-1", "欧洲（法兰克福）"),
					emOption("ap-southeast-1", "亚太（新加坡）"),
					emOption("ap-southeast-2", "亚太（悉尼）"),
					emOption("ap-northeast-1", "亚太（东京）"),
					emOption("ap-south-1", "亚太（孟买）"),
				),
				emText("accessKeyId", "Access Key ID", "AKIA...", "留空则使用实例角色 / 环境变量里的默认凭据", false),
				emSecret("secretAccessKey", "Secret Access Key", "留空则使用默认凭据链", "与 Access Key ID 成对填写", false),
				emText("configurationSet", "配置集", "aegis-transactional", "投递回执必须经由配置集里的 SNS 事件目标才会发出", false),
			),
			senderIdentityFields("发件域名或地址需先在 SES 完成验证（DKIM / MAIL FROM）"),
			emIn(emaildomain.GroupWebhook,
				emText("snsTopicArn", "SNS 主题 ARN", "arn:aws:sns:us-east-1:123456789012:aegis-email",
					"填写后只接受来自该主题的回执；留空则接受任何通过 AWS 证书验签的 SNS 消息", false),
			),
			emIn(emaildomain.GroupAdvanced,
				emURL("endpoint", "自定义端点", "https://email.us-east-1.amazonaws.com", "仅在使用 VPC 端点或兼容实现时需要"),
				tagsField("作为 SES 的 EmailTags 上报，可在 CloudWatch 里按标签聚合；键与值只允许字母、数字、下划线与短横线"),
			),
		),
	}
}

func (s *sesEmailSender) Validate(config emaildomain.Config) error {
	if err := validateByCatalog(s.Describe(), config); err != nil {
		return err
	}
	// 两项凭据要么都填、要么都不填。只填一项时 SDK 会静默回落到默认凭据链，
	// 于是「我明明配了 Access Key」和「用的是实例角色」这两件事同时成立，
	// 而失败信息只会说没有权限。
	hasKeyID := config.Setting("accessKeyId") != ""
	hasSecret := config.HasSecret("secretAccessKey")
	if hasKeyID != hasSecret {
		return apperrors.New(40076, http.StatusBadRequest,
			"AWS Access Key ID 与 Secret Access Key 必须成对填写；两项都留空表示使用实例角色 / 环境变量里的默认凭据")
	}
	if config.Setting("region") == "" {
		return apperrors.New(40077, http.StatusBadRequest, "AWS 地域不能为空")
	}
	return nil
}

func (s *sesEmailSender) Send(ctx context.Context, config *emaildomain.Config, out emailOutbound) (emailSendResult, error) {
	client, err := s.buildClient(ctx, config)
	if err != nil {
		return emailSendResult{}, err
	}

	fromAddress, fromName, replyTo := config.SenderIdentity()
	raw, messageID, err := buildRawMIME(fromAddress, fromName, replyTo, out)
	if err != nil {
		return emailSendResult{}, err
	}

	input := &sesv2.SendEmailInput{
		FromEmailAddress: aws.String(formatMailAddress(fromAddress, fromName)),
		Destination:      &sestypes.Destination{ToAddresses: []string{strings.TrimSpace(out.To)}},
		Content:          &sestypes.EmailContent{Raw: &sestypes.RawMessage{Data: raw}},
		EmailTags:        sesEmailTags(config.SettingMap(emaildomain.KeyTags), out.Purpose),
	}
	if replyTo != "" {
		input.ReplyToAddresses = []string{replyTo}
	}
	if configurationSet := config.Setting("configurationSet"); configurationSet != "" {
		input.ConfigurationSetName = aws.String(configurationSet)
	}

	breakerName := circuitbreaker.Name("email-ses", fmt.Sprintf("scope-%d", config.AppID), config.Name)
	response, err := resilience.Execute(ctx, breakerName, resilience.Options{
		Timeout:     timeutil.Seconds(sesHTTPTimeout),
		MaxRetries:  2,
		BaseBackoff: timeutil.Milliseconds(300),
		MaxBackoff:  timeutil.Milliseconds(1500),
		ShouldRetry: sesShouldRetry,
	}, func(attemptCtx context.Context) (*sesv2.SendEmailOutput, error) {
		return client.SendEmail(attemptCtx, input)
	})
	if err != nil {
		s.log.Error("ses email send failed",
			zap.Int64("appid", config.AppID), zap.String("config", config.Name), zap.Error(err))
		return emailSendResult{}, wrapSESError(err)
	}

	return emailSendResult{
		MessageID:         messageID,
		ProviderMessageID: aws.ToString(response.MessageId),
		// SES 接收即返回，投递结果由配置集上的 SNS 事件回填。
		Status: emaildomain.DeliveryStatusPending,
	}, nil
}

func (s *sesEmailSender) buildClient(ctx context.Context, config *emaildomain.Config) (*sesv2.Client, error) {
	region := config.Setting("region")
	options := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
		// 出网一律经过出海代理网关：SES 的端点在境外，与 OAuth / 对象存储同一条路由表。
		awsconfig.WithHTTPClient(egress.NewClient(egress.Profile{
			Name: "email.ses", Timeout: timeutil.Seconds(sesHTTPTimeout),
		})),
	}
	if keyID := config.Setting("accessKeyId"); keyID != "" {
		options = append(options, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(keyID, config.Secret("secretAccessKey"), "")))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, options...)
	if err != nil {
		return nil, apperrors.New(50060, http.StatusInternalServerError, "初始化 AWS SES 客户端失败："+err.Error())
	}
	clientOptions := make([]func(*sesv2.Options), 0, 1)
	if endpoint := config.Setting("endpoint"); endpoint != "" {
		clientOptions = append(clientOptions, func(o *sesv2.Options) { o.BaseEndpoint = aws.String(endpoint) })
	}
	return sesv2.NewFromConfig(awsCfg, clientOptions...), nil
}

// sesTagPattern SES 对标签键值的字符集要求。
// 不满足时整个 SendEmail 会被拒绝 —— 一封信因为标签里有个中文而发不出去，
// 排查方向会完全跑偏，所以这里直接过滤掉不合规的标签而不是原样透传。
var sesTagPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,256}$`)

func sesEmailTags(configured map[string]string, purpose string) []sestypes.MessageTag {
	merged := buildProviderTags(configured, purpose)
	if len(merged) == 0 {
		return nil
	}
	tags := make([]sestypes.MessageTag, 0, len(merged))
	for key, value := range merged {
		if !sesTagPattern.MatchString(key) || !sesTagPattern.MatchString(value) {
			continue
		}
		tags = append(tags, sestypes.MessageTag{Name: aws.String(key), Value: aws.String(value)})
	}
	if len(tags) == 0 {
		return nil
	}
	return tags
}

// sesShouldRetry 配额类错误不重试：SES 的发送速率与日配额都是按账号计的，
// 重试只会把剩余配额烧得更快，并且加深熔断。
func sesShouldRetry(err error) bool {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.ErrorCode() {
		case "TooManyRequestsException", "LimitExceededException", "SendingPausedException",
			"AccountSuspendedException", "MessageRejected", "MailFromDomainNotVerifiedException",
			"BadRequestException", "NotFoundException":
			return false
		}
	}
	return resilience.RetryableError(err)
}

// wrapSESError 把 SDK 的错误码翻成可直接展示给管理员的中文诊断。
func wrapSESError(err error) error {
	if circuitbreaker.IsOpenError(err) {
		return apperrors.New(50314, http.StatusServiceUnavailable, "邮件服务暂不可用，AWS SES 通道正在熔断保护中，请稍后重试")
	}
	var apiErr smithy.APIError
	if !errors.As(err, &apiErr) {
		return apperrors.New(50060, http.StatusBadGateway, "调用 AWS SES 失败："+err.Error())
	}
	detail := clampDetail(apiErr.ErrorMessage())
	switch apiErr.ErrorCode() {
	case "MessageRejected":
		return apperrors.New(40070, http.StatusBadRequest,
			withDetail("AWS SES 拒收了这封邮件（发件地址未验证、或账号仍在沙箱模式下发给了未验证的收件人）", detail))
	case "MailFromDomainNotVerifiedException":
		return apperrors.New(40070, http.StatusBadRequest,
			withDetail("发件域名尚未在 AWS SES 完成验证，请先在控制台完成 DKIM / MAIL FROM 配置", detail))
	case "AccountSuspendedException":
		return apperrors.New(40072, http.StatusForbidden,
			withDetail("AWS SES 账号的发信能力已被暂停，请在控制台查看账号运行状况", detail))
	case "SendingPausedException":
		return apperrors.New(40072, http.StatusForbidden,
			withDetail("该配置集或账号的发信已被暂停（通常因退信率 / 投诉率超标）", detail))
	case "TooManyRequestsException", "LimitExceededException":
		return apperrors.New(42974, http.StatusTooManyRequests,
			withDetail("AWS SES 配额已用尽或发送速率超限；沙箱账号每日 200 封、每秒 1 封", detail))
	case "NotFoundException":
		return apperrors.New(40473, http.StatusNotFound,
			withDetail("AWS SES 找不到指定的配置集，请检查「配置集」是否填写正确", detail))
	case "AccessDeniedException", "UnrecognizedClientException", "InvalidClientTokenId", "SignatureDoesNotMatch":
		return apperrors.New(40071, http.StatusUnauthorized,
			withDetail("AWS 凭据无效或缺少 ses:SendEmail 权限，请检查 Access Key 与 IAM 策略", detail))
	default:
		return apperrors.New(50060, http.StatusBadGateway,
			withDetail("AWS SES 发送失败（"+apiErr.ErrorCode()+"）", detail))
	}
}
