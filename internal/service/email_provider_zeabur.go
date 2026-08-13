package service

import (
	emaildomain "aegis/internal/domain/email"
	"aegis/pkg/circuitbreaker"
	"aegis/pkg/egress"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/resilience"
	"aegis/pkg/timeutil"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"go.uber.org/zap"
)

const (
	zeaburHTTPTimeout = 15
	zeaburMaxBodyRead = 1 << 20 // 出错时最多读 1MB 响应体，避免异常网关把内存打满
	zeaburSendPath    = "/emails"
)

// zeaburEmailSender 走 Zeabur Email 的 REST API。
//
// 这条通道是 Zeabur 部署形态下最省事的邮件出口：
// 平台底层（Akamai/Linode）封禁出站 SMTP 端口，直连 SMTP 必然超时。
type zeaburEmailSender struct {
	log    *zap.Logger
	client *http.Client
}

func newZeaburEmailSender(log *zap.Logger) *zeaburEmailSender {
	return &zeaburEmailSender{
		log:    log,
		client: egress.NewClient(egress.Profile{Name: "email.zeabur", Timeout: timeutil.Seconds(zeaburHTTPTimeout)}),
	}
}

func (s *zeaburEmailSender) Provider() string { return emaildomain.ProviderZeabur }

// SupportsAttachments Zeabur Email 的 REST 接口没有公开的附件字段。
//
// 它的请求体与 Resend 高度同构，因此附件**很可能**也用同一套 `attachments` 字段 ——
// 但「很可能」不足以拿来发凭证：未知字段被服务端忽略的表现是「邮件正常送达、
// 附件不翼而飞」，用户和运维都不会收到任何错误。
// 因此这里如实声明为不支持，调用方会改用签名下载链接投递凭证。
// 等 Zeabur 正式文档化附件字段，把这里改成 true 并在 Send 里带上即可。
func (s *zeaburEmailSender) SupportsAttachments() bool { return false }

func (s *zeaburEmailSender) Describe() emaildomain.ProviderMeta {
	return emaildomain.ProviderMeta{
		Provider:    emaildomain.ProviderZeabur,
		Name:        "Zeabur Email",
		Description: "Zeabur 平台自带的发信服务，部署在 Zeabur 上时无需另找服务商。",
		Category:    emaildomain.CategoryPlatform,
		Icon:        "zeabur",
		BrandColor:  "#6300FF",
		DocURL:      "https://zeabur.com/docs/data-management/email",
		Capabilities: emaildomain.ProviderCapabilities{
			Webhook:  true,
			Tags:     true,
			Tracking: true,
		},
		WebhookPath: "/api/email/webhook/zeabur/{scope}/{config}",
		WebhookNote: "填入 Zeabur 控制台的 Webhook 管理，用于回填送达 / 退信 / 投诉状态。",
		Notes: []string{
			"发件域名需先在 Zeabur 控制台完成 DKIM / SPF / DMARC 验证。",
			"未验证域名每日仅 100 封，验证后 1000 封，配额按 UTC 00:00 重置。",
			"这条通道**带不了附件**：需要寄送凭证 PDF 时平台会自动改发签名下载链接。",
		},
		Fields: emFields(
			emIn(emaildomain.GroupCredential,
				emSecret("apiKey", "API Key", "zsk_...", "在 Zeabur 控制台的 Email → API Keys 中创建", true),
			),
			senderIdentityFields("必须属于已在 Zeabur 完成验证的发件域名"),
			emIn(emaildomain.GroupWebhook,
				webhookSecretField("Webhook 签名密钥", "创建 Webhook 时生成的 Secret", "用于校验投递回执的来源"),
			),
			emIn(emaildomain.GroupAdvanced,
				emURL("baseUrl", "API 地址", emaildomain.ZeaburDefaultBaseURL, "留空即使用官方地址，仅自建代理时需要填"),
				tagsField("随邮件上报给 Zeabur，可在其控制台按标签筛选投递情况；平台会自动补一个 purpose 标签"),
			),
		),
	}
}

func (s *zeaburEmailSender) Validate(config emaildomain.Config) error {
	if err := validateByCatalog(s.Describe(), config); err != nil {
		return err
	}
	if !config.HasSecret("apiKey") {
		return apperrors.New(40068, http.StatusBadRequest,
			"Zeabur API Key 不能为空，请在 Zeabur 控制台的 Email → API Keys 中创建")
	}
	if base := config.Setting("baseUrl"); base != "" && !strings.HasPrefix(strings.ToLower(base), "https://") {
		return apperrors.New(40069, http.StatusBadRequest, "Zeabur API 地址必须以 https:// 开头")
	}
	return nil
}

// zeaburSendRequest 对应 POST /emails 的请求体。
type zeaburSendRequest struct {
	From    string            `json:"from"`
	To      []string          `json:"to"`
	ReplyTo []string          `json:"reply_to,omitempty"`
	Subject string            `json:"subject"`
	HTML    string            `json:"html,omitempty"`
	Text    string            `json:"text,omitempty"`
	Tags    map[string]string `json:"tags,omitempty"`
}

type zeaburSendResponse struct {
	ID        string `json:"id"`
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}

type zeaburErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func (s *zeaburEmailSender) Send(ctx context.Context, config *emaildomain.Config, out emailOutbound) (emailSendResult, error) {
	apiKey := config.Secret("apiKey")
	if apiKey == "" {
		return emailSendResult{}, apperrors.New(40068, http.StatusBadRequest, "Zeabur API Key 未配置或解密失败，请重新填写")
	}

	fromAddress, fromName, replyTo := config.SenderIdentity()
	payload := zeaburSendRequest{
		From:    formatMailAddress(fromAddress, fromName),
		To:      []string{strings.TrimSpace(out.To)},
		Subject: truncateSubject(out.Subject),
		HTML:    out.HTML,
		Text:    out.Text,
		Tags:    buildProviderTags(config.SettingMap(emaildomain.KeyTags), out.Purpose),
	}
	if replyTo != "" {
		payload.ReplyTo = []string{replyTo}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return emailSendResult{}, apperrors.New(50060, http.StatusInternalServerError, "构造 Zeabur 邮件请求失败")
	}

	endpoint := zeaburEndpoint(config.Setting("baseUrl"), zeaburSendPath)
	breakerName := circuitbreaker.Name("email-zeabur", fmt.Sprintf("scope-%d", config.AppID), config.Name)
	response, err := resilience.Execute(ctx, breakerName, resilience.Options{
		Timeout:     timeutil.Seconds(zeaburHTTPTimeout),
		MaxRetries:  2,
		BaseBackoff: timeutil.Milliseconds(300),
		MaxBackoff:  timeutil.Milliseconds(1500),
		ShouldRetry: zeaburShouldRetry,
	}, func(attemptCtx context.Context) (zeaburSendResponse, error) {
		return s.postSend(attemptCtx, endpoint, apiKey, body)
	})
	if err != nil {
		s.log.Error("zeabur email send failed",
			zap.Int64("appid", config.AppID), zap.String("config", config.Name), zap.Error(err))
		return emailSendResult{}, wrapZeaburError(err)
	}

	status := emaildomain.DeliveryStatusPending
	// Zeabur 入队即返回 pending/queued，真正的投递结果由 webhook 回填。
	if normalized := strings.ToLower(strings.TrimSpace(response.Status)); normalized == "sent" || normalized == "delivered" {
		status = normalized
	}
	return emailSendResult{
		MessageID:         response.MessageID,
		ProviderMessageID: response.ID,
		Status:            status,
	}, nil
}

func (s *zeaburEmailSender) postSend(ctx context.Context, endpoint string, apiKey string, body []byte) (zeaburSendResponse, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return zeaburSendResponse{}, apperrors.New(50060, http.StatusInternalServerError, "构造 Zeabur 邮件请求失败")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+apiKey)
	request.Header.Set("Accept", "application/json")

	httpResponse, err := s.client.Do(request)
	if err != nil {
		return zeaburSendResponse{}, err
	}
	defer httpResponse.Body.Close()

	raw, readErr := io.ReadAll(io.LimitReader(httpResponse.Body, zeaburMaxBodyRead))
	if httpResponse.StatusCode < 200 || httpResponse.StatusCode >= 300 {
		return zeaburSendResponse{}, classifyZeaburHTTPError(httpResponse.StatusCode, raw)
	}
	if readErr != nil {
		return zeaburSendResponse{}, apperrors.New(50060, http.StatusBadGateway, "读取 Zeabur 邮件响应失败")
	}

	var parsed zeaburSendResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return zeaburSendResponse{}, apperrors.New(50060, http.StatusBadGateway, "Zeabur 邮件响应格式异常")
	}
	if strings.TrimSpace(parsed.ID) == "" {
		return zeaburSendResponse{}, apperrors.New(50060, http.StatusBadGateway, "Zeabur 未返回邮件 ID，无法追踪投递状态")
	}
	return parsed, nil
}

// classifyZeaburHTTPError 把上游状态码翻译成可直接展示给管理员的中文诊断。
// HTTPStatus 会被 resilience 的重试判定复用，所以映射必须忠实于「这次还值不值得重试」。
func classifyZeaburHTTPError(statusCode int, raw []byte) error {
	var parsed zeaburErrorResponse
	_ = json.Unmarshal(raw, &parsed)
	detail := strings.TrimSpace(parsed.Message)
	if detail == "" {
		detail = strings.TrimSpace(parsed.Error)
	}
	if detail == "" {
		detail = strings.TrimSpace(string(raw))
	}
	detail = clampDetail(detail)

	switch statusCode {
	case http.StatusBadRequest:
		return apperrors.New(40070, http.StatusBadRequest, withDetail("Zeabur 拒绝了本次请求（参数校验未通过）", detail))
	case http.StatusUnauthorized:
		return apperrors.New(40071, http.StatusUnauthorized, withDetail("Zeabur API Key 无效或已被吊销，请在控制台重新生成", detail))
	case http.StatusForbidden:
		return apperrors.New(40072, http.StatusForbidden, withDetail("Zeabur 拒绝发信：API Key 无该发件域权限，或账号处于 paused/banned 状态", detail))
	case http.StatusNotFound:
		return apperrors.New(40473, http.StatusNotFound, withDetail("Zeabur 接口地址不存在，请检查 API 地址配置", detail))
	case http.StatusTooManyRequests:
		// 日配额按 UTC 00:00 重置，重试解决不了问题，原样把「何时恢复」交给管理员。
		return apperrors.New(42974, http.StatusTooManyRequests, withDetail("Zeabur 邮件配额已用尽（新账号 100 封/天，验证域名后 1000 封/天）", detail))
	default:
		if statusCode >= 500 {
			return apperrors.New(50060, http.StatusBadGateway, withDetail(fmt.Sprintf("Zeabur 邮件服务返回 %d，请稍后重试", statusCode), detail))
		}
		return apperrors.New(50060, http.StatusBadGateway, withDetail(fmt.Sprintf("Zeabur 邮件发送失败（HTTP %d）", statusCode), detail))
	}
}

// zeaburShouldRetry 在默认策略之上把 429 摘出来：
// 配额耗尽要等到 UTC 次日重置，重试只会白白拖长请求并加深熔断。
func zeaburShouldRetry(err error) bool {
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) && appErr.HTTPStatus == http.StatusTooManyRequests {
		return false
	}
	return resilience.RetryableError(err)
}

func wrapZeaburError(err error) error {
	if circuitbreaker.IsOpenError(err) {
		return apperrors.New(50314, http.StatusServiceUnavailable, "邮件服务暂不可用，Zeabur 邮件通道正在熔断保护中，请稍后重试")
	}
	var appErr *apperrors.AppError
	if errors.As(err, &appErr) {
		return appErr
	}
	return apperrors.New(50060, http.StatusBadGateway, "调用 Zeabur 邮件接口失败："+err.Error())
}

func zeaburEndpoint(baseURL string, path string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = emaildomain.ZeaburDefaultBaseURL
	}
	return base + path
}
