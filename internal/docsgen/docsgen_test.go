package docsgen

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestConfigurationParsesSectionsAndNotes(t *testing.T) {
	env := []byte(`# ── App ──────────
APP_ENV=local
APP_DEBUG=true                # verbose errors outside production
# APP_LOG_DIR=                # empty → stdout

# ── Cache ─────────
# Shared drivers are needed in production.
CACHE_DRIVER=memory
`)
	out := Configuration(env)
	for _, want := range []string{
		"## App", "## Cache",
		"| `APP_DEBUG` | `true` | verbose errors outside production |",
		"| `APP_LOG_DIR` | — | *Optional.* empty → stdout |",
		"| `CACHE_DRIVER` | `memory` | Shared drivers are needed in production. |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestMakeListsDocumentedTargets(t *testing.T) {
	out := Make([]byte("build: ## Build the binary.\n\tgo build\nhidden:\n\ttrue\ngen-check: skills-check ## Verify generated code.\n"))
	if !strings.Contains(out, "| `make build` | Build the binary. |") || !strings.Contains(out, "| `make gen-check` | Verify generated code. |") {
		t.Fatalf("documented targets missing:\n%s", out)
	}
	if strings.Contains(out, "hidden") {
		t.Fatalf("undocumented target listed:\n%s", out)
	}
}

func TestCLIListsRunnableCommandsWithFlags(t *testing.T) {
	root := &cobra.Command{Use: "ims"}
	gen := &cobra.Command{Use: "gen", Short: "Scaffold code"}
	module := &cobra.Command{Use: "module", Short: "Scaffold a module", RunE: func(*cobra.Command, []string) error { return nil }}
	module.Flags().String("name", "", "module name")
	gen.AddCommand(module)
	root.AddCommand(gen, &cobra.Command{Use: "migrate", Short: "Apply migrations", RunE: func(*cobra.Command, []string) error { return nil }})

	out := CLI(root)
	for _, want := range []string{"[`gen module`](#gen-module)", "### migrate", "| `--name` | — | module name |"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "### gen\n") {
		t.Error("non-runnable group command listed as a command")
	}
}

func TestPermissionsCoverCatalog(t *testing.T) {
	out := Permissions()
	if !strings.Contains(out, "`branch.create`") || !strings.Contains(out, "Admin") {
		t.Fatalf("permissions page incomplete:\n%.400s", out)
	}
}
