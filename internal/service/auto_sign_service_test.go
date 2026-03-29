package service

import (
	"testing"
	"time"

	userdomain "aegis/internal/domain/user"
)

func TestAutoSignServiceIsEligible(t *testing.T) {
	service := &AutoSignService{}

	if !service.isEligible(userdomain.AutoSignCandidate{
		Enabled:         true,
		SettingsEnabled: true,
	}) {
		t.Fatal("expected enabled auto-sign user to be eligible without VIP requirement")
	}

	if service.isEligible(userdomain.AutoSignCandidate{
		Enabled:         false,
		SettingsEnabled: true,
	}) {
		t.Fatal("expected disabled user to be ineligible")
	}

	if service.isEligible(userdomain.AutoSignCandidate{
		Enabled:         true,
		SettingsEnabled: false,
	}) {
		t.Fatal("expected user with auto-sign setting disabled to be ineligible")
	}
}

func TestAutoSignServiceInitialDue(t *testing.T) {
	location := time.FixedZone("CST", 8*3600)
	service := &AutoSignService{location: location}
	reference := time.Date(2026, 3, 29, 9, 30, 0, 0, location)

	due := service.initialDue(reference, "10:15", location)
	expected := time.Date(2026, 3, 29, 10, 15, 0, 0, location)
	if !due.Equal(expected) {
		t.Fatalf("expected due %v, got %v", expected, due)
	}

	pastDue := service.initialDue(reference, "08:15", location)
	if !pastDue.Equal(reference) {
		t.Fatalf("expected past due to fall back to reference %v, got %v", reference, pastDue)
	}
}
