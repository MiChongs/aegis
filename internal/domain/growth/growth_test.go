package growth

import (
	"math/rand/v2"
	"testing"
	"time"
)

func TestScaleRounding(t *testing.T) {
	third := Factor{Num: 1, Den: 3}
	cases := []struct {
		base     int64
		factor   Factor
		rounding Rounding
		want     int64
	}{
		{10, One, RoundFloor, 10},
		{10, Factor{Num: 2, Den: 1}, RoundFloor, 20},
		{10, Factor{Num: 0, Den: 1}, RoundCeil, 0},
		{10, third, RoundFloor, 3},
		{10, third, RoundCeil, 4},
		{10, third, RoundNearest, 3},
		{5, Factor{Num: 1, Den: 2}, RoundNearest, 3},
		{1, third, RoundFloor, 0},
		{1, third, RoundCeil, 1},
		{0, Factor{Num: 5, Den: 1}, RoundCeil, 0},
		{-3, Factor{Num: 5, Den: 1}, RoundCeil, 0},
		{7, Factor{Num: 3, Den: 2}, RoundFloor, 10},
	}
	for _, c := range cases {
		if got := Scale(c.base, c.factor, c.rounding, nil); got != c.want {
			t.Errorf("Scale(%d, %v, %s) = %d, want %d", c.base, c.factor, c.rounding, got, c.want)
		}
	}
}

func TestScaleRandomExpectation(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	var total int64
	const n = 30000
	for i := 0; i < n; i++ {
		total += Scale(1, Factor{Num: 1, Den: 3}, RoundRandom, rng)
	}
	mean := float64(total) / n
	if mean < 0.31 || mean > 0.356 {
		t.Fatalf("random rounding mean = %.4f, want about 1/3", mean)
	}
}

func TestCombineStacksAndCaps(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)
	rules := []Rule{
		{ID: 1, Kind: KindIntegral, Numerator: 2, Denominator: 1, Enabled: true, Rounding: RoundNearest, CreatedAt: past},
		{ID: 2, UserID: 9, Kind: KindIntegral, Numerator: 1, Denominator: 4, Enabled: true, DailyCap: 100, Rounding: RoundCeil, CreatedAt: past},
		{ID: 3, UserID: 9, Kind: KindIntegral, Numerator: 1, Denominator: 10, Enabled: true, StartsAt: &future},
		{ID: 4, UserID: 9, Kind: KindIntegral, Numerator: 5, Denominator: 1, Enabled: false},
		{ID: 5, UserID: 9, Kind: KindIntegral, Numerator: 1, Denominator: 2, Enabled: true, DailyCap: 50, Sources: []string{SourceCardKey}},
	}
	combined := Combine(rules, SourceLottery, now)
	if combined.Factor != (Factor{Num: 1, Den: 2}) {
		t.Fatalf("factor = %v, want 1/2", combined.Factor)
	}
	if combined.DailyCap != 100 || combined.Rounding != RoundCeil || len(combined.RuleIDs) != 2 {
		t.Fatalf("combined = %+v", combined)
	}
	// 卡密不是默认来源：只有显式列出卡密的规则 5 作用
	card := Combine(rules, SourceCardKey, now)
	if card.Factor != (Factor{Num: 1, Den: 2}) || card.DailyCap != 50 || len(card.RuleIDs) != 1 {
		t.Fatalf("card combined = %+v", card)
	}
}

func TestApplyDailyCap(t *testing.T) {
	combined := Combined{Factor: Factor{Num: 2, Den: 1}, Rounding: RoundFloor, DailyCap: 100}
	out := Apply(30, combined, 70, SourceSignin, nil)
	if out.Scaled != 60 || out.Granted != 30 || !out.Capped {
		t.Fatalf("outcome = %+v", out)
	}
	out = Apply(30, combined, 150, SourceSignin, nil)
	if out.Granted != 0 || !out.Capped {
		t.Fatalf("over cap outcome = %+v", out)
	}
}

func TestNormalizeRuleInput(t *testing.T) {
	now := time.Now()
	in := RuleInput{AppID: 1, Kind: KindExperience, Numerator: 4, Denominator: 8, Sources: []string{"lottery", "signin", "lottery"}}
	if err := in.Normalize(now); err != nil {
		t.Fatal(err)
	}
	if in.Numerator != 1 || in.Denominator != 2 || len(in.Sources) != 2 || in.Rounding != RoundFloor {
		t.Fatalf("normalized = %+v", in)
	}
	bad := RuleInput{AppID: 1, Kind: KindIntegral, Numerator: 1, Denominator: 1}
	if err := bad.Normalize(now); err == nil {
		t.Fatal("×1 without cap should be rejected")
	}
	store := RuleInput{AppID: 1, Kind: KindIntegral, Numerator: 2, Denominator: 1, Sources: []string{SourcePointsStore}}
	if err := store.Normalize(now); err == nil {
		t.Fatal("points_store does not grant integral")
	}
}

func TestSourceFor(t *testing.T) {
	if SourceFor(KindIntegral, "lottery", false) != SourceLottery {
		t.Fatal("lottery should be managed")
	}
	if SourceFor(KindIntegral, "points_store", false) != "" {
		t.Fatal("points_store integral is a refund, not managed")
	}
	if SourceFor(KindExperience, "points_store", false) != SourcePointsStore {
		t.Fatal("points_store experience pack is managed")
	}
	if SourceFor(KindIntegral, "payment_order", false) != "" || SourceFor(KindIntegral, "lottery", true) != "" {
		t.Fatal("purchases and refunds are never managed")
	}
}
