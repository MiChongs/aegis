package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"aegis/internal/config"
)

func TestFirewallSnapshotRequestBodyRestoresBody(t *testing.T) {
	firewall := &Firewall{cfg: config.FirewallConfig{RequestBodyLimit: 1024}}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login/password", strings.NewReader(`{"appid":10000,"account":"123456"}`))

	body, err := firewall.snapshotRequestBody(request)
	if err != nil {
		t.Fatalf("snapshotRequestBody returned error: %v", err)
	}
	if string(body) != `{"appid":10000,"account":"123456"}` {
		t.Fatalf("unexpected snapshot body: %s", string(body))
	}

	restored, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("failed to read restored body: %v", err)
	}
	if string(restored) != string(body) {
		t.Fatalf("unexpected restored body: %s", string(restored))
	}
}

func TestFirewallSnapshotRequestBodyRejectsOversizeBody(t *testing.T) {
	firewall := &Firewall{cfg: config.FirewallConfig{RequestBodyLimit: 4}}
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login/password", strings.NewReader(`12345`))

	if _, err := firewall.snapshotRequestBody(request); err == nil {
		t.Fatal("expected oversize body error")
	}
}
