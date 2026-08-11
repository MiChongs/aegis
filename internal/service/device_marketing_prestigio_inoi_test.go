package service

import "testing"

// 验证 Prestigio / INOI / 其他小众厂商的精准识别
func TestPrestigioAndINOI(t *testing.T) {
	cases := []struct {
		identifier string
		market     string
		expect     string
	}{
		// Prestigio MultiPhone
		{"MultiPhone 5504 DUO", "5504 DUO", "Prestigio"},
		{"PMP5870C_DUO", "MultiPad WIZE 3797", "Prestigio"},
		{"PSP3405DUO", "MultiPhone Wize", "Prestigio"},
		// Prestigio MultiPad Wize
		{"PMT3757_3G", "Multipad Wize 3757 3G", "Prestigio"},
		{"Prestigio Wize 4137 3G", "Wize 4137 3G", "Prestigio"},

		// INOI
		{"T107_Plus", "inoiPad_Pro", "INOI"},
		{"inoi_5", "INOI 5", "INOI"},
		{"inoiPad Pro", "INOI Pad Pro", "INOI"},
	}
	for _, c := range cases {
		got := guessManufacturer("android", c.identifier, c.market)
		if got != c.expect {
			t.Errorf("id=%q name=%q expected %q got %q", c.identifier, c.market, c.expect, got)
		}
	}
}

// 防误伤：inoi 不是独立词时不应误判
func TestINOIWordBoundary(t *testing.T) {
	// "Sinoia 7" 里含 "inoi" 作为子串但不是独立词
	got := guessManufacturer("android", "Sinoia7", "Sinoia 7")
	if got == "INOI" {
		t.Fatalf("BUG: Sinoia should NOT be INOI, got %q", got)
	}
}
