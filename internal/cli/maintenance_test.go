package cli

import (
	"strings"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/platform/database/dbtest"
)

func TestMaintenanceCommands(t *testing.T) {
	dsn := dbtest.DSN(t) // skips without DATABASE_TEST_DSN; migrated per package
	t.Setenv("DATABASE_DSN", dsn)
	t.Setenv("APP_ENV_FILE", "")

	steps := []struct {
		args []string
		want []string
	}{
		{[]string{"maintenance:status"}, []string{"is live"}},
		{[]string{"down", "--message", "CLI smoke", "--retry", "30", "--with-secret"},
			[]string{"now in maintenance mode", "Message:     CLI smoke", "Retry-After: 30s", "X-Maintenance-Bypass: "}},
		{[]string{"maintenance:status"}, []string{"DOWN for maintenance", "CLI smoke", "secret set"}},
		{[]string{"up"}, []string{"now live"}},
		{[]string{"up"}, []string{"was not in maintenance mode"}},
	}
	for _, step := range steps {
		out, err := run(t, step.args...)
		if err != nil {
			t.Fatalf("ims %s: %v\n%s", strings.Join(step.args, " "), err, out)
		}
		for _, want := range step.want {
			if !strings.Contains(out, want) {
				t.Errorf("ims %s output = %q, want %q", strings.Join(step.args, " "), out, want)
			}
		}
	}

	if out, err := run(t, "down", "--secret", "short"); err == nil {
		t.Errorf("down --secret=short: want a validation error, got\n%s", out)
	}
}
