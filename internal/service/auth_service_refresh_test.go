package service

import (
	"context"
	"testing"
	"time"

	"aegis/internal/domain/auth"
	redisrepo "aegis/internal/repository/redis"
	miniredis "github.com/alicebob/miniredis/v2"
	redislib "github.com/redis/go-redis/v9"
)

func TestRevokeAccessSessionsByFamily(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("start miniredis: %v", err)
	}
	defer mr.Close()

	client := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	defer client.Close()

	repo := redisrepo.NewSessionRepository(client, "test")
	svc := &AuthService{sessions: repo}
	ctx := context.Background()
	now := time.Now().UTC()

	seedSession := func(token string, session auth.Session) {
		t.Helper()
		if err := repo.SetSession(ctx, token, session, time.Hour); err != nil {
			t.Fatalf("set session %s: %v", token, err)
		}
	}

	seedSession("token-old-1", auth.Session{
		UserID:          9,
		AppID:           10000,
		Account:         "demo",
		TokenID:         "access-old-1",
		RefreshFamilyID: "family-a",
		ExpiresAt:       now.Add(time.Hour),
		IssuedAt:        now.Add(-2 * time.Minute),
	})
	seedSession("token-old-2", auth.Session{
		UserID:          9,
		AppID:           10000,
		Account:         "demo",
		TokenID:         "access-old-2",
		RefreshFamilyID: "family-a",
		ExpiresAt:       now.Add(time.Hour),
		IssuedAt:        now.Add(-1 * time.Minute),
	})
	seedSession("token-keep", auth.Session{
		UserID:          9,
		AppID:           10000,
		Account:         "demo",
		TokenID:         "access-keep",
		RefreshFamilyID: "family-b",
		ExpiresAt:       now.Add(time.Hour),
		IssuedAt:        now,
	})

	if err := svc.revokeAccessSessionsByFamily(ctx, 10000, 9, "family-a", ""); err != nil {
		t.Fatalf("revoke access sessions by family: %v", err)
	}

	items, err := repo.ListUserSessions(ctx, 10000, 9)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 remaining session, got %d", len(items))
	}
	if items[0].Session.TokenID != "access-keep" {
		t.Fatalf("expected remaining session access-keep, got %s", items[0].Session.TokenID)
	}

	for _, tokenID := range []string{"access-old-1", "access-old-2"} {
		blacklisted, err := repo.IsBlacklisted(ctx, tokenID)
		if err != nil {
			t.Fatalf("check blacklist %s: %v", tokenID, err)
		}
		if !blacklisted {
			t.Fatalf("expected token %s to be blacklisted", tokenID)
		}
	}

	blacklisted, err := repo.IsBlacklisted(ctx, "access-keep")
	if err != nil {
		t.Fatalf("check blacklist keep token: %v", err)
	}
	if blacklisted {
		t.Fatalf("expected keep token to remain active")
	}
}
