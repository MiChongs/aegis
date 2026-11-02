// Package promo 推广活动：一段时间内的「邀请好友」档位奖励与「分享推广」文章审核。
//
// 规则见 docs/promo.md。这个包只放类型、配置的清洗与校验、平台目录、链接解析与判定（纯函数），
// 领取、提交与审核在仓储层的事务里完成。奖励目录与发放沿用邀请（invite）。
package promo

import (
	"time"

	"aegis/internal/domain/invite"
	"aegis/internal/domain/pointstore"
)

// 活动状态（运营设置的）。
const (
	StatusDraft   = "draft"
	StatusOnline  = "online"
	StatusOffline = "offline"
)

// 活动阶段（按时间推出来的）。
const (
	PhaseUpcoming = "upcoming"
	PhaseActive   = "active"
	PhaseEnded    = "ended"
)

// 邀请档位在用户端的状态。
const (
	TierLocked    = "locked"
	TierClaimable = "claimable"
	TierClaimed   = "claimed"
	TierExpired   = "expired"
)

// 分享提交的状态。
const (
	SubmissionPending  = "pending"
	SubmissionApproved = "approved"
	SubmissionRejected = "rejected"
)

// 审核动作。
const (
	ReviewApprove = "approve"
	ReviewReject  = "reject"
)

// 判定的 reason，与错误码一一对应（见 docs/promo.md）。
const (
	ReasonNotFound             = "not_found"
	ReasonNotStarted           = "not_started"
	ReasonEnded                = "ended"
	ReasonInviteDisabled       = "invite_disabled"
	ReasonTierNotFound         = "tier_not_found"
	ReasonTierLocked           = "tier_locked"
	ReasonTierClaimed          = "tier_claimed"
	ReasonShareDisabled        = "share_disabled"
	ReasonPlatformUnavailable  = "platform_unavailable"
	ReasonLinkInvalid          = "link_invalid"
	ReasonLinkPlatformMismatch = "link_platform_mismatch"
	ReasonLinkForbidden        = "link_forbidden"
	ReasonLinkDuplicate        = "link_duplicate"
	ReasonDailyLimited         = "daily_limited"
	ReasonUserLimited          = "user_limited"
	ReasonPlatformLimited      = "platform_limited"
	ReasonRewardUnavailable    = "reward_unavailable"
	ReasonAlreadyReviewed      = "already_reviewed"
)

// 字段上限与默认值。
const (
	MaxTitleRunes         = 32
	MaxSubtitleRunes      = 64
	MaxRulesRunes         = 2000
	MaxTierTitleRunes     = 16
	MaxShareTemplateRunes = 300
	MaxNoteRunes          = 200
	MaxRejectReasonRunes  = 200
	MaxURLLength          = 1024
	MaxTiers              = 10
	MaxTierCount          = 100_000
	MaxDurationDays       = 180
	MaxClaimGraceDays     = 30
	DefaultClaimGraceDays = 7
	MinLeaderboardSize    = 10
	MaxLeaderboardSize    = 100
	DefaultLeaderboard    = 20
	MaxPerUserLimit       = 50
	MaxBlockedDomains     = 20
	MaxDomainLength       = 128
	MaxReviewBatch        = 100

	// IntegralTitlePrefix 流水标题的前缀：「推广活动：<标题>」
	IntegralTitlePrefix = "推广活动："
)

// Reward 一项权益，与邀请同一套目录与账本。
type Reward = invite.Reward

// RewardResult 一项权益的实际发放结果。
type RewardResult = invite.RewardResult

// ClientReward 用户端看到的一项权益。
type ClientReward = invite.ClientReward

// Page 分页结果。
type Page[T any] = pointstore.Page[T]

// Tier 邀请任务的一档：活动期间成功邀请满 Count 人可领一次。
type Tier struct {
	Count   int      `json:"count"`
	Title   string   `json:"title,omitempty"`
	Rewards []Reward `json:"rewards"`
}

// InviteTask 邀请好友任务。
type InviteTask struct {
	Enabled         bool   `json:"enabled"`
	Tiers           []Tier `json:"tiers"`
	Leaderboard     bool   `json:"leaderboard"`
	LeaderboardSize int    `json:"leaderboardSize"`
}

// ShareTask 分享推广任务。
type ShareTask struct {
	Enabled bool `json:"enabled"`
	// Platforms 启用的平台键（PlatformCatalog 里的）
	Platforms []string `json:"platforms"`
	// Rewards 每篇审核通过的文章发一次
	Rewards                []Reward `json:"rewards"`
	MaxApprovedPerUser     int      `json:"maxApprovedPerUser"`
	MaxApprovedPerPlatform int      `json:"maxApprovedPerPlatform"`
	DailySubmitLimit       int      `json:"dailySubmitLimit"`
	// BlockedDomains 额外禁用的域名（写死的禁用平台之外）
	BlockedDomains []string `json:"blockedDomains"`
	// ShareTemplate 推荐文案：{code} {link} {app} {platform}
	ShareTemplate string `json:"shareTemplate"`
}

// Config 活动除标题、时间、状态以外的配置（存 config JSONB）。
type Config struct {
	ClaimGraceDays int        `json:"claimGraceDays"`
	Invite         InviteTask `json:"invite"`
	Share          ShareTask  `json:"share"`
}

// Campaign 一期活动。
type Campaign struct {
	ID        int64     `json:"id"`
	AppID     int64     `json:"appid"`
	Title     string    `json:"title"`
	Subtitle  string    `json:"subtitle"`
	RulesText string    `json:"rulesText"`
	Status    string    `json:"status"`
	StartsAt  time.Time `json:"startsAt"`
	EndsAt    time.Time `json:"endsAt"`
	Config    Config    `json:"config"`
	CreatedBy string    `json:"createdBy,omitempty"`
	UpdatedBy string    `json:"updatedBy,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ClaimUntil 邀请档位最晚能领到什么时候。
func (c Campaign) ClaimUntil() time.Time {
	return c.EndsAt.Add(time.Duration(c.Config.ClaimGraceDays) * 24 * time.Hour)
}

// CampaignInput 新建 / 修改活动。
type CampaignInput struct {
	AppID     int64
	Title     string
	Subtitle  string
	RulesText string
	StartsAt  time.Time
	EndsAt    time.Time
	Config    Config
	Operator  string
}

// AdminCampaign 管理端列表里的一期活动，带简要统计。
type AdminCampaign struct {
	Campaign
	Phase              string `json:"phase"`
	Participants       int64  `json:"participants"`
	TierClaims         int64  `json:"tierClaims"`
	PendingSubmissions int64  `json:"pendingSubmissions"`
	ApprovedSubmission int64  `json:"approvedSubmissions"`
}

// CampaignQuery 管理端活动列表。
type CampaignQuery struct {
	AppID   int64
	Status  string
	Keyword string
	Page    int
	Limit   int
}

// Submission 一条分享提交。
type Submission struct {
	ID           int64          `json:"id"`
	AppID        int64          `json:"appid"`
	CampaignID   int64          `json:"campaignId"`
	UserID       int64          `json:"userId"`
	Platform     string         `json:"platform"`
	URL          string         `json:"url"`
	URLKey       string         `json:"-"`
	Note         string         `json:"note,omitempty"`
	Status       string         `json:"status"`
	RejectReason string         `json:"rejectReason,omitempty"`
	Results      []RewardResult `json:"results"`
	ReviewedBy   string         `json:"reviewedBy,omitempty"`
	ReviewedAt   *time.Time     `json:"reviewedAt,omitempty"`
	DeviceID     string         `json:"deviceId,omitempty"`
	ClientIP     string         `json:"clientIp,omitempty"`
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`

	// 管理端列表才带
	PlatformName  string `json:"platformName,omitempty"`
	CampaignTitle string `json:"campaignTitle,omitempty"`
	UserAccount   string `json:"userAccount,omitempty"`
	UserNickname  string `json:"userNickname,omitempty"`
	UserAvatar    string `json:"userAvatar,omitempty"`
}

// SubmissionQuery 管理端提交列表。
type SubmissionQuery struct {
	AppID      int64
	CampaignID int64
	UserID     int64
	Status     string
	Platform   string
	Keyword    string
	Page       int
	Limit      int
}

// SubmitInput 用户提交一篇文章。
type SubmitInput struct {
	AppID      int64
	CampaignID int64
	UserID     int64
	Platform   string
	URL        string
	Note       string
	DeviceID   string
	ClientIP   string
}

// SubmitFacts 判定提交时取齐的事实（锁住用户行之后）。
type SubmitFacts struct {
	TodaySubmitted int
	// Approved / Pending 这期活动里我已通过 / 待审核的篇数
	Approved int
	Pending  int
	// PlatformTaken 所选平台上我已通过与待审核的篇数
	PlatformTaken int
	// Duplicate 这篇文章已经有一条未驳回的提交
	Duplicate bool
}

// TierFacts 判定领取时取齐的事实。
type TierFacts struct {
	Count   int64
	Claimed bool
}

// TierGrant 一次档位领取。
type TierGrant struct {
	CampaignID int64          `json:"campaignId"`
	Count      int            `json:"count"`
	Results    []RewardResult `json:"results"`
	CreatedAt  time.Time      `json:"createdAt"`
}

// ReviewInput 管理端批量审核。
type ReviewInput struct {
	AppID    int64
	IDs      []int64
	Action   string
	Reason   string
	Operator string
}

// ReviewOutcome 一条提交的审核结论。
type ReviewOutcome struct {
	ID      int64          `json:"id"`
	OK      bool           `json:"ok"`
	Status  string         `json:"status,omitempty"`
	Reason  string         `json:"reason,omitempty"`
	Message string         `json:"message,omitempty"`
	Results []RewardResult `json:"results,omitempty"`

	// 发通知用
	UserID     int64  `json:"-"`
	CampaignID int64  `json:"-"`
	Title      string `json:"-"`
}

// ── 用户端 ──

// ClientCampaignSummary 用户端列表里的一期活动。
type ClientCampaignSummary struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Subtitle  string    `json:"subtitle,omitempty"`
	Phase     string    `json:"phase"`
	StartsAt  time.Time `json:"startsAt"`
	EndsAt    time.Time `json:"endsAt"`
	HasInvite bool      `json:"hasInvite"`
	HasShare  bool      `json:"hasShare"`
	Claimable bool      `json:"claimable"`
}

// ClientTier 用户端的一档。
type ClientTier struct {
	Count     int            `json:"count"`
	Title     string         `json:"title,omitempty"`
	Rewards   []ClientReward `json:"rewards"`
	State     string         `json:"state"`
	ClaimedAt *time.Time     `json:"claimedAt"`
}

// ClientInvite 用户端的邀请任务。
type ClientInvite struct {
	Count         int64        `json:"count"`
	Pending       int64        `json:"pending"`
	Code          string       `json:"code,omitempty"`
	ShareText     string       `json:"shareText,omitempty"`
	ShareLink     string       `json:"shareLink,omitempty"`
	CanInvite     bool         `json:"canInvite"`
	InviteMessage string       `json:"inviteMessage,omitempty"`
	Tiers         []ClientTier `json:"tiers"`
	Leaderboard   bool         `json:"leaderboard"`
}

// ClientPlatform 用户端的一个平台。
type ClientPlatform struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Hint        string   `json:"hint,omitempty"`
	PackageName string   `json:"packageName,omitempty"`
	Domains     []string `json:"domains"`
}

// ClientShare 用户端的分享任务。
type ClientShare struct {
	Platforms              []ClientPlatform `json:"platforms"`
	Forbidden              []string         `json:"forbidden"`
	Rewards                []ClientReward   `json:"rewards"`
	ShareText              string           `json:"shareText,omitempty"`
	MaxApprovedPerUser     int              `json:"maxApprovedPerUser"`
	MaxApprovedPerPlatform int              `json:"maxApprovedPerPlatform"`
	DailySubmitLimit       int              `json:"dailySubmitLimit"`
	Approved               int              `json:"approved"`
	Pending                int              `json:"pending"`
	TodaySubmitted         int              `json:"todaySubmitted"`
	CanSubmit              bool             `json:"canSubmit"`
	Reason                 string           `json:"reason,omitempty"`
	Message                string           `json:"message,omitempty"`
}

// ClientCampaign 用户端的活动详情。
type ClientCampaign struct {
	ID         int64         `json:"id"`
	Title      string        `json:"title"`
	Subtitle   string        `json:"subtitle,omitempty"`
	RulesText  string        `json:"rulesText,omitempty"`
	Phase      string        `json:"phase"`
	StartsAt   time.Time     `json:"startsAt"`
	EndsAt     time.Time     `json:"endsAt"`
	ClaimUntil time.Time     `json:"claimUntil"`
	Invite     *ClientInvite `json:"invite"`
	Share      *ClientShare  `json:"share"`
}

// ClientSubmission 用户端的一条提交。
type ClientSubmission struct {
	ID           int64          `json:"id"`
	Platform     string         `json:"platform"`
	PlatformName string         `json:"platformName"`
	URL          string         `json:"url"`
	Note         string         `json:"note,omitempty"`
	Status       string         `json:"status"`
	RejectReason string         `json:"rejectReason,omitempty"`
	Results      []RewardResult `json:"results"`
	CreatedAt    time.Time      `json:"createdAt"`
	ReviewedAt   *time.Time     `json:"reviewedAt,omitempty"`
}

// RankEntry 排行榜的一行。
type RankEntry struct {
	Rank     int    `json:"rank"`
	UserID   int64  `json:"userId,omitempty"`
	Account  string `json:"account,omitempty"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar,omitempty"`
	Count    int64  `json:"count"`
	// ReachedAt 达到这个人数的时间（同人数先到先排）
	ReachedAt time.Time `json:"reachedAt"`

	RawAvatar string `json:"-"`
}

// Leaderboard 排行榜：前 N 名与我的名次（我没有有效邀请时 rank 为 0）。
type Leaderboard struct {
	Items []RankEntry `json:"items"`
	Me    *RankMe     `json:"me,omitempty"`
}

// RankMe 我的名次。
type RankMe struct {
	Rank  int   `json:"rank"`
	Count int64 `json:"count"`
}

// ── 统计 ──

// Stats 一期活动的统计。
type Stats struct {
	Participants   int64              `json:"participants"`
	Invited        int64              `json:"invited"`
	PendingInvites int64              `json:"pendingInvites"`
	TierClaims     []TierClaimStat    `json:"tierClaims"`
	Submissions    SubmissionCounts   `json:"submissions"`
	ByPlatform     []PlatformStat     `json:"byPlatform"`
	Daily          []DailyCampaignRow `json:"daily"`
}

// TierClaimStat 一档的领取人数。
type TierClaimStat struct {
	Count   int   `json:"count"`
	Claimed int64 `json:"claimed"`
}

// SubmissionCounts 各状态的提交数。
type SubmissionCounts struct {
	Pending  int64 `json:"pending"`
	Approved int64 `json:"approved"`
	Rejected int64 `json:"rejected"`
}

// PlatformStat 一个平台的提交数。
type PlatformStat struct {
	Platform string `json:"platform"`
	Name     string `json:"name"`
	SubmissionCounts
}

// DailyCampaignRow 一天的数据。
type DailyCampaignRow struct {
	Date      string `json:"date"`
	Invited   int64  `json:"invited"`
	Submitted int64  `json:"submitted"`
	Approved  int64  `json:"approved"`
}
