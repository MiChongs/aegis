package service

import "testing"

// 验证常见机型的厂商匹配精度 + 防误伤
func TestGuessManufacturer(t *testing.T) {
	cases := []struct {
		name       string
		platform   string
		identifier string
		market     string
		expect     string
	}{
		// 正向匹配
		{name: "HTC by id prefix", platform: "android", identifier: "HTC_0P3P5", market: "0P3P5", expect: "HTC"},
		{name: "HTC word match", platform: "android", identifier: "HTC 0P9C8", market: "0P9C8", expect: "HTC"},
		{name: "LG by id prefix", platform: "android", identifier: "LG-FL40L", market: "070 touch", expect: "LG"},
		{name: "ZTE by id prefix", platform: "android", identifier: "ZTE V882", market: "009Z", expect: "ZTE"},
		{name: "Luvo by id prefix", platform: "android", identifier: "Luvo 001", market: "001", expect: "Luvo"},
		{name: "Luvo uppercase", platform: "android", identifier: "LUVO 001L", market: "001L", expect: "Luvo"},
		{name: "Archos in id", platform: "android", identifier: "Archos 101 Cobalt", market: "101 Cobalt", expect: "Archos"},
		{name: "Samsung SM- prefix", platform: "android", identifier: "SM-G998B", market: "Galaxy S21 Ultra", expect: "Samsung"},
		{name: "Xiaomi Redmi", platform: "android", identifier: "Redmi Note 13", market: "Redmi Note 13", expect: "Xiaomi"},
		{name: "Honor split out", platform: "android", identifier: "HRY-LX1", market: "Honor 10 Lite", expect: "Honor"},
		{name: "Apple ios", platform: "ios", identifier: "iPhone14,3", market: "iPhone 13 Pro Max", expect: "Apple"},
		{name: "Lenovo TB-", platform: "android", identifier: "TB-J606F", market: "Tab P11", expect: "Lenovo"},

		// 反向：防误伤（不能识别为 Xiaomi）
		{name: "Azumi not xiaomi", platform: "android", identifier: "Azumi A45", market: "A45", expect: "Azumi"},
		// "mi" 作为独立词在 "Azumi" 中不应命中 —— containsWord 会检测字符边界

		// 防 Apple 误伤：DroiPad 里 "ipad" 不是独立词，不能归为 Apple；DroiPad 是 TECNO 子品牌
		{name: "DroiPad is TECNO", platform: "android", identifier: "DP10APro-SLA1", market: "DroiPad 10 ProⅡ", expect: "TECNO"},
	}
	for _, c := range cases {
		got := guessManufacturer(c.platform, c.identifier, c.market)
		if got != c.expect {
			t.Errorf("[%s] expected %q, got %q (id=%q name=%q)", c.name, c.expect, got, c.identifier, c.market)
		}
	}
}

func TestContainsWordBoundary(t *testing.T) {
	// "mi" 不应在 "azumi a45" 中被当作独立词命中
	if containsWord("azumi a45", "mi") {
		t.Fatal(`"mi" should NOT be a word in "azumi a45"`)
	}
	// "mi" 在 "mi 8 lite" 中是独立词
	if !containsWord("mi 8 lite", "mi") {
		t.Fatal(`"mi" should be a word in "mi 8 lite"`)
	}
	// "htc" 在 "htc_0p9c8" 中是独立词（下划线作为分隔）
	if !containsWord("htc_0p9c8", "htc") {
		t.Fatal(`"htc" should be a word in "htc_0p9c8"`)
	}
}
