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
	UserID          int64     `json:"user_id"`
	AppID           int64     `json:"appid"`
	Account         string    `json:"account"`
	TokenID         string    `json:"token_id"`
	RefreshFamilyID string    `json:"refresh_family_id,omitempty"`
	SessionVersion  int64     `json:"session_version"`
	DeviceID        string    `json:"device_id,omitempty"` // 设备唯一识别码（UUID/指纹）
	Device          string    `json:"device,omitempty"`    // 设备可读名称（Chrome on Windows 等）
	IP              string    `json:"ip,omitempty"`
	UserAgent       string    `json:"user_agent,omitempty"`
	ExpiresAt       time.Time `json:"expires_at"`
	IssuedAt        time.Time `json:"issued_at"`
	Provider        string    `json:"provider,omitempty"`
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
	UserID          int64      `json:"user_id"`
	AppID           int64      `json:"appid"`
	Account         string     `json:"account"`
	TokenID         string     `json:"token_id"`
	FamilyID        string     `json:"family_id"`
	SessionVersion  int64      `json:"session_version"`
	DeviceID        string     `json:"device_id,omitempty"`
	Device          string     `json:"device,omitempty"`
	IP              string     `json:"ip,omitempty"`
	UserAgent       string     `json:"user_agent,omitempty"`
	Provider        string     `json:"provider,omitempty"`
	ExpiresAt       time.Time  `json:"expires_at"`
	IssuedAt        time.Time  `json:"issued_at"`
	UsedAt          *time.Time `json:"used_at,omitempty"`
	RotatedAt       *time.Time `json:"rotated_at,omitempty"`
	ReplacedByToken string     `json:"replaced_by_token,omitempty"`
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
	AccessToken          string                 `json:"accessToken,omitempty"`
	RefreshToken         string                 `json:"refreshToken,omitempty"`
	ExpiresAt            time.Time              `json:"expiresAt,omitempty"`
	RefreshExpiresAt     time.Time              `json:"refreshExpiresAt,omitempty"`
	TokenType            string                 `json:"tokenType,omitempty"`
	UserID               int64                  `json:"userId"`
	Account              string                 `json:"account"`
	Provider             string                 `json:"provider,omitempty"`
	RequiresSecondFactor bool                   `json:"requiresSecondFactor,omitempty"`
	AuthenticationState  string                 `json:"authenticationState,omitempty"`
	Challenge            *SecondFactorChallenge `json:"challenge,omitempty"`
	// PasswordChangeRequired 该账号被标记为必须修改密码（如批量导入后设置统一密码），
	// 客户端应在登录成功后立即引导强制改密
	PasswordChangeRequired bool `json:"passwordChangeRequired,omitempty"`
}
