// Package invite 邀请：个人邀请码、运营的活动码、邀请关系与双方奖励。
//
// 规则见 docs/invite.md。这个包只放类型、配置的清洗与校验、权益目录与判定（纯函数），
// 绑定与发放在仓储层的同一个事务里完成。
package invite

import (
	"time"

	"aegis/internal/domain/cosmetic"
	"aegis/internal/domain/pointstore"
)

// 注册表单上邀请码字段的形态。
const (
	FieldHidden   = "hidden"   // 不出现（注册后仍可在时限内补填）
	FieldOptional = "optional" // 选填
	FieldRequired = "required" // 必填：没有邀请码不能注册
)

// 个人码的字符集。
const (
	CharsetAlnum   = "alnum"   // 大写字母与数字，去掉 0 O 1 I 这类容易看错的
	CharsetDigits  = "digits"  // 纯数字
	CharsetLetters = "letters" // 大写字母，去掉 I O
)

// 谁的个人码可以被填写。
const (
	AudienceAll = "all"
	AudienceVip = "vip" // 有效会员（含永久与试用）
)

// 邀请人奖励的发放时机。
const (
	TriggerBind   = "bind"   // 被邀请人填写邀请码即发
	TriggerSignIn = "signin" // 被邀请人累计签到满 N 天后发
)

// 邀请码类型与状态。
const (
	KindPersonal = "personal" // 个人码：一人一个
	KindCampaign = "campaign" // 活动码：运营创建，可挂推广人

	CodeActive   = "active"
	CodeDisabled = "disabled"
)

// 邀请关系的来源。
const (
	SourceRegister = "register" // 注册时填写
	SourceBind     = "bind"     // 注册后补填
)

// 邀请人奖励的状态。
const (
	InviterNone      = "none"      // 没有邀请人（纯渠道码）
	InviterPending   = "pending"   // 等被邀请人达成条件
	InviterGranted   = "granted"   // 已发放（算作一次成功邀请）
	InviterLimited   = "limited"   // 邀请人已达到奖励上限，这次不发
	InviterCancelled = "cancelled" // 管理员取消
)

// 积分流水与账本上的来源标记。
const (
	IntegralCategory = "invite"
	SourceType       = "invite"
)

// 字段上限与默认值。
const (
	DefaultBindWindowHours = 72
	MaxBindWindowHours     = 720
	DefaultCodeLength      = 8
	MinCodeLength          = 4
	MinDigitsCodeLength    = 6
	MaxCodeLength          = 16
	MaxPrefixLength        = 6
	MinCustomCodeLength    = 4
	MaxCustomCodeLength    = 16
	// MaxStoredCodeLength 库里能存的最长邀请码（前缀 + 主体，以及老系统 24 位的个人码）
	MaxStoredCodeLength  = 32
	MaxRewardsPerList    = 6
	MaxMilestones        = 10
	MaxMilestoneCount    = 100_000
	MaxTriggerSignInDays = 365
	MaxInviterLimit      = 1_000_000
	MaxDeviceLimit       = 100
	MaxIPDailyLimit      = 10_000
	MaxMinLevel          = 1000
	MaxFieldLabelRunes   = 16
	MaxPlaceholderRunes  = 32
	MaxMilestoneTitle    = 16
	MaxRulesRunes        = 2000
	MaxShareRunes        = 300
	MaxLandingURLLength  = 512
	MaxCampaignTitle     = 32
	MaxCampaignNote      = 128
	MaxBatchCount        = 500
	MaxCodeUses          = 100_000_000
)

// Reward 一项权益，形状与积分商城相同（同一套账本）。
type Reward = pointstore.Reward

// RewardResult 一项权益的实际发放结果。
type RewardResult = pointstore.RewardResult

// ClientReward 用户端看到的一项权益。
type ClientReward = pointstore.ClientReward

// Milestone 邀请人累计成功邀请满 Count 人时的一档奖励，每档只发一次。
type Milestone struct {
	Count   int      `json:"count"`
	Title   string   `json:"title,omitempty"`
	Rewards []Reward `json:"rewards"`
}

// Config 应用的邀请配置（除开关以外的全部）。
type Config struct {
	// 注册表单
	RegisterField    string `json:"registerField"`
	FieldLabel       string `json:"fieldLabel"`
	FieldPlaceholder string `json:"fieldPlaceholder"`
	// BindWindowHours 注册后多少小时内可以补填，0 为只能在注册时填
	BindWindowHours int `json:"bindWindowHours"`

	// 个人码格式
	CodeLength      int    `json:"codeLength"`
	CodeCharset     string `json:"codeCharset"`
	CodePrefix      string `json:"codePrefix"`
	AllowCustomCode bool   `json:"allowCustomCode"`

	// 邀请人资格
	InviterAudience string `json:"inviterAudience"`
	InviterMinLevel int    `json:"inviterMinLevel"`

	// 奖励
	InviteeRewards []Reward    `json:"inviteeRewards"`
	InviterRewards []Reward    `json:"inviterRewards"`
	Milestones     []Milestone `json:"milestones"`
	InviterTrigger string      `json:"inviterTrigger"`
	// TriggerSignInDays inviterTrigger = signin 时被邀请人需要累计签到的天数
	TriggerSignInDays int `json:"triggerSignInDays"`
	// InviterDailyLimit / InviterTotalLimit 邀请人每天 / 累计最多拿几次奖励，0 不限。
	// 超出后邀请照样成立、被邀请人照样拿奖励，只是邀请人这次不发
	InviterDailyLimit int `json:"inviterDailyLimit"`
	InviterTotalLimit int `json:"inviterTotalLimit"`
	// NotifyInviter 邀请人拿到奖励时发站内信
	NotifyInviter bool `json:"notifyInviter"`

	// 防刷：同一台设备最多填写几次（全应用），同一 IP 每天最多填写几次；0 不限
	DeviceLimit  int `json:"deviceLimit"`
	IPDailyLimit int `json:"ipDailyLimit"`

	// 文案：活动规则、分享文案（{code} {link} {app}）、落地页（{code}）
	RulesText     string `json:"rulesText"`
	ShareTemplate string `json:"shareTemplate"`
	LandingURL    string `json:"landingUrl"`
}

// Settings 应用的邀请开关与配置。
type Settings struct {
	AppID   int64 `json:"appid"`
	Enabled bool  `json:"enabled"`
	Config
	UpdatedBy string     `json:"updatedBy,omitempty"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
	// Configured 是否保存过（没保存过时按关闭处理）
	Configured bool `json:"configured"`
}

// Code 一个邀请码。
type Code struct {
	ID    int64  `json:"id"`
	AppID int64  `json:"appid"`
	Code  string `json:"code"`
	Kind  string `json:"kind"`
	// OwnerUserID 个人码的主人 / 活动码的推广人（可空）
	OwnerUserID *int64 `json:"ownerUserId,omitempty"`
	Title       string `json:"title,omitempty"`
	Note        string `json:"note,omitempty"`
	// Rewards 活动码单独配置的被邀请人奖励；nil 为跟随应用默认
	Rewards   *[]Reward  `json:"rewards,omitempty"`
	MaxUses   *int64     `json:"maxUses,omitempty"`
	UsedCount int64      `json:"usedCount"`
	StartsAt  *time.Time `json:"startsAt,omitempty"`
	EndsAt    *time.Time `json:"endsAt,omitempty"`
	Status    string     `json:"status"`
	Custom    bool       `json:"custom,omitempty"`
	CreatedBy string     `json:"createdBy,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	// Legacy 老系统 user_profiles.invite_code 回落查到的个人码（库里没有这一行，ID 为 0）
	Legacy bool `json:"legacy,omitempty"`
}

// AdminCode 管理端的邀请码：带主人 / 推广人的账号。
type AdminCode struct {
	Code
	OwnerAccount  string `json:"ownerAccount,omitempty"`
	OwnerNickname string `json:"ownerNickname,omitempty"`
}

// CodeInput 活动码的创建 / 更新。
type CodeInput struct {
	AppID int64
	// Code 指定码（单个创建）；为空时按 Prefix + 应用格式生成 Count 个
	Code        string
	Count       int
	Prefix      string
	OwnerUserID *int64
	Title       string
	Note        string
	Rewards     *[]Reward
	MaxUses     *int64
	StartsAt    *time.Time
	EndsAt      *time.Time
	Status      string
	Operator    string
}

// CodeQuery 管理端邀请码列表。
type CodeQuery struct {
	AppID   int64
	Kind    string
	Status  string
	OwnerID int64
	Keyword string
	Page    int
	Limit   int
}

// Relation 一条邀请关系。
type Relation struct {
	ID        int64  `json:"id"`
	AppID     int64  `json:"appid"`
	InviteeID int64  `json:"inviteeId"`
	InviterID *int64 `json:"inviterId,omitempty"`
	CodeID    *int64 `json:"codeId,omitempty"`
	Code      string `json:"code"`
	CodeKind  string `json:"codeKind"`
	Source    string `json:"source"`
	// InviteeResults 被邀请人拿到了什么
	InviteeResults []RewardResult `json:"inviteeResults"`
	InviterStatus  string         `json:"inviterStatus"`
	InviterResults []RewardResult `json:"inviterResults"`
	// InviterGrantedAt 邀请人奖励的结算时间（发放或判定达到上限）
	InviterGrantedAt *time.Time `json:"inviterGrantedAt,omitempty"`
	HandledBy        string     `json:"handledBy,omitempty"`
	DeviceID         string     `json:"deviceId,omitempty"`
	ClientIP         string     `json:"clientIp,omitempty"`
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`

	InviteeAccount  string `json:"inviteeAccount,omitempty"`
	InviteeNickname string `json:"inviteeNickname,omitempty"`
	InviteeAvatar   string `json:"inviteeAvatar,omitempty"`
	InviterAccount  string `json:"inviterAccount,omitempty"`
	InviterNickname string `json:"inviterNickname,omitempty"`
	InviterAvatar   string `json:"inviterAvatar,omitempty"`
}

// RelationQuery 邀请关系列表。
type RelationQuery struct {
	AppID         int64
	InviterID     int64
	InviteeID     int64
	CodeID        int64
	InviterStatus string
	Source        string
	Keyword       string
	Page          int
	Limit         int
}

// MilestoneGrant 一档阶梯奖励的发放记录。
type MilestoneGrant struct {
	Milestone int            `json:"milestone"`
	Results   []RewardResult `json:"results"`
	CreatedAt time.Time      `json:"createdAt"`
}

// Page 分页结果。
type Page[T any] = pointstore.Page[T]

// BindInput 一次填写邀请码。
type BindInput struct {
	AppID     int64
	UserID    int64
	Code      string
	Source    string
	DeviceID  string
	ClientIP  string
	Operator  string
	Settings  Settings
	Windows   pointstore.Windows
	Now       time.Time
	AppName   string
	UserAgent string
}

// BindResult 填写的结果。
type BindResult struct {
	// Relation 只在服务端流转：里面有邀请人的奖励结果与流水号，不该给填写的人看
	Relation Relation `json:"-"`
	// Code 填写的邀请码（规范化之后的原样）
	Code string `json:"code"`
	// Rewards 被邀请人这次拿到的
	Rewards []RewardResult `json:"rewards"`
	// Inviter 邀请人的展示信息（纯渠道码为空）
	Inviter *ClientUser `json:"inviter,omitempty"`
	// InviterSettled 邀请人奖励是否已在这次结算（发放或判定达到上限）
	InviterSettled bool `json:"-"`
	// Milestones 这次顺带让邀请人达成的阶梯
	Milestones []MilestoneGrant `json:"-"`
}

// SettleResult 一次邀请人奖励的结算。
type SettleResult struct {
	Relation   Relation
	Status     string
	Results    []RewardResult
	Milestones []MilestoneGrant
}

// ClientUser 用户端看到的另一个人：昵称（没有昵称时是打码的账号）与头像。
type ClientUser struct {
	UserID    int64  `json:"-"`
	Account   string `json:"-"`
	RawAvatar string `json:"-"`
	Nickname  string `json:"nickname"`
	Avatar    string `json:"avatar,omitempty"`
}

// ClientMilestone 用户端看到的一档阶梯奖励。
type ClientMilestone struct {
	Count   int            `json:"count"`
	Title   string         `json:"title,omitempty"`
	Rewards []ClientReward `json:"rewards"`
	// Reached 已达成；Granted 已发放（达成而未发放说明当时发放失败，等下一次结算补发）
	Reached bool `json:"reached"`
	Granted bool `json:"granted"`
}

// ClientStats 我的邀请统计。
type ClientStats struct {
	// Invited 填写了我的邀请码的人数
	Invited int64 `json:"invited"`
	// Rewarded 成功邀请（已发放）
	Rewarded int64 `json:"rewarded"`
	// Pending 等对方达成条件
	Pending int64 `json:"pending"`
	// TodayRewarded 今天已拿到奖励的次数（对照每日上限）
	TodayRewarded int64 `json:"todayRewarded"`
}

// ClientBound 我填写过的邀请码。
type ClientBound struct {
	Code    string         `json:"code"`
	Source  string         `json:"source"`
	Inviter *ClientUser    `json:"inviter,omitempty"`
	Rewards []RewardResult `json:"rewards"`
	BoundAt time.Time      `json:"boundAt"`
}

// ClientBind 我能不能填写邀请码。
type ClientBind struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
	Message   string `json:"message,omitempty"`
	// Deadline 补填截止时间（已填写或只能注册时填写时为空）
	Deadline *time.Time   `json:"deadline,omitempty"`
	Bound    *ClientBound `json:"bound,omitempty"`
}

// ClientCenter 用户端的邀请页。
type ClientCenter struct {
	Enabled bool `json:"enabled"`
	// Code 我的个人码；不满足邀请人资格时为空，原因在 InviteMessage
	Code            string `json:"code,omitempty"`
	CanInvite       bool   `json:"canInvite"`
	InviteMessage   string `json:"inviteMessage,omitempty"`
	AllowCustomCode bool   `json:"allowCustomCode"`
	CodeCustom      bool   `json:"codeCustom"`
	ShareText       string `json:"shareText,omitempty"`
	ShareLink       string `json:"shareLink,omitempty"`
	RulesText       string `json:"rulesText,omitempty"`

	InviteeRewards    []ClientReward    `json:"inviteeRewards"`
	InviterRewards    []ClientReward    `json:"inviterRewards"`
	Milestones        []ClientMilestone `json:"milestones"`
	InviterTrigger    string            `json:"inviterTrigger"`
	TriggerSignInDays int               `json:"triggerSignInDays,omitempty"`
	DailyLimit        int               `json:"dailyLimit"`
	TotalLimit        int               `json:"totalLimit"`

	Stats      ClientStats `json:"stats"`
	Bind       ClientBind  `json:"bind"`
	ServerTime time.Time   `json:"serverTime"`
}

// ClientInvitee 我邀请的一个人。
type ClientInvitee struct {
	Nickname string         `json:"nickname"`
	Avatar   string         `json:"avatar,omitempty"`
	Source   string         `json:"source"`
	Status   string         `json:"status"`
	Results  []RewardResult `json:"results"`
	BoundAt  time.Time      `json:"boundAt"`
	// SettledAt 邀请人奖励的结算时间
	SettledAt *time.Time `json:"settledAt,omitempty"`
	// Cosmetics 被邀请人身上的装扮（docs/cosmetics.md），由传输层并进来；没戴或未开启时省略
	Cosmetics *cosmetic.Loadout `json:"cosmetics,omitempty"`
	// RawAvatar / UserID / Account 用于在传输层解析头像，不出网
	UserID    int64  `json:"-"`
	Account   string `json:"-"`
	RawAvatar string `json:"-"`
}

// ClientPreview 注册页填完邀请码后的预览：码能不能用、谁邀请的、填了能拿什么。
type ClientPreview struct {
	Valid   bool           `json:"valid"`
	Reason  string         `json:"reason,omitempty"`
	Message string         `json:"message,omitempty"`
	Code    string         `json:"code,omitempty"`
	Inviter *ClientUser    `json:"inviter,omitempty"`
	Rewards []ClientReward `json:"rewards"`
}

// Capability /config 里的邀请能力：注册表单据此决定邀请码字段出不出现、是否必填。
type Capability struct {
	Enabled bool `json:"enabled"`
	// Field hidden / optional / required
	Field       string `json:"field"`
	Label       string `json:"label"`
	Placeholder string `json:"placeholder,omitempty"`
	MaxLength   int    `json:"maxLength"`
	// BindWindowHours 注册后多少小时内还能补填，0 为只能注册时填
	BindWindowHours int `json:"bindWindowHours"`
}

// Stats 管理端概览。
type Stats struct {
	Summary     StatsSummary `json:"summary"`
	Trend       []StatsDay   `json:"trend"`
	TopInviters []TopInviter `json:"topInviters"`
	TopCodes    []TopCode    `json:"topCodes"`
}

// StatsSummary 概览数字。
type StatsSummary struct {
	Relations     int64 `json:"relations"`
	Today         int64 `json:"today"`
	Granted       int64 `json:"granted"`
	Pending       int64 `json:"pending"`
	Limited       int64 `json:"limited"`
	Inviters      int64 `json:"inviters"`
	FromRegister  int64 `json:"fromRegister"`
	FromBind      int64 `json:"fromBind"`
	PersonalCodes int64 `json:"personalCodes"`
	CampaignCodes int64 `json:"campaignCodes"`
}

// StatsDay 一天的数字。
type StatsDay struct {
	Date      string `json:"date"`
	Relations int64  `json:"relations"`
	Granted   int64  `json:"granted"`
}

// TopInviter 邀请最多的人。
type TopInviter struct {
	UserID   int64  `json:"userId"`
	Account  string `json:"account"`
	Nickname string `json:"nickname,omitempty"`
	Invited  int64  `json:"invited"`
	Granted  int64  `json:"granted"`
}

// TopCode 使用最多的活动码。
type TopCode struct {
	CodeID int64  `json:"codeId"`
	Code   string `json:"code"`
	Title  string `json:"title,omitempty"`
	Used   int64  `json:"used"`
}

// RegisterOutcome 注册时填写的邀请码的结论，随注册结果一起返回。
// 注册不因邀请码失败而失败：码在建号前已核对过，走到这里还失败的（并发用完次数这类）只说明原因。
type RegisterOutcome struct {
	Bound   bool           `json:"bound"`
	Code    string         `json:"code"`
	Message string         `json:"message,omitempty"`
	Rewards []RewardResult `json:"rewards"`
	Inviter *ClientUser    `json:"inviter,omitempty"`
}
