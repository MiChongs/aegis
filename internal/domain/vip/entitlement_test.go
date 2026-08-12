package vip

import (
	"testing"
	"time"
)

// 会员判定的全部分支都在这里跑一遍。
//
// 之所以值得为一个"看起来只是比大小"的函数写这么多用例：这套判定的错误
// **不会以报错的形式出现**。判错的表现是某个用户的功能突然打不开、
// 或者一个已经付费的人被继续弹"免费试用"，而两者都要等用户投诉才知道。

func ptr(t time.Time) *time.Time { return &t }

func TestEvaluateMembership(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	trialPlan := &TrialPlanRef{ID: 7, Name: "7 天试用", DurationDays: 7}

	trialEnds := now.Add(72 * time.Hour)
	activeTrialClaim := &TrialClaim{
		ID: 1, PlanName: "7 天试用", DurationDays: 7,
		TrialEndsAt: trialEnds, CreatedAt: now.Add(-96 * time.Hour),
	}
	expiredTrialClaim := &TrialClaim{
		ID: 2, PlanName: "7 天试用", DurationDays: 7,
		TrialEndsAt: now.Add(-24 * time.Hour), CreatedAt: now.Add(-8 * 24 * time.Hour),
	}

	cases := []struct {
		name        string
		in          EvalInput
		wantVIP     bool
		wantTrial   bool
		wantSource  string
		wantOffer   bool
		wantReason  string
		wantSeconds int64
	}{
		{
			name:       "从未开通：不是会员，但可以领试用",
			in:         EvalInput{TrialPlan: trialPlan},
			wantOffer:  true,
			wantSource: SourceNone,
			wantReason: TrialReasonEligible,
		},
		{
			name:       "应用没配试用：入口整个不该出现",
			in:         EvalInput{},
			wantSource: SourceNone,
			wantReason: TrialReasonNotConfigured,
		},
		{
			name: "付费会员：不是试用，且不能再领试用",
			in: EvalInput{
				ExpireAt:     ptr(now.Add(240 * time.Hour)),
				LastChannel:  ChannelWallet,
				LastPlanName: "月度会员",
				TrialPlan:    trialPlan,
			},
			wantVIP:     true,
			wantSource:  SourceWallet,
			wantReason:  TrialReasonMemberActive,
			wantSeconds: 240 * 3600,
		},
		{
			name: "试用会员：到期时间恰好是试用发到的那一刻",
			in: EvalInput{
				ExpireAt:     ptr(trialEnds),
				LastChannel:  ChannelTrial,
				LastPlanName: "7 天试用",
				Claim:        activeTrialClaim,
				TrialPlan:    trialPlan,
			},
			wantVIP:     true,
			wantTrial:   true,
			wantSource:  SourceTrial,
			wantReason:  TrialReasonAlreadyClaimed,
			wantSeconds: 72 * 3600,
		},
		{
			name: "试用期内又买了付费：不再算试用，也不再是 trial 来源",
			in: EvalInput{
				// 续期是顺延的，到期时间被推到试用之后 —— 判定据此自动切换
				ExpireAt:     ptr(trialEnds.Add(720 * time.Hour)),
				LastChannel:  ChannelPaymentOrder,
				LastPlanName: "月度会员",
				Claim:        activeTrialClaim,
				TrialPlan:    trialPlan,
			},
			wantVIP:     true,
			wantTrial:   false,
			wantSource:  SourcePaymentOrder,
			wantReason:  TrialReasonAlreadyClaimed,
			wantSeconds: (72 + 720) * 3600,
		},
		{
			name: "试用已过期且没续：不是会员，资格也不会回来",
			in: EvalInput{
				ExpireAt:    ptr(now.Add(-24 * time.Hour)),
				LastChannel: ChannelTrial,
				Claim:       expiredTrialClaim,
				TrialPlan:   trialPlan,
			},
			wantSource: SourceNone,
			wantReason: TrialReasonAlreadyClaimed,
		},
		{
			name: "老数据：是会员但账本里没有对应流水",
			in: EvalInput{
				ExpireAt:  ptr(now.Add(48 * time.Hour)),
				TrialPlan: trialPlan,
			},
			wantVIP:     true,
			wantSource:  SourceUnknown,
			wantReason:  TrialReasonMemberActive,
			wantSeconds: 48 * 3600,
		},
		{
			name: "开了设备去重但请求没带设备标识：拒领而不是放行",
			in: EvalInput{
				TrialPlan:     &TrialPlanRef{ID: 7, Name: "7 天试用", DurationDays: 7, DeviceLimited: true},
				DeviceMissing: true,
			},
			wantSource: SourceNone,
			wantReason: TrialReasonDeviceRequired,
		},
		{
			name: "同一设备已经有人领过",
			in: EvalInput{
				TrialPlan:     &TrialPlanRef{ID: 7, Name: "7 天试用", DurationDays: 7, DeviceLimited: true},
				DeviceClaimed: true,
			},
			wantSource: SourceNone,
			wantReason: TrialReasonDeviceClaimed,
		},
		{
			name: "刚好在到期这一刻：不算会员",
			in: EvalInput{
				ExpireAt:  ptr(now),
				TrialPlan: trialPlan,
			},
			wantSource: SourceNone,
			wantReason: TrialReasonEligible,
			wantOffer:  true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate(tc.in, now)
			if got.IsVIP != tc.wantVIP {
				t.Errorf("isVip = %v，期望 %v", got.IsVIP, tc.wantVIP)
			}
			if got.IsTrial != tc.wantTrial {
				t.Errorf("isTrial = %v，期望 %v", got.IsTrial, tc.wantTrial)
			}
			if got.Source != tc.wantSource {
				t.Errorf("source = %q，期望 %q", got.Source, tc.wantSource)
			}
			if got.TrialOffer.Available != tc.wantOffer {
				t.Errorf("trialOffer.available = %v，期望 %v", got.TrialOffer.Available, tc.wantOffer)
			}
			if got.TrialOffer.Reason != tc.wantReason {
				t.Errorf("trialOffer.reason = %q，期望 %q", got.TrialOffer.Reason, tc.wantReason)
			}
			if got.RemainingSeconds != tc.wantSeconds {
				t.Errorf("remainingSeconds = %d，期望 %d", got.RemainingSeconds, tc.wantSeconds)
			}
		})
	}
}

// 试用历史与"当前是不是试用中"是两件事：领过但已过期的人，
// trial 区块必须还在（客户端要据此把"免费试用"换成"续费"），只是 active 为假。
func TestEvaluateKeepsTrialHistoryAfterExpiry(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	claim := &TrialClaim{
		ID: 3, PlanName: "7 天试用", DurationDays: 7,
		TrialEndsAt: now.Add(-time.Hour), CreatedAt: now.Add(-8 * 24 * time.Hour),
	}

	got := Evaluate(EvalInput{Claim: claim, TrialPlan: &TrialPlanRef{ID: 7, DurationDays: 7}}, now)
	if got.Trial == nil {
		t.Fatal("领取过试用的用户必须保留 trial 区块")
	}
	if got.Trial.Active {
		t.Error("试用已过期，active 应为 false")
	}
	if got.Trial.RemainingSeconds != 0 {
		t.Errorf("过期后剩余秒数应为 0，实际 %d", got.Trial.RemainingSeconds)
	}
	if got.TrialOffer.Available {
		t.Error("试用一人一次，过期后不该恢复资格")
	}
}

// 剩余天数沿用旧口径（向下取整）：控制台与客户端上已有的「还剩 N 天」都是这么算的。
// 改成四舍五入会让所有展示位在同一天集体 +1，而没有任何发布记录解释得了。
func TestRemainingDaysFloorsLikeBefore(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	got := Evaluate(EvalInput{ExpireAt: ptr(now.Add(47*time.Hour + 59*time.Minute))}, now)
	if got.RemainingDays != 1 {
		t.Errorf("remainingDays = %d，期望 1（47h59m 不足两天）", got.RemainingDays)
	}
}

// 每个判据都必须有中文说明：缺一条的表现是接口回一个空 message，
// 而客户端多半会把它原样显示出来。
func TestEveryTrialReasonHasMessage(t *testing.T) {
	reasons := []string{
		TrialReasonEligible, TrialReasonNotConfigured, TrialReasonAlreadyClaimed,
		TrialReasonMemberActive, TrialReasonDeviceClaimed, TrialReasonDeviceRequired,
	}
	for _, reason := range reasons {
		if TrialReasonMessage(reason) == "" {
			t.Errorf("判据 %q 没有中文说明", reason)
		}
	}
}
