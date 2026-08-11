package service

import "testing"

// HTC 多种 identifier 拼写、HP 系列与防误伤
func TestHTCAndHPRecognition(t *testing.T) {
	cases := []struct {
		identifier string
		market     string
		expect     string
	}{
		// HTC 多种拼写全覆盖
		{"HTC6545LVW", "HTC One (M8)", "HTC"}, // 无空格无连字符
		{"HTC_0P3P5", "0P3P5", "HTC"},         // 下划线
		{"HTC 0P6B9", "One (M8 Eye)", "HTC"},  // 空格
		{"htc-0pja2", "Desire 626G+", "HTC"},  // 连字符 / 小写
		{"HTC One M9PLUS", "One M9+", "HTC"},  // 空格 + 混合

		// HP 多种产品线
		{"HP 10", "HP 10 Tablet", "HP"},
		{"HP Slate 7", "HP Slate 7 HD", "HP"},
		{"HP_Stream_8", "HP Stream 8", "HP"},
		{"HP-Pro-Tablet-608", "HP Pro Tablet 608", "HP"},
		{"hewlett_packard_envy", "HP ENVY", "HP"},
	}
	for _, c := range cases {
		got := guessManufacturer("android", c.identifier, c.market)
		if got != c.expect {
			t.Errorf("id=%q name=%q expected %q got %q", c.identifier, c.market, c.expect, got)
		}
	}
}

// HP 不应对常见子串 "hp" 误伤（如 "phone"、"sharp" 里偶然包含字母）
func TestHPWordBoundary(t *testing.T) {
	// "sharp_ABC" 不应被识别为 HP
	if g := guessManufacturer("android", "sharp_ABC", "Sharp Android"); g == "HP" {
		t.Fatalf("sharp_ABC should not be HP, got %q", g)
	}
	// "alphaplus" 里有 "hp" 子串但非独立词，不应为 HP
	if g := guessManufacturer("android", "alphaplus", "Alpha Plus"); g == "HP" {
		t.Fatalf("alphaplus should not be HP, got %q", g)
	}
}
