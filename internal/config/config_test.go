package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("APP_ENV_FILE", "")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.App.Name != "ims-go-admin-starter" {
		t.Errorf("App.Name = %q, want default", cfg.App.Name)
	}
	if cfg.HTTP.ListenAddr != ":8080" {
		t.Errorf("HTTP.ListenAddr = %q, want :8080", cfg.HTTP.ListenAddr)
	}
	if cfg.HTTP.ReadTimeout != 15*time.Second {
		t.Errorf("HTTP.ReadTimeout = %v, want 15s", cfg.HTTP.ReadTimeout)
	}
	if cfg.RateLimit.PerMinute != 1000 {
		t.Errorf("RateLimit.PerMinute = %d, want 1000", cfg.RateLimit.PerMinute)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() error = %v", err)
	}
}

func TestLoadEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := "APP_NAME=test-app\nAPP_ENV=Staging\nHTTP_READ_TIMEOUT=20\nRATE_LIMIT_PER_MINUTE=50\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	t.Cleanup(func() {
		os.Unsetenv("APP_NAME")
		os.Unsetenv("APP_ENV")
		os.Unsetenv("HTTP_READ_TIMEOUT")
		os.Unsetenv("RATE_LIMIT_PER_MINUTE")
	})

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.App.Name != "test-app" {
		t.Errorf("App.Name = %q, want test-app", cfg.App.Name)
	}
	if cfg.App.Env != EnvStaging {
		t.Errorf("App.Env = %q, want %q (normalised: trimmed + lowercased)", cfg.App.Env, EnvStaging)
	}
	if cfg.HTTP.ReadTimeout != 20*time.Second {
		t.Errorf("HTTP.ReadTimeout = %v, want 20s (integer seconds fallback)", cfg.HTTP.ReadTimeout)
	}
	if cfg.RateLimit.PerMinute != 50 {
		t.Errorf("RateLimit.PerMinute = %d, want 50", cfg.RateLimit.PerMinute)
	}
	if cfg.App.EnvFile != path {
		t.Errorf("App.EnvFile = %q, want %q", cfg.App.EnvFile, path)
	}
}

func TestLoadRejectsUnknownEnv(t *testing.T) {
	t.Setenv("APP_ENV_FILE", "")
	t.Setenv("APP_ENV", "prodution") // typo — must fail, not silently disable hardening
	if _, err := Load(""); err == nil {
		t.Fatal("Load() with unknown APP_ENV = nil error, want error")
	}
}

func TestEnvNormalisation(t *testing.T) {
	for raw, want := range map[string]string{
		"  production ": "production",
		"PRODUCTION":    "production",
		" Dev ":         EnvDev,
	} {
		t.Setenv("APP_ENV_FILE", "")
		t.Setenv("APP_ENV", raw)
		cfg, err := Load("")
		if err != nil {
			t.Fatalf("Load(%q) error = %v", raw, err)
		}
		if cfg.App.Env != want {
			t.Errorf("App.Env for %q = %q, want %q", raw, cfg.App.Env, want)
		}
	}
}

func TestLoadMissingExplicitEnvFile(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "missing.env")); err == nil {
		t.Fatal("Load() with explicit missing env file = nil error, want error")
	}
}

func TestLoadInvalidDuration(t *testing.T) {
	t.Setenv("HTTP_READ_TIMEOUT", "not-a-duration")
	if _, err := Load(""); err == nil {
		t.Fatal("Load() with invalid duration = nil error, want error")
	}
}

func TestValidateFallbackLocaleMembership(t *testing.T) {
	t.Setenv("APP_SUPPORTED_LOCALES", "en")
	t.Setenv("APP_FALLBACK_LOCALE", "fr")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() = nil error, want fallback-locale membership error")
	}
}

func TestDatabaseDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Database.Primary.Driver != "pgsql" {
		t.Errorf("Primary.Driver = %q, want pgsql", cfg.Database.Primary.Driver)
	}
	if cfg.Database.Primary.MaxConns != 20 {
		t.Errorf("Primary.MaxConns = %d, want 20", cfg.Database.Primary.MaxConns)
	}
	if len(cfg.Database.Connections) != 1 || cfg.Database.Connections[0].Name != "primary" {
		t.Errorf("Connections = %+v, want just primary", cfg.Database.Connections)
	}
}

func TestDatabaseNamedConnections(t *testing.T) {
	t.Setenv("DATABASE_CONNECTIONS", "primary,analytics")
	t.Setenv("DATABASE_ANALYTICS_DSN", "postgres://analytics:secret@127.0.0.1:5432/analytics?sslmode=disable")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(cfg.Database.Connections) != 2 {
		t.Fatalf("Connections length = %d, want 2", len(cfg.Database.Connections))
	}
	analytics := cfg.Database.Connections[1]
	if analytics.Name != "analytics" || analytics.DSN == "" {
		t.Errorf("analytics = %+v", analytics)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate() error = %v", err)
	}
}

func TestValidateRejectsSQLite(t *testing.T) {
	t.Setenv("DATABASE_DRIVER", "sqlite")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() with sqlite = nil error, want unsupported-driver error")
	}
}

func TestValidateRejectsBadPoolLimits(t *testing.T) {
	t.Setenv("DATABASE_MIN_CONNS", "30")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() with min > max = nil error, want error")
	}
}

func TestIsLocal(t *testing.T) {
	cases := map[string]bool{
		"local":       true,
		"development": true,
		"dev":         true,
		"staging":     false,
		"production":  false,
		"":            false,
	}
	for env, want := range cases {
		cfg := &Config{App: App{Env: env}}
		if got := cfg.IsLocal(); got != want {
			t.Errorf("IsLocal() with APP_ENV=%q = %v, want %v", env, got, want)
		}
	}
}
