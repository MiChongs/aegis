package regip

import (
	"net/netip"
	"testing"
	"time"
)

func enabledPolicy(t *testing.T, mutate func(*Policy)) Policy {
	t.Helper()
	p := DefaultPolicy()
	p.Enabled = true
	if mutate != nil {
		mutate(&p)
	}
	out, err := p.Normalize()
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	return out
}

func factsFor(t *testing.T, p Policy, ip string) Facts {
	t.Helper()
	addr, ok := ParseAddress(ip, p.Network)
	if !ok {
		t.Fatalf("ParseAddress(%q) failed", ip)
	}
	return Facts{Method: MethodPassword, Address: addr, Counts: map[string]int{}}
}

func TestParseAddressAggregatesIPv6(t *testing.T) {
	net := Network{IPv4Subnet: 24, IPv6Host: 64, IPv6Subnet: 48}
	cases := map[string][2]string{
		"1.2.3.4":                  {"1.2.3.4/32", "1.2.3.0/24"},
		"1.2.3.4:5678":             {"1.2.3.4/32", "1.2.3.0/24"},
		"::ffff:10.0.0.9":          {"10.0.0.9/32", "10.0.0.0/24"},
		"[2408:8207:1:2:3:4:5:6]":  {"2408:8207:1:2::/64", "2408:8207:1::/48"},
		"2408:8207:1:2:aaaa::1%en": {"2408:8207:1:2::/64", "2408:8207:1::/48"},
	}
	for raw, want := range cases {
		addr, ok := ParseAddress(raw, net)
		if !ok {
			t.Fatalf("%s: parse failed", raw)
		}
		if addr.Host.String() != want[0] || addr.Subnet.String() != want[1] {
			t.Errorf("%s: got host=%s subnet=%s, want %v", raw, addr.Host, addr.Subnet, want)
		}
	}
	if _, ok := ParseAddress("not-an-ip", net); ok {
		t.Error("garbage should not parse")
	}
}

func TestCompiledListsPrecedence(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Hour)
	lists := CompileLists([]ListEntry{
		{ID: 1, CIDR: "10.0.0.0/8", Kind: ListAllow},
		{ID: 2, CIDR: "10.1.0.0/16", Kind: ListDeny},
		{ID: 3, CIDR: "172.16.0.0/12", Kind: ListElevated, Factor: 5},
		{ID: 4, CIDR: "172.16.1.0/24", Kind: ListElevated, Factor: 20},
		{ID: 5, CIDR: "192.168.0.0/16", Kind: ListDeny, ExpiresAt: &past},
	}, now)
	check := func(ip, kind string, id int64) {
		t.Helper()
		match := lists.Match(netip.MustParseAddr(ip))
		if kind == "" {
			if match != nil {
				t.Errorf("%s: want no match, got %+v", ip, match)
			}
			return
		}
		if match == nil || match.Kind != kind || match.EntryID != id {
			t.Errorf("%s: want %s#%d, got %+v", ip, kind, id, match)
		}
	}
	check("10.2.3.4", ListAllow, 1)
	check("10.1.3.4", ListDeny, 2) // 拒绝优先于放行
	check("172.16.1.9", ListElevated, 4)
	check("172.17.0.1", ListElevated, 3)
	check("192.168.1.1", "", 0) // 已过期
	check("8.8.8.8", "", 0)
}

func TestParsePrefixRejectsHugeRanges(t *testing.T) {
	if _, err := ParsePrefix("1.0.0.0/4"); err == nil {
		t.Error("/4 should be rejected")
	}
	p, err := ParsePrefix("1.2.3.4/24")
	if err != nil || p.String() != "1.2.3.0/24" {
		t.Errorf("got %v %v", p, err)
	}
	p, err = ParsePrefix("::ffff:1.2.3.4/120")
	if err != nil || p.String() != "1.2.3.0/24" {
		t.Errorf("mapped prefix: got %v %v", p, err)
	}
}

func TestDecideRulePriority(t *testing.T) {
	p := enabledPolicy(t, nil)
	f := factsFor(t, p, "1.2.3.4")
	f.Counts = map[string]int{"ip-hour": 3, "ip-day": 3, "subnet-day": 3}

	d := Decide(p, f)
	if d.Outcome != OutcomeChallenge || d.RuleID != "ip-hour" {
		t.Fatalf("want challenge by ip-hour, got %+v", d)
	}
	f.CaptchaPassed = true
	d = Decide(p, f)
	if d.Outcome != OutcomeAllow || !d.ChallengePassed {
		t.Fatalf("captcha passed should allow, got %+v", d)
	}
	f.Counts["ip-day"] = 10
	d = Decide(p, f)
	if d.Outcome != OutcomeReject || d.RuleID != "ip-day" || d.RetryAfter != 86400 {
		t.Fatalf("reject should win, got %+v", d)
	}
}

func TestDecideObserveModeNeverBlocks(t *testing.T) {
	p := enabledPolicy(t, func(p *Policy) { p.Mode = ModeObserve })
	f := factsFor(t, p, "1.2.3.4")
	f.Counts = map[string]int{"ip-day": 99}
	d := Decide(p, f)
	if d.Blocking() || d.Observed != OutcomeReject || d.RuleID != "ip-day" {
		t.Fatalf("observe mode: got %+v", d)
	}
	if EventOf(d) != EventObserved {
		t.Fatalf("event kind %q", EventOf(d))
	}
	for _, rule := range p.Rules {
		if RuleBlocks(p, rule, false) {
			t.Fatalf("rule %s must not block in observe mode", rule.ID)
		}
	}
}

func TestDecideElevatedAndSurgeLimits(t *testing.T) {
	p := enabledPolicy(t, func(p *Policy) { p.Surge.Enabled = true; p.Surge.Action = ActionObserve })
	f := factsFor(t, p, "1.2.3.4")
	f.List = &ListMatch{Kind: ListElevated, Factor: 10, CIDR: "1.2.3.0/24"}
	f.Counts = map[string]int{"ip-day": 50}
	if d := Decide(p, f); d.Blocking() {
		t.Fatalf("factor 10 → limit 100, should allow: %+v", d)
	}
	f.Surge = SurgeState{Active: true, Current: 100, Expected: 3}
	d := Decide(p, f)
	// 10 × 0.5 = 5 倍，ip-day 生效上限 50，50 个已满
	if d.Outcome != OutcomeReject || d.RuleID != "ip-day" {
		t.Fatalf("surge tighten should reject: %+v", d)
	}
}

func TestPreDecideOrder(t *testing.T) {
	p := enabledPolicy(t, func(p *Policy) {
		p.ISP.Enabled = true
		p.ISP.Action = ActionReject
		p.Geo = GeoRule{Mode: GeoAllow, Regions: []string{"中国"}, Action: ActionReject}
	})
	f := factsFor(t, p, "1.2.3.4")
	f.Location = Location{Resolved: true, Country: "中国", Province: "浙江", ISP: "阿里云"}
	if d := Decide(p, f); d.Reason != ReasonISP || d.Outcome != OutcomeReject {
		t.Fatalf("isp: %+v", d)
	}
	f.Location = Location{Resolved: true, Country: "美国", ISP: "Comcast"}
	if d := Decide(p, f); d.Reason != ReasonGeo {
		t.Fatalf("geo: %+v", d)
	}
	f.PenaltySeconds = 120
	if d := Decide(p, f); d.Reason != ReasonPenalty || d.RetryAfter != 120 {
		t.Fatalf("penalty first: %+v", d)
	}
	f.List = &ListMatch{Kind: ListAllow}
	if d := Decide(p, f); d.Blocking() || d.Reason != ReasonListAllow {
		t.Fatalf("allow list bypasses all: %+v", d)
	}
}

func TestMissingIP(t *testing.T) {
	p := enabledPolicy(t, nil)
	if d := Decide(p, Facts{Method: MethodSMS}); d.Blocking() {
		t.Fatalf("missing ip allowed by default: %+v", d)
	}
	p.MissingIP = MissingIPReject
	if d := Decide(p, Facts{Method: MethodSMS}); d.Outcome != OutcomeReject || d.Reason != ReasonMissingIP {
		t.Fatalf("missing ip reject: %+v", d)
	}
}

func TestLegacyPolicy(t *testing.T) {
	p, configured := FromSettings(map[string]any{"registerCheckIp": true})
	if configured || !p.Enabled || len(p.Rules) != 1 || !p.Rules[0].Lifetime() || p.MissingIP != MissingIPReject {
		t.Fatalf("legacy: %+v", p)
	}
	if !p.LegacyValue() {
		t.Fatal("legacy policy should write back true")
	}
	f := factsFor(t, p, "1.2.3.4")
	f.Counts = map[string]int{"legacy-ip": 1}
	if d := Decide(p, f); d.Outcome != OutcomeReject {
		t.Fatalf("legacy should reject second account: %+v", d)
	}
	off, _ := FromSettings(map[string]any{})
	if off.Enabled {
		t.Fatal("no settings → disabled")
	}
	saved, configured := FromSettings(map[string]any{SettingsKey: map[string]any{"enabled": true, "rules": []any{map[string]any{"scope": "subnet", "limit": 2, "windowSeconds": 3600, "enabled": true}}}})
	if !configured || len(saved.Rules) != 1 || saved.Rules[0].Scope != ScopeSubnet {
		t.Fatalf("saved rules must replace defaults: %+v", saved.Rules)
	}
}

func TestNormalizeValidation(t *testing.T) {
	bad := []func(*Policy){
		func(p *Policy) { p.Rules[0].Scope = "planet" },
		func(p *Policy) { p.Rules[0].Limit = 0 },
		func(p *Policy) { p.Rules[0].WindowSeconds = 10 },
		func(p *Policy) { p.Rules[0].Scope = ScopeApp; p.Rules[0].WindowSeconds = 0 },
		func(p *Policy) { p.Rules[0].Methods = []string{"fax"} },
		func(p *Policy) { p.Network.IPv6Subnet = 96 },
		func(p *Policy) { p.Surge.BaselineMinutes = 6 },
		func(p *Policy) { p.Geo.Mode = GeoAllow; p.Geo.Regions = nil },
		func(p *Policy) { p.Penalty.MaxSeconds = 60; p.Penalty.BaseSeconds = 120 },
	}
	for i, mutate := range bad {
		p := DefaultPolicy()
		mutate(&p)
		if _, err := p.Normalize(); err == nil {
			t.Errorf("case %d should fail", i)
		}
	}
}

func TestDetectSurge(t *testing.T) {
	cfg := Surge{WindowMinutes: 5, BaselineMinutes: 60, Sensitivity: 4, MinRegistrations: 20}
	quiet := make([]int, 65)
	for i := range quiet {
		quiet[i] = 2 + i%3
	}
	if s := DetectSurge(quiet, cfg); s.Active {
		t.Fatalf("steady traffic flagged: %+v", s)
	}
	spike := append([]int(nil), quiet...)
	for i := 60; i < 65; i++ {
		spike[i] = 40
	}
	s := DetectSurge(spike, cfg)
	if !s.Active || s.Current != 200 || s.Score < 4 {
		t.Fatalf("spike not flagged: %+v", s)
	}
	// 冷门应用：基线几乎为零，窗口来了 10 个也不到最小注册数
	cold := make([]int, 65)
	cold[64] = 10
	if s := DetectSurge(cold, cfg); s.Active {
		t.Fatalf("cold app below minimum flagged: %+v", s)
	}
}

func TestPenaltyDurationEscalates(t *testing.T) {
	cfg := Penalty{BaseSeconds: 600, Multiplier: 3, MaxSeconds: 10000}
	want := []int{600, 1800, 5400, 10000, 10000}
	for i, w := range want {
		if got := PenaltyDuration(cfg, i+1); got != w {
			t.Errorf("level %d: got %d want %d", i+1, got, w)
		}
	}
}

func TestLocationMatching(t *testing.T) {
	loc := Location{Resolved: true, Country: "中国", Province: "广东省", City: "深圳"}
	if !loc.MatchRegion("中国") || !loc.MatchRegion("中国/广东") || loc.MatchRegion("中国/浙江") {
		t.Fatal("region matching wrong")
	}
	if loc.CityKey() != "中国/广东省/深圳" {
		t.Fatalf("city key %q", loc.CityKey())
	}
	if NormalizeRegion(" 中国 / 广东 /x") != "中国/广东" {
		t.Fatal("normalize region")
	}
	if _, ok := MatchISP("Alibaba (阿里云) BGP", DefaultISPKeywords); !ok {
		t.Fatal("isp keyword")
	}
}

func TestPrefixRange(t *testing.T) {
	from, to := PrefixRange(netip.MustParsePrefix("1.2.3.0/24"))
	if from.String() != "1.2.3.0" || to.String() != "1.2.3.255" {
		t.Fatalf("got %s-%s", from, to)
	}
}
