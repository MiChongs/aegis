package service

import (
	emaildomain "aegis/internal/domain/email"
	"aegis/pkg/circuitbreaker"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/resilience"
	"aegis/pkg/timeutil"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"

	tccommon "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	tcerrors "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/errors"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	ses "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/ses/v20201002"
	"go.uber.org/zap"
)

const tencentMailTimeout = 15

// tencentEmailSender 走腾讯云邮件推送（SES）的官方 SDK。
type tencentEmailSender struct {
	log *zap.Logger
}

func newTencentEmailSender(log *zap.Logger) *tencentEmailSender {
	return &tencentEmailSender{log: log}
}

func (s *tencentEmailSender) Provider() string { return emaildomain.ProviderTencent }

func (s *tencentEmailSender) SupportsAttachments() bool { return true }

func (s *tencentEmailSender) Describe() emaildomain.ProviderMeta {
	return emaildomain.ProviderMeta{
		Provider:    emaildomain.ProviderTencent,
		Name:        "腾讯云邮件推送",
		Description: "腾讯云 SES。国内节点，支持附件，发信域名与发信地址在控制台配置。",
		Category:    emaildomain.CategoryChina,
		Icon:        "tencentqq",
		BrandColor:  "#00A4FF",
		DocURL:      "https://cloud.tencent.com/document/product/1288/51034",
		Capabilities: emaildomain.ProviderCapabilities{
			Attachments: true,
			Webhook:     true,
			Tracking:    true,
		},
		WebhookPath: "/api/email/webhook/tencent/{scope}/{config}?token={token}",
		WebhookNote: "在腾讯云 SES 控制台的「发信设置 → 回调设置」里填这个地址。" +
			"腾讯云的回执报文**不带签名**，因此地址里的 token 就是准入凭据，等同于密钥。",
		Notes: []string{
			"发件地址必须是控制台里创建并验证过的**发信地址**，且其域名已完成 SPF / DKIM 配置。",
			"附件总大小上限 4MB（Base64 后约 6MB），超出会被接口直接拒绝。",
			"腾讯云 SES 只在广州、香港、新加坡三个地域提供服务，选错地域会得到一句签名或地域错误。",
		},
		Fields: emFields(
			emIn(emaildomain.GroupCredential,
				emText("secretId", "SecretId", "AKID...", "建议使用只授予 QcloudSESFullAccess 的子账号", true),
				emSecret("secretKey", "SecretKey", "腾讯云访问密钥", "与 SecretId 成对使用", true),
				emSelect("region", "地域", "与控制台里创建发信域名时选择的地域一致", "ap-guangzhou",
					emOption("ap-guangzhou", "广州"),
					emOption("ap-hongkong", "香港"),
					emOption("ap-singapore", "新加坡"),
				),
			),
			senderIdentityFields("必须是控制台「发信地址」里已创建并验证的地址"),
			emIn(emaildomain.GroupWebhook,
				webhookSecretField("回调令牌", "自己起一串足够长的随机字符串",
					"腾讯云不对回执签名，令牌拼在回调地址里"),
			),
			emIn(emaildomain.GroupAdvanced,
				emText("unsubscribe", "退订链接档位", "1", "腾讯云的退订链接语言与样式档位（1~8）；营销类邮件必填，事务邮件可留空", false),
			),
		),
	}
}

func (s *tencentEmailSender) Validate(config emaildomain.Config) error {
	if err := validateByCatalog(s.Describe(), config); err != nil {
		return err
	}
	if !config.HasSecret("secretKey") {
		return apperrors.New(40068, http.StatusBadRequest, "腾讯云 SecretKey 不能为空")
	}
	return nil
}

func (s *tencentEmailSender) Send(ctx context.Context, config *emaildomain.Config, out emailOutbound) (emailSendResult, error) {
	client, err := s.buildClient(config)
	if err != nil {
		return emailSendResult{}, err
	}

	fromAddress, fromName, replyTo := config.SenderIdentity()
	request := ses.NewSendEmailRequest()
	// 腾讯云的 FromEmailAddress 直接吃 "别名 <addr>" 这一整串，
	// 没有单独的昵称字段 —— 因此发件人名称由 formatMailAddress 编进去。
	request.FromEmailAddress = tccommon.StringPtr(formatMailAddress(fromAddress, fromName))
	request.Destination = tccommon.StringPtrs([]string{strings.TrimSpace(out.To)})
	request.Subject = tccommon.StringPtr(truncateSubject(out.Subject))
	// Simple 的两段正文都要求是 **base64**，直接塞原文的表现是收件人看到一封空信。
	request.Simple = &ses.Simple{Html: tccommon.StringPtr(base64.StdEncoding.EncodeToString([]byte(out.HTML)))}
	if text := strings.TrimSpace(out.Text); text != "" {
		request.Simple.Text = tccommon.StringPtr(base64.StdEncoding.EncodeToString([]byte(text)))
	}
	if replyTo != "" {
		request.ReplyToAddresses = tccommon.StringPtr(replyTo)
	}
	if unsubscribe := config.Setting("unsubscribe"); unsubscribe != "" {
		request.Unsubscribe = tccommon.StringPtr(unsubscribe)
	}
	for _, attachment := range out.Attachments {
		request.Attachments = append(request.Attachments, &ses.Attachment{
			FileName: tccommon.StringPtr(attachment.Filename),
			Content:  tccommon.StringPtr(base64.StdEncoding.EncodeToString(attachment.Content)),
		})
	}

	breakerName := circuitbreaker.Name("email-tencent", fmt.Sprintf("scope-%d", config.AppID), config.Name)
	response, err := resilience.Execute(ctx, breakerName, resilience.Options{
		Timeout:     timeutil.Seconds(tencentMailTimeout),
		MaxRetries:  2,
		BaseBackoff: timeutil.Milliseconds(300),
		MaxBackoff:  timeutil.Milliseconds(1500),
		ShouldRetry: tencentShouldRetry,
	}, func(attemptCtx context.Context) (*ses.SendEmailResponse, error) {
		return client.SendEmailWithContext(attemptCtx, request)
	})
	if err != nil {
		s.log.Error("tencent email send failed",
			zap.Int64("appid", config.AppID), zap.String("config", config.Name), zap.Error(err))
		return emailSendResult{}, wrapTencentEmailError(err)
	}
	if response == nil || response.Response == nil {
		return emailSendResult{}, apperrors.New(50060, http.StatusBadGateway, "腾讯云邮件推送响应为空")
	}

	return emailSendResult{
		ProviderMessageID: derefString(response.Response.MessageId),
		// 腾讯云没有面向单封信的主动回执，交出即是能确认的最终状态。
		Status: emaildomain.DeliveryStatusSent,
	}, nil
}

func (s *tencentEmailSender) buildClient(config *emaildomain.Config) (*ses.Client, error) {
	region := config.Setting("region")
	if region == "" {
		region = "ap-guangzhou"
	}
	credential := tccommon.NewCredential(config.Setting("secretId"), config.Secret("secretKey"))
	clientProfile := profile.NewClientProfile()
	clientProfile.HttpProfile.Endpoint = "ses.tencentcloudapi.com"
	clientProfile.HttpProfile.ReqTimeout = tencentMailTimeout
	client, err := ses.NewClient(credential, region, clientProfile)
	if err != nil {
		return nil, apperrors.New(50060, http.StatusInternalServerError, "初始化腾讯云邮件推送客户端失败："+err.Error())
	}
	return client, nil
}

// tencentShouldRetry 参数与配额类错误不重试 —— 重试解决不了「发信地址没建」这种问题，
// 只会拖长请求并加深熔断。
func tencentShouldRetry(err error) bool {
	var sdkErr *tcerrors.TencentCloudSDKError
	if errors.As(err, &sdkErr) {
		code := sdkErr.GetCode()
		if strings.HasPrefix(code, "InvalidParameter") ||
			strings.HasPrefix(code, "AuthFailure") ||
			strings.HasPrefix(code, "UnauthorizedOperation") ||
			strings.HasPrefix(code, "LimitExceeded") ||
			strings.HasPrefix(code, "RequestLimitExceeded") ||
			strings.HasPrefix(code, "FailedOperation.NotAuthenticatedSender") {
			return false
		}
	}
	return resilience.RetryableError(err)
}

func wrapTencentEmailError(err error) error {
	if circuitbreaker.IsOpenError(err) {
		return apperrors.New(50314, http.StatusServiceUnavailable, "邮件服务暂不可用，腾讯云邮件推送通道正在熔断保护中，请稍后重试")
	}
	var sdkErr *tcerrors.TencentCloudSDKError
	if !errors.As(err, &sdkErr) {
		return apperrors.New(50060, http.StatusBadGateway, "调用腾讯云邮件推送失败："+err.Error())
	}
	code := sdkErr.GetCode()
	detail := clampDetail(sdkErr.GetMessage())
	switch {
	case strings.HasPrefix(code, "AuthFailure"):
		return apperrors.New(40071, http.StatusUnauthorized,
			withDetail("腾讯云密钥无效或与地域不匹配（地域填错时返回的正是鉴权失败）", detail))
	case strings.HasPrefix(code, "UnauthorizedOperation"):
		return apperrors.New(40072, http.StatusForbidden,
			withDetail("腾讯云拒绝发信：子账号缺少 SES 权限，或账号未开通邮件推送服务", detail))
	case strings.Contains(code, "NotAuthenticatedSender"), strings.Contains(code, "NotExistDomain"),
		strings.Contains(code, "SenderNotExist"):
		return apperrors.New(40070, http.StatusBadRequest,
			withDetail("发件地址不存在或未通过验证，请在腾讯云控制台的「发信地址」里创建并验证它", detail))
	case strings.HasPrefix(code, "LimitExceeded"), strings.HasPrefix(code, "RequestLimitExceeded"):
		return apperrors.New(42974, http.StatusTooManyRequests,
			withDetail("腾讯云邮件推送限频或日配额已用尽", detail))
	case strings.HasPrefix(code, "InvalidParameter"):
		return apperrors.New(40070, http.StatusBadRequest,
			withDetail("腾讯云拒绝了本次请求（参数校验未通过）", detail))
	default:
		return apperrors.New(50060, http.StatusBadGateway,
			withDetail("腾讯云邮件推送失败（"+code+"）", detail))
	}
}
