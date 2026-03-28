package user

import (
	securitydomain "aegis/internal/domain/security"
	"time"
)

type User struct {
	ID              int64      `json:"id"`
	AppID           int64      `json:"appid"`
	Account         string     `json:"account"`
	PasswordHash    string     `json:"-"`
	Integral        int64      `json:"integral"`
	Experience      int64      `json:"experience"`
	Enabled         bool       `json:"enabled"`
	DisabledEndTime *time.Time `json:"disabledEndTime,omitempty"`
	VIPExpireAt     *time.Time `json:"vipExpireAt,omitempty"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

// ContactInfo 多平台联系方式
type ContactInfo struct {
	Platform string `json:"platform"`
	Value    string `json:"value"`
	Label    string `json:"label,omitempty"`
}

type Profile struct {
	UserID    int64          `json:"userId"`
	Nickname  string         `json:"nickname,omitempty"`
	Avatar    string         `json:"avatar,omitempty"`
	Email     string         `json:"email,omitempty"`
	Phone     string         `json:"phone,omitempty"`
	Birthday  *time.Time     `json:"birthday,omitempty"`
	Bio       string         `json:"bio,omitempty"`
	Contacts  []ContactInfo  `json:"contacts,omitempty"`
	Extra     map[string]any `json:"extra,omitempty"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

type Settings struct {
	UserID    int64          `json:"userId"`
	Category  string         `json:"category"`
	Settings  map[string]any `json:"settings"`
	Version   int            `json:"version"`
	IsActive  bool           `json:"isActive"`
	CreatedAt time.Time      `json:"createdAt,omitempty"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

type SettingRecord struct {
	ID        int64          `json:"id"`
	UserID    int64          `json:"userId"`
	AppID     int64          `json:"appid"`
	Category  string         `json:"category"`
	Settings  map[string]any `json:"settings"`
	Version   int            `json:"version"`
	IsActive  bool           `json:"isActive"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

type SettingCoverageStat struct {
	UsersWithSettings    int64   `json:"usersWithSettings"`
	UsersWithoutSettings int64   `json:"usersWithoutSettings"`
	Coverage             float64 `json:"coverage"`
}

type RecentSettingRecord struct {
	UserID    int64     `json:"userId"`
	Category  string    `json:"category"`
	CreatedAt time.Time `json:"createdAt"`
	Version   int       `json:"version"`
}

type AdminSettingsStatsSummary struct {
	TotalCategories int     `json:"totalCategories"`
	AvgCoverage     float64 `json:"avgCoverage"`
}

type AdminSettingsStatsResult struct {
	AppID          int64                          `json:"appid"`
	TotalUsers     int64                          `json:"totalUsers"`
	SettingsStats  map[string]SettingCoverageStat `json:"settingsStats"`
	RecentSettings []RecentSettingRecord          `json:"recentSettings"`
	Categories     []string                       `json:"categories"`
	Summary        AdminSettingsStatsSummary      `json:"summary"`
}

type AdminUserSettingRecordInfo struct {
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	IsActive  bool      `json:"isActive"`
}

type AdminUserBasic struct {
	ID       int64  `json:"id"`
	Account  string `json:"account"`
	Nickname string `json:"nickname,omitempty"`
	Avatar   string `json:"avatar,omitempty"`
	Email    string `json:"email,omitempty"`
}

type AdminUserSettingsView struct {
	User                 AdminUserBasic                        `json:"user"`
	Settings             map[string]Settings                   `json:"settings"`
	RecordInfo           map[string]AdminUserSettingRecordInfo `json:"recordInfo"`
	Categories           []string                              `json:"categories"`
	ConfiguredCategories []string                              `json:"configuredCategories"`
	MissingCategories    []string                              `json:"missingCategories"`
}

type SettingsInitializeResult struct {
	AppID                 int64    `json:"appid"`
	UserIDs               []int64  `json:"userIds"`
	Categories            []string `json:"categories"`
	ProcessedUsers        int      `json:"processedUsers"`
	InitializedCategories int      `json:"initializedCategories"`
	SkippedExisting       int      `json:"skippedExisting"`
}

type SettingIntegrityIssue struct {
	UserID      int64    `json:"userId"`
	Category    string   `json:"category"`
	MissingKeys []string `json:"missingKeys"`
	SettingID   int64    `json:"settingId"`
}

type SettingIntegrityRepair struct {
	UserID       int64    `json:"userId"`
	Category     string   `json:"category"`
	RepairedKeys []string `json:"repairedKeys"`
}

type SettingsIntegrityResult struct {
	AppID       int64                    `json:"appid"`
	TotalIssues int                      `json:"totalIssues"`
	Issues      []SettingIntegrityIssue  `json:"issues"`
	Repairs     []SettingIntegrityRepair `json:"repairs"`
	AutoRepair  bool                     `json:"autoRepair"`
}

type InvalidSettingRecord struct {
	ID       int64  `json:"id"`
	UserID   int64  `json:"userId"`
	Category string `json:"category"`
	IsActive bool   `json:"isActive"`
}

type SettingsCleanupResult struct {
	AppID           int64                  `json:"appid"`
	FoundInvalid    int                    `json:"foundInvalid"`
	Cleaned         int64                  `json:"cleaned"`
	DryRun          bool                   `json:"dryRun"`
	InvalidSettings []InvalidSettingRecord `json:"invalidSettings"`
}

type SecurityStatus struct {
	HasPassword            bool                               `json:"hasPassword"`
	TwoFactorEnabled       bool                               `json:"twoFactorEnabled"`
	TwoFactorMethod        string                             `json:"twoFactorMethod,omitempty"`
	PasskeyEnabled         bool                               `json:"passkeyEnabled"`
	PasswordStrengthScore  int                                `json:"passwordStrengthScore"`
	PasswordChangeRequired bool                               `json:"passwordChangeRequired"`
	PasswordChangedAt      *time.Time                         `json:"passwordChangedAt,omitempty"`
	PasswordExpiresAt      *time.Time                         `json:"passwordExpiresAt,omitempty"`
	OAuth2Bindings         int                                `json:"oauth2Bindings"`
	OAuth2Providers        []string                           `json:"oauth2Providers,omitempty"`
	TwoFactor              securitydomain.TOTPStatus          `json:"twoFactor"`
	RecoveryCodes          securitydomain.RecoveryCodeSummary `json:"recoveryCodes"`
	Passkeys               securitydomain.PasskeySummary      `json:"passkeys"`
	Modules                []securitydomain.ModuleStatus      `json:"modules,omitempty"`
}

type SessionView struct {
	TokenHash string     `json:"tokenHash"`
	Current   bool       `json:"current"`
	Account   string     `json:"account"`
	Provider  string     `json:"provider,omitempty"`
	DeviceID  string     `json:"deviceId,omitempty"`
	IP        string     `json:"ip,omitempty"`
	UserAgent string     `json:"userAgent,omitempty"`
	IssuedAt  time.Time  `json:"issuedAt"`
	ExpiresAt time.Time  `json:"expiresAt"`
	LastSeen  *time.Time `json:"lastSeen,omitempty"`
}

type SessionListResult struct {
	Items []SessionView `json:"items"`
	Total int           `json:"total"`
}

// SessionDetailView 管理员查看的会话详情（含位置信息）
type SessionDetailView struct {
	TokenHash   string    `json:"tokenHash"`
	TokenID     string    `json:"tokenId"`
	Account     string    `json:"account"`
	DeviceID    string    `json:"deviceId,omitempty"`
	IP          string    `json:"ip"`
	UserAgent   string    `json:"userAgent"`
	Provider    string    `json:"provider,omitempty"`
	IssuedAt    time.Time `json:"issuedAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
	Country     string    `json:"country,omitempty"`
	CountryCode string    `json:"countryCode,omitempty"`
	Region      string    `json:"region,omitempty"`
	City        string    `json:"city,omitempty"`
	ISP         string    `json:"isp,omitempty"`
	Location    string    `json:"location,omitempty"`
}

type SessionRevokeResult struct {
	AppID         int64    `json:"appid"`
	UserID        int64    `json:"userId"`
	Revoked       int      `json:"revoked"`
	RevokedTokens []string `json:"revokedTokens"`
	CurrentKilled bool     `json:"currentKilled"`
}

type LoginAuditQuery struct {
	Status string `json:"status"`
	Page   int    `json:"page"`
	Limit  int    `json:"limit"`
}

type LoginAuditExportQuery struct {
	Status string `json:"status"`
	Limit  int    `json:"limit"`
}

type LoginAuditItem struct {
	ID        int64          `json:"id"`
	AppID     int64          `json:"appid"`
	LoginType string         `json:"loginType"`
	Provider  string         `json:"provider,omitempty"`
	TokenJTI  string         `json:"tokenJti,omitempty"`
	LoginIP   string         `json:"loginIp,omitempty"`
	DeviceID  string         `json:"deviceId,omitempty"`
	UserAgent string         `json:"userAgent,omitempty"`
	Status    string         `json:"status"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}

type LoginAuditListResult struct {
	Items      []LoginAuditItem `json:"items"`
	Page       int              `json:"page"`
	Limit      int              `json:"limit"`
	Total      int64            `json:"total"`
	TotalPages int              `json:"totalPages"`
}

type SessionAuditQuery struct {
	EventType string `json:"eventType"`
	Page      int    `json:"page"`
	Limit     int    `json:"limit"`
}

type SessionAuditExportQuery struct {
	EventType string `json:"eventType"`
	Limit     int    `json:"limit"`
}

type SessionAuditItem struct {
	ID        int64          `json:"id"`
	AppID     int64          `json:"appid"`
	TokenJTI  string         `json:"tokenJti,omitempty"`
	EventType string         `json:"eventType"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}

type SessionAuditListResult struct {
	Items      []SessionAuditItem `json:"items"`
	Page       int                `json:"page"`
	Limit      int                `json:"limit"`
	Total      int64              `json:"total"`
	TotalPages int                `json:"totalPages"`
}

type RoleDefinition struct {
	ID          int64          `json:"id"`
	AppID       int64          `json:"appid"`
	RoleKey     string         `json:"roleKey"`
	RoleName    string         `json:"roleName"`
	Description string         `json:"description,omitempty"`
	Priority    int            `json:"priority"`
	IsEnabled   bool           `json:"isEnabled"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

type RoleApplication struct {
	ID             int64          `json:"id"`
	AppID          int64          `json:"appid"`
	UserID         int64          `json:"userId"`
	Account        string         `json:"account,omitempty"`
	Nickname       string         `json:"nickname,omitempty"`
	Avatar         string         `json:"avatar,omitempty"`
	RequestedRole  string         `json:"requestedRole"`
	CurrentRole    string         `json:"currentRole"`
	Reason         string         `json:"reason"`
	Status         string         `json:"status"`
	Priority       string         `json:"priority"`
	ValidDays      int            `json:"validDays"`
	ReviewReason   string         `json:"reviewReason,omitempty"`
	ReviewedBy     *int64         `json:"reviewedBy,omitempty"`
	ReviewedByName string         `json:"reviewedByName,omitempty"`
	ReviewedAt     *time.Time     `json:"reviewedAt,omitempty"`
	CancelledAt    *time.Time     `json:"cancelledAt,omitempty"`
	DeviceInfo     map[string]any `json:"deviceInfo,omitempty"`
	Extra          map[string]any `json:"extra,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

type RoleApplicationListQuery struct {
	Page          int    `json:"page"`
	Limit         int    `json:"limit"`
	Status        string `json:"status"`
	RequestedRole string `json:"requestedRole"`
	Priority      string `json:"priority"`
	Keyword       string `json:"keyword"`
	SortBy        string `json:"sortBy"`
	SortOrder     string `json:"sortOrder"`
}

type RoleApplicationListResult struct {
	Items      []RoleApplication `json:"items"`
	Page       int               `json:"page"`
	Limit      int               `json:"limit"`
	Total      int64             `json:"total"`
	TotalPages int               `json:"totalPages"`
}

type RoleApplicationStatistics struct {
	AppID      int64            `json:"appid"`
	Total      int64            `json:"total"`
	Pending    int64            `json:"pending"`
	Approved   int64            `json:"approved"`
	Rejected   int64            `json:"rejected"`
	Cancelled  int64            `json:"cancelled"`
	ByRole     map[string]int64 `json:"byRole"`
	ByPriority map[string]int64 `json:"byPriority"`
}

type ProfileUpdate struct {
	Nickname string        `json:"nickname"`
	Avatar   string        `json:"avatar"`
	Email    string        `json:"email"`
	Phone    string        `json:"phone"`
	Birthday string        `json:"birthday"` // "2000-01-15" 或 ""
	Bio      string        `json:"bio"`
	Contacts []ContactInfo `json:"contacts"`
}

type PendingProfileChange struct {
	Field       string    `json:"field"`
	Value       string    `json:"value"`
	MaskedValue string    `json:"maskedValue,omitempty"`
	Purpose     string    `json:"purpose"`
	ExpiresAt   time.Time `json:"expiresAt"`
	RequestedAt time.Time `json:"requestedAt"`
}

type ProfileUpdateResult struct {
	Profile        *Profile               `json:"profile"`
	PendingChanges []PendingProfileChange `json:"pendingChanges,omitempty"`
}

type AdminUserQuery struct {
	Keyword     string     `json:"keyword"`
	Account     string     `json:"account"`
	Nickname    string     `json:"nickname"`
	Email       string     `json:"email"`
	Phone       string     `json:"phone"`
	RegisterIP  string     `json:"registerIp"`
	UserID      *int64     `json:"userId,omitempty"`
	Enabled     *bool      `json:"enabled,omitempty"`
	CreatedFrom *time.Time `json:"createdFrom,omitempty"`
	CreatedTo   *time.Time `json:"createdTo,omitempty"`
	Page        int        `json:"page"`
	Limit       int        `json:"limit"`
}

type AdminUserStatusMutation struct {
	Enabled              *bool      `json:"enabled,omitempty"`
	DisabledEndTime      *time.Time `json:"disabledEndTime,omitempty"`
	ClearDisabledEndTime bool       `json:"clearDisabledEndTime"`
	DisabledReason       *string    `json:"disabledReason,omitempty"`
}

type AdminUserBatchStatusMutation struct {
	UserIDs []int64 `json:"userIds"`
	AdminUserStatusMutation
}

type AdminUserBatchStatusResult struct {
	AppID            int64   `json:"appid"`
	Requested        int     `json:"requested"`
	Updated          int64   `json:"updated"`
	ProcessedUserIDs []int64 `json:"processedUserIds"`
}

type AdminUserView struct {
	ID               int64          `json:"id"`
	AppID            int64          `json:"appid"`
	Account          string         `json:"account"`
	Nickname         string         `json:"nickname,omitempty"`
	Avatar           string         `json:"avatar,omitempty"`
	Email            string         `json:"email,omitempty"`
	Phone            string         `json:"phone,omitempty"`
	Integral         int64          `json:"integral"`
	Experience       int64          `json:"experience"`
	Enabled          bool           `json:"enabled"`
	DisabledEndTime  *time.Time     `json:"disabledEndTime,omitempty"`
	VIPExpireAt      *time.Time     `json:"vipExpireAt,omitempty"`
	CreatedAt        time.Time      `json:"createdAt"`
	UpdatedAt        time.Time      `json:"updatedAt"`
	RegisterIP       string         `json:"registerIp,omitempty"`
	RegisterTime     *time.Time     `json:"registerTime,omitempty"`
	RegisterProvince string         `json:"registerProvince,omitempty"`
	RegisterCity     string         `json:"registerCity,omitempty"`
	RegisterISP      string         `json:"registerIsp,omitempty"`
	DisabledReason   string         `json:"disabledReason,omitempty"`
	MarkCode         string         `json:"markcode,omitempty"`
	Extra            map[string]any `json:"extra,omitempty"`
}

type AdminUserListResult struct {
	Items      []AdminUserView `json:"items"`
	Page       int             `json:"page"`
	Limit      int             `json:"limit"`
	Total      int64           `json:"total"`
	TotalPages int             `json:"totalPages"`
}

type AdminUserSearchSource struct {
	UserID          int64     `json:"userId"`
	AppID           int64     `json:"appid"`
	Account         string    `json:"account"`
	Nickname        string    `json:"nickname"`
	Email           string    `json:"email"`
	Phone           string    `json:"phone"`
	RegisterIP      string    `json:"registerIp"`
	Enabled         bool      `json:"enabled"`
	CreatedAt       time.Time `json:"createdAt"`
	SourceUpdatedAt time.Time `json:"sourceUpdatedAt"`
}

type AutoSignCandidate struct {
	UserID                  int64      `json:"userId"`
	AppID                   int64      `json:"appid"`
	Account                 string     `json:"account"`
	VIPExpireAt             *time.Time `json:"vipExpireAt,omitempty"`
	Enabled                 bool       `json:"enabled"`
	SettingsEnabled         bool       `json:"settingsEnabled"`
	Time                    string     `json:"time"`
	RetryOnFail             bool       `json:"retryOnFail"`
	MaxRetries              int        `json:"maxRetries"`
	NotifyOnSuccess         bool       `json:"notifyOnSuccess"`
	NotifyOnFail            bool       `json:"notifyOnFail"`
	DisableLocationTracking bool       `json:"disableLocationTracking"`
}

type MyView struct {
	ID                  int64      `json:"id"`
	AppID               int64      `json:"appid"`
	Account             string     `json:"account"`
	Integral            int64      `json:"integral"`
	Experience          int64      `json:"experience"`
	UnreadNotifications int64      `json:"unreadNotifications"`
	Nickname            string     `json:"nickname,omitempty"`
	Avatar              string     `json:"avatar,omitempty"`
	Email               string     `json:"email,omitempty"`
	Enabled             bool       `json:"enabled"`
	VIPExpireAt         *time.Time `json:"vipExpireAt,omitempty"`
	IsVIP               bool       `json:"isVip"`
	TokenSource         string     `json:"tokenSource,omitempty"`
	LastLoginIP         string     `json:"lastLoginIp,omitempty"`
	LastDeviceID        string     `json:"lastDeviceId,omitempty"`
}

type SignInReward struct {
	BaseIntegral     int64   `json:"baseIntegral"`
	IntegralReward   int64   `json:"integralReward"`
	ExperienceReward int64   `json:"experienceReward"`
	RewardMultiplier float64 `json:"rewardMultiplier"`
	BonusType        string  `json:"bonusType,omitempty"`
	BonusDescription string  `json:"bonusDescription,omitempty"`
}

type SignInStatus struct {
	TodaySigned     bool         `json:"todaySigned"`
	SignDate        string       `json:"signDate"`
	ConsecutiveDays int          `json:"consecutiveDays"`
	TotalSignIns    int64        `json:"totalSignIns"`
	TotalIntegral   int64        `json:"totalIntegral"`
	TotalExperience int64        `json:"totalExperience"`
	LastSignAt      *time.Time   `json:"lastSignAt,omitempty"`
	LastSignedDate  string       `json:"lastSignedDate,omitempty"`
	CurrentReward   SignInReward `json:"currentReward"`
}

type DailySignIn struct {
	ID               int64     `json:"id"`
	UserID           int64     `json:"userId"`
	AppID            int64     `json:"appid"`
	SignedAt         time.Time `json:"signedAt"`
	SignDate         string    `json:"signDate"`
	IntegralReward   int64     `json:"integralReward"`
	ExperienceReward int64     `json:"experienceReward"`
	IntegralBefore   int64     `json:"integralBefore"`
	IntegralAfter    int64     `json:"integralAfter"`
	ExperienceBefore int64     `json:"experienceBefore"`
	ExperienceAfter  int64     `json:"experienceAfter"`
	ConsecutiveDays  int       `json:"consecutiveDays"`
	RewardMultiplier float64   `json:"rewardMultiplier"`
	BonusType        string    `json:"bonusType,omitempty"`
	BonusDescription string    `json:"bonusDescription,omitempty"`
	SignInSource     string    `json:"signInSource"`
	DeviceInfo       string    `json:"deviceInfo,omitempty"`
	IPAddress        string    `json:"ipAddress,omitempty"`
	Location         string    `json:"location,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
}

type SignStats struct {
	UserID                int64      `json:"userId"`
	AppID                 int64      `json:"appid"`
	LastSignDate          string     `json:"lastSignDate,omitempty"`
	LastSignAt            *time.Time `json:"lastSignAt,omitempty"`
	ConsecutiveDays       int        `json:"consecutiveDays"`
	TotalSignDays         int64      `json:"totalSignDays"`
	TotalIntegralReward   int64      `json:"totalIntegralReward"`
	TotalExperienceReward int64      `json:"totalExperienceReward"`
	UpdatedAt             time.Time  `json:"updatedAt"`
}

type SignInResult struct {
	Record        DailySignIn  `json:"record"`
	Reward        SignInReward `json:"reward"`
	TotalSignIns  int64        `json:"totalSignIns"`
	AlreadySigned bool         `json:"alreadySigned,omitempty"`
}

type SignHistoryResult struct {
	Items      []DailySignIn `json:"items"`
	Page       int           `json:"page"`
	Limit      int           `json:"limit"`
	Total      int64         `json:"total"`
	TotalPages int           `json:"totalPages"`
}

type SignHistoryExportQuery struct {
	Limit int `json:"limit"`
}
