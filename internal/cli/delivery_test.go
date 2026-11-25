package cli

import (
	"strings"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database/dbtest"
)

func TestDeliveryCommands(t *testing.T) {
	dsn := dbtest.DSN(t) // skips without DATABASE_TEST_DSN; migrated per package
	t.Setenv("DATABASE_DSN", dsn)
	t.Setenv("APP_ENV_FILE", "")
	t.Setenv("MAIL_DRIVER", "log")

	out, err := run(t, "delivery:test", "--to", "ops@example.com", "--template", "password-reset", "--locale", "bn")
	if err != nil {
		t.Fatalf("delivery:test: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Sent a test mail to ops@example.com through the log driver") {
		t.Errorf("output = %q", out)
	}
	if out, err := run(t, "delivery:test"); err == nil {
		t.Errorf("without --to: want an error, got\n%s", out)
	}
	if out, err := run(t, "delivery:test", "--to", "ops@example.com", "--template", "nope"); err == nil {
		t.Errorf("unknown template: want an error, got\n%s", out)
	}

	out, err = run(t, "delivery:prune", "--days", "30")
	if err != nil || !strings.Contains(out, "older than 30 day(s)") {
		t.Fatalf("delivery:prune: %v\n%s", err, out)
	}
}
