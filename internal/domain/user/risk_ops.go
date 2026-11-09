package user

import "time"

// RiskOpsUser 账号处置（一键解除风控、关联删除）里一个账号的基本情况与当前封禁
type RiskOpsUser struct {
	ID          int64       `json:"id"`
	PublicID    int64       `json:"publicId"`
	AppID       int64       `json:"appid"`
	AppName     string      `json:"appName,omitempty"`
	Account     string      `json:"account"`
	Nickname    string      `json:"nickname,omitempty"`
	Avatar      string      `json:"avatar,omitempty"`
	Enabled     bool        `json:"enabled"`
	CreatedAt   time.Time   `json:"createdAt"`
	LastLoginAt *time.Time  `json:"lastLoginAt,omitempty"`
	Ban         *AccountBan `json:"ban,omitempty"`
	// BanFromRisk 当前封禁是不是风控平台下的（人工封禁一键解除时默认不动）
	BanFromRisk bool `json:"banFromRisk"`
}

// RiskBannedQuery 被风控平台封禁中的账号
type RiskBannedQuery struct {
	AppID   int64
	Keyword string
	Page    int
	Limit   int
}

// RiskDeviceCluster 一台设备上登录过多个账号（批量注册、养号的典型特征）
type RiskDeviceCluster struct {
	AppID     int64     `json:"appid"`
	AppName   string    `json:"appName,omitempty"`
	DeviceKey string    `json:"deviceKey"`
	Platform  string    `json:"platform,omitempty"`
	Model     string    `json:"model,omitempty"`
	Name      string    `json:"name,omitempty"`
	Users     int       `json:"users"`
	LastSeen  time.Time `json:"lastSeen"`
	UserIDs   []int64   `json:"userIds"`
}
