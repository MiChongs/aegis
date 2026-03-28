package admin

import (
	securitydomain "aegis/internal/domain/security"
	"time"
)

type SecurityStatus struct {
	HasPassword      bool                          `json:"hasPassword"`
	TwoFactorEnabled bool                          `json:"twoFactorEnabled"`
	TwoFactorMethod  string                        `json:"twoFactorMethod,omitempty"`
	PasskeyEnabled   bool                          `json:"passkeyEnabled"`
	LastLoginAt      *time.Time                    `json:"lastLoginAt,omitempty"`
	TwoFactor        securitydomain.TOTPStatus     `json:"twoFactor"`
	RecoveryCodes    RecoveryCodeSummary           `json:"recoveryCodes"`
	Passkeys         securitydomain.PasskeySummary `json:"passkeys"`
	Modules          []securitydomain.ModuleStatus `json:"modules,omitempty"`
}

type TOTPSecretRecord struct {
	AdminID        int64      `json:"adminId"`
	SecretCipher   string     `json:"-"`
	Issuer         string     `json:"issuer"`
	AccountName    string     `json:"accountName"`
	Enabled        bool       `json:"enabled"`
	EnabledAt      *time.Time `json:"enabledAt,omitempty"`
	LastVerifiedAt *time.Time `json:"lastVerifiedAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type RecoveryCodeRecord struct {
	ID        int64      `json:"id"`
	AdminID   int64      `json:"adminId"`
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

type PasskeyRecord struct {
	ID             int64      `json:"id"`
	AdminID        int64      `json:"adminId"`
	CredentialID   []byte     `json:"-"`
	CredentialName string     `json:"credentialName,omitempty"`
	CredentialJSON []byte     `json:"-"`
	AAGUID         []byte     `json:"-"`
	SignCount      uint32     `json:"signCount"`
	LastUsedAt     *time.Time `json:"lastUsedAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}
