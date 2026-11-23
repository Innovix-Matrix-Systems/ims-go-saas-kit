package cli

import (
	"strings"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/platform/database/dbtest"
)

func TestPrivacyEraseNeedsUserAndForce(t *testing.T) {
	if out, err := run(t, "privacy:erase"); err == nil || !strings.Contains(err.Error(), "--user") {
		t.Errorf("no --user: err = %v\n%s", err, out)
	}
	if out, err := run(t, "privacy:erase", "--user", "42"); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Errorf("no --force: err = %v\n%s", err, out)
	}
}

func TestPrivacyCoverageCommand(t *testing.T) {
	dsn := dbtest.DSN(t) // skips without DATABASE_TEST_DSN; migrated per package
	t.Setenv("DATABASE_DSN", dsn)
	t.Setenv("APP_ENV_FILE", "")

	out, err := run(t, "privacy:coverage")
	if err != nil {
		t.Fatalf("privacy:coverage: %v\n%s", err, out)
	}
	for _, want := range []string{"users.invited_by", "activity_log.actor_id", "exempt: the erasure ledger"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}
