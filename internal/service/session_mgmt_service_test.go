package service

import (
	"testing"
	"time"

	admindomain "aegis/internal/domain/admin"
	"aegis/pkg/timeutil"
)

func TestStaleAdminSessionIDs(t *testing.T) {
	active := []admindomain.AdminSessionRecord{
		{ID: "sess-1"},
		{ID: "sess-2"},
	}
	current := []string{"sess-1", "stale-1", "", "stale-1", "sess-2", "stale-2"}

	got := staleAdminSessionIDs(current, active)
	want := []string{"stale-1", "stale-2"}

	if len(got) != len(want) {
		t.Fatalf("unexpected stale session count: got %d want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected stale session order: got %v want %v", got, want)
		}
	}
}

func TestLatestAdminSessionActivity(t *testing.T) {
	now := timeutil.NowUTC().Truncate(time.Second)
	items := []admindomain.AdminSessionRecord{
		{ID: "sess-1", LastActiveAt: now.Add(-10 * time.Minute)},
		{ID: "sess-2", LastActiveAt: now.Add(-2 * time.Minute)},
		{ID: "sess-3", LastActiveAt: now.Add(-5 * time.Minute)},
	}

	got := latestAdminSessionActivity(items)
	want := now.Add(-2 * time.Minute)

	if !got.Equal(want) {
		t.Fatalf("unexpected latest activity: got %s want %s", got.Format(time.RFC3339), want.Format(time.RFC3339))
	}
}
