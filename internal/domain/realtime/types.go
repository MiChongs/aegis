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

// AppOnlineUser 一个在线用户在某应用下的连接概况。
//
// Account / Nickname / IP / ConnectedAt 是给管理端表格直接用的：
// presence 存在 Redis 里，那里只有 userId，账号名要回 Postgres 查；
// IP 与建立时间躺在 SampleConnection 里，嵌一层就得让每个调用方自己去挖。
// 之前这四项都没有，管理端的在线用户表于是只有时间列有值，
// 用户与 IP 两列一直是空的。
type AppOnlineUser struct {
	AppID       int64  `json:"appid"`
	UserID      int64  `json:"userId"`
	Account     string `json:"account,omitempty"`
	Nickname    string `json:"nickname,omitempty"`
	IP          string `json:"ip,omitempty"`
	Connections int64  `json:"connections"`
	// ConnectedAt 取最早的一条连接：用户关心的是"从什么时候开始在线"，
	// 而不是最近一次重连的时刻。
	ConnectedAt       time.Time            `json:"connectedAt,omitzero"`
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
