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
	DeviceID        string    `json:"device_id,omitempty"`
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
	IP              string     `json:"ip,omitempty"`
	UserAgent       string     `json:"user_agent,omitempty"`
	Provider        string     `json:"provider,omitempty"`
	ExpiresAt       time.Time  `json:"expires_at"`
	IssuedAt        time.Time  `json:"issued_at"`
	UsedAt          *time.Time `json:"used_at,omitempty"`
	RotatedAt       *time.Time `json:"rotated_at,omitempty"`
	ReplacedByToken string     `json:"replaced_by_token,omitempty"`
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
}
