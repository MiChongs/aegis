package security

import "time"

const (
	ModuleTOTP          = "totp"
	ModuleRecoveryCodes = "recovery_codes"
	ModulePasskey       = "passkey"
)

type ModuleStatus struct {
	Key       string `json:"key"`
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	Ready     bool   `json:"ready"`
	HotReload bool   `json:"hotReload"`
	Message   string `json:"message,omitempty"`
}

type TOTPStatus struct {
	Enabled        bool       `json:"enabled"`
	Method         string     `json:"method,omitempty"`
	Issuer         string     `json:"issuer,omitempty"`
	AccountName    string     `json:"accountName,omitempty"`
	EnabledAt      *time.Time `json:"enabledAt,omitempty"`
	LastVerifiedAt *time.Time `json:"lastVerifiedAt,omitempty"`
	PendingSetup   bool       `json:"pendingSetup"`
}

type TOTPSecretRecord struct {
	UserID         int64      `json:"userId"`
	AppID          int64      `json:"appid"`
	SecretCipher   string     `json:"-"`
	Issuer         string     `json:"issuer"`
	AccountName    string     `json:"accountName"`
	Enabled        bool       `json:"enabled"`
	EnabledAt      *time.Time `json:"enabledAt,omitempty"`
	LastVerifiedAt *time.Time `json:"lastVerifiedAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type TOTPEnrollment struct {
	EnrollmentID    string    `json:"enrollmentId"`
	Secret          string    `json:"secret"`
	SecretMasked    string    `json:"secretMasked"`
	ProvisioningURI string    `json:"provisioningUri"`
	Issuer          string    `json:"issuer"`
	AccountName     string    `json:"accountName"`
	ExpiresAt       time.Time `json:"expiresAt"`
}

type RecoveryCodeRecord struct {
	ID        int64      `json:"id"`
	UserID    int64      `json:"userId"`
	AppID     int64      `json:"appid"`
	CodeHash  string     `json:"-"`
	CodeHint  string     `json:"codeHint"`
	UsedAt    *time.Time `json:"usedAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
}

type RecoveryCodeSummary struct {
	Enabled     bool                 `json:"enabled"`
	Total       int                  `json:"total"`
	Remaining   int                  `json:"remaining"`
	GeneratedAt *time.Time           `json:"generatedAt,omitempty"`
	Items       []RecoveryCodeRecord `json:"items,omitempty"`
}

type RecoveryCodeIssueResult struct {
	Total       int                  `json:"total"`
	Remaining   int                  `json:"remaining"`
	GeneratedAt time.Time            `json:"generatedAt"`
	Codes       []string             `json:"codes"`
	Items       []RecoveryCodeRecord `json:"items"`
}

type LoginChallenge struct {
	ChallengeID string    `json:"challengeId"`
	AppID       int64     `json:"appid"`
	UserID      int64     `json:"userId"`
	Account     string    `json:"account"`
	Provider    string    `json:"provider,omitempty"`
	LoginType   string    `json:"loginType,omitempty"`
	DeviceID    string    `json:"deviceId,omitempty"`
	IP          string    `json:"ip,omitempty"`
	UserAgent   string    `json:"userAgent,omitempty"`
	Methods     []string  `json:"methods"`
	ExpiresAt   time.Time `json:"expiresAt"`
	CreatedAt   time.Time `json:"createdAt"`
}

type PasskeyRecord struct {
	ID             int64      `json:"id"`
	UserID         int64      `json:"userId"`
	AppID          int64      `json:"appid"`
	CredentialID   []byte     `json:"-"`
	CredentialName string     `json:"credentialName,omitempty"`
	CredentialJSON []byte     `json:"-"`
	AAGUID         []byte     `json:"-"`
	SignCount      uint32     `json:"signCount"`
	LastUsedAt     *time.Time `json:"lastUsedAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type PasskeyView struct {
	ID             int64      `json:"id"`
	CredentialID   string     `json:"credentialId"`
	CredentialName string     `json:"credentialName,omitempty"`
	SignCount      uint32     `json:"signCount"`
	LastUsedAt     *time.Time `json:"lastUsedAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type PasskeySummary struct {
	Enabled bool          `json:"enabled"`
	Count   int           `json:"count"`
	Items   []PasskeyView `json:"items,omitempty"`
}

type PasskeyRegistrationSession struct {
	ChallengeID string    `json:"challengeId"`
	AppID       int64     `json:"appid"`
	UserID      int64     `json:"userId"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

type PasskeyLoginSession struct {
	ChallengeID string    `json:"challengeId"`
	AppID       int64     `json:"appid"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

type TOTPEnrollmentState struct {
	EnrollmentID string    `json:"enrollmentId"`
	AppID        int64     `json:"appid"`
	UserID       int64     `json:"userId"`
	Secret       string    `json:"secret"`
	Issuer       string    `json:"issuer"`
	AccountName  string    `json:"accountName"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

type PasskeyRegistrationState struct {
	ChallengeID string    `json:"challengeId"`
	AppID       int64     `json:"appid"`
	UserID      int64     `json:"userId"`
	SessionData []byte    `json:"sessionData"`
	ExpiresAt   time.Time `json:"expiresAt"`
}

type PasskeyLoginState struct {
	ChallengeID string    `json:"challengeId"`
	AppID       int64     `json:"appid"`
	SessionData []byte    `json:"sessionData"`
	ExpiresAt   time.Time `json:"expiresAt"`
}
