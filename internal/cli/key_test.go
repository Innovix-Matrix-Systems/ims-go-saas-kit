package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/config"
)

func TestKeyGenerateCommand(t *testing.T) {
	envFile := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envFile, []byte("APP_NAME=test\nAPP_KEY=\n"), 0o600); err != nil {
		t.Fatalf("write env: %v", err)
	}
	t.Setenv("APP_ENV_FILE", envFile)
	t.Setenv("APP_KEY", "")

	out, err := run(t, "key:generate", "--show")
	if err != nil || !strings.HasPrefix(strings.TrimSpace(out), "base64:") {
		t.Fatalf("key:generate --show = %q, %v", out, err)
	}

	if out, err := run(t, "key:generate"); err != nil {
		t.Fatalf("key:generate: %v\n%s", err, out)
	}
	contents, _ := os.ReadFile(envFile)
	line := ""
	for _, l := range strings.Split(string(contents), "\n") {
		if strings.HasPrefix(l, "APP_KEY=") {
			line = l
		}
	}
	t.Setenv("APP_KEY", strings.TrimPrefix(line, "APP_KEY="))
	cfg, err := config.Load(envFile)
	if err != nil || cfg.AppKey.Development || len(cfg.AppKey.Key) != config.AppKeyLength {
		t.Fatalf("the written key does not load: %q, %v", line, err)
	}

	if out, err := run(t, "key:generate"); err == nil {
		t.Fatalf("replacing a key without --force: want an error, got\n%s", out)
	}
	if out, err := run(t, "key:generate", "--force"); err != nil {
		t.Fatalf("key:generate --force: %v\n%s", err, out)
	}
}
