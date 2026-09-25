package vip

import (
	"reflect"
	"testing"
	"time"
)

// 会员权益跟随套餐。
//
// 这组用例钉住的是此前那个"改了套餐、老用户纹丝不动"的缺陷：功能按开通时的快照判定，
// 运营给套餐加功能老用户拿不到，把功能拿掉（降级）老用户照用不误。

var segmentNow = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

const day = 24 * time.Hour

func planID(id int64) *int64 { return &id }

// planSegment 一段按套餐开通的会员期：快照是开通时的配置，Plan 是套餐现在的配置。
func planSegment(id int64, from, until time.Time, snapshot []string, current *SegmentPlan) Segment {
	return Segment{
		ID:            id,
		TransactionNo: "VIP-plan",
		Channel:       ChannelWallet,
		PlanID:        planID(100 + id),
		PlanName:      "开通时的名字",
		Features:      snapshot,
		Plan:          current,
		ActiveFrom:    from,
		ActiveUntil:   until,
	}
}

func evaluateSegments(segments []Segment, catalog []string, now time.Time) Entitlement {
	end := now
	for _, segment := range segments {
		if segment.ActiveUntil.After(end) {
			end = segment.ActiveUntil
		}
	}
	return Evaluate(EvalInput{ExpireAt: &end, Segments: segments, FeatureCatalog: catalog}, now)
}

func TestFeaturesFollowCurrentPlanConfig(t *testing.T) {
	now := segmentNow
	catalog := []string{"export", "ai.chat", "hd_video"}

	cases := []struct {
		name     string
		segment  Segment
		want     []string
		wantName string
	}{
		{
			name: "套餐加了功能：老用户立即拿到",
			segment: planSegment(1, now.Add(-day), now.Add(29*day), []string{"export"},
				&SegmentPlan{Name: "高级版", Features: []string{"export", "ai.chat"}}),
			want:     []string{"ai.chat", "export"},
			wantName: "高级版",
		},
		{
			name: "套餐拿掉了功能（降级）：老用户立即失去",
			segment: planSegment(1, now.Add(-day), now.Add(29*day), []string{"export", "ai.chat"},
				&SegmentPlan{Name: "高级版", Features: []string{"export"}}),
			want:     []string{"export"},
			wantName: "高级版",
		},
		{
			name: "套餐清空功能：仍是会员，只是不带细分权益",
			segment: planSegment(1, now.Add(-day), now.Add(29*day), []string{"export"},
				&SegmentPlan{Name: "高级版", Features: []string{}}),
			want:     []string{},
			wantName: "高级版",
		},
		{
			name:     "套餐已删除：回落到快照（删除时已定格成最后的配置）",
			segment:  planSegment(1, now.Add(-day), now.Add(29*day), []string{"export"}, nil),
			want:     []string{"export"},
			wantName: "开通时的名字",
		},
		{
			name: "套餐改名：当前套餐名跟着改",
			segment: planSegment(1, now.Add(-day), now.Add(29*day), []string{"export"},
				&SegmentPlan{Name: "Pro 年度版", Features: []string{"export"}}),
			want:     []string{"export"},
			wantName: "Pro 年度版",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evaluateSegments([]Segment{tc.segment}, catalog, now)
			if !got.IsVIP {
				t.Fatal("应当是会员")
			}
			if !reflect.DeepEqual(got.Features, tc.want) {
				t.Errorf("features = %v，期望 %v", got.Features, tc.want)
			}
			if got.PlanName != tc.wantName {
				t.Errorf("planName = %q，期望 %q", got.PlanName, tc.wantName)
			}
		})
	}
}

// 停用 / 删除功能标识：所有人同时失去，与服务端校验的结论一致。
// 否则 /vip/status 列着一个 verify 判不通过的功能，客户端会亮起一个点了没用的入口。
func TestFeaturesLimitedToEnabledCatalog(t *testing.T) {
	now := segmentNow
	segments := []Segment{planSegment(1, now.Add(-day), now.Add(29*day), nil,
		&SegmentPlan{Name: "高级版", Features: []string{"export", "ai.chat"}})}

	got := evaluateSegments(segments, []string{"export"}, now)
	if !reflect.DeepEqual(got.Features, []string{"export"}) {
		t.Errorf("停用的 ai.chat 不该出现在功能集合里：%v", got.Features)
	}
	if got.HasFeature("ai.chat") {
		t.Error("停用的功能 HasFeature 必须为假")
	}

	empty := evaluateSegments(segments, nil, now)
	if !empty.IsVIP || len(empty.Features) != 0 {
		t.Errorf("目录为空时仍是会员、但没有任何功能：isVip=%v features=%v", empty.IsVIP, empty.Features)
	}
}

// 用户自己换档：会员期顺延，两段按先后排在链上。
func TestStackedSegmentsUpgradeAndDowngrade(t *testing.T) {
	now := segmentNow
	catalog := []string{"export", "ai.chat"}
	basic := &SegmentPlan{Name: "基础版", Features: []string{"export"}}
	pro := &SegmentPlan{Name: "高级版", Features: []string{"export", "ai.chat"}}

	t.Run("先基础版后高级版：高级版的功能当场生效", func(t *testing.T) {
		segments := []Segment{
			planSegment(1, now.Add(-10*day), now.Add(20*day), nil, basic),
			planSegment(2, now.Add(20*day), now.Add(50*day), nil, pro),
		}
		got := evaluateSegments(segments, catalog, now)
		if !got.HasFeature("ai.chat") {
			t.Errorf("付了高级版的钱，现在就该能用：%v", got.Features)
		}
		if got.PlanName != "高级版" {
			t.Errorf("当前套餐取最近开通的那段，得到 %q", got.PlanName)
		}
	})

	t.Run("先高级版后基础版：高级版那段用完就只剩基础版", func(t *testing.T) {
		segments := []Segment{
			planSegment(1, now.Add(-10*day), now.Add(20*day), nil, pro),
			planSegment(2, now.Add(20*day), now.Add(50*day), nil, basic),
		}
		during := evaluateSegments(segments, catalog, now)
		if !during.HasFeature("ai.chat") {
			t.Errorf("高级版那段还没用完：%v", during.Features)
		}
		later := now.Add(25 * day)
		after := Evaluate(EvalInput{ExpireAt: ptr(now.Add(50 * day)), Segments: segments, FeatureCatalog: catalog}, later)
		if after.HasFeature("ai.chat") {
			t.Errorf("高级版那段已结束，降级应当生效：%v", after.Features)
		}
		if !after.HasFeature("export") {
			t.Errorf("基础版的功能仍在：%v", after.Features)
		}
	})
}

// 扣减把排在最后的那段整段截掉之后，它的窗口收成空，不再贡献功能。
func TestTruncatedSegmentNoLongerCounts(t *testing.T) {
	now := segmentNow
	basic := planSegment(1, now.Add(-10*day), now.Add(5*day), nil, &SegmentPlan{Name: "基础版", Features: []string{"export"}})
	// 起点在截断点之后：active_until 被收到 active_from，窗口为空
	pro := planSegment(2, now.Add(20*day), now.Add(20*day), nil, &SegmentPlan{Name: "高级版", Features: []string{"export", "ai.chat"}})

	got := Evaluate(EvalInput{
		ExpireAt:       ptr(now.Add(5 * day)),
		Segments:       []Segment{basic, pro},
		FeatureCatalog: []string{"export", "ai.chat"},
	}, now)
	if got.HasFeature("ai.chat") {
		t.Errorf("被扣掉的高级版不该再贡献功能：%v", got.Features)
	}
	if got.PlanName != "基础版" {
		t.Errorf("被截掉的段也不该再是当前套餐，得到 %q", got.PlanName)
	}
}

// 扣减过天数的试用：到期时间不再等于试用发到的时刻，但最近一段仍是试用 —— 仍算试用。
func TestTrialStillTrialAfterDeduction(t *testing.T) {
	now := segmentNow
	claim := &TrialClaim{ID: 1, PlanName: "7 天试用", DurationDays: 7,
		TrialEndsAt: now.Add(5 * day), CreatedAt: now.Add(-2 * day)}
	got := Evaluate(EvalInput{
		ExpireAt: ptr(now.Add(3 * day)),
		Segments: []Segment{segment(1, ChannelTrial, "7 天试用", claim.CreatedAt, now.Add(3*day))},
		Claim:    claim,
	}, now)
	if !got.IsTrial || got.Source != SourceTrial {
		t.Errorf("扣减过天数的试用仍是试用：isTrial=%v source=%q", got.IsTrial, got.Source)
	}
}

// 卡密核销开出来的会员有自己的来源，不能落进「来源未知」——
// 那一档的意思是账本里没有流水，会让对账的人以为这是老系统迁移的数据。
func TestCardKeySourceIsNamed(t *testing.T) {
	now := segmentNow
	got := evaluateSegments([]Segment{segment(1, ChannelCardKey, "卡密赠送", now, now.Add(7*day))}, nil, now)
	if got.Source != SourceCardKey {
		t.Errorf("source = %q，期望 %q", got.Source, SourceCardKey)
	}
}

// 是会员但没有任何一段仍生效的开通（老系统直接写进 users 的到期时间）：
// 来源说不清就说不清，也不凭空给功能。
func TestMemberWithoutSegmentsIsUnknown(t *testing.T) {
	now := segmentNow
	got := Evaluate(EvalInput{
		ExpireAt:       ptr(now.Add(10 * day)),
		Segments:       []Segment{segment(1, ChannelWallet, "早已用完的一段", now.Add(-40*day), now.Add(-10*day), "export")},
		FeatureCatalog: []string{"export"},
	}, now)
	if got.Source != SourceUnknown || got.PlanName != "" {
		t.Errorf("source = %q planName = %q，期望 unknown 且无套餐名", got.Source, got.PlanName)
	}
	if len(got.Features) != 0 {
		t.Errorf("已结束的段不该贡献功能：%v", got.Features)
	}
}
