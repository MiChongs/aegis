package promo

import (
	"fmt"
	"slices"
	"time"
)

// Phase 活动在 now 时的阶段。下线了就算结束（领取期照样按 endsAt 算，见 CanClaimAt）。
func Phase(campaign Campaign, now time.Time) string {
	switch {
	case campaign.Status != StatusOnline:
		return PhaseEnded
	case now.Before(campaign.StartsAt):
		return PhaseUpcoming
	case !now.Before(campaign.EndsAt):
		return PhaseEnded
	default:
		return PhaseActive
	}
}

// VisibleToUsers 用户端能不能看到这期活动：已上线、且没过领取期。
func VisibleToUsers(campaign Campaign, now time.Time) bool {
	return campaign.Status == StatusOnline && now.Before(campaign.ClaimUntil())
}

// FindTier 按人数取一档。
func FindTier(cfg Config, count int) (Tier, bool) {
	for _, tier := range cfg.Invite.Tiers {
		if tier.Count == count {
			return tier, true
		}
	}
	return Tier{}, false
}

// EvaluateTier 能不能领 tierCount 这一档。返回空串表示可以。
//
// 判据顺序即优先级：活动是否上线 → 是否开始 → 是否过了领取期 → 有没有邀请任务 → 有没有这一档 →
// 领过没有 → 人数够不够。
func EvaluateTier(campaign Campaign, tierCount int, facts TierFacts, now time.Time) string {
	if campaign.Status != StatusOnline {
		return ReasonNotFound
	}
	if now.Before(campaign.StartsAt) {
		return ReasonNotStarted
	}
	if !now.Before(campaign.ClaimUntil()) {
		return ReasonEnded
	}
	if !campaign.Config.Invite.Enabled {
		return ReasonInviteDisabled
	}
	if _, ok := FindTier(campaign.Config, tierCount); !ok {
		return ReasonTierNotFound
	}
	if facts.Claimed {
		return ReasonTierClaimed
	}
	if facts.Count < int64(tierCount) {
		return ReasonTierLocked
	}
	return ""
}

// TierState 用户端一档的状态。
func TierState(campaign Campaign, tier Tier, count int64, claimed bool, now time.Time) string {
	switch {
	case claimed:
		return TierClaimed
	case count < int64(tier.Count):
		if !now.Before(campaign.EndsAt) {
			// 活动结束了人数不会再涨，还差的档位就是领不到了
			return TierExpired
		}
		return TierLocked
	case !now.Before(campaign.ClaimUntil()):
		return TierExpired
	default:
		return TierClaimable
	}
}

// EvaluateSubmitWindow 活动层面能不能提交（与具体链接无关），用户端的 canSubmit 也用它。
func EvaluateSubmitWindow(campaign Campaign, facts SubmitFacts, now time.Time) string {
	if campaign.Status != StatusOnline {
		return ReasonNotFound
	}
	if now.Before(campaign.StartsAt) {
		return ReasonNotStarted
	}
	if !now.Before(campaign.EndsAt) {
		return ReasonEnded
	}
	task := campaign.Config.Share
	if !task.Enabled {
		return ReasonShareDisabled
	}
	if facts.Approved+facts.Pending >= task.MaxApprovedPerUser {
		return ReasonUserLimited
	}
	if facts.TodaySubmitted >= task.DailySubmitLimit {
		return ReasonDailyLimited
	}
	return ""
}

// EvaluateSubmit 能不能提交这篇文章。link 由 ParseLink 解析（linkReason 是它的结论）。
// 判据顺序：活动 → 平台 → 链接 → 篇数与次数 → 重复。
func EvaluateSubmit(campaign Campaign, platformKey string, linkReason string, facts SubmitFacts, now time.Time) string {
	if reason := EvaluateSubmitWindow(campaign, SubmitFacts{}, now); reason != "" {
		return reason
	}
	if _, ok := FindPlatform(platformKey); !ok || !slices.Contains(campaign.Config.Share.Platforms, platformKey) {
		return ReasonPlatformUnavailable
	}
	if linkReason != "" {
		return linkReason
	}
	task := campaign.Config.Share
	if facts.Approved+facts.Pending >= task.MaxApprovedPerUser {
		return ReasonUserLimited
	}
	if facts.PlatformTaken >= task.MaxApprovedPerPlatform {
		return ReasonPlatformLimited
	}
	if facts.TodaySubmitted >= task.DailySubmitLimit {
		return ReasonDailyLimited
	}
	if facts.Duplicate {
		return ReasonLinkDuplicate
	}
	return ""
}

// EvaluateApprove 审核通过前的最后一道：只数已通过的（待审核的就是它自己与别的待审核）。
func EvaluateApprove(task ShareTask, approved, approvedOnPlatform int) string {
	if approved >= task.MaxApprovedPerUser {
		return ReasonUserLimited
	}
	if approvedOnPlatform >= task.MaxApprovedPerPlatform {
		return ReasonPlatformLimited
	}
	return ""
}

// ReasonMessage reason 的用户可读说明。
func ReasonMessage(reason string, campaign Campaign, platformKey string) string {
	task := campaign.Config.Share
	switch reason {
	case "":
		return ""
	case ReasonNotFound:
		return "活动不存在或已下线"
	case ReasonNotStarted:
		return "活动还没有开始"
	case ReasonEnded:
		return "活动已结束"
	case ReasonInviteDisabled:
		return "本期活动没有邀请任务"
	case ReasonTierNotFound:
		return "没有这一档奖励"
	case ReasonTierLocked:
		return "邀请人数还不够"
	case ReasonTierClaimed:
		return "这一档奖励已经领取过"
	case ReasonShareDisabled:
		return "本期活动没有分享任务"
	case ReasonPlatformUnavailable:
		return "本期活动不接受这个平台的文章"
	case ReasonLinkInvalid:
		return "请填写完整的文章链接"
	case ReasonLinkPlatformMismatch:
		return fmt.Sprintf("链接不属于%s，请检查所选平台", PlatformName(platformKey))
	case ReasonLinkForbidden:
		return "禁止在抖音等平台发布推广内容，该链接不予受理"
	case ReasonLinkDuplicate:
		return "这篇文章已经提交过"
	case ReasonDailyLimited:
		return fmt.Sprintf("每天最多提交 %d 次，请明天再来", task.DailySubmitLimit)
	case ReasonUserLimited:
		return fmt.Sprintf("每人最多 %d 篇文章获得奖励，已达上限", task.MaxApprovedPerUser)
	case ReasonPlatformLimited:
		return fmt.Sprintf("每个平台最多 %d 篇文章获得奖励，%s已达上限", task.MaxApprovedPerPlatform, PlatformName(platformKey))
	case ReasonRewardUnavailable:
		return "活动奖励配置已失效，请联系客服"
	case ReasonAlreadyReviewed:
		return "这条提交已经审核过"
	default:
		return "暂时无法参与"
	}
}
