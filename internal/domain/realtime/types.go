package realtime

import "time"

type Event struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	AppID     int64          `json:"appid"`
	UserID    int64          `json:"userId"`
	Timestamp time.Time      `json:"timestamp"`
	Data      map[string]any `json:"data,omitempty"`
}

type PresenceConnection struct {
	ConnectionID string    `json:"connectionId"`
	AppID        int64     `json:"appid"`
	UserID       int64     `json:"userId"`
	TokenID      string    `json:"tokenId,omitempty"`
	DeviceID     string    `json:"deviceId,omitempty"`
	IP           string    `json:"ip,omitempty"`
	UserAgent    string    `json:"userAgent,omitempty"`
	ConnectedAt  time.Time `json:"connectedAt"`
	LastSeenAt   time.Time `json:"lastSeenAt"`
	ServerID     string    `json:"serverId,omitempty"`
}

type OnlineStats struct {
	OnlineUsers       int64     `json:"onlineUsers"`
	OnlineConnections int64     `json:"onlineConnections"`
	OnlineApps        int64     `json:"onlineApps"`
	RefreshedAt       time.Time `json:"refreshedAt"`
}

type AppOnlineStats struct {
	AppID             int64     `json:"appid"`
	OnlineUsers       int64     `json:"onlineUsers"`
	OnlineConnections int64     `json:"onlineConnections"`
	RefreshedAt       time.Time `json:"refreshedAt"`
}

type AppOnlineUser struct {
	AppID             int64                `json:"appid"`
	UserID            int64                `json:"userId"`
	Connections       int64                `json:"connections"`
	LastSeenAt        time.Time            `json:"lastSeenAt"`
	SampleConnection  *PresenceConnection  `json:"sampleConnection,omitempty"`
	ConnectionSamples []PresenceConnection `json:"connectionSamples,omitempty"`
}

type AppOnlineUserList struct {
	AppID       int64           `json:"appid"`
	Page        int             `json:"page"`
	Limit       int             `json:"limit"`
	Total       int64           `json:"total"`
	TotalPages  int             `json:"totalPages"`
	Items       []AppOnlineUser `json:"items"`
	RefreshedAt time.Time       `json:"refreshedAt"`
}
