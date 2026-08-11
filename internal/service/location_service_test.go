package service

import (
	"testing"
	"time"
)

func TestRequiresAdministrativeEnrichment(t *testing.T) {
	if !requiresAdministrativeEnrichment(IPLocation{ISP: "移动"}, true) {
		t.Fatal("expected isp-only location to require enrichment when remote fallback is enabled")
	}
	if requiresAdministrativeEnrichment(IPLocation{Region: "湖北省", ISP: "移动"}, true) {
		t.Fatal("expected province+isp location to skip enrichment")
	}
	if requiresAdministrativeEnrichment(IPLocation{Region: "湖北省", City: "十堰市"}, true) {
		t.Fatal("expected province+city location to skip enrichment")
	}
	if requiresAdministrativeEnrichment(IPLocation{Region: "湖北省", City: "十堰市", ISP: "移动"}, true) {
		t.Fatal("expected province+city+isp location to skip enrichment")
	}
	if requiresAdministrativeEnrichment(IPLocation{ISP: "移动"}, false) {
		t.Fatal("expected enrichment disabled when remote fallback is disabled")
	}
}

func TestMergeIPLocationPrefersLocalAndFillsMissingFields(t *testing.T) {
	primary := IPLocation{
		IP:         "117.183.236.137",
		Country:    "中国",
		ISP:        "移动",
		Source:     "geoip-mmdb",
		ResolvedAt: time.Date(2026, 3, 29, 8, 0, 0, 0, time.UTC),
	}
	secondary := IPLocation{
		IP:         "117.183.236.137",
		Country:    "中国",
		Region:     "湖北省",
		City:       "十堰市",
		ISP:        "中国移动",
		Source:     "mir6",
		ResolvedAt: time.Date(2026, 3, 29, 8, 0, 1, 0, time.UTC),
	}

	merged := mergeIPLocation(primary, secondary)
	if merged.Region != "湖北省" {
		t.Fatalf("expected region from remote, got %q", merged.Region)
	}
	if merged.City != "十堰市" {
		t.Fatalf("expected city from remote, got %q", merged.City)
	}
	if merged.ISP != "移动" {
		t.Fatalf("expected local isp to win, got %q", merged.ISP)
	}
	if merged.Source != "geoip-mmdb+mir6" {
		t.Fatalf("expected merged source, got %q", merged.Source)
	}
	if merged.Location != "中国 湖北省 十堰市" {
		t.Fatalf("expected composed location, got %q", merged.Location)
	}
}

func TestAdministrativeQuality(t *testing.T) {
	if got := (IPLocation{}).administrativeQuality(); got != 0 {
		t.Fatalf("expected quality 0, got %d", got)
	}
	if got := (IPLocation{ISP: "移动"}).administrativeQuality(); got != 1 {
		t.Fatalf("expected quality 1 for isp-only, got %d", got)
	}
	if got := (IPLocation{Region: "湖北省", ISP: "移动"}).administrativeQuality(); got != 3 {
		t.Fatalf("expected quality 3 for province+isp, got %d", got)
	}
	if got := (IPLocation{Region: "湖北省", City: "十堰市", ISP: "移动"}).administrativeQuality(); got != 4 {
		t.Fatalf("expected quality 4 for province+city+isp, got %d", got)
	}
}
