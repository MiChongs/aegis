package service

import (
	emaildomain "aegis/internal/domain/email"
	"aegis/pkg/circuitbreaker"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/resilience"
	"aegis/pkg/timeutil"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/mail"
	"strings"

	mailpkg "github.com/wneessen/go-mail"
	"go.uber.org/zap"
)

// smtpEmailSender 是直连 SMTP 的发送器。
//
// 注意：Zeabur / Linode / 多数 Serverless 平台在网络层封禁出站 25/465/587，
// 这条通道在那些环境里必然超时，`classifyEmailSendError` 会据此给出改用 zeabur 渠道的提示。
type smtpEmailSender struct {
	log *zap.Logger
}

func newSMTPEmailSender(log *zap.Logger) *smtpEmailSender {
	return &smtpEmailSender{log: log}
}

func (s *smtpEmailSender) Provider() string { return emaildomain.ProviderSMTP }

// SupportsAttachments SMTP 天生支持 MIME 附件。
func (s *smtpEmailSender) SupportsAttachments() bool { return true }

func (s *smtpEmailSender) Validate(config emaildomain.Config) error {
	cfg := config.SMTP
	if strings.TrimSpace(cfg.Host) == "" {
		return apperrors.New(40063, http.StatusBadRequest, "SMTP Host 不能为空")
	}
	if cfg.Port <= 0 {
		return apperrors.New(40064, http.StatusBadRequest, "SMTP 端口无效")
	}
	if strings.TrimSpace(cfg.Username) == "" || strings.TrimSpace(cfg.Password) == "" {
		return apperrors.New(40065, http.StatusBadRequest, "SMTP 账号或密码不能为空")
	}
	if _, err := mail.ParseAddress(strings.TrimSpace(cfg.FromAddress)); err != nil {
		return apperrors.New(40066, http.StatusBadRequest, "发件人邮箱格式错误")
	}
	return nil
}

func (s *smtpEmailSender) Send(ctx context.Context, config *emaildomain.Config, out emailOutbound) (emailSendResult, error) {
	options := []mailpkg.Option{
		mailpkg.WithPort(config.SMTP.Port),
		mailpkg.WithUsername(config.SMTP.Username),
		mailpkg.WithPassword(config.SMTP.Password),
		mailpkg.WithSMTPAuth(mailpkg.SMTPAuthAutoDiscover),
		mailpkg.WithTimeout(timeutil.Seconds(10)),
		mailpkg.WithTLSConfig(&tls.Config{InsecureSkipVerify: config.SMTP.InsecureSkipVerify, ServerName: config.SMTP.Host}),
	}
	if config.SMTP.UseTLS {
		options = append(options, mailpkg.WithSSL())
	} else {
		options = append(options, mailpkg.WithTLSPolicy(mailpkg.TLSMandatory))
	}
	client, err := mailpkg.NewClient(config.SMTP.Host, options...)
	if err != nil {
		s.log.Error("build email client failed", zap.Int64("appid", config.AppID), zap.String("config", config.Name), zap.Error(err))
		return emailSendResult{}, classifyEmailClientError(config, err)
	}

	msg := mailpkg.NewMsg()
	if strings.TrimSpace(config.SMTP.FromName) != "" {
		err = msg.FromFormat(config.SMTP.FromName, config.SMTP.FromAddress)
	} else {
		err = msg.From(config.SMTP.FromAddress)
	}
	if err == nil {
		err = msg.To(out.To)
	}
	if err == nil && strings.TrimSpace(config.SMTP.ReplyTo) != "" {
		err = msg.ReplyTo(config.SMTP.ReplyTo)
	}
	if err != nil {
		return emailSendResult{}, apperrors.New(40062, http.StatusBadRequest, "邮件地址配置错误")
	}
	msg.Subject(out.Subject)
	msg.SetBodyString(mailpkg.TypeTextHTML, out.HTML)
	if text := strings.TrimSpace(out.Text); text != "" {
		msg.AddAlternativeString(mailpkg.TypeTextPlain, text)
	}
	for _, attachment := range out.Attachments {
		contentType := strings.TrimSpace(attachment.ContentType)
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		if err := msg.AttachReader(attachment.Filename, bytes.NewReader(attachment.Content),
			mailpkg.WithFileContentType(mailpkg.ContentType(contentType))); err != nil {
			// 附件挂不上就别把信发出去：一封「您的收据见附件」却没有附件的邮件
			// 会让用户以为收据丢了，比发信失败更难排查。
			s.log.Error("attach email file failed",
				zap.Int64("appid", config.AppID), zap.String("file", attachment.Filename), zap.Error(err))
			return emailSendResult{}, apperrors.New(50061, http.StatusInternalServerError, "邮件附件构造失败："+attachment.Filename)
		}
	}
	msg.SetMessageID()

	breakerName := circuitbreaker.Name("email", fmt.Sprintf("app-%d", config.AppID), config.Name)
	if _, err := resilience.Execute(ctx, breakerName, resilience.Options{
		Timeout:     timeutil.Seconds(12),
		MaxRetries:  2,
		BaseBackoff: timeutil.Milliseconds(300),
		MaxBackoff:  timeutil.Milliseconds(1500),
	}, func(_ context.Context) (struct{}, error) {
		return struct{}{}, client.DialAndSend(msg)
	}); err != nil {
		s.log.Error("send email failed", zap.Int64("appid", config.AppID), zap.String("config", config.Name), zap.Error(err))
		return emailSendResult{}, classifyEmailSendError(config, err)
	}

	return emailSendResult{
		MessageID: msg.GetMessageID(),
		// SMTP 协议本身没有异步回执，交出信件即是这条链路能确认的最终状态。
		Status: emaildomain.DeliveryStatusSent,
	}, nil
}

func classifyEmailClientError(config *emaildomain.Config, err error) error {
	address := smtpAddress(config)
	message := "邮件客户端初始化失败，请检查 SMTP 主机、端口和加密配置"

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return apperrors.New(50060, http.StatusGatewayTimeout, fmt.Sprintf("连接 SMTP 服务器 %s 超时，请检查网络或防火墙", address))
	case strings.Contains(strings.ToLower(err.Error()), "missing address"):
		message = "SMTP 主机地址不能为空"
	case strings.Contains(strings.ToLower(err.Error()), "port"):
		message = "SMTP 端口配置无效"
	}

	return apperrors.New(50060, http.StatusBadGateway, message)
}

func classifyEmailSendError(config *emaildomain.Config, err error) error {
	address := smtpAddress(config)
	lower := strings.ToLower(err.Error())

	var netErr net.Error
	switch {
	case circuitbreaker.IsOpenError(err):
		return apperrors.New(50314, http.StatusServiceUnavailable, fmt.Sprintf("邮件服务暂不可用，SMTP 通道 %s 正在熔断保护中，请稍后重试", address))
	case errors.Is(err, context.DeadlineExceeded):
		return apperrors.New(50060, http.StatusGatewayTimeout, smtpBlockedHint(address))
	case errors.As(err, &netErr) && netErr.Timeout():
		return apperrors.New(50060, http.StatusGatewayTimeout, smtpBlockedHint(address))
	case strings.Contains(lower, "i/o timeout"):
		return apperrors.New(50060, http.StatusGatewayTimeout, smtpBlockedHint(address))
	case strings.Contains(lower, "no such host"):
		return apperrors.New(50060, http.StatusBadGateway, fmt.Sprintf("SMTP 主机 %s 无法解析，请检查主机地址是否填写正确", config.SMTP.Host))
	case strings.Contains(lower, "connection refused") || strings.Contains(lower, "actively refused"):
		return apperrors.New(50060, http.StatusBadGateway, fmt.Sprintf("SMTP 服务器 %s 拒绝连接，请检查端口和加密方式是否匹配", address))
	case strings.Contains(lower, "authentication failed") || strings.Contains(lower, "auth failed") || strings.Contains(lower, "535") || strings.Contains(lower, "username and password not accepted"):
		return apperrors.New(50060, http.StatusBadGateway, "SMTP 认证失败，请检查账号、密码或授权码是否正确")
	case strings.Contains(lower, "certificate") || strings.Contains(lower, "x509") || strings.Contains(lower, "tls") || strings.Contains(lower, "handshake"):
		return apperrors.New(50060, http.StatusBadGateway, fmt.Sprintf("SMTP TLS 握手失败，请检查 %s 的证书、端口和加密方式是否匹配", address))
	default:
		return apperrors.New(50060, http.StatusBadGateway, fmt.Sprintf("邮件发送失败，请检查 SMTP 服务器 %s、端口、加密方式和认证信息", address))
	}
}

// smtpBlockedHint 在超时场景下顺带点破最常见的根因。
// 出站 SMTP 被平台封禁时表现就是纯超时，只说「检查网络」会让人一路怀疑到邮箱服务商那边去。
func smtpBlockedHint(address string) string {
	return fmt.Sprintf("连接 SMTP 服务器 %s 超时，请检查服务器网络、防火墙或端口是否开放；"+
		"若本实例部署在 Zeabur / Linode 等封禁出站 SMTP 端口的平台，请把邮件配置的服务商改为 zeabur（Zeabur Email HTTP API）", address)
}

func smtpAddress(config *emaildomain.Config) string {
	if config == nil {
		return "<unknown>"
	}
	return fmt.Sprintf("%s:%d", strings.TrimSpace(config.SMTP.Host), config.SMTP.Port)
}
