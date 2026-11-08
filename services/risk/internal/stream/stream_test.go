package stream

import (
	"strings"
	"testing"

	"aegis/services/risk/internal/bundle"
)

func TestSQLNATSAuthAndPreviewMask(t *testing.T) {
	f := bundle.Feature{Key: "fails", Kind: bundle.FeatureStream, Entity: bundle.EntityUser,
		Stream: &bundle.StreamDef{Events: []string{"login.fail"}, Agg: "count", WindowSeconds: 60}}
	plain, err := SQL(f, Target{NATSServer: "n:4222", RedisURLs: []string{"redis://r:1"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plain[0], "connect_mode = 'plain'") {
		t.Fatalf("plain mode missing:\n%s", plain[0])
	}
	auth := Target{NATSServer: "n:4222", NATSUser: "app", NATSPassword: "s3cret", RedisURLs: []string{"redis://:rpw@r:1"}}
	stmts, err := SQL(f, auth)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stmts[0], "connect_mode = 'user_and_password', username = 'app', password = 's3cret'") {
		t.Fatalf("auth mode missing:\n%s", stmts[0])
	}
	pv, err := Preview(f, auth)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(pv, "s3cret") || strings.Contains(pv, "rpw") {
		t.Fatalf("preview leaks secrets:\n%s", pv)
	}
}
