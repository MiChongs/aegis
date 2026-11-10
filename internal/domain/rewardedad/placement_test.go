package rewardedad

import (
	"slices"
	"testing"
)

func TestCallbackKeyCiphers(t *testing.T) {
	cfg := &Config{
		SecurityKeyCipher: "default",
		Placements: []Placement{
			{ID: "p1", SecurityKeyCipher: "k1"},
			{ID: "p2"},
			{ID: "p3", SecurityKeyCipher: "k3"},
		},
	}
	cases := []struct {
		name      string
		cfg       *Config
		placement string
		want      []string
	}{
		{"own key only", cfg, "p1", []string{"k1"}},
		{"registered without key falls back to default", cfg, "p2", []string{"default"}},
		{"unregistered uses default", cfg, "p9", []string{"default"}},
		{"no placement uses default", cfg, "", []string{"default"}},
		{"no default, registered without key", &Config{Placements: cfg.Placements}, "p2", nil},
		{"no default, unregistered tries every key", &Config{Placements: cfg.Placements}, "p9", []string{"k1", "k3"}},
		{"no default, empty placement tries every key", &Config{Placements: cfg.Placements}, "", []string{"k1", "k3"}},
		{"own key wins without default", &Config{Placements: cfg.Placements}, "p3", []string{"k3"}},
		{"nothing configured", &Config{}, "p1", []string{}},
		{"nil config", nil, "p1", nil},
	}
	for _, tc := range cases {
		got := tc.cfg.CallbackKeyCiphers(tc.placement)
		if len(got) != len(tc.want) || (len(got) > 0 && !slices.Equal(got, tc.want)) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestKeySource(t *testing.T) {
	cfg := &Config{Placements: []Placement{{ID: "p1", SecurityKeyCipher: "k1"}, {ID: "p2"}}}
	if got := cfg.KeySource("p1"); got != KeySourceOwn {
		t.Fatalf("p1: %s", got)
	}
	if got := cfg.KeySource("p2"); got != KeySourceMissing {
		t.Fatalf("p2 without default: %s", got)
	}
	cfg.SecurityKeyCipher = "default"
	if got := cfg.KeySource("p2"); got != KeySourceDefault {
		t.Fatalf("p2 with default: %s", got)
	}
}

func TestEnsureScenePlacements(t *testing.T) {
	cfg := &Config{
		Placements: []Placement{{ID: "p1", Name: "激励视频"}},
		Scenes: []Scene{
			{Key: "a", Name: "领会员", PlacementID: "p1"},
			{Key: "b", Name: "领积分", PlacementID: "p2"},
			{Key: "c", Name: "领抽奖", PlacementID: "p2"},
		},
	}
	cfg.EnsureScenePlacements()
	if len(cfg.Placements) != 2 || cfg.Placements[1].ID != "p2" || cfg.Placements[1].Name != "领积分" {
		t.Fatalf("scene placement should be registered once with the first scene's name, got %+v", cfg.Placements)
	}
	if got := cfg.ScenesByPlacement()["p2"]; !slices.Equal(got, []string{"b", "c"}) {
		t.Fatalf("scenes by placement: %v", got)
	}
}

func TestValidatePlacements(t *testing.T) {
	ok := []PlacementInput{{ID: "5736335593863633", Name: "激励视频"}, {ID: "8725524153286753"}}
	if err := ValidatePlacements(ok); err != nil {
		t.Fatal(err)
	}
	bad := [][]PlacementInput{
		{{ID: ""}},
		{{ID: "has space"}},
		{{ID: "p1"}, {ID: "p1"}},
		{{ID: "p1", Name: "一二三四五六七八九十一二三四五六七八九十一二三四五六七八九十一二三"}},
	}
	for _, input := range bad {
		if err := ValidatePlacements(input); err == nil {
			t.Errorf("expected an error for %+v", input)
		}
	}
	many := make([]PlacementInput, MaxPlacements+1)
	for index := range many {
		many[index] = PlacementInput{ID: "p" + string(rune('a'+index))}
	}
	if err := ValidatePlacements(many); err == nil {
		t.Fatal("too many placements should be refused")
	}
}
