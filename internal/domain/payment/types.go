package payment

import (
	"time"

	"github.com/shopspring/decimal"
)

type Config struct {
	ID            int64          `json:"id"`
	AppID         int64          `json:"appid"`
	PaymentMethod string         `json:"payment_method"`
	ConfigName    string         `json:"config_name"`
	ConfigData    map[string]any `json:"config_data"`
	Enabled       bool           `json:"enabled"`
	IsDefault     bool           `json:"is_default"`
	Description   string         `json:"description,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

type ConfigMutation struct {
	ID            int64
	AppID         int64
	PaymentMethod *string
	ConfigName    *string
	ConfigData    map[string]any
	Enabled       *bool
	IsDefault     *bool
	Description   *string
}

type EpayConfig struct {
	PID            string   `json:"pid"`
	Key            string   `json:"key"`
	APIURL         string   `json:"apiUrl"`
	SiteName       string   `json:"sitename"`
	NotifyURL      string   `json:"notifyUrl"`
	ReturnURL      string   `json:"returnUrl"`
	SignType       string   `json:"signType"`
	SupportedTypes []string `json:"supportedTypes"`
	ExpireMinutes  int      `json:"expireMinutes"`
	MinAmount      float64  `json:"minAmount"`
	MaxAmount      float64  `json:"maxAmount"`
	AllowedIPs     []string `json:"allowedIPs"`
	VerifyIP       bool     `json:"verifyIP"`
}

type Order struct {
	ID              int64           `json:"id"`
	AppID           int64           `json:"appid"`
	UserID          *int64          `json:"user_id,omitempty"`
	ConfigID        int64           `json:"config_id"`
	OrderNo         string          `json:"order_no"`
	ProviderOrderNo string          `json:"provider_order_no,omitempty"`
	Subject         string          `json:"subject"`
	Body            string          `json:"body,omitempty"`
	Amount          decimal.Decimal `json:"amount"`
	PaymentMethod   string          `json:"payment_method"`
	ProviderType    string          `json:"provider_type"`
	Status          string          `json:"status"`
	NotifyStatus    string          `json:"notify_status"`
	ClientIP        string          `json:"client_ip,omitempty"`
	NotifyURL       string          `json:"notify_url,omitempty"`
	ReturnURL       string          `json:"return_url,omitempty"`
	Metadata        map[string]any  `json:"metadata,omitempty"`
	RawCallback     map[string]any  `json:"raw_callback,omitempty"`
	PaidAt          *time.Time      `json:"paid_at,omitempty"`
	ExpireAt        *time.Time      `json:"expire_at,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
}

type OrderMutation struct {
	AppID         int64
	UserID        *int64
	ConfigID      int64
	OrderNo       string
	Subject       string
	Body          string
	Amount        decimal.Decimal
	PaymentMethod string
	ProviderType  string
	ClientIP      string
	NotifyURL     string
	ReturnURL     string
	Metadata      map[string]any
	ExpireAt      *time.Time
}

type PaymentPayload struct {
	Success      bool           `json:"success"`
	OrderNo      string         `json:"order_no"`
	PaymentURL   string         `json:"payment_url,omitempty"`
	RedirectURL  string         `json:"redirect_url,omitempty"`
	HTML         string         `json:"html,omitempty"`
	FormData     map[string]any `json:"form_data,omitempty"`
	Message      string         `json:"message,omitempty"`
	ProviderType string         `json:"provider_type,omitempty"`
}

type CallbackResult struct {
	Success         bool            `json:"success"`
	Paid            bool            `json:"paid"`
	OrderNo         string          `json:"order_no"`
	ProviderOrderNo string          `json:"provider_order_no,omitempty"`
	TradeStatus     string          `json:"trade_status,omitempty"`
	PaymentMethod   string          `json:"payment_method,omitempty"`
	Amount          decimal.Decimal `json:"amount"`
	Message         string          `json:"message,omitempty"`
	RawData         map[string]any  `json:"raw_data,omitempty"`
}
