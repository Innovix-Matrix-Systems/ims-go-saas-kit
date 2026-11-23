package privacy_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/apperr"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/modules/privacy"
	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/testkit"
)

// TestCoverage fails when a migration adds a user-reference column that no
// privacy contributor claims and no exemption explains. Fix it by adding the
// column to the module's PrivacyContributor (export + erase), or to
// privacyExemptions in internal/app/privacy.go with the reason.
func TestCoverage(t *testing.T) {
	app := testkit.New(t)
	coverage, err := app.Deps.Platform.Privacy.Coverage(context.Background())
	if err != nil {
		t.Fatalf("coverage: %v", err)
	}
	if len(coverage) == 0 {
		t.Fatal("no user-reference columns found: the schema query is broken")
	}
	for _, entry := range coverage {
		if !entry.Covered() {
			t.Errorf("%s may hold a user id but no privacy contributor or exemption covers it", entry.Column)
		}
	}
}

func TestEraseRemovesPersonalData(t *testing.T) {
	app := testkit.New(t)
	ctx := context.Background()
	const email, name = "karim.hasan@privacy.test", "Karim Hasan"
	karim := app.User(t, testkit.Email(email), testkit.Name(name))
	// A second admin, so karim is not the tenant's last one.
	app.User(t)

	// Personal data in several modules: sessions and logins (signing in),
	// a failed login carrying the e-mail, activity about the user (created).
	app.Guest().Post("/api/v1/login", map[string]any{"email": email, "password": "wrong-password"})
	app.As(karim).Get("/api/v1/user/profile").Status(t, http.StatusOK)

	request, err := app.Deps.Platform.Privacy.Erase(ctx, privacy.EraseInput{UserID: karim.ID, Source: privacy.SourceCLI})
	if err != nil {
		t.Fatalf("erase: %v", err)
	}
	if request.Status != privacy.StatusCompleted || request.Report["identity"].Anonymized != 1 {
		t.Errorf("request = %+v, want completed with the tombstone", request)
	}

	assertNoPersonalData(t, app.Pool, email, name)

	var tombstone struct {
		Name     string
		Email    string
		IsActive int16
		Erased   bool
	}
	if err := app.Pool.QueryRow(ctx, `SELECT name, email, is_active, erased_at IS NOT NULL FROM users WHERE id = $1`, karim.ID).
		Scan(&tombstone.Name, &tombstone.Email, &tombstone.IsActive, &tombstone.Erased); err != nil {
		t.Fatalf("tombstone: %v", err)
	}
	if tombstone.Name != "Deleted user" || tombstone.Email != fmt.Sprintf("erased+%d@invalid", karim.ID) || tombstone.IsActive != 0 || !tombstone.Erased {
		t.Errorf("tombstone = %+v", tombstone)
	}

	// The old session is gone and the old credentials no longer sign in.
	app.As(karim).Get("/api/v1/user/profile").Status(t, http.StatusUnauthorized)
	if status := app.Guest().Post("/api/v1/login", map[string]any{"email": email, "password": testkit.Password}).Code; status == http.StatusOK {
		t.Error("an erased account signed in")
	}

	// Erasing twice is refused; the address can be used again.
	if _, err := app.Deps.Platform.Privacy.Erase(ctx, privacy.EraseInput{UserID: karim.ID, Source: privacy.SourceCLI}); !apperr.Is(err, apperr.Conflict) {
		t.Errorf("second erase: err = %v, want CONFLICT", err)
	}
	again := app.User(t, testkit.Email(email))
	if again.ID == karim.ID {
		t.Error("the e-mail should create a new account")
	}

	// The erasure is recorded, by id only.
	var recorded int
	if err := app.Pool.QueryRow(ctx, `SELECT count(*) FROM activity_log WHERE action = 'user.erased' AND subject_id = $1`, karim.ID).Scan(&recorded); err != nil || recorded != 1 {
		t.Errorf("user.erased entries = %d (%v), want 1", recorded, err)
	}
}

func TestEraseGuards(t *testing.T) {
	app := testkit.New(t)
	ctx := context.Background()
	service := app.Deps.Platform.Privacy

	if _, err := service.Erase(ctx, privacy.EraseInput{UserID: app.SuperAdmin(t).ID, Source: privacy.SourceCLI}); !apperr.Is(err, apperr.Forbidden) {
		t.Errorf("super admin: err = %v, want FORBIDDEN", err)
	}
	if _, err := service.Erase(ctx, privacy.EraseInput{UserID: 999999, Source: privacy.SourceCLI}); !apperr.Is(err, apperr.ResourceNotFound) {
		t.Errorf("unknown user: err = %v, want RESOURCE_NOT_FOUND", err)
	}

	// The only Admin of a tenant is refused until there is another one.
	tenant := app.Tenant(t, "Privacy Guard Tenant")
	only := app.User(t, testkit.InTenant(tenant))
	_, err := service.Erase(ctx, privacy.EraseInput{UserID: only.ID, Source: privacy.SourceCLI})
	if !apperr.Is(err, apperr.Conflict) || !strings.Contains(err.Error(), "last active Admin") {
		t.Fatalf("last admin: err = %v, want CONFLICT naming the last admin", err)
	}
	var untouched string
	if err := app.Pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, only.ID).Scan(&untouched); err != nil || untouched != only.Email {
		t.Errorf("refused erasure changed the user: %q (%v)", untouched, err)
	}

	app.User(t, testkit.InTenant(tenant))
	if _, err := service.Erase(ctx, privacy.EraseInput{UserID: only.ID, Source: privacy.SourceCLI}); err != nil {
		t.Errorf("with a second admin: %v", err)
	}
}

func TestExport(t *testing.T) {
	app := testkit.New(t)
	ctx := context.Background()
	const email = "export.me@privacy.test"
	user := app.User(t, testkit.Email(email))
	app.As(user).Get("/api/v1/user/profile").Status(t, http.StatusOK)

	var buf bytes.Buffer
	manifest, err := app.Deps.Platform.Privacy.Export(ctx, privacy.ExportInput{UserID: user.ID, Source: privacy.SourceCLI}, &buf)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	archive, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip: %v", err)
	}
	contents := map[string]string{}
	for _, file := range archive.File {
		reader, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(reader)
		_ = reader.Close()
		contents[file.Name] = string(data)
	}

	for _, want := range []string{"manifest.json", "identity/profile.json", "auth/sessions.json", "access/roles.json", "activity/performed.json"} {
		if _, ok := contents[want]; !ok {
			t.Errorf("export has no %s (files: %v)", want, manifest.Files)
		}
	}
	if !strings.Contains(contents["identity/profile.json"], email) {
		t.Errorf("profile.json does not hold the e-mail:\n%s", contents["identity/profile.json"])
	}
	if !strings.Contains(contents["access/roles.json"], "Admin") {
		t.Errorf("roles.json = %s, want the Admin role", contents["access/roles.json"])
	}
	// Credentials are never data about the person. The activity log stores
	// secret-looking keys already redacted; anything else is a leak.
	for name, body := range contents {
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		var document any
		if err := json.Unmarshal([]byte(body), &document); err != nil {
			t.Fatalf("%s is not JSON: %v", name, err)
		}
		assertNoSecrets(t, name, document)
	}

	var exports int
	if err := app.Pool.QueryRow(ctx, `SELECT count(*) FROM privacy_requests WHERE type = 'export' AND status = 'completed' AND user_id = $1`, user.ID).Scan(&exports); err != nil || exports != 1 {
		t.Errorf("export ledger rows = %d (%v), want 1", exports, err)
	}
}

// assertNoSecrets walks a JSON document for credential keys whose value is
// not the redaction placeholder.
func assertNoSecrets(t *testing.T, file string, node any) {
	t.Helper()
	secret := map[string]bool{"password": true, "secret": true, "recovery_codes": true, "remember_token": true, "token": true}
	switch value := node.(type) {
	case map[string]any:
		for key, child := range value {
			if secret[key] && child != "[redacted]" {
				t.Errorf("%s holds %q = %v", file, key, child)
			}
			assertNoSecrets(t, file, child)
		}
	case []any:
		for _, child := range value {
			assertNoSecrets(t, file, child)
		}
	}
}

// assertNoPersonalData scans every text and jsonb column of the application
// tables for the needles. River's tables are skipped: finished jobs (a queued
// mail carries its address) expire with the queue's retention.
func assertNoPersonalData(t *testing.T, pool *pgxpool.Pool, needles ...string) {
	t.Helper()
	ctx := context.Background()
	rows, err := pool.Query(ctx, `
		SELECT table_name, column_name FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND data_type IN ('text', 'character varying', 'character', 'jsonb', 'json')
		  AND table_name NOT LIKE 'river\_%' AND table_name <> 'goose_db_version'
		ORDER BY 1, 2`)
	if err != nil {
		t.Fatalf("columns: %v", err)
	}
	type column struct{ table, name string }
	columns, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (column, error) {
		var c column
		return c, row.Scan(&c.table, &c.name)
	})
	if err != nil {
		t.Fatalf("columns: %v", err)
	}
	for _, c := range columns {
		for _, needle := range needles {
			var hits int
			query := fmt.Sprintf(`SELECT count(*) FROM %s WHERE %s::text ILIKE '%%' || $1 || '%%'`,
				pgx.Identifier{c.table}.Sanitize(), pgx.Identifier{c.name}.Sanitize())
			if err := pool.QueryRow(ctx, query, needle).Scan(&hits); err != nil {
				t.Fatalf("%s.%s: %v", c.table, c.name, err)
			}
			if hits > 0 {
				t.Errorf("%s.%s still holds %q in %d row(s)", c.table, c.name, needle, hits)
			}
		}
	}
}
