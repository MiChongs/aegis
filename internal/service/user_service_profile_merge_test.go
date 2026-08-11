package service

import (
	"testing"
	"time"

	userdomain "aegis/internal/domain/user"
)

func TestMergeAdminUserProfileFieldsHydratesRegisterLocationFromExtra(t *testing.T) {
	item := &userdomain.AdminUserView{
		ID:        31,
		AppID:     10000,
		Account:   "demo",
		CreatedAt: time.Date(2026, 3, 29, 0, 0, 0, 0, time.UTC),
		Extra: map[string]any{
			"register_ip":       "220.195.74.125",
			"register_province": "北京市",
			"register_city":     "西城区",
			"register_isp":      "联通",
			"register_time":     "2024-05-26T00:40:47Z",
		},
	}
	profile := &userdomain.Profile{
		UserID: 31,
		Extra:  map[string]any{},
	}

	mergeAdminUserProfileFields(item, profile)

	if item.RegisterIP != "220.195.74.125" {
		t.Fatalf("expected top-level register ip, got %q", item.RegisterIP)
	}
	if item.RegisterProvince != "北京市" {
		t.Fatalf("expected top-level register province, got %q", item.RegisterProvince)
	}
	if item.RegisterCity != "西城区" {
		t.Fatalf("expected top-level register city, got %q", item.RegisterCity)
	}
	if item.RegisterISP != "联通" {
		t.Fatalf("expected top-level register isp, got %q", item.RegisterISP)
	}
	if item.RegisterTime == nil || item.RegisterTime.UTC().Format(time.RFC3339) != "2024-05-26T00:40:47Z" {
		t.Fatalf("expected top-level register time, got %v", item.RegisterTime)
	}
	if profile.RegisterIP != "220.195.74.125" {
		t.Fatalf("expected profile register ip, got %q", profile.RegisterIP)
	}
	if profile.RegisterProvince != "北京市" {
		t.Fatalf("expected profile register province, got %q", profile.RegisterProvince)
	}
	if profile.RegisterCity != "西城区" {
		t.Fatalf("expected profile register city, got %q", profile.RegisterCity)
	}
	if profile.RegisterISP != "联通" {
		t.Fatalf("expected profile register isp, got %q", profile.RegisterISP)
	}
	if profile.RegisterTime == nil || profile.RegisterTime.UTC().Format(time.RFC3339) != "2024-05-26T00:40:47Z" {
		t.Fatalf("expected profile register time, got %v", profile.RegisterTime)
	}
}

func TestShouldEnrichProfileRegisterLocation(t *testing.T) {
	tests := []struct {
		name    string
		profile *userdomain.Profile
		wantIP  string
		wantOK  bool
	}{
		{
			name: "missing register fields uses top level ip",
			profile: &userdomain.Profile{
				RegisterIP: "220.195.74.125",
			},
			wantIP: "220.195.74.125",
			wantOK: true,
		},
		{
			name: "missing register fields uses extra ip",
			profile: &userdomain.Profile{
				Extra: map[string]any{"register_ip": "220.195.74.125"},
			},
			wantIP: "220.195.74.125",
			wantOK: true,
		},
		{
			name: "complete location skips enrichment",
			profile: &userdomain.Profile{
				RegisterIP:       "220.195.74.125",
				RegisterProvince: "北京市",
				RegisterCity:     "西城区",
				RegisterISP:      "联通",
			},
			wantOK: false,
		},
		{
			name: "missing ip skips enrichment",
			profile: &userdomain.Profile{
				RegisterProvince: "北京市",
			},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotIP, gotOK := shouldEnrichProfileRegisterLocation(tt.profile)
			if gotIP != tt.wantIP || gotOK != tt.wantOK {
				t.Fatalf("expected (%q,%v), got (%q,%v)", tt.wantIP, tt.wantOK, gotIP, gotOK)
			}
		})
	}
}

func TestBuildRegisterLocationPatch(t *testing.T) {
	patch := buildRegisterLocationPatch("220.195.74.125", IPLocation{
		Region: "北京市",
		City:   "西城区",
		ISP:    "联通",
	})
	if patch["register_ip"] != "220.195.74.125" {
		t.Fatalf("expected register_ip, got %#v", patch["register_ip"])
	}
	if patch["register_province"] != "北京市" {
		t.Fatalf("expected register_province, got %#v", patch["register_province"])
	}
	if patch["register_city"] != "西城区" {
		t.Fatalf("expected register_city, got %#v", patch["register_city"])
	}
	if patch["register_isp"] != "联通" {
		t.Fatalf("expected register_isp, got %#v", patch["register_isp"])
	}

	if got := buildRegisterLocationPatch("127.0.0.1", IPLocation{IsPrivate: true, Region: "内网"}); got != nil {
		t.Fatalf("expected nil patch for private ip, got %#v", got)
	}
	if got := buildRegisterLocationPatch("220.195.74.125", IPLocation{}); got != nil {
		t.Fatalf("expected nil patch for unresolved location, got %#v", got)
	}
}

func TestApplyRegisterIPFallback(t *testing.T) {
	tests := []struct {
		name           string
		userID         int64
		profile        *userdomain.Profile
		requestIP      string
		wantProfileIP  string
		wantPersistIP  string
		wantShouldSave bool
	}{
		{
			name:           "backfill from request ip",
			userID:         31,
			profile:        &userdomain.Profile{UserID: 31, Extra: map[string]any{}},
			requestIP:      "117.183.236.137",
			wantProfileIP:  "117.183.236.137",
			wantPersistIP:  "117.183.236.137",
			wantShouldSave: true,
		},
		{
			name:           "reuse existing extra register ip",
			userID:         31,
			profile:        &userdomain.Profile{UserID: 31, Extra: map[string]any{"register_ip": "220.195.74.125"}},
			requestIP:      "117.183.236.137",
			wantProfileIP:  "220.195.74.125",
			wantPersistIP:  "",
			wantShouldSave: false,
		},
		{
			name:           "create profile when absent",
			userID:         88,
			profile:        nil,
			requestIP:      "113.127.223.148",
			wantProfileIP:  "113.127.223.148",
			wantPersistIP:  "113.127.223.148",
			wantShouldSave: true,
		},
		{
			name:           "skip empty request ip",
			userID:         31,
			profile:        &userdomain.Profile{UserID: 31, Extra: map[string]any{}},
			requestIP:      "",
			wantProfileIP:  "",
			wantPersistIP:  "",
			wantShouldSave: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile, persistIP, shouldSave := applyRegisterIPFallback(tt.userID, tt.profile, tt.requestIP)
			if shouldSave != tt.wantShouldSave {
				t.Fatalf("expected shouldSave=%v, got %v", tt.wantShouldSave, shouldSave)
			}
			if persistIP != tt.wantPersistIP {
				t.Fatalf("expected persistIP=%q, got %q", tt.wantPersistIP, persistIP)
			}
			if tt.wantProfileIP == "" {
				if profile != nil && profile.RegisterIP != "" {
					t.Fatalf("expected empty profile register ip, got %q", profile.RegisterIP)
				}
				return
			}
			if profile == nil {
				t.Fatal("expected profile to be initialized")
			}
			if profile.RegisterIP != tt.wantProfileIP {
				t.Fatalf("expected profile register ip=%q, got %q", tt.wantProfileIP, profile.RegisterIP)
			}
			if profile.Extra["register_ip"] != tt.wantProfileIP {
				t.Fatalf("expected extra register_ip=%q, got %#v", tt.wantProfileIP, profile.Extra["register_ip"])
			}
		})
	}
}
