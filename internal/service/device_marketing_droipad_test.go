package service

import "testing"

// 强化验证：DroiPad 系列必须识别为 Tecno，绝不能命中 Apple
func TestDroiPadNotApple(t *testing.T) {
	cases := []struct {
		identifier string
		market     string
	}{
		{"DP10APro-SLA1", "DroiPad 10 ProⅡ"},
		{"DP7BPro", "DroiPad 7"},
		{"DroiPad 10 Pro", "DroiPad 10 Pro"},
	}
	for _, c := range cases {
		got := guessManufacturer("android", c.identifier, c.market)
		if got == "Apple" {
			t.Fatalf("BUG: %q / %q should NOT be Apple, got %q", c.identifier, c.market, got)
		}
		if got != "TECNO" {
			t.Errorf("expected TECNO for %q / %q, got %q", c.identifier, c.market, got)
		}
	}
}
