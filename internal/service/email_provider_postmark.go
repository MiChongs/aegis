package service

import (
	emaildomain "aegis/internal/domain/email"
	"aegis/pkg/circuitbreaker"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/resilience"
	"aegis/pkg/timeutil"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"go.uber.org/zap"
)

const (
	postmarkHTTPTimeout = 15
	postmarkEndpoint    = "https://api.postmarkapp.com/email"
)

// postmarkEmailSender 走 Postmark 的 REST 接口。
//
// 这是九档里唯一没有用官方 SDK 的：Postmark 没有官方 Go 客户端，
// 社区实现（mrz1836/postmark 等）既不换 HTTP 客户端也不带 context，
// 接不进平台的出海代理网关与熔断。它的发信接口只有一个 POST，
// 自己实现比包一层第三方更可控。
type postmarkEmailSender struct {
	log    *zap.Logger
	client *http.Client
}

func newPostmarkEmailSender(log *zap.Logger) *postmarkEmailSender {
	return &postmarkEmailSender{
		log:    log,
		client: newEmailHTTPClient("email.postmark", postmarkHTTPTimeout),
	}
}

func (s *postmarkEmailSender) Provider() string { return emaildomain.ProviderPostmark }

func (s *postmarkEmailSender) SupportsAttachments() bool { return true }

func (s *postmarkEmailSender) Describe() emaildomain.ProviderMeta {
	return emaildomain.ProviderMeta{
		Provider:    emaildomain.ProviderPostmark,
		Name:        "Postmark",
		Description: "只做事务邮件，把营销邮件挡在门外，因此送达率口碑长期领先。",
		Category:    emaildomain.CategoryInternational,
		Icon:        "postmark",
		BrandColor:  "#FFDE00",
		DocURL:      "https://postmarkapp.com/developer/api/email-api",
		Capabilities: emaildomain.ProviderCapabilities{
			Attachments: true,
			Webhook:     true,
			Tags:        true,
			Tracking:    true,
		},
		WebhookPath: "/api/email/webhook/postmark/{scope}/{config}?token={token}",
		WebhookNote: "在 Servers → 对应 Server → Webhooks 里新建，勾上 Delivery / Bounce / Spam Complaint（需要打开率再勾 Open / Click）。" +
			"Postmark **不对回执签名**，因此地址里的 token 就是准入凭据，等同于密钥；" +
			"也可以改用 Postmark 的 Basic Auth：用户名任意、密码填同一个回调令牌。",
		Notes: []string{
			"Server Token 是**按 Server 分配**的，不是账号级令牌；换 Server 要换令牌。",
			"发件地址必须是已验证的 Sender Signature 或已认证域名下的地址。",
			"事务邮件请用 outbound 流；把营销内容发进事务流会被 Postmark 直接封停账号。",
			"Postmark 的标签是**单个字符串**，因此平台只上报 purpose 那一个值。",
		},
		Fields: emFields(
			emIn(emaildomain.GroupCredential,
				emSecret("serverToken", "Server Token", "在 Servers → API Tokens 中获取", "注意是 Server Token，不是 Account Token", true),
				emText("messageStream", "消息流", "outbound", "事务邮件用 outbound；留空即 outbound", false),
			),
			senderIdentityFields("必须是已验证的 Sender Signature，或已认证域名下的地址"),
			emIn(emaildomain.GroupWebhook,
				webhookSecretField("回调令牌", "自己起一串足够长的随机字符串",
					"Postmark 不对回执签名，令牌拼在回调地址里（或用 Basic Auth 的密码位）"),
			),
			emIn(emaildomain.GroupAdvanced,
				emSwitch("trackOpens", "追踪打开", "在正文里插入一个追踪像素统计打开率", false),
			),
		),
	}
}

func (s *postmarkEmailSender) Validate(config emaildomain.Config) error {
	if err := validateByCatalog(s.Describe(), config); err != nil {
		return err
	}
	if !config.HasSecret("serverToken") {
		return apperrors.New(40068, http.StatusBadRequest, "Postmark Server Token 不能为空")
	}
	return nil
}

// postmarkSendRequest 对应 POST /email 的请求体。字段名大小写敏感。
type postmarkSendRequest struct {
	From          string               `json:"From"`
	To            string               `json:"To"`
	Subject       string               `json:"Subject"`
	HtmlBody      string               `json:"HtmlBody,omitempty"`
	TextBody      string               `json:"TextBody,omitempty"`
	ReplyTo       string               `json:"ReplyTo,omitempty"`
	Tag           string               `json:"Tag,omitempty"`
	TrackOpens    bool                 `json:"TrackOpens,omitempty"`
	MessageStream string               `json:"MessageStream,omitempty"`
	Attachments   []postmarkAttachment `json:"Attachments,omitempty"`
}

type postmarkAttachment struct {
	Name        string `json:"Name"`
	Content     string `json:"Content"`
	ContentType string `json:"ContentType"`
}

type postmarkSendResponse struct {
	To          string `json:"To"`
	MessageID   string `json:"MessageID"`
	ErrorCode   int    `json:"ErrorCode"`
	Message     string `json:"Message"`
	SubmittedAt string `json:"SubmittedAt"`
}

func (s *postmarkEmailSender) Send(ctx context.Context, config *emaildomain.Config, out emailOutbound) (emailSendResult, error) {
	token := config.Secret("serverToken")
	if token == "" {
		return emailSendResult{}, apperrors.New(40068, http.StatusBadRequest, "Postmark Server Token 未配置或解密失败，请重新填写")
	}

	fromAddress, fromName, replyTo := config.SenderIdentity()
	stream := config.Setting("messageStream")
	if stream == "" {
		stream = "outbound"
	}
	payload := postmarkSendRequest{
		From:          formatMailAddress(fromAddress, fromName),
		To:            strings.TrimSpace(out.To),
		Subject:       truncateSubject(out.Subject),
		HtmlBody:      out.HTML,
		TextBody:      out.Text,
		ReplyTo:       replyTo,
		Tag:           strings.TrimSpace(out.Purpose),
		TrackOpens:    config.SettingBool("trackOpens"),
		MessageStream: stream,
	}
	for _, attachment := range out.Attachments {
		contentType := strings.TrimSpace(attachment.ContentType)
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		payload.Attachments = append(payload.Attachments, postmarkAttachment{
			Name:        attachment.Filename,
			Content:     base64.StdEncoding.EncodeToString(attachment.Content),
			ContentType: contentType,
		})
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return emailSendResult{}, apperrors.New(50060, http.StatusInternalServerError, "构造 Postmark 邮件请求失败")
	}

	breakerName := circuitbreaker.Name("email-postmark", fmt.Sprintf("scope-%d", config.AppID), config.Name)
	response, err := resilience.Execute(ctx, breakerName, resilience.Options{
		Timeout:     timeutil.Seconds(postmarkHTTPTimeout),
		MaxRetries:  2,
		BaseBackoff: timeutil.Milliseconds(300),
		MaxBackoff:  timeutil.Milliseconds(1500),
		ShouldRetry: postmarkShouldRetry,
	}, func(attemptCtx context.Context) (postmarkSendResponse, error) {
		return s.postSend(attemptCtx, token, body)
	})
	if err != nil {
		s.log.Error("postmark email send failed",
			zap.Int64("appid", config.AppID), zap.String("config", config.Name), zap.Error(err))
		return emailSendResult{}, wrapPostmarkError(err)
	}

	return emailSendResult{
		ProviderMessageID: response.MessageID,
		// Postmark 接受即返回，没有配 webhook 时这就是最终可知的状态。
		Status: emaildomain.DeliveryStatusSent,
	}, nil
}

func (s *postmarkEmailSender) postSend(ctx context.Context, token string, body []byte) (postmarkSendResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, postmarkEndpoint, bytes.NewReader(body))
	if err != nil {
		return postmarkSendResponse{}, apperrors.New(50060, http.StatusInternalServerError, "构造 Postmark 邮件请求失败")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("X-Postmark-Server-Token", token)

	httpResponse, err := s.client.Do(request)
	if err != nil {
		return postmarkSendResponse{}, err
	}
	defer httpResponse.Body.Close()

	raw := readLimitedBody(httpResponse.Body)
	var parsed postmarkSendResponse
	_ = json.Unmarshal(raw, &parsed)

	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		return postmarkSendResponse{}, classifyPostmarkError(httpResponse.StatusCode, parsed, raw)
	}
	if strings.TrimSpace(parsed.MessageID) == "" {
		return postmarkSendResponse{}, apperrors.New(50060, http.StatusBadGateway, "Postmark 未返回邮件 ID，无法追踪投递状态")
	}
	return parsed, nil
}

// classifyPostmarkError Postmark 的业务错误码比 HTTP 状态码有信息量得多：
// 422 之下有几十种 ErrorCode，而管理员真正要区分的只有「改配置」与「等一会儿」两类。
func classifyPostmarkError(statusCode int, parsed postmarkSendResponse, raw []byte) error {
	detail := clampDetail(parsed.Message)
	if detail == "" {
		detail = clampDetail(string(raw))
	}
	switch parsed.ErrorCode {
	case 10:
		return apperrors.New(40071, http.StatusUnauthorized, withDetail("Postmark Server Token 无效", detail))
	case 300:
		return apperrors.New(40070, http.StatusBadRequest, withDetail("Postmark 拒绝了本次请求（参数校验未通过）", detail))
	case 400, 401:
		return apperrors.New(40072, http.StatusForbidden,
			withDetail("Postmark 拒绝发信：账号未获批准发信，或仍处于待审核状态", detail))
	case 405:
		return apperrors.New(40072, http.StatusForbidden,
			withDetail("Postmark 账号因退信 / 投诉率过高被暂停发信", detail))
	case 406:
		return apperrors.New(40070, http.StatusBadRequest,
			withDetail("该收件人在 Postmark 的抑制列表里（曾硬退信或投诉），需先在控制台移除", detail))
	case 429:
		return apperrors.New(42974, http.StatusTooManyRequests, withDetail("Postmark 限频", detail))
	case 1101:
		return apperrors.New(40070, http.StatusBadRequest,
			withDetail("消息流不存在或类型不符（事务邮件应使用 outbound 流）", detail))
	}
	switch {
	case statusCode == http.StatusUnauthorized:
		return apperrors.New(40071, http.StatusUnauthorized, withDetail("Postmark Server Token 无效", detail))
	case statusCode == http.StatusUnprocessableEntity:
		return apperrors.New(40070, http.StatusBadRequest,
			withDetail(fmt.Sprintf("Postmark 拒绝了本次请求（ErrorCode %d）", parsed.ErrorCode), detail))
	case statusCode == http.StatusTooManyRequests:
		return apperrors.New(42974, http.StatusTooManyRequests, withDetail("Postmark 限频", detail))
	case statusCode >= 500:
		return apperrors.New(50060, http.StatusBadGateway, withDetail(fmt.Sprintf("Postmark 返回 %d，请稍后重试", statusCode), detail))
	default:
		return apperrors.New(50060, http.StatusBadGateway, withDetail(fmt.Sprintf("Postmark 发送失败（HTTP %d）", statusCode), detail))
	}
}

func postmarkShouldRetry(err error) bool {
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) && appErr.HTTPStatus < http.StatusInternalServerError {
		return false
	}
	return resilience.RetryableError(err)
}

func wrapPostmarkError(err error) error {
	if circuitbreaker.IsOpenError(err) {
		return apperrors.New(50314, http.StatusServiceUnavailable, "邮件服务暂不可用，Postmark 通道正在熔断保护中，请稍后重试")
	}
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	return apperrors.New(50060, http.StatusBadGateway, "调用 Postmark 接口失败："+err.Error())
}
