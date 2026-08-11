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
	"net/mail"
	"strings"

	"go.uber.org/zap"
)

const (
	zeaburHTTPTimeout   = 15
	zeaburMaxBodyRead   = 1 << 20 // 出错时最多读 1MB 响应体，避免异常网关把内存打满
	zeaburSendPath      = "/emails"
	zeaburSubjectMaxLen = 998
)

// zeaburEmailSender 走 Zeabur Email 的 REST API。
//
// 这条通道是 Zeabur 部署形态下**唯一**可用的邮件出口：
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

func (s *zeaburEmailSender) Validate(config emaildomain.Config) error {
	cfg := config.Zeabur
	// 密文非空即视为已配置：编辑配置时前端不会回传原 Key（留空表示不修改）。
	if strings.TrimSpace(cfg.APIKey) == "" && strings.TrimSpace(cfg.APIKeyCipher) == "" {
		return apperrors.New(40068, http.StatusBadRequest, "Zeabur API Key 不能为空，请在 Zeabur 控制台的 Email → API Keys 中创建")
	}
	if _, err := mail.ParseAddress(strings.TrimSpace(cfg.FromAddress)); err != nil {
		return apperrors.New(40066, http.StatusBadRequest, "发件人邮箱格式错误")
	}
	if replyTo := strings.TrimSpace(cfg.ReplyTo); replyTo != "" {
		if _, err := mail.ParseAddress(replyTo); err != nil {
			return apperrors.New(40066, http.StatusBadRequest, "回信邮箱格式错误")
		}
	}
	if base := strings.TrimSpace(cfg.BaseURL); base != "" && !strings.HasPrefix(strings.ToLower(base), "https://") {
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
	cfg := config.Zeabur
	apiKey := strings.TrimSpace(cfg.APIKey)
	if apiKey == "" {
		return emailSendResult{}, apperrors.New(40068, http.StatusBadRequest, "Zeabur API Key 未配置或解密失败，请重新填写")
	}

	subject := out.Subject
	if len([]rune(subject)) > zeaburSubjectMaxLen {
		subject = string([]rune(subject)[:zeaburSubjectMaxLen])
	}

	payload := zeaburSendRequest{
		From:    formatZeaburFrom(cfg.FromAddress, cfg.FromName),
		To:      []string{strings.TrimSpace(out.To)},
		Subject: subject,
		HTML:    out.HTML,
		Text:    out.Text,
		Tags:    buildZeaburTags(cfg.Tags, out.Purpose),
	}
	if replyTo := strings.TrimSpace(cfg.ReplyTo); replyTo != "" {
		payload.ReplyTo = []string{replyTo}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return emailSendResult{}, apperrors.New(50060, http.StatusInternalServerError, "构造 Zeabur 邮件请求失败")
	}

	endpoint := zeaburEndpoint(cfg.BaseURL, zeaburSendPath)
	breakerName := circuitbreaker.Name("email-zeabur", fmt.Sprintf("app-%d", config.AppID), config.Name)
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
	if len([]rune(detail)) > 300 {
		detail = string([]rune(detail)[:300])
	}

	switch statusCode {
	case http.StatusBadRequest:
		return apperrors.New(40070, http.StatusBadRequest, withZeaburDetail("Zeabur 拒绝了本次请求（参数校验未通过）", detail))
	case http.StatusUnauthorized:
		return apperrors.New(40071, http.StatusUnauthorized, withZeaburDetail("Zeabur API Key 无效或已被吊销，请在控制台重新生成", detail))
	case http.StatusForbidden:
		return apperrors.New(40072, http.StatusForbidden, withZeaburDetail("Zeabur 拒绝发信：API Key 无该发件域权限，或账号处于 paused/banned 状态", detail))
	case http.StatusNotFound:
		return apperrors.New(40473, http.StatusNotFound, withZeaburDetail("Zeabur 接口地址不存在，请检查 API 地址配置", detail))
	case http.StatusTooManyRequests:
		// 日配额按 UTC 00:00 重置，重试解决不了问题，原样把「何时恢复」交给管理员。
		return apperrors.New(42974, http.StatusTooManyRequests, withZeaburDetail("Zeabur 邮件配额已用尽（新账号 100 封/天，验证域名后 1000 封/天）", detail))
	default:
		if statusCode >= 500 {
			return apperrors.New(50060, http.StatusBadGateway, withZeaburDetail(fmt.Sprintf("Zeabur 邮件服务返回 %d，请稍后重试", statusCode), detail))
		}
		return apperrors.New(50060, http.StatusBadGateway, withZeaburDetail(fmt.Sprintf("Zeabur 邮件发送失败（HTTP %d）", statusCode), detail))
	}
}

func withZeaburDetail(message string, detail string) string {
	if detail == "" {
		return message
	}
	return message + "：" + detail
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

// formatZeaburFrom 组装 RFC 5322 的 "Name <addr>" 形式。
func formatZeaburFrom(address string, name string) string {
	address = strings.TrimSpace(address)
	name = strings.TrimSpace(name)
	if name == "" {
		return address
	}
	return (&mail.Address{Name: name, Address: address}).String()
}

// buildZeaburTags 把业务用途并进标签，方便在 Zeabur 控制台按用途筛选投递情况。
func buildZeaburTags(configured map[string]string, purpose string) map[string]string {
	tags := make(map[string]string, len(configured)+1)
	for key, value := range configured {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		tags[key] = value
	}
	if purpose = strings.TrimSpace(purpose); purpose != "" {
		if _, exists := tags["purpose"]; !exists {
			tags["purpose"] = purpose
		}
	}
	if len(tags) == 0 {
		return nil
	}
	return tags
}
