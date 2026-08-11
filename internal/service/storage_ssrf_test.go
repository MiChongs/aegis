package service

import (
	"net"
	"testing"

	"aegis/pkg/egress"
)

func TestValidateStorageEndpointRejectsPrivateTargets(t *testing.T) {
	t.Parallel()

	for _, endpoint := range []string{
		"http://127.0.0.1:8080",
		"http://[::1]/dav",
		"http://10.0.0.5/dav",
		"http://169.254.169.254/latest/meta-data",
		"file:///etc/passwd",
		"http://user:pass@example.com/dav",
	} {
		if err := validateStorageEndpoint(endpoint); err == nil {
			t.Errorf("期望拒绝 Endpoint %q", endpoint)
		}
	}
}

func TestValidateStorageEndpointAllowsPublicHTTPS(t *testing.T) {
	t.Parallel()

	if err := validateStorageEndpoint("https://storage.example.com/dav"); err != nil {
		t.Fatalf("公开 HTTPS Endpoint 被拒绝: %v", err)
	}
}

func TestBlockedOutboundIPClassification(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"127.0.0.1", "10.1.2.3", "169.254.169.254", "100.64.0.1", "::1", "fc00::1"} {
		if !egress.IsBlockedOutboundIP(net.ParseIP(raw)) {
			t.Errorf("期望阻止 IP %s", raw)
		}
	}
	if egress.IsBlockedOutboundIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("公开 IP 不应被阻止")
	}
}
