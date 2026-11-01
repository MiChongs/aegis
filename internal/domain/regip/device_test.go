package regip

import (
	"testing"
	"time"
)

func TestParseDevice(t *testing.T) {
	cases := []struct {
		raw     string
		valid   bool
		invalid bool
	}{
		{"", false, false},
		{"  ", false, false},
		{"6F9619FF-8B86-D011-B42D-00C04FC964FF", true, false},
		{"a1b2c3d4e5f60718", true, false},
		{"abc", false, true},           // 太短
		{"aaaaaaaaaaaa", false, true},  // 单一字符
		{"12121212-1212", false, true}, // 只有两种字符
		{"device id with space", false, true},
		{"设备标识设备标识设备标识", false, true}, // 非 ASCII
	}
	for _, c := range cases {
		dev := ParseDevice(c.raw, 8)
		if dev.Valid != c.valid || dev.Invalid != c.invalid {
			t.Errorf("%q: got valid=%v invalid=%v (%s)", c.raw, dev.Valid, dev.Invalid, dev.InvalidReason)
		}
	}
	if dev := ParseDevice("ABCDEF123456", 8); dev.ID != "abcdef123456" || dev.Subject() != "device:abcdef123456" {
		t.Fatalf("normalize: %+v", dev)
	}
}

func TestDeviceRulesComplementIP(t *testing.T) {
	p := enabledPolicy(t, func(p *Policy) {
		p.Penalty.Enabled = false
		p.Rules = []Rule{
			{ID: "dev", Enabled: true, Scope: ScopeDevice, WindowSeconds: 86400, Limit: 2, Action: ActionReject},
			{ID: "ipdev", Enabled: true, Scope: ScopeIPDevices, WindowSeconds: 3600, Limit: 3, Action: ActionChallenge},
			{ID: "devip", Enabled: true, Scope: ScopeDeviceIPs, WindowSeconds: 86400, Limit: 4, Action: ActionReject},
		}
	})
	f := factsFor(t, p, "1.2.3.4")
	f.Device = ParseDevice("a1b2c3d4e5f60718", 8)
	f.Counts = map[string]int{"dev": 2, "ipdev": 0, "devip": 0}
	d := Decide(p, f)
	if d.Outcome != OutcomeReject || d.RuleID != "dev" || d.Message == "" {
		t.Fatalf("device limit: %+v", d)
	}
	f.Counts = map[string]int{"dev": 0, "ipdev": 3, "devip": 0}
	if d := Decide(p, f); d.Outcome != OutcomeChallenge || d.RuleID != "ipdev" {
		t.Fatalf("ip devices: %+v", d)
	}
	// 没有 IP：地址相关规则跳过，设备规则照常
	noIP := f
	noIP.Address = Address{}
	noIP.Counts = map[string]int{"dev": 2}
	if d := Decide(p, noIP); d.Outcome != OutcomeReject || d.RuleID != "dev" {
		t.Fatalf("device without ip: %+v", d)
	}
	for _, hit := range Decide(p, noIP).Rules {
		if hit.RuleID != "dev" && !hit.Skipped {
			t.Fatalf("%s should be skipped without ip", hit.RuleID)
		}
	}
	// 没有设备：设备规则跳过
	noDev := factsFor(t, p, "1.2.3.4")
	noDev.Counts = map[string]int{"dev": 99}
	if d := Decide(p, noDev); d.Blocking() {
		t.Fatalf("device rules must skip without device: %+v", d)
	}
}

func TestDeviceQualityActions(t *testing.T) {
	p := enabledPolicy(t, func(p *Policy) {
		p.Device = DeviceRule{Missing: DeviceReject, Invalid: DeviceChallenge, MinLength: 8}
	})
	f := factsFor(t, p, "1.2.3.4")
	if d := Decide(p, f); d.Outcome != OutcomeReject || d.Reason != ReasonMissingDevice {
		t.Fatalf("missing device: %+v", d)
	}
	f.Device = ParseDevice("aaaaaaaaaa", 8)
	if d := Decide(p, f); d.Outcome != OutcomeChallenge || d.Reason != ReasonInvalidDevice {
		t.Fatalf("invalid device: %+v", d)
	}
	f.CaptchaPassed = true
	if d := Decide(p, f); d.Blocking() {
		t.Fatalf("captcha passes invalid device: %+v", d)
	}
}

func TestCombinedListsAndPenalty(t *testing.T) {
	now := time.Now()
	devices := CompileDeviceLists([]DeviceListEntry{
		{ID: 7, DeviceID: "A1B2C3D4E5F60718", Kind: ListDeny},
		{ID: 8, DeviceID: "ffffeeeeddddcccc", Kind: ListElevated, Factor: 4},
	}, now)
	dev := ParseDevice("a1b2c3d4e5f60718", 8)
	match := devices.Match(dev)
	if match == nil || match.Kind != ListDeny || match.Target != ListTargetDevice {
		t.Fatalf("device match: %+v", match)
	}
	ipAllow := &ListMatch{Kind: ListAllow, Target: ListTargetIP, CIDR: "1.2.3.0/24"}
	if got := CombineLists(ipAllow, match); got.Kind != ListDeny {
		t.Fatalf("device deny must beat ip allow: %+v", got)
	}
	ipElevated := &ListMatch{Kind: ListElevated, Factor: 2}
	if got := CombineLists(ipElevated, devices.Match(ParseDevice("ffffeeeeddddcccc", 8))); got.Factor != 4 {
		t.Fatalf("max factor: %+v", got)
	}

	p := enabledPolicy(t, nil)
	f := factsFor(t, p, "1.2.3.4")
	f.Device = dev
	f.DevicePenaltySeconds = 600
	d := Decide(p, f)
	if d.Reason != ReasonPenalty || d.RetryAfter != 600 || d.Message == "" {
		t.Fatalf("device penalty: %+v", d)
	}
}

func TestDistinctScopesNeedWindow(t *testing.T) {
	p := DefaultPolicy()
	p.Rules = []Rule{{ID: "x", Enabled: true, Scope: ScopeDeviceIPs, Limit: 3, Action: ActionReject}}
	if _, err := p.Normalize(); err == nil {
		t.Fatal("distinct scope without window should fail")
	}
}
