package user

import "time"

const (
	AccountBanTypeTemporary = "temporary"
	AccountBanTypePermanent = "permanent"

	// AccountBanScopeLogin 只拦新会话：各类登录都被拒，已登录的设备照常使用（含刷新令牌续期）。
	AccountBanScopeLogin = "login"
	// AccountBanScopeAll 拦全部访问：封禁当场撤销所有会话与第三方授权，已签发的令牌访问业务接口也被拒。
	AccountBanScopeAll = "all"

	AccountBanStatusActive  = "active"
	AccountBanStatusExpired = "expired"
	AccountBanStatusRevoked = "revoked"

	// AccountBanLevelApp 只封当前应用里的这个账号
	AccountBanLevelApp = "app"
	// AccountBanLevelPlatform 全站封禁连带的账号封禁（PlatformBanID 指向那条全站封禁）
	AccountBanLevelPlatform = "platform"

	// 封禁来源
	AccountBanSourceManual   = "manual"
	AccountBanSourceRisk     = "risk"
	AccountBanSourcePlatform = "platform"
)

// BanReasonCodes 原因类别（给用户看的标准文案见 domain/suspension）
var BanReasonCodes = []string{"policy", "fraud", "spam", "abuse", "security", "payment", "other"}

// ValidBanReasonCode 原因类别是否认识
func ValidBanReasonCode(code string) bool {
	for _, c := range BanReasonCodes {
		if c == code {
			return true
		}
	}
	return false
}

// AccountBanCheckpoint 是封禁的执行点。两档范围的区别完全落在这里：
// 仅登录的封禁只在 SignIn 生效，全部访问的封禁两处都生效。
type AccountBanCheckpoint uint8

const (
	// AccountBanCheckpointSignIn 换取新会话：密码 / 短信 / 第三方 / 卡密 / 扫码 / 通行密钥登录、
	// 二次验证、找回密码后的登录，以及 OAuth2 授权服务器上的新登录。
	AccountBanCheckpointSignIn AccountBanCheckpoint = iota
	// AccountBanCheckpointSession 已签发的会话继续使用：业务接口、刷新令牌、OAuth2 刷新。
	AccountBanCheckpointSession
)

// Blocks 判定这条封禁在某个执行点是否拦人。nil 表示没有生效中的封禁。
func (b *AccountBan) Blocks(checkpoint AccountBanCheckpoint) bool {
	if b == nil {
		return false
	}
	if checkpoint == AccountBanCheckpointSignIn {
		return true
	}
	return b.BanScope != AccountBanScopeLogin
}

type BanOperator struct {
	AdminID   int64  `json:"adminId"`
	AdminName string `json:"adminName"`
}

type AccountBan struct {
	ID                 int64          `json:"id"`
	AppID              int64          `json:"appid"`
	UserID             int64          `json:"userId"`
	BanType            string         `json:"banType"`
	BanScope           string         `json:"banScope"`
	Status             string         `json:"status"`
	Reason             string         `json:"reason"`
	Evidence           map[string]any `json:"evidence,omitempty"`
	BannedByAdminID    *int64         `json:"bannedByAdminId,omitempty"`
	BannedByAdminName  string         `json:"bannedByAdminName,omitempty"`
	RevokedByAdminID   *int64         `json:"revokedByAdminId,omitempty"`
	RevokedByAdminName string         `json:"revokedByAdminName,omitempty"`
	RevokeReason       string         `json:"revokeReason,omitempty"`
	StartAt            time.Time      `json:"startAt"`
	EndAt              *time.Time     `json:"endAt,omitempty"`
	RevokedAt          *time.Time     `json:"revokedAt,omitempty"`
	CreatedAt          time.Time      `json:"createdAt"`
	UpdatedAt          time.Time      `json:"updatedAt"`
	// Level app 应用级 / platform 全站封禁连带
	Level         string `json:"level"`
	PlatformBanID *int64 `json:"platformBanId,omitempty"`
	// ReasonCode 原因类别；PublicReason 给用户看的说明（空时用类别标准文案），Reason 是内部原因
	ReasonCode   string `json:"reasonCode"`
	PublicReason string `json:"publicReason,omitempty"`
	Appealable   bool   `json:"appealable"`
	// Source manual / risk / platform
	Source string `json:"source"`
}

type AccountBanCreateInput struct {
	BanType  string         `json:"banType"`
	BanScope string         `json:"banScope"`
	Reason   string         `json:"reason"`
	Evidence map[string]any `json:"evidence,omitempty"`
	StartAt  *time.Time     `json:"startAt,omitempty"`
	EndAt    *time.Time     `json:"endAt,omitempty"`
	Operator BanOperator    `json:"operator"`
	// Level app（默认）/ platform：全站封禁这个人（邮箱、手机号、第三方身份、设备），连带所有应用里的账号
	Level        string `json:"level,omitempty"`
	ReasonCode   string `json:"reasonCode,omitempty"`
	PublicReason string `json:"publicReason,omitempty"`
	// Appealable 默认允许申诉（nil = true）
	Appealable *bool `json:"appealable,omitempty"`
	// Notify 默认发封禁通知（nil = true）
	Notify *bool `json:"notify,omitempty"`
	// Source manual（默认）/ risk / platform；PlatformBanID 由全站封禁连带时填
	Source        string `json:"source,omitempty"`
	PlatformBanID *int64 `json:"platformBanId,omitempty"`
}

type AccountBanRevokeInput struct {
	Reason   string      `json:"reason"`
	Operator BanOperator `json:"operator"`
	// Notify 默认发解封通知（nil = true）
	Notify *bool `json:"notify,omitempty"`
}

type AccountBanQuery struct {
	Status string `json:"status"`
	Page   int    `json:"page"`
	Limit  int    `json:"limit"`
}

// AccountBanCounts 是某个用户各状态封禁的条数，不受列表的状态筛选影响。
type AccountBanCounts struct {
	Total   int64 `json:"total"`
	Active  int64 `json:"active"`
	Expired int64 `json:"expired"`
	Revoked int64 `json:"revoked"`
}

type AccountBanListResult struct {
	Items      []AccountBan     `json:"items"`
	Page       int              `json:"page"`
	Limit      int              `json:"limit"`
	Total      int64            `json:"total"`
	TotalPages int              `json:"totalPages"`
	Counts     AccountBanCounts `json:"counts"`
}

type AccountBanBatchCreateInput struct {
	UserIDs []int64 `json:"userIds"`
	AccountBanCreateInput
}

type AccountBanBatchCreateResult struct {
	AppID            int64   `json:"appid"`
	Requested        int     `json:"requested"`
	Created          int64   `json:"created"`
	Failed           int     `json:"failed"`
	ProcessedUserIDs []int64 `json:"processedUserIds"`
	FailedUserIDs    []int64 `json:"failedUserIds"`
}
