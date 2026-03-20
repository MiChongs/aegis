package service

import (
	"net/http/httptest"
	"testing"
)

func TestExtractRealtimeToken(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/ws?access_token=query-token", nil)
	req.Header.Set("Authorization", "Bearer header-token")
	if token := extractRealtimeToken(req); token != "header-token" {
		t.Fatalf("expected header token, got %q", token)
	}

	req = httptest.NewRequest("GET", "/api/ws?token=query-token", nil)
	if token := extractRealtimeToken(req); token != "query-token" {
		t.Fatalf("expected query token, got %q", token)
	}
}
