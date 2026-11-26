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
	"strconv"
	"strings"

	"go.uber.org/zap"
)

// skymail 发信器的配置键。
const (
	skymailKeyBaseURL    = "baseUrl"
	skymailKeyLoginEmail = "loginEmail"
	skymailKeyPassword   = "password"

	// skymailMaxAttachments Cloud Mail 单封信最多 10 个附件（正文内嵌图片另算 10 张）。
	skymailMaxAttachments = 10
)

// skymailEmailSender 走 Cloud Mail（docs.skymail.ink）的 REST 接口发信。
//
// Cloud Mail 没有 API Key：凭据就是邮局里一个用户的邮箱与密码，换来的令牌由 skymailClient 缓存。
// 它本身也不直接投递公网邮件 —— 站外收件人经它绑定的 Resend 或 Cloudflare Email 发出，
// 投递结果（送达 / 退信 / 投诉）回写到它自己的发件箱里。Cloud Mail 不向外推送回执，
// 因此平台靠定时比对它的发件箱回填状态（见 skymail_sync.go），而不是 webhook。
type skymailEmailSender struct {
	log    *zap.Logger
	client *skymailClient
}

func newSkymailEmailSender(log *zap.Logger) *skymailEmailSender {
	return &skymailEmailSender{log: log, client: newSkymailClient(log)}
}

func (s *skymailEmailSender) Provider() string { return emaildomain.ProviderSkymail }

func (s *skymailEmailSender) SupportsAttachments() bool { return true }

func (s *skymailEmailSender) Describe() emaildomain.ProviderMeta {
	return emaildomain.ProviderMeta{
		Provider:    emaildomain.ProviderSkymail,
		Name:        "Cloud Mail",
		Description: "部署在 Cloudflare 上的自建邮局，收发一体；除了发信，还能在这里直接管理它的收件箱、邮箱账号、用户与注册码。",
		Category:    emaildomain.CategoryDirect,
		// Simple Icons 没有收录 Cloud Mail。它跑在 Cloudflare 上，但不是 Cloudflare 的产品，
		// 借那家的标识等于冒充品牌，因此留空落到控制台的中性信封。
		Icon:       "",
		BrandColor: "#2563EB",
		DocURL:     "https://docs.skymail.ink/api/api-doc.html",
		Capabilities: emaildomain.ProviderCapabilities{
			Attachments: true,
			Webhook:     false,
			Tags:        false,
			Tracking:    false,
		},
		Notes: []string{
			"凭据是邮局里一个用户的登录邮箱与密码（不是 API Key）。建议单独建一个发信用户，别用管理员本人的账号。",
			"站外收件人要靠邮局后台绑定的 Resend 或 Cloudflare Email 才发得出去；只配了收信的邮局只能发给站内地址。",
			"Cloud Mail 不推送投递回执。平台每两分钟比对一次它的发件箱，把送达、退信、投诉回填到投递记录里。",
			"发件地址必须是登录用户名下的邮箱；留空则用登录邮箱发信。Cloud Mail 不支持单独的回信地址。",
			"单封信最多 10 个附件。普通用户受邮局里权限身份的发件次数限制，用尽后发信会被拒绝。",
		},
		Fields: emFields(
			emIn(emaildomain.GroupCredential,
				emaildomain.ConfigField{
					Key: skymailKeyBaseURL, Label: "邮局地址", Type: emaildomain.FieldURL, Required: true,
					Placeholder: "https://mail.example.com",
					Help:        "Cloud Mail 站点的根地址，不要带 /api",
				},
				emaildomain.ConfigField{
					Key: skymailKeyLoginEmail, Label: "登录邮箱", Type: emaildomain.FieldEmail, Required: true,
					Placeholder: "sender@example.com",
					Help:        "用来登录邮局的完整邮箱地址",
				},
				emSecret(skymailKeyPassword, "登录密码", "邮局账号的密码", "加密保存；留空表示不修改", true),
			),
			emIn(emaildomain.GroupSender,
				emaildomain.ConfigField{
					Key: emaildomain.KeyFromAddress, Label: "发件地址", Type: emaildomain.FieldEmail,
					Placeholder: "留空即用登录邮箱",
					Help:        "必须是登录用户名下的邮箱账号",
				},
				emaildomain.ConfigField{
					Key: emaildomain.KeyFromName, Label: "发件人名称", Type: emaildomain.FieldText,
					Placeholder: "Aegis",
					Help:        "显示在收件人邮件列表里的名字，留空则取邮箱前缀",
				},
			),
		),
	}
}

func (s *skymailEmailSender) Validate(config emaildomain.Config) error {
	if err := validateByCatalog(s.Describe(), config); err != nil {
		return err
	}
	if !config.HasSecret(skymailKeyPassword) {
		return apperrors.New(40068, http.StatusBadRequest, "Cloud Mail 登录密码不能为空")
	}
	return nil
}

// skymailCredentialsOf 从配置取凭据。密码只在发送链路解密后才有值。
func skymailCredentialsOf(config *emaildomain.Config) skymailCredentials {
	return skymailCredentials{
		BaseURL:  normalizeSkymailBaseURL(config.Setting(skymailKeyBaseURL)),
		Email:    strings.TrimSpace(config.Setting(skymailKeyLoginEmail)),
		Password: config.Secret(skymailKeyPassword),
	}
}

// skymailProviderMessageID 平台记的服务商邮件 ID：`<emailId>@<站点主机>`。
// Cloud Mail 的 id 是自增整数，两套邮局各发一封就会撞号，带上主机才能唯一定位。
func skymailProviderMessageID(cred skymailCredentials, emailID int64) string {
	return strconv.FormatInt(emailID, 10) + "@" + cred.host()
}

// parseSkymailProviderMessageID 拆出邮件 id 与主机。
func parseSkymailProviderMessageID(value string) (int64, string, bool) {
	idPart, host, ok := strings.Cut(strings.TrimSpace(value), "@")
	if !ok {
		return 0, "", false
	}
	id, err := strconv.ParseInt(idPart, 10, 64)
	if err != nil || id <= 0 {
		return 0, "", false
	}
	return id, strings.ToLower(host), true
}

func (s *skymailEmailSender) Send(ctx context.Context, config *emaildomain.Config, out emailOutbound) (emailSendResult, error) {
	cred := skymailCredentialsOf(config)
	if cred.Password == "" {
		return emailSendResult{}, apperrors.New(40068, http.StatusBadRequest, "Cloud Mail 登录密码未配置或解密失败，请重新填写")
	}
	if len(out.Attachments) > skymailMaxAttachments {
		return emailSendResult{}, apperrors.New(40070, http.StatusBadRequest,
			fmt.Sprintf("Cloud Mail 单封信最多 %d 个附件，这封有 %d 个", skymailMaxAttachments, len(out.Attachments)))
	}
	fromAddress, fromName, _ := config.SenderIdentity()

	breakerName := circuitbreaker.Name("email-skymail", fmt.Sprintf("scope-%d", config.AppID), config.Name)
	result, err := resilience.Execute(ctx, breakerName, resilience.Options{
		Timeout: timeutil.Seconds(skymailHTTPTimeout * 2),
		// 发信不是幂等的：一次 5xx 之后对方可能已经发出去了，重试就是重复发信。
		MaxRetries: 0,
		ShouldRetry: func(error) bool {
			return false
		},
	}, func(attemptCtx context.Context) (emailSendResult, error) {
		accountID, err := s.client.resolveAccountID(attemptCtx, cred, fromAddress)
		if err != nil {
			return emailSendResult{}, err
		}
		request := skymailSendRequest{
			AccountID:    accountID,
			ReceiveEmail: []string{strings.TrimSpace(out.To)},
			Subject:      truncateSubject(out.Subject),
			Content:      out.HTML,
			Text:         out.Text,
			Name:         strings.TrimSpace(fromName),
		}
		for _, attachment := range out.Attachments {
			contentType := strings.TrimSpace(attachment.ContentType)
			if contentType == "" {
				contentType = "application/octet-stream"
			}
			request.Attachments = append(request.Attachments, skymailSendAttachment{
				Filename:    attachment.Filename,
				Content:     base64.StdEncoding.EncodeToString(attachment.Content),
				ContentType: contentType,
				Size:        int64(len(attachment.Content)),
			})
		}
		sent, err := s.client.send(attemptCtx, cred, request)
		if err != nil {
			return emailSendResult{}, err
		}
		if len(sent) == 0 || sent[0].EmailID <= 0 {
			return emailSendResult{}, apperrors.New(50060, http.StatusBadGateway, "Cloud Mail 未返回邮件 ID，无法追踪投递状态")
		}
		status := emaildomain.DeliveryStatusSent
		if sent[0].Status == skymailStatusDelivered {
			status = emaildomain.DeliveryStatusDelivered
		}
		return emailSendResult{
			ProviderMessageID: skymailProviderMessageID(cred, sent[0].EmailID),
			Status:            status,
		}, nil
	})
	if err != nil {
		s.log.Error("skymail email send failed",
			zap.Int64("appid", config.AppID), zap.String("config", config.Name), zap.Error(err))
		return emailSendResult{}, wrapSkymailError(err)
	}
	return result, nil
}

func wrapSkymailError(err error) error {
	if circuitbreaker.IsOpenError(err) {
		return apperrors.New(50314, http.StatusServiceUnavailable, "邮件服务暂不可用，Cloud Mail 通道正在熔断保护中，请稍后重试")
	}
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return apperrors.New(50060, http.StatusGatewayTimeout, "调用 Cloud Mail 超时")
	}
	return apperrors.New(50060, http.StatusBadGateway, "调用 Cloud Mail 接口失败："+clampDetail(err.Error()))
}

// Cloud Mail 的邮件状态码（email.status）。
const (
	skymailStatusReceived  = 0
	skymailStatusSent      = 1
	skymailStatusDelivered = 2
	skymailStatusBounced   = 3
	skymailStatusComplaint = 4
	skymailStatusDelayed   = 5
	skymailStatusSaving    = 6
	skymailStatusNoRecip   = 7
	skymailStatusFailed    = 8
)
