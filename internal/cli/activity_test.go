package cli

import (
	"strings"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database/dbtest"
)

func TestActivityPruneCommand(t *testing.T) {
	dsn := dbtest.DSN(t) // skips without DATABASE_TEST_DSN; migrated per package
	t.Setenv("DATABASE_DSN", dsn)
	t.Setenv("APP_ENV_FILE", "")

	out, err := run(t, "activity:prune", "--days", "90")
	if err != nil {
		t.Fatalf("activity:prune: %v\n%s", err, out)
	}
	if !strings.Contains(out, "older than 90 day(s)") {
		t.Errorf("output = %q, want the retention in days", out)
	}

	if out, err := run(t, "activity:prune", "--days", "-1"); err == nil {
		t.Errorf("--days=-1: want an error, got\n%s", out)
	}
}
