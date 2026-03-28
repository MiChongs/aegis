package app

import "time"

type App struct {
	ID                     int64          `json:"id"`
	Name                   string         `json:"name"`
	AppKey                 string         `json:"appKey,omitempty"`
	Status                 bool           `json:"status"`
	DisabledReason         string         `json:"disabledReason,omitempty"`
	RegisterStatus         bool           `json:"registerStatus"`
	DisabledRegisterReason string         `json:"disabledRegisterReason,omitempty"`
	LoginStatus            bool           `json:"loginStatus"`
	DisabledLoginReason    string         `json:"disabledLoginReason,omitempty"`
	Settings               map[string]any `json:"settings,omitempty"`
	CreatedAt              time.Time      `json:"createdAt"`
	UpdatedAt              time.Time      `json:"updatedAt"`
}

type AppMutation struct {
	ID                     int64
	Name                   *string
	AppKey                 *string
	Status                 *bool
	DisabledReason         *string
	RegisterStatus         *bool
	DisabledRegisterReason *string
	LoginStatus            *bool
	DisabledLoginReason    *string
	Settings               map[string]any
}

type Policy struct {
	LoginCheckDevice        bool `json:"loginCheckDevice"`
	LoginCheckUser          bool `json:"loginCheckUser"`
	LoginCheckIP            bool `json:"loginCheckIp"`
	LoginCheckDeviceTimeout int  `json:"loginCheckDeviceTimeOut"`
	MultiDeviceLogin        bool `json:"multiDeviceLogin"`
	MultiDeviceLimit        int  `json:"multiDeviceLimit"`
	RegisterCaptcha         bool `json:"registerCaptcha"`
	RegisterCaptchaTimeout  int  `json:"registerCaptchaTimeOut"`
	RegisterCheckIP         bool `json:"registerCheckIp"`
}

type TransportEncryptionPolicy struct {
	Enabled            bool   `json:"enabled"`
	Strict             bool   `json:"strict"`
	ResponseEncryption bool   `json:"responseEncryption"`
	Secret             string `json:"-"`
}

// TransportEncryptionView 加密配置视图（不含私钥）
type TransportEncryptionView struct {
	Enabled            bool     `json:"enabled"`
	Strict             bool     `json:"strict"`
	ResponseEncryption bool     `json:"responseEncryption"`
	HasSecret          bool     `json:"hasSecret"`
	SecretHint         string   `json:"secretHint,omitempty"`
	AllowedAlgorithms  []string `json:"allowedAlgorithms"`
	SupportedAlgorithms []string `json:"supportedAlgorithms"`
	HasRSAKey          bool     `json:"hasRSAKey"`
	RSAPublicKey       string   `json:"rsaPublicKey,omitempty"`
	HasECDHKey         bool     `json:"hasECDHKey"`
	ECDHPublicKey      string   `json:"ecdhPublicKey,omitempty"`
}

// TransportEncryptionUpdate 加密配置更新
type TransportEncryptionUpdate struct {
	Enabled            *bool    `json:"enabled"`
	Strict             *bool    `json:"strict"`
	ResponseEncryption *bool    `json:"responseEncryption"`
	Secret             *string  `json:"secret"`
	AllowedAlgorithms  []string `json:"allowedAlgorithms,omitempty"`
	GenerateRSAKey     bool     `json:"generateRSAKey,omitempty"`
	GenerateECDHKey    bool     `json:"generateECDHKey,omitempty"`
}

type Stats struct {
	AppID              int64 `json:"appid"`
	TotalUsers         int64 `json:"totalUsers"`
	EnabledUsers       int64 `json:"enabledUsers"`
	DisabledUsers      int64 `json:"disabledUsers"`
	BannerCount        int64 `json:"bannerCount"`
	NoticeCount        int64 `json:"noticeCount"`
	OAuthBindCount     int64 `json:"oauthBindCount"`
	NewUsersToday      int64 `json:"newUsersToday"`
	NewUsersLast7Days  int64 `json:"newUsersLast7Days"`
	NewUsersLast30Days int64 `json:"newUsersLast30Days"`
	LoginSuccessToday  int64 `json:"loginSuccessToday"`
	LoginFailureToday  int64 `json:"loginFailureToday"`
}

type UserTrendPoint struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

type UserTrend struct {
	AppID    int64            `json:"appid"`
	Days     int              `json:"days"`
	TotalNew int64            `json:"totalNew"`
	Series   []UserTrendPoint `json:"series"`
}

type LoginAuditQuery struct {
	Keyword string `json:"keyword"`
	Status  string `json:"status"`
	Page    int    `json:"page"`
	Limit   int    `json:"limit"`
}

type LoginAuditExportQuery struct {
	Keyword string `json:"keyword"`
	Status  string `json:"status"`
	Limit   int    `json:"limit"`
}

type LoginAuditItem struct {
	ID        int64          `json:"id"`
	UserID    *int64         `json:"userId,omitempty"`
	AppID     int64          `json:"appid"`
	Account   string         `json:"account,omitempty"`
	Nickname  string         `json:"nickname,omitempty"`
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
	Keyword   string `json:"keyword"`
	EventType string `json:"eventType"`
	Page      int    `json:"page"`
	Limit     int    `json:"limit"`
}

type SessionAuditExportQuery struct {
	Keyword   string `json:"keyword"`
	EventType string `json:"eventType"`
	Limit     int    `json:"limit"`
}

type SessionAuditItem struct {
	ID        int64          `json:"id"`
	UserID    *int64         `json:"userId,omitempty"`
	AppID     int64          `json:"appid"`
	Account   string         `json:"account,omitempty"`
	Nickname  string         `json:"nickname,omitempty"`
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

type RegionStatsQuery struct {
	Type  string `json:"type"`
	Limit int    `json:"limit"`
}

type RegionStatItem struct {
	Region     string `json:"region"`
	Code       string `json:"code,omitempty"`
	Parent     string `json:"parent,omitempty"`
	ParentPath string `json:"parentPath,omitempty"`
	Count      int64  `json:"count"`
}

type RegionStatsResult struct {
	AppID int64            `json:"appid"`
	Type  string           `json:"type"`
	Total int64            `json:"total"`
	Items []RegionStatItem `json:"items"`
}

type AuthSourceStatItem struct {
	Source string `json:"source"`
	Count  int64  `json:"count"`
}

type AuthSourceStats struct {
	AppID            int64                `json:"appid"`
	TotalUsers       int64                `json:"totalUsers"`
	PasswordUsers    int64                `json:"passwordUsers"`
	OAuthBoundUsers  int64                `json:"oauthBoundUsers"`
	ProviderBindings []AuthSourceStatItem `json:"providerBindings"`
}

type Banner struct {
	ID         int64      `json:"id"`
	Header     string     `json:"header,omitempty"`
	Title      string     `json:"title"`
	Content    string     `json:"content,omitempty"`
	URL        string     `json:"url,omitempty"`
	Type       string     `json:"type"`
	Position   int        `json:"position"`
	Status     bool       `json:"status"`
	StartTime  *time.Time `json:"startTime,omitempty"`
	EndTime    *time.Time `json:"endTime,omitempty"`
	ViewCount  int64      `json:"viewCount"`
	ClickCount int64      `json:"clickCount"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

type BannerMutation struct {
	ID        int64
	Header    *string
	Title     *string
	Content   *string
	URL       *string
	Type      *string
	Position  *int
	Status    *bool
	StartTime *time.Time
	EndTime   *time.Time
}

type Notice struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title,omitempty"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type NoticeMutation struct {
	ID      int64
	Title   *string
	Content *string
}

type PasswordPolicy struct {
	Name                string `json:"name"`
	Description         string `json:"description,omitempty"`
	MinLength           int    `json:"minLength"`
	MaxLength           int    `json:"maxLength"`
	RequireUppercase    bool   `json:"requireUppercase"`
	RequireLowercase    bool   `json:"requireLowercase"`
	RequireNumbers      bool   `json:"requireNumbers"`
	RequireSpecialChars bool   `json:"requireSpecialChars"`
	MinScore            int    `json:"minScore"`
	MaxAge              int    `json:"maxAge"`
	PreventReuse        int    `json:"preventReuse"`
	IsDefault           bool   `json:"isDefault"`
}

type PasswordPolicyStats struct {
	TotalUsers      int64 `json:"totalUsers"`
	PasswordUsers   int64 `json:"passwordUsers"`
	CompliantUsers  int64 `json:"compliantUsers"`
	ComplianceRate  int64 `json:"complianceRate"`
	NeedChangeUsers int64 `json:"needChangeUsers"`
	NeedChangeRate  int64 `json:"needChangeRate"`
}

type PasswordPolicyView struct {
	AppID   int64                `json:"appid"`
	AppName string               `json:"appName"`
	Policy  PasswordPolicy       `json:"policy"`
	Stats   *PasswordPolicyStats `json:"stats,omitempty"`
}

type PasswordStrengthDetails struct {
	Length            int      `json:"length"`
	HasLowercase      bool     `json:"hasLowercase"`
	HasUppercase      bool     `json:"hasUppercase"`
	HasNumbers        bool     `json:"hasNumbers"`
	HasSpecialChars   bool     `json:"hasSpecialChars"`
	HasCommonPatterns []string `json:"hasCommonPatterns"`
	Entropy           float64  `json:"entropy"`
}

type PasswordRecommendation struct {
	Type     string `json:"type"`
	Priority string `json:"priority"`
	Message  string `json:"message"`
}

type PasswordStrengthAnalysis struct {
	Score           int                      `json:"score"`
	Level           string                   `json:"level"`
	Feedback        []string                 `json:"feedback"`
	Details         PasswordStrengthDetails  `json:"details"`
	Recommendations []PasswordRecommendation `json:"recommendations"`
}

type PasswordPolicyCheck struct {
	IsValid    bool                     `json:"isValid"`
	Violations []string                 `json:"violations"`
	Analysis   PasswordStrengthAnalysis `json:"analysis"`
	Policy     PasswordPolicy           `json:"policy"`
}

type PasswordPolicyTestSummary struct {
	IsValid         bool                     `json:"isValid"`
	Score           int                      `json:"score"`
	Level           string                   `json:"level"`
	Violations      []string                 `json:"violations"`
	Recommendations []PasswordRecommendation `json:"recommendations"`
}

type PasswordPolicyTestResult struct {
	Password         string                    `json:"password"`
	Policy           PasswordPolicy            `json:"policy"`
	StrengthAnalysis PasswordStrengthAnalysis  `json:"strengthAnalysis"`
	PolicyCheck      PasswordPolicyCheck       `json:"policyCheck"`
	Result           PasswordPolicyTestSummary `json:"result"`
}

type Site struct {
	ID          int64          `json:"id"`
	AppID       int64          `json:"appid"`
	UserID      int64          `json:"userId"`
	Account     string         `json:"account,omitempty"`
	Nickname    string         `json:"nickname,omitempty"`
	Avatar      string         `json:"avatar,omitempty"`
	Header      string         `json:"header,omitempty"`
	Name        string         `json:"name"`
	URL         string         `json:"url"`
	Type        string         `json:"type"`
	Description string         `json:"description"`
	Category    string         `json:"category,omitempty"`
	Status      string         `json:"status"`
	AuditStatus string         `json:"audit_status"`
	AuditReason string         `json:"audit_reason,omitempty"`
	IsPinned    bool           `json:"is_pinned"`
	ViewCount   int64          `json:"view_count"`
	LikeCount   int64          `json:"like_count"`
	Extra       map[string]any `json:"extra,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

type SiteMutation struct {
	ID          int64
	AppID       int64
	UserID      int64
	Header      *string
	Name        *string
	URL         *string
	Type        *string
	Description *string
	Category    *string
}

type SiteListResult struct {
	List        []Site `json:"list"`
	Page        int    `json:"page"`
	Limit       int    `json:"limit"`
	Total       int64  `json:"total"`
	TotalPages  int    `json:"totalPages"`
	HasNextPage bool   `json:"hasNextPage"`
	HasPrevPage bool   `json:"hasPrevPage"`
	Cached      bool   `json:"cached,omitempty"`
}

type SiteListQuery struct {
	Page      int    `json:"page"`
	Limit     int    `json:"limit"`
	Keyword   string `json:"keyword"`
	SortBy    string `json:"sortBy"`
	SortOrder string `json:"sortOrder"`
	Category  string `json:"category"`
	Status    string `json:"status"`
}

type SiteAuditStats struct {
	AppID    int64            `json:"appid"`
	Total    int64            `json:"total"`
	ByStatus map[string]int64 `json:"byStatus"`
	Pending  int64            `json:"pending"`
	Approved int64            `json:"approved"`
	Rejected int64            `json:"rejected"`
}

type AppVersion struct {
	ID            int64          `json:"id"`
	AppID         int64          `json:"appid"`
	ChannelID     *int64         `json:"channel_id,omitempty"`
	ChannelName   string         `json:"channel_name,omitempty"`
	Version       string         `json:"version"`
	VersionCode   int64          `json:"version_code"`
	Description   string         `json:"description,omitempty"`
	ReleaseNotes  string         `json:"release_notes,omitempty"`
	DownloadURL   string         `json:"download_url,omitempty"`
	FileSize      int64          `json:"file_size"`
	FileHash      string         `json:"file_hash,omitempty"`
	ForceUpdate   bool           `json:"force_update"`
	UpdateType    string         `json:"update_type"`
	Platform      string         `json:"platform"`
	MinOSVersion  string         `json:"min_os_version,omitempty"`
	Status        string         `json:"status"`
	DownloadCount int64          `json:"download_count"`
	Metadata      map[string]any `json:"metadata,omitempty"`
	CreatedAt     time.Time      `json:"createdAt"`
	UpdatedAt     time.Time      `json:"updatedAt"`
}

type AppVersionMutation struct {
	ID           int64
	AppID        int64
	ChannelID    *int64
	Version      *string
	VersionCode  *int64
	Description  *string
	ReleaseNotes *string
	DownloadURL  *string
	FileSize     *int64
	FileHash     *string
	ForceUpdate  *bool
	UpdateType   *string
	Platform     *string
	MinOSVersion *string
	Status       *string
	Metadata     map[string]any
}

type AppVersionListQuery struct {
	Page      int    `json:"page"`
	Limit     int    `json:"limit"`
	Status    string `json:"status"`
	Platform  string `json:"platform"`
	ChannelID int64  `json:"channel_id"`
}

type AppVersionListResult struct {
	Items      []AppVersion `json:"items"`
	Page       int          `json:"page"`
	Limit      int          `json:"limit"`
	Total      int64        `json:"total"`
	TotalPages int          `json:"totalPages"`
}

type AppVersionChannel struct {
	ID             int64          `json:"id"`
	AppID          int64          `json:"appid"`
	Name           string         `json:"name"`
	Code           string         `json:"code"`
	Description    string         `json:"description,omitempty"`
	IsDefault      bool           `json:"is_default"`
	Status         bool           `json:"status"`
	Priority       int            `json:"priority"`
	Color          string         `json:"color,omitempty"`
	Level          string         `json:"level"`
	RolloutPct     int            `json:"rollout_pct"`
	Platforms      []string       `json:"platforms,omitempty"`
	MinVersionCode int64          `json:"min_version_code"`
	MaxVersionCode int64          `json:"max_version_code"`
	Rules          []ChannelRule  `json:"rules,omitempty"`
	TargetAudience map[string]any `json:"targetAudience,omitempty"`
	UserCount      int64          `json:"userCount,omitempty"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
}

// ChannelRule 灰度分发条件规则
type ChannelRule struct {
	Field string `json:"field"` // 匹配字段：platform / os_version / user_id / region / tag 等
	Op    string `json:"op"`    // 操作符：eq / neq / in / not_in / gt / lt / gte / lte / regex / contains
	Value any    `json:"value"` // 匹配值（字符串 / 数字 / 数组）
}

type AppVersionChannelMutation struct {
	ID             int64
	AppID          int64
	Name           *string
	Code           *string
	Description    *string
	IsDefault      *bool
	Status         *bool
	Priority       *int
	Color          *string
	Level          *string
	RolloutPct     *int
	Platforms      []string
	MinVersionCode *int64
	MaxVersionCode *int64
	Rules          []ChannelRule
	TargetAudience map[string]any
}

type AppVersionCheckResult struct {
	Version     *AppVersion `json:"version,omitempty"`
	ChannelName string      `json:"channelName,omitempty"`
}

type AppVersionStats struct {
	AppID          int64            `json:"appid"`
	TotalVersions  int64            `json:"totalVersions"`
	PublishedCount int64            `json:"publishedCount"`
	ChannelCount   int64            `json:"channelCount"`
	PlatformCounts map[string]int64 `json:"platformCounts"`
}
