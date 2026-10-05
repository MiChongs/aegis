// Package rewardedad 应用级激励广告：看完一段激励视频，按场景发放权益。
//
// 一次观看要由两方各确认一次才发奖（见 VerifyDual）：广告平台的服务端回调证明观看真实发生，
// 客户端上报说明它是为哪个场景、哪个账号看的。两者靠平台下发的 trans_id 对上。
package rewardedad

import (
	"time"

	cardkeydomain "aegis/internal/domain/cardkey"
)

// 广告平台。
const (
	// ProviderHuijing 灰鲸（huimiaokeji）聚合广告，底层是 ToBid / WindMill。
	ProviderHuijing = "huijing"
)

// 校验模式：一次观看在什么条件下算数。
const (
	// VerifyDual 服务端回调与客户端上报都到齐才发奖（默认，推荐）。
	VerifyDual = "dual"
	// VerifyServer 只要服务端回调验签通过就发奖；场景取客户端上报或广告位对应的第一个场景。
	VerifyServer = "server"
	// VerifyClient 只凭客户端上报发奖。任何拿到用户令牌的人都能伪造观看，只用于联调。
	VerifyClient = "client"
)

// 观看记录的状态。
const (
	StatusPending  = "pending"
	StatusGranted  = "granted"
	StatusRejected = "rejected"
)

// 观看记录的拒发原因。客户端按 reason 分支，不要匹配文案。
const (
	ReasonDisabled         = "disabled"          // 应用关闭了激励广告
	ReasonSceneUnavailable = "scene_unavailable" // 场景不存在或已停用
	ReasonDailyLimit       = "daily_limit"       // 今日所有场景合计次数已满
	ReasonSceneDailyLimit  = "scene_daily_limit" // 今日该场景次数已满
	ReasonCooldown         = "cooldown"          // 距离上次领取太近
	ReasonUserMismatch     = "user_mismatch"     // 回调与上报不是同一个账号
	ReasonClientUnverified = "client_unverified" // client 模式下 SDK 自己判定不发奖
	ReasonUserNotFound     = "user_not_found"    // 回调里的用户不存在
)

// 场景与配置的规模上界。
const (
	MaxScenes          = 20
	MaxDailyLimit      = 1000
	MaxCooldownSeconds = 86400
)

// 激励广告可发的权益。复用卡密的权益目录与数据形态（同一套账本、同一套控制台表单），
// 但只开放适合「看一次广告」的几档：余额是真钱、设备位挂在授权卡上，都不该由广告发。
var allowedRewardTypes = []string{
	cardkeydomain.RewardVipPlan,
	cardkeydomain.RewardVipDays,
	cardkeydomain.RewardIntegral,
	cardkeydomain.RewardExperience,
	cardkeydomain.RewardLotteryDraws,
}

// Reward 一项权益，与卡密同构。
type Reward = cardkeydomain.Reward

// RewardResult 一项权益的实际发放结果。
type RewardResult = cardkeydomain.RewardResult

// Scene 一个奖励场景：「看一次广告领 1 天会员」「看一次广告领 50 积分」各是一个场景。
//
// 场景与广告位是多对一：几个场景可以共用平台上的同一个激励广告位，
// 客户端上报时说明是为哪个场景看的。
type Scene struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	PlacementID string   `json:"placementId"`
	Enabled     bool     `json:"enabled"`
	Rewards     []Reward `json:"rewards"`
	// DailyLimit 每人每天在这个场景最多领几次，0 表示只受应用级总限额约束。
	DailyLimit int `json:"dailyLimit"`
	// CooldownSeconds 两次领取之间至少间隔多久，0 表示不限。
	CooldownSeconds int `json:"cooldownSeconds"`
}

// Config 一个应用的激励广告配置（含密文，只在服务端内部流转）。
type Config struct {
	AppID             int64     `json:"appid"`
	Enabled           bool      `json:"enabled"`
	Provider          string    `json:"provider"`
	ProviderAppID     string    `json:"providerAppId"`
	SecurityKeyCipher string    `json:"-"`
	SecurityKeyHint   string    `json:"securityKeyHint"`
	VerifyMode        string    `json:"verifyMode"`
	DailyLimit        int       `json:"dailyLimit"`
	Scenes            []Scene   `json:"scenes"`
	UpdatedBy         string    `json:"updatedBy,omitempty"`
	UpdatedAt         time.Time `json:"updatedAt"`
}

// FindScene 按 key 取场景。
func (c *Config) FindScene(key string) (Scene, bool) {
	if c == nil {
		return Scene{}, false
	}
	for _, scene := range c.Scenes {
		if scene.Key == key {
			return scene, true
		}
	}
	return Scene{}, false
}

// SceneForPlacement 广告位对应的第一个启用场景。只在 server 模式、客户端没说是哪个场景时兜底用。
func (c *Config) SceneForPlacement(placementID string) (Scene, bool) {
	if c == nil || placementID == "" {
		return Scene{}, false
	}
	for _, scene := range c.Scenes {
		if scene.Enabled && scene.PlacementID == placementID {
			return scene, true
		}
	}
	return Scene{}, false
}

// AdminConfig 管理端看到的配置：密钥只回答「配没配」。
type AdminConfig struct {
	Enabled          bool                       `json:"enabled"`
	Provider         string                     `json:"provider"`
	ProviderAppID    string                     `json:"providerAppId"`
	HasSecurityKey   bool                       `json:"hasSecurityKey"`
	SecurityKeyHint  string                     `json:"securityKeyHint"`
	VerifyMode       string                     `json:"verifyMode"`
	DailyLimit       int                        `json:"dailyLimit"`
	Scenes           []Scene                    `json:"scenes"`
	CallbackURL      string                     `json:"callbackUrl"`
	CallbackAbsolute bool                       `json:"callbackAbsolute"`
	UpdatedBy        string                     `json:"updatedBy,omitempty"`
	UpdatedAt        *time.Time                 `json:"updatedAt,omitempty"`
	Catalog          []cardkeydomain.RewardSpec `json:"catalog"`
	VerifyModes      []string                   `json:"verifyModes"`
	Providers        []string                   `json:"providers"`
}

// SaveConfigInput 保存配置。SecurityKey 留空表示不修改。
type SaveConfigInput struct {
	AppID            int64
	Enabled          bool
	Provider         string
	ProviderAppID    string
	SecurityKey      string
	ClearSecurityKey bool
	VerifyMode       string
	DailyLimit       int
	Scenes           []Scene
	Operator         string
}

// View 一次观看。
type View struct {
	ID               int64          `json:"id"`
	AppID            int64          `json:"appid"`
	UserID           int64          `json:"userId"`
	Account          string         `json:"account,omitempty"`
	TransID          string         `json:"transId"`
	Scene            string         `json:"scene"`
	SceneName        string         `json:"sceneName,omitempty"`
	PlacementID      string         `json:"placementId"`
	Status           string         `json:"status"`
	Reason           string         `json:"reason,omitempty"`
	ServerVerifiedAt *time.Time     `json:"serverVerifiedAt,omitempty"`
	RewardName       string         `json:"rewardName,omitempty"`
	RewardAmount     int            `json:"rewardAmount,omitempty"`
	NetworkID        string         `json:"networkId,omitempty"`
	Extra            string         `json:"extra,omitempty"`
	ClientReportedAt *time.Time     `json:"clientReportedAt,omitempty"`
	ClientVerified   *bool          `json:"clientVerified,omitempty"`
	ClientError      string         `json:"clientError,omitempty"`
	DeviceID         string         `json:"deviceId,omitempty"`
	ClientIP         string         `json:"clientIp,omitempty"`
	Results          []RewardResult `json:"results"`
	GrantedAt        *time.Time     `json:"grantedAt,omitempty"`
	Operator         string         `json:"operator,omitempty"`
	CreatedAt        time.Time      `json:"createdAt"`
	UpdatedAt        time.Time      `json:"updatedAt"`
}

// ViewQuery 观看记录筛选。
type ViewQuery struct {
	AppID   int64
	UserID  int64
	Status  string
	Scene   string
	Keyword string
	Start   *time.Time
	End     *time.Time
	Page    int
	Limit   int
}

// ViewPage 观看记录分页。
type ViewPage struct {
	Items      []View `json:"items"`
	Total      int64  `json:"total"`
	Page       int    `json:"page"`
	Limit      int    `json:"limit"`
	TotalPages int    `json:"totalPages"`
}

// 观看的两方。
const (
	SideServer = "server"
	SideClient = "client"
	SideAdmin  = "admin"
)

// RecordInput 一方带来的事实。仓储层据此补齐记录，并在条件满足时同事务发奖。
type RecordInput struct {
	AppID   int64
	UserID  int64
	TransID string
	Side    string
	// Scene 客户端上报的场景，或 server 模式下从 extra / 广告位推出来的场景。
	Scene       string
	PlacementID string

	// 服务端回调
	RewardName   string
	RewardAmount int
	NetworkID    string
	Extra        string

	// 客户端上报
	ClientVerified *bool
	ClientError    string
	DeviceID       string
	ClientIP       string

	// 管理端补发：忽略限额与另一方是否到齐。
	Operator string
	Now      time.Time
}

// SettlePolicy 发奖判定所需的配置快照，由服务层读好传进事务。
type SettlePolicy struct {
	Enabled    bool
	VerifyMode string
	DailyLimit int
	Scenes     []Scene
	// DayStart 「今天」从什么时候算起（默认时区的零点）。
	DayStart time.Time
	// ForceGrant 管理端补发：跳过另一方是否到齐与限额判定。
	ForceGrant bool
}

// FindScene 按 key 取场景。
func (p SettlePolicy) FindScene(key string) (Scene, bool) {
	for _, scene := range p.Scenes {
		if scene.Key == key {
			return scene, true
		}
	}
	return Scene{}, false
}

// Ready 这条记录在当前模式下是否已经可以结算。
func (p SettlePolicy) Ready(view *View) bool {
	if p.ForceGrant {
		return true
	}
	server := view.ServerVerifiedAt != nil
	client := view.ClientReportedAt != nil
	switch p.VerifyMode {
	case VerifyServer:
		return server
	case VerifyClient:
		return client
	default:
		return server && client
	}
}

// ClaimResult 客户端上报（或轮询）的结果。
//
// 上报本身总是成功的 —— 记录已经落库；Status 说明这次观看最终怎样：
// pending 表示还在等广告平台的服务端回调，客户端可以稍后用同一个 transId 再上报一次来查询。
type ClaimResult struct {
	TransID   string         `json:"transId"`
	Scene     string         `json:"scene"`
	Status    string         `json:"status"`
	Reason    string         `json:"reason,omitempty"`
	Message   string         `json:"message"`
	Results   []RewardResult `json:"results"`
	GrantedAt *time.Time     `json:"grantedAt,omitempty"`
	// Remaining 结算后该场景今天还能领几次，-1 表示不限。
	Remaining int `json:"remaining"`
}

// ClientScene 客户端看到的一个场景。
type ClientScene struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	PlacementID string   `json:"placementId"`
	Rewards     []Reward `json:"rewards"`
	// RewardSummary 一句话说清看完能拿到什么，客户端直接展示。
	RewardSummary   string `json:"rewardSummary"`
	DailyLimit      int    `json:"dailyLimit"`
	TodayCount      int    `json:"todayCount"`
	Remaining       int    `json:"remaining"`
	CooldownSeconds int    `json:"cooldownSeconds"`
	// CooldownRemaining 距离下一次可领还要等几秒，0 表示现在就能看。
	CooldownRemaining int        `json:"cooldownRemaining"`
	NextAvailableAt   *time.Time `json:"nextAvailableAt,omitempty"`
	Available         bool       `json:"available"`
}

// ClientStatus 客户端拉取的激励广告状态。
type ClientStatus struct {
	Enabled       bool   `json:"enabled"`
	Provider      string `json:"provider"`
	ProviderAppID string `json:"providerAppId"`
	VerifyMode    string `json:"verifyMode"`
	// UserID 传给广告 SDK 的用户标识（HJRewardAdRequest 的 userId）。它带签名，
	// 服务端回调时据此认出是谁看的；客户端不要自己拼，也不要传内部用户 ID。
	UserID     string        `json:"userId"`
	DailyLimit int           `json:"dailyLimit"`
	TodayCount int           `json:"todayCount"`
	Remaining  int           `json:"remaining"`
	Scenes     []ClientScene `json:"scenes"`
	ServerTime time.Time     `json:"serverTime"`
}

// Usage 一个用户今天的领取情况，仓储层一次查齐。
type Usage struct {
	TodayTotal   int
	TodayByScene map[string]int
	LastByScene  map[string]time.Time
}

// StatsDay 一天的统计。
type StatsDay struct {
	Date     string `json:"date"`
	Views    int64  `json:"views"`
	Granted  int64  `json:"granted"`
	Rejected int64  `json:"rejected"`
	Users    int64  `json:"users"`
}

// StatsScene 一个场景在统计窗口内的表现。
type StatsScene struct {
	Scene   string `json:"scene"`
	Name    string `json:"name"`
	Views   int64  `json:"views"`
	Granted int64  `json:"granted"`
}

// StatsSummary 概览数字。
type StatsSummary struct {
	TodayViews    int64 `json:"todayViews"`
	TodayGranted  int64 `json:"todayGranted"`
	TodayRejected int64 `json:"todayRejected"`
	TodayUsers    int64 `json:"todayUsers"`
	Pending       int64 `json:"pending"`
	TotalGranted  int64 `json:"totalGranted"`
}

// Stats 管理端概览。
type Stats struct {
	Days    int          `json:"days"`
	Summary StatsSummary `json:"summary"`
	Trend   []StatsDay   `json:"trend"`
	Scenes  []StatsScene `json:"scenes"`
}
