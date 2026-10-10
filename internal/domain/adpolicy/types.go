// Package adpolicy 广告服务策略：用户是否同意广告服务、同意与否决定的服务范围，以及开屏广告的展示记录。
//
// 同意是账号上的事实：拒绝或尚未选择的用户只能使用应用配置的基础服务（见 Evaluate）。
// 未登录用户的选择由客户端记在本机，登录后以账号上的为准，账号上还没有时再把本机的同步上来
// （SaveConsentInput.IfAbsent）。
package adpolicy

import "time"

// 服务模式。
const (
	// ModeFull 完整服务。
	ModeFull = "full"
	// ModeBasic 只提供基础服务：只有 BasicTools 里的功能可用。
	ModeBasic = "basic"
)

// 选择的来源。
const (
	SourceApp = "app"
	SourceWeb = "web"
	// SourceGuestSync 未登录时在本机做的选择，登录后同步到账号上。
	SourceGuestSync = "guest_sync"
)

// 开屏广告一次尝试的结局。
const (
	SplashShown   = "shown"
	SplashClicked = "clicked"
	SplashFailed  = "failed"
	SplashTimeout = "timeout"
)

// 规模上界。
const (
	MaxBasicTools            = 200
	MaxSplashIntervalSeconds = 86400
	MaxSplashDailyLimit      = 100
	MaxSplashBatch           = 50
	MaxConsentVersion        = 1_000_000
	// SplashEventMaxAge 离线攒下的开屏记录最多补报多久以前的。
	SplashEventMaxAge = 7 * 24 * time.Hour
)

// SplashConfig 开屏广告配置。
type SplashConfig struct {
	Enabled bool `json:"enabled"`
	// PlacementID 平台上的开屏广告位，留空时客户端使用包里内置的那一个。
	PlacementID string `json:"placementId"`
	// MinIntervalSeconds 两次开屏之间至少间隔多久，0 表示不限。
	MinIntervalSeconds int `json:"minIntervalSeconds"`
	// DailyLimit 每人每天最多展示几次，0 表示不限。
	DailyLimit int `json:"dailyLimit"`
}

// Policy 一个应用的广告服务策略。
type Policy struct {
	AppID int64 `json:"appid"`
	// Enabled 要求同意广告服务：开启后，拒绝或尚未选择的用户只能使用基础服务。
	Enabled bool `json:"enabled"`
	// ConsentVersion 条款版本。调高后所有人都要按新版本重新选择一次。
	ConsentVersion int          `json:"consentVersion"`
	PolicyURL      string       `json:"policyUrl"`
	VipExempt      bool         `json:"vipExempt"`
	BasicTools     []string     `json:"basicTools"`
	Splash         SplashConfig `json:"splash"`
	UpdatedBy      string       `json:"updatedBy,omitempty"`
	UpdatedAt      time.Time    `json:"updatedAt"`
}

// DefaultPolicy 应用没配过时管理端看到的初始值。
func DefaultPolicy(appID int64) Policy {
	return Policy{
		AppID:          appID,
		ConsentVersion: 1,
		VipExempt:      true,
		BasicTools:     []string{},
		Splash:         SplashConfig{Enabled: true},
	}
}

// AdminPolicy 管理端看到的策略。
type AdminPolicy struct {
	Policy
	// Configured 是否保存过。没保存过时客户端按自己内置的默认策略处理。
	Configured bool `json:"configured"`
}

// SavePolicyInput 保存策略。
type SavePolicyInput struct {
	AppID          int64
	Enabled        bool
	ConsentVersion int
	PolicyURL      string
	VipExempt      bool
	BasicTools     []string
	Splash         SplashConfig
	Operator       string
}

// Consent 一个用户当前的选择。
type Consent struct {
	UserID    int64     `json:"userId,omitempty"`
	Account   string    `json:"account,omitempty"`
	Accepted  bool      `json:"accepted"`
	Version   int       `json:"version"`
	Source    string    `json:"source"`
	DeviceID  string    `json:"deviceId,omitempty"`
	ClientIP  string    `json:"clientIp,omitempty"`
	DecidedAt time.Time `json:"decidedAt"`
	// Outdated 选择所依据的条款已经不是当前版本（只在管理端列表里填）。
	Outdated bool `json:"outdated,omitempty"`
}

// SaveConsentInput 记录一次选择。
type SaveConsentInput struct {
	AppID    int64
	UserID   int64
	Accepted bool
	// Version 用户看到的条款版本，0 表示当前版本。
	Version int
	// IfAbsent 账号上已经有选择时不覆盖，原样返回已有的那一条（未登录选择的登录后同步）。
	IfAbsent bool
	Source   string
	DeviceID string
	ClientIP string
	Now      time.Time
}

// ConsentLog 选择的一次变更。
type ConsentLog struct {
	ID        int64     `json:"id"`
	UserID    int64     `json:"userId"`
	Account   string    `json:"account,omitempty"`
	Accepted  bool      `json:"accepted"`
	Version   int       `json:"version"`
	Source    string    `json:"source"`
	DeviceID  string    `json:"deviceId,omitempty"`
	ClientIP  string    `json:"clientIp,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// ConsentQuery 管理端按当前选择筛用户。
type ConsentQuery struct {
	AppID int64
	// Accepted 为空表示不按选择筛。
	Accepted *bool
	// Outdated 只看按旧版本条款做的选择。
	Outdated       bool
	CurrentVersion int
	Source         string
	Keyword        string
	Page           int
	Limit          int
}

// ConsentLogQuery 变更历史筛选。
type ConsentLogQuery struct {
	AppID   int64
	UserID  int64
	Keyword string
	Page    int
	Limit   int
}

// SplashEvent 一次开屏尝试。
type SplashEvent struct {
	ID           int64     `json:"id"`
	AppID        int64     `json:"appid"`
	UserID       int64     `json:"userId,omitempty"`
	Account      string    `json:"account,omitempty"`
	EventID      string    `json:"eventId"`
	DeviceID     string    `json:"deviceId,omitempty"`
	PlacementID  string    `json:"placementId"`
	Status       string    `json:"status"`
	ErrorCode    string    `json:"errorCode,omitempty"`
	ErrorMessage string    `json:"errorMessage,omitempty"`
	LoadMs       int       `json:"loadMs"`
	ShownMs      int       `json:"shownMs"`
	ClientIP     string    `json:"clientIp,omitempty"`
	OccurredAt   time.Time `json:"occurredAt"`
	CreatedAt    time.Time `json:"createdAt"`
}

// SplashEventInput 客户端上报的一条开屏记录。
type SplashEventInput struct {
	EventID      string
	PlacementID  string
	Status       string
	ErrorCode    string
	ErrorMessage string
	LoadMs       int
	ShownMs      int
	OccurredAt   *time.Time
}

// SplashReportInput 一批开屏记录。登录用户挂在账号上，未登录时 UserID 为 0。
type SplashReportInput struct {
	AppID    int64
	UserID   int64
	DeviceID string
	ClientIP string
	Events   []SplashEventInput
	Now      time.Time
}

// SplashReportResult 上报结果。重复的 eventId 不算新增。
type SplashReportResult struct {
	Received int `json:"received"`
	Recorded int `json:"recorded"`
}

// SplashQuery 开屏记录筛选。
type SplashQuery struct {
	AppID   int64
	UserID  int64
	Status  string
	Keyword string
	Start   *time.Time
	End     *time.Time
	Page    int
	Limit   int
}

// SplashUsage 一个人（登录用户按账号，未登录按设备）今天的开屏展示情况。
type SplashUsage struct {
	TodayCount  int
	LastShownAt *time.Time
}

// SplashSummary 一个用户的开屏广告累计情况。
type SplashSummary struct {
	Shown       int64      `json:"shown"`
	Clicked     int64      `json:"clicked"`
	Failed      int64      `json:"failed"`
	TodayShown  int64      `json:"todayShown"`
	LastShownAt *time.Time `json:"lastShownAt,omitempty"`
}

// Page 分页结果。
type Page[T any] struct {
	Items      []T   `json:"items"`
	Total      int64 `json:"total"`
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	TotalPages int   `json:"totalPages"`
}

// ClientSplash 客户端看到的开屏配置与这个人的频控结论。
type ClientSplash struct {
	SplashConfig
	TodayCount  int        `json:"todayCount"`
	LastShownAt *time.Time `json:"lastShownAt,omitempty"`
	// NextAvailableAt 间隔未到时，下一次最早什么时候可以展示。
	NextAvailableAt *time.Time `json:"nextAvailableAt,omitempty"`
	// Available 现在能不能展示：开着、没被会员免除、同意了广告服务、没超过频控。
	Available bool `json:"available"`
}

// ClientPolicy 客户端拉取的广告服务策略，以及当前用户的选择与服务模式。
type ClientPolicy struct {
	// Configured 应用是否配过策略。为 false 时其余策略字段是默认值，客户端应按内置的默认策略处理。
	Configured     bool         `json:"configured"`
	Enabled        bool         `json:"enabled"`
	ConsentVersion int          `json:"consentVersion"`
	PolicyURL      string       `json:"policyUrl"`
	VipExempt      bool         `json:"vipExempt"`
	BasicTools     []string     `json:"basicTools"`
	Splash         ClientSplash `json:"splash"`

	SignedIn bool `json:"signedIn"`
	// Consent 账号上的选择；未登录或还没选过时为空。
	Consent *Consent `json:"consent,omitempty"`
	Vip     bool     `json:"vip"`
	// Exempt 会员免除：不受同意要求约束，也不展示开屏广告。
	Exempt bool `json:"exempt"`
	// DecisionRequired 需要（重新）选择：没选过，或选择所依据的条款版本已过期。
	DecisionRequired bool `json:"decisionRequired"`
	// Mode 当前账号的服务模式。未登录时总是按「还没选择」回答，客户端应以本机的选择为准。
	Mode string `json:"mode"`
	// AdUserID 传给广告 SDK 的用户标识（与激励广告同一个带签名的标识），未登录时为空。
	AdUserID   string    `json:"adUserId,omitempty"`
	ServerTime time.Time `json:"serverTime"`
}

// UserProfile 管理端看一个用户的广告服务情况。
type UserProfile struct {
	Configured       bool          `json:"configured"`
	Enabled          bool          `json:"enabled"`
	ConsentVersion   int           `json:"consentVersion"`
	Consent          *Consent      `json:"consent,omitempty"`
	Vip              bool          `json:"vip"`
	Exempt           bool          `json:"exempt"`
	DecisionRequired bool          `json:"decisionRequired"`
	Mode             string        `json:"mode"`
	Logs             []ConsentLog  `json:"logs"`
	Splash           SplashSummary `json:"splash"`
	RecentSplash     []SplashEvent `json:"recentSplash"`
}

// StatsDay 一天的统计。
type StatsDay struct {
	Date        string `json:"date"`
	Shown       int64  `json:"shown"`
	Clicked     int64  `json:"clicked"`
	Failed      int64  `json:"failed"`
	SplashUsers int64  `json:"splashUsers"`
	Accepted    int64  `json:"accepted"`
	Declined    int64  `json:"declined"`
}

// StatsSummary 概览数字。
type StatsSummary struct {
	// 当前选择的分布（按账号）
	Accepted int64 `json:"accepted"`
	Declined int64 `json:"declined"`
	Outdated int64 `json:"outdated"`
	// 今天的开屏
	TodayShown   int64 `json:"todayShown"`
	TodayClicked int64 `json:"todayClicked"`
	TodayFailed  int64 `json:"todayFailed"`
	TodayUsers   int64 `json:"todayUsers"`
}

// Stats 管理端概览。
type Stats struct {
	Days    int          `json:"days"`
	Summary StatsSummary `json:"summary"`
	Trend   []StatsDay   `json:"trend"`
}
