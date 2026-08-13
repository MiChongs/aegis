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

	openapiutil "github.com/alibabacloud-go/darabonba-openapi/v2/utils"
	dmclient "github.com/alibabacloud-go/dm-20151123/v2/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"
	"go.uber.org/zap"
)

const aliyunMailTimeout = 15

// aliyunEmailSender 走阿里云邮件推送（DirectMail）的官方 SDK。
//
// 与阿里云短信共用同一套 darabonba openapi 框架，凭据与地域的处理方式也一致。
type aliyunEmailSender struct {
	log *zap.Logger
}

func newAliyunEmailSender(log *zap.Logger) *aliyunEmailSender {
	return &aliyunEmailSender{log: log}
}

func (s *aliyunEmailSender) Provider() string { return emaildomain.ProviderAliyun }

// SupportsAttachments 阿里云 SingleSendMail 的附件字段是 **AttachmentUrl**（要求先把
// 文件传到公网可访问的地址），没有直传二进制的入口。
//
// 凭证 PDF 恰恰是不该有公网直链的东西 —— 把它传到一个无鉴权地址上再让阿里云去取，
// 等于用一个更大的问题换一个附件。因此这里如实声明不支持，
// 调用方会改用带签名、有失效时间的下载链接。
func (s *aliyunEmailSender) SupportsAttachments() bool { return false }

func (s *aliyunEmailSender) Describe() emaildomain.ProviderMeta {
	return emaildomain.ProviderMeta{
		Provider:    emaildomain.ProviderAliyun,
		Name:        "阿里云邮件推送",
		Description: "阿里云 DirectMail。国内节点、发信地址在阿里云控制台配置，适合面向国内用户的应用。",
		Category:    emaildomain.CategoryChina,
		Icon:        "alibabacloud",
		BrandColor:  "#FF6A00",
		DocURL:      "https://help.aliyun.com/zh/direct-mail/api-singlesendmail",
		Capabilities: emaildomain.ProviderCapabilities{
			Webhook:  true,
			Tags:     true,
			Tracking: true,
		},
		WebhookPath: "/api/email/webhook/aliyun/{scope}/{config}?token={token}",
		WebhookNote: "在阿里云控制台的「事件通知 / 回执消息」里把 HTTP 回调地址填成这个。" +
			"阿里云的回执报文**不带签名**，因此地址里的 token 就是准入凭据，等同于密钥。",
		Notes: []string{
			"发件地址必须是控制台里创建好的**发信地址**（AccountName），不能随便填。",
			"「发件人名称」对应控制台的发信人昵称，长度上限 15 个字符。",
			"这条通道**带不了附件**：阿里云只接受公网可访问的附件 URL，平台会自动改发签名下载链接。",
			"发信域名需完成 SPF / MX / DKIM 配置并通过验证，否则触发率与到达率都会很差。",
		},
		Fields: emFields(
			emIn(emaildomain.GroupCredential,
				emText("accessKeyId", "AccessKey ID", "LTAI...", "建议使用只授予 AliyunDirectMailFullAccess 的 RAM 子账号", false),
				emSecret("accessKeySecret", "AccessKey Secret", "留空则使用实例 RAM 角色", "与 AccessKey ID 成对填写", false),
				emSelect("region", "地域", "与控制台里创建发信域名时选择的地域一致", "cn-hangzhou",
					emOption("cn-hangzhou", "华东 1（杭州）"),
					emOption("ap-southeast-1", "新加坡"),
					emOption("ap-southeast-2", "澳大利亚（悉尼）"),
				),
			),
			senderIdentityFields("必须是控制台「发信地址」里已创建并通过验证的地址"),
			emIn(emaildomain.GroupWebhook,
				webhookSecretField("回调令牌", "自己起一串足够长的随机字符串",
					"阿里云不对回执签名，令牌拼在回调地址里"),
			),
			emIn(emaildomain.GroupAdvanced,
				emText("tagName", "数据标签", "aegis", "控制台里创建好的标签名，用于分类统计；填了不存在的标签会导致发信失败", false),
				emSwitch("replyToAddress", "回信到发信地址",
					"打开后阿里云会把回信投递到发信地址；关闭时收件人的回复不会被任何人看到", false),
			),
		),
	}
}

func (s *aliyunEmailSender) Validate(config emaildomain.Config) error {
	if err := validateByCatalog(s.Describe(), config); err != nil {
		return err
	}
	// 与 SES 同一条：只填一半会静默回落到实例角色，而失败信息只会说没有权限。
	hasKeyID := config.Setting("accessKeyId") != ""
	hasSecret := config.HasSecret("accessKeySecret")
	if hasKeyID != hasSecret {
		return apperrors.New(40076, http.StatusBadRequest,
			"AccessKey ID 与 AccessKey Secret 必须成对填写；两项都留空表示使用实例 RAM 角色")
	}
	if alias := config.Setting(emaildomain.KeyFromName); len([]rune(alias)) > 15 {
		return apperrors.New(40079, http.StatusBadRequest, "阿里云邮件推送的发件人名称上限为 15 个字符")
	}
	return nil
}

func (s *aliyunEmailSender) Send(ctx context.Context, config *emaildomain.Config, out emailOutbound) (emailSendResult, error) {
	client, err := s.buildClient(config)
	if err != nil {
		return emailSendResult{}, err
	}

	fromAddress, fromName, replyTo := config.SenderIdentity()
	request := &dmclient.SingleSendMailRequest{
		AccountName: tea.String(fromAddress),
		// AddressType=1 表示「发信地址」，0 是随机账号。随机账号发出去的信
		// 用的是阿里云的域名，收件人看到的发件人与产品毫无关系。
		AddressType:    tea.Int32(1),
		ReplyToAddress: tea.Bool(config.SettingBool("replyToAddress")),
		ToAddress:      tea.String(strings.TrimSpace(out.To)),
		Subject:        tea.String(truncateSubject(out.Subject)),
		HtmlBody:       tea.String(out.HTML),
	}
	if text := strings.TrimSpace(out.Text); text != "" {
		request.TextBody = tea.String(text)
	}
	if fromName != "" {
		request.FromAlias = tea.String(fromName)
	}
	if replyTo != "" {
		request.ReplyAddress = tea.String(replyTo)
		if fromName != "" {
			request.ReplyAddressAlias = tea.String(fromName)
		}
	}
	if tagName := config.Setting("tagName"); tagName != "" {
		request.TagName = tea.String(tagName)
	}

	breakerName := circuitbreaker.Name("email-aliyun", fmt.Sprintf("scope-%d", config.AppID), config.Name)
	response, err := resilience.Execute(ctx, breakerName, resilience.Options{
		Timeout:     timeutil.Seconds(aliyunMailTimeout),
		MaxRetries:  2,
		BaseBackoff: timeutil.Milliseconds(300),
		MaxBackoff:  timeutil.Milliseconds(1500),
	}, func(attemptCtx context.Context) (*dmclient.SingleSendMailResponse, error) {
		return client.SingleSendMailWithContext(attemptCtx, request, &dara.RuntimeOptions{})
	})
	if err != nil {
		s.log.Error("aliyun email send failed",
			zap.Int64("appid", config.AppID), zap.String("config", config.Name), zap.Error(err))
		return emailSendResult{}, wrapAliyunEmailError(err)
	}
	if response == nil || response.Body == nil {
		return emailSendResult{}, apperrors.New(50060, http.StatusBadGateway, "阿里云邮件推送响应为空")
	}

	return emailSendResult{
		// EnvId 是这封信在阿里云侧的唯一标识，工单排查时对方会要这个值。
		ProviderMessageID: tea.StringValue(response.Body.EnvId),
		// 阿里云没有面向单封信的投递回执接口，交出即是能确认的最终状态。
		Status: emaildomain.DeliveryStatusSent,
	}, nil
}

func (s *aliyunEmailSender) buildClient(config *emaildomain.Config) (*dmclient.Client, error) {
	region := config.Setting("region")
	if region == "" {
		region = "cn-hangzhou"
	}
	openapiConfig := &openapiutil.Config{
		// 端点按地域拼：填错地域时阿里云返回的是签名错误而不是「地域不对」，
		// 因此这里不给自定义端点的口子，减少一种排查不出来的失败方式。
		Endpoint: tea.String("dm." + region + ".aliyuncs.com"),
		RegionId: tea.String(region),
	}
	if keyID := config.Setting("accessKeyId"); keyID != "" {
		openapiConfig.AccessKeyId = tea.String(keyID)
		openapiConfig.AccessKeySecret = tea.String(config.Secret("accessKeySecret"))
	}
	client, err := dmclient.NewClient(openapiConfig)
	if err != nil {
		return nil, apperrors.New(50060, http.StatusInternalServerError, "初始化阿里云邮件推送客户端失败："+err.Error())
	}
	return client, nil
}

// wrapAliyunEmailError 把阿里云的错误码翻成可直接展示给管理员的中文诊断。
//
// 阿里云的原始报错是英文且高度模板化（"InvalidMailAddress.NotFound the mail
// address is not exist"），直接抛给管理员的话，最常见的两个原因
// （发信地址没建、AccessKey 没授权）看起来一模一样。
func wrapAliyunEmailError(err error) error {
	if circuitbreaker.IsOpenError(err) {
		return apperrors.New(50314, http.StatusServiceUnavailable, "邮件服务暂不可用，阿里云邮件推送通道正在熔断保护中，请稍后重试")
	}
	var sdkErr *tea.SDKError
	if !errors.As(err, &sdkErr) {
		return apperrors.New(50060, http.StatusBadGateway, "调用阿里云邮件推送失败："+err.Error())
	}
	code := tea.StringValue(sdkErr.Code)
	detail := clampDetail(tea.StringValue(sdkErr.Message))
	switch {
	case strings.Contains(code, "InvalidMailAddress"), strings.Contains(code, "AccountName"):
		return apperrors.New(40070, http.StatusBadRequest,
			withDetail("发件地址不存在或未通过验证，请在阿里云控制台的「发信地址」里创建并验证它", detail))
	case strings.Contains(code, "InvalidTagName"):
		return apperrors.New(40070, http.StatusBadRequest,
			withDetail("数据标签不存在，请先在阿里云控制台创建同名标签，或把「数据标签」留空", detail))
	case strings.Contains(code, "Forbidden"), strings.Contains(code, "NoPermission"), strings.Contains(code, "Unauthorized"):
		return apperrors.New(40072, http.StatusForbidden,
			withDetail("阿里云拒绝发信：AccessKey 缺少邮件推送权限，或账号欠费 / 未实名", detail))
	case strings.Contains(code, "InvalidAccessKeyId"), strings.Contains(code, "SignatureDoesNotMatch"):
		return apperrors.New(40071, http.StatusUnauthorized,
			withDetail("AccessKey 无效或与地域不匹配（地域填错时阿里云返回的正是签名错误）", detail))
	case strings.Contains(code, "Throttling"), strings.Contains(code, "QuotaExceed"), strings.Contains(code, "DayLimit"):
		return apperrors.New(42974, http.StatusTooManyRequests,
			withDetail("阿里云邮件推送限频或日配额已用尽", detail))
	default:
		return apperrors.New(50060, http.StatusBadGateway,
			withDetail("阿里云邮件推送失败（"+code+"）", detail))
	}
}
