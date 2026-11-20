package auth

import (
	"aegis/internal/domain/devicelimit"
	"time"

	"aegis/internal/domain/accountdeletion"
	devicedomain "aegis/internal/domain/device"
)

type Claims struct {
	UserID         int64  `json:"uid"`
	AppID          int64  `json:"appid"`
	Account        string `json:"account"`
	SessionVersion int64  `json:"sv"`
	TokenID        string `json:"jti"`
}

type Session struct {
	UserID          int64  `json:"user_id"`
	AppID           int64  `json:"appid"`
	Account         string `json:"account"`
	TokenID         string `json:"token_id"`
	RefreshFamilyID string `json:"refresh_family_id,omitempty"`
	SessionVersion  int64  `json:"session_version"`
	DeviceID        string `json:"device_id,omitempty"` // 设备唯一识别码（UUID/指纹）
	Device          string `json:"device,omitempty"`    // 设备可读名称（Chrome on Windows 等）
	// DeviceModel / DevicePlatform 客户端上报的原始型号与平台。对外接口的 deviceInfo
	// 按它们读取时现查设备字典；Device 只是签发那一刻翻译出的名字
	DeviceModel    string `json:"device_model,omitempty"`
	DevicePlatform string `json:"device_platform,omitempty"`
	// DeviceExtra 客户端上报的厂商、品牌、代号、系统与 App 版本（旧会话没有）
	DeviceExtra *devicedomain.Extra `json:"device_extra,omitempty"`
	IP          string              `json:"ip,omitempty"`
	UserAgent   string              `json:"user_agent,omitempty"`
	ExpiresAt   time.Time           `json:"expires_at"`
	IssuedAt    time.Time           `json:"issued_at"`
	Provider    string              `json:"provider,omitempty"`
}

type IndexedSession struct {
	TokenHash string  `json:"tokenHash"`
	Session   Session `json:"session"`
}

type IndexedRefreshSession struct {
	TokenHash string         `json:"tokenHash"`
	Session   RefreshSession `json:"session"`
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

type RefreshSession struct {
	UserID          int64               `json:"user_id"`
	AppID           int64               `json:"appid"`
	Account         string              `json:"account"`
	TokenID         string              `json:"token_id"`
	FamilyID        string              `json:"family_id"`
	SessionVersion  int64               `json:"session_version"`
	DeviceID        string              `json:"device_id,omitempty"`
	Device          string              `json:"device,omitempty"`
	DeviceModel     string              `json:"device_model,omitempty"`
	DevicePlatform  string              `json:"device_platform,omitempty"`
	DeviceExtra     *devicedomain.Extra `json:"device_extra,omitempty"`
	IP              string              `json:"ip,omitempty"`
	UserAgent       string              `json:"user_agent,omitempty"`
	Provider        string              `json:"provider,omitempty"`
	ExpiresAt       time.Time           `json:"expires_at"`
	IssuedAt        time.Time           `json:"issued_at"`
	UsedAt          *time.Time          `json:"used_at,omitempty"`
	RotatedAt       *time.Time          `json:"rotated_at,omitempty"`
	ReplacedByToken string              `json:"replaced_by_token,omitempty"`
}

// FirstDevice 用户首次登录/注册使用的设备记录
// 用于风控比对、账号异常登录告警等场景；生命周期通常等同账号存在
type FirstDevice struct {
	UserID      int64     `json:"userId"`
	AppID       int64     `json:"appid"`
	DeviceID    string    `json:"deviceId"`
	Device      string    `json:"device"`
	IP          string    `json:"ip"`
	UserAgent   string    `json:"userAgent"`
	Provider    string    `json:"provider,omitempty"` // password / oauth / passkey
	Scene       string    `json:"scene"`              // register / login
	FirstSeenAt time.Time `json:"firstSeenAt"`
}

type SecondFactorChallenge struct {
	ChallengeID string    `json:"challengeId"`
	State       string    `json:"state"`
	Methods     []string  `json:"methods"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

type LoginResult struct {
	AccessToken      string    `json:"accessToken,omitempty"`
	RefreshToken     string    `json:"refreshToken,omitempty"`
	ExpiresAt        time.Time `json:"expiresAt,omitempty"`
	RefreshExpiresAt time.Time `json:"refreshExpiresAt,omitempty"`
	TokenType        string    `json:"tokenType,omitempty"`
	// UserID 数据库主键，服务端内部用（登录后的邀请绑定、钩子等），不下发
	UserID int64 `json:"-"`
	// PublicUserID 对外用户 ID，响应里的 userId 就是它（docs/user-public-id.md）
	PublicUserID int64 `json:"userId"`
	// Number 号码（docs/user-number.md）：展示用，可以更换
	Number               int64                  `json:"number,omitempty"`
	Account              string                 `json:"account"`
	Provider             string                 `json:"provider,omitempty"`
	RequiresSecondFactor bool                   `json:"requiresSecondFactor,omitempty"`
	AuthenticationState  string                 `json:"authenticationState,omitempty"`
	Challenge            *SecondFactorChallenge `json:"challenge,omitempty"`
	// PasswordChangeRequired 该账号被标记为必须修改密码（如批量导入后设置统一密码），
	// 客户端应在登录成功后立即引导强制改密
	PasswordChangeRequired bool `json:"passwordChangeRequired,omitempty"`
	// OAuthLink 第三方身份已核实但还没有关联任何账号（原生换会话时带了 deferRegistration）。
	// 此时不签发令牌：客户端让用户选「快速注册」或「绑定已有账号」，再凭 Ticket 继续
	OAuthLink *OAuthLinkTicket `json:"oauthLink,omitempty"`
	// AccountDeletion 账号处于注销冷静期或待审核时出现；Cancelled 为真表示这次登录撤销了冷静期。
	// 客户端据此提示「账号将于某日注销」或「注销已撤销」
	AccountDeletion *accountdeletion.LoginNotice `json:"accountDeletion,omitempty"`
	// DeviceLimit 登录设备数已满、需要用户选择下线哪台时出现（authenticationState=device_limit_required）。
	// 此时不签发令牌：客户端展示设备列表，凭 Ticket 调 /auth/device-limit/resolve 完成登录（docs/device-limit.md）
	DeviceLimit *devicelimit.Challenge `json:"deviceLimit,omitempty"`
}

// OAuthLinkTicket 待关联的第三方身份。
//
// 身份本身（openid、unionid、令牌）只存在服务端，客户端拿到的只有一次性凭证与展示用的昵称头像。
// AllowRegister / AllowBind 是这一刻两条路能不能走的结论，客户端据此决定显示哪个按钮。
type OAuthLinkTicket struct {
	Ticket        string    `json:"ticket"`
	Provider      string    `json:"provider"`
	DisplayName   string    `json:"displayName,omitempty"`
	Nickname      string    `json:"nickname,omitempty"`
	Avatar        string    `json:"avatar,omitempty"`
	AllowRegister bool      `json:"allowRegister"`
	AllowBind     bool      `json:"allowBind"`
	ExpiresAt     time.Time `json:"expiresAt"`
}

// OAuthLinkPayload 关联凭证在服务端保存的内容。
type OAuthLinkPayload struct {
	AppID         int64           `json:"appid"`
	Profile       ProviderProfile `json:"profile"`
	DeviceID      string          `json:"deviceId,omitempty"`
	AllowRegister bool            `json:"allowRegister"`
}
