package notification

import (
	"strings"
	"testing"
)

func TestNormalizeAudienceDefaultsAndScopes(t *testing.T) {
	all, msg := NormalizeAudience(Audience{})
	if msg != "" || all.Scope != AudienceAll || all.Account != AccountAvailable {
		t.Fatalf("空人群应是全站可用账号：%+v %q", all, msg)
	}
	users, msg := NormalizeAudience(Audience{UserIDs: []int64{3, 3, -1, 0, 7}})
	if msg != "" || users.Scope != AudienceUsers || len(users.UserIDs) != 2 || users.UserIDs[0] != 3 || users.UserIDs[1] != 7 {
		t.Fatalf("给了 userIds 应推成指定用户并去重：%+v %q", users, msg)
	}
	if _, msg := NormalizeAudience(Audience{Scope: AudienceUsers}); msg == "" {
		t.Fatal("指定用户为空应报错")
	}
	if _, msg := NormalizeAudience(Audience{Scope: "everyone"}); msg == "" {
		t.Fatal("未知范围应报错")
	}
	// 全站时与范围无关的条件被清掉，排除名单与账号状态保留
	cleared, _ := NormalizeAudience(Audience{Scope: AudienceAll, Membership: MembershipMember, MinLevel: 3,
		ExcludeUserIDs: []int64{9}, Account: "ALL"})
	if cleared.Membership != "" || cleared.MinLevel != 0 || len(cleared.ExcludeUserIDs) != 1 || cleared.Account != AccountAll {
		t.Fatalf("全站人群归一不对：%+v", cleared)
	}
}

func TestNormalizeAudienceFilterValidation(t *testing.T) {
	cases := []struct {
		name string
		in   Audience
	}{
		{"未知会员身份", Audience{Scope: AudienceFilter, Membership: "gold"}},
		{"天数为负", Audience{Scope: AudienceFilter, ActiveWithinDays: -1}},
		{"天数过大", Audience{Scope: AudienceFilter, InactiveDays: MaxAudienceDays + 1}},
		{"注册范围为空", Audience{Scope: AudienceFilter, RegisteredWithinDays: 7, RegisteredBeforeDays: 30}},
		{"活跃与沉默同时设置", Audience{Scope: AudienceFilter, ActiveWithinDays: 7, InactiveDays: 30}},
		{"等级反了", Audience{Scope: AudienceFilter, MinLevel: 5, MaxLevel: 2}},
		{"未知平台", Audience{Scope: AudienceFilter, Platforms: []string{"symbian"}}},
		{"未知账号状态", Audience{Account: "banned"}},
		{"人数上限为负", Audience{Limit: -1}},
		{"关键词过长", Audience{Scope: AudienceFilter, Keyword: strings.Repeat("字", 65)}},
	}
	for _, tc := range cases {
		if _, msg := NormalizeAudience(tc.in); msg == "" {
			t.Errorf("%s：应报错", tc.name)
		}
	}
	ok, msg := NormalizeAudience(Audience{Scope: AudienceFilter, Membership: "ALL", Platforms: []string{"iPhone", "ios", "Android"},
		RegisteredWithinDays: 30, RegisteredBeforeDays: 7, MinLevel: 2, MaxLevel: 9, Keyword: "  vip  "})
	if msg != "" {
		t.Fatalf("合法条件报错：%q", msg)
	}
	if ok.Membership != "" || len(ok.Platforms) != 2 || ok.Platforms[0] != "ios" || ok.Platforms[1] != "android" || ok.Keyword != "vip" {
		t.Fatalf("条件归一不对：%+v", ok)
	}
}

func TestAudienceLabelAndPresets(t *testing.T) {
	label := AudienceLabel(Audience{Scope: AudienceFilter, Membership: MembershipNonMember, InactiveDays: 30,
		Platforms: []string{"android"}, ExcludeUserIDs: []int64{1, 2}, Limit: 500})
	for _, want := range []string{"条件筛选", "非会员", "30 天未活跃", "Android", "排除 2 人", "最多 500 人"} {
		if !strings.Contains(label, want) {
			t.Fatalf("人群摘要 %q 缺少 %q", label, want)
		}
	}
	if label := AudienceLabel(Audience{Scope: AudienceAll, Account: AccountAvailable}); label != "全站用户" {
		t.Fatalf("全站摘要：%q", label)
	}
	seen := map[string]bool{}
	for _, preset := range AudiencePresets() {
		if seen[preset.Key] {
			t.Fatalf("预设人群重复：%s", preset.Key)
		}
		seen[preset.Key] = true
		if _, msg := NormalizeAudience(preset.Audience); msg != "" {
			t.Fatalf("预设人群 %s 不合法：%s", preset.Key, msg)
		}
	}
	catalog := BuildCampaignCatalog()
	if len(catalog.Statuses) != 8 || len(catalog.Categories) == 0 || catalog.Limits["maxUserIds"] != MaxAudienceUserIDs {
		t.Fatalf("目录不完整：%+v", catalog)
	}
	for _, option := range catalog.Statuses {
		if !ValidCampaignStatus(option.Value) || option.Label == option.Value {
			t.Fatalf("状态 %s 没有中文名", option.Value)
		}
	}
}
