package bootstrap

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveProjectPathSearchesParentDirectories(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "migrations", "postgres")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}

	nested := filepath.Join(root, "cmd", "server")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(oldWD)
	})

	if err := os.Chdir(nested); err != nil {
		t.Fatalf("chdir nested: %v", err)
	}

	got, err := resolveProjectPath(filepath.Join("migrations", "postgres"))
	if err != nil {
		t.Fatalf("resolveProjectPath: %v", err)
	}
	if got != target {
		t.Fatalf("expected %q, got %q", target, got)
	}
}
