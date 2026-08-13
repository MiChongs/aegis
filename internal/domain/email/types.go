package email

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// 邮件服务商标识。
//
// 这些不是「口味选择」：
//   - Zeabur 平台在网络层封禁了出站 SMTP 端口（底层为 Akamai/Linode），
//     部署在其上的实例**无论如何配置 SMTP 都发不出信**，只能走 REST API；
//   - 国内云厂商（阿里云 / 腾讯云）的发信域名要在各自控制台备案，
//     换一家等于换一次域名验证。
//
// 因此服务商是部署形态与合规要求共同决定的，平台把它们全部做成可选项。
const (
	ProviderSMTP     = "smtp"
	ProviderZeabur   = "zeabur"
	ProviderSES      = "ses"      // AWS Simple Email Service v2
	ProviderResend   = "resend"   // Resend
	ProviderSendGrid = "sendgrid" // Twilio SendGrid
	ProviderMailgun  = "mailgun"  // Mailgun
	ProviderPostmark = "postmark" // Postmark
	ProviderAliyun   = "aliyun"   // 阿里云邮件推送 DirectMail
	ProviderTencent  = "tencent"  // 腾讯云邮件推送 SES
)

// ZeaburDefaultBaseURL 是 Zeabur Email 的 REST 根地址，配置留空时使用。
const ZeaburDefaultBaseURL = "https://api.zeabur.com/api/v1/zsend"

// ── 作用域 ──

// PlatformAppID 是平台级配置在 appid 这一维上的取值。
//
// 与 notify_channels / ticket_categories 同一条约定：0 表示「不属于任何应用」。
// 平台级邮件通道服务的是管理端自己的信（管理员通知、平台告警、平台级测试），
// 以及**被显式共享**给应用的兜底通道（见 Config.Shared）。
const PlatformAppID int64 = 0

// ScopeLabel 把作用域翻成可直接展示的中文，避免各处各写一遍三元表达式。
func ScopeLabel(appID int64) string {
	if appID == PlatformAppID {
		return "平台级"
	}
	return "应用级"
}

// ── 通用配置字段键 ──
//
// 发件人身份三件套所有服务商通用，因此提成常量：
// 它同时出现在目录声明、各发送器的读取处、以及投递留痕里，
// 拼错任何一处的表现都是「发件人莫名其妙变成空的」而不报错。
const (
	KeyFromAddress   = "fromAddress"
	KeyFromName      = "fromName"
	KeyReplyTo       = "replyTo"
	KeyWebhookSecret = "webhookSecret"
	KeyTags          = "tags"
)

// SMTP 的加密方式。取值放在 domain 是因为仓储层解存量行时也要写它：
// 旧行存的是一个 useTLS 布尔位，翻译成枚举的代码在仓储，读它的代码在发送器。
const (
	SMTPEncryptionSSL      = "ssl"      // 隐式 TLS，通常是 465
	SMTPEncryptionSTARTTLS = "starttls" // 明文连接后升级，通常是 587
)

// Config 是一条邮件通道的配置。
//
// 字段值放在**通用的 Settings / Secrets 两个袋子**里，而不是每家服务商一个具名 struct：
// 键的含义由服务商目录（ProviderMeta.Fields）声明，服务端据此校验、加密与抹除。
// 具名 struct 的做法在两家服务商时还行，到九家就意味着每加一家要动
// 领域类型、仓储载荷、传输 DTO、控制台表单四处，而其中任何一处漏改都不报错。
type Config struct {
	ID int64 `json:"id"`
	// AppID 为 0 时是平台级配置（PlatformAppID）。
	AppID     int64  `json:"appid"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Enabled   bool   `json:"enabled"`
	IsDefault bool   `json:"isDefault"`
	// Shared 只对平台级配置有意义：允许应用在自己**没有任何可用配置**时回落到这条通道。
	//
	// 默认关闭是刻意的 —— 打开它意味着该应用发出的信会用平台的发件人身份，
	// 而应用管理员既没参与这个决定、也看不出信是从哪条通道出去的。
	// 因此这是平台管理员的一次显式授权，不是默认行为。
	Shared      bool   `json:"shared"`
	Description string `json:"description,omitempty"`

	// Settings 非密钥字段，出网原样返回。
	Settings map[string]string `json:"settings"`
	// Secrets 密钥明文，**只在进程内存在**：从仓储读出后由服务层解密填入，
	// 任何出网响应都会被 Sanitize 清空。
	Secrets map[string]string `json:"-"`
	// SecretsCipher 密钥密文，仓储层与服务层之间传递用。
	SecretsCipher map[string]string `json:"-"`
	// SecretSet 出网用的「这个密钥配没配」布尔位。
	SecretSet map[string]bool `json:"secretSet"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// IsPlatform 该配置是不是平台级。
func (c Config) IsPlatform() bool { return c.AppID == PlatformAppID }

// Setting 取一个非密钥字段，缺失返回空串。
func (c Config) Setting(key string) string {
	if c.Settings == nil {
		return ""
	}
	return strings.TrimSpace(c.Settings[key])
}

// SettingRaw 取一个非密钥字段且**不做裁剪**。
// 少数字段（如自定义签名前缀）的首尾空白是有意义的。
func (c Config) SettingRaw(key string) string {
	if c.Settings == nil {
		return ""
	}
	return c.Settings[key]
}

// SettingBool 把开关字段解析成布尔。只认 true/1/yes/on，其余一律 false ——
// 「解析不出来就当开」会让一个拼错的值静默打开某项防护之外的行为。
func (c Config) SettingBool(key string) bool {
	switch strings.ToLower(c.Setting(key)) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}

// SettingInt 把数值字段解析成整数，解析失败返回 fallback。
func (c Config) SettingInt(key string, fallback int) int {
	value := c.Setting(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

// SettingMap 把 kv 类型字段（以 JSON 对象字符串存放）解析成 map。
func (c Config) SettingMap(key string) map[string]string {
	raw := c.Setting(key)
	if raw == "" {
		return nil
	}
	parsed := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil
	}
	if len(parsed) == 0 {
		return nil
	}
	return parsed
}

// Secret 取一个已解密的密钥字段。
func (c Config) Secret(key string) string {
	if c.Secrets == nil {
		return ""
	}
	return strings.TrimSpace(c.Secrets[key])
}

// HasSecret 该密钥是否已配置（不暴露值本身）。
func (c Config) HasSecret(key string) bool {
	if strings.TrimSpace(c.Secret(key)) != "" {
		return true
	}
	if c.SecretsCipher == nil {
		return false
	}
	return strings.TrimSpace(c.SecretsCipher[key]) != ""
}

// SenderIdentity 返回发件人身份。三个键全服务商通用，
// 调用方因此不必按 provider 分支去取 From / ReplyTo。
func (c Config) SenderIdentity() (fromAddress string, fromName string, replyTo string) {
	return c.Setting(KeyFromAddress), c.Setting(KeyFromName), c.Setting(KeyReplyTo)
}

// Clone 深拷贝，避免调用方改到共享的 map。
func (c Config) Clone() Config {
	cloned := c
	cloned.Settings = cloneStringMap(c.Settings)
	cloned.Secrets = cloneStringMap(c.Secrets)
	cloned.SecretsCipher = cloneStringMap(c.SecretsCipher)
	if c.SecretSet != nil {
		set := make(map[string]bool, len(c.SecretSet))
		for key, value := range c.SecretSet {
			set[key] = value
		}
		cloned.SecretSet = set
	}
	return cloned
}

func cloneStringMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	cloned := make(map[string]string, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

// ConfigMutation 是一次配置写入。指针字段为 nil 表示「不修改这一项」。
//
// Settings 与 Secrets 同样是「只覆盖出现的键」：控制台切换服务商时
// 只会提交新服务商的字段，全量覆盖会把上一家的配置清空，
// 而切回去时用户会发现凭据没了 —— 那次丢失没有任何提示。
type ConfigMutation struct {
	ID          int64
	AppID       int64
	Name        *string
	Provider    *string
	Enabled     *bool
	IsDefault   *bool
	Shared      *bool
	Description *string
	Settings    map[string]string
	// Secrets 明文密钥。值为空串的键会被**忽略**（留空即不修改）；
	// 要清空一个已配置的密钥请显式传 ClearSecrets。
	Secrets      map[string]string
	ClearSecrets []string
	// ReplaceSettings 为 true 时用 Settings 整体替换而不是逐键合并，
	// 供「切换服务商」这类确实要丢弃旧字段的场景使用。
	ReplaceSettings bool
}

type VerificationResult struct {
	Success   bool      `json:"success"`
	Email     string    `json:"email"`
	Purpose   string    `json:"purpose"`
	Code      string    `json:"code,omitempty"`
	ExpireAt  time.Time `json:"expireAt"`
	MessageID string    `json:"messageId,omitempty"`
}

type ResetResult struct {
	Success   bool      `json:"success"`
	Email     string    `json:"email"`
	Token     string    `json:"token,omitempty"`
	ResetURL  string    `json:"resetUrl,omitempty"`
	ExpireAt  time.Time `json:"expireAt"`
	MessageID string    `json:"messageId,omitempty"`
}

// ── 通道解析结论 ──

// Resolution 描述「这次发信最终用的是哪条通道、为什么」。
//
// 它是给控制台看的：应用没有自己的配置而回落到平台通道时，
// 界面必须说得出这件事，否则管理员会对着一个空的邮件配置页
// 纳闷「明明没配，验证码怎么发出去的」。
type Resolution struct {
	ConfigID   int64  `json:"configId"`
	ConfigName string `json:"configName"`
	Provider   string `json:"provider"`
	// Scope 这条通道属于哪个作用域：app / platform
	Scope string `json:"scope"`
	// Inherited 为 true 表示这是应用回落到平台共享通道的结果
	Inherited   bool `json:"inherited"`
	Attachments bool `json:"attachments"`
}

const (
	ScopeApp      = "app"
	ScopePlatform = "platform"
)

// ── 投递记录 ──

// 投递状态。SMTP 通道交出信件即为 sent（协议本身没有回执），
// 只有带 webhook 的服务商才能把状态推进到 delivered / bounced 等终态。
const (
	DeliveryStatusPending    = "pending"
	DeliveryStatusSent       = "sent"
	DeliveryStatusDelivered  = "delivered"
	DeliveryStatusBounced    = "bounced"
	DeliveryStatusComplained = "complained"
	DeliveryStatusRejected   = "rejected"
	DeliveryStatusFailed     = "failed"
)

// 归一化后的投递事件类型。
//
// 各家 webhook 的事件名各不相同（Zeabur 叫 delivery、SES 叫 Delivery、
// SendGrid 叫 delivered、Resend 叫 email.delivered），
// 统一翻译到这一组再往下走，投递记录里的 last_event 才有可比性。
const (
	EventSend      = "send"
	EventDelivery  = "delivery"
	EventBounce    = "bounce"
	EventComplaint = "complaint"
	EventReject    = "reject"
	EventOpen      = "open"
	EventClick     = "click"
)

// Delivery 是一封外发邮件的留痕。
//
// ProviderMessageID 存的是服务商侧的邮件 ID（webhook 回填的关联键），
// MessageID 是 RFC 5322 的 Message-ID，两者不同源，不要混用。
type Delivery struct {
	ID    int64 `json:"id"`
	AppID int64 `json:"appid"`
	// Scope 冗余一列作用域，让平台级留痕不必靠「appid 是不是 0」去反推。
	Scope             string         `json:"scope"`
	ConfigID          int64          `json:"configId"`
	ConfigName        string         `json:"configName,omitempty"`
	Provider          string         `json:"provider"`
	ProviderMessageID string         `json:"providerMessageId,omitempty"`
	MessageID         string         `json:"messageId,omitempty"`
	ToAddress         string         `json:"toAddress"`
	FromAddress       string         `json:"fromAddress,omitempty"`
	Subject           string         `json:"subject"`
	Purpose           string         `json:"purpose,omitempty"`
	Status            string         `json:"status"`
	ErrorMessage      string         `json:"errorMessage,omitempty"`
	OpenCount         int            `json:"openCount"`
	ClickCount        int            `json:"clickCount"`
	LastEvent         string         `json:"lastEvent,omitempty"`
	LastEventPayload  map[string]any `json:"lastEventPayload,omitempty"`
	SentAt            *time.Time     `json:"sentAt,omitempty"`
	DeliveredAt       *time.Time     `json:"deliveredAt,omitempty"`
	CreatedAt         time.Time      `json:"createdAt"`
	UpdatedAt         time.Time      `json:"updatedAt"`
}

// DeliveryQuery 是投递记录的分页筛选条件。
type DeliveryQuery struct {
	// AppID 为 PlatformAppID 时查平台级留痕。
	AppID    int64
	ConfigID int64
	Status   string
	Provider string
	Purpose  string
	Keyword  string
	Page     int
	PageSize int
}

type DeliveryPage struct {
	Items    []Delivery `json:"items"`
	Total    int64      `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"pageSize"`
}

// DeliveryStats 一个作用域下的投递概况，供控制台顶部的状态条使用。
//
// 「发了多少封」与「有多少封退回来了」是两个完全不同的运维信号，
// 只给一张流水表的话，退信率要靠人翻页去数。
type DeliveryStats struct {
	Total     int64 `json:"total"`
	Sent      int64 `json:"sent"`
	Delivered int64 `json:"delivered"`
	Failed    int64 `json:"failed"`
	Bounced   int64 `json:"bounced"`
	Pending   int64 `json:"pending"`
	// Last24h 最近 24 小时的发信量，用来判断「这条通道还在不在用」。
	Last24h int64 `json:"last24h"`
}

// WebhookEvent 是 Zeabur webhook 的请求体。
type WebhookEvent struct {
	Event     string `json:"event"`
	Timestamp string `json:"timestamp"`
	Email     struct {
		ID        string   `json:"id"`
		MessageID string   `json:"message_id"`
		From      string   `json:"from"`
		To        []string `json:"to"`
		Subject   string   `json:"subject"`
		SentAt    string   `json:"sent_at"`
	} `json:"email"`
	Data map[string]any `json:"data"`
}
