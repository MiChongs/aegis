package clientip

import (
	"context"
	"testing"
)

func TestContextIP(t *testing.T) {
	ctx := context.Background()
	if got := FromContext(ctx, " 10.0.1.9 "); got != "10.0.1.9" {
		t.Fatalf("fallback: %q", got)
	}
	ctx = WithIP(ctx, "2409:8962:5a6:57::1")
	if got := FromContext(ctx, "10.0.1.9"); got != "2409:8962:5a6:57::1" {
		t.Fatalf("from ctx: %q", got)
	}
	if WithIP(context.Background(), "") != context.Background() {
		t.Fatal("empty ip should not wrap")
	}
}
