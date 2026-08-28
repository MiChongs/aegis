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
	if token := extractRealtimeToken(req); token != "" {
		t.Fatalf("query token must be rejected, got %q", token)
	}

	req = httptest.NewRequest("GET", "/api/ws", nil)
	req.Header.Set("Sec-WebSocket-Protocol", "aegis, aegis.jwt.protocol-token")
	if token := extractRealtimeToken(req); token != "protocol-token" {
		t.Fatalf("expected websocket protocol token, got %q", token)
	}
}

func TestWebSocketOriginChecker(t *testing.T) {
	check := WebSocketOriginChecker([]string{"https://console.example.com"})

	sameHost := httptest.NewRequest("GET", "https://api.example.com/api/ws", nil)
	sameHost.Header.Set("Origin", "https://api.example.com")
	if !check(sameHost) {
		t.Fatal("同源 WebSocket 应被允许")
	}

	allowed := httptest.NewRequest("GET", "https://api.example.com/api/ws", nil)
	allowed.Header.Set("Origin", "https://console.example.com")
	if !check(allowed) {
		t.Fatal("显式允许的跨域 WebSocket 应被允许")
	}

	blocked := httptest.NewRequest("GET", "https://api.example.com/api/ws", nil)
	blocked.Header.Set("Origin", "https://evil.example")
	if check(blocked) {
		t.Fatal("未允许的跨域 WebSocket 必须被拒绝")
	}
}
