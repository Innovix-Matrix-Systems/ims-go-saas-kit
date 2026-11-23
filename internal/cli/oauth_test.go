package cli

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/Innovix-Matrix-Systems/ims-go-api-starter/internal/platform/database/dbtest"
)

// run executes one command through BuildRoot, as `ims` would.
func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root, err := BuildRoot(&State{})
	if err != nil {
		t.Fatalf("BuildRoot() error = %v", err)
	}
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	err = root.Execute()
	return buf.String(), err
}

func TestOAuthClientCommands(t *testing.T) {
	dsn := dbtest.DSN(t) // skips without DATABASE_TEST_DSN; migrated per package
	t.Setenv("DATABASE_DSN", dsn)
	t.Setenv("APP_ENV_FILE", "")

	out, err := run(t, "oauth:client:create", "--name", "CLI smoke", "--redirect", "https://agent.example.com/cb", "--confidential", "--read-only")
	if err != nil {
		t.Fatalf("create: %v\n%s", err, out)
	}
	clientID := regexp.MustCompile(`client_id:\s+(ims_[0-9a-f]+)`).FindStringSubmatch(out)
	if clientID == nil || !strings.Contains(out, "client_secret:") || !strings.Contains(out, "scopes:        mcp:read\n") {
		t.Fatalf("create output = %q", out)
	}

	out, err = run(t, "oauth:client:list")
	if err != nil || !strings.Contains(out, clientID[1]) || !strings.Contains(out, "confidential") {
		t.Fatalf("list: %v\n%s", err, out)
	}
	id := strings.Fields(out[strings.Index(out, "\n")+1:])[0]

	out, err = run(t, "oauth:client:rotate", id)
	if err != nil || !strings.Contains(out, "client_secret:") {
		t.Fatalf("rotate: %v\n%s", err, out)
	}

	if out, err = run(t, "oauth:client:create", "--name", "Bad", "--redirect", "http://example.com/cb"); err == nil {
		t.Fatalf("a non-https, non-loopback redirect must be refused:\n%s", out)
	}

	out, err = run(t, "oauth:client:delete", id)
	if err != nil || !strings.Contains(out, "deleted") {
		t.Fatalf("delete: %v\n%s", err, out)
	}
	if out, err = run(t, "oauth:client:rotate", id); err == nil {
		t.Fatalf("rotating a deleted client must fail:\n%s", out)
	}
}
