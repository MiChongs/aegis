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

// passwordResetTTLMinutes 重置链接的有效期。
// 邮件正文里那句「链接 N 分钟后失效」由它渲染，改这里两边一起动 ——
// 分开写迟早会出现「信里说 30 分钟、实际 10 分钟就打不开了」。
const passwordResetTTLMinutes = 30

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
	expireAt := timeutil.Now().Add(timeutil.Minutes(passwordResetTTLMinutes))
	resetURL := strings.TrimRight(strings.TrimSpace(resetBaseURL), "/")
	if resetURL != "" {
		resetURL += "?token=" + token + "&email=" + email
	}
	subject := fmt.Sprintf("重置 %s 的登录密码", app.Name)
	html, text := renderEmail(emailLayout{
		AppName:    app.Name,
		Title:      "重置密码",
		Lead:       "我们收到了一次重置这个账号密码的请求。",
		Preheader:  fmt.Sprintf("重置密码的链接 %d 分钟内有效", passwordResetTTLMinutes),
		Blocks:     passwordResetBlocks(resetURL, passwordResetTTLMinutes),
		FooterNote: "不是您本人申请的话，忽略这封信即可，密码不会有任何变化。",
	})
	messageID, err := s.sendRenderedMail(ctx, config, email, subject, mailBody{HTML: html, Text: text}, "password_reset")
	if err != nil {
		return nil, err
	}
	if err := s.redis.Set(ctx, s.resetTokenKey(appID, email), token, timeutil.Minutes(passwordResetTTLMinutes)).Err(); err != nil {
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
	subject := fmt.Sprintf("%s 账号已开通", app.Name)
	html, text := renderEmail(emailLayout{
		AppName: app.Name,
		Title:   "账号已开通",
		Lead:    welcomeLead(app.Name, userName),
		Blocks: []mailBlock{
			mailDetails(emailDetail{Label: "登录邮箱", Value: email}),
		},
		FooterNote: fmt.Sprintf("如果您没有注册过 %s，忽略这封信即可。", app.Name),
	})
	_, err = s.sendRenderedMail(ctx, config, email, subject, mailBody{HTML: html, Text: text}, "welcome")
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
	fieldName := describeProfileChangeField(field)
	subject := fmt.Sprintf("%s 的%s已变更", app.Name, fieldName)
	html, text := renderEmail(emailLayout{
		AppName:   app.Name,
		Title:     fieldName + "已变更",
		Lead:      "变更已经生效，下面是这次的改动。",
		Preheader: fmt.Sprintf("%s 的%s刚刚被修改", app.Name, fieldName),
		Blocks: []mailBlock{
			mailDetails(
				emailDetail{Label: "变更前", Value: maskProfileChangeNotificationValue(field, oldValue)},
				emailDetail{Label: "变更后", Value: maskProfileChangeNotificationValue(field, newValue)},
			),
		},
		FooterNote: "不是您本人改的话，请立刻修改密码，并检查账号的绑定信息和最近登录记录。",
	})
	_, err = s.sendRenderedMail(ctx, config, email, subject, mailBody{HTML: html, Text: text}, "profile_change")
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
	subject, html, text := renderCodeMailContent(app.Name, config.Name, purpose, code, expireMinutes)
	messageID, err := s.sendRenderedMail(ctx, config, email, subject, mailBody{HTML: html, Text: text}, purpose)
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

// mailBody 一封渲染完成的信：HTML 与配套纯文本成对交出。
//
// 模板渲染的信（验证码 / 重置 / 欢迎 / 资料变更 / 凭证）由 layout.gotxt
// 从同一份内容模型生成纯文本；只有「正文是外部给的一段 HTML」那条路径
// 才回落到 htmlToPlainText 去抓。抓取版永远比照着内容写的差一档。
type mailBody struct {
	HTML string
	Text string
}

// sendMail 正文由外部给 HTML 时的入口，纯文本靠抓取补齐。
func (s *EmailService) sendMail(ctx context.Context, config *emaildomain.Config, to string, subject string, htmlBody string, purpose string, attachments ...emailAttachment) (string, error) {
	return s.sendRenderedMail(ctx, config, to, subject,
		mailBody{HTML: htmlBody, Text: htmlToPlainText(htmlBody)}, purpose, attachments...)
}

func (s *EmailService) sendRenderedMail(ctx context.Context, config *emaildomain.Config, to string, subject string, body mailBody, purpose string, attachments ...emailAttachment) (string, error) {
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
		HTML:        body.HTML,
		Text:        body.Text,
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
