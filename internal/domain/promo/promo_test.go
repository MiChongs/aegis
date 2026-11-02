package promo

import (
	"strings"
	"testing"
	"time"

	"aegis/internal/domain/invite"
)

func points(amount int64) []Reward { return []Reward{{Type: invite.RewardPoints, Amount: amount}} }

func sampleCampaign(now time.Time) Campaign {
	cfg := DefaultConfig()
	cfg.Invite.Tiers = []Tier{{Count: 3, Rewards: points(100)}, {Count: 1, Rewards: points(10)}}
	cfg.Share.Rewards = points(50)
	cfg = NormalizeConfig(cfg)
	return Campaign{
		ID: 1, Status: StatusOnline, Title: "推广季",
		StartsAt: now.Add(-24 * time.Hour), EndsAt: now.Add(24 * time.Hour), Config: cfg,
	}
}

func TestNormalizeAndValidate(t *testing.T) {
	now := time.Now()
	campaign := sampleCampaign(now)
	if campaign.Config.Invite.Tiers[0].Count != 1 {
		t.Fatalf("tiers should be sorted ascending: %+v", campaign.Config.Invite.Tiers)
	}
	input := NormalizeInput(CampaignInput{Title: " 推广季 ", StartsAt: campaign.StartsAt, EndsAt: campaign.EndsAt, Config: campaign.Config})
	if err := ValidateInput(input); err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}

	bad := input
	bad.Config.Invite.Tiers = append(bad.Config.Invite.Tiers, Tier{Count: 3, Rewards: points(1)})
	bad.Config = NormalizeConfig(bad.Config)
	if err := ValidateInput(bad); err == nil || !strings.Contains(err.Error(), "两档") {
		t.Fatalf("duplicate tier count should fail, got %v", err)
	}

	bad = input
	bad.Config.Share.BlockedDomains = []string{"https://qq.com/x"}
	bad.Config = NormalizeConfig(bad.Config)
	if bad.Config.Share.BlockedDomains[0] != "qq.com" {
		t.Fatalf("domain not normalized: %v", bad.Config.Share.BlockedDomains)
	}
	if err := ValidateInput(bad); err == nil || !strings.Contains(err.Error(), "冲突") {
		t.Fatalf("blocking qq.com conflicts with 公众号, got %v", err)
	}

	bad = input
	bad.Config.Invite.Enabled, bad.Config.Share.Enabled = false, false
	if err := ValidateInput(bad); err == nil {
		t.Fatal("campaign with no task should fail")
	}

	bad = input
	bad.EndsAt = bad.StartsAt.Add((MaxDurationDays + 1) * 24 * time.Hour)
	if err := ValidateInput(bad); err == nil {
		t.Fatal("too long campaign should fail")
	}
}

func TestParseLink(t *testing.T) {
	xhs, _ := FindPlatform("xiaohongshu")
	mp, _ := FindPlatform("wechat_mp")
	blocked := DefaultBlockedDomains()
	cases := []struct {
		raw      string
		platform Platform
		reason   string
		key      string
	}{
		{"https://www.xiaohongshu.com/explore/abc?xsec_token=1#c", xhs, "", "xiaohongshu.com/explore/abc"},
		{"http://xhslink.com/a/Xyz", xhs, "", "xhslink.com/a/Xyz"},
		{"https://evilxiaohongshu.com/explore/abc", xhs, ReasonLinkPlatformMismatch, ""},
		{"https://www.douyin.com/video/1", xhs, ReasonLinkForbidden, ""},
		{"https://v.douyin.com/abc/", mp, ReasonLinkForbidden, ""},
		{"https://www.iesdouyin.com/share/video/1", xhs, ReasonLinkForbidden, ""},
		{"https://v.kuaishou.com/x", xhs, ReasonLinkForbidden, ""},
		{"ftp://xiaohongshu.com/a", xhs, ReasonLinkInvalid, ""},
		{"xiaohongshu.com/a", xhs, ReasonLinkInvalid, ""},
		{"https://xiaohongshu.com/", xhs, ReasonLinkInvalid, ""},
		{"https://mp.weixin.qq.com/s/AbCdEf", mp, "", "mp.weixin.qq.com/s/AbCdEf"},
		{"https://mp.weixin.qq.com/s?__biz=MzA&mid=1&idx=1&sn=ff&chksm=zz", mp, "", "mp.weixin.qq.com/s?__biz=MzA&mid=1&idx=1&sn=ff"},
		{"https://mp.weixin.qq.com/s?chksm=zz", mp, ReasonLinkInvalid, ""},
	}
	for _, tc := range cases {
		link, reason := ParseLink(tc.raw, tc.platform, blocked)
		if reason != tc.reason {
			t.Errorf("%s: reason %q, want %q", tc.raw, reason, tc.reason)
			continue
		}
		if tc.key != "" && link.Key != tc.key {
			t.Errorf("%s: key %q, want %q", tc.raw, link.Key, tc.key)
		}
	}
	// 写死的禁用平台不受运营配置影响
	if _, reason := ParseLink("https://www.douyin.com/video/1", xhs, nil); reason != ReasonLinkForbidden {
		t.Fatalf("douyin must stay forbidden without blocked list, got %q", reason)
	}
}

func TestEvaluateTier(t *testing.T) {
	now := time.Now()
	campaign := sampleCampaign(now)
	if reason := EvaluateTier(campaign, 3, TierFacts{Count: 2}, now); reason != ReasonTierLocked {
		t.Fatalf("got %q", reason)
	}
	if reason := EvaluateTier(campaign, 3, TierFacts{Count: 3}, now); reason != "" {
		t.Fatalf("got %q", reason)
	}
	if reason := EvaluateTier(campaign, 3, TierFacts{Count: 5, Claimed: true}, now); reason != ReasonTierClaimed {
		t.Fatalf("got %q", reason)
	}
	if reason := EvaluateTier(campaign, 2, TierFacts{Count: 5}, now); reason != ReasonTierNotFound {
		t.Fatalf("got %q", reason)
	}
	// 结束后领取期内仍可领，过了领取期不行
	ended := campaign
	ended.EndsAt = now.Add(-time.Hour)
	if reason := EvaluateTier(ended, 3, TierFacts{Count: 3}, now); reason != "" {
		t.Fatalf("grace claim got %q", reason)
	}
	if state := TierState(ended, ended.Config.Invite.Tiers[1], 2, false, now); state != TierExpired {
		t.Fatalf("unreachable tier after end should be expired, got %q", state)
	}
	ended.Config.ClaimGraceDays = 0
	if reason := EvaluateTier(ended, 3, TierFacts{Count: 3}, now); reason != ReasonEnded {
		t.Fatalf("got %q", reason)
	}
	upcoming := campaign
	upcoming.StartsAt = now.Add(time.Hour)
	if reason := EvaluateTier(upcoming, 1, TierFacts{Count: 1}, now); reason != ReasonNotStarted {
		t.Fatalf("got %q", reason)
	}
}

func TestEvaluateSubmit(t *testing.T) {
	now := time.Now()
	campaign := sampleCampaign(now)
	if reason := EvaluateSubmit(campaign, "zhihu", "", SubmitFacts{}, now); reason != ReasonPlatformUnavailable {
		t.Fatalf("zhihu not enabled by default, got %q", reason)
	}
	if reason := EvaluateSubmit(campaign, "xiaohongshu", ReasonLinkForbidden, SubmitFacts{}, now); reason != ReasonLinkForbidden {
		t.Fatalf("got %q", reason)
	}
	if reason := EvaluateSubmit(campaign, "xiaohongshu", "", SubmitFacts{PlatformTaken: 1}, now); reason != ReasonPlatformLimited {
		t.Fatalf("got %q", reason)
	}
	if reason := EvaluateSubmit(campaign, "coolapk", "", SubmitFacts{Approved: 2, Pending: 1}, now); reason != ReasonUserLimited {
		t.Fatalf("got %q", reason)
	}
	if reason := EvaluateSubmit(campaign, "coolapk", "", SubmitFacts{TodaySubmitted: 5}, now); reason != ReasonDailyLimited {
		t.Fatalf("got %q", reason)
	}
	if reason := EvaluateSubmit(campaign, "coolapk", "", SubmitFacts{Duplicate: true}, now); reason != ReasonLinkDuplicate {
		t.Fatalf("got %q", reason)
	}
	if reason := EvaluateSubmit(campaign, "coolapk", "", SubmitFacts{}, now); reason != "" {
		t.Fatalf("got %q", reason)
	}
	ended := campaign
	ended.EndsAt = now.Add(-time.Minute)
	if reason := EvaluateSubmit(ended, "coolapk", "", SubmitFacts{}, now); reason != ReasonEnded {
		t.Fatalf("submissions close at endsAt, got %q", reason)
	}
	if reason := EvaluateApprove(campaign.Config.Share, 0, 1); reason != ReasonPlatformLimited {
		t.Fatalf("got %q", reason)
	}
}

func TestForbiddenAlwaysListed(t *testing.T) {
	names := ForbiddenNames()
	if len(names) == 0 || names[0] != "抖音" {
		t.Fatalf("抖音 must be the first forbidden platform: %v", names)
	}
	for _, platform := range PlatformCatalog() {
		for _, domain := range platform.Domains {
			if IsForbiddenHost(domain) {
				t.Fatalf("platform %s uses a forbidden domain %s", platform.Key, domain)
			}
		}
	}
}
