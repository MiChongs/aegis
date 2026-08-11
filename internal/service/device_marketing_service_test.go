package service

import "testing"

// TestParseDeviceIdentifiersKT 校验嵌入的 DeviceIdentifiers.kt 能被正确解析
// 期望：至少 20k 条映射；覆盖 iOS 典型款和 Android 典型款
func TestParseDeviceIdentifiersKT(t *testing.T) {
	items := parseDeviceIdentifiersKT(deviceMarketingSeed)
	if len(items) < 20000 {
		t.Fatalf("expected at least 20000 parsed items, got %d", len(items))
	}

	iosCount, androidCount := 0, 0
	var hasIPhone, hasGalaxy bool
	for _, it := range items {
		switch it.Platform {
		case "ios":
			iosCount++
		case "android":
			androidCount++
		default:
			t.Fatalf("unexpected platform: %q for %q", it.Platform, it.Identifier)
		}
		if it.Identifier == "" || it.MarketingName == "" {
			t.Fatalf("empty field: %+v", it)
		}
		if it.Identifier == "iPhone14,3" {
			hasIPhone = true
		}
		// 任何 Galaxy 开头都算
		if len(it.MarketingName) >= 6 && it.MarketingName[:6] == "Galaxy" {
			hasGalaxy = true
		}
	}
	if iosCount < 50 {
		t.Fatalf("expected >=50 ios entries, got %d", iosCount)
	}
	if androidCount < 10000 {
		t.Fatalf("expected >=10000 android entries, got %d", androidCount)
	}
	if !hasIPhone {
		t.Fatal("expected iPhone14,3 entry")
	}
	if !hasGalaxy {
		t.Fatal("expected at least one Galaxy* entry")
	}
}
