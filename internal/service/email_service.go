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
	// masterKey 用于加解密渠道密钥（API Key / webhook 签名密钥 / SMTP 密码），
	// 派生自 SECURITY_MASTER_KEY，与 OAuth / NotifyHub 同构。
	masterKey []byte
	senders   map[string]emailSender
	// governance 平台治理判定：被治理应用不得再对外发信
	governance *PlatformGovernanceService
	// settings 平台设置，只用来取平台品牌名（平台级邮件的「应用名」）。
	// 可选注入：为 nil 时回落到 defaultPlatformName，不影响发信。
	settings *PlatformSettingsService
}

// defaultPlatformName 平台级邮件在品牌名缺省时的落款。
// 邮件模板里那个位置是必填的（标题、页脚都用它），空着会渲染出
// 「 账号已开通」这种前面缺一个词的句子。
const defaultPlatformName = "Aegis"

// SetGovernanceService 注入平台治理服务（bootstrap 中调用）。
func (s *EmailService) SetGovernanceService(g *PlatformGovernanceService) { s.governance = g }

// SetPlatformSettings 注入平台设置，用于取平台品牌名。
func (s *EmailService) SetPlatformSettings(p *PlatformSettingsService) { s.settings = p }

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
		emaildomain.ProviderSMTP:     newSMTPEmailSender(log),
		emaildomain.ProviderZeabur:   newZeaburEmailSender(log),
		emaildomain.ProviderSES:      newSESEmailSender(log),
		emaildomain.ProviderResend:   newResendEmailSender(log),
		emaildomain.ProviderSendGrid: newSendGridEmailSender(log),
		emaildomain.ProviderMailgun:  newMailgunEmailSender(log),
		emaildomain.ProviderPostmark: newPostmarkEmailSender(log),
		emaildomain.ProviderAliyun:   newAliyunEmailSender(log),
		emaildomain.ProviderTencent:  newTencentEmailSender(log),
	}
	return service
}

// ── 配置管理面 ──

func (s *EmailService) ListConfigs(ctx context.Context, appID int64) ([]emaildomain.Config, error) {
	if err := s.ensureScope(ctx, appID); err != nil {
		return nil, err
	}
	items, err := s.pg.ListEmailConfigs(ctx, appID)
	if err != nil {
		return nil, err
	}
	for i := range items {
		s.sanitizeConfig(&items[i])
	}
	return items, nil
}

func (s *EmailService) Detail(ctx context.Context, appID int64, id int64) (*emaildomain.Config, error) {
	item, err := s.loadConfig(ctx, appID, id)
	if err != nil {
		return nil, err
	}
	sanitized := item.Clone()
	s.sanitizeConfig(&sanitized)
	return &sanitized, nil
}

// loadConfig 取出配置**并解密渠道密钥**，仅供内部发送链路使用。
// 对外出口一律走 Detail / ListConfigs，那两条路径会抹掉所有密钥。
func (s *EmailService) loadConfig(ctx context.Context, appID int64, id int64) (*emaildomain.Config, error) {
	if err := s.ensureScope(ctx, appID); err != nil {
		return nil, err
	}
	item, err := s.pg.GetEmailConfigByID(ctx, appID, id)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, apperrors.New(40460, http.StatusNotFound, "邮件配置不存在")
	}
	s.decryptSecrets(item)
	return item, nil
}

// sanitizeConfig 抹掉一切密钥，只保留「是否已配置」的布尔位。
//
// 依据是服务商目录里 Secret 为 true 的字段，而不是一串写死的字段名 ——
// 新增一个密钥字段时忘了在这里补一行，表现是密钥经管理接口回流到浏览器，
// 而那种泄露不会有任何报错。
func (s *EmailService) sanitizeConfig(item *emaildomain.Config) {
	if item == nil {
		return
	}
	set := make(map[string]bool, 4)
	for _, key := range s.providerMeta(item.Provider).SecretKeys() {
		set[key] = item.HasSecret(key)
	}
	// 目录之外的残留密文也要如实上报「已配置」：切换过服务商的配置里
	// 会留着上一家的密钥，界面上不说的话，切回去时用户会以为要重填。
	for key, cipherText := range item.SecretsCipher {
		if _, known := set[key]; !known && strings.TrimSpace(cipherText) != "" {
			set[key] = true
		}
	}
	item.SecretSet = set
	item.Secrets = nil
	item.SecretsCipher = nil
}

// decryptSecrets 把落库密文还原成可用的明文密钥。
//
// 解密失败不阻断流程：让发送器给出「API Key 未配置或解密失败」这种可操作的报错，
// 比在这里抛一个通用错误更容易定位（典型诱因是换了 SECURITY_MASTER_KEY）。
func (s *EmailService) decryptSecrets(item *emaildomain.Config) {
	if item == nil {
		return
	}
	if item.Secrets == nil {
		item.Secrets = map[string]string{}
	}
	for key, cipherText := range item.SecretsCipher {
		if strings.TrimSpace(cipherText) == "" {
			continue
		}
		plain, err := decryptSecret(s.masterKey, cipherText)
		if err != nil {
			s.log.Error("decrypt email secret failed",
				zap.Int64("appid", item.AppID), zap.String("config", item.Name),
				zap.String("field", key), zap.Error(err))
			continue
		}
		item.Secrets[key] = plain
	}
}

func (s *EmailService) Save(ctx context.Context, mutation emaildomain.ConfigMutation) (*emaildomain.Config, error) {
	if err := s.ensureScope(ctx, mutation.AppID); err != nil {
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
		Settings:  map[string]string{},
	}
	if current != nil {
		s.decryptSecrets(current)
		item = current.Clone()
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
	if mutation.Shared != nil {
		item.Shared = *mutation.Shared
	}
	// 共享开关只对平台级配置有意义；应用级配置带上它一律当没写。
	// 静默忽略而不是报错：控制台的表单是同一份，应用级面板根本不显示这一项，
	// 而某些客户端会把整份表单原样回传。
	if !item.IsPlatform() {
		item.Shared = false
	}
	if item.Settings == nil {
		item.Settings = map[string]string{}
	}
	if mutation.ReplaceSettings {
		item.Settings = map[string]string{}
	}
	for key, value := range mutation.Settings {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		item.Settings[key] = value
	}

	if strings.TrimSpace(item.Name) == "" {
		return nil, apperrors.New(40060, http.StatusBadRequest, "配置名称不能为空")
	}
	item.Provider = normalizeEmailProvider(item.Provider)

	sender, err := s.resolveSender(item.Provider)
	if err != nil {
		return nil, err
	}
	if err := s.mergeSecrets(&item, mutation); err != nil {
		return nil, err
	}
	if err := sender.Validate(item); err != nil {
		return nil, err
	}

	saved, err := s.pg.UpsertEmailConfig(ctx, item)
	if err != nil {
		return nil, err
	}
	s.sanitizeConfig(saved)
	return saved, nil
}

// mergeSecrets 把提交上来的明文密钥与既有密文合并，产出落库用的 SecretsCipher。
//
// 三条约定，缺任何一条都会造成一次静默的凭据丢失：
//   - 提交的明文非空 → 重新加密（改密钥）
//   - 提交的明文为空 → 沿用既有密文（留空即不修改；前端编辑态从不回显密钥，
//     无条件覆盖会让「改个发件人名」把 API Key 清空，而这件事要到下一次发信才暴露）
//   - 只有明文没有密文 → 现在加密（自愈：存量 SMTP 密码是明文落库的）
//
// 显式清空走 ClearSecrets，不与「留空」共用一个表达方式。
func (s *EmailService) mergeSecrets(item *emaildomain.Config, mutation emaildomain.ConfigMutation) error {
	merged := map[string]string{}
	for key, cipherText := range item.SecretsCipher {
		if strings.TrimSpace(cipherText) != "" {
			merged[key] = cipherText
		}
	}
	// 存量明文（旧版 SMTP 密码）在这里补上密文，下一次保存即完成迁移。
	for key, plain := range item.Secrets {
		if strings.TrimSpace(plain) == "" {
			continue
		}
		if _, exists := merged[key]; exists {
			continue
		}
		cipherText, err := encryptSecret(s.masterKey, plain)
		if err != nil {
			return apperrors.New(50060, http.StatusInternalServerError, "加密邮件密钥失败："+key)
		}
		merged[key] = cipherText
	}
	for key, plain := range mutation.Secrets {
		key = strings.TrimSpace(key)
		if key == "" || strings.TrimSpace(plain) == "" {
			continue
		}
		cipherText, err := encryptSecret(s.masterKey, plain)
		if err != nil {
			return apperrors.New(50060, http.StatusInternalServerError, "加密邮件密钥失败："+key)
		}
		merged[key] = cipherText
		if item.Secrets == nil {
			item.Secrets = map[string]string{}
		}
		item.Secrets[key] = plain
	}
	for _, key := range mutation.ClearSecrets {
		key = strings.TrimSpace(key)
		delete(merged, key)
		delete(item.Secrets, key)
	}
	item.SecretsCipher = merged
	return nil
}

func (s *EmailService) Delete(ctx context.Context, appID int64, id int64) error {
	if err := s.ensureScope(ctx, appID); err != nil {
		return err
	}
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

// DeliveryStats 某个作用域的投递概况。
func (s *EmailService) DeliveryStats(ctx context.Context, appID int64) (*emaildomain.DeliveryStats, error) {
	if err := s.ensureScope(ctx, appID); err != nil {
		return nil, err
	}
	return s.pg.EmailDeliveryStats(ctx, appID)
}

// ── 业务发信入口 ──

func (s *EmailService) SendVerificationCode(ctx context.Context, appID int64, email string, purpose string, expireMinutes int, configName string) (*emaildomain.VerificationResult, error) {
	return s.SendCode(ctx, CodeRequest{AppID: appID, Email: email, Purpose: purpose, ExpireMinutes: expireMinutes, ConfigName: configName})
}

func (s *EmailService) VerifyCode(ctx context.Context, appID int64, email string, code string, purpose string) (bool, error) {
	return s.verifyStoredSecret(ctx, appID, s.emailCodeKey(appID, purpose, email), emailCodeDigestInput(appID, purpose, email, code))
}

// passwordResetTTLMinutes 重置链接的有效期。
// 邮件正文里那句「链接 N 分钟后失效」由它渲染，改这里两边一起动 ——
// 分开写迟早会出现「信里说 30 分钟、实际 10 分钟就打不开了」。
const passwordResetTTLMinutes = 30

func (s *EmailService) SendPasswordResetEmail(ctx context.Context, appID int64, email string, resetBaseURL string, configName string) (*emaildomain.ResetResult, error) {
	return s.SendPasswordResetLink(ctx, ResetLinkRequest{AppID: appID, Email: email, ResetURL: resetBaseURL, ConfigName: configName})
}

// RecoveryMailReady 这个应用能不能发信（有可用的邮件配置）。找回密码据此决定邮箱算不算一项可用的验证方式。
func (s *EmailService) RecoveryMailReady(ctx context.Context, appID int64) bool {
	_, err := s.resolveConfig(ctx, appID, "")
	return err == nil
}

// SendRecoveryCode 找回密码流程里发往绑定邮箱的验证码。
//
// 码由找回流程生成并只保存摘要，这里只负责投递：不写 emailCodeKey ——
// 那个 key 能被公开的 /auth/email/verify 逐个试探，找回用的码不能放在那里。
func (s *EmailService) SendRecoveryCode(ctx context.Context, appID int64, email string, code string, expireMinutes int) error {
	config, err := s.resolveConfig(ctx, appID, "")
	if err != nil {
		return err
	}
	_, _, err = s.sendCodeMail(ctx, appID, config, email, code, "reset_password", expireMinutes)
	return err
}

// PasswordResetNotice 密码被重置之后告知信里的事实。
type PasswordResetNotice struct {
	Account   string
	At        time.Time
	IP        string
	Location  string
	Device    string
	SignedOut int
}

// SendPasswordResetNotice 密码重置完成后寄往绑定邮箱的告知信。
//
// 这封信是唯一一个「不是本人操作」时还能被本人看到的信号：找回成功意味着
// 对方已经拿到了验证码，此时能做的就是让真正的主人尽快知道、尽快再找回一次。
func (s *EmailService) SendPasswordResetNotice(ctx context.Context, appID int64, email string, notice PasswordResetNotice) error {
	config, err := s.resolveConfig(ctx, appID, "")
	if err != nil {
		return err
	}
	policy, brand, err := s.mailContext(ctx, appID)
	if err != nil {
		return err
	}
	at := notice.At
	if at.IsZero() {
		at = timeutil.Now()
	}
	details := []emailDetail{
		{Label: "账号", Value: notice.Account},
		{Label: "时间", Value: at.In(timeutil.DefaultLocation()).Format("2006-01-02 15:04:05")},
		{Label: "设备", Value: notice.Device},
		{Label: "IP 地址", Value: notice.IP},
		{Label: "位置", Value: notice.Location},
	}
	leadDefault := ""
	if notice.SignedOut > 0 {
		leadDefault = fmt.Sprintf("账号的登录密码已通过找回流程重新设置，%d 个登录中的设备已退出。", notice.SignedOut)
	}
	subject, html, text := composeScene(policy, brand, sceneMail{
		Scene:       emaildomain.ScenePasswordResetNotice,
		Vars:        map[string]string{"account": notice.Account},
		Preheader:   fmt.Sprintf("%s 的登录密码刚刚被重置", brand.Name),
		Blocks:      []mailBlock{mailDetails(details...)},
		LeadDefault: leadDefault,
	})
	_, err = s.sendRenderedMail(ctx, appID, config, email, subject, mailBody{HTML: html, Text: text}, "password_reset_notice")
	return err
}

func (s *EmailService) VerifyResetToken(ctx context.Context, appID int64, email string, token string) (bool, error) {
	return s.verifyStoredSecret(ctx, appID, s.resetTokenKey(appID, email), resetTokenDigestInput(appID, email, token))
}

func (s *EmailService) SendWelcomeEmail(ctx context.Context, appID int64, email string, userName string, configName string) error {
	policy, brand, err := s.mailContext(ctx, appID)
	if err != nil {
		return err
	}
	if !policy.SceneEnabled(emaildomain.SceneWelcome) {
		return nil
	}
	config, err := s.resolveConfig(ctx, appID, configName)
	if err != nil {
		return err
	}
	subject, html, text := composeScene(policy, brand, sceneMail{
		Scene:       emaildomain.SceneWelcome,
		Vars:        map[string]string{"name": strings.TrimSpace(userName), "email": strings.TrimSpace(email)},
		Blocks:      []mailBlock{mailDetails(emailDetail{Label: "登录邮箱", Value: email})},
		LeadDefault: welcomeLead(brand.Name, userName),
	})
	_, err = s.sendRenderedMail(ctx, appID, config, email, subject, mailBody{HTML: html, Text: text}, "welcome")
	return err
}

func (s *EmailService) SendProfileChangeCompletedEmail(ctx context.Context, appID int64, email string, field string, oldValue string, newValue string, configName string) error {
	policy, brand, err := s.mailContext(ctx, appID)
	if err != nil {
		return err
	}
	if !policy.SceneEnabled(emaildomain.SceneProfileChange) {
		return nil
	}
	config, err := s.resolveConfig(ctx, appID, configName)
	if err != nil {
		return err
	}
	fieldName := describeProfileChangeField(field)
	subject, html, text := composeScene(policy, brand, sceneMail{
		Scene:     emaildomain.SceneProfileChange,
		Vars:      map[string]string{"field": fieldName},
		Preheader: fmt.Sprintf("%s 的%s刚刚被修改", brand.Name, fieldName),
		Blocks: []mailBlock{
			mailDetails(
				emailDetail{Label: "变更前", Value: maskProfileChangeNotificationValue(field, oldValue)},
				emailDetail{Label: "变更后", Value: maskProfileChangeNotificationValue(field, newValue)},
			),
		},
	})
	_, err = s.sendRenderedMail(ctx, appID, config, email, subject, mailBody{HTML: html, Text: text}, "profile_change")
	return err
}

// SendNotificationEmail 通用通知邮件出口，供 NotifyHub 的 email 渠道调用。
// 与业务邮件（验证码/欢迎信）区分：正文由调用方渲染好，这里只负责挑配置并投递。
//
// appID 传 0 即平台级：管理员通知、平台告警走的就是这条路径。
// 重构前这里会在「查 appid=0 这个应用」那一步失败，于是平台级通知渠道
// 配得起来、发不出去，而错误信息说的是「无法找到该应用」。
func (s *EmailService) SendNotificationEmail(ctx context.Context, appID int64, to string, subject string, htmlBody string, configName string) error {
	config, err := s.resolveConfig(ctx, appID, configName)
	if err != nil {
		return err
	}
	subject = strings.TrimSpace(subject)
	if subject == "" {
		subject = "Aegis 通知"
	}
	_, err = s.sendMail(ctx, appID, config, to, subject, htmlBody, "notification")
	return err
}

// ── 作用域与通道解析 ──

// ensureScope 校验作用域存在。平台级（appid=0）恒成立，不打库。
func (s *EmailService) ensureScope(ctx context.Context, appID int64) error {
	if appID == emaildomain.PlatformAppID {
		return nil
	}
	app, err := s.pg.GetAppByID(ctx, appID)
	if err != nil {
		return err
	}
	if app == nil {
		return apperrors.New(40410, http.StatusNotFound, "无法找到该应用")
	}
	return nil
}

// scopeName 邮件正文里的落款主体：应用级用应用名，平台级用平台品牌名。
func (s *EmailService) scopeName(ctx context.Context, appID int64) (string, error) {
	if appID == emaildomain.PlatformAppID {
		if s.settings != nil {
			if name := strings.TrimSpace(s.settings.BrandingPlatformName(ctx)); name != "" {
				return name, nil
			}
		}
		return defaultPlatformName, nil
	}
	app, err := s.pg.GetAppByID(ctx, appID)
	if err != nil {
		return "", err
	}
	if app == nil {
		return "", apperrors.New(40410, http.StatusNotFound, "无法找到该应用")
	}
	return app.Name, nil
}

// resolveConfig 决定这次发信走哪条通道。
//
// 优先级：本作用域指名的配置 → 本作用域的默认配置 → 平台级**已共享**的默认通道。
// 最后一档只对应用级请求生效，且要求平台管理员显式打开共享开关 ——
// 默认关是刻意的：打开它意味着该应用的信会用平台的发件人身份发出去，
// 而应用管理员既没参与这个决定、也看不出信是从哪条通道走的。
// 因此控制台会明确显示「当前继承自平台通道 X」（见 ResolveChannel）。
func (s *EmailService) resolveConfig(ctx context.Context, appID int64, configName string) (*emaildomain.Config, error) {
	if err := s.ensureScope(ctx, appID); err != nil {
		return nil, err
	}
	var (
		item *emaildomain.Config
		err  error
	)
	if name := strings.TrimSpace(configName); name != "" {
		item, err = s.pg.GetEmailConfigByName(ctx, appID, name)
		if err != nil {
			return nil, err
		}
		// 指名了配置就必须是那一条：静默回落到别的通道会让
		// 「这个业务用专用发件人」这类要求悄悄失效。
		if item == nil {
			return nil, apperrors.New(40461, http.StatusNotFound, "未找到名为 "+name+" 的邮件配置")
		}
		if !item.Enabled {
			return nil, apperrors.New(40061, http.StatusBadRequest, "邮件配置未启用")
		}
		s.decryptSecrets(item)
		return item, nil
	}

	item, err = s.pg.GetDefaultEmailConfig(ctx, appID)
	if err != nil {
		return nil, err
	}
	if item != nil {
		s.decryptSecrets(item)
		return item, nil
	}
	if appID != emaildomain.PlatformAppID {
		shared, err := s.pg.GetSharedPlatformEmailConfig(ctx)
		if err != nil {
			return nil, err
		}
		if shared != nil {
			s.decryptSecrets(shared)
			return shared, nil
		}
		return nil, apperrors.New(40461, http.StatusNotFound,
			"该应用未配置可用邮件服务；平台级通道也没有开启共享，请在应用的「邮件」区块新建一条配置")
	}
	return nil, apperrors.New(40461, http.StatusNotFound,
		"平台级邮件通道尚未配置，请在「配置 → 邮件」中新建一条")
}

// ResolveChannel 回答「这个作用域现在实际用哪条通道发信、能不能带附件、是不是继承来的」。
//
// 控制台靠 Inherited 说清「本应用没配、当前借用平台通道」这件事：
// 不说的话，管理员会对着一个空的邮件配置页纳闷验证码是怎么发出去的。
func (s *EmailService) ResolveChannel(ctx context.Context, appID int64, configName string) (*emaildomain.Resolution, error) {
	config, err := s.resolveConfig(ctx, appID, configName)
	if err != nil {
		return nil, err
	}
	sender, err := s.resolveSender(config.Provider)
	if err != nil {
		return nil, err
	}
	scope := emaildomain.ScopeApp
	if config.IsPlatform() {
		scope = emaildomain.ScopePlatform
	}
	return &emaildomain.Resolution{
		ConfigID:    config.ID,
		ConfigName:  config.Name,
		Provider:    sender.Provider(),
		Scope:       scope,
		Inherited:   config.AppID != appID,
		Attachments: sender.SupportsAttachments(),
	}, nil
}

// ChannelCapability 一条邮件通道的能力自述。
type ChannelCapability struct {
	Provider    string `json:"provider"`
	Attachments bool   `json:"attachments"`
}

// ResolveChannelCapability 查某个作用域当前生效的邮件通道能带不带附件。
//
// 调用方需要**在渲染正文之前**知道这件事：能带附件的写「收据见附件」，
// 不能的写「点下面的按钮下载」。先发了再看结果就来不及改措辞了。
func (s *EmailService) ResolveChannelCapability(ctx context.Context, appID int64, configName string) (ChannelCapability, error) {
	resolution, err := s.ResolveChannel(ctx, appID, configName)
	if err != nil {
		return ChannelCapability{}, err
	}
	return ChannelCapability{Provider: resolution.Provider, Attachments: resolution.Attachments}, nil
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
	return s.sendMail(ctx, appID, config, mail.To, mail.Subject, mail.HTML, purpose, files...)
}

func (s *EmailService) sendCodeMail(ctx context.Context, appID int64, config *emaildomain.Config, email string, code string, purpose string, expireMinutes int) (time.Time, string, error) {
	policy, brand, err := s.mailContext(ctx, appID)
	if err != nil {
		return time.Time{}, "", err
	}
	if expireMinutes <= 0 {
		expireMinutes = policy.Code.TTLMinutes
	}
	expireAt := timeutil.Now().Add(timeutil.Minutes(expireMinutes))
	subject, html, text := renderCodeMailWithPolicy(policy, brand, config.Name, purpose, code, expireMinutes, email)
	messageID, err := s.sendRenderedMail(ctx, appID, config, email, subject, mailBody{HTML: html, Text: text}, purpose)
	return expireAt, messageID, err
}

// mailContext 一次取齐渲染要用的策略与落款。
func (s *EmailService) mailContext(ctx context.Context, appID int64) (emaildomain.Policy, mailBrand, error) {
	scopeName, err := s.scopeName(ctx, appID)
	if err != nil {
		return emaildomain.Policy{}, mailBrand{}, err
	}
	policy := s.policyFor(ctx, appID)
	return policy, brandFromPolicy(policy, scopeName), nil
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
func (s *EmailService) sendMail(ctx context.Context, appID int64, config *emaildomain.Config, to string, subject string, htmlBody string, purpose string, attachments ...emailAttachment) (string, error) {
	return s.sendRenderedMail(ctx, appID, config, to, subject,
		mailBody{HTML: htmlBody, Text: htmlToPlainText(htmlBody)}, purpose, attachments...)
}

// sendRenderedMail 是全平台唯一的邮件出口：抑制名单 → 挑发送器 → 交付 → 留痕。
// 模板已在调用方渲染完毕，这里不关心内容，只关心「用哪条通道发、发没发出去、发到哪儿了」。
//
// appID 是**发信的作用域**，不一定等于 config.AppID：应用借用平台共享通道时两者不同。
// 抑制名单与投递留痕都按发信的作用域算 —— 否则应用的投递记录里看不到自己借道发出的信，
// 应用的抑制名单也拦不住经共享通道发出的那一封。
func (s *EmailService) sendRenderedMail(ctx context.Context, appID int64, config *emaildomain.Config, to string, subject string, body mailBody, purpose string, attachments ...emailAttachment) (string, error) {
	to = strings.TrimSpace(to)
	if _, err := mail.ParseAddress(to); err != nil {
		return "", apperrors.New(40062, http.StatusBadRequest, "邮箱地址格式错误")
	}
	// 通道自检是管理员对着一个明确的地址手动发的，不受抑制名单约束
	if normalizeEmailPurpose(purpose) != "test" {
		if err := s.ensureNotSuppressed(ctx, appID, to); err != nil {
			return "", err
		}
	}
	// 平台治理挂在这里而不是各调用点：这里是全平台唯一的邮件出口，
	// 验证码 / 密码重置 / 欢迎信 / NotifyHub 的 email 渠道全部经过这一处。
	// 平台级配置（appid=0）不参与应用治理判定 —— 它不属于任何被治理的应用，
	// 且平台告警恰恰在治理动作发生时最需要发得出去。
	if s.governance != nil && config != nil && !config.IsPlatform() {
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
			"当前邮件通道（"+sender.Provider()+"）不支持附件，请改用支持附件的服务商（SMTP / AWS SES / Resend / SendGrid / Mailgun / Postmark / 腾讯云），或改为发送下载链接")
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
	s.recordDelivery(ctx, appID, config, out, result, sendErr)
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
func (s *EmailService) recordDelivery(ctx context.Context, appID int64, config *emaildomain.Config, out emailOutbound, result emailSendResult, sendErr error) {
	fromAddress, _, _ := config.SenderIdentity()
	record := emaildomain.Delivery{
		AppID:             appID,
		Inherited:         config.AppID != appID,
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
	if err := s.ensureScope(ctx, query.AppID); err != nil {
		return nil, err
	}
	return s.pg.ListEmailDeliveries(ctx, query)
}

func (s *EmailService) emailCodeKey(appID int64, purpose string, email string) string {
	return fmt.Sprintf("%s:email:code:%d:%s:%s", s.keyPrefix, appID, normalizeEmailPurpose(purpose), strings.ToLower(strings.TrimSpace(email)))
}

func (s *EmailService) resetTokenKey(appID int64, email string) string {
	return fmt.Sprintf("%s:email:reset:%d:%s", s.keyPrefix, appID, strings.ToLower(strings.TrimSpace(email)))
}

// appNameHolder 是几个服务共用的「只要应用名」的极简载体
//（storage / payment / workflow 各有一个 requireApp 返回它）。
type appNameHolder struct {
	Name string
}

// derefString 安全解引用 SDK 返回的字符串指针。
// 各家 SDK 的响应字段全是指针，逐处写 if != nil 会把发送器淹没在样板里。
func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func generateEmailCode() string {
	return generateEmailCodeLength(6)
}

// generateEmailCodeLength 指定位数的数字验证码。
func generateEmailCodeLength(length int) string {
	if length <= 0 {
		length = 6
	}
	const digits = "0123456789"
	buf := make([]byte, length)
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
