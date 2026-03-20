package auth

import "time"

type Claims struct {
	UserID         int64  `json:"uid"`
	AppID          int64  `json:"appid"`
	Account        string `json:"account"`
	SessionVersion int64  `json:"sv"`
	TokenID        string `json:"jti"`
}

type Session struct {
	UserID         int64     `json:"user_id"`
	AppID          int64     `json:"appid"`
	Account        string    `json:"account"`
	TokenID        string    `json:"token_id"`
	SessionVersion int64     `json:"session_version"`
	DeviceID       string    `json:"device_id,omitempty"`
	IP             string    `json:"ip,omitempty"`
	UserAgent      string    `json:"user_agent,omitempty"`
	ExpiresAt      time.Time `json:"expires_at"`
	IssuedAt       time.Time `json:"issued_at"`
	Provider       string    `json:"provider,omitempty"`
}

type IndexedSession struct {
	TokenHash string  `json:"tokenHash"`
	Session   Session `json:"session"`
}

type ProviderProfile struct {
	Provider       string            `json:"provider"`
	ProviderUserID string            `json:"providerUserId"`
	UnionID        string            `json:"unionId,omitempty"`
	Nickname       string            `json:"nickname,omitempty"`
	Avatar         string            `json:"avatar,omitempty"`
	Email          string            `json:"email,omitempty"`
	RawProfile     map[string]any    `json:"rawProfile,omitempty"`
	Tokens         map[string]string `json:"tokens,omitempty"`
}

type LoginResult struct {
	AccessToken string    `json:"accessToken"`
	ExpiresAt   time.Time `json:"expiresAt"`
	TokenType   string    `json:"tokenType"`
	UserID      int64     `json:"userId"`
	Account     string    `json:"account"`
	Provider    string    `json:"provider,omitempty"`
}
