package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveEnvFilePathSearchesParentDirectories(t *testing.T) {
	root := t.TempDir()
	envPath := filepath.Join(root, ".env")
	if err := os.WriteFile(envPath, []byte("JWT_SECRET=test\n"), 0o600); err != nil {
		t.Fatalf("write .env: %v", err)
	}

	nested := filepath.Join(root, ".runtime", "bin")
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

	path, err := resolveEnvFilePath()
	if err != nil {
		t.Fatalf("resolveEnvFilePath: %v", err)
	}
	if path != envPath {
		t.Fatalf("expected %q, got %q", envPath, path)
	}
}

func TestResolveEnvFilePathHonorsAEGISENVFILE(t *testing.T) {
	root := t.TempDir()
	envPath := filepath.Join(root, "custom.env")
	if err := os.WriteFile(envPath, []byte("JWT_SECRET=test\n"), 0o600); err != nil {
		t.Fatalf("write custom env: %v", err)
	}

	oldValue, hadValue := os.LookupEnv("AEGIS_ENV_FILE")
	if err := os.Setenv("AEGIS_ENV_FILE", envPath); err != nil {
		t.Fatalf("setenv: %v", err)
	}
	t.Cleanup(func() {
		if hadValue {
			_ = os.Setenv("AEGIS_ENV_FILE", oldValue)
			return
		}
		_ = os.Unsetenv("AEGIS_ENV_FILE")
	})

	path, err := resolveEnvFilePath()
	if err != nil {
		t.Fatalf("resolveEnvFilePath: %v", err)
	}
	if path != envPath {
		t.Fatalf("expected %q, got %q", envPath, path)
	}
}
