package service

import "testing"

// CHUWI HiPad / Olivetti Olipad / Klipad / TECNO Droipad 多种写法识别
// 同时验证：不会被误识别为 Apple（虽然 identifier/name 里有 "pad" 子串但词边界判定拒绝命中）
func TestPadSuffixBrandsRecognition(t *testing.T) {
	cases := []struct {
		identifier string
		market     string
		expect     string
	}{
		// CHUWI HiPad 系列
		{"HiPad_11_EEA", "HiPad 11", "CHUWI"},
		{"HiPad 11", "HiPad 11", "CHUWI"},
		{"Chuwi HiPad Plus", "HiPad Plus", "CHUWI"},
		{"HiBook", "HiBook 10.1", "CHUWI"},
		{"SurBook", "SurBook Mini", "CHUWI"},

		// Olivetti Olipad 系列
		{"Olipad", "chagall", "Olivetti"},
		{"Olipad Smart", "Olivetti Olipad Smart", "Olivetti"},
		{"OLIPAD_3", "Olipad 3", "Olivetti"},

		// Klipad 系列
		{"KLIPAD_A1040M", "A1040M", "Klipad"},
		{"klipad_pro", "Klipad Pro", "Klipad"},

		// TECNO DroiPad 无分隔符也要命中
		{"Droipad10", "DroiPad 10", "TECNO"},
		{"DroiPad_10_Pro", "DroiPad 10 Pro", "TECNO"},
	}
	for _, c := range cases {
		got := guessManufacturer("android", c.identifier, c.market)
		if got == "Apple" {
			t.Fatalf("BUG: %q / %q should NOT be Apple, got Apple", c.identifier, c.market)
		}
		if got != c.expect {
			t.Errorf("id=%q name=%q expected %q got %q", c.identifier, c.market, c.expect, got)
		}
	}
}
