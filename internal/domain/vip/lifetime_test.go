package vip

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// 永久会员的判定。
//
// 永久与限时是并列的两条线，最容易错的是两条线同时存在的时候：
// 永久会员另买一段限时高级版、试用期间开通永久、永久会员没有任何开通记录（老系统迁移）。

// lifetimeSegment 造一笔永久开通（没有终点）。
func lifetimeSegment(id int64, channel string, name string, from time.Time, features ...string) Segment {
	return Segment{
		ID:            id,
		TransactionNo: "VIPL" + channel,
		Channel:       channel,
		PlanName:      name,
		Features:      features,
		ActiveFrom:    from,
		Lifetime:      true,
	}
}

func TestLifetimeMemberHasNoExpiry(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	since := now.Add(-30 * 24 * time.Hour)

	got := Evaluate(EvalInput{
		LifetimeSince:  &since,
		Segments:       []Segment{lifetimeSegment(1, ChannelWallet, "永久会员", since, "export")},
		FeatureCatalog: []string{"export"},
		TrialPlan:      &TrialPlanRef{ID: 7, Name: "7 天试用", DurationDays: 7},
	}, now)

	if !got.IsVIP || !got.IsLifetime {
		t.Fatalf("永久会员应判为会员且 isLifetime：%+v", got)
	}
	if got.ExpireAt != nil || got.RemainingSeconds != 0 || got.RemainingDays != 0 {
		t.Fatalf("永久会员不该有到期时间与剩余时长：%+v", got)
	}
	if got.LifetimeSince == nil || !got.LifetimeSince.Equal(since) {
		t.Fatalf("lifetimeSince 应为成为永久会员的时间，得到 %v", got.LifetimeSince)
	}
	if got.Source != SourceWallet || got.PlanName != "永久会员" {
		t.Fatalf("来源与套餐应取永久开通那一笔，得到 %s / %s", got.Source, got.PlanName)
	}
	if len(got.Features) != 1 || got.Features[0] != "export" {
		t.Fatalf("永久开通的功能应生效，得到 %v", got.Features)
	}
	if got.TrialOffer.Available || got.TrialOffer.Reason != TrialReasonMemberActive {
		t.Fatalf("永久会员不能再领试用，得到 %+v", got.TrialOffer)
	}
}

// 永久基础版 + 一段限时高级版：两边的功能都在，身份仍是永久基础版，
// 高级版的到期时间单独给出；高级版到期后只剩基础版的功能，人仍是永久会员。
func TestLifetimeWithTimedUpgrade(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	since := now.Add(-60 * 24 * time.Hour)
	timedEnds := now.Add(10 * 24 * time.Hour)
	in := EvalInput{
		ExpireAt:      &timedEnds,
		LifetimeSince: &since,
		Segments: []Segment{
			lifetimeSegment(1, ChannelPaymentOrder, "永久基础版", since, "export"),
			segment(2, ChannelWallet, "高级版月卡", now.Add(-20*24*time.Hour), timedEnds, "ai.chat", "export"),
		},
		FeatureCatalog: []string{"ai.chat", "export"},
	}

	got := Evaluate(in, now)
	if !got.IsLifetime || got.PlanName != "永久基础版" || got.Source != SourcePaymentOrder {
		t.Fatalf("身份应是永久基础版，得到 %+v", got)
	}
	if got.TimedExpireAt == nil || !got.TimedExpireAt.Equal(timedEnds) {
		t.Fatalf("应给出限时高级版的到期时间，得到 %v", got.TimedExpireAt)
	}
	if len(got.Features) != 2 {
		t.Fatalf("两条线的功能应取并集，得到 %v", got.Features)
	}

	later := Evaluate(in, timedEnds.Add(time.Hour))
	if !later.IsVIP || !later.IsLifetime {
		t.Fatalf("限时那段到期后仍是永久会员，得到 %+v", later)
	}
	if later.TimedExpireAt != nil {
		t.Fatalf("限时那段到期后不该再给 timedExpireAt，得到 %v", later.TimedExpireAt)
	}
	if len(later.Features) != 1 || later.Features[0] != "export" {
		t.Fatalf("高级版到期后只剩永久基础版的功能，得到 %v", later.Features)
	}
}

// 试用期间开通永久：到期时间仍等于试用发到的那一刻（永久不动它），但已经不是试用会员了。
func TestLifetimeDuringTrialIsNotTrial(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	trialEnds := now.Add(72 * time.Hour)
	since := now.Add(-time.Hour)
	got := Evaluate(EvalInput{
		ExpireAt:      &trialEnds,
		LifetimeSince: &since,
		Segments: []Segment{
			segment(1, ChannelTrial, "7 天试用", now.Add(-96*time.Hour), trialEnds),
			lifetimeSegment(2, ChannelWallet, "永久会员", since),
		},
		Claim: &TrialClaim{ID: 1, PlanName: "7 天试用", DurationDays: 7, TrialEndsAt: trialEnds,
			CreatedAt: now.Add(-96 * time.Hour)},
		TrialPlan: &TrialPlanRef{ID: 7, Name: "7 天试用", DurationDays: 7},
	}, now)

	if got.IsTrial || got.Source != SourceWallet {
		t.Fatalf("开通永久之后不该再算试用，得到 isTrial=%v source=%s", got.IsTrial, got.Source)
	}
	if got.Trial == nil || !got.Trial.Active {
		t.Fatalf("试用历史仍应保留，得到 %+v", got.Trial)
	}
}

// 老系统迁移进来的永久会员没有任何开通记录：仍是永久会员，来源说不清。
func TestLifetimeWithoutSegmentsIsUnknown(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	since := now.Add(-time.Hour)
	got := Evaluate(EvalInput{LifetimeSince: &since}, now)
	if !got.IsVIP || !got.IsLifetime || got.Source != SourceUnknown {
		t.Fatalf("没有开通记录的永久会员应判为来源不明，得到 %+v", got)
	}
}

// 作废的永久开通不会进到判定事实里；永久身份由 users.vip_lifetime_at 决定，
// 取消永久会员时两者同时清掉。这里钉住「没有 lifetimeSince 就不是永久会员」——
// 即使事实里混进了一笔永久开通（例如缓存里的旧数据），也不能凭它判成会员。
func TestLifetimeSegmentAloneIsNotMembership(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	got := Evaluate(EvalInput{
		Segments:       []Segment{lifetimeSegment(1, ChannelAdminGrant, "永久会员", now.Add(-time.Hour), "export")},
		FeatureCatalog: []string{"export"},
	}, now)
	if got.IsVIP || got.IsLifetime || len(got.Features) != 0 {
		t.Fatalf("没有永久身份时不该判为会员，得到 %+v", got)
	}
}

func TestLifetimeSegmentIsAlwaysLive(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	if !lifetimeSegment(1, ChannelWallet, "永久", now.Add(-24*time.Hour)).LiveAt(now.AddDate(50, 0, 0)) {
		t.Fatal("永久开通没有终点，任何时刻都应生效")
	}
}

func TestLifetimeIncludes(t *testing.T) {
	plan := func(lifetime bool, features ...string) Plan {
		days := 30
		if lifetime {
			days = 0
		}
		return Plan{Kind: KindPaid, Lifetime: lifetime, DurationDays: days, Features: features, Price: decimal.NewFromInt(30)}
	}
	cases := []struct {
		name     string
		lifetime bool
		have     []string
		plan     Plan
		want     bool
	}{
		{"不是永久会员：什么都能买", false, []string{"export"}, plan(false, "export"), false},
		{"永久会员续同档月卡：已包含", true, []string{"export"}, plan(false, "export"), true},
		{"同一个永久套餐买第二次：已包含", true, []string{"export"}, plan(true, "Export "), true},
		{"永久会员升级到含新功能的永久套餐：放行", true, []string{"export"}, plan(true, "ai.chat", "export"), false},
		{"永久基础版买一段限时高级版：放行", true, []string{"export"}, plan(false, "ai.chat"), false},
		{"不带功能的套餐对永久会员没有意义", true, nil, plan(false), true},
		{"试用对永久会员一律视为已包含", true, nil, Plan{Kind: KindTrial, DurationDays: 7, Features: []string{"ai.chat"}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := LifetimeIncludes(tc.lifetime, tc.have, tc.plan); got != tc.want {
				t.Fatalf("LifetimeIncludes = %v，期望 %v", got, tc.want)
			}
		})
	}
}

func TestPlanTermLabel(t *testing.T) {
	if got := (Plan{Lifetime: true}).TermLabel(); got != "永久" {
		t.Fatalf("永久套餐应显示「永久」，得到 %q", got)
	}
	if got := (Plan{DurationDays: 30}).TermLabel(); got != "30 天" {
		t.Fatalf("限时套餐应显示天数，得到 %q", got)
	}
}

func TestViewCarriesLifetime(t *testing.T) {
	since := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	view := Entitlement{IsVIP: true, IsLifetime: true, LifetimeSince: &since}.View()
	if !view.IsLifetime || view.LifetimeSince == nil || !view.LifetimeSince.Equal(since) {
		t.Fatalf("服务端校验的投影应带上永久会员，得到 %+v", view)
	}
}
