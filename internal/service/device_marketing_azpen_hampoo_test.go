package service

import "testing"

// Azpen / Hampoo 识别 + 防止与 Prestigio 冲突
func TestAzpenAndHampooRecognition(t *testing.T) {
	cases := []struct {
		identifier string
		market     string
		expect     string
	}{
		// Azpen Prestige 系列
		{"Prestige_10QH_Plus", "Prestige 10QH Plus", "Azpen"},
		{"Prestige 7", "Azpen Prestige 7", "Azpen"},
		{"Azpen A746", "Azpen A746", "Azpen"},

		// Hampoo HPPR 系列
		{"HPPR10A", "Hampoo Pad HPPR10A", "Hampoo"},
		{"HPPR07S", "HPPR07S", "Hampoo"},
		{"hampoo_tab", "Hampoo Tab", "Hampoo"},

		// 防冲突：Prestigio 不应被 Azpen 吞掉
		{"Prestigio Wize 4137", "Wize 4137 3G", "Prestigio"},
		{"MultiPhone 5504 DUO", "5504 DUO", "Prestigio"},
	}
	for _, c := range cases {
		got := guessManufacturer("android", c.identifier, c.market)
		if got != c.expect {
			t.Errorf("id=%q name=%q expected %q got %q", c.identifier, c.market, c.expect, got)
		}
	}
}
