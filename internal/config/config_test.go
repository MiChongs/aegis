package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
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

func TestSetDefaultsPostgresSessionTimezoneFollowsDefaultTimezone(t *testing.T) {
	cfg := Config{}
	setDefaults(&cfg)

	if cfg.DefaultTimezone != "Asia/Shanghai" {
		t.Fatalf("expected default timezone Asia/Shanghai, got %q", cfg.DefaultTimezone)
	}
	if cfg.Postgres.SessionTimezone != cfg.DefaultTimezone {
		t.Fatalf("expected postgres session timezone to follow default timezone %q, got %q", cfg.DefaultTimezone, cfg.Postgres.SessionTimezone)
	}
}

func TestLoadWithViperKeepsExplicitPostgresSessionTimezone(t *testing.T) {
	v := viper.New()
	v.Set("JWT_SECRET", "test-secret")
	v.Set("POSTGRES_DSN", "postgres://aegis:aegis@localhost:5432/aegis?sslmode=disable")
	v.Set("REDIS_ADDR", "127.0.0.1:6379")
	v.Set("NATS_URL", "nats://127.0.0.1:4222")
	v.Set("APP_DEFAULT_IANA_TIMEZONE", "Asia/Shanghai")
	v.Set("POSTGRES_SESSION_TIMEZONE", "UTC")

	cfg, err := loadWithViper(v)
	if err != nil {
		t.Fatalf("loadWithViper: %v", err)
	}
	if cfg.Postgres.SessionTimezone != "UTC" {
		t.Fatalf("expected explicit postgres session timezone UTC, got %q", cfg.Postgres.SessionTimezone)
	}
}

func TestLoadWithViperParsesTrustedProxies(t *testing.T) {
	v := viper.New()
	v.Set("JWT_SECRET", "test-secret")
	v.Set("POSTGRES_DSN", "postgres://aegis:aegis@localhost:5432/aegis?sslmode=disable")
	v.Set("REDIS_ADDR", "127.0.0.1:6379")
	v.Set("NATS_URL", "nats://127.0.0.1:4222")
	v.Set("TRUSTED_PROXIES", "127.0.0.1/32, 10.0.0.0/8")
	v.Set("CLIENT_IP_STRATEGY", "trusted-ranges")
	v.Set("CLIENT_IP_LIST_HEADER", "Forwarded")
	v.Set("CLIENT_IP_HOPS", "2")

	cfg, err := loadWithViper(v)
	if err != nil {
		t.Fatalf("loadWithViper() error = %v", err)
	}
	if got, want := strings.Join(cfg.ClientIP.TrustedProxies, ","), "127.0.0.1/32,10.0.0.0/8"; got != want {
		t.Fatalf("ClientIP.TrustedProxies = %q, want %q", got, want)
	}
	if got, want := cfg.ClientIP.Strategy, "trusted-ranges"; got != want {
		t.Fatalf("ClientIP.Strategy = %q, want %q", got, want)
	}
	if got, want := cfg.ClientIP.ListHeader, "Forwarded"; got != want {
		t.Fatalf("ClientIP.ListHeader = %q, want %q", got, want)
	}
	if got, want := cfg.ClientIP.Hops, 2; got != want {
		t.Fatalf("ClientIP.Hops = %d, want %d", got, want)
	}
}
