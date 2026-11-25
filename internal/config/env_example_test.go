package config

import (
	"strings"
	"testing"

	"github.com/joho/godotenv"
)

// .env.example is copied to .env as-is (`make env`), so it must load: no key
// may read a trailing comment as its value, and the whole file must pass Load
// and Validate.
func TestEnvExampleLoads(t *testing.T) {
	values, err := godotenv.Read("../../.env.example")
	if err != nil {
		t.Fatalf("read .env.example: %v", err)
	}
	for key, value := range values {
		if strings.HasPrefix(strings.TrimSpace(value), "#") {
			t.Errorf("%s reads its comment as the value (%q): put the comment on its own line", key, value)
		}
	}

	t.Setenv("APP_ENV_FILE", "")
	for key, value := range values {
		t.Setenv(key, value)
	}
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() with .env.example: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() with .env.example: %v", err)
	}
}
