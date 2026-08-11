package service

import (
	emaildomain "aegis/internal/domain/email"
	platformdomain "aegis/internal/domain/platform"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"math/big"
	"net/http"
	"net/mail"
	"strings"
	"time"

	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type EmailService struct {
	log       *zap.Logger
	pg        *pgrepo.Repository
	redis     *redislib.Client
	keyPrefix string
	// masterKey 用于加解密渠道密钥（Zeabur API Key / webhook 签名密钥），
	// 派生自 SECURITY_MASTER_KEY，与 OAuth / NotifyHub 同构。
	masterKey []byte
	senders   map[string]emailSender
	// governance 平台治理判定：被治理应用不得再对外发信
	governance *PlatformGovernanceService
}

// SetGovernanceService 注入平台治理服务（bootstrap 中调用）。
func (s *EmailService) SetGovernanceService(g *PlatformGovernanceService) { s.governance = g }

func NewEmailService(log *zap.Logger, pg *pgrepo.Repository, redis *redislib.Client, keyPrefix string, masterKey string) *EmailService {
	digest := sha256.Sum256([]byte("aegis.email.master\x00" + masterKey))
	service := &EmailService{
		log:       log,
		pg:        pg,
		redis:     redis,
		keyPrefix: keyPrefix,
		masterKey: digest[:],
	}
	service.senders = map[string]emailSender{
		emaildomain.ProviderSMTP:   newSMTPEmailSender(log),
		emaildomain.ProviderZeabur: newZeaburEmailSender(log),
	}
	return service
}

func (s *EmailService) ListConfigs(ctx context.Context, appID int64) ([]emaildomain.Config, error) {
	if _, err := s.requireApp(ctx, appID); err != nil {
		return nil, err
	}
	items, err := s.pg.ListEmailConfigs(ctx, appID)
	if err != nil {
		return nil, err
	}
	for i := range items {
		sanitizeEmailConfig(&items[i])
	}
	return items, nil
}

func (s *EmailService) Detail(ctx context.Context, appID int64, id int64) (*emaildomain.Config, error) {
	item, err := s.loadConfig(ctx, appID, id)
	if err != nil {
		return nil, err
	}
	sanitized := *item
	sanitizeEmailConfig(&sanitized)
	return &sanitized, nil
}

// loadConfig 取出配置**并解密渠道密钥**，仅供内部发送链路使用。
// 对外出口一律走 Detail / ListConfigs，那两条路径会抹掉所有密钥。
func (s *EmailService) loadConfig(ctx context.Context, appID int64, id int64) (*emaildomain.Config, error) {
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
	s.decryptEmailSecrets(item)
	return item, nil
}

// sanitizeEmailConfig 抹掉一切密钥，只保留「是否已配置」的布尔位。
func sanitizeEmailConfig(item *emaildomain.Config) {
	if item == nil {
		return
	}
	item.SMTP.Password = ""
	item.Zeabur.APIKeySet = item.Zeabur.APIKeySet || strings.TrimSpace(item.Zeabur.APIKey) != ""
	item.Zeabur.WebhookSecretSet = item.Zeabur.WebhookSecretSet || strings.TrimSpace(item.Zeabur.WebhookSecret) != ""
	item.Zeabur.APIKey = ""
	item.Zeabur.APIKeyCipher = ""
	item.Zeabur.WebhookSecret = ""
	item.Zeabur.WebhookSecretCipher = ""
}

// decryptEmailSecrets 把落库密文还原成可用的明文密钥。
// 解密失败不阻断流程：让发送器给出「API Key 未配置或解密失败」这种可操作的报错，
// 比在这里抛一个通用错误更容易定位（典型诱因是换了 SECURITY_MASTER_KEY）。
func (s *EmailService) decryptEmailSecrets(item *emaildomain.Config) {
	if item == nil {
		return
	}
	if cipherText := strings.TrimSpace(item.Zeabur.APIKeyCipher); cipherText != "" {
		if plain, err := decryptSecret(s.masterKey, cipherText); err == nil {
			item.Zeabur.APIKey = plain
		} else {
			s.log.Error("decrypt zeabur api key failed",
				zap.Int64("appid", item.AppID), zap.String("config", item.Name), zap.Error(err))
		}
	}
	if cipherText := strings.TrimSpace(item.Zeabur.WebhookSecretCipher); cipherText != "" {
		if plain, err := decryptSecret(s.masterKey, cipherText); err == nil {
			item.Zeabur.WebhookSecret = plain
		} else {
			s.log.Error("decrypt zeabur webhook secret failed",
				zap.Int64("appid", item.AppID), zap.String("config", item.Name), zap.Error(err))
		}
	}
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
		Provider:  emaildomain.ProviderSMTP,
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
		item.Provider = normalizeEmailProvider(*mutation.Provider)
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
		incoming := *mutation.SMTP
		// 密码留空表示「不修改」—— 前端编辑态从不回显密码，
		// 直接覆盖会让每次改个发件人名就把密码清空，然后在下一次发信时才暴露。
		if strings.TrimSpace(incoming.Password) == "" && current != nil {
			incoming.Password = current.SMTP.Password
		}
		item.SMTP = incoming
	}
	if mutation.Zeabur != nil {
		merged, err := s.mergeZeaburConfig(current, *mutation.Zeabur)
		if err != nil {
			return nil, err
		}
		item.Zeabur = merged
	}
	if strings.TrimSpace(item.Name) == "" {
		return nil, apperrors.New(40060, http.StatusBadRequest, "配置名称不能为空")
	}
	item.Provider = normalizeEmailProvider(item.Provider)

	sender, err := s.resolveSender(item.Provider)
	if err != nil {
		return nil, err
	}
	if err := sender.Validate(item); err != nil {
		return nil, err
	}

	saved, err := s.pg.UpsertEmailConfig(ctx, item)
	if err != nil {
		return nil, err
	}
	sanitizeEmailConfig(saved)
	return saved, nil
}

// mergeZeaburConfig 把提交上来的 Zeabur 配置与既有密文合并。
// 明文密钥进来就重新加密；留空则沿用旧密文，实现「留空不修改」。
func (s *EmailService) mergeZeaburConfig(current *emaildomain.Config, incoming emaildomain.ZeaburConfig) (emaildomain.ZeaburConfig, error) {
	merged := emaildomain.ZeaburConfig{
		BaseURL:     strings.TrimSpace(incoming.BaseURL),
		FromAddress: strings.TrimSpace(incoming.FromAddress),
		FromName:    strings.TrimSpace(incoming.FromName),
		ReplyTo:     strings.TrimSpace(incoming.ReplyTo),
		Tags:        incoming.Tags,
	}
	if current != nil {
		merged.APIKeyCipher = current.Zeabur.APIKeyCipher
		merged.WebhookSecretCipher = current.Zeabur.WebhookSecretCipher
	}
	if apiKey := strings.TrimSpace(incoming.APIKey); apiKey != "" {
		cipherText, err := encryptSecret(s.masterKey, apiKey)
		if err != nil {
			return emaildomain.ZeaburConfig{}, apperrors.New(50060, http.StatusInternalServerError, "加密 Zeabur API Key 失败")
		}
		merged.APIKeyCipher = cipherText
		merged.APIKey = apiKey
	}
	if secret := strings.TrimSpace(incoming.WebhookSecret); secret != "" {
		cipherText, err := encryptSecret(s.masterKey, secret)
		if err != nil {
			return emaildomain.ZeaburConfig{}, apperrors.New(50060, http.StatusInternalServerError, "加密 Zeabur Webhook 密钥失败")
		}
		merged.WebhookSecretCipher = cipherText
		merged.WebhookSecret = secret
	}
	merged.APIKeySet = strings.TrimSpace(merged.APIKeyCipher) != ""
	merged.WebhookSecretSet = strings.TrimSpace(merged.WebhookSecretCipher) != ""
	return merged, nil
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
	config, err := s.loadConfig(ctx, appID, id)
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
	if err := s.redis.Set(ctx, s.emailCodeKey(appID, purpose, email), code, timeutil.Minutes(expireMinutes)).Err(); err != nil {
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
	expireAt := timeutil.Now().Add(timeutil.Minutes(30))
	resetURL := strings.TrimRight(strings.TrimSpace(resetBaseURL), "/")
	if resetURL != "" {
		resetURL += "?token=" + token + "&email=" + email
	}
	subject := fmt.Sprintf("%s 密码重置通知", app.Name)
	html := renderEmailLayout(
		app.Name,
		"安全通知",
		"密码重置请求",
		"系统收到一次密码重置申请，请在 30 分钟内完成验证。",
		renderPasswordResetBody(resetURL),
		"如果这不是您本人发起的操作，请忽略本邮件，并尽快检查账号安全设置。",
	)
	messageID, err := s.sendMail(ctx, config, email, subject, html, "password_reset")
	if err != nil {
		return nil, err
	}
	if err := s.redis.Set(ctx, s.resetTokenKey(appID, email), token, timeutil.Minutes(30)).Err(); err != nil {
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
	html := renderEmailLayout(
		app.Name,
		"账号开通成功",
		fmt.Sprintf("欢迎加入 %s", app.Name),
		renderWelcomeLead(userName),
		renderWelcomeBody(app.Name),
		"建议首次登录后尽快完善资料，并开启更高等级的账号安全保护。",
	)
	_, err = s.sendMail(ctx, config, email, subject, html, "welcome")
	return err
}

func (s *EmailService) SendProfileChangeCompletedEmail(ctx context.Context, appID int64, email string, field string, oldValue string, newValue string, configName string) error {
	config, err := s.resolveConfig(ctx, appID, configName)
	if err != nil {
		return err
	}
	app, err := s.requireApp(ctx, appID)
	if err != nil {
		return err
	}
	subject := fmt.Sprintf("%s 资料变更完成通知", app.Name)
	html := renderEmailLayout(
		app.Name,
		"安全通知",
		"资料变更已生效",
		fmt.Sprintf("您的%s已经完成变更。如非本人操作，请立即检查账号安全设置。", describeProfileChangeField(field)),
		renderProfileChangeCompletedBody(field, oldValue, newValue),
		"若本次操作并非您本人发起，建议立即修改密码并检查近期登录记录。",
	)
	_, err = s.sendMail(ctx, config, email, subject, html, "profile_change")
	return err
}

// SendNotificationEmail 通用通知邮件出口，供 NotifyHub 的 email 渠道调用。
// 与业务邮件（验证码/欢迎信）区分：正文由调用方渲染好，这里只负责挑配置并投递。
func (s *EmailService) SendNotificationEmail(ctx context.Context, appID int64, to string, subject string, htmlBody string, configName string) error {
	config, err := s.resolveConfig(ctx, appID, configName)
	if err != nil {
		return err
	}
	subject = strings.TrimSpace(subject)
	if subject == "" {
		subject = "Aegis 通知"
	}
	_, err = s.sendMail(ctx, config, to, subject, htmlBody, "notification")
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
	s.decryptEmailSecrets(item)
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
	expireAt := timeutil.Now().Add(timeutil.Minutes(expireMinutes))
	subject, html := renderCodeMailContent(app.Name, config.Name, purpose, code, expireMinutes)
	messageID, err := s.sendMail(ctx, config, email, subject, html, purpose)
	return expireAt, messageID, err
}

// sendMail 是全平台唯一的邮件出口：挑发送器 → 交付 → 留痕。
// 模板已在调用方渲染完毕，这里不关心内容，只关心「用哪条通道发、发没发出去、发到哪儿了」。
// ChannelCapability 一条邮件通道的能力自述。
type ChannelCapability struct {
	Provider    string `json:"provider"`
	Attachments bool   `json:"attachments"`
}

// ResolveChannelCapability 查某个应用当前生效的邮件通道能带不带附件。
//
// 调用方需要**在渲染正文之前**知道这件事：能带附件的写「收据见附件」，
// 不能的写「点下面的按钮下载」。先发了再看结果就来不及改措辞了。
func (s *EmailService) ResolveChannelCapability(ctx context.Context, appID int64, configName string) (ChannelCapability, error) {
	config, err := s.resolveConfig(ctx, appID, configName)
	if err != nil {
		return ChannelCapability{}, err
	}
	sender, err := s.resolveSender(config.Provider)
	if err != nil {
		return ChannelCapability{}, err
	}
	return ChannelCapability{Provider: sender.Provider(), Attachments: sender.SupportsAttachments()}, nil
}

// DocumentEmail 一封带附件的业务邮件（凭证、对账单等）。
type DocumentEmail struct {
	To      string
	Subject string
	HTML    string
	Purpose string
	// ConfigName 指定邮件配置，留空取默认
	ConfigName string
	// Files 附件；为空时等价于普通通知邮件
	Files []DocumentAttachment
}

// DocumentAttachment 一个附件。
type DocumentAttachment struct {
	Filename    string
	ContentType string
	Content     []byte
}

// SendDocumentEmail 发送带附件的业务邮件。附件为空时等价于普通通知邮件。
func (s *EmailService) SendDocumentEmail(ctx context.Context, appID int64, mail DocumentEmail) (string, error) {
	config, err := s.resolveConfig(ctx, appID, mail.ConfigName)
	if err != nil {
		return "", err
	}
	files := make([]emailAttachment, 0, len(mail.Files))
	for _, file := range mail.Files {
		files = append(files, emailAttachment{
			Filename:    file.Filename,
			ContentType: file.ContentType,
			Content:     file.Content,
		})
	}
	purpose := strings.TrimSpace(mail.Purpose)
	if purpose == "" {
		purpose = "document"
	}
	return s.sendMail(ctx, config, mail.To, mail.Subject, mail.HTML, purpose, files...)
}

func (s *EmailService) sendMail(ctx context.Context, config *emaildomain.Config, to string, subject string, htmlBody string, purpose string, attachments ...emailAttachment) (string, error) {
	to = strings.TrimSpace(to)
	if _, err := mail.ParseAddress(to); err != nil {
		return "", apperrors.New(40062, http.StatusBadRequest, "邮箱地址格式错误")
	}
	// 平台治理挂在 sendMail 而不是各调用点：这里是全平台唯一的邮件出口，
	// 验证码 / 密码重置 / 欢迎信 / NotifyHub 的 email 渠道全部经过这一处。
	if s.governance != nil && config != nil {
		if err := s.governance.EnsureCapability(config.AppID, platformdomain.CapabilityNotification); err != nil {
			return "", err
		}
	}
	sender, err := s.resolveSender(config.Provider)
	if err != nil {
		return "", err
	}
	// 通道带不了附件就当场报错，绝不静默把附件丢掉再把信发出去 ——
	// 那样用户收到的是一封「您的收据见附件」但没有附件的邮件，谁也发现不了。
	if len(attachments) > 0 && !sender.SupportsAttachments() {
		return "", apperrors.New(50062, http.StatusBadGateway,
			"当前邮件通道（"+sender.Provider()+"）不支持附件，请改用 SMTP 通道，或改为发送下载链接")
	}

	out := emailOutbound{
		To:          to,
		Subject:     subject,
		HTML:        htmlBody,
		Text:        htmlToPlainText(htmlBody),
		Purpose:     purpose,
		Attachments: attachments,
	}
	result, sendErr := sender.Send(ctx, config, out)
	s.recordDelivery(ctx, config, out, result, sendErr)
	if sendErr != nil {
		return "", sendErr
	}

	messageID := result.MessageID
	if messageID == "" {
		messageID = result.ProviderMessageID
	}
	if messageID == "" {
		messageID = fmt.Sprintf("%d-%s", timeutil.Now().UnixNano(), strings.ReplaceAll(strings.ToLower(subject), " ", "-"))
	}
	return messageID, nil
}

// recordDelivery 落投递留痕。
//
// 留痕失败绝不能反过来让发信失败 —— 信已经交出去了，这里再报错只会让调用方重发一封。
// 因此所有错误止于一条 warn 日志。
func (s *EmailService) recordDelivery(ctx context.Context, config *emaildomain.Config, out emailOutbound, result emailSendResult, sendErr error) {
	fromAddress, _, _ := config.SenderIdentity()
	record := emaildomain.Delivery{
		AppID:             config.AppID,
		ConfigID:          config.ID,
		ConfigName:        config.Name,
		Provider:          normalizeEmailProvider(config.Provider),
		ProviderMessageID: result.ProviderMessageID,
		MessageID:         result.MessageID,
		ToAddress:         out.To,
		FromAddress:       fromAddress,
		Subject:           out.Subject,
		Purpose:           out.Purpose,
		Status:            result.Status,
	}
	if sendErr != nil {
		record.Status = emaildomain.DeliveryStatusFailed
		record.ErrorMessage = sendErr.Error()
	} else {
		if record.Status == "" {
			record.Status = emaildomain.DeliveryStatusSent
		}
		now := timeutil.Now()
		record.SentAt = &now
	}

	// 用独立超时的 context：调用方的 ctx 可能已随请求结束被取消，
	// 但留痕是发信的事后账，不该跟着请求生命周期一起消失。
	recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeutil.Seconds(5))
	defer cancel()
	if _, err := s.pg.CreateEmailDelivery(recordCtx, record); err != nil {
		s.log.Warn("record email delivery failed",
			zap.Int64("appid", config.AppID), zap.String("config", config.Name),
			zap.String("to", out.To), zap.Error(err))
	}
}

// ListDeliveries 分页查询投递留痕，供控制台排查「这封信到底发出去没有」。
func (s *EmailService) ListDeliveries(ctx context.Context, query emaildomain.DeliveryQuery) (*emaildomain.DeliveryPage, error) {
	if _, err := s.requireApp(ctx, query.AppID); err != nil {
		return nil, err
	}
	return s.pg.ListEmailDeliveries(ctx, query)
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

type emailDetail struct {
	Label string
	Value string
}

type emailPurposePresentation struct {
	Eyebrow     string
	Title       string
	DisplayName string
	Lead        string
	Footer      string
}

func renderCodeMailContent(appName string, configName string, purpose string, code string, expireMinutes int) (string, string) {
	normalizedPurpose := normalizeEmailPurpose(purpose)
	if normalizedPurpose == "test" {
		return fmt.Sprintf("%s 邮件通道测试", appName), renderEmailLayout(
			appName,
			"通道测试",
			"邮件服务连通性验证",
			"这是一封由后台主动触发的测试邮件，用于验证当前邮件配置是否可正常投递。",
			renderCodeBody(code, expireMinutes, []emailDetail{
				{Label: "测试用途", Value: "邮件服务配置验证"},
				{Label: "发送通道", Value: fallbackEmailValue(configName, "默认配置")},
			}),
			"如果您已收到此邮件，说明当前 SMTP 配置、认证和模板渲染链路均已生效。",
		)
	}

	presentation := getEmailPurposePresentation(normalizedPurpose)
	return fmt.Sprintf("%s 验证码", appName), renderEmailLayout(
		appName,
		presentation.Eyebrow,
		presentation.Title,
		presentation.Lead,
		renderCodeBody(code, expireMinutes, []emailDetail{
			{Label: "用途", Value: presentation.DisplayName},
			{Label: "有效期", Value: fmt.Sprintf("%d 分钟", expireMinutes)},
		}),
		presentation.Footer,
	)
}

func renderPasswordResetBody(resetURL string) string {
	content := renderEmailParagraph("点击下方按钮即可进入密码重置流程。若按钮无法打开，可复制备用链接到浏览器访问。")
	if strings.TrimSpace(resetURL) != "" {
		content += renderEmailButton("立即重置密码", resetURL)
		content += renderEmailLinkBlock("备用链接", resetURL)
	} else {
		content += renderEmailInfoBox("未配置密码重置地址", "当前应用尚未提供密码重置入口地址，请联系管理员补充 resetBaseURL 配置。")
	}
	return content
}

func renderWelcomeLead(userName string) string {
	name := strings.TrimSpace(userName)
	if name == "" {
		return "您的账号已完成初始化，可以开始使用当前应用。"
	}
	return fmt.Sprintf("%s，您好。您的账号已完成初始化，可以开始使用当前应用。", name)
}

func renderWelcomeBody(appName string) string {
	return renderEmailParagraph(fmt.Sprintf("欢迎使用 %s。系统已为您完成基础账号准备工作。", appName)) +
		renderEmailInfoBox("建议后续操作", "首次登录后建议尽快修改初始密码、补充安全信息，并检查通知与隐私设置。")
}

func renderProfileChangeCompletedBody(field string, oldValue string, newValue string) string {
	return renderEmailParagraph("系统已完成本次敏感资料变更，以下为本次变更摘要。") +
		renderEmailDetails([]emailDetail{
			{Label: "变更项目", Value: describeProfileChangeField(field)},
			{Label: "变更前", Value: maskProfileChangeNotificationValue(field, oldValue)},
			{Label: "变更后", Value: maskProfileChangeNotificationValue(field, newValue)},
		}) +
		renderEmailInfoBox("安全提醒", "如果这不是您本人发起的操作，请立即修改密码，并检查账号绑定信息与近期登录记录。")
}

func renderCodeBody(code string, expireMinutes int, details []emailDetail) string {
	return renderEmailDetails(details) +
		fmt.Sprintf(`<div style="margin:24px 0;padding:24px;border:1px solid #e2e2e2;border-radius:10px;background:#fcfcfc;text-align:center;">
<div style="font-size:11px;line-height:16px;letter-spacing:0.08em;text-transform:uppercase;color:#6f6f6f;">Verification Code</div>
<div style="margin-top:12px;font-size:32px;line-height:1;font-weight:700;letter-spacing:0.28em;color:#171717;">%s</div>
</div>`, html.EscapeString(strings.TrimSpace(code))) +
		renderEmailInfoBox("有效期说明", fmt.Sprintf("验证码将在 %d 分钟后失效，请尽快完成操作。", expireMinutes))
}

// emailLayout 一封信的外壳参数。
//
// 拆出来是因为凭证邮件要按收件人的语言出具（含 <html lang> 与「请勿回复」那句话），
// 而平台自身的验证码 / 密码重置邮件仍是中文。同一个外壳、两种语言，
// 用参数表达比复制一份布局函数可靠。
type emailLayout struct {
	// Lang HTML 语言标记，留空为 zh-CN
	Lang       string
	AppName    string
	Eyebrow    string
	Title      string
	Lead       string
	Body       string
	FooterNote string
	// NoReplyNote 页脚的「请勿回复」提示，留空用中文默认句
	NoReplyNote string
}

func renderEmailLayout(appName string, eyebrow string, title string, lead string, bodyHTML string, footerNote string) string {
	return renderEmailLayoutWith(emailLayout{
		AppName: appName, Eyebrow: eyebrow, Title: title,
		Lead: lead, Body: bodyHTML, FooterNote: footerNote,
	})
}

func renderEmailLayoutWith(layout emailLayout) string {
	appName, eyebrow, title := layout.AppName, layout.Eyebrow, layout.Title
	lead, bodyHTML, footerNote := layout.Lead, layout.Body, layout.FooterNote
	lang := strings.TrimSpace(layout.Lang)
	if lang == "" {
		lang = "zh-CN"
	}
	noReply := strings.TrimSpace(layout.NoReplyNote)
	if noReply == "" {
		noReply = "这是一封系统邮件，请勿直接回复。"
	}
	safeNoReply := html.EscapeString(noReply)
	safeAppName := html.EscapeString(strings.TrimSpace(appName))
	safeEyebrow := html.EscapeString(strings.TrimSpace(eyebrow))
	safeTitle := html.EscapeString(strings.TrimSpace(title))
	safeLead := html.EscapeString(strings.TrimSpace(lead))
	safeFooter := html.EscapeString(strings.TrimSpace(footerNote))

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="%s">
<head>
<meta charset="UTF-8" />
<meta name="viewport" content="width=device-width, initial-scale=1.0" />
<title>%s</title>
</head>
<body style="margin:0;padding:0;background:#f8f8f8;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,'PingFang SC','Hiragino Sans GB','Microsoft YaHei',sans-serif;color:#171717;">
<div style="padding:32px 16px;">
<div style="max-width:640px;margin:0 auto;border:1px solid #e2e2e2;border-radius:12px;overflow:hidden;background:#fcfcfc;">
<div style="padding:28px 28px 20px;background:#fcfcfc;border-bottom:1px solid #ededed;">
<div style="display:inline-block;padding:2px 10px;border:1px solid #e2e2e2;border-radius:999px;background:#f8f8f8;font-size:12px;line-height:22px;font-weight:500;color:#6f6f6f;">%s</div>
<h1 style="margin:16px 0 8px;font-size:28px;line-height:1.2;font-weight:700;color:#171717;">%s</h1>
<p style="margin:0;font-size:14px;line-height:24px;color:#6f6f6f;">%s</p>
</div>
<div style="padding:24px 28px 12px;background:#fcfcfc;">%s</div>
<div style="padding:0 28px 28px;background:#fcfcfc;">
<div style="padding:14px 16px;border:1px solid #e2e2e2;border-radius:10px;background:#f8f8f8;color:#6f6f6f;font-size:13px;line-height:22px;">%s</div>
</div>
</div>
<div style="max-width:640px;margin:12px auto 0;padding:0 8px;color:#6f6f6f;font-size:12px;line-height:22px;text-align:center;">
<div style="font-weight:500;color:#6f6f6f;">%s</div>
<div>%s</div>
</div>
</div>
</body>
</html>`, lang, safeTitle, safeEyebrow, safeTitle, safeLead, bodyHTML, safeFooter, safeAppName, safeNoReply)
}

func renderEmailParagraph(text string) string {
	return fmt.Sprintf(`<p style="margin:0 0 16px;font-size:14px;line-height:24px;color:#6f6f6f;">%s</p>`, html.EscapeString(strings.TrimSpace(text)))
}

func renderEmailInfoBox(title string, text string) string {
	return fmt.Sprintf(`<div style="margin:16px 0;padding:16px 18px;border-radius:10px;background:#f8f8f8;border:1px solid #e2e2e2;">
<div style="font-size:13px;line-height:20px;font-weight:600;color:#171717;">%s</div>
<div style="margin-top:6px;font-size:14px;line-height:24px;color:#6f6f6f;">%s</div>
</div>`, html.EscapeString(strings.TrimSpace(title)), html.EscapeString(strings.TrimSpace(text)))
}

func renderEmailButton(label string, url string) string {
	safeLabel := html.EscapeString(strings.TrimSpace(label))
	safeURL := html.EscapeString(strings.TrimSpace(url))
	return fmt.Sprintf(`<div style="margin:24px 0 20px;">
<a href="%s" style="display:inline-block;padding:10px 16px;border:1px solid #171717;border-radius:8px;background:#171717;color:#ffffff;text-decoration:none;font-size:14px;line-height:20px;font-weight:500;">%s</a>
</div>`, safeURL, safeLabel)
}

func renderEmailLinkBlock(label string, url string) string {
	return fmt.Sprintf(`<div style="margin:0 0 16px;padding:16px 18px;border-radius:10px;background:#fcfcfc;border:1px solid #e2e2e2;">
<div style="font-size:13px;line-height:20px;font-weight:600;color:#171717;">%s</div>
<div style="margin-top:8px;word-break:break-all;font-size:13px;line-height:22px;color:#3e63dd;">%s</div>
</div>`, html.EscapeString(strings.TrimSpace(label)), html.EscapeString(strings.TrimSpace(url)))
}

func renderEmailDetails(details []emailDetail) string {
	if len(details) == 0 {
		return ""
	}
	filtered := make([]emailDetail, 0, len(details))
	for _, item := range details {
		if strings.TrimSpace(item.Label) == "" && strings.TrimSpace(item.Value) == "" {
			continue
		}
		filtered = append(filtered, item)
	}
	if len(filtered) == 0 {
		return ""
	}
	var builder strings.Builder
	builder.WriteString(`<div style="margin:0 0 16px;padding:14px 18px;border-radius:10px;background:#fcfcfc;border:1px solid #e2e2e2;">`)
	for idx, item := range filtered {
		borderStyle := ""
		if idx < len(filtered)-1 {
			borderStyle = "border-bottom:1px solid #ededed;"
		}
		builder.WriteString(fmt.Sprintf(`<div style="display:flex;justify-content:space-between;gap:16px;padding:10px 0;%s">
<div style="font-size:13px;line-height:20px;color:#6f6f6f;">%s</div>
<div style="font-size:13px;line-height:20px;font-weight:500;color:#171717;text-align:right;">%s</div>
</div>`, borderStyle, html.EscapeString(strings.TrimSpace(item.Label)), html.EscapeString(strings.TrimSpace(item.Value))))
	}
	builder.WriteString(`</div>`)
	return builder.String()
}

func normalizeEmailPurpose(purpose string) string {
	return strings.ToLower(strings.TrimSpace(purpose))
}

func describeEmailPurpose(purpose string) string {
	return getEmailPurposePresentation(purpose).DisplayName
}

func getEmailPurposePresentation(purpose string) emailPurposePresentation {
	switch normalizeEmailPurpose(purpose) {
	case "register", "signup", "sign-up":
		return emailPurposePresentation{
			Eyebrow:     "账号安全",
			Title:       "注册验证码",
			DisplayName: "账号注册",
			Lead:        "本次验证码用于新账号注册，请在有效期内完成验证。",
			Footer:      "请勿将验证码泄露给任何人。系统工作人员不会向您索取验证码。",
		}
	case "login", "signin", "sign-in":
		return emailPurposePresentation{
			Eyebrow:     "身份校验",
			Title:       "登录验证码",
			DisplayName: "登录验证",
			Lead:        "本次验证码用于登录校验，请确认是您本人正在进行登录操作。",
			Footer:      "如非本人操作，请立即修改密码并检查账号安全设置。",
		}
	case "admin_login":
		return emailPurposePresentation{
			Eyebrow:     "后台安全",
			Title:       "管理员登录验证码",
			DisplayName: "管理员登录",
			Lead:        "本次验证码用于管理员登录校验，请仅在您本人操作时使用。",
			Footer:      "后台验证码具有较高安全等级，请勿转发或截图外传。",
		}
	case "bind_email", "bind-email":
		return emailPurposePresentation{
			Eyebrow:     "资料验证",
			Title:       "绑定邮箱验证码",
			DisplayName: "绑定邮箱",
			Lead:        "本次验证码用于绑定邮箱，请在有效期内完成验证。",
			Footer:      "完成验证后，该邮箱将绑定到您的账号。",
		}
	case "change_email", "change-email":
		return emailPurposePresentation{
			Eyebrow:     "资料验证",
			Title:       "邮箱变更验证码",
			DisplayName: "变更邮箱",
			Lead:        "本次验证码用于修改邮箱，请在有效期内完成验证。",
			Footer:      "若本次变更不是您本人发起，请忽略本邮件并及时检查账号安全。",
		}
	case "profile_email_change":
		return emailPurposePresentation{
			Eyebrow:     "资料验证",
			Title:       "邮箱变更验证码",
			DisplayName: "个人资料邮箱变更",
			Lead:        "本次验证码用于确认新的个人资料邮箱地址，请在有效期内完成验证。",
			Footer:      "完成验证后，新的邮箱地址将更新到您的个人资料。",
		}
	case "profile_phone_change":
		return emailPurposePresentation{
			Eyebrow:     "资料验证",
			Title:       "手机号变更验证码",
			DisplayName: "个人资料手机号变更",
			Lead:        "本次验证码用于确认新的个人资料手机号，请在有效期内完成验证。",
			Footer:      "完成验证后，新的手机号将更新到您的个人资料。",
		}
	case "bind_phone", "bind-phone":
		return emailPurposePresentation{
			Eyebrow:     "资料验证",
			Title:       "绑定手机验证码",
			DisplayName: "绑定手机号",
			Lead:        "本次验证码用于绑定手机号，请在有效期内完成验证。",
			Footer:      "完成验证后，该手机号将绑定到您的账号。",
		}
	case "change_phone", "change-phone":
		return emailPurposePresentation{
			Eyebrow:     "资料验证",
			Title:       "手机号变更验证码",
			DisplayName: "变更手机号",
			Lead:        "本次验证码用于修改手机号，请在有效期内完成验证。",
			Footer:      "若本次变更不是您本人发起，请及时检查账号安全设置。",
		}
	case "password_reset", "reset_password", "reset-password":
		return emailPurposePresentation{
			Eyebrow:     "账号安全",
			Title:       "密码重置验证码",
			DisplayName: "重置密码",
			Lead:        "本次验证码用于密码重置，请在有效期内完成验证。",
			Footer:      "若本次操作并非您本人发起，请忽略本邮件并尽快检查账号安全。",
		}
	case "verify_identity", "identity_verify", "identity-verification":
		return emailPurposePresentation{
			Eyebrow:     "身份校验",
			Title:       "身份验证验证码",
			DisplayName: "身份验证",
			Lead:        "本次验证码用于身份核验，请在有效期内完成验证。",
			Footer:      "请勿向任何人透露验证码，系统人员不会以任何理由索要该验证码。",
		}
	case "two_factor", "two-factor", "2fa", "mfa":
		return emailPurposePresentation{
			Eyebrow:     "二次验证",
			Title:       "二次验证验证码",
			DisplayName: "双重验证",
			Lead:        "本次验证码用于双重验证，请在有效期内完成确认。",
			Footer:      "如您未开启或未触发本次验证，请立即检查账号安全。",
		}
	case "custom":
		return emailPurposePresentation{
			Eyebrow:     "身份校验",
			Title:       "验证码",
			DisplayName: "自定义验证",
			Lead:        "本次验证码用于自定义安全校验，请在有效期内完成验证。",
			Footer:      "请勿将验证码泄露给任何人。系统工作人员不会向您索取验证码。",
		}
	case "test":
		return emailPurposePresentation{
			Eyebrow:     "通道测试",
			Title:       "邮件服务连通性验证",
			DisplayName: "邮件配置测试",
			Lead:        "这是一封由后台主动触发的测试邮件，用于验证当前邮件配置是否可正常投递。",
			Footer:      "如果您已收到此邮件，说明当前 SMTP 配置、认证和模板渲染链路均已生效。",
		}
	default:
		return emailPurposePresentation{
			Eyebrow:     "身份校验",
			Title:       "验证码",
			DisplayName: "安全验证",
			Lead:        "本次验证码用于安全校验，请在有效期内完成验证。",
			Footer:      "请勿将验证码泄露给任何人。系统工作人员不会向您索取验证码。",
		}
	}
}

func fallbackEmailValue(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func describeProfileChangeField(field string) string {
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "email":
		return "邮箱地址"
	case "phone":
		return "手机号码"
	default:
		return "资料信息"
	}
}

func maskProfileChangeNotificationValue(field string, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "未设置"
	}
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "email":
		return maskEmailForNotification(value)
	case "phone":
		return maskPhoneForNotification(value)
	default:
		return value
	}
}

func maskEmailForNotification(value string) string {
	parts := strings.Split(value, "@")
	if len(parts) != 2 {
		return value
	}
	local := parts[0]
	if len(local) <= 1 {
		return "*@" + parts[1]
	}
	if len(local) == 2 {
		return local[:1] + "*@" + parts[1]
	}
	return local[:1] + strings.Repeat("*", len(local)-2) + local[len(local)-1:] + "@" + parts[1]
}

func maskPhoneForNotification(value string) string {
	if len(value) <= 7 {
		return "***"
	}
	return value[:3] + "****" + value[len(value)-4:]
}
