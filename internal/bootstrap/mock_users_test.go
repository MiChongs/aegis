package bootstrap

import (
	"math/rand"
	"testing"
)

func TestChinaMockUserCatalogCoverage(t *testing.T) {
	if len(chinaMockUserCatalog) < 34 {
		t.Fatalf("expected at least 34 province-level regions, got %d", len(chinaMockUserCatalog))
	}

	seen := make(map[string]struct{}, len(chinaMockUserCatalog))
	for _, item := range chinaMockUserCatalog {
		if item.Province == "" {
			t.Fatal("province name must not be empty")
		}
		if len(item.Cities) == 0 {
			t.Fatalf("province %s must have at least one city", item.Province)
		}
		if _, ok := seen[item.Province]; ok {
			t.Fatalf("duplicate province found: %s", item.Province)
		}
		seen[item.Province] = struct{}{}
	}
}

func TestBuildMockUserRecordsCreatesRegionalUsers(t *testing.T) {
	rng := rand.New(rand.NewSource(20260321))
	records := buildMockUserRecords(10000, 2, rng)

	if len(records) != countMockCities()*2 {
		t.Fatalf("unexpected record count: %d", len(records))
	}
	first := records[0]
	if first.Account == "" || first.Email == "" || first.Nickname == "" {
		t.Fatal("expected generated identity fields")
	}
	if first.Extra["register_province"] == nil || first.Extra["register_city"] == nil {
		t.Fatal("expected register province and city in profile extra")
	}
}
