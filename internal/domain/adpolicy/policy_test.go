package adpolicy

import (
	"testing"
	"time"
)

func TestEvaluate(t *testing.T) {
	enabled := Policy{Enabled: true, ConsentVersion: 2, VipExempt: true}
	accepted := &Consent{Accepted: true, Version: 2}
	declined := &Consent{Accepted: false, Version: 2}
	outdated := &Consent{Accepted: true, Version: 1}

	cases := []struct {
		name     string
		policy   Policy
		consent  *Consent
		vip      bool
		mode     string
		decision bool
		exempt   bool
	}{
		{"未选择", enabled, nil, false, ModeBasic, true, false},
		{"已同意", enabled, accepted, false, ModeFull, false, false},
		{"已拒绝", enabled, declined, false, ModeBasic, false, false},
		{"按旧版本同意", enabled, outdated, false, ModeBasic, true, false},
		{"会员免除", enabled, declined, true, ModeFull, false, true},
		{"会员免除但未选择", enabled, nil, true, ModeFull, true, true},
		{"会员不免除", Policy{Enabled: true, ConsentVersion: 2}, declined, true, ModeBasic, false, false},
		{"策略未启用", Policy{ConsentVersion: 1}, declined, false, ModeFull, false, false},
		{"策略未启用且未选择", Policy{ConsentVersion: 1}, nil, false, ModeFull, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate(tc.policy, tc.consent, tc.vip)
			if got.Mode != tc.mode || got.DecisionRequired != tc.decision || got.Exempt != tc.exempt {
				t.Fatalf("got %+v, want mode=%s decision=%v exempt=%v", got, tc.mode, tc.decision, tc.exempt)
			}
		})
	}
}

func TestValidatePolicy(t *testing.T) {
	valid := NormalizePolicy(SavePolicyInput{
		AppID:      1,
		PolicyURL:  " https://example.com/ads ",
		BasicTools: []string{" Todo ", "todo", "stopwatch", ""},
		Splash:     SplashConfig{Enabled: true, PlacementID: " 123abc ", MinIntervalSeconds: 600, DailyLimit: 5},
	})
	if err := ValidatePolicy(valid); err != nil {
		t.Fatalf("valid policy rejected: %v", err)
	}
	if valid.ConsentVersion != 1 {
		t.Fatalf("version 0 should normalize to 1, got %d", valid.ConsentVersion)
	}
	if len(valid.BasicTools) != 2 || valid.BasicTools[0] != "todo" || valid.BasicTools[1] != "stopwatch" {
		t.Fatalf("basic tools not normalized: %v", valid.BasicTools)
	}
	if valid.Splash.PlacementID != "123abc" || valid.PolicyURL != "https://example.com/ads" {
		t.Fatalf("fields not trimmed: %+v", valid)
	}

	invalid := map[string]func(p *Policy){
		"版本为负":    func(p *Policy) { p.ConsentVersion = -1 },
		"链接不是网址":  func(p *Policy) { p.PolicyURL = "javascript:alert(1)" },
		"功能标识含空格": func(p *Policy) { p.BasicTools = []string{"a b"} },
		"广告位含符号":  func(p *Policy) { p.Splash.PlacementID = "a/b" },
		"间隔过长":    func(p *Policy) { p.Splash.MinIntervalSeconds = MaxSplashIntervalSeconds + 1 },
		"次数为负":    func(p *Policy) { p.Splash.DailyLimit = -1 },
	}
	for name, mutate := range invalid {
		t.Run(name, func(t *testing.T) {
			policy := valid
			policy.BasicTools = append([]string(nil), valid.BasicTools...)
			mutate(&policy)
			if err := ValidatePolicy(policy); err == nil {
				t.Fatalf("expected rejection")
			}
		})
	}
}

func TestSplashVerdict(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-5 * time.Minute)
	cfg := SplashConfig{Enabled: true, MinIntervalSeconds: 600, DailyLimit: 3}

	if ok, _ := SplashVerdict(cfg, SplashUsage{}, true, false, now); !ok {
		t.Fatal("fresh user should see the splash")
	}
	if ok, _ := SplashVerdict(cfg, SplashUsage{}, false, false, now); ok {
		t.Fatal("no consent, no splash")
	}
	if ok, _ := SplashVerdict(cfg, SplashUsage{}, true, true, now); ok {
		t.Fatal("exempt members see no splash")
	}
	if ok, _ := SplashVerdict(cfg, SplashUsage{TodayCount: 3}, true, false, now); ok {
		t.Fatal("daily limit reached")
	}
	ok, next := SplashVerdict(cfg, SplashUsage{TodayCount: 1, LastShownAt: &recent}, true, false, now)
	if ok || next == nil || !next.Equal(recent.Add(10*time.Minute)) {
		t.Fatalf("interval not enforced: ok=%v next=%v", ok, next)
	}
	if ok, _ := SplashVerdict(SplashConfig{}, SplashUsage{}, true, false, now); ok {
		t.Fatal("disabled splash")
	}
}

func TestClampOccurredAt(t *testing.T) {
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	hourAgo := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	stale := now.Add(-SplashEventMaxAge - time.Hour)
	if got := ClampOccurredAt(&hourAgo, now); !got.Equal(hourAgo) {
		t.Fatalf("recent time should be kept, got %v", got)
	}
	for _, value := range []*time.Time{nil, &future, &stale} {
		if got := ClampOccurredAt(value, now); !got.Equal(now) {
			t.Fatalf("expected now, got %v", got)
		}
	}
}
