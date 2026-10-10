package vip

import (
	"reflect"
	"testing"
)

// 看广告赠送的会员：权益跟随所属场景的当前配置，与套餐同理。

func boolPtr(value bool) *bool { return &value }

// adSegment 一段看广告领的会员天数：快照是开通时场景的配置，terms 是场景现在的配置。
func adSegment(id int64, snapshot []string, snapshotAdFree *bool, terms *SegmentAdTerms) Segment {
	return Segment{
		ID:            id,
		TransactionNo: "VIP-ad",
		Channel:       ChannelAdReward,
		PlanName:      "看广告赠送",
		Features:      snapshot,
		ActiveFrom:    segmentNow.Add(-day),
		ActiveUntil:   segmentNow.Add(day),
		AdTerms:       terms,
		AdFree:        snapshotAdFree,
	}
}

func TestAdRewardFeaturesFollowCurrentSceneConfig(t *testing.T) {
	catalog := []string{"export", "ai.chat"}
	cases := []struct {
		name    string
		segment Segment
		want    []string
	}{
		{"场景加了功能：已领到的会员立即拿到",
			adSegment(1, []string{}, nil, &SegmentAdTerms{Features: []string{"export", "ai.chat"}}),
			[]string{"ai.chat", "export"}},
		{"场景拿掉了功能：已领到的会员随之失去",
			adSegment(1, []string{"export"}, nil, &SegmentAdTerms{Features: []string{}}),
			[]string{}},
		{"场景被删除：回落到开通时的快照",
			adSegment(1, []string{"export"}, nil, nil),
			[]string{"export"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evaluateSegments([]Segment{tc.segment}, catalog, segmentNow)
			if !reflect.DeepEqual(got.Features, tc.want) {
				t.Fatalf("features = %v, want %v", got.Features, tc.want)
			}
		})
	}

	// 看广告领的「会员套餐」仍按套餐算，场景里那份只对会员天数生效的功能不顶替它
	plan := adSegment(2, []string{"export"}, nil, &SegmentAdTerms{Features: []string{"ai.chat"}})
	plan.PlanID = planID(7)
	plan.Plan = &SegmentPlan{Name: "基础版", Features: []string{"export"}}
	if got := evaluateSegments([]Segment{plan}, catalog, segmentNow); !reflect.DeepEqual(got.Features, []string{"export"}) {
		t.Fatalf("plan segment features = %v, want [export]", got.Features)
	}
	plan.Plan = nil
	if got := evaluateSegments([]Segment{plan}, catalog, segmentNow); !reflect.DeepEqual(got.Features, []string{"export"}) {
		t.Fatalf("deleted plan should fall back to the snapshot, got %v", got.Features)
	}
}

func TestAdRewardAdFree(t *testing.T) {
	cases := []struct {
		name     string
		segments []Segment
		want     bool
	}{
		{"场景设为不免广告", []Segment{adSegment(1, nil, nil, &SegmentAdTerms{AdFree: boolPtr(false)})}, false},
		{"场景设为免广告", []Segment{adSegment(1, nil, nil, &SegmentAdTerms{AdFree: boolPtr(true)})}, true},
		{"场景没设置：按免广告（与此前一致）", []Segment{adSegment(1, nil, nil, &SegmentAdTerms{})}, true},
		{"场景删除：按开通时的快照", []Segment{adSegment(1, nil, boolPtr(false), nil)}, false},
		{"场景删除且无快照（设置出现之前领的）", []Segment{adSegment(1, nil, nil, nil)}, true},
		{"另有一段付费会员：免广告", []Segment{
			adSegment(1, nil, nil, &SegmentAdTerms{AdFree: boolPtr(false)}),
			planSegment(2, segmentNow.Add(-day), segmentNow.Add(10*day), nil, &SegmentPlan{Name: "基础版"}),
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := evaluateSegments(tc.segments, nil, segmentNow)
			if !got.IsVIP || got.AdFree != tc.want {
				t.Fatalf("isVip=%v adFree=%v, want adFree %v", got.IsVIP, got.AdFree, tc.want)
			}
			if got.View().AdFree != tc.want {
				t.Fatal("membership view must carry the same adFree")
			}
		})
	}

	// 不是会员时恒为 false；是会员却找不到任何一段（老系统写进 users 的到期时间）按免广告
	if Evaluate(EvalInput{}, segmentNow).AdFree {
		t.Fatal("non-members are never ad-free")
	}
	end := segmentNow.Add(day)
	if !Evaluate(EvalInput{ExpireAt: &end}, segmentNow).AdFree {
		t.Fatal("legacy members without segments keep the ad-free behaviour")
	}
}
