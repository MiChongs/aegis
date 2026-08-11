package postgres

import (
	"testing"
	"time"
)

func TestSignDateDeltaUsesLocalCalendarDays(t *testing.T) {
	location := time.FixedZone("CST", 8*3600)
	signDate := time.Date(2026, 3, 29, 0, 55, 0, 0, location)

	delta, err := signDateDelta("2026-03-28", signDate)
	if err != nil {
		t.Fatalf("signDateDelta returned error: %v", err)
	}
	if delta != 1 {
		t.Fatalf("expected delta 1 day, got %d", delta)
	}
}

func TestSignDateDeltaSameDay(t *testing.T) {
	location := time.FixedZone("CST", 8*3600)
	signDate := time.Date(2026, 3, 29, 7, 59, 59, 0, location)

	delta, err := signDateDelta("2026-03-29", signDate)
	if err != nil {
		t.Fatalf("signDateDelta returned error: %v", err)
	}
	if delta != 0 {
		t.Fatalf("expected delta 0 day, got %d", delta)
	}
}
