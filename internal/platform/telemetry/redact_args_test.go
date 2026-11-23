package telemetry

import (
	"strings"
	"testing"
)

func TestArgsRedactsCredentials(t *testing.T) {
	r := NewRedactor(nil)
	hash := "$2a$12$abcdefghijklmnopqrstuuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ012"

	got := string(r.Args("UPDATE users SET password = $1 WHERE id = $2", []any{"plain-or-hash", int64(7)}))
	if strings.Contains(got, "plain-or-hash") || !strings.Contains(got, "7") {
		t.Fatalf("sensitive statement args = %s", got)
	}
	got = string(r.Args("INSERT INTO audit (note) VALUES ($1)", []any{hash}))
	if strings.Contains(got, "$2a$") {
		t.Fatalf("bcrypt hash leaked: %s", got)
	}
	got = string(r.Args("SELECT * FROM branches WHERE name = $1", []any{"Dhaka"}))
	if !strings.Contains(got, "Dhaka") {
		t.Fatalf("ordinary args over-redacted: %s", got)
	}
}
