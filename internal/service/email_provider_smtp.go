package service

import (
	emaildomain "aegis/internal/domain/email"
	"aegis/pkg/circuitbreaker"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/resilience"
	"aegis/pkg/timeutil"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	mailpkg "github.com/wneessen/go-mail"
	"go.uber.org/zap"
)

const smtpDefaultPort = 587

// smtpEmailSender 是直连 SMTP 的发送器。
//
// 注意：Zeabur / Linode / 多数 Serverless 平台在网络层封禁出站 25/465/587，
// 这条通道在那些环境里必然超时，`classifyEmailSendError` 会据此给出改用其它渠道的提示。
type smtpEmailSender struct {
	log *zap.Logger
}

func newSMTPEmailSender(log *zap.Logger) *smtpEmailSender {
	return &smtpEmailSender{log: log}
}

func (s *smtpEmailSender) Provider() string { return emaildomain.ProviderSMTP }

// SupportsAttachments SMTP 天生支持 MIME 附件。
func (s *smtpEmailSender) SupportsAttachments() bool { return true }

func (s *smtpEmailSender) Describe() emaildomain.ProviderMeta {
	return emaildomain.ProviderMeta{
		Provider:    emaildomain.ProviderSMTP,
		Name:        "SMTP 直连",
		Description: "用任意支持 SMTP 的邮箱账号发信，不依赖任何第三方 API。",
		Category:    emaildomain.CategoryDirect,
		Icon:        "maildotru",
		BrandColor:  "#0F766E",
		Capabilities: emaildomain.ProviderCapabilities{
			Attachments: true,
		},
		Notes: []string{
			"部署在 Zeabur / Linode 等封禁出站 SMTP 端口的平台时，这条通道会一律超时，请改用 REST 类服务商。",
			"多数邮箱服务商（QQ / 163 / Gmail）要求用「授权码」而不是登录密码。",
		},
		Fields: emFields(
			emIn(emaildomain.GroupCredential,
				emText("host", "SMTP 主机", "smtp.example.com", "", true),
				emNumber("port", "端口", "587", "常见取值：465（隐式 TLS）、587（STARTTLS）、2525（部分服务商的备用端口）", smtpDefaultPort),
				emSelect("encryption", "加密方式", "选错会表现为 TLS 握手失败或连接被立刻关闭",
					emaildomain.SMTPEncryptionSTARTTLS,
					emOption(emaildomain.SMTPEncryptionSTARTTLS, "STARTTLS（587）"),
					emOption(emaildomain.SMTPEncryptionSSL, "隐式 TLS / SSL（465）"),
				),
				emText("username", "用户名", "user@example.com", "", true),
				emSecret("password", "密码 / 授权码", "SMTP 密码", "多数邮箱要求填授权码而非登录密码", true),
			),
			senderIdentityFields("必须是该 SMTP 账号有权代发的地址，否则会被服务商拒收"),
			emIn(emaildomain.GroupAdvanced,
				emSwitch("insecureSkipVerify", "跳过证书校验",
					"仅用于自签证书的内网邮件服务器。打开后中间人可以解密这条链路上的全部邮件", false),
			),
		),
	}
}

func (s *smtpEmailSender) Validate(config emaildomain.Config) error {
	if err := validateByCatalog(s.Describe(), config); err != nil {
		return err
	}
	if config.SettingInt("port", smtpDefaultPort) <= 0 {
		return apperrors.New(40064, http.StatusBadRequest, "SMTP 端口无效")
	}
	return nil
}

func (s *smtpEmailSender) Send(ctx context.Context, config *emaildomain.Config, out emailOutbound) (emailSendResult, error) {
	host := config.Setting("host")
	options := []mailpkg.Option{
		mailpkg.WithPort(config.SettingInt("port", smtpDefaultPort)),
		mailpkg.WithUsername(config.Setting("username")),
		mailpkg.WithPassword(config.Secret("password")),
		mailpkg.WithSMTPAuth(mailpkg.SMTPAuthAutoDiscover),
		mailpkg.WithTimeout(timeutil.Seconds(10)),
		mailpkg.WithTLSConfig(&tls.Config{
			InsecureSkipVerify: config.SettingBool("insecureSkipVerify"), //nolint:gosec // 由管理员显式开启，用于自签证书的内网邮件服务器
			ServerName:         host,
		}),
	}
	if smtpEncryption(*config) == emaildomain.SMTPEncryptionSSL {
		options = append(options, mailpkg.WithSSL())
	} else {
		options = append(options, mailpkg.WithTLSPolicy(mailpkg.TLSMandatory))
	}
	client, err := mailpkg.NewClient(host, options...)
	if err != nil {
		s.log.Error("build email client failed", zap.Int64("appid", config.AppID), zap.String("config", config.Name), zap.Error(err))
		return emailSendResult{}, classifyEmailClientError(config, err)
	}

	fromAddress, fromName, replyTo := config.SenderIdentity()
	msg := mailpkg.NewMsg()
	if strings.TrimSpace(fromName) != "" {
		err = msg.FromFormat(fromName, fromAddress)
	} else {
		err = msg.From(fromAddress)
	}
	if err == nil {
		err = msg.To(out.To)
	}
	if err == nil && replyTo != "" {
		err = msg.ReplyTo(replyTo)
	}
	if err != nil {
		return emailSendResult{}, apperrors.New(40062, http.StatusBadRequest, "邮件地址配置错误")
	}
	msg.Subject(truncateSubject(out.Subject))
	msg.SetBodyString(mailpkg.TypeTextHTML, out.HTML)
	if text := strings.TrimSpace(out.Text); text != "" {
		msg.AddAlternativeString(mailpkg.TypeTextPlain, text)
	}
	if err := attachToMessage(msg, out.Attachments); err != nil {
		s.log.Error("attach email file failed", zap.Int64("appid", config.AppID), zap.Error(err))
		return emailSendResult{}, err
	}
	msg.SetMessageID()

	breakerName := circuitbreaker.Name("email", fmt.Sprintf("scope-%d", config.AppID), config.Name)
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

// smtpEncryption 解析加密方式。
// 存量行由仓储层在解载荷时已经翻成枚举，这里只兜一个默认值。
func smtpEncryption(config emaildomain.Config) string {
	if value := strings.ToLower(config.Setting("encryption")); value == emaildomain.SMTPEncryptionSSL {
		return emaildomain.SMTPEncryptionSSL
	}
	return emaildomain.SMTPEncryptionSTARTTLS
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
		return apperrors.New(50060, http.StatusBadGateway, fmt.Sprintf("SMTP 主机 %s 无法解析，请检查主机地址是否填写正确", config.Setting("host")))
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
		"若本实例部署在 Zeabur / Linode 等封禁出站 SMTP 端口的平台，请把服务商改成走 HTTP API 的那几档"+
		"（Zeabur Email / AWS SES / Resend / SendGrid / Mailgun / Postmark / 阿里云 / 腾讯云）", address)
}

func smtpAddress(config *emaildomain.Config) string {
	if config == nil {
		return "<unknown>"
	}
	return fmt.Sprintf("%s:%d", config.Setting("host"), config.SettingInt("port", smtpDefaultPort))
}
