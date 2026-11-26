package ticket

import (
	"errors"
	"testing"
	"time"
)

func TestDefaultRewardPolicyIsOffAndValid(t *testing.T) {
	policy := DefaultRewardPolicy(7)
	if policy.Enabled || policy.Configured {
		t.Fatalf("默认策略必须未开启、未配置：%+v", policy)
	}
	if _, err := NormalizeRewardPolicy(policy); err != nil {
		t.Fatalf("推荐默认值本身要能通过校验：%v", err)
	}
	if err := policy.CheckGrant(10, RewardUsage{}); !errors.Is(err, ErrRewardDisabled) {
		t.Fatalf("未开启时不能发，得到 %v", err)
	}
}

func TestNormalizeRewardPolicy(t *testing.T) {
	cases := []struct {
		name   string
		policy RewardPolicy
		ok     bool
	}{
		{"负数", RewardPolicy{DefaultPoints: -1}, false},
		{"超过总上限", RewardPolicy{MaxPoints: RewardMaxPoints + 1}, false},
		{"默认值超过单条上限", RewardPolicy{DefaultPoints: 600, MaxPoints: 500}, false},
		{"分类超过单条上限", RewardPolicy{MaxPoints: 100, CategoryPoints: map[string]int64{"feedback_bug": 200}}, false},
		{"日上限大于月上限", RewardPolicy{UserDailyLimit: 300, UserMonthlyLimit: 200}, false},
		{"空分类键", RewardPolicy{CategoryPoints: map[string]int64{" ": 10}}, false},
		{"不限上限", RewardPolicy{DefaultPoints: 1_000_000}, true},
		{"正常", RewardPolicy{DefaultPoints: 20, MaxPoints: 100, UserDailyLimit: 100, UserMonthlyLimit: 300,
			CategoryPoints: map[string]int64{"feedback_bug": 50}}, true},
	}
	for _, tc := range cases {
		_, err := NormalizeRewardPolicy(tc.policy)
		if (err == nil) != tc.ok {
			t.Errorf("%s：期望通过=%v，得到 %v", tc.name, tc.ok, err)
		}
		var policyErr *RewardPolicyError
		if err != nil && !errors.As(err, &policyErr) {
			t.Errorf("%s：校验错误应为 RewardPolicyError，得到 %T", tc.name, err)
		}
	}

	out, err := NormalizeRewardPolicy(RewardPolicy{CategoryPoints: map[string]int64{" feedback_bug ": 30, "feedback_other": 0}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.CategoryPoints) != 1 || out.CategoryPoints["feedback_bug"] != 30 {
		t.Fatalf("分类键应去空白、0 值项去掉：%v", out.CategoryPoints)
	}
}

func TestSuggestPoints(t *testing.T) {
	policy := RewardPolicy{DefaultPoints: 20, MaxPoints: 40, CategoryPoints: map[string]int64{"feedback_bug": 50, "feedback_feature": 30}}
	if got := policy.SuggestPoints("feedback_feature"); got != 30 {
		t.Fatalf("配了分类用分类的值，得到 %d", got)
	}
	if got := policy.SuggestPoints("feedback_other"); got != 20 {
		t.Fatalf("没配的分类用默认值，得到 %d", got)
	}
	if got := policy.SuggestPoints("feedback_bug"); got != 40 {
		t.Fatalf("建议值不超过单条上限，得到 %d", got)
	}
}

func TestCheckGrantLimits(t *testing.T) {
	policy := RewardPolicy{Enabled: true, MaxPoints: 100, UserDailyLimit: 150, UserMonthlyLimit: 400}
	var limit *RewardLimitError
	if err := policy.CheckGrant(0, RewardUsage{}); !errors.Is(err, ErrRewardPoints) {
		t.Fatalf("0 积分不能发，得到 %v", err)
	}
	if err := policy.CheckGrant(101, RewardUsage{}); !errors.As(err, &limit) || limit.Scope != "single" {
		t.Fatalf("超过单条上限，得到 %v", err)
	}
	if err := policy.CheckGrant(60, RewardUsage{Daily: 100, Monthly: 100}); !errors.As(err, &limit) || limit.Scope != "daily" || limit.Remaining != 50 {
		t.Fatalf("超过当日上限、余 50，得到 %v", err)
	}
	if err := policy.CheckGrant(50, RewardUsage{Daily: 100, Monthly: 100}); err != nil {
		t.Fatalf("正好用满当日上限可以发，得到 %v", err)
	}
	if err := policy.CheckGrant(60, RewardUsage{Daily: 0, Monthly: 380}); !errors.As(err, &limit) || limit.Scope != "monthly" || limit.Remaining != 20 {
		t.Fatalf("超过当月上限、余 20，得到 %v", err)
	}
	if err := (RewardPolicy{Enabled: true}).CheckGrant(RewardMaxPoints, RewardUsage{Daily: 1 << 40}); err != nil {
		t.Fatalf("不限上限时只受总上限约束，得到 %v", err)
	}
}

func TestMaxGrantable(t *testing.T) {
	if got := (RewardPolicy{}).MaxGrantable(RewardUsage{}); got != nil {
		t.Fatalf("全都不限时为 nil，得到 %d", *got)
	}
	policy := RewardPolicy{MaxPoints: 100, UserDailyLimit: 150, UserMonthlyLimit: 400}
	if got := policy.MaxGrantable(RewardUsage{Daily: 120, Monthly: 120}); got == nil || *got != 30 {
		t.Fatalf("取三档最小（当日余 30），得到 %v", got)
	}
	if got := policy.MaxGrantable(RewardUsage{Daily: 200, Monthly: 200}); got == nil || *got != 0 {
		t.Fatalf("超额的存量不会变成负数，得到 %v", got)
	}
}

func TestRewardIneligibleReason(t *testing.T) {
	userID := int64(9)
	base := func() *Ticket {
		return &Ticket{Kind: KindFeedback, AppID: 1, RequesterType: RequesterUser, RequesterUserID: &userID, Status: StatusProcessing}
	}
	if reason := RewardIneligibleReason(base()); reason != "" {
		t.Fatalf("处理中的用户反馈可以发，得到 %q", reason)
	}
	for _, mutate := range []func(*Ticket){
		func(item *Ticket) { item.Kind = KindTicket },
		func(item *Ticket) { item.RequesterType = RequesterAdmin; item.RequesterUserID = nil },
		func(item *Ticket) { item.RequesterUserID = nil },
		func(item *Ticket) { item.Status = StatusCancelled },
		func(item *Ticket) { item.Status = StatusRejected },
	} {
		item := base()
		mutate(item)
		if RewardIneligibleReason(item) == "" {
			t.Errorf("不该能发：%+v", item)
		}
	}
	for _, status := range []string{StatusResolved, StatusClosed, StatusOpen, StatusPending} {
		item := base()
		item.Status = status
		if reason := RewardIneligibleReason(item); reason != "" {
			t.Errorf("%s 的反馈可以发，得到 %q", status, reason)
		}
	}
	if RewardableStatusTarget(StatusRejected) || RewardableStatusTarget(StatusCancelled) || !RewardableStatusTarget(StatusResolved) {
		t.Fatal("改为不采纳 / 已撤销时不能同时发奖励，改为已解决可以")
	}
}

func TestRewardPeriodStarts(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*3600)
	// UTC 1 月 31 日 17:30 = 东八区 2 月 1 日 01:30
	day, month := RewardPeriodStarts(time.Date(2026, 1, 31, 17, 30, 0, 0, time.UTC), loc)
	if !day.Equal(time.Date(2026, 2, 1, 0, 0, 0, 0, loc)) || !month.Equal(time.Date(2026, 2, 1, 0, 0, 0, 0, loc)) {
		t.Fatalf("按应用时区切日与切月，得到 %v / %v", day, month)
	}
}
