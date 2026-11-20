package usernumber

import (
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"
)

func TestClassifyExamples(t *testing.T) {
	s := DefaultSettings()
	cases := []struct {
		number int64
		tier   int
	}{
		{36521, TierSupreme},     // 五位号
		{888888888, TierSupreme}, // 全同
		{473920, TierLegend},     // 六位
		{318888802, TierLegend},  // 五连
		{527777103, TierRare},    // 四连
		{601234957, TierRare},    // 顺子
		{538202026, TierRare},    // 年份尾号
		{123454321, TierRare},    // 对称
		{530271888, TierFine},    // 尾号三连
		{590335571, TierFine},    // AABB
		{507373219, TierFine},    // ABAB
		{730261314, TierFine},    // 吉利尾号
		{530729164, TierCommon},
	}
	for _, c := range cases {
		got := Classify(c.number, &s)
		if got.Tier != c.tier {
			t.Errorf("Classify(%d) = %d %v, want %d", c.number, got.Tier, got.Patterns, c.tier)
		}
	}
}

func TestPatternExamplesMatchTheirTier(t *testing.T) {
	s := DefaultSettings()
	for _, p := range Patterns(&s) {
		n, err := strconv.ParseInt(p.Example, 10, 64)
		if err != nil {
			t.Fatalf("%s example %q", p.Key, p.Example)
		}
		if got := Classify(n, &s); got.Tier < p.DefaultTier {
			t.Errorf("%s example %s tier %d < %d", p.Key, p.Example, got.Tier, p.DefaultTier)
		}
	}
}

func TestPatternOverride(t *testing.T) {
	s := DefaultSettings()
	s.PatternTiers = map[string]int{PatternLuckyTail: TierRare, PatternAABB: PatternDisabled}
	if got := Classify(730261314, &s).Tier; got != TierRare {
		t.Fatalf("override lucky tail: %d", got)
	}
	if got := Classify(590335571, &s).Tier; got != TierCommon {
		t.Fatalf("disabled aabb: %d", got)
	}
}

func TestNumberOfTierHitsExactTier(t *testing.T) {
	s := DefaultSettings()
	r := rand.New(rand.NewPCG(1, 2))
	for tier := TierCommon; tier < TierCount; tier++ {
		for range 300 {
			n := NumberOfTier(tier, &s, r)
			if n == 0 {
				t.Fatalf("tier %d produced nothing", tier)
			}
			if err := s.ValidNumber(n); err != nil {
				t.Fatalf("tier %d produced invalid %d: %v", tier, n, err)
			}
			if got := Classify(n, &s).Tier; got != tier {
				t.Fatalf("tier %d produced %d classified %d", tier, n, got)
			}
		}
	}
}

// plainSQL 与迁移 000165 的 aegis_user_number_is_plain 同一套判据（Go 的 regexp 没有反向引用，逐条手写）。
func plainSQL(d string) bool {
	for i := 0; i+2 < len(d); i++ {
		if d[i] == d[i+1] && d[i+1] == d[i+2] { // (\d)\1\1
			return false
		}
	}
	for i := 0; i+3 < len(d); i++ {
		if d[i] == d[i+1] && d[i+2] == d[i+3] { // (\d)\1(\d)\2
			return false
		}
		if d[i] == d[i+2] && d[i+1] == d[i+3] { // (\d)(\d)\1\2
			return false
		}
	}
	for _, s := range []string{"0123", "1234", "2345", "3456", "4567", "5678", "6789", "9876", "8765", "7654", "6543", "5432", "4321", "3210"} {
		if strings.Contains(d, s) {
			return false
		}
	}
	for _, tail := range []string{"520", "1314", "168", "518"} {
		if strings.HasSuffix(d, tail) {
			return false
		}
	}
	if y, _ := strconv.Atoi(d[len(d)-4:]); y >= 1950 && y <= 2039 {
		return false
	}
	rev := []byte(d)
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return string(rev) != d
}

// TestPlainFilterIsSuperset 数据库放行的默认位数号码，这里一定判普通（默认等级下）。
func TestPlainFilterIsSuperset(t *testing.T) {
	s := DefaultSettings()
	r := rand.New(rand.NewPCG(7, 9))
	for length := MinDefaultLength; length <= HardMaxLength; length++ {
		passed := 0
		for range 20000 {
			d := randomDigits(length, r)
			if !plainSQL(d) {
				continue
			}
			passed++
			n, _ := strconv.ParseInt(d, 10, 64)
			if got := Classify(n, &s); got.Tier != TierCommon {
				t.Fatalf("SQL plain %s classified %d %v", d, got.Tier, got.Patterns)
			}
		}
		if passed == 0 {
			t.Fatalf("length %d: filter rejects everything", length)
		}
	}
}

func TestPickRight(t *testing.T) {
	rights := Rights{Change: 0, Tickets: []Right{{Kind: RightTicket, Level: 1, Balance: 0}, {Kind: RightTicket, Level: 2, Balance: 1}, {Kind: RightTicket, Level: 4, Balance: 1}}}
	if k, l, ok := PickRight(rights, TierCommon); !ok || k != RightTicket || l != 2 {
		t.Fatalf("common falls back to lowest ticket: %s %d %v", k, l, ok)
	}
	if k, l, ok := PickRight(rights, TierLegend); !ok || l != 4 || k != RightTicket {
		t.Fatalf("legend needs level 4: %s %d %v", k, l, ok)
	}
	rights.Change = 1
	if k, _, _ := PickRight(rights, TierCommon); k != RightChange {
		t.Fatalf("common prefers change card: %s", k)
	}
	if _, _, ok := PickRight(Rights{}, TierFine); ok {
		t.Fatal("no rights")
	}
}

func TestSettingsValidate(t *testing.T) {
	s := DefaultSettings()
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	s.DefaultLength = 7
	if s.Validate() == nil {
		t.Fatal("default length 7 must be rejected")
	}
	var empty Settings
	empty.Normalize()
	if err := empty.Validate(); err != nil {
		t.Fatalf("normalized zero settings: %v", err)
	}
}

func TestTierOddsLuck(t *testing.T) {
	s := DefaultSettings()
	base := s.TierOdds(1)
	lucky := s.TierOdds(2)
	if lucky[TierSupreme] <= base[TierSupreme] || lucky[TierCommon] >= base[TierCommon] {
		t.Fatalf("luck should shift odds: %v vs %v", base, lucky)
	}
}

func TestContainingNumbers(t *testing.T) {
	s := DefaultSettings()
	r := rand.New(rand.NewPCG(3, 4))
	got := ContainingNumbers("520", 8, &s, r)
	if len(got) == 0 {
		t.Fatal("no suggestions")
	}
	for _, n := range got {
		if !strings.Contains(strconv.FormatInt(n, 10), "520") || s.ValidNumber(n) != nil {
			t.Fatalf("bad suggestion %d", n)
		}
	}
}
