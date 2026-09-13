package config

import (
	"strings"
	"testing"
)

func TestValidateRefusesEmptyDBPath(t *testing.T) {
	t.Setenv("DB_PATH", "")
	t.Setenv("DATABASE_URL", "postgresql://user:pass@127.0.0.1:5432/cqim")
	cfg := Load()
	if cfg.DBPath != "" {
		t.Fatalf("DBPath=%q, want empty default", cfg.DBPath)
	}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected Validate to fail when DB_PATH is unset")
	}
	if !strings.Contains(err.Error(), "DB_PATH is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateAcceptsNodeLocalDBPath(t *testing.T) {
	t.Setenv("DB_PATH", "/var/lib/cqim/gateway-local.db")
	cfg := Load()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidateRefusesSharedSQLitePaths(t *testing.T) {
	cases := []string{
		"/app/data/cqim.db",
		"file:/app/data/cqim.db",
		"cqim.db",
		"/home/ubuntu/cqim_shared/data/gateway.db",
	}
	for _, path := range cases {
		cfg := &Config{DBPath: path}
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected Validate to refuse %q", path)
		}
	}
}

func TestValidateDoesNotUseDatabaseURLAsFallback(t *testing.T) {
	t.Setenv("DB_PATH", "")
	t.Setenv("DATABASE_URL", "file:/app/data/cqim.db")
	cfg := Load()
	if cfg.DBPath != "" {
		t.Fatalf("DBPath must not inherit DATABASE_URL, got %q", cfg.DBPath)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("empty DB_PATH must fail closed even if DATABASE_URL is set")
	}
}
