package service

import (
	emaildomain "aegis/internal/domain/email"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/http"
	"net/mail"
	"strings"
	"time"

	redislib "github.com/redis/go-redis/v9"
	mailpkg "github.com/wneessen/go-mail"
	"go.uber.org/zap"
)

type EmailService struct {
	log       *zap.Logger
	pg        *pgrepo.Repository
	redis     *redislib.Client
	keyPrefix string
}

func NewEmailService(log *zap.Logger, pg *pgrepo.Repository, redis *redislib.Client, keyPrefix string) *EmailService {
	return &EmailService{log: log, pg: pg, redis: redis, keyPrefix: keyPrefix}
}

func (s *EmailService) ListConfigs(ctx context.Context, appID int64) ([]emaildomain.Config, error) {
	if _, err := s.requireApp(ctx, appID); err != nil {
		return nil, err
	}
	return s.pg.ListEmailConfigs(ctx, appID)
}

func (s *EmailService) Detail(ctx context.Context, appID int64, id int64) (*emaildomain.Config, error) {
	if _, err := s.requireApp(ctx, appID); err != nil {
		return nil, err
	}
	item, err := s.pg.GetEmailConfigByID(ctx, appID, id)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40460, http.StatusNotFound, "邮件配置不存在")
	}
	return item, nil
}

func (s *EmailService) Save(ctx context.Context, mutation emaildomain.ConfigMutation) (*emaildomain.Config, error) {
	if _, err := s.requireApp(ctx, mutation.AppID); err != nil {
		return nil, err
	}
	current, err := s.pg.GetEmailConfigByID(ctx, mutation.AppID, mutation.ID)
	if err != nil {
		return nil, err
	}
	item := emaildomain.Config{
		ID:        mutation.ID,
		AppID:     mutation.AppID,
		Name:      "default",
		Provider:  "smtp",
		Enabled:   true,
		IsDefault: mutation.ID == 0,
		SMTP: emaildomain.SMTPConfig{
			Port:               587,
			MaxConnections:     5,
			MaxMessagesPerConn: 100,
		},
	}
	if current != nil {
		item = *current
	}
	if mutation.Name != nil {
		item.Name = strings.TrimSpace(*mutation.Name)
	}
	if mutation.Provider != nil {
		item.Provider = strings.TrimSpace(*mutation.Provider)
	}
	if mutation.Enabled != nil {
		item.Enabled = *mutation.Enabled
	}
	if mutation.IsDefault != nil {
		item.IsDefault = *mutation.IsDefault
	}
	if mutation.Description != nil {
		item.Description = strings.TrimSpace(*mutation.Description)
	}
	if mutation.SMTP != nil {
		item.SMTP = *mutation.SMTP
	}
	if strings.TrimSpace(item.Name) == "" {
		return nil, apperrors.New(40060, http.StatusBadRequest, "配置名称不能为空")
	}
	if strings.TrimSpace(item.Provider) == "" {
		item.Provider = "smtp"
	}
	if err := validateEmailConfig(item.SMTP); err != nil {
		return nil, err
	}
	return s.pg.UpsertEmailConfig(ctx, item)
}

func (s *EmailService) Delete(ctx context.Context, appID int64, id int64) error {
	deleted, err := s.pg.DeleteEmailConfig(ctx, appID, id)
	if err != nil {
		return err
	}
	if !deleted {
		return apperrors.New(40460, http.StatusNotFound, "邮件配置不存在")
	}
	return nil
}

func (s *EmailService) TestConfig(ctx context.Context, appID int64, id int64, email string) (*emaildomain.VerificationResult, error) {
	config, err := s.Detail(ctx, appID, id)
	if err != nil {
		return nil, err
	}
	code := generateEmailCode()
	expireAt, messageID, err := s.sendCodeMail(ctx, appID, config, email, code, "test", 5)
	if err != nil {
		return nil, err
	}
	return &emaildomain.VerificationResult{Success: true, Email: email, Purpose: "test", Code: code, ExpireAt: expireAt, MessageID: messageID}, nil
}

func (s *EmailService) SendVerificationCode(ctx context.Context, appID int64, email string, purpose string, expireMinutes int, configName string) (*emaildomain.VerificationResult, error) {
	config, err := s.resolveConfig(ctx, appID, configName)
	if err != nil {
		return nil, err
	}
	code := generateEmailCode()
	expireAt, messageID, err := s.sendCodeMail(ctx, appID, config, email, code, purpose, expireMinutes)
	if err != nil {
		return nil, err
	}
	if err := s.redis.Set(ctx, s.emailCodeKey(appID, purpose, email), code, time.Duration(expireMinutes)*time.Minute).Err(); err != nil {
		return nil, err
	}
	return &emaildomain.VerificationResult{Success: true, Email: email, Purpose: purpose, ExpireAt: expireAt, MessageID: messageID}, nil
}

func (s *EmailService) VerifyCode(ctx context.Context, appID int64, email string, code string, purpose string) (bool, error) {
	stored, err := s.redis.Get(ctx, s.emailCodeKey(appID, purpose, email)).Result()
	if err != nil {
		if err == redislib.Nil {
			return false, nil
		}
		return false, err
	}
	valid := strings.TrimSpace(stored) == strings.TrimSpace(code)
	if valid {
		_ = s.redis.Del(ctx, s.emailCodeKey(appID, purpose, email)).Err()
	}
	return valid, nil
}

func (s *EmailService) SendPasswordResetEmail(ctx context.Context, appID int64, email string, resetBaseURL string, configName string) (*emaildomain.ResetResult, error) {
	config, err := s.resolveConfig(ctx, appID, configName)
	if err != nil {
		return nil, err
	}
	app, err := s.requireApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	token, err := generateResetToken()
	if err != nil {
		return nil, err
	}
	expireAt := time.Now().Add(30 * time.Minute)
	resetURL := strings.TrimRight(strings.TrimSpace(resetBaseURL), "/")
	if resetURL != "" {
		resetURL += "?token=" + token + "&email=" + email
	}
	subject := fmt.Sprintf("%s 密码重置通知", app.Name)
	html := fmt.Sprintf(`<div style="font-family:Arial,sans-serif;line-height:1.7"><h2>%s</h2><p>收到密码重置请求。</p><p>重置链接：</p><p><a href="%s">%s</a></p><p>链接 30 分钟内有效。</p></div>`, app.Name, resetURL, resetURL)
	messageID, err := s.sendMail(config, email, subject, html)
	if err != nil {
		return nil, err
	}
	if err := s.redis.Set(ctx, s.resetTokenKey(appID, email), token, 30*time.Minute).Err(); err != nil {
		return nil, err
	}
	return &emaildomain.ResetResult{Success: true, Email: email, Token: token, ResetURL: resetURL, ExpireAt: expireAt, MessageID: messageID}, nil
}

func (s *EmailService) VerifyResetToken(ctx context.Context, appID int64, email string, token string) (bool, error) {
	stored, err := s.redis.Get(ctx, s.resetTokenKey(appID, email)).Result()
	if err != nil {
		if err == redislib.Nil {
			return false, nil
		}
		return false, err
	}
	return strings.TrimSpace(stored) == strings.TrimSpace(token), nil
}

func (s *EmailService) SendWelcomeEmail(ctx context.Context, appID int64, email string, userName string, configName string) error {
	config, err := s.resolveConfig(ctx, appID, configName)
	if err != nil {
		return err
	}
	app, err := s.requireApp(ctx, appID)
	if err != nil {
		return err
	}
	subject := fmt.Sprintf("欢迎加入 %s", app.Name)
	html := fmt.Sprintf(`<div style="font-family:Arial,sans-serif;line-height:1.7"><h2>欢迎加入 %s</h2><p>%s，您好。</p><p>您的账号已完成初始化。</p></div>`, app.Name, strings.TrimSpace(userName))
	_, err = s.sendMail(config, email, subject, html)
	return err
}

func (s *EmailService) resolveConfig(ctx context.Context, appID int64, configName string) (*emaildomain.Config, error) {
	if _, err := s.requireApp(ctx, appID); err != nil {
		return nil, err
	}
	var (
		item *emaildomain.Config
		err  error
	)
	if strings.TrimSpace(configName) != "" {
		item, err = s.pg.GetEmailConfigByName(ctx, appID, configName)
	} else {
		item, err = s.pg.GetDefaultEmailConfig(ctx, appID)
	}
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40461, http.StatusNotFound, "未配置可用邮件服务")
	}
	if !item.Enabled {
		return nil, apperrors.New(40061, http.StatusBadRequest, "邮件配置未启用")
	}
	return item, nil
}

func (s *EmailService) requireApp(ctx context.Context, appID int64) (appNameHolder, error) {
	app, err := s.pg.GetAppByID(ctx, appID)
	if err != nil {
		return appNameHolder{}, err
	}
	if app == nil {
		return appNameHolder{}, apperrors.New(40410, http.StatusNotFound, "无法找到该应用")
	}
	return appNameHolder{Name: app.Name}, nil
}

func (s *EmailService) sendCodeMail(ctx context.Context, appID int64, config *emaildomain.Config, email string, code string, purpose string, expireMinutes int) (time.Time, string, error) {
	app, err := s.requireApp(ctx, appID)
	if err != nil {
		return time.Time{}, "", err
	}
	if expireMinutes <= 0 {
		expireMinutes = 5
	}
	expireAt := time.Now().Add(time.Duration(expireMinutes) * time.Minute)
	subject := fmt.Sprintf("%s 验证码", app.Name)
	html := fmt.Sprintf(`<div style="font-family:Arial,sans-serif;line-height:1.7"><h2>%s</h2><p>用途：%s</p><p>验证码：</p><div style="font-size:32px;font-weight:700;letter-spacing:8px">%s</div><p>%d 分钟内有效。</p></div>`, app.Name, purpose, code, expireMinutes)
	messageID, err := s.sendMail(config, email, subject, html)
	return expireAt, messageID, err
}

func (s *EmailService) sendMail(config *emaildomain.Config, to string, subject string, html string) (string, error) {
	if _, err := mail.ParseAddress(strings.TrimSpace(to)); err != nil {
		return "", apperrors.New(40062, http.StatusBadRequest, "邮箱地址格式错误")
	}
	options := []mailpkg.Option{
		mailpkg.WithPort(config.SMTP.Port),
		mailpkg.WithUsername(config.SMTP.Username),
		mailpkg.WithPassword(config.SMTP.Password),
		mailpkg.WithSMTPAuth(mailpkg.SMTPAuthAutoDiscover),
		mailpkg.WithTimeout(10 * time.Second),
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
		return "", apperrors.New(50060, http.StatusInternalServerError, "邮件发送失败")
	}

	msg := mailpkg.NewMsg()
	if strings.TrimSpace(config.SMTP.FromName) != "" {
		err = msg.FromFormat(config.SMTP.FromName, config.SMTP.FromAddress)
	} else {
		err = msg.From(config.SMTP.FromAddress)
	}
	if err == nil {
		err = msg.To(to)
	}
	if err == nil && strings.TrimSpace(config.SMTP.ReplyTo) != "" {
		err = msg.ReplyTo(config.SMTP.ReplyTo)
	}
	if err != nil {
		return "", apperrors.New(40062, http.StatusBadRequest, "邮件地址配置错误")
	}
	msg.Subject(subject)
	msg.SetBodyString(mailpkg.TypeTextHTML, html)
	msg.SetMessageID()

	if err := client.DialAndSend(msg); err != nil {
		s.log.Error("send email failed", zap.Int64("appid", config.AppID), zap.String("config", config.Name), zap.Error(err))
		return "", apperrors.New(50060, http.StatusInternalServerError, "邮件发送失败")
	}
	messageID := msg.GetMessageID()
	if messageID == "" {
		messageID = fmt.Sprintf("%d-%s", time.Now().UnixNano(), strings.ReplaceAll(strings.ToLower(subject), " ", "-"))
	}
	return messageID, nil
}

func (s *EmailService) emailCodeKey(appID int64, purpose string, email string) string {
	return fmt.Sprintf("%s:email:code:%d:%s:%s", s.keyPrefix, appID, strings.TrimSpace(purpose), strings.ToLower(strings.TrimSpace(email)))
}

func (s *EmailService) resetTokenKey(appID int64, email string) string {
	return fmt.Sprintf("%s:email:reset:%d:%s", s.keyPrefix, appID, strings.ToLower(strings.TrimSpace(email)))
}

type appNameHolder struct {
	Name string
}

func validateEmailConfig(cfg emaildomain.SMTPConfig) error {
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

func generateEmailCode() string {
	const digits = "0123456789"
	buf := make([]byte, 6)
	for i := range buf {
		n, _ := rand.Int(rand.Reader, big.NewInt(10))
		buf[i] = digits[n.Int64()]
	}
	return string(buf)
}

func generateResetToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
