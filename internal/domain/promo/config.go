package promo

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"aegis/internal/domain/invite"
)

// DefaultConfig 新建活动时的默认配置；控制台的新建表单展示的也是它。
func DefaultConfig() Config {
	return Config{
		ClaimGraceDays: DefaultClaimGraceDays,
		Invite: InviteTask{
			Enabled: true, Tiers: []Tier{}, Leaderboard: true, LeaderboardSize: DefaultLeaderboard,
		},
		Share: ShareTask{
			Enabled:                true,
			Platforms:              DefaultPlatforms(),
			Rewards:                []Reward{},
			MaxApprovedPerUser:     3,
			MaxApprovedPerPlatform: 1,
			DailySubmitLimit:       5,
			BlockedDomains:         DefaultBlockedDomains(),
			ShareTemplate:          "推荐一个好用的工具 App：{app}。注册时填写我的邀请码 {code} 可以领取新人奖励。{link}",
		},
	}
}

// Statuses 活动状态，顺序即控制台的展示顺序。
func Statuses() []string { return []string{StatusDraft, StatusOnline, StatusOffline} }

// SubmissionStatuses 提交状态。
func SubmissionStatuses() []string {
	return []string{SubmissionPending, SubmissionApproved, SubmissionRejected}
}

// NormalizeInput 清洗：去空白、补默认值、档位按人数升序、奖励按目录重排、平台按目录顺序去重、域名清洗去重。
func NormalizeInput(input CampaignInput) CampaignInput {
	input.Title = strings.TrimSpace(input.Title)
	input.Subtitle = strings.TrimSpace(input.Subtitle)
	input.RulesText = strings.TrimSpace(input.RulesText)
	input.Config = NormalizeConfig(input.Config)
	return input
}

// NormalizeConfig 见 NormalizeInput。
func NormalizeConfig(cfg Config) Config {
	tiers := make([]Tier, 0, len(cfg.Invite.Tiers))
	for _, tier := range cfg.Invite.Tiers {
		tiers = append(tiers, Tier{
			Count: tier.Count, Title: strings.TrimSpace(tier.Title), Rewards: invite.NormalizeRewards(tier.Rewards),
		})
	}
	sort.SliceStable(tiers, func(i, j int) bool { return tiers[i].Count < tiers[j].Count })
	cfg.Invite.Tiers = tiers
	if cfg.Invite.LeaderboardSize == 0 {
		cfg.Invite.LeaderboardSize = DefaultLeaderboard
	}

	selected := make([]string, 0, len(cfg.Share.Platforms))
	for _, platform := range platforms {
		if slices.Contains(cfg.Share.Platforms, platform.Key) {
			selected = append(selected, platform.Key)
		}
	}
	// 未登记的键留在末尾交给校验报错
	for _, key := range cfg.Share.Platforms {
		if _, ok := FindPlatform(key); !ok && !slices.Contains(selected, key) {
			selected = append(selected, key)
		}
	}
	cfg.Share.Platforms = selected
	cfg.Share.Rewards = invite.NormalizeRewards(cfg.Share.Rewards)
	domains := make([]string, 0, len(cfg.Share.BlockedDomains))
	for _, raw := range cfg.Share.BlockedDomains {
		if domain := NormalizeDomain(raw); domain != "" && !slices.Contains(domains, domain) {
			domains = append(domains, domain)
		}
	}
	cfg.Share.BlockedDomains = domains
	cfg.Share.ShareTemplate = strings.TrimSpace(cfg.Share.ShareTemplate)
	return cfg
}

// ValidateInput 校验形状（引用的套餐与功能是否可用由服务层查）。调用前先 NormalizeInput。
func ValidateInput(input CampaignInput) error {
	if input.Title == "" {
		return fmt.Errorf("请填写活动标题")
	}
	if utf8.RuneCountInString(input.Title) > MaxTitleRunes {
		return fmt.Errorf("活动标题最多 %d 字", MaxTitleRunes)
	}
	if utf8.RuneCountInString(input.Subtitle) > MaxSubtitleRunes {
		return fmt.Errorf("副标题最多 %d 字", MaxSubtitleRunes)
	}
	if utf8.RuneCountInString(input.RulesText) > MaxRulesRunes {
		return fmt.Errorf("活动规则最多 %d 字", MaxRulesRunes)
	}
	if input.StartsAt.IsZero() || input.EndsAt.IsZero() {
		return fmt.Errorf("请设置活动的开始与结束时间")
	}
	if !input.EndsAt.After(input.StartsAt) {
		return fmt.Errorf("结束时间需要晚于开始时间")
	}
	if input.EndsAt.Sub(input.StartsAt) > MaxDurationDays*24*time.Hour {
		return fmt.Errorf("一期活动最长 %d 天", MaxDurationDays)
	}
	return ValidateConfig(input.Config)
}

// ValidateConfig 见 ValidateInput。
func ValidateConfig(cfg Config) error {
	if cfg.ClaimGraceDays < 0 || cfg.ClaimGraceDays > MaxClaimGraceDays {
		return fmt.Errorf("领取期需要在 0–%d 天之间", MaxClaimGraceDays)
	}
	if !cfg.Invite.Enabled && !cfg.Share.Enabled {
		return fmt.Errorf("邀请任务与分享任务至少开启一项")
	}
	if cfg.Invite.Enabled {
		if err := validateInviteTask(cfg.Invite); err != nil {
			return err
		}
	}
	if cfg.Share.Enabled {
		if err := validateShareTask(cfg.Share); err != nil {
			return err
		}
	}
	return nil
}

func validateInviteTask(task InviteTask) error {
	if len(task.Tiers) == 0 {
		return fmt.Errorf("邀请任务至少配置一档奖励")
	}
	if len(task.Tiers) > MaxTiers {
		return fmt.Errorf("邀请任务最多 %d 档", MaxTiers)
	}
	previous := 0
	for _, tier := range task.Tiers {
		if tier.Count < 1 || tier.Count > MaxTierCount {
			return fmt.Errorf("档位人数需要在 1–%d 之间", MaxTierCount)
		}
		if tier.Count == previous {
			return fmt.Errorf("有两档都是邀请 %d 人", tier.Count)
		}
		previous = tier.Count
		if utf8.RuneCountInString(tier.Title) > MaxTierTitleRunes {
			return fmt.Errorf("档位名称最多 %d 字", MaxTierTitleRunes)
		}
		label := fmt.Sprintf("邀请 %d 人的奖励", tier.Count)
		if len(tier.Rewards) == 0 {
			return fmt.Errorf("%s不能为空", label)
		}
		if err := invite.ValidateRewards(label, tier.Rewards); err != nil {
			return err
		}
	}
	if task.Leaderboard && (task.LeaderboardSize < MinLeaderboardSize || task.LeaderboardSize > MaxLeaderboardSize) {
		return fmt.Errorf("排行榜人数需要在 %d–%d 之间", MinLeaderboardSize, MaxLeaderboardSize)
	}
	return nil
}

func validateShareTask(task ShareTask) error {
	if len(task.Platforms) == 0 {
		return fmt.Errorf("分享任务至少启用一个平台")
	}
	for _, key := range task.Platforms {
		if _, ok := FindPlatform(key); !ok {
			return fmt.Errorf("未登记的平台：%s", key)
		}
	}
	if len(task.Rewards) == 0 {
		return fmt.Errorf("分享任务的奖励不能为空")
	}
	if err := invite.ValidateRewards("分享奖励", task.Rewards); err != nil {
		return err
	}
	for _, limit := range []struct {
		value int
		label string
	}{
		{task.MaxApprovedPerUser, "每人通过篇数"},
		{task.MaxApprovedPerPlatform, "每个平台的篇数"},
		{task.DailySubmitLimit, "每天提交次数"},
	} {
		if limit.value < 1 || limit.value > MaxPerUserLimit {
			return fmt.Errorf("%s需要在 1–%d 之间", limit.label, MaxPerUserLimit)
		}
	}
	if len(task.BlockedDomains) > MaxBlockedDomains {
		return fmt.Errorf("禁用域名最多 %d 个", MaxBlockedDomains)
	}
	for _, domain := range task.BlockedDomains {
		if len(domain) > MaxDomainLength || !strings.Contains(domain, ".") {
			return fmt.Errorf("禁用域名格式不对：%s", domain)
		}
		for _, key := range task.Platforms {
			platform, _ := FindPlatform(key)
			if anyHostMatches(domain, platform.Domains) || coveredBy(platform.Domains, domain) {
				return fmt.Errorf("禁用域名 %s 与已启用的平台「%s」冲突", domain, platform.Name)
			}
		}
	}
	if utf8.RuneCountInString(task.ShareTemplate) > MaxShareTemplateRunes {
		return fmt.Errorf("推荐文案最多 %d 字", MaxShareTemplateRunes)
	}
	return nil
}

// coveredBy 平台的某个域名落在 blocked 之下（禁用 qq.com 会把公众号一起禁掉）。
func coveredBy(domains []string, blocked string) bool {
	for _, domain := range domains {
		if hostMatches(domain, blocked) {
			return true
		}
	}
	return false
}

// AllRewards 活动里配置的全部奖励（核对引用用）。
func AllRewards(cfg Config) []Reward {
	out := make([]Reward, 0)
	if cfg.Invite.Enabled {
		for _, tier := range cfg.Invite.Tiers {
			out = append(out, tier.Rewards...)
		}
	}
	if cfg.Share.Enabled {
		out = append(out, cfg.Share.Rewards...)
	}
	return out
}

// RenderShareText 推荐文案：{code} {link} {app} {platform}。
func RenderShareText(template, code, link, app, platform string) string {
	return strings.TrimSpace(strings.NewReplacer(
		"{code}", code, "{link}", link, "{app}", app, "{platform}", platform,
	).Replace(template))
}

// TierTitle 档位在流水与通知里的名字。
func TierTitle(tier Tier) string {
	if tier.Title != "" {
		return tier.Title
	}
	return fmt.Sprintf("邀请 %d 人", tier.Count)
}
