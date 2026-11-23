package backup

import (
	"strings"
	"testing"
)

func TestPGEnvFromDSN(t *testing.T) {
	env, database, err := pgEnvFromDSN("postgres://alice:s3cret@db.example.test:6543/ims_app?sslmode=require")
	if err != nil {
		t.Fatalf("pgEnvFromDSN() error = %v", err)
	}
	if database != "ims_app" {
		t.Fatalf("database = %q, want ims_app", database)
	}
	values := map[string]string{}
	for _, entry := range env {
		name, value, ok := strings.Cut(entry, "=")
		if ok {
			values[name] = value
		}
	}
	want := map[string]string{
		"PGHOST":            "db.example.test",
		"PGPORT":            "6543",
		"PGUSER":            "alice",
		"PGPASSWORD":        "s3cret",
		"PGDATABASE":        "ims_app",
		"PGSSLMODE":         "require",
		"PGCONNECT_TIMEOUT": "10",
	}
	for key, expected := range want {
		if values[key] != expected {
			t.Errorf("%s = %q, want %q", key, values[key], expected)
		}
	}
}

func TestPGEnvFromDSNDefaultsSSLMode(t *testing.T) {
	env, _, err := pgEnvFromDSN("postgres://postgres@127.0.0.1:5432/ims?sslmode=disable")
	if err != nil {
		t.Fatalf("pgEnvFromDSN() error = %v", err)
	}
	found := false
	for _, entry := range env {
		if entry == "PGSSLMODE=disable" {
			found = true
		}
	}
	if !found {
		t.Fatal("PGSSLMODE=disable missing from environment")
	}
}
