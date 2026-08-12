package vip

import (
	"reflect"
	"testing"
	"time"
)

func TestNormalizeFeatureTags(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"空输入给空数组而不是 nil", nil, []string{}},
		{"去空白与大小写", []string{" Export ", "AI.Chat"}, []string{"ai.chat", "export"}},
		{"去重", []string{"export", "export", "EXPORT"}, []string{"export"}},
		{"丢掉空串", []string{"", "  ", "export"}, []string{"export"}},
		// 顺序不定会让两次保存产生不同的数组，diff 与幂等判断都跟着失真
		{"结果有序", []string{"z", "a", "m"}, []string{"a", "m", "z"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeFeatureTags(tc.in); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("NormalizeFeatureTags(%v) = %v，期望 %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestFeatureTagPattern(t *testing.T) {
	valid := []string{"export", "ai.chat", "hd_video", "pro-tier", "a1"}
	invalid := []string{"", "a", "Export", "1export", "ai chat", "导出", "export!", "_export"}

	for _, tag := range valid {
		if !FeatureTagPattern.MatchString(tag) {
			t.Errorf("%q 应当是合法的功能标识", tag)
		}
	}
	for _, tag := range invalid {
		if FeatureTagPattern.MatchString(tag) {
			t.Errorf("%q 不该被当成合法的功能标识", tag)
		}
	}
}

// 功能权益必须以「是不是会员」为前提。
//
// 过期用户的功能快照仍留在账本里 —— 只按标签命中会让一个到期三个月的用户
// 继续用着高级功能，而这正是这套判定要防的事。
func TestHasFeatureRequiresActiveMembership(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

	member := Evaluate(EvalInput{
		ExpireAt:    ptr(now.Add(48 * time.Hour)),
		LastChannel: ChannelWallet,
		Features:    []string{"export", "ai.chat"},
	}, now)
	if !member.HasFeature("export") {
		t.Error("会员应当拥有快照里的 export")
	}
	if !member.HasFeature(" EXPORT ") {
		t.Error("功能标识判定应当忽略大小写与空白")
	}
	if member.HasFeature("hd_video") {
		t.Error("不该凭空多出没买过的功能")
	}

	expired := Evaluate(EvalInput{
		ExpireAt:    ptr(now.Add(-time.Hour)),
		LastChannel: ChannelWallet,
		Features:    []string{"export"},
	}, now)
	if expired.HasFeature("export") {
		t.Error("已过期的用户不该还拥有功能权益")
	}
	if len(expired.Features) != 0 {
		t.Errorf("不是会员时功能集合应为空，实际 %v", expired.Features)
	}
}

// 空标签一律判不通过：调用方漏传参数不该被当成"通用会员校验"放行 ——
// 那两件事的结论可能正好相反。
func TestHasFeatureRejectsEmptyTag(t *testing.T) {
	now := time.Now()
	member := Evaluate(EvalInput{ExpireAt: ptr(now.Add(time.Hour)), Features: []string{"export"}}, now)
	if member.HasFeature("") || member.HasFeature("   ") {
		t.Error("空功能标识必须判不通过")
	}
}

// View 是投影而不是另一份组装：字段一旦漂移，服务端校验给出的结论
// 就会与客户端 /vip/status 看到的不一致，而这种不一致没人能自查。
func TestViewProjectsEntitlement(t *testing.T) {
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	entitlement := Evaluate(EvalInput{
		ExpireAt:     ptr(now.Add(72 * time.Hour)),
		LastChannel:  ChannelPaymentOrder,
		LastPlanName: "高级版",
		Features:     []string{"export"},
	}, now)

	view := entitlement.View()
	if view.IsVIP != entitlement.IsVIP || view.IsTrial != entitlement.IsTrial ||
		view.Source != entitlement.Source || view.PlanName != entitlement.PlanName ||
		view.RemainingSeconds != entitlement.RemainingSeconds ||
		view.RemainingDays != entitlement.RemainingDays {
		t.Errorf("投影与判定结论不一致：%+v vs %+v", view, entitlement)
	}
	if !reflect.DeepEqual(view.Features, []string{"export"}) {
		t.Errorf("功能集合未被投影：%v", view.Features)
	}
	if view.ExpireAt == nil || !view.ExpireAt.Equal(*entitlement.ExpireAt) {
		t.Error("到期时间未被投影")
	}
}

// 不是会员时功能集合必须是空数组而不是 nil：序列化出去 nil 是 `null`，
// 而调用方多半直接 `for (f of features)` —— 那会当场炸掉。
func TestViewNeverEmitsNullFeatures(t *testing.T) {
	view := Evaluate(EvalInput{}, time.Now()).View()
	if view.Features == nil {
		t.Fatal("features 不能是 nil")
	}
	if len(view.Features) != 0 {
		t.Errorf("非会员的功能集合应为空，实际 %v", view.Features)
	}
}
