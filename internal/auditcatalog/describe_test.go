package auditcatalog

import "testing"

func TestDescribeUsesCatalogAndDropsAppKey(t *testing.T) {
	d := Describe("PUT", "/api/admin/apps/:appkey/releases/:rid/rollout", "appkey=b7784e2f-9d48-4a72-b31e-bb80ae70334f,rid=12", "")
	if !d.Known || d.OperationName != "调整灰度比例" || d.ModuleLabel != "发布中心" || d.TargetType != "版本" {
		t.Fatalf("unexpected display: %+v", d)
	}
	if d.TargetLabel != "#12" {
		t.Fatalf("appkey must not become the target, got %q", d.TargetLabel)
	}
	// handler 给出的对象名优先
	d = Describe("POST", "/api/admin/apps/:appkey/releases/assets", "appkey=b7784e2f", "voyage-1.0.9-arm64-v8a.apk")
	if d.OperationName != "上传安装包" || d.TargetLabel != "voyage-1.0.9-arm64-v8a.apk" {
		t.Fatalf("unexpected display: %+v", d)
	}
	// 整体性操作没有对象
	if d = Describe("GET", "/api/admin/apps/:appkey/releases", "appkey=b7784e2f", ""); d.TargetLabel != "" {
		t.Fatalf("list view should have no target, got %q", d.TargetLabel)
	}
}

func TestModuleForLegacyCategories(t *testing.T) {
	for category, want := range map[string]string{"profile": "account", "monitor": "platform", "rbac": "admin", "release": "release", "unknown": "platform"} {
		if got := ModuleFor(category); got != want {
			t.Errorf("ModuleFor(%q) = %q, want %q", category, got, want)
		}
	}
}

func TestParseUserAgent(t *testing.T) {
	cases := []struct{ ua, browser, system string }{
		{"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36", "Chrome 141", "macOS"},
		{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36 Edg/140.0.0.0", "Edge 140", "Windows 10/11"},
		{"Mozilla/5.0 (iPhone; CPU iPhone OS 18_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.1 Mobile/15E148 Safari/604.1", "Safari 18", "iOS 18"},
		{"Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0", "Firefox 131", "Linux"},
		{"curl/8.5.0", "curl", ""},
	}
	for _, tc := range cases {
		browser, system := ParseUserAgent(tc.ua)
		if browser != tc.browser || system != tc.system {
			t.Errorf("ParseUserAgent(%q) = %q, %q; want %q, %q", tc.ua, browser, system, tc.browser, tc.system)
		}
	}
}
