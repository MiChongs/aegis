package timeutil

import (
	"testing"
	"time"
)

func TestParseRFC3339Strict(t *testing.T) {
	parsed, err := ParseRFC3339Strict("2026-03-30T10:00:00+08:00")
	if err != nil {
		t.Fatalf("expected strict parse success, got error: %v", err)
	}
	if parsed.Location() != time.UTC {
		t.Fatalf("expected UTC result, got %s", parsed.Location())
	}
}

func TestParseRFC3339StrictRejectsNaive(t *testing.T) {
	if _, err := ParseRFC3339Strict("2026-03-30 10:00:00"); err == nil {
		t.Fatal("expected naive datetime to be rejected")
	}
}

func TestNextOccurrence(t *testing.T) {
	next, err := NextOccurrence(RuleSchedule{
		TimeOfDay: LocalTimeOfDay{Hour: 9, Minute: 0, Second: 0},
		Timezone:  "Asia/Shanghai",
	}, time.Date(2026, 3, 30, 1, 30, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("expected next occurrence, got error: %v", err)
	}
	expected := time.Date(2026, 3, 31, 1, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Fatalf("unexpected next occurrence: got %s want %s", next.Format(time.RFC3339), expected.Format(time.RFC3339))
	}
}
