package email

import "time"

// 邮件服务商标识。
//
// Zeabur 平台在网络层封禁了出站 SMTP 端口（底层为 Akamai/Linode），
// 部署在 Zeabur 上的实例**无论如何配置 SMTP 都发不出信**，
// 只能走 Zeabur Email 的 REST API。因此这两档不是「口味选择」而是部署形态决定的。
const (
	ProviderSMTP   = "smtp"
	ProviderZeabur = "zeabur"
)

// ZeaburDefaultBaseURL 是 Zeabur Email 的 REST 根地址，配置留空时使用。
const ZeaburDefaultBaseURL = "https://api.zeabur.com/api/v1/zsend"

type SMTPConfig struct {
	Host               string `json:"host"`
	Port               int    `json:"port"`
	Username           string `json:"username"`
	Password           string `json:"password,omitempty"`
	FromAddress        string `json:"fromAddress"`
	FromName           string `json:"fromName,omitempty"`
	ReplyTo            string `json:"replyTo,omitempty"`
	UseTLS             bool   `json:"useTLS"`
	InsecureSkipVerify bool   `json:"insecureSkipVerify"`
	MaxConnections     int    `json:"maxConnections,omitempty"`
	MaxMessagesPerConn int    `json:"maxMessagesPerConn,omitempty"`
}

// ZeaburConfig 是 Zeabur Email 渠道配置。
//
// APIKey / WebhookSecret 以 AES-GCM 密文落库（密钥派生自 SECURITY_MASTER_KEY），
// 出网一律置空并改用 *Set 布尔位表达「已配置」，避免密钥经由管理接口回流到浏览器。
type ZeaburConfig struct {
	APIKeyCipher        string            `json:"-"`
	APIKey              string            `json:"apiKey,omitempty"`
	APIKeySet           bool              `json:"apiKeySet"`
	BaseURL             string            `json:"baseUrl,omitempty"`
	FromAddress         string            `json:"fromAddress"`
	FromName            string            `json:"fromName,omitempty"`
	ReplyTo             string            `json:"replyTo,omitempty"`
	WebhookSecretCipher string            `json:"-"`
	WebhookSecret       string            `json:"webhookSecret,omitempty"`
	WebhookSecretSet    bool              `json:"webhookSecretSet"`
	Tags                map[string]string `json:"tags,omitempty"`
}

type Config struct {
	ID          int64        `json:"id"`
	AppID       int64        `json:"appid"`
	Name        string       `json:"name"`
	Provider    string       `json:"provider"`
	Enabled     bool         `json:"enabled"`
	IsDefault   bool         `json:"isDefault"`
	Description string       `json:"description,omitempty"`
	SMTP        SMTPConfig   `json:"smtp"`
	Zeabur      ZeaburConfig `json:"zeabur"`
	CreatedAt   time.Time    `json:"createdAt"`
	UpdatedAt   time.Time    `json:"updatedAt"`
}

// SenderIdentity 返回当前 provider 生效的发件人身份，
// 让调用方不必按 provider 分支去取 From/ReplyTo。
func (c Config) SenderIdentity() (fromAddress string, fromName string, replyTo string) {
	if c.Provider == ProviderZeabur {
		return c.Zeabur.FromAddress, c.Zeabur.FromName, c.Zeabur.ReplyTo
	}
	return c.SMTP.FromAddress, c.SMTP.FromName, c.SMTP.ReplyTo
}

type ConfigMutation struct {
	ID          int64
	AppID       int64
	Name        *string
	Provider    *string
	Enabled     *bool
	IsDefault   *bool
	Description *string
	SMTP        *SMTPConfig
	Zeabur      *ZeaburConfig
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

// ── 投递记录 ──

// 投递状态。SMTP 通道交出信件即为 sent（协议本身没有回执），
// 只有 Zeabur webhook 才能把状态推进到 delivered / bounced 等终态。
const (
	DeliveryStatusPending    = "pending"
	DeliveryStatusSent       = "sent"
	DeliveryStatusDelivered  = "delivered"
	DeliveryStatusBounced    = "bounced"
	DeliveryStatusComplained = "complained"
	DeliveryStatusRejected   = "rejected"
	DeliveryStatusFailed     = "failed"
)

// Zeabur webhook 事件类型（X-ZSend-Event）。
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
// ProviderMessageID 存的是 Zeabur 侧的 email id（webhook 回填的关联键），
// MessageID 是 RFC 5322 的 Message-ID，两者不同源，不要混用。
type Delivery struct {
	ID                int64          `json:"id"`
	AppID             int64          `json:"appid"`
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
	AppID    int64
	ConfigID int64
	Status   string
	Provider string
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
