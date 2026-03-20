package email

import "time"

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

type Config struct {
	ID          int64      `json:"id"`
	AppID       int64      `json:"appid"`
	Name        string     `json:"name"`
	Provider    string     `json:"provider"`
	Enabled     bool       `json:"enabled"`
	IsDefault   bool       `json:"isDefault"`
	Description string     `json:"description,omitempty"`
	SMTP        SMTPConfig `json:"smtp"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
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
